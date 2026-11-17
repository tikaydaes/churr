// Package mod is moderation: the checks made when someone posts, after perm
// has said yes (slow mode and the word filter), and the moderator's tools
// (time-outs, reports, the filter list). docs/FEATURES.md sections 15 to 18
// say what each does; who may do it is perm's.
//
// mod sits below chat. chat calls it through chat.Moderator on the post
// path, and mod reaches back only through the small Deleter interface, for
// the one report outcome that deletes a message.
package mod

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Hub is what mod needs of the live hub: telling single accounts. None of
// mod's events belongs to a conversation's log.
type Hub interface {
	Send(account model.AccountID, v any)
}

// Deleter deletes a message as a moderator; chat.Service implements it.
type Deleter interface {
	Delete(ctx context.Context, actor model.Account, id model.MessageID) (model.Message, error)
	Purge(ctx context.Context, actor model.Account, id model.MessageID) (string, model.Message, error)
}

// Deps is what a Service is built from.
type Deps struct {
	Store store.Store
	Perm  *perm.Checker
	Hub   Hub
	Log   *slog.Logger
}

// Service is the moderation service.
type Service struct {
	store   store.Store
	perm    *perm.Checker
	hub     Hub
	log     *slog.Logger
	now     func() model.Time
	deleter Deleter

	// compiled keeps each filter's pattern compiled, keyed by the pattern
	// itself, so a post costs one small query and no compiling.
	mu       sync.Mutex
	compiled map[string]*regexp.Regexp
}

// New makes the service.
func New(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Service{store: d.Store, perm: d.Perm, hub: d.Hub, log: d.Log, now: model.Now, compiled: map[string]*regexp.Regexp{}}
}

// SetDeleter hands mod the chat service, which is built after it.
func (s *Service) SetDeleter(d Deleter) { s.deleter = d }

// ------------------------------------------------------------ the post path

// SlowModeExempt reports whether the actor of st posts without waiting for
// slow mode: the channel's moderators and managers, bots and webhooks
// (docs/PERMISSIONS.md section 7).
func SlowModeExempt(st perm.State) bool {
	return st.KeepsPeace() || st.Actor.Role == model.RoleBot
}

// Check is made before a new message is stored: slow mode, then the word
// filter. It answers nil, slow_mode with the seconds to wait, or
// blocked_by_filter.
func (s *Service) Check(ctx context.Context, st perm.State, text string) error {
	if wait := s.slowModeWait(st); wait > 0 {
		e := *model.ErrSlowMode
		e.RetryAfterSeconds = wait
		return &e
	}
	return s.CheckEdit(ctx, st, text)
}

// slowModeWait is how many seconds the actor must still wait, or 0.
func (s *Service) slowModeWait(st perm.State) int {
	interval := st.Conversation.SlowModeSeconds
	if interval <= 0 || st.Member == nil || st.Member.LastPostAt.IsZero() || SlowModeExempt(st) {
		return 0
	}
	left := time.Duration(interval)*time.Second - s.now().Sub(st.Member.LastPostAt)
	if left <= 0 {
		return 0
	}
	return int((left + time.Second - 1) / time.Second)
}

// CheckEdit is made before an edit is stored: the word filter only, since
// slow mode does not apply to edits.
func (s *Service) CheckEdit(ctx context.Context, st perm.State, text string) error {
	verdict, _, err := s.match(ctx, s.store, st, text)
	if err != nil {
		return err
	}
	if verdict == model.FilterBlock {
		// The sentence does not quote the pattern, and nothing is logged.
		return model.ErrBlockedByFilter.With("That message has a word that is not allowed here.")
	}
	return nil
}

