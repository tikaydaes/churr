package storetest

import (
	"errors"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("chat_requests", testChatRequests) }

// testChatRequests is the chat_requests and ghost_members tables and the
// two conversation columns of migration 0007 (rulings R77, R78): one open
// request per pair and kind, answers that cannot both win, the counts the
// caps read, ghosts in the member key, closing, and whom an account has
// blocked kept in memory and dropped on every write.
func testChatRequests(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	cat := newAccount(t, s, "cat", model.RoleMember)
	now := model.Now()

	// A chat that waits: ann alone, bob a ghost she is shown.
	c := model.Conversation{Kind: model.KindChannel, Private: true, CreatedByID: ann.ID, CreatedAt: now, RequestState: model.ChatWaiting}
	must(t, s.Conversations().Create(ctx, &c))
	_, err := s.Members().AddDirect(ctx, c.ID, ann.ID, model.LevelManager, now)
	must(t, err)
	must(t, s.ChatRequests().AddGhost(ctx, model.Ghost{ConversationID: c.ID, AccountID: bob.ID, ShownTo: ann.ID, CreatedAt: now}))
	got, err := s.Conversations().ByMemberKey(ctx, store.AdhocMemberKey([]model.AccountID{ann.ID, bob.ID}))
	must(t, err)
	eq(t, "the ghost counts in the member key", got.ID, c.ID)
	eq(t, "waiting", got.RequestState, model.ChatWaiting)
	eq(t, "one real member", got.MemberCount, int64(1))

	r := model.ChatRequest{RequesterID: ann.ID, RecipientID: bob.ID, Kind: model.RequestDirect, ConversationID: c.ID,
		State: model.RequestPending, CreatedAt: now}
	must(t, s.ChatRequests().Create(ctx, &r))
	again := model.ChatRequest{RequesterID: ann.ID, RecipientID: bob.ID, Kind: model.RequestDirect, State: model.RequestPending}
	eq(t, "one open request per pair and kind", errors.Is(s.ChatRequests().Create(ctx, &again), model.ErrAlreadyExists), true)
	sealedToo := model.ChatRequest{RequesterID: ann.ID, RecipientID: bob.ID, Kind: model.RequestSealed, State: model.RequestPending}
	must(t, s.ChatRequests().Create(ctx, &sealedToo))

	open, found, err := s.ChatRequests().Open(ctx, ann.ID, bob.ID, model.RequestDirect)
	must(t, err)
	eq(t, "open", found && open.ID == r.ID, true)
	in, err := s.ChatRequests().Incoming(ctx, bob.ID)
	must(t, err)
	eq(t, "bob's list", len(in), 2)
	by, found, err := s.ChatRequests().ByConversation(ctx, c.ID)
	must(t, err)
	eq(t, "by conversation", found && by.ID == r.ID, true)

	// Dismissed is still open, and out of bob's list.
	must(t, s.ChatRequests().Answer(ctx, r.ID, []string{model.RequestPending}, model.RequestDismissed, now))
	eq(t, "a second answer loses", model.CodeOf(s.ChatRequests().Answer(ctx, r.ID, []string{model.RequestPending}, model.RequestDeclined, now)), model.CodeConflict)
	_, found, err = s.ChatRequests().Open(ctx, ann.ID, bob.ID, model.RequestDirect)
	must(t, err)
	eq(t, "dismissed still waits for ann", found, true)
	in, err = s.ChatRequests().Incoming(ctx, bob.ID)
	must(t, err)
	eq(t, "dismissed leaves bob's list", len(in), 1)

	// Counts: three unapproved (pending, dismissed, a quickly closed one),
	// one kept, one shown open.
	day := now.Add(-24 * time.Hour)
	counts, err := s.ChatRequests().Counts(ctx, ann.ID, day, model.RequestQuickClose.Milliseconds())
	must(t, err)
	eq(t, "two unapproved", counts.Unapproved, 2)
	quick := model.ChatRequest{RequesterID: ann.ID, RecipientID: cat.ID, Kind: model.RequestDirect, State: model.RequestAccepted,
		CreatedAt: now, AnsweredAt: now, ClosedAt: now.Add(time.Minute)}
	must(t, s.ChatRequests().Create(ctx, &quick))
	kept := model.ChatRequest{RequesterID: ann.ID, RecipientID: cat.ID, Kind: model.RequestSealed, State: model.RequestAccepted,
		CreatedAt: now, AnsweredAt: now, ClosedAt: now.Add(time.Hour)}
	must(t, s.ChatRequests().Create(ctx, &kept))
	shownOpen := model.ChatRequest{RequesterID: bob.ID, RecipientID: cat.ID, Kind: model.RequestDirect, State: model.RequestPending,
		ShownOpen: true, CreatedAt: now}
	must(t, s.ChatRequests().Create(ctx, &shownOpen))
	counts, err = s.ChatRequests().Counts(ctx, ann.ID, day, model.RequestQuickClose.Milliseconds())
	must(t, err)
	eq(t, "quickly closed is unapproved", counts.Unapproved, 3)
	eq(t, "closed after an hour is kept", counts.Kept, 1)
	counts, err = s.ChatRequests().Counts(ctx, bob.ID, day, model.RequestQuickClose.Milliseconds())
	must(t, err)
	eq(t, "shown open counts as kept", counts.Kept == 1 && counts.Unapproved == 0, true)
	counts, err = s.ChatRequests().Counts(ctx, ann.ID, now.Add(time.Hour), model.RequestQuickClose.Milliseconds())
	must(t, err)
	eq(t, "older than the day is not counted", counts.Kept+counts.Unapproved, 0)

	// Withdrawn and declined start the wait; accepted does not.
	last, err := s.ChatRequests().LastRefused(ctx, ann.ID, bob.ID)
	must(t, err)
	eq(t, "nothing refused yet", last.IsZero(), true)
	must(t, s.ChatRequests().Answer(ctx, r.ID, []string{model.RequestPending, model.RequestDismissed}, model.RequestWithdrawn, now.Add(time.Second)))
	last, err = s.ChatRequests().LastRefused(ctx, ann.ID, bob.ID)
	must(t, err)
	eq(t, "withdrawn", last, now.Add(time.Second))

	// Accepted: the ghost goes, bob is a member, the key stays.
	must(t, s.ChatRequests().RemoveGhosts(ctx, c.ID, bob.ID))
	must(t, s.Conversations().SetRequestState(ctx, c.ID, ""))
	_, err = s.Members().AddDirect(ctx, c.ID, bob.ID, model.LevelMember, now)
	must(t, err)
	got, err = s.Conversations().Get(ctx, c.ID)
	must(t, err)
	eq(t, "open", got.RequestState == "" && got.MemberCount == 2, true)
	ghosts, err := s.ChatRequests().Ghosts(ctx, c.ID)
	must(t, err)
	eq(t, "no ghosts", len(ghosts), 0)

	// Closed: no key, and a member change does not bring one back.
	must(t, s.ChatRequests().Create(ctx, &model.ChatRequest{RequesterID: ann.ID, RecipientID: bob.ID, Kind: model.RequestDirect,
		ConversationID: c.ID, State: model.RequestAccepted, CreatedAt: now, AnsweredAt: now}))
	must(t, s.Conversations().Close(ctx, c.ID, now.Add(time.Minute)))
	must(t, s.ChatRequests().Closed(ctx, c.ID, now.Add(time.Minute)))
	got, err = s.Conversations().Get(ctx, c.ID)
	must(t, err)
	eq(t, "closed", got.Closed() && got.MemberKey == "", true)
	_, err = s.Members().AddDirect(ctx, c.ID, cat.ID, model.LevelMember, now)
	must(t, err)
	got, err = s.Conversations().Get(ctx, c.ID)
	must(t, err)
	eq(t, "a closed chat keeps no key", got.MemberKey, "")
	by, _, err = s.ChatRequests().ByConversation(ctx, c.ID)
	must(t, err)
	eq(t, "the newest request is marked closed", by.ClosedAt, now.Add(time.Minute))

	// Ghosts shown to one viewer, by conversation.
	g := model.Conversation{Kind: model.KindChannel, Private: true, CreatedByID: bob.ID, CreatedAt: now}
	must(t, s.Conversations().Create(ctx, &g))
	_, err = s.Members().AddDirect(ctx, g.ID, bob.ID, model.LevelManager, now)
	must(t, err)
	_, err = s.Members().AddDirect(ctx, g.ID, cat.ID, model.LevelMember, now)
	must(t, err)
	must(t, s.ChatRequests().AddGhost(ctx, model.Ghost{ConversationID: g.ID, AccountID: ann.ID, ShownTo: bob.ID, CreatedAt: now}))
	shown, err := s.ChatRequests().GhostsShownTo(ctx, bob.ID, []model.ConversationID{g.ID, c.ID})
	must(t, err)
	eq(t, "bob is shown ann", len(shown[g.ID]) == 1 && shown[g.ID][0].AccountID == ann.ID, true)
	shown, err = s.ChatRequests().GhostsShownTo(ctx, cat.ID, []model.ConversationID{g.ID})
	must(t, err)
	eq(t, "cat is shown nobody", len(shown[g.ID]), 0)
	got, err = s.Conversations().ByMemberKey(ctx, store.AdhocMemberKey([]model.AccountID{ann.ID, bob.ID, cat.ID}))
	must(t, err)
	eq(t, "found as the chat of all three", got.ID, g.ID)
	peers, err := s.ChatRequests().GhostPeers(ctx, bob.ID)
	must(t, err)
	eq(t, "bob's ghost peer in an open chat", len(peers) == 1 && peers[0] == ann.ID, true)
	peers, err = s.ChatRequests().GhostPeers(ctx, ann.ID)
	must(t, err)
	eq(t, "the person asked in a waiting chat is no ghost peer", len(peers), 0)

	// Whom an account has blocked, kept in memory: every write is seen.
	list, err := s.Blocks().Blocking(ctx, ann.ID)
	must(t, err)
	eq(t, "nobody blocked", len(list), 0)
	must(t, s.Blocks().Add(ctx, ann.ID, bob.ID, now))
	list, err = s.Blocks().Blocking(ctx, ann.ID)
	must(t, err)
	eq(t, "bob blocked", len(list) == 1 && list[0] == bob.ID, true)
	must(t, s.Tx(ctx, func(tx store.Tx) error { return tx.Blocks().Add(ctx, ann.ID, cat.ID, now) }))
	list, err = s.Blocks().Blocking(ctx, ann.ID)
	must(t, err)
	eq(t, "a write in a transaction is seen after it", len(list), 2)
	must(t, s.Blocks().Remove(ctx, ann.ID, bob.ID))
	list, err = s.Blocks().Blocking(ctx, ann.ID)
	must(t, err)
	eq(t, "unblocked", len(list) == 1 && list[0] == cat.ID, true)
}
