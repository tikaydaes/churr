package live

import (
	"bytes"
	"container/list"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/wirepb"
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
	hub *Hub
	ws  *websocket.Conn
	// nc is the socket under ws, which the hub writes data frames to
	// itself (flush.go), and deflate says the client asked for them
	// compressed (prepared.go).
	nc      *wireConn
	deflate bool
	// proto says the connection negotiated protobuf: every frame is an
	// Envelope in a binary message, both ways (prepared.go).
	proto   bool
	id      string
	session model.Session
	account model.Account
	cancel  context.CancelFunc

	mu         sync.Mutex
	queue      [][]byte
	queueBytes int
	// urgent is the answers to pings, which go before anything in queue:
	// a client that is far behind must still hear that the connection is
	// alive, or it gives up on one that was only busy.
	urgent [][]byte
	// writing is true while the writer has a frame it has not finished
	// handing to the socket.
	writing bool
	// scheduled is true while someone has this connection to write: it
	// is on its flusher's list, or a goroutine is emptying its queue.
	// started is false until Serve lets it be written (flush.go).
	scheduled bool
	started   bool
	flusher   *flusher
	closed    bool
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
	// timer waits for `resume`, and after it for silence (watch).
	timer *time.Timer

	// pending is the presence changes waiting to be sent together, oldest
	// first; pendingDue is when they should go, and pendingListed says the
	// hub has this connection on its list of ones to send to (coalesce.go).
	pending       []*presenceItem
	pendingDue    time.Time
	pendingListed bool
	// lastItem is the presence change this connection was last handed, so
	// one addressed to it twice is taken once.
	lastItem *presenceItem

	// What the connection has open (lazy.go). declared is false until the
	// client first says: a client from before this, or a bot, which is
	// sent everything. opening holds the live events of a conversation
	// while it is brought up to date after being opened; a conversation
	// is in it only for that long. lazy is what has not been told yet
	// about each lazy conversation that is not open, and signalListed
	// says the hub has this connection on its list of ones to tell.
	// countsSent is the presence.counts it was last sent.
	declared     bool
	open         map[model.ConversationID]struct{}
	opening      map[model.ConversationID][]heldEvent
	lazy         map[model.ConversationID]*lazyState
	signalListed bool
	countsSent   string
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
	c.sendEventLocked(conv, seq, frame)
}

func (c *conn) sendEventLocked(conv model.ConversationID, seq int64, frame []byte) {
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
		if held, opening := c.opening[conv]; opening {
			// The conversation was just opened and is being brought up
			// to date from the log (lazy.go): this waits its turn.
			c.opening[conv] = append(held, heldEvent{conv, seq, frame})
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
	c.hub.count(kindControl, 1, len(b))
	b = c.own(b)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.enqueueLocked(b)
	}
}

// maxUrgent is how many answers to pings may wait. A client pings twice a
// minute; one with this many unanswered is not taking what it is sent.
const maxUrgent = 16

// sendUrgent queues a frame ahead of everything waiting in the send queue.
// It is for `pong` alone: nothing else may overtake an event.
func (c *conn) sendUrgent(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	size := len(b)
	b = c.own(b)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if len(c.urgent) >= maxUrgent {
		c.closeLocked(CloseQueueFull, "the send queue filled")
		return
	}
	c.hub.count(kindControl, 1, size)
	c.urgent = append(c.urgent, b)
	c.scheduleLocked()
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
	c.scheduleLocked()
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
	c.queue, c.urgent, c.held, c.pending = nil, nil, nil, nil
	if c.timer != nil {
		c.timer.Stop()
	}
	switch code {
	case CloseQueueFull:
		c.hub.wire.slow.Add(1)
	case CloseNoHeartbeat:
		c.hub.wire.silent.Add(1)
	}
	// The close handshake waits for the peer; whoever asked for the close
	// (often a fan-out) must not. The connection's context is ended only
	// afterwards: ending it first would cut the socket under the reader
	// before the close frame, and its code, had been sent.
	go func() {
		c.ws.Close(websocket.StatusCode(code), reason)
		c.cancel()
	}()
}

// A connection is closed if no `resume` arrives in time, and later if the
// client has sent nothing for three heartbeats. Both are timers and not a
// goroutine that waits: a goroutine for each connection is several
// kilobytes of stack for each, held for as long as it is connected, to do
// something twice a minute.

// watch starts the wait for `resume`.
func (c *conn) watch() {
	c.mu.Lock()
	if !c.closed {
		c.timer = time.AfterFunc(c.hub.opts.ResumeWithin, func() {
			c.close(CloseNoResume, "no resume arrived in time")
		})
	}
	c.mu.Unlock()
}

