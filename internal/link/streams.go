package link

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// StreamServer is a server in a stream as the admin console shows it.
type StreamServer struct {
	ServerID model.ServerID `json:"server_id"`
	Name     string         `json:"name"`
	State    string         `json:"state"`
	JoinedAt model.Time     `json:"joined_at,omitempty"`
}

// Stream is a stream as the admin console shows it (schema Stream).
type Stream struct {
	model.Stream
	OfferedTopic   string         `json:"offered_topic,omitempty"`
	OfferExpiresAt model.Time     `json:"offer_expires_at,omitempty"`
	Servers        []StreamServer `json:"servers"`
}

// offer is what an offer said that the streams table has no column for.
type offer struct {
	Topic string
	// Private: the owner's channel is private, and so is every copy of it
	// (ruling R87).
	Private bool
}

func (s *Service) streamView(ctx context.Context, st store.Stream) (Stream, error) {
	out := Stream{Stream: model.Stream{ID: st.ID, PeerID: st.PeerID, ConversationID: st.ConversationID, Reference: st.Sequencer,
		Kind: st.Kind, State: st.State, KeyEpoch: st.KeyEpoch, OfferedName: st.OfferedName, OfferedAt: st.OfferedAt,
		HistoryDays: st.HistoryDays, MaxBlobBytes: st.MaxBlobBytes, CreatedAt: st.CreatedAt}, Servers: []StreamServer{}}
	owner, err := s.ownerOf(ctx, st)
	if err != nil {
		return out, err
	}
	out.OwnerServer = owner.Name
	if st.State == store.StreamOffered {
		out.OfferExpiresAt = st.OfferedAt.Add(offerLife)
		s.mu.Lock()
		out.OfferedTopic = s.offers[st.ID].Topic
		s.mu.Unlock()
	}
	servers, err := s.store.Streams().Servers(ctx, st.ID)
	if err != nil {
		return out, err
	}
	for _, ss := range servers {
		v := StreamServer{ServerID: ss.ServerID, Name: ss.Name, State: ss.State}
		if ss.State != store.StreamServerInvited {
			v.JoinedAt = ss.JoinedAt
		}
		out.Servers = append(out.Servers, v)
	}
	return out, nil
}

