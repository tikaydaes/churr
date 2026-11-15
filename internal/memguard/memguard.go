// Package memguard keeps the server inside the memory it has.
//
// A small machine has no swap, and a process that takes more memory than
// there is gets killed by the kernel: no warning in its log, no answer to
// any client, every connection gone at once. Measured on 1 vCPU and 1 GB,
// that happened at about 9,500 live connections. The guard is there so the
// server stops growing before that point, says so, and at the worst gives
// up a few connections on purpose instead of all of them by accident.
//
// Once a second it reads how much is left (a Source: the cgroup's limit, or
// the machine's own memory) and how much of what the Go runtime holds is
// garbage it could give back (a Runtime). Their sum is what would be left
// after a full collection, and two thresholds on it give three states:
//
//   - normal: connections are admitted, as many each second as there is
//     room for above the tight threshold;
//   - tight, under a fifth of the limit left: no new live connections, and
//     what can be given back is given back;
//   - critical, under a sixteenth left: connections are also closed, the
//     ones with the most queued first, until it is not.
//
// Why those numbers. The sixteenth (60 MB of 1 GB) is what is always kept
// free for the kernel, the database's pages in the page cache and whatever
// else runs on the machine; the Go runtime is given a memory limit
// (debug.SetMemoryLimit) that makes it collect harder and harder to stay
// that far from the edge, so the kernel never has to act. The fifth (190 MB
// of 1 GB) leaves the collector the difference to work in: at the moment
// admission stops, about an eighth of the limit is still garbage-to-be, so
// the server is slower (it collects more often) but not spinning. Under a
// sixteenth there is nothing to collect: the live data alone is at the
// runtime's limit, and only having less of it helps.
package memguard

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// State is how short of memory the server is.
type State int32

// The three states, in order of trouble.
const (
	Normal State = iota
	Tight
	Critical
)

func (s State) String() string {
	switch s {
	case Tight:
		return "tight"
	case Critical:
		return "critical"
	}
	return "normal"
}

// Reading is how much memory there is, from outside the process.
type Reading struct {
	// Total is the size of whatever limits the process: the cgroup's
	// limit, the machine's memory, or the operator's figure.
	Total int64
	// Headroom is how much more could be taken before the kernel acts.
	Headroom int64
	// From names what the figures were read from, for the log.
	From string
	// Hard is true when the limit is one the kernel enforces on this
	// process alone (a cgroup's), where going over is fatal at once and
	// nothing else on the machine gives way first.
	Hard bool
}

// Source reads the memory there is. ok is false when nothing can be read
// (not Linux, or /proc is not there), and the guard then does nothing.
type Source interface {
	Read() (r Reading, ok bool)
}

// Heap is the Go runtime's own account.
type Heap struct {
	// Held is what the runtime holds from the operating system now.
	Held int64
	// Floor is what it would hold just after a full collection: the data
	// in use, with everything allocated since the last collection counted
	// as in use, and what is not heap at all (stacks, the runtime's own).
	Floor int64
}

// Runtime is what the guard needs of the Go runtime. Tests give a fake one.
type Runtime interface {
	Heap() Heap
	// Collect runs a full collection and gives freed memory back to the
	// operating system. It takes as long as that takes.
	Collect()
	// SetLimit sets the runtime's memory limit.
	SetLimit(bytes int64)
}

