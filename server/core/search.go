package core

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"modernc.org/sqlite"
)

// The search index is one row per timeline entry: the text a reader would look for it by. The
// entry's own words (a repost or quote carries some), its post (title, text, hashtags, author),
// the post it answers (text and author) and the comments under it. It is an FTS5 table with
// the trigram tokenizer, so any run of three or more characters is found, in any language and
// without regard to case. Shorter words are found by a scan of the same table.
//
// The index follows what is stored: an ingest that changes something refreshes the entries it
// touches. Hidden entries stay in the index and are left out by the timeline's own filter.

func init() {
	// A regular expression match for the index: regexp(pattern, text), case-insensitive.
	sqlite.MustRegisterScalarFunction("regexp", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		pattern, _ := args[0].(string)
		text, _ := args[1].(string)
		re, err := compileSearchPattern(pattern)
		if err != nil {
			return nil, err
		}
		return re.MatchString(text), nil
	})
}

// maxSearchPattern bounds what a visitor may type as a regular expression; maxCachedPatterns bounds how many compiled
// patterns are kept. When the cache is full it starts again, so memory stays bounded whatever visitors type.
const (
	maxSearchPattern  = 200
	maxCachedPatterns = 64
)

var patterns struct {
	sync.Mutex
	compiled map[string]*regexp.Regexp
}

func compileSearchPattern(pattern string) (*regexp.Regexp, error) {
	if len(pattern) > maxSearchPattern {
		return nil, badf("a pattern is at most %d characters", maxSearchPattern)
	}
	patterns.Lock()
	defer patterns.Unlock()
	if re, ok := patterns.compiled[pattern]; ok {
		return re, nil
	}
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, err
	}
	if patterns.compiled == nil || len(patterns.compiled) >= maxCachedPatterns {
		patterns.compiled = map[string]*regexp.Regexp{}
	}
	patterns.compiled[pattern] = re
	return re, nil
}

// ValidSearchPattern reports whether pattern is a regular expression the search accepts.
func ValidSearchPattern(pattern string) error {
	_, err := compileSearchPattern(pattern)
	return err
}

// searchText is the SQL expression of an entry's search text; a is the activity and o the
// latest version of its object.
const searchText = `coalesce(json_extract(a.document,'$.content'),'') || char(10) ||
	coalesce(json_extract(o.document,'$.name'),'') || char(10) ||
	coalesce(json_extract(o.document,'$.content'),'') || char(10) ||
	coalesce(json_extract(o.document,'$.summary'),'') || char(10) ||
	coalesce((SELECT group_concat(json_extract(t.value,'$.name'),' ') FROM json_each(o.document,'$.tag') t),'') || char(10) ||
	coalesce(` + `(SELECT json_extract(p.document,'$.name') FROM object_version p WHERE p.object_id=o.attributed_to ORDER BY p.id DESC LIMIT 1)` + `,'') || char(10) ||
	coalesce(` + `(SELECT coalesce(json_extract(q.document,'$.name'),'') || char(10) || coalesce(json_extract(q.document,'$.content'),'') FROM object_version q WHERE q.object_id=o.in_reply_to ORDER BY q.id DESC LIMIT 1)` + `,'') || char(10) ||
	coalesce(` + `(SELECT json_extract(pa.document,'$.name') FROM object_version pa WHERE pa.object_id=(SELECT q.attributed_to FROM object_version q WHERE q.object_id=o.in_reply_to ORDER BY q.id DESC LIMIT 1) ORDER BY pa.id DESC LIMIT 1)` + `,'') || char(10) ||
	coalesce((SELECT coalesce(json_extract(q.document,'$.name'),'') || char(10) || coalesce(json_extract(q.document,'$.content'),'') || char(10) || coalesce((SELECT json_extract(pa.document,'$.name') FROM object_version pa WHERE pa.object_id=q.attributed_to ORDER BY pa.id DESC LIMIT 1),'') FROM object_version q WHERE q.object_id=o.quote_of ORDER BY q.id DESC LIMIT 1),'') || char(10) ||
	coalesce((WITH RECURSIVE thread(id, depth) AS (
			SELECT c.object_id, 1 FROM object_version c
				WHERE c.in_reply_to=a.object AND c.id=(SELECT max(id) FROM object_version WHERE object_id=c.object_id)
				AND ` + replyCounts + `
			UNION ALL
			SELECT c.object_id, t.depth+1 FROM object_version c JOIN thread t ON c.in_reply_to=t.id
				WHERE t.depth<` + maxSearchThread + ` AND c.id=(SELECT max(id) FROM object_version WHERE object_id=c.object_id)
				AND ` + replyCounts + `
		) SELECT group_concat(coalesce(json_extract(v.document,'$.content'),''), char(10)) FROM thread t
			JOIN object_version v ON v.object_id=t.id AND v.id=(SELECT max(id) FROM object_version WHERE object_id=t.id)),'')`

// replyCounts is the condition that a reply c is in its post's search text: the owner made it public and did not hide it.
const replyCounts = `EXISTS (SELECT 1 FROM activity ca JOIN activity_public cp ON cp.activity_id=ca.id
					WHERE ca.object=c.object_id AND ca.kind='Create' AND NOT EXISTS (SELECT 1 FROM activity_hidden ch WHERE ch.activity_id=ca.id))`

