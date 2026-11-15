package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Members is the `members` and `channel_groups` tables.
//
// The store keeps these true, in the same transaction as whatever changed
// (docs/SCHEMA.md section 5):
//
//   - level is the highest of direct_level and the level of every group
//     added to the conversation that the account is in;
//   - via_group is true if at least one such group contains the account;
//   - a row exists if and only if direct_level is set or via_group is true;
//   - a channel that has members has at least one manager: when the last
//     manager goes, the longest-standing member becomes one;
//   - a private or ad-hoc channel whose last member goes is deleted with its
//     content (ruling R21), and so is a sealed chat with nobody left;
//   - an ad-hoc channel's member key follows its members.
//
// Every method that can change who is in a conversation returns a
// MembershipResult saying what changed, so the service can send events.
type Members interface {
	// Get returns one account's member row. found is false if it is not a
	// member.
	Get(ctx context.Context, conv model.ConversationID, account model.AccountID) (m model.Member, found bool, err error)
	// Standing returns the conversation, the account's member row in it,
	// the end of any time-out of the account in force at `now` there or
	// on the whole server, and the account's bots row, all in one read
	// (standing.go). model.ErrNotFound if there is no such conversation.
	Standing(ctx context.Context, conv model.ConversationID, account model.AccountID, now model.Time) (Standing, error)
	// List returns a conversation's members, longest-standing first. An
	// empty level lists every level.
	List(ctx context.Context, conv model.ConversationID, level model.Level, p Page) (members []model.Member, next string, err error)
	// All returns every member row of a conversation in one read, in no
	// particular order: for a fan-out, which needs each recipient's row
	// and cannot afford to ask for them one at a time.
	All(ctx context.Context, conv model.ConversationID) ([]model.Member, error)
	// IDs returns the account ids of a conversation's members.
	IDs(ctx context.Context, conv model.ConversationID) ([]model.AccountID, error)
	// Roster returns the conversation's members as a fan-out needs them,
	// from memory once they have been read (roster.go). The roster is
	// shared: read it only. Inside a transaction it is read afresh.
	Roster(ctx context.Context, conv model.ConversationID) (*Roster, error)
	// ConversationIDs returns the conversations an account is a member
	// of.
	ConversationIDs(ctx context.Context, account model.AccountID) ([]model.ConversationID, error)
	// Count returns how many members a conversation has.
	Count(ctx context.Context, conv model.ConversationID) (int64, error)
	// OfAccount returns every member row an account has.
	OfAccount(ctx context.Context, account model.AccountID) ([]model.Member, error)
	// Managers returns the accounts whose level in the conversation is
	// manager.
	Managers(ctx context.Context, conv model.ConversationID) ([]model.AccountID, error)

	// AddDirect gives an account a direct level in a conversation: by
	// joining, being added, or creating it. A new row starts with mode
	// `mentions` in a named channel and `any` otherwise, and with read_seq
	// at the conversation's latest seq. model.ErrAlreadyExists if the
	// account already holds a direct level; model.ErrLocked if the
	// conversation is locked.
	AddDirect(ctx context.Context, conv model.ConversationID, account model.AccountID, level model.Level, at model.Time) (MembershipResult, error)
	// SetDirectLevel changes the level a member holds directly (a member
	// who is in only through groups gains a direct level). The level in
	// force stays the highest of direct and group levels.
	// model.ErrNotFound if the account is not a member; model.ErrConflict
	// if the change would leave the conversation without a manager.
	SetDirectLevel(ctx context.Context, conv model.ConversationID, account model.AccountID, level model.Level) (MembershipResult, error)
	// RemoveDirect takes away the level an account holds directly: it
	// left or was removed. If a group still brings it in, the row stays at
	// the group's level; otherwise the row goes, with the account's thread
	// states, mentions and scheduled items in that conversation.
	// model.ErrNotFound if the account holds no direct level there.
	RemoveDirect(ctx context.Context, conv model.ConversationID, account model.AccountID) (MembershipResult, error)

	// SetRead moves an account's read position, clamped to between 0 and
	// the conversation's latest seq. With flat set (the account reads this
	// conversation in the flat view) every thread position of the account
	// in the conversation that is below the new position is raised to it
	// (docs/FEATURES.md section 2.1).
	SetRead(ctx context.Context, conv model.ConversationID, account model.AccountID, seq int64, flat bool) (model.Member, error)
	// UpdateOwn reads a member row, lets fn change the member's own
	// settings (mode, show, mode before mute, muted until, starred, thread view,
	// last post time) and writes them back.
	UpdateOwn(ctx context.Context, conv model.ConversationID, account model.AccountID, fn func(m *model.Member) error) (model.Member, error)
	// SetMayPost records whether a member may post while the conversation
	// is an announcement channel (ruling R66). It returns the row, or
	// model.ErrNotFound for someone who is not a member.
	SetMayPost(ctx context.Context, conv model.ConversationID, account model.AccountID, may bool) (model.Member, error)
	// Unread counts the unread messages of one account in one
	// conversation, split into the sets of docs/FEATURES.md section 2.2.
	Unread(ctx context.Context, conv model.ConversationID, account model.AccountID) (UnreadSets, error)
	// ExpiredMutes returns member rows whose mute ran out at or before
	// `now`, oldest first, at most limit of them, so the rows can be
	// rewritten as for an unmute (docs/FEATURES.md section 2.4).
	ExpiredMutes(ctx context.Context, now model.Time, limit int) ([]model.Member, error)

	// AddGroup adds a group to a named channel at a level, or changes the
	// level of one already there (Added tells which), and brings
	// memberships in step. model.ErrLocked for a locked channel;
	// model.ErrConflict for an ad-hoc channel, a sealed chat or the
	// library.
	AddGroup(ctx context.Context, cg model.ChannelGroup) (res MembershipResult, added bool, err error)
	// RemoveGroup takes a group out of a channel. model.ErrNotFound if it
	// was not there.
	RemoveGroup(ctx context.Context, conv model.ConversationID, group model.GroupID) (MembershipResult, error)
	// Groups returns the groups added to a channel.
	Groups(ctx context.Context, conv model.ConversationID) ([]model.ChannelGroup, error)
}

