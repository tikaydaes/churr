package wire

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// The waits of section 5.
const (
	// HandshakeTimeout is the most time between the first handshake frame
	// and the last.
	HandshakeTimeout = 10 * time.Second
	// PingEvery is how often a time reference pings.
	PingEvery = 30 * time.Second
	// DeadAfter is how long a connection may be silent before it is closed.
	DeadAfter = 90 * time.Second
	writeWait = 15 * time.Second
)

// Conn is one authenticated link connection. Frames are sent with Send and
// read with Read; one goroutine reads, any number send.
type Conn struct {
	ws *websocket.Conn
	// Remote is the other side's hello, already verified.
	Remote Frame
	// Dialler says whether this side opened the connection.
	Dialler bool

	sendMu sync.Mutex
	// q, when Queue has been called, holds what Send has been given and the
	// connection's own writer has not yet written.
	q      *sendQueue
	nextID atomic.Int64
	// heard is when a frame last arrived, in nanoseconds.
	heard  atomic.Int64
	closed atomic.Bool
	done   chan struct{}
}

func newConn(ws *websocket.Conn, remote Frame, dialler bool) *Conn {
	ws.SetReadLimit(MaxFrameBytes)
	c := &Conn{ws: ws, Remote: remote, Dialler: dialler, done: make(chan struct{})}
	c.heard.Store(time.Now().UnixNano())
	return c
}

// NextID gives the next frame id: a positive integer that only grows on
// this connection for this sender.
func (c *Conn) NextID() int64 { return c.nextID.Add(1) }

// Send writes one frame. It is safe from many goroutines.
func (c *Conn) Send(f Frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return c.SendRaw(b)
}

// sendNow writes a frame at once, ahead of anything queued: a ping and its
// pong, whose times are the measurement.
func (c *Conn) sendNow(f Frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return c.write(b)
}

// SendRaw writes bytes that are already a frame.
func (c *Conn) SendRaw(b []byte) error {
	if len(b) > MaxFrameBytes {
		return Errf(ETooLarge, "A frame is at most %d bytes.", MaxFrameBytes)
	}
	if c.q != nil {
		return c.enqueue(b, false)
	}
	return c.write(b)
}

func (c *Conn) write(b []byte) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), writeWait)
	defer cancel()
	if err := c.ws.Write(ctx, websocket.MessageText, b); err != nil {
		c.shut()
		return err
	}
	return nil
}

// sendQueue is what a connection whose sends are queued has yet to write.
type sendQueue struct {
	mu      sync.Mutex
	frames  [][]byte
	bytes   int
	max     int
	closing bool
	wake    chan struct{}
	drained chan struct{}
}

// Queue makes Send and SendRaw return at once from then on: frames are
// written by a goroutine of the connection's own, in the order they were
// given. A hub uses it for its servers, so that one server whose socket is
// slow holds up nobody else's envelopes. A connection whose unwritten frames
// pass maxBytes is closed: its other side catches up from the log when it
// returns. Queue is called once, before anything else sends.
func (c *Conn) Queue(maxBytes int) {
	c.q = &sendQueue{max: maxBytes, wake: make(chan struct{}, 1), drained: make(chan struct{})}
	go c.drain()
}

func (c *Conn) enqueue(b []byte, last bool) error {
	q := c.q
	q.mu.Lock()
	if c.closed.Load() || q.closing {
		q.mu.Unlock()
		return Errf(EUnavailable, "The connection has ended.")
	}
	if q.bytes+len(b) > q.max {
		q.mu.Unlock()
		c.Close()
		return Errf(EUnavailable, "The connection could not keep up and was closed.")
	}
	q.frames = append(q.frames, b)
	q.bytes += len(b)
	q.closing = last
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return nil
}

// drain is a queued connection's writer.
func (c *Conn) drain() {
	q := c.q
	defer close(q.drained)
	for {
		q.mu.Lock()
		frames, closing := q.frames, q.closing
		q.frames = nil
		q.mu.Unlock()
		for _, b := range frames {
			err := c.write(b)
			q.mu.Lock()
			q.bytes -= len(b)
			q.mu.Unlock()
			if err != nil {
				return
			}
		}
		if closing {
			return
		}
		if len(frames) > 0 {
			continue
		}
		select {
		case <-q.wake:
		case <-c.done:
			return
		}
	}
}

