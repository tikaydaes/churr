package push

import (
	"context"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/notify"
)

// One message that mentions a channel of five thousand people would send
// five thousand wake-ups in the same instant: to the relay, which limits
// what one server may send, and then to five thousand phones that all come
// back to this server for the message at once. So the wake-ups of one
// message are spread over a short time (ruling R64): a tenth of a second's
// worth go at once, and the rest follow at the same rate. A message that
// wakes a few phones is not held back at all, and the people a message
// names are woken before the people a broad mention reaches.

const (
	// DefaultWakeRate is how many phones one message wakes a second.
	DefaultWakeRate = 200
	// wakeTick is how often a share of them is sent.
	wakeTick = 100 * time.Millisecond
	// wakeTimeout bounds the wake-ups of one message, spread included.
	wakeTimeout = 10 * time.Minute
)

// pace counts the wake-ups of one message and holds them to the rate.
type pace struct {
	s *Service
	// share is how many go in one tick, and n how many have in this one.
	share, n int
}

func (s *Service) newPace() *pace {
	return &pace{s: s, share: max(1, s.wakeRate*int(wakeTick/time.Millisecond)/1000)}
}

// sent is told how many wake-ups were just sent, and waits when the tick's
// share is used up. It reports false when the service is stopping.
func (p *pace) sent(n int) bool {
	p.n += n
	if p.n < p.share {
		return true
	}
	p.n = 0
	t := time.NewTimer(wakeTick)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-p.s.stop:
		return false
	}
}

// wakeNotified wakes the phones of everyone a judged message notifies:
// first the people whose wake-up is of high priority (named, or in a small
// conversation), then the rest, at the rate.
func (s *Service) wakeNotified(ctx context.Context, m model.Message, f *notify.Fanout) error {
	var first, rest []model.AccountID
	err := f.Notified(func(to model.AccountID, high bool) {
		if high {
			first = append(first, to)
		} else {
			rest = append(rest, to)
		}
	})
	if err != nil {
		return err
	}
	all := append(first, rest...)
	// Who has a phone to wake is read for everyone together: most of a
	// large channel has none, or is at a desk.
	phones, err := s.store.Devices().WithWakeable(ctx, all)
	if err != nil {
		return err
	}
	pace := s.newPace()
	for i, to := range all {
		if !phones[to] {
			continue
		}
		if !pace.sent(s.wakeJudged(ctx, m, to, i < len(first))) {
			return nil
		}
	}
	return nil
}
