package model

// The types of docs/BOTS.md: bots as admins see them, and webhooks. Command
// and Dialog are in scheduled.go.

// WebhookID names a webhook, from request to revocation.
type WebhookID string

// Bot is a bot account with what admins see about it (schema Bot).
type Bot struct {
	Account     Account          `json:"account"`
	Description string           `json:"description"`
	CreatedByID AccountID        `json:"created_by_id"`
	WebhookOnly bool             `json:"webhook_only"`
	Commands    []Command        `json:"commands,omitempty"`
	ChannelIDs  []ConversationID `json:"channel_ids,omitempty"`
}

// The states of a webhook.
const (
	WebhookWaiting  = "waiting"
	WebhookApproved = "approved"
	WebhookDeclined = "declined"
	WebhookRevoked  = "revoked"
)

// Webhook is a webhook, from request to revocation (schema Webhook). The
// secret address and the signing secret are never part of it.
type Webhook struct {
	ID             WebhookID      `json:"id"`
	ConversationID ConversationID `json:"conversation_id"`
	BotID          AccountID      `json:"bot_id"`
	Name           string         `json:"name"`
	Purpose        string         `json:"purpose"`
	State          string         `json:"state"`
	RequestedByID  AccountID      `json:"requested_by_id"`
	RequestedAt    Time           `json:"requested_at"`
	DecidedByID    AccountID      `json:"decided_by_id,omitempty"`
	DecidedAt      Time           `json:"decided_at,omitempty"`
	// HasSecret says the requester has collected the address. Until then
	// the webhook cannot post.
	HasSecret  bool `json:"has_secret"`
	LastUsedAt Time `json:"last_used_at,omitempty"`
}
