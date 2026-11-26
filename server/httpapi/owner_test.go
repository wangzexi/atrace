package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"git.zexi.me/zexi/atrace/server/auth"
)

// From a server that has met nobody to an owner timeline: a device reports the accounts its collectors are
// signed in to, the owner gives them to a person, makes that person the one they are, and what those accounts did
// is the timeline.
func TestTheOwnerIsOneOfThePeople(t *testing.T) {
	srv := serveUnbound(t, auth.AsOwner)
	for _, doc := range []string{
		`{"type":"Person","id":"weibo:user:1","name":"微博昵称"}`,
		`{"type":"Person","id":"x:user:2","name":"X 昵称"}`,
		`{"type":"Note","id":"weibo:post:1","attributedTo":"weibo:user:1","content":"one"}`,
		`{"type":"Note","id":"x:post:2","attributedTo":"x:user:2","content":"two"}`,
		`{"type":"Create","actor":"weibo:user:1","object":"weibo:post:1"}`,
		`{"type":"Create","actor":"x:user:2","object":"x:post:2"}`,
	} {
		status, b := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`)
		if status != 200 && status != 201 {
			t.Fatalf("ingest: %d %s", status, b)
		}
	}
	people := srv.URL + "/admin/people"
	type list struct {
		People []struct {
			ID    int64
			Name  string
			Owner bool
		}
		Offered []struct{ ID, Name string }
	}
	read := func() list {
		t.Helper()
		_, body := do(t, "GET", people, "")
		var l list
		if err := json.Unmarshal([]byte(body), &l); err != nil {
			t.Fatalf("%s (%v)", body, err)
		}
		return l
	}
	if l := read(); len(l.People) != 0 || len(l.Offered) != 0 {
		t.Fatalf("a server no device has reported to has nobody and offers nothing: %+v", l)
	}
	check := func(weibo, x string) {
		t.Helper()
		_, body := do(t, "GET", srv.URL+"/api/entries", "")
		var page struct{ Entries []struct{ ID, Actor string } }
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatal(err)
		}
		names := map[string]string{}
		for _, entry := range page.Entries {
			names[entry.ID] = entry.Actor
		}
		if names["weibo:post:1"] != weibo || names["x:post:2"] != x {
			t.Fatalf("names: %v", names)
		}
	}
	// Nobody is the owner until the owner says so, so the owner's timeline is empty.
	check("", "")

	// A device reports what its collectors are signed in to: those accounts are offered, to nobody yet.
	report := `{"device":"desk","collectors":[{"name":"weibo","account":"weibo:user:1","state":"ok"},{"name":"x","account":"x:user:2","state":"ok"}]}`
	if status, b := do(t, "POST", srv.URL+"/collect/status", report); status != http.StatusNoContent {
		t.Fatalf("report: %d %s", status, b)
	}
	if l := read(); len(l.Offered) != 2 || l.Offered[0].Name != "微博昵称" {
		t.Fatalf("offered accounts: %+v", l)
	}

	// A person is made with one of them, and made the owner: only what that account did is the owner's.
	status, b := do(t, "POST", people, `{"name":"Alice","accounts":["weibo:user:1"]}`)
	var created struct{ ID int64 }
	if (status != 200 && status != 201) || json.Unmarshal([]byte(b), &created) != nil {
		t.Fatalf("create: %d %s", status, b)
	}
	owner := srv.URL + "/admin/owner"
	for _, bad := range []string{`{"person":404}`, `{"person":0}`, `{}`, `{"person":"1"}`} {
		if status, _ := do(t, "PUT", owner, bad); status != 400 && status != 404 {
			t.Errorf("%s = %d, want 400 or 404", bad, status)
		}
	}
	if status, b := do(t, "PUT", owner, `{"person":`+strconv.FormatInt(created.ID, 10)+`}`); status != 204 {
		t.Fatalf("make the owner: %d %s", status, b)
	}
	check("Alice", "")
	l := read()
	if len(l.People) != 1 || !l.People[0].Owner || len(l.Offered) != 1 || l.Offered[0].ID != "x:user:2" {
		t.Fatalf("after: %+v; the account that was given is no longer on offer", l)
	}

	// Another person is the owner when the owner says so, and the first is then an ordinary person.
	status, b = do(t, "POST", people, `{"name":"Other","accounts":["x:user:2"]}`)
	var other struct{ ID int64 }
	if (status != 200 && status != 201) || json.Unmarshal([]byte(b), &other) != nil {
		t.Fatalf("create: %d %s", status, b)
	}
	if status, b := do(t, "PUT", owner, `{"person":`+strconv.FormatInt(other.ID, 10)+`}`); status != 204 {
		t.Fatalf("make the other the owner: %d %s", status, b)
	}
	check("", "Other")
	if l = read(); !l.People[0].Owner || !strings.Contains(l.People[0].Name, "Other") || l.People[1].Owner {
		t.Errorf("the owner comes first and only one is the owner: %+v", l.People)
	}
}
