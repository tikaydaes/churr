package link

import (
	"context"
	"sort"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is ad-hoc channels and sealed chats across servers (section 16):
// conversations people start themselves, with no owner's approval, between
// accounts that share a shared channel.
//
// The rule is checked by every server involved. The server of whoever
// starts or adds checks it before it sends anything (CanReach, Invite, and
// perm before them); the server of each account named checks it again on
// the offer or the member.invited (checkPersonalOffer, invitedHere); and
// every server checks that a member.joined in an ad-hoc stream follows an
// invitation (invited). When the rule stops holding for a member, the
// conversation is read-only for them (ReadOnly, personalWritable), and a
// receiver rejects what it can see should not have been sent
// (personalAllowed).

// personal reports whether a stream is one of these.
func personal(st store.Stream) bool {
	return st.Kind == store.StreamAdhoc || st.Kind == store.StreamSealed
}

// sharesOn reports whether two accounts are both members of an active
// stream of kind channel that runs over one given peer.
func (s *Service) sharesOn(ctx context.Context, a store.Areas, x, y model.AccountID, peer model.PeerID) bool {
	streams, err := a.Streams().SharedWith(ctx, x, y)
	if err != nil {
		return false
	}
	for _, st := range streams {
		if st.PeerID == peer {
			return true
		}
	}
	return false
}

// peerFor finds the one peer a conversation between a local account and
// some remote ones can run over: a peer on which the local account shares a
// shared channel with each of them.
func (s *Service) peerFor(ctx context.Context, by model.AccountID, remote []model.AccountID) (model.PeerID, error) {
	var common map[model.PeerID]bool
	for _, r := range remote {
		streams, err := s.store.Streams().SharedWith(ctx, by, r)
		if err != nil {
			return "", err
		}
		here := map[model.PeerID]bool{}
		for _, st := range streams {
			if common == nil || common[st.PeerID] {
				here[st.PeerID] = true
			}
		}
		common = here
	}
	if len(common) == 0 {
		return "", model.ErrForbidden.With("You can only start this with people you share a shared channel with, all over the same link.")
	}
	ids := make([]model.PeerID, 0, len(common))
	for id := range common {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if s.connOf(id) != nil {
			return id, nil
		}
	}
	return "", model.ErrUnavailable.With("The other server cannot be reached right now.")
}

// CanReach implements chat.Outbound.
func (s *Service) CanReach(ctx context.Context, by model.AccountID, remote []model.AccountID) error {
	_, err := s.peerFor(ctx, by, remote)
	return err
}

// memberProfiles returns the profile of every member of a conversation, as
// an offer carries them, and the remote accounts among them by home server.
func (s *Service) memberProfiles(ctx context.Context, conv model.ConversationID) ([]wireProfile, map[model.ServerID][]model.AccountID, error) {
	ids, err := s.store.Members().IDs(ctx, conv)
	if err != nil {
		return nil, nil, err
	}
	accounts, err := s.store.Accounts().GetMany(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	profiles := make([]wireProfile, 0, len(accounts))
	remote := map[model.ServerID][]model.AccountID{}
	for _, acc := range accounts {
		p, _ := s.profileFor(ctx, acc)
		p.AvatarFileID = "" // an offer carries no files
		profiles = append(profiles, p)
		if !acc.Local() {
			remote[acc.ServerID] = append(remote[acc.ServerID], acc.ID)
		}
	}
	return profiles, remote, nil
}

// StartAdhoc implements chat.Outbound: steps 1 and 2 of "Starting an ad-hoc
// channel". This server becomes the stream's owner and offers it to the
// home server of each remote member, naming who started it and which of
// that server's accounts are in it.
func (s *Service) StartAdhoc(ctx context.Context, by model.Account, conv model.Conversation) error {
	profiles, remote, err := s.memberProfiles(ctx, conv.ID)
	if err != nil {
		return err
	}
	var all []model.AccountID
	for _, ids := range remote {
		all = append(all, ids...)
	}
	st, servers, err := s.openPersonal(ctx, by.ID, conv, store.StreamAdhoc, all)
	if err != nil {
		return err
	}
	me, _ := s.profileOf(ctx, s.store, by)
	me.AvatarFileID = ""
	for _, sv := range servers {
		s.sendOffer(ctx, st, sv, map[string]any{"by_account": me, "account_ids": remote[sv.ID], "members": profiles})
	}
	return nil
}

// openPersonal opens the stream behind a conversation a member started with
// accounts of other servers, over the one peer they all share a channel on.
// It returns the stream and the servers to offer it to.
func (s *Service) openPersonal(ctx context.Context, by model.AccountID, conv model.Conversation, kind string, remote []model.AccountID) (store.Stream, []store.Server, error) {
	peerID, err := s.peerFor(ctx, by, remote)
	if err != nil {
		return store.Stream{}, nil, err
	}
	peer, err := s.store.Peers().Get(ctx, peerID)
	if err != nil {
		return store.Stream{}, nil, err
	}
	accounts, err := s.store.Accounts().GetMany(ctx, remote)
	if err != nil {
		return store.Stream{}, nil, err
	}
	var servers []store.Server
	seen := map[model.ServerID]bool{}
	for _, acc := range accounts {
		if acc.Local() || seen[acc.ServerID] {
			continue
		}
		seen[acc.ServerID] = true
		sv, err := s.store.Peers().Server(ctx, acc.ServerID)
		if err != nil || sv.Blocked() || !s.reaches(ctx, peer, sv.ID) {
			return store.Stream{}, nil, model.ErrForbidden.With("That person's server cannot be reached over the link you share a channel on.")
		}
		servers = append(servers, sv)
	}
	sequencer := store.SequencerSelf
	if s.connOf(peerID) != nil && s.connOf(peerID).hub {
		sequencer = store.SequencerHub
	}
	st, err := s.openStream(ctx, peer, conv, kind, sequencer, 0, s.maxBlobDefault(ctx), servers)
	return st, servers, err
}

// Invite implements chat.Outbound: adding someone later.
func (s *Service) Invite(ctx context.Context, tx store.Tx, rec *chat.Recorder, conv model.Conversation, by, target model.Account) error {
	st, err := s.streamOf(ctx, tx, conv)
	if err != nil || st.Kind != store.StreamAdhoc || st.State != store.StreamActive {
		return model.ErrForbidden.With("That person is on another server, and this conversation is not shared with it.")
	}
	server := s.id.Name
	if !target.Local() {
		server = target.ServerName
		if !s.sharesOn(ctx, tx, by.ID, target.ID, st.PeerID) {
			return model.ErrForbidden.With("You can only add people you share a shared channel with, over the link this conversation runs on.")
		}
	}
	n, err := tx.Members().Count(ctx, conv.ID)
	if err != nil {
		return err
	}
	if n >= model.MaxAdhocMembers {
		return model.ErrTooLarge.With("An ad-hoc channel holds at most 50 people.")
	}
	fields := map[string]any{"account_id": target.ID, "by_account_id": by.ID, "server": server}
	s.later(rec, func(ctx context.Context) {
		if err := s.enqueue(ctx, st, evMemberInvited, fields); err != nil {
			s.log.Warn("link: an invitation could not be submitted", "stream", st.ID, "err", err)
		}
	})
	return nil
}

// noteInvited remembers that a member.invited named an account, so its
// member.joined is accepted.
func (s *Service) noteInvited(stream model.StreamID, account model.AccountID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.invitedTo == nil {
		s.invitedTo = map[model.StreamID]map[model.AccountID]bool{}
	}
	if s.invitedTo[stream] == nil {
		s.invitedTo[stream] = map[model.AccountID]bool{}
	}
	s.invitedTo[stream][account] = true
}

// invited reports whether an account may join an ad-hoc stream: a
// member.invited named it, or it came with the offer that made the channel
// (and is then a member of this server's copy already, or was noted when
// the offer was taken).
func (a *apply) invited(account model.AccountID) (bool, error) {
	a.s.mu.Lock()
	noted := a.s.invitedTo[a.st.ID][account]
	a.s.mu.Unlock()
	if noted {
		return true, nil
	}
	_, found, err := a.tx.Members().Get(a.ctx, a.conv.ID, account)
	return found, err
}

// invitedHere is what a member.invited asks of this server. The home server
// of the invited account checks the rule for its own account and the adder,
// and joins or does nothing. The owner server, when that home server is not
// in the stream yet, offers it the stream.
func (s *Service) invitedHere(a *apply, account, by model.AccountID, server string) {
	ctx, tx, st, conv := a.ctx, a.tx, a.st, a.conv
	target, err := tx.Accounts().Get(ctx, account)
	if err == nil && target.Local() {
		if !target.Active() {
			return
		}
		if !isLocal(ctx, tx, by) && !s.sharesOn(ctx, tx, account, by, st.PeerID) {
			return // the rule is between people on different servers
		}
		if s.blockedHereTx(ctx, tx, account, by) {
			return
		}
		s.later(a.rec, func(ctx context.Context) {
			if err := s.again(ctx, func() error { return s.chat.JoinInvited(ctx, conv.ID, account, by) }); err != nil {
				s.log.Warn("link: an invited member could not join", "stream", st.ID, "err", err)
			}
		})
		return
	}
	if !st.Owned() {
		return
	}
	if err == nil {
		server = target.ServerName
	}
	home, err := tx.Peers().ServerByName(ctx, server)
	if err != nil || home.Blocked() {
		return
	}
	servers, err := tx.Streams().Servers(ctx, st.ID)
	if err != nil {
		return
	}
	for _, ss := range servers {
		if ss.ServerID == home.ID && ss.State != store.StreamServerRemoved {
			return // in the stream, or offered it already
		}
	}
	peer, err := tx.Peers().Get(ctx, st.PeerID)
	if err != nil || !s.reaches(ctx, peer, home.ID) {
		return
	}
	s.later(a.rec, func(ctx context.Context) {
		adder, err := s.store.Accounts().Get(ctx, by)
		if err != nil {
			return
		}
		profiles, _, err := s.memberProfiles(ctx, conv.ID)
		if err != nil {
			return
		}
		if err := s.store.Streams().PutServer(ctx, store.StreamServer{StreamID: st.ID, ServerID: home.ID, State: store.StreamServerInvited, JoinedAt: s.now()}); err != nil {
			return
		}
		me, _ := s.profileFor(ctx, adder)
		me.AvatarFileID = ""
		s.sendOffer(ctx, st, home, map[string]any{"by_account": me, "account_ids": []model.AccountID{account}, "members": profiles})
	})
}

// checkPersonalOffer is the receiving server's checks on an ad-hoc or
// sealed offer (the table of section 16). It returns the reason to decline
// with, or "".
func (s *Service) checkPersonalOffer(ctx context.Context, st store.Stream, from store.Server, o offerBody) string {
	if o.ByAccount == nil || len(o.AccountIDs) == 0 || len(o.Members) == 0 || len(o.Members) > model.MaxAdhocMembers {
		return "not_allowed"
	}
	peer, err := s.store.Peers().Get(ctx, st.PeerID)
	if err != nil {
		return "not_allowed"
	}
	// The adder: an account of the server it says, which this server has
	// met in a shared channel.
	by, err := s.store.Accounts().Get(ctx, o.ByAccount.ID)
	if err != nil || by.Local() || by.Removed() || by.ServerName != o.ByAccount.Server {
		return "not_allowed"
	}
	// Every server in it is on this one peer.
	inMembers := map[model.AccountID]bool{}
	for _, m := range o.Members {
		inMembers[m.ID] = true
		if m.Server == s.id.Name {
			continue
		}
		sv, err := s.store.Peers().ServerByName(ctx, m.Server)
		if err != nil || !s.reaches(ctx, peer, sv.ID) {
			return "not_allowed"
		}
	}
	if !inMembers[by.ID] && st.Kind == store.StreamSealed {
		return "not_allowed"
	}
	for _, id := range o.AccountIDs {
		acc, err := s.store.Accounts().Get(ctx, id)
		if err != nil || !acc.Local() || acc.Removed() {
			return "not_allowed"
		}
		if !s.sharesOn(ctx, s.store, id, by.ID, st.PeerID) {
			return "not_allowed"
		}
		// A block is not checked here: the offer is taken as any other and
		// the blocking account is left out of it (acceptPersonalOffer,
		// ruling R77).
		if st.Kind == store.StreamAdhoc && !acc.Active() {
			return "not_allowed"
		}
	}
	if st.Kind == store.StreamSealed {
		return s.checkSealedOffer(ctx, st, from, by, o)
	}
	return ""
}

// acceptPersonalOffer takes an offer that passed its checks: the local
// conversation is made, and the owner is told, which then adds this server
// to the stream.
func (s *Service) acceptPersonalOffer(ctx context.Context, st store.Stream, m directMessage, o offerBody) {
	refuse := func(why string, err error) {
		s.log.Warn("link: an offer that passed its checks could not be taken", "stream", st.ID, "why", why, "err", err)
		s.store.Streams().Delete(ctx, st.ID)
		s.sendDirect(ctx, m.peer, m.from, kDecline, m.MsgID, map[string]any{"stream_id": st.ID, "reason": "not_allowed"})
	}
	// The members that are there already: accounts of servers in the
	// stream. Accounts of a server that has only been offered it are not
	// members until that server says they have joined; they are noted as
	// invited, as are this server's own.
	active := map[string]bool{}
	for _, name := range o.Servers {
		active[name] = true
	}
	var members, key []model.AccountID
	mine := map[model.AccountID]bool{}
	// Accounts here that have blocked the starter are left out and told
	// nothing; to the starter's server the offer is taken as any other
	// (ruling R77). A block is known only on the blocker's own server, so
	// this is where it is applied.
	deaf := map[model.AccountID]bool{}
	for _, id := range o.AccountIDs {
		mine[id] = true
		deaf[id] = s.blockedHere(ctx, id, o.ByAccount.ID)
	}
	err := s.chat.RemoteTx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		for _, p := range o.Members {
			key = append(key, p.ID)
			s.noteInvited(st.ID, p.ID)
			if p.Server == s.id.Name {
				if mine[p.ID] && !deaf[p.ID] {
					members = append(members, p.ID)
				}
				continue
			}
			if !active[p.Server] {
				continue
			}
			sv, err := tx.Peers().ServerByName(ctx, p.Server)
			if err != nil {
				return err
			}
			a := &apply{s: s, ctx: ctx, tx: tx, rec: rec, st: st, origin: store.StreamServer{StreamID: st.ID, ServerID: sv.ID, Name: sv.Name},
				in: inner{Type: kOffer, At: s.now()}}
			if rej, err := a.profile(p, nil, false); err != nil {
				return err
			} else if rej != nil {
				return model.ErrConflict.With("A member's profile was not accepted: " + rej.reason + ".")
			}
			members = append(members, p.ID)
		}
		return nil
	})
	if err != nil {
		refuse("profiles", err)
		return
	}
	var conv model.Conversation
	if st.Kind == store.StreamSealed {
		conv, err = s.acceptSealed(ctx, st, o, deaf[o.AccountIDs[0]])
	} else {
		conv, err = s.chat.CreateAdhocShared(ctx, o.ByAccount.ID, members, key, st.ID)
	}
	if err != nil {
		refuse("conversation", err)
		return
	}
	if _, err := s.store.Streams().Update(ctx, st.ID, func(x *store.Stream) error {
		x.ConversationID = conv.ID
		return nil
	}); err != nil {
		refuse("stream", err)
		return
	}
	s.sendDirect(ctx, m.peer, m.from, kAccept, m.MsgID, map[string]any{"stream_id": st.ID})
}

