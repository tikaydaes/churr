package live

import (
	"bytes"
	"encoding/json"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// Presence under pressure (ruling R62).
//
// Presence is state, not a log: only the newest value for an account
// matters. So when many people's presence changes at once (a server
// restart, everyone arriving at nine) each connection is not sent one frame
// per change. The changes wait a moment in the connection, the newest for
// each account replacing any older one, and go out together as one
// `presence.batch` frame (docs/EVENTS.md section 9).
//
// Under normal load nothing waits: a change is sent the moment it happens,
// as `presence.changed`. How long changes wait is worked out from two things
// the server sees of itself, never from how many members it has:
//
//   - how many presence frames a second the changes of the last second
//     would have cost sent one by one (changes times the connections each
//     goes to), over the whole server;
//   - how full this one connection's send queue is.
//
// Either grows the wait from nothing up to presenceCap and lets it fall back
// to nothing when the pressure has gone. Messages, edits, reactions and
// every other event never pass through here: they stay one frame each, in
// order.
//
// Who is sent what is decided before any of this, by whoever calls
// SendPresence, and nothing here widens it: a connection's batch is built
// from the changes that were addressed to that connection and from nothing
// else. Connections that were addressed exactly the same changes (everyone
// in one large channel, typically) are handed the same bytes, which is what
// keeps a storm from costing memory for every connection separately.

const (
	// presenceCalm is how many presence frames a second the server sends
	// one by one without holding any back.
	presenceCalm = 5000
	// presenceStep is the wait for each presenceCalm frames a second of
	// load: 5,000 a second waits 100 ms, 25,000 half a second.
	presenceStep = 100 * time.Millisecond
	// presenceCap is the longest a presence change waits to be sent.
	presenceCap = 1500 * time.Millisecond
	// A connection's queue holds presence back from a quarter full, and
	// for the whole of presenceCap from three quarters.
	fillCalm, fillFull = 0.25, 0.75
	// presenceBatchMax is the most changes in one frame. A frame may be
	// 1 MiB and a change is some 150 bytes, so this leaves plenty of room.
	presenceBatchMax = 2000
)

// EventPresenceBatch is the frame that carries several presence changes at
// once: `changes` is a list of presence.changed objects, one per account.
const EventPresenceBatch = "presence.batch"

// presenceLoad measures the presence frames a second the server has been
// asked for over the last second, in tenths of a second.
type presenceLoad struct {
	mu    sync.Mutex
	slots [10]int64
	// at is the tenth of a second slots[at%10] counts.
	at int64
}

// advance forgets the tenths that have passed. The caller holds l.mu.
func (l *presenceLoad) advance(now time.Time) {
	t := now.UnixNano() / int64(100*time.Millisecond)
	if t-l.at >= int64(len(l.slots)) {
		l.slots = [10]int64{}
		l.at = t
	}
	for l.at < t {
		l.at++
		l.slots[l.at%int64(len(l.slots))] = 0
	}
}

// add counts n frames.
func (l *presenceLoad) add(now time.Time, n int) {
	l.mu.Lock()
	l.advance(now)
	l.slots[l.at%int64(len(l.slots))] += int64(n)
	l.mu.Unlock()
}

// perSecond is how many frames were counted in the last second.
func (l *presenceLoad) perSecond(now time.Time) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.advance(now)
	var n int64
	for _, v := range l.slots {
		n += v
	}
	return n
}

// presenceWait is how long a presence change waits before it is sent, from
// the server-wide load alone: nothing while the load is calm, then a step
// for every presenceCalm frames a second, up to the cap.
func (h *Hub) presenceWait(now time.Time) time.Duration {
	calm, step, limit := h.opts.presenceCalm, h.opts.presenceStep, h.opts.presenceCap
	rate := h.load.perSecond(now)
	if rate <= calm {
		return 0
	}
	wait := time.Duration(float64(step) * float64(rate) / float64(calm))
	if wait > limit {
		wait = limit
	}
	return wait
}

// PresenceStats counts what the hub has done with presence since it
// started, for tests and the overview.
type PresenceStats struct {
	// Changes is how many presence changes the hub was given, and
	// Deliveries how many connection-and-change pairs that came to.
	Changes, Deliveries int64
	// Frames is how many frames carried them: one per delivery under
	// normal load, far fewer under pressure. Batches is how many of those
	// frames were presence.batch.
	Frames, Batches int64
	// Merged is how many deliveries were never sent because a newer value
	// for the same account replaced them while they waited.
	Merged int64
}

type presenceCounters struct {
	changes, deliveries, frames, batches, merged atomic.Int64
}

// PresenceStats returns the counts.
func (h *Hub) PresenceStats() PresenceStats {
	return PresenceStats{Changes: h.presStats.changes.Load(), Deliveries: h.presStats.deliveries.Load(),
		Frames: h.presStats.frames.Load(), Batches: h.presStats.batches.Load(), Merged: h.presStats.merged.Load()}
}

