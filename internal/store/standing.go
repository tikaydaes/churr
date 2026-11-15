package store

import "github.com/tikaydaes/churr/internal/model"

// Standing is what the permission rules need to know of one account in one
// conversation, read together by Members.Standing: every check of every
// request starts with it, and read one row at a time it was four trips to
// the database.
type Standing struct {
	// Conversation is the row as Conversations.Get returns it.
	Conversation model.Conversation
	// Member is the account's member row as Members.Get returns it, or
	// nil if it is not a member.
	Member *model.Member
	// TimedOutUntil is when the account's time-out here or on the whole
	// server ends, as Moderation.TimedOutUntil works it out; zero if none
	// is in force.
	TimedOutUntil model.Time
	// Bot is the account's bots row as Bots.Get returns it, or nil if it
	// has none.
	Bot *BotInfo
}
