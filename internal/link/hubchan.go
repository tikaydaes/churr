package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is a server's side of channels a hub owns (section 23a of
// docs/LINK-PROTOCOL.md, rulings R56 and R59). On a hub, a shared channel
// belongs to the hub: the hub keeps its record (name, description, listing
// mode, member servers, moderators) and is the only party whose signature
// changes it. A server holds the newest record it has been sent, takes the
// server list, the moderators and the key-makers from it and from nowhere
// else, and asks the hub for changes in the name of a moderator who is one
// of its own accounts.
//
// Ad-hoc channels and sealed chats that cross a hub are not channels in this
// sense (R59). They keep the owner-server frames of section 6 and the hub
// carries them blind.

// EventChannelChanged tells a channel's local members that the record the
// hub keeps of it has changed.
const EventChannelChanged = "stream.channel_changed"

// actionWait is how long a moderator's action waits for the hub to answer.
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

// channelOf returns the record held for a hub-owned channel and, by key
// epoch, the server the hub named to make each key.
func (s *Service) channelOf(ctx context.Context, a store.Areas, id model.StreamID) (wire.ChannelRecord, map[string]string, error) {
	var rec wire.ChannelRecord
	makers := map[string]string{}
	row, err := a.Streams().Channel(ctx, id)
	if err != nil {
		return rec, makers, err
	}
	if err := json.Unmarshal(row.Record, &rec); err != nil {
		return rec, makers, err
	}
	json.Unmarshal(row.Makers, &makers)
	return rec, makers, nil
}

// keyMakerOf is the server the hub named to make an epoch's key, or "".
func (s *Service) keyMakerOf(ctx context.Context, a store.Areas, id model.StreamID, epoch int64) string {
	_, makers, err := s.channelOf(ctx, a, id)
	if err != nil {
		return ""
	}
	return makers[strconv.FormatInt(epoch, 10)]
}

// moderates reports whether an account of a named server is on the
// hub-signed moderator list of a stream.
func (s *Service) moderates(ctx context.Context, a store.Areas, st store.Stream, account model.AccountID, server string) bool {
	if !hubOwned(st) || account == "" {
		return false
	}
	rec, _, err := s.channelOf(ctx, a, st.ID)
	return err == nil && rec.Moderates(string(account), server)
}

// storeChannel keeps a record that has been verified: the signed bytes, the
// key-makers it names, and which of its moderators are accounts of this
// server (internal/perm reads those).
func (s *Service) storeChannel(ctx context.Context, a store.Areas, signed, sig []byte, rec wire.ChannelRecord) error {
	_, makers, _ := s.channelOf(ctx, a, model.StreamID(rec.StreamID))
	for epoch, name := range rec.Makers {
		makers[epoch] = name
	}
	if rec.KeyMaker != "" {
		makers[strconv.FormatInt(rec.Epoch, 10)] = rec.KeyMaker
	}
	var mine []model.AccountID
	for _, m := range rec.Moderators {
		if m.Server == s.id.Name {
			mine = append(mine, model.AccountID(m.AccountID))
		}
	}
	return a.Streams().PutChannel(ctx, store.ChannelRecord{StreamID: model.StreamID(rec.StreamID), Version: rec.Version, HomeHub: rec.HomeHub,
		Record: signed, Sig: sig, Makers: mustJSON(makers), Moderators: mine})
}

