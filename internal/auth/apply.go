package auth

import (
	"context"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Registrations that wait for approval (docs/FEATURES.md section 31,
// AUTH.md section 4.4). While the admin setting registration.approval is on,
// someone who registers without an invite, in open registration, makes
// their account and its first way in as anyone does, and then waits: the
// account is not claimed, so it signs in to nothing and is in no group or
// channel, until one who may approve registrations approves it. An invite
// is someone already vouching, and goes straight in as before.

// applying reports whether a registration waits for approval, and refuses
// one when too many wait already.
func (s *Service) applying(ctx context.Context, inviteCode, note string) (bool, error) {
	if inviteCode != "" {
		return false, nil
	}
	on, err := store.SettingBool(ctx, s.store, store.SettingRegistrationApproval, false)
	if err != nil || !on {
		return false, err
	}
	if utf8.RuneCountInString(note) > model.MaxApprovalNote {
		return false, model.Invalid("note", "A note is at most 500 characters.")
	}
	n, err := s.store.Approvals().CountWaiting(ctx, []string{model.ApprovalRegistration}, s.now())
	if err != nil {
		return false, err
	}
	if n[model.ApprovalRegistration] >= model.MaxWaitingRegistrations {
		return false, model.RateLimited(3600).With("Too many people are waiting to join already. Try again later.")
	}
	return true, nil
}

// applyTx makes the waiting account with its first way in, and its
// application. Nobody is signed in.
func (s *Service) applyTx(ctx context.Context, tx store.Tx, c ceremony, way firstWay) (SignIn, error) {
	handle := c.handle
	if way.passkey == nil {
		handle = nil
	}
	now := s.now()
	acc := model.Account{Username: c.username, DisplayName: c.displayName, Role: model.RoleMember, WebAuthnHandle: handle, CreatedAt: now}
	if err := tx.Accounts().CreateUnclaimed(ctx, &acc); err != nil {
		return SignIn{}, err
	}
	if _, err := way.addTx(ctx, tx, acc.ID, now); err != nil {
		return SignIn{}, err
	}
	a := model.Approval{Kind: model.ApprovalRegistration, RequesterID: acc.ID, Note: c.note, CreatedAt: now}
	if err := tx.Approvals().Create(ctx, &a); err != nil {
		return SignIn{}, err
	}
	if err := tx.Audit().Append(ctx, model.AuditEntry{At: now, ActorID: string(acc.ID), ActorKind: "account",
		Action: "account.applied", TargetKind: "account", TargetID: string(acc.ID), Detail: map[string]any{"how": way.how()}}); err != nil {
		return SignIn{}, err
	}
	return SignIn{Account: acc, Applied: &a}, nil
}

// application is what an account that cannot sign in is told when the way
// in was right and it is an applicant's: that it waits, was declined (with
// the reason given) or lapsed. nil means it is no applicant's.
func (s *Service) application(ctx context.Context, acc model.Account) error {
	if !acc.ClaimedAt.IsZero() || !acc.Local() {
		return nil
	}
	a, found, err := s.store.Approvals().Latest(ctx, acc.ID, model.ApprovalRegistration)
	if err != nil || !found {
		return err
	}
	switch {
	case a.Waiting(s.now()):
		return model.ErrApprovalPending
	case a.State == model.ApprovalDeclined:
		msg := "Your request to join was declined."
		if a.Reason != "" {
			msg += " " + a.Reason
		}
		return model.ErrForbidden.With(msg)
	case a.State != model.ApprovalApproved:
		return model.ErrForbidden.With("Your request to join waited too long and has lapsed.")
	}
	return nil
}
