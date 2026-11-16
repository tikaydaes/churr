package model

import "time"

// Chat requests (rulings R77, R78): a new direct chat or sealed chat with
// someone who does not approve new chats automatically waits for them to
// accept it.

// What a conversation's chat_state says.
const (
	ChatWaiting  = "waiting"
	ChatDeclined = "declined"
	ChatClosed   = "closed"
)

// The kinds of chat a request is for.
const (
	RequestDirect = "direct"
	RequestSealed = "sealed"
)

// The states of a stored request. Only RequestPending is ever shown to the
// person asked; the person asking is shown the chat it opened, waiting,
// declined, open or closed.
const (
	RequestPending   = "pending"
	RequestAccepted  = "accepted"
	RequestDeclined  = "declined"
	RequestDismissed = "dismissed"
	RequestWithdrawn = "withdrawn"
)

// Limits of chat requests (ruling R78).
const (
	// RequestWait is how long someone waits to ask the same person again
	// after a decline or after withdrawing.
	RequestWait = 24 * time.Hour
	// RequestsUnapprovedPerDay is how many requests one account may have
	// made in a rolling day that were not accepted and kept: waiting,
	// declined, dismissed, withdrawn, or accepted and closed again soon.
	RequestsUnapprovedPerDay = 10
	// RequestsKeptPerDay is how many accepted and kept ones.
	RequestsKeptPerDay = 100
	// RequestQuickClose is how soon after it opened a closed chat still
	// counts as not approved.
	RequestQuickClose = 10 * time.Minute
)

// ChatRequest is a stored request. The API shows the person asked only
// RequestShown.
type ChatRequest struct {
	ID             string
	RequesterID    AccountID
	RecipientID    AccountID
	Kind           string
	ConversationID ConversationID
	State          string
	// ShownOpen: the person asked approves automatically but has blocked
	// the one asking, who is shown an open chat that reaches nobody.
	ShownOpen  bool
	CreatedAt  Time
	AnsweredAt Time
	ClosedAt   Time
}

// Open reports whether the request still waits on the requester's side.
func (r ChatRequest) Open() bool { return r.State == RequestPending || r.State == RequestDismissed }

// RequestShown is a request as the person asked sees it (schema
// ChatRequest): who is asking and for what kind of chat, nothing more.
type RequestShown struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Account   Account `json:"account"`
	CreatedAt Time    `json:"created_at"`
}

// Ghost is someone a conversation shows one viewer as a member who is not
// one: the person asked in a chat that waits, or someone who has blocked
// the viewer and was "added" by them (ruling R77).
type Ghost struct {
	ConversationID ConversationID
	AccountID      AccountID
	ShownTo        AccountID
	CreatedAt      Time
}

// RequestCounts is what a requester asked for in the last day, for the caps.
type RequestCounts struct {
	Unapproved int
	Kept       int
}
