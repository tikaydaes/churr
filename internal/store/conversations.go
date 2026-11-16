package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Conversations is the `conversations` table: named channels, ad-hoc
// channels, sealed chats and the one library conversation.
//
// Every conversation returned has LastSeq (next_seq - 1) and MemberCount
// filled in. MemberIDs, Membership, Shared, ReadOnly and Call are for the
// service to fill.
type Conversations interface {
	// EnsureLibrary returns the library conversation, creating it at first
	// start: a private channel with no name, no members and the member key
	// model.LibraryMemberKey, which holds avatar and emoji files.
	EnsureLibrary(ctx context.Context) (model.Conversation, error)
	// Create inserts a conversation with next_seq 1. An empty ID and zero
	// CreatedAt are filled in. Names are not unique (ruling R50). A member key that another has is
	// model.ErrAlreadyExists. Members are added afterwards, in the same
	// transaction, through Members.
	Create(ctx context.Context, c *model.Conversation) error
	// Get returns one conversation.
	Get(ctx context.Context, id model.ConversationID) (model.Conversation, error)
	// GetMany returns the conversations that exist among ids.
	GetMany(ctx context.Context, ids []model.ConversationID) ([]model.Conversation, error)
	// ByName finds a named channel. Names are not unique (ruling R50): if
	// several channels have the name this returns the oldest, so use it
	// only where any one will do, and ByNameAll otherwise.
	ByName(ctx context.Context, name string) (model.Conversation, error)
	// ByNameAll returns every named channel with this name, oldest first.
	ByNameAll(ctx context.Context, name string) ([]model.Conversation, error)
	// SubChannelCount returns how many channels have this one as parent.
	SubChannelCount(ctx context.Context, parent model.ConversationID) (int64, error)
	// ByMemberKey finds an ad-hoc channel or sealed chat by who is in it
	// (docs/SCHEMA.md decision S8).
	ByMemberKey(ctx context.Context, key string) (model.Conversation, error)

	// Update reads the conversation (locking it on PostgreSQL), lets fn
	// change it, and writes back: name, parent,
	// topic, welcome, section, default level, retention, slow mode,
	// archived_at, member key, last_message_at, moved_from_message_id,
	// ticket number, stream id and key epoch. `private`
	// may go from false to true and never back (model.ErrConflict).
	// next_seq and the lock have their own methods.
	Update(ctx context.Context, id model.ConversationID, fn func(c *model.Conversation) error) (model.Conversation, error)
	// NextSeq takes the conversation's next sequence number: it returns
	// the current next_seq and adds one, in one statement that locks the
	// row, so two writers never get the same number. Call it inside the
	// transaction that writes the event with that number; if the
	// transaction fails the number is not used up.
	NextSeq(ctx context.Context, id model.ConversationID) (int64, error)
	// MakePrivate turns a public channel private and deletes the mentions
	// stored for accounts that are not members (docs/PERMISSIONS.md
	// section 7.1). Making an already private channel private changes
	// nothing.
	MakePrivate(ctx context.Context, id model.ConversationID) (model.Conversation, error)
	// Lock sets locked_at and locked_by for good. In the same go every
	// member who was in through a group becomes a direct member at the
	// level it had, and the channel's groups are detached (decision S16).
	// model.ErrConflict if the conversation is not a private channel or is
	// shared. Locking a locked channel changes nothing.
	Lock(ctx context.Context, id model.ConversationID, by model.AccountID, at model.Time) (model.Conversation, error)
	// RefreshMemberKey works out an ad-hoc channel's member key again
	// after its members changed. If another ad-hoc channel already holds
	// the new key, this one's becomes null and the older keeps it. It does
	// nothing for other kinds of conversation.
	RefreshMemberKey(ctx context.Context, id model.ConversationID) error
	// SetRequestState sets what the chat's request says: model.ChatWaiting,
	// model.ChatDeclined, or "" once it is open. An ad-hoc channel's member
	// key is worked out again.
	SetRequestState(ctx context.Context, id model.ConversationID, state string) error
	// Close closes a direct or sealed chat for good (ruling R78): it sets
	// closed_at and clears the member key, so a new chat between the same
	// people is a new conversation. Closing a closed chat changes nothing.
	Close(ctx context.Context, id model.ConversationID, at model.Time) error
	// Delete removes a conversation and everything that hangs from it,
	// its search entries included, and returns the keys of the blobs its
	// files held so the caller can remove them from the file store.
	Delete(ctx context.Context, id model.ConversationID) (DeletedConversation, error)

	// ForAccount returns the conversations an account is a member of,
	// each with its member row, most recently active first.
	ForAccount(ctx context.Context, account model.AccountID, f ConversationFilter, p Page) (list []ConversationMember, next string, err error)
	// Browse returns named channels ordered by name: the ones the account
	// is a member of, and every public one if f.Public is set.
	Browse(ctx context.Context, f BrowseFilter, p Page) (list []model.Conversation, next string, err error)

	// AdminList returns every named channel, public and private, ordered by
	// name, for the admin console. Ad-hoc channels, sealed chats and the
	// library are never in it.
	AdminList(ctx context.Context, f AdminChannelFilter, p Page) (list []model.Conversation, next string, err error)
}

// AdminChannelFilter narrows Conversations.AdminList.
type AdminChannelFilter struct {
	// Query matches a substring of the name, without regard to case.
	Query string
	// Private keeps only private (true) or only public (false) channels;
	// nil keeps both.
	Private *bool
	// IncludeArchived also returns archived channels.
	IncludeArchived bool
	// ParentID keeps only the sub-channels of this channel.
	ParentID model.ConversationID
}

// AdhocMemberKey is the member key of an ad-hoc channel with exactly these
// members: "a:" and the SHA-256, in hex, of the ids sorted and joined by
// commas. It is here, and not in a backend, because the chat service needs
// the same value to look a channel up.
func AdhocMemberKey(ids []model.AccountID) string { return adhocMemberKey(ids) }

// SealedMemberKey is the member key of the sealed chat between two accounts:
// "s:" and the two ids in sorted order, joined by ":".
func SealedMemberKey(a, b model.AccountID) string {
	if b < a {
		a, b = b, a
	}
	return "s:" + string(a) + ":" + string(b)
}

// ConversationMember is a conversation together with one account's member
// row in it.
type ConversationMember struct {
	Conversation model.Conversation
	Member       model.Member
}

// ConversationFilter narrows Conversations.ForAccount.
type ConversationFilter struct {
	// Kind keeps only "channel" or "sealed"; "" keeps both.
	Kind string
	// IncludeArchived also returns archived conversations.
	IncludeArchived bool
}

// BrowseFilter narrows Conversations.Browse.
type BrowseFilter struct {
	// AccountID is who is browsing; channels it is a member of are always
	// included.
	AccountID model.AccountID
	// Public also includes every public channel: set for owner, admin and
	// member, not for guests and bots.
	Public bool
	// Query matches a substring of the name or topic, without regard to
	// case.
	Query     string
	SectionID model.SectionID
	// ParentID keeps only the sub-channels of this channel.
	ParentID model.ConversationID
	// IncludeArchived also returns archived channels.
	IncludeArchived bool
}

// DeletedConversation is a conversation that was deleted, with the blobs to
// remove from the file store.
type DeletedConversation struct {
	ID       model.ConversationID
	BlobKeys []string
}
