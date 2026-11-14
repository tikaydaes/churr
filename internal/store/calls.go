package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// StoredCall is one row of `calls`, with the name of the server that
// carries its media when that is not this one.
type StoredCall struct {
	ID             model.CallID
	ConversationID model.ConversationID
	MessageID      model.MessageID
	StartedBy      model.AccountID
	StartedAt      model.Time
	EndedAt        model.Time
	// HostServerID is empty when this server carries the media.
	HostServerID model.ServerID
	// HostName is the host server's name, read from `servers`.
	HostName string
}

// Live reports whether the call has not ended.
func (c StoredCall) Live() bool { return c.EndedAt.IsZero() }

// CallStay is one row of `call_participants`: one stay of one account in a
// call. Someone who leaves and comes back has two.
type CallStay struct {
	AccountID model.AccountID
	JoinedAt  model.Time
	LeftAt    model.Time
}

// Calls is the `calls` and `call_participants` tables. What is in a call
// right now (tracks, who is forwarded what) is held in memory by the calls
// package and never stored.
type Calls interface {
	// Start adds a live call. c.ID, c.ConversationID, c.StartedBy and
	// c.StartedAt must be set; c.HostServerID names the server carrying
	// the media, or is empty for this one. A conversation has one live
	// call at most: started is false, and nothing is stored, when it
	// already has one (two people starting at the same moment end up in
	// the first one's call).
	Start(ctx context.Context, c StoredCall) (started bool, err error)
	// SetMessage records the call's notice message.
	// model.ErrNotFound if there is no such call.
	SetMessage(ctx context.Context, id model.CallID, msg model.MessageID) error
	// Get returns one call, live or ended. model.ErrNotFound if none.
	Get(ctx context.Context, id model.CallID) (StoredCall, error)
	// Live returns the conversation's live call. found is false when it
	// has none.
	Live(ctx context.Context, conv model.ConversationID) (c StoredCall, found bool, err error)
	// AllLive returns every call with no end, oldest first.
	AllLive(ctx context.Context) ([]StoredCall, error)
	// End sets the call's end and closes the stay of anyone still in it.
	// ended is false, and nothing changes, when the call had ended
	// already or does not exist.
	End(ctx context.Context, id model.CallID, at model.Time) (ended bool, err error)
	// Join adds a stay for the account. An account has one open stay in
	// a call at most: joined is false, and nothing is stored, when it has
	// one already.
	Join(ctx context.Context, id model.CallID, account model.AccountID, at model.Time) (joined bool, err error)
	// Leave closes the account's open stay. left is false when it had
	// none.
	Leave(ctx context.Context, id model.CallID, account model.AccountID, at model.Time) (left bool, err error)
	// Present returns the open stays of a call, in the order joined.
	Present(ctx context.Context, id model.CallID) ([]CallStay, error)
	// EverIn returns the accounts that were in the call at any time, each
	// once, in the order they first joined, at most limit of them.
	EverIn(ctx context.Context, id model.CallID, limit int) ([]model.AccountID, error)
	// SetNotice replaces the `system` object of the call's notice
	// message, as when `call_started` becomes `call_ended`. The message
	// is not marked as edited and keeps no history. It returns the
	// message's conversation. model.ErrNotFound if there is no such
	// message.
	SetNotice(ctx context.Context, msg model.MessageID, system map[string]any) (model.ConversationID, error)
}
