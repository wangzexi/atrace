package core

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// A collector reading a list newest first stops where it meets what is stored. Known says, for the
// activities it has read, which ones ingesting again would not change.
func TestKnown(t *testing.T) {
	s := openTest(t)
	for _, doc := range []string{
		`{"type":"Note","id":"x:post:1","attributedTo":"x:user:1","content":"mine"}`,
		`{"type":"Create","actor":"x:user:1","object":"x:post:1"}`,
		`{"type":"Note","id":"x:post:2","attributedTo":"x:user:2","content":"theirs"}`,
		`{"type":"Like","actor":"x:user:1","object":"x:post:2"}`,
		`{"type":"Collection","id":"x:collection:bookmarks","name":"bookmarks"}`,
		`{"type":"Add","actor":"x:user:1","object":"x:post:2","target":"x:collection:bookmarks"}`,
		`{"type":"Announce","id":"x:post:9","actor":"x:user:1","object":"x:post:2"}`,
	} {
		ingest(t, s, doc)
	}
	ask := func(queries ...KnownQuery) []bool {
		t.Helper()
		got, err := s.Known(context.Background(), queries)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := ask(
		KnownQuery{Actor: "x:user:1", Kind: "Create", Object: "x:post:1"},
		KnownQuery{Actor: "x:user:1", Kind: "Create", Object: "x:post:404"},
		KnownQuery{Actor: "x:user:1", Kind: "Like", Object: "x:post:2"},
		KnownQuery{Actor: "x:user:1", Kind: "Like", Object: "x:post:1"},
		KnownQuery{Actor: "x:user:1", Kind: "Add", Object: "x:post:2", Target: "x:collection:bookmarks"},
		KnownQuery{Actor: "x:user:1", Kind: "Add", Object: "x:post:2", Target: "x:collection:other"},
		KnownQuery{Actor: "x:user:1", Kind: "Announce", ID: "x:post:9"},
		KnownQuery{Actor: "x:user:1", Kind: "Announce", ID: "x:post:10"},
		KnownQuery{Actor: "x:user:7", Kind: "Like", Object: "x:post:2"},
	)
	want := []bool{true, false, true, false, true, false, true, false, false}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("query %d = %v, want %v", i, got[i], want[i])
		}
	}
	// A like taken back is not stored as one: reading it again on the platform has to ingest it.
	ingest(t, s, `{"type":"Undo","actor":"x:user:1","object":{"type":"Like","actor":"x:user:1","object":"x:post:2"}}`)
	if got := ask(KnownQuery{Actor: "x:user:1", Kind: "Like", Object: "x:post:2"}); got[0] {
		t.Error("a like that was undone is known")
	}
	if got := ask(); len(got) != 0 {
		t.Errorf("no questions, answers = %v", got)
	}
}

// The accounts on offer (those the devices name) are nobody's until the owner gives them to a person, and the owner's
// timeline is what the accounts of the person the owner is did.
func TestTheOwnerIsOneOfThePeople(t *testing.T) {
	s := openBare(t)
	ctx := context.Background()
	ingest(t, s, `{"type":"Person","id":"x:user:1","name":"X User"}`)
	ingest(t, s, `{"type":"Note","id":"x:post:1","attributedTo":"x:user:1","content":"mine"}`)
	ingest(t, s, `{"type":"Create","actor":"x:user:1","object":"x:post:1"}`)

	offered, err := s.OfferedAccounts(ctx, []string{"x:user:1"})
	if err != nil || len(offered) != 1 || offered[0].ID != "x:user:1" || offered[0].Name != "X User" {
		t.Fatalf("before anybody has it: %+v %v, want the account on offer and named", offered, err)
	}
	if items, _ := s.TimelinePage(ctx, 0, nil, 10, false, "", false, PageRange{}); len(items) != 0 {
		t.Fatalf("the owner's timeline holds %d entries before there is an owner", len(items))
	}

	id := makeOwner(t, s, "Me", []string{"x:user:1"})
	// What a person has is no longer on offer, and stays theirs when no device offers it any more.
	if offered, err = s.OfferedAccounts(ctx, []string{"x:user:1"}); err != nil || len(offered) != 0 {
		t.Fatalf("after it was given: %+v %v", offered, err)
	}
	if items, _ := s.TimelinePage(ctx, 0, nil, 10, false, "", false, PageRange{}); len(items) != 1 {
		t.Fatalf("the owner's timeline holds %d entries after the account was given, want 1", len(items))
	}
	if name, _ := s.OwnerNameForAccount(ctx, "x:user:1"); name != "Me" {
		t.Errorf("the owner's name on their account = %q", name)
	}
	// The owner is another person once the owner says so; there is no such person otherwise.
	other, err := s.CreatePerson(ctx, "Other", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetOwner(ctx, 9999); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("a person there is not: %v", err)
	}
	if err := s.SetOwner(ctx, other); err != nil {
		t.Fatal(err)
	}
	if items, _ := s.TimelinePage(ctx, 0, nil, 10, false, "", false, PageRange{}); len(items) != 0 {
		t.Errorf("the account of someone who is not the owner is still the owner's: %d entries", len(items))
	}
	if name, _ := s.OwnerNameForAccount(ctx, "x:user:1"); name != "" {
		t.Errorf("the name of someone who is not the owner is given out: %q", name)
	}
	// The owner cannot be deleted, but a person who no longer is the owner can.
	if err := s.DeletePerson(ctx, other); !IsInputError(err) {
		t.Errorf("deleting the owner: %v", err)
	}
	if err := s.DeletePerson(ctx, id); err != nil {
		t.Errorf("deleting a person who is not the owner: %v", err)
	}
}
