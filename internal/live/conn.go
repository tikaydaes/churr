package live

import (
	"bytes"
	"container/list"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// maxFrame is the largest frame either side may send.
const maxFrame = 1 << 20

// Limits on what a client sends (docs/EVENTS.md section 5).
const (
	framesPerSecond  = 20
	unreadablePerMin = 10
	// floodFrames is how many frames over the rate a client may send
	// before it is taken to be one that "keeps going" and is closed.
	floodFrames = 100
)

// conn is one WebSocket connection.
type conn struct {
	hub     *Hub
	ws      *websocket.Conn
	id      string
	session model.Session
	account model.Account
	cancel  context.CancelFunc

	mu         sync.Mutex
	queue      [][]byte
	queueBytes int
	wake       chan struct{}
	closed     bool
	// replaying is true from the moment the connection is registered
	// until its catch-up is done. Live events that arrive meanwhile are
	// held, so a conversation's events are never seen out of order.
	replaying bool
	// catchingUp is true once the catch-up has started, which may be some
	// time after the connection was registered (resume.go). Before that
	// a conversation's live events are not held: the catch-up reads the
	// log when it starts, and finds them there.
	catchingUp bool
	// queuedAt is this connection's place in the resume queue while it
	// waits there. The queue's lock guards it.
	queuedAt *list.Element
	held     []heldEvent
	// replayed is, per conversation, the highest seq the catch-up sent or
	// the client said it already had. A live event at or below it is one
	// the client has, and is dropped.
	replayed   map[model.ConversationID]int64
	lastActive time.Time
	lastHeard  time.Time

	// pending is the presence changes waiting to be sent together, oldest
	// first; pendingDue is when they should go, and pendingListed says the
	// hub has this connection on its list of ones to send to (coalesce.go).
	pending       []*presenceItem
	pendingDue    time.Time
	pendingListed bool
	// lastItem is the presence change this connection was last handed, so
	// one addressed to it twice is taken once.
	lastItem *presenceItem
}

type heldEvent struct {
	conv  model.ConversationID
	seq   int64
	frame []byte
}

func newConnectionID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "c_" + base64.RawURLEncoding.EncodeToString(b)
}

func (c *conn) activeAt(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return now.Sub(c.lastActive) < activeFor
}

// sendEvent queues an event for this connection, holding it during catch-up
// and dropping it if the catch-up already sent it.
func (c *conn) sendEvent(conv model.ConversationID, seq int64, frame []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if conv != "" && seq != 0 {
		if c.replaying {
			if c.catchingUp {
				c.held = append(c.held, heldEvent{conv, seq, frame})
			}
			return
		}
		if seq <= c.replayed[conv] {
			return
		}
	}
	c.enqueueLocked(frame)
}

// send queues a frame that is not a conversation event.
func (c *conn) send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.enqueueLocked(b)
	}
}

// enqueueLocked adds a frame to the send queue. A client that reads too
// slowly is not waited for: when its queue is full the connection is closed,
// and it catches up from the log when it reconnects, so nothing is lost.
func (c *conn) enqueueLocked(frame []byte) {
	if len(c.queue) >= c.hub.opts.QueueFrames || c.queueBytes+len(frame) > c.hub.opts.QueueBytes {
		c.closeLocked(CloseQueueFull, "the send queue filled")
		return
	}
	c.queue = append(c.queue, frame)
	c.queueBytes += len(frame)
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *conn) close(code int, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked(code, reason)
}

func (c *conn) closeLocked(code int, reason string) {
	if c.closed {
		return
	}
	c.closed = true
	c.queue, c.held, c.pending = nil, nil, nil
	// The close handshake waits for the peer; whoever asked for the close
	// (often a fan-out) must not. The connection's context is ended only
	// afterwards: ending it first would cut the socket under the reader
	// before the close frame, and its code, had been sent.
	go func() {
		c.ws.Close(websocket.StatusCode(code), reason)
		c.cancel()
	}()
}

