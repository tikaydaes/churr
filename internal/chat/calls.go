package chat

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is what the calls package needs of chat: a conversation shows
// its live call, and a call has one notice message that changes when the
// call ends (docs/CALLS.md sections 2 and 19).

// LiveCalls is what chat needs of the calls package to show a conversation.
// calls.Server implements it.
type LiveCalls interface {
	// LiveCall returns the call in progress in a conversation as someone
	// who is not in it sees it, or nil when there is none.
	LiveCall(ctx context.Context, conv model.ConversationID) *model.Call
}

// SetCalls gives the service the calls package. Until then no conversation
// shows a call.
func (s *Service) SetCalls(c LiveCalls) { s.calls = c }

// withCall adds the conversation's live call, so a client that connects
// while a call is on can show it.
func (s *Service) withCall(ctx context.Context, c model.Conversation) model.Conversation {
	if s.calls != nil {
		c.Call = s.calls.LiveCall(ctx, c.ID)
	}
	return c
}

// CallNotice posts a call's notice message (`call_started`) inside the
// caller's transaction, as a system message from whoever started the call,
// and emits its message.created. A system message notifies nobody; the ring
// is the notification.
func (s *Service) CallNotice(ctx context.Context, tx store.Tx, rec *Recorder, conv model.Conversation, by model.AccountID, system map[string]any) (model.Message, error) {
	return s.systemNotice(ctx, tx, rec, conv, by, system)
}

// CallNoticeChanged replaces the `system` object of a call's notice inside
// the caller's transaction and emits message.edited, which is how the
// server says it changed a message it posted itself (docs/EVENTS.md section
// 8). The message is not marked as edited and keeps no history. A notice
// that retention has already removed is not an error: found is false.
func (s *Service) CallNoticeChanged(ctx context.Context, tx store.Tx, rec *Recorder, msg model.MessageID, system map[string]any) (found bool, err error) {
	convID, err := tx.Calls().SetNotice(ctx, msg, system)
	if model.CodeOf(err) == model.CodeNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	conv, err := tx.Conversations().Get(ctx, convID)
	if err != nil {
		return false, err
	}
	m, err := tx.Messages().Get(ctx, msg)
	if err != nil {
		return false, err
	}
	out, err := s.neutral(ctx, tx, conv, m)
	if err != nil {
		return false, err
	}
	_, err = rec.Emit(ctx, model.Event{Type: model.EventMessageEdited, ConversationID: convID, MessageID: msg,
		Payload: map[string]any{"message": out}})
	return true, err
}
