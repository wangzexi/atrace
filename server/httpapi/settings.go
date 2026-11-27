package httpapi

// The owner's settings: the site's title and footer.

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

// siteInfo includes public site settings and whether the viewer is the owner.
func (a *API) siteInfo(w http.ResponseWriter, r *http.Request) {
	title, err := a.store.SiteTitle(r.Context(), a.site.DefaultTitle)
	if err != nil {
		http.Error(w, "could not read site settings", http.StatusInternalServerError)
		return
	}
	footer, err := a.store.SiteFooter(r.Context())
	if err != nil {
		http.Error(w, "could not read site settings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Title  string `json:"title"`
		Owner  bool   `json:"owner"`
		Footer string `json:"footer,omitempty"`
	}{Title: title, Owner: auth.Owner(r.Context()), Footer: footer})
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

func (a *API) setSiteFooter(w http.ResponseWriter, r *http.Request) {
	if !auth.Owner(r.Context()) {
		http.Error(w, "login required", http.StatusUnauthorized)
		return
	}
	var input struct {
		HTML *string `json:"html"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)).Decode(&input); err != nil || input.HTML == nil {
		http.Error(w, "页脚设置无效。", http.StatusBadRequest)
		return
	}
	footer, err := sanitizeFooter(*input.HTML)
	if err != nil || utf8.RuneCountInString(footer) > 4096 {
		http.Error(w, "页脚 HTML 无效或超过 4096 个字符。", http.StatusBadRequest)
		return
	}
	if err := a.store.SetSiteFooter(r.Context(), footer); err != nil {
		http.Error(w, "could not save site settings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"html": footer})
}
