// Package approvals is the instance's one queue of requests that wait for
// someone who may approve them (docs/FEATURES.md section 31): a
// registration that waits before its account may sign in, a channel a member
// asked to have made, a webhook asked for in a channel. Each kind is an
// entry in a registry that says who may act on it and what approving and
// declining do; adding a kind is a Register call with its handlers.
//
// Who may act: an Owner or an Admin, and for the kinds that say so the
// accounts given the instance-level flag "may approve requests" (a flag, not
// a role, ruling R97). Every decision is written to the audit log with who
// made it, and the one who asked is told the outcome.
package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Hub sends live events to accounts; live.Hub implements it.
type Hub interface {
	Send(account model.AccountID, v any)
}

// EventChanged tells an account to read its approvals summary again: the
// flag was given or taken away.
const EventChanged = "approvals.changed"

// Kind is one entry of the registry.
type Kind struct {
	Name string
	// Flag says the holders of the "may approve requests" flag act on
	// this kind, as well as Owners and Admins.
	Flag bool
	// List lists what waits, for a kind kept elsewhere than the approvals
	// table (webhooks); nil for a kind kept in it.
	List func(ctx context.Context, actor model.Account) ([]model.Approval, error)
	// Describe fills in Summary for a kind kept in the table.
	Describe func(ctx context.Context, a *model.Approval)
	// Approve and Decline carry out the decision. For a kind kept in the
	// table the queue records it after Approve or Decline has succeeded;
	// a kind kept elsewhere records and audits it itself.
	Approve func(ctx context.Context, actor model.Account, a model.Approval) error
	Decline func(ctx context.Context, actor model.Account, a model.Approval, reason string) error
}

// Service is the queue.
type Service struct {
	store store.Store
	hub   Hub
	log   *slog.Logger
	now   func() model.Time
	kinds map[string]Kind
	order []string
	// chat makes the channels approved (kinds.go).
	chat *chat.Service
}

// New returns the queue with no kinds registered.
func New(st store.Store, hub Hub, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: st, hub: hub, log: log, now: model.Now, kinds: map[string]Kind{}}
}

// Register adds a kind. It is called while the server is being put
// together.
func (s *Service) Register(k Kind) {
	if _, dup := s.kinds[k.Name]; !dup {
		s.order = append(s.order, k.Name)
	}
	s.kinds[k.Name] = k
}

// MayApprove reports whether the account holds the flag.
func (s *Service) MayApprove(ctx context.Context, a model.Account) (bool, error) {
	if !a.Active() || !a.Role.IsFullMember() {
		return false, nil
	}
	return s.store.Approvals().IsApprover(ctx, a.ID)
}

// mayAct reports whether actor may see and decide kind k.
func (s *Service) mayAct(ctx context.Context, actor model.Account, k Kind) (bool, error) {
	if !actor.Active() {
		return false, nil
	}
	if actor.Role.IsAdmin() {
		return true, nil
	}
	if !k.Flag {
		return false, nil
	}
	return s.MayApprove(ctx, actor)
}

// Kinds returns the kinds actor may act on, in the order they were
// registered.
func (s *Service) Kinds(ctx context.Context, actor model.Account) ([]string, error) {
	out := []string{}
	for _, name := range s.order {
		ok, err := s.mayAct(ctx, actor, s.kinds[name])
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, name)
		}
	}
	return out, nil
}

// Summary is what a client shows of the queue: whether the account holds
// the flag, the kinds it may act on and how many of each wait.
type Summary struct {
	MayApprove bool           `json:"may_approve"`
	Kinds      []string       `json:"kinds"`
	Counts     map[string]int `json:"counts"`
}

// Summarize answers for actor.
func (s *Service) Summarize(ctx context.Context, actor model.Account) (Summary, error) {
	if !actor.Active() {
		return Summary{}, model.ErrUnauthenticated
	}
	may, err := s.MayApprove(ctx, actor)
	if err != nil {
		return Summary{}, err
	}
	kinds, err := s.Kinds(ctx, actor)
	if err != nil {
		return Summary{}, err
	}
	out := Summary{MayApprove: may, Kinds: kinds, Counts: map[string]int{}}
	var table []string
	for _, name := range kinds {
		k := s.kinds[name]
		out.Counts[name] = 0
		if k.List == nil {
			table = append(table, name)
			continue
		}
		list, err := k.List(ctx, actor)
		if err != nil {
			return out, err
		}
		out.Counts[name] = len(list)
	}
	n, err := s.store.Approvals().CountWaiting(ctx, table, s.now())
	if err != nil {
		return out, err
	}
	for k, v := range n {
		out.Counts[k] = v
	}
	return out, nil
}

