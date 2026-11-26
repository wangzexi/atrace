// People: /admin/people, the owner's notes on who is who. A person is a name and the accounts, on any platform,
// that are theirs; their timeline is read from /api/entries?person=<id>. Only the owner reaches these paths.
package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type personBody struct {
	Name     string   `json:"name"`
	Accounts []string `json:"accounts"`
}

func readPerson(w http.ResponseWriter, r *http.Request) (personBody, bool) {
	body, e := readJSON(w, r)
	if e != nil {
		writeError(w, e)
		return personBody{}, false
	}
	var req personBody
	if json.Unmarshal(body, &req) != nil {
		writeError(w, badf("name must be a string and accounts a list of account IDs"))
		return personBody{}, false
	}
	req.Name = strings.TrimSpace(req.Name)
	return req, true
}

func personID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, badf("id must be a person's ID"))
		return 0, false
	}
	return id, true
}

// listPeople is everyone with their accounts, the owner first, and the accounts the devices report that are nobody's
// yet: what a person may be given.
func (a *API) listPeople(w http.ResponseWriter, r *http.Request) {
	people, e := a.store.People(r.Context())
	if e != nil {
		writeError(w, e)
		return
	}
	offered, e := a.store.OfferedAccounts(r.Context(), a.devices.Accounts())
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": people, "offered": offered})
}

// setOwner is PUT /admin/owner {"person": <id>}: which of the people the owner is.
func (a *API) setOwner(w http.ResponseWriter, r *http.Request) {
	body, e := readJSON(w, r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req struct {
		Person int64 `json:"person"`
	}
	if json.Unmarshal(body, &req) != nil || req.Person < 1 {
		writeError(w, badf("person must be a person's ID"))
		return
	}
	if e := a.store.SetOwner(r.Context(), req.Person); e != nil {
		writeError(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) createPerson(w http.ResponseWriter, r *http.Request) {
	req, ok := readPerson(w, r)
	if !ok {
		return
	}
	id, e := a.store.CreatePerson(r.Context(), req.Name, req.Accounts)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *API) updatePerson(w http.ResponseWriter, r *http.Request) {
	id, ok := personID(w, r)
	if !ok {
		return
	}
	req, ok := readPerson(w, r)
	if !ok {
		return
	}
	if e := a.store.UpdatePerson(r.Context(), id, req.Name, req.Accounts); e != nil {
		writeError(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deletePerson(w http.ResponseWriter, r *http.Request) {
	id, ok := personID(w, r)
	if !ok {
		return
	}
	if e := a.store.DeletePerson(r.Context(), id); e != nil {
		writeError(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// personOfAccount answers which person an account belongs to (0 for none), for the page to open a name's timeline.
func (a *API) personOfAccount(w http.ResponseWriter, r *http.Request) {
	account := r.URL.Query().Get("account")
	if account == "" {
		writeError(w, badf("account must be an account ID"))
		return
	}
	id, e := a.store.PersonOfAccount(r.Context(), account)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// orderPeople saves the order and its first person as the default timeline.
func (a *API) orderPeople(w http.ResponseWriter, r *http.Request) {
	body, err := readJSON(w, r)
	if err != nil {
		writeError(w, err)
		return
	}
	var req struct {
		People []int64 `json:"people"`
	}
	if json.Unmarshal(body, &req) != nil {
		writeError(w, badf("people must be a list of person IDs"))
		return
	}
	if err := a.store.OrderPeople(r.Context(), req.People); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
