package storetest

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("resume", testResume) }

// testResume covers the reads a catch-up makes once for one reader: each
// must say what the read for one message or one conversation says.
func testResume(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	c := newChannel(t, s, "resume", false, ann.ID)
	join(t, s, c.ID, bob.ID, model.LevelMember)
	d := newChannel(t, s, "resume-two", false, ann.ID)
	join(t, s, d.ID, bob.ID, model.LevelMember)

	// One account's mention rows among several messages.
	plain := post(t, s, c.ID, ann.ID, "", "nobody")
	named := post(t, s, c.ID, ann.ID, "", "bob", model.Mention{AccountID: bob.ID, Kind: model.MentionAccount})
	here := post(t, s, d.ID, ann.ID, "", "here", model.Mention{AccountID: bob.ID, Kind: model.MentionHere}, model.Mention{AccountID: ann.ID, Kind: model.MentionHere})
	ids := []model.MessageID{plain.ID, named.ID, here.ID, model.NewID[model.MessageID]()}
	kinds, err := s.Messages().MentionKinds(ctx, bob.ID, ids)
	must(t, err)
	eq(t, "bob's rows", len(kinds), 2)
	eq(t, "by name", kinds[named.ID], model.MentionAccount)
	eq(t, "here", kinds[here.ID], model.MentionHere)
	for _, id := range ids {
		rows, err := s.Messages().Mentions(ctx, id)
		must(t, err)
		want := ""
		for _, r := range rows {
			if r.AccountID == bob.ID {
				want = r.Kind
			}
		}
		eq(t, "the same as Mentions says", kinds[id], want)
	}
	kinds, err = s.Messages().MentionKinds(ctx, ann.ID, ids)
	must(t, err)
	eq(t, "ann's rows", len(kinds) == 1 && kinds[here.ID] == model.MentionHere, true)
	kinds, err = s.Messages().MentionKinds(ctx, bob.ID, nil)
	must(t, err)
	eq(t, "no messages, no rows", len(kinds), 0)

	// One account's states among several threads.
	_, err = s.Threads().Set(ctx, plain.ID, bob.ID, model.ThreadFollowing, 0, model.Now())
	must(t, err)
	_, err = s.Threads().Set(ctx, named.ID, bob.ID, model.ThreadIgnored, 0, model.Now())
	must(t, err)
	_, err = s.Threads().Set(ctx, here.ID, ann.ID, model.ThreadFollowing, 0, model.Now())
	must(t, err)
	states, err := s.Threads().StatesOf(ctx, bob.ID, ids)
	must(t, err)
	eq(t, "bob's states", len(states), 2)
	for _, id := range ids {
		st, found, err := s.Threads().Get(ctx, id, bob.ID)
		must(t, err)
		want := ""
		if found {
			want = st.State
		}
		eq(t, "the same as Get says", states[id], want)
	}
	states, err = s.Threads().StatesOf(ctx, bob.ID, nil)
	must(t, err)
	eq(t, "no threads, no states", len(states), 0)

	// Time-outs for every conversation at once.
	now := model.Now()
	hour := model.Time(3600 * 1000)
	check := func(account model.AccountID) {
		t.Helper()
		server, by, err := s.Moderation().TimedOutEverywhere(ctx, account, now)
		must(t, err)
		for _, conv := range []model.ConversationID{c.ID, d.ID, ""} {
			want, timedOut, err := s.Moderation().TimedOutUntil(ctx, account, conv, now)
			must(t, err)
			got := server
			if by[conv] > got {
				got = by[conv]
			}
			eq(t, "the same end as TimedOutUntil", got, want)
			eq(t, "the same answer as TimedOutUntil", !got.IsZero(), timedOut)
		}
	}
	check(bob.ID)
	out := func(conv model.ConversationID, until model.Time) *model.Timeout {
		to := &model.Timeout{AccountID: bob.ID, ConversationID: conv, Until: until, CreatedByID: ann.ID}
		must(t, s.Moderation().CreateTimeout(ctx, to))
		return to
	}
	out(c.ID, now+hour)
	out(c.ID, now+3*hour)
	check(bob.ID)
	out(d.ID, now-hour) // over
	lifted := out(d.ID, now+5*hour)
	must(t, s.Moderation().LiftTimeout(ctx, lifted.ID, now))
	check(bob.ID)
	out("", now+2*hour)
	check(bob.ID)
	check(ann.ID)
	server, by, err := s.Moderation().TimedOutEverywhere(ctx, bob.ID, now)
	must(t, err)
	eq(t, "server-wide", server, now+2*hour)
	eq(t, "one conversation has its own", len(by) == 1 && by[c.ID] == now+3*hour, true)
}
