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
	"strings"
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
	// Tidy settles a conversation once the last report about it is
	// handled: one nobody is in any more goes then.
	Tidy(ctx context.Context, id model.ConversationID) error
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
	to, toAdmins, err := s.route(ctx, tx, st.Conversation, m.AuthorID)
	if err != nil {
		return nil, err
	}
	r := model.Report{MessageID: m.ID, ConversationID: m.ConversationID, AccountID: m.AuthorID, MessageAt: m.CreatedAt,
		Snapshot: snapshotOf(st.Conversation, m), ToAdmins: toAdmins, Source: model.ReportByFilter, Reason: filter, CreatedAt: s.now()}
	created, err := tx.Moderation().CreateReport(ctx, &r)
	if err != nil || !created {
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

// route returns who is told of a report of a message by author in a
// conversation, and whether it goes to the instance's admins
// (docs/PERMISSIONS.md section 8.4):
//
//   - a public channel: its moderators and managers, and every admin, for
//     what is the instance's to do;
//   - a named private channel: its moderators and managers; and the admins
//     too when the author is one of those or it has none, for a report
//     must reach someone who can act;
//   - a direct or group chat: the admins, since a chat has no moderator.
//
// The author is never told of a report about themselves. Admins sent a
// report from a private channel or a chat are shown its snapshot and
// nothing else of the conversation.
func (s *Service) route(ctx context.Context, a store.Areas, conv model.Conversation, author model.AccountID) ([]model.AccountID, bool, error) {
	var ids []model.AccountID
	toAdmins := false
	if conv.Named() {
		keepers, err := a.Members().Keepers(ctx, conv.ID)
		if err != nil {
			return nil, false, err
		}
		ids = keepers
		if !conv.Public() {
			others := 0
			for _, id := range keepers {
				if id == author {
					toAdmins = true
				} else {
					others++
				}
			}
			toAdmins = toAdmins || others == 0
		}
	} else {
		toAdmins = true
	}
	if conv.Public() || toAdmins {
		more, err := admins(ctx, a)
		if err != nil {
			return nil, false, err
		}
		ids = append(ids, more...)
	}
	return without(ids, author), toAdmins, nil
}

// without returns ids once each, leaving out one account.
func without(ids []model.AccountID, leave model.AccountID) []model.AccountID {
	seen := map[model.AccountID]bool{leave: true}
	var out []model.AccountID
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// snapshotOf is what a report of m keeps, whatever later happens to m: where
// it was, its text, author and times, and its files' names (never the files).
func snapshotOf(conv model.Conversation, m model.Message) *model.ReportSnapshot {
	place := model.PlaceChat
	switch {
	case conv.Public():
		place = model.PlacePublic
	case conv.Named():
		place = model.PlacePrivate
	}
	out := &model.ReportSnapshot{Place: place, AuthorID: m.AuthorID, Text: m.Text, CreatedAt: m.CreatedAt, EditedAt: m.EditedAt}
	for _, f := range m.Files {
		out.FileNames = append(out.FileNames, f.Name)
	}
	return out
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
// already_exists. The report keeps a snapshot of the message as it is now,
// so deleting or purging it, or the chat it is in, does not take the
// evidence away.
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
	msgs := []model.Message{m}
	if err := s.store.Messages().Hydrate(ctx, msgs, actor.ID); err != nil {
		return model.Report{}, err
	}
	to, toAdmins, err := s.route(ctx, s.store, st.Conversation, m.AuthorID)
	if err != nil {
		return model.Report{}, err
	}
	r := model.Report{MessageID: id, ConversationID: m.ConversationID, AccountID: m.AuthorID, MessageAt: m.CreatedAt,
		Snapshot: snapshotOf(st.Conversation, msgs[0]), ToAdmins: toAdmins, ReportedByID: actor.ID, Reason: reason, CreatedAt: s.now()}
	created, err := s.store.Moderation().CreateReport(ctx, &r)
	if err != nil {
		return model.Report{}, err
	}
	if !created {
		return r, model.ErrAlreadyExists.With("You have already reported this message.")
	}
	// The author is never told, nor who reported; handlers are.
	s.tell(to, model.EventReportCreated, map[string]any{"report": r})
	return r, nil
}

// ReportAccount files a report of a person, for their name, status or
// picture, by anyone who can see them. It goes to the instance's admins
// with a snapshot of what the profile shows now; the person is not told.
func (s *Service) ReportAccount(ctx context.Context, actor model.Account, id model.AccountID, reason string) (model.Report, error) {
	if !actor.Active() {
		return model.Report{}, model.ErrUnauthenticated
	}
	target, err := s.store.Accounts().Get(ctx, id)
	if err != nil || s.perm.CanSeeAccount(ctx, actor, target) != nil || target.Removed() {
		return model.Report{}, model.ErrNotFound.With("There is no such account.")
	}
	if target.ID == actor.ID {
		return model.Report{}, model.ErrForbidden.With("You cannot report yourself.")
	}
	if actor.Role == model.RoleBot {
		return model.Report{}, model.ErrForbidden.With("Bots cannot report people.")
	}
	if err := checkReason(reason); err != nil {
		return model.Report{}, err
	}
	snap := &model.ReportSnapshot{AuthorID: target.ID, Username: target.Username, DisplayName: target.DisplayName, AvatarURL: target.AvatarURL}
	if target.Status != nil {
		snap.StatusText = strings.TrimSpace(target.Status.Emoji + " " + target.Status.Text)
	}
	r := model.Report{Kind: model.ReportOfAccount, AccountID: target.ID, Snapshot: snap, ToAdmins: true, ReportedByID: actor.ID,
		Reason: reason, CreatedAt: s.now()}
	created, err := s.store.Moderation().CreateReport(ctx, &r)
	if err != nil {
		return model.Report{}, err
	}
	if !created {
		return r, model.ErrAlreadyExists.With("You have already reported this person.")
	}
	to, err := admins(ctx, s.store)
	if err != nil {
		return r, err
	}
	s.tell(without(to, target.ID), model.EventReportCreated, map[string]any{"report": r})
	return r, nil
}

// Reports lists the reports the actor may handle, each with its snapshot
// and, where the actor may read it, the message as they see it now. A
// channel's moderators and managers are not shown reports of messages from
// before their first join there; nobody is shown a report about
// themselves.
func (s *Service) Reports(ctx context.Context, actor model.Account, conv model.ConversationID, waiting bool, p store.Page) ([]model.Report, string, error) {
	list, next, err := s.store.Moderation().Reports(ctx,
		store.ReportFilter{Scope: perm.ReportScope(actor), ConversationID: conv, Waiting: waiting}, p)
	if err != nil {
		return nil, "", err
	}
	states := map[model.ConversationID]*perm.State{}
	for i := range list {
		r := &list[i]
		if r.MessageID == "" {
			continue
		}
		st, known := states[r.ConversationID]
		if !known {
			if one, err := s.perm.Load(ctx, actor, r.ConversationID); err == nil {
				st = &one
			}
			states[r.ConversationID] = st
		}
		if st == nil {
			continue
		}
		m, err := s.store.Messages().Get(ctx, r.MessageID)
		if err != nil || perm.CheckMessage(*st, perm.ReadMessage, m) != nil {
			// An admin shown a report from a private channel or a chat
			// gets its snapshot, and nothing of the conversation.
			continue
		}
		msgs := []model.Message{m}
		if err := s.store.Messages().Hydrate(ctx, msgs, actor.ID); err != nil {
			return nil, "", err
		}
		msgs[0].ClientID = ""
		r.Message = &msgs[0]
	}
	return list, next, nil
}

// mayHandle answers whether the actor handles r, and how: as one of the
// conversation's moderators or managers there (st set), or as an admin of
// the instance. A report nobody may show the actor is not found.
func (s *Service) mayHandle(ctx context.Context, actor model.Account, r model.Report) (st *perm.State, asAdmin bool, err error) {
	if !actor.Active() || r.AccountID == actor.ID {
		return nil, false, model.ErrNotFound
	}
	if r.ConversationID != "" {
		if one, err := s.perm.Load(ctx, actor, r.ConversationID); err == nil && one.CanSee() {
			if cerr := perm.Check(one, perm.HandleReports); cerr == nil && one.Sees(model.Message{CreatedAt: r.MessageAt}) {
				return &one, actor.Role.IsAdmin(), nil
			} else if model.CodeOf(cerr) == model.CodeTimedOut {
				return nil, false, model.ErrTimedOut
			}
			if actor.Role.IsAdmin() && (r.ToAdmins || one.Conversation.Public()) {
				return &one, true, nil
			}
		}
	}
	if actor.Role.IsAdmin() && r.ToAdmins {
		return nil, true, nil
	}
	return nil, false, model.ErrNotFound
}

// HandleReport records a handler's decision and carries it out: nothing
// more, deleting the message (for an admin who is no moderator there,
// purging it where they can read it), or timing its author out: in that
// channel for one of its moderators or managers, and on the whole instance
// for an admin who is not one. It settles every waiting report on the same
// message or person.
func (s *Service) HandleReport(ctx context.Context, actor model.Account, id, outcome string, timeoutSeconds int64) (model.Report, error) {
	r, err := s.store.Moderation().GetReport(ctx, id)
	if err != nil {
		return r, hide(err)
	}
	st, asAdmin, err := s.mayHandle(ctx, actor, r)
	if err != nil {
		return model.Report{}, err
	}
	keeper := st != nil && st.KeepsPeace()
	if !r.Waiting() {
		return r, model.ErrConflict.With("That report has already been handled.")
	}
	var m model.Message
	if r.MessageID != "" {
		if m, err = s.store.Messages().Get(ctx, r.MessageID); err != nil && !errors.Is(err, model.ErrNotFound) {
			return r, err
		}
	}
	// Each outcome needs the handler to hold that power anyway.
	switch outcome {
	case model.ReportDismissed:
	case model.ReportDeleted:
		if r.MessageID == "" || m.ID == "" {
			return r, model.ErrConflict.With("There is no message left to delete.")
		}
		if !m.Deleted() {
			if s.deleter == nil {
				return r, model.ErrUnavailable
			}
			switch {
			case keeper:
				if _, err := s.deleter.Delete(ctx, actor, m.ID); err != nil && !errors.Is(err, model.ErrConflict) {
					return r, err
				}
			case st != nil && perm.CheckMessage(*st, perm.PurgeDirectly, m) == nil:
				// An admin handling a public channel's report is no
				// manager there (ruling R99): what is the instance's to
				// do is to purge, which deletes too.
				if _, _, err := s.deleter.Purge(ctx, actor, m.ID); err != nil && !errors.Is(err, model.ErrConflict) {
					return r, err
				}
			default:
				return r, model.ErrForbidden.With("You cannot read that conversation, so you cannot delete in it. Time the author out, or exile them, instead.")
			}
		}
	case model.ReportTimedOut:
		if timeoutSeconds == 0 {
			return r, model.Invalid("timeout_seconds", "Say for how long to time the author out.")
		}
		var err error
		if keeper && r.ConversationID != "" {
			_, err = s.TimeOut(ctx, actor, r.ConversationID, r.AccountID, timeoutSeconds, "")
		} else if asAdmin {
			// An admin who is no moderator there times the person out on
			// the whole instance.
			_, err = s.TimeOutEverywhere(ctx, actor, r.AccountID, timeoutSeconds, "")
		} else {
			err = model.ErrForbidden.With("You cannot time this person out.")
		}
		if err != nil {
			if e, ok := model.AsError(err); ok && e.Field == "seconds" {
				return r, model.Invalid("timeout_seconds", e.Message)
			}
			return r, err
		}
	default:
		return r, model.Invalid("outcome", "An outcome is dismissed, deleted or timed_out.")
	}
	now := s.now()
	var conv *model.Conversation
	if st != nil {
		conv = &st.Conversation
	} else if r.ConversationID != "" {
		if c, err := s.store.Conversations().Get(ctx, r.ConversationID); err == nil {
			conv = &c
		}
	}
	var settled []model.Report
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		var err error
		if settled, err = tx.Moderation().SettleReports(ctx, r, outcome, actor.ID, now); err != nil {
			return err
		}
		// Without the message text. A report an admin handled is the
		// instance's business, and written down, from a chat too but then
		// without naming the chat; nothing else done in a chat is.
		target, targetID := "report", r.ID
		if r.Kind == model.ReportOfAccount {
			target, targetID = "account", string(r.AccountID)
		}
		detail := map[string]any{"report_id": r.ID, "kind": r.Kind, "outcome": outcome, "reports": len(settled), "account_id": r.AccountID}
		at := conv
		if conv != nil && conv.Adhoc() && !keeper {
			at = nil
		} else if r.ConversationID != "" {
			detail["conversation_id"] = r.ConversationID
			if r.MessageID != "" {
				target, targetID = "message", string(r.MessageID)
			}
		}
		return audit(ctx, tx, at, model.AuditEntry{At: now, ActorID: string(actor.ID), Action: "report.handled",
			TargetKind: target, TargetID: targetID, Detail: detail})
	})
	if err != nil {
		return r, err
	}
	var to []model.AccountID
	if conv != nil {
		if to, _, err = s.route(ctx, s.store, *conv, r.AccountID); err != nil {
			return r, err
		}
	} else if r.ToAdmins {
		ids, err := admins(ctx, s.store)
		if err != nil {
			return r, err
		}
		to = without(ids, r.AccountID)
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
	if n, err := s.store.Members().Count(ctx, r.ConversationID); r.ConversationID != "" && s.deleter != nil && err == nil && n == 0 {
		// A chat nobody is in any more waited only for its reports.
		if err := s.deleter.Tidy(ctx, r.ConversationID); err != nil && !errors.Is(err, model.ErrNotFound) {
			s.log.Warn("could not tidy a conversation after its report", "err", err)
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
