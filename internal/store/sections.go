package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Sections is the `sections` table: the headings admins define for the
// channel list.
type Sections interface {
	// Create inserts a section. An empty ID is filled in.
	Create(ctx context.Context, s *model.Section) error
	// Get returns one section.
	Get(ctx context.Context, id model.SectionID) (model.Section, error)
	// List returns every section ordered by position, then name.
	List(ctx context.Context) ([]model.Section, error)
	// Update changes a section's name and position.
	Update(ctx context.Context, s model.Section) error
	// Delete removes a section; channels in it then have none.
	Delete(ctx context.Context, id model.SectionID) error
}
