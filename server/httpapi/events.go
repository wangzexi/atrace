package httpapi

// Events: the page is told that something changed, and asks for what it shows. A signal carries no
// data: nothing is sent to a viewer that the viewer could not have asked for.

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"git.zexi.me/zexi/atrace/server/auth"
)

// The kinds of change. Entries: what the timeline or a post shows. Collectors: the collector's
// connection, its runs and the settings of its platforms, which only the owner sees.
const (
	ChangedEntries    = "entries"
	ChangedCollectors = "collectors"
)

// Hub tells the connected pages what changed. Changes that come in a burst are told once.
type Hub struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

type subscriber struct {
	owner   bool
	wake    chan struct{}
	pending map[string]bool
}

func NewHub() *Hub { return &Hub{subs: map[*subscriber]struct{}{}} }

// Publish says that something of the kind changed. It never waits for a page.
func (h *Hub) Publish(kind string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		s.pending[kind] = true
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

func (h *Hub) subscribe(owner bool) *subscriber {
	s := &subscriber{owner: owner, wake: make(chan struct{}, 1), pending: map[string]bool{}}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

func (h *Hub) unsubscribe(s *subscriber) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
}

func (s *subscriber) take(h *Hub) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var kinds []string
	for _, kind := range []string{ChangedEntries, ChangedCollectors} {
		if s.pending[kind] && (kind != ChangedCollectors || s.owner) {
			kinds = append(kinds, kind)
		}
	}
	clear(s.pending)
	return kinds
}

// ServeHTTP is GET /api/events, a stream of server-sent events: one "event: <kind>" for each kind of
// change. The page reconnects by itself when the stream ends.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}
	// A stream has no write deadline: the server's is for a reply, not for a stream.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Accel-Buffering", "no")
	sub := h.subscribe(auth.Owner(r.Context()))
	defer h.unsubscribe(sub)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	keepAlive := time.NewTicker(25 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			fmt.Fprint(w, ": keep-alive\n\n")
		case <-sub.wake:
			for _, kind := range sub.take(h) {
				fmt.Fprintf(w, "event: %s\ndata: 1\n\n", kind)
			}
		}
		flusher.Flush()
	}
}

// Notify publishes after a request that changed something has succeeded: a collector's write, or the
// owner's edit. Other requests only read.
func (h *Hub) Notify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.status >= 300 {
			return
		}
		if kind := changeOf(r.URL.Path); kind != "" {
			h.Publish(kind)
		}
	})
}

// changeOf is what a write to the path changes; "" when nothing a page shows.
func changeOf(path string) string {
	switch {
	case path == "/collect/known", path == "/collect/status", path == "/admin/fetch":
		// A question about what is stored, not a write; a device's report is announced by the handler, when it says something new; a want changes nothing a page shows until a device has read the post.
		return ""
	case strings.HasPrefix(path, "/collect/"), strings.HasPrefix(path, "/admin/"):
		return ChangedEntries
	}
	return ""
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the writer underneath.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
