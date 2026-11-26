package httpapi_test

// Shared helpers of the httpapi tests.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"git.zexi.me/zexi/atrace/server/auth"
	"git.zexi.me/zexi/atrace/server/core"
	"git.zexi.me/zexi/atrace/server/httpapi"
	"git.zexi.me/zexi/atrace/server/site"
)

// testOwnerAccounts are the accounts the tests' own posts belong to; any other actor is someone else.
var testOwnerAccounts = []string{"weibo:user:1", "weibo:user:9", "x:user:1", "xhs:user:1", "zhihu:user:1", "blog:user:z", "bilibili:user:1", "z:user:1", "douyin:user:1", "nowhere:user:1"}

// server is an atrace where every request is the owner's.
func server(t *testing.T) *httptest.Server {
	t.Helper()
	return serve(t, auth.AsOwner)
}

func serve(t *testing.T, wrap func(http.Handler) http.Handler) *httptest.Server {
	t.Helper()
	return build(t, wrap, true)
}

// serveUnbound is serve for an owner who has not bound any account yet.
func serveUnbound(t *testing.T, wrap func(http.Handler) http.Handler) *httptest.Server {
	t.Helper()
	return build(t, wrap, false)
}

// newStore is an empty archive; with a password, the owner logs in with it.
func newStore(t *testing.T, password string) *core.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := core.Open(filepath.Join(dir, "atrace.db"), filepath.Join(dir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if password != "" {
		if err := auth.SetPassword(context.Background(), store, password); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func build(t *testing.T, wrap func(http.Handler) http.Handler, bound bool) *httptest.Server {
	t.Helper()
	return serveStore(t, newStore(t, ""), wrap, bound)
}

// serveStore serves the archive in store through wrap.
func serveStore(t *testing.T, store *core.Store, wrap func(http.Handler) http.Handler, bound bool) *httptest.Server {
	t.Helper()
	// The owner's timeline is what their accounts did; these are the ones the tests' own posts come from.
	if bound {
		for _, account := range testOwnerAccounts {
			doc := `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":{"type":"Person","id":"` + account + `","name":"Alice"}}`
			if _, err := store.Ingest(context.Background(), json.RawMessage(doc)); err != nil {
				t.Fatal(err)
			}
		}
		id, err := store.CreatePerson(context.Background(), "Alice", testOwnerAccounts)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetOwner(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	hub := httpapi.NewHub()
	mux := http.NewServeMux()
	owner := os.Getenv("ATRACE_OWNER")
	if owner == "" {
		owner = "Alice"
	}
	httpapi.RegisterRoutes(mux, store, hub, httpapi.Site{Owner: owner, DefaultTitle: "Test site"})
	site.RegisterRoutes(mux, store, owner)
	srv := httptest.NewServer(wrap(hub.Notify(mux)))
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return resp.StatusCode, sb.String()
}

// collect posts to a /collect path with the given bearer token (none when empty).
func collect(t *testing.T, url, token, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

// bindOwner makes the accounts the owner's, as the owner does in the page: the owner is one of the people, and the
// accounts are given to that person. Only then does the timeline call their entries the owner's.
func bindOwner(t *testing.T, srv *httptest.Server, accounts ...string) {
	t.Helper()
	list, _ := json.Marshal(accounts)
	_, body := do(t, "GET", srv.URL+"/admin/people", "")
	var people struct {
		People []struct {
			ID    int64
			Owner bool
		}
	}
	if err := json.Unmarshal([]byte(body), &people); err != nil {
		t.Fatal(err)
	}
	for _, person := range people.People {
		if person.Owner {
			if status, b := do(t, "PUT", srv.URL+"/admin/people/"+strconv.FormatInt(person.ID, 10), `{"name":"Alice","accounts":`+string(list)+`}`); status != http.StatusNoContent {
				t.Fatalf("bind %v = %d %s", accounts, status, b)
			}
			return
		}
	}
	status, b := do(t, "POST", srv.URL+"/admin/people", `{"name":"Alice","accounts":`+string(list)+`}`)
	var created struct{ ID int64 }
	if status != http.StatusOK && status != http.StatusCreated || json.Unmarshal([]byte(b), &created) != nil {
		t.Fatalf("bind %v = %d %s", accounts, status, b)
	}
	if status, b := do(t, "PUT", srv.URL+"/admin/owner", `{"person":`+strconv.FormatInt(created.ID, 10)+`}`); status != http.StatusNoContent {
		t.Fatalf("make the owner = %d %s", status, b)
	}
}

// publish makes the owner's entry for an object public, by the entry's ref, as the page does.
func publish(t *testing.T, store *core.Store, base, object string) {
	t.Helper()
	entry, err := store.ObjectEntry(context.Background(), object)
	if err != nil || entry.Ref == "" {
		t.Fatalf("entry for %s: %v", object, err)
	}
	if status, body := do(t, "PATCH", base+"/admin/entry/"+entry.Ref, `{"public":true}`); status != 200 {
		t.Fatalf("publish %s: %d %s", object, status, body)
	}
}
