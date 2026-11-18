package link

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is the link as a hub's holder (docs/LINK-PROTOCOL.md section
// 23a, "The hub holds the whole channel"). The hub owns the channels it is
// the home of: their keys, and their whole contents, files and history. It
// keeps them by running this same code against a database of its own, with
// the instance's schema: a service built with Deps.Holder, under the hub's
// own name and keys, that dials the hub on an internal connection and speaks
// the frames any instance speaks.
//
// The holder is no instance of any channel. It is in no record's server
// list, and nobody sees it there. The hub delivers it every envelope of a
// channel it is the home of, sends it every record and every key, and hands
// it the direct messages addressed to the hub that ask for history or a
// file; it applies each envelope as an instance applies it, checking the
// origin's signature against the hub's roster, and fetches each file from
// its origin when the event that names it passes. What it sends (history,
// file chunks) leaves as the hub's own direct messages, signed with the hub's
// key, which an instance takes from its channel's home hub as it takes the
// channel's keys. The holder originates nothing: it has no accounts, and no
// event of a channel is ever its own.

// holds reports whether this service is a hub's holder and the hub is the
// home of the channel a record is of: then it keeps that channel whole.
func (s *Service) holds(rec wire.ChannelRecord) bool {
	home := rec.HomeHub
	if home == "" {
		home = rec.Hub
	}
	return s.holder && home == s.id.Name
}

// inChannel reports whether this server is in a hub-owned channel as a
// record says: one of its instances, or the holder of its home hub.
func (s *Service) inChannel(rec wire.ChannelRecord) bool {
	return rec.Has(s.id.Name) || s.holds(rec)
}

// IsHolder reports whether this service is a hub's holder.
func (s *Service) IsHolder() bool { return s.holder }

// Hold makes, once, the peer a holder dials: its own hub, at the address of
// the internal connection, under the hub's own key. Start dials it from then
// on, as it dials any peer.
func (s *Service) Hold(ctx context.Context, url string) error {
	if !s.holder {
		return errors.New("link: only a hub's holder holds for a hub")
	}
	peers, err := s.store.Peers().List(ctx)
	if err != nil {
		return err
	}
	for _, p := range peers {
		if p.Kind == wire.KindHub && p.HubName == s.id.Name {
			return nil
		}
	}
	p := store.Peer{Kind: wire.KindHub, HubName: s.id.Name, HubSignKey: s.keys.SignPublic(), URL: url, Dials: true,
		State: store.PeerActive, CreatedAt: s.now()}
	if err := s.store.Peers().Create(ctx, &p); err != nil {
		return err
	}
	s.startDialling(p)
	return nil
}

// holdChannel makes the holder's copy of a channel its hub has just become
// the home of: created on the hub, moved onto it, or handed to it by another
// hub of its cluster. The copy is a channel conversation with nobody of the
// holder's own in it. It is taken in as an instance that has joined takes a
// channel (joinedHub): from where the hub's log stood when the record was
// sent, with what came before asked for as history of the channel's
// instances, in full, whatever the channel's days of history.
func (s *Service) holdChannel(ctx context.Context, pc *peerConn, f wire.Frame, rec wire.ChannelRecord) error {
	hub, err := s.hubRow(ctx, s.store, pc.peer, rec.HomeHub)
	if err != nil {
		return err
	}
	id := model.StreamID(rec.StreamID)
	now := s.now()
	conv := model.Conversation{Kind: model.KindChannel, Name: offeredName(rec.Name), Topic: rec.Description, Private: rec.Private,
		DefaultLevel: model.LevelMember, CreatedAt: now, StreamID: id}
	st := store.Stream{ID: id, PeerID: pc.peer.ID, OwnerServerID: hub.ID, Sequencer: store.SequencerHub, Kind: store.StreamChannel,
		State: store.StreamOffered, OfferedName: conv.Name, OfferedAt: now, MaxBlobBytes: s.maxBlobDefault(ctx),
		HomeHub: rec.HomeHub, HistoryDays: rec.HistoryDays, CreatedAt: now}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Conversations().Create(ctx, &conv); err != nil {
			return err
		}
		st.ConversationID = conv.ID
		if err := tx.Streams().Create(ctx, &st); err != nil {
			return err
		}
		return s.storeChannel(ctx, tx, f.Signed, f.Sig, rec)
	})
	if err != nil {
		return err
	}
	if rec.Hub != s.id.Name {
		// The record by which another hub of the cluster handed the channel
		// to this one: that hub holds it whole, and is asked for it first.
		s.store.Settings().Set(ctx, heldFromKey(id), rec.Hub, "")
	}
	s.audit(ctx, s.store, "", "link.channel_held", "stream", string(id), map[string]any{"name": rec.Name})
	if err := s.joinedHub(ctx, st, rec, f.Have); err != nil {
		return err
	}
	if !rec.Prepared && !rec.Closed {
		s.adoptEpoch(ctx, id)
	}
	return nil
}

