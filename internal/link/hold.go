package link

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file holds two things a server does with an envelope it has been
// delivered and cannot apply yet, so that every server of a stream ends with
// the same state whatever order things reached it in
// (docs/LINK-PROTOCOL.md section 13).
//
// 1. An event that refers to another event which has not arrived. A
// reaction, an edit, a deletion, a reply, a poll answer names a message. One
// origin's events reach a server in the order they were made, but two
// origins' events need not: between hubs each hub has its own log, a hub
// asks again for what it missed of one origin while it goes on taking
// another's, and a server that drops a delivery asks for it again in the
// same way. Such an event is **held** until the message it names arrives,
// and then applied; events held for the same message are applied in the
// order they came.
//
// The bounds, and what happens at them:
//
//   - Time: an event is held for at most holdWait (60 seconds). After that it
//     is rejected as it would have been at once (`unknown_message`): the
//     message it names is one this server was never going to be given.
//   - Count: a stream holds at most holdMax (256) events. When one more
//     comes, the one held longest is rejected in the same way.
//   - A restart: what is held is in memory only, and the counter this server
//     keeps of each origin ("I hold everything of yours up to n") does not
//     pass a held event. So after a restart the hub's log is asked for
//     everything above that counter, the held event comes again with what
//     followed it, and is held again from then if it must be. What followed
//     it and was applied the first time is recognised as held already.
//
// Only a stream that reaches this server through a hub holds anything. On a
// direct link there are two servers and one log, and whatever one of them
// refers to the other has already delivered.
//
// 2. An envelope under a key epoch from before this server was let in, made
// after it was let in: its origin had not heard of the new epoch yet (it was
// on the far side of a split, or its hub took the envelope in the moment the
// epoch changed elsewhere in the cluster). On one hub the hub refuses such an
// envelope and the origin sends it again under the new key; between hubs the
// origin's hub has taken it already. This server cannot read it and is owed
// it. It asks a server that holds that epoch's key to open it
// (`open_request`), and applies what comes back as if it had opened the
// envelope itself: the origin's signature on the envelope is checked here,
// the plaintext is vouched for by the server that opened it, as history is.
// If nobody opens it, the messages of that stretch are asked for as history.

// The bounds of holding. Tests shorten the wait.
const holdMax = 256

var holdWait atomic.Int64

func init() { holdWait.Store(int64(60 * time.Second)) }

// SetHoldWait changes how long an event is held for what it refers to, and
// returns a function that puts it back. It is for tests.
func SetHoldWait(d time.Duration) func() {
	old := holdWait.Swap(int64(d))
	return func() { holdWait.Store(old) }
}

// heldEvent is an envelope waiting for the message it refers to.
type heldEvent struct {
	env    wire.Envelope
	o      opened
	peer   model.PeerID
	target model.MessageID
	until  time.Time
	// rej is what it is rejected with if the message never comes.
	rej *rejection
}

// holdState is what one stream holds. It is touched only by the stream's
// own goroutine (streamRT.jobs).
type holdState struct {
	// events are what is held, longest first.
	events []*heldEvent
	// seen is, per origin, the highest counter delivered here while
	// something of that origin is held: the counter in the database stays
	// below the held event, and this says nothing has been missed since.
	seen  map[string]int64
	timer *time.Timer
}

func (h *holdState) has(eventID string) bool {
	for _, e := range h.events {
		if e.env.OriginEventID == eventID {
			return true
		}
	}
	return false
}

// lowest returns the lowest counter held of an origin, 0 if nothing is.
func (h *holdState) lowest(origin string) int64 {
	low := int64(0)
	for _, e := range h.events {
		if e.env.Origin == origin && (low == 0 || e.env.N < low) {
			low = e.env.N
		}
	}
	return low
}

