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

// The kinds of chat a request is for. A group chat request is being
// included in an ad-hoc channel of three or more; a channel request is an
// invitation to a named channel (ruling R103).
const (
	RequestDirect  = "direct"
	RequestSealed  = "sealed"
	RequestGroup   = "group"
	RequestChannel = "channel"
)

// Invitation reports whether a kind of request is to join a conversation
// that already exists, with others in it: a group chat or a named channel.
func Invitation(kind string) bool { return kind == RequestGroup || kind == RequestChannel }

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
	// ChannelInvitesPerDay is how many invitations to named channels one
	// account may have sent in a rolling day that were not accepted.
	ChannelInvitesPerDay = 100
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
	ShownOpen bool
	// Level is the level a channel invitation brings; Override marks one
	// an Owner sent from the admin console.
	Level      Level
	Override   bool
	CreatedAt  Time
	AnsweredAt Time
	ClosedAt   Time
}

// Open reports whether the request still waits on the requester's side.
func (r ChatRequest) Open() bool { return r.State == RequestPending || r.State == RequestDismissed }

// RequestShown is a request as the person asked sees it (schema
// ChatRequest): who is asking and for what kind of chat. For a group chat,
// also who else is in it, by name only; for a named channel, the channel
// as someone outside it is shown it.
type RequestShown struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Account   Account `json:"account"`
	CreatedAt Time    `json:"created_at"`
	// ConversationID is the group chat or channel of an invitation.
	ConversationID ConversationID `json:"conversation_id,omitempty"`
	// People is the names of who else is in a group chat, at most 50.
	People []string `json:"people,omitempty"`
	// Channel is the channel of a channel invitation: its name, access and
	// topic, as someone outside it is shown it. Level is the level it
	// brings; Override marks an Owner's override.
	Channel  *Conversation `json:"channel,omitempty"`
	Level    Level         `json:"level,omitempty"`
	Override bool          `json:"override,omitempty"`
}

// The sources and states of a channel offer (ruling R103).
const (
	OfferGroup    = "group"
	OfferDefault  = "default"
	OfferOffered  = "offered"
	OfferAccepted = "accepted"
	OfferDeclined = "declined"
)

// ChannelOffer is a named channel offered to one person: through a group
// added to it, or as a default channel made one after they signed up. The
// person accepts it (and is then a member) or declines it (and may still
// join while the offer's access lasts).
type ChannelOffer struct {
	ConversationID ConversationID `json:"conversation_id"`
	AccountID      AccountID      `json:"-"`
	Source         string         `json:"source"`
	State          string         `json:"-"`
	// Level is the level accepting brings.
	Level     Level `json:"level"`
	CreatedAt Time  `json:"created_at"`
	// Channel is the channel as someone outside it is shown it.
	Channel *Conversation `json:"channel,omitempty"`
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
