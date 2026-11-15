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
// is allowed; after that it is the flushers' again. A connection whose TLS
// ends in this process is offered writes through it (offer.go). Connections
// whose sockets cannot be offered a write at all (a pipe in a test, TLS over
// a listener that is not Offerable) are always written by a goroutine of
// their own.

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
		go runPace()
	})
	return flushers.all[int(flushers.next.Add(1))%len(flushers.all)]
}

// gathering counts the fan-outs that are, at this moment, handing one event
// to a run of connections (publish.go). While there is one, a flusher that
// sleeps is not woken for each connection put on its list: it is woken once
// when the run ends, and finds them all. Woken for each, a flusher that
// keeps up wakes, writes one frame and sleeps again fifty thousand times a
// message, and every one of those wake-ups goes through the scheduler's
// lock, which the goroutines handing the message out then queue for.
var gathering atomic.Int32

// push adds a connection to the list, waking the flusher if it sleeps,
// unless a fan-out is about to wake it for many at once.
func (f *flusher) push(c *conn) {
	f.mu.Lock()
	f.ready = append(f.ready, c)
	// Read with the list's lock held: whoever is gathering wakes the
	// flushers after it has stopped, and takes this lock to do it, so it
	// sees this connection or this sees that it has stopped.
	wake := f.idle && gathering.Load() == 0
	if wake {
		f.idle = false
	}
	f.mu.Unlock()
	if wake {
		f.wake <- struct{}{}
	}
}

// startGather starts a run of sends that ends with flushGathered.
func startGather() { gathering.Add(1) }

// flushGathered ends a run: every flusher that sleeps with connections on
// its list is woken.
func flushGathered() {
	gathering.Add(-1)
	for _, f := range flushers.all {
		f.mu.Lock()
		wake := f.idle && len(f.ready) > 0
		if wake {
			f.idle = false
		}
		f.mu.Unlock()
		if wake {
			f.wake <- struct{}{}
		}
	}
}

func (f *flusher) run() {
	var mine []*conn
	var sent tally
	allowed := 0 // writes this flusher may still make (pacer)
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
			if allowed == 0 {
				allowed = writePace.take(paceChunk)
			}
			allowed--
			c.flush(&sent)
			if i%yieldEvery == yieldEvery-1 {
				// A long list is most of a processor for tens of
				// milliseconds. On a server with one processor a
				// request that has come back from the database
				// waits for all of it unless the flusher steps
				// aside; with nobody waiting this costs nothing.
				runtime.Gosched()
			}
		}
		sent.settle()
	}
}

// yieldEvery is how many connections a flusher writes before it lets
// whatever else is ready to run have the processor: about half a
// millisecond of writing.
const yieldEvery = 64

// tally is what one flusher wrote in one pass over its list. The hub's
// counts are one line of memory that every flusher would otherwise write
// three times for each connection, and sixteen processors writing one line
// spend their time passing it around: so a flusher counts on its own and
// adds the sums up when the pass is done.
type tally struct {
	hub                *Hub
	bytes, frames, raw int64
	writes             int64
}

func (t *tally) add(h *Hub, bytes, frames, raw int) {
	if t.hub != h {
		t.settle()
		t.hub = h
	}
	t.writes++
	t.bytes += int64(bytes)
	t.frames += int64(frames)
	t.raw += int64(raw)
}

