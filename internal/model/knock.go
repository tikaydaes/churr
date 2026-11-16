package model

// Knocks (docs/FEATURES.md section 12.5, rulings R74 and R79): someone asks
// the managers of a knock channel, which they can see and not read, to add
// them.

// The events of knocks. They belong to no conversation's log.
const (
	EventKnockCreated  = "knock.created"
	EventKnockAnswered = "knock.answered"
)

// The outcomes of a knock.
const (
	KnockPending  = "pending"
	KnockAccepted = "accepted"
	KnockDeclined = "declined"
)

// Knock is one account's request to be added to one channel (schema Knock).
type Knock struct {
	ID             string         `json:"id"`
	ConversationID ConversationID `json:"conversation_id"`
	AccountID      AccountID      `json:"account_id"`
	CreatedAt      Time           `json:"created_at"`
	Outcome        string         `json:"outcome"`
	// AnsweredByID is the manager who answered. It is left out of what the
	// one who knocked is told, which names no member.
	AnsweredByID AccountID `json:"answered_by_id,omitempty"`
	AnsweredAt   Time      `json:"answered_at,omitempty"`
}