// Streams lists shared channels and offers. Ad-hoc channels and sealed chats
// across servers are never shown to an owner.
func (s *Service) Streams(ctx context.Context, state string) ([]Stream, error) {
	list, err := s.store.Streams().List(ctx, state)
	if err != nil {
		return nil, err
	}
	out := []Stream{}
	for _, st := range list {
		if st.Kind != store.StreamChannel {
			continue
		}
		v, err := s.streamView(ctx, st)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// channelStream loads a stream of kind channel for an admin operation; the
// others do not exist as far as an owner is concerned.
func (s *Service) channelStream(ctx context.Context, id model.StreamID) (store.Stream, error) {
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil {
		return st, err
	}
	if st.Kind != store.StreamChannel {
		return st, model.ErrNotFound
	}
	return st, nil
}

// activeNames returns the names of a stream's active servers, sorted, with
// their rows.
func (s *Service) activeServers(ctx context.Context, a store.Areas, id model.StreamID) ([]store.StreamServer, []string, error) {
	all, err := a.Streams().Servers(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	var active []store.StreamServer
	var names []string
	for _, ss := range all {
		if ss.State == store.StreamServerActive {
			active = append(active, ss)
			names = append(names, ss.Name)
		}
	}
	sort.Strings(names)
	return active, names, nil
}

// sendControl signs a stream control frame and sends it over the stream's
// peer: to the hub, which relays it, or on a direct link to the other
// server. Whoever keeps the stream's log says beside the frame, unsigned,
// what the log holds of each origin at that moment (`have`): a server that
// is joining starts from there. On a direct link that is this server. It
// reports whether the frame left.
func (s *Service) sendControl(ctx context.Context, st store.Stream, name string, c wire.Control) bool {
	c.StreamID, c.At = string(st.ID), s.now()
	f, err := wire.SignBlock(name, c, s.keys)
	if err != nil {
		return false
	}
	pc := s.connOf(st.PeerID)
	if pc == nil {
		return false
	}
	if !pc.hub {
		if have, err := s.loggedOf(ctx, s.store, st.ID); err == nil {
			f.Have = have
		}
	}
	f.ID = pc.conn.NextID()
	return pc.conn.Send(f) == nil
}

// ShareInput is what sharing a channel takes.
type ShareInput struct {
	ConversationID model.ConversationID
	PeerID         model.PeerID
	ServerIDs      []model.ServerID
	HistoryDays    int
	MaxBlobBytes   int64
	// Listing is, on a hub, the channel's listing mode: unlisted when
	// empty.
	Listing string
}

// Share shares a named channel over a peer. On a direct link it makes the
// stream and its first key, and sends the other server an offer; this
// server owns the stream. On a hub the hub owns the channel from the start
// (ruling R56): the hub is told, the person sharing is its first manager,
// and each server named is invited in that person's name.
func (s *Service) Share(ctx context.Context, by model.Account, in ShareInput) (Stream, error) {
	conv, err := s.chat.Conversation(ctx, by, in.ConversationID)
	if err != nil {
		return Stream{}, err
	}
	switch {
	case !conv.Named():
		return Stream{}, model.Invalid("conversation_id", "Only a named channel can be shared.")
	case conv.IsShared():
		return Stream{}, model.ErrConflict.With("That channel is already shared.")
	case conv.Locked():
		return Stream{}, model.ErrConflict.With("A locked channel cannot be shared.")
	case conv.Archived():
		return Stream{}, model.ErrConflict.With("An archived channel cannot be shared.")
	case conv.IsKnock():
		// Knock channels are this server's alone for now (ruling R79).
		return Stream{}, model.ErrConflict.With("A knock channel cannot be shared. Make it public or private first.")
	case in.HistoryDays < 0 || in.HistoryDays > 90:
		return Stream{}, model.Invalid("history_days", "history_days is 0 to 90.")
	case in.MaxBlobBytes < 0:
		return Stream{}, model.Invalid("max_blob_bytes", "max_blob_bytes is more than 0.")
	}
	if in.MaxBlobBytes == 0 {
		in.MaxBlobBytes = s.maxBlobDefault(ctx)
	}
	peer, err := s.store.Peers().Get(ctx, in.PeerID)
	if err != nil {
		return Stream{}, model.Invalid("peer_id", "There is no such link.")
	}
	if peer.State != store.PeerActive {
		return Stream{}, model.ErrConflict.With("That link is not active.")
	}
	if conv.Private && peer.Kind == wire.KindHub {
		// Private on the hub too, for good: never listed, and its people
		// admitted by its moderators (ruling R94).
		if in.Listing != "" && in.Listing != wire.ListingUnlisted {
			return Stream{}, model.Invalid("listing", "A private channel is never listed.")
		}
		if err := s.noGroupsIn(ctx, conv.ID); err != nil {
			return Stream{}, err
		}
	}
	var invited []store.Server
	sequencer := store.SequencerSelf
	if peer.Kind == wire.KindHub {
		sequencer = store.SequencerHub
		if in.Listing == "" {
			in.Listing = wire.ListingUnlisted
		}
		if !wire.ValidListing(in.Listing) {
			return Stream{}, model.Invalid("listing", "The listing mode is unlisted, request or open.")
		}
		if s.connOf(peer.ID) == nil {
			return Stream{}, model.ErrConflict.With("The hub is not connected, so the channel cannot be shared now.")
		}
		for _, id := range in.ServerIDs {
			sv, err := s.store.Peers().Server(ctx, id)
			if err != nil || !s.reaches(ctx, peer, id) {
				return Stream{}, model.Invalid("server_ids", "One of those servers is not on that hub.")
			}
			invited = append(invited, sv)
		}
	} else {
		if in.Listing != "" {
			return Stream{}, model.Invalid("listing", "Only a channel on a hub has a listing mode.")
		}
		// A direct link joins exactly two servers (ruling R10).
		if len(in.ServerIDs) > 1 || (len(in.ServerIDs) == 1 && in.ServerIDs[0] != peer.ServerID) {
			return Stream{}, model.Invalid("server_ids", "A direct link shares with the one server at its other end.")
		}
		sv, err := s.store.Peers().Server(ctx, peer.ServerID)
		if err != nil {
			return Stream{}, err
		}
		invited = append(invited, sv)
	}
	for _, sv := range invited {
		if sv.Blocked() {
			return Stream{}, model.ErrConflict.With(sv.Name + " is blocked on this server.")
		}
	}
	if peer.Kind == wire.KindHub {
		st, err := s.shareToHub(ctx, by, peer, conv, in, invited)
		if err != nil {
			return Stream{}, err
		}
		s.audit(ctx, s.store, by.ID, "link.channel_shared", "stream", string(st.ID),
			map[string]any{"conversation_id": conv.ID, "peer_id": peer.ID, "hub": peer.HubName, "listing": in.Listing})
		return s.streamView(ctx, st)
	}
	st, err := s.openStream(ctx, peer, conv, store.StreamChannel, sequencer, in.HistoryDays, in.MaxBlobBytes, invited)
	if err != nil {
		return Stream{}, err
	}
	s.audit(ctx, s.store, by.ID, "link.channel_shared", "stream", string(st.ID), map[string]any{"conversation_id": conv.ID, "peer_id": peer.ID})
	for _, sv := range invited {
		s.sendOffer(ctx, st, sv, nil)
	}
	return s.streamView(ctx, st)
}

// openStream makes a stream this server owns for a local conversation: the
// rows, epoch 1, the mark on the conversation, and stream_open.
func (s *Service) openStream(ctx context.Context, peer store.Peer, conv model.Conversation, kind, sequencer string, historyDays int, maxBlob int64, invited []store.Server) (store.Stream, error) {
	now := s.now()
	st := store.Stream{ID: model.NewID[model.StreamID](), PeerID: peer.ID, ConversationID: conv.ID, Sequencer: sequencer, Kind: kind,
		State: store.StreamActive, KeyEpoch: 1, ServersVersion: 1, MaxBlobBytes: maxBlob, HistoryDays: historyDays, CreatedAt: now}
	if peer.Kind == wire.KindHub {
		// The hub the stream is opened on is its home and its time
		// reference.
		st.HomeHub = peer.HubName
	}
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Streams().Create(ctx, &st); err != nil {
			return err
		}
		if err := tx.Streams().PutServer(ctx, store.StreamServer{StreamID: st.ID, ServerID: s.self.ID, State: store.StreamServerActive, JoinedAt: now}); err != nil {
			return err
		}
		for _, sv := range invited {
			if err := tx.Streams().PutServer(ctx, store.StreamServer{StreamID: st.ID, ServerID: sv.ID, State: store.StreamServerInvited, JoinedAt: now}); err != nil {
				return err
			}
		}
		return s.putKey(ctx, tx, st.ID, 1, secret.RandomBytes(32), 0)
	})
	if err != nil {
		return st, err
	}
	// The conversation is marked: from here on its events are stamped and
	// travel. What it holds already keeps the order it has, before
	// everything that is stamped.
	if err := s.chat.AttachStream(ctx, conv.ID, st.ID, false); err != nil {
		s.store.Streams().Delete(ctx, st.ID)
		return st, err
	}
	s.sendControl(ctx, st, wire.FStreamOpen, wire.Control{Owner: s.id.Name, Servers: []string{s.id.Name}, Version: 1})
	return st, nil
}

// sendOffer sends one server the offer of a stream. extra holds the fields
// an ad-hoc or sealed offer adds.
func (s *Service) sendOffer(ctx context.Context, st store.Stream, to store.Server, extra map[string]any) {
	_, names, err := s.activeServers(ctx, s.store, st.ID)
	if err != nil {
		return
	}
	body := map[string]any{"stream_id": st.ID, "stream_kind": st.Kind, "owner": s.id.Name, "servers": names, "invited": []string{to.Name},
		"history_days": st.HistoryDays, "max_blob_bytes": st.MaxBlobBytes, "expires_at": s.now().Add(offerLife)}
	if st.Kind == store.StreamChannel {
		conv, err := s.store.Conversations().Get(ctx, st.ConversationID)
		if err != nil {
			return
		}
		body["name"], body["topic"], body["private"] = conv.Name, conv.Topic, conv.Private
	}
	for k, v := range extra {
		body[k] = v
	}
	s.sendDirect(ctx, st.PeerID, to, kOffer, "", body)
}

// resendOffersTo sends again, after a reconnection, the offers nobody has
// answered and the acceptances that have not been taken up.
func (s *Service) resendOffersTo(ctx context.Context, peer model.PeerID) {
	streams, err := s.store.Streams().List(ctx, "")
	if err != nil {
		return
	}
	now := s.now()
	for _, st := range streams {
		if st.PeerID != peer {
			continue
		}
		switch {
		case st.State == store.StreamActive && st.Owned():
			servers, err := s.store.Streams().Servers(ctx, st.ID)
			if err != nil {
				continue
			}
			for _, ss := range servers {
				if ss.State != store.StreamServerInvited {
					continue
				}
				if now.Sub(ss.JoinedAt) > offerLife {
					ss.State = store.StreamServerRemoved // the offer lapsed
					s.store.Streams().PutServer(ctx, ss)
					continue
				}
				if sv, err := s.store.Peers().Server(ctx, ss.ServerID); err == nil {
					var extra map[string]any
					if personal(st) {
						if extra = s.personalOffer(ctx, st, sv, nil); extra == nil {
							continue
						}
					}
					s.sendOffer(ctx, st, sv, extra)
				}
			}
		case st.State == store.StreamOffered && st.ConversationID != "" && !hubOwned(st):
			// Accepted here, not yet joined there. (For a channel a hub
			// owns the hub is told again: resendJoins.)
			if owner, err := s.ownerOf(ctx, st); err == nil {
				s.sendDirect(ctx, st.PeerID, owner, kAccept, "", map[string]any{"stream_id": st.ID})
			}
		}
	}
}

// resendOffers is resendOffersTo for every connected peer, from time to time.
func (s *Service) resendOffers(ctx context.Context) {
	s.mu.Lock()
	ids := make([]model.PeerID, 0, len(s.conns))
	for id := range s.conns {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.resendOffersTo(ctx, id)
	}
}

// offerBody is the offer direct message (section 8).
type offerBody struct {
	StreamID     model.StreamID `json:"stream_id"`
	StreamKind   string         `json:"stream_kind"`
	Owner        string         `json:"owner"`
	Name         string         `json:"name"`
	Topic        string         `json:"topic"`
	Private      bool           `json:"private"`
	Servers      []string       `json:"servers"`
	Invited      []string       `json:"invited"`
	HistoryDays  int            `json:"history_days"`
	MaxBlobBytes int64          `json:"max_blob_bytes"`
	ExpiresAt    model.Time     `json:"expires_at"`

	// Ad-hoc and sealed offers.
	ByAccount   *wireProfile      `json:"by_account"`
	AccountIDs  []model.AccountID `json:"account_ids"`
	Members     []wireProfile     `json:"members"`
	DeviceLists json.RawMessage   `json:"device_lists"`
	EpochRecord wire.Bytes        `json:"epoch_record"`
}

// onOffer records an offer for the owner to see, or, for an ad-hoc channel
// or a sealed chat, answers it at once from the rule of section 16.
func (s *Service) onOffer(ctx context.Context, m directMessage) {
	var o offerBody
	if json.Unmarshal(m.raw, &o) != nil || !wire.ValidID(string(o.StreamID)) || o.Owner != m.from.Name {
		s.count(func(st *Stats) { st.Dropped["offer_malformed"]++ })
		return
	}
	decline := func(reason string) {
		s.sendDirect(ctx, m.peer, m.from, kDecline, m.MsgID, map[string]any{"stream_id": o.StreamID, "reason": reason})
	}
	peer, err := s.store.Peers().Get(ctx, m.peer)
	if err != nil {
		return
	}
	if cur, err := s.store.Streams().Get(ctx, o.StreamID); err == nil {
		if cur.State == store.StreamOffered && cur.OwnerServerID == m.from.ID && cur.ConversationID != "" {
			// Already accepted here; the owner seems not to know.
			s.sendDirect(ctx, m.peer, m.from, kAccept, m.MsgID, map[string]any{"stream_id": o.StreamID})
		}
		return
	}
	sequencer := store.SequencerOwner
	if peer.Kind == wire.KindHub {
		sequencer = store.SequencerHub
	}
	st := store.Stream{ID: o.StreamID, PeerID: m.peer, OwnerServerID: m.from.ID, Sequencer: sequencer, Kind: o.StreamKind, State: store.StreamOffered,
		MaxBlobBytes: o.MaxBlobBytes, HistoryDays: o.HistoryDays, OfferedName: o.Name, OfferedAt: s.now(), CreatedAt: s.now()}
	if peer.Kind == wire.KindHub {
		st.HomeHub = peer.HubName
	}
	switch o.StreamKind {
	case store.StreamChannel:
		if peer.Kind == wire.KindHub {
			// A named channel on a hub is the hub's, and is joined by a
			// manager's invitation or from the directory (ruling R56):
			// no server offers one.
			decline("not_allowed")
			return
		}
		if !model.ValidChannelName(o.Name) {
			st.OfferedName = "shared channel"
		}
	case store.StreamAdhoc, store.StreamSealed:
		if reason := s.checkPersonalOffer(ctx, st, m.from, o); reason != "" {
			decline(reason)
			return
		}
	default:
		decline("not_allowed")
		return
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Streams().Create(ctx, &st); err != nil {
			return err
		}
		now := s.now()
		for _, name := range o.Servers {
			sv, err := tx.Peers().ServerByName(ctx, name)
			if err != nil {
				continue // a server this one has not met; the roster will bring it
			}
			if err := tx.Streams().PutServer(ctx, store.StreamServer{StreamID: st.ID, ServerID: sv.ID, State: store.StreamServerActive, JoinedAt: now}); err != nil {
				return err
			}
		}
		return tx.Streams().PutServer(ctx, store.StreamServer{StreamID: st.ID, ServerID: s.self.ID, State: store.StreamServerInvited, JoinedAt: now})
	})
	if err != nil {
		if !errors.Is(err, model.ErrAlreadyExists) {
			s.log.Warn("link: an offer could not be stored", "err", err)
		}
		return
	}
	s.mu.Lock()
	s.offers[st.ID] = offer{Topic: o.Topic, Private: o.Private}
	s.mu.Unlock()
	if st.Kind != store.StreamChannel {
		s.acceptPersonalOffer(ctx, st, m, o)
		return
	}
	s.audit(ctx, s.store, "", "link.offer_received", "stream", string(st.ID), map[string]any{"owner": m.from.Name, "name": o.Name})
}