// Read returns the next frame and its exact bytes. A binary message, or one
// that is not a JSON object with a frame name, comes back as an *Error with
// code bad_frame and the connection stays usable.
func (c *Conn) Read(ctx context.Context) (Frame, []byte, error) {
	typ, b, err := c.ws.Read(ctx)
	if err != nil {
		c.shut()
		return Frame{}, nil, err
	}
	c.heard.Store(time.Now().UnixNano())
	if typ != websocket.MessageText {
		return Frame{}, b, Errf(EBadFrame, "Frames are text messages.")
	}
	var f Frame
	if err := json.Unmarshal(b, &f); err != nil || f.Frame == "" {
		return Frame{}, b, Errf(EBadFrame, "A frame is a JSON object with a frame name.")
	}
	return f, b, nil
}

// Watch closes the connection when nothing has arrived on it for DeadAfter.
// The heartbeat is the link's own ping and pong (clock.go): a time reference
// pings every PingEvery and the other side answers, so on a healthy
// connection each side hears from the other at least that often. The reader
// must be running, since it is the reader that notes what arrives.
func (c *Conn) Watch() {
	t := time.NewTicker(DeadAfter / 6)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			if time.Since(time.Unix(0, c.heard.Load())) > DeadAfter {
				c.Close()
				return
			}
		}
	}
}

func (c *Conn) shut() {
	if c.closed.CompareAndSwap(false, true) {
		close(c.done)
	}
}

// Done is closed when the connection has ended.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Close ends the connection at once.
func (c *Conn) Close() {
	c.shut()
	c.ws.CloseNow()
}

// CloseWith sends a last frame (an error or a bye) and then closes.
func (c *Conn) CloseWith(f Frame) {
	if c.q != nil {
		// After everything queued before it; and waited for, so that the
		// last word is on the wire before the caller lets the socket go.
		if b, err := json.Marshal(f); err == nil && c.enqueue(b, true) == nil {
			select {
			case <-c.q.drained:
			case <-time.After(writeWait):
			}
		}
	} else {
		c.Send(f)
	}
	c.shut()
	// The closing handshake waits for the other side, which may be gone;
	// nobody here needs to wait with it.
	go c.ws.Close(websocket.StatusNormalClosure, "")
}

// ---------------------------------------------------------------- handshake

// transcript is th of section 5: SHA-256("churr-link-hs-v1" 0x00 ||
// u32(len(h1)) || h1 || u32(len(h2)) || h2) over the two hellos exactly as
// sent.
func transcript(h1, h2 []byte) []byte {
	var l layout
	l.label("churr-link-hs-v1").u32(len(h1)).raw(h1).u32(len(h2)).raw(h2)
	sum := sha256.Sum256(l.Bytes())
	return sum[:]
}

// authMessage is what each side's handshake signature covers.
func authMessage(role string, th []byte) []byte {
	var l layout
	l.label("churr-link-auth-v1").label(role).raw(th)
	return l.Bytes()
}

func (id Identity) hello() Frame {
	nonce := make([]byte, 32)
	rand.Read(nonce)
	lim := DefaultLimits()
	f := Frame{Frame: FHello, Kind: id.Kind, Name: id.Name, SignKey: Bytes(id.Keys.SignPublic()), Nonce: nonce,
		Software: id.Software, Limits: &lim}
	if id.Kind == KindServer {
		f.BoxKey = id.Keys.BoxPublic()
		f.BoxKeySig = id.BoxKeySig()
	}
	return f
}

// helloSound checks what both sides check of the other's hello before
// anything else: a name, a kind, a signing key, and for a server a box key
// its signing key vouches for.
func helloSound(h Frame) bool {
	if h.Frame != FHello || h.Name == "" || len(h.SignKey) != 32 || len(h.Nonce) != 32 {
		return false
	}
	switch h.Kind {
	case KindServer:
		return VerifyBoxKey(h.Name, h.SignKey, h.BoxKey, h.BoxKeySig)
	case KindHub:
		return len(h.BoxKey) == 0
	}
	return false
}

