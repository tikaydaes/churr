package link

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is a server's side of a cluster of hubs (docs/LINK-PROTOCOL.md
// section 23b, ruling R58). A server is connected to one hub of a cluster
// and may be in channels whose home is another. Two things reach it from
// its own hub for that: the cluster's roster, which says whose signature on
// a channel record it may take, and its clock against every other hub of
// the cluster, by which it stamps the events of a channel that hub is the
// home of.

// clusterState is what a server holds of clusters, per hub peer. The roster
// is kept in memory: a hub sends it after every ready, before any record.
// The clocks come with every ping and are also written down (homeClocksKey),
// as the clock against the hub itself is: a server that restarts stamps by
// the figures it last held, trusted a little less for their age, and does
// not have to wait for its hub's first ping before it can post in a channel
// homed on another hub.
type clusterState struct {
	clusters map[model.PeerID]wire.Cluster
	homes    map[model.PeerID]map[string]model.HomeClock
	hubNames map[model.PeerID]string
	// saved is what was last written of each peer's figures, and read
	// whether the written figures have been looked for since the start.
	saved map[model.PeerID]string
	read  map[model.PeerID]bool
}

// homeClocksKey is the setting under which a server keeps what a hub last
// told it of its clock against the other hubs of the hub's cluster: one
// value per hub peer, a JSON object from hub name to offset_ms,
// uncertainty_ms and measured_at.
func homeClocksKey(peer model.PeerID) string { return "link.home_clocks." + string(peer) }

// saveHomes writes a peer's figures down if they have changed.
func (s *Service) saveHomes(ctx context.Context, peer model.PeerID) {
	s.mu.Lock()
	homes := map[string]model.HomeClock{}
	for hub, c := range s.cluster.homes[peer] {
		homes[hub] = c
	}
	raw := string(mustJSON(homes))
	if s.cluster.saved == nil {
		s.cluster.saved = map[model.PeerID]string{}
	}
	before := s.cluster.saved[peer]
	s.cluster.saved[peer] = raw
	s.mu.Unlock()
	if before == raw || (before == "" && len(homes) == 0) {
		return // nothing new; or a hub that is in no cluster, of which nothing was ever kept
	}
	if err := s.store.Settings().Set(ctx, homeClocksKey(peer), homes, ""); err != nil && ctx.Err() == nil {
		s.log.Warn("link: the clocks a hub told could not be stored", "peer", peer, "err", err)
	}
}

// readHomes fills in, once after a start, the figures written down for a
// peer, for the hubs nothing has been heard about since.
func (s *Service) readHomes(ctx context.Context, peer model.PeerID) {
	s.mu.Lock()
	if s.cluster.read == nil {
		s.cluster.read = map[model.PeerID]bool{}
	}
	done := s.cluster.read[peer]
	s.cluster.read[peer] = true
	s.mu.Unlock()
	if done {
		return
	}
	raw, found, err := s.store.Settings().Get(ctx, homeClocksKey(peer))
	var kept map[string]model.HomeClock
	if err != nil || !found || json.Unmarshal(raw, &kept) != nil {
		if err != nil {
			// Not read: it is looked for again next time.
			s.mu.Lock()
			s.cluster.read[peer] = false
			s.mu.Unlock()
		}
		return
	}
	s.mu.Lock()
	if s.cluster.homes == nil {
		s.cluster.homes = map[model.PeerID]map[string]model.HomeClock{}
		s.cluster.hubNames = map[model.PeerID]string{}
	}
	if s.cluster.homes[peer] == nil {
		s.cluster.homes[peer] = map[string]model.HomeClock{}
	}
	for hub, c := range kept {
		if _, fresh := s.cluster.homes[peer][hub]; !fresh {
			s.cluster.homes[peer][hub] = c
		}
	}
	s.mu.Unlock()
}

// onCluster takes the roster of the cluster a hub is in. It is refused
// whole, and the roster held before stays, if any entry in it is not what
// it claims, if it does not list the hub that sent it with the key pinned
// for that hub, or if it gives a hub this server has met another key.
func (s *Service) onCluster(ctx context.Context, pc *peerConn, f wire.Frame) error {
	if !pc.hub {
		return wire.Errf(wire.EBadFrame, "Only a hub sends a cluster roster.")
	}
	refuse := func(why string, err error) error {
		s.count(func(st *Stats) { st.Dropped["cluster_refused"]++ })
		s.audit(ctx, s.store, "", "link.cluster_refused", "peer", string(pc.peer.ID), map[string]any{"hub": pc.peer.HubName, "why": why})
		return err
	}
	c, err := wire.VerifyCluster(f.Cluster)
	if err != nil {
		return refuse("an entry is not what it claims", err)
	}
	if !bytes.Equal(c.Key(pc.peer.HubName), pc.peer.HubSignKey) {
		return refuse("it does not list the hub that sent it", wire.Errf(wire.EBadSignature, "The roster does not list %s with its pinned key.", pc.peer.HubName))
	}
	for name, m := range c.Members {
		known, err := s.store.Peers().ServerByName(ctx, name)
		if err == nil && len(known.SignKey) > 0 && !bytes.Equal(known.SignKey, m.SignKey) {
			return refuse("it gives "+name+" another key than the one pinned", wire.Errf(wire.EKeyMismatch, "%s is pinned with another key.", name))
		}
	}
	s.mu.Lock()
	if s.cluster.clusters == nil {
		s.cluster.clusters = map[model.PeerID]wire.Cluster{}
	}
	s.cluster.clusters[pc.peer.ID] = c
	s.mu.Unlock()
	return nil
}

