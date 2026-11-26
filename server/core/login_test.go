package core

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// A database written before the owner's login existed gets its table when it is opened, and keeps the login it had.
func TestOpeningAVersionNineDatabaseAddsTheLoginTable(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "atrace.db")
	s, err := Open(dbPath, filepath.Join(dir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DROP TABLE owner_login`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA user_version = 9`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(dbPath, filepath.Join(dir, "files"))
	if err != nil {
		t.Fatalf("open a version nine database: %v", err)
	}
	defer s.Close()
	if _, err := s.OwnerLogin(context.Background()); err != sql.ErrNoRows {
		t.Fatalf("login after the upgrade = %v, want no login yet", err)
	}
	want := Login{Salt: []byte("salt"), Verifier: []byte("verifier"), Key: []byte("key")}
	if err := s.SetOwnerLogin(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := s.OwnerLogin(context.Background())
	if err != nil || string(got.Verifier) != "verifier" {
		t.Fatalf("login = %+v, %v", got, err)
	}
}
