package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Themes is the `themes` table: each account's colour themes (ruling R61).
// The store keeps what it is given; the shape and contrast of the colours
// are checked by the themes service before it gets here. A theme goes with
// its owner when the account is removed.
type Themes interface {
	// Create stores a new theme for its owner. An empty ID is filled in.
	// model.ErrConflict if the owner already has max themes; the count is
	// taken with the owner's account row locked, so two creates at once
	// cannot both pass it.
	Create(ctx context.Context, t *model.Theme, max int) error
	// Get returns one theme. model.ErrNotFound if there is none.
	Get(ctx context.Context, id model.ThemeID) (model.Theme, error)
	// Update reads a theme, lets fn change its name, kind, colours and
	// publication (Published and PublishedAt), sets UpdatedAt to at, and
	// writes it back. If fn returns an error nothing is written.
	Update(ctx context.Context, id model.ThemeID, at model.Time, fn func(t *model.Theme) error) (model.Theme, error)
	// Delete removes a theme. model.ErrNotFound if there is none.
	Delete(ctx context.Context, id model.ThemeID) error
	// ByOwner returns an account's themes, oldest first.
	ByOwner(ctx context.Context, owner model.AccountID) ([]model.Theme, error)
	// Library returns published themes, most recently published first, a
	// page at a time. A non-empty query keeps those whose name contains it,
	// ignoring case.
	Library(ctx context.Context, query string, p Page) (themes []model.Theme, next string, err error)
}
