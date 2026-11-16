package sched

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/store"
)

// Watches: "tell me when this person is around" (ruling R76,
// docs/FEATURES.md section 9.7). A watch is a scheduled row of kind `watch`
// in its owner's own one-person channel. Its next run is when it expires;
// before then it fires once, as a reminder to its owner alone, when the
// person is next around.
//
// This server watches for its own people, whoever they watch: someone here
// or someone of another server, whose presence arrives over the link. Who
// watches whom never leaves this server, and the person watched is never
// told.
//
// Presence changes all the time, so the cost of a change must not grow with
// the watches: the watches are held in memory by the person watched, and a
// change for anyone nobody watches is one map lookup (Around).

// Presence says whether someone is around now: in truth, and as one viewer
// may see it (where a block makes someone always appear online, ruling
// R77). The people service has it.
type Presence interface {
	Around(ctx context.Context, a model.Account) bool
	AroundFor(ctx context.Context, viewer, a model.Account) (bool, error)
}

// watchIndex is the waiting watches by the person watched.
type watchIndex struct {
	mu sync.RWMutex
	by map[model.AccountID]map[string]struct{}
	// looked counts the changes that were looked into: only those of
	// someone watched. A test reads it.
	looked atomic.Int64
}

func (w *watchIndex) add(watched model.AccountID, item string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.by == nil {
		w.by = map[model.AccountID]map[string]struct{}{}
	}
	if w.by[watched] == nil {
		w.by[watched] = map[string]struct{}{}
	}
	w.by[watched][item] = struct{}{}
}

func (w *watchIndex) remove(watched model.AccountID, item string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.by[watched], item)
	if len(w.by[watched]) == 0 {
		delete(w.by, watched)
	}
}

func (w *watchIndex) of(watched model.AccountID) []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]string, 0, len(w.by[watched]))
	for id := range w.by[watched] {
		out = append(out, id)
	}
	return out
}

func (w *watchIndex) has(watched model.AccountID) bool {
	w.mu.RLock()
	_, ok := w.by[watched]
	w.mu.RUnlock()
	return ok
}

// newWatchLimit is the limit on asking for watches.
func newWatchLimit() *ratelimit.Limiter {
	return ratelimit.New(ratelimit.Per(model.WatchesPerDay, 24*time.Hour), model.WatchesPerDay)
}

// LoadWatches reads the waiting watches into memory. The server calls it
// once as it starts, before people connect, so a watch outlives a restart.
func (s *Service) LoadWatches(ctx context.Context) error {
	list, err := s.store.Scheduled().Waiting(ctx, model.ScheduledWatch, "")
	if err != nil {
		return err
	}
	for _, it := range list {
		s.watches.add(it.WatchAccountID, it.ID)
	}
	return nil
}

