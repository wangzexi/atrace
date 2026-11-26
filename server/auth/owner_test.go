package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"git.zexi.me/zexi/atrace/server/core"
)

// Who the middleware takes a request for: "owner", "visitor", or the status it refuses with.
func whoIs(t *testing.T, a *Auth, method, path, authorization string) string {
	t.Helper()
	handler := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Owner(r.Context()) {
			_, _ = w.Write([]byte("owner"))
			return
		}
		_, _ = w.Write([]byte("visitor"))
	}))
	request := httptest.NewRequest(method, path, nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		return http.StatusText(recorder.Code)
	}
	return recorder.Body.String()
}

// There is one owner. Its password (or the login cookie) reads and writes everything under /admin/ and /api/shell; a visitor
// gets the public paths and is refused the rest; a collector's token opens nothing but /collect.
func TestOneOwnerForEverythingUnderAdmin(t *testing.T) {
	t.Setenv("ATRACE_COLLECT_TOKEN", "collect-secret")
	dir := t.TempDir()
	store, err := core.Open(filepath.Join(dir, "atrace.db"), filepath.Join(dir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := SetPassword(context.Background(), store, "owner-secret"); err != nil {
		t.Fatal(err)
	}
	a := New(store)
	owner := "Bearer owner-secret"
	for _, path := range []string{"/admin/people", "/admin/devices", "/admin/owner"} {
		if got := whoIs(t, a, http.MethodGet, path, owner); got != "owner" {
			t.Errorf("owner GET %s = %s", path, got)
		}
		if got := whoIs(t, a, http.MethodGet, path, ""); got == "owner" {
			t.Errorf("visitor GET %s was let in", path)
		}
	}
	for _, method := range []string{http.MethodPatch, http.MethodPost, http.MethodPut, http.MethodDelete} {
		if got := whoIs(t, a, method, "/admin/entry/k7x2m9ab", owner); got != "owner" {
			t.Errorf("owner %s /admin/entry/k7x2m9ab = %s", method, got)
		}
		if got := whoIs(t, a, method, "/admin/entry/k7x2m9ab", "Bearer collect-secret"); got == "owner" {
			t.Errorf("collector %s /admin/entry/k7x2m9ab was let in", method)
		}
	}
	if got := whoIs(t, a, http.MethodPost, "/api/shell", owner); got != "owner" {
		t.Errorf("owner POST /api/shell = %s", got)
	}
	if got := whoIs(t, a, http.MethodPost, "/api/shell", ""); got == "owner" {
		t.Errorf("visitor POST /api/shell is the owner")
	}
	if got := whoIs(t, a, http.MethodGet, "/api/entries", ""); got != "visitor" {
		t.Errorf("visitor GET /api/entries = %s", got)
	}
}
