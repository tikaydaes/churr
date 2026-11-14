package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// The people area: block lists, bans and what admins see, status presets,
// bookmarks, notify anyway, the Threads view, and the small lookups the
// people service needs.

func init() {
	register("blocks_list", testBlocksList)
	register("bans", testBans)
	register("status_presets", testStatusPresets)
	register("bookmarks", testBookmarks)
	register("notify_anyway", testNotifyAnyway)
	register("threads_view", testThreadsView)
	register("people_lookups", testPeopleLookups)
}

func testBlocksList(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	cat := newAccount(t, s, "cat", model.RoleMember)
	dan := newAccount(t, s, "dan", model.RoleMember)
	must(t, s.Blocks().Add(ctx, ann.ID, bob.ID, 1000))
	must(t, s.Blocks().Add(ctx, ann.ID, cat.ID, 2000))
	must(t, s.Blocks().Add(ctx, dan.ID, bob.ID, 3000))
	list, err := s.Blocks().List(ctx, ann.ID)
	must(t, err)
	eq(t, "most recent first", len(list) == 2 && list[0] == cat.ID && list[1] == bob.ID, true)
	none, err := s.Blocks().List(ctx, bob.ID)
	must(t, err)
	eq(t, "nobody blocked", len(none), 0)

	who, err := s.Blocks().Blockers(ctx, bob.ID, []model.AccountID{ann.ID, cat.ID, dan.ID})
	must(t, err)
	eq(t, "who has blocked bob", len(who), 2)
	who, err = s.Blocks().Blockers(ctx, bob.ID, []model.AccountID{cat.ID})
	must(t, err)
	eq(t, "only among those asked about", len(who), 0)
	who, err = s.Blocks().Blockers(ctx, bob.ID, nil)
	must(t, err)
	eq(t, "nobody asked about", len(who), 0)

	// Removing an account deletes its blocks in both directions.
	_, err = s.Accounts().Remove(ctx, bob.ID, ann.ID, model.Now())
	must(t, err)
	list, _ = s.Blocks().List(ctx, ann.ID)
	eq(t, "blocks of a removed account are gone", len(list) == 1 && list[0] == cat.ID, true)
}

