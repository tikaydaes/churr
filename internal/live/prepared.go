package live

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// Compress once, send to many.
//
// A message in a channel of fifty thousand people is the same bytes for
// nearly all of them (publish.go: a few forms of one event). The frame a
// server sends is not masked, and with "no context takeover" its compression
// depends on nothing but the payload, so the WebSocket frame for a payload
// is the same bytes on every connection too. The WebSocket library cannot
// send a frame made beforehand: it compresses the payload again for every
// connection, which at fifty thousand recipients is most of the processor
// the server uses.
//
// So the hub writes its own data frames. The library still accepts the
// connection, reads everything, and writes the control frames (pong, close):
// what it writes is small and goes to the socket in one Write for each
// frame, and both it and the hub write through the wireConn below, whose
// lock keeps whole frames whole. The frames built here are byte for byte
// what the library would have written for the same payload (prepared_test.go
// holds the two against each other).

// wireConn is the socket of one connection, as the library and the hub's
// writer share it.
type wireConn struct {
	net.Conn
	sent *atomic.Int64 // every byte written, for Stats

	// try writes what the socket takes at once, where that can be done
	// (flush.go).
	try func([]byte) (int, error)

	mu sync.Mutex
	// closeSent is true once a close frame has gone out. Nothing may
	// follow one.
	closeSent bool
}

// errNotOneFrame is what the library is told if it ever hands the socket
// anything but one whole frame, which would let a frame of the hub's land
// in the middle of one of its own. It does not, for the frames it is left
// to write; this makes sure a change to it is noticed and not sent.
var errNotOneFrame = errors.New("live: the WebSocket library wrote part of a frame")

// Write is the library's way to the socket. The hub's is offer and finish
// (flush.go).
func (w *wireConn) Write(p []byte) (int, error) {
	if !oneFrame(p) {
		return 0, errNotOneFrame
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.Conn.Write(p)
	w.sent.Add(int64(n))
	if err == nil && p[0]&0x0f == 0x8 {
		w.closeSent = true
	}
	return n, err
}

// oneFrame reports whether p is exactly one whole, final, unmasked frame.
func oneFrame(p []byte) bool {
	if len(p) < 2 || p[0]&0x80 == 0 || p[1]&0x80 != 0 {
		return false
	}
	switch n := int(p[1] & 0x7f); n {
	case 126:
		return len(p) >= 4 && len(p) == 4+int(binary.BigEndian.Uint16(p[2:]))
	case 127:
		return len(p) >= 10 && uint64(len(p)) == 10+binary.BigEndian.Uint64(p[2:])
	default:
		return len(p) == 2+n
	}
}

// takenResponse hands the WebSocket library a wireConn as its socket, and
// keeps it for the hub.
type takenResponse struct {
	http.ResponseWriter
	sent *atomic.Int64
	conn *wireConn
}

// Hijack takes the socket over from the HTTP server, as the library asks.
func (r *takenResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	// Whatever wraps the server's own writer is looked through.
	nc, brw, err := http.NewResponseController(r.ResponseWriter).Hijack()
	if err != nil {
		return nil, nil, err
	}
	r.conn = &wireConn{Conn: nc, sent: r.sent, try: tryWriter(nc)}
	// The HTTP server's writer goes straight to its own socket; the
	// library must write through ours. Nothing is waiting in the writer:
	// the answer to the upgrade was flushed before this.
	brw.Writer.Reset(r.conn)
	return r.conn, brw, nil
}

// appendHeader adds the header of a final text frame from a server, with a
// payload of n bytes, compressed or not.
func appendHeader(b []byte, n int, compressed bool) []byte {
	first := byte(0x80 | 0x1) // FIN, text
	if compressed {
		first |= 0x40 // RSV1: this message is compressed (RFC 7692)
	}
	switch {
	case n <= 125:
		return append(b, first, byte(n))
	case n <= 0xffff:
		return append(b, first, 126, byte(n>>8), byte(n))
	default:
		b = append(b, first, 127)
		return binary.BigEndian.AppendUint64(b, uint64(n))
	}
}

// deflaters are the compressors, each over a megabyte, borrowed for the
// one compression a payload now gets.
var deflaters = sync.Pool{New: func() any {
	// The level the library uses, so the bytes are the ones it would send.
	w, _ := flate.NewWriter(nil, flate.BestSpeed)
	return w
}}

// compressedFrame builds the whole frame for a payload sent compressed.
func compressedFrame(payload []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(payload)/2 + 64)
	fw := deflaters.Get().(*flate.Writer)
	fw.Reset(&out)
	fw.Write(payload)
	fw.Flush()
	deflaters.Put(fw)
	// A flushed deflate stream ends 00 00 ff ff, which the frame leaves
	// out and the reader puts back (RFC 7692 section 7.2.1).
	z := out.Bytes()
	z = z[:len(z)-4]
	return append(appendHeader(make([]byte, 0, len(z)+10), len(z), true), z...)
}

