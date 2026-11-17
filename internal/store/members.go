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
//     added to the conversation that the account is in, where a group
//     counts only for an account that holds a direct level or accepted
//     the channel's offer (ruling R103, channel_offers);
//   - via_group is true if at least one such group counts for the account;
//   - an account that a group gives access to and that is not a member and
//     has no answer yet has an `offered` channel_offers row; one that no
//     group gives access to any more has no row of source `group`;
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
	// Keepers returns the accounts whose level in the conversation is
	// moderator or manager: who answers its requests to join and its
	// reports.
	Keepers(ctx context.Context, conv model.ConversationID) ([]model.AccountID, error)

	// Knock stores a waiting knock, filling in an empty ID and a zero
	// CreatedAt. created is false, and nothing is stored, when the account
	// already has one waiting in that channel.
	Knock(ctx context.Context, k *model.Knock) (created bool, err error)
	// Knocks returns the knocks waiting in a channel, oldest first, leaving
	// out those of accounts that have become members or are no longer
	// active.
	Knocks(ctx context.Context, conv model.ConversationID) ([]model.Knock, error)
	// UnacceptedKnocks counts the knocks an account made from since on
	// that were not accepted (waiting or declined), and gives the oldest
	// one's time.
	UnacceptedKnocks(ctx context.Context, account model.AccountID, since model.Time) (n int, oldest model.Time, err error)
	// AnswerKnock records the answer to a waiting knock and returns it.
	// found is false when there is no such knock waiting in that channel.
	AnswerKnock(ctx context.Context, conv model.ConversationID, id string, by model.AccountID, outcome string, at model.Time) (k model.Knock, found bool, err error)

	// AddDirect gives an account a direct level in a conversation: by
	// joining, being added, or creating it. A new row starts with mode
	// `mentions` in a named channel and `any` otherwise, and with read_seq
	// at the conversation's latest seq. model.ErrAlreadyExists if the
	// account already holds a direct level; model.ErrLocked if the
	// conversation is locked.
	AddDirect(ctx context.Context, conv model.ConversationID, account model.AccountID, level model.Level, at model.Time) (MembershipResult, error)
	// AddByOverride is AddDirect for an Owner's override (docs/PERMISSIONS.md
	// section 6.3): it adds to a locked conversation too.
	AddByOverride(ctx context.Context, conv model.ConversationID, account model.AccountID, level model.Level, at model.Time) (MembershipResult, error)
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

	// Offer returns an account's channel_offers row for a channel, with
	// found false if it has none.
	Offer(ctx context.Context, conv model.ConversationID, account model.AccountID) (model.ChannelOffer, bool, error)
	// Eligible reports whether an account may join a named channel
	// without being invited or knocking (ruling R103): a group added to it
	// contains the account, or it has a default-channel offer there. level
	// is the level joining brings: the highest group level, or the
	// channel's default level for a default-channel offer.
	Eligible(ctx context.Context, conv model.ConversationID, account model.AccountID) (level model.Level, ok bool, err error)
	// AnswerOffer accepts or declines what an account is eligible for in
	// a channel, whether or not it was offered in the sidebar: accepting
	// makes it a member at the level Eligible gives, declining closes the
	// offer and takes away a membership it brought. model.ErrNotFound if
	// the account is not eligible; model.ErrLocked for a locked channel.
	AnswerOffer(ctx context.Context, conv model.ConversationID, account model.AccountID, accept bool, at model.Time) (MembershipResult, error)
	// Offers returns the offers waiting for an account's answer, oldest
	// first, with the level each brings.
	Offers(ctx context.Context, account model.AccountID) ([]model.ChannelOffer, error)
	// OfferDefault offers a channel that has just become a default channel
	// to every active owner, admin and member of the instance who is not
	// in it and has no offer there, and returns them in Changes as
	// MemberOffered. OfferDefault does not check the channel.
	OfferDefault(ctx context.Context, conv model.ConversationID, at model.Time) (MembershipResult, error)
	// WithdrawDefault takes away the default-channel offers of a channel
	// that is no longer a default channel, and returns the ones that were
	// waiting in Changes as MemberOfferGone.
	WithdrawDefault(ctx context.Context, conv model.ConversationID) (MembershipResult, error)
}

// The kinds of MemberChange.
const (
	MemberJoined       = "joined"
	MemberLeft         = "left"
	MemberLevelChanged = "level"
	// MemberOffered: the channel is now offered to the account, which is
	// not a member (ruling R103). MemberOfferGone: an offer that waited
	// was taken away. Member holds the conversation, the account and, for
	// MemberOffered, the level accepting brings.
	MemberOffered   = "offered"
	MemberOfferGone = "offer_gone"
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
