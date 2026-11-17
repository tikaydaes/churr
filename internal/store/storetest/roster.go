package storetest

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// The rosters and account facts the store keeps in memory (store/roster.go)
// must say what the tables say after every kind of write. Each check below
// asks first, so that something is kept, then writes, then asks again.

func init() {
	register("roster_follows_members", testRosterFollowsMembers)
	register("facts_follow_accounts", testFactsFollowAccounts)
	register("roster_follows_conversation_settings", testRosterFollowsSettings)
}

// sameRoster compares the kept roster with the member rows.
func sameRoster(t *testing.T, s store.Store, conv model.ConversationID, after string) {
	t.Helper()
	r, err := s.Members().Roster(ctx, conv)
	must(t, err)
	rows, err := s.Members().All(ctx, conv)
	must(t, err)
	ids, err := s.Members().IDs(ctx, conv)
	must(t, err)
	if r.Len() != len(rows) || len(r.IDs) != len(rows) {
		t.Fatalf("after %s: the roster has %d members, the table %d", after, r.Len(), len(rows))
	}
	a, b := slices.Clone(r.IDs), slices.Clone(ids)
	slices.Sort(a)
	slices.Sort(b)
	if !slices.Equal(a, b) {
		t.Fatalf("after %s: the roster's ids are not the table's", after)
	}
	for _, row := range rows {
		got, ok := r.Find(row.AccountID)
		want := store.RosterMember{AccountID: row.AccountID, Level: row.Level, Mode: row.Mode,
			ModeBeforeMute: row.ModeBeforeMute, MutedUntil: row.MutedUntil, ThreadView: row.ThreadView}
		if !ok || got != want || !r.Has(row.AccountID) {
			t.Fatalf("after %s: the roster says %+v (found %v), the table %+v", after, got, ok, want)
		}
		loud := false
		for _, i := range r.Loud {
			loud = loud || r.Members[i].AccountID == row.AccountID
		}
		if loud != (row.Mode == model.ModeAny || row.ModeBeforeMute == model.ModeAny) {
			t.Fatalf("after %s: %s is wrongly listed among those on any activity (%v)", after, row.AccountID, loud)
		}
	}
}