// personalDeclined is the owner's side of a declined ad-hoc or sealed
// offer: the accounts of the server that declined are not in the
// conversation after all, and a stream with nobody else in it is closed.
func (s *Service) personalDeclined(ctx context.Context, st store.Stream) {
	if st.ConversationID == "" {
		return
	}
	conv, err := s.store.Conversations().Get(ctx, st.ConversationID)
	if err != nil {
		return
	}
	servers, err := s.store.Streams().Servers(ctx, st.ID)
	if err != nil {
		return
	}
	in := map[model.ServerID]bool{}
	others := 0
	for _, ss := range servers {
		if ss.State != store.StreamServerRemoved {
			in[ss.ServerID] = true
			if !s.isSelf(ss.ServerID) {
				others++
			}
		}
	}
	if st.Kind == store.StreamAdhoc {
		ids, err := s.store.Members().IDs(ctx, conv.ID)
		if err != nil {
			return
		}
		accounts, err := s.store.Accounts().GetMany(ctx, ids)
		if err != nil {
			return
		}
		var gone []model.AccountID
		for _, acc := range accounts {
			if !acc.Local() && !in[acc.ServerID] {
				gone = append(gone, acc.ID)
			}
		}
		if len(gone) > 0 {
			if err := s.chat.DropFromShared(ctx, conv, gone); err != nil {
				s.log.Warn("link: the accounts of a server that declined could not be taken out", "stream", st.ID, "err", err)
			}
		}
	}
	if others != 0 {
		return
	}
	if st.Kind != store.StreamSealed {
		s.closeOwned(ctx, st)
		return
	}
	// A sealed chat the other person's server would not have (it was a
	// bot, or the two have blocked each other there) does not exist: left
	// standing it would be the pair's one sealed chat for ever.
	if ids, err := s.store.Members().IDs(ctx, conv.ID); err == nil {
		if err := s.chat.DropFromShared(ctx, conv, ids); err != nil {
			s.log.Warn("link: a declined sealed chat could not be emptied", "stream", st.ID, "err", err)
		}
	}
	s.closeOwned(ctx, st)
	if _, err := s.store.Conversations().Delete(ctx, conv.ID); err != nil && model.CodeOf(err) != model.CodeNotFound {
		s.log.Warn("link: a declined sealed chat could not be removed", "stream", st.ID, "err", err)
	}
	s.store.Streams().Delete(ctx, st.ID)
}

