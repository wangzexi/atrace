// Routes: the HTTP handlers of the page and its JSON, and the paging of the timeline.
package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"git.zexi.me/zexi/atrace/server/auth"
	"git.zexi.me/zexi/atrace/server/core"
)

type FeedQuery interface {
	ArchiveEntries(context.Context, bool, string) ([]core.ArchiveEntry, error)
	ArchiveAttachmentPaths(context.Context, string) ([]string, error)
	ArchiveAttachmentText(context.Context, string, string) (string, error)
	LatestRaw(context.Context, string) (json.RawMessage, error)
	OpenFile(string) (io.ReadCloser, error)
	SiteTitle(context.Context, string) (string, error)
	TimelinePage(context.Context, int64, []string, int, bool, string, bool, core.PageRange) ([]core.Item, error)
	TimelineItem(context.Context, string, bool) (core.Item, error)
	MediaRefs(context.Context, []string) (map[string]string, error)
	Object(context.Context, string, bool) (core.ObjectRecord, error)
	Replies(context.Context, string) ([]core.ObjectRecord, error)
	ObjectEntry(context.Context, string) (core.EntryState, error)
	OwnerNameForAccount(context.Context, string) (string, error)
	PersonNameForAccount(context.Context, string) (string, error)
}

type Renderer struct {
	feed FeedQuery
	// owner is the name every entry is headed with: the owner of the archive has one
	// name here, whatever each platform calls them.
	owner      string
	dev        bool
	boot       string
	web        fs.FS
	shellSlots chan struct{}
}

// RegisterRoutes serves the page and the entries it shows. owner is the name every entry is headed with.
func RegisterRoutes(m *http.ServeMux, feed FeedQuery, owner string) {
	renderer := &Renderer{feed: feed, owner: owner, dev: os.Getenv("ATRACE_DEV") != "", boot: strconv.FormatInt(time.Now().UnixNano(), 10), web: webBuild(), shellSlots: make(chan struct{}, 4)}
	// The page and its data are for everyone; what a visitor gets is filtered by who asks.
	// /admin/* is the owner's, see package auth.
	m.HandleFunc("GET /{$}", renderer.home)
	m.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Robots-Tag", "noindex")
		if auth.Owner(r.Context()) {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		renderer.page(w, r)
	})
	m.HandleFunc("GET /settings/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/settings/", "/settings/site/", "/settings/people/", "/settings/devices/", "/settings/password/":
		default:
			http.NotFound(w, r)
			return
		}
		if !auth.Owner(r.Context()) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		renderer.page(w, r)
	})
	m.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/settings/", http.StatusFound)
	})
	// The timeline as Markdown, which is also where it is searched and paged.
	m.HandleFunc("GET /index.md", renderer.timelineMarkdown)
	m.HandleFunc("POST /api/shell", renderer.shell)
	m.HandleFunc("GET /api/archive-file", renderer.archiveDownload)

	// A post's page is /<ref>; the front end routes it. /<ref>.md is the same post as Markdown.
	m.HandleFunc("GET /{ref}", func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if name, ok := strings.CutSuffix(ref, ".md"); ok && refPattern.MatchString(name) {
			renderer.postMarkdown(w, r, name)
			return
		}
		if !refPattern.MatchString(ref) {
			http.NotFound(w, r)
			return
		}
		// A page for an entry that is not there, or that this viewer may not see, is a 404.
		if _, err := renderer.feed.TimelineItem(r.Context(), ref, !auth.Owner(r.Context())); errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		alternate(w, "/"+ref+".md")
		markdown, acceptable := pageRepresentation(r)
		if !acceptable {
			http.Error(w, "no acceptable representation; request text/html or text/markdown", http.StatusNotAcceptable)
			return
		}
		if markdown {
			renderer.postMarkdown(w, r, ref)
			return
		}
		renderer.page(w, r)
	})
	m.HandleFunc("GET /assets/{file}", renderer.asset)
	m.HandleFunc("GET /api/entries", renderer.entries)
	m.HandleFunc("GET /api/entry", renderer.entry)
	if renderer.dev {
		// The page polls this and reloads when the process was replaced.
		m.HandleFunc("GET /dev/boot", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(renderer.boot))
		})
	}
}

