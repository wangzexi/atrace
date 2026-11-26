package site

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMarkdownExcerptBoundsLinesAndKeepsShortContent(t *testing.T) {
	for _, test := range []struct{ name, text, want string }{
		{"short", "一行\n二行", "一行\n二行"},
		{"exactly five", "1\n2\n3\n4\n5", "1\n2\n3\n4\n5"},
		{"more than five", "1\n2\n3\n4\n5\n6", "1\n2\n3\n4\n5\n…"},
		{"blank lines", "1\n\n2", "1\n2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := markdownExcerpt(test.text); got != test.want {
				t.Fatalf("%q != %q", got, test.want)
			}
		})
	}
	got := markdownExcerpt(strings.Repeat("长", 1000))
	lines := strings.Split(got, "\n")
	if len(lines) != 6 || lines[5] != "…" {
		t.Fatal("unbounded excerpt", got)
	}
	for _, line := range lines[:5] {
		if utf8.RuneCountInString(line) != 120 {
			t.Fatal("broken Unicode or line width")
		}
	}
	if got := markdownExcerpt("![image](https://example.com/a.jpg)"); strings.Contains(got, "![image]") {
		t.Fatal("preview embeds an image", got)
	}
}

func TestTimelineMarkdownMarksMediaWithoutEmbedding(t *testing.T) {
	for _, test := range []struct {
		name  string
		entry Entry
		want  string
	}{
		{"text only", Entry{Post: &Post{Content: "正文"}}, ""},
		{"images", Entry{Post: &Post{Images: []string{"/a.jpg", "/b.jpg"}}}, "[图片]"},
		{"quoted video", Entry{Quote: &Post{Videos: []string{"/a.mp4"}}}, "> [视频]"},
		{"reply and duplicate media", Entry{Post: &Post{Images: []string{"/a.jpg"}}, ReplyTo: &Post{Images: []string{"/a.jpg"}, Videos: []string{"/a.mp4"}}}, "> [图片] [视频]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := entryMarkdown(test.entry, "https://example.com", false)
			if test.want != "" && !strings.Contains(got, test.want+"\n") {
				t.Fatalf("missing media summary %q: %s", test.want, got)
			}
			if test.want == "" && (strings.Contains(got, "图片") || strings.Contains(got, "视频")) {
				t.Fatalf("unexpected media summary: %s", got)
			}
			if strings.Contains(got, "/a.") || strings.Contains(got, "/b.") {
				t.Fatalf("timeline embeds media URLs: %s", got)
			}
		})
	}
}

func TestTimelineMarkdownKeepsQuotedMediaWithQuote(t *testing.T) {
	got := entryMarkdown(Entry{Post: &Post{Content: "自己的内容", Images: []string{"/a.jpg"}}, Quote: &Post{Author: "别人", Content: "引用内容", Videos: []string{"/a.mp4"}}}, "", false)
	if !strings.Contains(got, "自己的内容\n[图片]\n\n> 别人") || !strings.Contains(got, "\n> [视频]\n") {
		t.Fatalf("media attribution lost: %s", got)
	}
}

// An address with a parenthesis or bracket cannot end the Markdown link early and start another.
func TestMarkdownLinkCannotEndEarly(t *testing.T) {
	evil := "https://a.example/p)[x](https://evil.example/"
	link := "[原帖](" + mdLinkEscaper.Replace(evil) + ")"
	if strings.Count(link, "](") != 1 || strings.Count(link, ")") != 1 {
		t.Fatalf("the link has more than one target: %s", link)
	}
}
