package httpapi_test

// Tests of the collect token and the owner login.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"git.zexi.me/zexi/atrace/server/auth"
)

// Only a collector that shows the token may write to /collect, and without a token configured
// nothing may; the health check stays open.
func TestCollectNeedsTheToken(t *testing.T) {
	doc := `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":{"type":"Note","id":"weibo:post:1","content":"x"}}`
	run := func(configured string) *httptest.Server {
		t.Setenv("ATRACE_COLLECT_TOKEN", configured)
		login := auth.New(newStore(t, ""))
		return serve(t, login.Middleware)
	}
	srv := run("collector secret")
	for token, want := range map[string]int{"": 401, "wrong": 401, "collector secret": 201} {
		if status, body := collect(t, srv.URL+"/collect/ingest", token, doc); status != want {
			t.Errorf("token %q: ingest = %d %s, want %d", token, status, body, want)
		}
	}
	if status, _ := collect(t, srv.URL+"/collect/media?url=https://x/y.png", "", ""); status != 401 {
		t.Errorf("media without a token = %d, want 401", status)
	}
	if status, _ := do(t, "GET", srv.URL+"/healthz", ""); status != 200 {
		t.Errorf("healthz = %d, want 200", status)
	}
	closed := run("")
	for _, token := range []string{"", "anything"} {
		if status, _ := collect(t, closed.URL+"/collect/ingest", token, doc); status != 401 {
			t.Errorf("no token configured, sent %q: %d, want 401", token, status)
		}
	}
}

