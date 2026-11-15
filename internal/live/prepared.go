package live

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/tikaydaes/churr/internal/wirepb"
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
	// The HTTP server reads and writes through two buffers of four
	// kilobytes, and the library would keep them for as long as the
	// connection lives: most of what an idle connection held. What goes
	// through them here is small. The library writes only control frames
	// (a pong or a close, 127 bytes at most; the hub writes the data
	// frames itself), and a client's frames are a ping or a list of a
	// few ids; a longer one is read past the buffer, straight into where
	// it is going. So the library is given small ones, and the server's
	// are left to be collected. Nothing is waiting in the server's
	// writer: the answer to the upgrade was flushed before this.
	var from io.Reader = r.conn
	size := readBuffer
	// What the client sent straight behind its upgrade request has been
	// read already, and is handed on the way the library expects to
	// find it: buffered in the reader.
	early, _ := brw.Reader.Peek(brw.Reader.Buffered())
	if len(early) > 0 {
		early = bytes.Clone(early)
		from = io.MultiReader(bytes.NewReader(early), r.conn)
		size = max(size, len(early))
	}
	br := bufio.NewReaderSize(from, size)
	if len(early) > 0 {
		br.Peek(len(early))
	}
	return r.conn, bufio.NewReadWriter(br, bufio.NewWriterSize(r.conn, writeBuffer)), nil
}

// The sizes of the buffers the library reads and writes a connection
// through (Hijack). writeBuffer must hold a whole control frame, since the
// socket takes whole frames only (wireConn.Write).
const (
	readBuffer  = 512
	writeBuffer = 256
)

// The two kinds of message: JSON travels as text, protobuf as binary.
const (
	opText   = 0x1
	opBinary = 0x2
)

