package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Lazy delivery of large channels (ruling R66).
//
// A message in a channel of twenty thousand people is read, as it arrives,
// by the few who have that channel on screen. Sent in full to every
// connected member it costs a frame each, ten times a second in a busy
// channel, for screens that are showing something else. So a connection
// says which conversations it has open (`conversations.open`,
// docs/EVENTS.md section 4.1), and a conversation that is delivered lazily
// (one above the server's large_channel_limit, or one that says so itself)
// goes in full only to the connections that have it open. The others are
// sent `conversation.changed`: the newest seq, how many messages came, and
// the reader's numbers if the server has them at hand. That is sent at most
// once a second for each conversation on each connection, however much
// happened.
//
// Two things are never held back, and travel inside a
// `conversation.changed` of their own the moment they happen:
//
//   - a message the one notification rule says notifies the reader (they
//     are named in it, a group of theirs is, they follow the thread, or
//     they asked to hear of everything there);
//   - an event that is not message traffic (someone joined, the channel was
//     renamed), or that carries something for this reader alone (their own
//     message coming back with its client_id).
//
// A connection that is not showing a lazy conversation is never sent one of
// its events as a frame of its own with a seq, so what a client holds of a
// conversation always ends at one seq with nothing missing before it. When
// the client opens the conversation it says where what it holds ends, and
// the server sends what came after from the log, exactly as it does after
// `resume`, holding live events meanwhile: no gap and nothing twice.
//
// A connection that has never said what it has open is a client from
// before this, or a bot. It is sent everything in full, as before: a bot
// answers messages in channels nobody "opens" for it, and an old app would
// take `conversation.changed` for an event it does not know and show
// nothing new until it reconnected.

const (
	// MaxOpenConversations is how many conversations one connection may
	// say it has open.
	MaxOpenConversations = 8
	// signalEvery is the least time between two conversation.changed
	// frames for one conversation on one connection.
	signalEvery = time.Second
	// signalTick is how often waiting signals are looked at.
	signalTick = 200 * time.Millisecond
	// countsEvery is how old an online count may be (Options.CountsEvery).
	countsEvery = 5 * time.Second
	// limitsEvery is how often the two sizes are read from the settings.
	limitsEvery = 2 * time.Second
)

// EventConversationChanged tells a connection that is not showing a lazy
// conversation that something happened in it. EventPresenceCounts carries
// the online counts.
const (
	EventConversationChanged = "conversation.changed"
	EventPresenceCounts      = "presence.counts"
)

// SignalTailor is a Tailor that can also say what a reader's numbers in a
// conversation are now, from memory alone, for conversation.changed. The
// fields it returns are added to the frame; nil when it does not have them.
type SignalTailor interface {
	Signal(to model.AccountID, conv model.ConversationID) map[string]any
}

// lazyState is what one connection has not been told yet about one lazy
// conversation it is not showing.
type lazyState struct {
	sentAt    time.Time
	seq       int64
	messages  int
	mentioned bool
	dirty     bool
}

// signal is one conversation.changed to send.
type signal struct {
	conv      model.ConversationID
	seq       int64
	messages  int
	mentioned bool
}

// lazyHub is the hub's side: the sizes, the connections with a signal
// waiting, who has what open, and the online counts.
type lazyHub struct {
	large, feature atomic.Int64
	limitsAt       atomic.Int64 // unix nanoseconds of the last read; 0: never
	fixed          atomic.Bool  // SetLimits was called: the settings are not read

	mu      sync.Mutex
	waiting []*conn

	viewMu  sync.Mutex
	viewers map[model.ConversationID]map[*conn]struct{}

	countMu  sync.Mutex
	online   map[model.AccountID]struct{}
	people   int
	onlineAt time.Time
	counts   map[model.ConversationID]cachedCount

	stats lazyCounters
}

type cachedCount struct {
	n  int
	ok bool
	at time.Time
}

type lazyCounters struct {
	full, signals, urgent, merged atomic.Int64
}

// LazyStats counts what lazy delivery has done since the hub started.
type LazyStats struct {
	// Full is how many events of lazy conversations were sent in full,
	// to connections showing them and to ones that never said what they
	// show. Signals is how many conversation.changed frames were sent
	// that carry no event, Urgent how many that carry one, and Merged
	// how many events were told of only by a later signal.
	Full, Signals, Urgent, Merged int64
}

// LazyStats returns the counts.
func (h *Hub) LazyStats() LazyStats {
	s := &h.lazy.stats
	return LazyStats{Full: s.full.Load(), Signals: s.signals.Load(), Urgent: s.urgent.Load(), Merged: s.merged.Load()}
}