const (
	// tightShare and criticalShare are the thresholds, as shares of the
	// limit (the package comment says why).
	tightShare    = 5
	criticalShare = 16
	// PerConnection is what one more live connection is taken to cost,
	// kernel buffers included, when deciding how many to admit before the
	// next look and how many to close. It is deliberately high.
	PerConnection = 128 << 10
	// calmChecks is how many looks in a row must say a state is over
	// before it is left, and leaveBy how far past the threshold (a
	// quarter) they must be, so the state does not flicker.
	calmChecks = 5
	// collectEvery is the least time between collections the guard asks
	// for to make sure of a threshold, and tightCollectEvery how often it
	// collects while tight.
	collectEvery      = 5 * time.Second
	tightCollectEvery = 30 * time.Second
	// reclaimEvery is the least time between collections made because a
	// connection found no room or a crowd left. A collection takes a
	// processor for as long as it takes to walk everything in use
	// (tenths of a second for a few hundred megabytes), so during a
	// storm this is the share of one processor the guard may spend.
	reclaimEvery = 2 * time.Second
	// crowdLeft is how many connections must have gone, and be a fifth
	// of those there were, for the guard to collect on that alone.
	crowdLeft = 500
	// admitPatience is how long a connection is kept waiting for room
	// before it is refused (AdmitWait), maxWaiting how many may wait at
	// once, and pokeEvery the least time between the looks they ask for.
	admitPatience = 5 * time.Second
	maxWaiting    = 2000
	pokeEvery     = 250 * time.Millisecond
)

// Options configure a Guard.
type Options struct {
	Log *slog.Logger
	// Source is where memory is read from; nil is no guard at all.
	Source Source
	// Runtime is the Go runtime; nil is the real one.
	Runtime Runtime
	// KeepLimit leaves the runtime's memory limit alone, for an operator
	// who set GOMEMLIMIT.
	KeepLimit bool
	// Trim is called when memory becomes tight, and again each time the
	// guard collects while it stays so: give back what can be given back.
	Trim func()
	// Shed is called each second while memory is critical with how many
	// bytes must be found; it closes connections and returns how many.
	Shed func(need int64) (closed int)
	// Connections reports how many live connections there are, if set.
	// When a crowd of them has gone the guard collects without waiting
	// to be short: what they held is then known to be free when they
	// come back, and is given back to the system meanwhile.
	Connections func() int

	// The stricter rule, off unless set (the owner's decision). The
	// thresholds are normally held against what would be left after a
	// collection. RawUnderHardLimit holds them against what is left now,
	// garbage or not, when the limit is a cgroup's. RawFloorShare, if
	// not 0, makes memory tight whenever less than that share of the
	// limit (1/RawFloorShare) is left now, whatever a collection would
	// give back and whatever the limit is.
	RawUnderHardLimit bool
	RawFloorShare     int
	// now is the clock, and patience how long AdmitWait waits; tests
	// set them.
	now      func() time.Time
	patience time.Duration
}

// Guard watches memory. The zero value is not usable; a nil *Guard is, and
// admits everything.
type Guard struct {
	opts Options

	// active is true once memory has been read. Until then, and on a
	// system where it cannot be, everything is admitted.
	active atomic.Bool
	state  atomic.Int32
	// allowance is how many connections may still be admitted before the
	// next look.
	allowance atomic.Int64
	refused   atomic.Int64
	shed      atomic.Int64
	// wanted is set when a connection found no room: the next look
	// collects before it decides. waiting counts those held in AdmitWait,
	// poke asks Run for a look now, and looked is closed when one is done.
	wanted  atomic.Bool
	waiting atomic.Int64
	poke    chan struct{}
	lookMu  sync.Mutex
	looked  chan struct{}

	mu          sync.Mutex
	last        Snapshot
	calm        int
	limit       int64 // the runtime limit last set
	collectedAt time.Time
	since       time.Time // when the present state was entered
	silent      bool      // the "no limit can be read" line has been written

	// A spell of refusing: from the first connection turned away until
	// none has been for refusingFor. Connections are turned away in the
	// normal state too, when more arrive in a second than there is room
	// for above the tight line, and that must be as visible as a state.
	// peak is the most connections there have been since the last
	// collection.
	peak int

	// near is true while the system's own figure is under the tight line
	// (Check says so once).
	near bool

	refusing     bool
	refusedSeen  int64 // refused, at the last look
	refusedFirst int64 // refused, when the spell began
	refusedAt    time.Time
	spellSince   time.Time
}

