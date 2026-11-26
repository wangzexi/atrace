package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// A visitor's pattern is bounded in length, and the compiled cache never grows past its limit.
func TestSearchPatternsAreBounded(t *testing.T) {
	if err := ValidSearchPattern(strings.Repeat("a", maxSearchPattern+1)); err == nil {
		t.Fatal("a pattern over the length limit was accepted")
	}
	for i := 0; i < maxCachedPatterns*3; i++ {
		if err := ValidSearchPattern(fmt.Sprintf("word%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	patterns.Lock()
	n := len(patterns.compiled)
	patterns.Unlock()
	if n > maxCachedPatterns {
		t.Fatalf("the cache holds %d patterns, want at most %d", n, maxCachedPatterns)
	}
}

// An entry's own public state: the Create of a reply or post, and not a reply's parent.
func TestObjectIsPublicIsItsOwnEntry(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	ingest(t, s, note)
	ingest(t, s, `{"type":"Create","actor":"weibo:user:9","object":"weibo:post:1","published":"2026-11-13T00:00:00Z"}`)
	if e, err := s.ObjectEntry(ctx, "weibo:post:1"); err != nil || e.Public {
		t.Fatalf("a private post is public: %+v %v", e, err)
	}
	var id int64
	if err := s.reads.QueryRowContext(ctx, `SELECT id FROM activity WHERE object='weibo:post:1'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPublic(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if e, err := s.ObjectEntry(ctx, "weibo:post:1"); err != nil || !e.Public || e.ActivityID == 0 {
		t.Fatalf("a public post is not public: %+v, %v", e, err)
	}
	if e, _ := s.ObjectEntry(ctx, "weibo:post:404"); e.Public || e.ActivityID != 0 {
		t.Fatal("an unknown object has an entry")
	}
}