// counterFor is what the database's counter of an origin may move to when an
// envelope with counter n has been settled: n itself when nothing of the
// origin is held; otherwise 0 (no change) while a held event is below it.
// The second result reports whether the envelope was noted in memory instead.
func (h *holdState) counterFor(origin string, n int64) (int64, bool) {
	if h == nil {
		return n, false
	}
	low := h.lowest(origin)
	if low == 0 || n < low {
		return n, false
	}
	if n > h.seen[origin] {
		h.seen[origin] = n
	}
	return 0, true
}

// released is the counter of an origin to write once an event of its has
// left the hold: just below the next one still held, or the highest
// delivered if none is.
func (h *holdState) released(origin string) int64 {
	top := h.seen[origin]
	if low := h.lowest(origin); low != 0 {
		return low - 1
	}
	delete(h.seen, origin)
	return top
}

// mayHold reports whether an envelope that was rejected for a message this
// server does not hold is to wait for it.
func (s *Service) mayHold(st store.Stream, rej *rejection) bool {
	return rej != nil && rej.missing != "" && st.Sequencer == store.SequencerHub
}

// park puts an envelope in the stream's hold. The one held longest gives way
// if the hold is full.
func (s *Service) park(ctx context.Context, rt *streamRT, st store.Stream, peer model.PeerID, env wire.Envelope, o opened, rej *rejection) {
	if rt.hold == nil {
		rt.hold = &holdState{seen: map[string]int64{}}
	}
	h := rt.hold
	if h.has(env.OriginEventID) {
		return // delivered again while it waits
	}
	if env.N > h.seen[env.Origin] {
		h.seen[env.Origin] = env.N
	}
	h.events = append(h.events, &heldEvent{env: env, o: o, peer: peer, target: rej.missing, until: time.Now().Add(time.Duration(holdWait.Load())), rej: rej})
	s.count(func(stats *Stats) { stats.Held++ })
	s.log.Debug("link: an event is held for the message it refers to", "stream", st.ID, "origin", env.Origin, "n", env.N, "type", o.in.Type, "message", rej.missing)
	for len(h.events) > holdMax {
		s.giveUp(ctx, rt, h.events[0])
	}
	s.armHold(rt)
}

// armHold sets the stream's timer for the event that has waited longest.
func (s *Service) armHold(rt *streamRT) {
	h := rt.hold
	if h.timer != nil {
		h.timer.Stop()
		h.timer = nil
	}
	if len(h.events) == 0 {
		return
	}
	wait := time.Until(h.events[0].until)
	if wait < 0 {
		wait = 0
	}
	h.timer = time.AfterFunc(wait, func() {
		select {
		case rt.jobs <- func() { s.expireHeld(s.ctx, rt) }:
		case <-s.ctx.Done():
		}
	})
}

// expireHeld rejects what has been held for as long as anything is.
func (s *Service) expireHeld(ctx context.Context, rt *streamRT) {
	h := rt.hold
	if h == nil {
		return
	}
	now := time.Now()
	for len(h.events) > 0 && !h.events[0].until.After(now) {
		s.giveUp(ctx, rt, h.events[0])
	}
	s.armHold(rt)
}

// unpark takes an event out of the hold.
func (h *holdState) unpark(e *heldEvent) {
	for i, x := range h.events {
		if x == e {
			h.events = append(h.events[:i], h.events[i+1:]...)
			return
		}
	}
}

// giveUp rejects a held event whose message did not come: the origin's
// counter moves past it, as it would have when it was delivered.
func (s *Service) giveUp(ctx context.Context, rt *streamRT, e *heldEvent) {
	h := rt.hold
	h.unpark(e)
	n := h.released(e.env.Origin)
	if n < e.env.N && h.lowest(e.env.Origin) == 0 {
		n = e.env.N
	}
	st, err := s.store.Streams().Get(ctx, model.StreamID(e.env.StreamID))
	if err != nil {
		return
	}
	if n > 0 && e.o.origin.ServerID != "" {
		if err := s.store.Streams().Applied(ctx, st.ID, e.o.origin.ServerID, n, 0); err != nil {
			s.log.Warn("link: a held event could not be let go", "stream", st.ID, "err", err)
		}
	}
	s.count(func(stats *Stats) { stats.HeldExpired++ })
	s.log.Debug("link: a held event's message never came", "stream", st.ID, "origin", e.env.Origin, "n", e.env.N, "type", e.o.in.Type, "message", e.target)
	s.noteRejected(ctx, st, e.env, e.rej)
}