// refusingFor is how long nothing must be refused before a spell of
// refusing is over. Clients that were turned away come back after about
// thirty seconds, so a spell that is not over yet shows within a minute.
const refusingFor = time.Minute

// Snapshot is how the guard stood at its last look.
type Snapshot struct {
	// Active is false when no limit could be read; the rest is then zero.
	Active bool
	State  State
	// From is what the figures were read from.
	From string
	// Limit is the size of what limits the process, Headroom what the
	// kernel says is left, and AfterCollection what would be left once
	// the runtime's garbage was given back: the figure the thresholds
	// TightUnder and CriticalUnder are held against.
	Limit, Headroom, AfterCollection int64
	TightUnder, CriticalUnder        int64
	// ActsOn is the figure held against the thresholds: AfterCollection,
	// unless the stricter rule is on (Options.RawUnderHardLimit).
	ActsOn int64
	// GoHeld is what the Go runtime holds, GoLimit the limit it was set.
	GoHeld, GoLimit int64
	// Refused is how many connections were turned away, Shed how many
	// were closed, since the server started.
	Refused, Shed int64
	// Refusing is true while connections are being turned away: in the
	// tight and critical states, and in the normal state from the first
	// one refused until none has been for a minute.
	Refusing bool
}

// New makes a guard. Nothing is read until Check or Run.
func New(opts Options) *Guard {
	if opts.Source == nil {
		return nil
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.Runtime == nil {
		opts.Runtime = goRuntime{}
	}
	if opts.now == nil {
		opts.now = time.Now
	}
	if opts.patience == 0 {
		opts.patience = admitPatience
	}
	return &Guard{opts: opts, poke: make(chan struct{}, 1)}
}

// Admit reports whether one more live connection may be taken on. It costs
// one atomic operation, so it can be asked for every connection.
func (g *Guard) Admit() bool {
	if g == nil || !g.active.Load() {
		return true
	}
	if g.allowance.Add(-1) >= 0 {
		return true
	}
	// The next look collects first, in case the room is there and has
	// only not been collected yet.
	g.wanted.Store(true)
	g.refused.Add(1)
	return false
}

// State is the state at the last look.
func (g *Guard) State() State {
	if g == nil {
		return Normal
	}
	return State(g.state.Load())
}

// Snapshot is how the guard stood at its last look.
func (g *Guard) Snapshot() Snapshot {
	if g == nil {
		return Snapshot{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.last
	s.Refused, s.Shed = g.refused.Load(), g.shed.Load()
	return s
}

// Run looks once a second until ctx ends.
func (g *Guard) Run(ctx context.Context) {
	if g == nil {
		return
	}
	g.Check()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			g.Check()
		case <-g.poke:
			// Someone is waiting for room. Not more often than
			// every pokeEvery, however many ask.
			g.Check()
			select {
			case <-ctx.Done():
				return
			case <-time.After(pokeEvery):
			}
		}
	}
}

// measure reads both accounts and works out the figures.
func (g *Guard) measure() (s Snapshot, ok bool) {
	r, ok := g.opts.Source.Read()
	if !ok || r.Total <= 0 {
		return Snapshot{}, false
	}
	hp := g.opts.Runtime.Heap()
	s = Snapshot{Active: true, From: r.From, Limit: r.Total, Headroom: r.Headroom, GoHeld: hp.Held,
		TightUnder: r.Total / tightShare, CriticalUnder: r.Total / criticalShare}
	s.AfterCollection = r.Headroom + max(0, hp.Held-hp.Floor)
	s.ActsOn = s.AfterCollection
	if g.opts.RawUnderHardLimit && r.Hard {
		s.ActsOn = s.Headroom
	}
	if n := int64(g.opts.RawFloorShare); n > 0 && s.Headroom < s.Limit/n {
		// Under the floor: tight at the least.
		s.ActsOn = min(s.ActsOn, s.TightUnder-1)
	}
	return s, true
}

func stateFor(s Snapshot) State {
	switch {
	case s.ActsOn < s.CriticalUnder:
		return Critical
	case s.ActsOn < s.TightUnder:
		return Tight
	}
	return Normal
}

// Check looks once. Run calls it every second; tests call it themselves.
func (g *Guard) Check() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.opts.now()
	s, ok := g.measure()
	if !ok {
		// Nothing to go by: as if there were no guard.
		if g.active.Load() || !g.silent {
			g.silent = true
			g.opts.Log.Info("memory guard: no memory limit can be read on this system, so the guard is off")
		}
		g.active.Store(false)
		g.state.Store(int32(Normal))
		g.last, g.calm = Snapshot{}, 0
		return
	}
	state := State(g.state.Load())
	if !g.active.Load() {
		g.since = now
	}
	if !g.active.Load() || s.From != g.last.From || s.Limit != g.last.Limit {
		// Said at the start, and again if the limit is changed under
		// the running server: which limit is being kept to, and where
		// it was read.
		g.opts.Log.Info("memory guard: watching memory against this limit; new live connections are refused when less than tight_under would be left after a collection",
			"limit", mib(s.Limit), "limit_from", s.From, "left", mib(s.Headroom), "left_after_collection", mib(s.AfterCollection),
			"tight_under", mib(s.TightUnder), "critical_under", mib(s.CriticalUnder), "go_runtime_holds", mib(s.GoHeld))
	}
	want := stateFor(s)
	conns := -1
	if g.opts.Connections != nil {
		conns = g.opts.Connections()
		g.peak = max(g.peak, conns)
	}
	// A figure that looks short is made exact by a collection before
	// anyone is refused on it, because it errs towards trouble in two
	// ways. Everything allocated since the last collection is counted as
	// in use. And what closed connections held is counted as in use
	// until a collection has seen that it is not: after a network blip
	// that is every connection there was, and the people coming back
	// would be turned away for memory that is theirs to have. So the
	// guard collects, gives the memory back to the system, and looks
	// again: when the state would get worse, when a connection found no
	// room (wanted), and when a crowd has just gone.
	crowdGone := conns >= 0 && g.peak-conns >= crowdLeft && conns <= g.peak-g.peak/5
	since := now.Sub(g.collectedAt)
	if (want > state && since >= collectEvery) ||
		(state == Normal && since >= reclaimEvery && (crowdGone || g.wanted.Load())) {
		g.wanted.Store(false)
		g.collect(now)
		g.peak = conns
		if s2, ok := g.measure(); ok {
			s = s2
			want = stateFor(s)
		}
	}
	g.setLimit(&s)

	switch {
	case want > state:
		g.enter(want, state, s, now)
		state = want
	case want < state:
		// Left only when it has been clear of the threshold, by a
		// quarter, for a few looks running.
		to := Normal
		if s.ActsOn < s.TightUnder+s.TightUnder/4 {
			to = Tight
		}
		if state == Critical && s.ActsOn < s.CriticalUnder+s.CriticalUnder/4 {
			to = Critical
		}
		if to < state {
			g.calm++
		} else {
			g.calm = 0
		}
		if g.calm >= calmChecks {
			g.enter(to, state, s, now)
			state = to
		}
	default:
		g.calm = 0
	}

	switch state {
	case Normal:
		// As many as there is room for above the tight threshold, so a
		// crowd arriving between two looks cannot run past it.
		g.allowance.Store((s.ActsOn - s.TightUnder) / PerConnection)
	case Tight:
		g.allowance.Store(0)
		if now.Sub(g.collectedAt) >= tightCollectEvery {
			g.trim()
			g.collect(now)
		}
	case Critical:
		g.allowance.Store(0)
		// Enough to be clear of the threshold again, and no more: the
		// connections that are left should stay.
		if need := s.CriticalUnder + s.CriticalUnder/4 - s.ActsOn; need > 0 && g.opts.Shed != nil {
			if n := g.opts.Shed(need); n > 0 {
				g.shed.Add(int64(n))
				g.opts.Log.Warn("memory guard: closed live connections to stay inside the memory limit, largest queues first",
					"closed", n, "closed_since_start", g.shed.Load(), "left_after_collection", mib(s.AfterCollection), "critical_under", mib(s.CriticalUnder))
			}
		}
		// What they held is only known to be free after a collection,
		// and the next look must see it, or it would close more.
		g.collect(now)
	}
	g.noteRefusals(state, s, now)
	// What the system reports as left can be under the tight line while
	// the state is normal, because the runtime holds garbage it will give
	// back. Someone watching the system's own figure sees a server past
	// its line doing nothing; so that is said, once each time it happens.
	switch {
	case state == Normal && !g.near && s.Headroom < s.TightUnder:
		g.near = true
		g.opts.Log.Info("memory guard: less is left than the tight line, but the Go runtime holds enough that it can give back; not tight, and the runtime now collects harder to stay inside its limit",
			"limit", mib(s.Limit), "limit_from", s.From, "left", mib(s.Headroom), "runtime_can_give_back", mib(s.AfterCollection-s.Headroom),
			"left_after_collection", mib(s.AfterCollection), "tight_under", mib(s.TightUnder), "go_runtime_holds", mib(s.GoHeld), "go_runtime_limit", mib(s.GoLimit))
	case g.near && s.Headroom >= s.TightUnder+s.TightUnder/4:
		g.near = false
	}
	s.State = state
	s.Refusing = g.refusing || state != Normal
	g.last = s
	g.active.Store(true)
	// Whoever is waiting for this look (AdmitWait) tries again.
	g.lookMu.Lock()
	if g.looked != nil {
		close(g.looked)
		g.looked = nil
	}
	g.lookMu.Unlock()
}

