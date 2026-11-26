package core

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
)

// Ingest and the refs: what collectors write.

// Result tells the caller what a submission did. Created is false when the
// submission changed nothing.
type Result struct {
	Kind       string `json:"kind"` // "object" or "activity"
	ID         string `json:"id,omitempty"`
	VersionID  int64  `json:"version_id,omitempty"`
	ActivityID int64  `json:"activity_id,omitempty"`
	Created    bool   `json:"created"`
}

// Ingest stores one ActivityStreams document with the observation that
// produced it. A content object whose document equals its latest version, and
// an activity that would not change the current state, are not stored again.
func (s *Store) Ingest(ctx context.Context, raw json.RawMessage) (Result, error) {
	in, err := parseInput(raw)
	if err != nil {
		return Result{}, err
	}
	var result Result
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if in.object != nil {
			result, err = ingestObject(ctx, tx, in)
		} else {
			result, err = ingestActivity(ctx, tx, in)
		}
		return err
	})
	if err == nil {
		s.refreshSearch(ctx, result, in)
	}
	return result, err
}

func ingestObject(ctx context.Context, tx *sql.Tx, in *parsedInput) (Result, error) {
	o := in.object
	var latestID int64
	var latestHash, latestType string
	err := tx.QueryRowContext(ctx, `SELECT id,content_hash,type FROM object_version WHERE object_id=? ORDER BY id DESC LIMIT 1`, o.id).Scan(&latestID, &latestHash, &latestType)
	if err == nil && latestType != in.typ {
		return Result{}, badf("object %s is a %s, not a %s", o.id, latestType, in.typ)
	}
	if err == nil && latestHash == in.hash {
		return Result{Kind: "object", ID: o.id, VersionID: latestID}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO object_version (object_id,document,content_hash,attributed_to,in_reply_to,quote_of,published_at,updated_at) VALUES (?,?,?,NULLIF(?,''),NULLIF(?,''),NULLIF(?,''),NULLIF(?,''),NULLIF(?,''))`,
		o.id, string(in.canonical), in.hash, o.attributedTo, o.inReplyTo, o.quote, o.publishedAt, o.updatedAt)
	if err != nil {
		return Result{}, err
	}
	versionID, err := r.LastInsertId()
	if err != nil {
		return Result{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO observation (version_id,observed_at,observed_by,raw) VALUES (?,?,?,NULLIF(?,''))`,
		versionID, formatTime(in.observedAt), in.observedBy, in.raw); err != nil {
		return Result{}, err
	}
	return Result{Kind: "object", ID: o.id, VersionID: versionID, Created: true}, nil
}

func ingestActivity(ctx context.Context, tx *sql.Tx, in *parsedInput) (Result, error) {
	a := in.activity
	positive := positiveActivities[in.typ]
	var latestID int64
	var latestType string
	query := `SELECT id,type FROM activity WHERE actor=? AND kind=? AND object=? AND coalesce(target,'')=?`
	args := []any{a.actor, a.kind, a.object, a.target}
	if in.typ == "Announce" && a.announceID != "" {
		// A replay of an identifiable Announce must not restore it after an Undo.
		query = `SELECT id,type FROM activity WHERE type='Announce' AND announce_id=?`
		args = []any{a.announceID}
	} else if a.kind == "Announce" {
		query += ` AND coalesce(announce_id,'')=?`
		args = append(args, a.announceID)
	}
	err := tx.QueryRowContext(ctx, query+` ORDER BY id DESC LIMIT 1`, args...).Scan(&latestID, &latestType)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	active := err == nil && positiveActivities[latestType]
	if active == positive {
		return Result{Kind: "activity"}, nil
	}
	ref, err := newRef(ctx, tx)
	if err != nil {
		return Result{}, err
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO activity (type,kind,actor,object,target,published_at,announce_id,document,ref) VALUES (?,?,?,?,NULLIF(?,''),NULLIF(?,''),NULLIF(?,''),?,?)`,
		in.typ, a.kind, a.actor, a.object, a.target, a.publishedAt, a.announceID, string(in.canonical), ref)
	if err != nil {
		return Result{}, err
	}
	activityID, err := r.LastInsertId()
	if err != nil {
		return Result{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO observation (activity_id,observed_at,observed_by,raw) VALUES (?,?,?,NULLIF(?,''))`,
		activityID, formatTime(in.observedAt), in.observedBy, in.raw); err != nil {
		return Result{}, err
	}
	return Result{Kind: "activity", ActivityID: activityID, Created: true}, nil
}

// refAlphabet is letters and digits, and a ref is case-sensitive, as a YouTube video ID is.
const refAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// refLength is the length of every ref: 62^8 names, too many to find one by guessing.
const refLength = 8

func randomRef(length int) (string, error) {
	ref := make([]byte, 0, length)
	var b [16]byte
	for len(ref) < length {
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		for _, c := range b {
			// 62 does not divide 256: bytes past the last multiple would favour the first names.
			if c < 248 && len(ref) < length {
				ref = append(ref, refAlphabet[c%62])
			}
		}
	}
	return string(ref), nil
}

// newRef draws a random ref no activity has, so that nobody can reach the entries around one by counting.
// A collision, which is all but impossible, draws again.
func newRef(ctx context.Context, tx *sql.Tx) (string, error) {
	for {
		ref, err := randomRef(refLength)
		if err != nil {
			return "", err
		}
		var taken bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM activity WHERE ref=?)`, ref).Scan(&taken); err != nil || !taken {
			return ref, err
		}
	}
}