// SetLimits fixes the two sizes of ruling R66: the number of members above
// which a channel is delivered lazily, and the number above which a
// conversation shows an online count. Left alone, the hub reads them from
// the server's settings every couple of seconds, and at once when whoever
// changed the settings calls RefreshLimits; after SetLimits it reads them
// no more. It is for tests and tools that have no settings to change.
func (h *Hub) SetLimits(large, feature int64) {
	h.lazy.fixed.Store(true)
	h.setLimits(large, feature)
}

func (h *Hub) setLimits(large, feature int64) {
	h.lazy.large.Store(large)
	h.lazy.feature.Store(feature)
	h.lazy.limitsAt.Store(time.Now().UnixNano())
}

// RefreshLimits reads the two sizes from the server's settings.
func (h *Hub) RefreshLimits(ctx context.Context) {
	if h.lazy.fixed.Load() {
		return
	}
	set, err := h.perm.Settings(ctx)
	if err != nil {
		// What was read before stands; the defaults if nothing was.
		if h.lazy.limitsAt.Load() == 0 {
			h.setLimits(model.DefaultLargeChannelLimit, model.DefaultLargeFeatureLimit)
		}
		return
	}
	h.setLimits(set.LargeChannelLimit, set.LargeFeatureLimit)
}

// limits returns the two sizes, reading them the first time.
func (h *Hub) limits(ctx context.Context) (large, feature int64) {
	if h.lazy.limitsAt.Load() == 0 {
		h.RefreshLimits(ctx)
	}
	return h.lazy.large.Load(), h.lazy.feature.Load()
}

// deferrable reports whether an event is message traffic: what a reader who
// is not looking at a lazy conversation can be told of later. Everything
// else (membership, the conversation itself, calls) is told at once.
func deferrable(e model.Event) bool {
	if e.MessageID != "" {
		return true
	}
	for _, prefix := range []string{"message.", "reaction.", "task.", "pin.", "form.", "preview."} {
		if strings.HasPrefix(e.Type, prefix) {
			return true
		}
	}
	return false
}

// wantsFull reports whether the connection is sent a lazy conversation's
// events in full: it has it open, or has never said what it has open.
func (c *conn) wantsFull(conv model.ConversationID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.declared {
		return true
	}
	_, open := c.open[conv]
	return open
}

// sendIfFull sends an event of a lazy conversation in full if the
// connection still wants it so, and reports whether it did. The question
// and the sending are one step: a connection that has closed a conversation
// is never sent another of its events, so what the client holds of it ends
// where it said.
func (c *conn) sendIfFull(conv model.ConversationID, seq int64, frame []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.declared {
		if _, open := c.open[conv]; !open {
			return false
		}
	}
	c.sendEventLocked(conv, seq, frame)
	return true
}

// noteLazy records an event of a lazy conversation this connection is not
// showing, and sends conversation.changed now if a second has passed since
// the last one for it.
func (c *conn) noteLazy(conv model.ConversationID, seq int64, message, counted bool, now time.Time) {
	h := c.hub
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	if _, open := c.open[conv]; open {
		// Opened since the fan-out asked: the catch-up that opening
		// started finds the event in the log.
		c.mu.Unlock()
		return
	}
	if c.lazy == nil {
		c.lazy = map[model.ConversationID]*lazyState{}
	}
	st := c.lazy[conv]
	if st == nil {
		st = &lazyState{}
		c.lazy[conv] = st
	}
	if st.dirty {
		h.lazy.stats.merged.Add(1)
	}
	if seq > st.seq {
		st.seq = seq
	}
	if message {
		st.messages++
	}
	st.mentioned = st.mentioned || counted
	st.dirty = true
	var send *signal
	list := false
	// Until `ready` nothing is sent: the catch-up is about to say more.
	if !c.replaying && now.Sub(st.sentAt) >= signalEvery {
		send = st.take(conv, now)
	} else if !c.signalListed {
		c.signalListed, list = true, true
	}
	c.mu.Unlock()
	if send != nil {
		h.sendSignal(c, *send)
	}
	if list {
		h.lazy.mu.Lock()
		h.lazy.waiting = append(h.lazy.waiting, c)
		h.lazy.mu.Unlock()
	}
}

// take empties the state into a signal. The caller holds the connection's
// lock.
func (st *lazyState) take(conv model.ConversationID, now time.Time) *signal {
	s := &signal{conv: conv, seq: st.seq, messages: st.messages, mentioned: st.mentioned}
	st.messages, st.mentioned, st.dirty, st.sentAt = 0, false, false, now
	return s
}

