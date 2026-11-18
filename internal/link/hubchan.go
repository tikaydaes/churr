package link

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is a server's side of channels a hub owns (section 23a of
// docs/LINK-PROTOCOL.md, rulings R56 and R59). On a hub, a shared channel
// belongs to the hub: the hub keeps its record (name, description, listing
// mode, member servers, managers) and is the only party whose signature
// changes it. A server holds the newest record it has been sent, takes the
// server list and the managers from it and from nowhere
// else, and asks the hub for changes in the name of a manager who is one
// of its own accounts. The home hub makes every key epoch's stream key and
// begins the epoch itself: a server takes such a channel's key from the
// home hub and from nobody else (ruling R109).
//
// Ad-hoc channels and sealed chats that cross a hub are not channels in this
// sense (R59). They keep the owner-server frames of section 6 and the hub
// carries them blind.

// EventChannelChanged tells a channel's local members that the record the
// hub keeps of it has changed.
const EventChannelChanged = "stream.channel_changed"

// actionWait is how long a manager's action waits for the hub to answer.
const actionWait = 5 * time.Second

// hubOwned reports whether a stream is a channel a hub owns.
func hubOwned(st store.Stream) bool {
	return st.Kind == store.StreamChannel && st.Sequencer == store.SequencerHub
}

// hubServer returns the row that stands for a hub in `servers`, so that a
// hub-owned stream has an owner that is no server in it: every path that
// asks "is this the owner server" then answers no, for every server.
func (s *Service) hubServer(ctx context.Context, a store.Areas, peer store.Peer) (store.Server, error) {
	sv, _, err := a.Peers().PinServer(ctx, store.Server{Name: peer.HubName, SignKey: peer.HubSignKey})
	return sv, err
}

// acceptedHubKeys is the set of hub keys whose signature this server takes
// on a record arriving over a peer. Today it is the one key pinned for the
// hub at the other end. It is a set so that a cluster of hubs can be let in
// later without changing how a record is checked (trunks, ruling R58).
func (s *Service) acceptedHubKeys(peer store.Peer) [][]byte {
	keys := [][]byte{peer.HubSignKey}
	// And the keys of the other hubs of its cluster, from the roster it
	// sent and this server checked (cluster.go).
	s.mu.Lock()
	for _, m := range s.cluster.clusters[peer.ID].Members {
		keys = append(keys, m.SignKey)
	}
	s.mu.Unlock()
	return keys
}

// verifyHubBlock checks a hub-signed block: the key it names must be one
// this server accepts for the peer it came over, and the signature must
// verify under that key. named reads the key out of the parsed block, which
// is trusted for nothing but that lookup until the signature has verified.
func (s *Service) verifyHubBlock(peer store.Peer, name string, signed, sig []byte, named func() []byte, out any) error {
	if peer.Kind != wire.KindHub {
		return wire.Errf(wire.EBadFrame, "%s comes only from a hub.", name)
	}
	f := wire.Frame{Frame: name, Signed: signed, Sig: sig}
	if err := wire.PeekBlock(f, out); err != nil {
		return err
	}
	claimed := named()
	for _, k := range s.acceptedHubKeys(peer) {
		if len(k) > 0 && bytes.Equal(k, claimed) {
			return wire.VerifyBlock(f, k, out)
		}
	}
	return wire.Errf(wire.EBadSignature, "%s is not signed by a hub this server accepts on that link.", name)
}

// channelOf returns the record held for a hub-owned channel.
func (s *Service) channelOf(ctx context.Context, a store.Areas, id model.StreamID) (wire.ChannelRecord, error) {
	var rec wire.ChannelRecord
	row, err := a.Streams().Channel(ctx, id)
	if err != nil {
		return rec, err
	}
	err = wire.PeekBytes(wire.FChannel, row.Record, &rec)
	return rec, err
}

// manages reports whether an account of a named instance is on the
// hub-signed manager list of a stream.
func (s *Service) manages(ctx context.Context, a store.Areas, st store.Stream, account model.AccountID, server string) bool {
	if !hubOwned(st) || account == "" {
		return false
	}
	rec, err := s.channelOf(ctx, a, st.ID)
	return err == nil && rec.Manages(string(account), server)
}

// keeps reports whether an account of a named instance is on the
// hub-signed manager list of a stream or on its moderator list: who may
// remove messages and people on every instance.
func (s *Service) keeps(ctx context.Context, a store.Areas, st store.Stream, account model.AccountID, server string) bool {
	if !hubOwned(st) || account == "" {
		return false
	}
	rec, err := s.channelOf(ctx, a, st.ID)
	return err == nil && rec.Keeps(string(account), server)
}

// storeChannel keeps a record that has been verified: the signed bytes, and
// which of its managers and moderators are accounts of this server
// (internal/perm reads those).
func (s *Service) storeChannel(ctx context.Context, a store.Areas, signed, sig []byte, rec wire.ChannelRecord) error {
	mine := func(list []wire.ChannelAccount) []model.AccountID {
		var out []model.AccountID
		for _, m := range list {
			if m.Server == s.id.Name {
				out = append(out, model.AccountID(m.AccountID))
			}
		}
		return out
	}
	return a.Streams().PutChannel(ctx, store.ChannelRecord{StreamID: model.StreamID(rec.StreamID), Version: rec.Version, HomeHub: rec.HomeHub,
		Record: signed, Sig: sig, Managers: mine(rec.Managers), Moderators: mine(rec.Moderators), Private: rec.Private})
}

