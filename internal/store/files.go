package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Files is the `files` table: one row per stored file. Each row owns its
// blob, thumbnail and poster (docs/SCHEMA.md decision S9). Files returned
// from here have their URLs filled in.
type Files interface {
	// Create inserts an upload, not yet attached to a message. An empty
	// ID, zero CreatedAt, empty Purpose (`attachment`) and empty State
	// (`ready`) are filled in.
	Create(ctx context.Context, f *model.File) error
	// Get returns one file.
	Get(ctx context.Context, id model.FileID) (model.File, error)
	// GetMany returns the files that exist among ids.
	GetMany(ctx context.Context, ids []model.FileID) ([]model.File, error)
	// Attach ties uploads to a message, in the order given. Each must be
	// an upload to that conversation by that uploader that is on no
	// message yet; otherwise nothing is attached and the answer is
	// model.ErrNotFound.
	Attach(ctx context.Context, msg model.MessageID, conv model.ConversationID, uploader model.AccountID, ids []model.FileID) error
	// ForMessage returns a message's files in order. Detached ones (taken
	// off by an edit or delete, kept for history) only if asked for.
	ForMessage(ctx context.Context, msg model.MessageID, includeDetached bool) ([]model.File, error)
	// Detach marks every file of a message detached except the ones to
	// keep, and returns the ones it marked.
	Detach(ctx context.Context, msg model.MessageID, keep []model.FileID, at model.Time) ([]model.File, error)
	// Delete removes a file row and returns it, so the caller can remove
	// its blobs (BlobKey, ThumbKey, PosterKey) from the file store.
	Delete(ctx context.Context, id model.FileID) (model.File, error)
	// Unattached returns uploads created before the given moment that are
	// on no message, oldest first, for the janitor.
	Unattached(ctx context.Context, before model.Time, limit int) ([]model.File, error)

	// Insert stores a file row exactly as given, MessageID, Purpose and
	// Position included: a card's image on a message, an emoji's image, the
	// copies a thread move makes. An empty ID and zero CreatedAt are filled
	// in.
	Insert(ctx context.Context, f *model.File) error
	// CardImages returns the files of purpose `card` on a message: the
	// images of its cards. ForMessage and Detach leave them alone.
	CardImages(ctx context.Context, msg model.MessageID) ([]model.File, error)
	// KeyKnown reports whether any row owns this blob key, as its content,
	// thumbnail or poster. The janitor's sweep asks before removing a blob.
	KeyKnown(ctx context.Context, key string) (bool, error)
	// Totals returns how many file rows there are and the sum of their
	// sizes, for the admin console.
	Totals(ctx context.Context) (count, bytes int64, err error)
}

// BlobKeys returns the keys of the blobs a file row owns.
func BlobKeys(f model.File) []string {
	var keys []string
	for _, k := range []string{f.BlobKey, f.ThumbKey, f.PosterKey} {
		if k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}
