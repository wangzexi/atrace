package site

// The archive as Markdown, for a program that reads it (an AI): the timeline at /index.md, which is also
// where it is searched and paged, and one post at /<ref>.md with its comments. They are for visitors: they show
// the public entries and nothing the owner keeps private, whoever asks. What is private, and everything about
// how it is stored, is read through /admin.

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"git.zexi.me/zexi/atrace/server/auth"
	"git.zexi.me/zexi/atrace/server/core"
)

const (
	markdownType = "text/markdown; charset=utf-8"
	// timelineMarkdownDefaultLimit and timelineMarkdownMaxLimit are how many entries a page of /index.md has by default and at most.
	timelineMarkdownDefaultLimit = 20
	timelineMarkdownMaxLimit     = 100
)

// shown is the timezone used for Markdown dates.
var shown = time.FixedZone("Asia/Shanghai", 8*60*60)

// kindLabels name what an entry is, in the words of the page.
var kindLabels = map[string]string{"Create": "原创", "Reply": "回复", "Announce": "转发", "Like": "点赞", "Add": "收藏"}

// alternate says, in the headers of a page, where its Markdown is; the answer depends on what the request accepts.
func alternate(w http.ResponseWriter, href string) {
	w.Header().Add("Link", "<"+href+`>; rel="alternate"; type="text/markdown"`)
	w.Header().Add("Vary", "Accept")
}

// prefersMarkdown is whether the request asks for Markdown before HTML. A program that reads the page says so in
// Accept (text/markdown, with or without text/html after it); a browser asks for text/html and does not.
func prefersMarkdown(r *http.Request) bool {
	quality := map[string]float64{}
	position := map[string]int{}
	for i, part := range strings.Split(r.Header.Get("Accept"), ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		media := strings.ToLower(strings.TrimSpace(fields[0]))
		q := 1.0
		for _, field := range fields[1:] {
			if value, ok := strings.CutPrefix(strings.TrimSpace(field), "q="); ok {
				if parsed, err := strconv.ParseFloat(value, 64); err == nil {
					q = parsed
				}
			}
		}
		if _, seen := quality[media]; !seen {
			quality[media], position[media] = q, i
		}
	}
	markdown, html := quality["text/markdown"], quality["text/html"]
	if markdown <= 0 {
		return false
	}
	return markdown > html || (markdown == html && position["text/markdown"] < position["text/html"])
}

