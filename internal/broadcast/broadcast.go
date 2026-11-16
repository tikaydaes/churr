// Package broadcast is the server broadcast (docs/FEATURES.md section 12.6,
// ruling R74): an admin's notice to everyone connected, such as "upgrading
// in five minutes", shown as a dismissable banner until it runs out. It is
// one live event, and the one showing is carried by the status summary that
// every client fetches when it connects. It is not a message in any
// conversation and is kept only in memory, so a restart clears it; the audit
// log keeps the record. It does not cross to linked servers.
package broadcast

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/store"
)

// Hub is what the service needs of the live hub: a frame to every
// connection.
type Hub interface {
	Broadcast(v any)
}

// Service holds the broadcast showing.
type Service struct {
	store   store.Store
	perm    *perm.Checker
	hub     Hub
	now     func() model.Time
	limiter *ratelimit.Limiter

	mu      sync.Mutex
	current *model.Broadcast
}

// New makes the service.
func New(st store.Store, p *perm.Checker, hub Hub) *Service {
	return &Service{store: st, perm: p, hub: hub, now: model.Now,
		// Ten an hour for each admin: enough to correct a mistake or two.
		limiter: ratelimit.New(ratelimit.Per(10, time.Hour), 10)}
}

// Current returns the broadcast showing, or nil when there is none or it has
// run out.
func (s *Service) Current() *model.Broadcast {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil || !s.current.Until.After(s.now()) {
		return nil
	}
	b := *s.current
	return &b
}

// Send shows text to everyone connected for seconds (0 is ten minutes, at
// most a day), in place of any broadcast showing. Admins only. The text is
// one line of plain text with the inline Markdown of messages.
func (s *Service) Send(ctx context.Context, actor model.Account, text string, seconds int64) (model.Broadcast, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.SendBroadcast); err != nil {
		return model.Broadcast{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > model.MaxBroadcastText || strings.IndexFunc(text, unicode.IsControl) >= 0 {
		return model.Broadcast{}, model.Invalid("text", "A broadcast is one line of 1 to 500 characters.")
	}
	if seconds == 0 {
		seconds = model.BroadcastDefaultSeconds
	}
	if seconds < model.BroadcastMinSeconds || seconds > model.BroadcastMaxSeconds {
		return model.Broadcast{}, model.Invalid("seconds", "A broadcast shows for a minute to 24 hours.")
	}
	if res := s.limiter.Take(string(actor.ID)); !res.OK {
		return model.Broadcast{}, model.RateLimited(res.RetryAfterSeconds())
	}
	now := s.now()
	b := model.Broadcast{ID: model.NewID[string](), Text: text, ByID: actor.ID, CreatedAt: now,
		Until: now.Add(time.Duration(seconds) * time.Second)}
	if err := s.store.Audit().Append(ctx, model.AuditEntry{At: now, ActorID: string(actor.ID), ActorKind: "account",
		Action: "broadcast.sent", TargetKind: "broadcast", TargetID: b.ID, Detail: map[string]any{"text": text, "until": b.Until}}); err != nil {
		return model.Broadcast{}, err
	}
	s.mu.Lock()
	s.current = &b
	s.mu.Unlock()
	s.hub.Broadcast(model.Event{Type: model.EventServerBroadcast, At: now, Payload: map[string]any{"broadcast": b}})
	return b, nil
}

// Clear takes the broadcast showing down for everyone. Admins only.
func (s *Service) Clear(ctx context.Context, actor model.Account) error {
	if err := s.perm.CanAdminister(ctx, actor, perm.SendBroadcast); err != nil {
		return err
	}
	cur := s.Current()
	if cur == nil {
		return model.ErrNotFound.With("No broadcast is showing.")
	}
	now := s.now()
	if err := s.store.Audit().Append(ctx, model.AuditEntry{At: now, ActorID: string(actor.ID), ActorKind: "account",
		Action: "broadcast.cleared", TargetKind: "broadcast", TargetID: cur.ID}); err != nil {
		return err
	}
	s.mu.Lock()
	s.current = nil
	s.mu.Unlock()
	s.hub.Broadcast(model.Event{Type: model.EventServerBroadcast, At: now, Payload: map[string]any{}})
	return nil
}
