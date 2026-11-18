// Package sched is everything that happens later: messages sent at a chosen
// time, reminders, and recurring check-in forms (docs/FEATURES.md sections 9
// to 11). It also holds saved replies and the command menu (commands.go).
//
// A scheduled item is a row; Service.Run is the one loop that fires rows as
// they fall due. Firing posts through chat.Service.Post as the item's owner,
// so every rule of posting is applied again at that moment, and the message
// is an ordinary one.
//
// The loop is safe across restarts. A row that fell due while the server was
// down runs once when it starts, and a repeating one then moves on to its
// next future moment. A crash between posting and recording the run cannot
// post twice: each run posts under a client id made from the row and the
// moment it was due, and chat returns the message already made.
package sched

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/forms"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/store"
)

// Hub is what sched needs of the live hub: telling an item's owner what
// became of it.
type Hub interface {
	Send(account model.AccountID, v any)
}

// Deps is what a Service is built from.
type Deps struct {
	Store store.Store
	Perm  *perm.Checker
	Chat  *chat.Service
	// Forms closes forms whose time has come. It may be nil.
	Forms *forms.Service
	Hub   Hub
	Log   *slog.Logger
	// Around says who is around, for the watches (watch.go). It may be
	// nil, and then no watch fires.
	Around Presence
}

// Service is scheduled items and the loop that runs them.
type Service struct {
	store store.Store
	perm  *perm.Checker
	chat  *chat.Service
	forms *forms.Service
	hub   Hub
	log   *slog.Logger
	now   func() model.Time

	// mu makes firing, sending now, changing and cancelling one after
	// another, so an item is never fired while it is being changed.
	mu sync.Mutex
	// wake tells the loop that the earliest due time may have changed.
	wake chan struct{}
	// later holds rows whose run failed for a reason of the server's own
	// (the database, not a rule): they are tried again after a pause
	// instead of being marked failed.
	later map[string]time.Time
	// swept is when the hourly housekeeping last ran.
	swept time.Time
	// started and done tell Wait whether there is a loop to wait for.
	started atomic.Bool
	done    chan struct{}

	// The watches (watch.go): who is around, the waiting watches by the
	// person watched, and the limit on asking for them.
	around     Presence
	watches    watchIndex
	watchLimit *ratelimit.Limiter
}

// New makes the service. Nothing runs until Run is called.
func New(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Service{store: d.Store, perm: d.Perm, chat: d.Chat, forms: d.Forms, hub: d.Hub, log: d.Log, now: model.Now,
		wake: make(chan struct{}, 1), later: map[string]time.Time{}, done: make(chan struct{}),
		around: d.Around, watchLimit: newWatchLimit()}
}

// NewItem is what Create takes: the contract's ScheduledInput.
type NewItem struct {
	ConversationID model.ConversationID
	Kind           string
	Content        model.MessageInput
	AboutMessageID model.MessageID
	// RunAt is the one run, or the first of a repeat; zero if not given.
	RunAt  model.Time
	Repeat *model.Repeat
}

func (s *Service) tell(account model.AccountID, eventType string, item model.ScheduledItem, extra map[string]any) {
	payload := map[string]any{"scheduled": item}
	for k, v := range extra {
		payload[k] = v
	}
	s.hub.Send(account, model.Event{Type: eventType, At: s.now(), Payload: payload})
}

// Wake makes the loop look at what is due now, for a caller that changed a
// row without going through this service.
func (s *Service) Wake() { s.poke() }

