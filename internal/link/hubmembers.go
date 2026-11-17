package link

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is who is in a channel a hub owns (ruling R94,
// docs/LINK-PROTOCOL.md section 23a, "Who is in a channel"). The hub owns
// the channel, and its managers decide which instances are in it; who of
// each instance's people are in a private one is decided by its moderators
// and managers, from whichever instance is their home. Nobody else adds
// anyone to a private one (internal/perm, HubPrivate). Moderators and
// managers also remove people, on every instance, in any channel a hub owns.
//
// Requests, admissions and removals are events in the channel, as the
// removal of a message is: each is signed by the home instance of the
// account that acts, and every instance checks for itself that the account
// is on the hub-signed moderator or manager list as an account of the
// envelope's origin (`not_moderator` otherwise). Nothing new is kept on
// the hub. A request waits on every instance as a knock on its copy, so
// that the moderators and managers of every instance see it.

const (
	evMemberRequested = "member.requested"
	evMemberAdmitted  = "member.admitted"
	evMemberDeclined  = "member.request_declined"
	evMemberRemoved   = "member.removed"
)

// noGroupsIn refuses to put a private channel with groups in it under a
// hub: a group would bring people in that no moderator admitted.
func (s *Service) noGroupsIn(ctx context.Context, conv model.ConversationID) error {
	groups, err := s.store.Members().Groups(ctx, conv)
	if err != nil {
		return err
	}
	if len(groups) > 0 {
		return model.ErrConflict.With("A private channel shared through a hub has no groups in it: its moderators admit its people. Remove its groups first.")
	}
	return nil
}

// hubPrivate reports whether a stream is a private channel a hub owns.
func (s *Service) hubPrivate(ctx context.Context, a store.Areas, st store.Stream) bool {
	if !hubOwned(st) {
		return false
	}
	rec, _, err := s.channelOf(ctx, a, st.ID)
	return err == nil && rec.Private
}

// moderatorEvent is what an admission, a declined request and a removal
// carry: whom it is about and which moderator did it.
type moderatorEvent struct {
	AccountID   model.AccountID `json:"account_id"`
	Server      string          `json:"server"`
	ByAccountID model.AccountID `json:"by_account_id"`
	ByAccount   *wireProfile    `json:"by_account"`
	Blobs       []wireBlob      `json:"blobs"`
}

// byModerator checks a moderator's event the way every instance checks it:
// the account that acts is on the hub-signed moderator list as an account of
// the instance that signed the envelope. It shadows the moderator, who need
// not be a member of the channel and so may be new here.
func (a *apply) byModerator(b *moderatorEvent) (*rejection, error) {
	if rej := a.decode(b); rej != nil {
		return rej, nil
	}
	if !model.ValidID(string(b.AccountID)) {
		return reject("invalid"), nil
	}
	// Removing someone: any channel a hub owns. Admitting someone or
	// declining their request: a private one.
	if !hubOwned(a.st) || (a.in.Type != evMemberRemoved && !a.s.hubPrivate(a.ctx, a.tx, a.st)) {
		return reject("not_a_private_hub_channel"), nil
	}
	// A moderator's work, and so a manager's too.
	if !a.s.keeps(a.ctx, a.tx, a.st, b.ByAccountID, a.origin.Name) {
		return &rejection{reason: "not_moderator", detail: map[string]any{"account_id": b.ByAccountID, "origin": a.origin.Name, "type": a.in.Type}}, nil
	}
	if b.ByAccount != nil && b.ByAccount.ID == b.ByAccountID {
		if rej, err := a.profile(*b.ByAccount, b.Blobs, false); rej != nil || err != nil {
			return rej, err
		}
	}
	return nil, nil
}

// answerRequest records on this instance the answer to a request waiting
// here, if one is.
func (a *apply) answerRequest(account, by model.AccountID, outcome string) error {
	waiting, err := a.tx.Members().Knocks(a.ctx, a.conv.ID)
	if err != nil {
		return err
	}
	for _, k := range waiting {
		if k.AccountID == account {
			_, _, err := a.tx.Members().AnswerKnock(a.ctx, a.conv.ID, k.ID, by, outcome, a.in.At)
			return err
		}
	}
	return nil
}

// memberRequested is someone asking to be admitted. Their home instance
// sends it, with their profile; every instance keeps the request for its
// moderators to see.
func (a *apply) memberRequested() (*rejection, error) {
	var b struct {
		Account wireProfile `json:"account"`
		Blobs   []wireBlob  `json:"blobs"`
	}
	if rej := a.decode(&b); rej != nil {
		return rej, nil
	}
	if !a.s.hubPrivate(a.ctx, a.tx, a.st) {
		return reject("not_a_private_hub_channel"), nil
	}
	if b.Account.Bot {
		return reject("invalid"), nil
	}
	if rej, err := a.profile(b.Account, b.Blobs, false); rej != nil || err != nil {
		return rej, err
	}
	if _, rej, err := a.account(b.Account.ID); rej != nil || err != nil {
		return rej, err
	}
	if _, found, err := a.tx.Members().Get(a.ctx, a.conv.ID, b.Account.ID); err != nil || found {
		return nil, err
	}
	_, err := a.tx.Members().Knock(a.ctx, &model.Knock{ConversationID: a.conv.ID, AccountID: b.Account.ID, CreatedAt: a.in.At})
	return nil, err
}

// memberAdmitted is a moderator admitting someone. On the instance that is
// the person's home they join its copy, which is announced there and
// travels as member.joined; everywhere a request of theirs is answered.
func (a *apply) memberAdmitted() (*rejection, error) {
	var b moderatorEvent
	if rej, err := a.byModerator(&b); rej != nil || err != nil {
		return rej, err
	}
	if err := a.answerRequest(b.AccountID, b.ByAccountID, model.KnockAccepted); err != nil {
		return nil, err
	}
	target, err := a.tx.Accounts().Get(a.ctx, b.AccountID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, nil // someone of another instance this one has not met
	}
	if err != nil {
		return nil, err
	}
	if !target.Local() || !target.Active() || target.Role == model.RoleBot {
		return nil, nil
	}
	conv, by, st := a.conv.ID, b.ByAccountID, a.st.ID
	a.s.later(a.rec, func(ctx context.Context) {
		if err := a.s.again(ctx, func() error { return a.s.chat.JoinAdmitted(ctx, conv, target.ID, by) }); err != nil {
			a.s.log.Warn("link: an admitted member could not join", "stream", st, "err", err)
		}
	})
	return nil, nil
}

// memberDeclined is a moderator declining a request: it stops waiting
// everywhere.
func (a *apply) memberDeclined() (*rejection, error) {
	var b moderatorEvent
	if rej, err := a.byModerator(&b); rej != nil || err != nil {
		return rej, err
	}
	return nil, a.answerRequest(b.AccountID, b.ByAccountID, model.KnockDeclined)
}

// memberRemoved is a moderator removing someone: every instance takes them
// out of its copy.
func (a *apply) memberRemoved() (*rejection, error) {
	var b moderatorEvent
	if rej, err := a.byModerator(&b); rej != nil || err != nil {
		return rej, err
	}
	return nil, a.s.chat.ApplyLeave(a.ctx, a.tx, a.rec, a.conv, b.AccountID, "removed")
}

// ------------------------------------------------------------------- the API

// HubPerson is someone a moderator of a private channel a hub owns may
// admit, or who asks to be admitted (schema HubPerson).
type HubPerson struct {
	AccountID   model.AccountID `json:"account_id"`
	Username    string          `json:"username"`
	DisplayName string          `json:"display_name"`
	Server      string          `json:"server"`
	// RequestedAt is when they asked; absent for someone who has not.
	RequestedAt model.Time `json:"requested_at,omitempty"`
}

// present reports whether an account is one that can be admitted: an
// active account of this instance, or one of another that has not been
// removed there.
func present(acc model.Account) bool {
	if acc.Local() {
		return acc.Active()
	}
	return !acc.Removed()
}

func (s *Service) personOf(acc model.Account) HubPerson {
	server := s.id.Name
	if !acc.Local() {
		server = acc.ServerName
	}
	return HubPerson{AccountID: acc.ID, Username: acc.Username, DisplayName: acc.DisplayName, Server: server}
}

// privateFor loads a private channel a hub owns, active here with a copy.
func (s *Service) privateFor(ctx context.Context, id model.StreamID) (store.Stream, model.Conversation, error) {
	return s.hubCopy(ctx, id, true)
}

// hubCopy loads a channel a hub owns, active here with a copy; with
// private, only a private one.
func (s *Service) hubCopy(ctx context.Context, id model.StreamID, private bool) (store.Stream, model.Conversation, error) {
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil || !hubOwned(st) || st.State != store.StreamActive || st.ConversationID == "" || (private && !s.hubPrivate(ctx, s.store, st)) {
		return st, model.Conversation{}, model.ErrNotFound
	}
	conv, err := s.store.Conversations().Get(ctx, st.ConversationID)
	if err != nil {
		return st, conv, model.ErrNotFound
	}
	return st, conv, nil
}

// asHubModerator loads a private channel a hub owns for one of its
// moderators or managers, as an account of this instance.
func (s *Service) asHubModerator(ctx context.Context, by model.Account, id model.StreamID) (store.Stream, model.Conversation, error) {
	return s.asKeeper(ctx, by, id, true)
}

// asKeeper is asHubModerator for any channel a hub owns, or with private
// only a private one.
func (s *Service) asKeeper(ctx context.Context, by model.Account, id model.StreamID, private bool) (store.Stream, model.Conversation, error) {
	st, conv, err := s.hubCopy(ctx, id, private)
	if err != nil {
		return st, conv, err
	}
	rec, _, err := s.channelOf(ctx, s.store, id)
	if err != nil {
		return st, conv, model.ErrNotFound
	}
	if !s.isKeeper(by, rec) {
		return st, conv, model.ErrForbidden.With("Only a moderator or a manager of the channel can do that.")
	}
	return st, conv, nil
}

// AskToJoin is someone of this instance asking the moderators of a private
// channel a hub owns to admit them. The channel's identifier is what a
// moderator gave them: nobody here sees the channel before they are in it.
// Asking again while a request waits changes nothing.
func (s *Service) AskToJoin(ctx context.Context, by model.Account, id model.StreamID) (HubPerson, error) {
	if !by.Local() || !by.Active() || !by.Role.IsFullMember() {
		return HubPerson{}, model.ErrForbidden.With("You cannot ask to join channels.")
	}
	st, conv, err := s.privateFor(ctx, id)
	if err != nil {
		return HubPerson{}, err
	}
	if _, found, err := s.store.Members().Get(ctx, conv.ID, by.ID); err != nil {
		return HubPerson{}, err
	} else if found {
		return HubPerson{}, model.ErrConflict.With("You are in this channel already.")
	}
	me := s.personOf(by)
	waiting, err := s.store.Members().Knocks(ctx, conv.ID)
	if err != nil {
		return HubPerson{}, err
	}
	for _, k := range waiting {
		if k.AccountID == by.ID {
			me.RequestedAt = k.CreatedAt
			return me, nil
		}
	}
	now := s.now()
	n, oldest, err := s.store.Members().UnacceptedKnocks(ctx, by.ID, now.Add(-time.Hour))
	if err != nil {
		return HubPerson{}, err
	}
	if n >= chat.KnocksUnacceptedPerHour {
		return HubPerson{}, model.RateLimited(max(int(oldest.Add(time.Hour).Sub(now).Seconds())+1, 1))
	}
	p, blobs := s.profileOf(ctx, s.store, by)
	if err := s.originate(ctx, st, evMemberRequested, map[string]any{"account": p, "blobs": orEmptyBlobs(blobs)}, 0); err != nil {
		return HubPerson{}, err
	}
	s.audit(ctx, s.store, by.ID, "link.member_requested", "stream", string(id), nil)
	me.RequestedAt = now
	return me, nil
}

func orEmptyBlobs(b []wireBlob) []wireBlob {
	if b == nil {
		return []wireBlob{}
	}
	return b
}

// JoinRequests lists the requests waiting in a private channel a hub owns,
// for its moderators: those made on every instance in it.
func (s *Service) JoinRequests(ctx context.Context, by model.Account, id model.StreamID) ([]HubPerson, error) {
	_, conv, err := s.asHubModerator(ctx, by, id)
	if err != nil {
		return nil, err
	}
	waiting, err := s.store.Members().Knocks(ctx, conv.ID)
	if err != nil {
		return nil, err
	}
	out := []HubPerson{}
	for _, k := range waiting {
		acc, err := s.store.Accounts().Get(ctx, k.AccountID)
		if err != nil {
			continue
		}
		p := s.personOf(acc)
		p.RequestedAt = k.CreatedAt
		out = append(out, p)
	}
	return out, nil
}

// AdmitCandidates lists the people a moderator may admit from this
// instance: the active accounts this instance knows, of the instances in the
// channel, that are not in it. For another instance that is the people this
// one has met, in a shared conversation; anyone else asks to join.
func (s *Service) AdmitCandidates(ctx context.Context, by model.Account, id model.StreamID, query string) ([]HubPerson, error) {
	_, conv, err := s.asHubModerator(ctx, by, id)
	if err != nil {
		return nil, err
	}
	rec, _, err := s.channelOf(ctx, s.store, id)
	if err != nil {
		return nil, err
	}
	accounts, _, err := s.store.Accounts().List(ctx, store.AccountFilter{Query: strings.TrimSpace(query)}, store.Page{Limit: 200})
	if err != nil {
		return nil, err
	}
	out := []HubPerson{}
	for _, acc := range accounts {
		p := s.personOf(acc)
		if acc.Role == model.RoleBot || !present(acc) || !rec.Has(p.Server) {
			continue
		}
		if _, found, err := s.store.Members().Get(ctx, conv.ID, acc.ID); err != nil {
			return nil, err
		} else if found {
			continue
		}
		out = append(out, p)
		if len(out) == 50 {
			break
		}
	}
	return out, nil
}

// MemberAction is a moderator admitting someone, declining their request
// or removing them, from this instance; the account may be of any instance
// in the channel.
func (s *Service) MemberAction(ctx context.Context, by model.Account, id model.StreamID, action string, account model.AccountID) error {
	st, conv, err := s.asKeeper(ctx, by, id, action != "remove")
	if err != nil {
		return err
	}
	target, err := s.store.Accounts().Get(ctx, account)
	if err != nil {
		return model.Invalid("account_id", "There is no such account.")
	}
	_, member, err := s.store.Members().Get(ctx, conv.ID, account)
	if err != nil {
		return err
	}
	typ := ""
	switch action {
	case "admit":
		rec, _, err := s.channelOf(ctx, s.store, id)
		if err != nil {
			return err
		}
		if target.Role == model.RoleBot || !present(target) || !rec.Has(s.personOf(target).Server) {
			return model.Invalid("account_id", "Only people of the instances in the channel can be admitted.")
		}
		if member {
			return model.ErrConflict.With("They are in the channel already.")
		}
		typ = evMemberAdmitted
	case "decline":
		waiting, err := s.store.Members().Knocks(ctx, conv.ID)
		if err != nil {
			return err
		}
		asked := false
		for _, k := range waiting {
			asked = asked || k.AccountID == account
		}
		if !asked {
			return model.ErrNotFound.With("They have not asked to join, or have been answered.")
		}
		typ = evMemberDeclined
	case "remove":
		if !member {
			return model.ErrNotFound.With("They are not in the channel.")
		}
		typ = evMemberRemoved
	default:
		return model.Invalid("action", "The action is admit, decline or remove.")
	}
	p, blobs := s.profileOf(ctx, s.store, by)
	fields := map[string]any{"account_id": account, "server": s.personOf(target).Server, "by_account_id": by.ID, "by_account": p,
		"blobs": orEmptyBlobs(blobs)}
	if err := s.originate(ctx, st, typ, fields, 0); err != nil {
		return err
	}
	s.audit(ctx, s.store, by.ID, "link.member_"+action, "stream", string(id), map[string]any{"account_id": account})
	return nil
}

// syncManagers makes the levels of this server's copy of a channel a hub
// owns agree with a record: the accounts of this instance on the hub's
// manager list are its managers, and nobody else is.
func (s *Service) syncManagers(ctx context.Context, conv model.ConversationID, rec wire.ChannelRecord) {
	mods := map[model.AccountID]bool{}
	for _, m := range rec.Managers {
		if m.Server == s.id.Name {
			mods[model.AccountID(m.AccountID)] = true
		}
	}
	if err := s.again(ctx, func() error { return s.chat.SyncManagers(ctx, conv, mods) }); err != nil {
		s.log.Warn("link: the managers of a channel could not be made those on the hub's list", "conversation", conv, "err", err)
	}
}
