package notify

import (
	"context"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// judgeAll is the rule worked out for everyone at once the way it was
// before rosters and account facts were kept in memory: it reads every
// member row and every account row from the tables, each time. It stays
// here as what Judge is compared with, since it cannot be stale.
func (s *Service) judgeAll(ctx context.Context, m model.Message, also []model.AccountID) (map[model.AccountID]*judged, model.Conversation, error) {
	now := s.now()
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return nil, model.Conversation{}, err
	}
	conv, err := s.store.Conversations().Get(ctx, m.ConversationID)
	if err != nil {
		return nil, model.Conversation{}, err
	}
	rows, err := s.store.Members().All(ctx, conv.ID)
	if err != nil {
		return nil, model.Conversation{}, err
	}
	mentions, err := s.store.Messages().Mentions(ctx, m.ID)
	if err != nil {
		return nil, model.Conversation{}, err
	}
	kinds := make(map[model.AccountID]string, len(mentions))
	for _, mn := range mentions {
		kinds[mn.AccountID] = mn.Kind
	}
	var states map[model.AccountID]string
	if m.ParentID != "" {
		if states, err = s.store.Threads().States(ctx, m.ParentID); err != nil {
			return nil, model.Conversation{}, err
		}
	}

	members := make(map[model.AccountID]model.Member, len(rows))
	seen := make(map[model.AccountID]bool, len(rows)+len(mentions)+len(also))
	ids := make([]model.AccountID, 0, len(rows)+len(mentions)+len(also))
	add := func(id model.AccountID) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, r := range rows {
		members[r.AccountID] = r
		add(r.AccountID)
	}
	for _, mn := range mentions {
		add(mn.AccountID)
	}
	for _, id := range also {
		add(id)
	}

	accounts := make([]model.Account, 0, len(ids))
	// Who has blocked the author is read whatever the server's setting
	// says: the setting decides whether a block stops a notification, but
	// the unread numbers leave a blocked author's messages out always.
	blockers := map[model.AccountID]bool{}
	for from := 0; from < len(ids); from += readChunk {
		chunk := ids[from:min(from+readChunk, len(ids))]
		got, err := s.store.Accounts().GetMany(ctx, chunk)
		if err != nil {
			return nil, model.Conversation{}, err
		}
		accounts = append(accounts, got...)
		blocking, err := s.store.Blocks().Blockers(ctx, m.AuthorID, chunk)
		if err != nil {
			return nil, model.Conversation{}, err
		}
		for _, id := range blocking {
			blockers[id] = true
		}
	}

	by := make(map[model.AccountID]*judged, len(accounts))
	// Requests to notify anyway are read once, and only if someone is in
	// do not disturb: a message that has just been posted has none.
	var reached map[model.AccountID]bool
	for _, acc := range accounts {
		mem, isMember := members[acc.ID]
		state := states[acc.ID]
		fc := facts{
			blocked: set.BlockingEnabled && m.AuthorID != acc.ID && blockers[acc.ID],
			conv:    conv, mem: mem, isMember: isMember, kind: kinds[acc.ID],
			following: isMember && state == model.ThreadFollowing,
			ignored:   isMember && state == model.ThreadIgnored,
		}
		j := &judged{v: rule(m, acc, now, fc), level: None}
		switch {
		case !j.v.eligible:
		case j.v.would && !DND(acc, now).On:
			j.level = Notify
		case j.v.would:
			if reached == nil {
				list, err := s.store.NotifyAnyway().Reached(ctx, m.ID)
				if err != nil {
					return nil, model.Conversation{}, err
				}
				reached = make(map[model.AccountID]bool, len(list))
				for _, id := range list {
					reached[id] = true
				}
			}
			if reached[acc.ID] {
				j.level = Notify
			} else if j.v.inBadge {
				j.level = Badge
			}
		case j.v.inBadge:
			j.level = Badge
		}
		if isMember {
			j.counts, j.mode, j.flat = true, mem.ModeInForce(now), FlatView(acc, mem)
			j.adds = added(m, acc.ID, blockers[acc.ID], fc)
		}
		by[acc.ID] = j
	}
	return by, conv, nil
}

