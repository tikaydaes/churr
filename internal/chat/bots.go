package chat

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// What the bots package needs of chat beyond posting: resolving text that
// is not a message, and announcing a change to a message that is not an
// edit.

// Entities resolves what the actor could mention in a text typed in a
// conversation, as Post would for a message: accounts, groups, channels the
// actor can see, emoji and links, with positions in the text. Nothing is
// stored and nobody is notified. It is for the text after a command's name
// and for a bot's private reply (docs/BOTS.md section 5).
func (s *Service) Entities(ctx context.Context, actor model.Account, conv model.ConversationID, text string) ([]model.Entity, error) {
	st, err := s.perm.Can(ctx, actor, perm.See, conv)
	if err != nil {
		return nil, hide(err)
	}
	got, err := s.read(ctx, s.store, st, text)
	if err != nil {
		return nil, err
	}
	if got.entities == nil {
		return []model.Entity{}, nil
	}
	return got.entities, nil
}

// EmitMessageChange emits an event that carries a message whole, as nobody
// in particular would see it, after tx changed something about it that is
// not an edit: the images of its cards arriving (docs/BOTS.md section 11),
// which uses the event that adds a link preview.
func (s *Service) EmitMessageChange(ctx context.Context, tx store.Tx, rec *Recorder, eventType string, id model.MessageID) (model.Message, error) {
	m, err := tx.Messages().Get(ctx, id)
	if err != nil {
		return m, err
	}
	conv, err := tx.Conversations().Get(ctx, m.ConversationID)
	if err != nil {
		return m, err
	}
	out, err := s.neutral(ctx, tx, conv, m)
	if err != nil {
		return m, err
	}
	_, err = rec.Emit(ctx, model.Event{Type: eventType, ConversationID: m.ConversationID, MessageID: m.ID,
		Payload: map[string]any{"message": out}})
	return out, err
}