// Held returns every channel the holder keeps a copy of, in the channel or
// over (closed, or handed to another hub).
func (s *Service) Held(ctx context.Context) ([]model.StreamID, error) {
	var out []model.StreamID
	for _, state := range []string{store.StreamActive, store.StreamRemoved, store.StreamLeft} {
		list, err := s.store.Streams().List(ctx, state)
		if err != nil {
			return nil, err
		}
		for _, st := range list {
			if hubOwned(st) {
				out = append(out, st.ID)
			}
		}
	}
	return out, nil
}

// ForgetHeld removes the holder's copy of a channel the hub no longer has:
// its conversation with everything in it, its files, and its stream.
func (s *Service) ForgetHeld(ctx context.Context, id model.StreamID) {
	if !s.holder {
		return
	}
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil {
		return
	}
	if st.ConversationID != "" {
		gone, err := s.store.Conversations().Delete(ctx, st.ConversationID)
		if err != nil && model.CodeOf(err) != model.CodeNotFound {
			s.log.Warn("link: the hub's copy of a channel could not be removed", "stream", id, "err", err)
			return
		}
		if s.blobs != nil {
			for _, key := range gone.BlobKeys {
				s.blobs.Delete(ctx, key)
			}
		}
	}
	s.store.Streams().Delete(ctx, id)
	s.store.Settings().Delete(ctx, heldFromKey(id))
	s.store.Settings().Delete(ctx, handedOverKey(id))
	s.audit(ctx, s.store, "", "link.channel_forgotten", "stream", string(id), nil)
}

// Handing a channel to another hub of a cluster (section 23b): the contents
// do not travel with the keys. The new home's holder fetches them from the
// old home's holder, which holds the channel whole, as soon as it takes the
// channel over: the old home's history first, then the channel's instances
// if the old home cannot be reached, and each file from the old home first
// and its origin after. The old home keeps its copy for HandOverKeep after
// the record that hands the channel over, answering only the new home, and
// then forgets it: a hub that only carries a channel holds nothing of it.

// HandOverKeep is how long the old home of a channel handed to another hub
// keeps its copy for the new home to fetch from: the time a file is asked
// for before it is given up as unavailable.
const HandOverKeep = 7 * 24 * time.Hour

func heldFromKey(id model.StreamID) string   { return "link.held_from." + string(id) }
func handedOverKey(id model.StreamID) string { return "link.handed_over." + string(id) }

// heldFrom is the hub a channel was handed over from, which keeps its copy
// for a while; empty for a channel that was not.
func (s *Service) heldFrom(ctx context.Context, id model.StreamID) string {
	raw, found, err := s.store.Settings().Get(ctx, heldFromKey(id))
	var hub string
	if err != nil || !found || json.Unmarshal(raw, &hub) != nil {
		return ""
	}
	return hub
}

// clusterHub returns the row that stands for a hub of the cluster a stream's
// peer is in, with the box key the roster gives it.
func (s *Service) clusterHub(ctx context.Context, st store.Stream, name string) (store.Server, bool) {
	peer, err := s.store.Peers().Get(ctx, st.PeerID)
	if err != nil || peer.Kind != wire.KindHub {
		return store.Server{}, false
	}
	sv, err := s.hubRow(ctx, s.store, peer, name)
	if err != nil {
		return store.Server{}, false
	}
	if sv.BoxKey = s.clusterHubBox(peer, name); len(sv.BoxKey) != 32 {
		return store.Server{}, false
	}
	return sv, true
}

// answersFor reports whether this server answers a request for a stream's
// history or files: in it, or a holder whose hub handed the channel to the
// asking hub, which is the channel's home now.
func (s *Service) answersFor(st store.Stream, from store.Server) bool {
	return st.State == store.StreamActive || (s.holder && st.State == store.StreamRemoved && s.isHomeHub(st, from))
}

// handedOver notes that the channel this holder kept has been handed to
// another hub: the copy is kept for the new home for HandOverKeep.
func (s *Service) handedOver(ctx context.Context, id model.StreamID) {
	if _, found, err := s.store.Settings().Get(ctx, handedOverKey(id)); err == nil && !found {
		s.store.Settings().Set(ctx, handedOverKey(id), int64(s.now()), "")
	}
}

// ForgetHandedOver forgets every copy of a channel this holder's hub handed
// to another hub before a time.
func (s *Service) ForgetHandedOver(ctx context.Context, before model.Time) {
	ids, err := s.Held(ctx)
	if err != nil {
		return
	}
	for _, id := range ids {
		raw, found, err := s.store.Settings().Get(ctx, handedOverKey(id))
		var at int64
		if err == nil && found && json.Unmarshal(raw, &at) == nil && model.Time(at) < before {
			s.ForgetHeld(ctx, id)
		}
	}
}
