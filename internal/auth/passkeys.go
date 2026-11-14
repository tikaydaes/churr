package auth

import (
	"context"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// secondFactorState says which proofs the admin accepts that a person can
// give here, and which of those the account already has.
func (s *Service) secondFactorState(ctx context.Context, account model.AccountID) (model.SecondFactorState, error) {
	st := model.SecondFactorState{Accepted: []string{}, SetUp: []string{}}
	pol, err := s.policy(ctx, s.store)
	if err != nil {
		return st, err
	}
	keys, err := s.store.Passkeys().Count(ctx, account)
	if err != nil {
		return st, err
	}
	t, found, err := s.store.Identities().GetTotp(ctx, account)
	if err != nil {
		return st, err
	}
	if pol.Accepted["passkey"] {
		st.Accepted = append(st.Accepted, "passkey")
		if keys > 0 {
			st.SetUp = append(st.SetUp, "passkey")
		}
	}
	if pol.Accepted["totp"] {
		st.Accepted = append(st.Accepted, "totp")
		if found && !t.ConfirmedAt.IsZero() {
			st.SetUp = append(st.SetUp, "totp")
		}
	}
	return st, nil
}

// SecondFactor lists the proofs a pending session may give.
func (s *Service) SecondFactor(ctx context.Context, who Who) (model.SecondFactorState, error) {
	if err := person(who); err != nil {
		return model.SecondFactorState{}, err
	}
	return s.secondFactorState(ctx, who.Account.ID)
}

// proofGiven records a good passkey or authenticator proof on the caller's
// session. A pending session becomes complete, with its full lifetime and a
// new token, so the pending token stops working. Any other session is
// marked freshly checked for ten minutes.
func (s *Service) proofGiven(ctx context.Context, tx store.Areas, who Who, proof string) (*SignIn, error) {
	now := s.now()
	ses := who.Session
	if !ses.PendingSecondFactor {
		if ses.Recovery {
			return &SignIn{Session: ses, Account: who.Account}, nil
		}
		if err := tx.Sessions().SetFresh(ctx, ses.ID, now); err != nil {
			return nil, err
		}
		ses.FreshAt = now
		return &SignIn{Session: ses, Account: who.Account}, nil
	}
	end := ses.MaxExpiresAt
	if end.IsZero() {
		life, err := s.ssoLife(ctx, tx)
		if err != nil {
			return nil, err
		}
		end = ses.CreatedAt.Add(life)
	}
	token, hash := secret.NewToken(secret.PrefixSession)
	ok, err := tx.Sessions().Complete(ctx, ses.ID, hash, proof, end, now)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, model.ErrUnauthenticated
	}
	s.wrongProofs.drop(string(ses.ID))
	ses.PendingSecondFactor, ses.SecondFactor, ses.ExpiresAt, ses.FreshAt = false, proof, end, now
	return &SignIn{Session: ses, Account: who.Account, Token: token}, nil
}

// wrongProof counts a wrong proof against a pending session. Five end it,
// and the person starts again at the provider (docs/AUTH.md section 9.3).
func (s *Service) wrongProof(ctx context.Context, who Who, err error) error {
	if !who.Session.PendingSecondFactor {
		return err
	}
	key := string(who.Session.ID)
	n, _ := s.wrongProofs.get(key)
	n++
	if n < maxWrongProofs {
		s.wrongProofs.put(key, n)
		return err
	}
	if rerr := s.store.Sessions().Revoke(ctx, who.Session.ID, s.now()); rerr != nil {
		return rerr
	}
	s.sessionsEnded(ctx, "too many wrong tries", store.SessionRef{ID: who.Session.ID, AccountID: who.Account.ID})
	return model.ErrUnauthenticated.With("Too many wrong tries. Sign in again.")
}

// credentials returns an account's passkeys and the library's records.
func (s *Service) credentials(ctx context.Context, account model.AccountID) ([]model.Passkey, []webauthn.Credential, error) {
	keys, err := s.store.Passkeys().ForAccount(ctx, account)
	if err != nil {
		return nil, nil, err
	}
	creds := make([]webauthn.Credential, 0, len(keys))
	for _, k := range keys {
		var wc webauthn.Credential
		if jsonUnmarshal(k.Credential, &wc) == nil {
			creds = append(creds, wc)
		}
	}
	return keys, creds, nil
}

