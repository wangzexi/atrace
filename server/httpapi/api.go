package httpapi

// The API type, its routes and the shared HTTP helpers.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"

	"git.zexi.me/zexi/atrace/server/core"
)

const maxJSONBody = 2 << 20
const maxFileBody = 1 << 30

type API struct {
	store   *core.Store
	hub     *Hub
	site    Site
	devices *Devices
	wants   *Wants
}

// RegisterRoutes serves the API. The collectors write through /collect and tell the server how they are doing; the
// server asks them for nothing. hub tells the pages what changed.
func RegisterRoutes(m *http.ServeMux, store *core.Store, hub *Hub, site Site) {
	a := &API{store: store, hub: hub, site: site, devices: NewDevices(nil), wants: NewWants(nil)}
	m.Handle("GET /api/events", hub)
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]bool{"ok": true}) })
	// Collectors write through /collect. The owner reads and writes under /admin; visitors read the pages, /index.md,
	// and /<ref>.md.
	m.HandleFunc("POST /collect/ingest", a.ingest)
	m.HandleFunc("POST /collect/known", a.known)
	m.HandleFunc("POST /collect/status", a.reportDevice)
	m.HandleFunc("GET /collect/wants", a.serveWants)
	m.HandleFunc("GET /collect/media", a.getMedia)
	m.HandleFunc("PUT /collect/media", a.putMedia)
	// An archived file is public while a visible entry displays it.
	m.HandleFunc("GET /files/{ref}", a.getFile)
	m.HandleFunc("GET /api/site", a.siteInfo)
	// The owner's management is all under /admin/, and only the owner may use any of it.
	m.HandleFunc("PUT /admin/settings", a.setSiteTitle)
	m.HandleFunc("PUT /admin/settings/footer", a.setSiteFooter)
	m.HandleFunc("PUT /admin/owner", a.setOwner)
	m.HandleFunc("GET /admin/devices", a.listDevices)
	m.HandleFunc("GET /admin/people", a.listPeople)
	m.HandleFunc("POST /admin/people", a.createPerson)
	m.HandleFunc("PUT /admin/people/order", a.orderPeople)
	m.HandleFunc("GET /admin/people/of", a.personOfAccount)
	m.HandleFunc("PUT /admin/people/{id}", a.updatePerson)
	m.HandleFunc("DELETE /admin/people/{id}", a.deletePerson)
	m.HandleFunc("POST /admin/fetch", a.fetchPost)
	m.HandleFunc("PUT /admin/attachments", a.replaceArchiveAttachments)
	m.HandleFunc("PATCH /admin/entry/{ref}", a.patchEntry)
}

func readJSON(w http.ResponseWriter, r *http.Request) (json.RawMessage, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	b, e := io.ReadAll(r.Body)
	if e != nil {
		var maxErr *http.MaxBytesError
		if errors.As(e, &maxErr) {
			return nil, badf("JSON body exceeds 2 MiB")
		}
		return nil, e
	}
	return b, nil
}

func errorBody(code, msg string) map[string]any {
	return map[string]any{"error": map[string]string{"code": code, "message": msg}}
}

func writeError(w http.ResponseWriter, e error) {
	var requestErr *requestError
	if core.IsInputError(e) || errors.As(e, &requestErr) {
		writeJSON(w, 400, errorBody("invalid_argument", e.Error()))
		return
	}
	if errors.Is(e, sql.ErrNoRows) {
		writeJSON(w, 404, errorBody("not_found", "resource not found"))
		return
	}
	log.Printf("HTTP API internal error: %v", e)
	writeJSON(w, 500, errorBody("internal", "internal server error"))
}

type requestError struct{ message string }

func (e *requestError) Error() string { return e.message }

func badf(format string, args ...any) error {
	return &requestError{message: fmt.Sprintf(format, args...)}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
