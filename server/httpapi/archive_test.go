package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"git.zexi.me/zexi/atrace/server/auth"
)

func TestArchiveShellUsesDefaultPersonForVisitorsAndKeepsDraftsPrivate(t *testing.T) {
	const password = "owner test password"
	store := newStore(t, password)
	srv := serveStore(t, store, auth.New(store).Middleware, true)
	ctx := context.Background()
	for _, doc := range []string{
		`{"type":"Article","id":"blog:article:public","name":"成稿","attributedTo":"blog:user:z","content":"公开正文","published":"2026-11-30T18:00:00Z","attachment":[{"type":"Image","url":"https://example.test/cover.png"}]}`,
		`{"type":"Create","actor":"blog:user:z","object":"blog:article:public"}`,
		`{"type":"Article","id":"blog:article:private","name":"私人文章","attributedTo":"blog:user:z","content":"全私有内容","published":"2026-11-30T18:00:00Z"}`,
		`{"type":"Create","actor":"blog:user:z","object":"blog:article:private"}`,
		`{"type":"Person","id":"weibo:user:secondary","name":"Bob"}`,
		`{"type":"Note","id":"weibo:post:secondary","attributedTo":"weibo:user:secondary","content":"另一位的公开笔记","published":"2026-12-01T01:00:00Z"}`,
		`{"type":"Create","actor":"weibo:user:secondary","object":"weibo:post:secondary"}`,
	} {
		if _, err := store.Ingest(ctx, json.RawMessage(`{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","raw":{"source":"owner-only raw marker"},"document":`+doc+`}`)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.PutFile(ctx, strings.NewReader("cover bytes"), "https://example.test/cover.png", "image/png"); err != nil {
		t.Fatal(err)
	}
	entry, err := store.ObjectEntry(ctx, "blog:article:public")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetPublic(ctx, entry.ActivityID, true); err != nil {
		t.Fatal(err)
	}
	secondaryPerson, err := store.CreatePerson(ctx, "Bob", []string{"weibo:user:secondary"})
	if err != nil {
		t.Fatal(err)
	}
	secondaryEntry, err := store.ObjectEntry(ctx, "weibo:post:secondary")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetPublic(ctx, secondaryEntry.ActivityID, true); err != nil {
		t.Fatal(err)
	}
	request := func(route string, body any, owner bool) (int, string) {
		t.Helper()
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		token := ""
		if owner {
			token = password
		}
		return collect(t, srv.URL+route, token, string(data))
	}
	put := func(body string, owner bool) int {
		t.Helper()
		r, err := http.NewRequest("PUT", srv.URL+"/admin/attachments", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if owner {
			r.Header.Set("Authorization", "Bearer "+password)
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	attachment := `{"object":"blog:article:public","files":[{"path":"refs/drafts/第二稿.md","content":"第一行\n只在草稿里的词\n"}]}`
	if got := put(attachment, false); got != 401 {
		t.Fatalf("visitor writes attachments: %d", got)
	}
	if got := put(attachment, true); got != 204 {
		t.Fatalf("owner writes attachments: %d", got)
	}
	if got := put(strings.Replace(attachment, "refs/drafts/第二稿.md", "refs/../第二稿.md", 1), true); got != 400 {
		t.Fatalf("invalid path: %d", got)
	}
	type result struct {
		CWD, Stdout, Stderr string
		Status              int
	}
	run := func(command, cwd string, owner bool) result {
		t.Helper()
		status, body := request("/api/shell", map[string]string{"command": command, "cwd": cwd}, owner)
		var got result
		if status != 200 || json.Unmarshal([]byte(body), &got) != nil {
			t.Fatalf("%s: %d %s", command, status, body)
		}
		return got
	}
	month := "/atrace/Alice/2026-12"
	base := month + "/" + entry.Ref
	draft := base + "/attachments/refs/drafts/第二稿.md"
	visitorMonth := "/atrace/2026-12"
	visitorBase := visitorMonth + "/" + entry.Ref
	if got := run("ls", "", true); got.Status != 0 || got.Stdout != "Alice\nBob\nREADME.md\n" {
		t.Fatalf("owner lists both people: %+v", got)
	}
	if got := run("ls", "", false); got.Status != 0 || got.Stdout != "2026-12\nREADME.md\n" {
		t.Fatalf("visitor lists months: %+v", got)
	}
	if got := run("cat "+visitorMonth+"/"+secondaryEntry.Ref+"/entry.md", "", false); got.Status == 0 || got.Stdout != "" {
		t.Fatalf("nondefault record direct lookup: %+v", got)
	}
	if got := run("cd "+base+" && ls", "", true); got.Status != 0 || got.CWD != base || !strings.Contains(got.Stdout, "attachments\n") {
		t.Fatalf("cd and ls: %+v", got)
	}
	if got := run("cat entry.md", base, true); got.Status != 0 || !strings.Contains(got.Stdout, "公开正文") {
		t.Fatalf("read at the Beijing month boundary: %+v", got)
	}
	if got := run("pwd", base, true); got.Stdout != base+"\n" {
		t.Fatalf("interactive cwd: %+v", got)
	}
	if got := run("pwd", "", true); got.Stdout != "/atrace\n" {
		t.Fatalf("independent cwd: %+v", got)
	}
	if got := run("ls attachments", base, true); got.Status != 0 || got.Stdout != "1-cover.png\nrefs\n" {
		t.Fatalf("owner lists media and draft folders together: %+v", got)
	}
	if got := run("ls attachments", visitorBase, false); got.Status != 0 || got.Stdout != "1-cover.png\n" {
		t.Fatalf("visitor lists the public attachment: %+v", got)
	}
	if got := run("grep -rn '只在草稿里的词' . | head -n 1", month, true); got.Status != 0 || !strings.Contains(got.Stdout, draft+":2:") {
		t.Fatalf("find the draft: %+v", got)
	}
	if got := run("grep -rn '只在草稿里的词' .", visitorMonth, false); got.Status != 1 || got.Stdout != "" {
		t.Fatalf("visitor found a draft: %+v", got)
	}
	if got := run("find . -name '*.md'", visitorMonth, false); got.Status != 0 || got.Stdout != visitorBase+"/entry.md\n" {
		t.Fatalf("visitor lists only public files: %+v", got)
	}
	if got := run("cat entry.json", base, true); got.Status != 0 || !strings.Contains(got.Stdout, "owner-only raw marker") {
		t.Fatalf("owner can read raw: %+v", got)
	}
	if got := run("cat entry.json", visitorBase, false); got.Status != 0 || strings.Contains(got.Stdout, "owner-only raw marker") {
		t.Fatalf("visitor reads the public entry: %+v", got)
	}
	if got := run("cat /etc/passwd", "", true); got.Status == 0 || got.Stdout != "" {
		t.Fatalf("shell read outside the archive: %+v", got)
	}
	for _, test := range []struct {
		path, text string
		owner      bool
		status     int
	}{
		{draft, "只在草稿里的词", true, 200},
		{draft, "", false, 404},
		{visitorBase + "/attachments/refs/drafts/第二稿.md", "", false, 404},
		{visitorBase + "/attachments/1-cover.png", "cover bytes", false, 200},
		{visitorBase + "/attachments/1-cover.png", "cover bytes", true, 200},
	} {
		token := ""
		if test.owner {
			token = password
		}
		r, err := http.NewRequest("GET", srv.URL+"/api/archive-file?path="+url.QueryEscape(test.path), nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != test.status || !strings.Contains(string(body), test.text) {
			t.Fatalf("download, owner=%t: %d %s", test.owner, resp.StatusCode, string(body))
		}
	}
	if got := put(`{"object":"blog:article:public","files":[]}`, true); got != 204 {
		t.Fatalf("remove drafts: %d", got)
	}
	if got := run("find . -name '*.md'", base, true); got.Status != 0 || got.Stdout != base+"/entry.md\n" {
		t.Fatalf("deleted drafts still listed: %+v", got)
	}
	if err := store.SetOwner(ctx, secondaryPerson); err != nil {
		t.Fatal(err)
	}
	if got := run("ls", visitorMonth, false); got.Status != 0 || got.Stdout != secondaryEntry.Ref+"\n" {
		t.Fatalf("visitor follows the newly selected default person: %+v", got)
	}
}
