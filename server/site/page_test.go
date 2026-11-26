package site

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

type pageFeed struct {
	FeedQuery
	title string
}

func (f pageFeed) SiteTitle(_ context.Context, fallback string) (string, error) {
	if f.title == "" {
		return fallback, nil
	}
	return f.title, nil
}

func TestPageStartsWithSiteTitle(t *testing.T) {
	for _, title := range []string{"Zexi's Notes", "<Notes> & friends", ""} {
		renderer := Renderer{feed: pageFeed{title: title}, owner: "Owner", web: fstest.MapFS{"index.html": {Data: []byte("<title></title><div id=\"root\"></div>")}}}
		response := httptest.NewRecorder()
		renderer.page(response, httptest.NewRequest("GET", "/", nil))
		want := map[string]string{"Zexi's Notes": "Zexi&#39;s Notes", "<Notes> & friends": "&lt;Notes&gt; &amp; friends", "": "Owner&#39;s Notes"}[title]
		if response.Code != 200 || !strings.Contains(response.Body.String(), "<title>"+want+"</title>") {
			t.Fatalf("title %q: %d %s", title, response.Code, response.Body.String())
		}
	}
}
