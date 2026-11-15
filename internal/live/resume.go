package live

import (
	"container/list"
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// The resume queue (ruling R63).
//
// After a restart every client comes back within the same few seconds.
// Opening a connection costs little, but answering `resume` reads the
// database for every conversation the client holds and then tells everyone
// who can see the account that it is online. Thousands of those at once
// take every database connection and every processor, and people who are
// trying to post wait behind them.
//
// So a connection is accepted and told `hello` at once, and its `resume` is
// put in a queue. A bounded number are worked at a time, first come first
// served. A connection that is waiting is a full connection in every other
// way: its pings are answered, so no client gives up on it. Its catch-up is
// worked out when it reaches the front, not when it arrived, so it holds
// everything up to that moment: events of its conversations that happen
// while it waits are not kept for it in memory, because the log has them and
// the catch-up reads the log. From the moment the catch-up starts, live
// events are held and sent after it exactly as before (conn.replay).
//
// With a free place a resume starts the moment it arrives; nothing waits
// under normal load.
//
// Telling others that an account has gone offline shares the same places,
// behind the resumes. It reads who can see the account, which for a large
// channel is everyone, and when every connection drops at once (a proxy
// restarting, a network coming back) five thousand of those would take the
// database just as the resumes arrive. Waiting costs nothing here: by the
// time its turn comes the account has usually reconnected, is not offline,
// and nobody needs telling anything.
//
// The queue has a limit. Past it a connection is closed with 4029, which
// every client answers by coming back in about thirty seconds: by then the
// queue has moved on.

const (
	// resumeQueuePerPlace is how many resumes may wait for each one being
	// worked. With eight in flight that is 8,000 waiting.
	resumeQueuePerPlace = 1000
	// defaultPool is the database pool assumed for a store that does not
	// say how large its own is.
	defaultPool = 8
)

// readPool is what a store offers when it can say how many database
// connections plain reads may use at once.
type readPool interface {
	ReadPool() int
}

// resumeBound is how many resumes are worked at once on a machine with this
// many processors and a database pool of this size. Each one uses a database
// connection while it reads, so they are kept to half the pool, which leaves
// the other half to people posting and reading; and to twice the processors,
// because past that they only take turns.
func resumeBound(pool, cpus int) int {
	n := min(pool/2, 2*cpus)
	if n < 2 {
		n = 2
	}
	return n
}

// queuedResume is one `resume` waiting its turn.
type queuedResume struct {
	c         *conn
	ctx       context.Context
	positions map[string]int64
	// answer sends the `ack`, if the client asked for one.
	answer func()
	at     time.Time
}

type resumeQueue struct {
	mu      sync.Mutex
	waiting list.List // of *queuedResume, oldest first
	// left is the accounts whose last connection may have gone and whose
	// presence has not been looked at yet, oldest first, each once.
	left     []model.AccountID
	leftSet  map[model.AccountID]struct{}
	inFlight int
	bound    int
	limit    int

	// What the queue has done, for ResumeStats and the log.
	served, refused int64
	lastWait        time.Duration
	// The busy spell going on now, from the first resume that had to wait
	// until nothing is waiting: how many waited and the longest wait.
	spellWaited  int
	spellLongest time.Duration
}

// ResumeStats says how the resume queue stands, for tests and operators.
type ResumeStats struct {
	// InFlight is how many resumes are being worked now, at most Bound.
	// Waiting is how many are queued behind them, at most Limit.
	InFlight, Waiting int
	Bound, Limit      int
	// Served is how many resumes have been started since the server did,
	// and Refused how many were turned away with 4029 for a full queue.
	Served, Refused int64
	// LastWait is how long the resume started most recently had waited.
	LastWait time.Duration
}

// ResumeStats returns how the resume queue stands.
func (h *Hub) ResumeStats() ResumeStats {
	q := &h.resumes
	q.mu.Lock()
	defer q.mu.Unlock()
	return ResumeStats{InFlight: q.inFlight, Waiting: q.waiting.Len(), Bound: q.bound, Limit: q.limit,
		Served: q.served, Refused: q.refused, LastWait: q.lastWait}
}

func (h *Hub) initResumes() {
	q := &h.resumes
	q.bound = h.opts.resumeInFlight
	if q.bound <= 0 {
		pool := defaultPool
		if p, ok := h.store.(readPool); ok && p.ReadPool() > 0 {
			pool = p.ReadPool()
		}
		q.bound = resumeBound(pool, runtime.GOMAXPROCS(0))
	}
	q.limit = h.opts.resumeQueue
	if q.limit <= 0 {
		q.limit = q.bound * resumeQueuePerPlace
	}
}

// enqueueResume takes a connection's `resume`. It is started at once if
// there is a place, queued if not, and refused (false) if the queue is full.
// It never waits, so the connection's reader goes straight back to reading.
func (h *Hub) enqueueResume(ctx context.Context, c *conn, positions map[string]int64, answer func()) bool {
	q := &h.resumes
	r := &queuedResume{c: c, ctx: ctx, positions: positions, answer: answer, at: time.Now()}
	q.mu.Lock()
	if q.inFlight < q.bound {
		q.inFlight++
		q.served++
		q.lastWait = 0
		q.mu.Unlock()
		go h.workResumes(r, "")
		return true
	}
	if q.waiting.Len() >= q.limit {
		q.refused++
		q.mu.Unlock()
		return false
	}
	c.queuedAt = q.waiting.PushBack(r)
	q.spellWaited++
	q.mu.Unlock()
	return true
}

// leaveResumeQueue forgets a connection that closed while it was waiting,
// so the dead do not fill the queue.
func (h *Hub) leaveResumeQueue(c *conn) {
	q := &h.resumes
	q.mu.Lock()
	if c.queuedAt != nil {
		q.waiting.Remove(c.queuedAt)
		c.queuedAt = nil
	}
	q.mu.Unlock()
}

// accountLeft looks at an account's presence after one of its connections
// has gone, at once if there is a place and after the waiting resumes if
// not.
func (h *Hub) accountLeft(account model.AccountID) {
	q := &h.resumes
	q.mu.Lock()
	if q.inFlight < q.bound {
		q.inFlight++
		q.mu.Unlock()
		go h.workResumes(nil, account)
		return
	}
	if _, listed := q.leftSet[account]; !listed {
		if q.leftSet == nil {
			q.leftSet = map[model.AccountID]struct{}{}
		}
		q.leftSet[account] = struct{}{}
		q.left = append(q.left, account)
	}
	q.mu.Unlock()
}

// workResumes holds one of the places: it works r, or looks at the presence
// of the account that left, then takes whatever is at the front of the
// queue, until there is nothing waiting.
func (h *Hub) workResumes(r *queuedResume, left model.AccountID) {
	q := &h.resumes
	for {
		if r != nil {
			h.resume(r)
		} else {
			h.presenceCheck(left)
		}
		q.mu.Lock()
		front := q.waiting.Front()
		if front == nil && len(q.left) > 0 {
			r, left = nil, q.left[0]
			q.left = q.left[1:]
			delete(q.leftSet, left)
			if len(q.left) == 0 {
				q.left = nil
			}
			q.mu.Unlock()
			continue
		}
		if front == nil {
			q.inFlight--
			waited, longest := q.spellWaited, q.spellLongest
			// The spell is over when the last place is given up.
			over := waited > 0 && q.inFlight == 0
			if over {
				q.spellWaited, q.spellLongest = 0, 0
			}
			q.mu.Unlock()
			if over {
				h.opts.Log.Info("the resume queue has emptied", "waited", waited, "longest_wait", longest.Round(time.Millisecond).String(), "at_once", q.bound)
			}
			return
		}
		r = q.waiting.Remove(front).(*queuedResume)
		r.c.queuedAt = nil
		q.served++
		q.lastWait = time.Since(r.at)
		if q.lastWait > q.spellLongest {
			q.spellLongest = q.lastWait
		}
		q.mu.Unlock()
	}
}

// resume answers one `resume` whose turn has come.
func (h *Hub) resume(r *queuedResume) {
	c := r.c
	c.mu.Lock()
	gone := c.closed
	// From here on live events are held for the catch-up to sort out.
	c.catchingUp = true
	c.mu.Unlock()
	if gone {
		return
	}
	c.replay(r.ctx, r.positions)
	// The account is now online to others; its own clients hear of it
	// after `ready`.
	h.presenceCheck(c.account.ID)
	r.answer()
}

// spreadPerSecond is how many connections the server would like to see come
// back in each second after it restarts.
const spreadPerSecond = 250

// reconnectSpread is the `reconnect_spread_seconds` of `hello`: how many
// seconds clients should spread their next reconnect over, for a server with
// this many live connections. A small server is back in a couple of seconds;
// five thousand connections come back over twenty; nothing waits more than a
// minute for this.
func reconnectSpread(connections int) int {
	return min(max((connections+spreadPerSecond-1)/spreadPerSecond, 2), 60)
}
