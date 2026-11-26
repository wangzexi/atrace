// Wants: the owner opens a post and a device is told that someone wants it read again. The server schedules nothing:
// it keeps a short list of wanted posts in memory and tells the devices that connect, and a device decides whether
// and when to read. A want that no device takes in wantTTL is forgotten; opening the post again asks again.
package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// wantTTL is how long a want waits for a device, and how long after a want the same post is not asked for again.
	wantTTL = 10 * time.Minute
	// maxWants bounds the list: an owner opening posts faster than a device reads them does not grow it.
	maxWants = 200
)

// Want is a post the owner opened: its ID on the platform ("x:post:123") and the platform's name.
type Want struct {
	Post     string `json:"post"`
	Platform string `json:"platform"`
}

type wantEntry struct {
	Want
	at    time.Time
	taken bool
}

type wantSubscriber struct {
	platforms map[string]bool
	wake      chan struct{}
}

// Wants is the list of posts wanted and the devices listening for them.
type Wants struct {
	mu   sync.Mutex
	now  func() time.Time
	list []*wantEntry
	subs map[*wantSubscriber]struct{}
}

func NewWants(now func() time.Time) *Wants {
	if now == nil {
		now = time.Now
	}
	return &Wants{now: now, subs: map[*wantSubscriber]struct{}{}}
}

// platformOf is the platform of a post ID: the part before the first colon.
func platformOf(post string) string {
	platform, _, _ := strings.Cut(post, ":")
	return platform
}

// Add records that the post is wanted and wakes the devices. It reports false when the same post was asked for
// within wantTTL: a device has it, or will.
func (w *Wants) Add(post string) bool {
	platform := platformOf(post)
	if platform == "" || platform == post {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	kept := w.list[:0]
	for _, e := range w.list {
		if now.Sub(e.at) < wantTTL {
			kept = append(kept, e)
		}
	}
	w.list = kept
	for _, e := range w.list {
		if e.Post == post {
			return false
		}
	}
	if len(w.list) >= maxWants {
		return false
	}
	w.list = append(w.list, &wantEntry{Want: Want{Post: post, Platform: platform}, at: now})
	for s := range w.subs {
		if s.platforms[platform] {
			select {
			case s.wake <- struct{}{}:
			default:
			}
		}
	}
	return true
}

// take hands a device the wants of its platforms that no device has taken yet.
func (w *Wants) take(platforms map[string]bool) []Want {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	var out []Want
	for _, e := range w.list {
		if !e.taken && platforms[e.Platform] && now.Sub(e.at) < wantTTL {
			e.taken = true
			out = append(out, e.Want)
		}
	}
	return out
}

func (w *Wants) subscribe(platforms map[string]bool) *wantSubscriber {
	s := &wantSubscriber{platforms: platforms, wake: make(chan struct{}, 1)}
	w.mu.Lock()
	w.subs[s] = struct{}{}
	w.mu.Unlock()
	return s
}

func (w *Wants) unsubscribe(s *wantSubscriber) {
	w.mu.Lock()
	delete(w.subs, s)
	w.mu.Unlock()
}

// serveWants is GET /collect/wants?platforms=x,weibo, a stream of server-sent events: one "event: want" for each
// wanted post of the platforms the device has. What is already waiting is sent first, so a device that was away
// finds it. The device reconnects by itself when the stream ends.
func (a *API) serveWants(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}
	platforms := map[string]bool{}
	for _, name := range strings.Split(r.URL.Query().Get("platforms"), ",") {
		if name = strings.TrimSpace(name); name != "" && len(platforms) < maxDeviceCollector {
			platforms[name] = true
		}
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Accel-Buffering", "no")
	sub := a.wants.subscribe(platforms)
	defer a.wants.unsubscribe(sub)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()
	send := func() {
		for _, want := range a.wants.take(platforms) {
			data, _ := json.Marshal(want)
			fmt.Fprintf(w, "event: want\ndata: %s\n\n", data)
		}
		flusher.Flush()
	}
	send()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case <-sub.wake:
			send()
		}
	}
}

// fetchPost is POST /admin/fetch {"ref": "<entry ref>"}: the owner asks for the post to be read again now.
func (a *API) fetchPost(w http.ResponseWriter, r *http.Request) {
	body, e := readJSON(w, r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req struct {
		Ref string `json:"ref"`
	}
	if json.Unmarshal(body, &req) != nil || req.Ref == "" {
		writeError(w, badf("ref is required"))
		return
	}
	post, e := a.store.PostOfEntry(r.Context(), req.Ref)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"asked": a.wants.Add(post)})
}