// personalAllowed is the receiving server's test of section 16 on an event
// from a remote account in a cross-server ad-hoc channel or sealed chat:
// when the only other members are this server's own, and none of them
// shares a shared channel with the author over this link, the event should
// not have been sent.
func (s *Service) personalAllowed(a *apply, author model.AccountID) *rejection {
	if !personal(a.st) || s.isSelf(a.origin.ServerID) {
		return nil
	}
	ids, err := a.tx.Members().IDs(a.ctx, a.conv.ID)
	if err != nil {
		return nil
	}
	accounts, err := a.tx.Accounts().GetMany(a.ctx, ids)
	if err != nil {
		return nil
	}
	local := 0
	for _, acc := range accounts {
		if acc.ID == author {
			continue
		}
		if !acc.Local() {
			return nil // a member of a third server: this server cannot see both sides
		}
		local++
		if s.sharesOn(a.ctx, a.tx, acc.ID, author, a.st.PeerID) {
			return nil
		}
	}
	if local == 0 {
		return nil
	}
	return reject("no_shared_channel")
}

// readOnlyFor is the per-member rule of section 16, judged by the member's
// home server: the conversation is read-only for one of this server's
// members who shares no shared channel, over the conversation's link, with
// any member of it from another server.
func (s *Service) readOnlyFor(ctx context.Context, a store.Areas, st store.Stream, conv model.Conversation, account model.AccountID) bool {
	if !personal(st) {
		return false
	}
	if st.State != store.StreamActive && st.State != store.StreamOffered {
		return true
	}
	ids, err := a.Members().IDs(ctx, conv.ID)
	if err != nil {
		return false
	}
	accounts, err := a.Accounts().GetMany(ctx, ids)
	if err != nil {
		return false
	}
	remote := 0
	for _, acc := range accounts {
		if acc.Local() {
			continue
		}
		remote++
		if s.sharesOn(ctx, a, account, acc.ID, st.PeerID) {
			return false
		}
	}
	// With nobody from another server in it yet (their server has not
	// answered the offer), there is nobody to be cut off from.
	return remote > 0
}