// refPattern is what a ref or an alias looks like: a ref is eight letters or digits, an alias 3 to 40 characters with hyphens.
var refPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{1,38}[A-Za-z0-9]$`)

// window builds the entries of one page of the timeline after a cursor, at most limit of them.
// Entries whose post was never collected are skipped, so a window can show
// fewer than limit entries; next is where the following window starts.
// A visitor gets only the public entries, without the internal activity ID.
func (renderer *Renderer) window(ctx context.Context, person int64, page core.PageRange, limit int, kinds []string, text string, regex, withReplies bool) (cards []Entry, next string, more bool, err error) {
	owner := auth.Owner(ctx)
	items, err := renderer.feed.TimelinePage(ctx, person, kinds, limit+1, !owner, text, regex, page)
	if err != nil {
		return nil, "", false, err
	}
	if more = len(items) > limit; more {
		items = items[:limit]
	}
	names := map[string]string{}
	for _, item := range items {
		if item.Activity.Type != "Announce" && item.Object == nil {
			continue // the post itself was never collected
		}
		entry, err := renderer.entryOf(ctx, item, names, withReplies)
		if err != nil {
			return nil, "", false, fmt.Errorf("timeline item %s: %w", item.Activity.Object, err)
		}
		cards = append(cards, visibleTo(owner, entry))
	}
	if more && len(items) > 0 {
		next = items[len(items)-1].Ref
	}
	return cards, next, more, nil
}

// webBuild locates the independent Web build next to the source tree, or at the configured deployment path.
func webBuild() fs.FS {
	if dir := os.Getenv("ATRACE_WEB_BUILD"); dir != "" {
		return os.DirFS(dir)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "..", "..", "client", "web", "build")
		if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
			return os.DirFS(dir)
		}
	}
	for _, dir := range []string{"client/web/build", "../client/web/build"} {
		if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
			return os.DirFS(dir)
		}
	}
	return os.DirFS("client/web/build")
}

// home is the timeline: the page, or its Markdown for a request that asks for that.
func (renderer *Renderer) home(w http.ResponseWriter, r *http.Request) {
	alternate(w, "/index.md")
	markdown, acceptable := pageRepresentation(r)
	if !acceptable {
		http.Error(w, "no acceptable representation; request text/html or text/markdown", http.StatusNotAcceptable)
		return
	}
	if markdown {
		renderer.timelineMarkdown(w, r)
		return
	}
	renderer.page(w, r)
}

// page serves the Web build.
func (renderer *Renderer) page(w http.ResponseWriter, r *http.Request) {
	index, err := fs.ReadFile(renderer.web, "index.html")
	if err != nil {
		http.Error(w, "front end not built", http.StatusServiceUnavailable)
		return
	}
	title, err := renderer.feed.SiteTitle(r.Context(), renderer.owner+"'s Notes")
	if err != nil {
		http.Error(w, "could not read site settings", http.StatusInternalServerError)
		return
	}
	index = []byte(strings.Replace(string(index), "<title></title>", "<title>"+html.EscapeString(title)+"</title>", 1))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(index)
}

// asset serves the hashed files of the built front end.
func (renderer *Renderer) asset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	data, err := fs.ReadFile(renderer.web, "assets/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(name)))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(data)
}

// entries serves the next entries as JSON for the front end, in standard fields: the page draws them.
func (renderer *Renderer) entries(w http.ResponseWriter, r *http.Request) {
	page, limit, text, regex, kinds, problem := entryListParams(r.URL.Query())
	if problem != "" {
		http.Error(w, problem, http.StatusBadRequest)
		return
	}
	// Whose timeline: the owner's by default. Other people are the owner's notes and are not for visitors.
	var person int64
	if raw := r.URL.Query().Get("person"); raw != "" {
		var err error
		if person, err = strconv.ParseInt(raw, 10, 64); err != nil || person < 1 {
			http.Error(w, "person must be a person's ID", http.StatusBadRequest)
			return
		}
		if !auth.Owner(r.Context()) {
			http.NotFound(w, r)
			return
		}
	}
	cards, next, more, err := renderer.window(r.Context(), person, page, limit, kinds, text, regex, true)
	if err != nil {
		if errors.Is(err, core.ErrCursor) {
			http.Error(w, err.Error(), 400)
			return
		}
		log.Printf("entries query failed: %v", err)
		http.Error(w, "entries unavailable", http.StatusInternalServerError)
		return
	}
	if cards == nil {
		cards = []Entry{}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"entries": cards, "next": next, "more": more, "owner": auth.Owner(r.Context())})
}

// entry serves one entry as JSON by its ref: for its own page, and to refresh it when the page is told it changed.
func (renderer *Renderer) entry(w http.ResponseWriter, r *http.Request) {
	ref := r.URL.Query().Get("ref")
	if ref == "" {
		http.Error(w, "ref is required", http.StatusBadRequest)
		return
	}
	owner := auth.Owner(r.Context())
	w.Header().Set("Cache-Control", "no-store")
	item, err := renderer.feed.TimelineItem(r.Context(), ref, !owner)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	var entry Entry
	if err == nil {
		entry, err = renderer.entryOf(r.Context(), item, map[string]string{}, true)
	}
	if err != nil {
		if r.Context().Err() == nil {
			log.Printf("entry %s failed: %v", ref, err)
			http.Error(w, "entry unavailable", http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(visibleTo(owner, entry))
}