// With a password set, a visitor sees only what the owner made public, and cannot reach
// /admin; the owner logs in at /login and sees everything.
func TestLoginSeparatesOwnerFromVisitor(t *testing.T) {
	t.Setenv("ATRACE_COLLECT_TOKEN", "collector secret")
	store := newStore(t, "open sesame")
	login := auth.New(store)
	srv := serveStore(t, store, func(mux http.Handler) http.Handler {
		m := http.NewServeMux()
		m.Handle("/", mux)
		login.Register(m)
		return login.Middleware(m)
	}, true)
	for _, doc := range []string{
		`{"type":"Note","id":"weibo:post:21","content":"private note","published":"2026-11-13T00:00:00Z"}`,
		`{"type":"Create","actor":"weibo:user:1","object":"weibo:post:21","published":"2026-11-13T00:00:00Z"}`,
		`{"type":"Note","id":"weibo:post:22","content":"shared note","published":"2026-11-12T00:00:00Z"}`,
		`{"type":"Create","actor":"weibo:user:1","object":"weibo:post:22","published":"2026-11-12T00:00:00Z"}`,
	} {
		if status, body := collect(t, srv.URL+"/collect/ingest", "collector secret", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`); status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, body)
		}
	}
	jar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(c *http.Client, path string) (int, string) {
		resp, err := c.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	visitor := &http.Client{}

	if status, _ := get(visitor, "/admin/public"); status != 401 {
		t.Errorf("visitor on /admin = %d, want 401", status)
	}
	for _, path := range []string{"/admin/devices", "/admin/people"} {
		if status, _ := get(visitor, path); status != 401 {
			t.Errorf("visitor on %s = %d, want 401", path, status)
		}
	}
	if status, _ := get(visitor, "/admin/devices"); status != 401 {
		t.Errorf("visitor on /admin/devices = %d, want 401", status)
	}
	if status, _ := get(visitor, "/api/entries"); status != 200 {
		t.Fatalf("visitor on /api/entries = %d", status)
	}
	if _, body := get(visitor, "/api/entries"); strings.Contains(body, "private note") || strings.Contains(body, "shared note") {
		t.Errorf("a visitor sees entries nobody made public: %s", body)
	}

	resp, err := owner.PostForm(srv.URL+"/login", url.Values{"password": {"wrong"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("wrong password = %d, want 401", resp.StatusCode)
	}
	if status, _ := get(owner, "/admin/devices"); status != 401 {
		t.Errorf("a wrong password logged in: %d", status)
	}
	resp, err = owner.PostForm(srv.URL+"/login", url.Values{"password": {"open sesame"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login = %d, want 303", resp.StatusCode)
	}
	_, body := get(owner, "/api/entries")
	var page struct {
		Owner   bool `json:"owner"`
		Entries []struct {
			ActivityID int64  `json:"activityId"`
			Ref        string `json:"ref"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatal(err)
	}
	if !page.Owner || len(page.Entries) != 2 {
		t.Fatalf("owner sees %+v, want both entries", page)
	}
	shared := page.Entries[1]
	req, _ := http.NewRequest("PATCH", fmt.Sprintf("%s/admin/entry/%s", srv.URL, shared.Ref), strings.NewReader(`{"public":true}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = owner.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("make public = %d", resp.StatusCode)
	}

	_, body = get(visitor, "/api/entries")
	if !strings.Contains(body, "shared note") || strings.Contains(body, "private note") || !strings.Contains(body, `"activityId":0`) || !strings.Contains(body, `"owner":false`) {
		t.Errorf("visitor sees the wrong entries: %s", body)
	}
	for _, e := range page.Entries {
		status, _ := get(visitor, "/api/entry?ref="+e.Ref)
		if want := map[bool]int{true: 200, false: 404}[e.Ref == shared.Ref]; status != want {
			t.Errorf("visitor on entry %s = %d, want %d", e.Ref, status, want)
		}
	}

	resp, err = owner.PostForm(srv.URL+"/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if status, _ := get(owner, "/admin/devices"); status != 401 {
		t.Errorf("after logout /admin/devices = %d, want 401", status)
	}
}

// The owner's password is kept on the server, not in the environment: before one is set nobody can log in, and
// the owner changes it at /admin/password, which needs the current one. A changed password turns the old one away.
func TestOwnerPasswordIsSetOnTheServerAndChanged(t *testing.T) {
	t.Setenv("ATRACE_COLLECT_TOKEN", "collector secret")
	store := newStore(t, "")
	login := auth.New(store)
	srv := serveStore(t, store, func(mux http.Handler) http.Handler {
		m := http.NewServeMux()
		m.Handle("/", mux)
		login.Register(m)
		return login.Middleware(m)
	}, true)
	if status, body := do(t, "POST", srv.URL+"/login", "password=owner+secret"); status != http.StatusServiceUnavailable || !strings.Contains(body, "set-password") {
		t.Fatalf("login before a password = %d %s, want 503 pointing at set-password", status, body)
	}
	if err := auth.SetPassword(context.Background(), store, "short"); err != auth.ErrShortPassword {
		t.Fatalf("a short password = %v, want ErrShortPassword", err)
	}
	if err := auth.SetPassword(context.Background(), store, "owner secret"); err != nil {
		t.Fatal(err)
	}
	owner := func(password string) (int, string) {
		req, _ := http.NewRequest("GET", srv.URL+"/admin/devices", nil)
		req.Header.Set("Authorization", "Bearer "+password)
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
	if status, _ := owner("owner secret"); status != http.StatusOK {
		t.Fatalf("the owner with the password = %d, want 200", status)
	}
	// The owner is signed in with the password they have; the current one is what they give to change it.
	change := func(current, password string) (int, string) {
		req, _ := http.NewRequest("PUT", srv.URL+"/admin/password", strings.NewReader(`{"current":`+strconv.Quote(current)+`,"password":`+strconv.Quote(password)+`}`))
		req.Header.Set("Authorization", "Bearer owner secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	if status, _ := change("wrong one", "new secret one"); status != http.StatusUnauthorized {
		t.Fatalf("change with the wrong current = %d, want 401", status)
	}
	if status, body := change("owner secret", "short"); status != http.StatusBadRequest {
		t.Fatalf("change to a short one = %d %s, want 400", status, body)
	}
	if status, body := change("owner secret", "new secret one"); status != http.StatusNoContent {
		t.Fatalf("change = %d %s", status, body)
	}
	if status, _ := owner("owner secret"); status != http.StatusUnauthorized {
		t.Errorf("the old password still works: %d", status)
	}
	if status, _ := owner("new secret one"); status != http.StatusOK {
		t.Errorf("the new password = %d, want 200", status)
	}
	if status, _ := do(t, "PUT", srv.URL+"/admin/password", `{"current":"new secret one","password":"x"}`); status != http.StatusUnauthorized {
		t.Errorf("a visitor changing the password = %d, want 401", status)
	}
}
