package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// errNoMatch is the one answer to every failed recovery by code, whether or
// not the username exists (docs/AUTH.md section 1).
var errNoMatch = model.ErrUnauthenticated.With("That username and code do not match.")

// errBadLink is the one answer to every failed recovery link.
var errBadLink = model.ErrUnauthenticated.With("That recovery link has been used or has run out.")

// dummyAccount stands in for an account that does not exist, so a wrong
// username costs the same work as a wrong code.
const dummyAccount = model.AccountID("00000000-0000-7000-8000-000000000000")

// recoverySession makes the restricted session a recovery code or link
// leads to: one hour, never extended, and able only to set up a new way in.
func (s *Service) recoverySession(ctx context.Context, tx store.Areas, acc model.Account, by, label string) (SignIn, error) {
	now := s.now()
	end := now.Add(recoveryLife)
	ses := model.Session{AccountID: acc.ID, Label: label, SignedInBy: by, CreatedAt: now, ExpiresAt: end, MaxExpiresAt: end}
	token, err := s.webSession(ctx, tx, &ses)
	return SignIn{Session: ses, Account: acc, Token: token}, err
}

// RecoverWithCode signs in with a username and a recovery code. Any failure
// gives the same answer after the same work.
func (s *Service) RecoverWithCode(ctx context.Context, addr, username, code, label string) (SignIn, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" || strings.TrimSpace(code) == "" {
		return SignIn{}, model.BadRequest("username and code are needed.")
	}
	if err := blocked(s.limitRecoveryAddr, addr); err != nil {
		return SignIn{}, err
	}
	if err := blocked(s.limitRecoveryUser, username); err != nil {
		return SignIn{}, err
	}
	fail := func() (SignIn, error) {
		failed(s.limitRecoveryAddr, addr)
		failed(s.limitRecoveryUser, username)
		s.log.Warn("sign-in failed", "method", "recovery_code", "address", addr)
		return SignIn{}, errNoMatch
	}
	acc, err := s.store.Accounts().ByUsername(ctx, username)
	known := err == nil
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return SignIn{}, err
	}
	pol, err := s.policy(ctx, s.store)
	if err != nil {
		return SignIn{}, err
	}
	hash, wellFormed := secret.HashRecoveryCode(code)
	if !wellFormed {
		hash = secret.HashToken(code)
	}
	target := dummyAccount
	if known && acc.Active() && pol.passkeyAllowed(acc.Role) {
		target = acc.ID
	}
	var out SignIn
	matched := false
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		ok, err := tx.Recovery().UseCode(ctx, target, hash, s.now())
		if err != nil || !ok || target == dummyAccount {
			return err
		}
		matched = true
		if out, err = s.recoverySession(ctx, tx, acc, "recovery_code", label); err != nil {
			return err
		}
		_, left, err := tx.Recovery().CountCodes(ctx, acc.ID)
		if err != nil {
			return err
		}
		return s.audit(ctx, tx, acc.ID, "recovery.code_used", "account", string(acc.ID), map[string]any{"codes_left": left})
	})
	if err != nil {
		return SignIn{}, err
	}
	if !matched {
		return fail()
	}
	s.sessionStarted(out.Session)
	return out, nil
}

// RecoverWithLink uses up a recovery link and starts a recovery session.
// Opening the link's page uses nothing; this, made by the button on that
// page, does.
func (s *Service) RecoverWithLink(ctx context.Context, addr, value, label string) (SignIn, error) {
	if err := limited(s.limitRecoveryLink, addr); err != nil {
		return SignIn{}, err
	}
	if value == "" {
		return SignIn{}, model.BadRequest("token is needed.")
	}
	var out SignIn
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		id, ok, err := tx.Recovery().UseLink(ctx, secret.HashToken(value), s.now())
		if err != nil {
			return err
		}
		if !ok {
			return errBadLink
		}
		acc, err := tx.Accounts().Get(ctx, id)
		if err != nil || !acc.Active() || !isPerson(acc.Role) {
			return errBadLink
		}
		if out, err = s.recoverySession(ctx, tx, acc, "recovery_link", label); err != nil {
			return err
		}
		return s.audit(ctx, tx, acc.ID, "recovery.link_used", "account", string(acc.ID), nil)
	})
	if err != nil {
		if errors.Is(err, errBadLink) {
			s.log.Warn("sign-in failed", "method", "recovery_link", "address", addr)
		}
		return SignIn{}, err
	}
	s.sessionStarted(out.Session)
	return out, nil
}

// isPerson reports whether a role is one a person signs in with.
func isPerson(r model.Role) bool {
	return r == model.RoleOwner || r == model.RoleAdmin || r == model.RoleMember || r == model.RoleGuest
}

