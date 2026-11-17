package chat

import (
	"context"
	"errors"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/notify"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// First contact (ruling R103; docs/FEATURES.md section 20.2): nobody ends up
// in a conversation they did not accept.
//
// Being included in a group chat (an ad-hoc channel of three or more), when
// it is started or later, is a request of kind `group` to that person,
// unless they approve new direct chats automatically or already have an
// open direct chat with whoever included them. Being added to a named
// channel by its moderators or managers is an invitation of kind `channel`,
// always. Until they accept, the person is not a member: they get no
// messages, no notifications but the one request, and no rings. The members
// see them as invited, not yet joined. Someone who has blocked the one
// inviting is invited all the same and is never told: invited for ever, as
// someone who never answers is.

// invitePlan is what including one person comes to.
type invitePlan struct {
	// Open: the person is added at once (a group chat, and they approve
	// new direct chats automatically or have an open chat with the actor).
	Open bool
	// Deaf: the person has blocked the actor. Nothing reaches them.
	Deaf bool
}

// planInvite works out what inviting other to a conversation of a kind
// comes to, and refuses it within a day of other declining something of
// the actor's, or past the daily caps (ruling R78). The checks are the same
// whether or not the person has blocked the actor.
func (s *Service) planInvite(ctx context.Context, a store.Areas, p *perm.Checker, actor, other model.Account, kind string) (invitePlan, error) {
	var plan invitePlan
	deaf, err := p.HasBlocked(ctx, other.ID, actor.ID)
	if err != nil {
		return plan, err
	}
	plan.Deaf = deaf
	if kind == model.RequestGroup {
		// The preference that governs direct chats governs group chats:
		// both are being drawn into a conversation by one person.
		if notify.ReadPrefs(other).AutoApproveDirect {
			plan.Open = true
		} else if plan.Open, err = s.openChatWith(ctx, a, actor.ID, other.ID); err != nil {
			return plan, err
		}
	}
	now := s.requestNow()
	last, err := a.ChatRequests().LastRefused(ctx, actor.ID, other.ID)
	if err != nil {
		return plan, err
	}
	if !last.IsZero() && now.Sub(last) < model.RequestWait {
		return plan, model.ErrRateLimited.With("You can invite " + displayName(other) + " again a day after the last request ended.")
	}
	since := now.Add(-24 * time.Hour)
	if kind == model.RequestChannel {
		counts, err := a.ChatRequests().Counts(ctx, actor.ID, since, model.RequestQuickClose.Milliseconds(), model.RequestChannel)
		if err != nil {
			return plan, err
		}
		if counts.Unapproved >= model.ChannelInvitesPerDay {
			return plan, model.ErrRateLimited.With("You have invited as many people to channels as you may in a day. Try again tomorrow.")
		}
		return plan, nil
	}
	counts, err := a.ChatRequests().Counts(ctx, actor.ID, since, model.RequestQuickClose.Milliseconds(),
		model.RequestDirect, model.RequestSealed, model.RequestGroup)
	if err != nil {
		return plan, err
	}
	if plan.Open && counts.Kept >= model.RequestsKeptPerDay {
		return plan, model.ErrRateLimited.With("You have started as many new chats as you may in a day. Try again tomorrow.")
	}
	if !plan.Open && counts.Unapproved >= model.RequestsUnapprovedPerDay {
		return plan, model.ErrRateLimited.With("You have asked as many people to chat as you may in a day. Try again tomorrow.")
	}
	return plan, nil
}

// openChatWith reports whether two people have a direct chat that is open:
// both are in it, nothing waits and it was not closed.
func (s *Service) openChatWith(ctx context.Context, a store.Areas, one, other model.AccountID) (bool, error) {
	c, err := a.Conversations().ByMemberKey(ctx, store.AdhocMemberKey([]model.AccountID{one, other}))
	if errors.Is(err, model.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if c.Closed() || c.Waiting() {
		return false, nil
	}
	for _, id := range []model.AccountID{one, other} {
		if _, found, err := a.Members().Get(ctx, c.ID, id); err != nil || !found {
			return false, err
		}
	}
	return true, nil
}

// invite includes one local person in a conversation that exists, inside
// the transaction that makes or changes it: added at once for a group chat
// they would open anyway, otherwise a request they accept. level is the
// level a channel invitation brings. pending reports whether an invitation
// now waits that the members are shown.
func (s *Service) invite(ctx context.Context, tx store.Tx, rec *Recorder, p *perm.Checker, actor, target model.Account, c model.Conversation, kind string, level model.Level, override bool) (pending bool, err error) {
	plan, err := s.planInvite(ctx, tx, p, actor, target, kind)
	if err != nil {
		return false, err
	}
	now := s.now()
	r := model.ChatRequest{RequesterID: actor.ID, RecipientID: target.ID, Kind: kind, ConversationID: c.ID,
		State: model.RequestPending, Level: level, Override: override, CreatedAt: now}
	switch {
	case plan.Open && !plan.Deaf:
		res, err := tx.Members().AddDirect(ctx, c.ID, target.ID, level, now)
		if errors.Is(err, model.ErrAlreadyExists) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if err := rec.Membership(ctx, res, actor.ID, ""); err != nil {
			return false, err
		}
		// Kept, for the caps.
		r.State, r.AnsweredAt = model.RequestAccepted, now
		return false, tx.ChatRequests().Create(ctx, &r)
	case plan.Open && plan.Deaf:
		// They would have been added at once: the actor is shown them
		// added, to the actor alone, and nothing reaches them (ruling
		// R77).
		if err := s.ghostJoins(ctx, tx, rec, c.ID, target.ID, actor.ID, now); err != nil {
			return false, err
		}
		r.State, r.AnsweredAt, r.ShownOpen = model.RequestAccepted, now, true
		return false, tx.ChatRequests().Create(ctx, &r)
	}
	if err := tx.ChatRequests().Create(ctx, &r); errors.Is(err, model.ErrAlreadyExists) {
		return false, nil // invited already
	} else if err != nil {
		return false, err
	}
	if !plan.Deaf {
		recipient, requester := target, actor
		rec.After(func() { s.sendRequest(ctx, recipient, requester, r, true) })
	}
	return true, nil
}

// invitedKey carries, through a listing, the open invitations of each
// conversation on the page, read once.
type invitedKey struct{}

// invitedIDs is who a conversation's members are shown as invited, not yet
// joined, in the order they were invited.
func (s *Service) invitedIDs(ctx context.Context, a store.Areas, conv model.ConversationID) ([]model.AccountID, error) {
	if pre, ok := ctx.Value(invitedKey{}).(map[model.ConversationID][]model.ChatRequest); ok {
		if list, ok := pre[conv]; ok {
			return requestRecipients(list), nil
		}
	}
	all, err := a.ChatRequests().Invitations(ctx, conv)
	if err != nil {
		return nil, err
	}
	return requestRecipients(all[conv]), nil
}

func requestRecipients(list []model.ChatRequest) []model.AccountID {
	var out []model.AccountID
	seen := map[model.AccountID]bool{}
	for _, r := range list {
		if !seen[r.RecipientID] {
			seen[r.RecipientID] = true
			out = append(out, r.RecipientID)
		}
	}
	return out
}

// preloadInvited reads the open invitations of a page of conversations.
func (s *Service) preloadInvited(ctx context.Context, convs []model.ConversationID) context.Context {
	if len(convs) == 0 {
		return ctx
	}
	all, err := s.store.ChatRequests().Invitations(ctx, convs...)
	if err != nil {
		return ctx
	}
	for _, c := range convs {
		if _, ok := all[c]; !ok {
			all[c] = nil
		}
	}
	return context.WithValue(ctx, invitedKey{}, all)
}

// invitedMembers is the invited people of a conversation as member rows
// marked invited, for a member list.
func (s *Service) invitedMembers(ctx context.Context, a store.Areas, conv model.ConversationID) ([]model.Member, error) {
	all, err := a.ChatRequests().Invitations(ctx, conv)
	if err != nil {
		return nil, err
	}
	var out []model.Member
	seen := map[model.AccountID]bool{}
	for _, r := range all[conv] {
		if seen[r.RecipientID] {
			continue
		}
		seen[r.RecipientID] = true
		level := r.Level
		if level == "" {
			level = model.LevelMember
		}
		out = append(out, model.Member{ConversationID: conv, AccountID: r.RecipientID, Level: level, JoinedAt: r.CreatedAt, Invited: true})
	}
	return out, nil
}

// acceptInvitation makes the person invited a member, at the level the
// invitation brings, from now on: in a private channel or a group chat
// they read from their join (rulings R87, R103). An Owner's override is
// announced to every member as such.
func (s *Service) acceptInvitation(ctx context.Context, actor model.Account, r model.ChatRequest) (model.Conversation, error) {
	wasShown := r.State == model.RequestPending
	err := s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		c, err := tx.Conversations().Get(ctx, r.ConversationID)
		if err != nil {
			return err
		}
		if c.Closed() || c.Archived() {
			return model.ErrNotFound
		}
		if err := s.notBanned(ctx, tx, c.ID, actor.ID); err != nil {
			return err
		}
		now := s.now()
		if err := tx.ChatRequests().Answer(ctx, r.ID, []string{model.RequestPending, model.RequestDismissed}, model.RequestAccepted, now); err != nil {
			return err
		}
		level := r.Level
		if level == "" {
			level = model.LevelMember
		}
		var res store.MembershipResult
		if r.Override {
			rec.override = true
			res, err = tx.Members().AddByOverride(ctx, c.ID, actor.ID, level, now)
		} else {
			res, err = tx.Members().AddDirect(ctx, c.ID, actor.ID, level, now)
		}
		if errors.Is(err, model.ErrAlreadyExists) {
			return nil
		}
		if err != nil {
			return err
		}
		if c.Adhoc() {
			n, err := tx.Members().Count(ctx, c.ID)
			if err != nil {
				return err
			}
			if n > model.MaxAdhocMembers {
				return model.ErrTooLarge.With("This chat is full.")
			}
		}
		if err := rec.Membership(ctx, res, r.RequesterID, ""); err != nil {
			return err
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

// declineInvitation answers an invitation with "no": the members no longer
// see the person as invited, and whoever invited them may not ask them
// again for a day.
func (s *Service) declineInvitation(ctx context.Context, r model.ChatRequest) error {
	return s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		if err := tx.ChatRequests().Answer(ctx, r.ID, []string{model.RequestPending}, model.RequestDeclined, s.now()); err != nil {
			return err
		}
		c, err := tx.Conversations().Get(ctx, r.ConversationID)
		if errors.Is(err, model.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return s.conversationEvent(ctx, tx, rec, model.EventConversationUpdated, c, "")
	})
}

// withdrawInvitation takes back an invitation that waits: a moderator, a
// manager or (in a group chat) any member taking the person out before
// they joined.
func (s *Service) withdrawInvitation(ctx context.Context, actor model.Account, st perm.State, account model.AccountID) error {
	var gone []model.ChatRequest
	err := s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		var err error
		if gone, err = tx.ChatRequests().EndInvitations(ctx, st.Conversation.ID, account, model.RequestWithdrawn, s.now()); err != nil {
			return err
		}
		c, err := tx.Conversations().Get(ctx, st.Conversation.ID)
		if err != nil {
			return err
		}
		return s.conversationEvent(ctx, tx, rec, model.EventConversationUpdated, c, actor.ID)
	})
	for _, r := range gone {
		s.tellRequestGone(r.RecipientID, r.ID)
	}
	return err
}

// invitationShown is what the person invited is shown of the conversation:
// for a group chat the names of who else is in it, nothing more; for a
// named channel the channel as someone outside it sees it.
func (s *Service) invitationShown(ctx context.Context, recipient model.Account, r model.ChatRequest, out *model.RequestShown) error {
	out.ConversationID = r.ConversationID
	out.Override = r.Override
	c, err := s.store.Conversations().Get(ctx, r.ConversationID)
	if err != nil {
		return err
	}
	if r.Kind == model.RequestChannel {
		shown := outsiderView(c)
		out.Channel = &shown
		out.Level = r.Level
		if out.Level == "" {
			out.Level = c.DefaultLevel
		}
		return nil
	}
	ids, err := s.store.Members().IDs(ctx, c.ID)
	if err != nil {
		return err
	}
	var others []model.AccountID
	for _, id := range ids {
		if id != recipient.ID {
			others = append(others, id)
		}
	}
	if len(others) > model.MaxAdhocMembers {
		others = others[:model.MaxAdhocMembers]
	}
	accs, err := s.store.Accounts().GetMany(ctx, others)
	if err != nil {
		return err
	}
	out.People = []string{}
	for _, a := range accs {
		if s.showAccount != nil {
			if shown, err := s.showAccount(ctx, recipient, a); err == nil {
				a = shown
			}
		}
		out.People = append(out.People, displayName(a))
	}
	return nil
}

// outsiderView is a named channel as someone who is not in it is shown it
// before they join: its name, topic, access and size, and nothing of what
// is said, who is there or whether a call is on.
func outsiderView(c model.Conversation) model.Conversation {
	return model.Conversation{ID: c.ID, Kind: c.Kind, Name: c.Name, Topic: c.Topic, Private: c.Private, Knock: c.Knock,
		Access: c.AccessKind(), MemberCount: c.MemberCount, CreatedAt: c.CreatedAt, SectionID: c.SectionID,
		DefaultLevel: c.DefaultLevel, Announcement: c.Announcement}
}

// Offers lists the named channels offered to the actor that wait for an
// answer (ruling R103): through a group added to them, or as a default
// channel made one after the actor signed up.
func (s *Service) Offers(ctx context.Context, actor model.Account) ([]model.ChannelOffer, error) {
	if !actor.Active() {
		return nil, model.ErrUnauthenticated
	}
	list, err := s.store.Members().Offers(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	out := []model.ChannelOffer{}
	for _, o := range list {
		c, err := s.store.Conversations().Get(ctx, o.ConversationID)
		if err != nil {
			continue
		}
		shown := outsiderView(c)
		o.Channel = &shown
		out = append(out, o)
	}
	return out, nil
}

// AnswerOffer accepts or declines a channel the actor may join without an
// invitation. Accepting makes them a member at the level the offer brings
// and returns the channel; declining closes the offer (and takes away a
// membership it brought), and they may still join later from Browse while
// the access lasts.
func (s *Service) AnswerOffer(ctx context.Context, actor model.Account, id model.ConversationID, accept bool) (model.Conversation, error) {
	st, err := s.perm.Load(ctx, actor, id)
	if err != nil {
		return model.Conversation{}, hide(err)
	}
	if !st.Conversation.Named() || actor.Role == model.RoleBot || !actor.Active() {
		return model.Conversation{}, model.ErrNotFound
	}
	if !st.Eligible && !(st.Member != nil && st.Member.ViaGroup) {
		return model.Conversation{}, model.ErrNotFound
	}
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		res, err := tx.Members().AnswerOffer(ctx, id, actor.ID, accept, s.now())
		if err != nil {
			return err
		}
		reason := ""
		if !accept {
			reason = "left"
		}
		return rec.Membership(ctx, res, "", reason)
	})
	if err != nil {
		return model.Conversation{}, hide(err)
	}
	s.hub.Send(actor.ID, model.Event{Type: model.EventChannelOfferRemoved, At: s.now(), ConversationID: id})
	if !accept {
		return model.Conversation{}, nil
	}
	return s.Conversation(ctx, actor, id)
}