// HubChannel is a hub-owned channel as the API shows it (schema HubChannel).
type HubChannel struct {
	StreamID    model.StreamID   `json:"stream_id"`
	Hub         string           `json:"hub"`
	Version     int64            `json:"version"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Listing     string           `json:"listing"`
	Servers     []string         `json:"servers"`
	Moderators  []wire.Moderator `json:"moderators"`
	Invited     []string         `json:"invited"`
	Requests    []string         `json:"requests"`
	KeyMaker    string           `json:"key_maker"`
	Closed      bool             `json:"closed"`
	UpdatedAt   model.Time       `json:"updated_at"`
}

func channelView(rec wire.ChannelRecord) HubChannel {
	v := HubChannel{StreamID: model.StreamID(rec.StreamID), Hub: rec.Hub, Version: rec.Version, Name: rec.Name,
		Description: rec.Description, Listing: rec.Listing, Servers: rec.Servers, Moderators: rec.Moderators, Invited: rec.Invited,
		Requests: rec.Requests, KeyMaker: rec.KeyMaker, Closed: rec.Closed, UpdatedAt: rec.At}
	for _, l := range []*[]string{&v.Servers, &v.Invited, &v.Requests} {
		if *l == nil {
			*l = []string{}
		}
	}
	if v.Moderators == nil {
		v.Moderators = []wire.Moderator{}
	}
	return v
}

// isModerator reports whether an account may act as a moderator of a channel
// from this server: an active account of this server that the hub-signed
// list names as one of this server's.
func (s *Service) isModerator(by model.Account, rec wire.ChannelRecord) bool {
	return by.Active() && by.Local() && by.Role != model.RoleBot && rec.Moderates(string(by.ID), s.id.Name)
}

// Channel returns a hub-owned channel's record for one of its local members,
// one of its moderators or the server's owner. The lists of servers invited
// and asking are for moderators and the owner only.
func (s *Service) Channel(ctx context.Context, by model.Account, id model.StreamID) (HubChannel, error) {
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil || !hubOwned(st) {
		return HubChannel{}, model.ErrNotFound
	}
	rec, _, err := s.channelOf(ctx, s.store, id)
	if err != nil {
		return HubChannel{}, model.ErrNotFound
	}
	v := channelView(rec)
	if s.Allowed(ctx, by) != nil && !s.isModerator(by, rec) {
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

// ModerateInput is one moderator's action on a hub-owned channel.
type ModerateInput struct {
	Action      string
	Server      string // a server's name, for invite, approve, decline and remove_server
	AccountID   model.AccountID
	Name        *string
	Description *string
	Listing     string
}

// Moderate asks the hub, in the name of a moderator who is an account of
// this server, to change a channel. It returns the record as the hub signed
// it after the change. The hub checks again that the account is a moderator;
// this server checks first so that it never signs for an account that is
// not.
func (s *Service) Moderate(ctx context.Context, by model.Account, id model.StreamID, in ModerateInput) (HubChannel, error) {
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil || !hubOwned(st) {
		return HubChannel{}, model.ErrNotFound
	}
	rec, _, err := s.channelOf(ctx, s.store, id)
	if err != nil {
		return HubChannel{}, model.ErrNotFound
	}
	if !s.isModerator(by, rec) {
		return HubChannel{}, model.ErrForbidden.With("Only a moderator of the channel can do that.")
	}
	if st.State != store.StreamActive {
		return HubChannel{}, model.ErrNotFound
	}
	a := wire.ModAction{StreamID: string(id), ActionID: newID(), Action: in.Action, Server: in.Server, Name: in.Name,
		Description: in.Description, Listing: in.Listing, By: wire.Moderator{AccountID: string(by.ID), Server: s.id.Name, Username: by.Username}}
	switch in.Action {
	case wire.ModInvite, wire.ModApprove, wire.ModDecline, wire.ModRemoveServer:
		if in.Server == "" || (in.Server == s.id.Name && in.Action != wire.ModRemoveServer) {
			return HubChannel{}, model.Invalid("server", "Name the server.")
		}
		if in.Action == wire.ModInvite {
			if sv, err := s.store.Peers().ServerByName(ctx, in.Server); err == nil && sv.Blocked() {
				return HubChannel{}, model.ErrConflict.With(in.Server + " is blocked on this server.")
			}
		}
	case wire.ModAddModerator, wire.ModRemoveModerator:
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
		} else if in.Action == wire.ModAddModerator && (!acc.Active() || acc.Role == model.RoleBot) {
			return HubChannel{}, model.Invalid("account_id", "A moderator is a person's active account.")
		}
		a.Moderator = &wire.Moderator{AccountID: string(acc.ID), Server: home, Username: acc.Username}
	case wire.ModDescribe:
		if in.Name == nil && in.Description == nil {
			return HubChannel{}, model.Invalid("name", "Give a name, a description or both.")
		}
	case wire.ModSetListing:
		if !wire.ValidListing(in.Listing) {
			return HubChannel{}, model.Invalid("listing", "The listing mode is unlisted, request or open.")
		}
	case wire.ModClose:
	default:
		return HubChannel{}, model.Invalid("action", "There is no such action.")
	}
	if err := s.sendModAction(ctx, st, a, rec.Version); err != nil {
		return HubChannel{}, err
	}
	s.audit(ctx, s.store, by.ID, "link.channel_moderated", "stream", string(id), map[string]any{"action": in.Action, "server": in.Server})
	if rec, _, err = s.channelOf(ctx, s.store, id); err != nil {
		return HubChannel{}, err
	}
	return channelView(rec), nil
}

// sendModAction signs an action as this server, sends it to the hub and
// waits until the hub has refused it or signed a newer record.
func (s *Service) sendModAction(ctx context.Context, st store.Stream, a wire.ModAction, after int64) error {
	a.At = s.now()
	f, err := wire.SignBlock(wire.FModAction, a, s.keys)
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
// with the sharer as its first moderator, and owns the channel from the
// moment it answers; then each server named is invited in the sharer's
// name. Nothing is shared if the hub does not answer.
func (s *Service) shareToHub(ctx context.Context, by model.Account, peer store.Peer, conv model.Conversation, in ShareInput, invited []store.Server) (store.Stream, error) {
	hub, err := s.hubServer(ctx, s.store, peer)
	if err != nil {
		return store.Stream{}, err
	}
	now := s.now()
	st := store.Stream{ID: model.NewID[model.StreamID](), PeerID: peer.ID, ConversationID: conv.ID, OwnerServerID: hub.ID, Sequencer: store.SequencerHub,
		Kind: store.StreamChannel, State: store.StreamActive, KeyEpoch: 1, MaxBlobBytes: in.MaxBlobBytes, HistoryDays: in.HistoryDays,
		HomeHub: peer.HubName, CreatedAt: now}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Streams().Create(ctx, &st); err != nil {
			return err
		}
		if err := tx.Streams().PutServer(ctx, store.StreamServer{StreamID: st.ID, ServerID: s.self.ID, State: store.StreamServerActive, JoinedAt: now}); err != nil {
			return err
		}
		// The sharer's server is the first key-maker: epoch 1 is its own.
		return s.putKey(ctx, tx, st.ID, 1, secret.RandomBytes(32), 0)
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
	me := wire.Moderator{AccountID: string(by.ID), Server: s.id.Name, Username: by.Username}
	cc := wire.ChannelCreate{StreamID: string(st.ID), Name: conv.Name, Description: conv.Topic, Listing: in.Listing, Creator: me, At: now}
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
		s.sendModAction(ctx, st, wire.ModAction{StreamID: string(st.ID), ActionID: newID(), Action: wire.ModClose, By: me}, 1)
		return fail(err)
	}
	for _, sv := range invited {
		rec, _, err := s.channelOf(ctx, s.store, st.ID)
		if err != nil {
			return st, err
		}
		a := wire.ModAction{StreamID: string(st.ID), ActionID: newID(), Action: wire.ModInvite, Server: sv.Name, By: me}
		if err := s.sendModAction(ctx, st, a, rec.Version); err != nil {
			return st, err
		}
	}
	return s.store.Streams().Get(ctx, st.ID)
}

// onChannel takes a channel record from a hub: the only way a hub-owned
// channel's server list, moderators, listing mode, name or description
// change on this server. A record that is not signed by a hub this server
// accepts on that link changes nothing.
func (s *Service) onChannel(ctx context.Context, pc *peerConn, f wire.Frame) error {
	var rec wire.ChannelRecord
	if err := s.verifyHubBlock(pc.peer, wire.FChannel, f.Signed, f.Sig, func() []byte { return rec.HubKey }, &rec); err != nil {
		s.count(func(st *Stats) { st.Dropped["channel_bad_signature"]++ })
		s.audit(ctx, s.store, "", "link.record_refused", "peer", string(pc.peer.ID), map[string]any{"why": "the signature is not an accepted hub's"})
		return err
	}
	if !wire.ValidID(rec.StreamID) || rec.Version < 1 || !wire.ValidListing(rec.Listing) {
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
	// One record at a time per channel: two that arrive together must not
	// both find the older one held.
	rt := s.rt(id)
	rt.record.Lock()
	defer rt.record.Unlock()
	self := s.id.Name
	member, invited := rec.Has(self), false
	for _, n := range rec.Invited {
		invited = invited || n == self
	}
	st, err := s.store.Streams().Get(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		switch {
		case rec.Closed || rec.Prepared:
		case invited:
			// A moderator invited this server: its owner sees an offer.
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
		}
	}
	if err == nil && have.Version >= rec.Version {
		// Nothing new. A key this server was named to make and has not
		// made is made now all the same: the first try may have failed.
		if member && rec.KeyMaker == self && have.Version == rec.Version && st.State == store.StreamActive {
			s.makeKeySoon(id)
		}
		return nil
	}
	if member && (st.State == store.StreamLeft || st.State == store.StreamRemoved) {
		// This server has left; the hub seems not to know.
		s.sendLeave(pc.peer.ID, id)
		return nil
	}
	wasPrepared := false
	if old, _, err := s.channelOf(ctx, s.store, id); err == nil {
		wasPrepared = old.Prepared
	}
	if err := s.storeChannel(ctx, s.store, f.Signed, f.Sig, rec); err != nil {
		return err
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
	if member && rec.KeyMaker == self {
		s.makeKeySoon(id)
	}
	return nil
}

// memberView is the record as every local member of the channel is told it:
// without the servers invited and asking, which are its moderators' and the
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
		if rec, _, err := s.channelOf(ctx, s.store, st.ID); err == nil && rec.Has(s.id.Name) {
			s.store.Tx(ctx, func(tx store.Tx) error { return s.putList(ctx, tx, st.ID, rec.Servers, rec.Version) })
		}
	}
}

// joinedHub is the moment this server is in a channel it asked to join or
// was invited to. The hub said beside the record what its log held of each
// origin then: this server starts from there, since what came before reaches
// it as history from the key-maker and not from the log.
func (s *Service) joinedHub(ctx context.Context, st store.Stream, rec wire.ChannelRecord, have map[string]int64) error {
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		if err := s.putList(ctx, tx, st.ID, rec.Servers, rec.Version); err != nil {
			return err
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
		return err
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.announceMembers(s.ctx, st.ID)
		// What the log took between the hub's admitting this server and
		// this server's hearing of it.
		if cur, err := s.store.Streams().Get(s.ctx, st.ID); err == nil {
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

// askFirstHistory is what a server does that joined a hub-owned channel and
// was not sent the snapshot a joining server is owed: it was not connected
// when its key-maker sent it, or the hub named a key-maker that did not.
// After a wait it asks the channel's servers for it, one after another, the
// current key-maker first. Without it this server would know none of the
// channel's members, and would take nothing they say.
func (s *Service) askFirstHistory(ctx context.Context, id model.StreamID) {
	rt := s.rt(id)
	here := func() bool {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		return rt.historyHere
	}
	for try := 0; try < 6; try++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(firstHistoryWait.Load())):
		}
		st, err := s.store.Streams().Get(ctx, id)
		if here() || err != nil || st.State != store.StreamActive {
			return
		}
		rec, _, err := s.channelOf(ctx, s.store, id)
		if err != nil {
			return
		}
		names := append([]string{rec.KeyMaker}, rec.Servers...)
		asked := map[string]bool{s.id.Name: true}
		for _, name := range names {
			if asked[name] || !rec.Has(name) {
				continue
			}
			asked[name] = true
			sv, err := s.store.Peers().ServerByName(ctx, name)
			if err != nil {
				continue
			}
			ch := make(chan directMessage, 4)
			msg, err := s.sendDirectAwait(ctx, st.PeerID, sv, kHistoryRequest, map[string]any{"stream_id": id, "first": true}, ch)
			if err != nil {
				continue
			}
			rt.mu.Lock()
			rt.firstAsked[msg] = true
			rt.mu.Unlock()
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

// makeKeySoon makes the key this server was named to make, off the
// connection's reader.
func (s *Service) makeKeySoon(id model.StreamID) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.makeKey(s.ctx, id); err != nil {
			s.log.Warn("link: the key this server was named to make could not be made", "stream", id, "err", err)
		}
	}()
}

// makeKey is what the server a hub names as key-maker does: it makes the
// stream key of the epoch the record calls for, seals it to every server in
// the hub-signed list and to nobody else, starts the epoch with
// stream.servers_changed, and sends the servers that have just joined their
// history. The hub never sees the key: it travels in direct messages sealed
// to each server (section 7).
func (s *Service) makeKey(ctx context.Context, id model.StreamID) error {
	rt := s.rt(id)
	rt.work.Lock()
	defer rt.work.Unlock()
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil || !hubOwned(st) || st.State != store.StreamActive {
		return nil
	}
	rec, _, err := s.channelOf(ctx, s.store, id)
	if err != nil || rec.KeyMaker != s.id.Name || !rec.Has(s.id.Name) || rec.Closed || rec.Prepared || st.KeyEpoch >= rec.Epoch {
		return nil
	}
	if s.testNoKeys.Load() {
		return nil // a test is playing a key-maker that does nothing
	}
	var key []byte
	if k, err := s.keyOf(ctx, s.store, id, rec.Epoch); err == nil {
		key = k.key // made before; the epoch was never started
	} else {
		key = secret.RandomBytes(32)
		if err := s.putKey(ctx, s.store, id, rec.Epoch, key, 0); err != nil {
			return err
		}
	}
	if hook := s.testKeyMade.Load(); hook != nil {
		(*hook)(id, rec.Epoch, key)
	}
	for _, name := range rec.Servers {
		if name == s.id.Name {
			continue
		}
		if sv, err := s.store.Peers().ServerByName(ctx, name); err == nil {
			s.sendKey(ctx, st, sv.ID, rec.Epoch, key, "")
		}
	}
	eventID := newID()
	err = s.emitOwnID(ctx, st, eventID, evServersChanged, map[string]any{"version": rec.Version, "servers": rec.Servers, "joined": orNone(rec.Joined),
		"left": orNone(rec.Left), "epoch": rec.Epoch}, rec.Epoch, true)
	if err != nil && !errors.Is(err, model.ErrUnavailable) {
		return err
	}
	// The snapshot for a server that has just joined is cut once the hub
	// has taken the event above (from then on it takes nothing under an
	// older epoch), and on the stream's own queue, behind whatever was
	// delivered here before that.
	var joined []store.Server
	for _, name := range rec.Joined {
		if name == s.id.Name || !rec.Has(name) {
			continue
		}
		if sv, err := s.store.Peers().ServerByName(ctx, name); err == nil {
			joined = append(joined, sv)
		}
	}
	if len(joined) == 0 {
		return nil
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for try := 0; try < 1500; try++ {
			if _, err := s.store.Streams().GetOutbox(s.ctx, eventID); errors.Is(err, model.ErrNotFound) {
				break // acknowledged
			}
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
		rt.jobs <- func() {
			if cur, err := s.store.Streams().Get(s.ctx, id); err == nil && cur.State == store.StreamActive {
				for _, sv := range joined {
					s.sendHistory(s.ctx, cur, sv)
				}
			}
		}
	}()
	return nil
}

func orNone(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

// keyHolders are the servers to ask for an epoch's key, in order: on a
// direct link or in an ad-hoc or sealed stream the owner; in a hub-owned
// channel the server the hub named to make that key, and after it the
// channel's other servers, any of which may hold it.
func (s *Service) keyHolders(ctx context.Context, st store.Stream, epoch int64) []store.Server {
	if !hubOwned(st) {
		if st.Owned() {
			return nil
		}
		if owner, err := s.ownerOf(ctx, st); err == nil {
			return []store.Server{owner}
		}
		return nil
	}
	rec, makers, err := s.channelOf(ctx, s.store, st.ID)
	if err != nil {
		return nil
	}
	var out []store.Server
	add := func(name string) {
		if name == "" || name == s.id.Name || !rec.Has(name) {
			return
		}
		for _, sv := range out {
			if sv.Name == name {
				return
			}
		}
		if sv, err := s.store.Peers().ServerByName(ctx, name); err == nil {
			out = append(out, sv)
		}
	}
	add(makers[strconv.FormatInt(epoch, 10)])
	for _, name := range rec.Servers {
		add(name)
	}
	return out
}

// keyFrom reports whether a key for an epoch of a hub-owned channel may be
// taken from a server: from the maker the hub named for that epoch always;
// from another server of the channel only as the answer to this server's
// own request, and only for an epoch that is already over, so that nobody
// but its maker can put a key in place for an epoch that has yet to begin.
func (s *Service) keyFrom(ctx context.Context, st store.Stream, from store.Server, epoch int64, answer bool) bool {
	rec, makers, err := s.channelOf(ctx, s.store, st.ID)
	if err != nil || !rec.Has(from.Name) {
		return false
	}
	if makers[strconv.FormatInt(epoch, 10)] == from.Name {
		return true
	}
	if !answer || epoch >= rec.Epoch {
		return false
	}
	rt := s.rt(st.ID)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.keyAsked[epoch]
}

// DirectoryEntry is a channel in a hub's directory as the API shows it
// (schema HubDirectoryEntry).
type DirectoryEntry struct {
	StreamID    model.StreamID   `json:"stream_id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Listing     string           `json:"listing"`
	Moderators  []wire.Moderator `json:"moderators"`
	Servers     int              `json:"servers"`
	// State is this server's place in it: none, invited, requested or
	// member.
	State string `json:"state"`
}