func testRosterFollowsMembers(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	cat := newAccount(t, s, "cat", model.RoleMember)
	dan := newAccount(t, s, "dan", model.RoleMember)
	c := newChannel(t, s, "town", false, ann.ID)
	other := newChannel(t, s, "other", false, ann.ID)
	sameRoster(t, s, c.ID, "the channel was made")
	sameRoster(t, s, other.ID, "the other channel was made")

	join(t, s, c.ID, bob.ID, model.LevelMember)
	sameRoster(t, s, c.ID, "a member joined")
	sameRoster(t, s, other.ID, "a member joined elsewhere")

	_, err := s.Members().SetDirectLevel(ctx, c.ID, bob.ID, model.LevelManager)
	must(t, err)
	sameRoster(t, s, c.ID, "a level changed")

	for name, fn := range map[string]func(m *model.Member){
		"a mode changed": func(m *model.Member) { m.Mode = model.ModeAny },
		"a mute": func(m *model.Member) {
			m.ModeBeforeMute, m.Mode, m.MutedUntil = m.Mode, model.ModeNothing, model.Now().Add(time.Hour)
		},
		"a view changed": func(m *model.Member) { m.ThreadView = "flat" },
	} {
		_, err := s.Members().UpdateOwn(ctx, c.ID, bob.ID, func(m *model.Member) error { fn(m); return nil })
		must(t, err)
		sameRoster(t, s, c.ID, name)
	}

	// A group added to the channel, and people joining and leaving it.
	g := newGroup(t, s, "crew", cat.ID)
	_, _, err = s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: c.ID, GroupID: g.ID, Level: model.LevelMember, AddedByID: ann.ID, AddedAt: model.Now()})
	must(t, err)
	accept(t, s, c.ID, cat.ID)
	sameRoster(t, s, c.ID, "a group was added")
	if r, _ := s.Members().Roster(ctx, c.ID); !r.Has(cat.ID) {
		t.Fatal("the group's member is not in the roster")
	}
	_, err = s.Groups().AddMember(ctx, g.ID, dan.ID, ann.ID, model.Now())
	must(t, err)
	accept(t, s, c.ID, dan.ID)
	sameRoster(t, s, c.ID, "someone joined the group")
	_, err = s.Groups().RemoveMember(ctx, g.ID, cat.ID)
	must(t, err)
	sameRoster(t, s, c.ID, "someone left the group")
	if r, _ := s.Members().Roster(ctx, c.ID); r.Has(cat.ID) || !r.Has(dan.ID) {
		t.Fatal("the roster did not follow the group")
	}
	_, err = s.Members().RemoveGroup(ctx, c.ID, g.ID)
	must(t, err)
	sameRoster(t, s, c.ID, "the group was taken away")

	// A change in a transaction that is undone leaves the roster alone;
	// one that commits is seen.
	boom := errors.New("undone")
	err = s.Tx(ctx, func(tx store.Tx) error {
		if _, err := tx.Members().AddDirect(ctx, c.ID, cat.ID, model.LevelMember, model.Now()); err != nil {
			return err
		}
		in, err := tx.Members().Roster(ctx, c.ID)
		if err != nil {
			return err
		}
		if !in.Has(cat.ID) {
			t.Error("a transaction does not see its own member in the roster")
		}
		if out, _ := s.Members().Roster(ctx, c.ID); out.Has(cat.ID) && s.Dialect() == "postgres" {
			t.Error("a member not yet committed is in everyone's roster")
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	sameRoster(t, s, c.ID, "a transaction was undone")
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		_, err := tx.Members().AddDirect(ctx, c.ID, cat.ID, model.LevelMember, model.Now())
		return err
	}))
	sameRoster(t, s, c.ID, "a transaction committed")
	if r, _ := s.Members().Roster(ctx, c.ID); !r.Has(cat.ID) {
		t.Fatal("the committed member is not in the roster")
	}

	_, err = s.Members().RemoveDirect(ctx, c.ID, cat.ID)
	must(t, err)
	sameRoster(t, s, c.ID, "a member left")

	// A role change and a removal move people in and out everywhere.
	_, _, err = s.Accounts().SetRole(ctx, bob.ID, model.RoleGuest)
	must(t, err)
	sameRoster(t, s, c.ID, "a role changed")
	_, err = s.Accounts().Remove(ctx, bob.ID, ann.ID, model.Now())
	must(t, err)
	sameRoster(t, s, c.ID, "an account was removed")
	if r, _ := s.Members().Roster(ctx, c.ID); r.Has(bob.ID) {
		t.Fatal("a removed account is still in the roster")
	}

	ids, err := s.Members().ConversationIDs(ctx, ann.ID)
	must(t, err)
	if !slices.Contains(ids, c.ID) || !slices.Contains(ids, other.ID) {
		t.Fatalf("ConversationIDs left out a conversation: %v", ids)
	}

	_, err = s.Conversations().Delete(ctx, c.ID)
	must(t, err)
	r, err := s.Members().Roster(ctx, c.ID)
	must(t, err)
	eq(t, "members of a deleted conversation", r.Len(), 0)
	sameRoster(t, s, other.ID, "another conversation was deleted")
}

// sameFacts compares the kept facts of an account with its row.
func sameFacts(t *testing.T, s store.Store, id model.AccountID, after string) {
	t.Helper()
	got, err := s.Accounts().NotifyFacts(ctx, []model.AccountID{id})
	must(t, err)
	row, err := s.Accounts().Get(ctx, id)
	must(t, err)
	f := got[0]
	if f == nil {
		t.Fatalf("after %s: no facts for an account that exists", after)
	}
	ptr := func(p *model.Time) model.Time {
		if p == nil {
			return -1
		}
		return *p
	}
	if f.ID != row.ID || f.Role != row.Role || f.Active() != row.Active() || f.Local() != row.Local() || f.RemovedAt != row.RemovedAt ||
		f.ClaimedAt != row.ClaimedAt || f.TimeZone != row.TimeZone ||
		f.StatusEmoji != row.StatusEmoji || f.StatusText != row.StatusText || f.StatusUntil != row.StatusUntil ||
		f.StatusDND != row.StatusDND || f.StatusAway != row.StatusAway ||
		f.AvailabilityIs != row.AvailabilityIs || f.AvailabilityEnd != row.AvailabilityEnd || ptr(f.DNDUntil) != ptr(row.DNDUntil) ||
		f.QuietStart != row.QuietStart || f.QuietEnd != row.QuietEnd || f.QuietDays != row.QuietDays ||
		f.QuietSkipUntil != row.QuietSkipUntil || f.AllowNotifyAnyway != row.AllowNotifyAnyway || string(f.PrefsJSON) != string(row.PrefsJSON) {
		t.Fatalf("after %s: the kept facts are %+v, the row is %+v", after, *f, row)
	}
}

