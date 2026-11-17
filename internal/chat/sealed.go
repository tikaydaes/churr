package chat

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/notify"
	"github.com/tikaydaes/churr/internal/store"
)

// What chat needs of the packages that handle sealed chats and phone
// wake-ups, and the few places where a sealed chat differs inside this
// package. Sealed messages themselves are posted by the sealed package,
// through Service.Tx like any other change.

// Waker is what chat needs of the push package: it is told of every new
// message once the message has been published, of a notify-anyway request
// that reached its target, and of a chat request that waits for someone.
// All return at once.
type Waker interface {
	MessageCreated(m model.Message)
	NotifiedAnyway(m model.Message, to model.AccountID)
	RequestReceived(to model.AccountID, conv model.ConversationID)
}

// judgedWaker is a Waker that takes the rule as chat has already worked it
// out for the message, in place of working it out again. push.Service is
// one.
type judgedWaker interface {
	MessageJudged(m model.Message, f *notify.Fanout)
}

// SetWaker connects the push package. Without one no phone is woken.
func (s *Service) SetWaker(w Waker) { s.waker = w }

// wake hands a published message.created event to the push package.
func (s *Service) wake(e model.Event) {
	if s.waker == nil || e.Type != model.EventMessageCreated {
		return
	}
	// The rule has been worked out for this message already, for the live
	// delivery: a waker that can use the same answers is handed them, so
	// one message is judged once.
	if jw, ok := s.waker.(judgedWaker); ok {
		s.fan.mu.Lock()
		f := s.fan.judged[e.MessageID]
		s.fan.mu.Unlock()
		if m, ok := createdMessage(e); ok && f != nil {
			jw.MessageJudged(m, f)
			return
		}
	}
	// The payload still holds what the service put there; after a trip
	// through JSON it is a map.
	switch m := e.Payload["message"].(type) {
	case model.Message:
		s.waker.MessageCreated(m)
	case *model.Message:
		s.waker.MessageCreated(*m)
	default:
		if n, _, err := e.Normalize(); err == nil {
			if m, ok := eventMessage(n); ok {
				s.waker.MessageCreated(m)
			}
		}
	}
}

// wakeAnyway hands a notify-anyway request that applied to the push
// package.
func (s *Service) wakeAnyway(m model.Message, to model.AccountID) {
	if s.waker != nil {
		s.waker.NotifiedAnyway(m, to)
	}
}

// SealedChats is what chat needs of the sealed package: whether a sealed
// chat has become read-only for one of its members.
type SealedChats interface {
	ReadOnly(ctx context.Context, actor model.AccountID, conv model.Conversation) bool
}

// SetSealed connects the sealed package.
func (s *Service) SetSealed(sc SealedChats) { s.sealed = sc }

// withSealed marks a sealed chat that accepts nothing new: sealed chats
// are off for the server, or the other person was removed.
func (s *Service) withSealed(ctx context.Context, actor model.Account, c model.Conversation) model.Conversation {
	if s.sealed != nil && c.Sealed() {
		c.ReadOnly = s.sealed.ReadOnly(ctx, actor.ID, c)
	}
	return c
}

// eraseSealed deletes a message of a sealed chat (docs/SEALED.md section
// 8.3): the row's ciphertext and file blobs are erased at once and the
// marker stays, whatever the quick-correction window says. The server
// keeps no history of a sealed chat.
func (s *Service) eraseSealed(ctx context.Context, tx store.Tx, rec *Recorder, id model.MessageID, by model.AccountID, at model.Time) (model.Message, error) {
	marker, keys, err := tx.Sealed().EraseMessage(ctx, id, by, at)
	if err != nil {
		return marker, err
	}
	rec.RemoveBlobs(keys...)
	return marker, nil
}

// sealedShape leaves on a message of a sealed chat only what the server
// knows about it: the files it carries are named inside the ciphertext,
// and the rows the server ties to it are for retention, not for display.
func sealedShape(m *model.Message) {
	m.Files, m.Reactions, m.Tasks, m.Thread, m.HasHistory = nil, nil, nil, nil, false
}
