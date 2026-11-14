package notify

import (
	"context"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

var ctx = context.Background()

// world is a store with a notify service over it and helpers that fail the
// test on any error.
type world struct {
	t *testing.T
	s store.Store
	n *Service
}

func newWorld(t *testing.T) *world {
	s := storetest.OpenSQLite(t)
	if err := s.Groups().EnsureBuiltin(ctx); err != nil {
		t.Fatal(err)
	}
	return &world{t: t, s: s, n: New(s, perm.New(s))}
}

func (w *world) must(err error) {
	w.t.Helper()
	if err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) account(name string, role model.Role) model.Account {
	w.t.Helper()
	a := model.Account{Username: name, DisplayName: name, Role: role}
	_, err := w.s.Accounts().Create(ctx, &a)
	w.must(err)
	return a
}

func (w *world) channel(name string, private bool, creator model.Account, members ...model.Account) model.Conversation {
	w.t.Helper()
	c := model.Conversation{Kind: model.KindChannel, Name: name, Private: private, CreatedByID: creator.ID}
	w.must(w.s.Conversations().Create(ctx, &c))
	for i, a := range append([]model.Account{creator}, members...) {
		level := model.LevelMember
		if i == 0 {
			level = model.LevelManager
		}
		_, err := w.s.Members().AddDirect(ctx, c.ID, a.ID, level, model.Now())
		w.must(err)
	}
	return c
}

func (w *world) post(c model.Conversation, author model.Account, parent model.MessageID, mentions ...model.Mention) model.Message {
	w.t.Helper()
	m := model.Message{ID: model.NewID[model.MessageID](), ConversationID: c.ID, AuthorID: author.ID, ParentID: parent, Text: "hello", CreatedAt: model.Now()}
	w.must(w.s.Tx(ctx, func(tx store.Tx) error {
		seq, err := tx.Conversations().NextSeq(ctx, c.ID)
		if err != nil {
			return err
		}
		m.Seq = seq
		for i := range mentions {
			mentions[i].MessageID = m.ID
		}
		return tx.Messages().Insert(ctx, &m, mentions)
	}))
	w.n.Changed(model.Event{Type: model.EventMessageCreated, ConversationID: c.ID, Seq: m.Seq, MessageID: m.ID})
	return m
}

func (w *world) mode(c model.Conversation, a model.Account, fn func(m *model.Member)) {
	w.t.Helper()
	_, err := w.s.Members().UpdateOwn(ctx, c.ID, a.ID, func(m *model.Member) error { fn(m); return nil })
	w.must(err)
	w.n.Touch(a.ID, c.ID)
}

func (w *world) update(a model.Account, fn func(a *model.Account)) {
	w.t.Helper()
	_, err := w.s.Accounts().Update(ctx, a.ID, func(a *model.Account) error { fn(a); return nil })
	w.must(err)
	w.n.Forget(a.ID)
}

func mention(a model.Account, kind string) model.Mention {
	return model.Mention{AccountID: a.ID, Kind: kind}
}

func setMode(mode model.Mode) func(*model.Member) { return func(m *model.Member) { m.Mode = mode } }

// TestDecide walks the table of docs/FEATURES.md section 3.7.
func TestDecide(t *testing.T) {
	w := newWorld(t)
	ann := w.account("ann", model.RoleOwner)
	bob := w.account("bob", model.RoleMember)
	c := w.channel("general", false, ann, bob)
	check := func(name string, m model.Message, to model.Account, want Level) {
		t.Helper()
		if got := w.n.Decide(ctx, m, to.ID); got != want {
			t.Errorf("%s: got %s, want %s", name, got, want)
		}
	}

	top := w.post(c, ann, "")
	named := w.post(c, ann, "", mention(bob, model.MentionAccount))
	group := w.post(c, ann, "", mention(bob, model.MentionChannel))
	parent := w.post(c, ann, "")
	reply := w.post(c, ann, parent.ID)
	replyNamed := w.post(c, ann, parent.ID, mention(bob, model.MentionAccount))
	replyGroup := w.post(c, ann, parent.ID, mention(bob, model.MentionGroup))

	// Mode `any`.
	w.mode(c, bob, setMode(model.ModeAny))
	check("any, top-level", top, bob, Notify)
	check("any, a reply in a thread neither followed nor ignored", reply, bob, None)
	check("any, a reply that mentions", replyGroup, bob, Notify)
	check("one's own message", top, ann, None)

	// The flat view counts such a reply for the badge without notifying.
	w.mode(c, bob, func(m *model.Member) { m.ThreadView = "flat" })
	check("any, flat, a reply in a thread neither followed nor ignored", reply, bob, Badge)
	w.mode(c, bob, func(m *model.Member) { m.ThreadView = "" })

	// Following and ignoring.
	_, err := w.s.Threads().Set(ctx, parent.ID, bob.ID, model.ThreadFollowing, 0, model.Now())
	w.must(err)
	check("any, a reply in a followed thread", reply, bob, Notify)
	_, err = w.s.Threads().Set(ctx, parent.ID, bob.ID, model.ThreadIgnored, 0, model.Now())
	w.must(err)
	check("a reply in an ignored thread", reply, bob, None)
	check("a group mention in an ignored thread", replyGroup, bob, None)
	check("a mention by name in an ignored thread still notifies", replyNamed, bob, Notify)
	w.must(w.s.Threads().Delete(ctx, parent.ID, bob.ID))

	// Mode `mentions`.
	w.mode(c, bob, setMode(model.ModeMentions))
	check("mentions, top-level", top, bob, None)
	check("mentions, a mention by name", named, bob, Notify)
	check("mentions, @channel", group, bob, Notify)
	check("mentions, a reply", reply, bob, None)
	_, err = w.s.Threads().Set(ctx, parent.ID, bob.ID, model.ThreadFollowing, 0, model.Now())
	w.must(err)
	check("mentions, a reply in a followed thread", reply, bob, Notify)
	w.must(w.s.Threads().Delete(ctx, parent.ID, bob.ID))

	// Nothing, mute, and hidden from the sidebar.
	w.mode(c, bob, setMode(model.ModeNothing))
	check("nothing, top-level", top, bob, None)
	check("nothing, @channel", group, bob, None)
	check("nothing, a mention by name counts for the badge", named, bob, Badge)
	w.mode(c, bob, func(m *model.Member) {
		m.Mode, m.ModeBeforeMute, m.MutedUntil = model.ModeNothing, model.ModeAny, model.Now().Add(time.Hour)
	})
	check("muted behaves as nothing", top, bob, None)
	w.mode(c, bob, func(m *model.Member) { m.MutedUntil = model.Now().Add(-time.Minute) })
	check("a mute that ran out is read as the mode before it", top, bob, Notify)
	// What is shown does not constrain what notifies (ruling R46).
	w.mode(c, bob, func(m *model.Member) {
		m.Mode, m.ModeBeforeMute, m.MutedUntil, m.Show = model.ModeMentions, "", 0, model.ShowHidden
	})
	check("hidden from the sidebar, a mention by name still notifies", named, bob, Notify)
	check("hidden from the sidebar, no mention", top, bob, None)
	w.mode(c, bob, func(m *model.Member) { m.Show = "" })
	w.mode(c, bob, setMode(model.ModeAny))

	// Do not disturb: what would have notified only counts.
	w.update(bob, func(a *model.Account) { zero := model.Time(0); a.DNDUntil = &zero })
	check("in do not disturb, what would notify is a badge", top, bob, Badge)
	check("in do not disturb, what would not is nothing", reply, bob, None)
	w.update(bob, func(a *model.Account) { a.DNDUntil = nil })

	// Blocks, while blocking is on.
	w.must(w.s.Blocks().Add(ctx, bob.ID, ann.ID, model.Now()))
	check("the author is blocked", named, bob, None)
	w.must(w.s.Settings().Set(ctx, store.SettingBlockingEnabled, false, ""))
	check("blocking is off for the server: stored blocks have no effect", named, bob, Notify)
	w.must(w.s.Settings().Set(ctx, store.SettingBlockingEnabled, true, ""))
	w.must(w.s.Blocks().Remove(ctx, bob.ID, ann.ID))

	// System notices, quiet messages and reminders.
	sys := top
	sys.System = map[string]any{"kind": "joined"}
	check("a system notice", sys, bob, None)
	q := top
	q.Quiet = true
	check("a quiet message", q, bob, None)
	rem := top
	rem.Via = &model.Via{Kind: "reminder"}
	w.mode(c, ann, setMode(model.ModeAny))
	check("a reminder notifies its own author", rem, ann, Notify)

	// Who is not notified at all.
	bot := w.account("bot", model.RoleBot)
	_, err = w.s.Members().AddDirect(ctx, c.ID, bot.ID, model.LevelMember, model.Now())
	w.must(err)
	w.mode(c, bot, setMode(model.ModeAny))
	check("a bot", top, bot, None)
	guest := w.account("gus", model.RoleGuest)
	check("a guest who cannot read the channel", named, guest, None)
	eve := w.account("eve", model.RoleMember)
	check("a member who has not joined a public channel", top, eve, None)
	mentioned := w.post(c, ann, "", mention(eve, model.MentionAccount))
	check("a mention of someone who has not joined notifies once", mentioned, eve, Notify)
	priv := w.channel("priv", true, ann)
	secret := w.post(priv, ann, "", mention(eve, model.MentionAccount))
	check("someone who cannot read a private channel", secret, eve, None)
	_, err = w.s.Accounts().Remove(ctx, eve.ID, ann.ID, model.Now())
	w.must(err)
	check("a removed account", mentioned, eve, None)
	check("an account that does not exist", top, model.Account{ID: model.NewID[model.AccountID]()}, None)
}

// TestAnywayApplies is docs/FEATURES.md section 3.8 step 2, and cases 190 to
// 195 of docs/PERMISSIONS.md.
func TestAnywayApplies(t *testing.T) {
	w := newWorld(t)
	ann := w.account("ann", model.RoleOwner)
	bob := w.account("bob", model.RoleMember)
	c := w.channel("general", false, ann, bob)
	dnd := func(on bool) {
		w.update(bob, func(a *model.Account) {
			a.DNDUntil = nil
			if on {
				zero := model.Time(0)
				a.DNDUntil = &zero
			}
		})
	}
	top := w.post(c, ann, "")
	named := w.post(c, ann, "", mention(bob, model.MentionAccount))
	broadcast := w.post(c, ann, "", mention(bob, model.MentionChannel))
	check := func(name string, m model.Message, want bool) {
		t.Helper()
		if got := w.n.AnywayApplies(ctx, m, bob.ID); got != want {
			t.Errorf("%s: applies = %v, want %v", name, got, want)
		}
	}

	w.mode(c, bob, setMode(model.ModeAny))
	check("not in do not disturb: nothing to push through", top, false)
	dnd(true)
	check("case 190: in do not disturb, on Any activity", top, true)

	w.mode(c, bob, setMode(model.ModeMentions))
	check("Mentions, mentioned by name", named, true)
	check("Mentions, reached only by @channel", broadcast, false)
	check("Mentions, not mentioned", top, false)

	w.mode(c, bob, setMode(model.ModeNothing))
	check("case 191: mode nothing", named, false)
	w.mode(c, bob, func(m *model.Member) {
		m.Mode, m.ModeBeforeMute, m.MutedUntil = model.ModeNothing, model.ModeAny, model.Now().Add(time.Hour)
	})
	check("muted", named, false)
	// Case 192 as ruling R46 leaves it: hiding a conversation from the
	// sidebar changes nothing about what notifies.
	w.mode(c, bob, func(m *model.Member) {
		m.Mode, m.ModeBeforeMute, m.MutedUntil, m.Show = model.ModeAny, "", 0, model.ShowHidden
	})
	check("case 192: hidden from the sidebar, on Any activity", named, true)
	w.mode(c, bob, func(m *model.Member) { m.Show = "" })

	w.must(w.s.Blocks().Add(ctx, bob.ID, ann.ID, model.Now()))
	check("case 193: has blocked the author", named, false)
	w.must(w.s.Blocks().Remove(ctx, bob.ID, ann.ID))

	w.update(bob, func(a *model.Account) { a.AllowNotifyAnyway = false })
	check("case 194: has turned the feature off", named, false)
	w.update(bob, func(a *model.Account) { a.AllowNotifyAnyway = true })
	check("back on", named, true)

	_, err := w.s.Members().RemoveDirect(ctx, c.ID, bob.ID)
	w.must(err)
	_, err = w.s.Conversations().MakePrivate(ctx, c.ID)
	w.must(err)
	check("case 195: cannot read the conversation", named, false)

	// A request that applied is what lets Decide answer notify.
	c2 := w.channel("other", false, ann, bob)
	w.mode(c2, bob, setMode(model.ModeAny))
	m := w.post(c2, ann, "")
	if got := w.n.Decide(ctx, m, bob.ID); got != Badge {
		t.Fatalf("in do not disturb: %s", got)
	}
	_, _, err = w.s.NotifyAnyway().Record(ctx, m.ID, bob.ID, ann.ID, model.Now(), false)
	w.must(err)
	if got := w.n.Decide(ctx, m, bob.ID); got != Badge {
		t.Fatalf("a request that did not apply changes nothing: %s", got)
	}
	m2 := w.post(c2, ann, "")
	_, _, err = w.s.NotifyAnyway().Record(ctx, m2.ID, bob.ID, ann.ID, model.Now(), true)
	w.must(err)
	if got := w.n.Decide(ctx, m2, bob.ID); got != Notify {
		t.Fatalf("a request that applied notifies: %s", got)
	}
}

// TestTotals: the totals equal the sum of the per-conversation numbers, and
// follow what happens.
func TestTotals(t *testing.T) {
	w := newWorld(t)
	ann := w.account("ann", model.RoleOwner)
	bob := w.account("bob", model.RoleMember)
	a := w.channel("alpha", false, ann, bob)
	b := w.channel("beta", false, ann, bob)
	h := w.channel("hidden", false, ann, bob)
	w.mode(a, bob, setMode(model.ModeAny))
	w.mode(b, bob, setMode(model.ModeMentions))
	w.mode(h, bob, func(m *model.Member) { m.Mode, m.Show = model.ModeMentions, model.ShowHidden })

	total := func() model.Totals {
		t.Helper()
		got, err := w.n.Totals(ctx, bob.ID)
		w.must(err)
		// Whatever is kept in memory must equal what the store says now.
		var want model.Totals
		acc, err := w.s.Accounts().Get(ctx, bob.ID)
		w.must(err)
		list, _, err := w.s.Conversations().ForAccount(ctx, bob.ID, store.ConversationFilter{}, store.Page{})
		w.must(err)
		for _, cm := range list {
			c, err := w.n.CountsFor(ctx, w.s, acc, cm.Member)
			w.must(err)
			want.Badge += c.Badge
			want.Mentions += c.Mentions
			if c.Unread > 0 {
				want.UnreadConversations++
			}
			if n, _ := w.n.Badge(ctx, bob.ID, cm.Conversation.ID); int64(n) != c.Badge {
				t.Fatalf("Badge = %d, want %d", n, c.Badge)
			}
		}
		want.UnreadThreads, err = w.s.Threads().CountUnread(ctx, bob.ID)
		w.must(err)
		if got != want {
			t.Fatalf("totals %+v, want the sum of the conversations %+v", got, want)
		}
		return got
	}

	if got := total(); got != (model.Totals{}) {
		t.Fatalf("nothing yet: %+v", got)
	}
	w.post(a, ann, "")
	w.post(a, ann, "")
	if got := total(); got.Badge != 2 || got.UnreadConversations != 1 {
		t.Fatalf("two messages on Any activity: %+v", got)
	}
	w.post(b, ann, "")
	if got := total(); got.Badge != 2 || got.UnreadConversations != 2 {
		t.Fatalf("a message on Mentions is unread and has no badge: %+v", got)
	}
	w.post(b, ann, "", mention(bob, model.MentionAccount))
	if got := total(); got.Badge != 3 || got.Mentions != 1 {
		t.Fatalf("a mention: %+v", got)
	}
	w.post(h, ann, "", mention(bob, model.MentionAccount))
	if got := total(); got.Badge != 4 || got.Mentions != 2 || got.UnreadConversations != 3 {
		t.Fatalf("a conversation hidden from the sidebar counts like any other: %+v", got)
	}
	_, newest1, err := w.n.Summary(ctx, bob.ID)
	w.must(err)

	// A followed thread with an unread reply.
	parent := w.post(a, bob, "")
	_, _, err = w.s.Threads().Follow(ctx, parent.ID, bob.ID, parent.Seq, 0, false, model.Now())
	w.must(err)
	time.Sleep(2 * time.Millisecond)
	w.post(a, ann, parent.ID)
	if got := total(); got.UnreadThreads != 1 || got.Badge != 5 {
		t.Fatalf("a reply in a followed thread: %+v", got)
	}
	_, newest2, err := w.n.Summary(ctx, bob.ID)
	w.must(err)
	if newest1 == "" || newest1 == newest2 {
		t.Fatalf("newest did not change with a newer message: %q then %q", newest1, newest2)
	}

	// Reading, changing mode, blocking.
	last, err := w.s.Conversations().Get(ctx, a.ID)
	w.must(err)
	_, err = w.s.Members().SetRead(ctx, a.ID, bob.ID, last.LastSeq, false)
	w.must(err)
	w.n.Touch(bob.ID, a.ID)
	if got := total(); got.Badge != 3 || got.UnreadThreads != 1 {
		t.Fatalf("after reading alpha (threaded: the thread keeps its position): %+v", got)
	}
	w.mode(b, bob, setMode(model.ModeAny))
	if got := total(); got.Badge != 4 {
		t.Fatalf("beta on Any activity now counts both: %+v", got)
	}
	w.must(w.s.Blocks().Add(ctx, bob.ID, ann.ID, model.Now()))
	w.n.Forget(bob.ID)
	if got := total(); got != (model.Totals{}) {
		t.Fatalf("everything unread was by a blocked author: %+v", got)
	}
	w.must(w.s.Blocks().Remove(ctx, bob.ID, ann.ID))
	w.n.Forget(bob.ID)
	total()

	// Leaving a conversation takes its numbers with it.
	res, err := w.s.Members().RemoveDirect(ctx, b.ID, bob.ID)
	w.must(err)
	w.n.Changed(model.Event{Type: model.EventMemberLeft, ConversationID: b.ID, AlsoTo: []model.AccountID{bob.ID},
		Payload: map[string]any{"account_id": res.Changes[0].Member.AccountID}})
	if got := total(); got.Badge != 2 {
		t.Fatalf("after leaving beta: %+v", got)
	}
}

func TestRings(t *testing.T) {
	w := newWorld(t)
	ann := w.account("ann", model.RoleOwner)
	bob := w.account("bob", model.RoleMember)
	cat := w.account("cat", model.RoleMember)
	dan := w.account("dan", model.RoleMember)
	eve := w.account("eve", model.RoleMember)
	fay := w.account("fay", model.RoleMember)
	c := w.channel("calls", true, ann, bob, cat, dan, eve, fay)
	for _, a := range []model.Account{bob, cat, dan, eve, fay} {
		w.mode(c, a, setMode(model.ModeAny))
	}
	w.mode(c, cat, setMode(model.ModeMentions))                                         // not on Any activity
	w.update(dan, func(a *model.Account) { zero := model.Time(0); a.DNDUntil = &zero }) // in do not disturb
	w.must(w.s.Blocks().Add(ctx, eve.ID, ann.ID, model.Now()))                          // has blocked the starter
	_, err := w.s.Members().SetDirectLevel(ctx, c.ID, fay.ID, model.LevelReader)        // a reader
	w.must(err)
	rings, err := w.n.Rings(ctx, model.Call{ConversationID: c.ID, StartedByID: ann.ID})
	w.must(err)
	if len(rings) != 1 || rings[0] != bob.ID {
		t.Fatalf("rings %v, want only bob", rings)
	}
}