// ListPasskeys returns the caller's passkeys.
func (s *Service) ListPasskeys(ctx context.Context, who Who) ([]model.Passkey, error) {
	if err := person(who); err != nil {
		return nil, err
	}
	return s.store.Passkeys().ForAccount(ctx, who.Account.ID)
}

// BeginAddPasskey starts adding a passkey to the caller's account. It needs
// a fresh check, except from a recovery session (whose whole purpose it is)
// and from a pending session whose account has no proof yet.
func (s *Service) BeginAddPasskey(ctx context.Context, who Who) (Ceremony, error) {
	if err := person(who); err != nil {
		return Ceremony{}, err
	}
	acc := who.Account
	if who.Session.Recovery {
		pol, err := s.policy(ctx, s.store)
		if err != nil {
			return Ceremony{}, err
		}
		if !pol.passkeyAllowed(acc.Role) {
			// docs/AUTH.md section 6.2: where the role must use
			// company sign-in, a recovery session only links an identity.
			return Ceremony{}, model.ErrForbidden.With("Accounts on this server sign in through the company's sign-in. Link your identity instead.")
		}
	} else if err := s.mayEnrol(ctx, who, "passkey"); err != nil {
		return Ceremony{}, err
	}
	_, creds, err := s.credentials(ctx, acc.ID)
	if err != nil {
		return Ceremony{}, err
	}
	handle := acc.WebAuthnHandle
	if len(handle) == 0 {
		// An account made by company sign-in gets its user handle with
		// its first passkey.
		handle = secret.RandomBytes(32)
	}
	return s.beginCreateFor(ceremony{kind: "add", username: acc.Username, displayName: acc.DisplayName, handle: handle,
		account: acc.ID, sessionID: who.Session.ID}, creds)
}

// AddedPasskey is the result of adding a passkey.
type AddedPasskey struct {
	Passkey model.Passkey
	// SignIn is set when the call changed the session or made recovery
	// codes to show.
	SignIn *SignIn
}

// FinishAddPasskey stores the new passkey. From a pending session it counts
// as the proof and completes the sign-in; from a recovery session it
// replaces that session with an ordinary one, in the same transaction as
// the passkey. An account's first passkey also makes its recovery codes.
func (s *Service) FinishAddPasskey(ctx context.Context, who Who, in Finish, label string) (AddedPasskey, error) {
	if err := person(who); err != nil {
		return AddedPasskey{}, err
	}
	c, ok := s.takeCeremony(in.CeremonyID, "add")
	if !ok || c.account != who.Account.ID || c.sessionID != who.Session.ID {
		return AddedPasskey{}, model.ErrNotFound.With("That took too long. Start again.")
	}
	pk, err := s.verifyCreate(c, in)
	if err != nil {
		return AddedPasskey{}, err
	}
	acc := who.Account
	pk.AccountID = acc.ID
	var (
		out   AddedPasskey
		codes []string
		ended []store.SessionRef
	)
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		updated, err := tx.Accounts().Update(ctx, acc.ID, func(a *model.Account) error {
			if len(a.WebAuthnHandle) == 0 {
				a.WebAuthnHandle = c.handle
			} else if string(a.WebAuthnHandle) != string(c.handle) {
				return model.ErrConflict.With("Another passkey was added meanwhile. Start again.")
			}
			return nil
		})
		if err != nil {
			return err
		}
		acc = updated
		if err := tx.Passkeys().Add(ctx, &pk); err != nil {
			return err
		}
		if total, _, err := tx.Recovery().CountCodes(ctx, acc.ID); err != nil {
			return err
		} else if total == 0 {
			if codes, err = newRecoveryCodes(ctx, tx, acc.ID); err != nil {
				return err
			}
		}
		if err := s.audit(ctx, tx, acc.ID, "passkey.added", "account", string(acc.ID), map[string]any{"passkey": pk.Name}); err != nil {
			return err
		}
		switch {
		case who.Session.Recovery:
			// A token is never reused across a change of state.
			if err := tx.Sessions().Revoke(ctx, who.Session.ID, s.now()); err != nil {
				return err
			}
			ended = append(ended, store.SessionRef{ID: who.Session.ID, AccountID: acc.ID})
			ses, token, err := s.newSessionTx(ctx, tx, acc.ID, label)
			if err != nil {
				return err
			}
			out.SignIn = &SignIn{Session: ses, Account: acc, Token: token}
		case who.Session.PendingSecondFactor:
			if out.SignIn, err = s.proofGiven(ctx, tx, who, "passkey"); err != nil {
				return err
			}
			out.SignIn.Account = acc
		}
		return nil
	})
	if err != nil {
		return AddedPasskey{}, err
	}
	out.Passkey = pk
	if len(codes) > 0 {
		if out.SignIn == nil {
			out.SignIn = &SignIn{Session: who.Session, Account: acc}
		}
		out.SignIn.RecoveryCodes = codes
	}
	s.sessionsEnded(ctx, "replaced", ended...)
	if len(ended) > 0 {
		s.sessionStarted(out.SignIn.Session)
	}
	return out, nil
}

