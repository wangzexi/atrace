package site

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"git.zexi.me/zexi/atrace/server/core"
)

func TestLivePhotoUsesPairedLocalCopies(t *testing.T) {
	dir := t.TempDir()
	store, err := core.Open(filepath.Join(dir, "test.db"), filepath.Join(dir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	still := "https://wx1.sinaimg.cn/large/photo.jpg"
	motion := "https://video.weibo.com/photo.mov"
	imageRef, err := store.PutFile(ctx, strings.NewReader("still"), still, "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	videoRef, err := store.PutFile(ctx, strings.NewReader("motion"), motion, "video/quicktime")
	if err != nil {
		t.Fatal(err)
	}
	var doc document
	if err := json.Unmarshal([]byte(`{"attachment":[{"type":"Image","url":"`+still+`"},{"type":"Video","url":"`+motion+`","preview":{"type":"Image","url":"`+still+`"}}]}`), &doc); err != nil {
		t.Fatal(err)
	}
	renderer := &Renderer{feed: store}
	images, videos, _, livePhotos, err := renderer.media(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0] != "/files/"+imageRef || len(videos) != 1 || videos[0] != "/files/"+videoRef || livePhotos[images[0]] != videos[0] {
		t.Fatalf("pair lost: %v %v %v", images, videos, livePhotos)
	}
}