// resumed ends the wait for `resume` and starts listening for silence.
func (c *conn) resumed() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.timer.Stop()
	c.timer = time.AfterFunc(c.hub.opts.Heartbeat/2, c.checkHeard)
}

// checkHeard runs every half heartbeat once the client has resumed.
func (c *conn) checkHeard() {
	now := time.Now()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	quiet := now.Sub(c.lastHeard)
	behind := c.writing || len(c.queue) > 0 || len(c.urgent) > 0
	if quiet <= 3*c.hub.opts.Heartbeat {
		c.timer.Reset(c.hub.opts.Heartbeat / 2)
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	if behind {
		// Nothing was heard, and frames are waiting that the client
		// has not taken: it is not gone but buried, or its pings are
		// stuck behind what it has not read. It is closed as any slow
		// consumer is, and told so, not as a dead connection.
		c.close(CloseQueueFull, "the client fell behind and went quiet")
	} else {
		c.close(CloseNoHeartbeat, "no heartbeat")
	}
}

// Serve starts one connection: it upgrades the request and says hello. The
// connection then runs on a goroutine of its own (run), which waits for the
// client's resume, has what the client missed replayed, says ready, and
// reads the client's frames until the connection ends. The caller has
// already authenticated the request and checked its origin and subprotocol.
//
// Serve returns once the connection is up, not when it ends. The goroutine
// the HTTP server gave the request has by then a stack grown by everything
// the request passed through (TLS, routing, signing in, the upgrade) and
// holds the request with its headers; kept for the life of the connection,
// that was the largest thing a connection held. The one that reads starts
// small and stays small.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, session model.Session, account model.Account) {
	// Protobuf if the client offered it, and then always compressed
	// (ruling R65): a client that offers protobuf and no compression is
	// refused before the upgrade, and NoCompression does not apply.
	subprotocol, _, proto := wirepb.ChooseSubprotocol(offeredSubprotocols(r))
	if e := Refusal(r); e != nil {
		http.Error(w, e.Message, e.Status())
		return
	}
	mode := websocket.CompressionNoContextTakeover
	if h.opts.NoCompression && !proto {
		mode = websocket.CompressionDisabled
	}
	if !proto {
		subprotocol = Subprotocol
	}
	// Short of memory, nothing is kept for one more connection: it is
	// told to come back later (memory.go).
	if h.opts.Admit != nil && !h.opts.Admit(r.Context()) {
		h.refuse(w, r, subprotocol, mode)
		return
	}
	// The socket counts what is written to it (wire.go).
	taken := &takenResponse{ResponseWriter: w, sent: &h.wire.bytes}
	ws, err := websocket.Accept(taken, r, &websocket.AcceptOptions{
		Subprotocols: []string{subprotocol},
		// Compressed for a client that asks, each message on its own
		// (wire.go).
		CompressionMode:      mode,
		CompressionThreshold: compressFrom,
		// The API layer has applied Churr's own origin rule, which
		// differs by credential; the library's check would refuse apps.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return // Accept has already answered
	}
	ws.SetReadLimit(maxFrame)
	deflate := w.Header().Get("Sec-WebSocket-Extensions") != ""
	if proto && !deflate {
		// The client named compression in a form this server does not
		// speak. Protobuf is never sent uncompressed, so this ends here.
		ws.Close(websocket.StatusProtocolError, "protobuf needs permessage-deflate")
		return
	}
	// The request's context ends when the handler returns, and the
	// connection's work must end with it either way.
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	now := time.Now()
	c := &conn{
		hub: h, ws: ws, nc: taken.conn, deflate: deflate, proto: proto, id: newConnectionID(), session: session, account: account, cancel: cancel,
		flusher: flusherFor(), replaying: true, replayed: map[model.ConversationID]int64{},
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
		// The server sends presence by interest (interest.go): a client
		// that sees this says whom it is showing.
		"presence_subscribe_max": MaxPresenceInterest,
		// Large channels are delivered in full to the connections that
		// have them open (lazy.go): a client that sees this says which
		// conversations it has open.
		"conversations_open_max": MaxOpenConversations,
		"server_time":            model.Now(),
	})
	h.statsOnce.Do(func() {
		go h.logStats()
		go h.runLazy()
	})
	if !h.add(c) {
		ws.Close(websocket.StatusCode(CloseRestarting), "the server is restarting")
		cancel()
		return
	}
	c.start()
	c.watch()
	go c.run(ctx)
}

