package chat

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/notify"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Chat requests, closing a chat, and a block as deaf ears (rulings R77, R78,
// R79; docs/FEATURES.md section 20).
//
// A new direct chat or sealed chat with someone who does not approve new
// chats automatically is a request. Its conversation holds only the person
// who asked, who is shown the other person through a ghost row and cannot
// write until it is accepted; the person asked is sent the request and
// nothing else. A block takes the same path to the point of delivery and
// then delivers nothing: the person blocked sees exactly what anyone else
// would see while waiting for an answer, or an open chat when the person
// approves chats automatically, and the blocker gets nothing at all.

// AccountShower shows one account to a viewer as the people service does.
type AccountShower func(ctx context.Context, viewer, a model.Account) (model.Account, error)

// SetAccountShower connects the people service, which shows the person asked
// who is asking. It is set once while the server is being put together.
func (s *Service) SetAccountShower(f AccountShower) { s.showAccount = f }

// RequestPlan is what asking someone for a new chat comes to. PlanChat makes
// it; the conversation is then made with Members and State, the request is
// stored with RecordRequestTx in the same transaction, and DeliverRequest
// tells the person asked after the commit.
type RequestPlan struct {
	Actor, Other model.Account
	Kind         string
	// Plain: no request is involved (a bot, someone on another server):
	// the chat opens as it always has.
	Plain bool
	// Open: the person asked approves chats of this kind automatically.
	Open bool
	// Deaf: the person asked has blocked the actor. Nothing reaches them;
	// the actor cannot tell.
	Deaf bool
}

// opens reports whether the person asked becomes a member at once.
func (p RequestPlan) opens() bool { return p.Plain || (p.Open && !p.Deaf) }

// Members is who the new conversation's members are: the actor, and the
// person asked if the chat opens for real now.
func (p RequestPlan) Members() []model.AccountID {
	if p.opens() {
		return []model.AccountID{p.Actor.ID, p.Other.ID}
	}
	return []model.AccountID{p.Actor.ID}
}

// State is the request state the new conversation starts with: waiting,
// unless the actor is shown it open.
func (p RequestPlan) State() string {
	if p.Plain || p.Open {
		return ""
	}
	return model.ChatWaiting
}

// requestable reports whether a chat with other goes through a request: a
// person of this server, not the actor. Chat requests do not travel between
// servers (docs/FEATURES.md section 20).
func requestable(actor, other model.Account) bool {
	return other.ID != actor.ID && other.Local() && other.Role != model.RoleBot && other.Role != model.RoleRemote
}

// PlanChat works out what asking other for a new chat of a kind comes to,
// and refuses it if the actor has asked too often (ruling R78): within a day
// of a decline or of withdrawing from that person, or past the caps. The
// checks are the same whether or not the person has blocked the actor, and
// count only what the actor can see of their own requests.
func (s *Service) PlanChat(ctx context.Context, actor, other model.Account, kind string) (RequestPlan, error) {
	p := RequestPlan{Actor: actor, Other: other, Kind: kind}
	if !requestable(actor, other) {
		p.Plain = true
		return p, nil
	}
	deaf, err := s.perm.HasBlocked(ctx, other.ID, actor.ID)
	if err != nil {
		return p, err
	}
	p.Deaf = deaf
	prefs := notify.ReadPrefs(other)
	p.Open = (kind == model.RequestDirect && prefs.AutoApproveDirect) || (kind == model.RequestSealed && prefs.AutoApproveSealed)
	now := s.requestNow()
	last, err := s.store.ChatRequests().LastRefused(ctx, actor.ID, other.ID)
	if err != nil {
		return p, err
	}
	if !last.IsZero() && now.Sub(last) < model.RequestWait {
		return p, model.ErrRateLimited.With("You can ask " + displayName(other) + " to chat again a day after the last request ended.")
	}
	counts, err := s.store.ChatRequests().Counts(ctx, actor.ID, now.Add(-24*time.Hour), model.RequestQuickClose.Milliseconds())
	if err != nil {
		return p, err
	}
	if p.Open && counts.Kept >= model.RequestsKeptPerDay {
		return p, model.ErrRateLimited.With("You have started as many new chats as you may in a day. Try again tomorrow.")
	}
	if !p.Open && counts.Unapproved >= model.RequestsUnapprovedPerDay {
		return p, model.ErrRateLimited.With("You have asked as many people to chat as you may in a day. Try again tomorrow.")
	}
	return p, nil
}

