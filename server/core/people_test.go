package core

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestPeopleGroupAccountsAcrossPlatforms(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	ingest(t, s, `{"type":"Person","id":"weibo:user:5","name":"阿明"}`)
	ingest(t, s, `{"type":"Note","id":"x:post:5","attributedTo":"x:user:5","content":"hi"}`)

	id, err := s.CreatePerson(ctx, "Ming", []string{"weibo:user:5", "x:user:5"})
	if err != nil {
		t.Fatal(err)
	}
	people, err := s.People(ctx)
	if err != nil || len(people) != 2 {
		t.Fatalf("people = %+v, %v", people, err)
	}
	if !people[0].Owner || people[0].Name != "Alice" {
		t.Errorf("the owner comes first: %+v", people[0])
	}
	ming := people[1]
	if ming.ID != id || ming.Owner || len(ming.Accounts) != 2 || ming.Accounts[0].Name != "阿明" {
		t.Errorf("ming = %+v, want two accounts and the platform's name for the first", ming)
	}
	if got, err := s.PersonOfAccount(ctx, "x:user:5"); err != nil || got != id {
		t.Errorf("person of x:user:5 = %d, %v", got, err)
	}
	if got, _ := s.PersonOfAccount(ctx, "x:user:6"); got != 0 {
		t.Errorf("an account nobody has is %d", got)
	}

	// An account taken by another person moves; a person is given exactly the accounts named.
	other, err := s.CreatePerson(ctx, "Other", []string{"x:user:5"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.PersonOfAccount(ctx, "x:user:5"); got != other {
		t.Errorf("the account did not move to the other person: %d", got)
	}
	if err := s.UpdatePerson(ctx, id, "Ming B", []string{"weibo:user:5"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePerson(ctx, 999, "Nobody", nil); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("updating a person there is not: %v", err)
	}

	// Deleting a person frees their accounts; the owner stays.
	if err := s.DeletePerson(ctx, other); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.PersonOfAccount(ctx, "x:user:5"); got != 0 {
		t.Errorf("a deleted person still has an account: %d", got)
	}
	owner, _ := s.People(ctx)
	if err := s.DeletePerson(ctx, owner[0].ID); !IsInputError(err) {
		t.Errorf("deleting the owner: %v", err)
	}
}

func TestAPersonNeedsANameAndAccountsTheArchiveHasMet(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, err := s.CreatePerson(ctx, "", nil); !IsInputError(err) {
		t.Errorf("no name: %v", err)
	}
	if _, err := s.CreatePerson(ctx, "Ghost", []string{"x:user:404"}); !IsInputError(err) {
		t.Errorf("an account nobody has seen: %v", err)
	}
	people, _ := s.People(ctx)
	if len(people) != 1 {
		t.Errorf("a refused person was kept: %+v", people)
	}
}

// What the owner's accounts did is the owner's timeline; the same entries kept for someone else are theirs.
func TestATimelineIsOnePersonsView(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	ingest(t, s, `{"type":"Note","id":"weibo:post:1","attributedTo":"weibo:user:1","content":"mine"}`)
	ingest(t, s, `{"type":"Create","actor":"weibo:user:1","object":"weibo:post:1","published":"2026-11-13T00:00:00Z"}`)
	ingest(t, s, `{"type":"Note","id":"weibo:comment:1","attributedTo":"weibo:user:5","inReplyTo":"weibo:post:1","content":"nice","published":"2026-11-13T01:00:00Z"}`)
	ingest(t, s, `{"type":"Create","actor":"weibo:user:5","object":"weibo:comment:1","published":"2026-11-13T01:00:00Z"}`)
	ingest(t, s, `{"type":"Note","id":"x:post:5","attributedTo":"x:user:5","content":"over there"}`)
	ingest(t, s, `{"type":"Create","actor":"x:user:5","object":"x:post:5","published":"2026-11-14T00:00:00Z"}`)

	kinds := []string{"Create", "Reply"}
	owners, err := s.TimelinePage(ctx, 0, kinds, 10, false, "", false, PageRange{})
	if err != nil || len(owners) != 1 || owners[0].Activity.Object != "weibo:post:1" {
		t.Fatalf("the owner's timeline = %+v, %v; others' posts are stored the same but are not the owner's", owners, err)
	}
	ming, err := s.CreatePerson(ctx, "Ming", []string{"weibo:user:5", "x:user:5"})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := s.TimelinePage(ctx, ming, kinds, 10, false, "", false, PageRange{})
	if err != nil || len(theirs) != 2 || theirs[0].Activity.Object != "x:post:5" || theirs[1].Activity.Object != "weibo:comment:1" {
		t.Fatalf("Ming's timeline = %+v, %v; want both accounts, newest first", theirs, err)
	}
	// An entry opened by its own ref is found whoever's timeline it is on.
	if item, err := s.TimelineItem(ctx, theirs[0].Ref, false); err != nil || item.Activity.Object != "x:post:5" {
		t.Errorf("an entry of Ming's opened by its ref = %+v, %v", item, err)
	}
	if empty, err := s.TimelinePage(ctx, ming+100, kinds, 10, false, "", false, PageRange{}); err != nil || len(empty) != 0 {
		t.Errorf("the timeline of nobody = %+v, %v", empty, err)
	}
}

func TestOwnerNameIsTheOnlyNameGivenOut(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	ingest(t, s, `{"type":"Person","id":"weibo:user:5","name":"阿明"}`)
	if _, err := s.CreatePerson(ctx, "Ming (classmate)", []string{"weibo:user:5"}); err != nil {
		t.Fatal(err)
	}
	if name, _ := s.OwnerNameForAccount(ctx, "weibo:user:1"); name != "Alice" {
		t.Errorf("the owner's name = %q", name)
	}
	if name, _ := s.OwnerNameForAccount(ctx, "weibo:user:5"); name != "" {
		t.Errorf("the name the owner gave another person leaked: %q", name)
	}
	for account, want := range map[string]string{"weibo:user:1": "Alice", "weibo:user:5": "Ming (classmate)", "weibo:user:unknown": ""} {
		if name, err := s.PersonNameForAccount(ctx, account); err != nil || name != want {
			t.Errorf("configured name for %s = %q, %v; want %q", account, name, err, want)
		}
	}
}

// Before an owner is chosen, people are listed (none the owner) and can be deleted.
func TestPeopleBeforeAnOwnerIsChosen(t *testing.T) {
	s := openBare(t)
	ctx := context.Background()
	id, err := s.CreatePerson(ctx, "阿明", nil)
	if err != nil {
		t.Fatal(err)
	}
	people, err := s.People(ctx)
	if err != nil || len(people) != 1 || people[0].Owner {
		t.Fatalf("people = %+v, %v", people, err)
	}
	if err := s.DeletePerson(ctx, id); err != nil {
		t.Fatalf("deleting a person with no owner chosen: %v", err)
	}
}

func TestPeopleOrderPersistsAndRejectsIncompleteLists(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	initial, _ := s.People(ctx)
	alice := initial[0].ID
	bob, err := s.CreatePerson(ctx, "Bob", nil)
	if err != nil {
		t.Fatal(err)
	}
	carol, err := s.CreatePerson(ctx, "Carol", nil)
	if err != nil {
		t.Fatal(err)
	}
	order := []int64{carol, bob, alice}
	if err := s.OrderPeople(ctx, order); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		people, err := s.People(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i, id := range order {
			if people[i].ID != id || people[i].Owner != (i == 0) {
				t.Fatalf("order = %+v", people)
			}
		}
	}
	check()
	for _, invalid := range [][]int64{nil, {alice}, {alice, bob, bob}, {alice, bob, 9999}} {
		if err := s.OrderPeople(ctx, invalid); err == nil {
			t.Fatalf("accepted %v", invalid)
		}
		check()
	}
	if err := s.UpdatePerson(ctx, bob, "Zed", nil); err != nil {
		t.Fatal(err)
	}
	check()
}
