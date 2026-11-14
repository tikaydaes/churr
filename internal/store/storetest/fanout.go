package storetest

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("fanout", testFanout) }

// testFanout covers the reads a fan-out makes once for everyone.
func testFanout(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	cat := newAccount(t, s, "cat", model.RoleMember)
	c := newChannel(t, s, "fanout", false, ann.ID)
	join(t, s, c.ID, bob.ID, model.LevelMember)
	other := newChannel(t, s, "elsewhere", false, ann.ID)
	join(t, s, other.ID, cat.ID, model.LevelMember)

	// Every member row of one conversation, and of no other.
	rows, err := s.Members().All(ctx, c.ID)
	must(t, err)
	eq(t, "member rows", len(rows), 2)
	for _, r := range rows {
		one, found, err := s.Members().Get(ctx, c.ID, r.AccountID)
		must(t, err)
		eq(t, "a row from All is the row from Get", found && one == r, true)
	}
	rows, err = s.Members().All(ctx, model.NewID[model.ConversationID]())
	must(t, err)
	eq(t, "a conversation that does not exist has no rows", len(rows), 0)

	// Everyone's state for one thread.
	p := post(t, s, c.ID, ann.ID, "", "parent")
	q := post(t, s, c.ID, ann.ID, "", "another parent")
	states, err := s.Threads().States(ctx, p.ID)
	must(t, err)
	eq(t, "no states yet", len(states), 0)
	_, err = s.Threads().Set(ctx, p.ID, ann.ID, model.ThreadFollowing, 0, model.Now())
	must(t, err)
	_, err = s.Threads().Set(ctx, p.ID, bob.ID, model.ThreadIgnored, 0, model.Now())
	must(t, err)
	_, err = s.Threads().Set(ctx, q.ID, bob.ID, model.ThreadFollowing, 0, model.Now())
	must(t, err)
	states, err = s.Threads().States(ctx, p.ID)
	must(t, err)
	eq(t, "states of one thread only", len(states), 2)
	eq(t, "ann follows", states[ann.ID], model.ThreadFollowing)
	eq(t, "bob ignores", states[bob.ID], model.ThreadIgnored)

	// Who a notify-anyway request for a message was delivered to.
	_, _, err = s.NotifyAnyway().Record(ctx, p.ID, bob.ID, ann.ID, model.Now(), true)
	must(t, err)
	_, _, err = s.NotifyAnyway().Record(ctx, p.ID, cat.ID, ann.ID, model.Now(), false)
	must(t, err)
	reached, err := s.NotifyAnyway().Reached(ctx, p.ID)
	must(t, err)
	eq(t, "reached", len(reached) == 1 && reached[0] == bob.ID, true)
	reached, err = s.NotifyAnyway().Reached(ctx, q.ID)
	must(t, err)
	eq(t, "nobody reached for a message without a request", len(reached), 0)

	// The unread sets say which seq they were read at, and that is the
	// conversation's last.
	u, err := s.Members().Unread(ctx, c.ID, bob.ID)
	must(t, err)
	eq(t, "as of the last message", u.AsOf, q.Seq)
	eq(t, "two unread", u.Top, int64(2))
	last := post(t, s, c.ID, ann.ID, "", "one more")
	u, err = s.Members().Unread(ctx, c.ID, bob.ID)
	must(t, err)
	eq(t, "as of moves with the conversation", u.AsOf, last.Seq)
	u, err = s.Members().Unread(ctx, other.ID, cat.ID)
	must(t, err)
	eq(t, "a conversation nothing was posted in", u.AsOf, int64(0))

	// A mention that is not by name, in a reply in a thread the account
	// neither follows nor ignores, is a mention like any other.
	dan := newAccount(t, s, "dan", model.RoleMember)
	join(t, s, c.ID, dan.ID, model.LevelMember)
	post(t, s, c.ID, ann.ID, q.ID, "a reply", model.Mention{AccountID: dan.ID, Kind: model.MentionChannel})
	u, err = s.Members().Unread(ctx, c.ID, dan.ID)
	must(t, err)
	eq(t, "MEN counts the reply", u.Men, int64(1))
	eq(t, "it is in NEI", u.MenNei, int64(1))
	eq(t, "it is not by name", u.Nam, int64(0))
	// The same in an ignored thread is left out, unless it is by name.
	_, err = s.Threads().Set(ctx, q.ID, dan.ID, model.ThreadIgnored, 0, model.Now())
	must(t, err)
	u, err = s.Members().Unread(ctx, c.ID, dan.ID)
	must(t, err)
	eq(t, "MEN leaves out an ignored thread", u.Men, int64(0))
	post(t, s, c.ID, ann.ID, q.ID, "by name", model.Mention{AccountID: dan.ID, Kind: model.MentionAccount})
	u, err = s.Members().Unread(ctx, c.ID, dan.ID)
	must(t, err)
	eq(t, "MEN keeps a mention by name in an ignored thread", u.Men == 1 && u.MenIgn == 1 && u.Nam == 1, true)
}