// writer sends queued frames until the connection closes.
func (c *conn) writer(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		}
		for {
			c.mu.Lock()
			if len(c.queue) == 0 || c.closed {
				c.mu.Unlock()
				break
			}
			frame := c.queue[0]
			c.queue = c.queue[1:]
			c.queueBytes -= len(frame)
			c.mu.Unlock()
			if d := c.hub.opts.writeDelay; d > 0 {
				time.Sleep(d)
			}
			wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := c.ws.Write(wctx, websocket.MessageText, frame)
			cancel()
			if err != nil {
				c.close(CloseGoingAway, "write failed")
				return
			}
		}
	}
}

// watchdog closes the connection if no `resume` arrives in time, and later
// if the client has sent nothing for three heartbeats.
func (c *conn) watchdog(ctx context.Context, resumed <-chan struct{}) {
	select {
	case <-ctx.Done():
		return
	case <-resumed:
	case <-time.After(c.hub.opts.ResumeWithin):
		c.close(CloseNoResume, "no resume arrived in time")
		return
	}
	silence := 3 * c.hub.opts.Heartbeat
	t := time.NewTicker(c.hub.opts.Heartbeat / 2)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			c.mu.Lock()
			quiet := now.Sub(c.lastHeard)
			c.mu.Unlock()
			if quiet > silence {
				c.close(CloseNoHeartbeat, "no heartbeat")
				return
			}
		}
	}
}

// Serve runs one connection: it upgrades the request, says hello, waits for
// the client's resume, replays what the client missed, says ready, and then
// reads the client's frames until the connection ends. The caller has
// already authenticated the request and checked its origin and subprotocol.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, session model.Session, account model.Account) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{Subprotocol},
		// The API layer has applied Churr's own origin rule, which
		// differs by credential; the library's check would refuse apps.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return // Accept has already answered
	}
	ws.SetReadLimit(maxFrame)
	// The request's context ends when the handler returns, and the
	// connection's work must end with it either way.
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	now := time.Now()
	c := &conn{
		hub: h, ws: ws, id: newConnectionID(), session: session, account: account, cancel: cancel,
		wake: make(chan struct{}, 1), replaying: true, replayed: map[model.ConversationID]int64{},
		// A client that has just connected is one someone is using.
		lastActive: now, lastHeard: now,
	}
	// hello is queued before the connection can be sent anything else, so
	// it is always the first frame, whatever is happening on the server.
	c.send(map[string]any{
		"type": "hello", "connection_id": c.id, "account_id": account.ID, "session_id": session.ID,
		"heartbeat_seconds":     int(h.opts.Heartbeat / time.Second),
		"resume_within_seconds": int(h.opts.ResumeWithin / time.Second),
		// Counted with this connection, which is not registered yet.
		"reconnect_spread_seconds": reconnectSpread(h.Connected() + 1),
		"server_time":              model.Now(),
	})
	if !h.add(c) {
		ws.Close(websocket.StatusCode(CloseRestarting), "the server is restarting")
		cancel()
		return
	}
	defer func() {
		h.leaveResumeQueue(c)
		h.remove(c)
		// The account may now be away or offline to others. Looked at
		// in its turn, so a crowd leaving together does not take the
		// database from the crowd coming back (resume.go).
		h.accountLeft(account.ID)
		// Whoever follows single connections hears that this one went.
		h.connClosed(c)
	}()
	resumed := make(chan struct{})
	go c.writer(ctx)
	go c.watchdog(ctx, resumed)

	c.readLoop(ctx, resumed)
	c.close(CloseNormal, "")
	// The close handshake runs beside this and ends the context when it
	// is done. Returning before that would end the context under a frame
	// the writer is still sending, which cuts the socket and loses the
	// close code the client is owed.
	<-ctx.Done()
}