// List returns what waits for actor, oldest first, of one kind or, with
// kind "", of every kind actor may act on. Each carries its requester.
func (s *Service) List(ctx context.Context, actor model.Account, kind string) ([]model.Approval, error) {
	kinds, err := s.Kinds(ctx, actor)
	if err != nil {
		return nil, err
	}
	if len(kinds) == 0 {
		return nil, model.ErrForbidden.With("Only those who may approve requests see the queue.")
	}
	if kind != "" {
		if _, known := s.kinds[kind]; !known {
			return nil, model.Invalid("kind", "There is no such kind of request.")
		}
		if !contains(kinds, kind) {
			return nil, model.ErrForbidden.With("You may not approve requests of that kind.")
		}
		kinds = []string{kind}
	}
	out := []model.Approval{}
	var table []string
	for _, name := range kinds {
		k := s.kinds[name]
		if k.List == nil {
			table = append(table, name)
			continue
		}
		list, err := k.List(ctx, actor)
		if err != nil {
			return nil, err
		}
		out = append(out, list...)
	}
	rows, err := s.store.Approvals().Waiting(ctx, table, s.now(), model.MaxLimit)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if d := s.kinds[rows[i].Kind].Describe; d != nil {
			d(ctx, &rows[i])
		}
	}
	out = append(out, rows...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	for i := range out {
		if acc, err := s.store.Accounts().Get(ctx, out[i].RequesterID); err == nil {
			acc.WebAuthnHandle, acc.PrefsJSON = nil, nil
			out[i].Requester = &acc
		}
	}
	return out, nil
}

// MaxReason is the longest reason a decider may give, in characters.
const MaxReason = 500

// Decide approves or declines one request. A request that does not wait any
// more is not_found; one of a kind actor may not act on is forbidden.
func (s *Service) Decide(ctx context.Context, actor model.Account, kind, id string, approve bool, reason string) (model.Approval, error) {
	k, known := s.kinds[kind]
	if !known {
		return model.Approval{}, model.ErrNotFound
	}
	ok, err := s.mayAct(ctx, actor, k)
	if err != nil {
		return model.Approval{}, err
	}
	if !ok {
		return model.Approval{}, model.ErrForbidden.With("You may not approve requests of that kind.")
	}
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > MaxReason {
		return model.Approval{}, model.Invalid("reason", "A reason is at most 500 characters.")
	}
	if k.List != nil {
		// Kept elsewhere: the kind's own service records and audits it.
		a := model.Approval{ID: id, Kind: kind}
		if approve {
			err = k.Approve(ctx, actor, a)
		} else {
			err = k.Decline(ctx, actor, a, reason)
		}
		if err != nil {
			return a, err
		}
		a.State, a.DecidedBy, a.DecidedAt = model.ApprovalDeclined, actor.ID, s.now()
		if approve {
			a.State = model.ApprovalApproved
		}
		s.tellDeciders(ctx, a)
		return a, nil
	}
	a, err := s.store.Approvals().Get(ctx, id)
	if err != nil || a.Kind != kind || !a.Waiting(s.now()) {
		return model.Approval{}, model.ErrNotFound.With("That request has been answered already, or has lapsed.")
	}
	if approve {
		err = k.Approve(ctx, actor, a)
	} else if k.Decline != nil {
		err = k.Decline(ctx, actor, a, reason)
	}
	if err != nil {
		return a, err
	}
	state := model.ApprovalDeclined
	if approve {
		state = model.ApprovalApproved
	}
	now := s.now()
	var found bool
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		var err error
		if a, found, err = tx.Approvals().Decide(ctx, id, state, actor.ID, reason, now); err != nil || !found {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{At: now, ActorID: string(actor.ID), ActorKind: "account",
			Action: "approval." + state, TargetKind: "account", TargetID: string(a.RequesterID),
			Detail: map[string]any{"kind": kind, "approval_id": id, "reason": reason}})
	})
	if err != nil {
		return a, err
	}
	if !found {
		return a, model.ErrNotFound.With("That request has been answered already, or has lapsed.")
	}
	if d := k.Describe; d != nil {
		d(ctx, &a)
	}
	// The one who asked is told the outcome and the reason, not who
	// decided; a registration's applicant is told when they next come.
	if kind != model.ApprovalRegistration {
		told := a
		told.DecidedBy = ""
		s.hub.Send(a.RequesterID, model.Event{Type: model.EventApprovalDecided, At: now, Payload: map[string]any{"approval": told}})
	}
	s.tellDeciders(ctx, a)
	return a, nil
}

