package model

// Knocks (docs/FEATURES.md section 12.5, ruling R74): someone asks the
// managers of a private channel to add them. The one who knocks learns
// nothing of the channel beyond the name they typed, and not even whether it
// exists, until a manager answers.

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

// MaxKnockTargets is how many channels of one name a single knock reaches:
// names are not unique, and the one who knocks cannot tell them apart.
const MaxKnockTargets = 10

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