// clientFrame is the part every client frame has.
type clientFrame struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// strict decodes a frame, refusing fields the frame type does not have.
func strict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func (c *conn) readLoop(ctx context.Context, resumed chan struct{}) {
	var (
		gotResume  bool
		windowAt   = time.Now()
		inWindow   int
		over       int
		unreadable []time.Time
	)
	bad := func(replyTo string, e *model.Error) bool {
		f := map[string]any{"type": "error", "error": e}
		if replyTo != "" {
			f["reply_to"] = replyTo
		}
		c.send(f)
		now := time.Now()
		keep := unreadable[:0]
		for _, t := range unreadable {
			if now.Sub(t) < time.Minute {
				keep = append(keep, t)
			}
		}
		unreadable = append(keep, now)
		if len(unreadable) >= unreadablePerMin {
			c.close(CloseUnreadable, "too many frames the server could not read")
			return false
		}
		return true
	}
	for {
		typ, data, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		now := time.Now()
		c.mu.Lock()
		c.lastHeard = now
		c.mu.Unlock()

		// At most twenty frames a second. Past that the frame is dropped
		// with an error; a client that keeps going is closed.
		if now.Sub(windowAt) >= time.Second {
			windowAt, inWindow = now, 0
		}
		inWindow++
		if inWindow > framesPerSecond {
			over++
			if over > floodFrames {
				c.close(CloseTooManySends, "the client sent too much")
				return
			}
			c.send(map[string]any{"type": "error", "error": model.RateLimited(1)})
			continue
		}

		if typ != websocket.MessageText {
			if !bad("", model.BadRequest("Frames are text frames holding one JSON object.")) {
				return
			}
			continue
		}
		var head clientFrame
		if err := json.Unmarshal(data, &head); err != nil || head.Type == "" || len(head.ID) > 64 {
			if !bad("", model.BadRequest("That frame could not be read.")) {
				return
			}
			continue
		}
		ack := func() {
			if head.ID != "" {
				c.send(map[string]any{"type": "ack", "reply_to": head.ID})
			}
		}

		if !gotResume {
			var f struct {
				clientFrame
				Positions map[string]int64 `json:"positions"`
			}
			if head.Type != "resume" || strict(data, &f) != nil {
				if !bad(head.ID, model.BadRequest("The first frame must be `resume` with `positions`.")) {
					return
				}
				continue
			}
			gotResume = true
			close(resumed)
			// The catch-up is worked beside this loop, when its turn
			// comes (resume.go), so that pings are answered meanwhile.
			if !c.hub.enqueueResume(ctx, c, f.Positions, ack) {
				c.close(CloseTooManySends, "too many clients are reconnecting; try again shortly")
				return
			}
			continue
		}

		switch head.Type {
		case "ping":
			var f clientFrame
			if strict(data, &f) != nil {
				if !bad(head.ID, model.BadRequest("`ping` takes no fields.")) {
					return
				}
				continue
			}
			// A session that ran out while connected ends here.
			if !c.session.ExpiresAt.IsZero() && c.session.ExpiresAt.Before(model.Now()) {
				c.close(CloseCredential, "the session ended")
				return
			}
			pong := map[string]any{"type": "pong", "server_time": model.Now()}
			if head.ID != "" {
				pong["reply_to"] = head.ID
			}
			c.send(pong)
		case "activity":
			var f clientFrame
			if strict(data, &f) != nil {
				if !bad(head.ID, model.BadRequest("`activity` takes no fields.")) {
					return
				}
				continue
			}
			c.mu.Lock()
			c.lastActive = now
			waiting := c.replaying
			c.mu.Unlock()
			// Someone who was away is online again. A connection that
			// is still catching up is announced when it is done.
			if !waiting {
				c.hub.presenceCheck(c.account.ID)
			}
			ack()
		case "resume":
			if !bad(head.ID, model.BadRequest("`resume` is sent once, first.")) {
				return
			}
		default:
			hd := c.hub.handlerFor(head.Type)
			if hd == nil {
				if !bad(head.ID, model.BadRequest("That frame type is not known.")) {
					return
				}
				continue
			}
			err := hd(ctx, Client{ConnectionID: c.id, Session: c.session, Account: c.account}, head.Type, data)
			if errors.Is(err, ErrAnswered) {
				// The handler wrote its own answer (direct.go).
				continue
			}
			if err != nil {
				e, ok := model.AsError(err)
				if !ok {
					c.hub.opts.Log.Error("a client frame failed", "type", head.Type, "err", err)
					e = model.ErrInternal
				}
				f := map[string]any{"type": "error", "error": e}
				if head.ID != "" {
					f["reply_to"] = head.ID
				}
				c.send(f)
				continue
			}
			ack()
		}
	}
}

