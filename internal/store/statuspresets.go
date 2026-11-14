package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// StatusPresets is the `status_presets` table: saved statuses. A preset
// with no owner is a community one, made by an admin for everyone. Personal
// ones go with their owner when the account is removed.
type StatusPresets interface {
	// Create stores a preset. An empty ID is filled in. OwnerID "" makes a
	// community preset.
	Create(ctx context.Context, p *model.StatusPreset) error
	// Get returns one preset.
	Get(ctx context.Context, id string) (model.StatusPreset, error)
	// Update reads a preset, lets fn change its emoji, text, duration,
	// switches, availability and position, and writes it back. If fn
	// returns an error nothing is written.
	Update(ctx context.Context, id string, fn func(p *model.StatusPreset) error) (model.StatusPreset, error)
	// Delete removes a preset. model.ErrNotFound if it does not exist.
	Delete(ctx context.Context, id string) error
	// List returns the community presets and those of one owner (none of
	// anyone's for ""), community ones first, each part ordered by
	// position and then by id.
	List(ctx context.Context, owner model.AccountID) ([]model.StatusPreset, error)
}
