package core

// Tests of what the page and the API read.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestObjectVersions(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	first := ingest(t, s, note)
	if !first.Created || first.VersionID == 0 {
		t.Fatalf("first = %+v", first)
	}
	// Key order and whitespace do not make a new version.
	same := ingest(t, s, `{ "published":"2026-11-13T08:00:00+08:00", "content":"hello", "attributedTo":"weibo:user:9", "id":"weibo:post:1", "type":"Note" }`)
	if same.Created || same.VersionID != first.VersionID {
		t.Fatalf("same = %+v, want no new version", same)
	}
	changed := ingest(t, s, strings.Replace(note, "hello", "hello again", 1))
	if !changed.Created || changed.VersionID == first.VersionID {
		t.Fatalf("changed = %+v", changed)
	}
	obj, err := s.Object(ctx, "weibo:post:1", false)
	if err != nil || obj.VersionID != changed.VersionID || !strings.Contains(string(obj.Document), "hello again") {
		t.Fatalf("latest = %+v, %v", obj, err)
	}
	var versions int
	if err := s.reads.QueryRowContext(ctx, `SELECT count(*) FROM object_version WHERE object_id='weibo:post:1'`).Scan(&versions); err != nil || versions != 2 {
		t.Fatalf("versions = %d, %v, want 2", versions, err)
	}
	// Going back to an earlier text is a new version, not a duplicate.
	back := ingest(t, s, note)
	if !back.Created {
		t.Fatal("reverting to older content should add a version")
	}
}

func TestRawIsOptionalAndHiddenByDefault(t *testing.T) {
	s := openTest(t)
	_, err := s.Ingest(context.Background(), json.RawMessage(`{"observed_at":"2026-11-14T00:00:00Z","raw":{"mid":"1"},"document":`+note+`}`))
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := s.Object(context.Background(), "weibo:post:1", false)
	withRaw, _ := s.Object(context.Background(), "weibo:post:1", true)
	if plain.Raw != nil || string(withRaw.Raw) != `{"mid":"1"}` {
		t.Fatalf("raw plain=%s withRaw=%s", plain.Raw, withRaw.Raw)
	}
}

func TestReplies(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	ingest(t, s, note)
	reply := func(id, text, at string) {
		ingest(t, s, `{"type":"Note","id":"weibo:comment:`+id+`","attributedTo":"weibo:user:5","inReplyTo":"weibo:post:1","content":"`+text+`","published":"`+at+`"}`)
	}
	reply("2", "second", "2026-11-13T10:00:00Z")
	reply("1", "first", "2026-11-13T09:00:00Z")
	ingest(t, s, `{"type":"Note","id":"weibo:comment:3","inReplyTo":"weibo:post:other","content":"elsewhere"}`)
	got, err := s.Replies(ctx, "weibo:post:1")
	if err != nil || len(got) != 2 || !strings.Contains(string(got[0].Document), "first") {
		t.Fatalf("replies = %+v, %v", got, err)
	}
	// An edited comment shows only its latest text.
	reply("1", "first edited", "2026-11-13T09:00:00Z")
	if got, _ = s.Replies(ctx, "weibo:post:1"); len(got) != 2 || !strings.Contains(string(got[0].Document), "first edited") {
		t.Fatalf("replies after edit = %+v", got)
	}
}

func TestTimeline(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	ingest(t, s, note)
	ingest(t, s, `{"type":"Create","actor":"weibo:user:9","object":"weibo:post:1","published":"2026-11-13T00:00:00Z"}`)
	ingest(t, s, `{"type":"Note","id":"weibo:post:7","content":"original"}`)
	ingest(t, s, `{"type":"Announce","actor":"weibo:user:9","object":"weibo:post:7","content":"look","published":"2026-11-14T00:00:00Z"}`)
	ingest(t, s, `{"type":"Like","actor":"weibo:user:9","object":"weibo:post:7"}`)
	items, err := s.TimelinePage(ctx, 0, nil, 10, false, "", false, PageRange{})
	if err != nil || len(items) != 2 {
		t.Fatalf("timeline = %+v, %v", items, err)
	}
	if items[0].Activity.Type != "Announce" || !strings.Contains(string(items[0].Object), "original") {
		t.Fatalf("newest item = %+v", items[0])
	}
	if items[1].Activity.Type != "Create" || !strings.Contains(string(items[1].Object), "hello") {
		t.Fatalf("second item = %+v", items[1])
	}
	ingest(t, s, `{"type":"Undo","actor":"weibo:user:9","object":{"type":"Announce","object":"weibo:post:7"}}`)
	if items, _ = s.TimelinePage(ctx, 0, nil, 10, false, "", false, PageRange{}); len(items) != 1 {
		t.Fatalf("timeline after undoing the announce = %d, want 1", len(items))
	}
	// Activities about objects that were never collected still list.
	ingest(t, s, `{"type":"Like","actor":"weibo:user:9","object":"weibo:post:404"}`)
	likes, _ := s.TimelinePage(ctx, 0, []string{"Like"}, 10, false, "", false, PageRange{})
	if len(likes) != 2 || likes[0].Object != nil {
		t.Fatalf("likes = %+v", likes)
	}
}

func TestLatestRawSkipsVersionsWithoutOne(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, err := s.Ingest(ctx, json.RawMessage(`{"observed_at":"2026-11-14T00:00:00Z","raw":{"token":"abc"},"document":{"type":"Note","id":"x:post:1","content":"one"}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ingest(ctx, json.RawMessage(`{"observed_at":"2026-11-15T00:00:00Z","document":{"type":"Note","id":"x:post:1","content":"two"}}`)); err != nil {
		t.Fatal(err)
	}
	raw, err := s.LatestRaw(ctx, "x:post:1")
	if err != nil || !strings.Contains(string(raw), "abc") {
		t.Errorf("LatestRaw = %s, %v", raw, err)
	}
	if raw, err = s.LatestRaw(ctx, "x:post:none"); err != nil || raw != nil {
		t.Errorf("LatestRaw of an unknown object = %s, %v", raw, err)
	}
}