// baseOf is the address the request came to, for links that are followed from outside.
func baseOf(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// timelineMarkdown serves the timeline as Markdown for visitors, newest first: GET /index.md?after=&limit=. It does not
// filter. The archive terminal can search by text and read an entry.
func (renderer *Renderer) timelineMarkdown(w http.ResponseWriter, r *http.Request) {
	page, limit, problem := timelineMarkdownParams(r.URL.Query())
	if problem != "" {
		http.Error(w, problem, http.StatusBadRequest)
		return
	}
	entries, next, more, err := renderer.window(auth.Visitor(r.Context()), 0, page, limit, allKinds, "", false, false)
	if err != nil {
		if errors.Is(err, core.ErrCursor) {
			http.Error(w, err.Error(), 400)
			return
		}
		http.Error(w, "timeline unavailable", http.StatusInternalServerError)
		return
	}
	base := baseOf(r)
	var out strings.Builder
	out.WriteString("# " + renderer.owner + " 的时间线\n\n")
	for _, entry := range entries {
		out.WriteString(entryMarkdown(entry, base, false))
	}
	switch {
	case more:
		params := url.Values{"after": {next}, "limit": {strconv.Itoa(limit)}}
		fmt.Fprintf(&out, "[下一页](%s/index.md?%s)\n", base, params.Encode())
	case len(entries) == 0:
		out.WriteString("这里还没有条目。\n")
	default:
		out.WriteString("已到最后一页。\n")
	}
	fmt.Fprintf(&out, "\n[档案终端](%s/terminal)\n\n[目录说明](%s/terminal.md)\n", base, base)
	writeMarkdown(w, r, out.String())
}

// timelineMarkdownParams reads and checks what /index.md is asked: where a page starts and how long it is.
func timelineMarkdownParams(query url.Values) (page core.PageRange, limit int, problem string) {
	limit = timelineMarkdownDefaultLimit
	var err error
	page, err = core.ParsePageRange(query.Get("after"), "", "", "")
	if err != nil {
		return page, 0, err.Error()
	}
	if raw := query.Get("limit"); raw != "" {
		if limit, err = strconv.Atoi(raw); err != nil || limit < 1 || limit > timelineMarkdownMaxLimit {
			return page, 0, fmt.Sprintf("limit must be from 1 to %d", timelineMarkdownMaxLimit)
		}
	}
	return page, limit, ""
}

// entryListParams reads and checks what the web page asks of the entries it lists: paging, search and the kinds
// and visibility to show. The page is the only caller that filters.
func entryListParams(query url.Values) (page core.PageRange, limit int, text string, regex bool, kinds []string, problem string) {
	limit = timelineMarkdownDefaultLimit
	var err error
	page, err = core.ParsePageRange(query.Get("after"), query.Get("since"), query.Get("until"), query.Get("visibility"))
	if err != nil {
		return page, 0, "", false, nil, err.Error()
	}
	if raw := query.Get("limit"); raw != "" {
		if limit, err = strconv.Atoi(raw); err != nil || limit < 1 || limit > timelineMarkdownMaxLimit {
			return page, 0, "", false, nil, fmt.Sprintf("limit must be from 1 to %d", timelineMarkdownMaxLimit)
		}
	}
	text = strings.TrimSpace(query.Get("q"))
	if utf8.RuneCountInString(text) > 200 {
		return page, 0, "", false, nil, "q is at most 200 characters"
	}
	switch query.Get("regex") {
	case "", "0":
	case "1":
		regex = text != ""
	default:
		return page, 0, "", false, nil, "regex must be 0 or 1"
	}
	if regex {
		if err := core.ValidSearchPattern(text); err != nil {
			return page, 0, "", false, nil, "invalid regular expression: " + err.Error()
		}
	}
	kinds = allKinds
	if raw := query.Get("types"); raw != "" {
		kinds = nil
		for _, kind := range strings.Split(raw, ",") {
			if !slices.Contains(allKinds, kind) {
				return page, 0, "", false, nil, "types must be among Create, Reply, Announce, Like, Add"
			}
			kinds = append(kinds, kind)
		}
	}
	return page, limit, text, regex, kinds, ""
}

// post serves one entry as Markdown: GET /<ref>.md.
func (renderer *Renderer) postMarkdown(w http.ResponseWriter, r *http.Request, ref string) {
	ctx := auth.Visitor(r.Context())
	item, err := renderer.feed.TimelineItem(ctx, ref, true)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	entry, err := renderer.entryOf(ctx, item, map[string]string{}, true)
	if err != nil {
		http.Error(w, "entry unavailable", http.StatusInternalServerError)
		return
	}
	writeMarkdown(w, r, entryMarkdown(visibleTo(false, entry), baseOf(r), true))
}

func writeMarkdown(w http.ResponseWriter, r *http.Request, body string) {
	// Browsers commonly download text/markdown instead of displaying it.
	contentType := "text/plain; charset=utf-8"
	if prefersMarkdown(r) {
		contentType = markdownType
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Add("Vary", "Accept")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(body))
	}
}

// entryMarkdown is one entry: in the timeline a section of its own, as a post the whole of it.
func entryMarkdown(entry Entry, base string, whole bool) string {
	var out strings.Builder
	label := kindLabels[entry.Type]
	if entry.Post != nil && entry.Post.InReplyTo != "" {
		label = kindLabels["Reply"]
	}
	when := timeLabel(entryTime(entry))
	if whole {
		out.WriteString("---\n")
		fmt.Fprintf(&out, "类型: %s\n", entry.Type)
		if when != "" {
			fmt.Fprintf(&out, "时间: %q\n", when)
		}
		if entry.Actor != "" {
			fmt.Fprintf(&out, "作者: %q\n", entry.Actor)
		}
		if entry.URL != "" {
			fmt.Fprintf(&out, "原文: %q\n", entry.URL)
		}
		out.WriteString("---\n\n")
		if title := titleOf(entry); title != "" {
			out.WriteString("# " + title + "\n\n")
		}
	} else {
		date, _, _ := strings.Cut(when, " ")
		out.WriteString(joinTitle("["+entry.Ref+"]", date, entry.Actor, label, markdownPreview(titleOf(entry), 120)) + "\n\n")
	}
	if !whole {
		for _, post := range []*Post{entry.Post, entry.Quote, entry.ReplyTo} {
			if post == nil {
				continue
			}
			text := strings.TrimSpace(post.Content)
			if post != entry.Post {
				text = joinTitle(post.Author, post.Title, text)
			}
			var media []string
			if len(post.Images) > 0 {
				media = append(media, "[图片]")
			}
			if len(post.Videos) > 0 {
				media = append(media, "[视频]")
			}
			preview := markdownExcerpt(text)
			if len(media) > 0 {
				preview = strings.TrimSpace(preview + "\n" + strings.Join(media, " "))
			}
			if preview != "" {
				if post != entry.Post {
					preview = "> " + strings.ReplaceAll(preview, "\n", "\n> ")
				}
				out.WriteString(preview + "\n\n")
			}
		}
		return out.String()
	}
	if entry.ReplyTo != nil {
		out.WriteString("回复：\n\n" + quoteMarkdown(entry.ReplyTo, base))
	}
	if entry.Post != nil {
		if entry.Post.Content != "" {
			out.WriteString(strings.TrimSpace(entry.Post.Content) + "\n\n")
		}
		out.WriteString(imagesMarkdown(entry.Post, base))
	}
	if entry.Quote != nil {
		if entry.Type == "Create" || entry.Post != nil {
			out.WriteString("引用：\n\n")
		}
		out.WriteString(quoteMarkdown(entry.Quote, base))
	}
	if len(entry.Replies) > 0 {
		fmt.Fprintf(&out, "## 评论（%d）\n\n", len(entry.Replies))
		writeReplies(&out, entry.Replies)
		out.WriteString("\n")
	}

	return out.String()
}