// Stored is called inside the transaction that stored a new message or an
// edit. For a new message it starts the slow-mode clock. If the text
// matches a `flag` filter it files the server's own report on the message.
// The function it returns, if any, is run after the commit and tells the
// handlers.
func (s *Service) Stored(ctx context.Context, tx store.Tx, st perm.State, m model.Message, edit bool) (func(), error) {
	if !edit && st.Member != nil && st.Conversation.SlowModeSeconds > 0 {
		_, err := tx.Members().UpdateOwn(ctx, m.ConversationID, st.Actor.ID, func(mem *model.Member) error {
			mem.LastPostAt = m.CreatedAt
			return nil
		})
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return nil, err
		}
	}
	verdict, filter, err := s.match(ctx, tx, st, m.Text)
	if err != nil || verdict == "" {
		return nil, err
	}
	// A `block` match only gets here for a message that could not be
	// refused (one from another server); it is then a flag.
	r := model.Report{MessageID: m.ID, ConversationID: m.ConversationID, Source: model.ReportByFilter, Reason: filter, CreatedAt: s.now()}
	created, err := tx.Moderation().CreateReport(ctx, &r)
	if err != nil || !created {
		return nil, err
	}
	to, err := s.handlers(ctx, tx, st.Conversation, m.AuthorID)
	if err != nil {
		return nil, err
	}
	return func() { s.tell(to, model.EventReportCreated, map[string]any{"report": r}) }, nil
}

// match runs the filter list over a text. verdict is "", `flag` or `block`
// (block wins), and filter the identifier of the first entry that gave it.
// Sealed chats are never checked: the server cannot read them.
func (s *Service) match(ctx context.Context, a store.Areas, st perm.State, text string) (verdict, filter string, err error) {
	if text == "" || st.Conversation.Sealed() {
		return "", "", nil
	}
	list, err := a.Moderation().WordFilters(ctx)
	if err != nil {
		return "", "", err
	}
	for _, f := range list {
		re, err := s.compile(f)
		if err != nil || !re.MatchString(text) {
			continue // an entry that no longer compiles matches nothing
		}
		if f.Action == model.FilterBlock {
			if st.Actor.Role == model.RoleRemote {
				// Already part of the shared channel: it cannot be
				// refused, so it is flagged.
				return model.FilterFlag, f.ID, nil
			}
			return model.FilterBlock, f.ID, nil
		}
		if verdict == "" {
			verdict, filter = model.FilterFlag, f.ID
		}
	}
	return verdict, filter, nil
}

// pattern turns a filter entry into the expression that is run. A plain
// word or phrase matches without regard to case and only at word
// boundaries, so `ass` does not match `class`; a regular expression is
// RE2, which cannot be made to run for unbounded time, without regard to
// case.
func pattern(f model.WordFilter) string {
	if f.IsRegex {
		return `(?i)` + f.Pattern
	}
	const edge = `[^\p{L}\p{N}_]`
	return `(?i)(?:^|` + edge + `)` + regexp.QuoteMeta(f.Pattern) + `(?:$|` + edge + `)`
}

func (s *Service) compile(f model.WordFilter) (*regexp.Regexp, error) {
	p := pattern(f)
	s.mu.Lock()
	defer s.mu.Unlock()
	if re, ok := s.compiled[p]; ok {
		return re, nil
	}
	re, err := regexp.Compile(p)
	if err != nil {
		return nil, err
	}
	if len(s.compiled) > 4096 {
		// Entries that were deleted long ago; start again.
		s.compiled = map[string]*regexp.Regexp{}
	}
	s.compiled[p] = re
	return re, nil
}

// ---------------------------------------------------------------- who hears

// tell sends one event, which belongs to no conversation's log, to a list
// of accounts.
func (s *Service) tell(to []model.AccountID, eventType string, payload map[string]any) {
	e := model.Event{Type: eventType, At: s.now(), Payload: payload}
	seen := map[model.AccountID]bool{}
	for _, id := range to {
		if id != "" && !seen[id] {
			seen[id] = true
			s.hub.Send(id, e)
		}
	}
}

// admins returns the owner and the admins.
func admins(ctx context.Context, a store.Areas) ([]model.AccountID, error) {
	g, err := a.Groups().Builtin(ctx, model.GroupAdmins)
	if err != nil {
		return nil, err
	}
	return a.Groups().MemberIDs(ctx, g.ID)
}

// Admins returns the owner and the admins: who is told about what only
// admins handle.
func Admins(ctx context.Context, a store.Areas) ([]model.AccountID, error) { return admins(ctx, a) }