// Submit puts a request of a kind kept in the table in the queue and tells
// those who may act on it.
func (s *Service) Submit(ctx context.Context, a *model.Approval) error {
	if _, known := s.kinds[a.Kind]; !known {
		return errors.New("approvals: no such kind " + a.Kind)
	}
	if err := s.store.Approvals().Create(ctx, a); err != nil {
		return err
	}
	s.Created(ctx, *a)
	return nil
}

// Created tells those who may act on a new request of it, with the
// request: a live event their clients count and may notify.
func (s *Service) Created(ctx context.Context, a model.Approval) {
	k := s.kinds[a.Kind]
	if d := k.Describe; d != nil {
		d(ctx, &a)
	}
	e := model.Event{Type: model.EventApprovalCreated, At: s.now(), Payload: map[string]any{"approval": a}}
	for _, id := range s.deciders(ctx, k) {
		s.hub.Send(id, e)
	}
}

// tellDeciders tells those who may act on a kind that a request was
// decided, so their lists and counts drop it.
func (s *Service) tellDeciders(ctx context.Context, a model.Approval) {
	e := model.Event{Type: model.EventApprovalDecided, At: s.now(), Payload: map[string]any{"approval": a}}
	for _, id := range s.deciders(ctx, s.kinds[a.Kind]) {
		s.hub.Send(id, e)
	}
}

// deciders are the Owners and Admins, and the flag's holders for a kind
// that takes them.
func (s *Service) deciders(ctx context.Context, k Kind) []model.AccountID {
	var ids []model.AccountID
	g, err := s.store.Groups().Builtin(ctx, model.GroupAdmins)
	if err == nil {
		ids, err = s.store.Groups().MemberIDs(ctx, g.ID)
	}
	if err != nil {
		s.log.Warn("could not work out who to tell of a request", "err", err)
	}
	if k.Flag {
		more, err := s.store.Approvals().Approvers(ctx)
		if err != nil {
			s.log.Warn("could not work out who to tell of a request", "err", err)
		}
		ids = append(ids, more...)
	}
	seen := map[model.AccountID]bool{}
	out := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// SetApprover gives an account the flag, or takes it away: an Owner's or an
// Admin's, for an active person's account of this instance that is not a
// guest. It is written to the audit log and the account is told.
func (s *Service) SetApprover(ctx context.Context, actor model.Account, account model.AccountID, may bool) error {
	if !actor.Active() {
		return model.ErrUnauthenticated
	}
	if !actor.Role.IsAdmin() {
		return model.ErrForbidden.With("Only admins can do that.")
	}
	target, err := s.store.Accounts().Get(ctx, account)
	if err != nil {
		return err
	}
	if may && (!target.Active() || !target.Role.IsFullMember()) {
		return model.ErrConflict.With("Only an active owner, admin or member can be given that.")
	}
	now := s.now()
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Approvals().SetApprover(ctx, account, may, actor.ID, now); err != nil {
			return err
		}
		action := "approver.granted"
		if !may {
			action = "approver.revoked"
		}
		return tx.Audit().Append(ctx, model.AuditEntry{At: now, ActorID: string(actor.ID), ActorKind: "account",
			Action: action, TargetKind: "account", TargetID: string(account)})
	})
	if err != nil {
		return err
	}
	s.hub.Send(account, model.Event{Type: EventChanged, At: now, Payload: map[string]any{"may_approve": may}})
	return nil
}

// Approvers lists the accounts that hold the flag, for the admin console.
func (s *Service) Approvers(ctx context.Context, actor model.Account) ([]model.AccountID, error) {
	if !actor.Active() || !actor.Role.IsAdmin() {
		return nil, model.ErrForbidden.With("Only admins can do that.")
	}
	ids, err := s.store.Approvals().Approvers(ctx)
	if ids == nil {
		ids = []model.AccountID{}
	}
	return ids, err
}

// Payload decodes an approval's payload.
func Payload(a model.Approval, into any) error {
	if len(a.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(a.Payload, into)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