// clusterHubKey returns the key of a hub of the cluster a hub peer is in:
// the peer's own hub always, and any other the roster it sent lists.
func (s *Service) clusterHubKey(peer store.Peer, hub string) []byte {
	if hub == peer.HubName {
		return peer.HubSignKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cluster.clusters[peer.ID].Key(hub)
}

// clusterHubBox returns the box key of a hub of the cluster a hub peer is
// in, as the roster it sent lists it under that hub's own signature: what a
// direct message to that hub is sealed to. Nil until the roster has come.
func (s *Service) clusterHubBox(peer store.Peer, hub string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.cluster.clusters[peer.ID].Members[hub]
	if !ok || (hub == peer.HubName && !bytes.Equal(m.SignKey, peer.HubSignKey)) {
		return nil
	}
	return m.BoxKey
}

// hubRow returns the row that stands in `servers` for a hub of a peer's
// cluster: the owner of the channels that hub is the home of.
func (s *Service) hubRow(ctx context.Context, a store.Areas, peer store.Peer, hub string) (store.Server, error) {
	key := s.clusterHubKey(peer, hub)
	if hub == "" || hub == peer.HubName || len(key) == 0 {
		return s.hubServer(ctx, a, peer)
	}
	sv, _, err := a.Peers().PinServer(ctx, store.Server{Name: hub, SignKey: key})
	return sv, err
}

// noteHomes keeps what a hub told this server of its clock against the
// other hubs of the cluster. It reports whether any figure moved.
func (s *Service) noteHomes(pc *peerConn, homes map[string]model.HomeClock) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cluster.homes == nil {
		s.cluster.homes = map[model.PeerID]map[string]model.HomeClock{}
		s.cluster.hubNames = map[model.PeerID]string{}
	}
	s.cluster.hubNames[pc.peer.ID] = pc.peer.HubName
	moved := false
	old := s.cluster.homes[pc.peer.ID]
	for hub, c := range homes {
		if before, ok := old[hub]; !ok || before.Offset != c.Offset {
			moved = true
		}
	}
	if homes != nil {
		s.cluster.homes[pc.peer.ID] = homes
		// What a ping says replaces what was written down, whole: a hub
		// that has left the cluster is no longer in it.
		if s.cluster.read == nil {
			s.cluster.read = map[model.PeerID]bool{}
		}
		s.cluster.read[pc.peer.ID] = true
	}
	return moved
}

// awayHome reports whether a stream's home hub is another hub than the one
// this server reaches it through, and names it.
func (s *Service) awayHome(st store.Stream) (string, bool) {
	if st.HomeHub == "" || st.Sequencer != store.SequencerHub {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	name, known := s.cluster.hubNames[st.PeerID]
	return st.HomeHub, known && name != st.HomeHub
}

// toldFor is what this server holds of its clock against a stream's time
// reference: what the peer told it, and for a channel whose home is another
// hub of the peer's cluster, the figure the peer composed for that hub.
// Those figures are written down as they are told (saveHomes), and read back
// after a restart.
func (s *Service) toldFor(ctx context.Context, st store.Stream) (model.PeerClock, bool) {
	c, ok := s.toldBy(ctx, s.referencePeer(st))
	if !ok {
		return c, false
	}
	if st.HomeHub == "" || st.Sequencer != store.SequencerHub {
		return c, true
	}
	s.mu.Lock()
	name, known := s.cluster.hubNames[st.PeerID]
	hc, composed := s.cluster.homes[st.PeerID][st.HomeHub]
	s.mu.Unlock()
	if !composed {
		// Nothing heard of that hub since this server started: what was
		// written down before it stopped.
		s.readHomes(ctx, st.PeerID)
		s.mu.Lock()
		hc, composed = s.cluster.homes[st.PeerID][st.HomeHub]
		s.mu.Unlock()
	}
	if !known {
		// Nothing has been heard from the hub since this server started.
		// The hub's name is in the peer's row.
		if p, err := s.store.Peers().Get(ctx, st.PeerID); err == nil {
			name = p.HubName
		}
	}
	if name == st.HomeHub {
		return c, true
	}
	if !composed {
		return model.PeerClock{}, false
	}
	c.Offset, c.Uncertainty, c.MeasuredAt = hc.Offset, hc.Uncertainty, hc.MeasuredAt
	return c, true
}

// setToldFor keeps the figure a refusal carried: what the hub holds of this
// server's clock against the stream's reference, now.
func (s *Service) setToldFor(ctx context.Context, pc *peerConn, st store.Stream, c model.PeerClock) {
	if home, away := s.awayHome(st); away {
		s.mu.Lock()
		if s.cluster.homes[pc.peer.ID] == nil {
			s.cluster.homes[pc.peer.ID] = map[string]model.HomeClock{}
		}
		s.cluster.homes[pc.peer.ID][home] = model.HomeClock{Offset: c.Offset, Uncertainty: c.Uncertainty, MeasuredAt: c.MeasuredAt}
		s.mu.Unlock()
		s.saveHomes(ctx, pc.peer.ID)
		return
	}
	s.store.Peers().SetClockTold(ctx, pc.peer.ID, c)
	s.noteTold(pc.peer.ID, c)
}