func testBans(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	cat := newAccount(t, s, "cat", model.RoleMember)
	c := newChannel(t, s, "general", false, ann.ID)
	join(t, s, c.ID, bob.ID, model.LevelMember)
	now := model.Now()

	// A passkey-only account gets one row with neither provider nor
	// subject, and is removed.
	bans, removed, err := s.Accounts().Ban(ctx, bob.ID, ann.ID, "spam", now)
	must(t, err)
	eq(t, "one row", len(bans) == 1 && bans[0].AccountID == bob.ID && bans[0].ProviderID == "" && bans[0].Reason == "spam", true)
	eq(t, "removed with it", removed.Account.Removed() && len(removed.Changes) == 1, true)
	got, err := s.Accounts().Get(ctx, bob.ID)
	must(t, err)
	eq(t, "the account is removed", got.Removed() && got.RemovedBy == ann.ID, true)
	eq(t, "and out of its channels", level(t, s, c.ID, bob.ID), model.Level(""))
	b, err := s.Accounts().GetBan(ctx, bans[0].ID)
	must(t, err)
	eq(t, "stored", b.CreatedByID == ann.ID && b.CreatedAt == now && b.LiftedAt.IsZero(), true)

	// Banning someone already removed adds a row and removes nothing.
	again, r2, err := s.Accounts().Ban(ctx, bob.ID, ann.ID, "again", now.Add(time.Second))
	must(t, err)
	eq(t, "banned again", len(again) == 1 && len(r2.Changes) == 0, true)
	_, _, err = s.Accounts().Ban(ctx, model.NewID[model.AccountID](), ann.ID, "", now)
	wantErr(t, err, model.ErrNotFound)

	// An identity that has no account.
	provider := model.NewID[string]()
	idb := model.Ban{ProviderID: provider, Subject: "sub-1", Reason: "known", CreatedByID: ann.ID, CreatedAt: now.Add(2 * time.Second)}
	must(t, s.Accounts().BanIdentity(ctx, &idb))
	banned, err := s.Accounts().IdentityBanned(ctx, provider, "sub-1")
	must(t, err)
	eq(t, "the identity is banned", banned, true)
	banned, _ = s.Accounts().IdentityBanned(ctx, provider, "sub-2")
	eq(t, "another subject is not", banned, false)
	banned, _ = s.Accounts().IdentityBanned(ctx, "", "")
	eq(t, "no identity is never banned", banned, false)

	list, next, err := s.Accounts().ListBans(ctx, store.Page{Limit: 2})
	must(t, err)
	eq(t, "newest first, paged", len(list) == 2 && list[0].ID == idb.ID && next != "", true)
	rest, next, err := s.Accounts().ListBans(ctx, store.Page{Limit: 2, Cursor: next})
	must(t, err)
	eq(t, "the rest", len(rest) == 1 && rest[0].ID == bans[0].ID && next == "", true)

	details, err := s.Accounts().AdminDetails(ctx, []model.AccountID{bob.ID, cat.ID})
	must(t, err)
	eq(t, "banned shows on the account", details[bob.ID].Banned && !details[cat.ID].Banned, true)

	must(t, s.Accounts().LiftBan(ctx, idb.ID, now.Add(time.Minute)))
	must(t, s.Accounts().LiftBan(ctx, idb.ID, now.Add(time.Hour))) // again changes nothing
	b, _ = s.Accounts().GetBan(ctx, idb.ID)
	eq(t, "lifted once", b.LiftedAt, now.Add(time.Minute))
	banned, _ = s.Accounts().IdentityBanned(ctx, provider, "sub-1")
	eq(t, "a lifted ban no longer refuses", banned, false)
	wantErr(t, s.Accounts().LiftBan(ctx, model.NewID[string](), now), model.ErrNotFound)
	got, _ = s.Accounts().Get(ctx, bob.ID)
	eq(t, "lifting does not bring the account back", got.Removed(), true)
}

func testStatusPresets(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	hour := int64(3600)
	lunch := model.StatusPreset{Emoji: "🍜", Text: "Lunch", DurationSeconds: &hour, PauseNotifications: true, Position: 2}
	must(t, s.StatusPresets().Create(ctx, &lunch))
	away := model.StatusPreset{Text: "On holiday", AppearAway: true, Availability: model.AvailabilityOutOfOffice, Position: 1}
	must(t, s.StatusPresets().Create(ctx, &away))
	mine := model.StatusPreset{OwnerID: bob.ID, Text: "Deep work", Position: 0}
	must(t, s.StatusPresets().Create(ctx, &mine))
	eq(t, "community is worked out from the owner", lunch.Community && !mine.Community, true)

	got, err := s.StatusPresets().Get(ctx, lunch.ID)
	must(t, err)
	eq(t, "stored", got.Emoji == "🍜" && got.Text == "Lunch" && *got.DurationSeconds == 3600 && got.PauseNotifications && !got.AppearAway && got.Community, true)
	got, _ = s.StatusPresets().Get(ctx, away.ID)
	eq(t, "no duration stays none", got.DurationSeconds == nil && got.Availability == model.AvailabilityOutOfOffice, true)

	list, err := s.StatusPresets().List(ctx, bob.ID)
	must(t, err)
	eq(t, "community first by position, then one's own", len(list) == 3 && list[0].ID == away.ID && list[1].ID == lunch.ID && list[2].ID == mine.ID, true)
	list, _ = s.StatusPresets().List(ctx, ann.ID)
	eq(t, "nobody sees another's", len(list), 2)

	up, err := s.StatusPresets().Update(ctx, lunch.ID, func(p *model.StatusPreset) error {
		p.Text, p.DurationSeconds, p.Position = "Long lunch", nil, 0
		return nil
	})
	must(t, err)
	eq(t, "updated", up.Text == "Long lunch" && up.DurationSeconds == nil, true)
	_, err = s.StatusPresets().Update(ctx, lunch.ID, func(p *model.StatusPreset) error { return model.ErrConflict })
	wantErr(t, err, model.ErrConflict)
	_, err = s.StatusPresets().Update(ctx, model.NewID[string](), func(p *model.StatusPreset) error { return nil })
	wantErr(t, err, model.ErrNotFound)

	must(t, s.StatusPresets().Delete(ctx, away.ID))
	wantErr(t, s.StatusPresets().Delete(ctx, away.ID), model.ErrNotFound)
	_, err = s.StatusPresets().Get(ctx, away.ID)
	wantErr(t, err, model.ErrNotFound)

	// Personal presets go with the account; community ones stay.
	_, err = s.Accounts().Remove(ctx, bob.ID, ann.ID, model.Now())
	must(t, err)
	_, err = s.StatusPresets().Get(ctx, mine.ID)
	wantErr(t, err, model.ErrNotFound)
	_, err = s.StatusPresets().Get(ctx, lunch.ID)
	must(t, err)
}