// The kinds of MemberChange.
const (
	MemberJoined       = "joined"
	MemberLeft         = "left"
	MemberLevelChanged = "level"
)

// MemberChange is how one account's place in one conversation changed.
type MemberChange struct {
	Kind string
	// Member is the row as it now is; for MemberLeft, as it was.
	Member model.Member
}

// MembershipResult is what a change did to memberships.
type MembershipResult struct {
	Changes []MemberChange
	// Deleted lists conversations that were deleted because their last
	// member went. Their blobs must be removed from the file store.
	Deleted []DeletedConversation
}

// Merge adds another result to this one.
func (r *MembershipResult) Merge(o MembershipResult) {
	r.Changes = append(r.Changes, o.Changes...)
	r.Deleted = append(r.Deleted, o.Deleted...)
}

// UnreadSets are the sizes of the sets of unread, counted messages of
// docs/FEATURES.md section 2.2, for one account in one conversation. A
// message is counted when it is not deleted, is not a system notice, was not
// written by the account (unless it is a reminder), is not marked quiet, and
// its author is not blocked by the account. It is unread when it is
// top-level and above the read position, or a reply above its thread's
// position (the account's thread state if it has one, the read position
// otherwise).
type UnreadSets struct {
	Top int64 // TOP: top-level
	Fol int64 // FOL: replies in followed threads
	Nei int64 // NEI: replies in threads neither followed nor ignored
	// Men is MEN: messages with a mention row for the account, leaving
	// out replies in ignored threads unless the row's kind is `account`.
	Men int64
	// Nam is NAM: messages with a mention row of kind `account`.
	Nam int64
	// The parts of Men by where the message is, so unions can be sized:
	// |TOP ∪ FOL ∪ MEN| = Top + Fol + MenNei + MenIgn.
	MenTop, MenFol, MenNei, MenIgn int64
	// AsOf is the conversation's last seq at the moment the sets were
	// read: they count every message up to it and none after. Unread
	// fills it in; whoever keeps the sets and adds later messages itself
	// needs it to know which messages those are.
	AsOf int64
}

// Counts turns the sets into the three numbers of a membership for the mode
// in force and the view (docs/FEATURES.md section 2.3).
func (u UnreadSets) Counts(mode model.Mode, flat bool) (unread, mentions, badge int64) {
	unread = u.Top
	if flat {
		unread = u.Top + u.Fol + u.Nei
	}
	switch mode {
	case model.ModeAny:
		badge = u.Top + u.Fol + u.MenNei + u.MenIgn
		if flat {
			badge = u.Top + u.Fol + u.Nei + u.MenIgn
		}
		mentions = u.Men
	case model.ModeMentions:
		badge = u.Men + u.Fol - u.MenFol
		mentions = u.Men
	case model.ModeNothing:
		// Nothing notifies, and a mention by name still badges.
		badge, mentions = u.Nam, u.Nam
	}
	return unread, mentions, badge
}