func displayName(a model.Account) string {
	if a.DisplayName != "" {
		return a.DisplayName
	}
	return a.Username
}

// RecordRequestTx stores the request a plan makes for a new conversation,
// in the transaction that made it: the request row, and the person asked as
// a ghost the actor is shown, unless they are a member already. A chat the
// actor is shown open although it reaches nobody gets the member.joined the
// actor would have had: the actor is its only member, so nobody else hears
// of it, and its sequence numbers run as in any new chat.
func (s *Service) RecordRequestTx(ctx context.Context, tx store.Tx, rec *Recorder, p RequestPlan, conv model.Conversation) (model.ChatRequest, error) {
	if p.Plain {
		return model.ChatRequest{}, nil
	}
	if !p.opens() {
		g := model.Ghost{ConversationID: conv.ID, AccountID: p.Other.ID, ShownTo: p.Actor.ID, CreatedAt: conv.CreatedAt}
		if err := tx.ChatRequests().AddGhost(ctx, g); err != nil {
			return model.ChatRequest{}, err
		}
		if p.Open {
			m := withGhosts(nil, []model.Ghost{g}, conv.ID)[0]
			if _, err := rec.Emit(ctx, model.Event{Type: model.EventMemberJoined, ConversationID: conv.ID,
				Payload: map[string]any{"member": m, "by_id": p.Actor.ID}}); err != nil {
				return model.ChatRequest{}, err
			}
		}
	}
	r := model.ChatRequest{RequesterID: p.Actor.ID, RecipientID: p.Other.ID, Kind: p.Kind, ConversationID: conv.ID,
		State: model.RequestPending, ShownOpen: p.Open && p.Deaf, CreatedAt: conv.CreatedAt}
	if p.opens() {
		r.State, r.AnsweredAt = model.RequestAccepted, conv.CreatedAt
	}
	return r, tx.ChatRequests().Create(ctx, &r)
}

// DeliverRequest tells the person asked of a request that waits for them,
// after the commit. A request from someone they have blocked, or one that
// opened at once, is not delivered: here, and only here, the two paths
// part.
func (s *Service) DeliverRequest(ctx context.Context, p RequestPlan, r model.ChatRequest) {
	if p.Plain || p.Deaf || r.State != model.RequestPending {
		return
	}
	s.sendRequest(ctx, p.Other, p.Actor, r, true)
}

// sendRequest sends chat_request.received to the person asked. notifyNow
// asks for a notification like a mention, unless they are in do not
// disturb; it never shows more than who is asking.
func (s *Service) sendRequest(ctx context.Context, recipient, requester model.Account, r model.ChatRequest, notifyNow bool) {
	shown, err := s.shownRequest(ctx, recipient, requester, r)
	if err != nil {
		s.log.Warn("a chat request could not be shown to the person asked", "err", err)
		return
	}
	notifies := notifyNow && !notify.DND(recipient, s.now()).On
	s.hub.Send(recipient.ID, model.Event{Type: model.EventChatRequestReceived, At: s.now(),
		Payload: map[string]any{"request": shown, "notify": notifies}})
	if notifies && s.waker != nil {
		// Their phones are woken as for a message, with nothing in the
		// wake-up; the app asks what it is about (docs/PUSH.md section 2).
		s.waker.RequestReceived(recipient.ID, r.ConversationID)
	}
}

