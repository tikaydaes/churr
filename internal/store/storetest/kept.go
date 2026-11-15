package storetest

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// What the store keeps in memory for the post path (sqlstore/kept.go) must
// say what the tables say after every kind of write, as the rosters must
// (roster.go). Each check asks first, so that something is kept, then
// writes, then asks again. The same file holds the store's other shortcuts
// for a post: the checks read as one statement, and writes held back to
// travel together.

func init() {
	register("kept_settings_and_filters", testKeptSettingsAndFilters)
	register("kept_sessions", testKeptSessions)
	register("kept_accounts", testKeptAccounts)
	register("standing", testStanding)
	register("held_writes", testHeldWrites)
}

func testKeptSettingsAndFilters(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	name := func(a store.Areas) string {
		v, err := store.SettingString(ctx, a, store.SettingServerName, "unset")
		must(t, err)
		return v
	}
	filters := func(a store.Areas) int {
		list, err := a.Moderation().WordFilters(ctx)
		must(t, err)
		return len(list)
	}
	eq(t, "no name yet", name(s), "unset")
	eq(t, "no filters yet", filters(s), 0)
	must(t, s.Settings().Set(ctx, store.SettingServerName, "One", ann.ID))
	eq(t, "after Set", name(s), "One")
	all, err := s.Settings().All(ctx)
	must(t, err)
	// The map handed out is the caller's own.
	delete(all, store.SettingServerName)
	eq(t, "after a caller changed its copy", name(s), "One")
	must(t, s.Settings().Delete(ctx, store.SettingServerName))
	eq(t, "after Delete", name(s), "unset")

	f := model.WordFilter{Pattern: "one", Action: model.FilterBlock, CreatedByID: ann.ID}
	must(t, s.Moderation().CreateWordFilter(ctx, &f))
	eq(t, "after CreateWordFilter", filters(s), 1)
	f.Pattern = "two"
	must(t, s.Moderation().UpdateWordFilter(ctx, f))
	list, err := s.Moderation().WordFilters(ctx)
	must(t, err)
	eq(t, "after UpdateWordFilter", list[0].Pattern, "two")
	// The list handed out is the caller's own.
	list[0].Pattern = "scribbled"
	list, _ = s.Moderation().WordFilters(ctx)
	eq(t, "after a caller changed its copy", list[0].Pattern, "two")

	// A transaction reads what it wrote; nobody else does until it has
	// committed, and never if it fails.
	boom := errors.New("boom")
	err = s.Tx(ctx, func(tx store.Tx) error {
		eq(t, "in a transaction, before its write", name(tx), "unset")
		must(t, tx.Settings().Set(ctx, store.SettingServerName, "Never", ""))
		eq(t, "in a transaction, its own write", name(tx), "Never")
		must(t, tx.Moderation().DeleteWordFilter(ctx, f.ID))
		eq(t, "in a transaction, its own delete", filters(tx), 0)
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("the transaction returned %v", err)
	}
	eq(t, "after a failed transaction", name(s), "unset")
	eq(t, "after a failed transaction", filters(s), 1)
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		must(t, tx.Settings().Set(ctx, store.SettingServerName, "Two", ""))
		return tx.Moderation().DeleteWordFilter(ctx, f.ID)
	}))
	eq(t, "after a committed transaction", name(s), "Two")
	eq(t, "after a committed transaction", filters(s), 0)
}