// poke wakes the loop without waiting for it.
func (s *Service) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// checkContent checks what an item of this kind may carry, before the
// content is tried against the rules of posting.
func checkContent(kind string, c model.MessageInput, about model.MessageID, repeat *model.Repeat) error {
	if c.ClientID != "" {
		return model.Invalid("content.client_id", "A scheduled item is made once; it takes no client_id.")
	}
	if utf8.RuneCountInString(c.Text) > model.MaxMessageText {
		return model.ErrTooLarge.With("A message is at most 16,000 characters.")
	}
	switch kind {
	case model.ScheduledMessage:
		if repeat != nil {
			return model.BadRequest("A scheduled message does not repeat. Set a reminder, or a recurring form.")
		}
		if about != "" {
			return model.Invalid("about_message_id", "Only a reminder is about a message.")
		}
	case model.ScheduledReminder:
		if c.Form != nil || len(c.Cards) > 0 || c.ParentID != "" || c.Quote != nil || c.Chance != nil {
			return model.Invalid("content", "A reminder is text, with files if you like. It arrives as a top-level message.")
		}
		if c.Text == "" && len(c.FileIDs) == 0 && about == "" {
			return model.Invalid("content.text", "A reminder says what to remember, unless it is about a message.")
		}
	case model.ScheduledForm:
		if about != "" {
			return model.Invalid("about_message_id", "Only a reminder is about a message.")
		}
		if repeat == nil {
			return model.Invalid("repeat", "A recurring form needs a schedule.")
		}
		f := c.Form
		if f == nil {
			return model.Invalid("content.form", "A recurring form needs its form.")
		}
		if len(c.Cards) > 0 || c.ParentID != "" || c.Quote != nil || len(c.FileIDs) > 0 || c.Chance != nil {
			return model.Invalid("content", "A recurring form is posted as a top-level message with its text and its form.")
		}
		// A check-in: questions answered in that run's thread, one
		// answer per person per run (docs/FEATURES.md section 11).
		if f.Action.Type != model.FormThread || (f.OneAnswer != nil && !*f.OneAnswer) {
			return model.Invalid("content.form.action", "A recurring form uses the thread action, with one answer per person.")
		}
		if len(f.Fields) == 0 {
			return model.Invalid("content.form.fields", "A recurring form has 1 to 20 questions.")
		}
		for _, field := range f.Fields {
			if field.Type != "text" && field.Type != "longtext" {
				return model.Invalid("content.form.fields", "A recurring form's questions are text or long text.")
			}
		}
		if !f.ClosesAt.IsZero() {
			return model.Invalid("content.form.closes_at", "Each run's form closes when the next is posted.")
		}
	default:
		return model.Invalid("kind", "A scheduled item is a message, a reminder or a form.")
	}
	return nil
}

// post turns an item into the message to post. due is the run being made
// (zero when the owner sends it by hand) and nextRun the one after, which is
// when a recurring form's run closes.
func (s *Service) post(ctx context.Context, owner model.Account, item model.ScheduledItem, due, nextRun model.Time, dry bool) (chat.Posted, error) {
	c := item.Content
	in := chat.NewMessage{ConversationID: item.ConversationID, Text: c.Text, FileIDs: c.FileIDs, ParentID: c.ParentID,
		Quote: c.Quote, Cards: c.Cards, Action: c.Action, Chance: c.Chance, DryRun: dry, Scheduled: true,
		Instants: chat.Instants{TimeZone: c.TimeZone, Hints: c.InstantHints, Objects: c.ObjectHints}}
	switch item.Kind {
	case model.ScheduledWatch:
		// A watch arrives as a reminder: to its owner alone, notifying
		// them as a reminder does.
		in.Via = &model.Via{Kind: "reminder"}
		in.Reminder = true
	case model.ScheduledReminder:
		in.Via = &model.Via{Kind: "reminder"}
		in.Reminder = true
		if item.AboutMessageID != "" {
			// The reminder carries the message as its quote, if its
			// owner can still read it. One that has gone, or gone out of
			// reach, is left off rather than failing the reminder.
			m, err := s.chat.Message(ctx, owner, item.AboutMessageID)
			switch {
			case err == nil && !m.Deleted():
				in.Quote = &model.Quote{MessageID: item.AboutMessageID}
			case err != nil && !errors.Is(err, model.ErrNotFound):
				return chat.Posted{}, err
			case in.Text == "" && len(in.FileIDs) == 0:
				return chat.Posted{}, model.ErrNotFound.With("The message this reminder was about is gone.")
			}
		}
	case model.ScheduledForm:
		f := *c.Form
		in.Via = &model.Via{Kind: "scheduled", Name: f.Title}
		// Each run closes when the next is posted.
		f.ClosesAt = nextRun
		in.Form = &f
	default:
		in.Via = &model.Via{Kind: "scheduled"}
		in.Form = c.Form
	}
	if !due.IsZero() && !dry {
		in.ClientID = runID(item.ID, due)
	}
	return s.chat.Post(ctx, owner, in)
}

