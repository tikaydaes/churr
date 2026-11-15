package chat

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/notify"
)

// Notifier is what chat needs of the notify package: the one rule for what
// a message means to each person, and the numbers that follow from it.
// notify.Service implements it.
type Notifier interface {
	// Decide says whether a new message notifies an account, only counts
	// for its badge, or neither.
	Decide(ctx context.Context, m model.Message, to model.AccountID) notify.Level
	// Judge works Decide out for a new message and everyone it might
	// concern at once; Created is Changed for that message, moving the
	// kept numbers of those people without reading them again; Kept is
	// Numbers from memory alone, with ok false when that needs a read
	// (fanout.go).
	Judge(ctx context.Context, m model.Message, also []model.AccountID) (*notify.Fanout, error)
	Created(e model.Event, f *notify.Fanout)
	Kept(account model.AccountID, conv model.ConversationID) (c notify.Counts, t model.Totals, listed, ok bool)
	// AnywayApplies says whether a notify-anyway request reaches its
	// target. The answer is never shown to anyone.
	AnywayApplies(ctx context.Context, m model.Message, to model.AccountID) bool
	// Numbers returns an account's counts for one conversation and its
	// totals across all of them.
	Numbers(ctx context.Context, account model.AccountID, conv model.ConversationID) (notify.Counts, model.Totals, bool, error)
	// Totals returns only the totals.
	Totals(ctx context.Context, account model.AccountID) (model.Totals, error)
	// Changed is told of every committed conversation event; Touch and
	// Forget of changes to one account's numbers that have no event.
	Changed(e model.Event)
	Touch(account model.AccountID, conv model.ConversationID)
	Forget(accounts ...model.AccountID)
}

// SetNotifier connects the notify package. It is called once while the
// server is being put together. Without one, nothing notifies and no totals
// are sent.
func (s *Service) SetNotifier(n Notifier) { s.notifier = n }

// eventMessage reads the fields the notification rule needs out of the
// `message` of an event, which is plain JSON by the time it is fanned out.
func eventMessage(e model.Event) (model.Message, bool) {
	msg, _ := e.Payload["message"].(map[string]any)
	if msg == nil {
		return model.Message{}, false
	}
	str := func(k string) string { v, _ := msg[k].(string); return v }
	m := model.Message{
		ID: model.MessageID(str("id")), ConversationID: e.ConversationID, Seq: e.Seq,
		AuthorID: model.AccountID(str("author_id")), ParentID: model.MessageID(str("parent_id")),
	}
	if m.ID == "" {
		m.ID = e.MessageID
	}
	if sys, ok := msg["system"].(map[string]any); ok {
		m.System = sys
	}
	m.Quiet, _ = msg["quiet"].(bool)
	if via, ok := msg["via"].(map[string]any); ok {
		kind, _ := via["kind"].(string)
		m.Via = &model.Via{Kind: kind}
	}
	return m, true
}

// decide asks the notify package what a message.created event means for
// one recipient.
func (s *Service) decide(ctx context.Context, e model.Event, to model.AccountID) string {
	if s.notifier == nil {
		return string(notify.None)
	}
	// While the event is being fanned out the answer is there already,
	// worked out for everyone at once.
	if level, known := s.judgedLevel(e, to); known {
		return string(level)
	}
	m, ok := eventMessage(e)
	if !ok {
		return string(notify.None)
	}
	return string(s.notifier.Decide(ctx, m, to))
}

func (s *Service) changed(e model.Event) {
	if s.notifier != nil {
		s.notifier.Changed(e)
	}
}

func (s *Service) touch(account model.AccountID, conv model.ConversationID) {
	if s.notifier != nil {
		s.notifier.Touch(account, conv)
	}
}

func (s *Service) forget(accounts ...model.AccountID) {
	if s.notifier != nil {
		s.notifier.Forget(accounts...)
	}
}

// total returns an account's totals for an event, if they can be had.
func (s *Service) total(ctx context.Context, account model.AccountID) (model.Totals, bool) {
	if s.notifier == nil {
		return model.Totals{}, false
	}
	t, err := s.notifier.Totals(ctx, account)
	if err != nil {
		s.log.Warn("an account's totals could not be worked out", "err", err)
		return t, false
	}
	return t, true
}

// noteBadge remembers that a message being fanned out counted for a
// connected recipient. Tailor calls it; afterPublish sends the events.
func (s *Service) noteBadge(msg model.MessageID, to model.AccountID) {
	if msg == "" {
		return
	}
	s.badgesMu.Lock()
	if s.badges == nil {
		s.badges = map[model.MessageID][]model.AccountID{}
	}
	s.badges[msg] = append(s.badges[msg], to)
	s.badgesMu.Unlock()
}

// afterPublish sends badge.changed to each connected recipient a new
// message counted for: its counts for the conversation went up
// (docs/EVENTS.md, "Notifications and badges"). It follows the message
// itself on each connection.
func (s *Service) afterPublish(ctx context.Context, e model.Event) {
	if e.Type != model.EventMessageCreated || e.MessageID == "" {
		return
	}
	// Phones are woken for it, if the one rule says so (sealed.go).
	s.wake(e)
	s.badgesMu.Lock()
	to := s.badges[e.MessageID]
	delete(s.badges, e.MessageID)
	s.badgesMu.Unlock()
	if s.notifier == nil {
		return
	}
	for _, account := range to {
		s.badgeSoon(account, e.ConversationID)
	}
}

// notifyFields are the `notify` field of message.created for each level,
// made once: Tailor returns one of them for every reader of every message,
// and the hub only reads what Tailor returns.
var notifyFields = map[string]map[string]any{
	string(notify.None):   {"notify": string(notify.None)},
	string(notify.Badge):  {"notify": string(notify.Badge)},
	string(notify.Notify): {"notify": string(notify.Notify)},
}

func notifyField(level string) map[string]any {
	if f := notifyFields[level]; f != nil {
		return f
	}
	return map[string]any{"notify": level}
}