// DialOptions is what the dialling side brings to a handshake.
type DialOptions struct {
	URL      string
	Identity Identity
	// Check is given the listener's hello after its box key signature has
	// been verified and before its auth: the dialler says whether name,
	// kind and key are what the invitation or the stored peer says.
	// Returning an error ends the handshake before anything derived from
	// the invitation secret is sent.
	Check func(listener Frame) error
	// InviteSecret is the invitation's secret, on the first connection of
	// a new peer only.
	InviteSecret []byte
	// HTTPClient carries the dial, for tests and proxies. Nil means the
	// default client.
	HTTPClient *http.Client
	// Versions overrides what the hello offers; tests use it.
	Versions []int
}

// loopback reports whether a host is this machine: the only place plain
// ws:// is accepted.
func loopback(host string) bool {
	if host == "localhost" || len(host) > 10 && host[len(host)-10:] == ".localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Dial opens a link connection and runs the dialler's half of the handshake.
// A refusal by the listener comes back as an *Error with the listener's code.
func Dial(ctx context.Context, o DialOptions) (*Conn, error) {
	u, err := url.Parse(o.URL)
	if err != nil || (u.Scheme != "wss" && u.Scheme != "ws") {
		return nil, errors.New("link: the address to dial must start with wss://")
	}
	if u.Scheme == "ws" && !loopback(u.Hostname()) {
		return nil, errors.New("link: plain ws:// is accepted only on loopback")
	}
	ctx, cancel := context.WithTimeout(ctx, HandshakeTimeout)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, o.URL, &websocket.DialOptions{Subprotocols: []string{Subprotocol}, HTTPClient: o.HTTPClient})
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(MaxFrameBytes)
	fail := func(err error) (*Conn, error) {
		ws.CloseNow()
		return nil, err
	}
	hello := o.Identity.hello()
	hello.Versions = o.Versions
	if hello.Versions == nil {
		hello.Versions = []int{Version}
	}
	h1, _ := json.Marshal(hello)
	if err := ws.Write(ctx, websocket.MessageText, h1); err != nil {
		return fail(err)
	}
	read := func() (Frame, []byte, error) {
		_, b, err := ws.Read(ctx)
		if err != nil {
			return Frame{}, nil, err
		}
		var f Frame
		if err := json.Unmarshal(b, &f); err != nil {
			return Frame{}, nil, Errf(EBadFrame, "The listener sent something that is not a frame.")
		}
		if f.Frame == FError {
			return f, b, &Error{Code: f.Code, Message: f.Message}
		}
		return f, b, nil
	}
	theirs, h2, err := read()
	if err != nil {
		return fail(err)
	}
	if theirs.Frame == FHello && theirs.Version != Version {
		// A listener of another version. Nothing more is sent: not this
		// side's signature, and nothing of an invitation.
		return fail(Errf(EVersionUnsupported, "The listener speaks version %d; this side speaks version %d only.", theirs.Version, Version))
	}
	if !helloSound(theirs) {
		return fail(Errf(EBadFrame, "The listener's hello is not sound."))
	}
	if o.Check != nil {
		if err := o.Check(theirs); err != nil {
			return fail(err)
		}
	}
	th := transcript(h1, h2)
	auth, _, err := read()
	if err != nil {
		return fail(err)
	}
	if auth.Frame != FAuth || !verify(theirs.SignKey, authMessage("listener", th), auth.Sig) {
		return fail(Errf(EAuthFailed, "The listener did not prove its key."))
	}
	// Only now is anything derived from the invitation secret revealed.
	mine := Frame{Frame: FAuth, Sig: o.Identity.Keys.Sign(authMessage("dialler", th))}
	if len(o.InviteSecret) > 0 {
		mine.InviteProof = InviteProof(InviteHash(o.InviteSecret), th)
	}
	b, _ := json.Marshal(mine)
	if err := ws.Write(ctx, websocket.MessageText, b); err != nil {
		return fail(err)
	}
	ready, _, err := read()
	if err != nil {
		return fail(err)
	}
	if ready.Frame != FReady {
		return fail(Errf(EBadFrame, "The listener did not finish the handshake."))
	}
	return newConn(ws, theirs, true), nil
}

