package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Invites is the `invites` table. The code is stored as it is, not hashed,
// so an admin can copy the link again (docs/AUTH.md section 4.2).
type Invites interface {
	// Create inserts an invite. An empty ID and zero CreatedAt are filled
	// in. The URL field is not stored; the service fills it when showing.
	Create(ctx context.Context, i *model.Invite) error
	// Get returns one invite by identifier.
	Get(ctx context.Context, id model.InviteID) (model.Invite, error)
	// ByCode returns the invite with this code, usable or not.
	ByCode(ctx context.Context, code string) (model.Invite, error)
	// List returns invites newest first. createdBy "" lists everyone's.
	List(ctx context.Context, createdBy model.AccountID, p Page) (invites []model.Invite, next string, err error)
	// Use counts one use, with an update that only succeeds while the
	// invite is usable at `now` (not revoked, not expired, uses left).
	// model.ErrNotFound if it is not, so two people racing for the last
	// use cannot both win. Call it in the transaction that creates the
	// account.
	Use(ctx context.Context, id model.InviteID, now model.Time) error
	// Revoke withdraws an invite. model.ErrNotFound if it does not exist.
	Revoke(ctx context.Context, id model.InviteID, at model.Time) error
	// DeleteEnded deletes invites that expired, were revoked or were used
	// up before the given moment, for the janitor.
	DeleteEnded(ctx context.Context, before model.Time) (int64, error)
}