// runID is the client id one run of one item posts under, so a run that was
// posted but not recorded before a crash is not posted again.
func runID(item string, due model.Time) string {
	sum := sha256.Sum256([]byte("churr scheduled run\x00" + item + "\x00" + due.String()))
	h := hex.EncodeToString(sum[:16])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// firstRun works out when an item first runs, and anchors a weekly or
// monthly rule there.
func (s *Service) firstRun(runAt model.Time, repeat *model.Repeat) (model.Time, error) {
	now := s.now()
	if !runAt.IsZero() && !runAt.After(now) {
		return 0, model.Invalid("run_at", "That time has already passed.")
	}
	if repeat == nil {
		if runAt.IsZero() {
			return 0, model.Invalid("run_at", "Say when: give run_at, or a repeat.")
		}
		return runAt, nil
	}
	if err := checkRepeat(repeat); err != nil {
		return 0, err
	}
	repeat.Anchor = 0
	first := runAt
	if first.IsZero() {
		t, err := next(*repeat, now.Time())
		if err != nil {
			return 0, err
		}
		first = model.FromTime(t)
	}
	anchor(repeat, first.Time())
	return first, nil
}

// Create schedules a message, a reminder or a recurring form. The content is
// checked as if it were posted now.
func (s *Service) Create(ctx context.Context, actor model.Account, in NewItem) (model.ScheduledItem, error) {
	var none model.ScheduledItem
	st, err := s.perm.Load(ctx, actor, in.ConversationID)
	if err != nil {
		return none, err
	}
	if err := checkKind(in.Kind); err != nil {
		return none, err
	}
	if err := perm.CheckSchedule(st, in.Kind); err != nil {
		return none, err
	}
	if err := checkContent(in.Kind, in.Content, in.AboutMessageID, in.Repeat); err != nil {
		return none, err
	}
	item := model.ScheduledItem{AccountID: actor.ID, ConversationID: in.ConversationID, Kind: in.Kind, Content: in.Content,
		AboutMessageID: in.AboutMessageID, CreatedAt: s.now()}
	if in.Repeat != nil {
		r := *in.Repeat
		item.Repeat = &r
	}
	if item.NextRunAt, err = s.firstRun(in.RunAt, item.Repeat); err != nil {
		return none, err
	}
	if err := s.about(ctx, actor, item); err != nil {
		return none, err
	}
	if _, err := s.post(ctx, actor, item, 0, item.NextRunAt.Add(time.Hour), true); err != nil {
		return none, err
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
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
		return none, err
	}
	s.tell(actor.ID, model.EventScheduledCreated, item, nil)
	s.poke()
	return item, nil
}

func checkKind(kind string) error {
	switch kind {
	case model.ScheduledMessage, model.ScheduledReminder, model.ScheduledForm:
		return nil
	}
	return model.Invalid("kind", "A scheduled item is a message, a reminder or a form.")
}

// about checks that the message a reminder is about is one its owner can
// read.
func (s *Service) about(ctx context.Context, owner model.Account, item model.ScheduledItem) error {
	if item.AboutMessageID == "" {
		return nil
	}
	m, err := s.chat.Message(ctx, owner, item.AboutMessageID)
	if errors.Is(err, model.ErrNotFound) || (err == nil && m.Deleted()) {
		return model.Invalid("about_message_id", "There is no such message.")
	}
	return err
}

// manage loads an item for someone who may see and change it: its owner, or
// for a recurring form an account acting as manager of the channel. For
// anyone else, and once it is cancelled, it does not exist.
func (s *Service) manage(ctx context.Context, actor model.Account, id string) (model.ScheduledItem, perm.State, error) {
	item, err := s.store.Scheduled().Get(ctx, id)
	if err != nil {
		return item, perm.State{}, err
	}
	st, err := s.perm.Load(ctx, actor, item.ConversationID)
	if err != nil {
		return item, st, err
	}
	if !item.CancelledAt.IsZero() || !perm.MayManageScheduled(st, item) {
		return item, st, model.ErrNotFound
	}
	return item, st, nil
}

// Get returns one scheduled item.
func (s *Service) Get(ctx context.Context, actor model.Account, id string) (model.ScheduledItem, error) {
	item, _, err := s.manage(ctx, actor, id)
	return item, err
}

// List returns the actor's scheduled items that are not cancelled, newest
// first. With a conversation the actor acts as manager of, that channel's
// recurring forms are listed too, whoever owns them.
func (s *Service) List(ctx context.Context, actor model.Account, kind string, conv model.ConversationID, p store.Page) ([]model.ScheduledItem, string, error) {
	if kind != "" && kind != model.ScheduledWatch {
		if err := checkKind(kind); err != nil {
			return nil, "", err
		}
	}
	f := store.ScheduledFilter{AccountID: actor.ID, ConversationID: conv, Kind: kind}
	if conv != "" {
		if st, err := s.perm.Load(ctx, actor, conv); err == nil && st.CanSee() && st.ActsAsManager() {
			f.AlsoForms = true
		} else if err != nil && !errors.Is(err, model.ErrNotFound) {
			return nil, "", err
		}
	}
	return s.store.Scheduled().List(ctx, f, p)
}

// Change is what Update takes. Nil fields stay as they are.
type Change struct {
	Content *model.MessageInput
	RunAt   *model.Time
	Repeat  *model.Repeat
}

// Update changes a scheduled item before it runs: its content, its time or
// its repeat. Changing a failed item clears the failure. Whoever changes a
// recurring form becomes its owner.
func (s *Service) Update(ctx context.Context, actor model.Account, id string, ch Change) (model.ScheduledItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var none model.ScheduledItem
	item, st, err := s.manage(ctx, actor, id)
	if err != nil {
		return none, err
	}
	if item.Kind == model.ScheduledWatch {
		return none, model.ErrConflict.With("A watch cannot be changed. Stop watching, then watch again.")
	}
	failed := !item.FailedAt.IsZero()
	if item.NextRunAt.IsZero() && !failed {
		return none, model.ErrConflict.With("That item has already run.")
	}
	previous := item.AccountID
	// The one who changes it is the one it will be posted as, and must be
	// allowed to schedule it here now.
	if err := perm.CheckSchedule(st, item.Kind); err != nil {
		return none, err
	}
	item.AccountID = actor.ID
	if ch.Content != nil {
		item.Content = *ch.Content
	}
	var runAt model.Time
	if ch.RunAt != nil {
		runAt = *ch.RunAt
	}
	if ch.Repeat != nil {
		r := *ch.Repeat
		item.Repeat = &r
	}
	if err := checkContent(item.Kind, item.Content, item.AboutMessageID, item.Repeat); err != nil {
		return none, err
	}
	switch {
	case ch.RunAt != nil || ch.Repeat != nil || (failed && item.Repeat != nil):
		// A new time, a new rule, or a failed repeat picking up again.
		if item.NextRunAt, err = s.firstRun(runAt, item.Repeat); err != nil {
			return none, err
		}
	case failed:
		return none, model.Invalid("run_at", "This item failed when it ran. Give it a new time, or send it now.")
	}
	if _, err := s.post(ctx, actor, item, 0, item.NextRunAt.Add(time.Hour), true); err != nil {
		return none, err
	}
	var out model.ScheduledItem
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if failed || previous != actor.ID {
			// It becomes one more waiting item of this account.
			n, err := tx.Scheduled().CountWaiting(ctx, actor.ID)
			if err != nil {
				return err
			}
			if n >= model.MaxWaitingScheduled {
				return model.ErrConflict.With("You have 200 scheduled items waiting, which is the most there can be. Cancel one first.")
			}
		}
		out, err = tx.Scheduled().Update(ctx, id, func(cur *model.ScheduledItem) error {
			if !cur.CancelledAt.IsZero() {
				return model.ErrNotFound
			}
			cur.AccountID, cur.Content, cur.Repeat, cur.NextRunAt = item.AccountID, item.Content, item.Repeat, item.NextRunAt
			cur.FailedAt, cur.Failure, cur.FailureMessage = 0, "", ""
			return nil
		})
		return err
	})
	if err != nil {
		return none, err
	}
	delete(s.later, id)
	s.tell(out.AccountID, model.EventScheduledUpdated, out, nil)
	if previous != out.AccountID {
		// The earlier owner's clients see that it is no longer theirs.
		s.tell(previous, model.EventScheduledUpdated, out, nil)
	}
	s.poke()
	return out, nil
}