// PresenceWait reports how long presence changes are being held back right
// now by the server-wide load: zero under normal load.
func (h *Hub) PresenceWait() time.Duration { return h.presenceWait(time.Now()) }

// presenceItem is one presence change as it is sent: the account it is
// about and the presence.changed frame, encoded once for everyone.
type presenceItem struct {
	about model.AccountID
	frame []byte
}

// presenceQueue is the hub's side of holding presence back: the connections
// with something waiting, and the one timer that sends it.
type presenceQueue struct {
	// mu is held while a change is handed to its connections and while
	// waiting changes are sent, so the two never interleave: connections
	// addressed the same changes then hold the same list, and can share
	// one encoded batch. It is taken before any connection's own lock.
	mu      sync.Mutex
	waiting []*conn
	timer   *time.Timer
	// due is when the timer will fire; zero when it is not running.
	due time.Time
}

// SendPresence sends one presence.changed event about one account to every
// connection of the accounts in to. v is the event: anything that marshals
// to the JSON object of docs/EVENTS.md section 9. An account named twice in
// to is told once.
//
// Under normal load the frame goes out at once. Under pressure it waits in
// each connection, where a newer change for the same account replaces it,
// and leaves in a presence.batch frame (see the top of this file). The
// caller decides who may see the account; this only delivers.
func (h *Hub) SendPresence(to []model.AccountID, about model.AccountID, v any) {
	if len(to) == 0 {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		h.opts.Log.Error("a presence event could not be encoded", "err", err)
		return
	}
	item := &presenceItem{about: about, frame: b}
	// Every connection is found under one hold of the lock: with five
	// thousand recipients, taking it for each was most of the work.
	conns := make([]*conn, 0, len(to))
	h.mu.RLock()
	for _, id := range to {
		for c := range h.conns[id] {
			conns = append(conns, c)
		}
	}
	h.mu.RUnlock()
	h.presStats.changes.Add(1)
	if len(conns) == 0 {
		return
	}
	q := &h.presQueue
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	// The wait is read before this change is counted, so a change that
	// comes alone is never held back by its own size.
	wait := h.presenceWait(now)
	h.load.add(now, len(conns))
	h.presStats.deliveries.Add(int64(len(conns)))
	var earliest time.Time
	for _, c := range conns {
		if due, queued := c.sendPresence(item, now, wait); queued {
			q.waiting = append(q.waiting, c)
			if earliest.IsZero() || due.Before(earliest) {
				earliest = due
			}
		}
	}
	if !earliest.IsZero() {
		h.armPresenceLocked(now, earliest)
	}
}

// armPresenceLocked makes sure the timer fires by due. The caller holds the
// queue's lock.
func (h *Hub) armPresenceLocked(now, due time.Time) {
	q := &h.presQueue
	if !q.due.IsZero() && !due.Before(q.due) {
		return // it fires soon enough already
	}
	q.due = due
	wait := due.Sub(now)
	if wait < time.Millisecond {
		wait = time.Millisecond
	}
	if q.timer == nil {
		q.timer = time.AfterFunc(wait, h.flushPresence)
	} else {
		q.timer.Reset(wait)
	}
}

// builtBatch is the frames made for one list of waiting changes, kept for
// the length of one flush so the next connection holding the very same list
// is handed the same bytes.
type builtBatch struct {
	items  []*presenceItem
	frames [][]byte
	merged int
}

// flushPresence sends what is waiting in every connection whose time has
// come, and sets the timer again for the rest.
func (h *Hub) flushPresence() {
	q := &h.presQueue
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	q.due = time.Time{}
	waiting := q.waiting
	q.waiting = nil
	var built []builtBatch
	var earliest time.Time
	for _, c := range waiting {
		c.mu.Lock()
		switch {
		case c.closed || len(c.pending) == 0:
			// Gone, or sent already when its catch-up ended.
			c.pendingListed = false
		case c.pendingDue.After(now):
			q.waiting = append(q.waiting, c)
			if earliest.IsZero() || c.pendingDue.Before(earliest) {
				earliest = c.pendingDue
			}
		default:
			c.pendingListed = false
			built = c.flushPresenceLocked(built)
		}
		c.mu.Unlock()
	}
	if !earliest.IsZero() {
		h.armPresenceLocked(now, earliest)
	}
}

// presenceOf picks a presence.changed event out of what Send was given, so
// that presence reaches the same path whoever sends it.
func presenceOf(v any) (model.AccountID, bool) {
	var e model.Event
	switch x := v.(type) {
	case model.Event:
		e = x
	case *model.Event:
		if x == nil {
			return "", false
		}
		e = *x
	default:
		return "", false
	}
	if e.Type != model.EventPresenceChanged {
		return "", false
	}
	switch id := e.Payload["account_id"].(type) {
	case model.AccountID:
		return id, id != ""
	case string:
		return model.AccountID(id), id != ""
	}
	return "", false
}