// Authorizer is the listener's decision about a dialler whose hello and
// signature have verified: is this an active peer with these pinned keys,
// or does the proof match an invitation that can be used. th is the
// transcript hash the proof must be checked against. Returning an *Error
// refuses the dialler with that code.
type Authorizer func(dialler Frame, proof, th []byte) error

// Accept upgrades an HTTP request at /link/v1 and runs the listener's half of
// the handshake. It answers failures itself and returns an error; on
// success the connection is ready and the ready frame has been sent.
func Accept(w http.ResponseWriter, r *http.Request, id Identity, authorize Authorizer) (*Conn, error) {
	return AcceptKinds(w, r, id, authorize, KindServer)
}

// AcceptKinds is Accept for a listener that takes diallers of the kinds
// named: a hub takes servers, and other hubs over a trunk (section 23b).
func AcceptKinds(w http.ResponseWriter, r *http.Request, id Identity, authorize Authorizer, kinds ...string) (*Conn, error) {
	// The old subprotocol name is taken too, so that a dialler of version 1
	// is told version_unsupported below and does not just fail to connect.
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{Subprotocol, oldSubprotocol}, InsecureSkipVerify: true})
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(MaxFrameBytes)
	ctx, cancel := context.WithTimeout(r.Context(), HandshakeTimeout)
	defer cancel()
	refuse := func(e *Error) (*Conn, error) {
		b, _ := json.Marshal(e.Frame(0))
		ws.Write(ctx, websocket.MessageText, b)
		ws.Close(websocket.StatusPolicyViolation, e.Code)
		return nil, e
	}
	_, h1, err := ws.Read(ctx)
	if err != nil {
		ws.CloseNow()
		return nil, err
	}
	var theirs Frame
	if err := json.Unmarshal(h1, &theirs); err != nil || theirs.Frame != FHello {
		return refuse(Errf(EBadFrame, "The first frame must be a hello."))
	}
	if !slices.Contains(theirs.Versions, Version) {
		// Refused before this side says anything else: no hello, no
		// signature.
		return refuse(Errf(EVersionUnsupported, "This side speaks version %d only.", Version))
	}
	if !helloSound(theirs) || !slices.Contains(kinds, theirs.Kind) {
		return refuse(Errf(EAuthFailed, "The hello is not sound."))
	}
	hello := id.hello()
	hello.Version = Version
	h2, _ := json.Marshal(hello)
	if err := ws.Write(ctx, websocket.MessageText, h2); err != nil {
		ws.CloseNow()
		return nil, err
	}
	th := transcript(h1, h2)
	b, _ := json.Marshal(Frame{Frame: FAuth, Sig: id.Keys.Sign(authMessage("listener", th))})
	if err := ws.Write(ctx, websocket.MessageText, b); err != nil {
		ws.CloseNow()
		return nil, err
	}
	_, raw, err := ws.Read(ctx)
	if err != nil {
		ws.CloseNow()
		return nil, err
	}
	var auth Frame
	if err := json.Unmarshal(raw, &auth); err != nil || auth.Frame != FAuth {
		return refuse(Errf(EBadFrame, "An auth frame was expected."))
	}
	if !verify(theirs.SignKey, authMessage("dialler", th), auth.Sig) {
		return refuse(Errf(EAuthFailed, "The signature does not verify."))
	}
	if err := authorize(theirs, auth.InviteProof, th); err != nil {
		return refuse(AsError(err))
	}
	b, _ = json.Marshal(Frame{Frame: FReady})
	if err := ws.Write(ctx, websocket.MessageText, b); err != nil {
		ws.CloseNow()
		return nil, err
	}
	return newConn(ws, theirs, false), nil
}