// ownPasskey finds one of the caller's passkeys; anybody else's is not found.
func (s *Service) ownPasskey(ctx context.Context, who Who, id model.PasskeyID) (model.Passkey, error) {
	pk, err := s.store.Passkeys().Get(ctx, id)
	if err != nil {
		return pk, err
	}
	if pk.AccountID != who.Account.ID {
		return pk, model.ErrNotFound
	}
	return pk, nil
}

// RenamePasskey renames a passkey, or notes that a later use asked it for a
// PRF value and got none.
func (s *Service) RenamePasskey(ctx context.Context, who Who, id model.PasskeyID, name *string, hasPRF *bool) (model.Passkey, error) {
	if err := complete(who); err != nil {
		return model.Passkey{}, err
	}
	pk, err := s.ownPasskey(ctx, who, id)
	if err != nil {
		return pk, err
	}
	if hasPRF != nil && *hasPRF {
		return pk, model.Invalid("has_prf", "has_prf can only be set to false.")
	}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" || len(n) > 100 {
			return pk, model.Invalid("name", "A passkey name is 1 to 100 characters.")
		}
		if err := s.store.Passkeys().Rename(ctx, id, n); err != nil {
			return pk, err
		}
	}
	if hasPRF != nil {
		if err := s.store.Passkeys().SetPRF(ctx, id, false, nil); err != nil {
			return pk, err
		}
	}
	return s.store.Passkeys().Get(ctx, id)
}

// DeletePasskey removes one of the caller's passkeys. The last passkey of
// an account with no linked company identity cannot be removed.
func (s *Service) DeletePasskey(ctx context.Context, who Who, id model.PasskeyID) error {
	if err := person(who); err != nil {
		return err
	}
	if who.Session.PendingSecondFactor {
		return model.ErrSecondFactorRequired
	}
	if !who.Session.Recovery {
		if err := s.fresh(who); err != nil {
			return err
		}
	}
	pk, err := s.ownPasskey(ctx, who, id)
	if err != nil {
		return err
	}
	if left, err := s.needsSecondFactor(ctx, who.Account.ID, "passkey"); err != nil {
		return err
	} else if left {
		return model.ErrConflict.With("This server needs a second sign-in step. Set up an authenticator app before removing this passkey.")
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		n, err := tx.Passkeys().Count(ctx, who.Account.ID)
		if err != nil {
			return err
		}
		if n <= 1 {
			linked, err := tx.Identities().CountForAccount(ctx, who.Account.ID)
			if err != nil {
				return err
			}
			if linked == 0 {
				return model.ErrConflict.With("This is your last passkey. Add another before removing it.")
			}
		}
		if err := tx.Passkeys().Delete(ctx, pk.ID); err != nil {
			return err
		}
		return s.audit(ctx, tx, who.Account.ID, "passkey.removed", "account", string(who.Account.ID), map[string]any{"passkey": pk.Name})
	})
	if err == nil && pk.CanUnlock {
		// Its locked copy went with it: the backup key must be replaced
		// (sealed.go).
		s.unlockingPasskeyRemoved(ctx, who.Account.ID)
	}
	return err
}