func testBookmarks(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	pub := newChannel(t, s, "pub", false, ann.ID)
	priv := newChannel(t, s, "priv", true, ann.ID)
	join(t, s, priv.ID, bob.ID, model.LevelMember)
	m1 := post(t, s, pub.ID, ann.ID, "", "one")
	m2 := post(t, s, priv.ID, ann.ID, "", "two")
	m3 := post(t, s, pub.ID, ann.ID, "", "three")

	b, added, err := s.Bookmarks().Add(ctx, bob.ID, m1.ID, 1000)
	must(t, err)
	eq(t, "added", added && b.MessageID == m1.ID && b.CreatedAt == 1000, true)
	b, added, err = s.Bookmarks().Add(ctx, bob.ID, m1.ID, 5000)
	must(t, err)
	eq(t, "again keeps the first", !added && b.CreatedAt == 1000, true)
	_, _, err = s.Bookmarks().Add(ctx, bob.ID, model.NewID[model.MessageID](), 1000)
	wantErr(t, err, model.ErrNotFound)
	_, _, err = s.Bookmarks().Add(ctx, bob.ID, m2.ID, 2000)
	must(t, err)
	_, _, err = s.Bookmarks().Add(ctx, bob.ID, m3.ID, 3000)
	must(t, err)

	full := store.Scope{AccountID: bob.ID, PublicChannels: true}
	list, next, err := s.Bookmarks().List(ctx, bob.ID, full, store.Page{Limit: 2})
	must(t, err)
	eq(t, "newest first, paged", len(list) == 2 && list[0].MessageID == m3.ID && list[1].MessageID == m2.ID && next != "", true)
	list, next, err = s.Bookmarks().List(ctx, bob.ID, full, store.Page{Limit: 2, Cursor: next})
	must(t, err)
	eq(t, "the rest", len(list) == 1 && list[0].MessageID == m1.ID && next == "", true)
	list, _, _ = s.Bookmarks().List(ctx, ann.ID, store.Scope{AccountID: ann.ID, PublicChannels: true}, store.Page{})
	eq(t, "nobody sees another's", len(list), 0)

	// A guest's scope has no public channels it is not in; and leaving a
	// private channel hides its bookmarks without deleting them.
	list, _, _ = s.Bookmarks().List(ctx, bob.ID, store.Scope{AccountID: bob.ID}, store.Page{})
	eq(t, "only where a member", len(list) == 1 && list[0].MessageID == m2.ID, true)
	_, err = s.Members().RemoveDirect(ctx, priv.ID, bob.ID)
	must(t, err)
	list, _, _ = s.Bookmarks().List(ctx, bob.ID, full, store.Page{})
	eq(t, "what can no longer be read is left out", len(list), 2)
	join(t, s, priv.ID, bob.ID, model.LevelMember)
	list, _, _ = s.Bookmarks().List(ctx, bob.ID, full, store.Page{})
	eq(t, "and was kept", len(list), 3)

	removed, err := s.Bookmarks().Remove(ctx, bob.ID, m1.ID)
	must(t, err)
	eq(t, "removed", removed, true)
	removed, _ = s.Bookmarks().Remove(ctx, bob.ID, m1.ID)
	eq(t, "nothing to remove", removed, false)
}