// AcceptInput is what accepting an offered channel takes.
type AcceptInput struct {
	Name      string
	Private   bool
	SectionID model.SectionID
}

// Accept accepts an offered channel as a new local channel and tells the
// owner server, which then adds this server to the stream.
func (s *Service) Accept(ctx context.Context, by model.Account, id model.StreamID, in AcceptInput) (Stream, model.Conversation, error) {
	st, err := s.channelStream(ctx, id)
	if err != nil {
		return Stream{}, model.Conversation{}, err
	}
	if st.State != store.StreamOffered || st.Owned() || st.ConversationID != "" {
		return Stream{}, model.Conversation{}, model.ErrConflict.With("That offer has already been answered.")
	}
	if s.now().Sub(st.OfferedAt) > offerLife {
		return Stream{}, model.Conversation{}, model.ErrConflict.With("That offer has lapsed.")
	}
	s.mu.Lock()
	o, known := s.offers[st.ID]
	s.mu.Unlock()
	// A copy of a private channel is private here too, and stays so
	// (ruling R87). An offer whose details this server no longer holds
	// (it restarted since) is taken as private.
	private := in.Private || o.Private || (!known && !hubOwned(st))
	// The one who accepts manages the copy, except of a channel a hub owns,
	// whose managers are those on the hub's list. A private channel a hub
	// owns: its people here are those its moderators admit, so the copy starts with
	// none (ruling R94).
	level, hubPrivate := model.LevelManager, false
	if hubOwned(st) {
		rec, _, err := s.channelOf(ctx, s.store, st.ID)
		if err == nil && !s.isManager(by, rec) {
			level = model.LevelMember
		}
		if err == nil && rec.Private {
			private, hubPrivate, level = true, true, ""
		}
	}
	conv, err := s.chat.CreateShared(ctx, by, in.Name, o.Topic, private, level, in.SectionID, st.ID)
	if err != nil {
		return Stream{}, model.Conversation{}, err
	}
	if st, err = s.store.Streams().Update(ctx, st.ID, func(x *store.Stream) error {
		x.ConversationID, x.AcceptedBy = conv.ID, by.ID
		return nil
	}); err != nil {
		return Stream{}, model.Conversation{}, err
	}
	s.audit(ctx, s.store, by.ID, "link.offer_accepted", "stream", string(st.ID), map[string]any{"conversation_id": conv.ID})
	if hubOwned(st) {
		// The hub owns it: this server says it wishes to be in, and the
		// hub admits it (invited, or the channel is open) or records the
		// request for a manager.
		s.sendJoin(st)
	} else if owner, err := s.ownerOf(ctx, st); err == nil {
		// If the owner is not connected this goes again on reconnection.
		s.sendDirect(ctx, st.PeerID, owner, kAccept, "", map[string]any{"stream_id": st.ID})
	}
	v, err := s.streamView(ctx, st)
	if err != nil {
		return Stream{}, model.Conversation{}, err
	}
	if hubPrivate {
		return v, conv, nil // nobody here is in it yet, the owner included
	}
	shown, err := s.chat.Conversation(ctx, by, conv.ID)
	return v, shown, err
}