// sendSignal sends one conversation.changed.
func (h *Hub) sendSignal(c *conn, s signal) {
	f := map[string]any{"type": EventConversationChanged, "at": model.Now(), "conversation_id": s.conv,
		"last_seq": s.seq, "messages": s.messages, "mentioned": s.mentioned}
	h.mu.RLock()
	tailor := h.tailor
	h.mu.RUnlock()
	if st, ok := tailor.(SignalTailor); ok {
		for k, v := range st.Signal(c.account.ID, s.conv) {
			f[k] = v
		}
	}
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	h.lazy.stats.signals.Add(1)
	h.count(EventConversationChanged, 1, len(b))
	b = c.own(b) // made for this reader alone
	c.mu.Lock()
	if !c.closed {
		c.enqueueLocked(b)
	}
	c.mu.Unlock()
}

// sendWithin sends an event inside a conversation.changed of its own, at
// once: the reader should have it although the conversation is not on
// their screen. frame is the event as this reader sees it.
func (c *conn) sendWithin(conv model.ConversationID, seq int64, frame []byte) {
	var buf bytes.Buffer
	buf.Grow(len(frame) + 160)
	buf.WriteString(`{"type":"` + EventConversationChanged + `","at":`)
	at, _ := json.Marshal(model.Now())
	buf.Write(at)
	buf.WriteString(`,"conversation_id":"`)
	buf.WriteString(string(conv))
	buf.WriteString(`","last_seq":`)
	seqText, _ := json.Marshal(seq)
	buf.Write(seqText)
	buf.WriteString(`,"event":`)
	buf.Write(frame)
	buf.WriteByte('}')
	b := buf.Bytes()
	c.hub.lazy.stats.urgent.Add(1)
	c.hub.count(EventConversationChanged, 1, len(b))
	b = c.own(b) // made for this reader alone
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if c.replaying {
		// Kept for after `ready`, like any live event during a catch-up
		// that has started; before that the catch-up will find it in the
		// log.
		if c.catchingUp {
			c.held = append(c.held, heldEvent{"", 1, b})
		}
		return
	}
	c.enqueueLocked(b)
}

// flushSignals sends the signals whose second has passed.
func (h *Hub) flushSignals(now time.Time) {
	h.lazy.mu.Lock()
	waiting := h.lazy.waiting
	h.lazy.waiting = nil
	h.lazy.mu.Unlock()
	var keep []*conn
	var send []signal
	for _, c := range waiting {
		send = send[:0]
		c.mu.Lock()
		still := false
		if !c.closed {
			for conv, st := range c.lazy {
				switch {
				case !st.dirty:
					// Nothing to say; forgotten once it is old, so the
					// map holds only conversations that are busy.
					if now.Sub(st.sentAt) > time.Minute {
						delete(c.lazy, conv)
					}
				case c.replaying || now.Sub(st.sentAt) < signalEvery:
					still = true
				default:
					send = append(send, *st.take(conv, now))
				}
			}
		}
		c.signalListed = still
		c.mu.Unlock()
		if still {
			keep = append(keep, c)
		}
		for _, s := range send {
			h.sendSignal(c, s)
		}
	}
	if len(keep) > 0 {
		h.lazy.mu.Lock()
		h.lazy.waiting = append(h.lazy.waiting, keep...)
		h.lazy.mu.Unlock()
	}
}

// runLazy sends waiting signals and online counts, and keeps the two sizes
// fresh, until the hub shuts down. It starts with the first connection.
func (h *Hub) runLazy() {
	t := time.NewTicker(signalTick)
	defer t.Stop()
	var countsAt, limitsAt time.Time
	for {
		select {
		case <-h.done:
			return
		case now := <-t.C:
			h.flushSignals(now)
			if now.Sub(limitsAt) >= limitsEvery {
				limitsAt = now
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				h.RefreshLimits(ctx)
				cancel()
			}
			if now.Sub(countsAt) >= h.opts.CountsEvery {
				countsAt = now
				h.sendCounts(now)
			}
		}
	}
}

// --- what a connection has open ---

// openFrame is `conversations.open`: the conversations on the client's
// screen, in place of any it named before, each with the seq where what
// the client holds of it ends (-1: it holds nothing and will load it).
type openFrame struct {
	clientFrame
	Positions map[string]int64 `json:"positions"`
}