func (s *Service) shownRequest(ctx context.Context, recipient, requester model.Account, r model.ChatRequest) (model.RequestShown, error) {
	acc := requester
	if s.showAccount != nil {
		var err error
		if acc, err = s.showAccount(ctx, recipient, requester); err != nil {
			return model.RequestShown{}, err
		}
	}
	return model.RequestShown{ID: r.ID, Kind: r.Kind, Account: acc, CreatedAt: r.CreatedAt}, nil
}

// tellRequestGone tells the person asked that a request left their list.
func (s *Service) tellRequestGone(recipient model.AccountID, id string) {
	s.hub.Send(recipient, model.Event{Type: model.EventChatRequestRemoved, At: s.now(), Payload: map[string]any{"id": id}})
}

// startDirect is FindOrCreateChat for a chat with one person of this
// server: the chat the two have, or a new one that opens at once or waits
// for them to accept.
func (s *Service) startDirect(ctx context.Context, actor, other model.Account) (model.Conversation, bool, error) {
	key := store.AdhocMemberKey([]model.AccountID{actor.ID, other.ID})
	for attempt := 0; ; attempt++ {
		c, err := s.store.Conversations().ByMemberKey(ctx, key)
		switch {
		case err == nil:
			out, err := s.existingChat(ctx, actor, c)
			return out, false, err
		case !errors.Is(err, model.ErrNotFound):
			return model.Conversation{}, false, err
		}
		p, err := s.PlanChat(ctx, actor, other, model.RequestDirect)
		if err != nil {
			return model.Conversation{}, false, err
		}
		c = model.Conversation{Kind: model.KindChannel, Private: true, CreatedByID: actor.ID, CreatedAt: s.now(), RequestState: p.State()}
		var r model.ChatRequest
		err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
			if err := tx.Conversations().Create(ctx, &c); err != nil {
				return err
			}
			for i, id := range p.Members() {
				level := model.LevelMember
				if i == 0 {
					level = model.LevelManager
				}
				res, err := tx.Members().AddDirect(ctx, c.ID, id, level, c.CreatedAt)
				if err != nil {
					return err
				}
				if err := rec.Membership(ctx, res, actor.ID, ""); err != nil {
					return err
				}
			}
			r, err = s.RecordRequestTx(ctx, tx, rec, p, c)
			return err
		})
		if errors.Is(err, model.ErrAlreadyExists) && attempt == 0 {
			continue
		}
		if err != nil {
			return model.Conversation{}, false, err
		}
		s.DeliverRequest(ctx, p, r)
		out, err := s.Conversation(ctx, actor, c.ID)
		return out, true, err
	}
}

// existingChat is the chat a pair already has, for one of them: theirs if
// they are in it; if it is the other person's request to them (waiting,
// dismissed, declined, or kept from them by their own block), starting a
// chat with that person accepts it.
func (s *Service) existingChat(ctx context.Context, actor model.Account, c model.Conversation) (model.Conversation, error) {
	if _, member, err := s.store.Members().Get(ctx, c.ID, actor.ID); err != nil {
		return model.Conversation{}, err
	} else if member {
		return s.Conversation(ctx, actor, c.ID)
	}
	r, found, err := s.store.ChatRequests().ByConversation(ctx, c.ID)
	if err != nil {
		return model.Conversation{}, err
	}
	if !found || r.RecipientID != actor.ID || !r.ClosedAt.IsZero() {
		return model.Conversation{}, model.ErrNotFound
	}
	return s.accept(ctx, actor, r)
}

// ExistingSealed is existingChat for the sealed package.
func (s *Service) ExistingSealed(ctx context.Context, actor model.Account, c model.Conversation) (model.Conversation, error) {
	return s.existingChat(ctx, actor, c)
}