// Decline declines an offered channel. No stream is left here, and no key.
func (s *Service) Decline(ctx context.Context, by model.AccountID, id model.StreamID) error {
	st, err := s.channelStream(ctx, id)
	if err != nil {
		return err
	}
	if st.State != store.StreamOffered || st.Owned() || st.ConversationID != "" {
		return model.ErrConflict.With("That offer has already been answered.")
	}
	if hubOwned(st) {
		s.sendLeave(st.PeerID, st.ID) // the invitation is given up
	} else if owner, err := s.ownerOf(ctx, st); err == nil {
		s.sendDirect(ctx, st.PeerID, owner, kDecline, "", map[string]any{"stream_id": st.ID, "reason": "declined"})
	}
	if err := s.store.Streams().Delete(ctx, st.ID); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.offers, st.ID)
	s.mu.Unlock()
	s.audit(ctx, s.store, by, "link.offer_declined", "stream", string(st.ID), nil)
	return nil
}

// onAccept is the owner's side of an acceptance: the server is added.
func (s *Service) onAccept(ctx context.Context, m directMessage) {
	var body struct {
		StreamID model.StreamID `json:"stream_id"`
	}
	if json.Unmarshal(m.raw, &body) != nil {
		return
	}
	st, err := s.store.Streams().Get(ctx, body.StreamID)
	if err != nil || !st.Owned() || st.State != store.StreamActive || st.PeerID != m.peer {
		return
	}
	servers, err := s.store.Streams().Servers(ctx, st.ID)
	if err != nil {
		return
	}
	for _, ss := range servers {
		if ss.ServerID == m.from.ID && ss.State == store.StreamServerInvited {
			// Adding waits for the sequencer, so it does not hold up
			// the connection the acceptance came on.
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				if err := s.addServer(s.ctx, st.ID, m.from); err != nil {
					s.log.Warn("link: a server could not be added to a stream", "stream", st.ID, "server", m.from.Name, "err", err)
					return
				}
				if st.Kind == store.StreamSealed {
					s.announceEpochs(s.ctx, st.ID)
				}
			}()
			return
		}
	}
}

// onDecline is the owner's side of a refusal.
func (s *Service) onDecline(ctx context.Context, m directMessage) {
	var body struct {
		StreamID model.StreamID `json:"stream_id"`
		Reason   string         `json:"reason"`
	}
	if json.Unmarshal(m.raw, &body) != nil {
		return
	}
	st, err := s.store.Streams().Get(ctx, body.StreamID)
	if err != nil || !st.Owned() || st.PeerID != m.peer {
		return
	}
	servers, _ := s.store.Streams().Servers(ctx, st.ID)
	for _, ss := range servers {
		if ss.ServerID == m.from.ID && ss.State == store.StreamServerInvited {
			ss.State = store.StreamServerRemoved
			s.store.Streams().PutServer(ctx, ss)
			s.audit(ctx, s.store, "", "link.offer_declined_by", "stream", string(st.ID), map[string]any{"server": m.from.Name, "reason": body.Reason})
			if st.Kind != store.StreamChannel {
				s.personalDeclined(ctx, st)
			}
		}
	}
}

// changeList is the owner's change to a stream's server list, the same four
// steps whether a server joins or goes (sections 8 and 21): the control
// frame, a new key epoch, stream.servers_changed under it, and for a
// joining server its history.
func (s *Service) changeList(ctx context.Context, id model.StreamID, sv store.Server, join bool, reason string) error {
	rt := s.rt(id)
	rt.work.Lock()
	defer rt.work.Unlock()
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil {
		return err
	}
	if !st.Owned() || st.State != store.StreamActive {
		return model.ErrConflict.With("This server does not own that stream.")
	}
	state, joined, left := store.StreamServerRemoved, []string{}, []string{sv.Name}
	if join {
		state, joined, left = store.StreamServerActive, []string{sv.Name}, []string{}
	}
	if err := s.store.Streams().PutServer(ctx, store.StreamServer{StreamID: id, ServerID: sv.ID, State: state, JoinedAt: s.now()}); err != nil {
		return err
	}
	_, names, err := s.activeServers(ctx, s.store, id)
	if err != nil {
		return err
	}
	version := st.ServersVersion + 1
	if st, err = s.store.Streams().Update(ctx, id, func(x *store.Stream) error {
		x.ServersVersion = version
		return nil
	}); err != nil {
		return err
	}
	c := wire.Control{Owner: s.id.Name, Server: sv.Name, Servers: names, Version: version}
	if join {
		s.sendControl(ctx, st, wire.FServerJoined, c)
	} else {
		c.Reason = reason
		s.sendControl(ctx, st, wire.FServerLeft, c)
	}
	epoch, err := s.newEpoch(ctx, st)
	if err != nil {
		return err
	}
	if err := s.originate(ctx, st, evServersChanged, map[string]any{"version": version, "servers": names, "joined": joined, "left": left,
		"epoch": epoch}, epoch); err != nil {
		return err
	}
	if join {
		// The snapshot is cut on the stream's own queue, behind whatever
		// was delivered to this server before the reference took the event
		// above: once it has, nothing under an older key epoch is taken
		// into the log, so the snapshot holds everything the joining
		// server cannot read for itself.
		rt.jobs <- func() {
			if cur, err := s.store.Streams().Get(s.ctx, id); err == nil {
				s.sendHistory(s.ctx, cur, sv)
			}
		}
	}
	return nil
}

