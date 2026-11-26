package core

// Tests of what collectors write.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestObjectTypeCannotChange(t *testing.T) {
	s := openTest(t)
	ingest(t, s, note)
	ingestErr(t, s, `{"type":"Article","id":"weibo:post:1","content":"x"}`, "is a Note")
}

func TestLikeState(t *testing.T) {
	s := openTest(t)
	like := `{"type":"Like","actor":"weibo:user:9","object":"weibo:post:2"}`
	undo := `{"type":"Undo","actor":"weibo:user:9","object":` + like + `}`
	if r := ingest(t, s, undo); r.Created {
		t.Fatal("undo without a like changes nothing")
	}
	if r := ingest(t, s, like); !r.Created {
		t.Fatal("first like should be stored")
	}
	if r := ingest(t, s, like); r.Created {
		t.Fatal("repeated like changes nothing")
	}
	if n := countRows(t, s, `SELECT count(*) FROM activity a WHERE a.kind='Like'`+activeClause); n != 1 {
		t.Fatalf("active likes = %d, want 1", n)
	}
	if r := ingest(t, s, undo); !r.Created {
		t.Fatal("undo of an active like should be stored")
	}
	if n := countRows(t, s, `SELECT count(*) FROM activity a WHERE a.kind='Like'`+activeClause); n != 0 {
		t.Fatalf("active likes after undo = %d, want 0", n)
	}
	if r := ingest(t, s, like); !r.Created {
		t.Fatal("liking again after an undo should be stored")
	}
	if n := countRows(t, s, `SELECT count(*) FROM activity WHERE kind='Like'`); n != 3 {
		t.Fatalf("all like activities = %d, want 3", n)
	}
}

func TestAddAndRemovePerCollection(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	add := func(c string) string {
		return `{"type":"Add","actor":"zhihu:user:1","object":"zhihu:answer:5","target":"zhihu:collection:` + c + `"}`
	}
	remove := `{"type":"Remove","actor":"zhihu:user:1","object":"zhihu:answer:5","target":"zhihu:collection:ai"}`
	ingest(t, s, add("ai"))
	ingest(t, s, add("later"))
	if r := ingest(t, s, add("ai")); r.Created {
		t.Fatal("same collection twice changes nothing")
	}
	ingest(t, s, remove)
	var target string
	var adds int
	if err := s.reads.QueryRowContext(ctx, `SELECT count(*), coalesce(max(target),'') FROM activity a WHERE a.kind='Add'`+activeClause).Scan(&adds, &target); err != nil || adds != 1 || target != "zhihu:collection:later" {
		t.Fatalf("active adds = %d %q %v, want only the other collection", adds, target, err)
	}
}

func TestRejectsInvalidDocuments(t *testing.T) {
	s := openTest(t)
	ingestErr(t, s, `{"type":"Like","actor":"weibo:user:9"}`, "needs an object")
	ingestErr(t, s, `{"type":"Like","actor":{"type":"Person","id":"weibo:user:9"},"object":"weibo:post:2"}`, "embedded objects")
	ingestErr(t, s, `{"type":"Like","actor":"nope","object":"weibo:post:2"}`, "platform:kind:id")
	ingestErr(t, s, `{"type":"Add","actor":"weibo:user:9","object":"weibo:post:2"}`, "needs a target")
	ingestErr(t, s, `{"type":"Like","actor":"weibo:user:9","object":"weibo:post:2","target":"weibo:collection:1"}`, "no target")
	ingestErr(t, s, `{"type":"Undo","actor":"weibo:user:9","object":{"type":"Add","object":"weibo:post:2"}}`, "Remove for Add")
	ingestErr(t, s, `{"type":"Note","content":"no id"}`, "platform:kind:id")
	ingestErr(t, s, `{"type":"Note","id":"weibo:post:3","published":"yesterday"}`, "RFC 3339")
	ingestErr(t, s, `{"type":"Note","id":"weibo:post:3","attachment":[{"type":"Image"}]}`, "needs a url")
	ingestErr(t, s, `{"content":"no type"}`, "needs a type")
	if _, err := s.Ingest(context.Background(), json.RawMessage(`{"document":`+note+`}`)); err == nil {
		t.Fatal("observed_at is required")
	}
}

func TestAnnounceIdentity(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	const first = `{"type":"Announce","id":"x:post:11","actor":"x:user:9","object":"x:post:1","content":"I agree"}`
	const second = `{"type":"Announce","id":"x:post:12","actor":"x:user:9","object":"x:post:1","content":"I changed my mind"}`
	if !ingest(t, s, first).Created || !ingest(t, s, second).Created || ingest(t, s, first).Created {
		t.Fatal("distinct quotes must be stored and replays must be ignored")
	}
	items, err := s.TimelinePage(ctx, 0, []string{"Announce"}, 10, false, "", false, PageRange{})
	if err != nil || len(items) != 2 {
		t.Fatalf("timeline after two quotes: got %d items, err %v", len(items), err)
	}
	undo := `{"type":"Undo","actor":"x:user:9","object":` + first + `}`
	if !ingest(t, s, undo).Created || ingest(t, s, undo).Created || ingest(t, s, first).Created {
		t.Fatal("undo must apply once and replaying the quote must not restore it")
	}
	items, err = s.TimelinePage(ctx, 0, []string{"Announce"}, 10, false, "", false, PageRange{})
	if err != nil || len(items) != 1 || !strings.Contains(string(items[0].Activity.Document), "x:post:12") {
		t.Fatalf("timeline after undo: got %+v, err %v", items, err)
	}
}

func TestRefShape(t *testing.T) {
	for i := 0; i < 2000; i++ {
		ref, err := randomRef(refLength)
		if err != nil || len(ref) != refLength || strings.Trim(ref, refAlphabet) != "" {
			t.Fatalf("randomRef = %q, %v", ref, err)
		}
	}
}

// A later version stored without a raw response (a repair of the document) does not hide the
// raw response of an earlier one: re-checks read the newest there is.

// countRows runs a counting query on the archive's read connection.
func countRows(t *testing.T, s *Store, query string) int {
	t.Helper()
	var n int
	if err := s.reads.QueryRowContext(context.Background(), query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
