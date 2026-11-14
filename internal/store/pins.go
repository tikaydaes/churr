package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Pins is the `pins` table.
type Pins interface {
	// Add pins a message. model.ErrAlreadyExists if it is pinned;
	// model.ErrConflict if the conversation already has 100 pins.
	Add(ctx context.Context, conv model.ConversationID, msg model.MessageID, by model.AccountID, at model.Time) (model.Pin, error)
	// Remove unpins a message. model.ErrNotFound if it was not pinned.
	Remove(ctx context.Context, conv model.ConversationID, msg model.MessageID) error
	// List returns a conversation's pins, newest first. Message is left
	// for the service to fill.
	List(ctx context.Context, conv model.ConversationID) ([]model.Pin, error)
}