func testNotifyAnyway(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	cat := newAccount(t, s, "cat", model.RoleMember)
	c := newChannel(t, s, "general", false, ann.ID)
	m := post(t, s, c.ID, ann.ID, "", "wake up")
	eq(t, "no mark at first", m.NotifiedAnyway, false)

	marked, first, err := s.NotifyAnyway().Record(ctx, m.ID, bob.ID, ann.ID, model.Now(), false)
	must(t, err)
	eq(t, "the mark is set whether or not it applied", marked && first, true)
	got, err := s.Messages().Get(ctx, m.ID)
	must(t, err)
	eq(t, "the message carries the mark", got.NotifiedAnyway, true)
	delivered, err := s.NotifyAnyway().Delivered(ctx, m.ID, bob.ID)
	must(t, err)
	eq(t, "not delivered", delivered, false)

	// Asking again changes nothing, even if it would now apply.
	marked, first, err = s.NotifyAnyway().Record(ctx, m.ID, bob.ID, ann.ID, model.Now(), true)
	must(t, err)
	eq(t, "nothing new", marked || first, false)
	delivered, _ = s.NotifyAnyway().Delivered(ctx, m.ID, bob.ID)
	eq(t, "the first answer stands", delivered, false)

	marked, first, err = s.NotifyAnyway().Record(ctx, m.ID, cat.ID, ann.ID, model.Now(), true)
	must(t, err)
	eq(t, "a second person: a row, the mark was there", !marked && first, true)
	delivered, _ = s.NotifyAnyway().Delivered(ctx, m.ID, cat.ID)
	eq(t, "delivered", delivered, true)
	delivered, _ = s.NotifyAnyway().Delivered(ctx, m.ID, ann.ID)
	eq(t, "nobody asked for this one", delivered, false)

	_, _, err = s.NotifyAnyway().Record(ctx, model.NewID[model.MessageID](), bob.ID, ann.ID, model.Now(), true)
	wantErr(t, err, model.ErrNotFound)
	_, _, err = s.NotifyAnyway().Record(ctx, m.ID, model.NewID[model.AccountID](), ann.ID, model.Now(), true)
	wantErr(t, err, model.ErrNotFound)

	// Rows go when the target account is removed.
	_, err = s.Accounts().Remove(ctx, cat.ID, ann.ID, model.Now())
	must(t, err)
	delivered, _ = s.NotifyAnyway().Delivered(ctx, m.ID, cat.ID)
	eq(t, "gone with the target", delivered, false)
}

func testThreadsView(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	cat := newAccount(t, s, "cat", model.RoleMember)
	c := newChannel(t, s, "threads", false, ann.ID)
	join(t, s, c.ID, bob.ID, model.LevelMember)
	join(t, s, c.ID, cat.ID, model.LevelMember)
	p1 := post(t, s, c.ID, ann.ID, "", "first")
	p2 := post(t, s, c.ID, ann.ID, "", "second")
	p3 := post(t, s, c.ID, ann.ID, "", "third")
	for _, p := range []model.Message{p1, p2, p3} {
		_, _, err := s.Threads().Follow(ctx, p.ID, ann.ID, p.Seq, 0, false, model.Now())
		must(t, err)
	}
	r1 := post(t, s, c.ID, bob.ID, p1.ID, "reply to first")
	time.Sleep(2 * time.Millisecond) // latest reply first needs distinct times
	post(t, s, c.ID, cat.ID, p2.ID, "reply to second")

	n, err := s.Threads().CountUnread(ctx, ann.ID)
	must(t, err)
	eq(t, "two threads have unread replies", n, int64(2))
	list, next, err := s.Threads().Unread(ctx, ann.ID, false, store.Page{Limit: 1})
	must(t, err)
	eq(t, "latest reply first", len(list) == 1 && list[0].ParentID == p2.ID && list[0].UnreadCount == 1 && next != "", true)
	list, next, err = s.Threads().Unread(ctx, ann.ID, false, store.Page{Limit: 1, Cursor: next})
	must(t, err)
	eq(t, "then the older", len(list) == 1 && list[0].ParentID == p1.ID && next == "", true)
	all, _, err := s.Threads().Unread(ctx, ann.ID, true, store.Page{})
	must(t, err)
	eq(t, "with all, followed threads with nothing unread too", len(all), 3)

	// Reading a thread takes it off the list.
	_, _, err = s.Threads().SetRead(ctx, p1.ID, ann.ID, r1.Seq, model.Now())
	must(t, err)
	n, _ = s.Threads().CountUnread(ctx, ann.ID)
	eq(t, "one left", n, int64(1))

	// Replies by someone the account has blocked are not counted.
	must(t, s.Blocks().Add(ctx, ann.ID, cat.ID, model.Now()))
	n, _ = s.Threads().CountUnread(ctx, ann.ID)
	eq(t, "a blocked author's reply does not count", n, int64(0))
	st, _, _ := s.Threads().Get(ctx, p2.ID, ann.ID)
	eq(t, "nor in the thread's own number", st.UnreadCount, int64(0))
	must(t, s.Blocks().Remove(ctx, ann.ID, cat.ID))
	n, _ = s.Threads().CountUnread(ctx, ann.ID)
	eq(t, "unblocked, it counts again", n, int64(1))

	// An ignored thread is not in the view, and one's own replies never
	// make a thread unread.
	_, err = s.Threads().Set(ctx, p2.ID, ann.ID, model.ThreadIgnored, 0, model.Now())
	must(t, err)
	n, _ = s.Threads().CountUnread(ctx, ann.ID)
	eq(t, "ignored threads are not listed", n, int64(0))
	post(t, s, c.ID, ann.ID, p3.ID, "my own reply")
	n, _ = s.Threads().CountUnread(ctx, ann.ID)
	eq(t, "one's own reply is not unread", n, int64(0))
}