// run is the life of a connection after hello.
func (c *conn) run(ctx context.Context) {
	h := c.hub
	defer func() {
		h.leaveResumeQueue(c)
		h.remove(c)
		// The account may now be away or offline to others. Looked at
		// in its turn, so a crowd leaving together does not take the
		// database from the crowd coming back (resume.go).
		h.accountLeft(c.account.ID)
		// Whoever follows single connections hears that this one went.
		h.connClosed(c)
	}()
	func() {
		// A frame whose handling panics ends its connection, not the
		// server: this goroutine is no longer under the API's own net.
		defer func() {
			if p := recover(); p != nil {
				h.opts.Log.Error("a client frame's handler panicked", "panic", fmt.Sprint(p))
				c.close(int(websocket.StatusInternalError), "the server could not handle a frame")
			}
		}()
		c.readLoop(ctx)
	}()
	c.close(CloseNormal, "")
	// The close handshake runs beside this and ends the context when it
	// is done. Returning before that would end the context under a frame
	// the writer is still sending, which cuts the socket and loses the
	// close code the client is owed.
	<-ctx.Done()
}

// offeredSubprotocols is what the client asked for in Sec-WebSocket-Protocol.
func offeredSubprotocols(r *http.Request) []string {
	var out []string
	for _, h := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, p := range strings.Split(h, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// Refusal says why an upgrade request cannot become a live connection, or
// nil if it can: it must ask for `churr.v1` or the protobuf subprotocol, and
// with protobuf it must offer permessage-deflate, because protobuf is never
// sent uncompressed (ruling R65). The API layer asks before it hands the
// request to Serve, so the client gets an error in the usual shape.
func Refusal(r *http.Request) *model.Error {
	offered := offeredSubprotocols(r)
	if _, _, proto := wirepb.ChooseSubprotocol(offered); proto {
		for _, h := range r.Header.Values("Sec-WebSocket-Extensions") {
			for _, ext := range strings.Split(h, ",") {
				name, _, _ := strings.Cut(ext, ";")
				if strings.EqualFold(strings.TrimSpace(name), "permessage-deflate") {
					return nil
				}
			}
		}
		return model.BadRequest("The protobuf subprotocol needs compression: offer permessage-deflate in Sec-WebSocket-Extensions.")
	}
	if !slices.Contains(offered, Subprotocol) {
		return model.BadRequest("Ask for the subprotocol churr.v1, or " + wirepb.SubprotocolFor(wirepb.SchemaVersion) + " for protobuf.")
	}
	return nil
}

// reading is the context of every read (readLoop).
var reading = context.Background()

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

func (c *conn) readLoop(ctx context.Context) {
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
		// Read with a context that cannot end: the library sets a
		// watch on one that can for every frame it reads, which is an
		// allocation a frame and a table kept for each connection.
		// Nothing is lost by it: the connection's context is ended
		// only after the socket is closed (closeLocked), and closing
		// the socket is what ends a read.
		typ, data, err := c.ws.Read(reading)
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

		if c.proto {
			// The Envelope is read into the JSON of its frame, and from
			// here on the frame is handled as any other.
			if typ == websocket.MessageBinary {
				data, err = wirepb.DecodeClientFrameJSON(data)
			}
			if typ != websocket.MessageBinary || err != nil {
				if !bad("", model.BadRequest("Frames are binary frames holding one protobuf Envelope.")) {
					return
				}
				continue
			}
		} else if typ != websocket.MessageText {
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
				// Open is the conversations on the client's screen
				// (lazy.go). A client that sends it, even empty, is one
				// that says what it has open.
				Open *[]string `json:"open"`
			}
			if head.Type != "resume" || strict(data, &f) != nil || (f.Open != nil && len(*f.Open) > MaxOpenConversations) {
				if !bad(head.ID, model.BadRequest("The first frame must be `resume` with `positions`, and at most 8 conversations in `open`.")) {
					return
				}
				continue
			}
			if f.Open != nil {
				// Before the catch-up, which brings them up to date
				// with every other conversation the client named.
				c.declare(c.hub.memberOf(ctx, c.account.ID, *f.Open), nil)
			}
			gotResume = true
			c.resumed()
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
			// Ahead of whatever is queued: a long queue must not make
			// a live connection look dead to its client.
			c.sendUrgent(pong)
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
		case "conversations.open":
			if e := c.openConversations(ctx, data); e != nil {
				if !bad(head.ID, e) {
					return
				}
				continue
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
	reload := c.sendReload

	// What the account may see is read for every conversation the client
	// named at once, so a client with nothing to catch up on costs a few
	// queries however many conversations it holds. If that fails, each is
	// read on its own.
	var ask []model.ConversationID
	for _, raw := range convs {
		if model.ValidID(raw) && positions[raw] >= 0 {
			ask = append(ask, model.ConversationID(raw))
		}
	}
	states, err := h.perm.LoadMany(ctx, c.account, ask)
	if err != nil {
		states = nil
	}
	// What differs per reader in the events is worked out a page at a
	// time, from what has just been read where that covers it (hub.go).
	known := make([]perm.State, 0, len(states))
	for _, st := range states {
		if st.IsMember() {
			known = append(known, st)
		}
	}
	tailored := h.tailoredFor(c.account.ID, known)

	for _, raw := range convs {
		pos := positions[raw]
		if !model.ValidID(raw) || pos < 0 {
			reload(raw, "unknown", 0)
			continue
		}
		conv := model.ConversationID(raw)
		st, found := states[conv]
		var err error
		if states == nil {
			st, err = h.perm.Load(ctx, c.account, conv)
		} else if !found {
			err = model.ErrNotFound
		}
		c.catchUp(ctx, raw, pos, st, err, tailored)
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

	ready, _ := json.Marshal(map[string]any{"type": "ready", "new_conversation_ids": fresh})
	ready = c.own(ready)

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
		c.enqueueLocked(ready)
	}
	// Presence that changed while the client was catching up goes now, in
	// one frame, the newest value for each account (coalesce.go).
	c.flushPresenceLocked(nil)
	c.mu.Unlock()
}

// sendReload tells the client to load a conversation again instead of
// catching up on it.
func (c *conn) sendReload(conv string, reason string, lastSeq int64) {
	f := map[string]any{"type": "reload", "conversation_id": conv, "reason": reason}
	if reason == "too_far_behind" {
		f["last_seq"] = lastSeq
	}
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	c.hub.count(kindControl, 1, len(b))
	b = c.own(b)
	c.mu.Lock()
	if !c.closed {
		c.enqueueLocked(b)
	}
	c.mu.Unlock()
}

// markReplayed notes that the client has a conversation's events up to seq.
func (c *conn) markReplayed(conv model.ConversationID, seq int64) {
	c.mu.Lock()
	if seq > c.replayed[conv] {
		c.replayed[conv] = seq
	}
	c.mu.Unlock()
}

// catchUp sends one conversation's events after pos, or one reload frame.
// st and err are what perm said of the account there. It is the catch-up of
// `resume` for each conversation the client named, and of a conversation a
// client opens after it was only told that it changed (lazy.go).
func (c *conn) catchUp(ctx context.Context, raw string, pos int64, st perm.State, err error, tailored func(ctx context.Context, events []model.Event) []json.RawMessage) {
	h := c.hub
	conv := model.ConversationID(raw)
	reload, mark := c.sendReload, c.markReplayed
	if err != nil || !st.CanSee() {
		// One answer whether the conversation does not exist or the
		// account may not know that it does.
		reload(raw, "unknown", 0)
		return
	}
	if !st.IsMember() {
		reload(raw, "no_longer_member", 0)
		return
	}
	last := st.Conversation.LastSeq
	mark(conv, pos)
	if pos >= last {
		return
	}
	// The log keeps thirty days. If it no longer starts where the
	// client left off, or the client is very far behind, loading the
	// newest page is cheaper and always right.
	if last-pos > int64(h.opts.ReplayLimit) {
		reload(raw, "too_far_behind", last)
		mark(conv, last)
		return
	}
	after := pos
	for first := true; ; first = false {
		events, more, err := h.store.Events().List(ctx, conv, after, 0, model.MaxLimit)
		if err != nil {
			h.opts.Log.Error("catch-up could not read the log", "err", err)
			if first {
				reload(raw, "too_far_behind", last)
				mark(conv, last)
			}
			break
		}
		// The log starts where the client left off if it holds the
		// very next event, which is the usual case and is known from
		// the page just read. Only otherwise is its start looked up.
		if first && (len(events) == 0 || events[0].Seq != pos+1) {
			oldest, err := h.store.Events().Oldest(ctx, conv)
			if err != nil {
				h.opts.Log.Error("catch-up could not read the log", "err", err)
			}
			if err != nil || oldest == 0 || oldest > pos+1 {
				reload(raw, "too_far_behind", last)
				mark(conv, last)
				break
			}
		}
		frames := tailored(ctx, events)
		replayed, replayedBytes := 0, 0
		for _, frame := range frames {
			if frame != nil {
				replayed++
				replayedBytes += len(frame)
			}
		}
		h.count(kindReplay, replayed, replayedBytes)
		for i, e := range events {
			if frame := frames[i]; frame != nil {
				// Made for this reader, so encoded for it here.
				frame = c.own(frame)
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

// Compile-time check that the store offers what catch-up reads.
var _ interface {
	Events() store.Events
	Members() store.Members
} = store.Store(nil)
