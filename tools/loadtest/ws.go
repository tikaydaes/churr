package main

import (
	"bufio"
	"bytes"
	"compress/flate"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/live"
)

// A WebSocket client with only what the load test needs: text frames in
// both directions, pings answered, and the one extension apps and browsers
// ask a server for, compression of each message on its own. It is
// written here, in a few lines of the standard library, so that the module's
// one WebSocket dependency stays with the packages that speak the protocol
// for the product, and so that five thousand connections cost this program
// as little as they can.

type wsConn struct {
	conn net.Conn
	r    *bufio.Reader
	wmu  sync.Mutex
	// cut is set before the storm drops this connection on purpose, so
	// its end is not counted as a connection the server lost.
	cut atomic.Bool
	// skip is how many bytes of the reader's buffer the last message
	// handed out still occupies; big holds a message too long for it.
	skip int
	big  []byte
	// deflate says the server agreed to compress; plain is where a
	// compressed message is unpacked, and wire is how many bytes the last
	// message read took on the socket, header and all.
	deflate bool
	plain   []byte
	wire    int
	// proto says the connection speaks protobuf: every message is binary
	// and holds one Envelope.
	proto bool
}

// offerDeflate is what a client that can read compressed messages says in
// the upgrade, and what the server must agree to: each message compressed
// on its own, which is all this client reads.
const offerDeflate = "permessage-deflate; client_no_context_takeover; server_no_context_takeover"

// inflater unpacks one compressed message. They are shared, since a
// connection needs one only for the moment a frame takes to read.
type inflater struct {
	fr  io.ReadCloser
	src bytes.Reader
	in  []byte
}

var inflaters = sync.Pool{New: func() any {
	in := &inflater{}
	in.fr = flate.NewReader(&in.src)
	return in
}}

// inflate unpacks a compressed message into c.plain. The sender left off
// the four bytes that end its last block (RFC 7692 section 7.2.1); they are
// put back, with an empty final block so the reader knows it has it all.
func (c *wsConn) inflate(payload []byte) ([]byte, error) {
	in := inflaters.Get().(*inflater)
	defer inflaters.Put(in)
	in.in = append(append(in.in[:0], payload...), "\x00\x00\xff\xff\x01\x00\x00\xff\xff"...)
	in.src.Reset(in.in)
	in.fr.(flate.Resetter).Reset(&in.src, nil)
	if cap(c.plain) > 64<<10 {
		c.plain = nil
	}
	c.plain = c.plain[:0]
	for {
		if len(c.plain) == cap(c.plain) {
			c.plain = append(c.plain, 0)[:len(c.plain)]
		}
		n, err := in.fr.Read(c.plain[len(c.plain):cap(c.plain)])
		c.plain = c.plain[:len(c.plain)+n]
		if err == io.EOF {
			return c.plain, nil
		}
		if err != nil {
			return nil, fmt.Errorf("a compressed frame could not be unpacked: %w", err)
		}
		if len(c.plain) > 8<<20 {
			return nil, errors.New("a frame larger than this client reads")
		}
	}
}

// dialWS opens a WebSocket to a ws:// or wss:// address with a bearer token
// and a subprotocol, asking for compression if compress is set.
func dialWS(address, bearer, subprotocol string, timeout time.Duration, compress bool) (*wsConn, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if u.Port() == "" {
		if u.Scheme == "wss" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	d := net.Dialer{Timeout: timeout}
	var conn net.Conn
	if u.Scheme == "wss" {
		conn, err = tls.DialWithDialer(&d, "tcp", host, &tls.Config{ServerName: u.Hostname(), NextProtos: []string{"http/1.1"}})
	} else {
		conn, err = d.Dial("tcp", host)
	}
	if err != nil {
		return nil, err
	}
	var nonce [16]byte
	rand.Read(nonce[:])
	ext := ""
	if compress {
		ext = "Sec-WebSocket-Extensions: " + offerDeflate + "\r\n"
	}
	req := "GET " + u.RequestURI() + " HTTP/1.1\r\nHost: " + u.Host + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + base64.StdEncoding.EncodeToString(nonce[:]) + "\r\n" + ext +
		"Sec-WebSocket-Protocol: " + subprotocol + "\r\nAuthorization: Bearer " + bearer + "\r\n\r\n"
	conn.SetDeadline(time.Now().Add(timeout))
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		return nil, err
	}
	r := bufio.NewReaderSize(conn, readBuffer)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		conn.Close()
		return nil, fmt.Errorf("the server answered %d: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Sec-WebSocket-Protocol"); got != subprotocol {
		conn.Close()
		return nil, fmt.Errorf("the server chose the subprotocol %q, not %q", got, subprotocol)
	}
	conn.SetDeadline(time.Time{})
	c := &wsConn{conn: conn, r: r, proto: subprotocol != live.Subprotocol}
	// A server that does not compress leaves the header out, and is read
	// as before. One that agrees to anything but what was offered would
	// send what this client cannot read.
	agreed := resp.Header.Get("Sec-WebSocket-Extensions")
	if c.proto && agreed == "" {
		conn.Close()
		return nil, errors.New("the server agreed to protobuf without compression")
	}
	if agreed != "" {
		if !compress || !strings.Contains(agreed, "permessage-deflate") || !strings.Contains(agreed, "server_no_context_takeover") {
			conn.Close()
			return nil, fmt.Errorf("the server chose an extension this client does not speak: %s", agreed)
		}
		c.deflate = true
	}
	return c, nil
}