// maxSearchThread is how deep into the replies under a post the search reaches.
const maxSearchThread = "8"

// reindex rebuilds the rows of the activities that satisfy the condition.
func reindex(ctx context.Context, tx *sql.Tx, condition string, args ...any) error {
	where := ` WHERE ` + condition + activeClause
	if _, err := tx.ExecContext(ctx, `DELETE FROM entry_search WHERE rowid IN (SELECT a.id FROM activity a`+where+`)`, args...); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO entry_search (rowid, text)
		SELECT a.id, `+searchText+` FROM activity a
		LEFT JOIN object_version o ON o.id=(SELECT max(id) FROM object_version WHERE object_id=a.object)`+where, args...)
	return err
}

// ensureSearch creates the index and, when it is empty, fills it, in slices so that readers
// are not kept waiting for the whole archive.
func (s *Store) ensureSearch(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE VIRTUAL TABLE IF NOT EXISTS entry_search USING fts5(text, tokenize='trigram')`); err != nil {
		return err
	}
	var indexed, entries int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM entry_search`).Scan(&indexed); err != nil {
		return err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM activity a WHERE 1=1`+activeClause).Scan(&entries); err != nil {
		return err
	}
	if indexed >= entries {
		return nil
	}
	log.Printf("search: indexing %d entries", entries-indexed)
	const slice = 2000
	for from := int64(0); ; {
		var to sql.NullInt64
		if err := s.db.QueryRowContext(ctx, `SELECT max(id) FROM (SELECT id FROM activity WHERE id>? ORDER BY id LIMIT ?)`, from, slice).Scan(&to); err != nil {
			return err
		}
		if !to.Valid {
			return nil
		}
		err := s.tx(ctx, func(tx *sql.Tx) error {
			return reindex(ctx, tx, `a.id>? AND a.id<=?`, from, to.Int64)
		})
		if err != nil {
			return fmt.Errorf("indexing entries up to %d: %w", to.Int64, err)
		}
		from = to.Int64
	}
}

// refreshSearch brings the index up to date after an ingest that created something.
func (s *Store) refreshSearch(ctx context.Context, result Result, in *parsedInput) {
	if !result.Created {
		return
	}
	var err error
	switch {
	case result.Kind == "activity" && result.ActivityID > 0:
		err = s.tx(ctx, func(tx *sql.Tx) error { return reindex(ctx, tx, `a.id=?`, result.ActivityID) })
	case result.Kind == "object" && in.object != nil:
		o := in.object
		if in.typ == "Person" {
			// A name changed: the entries of everything this person wrote.
			err = s.tx(ctx, func(tx *sql.Tx) error {
				return reindex(ctx, tx, `a.object IN (SELECT object_id FROM object_version WHERE attributed_to=?)`, o.id)
			})
		} else {
			err = s.tx(ctx, func(tx *sql.Tx) error {
				// The entries about this object, about the posts it answers (it is one of their comments,
				// however deep in a thread), and the entries whose post this one answers.
				ancestors, err := threadAncestors(ctx, tx, o.inReplyTo)
				if err != nil {
					return err
				}
				condition := `(a.object=? OR a.object=? OR a.announce_id=? OR a.object IN (SELECT object_id FROM object_version WHERE in_reply_to=? OR quote_of=?)`
				args := []any{o.id, o.quote, o.id, o.id, o.id}
				for _, id := range ancestors {
					condition += ` OR a.object=?`
					args = append(args, id)
				}
				return reindex(ctx, tx, condition+`)`, args...)
			})
		}
	}
	if err != nil {
		log.Printf("search: refreshing the index: %v", err)
	}
}

// threadAncestors is the post a reply answers and what that answers in turn, up to the first
// that answers nothing or maxSearchThread levels: the objects whose search text holds the reply.
func threadAncestors(ctx context.Context, tx *sql.Tx, from string) ([]string, error) {
	var found []string
	for depth := 0; from != "" && depth < 8; depth++ {
		found = append(found, from)
		var next sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT in_reply_to FROM object_version WHERE object_id=? ORDER BY id DESC LIMIT 1`, from).Scan(&next)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return nil, err
		}
		from = next.String
	}
	return found, nil
}

// searchClause is the part of a timeline query that keeps the entries matching text; its
// argument follows. A pattern or word the index cannot take is an error for the caller to show.
func searchClause(text string, regex bool) (string, any, error) {
	switch {
	case regex:
		if err := ValidSearchPattern(text); err != nil {
			return "", nil, badf("invalid regular expression: %v", err)
		}
		return ` AND a.id IN (SELECT rowid FROM entry_search WHERE regexp(?, text))`, text, nil
	case utf8.RuneCountInString(text) >= 3:
		return ` AND a.id IN (SELECT rowid FROM entry_search WHERE entry_search MATCH ?)`, `"` + strings.ReplaceAll(text, `"`, `""`) + `"`, nil
	default:
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text)
		return ` AND a.id IN (SELECT rowid FROM entry_search WHERE text LIKE ? ESCAPE '\')`, "%" + escaped + "%", nil
	}
}
