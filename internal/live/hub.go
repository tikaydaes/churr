// Package live is the WebSocket hub: connections, the hello/resume/ready
// exchange, catch-up from the events log, heartbeats, back-pressure, and the
// fan-out of events to the accounts that may receive them
// (docs/EVENTS.md).
//
// Fan-out is by membership and decided here alone, by asking perm. There is
// no subscribe request, and nothing in this package decides what an account
// may read.
package live

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Subprotocol is what a client must ask for in Sec-WebSocket-Protocol.
const Subprotocol = "churr.v1"

// Close codes of docs/EVENTS.md section 6.
const (
	CloseNormal       = 1000
	CloseGoingAway    = 1001
	CloseRestarting   = 1012
	CloseCredential   = 4001
	CloseRemoved      = 4003
	CloseUnreadable   = 4004
	CloseNoResume     = 4005
	CloseNoHeartbeat  = 4008
	CloseQueueFull    = 4009
	CloseTooManySends = 4029
)

// Tailor fills in what differs per reader in an event: `reactions[].me`,
// `notify` on message.created, a form's tally. The chat service implements
// it. The event it is given is in plain JSON form (model.Event.Normalize).
type Tailor interface {
	// Tailor returns top-level fields to add to or replace in e for one
	// recipient, or nil if the event is the same for them as for anyone.
	// live is false on catch-up, where `notify` is never `notify`.
	Tailor(ctx context.Context, e model.Event, to model.AccountID, live bool) map[string]any
}

// FanoutTailor is a Tailor that can also answer for many readers of one
// event at once. Asked one reader at a time, whatever the answer is read
// from is locked and let go once for each of fifty thousand readers by
// every goroutine handing the event out, and they then wait for one another
// more than they work. A Tailor need not be one.
type FanoutTailor interface {
	Tailor
	// TailorFanout returns, for each account in to, what
	// Tailor(ctx, e, account, true) returns. It may be called from
	// several goroutines at once, each with its own accounts, and an
	// account is named in one call only.
	TailorFanout(ctx context.Context, e model.Event, to []model.AccountID) []map[string]any
}

// Hider is a Tailor that also says which members of a conversation may not
// be sent an event at all: in a private channel, those who joined after
// what the event is about was posted (ruling R87). It is asked with the
// conversation's roster, and answers nil, reading nothing, when no member
// has such a floor. A Tailor need not be one.
type Hider interface {
	Tailor
	Hidden(ctx context.Context, e model.Event, roster *store.Roster) map[model.AccountID]bool
}

// hiddenFrom asks the tailor, if it is a Hider, who may not be sent e.
func (h *Hub) hiddenFrom(ctx context.Context, tailor Tailor, e model.Event, roster *store.Roster) map[model.AccountID]bool {
	hd, ok := tailor.(Hider)
	if !ok || e.ConversationID == "" {
		return nil
	}
	if roster == nil {
		var err error
		if roster, err = h.perm.Roster(ctx, e.ConversationID); err != nil {
			return nil
		}
	}
	return hd.Hidden(ctx, e, roster)
}

// ClientHandler receives client frames whose type starts with a prefix
// another package registered with Hub.Handle. It returns the error to send
// back, if any. `calls` registers "call.".
type ClientHandler func(ctx context.Context, from Client, frameType string, frame json.RawMessage) error

// Client says who sent a frame.
type Client struct {
	ConnectionID string
	Session      model.Session
	Account      model.Account
}

