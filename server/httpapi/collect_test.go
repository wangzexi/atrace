package httpapi_test

// Tests of what collectors send to /collect.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIngestTimelineAndHome(t *testing.T) {
	srv := server(t)
	const image = "https://wx1.sinaimg.cn/large/a.jpg"

	status, body := do(t, "PUT", srv.URL+"/collect/media?url="+image, "image bytes")
	if status != 201 || !strings.Contains(body, "sha256:") {
		t.Fatalf("upload = %d %s", status, body)
	}
	ref := strings.Split(strings.Split(body, `"ref":"`)[1], `"`)[0]

	if status, body = do(t, "GET", srv.URL+"/collect/media?url="+image, ""); status != 200 || !strings.Contains(body, ref) {
		t.Fatalf("media = %d %s", status, body)
	}
	if status, _ = do(t, "GET", srv.URL+"/collect/media?url=https://example.com/none.jpg", ""); status != 404 {
		t.Fatalf("unknown media = %d, want 404", status)
	}

	ingest := func(document string) (int, string) {
		return do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+document+`}`)
	}
	status, body = ingest(`{"type":"Note","id":"weibo:post:1","content":"hello [doge]","url":"https://weibo.com/1/a","published":"2026-11-13T00:00:00Z","attachment":[{"type":"Image","url":"` + image + `"}]}`)
	if status != 201 {
		t.Fatalf("ingest note = %d %s", status, body)
	}
	if status, _ = ingest(`{"type":"Create","actor":"weibo:user:1","object":"weibo:post:1","published":"2026-11-13T00:00:00Z"}`); status != 201 {
		t.Fatalf("ingest create = %d", status)
	}
	if status, body = ingest(`{"type":"Note","id":"weibo:post:1","content":"hello [doge]","url":"https://weibo.com/1/a","published":"2026-11-13T00:00:00Z","attachment":[{"type":"Image","url":"` + image + `"}]}`); status != 200 {
		t.Fatalf("repeat ingest = %d %s, want 200 (nothing new)", status, body)
	}
	if status, body = ingest(`{"type":"Like","actor":{"id":"x"},"object":"weibo:post:1"}`); status != 400 {
		t.Fatalf("invalid document = %d %s, want 400", status, body)
	}

	status, body = do(t, "GET", srv.URL+"/api/entries", "")
	if status != 200 || !strings.Contains(body, `"weibo:post:1"`) || !strings.Contains(body, "hello") {
		t.Fatalf("timeline = %d %s", status, body)
	}

	// One timeline for everything: a like and a favorite of someone else's old
	// post, and a comment under the user's own post.
	ingest(`{"type":"Person","id":"weibo:user:2","name":"Alice","url":"https://weibo.com/u/2"}`)
	ingest(`{"type":"Note","id":"weibo:post:9","attributedTo":"weibo:user:2","content":"alice old post","published":"2020-01-01T00:00:00Z"}`)
	ingest(`{"type":"Like","actor":"weibo:user:1","object":"weibo:post:9"}`)
	ingest(`{"type":"Collection","id":"weibo:collection:favorites","name":"favorites"}`)
	ingest(`{"type":"Add","actor":"weibo:user:1","object":"weibo:post:9","target":"weibo:collection:favorites"}`)
	ingest(`{"type":"Person","id":"weibo:user:1","name":"Alice"}`)
	ingest(`{"type":"Note","id":"weibo:comment:1","attributedTo":"weibo:user:1","inReplyTo":"weibo:post:1","content":"my own reply","published":"2026-11-13T01:00:00Z"}`)
	status, body = do(t, "GET", srv.URL+"/api/entries", "")
	for _, want := range []string{`"type":"Like"`, `"type":"Add"`, "Alice", "alice old post", "my own reply", "Alice"} {
		if status != 200 || !strings.Contains(body, want) {
			t.Fatalf("home lacks %q (status %d)", want, status)
		}
	}
	// The like is dated by the post it points at, so it sits below the newer post.
	if strings.Index(body, "hello") > strings.Index(body, "alice old post") {
		t.Fatalf("a 2020 post that was liked should sort below a 2026 post")
	}

	// A deleted entry is hidden from every reader until it is restored.
	var listed struct {
		Entries []struct {
			Ref    string `json:"ref"`
			Object string `json:"id"`
		} `json:"entries"`
	}
	_, body = do(t, "GET", srv.URL+"/api/entries?types=Like", "")
	if err := json.Unmarshal([]byte(body), &listed); err != nil || len(listed.Entries) == 0 {
		t.Fatalf("likes = %s (%v)", body, err)
	}
	liked := listed.Entries[0]
	hideURL := fmt.Sprintf("%s/admin/entry/%s", srv.URL, liked.Ref)
	// Only a post flagged as gone from its platform can be deleted; the rest is kept, and made private if it should not show
	// (the flagged case is the store's own test, as nothing sets a flag any more).
	if status, body = do(t, "PATCH", hideURL, `{"hidden":true}`); status != 400 {
		t.Fatalf("hiding a post that is not flagged as gone = %d %s, want 400", status, body)
	}
	if status, _ = do(t, "PATCH", srv.URL+"/admin/entry/nothere0", `{"hidden":true}`); status != 404 {
		t.Fatalf("hiding a missing entry = %d, want 404", status)
	}
	if _, body = do(t, "GET", srv.URL+"/api/entries?types=Like", ""); !strings.Contains(body, "alice old post") {
		t.Fatalf("a like that could not be hidden is missing: %s", body)
	}

	// An article shows its title once: as its own field, the text does not repeat it.
	ingest(`{"type":"Article","id":"blog:article:abc","name":"Distinct Title","content":"body text","published":"2026-11-14T00:00:00Z"}`)
	ingest(`{"type":"Create","actor":"blog:user:z","object":"blog:article:abc","published":"2026-11-14T00:00:00Z"}`)
	status, body = do(t, "GET", srv.URL+"/api/entries", "")
	if status != 200 || strings.Count(body, "Distinct Title") != 1 || !strings.Contains(body, "body text") {
		t.Fatalf("article title shown %d times (status %d)", strings.Count(body, "Distinct Title"), status)
	}

	// The first page holds pageSize cards; the rest load by cursor.
	for i := 0; i < 25; i++ {
		id := "weibo:post:filler" + string(rune('a'+i))
		ingest(`{"type":"Note","id":"` + id + `","content":"filler","published":"2019-01-01T00:00:00Z"}`)
		ingest(`{"type":"Create","actor":"weibo:user:1","object":"` + id + `","published":"2019-01-01T00:00:00Z"}`)
	}
	status, body = do(t, "GET", srv.URL+"/api/entries", "")
	if status != 200 || !strings.Contains(body, `"next":"`) || !strings.Contains(body, `"more":true`) {
		t.Fatalf("window = %d %.200s", status, body)
	}
	var cursorPage struct {
		Next string `json:"next"`
	}
	if err := json.Unmarshal([]byte(body), &cursorPage); err != nil || cursorPage.Next == "" {
		t.Fatal("missing cursor", body)
	}
	if status, body = do(t, "GET", srv.URL+"/api/entries?after="+cursorPage.Next, ""); status != 200 || !strings.Contains(body, `"more":false`) {
		t.Fatalf("second window = %d %.200s", status, body)
	}

	// The page uses the local copy of the image, not the platform URL.
	status, body = do(t, "GET", srv.URL+"/api/entries", "")
	if status != 200 || !strings.Contains(body, "/files/"+ref) || strings.Contains(body, "sinaimg.cn") {
		t.Fatalf("home = %d, want local image %s in:\n%s", status, ref, body)
	}
	if status, body = do(t, "GET", srv.URL+"/files/"+ref, ""); status != 200 || body != "image bytes" {
		t.Fatalf("file = %d %q", status, body)
	}
}

