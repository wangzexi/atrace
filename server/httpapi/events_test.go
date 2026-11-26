package httpapi_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.zexi.me/zexi/atrace/server/auth"
	"git.zexi.me/zexi/atrace/server/httpapi"
)

// openEvents connects to the event stream and returns the kinds of change it tells, once it is connected.
func openEvents(t *testing.T, url string) <-chan string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, "GET", url+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("status %d, content type = %q", resp.StatusCode, got)
	}
	kinds := make(chan string, 32)
	connected := make(chan struct{})
	go func() {
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, ": connected") {
				close(connected)
			}
			if kind, ok := strings.CutPrefix(line, "event: "); ok {
				kinds <- kind
			}
		}
	}()
	select {
	case <-connected:
	case <-time.After(3 * time.Second):
		t.Fatal("the stream did not connect")
	}
	return kinds
}

func expectKind(t *testing.T, kinds <-chan string, want string) {
	t.Helper()
	select {
	case got := <-kinds:
		if got != want {
			t.Fatalf("told %q, want %q", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("not told %q", want)
	}
}

func expectNothing(t *testing.T, kinds <-chan string) {
	t.Helper()
	select {
	case got := <-kinds:
		t.Fatalf("told %q, want nothing", got)
	case <-time.After(300 * time.Millisecond):
	}
}

// A page is told when a collector writes an entry or the owner edits one, and not when anything is read.
func TestPagesAreToldWhatChanged(t *testing.T) {
	srv := server(t)
	kinds := openEvents(t, srv.URL)
	do(t, "GET", srv.URL+"/api/entries", "")
	expectNothing(t, kinds)
	ingest := func(doc string) {
		t.Helper()
		if status, body := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`); status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, body)
		}
	}
	ingest(`{"type":"Note","id":"weibo:post:1","content":"hello","published":"2026-11-13T00:00:00Z"}`)
	expectKind(t, kinds, "entries")
	// A write that fails changes nothing.
	if status, _ := do(t, "POST", srv.URL+"/collect/ingest", `not json`); status < 400 {
		t.Fatalf("a bad document = %d", status)
	}
	expectNothing(t, kinds)
	// A burst of writes is told at least once and never more often than it wrote.
	for i := 0; i < 5; i++ {
		ingest(`{"type":"Note","id":"weibo:post:2","content":"again","published":"2026-11-13T00:00:00Z"}`)
	}
	expectKind(t, kinds, "entries")
	told := 1
	for {
		select {
		case <-kinds:
			told++
			continue
		case <-time.After(300 * time.Millisecond):
		}
		break
	}
	if told > 5 {
		t.Errorf("told %d times for 5 writes", told)
	}
}

// What the collectors are doing is the owner's to see: a visitor is not told of it, and is told of entries.
func TestCollectorChangesAreOnlyToldToTheOwner(t *testing.T) {
	hub := httpapi.NewHub()
	owner := httptest.NewServer(auth.AsOwner(hub))
	t.Cleanup(owner.Close) // runs after the streams are cancelled: a server waits for its open streams
	visitor := httptest.NewServer(hub)
	t.Cleanup(visitor.Close)
	ownerKinds, visitorKinds := openEvents(t, owner.URL), openEvents(t, visitor.URL)
	hub.Publish(httpapi.ChangedCollectors)
	expectKind(t, ownerKinds, "collectors")
	expectNothing(t, visitorKinds)
	hub.Publish(httpapi.ChangedEntries)
	expectKind(t, ownerKinds, "entries")
	expectKind(t, visitorKinds, "entries")
}
