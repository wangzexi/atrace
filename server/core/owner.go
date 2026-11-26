package core

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"
)

// The owner's edits: hiding, publishing and aliases.

// HideActivity soft-deletes one timeline entry: readers no longer return it.
// Only a post flagged as deleted on its platform, or a like or favorite flagged as taken
// back there, can be deleted here; one that is still there is kept, and made private if it should not be shown.
// Hiding one that is already hidden changes nothing.
func (s *Store) hideActivity(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var kind, object string
		var announceID sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT kind, object, announce_id FROM activity WHERE id=?`, id).Scan(&kind, &object, &announceID); err != nil {
			return err
		}
		var flagged int
		if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM object_gone WHERE object_id=?) + (SELECT count(*) FROM activity_dropped WHERE activity_id=?)`,
			flagKey(kind, object, announceID), id).Scan(&flagged); err != nil {
			return err
		}
		if flagged == 0 {
			return badf("only a post confirmed deleted on its platform, or a like or favorite taken back there, can be deleted")
		}
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO activity_hidden (activity_id, hidden_at) VALUES (?,?)`, id, formatTime(time.Now()))
		return err
	})
}

// SetPublic makes a timeline entry public or private again. Entries are private
// until made public; a hidden entry cannot be made public.
func (s *Store) setPublic(ctx context.Context, id int64, public bool) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if !public {
			_, err := tx.ExecContext(ctx, `DELETE FROM activity_public WHERE activity_id=?`, id)
			return err
		}
		var found int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM activity a WHERE a.id=?`+hiddenClause, id).Scan(&found); err != nil {
			return err
		}
		if found == 0 {
			return sql.ErrNoRows
		}
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO activity_public (activity_id, public_at) VALUES (?,?)`, id, formatTime(time.Now()))
		return err
	})
}

// An alias is 3 to 40 lower-case letters, digits and hyphens, not starting or ending with a
// hyphen. Eight letters or digits are left to the generated refs, so an alias never reads as one.
var aliasPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)
var refShaped = regexp.MustCompile(`^[a-z0-9]{8}$`)

// reservedAliases are one-segment paths the server answers itself.
var reservedAliases = map[string]bool{
	"api": true, "dev": true, "files": true, "login": true, "logout": true, "assets": true,
	"admin": true, "terminal": true, "settings": true, "collect": true, "healthz": true, "status": true, "index": true,
}

// SetAlias gives a timeline entry a readable address, or takes it away when alias is empty.
func (s *Store) SetAlias(ctx context.Context, id int64, alias string) error {
	if alias == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM activity_alias WHERE activity_id=?`, id)
		return err
	}
	if !aliasPattern.MatchString(alias) || refShaped.MatchString(alias) || reservedAliases[alias] {
		return badf("alias must be 3-40 lower-case letters, digits or hyphens (not 8 letters or digits, not a name the server uses)")
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		var found int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM activity a WHERE a.id=?`+hiddenClause, id).Scan(&found); err != nil {
			return err
		}
		if found == 0 {
			return sql.ErrNoRows
		}
		var owner int64
		switch err := tx.QueryRowContext(ctx, `SELECT activity_id FROM activity_alias WHERE alias=?`, alias).Scan(&owner); {
		case err == nil && owner != id:
			return badf("alias is already used by another entry")
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO activity_alias (activity_id, alias) VALUES (?,?) ON CONFLICT (activity_id) DO UPDATE SET alias=excluded.alias`, id, alias)
		return err
	})
}

// RestoreActivity undoes HideActivity.
func (s *Store) restoreActivity(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM activity_hidden WHERE activity_id=?`, id)
	return err
}

// flagKey is what the flags of a post are kept under: a repost under the repost's own ID, anything else under the
// post's.
func flagKey(kind, object string, announceID sql.NullString) string {
	if kind == "Announce" && announceID.Valid && announceID.String != "" {
		return announceID.String
	}
	return object
}

// ActivityByRef is the activity an entry's ref names. sql.ErrNoRows when no entry has it.
func (s *Store) ActivityByRef(ctx context.Context, ref string) (int64, error) {
	var id int64
	err := s.reads.QueryRowContext(ctx, `SELECT id FROM activity WHERE ref=?`, ref).Scan(&id)
	return id, err
}

// reindexThreadOfActivity refreshes the search text of the posts an entry answers: a reply counts in them only while
// it is public and not hidden, so its own change to either is seen there.
func (s *Store) reindexThreadOfActivity(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var parent sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT v.in_reply_to FROM activity a JOIN object_version v ON v.object_id=a.object
			WHERE a.id=? AND v.id=(SELECT max(id) FROM object_version WHERE object_id=a.object)`, id).Scan(&parent)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		ancestors, err := threadAncestors(ctx, tx, parent.String)
		if err != nil || len(ancestors) == 0 {
			return err
		}
		condition := `a.object=?`
		args := []any{ancestors[0]}
		for _, other := range ancestors[1:] {
			condition += ` OR a.object=?`
			args = append(args, other)
		}
		return reindex(ctx, tx, condition, args...)
	})
}

// HideActivity, SetPublic and RestoreActivity change what the search finds, so they refresh the posts a reply answers.
func (s *Store) HideActivity(ctx context.Context, id int64) error {
	if err := s.hideActivity(ctx, id); err != nil {
		return err
	}
	return s.reindexThreadOfActivity(ctx, id)
}

func (s *Store) SetPublic(ctx context.Context, id int64, public bool) error {
	if err := s.setPublic(ctx, id, public); err != nil {
		return err
	}
	return s.reindexThreadOfActivity(ctx, id)
}

func (s *Store) RestoreActivity(ctx context.Context, id int64) error {
	if err := s.restoreActivity(ctx, id); err != nil {
		return err
	}
	return s.reindexThreadOfActivity(ctx, id)
}
