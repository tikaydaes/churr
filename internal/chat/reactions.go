package chat

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// validEmoji checks what may be stored as a reaction: a standard emoji
// sequence. A custom emoji is written :name:.
func validEmoji(e string) error {
	bad := model.Invalid("emoji", "A reaction is one emoji.")
	if e == "" || !utf8.ValidString(e) || utf8.RuneCountInString(e) > 16 {
		return bad
	}
	if strings.HasPrefix(e, ":") && strings.HasSuffix(e, ":") && len(e) > 2 {
		// TODO(emoji): look the custom emoji up once that area exists.
		return model.ErrNotFound.With("There is no custom emoji with that name.")
	}
	for _, r := range e {
		// Emoji sequences are symbols, marks, joiners and, for flags
		// and keycaps, a few digits and marks; never letters or spaces.
		if unicode.IsLetter(r) || unicode.IsSpace(r) || unicode.IsControl(r) && r != 0x200D || r < 0x80 && !unicode.IsDigit(r) && r != '#' && r != '*' {
			return bad
		}
	}
	return nil
}

// React adds the actor's reaction to a message.
func (s *Service) React(ctx context.Context, actor model.Account, id model.MessageID, emoji string) (model.Reaction, error) {
	_, m, err := s.load(ctx, actor, id, perm.ReactTo)
	if err != nil {
		return model.Reaction{}, err
	}
	if err := s.validReaction(ctx, emoji); err != nil {
		return model.Reaction{}, err
	}
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		added, err := tx.Reactions().Add(ctx, id, actor.ID, emoji, s.now())
		if err != nil || !added {
			return err
		}
		return s.reactionEvent(ctx, tx, rec, model.EventReactionAdded, m, actor.ID, emoji)
	})
	if err != nil {
		return model.Reaction{}, err
	}
	return s.store.Reactions().One(ctx, id, emoji, actor.ID)
}

// Unreact takes the actor's reaction back.
func (s *Service) Unreact(ctx context.Context, actor model.Account, id model.MessageID, emoji string) error {
	_, m, err := s.load(ctx, actor, id, perm.ReadMessage)
	if err != nil {
		return err
	}
	return s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		removed, err := tx.Reactions().Remove(ctx, id, actor.ID, emoji)
		if err != nil {
			return err
		}
		if !removed {
			return model.ErrNotFound
		}
		return s.reactionEvent(ctx, tx, rec, model.EventReactionRemoved, m, actor.ID, emoji)
	})
}

func (s *Service) reactionEvent(ctx context.Context, tx store.Tx, rec *Recorder, eventType string, m model.Message, by model.AccountID, emoji string) error {
	entry, err := tx.Reactions().One(ctx, m.ID, emoji, "")
	if err != nil {
		return err
	}
	if entry.AccountIDs == nil {
		entry.AccountIDs = []model.AccountID{}
	}
	entry.Emoji = emoji
	_, err = rec.Emit(ctx, model.Event{Type: eventType, ConversationID: m.ConversationID, MessageID: m.ID,
		Payload: map[string]any{"message_id": m.ID, "account_id": by, "reaction": entry}})
	return err
}

// Reactions lists every reaction on a message with the accounts behind each.
func (s *Service) Reactions(ctx context.Context, actor model.Account, id model.MessageID) ([]model.Reaction, error) {
	if _, _, err := s.load(ctx, actor, id, perm.ReadMessage); err != nil {
		return nil, err
	}
	return s.store.Reactions().List(ctx, id, actor.ID)
}

// Events returns a conversation's events after a seq, each as the actor
// should see it. reload is true when the log no longer reaches back to
// `after`: the client drops what it holds and loads the conversation again.
func (s *Service) Events(ctx context.Context, actor model.Account, conv model.ConversationID, after, until int64, limit int) (events []json.RawMessage, more, reload bool, err error) {
	st, err := s.perm.Can(ctx, actor, perm.Read, conv)
	if err != nil {
		return nil, false, false, hide(err)
	}
	events = []json.RawMessage{}
	if after < 0 {
		return nil, false, false, model.Invalid("after", "after is 0 or more.")
	}
	if after >= st.Conversation.LastSeq {
		return events, false, false, nil
	}
	oldest, err := s.store.Events().Oldest(ctx, conv)
	if err != nil {
		return nil, false, false, err
	}
	if oldest == 0 || oldest > after+1 {
		return events, false, true, nil
	}
	list, more, err := s.store.Events().List(ctx, conv, after, until, limit)
	if err != nil {
		return nil, false, false, err
	}
	for _, e := range list {
		if b := s.hub.Tailored(ctx, e, actor.ID); b != nil {
			events = append(events, b)
		}
	}
	return events, more, false, nil
}

