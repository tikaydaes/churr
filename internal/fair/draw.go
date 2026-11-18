package fair

import (
	"context"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// Drawer makes a server's draws: the one place the VRF key is applied, and
// only to an input of the one shape (Alpha) whose nonce it has not proved
// under any second still inside the window. It keeps the pairs it has taken,
// in memory, for the seconds inside the window of its clock, and lets each
// second's go once the second has left the window: a pair outside the window
// is refused anyway.
//
// Every pair is also written to the ledger (the draws_used table) before the
// key is applied, and the ledger is read back before the first draw after a
// start. So a restart or a crash never lets a pair draw twice, and nothing
// after a start is refused that would not have been refused before it: an
// honest client is never refused (docs/FAIR.md section 6). The memory is the
// fast path and the one lock; the ledger's rows are deleted, in batches, as
// their seconds leave the window.
//
// The clock may step backwards. The drawer keeps the highest second it has
// read and uses it as "now" for the window's lower bound and for letting
// seconds go, so a step back never reopens a second whose pairs it has let
// go.
type Drawer struct {
	p      Prover
	owner  string
	now    func() time.Time
	ledger Ledger

	mu sync.Mutex
	// loaded is whether the ledger has been read since the start.
	loaded bool
	// high is the highest second the clock has shown.
	high int64
	// forgot is the second before which the ledger was last told to forget.
	forgot int64
	// used is, per second, each nonce taken and who took it: the same
	// request made again (a post retried after no answer came) is drawn
	// again, to the same result, and anyone else's use of the pair is
	// refused.
	used map[int64]map[string]string
}

// Used is one pair of second and nonce taken, and who took it.
type Used struct {
	Second int64
	Nonce  string
	Who    string
}

// Ledger keeps the pairs a drawer has taken across a restart (the store's
// Draws). Take returns only once the pair is durable.
type Ledger interface {
	Take(ctx context.Context, u Used) error
	All(ctx context.Context) ([]Used, error)
	Forget(ctx context.Context, before int64) error
}

// NewDrawer makes the drawer of a server named owner, its key p, its clock
// now (time.Now but in tests), and the ledger it keeps its pairs in.
func NewDrawer(p Prover, owner string, now func() time.Time, ledger Ledger) *Drawer {
	if now == nil {
		now = time.Now
	}
	return &Drawer{p: p, owner: owner, now: now, ledger: ledger, high: now().Unix(), used: map[int64]map[string]string{}}
}

// Owner is the name of the server the drawer draws for.
func (d *Drawer) Owner() string { return d.owner }

// clockErr is the refusal of a second the server will not draw for, with its
// own time.
func clockErr(now time.Time) error {
	e := model.ErrClock.With("The draw's second is more than 10 seconds from the server's clock. An honest client's never is: its clock is kept to the server's. The draw was refused; a new draw takes a new client_id.")
	e.ServerTime = model.FromTime(now)
	return e
}

// errReused is the refusal of a nonce already taken (model.ErrConflict).
var errReused = model.ErrConflict.With("That client_id has drawn already. An honest client sends one again only with the same draw_t, to retry the same message after no answer came; a new draw takes a new client_id.")

// Draw draws for the second t and the nonce (canonical, Canonical), for the
// request named by who (the same who for the same request made again): the
// block the message carries. It refuses, drawing nothing, a second outside
// the window of the clock (model.ErrClock, with the server's time) and a nonce
// already taken under any second it still keeps, by another request or with
// another second (model.ErrConflict).
func (d *Drawer) Draw(ctx context.Context, t int64, nonce, who string) (model.FairBlock, error) {
	if !Canonical(nonce) {
		return model.FairBlock{}, model.Invalid("client_id", "A draw's client_id is a UUID in lowercase, hyphenated, and nothing else.")
	}
	now := d.now()
	secs := now.Unix()
	d.mu.Lock()
	if secs > d.high {
		d.high = secs
	}
	low := d.high - Window
	if t < low || t > secs+Window {
		d.mu.Unlock()
		return model.FairBlock{}, clockErr(now)
	}
	if !d.loaded {
		all, err := d.ledger.All(ctx)
		if err != nil {
			d.mu.Unlock()
			return model.FairBlock{}, err
		}
		for _, u := range all {
			d.keep(u.Second, u.Nonce, u.Who)
		}
		d.loaded = true
	}
	for s := range d.used {
		if s < low {
			delete(d.used, s)
		}
	}
	// A nonce draws once in the whole window: taken under any second still
	// kept, it is refused whatever second it now comes with, but for the
	// same request made again with the same second.
	for s, set := range d.used {
		if had, taken := set[nonce]; taken && (s != t || had != who) {
			d.mu.Unlock()
			return model.FairBlock{}, errReused
		}
	}
	if _, again := d.used[t][nonce]; !again {
		// Durable before the key is applied: a crash after this line draws
		// the same request again, to the same result, and nobody else.
		if err := d.ledger.Take(ctx, Used{Second: t, Nonce: nonce, Who: who}); err != nil {
			d.mu.Unlock()
			return model.FairBlock{}, err
		}
		d.keep(t, nonce, who)
	}
	// The ledger forgets the seconds that have left the window, once per
	// window's length, not with every draw.
	forget := low > d.forgot+Window
	if forget {
		d.forgot = low
	}
	d.mu.Unlock()
	if forget {
		// Every pair still to be taken is for a second at or after low, so
		// this runs outside the lock. A failure leaves rows that the next
		// forgetting deletes.
		_ = d.ledger.Forget(ctx, low)
	}
	alpha := Alpha(t, nonce)
	proof, beta, err := d.p.VRFProve([]byte(alpha))
	if err != nil {
		return model.FairBlock{}, err
	}
	return model.FairBlock{V: BlockVersion, Derive: Derive, Owner: d.owner, PK: d.p.VRFPublic(),
		Alpha: alpha, Beta: beta, Proof: proof, T: t}, nil
}

// keep adds a pair to the memory. The caller holds the lock.
func (d *Drawer) keep(t int64, nonce, who string) {
	set := d.used[t]
	if set == nil {
		set = map[string]string{}
		d.used[t] = set
	}
	set[nonce] = who
}

// pairs is how many seconds the drawer keeps pairs for, for tests.
func (d *Drawer) pairs() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.used)
}
