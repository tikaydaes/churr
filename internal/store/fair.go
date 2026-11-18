package store

import (
	"context"
	"errors"
)

// Fair keeps the head of each chain of draws (docs/FAIR.md section 6): the
// `fair_chains` table, one row per chain, holding the number of its last
// draw and SHA-256 of that draw's proof. The head moves in the same
// transaction as the message that uses the draw, so a draw is used exactly
// once and none is skipped.
type Fair interface {
	// Head is a chain's last draw and the hash of its proof; n is 0 and
	// prev nil for a chain with none yet.
	Head(ctx context.Context, chain string) (n int64, prev []byte, err error)
	// Advance moves a chain's head to draw n, whose proof hashes to prev.
	// It refuses with ErrFairMoved, changing nothing, unless the head was
	// draw n-1: another message took that draw first.
	Advance(ctx context.Context, chain string, n int64, prev []byte) error
}

// ErrFairMoved is a chain's head that is no longer where a draw was made
// from: another message used the draw, and this one must draw again.
var ErrFairMoved = errors.New("store: the chain's head has moved")
