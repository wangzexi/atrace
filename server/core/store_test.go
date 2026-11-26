package core

// Test helpers, and tests of opening and upgrading the store.

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testOwnerAccounts are the accounts the tests' own posts belong to; any other actor is someone else.
var testOwnerAccounts = []string{"weibo:user:1", "weibo:user:9", "x:user:9", "x:user:1", "xhs:user:1", "zhihu:user:1", "blog:user:z", "bilibili:user:1", "z:user:1", "nowhere:user:1"}

// openBare opens a store nobody has set up: no owner, no accounts.
func openBare(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "atrace.db"), filepath.Join(dir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// openTest opens a store whose owner has bound testOwnerAccounts.
func openTest(t *testing.T) *Store {
	t.Helper()
	s := openBare(t)
	makeOwner(t, s, "Alice", testOwnerAccounts)
	return s
}

// makeOwner makes a person with the accounts, and the owner that person. The accounts need not have been met yet, as
// the owner's are in tests that ingest their posts afterwards.
func makeOwner(t *testing.T, s *Store, name string, accounts []string) int64 {
	t.Helper()
	var id int64
	err := s.tx(context.Background(), func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO person (name) VALUES (?)`, name)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		if err := setAccounts(context.Background(), tx, id, accounts); err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO site (id,owner) VALUES (1,?) ON CONFLICT(id) DO UPDATE SET owner=excluded.owner`, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func input(document string) json.RawMessage {
	return json.RawMessage(`{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":` + document + `}`)
}

func ingest(t *testing.T, s *Store, document string) Result {
	t.Helper()
	r, err := s.Ingest(context.Background(), input(document))
	if err != nil {
		t.Fatalf("ingest %s: %v", document, err)
	}
	return r
}

func ingestErr(t *testing.T, s *Store, document, want string) {
	t.Helper()
	_, err := s.Ingest(context.Background(), input(document))
	if err == nil || !IsInputError(err) || !strings.Contains(err.Error(), want) {
		t.Fatalf("ingest %s: error %v, want input error containing %q", document, err, want)
	}
}

const note = `{"type":"Note","id":"weibo:post:1","attributedTo":"weibo:user:9","content":"hello","published":"2026-11-13T08:00:00+08:00"}`

// A database of the previous version, with something in every table the migration moves, keeps all of it.
func TestMigrationFromTheVersionBefore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atrace.db")
	old, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile("testdata/schema-v7.sql")
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{string(schema), `PRAGMA user_version = 7`,
		`INSERT INTO object (id,type) VALUES ('x:post:1','Note'),('x:user:1','Person'),('x:post:2','Note'),('x:collection:1','Collection')`,
		`INSERT INTO object_version (object_id,document,content_hash,attributed_to,published_at) VALUES ('x:post:1','{"type":"Note","id":"x:post:1","content":"hello"}','h1','x:user:1','2026-11-01T00:00:00Z')`,
		`INSERT INTO object_version (object_id,document,content_hash) VALUES ('x:post:1','{"type":"Note","id":"x:post:1","content":"hello again"}','h2')`,
		`INSERT INTO object_version (object_id,document,content_hash) VALUES ('x:user:1','{"type":"Person","id":"x:user:1","name":"Alice"}','h3')`,
		`INSERT INTO object_version (object_id,document,content_hash,attributed_to,published_at) VALUES ('x:post:2','{"type":"Note","id":"x:post:2","content":"someone else"}','h4','x:user:2','2026-11-03T00:00:00Z')`,
		`INSERT INTO object_version (object_id,document,content_hash,attributed_to) VALUES ('x:collection:1','{"type":"Collection","id":"x:collection:1","name":"c"}','h5','x:user:2')`,
		`INSERT INTO observation (version_id,observed_at,observed_by) VALUES (4,'2026-11-04T00:00:00Z','x-collector')`,
		`INSERT INTO object (id,type) VALUES ('x:highlight:1','Note')`,
		`INSERT INTO object_version (object_id,document,content_hash,attributed_to) VALUES ('x:highlight:1','{"type":"Note","id":"x:highlight:1","content":"underlined","quote":"x:post:2"}','h6','x:user:1')`,
		`INSERT INTO observation (version_id,observed_at,observed_by) VALUES (6,'2026-11-04T00:00:00Z','x-collector')`,
		`INSERT INTO activity (type,kind,actor,object,document) VALUES ('Create','Create','x:user:1','x:post:1','{"type":"Create"}')`,
		`INSERT INTO activity_ref (activity_id,ref) VALUES (1,'abcd1234')`,
		`INSERT INTO observation (version_id,observed_at,observed_by,raw) VALUES (1,'2026-11-02T00:00:00Z','x-collector','{"raw":1}')`,
		`INSERT INTO observation (activity_id,observed_at,observed_by) VALUES (1,'2026-11-02T00:00:00Z','x-collector')`,
		`INSERT INTO activity (type,kind,actor,object,target,document) VALUES ('Add','Add','x:user:1','x:highlight:1','x:collection:1','{"type":"Add"}')`,
		`INSERT INTO activity_ref (activity_id,ref) VALUES (2,'efgh5678')`,
		`INSERT INTO observation (activity_id,observed_at,observed_by) VALUES (2,'2026-11-04T00:00:00Z','x-collector')`,
		`INSERT INTO media (url,file_ref,mime,size,stored_at) VALUES ('https://img/a.jpg','sha256:aa','image/jpeg',10,'2026-11-02T00:00:00Z')`,
		`INSERT INTO collector_run (plugin,mode,started_at,status) VALUES ('x','incremental','2026-11-02T00:00:00Z','ok')`,
		`INSERT INTO collector_state (plugin,paused,next_run_at,failures) VALUES ('x',1,'2026-11-03T00:00:00Z',2)`,
		`INSERT INTO collector_interval VALUES ('weibo',3600)`,
		`INSERT INTO collector_cron VALUES ('x','0 */6 * * *')`,
		`INSERT INTO collector_probe VALUES ('x','ok','2026-11-02T00:00:00Z')`,
		`INSERT INTO collector_account_seen VALUES ('x','x:user:1','Alice X','2026-11-02T00:00:00Z')`,
		`INSERT INTO site_settings VALUES (1,'My notes')`,
		`INSERT INTO owner_profile VALUES (1,'Alice','["x:user:1"]')`,
	}
	for _, statement := range statements {
		if _, err := old.Exec(statement); err != nil {
			t.Fatalf("%v in %.80s", err, statement)
		}
	}
	old.Close()

	s, err := Open(path, filepath.Join(dir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	count := func(query string, want int) {
		t.Helper()
		var n int
		if err := s.db.QueryRow(query).Scan(&n); err != nil || n != want {
			t.Errorf("%s = %d (%v), want %d", query, n, err, want)
		}
	}
	count(`SELECT count(*) FROM object_version`, 6)
	count(`SELECT count(*) FROM object_version WHERE type='Note'`, 4)
	count(`SELECT count(*) FROM observation WHERE version_id=1 AND raw='{"raw":1}'`, 1)
	count(`SELECT count(*) FROM activity WHERE ref='abcd1234'`, 1)
	count(`SELECT count(*) FROM media WHERE file_ref='sha256:aa' AND mime='image/jpeg'`, 1)
	count(`SELECT count(*) FROM site s JOIN person p ON p.id=s.owner JOIN person_account a ON a.person_id=p.id WHERE s.title='My notes' AND p.name='Alice' AND a.account='x:user:1'`, 1)
	// Someone else's post gets the Create a collector would write now, observed by the migration at the time the post was first seen.
	count(`SELECT count(*) FROM activity a JOIN observation o ON o.activity_id=a.id WHERE a.kind='Create' AND a.actor='x:user:2' AND a.object='x:post:2' AND a.published_at='2026-11-03T00:00:00Z' AND length(a.ref)=8 AND o.observed_by='migration' AND o.observed_at='2026-11-04T00:00:00Z' AND json_extract(a.document,'$.published')='2026-11-03T00:00:00Z'`, 1)
	count(`SELECT count(*) FROM activity WHERE object='x:post:1' AND kind='Create'`, 1)
	count(`SELECT count(*) FROM activity WHERE object IN ('x:collection:1','x:user:1')`, 0)
	// A note its own author kept is not something they published.
	count(`SELECT count(*) FROM activity WHERE object='x:highlight:1' AND kind='Create'`, 0)
	count(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('object','activity_ref','collector_run','collector_task','collector','collector_state','collector_interval','collector_cron','collector_probe','collector_account_seen','site_settings','owner_profile')`, 0)
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("user_version = %d, %v; want %d", version, err, schemaVersion)
	}
	var foreignKeys int
	if err := s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("foreign keys after the migration = %d, %v", foreignKeys, err)
	}
	// The migrated store is the new one's twin: it reads, and it takes new records, observations included.
	if name, err := s.OwnerNameForAccount(context.Background(), "x:user:1"); err != nil || name != "Alice" {
		t.Fatalf("owner name = %q, %v", name, err)
	}
	if rec, err := s.Object(context.Background(), "x:post:1", false); err != nil || rec.Type != "Note" || !strings.Contains(string(rec.Document), "hello") {
		t.Fatalf("object = %+v, %v", rec, err)
	}
	if r := ingest(t, s, `{"type":"Note","id":"x:post:2","content":"new"}`); !r.Created {
		t.Fatal("a new object after the migration was not stored")
	}
	if r := ingest(t, s, `{"type":"Like","actor":"x:user:1","object":"x:post:2"}`); !r.Created {
		t.Fatal("a new activity after the migration was not stored")
	}
	count(`SELECT count(*) FROM activity WHERE ref IS NULL`, 0)
	s.Close()
	// Opening it again runs no migration.
	if s, err = Open(path, filepath.Join(dir, "files")); err != nil {
		t.Fatal(err)
	}
}

// A database that is neither of this version nor the one before is refused, not guessed at.
func TestOlderDatabasesAreRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atrace.db")
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`CREATE TABLE activity (id INTEGER)`, `PRAGMA user_version = 3`} {
		if _, err := old.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()
	if _, err := Open(path, filepath.Join(dir, "files")); err == nil || !strings.Contains(err.Error(), "schema version 3") {
		t.Fatalf("open = %v", err)
	}
}

// Every ref is refLength characters of the alphabet.

// The homepage must be able to read committed data even while a write holds the writer connection.
func TestTimelineReadsDuringWrite(t *testing.T) {
	s := openTest(t)
	if err := s.SetSiteTitle(context.Background(), "Committed"); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE site SET title='Uncommitted' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := s.TimelinePage(ctx, 0, []string{"Create"}, 20, false, "", false, PageRange{}); err != nil {
		t.Fatal(err)
	}
	if title, err := s.SiteTitle(ctx, "Fallback"); err != nil || title != "Committed" {
		t.Fatalf("title=%q, err=%v", title, err)
	}
}

func TestPlatformRenamePreservesEntryAndVersionHistory(t *testing.T) {
	s := openBare(t)
	makeOwner(t, s, "Owner", []string{"blog:user:owner"})
	for _, document := range []string{
		`{"type":"Article","id":"blog:article:1","attributedTo":"blog:user:owner","content":"first"}`,
		`{"type":"Article","id":"blog:article:1","attributedTo":"blog:user:owner","content":"xhs: is literal text","quote":"xhs:note:1"}`,
		`{"type":"Create","actor":"blog:user:owner","object":"blog:article:1"}`,
	} {
		ingest(t, s, document)
	}
	if _, err := s.db.Exec(`INSERT INTO activity_public SELECT id,'now' FROM activity`); err != nil {
		t.Fatal(err)
	}
	var ref string
	if err := s.db.QueryRow(`SELECT ref FROM activity`).Scan(&ref); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE observation SET raw = '{"original":"blog:article:1"}'`); err != nil {
		t.Fatal(err)
	}
	if err := renamePlatforms(s.db); err != nil {
		t.Fatal(err)
	}
	if err := s.ensureSearch(context.Background()); err != nil {
		t.Fatal(err)
	}

	assertCount := func(query string, expected int, args ...any) {
		t.Helper()
		var got int
		if err := s.db.QueryRow(query, args...).Scan(&got); err != nil || got != expected {
			t.Fatalf("count = %d, %v; want %d: %s", got, err, expected, query)
		}
	}
	assertCount(`SELECT count(*) FROM object_version WHERE object_id='github:article:1'`, 2)
	assertCount(`SELECT count(*) FROM object_version WHERE quote_of='xiaohongshu:note:1' AND json_extract(document,'$.content')='xhs: is literal text'`, 1)
	assertCount(`SELECT count(*) FROM activity a JOIN activity_public p ON p.activity_id=a.id WHERE a.ref=? AND a.object='github:article:1' AND a.actor='github:user:owner'`, 1, ref)
	assertCount(`SELECT count(*) FROM person_account WHERE account='github:user:owner'`, 1)
	assertCount(`SELECT count(*) FROM observation WHERE json_extract(raw,'$.original')='blog:article:1'`, 3)
	ingest(t, s, `{"type":"Article","id":"github:article:1","attributedTo":"github:user:owner","content":"xhs: is literal text","quote":"xiaohongshu:note:1"}`)
	assertCount(`SELECT count(*) FROM object_version WHERE object_id='github:article:1'`, 2)
}

// An existing archive must gain the ordering table without changing its people or default.
func TestUpgradeAddsPersonOrdering(t *testing.T) {
	dir := t.TempDir()
	database, files := filepath.Join(dir, "archive.db"), filepath.Join(dir, "files")
	s, err := Open(database, files)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id, err := s.CreatePerson(ctx, "Existing person", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetOwner(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DROP TABLE person_position; PRAGMA user_version=10"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(database, files)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	people, err := s.People(ctx)
	if err != nil || len(people) != 1 || people[0].ID != id || !people[0].Owner {
		t.Fatalf("people after upgrade = %+v, %v", people, err)
	}
	if err := s.OrderPeople(ctx, []int64{id}); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("version = %d, %v", version, err)
	}
}
