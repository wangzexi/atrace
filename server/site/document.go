// Document: the stored document the page reads, and the entries and posts the API shows it as.
package site

import (
	"encoding/json"
)

// document is the part of an ActivityStreams document the page shows.
type document struct {
	ID           string          `json:"id"`
	MediaType    string          `json:"mediaType"`
	InReplyTo    string          `json:"inReplyTo"`
	Quote        string          `json:"quote"`
	Name         string          `json:"name"`
	AttributedTo string          `json:"attributedTo"`
	Content      string          `json:"content"`
	URL          json.RawMessage `json:"url"`
	Published    string          `json:"published"`
	Attachment   []struct {
		Type    string `json:"type"`
		URL     string `json:"url"`
		Preview *struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"preview"`
	} `json:"attachment"`
}

// link returns the document's URL when it is a plain string.
func (d document) link() string {
	var u string
	if json.Unmarshal(d.URL, &u) != nil {
		return ""
	}
	return safeWebURL(u)
}

// allKinds are the activity types the timeline shows.
var allKinds = []string{"Create", "Reply", "Announce", "Like", "Add"}

// Entry is one line of the timeline in standard fields. The page decides how it looks: the verb, which
// time to show, what a like hides. Post is the owner's own words, when the entry has any; Quote is another
// post shown as a card (what a repost repeats, what a like or a favorite points at, what a post quotes);
// ReplyTo is the post a reply answers.
type Entry struct {
	ActivityID int64  `json:"activityId"`
	Ref        string `json:"ref"`
	Gone       bool   `json:"gone"`
	Dropped    bool   `json:"dropped"`
	Public     bool   `json:"public"`
	Alias      string `json:"alias"`
	// ID is the post the entry is about; for a repost, the repost's own ID.
	ID   string `json:"id"`
	Type string `json:"type"`
	// Actor is who acted: the owner's name. ActorID is the account, for the owner to open its person.
	Actor   string `json:"actor"`
	ActorID string `json:"actorId,omitempty"`
	// URL is the entry's own permalink: for a repost, the repost, not the post it repeats.
	URL string `json:"url"`
	// Published is the activity's own time and Observed when it was collected; either can be all there is.
	Published string  `json:"published"`
	Observed  string  `json:"observed"`
	Post      *Post   `json:"post"`
	Quote     *Post   `json:"quote"`
	ReplyTo   *Post   `json:"replyTo"`
	Replies   []Reply `json:"replies"`
}

// Post is a post as the page draws it: the text as it was written, never as HTML.
type Post struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	AuthorID  string `json:"authorId,omitempty"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Markdown  bool   `json:"markdown"`
	Published string `json:"published"`
	URL       string `json:"url"`
	// InReplyTo is the ID of the post this one answers, whether or not that post was collected.
	InReplyTo string `json:"inReplyTo,omitempty"`
	// Images and Videos use the archive's copy where there is one.
	Images []string `json:"images"`
	Videos []string `json:"videos"`
	// LivePhotos pairs a still image with its motion clip.
	LivePhotos map[string]string `json:"livePhotos,omitempty"`
	// Attachments maps the original address of each picture the archive holds to its copy, for
	// the Markdown that shows pictures in place.
	Attachments map[string]string `json:"attachments,omitempty"`
}

// Reply is a comment under a post.
type Reply struct {
	Author    string `json:"author"`
	AuthorID  string `json:"authorId,omitempty"`
	Published string `json:"published"`
	Content   string `json:"content"`
	// ID names the comment for the owner's edits; Gone is set when a re-check found it deleted on its platform.
	ID   string `json:"id,omitempty"`
	Gone bool   `json:"gone,omitempty"`
	// Ref names the comment for the owner's edits; ActivityID and Public are the owner's own entry for it, when there is one.
	Ref        string `json:"ref,omitempty"`
	ActivityID int64  `json:"activityId,omitempty"`
	Public     bool   `json:"public,omitempty"`
	// ReplyTo is who a reply inside a thread answers; empty for a comment on the post itself.
	ReplyTo string `json:"replyTo,omitempty"`
}
