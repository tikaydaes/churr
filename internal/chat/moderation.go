package chat

import (
	"context"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// How the post path uses the Moderator (service.go):
//
//	Post: Check before the transaction; Stored inside it.
//	Edit: CheckEdit before the transaction; Stored inside it.
//
// Scheduled messages, bots and webhooks all post through Post, so they
// pass the same checks; slow mode exempts bots and webhooks inside mod.

// checkEdit runs the word filter over an edit. Slow mode does not apply to
// edits (docs/FEATURES.md section 4.1).
func (s *Service) checkEdit(ctx context.Context, st perm.State, text string) error {
	if s.mod == nil {
		return nil
	}
	return s.mod.CheckEdit(ctx, st, text)
}

// stored tells the moderator, inside the transaction, that a message or an
// edit was stored.
func (s *Service) stored(ctx context.Context, tx store.Tx, rec *Recorder, st perm.State, m model.Message, edit bool) error {
	if s.mod == nil {
		return nil
	}
	after, err := s.mod.Stored(ctx, tx, st, m, edit)
	if after != nil {
		rec.After(after)
	}
	return err
}

// threadOpen refuses a reply to a thread that was moved out to a channel:
// the pointer under the parent sends people there (section 1.6, step 5).
func (s *Service) threadOpen(ctx context.Context, parent model.Message) error {
	moved, err := s.store.Messages().ThreadMovedOut(ctx, parent.ID)
	if err != nil {
		return err
	}
	if moved {
		return model.ErrConflict.With("This thread has moved to a channel of its own. Reply there.")
	}
	return nil
}

// validReaction checks what may be stored as a reaction: a standard emoji,
// or `:name:` for one of this server's custom emoji that exists.
func (s *Service) validReaction(ctx context.Context, emoji string) error {
	if strings.HasPrefix(emoji, ":") && strings.HasSuffix(emoji, ":") && len(emoji) > 2 {
		name := strings.Trim(emoji, ":")
		if !model.ValidEmojiName(name) {
			return model.Invalid("emoji", "A reaction is one emoji.")
		}
		if _, err := s.store.Emoji().ByName(ctx, name); err != nil {
			if model.CodeOf(err) == model.CodeNotFound {
				return model.ErrNotFound.With("There is no custom emoji with that name.")
			}
			return err
		}
		return nil
	}
	return validEmoji(emoji)
}

// systemNotice posts a message the server writes itself (`pinned`, and so
// on) inside the caller's transaction and emits its message.created.
func (s *Service) systemNotice(ctx context.Context, tx store.Tx, rec *Recorder, conv model.Conversation, by model.AccountID, system map[string]any) (model.Message, error) {
	m := model.Message{ID: model.NewID[model.MessageID](), ConversationID: conv.ID, AuthorID: by, System: system,
		Via: &model.Via{Kind: "system"}, CreatedAt: s.now()}
	seq, err := rec.Seq(ctx, conv.ID)
	if err != nil {
		return m, err
	}
	m.Seq, m.Order = seq, rec.Order(conv.ID, seq)
	if err := tx.Messages().Insert(ctx, &m, nil); err != nil {
		return m, err
	}
	out, err := s.neutral(ctx, tx, conv, m)
	if err != nil {
		return m, err
	}
	return out, rec.EmitAt(ctx, model.Event{Type: model.EventMessageCreated, At: m.CreatedAt, ConversationID: conv.ID, Seq: seq,
		MessageID: m.ID, Payload: map[string]any{"message": out}})
}

// audit writes an audit entry for something done in a conversation, unless
// it is an ad-hoc channel: nothing done inside one is written to the log
// admins read (docs/SCHEMA.md section 2).
func (s *Service) audit(ctx context.Context, tx store.Tx, conv model.Conversation, actor model.AccountID, action, targetKind, targetID string, detail map[string]any) error {
	if conv.Adhoc() {
		return nil
	}
	e := model.AuditEntry{At: s.now(), ActorID: string(actor), ActorKind: "account", Action: action, TargetKind: targetKind, TargetID: targetID, Detail: detail}
	if actor == "" {
		e.ActorKind = "server"
	}
	return tx.Audit().Append(ctx, e)
}

// admins returns the owner and the admins: who hears what only admins
// handle.
func admins(ctx context.Context, a store.Areas) ([]model.AccountID, error) {
	g, err := a.Groups().Builtin(ctx, model.GroupAdmins)
	if err != nil {
		return nil, err
	}
	return a.Groups().MemberIDs(ctx, g.ID)
}

// tell sends one event that belongs to no conversation's log to accounts.
func (s *Service) tell(to []model.AccountID, eventType string, payload map[string]any) {
	e := model.Event{Type: eventType, At: s.now(), Payload: payload}
	seen := map[model.AccountID]bool{}
	for _, id := range to {
		if id != "" && !seen[id] {
			seen[id] = true
			s.hub.Send(id, e)
		}
	}
}

// SetModerator hands chat the mod package, which is built after it because
// the one report outcome that deletes a message reaches back into chat.
func (s *Service) SetModerator(m Moderator) { s.mod = m }
