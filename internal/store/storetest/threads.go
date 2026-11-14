package storetest

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("threads", testThreads) }

func testThreads(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	c := newChannel(t, s, "threads", false, ann.ID)
	join(t, s, c.ID, bob.ID, model.LevelMember)
	p := post(t, s, c.ID, ann.ID, "", "parent")
	r1 := post(t, s, c.ID, bob.ID, p.ID, "one")
	r2 := post(t, s, c.ID, bob.ID, p.ID, "two")

	_, found, err := s.Threads().Get(ctx, p.ID, ann.ID)
	must(t, err)
	eq(t, "neither", found, false)

	st, changed, err := s.Threads().Follow(ctx, p.ID, ann.ID, p.Seq, 0, false, model.Now())
	must(t, err)
	eq(t, "now following", changed && st.State == model.ThreadFollowing, true)
	eq(t, "conversation filled in", st.ConversationID, c.ID)
	eq(t, "unread replies", st.UnreadCount, int64(2))
	_, changed, _ = s.Threads().Follow(ctx, p.ID, ann.ID, 0, 0, false, model.Now())
	eq(t, "following again changes nothing", changed, false)

	st, found, err = s.Threads().SetRead(ctx, p.ID, ann.ID, r1.Seq, model.Now())
	must(t, err)
	eq(t, "read one", found && st.UnreadCount == 1, true)
	_, found, _ = s.Threads().SetRead(ctx, p.ID, bob.ID, r1.Seq, model.Now())
	eq(t, "nothing stored for neither", found, false)

	st, err = s.Threads().Set(ctx, p.ID, ann.ID, model.ThreadIgnored, 0, model.Now())
	must(t, err)
	eq(t, "ignored, position kept", st.State == model.ThreadIgnored && st.ReadSeq == r1.Seq, true)
	_, changed, _ = s.Threads().Follow(ctx, p.ID, ann.ID, 0, 0, false, model.Now())
	eq(t, "an ignorer is left alone", changed, false)
	st, changed, _ = s.Threads().Follow(ctx, p.ID, ann.ID, 0, r2.Seq, true, model.Now())
	eq(t, "unless it replied itself", changed && st.State == model.ThreadFollowing && st.ReadSeq == r2.Seq, true)

	// The replier's own row starts read up to its reply.
	st, _, err = s.Threads().Follow(ctx, p.ID, bob.ID, 0, r2.Seq, true, model.Now())
	must(t, err)
	eq(t, "bob has read his own", st.ReadSeq == r2.Seq && st.UnreadCount == 0, true)

	states, err := s.Threads().ForParents(ctx, ann.ID, []model.MessageID{p.ID, r1.ID})
	must(t, err)
	eq(t, "for parents", len(states), 1)
	list, next, err := s.Threads().ForAccount(ctx, ann.ID, model.ThreadFollowing, store.Page{})
	must(t, err)
	eq(t, "for account", len(list) == 1 && next == "", true)

	// The flat view reads replies in the conversation itself.
	st, _, _ = s.Threads().SetRead(ctx, p.ID, ann.ID, 0, model.Now())
	_, err = s.Members().SetRead(ctx, c.ID, ann.ID, r2.Seq, true)
	must(t, err)
	st, _, _ = s.Threads().Get(ctx, p.ID, ann.ID)
	eq(t, "thread position raised with the conversation's", st.ReadSeq, r2.Seq)

	must(t, s.Threads().Delete(ctx, p.ID, ann.ID))
	_, found, _ = s.Threads().Get(ctx, p.ID, ann.ID)
	eq(t, "back to neither", found, false)

	// Leaving takes the account's thread states along.
	_, err = s.Members().RemoveDirect(ctx, c.ID, bob.ID)
	must(t, err)
	_, found, _ = s.Threads().Get(ctx, p.ID, bob.ID)
	eq(t, "gone with the membership", found, false)
}
