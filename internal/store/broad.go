package store

import "github.com/tikaydaes/churr/internal/model"

// The three broad mentions (ruling R64).
const (
	// BroadChannel is @channel: every member of the conversation.
	BroadChannel = "channel"
	// BroadHere is @here: the members who are online at that moment.
	BroadHere = "here"
	// BroadEveryone is @everyone: every account in the built-in group
	// that can read the conversation, which in a public channel includes
	// people who have not joined it.
	BroadEveryone = "everyone"
)

// BroadMention is one broad mention as the store writes it. Whoever it
// names, no row is written for the author, for an account that is removed,
// unclaimed or of another server, for a member who has chosen to ignore
// broad mentions in the conversation, or, with Blocking, for someone who
// has blocked the author.
type BroadMention struct {
	// Scope is BroadChannel, BroadHere or BroadEveryone.
	Scope string
	// Online is, for BroadHere, the members who are online. The store
	// does not know who is connected; the caller does.
	Online []model.AccountID
	// Blocking is the server's setting: blocks stop notifications.
	Blocking bool
}

// Kind is the kind the mention's rows carry: @everyone is a group.
func (b BroadMention) Kind() string {
	switch b.Scope {
	case BroadHere:
		return model.MentionHere
	case BroadEveryone:
		return model.MentionGroup
	}
	return model.MentionChannel
}
