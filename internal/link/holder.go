package link

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
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
// on, as it dials any peer. Each time, it asks again for the earlier history
// of every channel it still lacks some of.
func (s *Service) Hold(ctx context.Context, url string) error {
	if !s.holder {
		return errors.New("link: only a hub's holder holds for a hub")
	}
	peers, err := s.store.Peers().List(ctx)
	if err != nil {
		return err
	}
	s.resumeOwed(ctx)
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
	// Who may hold what was said before the hub held the channel: the hub
	// it was handed over from, which holds it whole, then every instance in
	// the record. Each is asked until it has answered (askOwed), and the
	// list is kept, so a restart or an instance away for a week costs none
	// of it.
	var owed []string
	if rec.Hub != s.id.Name {
		// The record by which another hub of the cluster handed the channel
		// to this one: that hub holds it whole, and is asked for it first.
		s.store.Settings().Set(ctx, heldFromKey(id), rec.Hub, "")
		owed = append(owed, rec.Hub)
	}
	for _, name := range rec.Servers {
		if name != s.id.Name && !slices.Contains(owed, name) {
			owed = append(owed, name)
		}
	}
	if len(owed) > 0 {
		if err := s.store.Settings().Set(ctx, owedKey(id), owed, ""); err != nil {
			return err
		}
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
	s.store.Settings().Delete(ctx, owedKey(id))
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
	// An instance that has left or been taken out of the channel still
	// answers its home hub with the copy it kept: the hub may not yet hold
	// what it said before the hub became the home.
	return st.State == store.StreamActive || (st.ConversationID != "" && s.isHomeHub(st, from)) ||
		(s.holder && st.State == store.StreamRemoved && s.isHomeHub(st, from))
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

// ------------------------------------------------------- history still owed

// What was said in a channel before its hub became the home is held by
// others: the hub it was handed over from, and the channel's instances, each
// of which may hold a part (one that joined late, one whose retention took
// some). The holder keeps, in its database, the list of those it has not yet
// had an answer from (owedKey), and asks each of them with history_request
// carrying `first: true` until it has: after a restart, and whenever one of
// them may have come back. The channel's earlier history is complete when
// the list is empty: when the hub it was handed over from has answered (it
// holds the channel whole), or when every one of them has answered or can
// never answer (it holds nothing of the channel, or is gone from the hub and
// the cluster).

func owedKey(id model.StreamID) string { return "link.history_owed." + string(id) }

// owedWait is the longest the holder waits between two rounds of asking
// for history still owed; the first wait is firstHistoryWait, doubling. A
// server that connects to the hub wakes it at once (Nudge). Tests shorten
// it.
var owedWait atomic.Int64

func init() { owedWait.Store(int64(2 * time.Minute)) }

// owedMu serialises changes to the lists of history still owed.
var owedMu sync.Mutex

// HistoryOwed is who the holder still waits on for a channel's earlier
// history, in the order it asks them; empty when it has all of it.
func (s *Service) HistoryOwed(ctx context.Context, id model.StreamID) []string {
	raw, found, err := s.store.Settings().Get(ctx, owedKey(id))
	var out []string
	if err != nil || !found || json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

// settleOwed takes a server off the list of those a channel's earlier
// history is owed by: it has answered, all it holds, or it never can. The
// hub the channel was handed over from holds the channel whole, so its
// answer settles the whole list.
func (s *Service) settleOwed(ctx context.Context, id model.StreamID, name string, whole bool) {
	s.settleOwedAs(ctx, id, name, true, whole)
}

// settleOwedAs is settleOwed for a server that answered (whole: the hub
// the channel was handed over from, holding all of it), or, with answered
// false, that cannot.
func (s *Service) settleOwedAs(ctx context.Context, id model.StreamID, name string, answered, whole bool) {
	owedMu.Lock()
	defer owedMu.Unlock()
	owed := s.HistoryOwed(ctx, id)
	if !slices.Contains(owed, name) {
		return
	}
	if answered && whole && name == s.heldFrom(ctx, id) {
		owed = nil
	} else {
		owed = slices.DeleteFunc(owed, func(n string) bool { return n == name })
	}
	if !answered {
		s.log.Warn("link: a server that may hold some of a channel's earlier history cannot give it", "stream", id, "server", name)
		s.audit(ctx, s.store, "", "link.history_unavailable", "stream", string(id), map[string]any{"server": name})
	}
	if len(owed) == 0 {
		s.store.Settings().Delete(ctx, owedKey(id))
		s.audit(ctx, s.store, "", "link.history_complete", "stream", string(id), nil)
		// What waited for the history is applied, and then every instance
		// that was given a short snapshot is told to ask again.
		rt := s.rt(id)
		s.onJobs(rt, func() {
			s.historyHere(s.ctx, rt)
			s.tellShort(s.ctx, id, "")
		})
		return
	}
	s.store.Settings().Set(ctx, owedKey(id), owed, "")
}

// resumeOwed starts asking again, after the holder has started, for every
// channel whose earlier history is still owed.
func (s *Service) resumeOwed(ctx context.Context) {
	ids, err := s.Held(ctx)
	if err != nil {
		return
	}
	for _, id := range ids {
		if len(s.HistoryOwed(ctx, id)) > 0 {
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				s.askOwed(s.ctx, id)
			}()
		}
	}
}

// Nudge tells the holder that a server has connected to its hub, or, with
// "", that a trunk to another hub of the cluster has come up: whoever it
// still waits on for a channel's earlier history may answer now, and is
// asked at once.
func (s *Service) Nudge(server string) {
	if !s.holder {
		return
	}
	// An instance given a short snapshot of a channel whose history is now
	// complete is told to ask again.
	s.tellAllShort(s.ctx, server)
	s.mu.Lock()
	wake := make(map[model.StreamID]chan struct{}, len(s.owing))
	for id, ch := range s.owing {
		wake[id] = ch
	}
	s.mu.Unlock()
	for id, ch := range wake {
		if server != "" && !slices.Contains(s.HistoryOwed(s.ctx, id), server) {
			continue
		}
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// askOwed asks, round after round, everyone a channel's earlier history is
// still owed by, until nobody is left or the channel is no longer this
// holder's to keep. One runs per channel.
func (s *Service) askOwed(ctx context.Context, id model.StreamID) {
	wake := make(chan struct{}, 1)
	s.mu.Lock()
	if s.owing == nil {
		s.owing = map[model.StreamID]chan struct{}{}
	}
	if _, running := s.owing[id]; running {
		s.mu.Unlock()
		return
	}
	s.owing[id] = wake
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.owing, id)
		s.mu.Unlock()
	}()
	rt := s.rt(id)
	wait := time.Duration(firstHistoryWait.Load())
	for {
		st, err := s.store.Streams().Get(ctx, id)
		if err != nil || st.State != store.StreamActive {
			return
		}
		owed := s.HistoryOwed(ctx, id)
		if len(owed) == 0 {
			return
		}
		from := s.heldFrom(ctx, id)
		for _, name := range owed {
			if !slices.Contains(s.HistoryOwed(ctx, id), name) {
				continue
			}
			var sv store.Server
			ok := false
			if name == from {
				sv, ok = s.clusterHub(ctx, st, name)
			} else if row, err := s.store.Peers().ServerByName(ctx, name); err == nil {
				sv, ok = row, s.reaches(ctx, store.Peer{ID: st.PeerID, Kind: wire.KindHub}, row.ID)
			}
			if !ok {
				// Gone from the hub and the cluster: nobody can reach it
				// again to ask.
				s.settleOwedAs(ctx, id, name, false, false)
				continue
			}
			ch := make(chan directMessage, 4)
			msg, err := s.sendDirectAwait(ctx, st.PeerID, sv, kHistoryRequest, map[string]any{"stream_id": id, "first": true}, ch)
			if err != nil {
				break // the hub is not connected: the next round
			}
			rt.mu.Lock()
			rt.firstAsked[msg] = name
			rt.mu.Unlock()
			// The parts are taken as they arrive (onHistory), which settles
			// the server's debt with the last of them; this only waits to see
			// whether it answered at all.
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
			case m := <-ch:
				if m.Kind == kError && m.Code == wire.EUnknownStream {
					s.settleOwedAs(ctx, id, name, false, false) // it holds nothing of the channel
				} else {
					// History, or not_in_stream from one that has yet to
					// learn this hub is the home: asked again next round.
					time.Sleep(200 * time.Millisecond)
				}
			}
			s.mu.Lock()
			delete(s.replies, msg)
			s.mu.Unlock()
			if ctx.Err() != nil {
				return
			}
		}
		if len(s.HistoryOwed(ctx, id)) == 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
			wait = time.Duration(firstHistoryWait.Load())
		case <-time.After(wait):
			if wait *= 2; wait > time.Duration(owedWait.Load()) {
				wait = time.Duration(owedWait.Load())
			}
		}
	}
}

