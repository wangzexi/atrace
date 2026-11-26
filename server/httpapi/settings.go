package httpapi

// The owner's settings: the site's title. Who the owner is, is one of the people (people.go).

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"git.zexi.me/zexi/atrace/server/auth"
)

// Site is what the server is configured with: the name of the owner, which heads every entry, and the title
// of the site until the owner sets one.
type Site struct {
	Owner        string
	DefaultTitle string
}

// siteInfo is the title for everyone and whether the viewer is the owner.
func (a *API) siteInfo(w http.ResponseWriter, r *http.Request) {
	title, err := a.store.SiteTitle(r.Context(), a.site.DefaultTitle)
	if err != nil {
		http.Error(w, "could not read site settings", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		Title string `json:"title"`
		Owner bool   `json:"owner"`
	}{title, auth.Owner(r.Context())})
}

func (a *API) setSiteTitle(w http.ResponseWriter, r *http.Request) {
	if !auth.Owner(r.Context()) {
		http.Error(w, "login required", http.StatusUnauthorized)
		return
	}
	var input struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		http.Error(w, "invalid settings", http.StatusBadRequest)
		return
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" || utf8.RuneCountInString(input.Title) > 80 {
		http.Error(w, "invalid title", http.StatusBadRequest)
		return
	}
	if err := a.store.SetSiteTitle(r.Context(), input.Title); err != nil {
		http.Error(w, "could not save site settings", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