// BeginPasskeyCheck starts a passkey assertion limited to the caller's own
// passkeys: the second factor of a pending session, or a fresh check.
func (s *Service) BeginPasskeyCheck(ctx context.Context, who Who) (Ceremony, error) {
	if err := person(who); err != nil {
		return Ceremony{}, err
	}
	if who.Session.Recovery {
		return Ceremony{}, model.ErrForbidden.With("A recovery session can only set up a new passkey.")
	}
	acc := who.Account
	if who.Session.PendingSecondFactor {
		st, err := s.secondFactorState(ctx, acc.ID)
		if err != nil {
			return Ceremony{}, err
		}
		if !contains(st.Accepted, "passkey") {
			return Ceremony{}, model.ErrForbidden.With("This server does not accept a passkey as a second step.")
		}
	}
	_, creds, err := s.credentials(ctx, acc.ID)
	if err != nil {
		return Ceremony{}, err
	}
	if len(creds) == 0 {
		return Ceremony{}, model.ErrForbidden.With("This account has no passkey.")
	}
	user := waUser{handle: acc.WebAuthnHandle, name: acc.Username, displayName: acc.DisplayName, creds: creds}
	opts, session, err := s.wa.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return Ceremony{}, err
	}
	id := s.putCeremony(ceremony{kind: "check", session: *session, account: acc.ID, sessionID: who.Session.ID})
	return Ceremony{CeremonyID: id, PublicKey: opts.Response}, nil
}

// errCheckFailed is the one answer to every failed passkey check.
var errCheckFailed = model.ErrUnauthenticated.With("That passkey could not be verified for this account.")

// FinishPasskeyCheck finishes a passkey check. For a pending session it
// completes the sign-in; for a signed-in one it marks the session freshly
// checked.
func (s *Service) FinishPasskeyCheck(ctx context.Context, who Who, in Finish) (SignIn, error) {
	if err := person(who); err != nil {
		return SignIn{}, err
	}
	c, ok := s.takeCeremony(in.CeremonyID, "check")
	if !ok || c.account != who.Account.ID || c.sessionID != who.Session.ID {
		return SignIn{}, model.ErrNotFound.With("That took too long. Start again.")
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(in.Credential)
	if err != nil {
		return SignIn{}, model.BadRequest("The passkey response could not be read.")
	}
	acc := who.Account
	keys, creds, err := s.credentials(ctx, acc.ID)
	if err != nil {
		return SignIn{}, err
	}
	user := waUser{handle: acc.WebAuthnHandle, name: acc.Username, displayName: acc.DisplayName, creds: creds}
	cred, err := s.wa.ValidateLogin(user, c.session, parsed)
	if err != nil {
		s.log.Warn("passkey check failed", "err", err)
		return SignIn{}, s.wrongProof(ctx, who, errCheckFailed)
	}
	var used model.Passkey
	for _, k := range keys {
		if string(k.CredentialID) == string(cred.ID) {
			used = k
		}
	}
	if used.ID == "" {
		return SignIn{}, s.wrongProof(ctx, who, errCheckFailed)
	}
	if cred.Authenticator.CloneWarning {
		s.audit(ctx, s.store, acc.ID, "passkey.clone_suspected", "account", string(acc.ID), map[string]any{"passkey": used.Name})
		return SignIn{}, s.wrongProof(ctx, who, errCheckFailed)
	}
	record, err := jsonMarshal(cred)
	if err != nil {
		return SignIn{}, err
	}
	var out *SignIn
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Passkeys().Used(ctx, used.ID, record, s.now()); err != nil {
			return err
		}
		var err error
		out, err = s.proofGiven(ctx, tx, who, "passkey")
		return err
	})
	if err != nil {
		return SignIn{}, err
	}
	return *out, nil
}