// handlers returns who may handle a report in a conversation
// (docs/FEATURES.md section 18): its moderators and managers, and in a
// public channel every admin too, for what is the instance's to do; in a
// private or ad-hoc channel nobody else. A report about a handler's own message is kept
// from that handler if another handler exists.
func (s *Service) handlers(ctx context.Context, a store.Areas, conv model.Conversation, author model.AccountID) ([]model.AccountID, error) {
	ids, err := a.Members().Keepers(ctx, conv.ID)
	if err != nil {
		return nil, err
	}
	if conv.Public() {
		more, err := admins(ctx, a)
		if err != nil {
			return nil, err
		}
		ids = append(ids, more...)
	}
	seen := map[model.AccountID]bool{}
	var out []model.AccountID
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if len(out) > 1 && seen[author] {
		kept := out[:0]
		for _, id := range out {
			if id != author {
				kept = append(kept, id)
			}
		}
		out = kept
	}
	return out, nil
}

// audit writes an audit entry for something done in a conversation, unless
// it is an ad-hoc channel: nothing done inside one reaches the log admins
// read.
func audit(ctx context.Context, a store.Areas, conv *model.Conversation, e model.AuditEntry) error {
	if conv != nil && conv.Adhoc() {
		return nil
	}
	if e.ActorKind == "" {
		e.ActorKind = "account"
	}
	return a.Audit().Append(ctx, e)
}

// hide turns "no such row" into the one answer an outsider gets.
func hide(err error) error {
	if errors.Is(err, model.ErrNotFound) {
		return model.ErrNotFound
	}
	return err
}

// ---------------------------------------------------------------- time-outs

func checkSeconds(seconds int64) error {
	if seconds < model.MinTimeoutSeconds || seconds > model.MaxTimeoutSeconds {
		return model.Invalid("seconds", "A time-out lasts from 60 seconds to 28 days.")
	}
	return nil
}

func checkReason(reason string) error {
	if utf8.RuneCountInString(reason) > model.MaxReportReason {
		return model.Invalid("reason", "A reason is at most 1,000 characters.")
	}
	return nil
}

// TimeOut stops an account posting and reacting in one channel for a set
// time. A new time-out replaces the one in force.
func (s *Service) TimeOut(ctx context.Context, actor model.Account, conv model.ConversationID, target model.AccountID, seconds int64, reason string) (model.Timeout, error) {
	st, err := s.perm.Load(ctx, actor, conv)
	if err != nil {
		return model.Timeout{}, hide(err)
	}
	if !st.CanSee() {
		return model.Timeout{}, model.ErrNotFound
	}
	if err := perm.Check(st, perm.TimeOutMember); err != nil {
		return model.Timeout{}, err
	}
	if err := checkSeconds(seconds); err != nil {
		return model.Timeout{}, err
	}
	if err := checkReason(reason); err != nil {
		return model.Timeout{}, err
	}
	who, err := s.store.Accounts().Get(ctx, target)
	if err != nil || s.perm.CanSeeAccount(ctx, actor, who) != nil {
		return model.Timeout{}, model.ErrNotFound.With("There is no such account.")
	}
	row, _, err := s.store.Members().Get(ctx, conv, target)
	if err != nil {
		return model.Timeout{}, err
	}
	if err := perm.CheckTimeOut(st, who, row.Level); err != nil {
		return model.Timeout{}, err
	}
	now := s.now()
	t := model.Timeout{AccountID: target, ConversationID: conv, Until: now.Add(time.Duration(seconds) * time.Second),
		Reason: reason, CreatedByID: actor.ID, CreatedAt: now}
	var lifted []model.Timeout
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		var err error
		if lifted, err = tx.Moderation().ReplaceTimeout(ctx, &t); err != nil {
			return err
		}
		return audit(ctx, tx, &st.Conversation, model.AuditEntry{At: now, ActorID: string(actor.ID), Action: "timeout.started",
			TargetKind: "account", TargetID: string(target),
			Detail: map[string]any{"conversation_id": conv, "seconds": seconds, "timeout_id": t.ID}})
	})
	if err != nil {
		return model.Timeout{}, err
	}
	s.timeoutEvents(ctx, lifted, t)
	return t, nil
}