// Tailor fills in what differs per reader in an event. It implements
// live.Tailor; the event arrives in plain JSON form.
func (s *Service) Tailor(ctx context.Context, e model.Event, to model.AccountID, live bool) map[string]any {
	if extra, handled := s.tailorFormEvent(ctx, e, to); handled {
		return extra
	}
	if extra, handled := s.tailorConversationEvent(ctx, e, to); handled {
		return extra
	}
	switch e.Type {
	case model.EventMessageCreated:
		level := s.notify(ctx, e, to)
		// On catch-up nothing pops up for what happened while away.
		if !live && level == "notify" {
			level = "badge"
		}
		if live && level != "none" {
			// The recipient's numbers went up; badge.changed follows
			// the message (notifier.go).
			s.noteBadge(e.MessageID, to)
		}
		// One of three maps everyone shares: a channel's ten thousand
		// readers must not cost one each (notifier.go).
		return notifyField(level)
	case model.EventMessageEdited, model.EventMessageDeleted:
		msg, _ := e.Payload["message"].(map[string]any)
		// The form on the message is filled in for this reader; msg is
		// then a copy, and the event stays as it is for the others.
		msg, changed := s.tailorMessageForm(ctx, e, msg, to)
		// The event's payload is shared by every recipient of the fan-out,
		// so `me` is never written into it: the reactions this reader made
		// are marked on copies, and the message is copied to carry them.
		if list, mine := s.reactionsFor(ctx, msg, e.MessageID, to); mine {
			out := make(map[string]any, len(msg))
			for k, v := range msg {
				out[k] = v
			}
			out["reactions"] = list
			msg, changed = out, true
		}
		if !changed {
			return nil
		}
		return map[string]any{"message": msg}
	case model.EventReactionAdded, model.EventReactionRemoved:
		entry, _ := e.Payload["reaction"].(map[string]any)
		if entry == nil || !s.isMine(ctx, entry, e.MessageID, to) {
			return nil
		}
		return map[string]any{"reaction": withMe(entry)}
	}
	return nil
}

// reactionsFor returns a message's reactions as one reader sees them and
// whether any of them is the reader's own. The list is new and the entries
// that gain `me` are copies; the message it came from is not touched.
func (s *Service) reactionsFor(ctx context.Context, msg map[string]any, id model.MessageID, to model.AccountID) ([]any, bool) {
	list, _ := msg["reactions"].([]any)
	var out []any
	for i, r := range list {
		entry, ok := r.(map[string]any)
		if !ok || !s.isMine(ctx, entry, id, to) {
			continue
		}
		if out == nil {
			out = append(make([]any, 0, len(list)), list...)
		}
		out[i] = withMe(entry)
	}
	return out, out != nil
}

// withMe returns a copy of a reaction entry with `me` set, leaving the
// shared entry as it was.
func withMe(entry map[string]any) map[string]any {
	out := make(map[string]any, len(entry)+1)
	for k, v := range entry {
		out[k] = v
	}
	out["me"] = true
	return out
}

// isMine reports whether a reaction entry includes one reader. It changes
// nothing. An entry lists at most ten accounts; beyond that the store is
// asked, which answers for now and not for when the event happened.
func (s *Service) isMine(ctx context.Context, entry map[string]any, msg model.MessageID, to model.AccountID) bool {
	ids, _ := entry["account_ids"].([]any)
	for _, id := range ids {
		if id == string(to) {
			return true
		}
	}
	count, _ := entry["count"].(float64)
	emoji, _ := entry["emoji"].(string)
	if int(count) > len(ids) && msg != "" {
		if has, err := s.store.Reactions().Has(ctx, msg, emoji, to); err == nil && has {
			return true
		}
	}
	return false
}

// notify says what a new message means for one recipient: `none`, `badge`
// or `notify`. The rule is the notify package's (notifier.go).
func (s *Service) notify(ctx context.Context, e model.Event, to model.AccountID) string {
	return s.decide(ctx, e, to)
}
