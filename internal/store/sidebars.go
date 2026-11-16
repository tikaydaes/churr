package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Sidebars is the `sidebars` table: each person's own sidebar layout
// (ruling R83). One row per account that has changed anything.
type Sidebars interface {
	// Get returns an account's layout, or an empty one at version 0 if
	// it has none.
	Get(ctx context.Context, account model.AccountID) (model.Sidebar, error)
	// Update reads an account's layout (empty at version 0 if none), lets
	// fn change it, and writes it back one version higher. If fn returns
	// an error nothing is written. If another write got in between,
	// model.ErrConflict.
	Update(ctx context.Context, account model.AccountID, fn func(l *model.Sidebar) error) (model.Sidebar, error)
}
