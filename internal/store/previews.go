package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Previews is the `preview_cache` table: what the link-preview fetcher found
// at an address, keyed by the SHA-256 of the normalised address
// (docs/FEATURES.md section 23.2).
type Previews interface {
	// Get returns the cached entry for a hash. found is false if there is
	// none, whatever its age; the caller decides what is fresh.
	Get(ctx context.Context, hash []byte) (e PreviewEntry, found bool, err error)
	// Put stores an entry, replacing the one with the same hash, and
	// returns the entry it replaced so the caller can delete its image.
	Put(ctx context.Context, e PreviewEntry) (old PreviewEntry, replaced bool, err error)
	// Stale returns entries fetched before cardBefore, and entries with no
	// card fetched before emptyBefore, oldest first, for the janitor.
	Stale(ctx context.Context, cardBefore, emptyBefore model.Time, limit int) ([]PreviewEntry, error)
	// Delete removes an entry.
	Delete(ctx context.Context, hash []byte) error
}

// PreviewEntry is one row of `preview_cache`. Card is nil when there was
// nothing usable at the address. A card's image is a file in the library
// conversation that the entry owns.
type PreviewEntry struct {
	Hash      []byte
	URL       string
	Card      *model.Card
	FetchedAt model.Time
}