// Cancel cancels a scheduled item. It will not run again.
func (s *Service) Cancel(ctx context.Context, actor model.Account, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, _, err := s.manage(ctx, actor, id); err != nil {
		return err
	}
	out, err := s.store.Scheduled().Update(ctx, id, func(cur *model.ScheduledItem) error {
		if !cur.CancelledAt.IsZero() {
			return model.ErrNotFound
		}
		cur.CancelledAt, cur.NextRunAt = s.now(), 0
		return nil
	})
	if err != nil {
		return err
	}
	delete(s.later, id)
	if out.Kind == model.ScheduledWatch {
		s.watches.remove(out.WatchAccountID, id)
	}
	s.tell(out.AccountID, model.EventScheduledCancelled, out, nil)
	return nil
}

// SendNow posts a scheduled item at once, as its owner asks. It is also how
// a failed item is sent by hand. A single item is then done; a repeating one
// keeps its schedule. If posting is refused the refusal is the answer and
// the item stays as it was.
func (s *Service) SendNow(ctx context.Context, actor model.Account, id string) (model.ScheduledItem, model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var none model.ScheduledItem
	item, err := s.store.Scheduled().Get(ctx, id)
	if err != nil {
		return none, model.Message{}, err
	}
	// Only its owner sends it: it is posted under their name.
	if item.AccountID != actor.ID || !item.CancelledAt.IsZero() {
		return none, model.Message{}, model.ErrNotFound
	}
	if item.Kind == model.ScheduledWatch {
		return none, model.Message{}, model.ErrConflict.With("A watch is told when its person is around; it cannot be sent now.")
	}
	failed := !item.FailedAt.IsZero()
	if item.NextRunAt.IsZero() && !failed {
		return none, model.Message{}, model.ErrConflict.With("That item has already run.")
	}
	st, err := s.perm.Load(ctx, actor, item.ConversationID)
	if err != nil {
		return none, model.Message{}, err
	}
	if err := perm.CheckSchedule(st, item.Kind); err != nil {
		return none, model.Message{}, err
	}
	now := s.now()
	nextRun := item.NextRunAt
	if item.Repeat != nil && (nextRun.IsZero() || !nextRun.After(now)) {
		t, err := next(*item.Repeat, now.Time())
		if err != nil {
			return none, model.Message{}, err
		}
		nextRun = model.FromTime(t)
	}
	posted, err := s.post(ctx, actor, item, 0, nextRun, false)
	if err != nil {
		return none, model.Message{}, err
	}
	out, err := s.store.Scheduled().Update(ctx, id, func(cur *model.ScheduledItem) error {
		cur.LastRunAt, cur.FailedAt, cur.Failure, cur.FailureMessage = now, 0, "", ""
		cur.NextRunAt = 0
		if cur.Repeat != nil {
			cur.NextRunAt = nextRun
		}
		return nil
	})
	if err != nil {
		return none, model.Message{}, err
	}
	delete(s.later, id)
	s.tell(actor.ID, model.EventScheduledFired, out, map[string]any{"message_id": posted.Message.ID})
	s.poke()
	return out, posted.Message, nil
}