// timeoutEvents tells the account and those who can manage time-outs in the
// scope: the channel's moderators and managers, or the admins for a
// server-wide one. The
// rest of the channel is not told.
func (s *Service) timeoutEvents(ctx context.Context, lifted []model.Timeout, started model.Timeout) {
	to := func(t model.Timeout) []model.AccountID {
		var ids []model.AccountID
		var err error
		if t.ConversationID == "" {
			ids, err = admins(ctx, s.store)
		} else {
			ids, err = s.store.Members().Keepers(ctx, t.ConversationID)
		}
		if err != nil {
			s.log.Warn("could not work out who to tell about a time-out", "err", err)
		}
		return append(ids, t.AccountID)
	}
	for _, t := range lifted {
		s.tell(to(t), model.EventTimeoutLifted, map[string]any{"timeout": t})
	}
	if started.ID != "" {
		s.tell(to(started), model.EventTimeoutStarted, map[string]any{"timeout": started})
	}
}

// ChannelTimeouts lists the time-outs in force in a channel, for those who
// could set one.
func (s *Service) ChannelTimeouts(ctx context.Context, actor model.Account, conv model.ConversationID) ([]model.Timeout, error) {
	if _, err := s.perm.Can(ctx, actor, perm.TimeOutMember, conv); err != nil {
		return nil, hide(err)
	}
	list, _, err := s.store.Moderation().Timeouts(ctx, store.TimeoutFilter{ConversationID: conv}, s.now(), store.Page{Limit: model.MaxLimit})
	return list, err
}

// lift ends a time-out the caller has already been found able to lift.
func (s *Service) lift(ctx context.Context, actor model.Account, conv *model.Conversation, t model.Timeout) error {
	now := s.now()
	if !t.LiftedAt.IsZero() || !t.Until.After(now) {
		return nil // already over; lifting it again changes nothing
	}
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Moderation().LiftTimeout(ctx, t.ID, now); err != nil {
			return err
		}
		return audit(ctx, tx, conv, model.AuditEntry{At: now, ActorID: string(actor.ID), Action: "timeout.lifted",
			TargetKind: "account", TargetID: string(t.AccountID), Detail: map[string]any{"conversation_id": t.ConversationID, "timeout_id": t.ID}})
	})
	if err != nil {
		return err
	}
	t.LiftedAt = now
	s.timeoutEvents(ctx, []model.Timeout{t}, model.Timeout{})
	return nil
}

// LiftChannelTimeout ends a channel's time-out early. Anyone who could have
// set it may.
func (s *Service) LiftChannelTimeout(ctx context.Context, actor model.Account, conv model.ConversationID, id string) error {
	st, err := s.perm.Can(ctx, actor, perm.TimeOutMember, conv)
	if err != nil {
		return hide(err)
	}
	t, err := s.store.Moderation().GetTimeout(ctx, id)
	if err != nil || t.ConversationID != conv {
		return model.ErrNotFound
	}
	if who, err := s.store.Accounts().Get(ctx, t.AccountID); err == nil {
		row, _, err := s.store.Members().Get(ctx, conv, t.AccountID)
		if err != nil {
			return err
		}
		if err := perm.CheckTimeOut(st, who, row.Level); err != nil {
			return err
		}
	}
	return s.lift(ctx, actor, &st.Conversation, t)
}

// TimeOutEverywhere times an account out on the whole server.
func (s *Service) TimeOutEverywhere(ctx context.Context, actor model.Account, target model.AccountID, seconds int64, reason string) (model.Timeout, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.UseAdminConsole); err != nil {
		return model.Timeout{}, err
	}
	if err := checkSeconds(seconds); err != nil {
		return model.Timeout{}, err
	}
	if err := checkReason(reason); err != nil {
		return model.Timeout{}, err
	}
	who, err := s.store.Accounts().Get(ctx, target)
	if err != nil {
		return model.Timeout{}, hide(err)
	}
	if err := s.perm.CanTimeOutEverywhere(actor, who); err != nil {
		return model.Timeout{}, err
	}
	now := s.now()
	t := model.Timeout{AccountID: target, Until: now.Add(time.Duration(seconds) * time.Second), Reason: reason, CreatedByID: actor.ID, CreatedAt: now}
	var lifted []model.Timeout
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		var err error
		if lifted, err = tx.Moderation().ReplaceTimeout(ctx, &t); err != nil {
			return err
		}
		return audit(ctx, tx, nil, model.AuditEntry{At: now, ActorID: string(actor.ID), Action: "timeout.started",
			TargetKind: "account", TargetID: string(target), Detail: map[string]any{"seconds": seconds, "timeout_id": t.ID}})
	})
	if err != nil {
		return model.Timeout{}, err
	}
	s.timeoutEvents(ctx, lifted, t)
	return t, nil
}

