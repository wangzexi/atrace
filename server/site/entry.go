// Entry: building one timeline entry from an activity and the posts it points at.
package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"git.zexi.me/zexi/atrace/server/core"
)

// visibleTo trims an entry to what the viewer may see: a visitor gets neither the
// internal activity ID nor the owner's flags.
func visibleTo(owner bool, entry Entry) Entry {
	if !owner {
		entry.ActivityID, entry.Gone, entry.Dropped, entry.Public, entry.Alias = 0, false, false, false, ""
		// The accounts behind the names are for the owner, who opens a person from them.
		entry.ActorID = ""
		for _, post := range []*Post{entry.Post, entry.Quote, entry.ReplyTo} {
			if post != nil {
				post.AuthorID = ""
			}
		}
		entry.Replies = withoutOwnerMarks(entry.Replies)
	}
	return entry
}

// entryOf builds one timeline entry. It only says which posts the entry is made of; how they look is
// the page's business.
//
//	Create    the owner's post, the post it quotes or answers, the comments collected under it
//	Announce  the post it repeats, as a card
//	Like, Add the post it points at, as a card; unless that is the owner's own note that quotes a post
//	          (a sentence underlined in an answer), which is the owner's post, with the quoted post as a card
//
// withReplies says whether to read the comments under a post of the owner's.
func (renderer *Renderer) entryOf(ctx context.Context, item core.Item, names map[string]string, withReplies bool) (Entry, error) {
	var activity, object document
	if err := json.Unmarshal(item.Activity.Document, &activity); err != nil {
		return Entry{}, err
	}
	if item.Object != nil {
		if err := json.Unmarshal(item.Object, &object); err != nil {
			return Entry{}, err
		}
	}
	kind := item.Activity.Type
	entry := Entry{
		ActivityID: item.Activity.ID,
		Ref:        item.Ref,
		Gone:       item.Gone,
		Dropped:    item.Dropped,
		Public:     item.Public,
		Alias:      item.Alias,
		ID:         item.Activity.Object,
		Type:       kind,
		Actor:      renderer.nameOf(ctx, names, item.Activity.Actor),
		ActorID:    item.Activity.Actor,
		URL:        object.link(),
		Published:  item.Activity.PublishedAt,
		Observed:   item.Activity.ObservedAt,
	}
	if kind == "Announce" {
		// The repost is an entry of its own: its ID and permalink, not those of the post it repeats.
		if activity.ID != "" {
			entry.ID = activity.ID
		}
		entry.URL = activity.link()
	}
	var err error
	switch kind {
	case "Announce":
		if item.Object != nil {
			entry.Quote, err = renderer.post(ctx, names, object)
		}
	case "Like", "Add":
		if item.Object == nil {
			break
		}
		if object.Quote != "" && renderer.isOwners(ctx, object) {
			if entry.Post, err = renderer.post(ctx, names, object); err == nil {
				entry.Quote, err = renderer.postByID(ctx, names, object.Quote)
			}
			break
		}
		entry.Quote, err = renderer.post(ctx, names, object)
	default:
		if item.Object == nil {
			break
		}
		if entry.Post, err = renderer.post(ctx, names, object); err != nil {
			break
		}
		if object.Quote != "" {
			if entry.Quote, err = renderer.postByID(ctx, names, object.Quote); err != nil {
				break
			}
		}
		if object.InReplyTo != "" {
			if entry.ReplyTo, err = renderer.postByID(ctx, names, object.InReplyTo); err != nil {
				break
			}
		}
		if kind == "Create" && withReplies {
			// The owner's own comment under their post is a comment like any other.
			budget := maxThreadReplies
			err = renderer.thread(ctx, names, item.Activity.Object, "", &budget, &entry.Replies)
		}
	}
	return entry, err
}

// isOwners is whether the owner wrote the post, under whatever name the platform knows them.
func (renderer *Renderer) isOwners(ctx context.Context, doc document) bool {
	name, err := renderer.feed.OwnerNameForAccount(ctx, doc.AttributedTo)
	return err == nil && name != ""
}

// post is a document as a post of the page: its text as written and its pictures.
func (renderer *Renderer) post(ctx context.Context, names map[string]string, doc document) (*Post, error) {
	post := &Post{
		ID:        doc.ID,
		Author:    renderer.nameOf(ctx, names, doc.AttributedTo),
		AuthorID:  doc.AttributedTo,
		Title:     doc.Name,
		Content:   doc.Content,
		Markdown:  doc.MediaType == "text/markdown",
		Published: doc.Published,
		URL:       doc.link(),
		InReplyTo: doc.InReplyTo,
	}
	var copies map[string]string
	var err error
	if post.Images, post.Videos, copies, post.LivePhotos, err = renderer.media(ctx, doc); err != nil {
		return nil, err
	}
	if post.Markdown {
		post.Attachments = copies
	}
	return post, nil
}

// postByID is the post with the given ID as a post of the page; nil when it was never collected, as
// then there is nothing to show.
func (renderer *Renderer) postByID(ctx context.Context, names map[string]string, id string) (*Post, error) {
	record, err := renderer.feed.Object(ctx, id, false)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc document
	if err := json.Unmarshal(record.Document, &doc); err != nil {
		return nil, err
	}
	return renderer.post(ctx, names, doc)
}

// nameOf returns the display name of a user, falling back to the ID's last part
// when the user was never collected. names caches lookups for one request.
func (renderer *Renderer) nameOf(ctx context.Context, names map[string]string, ref string) string {
	if ref == "" {
		return ""
	}
	if name, ok := names[ref]; ok {
		return name
	}
	// Every configured person has one display name across their platform accounts.
	if name, _ := renderer.feed.PersonNameForAccount(ctx, ref); name != "" {
		names[ref] = name
		return name
	}
	name := ref[strings.LastIndex(ref, ":")+1:]
	if rec, err := renderer.feed.Object(ctx, ref, false); err == nil {
		var person document
		if json.Unmarshal(rec.Document, &person) == nil && person.Name != "" {
			name = person.Name
		}
	}
	names[ref] = name
	return name
}