// ------------------------------------------------- snapshots given short

// An instance that joins while the holder still waits for some of what was
// said before the hub held the channel is given the snapshot the holder has,
// marked short. The holder notes the instance (shortKey), and once the
// channel's earlier history is complete tells it, with history_more, to ask
// for its snapshot again: at once, whenever the instance next connects to
// the hub, and after the hub restarts, until the instance has asked and been
// given one that is whole. The instance, for its part, keeps waiting for the
// history until it has a snapshot that is not short (historyPendingKey), and
// holds meanwhile what it cannot apply without it (hold.go).

func shortKey(id model.StreamID) string { return "link.history_short." + string(id) }

// ShortGiven is the instances the holder gave a short snapshot of a channel
// to and has not yet given a whole one.
func (s *Service) ShortGiven(ctx context.Context, id model.StreamID) []string {
	raw, found, err := s.store.Settings().Get(ctx, shortKey(id))
	var out []string
	if err != nil || !found || json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

// noteShort notes whether the snapshot the holder is giving an instance is
// short.
func (s *Service) noteShort(ctx context.Context, id model.StreamID, name string, short bool) {
	owedMu.Lock()
	defer owedMu.Unlock()
	list := s.ShortGiven(ctx, id)
	had := slices.Contains(list, name)
	switch {
	case short && !had:
		list = append(list, name)
	case !short && had:
		list = slices.DeleteFunc(list, func(n string) bool { return n == name })
	default:
		return
	}
	if len(list) == 0 {
		s.store.Settings().Delete(ctx, shortKey(id))
		return
	}
	s.store.Settings().Set(ctx, shortKey(id), list, "")
}

// tellShort tells each instance given a short snapshot of a channel whose
// earlier history is now complete to ask for it again; with a name, that
// instance only, which has just connected.
func (s *Service) tellShort(ctx context.Context, id model.StreamID, only string) {
	if s.historyPending(ctx, s.store, id) {
		return
	}
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil || st.State != store.StreamActive {
		return
	}
	for _, name := range s.ShortGiven(ctx, id) {
		if only != "" && name != only {
			continue
		}
		sv, err := s.store.Peers().ServerByName(ctx, name)
		if err != nil {
			continue
		}
		s.sendDirect(ctx, st.PeerID, sv, kHistoryMore, "", map[string]any{"stream_id": id})
	}
}

// tellAllShort is tellShort for every channel the holder keeps.
func (s *Service) tellAllShort(ctx context.Context, only string) {
	ids, err := s.Held(ctx)
	if err != nil {
		return
	}
	for _, id := range ids {
		if len(s.ShortGiven(ctx, id)) > 0 {
			s.tellShort(ctx, id, only)
		}
	}
}

// onHistoryMore is an instance told by its channel's home hub that the hub
// now holds the channel's earlier history whole: it asks for its snapshot
// again, if it still waits for it.
func (s *Service) onHistoryMore(ctx context.Context, m directMessage) {
	var b struct {
		StreamID model.StreamID `json:"stream_id"`
	}
	if s.holder || json.Unmarshal(m.raw, &b) != nil {
		return
	}
	st, err := s.store.Streams().Get(ctx, b.StreamID)
	if err != nil || st.State != store.StreamActive || st.PeerID != m.peer || !s.isHomeHub(st, m.from) {
		return
	}
	if !s.historyPending(ctx, s.store, st.ID) {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.askHistoryAgain(s.ctx, st.ID)
	}()
}

// askHistoryAgain asks a hub-owned channel's home for the snapshot a joining
// server is owed, once, as askFirstHistory does.
func (s *Service) askHistoryAgain(ctx context.Context, id model.StreamID) {
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil || st.State != store.StreamActive {
		return
	}
	hub, ok := s.homeHub(ctx, st)
	if !ok {
		return
	}
	rt := s.rt(id)
	ch := make(chan directMessage, 4)
	msg, err := s.sendDirectAwait(ctx, st.PeerID, hub, kHistoryRequest, map[string]any{"stream_id": id, "first": true}, ch)
	if err != nil {
		return
	}
	rt.mu.Lock()
	rt.firstAsked[msg] = hub.Name
	rt.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-time.After(30 * time.Second):
	case <-ch:
		time.Sleep(200 * time.Millisecond)
	}
	s.mu.Lock()
	delete(s.replies, msg)
	s.mu.Unlock()
}