// RecoveryLink is a one-time link and when it stops working.
type RecoveryLink struct {
	URL       string     `json:"url"`
	ExpiresAt model.Time `json:"expires_at"`
}

// CreateRecoveryLink makes a 24-hour link for an account, revoking any
// earlier unused one. Admins make them for members and guests, the owner
// for admins too, and nobody through the API for the owner, so a link
// cannot be used to climb to a higher role.
func (s *Service) CreateRecoveryLink(ctx context.Context, who Who, target model.AccountID) (RecoveryLink, error) {
	if err := complete(who); err != nil {
		return RecoveryLink{}, err
	}
	if err := s.admin(ctx, who, perm.UseAdminConsole); err != nil {
		return RecoveryLink{}, err
	}
	acc, err := s.store.Accounts().Get(ctx, target)
	if err != nil {
		return RecoveryLink{}, err
	}
	if acc.Role == model.RoleOwner {
		return RecoveryLink{}, model.ErrForbidden.With("A recovery link for an owner is made on the server with `churr recovery`.")
	}
	if err := perm.CheckOnAccount(who.Account, perm.MakeRecoveryLink, acc); err != nil {
		return RecoveryLink{}, err
	}
	if !acc.Active() || !isPerson(acc.Role) {
		return RecoveryLink{}, model.ErrForbidden.With("Recovery links are for people with a claimed account on this server.")
	}
	if err := s.fresh(who); err != nil {
		return RecoveryLink{}, err
	}
	return makeRecoveryLink(ctx, s.store, s.cfg.Origin, acc, who.Account.ID, adminLinkLife)
}

func makeRecoveryLink(ctx context.Context, st store.Store, origin string, acc model.Account, by model.AccountID, life time.Duration) (RecoveryLink, error) {
	value, hash := secret.NewLinkValue()
	now := model.Now()
	out := RecoveryLink{URL: origin + "/recover/" + value, ExpiresAt: now.Add(life)}
	err := st.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Recovery().CreateLink(ctx, hash, acc.ID, by, out.ExpiresAt); err != nil {
			return err
		}
		e := model.AuditEntry{At: now, ActorID: string(by), ActorKind: "account", Action: "recovery.link_created",
			TargetKind: "account", TargetID: string(acc.ID), Detail: map[string]any{"for": acc.Username}}
		if by == "" {
			e.ActorKind = "cli"
		}
		return tx.Audit().Append(ctx, e)
	})
	return out, err
}

// CLIRecoveryLink is `churr recovery <username>`: a one-hour link for any
// local person, the owner included. It needs only the data directory, which
// already means full control.
func CLIRecoveryLink(ctx context.Context, st store.Store, origin, username string) (RecoveryLink, error) {
	acc, err := st.Accounts().ByUsername(ctx, strings.ToLower(strings.TrimSpace(username)))
	if err != nil {
		return RecoveryLink{}, err
	}
	if !acc.Active() || !isPerson(acc.Role) {
		return RecoveryLink{}, model.ErrForbidden.With("Recovery links are for people with a claimed account on this server.")
	}
	return makeRecoveryLink(ctx, st, origin, acc, "", cliLinkLife)
}

// newRecoveryCodes makes a fresh set of eight and stores their hashes,
// deleting any earlier set. The codes are shown once.
func newRecoveryCodes(ctx context.Context, tx store.Areas, account model.AccountID) ([]string, error) {
	codes := make([]string, 0, recoveryCodes)
	hashes := make([][]byte, 0, recoveryCodes)
	for i := 0; i < recoveryCodes; i++ {
		code := secret.NewRecoveryCode()
		h, _ := secret.HashRecoveryCode(code)
		codes, hashes = append(codes, code), append(hashes, h)
	}
	return codes, tx.Recovery().ReplaceCodes(ctx, account, hashes)
}

// RecoveryCodesLeft says how many of the caller's codes are unused.
func (s *Service) RecoveryCodesLeft(ctx context.Context, who Who) (int, error) {
	if err := complete(who); err != nil {
		return 0, err
	}
	_, left, err := s.store.Recovery().CountCodes(ctx, who.Account.ID)
	return left, err
}

// RegenerateRecoveryCodes replaces the caller's codes with eight new ones.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, who Who) ([]string, error) {
	if err := s.fresh(who); err != nil {
		return nil, err
	}
	var codes []string
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		var err error
		if codes, err = newRecoveryCodes(ctx, tx, who.Account.ID); err != nil {
			return err
		}
		return s.audit(ctx, tx, who.Account.ID, "recovery.codes_replaced", "account", string(who.Account.ID), nil)
	})
	return codes, err
}

// same compares two secrets without leaking where they differ.
func same(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