// AdminTimeouts lists what the admin console shows: server-wide time-outs
// and those in public channels.
func (s *Service) AdminTimeouts(ctx context.Context, actor model.Account, p store.Page) ([]model.Timeout, string, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.UseAdminConsole); err != nil {
		return nil, "", err
	}
	return s.store.Moderation().Timeouts(ctx, store.TimeoutFilter{AdminView: true}, s.now(), p)
}

// AdminLiftTimeout lifts a time-out from the admin console: a server-wide
// one, or one in a channel where the admin acts as manager. A time-out in a
// private channel the admin is not in does not exist for them.
func (s *Service) AdminLiftTimeout(ctx context.Context, actor model.Account, id string) error {
	if err := s.perm.CanAdminister(ctx, actor, perm.UseAdminConsole); err != nil {
		return err
	}
	t, err := s.store.Moderation().GetTimeout(ctx, id)
	if err != nil {
		return hide(err)
	}
	if t.ConversationID != "" {
		return s.LiftChannelTimeout(ctx, actor, t.ConversationID, id)
	}
	if who, err := s.store.Accounts().Get(ctx, t.AccountID); err == nil {
		if err := s.perm.CanTimeOutEverywhere(actor, who); err != nil {
			return err
		}
	}
	return s.lift(ctx, actor, nil, t)
}

// ------------------------------------------------------------------ reports

// Report files a report of a message by someone who can read it. A second
// report by the same person while the first waits is refused with
// already_exists.
func (s *Service) Report(ctx context.Context, actor model.Account, id model.MessageID, reason string) (model.Report, error) {
	m, err := s.store.Messages().Get(ctx, id)
	if err != nil {
		return model.Report{}, hide(err)
	}
	st, err := s.perm.Load(ctx, actor, m.ConversationID)
	if err != nil {
		return model.Report{}, hide(err)
	}
	// A message the reader may not know exists is not found, whatever
	// else is wrong (case 123).
	if err := perm.CheckMessage(st, perm.ReadMessage, m); err != nil {
		return model.Report{}, err
	}
	if err := perm.Check(st, perm.Report); err != nil {
		return model.Report{}, err
	}
	if err := checkReason(reason); err != nil {
		return model.Report{}, err
	}
	r := model.Report{MessageID: id, ConversationID: m.ConversationID, ReportedByID: actor.ID, Reason: reason, CreatedAt: s.now()}
	created, err := s.store.Moderation().CreateReport(ctx, &r)
	if err != nil {
		return model.Report{}, err
	}
	if !created {
		return r, model.ErrAlreadyExists.With("You have already reported this message.")
	}
	to, err := s.handlers(ctx, s.store, st.Conversation, m.AuthorID)
	if err != nil {
		return r, err
	}
	// The author is never told who reported; handlers are.
	s.tell(to, model.EventReportCreated, map[string]any{"report": r})
	return r, nil
}