// Watch asks to be told once when target is next around. If target is
// around already, as the caller may see them, nothing is stored and around
// is true. Someone who has blocked the caller always appears online to them
// (ruling R77), so for them the answer is the same: around, by the same
// path, with nothing stored.
func (s *Service) Watch(ctx context.Context, actor model.Account, target model.AccountID) (item model.ScheduledItem, around bool, err error) {
	var none model.ScheduledItem
	if !actor.Active() {
		return none, false, model.ErrUnauthenticated
	}
	if ok, retry := s.watchLimit.Allow(string(actor.ID)); !ok {
		return none, false, model.RateLimited(int(retry/time.Second) + 1)
	}
	t, err := s.store.Accounts().Get(ctx, target)
	if err != nil {
		return none, false, err
	}
	if err := s.perm.CanWatch(ctx, actor, t); err != nil {
		return none, false, err
	}
	if s.around != nil {
		if yes, err := s.around.AroundFor(ctx, actor, t); err != nil {
			return none, false, err
		} else if yes {
			return none, true, nil
		}
	}
	mine, _, err := s.chat.FindOrCreateChat(ctx, actor, nil)
	if err != nil {
		return none, false, err
	}
	now := s.now()
	item = model.ScheduledItem{AccountID: actor.ID, ConversationID: mine.ID, Kind: model.ScheduledWatch, WatchAccountID: t.ID,
		NextRunAt: now.Add(model.WatchLifetime), CreatedAt: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		waiting, err := tx.Scheduled().Waiting(ctx, model.ScheduledWatch, actor.ID)
		if err != nil {
			return err
		}
		for _, w := range waiting {
			if w.WatchAccountID == t.ID {
				return model.ErrConflict.With("You are already watching for " + nameOf(t) + ".")
			}
		}
		if len(waiting) >= model.MaxWatches {
			return model.ErrConflict.With("You are watching for 5 people, which is the most there can be. Stop watching for one first.")
		}
		n, err := tx.Scheduled().CountWaiting(ctx, actor.ID)
		if err != nil {
			return err
		}
		if n >= model.MaxWaitingScheduled {
			return model.ErrConflict.With("You have 200 scheduled items waiting, which is the most there can be. Cancel one first.")
		}
		return tx.Scheduled().Create(ctx, &item)
	})
	if err != nil {
		return none, false, err
	}
	s.watches.add(t.ID, item.ID)
	s.tell(actor.ID, model.EventScheduledCreated, item, nil)
	s.poke()
	return item, false, nil
}

// Unwatch cancels the caller's watch for target.
func (s *Service) Unwatch(ctx context.Context, actor model.Account, target model.AccountID) error {
	if !actor.Active() {
		return model.ErrUnauthenticated
	}
	if _, err := s.store.Accounts().Get(ctx, target); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dropWatches(ctx, actor.ID, target) == 0 {
		return model.ErrNotFound.With("You are not watching for them.")
	}
	return nil
}

// Blocked is told when blocker blocks blocked: blocker's watch for them goes,
// as if cancelled. A watch of blocked's for blocker stays waiting, never
// fires and expires.
func (s *Service) Blocked(blocker, blocked model.AccountID) {
	if !s.watches.has(blocked) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropWatches(ctx, blocker, blocked)
}

// dropWatches cancels owner's waiting watches for target and says how many
// went. s.mu is held.
func (s *Service) dropWatches(ctx context.Context, owner, target model.AccountID) int {
	waiting, err := s.store.Scheduled().Waiting(ctx, model.ScheduledWatch, owner)
	if err != nil {
		s.log.Error("watches could not be read", "err", err)
		return 0
	}
	n := 0
	for _, w := range waiting {
		if w.WatchAccountID != target {
			continue
		}
		out, err := s.store.Scheduled().Update(ctx, w.ID, func(cur *model.ScheduledItem) error {
			cur.CancelledAt, cur.NextRunAt = s.now(), 0
			return nil
		})
		if err != nil {
			s.log.Error("a watch could not be cancelled", "scheduled", w.ID, "err", err)
			continue
		}
		s.watches.remove(target, w.ID)
		s.tell(owner, model.EventScheduledCancelled, out, nil)
		n++
	}
	return n
}

// Around is told that others have started being shown an account as online:
// one of this server's, or one of another server's whose presence arrived
// over the link. For an account nobody watches it does nothing more than
// look it up in memory. The watches for it are looked at away from the
// caller, which is the presence path.
func (s *Service) Around(account model.AccountID) {
	if !s.watches.has(account) {
		return
	}
	s.watches.looked.Add(1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.checkWatches(ctx, account)
	}()
}