// accept opens the chat of a request for the person asked: they become a
// member, the ghost goes, and both are told.
func (s *Service) accept(ctx context.Context, actor model.Account, r model.ChatRequest) (model.Conversation, error) {
	wasShown := r.State == model.RequestPending
	err := s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		c, err := tx.Conversations().Get(ctx, r.ConversationID)
		if err != nil {
			return err
		}
		if c.Closed() {
			return model.ErrNotFound
		}
		now := s.now()
		if err := tx.ChatRequests().Answer(ctx, r.ID, []string{model.RequestPending, model.RequestDismissed, model.RequestDeclined},
			model.RequestAccepted, now); err != nil {
			return err
		}
		if err := tx.ChatRequests().RemoveGhosts(ctx, c.ID, actor.ID); err != nil {
			return err
		}
		if err := tx.Conversations().SetRequestState(ctx, c.ID, ""); err != nil {
			return err
		}
		res, err := tx.Members().AddDirect(ctx, c.ID, actor.ID, model.LevelMember, now)
		if err != nil && !errors.Is(err, model.ErrAlreadyExists) {
			return err
		}
		if err == nil {
			if err := rec.Membership(ctx, res, actor.ID, ""); err != nil {
				return err
			}
		}
		updated, err := tx.Conversations().Get(ctx, c.ID)
		if err != nil {
			return err
		}
		return s.conversationEvent(ctx, tx, rec, model.EventConversationUpdated, updated, "")
	})
	if err != nil {
		return model.Conversation{}, err
	}
	if wasShown {
		s.tellRequestGone(actor.ID, r.ID)
	}
	return s.Conversation(ctx, actor, r.ConversationID)
}

// shownTo returns a request to the actor that is in their list: pending,
// from someone they have not blocked, whose chat was not closed. Anything
// else is not_found, the same as a request that does not exist.
func (s *Service) shownTo(ctx context.Context, actor model.Account, id string) (model.ChatRequest, error) {
	r, err := s.store.ChatRequests().Get(ctx, id)
	if err != nil {
		return r, err
	}
	if r.RecipientID != actor.ID || r.State != model.RequestPending || !r.ClosedAt.IsZero() || r.ConversationID == "" {
		return r, model.ErrNotFound
	}
	if blocked, err := s.perm.HasBlocked(ctx, actor.ID, r.RequesterID); err != nil {
		return r, err
	} else if blocked {
		return r, model.ErrNotFound
	}
	return r, nil
}

// Requests lists the chat requests waiting for the actor, oldest first:
// who is asking and for what kind of chat.
func (s *Service) Requests(ctx context.Context, actor model.Account) ([]model.RequestShown, error) {
	if !actor.Active() {
		return nil, model.ErrUnauthenticated
	}
	list, err := s.store.ChatRequests().Incoming(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	out := []model.RequestShown{}
	for _, r := range list {
		if !r.ClosedAt.IsZero() || r.ConversationID == "" {
			continue
		}
		if blocked, err := s.perm.HasBlocked(ctx, actor.ID, r.RequesterID); err != nil {
			return nil, err
		} else if blocked {
			continue
		}
		requester, err := s.store.Accounts().Get(ctx, r.RequesterID)
		if err != nil || requester.Removed() {
			continue
		}
		shown, err := s.shownRequest(ctx, actor, requester, r)
		if err != nil {
			continue
		}
		out = append(out, shown)
	}
	return out, nil
}

// RequestRequester returns who made a request in the actor's list, for
// blocking them from it.
func (s *Service) RequestRequester(ctx context.Context, actor model.Account, id string) (model.AccountID, error) {
	r, err := s.shownTo(ctx, actor, id)
	return r.RequesterID, err
}

// AcceptRequest opens the chat of a request in the actor's list.
func (s *Service) AcceptRequest(ctx context.Context, actor model.Account, id string) (model.Conversation, error) {
	r, err := s.shownTo(ctx, actor, id)
	if err != nil {
		return model.Conversation{}, err
	}
	return s.accept(ctx, actor, r)
}

// DeclineRequest answers a request with "I don't want to". The person
// asking sees it was declined and may not ask again for a day. A chat they
// were shown open (the actor approves automatically and had blocked them
// when they asked) is shown to them closed instead.
func (s *Service) DeclineRequest(ctx context.Context, actor model.Account, id string) error {
	r, err := s.shownTo(ctx, actor, id)
	if err != nil {
		return err
	}
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		now := s.now()
		if err := tx.ChatRequests().Answer(ctx, r.ID, []string{model.RequestPending}, model.RequestDeclined, now); err != nil {
			return err
		}
		if r.ShownOpen {
			if err := tx.Conversations().Close(ctx, r.ConversationID, now); err != nil {
				return err
			}
			if err := tx.ChatRequests().Closed(ctx, r.ConversationID, now); err != nil {
				return err
			}
		} else if err := tx.Conversations().SetRequestState(ctx, r.ConversationID, model.ChatDeclined); err != nil {
			return err
		}
		c, err := tx.Conversations().Get(ctx, r.ConversationID)
		if err != nil {
			return err
		}
		return s.conversationEvent(ctx, tx, rec, model.EventConversationUpdated, c, "")
	})
	if err != nil {
		return err
	}
	s.tellRequestGone(actor.ID, r.ID)
	return nil
}