// Options tune a Hub. The zero value gives the documented behaviour; tests
// shorten the times and the queue.
type Options struct {
	Log *slog.Logger
	// Heartbeat is how often a client pings. A connection that sends
	// nothing for three of them is closed.
	Heartbeat time.Duration
	// ResumeWithin is how long a client has to send `resume`.
	ResumeWithin time.Duration
	// QueueFrames and QueueBytes bound each connection's send queue.
	QueueFrames int
	QueueBytes  int
	// ReplayLimit is how many events behind a client may be before it is
	// told to reload instead.
	ReplayLimit int
	// PresenceEvery is how often the hub looks for accounts that have
	// gone idle, to tell others they are now away.
	PresenceEvery time.Duration

	// CountsEvery is how old an online count may be, and how often
	// presence.counts is sent to those whose numbers changed (lazy.go).
	CountsEvery time.Duration

	// NoCompression refuses WebSocket compression to clients that ask for
	// it (wire.go). It is for measuring what compression costs and saves.
	NoCompression bool

	// WritesPerSecond pins the pace at which the hub hands frames to the
	// sockets of all connections together (flush.go). 0 leaves it to the
	// hub, which sets one only when the machine is losing packets on the
	// way out (pace.go); less than 0 is never any pace. It belongs to the
	// process, not to one hub.
	WritesPerSecond int

	// Admit is asked before each new connection is taken on, and may
	// take a few seconds to answer. When it says no the connection is
	// closed with 4029 before anything is kept for it (memory.go). The
	// server sets it to its memory guard; nil admits everyone.
	Admit func(ctx context.Context) bool

	// writeTimeout is how long one frame may take to be handed to the
	// socket before the client is closed as a slow consumer. Only this
	// package's tests set it.
	writeTimeout time.Duration

	// writeDelay holds the writer back before each frame. Only this
	// package's tests set it, to stand in for a client that reads slowly.
	writeDelay time.Duration

	// What decides how long presence waits under pressure (coalesce.go).
	// Only this package's tests set them.
	presenceCalm int64
	presenceStep time.Duration
	presenceCap  time.Duration

	// fanoutWorkers is the most goroutines one event is handed out on
	// (publish.go); 0 is one for each processor. Only this package's
	// tests set it.
	fanoutWorkers int
	// fanoutEach is how many connections one of those goroutines is
	// given at least; 0 is fanoutEach of publish.go.
	fanoutEach int

	// How many resumes are worked at once and how many may wait
	// (resume.go). Only this package's tests set them; otherwise they
	// come from the database pool and the processors.
	resumeInFlight int
	resumeQueue    int
}

func (o *Options) defaults() {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Heartbeat == 0 {
		o.Heartbeat = 30 * time.Second
	}
	if o.ResumeWithin == 0 {
		o.ResumeWithin = 10 * time.Second
	}
	if o.QueueFrames == 0 {
		o.QueueFrames = 1000
	}
	if o.QueueBytes == 0 {
		o.QueueBytes = 4 << 20
	}
	if o.ReplayLimit == 0 {
		o.ReplayLimit = 1000
	}
	if o.writeTimeout == 0 {
		o.writeTimeout = 30 * time.Second
	}
	if o.PresenceEvery == 0 {
		o.PresenceEvery = 15 * time.Second
	}
	if o.CountsEvery == 0 {
		o.CountsEvery = countsEvery
	}
	if o.presenceCalm == 0 {
		o.presenceCalm = presenceCalm
	}
	if o.presenceStep == 0 {
		o.presenceStep = presenceStep
	}
	if o.presenceCap == 0 {
		o.presenceCap = presenceCap
	}
}

// activeFor is how long after an `activity` frame a person counts as online
// and a client as active (docs/EVENTS.md section 4).
const activeFor = 5 * time.Minute

// Hub holds every live connection of one server.
type Hub struct {
	perm  *perm.Checker
	store store.Store
	opts  Options

	mu       sync.RWMutex
	conns    map[model.AccountID]map[*conn]struct{}
	tailor   Tailor
	handlers map[string]ClientHandler
	closed   bool
	open     int // how many connections conns holds

	// pres tells a listener when an account's presence changes
	// (presence.go).
	pres presenceWatch

	// load and presStats measure presence fan-out, which decides how long
	// presence waits under pressure (coalesce.go).
	load      presenceLoad
	presStats presenceCounters
	presQueue presenceQueue

	// onClose is told when a connection has gone (direct.go).
	onClose []func(Client)

	// resumes is the queue `resume` frames wait in (resume.go).
	resumes resumeQueue

	// interest is which connections are showing which accounts, and so
	// hear of their presence (interest.go).
	interest interest

	// lazy is the lazy delivery of large channels: what each connection
	// has open, the signals waiting, and the online counts (lazy.go).
	lazy lazyHub

	// wire counts what was sent (wire.go). done is closed when the hub
	// shuts down, and ends the goroutine that logs those counts, which
	// statsOnce starts with the first connection.
	wire      wireCounters
	started   time.Time
	done      chan struct{}
	statsOnce sync.Once
}

// New makes a hub. It asks perm who receives each event and reads the events
// log from the store for catch-up.
func New(p *perm.Checker, s store.Store, opts Options) *Hub {
	opts.defaults()
	h := &Hub{perm: p, store: s, opts: opts, conns: map[model.AccountID]map[*conn]struct{}{}, handlers: map[string]ClientHandler{},
		started: time.Now(), done: make(chan struct{})}
	h.initResumes()
	pacing.log.Store(opts.Log)
	if opts.WritesPerSecond != 0 {
		pacing.pinned.Store(true)
		writePace.rate.Store(int64(max(opts.WritesPerSecond, 0)))
	}
	return h
}

