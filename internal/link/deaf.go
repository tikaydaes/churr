package link

import (
	"context"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Deaf ears across servers (ruling R77, docs/LINK-PROTOCOL.md section 17a).
// When someone here has blocked someone of another server, that server is
// sent, in a direct message to it alone, which of its people each account
// here has blocked; it shows them that account online and nothing more,
// and passes on none of its typing. A hub cannot read the message, the
// other servers of a channel never see it, and the receiver holds it in
// memory only.
//
// Each deaf_ears message is everything the sender has for the receiver,
// whole: it replaces what came before, the newest by `at` standing. It is
// sent when a block of one of the receiver's accounts is made or lifted,
// to each server sharing a stream when a connection comes up (asking for
// theirs in turn, so a server that restarted is told again even when the
// other side's connection never dropped), to a server joining a stream,
// and to every server sharing a stream when blocking is turned on or off.

const kDeafEars = "deaf_ears"

// deafEars is one account of the sender and the receiver's accounts it has
// blocked.
type deafEars struct {
	AccountID  model.AccountID   `json:"account_id"`
	AccountIDs []model.AccountID `json:"account_ids"`
}

// deafFrom is what one server last said: its accounts, by the accounts
// here each has blocked.
type deafFrom struct {
	at    model.Time
	pairs map[model.AccountID]map[model.AccountID]bool
}

// deafEarsFor is what this server tells another: every block a local
// account has made of one of that server's accounts, while blocking is on.
func (s *Service) deafEarsFor(ctx context.Context, server model.ServerID) ([]deafEars, error) {
	if on, err := store.SettingBool(ctx, s.store, store.SettingBlockingEnabled, true); err != nil || !on {
		return []deafEars{}, err
	}
	blocks, err := s.store.Blocks().OfServer(ctx, server)
	if err != nil {
		return nil, err
	}
	out := []deafEars{}
	for _, b := range blocks {
		if n := len(out); n > 0 && out[n-1].AccountID == b.Account {
			out[n-1].AccountIDs = append(out[n-1].AccountIDs, b.Blocked)
			continue
		}
		out = append(out, deafEars{AccountID: b.Account, AccountIDs: []model.AccountID{b.Blocked}})
	}
	return out, nil
}

// sendDeafEars sends a server everything this server has for it, over a
// peer that reaches it. With ask, the receiver answers with its own.
func (s *Service) sendDeafEars(ctx context.Context, peer model.PeerID, sv store.Server, ask bool, re string) {
	if s.holder {
		return // a hub's holder has no accounts, and nobody has blocked anyone of its
	}
	if s.isSelf(sv.ID) || sv.Blocked() {
		return
	}
	list, err := s.deafEarsFor(ctx, sv.ID)
	if err != nil {
		s.log.Warn("link: the blocks of another server's accounts could not be read; it was not told of them", "server", sv.Name, "err", err)
		return
	}
	fields := map[string]any{"deaf_ears": list}
	if ask {
		fields["ask"] = true
	}
	s.sendDirect(ctx, peer, sv, kDeafEars, re, fields)
}

// BlockChanged is told when a local account blocks or unblocks someone. A
// block of an account of another server is sent to that server, over the
// first connected peer that reaches it; when none is connected it goes when
// one connects.
func (s *Service) BlockChanged(ctx context.Context, blocked model.AccountID) {
	acc, err := s.store.Accounts().Get(ctx, blocked)
	if err != nil || acc.Local() {
		return
	}
	sv, err := s.store.Peers().Server(ctx, acc.ServerID)
	if err != nil {
		return
	}
	s.mu.Lock()
	conns := make([]*peerConn, 0, len(s.conns))
	for _, pc := range s.conns {
		conns = append(conns, pc)
	}
	s.mu.Unlock()
	for _, pc := range conns {
		if s.reaches(ctx, pc.peer, sv.ID) {
			s.sendDeafEars(ctx, pc.peer.ID, sv, false, "")
			return
		}
	}
}

// BlockingChanged is told when blocking is turned on or off for this
// server: every server sharing a stream is told what now holds.
func (s *Service) BlockingChanged(ctx context.Context) {
	s.mu.Lock()
	peers := make([]model.PeerID, 0, len(s.conns))
	for id := range s.conns {
		peers = append(peers, id)
	}
	s.mu.Unlock()
	for _, p := range peers {
		s.deafEarsToPeer(ctx, p, false)
	}
}

// deafEarsToPeer sends deaf_ears to every server that shares an active
// stream with this one over a peer: after a connection comes up (with ask),
// and when blocking is turned on or off.
func (s *Service) deafEarsToPeer(ctx context.Context, peer model.PeerID, ask bool) {
	streams, err := s.store.Streams().List(ctx, store.StreamActive)
	if err != nil {
		return
	}
	told := map[model.ServerID]bool{}
	for _, st := range streams {
		if st.PeerID != peer {
			continue
		}
		active, _, err := s.activeServers(ctx, s.store, st.ID)
		if err != nil {
			continue
		}
		for _, ss := range active {
			if told[ss.ServerID] || s.isSelf(ss.ServerID) {
				continue
			}
			told[ss.ServerID] = true
			if sv, err := s.store.Peers().Server(ctx, ss.ServerID); err == nil {
				s.sendDeafEars(ctx, peer, sv, ask, "")
			}
		}
	}
}

// deafEarsOnJoin is told of the servers that have just joined a stream: each
// is sent what this server has for it. When this server is the one that
// joined, every other server of the stream is.
func (s *Service) deafEarsOnJoin(ctx context.Context, id model.StreamID, joined []string) {
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil || st.State != store.StreamActive {
		return
	}
	var to []model.ServerID
	for _, name := range joined {
		if name == s.id.Name {
			active, _, err := s.activeServers(ctx, s.store, id)
			if err != nil {
				return
			}
			for _, ss := range active {
				to = append(to, ss.ServerID)
			}
			continue
		}
		if sv, err := s.store.Peers().ServerByName(ctx, name); err == nil {
			to = append(to, sv.ID)
		}
	}
	told := map[model.ServerID]bool{}
	for _, id := range to {
		if told[id] || s.isSelf(id) {
			continue
		}
		told[id] = true
		if sv, err := s.store.Peers().Server(ctx, id); err == nil {
			s.sendDeafEars(ctx, st.PeerID, sv, false, "")
		}
	}
}

// onDeafEars takes what another server says its accounts have blocked of
// this server's. It replaces whatever that server said before, unless it is
// older. Only this server's own accounts are kept; whether each blocker's
// home is the sender is asked when it is used (DeafViewers), since a
// blocker may not be known here yet. Whoever is shown something different
// because of it is told.
func (s *Service) onDeafEars(ctx context.Context, m directMessage) {
	var b struct {
		DeafEars []deafEars `json:"deaf_ears"`
		Ask      bool       `json:"ask"`
	}
	if json.Unmarshal(m.raw, &b) != nil {
		s.count(func(st *Stats) { st.Dropped["deaf_ears_malformed"]++ })
		return
	}
	pairs := map[model.AccountID]map[model.AccountID]bool{}
	for _, d := range b.DeafEars {
		for _, v := range d.AccountIDs {
			acc, err := s.store.Accounts().Get(ctx, v)
			if err != nil || !acc.Local() || d.AccountID == "" {
				continue
			}
			if pairs[d.AccountID] == nil {
				pairs[d.AccountID] = map[model.AccountID]bool{}
			}
			pairs[d.AccountID][v] = true
		}
	}
	s.mu.Lock()
	if s.deaf == nil {
		s.deaf = map[model.ServerID]deafFrom{}
	}
	old, had := s.deaf[m.from.ID]
	stale := had && m.At.Before(old.at)
	if !stale {
		s.deaf[m.from.ID] = deafFrom{at: m.At, pairs: pairs}
	}
	tell := s.onDeaf
	s.mu.Unlock()
	if b.Ask {
		s.sendDeafEars(ctx, m.peer, m.from, false, m.MsgID)
	}
	if stale || tell == nil {
		return
	}
	for blocker, viewers := range pairs {
		for v := range viewers {
			if !old.pairs[blocker][v] {
				tell(ctx, blocker, v, true)
			}
		}
	}
	for blocker, viewers := range old.pairs {
		for v := range viewers {
			if !pairs[blocker][v] {
				tell(ctx, blocker, v, false)
			}
		}
	}
}

// OnDeafEars sets who is told when a block made on another server starts or
// stops applying to one of this server's accounts: the people service,
// which tells that account what it is now shown. It is set once while the
// server is being put together.
func (s *Service) OnDeafEars(fn func(ctx context.Context, blocker, viewer model.AccountID, on bool)) {
	s.mu.Lock()
	s.onDeaf = fn
	s.mu.Unlock()
}

// DeafViewers is the accounts of this server that an account of another
// server has blocked there, as its home server said: they are shown it
// online and nothing more, and none of its typing. It is nil for nearly
// everyone, and costs a map lookup then.
func (s *Service) DeafViewers(blocker model.AccountID) map[model.AccountID]bool {
	s.mu.Lock()
	var claims map[model.ServerID]map[model.AccountID]bool
	for from, d := range s.deaf {
		if v := d.pairs[blocker]; len(v) > 0 {
			if claims == nil {
				claims = map[model.ServerID]map[model.AccountID]bool{}
			}
			claims[from] = v
		}
	}
	s.mu.Unlock()
	if claims == nil {
		return nil
	}
	// Only the blocker's own server can say whom it has blocked.
	acc, err := s.store.Accounts().Get(s.ctx, blocker)
	if err != nil || acc.Local() || claims[acc.ServerID] == nil {
		return nil
	}
	set := claims[acc.ServerID]
	out := make(map[model.AccountID]bool, len(set))
	for v := range set {
		out[v] = true
	}
	return out
}