// DismissRequest takes a request out of the actor's list. The person asking
// goes on seeing it wait, as if it had not been seen.
func (s *Service) DismissRequest(ctx context.Context, actor model.Account, id string) error {
	r, err := s.shownTo(ctx, actor, id)
	if err != nil {
		return err
	}
	if err := s.store.ChatRequests().Answer(ctx, r.ID, []string{model.RequestPending}, model.RequestDismissed, s.now()); err != nil {
		return err
	}
	s.tellRequestGone(actor.ID, r.ID)
	return nil
}

// BlockChanged is told by the people service when blocker blocks blocked
// (on) or lifts the block: requests from blocked leave blocker's list, or
// come back to it. A request that comes back does not notify again.
func (s *Service) BlockChanged(ctx context.Context, blocker, blocked model.AccountID, on bool) {
	for _, kind := range []string{model.RequestDirect, model.RequestSealed} {
		r, found, err := s.store.ChatRequests().Open(ctx, blocked, blocker, kind)
		if err != nil || !found || r.State != model.RequestPending || !r.ClosedAt.IsZero() || r.ConversationID == "" {
			continue
		}
		if on {
			s.tellRequestGone(blocker, r.ID)
			continue
		}
		recipient, err1 := s.store.Accounts().Get(ctx, blocker)
		requester, err2 := s.store.Accounts().Get(ctx, blocked)
		if err1 == nil && err2 == nil {
			s.sendRequest(ctx, recipient, requester, r, false)
		}
	}
}

// directChat reports whether a conversation is a direct chat or sealed chat
// for the actor: sealed, or an ad-hoc channel the actor is shown with one
// other person in it.
func (s *Service) directChat(ctx context.Context, actor model.Account, c model.Conversation) (bool, error) {
	if c.Sealed() {
		return true, nil
	}
	if !c.Adhoc() {
		return false, nil
	}
	ids, err := s.shownMemberIDs(ctx, s.store, actor, c.ID)
	if err != nil {
		return false, err
	}
	return len(ids) == 2, nil
}