// SetTailor sets who fills in the per-reader parts of events. It is set once
// while the server is being put together, because the chat service that
// implements it is built after the hub.
func (h *Hub) SetTailor(t Tailor) {
	h.mu.Lock()
	h.tailor = t
	h.mu.Unlock()
}

// Handle lets another package receive client frames whose type starts with
// prefix.
func (h *Hub) Handle(prefix string, hd ClientHandler) {
	h.mu.Lock()
	h.handlers[prefix] = hd
	h.mu.Unlock()
}

func (h *Hub) handlerFor(frameType string) ClientHandler {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var best string
	var hd ClientHandler
	for prefix, fn := range h.handlers {
		if strings.HasPrefix(frameType, prefix) && len(prefix) >= len(best) {
			best, hd = prefix, fn
		}
	}
	return hd
}

func (h *Hub) add(c *conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	set := h.conns[c.account.ID]
	if set == nil {
		set = map[*conn]struct{}{}
		h.conns[c.account.ID] = set
	}
	set[c] = struct{}{}
	h.open++
	return true
}

func (h *Hub) remove(c *conn) {
	// The connection is closed by now, so nothing puts it back.
	h.interest.forget(c)
	h.forgetViewer(c)
	h.mu.Lock()
	defer h.mu.Unlock()
	if set := h.conns[c.account.ID]; set != nil {
		if _, held := set[c]; held {
			h.open--
		}
		delete(set, c)
		if len(set) == 0 {
			delete(h.conns, c.account.ID)
		}
	}
}

// connsOf returns the connections of one account.
func (h *Hub) connsOf(account model.AccountID) []*conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	set := h.conns[account]
	out := make([]*conn, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	return out
}

// Publish sends a conversation event to the connections of the
// conversation's recipients, and of the accounts in e.AlsoTo, with the parts
// that differ per reader filled in for each. It does not block on slow
// clients: a connection whose queue is full is closed, and catches up when
// it reconnects.
//
// Call it after the transaction that wrote the event has committed, and in
// seq order within a conversation (chat.Service.Tx does both).
func (h *Hub) Publish(ctx context.Context, e model.Event) {
	// publish.go: what this costs follows the connections it reaches.
	h.publish(ctx, e)
}

// splice returns the event with extra top-level fields added or replaced.
func splice(fields map[string]json.RawMessage, extras ...map[string]any) []byte {
	out := make(map[string]json.RawMessage, len(fields)+4)
	for k, v := range fields {
		out[k] = v
	}
	for _, extra := range extras {
		for k, v := range extra {
			b, err := json.Marshal(v)
			if err != nil {
				continue
			}
			out[k] = b
		}
	}
	b, _ := json.Marshal(out)
	return b
}

