package httpapi_test

// Tests of the timeline, its entries and cards.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// An entry is addressed by a random ref, never by its counting ID, so nobody can
// walk from one entry to the next.
func TestEntriesAreAddressedByRandomRefs(t *testing.T) {
	srv := server(t)
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("weibo:post:%d", 100+i)
		for _, doc := range []string{
			`{"type":"Note","id":"` + id + `","content":"post ` + id + `","published":"2026-11-13T00:00:00Z"}`,
			`{"type":"Create","actor":"weibo:user:1","object":"` + id + `","published":"2026-11-13T00:00:0` + fmt.Sprint(i) + `Z"}`,
		} {
			if status, body := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`); status != 200 && status != 201 {
				t.Fatalf("ingest = %d %s", status, body)
			}
		}
	}
	var page struct {
		Entries []struct {
			ActivityID int64  `json:"activityId"`
			Ref        string `json:"ref"`
			ID         string `json:"id"`
		} `json:"entries"`
	}
	_, body := do(t, "GET", srv.URL+"/api/entries", "")
	if err := json.Unmarshal([]byte(body), &page); err != nil || len(page.Entries) != 3 {
		t.Fatalf("entries = %s (%v)", body, err)
	}
	seen := map[string]bool{}
	for _, e := range page.Entries {
		if len(e.Ref) != 8 || seen[e.Ref] || strings.Trim(e.Ref, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789") != "" {
			t.Fatalf("ref %q is not a distinct random string", e.Ref)
		}
		seen[e.Ref] = true
		if status, body := do(t, "GET", srv.URL+"/api/entry?ref="+e.Ref, ""); status != 200 || !strings.Contains(body, e.ID) {
			t.Fatalf("entry by ref = %d %s", status, body)
		}
		// The counting ID opens nothing.
		if status, _ := do(t, "GET", srv.URL+fmt.Sprintf("/api/entry?ref=%d", e.ActivityID), ""); status != 404 {
			t.Fatalf("entry by counting ID = %d, want 404", status)
		}
	}
	if status, _ := do(t, "GET", srv.URL+"/api/entry?ref=nosuchentry", ""); status != 404 {
		t.Fatalf("unknown ref = %d, want 404", status)
	}
}

// A like or a favorite is the owner's entry, headed by the owner (under one name,
// not the nickname a platform knows them by), with the liked post as a card inside it; the timeline never shows other people's posts as
// the owner's own.
func TestReactionsAreTheOwnersEntriesWithACard(t *testing.T) {
	srv := server(t)
	for _, doc := range []string{
		`{"type":"Person","id":"weibo:user:1","name":"某平台上的昵称"}`,
		`{"type":"Person","id":"weibo:user:2","name":"Alice"}`,
		`{"type":"Note","id":"weibo:post:9","attributedTo":"weibo:user:2","content":"alice post","published":"2020-01-01T08:30:00Z"}`,
		`{"type":"Like","actor":"weibo:user:1","object":"weibo:post:9"}`,
	} {
		if status, body := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`); status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, body)
		}
	}
	bindOwner(t, srv, "weibo:user:1")
	var page struct {
		Entries []struct {
			Actor string    `json:"actor"`
			Type  string    `json:"type"`
			Post  *struct{} `json:"post"`
			Quote *struct {
				Author    string `json:"author"`
				Published string `json:"published"`
				Content   string `json:"content"`
			} `json:"quote"`
		} `json:"entries"`
	}
	_, body := do(t, "GET", srv.URL+"/api/entries", "")
	if err := json.Unmarshal([]byte(body), &page); err != nil || len(page.Entries) != 1 {
		t.Fatalf("entries = %s (%v)", body, err)
	}
	like := page.Entries[0]
	if like.Actor != "Alice" || like.Type != "Like" || like.Post != nil {
		t.Errorf("the like is not the owner's own entry: %+v", like)
	}
	if like.Quote == nil || like.Quote.Author != "Alice" || like.Quote.Content != "alice post" || like.Quote.Published != "2020-01-01T08:30:00Z" {
		t.Errorf("the liked post is not a card inside it: %+v", like.Quote)
	}
}

