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

	// writeDelay holds the writer back before each frame. Only this
	// package's tests set it, to stand in for a client that reads slowly.
	writeDelay time.Duration
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
	if o.PresenceEvery == 0 {
		o.PresenceEvery = 15 * time.Second
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

	// pres tells a listener when an account's presence changes
	// (presence.go).
	pres presenceWatch

	// onClose is told when a connection has gone (direct.go).
	onClose []func(Client)
}

// New makes a hub. It asks perm who receives each event and reads the events
// log from the store for catch-up.
func New(p *perm.Checker, s store.Store, opts Options) *Hub {
	opts.defaults()
	return &Hub{perm: p, store: s, opts: opts, conns: map[model.AccountID]map[*conn]struct{}{}, handlers: map[string]ClientHandler{}}
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
	return true
}

func (h *Hub) remove(c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set := h.conns[c.account.ID]; set != nil {
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
	norm, base, err := e.Normalize()
	if err != nil {
		h.opts.Log.Error("an event could not be encoded", "type", e.Type, "err", err)
		return
	}
	recipients, err := h.perm.Recipients(ctx, e.ConversationID)
	if err != nil {
		// The event is in the log; clients catch up from there.
		h.opts.Log.Error("recipients could not be read; the event was not fanned out", "type", e.Type, "err", err)
		return
	}
	seen := make(map[model.AccountID]bool, len(recipients)+len(e.AlsoTo))
	h.mu.RLock()
	tailor := h.tailor
	h.mu.RUnlock()
	var fields map[string]json.RawMessage // base decoded, made only if someone needs changes
	for _, to := range append(recipients, e.AlsoTo...) {
		if seen[to] {
			continue
		}
		seen[to] = true
		conns := h.connsOf(to)
		if len(conns) == 0 {
			continue
		}
		var extra map[string]any
		if tailor != nil {
			extra = tailor.Tailor(ctx, norm, to, true)
		}
		frame := base
		if only := e.Only[to]; len(extra) > 0 || len(only) > 0 {
			if fields == nil {
				if err := json.Unmarshal(base, &fields); err != nil {
					h.opts.Log.Error("an event could not be re-read", "type", e.Type, "err", err)
					return
				}
			}
			frame = splice(fields, extra, only)
		}
		for _, c := range conns {
			c.sendEvent(e.ConversationID, e.Seq, frame)
		}
	}
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

// Send sends one frame to every connection of one account, for events that
// belong to no conversation (docs/EVENTS.md section 9). v is anything that
// marshals to a JSON object with `type` and `at`.
func (h *Hub) Send(account model.AccountID, v any) {
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
	n := 0
	for _, set := range h.conns {
		n += len(set)
	}
	return n
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
	h.mu.Lock()
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
