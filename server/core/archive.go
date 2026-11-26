package core

import (
	"context"
	"database/sql"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

type ArchiveEntry struct {
	Ref, Person, Month string
}

// ArchiveEntries returns directory metadata without loading post bodies. Visitors see the default timeline.
func (s *Store) ArchiveEntries(ctx context.Context, publicOnly bool, ref string) ([]ArchiveEntry, error) {
	filter := ""
	var args []any
	if publicOnly {
		filter += personClause + " AND EXISTS (SELECT 1 FROM activity_public pub WHERE pub.activity_id=a.id)"
		args = append(args, 0)
	}
	if ref != "" {
		filter += " AND a.ref=?"
		args = append(args, ref)
	}
	rows, err := s.reads.QueryContext(ctx, `SELECT a.ref, coalesce(p.name, '未归类'),
		coalesce(strftime('%Y-%m', coalesce(`+timelineDate+`,o.observed_at), '+8 hours'),'未注明日期')
		FROM activity a JOIN observation o ON o.activity_id=a.id
		LEFT JOIN person_account pa ON pa.account=a.actor LEFT JOIN person p ON p.id=pa.person_id
		WHERE a.ref IS NOT NULL AND (a.kind='Announce' OR EXISTS (SELECT 1 FROM object_version v WHERE v.object_id=a.object))`+activeClause+hiddenClause+filter, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []ArchiveEntry
	for rows.Next() {
		var entry ArchiveEntry
		if err := rows.Scan(&entry.Ref, &entry.Person, &entry.Month); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

type ArchiveAttachment struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// ReplaceArchiveAttachments synchronizes the owner's text files for one post.
func (s *Store) ReplaceArchiveAttachments(ctx context.Context, object string, files []ArchiveAttachment) error {
	if len(files) > 128 {
		return badf("too many attachments")
	}
	seen := map[string]bool{}
	for _, file := range files {
		name := file.Path
		if name == "." || !strings.HasPrefix(name, "refs/") || path.Clean(name) != name ||
			strings.Contains(name, "\\") || strings.ContainsFunc(name, unicode.IsControl) || len(name) > 512 {
			return badf("invalid attachment path")
		}
		ext := strings.ToLower(path.Ext(name))
		if (ext != ".md" && ext != ".txt") || !utf8.ValidString(file.Content) || len(file.Content) > 512<<10 {
			return badf("expected a UTF-8 .md or .txt attachment of at most 512 KiB")
		}
		if seen[name] {
			return badf("duplicate attachment path")
		}
		seen[name] = true
	}
	for name := range seen {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if seen[parent] {
				return badf("attachment path is also a directory")
			}
		}
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		var exists bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM activity a
			WHERE a.object=? AND a.kind='Create'
			AND EXISTS (SELECT 1 FROM object_version v WHERE v.object_id=a.object)`+activeClause+hiddenClause+`)`, object).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return sql.ErrNoRows
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM archive_attachment WHERE object_id=?`, object); err != nil {
			return err
		}
		for _, file := range files {
			if _, err := tx.ExecContext(ctx, `INSERT INTO archive_attachment(object_id,path,content) VALUES(?,?,?)`, object, file.Path, file.Content); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) ArchiveAttachmentPaths(ctx context.Context, object string) ([]string, error) {
	rows, err := s.reads.QueryContext(ctx, `SELECT path FROM archive_attachment WHERE object_id=? ORDER BY path`, object)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		paths = append(paths, name)
	}
	return paths, rows.Err()
}

func (s *Store) ArchiveAttachmentText(ctx context.Context, object, name string) (string, error) {
	var text string
	err := s.reads.QueryRowContext(ctx, `SELECT content FROM archive_attachment WHERE object_id=? AND path=?`, object, name).Scan(&text)
	return text, err
}