// addServer adds a server that accepted its offer.
func (s *Service) addServer(ctx context.Context, id model.StreamID, sv store.Server) error {
	return s.changeList(ctx, id, sv, true, "")
}

// ownedChannel loads a stream for an operation only its owner server may do.
func (s *Service) ownedChannel(ctx context.Context, id model.StreamID) (store.Stream, error) {
	st, err := s.channelStream(ctx, id)
	if err != nil {
		return st, err
	}
	if !st.Owned() {
		return st, model.ErrForbidden.With("Only the server that owns the stream can do that.")
	}
	if st.State != store.StreamActive {
		return st, model.ErrNotFound
	}
	return st, nil
}

// settledChannel is ownedChannel for an operation that is refused while the
// stream is being moved onto a hub.
func (s *Service) settledChannel(ctx context.Context, id model.StreamID) (store.Stream, error) {
	st, err := s.ownedChannel(ctx, id)
	if err == nil && s.moving(ctx, s.store, id) {
		err = errMoving
	}
	return st, err
}

// asModerator does, for a channel a hub owns, what an owner server's
// operation does for a stream it owns: the same thing asked of the hub in
// the name of the person acting, who must be one of the channel's
// managers. handled is false for a stream that is not hub-owned.
func (s *Service) asModerator(ctx context.Context, by model.AccountID, id model.StreamID, in ModerateInput, server model.ServerID) (out Stream, handled bool, err error) {
	st, err := s.channelStream(ctx, id)
	if err != nil || !hubOwned(st) {
		return Stream{}, false, nil
	}
	acc, err := s.store.Accounts().Get(ctx, by)
	if err != nil {
		return Stream{}, true, err
	}
	if server != "" {
		sv, err := s.store.Peers().Server(ctx, server)
		if err != nil || s.isSelf(server) {
			return Stream{}, true, model.ErrNotFound
		}
		in.Server = sv.Name
	}
	rec, err := s.Moderate(ctx, acc, id, in)
	if err != nil {
		return Stream{}, true, err
	}
	// The hub names the manager's home server to make the key of the
	// epoch a change to the server list begins. That is this server, and
	// the answer waits a moment for it, so that it shows the new epoch as
	// the answer of an owner server's own operation does.
	for wait := 0; wait < 150 && rec.KeyMaker == s.id.Name && !rec.Closed; wait++ {
		cur, held, err := s.channelOf(ctx, s.store, id)
		now, err2 := s.store.Streams().Get(ctx, id)
		if err != nil || err2 != nil || now.State != store.StreamActive || now.KeyEpoch >= cur.Epoch || len(held) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return Stream{}, true, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	if st, err = s.store.Streams().Get(ctx, id); err != nil {
		return Stream{}, true, err
	}
	out, err = s.streamView(ctx, st)
	return out, true, err
}

// AddServer offers a shared channel to one more server.
func (s *Service) AddServer(ctx context.Context, by model.AccountID, id model.StreamID, server model.ServerID) (Stream, error) {
	if v, handled, err := s.asModerator(ctx, by, id, ModerateInput{Action: wire.ActInvite}, server); handled {
		return v, err
	}
	st, err := s.settledChannel(ctx, id)
	if err != nil {
		return Stream{}, err
	}
	peer, err := s.store.Peers().Get(ctx, st.PeerID)
	if err != nil {
		return Stream{}, err
	}
	sv, err := s.store.Peers().Server(ctx, server)
	if err != nil || s.isSelf(server) || !s.reaches(ctx, peer, server) || sv.Blocked() {
		return Stream{}, model.ErrNotFound
	}
	servers, err := s.store.Streams().Servers(ctx, id)
	if err != nil {
		return Stream{}, err
	}
	for _, ss := range servers {
		if ss.ServerID == server && ss.State == store.StreamServerActive {
			return s.streamView(ctx, st) // already in it
		}
	}
	if err := s.store.Streams().PutServer(ctx, store.StreamServer{StreamID: id, ServerID: server, State: store.StreamServerInvited, JoinedAt: s.now()}); err != nil {
		return Stream{}, err
	}
	s.audit(ctx, s.store, by, "link.server_offered", "stream", string(id), map[string]any{"server": sv.Name})
	s.sendOffer(ctx, st, sv, nil)
	return s.streamView(ctx, st)
}

// RemoveServer removes a server from a shared channel. A new key epoch
// starts and the removed server is sent nothing further.
func (s *Service) RemoveServer(ctx context.Context, by model.AccountID, id model.StreamID, server model.ServerID) (Stream, error) {
	if v, handled, err := s.asModerator(ctx, by, id, ModerateInput{Action: wire.ActRemoveServer}, server); handled {
		return v, err
	}
	st, err := s.settledChannel(ctx, id)
	if err != nil {
		return Stream{}, err
	}
	servers, err := s.store.Streams().Servers(ctx, id)
	if err != nil {
		return Stream{}, err
	}
	var target *store.StreamServer
	for i, ss := range servers {
		if ss.ServerID == server && !s.isSelf(server) && ss.State != store.StreamServerRemoved {
			target = &servers[i]
		}
	}
	if target == nil {
		return Stream{}, model.ErrNotFound
	}
	s.audit(ctx, s.store, by, "link.server_removed", "stream", string(id), map[string]any{"server": target.Name})
	if target.State == store.StreamServerInvited {
		// Only offered so far: the offer is withdrawn by forgetting it.
		target.State = store.StreamServerRemoved
		if err := s.store.Streams().PutServer(ctx, *target); err != nil {
			return Stream{}, err
		}
		return s.streamView(ctx, st)
	}
	sv, err := s.store.Peers().Server(ctx, server)
	if err != nil {
		return Stream{}, err
	}
	if err := s.changeList(ctx, id, sv, false, "removed"); err != nil {
		return Stream{}, err
	}
	if st, err = s.store.Streams().Get(ctx, id); err != nil {
		return Stream{}, err
	}
	return s.streamView(ctx, st)
}

// serverGone is what an owner does, without being asked, when a hub's
// roster no longer lists a server: it removes it from every stream it owns
// on that hub.
func (s *Service) serverGone(ctx context.Context, peer model.PeerID, sv store.Server) {
	streams, err := s.store.Streams().List(ctx, store.StreamActive)
	if err != nil {
		return
	}
	for _, st := range streams {
		if st.PeerID != peer || !st.Owned() {
			continue
		}
		servers, err := s.store.Streams().Servers(ctx, st.ID)
		if err != nil {
			continue
		}
		for _, ss := range servers {
			if ss.ServerID != sv.ID {
				continue
			}
			switch ss.State {
			case store.StreamServerActive:
				if err := s.changeList(ctx, st.ID, sv, false, "removed"); err != nil {
					s.log.Warn("link: a server that left the hub could not be removed from a stream", "stream", st.ID, "err", err)
				}
				s.audit(ctx, s.store, "", "link.server_removed", "stream", string(st.ID), map[string]any{"server": sv.Name, "why": "removed from the hub"})
			case store.StreamServerInvited:
				ss.State = store.StreamServerRemoved
				s.store.Streams().PutServer(ctx, ss)
			}
		}
	}
}

// Leave stops receiving a shared channel this server does not own. The
// local channel stays, archived, with the history it has.
func (s *Service) Leave(ctx context.Context, by model.AccountID, id model.StreamID) error {
	st, err := s.channelStream(ctx, id)
	if err != nil {
		return err
	}
	if st.Owned() {
		return model.ErrConflict.With("This server owns that stream. Stop sharing it, or hand it over first.")
	}
	if st.State != store.StreamActive {
		return model.ErrConflict.With("This server is not in that stream.")
	}
	if s.moving(ctx, s.store, id) {
		return errMoving
	}
	s.audit(ctx, s.store, by, "link.stream_left", "stream", string(id), nil)
	s.leave(ctx, st)
	return nil
}

// leave tells the sequencer this server is leaving and detaches. If nobody
// can be told, detaching is enough for the leaver.
func (s *Service) leave(ctx context.Context, st store.Stream) {
	s.sendControl(ctx, st, wire.FLeave, wire.Control{Server: s.id.Name})
	s.detachStream(ctx, st, store.StreamLeft)
}

// detachStream ends this server's part in a stream: the state is recorded,
// what was in flight is dropped, keys are kept so the copy can be read, and
// a shared channel's local copy becomes an archived local channel. A
// cross-server ad-hoc channel or sealed chat keeps its mark and becomes a
// read-only copy.
func (s *Service) detachStream(ctx context.Context, st store.Stream, state string) {
	// One transaction: the stream's state, its rows, where the local copy
	// orders what comes next, and what becomes of the conversation. They
	// were separate writes once, and a reader that saw the stream over could
	// still find the conversation shared. The conversation's row is locked
	// first and the stream's second, as everywhere.
	var cur store.Stream
	gone := false
	err := s.again(ctx, func() error {
		gone = false
		return s.chat.RemoteTx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
			before, err := tx.Streams().Get(ctx, st.ID)
			if err != nil {
				return err
			}
			if err := s.lockConversation(ctx, tx, before); err != nil {
				return err
			}
			cur, err = tx.Streams().Update(ctx, st.ID, func(x *store.Stream) error {
				if x.State != store.StreamActive && x.State != store.StreamOffered {
					return model.ErrConflict
				}
				x.State = state
				return nil
			})
			if errors.Is(err, model.ErrConflict) {
				gone = true // detached already, by whoever was first
				return nil
			}
			if err != nil {
				return err
			}
			if err := tx.Streams().Detach(ctx, st.ID); err != nil {
				return err
			}
			if cur.ConversationID == "" {
				return nil
			}
			// Whatever is posted in the local copy from now on is ordered
			// after everything the conversation held while it was shared.
			if err := tx.Streams().SetOrderFloor(ctx, cur.ConversationID, cur.MaxAt+1); err != nil {
				return err
			}
			if cur.Kind == store.StreamChannel {
				return s.chat.AttachStreamIn(ctx, tx, rec, cur.ConversationID, "", true)
			}
			conv := cur.ConversationID
			rec.After(func() {
				s.chat.TellLocal(s.ctx, conv, model.EventConversationUpdated, map[string]any{"read_only": true})
			})
			return nil
		})
	})
	if err != nil {
		s.log.Warn("link: a stream could not be detached", "stream", st.ID, "err", err)
		return
	}
	if gone {
		return
	}
	rt := s.rt(st.ID)
	rt.mu.Lock()
	for id, w := range rt.watchers {
		close(w)
		delete(rt.watchers, id)
	}
	rt.inflight = 0
	rt.mu.Unlock()
	s.dropHold(rt)
}