// write sends one text frame. A client masks what it sends.
func (c *wsConn) write(opcode byte, payload []byte) error {
	var head [14]byte
	head[0] = 0x80 | opcode
	n := 2
	switch {
	case len(payload) < 126:
		head[1] = 0x80 | byte(len(payload))
	case len(payload) < 1<<16:
		head[1] = 0x80 | 126
		binary.BigEndian.PutUint16(head[2:], uint16(len(payload)))
		n = 4
	default:
		head[1] = 0x80 | 127
		binary.BigEndian.PutUint64(head[2:], uint64(len(payload)))
		n = 10
	}
	var mask [4]byte
	rand.Read(mask[:])
	copy(head[n:], mask[:])
	buf := make([]byte, 0, n+4+len(payload))
	buf = append(buf, head[:n+4]...)
	for i, b := range payload {
		buf = append(buf, b^mask[i%4])
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_, err := c.conn.Write(buf)
	return err
}

func (c *wsConn) writeText(payload []byte) error { return c.write(1, payload) }

// writeFrame sends a frame given as its JSON, in the form the connection
// speaks: as it is, or as an Envelope in a binary message.
func (c *wsConn) writeFrame(text []byte) error { return c.writeMade(frameOut(c.proto, text)) }

// writeMade sends a frame frameOut already made for this kind of
// connection. (It is not compressed: a client may send any message plain,
// and what this one sends is a few bytes.)
func (c *wsConn) writeMade(frame []byte) error {
	if c.proto {
		return c.write(2, frame)
	}
	return c.write(1, frame)
}

var errClosed = errors.New("the server closed the connection")

// readBuffer is each connection's read buffer. A message that fits in it,
// and nearly all do, is handed to the caller where it lies.
const readBuffer = 4096

// read returns the next text message, answering pings on the way. The bytes
// are good until the next call: nothing is allocated for a frame, because a
// generator reads a hundred thousand of them a second.
func (c *wsConn) read() ([]byte, error) {
	if c.skip > 0 {
		c.r.Discard(c.skip)
		c.skip = 0
	}
	if cap(c.big) > 64<<10 {
		c.big = nil // one long message is no reason to keep its room for good
	}
	c.big = c.big[:0]
	c.wire = 0
	packed := false
	for {
		b0, err := c.r.ReadByte()
		if err != nil {
			return nil, err
		}
		b1, err := c.r.ReadByte()
		if err != nil {
			return nil, err
		}
		fin, opcode := b0&0x80 != 0, b0&0x0f
		length := uint64(b1 & 0x7f)
		switch length {
		case 126:
			b, err := c.r.Peek(2)
			if err != nil {
				return nil, err
			}
			length = uint64(binary.BigEndian.Uint16(b))
			c.r.Discard(2)
		case 127:
			b, err := c.r.Peek(8)
			if err != nil {
				return nil, err
			}
			length = binary.BigEndian.Uint64(b)
			c.r.Discard(8)
		}
		if length > 8<<20 {
			return nil, errors.New("a frame larger than this client reads")
		}
		n := int(length)
		c.wire += 2 + n
		switch {
		case length >= 1<<16:
			c.wire += 8
		case length >= 126:
			c.wire += 2
		}
		if opcode == 1 || opcode == 2 {
			// The first frame of a message says whether it is compressed.
			packed = c.deflate && b0&0x40 != 0
		}
		if opcode >= 8 {
			// A control frame is at most 125 bytes.
			payload, err := c.r.Peek(n)
			if err != nil {
				return nil, err
			}
			switch opcode {
			case 8:
				// The close frame says why: a code and a few words.
				if len(payload) >= 2 {
					return nil, fmt.Errorf("%w: %d %s", errClosed, binary.BigEndian.Uint16(payload), payload[2:])
				}
				return nil, errClosed
			case 9:
				c.write(10, payload)
			}
			c.r.Discard(n)
			continue
		}
		if fin && len(c.big) == 0 && n <= readBuffer {
			// The whole message, in one frame that fits the buffer.
			payload, err := c.r.Peek(n)
			if err != nil {
				return nil, err
			}
			c.skip = n
			if packed {
				return c.inflate(payload)
			}
			return payload, nil
		}
		at := len(c.big)
		if cap(c.big) < at+n {
			grown := make([]byte, at, at+n)
			copy(grown, c.big)
			c.big = grown
		}
		c.big = c.big[:at+n]
		if _, err := io.ReadFull(c.r, c.big[at:]); err != nil {
			return nil, err
		}
		if fin {
			if packed {
				return c.inflate(c.big)
			}
			return c.big, nil
		}
	}
}

func (c *wsConn) close() { c.conn.Close() }