func testKeptSessions(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	now := model.Now()
	make := func(account model.AccountID, token string) model.Session {
		ses := model.Session{AccountID: account, Kind: model.SessionWeb, SignedInBy: "passkey", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
		must(t, s.Sessions().Create(ctx, &ses, []byte(token), []byte("refresh-"+token)))
		return ses
	}
	// same holds that finding the session by its token gives the row as
	// it stands, which Get always reads.
	same := func(after, token string, id model.SessionID) {
		t.Helper()
		got, err := s.Sessions().ByTokenHash(ctx, []byte(token))
		must(t, err)
		want, err := s.Sessions().Get(ctx, id)
		must(t, err)
		if got != want {
			t.Fatalf("after %s: by its token the session is %+v, the table says %+v", after, got, want)
		}
	}
	gone := func(after, token string) {
		t.Helper()
		_, err := s.Sessions().ByTokenHash(ctx, []byte(token))
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("after %s: the token still finds a session (%v)", after, err)
		}
	}
	a := make(ann.ID, "a")
	same("Create", "a", a.ID)
	must(t, s.Sessions().Touch(ctx, a.ID, now.Add(time.Minute), now.Add(2*time.Hour)))
	same("Touch", "a", a.ID)
	must(t, s.Sessions().SetFresh(ctx, a.ID, now.Add(time.Minute)))
	same("SetFresh", "a", a.ID)
	must(t, s.Sessions().Rotate(ctx, a.ID, []byte("a2"), []byte("refresh-a2"), now.Add(time.Hour), now))
	gone("Rotate", "a")
	same("Rotate", "a2", a.ID)
	must(t, s.Sessions().Revoke(ctx, a.ID, now))
	same("Revoke", "a2", a.ID)

	b1, b2 := make(bob.ID, "b1"), make(bob.ID, "b2")
	same("Create", "b1", b1.ID)
	same("Create", "b2", b2.ID)
	_, err := s.Sessions().RevokeForAccount(ctx, bob.ID, b2.ID, now)
	must(t, err)
	same("RevokeForAccount", "b1", b1.ID)
	same("RevokeForAccount", "b2", b2.ID)
	_, err = s.Sessions().RevokeKinds(ctx, []string{model.SessionWeb}, "", now)
	must(t, err)
	same("RevokeKinds", "b2", b2.ID)

	pending := model.Session{AccountID: ann.ID, Kind: model.SessionWeb, PendingSecondFactor: true, CreatedAt: now}
	must(t, s.Sessions().Create(ctx, &pending, []byte("p"), nil))
	same("Create", "p", pending.ID)
	ok, err := s.Sessions().Complete(ctx, pending.ID, []byte("p2"), "totp", now.Add(time.Hour), now)
	must(t, err)
	eq(t, "completed", ok, true)
	gone("Complete", "p")
	same("Complete", "p2", pending.ID)

	// A revocation inside a transaction is seen when it has committed.
	c := make(ann.ID, "c")
	same("Create", "c", c.ID)
	must(t, s.Tx(ctx, func(tx store.Tx) error { return tx.Sessions().Revoke(ctx, c.ID, now) }))
	same("Revoke in a transaction", "c", c.ID)

	_, err = s.Sessions().DeleteEnded(ctx, now.Add(time.Minute))
	must(t, err)
	gone("DeleteEnded", "b1")
	gone("DeleteEnded", "c")

	d := make(bob.ID, "d")
	same("Create", "d", d.ID)
	_, err = s.Accounts().Remove(ctx, bob.ID, ann.ID, now)
	must(t, err)
	gone("removing the account", "d")
}

func testKeptAccounts(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	// same holds that Get gives the row as GetMany, which always reads
	// the table, gives it.
	same := func(after string, id model.AccountID) model.Account {
		t.Helper()
		got, err := s.Accounts().Get(ctx, id)
		must(t, err)
		want, err := s.Accounts().GetMany(ctx, []model.AccountID{id})
		must(t, err)
		if len(want) != 1 || !reflect.DeepEqual(got, want[0]) {
			t.Fatalf("after %s: Get says %+v, the table %+v", after, got, want)
		}
		return got
	}
	same("Create", bob.ID)
	_, err := s.Accounts().Update(ctx, bob.ID, func(a *model.Account) error {
		a.DisplayName, a.Title, a.PrefsJSON = "Robert", "Tester", []byte(`{"thread_view":"flat"}`)
		return nil
	})
	must(t, err)
	got := same("Update", bob.ID)
	eq(t, "the new name", got.DisplayName, "Robert")
	// What is handed out is the caller's own.
	if len(got.PrefsJSON) > 0 {
		got.PrefsJSON[0] = 'X'
	}
	same("a caller changed its copy", bob.ID)

	_, _, err = s.Accounts().SetRole(ctx, bob.ID, model.RoleAdmin)
	must(t, err)
	eq(t, "the new role", same("SetRole", bob.ID).Role, model.RoleAdmin)

	// Inside a transaction the table is read; afterwards the change is
	// seen, or not if the transaction failed.
	boom := errors.New("boom")
	err = s.Tx(ctx, func(tx store.Tx) error {
		if _, _, err := tx.Accounts().SetRole(ctx, bob.ID, model.RoleGuest); err != nil {
			return err
		}
		in, err := tx.Accounts().Get(ctx, bob.ID)
		must(t, err)
		eq(t, "in a transaction, its own write", in.Role, model.RoleGuest)
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("the transaction returned %v", err)
	}
	eq(t, "after a failed transaction", same("a failed transaction", bob.ID).Role, model.RoleAdmin)
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		_, _, err := tx.Accounts().SetRole(ctx, bob.ID, model.RoleMember)
		return err
	}))
	eq(t, "after a committed transaction", same("a committed transaction", bob.ID).Role, model.RoleMember)

	_, err = s.Accounts().Remove(ctx, bob.ID, ann.ID, model.Now())
	must(t, err)
	eq(t, "removed", same("Remove", bob.ID).Removed(), true)
}