// The filter kinds split what the owner did: an own post (one that quotes another is still an own
// post), a repost, a reply (a post of their own that answers another post), a like, a favorite.
func TestTimelineKindsSplitReplies(t *testing.T) {
	srv := server(t)
	for _, doc := range []string{
		`{"type":"Person","id":"x:user:1","name":"me"}`,
		`{"type":"Note","id":"x:post:1","attributedTo":"x:user:1","content":"original"}`,
		`{"type":"Create","actor":"x:user:1","object":"x:post:1"}`,
		`{"type":"Note","id":"x:post:2","attributedTo":"x:user:1","inReplyTo":"x:post:9","content":"a reply"}`,
		`{"type":"Create","actor":"x:user:1","object":"x:post:2"}`,
		`{"type":"Note","id":"x:post:3","attributedTo":"x:user:9","content":"theirs"}`,
		`{"type":"Announce","id":"x:post:4","actor":"x:user:1","object":"x:post:3"}`,
		`{"type":"Note","id":"x:post:5","attributedTo":"x:user:1","content":"quote that replies","inReplyTo":"x:post:9","quote":"x:post:3"}`,
		`{"type":"Create","actor":"x:user:1","object":"x:post:5"}`,
		`{"type":"Note","id":"x:post:7","attributedTo":"x:user:1","content":"quoting","quote":"x:post:3"}`,
		`{"type":"Create","actor":"x:user:1","object":"x:post:7"}`,
		`{"type":"Like","actor":"x:user:1","object":"x:post:3"}`,
		`{"type":"Add","actor":"x:user:1","object":"x:post:3","target":"x:collection:bookmarks"}`,
	} {
		if status, body := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`); status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, body)
		}
	}
	count := func(types string) int {
		_, body := do(t, "GET", srv.URL+"/api/entries?types="+types, "")
		var page struct {
			Entries []struct{} `json:"entries"`
		}
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatalf("%s: %s (%v)", types, body, err)
		}
		return len(page.Entries)
	}
	for types, want := range map[string]int{"Create": 2, "Announce": 1, "Reply": 2, "Like": 1, "Add": 1, "Create,Reply,Announce,Like,Add": 7} {
		if got := count(types); got != want {
			t.Errorf("types=%s: %d entries, want %d", types, got, want)
		}
	}
}

// An entry whose time nobody knows is not dated by when it was collected: it comes after every
// dated entry, newest sighting first, however late it was collected.
func TestUndatedEntriesComeLast(t *testing.T) {
	srv := server(t)
	at := func(observed, doc string) {
		t.Helper()
		if status, body := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"`+observed+`","observed_by":"test","document":`+doc+`}`); status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, body)
		}
	}
	// Two likes of posts with no time, sighted late; a like of a dated post, sighted early.
	at("2026-11-17T01:00:00Z", `{"type":"Note","id":"xhs:note:a","content":"undated first","url":"https://x/a"}`)
	at("2026-11-17T01:00:01Z", `{"type":"Like","actor":"xhs:user:1","object":"xhs:note:a"}`)
	at("2026-11-17T02:00:00Z", `{"type":"Note","id":"xhs:note:b","content":"undated second","url":"https://x/b"}`)
	at("2026-11-17T02:00:01Z", `{"type":"Like","actor":"xhs:user:1","object":"xhs:note:b"}`)
	at("2026-11-01T00:00:00Z", `{"type":"Note","id":"weibo:post:1","content":"dated","published":"2020-01-01T00:00:00Z"}`)
	at("2026-11-01T00:00:01Z", `{"type":"Like","actor":"weibo:user:1","object":"weibo:post:1"}`)

	_, body := do(t, "GET", srv.URL+"/api/entries", "")
	var page struct {
		Entries []struct {
			Quote *struct {
				Content string `json:"content"`
			} `json:"quote"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil || len(page.Entries) != 3 {
		t.Fatalf("entries = %s (%v)", body, err)
	}
	var order []string
	for _, e := range page.Entries {
		order = append(order, e.Quote.Content)
	}
	if got, want := strings.Join(order, ","), "dated,undated second,undated first"; got != want {
		t.Errorf("order = %s, want %s", got, want)
	}
}

// A post of the owner's that quotes another carries it as a card, as a repost does; a quote can
// also be a reply; the quoted post is found by search; a quote is an ID, never an embedded document.
func TestPostThatQuotesAnother(t *testing.T) {
	srv := server(t)
	ingest := func(doc string) (int, string) {
		return do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`)
	}
	for _, doc := range []string{
		`{"type":"Person","id":"x:user:9","name":"Chun"}`,
		`{"type":"Note","id":"x:post:100","attributedTo":"x:user:9","content":"the original thing","url":"https://x.com/chun/status/100","published":"2026-11-10T00:00:00Z"}`,
		`{"type":"Note","id":"x:post:99","attributedTo":"x:user:9","content":"what it answers","published":"2026-11-09T00:00:00Z"}`,
		`{"type":"Note","id":"x:post:200","attributedTo":"x:user:1","content":"my take","quote":"x:post:100","published":"2026-11-12T00:00:00Z"}`,
		`{"type":"Create","actor":"x:user:1","object":"x:post:200","published":"2026-11-12T00:00:00Z"}`,
		`{"type":"Note","id":"x:post:201","attributedTo":"x:user:1","content":"and an answer","inReplyTo":"x:post:99","quote":"x:post:100","published":"2026-11-13T00:00:00Z"}`,
		`{"type":"Create","actor":"x:user:1","object":"x:post:201","published":"2026-11-13T00:00:00Z"}`,
	} {
		if status, body := ingest(doc); status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, body)
		}
	}
	if status, _ := ingest(`{"type":"Note","id":"x:post:300","content":"x","quote":{"id":"x:post:100"}}`); status != 400 {
		t.Errorf("an embedded quote = %d, want 400", status)
	}
	_, body := do(t, "GET", srv.URL+"/api/entries", "")
	var page struct {
		Entries []struct {
			Type string `json:"type"`
			Post *struct {
				Content   string `json:"content"`
				InReplyTo string `json:"inReplyTo"`
			} `json:"post"`
			ReplyTo *struct {
				Content string `json:"content"`
			} `json:"replyTo"`
			Quote *struct {
				Author  string `json:"author"`
				Content string `json:"content"`
			} `json:"quote"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil || len(page.Entries) != 2 {
		t.Fatalf("entries = %s (%v)", body, err)
	}
	answer, quote := page.Entries[0], page.Entries[1] // newest first
	if quote.Type != "Create" || quote.Post == nil || quote.Post.Content != "my take" || quote.Post.InReplyTo != "" || quote.ReplyTo != nil {
		t.Errorf("the quote = %+v, want the owner's own post, answering nothing", quote)
	}
	if quote.Quote == nil || quote.Quote.Author != "Chun" || quote.Quote.Content != "the original thing" {
		t.Errorf("the quoted card = %+v", quote.Quote)
	}
	if answer.Type != "Create" || answer.Post == nil || answer.Post.InReplyTo != "x:post:99" || answer.ReplyTo == nil || answer.ReplyTo.Content != "what it answers" || answer.Quote == nil {
		t.Errorf("a quote that answers = %+v, want a reply with the answered post and the quoted card", answer)
	}
	for _, q := range []string{"original", "Chun"} {
		if _, body := do(t, "GET", srv.URL+"/api/entries?q="+q, ""); strings.Count(body, `"ref"`) != 2 {
			t.Errorf("search %q: %s", q, body)
		}
	}
}

// Something the owner kept can be their own note that quotes a post (a sentence underlined in an
// answer): the entry shows the sentence and the answer as the card.
func TestKeptNoteThatQuotesAPost(t *testing.T) {
	srv := server(t)
	for _, doc := range []string{
		`{"type":"Person","id":"z:user:1","name":"me"}`,
		`{"type":"Person","id":"z:user:2","name":"Writer"}`,
		`{"type":"Note","id":"z:answer:5","attributedTo":"z:user:2","name":"A question","content":"the whole answer"}`,
		`{"type":"Note","id":"z:highlight:7","attributedTo":"z:user:1","content":"the underlined sentence","quote":"z:answer:5"}`,
		`{"type":"Collection","id":"z:collection:highlights","name":"Highlights","attributedTo":"z:user:1"}`,
		`{"type":"Add","actor":"z:user:1","object":"z:highlight:7","target":"z:collection:highlights"}`,
	} {
		if status, body := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`); status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, body)
		}
	}
	bindOwner(t, srv, "z:user:1")
	_, body := do(t, "GET", srv.URL+"/api/entries", "")
	var page struct {
		Entries []struct {
			Type      string `json:"type"`
			Published string `json:"published"`
			Post      *struct {
				Content   string `json:"content"`
				Published string `json:"published"`
			} `json:"post"`
			Quote *struct {
				Author string `json:"author"`
				Title  string `json:"title"`
			} `json:"quote"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil || len(page.Entries) != 1 {
		t.Fatalf("entries = %s (%v)", body, err)
	}
	// Nothing says when it was kept: no time of the activity, none of the note.
	if e := page.Entries[0]; e.Type != "Add" || e.Post == nil || e.Post.Content != "the underlined sentence" || e.Quote == nil || e.Quote.Author != "Writer" || e.Quote.Title != "A question" || e.Published != "" || e.Post.Published != "" {
		t.Errorf("entry = %+v, want the sentence with the answer as the card", e)
	}
}

// A video is shown as its title and cover: its description is the summary, which is not shown but
// is searched.
func TestVideoShowsItsTitleAndSearchFindsItsSummary(t *testing.T) {
	srv := server(t)
	for _, doc := range []string{
		`{"type":"Person","id":"bilibili:user:1","name":"me"}`,
		`{"type":"Person","id":"bilibili:user:2","name":"UP"}`,
		`{"type":"Video","id":"bilibili:video:BV1","attributedTo":"bilibili:user:2","name":"A talk","summary":"a long description of the talk"}`,
		`{"type":"Add","actor":"bilibili:user:1","object":"bilibili:video:BV1","target":"bilibili:collection:1"}`,
	} {
		if status, body := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`); status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, body)
		}
	}
	_, body := do(t, "GET", srv.URL+"/api/entries", "")
	var page struct {
		Entries []struct {
			Quote *struct {
				Title string `json:"title"`
				HTML  string `json:"html"`
			} `json:"quote"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil || len(page.Entries) != 1 || page.Entries[0].Quote == nil {
		t.Fatalf("entries = %s (%v)", body, err)
	}
	if q := page.Entries[0].Quote; q.Title != "A talk" || q.HTML != "" {
		t.Errorf("card = %+v, want the title and no description", q)
	}
	if _, body := do(t, "GET", srv.URL+"/api/entries?q=long+description", ""); strings.Count(body, `"ref"`) != 1 {
		t.Errorf("a search for the description: %s", body)
	}
}

// The time of a repost links to the repost itself; the quoted post is opened by its card.
func TestRepostTimeLinksToTheRepost(t *testing.T) {
	srv := server(t)
	for _, doc := range []string{
		`{"type":"Note","id":"weibo:post:5","attributedTo":"weibo:user:2","content":"original","url":"https://weibo.com/2/orig","published":"2026-11-01T00:00:00Z"}`,
		`{"type":"Announce","id":"weibo:post:6","actor":"weibo:user:1","object":"weibo:post:5","content":"my words","url":"https://weibo.com/1/repost","published":"2026-11-02T00:00:00Z"}`,
	} {
		if status, body := do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`); status != 200 && status != 201 {
			t.Fatalf("ingest = %d %s", status, body)
		}
	}
	var page struct {
		Entries []struct {
			URL   string `json:"url"`
			Quote *struct {
				URL string `json:"url"`
			} `json:"quote"`
		} `json:"entries"`
	}
	_, body := do(t, "GET", srv.URL+"/api/entries", "")
	if err := json.Unmarshal([]byte(body), &page); err != nil || len(page.Entries) != 1 {
		t.Fatalf("entries = %s (%v)", body, err)
	}
	if e := page.Entries[0]; e.URL != "https://weibo.com/1/repost" || e.Quote == nil || e.Quote.URL != "https://weibo.com/2/orig" {
		t.Fatalf("the repost links to %q and its card to %+v, want the repost and the original", e.URL, e.Quote)
	}
}

