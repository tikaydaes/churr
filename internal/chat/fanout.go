package chat

import (
	"context"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/notify"
)

// A message in a large channel goes to thousands of connections. What it
// means to each reader (`notify`) and the numbers that follow it
// (badge.changed) are therefore not read from the database one reader at a
// time while the fan-out runs: the rule is worked out once for everyone
// before it (notify.Service.Judge), the numbers the notify package keeps
// move with the message, and only readers whose numbers are not kept are
// read, afterwards and off the poster's path.

// fanState is what the service holds while events are fanned out.
type fanState struct {
	mu sync.Mutex
	// judged is the rule worked out for each message.created being
	// published right now, for Tailor to answer from.
	judged map[model.MessageID]*notify.Fanout
	// queue is the badge.changed events that need a read first, in the
	// order they were asked for; waiting holds the same, so one is not
	// queued twice.
	queue   []badgeKey
	waiting map[badgeKey]struct{}
	workers int
	// sending is held from taking kept numbers to handing the event to
	// the hub, so two badge.changed events for one account leave in the
	// order their numbers were taken and the last one is the newest.
	sending sync.Mutex
}

type badgeKey struct {
	account model.AccountID
	conv    model.ConversationID
}

// badgeWorkers is how many goroutines read numbers for badge.changed at
// once. It is small on purpose: this work must never take the database
// connections that requests need.
const badgeWorkers = 4

// publish tells everyone concerned of one committed event: the notify
// package, the hub, and whatever follows a new message.
func (s *Service) publish(ctx context.Context, e model.Event) {
	f := s.judge(ctx, e)
	if f == nil {
		// The kept totals are marked before anyone hears of the event,
		// so a client that asks at once gets the new numbers.
		s.changed(e)
	} else {
		s.notifier.Created(e, f)
		s.fan.mu.Lock()
		if s.fan.judged == nil {
			s.fan.judged = map[model.MessageID]*notify.Fanout{}
		}
		s.fan.judged[e.MessageID] = f
		s.fan.mu.Unlock()
		defer func() {
			s.fan.mu.Lock()
			delete(s.fan.judged, e.MessageID)
			s.fan.mu.Unlock()
		}()
	}
	s.hub.Publish(ctx, e)
	s.afterPublish(ctx, e)
}

// judge works the notification rule out for a message.created event and
// everyone it goes to. It returns nil for any other event, and if the rule
// could not be worked out: each reader is then asked about on its own.
func (s *Service) judge(ctx context.Context, e model.Event) *notify.Fanout {
	if s.notifier == nil || e.Type != model.EventMessageCreated || e.MessageID == "" {
		return nil
	}
	m, ok := createdMessage(e)
	if !ok {
		return nil
	}
	// What Post held of the message when it stored it is not read back.
	var known *notify.Known
	if k, ok := s.fresh.LoadAndDelete(e.MessageID); ok {
		known = k.(*notify.Known)
	}
	f, err := s.notifier.JudgeKnown(ctx, m, e.AlsoTo, known)
	if err != nil {
		s.log.Warn("the notification rule could not be worked out for everyone at once", "err", err)
		return nil
	}
	return f
}

// createdMessage returns the message of a message.created event with the
// fields the notification rule needs. The payload still holds what the
// service put there; after a trip through JSON it is a map.
func createdMessage(e model.Event) (model.Message, bool) {
	var m model.Message
	switch v := e.Payload["message"].(type) {
	case model.Message:
		m = v
	case *model.Message:
		if v == nil {
			return m, false
		}
		m = *v
	default:
		n, _, err := e.Normalize()
		if err != nil {
			return m, false
		}
		return eventMessage(n)
	}
	if m.ID == "" {
		m.ID = e.MessageID
	}
	m.ConversationID, m.Seq = e.ConversationID, e.Seq
	return m, true
}

// judgedLevel answers `notify` for one reader from the fan-out in
// progress, if there is one for the event and it covers the reader.
func (s *Service) judgedLevel(e model.Event, to model.AccountID) (notify.Level, bool) {
	if e.MessageID == "" {
		return notify.None, false
	}
	s.fan.mu.Lock()
	f := s.fan.judged[e.MessageID]
	s.fan.mu.Unlock()
	if f == nil {
		return notify.None, false
	}
	return f.Level(to)
}