// agree checks that Judge answers exactly as judgeAll does, for each kind
// of message and every account of the crowd, and that Notified names those
// judgeAll would notify and nobody else.
func (k *crowd) agree(what string, also ...model.Account) {
	t := k.w.t
	t.Helper()
	everyone := append(append([]model.Account{}, k.everyone...), also...)
	ids := make([]model.AccountID, len(everyone))
	for i, a := range everyone {
		ids[i] = a.ID
	}
	msgs := map[string]model.Message{
		"top-level":             k.store(""),
		"mentions":              k.store("", k.mentionsOf(everyone)...),
		"a reply":               k.store(k.parent.ID),
		"a reply that mentions": k.store(k.parent.ID, k.mentionsOf(everyone)...),
	}
	rem := msgs["top-level"]
	rem.Via = &model.Via{Kind: "reminder"}
	msgs["a reminder"] = rem
	sys := msgs["mentions"]
	sys.System = map[string]any{"kind": "joined"}
	msgs["a system notice"] = sys
	for name, m := range msgs {
		for _, extra := range [][]model.AccountID{nil, ids} {
			want, conv, err := k.w.n.judgeAll(ctx, m, extra)
			k.w.must(err)
			f, err := k.w.n.Judge(ctx, m, extra)
			k.w.must(err)
			if got := f.Accounts(); len(got) != len(want) {
				t.Errorf("%s, %s: Judge covers %d accounts, the tables say %d", what, name, len(got), len(want))
			}
			wakes := map[model.AccountID]bool{}
			for _, a := range everyone {
				w := want[a.ID]
				j, known := f.judge(a.ID)
				if known != (w != nil) {
					t.Errorf("%s, %s, %s: known %v, the tables say %v", what, name, a.Username, known, w != nil)
					continue
				}
				if w == nil {
					continue
				}
				if !sameVerdict(j.v, w.v) || j.level != w.level || j.adds != w.adds || j.counts != w.counts || j.mode != w.mode || j.flat != w.flat {
					t.Errorf("%s, %s, %s: Judge says %+v, the tables say %+v", what, name, a.Username, j, *w)
				}
				if w.level == Notify {
					wakes[a.ID] = w.v.named || conv.Sealed() || conv.Adhoc() || w.v.following
				}
				wake, high := f.Wake(a.ID)
				if wantHigh, wantWake := wakes[a.ID]; wake != wantWake || high != wantHigh {
					t.Errorf("%s, %s, %s: wakes %v/%v, the tables say %v/%v", what, name, a.Username, wake, high, wantWake, wantHigh)
				}
			}
			// Everyone the tables would notify is in want, covered or not
			// by the accounts looked at above.
			wantN := 0
			for _, w := range want {
				if w.level == Notify {
					wantN++
				}
			}
			gotN := 0
			k.w.must(f.Notified(func(to model.AccountID, high bool) {
				gotN++
				w := want[to]
				if w == nil || w.level != Notify {
					t.Errorf("%s, %s: Notified names %s, whom the tables would not notify", what, name, to)
				} else if wantHigh := w.v.named || conv.Sealed() || conv.Adhoc() || w.v.following; high != wantHigh {
					t.Errorf("%s, %s: Notified gives %s priority high=%v, want %v", what, name, to, high, wantHigh)
				}
			}))
			if gotN != wantN {
				t.Errorf("%s, %s: Notified names %d accounts, the tables would notify %d", what, name, gotN, wantN)
			}
		}
	}
}

func sameVerdict(a, b verdict) bool {
	return a.eligible == b.eligible && a.would == b.would && a.inBadge == b.inBadge && a.named == b.named &&
		a.anyMode == b.anyMode && a.following == b.following && a.account.ID == b.account.ID
}

