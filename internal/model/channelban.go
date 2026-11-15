package model

// Channel bans (docs/FEATURES.md section 19.1, ruling R72): IRC's ban, where
// the server-wide ban is IRC's kline. A banned account is out of one channel
// and cannot come back to it, by joining, being added or through a group,
// until the ban is lifted or runs out.

// The events of channel bans. They belong to no conversation's log.
const (
	EventChannelBanCreated = "channel_ban.created"
	EventChannelBanLifted  = "channel_ban.lifted"
)

// The longest a channel ban with an end may last; a ban may also have none.
const (
	MinChannelBanSeconds = 60
	MaxChannelBanSeconds = 366 * 24 * 3600
)

// ChannelBan is one account banned from one channel (schema ChannelBan).
type ChannelBan struct {
	ID             string         `json:"id"`
	ConversationID ConversationID `json:"conversation_id"`
	AccountID      AccountID      `json:"account_id"`
	Reason         string         `json:"reason"`
	CreatedByID    AccountID      `json:"created_by_id"`
	CreatedAt      Time           `json:"created_at"`
	// ExpiresAt is zero for a ban without an end.
	ExpiresAt Time `json:"expires_at,omitempty"`
	LiftedAt  Time `json:"lifted_at,omitempty"`
}

// InForce reports whether the ban holds at now: not lifted, and not past its
// end if it has one.
func (b ChannelBan) InForce(now Time) bool {
	return b.LiftedAt.IsZero() && (b.ExpiresAt.IsZero() || b.ExpiresAt.After(now))
}