// Close closes a direct or sealed chat for both people (rulings R78, R79):
// both keep its history to read, neither can write in it, and it does not
// say who closed it. A new chat between them starts with a new request.
// A chat whose request still waits is withdrawn instead (Withdraw).
func (s *Service) Close(ctx context.Context, actor model.Account, id model.ConversationID) (model.Conversation, error) {
	st, err := s.perm.Can(ctx, actor, perm.See, id)
	if err != nil {
		return model.Conversation{}, hide(err)
	}
	if !st.IsMember() {
		return model.Conversation{}, model.ErrNotFound
	}
	c := st.Conversation
	if ok, err := s.directChat(ctx, actor, c); err != nil {
		return model.Conversation{}, err
	} else if !ok {
		return model.Conversation{}, model.ErrForbidden.With("Only a direct chat or a sealed chat can be closed. Use /leave or hide it instead.")
	}
	if c.Closed() {
		return s.Conversation(ctx, actor, id)
	}
	if c.RequestState == model.ChatWaiting {
		return model.Conversation{}, model.ErrConflict.With("This chat request still waits: withdraw it instead.")
	}
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		now := s.now()
		if err := tx.Conversations().Close(ctx, id, now); err != nil {
			return err
		}
		if err := tx.ChatRequests().Closed(ctx, id, now); err != nil {
			return err
		}
		updated, err := tx.Conversations().Get(ctx, id)
		if err != nil {
			return err
		}
		// No by_id: closing never says who closed.
		return s.conversationEvent(ctx, tx, rec, model.EventConversationUpdated, updated, "")
	})
	if err != nil {
		return model.Conversation{}, err
	}
	return s.Conversation(ctx, actor, id)
}

// Withdraw takes back a request of the actor's that still waits. Its chat
// goes, and the actor may not ask that person again for a day.
func (s *Service) Withdraw(ctx context.Context, actor model.Account, id model.ConversationID) error {
	st, err := s.perm.Can(ctx, actor, perm.See, id)
	if err != nil {
		return hide(err)
	}
	if !st.IsMember() {
		return model.ErrNotFound
	}
	r, found, err := s.store.ChatRequests().ByConversation(ctx, id)
	if err != nil {
		return err
	}
	if !found || r.RequesterID != actor.ID || !r.Open() || r.ShownOpen || st.Conversation.RequestState != model.ChatWaiting {
		return model.ErrConflict.With("Only a chat request that still waits can be withdrawn.")
	}
	shown := r.State == model.RequestPending
	if shown {
		blocked, err := s.perm.HasBlocked(ctx, r.RecipientID, actor.ID)
		if err != nil {
			return err
		}
		shown = !blocked
	}
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		if err := tx.ChatRequests().Answer(ctx, r.ID, []string{model.RequestPending, model.RequestDismissed}, model.RequestWithdrawn, s.now()); err != nil {
			return err
		}
		res, err := tx.Members().RemoveDirect(ctx, id, actor.ID)
		if err != nil {
			return err
		}
		return rec.Membership(ctx, res, "", "left")
	})
	if err != nil {
		return err
	}
	if shown {
		s.tellRequestGone(r.RecipientID, r.ID)
	}
	return nil
}

// ghostsKey carries, through a listing, the ghosts each conversation shows
// the viewer, read once for the page.
type ghostsKey struct{}

// shownMemberIDs is who a viewer is shown in a conversation: its members
// and the ghosts the viewer is shown, in the order members are listed
// (joined_at, then id).
func (s *Service) shownMemberIDs(ctx context.Context, a store.Areas, viewer model.Account, conv model.ConversationID) ([]model.AccountID, error) {
	ghosts, err := s.ghostsFor(ctx, a, viewer, conv)
	if err != nil {
		return nil, err
	}
	if len(ghosts) == 0 {
		return a.Members().IDs(ctx, conv)
	}
	members, _, err := a.Members().List(ctx, conv, "", store.Page{Limit: model.MaxAdhocMembers + 1})
	if err != nil {
		return nil, err
	}
	all := withGhosts(members, ghosts, conv)
	out := make([]model.AccountID, len(all))
	for i, m := range all {
		out[i] = m.AccountID
	}
	return out, nil
}

// ghostsFor is the ghosts a viewer is shown in one conversation.
func (s *Service) ghostsFor(ctx context.Context, a store.Areas, viewer model.Account, conv model.ConversationID) ([]model.Ghost, error) {
	if viewer.ID == "" {
		return nil, nil
	}
	if pre, ok := ctx.Value(ghostsKey{}).(map[model.ConversationID][]model.Ghost); ok {
		return pre[conv], nil
	}
	all, err := a.ChatRequests().Ghosts(ctx, conv)
	if err != nil {
		return nil, err
	}
	var out []model.Ghost
	for _, g := range all {
		if g.ShownTo == viewer.ID {
			out = append(out, g)
		}
	}
	return out, nil
}