// Tailored returns an event from the log as one account should see it, for
// catch-up over HTTP. The live connection's own catch-up does the same.
func (h *Hub) Tailored(ctx context.Context, e model.Event, to model.AccountID) json.RawMessage {
	norm, base, err := e.Normalize()
	if err != nil {
		return nil
	}
	h.mu.RLock()
	tailor := h.tailor
	h.mu.RUnlock()
	if tailor == nil {
		return base
	}
	if h.hiddenFrom(ctx, tailor, norm, nil)[to] {
		return nil
	}
	extra := tailor.Tailor(ctx, norm, to, false)
	if len(extra) == 0 {
		return base
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(base, &fields) != nil {
		return base
	}
	return splice(fields, extra)
}

// ReplayTailor is a Tailor that can also fill in many events for one reader
// at once, which a catch-up needs: asked one event at a time, what is the
// same for every event (the reader's account and settings, its place in the
// conversation) is read again for each. A Tailor need not be one.
type ReplayTailor interface {
	Tailor
	// TailorReplay returns a function for one catch-up of one account. It
	// returns, for each event, what Tailor(ctx, e, to, false) returns.
	// known is what the catch-up has just read about the account in the
	// conversations it is catching up on, which the tailor need not read
	// again; it may be empty.
	TailorReplay(to model.AccountID, known []perm.State) func(ctx context.Context, events []model.Event) []map[string]any
}

// TailoredFor is Tailored for many events and one account: the function it
// returns gives, for a page of events from the log, each as the account
// should see it, in the same order, with nil for one that Tailored would
// give nil for. It is for one catch-up and is not kept beyond it.
func (h *Hub) TailoredFor(to model.AccountID) func(ctx context.Context, events []model.Event) []json.RawMessage {
	return h.tailoredFor(to, nil)
}

// tailoredFor is TailoredFor for a caller that has already read the
// account's state in some conversations.
func (h *Hub) tailoredFor(to model.AccountID, known []perm.State) func(ctx context.Context, events []model.Event) []json.RawMessage {
	h.mu.RLock()
	tailor := h.tailor
	h.mu.RUnlock()
	var many func(ctx context.Context, events []model.Event) []map[string]any
	if rt, ok := tailor.(ReplayTailor); ok {
		many = rt.TailorReplay(to, known)
	}
	return func(ctx context.Context, events []model.Event) []json.RawMessage {
		out := make([]json.RawMessage, len(events))
		if many == nil {
			for i, e := range events {
				out[i] = h.Tailored(ctx, e, to)
			}
			return out
		}
		// Events that cannot be encoded are left out of what the tailor
		// is asked, and stay nil.
		// So are events the reader may not be sent (Hider).
		norms := make([]model.Event, 0, len(events))
		at := make([]int, 0, len(events))
		for i, e := range events {
			norm, base, err := e.Normalize()
			if err != nil || h.hiddenFrom(ctx, tailor, norm, nil)[to] {
				continue
			}
			out[i] = base
			norms = append(norms, norm)
			at = append(at, i)
		}
		for j, extra := range many(ctx, norms) {
			if len(extra) == 0 {
				continue
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(out[at[j]], &fields) != nil {
				continue
			}
			out[at[j]] = splice(fields, extra)
		}
		return out
	}
}

// Send sends one frame to every connection of one account, for events that
// belong to no conversation (docs/EVENTS.md section 9). v is anything that
// marshals to a JSON object with `type` and `at`.
func (h *Hub) Send(account model.AccountID, v any) {
	// Presence may be merged and held a moment under pressure, whoever
	// sends it (coalesce.go). Nothing else is.
	if about, ok := presenceOf(v); ok {
		h.SendPresence([]model.AccountID{account}, about, v)
		return
	}
	conns := h.connsOf(account)
	if len(conns) == 0 {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		h.opts.Log.Error("an account event could not be encoded", "err", err)
		return
	}
	for _, c := range conns {
		c.sendEvent("", 0, b)
	}
	h.count(kindOf(v), len(conns), len(conns)*len(b))
}

// AccountEvent builds an event that belongs to no conversation: `type`, `at`
// and the fields given.
func AccountEvent(eventType string, fields map[string]any) model.Event {
	return model.Event{Type: eventType, At: model.Now(), Payload: fields}
}

// Active reports whether a client of the account is connected and was active
// in the last five minutes. A phone is woken unless this is true (ruling R6).
func (h *Hub) Active(account model.AccountID) bool {
	now := time.Now()
	for _, c := range h.connsOf(account) {
		if c.activeAt(now) {
			return true
		}
	}
	return false
}

// Presence is what the connections say about an account: online while one of
// them was active in the last five minutes (a bot: while it is connected),
// away while it has a connection and none was, offline with none. Appear
// away, out of office and a status that appears away override this to away;
// that is the people service's to apply.
func (h *Hub) Presence(account model.AccountID) model.Presence {
	conns := h.connsOf(account)
	if len(conns) == 0 {
		return model.PresenceOffline
	}
	now := time.Now()
	for _, c := range conns {
		if c.account.Role == model.RoleBot || c.activeAt(now) {
			return model.PresenceOnline
		}
	}
	return model.PresenceAway
}

// Connected reports how many connections are open, for the overview page.
func (h *Hub) Connected() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.open
}

// CloseSession closes the connections of one session, when it is signed out
// or revoked.
func (h *Hub) CloseSession(account model.AccountID, session model.SessionID, code int, reason string) {
	for _, c := range h.connsOf(account) {
		if c.session.ID == session {
			c.close(code, reason)
		}
	}
}

// CloseAccount closes every connection of an account, when it is removed.
func (h *Hub) CloseAccount(account model.AccountID, code int, reason string) {
	for _, c := range h.connsOf(account) {
		c.close(code, reason)
	}
}

// Shutdown closes every connection with the "restarting" code, so clients
// reconnect promptly, and refuses new ones.
func (h *Hub) Shutdown() {
	h.stopPresence()
	h.presQueue.mu.Lock()
	if h.presQueue.timer != nil {
		h.presQueue.timer.Stop()
	}
	h.presQueue.mu.Unlock()
	h.mu.Lock()
	if !h.closed {
		close(h.done)
	}
	h.closed = true
	var all []*conn
	for _, set := range h.conns {
		for c := range set {
			all = append(all, c)
		}
	}
	h.mu.Unlock()
	for _, c := range all {
		c.close(CloseRestarting, "the server is restarting")
	}
}