// replay answers `resume`: for each conversation the client holds it sends
// the events the client missed, or one reload frame; then the events that
// arrived meanwhile; then ready.
func (c *conn) replay(ctx context.Context, positions map[string]int64) {
	h := c.hub
	convs := make([]string, 0, len(positions))
	for id := range positions {
		convs = append(convs, id)
	}
	sort.Strings(convs)
	direct := func(v any) {
		b, err := json.Marshal(v)
		if err != nil {
			return
		}
		c.mu.Lock()
		if !c.closed {
			c.enqueueLocked(b)
		}
		c.mu.Unlock()
	}
	reload := func(conv string, reason string, lastSeq int64) {
		f := map[string]any{"type": "reload", "conversation_id": conv, "reason": reason}
		if reason == "too_far_behind" {
			f["last_seq"] = lastSeq
		}
		direct(f)
	}
	mark := func(conv model.ConversationID, seq int64) {
		c.mu.Lock()
		if seq > c.replayed[conv] {
			c.replayed[conv] = seq
		}
		c.mu.Unlock()
	}

	for _, raw := range convs {
		pos := positions[raw]
		if !model.ValidID(raw) || pos < 0 {
			reload(raw, "unknown", 0)
			continue
		}
		conv := model.ConversationID(raw)
		st, err := h.perm.Load(ctx, c.account, conv)
		if err != nil || !st.CanSee() {
			// One answer whether the conversation does not exist or the
			// account may not know that it does.
			reload(raw, "unknown", 0)
			continue
		}
		if !st.IsMember() {
			reload(raw, "no_longer_member", 0)
			continue
		}
		last := st.Conversation.LastSeq
		mark(conv, pos)
		if pos >= last {
			continue
		}
		oldest, err := h.store.Events().Oldest(ctx, conv)
		if err != nil {
			h.opts.Log.Error("catch-up could not read the log", "err", err)
			reload(raw, "too_far_behind", last)
			mark(conv, last)
			continue
		}
		// The log keeps thirty days. If it no longer starts where the
		// client left off, or the client is very far behind, loading the
		// newest page is cheaper and always right.
		if oldest == 0 || oldest > pos+1 || last-pos > int64(h.opts.ReplayLimit) {
			reload(raw, "too_far_behind", last)
			mark(conv, last)
			continue
		}
		after := pos
		for {
			events, more, err := h.store.Events().List(ctx, conv, after, 0, model.MaxLimit)
			if err != nil {
				h.opts.Log.Error("catch-up could not read the log", "err", err)
				break
			}
			for _, e := range events {
				if frame := h.Tailored(ctx, e, c.account.ID); frame != nil {
					c.mu.Lock()
					if !c.closed {
						c.enqueueLocked(frame)
					}
					c.mu.Unlock()
				}
				after = e.Seq
				mark(conv, e.Seq)
			}
			if !more || len(events) == 0 {
				break
			}
		}
	}

	// Conversations the account is in that the client did not name: ones
	// joined while away, or all of them on a first connection.
	var fresh []model.ConversationID
	if members, err := h.store.Members().OfAccount(ctx, c.account.ID); err == nil {
		for _, m := range members {
			if _, known := positions[string(m.ConversationID)]; !known {
				fresh = append(fresh, m.ConversationID)
			}
		}
	} else {
		h.opts.Log.Error("catch-up could not list conversations", "err", err)
	}
	if fresh == nil {
		fresh = []model.ConversationID{}
	}
	sort.Slice(fresh, func(i, j int) bool { return fresh[i] < fresh[j] })

	// Hand over to live delivery in one step, so no event can slip
	// between the held ones and the first live one.
	c.mu.Lock()
	for _, e := range c.held {
		if e.seq > c.replayed[e.conv] && !c.closed {
			c.enqueueLocked(e.frame)
		}
	}
	c.held = nil
	c.replaying = false
	if !c.closed {
		b, _ := json.Marshal(map[string]any{"type": "ready", "new_conversation_ids": fresh})
		c.enqueueLocked(b)
	}
	// Presence that changed while the client was catching up goes now, in
	// one frame, the newest value for each account (coalesce.go).
	c.flushPresenceLocked(nil)
	c.mu.Unlock()
}

// Compile-time check that the store offers what catch-up reads.
var _ interface {
	Events() store.Events
	Members() store.Members
} = store.Store(nil)
