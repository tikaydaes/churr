package live

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// What a server of fifty thousand connections runs out of first is not
// processors but bytes on the wire: every delivery is its own frame on its
// own socket. This file is the two things the hub does about that besides
// sending less: it compresses, and it counts what it sent.
//
// Compression is the WebSocket's own (permessage-deflate, RFC 7692), asked
// for by the client in the upgrade as browsers and dart:io do unasked, so a
// client that does not ask is served exactly as before. Each message is
// compressed on its own ("no context takeover"): a window kept per
// connection would compress better, since a conversation's events repeat
// its ids, but costs a compressor of over a megabyte for every connection,
// which fifty thousand connections cannot pay. Compressed on its own, a
// message borrows a compressor for the few microseconds it takes and holds
// none while its bytes wait for the socket.

// compressFrom is the size under which a frame is never compressed, in
// either encoding: so short a frame does not come out shorter. Any other
// frame is compressed once and compared, and the smaller form is sent
// (prepared.go). There is nothing more to the rule: no memory of what a
// type of frame came to last time, and no other threshold.
const compressFrom = 64

// wireCounters is what the hub has written to its sockets since it started.
type wireCounters struct {
	bytes  atomic.Int64 // as written to the sockets, so after compression
	frames atomic.Int64 // frames taken off send queues and written
	raw    atomic.Int64 // what those frames were before compression
	slow   atomic.Int64 // connections closed as slow consumers (4009)
	silent atomic.Int64 // connections closed for no heartbeat (4008)
	// Turned away, and closed, for want of memory (memory.go).
	refused, shed atomic.Int64

	// kinds is what was queued for sending, by what it was: the event's
	// type, or "replay" for catch-up and "control" for the frames of the
	// connection itself. A *kindCount for each.
	kinds sync.Map

	// The rate is worked out when someone asks, from what was sent since
	// they last asked, so counting costs one addition a write and nothing
	// runs when nobody looks.
	mu      sync.Mutex
	at      time.Time
	atBytes int64
	rate    float64
}

// kindCount is the frames of one kind queued for sending and their bytes
// before compression.
type kindCount struct{ frames, bytes atomic.Int64 }

// Kinds of frame that are not an event's type.
const (
	// kindReplay is events sent again to a client catching up after
	// `resume`, whatever their type.
	kindReplay = "replay"
	// kindControl is hello, ready, reload, pong, ack and error.
	kindControl = "control"
	// kindOther is an account event whose type could not be seen.
	kindOther = "other"
)

// count adds frames of one kind to what was queued for sending. Fan-outs
// call it once for all their recipients, so it costs a lookup an event and
// not one a delivery.
func (h *Hub) count(kind string, frames, bytes int) {
	if frames == 0 {
		return
	}
	k, ok := h.wire.kinds.Load(kind)
	if !ok {
		k, _ = h.wire.kinds.LoadOrStore(kind, &kindCount{})
	}
	k.(*kindCount).frames.Add(int64(frames))
	k.(*kindCount).bytes.Add(int64(bytes))
}

// kindOf is the type of an account event as its sender built it.
func kindOf(v any) string {
	switch e := v.(type) {
	case model.Event:
		return e.Type
	case *model.Event:
		return e.Type
	case map[string]any:
		if t, ok := e["type"].(string); ok {
			return t
		}
	}
	return kindOther
}

// KindStats is the frames of one kind queued for sending, and their bytes
// before compression.
type KindStats struct{ Frames, Bytes int64 }

// Stats is what the hub has sent over its live connections since it
// started. The counts only ever grow.
type Stats struct {
	// Connections is how many are open now.
	Connections int
	// BytesOut is every byte written to the sockets of live connections:
	// WebSocket frames as sent, compressed where the client asked for
	// that. It leaves out what TCP and TLS add.
	BytesOut int64
	// RawBytesOut is what the frames written held before compression;
	// against BytesOut it says what compression saves.
	RawBytesOut int64
	// FramesOut is how many frames were written.
	FramesOut int64
	// BytesOutPerSecond is the rate of BytesOut since Stats was last
	// called, if that was at least a second ago, or else the rate it
	// reported then.
	BytesOutPerSecond float64
	// ClosedSlow and ClosedSilent count the connections the hub closed
	// because the client did not take what it was sent (4009) and because
	// nothing was heard from it (4008).
	ClosedSlow, ClosedSilent int64
	// RefusedForMemory and ShedForMemory count the connections turned
	// away, and the ones closed, because the server was short of memory
	// (4029).
	RefusedForMemory, ShedForMemory int64
	// Queued says where the bytes went: for each event type, and for
	// "replay" (catch-up after `resume`) and "control" (the connection's
	// own frames), how many frames were queued for connections and how
	// many bytes they held before compression. It counts at the queue,
	// so it runs slightly ahead of RawBytesOut, by what is still waiting
	// and what was dropped with a connection that closed.
	Queued map[string]KindStats
}

