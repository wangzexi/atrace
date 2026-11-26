package core

// Tests of stored files and media copies.

import (
	"context"
	"strings"
	"testing"
)

func TestMediaCopies(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	const source = "https://wx1.sinaimg.cn/large/a.jpg"
	// A document may point at media that was never downloaded.
	ingest(t, s, `{"type":"Note","id":"weibo:post:5","attachment":[{"type":"Image","url":"`+source+`"}]}`)
	refs, _ := s.MediaRefs(ctx, []string{source})
	if len(refs) != 0 {
		t.Fatalf("refs = %v, want none", refs)
	}
	ref, err := s.PutFile(ctx, strings.NewReader("image bytes"), source, "image/jpeg")
	if err != nil || !ValidFileRef(ref) {
		t.Fatalf("PutFile = %q, %v", ref, err)
	}
	if again, _ := s.PutFile(ctx, strings.NewReader("image bytes"), "", ""); again != ref {
		t.Fatalf("same bytes gave %q, want %q", again, ref)
	}
	refs, _ = s.MediaRefs(ctx, []string{source})
	if refs[source] != ref {
		t.Fatalf("refs = %v, want %s", refs, ref)
	}
	body, err := s.OpenFile(ref)
	if err != nil {
		t.Fatal(err)
	}
	body.Close()
	if _, err := s.PutFile(ctx, strings.NewReader("x"), "ftp://nope", ""); !IsInputError(err) {
		t.Fatalf("bad source URL error = %v", err)
	}
}

// A database written before announce_id existed is migrated once, keeping its
// quotes separate and an undone quote undone.