// Reports lists the reports the actor may handle, each with its message as
// the actor sees it.
func (s *Service) Reports(ctx context.Context, actor model.Account, conv model.ConversationID, waiting bool, p store.Page) ([]model.Report, string, error) {
	list, next, err := s.store.Moderation().Reports(ctx,
		store.ReportFilter{Scope: perm.ReportScope(actor), ConversationID: conv, Waiting: waiting}, p)
	if err != nil {
		return nil, "", err
	}
	out := list[:0]
	others := map[model.ConversationID]bool{} // whether a conversation has a handler besides the actor
	for _, r := range list {
		m, err := s.store.Messages().Get(ctx, r.MessageID)
		if err != nil {
			continue // the message went between the two reads
		}
		if m.AuthorID == actor.ID {
			// A report about a handler's own message is left out if
			// another handler exists.
			other, known := others[r.ConversationID]
			if !known {
				c, err := s.store.Conversations().Get(ctx, r.ConversationID)
				if err != nil {
					continue
				}
				hs, err := s.handlers(ctx, s.store, c, "")
				if err != nil {
					return nil, "", err
				}
				other = len(hs) > 1
				others[r.ConversationID] = other
			}
			if other {
				continue
			}
		}
		msgs := []model.Message{m}
		if err := s.store.Messages().Hydrate(ctx, msgs, actor.ID); err != nil {
			return nil, "", err
		}
		msgs[0].ClientID = ""
		r.Message = &msgs[0]
		out = append(out, r)
	}
	return out, next, nil
}

// HandleReport records a handler's decision and carries it out: nothing
// more, deleting the message (for an admin who is no moderator there,
// purging it), or timing its author out in that channel for the seconds
// given. It settles every waiting report on the message.
func (s *Service) HandleReport(ctx context.Context, actor model.Account, id, outcome string, timeoutSeconds int64) (model.Report, error) {
	r, err := s.store.Moderation().GetReport(ctx, id)
	if err != nil {
		return r, hide(err)
	}
	st, err := s.perm.Load(ctx, actor, r.ConversationID)
	if err != nil {
		return r, hide(err)
	}
	// A report exists only for those who may handle it.
	if !st.CanSee() || perm.Check(st, perm.HandleReports) != nil {
		if st.CanSee() && model.CodeOf(perm.Check(st, perm.HandleReports)) == model.CodeTimedOut {
			return r, model.ErrTimedOut
		}
		return r, model.ErrNotFound
	}
	if !r.Waiting() {
		return r, model.ErrConflict.With("That report has already been handled.")
	}
	m, err := s.store.Messages().Get(ctx, r.MessageID)
	if err != nil {
		return r, hide(err)
	}
	// Each outcome needs the handler to hold that power anyway.
	switch outcome {
	case model.ReportDismissed:
	case model.ReportDeleted:
		if !m.Deleted() {
			if s.deleter == nil {
				return r, model.ErrUnavailable
			}
			if perm.CheckMessage(st, perm.DeleteMessage, m) != nil && perm.CheckMessage(st, perm.PurgeDirectly, m) == nil {
				// An admin handling a public channel's report is no
				// manager there (ruling R99): what is the instance's to
				// do is to purge, which deletes too.
				if _, _, err := s.deleter.Purge(ctx, actor, m.ID); err != nil && !errors.Is(err, model.ErrConflict) {
					return r, err
				}
			} else if _, err := s.deleter.Delete(ctx, actor, m.ID); err != nil && !errors.Is(err, model.ErrConflict) {
				return r, err
			}
		}
	case model.ReportTimedOut:
		if timeoutSeconds == 0 {
			return r, model.Invalid("timeout_seconds", "Say for how long to time the author out.")
		}
		if _, err := s.TimeOut(ctx, actor, r.ConversationID, m.AuthorID, timeoutSeconds, ""); err != nil {
			if e, ok := model.AsError(err); ok && e.Field == "seconds" {
				return r, model.Invalid("timeout_seconds", e.Message)
			}
			return r, err
		}
	default:
		return r, model.Invalid("outcome", "An outcome is dismissed, deleted or timed_out.")
	}
	now := s.now()
	var settled []model.Report
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		var err error
		if settled, err = tx.Moderation().SettleReports(ctx, r.MessageID, outcome, actor.ID, now); err != nil {
			return err
		}
		// Without the message text, and not at all for an ad-hoc channel.
		return audit(ctx, tx, &st.Conversation, model.AuditEntry{At: now, ActorID: string(actor.ID), Action: "report.handled",
			TargetKind: "message", TargetID: string(r.MessageID),
			Detail: map[string]any{"conversation_id": r.ConversationID, "outcome": outcome, "reports": len(settled)}})
	})
	if err != nil {
		return r, err
	}
	to, err := s.handlers(ctx, s.store, st.Conversation, m.AuthorID)
	if err != nil {
		return r, err
	}
	to = append(to, actor.ID)
	for _, one := range settled {
		// The reporter is not told the outcome; only handlers are.
		s.tell(to, model.EventReportHandled, map[string]any{"report": one})
		if one.ID == r.ID {
			r = one
		}
	}
	if r.Waiting() {
		// Someone else settled it between the read and the write.
		if r, err = s.store.Moderation().GetReport(ctx, id); err != nil {
			return r, err
		}
	}
	return r, nil
}

