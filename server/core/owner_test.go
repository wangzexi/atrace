package core

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// flag marks what a platform is said to have taken away. Nothing in the store sets these flags any more, so a
// test does, as an archive that has them from before does.
func flag(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

// Only what is flagged as gone from its platform can be deleted from the archive; what is not is kept.
func TestOnlyAFlaggedEntryCanBeDeleted(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	ingest(t, s, `{"type":"Note","id":"weibo:post:1","attributedTo":"weibo:user:1","content":"mine"}`)
	ingest(t, s, `{"type":"Create","actor":"weibo:user:1","object":"weibo:post:1","published":"2026-11-13T00:00:00Z"}`)
	ingest(t, s, `{"type":"Note","id":"weibo:post:2","attributedTo":"weibo:user:5","content":"theirs"}`)
	ingest(t, s, `{"type":"Like","actor":"weibo:user:1","object":"weibo:post:2"}`)

	items, err := s.TimelinePage(ctx, 0, []string{"Create", "Like"}, 10, false, "", false, PageRange{})
	if err != nil || len(items) != 2 {
		t.Fatalf("timeline = %+v, %v", items, err)
	}
	var post, like int64
	for _, item := range items {
		if item.Activity.Kind == "Create" {
			post = item.Activity.ID
		} else {
			like = item.Activity.ID
		}
	}
	for _, id := range []int64{post, like} {
		if err := s.HideActivity(ctx, id); !IsInputError(err) {
			t.Errorf("hiding %d, which nothing flags: %v, want an input error", id, err)
		}
	}
	if err := s.HideActivity(ctx, 999); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("hiding an entry there is not: %v", err)
	}

	// A post flagged as gone, and a like flagged as taken back, can go.
	flag(t, s, `INSERT INTO object_gone (object_id, gone_at) VALUES ('weibo:post:1', ?)`, formatTime(time.Now()))
	flag(t, s, `INSERT INTO activity_dropped (activity_id, dropped_at) VALUES (?, ?)`, like, formatTime(time.Now()))
	for _, id := range []int64{post, like} {
		if err := s.HideActivity(ctx, id); err != nil {
			t.Fatalf("hiding %d: %v", id, err)
		}
		if err := s.HideActivity(ctx, id); err != nil {
			t.Errorf("hiding %d again changes nothing, got %v", id, err)
		}
	}
	if items, _ = s.TimelinePage(ctx, 0, []string{"Create", "Like"}, 10, false, "", false, PageRange{}); len(items) != 0 {
		t.Errorf("a deleted entry is still on the timeline: %+v", items)
	}
	if err := s.SetPublic(ctx, post, true); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("a deleted entry was made public: %v", err)
	}
	if err := s.RestoreActivity(ctx, post); err != nil {
		t.Fatal(err)
	}
	if items, _ = s.TimelinePage(ctx, 0, []string{"Create"}, 10, false, "", false, PageRange{}); len(items) != 1 {
		t.Errorf("a restored entry is missing: %+v", items)
	}
}

func TestOnlyAFlaggedCommentCanBeDeleted(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	ingest(t, s, `{"type":"Note","id":"weibo:post:1","attributedTo":"weibo:user:1","content":"mine"}`)
	ingest(t, s, `{"type":"Create","actor":"weibo:user:1","object":"weibo:post:1"}`)
	ingest(t, s, `{"type":"Note","id":"weibo:comment:1","attributedTo":"weibo:user:5","inReplyTo":"weibo:post:1","content":"gone from the platform"}`)
	ingest(t, s, `{"type":"Create","actor":"weibo:user:5","object":"weibo:comment:1"}`)
	ingest(t, s, `{"type":"Note","id":"weibo:comment:2","attributedTo":"weibo:user:5","inReplyTo":"weibo:post:1","content":"still there"}`)
	ingest(t, s, `{"type":"Create","actor":"weibo:user:5","object":"weibo:comment:2"}`)
	var comment int64
	if err := s.reads.QueryRowContext(ctx, `SELECT id FROM activity WHERE object='weibo:comment:1' AND kind='Create'`).Scan(&comment); err != nil {
		t.Fatal(err)
	}

	if err := s.HideActivity(ctx, comment); !IsInputError(err) {
		t.Fatalf("hiding a comment nothing flags: %v, want an input error", err)
	}
	flag(t, s, `INSERT INTO object_gone (object_id, gone_at) VALUES ('weibo:comment:1', ?)`, formatTime(time.Now()))
	if err := s.HideActivity(ctx, comment); err != nil {
		t.Fatal(err)
	}
	if err := s.HideActivity(ctx, comment); err != nil {
		t.Errorf("hiding it again changes nothing, got %v", err)
	}
	if replies, err := s.Replies(ctx, "weibo:post:1"); err != nil || len(replies) != 1 || replies[0].ID != "weibo:comment:2" {
		t.Errorf("replies after hiding = %+v, %v, want only the one that is still there", replies, err)
	}
	// What the entry is searched for no longer holds the comment that was deleted.
	if items, _ := s.TimelinePage(ctx, 0, nil, 10, false, "gone from the platform", false, PageRange{}); len(items) != 0 {
		t.Errorf("the search still finds a deleted comment: %+v", items)
	}
}