func (t *tally) settle() {
	if t.writes != 0 {
		pacing.writes.Add(t.writes) // what the pace is found from (pace.go)
		t.writes = 0
	}
	if t.hub != nil && (t.bytes != 0 || t.frames != 0 || t.raw != 0) {
		t.hub.wire.bytes.Add(t.bytes)
		t.hub.wire.frames.Add(t.frames)
		t.hub.wire.raw.Add(t.raw)
	}
	t.hub, t.bytes, t.frames, t.raw = nil, 0, 0, 0
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
func (c *conn) flush(sent *tally) {
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
		sent.add(c.hub, n, 0, 0)
		handovers.Add(1)
		go c.drain(b, p[n:], held)
		return
	}
	if err != nil {
		sent.add(c.hub, n, 0, 0)
		c.wrote(b, err)
		return
	}
	if c.nc.behind != nil && c.nc.behind() {
		// All of it is with the TLS layer, and sealed, but the socket
		// did not take all of that (offer.go). What is left is waited
		// for by a goroutine of the connection's own, as above.
		sent.add(c.hub, n, 0, 0)
		handovers.Add(1)
		go c.drain(b, nil, false)
		return
	}
	sent.add(c.hub, n, len(b.frames), b.raw)
	putBatch(b)
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
// once, and never waits. The caller counts the bytes (tally). If it took only part, held says the socket's lock
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
	if len(p) == 0 {
		// Nothing of the hub's is left to write, only what the socket
		// under TLS is behind by (offer.go).
		if w.settle != nil {
			return w.settle()
		}
		return nil
	}
	n, err := w.Conn.Write(p)
	w.sent.Add(int64(n))
	return err
}

// The pace of writes.
//
// A message to fifty thousand connections is fifty thousand small packets,
// and sixteen flushers hand them to the kernel in a few milliseconds. The
// network interface sends them one after another from a queue that, as
// Linux sets it up, holds about ten thousand: the rest are dropped on the
// spot, and TCP sends each again only when its timer runs out, 200 ms
// later, then 400, and so on. Measured at fifty thousand connections that
// was one packet in fifteen sent again, half the deliveries 0.7 s late and
// one in twenty 15 s late, on a machine with three quarters of its
// processors idle. Nothing in the program is slow; it is too fast for the
// queue below it.
//
// So the flushers together write no faster than a pace, after a burst the
// queue can hold. What then waits, waits in the connections' own queues,
// in order, and a connection that has waited is written everything it has
// in one go (prepared.go), so the frames still leave as fast as they come
// while the packets do not: held to 200,000 writes a second, the same
// machine sent 172,000 packets a second for 284,000 deliveries, with half
// of them there in 166 ms and 99 in 100 in 360 ms, where it had been 0.7 s
// and 28 s. There is no pace until the machine shows it needs one, and
// what it is then is found by watching (pace.go).

const (
	// paceBurst is how many writes may go at once: well inside a transmit
	// queue of 10,240 packets, which is what Linux gives an interface.
	paceBurst = 4096
	// paceChunk is how many writes a flusher is allowed at a time, so
	// that the pace is asked once for many writes and not for each.
	paceChunk = 64
)

// pacer hands out writes at a rate.
type pacer struct {
	rate atomic.Int64 // writes a second; 0 or less is no pace at all

	mu     sync.Mutex
	burst  float64 // the most writes that may go at once
	tokens float64
	at     time.Time
}

func newPacer(perSecond int64, burst float64) *pacer {
	p := &pacer{burst: burst, tokens: burst, at: time.Now()}
	p.rate.Store(perSecond)
	return p
}

var writePace = newPacer(0, paceBurst)

// take returns how many writes the caller may make now, at most n and at
// least one, waiting if the pace has been reached.
func (p *pacer) take(n int) int {
	for {
		rate := float64(p.rate.Load())
		if rate <= 0 {
			return n
		}
		p.mu.Lock()
		now := time.Now()
		p.tokens = min(p.burst, p.tokens+now.Sub(p.at).Seconds()*rate)
		p.at = now
		if p.tokens >= 1 {
			got := min(n, int(p.tokens))
			p.tokens -= float64(got)
			p.mu.Unlock()
			return got
		}
		// Long enough for a chunk to have come due, so the flushers
		// do not wake for one write each.
		wait := time.Duration((float64(n) - p.tokens) / rate * float64(time.Second))
		p.mu.Unlock()
		time.Sleep(wait)
	}
}
