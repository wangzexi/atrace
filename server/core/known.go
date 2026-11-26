package core

import (
	"context"
	"database/sql"
	"errors"
)

// KnownQuery asks whether an activity is already stored: the same one the collector is about to ingest.
// A repost is asked by its own ID; the others by who did what to which post (and into which collection,
// for a favorite).
type KnownQuery struct {
	Actor, Kind, Object, Target string
	ID                          string
}

// Known answers, for each query, whether ingesting that activity again would change nothing. A collector
// that reads a list newest first uses it to stop where it meets what it already has.
func (s *Store) Known(ctx context.Context, queries []KnownQuery) ([]bool, error) {
	known := make([]bool, len(queries))
	for i, q := range queries {
		var typ string
		var err error
		if q.Kind == "Announce" && q.ID != "" {
			// As ingestActivity: an identifiable repost that is stored stays as it is, even after an Undo.
			err = s.reads.QueryRowContext(ctx, `SELECT type FROM activity WHERE type='Announce' AND announce_id=? ORDER BY id DESC LIMIT 1`, q.ID).Scan(&typ)
			known[i] = err == nil
		} else {
			err = s.reads.QueryRowContext(ctx, `SELECT type FROM activity WHERE actor=? AND kind=? AND object=? AND coalesce(target,'')=? ORDER BY id DESC LIMIT 1`,
				q.Actor, q.Kind, q.Object, q.Target).Scan(&typ)
			known[i] = err == nil && positiveActivities[typ]
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	return known, nil
}
