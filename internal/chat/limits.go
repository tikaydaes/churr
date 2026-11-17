package chat

import (
	"sync"
	"time"
	"unicode/utf16"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/ratelimit"
)

// Per-person limits on what reaches many people at once (review finding
// H2): broad mentions, and adding people to conversations. Each is a number
// an hour across the instance, an admin's setting (mentions.broad_per_hour,
// members.adds_per_hour), 0 for no limit. They are kept in memory and
// forgotten on restart, as the other rate limits are. Bots are not held to
// them: an admin made the bot and decides what it does.

// hourly is one such limit. Its limiter is made again when the setting
// changes, which starts everyone afresh.
type hourly struct {
	mu sync.Mutex
	n  int64
	l  *ratelimit.Limiter
}

// limiter returns the limiter for n an hour, or nil for no limit.
func (h *hourly) limiter(n int64) *ratelimit.Limiter {
	if n <= 0 {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.l == nil || h.n != n {
		h.n, h.l = n, ratelimit.New(ratelimit.Per(int(n), time.Hour), int(n))
	}
	return h.l
}

// left says how many more an account may do now under n an hour; -1 is no
// limit.
func (h *hourly) left(actor model.Account, n int64) int {
	l := h.limiter(n)
	if l == nil || actor.Role == model.RoleBot {
		return -1
	}
	return l.Peek(string(actor.ID)).Remaining
}

// take counts k against the account, as far as there is room. It returns
// the wait until one more would fit if there was not room for all k.
func (h *hourly) take(actor model.Account, n int64, k int) (ok bool, retryAfter time.Duration) {
	l := h.limiter(n)
	if l == nil || actor.Role == model.RoleBot {
		return true, 0
	}
	for range k {
		r := l.Take(string(actor.ID))
		if !r.OK {
			return false, r.RetryAfter
		}
	}
	return true, 0
}

// broadIn says whether a stored message's entities hold a broad mention:
// @channel or @here, or the group entity of the text `@everyone`.
func broadIn(text string, ents []model.Entity) bool {
	for _, e := range ents {
		switch e.Type {
		case model.EntityBroadcast:
			return true
		case model.EntityGroup:
			if u := utf16At(text, e.At, e.Len); u == "@"+model.GroupEveryone {
				return true
			}
		}
	}
	return false
}

// utf16At is the part of text an entity covers: at and n count UTF-16 code
// units, as entities do.
func utf16At(text string, at, n int) string {
	u := utf16.Encode([]rune(text))
	if at < 0 || n < 0 || at+n > len(u) {
		return ""
	}
	return string(utf16.Decode(u[at : at+n]))
}

// countAdds counts n people added by actor against the hourly limit, and
// refuses with rate_limited when they do not fit.
func (s *Service) countAdds(actor model.Account, set perm.Settings, n int) error {
	if n == 0 {
		return nil
	}
	left := s.addLimit.left(actor, set.AddsPerHour)
	if left >= 0 && left < n {
		// Nothing is taken: the wait is until the n fit.
		wait := time.Duration(int64(n-left)) * time.Hour / time.Duration(set.AddsPerHour)
		return model.RateLimited(int(wait/time.Second) + 1).With("You have added as many people as this instance allows in an hour. Try again later.")
	}
	s.addLimit.take(actor, set.AddsPerHour, n)
	return nil
}