// tellOffer sends an account a channel offered to it, after the commit.
func (s *Service) tellOffer(ctx context.Context, conv model.ConversationID, account model.AccountID, level model.Level, at model.Time) {
	o, found, err := s.store.Members().Offer(ctx, conv, account)
	if err != nil || !found || o.State != model.OfferOffered {
		return
	}
	c, err := s.store.Conversations().Get(ctx, conv)
	if err != nil {
		return
	}
	shown := outsiderView(c)
	o.Channel, o.Level, o.CreatedAt = &shown, level, at
	s.hub.Send(account, model.Event{Type: model.EventChannelOfferAdded, At: s.now(), Payload: map[string]any{"offer": o}})
}

// offerChange turns a change to an offer into the event its account gets
// after the commit, and reports whether the change was one.
func (r *Recorder) offerChange(ctx context.Context, ch store.MemberChange) bool {
	m := ch.Member
	switch ch.Kind {
	case store.MemberOffered:
		r.After(func() { r.s.tellOffer(ctx, m.ConversationID, m.AccountID, m.Level, m.JoinedAt) })
		return true
	case store.MemberOfferGone:
		r.After(func() {
			r.s.hub.Send(m.AccountID, model.Event{Type: model.EventChannelOfferRemoved, At: r.s.now(), ConversationID: m.ConversationID})
		})
		return true
	}
	return false
}