// fillWaitLocked is how long this connection's own queue holds presence
// back: nothing below a quarter full, the whole cap from three quarters. A
// client that reads slowly is then sent fewer, larger presence frames
// instead of being cut off for a queue that presence filled.
func (c *conn) fillWaitLocked() time.Duration {
	if len(c.queue) == 0 {
		return 0
	}
	fill := float64(len(c.queue)) / float64(c.hub.opts.QueueFrames)
	if b := float64(c.queueBytes) / float64(c.hub.opts.QueueBytes); b > fill {
		fill = b
	}
	if fill <= fillCalm {
		return 0
	}
	part := (fill - fillCalm) / (fillFull - fillCalm)
	if part > 1 {
		part = 1
	}
	return time.Duration(part * float64(c.hub.opts.presenceCap))
}

// sendPresence sends a presence change to this connection now, or keeps it
// to send with others. wait is what the server-wide load asks for. queued
// is true when the connection must be put on the hub's list of connections
// with something waiting, to be sent by due. The caller holds the presence
// queue's lock.
func (c *conn) sendPresence(item *presenceItem, now time.Time, wait time.Duration) (due time.Time, queued bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// An account named twice among the recipients is told once.
	if c.closed || c.lastItem == item {
		return
	}
	c.lastItem = item
	if own := c.fillWaitLocked(); own > wait {
		wait = own
	}
	// Sent at once only if nothing older is waiting: otherwise an older
	// value for this account could leave after this newer one.
	if wait <= 0 && !c.replaying && len(c.pending) == 0 {
		c.hub.presStats.frames.Add(1)
		c.enqueueLocked(item.frame)
		return
	}
	if len(c.pending) == 0 {
		c.pendingDue = now.Add(wait)
	}
	c.pending = append(c.pending, item)
	// Until `ready` everything waits, and goes in one frame after it
	// (conn.replay): a client that has just connected is not sent a stream
	// of changes it is about to load anyway.
	if c.replaying || c.pendingListed {
		return
	}
	c.pendingListed = true
	return c.pendingDue, true
}

// flushPresenceLocked sends every waiting presence change: one alone as the
// presence.changed it is, several as presence.batch with the newest value
// for each account. built is the batches already made in this flush, by
// the list of changes they were made from; it is returned with this one
// added. A caller that flushes one connection alone passes nil.
func (c *conn) flushPresenceLocked(built []builtBatch) []builtBatch {
	pending := c.pending
	c.pending = nil
	if len(pending) == 0 || c.closed {
		return built
	}
	stats := &c.hub.presStats
	if len(pending) == 1 {
		stats.frames.Add(1)
		c.enqueueLocked(pending[0].frame)
		return built
	}
	var batch *builtBatch
	for i := range built {
		// The same changes in the same order, compared one by one: a
		// connection is only ever handed bytes made from its own list.
		if slices.Equal(built[i].items, pending) {
			batch = &built[i]
			break
		}
	}
	if batch == nil {
		built = append(built, encodePresence(pending))
		batch = &built[len(built)-1]
	}
	stats.merged.Add(int64(batch.merged))
	for _, frame := range batch.frames {
		if c.closed {
			break // the queue filled
		}
		stats.frames.Add(1)
		if bytes.HasPrefix(frame, batchPrefix) {
			stats.batches.Add(1)
		}
		c.enqueueLocked(frame)
	}
	return built
}

var batchPrefix = []byte(`{"type":"` + EventPresenceBatch + `"`)

// encodePresence makes the frames for a list of changes, oldest first: the
// newest change for each account, in presence.batch frames of at most
// presenceBatchMax, or the one presence.changed if that is all that is left.
func encodePresence(pending []*presenceItem) builtBatch {
	// Walked from the newest, so the first change met for an account is the
	// one that stands.
	seen := make(map[model.AccountID]struct{}, len(pending))
	keep := make([]*presenceItem, 0, len(pending))
	for i := len(pending) - 1; i >= 0; i-- {
		if _, dup := seen[pending[i].about]; dup {
			continue
		}
		seen[pending[i].about] = struct{}{}
		keep = append(keep, pending[i])
	}
	slices.Reverse(keep)
	out := builtBatch{items: pending, merged: len(pending) - len(keep)}
	if len(keep) == 1 {
		out.frames = [][]byte{keep[0].frame}
		return out
	}
	at, _ := json.Marshal(model.Now())
	for len(keep) > 0 {
		part := keep[:min(len(keep), presenceBatchMax)]
		keep = keep[len(part):]
		size := 64
		for _, it := range part {
			size += len(it.frame) + 1
		}
		var buf bytes.Buffer
		buf.Grow(size)
		buf.Write(batchPrefix)
		buf.WriteString(`,"at":`)
		buf.Write(at)
		buf.WriteString(`,"changes":[`)
		for i, it := range part {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.Write(it.frame)
		}
		buf.WriteString("]}")
		out.frames = append(out.frames, buf.Bytes())
	}
	return out
}
