package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Integrations is the `integrations` table: credentials for programs that
// work on the server as a whole (docs/AUTH.md section 12.4). Only the hash
// of a token is stored.
type Integrations interface {
	// Create stores an integration with the hash of its token. An empty
	// ID and zero CreatedAt are filled in.
	Create(ctx context.Context, i *model.Integration, tokenHash []byte) error
	// Get returns one integration, revoked or not.
	Get(ctx context.Context, id string) (model.Integration, error)
	// ByTokenHash finds the integration a token belongs to, whatever its
	// state; the caller checks revocation.
	ByTokenHash(ctx context.Context, tokenHash []byte) (model.Integration, error)
	// List returns every integration, newest first, revoked ones included.
	List(ctx context.Context) ([]model.Integration, error)
	// Used records a use.
	Used(ctx context.Context, id string, at model.Time) error
	// Revoke ends an integration. Revoking one already revoked changes
	// nothing.
	Revoke(ctx context.Context, id string, at model.Time) error
}