// The path groups are the boundary between collectors, readers and the future
// public page; old ungrouped paths must not answer.
func TestPathGroups(t *testing.T) {
	srv := server(t)
	for _, path := range []string{"/ingest", "/timeline", "/objects?id=x", "/activities", "/media?url=x", "/feed/window", "/admin/feed/window", "/admin/api/entries"} {
		if status, _ := do(t, "GET", srv.URL+path, ""); status != 404 && status != 405 {
			t.Errorf("GET %s = %d, want 404 or 405", path, status)
		}
	}
	if status, _ := do(t, "PUT", srv.URL+"/collect/media", "bytes"); status != 400 {
		t.Errorf("PUT /collect/media without url = %d, want 400", status)
	}
	resp, err := http.Get(srv.URL + "/admin/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("/admin/ = %d, want 404: the page lives at /", resp.StatusCode)
	}
}

// Every recorded collector fixture is what a collector really sends. The core
// must accept all of it. Replay one document per kind to exercise deduplication.
func TestCollectorFixtures(t *testing.T) {
	files, err := filepath.Glob("../../client/app/tests/collectors/*/testdata/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no collector fixtures found: %v", err)
	}
	srv := server(t)
	send := func(document json.RawMessage) int {
		body := `{"observed_at":"2026-11-15T00:00:00Z","observed_by":"fixture","document":` + string(document) + `}`
		status, response := do(t, "POST", srv.URL+"/collect/ingest", body)
		if status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, response)
		}
		return status
	}
	replayed := make(map[string]bool)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var fixture struct {
			Expected []struct {
				Document json.RawMessage `json:"document"`
			} `json:"expected"`
		}
		if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Expected) == 0 {
			t.Fatalf("%s: unreadable fixture: %v", file, err)
		}
		for _, entry := range fixture.Expected {
			send(entry.Document)
		}
		for _, entry := range fixture.Expected {
			var doc struct{ Type string }
			if err := json.Unmarshal(entry.Document, &doc); err != nil || doc.Type == "" {
				t.Fatalf("%s: invalid document kind: %v", file, err)
			}
			if replayed[doc.Type] {
				continue
			}
			replayed[doc.Type] = true
			if status := send(entry.Document); status != 200 {
				t.Errorf("%s: sending %s again = %d, want 200 (nothing new)", file, entry.Document, status)
			}
		}
	}
}

