package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// What the page and the API read.

// ObjectRecord is the latest observed state of a content object.
type ObjectRecord struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	VersionID  int64           `json:"version_id"`
	ObservedAt string          `json:"observed_at"`
	ObservedBy string          `json:"observed_by,omitempty"`
	Document   json.RawMessage `json:"document"`
	Raw        json.RawMessage `json:"raw,omitempty"`
	// Gone is set on a reply flagged as deleted on its platform.
	Gone bool `json:"gone,omitempty"`
}

// Object returns the latest version of an object. Raw is returned only when
// asked for, since it can be large.
func (s *Store) Object(ctx context.Context, id string, includeRaw bool) (ObjectRecord, error) {
	var rec ObjectRecord
	var doc string
	var raw sql.NullString
	err := s.reads.QueryRowContext(ctx, `SELECT v.object_id,v.type,v.id,ob.observed_at,ob.observed_by,v.document,ob.raw
		FROM object_version v JOIN observation ob ON ob.version_id=v.id
		WHERE v.object_id=? ORDER BY v.id DESC LIMIT 1`, id).Scan(&rec.ID, &rec.Type, &rec.VersionID, &rec.ObservedAt, &rec.ObservedBy, &doc, &raw)
	if err != nil {
		return ObjectRecord{}, err
	}
	rec.Document = json.RawMessage(doc)
	if includeRaw && raw.Valid {
		rec.Raw = json.RawMessage(raw.String)
	}
	return rec, nil
}

// LatestRaw is the newest raw response any observation of the object kept; empty when none did.
// It is not necessarily that of the latest version: a version stored without one (a repair of
// the document) leaves the raw response of an earlier version the newest there is.
func (s *Store) LatestRaw(ctx context.Context, id string) (json.RawMessage, error) {
	var raw sql.NullString
	err := s.reads.QueryRowContext(ctx, `SELECT ob.raw FROM observation ob JOIN object_version v ON v.id=ob.version_id
		WHERE v.object_id=? AND ob.raw IS NOT NULL ORDER BY ob.id DESC LIMIT 1`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw.String), nil
}

// EntryState is the owner's own entry for an object: its activity, its ref, and whether the owner made it public and
// whether it is hidden. A reply is not public because the post it answers is; it has its own entry.
type EntryState struct {
	ActivityID int64
	Ref        string
	Public     bool
	Hidden     bool
}

// ObjectEntry is the owner's entry for an object (the Create of a post or a reply). The zero EntryState when there is none.
func (s *Store) ObjectEntry(ctx context.Context, id string) (EntryState, error) {
	var e EntryState
	err := s.reads.QueryRowContext(ctx, `SELECT a.id, coalesce(a.ref,''),
		EXISTS (SELECT 1 FROM activity_public p WHERE p.activity_id=a.id),
		EXISTS (SELECT 1 FROM activity_hidden h WHERE h.activity_id=a.id)
		FROM activity a WHERE a.object=? AND a.kind='Create' ORDER BY a.id DESC LIMIT 1`, id).Scan(&e.ActivityID, &e.Ref, &e.Public, &e.Hidden)
	if errors.Is(err, sql.ErrNoRows) {
		return EntryState{}, nil
	}
	return e, err
}

