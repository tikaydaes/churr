package model

import (
	"encoding/json"
	"time"
)

// Approvals (docs/FEATURES.md section 31): the instance's one queue of
// requests that wait for someone who may approve them. Each kind says who
// may act on it and what approving and declining do (internal/approvals).

// The kinds of approval. Registrations and channel requests are kept in the
// approvals table; webhook requests are the bots area's own and are shown
// in the same queue.
const (
	ApprovalRegistration = "registration"
	ApprovalChannel      = "channel"
	ApprovalWebhook      = "webhook"
)

// The states of an approval kept in the table.
const (
	ApprovalWaiting  = "waiting"
	ApprovalApproved = "approved"
	ApprovalDeclined = "declined"
	ApprovalLapsed   = "lapsed"
)

// The events of approvals. They belong to no conversation's log.
const (
	EventApprovalCreated = "approval.created"
	EventApprovalDecided = "approval.decided"
)

// ApprovalLife is how long a request waits before it lapses: fourteen days.
const ApprovalLife = 14 * 24 * time.Hour

// MaxApprovalNote is the longest note a requester may add, in characters.
const MaxApprovalNote = 500

// MaxWaitingRegistrations is how many registrations may wait at once; past
// it new ones are refused with rate_limited until some are decided.
const MaxWaitingRegistrations = 200

// Approval is one request in the queue (schema Approval).
type Approval struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	RequesterID AccountID `json:"requester_id"`
	// Summary is what the queue shows the decider: the channel's name, the
	// webhook's name and channel.
	Summary   string          `json:"summary,omitempty"`
	Note      string          `json:"note,omitempty"`
	Payload   json.RawMessage `json:"-"`
	State     string          `json:"state"`
	Reason    string          `json:"reason,omitempty"`
	DecidedBy AccountID       `json:"decided_by_id,omitempty"`
	DecidedAt Time            `json:"decided_at,omitempty"`
	CreatedAt Time            `json:"created_at"`
	ExpiresAt Time            `json:"expires_at,omitempty"`
	// Requester is the requester's account as the decider is shown it.
	Requester *Account `json:"requester,omitempty"`
}

// Waiting reports whether the approval still waits at `now`.
func (a Approval) Waiting(now Time) bool {
	return a.State == ApprovalWaiting && (a.ExpiresAt.IsZero() || now.Before(a.ExpiresAt))
}