// joinedAnswers settles what waited for someone who has just joined by any
// route: an invitation to that conversation is accepted by it, and leaves
// their list.
func (r *Recorder) joinedAnswers(ctx context.Context, m model.Member) error {
	ended, err := r.tx.ChatRequests().EndInvitations(ctx, m.ConversationID, m.AccountID, model.RequestAccepted, r.s.now())
	if err != nil {
		return err
	}
	for _, e := range ended {
		id, to := e.ID, e.RecipientID
		r.After(func() { r.s.tellRequestGone(to, id) })
	}
	return nil
}

// DefaultChannelsChanged is told by the settings when the default channels
// change: a channel that became one is offered to everyone already here
// who is not in it, and one that stopped being one is no longer offered.
func (s *Service) DefaultChannelsChanged(ctx context.Context, before, after []model.ConversationID) error {
	was := map[model.ConversationID]bool{}
	for _, id := range before {
		was[id] = true
	}
	now := map[model.ConversationID]bool{}
	for _, id := range after {
		now[id] = true
	}
	return s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		var res store.MembershipResult
		for _, id := range after {
			if was[id] {
				continue
			}
			r, err := tx.Members().OfferDefault(ctx, id, s.now())
			if err != nil {
				return err
			}
			res.Merge(r)
		}
		for _, id := range before {
			if now[id] {
				continue
			}
			r, err := tx.Members().WithdrawDefault(ctx, id)
			if err != nil && !errors.Is(err, model.ErrNotFound) {
				return err
			}
			res.Merge(r)
		}
		return rec.Membership(ctx, res, "", "")
	})
}