// appendHeader adds the header of a final frame from a server, text or
// binary, with a payload of n bytes, compressed or not.
func appendHeader(b []byte, opcode byte, n int, compressed bool) []byte {
	first := 0x80 | opcode // FIN
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

// compressedFrame builds the whole text frame for a payload sent compressed.
func compressedFrame(payload []byte) []byte {
	z := deflated(payload)
	return append(appendHeader(make([]byte, 0, len(z)+10), opText, len(z), true), z...)
}

// smallerFrame builds the whole text frame for a payload sent to a
// connection with compression: compressed, unless that does not make it
// smaller, in which case it goes as it is. One rule for every frame and
// both encodings (see compressFrom).
func smallerFrame(payload []byte) []byte {
	if len(payload) >= compressFrom {
		if z := deflated(payload); len(z) < len(payload) {
			return append(appendHeader(make([]byte, 0, len(z)+10), opText, len(z), true), z...)
		}
	}
	return append(appendHeader(make([]byte, 0, len(payload)+10), opText, len(payload), false), payload...)
}

// deflated is a payload compressed as one message of permessage-deflate.
func deflated(payload []byte) []byte {
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
	return z[:len(z)-4]
}

// Protocol Buffers (ruling R65, docs/WIRE-PROTOBUF.md).
//
// Everything in the hub makes and queues a frame as its JSON, once, and the
// same bytes are handed to every connection it goes to. A connection that
// negotiated protobuf is sent the frame as an Envelope in a binary message
// instead, and that too is made once for a frame and shared: the first
// writer to need it encodes and compresses, exactly as the compressed JSON
// frame is made, and it is kept beside it under the same slice.
//
// A protobuf connection always has compression on. As on a JSON connection
// with compression, a frame under compressFrom bytes is never compressed
// and any other is compressed once and sent in whichever form is smaller;
// the bit in each message's header that says "compressed" tells the reader
// which it got, at no cost.
// So a frame has up to four forms in use at once: JSON as it is (never
// kept: the queue's own bytes with a header), JSON compressed, and the
// protobuf one, which is whichever of compressed and not is smaller.

// protoFrame builds the whole binary frame for a frame given as JSON: its
// Envelope, compressed unless that would not make it smaller. raw is the
// Envelope's size.
func protoFrame(payload []byte) (wire []byte, raw int) {
	pb, _, _ := wirepb.EncodeFrameJSON(payload)
	// (Nothing in the hub queues what is not a JSON object; if something
	// did, pb is empty here, an envelope with no body, which a reader
	// skips as it skips a frame type it does not know.)
	if len(pb) >= compressFrom {
		if z := deflated(pb); len(z) < len(pb) {
			return append(appendHeader(make([]byte, 0, len(z)+10), opBinary, len(z), true), z...), len(pb)
		}
	}
	return append(appendHeader(make([]byte, 0, len(pb)+10), opBinary, len(pb), false), pb...), len(pb)
}

// finished reports whether a queue entry is a whole WebSocket frame already
// and not a frame's JSON: a connection's own frames (hello, pong, ack, its
// catch-up) are encoded for it when they are queued, since nobody shares
// them and keeping them would only fill the table below. JSON starts with
// a brace; a final frame's first byte has its top bit set, which no JSON
// text begins with.
func finished(f []byte) bool { return len(f) > 0 && f[0]&0x80 != 0 }

// own is a frame only this connection is sent, as it goes in its queue:
// the JSON for a JSON connection, the finished frame for a protobuf one.
func (c *conn) own(payload []byte) []byte {
	if !c.proto {
		return payload
	}
	wire, _ := protoFrame(payload)
	return wire
}

// The compressed and the protobuf frames in use. A fan-out hands every connection the same
// slice, so the slice itself is the key: its first byte's address (held as
// a pointer, so the bytes cannot be freed and their address used again
// while the entry lives) and its length. Whichever writer comes first
// compresses and the rest share the result. An entry nobody
// has asked for in a few seconds is dropped, and a connection that is that
// far behind compresses the payload again.
type preparedKey struct {
	first *byte
	n     int
	// proto is false for the compressed JSON frame and true for the
	// protobuf one.
	proto bool
}

// builtFrame is a frame as it goes to a socket, and its payload's size
// before compression.
type builtFrame struct {
	wire []byte
	raw  int
}

type preparedFrame struct {
	// wire is the frame, once someone has built it.
	wire atomic.Pointer[builtFrame]
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

// compressedFor returns the whole frame for a payload sent to a connection
// with compression (smallerFrame), building it if this is the first
// connection to need it.
func compressedFor(payload []byte) []byte { return preparedFrameFor(payload, false).wire }

// protoFor returns the whole protobuf frame for a frame's JSON, likewise.
func protoFor(payload []byte) *builtFrame { return preparedFrameFor(payload, true) }

func preparedFrameFor(payload []byte, proto bool) *builtFrame {
	now := int64(time.Since(prepared.started)/time.Second) + 1
	key := preparedKey{unsafe.SliceData(payload), len(payload), proto}
	v, ok := prepared.frames.Load(key)
	if !ok {
		// Marked as used before it can be seen, or the sweep below would
		// take it for one nobody has asked for since the clock began.
		fresh := &preparedFrame{}
		fresh.used.Store(now)
		v, _ = prepared.frames.LoadOrStore(key, fresh)
		sweepPrepared(now)
	}
	f := v.(*preparedFrame)
	if f.used.Load() != now {
		f.used.Store(now)
	}
	if w := f.wire.Load(); w != nil {
		return w
	}
	// Nobody waits for anybody here. The few writers that arrive in the
	// microseconds the first compression takes each do it too, and the
	// results are the same bytes. Were they to wait in line for the
	// first, thousands would queue behind it faster than a lock can let
	// them go one at a time, and that queue would be the server's pace.
	// That holds for the protobuf frame as well: it is encoded by the
	// first writers to need it and by nobody after them.
	w := &builtFrame{raw: len(payload)}
	if proto {
		w.wire, w.raw = protoFrame(payload)
	} else {
		w.wire = smallerFrame(payload)
	}
	f.wire.CompareAndSwap(nil, w)
	return f.wire.Load()
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
	if c.proto {
		return c.protoBytes(b)
	}
	if len(b.frames) == 1 && c.deflate && len(b.frames[0]) >= compressFrom {
		// The usual case when the writer is keeping up: the shared
		// frame goes to the socket as it is, uncopied.
		return compressedFor(b.frames[0])
	}
	for _, f := range b.frames {
		if c.deflate && len(f) >= compressFrom {
			b.wire = append(b.wire, compressedFor(f)...)
		} else {
			b.wire = append(appendHeader(b.wire, opText, len(f), false), f...)
		}
	}
	return b.wire
}

// protoBytes is frameBytes for a protobuf connection. It also sets b.raw to
// what the frames are before compression in the form this connection is
// sent, which is not the JSON they were queued as.
func (c *conn) protoBytes(b *batch) []byte {
	if len(b.frames) == 1 {
		// The usual case, as above: the shared frame, uncopied.
		f := b.frames[0]
		if finished(f) {
			b.raw = len(f)
			return f
		}
		w := protoFor(f)
		b.raw = w.raw
		return w.wire
	}
	b.raw = 0
	for _, f := range b.frames {
		if finished(f) {
			b.raw += len(f)
			b.wire = append(b.wire, f...)
			continue
		}
		w := protoFor(f)
		b.raw += w.raw
		b.wire = append(b.wire, w.wire...)
	}
	return b.wire
}
