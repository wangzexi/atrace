package core

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"os"

	_ "modernc.org/sqlite"
)

//go:embed schema.sqlite.sql
var schemaSQL string

// Store holds the database and the file directory. Every write runs in one
// transaction on a single connection, so check-then-write is never racy.
type Store struct {
	db    *sql.DB
	reads *sql.DB
	files string
}

func Open(dbPath, filesDir string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=temp_store(MEMORY)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := upgrade(db); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`DROP VIEW IF EXISTS attachments; DROP VIEW IF EXISTS entries; DROP VIEW IF EXISTS latest_objects; DROP VIEW IF EXISTS people; DROP VIEW IF EXISTS accounts; DROP TABLE IF EXISTS object_ref`); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db, files: filesDir}
	if err := s.ensureSearch(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	// WAL lets read-only connections see committed data while the writer runs.
	s.reads, err = sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=temp_store(MEMORY)&_pragma=busy_timeout(5000)")
	if err != nil {
		db.Close()
		return nil, err
	}
	s.reads.SetMaxOpenConns(4)
	if err := s.reads.Ping(); err != nil {
		s.Close()
		return nil, err
	}
	if err := s.migrateCommentHides(context.Background()); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	var errs []error
	for _, db := range []*sql.DB{s.reads, s.db} {
		if db != nil {
			errs = append(errs, db.Close())
		}
	}
	return errors.Join(errs...)
}

func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// personClause keeps the activities of one person: those done by their accounts. Its argument is the person's ID,
// and 0 is the owner. Everything collected is stored the same way, whoever did it; a timeline is the view of one person.
const personClause = ` AND a.actor IN (SELECT account FROM person_account WHERE person_id=coalesce(nullif(?,0),(SELECT owner FROM site WHERE id=1)))`

// hiddenClause leaves out the entries the owner deleted.
const hiddenClause = ` AND NOT EXISTS (SELECT 1 FROM activity_hidden h WHERE h.activity_id=a.id)`

// migrateCommentHides moves hidden comments onto their entries (a comment is an entry now, hidden the same way) and
// drops the table that held them. Once it has run the table is gone and this does nothing. The search text of the posts
// counted those comments, so it is built again.
func (s *Store) migrateCommentHides(ctx context.Context) error {
	var tables int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='object_hidden'`).Scan(&tables); err != nil {
		return err
	}
	if tables == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO activity_hidden (activity_id, hidden_at)
			SELECT a.id, h.hidden_at FROM object_hidden h JOIN activity a ON a.object=h.object_id AND a.kind='Create'`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE object_hidden`); err != nil {
			return err
		}
		return reindex(ctx, tx, `a.id>0`)
	})
}
