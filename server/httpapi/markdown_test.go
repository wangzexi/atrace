package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"git.zexi.me/zexi/atrace/server/auth"
)

// markdownServer is an archive with an own post that has comments, an own post that stays private, a quote, a like and
// an article; all but the private one are public. Every request is the owner's.
func markdownServer(t *testing.T) (url string, refs map[string]string) {
	t.Helper()
	store := newStore(t, "")
	srv := serveStore(t, store, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Has("visitor") {
				next.ServeHTTP(w, r)
				return
			}
			auth.AsOwner(next).ServeHTTP(w, r)
		})
	}, true)
	for _, doc := range []string{
		`{"type":"Person","id":"weibo:user:1","name":"微博昵称"}`,
		`{"type":"Person","id":"weibo:user:2","name":"Alice"}`,
		`{"type":"Person","id":"x:user:9","name":"Chun"}`,
		`{"type":"Note","id":"weibo:post:1","attributedTo":"weibo:user:1","content":"hello public world\nsecond line","url":"https://weibo.com/1/a","published":"2026-11-10T00:00:00Z","attachment":[{"type":"Image","url":"https://wx1.sinaimg.cn/a.jpg"}]}`,
		`{"type":"Create","actor":"weibo:user:1","object":"weibo:post:1","published":"2026-11-10T00:00:00Z"}`,
		`{"type":"Note","id":"weibo:comment:1","attributedTo":"weibo:user:2","inReplyTo":"weibo:post:1","content":"nice one\nover two lines","published":"2026-11-10T01:00:00Z"}`,
		`{"type":"Note","id":"weibo:comment:2","attributedTo":"weibo:user:1","inReplyTo":"weibo:comment:1","content":"thanks","published":"2026-11-10T02:00:00Z"}`,
		`{"type":"Create","actor":"weibo:user:2","object":"weibo:comment:1","published":"2026-11-10T01:00:00Z"}`,
		`{"type":"Create","actor":"weibo:user:1","object":"weibo:comment:2","published":"2026-11-10T02:00:00Z"}`,
		`{"type":"Note","id":"weibo:post:2","attributedTo":"weibo:user:1","content":"private secret note","published":"2026-11-11T00:00:00Z"}`,
		`{"type":"Create","actor":"weibo:user:1","object":"weibo:post:2","published":"2026-11-11T00:00:00Z"}`,
		`{"type":"Note","id":"x:post:100","attributedTo":"x:user:9","content":"the original thing","url":"https://x.com/chun/status/100","published":"2026-11-01T00:00:00Z"}`,
		`{"type":"Note","id":"x:post:200","attributedTo":"x:user:1","content":"my take on it","quote":"x:post:100","published":"2026-11-12T00:00:00Z"}`,
		`{"type":"Create","actor":"x:user:1","object":"x:post:200","published":"2026-11-12T00:00:00Z"}`,
		`{"type":"Like","actor":"weibo:user:1","object":"x:post:100"}`,
		`{"type":"Article","id":"blog:article:1","name":"A Title","mediaType":"text/markdown","content":"## Part one\n\nbody of the article","published":"2026-11-13T00:00:00Z"}`,
		`{"type":"Create","actor":"blog:user:z","object":"blog:article:1","published":"2026-11-13T00:00:00Z"}`,
	} {
		if status, body := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`); status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, body)
		}
	}
	bindOwner(t, srv, "weibo:user:1", "x:user:1", "blog:user:z")
	_, body := do(t, "GET", srv.URL+"/api/entries?types=Create,Like", "")
	var page struct {
		Entries []struct {
			ActivityID int64
			Ref, ID    string
		}
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatal(err)
	}
	// The owner publishes the comments that should show under the post, each by its entry.
	publish(t, store, srv.URL, "weibo:comment:1")
	publish(t, store, srv.URL, "weibo:comment:2")
	refs = map[string]string{}
	for _, entry := range page.Entries {
		refs[entry.ID] = entry.Ref
		if entry.ID == "weibo:post:2" {
			continue // stays private
		}
		if status, b := do(t, "PATCH", fmt.Sprintf("%s/admin/entry/%s", srv.URL, entry.Ref), `{"public":true}`); status != 200 {
			t.Fatalf("make public = %d %s", status, b)
		}
	}
	return srv.URL, refs
}

func TestTimelineAsMarkdownIsTheVisitors(t *testing.T) {
	url, _ := markdownServer(t)
	get := func(path string) (string, string, int) {
		t.Helper()
		resp, err := http.Get(url + path)
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
		return sb.String(), resp.Header.Get("Content-Type"), resp.StatusCode
	}
	// Every request here is the owner's (see markdownServer), and the Markdown is the visitor's all the same: what the
	// owner keeps private is not in it.
	body, contentType, status := get("/index.md")
	if status != 200 || contentType != "text/plain; charset=utf-8" {
		t.Fatalf("%d %q", status, contentType)
	}
	for _, want := range []string{"# Alice 的时间线", "/terminal", "/terminal.md", "2026-11-13 Alice 原创 A Title", "hello public world",
		"Chun the original thing", "] Alice 点赞", "已到最后一页。"} {
		if !strings.Contains(body, want) {
			t.Errorf("the timeline lacks %q:\n%s", want, body)
		}
	}
	for _, never := range []string{"private secret note", "公开：", "Bearer", "![配图]", "https://x.com/chun/status/100", "https://weibo.com/1/a"} {
		if strings.Contains(body, never) {
			t.Errorf("the timeline has %q:\n%s", never, body)
		}
	}
	// The timeline is newest first, and has the comments of a post nowhere: they are the post's.
	if strings.Index(body, "A Title") > strings.Index(body, "my take on it") || strings.Index(body, "my take on it") > strings.Index(body, "hello public world") {
		t.Errorf("not newest first:\n%s", body)
	}
	// A reply shows the comment it answers as its quote, but the comment is not an entry of the owner's timeline.
	if strings.Contains(body, "[") && strings.Contains(body, "] Alice 评论") {
		t.Errorf("a comment of someone else is an entry of the owner's timeline:\n%s", body)
	}

	// Paging: a page of one says where the next begins, the last says it is the last.
	_, _, legacyStatus := get("/llms.txt?limit=1")
	first, _, _ := get("/index.md?limit=1")
	if legacyStatus != 404 {
		t.Fatal("/llms.txt is not served and must return 404")
	}
	if !strings.Contains(first, "/index.md?") || !strings.Contains(first, "after=") || !strings.Contains(first, "limit=1") || strings.Contains(first, "已到最后一页") {
		t.Errorf("first page:\n%s", first)
	}
	last := first
	for i := 0; i < 10; i++ {
		_, tail, ok := strings.Cut(last, "[下一页](")
		if !ok {
			break
		}
		next, _, _ := strings.Cut(tail, ")")
		last, _, _ = get(strings.TrimPrefix(next, url))
	}
	if !strings.Contains(last, "已到最后一页。") || strings.Contains(last, "[下一页]") {
		t.Errorf("last page:\n%s", last)
	}

}

func TestPostAsMarkdownWithItsComments(t *testing.T) {
	url, refs := markdownServer(t)
	get := func(path string) (string, int) {
		t.Helper()
		status, body := do(t, "GET", url+path, "")
		return body, status
	}
	post, status := get("/" + refs["weibo:post:1"] + ".md")
	if status != 200 {
		t.Fatalf("post = %d %s", status, post)
	}
	if !strings.HasPrefix(post, "---\n") || !strings.Contains(post, "\n---\n\nhello public world\nsecond line") {
		t.Errorf("metadata must precede the original body:\n%s", post)
	}
	if strings.Contains(post, "# hello public world") || strings.Contains(post, "标识") {
		t.Errorf("untitled notes must not get an invented title or identifier:\n%s", post)
	}
	for _, want := range []string{"类型: Create", "时间: \"2026-11-10 08:00\"", "作者: \"Alice\"", "原文: \"https://weibo.com/1/a\"",
		"hello public world\nsecond line", "![配图](https://wx1.sinaimg.cn/a.jpg)", "## 评论（2）",
		"- **Alice** · 2026-11-10 09:00：nice one\n  over two lines\n", "- **Alice** · 2026-11-10 10:00 回复 Alice：thanks"} {
		if !strings.Contains(post, want) {
			t.Errorf("the post lacks %q:\n%s", want, post)
		}
	}
	quote, _ := get("/" + refs["x:post:200"] + ".md")
	for _, want := range []string{"my take on it", "引用：", "> **Chun**", "> the original thing"} {
		if !strings.Contains(quote, want) {
			t.Errorf("the quote lacks %q:\n%s", want, quote)
		}
	}
	article, _ := get("/" + refs["blog:article:1"] + ".md")
	if !strings.Contains(article, "# A Title") || !strings.Contains(article, "## Part one") {
		t.Errorf("article:\n%s", article)
	}
	like, _ := get("/" + refs["x:post:100"] + ".md")
	if !strings.Contains(like, "类型: Like") || !strings.Contains(like, "> the original thing") {
		t.Errorf("like:\n%s", like)
	}
	// What is private is not there, for the owner's request too: nothing says it exists.
	if _, status := get("/" + refs["weibo:post:2"] + ".md"); status != 404 {
		t.Errorf("a private post = %d, want 404", status)
	}
	if _, status := get("/nosuchref.md"); status != 404 {
		t.Errorf("a missing post = %d, want 404", status)
	}
}

// A page says where its Markdown is, and gives it to a request that asks for it before HTML: a program that reads
// says so in Accept; a browser asks for HTML and gets the page.
func TestPagesOfferTheirMarkdown(t *testing.T) {
	url, refs := markdownServer(t)
	get := func(path, accept string) (*http.Response, string) {
		t.Helper()
		request, err := http.NewRequest("GET", url+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if accept != "" {
			request.Header.Set("Accept", accept)
		}
		resp, err := http.DefaultClient.Do(request)
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
		return resp, sb.String()
	}
	isMarkdown := func(resp *http.Response) bool {
		return resp.Header.Get("Content-Type") == "text/markdown; charset=utf-8"
	}
	post := "/" + refs["weibo:post:1"]
	for _, c := range []struct {
		accept   string
		markdown bool
	}{
		{"text/markdown", true},
		{"text/markdown, text/html", true},
		{"text/markdown, text/html;q=0.8", true},
		{"text/html, text/markdown;q=0.5", false},
		{"text/html, text/markdown", false},
		{"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", false},
		{"text/markdown;q=0", false},
		{"*/*", false},
		{"", false},
	} {
		for path, marker := range map[string]string{"/": "# Alice 的时间线", post: "类型: Create"} {
			resp, body := get(path, c.accept)
			if isMarkdown(resp) != c.markdown || (c.markdown && !strings.Contains(body, marker)) {
				t.Errorf("GET %s accepting %q: markdown=%v, want %v", path, c.accept, isMarkdown(resp), c.markdown)
			}
			wantLink := `</index.md>; rel="alternate"; type="text/markdown"`
			if path == post {
				wantLink = "<" + post + `.md>; rel="alternate"; type="text/markdown"`
			}
			if resp.Header.Get("Link") != wantLink || !strings.Contains(resp.Header.Get("Vary"), "Accept") {
				t.Errorf("GET %s accepting %q: Link %q, Vary %q", path, c.accept, resp.Header.Get("Link"), resp.Header.Get("Vary"))
			}
		}
	}
	// What is private is not offered as Markdown, whoever asks.
	if resp, _ := get("/"+refs["weibo:post:2"], "text/markdown"); resp.StatusCode != 404 {
		t.Errorf("a private post, asking for Markdown = %d, want 404", resp.StatusCode)
	}
}

// The complete interface is /admin: it has everything, private entries included, and it searches.

func TestMarkdownListSummarizesLongLikesAndLinksOnlyToDetails(t *testing.T) {
	base, refs := markdownServer(t)
	content := strings.Repeat("长文内容", 200) + "END_OF_LONG_POST"
	doc := map[string]any{"type": "Note", "id": "x:post:100", "attributedTo": "x:user:9", "content": content, "url": "https://x.com/chun/status/100"}
	payload, _ := json.Marshal(map[string]any{"observed_at": "2026-11-15T00:00:00Z", "observed_by": "test", "document": doc})
	if status, body := do(t, "POST", base+"/collect/ingest", string(payload)); status != 200 && status != 201 {
		t.Fatalf("ingest: %d %s", status, body)
	}
	status, list := do(t, "GET", base+"/index.md?types=Like", "")
	if status != 200 {
		t.Fatal(status, list)
	}
	if !strings.Contains(list, "长文内容") || !strings.Contains(list, "…") || !strings.Contains(list, "["+refs["x:post:100"]+"] Alice 点赞") {
		t.Fatal("missing summary or short ID", list)
	}
	for _, unwanted := range []string{"END_OF_LONG_POST", "https://x.com/chun/status/100", " · ", "![配图]", "\n## ", "\n---", base + "/" + refs["x:post:100"] + ".md"} {
		if strings.Contains(list, unwanted) {
			t.Errorf("index contains %q", unwanted)
		}
	}
	status, detail := do(t, "GET", base+"/"+refs["x:post:100"]+".md", "")
	if status != 200 || !strings.Contains(detail, "END_OF_LONG_POST") || !strings.Contains(detail, "https://x.com/chun/status/100") {
		t.Fatal("detail lost content or source", status, detail)
	}
}

func TestTimelineRangesAndCursorsAcrossReadInterfaces(t *testing.T) {
	base, _ := markdownServer(t)
	rangeQuery := "since=2026-11-10&until=2026-11-11"
	for _, path := range []string{"/api/entries?"} {
		status, body := do(t, "GET", base+path+rangeQuery, "")
		wanted := "hello public world"
		if status != 200 || !strings.Contains(body, wanted) || strings.Contains(body, "private secret note") || strings.Contains(body, "my take on it") {
			t.Errorf("%s: %d %s", path, status, body)
		}
		for _, bad := range []string{"since=bad", "since=2026-12-01&until=2026-11-01", "after=missing"} {
			if status, body := do(t, "GET", base+path+bad, ""); status != 400 {
				t.Errorf("%s%s: %d %s", path, bad, status, body)
			}
		}
	}
	for _, path := range []string{"/api/entries"} {
		status, body := do(t, "GET", base+path+"?kind=Like&since=2026-11-01&until=2026-11-02", "")
		if status != 200 || !strings.Contains(body, "x:post:100") {
			t.Errorf("missing undated like's post-date fallback: %d %s", status, body)
		}
	}
	for _, path := range []string{"/api/entries"} {
		status, body := do(t, "GET", base+path+"?limit=1", "")
		var first struct {
			Next    string `json:"next"`
			Entries []struct {
				Ref        string `json:"ref"`
				ActivityID int64  `json:"activityId"`
			} `json:"entries"`
		}
		if err := json.Unmarshal([]byte(body), &first); status != 200 || err != nil || first.Next == "" || len(first.Entries) != 1 {
			t.Fatalf("first cursor page %s: %d %s", path, status, body)
		}
		status, body = do(t, "GET", base+path+"?limit=1&after="+first.Next, "")
		var second struct {
			Next    string `json:"next"`
			Entries []struct {
				Ref string `json:"ref"`
			} `json:"entries"`
		}
		if err := json.Unmarshal([]byte(body), &second); status != 200 || err != nil || len(second.Entries) != 1 || second.Next == first.Next {
			t.Fatalf("next cursor page %s: %d %s", path, status, body)
		}
	}

}

func TestVisibilityFiltersBeforePagingAndCannotExposePrivateEntries(t *testing.T) {
	base, _ := markdownServer(t)
	for _, endpoint := range []string{"/api/entries?types=Create", "/api/entries?types=Create"} {
		for _, visibility := range []string{"all", "public", "private"} {
			status, body := do(t, "GET", base+endpoint+"&visibility="+visibility, "")
			if status != 200 {
				t.Fatal(status, body)
			}
			privateMarker := "private secret note"
			publicMarker := "hello public world"
			if strings.Contains(body, privateMarker) != (visibility != "public") || strings.Contains(body, publicMarker) != (visibility != "private") {
				t.Errorf("%s %s: %s", endpoint, visibility, body)
			}
		}
		if status, _ := do(t, "GET", base+endpoint+"&visibility=invalid", ""); status != 400 {
			t.Errorf("invalid visibility: %d", status)
		}
	}
	status, body := do(t, "GET", base+"/api/entries?visibility=private&visitor=1", "")
	if status != 200 || strings.Contains(body, "private secret note") || !strings.Contains(body, `"entries":[]`) {
		t.Fatal("visitor saw private entries", status, body)
	}
	status, body = do(t, "GET", base+"/index.md?visibility=private", "")
	if status != 200 || strings.Contains(body, "private secret note") {
		t.Fatal("public Markdown leaked private entry", status, body)
	}
	status, body = do(t, "GET", base+"/api/entries?types=Create&visibility=private&limit=1", "")
	var result struct {
		Next    string `json:"next"`
		Entries []struct {
			Public bool `json:"public"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &result); status != 200 || err != nil || len(result.Entries) != 1 || result.Entries[0].Public || result.Next != "" {
		t.Fatal("visibility was applied after paging", status, body)
	}
}