// checkWatches fires the waiting watches for an account that is around.
// One that may not fire (a block, no longer sharing a conversation) stays
// waiting and nobody is told.
func (s *Service) checkWatches(ctx context.Context, account model.AccountID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	target, err := s.store.Accounts().Get(ctx, account)
	if err != nil {
		return
	}
	// Presence comes and goes; what counts is what is true now.
	if s.around == nil || !s.around.Around(ctx, target) {
		return
	}
	now := s.now()
	for _, id := range s.watches.of(account) {
		item, err := s.store.Scheduled().Get(ctx, id)
		if err != nil || !item.Waiting() || item.Kind != model.ScheduledWatch || item.WatchAccountID != account {
			s.watches.remove(account, id)
			continue
		}
		if !item.NextRunAt.After(now) {
			continue // expired: the loop removes it
		}
		owner, err := s.store.Accounts().Get(ctx, item.AccountID)
		if err != nil {
			continue
		}
		if ok, err := s.perm.WatchMayFire(ctx, owner, target); err != nil || !ok {
			continue
		}
		s.fireWatch(ctx, owner, target, item, now)
	}
}

// fireWatch posts a watch's one message, to its owner alone in their own
// channel, as a reminder: it notifies them, and wakes their phone, as any
// reminder does, which their do not disturb holds back like any other.
// s.mu is held.
func (s *Service) fireWatch(ctx context.Context, owner, target model.Account, item model.ScheduledItem, now model.Time) {
	item.Content = model.MessageInput{Text: aroundText(target)}
	posted, err := s.post(ctx, owner, item, item.NextRunAt, 0, false)
	if err != nil {
		refusal, ours := model.AsError(err)
		if !ours || refusal.Code == model.CodeInternal || refusal.Code == model.CodeUnavailable {
			// Tried again the next time the person is around.
			s.log.Error("a watch could not be posted", "scheduled", item.ID, "err", err)
			return
		}
		out, uerr := s.store.Scheduled().Update(ctx, item.ID, func(cur *model.ScheduledItem) error {
			cur.FailedAt, cur.Failure, cur.FailureMessage, cur.NextRunAt = now, string(refusal.Code), refusal.Message, 0
			return nil
		})
		if uerr == nil {
			s.watches.remove(target.ID, item.ID)
			s.tell(item.AccountID, model.EventScheduledFired, out, map[string]any{"error": refusal})
		}
		return
	}
	out, err := s.store.Scheduled().Update(ctx, item.ID, func(cur *model.ScheduledItem) error {
		cur.LastRunAt, cur.NextRunAt = now, 0
		return nil
	})
	if err != nil {
		// Posted under a client id made from the row, so the next try
		// finds the message and only records it.
		s.log.Error("a watch's run could not be recorded", "scheduled", item.ID, "err", err)
		return
	}
	s.watches.remove(target.ID, item.ID)
	s.tell(item.AccountID, model.EventScheduledFired, out, map[string]any{"message_id": posted.Message.ID})
}

// expireWatch ends a watch whose time is up without its person having been
// around: it goes as if cancelled. s.mu is held.
func (s *Service) expireWatch(ctx context.Context, item model.ScheduledItem) {
	out, err := s.store.Scheduled().Update(ctx, item.ID, func(cur *model.ScheduledItem) error {
		cur.CancelledAt, cur.NextRunAt = s.now(), 0
		return nil
	})
	if err != nil {
		s.log.Error("a watch could not be expired; it will be tried again", "scheduled", item.ID, "err", err)
		s.later[item.ID] = time.Now().Add(retryAfter)
		return
	}
	s.watches.remove(item.WatchAccountID, item.ID)
	s.tell(item.AccountID, model.EventScheduledCancelled, out, nil)
}

// aroundText is what a watch posts: the person, as a mention their owner's
// clients draw with the current name, then "is around". Someone of another
// server is written with their server, which stays as plain text in a
// conversation not shared with it.
func aroundText(a model.Account) string {
	name := "@" + a.Username
	if !a.Local() && a.ServerName != "" {
		name += "@" + a.ServerName
	}
	return name + " is around"
}

// nameOf is how a refusal names an account.
func nameOf(a model.Account) string {
	if a.DisplayName != "" {
		return a.DisplayName
	}
	return a.Username
}