// retryHeld applies the held events whose message has arrived, in the order
// they came, and goes round again while that lets in others (a reply that
// was held, and a reaction to that reply).
func (s *Service) retryHeld(ctx context.Context, rt *streamRT) {
	h := rt.hold
	if h == nil || len(h.events) == 0 {
		return
	}
	for progress := true; progress; {
		progress = false
		for _, e := range append([]*heldEvent(nil), h.events...) {
			if _, err := s.store.Messages().Get(ctx, e.target); err != nil {
				continue
			}
			st, err := s.store.Streams().Get(ctx, model.StreamID(e.env.StreamID))
			if err != nil || st.State != store.StreamActive || st.PeerID != e.peer {
				h.unpark(e) // the stream is over here, or has changed hands
				continue
			}
			h.unpark(e)
			if s.settleDelivered(ctx, rt, st, e.peer, e.env, e.o, true, e.until) {
				progress = true
			}
		}
	}
	s.armHold(rt)
}

// dropHold forgets everything a stream holds: the stream is over for this
// server.
func (s *Service) dropHold(rt *streamRT) {
	select {
	case rt.jobs <- func() {
		if rt.hold != nil {
			rt.hold.events = nil
			s.armHold(rt)
		}
	}:
	default:
	}
}

// TestHeld returns how many events a stream holds for messages that have
// not arrived. It is for tests.
func (s *Service) TestHeld(id model.StreamID) int {
	n := make(chan int, 1)
	rt := s.rt(id)
	select {
	case rt.jobs <- func() {
		if rt.hold == nil {
			n <- 0
			return
		}
		n <- len(rt.hold.events)
	}:
	case <-time.After(5 * time.Second):
		return -1
	}
	select {
	case v := <-n:
		return v
	case <-time.After(5 * time.Second):
		return -1
	}
}

// ---------------------------------------------- envelopes under an old key

const kOpenRequest, kOpened = "open_request", "opened"

// openWait is how long one server is given to open an envelope for this one.
const openWait = 5 * time.Second

// askOpen has an envelope this server cannot read, because it is under a key
// epoch from before this server was let in, opened by a server that holds
// that key. It reports whether one did, and fills in what the envelope says.
func (s *Service) askOpen(ctx context.Context, st store.Stream, env wire.Envelope, o *opened) bool {
	rt := s.rt(st.ID)
	rt.mu.Lock()
	failing := rt.openFailed
	rt.mu.Unlock()
	if failing.After(time.Now()) {
		return false // nobody could a moment ago; history will have to do
	}
	clean := env
	clean.Cursor, clean.Prev = 0, 0
	fields := map[string]any{"stream_id": st.ID, "envelope": clean}
	tried := 0
	for _, holder := range s.keyHolders(ctx, st, env.KeyEpoch) {
		if tried++; tried > 3 {
			break
		}
		m, err := s.ask(ctx, st.PeerID, holder, kOpenRequest, fields, openWait)
		if err != nil {
			continue
		}
		var b struct {
			Plain wire.Bytes `json:"plain"`
		}
		var in inner
		if json.Unmarshal(m.raw, &b) != nil {
			continue
		}
		// The plaintext comes as it was sealed: an Event message.
		text, err := wire.DecodeEvent(b.Plain)
		if err != nil || json.Unmarshal(text, &in) != nil || in.Type == "" {
			continue
		}
		in.raw = text
		o.in, o.rej = in, nil
		s.count(func(stats *Stats) { stats.Reopened++ })
		return true
	}
	// Nobody did. The next envelopes of the stretch are not asked about one
	// by one for a while: the stretch is asked for as history instead.
	rt.mu.Lock()
	rt.openFailed = time.Now().Add(30 * time.Second)
	rt.mu.Unlock()
	return false
}