func testStanding(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	cat := newAccount(t, s, "cat", model.RoleMember)
	bot := botsNewBot(t, s, "robot", ann.ID, false)
	room := newChannel(t, s, "room", false, ann.ID)
	other := newChannel(t, s, "other", true, ann.ID)
	join(t, s, room.ID, bob.ID, model.LevelMember)
	join(t, s, room.ID, bot.ID, model.LevelMember)
	post(t, s, room.ID, ann.ID, "", "hello")
	now := model.Now()

	// same holds that the one read gives what the four reads it replaces
	// give.
	same := func(what string, conv model.ConversationID, account model.AccountID) store.Standing {
		t.Helper()
		got, err := s.Members().Standing(ctx, conv, account, now)
		must(t, err)
		c, err := s.Conversations().Get(ctx, conv)
		must(t, err)
		if !reflect.DeepEqual(got.Conversation, c) {
			t.Fatalf("%s: the conversation is %+v, Get says %+v", what, got.Conversation, c)
		}
		m, found, err := s.Members().Get(ctx, conv, account)
		must(t, err)
		if found != (got.Member != nil) || (found && *got.Member != m) {
			t.Fatalf("%s: the member is %+v, Get says %+v (found %v)", what, got.Member, m, found)
		}
		until, timedOut, err := s.Moderation().TimedOutUntil(ctx, account, conv, now)
		must(t, err)
		if got.TimedOutUntil != until || timedOut == got.TimedOutUntil.IsZero() {
			t.Fatalf("%s: timed out until %v, TimedOutUntil says %v (%v)", what, got.TimedOutUntil, until, timedOut)
		}
		b, found, err := s.Bots().Get(ctx, account)
		must(t, err)
		if found != (got.Bot != nil) || (found && *got.Bot != b) {
			t.Fatalf("%s: the bot is %+v, Get says %+v (found %v)", what, got.Bot, b, found)
		}
		return got
	}
	eq(t, "a member", same("a member", room.ID, bob.ID).Member != nil, true)
	eq(t, "someone who is not a member", same("not a member", room.ID, cat.ID).Member == nil, true)
	eq(t, "a bot", same("a bot", room.ID, bot.ID).Bot != nil, true)
	same("a private channel, its creator", other.ID, ann.ID)
	same("a private channel, someone else", other.ID, bob.ID)

	// Inside a transaction it sees what the transaction wrote.
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		if _, err := tx.Members().AddDirect(ctx, room.ID, cat.ID, model.LevelReader, now); err != nil {
			return err
		}
		in, err := tx.Members().Standing(ctx, room.ID, cat.ID, now)
		must(t, err)
		eq(t, "in a transaction, its own write", in.Member != nil && in.Member.Level == model.LevelReader, true)
		return nil
	}))
	eq(t, "the new member's level", same("a new member", room.ID, cat.ID).Member.Level, model.LevelReader)

	// Time-outs: here, elsewhere, on the whole server, over, lifted.
	here := model.Timeout{AccountID: bob.ID, ConversationID: room.ID, Until: now.Add(time.Hour), CreatedByID: ann.ID}
	must(t, s.Moderation().CreateTimeout(ctx, &here))
	eq(t, "timed out here", same("timed out here", room.ID, bob.ID).TimedOutUntil, here.Until)
	join(t, s, other.ID, bob.ID, model.LevelMember)
	eq(t, "not timed out elsewhere", same("timed out elsewhere", other.ID, bob.ID).TimedOutUntil.IsZero(), true)
	wide := model.Timeout{AccountID: bob.ID, Until: now.Add(2 * time.Hour), CreatedByID: ann.ID}
	must(t, s.Moderation().CreateTimeout(ctx, &wide))
	eq(t, "the later end counts", same("timed out everywhere", room.ID, bob.ID).TimedOutUntil, wide.Until)
	eq(t, "everywhere", same("timed out everywhere", other.ID, bob.ID).TimedOutUntil, wide.Until)
	must(t, s.Moderation().LiftTimeout(ctx, wide.ID, now))
	must(t, s.Moderation().LiftTimeout(ctx, here.ID, now))
	eq(t, "lifted", same("lifted", room.ID, bob.ID).TimedOutUntil.IsZero(), true)
	over := model.Timeout{AccountID: cat.ID, ConversationID: room.ID, Until: now.Add(-time.Minute), CreatedByID: ann.ID}
	must(t, s.Moderation().CreateTimeout(ctx, &over))
	eq(t, "one that is over", same("over", room.ID, cat.ID).TimedOutUntil.IsZero(), true)

	_, err := s.Members().Standing(ctx, model.NewID[model.ConversationID](), bob.ID, now)
	wantErr(t, err, model.ErrNotFound)
}

