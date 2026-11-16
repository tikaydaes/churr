package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// ChatRequests is the `chat_requests` and `ghost_members` tables (rulings
// R77, R78; docs/FEATURES.md section 20). Rows go with either account; a
// request's conversation_id becomes null when its conversation is deleted,
// and ghost rows go with their conversation.
type ChatRequests interface {
	// Create stores a request. An empty ID and zero CreatedAt are filled
	// in. A second open request (pending or dismissed) for the same
	// requester, recipient and kind is model.ErrAlreadyExists.
	Create(ctx context.Context, r *model.ChatRequest) error
	// Get returns one request, or model.ErrNotFound.
	Get(ctx context.Context, id string) (model.ChatRequest, error)
	// Open returns the open request (pending or dismissed) from requester
	// to recipient for a kind of chat, with found false if there is none.
	Open(ctx context.Context, requester, recipient model.AccountID, kind string) (model.ChatRequest, bool, error)
	// ByConversation returns the newest request that opened a conversation,
	// with found false if there is none.
	ByConversation(ctx context.Context, conv model.ConversationID) (model.ChatRequest, bool, error)
	// Answer moves a request from one state to another at a time, and
	// returns model.ErrConflict if it was no longer in `from`: two answers
	// at once cannot both win.
	Answer(ctx context.Context, id string, from []string, to string, at model.Time) error
	// SetConversation records the conversation a request opened.
	SetConversation(ctx context.Context, id string, conv model.ConversationID) error
	// Closed notes that the chat a conversation is records closed at a
	// time, on its newest request that has no closing time yet.
	Closed(ctx context.Context, conv model.ConversationID, at model.Time) error
	// Incoming returns the pending requests to an account, oldest first.
	// Whether the requester is blocked is for the caller to apply.
	Incoming(ctx context.Context, recipient model.AccountID) ([]model.ChatRequest, error)
	// LastRefused returns when requester last had a request to recipient,
	// of any kind, declined or withdrawn; zero if never.
	LastRefused(ctx context.Context, requester, recipient model.AccountID) (model.Time, error)
	// Counts returns what requester asked for from since on: requests
	// accepted (or shown open) and not closed within quick of opening, and
	// all the others.
	Counts(ctx context.Context, requester model.AccountID, since model.Time, quick int64) (model.RequestCounts, error)

	// AddGhost shows shownTo the account as a member of the conversation
	// that is not one. Adding one already there changes nothing. An ad-hoc
	// channel's member key is worked out again.
	AddGhost(ctx context.Context, g model.Ghost) error
	// RemoveGhosts takes every ghost of an account out of a conversation,
	// whoever was shown it.
	RemoveGhosts(ctx context.Context, conv model.ConversationID, account model.AccountID) error
	// Ghosts returns the ghosts of a conversation.
	Ghosts(ctx context.Context, conv model.ConversationID) ([]model.Ghost, error)
	// GhostPeers returns the accounts a viewer is shown as members of a
	// conversation the viewer is in, that is open as far as the viewer
	// knows (no request that waits or was declined), without their being
	// members: people who blocked the viewer and whom the viewer "added"
	// or opened a chat with (ruling R77). Presence treats them as people
	// the viewer shares a conversation with, so they are online to the
	// viewer as in a real chat.
	GhostPeers(ctx context.Context, viewer model.AccountID) ([]model.AccountID, error)
	// GhostsShownTo returns, among the conversations given, the ghosts one
	// viewer is shown, by conversation.
	GhostsShownTo(ctx context.Context, viewer model.AccountID, convs []model.ConversationID) (map[model.ConversationID][]model.Ghost, error)
}
