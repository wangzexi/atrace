// Thread: the comments under a post, as one flat list, and what a visitor is not shown of them.
package site

import (
	"context"
	"encoding/json"

	"git.zexi.me/zexi/atrace/server/auth"
)

// A thread is cut at this many comments, however deep a platform lets people reply.
const maxThreadReplies = 500

// thread appends the replies under id to out: every reply at any depth, each before the replies to it. A reply names
// the author it answers in ReplyTo; parent is that author for the replies directly under id. budget counts what is
// left to show.
func (renderer *Renderer) thread(ctx context.Context, names map[string]string, id, parent string, budget *int, out *[]Reply) error {
	records, err := renderer.feed.Replies(ctx, id)
	if err != nil {
		return err
	}
	for _, rec := range records {
		if *budget <= 0 {
			break
		}
		*budget--
		var reply document
		if err := json.Unmarshal(rec.Document, &reply); err != nil {
			return err
		}
		// Every comment is an entry of its author's, the same as a post. A visitor sees one only when the owner made
		// it public; nobody sees one once it is hidden.
		entry, err := renderer.feed.ObjectEntry(ctx, rec.ID)
		if err != nil {
			return err
		}
		if entry.Hidden || (!auth.Owner(ctx) && !entry.Public) {
			continue
		}
		item := Reply{
			Ref:        entry.Ref,
			ActivityID: entry.ActivityID,
			Public:     entry.Public,
			Author:     renderer.nameOf(ctx, names, reply.AttributedTo),
			AuthorID:   reply.AttributedTo,
			Published:  reply.Published,
			Content:    reply.Content,
			ID:         rec.ID,
			Gone:       rec.Gone,
			ReplyTo:    parent,
		}
		*out = append(*out, item)
		if err := renderer.thread(ctx, names, rec.ID, item.Author, budget, out); err != nil {
			return err
		}
	}
	return nil
}

// withoutOwnerMarks takes the owner's flags off a thread of comments before a visitor sees it.
func withoutOwnerMarks(replies []Reply) []Reply {
	for i := range replies {
		replies[i].ID, replies[i].Gone, replies[i].AuthorID = "", false, ""
		replies[i].Ref, replies[i].ActivityID, replies[i].Public = "", 0, false
	}
	return replies
}