// A post's page is /<ref>: the page for an entry that exists, a 404 for anything else.
func TestPostPageAddress(t *testing.T) {
	srv := server(t)
	for _, doc := range []string{
		`{"type":"Note","id":"weibo:post:31","content":"x","published":"2026-11-13T00:00:00Z"}`,
		`{"type":"Create","actor":"weibo:user:1","object":"weibo:post:31","published":"2026-11-13T00:00:00Z"}`,
	} {
		do(t, "POST", srv.URL+"/collect/ingest", `{"observed_at":"2026-11-14T00:00:00Z","observed_by":"test","document":`+doc+`}`)
	}
	_, body := do(t, "GET", srv.URL+"/api/entries", "")
	var page struct {
		Entries []struct{ Ref string } `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil || len(page.Entries) != 1 {
		t.Fatalf("entries = %s (%v)", body, err)
	}
	ref := page.Entries[0].Ref
	if len(ref) != 8 {
		t.Fatalf("ref %q is not 8 characters", ref)
	}
	for path, want := range map[string]int{"/" + ref: 200, "/nosuchpath": 404, "/post/" + ref: 404, "/zzzzzzzz": 404, "/ingest": 404} {
		if status, _ := do(t, "GET", srv.URL+path, ""); status != want && !(want == 200 && status == 503) {
			t.Errorf("GET %s = %d, want %d", path, status, want)
		}
	}
}