// nextLook returns a channel closed when the next look is done.
func (g *Guard) nextLook() <-chan struct{} {
	g.lookMu.Lock()
	defer g.lookMu.Unlock()
	if g.looked == nil {
		g.looked = make(chan struct{})
	}
	return g.looked
}

// AdmitWait is Admit for a connection that can be kept waiting a moment.
// When there is no room left in this second's allowance it does not refuse
// at once: it asks for a look now, with a collection first, and waits for
// that, and for the looks after it, up to admitPatience. After every
// connection has dropped and come straight back, the room they had is
// there; it has only not been collected yet, and a wait of a second or two
// is better for someone than being sent away for thirty. It refuses at
// once when memory is tight or critical, since waiting will not help, and
// when too many are waiting already, since each one waiting holds memory
// too.
func (g *Guard) AdmitWait(ctx context.Context) bool {
	if g == nil || !g.active.Load() {
		return true
	}
	if g.allowance.Add(-1) >= 0 {
		return true
	}
	deadline := time.NewTimer(g.opts.patience)
	defer deadline.Stop()
	for {
		if g.State() != Normal {
			break
		}
		if g.waiting.Add(1) > maxWaiting {
			g.waiting.Add(-1)
			break
		}
		// The channel first and the allowance after: a look that ends
		// between the two has left its allowance to be found here.
		looked := g.nextLook()
		if g.allowance.Add(-1) >= 0 {
			g.waiting.Add(-1)
			return true
		}
		g.wanted.Store(true)
		select {
		case g.poke <- struct{}{}:
		default:
		}
		select {
		case <-looked:
			g.waiting.Add(-1)
			if g.allowance.Add(-1) >= 0 {
				return true
			}
			continue
		case <-deadline.C:
		case <-ctx.Done():
		}
		g.waiting.Add(-1)
		break
	}
	g.refused.Add(1)
	return false
}