// withGhosts adds ghosts to a list of members as members at level member,
// in the order members are listed. Someone who has become a member is not
// listed twice.
func withGhosts(members []model.Member, ghosts []model.Ghost, conv model.ConversationID) []model.Member {
	have := make(map[model.AccountID]bool, len(members))
	for _, m := range members {
		have[m.AccountID] = true
	}
	out := append([]model.Member(nil), members...)
	for _, g := range ghosts {
		if have[g.AccountID] {
			continue
		}
		have[g.AccountID] = true
		out = append(out, model.Member{ConversationID: conv, AccountID: g.AccountID, Level: model.LevelMember,
			DirectLevel: model.LevelMember, JoinedAt: g.CreatedAt})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].JoinedAt != out[j].JoinedAt {
			return out[i].JoinedAt < out[j].JoinedAt
		}
		return out[i].AccountID < out[j].AccountID
	})
	return out
}

// preloadGhosts reads, for a page of conversations, the ghosts the viewer is
// shown, so showing each asks nothing more.
func (s *Service) preloadGhosts(ctx context.Context, viewer model.Account, convs []model.ConversationID) context.Context {
	if len(convs) == 0 || viewer.ID == "" {
		return ctx
	}
	byConv, err := s.store.ChatRequests().GhostsShownTo(ctx, viewer.ID, convs)
	if err != nil {
		return ctx
	}
	pre := make(map[model.ConversationID][]model.Ghost, len(byConv))
	for _, c := range convs {
		pre[c] = nil
	}
	for c, gs := range byConv {
		pre[c] = gs
	}
	return context.WithValue(ctx, ghostsKey{}, pre)
}

// ghostIDs is the member list of a conversation event for one reader, with
// the ghosts the reader is shown added, or nil when there are none.
func (s *Service) ghostIDs(ctx context.Context, conv model.ConversationID, to model.AccountID, ids []any) []model.AccountID {
	reader, err := s.store.Accounts().Get(ctx, to)
	if err != nil {
		return nil
	}
	ghosts, err := s.ghostsFor(ctx, s.store, reader, conv)
	if err != nil || len(ghosts) == 0 {
		return nil
	}
	shown, err := s.shownMemberIDs(ctx, s.store, reader, conv)
	if err != nil {
		return nil
	}
	return shown
}

// AdvanceRequestClock moves the clock the waits and caps of chat requests
// are measured by, for tests that need a day to pass.
func (s *Service) AdvanceRequestClock(d time.Duration) { s.requestSkew.Add(int64(d)) }

// requestNow is the time the waits and caps of chat requests are measured
// at.
func (s *Service) requestNow() model.Time { return s.now().Add(time.Duration(s.requestSkew.Load())) }

// ghostJoins shows someone who has blocked the actor as added to an ad-hoc
// channel, to the actor alone (ruling R77). In place of the member.joined a
// real addition makes, one conversation.updated is recorded: every member
// gets it and it changes nothing for them, while the actor's copy carries
// the member list they are shown (tailorConversationEvent), the same as on
// a reload. The conversation's events stay as many as a real addition
// makes.
func (s *Service) ghostJoins(ctx context.Context, tx store.Tx, rec *Recorder, conv model.ConversationID, account, actor model.AccountID, at model.Time) error {
	if err := tx.ChatRequests().AddGhost(ctx, model.Ghost{ConversationID: conv, AccountID: account, ShownTo: actor, CreatedAt: at}); err != nil {
		return err
	}
	c, err := tx.Conversations().Get(ctx, conv)
	if err != nil {
		return err
	}
	return s.conversationEvent(ctx, tx, rec, model.EventConversationUpdated, c, "")
}