// closeOwned ends a stream this server owns: stream.closed in the log,
// stream_close to the sequencer, and every server detaches.
func (s *Service) closeOwned(ctx context.Context, st store.Stream) {
	if err := s.originate(ctx, st, evStreamClosed, map[string]any{}, 0); err != nil {
		s.log.Debug("link: stream.closed could not be submitted", "stream", st.ID, "err", err)
	}
	s.sendControl(ctx, st, wire.FStreamClose, wire.Control{Owner: s.id.Name, Version: st.ServersVersion + 1})
	s.detachStream(ctx, st, store.StreamRemoved)
}

// Close stops sharing a channel with every server.
func (s *Service) Close(ctx context.Context, by model.AccountID, id model.StreamID) error {
	if _, handled, err := s.asModerator(ctx, by, id, ModerateInput{Action: wire.ActClose}, ""); handled {
		return err
	}
	st, err := s.settledChannel(ctx, id)
	if err != nil {
		return err
	}
	s.audit(ctx, s.store, by, "link.stream_closed", "stream", string(id), nil)
	s.closeOwned(ctx, st)
	return nil
}

// Update changes the largest file a shared channel carries.
func (s *Service) Update(ctx context.Context, by model.AccountID, id model.StreamID, maxBlob int64) (Stream, error) {
	st, err := s.ownedChannel(ctx, id)
	if err != nil {
		return Stream{}, err
	}
	if maxBlob <= 0 {
		return Stream{}, model.Invalid("max_blob_bytes", "max_blob_bytes is more than 0.")
	}
	if err := s.originate(ctx, st, evLimitsChanged, map[string]any{"max_blob_bytes": maxBlob}, 0); err != nil {
		return Stream{}, err
	}
	s.audit(ctx, s.store, by, "link.stream_limits_changed", "stream", string(id), map[string]any{"max_blob_bytes": maxBlob})
	if st, err = s.store.Streams().Get(ctx, id); err != nil {
		return Stream{}, err
	}
	return s.streamView(ctx, st)
}