// noteRefusals writes one line when connections start being turned away
// and one when that has stopped, whatever the state: nobody is refused
// without the log saying so.
func (g *Guard) noteRefusals(state State, s Snapshot, now time.Time) {
	refused := g.refused.Load()
	fresh := refused - g.refusedSeen
	g.refusedSeen = refused
	if fresh > 0 {
		g.refusedAt = now
		if !g.refusing {
			g.refusing, g.spellSince, g.refusedFirst = true, now, refused-fresh
			g.opts.Log.Warn("memory guard: new live connections are being refused (4029, clients retry in about 30 seconds): more are arriving than there is memory for",
				"state", state.String(), "refused_so_far", fresh, "refused_since_start", refused,
				"limit", mib(s.Limit), "limit_from", s.From, "left", mib(s.Headroom), "left_after_collection", mib(s.AfterCollection),
				"tight_under", mib(s.TightUnder), "critical_under", mib(s.CriticalUnder), "go_runtime_holds", mib(s.GoHeld))
		}
		return
	}
	if g.refusing && state == Normal && now.Sub(g.refusedAt) >= refusingFor {
		g.refusing = false
		g.opts.Log.Info("memory guard: no live connection has been refused for a minute",
			"refused", refused-g.refusedFirst, "over", g.refusedAt.Sub(g.spellSince).Round(time.Second).String(), "refused_since_start", refused,
			"left", mib(s.Headroom), "left_after_collection", mib(s.AfterCollection), "tight_under", mib(s.TightUnder))
	}
}