// HubChannel is a hub-owned channel as the API shows it (schema HubChannel).
type HubChannel struct {
	StreamID    model.StreamID        `json:"stream_id"`
	Hub         string                `json:"hub"`
	Version     int64                 `json:"version"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Listing     string                `json:"listing"`
	Servers     []string              `json:"servers"`
	Managers    []wire.ChannelAccount `json:"managers"`
	Moderators  []wire.ChannelAccount `json:"moderators"`
	Invited     []string              `json:"invited"`
	Requests    []string              `json:"requests"`
	Closed      bool                  `json:"closed"`
	// Private: private for good, never listed, and its people admitted by
	// its moderators (ruling R94).
	Private   bool       `json:"private"`
	UpdatedAt model.Time `json:"updated_at"`
}

func channelView(rec wire.ChannelRecord) HubChannel {
	v := HubChannel{StreamID: model.StreamID(rec.StreamID), Hub: rec.Hub, Version: rec.Version, Name: rec.Name,
		Description: rec.Description, Listing: rec.Listing, Servers: rec.Servers, Managers: rec.Managers, Moderators: rec.Moderators, Invited: rec.Invited,
		Requests: rec.Requests, Closed: rec.Closed, Private: rec.Private, UpdatedAt: rec.At}
	for _, l := range []*[]string{&v.Servers, &v.Invited, &v.Requests} {
		if *l == nil {
			*l = []string{}
		}
	}
	if v.Managers == nil {
		v.Managers = []wire.ChannelAccount{}
	}
	if v.Moderators == nil {
		v.Moderators = []wire.ChannelAccount{}
	}
	return v
}

// isModerator reports whether an account may act as a manager of a channel
// from this server: an active account of this server that the hub-signed
// list names as one of this server's.
func (s *Service) isManager(by model.Account, rec wire.ChannelRecord) bool {
	return by.Active() && by.Local() && by.Role != model.RoleBot && rec.Manages(string(by.ID), s.id.Name)
}

// isKeeper is isManager for the manager list or the moderator list: who
// may admit, decline and remove people and remove messages from this
// instance.
func (s *Service) isKeeper(by model.Account, rec wire.ChannelRecord) bool {
	return by.Active() && by.Local() && by.Role != model.RoleBot && rec.Keeps(string(by.ID), s.id.Name)
}

// Channel returns a hub-owned channel's record for one of its local members,
// one of its managers or the server's owner. The lists of servers invited
// and asking are for managers and the owner only.
func (s *Service) Channel(ctx context.Context, by model.Account, id model.StreamID) (HubChannel, error) {
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil || !hubOwned(st) {
		return HubChannel{}, model.ErrNotFound
	}
	rec, err := s.channelOf(ctx, s.store, id)
	if err != nil {
		return HubChannel{}, model.ErrNotFound
	}
	v := channelView(rec)
	if s.Allowed(ctx, by) != nil && !s.isKeeper(by, rec) {
		if st.ConversationID == "" {
			return HubChannel{}, model.ErrNotFound
		}
		if _, err := s.chat.Conversation(ctx, by, st.ConversationID); err != nil {
			return HubChannel{}, model.ErrNotFound
		}
		v.Invited, v.Requests = []string{}, []string{}
	}
	return v, nil
}

// awaitFrame registers interest in the answer to a frame sent with an id on
// a peer's connection: an error frame, or a frame that carries that id as
// its `re`.
func (s *Service) awaitFrame(peer model.PeerID, id int64) (chan wire.Frame, func()) {
	key := string(peer) + "|" + strconv.FormatInt(id, 10)
	ch := make(chan wire.Frame, 1)
	s.mu.Lock()
	s.frameWaits[key] = ch
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.frameWaits, key)
		s.mu.Unlock()
	}
}

// answerFrame hands a frame to whoever waits for the answer to frame `re`.
func (s *Service) answerFrame(peer model.PeerID, f wire.Frame) bool {
	if f.Re == 0 {
		return false
	}
	s.mu.Lock()
	ch := s.frameWaits[string(peer)+"|"+strconv.FormatInt(f.Re, 10)]
	s.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- f:
	default:
	}
	return true
}

// frameError turns a refusal from the hub into the API's error.
func frameError(f wire.Frame) error {
	switch f.Code {
	case wire.EForbidden, wire.ENotOwner, wire.EBadSignature, wire.ENotInStream:
		return model.ErrForbidden.With(f.Message)
	case wire.ENotFound, wire.EUnknownStream, wire.EUnknownServer:
		return model.ErrNotFound.With(f.Message)
	case wire.ERateLimited, wire.EConflict, wire.ETooLarge:
		return model.ErrConflict.With(f.Message)
	case wire.EBadFrame:
		return model.Invalid("action", f.Message)
	}
	return model.ErrUnavailable.With(f.Message)
}

// ModerateInput is one manager's action on a hub-owned channel.
type ModerateInput struct {
	Action      string
	Server      string // a server's name, for invite, approve, decline and remove_server
	AccountID   model.AccountID
	Name        *string
	Description *string
	Listing     string
}

// Moderate asks the hub, in the name of a manager who is an account of
// this server, to change a channel. It returns the record as the hub signed
// it after the change. The hub checks again that the account is a manager;
// this server checks first so that it never signs for an account that is
// not.
func (s *Service) Moderate(ctx context.Context, by model.Account, id model.StreamID, in ModerateInput) (HubChannel, error) {
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil || !hubOwned(st) {
		return HubChannel{}, model.ErrNotFound
	}
	rec, err := s.channelOf(ctx, s.store, id)
	if err != nil {
		return HubChannel{}, model.ErrNotFound
	}
	if !s.isManager(by, rec) {
		return HubChannel{}, model.ErrForbidden.With("Only a manager of the channel can do that.")
	}
	if st.State != store.StreamActive {
		return HubChannel{}, model.ErrNotFound
	}
	a := wire.ChannelAction{StreamID: string(id), ActionID: newID(), Action: in.Action, Server: in.Server, Name: in.Name,
		Description: in.Description, Listing: in.Listing, By: wire.ChannelAccount{AccountID: string(by.ID), Server: s.id.Name, Username: by.Username}}
	switch in.Action {
	case wire.ActInvite, wire.ActApprove, wire.ActDecline, wire.ActRemoveServer:
		if in.Server == "" || (in.Server == s.id.Name && in.Action != wire.ActRemoveServer) {
			return HubChannel{}, model.Invalid("server", "Name the server.")
		}
		if in.Action == wire.ActInvite {
			if sv, err := s.store.Peers().ServerByName(ctx, in.Server); err == nil && sv.Blocked() {
				return HubChannel{}, model.ErrConflict.With(in.Server + " is blocked on this server.")
			}
		}
	case wire.ActAddManager, wire.ActRemoveManager, wire.ActAddModerator, wire.ActRemoveModerator:
		acc, err := s.store.Accounts().Get(ctx, in.AccountID)
		if err != nil {
			return HubChannel{}, model.Invalid("account_id", "There is no such account.")
		}
		home := s.id.Name
		if !acc.Local() {
			sv, err := s.store.Peers().Server(ctx, acc.ServerID)
			if err != nil {
				return HubChannel{}, model.Invalid("account_id", "There is no such account.")
			}
			home = sv.Name
		} else if (in.Action == wire.ActAddManager || in.Action == wire.ActAddModerator) && (!acc.Active() || acc.Role == model.RoleBot) {
			return HubChannel{}, model.Invalid("account_id", "A manager or moderator is a person's active account.")
		}
		a.Account = &wire.ChannelAccount{AccountID: string(acc.ID), Server: home, Username: acc.Username}
	case wire.ActDescribe:
		if in.Name == nil && in.Description == nil {
			return HubChannel{}, model.Invalid("name", "Give a name, a description or both.")
		}
	case wire.ActSetListing:
		if !wire.ValidListing(in.Listing) {
			return HubChannel{}, model.Invalid("listing", "The listing mode is unlisted, request or open.")
		}
		if rec.Private && in.Listing != wire.ListingUnlisted {
			return HubChannel{}, model.ErrForbidden.With("A private channel is never listed, not even by a manager.")
		}
	case wire.ActClose:
	default:
		return HubChannel{}, model.Invalid("action", "There is no such action.")
	}
	if err := s.sendModAction(ctx, st, a, rec.Version); err != nil {
		return HubChannel{}, err
	}
	s.audit(ctx, s.store, by.ID, "link.channel_moderated", "stream", string(id), map[string]any{"action": in.Action, "server": in.Server})
	if rec, err = s.channelOf(ctx, s.store, id); err != nil {
		return HubChannel{}, err
	}
	return channelView(rec), nil
}

// sendModAction signs an action as this server, sends it to the hub and
// waits until the hub has refused it or signed a newer record.
func (s *Service) sendModAction(ctx context.Context, st store.Stream, a wire.ChannelAction, after int64) error {
	a.At = s.now()
	f, err := wire.SignBlock(wire.FChannelAction, a, s.keys)
	if err != nil {
		return err
	}
	return s.sendAndAwaitRecord(ctx, st.PeerID, st.ID, f, after)
}

// sendAndAwaitRecord sends the hub a frame that asks for a change to a
// channel and waits until the hub has refused it or a record newer than
// `after` is held here.
func (s *Service) sendAndAwaitRecord(ctx context.Context, peer model.PeerID, id model.StreamID, f wire.Frame, after int64) error {
	pc := s.connOf(peer)
	if pc == nil {
		return model.ErrUnavailable.With("The hub is not connected.")
	}
	f.ID = pc.conn.NextID()
	refused, done := s.awaitFrame(peer, f.ID)
	defer done()
	if err := pc.conn.Send(f); err != nil {
		return model.ErrUnavailable.With("The hub is not connected.")
	}
	deadline := time.NewTimer(actionWait)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case e := <-refused:
			return frameError(e)
		case <-poll.C:
			if row, err := s.store.Streams().Channel(ctx, id); err == nil && row.Version > after {
				return nil
			}
		case <-deadline.C:
			return model.ErrUnavailable.With("The hub did not answer.")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// shareToHub shares a channel to a hub: the hub is told (channel_create)
// with the sharer as its first manager, and owns the channel from the
// moment it answers; then each server named is invited in the sharer's
// name. Nothing is shared if the hub does not answer.
func (s *Service) shareToHub(ctx context.Context, by model.Account, peer store.Peer, conv model.Conversation, in ShareInput, invited []store.Server) (store.Stream, error) {
	hub, err := s.hubServer(ctx, s.store, peer)
	if err != nil {
		return store.Stream{}, err
	}
	now := s.now()
	// No key epoch has begun: the hub makes the key of the first and sends it
	// ahead of the record that answers channel_create.
	st := store.Stream{ID: model.NewID[model.StreamID](), PeerID: peer.ID, ConversationID: conv.ID, OwnerServerID: hub.ID, Sequencer: store.SequencerHub,
		Kind: store.StreamChannel, State: store.StreamActive, MaxBlobBytes: in.MaxBlobBytes, HistoryDays: in.HistoryDays,
		HomeHub: peer.HubName, CreatedAt: now}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Streams().Create(ctx, &st); err != nil {
			return err
		}
		return tx.Streams().PutServer(ctx, store.StreamServer{StreamID: st.ID, ServerID: s.self.ID, State: store.StreamServerActive, JoinedAt: now})
	})
	if err != nil {
		return st, err
	}
	fail := func(err error) (store.Stream, error) {
		// Nothing was shared: the channel is local again.
		s.chat.AttachStream(ctx, conv.ID, "", false)
		s.store.Streams().Delete(ctx, st.ID)
		return st, err
	}
	me := wire.ChannelAccount{AccountID: string(by.ID), Server: s.id.Name, Username: by.Username}
	// Its managers come with it and stay its managers, now on the hub's
	// list (section 23a).
	managers, err := s.managersOf(ctx, s.store, conv.ID)
	if err != nil {
		return fail(err)
	}
	cc := wire.ChannelCreate{StreamID: string(st.ID), Name: conv.Name, Description: conv.Topic, Listing: in.Listing, Creator: me, At: now,
		Private: conv.Private, Managers: managers, HistoryDays: in.HistoryDays}
	f, err := wire.SignBlock(wire.FChannelCreate, cc, s.keys)
	if err != nil {
		return fail(err)
	}
	if err := s.sendAndAwaitRecord(ctx, peer.ID, st.ID, f, 0); err != nil {
		return fail(err)
	}
	// The conversation is marked only now: from here on its events are
	// stamped and travel.
	if err := s.chat.AttachStream(ctx, conv.ID, st.ID, false); err != nil {
		s.sendModAction(ctx, st, wire.ChannelAction{StreamID: string(st.ID), ActionID: newID(), Action: wire.ActClose, By: me}, 1)
		return fail(err)
	}
	for _, sv := range invited {
		rec, err := s.channelOf(ctx, s.store, st.ID)
		if err != nil {
			return st, err
		}
		a := wire.ChannelAction{StreamID: string(st.ID), ActionID: newID(), Action: wire.ActInvite, Server: sv.Name, By: me}
		if err := s.sendModAction(ctx, st, a, rec.Version); err != nil {
			return st, err
		}
	}
	return s.store.Streams().Get(ctx, st.ID)
}

// onChannel takes a channel record from a hub: the only way a hub-owned
// channel's server list, managers, listing mode, name or description
// change on this server. A record that is not signed by a hub this server
// accepts on that link changes nothing.
func (s *Service) onChannel(ctx context.Context, pc *peerConn, f wire.Frame) error {
	var rec wire.ChannelRecord
	if err := s.verifyHubBlock(pc.peer, wire.FChannel, f.Signed, f.Sig, func() []byte { return rec.HubKey }, &rec); err != nil {
		s.count(func(st *Stats) { st.Dropped["channel_bad_signature"]++ })
		s.audit(ctx, s.store, "", "link.record_refused", "peer", string(pc.peer.ID), map[string]any{"why": "the signature is not an accepted hub's"})
		return err
	}
	if !wire.ValidID(rec.StreamID) || rec.Version < 1 || !wire.ValidListing(rec.Listing) || (rec.Private && rec.Listing != wire.ListingUnlisted) {
		return wire.Errf(wire.EBadFrame, "The channel record is malformed.")
	}
	// The hub that signed is the neighbour, or a hub of its cluster, under
	// the key the roster gives that name and no other.
	if key := s.clusterHubKey(pc.peer, rec.Hub); len(key) == 0 || !bytes.Equal(key, rec.HubKey) {
		s.count(func(st *Stats) { st.Dropped["channel_bad_signature"]++ })
		s.audit(ctx, s.store, "", "link.record_refused", "peer", string(pc.peer.ID), map[string]any{"why": rec.Hub + " is not a hub of this link's cluster under that key"})
		return wire.Errf(wire.EBadSignature, "%s is not a hub this server accepts on that link.", rec.Hub)
	}
	if rec.HomeHub == "" {
		rec.HomeHub = rec.Hub
	}
	id := model.StreamID(rec.StreamID)
	// A key that came ahead of this record from the home it names is taken
	// once the record is (after the lock below is let go), and what waited
	// for a record (an origin it lets in, a moderator it names) is tried
	// again.
	defer func() {
		s.takeEarlyKeys(ctx, id)
		s.answerEarlyAsks(ctx, id)
		rt := s.rt(id)
		s.onJobs(rt, func() { s.retryHeld(s.ctx, rt, true) })
	}()
	// One record at a time per channel: two that arrive together must not
	// both find the older one held.
	rt := s.rt(id)
	rt.record.Lock()
	defer rt.record.Unlock()
	self := s.id.Name
	member, invited := s.inChannel(rec), false
	for _, n := range rec.Invited {
		invited = invited || n == self
	}
	st, err := s.store.Streams().Get(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		switch {
		case rec.Closed || rec.Prepared:
		case s.holds(rec):
			// The hub this holder is the holder of has become the channel's
			// home: it keeps the channel whole from here (holder.go).
			return s.holdChannel(ctx, pc, f, rec)
		case invited:
			// A manager invited this server: its owner sees an offer.
			hub, err := s.hubRow(ctx, s.store, pc.peer, rec.HomeHub)
			if err != nil {
				return err
			}
			now := s.now()
			st = store.Stream{ID: id, PeerID: pc.peer.ID, OwnerServerID: hub.ID, Sequencer: store.SequencerHub, Kind: store.StreamChannel,
				State: store.StreamOffered, OfferedName: offeredName(rec.Name), OfferedAt: now, MaxBlobBytes: s.maxBlobDefault(ctx),
				HomeHub: rec.HomeHub, CreatedAt: now}
			err = s.store.Tx(ctx, func(tx store.Tx) error {
				if err := tx.Streams().Create(ctx, &st); err != nil {
					return err
				}
				return s.storeChannel(ctx, tx, f.Signed, f.Sig, rec)
			})
			if err != nil {
				return err
			}
			s.mu.Lock()
			s.offers[id] = offer{Topic: rec.Description}
			s.mu.Unlock()
			s.audit(ctx, s.store, "", "link.offer_received", "stream", string(id), map[string]any{"hub": rec.Hub, "name": rec.Name})
		case member:
			// The hub lists this server in a channel it holds nothing of.
			s.sendLeave(pc.peer.ID, id)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if st.PeerID != pc.peer.ID {
		// A channel on its way here from a direct link (section 21a).
		return s.onMovedRecord(ctx, pc, st, f, rec)
	}
	if !hubOwned(st) {
		return wire.Errf(wire.EConflict, "That stream is not a channel of this hub.")
	}
	have, err := s.store.Streams().Channel(ctx, id)
	if err == nil && have.Version < rec.Version && have.HomeHub != "" && have.HomeHub != rec.Hub {
		// Only a channel's home hub signs its record. Another hub of the
		// cluster becomes the home when the home says so in a record of its
		// own, and not before (section 23b).
		s.count(func(st *Stats) { st.Dropped["channel_not_home"]++ })
		s.audit(ctx, s.store, "", "link.record_refused", "stream", string(id),
			map[string]any{"why": rec.Hub + " signed it and the channel's home is " + have.HomeHub})
		return wire.Errf(wire.EBadSignature, "%s is not that channel's home hub.", rec.Hub)
	}
	if (err != nil || have.Version < rec.Version) && st.HomeHub != rec.HomeHub {
		// The home has changed hands, or this server learns where it is.
		if row, herr := s.hubRow(ctx, s.store, pc.peer, rec.HomeHub); herr == nil {
			s.store.Streams().Update(ctx, id, func(x *store.Stream) error {
				x.HomeHub, x.OwnerServerID = rec.HomeHub, row.ID
				return nil
			})
			st.HomeHub, st.OwnerServerID = rec.HomeHub, row.ID
			if !s.holder && st.State == store.StreamActive && s.historyPending(ctx, s.store, id) {
				// Still waiting for the history, which the old home will not
				// send now: the new home is asked at once.
				s.wg.Add(1)
				go func() {
					defer s.wg.Done()
					s.askHistoryAgain(s.ctx, id)
				}()
			}
		}
	}
	if err == nil && have.Private && !rec.Private && have.Version < rec.Version {
		// A private channel stays private (ruling R94).
		s.audit(ctx, s.store, "", "link.record_refused", "stream", string(id), map[string]any{"why": "the record makes a private channel not private"})
		return wire.Errf(wire.EForbidden, "That channel is private, and stays so.")
	}
	if err == nil && have.Version >= rec.Version {
		// Nothing new. The key of the epoch it calls for, if this server
		// lacks it, is asked of the home hub all the same.
		if member && have.Version == rec.Version && st.State == store.StreamActive {
			s.adoptEpoch(ctx, id)
		}
		return nil
	}
	if member && (st.State == store.StreamLeft || st.State == store.StreamRemoved) {
		// This server has left; the hub seems not to know.
		s.sendLeave(pc.peer.ID, id)
		return nil
	}
	wasPrepared, oldEpoch := false, int64(0)
	if old, err := s.channelOf(ctx, s.store, id); err == nil {
		wasPrepared, oldEpoch = old.Prepared, old.Epoch
	}
	if err := s.storeChannel(ctx, s.store, f.Signed, f.Sig, rec); err != nil {
		return err
	}
	if st.HistoryDays != rec.HistoryDays && rec.HistoryDays >= 0 && rec.HistoryDays <= 90 {
		// How much history the channel gives a joining server is the
		// hub's record's to say.
		s.store.Streams().Update(ctx, id, func(x *store.Stream) error {
			x.HistoryDays = rec.HistoryDays
			return nil
		})
	}
	if st.ConversationID != "" && member && !rec.Prepared {
		s.syncManagers(ctx, st.ConversationID, rec)
	}
	if wasPrepared && !rec.Prepared && !rec.Closed && member {
		// A channel moved here from a direct link has gone live on the hub.
		defer func() {
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				s.afterActivation(s.ctx, id)
			}()
		}()
	}
	switch {
	case rec.Closed || (!member && !invited):
		// Closed, or this server is out of it: removed, declined, or an
		// invitation withdrawn.
		if st.State == store.StreamOffered && st.ConversationID == "" {
			s.store.Streams().Delete(ctx, id)
			return nil
		}
		if st.State == store.StreamActive || st.State == store.StreamOffered {
			if s.holder && !rec.Closed {
				s.handedOver(ctx, id) // to another hub of the cluster
			}
			s.detachStream(ctx, st, store.StreamRemoved)
			s.audit(ctx, s.store, "", "link.removed_from_stream", "stream", string(id), map[string]any{"hub": rec.Hub, "closed": rec.Closed})
		}
		return nil
	case member && st.State == store.StreamOffered && st.ConversationID != "":
		if err := s.joinedHub(ctx, st, rec, f.Have); err != nil {
			return err
		}
	case member && st.State == store.StreamActive:
		if err := s.applyRecord(ctx, st, rec); err != nil {
			return err
		}
	case st.State == store.StreamOffered:
		s.store.Streams().Update(ctx, id, func(x *store.Stream) error {
			x.OfferedName = offeredName(rec.Name)
			return nil
		})
		s.mu.Lock()
		s.offers[id] = offer{Topic: rec.Description}
		s.mu.Unlock()
	}
	if st.ConversationID != "" {
		s.chat.TellLocal(ctx, st.ConversationID, EventChannelChanged, map[string]any{"stream_id": id, "channel": memberView(rec)})
	}
	if member && !rec.Prepared && !rec.Closed {
		// The epoch the record calls for begins here once this server holds
		// its key, which the hub sent ahead of the record.
		s.adoptEpoch(ctx, id)
		if rec.Epoch > oldEpoch && len(rec.Joined) > 0 {
			s.joinedEpoch(id, rec, f.Have)
		}
	}
	return nil
}

// adoptEpoch makes the key epoch a hub-owned channel's record calls for this
// server's current one, once it holds both the record and that epoch's key:
// from then on it encrypts under it. The home hub began the epoch when it
// signed the record, and takes nothing under an older one. A key this server
// lacks it asks the home hub for.
func (s *Service) adoptEpoch(ctx context.Context, id model.StreamID) {
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil || !hubOwned(st) || st.State != store.StreamActive {
		return
	}
	rec, err := s.channelOf(ctx, s.store, id)
	if err != nil || rec.Prepared || rec.Closed || !s.inChannel(rec) || rec.Epoch <= st.KeyEpoch {
		return
	}
	if _, err := s.keyOf(ctx, s.store, id, rec.Epoch); err != nil {
		s.askHubKey(ctx, st, rec.Epoch)
		return
	}
	raised := false
	if _, err := s.store.Streams().Update(ctx, id, func(x *store.Stream) error {
		if x.KeyEpoch < rec.Epoch {
			x.KeyEpoch, raised = rec.Epoch, true
		}
		return nil
	}); err != nil || !raised {
		return
	}
	// What waited for the epoch to begin here, or was refused for an
	// epoch that has ended, goes now.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.kickStream(s.ctx, id, true)
	}()
}

// askHubKey asks a hub-owned channel's home hub for one epoch's key, at most
// once every two seconds per epoch.
func (s *Service) askHubKey(ctx context.Context, st store.Stream, epoch int64) {
	rt := s.rt(st.ID)
	now := time.Now()
	rt.mu.Lock()
	if now.Sub(rt.hubAsked[epoch]) < 2*time.Second {
		rt.mu.Unlock()
		return
	}
	rt.hubAsked[epoch] = now
	rt.mu.Unlock()
	if hub, ok := s.homeHub(ctx, st); ok {
		s.sendDirect(ctx, st.PeerID, hub, kKeyRequest, "", map[string]any{"stream_id": st.ID, "epoch": epoch})
	}
}

// homeHub returns the row that stands for a hub-owned channel's home hub,
// with the box key the cluster roster gives it: what a direct message to the
// hub is sealed to.
func (s *Service) homeHub(ctx context.Context, st store.Stream) (store.Server, bool) {
	peer, err := s.store.Peers().Get(ctx, st.PeerID)
	if err != nil || peer.Kind != wire.KindHub {
		return store.Server{}, false
	}
	sv, err := s.hubRow(ctx, s.store, peer, st.HomeHub)
	if err != nil {
		return store.Server{}, false
	}
	box := s.clusterHubBox(peer, sv.Name)
	if len(box) != 32 {
		return store.Server{}, false
	}
	sv.BoxKey = box
	return sv, true
}

// joinedEpoch is what follows, on every server in the channel, a record that
// let servers in: each tells them what of its own it has blocked and who of
// its own is online. It also notes what the hub's log held of each origin
// when it signed the record (`have`, beside it): a server that is asked for
// the snapshot a joining server is owed cuts it only once it holds that much
// (onHistoryRequest), since from that moment the hub took nothing under an
// older epoch, which the joining server cannot read.
func (s *Service) joinedEpoch(id model.StreamID, rec wire.ChannelRecord, have map[string]int64) {
	names := append([]string{}, rec.Joined...)
	rt := s.rt(id)
	rt.mu.Lock()
	rt.joinedHave = have
	rt.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.deafEarsOnJoin(s.ctx, id, names)
		s.sendSnapshot(s.ctx, id)
	}()
}

// awaitHave waits, for at most a while, until this server holds every
// origin's events up to the counters given, and reports whether it does. It
// wakes whenever a counter of the stream moves (progressed), not on a timer.
func (s *Service) awaitHave(ctx context.Context, id model.StreamID, have map[string]int64, most time.Duration) bool {
	rt := s.rt(id)
	deadline := time.NewTimer(most)
	defer deadline.Stop()
	for {
		// The channel is taken before the counters are read: a move between
		// the two closes it, and is not missed.
		rt.mu.Lock()
		if rt.progress == nil {
			rt.progress = make(chan struct{})
		}
		moved := rt.progress
		rt.mu.Unlock()
		servers, err := s.store.Streams().Servers(ctx, id)
		if err != nil {
			return false
		}
		behind := false
		for _, ss := range servers {
			if !s.isSelf(ss.ServerID) && have[ss.Name] > ss.LastN {
				behind = true
			}
		}
		if !behind {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-moved:
		}
	}
}

// progressed wakes whatever waits for this server's counters of a stream to
// move (awaitHave): one has, its transaction committed.
func (rt *streamRT) progressed() {
	rt.mu.Lock()
	if rt.progress != nil {
		close(rt.progress)
		rt.progress = nil
	}
	rt.mu.Unlock()
}

// noteAsked returns what notes a request for the snapshot a joining server
// is owed as this server's own, asked of the server named: called before the
// request leaves (sendDirectNoted), so an answer that comes at once is known
// for the answer it is.
func (rt *streamRT) noteAsked(name string) func(string) {
	return func(msg string) {
		rt.mu.Lock()
		rt.firstAsked[msg] = name
		rt.mu.Unlock()
	}
}

// memberView is the record as every local member of the channel is told it:
// without the servers invited and asking, which are its managers' and the
// owner's to see (they read them with the operation).
func memberView(rec wire.ChannelRecord) HubChannel {
	v := channelView(rec)
	v.Invited, v.Requests = []string{}, []string{}
	return v
}

func offeredName(name string) string {
	if !model.ValidChannelName(name) {
		return "shared channel"
	}
	return name
}

// applyRecord makes a channel this server is in agree with a newer record:
// the server list, and with it the members of any server that is no longer
// there, whose home server cannot say so itself any more.
func (s *Service) applyRecord(ctx context.Context, st store.Stream, rec wire.ChannelRecord) error {
	return s.again(ctx, func() error {
		return s.chat.RemoteTx(ctx, func(tx store.Tx, r *chat.Recorder) error {
			cur, err := s.lockStream(ctx, tx, st.ID)
			if err != nil {
				return err
			}
			before, err := tx.Streams().Servers(ctx, st.ID)
			if err != nil {
				return err
			}
			if err := s.putList(ctx, tx, st.ID, rec.Servers, rec.Version); err != nil {
				return err
			}
			if cur.ConversationID == "" {
				return nil
			}
			var gone []model.AccountID
			for _, ss := range before {
				if ss.State != store.StreamServerActive || rec.Has(ss.Name) || s.isSelf(ss.ServerID) {
					continue
				}
				ids, err := tx.Members().IDs(ctx, cur.ConversationID)
				if err != nil {
					return err
				}
				accounts, err := tx.Accounts().GetMany(ctx, ids)
				if err != nil {
					return err
				}
				for _, acc := range accounts {
					if acc.ServerID == ss.ServerID {
						gone = append(gone, acc.ID)
					}
				}
			}
			if len(gone) > 0 {
				if err := s.chat.DropMembers(ctx, tx, cur.ConversationID, gone); err != nil {
					return err
				}
			}
			if _, err := tx.Conversations().Get(ctx, cur.ConversationID); err != nil {
				return nil
			}
			return s.chat.ApplyConversation(ctx, tx, r, cur.ConversationID, nil, nil)
		})
	})
}

// reapplyRecords puts the server lists of a hub's channels in force again
// after the roster brought a server this one had not met: a list that named
// it could not hold it until now.
func (s *Service) reapplyRecords(ctx context.Context, peer model.PeerID) {
	streams, err := s.store.Streams().List(ctx, store.StreamActive)
	if err != nil {
		return
	}
	for _, st := range streams {
		if st.PeerID != peer || !hubOwned(st) {
			continue
		}
		if rec, err := s.channelOf(ctx, s.store, st.ID); err == nil && s.inChannel(rec) {
			s.store.Tx(ctx, func(tx store.Tx) error { return s.putList(ctx, tx, st.ID, rec.Servers, rec.Version) })
		}
	}
}

// joinedHub is the moment this server is in a channel it asked to join or
// was invited to. The hub said beside the record what its log held of each
// origin then: this server starts from there, since what came before reaches
// it as history from the history sender and not from the log.
func (s *Service) joinedHub(ctx context.Context, st store.Stream, rec wire.ChannelRecord, have map[string]int64) error {
	// What is delivered waits for the history that says who is here.
	rt := s.rt(st.ID)
	rt.mu.Lock()
	if !rt.historyHere && rt.historyWait == nil {
		rt.historyWait = make(chan struct{})
	}
	rt.startHave = map[string]int64{}
	for origin, n := range have {
		rt.startHave[origin] = n
	}
	rt.mu.Unlock()
	s.holdForAnnouncements(st.ID, true)
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		if err := s.putList(ctx, tx, st.ID, rec.Servers, rec.Version); err != nil {
			return err
		}
		if !s.holder {
			// Until the home hub has given it the channel's history whole,
			// what this server cannot apply without that history waits for
			// it (hold.go). A hub's holder keeps its own list (holder.go).
			if err := tx.Settings().Set(ctx, historyPendingKey(st.ID), true, ""); err != nil {
				return err
			}
		}
		if _, err := tx.Streams().Update(ctx, st.ID, func(x *store.Stream) error {
			x.State, x.ServersVersion, x.HomeHub = store.StreamActive, rec.Version, rec.HomeHub
			return nil
		}); err != nil {
			return err
		}
		servers, err := tx.Streams().Servers(ctx, st.ID)
		if err != nil {
			return err
		}
		for _, ss := range servers {
			if n := have[ss.Name]; n > 0 && !s.isSelf(ss.ServerID) {
				if err := tx.Streams().SetLastN(ctx, st.ID, ss.ServerID, n); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		s.holdForAnnouncements(st.ID, false)
		return err
	}
	// A hub's holder whose hub signed the record that made it the home was
	// delivered, and kept, everything the log took since: it applies that
	// now, and asks the log only if it had to let some go (takeEarly). One
	// whose hub was handed the channel by another hub asks the log for what
	// it took while this hub only carried the channel.
	ask := !s.holder || s.takeEarly(st.ID) || rec.Hub != s.id.Name
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.announceMembers(s.ctx, st.ID)
		// What the log took between the hub's admitting this server and
		// this server's hearing of it.
		if cur, err := s.store.Streams().Get(s.ctx, st.ID); err == nil && ask {
			s.catchUp(s.ctx, cur, true)
		}
		s.askFirstHistory(s.ctx, st.ID)
	}()
	return nil
}

// firstHistoryWait is how long a server that has joined a hub-owned channel
// waits for its history before it asks for it. Tests shorten it.
var firstHistoryWait atomic.Int64

func init() { firstHistoryWait.Store(int64(5 * time.Second)) }

// historyAnswerDelay, when a test sets it, is how long a server waits before
// it answers a request for the snapshot a joining server is owed.
var historyAnswerDelay atomic.Int64

// askFirstHistory is how a server that has joined a hub-owned channel gets
// the snapshot a joining server is owed: it asks the channel's home hub,
// which keeps the channel whole, and asks again after a wait while the hub
// has not answered. Nobody sends it unasked, and no member instance is asked:
// the history does not depend on any instance staying. Without it this
// server would know none of the channel's members, and would take nothing
// they say.
//
// A hub's holder that has just become the home of a channel asks instead the
// hub it was handed over from and the channel's instances, every one of them,
// until each has answered: they hold what was said before the hub held the
// channel, and give the hub all of it (onHistoryRequest, askOwed).
func (s *Service) askFirstHistory(ctx context.Context, id model.StreamID) {
	if s.holder {
		// The hub's copy waits on nobody's leave: it asks until it has
		// everything, and remembers whom it still waits on (holder.go).
		s.askOwed(ctx, id)
		return
	}
	rt := s.rt(id)
	here := func() bool {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		return rt.historyHere
	}
	for try := 0; try < 6; try++ {
		if try > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(firstHistoryWait.Load())):
			}
		}
		st, err := s.store.Streams().Get(ctx, id)
		if here() || err != nil || st.State != store.StreamActive {
			return
		}
		rec, err := s.channelOf(ctx, s.store, id)
		if err != nil {
			return
		}
		var ask []store.Server
		if hub, ok := s.homeHub(ctx, st); ok && !s.holds(rec) {
			ask = []store.Server{hub}
		}
		for _, sv := range ask {
			ch := make(chan directMessage, 4)
			msg, err := s.sendDirectNoted(ctx, st.PeerID, sv, kHistoryRequest, map[string]any{"stream_id": id, "first": true}, ch, rt.noteAsked(sv.Name))
			if err != nil {
				continue
			}
			// The parts are handled as they arrive (onHistory); this only
			// waits to see whether the server answered at all.
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
			case <-ch:
				time.Sleep(200 * time.Millisecond)
			}
			s.mu.Lock()
			delete(s.replies, msg)
			s.mu.Unlock()
			if here() || ctx.Err() != nil {
				return
			}
		}
	}
}

// keyHolders are the servers to ask for an epoch's key: on a direct link or
// in an ad-hoc or sealed stream the owner; in a hub-owned channel its home
// hub, which made every key of it, and nobody else.
func (s *Service) keyHolders(ctx context.Context, st store.Stream, epoch int64) []store.Server {
	if hubOwned(st) {
		if hub, ok := s.homeHub(ctx, st); ok {
			return []store.Server{hub}
		}
		return nil
	}
	if st.Owned() {
		return nil
	}
	if owner, err := s.ownerOf(ctx, st); err == nil {
		return []store.Server{owner}
	}
	return nil
}

// openers are the servers to ask to open an envelope this server cannot
// read (section 13): those that may hold its key. In a hub-owned channel its
// home hub, then the channel's other servers, longest-standing first;
// elsewhere whoever holds the key.
func (s *Service) openers(ctx context.Context, st store.Stream, epoch int64) []store.Server {
	if !hubOwned(st) {
		return s.keyHolders(ctx, st, epoch)
	}
	rec, err := s.channelOf(ctx, s.store, st.ID)
	if err != nil {
		return nil
	}
	// The home hub first: its holder has taken every envelope of the
	// channel under every epoch's key (holder.go).
	var out []store.Server
	if hub, ok := s.homeHub(ctx, st); ok && !s.holder {
		out = append(out, hub)
	}
	for _, name := range rec.Servers {
		if name == s.id.Name {
			continue
		}
		if sv, err := s.store.Peers().ServerByName(ctx, name); err == nil {
			out = append(out, sv)
		}
	}
	return out
}

// DirectoryEntry is a channel in a hub's directory as the API shows it
// (schema HubDirectoryEntry).
type DirectoryEntry struct {
	StreamID    model.StreamID        `json:"stream_id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Listing     string                `json:"listing"`
	Managers    []wire.ChannelAccount `json:"managers"`
	Servers     int                   `json:"servers"`
	// State is this server's place in it: none, invited, requested or
	// member.
	State string `json:"state"`
}