// Transfer hands a stream's ownership to another server in it. On a direct
// link the new owner also becomes the sequencer, numbering from the seq
// after the handover event.
func (s *Service) Transfer(ctx context.Context, by model.AccountID, id model.StreamID, to model.ServerID) (Stream, error) {
	st, err := s.settledChannel(ctx, id)
	if err != nil {
		return Stream{}, err
	}
	active, names, err := s.activeServers(ctx, s.store, id)
	if err != nil {
		return Stream{}, err
	}
	var target *store.StreamServer
	for i, ss := range active {
		if ss.ServerID == to && !s.isSelf(to) {
			target = &active[i]
		}
	}
	if target == nil {
		return Stream{}, model.Invalid("server_id", "Ownership goes to a server that is in the stream.")
	}
	if s.connOf(st.PeerID) == nil {
		return Stream{}, model.ErrConflict.With("The link is not connected, so the stream cannot be handed over now.")
	}
	version := st.ServersVersion + 1
	if err := s.originate(ctx, st, evOwnerChanged, map[string]any{"new_owner": target.Name, "version": version}, 0); err != nil {
		return Stream{}, err
	}
	s.sendControl(ctx, st, wire.FOwnerChanged, wire.Control{Owner: s.id.Name, NewOwner: target.Name, Servers: names, Version: version})
	s.audit(ctx, s.store, by, "link.stream_transferred", "stream", string(id), map[string]any{"new_owner": target.Name})
	if st, err = s.store.Streams().Get(ctx, id); err != nil {
		return Stream{}, err
	}
	return s.streamView(ctx, st)
}

// onControl handles a stream control frame from the sequencer: a hub
// relaying the owner's, or on a direct link the owner itself.
func (s *Service) onControl(ctx context.Context, pc *peerConn, f wire.Frame) error {
	if f.Frame == wire.FStreamClose && len(f.Signed) == 0 && pc.hub && f.Reason == "hub_left" {
		// This server's hub has left the cluster of the hub a stream is
		// homed on (section 23b): nothing of the stream can reach this
		// server any more, and the home hub has taken it out. Its own hub's
		// word is enough for that, as its silence would be; the word is
		// taken only for a stream whose home is another hub, which is the
		// only kind a hub that leaves can stop carrying.
		st, err := s.store.Streams().Get(ctx, model.StreamID(f.StreamID))
		if err != nil || st.PeerID != pc.peer.ID {
			return nil
		}
		if st.HomeHub == "" || st.HomeHub == pc.peer.HubName {
			return wire.Errf(wire.EBadFrame, "A hub stops carrying only a stream whose home is another hub.")
		}
		if st.State == store.StreamOffered && st.ConversationID == "" {
			s.store.Streams().Delete(ctx, st.ID)
			return nil
		}
		if st.State == store.StreamActive || st.State == store.StreamOffered {
			s.detachStream(ctx, st, store.StreamRemoved)
			s.audit(ctx, s.store, "", "link.removed_from_stream", "stream", string(st.ID), map[string]any{"hub": pc.peer.HubName, "why": "the hub left the cluster of " + st.HomeHub})
		}
		return nil
	}
	if f.Frame == wire.FStreamClose && len(f.Signed) == 0 {
		// A hub closing a stream whose owner it removed (section 21).
		if !pc.hub || f.Reason != "owner_gone" {
			return wire.Errf(wire.EBadFrame, "An unsigned stream_close comes only from a hub, for a stream whose owner is gone.")
		}
		if st, err := s.store.Streams().Get(ctx, model.StreamID(f.StreamID)); err == nil && st.PeerID == pc.peer.ID {
			s.detachStream(ctx, st, store.StreamRemoved)
		}
		return nil
	}
	var c wire.Control
	if err := wire.PeekBlock(f, &c); err != nil {
		return err
	}
	signerName := c.Owner
	if f.Frame == wire.FLeave {
		signerName = c.Server
	}
	signer, err := s.store.Peers().ServerByName(ctx, signerName)
	if err != nil || s.isSelf(signer.ID) || !s.reaches(ctx, pc.peer, signer.ID) {
		return wire.Errf(wire.ENotOwner, "%s is not a server on this link.", signerName)
	}
	if err := wire.VerifyBlock(f, signer.SignKey, &c); err != nil {
		return err
	}
	id := model.StreamID(c.StreamID)
	st, err := s.store.Streams().Get(ctx, id)
	known := err == nil
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return err
	}
	if f.Frame == wire.FStreamOpen {
		// The first stream_open fixes the owner. A second is refused.
		s.mu.Lock()
		_, again := s.opened[id]
		if !again && !known {
			s.opened[id] = c.Owner
		}
		s.mu.Unlock()
		if again || known {
			return wire.Errf(wire.EConflict, "That stream is already open.")
		}
		return nil
	}
	if !known || st.PeerID != pc.peer.ID {
		return wire.Errf(wire.EUnknownStream, "No such stream here.")
	}
	if f.Frame == wire.FLeave {
		// This server owns the stream and one of the others is leaving.
		if !st.Owned() || st.State != store.StreamActive {
			return wire.Errf(wire.ENotOwner, "This server does not own that stream.")
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			servers, _ := s.store.Streams().Servers(s.ctx, id)
			for _, ss := range servers {
				if ss.ServerID == signer.ID && ss.State == store.StreamServerActive {
					// A move the other server had not agreed to yet is off:
					// there is nobody to move with.
					if mv, ok := s.openMove(s.ctx, s.store, id); ok && outgoing(mv) {
						s.abandonMove(s.ctx, mv, "the other server left the channel")
					}
					if err := s.changeList(s.ctx, id, signer, false, "left"); err != nil {
						s.log.Warn("link: a leaving server could not be taken out of a stream", "err", err)
					}
				}
			}
		}()
		return nil
	}
	if st.Owned() {
		return nil // the hub relaying this server's own frame back
	}
	if st.OwnerServerID != signer.ID {
		return wire.Errf(wire.ENotOwner, "%s does not own that stream.", signerName)
	}
	mine := c.Server == s.id.Name
	switch f.Frame {
	case wire.FServerJoined:
		if mine {
			if st.State != store.StreamOffered || st.ConversationID == "" {
				return nil
			}
			// Beside the frame the hub names the stream's home: the hub it
			// was opened on, its owner's, which in a cluster need not be
			// this server's own. The stream is stamped by that hub's clock.
			if f.HomeHub != "" && f.HomeHub != st.HomeHub && len(s.clusterHubKey(pc.peer, f.HomeHub)) > 0 {
				if moved, err := s.store.Streams().Update(ctx, st.ID, func(x *store.Stream) error {
					x.HomeHub = f.HomeHub
					return nil
				}); err == nil {
					st = moved
				}
			}
			return s.joined(ctx, st, c, f.Have)
		}
		if c.Version != st.ServersVersion+1 {
			return wire.Errf(wire.EConflict, "The server list is at version %d; %d is not the next.", st.ServersVersion, c.Version)
		}
		return s.applyList(ctx, st, c)
	case wire.FServerLeft:
		if mine {
			if st.State == store.StreamActive {
				s.detachStream(ctx, st, store.StreamRemoved)
				s.audit(ctx, s.store, "", "link.removed_from_stream", "stream", string(id), map[string]any{"owner": signerName})
			}
			return nil
		}
		if c.Version != st.ServersVersion+1 {
			return wire.Errf(wire.EConflict, "The server list is at version %d; %d is not the next.", st.ServersVersion, c.Version)
		}
		return s.applyList(ctx, st, c)
	case wire.FOwnerChanged:
		return nil // the stream.owner_changed event carries the change, in order
	case wire.FStreamClose:
		s.detachStream(ctx, st, store.StreamRemoved)
		return nil
	}
	return nil
}

