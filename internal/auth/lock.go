package auth

import (
	"context"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/ratelimit"
)

// The lock (ruling R75, docs/AUTH.md section 10.6). A person at a shared or
// unattended computer or phone locks their session instead of signing out,
// which would throw away the device's sealed-chat set-up. While the session
// is locked the API answers it only to unlock or sign out, its live
// connections are closed, and its wake-ups are fetched as "locked". It is
// unlocked by a passkey assertion of the account's own, with user
// verification, verified here exactly as a passkey check is.

const (
	// closeLocked closes the live connections of a session that was just
	// locked (docs/EVENTS.md section 6). The client shows its lock screen
	// and does not reconnect until it is unlocked.
	closeLocked = 4002
	// Wrong unlocking assertions: five per account in fifteen minutes,
	// then refusals until the window has passed (docs/AUTH.md section 14).
	unlockTries  = 5
	unlockWindow = 15 * time.Minute
)

// lockState is what the lock keeps in memory: the ways of each unlock
// ceremony, so the page an app opens can fetch them by the ceremony's
// identifier, and the count of wrong assertions and access keys.
type lockState struct {
	once    sync.Once
	options *vault[UnlockOptions]
	wrong   *ratelimit.Limiter
}

// UnlockOptions are the ways an unlock ceremony may be answered: the
// passkey request options, absent when the account has no passkey, and
// whether the account has an access key.
type UnlockOptions struct {
	PublicKey any  `json:"public_key,omitempty"`
	AccessKey bool `json:"access_key"`
}

func (s *Service) lockInit() *lockState {
	s.lock.once.Do(func() {
		s.lock.options = newVault[UnlockOptions](ceremonyLife)
		s.lock.wrong = ratelimit.New(ratelimit.Per(unlockTries, unlockWindow), unlockTries)
	})
	return &s.lock
}

// SessionUnlock is the start of unlocking (schema SessionUnlock): the
// passkey request options and whether the access key unlocks too, and the
// address of the server's page that runs them, for an app, which cannot use
// a passkey itself (ruling R43).
type SessionUnlock struct {
	CeremonyID string `json:"ceremony_id"`
	UnlockOptions
	UnlockURL string `json:"unlock_url"`
}

// LockSession locks the caller's own session. Locking a locked session
// changes nothing. Its live connections are closed with 4002, so every tab
// or window on the session shows its lock screen.
func (s *Service) LockSession(ctx context.Context, who Who) error {
	if err := complete(who); err != nil {
		return err
	}
	changed, err := s.store.Sessions().SetLocked(ctx, who.Session.ID, s.now())
	if err != nil {
		return err
	}
	if changed {
		s.hub.CloseSession(who.Account.ID, who.Session.ID, closeLocked, "locked")
	}
	return nil
}

// BeginSessionUnlock starts the passkey assertion that unlocks the caller's
// session, limited to the account's own passkeys with user verification
// required. It is the passkey check's ceremony under another kind, so a
// check cannot be spent as an unlock nor the other way round.
func (s *Service) BeginSessionUnlock(ctx context.Context, who Who) (SessionUnlock, error) {
	if err := complete(who); err != nil {
		return SessionUnlock{}, err
	}
	if who.Session.LockedAt.IsZero() {
		return SessionUnlock{}, model.ErrConflict.With("This session is not locked.")
	}
	st := s.lockInit()
	if err := blocked(st.wrong, string(who.Account.ID)); err != nil {
		return SessionUnlock{}, err
	}
	acc := who.Account
	_, creds, err := s.credentials(ctx, acc.ID)
	if err != nil {
		return SessionUnlock{}, err
	}
	_, hasKey, err := s.store.Passkeys().AccessKey(ctx, acc.ID)
	if err != nil {
		return SessionUnlock{}, err
	}
	if len(creds) == 0 && !hasKey {
		return SessionUnlock{}, model.ErrForbidden.With("This account has no passkey or access key to unlock with. Sign out, and sign in again.")
	}
	c := ceremony{kind: "unlock", account: acc.ID, sessionID: who.Session.ID}
	ways := UnlockOptions{AccessKey: hasKey}
	if len(creds) > 0 {
		user := waUser{handle: acc.WebAuthnHandle, name: acc.Username, displayName: acc.DisplayName, creds: creds}
		opts, session, err := s.wa.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationRequired))
		if err != nil {
			return SessionUnlock{}, err
		}
		c.session, ways.PublicKey = *session, opts.Response
	}
	id := s.putCeremony(c)
	st.options.put(id, ways)
	return SessionUnlock{
		CeremonyID: id, UnlockOptions: ways,
		// The identifier travels after the #, which a browser never
		// sends to the server or in a Referer.
		UnlockURL: s.cfg.Origin + "/locked/#ceremony=" + id,
	}, nil
}