// onOpenRequest opens, for a server of the stream, an envelope of the stream
// that this server has itself taken. It gives out nothing a server of the
// stream could not ask for as history.
func (s *Service) onOpenRequest(ctx context.Context, m directMessage) {
	var b struct {
		StreamID model.StreamID `json:"stream_id"`
		Envelope wire.Envelope  `json:"envelope"`
	}
	if json.Unmarshal(m.raw, &b) != nil || !b.Envelope.WellFormed() || b.Envelope.StreamID != string(b.StreamID) {
		s.replyError(ctx, m, wire.EBadFrame, "An open_request names a stream and carries one of its envelopes.")
		return
	}
	st, err := s.store.Streams().Get(ctx, b.StreamID)
	if err != nil || st.State != store.StreamActive || st.PeerID != m.peer {
		s.replyError(ctx, m, wire.EUnknownStream, "No such stream here.")
		return
	}
	active, _, err := s.activeServers(ctx, s.store, st.ID)
	if err != nil {
		return
	}
	in := false
	var origin store.StreamServer
	for _, ss := range active {
		in = in || ss.ServerID == m.from.ID
		if ss.Name == b.Envelope.Origin {
			origin = ss
		}
	}
	if !in {
		s.replyError(ctx, m, wire.ENotInStream, "That server is not in the stream.")
		return
	}
	env := b.Envelope
	// The envelope must be its origin's, and one this server has taken: it
	// opens nothing it would not have applied itself.
	key := s.keys.SignPublic()
	if env.Origin != s.id.Name {
		sv, err := s.store.Peers().Server(ctx, origin.ServerID)
		if origin.ServerID == "" || err != nil {
			s.replyError(ctx, m, wire.ENotInStream, "The envelope's origin is not in the stream.")
			return
		}
		key = sv.SignKey
	}
	if !env.Verify(key) {
		s.replyError(ctx, m, wire.EBadSignature, "The envelope's signature does not verify.")
		return
	}
	if _, seen, err := s.store.Streams().Seen(ctx, st.ID, env.OriginEventID); err != nil || !seen {
		s.replyError(ctx, m, wire.EUnavailable, "This server has not taken that envelope.")
		return
	}
	k, err := s.keyOf(ctx, s.store, st.ID, env.KeyEpoch)
	if err != nil {
		s.replyError(ctx, m, wire.ENotFound, "This server does not hold that epoch's key.")
		return
	}
	plain, err := env.Open(k.key)
	if err != nil {
		s.replyError(ctx, m, wire.EBadFrame, "The envelope does not open under its epoch's key.")
		return
	}
	s.sendDirect(ctx, m.peer, m.from, kOpened, m.MsgID, map[string]any{"stream_id": st.ID, "plain": wire.Bytes(plain)})
}

// unreadable notes an envelope of after this server was let in that nobody
// opened for it, and asks, when the stretch of them has gone quiet, for the
// messages of that stretch as history.
func (s *Service) unreadable(st store.Stream, env wire.Envelope) {
	rt := s.rt(st.ID)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.unreadFrom == 0 || env.At < rt.unreadFrom {
		rt.unreadFrom = env.At
	}
	if rt.unreadTimer != nil {
		rt.unreadTimer.Stop()
	}
	rt.unreadTimer = time.AfterFunc(2*time.Second, func() {
		rt.mu.Lock()
		from := rt.unreadFrom
		rt.unreadFrom, rt.unreadTimer = 0, nil
		rt.mu.Unlock()
		if from == 0 || s.ctx.Err() != nil {
			return
		}
		cur, err := s.store.Streams().Get(s.ctx, st.ID)
		if err != nil || cur.State != store.StreamActive {
			return
		}
		s.count(func(stats *Stats) { stats.HistoryAsked++ })
		s.recoverSince(s.ctx, cur, from-1)
	})
}
