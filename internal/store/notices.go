package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Notices is the `notices` table: notices kept for one account
// (model.Notice).
type Notices interface {
	// Add stores a notice. An empty ID and a zero CreatedAt are filled in.
	Add(ctx context.Context, n *model.Notice) error
	// List returns an account's notices, newest first.
	List(ctx context.Context, account model.AccountID, p Page) (list []model.Notice, next string, err error)
	// Unseen counts an account's notices not yet marked seen.
	Unseen(ctx context.Context, account model.AccountID) (int, error)
	// MarkSeen marks every notice of an account that is not yet seen as
	// seen at `at`.
	MarkSeen(ctx context.Context, account model.AccountID, at model.Time) error
}