// A collector asks which of the activities it has read are stored, and gets one answer for each, in order.
// Asking writes nothing, so it tells no page that anything changed.
func TestKnownAnswersInOrderAndChangesNothing(t *testing.T) {
	srv := server(t)
	kinds := openEvents(t, srv.URL)
	for _, doc := range []string{
		`{"type":"Note","id":"weibo:post:1","attributedTo":"weibo:user:1","content":"hello"}`,
		`{"type":"Create","actor":"weibo:user:1","object":"weibo:post:1"}`,
	} {
		if status, body := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`); status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, body)
		}
	}
	expectKind(t, kinds, "entries")
	for quiet := false; !quiet; { // the second write may have been told on its own
		select {
		case <-kinds:
		case <-time.After(300 * time.Millisecond):
			quiet = true
		}
	}
	status, body := do(t, "POST", srv.URL+"/collect/known", `{"items":[{"actor":"weibo:user:1","kind":"Create","object":"weibo:post:2"},{"actor":"weibo:user:1","kind":"Create","object":"weibo:post:1"}]}`)
	if status != 200 || strings.ReplaceAll(body, "\n", "") != `{"known":[false,true]}` {
		t.Fatalf("known = %d %s", status, body)
	}
	expectNothing(t, kinds)
	for name, request := range map[string]string{
		"not JSON":      `nope`,
		"unknown kind":  `{"items":[{"kind":"Poke","object":"weibo:post:1"}]}`,
		"nothing asked": `{"items":[{"kind":"Like"}]}`,
	} {
		if status, _ := do(t, "POST", srv.URL+"/collect/known", request); status != 400 {
			t.Errorf("%s = %d, want 400", name, status)
		}
	}
	var many strings.Builder
	many.WriteString(`{"items":[`)
	for i := 0; i < 501; i++ {
		if i > 0 {
			many.WriteString(",")
		}
		many.WriteString(`{"kind":"Like","object":"weibo:post:1"}`)
	}
	many.WriteString(`]}`)
	if status, _ := do(t, "POST", srv.URL+"/collect/known", many.String()); status != 400 {
		t.Errorf("501 items = %d, want 400", status)
	}
}

// A collector can ask what type the copy of an image is, and replace a copy that was not the picture: the
// same address uploaded again points at the new file.
func TestMediaLookupSaysTheTypeAndAnUploadReplacesTheCopy(t *testing.T) {
	srv := server(t)
	image := "https://wx4.sinaimg.cn/large/a.jpg"
	put := func(body, mime string) string {
		t.Helper()
		req, err := http.NewRequest("PUT", srv.URL+"/collect/media?url="+image, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", mime)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct{ Ref string }
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != 201 {
			t.Fatalf("put = %d %v", resp.StatusCode, err)
		}
		return out.Ref
	}
	lookup := func() (ref, mime string) {
		t.Helper()
		status, body := do(t, "GET", srv.URL+"/collect/media?url="+image, "")
		var out struct{ Ref, Mime string }
		if status != 200 || json.Unmarshal([]byte(body), &out) != nil {
			t.Fatalf("lookup = %d %s", status, body)
		}
		return out.Ref, out.Mime
	}
	placeholder := put("placeholder gif", "image/gif")
	if ref, mime := lookup(); ref != placeholder || mime != "image/gif" {
		t.Fatalf("lookup = %s %s", ref, mime)
	}
	picture := put("the real jpeg", "image/jpeg")
	if picture == placeholder {
		t.Fatal("different bytes, same ref")
	}
	if ref, mime := lookup(); ref != picture || mime != "image/jpeg" {
		t.Fatalf("after replacing, lookup = %s %s", ref, mime)
	}
}