// retryAfter is how long a row waits when a run failed for a reason of the
// server's own.
const retryAfter = 30 * time.Second

// fire runs one due item (docs/FEATURES.md section 9.4).
func (s *Service) fire(ctx context.Context, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Read again under the lock: it may have been changed, cancelled or
	// sent by hand since it was found due.
	item, err := s.store.Scheduled().Get(ctx, id)
	now := s.now()
	if err != nil || !item.Waiting() || item.NextRunAt.After(now) {
		return
	}
	if item.Kind == model.ScheduledWatch {
		// A watch's next run is when it expires unfired.
		s.expireWatch(ctx, item)
		return
	}
	due := item.NextRunAt
	var nextRun model.Time
	if item.Repeat != nil {
		// The next future moment: runs missed while the server was down
		// are not made up one by one.
		if t, err := next(*item.Repeat, now.Time()); err == nil {
			nextRun = model.FromTime(t)
		} else {
			s.log.Error("a repeat rule gives no next run", "scheduled", id, "err", err)
		}
	}

	var posted chat.Posted
	owner, err := s.store.Accounts().Get(ctx, item.AccountID)
	if err == nil {
		var st perm.State
		if st, err = s.perm.Load(ctx, owner, item.ConversationID); err == nil {
			// For a recurring form this is where an owner who is no
			// longer a manager is found out.
			err = perm.CheckSchedule(st, item.Kind)
		}
	}
	if err == nil {
		posted, err = s.post(ctx, owner, item, due, nextRun, false)
	}
	if err != nil {
		refusal, ours := model.AsError(err)
		if !ours || refusal.Code == model.CodeInternal || refusal.Code == model.CodeUnavailable {
			// The server's own trouble, not a rule: try again shortly
			// and leave the row waiting.
			s.log.Error("a scheduled item could not be run; it will be tried again", "scheduled", id, "err", err)
			s.later[id] = time.Now().Add(retryAfter)
			return
		}
		out, uerr := s.store.Scheduled().Update(ctx, id, func(cur *model.ScheduledItem) error {
			// Failed: it does not run again, repeating or not, until
			// its owner changes it. The content stays.
			cur.FailedAt, cur.Failure, cur.FailureMessage, cur.NextRunAt = now, string(refusal.Code), refusal.Message, 0
			return nil
		})
		if uerr != nil {
			s.log.Error("a failed run could not be recorded", "scheduled", id, "err", uerr)
			s.later[id] = time.Now().Add(retryAfter)
			return
		}
		s.tell(item.AccountID, model.EventScheduledFired, out, map[string]any{"error": refusal})
		return
	}
	out, err := s.store.Scheduled().Update(ctx, id, func(cur *model.ScheduledItem) error {
		cur.LastRunAt, cur.NextRunAt = now, nextRun
		return nil
	})
	if err != nil {
		// The message is posted. The row is still due, and the next try
		// finds the message under the same client id and only records it.
		s.log.Error("a run could not be recorded", "scheduled", id, "err", err)
		s.later[id] = time.Now().Add(retryAfter)
		return
	}
	s.tell(item.AccountID, model.EventScheduledFired, out, map[string]any{"message_id": posted.Message.ID})
}