// TailorFanout is Tailor for many readers of one event that is being fanned
// out (live.FanoutTailor). For a new message the rule's answers for all of
// them are taken in one go and the readers whose numbers went up are noted
// together: asked one at a time, each reader cost three locks that every
// goroutine of the fan-out was waiting for.
func (s *Service) TailorFanout(ctx context.Context, e model.Event, to []model.AccountID) []map[string]any {
	out := make([]map[string]any, len(to))
	if e.Type != model.EventMessageCreated || s.notifier == nil || e.MessageID == "" {
		for i, id := range to {
			out[i] = s.Tailor(ctx, e, id, true)
		}
		return out
	}
	s.fan.mu.Lock()
	f := s.fan.judged[e.MessageID]
	s.fan.mu.Unlock()
	var (
		levels []notify.Level
		known  []bool
	)
	if f != nil {
		levels, known = f.Levels(to)
	}
	counted := make([]model.AccountID, 0, len(to)/4)
	for i, id := range to {
		var level string
		if known != nil && known[i] {
			level = string(levels[i])
		} else {
			// Someone the rule was not worked out for is asked about
			// alone, as Tailor does.
			level = s.decide(ctx, e, id)
		}
		if level != string(notify.None) {
			counted = append(counted, id)
		}
		out[i] = notifyField(level)
	}
	if len(counted) > 0 {
		// badge.changed follows the message for these (notifier.go).
		s.badgesMu.Lock()
		if s.badges == nil {
			s.badges = map[model.MessageID][]model.AccountID{}
		}
		s.badges[e.MessageID] = append(s.badges[e.MessageID], counted...)
		s.badgesMu.Unlock()
	}
	return out
}

// badgeSoon sends one account badge.changed for a conversation: at once if
// its numbers are kept in memory, and otherwise from the background, where
// they are read. Either way the event follows the message on each
// connection.
func (s *Service) badgeSoon(account model.AccountID, conv model.ConversationID) {
	key := badgeKey{account, conv}
	s.fan.mu.Lock()
	_, queued := s.fan.waiting[key]
	s.fan.mu.Unlock()
	if queued {
		// The read that is coming will see this message too.
		return
	}
	if s.sendKept(account, conv) {
		return
	}
	s.fan.mu.Lock()
	if _, queued := s.fan.waiting[key]; queued {
		s.fan.mu.Unlock()
		return
	}
	if s.fan.waiting == nil {
		s.fan.waiting = map[badgeKey]struct{}{}
	}
	s.fan.waiting[key] = struct{}{}
	s.fan.queue = append(s.fan.queue, key)
	start := s.fan.workers < badgeWorkers
	if start {
		s.fan.workers++
	}
	s.fan.mu.Unlock()
	if start {
		go s.badgeWorker()
	}
}

// badgeWorker sends the queued badge.changed events and ends when none are
// left.
func (s *Service) badgeWorker() {
	for {
		s.fan.mu.Lock()
		if len(s.fan.queue) == 0 {
			s.fan.queue = nil
			s.fan.workers--
			s.fan.mu.Unlock()
			return
		}
		key := s.fan.queue[0]
		s.fan.queue = s.fan.queue[1:]
		// From here on a new message asks for a new read: this one may
		// not see it.
		delete(s.fan.waiting, key)
		s.fan.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		// Reading the numbers also keeps them. What is sent is what is
		// kept by then, which includes any message that arrived during
		// the read; only if they cannot be kept is what was read sent.
		c, total, listed, err := s.notifier.Numbers(ctx, key.account, key.conv)
		cancel()
		if err != nil || s.sendKept(key.account, key.conv) || !listed {
			continue
		}
		s.hub.Send(key.account, s.badgeEvent(key.conv, c, total))
	}
}

// sendKept sends badge.changed from the numbers kept in memory and reports
// whether they could be used.
func (s *Service) sendKept(account model.AccountID, conv model.ConversationID) bool {
	s.fan.sending.Lock()
	defer s.fan.sending.Unlock()
	c, total, listed, ok := s.notifier.Kept(account, conv)
	if ok && listed {
		s.hub.Send(account, s.badgeEvent(conv, c, total))
	}
	return ok
}

func (s *Service) badgeEvent(conv model.ConversationID, c notify.Counts, total model.Totals) model.Event {
	return model.Event{Type: model.EventBadgeChanged, At: s.now(), ConversationID: conv, Payload: map[string]any{
		"unread_count": c.Unread, "mention_count": c.Mentions, "badge_count": c.Badge, "total": total,
	}}
}

// Signal is what conversation.changed says of one reader's numbers in a
// conversation (live.SignalTailor): the same fields as badge.changed, from
// the numbers kept in memory. When they are not at hand it returns nothing,
// and nothing is read: a signal goes to everyone who is not looking, and a
// read for each of them, every second, is what lazy delivery is there to
// spare. The client then counts by the signal's `messages`, and has the
// numbers themselves the next time it asks or badge.changed arrives.
func (s *Service) Signal(to model.AccountID, conv model.ConversationID) map[string]any {
	if s.notifier == nil {
		return nil
	}
	c, total, listed, ok := s.notifier.Kept(to, conv)
	if !ok || !listed {
		return nil
	}
	return map[string]any{"unread_count": c.Unread, "mention_count": c.Mentions, "badge_count": c.Badge, "total": total}
}