// applyList records a server list an owner's control frame announced.
func (s *Service) applyList(ctx context.Context, st store.Stream, c wire.Control) error {
	return s.store.Tx(ctx, func(tx store.Tx) error {
		return s.putList(ctx, tx, st.ID, c.Servers, c.Version)
	})
}

// putList makes a stream's stored server list equal to names.
func (s *Service) putList(ctx context.Context, tx store.Tx, id model.StreamID, names []string, version int64) error {
	now := s.now()
	in := map[string]bool{}
	for _, n := range names {
		in[n] = true
	}
	have, err := tx.Streams().Servers(ctx, id)
	if err != nil {
		return err
	}
	held := map[string]bool{}
	for _, ss := range have {
		held[ss.Name] = true
		switch {
		case in[ss.Name] && ss.State != store.StreamServerActive:
			ss.State, ss.JoinedAt = store.StreamServerActive, now
		case !in[ss.Name] && ss.State == store.StreamServerActive:
			ss.State = store.StreamServerRemoved
		default:
			continue
		}
		if err := tx.Streams().PutServer(ctx, ss); err != nil {
			return err
		}
	}
	for _, n := range names {
		if held[n] {
			continue
		}
		sv, err := tx.Peers().ServerByName(ctx, n)
		if err != nil {
			continue // not met yet; its events are rejected until it is
		}
		if err := tx.Streams().PutServer(ctx, store.StreamServer{StreamID: id, ServerID: sv.ID, State: store.StreamServerActive, JoinedAt: now}); err != nil {
			return err
		}
	}
	_, err = tx.Streams().Update(ctx, id, func(x *store.Stream) error {
		if version > x.ServersVersion {
			x.ServersVersion = version
		}
		return nil
	})
	return err
}

// joined is the moment this server is added to a stream it accepted. Whoever
// keeps the stream's log said beside the frame what the log held of each
// origin then: this server starts from there, since what came before reaches
// it as history and not from the log.
func (s *Service) joined(ctx context.Context, st store.Stream, c wire.Control, have map[string]int64) error {
	s.holdForAnnouncements(st.ID, true)
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		if err := s.putList(ctx, tx, st.ID, c.Servers, c.Version); err != nil {
			return err
		}
		if _, err := tx.Streams().Update(ctx, st.ID, func(x *store.Stream) error {
			x.State, x.ServersVersion = store.StreamActive, c.Version
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
	// The members already in the local channel are announced, ahead of
	// whatever they post from now on: the other servers accept nothing
	// from an account they have not been told has joined. Each
	// announcement is an event of this server's and is stamped like any
	// other, which needs the offset the stream's time reference tells it.
	// On a link made a moment ago that may be a ping away, and this runs
	// on the connection's reader, which is what would bring it: so not
	// here.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.announceMembers(s.ctx, st.ID)
	}()
	return nil
}

// announceMembers submits member.joined for every local member of a stream's
// conversation, once this server knows its offset from the stream's time
// reference.
//
// From the moment the stream became active here until this returns, nothing
// waiting for the stream is submitted (holdForAnnouncements). The key's
// arrival, a reconnection and the outbox's own clock each submit what waits,
// and any of them could run between the announcements being written and
// what was said earlier being stamped again after them: the earlier message
// then left ahead of its author's announcement with its first stamp, the
// reference took it, and every other server threw it away as written by
// nobody it knew, for good.
func (s *Service) announceMembers(ctx context.Context, id model.StreamID) {
	defer s.kickStream(ctx, id, true)
	defer s.holdForAnnouncements(id, false)
	deadline := time.Now().Add(30 * time.Second)
	for {
		st, err := s.store.Streams().Get(ctx, id)
		if err != nil || st.State != store.StreamActive {
			return
		}
		if _, _, _, known := s.referenceNow(ctx, st); known {
			break
		}
		if time.Now().After(deadline) {
			s.log.Warn("link: this server was never told its clock offset; its members were not announced", "stream", id)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
	// What local members said while the offer was being taken up waits
	// already. It must leave after the announcements below: the other
	// servers accept nothing from an account they have not been told has
	// joined.
	var first int64
	err := s.again(ctx, func() error {
		return s.store.Tx(ctx, func(tx store.Tx) error {
			cur, err := s.lockStream(ctx, tx, id)
			if err != nil {
				return err
			}
			ids, err := tx.Members().IDs(ctx, cur.ConversationID)
			if err != nil {
				return err
			}
			accounts, err := tx.Accounts().GetMany(ctx, ids)
			if err != nil {
				return err
			}
			first = 0
			for _, acc := range accounts {
				if !acc.Local() {
					continue
				}
				stamp, err := s.stamp(ctx, tx, cur)
				if err != nil {
					return err
				}
				if first == 0 {
					first = stamp.N
				}
				p, blobs := s.profileOf(ctx, tx, acc)
				if err := s.putOutbox(ctx, tx, cur, newID(), model.EventMemberJoined,
					map[string]any{"account": p, "blobs": blobs, "joined_at": s.now()}, stamp, 0); err != nil {
					return err
				}
			}
			return nil
		})
	})
	if err != nil {
		s.log.Warn("link: this server's members could not be announced", "stream", id, "err", err)
		return
	}
	// Everything stamped before the first announcement, read now: what
	// was said while the announcements were being written is among it.
	var early []waiting
	if rows, err := s.outboxOf(ctx, id); err == nil && first != 0 {
		for _, w := range rows {
			if w.rec.N < first {
				early = append(early, w)
			}
		}
	}
	if len(early) > 0 {
		if st, err := s.store.Streams().Get(ctx, id); err == nil {
			if err := s.stampAgain(ctx, st, early, false); err != nil {
				s.log.Warn("link: what was said before this server joined could not be put after its members' announcements", "stream", id, "err", err)
			}
		}
	}
}

// holdForAnnouncements holds back, or lets go, what waits for a stream this
// server has just come into, while its members are announced.
func (s *Service) holdForAnnouncements(id model.StreamID, on bool) {
	rt := s.rt(id)
	rt.mu.Lock()
	rt.announcing = on
	rt.mu.Unlock()
}
