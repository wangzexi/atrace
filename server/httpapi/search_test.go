package httpapi_test

// Tests of search.

import (
	"encoding/json"
	"net/url"
	"testing"

	"git.zexi.me/zexi/atrace/server/auth"
)

// The search finds an entry by what it shows and by what is around it: the post's text and title,
// its author, the post it answers and its comments; any run of characters, in any case, or a
// regular expression; and it follows what is ingested later.
func TestSearchFindsWhatAnEntryIsAbout(t *testing.T) {
	store := newStore(t, "")
	srv := serveStore(t, store, auth.AsOwner, true)
	ingest := func(doc string) {
		t.Helper()
		if status, body := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`); status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, body)
		}
	}
	for _, doc := range []string{
		`{"type":"Person","id":"bilibili:user:2","name":"UP主小明"}`,
		`{"type":"Video","id":"bilibili:video:BV1","attributedTo":"bilibili:user:2","name":"斯坦福黄仁勋演讲","content":"desc"}`,
		`{"type":"Add","actor":"bilibili:user:1","object":"bilibili:video:BV1","target":"bilibili:collection:1"}`,
		`{"type":"Note","id":"weibo:post:1","content":"今天吃了苹果 Apple #水果","tag":[{"type":"Hashtag","name":"水果"}],"published":"2026-11-13T00:00:00Z"}`,
		`{"type":"Create","actor":"weibo:user:1","object":"weibo:post:1","published":"2026-11-13T00:00:00Z"}`,
		`{"type":"Person","id":"weibo:user:5","name":"老王"}`,
		`{"type":"Note","id":"weibo:post:99","attributedTo":"weibo:user:5","content":"原帖关于火星探测","published":"2026-11-01T00:00:00Z"}`,
		`{"type":"Note","id":"weibo:post:2","inReplyTo":"weibo:post:99","content":"我同意","published":"2026-11-12T00:00:00Z"}`,
		`{"type":"Create","actor":"weibo:user:1","object":"weibo:post:2","published":"2026-11-12T00:00:00Z"}`,
	} {
		ingest(doc)
	}
	// A comment that arrives after the entry it is under, and a reply inside its thread.
	ingest(`{"type":"Note","id":"weibo:comment:1","inReplyTo":"weibo:post:1","content":"评论里提到香蕉","published":"2026-11-13T01:00:00Z"}`)
	ingest(`{"type":"Note","id":"weibo:comment:2","inReplyTo":"weibo:comment:1","content":"楼中楼提到榴莲","published":"2026-11-13T02:00:00Z"}`)
	// A comment is an entry of its author's; the owner publishes the ones that should count in the post.
	ingest(`{"type":"Create","actor":"weibo:user:5","object":"weibo:comment:1","published":"2026-11-13T01:00:00Z"}`)
	ingest(`{"type":"Create","actor":"weibo:user:5","object":"weibo:comment:2","published":"2026-11-13T02:00:00Z"}`)
	publish(t, store, srv.URL, "weibo:comment:1")
	publish(t, store, srv.URL, "weibo:comment:2")

	count := func(query string) (int, int) {
		status, body := do(t, "GET", srv.URL+"/api/entries?"+query, "")
		var page struct {
			Entries []struct{} `json:"entries"`
		}
		_ = json.Unmarshal([]byte(body), &page)
		return status, len(page.Entries)
	}
	for query, want := range map[string]int{
		"q=" + url.QueryEscape("黄仁勋"):                1, // the title of a video
		"q=" + url.QueryEscape("小明"):                 1, // its author, two characters
		"q=" + url.QueryEscape("苹果"):                 1, // the text of a post
		"q=apple":                                    1, // any case
		"q=" + url.QueryEscape("水果"):                 1, // a hashtag
		"q=" + url.QueryEscape("火星"):                 1, // the post an entry answers
		"q=" + url.QueryEscape("老王"):                 1, // and its author
		"q=" + url.QueryEscape("香蕉"):                 1, // a comment under the entry
		"q=" + url.QueryEscape("榴莲"):                 1, // a reply inside a comment's thread
		"q=" + url.QueryEscape("果"):                  1, // fewer than three characters
		"q=" + url.QueryEscape("不存在的词"):              0,
		"q=" + url.QueryEscape("吃了.果") + "&regex=1":  1,
		"q=" + url.QueryEscape("苹果") + "&types=Add":  0, // and it narrows what the type filter left
		"q=" + url.QueryEscape("黄仁勋") + "&types=Add": 1,
	} {
		if status, got := count(query); status != 200 || got != want {
			t.Errorf("%s: status %d, %d entries, want %d", query, status, got, want)
		}
	}
	if status, _ := count("q=" + url.QueryEscape("(") + "&regex=1"); status != 400 {
		t.Errorf("an invalid regular expression = %d, want 400", status)
	}
}
