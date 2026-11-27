package httpapi_test

// Tests of served files.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.zexi.me/zexi/atrace/server/auth"
)

// An SVG is served as an image, and sandboxed so that opening it on its own runs nothing.
func TestSVGFilesAreSandboxed(t *testing.T) {
	srv := server(t)
	_, body := do(t, "PUT", srv.URL+"/collect/media?url=https://example.com/a.svg", `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	ref := strings.Split(strings.Split(body, `"ref":"`)[1], `"`)[0]
	resp, err := http.Get(srv.URL + "/files/" + ref)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "image/svg+xml" {
		t.Errorf("Content-Type = %q, want image/svg+xml", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); !strings.Contains(got, "sandbox") || !strings.Contains(got, "default-src 'none'") {
		t.Errorf("Content-Security-Policy = %q, want a sandbox", got)
	}
}

func TestLivePhotoMotionIsServedAsQuickTimeWithRangeSupport(t *testing.T) {
	srv := server(t)
	motion := string([]byte{0, 0, 0, 20}) + "ftypqt  " + strings.Repeat("\x00", 24)
	_, body := do(t, "PUT", srv.URL+"/collect/media?url=https://us.sinaimg.cn/live.mov", motion)
	ref := strings.Split(strings.Split(body, `"ref":"`)[1], `"`)[0]
	req, err := http.NewRequest("GET", srv.URL+"/files/"+ref, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-11")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Type") != "video/quicktime" {
		t.Fatalf("motion response: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

func TestMediaRequiresVisibleEntryForVisitors(t *testing.T) {
	const password = "media owner password"
	store := newStore(t, password)
	srv := serveStore(t, store, auth.New(store).Middleware, true)
	ctx := context.Background()
	ingest := func(doc string) {
		t.Helper()
		if _, err := store.Ingest(ctx, json.RawMessage(`{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`)); err != nil {
			t.Fatal(err)
		}
	}
	const source = "https://example.test/cover.jpg"
	ref, err := store.PutFile(ctx, strings.NewReader("fixture media"), source, "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	ingest(`{"type":"Note","id":"weibo:post:media","attributedTo":"weibo:user:9","attachment":[{"type":"Image","url":"` + source + `"}]}`)
	ingest(`{"type":"Create","actor":"weibo:user:9","object":"weibo:post:media"}`)
	ingest(`{"type":"Note","id":"weibo:post:quote","attributedTo":"weibo:user:9","quote":"weibo:post:media"}`)
	ingest(`{"type":"Create","actor":"weibo:user:9","object":"weibo:post:quote"}`)
	post, err := store.ObjectEntry(ctx, "weibo:post:media")
	if err != nil {
		t.Fatal(err)
	}
	quote, err := store.ObjectEntry(ctx, "weibo:post:quote")
	if err != nil {
		t.Fatal(err)
	}
	get := func(owner bool, want int) {
		t.Helper()
		req, err := http.NewRequest("GET", srv.URL+"/files/"+ref, nil)
		if err != nil {
			t.Fatal(err)
		}
		if owner {
			req.Header.Set("Authorization", "Bearer "+password)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("owner %v: status %d, want %d", owner, resp.StatusCode, want)
		}
	}
	get(true, 200)
	get(false, 404)
	for _, id := range []int64{post.ActivityID, quote.ActivityID} {
		if err := store.SetPublic(ctx, id, true); err != nil {
			t.Fatal(err)
		}
		get(false, 200)
		if err := store.SetPublic(ctx, id, false); err != nil {
			t.Fatal(err)
		}
		get(false, 404)
	}
}

type zeroVideoReader struct{}

func (zeroVideoReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestLargeVideoUploadAndRange(t *testing.T) {
	srv := server(t)
	const size = 129 << 20
	header := string([]byte{0, 0, 0, 32}) + "ftypisom" + strings.Repeat("\x00", 20)
	body := io.MultiReader(strings.NewReader(header), io.LimitReader(zeroVideoReader{}, size-int64(len(header))))
	req, err := http.NewRequest("PUT", srv.URL+"/collect/media?url=https://example.test/large.mp4", body)
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "video/mp4")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Ref string `json:"ref"`
	}
	err = json.NewDecoder(response.Body).Decode(&stored)
	response.Body.Close()
	if err != nil || response.StatusCode >= 300 || stored.Ref == "" {
		t.Fatalf("upload: %d %v", response.StatusCode, err)
	}
	req, _ = http.NewRequest("GET", srv.URL+"/files/"+stored.Ref, nil)
	req.Header.Set("Range", "bytes=0-11")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	clip, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 206 || string(clip) != header[:12] {
		t.Fatalf("range: %d %q %v", response.StatusCode, clip, err)
	}
}

func TestVideoUploadRejectsDeclaredSizeOverLimit(t *testing.T) {
	srv := server(t)
	req := httptest.NewRequest("PUT", "/collect/media?url=https://example.test/too-large.mp4", nil)
	req.ContentLength = (1 << 30) + 1
	response := httptest.NewRecorder()
	srv.Config.Handler.ServeHTTP(response, req)
	if response.Code != 413 {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestLegacyQuickTimeRange(t *testing.T) {
	srv := server(t)
	motion := string([]byte{0, 0, 0, 8}) + "wide" + string([]byte{0, 0, 0, 32}) + "mdat" + strings.Repeat("\x00", 24)
	_, body := do(t, "PUT", srv.URL+"/collect/media?url=https://us.sinaimg.cn/legacy.mov", motion)
	var stored struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal([]byte(body), &stored); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/files/"+stored.Ref, nil)
	req.Header.Set("Range", "bytes=0-15")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 206 || resp.Header.Get("Content-Type") != "video/quicktime" {
		t.Fatalf("response %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}
