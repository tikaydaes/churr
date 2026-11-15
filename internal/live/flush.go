package live

import (
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Who writes a connection's queue to its socket.
//
// A goroutine for each connection, woken for each frame, does not scale: a
// message to fifty thousand people is fifty thousand goroutines woken by
// the one that fans it out, each to make one write and sleep again, and
// every one of those handovers goes through the scheduler's own lock. The
// server then runs at the pace of that lock with half its processors idle.
//
// So a few flushers, one for each processor, write for everybody. A
// connection with something to send is put on a flusher's list (once,
// however much is queued), and the flusher goes down its list writing each
// connection's frames. A flusher that is busy is not woken; under load none
// of them sleeps, and nothing is handed over at all.
//
// A flusher must never wait for one client, so it only offers the socket
// what it will take at once (trywrite_unix.go). When a socket will not take
// a whole write, because the client is slow or the library is writing a
// control frame, the connection gets a goroutine of its own to finish that
// write and empty the queue the old way, waiting as long as a slow client
// is allowed; after that it is the flushers' again. Connections whose
// sockets cannot be offered a write (TLS ended in this process, tests) are
// always written that way.

// flusher is one of the goroutines that write, and the connections waiting
// for it.
type flusher struct {
	mu    sync.Mutex
	ready []*conn
	idle  bool
	wake  chan struct{}
	_     [64]byte // each on its own cache line
}

var flushers struct {
	once sync.Once
	all  []*flusher
	next atomic.Uint32
}

// handovers counts the writes a flusher could not finish and left to a
// goroutine of the connection's own. The tests read it.
var handovers atomic.Int64

// flusherFor picks the flusher of a new connection, in turn.
func flusherFor() *flusher {
	flushers.once.Do(func() {
		flushers.all = make([]*flusher, runtime.GOMAXPROCS(0))
		for i := range flushers.all {
			f := &flusher{idle: true, wake: make(chan struct{}, 1)}
			flushers.all[i] = f
			go f.run()
		}
	})
	return flushers.all[int(flushers.next.Add(1))%len(flushers.all)]
}

// push adds a connection to the list, waking the flusher if it sleeps.
func (f *flusher) push(c *conn) {
	f.mu.Lock()
	f.ready = append(f.ready, c)
	wake := f.idle
	f.idle = false
	f.mu.Unlock()
	if wake {
		f.wake <- struct{}{}
	}
}

func (f *flusher) run() {
	var mine []*conn
	for {
		f.mu.Lock()
		if len(f.ready) == 0 {
			f.idle = true
			f.mu.Unlock()
			<-f.wake
			continue
		}
		mine, f.ready = f.ready, mine[:0]
		f.mu.Unlock()
		for i, c := range mine {
			mine[i] = nil
			c.flush()
		}
	}
}

// scheduleLocked sees that what is queued gets written: the connection is
// handed to whoever writes it, unless someone already has it.
func (c *conn) scheduleLocked() {
	if c.scheduled || !c.started {
		return
	}
	c.scheduled = true
	if c.offered() {
		c.flusher.push(c)
	} else {
		go c.drain(nil, nil, false)
	}
}

// offered reports whether the flushers write this connection.
func (c *conn) offered() bool {
	return c.nc.try != nil && c.hub.opts.writeDelay == 0
}

// start lets the queue be written. Until Serve calls it nothing is, so a
// connection the hub turns away is sent its close and no hello before it.
func (c *conn) start() {
	c.mu.Lock()
	c.started = true
	if len(c.queue) > 0 || len(c.urgent) > 0 {
		c.scheduleLocked()
	}
	c.mu.Unlock()
}

// flush is a flusher's visit: it writes one batch of the connection's
// frames if the socket takes them at once. What is still queued after it
// waits for the next visit, so one busy connection does not hold up the
// rest of the list.
func (c *conn) flush() {
	c.mu.Lock()
	if (len(c.queue) == 0 && len(c.urgent) == 0) || c.closed {
		c.scheduled, c.writing = false, false
		c.mu.Unlock()
		return
	}
	b := batches.Get().(*batch)
	b.raw = c.takeLocked(b, batchFrames)
	c.writing = true
	c.mu.Unlock()

	p := c.frameBytes(b)
	n, held, err := c.nc.offer(p)
	if err == nil && n < len(p) {
		// The socket is full, or busy: this connection is waited for
		// by a goroutine of its own, not by a flusher.
		handovers.Add(1)
		go c.drain(b, p[n:], held)
		return
	}
	if !c.wrote(b, err) {
		return
	}
	c.mu.Lock()
	c.writing = false
	more := (len(c.queue) > 0 || len(c.urgent) > 0) && !c.closed
	if !more {
		c.scheduled = false
	}
	c.mu.Unlock()
	if more {
		c.flusher.push(c)
	}
}

// wrote counts a batch that went out, or closes the connection if it did
// not, and reports which.
func (c *conn) wrote(b *batch, err error) bool {
	frames, raw := len(b.frames), b.raw
	putBatch(b)
	if err != nil {
		// The connection stays as it is, with a writer: nothing more
		// is to be written to it.
		c.close(CloseGoingAway, "write failed")
		return false
	}
	c.hub.wire.frames.Add(int64(frames))
	c.hub.wire.raw.Add(int64(raw))
	return true
}

// drain writes the connection's queue, waiting for the client as long as
// one is allowed, until nothing is left. It starts with the rest of a batch
// a flusher could not finish, if there is one; held says the socket's lock
// came with it, because part of a frame is already out.
func (c *conn) drain(b *batch, rest []byte, held bool) {
	// A client that is there and is not reading takes nothing for the
	// whole time allowed. That is a slow consumer, and it is closed as
	// one and told so. The close is asked for here and not left to the
	// write failing, so that it is counted and told as what it is.
	slow := time.AfterFunc(c.hub.opts.writeTimeout, func() {
		c.close(CloseQueueFull, "the client did not take a frame in time")
	})
	defer slow.Stop()
	if b != nil {
		if !c.wrote(b, c.nc.finish(rest, held)) {
			return
		}
	}
	for {
		c.mu.Lock()
		c.writing = false
		if (len(c.queue) == 0 && len(c.urgent) == 0) || c.closed {
			c.scheduled = false
			c.mu.Unlock()
			return
		}
		// Answers to pings go before the events waiting, and a writer
		// that is behind takes several frames at once (prepared.go).
		most := batchFrames
		if c.hub.opts.writeDelay > 0 {
			most = 1
		}
		b := batches.Get().(*batch)
		b.raw = c.takeLocked(b, most)
		c.writing = true
		c.mu.Unlock()
		if d := c.hub.opts.writeDelay; d > 0 {
			time.Sleep(d)
		}
		slow.Reset(c.hub.opts.writeTimeout)
		if !c.wrote(b, c.nc.finish(c.frameBytes(b), false)) {
			return
		}
	}
}

// offer writes as much of p, which is whole frames, as the socket takes at
// once, and never waits. If it took only part, held says the socket's lock
// is still held, for finish to give back: nobody else may write until the
// frame that was cut is whole.
func (w *wireConn) offer(p []byte) (n int, held bool, err error) {
	if !w.mu.TryLock() {
		// The library is writing a control frame, perhaps to a client
		// that is not reading.
		return 0, false, nil
	}
	if w.closeSent {
		w.mu.Unlock()
		return 0, false, net.ErrClosed
	}
	n, err = w.try(p)
	w.sent.Add(int64(n))
	if err != nil || n == len(p) {
		w.mu.Unlock()
		return n, false, err
	}
	return n, true, nil
}

// finish writes p, waiting for the socket as long as it takes.
func (w *wireConn) finish(p []byte, held bool) error {
	if !held {
		w.mu.Lock()
		if w.closeSent {
			w.mu.Unlock()
			return net.ErrClosed
		}
	}
	defer w.mu.Unlock()
	n, err := w.Conn.Write(p)
	w.sent.Add(int64(n))
	return err
}
