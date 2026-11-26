package httpapi

// The owner's edits to what the archive shows. One path for each: an entry or a comment is named by its ref.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// patchEntry edits an entry, named by its ref: any of {"hidden", "public", "alias"}, and only the ones given. Hiding is
// soft (hidden=false restores); public=false makes it private again; alias "" takes its address away. A comment is an
// entry too, so the same edit hides it.
func (a *API) patchEntry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := a.store.ActivityByRef(ctx, r.PathValue("ref"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, errorBody("not_found", "no entry has that ref"))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	body, e := readJSON(w, r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req struct {
		Hidden *bool   `json:"hidden"`
		Public *bool   `json:"public"`
		Alias  *string `json:"alias"`
	}
	if json.Unmarshal(body, &req) != nil || (req.Hidden == nil && req.Public == nil && req.Alias == nil) {
		writeError(w, badf("give at least one of hidden (true or false), public (true or false), alias (a string)"))
		return
	}
	changed := map[string]any{"ref": r.PathValue("ref")}
	if req.Alias != nil {
		if e = a.store.SetAlias(ctx, id, strings.TrimSpace(*req.Alias)); e != nil {
			writeError(w, e)
			return
		}
		changed["alias"] = strings.TrimSpace(*req.Alias)
	}
	if req.Public != nil {
		if e = a.store.SetPublic(ctx, id, *req.Public); e != nil {
			writeError(w, e)
			return
		}
		changed["public"] = *req.Public
	}
	if req.Hidden != nil {
		if *req.Hidden {
			e = a.store.HideActivity(ctx, id)
		} else {
			e = a.store.RestoreActivity(ctx, id)
		}
		if e != nil {
			writeError(w, e)
			return
		}
		changed["hidden"] = *req.Hidden
	}
	writeJSON(w, http.StatusOK, changed)
}