// memberOf keeps the conversations of ids the account is a member of.
func (h *Hub) memberOf(ctx context.Context, account model.AccountID, ids []string) map[model.ConversationID]*store.Roster {
	out := make(map[model.ConversationID]*store.Roster, len(ids))
	for _, raw := range ids {
		if !model.ValidID(raw) {
			continue
		}
		conv := model.ConversationID(raw)
		if roster, err := h.perm.Roster(ctx, conv); err == nil && roster.Has(account) {
			out[conv] = roster
		}
	}
	return out
}

// declare sets what the connection has open and returns the conversations
// that were not open before. hold says a catch-up follows for those: their
// live events wait for it.
func (c *conn) declare(open map[model.ConversationID]*store.Roster, hold func(model.ConversationID) bool) []model.ConversationID {
	h := c.hub
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	old := c.open
	now := make(map[model.ConversationID]struct{}, len(open))
	var added []model.ConversationID
	for conv := range open {
		now[conv] = struct{}{}
		if _, had := old[conv]; had {
			continue
		}
		added = append(added, conv)
		// What was waiting to be signalled is about to be sent in full.
		delete(c.lazy, conv)
		if hold != nil && hold(conv) {
			if c.opening == nil {
				c.opening = map[model.ConversationID][]heldEvent{}
			}
			c.opening[conv] = nil
		}
	}
	c.open, c.declared = now, true
	c.mu.Unlock()

	v := &h.lazy
	v.viewMu.Lock()
	if v.viewers == nil {
		v.viewers = map[model.ConversationID]map[*conn]struct{}{}
	}
	for conv := range old {
		if _, kept := now[conv]; !kept {
			v.dropViewerLocked(conv, c)
		}
	}
	for _, conv := range added {
		set := v.viewers[conv]
		if set == nil {
			set = map[*conn]struct{}{}
			v.viewers[conv] = set
		}
		set[c] = struct{}{}
	}
	v.viewMu.Unlock()
	return added
}

func (v *lazyHub) dropViewerLocked(conv model.ConversationID, c *conn) {
	if set := v.viewers[conv]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(v.viewers, conv)
		}
	}
}

// forgetViewer drops a connection that has closed.
func (h *Hub) forgetViewer(c *conn) {
	c.mu.Lock()
	open := c.open
	c.mu.Unlock()
	if len(open) == 0 {
		return
	}
	v := &h.lazy
	v.viewMu.Lock()
	for conv := range open {
		v.dropViewerLocked(conv, c)
	}
	v.viewMu.Unlock()
}

// openConversations answers `conversations.open`. It runs on the
// connection's reading goroutine, so two of them never overlap, and the
// answer follows whatever the catch-up sent.
func (c *conn) openConversations(ctx context.Context, data []byte) *model.Error {
	var f openFrame
	if strict(data, &f) != nil {
		return model.BadRequest("`conversations.open` takes `positions`: conversation ids, each with the last seq held or -1.")
	}
	if len(f.Positions) > MaxOpenConversations {
		return model.BadRequest("At most 8 conversations can be open on one connection.")
	}
	c.mu.Lock()
	early := c.replaying
	c.mu.Unlock()
	if early {
		return model.BadRequest("`conversations.open` is sent after `ready`; `resume` takes `open` before it.")
	}
	h := c.hub
	ids := make([]string, 0, len(f.Positions))
	for raw := range f.Positions {
		ids = append(ids, raw)
	}
	open := h.memberOf(ctx, c.account.ID, ids)
	added := c.declare(open, func(conv model.ConversationID) bool { return f.Positions[string(conv)] >= 0 })
	for _, conv := range added {
		pos := f.Positions[string(conv)]
		if pos < 0 {
			continue
		}
		c.catchUpOpened(ctx, conv, pos)
	}
	if len(added) > 0 {
		h.sendCountsTo(c, time.Now(), true)
	}
	return nil
}

// catchUpOpened brings a conversation the client has just opened up to
// date from pos, where what the client holds ends, and then lets the live
// events that waited through.
func (c *conn) catchUpOpened(ctx context.Context, conv model.ConversationID, pos int64) {
	h := c.hub
	st, err := h.perm.Load(ctx, c.account, conv)
	if err == nil && pos > st.Conversation.LastSeq {
		// Nobody holds what has not happened.
		pos = st.Conversation.LastSeq
	}
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		// Something could not be read just now. The client is not told
		// to drop a conversation over that: the live events follow, and
		// the first of them shows it the gap, which it fills by itself.
		h.opts.Log.Error("a conversation that was opened could not be brought up to date", "err", err)
	} else {
		// The client says what it holds, whatever an earlier catch-up
		// noted.
		c.mu.Lock()
		c.replayed[conv] = pos
		c.mu.Unlock()
		c.catchUp(ctx, string(conv), pos, st, err, h.tailoredFor(c.account.ID, nil))
	}
	c.mu.Lock()
	for _, e := range c.opening[conv] {
		if e.seq > c.replayed[conv] && !c.closed {
			c.enqueueLocked(e.frame)
		}
	}
	delete(c.opening, conv)
	c.mu.Unlock()
}

