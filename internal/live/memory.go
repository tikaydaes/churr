package live

import (
	"net/http"
	"sort"
	"time"

	"github.com/coder/websocket"
)

// What the hub does when the server is short of memory (internal/memguard,
// which decides when; the server connects the two).
//
// Tight: a new connection is turned away before anything is kept for it.
// The upgrade is answered, because only a WebSocket close can carry a
// code, and the close is 4029, which every client answers by waiting about
// thirty seconds, spread at random, as it does for a full resume queue.
//
// Critical: connections are closed on purpose, the same way, the ones with
// the most queued first. Those are the connections that hold the most and
// get the least from being connected: they are behind already, and catch
// up from the log when they come back. After them, the ones nobody has
// used for longest.

// shortOfMemory is the reason on both closes.
const shortOfMemory = "the server is short of memory; try again shortly"

// refuse answers an upgrade and closes it at once with 4029.
func (h *Hub) refuse(w http.ResponseWriter, r *http.Request, subprotocol string) {
	h.wire.refused.Add(1)
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{subprotocol},
		// Nothing is sent but the close, so no compressor is set up.
		CompressionMode:    websocket.CompressionDisabled,
		InsecureSkipVerify: true, // as in Serve: the API layer checked
	})
	if err != nil {
		return
	}
	ws.Close(websocket.StatusCode(CloseTooManySends), shortOfMemory)
}

// backlog is what a connection holds that it has not been sent.
func (c *conn) backlog() (bytes int, idleSince time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	bytes = c.queueBytes
	for _, f := range c.urgent {
		bytes += len(f)
	}
	for _, e := range c.held {
		bytes += len(e.frame)
	}
	for _, held := range c.opening {
		for _, e := range held {
			bytes += len(e.frame)
		}
	}
	return bytes, c.lastActive
}

// Shed closes up to n connections with 4029, those with the most queued
// first and then those idle longest, and returns how many it closed.
func (h *Hub) Shed(n int) int {
	if n <= 0 {
		return 0
	}
	type candidate struct {
		c     *conn
		bytes int
		idle  time.Time
	}
	h.mu.RLock()
	all := make([]candidate, 0, h.open)
	for _, set := range h.conns {
		for c := range set {
			all = append(all, candidate{c: c})
		}
	}
	h.mu.RUnlock()
	for i := range all {
		all[i].bytes, all[i].idle = all[i].c.backlog()
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].bytes != all[j].bytes {
			return all[i].bytes > all[j].bytes
		}
		return all[i].idle.Before(all[j].idle)
	})
	closed := 0
	for _, cand := range all {
		if closed == n {
			break
		}
		c := cand.c
		c.mu.Lock()
		if !c.closed {
			c.closeLocked(CloseTooManySends, shortOfMemory)
			closed++
		}
		c.mu.Unlock()
	}
	h.wire.shed.Add(int64(closed))
	return closed
}

// Trim gives back what connections hold only to save allocating it again:
// the room in queues that are empty now.
func (h *Hub) Trim() {
	h.mu.RLock()
	all := make([]*conn, 0, h.open)
	for _, set := range h.conns {
		for c := range set {
			all = append(all, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range all {
		c.mu.Lock()
		if len(c.queue) == 0 {
			c.queue = nil
		}
		if len(c.urgent) == 0 {
			c.urgent = nil
		}
		if len(c.held) == 0 {
			c.held = nil
		}
		if len(c.pending) == 0 {
			c.pending = nil
		}
		c.mu.Unlock()
	}
}