// The loop looks again at least this often, which is also the longest a form
// posted with a closing time waits to be closed after it; and it never spins
// faster than the shortest wait.
const (
	longestWait  = 15 * time.Second
	shortestWait = 50 * time.Millisecond
	sweepEvery   = time.Hour
	// keepFinished is how long a done or cancelled row is kept
	// (docs/SCHEMA.md section 8).
	keepFinished = 30 * 24 * time.Hour
)

// tick does everything that is due now.
func (s *Service) tick(ctx context.Context) {
	if s.forms != nil {
		if _, err := s.forms.CloseDue(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("forms could not be closed", "err", err)
		}
	}
	for ctx.Err() == nil {
		due, err := s.store.Scheduled().Due(ctx, s.now(), 100)
		if err != nil {
			if ctx.Err() == nil {
				s.log.Error("due scheduled items could not be read", "err", err)
			}
			return
		}
		ran := 0
		for _, item := range due {
			s.mu.Lock()
			until, waiting := s.later[item.ID]
			if waiting && !time.Now().Before(until) {
				delete(s.later, item.ID)
				waiting = false
			}
			s.mu.Unlock()
			if waiting || ctx.Err() != nil {
				continue
			}
			s.fire(ctx, item.ID)
			ran++
		}
		if ran == 0 || len(due) < 100 {
			break
		}
	}
	if time.Since(s.swept) >= sweepEvery && ctx.Err() == nil {
		s.swept = time.Now()
		if s.forms != nil {
			if _, err := s.forms.Sweep(ctx); err != nil {
				s.log.Warn("old dialogs could not be removed", "err", err)
			}
		}
		if _, err := s.store.Scheduled().DeleteFinishedBefore(ctx, s.now().Add(-keepFinished)); err != nil {
			s.log.Warn("finished scheduled items could not be removed", "err", err)
		}
	}
}

// wait says how long the loop may sleep: until the earliest thing due.
func (s *Service) wait(ctx context.Context) time.Duration {
	d := longestWait
	consider := func(at model.Time, found bool, err error) {
		if err != nil || !found {
			return
		}
		if until := at.Sub(s.now()); until < d {
			d = until
		}
	}
	consider(s.store.Scheduled().NextRun(ctx))
	if s.forms != nil {
		consider(s.forms.NextClose(ctx))
	}
	s.mu.Lock()
	held := len(s.later) > 0
	s.mu.Unlock()
	// A row held back for a retry is still the earliest due; without
	// this the loop would spin on it.
	if held && d < time.Second {
		d = time.Second
	}
	if d < shortestWait {
		d = shortestWait
	}
	return d
}

// Run fires due items until ctx ends. It runs what fell due while the server
// was down as it starts. Call it once, in its own goroutine; Wait returns
// when it has stopped.
func (s *Service) Run(ctx context.Context) {
	if !s.started.CompareAndSwap(false, true) {
		return
	}
	defer close(s.done)
	for {
		s.tick(ctx)
		if ctx.Err() != nil {
			return
		}
		timer := time.NewTimer(s.wait(ctx))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// Wait returns once Run has stopped, so the database is not closed under a
// run that is being made.
func (s *Service) Wait() {
	if s.started.Load() {
		<-s.done
	}
}
