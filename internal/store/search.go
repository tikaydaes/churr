package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Search is full-text search over channel messages: a generated tsvector
// column on PostgreSQL and the messages_fts table on SQLite
// (docs/SCHEMA.md sections 13 and 15). Deleted messages, history and sealed
// chats are never found.
//
// Typed text never reaches the database as query syntax: the search package
// parses it into terms, and each backend builds its own query from them.
type Search interface {
	// Messages returns matching messages the scope may read, newest first.
	// A query with no terms and no filter returns nothing.
	Messages(ctx context.Context, q SearchQuery, p Page) (msgs []model.Message, next string, err error)
	// Indexed returns how many messages the index holds. The storage
	// tests compare it with the number of messages that should be indexed
	// after each kind of change.
	Indexed(ctx context.Context) (int64, error)
}

// Scope describes what one account may read, in a form queries can join
// against. perm.Readable builds it; nothing else should.
type Scope struct {
	// AccountID: channels this account is a member of are readable.
	AccountID model.AccountID
	// PublicChannels: every public channel is readable too. Set for
	// owner, admin and member; not for guests and bots.
	PublicChannels bool
}

// SearchTerm is one term of a search.
type SearchTerm struct {
	// Text is a word, or for a phrase several words separated by spaces.
	Text string
	// Prefix matches words starting with Text.
	Prefix bool
	// Phrase matches the words next to each other in that order.
	Phrase bool
	// Exclude keeps only messages that do not contain the term.
	Exclude bool
}

// SearchQuery is a parsed search (docs/FEATURES.md section 24). All terms
// must match.
type SearchQuery struct {
	Terms []SearchTerm
	Scope Scope
	// In keeps only this conversation.
	In model.ConversationID
	// From keeps only this author.
	From model.AccountID
	// Since keeps messages posted at or after it; Until, before it.
	Since, Until model.Time
	// HasFile keeps only messages with at least one attached file.
	HasFile bool
}
