package core

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestCursorPagesSurviveNewAndHiddenEntries(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	add := func(id int, at string) {
		ingest(t, s, fmt.Sprintf(`{"type":"Note","id":"weibo:post:%d","content":"page fixture","published":%q}`, id, at))
		ingest(t, s, fmt.Sprintf(`{"type":"Create","actor":"weibo:user:1","object":"weibo:post:%d","published":%q}`, id, at))
	}
	for i := 1; i <= 5; i++ {
		add(i, "2026-11-10T00:00:00Z")
	}
	all, err := s.TimelinePage(ctx, 0, nil, 100, false, "", false, PageRange{})
	if err != nil {
		t.Fatal(err)
	}
	first := all[:2]
	cursor := first[1].Ref
	add(6, "2026-11-11T00:00:00Z")
	flag(t, s, `INSERT INTO object_gone (object_id,gone_at) VALUES (?,?)`, first[1].Activity.Object, "2026-11-20T00:00:00Z")
	if err := s.HideActivity(ctx, first[1].Activity.ID); err != nil {
		t.Fatal(err)
	}
	next, err := s.TimelinePage(ctx, 0, nil, 100, false, "", false, PageRange{After: cursor})
	if err != nil || len(next) != 3 {
		t.Fatal("unstable cursor", len(next), err)
	}
	for i := range next {
		if next[i].Ref != all[i+2].Ref {
			t.Fatal("duplicate or skipped entry")
		}
	}
	_, err = s.TimelinePage(ctx, 0, nil, 10, false, "", false, PageRange{After: "missing"})
	if !errors.Is(err, ErrCursor) {
		t.Fatal("missing cursor accepted", err)
	}
	if err := s.SetPublic(ctx, all[2].Activity.ID, true); err != nil {
		t.Fatal(err)
	}
	_, err = s.TimelinePage(ctx, 0, nil, 10, true, "", false, PageRange{After: cursor})
	if !errors.Is(err, ErrCursor) {
		t.Fatal("private cursor exposed", err)
	}
}

func TestDateRangeUsesBeijingBoundariesAndExcludesUnknownDates(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	for i, at := range []string{"2026-10-31T15:59:59Z", "2026-10-31T16:00:00Z", "2026-11-30T15:59:59Z", "2026-11-30T16:00:00Z"} {
		ingest(t, s, fmt.Sprintf(`{"type":"Note","id":"weibo:post:%d","published":%q}`, i, at))
		ingest(t, s, fmt.Sprintf(`{"type":"Like","actor":"weibo:user:1","object":"weibo:post:%d"}`, i))
	}
	ingest(t, s, `{"type":"Note","id":"weibo:post:unknown"}`)
	ingest(t, s, `{"type":"Like","actor":"weibo:user:1","object":"weibo:post:unknown"}`)
	all, err := s.TimelinePage(ctx, 0, []string{"Like"}, 100, false, "", false, PageRange{})
	if err != nil {
		t.Fatal(err)
	}
	cursor := ""
	for _, expected := range all {
		next, err := s.TimelinePage(ctx, 0, []string{"Like"}, 1, false, "", false, PageRange{After: cursor})
		if err != nil || len(next) != 1 || next[0].Ref != expected.Ref {
			t.Fatal("lost dated/undated transition", next, err)
		}
		cursor = next[0].Ref
	}
	tail, err := s.TimelinePage(ctx, 0, []string{"Like"}, 1, false, "", false, PageRange{After: cursor})
	if err != nil || len(tail) != 0 {
		t.Fatal("cursor did not reach end", tail, err)
	}
	f, err := ParsePageRange("", "2026-11-01", "2026-12-01", "")
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.TimelinePage(ctx, 0, []string{"Like"}, 100, false, "", false, f)
	if err != nil || len(items) != 2 || items[0].Activity.Object != "weibo:post:2" || items[1].Activity.Object != "weibo:post:1" {
		t.Fatal("wrong date boundaries", items, err)
	}
	for _, v := range [][3]string{{"!", "", ""}, {"", "bad", ""}, {"", "2026-11-02", "2026-11-01"}, {"", "2026-11-01", "2026-11-01"}} {
		if _, err := ParsePageRange(v[0], v[1], v[2], ""); err == nil {
			t.Fatal("accepted invalid range", v)
		}
	}
}