// Stats returns the counts.
func (h *Hub) Stats() Stats {
	w := &h.wire
	s := Stats{Connections: h.Connected(), BytesOut: w.bytes.Load(), RawBytesOut: w.raw.Load(), FramesOut: w.frames.Load(),
		ClosedSlow: w.slow.Load(), ClosedSilent: w.silent.Load(),
		RefusedForMemory: w.refused.Load(), ShedForMemory: w.shed.Load()}
	s.Queued = map[string]KindStats{}
	w.kinds.Range(func(k, v any) bool {
		s.Queued[k.(string)] = KindStats{v.(*kindCount).frames.Load(), v.(*kindCount).bytes.Load()}
		return true
	})
	now := time.Now()
	w.mu.Lock()
	if w.at.IsZero() {
		w.at = h.started
	}
	if d := now.Sub(w.at); d >= time.Second {
		w.rate = float64(s.BytesOut-w.atBytes) / d.Seconds()
		w.at, w.atBytes = now, s.BytesOut
	}
	s.BytesOutPerSecond = w.rate
	w.mu.Unlock()
	return s
}

// statsEvery is how often the hub writes what it is sending to the log.
const statsEvery = time.Minute

// busyBytesPerSecond is the rate from which that line is worth an
// operator's eye without being asked for: a megabyte a second is past what
// a server of a few hundred people sends.
const busyBytesPerSecond = 1 << 20

// logStats writes the traffic of the last minute to the log, until the hub
// shuts down. It is started with the first connection.
func (h *Hub) logStats() {
	t := time.NewTicker(statsEvery)
	defer t.Stop()
	last := Stats{Queued: map[string]KindStats{}}
	lastOut, lastAgain, _ := tcpSegments()
	lastDropped, _, _ := sendQueues()
	for {
		select {
		case <-h.done:
			return
		case <-t.C:
		}
		s := h.Stats()
		secs := statsEvery.Seconds()
		rate := float64(s.BytesOut-last.BytesOut) / secs
		log := h.opts.Log.Debug
		if rate >= busyBytesPerSecond {
			log = h.opts.Log.Info
		}
		// Where the bytes went, largest first: at fifty thousand
		// connections one kind of frame is usually most of them.
		type kind struct {
			name  string
			bytes int64
		}
		var kinds []kind
		for name, k := range s.Queued {
			if d := k.Bytes - last.Queued[name].Bytes; d > 0 {
				kinds = append(kinds, kind{name, d})
			}
		}
		sort.Slice(kinds, func(i, j int) bool { return kinds[i].bytes > kinds[j].bytes })
		by := make([]any, 0, 2*len(kinds))
		for _, k := range kinds {
			by = append(by, k.name, int64(float64(k.bytes)/secs))
		}
		log("live traffic", "connections", s.Connections,
			"bytes_out_per_second", int64(rate),
			"frames_out_per_second", int64(float64(s.FramesOut-last.FramesOut)/secs),
			"uncompressed_bytes_per_second", int64(float64(s.RawBytesOut-last.RawBytesOut)/secs),
			"closed_slow", s.ClosedSlow-last.ClosedSlow, "closed_silent", s.ClosedSilent-last.ClosedSilent,
			slog.Group("queued_bytes_per_second", by...))
		last = s
		// What the kernel had to send twice, and whether its own
		// transmit queues are why. Packets dropped there are this
		// machine sending more than its network card carries, which
		// the pace of writes is for (pace.go). Segments sent again
		// with nothing dropped here were lost on the path or by the
		// receivers; the server does not slow down for those, and
		// says so, since deliveries are late all the same.
		out, again, ok := tcpSegments()
		dropped, _, queues := sendQueues()
		if ok {
			sent, resent := out-lastOut, again-lastAgain
			if sent >= retransmitFloor && resent*100 >= sent {
				where := "packets are being lost on the path or by the receivers, not in this machine's transmit queues; nothing is slowed for it"
				if !queues {
					where = "packets are being lost somewhere between here and the clients"
				} else if d := dropped - lastDropped; d*1000 >= sent {
					where = "this machine's transmit queues are dropping packets (`tc -s qdisc`): it is sending more than its network card carries"
				}
				h.opts.Log.Warn("TCP segments are being sent again, so some deliveries arrive late: "+where,
					"segments_per_second", int64(float64(sent)/secs), "sent_again_percent", float64(resent*1000/sent)/10,
					"dropped_by_this_machines_queues", dropped-lastDropped, "connections", s.Connections)
			}
		}
		lastOut, lastAgain, lastDropped = out, again, dropped
	}
}

// retransmitFloor is how many segments a minute the machine must send
// before the share sent again is worth a warning.
const retransmitFloor = 60000

// countedResponse hands the WebSocket library a socket that counts what is
// written to it, which is the only place the bytes that really leave, after
// compression and framing, can be seen.
type countedResponse struct {
	http.ResponseWriter
	sent *atomic.Int64
}

// Hijack takes the socket over from the HTTP server, as the library asks.
func (r countedResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	// Whatever wraps the server's own writer is looked through.
	nc, brw, err := http.NewResponseController(r.ResponseWriter).Hijack()
	if err != nil {
		return nil, nil, err
	}
	cc := countedConn{nc, r.sent}
	// The HTTP server's writer goes straight to its own socket; the
	// library must write through the counting one. Nothing is waiting in
	// the writer: the answer to the upgrade was flushed before this.
	brw.Writer.Reset(cc)
	return cc, brw, nil
}

type countedConn struct {
	net.Conn
	sent *atomic.Int64
}

func (c countedConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.sent.Add(int64(n))
	return n, err
}
