package notify

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Wake answers the two questions a phone wake-up asks of the one rule
// (docs/PUSH.md section 2): does this new message notify the account, with
// do not disturb and a notify-anyway request taken into account (steps 1
// to 4), and is the wake-up of high priority.
//
// It is high for a mention of the account by name, a message in an ad-hoc
// channel or a sealed chat, and a reply in a thread the account follows;
// normal for everything else, which in practice is chatter in a named
// channel on Any activity and group and @channel mentions. Any failure
// reads as "do not wake".
func (s *Service) Wake(ctx context.Context, m model.Message, to model.AccountID) (wake, high bool) {
	acc, err := s.store.Accounts().Get(ctx, to)
	if err != nil {
		return false, false
	}
	v, err := s.judge(ctx, m, acc)
	if err != nil {
		return false, false
	}
	level, err := s.decide(ctx, m, v)
	if err != nil || level != Notify {
		return false, false
	}
	if v.named {
		return true, true
	}
	conv, err := s.store.Conversations().Get(ctx, m.ConversationID)
	if err != nil {
		return true, false
	}
	if conv.Sealed() || conv.Adhoc() {
		return true, true
	}
	if m.ParentID != "" {
		ts, found, err := s.store.Threads().Get(ctx, m.ParentID, to)
		if err == nil && found && ts.State == model.ThreadFollowing {
			return true, true
		}
	}
	return true, false
}

// WouldNotify reports whether a message is one that notifies the account
// by its mode, whatever its do not disturb says right now. The wake-up
// fetch lists such messages after the read position.
func (s *Service) WouldNotify(ctx context.Context, m model.Message, to model.AccountID) bool {
	acc, err := s.store.Accounts().Get(ctx, to)
	if err != nil {
		return false
	}
	v, err := s.judge(ctx, m, acc)
	return err == nil && v.eligible && v.would
}
