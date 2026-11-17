package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Approvals is the `approvals` and `approvers` tables: the instance's queue
// of requests that wait for someone who may approve them, and the accounts
// given the flag that lets them (docs/FEATURES.md section 31).
type Approvals interface {
	// Create inserts a waiting approval. An empty ID, a zero CreatedAt and
	// a zero ExpiresAt (model.ApprovalLife after it) are filled in.
	Create(ctx context.Context, a *model.Approval) error
	// Get returns one approval in any state.
	Get(ctx context.Context, id string) (model.Approval, error)
	// Waiting returns the approvals of these kinds that wait at now, oldest
	// first, at most limit.
	Waiting(ctx context.Context, kinds []string, now model.Time, limit int) ([]model.Approval, error)
	// CountWaiting counts the approvals of each kind that wait at now.
	CountWaiting(ctx context.Context, kinds []string, now model.Time) (map[string]int, error)
	// Latest returns the newest approval of a kind an account asked for.
	// found is false when there is none.
	Latest(ctx context.Context, requester model.AccountID, kind string) (a model.Approval, found bool, err error)
	// Decide records the answer to an approval that waits at `at`: state
	// is approved or declined. found is false when it does not wait (it
	// was answered, or lapsed, or never was).
	Decide(ctx context.Context, id, state string, by model.AccountID, reason string, at model.Time) (a model.Approval, found bool, err error)

	// SetApprover gives an account the flag, or takes it away.
	SetApprover(ctx context.Context, account model.AccountID, may bool, by model.AccountID, at model.Time) error
	// IsApprover reports whether an account holds the flag.
	IsApprover(ctx context.Context, account model.AccountID) (bool, error)
	// Approvers lists the accounts that hold the flag.
	Approvers(ctx context.Context) ([]model.AccountID, error)
}