// --- online counts ---

// onlineNow returns the accounts that are online and how many of them are
// people, from a count no older than Options.CountsEvery.
func (h *Hub) onlineNow(now time.Time) (map[model.AccountID]struct{}, int) {
	v := &h.lazy
	v.countMu.Lock()
	defer v.countMu.Unlock()
	if v.online != nil && now.Sub(v.onlineAt) < h.opts.CountsEvery {
		return v.online, v.people
	}
	var all []*conn
	h.mu.RLock()
	for _, set := range h.conns {
		for c := range set {
			all = append(all, c)
		}
	}
	h.mu.RUnlock()
	online := make(map[model.AccountID]struct{}, len(all))
	people := 0
	for _, c := range all {
		if _, counted := online[c.account.ID]; counted {
			continue
		}
		bot := c.account.Role == model.RoleBot
		if bot || c.activeAt(now) {
			online[c.account.ID] = struct{}{}
			if !bot {
				people++
			}
		}
	}
	v.online, v.people, v.onlineAt = online, people, now
	v.counts = nil
	return online, people
}

// onlineIn returns how many members of a conversation are online, if the
// conversation shows a count: it is above the large_feature_limit and is
// not an announcement channel, where nobody's presence is shown at all.
func (h *Hub) onlineIn(ctx context.Context, conv model.ConversationID, now time.Time) (int, bool) {
	online, _ := h.onlineNow(now)
	v := &h.lazy
	v.countMu.Lock()
	if got, cached := v.counts[conv]; cached {
		v.countMu.Unlock()
		return got.n, got.ok
	}
	v.countMu.Unlock()
	_, feature := h.limits(ctx)
	got := cachedCount{at: now}
	if roster, err := h.perm.Roster(ctx, conv); err == nil && !roster.Announcement && roster.LargeAbove(feature) {
		got.ok = true
		if roster.Len() <= len(online) {
			for _, id := range roster.IDs {
				if _, on := online[id]; on {
					got.n++
				}
			}
		} else {
			for id := range online {
				if roster.Has(id) {
					got.n++
				}
			}
		}
	}
	v.countMu.Lock()
	if v.counts == nil {
		v.counts = map[model.ConversationID]cachedCount{}
	}
	v.counts[conv] = got
	v.countMu.Unlock()
	return got.n, got.ok
}

// OnlineCount is how many people are online on this server, from a count
// no older than a few seconds.
func (h *Hub) OnlineCount() int {
	_, people := h.onlineNow(time.Now())
	return people
}

// sendCountsTo sends one connection presence.counts: how many people are
// online on the server, and how many members of each conversation it has
// open that shows a count. Nothing is sent when it would say what the
// connection was last told, unless always.
func (h *Hub) sendCountsTo(c *conn, now time.Time, always bool) {
	c.mu.Lock()
	declared := c.declared && !c.closed && !c.replaying
	open := make([]model.ConversationID, 0, len(c.open))
	for conv := range c.open {
		open = append(open, conv)
	}
	c.mu.Unlock()
	if !declared {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f := map[string]any{"type": EventPresenceCounts}
	// Whom a guest may see is narrower than the server, and a bot shows
	// nobody anything: neither is told the server's count.
	if c.account.Role.IsFullMember() {
		_, people := h.onlineNow(now)
		f["online_count"] = people
	}
	convs := map[model.ConversationID]int{}
	for _, conv := range open {
		if n, ok := h.onlineIn(ctx, conv, now); ok {
			convs[conv] = n
		}
	}
	f["conversations"] = convs
	key, err := json.Marshal(f)
	if err != nil {
		return
	}
	c.mu.Lock()
	same := string(key) == c.countsSent
	c.countsSent = string(key)
	c.mu.Unlock()
	if same && !always {
		return
	}
	f["at"] = model.Now()
	c.send(f)
}

// sendCounts sends presence.counts to every connection that has said what
// it has open and whose numbers changed.
func (h *Hub) sendCounts(now time.Time) {
	var all []*conn
	h.mu.RLock()
	for _, set := range h.conns {
		for c := range set {
			all = append(all, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range all {
		h.sendCountsTo(c, now, false)
	}
}