func testFactsFollowAccounts(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	sameFacts(t, s, ann.ID, "making it")
	sameFacts(t, s, bob.ID, "making it")

	got, err := s.Accounts().NotifyFacts(ctx, []model.AccountID{bob.ID, model.NewID[model.AccountID](), ann.ID, bob.ID})
	must(t, err)
	if len(got) != 4 || got[0] == nil || got[0].ID != bob.ID || got[1] != nil || got[2] == nil || got[2].ID != ann.ID || got[3] == nil {
		t.Fatalf("facts are not in the order asked for, with nil for nobody: %v", got)
	}

	zero := model.Time(0)
	for name, fn := range map[string]func(a *model.Account){
		"do not disturb": func(a *model.Account) { a.DNDUntil = &zero },
		"a status": func(a *model.Account) {
			a.StatusText, a.StatusDND, a.StatusUntil = "busy", true, model.Now().Add(time.Hour)
		},
		"an availability": func(a *model.Account) {
			a.AvailabilityIs, a.AvailabilityEnd = model.AvailabilityMeeting, model.Now().Add(time.Hour)
		},
		"quiet hours": func(a *model.Account) {
			a.QuietStart, a.QuietEnd, a.QuietDays, a.TimeZone = "22:00", "07:00", "1111111", "Europe/Berlin"
		},
		"a skipped quiet period": func(a *model.Account) { a.QuietSkipUntil = model.Now().Add(time.Hour) },
		"prefs": func(a *model.Account) {
			a.PrefsJSON = []byte(`{"thread_view":"flat","meeting_pauses":true}`)
			a.AllowNotifyAnyway = false
		},
	} {
		_, err := s.Accounts().Update(ctx, bob.ID, func(a *model.Account) error { fn(a); return nil })
		must(t, err)
		sameFacts(t, s, bob.ID, name)
	}

	_, _, err = s.Accounts().SetRole(ctx, bob.ID, model.RoleAdmin)
	must(t, err)
	sameFacts(t, s, bob.ID, "a role change")
	_, _, err = s.Accounts().SetRole(ctx, bob.ID, model.RoleOwner)
	must(t, err)
	sameFacts(t, s, bob.ID, "becoming an owner")

	// An account nobody has claimed, then claimed.
	cat := model.Account{Username: "cat", DisplayName: "cat", Role: model.RoleMember}
	must(t, s.Accounts().CreateUnclaimed(ctx, &cat))
	sameFacts(t, s, cat.ID, "making it unclaimed")
	if f, _ := s.Accounts().NotifyFacts(ctx, []model.AccountID{cat.ID}); f[0].Active() {
		t.Fatal("an unclaimed account reads as active")
	}
	_, err = s.Accounts().Claim(ctx, cat.ID, model.Now())
	must(t, err)
	sameFacts(t, s, cat.ID, "claiming it")
	if f, _ := s.Accounts().NotifyFacts(ctx, []model.AccountID{cat.ID}); !f[0].Active() {
		t.Fatal("a claimed account does not read as active")
	}

	// Undone in a transaction: nothing changes. Committed: it does.
	boom := errors.New("undone")
	err = s.Tx(ctx, func(tx store.Tx) error {
		if _, err := tx.Accounts().Update(ctx, cat.ID, func(a *model.Account) error { a.DNDUntil = &zero; return nil }); err != nil {
			return err
		}
		in, err := tx.Accounts().NotifyFacts(ctx, []model.AccountID{cat.ID})
		if err != nil {
			return err
		}
		if in[0] == nil || in[0].DNDUntil == nil {
			t.Error("a transaction does not see its own change in the facts")
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	sameFacts(t, s, cat.ID, "a transaction that was undone")
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		_, err := tx.Accounts().Update(ctx, cat.ID, func(a *model.Account) error { a.DNDUntil = &zero; return nil })
		return err
	}))
	sameFacts(t, s, cat.ID, "a transaction that committed")

	_, err = s.Accounts().Remove(ctx, cat.ID, bob.ID, model.Now())
	must(t, err)
	sameFacts(t, s, cat.ID, "removing it")
	if f, _ := s.Accounts().NotifyFacts(ctx, []model.AccountID{cat.ID}); f[0].Active() {
		t.Fatal("a removed account reads as active")
	}

	who, err := s.Blocks().BlockersOf(ctx, ann.ID)
	must(t, err)
	eq(t, "nobody has blocked ann", len(who), 0)
	must(t, s.Blocks().Add(ctx, bob.ID, ann.ID, model.Now()))
	who, err = s.Blocks().BlockersOf(ctx, ann.ID)
	must(t, err)
	eq(t, "bob has blocked ann", len(who) == 1 && who[0] == bob.ID, true)
}