// Replies returns the latest version of every object that replies to id,
// oldest first. An object counts only while its latest version still replies
// to id.
func (s *Store) Replies(ctx context.Context, id string) ([]ObjectRecord, error) {
	rows, err := s.reads.QueryContext(ctx, `SELECT v.object_id,v.type,v.id,ob.observed_at,ob.observed_by,v.document,
		EXISTS (SELECT 1 FROM object_gone g WHERE g.object_id=v.object_id)
		FROM object_version v JOIN observation ob ON ob.version_id=v.id
		WHERE v.in_reply_to=? AND v.id=(SELECT max(l.id) FROM object_version l WHERE l.object_id=v.object_id)
		AND NOT EXISTS (SELECT 1 FROM activity a JOIN activity_hidden h ON h.activity_id=a.id WHERE a.object=v.object_id AND a.kind='Create')
		ORDER BY coalesce(v.published_at,''), v.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	recs := []ObjectRecord{}
	for rows.Next() {
		var rec ObjectRecord
		var doc string
		if err := rows.Scan(&rec.ID, &rec.Type, &rec.VersionID, &rec.ObservedAt, &rec.ObservedBy, &doc, &rec.Gone); err != nil {
			return nil, err
		}
		rec.Document = json.RawMessage(doc)
		recs = append(recs, rec)
	}
	return recs, rows.Err()
}

// Thread is every reply under a post, however deep in the threads, by object ID.
func (s *Store) Thread(ctx context.Context, post string) ([]string, error) {
	rows, err := s.reads.QueryContext(ctx, `WITH RECURSIVE thread(id, depth) AS (
		SELECT v.object_id, 1 FROM object_version v WHERE v.in_reply_to=? AND v.id=(SELECT max(id) FROM object_version WHERE object_id=v.object_id)
		UNION
		SELECT v.object_id, t.depth+1 FROM object_version v JOIN thread t ON v.in_reply_to=t.id
			WHERE t.depth<`+maxSearchThread+` AND v.id=(SELECT max(id) FROM object_version WHERE object_id=v.object_id)
	) SELECT DISTINCT id FROM thread`, post)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ActivityRecord is one stored activity.
type ActivityRecord struct {
	Ref         string          `json:"ref,omitempty"`
	ID          int64           `json:"id"`
	Type        string          `json:"type"`
	Kind        string          `json:"kind"`
	Actor       string          `json:"actor"`
	Object      string          `json:"object"`
	Target      string          `json:"target,omitempty"`
	PublishedAt string          `json:"published_at,omitempty"`
	ObservedAt  string          `json:"observed_at"`
	ObservedBy  string          `json:"observed_by,omitempty"`
	Document    json.RawMessage `json:"document"`
}

const activeClause = ` AND a.type IN ('Create','Like','Announce','Add') AND NOT EXISTS (
	SELECT 1 FROM activity n WHERE n.actor=a.actor AND n.kind=a.kind AND n.object=a.object
	AND coalesce(n.target,'')=coalesce(a.target,'') AND n.id>a.id AND n.type IN ('Undo','Remove')
	AND (a.kind<>'Announce' OR coalesce(n.announce_id,'')=coalesce(a.announce_id,'')))`

// Item is one entry of the timeline: an activity with the latest state of the
// object it points at, when that object has been collected.
type Item struct {
	Activity ActivityRecord  `json:"activity"`
	Object   json.RawMessage `json:"object,omitempty"`
	// Ref is the entry's address outside the store (activity.ref).
	Ref string `json:"ref"`
	// Gone is set when the post is flagged as deleted on its platform.
	Gone bool `json:"gone,omitempty"`
	// Dropped is set when the like or favorite is flagged as taken back on its platform.
	Dropped bool `json:"dropped,omitempty"`
	// Public is set when the owner made this entry public; the default is private.
	Public bool `json:"public,omitempty"`
	// Alias is the readable address the owner gave the entry, if any.
	Alias string `json:"alias,omitempty"`
}

// TimelineItem returns the entry with the given ref (or alias) as the timeline shows it,
// or sql.ErrNoRows when there is none or it is hidden.
func (s *Store) TimelineItem(ctx context.Context, ref string, publicOnly bool) (Item, error) {
	items, err := s.timeline(ctx, 0, []string{"Create", "Announce", "Reply", "Like", "Add"}, 1, ref, publicOnly, "", false, PageRange{})
	if err != nil {
		return Item{}, err
	}
	if len(items) == 0 {
		return Item{}, sql.ErrNoRows
	}
	return items[0], nil
}

// objectPublished is when the post an activity points at was published, as its latest version says.
const objectPublished = `(SELECT v.published_at FROM object_version v WHERE v.object_id=a.object ORDER BY v.id DESC LIMIT 1)`

// timelineKinds is the SQL condition for the wanted kinds, with its arguments.
func timelineKinds(kinds []string) (string, []any) {
	if len(kinds) == 0 {
		kinds = []string{"Create", "Announce", "Reply"}
	}
	// The kinds of the timeline are what the owner did: Create a post of their own, Announce a
	// repost, Reply a post of their own that has an inReplyTo, Like, Add a favorite. A quote is part
	// of a post, not a kind of its own: a post that quotes another is a Create (or a Reply).
	const reply = `(a.kind='Create' AND coalesce((SELECT v.in_reply_to FROM object_version v WHERE v.object_id=a.object ORDER BY v.id DESC LIMIT 1),'')<>'')`
	var wanted []string
	args := make([]any, 0, len(kinds))
	for _, kind := range kinds {
		switch kind {
		case "Reply":
			wanted = append(wanted, reply)
		case "Create":
			wanted = append(wanted, "(a.kind=? AND NOT "+reply+")")
			args = append(args, kind)
		default:
			wanted = append(wanted, "a.kind=?")
			args = append(args, kind)
		}
	}
	return strings.Join(wanted, " OR "), args
}

// timeline is Timeline, optionally narrowed to the entry with the given ref.
func (s *Store) timeline(ctx context.Context, person int64, kinds []string, limit int, onlyRef string, publicOnly bool, text string, regex bool, page PageRange) ([]Item, error) {
	wanted, args := timelineKinds(kinds)
	// Whose timeline it is is for a list; one entry is found by its own ref, whoever's it is.
	whose := ""
	if onlyRef == "" {
		whose = personClause
		args = append(args, person)
	}
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	only := ""
	if onlyRef != "" {
		only = " AND a.id=(SELECT id FROM activity WHERE ref=? UNION ALL SELECT activity_id FROM activity_alias WHERE alias=? LIMIT 1)"
		args = append(args, onlyRef, onlyRef)
	}
	if publicOnly {
		only += " AND EXISTS (SELECT 1 FROM activity_public pp WHERE pp.activity_id=a.id)"
	}
	if text = strings.TrimSpace(text); text != "" {
		clause, arg, err := searchClause(text, regex)
		if err != nil {
			return nil, err
		}
		only += clause
		args = append(args, arg)
	}
	clause, values, err := s.pageClause(ctx, page, publicOnly, timelineDate)
	if err != nil {
		return nil, err
	}
	only += clause
	args = append(args, values...)

	args = append(args, limit)
	rows, err := s.reads.QueryContext(ctx, `SELECT a.id,a.type,a.kind,a.actor,a.object,coalesce(a.target,''),coalesce(a.published_at,''),o.observed_at,o.observed_by,a.document,
		(SELECT v.document FROM object_version v WHERE v.object_id=a.object ORDER BY v.id DESC LIMIT 1),
		EXISTS (SELECT 1 FROM object_gone g WHERE g.object_id=CASE WHEN a.kind='Announce' THEN coalesce(a.announce_id,a.object) ELSE a.object END),
		EXISTS (SELECT 1 FROM activity_dropped d WHERE d.activity_id=a.id),
		EXISTS (SELECT 1 FROM activity_public p WHERE p.activity_id=a.id),
		coalesce(a.ref,''),
		coalesce((SELECT al.alias FROM activity_alias al WHERE al.activity_id=a.id),'')
		FROM activity a JOIN observation o ON o.activity_id=a.id WHERE (`+wanted+`)`+activeClause+hiddenClause+whose+only+`
		ORDER BY `+pageOrder(timelineDate)+` LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

func scanItems(rows *sql.Rows) ([]Item, error) {
	items := []Item{}
	for rows.Next() {
		var it Item
		var doc string
		var object sql.NullString
		a := &it.Activity
		if err := rows.Scan(&a.ID, &a.Type, &a.Kind, &a.Actor, &a.Object, &a.Target, &a.PublishedAt, &a.ObservedAt, &a.ObservedBy, &doc, &object, &it.Gone, &it.Dropped, &it.Public, &it.Ref, &it.Alias); err != nil {
			return nil, err
		}
		a.Document = json.RawMessage(doc)
		if object.Valid {
			it.Object = json.RawMessage(object.String)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}
