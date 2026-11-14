package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Audit is the `audit_log` table. Rows are never changed or deleted.
type Audit interface {
	// Append writes one entry. An empty ID or zero At is filled in.
	// Detail must never hold message text, file contents, a token, a code
	// or a key.
	Append(ctx context.Context, e model.AuditEntry) error
	// List returns entries newest first.
	List(ctx context.Context, f AuditFilter, p Page) (entries []model.AuditEntry, next string, err error)
}

// AuditFilter narrows Audit.List. Empty fields do not filter.
type AuditFilter struct {
	Action     string
	ActorID    string
	TargetKind string
	TargetID   string
	// ActionPrefix keeps actions that start with this, such as `account.`.
	ActionPrefix string
	// Since keeps entries at or after it; Until, entries before it.
	Since, Until model.Time
}
