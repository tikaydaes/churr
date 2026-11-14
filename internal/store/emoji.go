package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Emoji is the `emoji` table. Every emoji returned has its URL filled in,
// and ServerName for one that came with a shared channel.
type Emoji interface {
	// Create stores one of this server's own emoji. An empty ID and zero
	// CreatedAt are filled in. model.ErrNameTaken if the name is in use.
	Create(ctx context.Context, e *model.Emoji) error
	// Get returns one emoji.
	Get(ctx context.Context, id model.EmojiID) (model.Emoji, error)
	// ByName finds one of this server's own emoji by name.
	ByName(ctx context.Context, name string) (model.Emoji, error)
	// List returns every emoji ordered by name, remote ones included.
	List(ctx context.Context, p Page) (list []model.Emoji, next string, err error)
	// Rename changes a local emoji's name. model.ErrNameTaken if in use.
	Rename(ctx context.Context, id model.EmojiID, name string) (model.Emoji, error)
	// Delete removes the row and returns it; the caller deletes its file.
	Delete(ctx context.Context, id model.EmojiID) (model.Emoji, error)
}
