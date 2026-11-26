package httpapi_test

// Tests of the wants: the owner opens a post, and a device that listens is told.

import (
	"bufio"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// listen opens a device's stream and returns the lines read from it.
func listen(t *testing.T, url string) <-chan string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	lines := make(chan string, 20)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	return lines
}

func nextData(t *testing.T, lines <-chan string) string {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("the stream ended")
			}
			if strings.HasPrefix(line, "data: ") {
				return strings.TrimPrefix(line, "data: ")
			}
		case <-timeout:
			t.Fatal("no want arrived")
		}
	}
}

func TestOpeningAPostTellsTheDeviceThatHasItsPlatform(t *testing.T) {
	srv := server(t)
	if status, body := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":{"type":"Note","id":"weibo:post:1","attributedTo":"weibo:user:1","content":"hello","published":"2026-11-13T00:00:00Z"}}`); status != 201 {
		t.Fatalf("ingest = %d %s", status, body)
	}
	do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":{"type":"Create","actor":"weibo:user:1","object":"weibo:post:1","published":"2026-11-13T00:00:00Z"}}`)
	_, timeline := do(t, "GET", srv.URL+"/api/entries", "")
	match := regexp.MustCompile(`"ref":"([A-Za-z0-9]{8})"`).FindStringSubmatch(timeline)
	if match == nil {
		t.Fatalf("no ref in %s", timeline)
	}
	ref := match[1]

	weibo := listen(t, srv.URL+"/collect/wants?platforms=weibo")
	other := listen(t, srv.URL+"/collect/wants?platforms=x")
	time.Sleep(100 * time.Millisecond)

	if status, body := do(t, "POST", srv.URL+"/admin/fetch", `{"ref":"`+ref+`"}`); status != 200 || !strings.Contains(body, `"asked":true`) {
		t.Fatalf("want = %d %s", status, body)
	}
	if got := nextData(t, weibo); got != `{"post":"weibo:post:1","platform":"weibo"}` {
		t.Fatalf("the weibo device was told %s", got)
	}
	select {
	case line := <-other:
		if strings.HasPrefix(line, "data:") {
			t.Fatalf("a device without weibo was told %s", line)
		}
	case <-time.After(200 * time.Millisecond):
	}

	// The same post again is not asked for twice.
	if _, body := do(t, "POST", srv.URL+"/admin/fetch", `{"ref":"`+ref+`"}`); !strings.Contains(body, `"asked":false`) {
		t.Fatalf("a second want = %s, want asked:false", body)
	}
	if status, _ := do(t, "POST", srv.URL+"/admin/fetch", `{"ref":"nothere0"}`); status != 404 {
		t.Fatalf("an unknown ref = %d, want 404", status)
	}
}

func TestAWantWaitsForADeviceThatComesLater(t *testing.T) {
	srv := server(t)
	do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":{"type":"Note","id":"weibo:post:2","attributedTo":"weibo:user:1","content":"later","published":"2026-11-13T00:00:00Z"}}`)
	do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":{"type":"Create","actor":"weibo:user:1","object":"weibo:post:2","published":"2026-11-13T00:00:00Z"}}`)
	_, timeline := do(t, "GET", srv.URL+"/api/entries", "")
	ref := regexp.MustCompile(`"ref":"([A-Za-z0-9]{8})"`).FindStringSubmatch(timeline)[1]
	do(t, "POST", srv.URL+"/admin/fetch", `{"ref":"`+ref+`"}`)

	device := listen(t, srv.URL+"/collect/wants?platforms=weibo")
	if got := nextData(t, device); !strings.Contains(got, "weibo:post:2") {
		t.Fatalf("the late device was told %s", got)
	}
}
