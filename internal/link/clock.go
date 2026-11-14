package link

import (
	"context"
	"errors"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is a server's part in the shared clock of ruling R57
// (docs/LINK-PROTOCOL.md sections 5 and 13). Every shared conversation has a
// time reference: the owner server on a direct link, the home hub on a hub.
// The reference measures each connected server's clock in the connection's
// ping and pong (wire/clock.go) and tells the server its offset on the next
// ping. Two things follow for a server:
//
//   - It answers pings, and keeps what each peer told it (peers.clock_told).
//     One offset per peer, whatever the number of conversations, kept across
//     disconnections and restarts.
//   - On a direct link it is itself the reference of the streams it owns, so
//     it measures the other server too, and keeps that
//     (peers.clock_measured). Both servers of a direct link do both.

// nowMs is this server's clock in milliseconds.
func (s *Service) nowMs() int64 { return int64(s.now()) }

// startMeasuring begins pinging the other server of a direct link. A hub is
// never measured by a server: the hub is the reference there.
func (s *Service) startMeasuring(pc *peerConn) {
	if pc.hub {
		return
	}
	peer := pc.peer.ID
	var prior *model.PeerClock
	if c, ok := s.measuredOf(s.ctx, peer); ok {
		prior = &c
	}
	track := wire.NewClockTrack(s.clockSynced, prior)
	pc.pinger = wire.NewPinger(pc.conn, s.nowMs, track, func(c wire.Clock) {
		if err := s.store.Peers().SetClockMeasured(s.ctx, peer, c); err != nil && s.ctx.Err() == nil {
			s.log.Warn("link: a measured clock could not be stored", "peer", pc.name, "err", err)
		}
		s.noteMeasured(peer, c)
	})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		pc.pinger.Run()
	}()
}

// onPing answers a ping and keeps what it says about this server's clock.
// t1 is when the ping arrived.
func (s *Service) onPing(ctx context.Context, pc *peerConn, f wire.Frame, t1 int64) {
	if err := pc.conn.AnswerPing(f, t1, s.nowMs); err != nil {
		var bad *wire.Error
		if errors.As(err, &bad) {
			pc.conn.Send(bad.Frame(f.ID))
		}
		return
	}
	if f.Clock == nil {
		return
	}
	// In a cluster the hub also says this server's clock against every
	// other hub: a channel is stamped by its home hub's clock.
	homesMoved := pc.hub && s.noteHomes(pc, f.Homes)
	if pc.hub && f.Homes != nil {
		s.saveHomes(ctx, pc.peer.ID)
	}
	c := *f.Clock
	before, had := s.toldBy(ctx, pc.peer.ID)
	if err := s.store.Peers().SetClockTold(ctx, pc.peer.ID, c); err != nil && ctx.Err() == nil {
		s.log.Warn("link: the clock a peer told could not be stored", "peer", pc.name, "err", err)
	}
	s.noteTold(pc.peer.ID, c)
	if !had || before.Offset != c.Offset || homesMoved {
		// Events that were refused for their time have been waiting for
		// exactly this.
		go s.clockMoved(s.ctx, pc.peer.ID)
	}
}

// clockMoved stamps again, with the offset just told, the events that a
// reference refused for their time while this server's clock was not what
// the reference had measured.
func (s *Service) clockMoved(ctx context.Context, peer model.PeerID) {
	streams, err := s.store.Streams().List(ctx, store.StreamActive)
	if err != nil {
		return
	}
	for _, st := range streams {
		if s.referencePeer(st) != peer {
			continue
		}
		rt := s.rt(st.ID)
		rt.mu.Lock()
		waiting := rt.awaitClock
		rt.awaitClock = false
		rt.mu.Unlock()
		if !waiting {
			continue
		}
		rows, err := s.outboxOf(ctx, st.ID)
		if err != nil || len(rows) == 0 {
			continue
		}
		s.log.Debug("link: the reference has measured this server's clock again; stamping again", "stream", st.ID, "waiting", len(rows))
		if err := s.restamp(ctx, st, rows[0].rec); err != nil {
			s.log.Warn("link: a refused event could not be stamped again", "stream", st.ID, "err", err)
			continue
		}
		s.count(func(stats *Stats) { stats.Restamped++ })
		s.kickStream(ctx, st.ID, true)
	}
}

// noteTold and noteMeasured keep the newest figures in memory, where
// stamping and checking read them without a query.
func (s *Service) noteTold(peer model.PeerID, c model.PeerClock) {
	s.mu.Lock()
	s.told[peer] = c
	s.mu.Unlock()
}

func (s *Service) noteMeasured(peer model.PeerID, c model.PeerClock) {
	s.mu.Lock()
	s.measured[peer] = c
	s.mu.Unlock()
}

// toldBy returns what a peer, as a time reference, last told this server
// about its clock: from memory, or from the peer's row after a restart.
func (s *Service) toldBy(ctx context.Context, peer model.PeerID) (model.PeerClock, bool) {
	s.mu.Lock()
	c, ok := s.told[peer]
	hidden := s.testNoClock[peer]
	s.mu.Unlock()
	if hidden {
		return model.PeerClock{}, false
	}
	if ok {
		return c, true
	}
	p, err := s.store.Peers().Get(ctx, peer)
	if err != nil || p.ClockTold == nil {
		return model.PeerClock{}, false
	}
	s.noteTold(peer, *p.ClockTold)
	return *p.ClockTold, true
}

// measuredOf returns what this server, as a time reference, last measured
// of a direct peer's clock.
func (s *Service) measuredOf(ctx context.Context, peer model.PeerID) (model.PeerClock, bool) {
	s.mu.Lock()
	c, ok := s.measured[peer]
	s.mu.Unlock()
	if ok {
		return c, true
	}
	p, err := s.store.Peers().Get(ctx, peer)
	if err != nil || p.ClockMeasured == nil {
		return model.PeerClock{}, false
	}
	s.noteMeasured(peer, *p.ClockMeasured)
	return *p.ClockMeasured, true
}

// noteAway records, when a direct peer connects, the stretch it was
// disconnected for: from when this server last measured its clock to now, by
// this server's clock. An event the peer made in that stretch arrives with
// an old `at`, which the 30-second rule would otherwise refuse (section 13).
// The last few stretches are kept with the peer's row, so a restart of this
// server in the middle of the peer's backlog does not strand it.
func (s *Service) noteAway(ctx context.Context, pc *peerConn) {
	if pc.hub {
		return
	}
	c, ok := s.measuredOf(ctx, pc.peer.ID)
	if !ok {
		return // never connected long enough to be measured: nothing was made
	}
	now := s.nowMs()
	from := int64(c.MeasuredAt)
	if from > now {
		from = now // this server's own clock was set back in between
	}
	if err := s.store.Peers().AddAway(ctx, pc.peer.ID, from, now, awayKept); err != nil && ctx.Err() == nil {
		s.log.Warn("link: a stretch of disconnection could not be noted", "peer", pc.name, "err", err)
	}
}

// wasAway reports whether a time, by this server's clock, falls in a stretch
// a direct peer was disconnected for, give or take the 30 seconds any `at`
// is allowed. A stretch stops counting a day after it ended.
func (s *Service) wasAway(ctx context.Context, peer model.PeerID, at int64) bool {
	p, err := s.store.Peers().Get(ctx, peer)
	if err != nil {
		return false
	}
	now := s.nowMs()
	for _, w := range p.Away {
		if now-w[1] <= int64(awayLife/time.Millisecond) && at >= w[0]-clockSlack && at <= w[1]+clockSlack {
			return true
		}
	}
	return false
}
