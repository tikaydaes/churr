package chat

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/notify"
	"github.com/tikaydaes/churr/internal/perm"
)

// A catch-up sends one reader many events from the log. Tailor fills them
// in one at a time, and for a new message that means asking the
// notification rule about one message and one account: ten or so queries
// each. TailorReplay fills in a page at a time for one reader, asking the
// rule about all of the page's new messages at once (notify.Reader). Every
// other kind of event goes through Tailor as before, so what a client is
// sent is the same either way; replay_test.go holds the two against each
// other.

// readerNotifier is the part of the notify package that answers for one
// account and many messages. notify.Service has it; with a Notifier that
// does not, each message is asked about on its own.
type readerNotifier interface {
	Reader(to model.AccountID) *notify.Reader
}

// TailorReplay implements live.ReplayTailor. The function it returns is for
// one catch-up of one account: it returns, for each event, what
// Tailor(ctx, e, to, false) returns. It remembers what it has read about
// the account, so it must not be kept beyond the catch-up.
//
// known is what the caller has just read about the account in some of the
// conversations, if anything: it is not read again.
func (s *Service) TailorReplay(to model.AccountID, known []perm.State) func(ctx context.Context, events []model.Event) []map[string]any {
	var reader *notify.Reader
	if rn, ok := s.notifier.(readerNotifier); ok {
		reader = rn.Reader(to)
		reader.Know(known...)
	}
	return func(ctx context.Context, events []model.Event) []map[string]any {
		out := make([]map[string]any, len(events))
		var at []int
		var msgs []model.Message
		for i, e := range events {
			if e.Type != model.EventMessageCreated || reader == nil {
				out[i] = s.Tailor(ctx, e, to, false)
				continue
			}
			// What decide (notifier.go) answers without reading.
			level := notify.None
			if judged, known := s.judgedLevel(e, to); known {
				level = judged
			} else if m, ok := eventMessage(e); ok {
				at = append(at, i)
				msgs = append(msgs, m)
				continue
			}
			out[i] = replayed(level)
		}
		for j, level := range reader.Decide(ctx, msgs) {
			out[at[j]] = replayed(level)
		}
		return out
	}
}

// replayed is the `notify` of a message.created sent on catch-up: nothing
// pops up for what happened while away.
func replayed(level notify.Level) map[string]any {
	if level == notify.Notify {
		level = notify.Badge
	}
	return map[string]any{"notify": string(level)}
}
