package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"git.zexi.me/zexi/atrace/server/auth"
)

// servePeople is a server where a request is the owner's unless it says ?visitor.
func servePeople(t *testing.T) string {
	t.Helper()
	srv := serve(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Has("visitor") {
				next.ServeHTTP(w, r)
				return
			}
			auth.AsOwner(next).ServeHTTP(w, r)
		})
	})
	for _, doc := range []string{
		`{"type":"Person","id":"weibo:user:5","name":"阿明"}`,
		`{"type":"Note","id":"weibo:post:1","attributedTo":"weibo:user:1","content":"my post"}`,
		`{"type":"Create","actor":"weibo:user:1","object":"weibo:post:1","published":"2026-11-10T00:00:00Z"}`,
		`{"type":"Note","id":"weibo:comment:1","attributedTo":"weibo:user:5","inReplyTo":"weibo:post:1","content":"nice one","published":"2026-11-10T01:00:00Z"}`,
		`{"type":"Create","actor":"weibo:user:5","object":"weibo:comment:1","published":"2026-11-10T01:00:00Z"}`,
		`{"type":"Note","id":"x:post:5","attributedTo":"x:user:5","content":"over on x","published":"2026-11-11T00:00:00Z"}`,
		`{"type":"Create","actor":"x:user:5","object":"x:post:5","published":"2026-11-11T00:00:00Z"}`,
	} {
		if status, body := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`); status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, body)
		}
	}
	return srv.URL
}

func TestPeopleAreTheOwnersToManage(t *testing.T) {
	t.Setenv("ATRACE_COLLECT_TOKEN", "collector secret")
	store := newStore(t, "open sesame")
	login := auth.New(store)
	srv := serveStore(t, store, login.Middleware, true)
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/admin/people", ""},
		{"POST", "/admin/people", `{"name":"Ming","accounts":[]}`},
		{"PUT", "/admin/people/1", `{"name":"Ming","accounts":[]}`},
		{"DELETE", "/admin/people/1", ""},
		{"GET", "/admin/people/of?account=weibo:user:5", ""},
	} {
		if status, _ := do(t, c.method, srv.URL+c.path, c.body); status != http.StatusUnauthorized {
			t.Errorf("a visitor %s %s = %d, want 401", c.method, c.path, status)
		}
	}
	if status, _ := do(t, "GET", srv.URL+"/api/entries?person=1", ""); status != http.StatusNotFound {
		t.Errorf("a visitor asking for a person's timeline = %d, want 404", status)
	}
}

// entryIDs are the posts of the entries a timeline answered with.
func entryIDs(t *testing.T, body string) []string {
	t.Helper()
	var page struct{ Entries []struct{ ID string } }
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("entries: %s (%v)", body, err)
	}
	ids := []string{}
	for _, e := range page.Entries {
		ids = append(ids, e.ID)
	}
	return ids
}

func TestAPersonsTimelineIsTheirAccountsEntries(t *testing.T) {
	url := servePeople(t)
	status, body := do(t, "POST", url+"/admin/people", `{"name":"Ming (classmate)","accounts":["weibo:user:5","x:user:5"]}`)
	if status != http.StatusCreated {
		t.Fatalf("create: %d %s", status, body)
	}
	var created struct{ ID int64 }
	if err := json.Unmarshal([]byte(body), &created); err != nil || created.ID < 1 {
		t.Fatalf("created = %s (%v)", body, err)
	}

	_, body = do(t, "GET", url+"/admin/people", "")
	var list struct {
		People []struct {
			ID       int64
			Name     string
			Owner    bool
			Accounts []struct{ ID, Name string }
		}
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list.People) != 2 || !list.People[0].Owner || len(list.People[1].Accounts) != 2 || list.People[1].Accounts[0].Name != "阿明" {
		t.Fatalf("people = %s (%v)", body, err)
	}
	if _, body = do(t, "GET", url+"/admin/people/of?account=x:user:5", ""); !strings.Contains(body, `"id":`+strconvID(created.ID)) {
		t.Errorf("who owns x:user:5: %s", body)
	}

	// The default timeline is the owner's; a person's is selected by id.
	_, mine := do(t, "GET", url+"/api/entries", "")
	if ids := entryIDs(t, mine); len(ids) != 1 || ids[0] != "weibo:post:1" {
		t.Errorf("the owner's timeline = %v, want only their own post", ids)
	}
	_, theirs := do(t, "GET", url+"/api/entries?person="+strconvID(created.ID), "")
	if ids := entryIDs(t, theirs); len(ids) != 2 || ids[0] != "x:post:5" || ids[1] != "weibo:comment:1" {
		t.Errorf("Ming's timeline = %v, want both accounts' entries, newest first", ids)
	}
	// An entry on a person's timeline opens on its own page like any other.
	var theirPage struct{ Entries []struct{ Ref string } }
	if err := json.Unmarshal([]byte(theirs), &theirPage); err != nil || len(theirPage.Entries) == 0 {
		t.Fatalf("Ming's entries: %s (%v)", theirs, err)
	}
	if status, body := do(t, "GET", url+"/api/entry?ref="+theirPage.Entries[0].Ref, ""); status != http.StatusOK || !strings.Contains(body, "over on x") {
		t.Errorf("opening one of Ming's entries = %d %s", status, body)
	}
	// The names the owner gives people are not for visitors, nor are the timelines they make.
	if status, _ := do(t, "GET", url+"/api/entries?visitor&person="+strconvID(created.ID), ""); status != http.StatusNotFound {
		t.Errorf("a visitor asking for a person's timeline = %d, want 404", status)
	}
	if status, body := do(t, "GET", url+"/api/entries?person=abc", ""); status != http.StatusBadRequest {
		t.Errorf("person=abc = %d %s", status, body)
	}

	if status, b := do(t, "PUT", url+"/admin/people/"+strconvID(created.ID), `{"name":"Ming","accounts":["weibo:user:5"]}`); status != http.StatusNoContent {
		t.Fatalf("update: %d %s", status, b)
	}
	if _, theirs = do(t, "GET", url+"/api/entries?person="+strconvID(created.ID), ""); len(entryIDs(t, theirs)) != 1 {
		t.Errorf("an account taken off a person is still on their timeline: %s", theirs)
	}
	if status, b := do(t, "DELETE", url+"/admin/people/"+strconvID(created.ID), ""); status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", status, b)
	}
	if status, _ := do(t, "DELETE", url+"/admin/people/"+strconvID(list.People[0].ID), ""); status != http.StatusBadRequest {
		t.Errorf("deleting the owner = %d, want 400", status)
	}
	if status, _ := do(t, "PUT", url+"/admin/people/999", `{"name":"x","accounts":[]}`); status != http.StatusNotFound {
		t.Errorf("updating nobody = %d, want 404", status)
	}
	if status, _ := do(t, "POST", url+"/admin/people", `{"name":"Ghost","accounts":["x:user:404"]}`); status != http.StatusBadRequest {
		t.Errorf("an account nobody has seen = %d, want 400", status)
	}
}

func strconvID(id int64) string { return strconv.FormatInt(id, 10) }

// The accounts behind the names let the owner open a person from a name; a visitor is not given them.
func TestEntriesNameTheirAccountsToTheOwnerOnly(t *testing.T) {
	url := servePeople(t)
	_, list := do(t, "GET", url+"/api/entries", "")
	var page struct {
		Entries []struct {
			Ref string `json:"ref"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(list), &page); err != nil || len(page.Entries) == 0 {
		t.Fatalf("entries = %s (%v)", list, err)
	}
	if status, body := do(t, "PATCH", url+"/admin/entry/"+page.Entries[0].Ref, `{"public":true}`); status != http.StatusOK && status != http.StatusNoContent {
		t.Fatalf("make public: %d %s", status, body)
	}
	_, mine := do(t, "GET", url+"/api/entries", "")
	for _, want := range []string{`"actorId":"weibo:user:1"`, `"authorId":"weibo:user:5"`} {
		if !strings.Contains(mine, want) {
			t.Errorf("the owner's entries lack %s: %s", want, mine)
		}
	}
	_, visitors := do(t, "GET", url+"/api/entries?visitor", "")
	if !strings.Contains(visitors, "my post") || strings.Contains(visitors, "actorId") || strings.Contains(visitors, "authorId") {
		t.Errorf("a visitor's entries: %s", visitors)
	}
}

