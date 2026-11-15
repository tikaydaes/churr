package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Messages is the `messages`, `message_versions` and `mentions` tables, and
// the thread summary columns on a parent.
//
// Messages come back as stored: Text, Entities, Cards, Fallback, System, Via,
// Quote, the sealed fields, and the summary columns ReplyCount, LastReplySeq,
// LastReplyAt and LastReplyOrder. What sits beside a message (Tasks, Files, Reactions,
// Thread, Pinned, HasHistory) is filled by Hydrate.
//
// The store keeps the search index in step in the same transaction: a
// message is indexed if and only if it is in a channel, is not deleted and
// has text or fallback (docs/SCHEMA.md section 13).
type Messages interface {
	// Insert stores a new message whose Seq was taken with
	// Conversations.NextSeq in the same transaction. Together with the row
	// it writes the mention rows (with the message's conversation, seq and
	// parent copied in), the search entry, the conversation's
	// last_message_at, and for a reply the parent's summary (reply_count,
	// last_reply_seq, last_reply_at, last_reply_ord). Version starts at 1.
	// model.ErrAlreadyExists if the author already used this client id in
	// this conversation.
	Insert(ctx context.Context, m *model.Message, mentions []model.Mention) error
	// Get returns one message.
	Get(ctx context.Context, id model.MessageID) (model.Message, error)
	// GetMany returns the messages that exist among ids, in ascending seq
	// order within each conversation.
	GetMany(ctx context.Context, ids []model.MessageID) ([]model.Message, error)
	// ByClientID finds the message an author already created with this
	// client id in this conversation.
	ByClientID(ctx context.Context, conv model.ConversationID, author model.AccountID, clientID string) (m model.Message, found bool, err error)
	// List returns a page of a conversation's messages in ascending seq
	// order, and whether there are more in the direction asked for.
	List(ctx context.Context, conv model.ConversationID, o MessageList) (msgs []model.Message, hasMore bool, err error)
	// Replies returns a page of a thread's replies in ascending seq order.
	Replies(ctx context.Context, parent model.MessageID, p SeqPage) (msgs []model.Message, hasMore bool, err error)

	// Edit changes a message's content. With KeepVersion the content it
	// had is first copied to message_versions (reason `edit`, with the ids
	// of the files it carried), version goes up by one and edited_at is
	// set; without it the message is replaced in place, as a quick
	// correction is. Either way the mention rows are replaced by the ones
	// given and the search entry is replaced.
	Edit(ctx context.Context, e MessageEdit) (model.Message, error)
	// Delete carries out an ordinary delete (docs/FEATURES.md section
	// 4.2): copies the content to message_versions with reason `delete`,
	// empties text, entities, cards and fallback, sets deleted_at and
	// deleted_by, marks its files detached, deletes its reactions,
	// mentions, pin and search entry, closes its open form, and for a
	// reply updates the parent's summary. model.ErrConflict if it is
	// already deleted.
	Delete(ctx context.Context, id model.MessageID, by model.AccountID, at model.Time) (model.Message, error)
	// DeleteOutright removes the row with everything that hangs from it,
	// the events that carry it and its search entry, as a delete inside
	// the quick-correction window does, and fixes the parent's summary if
	// it was a reply. It returns the keys of the blobs its files held.
	DeleteOutright(ctx context.Context, id model.MessageID) (blobKeys []string, err error)
	// Purge erases a message's history (docs/SCHEMA.md section 14.2). For
	// a message that is not deleted: its versions, detached files, removed
	// checklist items and the events carrying earlier versions. For a
	// deleted one, also everything else that hung from it, leaving a bare
	// record. Sets purged_at and returns the blob keys to remove.
	Purge(ctx context.Context, id model.MessageID, at model.Time) (blobKeys []string, err error)

	// Versions returns a message's earlier versions, oldest first, with
	// FileIDs set; the caller loads the files.
	Versions(ctx context.Context, id model.MessageID) ([]model.MessageVersion, error)
	// MentionBroadly writes the mention rows of one broad mention of a
	// stored message, for everyone it reaches who has no row for the
	// message yet, and returns how many it wrote. It is one statement
	// whatever the size of the conversation (for BroadHere, one for every
	// two thousand people online). Call it after Insert or Edit, in their
	// transaction, and for the scopes in the order BroadEveryone,
	// BroadChannel, BroadHere, so that the first kind wins.
	MentionBroadly(ctx context.Context, m model.Message, b BroadMention) (int64, error)
	// BroadReach counts the rows MentionBroadly would write for a message
	// by author in conv that mentions nobody else.
	BroadReach(ctx context.Context, conv model.Conversation, author model.AccountID, b BroadMention) (int64, error)
	// Mentions returns the mention rows of a message.
	Mentions(ctx context.Context, id model.MessageID) ([]model.Mention, error)
	// MentionKinds returns the kind of the mention row one account has
	// for each of the messages given that has one, in one read: Mentions
	// for one reader and a page of a catch-up.
	MentionKinds(ctx context.Context, account model.AccountID, ids []model.MessageID) (map[model.MessageID]string, error)

	// Hydrate fills in, for a batch of messages and one reader: Tasks,
	// Files (attached and not detached, in order, with their URLs),
	// Reactions (at most the first ten account ids each, and Me), Thread
	// (on a parent with replies, with the distinct authors of its replies
	// that are not deleted), Pinned and HasHistory (a version exists).
	// A deleted message gets none of the first three. The reader may be
	// "" (Me is then false everywhere).
	//
	// TODO(forms): Form is not filled yet.
	Hydrate(ctx context.Context, msgs []model.Message, reader model.AccountID) error

	// SetHistoryHidden sets history_hidden_at, or clears it when at is
	// zero: while it is set only admins can open the message's history.
	SetHistoryHidden(ctx context.Context, id model.MessageID, at model.Time) error
	// SetCards replaces a message's cards without making a version or
	// touching edited_at: adding and removing link previews is not an edit.
	SetCards(ctx context.Context, id model.MessageID, cards []model.Card) (model.Message, error)
	// MoveThread moves a thread out into a new channel in the ten steps of
	// docs/FEATURES.md section 1.6. See ThreadMove for what the caller
	// prepares. It answers invalid_request for a parent that is a reply, a
	// system notice or has no replies, model.ErrConflict for a thread that
	// was already moved, and model.ErrNameTaken for a name in use.
	MoveThread(ctx context.Context, mv ThreadMove) (ThreadMoved, error)
	// ThreadMovedOut reports whether the thread of this parent was moved
	// out to a channel: a system reply, the pointer, sits under it. Such a
	// thread takes no more replies, even if the new channel has since gone.
	ThreadMovedOut(ctx context.Context, parent model.MessageID) (bool, error)
}

