package httpapi

import (
	"encoding/json"
	"net/http"

	"git.zexi.me/zexi/atrace/server/core"
)

// replaceArchiveAttachments syncs the owner's selected text files for a post.
func (a *API) replaceArchiveAttachments(w http.ResponseWriter, r *http.Request) {
	body, err := readJSON(w, r)
	if err != nil {
		writeError(w, err)
		return
	}
	var request struct {
		Object string                   `json:"object"`
		Files  []core.ArchiveAttachment `json:"files"`
	}
	if json.Unmarshal(body, &request) != nil || request.Object == "" || request.Files == nil {
		writeError(w, badf("expected an object ID and a files array"))
		return
	}
	if err := a.store.ReplaceArchiveAttachments(r.Context(), request.Object, request.Files); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
