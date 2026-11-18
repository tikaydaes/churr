package fair

import (
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// Drawer makes a server's draws: the one place the VRF key is applied, and
// only to an input of the one shape (Alpha) whose pair of second and nonce it
// has not proved before. It keeps the pairs it has taken, in memory, for the
// seconds inside the window of its clock, and lets each second's go once the
// second has left the window: a pair outside the window is refused anyway.
//
// After a start nothing is kept, so for the length of the window a draw for a
// second no later than the start's is refused: a pair taken just before a
// restart cannot be taken again.
type Drawer struct {
	p     Prover
	owner string
	now   func() time.Time

	mu      sync.Mutex
	started int64
	// used is, per second, each nonce taken and who took it: the same
	// request made again (a post retried after a failure) is drawn again,
	// to the same result, and anyone else's use of the pair is refused.
	used map[int64]map[string]string
}

// NewDrawer makes the drawer of a server named owner, its key p, its clock
// now (time.Now but in tests).
func NewDrawer(p Prover, owner string, now func() time.Time) *Drawer {
	if now == nil {
		now = time.Now
	}
	return &Drawer{p: p, owner: owner, now: now, started: now().Unix(), used: map[int64]map[string]string{}}
}

// Owner is the name of the server the drawer draws for.
func (d *Drawer) Owner() string { return d.owner }

// clockErr is the refusal of a second the server will not draw for, with its
// own time for the client to correct its clock by.
func clockErr(now time.Time, why string) error {
	e := model.ErrClock.With(why)
	e.ServerTime = model.FromTime(now)
	return e
}

// Draw draws for the second t and the nonce (canonical, Canonical), for the request named by who
// (the same who for the same request made again): the block the message
// carries. It refuses, drawing nothing, a second outside the window of the
// clock (model.ErrClock, with the server's time), one it cannot vouch for
// just after a start (the same), and a pair already taken for another
// request (model.ErrConflict).
func (d *Drawer) Draw(t int64, nonce, who string) (model.FairBlock, error) {
	if !Canonical(nonce) {
		return model.FairBlock{}, model.Invalid("client_id", "A draw's client_id is a UUID in lowercase, hyphenated, and nothing else.")
	}
	now := d.now()
	secs := now.Unix()
	if t < secs-Window || t > secs+Window {
		return model.FairBlock{}, clockErr(now, "The draw's second is more than 10 seconds from the server's clock. Correct the clock by server_time and send again.")
	}
	d.mu.Lock()
	for s := range d.used {
		if s < secs-Window {
			delete(d.used, s)
		}
	}
	if secs <= d.started+Window && t <= d.started {
		d.mu.Unlock()
		return model.FairBlock{}, clockErr(now, "The server has just started and cannot draw for a second before it did. Send again with the current second.")
	}
	set := d.used[t]
	if set == nil {
		set = map[string]string{}
		d.used[t] = set
	}
	if had, taken := set[nonce]; taken && had != who {
		d.mu.Unlock()
		return model.FairBlock{}, model.ErrConflict.With("That client_id has drawn for this second already. Make a new one for a new message; send the same one again only to retry the same message.")
	}
	set[nonce] = who
	d.mu.Unlock()
	alpha := Alpha(t, nonce)
	proof, beta, err := d.p.VRFProve([]byte(alpha))
	if err != nil {
		return model.FairBlock{}, err
	}
	return model.FairBlock{V: BlockVersion, Derive: Derive, Owner: d.owner, PK: d.p.VRFPublic(),
		Alpha: alpha, Beta: beta, Proof: proof, T: t}, nil
}

// pairs is how many seconds the drawer keeps pairs for, for tests.
func (d *Drawer) pairs() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.used)
}
