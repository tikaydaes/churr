package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// ReferencePatterns is the `reference_patterns` table: the patterns that
// turn text such as `PROJ-123` into a reference (ruling R73). A row with no
// conversation is the server's; one with a conversation is that channel's
// and goes when the channel does.
//
// The whole table is kept in memory, because every post reads it and
// almost nothing changes it (sqlstore/kept.go).
type ReferencePatterns interface {
	// Create stores a pattern. An empty ID and a zero CreatedAt are
	// filled in.
	Create(ctx context.Context, p *model.ReferencePattern) error
	// Get returns one pattern.
	Get(ctx context.Context, id string) (model.ReferencePattern, error)
	// Update changes a pattern's name, prefix, expression and address
	// template. model.ErrNotFound if it does not exist.
	Update(ctx context.Context, p model.ReferencePattern) error
	// Delete removes a pattern. model.ErrNotFound if it does not exist.
	Delete(ctx context.Context, id string) error
	// All returns every pattern, oldest first.
	All(ctx context.Context) ([]model.ReferencePattern, error)
}