func testHeldWrites(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	room := newChannel(t, s, "room", false, ann.ID)
	join(t, s, room.ID, bob.ID, model.LevelMember)
	lastSeq := func() int64 {
		c, err := s.Conversations().Get(ctx, room.ID)
		must(t, err)
		return c.LastSeq
	}

	// The seq taken for a message also sets last_message_at, and the
	// message is there for the next read of the same transaction, with
	// its mentions, whether or not an event follows.
	at := model.Now().Add(-time.Minute)
	m := model.Message{ConversationID: room.ID, AuthorID: ann.ID, Text: "held", CreatedAt: at, ClientID: string(model.NewID[model.MessageID]())}
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		seq, stream, err := tx.Streams().TakeMessageSeq(ctx, room.ID, at)
		if err != nil {
			return err
		}
		eq(t, "no stream", stream, "")
		m.Seq = seq
		if err := tx.Messages().Insert(ctx, &m, []model.Mention{{AccountID: bob.ID, Kind: model.MentionAccount}, {AccountID: bob.ID, Kind: model.MentionGroup}}); err != nil {
			return err
		}
		in, err := tx.Messages().Get(ctx, m.ID)
		must(t, err)
		eq(t, "read back in its transaction", in.Text == "held" && in.Seq == seq && in.Order == m.Order, true)
		return nil
	}))
	got, err := s.Messages().Get(ctx, m.ID)
	must(t, err)
	eq(t, "stored without an event", got.Text == "held" && got.Seq == m.Seq && got.SearchText == "held", true)
	mentions, err := s.Messages().Mentions(ctx, m.ID)
	must(t, err)
	eq(t, "one mention row for each account, the first kind", len(mentions) == 1 && mentions[0].Kind == model.MentionAccount, true)
	c, err := s.Conversations().Get(ctx, room.ID)
	must(t, err)
	eq(t, "last_message_at", c.LastMessageAt, at)

	// With its event, as a post is stored.
	p := post(t, s, room.ID, bob.ID, "", "with an event")
	events, _, err := s.Events().List(ctx, room.ID, p.Seq-1, 0, 10)
	must(t, err)
	eq(t, "the event is stored with the message", len(events) == 1 && events[0].MessageID == p.ID, true)
	c, _ = s.Conversations().Get(ctx, room.ID)
	eq(t, "last_message_at follows a message stored the old way", c.LastMessageAt, p.CreatedAt)

	// A client_id used twice: the transaction as a whole fails with
	// ErrAlreadyExists, takes no seq and leaves nothing behind.
	before := lastSeq()
	dup := model.Message{ConversationID: room.ID, AuthorID: ann.ID, Text: "again", ClientID: m.ClientID, CreatedAt: model.Now()}
	err = s.Tx(ctx, func(tx store.Tx) error {
		seq, _, err := tx.Streams().TakeMessageSeq(ctx, room.ID, dup.CreatedAt)
		if err != nil {
			return err
		}
		dup.Seq = seq
		if err := tx.Messages().Insert(ctx, &dup, nil); err != nil {
			return err
		}
		return tx.Events().Append(ctx, model.Event{Type: model.EventMessageCreated, At: dup.CreatedAt, ConversationID: room.ID,
			Seq: seq, MessageID: dup.ID, Payload: map[string]any{"message": dup}})
	})
	wantErr(t, err, model.ErrAlreadyExists)
	_, err = s.Messages().Get(ctx, dup.ID)
	wantErr(t, err, model.ErrNotFound)
	eq(t, "no seq was used", lastSeq(), before)
	// The same when nothing follows the insert but the commit.
	dup.ID = model.NewID[model.MessageID]()
	err = s.Tx(ctx, func(tx store.Tx) error {
		seq, err := tx.Conversations().NextSeq(ctx, room.ID)
		if err != nil {
			return err
		}
		dup.Seq = seq
		return tx.Messages().Insert(ctx, &dup, nil)
	})
	wantErr(t, err, model.ErrAlreadyExists)
	eq(t, "no seq was used", lastSeq(), before)

	// A transaction that fails after a held write keeps nothing.
	boom := errors.New("boom")
	lost := model.Message{ConversationID: room.ID, AuthorID: ann.ID, Text: "lost", CreatedAt: model.Now()}
	err = s.Tx(ctx, func(tx store.Tx) error {
		seq, _, err := tx.Streams().TakeMessageSeq(ctx, room.ID, lost.CreatedAt)
		if err != nil {
			return err
		}
		lost.Seq = seq
		if err := tx.Messages().Insert(ctx, &lost, nil); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("the transaction returned %v", err)
	}
	_, err = s.Messages().Get(ctx, lost.ID)
	wantErr(t, err, model.ErrNotFound)
	eq(t, "no seq was used", lastSeq(), before)

	// A reply: the parent's summary counts it at once.
	r := post(t, s, room.ID, bob.ID, p.ID, "a reply")
	parent, err := s.Messages().Get(ctx, p.ID)
	must(t, err)
	eq(t, "the parent's summary", parent.ReplyCount == 1 && parent.LastReplySeq == r.Seq, true)
}