// ThreadMove is what Messages.MoveThread is given.
type ThreadMove struct {
	// ParentID is the top-level message whose replies move.
	ParentID model.MessageID
	// Channel is the new channel to create: its name, privacy, section,
	// retention, default level and creator set by the caller.
	Channel model.Conversation
	// By is who moves the thread; it is the author of the pointer left in
	// the old channel.
	By model.AccountID
	// PointerSeq is the seq taken in the old channel for the pointer and
	// for the thread.moved event.
	PointerSeq int64
	// CopyID is the identifier the copy of the parent gets. CopyCards are
	// the parent's cards as the copy carries them, and CopyFiles the new
	// file rows of the copy, each with a blob of its own that the caller
	// has already written; MessageID and ConversationID are filled in here.
	CopyID    model.MessageID
	CopyCards []model.Card
	CopyFiles []model.File
	At        model.Time
}

// ThreadMoved is what Messages.MoveThread did.
type ThreadMoved struct {
	// Channel is the new channel, Copy its first message.
	Channel model.Conversation
	Copy    model.Message
	// Parent is the parent as it now is in the old channel, and Pointer
	// the system reply left under it.
	Parent  model.Message
	Pointer model.Message
	// Moved are the replies that moved, in their new order.
	Moved []model.MessageID
	// Members says who the new channel's first members are.
	Members MembershipResult
}

// MessageList asks for a page of a conversation's messages.
type MessageList struct {
	SeqPage
	// Around returns the page centred on this seq instead of Before or
	// After: about half the limit on each side, the message itself
	// included.
	Around int64
	// TopLevel leaves replies out, for the threaded view.
	TopLevel bool
}

// MessageEdit is the new content of a message.
type MessageEdit struct {
	ID       model.MessageID
	Text     string
	Entities []model.Entity
	Cards    []model.Card
	Fallback string
	Mentions []model.Mention
	// KeepVersion keeps the earlier content as history.
	KeepVersion bool
	By          model.AccountID
	At          model.Time
}