func testPeopleLookups(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	gus := newAccount(t, s, "gus", model.RoleGuest)
	hal := newAccount(t, s, "hal", model.RoleGuest)
	imp := model.Account{Username: "imp", DisplayName: "Imp", Role: model.RoleMember}
	must(t, s.Accounts().CreateUnclaimed(ctx, &imp))
	c := newChannel(t, s, "general", true, ann.ID)
	join(t, s, c.ID, gus.ID, model.LevelMember)

	// Who shares a conversation with whom.
	shared, err := s.Accounts().SharedWith(ctx, ann.ID)
	must(t, err)
	eq(t, "ann shares with gus only", len(shared) == 1 && shared[0] == gus.ID, true)
	shared, _ = s.Accounts().SharedWith(ctx, bob.ID)
	eq(t, "bob shares with nobody", len(shared), 0)

	// What a guest sees of groups: the ones it is in and the ones added
	// to its conversations.
	in := newGroup(t, s, "helpers", hal.ID)
	onChannel := newGroup(t, s, "ops", bob.ID)
	other := newGroup(t, s, "secret", bob.ID)
	_, _, err = s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: c.ID, GroupID: onChannel.ID, Level: model.LevelMember, AddedByID: ann.ID, AddedAt: model.Now()})
	must(t, err)
	for _, tc := range []struct {
		what    string
		group   model.GroupID
		account model.AccountID
		want    bool
	}{
		{"a group one is in", in.ID, hal.ID, true},
		{"a group added to one's channel", onChannel.ID, gus.ID, true},
		{"a group that is neither", other.ID, gus.ID, false},
		{"another guest's group", in.ID, gus.ID, false},
		{"a group on a channel one is not in", onChannel.ID, hal.ID, false},
	} {
		seen, err := s.Groups().SeenBy(ctx, tc.group, tc.account)
		must(t, err)
		eq(t, tc.what, seen, tc.want)
	}

	// The states of the admin list.
	_, err = s.Accounts().Remove(ctx, hal.ID, ann.ID, model.Now())
	must(t, err)
	names := func(state string) string {
		list, _, err := s.Accounts().List(ctx, store.AccountFilter{State: state, IncludeRemoved: true}, store.Page{})
		must(t, err)
		out := ""
		for _, a := range list {
			out += a.Username + " "
		}
		return out
	}
	eq(t, "active", names("active"), "ann bob gus ")
	eq(t, "removed", names("removed"), "hal ")
	eq(t, "unclaimed", names("unclaimed"), "imp ")
	eq(t, "everyone", names(""), "ann bob gus hal imp ")

	// Sign-in details.
	must(t, s.Passkeys().Add(ctx, &model.Passkey{AccountID: bob.ID, Name: "key", CredentialID: []byte("cred-bob"), Credential: []byte(`{}`)}))
	details, err := s.Accounts().AdminDetails(ctx, []model.AccountID{ann.ID, bob.ID})
	must(t, err)
	eq(t, "passkeys are counted", details[bob.ID].PasskeyCount == 1 && details[ann.ID].PasskeyCount == 0, true)
	eq(t, "no totp, no identities, not banned", !details[bob.ID].HasTotp && len(details[bob.ID].ProviderIDs) == 0 && !details[bob.ID].Banned, true)
	empty, err := s.Accounts().AdminDetails(ctx, nil)
	must(t, err)
	eq(t, "nobody asked about", len(empty), 0)

	// A claim link replaces the one before it, and only an unclaimed
	// imported account can have one.
	now := model.Now()
	first := model.Invite{Code: "claim-1", Role: model.RoleMember, ClaimsAccountID: imp.ID, MaxUses: 1, ExpiresAt: now.Add(time.Hour), CreatedByID: ann.ID, CreatedAt: now}
	must(t, s.Accounts().ClaimLinkFor(ctx, &first))
	second := model.Invite{Code: "claim-2", Role: model.RoleMember, ClaimsAccountID: imp.ID, MaxUses: 1, ExpiresAt: now.Add(time.Hour), CreatedByID: ann.ID, CreatedAt: now}
	must(t, s.Accounts().ClaimLinkFor(ctx, &second))
	old, err := s.Invites().Get(ctx, first.ID)
	must(t, err)
	eq(t, "the earlier link is withdrawn", old.Usable(now), false)
	cur, _ := s.Invites().Get(ctx, second.ID)
	eq(t, "the new one works", cur.Usable(now) && cur.ClaimsAccountID == imp.ID, true)
	claimed := model.Invite{Code: "claim-3", Role: model.RoleMember, ClaimsAccountID: bob.ID, MaxUses: 1, CreatedByID: ann.ID}
	wantErr(t, s.Accounts().ClaimLinkFor(ctx, &claimed), model.ErrConflict)

	// Accounts whose shown state changes with the clock.
	_, err = s.Accounts().Update(ctx, bob.ID, func(a *model.Account) error {
		a.StatusText, a.StatusUntil = "Lunch", now.Add(time.Hour)
		return nil
	})
	must(t, err)
	_, err = s.Accounts().Update(ctx, gus.ID, func(a *model.Account) error {
		a.QuietStart, a.QuietEnd, a.QuietDays = "22:00", "07:00", "1111111"
		return nil
	})
	must(t, err)
	forever := model.Time(0)
	_, err = s.Accounts().Update(ctx, ann.ID, func(a *model.Account) error {
		a.DNDUntil = &forever // until turned off: nothing for the clock to do
		return nil
	})
	must(t, err)
	timed, err := s.Accounts().Timed(ctx)
	must(t, err)
	eq(t, "only those with something timed", len(timed) == 2 && timed[0].ID != ann.ID && timed[1].ID != ann.ID, true)

	// Mutes that have run out.
	_, err = s.Members().UpdateOwn(ctx, c.ID, gus.ID, func(m *model.Member) error {
		m.ModeBeforeMute, m.Mode, m.MutedUntil = model.ModeAny, model.ModeNothing, now.Add(-time.Minute)
		return nil
	})
	must(t, err)
	_, err = s.Members().UpdateOwn(ctx, c.ID, ann.ID, func(m *model.Member) error {
		m.ModeBeforeMute, m.Mode, m.MutedUntil = model.ModeAny, model.ModeNothing, now.Add(time.Hour)
		return nil
	})
	must(t, err)
	expired, err := s.Members().ExpiredMutes(ctx, now, 10)
	must(t, err)
	eq(t, "only the mute that ran out", len(expired) == 1 && expired[0].AccountID == gus.ID, true)
}