// markdownExcerpt preserves line breaks and wraps long lines within a five-line preview.
func markdownExcerpt(text string) string {
	const width = 120
	const maxLines = 5
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		runes := []rune(strings.Join(strings.Fields(line), " "))
		for len(runes) > 0 {
			if len(lines) == maxLines {
				return strings.Join(lines, "\n") + "\n…"
			}
			end := min(width, len(runes))
			lines = append(lines, markdownPreview(string(runes[:end]), width))
			runes = runes[end:]
		}
	}
	return strings.Join(lines, "\n")
}

// markdownPreview keeps excerpts bounded and displays markup literally, including embedded images.
func markdownPreview(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) > limit {
		text = string([]rune(text)[:limit]) + "…"
	}
	return strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "#", "\\#").Replace(text)
}

// titleOf is the title of the owner's own post, if it has one.
func titleOf(entry Entry) string {
	if entry.Post != nil {
		return entry.Post.Title
	}
	return ""
}

// joinTitle is the heading of a section: what is known of when, what kind and which title, in that order.
func joinTitle(parts ...string) string {
	kept := parts[:0:0]
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " ")
}

// entryTime is when the entry says it happened, as the page decides it: nothing for a like or a favorite of
// someone else's post, whose time the card carries.
func entryTime(entry Entry) string {
	reaction := entry.Type == "Like" || entry.Type == "Add"
	if reaction && entry.Post == nil {
		return ""
	}
	if entry.Published != "" {
		return entry.Published
	}
	if entry.Post != nil && entry.Post.Published != "" {
		return entry.Post.Published
	}
	if reaction {
		return ""
	}
	return entry.Observed
}

func timeLabel(value string) string {
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return ""
	}
	return at.In(shown).Format("2006-01-02 15:04")
}

// quoteMarkdown shows another post as a block quote, with who wrote it, when, and where to read it.
func quoteMarkdown(post *Post, base string) string {
	var lines []string
	head := "**" + post.Author + "**"
	if at := timeLabel(post.Published); at != "" {
		head += " · " + at
	}
	if post.URL != "" {
		head += " · [原帖](" + mdLinkEscaper.Replace(post.URL) + ")"
	}
	lines = append(lines, head)
	if post.Title != "" {
		lines = append(lines, "", "**"+post.Title+"**")
	}
	if content := strings.TrimSpace(post.Content); content != "" {
		lines = append(lines, "", content)
	}
	if images := strings.TrimSpace(imagesMarkdown(post, base)); images != "" {
		lines = append(lines, "", images)
	}
	var out strings.Builder
	for _, line := range strings.Split(strings.Join(lines, "\n"), "\n") {
		out.WriteString(strings.TrimRight("> "+line, " ") + "\n")
	}
	return out.String() + "\n"
}

// imagesMarkdown lists a post's pictures with addresses that can be followed from outside (a copy kept by the
// archive is under /files; a private post's pictures need the same token as the post).
// mdLinkEscaper percent-encodes what would end a Markdown link or image early, or start another one.
var mdLinkEscaper = strings.NewReplacer("(", "%28", ")", "%29", "[", "%5B", "]", "%5D", " ", "%20")

func imagesMarkdown(post *Post, base string) string {
	var out strings.Builder
	for _, src := range post.Images {
		if strings.HasPrefix(src, "/") {
			src = base + src
		}
		out.WriteString("![配图](" + mdLinkEscaper.Replace(src) + ")\n\n")
	}
	for _, src := range post.Videos {
		if strings.HasPrefix(src, "/") {
			src = base + src
		}
		out.WriteString("[视频](" + src + ")\n\n")
	}
	return out.String()
}

// writeReplies writes the comments as one list, oldest first; a reply that answers someone says whom.
func writeReplies(out *strings.Builder, replies []Reply) {
	for _, reply := range replies {
		head := "**" + reply.Author + "**"
		if at := timeLabel(reply.Published); at != "" {
			head += " · " + at
		}
		if reply.ReplyTo != "" {
			head += " 回复 " + reply.ReplyTo
		}
		lines := strings.Split(strings.TrimSpace(reply.Content), "\n")
		fmt.Fprintf(out, "- %s：%s\n", head, lines[0])
		for _, line := range lines[1:] {
			fmt.Fprintf(out, "  %s\n", line)
		}
	}
}
