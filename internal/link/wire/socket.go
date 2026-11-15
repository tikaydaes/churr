package wire

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/coder/websocket"
)

// The link writes its own data frames.
//
// The rule of section 2 is by result: a frame goes deflated if that makes it
// smaller and as it is if not. The WebSocket library decides by size alone
// and has no way to be told "this message, as it is". So, as the live
// connection does for its own reasons (internal/live/prepared.go), the
// library accepts or dials the connection, reads everything, and writes the
// control frames (pong, close), and the link builds and writes each data
// frame itself: one final binary frame, with the "deflated" bit set or not
// (RFC 6455 section 5.2, RFC 7692 section 7.2.1). Both write through the
// socket below, whose lock keeps whole frames whole.

// socket is the way out of one connection, as the library and the link
// share it: the hijacked connection on the listener's side, the body of the
// answer to the upgrade on the dialler's.
type socket struct {
	w io.Writer
	// masked: this side dialled, and a client's frames are masked.
	masked bool

	mu sync.Mutex
	// closeSent is true once a close frame has gone out. Nothing may
	// follow one.
	closeSent bool
}

// errNotOneFrame is what the library is told if it ever hands the socket
// anything but one whole frame, which would let a frame of the link's land
// in the middle of one of its own. It does not, for the control frames it
// is left to write: each is at most 125 bytes and is flushed by itself. This
// makes sure a change to that is noticed and not sent.
var errNotOneFrame = errors.New("link: the WebSocket library wrote part of a frame")

// Write is the library's way out.
func (s *socket) Write(p []byte) (int, error) {
	if !oneFrame(p) {
		return 0, errNotOneFrame
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.w.Write(p)
	if err == nil && p[0]&0x0f == 0x8 {
		s.closeSent = true
	}
	return n, err
}

// oneFrame reports whether p is exactly one whole, final frame, masked or
// not.
func oneFrame(p []byte) bool {
	if len(p) < 2 || p[0]&0x80 == 0 {
		return false
	}
	head := 2
	if p[1]&0x80 != 0 {
		head += 4
	}
	switch n := int(p[1] & 0x7f); n {
	case 126:
		return len(p) >= 4 && len(p) == head+2+int(binary.BigEndian.Uint16(p[2:]))
	case 127:
		return len(p) >= 10 && uint64(len(p)) == uint64(head)+8+binary.BigEndian.Uint64(p[2:])
	default:
		return len(p) == head+n
	}
}

// send writes one binary message of the link's own in one frame. deflated
// says the payload is compressed (deflated in compress.go). The write ends,
// and the connection with it, when ctx does. The caller keeps the order of
// its own frames; the lock here only keeps the library's apart from them.
func (s *socket) send(ctx context.Context, ws *websocket.Conn, payload []byte, deflated bool) error {
	first := byte(0x80 | 0x2) // FIN, binary
	if deflated {
		first |= 0x40 // RSV1: this message is compressed
	}
	mask := byte(0)
	if s.masked {
		mask = 0x80
	}
	frame := make([]byte, 0, len(payload)+14)
	switch n := len(payload); {
	case n <= 125:
		frame = append(frame, first, mask|byte(n))
	case n <= 0xffff:
		frame = append(frame, first, mask|126, byte(n>>8), byte(n))
	default:
		frame = binary.BigEndian.AppendUint64(append(frame, first, mask|127), uint64(n))
	}
	if s.masked {
		var key [4]byte
		if _, err := rand.Read(key[:]); err != nil {
			return err
		}
		frame = append(frame, key[:]...)
		at := len(frame)
		frame = append(frame, payload...)
		for i, body := 0, frame[at:]; i < len(body); i++ {
			body[i] ^= key[i&3]
		}
	} else {
		frame = append(frame, payload...)
	}
	// A write that outlasts ctx ends as the library's own do: the
	// connection is closed under it.
	stop := context.AfterFunc(ctx, func() { ws.CloseNow() })
	defer stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closeSent {
		return net.ErrClosed
	}
	_, err := s.w.Write(frame)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// takenResponse hands the WebSocket library a socket as its way out of an
// accepted connection, and keeps it for the link.
type takenResponse struct {
	http.ResponseWriter
	out *socket
}

// takenConn is the hijacked connection with its writes sent through the
// socket.
type takenConn struct {
	net.Conn
	out *socket
}

func (c *takenConn) Write(p []byte) (int, error) { return c.out.Write(p) }

// Hijack takes the connection over from the HTTP server, as the library
// asks.
func (r *takenResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	// Whatever wraps the server's own writer is looked through.
	nc, brw, err := http.NewResponseController(r.ResponseWriter).Hijack()
	if err != nil {
		return nil, nil, err
	}
	r.out = &socket{w: nc}
	conn := &takenConn{Conn: nc, out: r.out}
	// The HTTP server's writer goes straight to its own connection; the
	// library must write through ours. Nothing is waiting in the writer:
	// the answer to the upgrade was flushed before this.
	brw.Writer.Reset(conn)
	return conn, brw, nil
}

// tap is the dialler's HTTP transport for one dial: it keeps the body of
// the answer to the upgrade, which is the connection, and gives the library
// one that writes through the socket.
type tap struct {
	base http.RoundTripper
	out  *socket
}

type tappedBody struct {
	io.ReadWriteCloser
	out *socket
}

func (b *tappedBody) Write(p []byte) (int, error) { return b.out.Write(p) }

func (t *tap) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusSwitchingProtocols {
		return resp, err
	}
	if rwc, ok := resp.Body.(io.ReadWriteCloser); ok {
		t.out = &socket{w: rwc, masked: true}
		resp.Body = &tappedBody{ReadWriteCloser: rwc, out: t.out}
	}
	return resp, nil
}

// tapped returns the HTTP client for one dial, and the tap that will hold
// the connection's socket once the dial has succeeded.
func tapped(c *http.Client) (*http.Client, *tap) {
	var own http.Client
	if c != nil {
		own = *c
	}
	t := &tap{base: own.Transport}
	if t.base == nil {
		t.base = http.DefaultTransport
	}
	own.Transport = t
	return &own, t
}
