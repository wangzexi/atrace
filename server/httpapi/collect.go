package httpapi

// What collectors call: ingest, the question what is already stored, and media.

import (
	"encoding/json"
	"errors"
	"net/http"

	"git.zexi.me/zexi/atrace/server/core"
)

// ingest accepts one ActivityStreams document with its observation.
func (a *API) ingest(w http.ResponseWriter, r *http.Request) {
	body, e := readJSON(w, r)
	if e != nil {
		writeError(w, e)
		return
	}
	result, e := a.store.Ingest(r.Context(), body)
	if e != nil {
		writeError(w, e)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}

// getMedia tells a collector whether a platform media URL already has a local
// copy, so it can skip downloading it again.
func (a *API) getMedia(w http.ResponseWriter, r *http.Request) {
	ref, mime, ok, e := a.store.MediaCopy(r.Context(), r.URL.Query().Get("url"))
	if e != nil {
		writeError(w, e)
		return
	}
	if !ok {
		writeJSON(w, 404, errorBody("not_found", "no local copy of this URL"))
		return
	}
	writeJSON(w, 200, map[string]string{"ref": ref, "mime": mime})
}

// putMedia stores the request body as a content-addressed file and registers it
// as the local copy of the platform media URL given in url.
func (a *API) putMedia(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("url") == "" {
		writeError(w, badf("url is required"))
		return
	}
	if r.ContentLength > maxFileBody {
		writeJSON(w, 413, errorBody("too_large", "file exceeds 1 GiB"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFileBody)
	ref, e := a.store.PutFile(r.Context(), r.Body, r.URL.Query().Get("url"), r.Header.Get("Content-Type"))
	if e != nil {
		var maxErr *http.MaxBytesError
		if errors.As(e, &maxErr) {
			writeJSON(w, 413, errorBody("too_large", "file exceeds 1 GiB"))
			return
		}
		writeError(w, e)
		return
	}
	writeJSON(w, 201, map[string]string{"ref": ref})
}

// maxKnown is how many activities one question may ask about.
const maxKnown = 500

// known tells a collector which of the activities it has just read are already stored, in the order it
// asked. It writes nothing.
func (a *API) known(w http.ResponseWriter, r *http.Request) {
	body, e := readJSON(w, r)
	if e != nil {
		writeError(w, e)
		return
	}
	var request struct {
		Items []struct {
			Actor  string `json:"actor"`
			Kind   string `json:"kind"`
			Object string `json:"object"`
			Target string `json:"target"`
			ID     string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		writeError(w, badf("the body is not JSON of the form {\"items\": [...]}"))
		return
	}
	if len(request.Items) > maxKnown {
		writeError(w, badf("at most %d items", maxKnown))
		return
	}
	queries := make([]core.KnownQuery, len(request.Items))
	for i, item := range request.Items {
		switch item.Kind {
		case "Create", "Like", "Announce", "Add":
		default:
			writeError(w, badf("item %d: kind must be Create, Like, Announce or Add", i))
			return
		}
		if item.Object == "" && item.ID == "" {
			writeError(w, badf("item %d: object or id is required", i))
			return
		}
		queries[i] = core.KnownQuery{Actor: item.Actor, Kind: item.Kind, Object: item.Object, Target: item.Target, ID: item.ID}
	}
	known, err := a.store.Known(r.Context(), queries)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"known": known})
}