// enter changes state and writes the one line that says so.
func (g *Guard) enter(to, from State, s Snapshot, now time.Time) {
	args := []any{"from", from.String(), "to", to.String(), "after", now.Sub(g.since).Round(time.Second).String(),
		"limit", mib(s.Limit), "limit_from", s.From, "left", mib(s.Headroom), "left_after_collection", mib(s.AfterCollection),
		"tight_under", mib(s.TightUnder), "critical_under", mib(s.CriticalUnder), "go_runtime_holds", mib(s.GoHeld),
		"refused_since_start", g.refused.Load(), "closed_since_start", g.shed.Load()}
	g.state.Store(int32(to))
	g.calm, g.since = 0, now
	switch {
	case to > from && to == Critical:
		g.opts.Log.Warn("memory guard: memory is critical; new live connections are refused (4029) and the connections with the most queued are being closed (4029) until there is room", args...)
		if from == Normal {
			g.trim()
		}
	case to > from:
		g.opts.Log.Warn("memory guard: memory is tight; new live connections are refused (4029, clients retry in about 30 seconds) until there is room", args...)
		g.trim()
	case to == Tight:
		g.opts.Log.Info("memory guard: memory is no longer critical; connections are no longer closed, new ones are still refused", args...)
	default:
		g.opts.Log.Info("memory guard: memory is back to normal; live connections are admitted again", args...)
	}
}

func (g *Guard) trim() {
	if g.opts.Trim != nil {
		g.opts.Trim()
	}
}

func (g *Guard) collect(now time.Time) {
	g.opts.Runtime.Collect()
	g.collectedAt = now
}

// setLimit keeps the Go runtime's memory limit where holding that much
// would leave the critical threshold free: what it holds now, plus what is
// left, less that reserve. The runtime then collects more often as it
// nears the limit, long before the kernel has anything to do. What else is
// on the machine comes and goes, so the limit follows; it is set again
// only when it has moved by a sixty-fourth of the total.
func (g *Guard) setLimit(s *Snapshot) {
	if g.opts.KeepLimit {
		return
	}
	limit := max(s.GoHeld+s.Headroom-s.CriticalUnder, 16<<20)
	if d := limit - g.limit; g.limit == 0 || d > s.Limit/64 || -d > s.Limit/64 {
		g.opts.Runtime.SetLimit(limit)
		g.limit = limit
	}
	s.GoLimit = g.limit
}

// mib is a number of bytes as the log gives it.
func mib(n int64) string {
	return formatInt(n>>20) + " MiB"
}

func formatInt(n int64) string {
	if n < 0 {
		return "-" + formatInt(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return formatInt(n/10) + string(rune('0'+n%10))
}
