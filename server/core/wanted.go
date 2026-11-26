package core

import (
	"context"
	"database/sql"
)

// PostOfEntry is the post a timeline entry is about, by the entry's ref: what a device is asked to read again when
// the owner opens the entry. A repost is read under the repost's own ID, as its flags are.
func (s *Store) PostOfEntry(ctx context.Context, ref string) (string, error) {
	var kind, object string
	var announceID sql.NullString
	if err := s.reads.QueryRowContext(ctx, `SELECT kind, object, announce_id FROM activity WHERE ref=?`, ref).Scan(&kind, &object, &announceID); err != nil {
		return "", err
	}
	return flagKey(kind, object, announceID), nil
}