// ReadOnly implements chat.Outbound.
func (s *Service) ReadOnly(ctx context.Context, conv model.Conversation, account model.AccountID) bool {
	st, err := s.streamOf(ctx, s.store, conv)
	return err == nil && s.readOnlyFor(ctx, s.store, st, conv, account)
}

// actorOf is the local account whose doing an event in a conversation is,
// for the events section 16 names: posting, reacting, ticking, answering.
func actorOf(ctx context.Context, e model.Event) model.AccountID {
	switch e.Type {
	case model.EventMessageCreated, model.EventMessageEdited:
		if m, ok := e.Payload["message"].(model.Message); ok {
			return m.AuthorID
		}
	case model.EventReactionAdded, model.EventReactionRemoved:
		id, _ := e.Payload["account_id"].(model.AccountID)
		return id
	case model.EventTaskChanged:
		if task, ok := e.Payload["task"].(model.Task); ok && task.CheckedByID != "" {
			return task.CheckedByID
		}
		return chat.ActorOf(ctx)
	case model.EventFormAnswered, model.EventFormAnswerWithdrawn, model.EventFormOptionAdded:
		return chat.ActorOf(ctx)
	}
	return ""
}

// personalWritable refuses a local member's change in a cross-server ad-hoc
// channel or sealed chat that is read-only for them, and a name for one.
func (s *Service) personalWritable(ctx context.Context, a store.Areas, st store.Stream, conv model.Conversation, events []model.Event) error {
	if !personal(st) {
		return nil
	}
	checked := map[model.AccountID]bool{}
	for _, e := range events {
		if e.Type == model.EventConversationUpdated {
			if c, ok := e.Payload["conversation"].(model.Conversation); ok && c.Name != "" {
				return model.ErrForbidden.With("A conversation with people on other servers cannot be given a name.")
			}
			continue
		}
		actor := actorOf(ctx, e)
		if actor == "" || checked[actor] || !isLocal(ctx, a, actor) {
			continue
		}
		checked[actor] = true
		if s.readOnlyFor(ctx, a, st, conv, actor) {
			return model.ErrForbidden.With("This conversation is read-only: you no longer share a shared channel with anyone in it on another server.")
		}
	}
	return nil
}

// blockedHere reports whether one of this server's accounts has blocked an
// account, while blocking is on for the server (ruling R77).
func (s *Service) blockedHere(ctx context.Context, account, by model.AccountID) bool {
	return s.blockedHereTx(ctx, s.store, account, by)
}

func (s *Service) blockedHereTx(ctx context.Context, a store.Areas, account, by model.AccountID) bool {
	if on, _ := store.SettingBool(ctx, a, store.SettingBlockingEnabled, true); !on {
		return false
	}
	blocked, err := a.Blocks().Has(ctx, account, by)
	return err != nil || blocked
}