func (k *crowd) mentionsOf(all []model.Account) []model.Mention {
	var out []model.Mention
	for i, a := range all {
		kind := model.MentionAccount
		if i%2 == 1 {
			kind = model.MentionChannel
		}
		out = append(out, mention(a, kind))
	}
	return out
}

// TestJudgeAgreesWithTheTables compares the rule answered from what is kept
// in memory with the rule worked out from the tables, over a crowd that
// differs in every way the rule looks at.
func TestJudgeAgreesWithTheTables(t *testing.T) {
	k := newCrowd(t)
	k.agree("blocking on")
	k.w.must(k.w.s.Settings().Set(ctx, store.SettingBlockingEnabled, false, ""))
	k.agree("blocking off")
	// The same again in an ad-hoc channel, where everyone is on any
	// activity and wake-ups are of high priority.
	adhoc := model.Conversation{Kind: model.KindChannel, Private: true, CreatedByID: k.ann.ID}
	k.w.must(k.w.s.Conversations().Create(ctx, &adhoc))
	for _, a := range k.members {
		_, err := k.w.s.Members().AddDirect(ctx, adhoc.ID, a.ID, model.LevelMember, model.Now())
		k.w.must(err)
	}
	k.c = adhoc
	k.parent = k.w.post(adhoc, k.ann, "")
	k.agree("an ad-hoc channel")
}