// The compressed frames in use. A fan-out hands every connection the same
// slice, so the slice itself is the key: its first byte's address (held as
// a pointer, so the bytes cannot be freed and their address used again
// while the entry lives) and its length. Whichever writer comes first
// compresses and the rest share the result. An entry nobody
// has asked for in a few seconds is dropped, and a connection that is that
// far behind compresses the payload again.
type preparedKey struct {
	first *byte
	n     int
}

type preparedFrame struct {
	// wire is the frame, once someone has built it.
	wire atomic.Pointer[[]byte]
	// used is when a writer last asked, in seconds; written only when it
	// changes, so fifty thousand writers do not fight over the line.
	used atomic.Int64
}

var prepared struct {
	frames sync.Map // preparedKey → *preparedFrame
	swept  atomic.Int64
	// started is where the clock of `used` begins.
	started time.Time
}

func init() { prepared.started = time.Now() }

// preparedFor is how long a compressed frame is kept after its last use.
const preparedFor = 3 // seconds

// compressedFor returns the whole compressed frame for a payload, building
// it if this is the first connection to need it.
func compressedFor(payload []byte) []byte {
	now := int64(time.Since(prepared.started)/time.Second) + 1
	key := preparedKey{unsafe.SliceData(payload), len(payload)}
	v, ok := prepared.frames.Load(key)
	if !ok {
		v, _ = prepared.frames.LoadOrStore(key, &preparedFrame{})
		sweepPrepared(now)
	}
	f := v.(*preparedFrame)
	if f.used.Load() != now {
		f.used.Store(now)
	}
	if w := f.wire.Load(); w != nil {
		return *w
	}
	// Nobody waits for anybody here. The few writers that arrive in the
	// microseconds the first compression takes each do it too, and the
	// results are the same bytes. Were they to wait in line for the
	// first, thousands would queue behind it faster than a lock can let
	// them go one at a time, and that queue would be the server's pace.
	w := compressedFrame(payload)
	f.wire.CompareAndSwap(nil, &w)
	return *f.wire.Load()
}

// sweepPrepared drops the frames not used lately, at most once a second,
// on the way past of a writer that had to add one.
func sweepPrepared(now int64) {
	last := prepared.swept.Load()
	if now == last || !prepared.swept.CompareAndSwap(last, now) {
		return
	}
	prepared.frames.Range(func(k, v any) bool {
		if now-v.(*preparedFrame).used.Load() > preparedFor {
			prepared.frames.Delete(k)
		}
		return true
	})
}

// A batch is what is written to one connection in one go: the frames taken
// off its queue, and their bytes as they go to the socket. Borrowed
// for the write and given back, so a connection with nothing to send holds
// none.
type batch struct {
	frames [][]byte
	wire   []byte
	raw    int // the frames' size before compression
}

// A writer that is behind takes up to this much of its queue at once, and
// sends it in one write: one trip into the kernel, and usually one packet,
// for what was one each.
const (
	batchFrames = 32
	batchBytes  = 16 << 10
)

var batches = sync.Pool{New: func() any {
	return &batch{frames: make([][]byte, 0, batchFrames), wire: make([]byte, 0, batchBytes+1024)}
}}

func putBatch(b *batch) {
	clear(b.frames)
	b.frames = b.frames[:0]
	if cap(b.wire) > 4*batchBytes {
		// One large frame went through; the next borrower need not
		// keep its buffer.
		b.wire = make([]byte, 0, batchBytes+1024)
	}
	b.wire = b.wire[:0]
	batches.Put(b)
}

// takeLocked moves what the writer should send next from the connection's
// queues to b: every waiting answer to a ping first, then events in order.
// It returns their size before compression.
func (c *conn) takeLocked(b *batch, most int) (raw int) {
	for len(c.urgent) > 0 && len(b.frames) < most {
		b.frames = append(b.frames, c.urgent[0])
		raw += len(c.urgent[0])
		c.urgent = c.urgent[1:]
	}
	n := 0
	for n < len(c.queue) && len(b.frames) < most && (len(b.frames) == 0 || raw+len(c.queue[n]) <= batchBytes) {
		b.frames = append(b.frames, c.queue[n])
		raw += len(c.queue[n])
		c.queueBytes -= len(c.queue[n])
		n++
	}
	// The queue keeps its place in memory, so that queueing a frame does
	// not allocate; unless a burst made it large, which is given back.
	rest := copy(c.queue, c.queue[n:])
	clear(c.queue[rest:])
	c.queue = c.queue[:rest]
	if rest == 0 && cap(c.queue) > 64 {
		c.queue = nil
	}
	return raw
}

// frameBytes lays out b's frames as they go to this connection's socket.
func (c *conn) frameBytes(b *batch) []byte {
	if len(b.frames) == 1 && c.deflate && len(b.frames[0]) >= compressFrom {
		// The usual case when the writer is keeping up: the shared
		// frame goes to the socket as it is, uncopied.
		return compressedFor(b.frames[0])
	}
	for _, f := range b.frames {
		if c.deflate && len(f) >= compressFrom {
			b.wire = append(b.wire, compressedFor(f)...)
		} else {
			b.wire = append(appendHeader(b.wire, len(f), false), f...)
		}
	}
	return b.wire
}
