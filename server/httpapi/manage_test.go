package httpapi_test

// Tests of the owner's edits under /admin.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Everything collected is private until the owner makes an entry public, and a
// like or favorite is made public on its own, apart from the post it points at.
func TestVisibilityDefaultsToPrivate(t *testing.T) {
	srv := server(t)
	for _, doc := range []string{
		`{"type":"Note","id":"weibo:post:11","content":"mine","published":"2026-11-13T00:00:00Z"}`,
		`{"type":"Create","actor":"weibo:user:1","object":"weibo:post:11","published":"2026-11-13T00:00:00Z"}`,
		`{"type":"Like","actor":"weibo:user:1","object":"weibo:post:11"}`,
	} {
		if status, body := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`); status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, body)
		}
	}
	var page struct {
		Entries []struct {
			Ref        string `json:"ref"`
			ActivityID int64  `json:"activityId"`
			Type       string `json:"type"`
			Public     bool   `json:"public"`
		} `json:"entries"`
	}
	load := func() {
		_, body := do(t, "GET", srv.URL+"/api/entries", "")
		page.Entries = nil
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatal(err)
		}
	}
	load()
	if len(page.Entries) != 2 {
		t.Fatalf("entries = %+v, want a post and a like", page.Entries)
	}
	for _, e := range page.Entries {
		if e.Public {
			t.Fatalf("%+v is public before the owner chose it", e)
		}
	}
	var like string
	for _, e := range page.Entries {
		if e.Type == "Like" {
			like = e.Ref
		}
	}
	if status, body := do(t, "PATCH", fmt.Sprintf("%s/admin/entry/%s", srv.URL, like), `{"public":true}`); status != 200 {
		t.Fatalf("make public = %d %s", status, body)
	}
	load()
	for _, e := range page.Entries {
		if e.Public != (e.Ref == like) {
			t.Fatalf("only the like should be public: %+v", page.Entries)
		}
	}
	if status, _ := do(t, "PATCH", srv.URL+"/admin/entry/nothere0", `{"public":true}`); status != 404 {
		t.Fatalf("publishing a missing entry = %d, want 404", status)
	}
	if status, _ := do(t, "PATCH", fmt.Sprintf("%s/admin/entry/%s", srv.URL, like), `{}`); status != 400 {
		t.Fatalf("a request without public = %d, want 400", status)
	}
	do(t, "PATCH", fmt.Sprintf("%s/admin/entry/%s", srv.URL, like), `{"public":false}`)
	load()
	for _, e := range page.Entries {
		if e.Public {
			t.Fatalf("%+v is still public after making it private", e)
		}
	}
}

// The owner can give an entry a readable address, /<alias>, besides its random ref.
func TestEntryAlias(t *testing.T) {
	srv := server(t)
	for _, id := range []string{"41", "42"} {
		for _, doc := range []string{
			`{"type":"Note","id":"weibo:post:` + id + `","content":"note ` + id + `","published":"2026-11-13T00:00:00Z"}`,
			`{"type":"Create","actor":"weibo:user:1","object":"weibo:post:` + id + `","published":"2026-11-13T00:00:00Z"}`,
		} {
			do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`)
		}
	}
	_, body := do(t, "GET", srv.URL+"/api/entries", "")
	var page struct {
		Entries []struct {
			ActivityID int64  `json:"activityId"`
			Ref        string `json:"ref"`
			Alias      string `json:"alias"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil || len(page.Entries) != 2 {
		t.Fatalf("entries = %s (%v)", body, err)
	}
	first, second := page.Entries[0], page.Entries[1]
	set := func(ref string, alias string) int {
		status, _ := do(t, "PATCH", fmt.Sprintf("%s/admin/entry/%s", srv.URL, ref), fmt.Sprintf(`{"alias":%q}`, alias))
		return status
	}
	if status := set(first.Ref, "my-first-post"); status != 200 {
		t.Fatalf("set alias = %d", status)
	}
	if status, _ := do(t, "GET", srv.URL+"/my-first-post", ""); status != 200 && status != 503 {
		t.Errorf("/my-first-post = %d, want the page", status)
	}
	if status, body := do(t, "GET", srv.URL+"/api/entry?ref=my-first-post", ""); status != 200 || !strings.Contains(body, `"alias":"my-first-post"`) || !strings.Contains(body, first.Ref) {
		t.Errorf("entry by alias = %d %s", status, body)
	}
	for _, bad := range []string{"ab", "Has-Capital", "-lead", "trail-", "under_score", "login", "abcd1234", strings.Repeat("a", 41)} {
		if status := set(second.Ref, bad); status != 400 {
			t.Errorf("alias %q = %d, want 400", bad, status)
		}
	}
	if status := set(second.Ref, "my-first-post"); status != 400 {
		t.Errorf("an alias another entry has = %d, want 400", status)
	}
	if status := set(first.Ref, "my-first-post"); status != 200 {
		t.Errorf("setting the same alias again = %d, want 200", status)
	}
	if status := set("nothere0", "nobody"); status != 404 {
		t.Errorf("alias on a missing entry = %d, want 404", status)
	}
	if status := set(first.Ref, ""); status != 200 {
		t.Fatalf("clear alias = %d", status)
	}
	if status, _ := do(t, "GET", srv.URL+"/my-first-post", ""); status != 404 {
		t.Errorf("a cleared alias = %d, want 404", status)
	}
}

// The owner's own comment under their post is in the comment area with the others, under their name.
func TestOwnCommentIsAmongTheComments(t *testing.T) {
	srv := server(t)
	for _, doc := range []string{
		`{"type":"Person","id":"xhs:user:2","name":"Lain"}`,
		`{"type":"Note","id":"xhs:note:1","attributedTo":"xhs:user:1","content":"the post","published":"2026-11-17T00:00:00Z"}`,
		`{"type":"Create","actor":"xhs:user:1","object":"xhs:note:1","published":"2026-11-17T00:00:00Z"}`,
		`{"type":"Note","id":"xhs:comment:a","attributedTo":"xhs:user:2","inReplyTo":"xhs:note:1","content":"a question","published":"2026-11-17T01:00:00Z"}`,
		`{"type":"Note","id":"xhs:comment:b","attributedTo":"xhs:user:1","inReplyTo":"xhs:note:1","content":"my answer","published":"2026-11-17T02:00:00Z"}`,
		`{"type":"Note","id":"xhs:comment:c","attributedTo":"xhs:user:1","inReplyTo":"xhs:comment:a","content":"an answer inside the thread","published":"2026-11-17T03:00:00Z"}`,
		`{"type":"Note","id":"xhs:comment:d","attributedTo":"xhs:user:2","inReplyTo":"xhs:comment:c","content":"and a reply to that","published":"2026-11-17T04:00:00Z"}`,
	} {
		if status, body := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-18T00:00:00Z","observed_by":"test","document":`+doc+`}`); status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, body)
		}
	}
	bindOwner(t, srv, "xhs:user:1")
	_, body := do(t, "GET", srv.URL+"/api/entries", "")
	var page struct {
		Entries []struct {
			Replies []struct {
				Author  string `json:"author"`
				Content string `json:"content"`
				ReplyTo string `json:"replyTo"`
			} `json:"replies"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil || len(page.Entries) != 1 {
		t.Fatalf("entries = %s (%v)", body, err)
	}
	// One flat list, oldest first; each reply names the author it answers.
	replies := page.Entries[0].Replies
	if len(replies) != 4 || replies[0].Author != "Lain" || replies[1].Author != "Alice" || replies[1].ReplyTo != "Lain" ||
		replies[2].Author != "Lain" || replies[2].ReplyTo != "Alice" || replies[3].Content != "my answer" {
		t.Errorf("replies = %+v, want one flat list: Lain, Alice answering Lain, Lain answering Alice, then Alice's answer", replies)
	}
	if strings.Contains(body, "additions") {
		t.Error("the entry still has additions")
	}
}
