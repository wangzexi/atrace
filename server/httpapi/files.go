package httpapi

// Serving stored files.

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"git.zexi.me/zexi/atrace/server/auth"
)

func (a *API) getFile(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("ref")
	if !auth.Owner(r.Context()) {
		public, err := a.store.PublicMedia(r.Context(), ref)
		if err != nil {
			writeError(w, err)
			return
		}
		if !public {
			http.NotFound(w, r)
			return
		}
	}
	f, e := a.store.OpenFile(ref)
	if errors.Is(e, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if e != nil {
		writeError(w, e)
		return
	}
	defer f.Close()
	body, e := io.ReadAll(f)
	if e != nil {
		writeError(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", contentType(body))
	// A file is data: an SVG opened on its own address must not run its scripts here.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	http.ServeContent(w, r, "file", time.Time{}, bytes.NewReader(body))
}

// contentType identifies formats Go does not sniff, including SVG and QuickTime.
func contentType(body []byte) string {
	if len(body) >= 12 && string(body[4:8]) == "ftyp" && string(body[8:12]) == "qt  " {
		return "video/quicktime"
	}
	kind := http.DetectContentType(body)
	if strings.HasPrefix(kind, "text/") && bytes.Contains(body[:min(len(body), 1024)], []byte("<svg")) {
		return "image/svg+xml"
	}
	return kind
}