// The roster carries the two settings of a conversation that a fan-out
// reads (ruling R66). Changing either must be seen by the next reader, and a
// change to the members must not lose them.
func testRosterFollowsSettings(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	c := newChannel(t, s, "news", false, ann.ID)
	want := func(after, lazy string, announcement bool) {
		t.Helper()
		r, err := s.Members().Roster(ctx, c.ID)
		must(t, err)
		if r.Lazy != lazy || r.Announcement != announcement {
			t.Fatalf("after %s: the roster says lazy %q, announcement %v; want %q, %v", after, r.Lazy, r.Announcement, lazy, announcement)
		}
	}
	want("the channel was made", model.LazyServer, false)

	set := func(fn func(*model.Conversation)) {
		t.Helper()
		_, err := s.Conversations().Update(ctx, c.ID, func(cur *model.Conversation) error { fn(cur); return nil })
		must(t, err)
	}
	set(func(cur *model.Conversation) { cur.LazyDelivery = model.LazyOn })
	want("lazy delivery was turned on", model.LazyOn, false)
	set(func(cur *model.Conversation) { cur.Announcement = true })
	want("it became an announcement channel", model.LazyOn, true)

	// A member joining patches the kept roster: the settings stay.
	join(t, s, c.ID, bob.ID, model.LevelMember)
	want("a member joined", model.LazyOn, true)
	sameRoster(t, s, c.ID, "a member joined an announcement channel")

	set(func(cur *model.Conversation) { cur.LazyDelivery, cur.Announcement = model.LazyOff, false })
	want("both were changed back", model.LazyOff, false)
	set(func(cur *model.Conversation) { cur.LazyDelivery = model.LazyServer })
	want("it follows the server again", model.LazyServer, false)

	// A change that is neither does not cost the roster.
	before, err := s.Members().Roster(ctx, c.ID)
	must(t, err)
	set(func(cur *model.Conversation) { cur.Topic = "all the news" })
	after, err := s.Members().Roster(ctx, c.ID)
	must(t, err)
	if before != after {
		t.Fatal("changing a topic made the roster be read again")
	}

	// Who may post in an announcement channel is the member row's.
	m, err := s.Members().SetMayPost(ctx, c.ID, bob.ID, true)
	must(t, err)
	if !m.MayPost {
		t.Fatal("may_post was not stored")
	}
	if got, _, err := s.Members().Get(ctx, c.ID, bob.ID); err != nil || !got.MayPost {
		t.Fatalf("may_post reads back %v, %v", got.MayPost, err)
	}
	if _, err := s.Members().SetMayPost(ctx, c.ID, model.NewID[model.AccountID](), true); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("someone who is not a member: %v", err)
	}
	got, err := s.Conversations().Get(ctx, c.ID)
	must(t, err)
	if got.LazyDelivery != model.LazyServer || got.Announcement || got.AnnouncementReplies {
		t.Fatalf("the conversation reads back %+v", got)
	}
}