// -------------------------------------------------------------- word filter

// checkFilter validates an entry before it is saved. An expression that
// does not compile is refused here.
func checkFilter(f model.WordFilter) error {
	if f.Pattern == "" || utf8.RuneCountInString(f.Pattern) > 500 {
		return model.Invalid("pattern", "A pattern is 1 to 500 characters.")
	}
	if f.Action != model.FilterBlock && f.Action != model.FilterFlag {
		return model.Invalid("action", "An action is block or flag.")
	}
	if _, err := regexp.Compile(pattern(f)); err != nil {
		return model.Invalid("pattern", "That is not a regular expression this server can run (RE2 syntax).")
	}
	return nil
}

// Filters returns the admins' word-filter list.
func (s *Service) Filters(ctx context.Context, actor model.Account) ([]model.WordFilter, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.ManageFilters); err != nil {
		return nil, err
	}
	return s.store.Moderation().WordFilters(ctx)
}

// CreateFilter adds an entry to the list.
func (s *Service) CreateFilter(ctx context.Context, actor model.Account, f model.WordFilter) (model.WordFilter, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.ManageFilters); err != nil {
		return f, err
	}
	if err := checkFilter(f); err != nil {
		return f, err
	}
	f.ID, f.CreatedByID, f.CreatedAt = "", actor.ID, s.now()
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Moderation().CreateWordFilter(ctx, &f); err != nil {
			return err
		}
		// Changing the list is in the audit log; the pattern itself is
		// not, and neither are matches.
		return tx.Audit().Append(ctx, model.AuditEntry{At: f.CreatedAt, ActorID: string(actor.ID), ActorKind: "account",
			Action: "word_filter.created", TargetKind: "word_filter", TargetID: f.ID, Detail: map[string]any{"action": f.Action, "is_regex": f.IsRegex}})
	})
	return f, err
}

// FilterChange is what UpdateFilter takes; nil fields stay as they are.
type FilterChange struct {
	Pattern *string
	IsRegex *bool
	Action  *string
}

// UpdateFilter changes an entry.
func (s *Service) UpdateFilter(ctx context.Context, actor model.Account, id string, ch FilterChange) (model.WordFilter, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.ManageFilters); err != nil {
		return model.WordFilter{}, err
	}
	f, err := s.store.Moderation().GetWordFilter(ctx, id)
	if err != nil {
		return f, hide(err)
	}
	if ch.Pattern != nil {
		f.Pattern = *ch.Pattern
	}
	if ch.IsRegex != nil {
		f.IsRegex = *ch.IsRegex
	}
	if ch.Action != nil {
		f.Action = *ch.Action
	}
	if err := checkFilter(f); err != nil {
		return f, err
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Moderation().UpdateWordFilter(ctx, f); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{ActorID: string(actor.ID), ActorKind: "account",
			Action: "word_filter.updated", TargetKind: "word_filter", TargetID: f.ID, Detail: map[string]any{"action": f.Action, "is_regex": f.IsRegex}})
	})
	return f, hide(err)
}

// DeleteFilter removes an entry.
func (s *Service) DeleteFilter(ctx context.Context, actor model.Account, id string) error {
	if err := s.perm.CanAdminister(ctx, actor, perm.ManageFilters); err != nil {
		return err
	}
	return hide(s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Moderation().DeleteWordFilter(ctx, id); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{ActorID: string(actor.ID), ActorKind: "account",
			Action: "word_filter.deleted", TargetKind: "word_filter", TargetID: id})
	}))
}