// Directory asks a hub for its directory: the channels listed as request or
// open, with name, description, listing mode, moderators and the number of
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
			Moderators: c.Moderators, Servers: c.Servers, State: c.Yours}
		if e.Moderators == nil {
			e.Moderators = []wire.Moderator{}
		}
		if e.State == "" {
			e.State = "none"
		}
		out = append(out, e)
	}
	return out, nil
}

// JoinListed joins a channel from a hub's directory as a new local channel:
// at once if it is open, and once a moderator approves if it is listed for
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
	f, err := wire.SignBlock(wire.FJoin, wire.ModAction{StreamID: string(st.ID), ActionID: newID(), Server: s.id.Name, At: s.now()}, s.keys)
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
// declined by a moderator, or the channel is not listed. The local channel
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

// TestNoKeys makes this server a key-maker that never makes the key it is
// named to make, or with false puts it back. It is for tests.
func (s *Service) TestNoKeys(on bool) { s.testNoKeys.Store(on) }

// TestKeyMade has fn told every stream key this server makes for a
// hub-owned channel. It is for tests that look for a key where it must
// never be.
func (s *Service) TestKeyMade(fn func(stream model.StreamID, epoch int64, key []byte)) {
	if fn == nil {
		s.testKeyMade.Store(nil)
		return
	}
	s.testKeyMade.Store(&fn)
}

// namedMaker reports whether the hub has named a server as the maker of any
// key epoch of a channel this server holds the record of, and it is still
// in the channel.
func (s *Service) namedMaker(ctx context.Context, st store.Stream, name string) bool {
	rec, makers, err := s.channelOf(ctx, s.store, st.ID)
	if err != nil || !rec.Has(name) {
		return false
	}
	for _, maker := range makers {
		if maker == name {
			return true
		}
	}
	return false
}

// IsModerator reports whether an account of this server is one of a
// hub-owned channel's moderators.
func (s *Service) IsModerator(ctx context.Context, by model.Account, id model.StreamID) bool {
	rec, _, err := s.channelOf(ctx, s.store, id)
	return err == nil && s.isModerator(by, rec)
}