func TestDevicesReportThemselvesAndOnlyTheOwnerReadsThem(t *testing.T) {
	t.Setenv("ATRACE_COLLECT_TOKEN", "collector secret")
	store := newStore(t, "open sesame")
	login := auth.New(store)
	srv := serveStore(t, store, login.Middleware, true)
	report := `{"device":"desk","collectors":[{"name":"weibo","account":"weibo:user:1","state":"ok","lastRun":"2026-11-24T00:00:00Z","message":"fine"}]}`

	if status, _ := collect(t, srv.URL+"/collect/status", "", report); status != http.StatusUnauthorized {
		t.Errorf("a report without the collect token = %d, want 401", status)
	}
	if status, body := collect(t, srv.URL+"/collect/status", "collector secret", report); status != http.StatusNoContent {
		t.Fatalf("report = %d %s", status, body)
	}
	for _, bad := range []string{
		`{"device":"","collectors":[]}`,
		`{"device":"desk","collectors":[{"name":"","state":"ok"}]}`,
		`{"device":"desk","collectors":[{"name":"weibo","message":"two\nlines"}]}`,
		`not json`,
	} {
		if status, _ := collect(t, srv.URL+"/collect/status", "collector secret", bad); status != http.StatusBadRequest {
			t.Errorf("report %q = %d, want 400", bad, status)
		}
	}
	// What the page shows is for the owner.
	if status, _ := do(t, "GET", srv.URL+"/admin/devices", ""); status != http.StatusUnauthorized {
		t.Errorf("a visitor reading devices = %d, want 401", status)
	}
}
