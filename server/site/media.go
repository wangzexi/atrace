// Media: the pictures and videos of a document.
package site

import (
	"context"
	"net/url"
	"strings"

	"git.zexi.me/zexi/atrace/server/core"
)

// media lists the pictures and videos of a document, using the archive's copy where there is one, and
// maps the original address of each picture that has a copy to it.
func (renderer *Renderer) media(ctx context.Context, doc document) (images, videos []string, copies, livePhotos map[string]string, err error) {
	urls := make([]string, 0, len(doc.Attachment))
	for _, a := range doc.Attachment {
		urls = append(urls, a.URL)
		if a.Preview != nil && a.Preview.Type == "Image" {
			urls = append(urls, a.Preview.URL)
		}
	}
	local, err := renderer.feed.MediaRefs(ctx, urls)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	copies = map[string]string{}
	livePhotos = map[string]string{}
	for _, a := range doc.Attachment {
		src := safeWebURL(a.URL)
		if ref := local[a.URL]; core.ValidFileRef(ref) {
			src = "/files/" + ref
			if a.Type == "Image" {
				copies[a.URL] = src
			}
		}
		if src == "" {
			continue
		}
		switch a.Type {
		case "Image":
			images = append(images, src)
		case "Video":
			videos = append(videos, src)
			if a.Preview != nil && a.Preview.Type == "Image" {
				still := safeWebURL(a.Preview.URL)
				if ref := local[a.Preview.URL]; core.ValidFileRef(ref) {
					still = "/files/" + ref
				}
				if still != "" {
					livePhotos[still] = src
				}
			}
		}
	}
	return images, videos, copies, livePhotos, nil
}

func safeWebURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return ""
	}
	return u.String()
}