// SessionUnlockOptions gives the ways of an unlock ceremony to the page an
// app opened. Knowing the identifier is not enough to unlock: the answer
// must be an assertion by one of the account's passkeys, or its access key.
func (s *Service) SessionUnlockOptions(id string) (UnlockOptions, error) {
	if opts, ok := s.lockInit().options.get(id); ok {
		return opts, nil
	}
	return UnlockOptions{}, model.ErrNotFound.With("That took too long. Go back to the app and press Unlock again.")
}

// FinishSessionUnlock verifies the assertion and unlocks the session the
// ceremony was begun for. It needs no credential of its own, because the
// app's page runs in the system browser, which holds none: the ceremony
// names the session, and only a passkey of that account, with user
// verification, can answer it. Every failure counts against the account.
func (s *Service) FinishSessionUnlock(ctx context.Context, in Finish) error {
	st := s.lockInit()
	c, ok := s.takeCeremony(in.CeremonyID, "unlock")
	st.options.drop(in.CeremonyID)
	if !ok {
		return model.ErrNotFound.With("That took too long. Press Unlock again.")
	}
	key := string(c.account)
	if err := blocked(st.wrong, key); err != nil {
		return err
	}
	fail := func() error {
		failed(st.wrong, key)
		return errCheckFailed
	}
	acc, err := s.store.Accounts().Get(ctx, c.account)
	if err != nil || !acc.Active() {
		return model.ErrUnauthenticated
	}
	if c.session.Challenge == "" {
		// Begun for an account with no passkey: only its access key
		// answers it.
		return fail()
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(in.Credential)
	if err != nil {
		return model.BadRequest("The passkey response could not be read.")
	}
	keys, creds, err := s.credentials(ctx, acc.ID)
	if err != nil {
		return err
	}
	user := waUser{handle: acc.WebAuthnHandle, name: acc.Username, displayName: acc.DisplayName, creds: creds}
	cred, err := s.wa.ValidateLogin(user, c.session, parsed)
	if err != nil {
		s.log.Warn("an unlocking passkey could not be verified", "err", err)
		return fail()
	}
	// The library checks the user-verified flag against the ceremony's
	// requirement; it is checked again here, because nothing weaker may
	// unlock.
	if !parsed.Response.AuthenticatorData.Flags.UserVerified() {
		return fail()
	}
	var used model.Passkey
	for _, k := range keys {
		if string(k.CredentialID) == string(cred.ID) {
			used = k
		}
	}
	if used.ID == "" {
		return fail()
	}
	if cred.Authenticator.CloneWarning {
		s.audit(ctx, s.store, acc.ID, "passkey.clone_suspected", "account", string(acc.ID), map[string]any{"passkey": used.Name})
		return fail()
	}
	record, err := jsonMarshal(cred)
	if err != nil {
		return err
	}
	ses, err := s.store.Sessions().Get(ctx, c.sessionID)
	if err != nil {
		return err
	}
	now := s.now()
	if !ses.RevokedAt.IsZero() || (!ses.ExpiresAt.IsZero() && !ses.ExpiresAt.After(now)) {
		return model.ErrUnauthenticated.With("This session has ended. Sign in again.")
	}
	if err := s.store.Passkeys().Used(ctx, used.ID, record, now); err != nil {
		return err
	}
	if _, err := s.store.Sessions().SetLocked(ctx, ses.ID, 0); err != nil {
		return err
	}
	// The unlock is a passkey check like any other: the session counts as
	// freshly checked for ten minutes.
	return s.store.Sessions().SetFresh(ctx, ses.ID, now)
}