// Directory asks a hub for its directory: the channels listed as request or
// open, with name, description, listing mode, managers and the number of
// servers. The answer is taken only when an accepted hub key signed it.
func (s *Service) Directory(ctx context.Context, peerID model.PeerID) ([]DirectoryEntry, error) {
	peer, err := s.store.Peers().Get(ctx, peerID)
	if err != nil || peer.Kind != wire.KindHub || peer.State == store.PeerRemoved {
		return nil, model.ErrNotFound
	}
	pc := s.connOf(peerID)
	if pc == nil {
		return nil, model.ErrUnavailable.With("The hub is not connected.")
	}
	ask := wire.Frame{Frame: wire.FDirectory, ID: pc.conn.NextID()}
	answer, done := s.awaitFrame(peerID, ask.ID)
	defer done()
	if err := pc.conn.Send(ask); err != nil {
		return nil, model.ErrUnavailable.With("The hub is not connected.")
	}
	var f wire.Frame
	select {
	case f = <-answer:
	case <-time.After(actionWait):
		return nil, model.ErrUnavailable.With("The hub did not answer.")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if f.Frame == wire.FError {
		return nil, frameError(f)
	}
	var d wire.Directory
	if err := s.verifyHubBlock(peer, wire.FDirectory, f.Signed, f.Sig, func() []byte { return d.HubKey }, &d); err != nil || d.Hub != peer.HubName {
		return nil, model.ErrUnavailable.With("The hub's directory did not verify.")
	}
	out := []DirectoryEntry{}
	for _, c := range d.Channels {
		e := DirectoryEntry{StreamID: model.StreamID(c.StreamID), Name: c.Name, Description: c.Description, Listing: c.Listing,
			Managers: c.Managers, Servers: c.Servers, State: c.Yours}
		if e.Managers == nil {
			e.Managers = []wire.ChannelAccount{}
		}
		if e.State == "" {
			e.State = "none"
		}
		out = append(out, e)
	}
	return out, nil
}

// JoinListed joins a channel from a hub's directory as a new local channel:
// at once if it is open, and once a manager approves if it is listed for
// requests. Until then the stream is `offered` here.
func (s *Service) JoinListed(ctx context.Context, by model.Account, peerID model.PeerID, id model.StreamID, in AcceptInput) (Stream, model.Conversation, error) {
	entries, err := s.Directory(ctx, peerID)
	if err != nil {
		return Stream{}, model.Conversation{}, err
	}
	var entry *DirectoryEntry
	for i := range entries {
		if entries[i].StreamID == id {
			entry = &entries[i]
		}
	}
	if entry == nil {
		return Stream{}, model.Conversation{}, model.ErrNotFound
	}
	peer, err := s.store.Peers().Get(ctx, peerID)
	if err != nil {
		return Stream{}, model.Conversation{}, err
	}
	st, err := s.store.Streams().Get(ctx, id)
	switch {
	case errors.Is(err, model.ErrNotFound):
		hub, err := s.hubServer(ctx, s.store, peer)
		if err != nil {
			return Stream{}, model.Conversation{}, err
		}
		now := s.now()
		st = store.Stream{ID: id, PeerID: peerID, OwnerServerID: hub.ID, Sequencer: store.SequencerHub, Kind: store.StreamChannel,
			State: store.StreamOffered, OfferedName: offeredName(entry.Name), OfferedAt: now, MaxBlobBytes: s.maxBlobDefault(ctx),
			HomeHub: peer.HubName, CreatedAt: now}
		if err := s.store.Streams().Create(ctx, &st); err != nil {
			return Stream{}, model.Conversation{}, err
		}
		s.mu.Lock()
		s.offers[id] = offer{Topic: entry.Description}
		s.mu.Unlock()
	case err != nil:
		return Stream{}, model.Conversation{}, err
	case st.State != store.StreamOffered || st.ConversationID != "":
		return Stream{}, model.Conversation{}, model.ErrConflict.With("This server is already in that channel, or has already asked.")
	}
	return s.Accept(ctx, by, id, in)
}

// sendJoin tells the hub this server wishes to be in a channel: it was
// invited, or the channel is open, or it is listed and this is a request.
func (s *Service) sendJoin(st store.Stream) {
	f, err := wire.SignBlock(wire.FJoin, wire.ChannelAction{StreamID: string(st.ID), ActionID: newID(), Server: s.id.Name, At: s.now()}, s.keys)
	if err != nil {
		return
	}
	if pc := s.connOf(st.PeerID); pc != nil {
		f.ID = pc.conn.NextID()
		pc.conn.Send(f)
	}
}

// sendLeave tells a hub this server is not, or is no longer, in a channel:
// it leaves, or gives up an invitation or a request.
func (s *Service) sendLeave(peer model.PeerID, id model.StreamID) {
	f, err := wire.SignBlock(wire.FLeave, wire.Control{StreamID: string(id), Server: s.id.Name, At: s.now()}, s.keys)
	if err != nil {
		return
	}
	if pc := s.connOf(peer); pc != nil {
		f.ID = pc.conn.NextID()
		pc.conn.Send(f)
	}
}

// joinRefused is the hub's answer to a wish to join that it will not grant:
// declined by a manager, or the channel is not listed. The local channel
// made for it, if any, is left as an archived local channel.
func (s *Service) joinRefused(ctx context.Context, pc *peerConn, f wire.Frame) {
	st, err := s.store.Streams().Get(ctx, model.StreamID(f.StreamID))
	if err != nil || st.PeerID != pc.peer.ID || !hubOwned(st) || st.State != store.StreamOffered {
		return
	}
	s.audit(ctx, s.store, "", "link.join_declined", "stream", string(st.ID), map[string]any{"hub": pc.name, "why": f.Message})
	if st.ConversationID == "" {
		s.store.Streams().Delete(ctx, st.ID)
		return
	}
	s.detachStream(ctx, st, store.StreamRemoved)
}

// resendJoins sends again, after a reconnection, the wishes to join that
// the hub has not granted yet: it may never have received them.
func (s *Service) resendJoins(ctx context.Context, peer model.PeerID) {
	streams, err := s.store.Streams().List(ctx, store.StreamOffered)
	if err != nil {
		return
	}
	for _, st := range streams {
		if st.PeerID == peer && hubOwned(st) && st.ConversationID != "" {
			s.sendJoin(st)
		}
	}
}

// hubNames are the names of the hubs this server is linked to: rows of
// `servers` that stand for a hub and not for a server.
func (s *Service) hubNames(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	peers, err := s.store.Peers().List(ctx)
	if err != nil {
		return out
	}
	for _, p := range peers {
		if p.Kind == wire.KindHub && p.HubName != "" {
			out[p.HubName] = true
		}
	}
	return out
}

// IsManager reports whether an account of this server is one of a
// hub-owned channel's managers.
func (s *Service) IsManager(ctx context.Context, by model.Account, id model.StreamID) bool {
	rec, err := s.channelOf(ctx, s.store, id)
	return err == nil && s.isManager(by, rec)
}

// IsModerator reports whether an account of this server is one of a
// hub-owned channel's moderators (not its managers, unless named on both lists).
func (s *Service) IsModerator(ctx context.Context, by model.Account, id model.StreamID) bool {
	rec, err := s.channelOf(ctx, s.store, id)
	return err == nil && by.Active() && by.Local() && rec.Moderates(string(by.ID), s.id.Name)
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
