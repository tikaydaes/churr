package store

import "context"

// DrawUsed is one pair of second and nonce this server's VRF key has drawn
// for, and who asked: the request it was drawn for (docs/FAIR.md section 6).
type DrawUsed struct {
	Second int64
	Nonce  string
	Who    string
}

// Draws is the durable half of the Drawer's tracker of used pairs
// (internal/fair): every pair is written here before the key is applied, and
// read back at start, so no pair draws twice across a restart or a crash.
type Draws interface {
	// Take records a pair. A pair already recorded is left as it is, with
	// its who: the Drawer, which holds every pair in memory too, has
	// already refused anyone else's use of it.
	Take(ctx context.Context, d DrawUsed) error
	// All is every pair recorded, for the Drawer to start from.
	All(ctx context.Context) ([]DrawUsed, error)
	// Forget deletes every pair whose second is before the one given: they
	// have left the window, and a draw for them is refused anyway.
	Forget(ctx context.Context, before int64) error
}