// TestNextMessageSeesEveryChange makes each kind of change that the kept
// rosters and account facts could miss, and checks that the very next
// message is judged with it: against the tables every time, and by the one
// answer the change was meant to move.
func TestNextMessageSeesEveryChange(t *testing.T) {
	k := newCrowd(t)
	w := k.w
	by := map[string]model.Account{}
	for _, a := range k.everyone {
		by[a.Username] = a
	}
	var extra []model.Account
	// level judges a new message and returns one account's answer.
	judge := func(a model.Account, parent model.MessageID, mentions ...model.Mention) judged {
		t.Helper()
		f, err := w.n.Judge(ctx, k.store(parent, mentions...), nil)
		w.must(err)
		j, known := f.judge(a.ID)
		if !known {
			// Not someone the message concerns at all.
			j.level = None
		}
		return j
	}
	level := func(a model.Account, mentions ...model.Mention) Level {
		t.Helper()
		return judge(a, "", mentions...).level
	}
	// step checks the answer before a change, makes it, and checks the
	// answer after; both times everything is compared with the tables.
	step := func(what string, a model.Account, before, after Level, change func(), mentions ...model.Mention) {
		t.Helper()
		k.agree("before "+what, extra...)
		if got := level(a, mentions...); got != before {
			t.Fatalf("before %s: %s is at %s, want %s", what, a.Username, got, before)
		}
		change()
		if got := level(a, mentions...); got != after {
			t.Errorf("after %s: the next message gives %s %s, want %s", what, a.Username, got, after)
		}
		k.agree("after "+what, extra...)
	}
	own := func(a model.Account, fn func(m *model.Member)) func() {
		return func() {
			_, err := w.s.Members().UpdateOwn(ctx, k.c.ID, a.ID, func(m *model.Member) error { fn(m); return nil })
			w.must(err)
		}
	}
	acct := func(a model.Account, fn func(a *model.Account)) func() {
		return func() {
			_, err := w.s.Accounts().Update(ctx, a.ID, func(a *model.Account) error { fn(a); return nil })
			w.must(err)
		}
	}
	men, anyone, eve, gus := by["men"], by["any"], by["eve"], by["gus"]
	zero := model.Time(0)

	// The membership's own settings.
	step("a mode change", men, None, Notify, own(men, setMode(model.ModeAny)))
	step("a mute", men, Notify, None, own(men, func(m *model.Member) {
		m.ModeBeforeMute, m.Mode, m.MutedUntil = m.Mode, model.ModeNothing, model.Now().Add(time.Hour)
	}))
	step("a mute that has run out", men, None, Notify, own(men, func(m *model.Member) { m.MutedUntil = model.Now().Add(-time.Minute) }))
	step("a mute cleared to mentions", men, Notify, None, own(men, func(m *model.Member) {
		m.Mode, m.ModeBeforeMute, m.MutedUntil = model.ModeMentions, "", 0
	}))
	if judge(anyone, k.parent.ID).flat {
		t.Fatal("the fixture's reader is in the flat view already")
	}
	own(anyone, func(m *model.Member) { m.ThreadView = "flat" })()
	if j := judge(anyone, k.parent.ID); !j.flat || j.level != Badge {
		t.Errorf("after a change of view the next reply is judged %+v, want the flat view and a badge", j)
	}
	own(anyone, func(m *model.Member) { m.ThreadView = "" })()

	// The account: do not disturb from each of its sources, prefs.
	step("do not disturb", anyone, Notify, Badge, acct(anyone, func(a *model.Account) { a.DNDUntil = &zero }))
	step("do not disturb ending", anyone, Badge, Notify, acct(anyone, func(a *model.Account) { a.DNDUntil = nil }))
	step("a status that pauses notifications", anyone, Notify, Badge, acct(anyone, func(a *model.Account) {
		a.StatusText, a.StatusDND = "heads down", true
	}))
	step("the status cleared", anyone, Badge, Notify, acct(anyone, func(a *model.Account) { a.StatusText, a.StatusDND = "", false }))
	step("quiet hours", anyone, Notify, Badge, acct(anyone, func(a *model.Account) {
		now := time.Now().UTC()
		a.TimeZone, a.QuietDays = "UTC", "1111111"
		a.QuietStart, a.QuietEnd = now.Add(-2*time.Hour).Format("15:04"), now.Add(2*time.Hour).Format("15:04")
	}))
	step("skipping the quiet period", anyone, Badge, Notify, acct(anyone, func(a *model.Account) {
		a.QuietSkipUntil = model.Now().Add(3 * time.Hour)
	}))
	step("quiet hours cleared", anyone, Notify, Notify, acct(anyone, func(a *model.Account) {
		a.QuietStart, a.QuietEnd, a.QuietDays, a.QuietSkipUntil = "", "", "", 0
	}))
	step("a meeting", anyone, Notify, Notify, acct(anyone, func(a *model.Account) {
		a.AvailabilityIs, a.AvailabilityEnd = model.AvailabilityMeeting, model.Now().Add(time.Hour)
	}))
	step("meetings pausing notifications", anyone, Notify, Badge, acct(anyone, func(a *model.Account) {
		a.PrefsJSON = []byte(`{"meeting_pauses":true}`)
	}))
	step("the meeting ending", anyone, Badge, Notify, acct(anyone, func(a *model.Account) { a.AvailabilityIs, a.AvailabilityEnd = "", 0 }))
	acct(anyone, func(a *model.Account) { a.PrefsJSON = []byte(`{"thread_view":"flat"}`) })()
	if j := judge(anyone, k.parent.ID); !j.flat {
		t.Error("after the account's view preference changed the next reply is not judged in the flat view")
	}
	acct(anyone, func(a *model.Account) { a.PrefsJSON = []byte(`{}`) })()

	// Membership: leaving, joining, through a group, and by role.
	counted := func(a model.Account) bool { return judge(a, "", mention(a, model.MentionAccount)).counts }
	step("leaving", anyone, Notify, None, func() {
		_, err := w.s.Members().RemoveDirect(ctx, k.c.ID, anyone.ID)
		w.must(err)
	})
	if counted(anyone) {
		t.Error("someone who left is still counted as a member")
	}
	step("joining", anyone, None, None, func() {
		_, err := w.s.Members().AddDirect(ctx, k.c.ID, anyone.ID, model.LevelMember, model.Now())
		w.must(err)
	})
	if !counted(anyone) {
		t.Error("someone who joined is not counted as a member by the next message")
	}
	step("the newcomer's mode", anyone, None, Notify, own(anyone, setMode(model.ModeAny)))

	g := model.Group{Handle: "crew", Name: "crew"}
	w.must(w.s.Groups().Create(ctx, &g))
	_, _, err := w.s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: k.c.ID, GroupID: g.ID, Level: model.LevelMember, AddedByID: k.ann.ID, AddedAt: model.Now()})
	w.must(err)
	k.agree("a group was added")
	if counted(eve) {
		t.Fatal("eve is a member before joining the group")
	}
	_, err = w.s.Groups().AddMember(ctx, g.ID, eve.ID, k.ann.ID, model.Now())
	w.must(err)
	if counted(eve) {
		t.Fatal("eve is a member before accepting the channel her group was offered (ruling R103)")
	}
	_, err = w.s.Members().AnswerOffer(ctx, k.c.ID, eve.ID, true, model.Now())
	w.must(err)
	if !counted(eve) {
		t.Error("someone added to a group of the channel is not a member for the next message")
	}
	k.agree("someone joined the group")
	_, err = w.s.Groups().RemoveMember(ctx, g.ID, eve.ID)
	w.must(err)
	if counted(eve) {
		t.Error("someone taken out of a group of the channel is still a member for the next message")
	}
	k.agree("someone left the group")

	// Someone who is not a member, mentioned in a public channel: a
	// member of the server is notified, a guest is not.
	step("a role change to guest", eve, Notify, None, func() {
		_, _, err := w.s.Accounts().SetRole(ctx, eve.ID, model.RoleGuest)
		w.must(err)
	}, mention(eve, model.MentionAccount))
	step("a role change to member", gus, None, Notify, func() {
		_, _, err := w.s.Accounts().SetRole(ctx, gus.ID, model.RoleMember)
		w.must(err)
	}, mention(gus, model.MentionAccount))

	// Blocks and thread states are read for each message; they are here
	// so that stays true.
	step("a block", anyone, Notify, None, func() { w.must(w.s.Blocks().Add(ctx, anyone.ID, k.ann.ID, model.Now())) })
	step("the block lifted", anyone, None, Notify, func() { w.must(w.s.Blocks().Remove(ctx, anyone.ID, k.ann.ID)) })
	if got := judge(men, k.parent.ID).level; got != None {
		t.Fatalf("a reply notifies someone on mentions who does not follow the thread: %s", got)
	}
	_, err = w.s.Threads().Set(ctx, k.parent.ID, men.ID, model.ThreadFollowing, 0, model.Now())
	w.must(err)
	if got := judge(men, k.parent.ID).level; got != Notify {
		t.Errorf("after following the thread the next reply gives %s, want notify", got)
	}

	// An account nobody has claimed is told nothing until it is claimed.
	ivy := model.Account{Username: "ivy", DisplayName: "ivy", Role: model.RoleMember}
	w.must(w.s.Accounts().CreateUnclaimed(ctx, &ivy))
	extra = append(extra, ivy)
	step("an account being claimed", ivy, None, Notify, func() {
		_, err := w.s.Accounts().Claim(ctx, ivy.ID, model.Now())
		w.must(err)
	}, mention(ivy, model.MentionAccount))

	// Removal: gone from the conversation and told nothing.
	step("an account being removed", anyone, Notify, None, func() {
		_, err := w.s.Accounts().Remove(ctx, anyone.ID, k.ann.ID, model.Now())
		w.must(err)
	})
	if counted(anyone) {
		t.Error("a removed account is still counted as a member")
	}

	// A change made inside a transaction with other work is seen once it
	// has committed.
	step("a change committed in a transaction", men, None, Notify, func() {
		w.must(w.s.Tx(ctx, func(tx store.Tx) error {
			_, err := tx.Members().UpdateOwn(ctx, k.c.ID, men.ID, func(m *model.Member) error { m.Mode = model.ModeAny; return nil })
			return err
		}))
	})
}
