package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// Access keys (ruling R80, docs/AUTH.md section 5.6). A person who cannot
// use a passkey gets a long random code instead, made by the page, shown
// once and kept by them; nobody chooses it. The server never sees the key:
// the page sends a value derived from it, and the server keeps the SHA-256
// of that value. An access key is one way in alongside passkeys: it signs
// in, gives the fresh check and unlocks a locked session, and adding a
// passkey later works as it always does.

// SettingAllowAccessKeys is "Allow accounts without a passkey": while it is
// on (the default) new access keys may be made. Off, the ones that exist
// keep working and can be replaced, and no account gets a first one.
const SettingAllowAccessKeys = "auth.allow_access_keys"

// Limits on wrong access keys (docs/AUTH.md section 14). Signing in counts
// failures per address and per account; the fresh check per account.
const (
	accessKeyAddrTries   = 10
	accessKeyAddrWindow  = time.Hour
	accessKeyUserTries   = 30
	accessKeyUserWindow  = time.Hour
	accessKeyCheckTries  = 5
	accessKeyCheckWindow = 15 * time.Minute
)

// accessKeyLimits are the counters of wrong access keys, and the accounts
// whose reaching the limit was written to the audit log in the last window.
type accessKeyLimits struct {
	addr, user, check *ratelimit.Limiter
	audited           *vault[struct{}]
}

func newAccessKeyLimits() accessKeyLimits {
	return accessKeyLimits{
		addr:    ratelimit.New(ratelimit.Per(accessKeyAddrTries, accessKeyAddrWindow), accessKeyAddrTries),
		user:    ratelimit.New(ratelimit.Per(accessKeyUserTries, accessKeyUserWindow), accessKeyUserTries),
		check:   ratelimit.New(ratelimit.Per(accessKeyCheckTries, accessKeyCheckWindow), accessKeyCheckTries),
		audited: newVault[struct{}](accessKeyUserWindow),
	}
}

var (
	// errAccessKeyNoMatch is the one answer to every failed sign-in with an
	// access key, whether or not the username exists.
	errAccessKeyNoMatch = model.ErrUnauthenticated.With("That username and access key do not match.")
	// errAccessKeyWrong is the answer to a wrong key from a signed-in or
	// locked session.
	errAccessKeyWrong = model.ErrUnauthenticated.With("That access key is not this account's.")
	errAccessKeysOff  = model.ErrForbidden.With("This server no longer makes new access keys. Use a passkey.")
)

// dummyAccessKeyHash stands in for the stored hash of an account that does
// not exist or has no key, so every attempt costs the same work.
var dummyAccessKeyHash = make([]byte, 32)

// accessKeyAuth reads the sent value: 32 bytes in base64url.
func accessKeyAuth(b64 string) ([]byte, error) {
	raw, err := model.DecodeBytes(strings.TrimSpace(b64))
	if err != nil || len(raw) != 32 {
		return nil, model.Invalid("access_key_auth", "access_key_auth is the 32-byte value derived from the access key, in base64url.")
	}
	return raw, nil
}

// accessKeyHash reads the sent value and hashes it as it is stored.
func accessKeyHash(b64 string) ([]byte, error) {
	raw, err := accessKeyAuth(b64)
	if err != nil {
		return nil, err
	}
	return secret.HashAccessKeyAuth(raw)
}

// accessKeyUnlock reads the unlock public key the page derived from the
// access key, for sealed chats: 32 bytes in base64url, or nothing.
func accessKeyUnlock(b64 string) ([]byte, error) {
	b64 = strings.TrimSpace(b64)
	if b64 == "" {
		return nil, nil
	}
	raw, err := model.DecodeBytes(b64)
	if err != nil || len(raw) != 32 {
		return nil, model.Invalid("unlock_public", "unlock_public is the 32-byte X25519 public key derived from the access key, in base64url.")
	}
	return raw, nil
}

// dropAccessKeyCopy deletes the account's locked copy of kind access_key,
// if it has one, and says whether it did.
func dropAccessKeyCopy(ctx context.Context, tx store.Tx, account model.AccountID) (bool, error) {
	b, found, err := tx.Sealed().Backup(ctx, account)
	if err != nil || !found {
		return false, err
	}
	dropped := false
	for _, cp := range b.LockedCopies {
		if cp.Kind == model.CopyAccessKey {
			if err := tx.Sealed().DeleteLockedCopy(ctx, account, cp.ID); err != nil {
				return false, err
			}
			dropped = true
		}
	}
	return dropped, nil
}

// accessKeysAllowed reads "Allow accounts without a passkey".
func accessKeysAllowed(ctx context.Context, a store.Areas) (bool, error) {
	return store.SettingBool(ctx, a, SettingAllowAccessKeys, true)
}

// matches compares a sent value's hash with an account's stored one in
// constant time. An account with no key is compared with the dummy, and
// never matches.
func (s *Service) matches(ctx context.Context, account model.AccountID, hash []byte) (bool, error) {
	k, found, err := s.store.Passkeys().AccessKey(ctx, account)
	if err != nil {
		return false, err
	}
	want := dummyAccessKeyHash
	if found {
		want = k.AuthHash
	}
	same := subtle.ConstantTimeCompare(hash, want) == 1
	return found && same, nil
}

// SetupWithAccessKey makes the owner with an access key in place of a
// passkey, for whoever holds the setup link.
func (s *Service) SetupWithAccessKey(ctx context.Context, addr, token, username, displayName, auth, unlockPublic, label string) (SignIn, error) {
	hash, err := accessKeyHash(auth)
	if err != nil {
		return SignIn{}, err
	}
	unlock, err := accessKeyUnlock(unlockPublic)
	if err != nil {
		return SignIn{}, err
	}
	c, err := s.prepareSetup(ctx, addr, token, username, displayName)
	if err != nil {
		return SignIn{}, err
	}
	if ok, err := accessKeysAllowed(ctx, s.store); err != nil {
		return SignIn{}, err
	} else if !ok {
		return SignIn{}, errAccessKeysOff
	}
	return s.setupTx(ctx, c, firstWay{accessKey: hash, unlockPublic: unlock}, label)
}

// RegisterWithAccessKey makes an account, or claims an imported one, with
// an access key in place of a passkey.
func (s *Service) RegisterWithAccessKey(ctx context.Context, addr, inviteCode, username, displayName, note, auth, unlockPublic, label string) (SignIn, error) {
	hash, err := accessKeyHash(auth)
	if err != nil {
		return SignIn{}, err
	}
	unlock, err := accessKeyUnlock(unlockPublic)
	if err != nil {
		return SignIn{}, err
	}
	if ok, err := accessKeysAllowed(ctx, s.store); err != nil {
		return SignIn{}, err
	} else if !ok {
		return SignIn{}, errAccessKeysOff
	}
	c, err := s.prepareRegistration(ctx, addr, inviteCode, username, displayName, note)
	if err != nil {
		return SignIn{}, err
	}
	if c.inviteID == "" {
		// With no passkey ceremony to slow it, open registration is held
		// to the day's count of new accounts per address.
		if err := limited(s.limitOpenAccounts, addr); err != nil {
			return SignIn{}, err
		}
	}
	return s.registerTx(ctx, c, firstWay{accessKey: hash, unlockPublic: unlock}, label)
}

// SignInWithAccessKey signs in with a username and an access key. Any
// failure gives the same answer after the same work.
func (s *Service) SignInWithAccessKey(ctx context.Context, addr, username, auth, label string) (SignIn, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return SignIn{}, model.BadRequest("username and access_key_auth are needed.")
	}
	hash, err := accessKeyHash(auth)
	if err != nil {
		return SignIn{}, err
	}
	lim := s.accessKeyLimits
	if err := blocked(lim.addr, addr); err != nil {
		return SignIn{}, err
	}
	if err := blocked(lim.user, username); err != nil {
		return SignIn{}, err
	}
	acc, err := s.store.Accounts().ByUsername(ctx, username)
	known := err == nil
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return SignIn{}, err
	}
	target := dummyAccount
	if known {
		target = acc.ID
	}
	ok, err := s.matches(ctx, target, hash)
	if err != nil {
		return SignIn{}, err
	}
	if ok && known && !acc.Active() {
		// The key is right: an applicant is told where their request
		// stands (docs/FEATURES.md section 31).
		if err := s.application(ctx, acc); err != nil {
			return SignIn{}, err
		}
	}
	if !ok || !known || !acc.Active() {
		failed(lim.addr, addr)
		failed(lim.user, username)
		s.log.Warn("sign-in failed", "method", "access_key", "address", addr)
		if known && blocked(lim.user, username) != nil {
			// Written when the account's limit is reached, and at most
			// once a window: an outsider must not be able to fill the
			// log.
			if _, done := lim.audited.get(string(acc.ID)); !done {
				lim.audited.put(string(acc.ID), struct{}{})
				s.audit(ctx, s.store, "", "access_key.limit_reached", "account", string(acc.ID), nil)
			}
		}
		return SignIn{}, errAccessKeyNoMatch
	}
	pol, err := s.policy(ctx, s.store)
	if err != nil {
		return SignIn{}, err
	}
	if !pol.passkeyAllowed(acc.Role) {
		return SignIn{}, model.ErrForbidden.With("Sign in through the company's sign-in.")
	}
	out := SignIn{Account: acc}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Passkeys().AccessKeyUsed(ctx, acc.ID, s.now()); err != nil {
			return err
		}
		var err error
		out.Session, out.Token, err = s.newSessionByTx(ctx, tx, acc.ID, label, signedInAccessKey)
		return err
	})
	if err == nil {
		s.sessionStarted(out.Session)
	}
	return out, err
}

// AccessKeyState is what a person sees of their own access key.
type AccessKeyState struct {
	Key *model.AccessKey
	// MayCreate is true when the server makes new access keys, or the
	// account has one to replace.
	MayCreate bool
}

// AccessKey says whether the caller has an access key. Never the key.
func (s *Service) AccessKey(ctx context.Context, who Who) (AccessKeyState, error) {
	if err := person(who); err != nil {
		return AccessKeyState{}, err
	}
	k, found, err := s.store.Passkeys().AccessKey(ctx, who.Account.ID)
	if err != nil {
		return AccessKeyState{}, err
	}
	allowed, err := accessKeysAllowed(ctx, s.store)
	if err != nil {
		return AccessKeyState{}, err
	}
	out := AccessKeyState{MayCreate: allowed || found}
	if found {
		out.Key = &model.AccessKey{CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt, UnlockPublic: k.UnlockPublic}
	}
	return out, nil
}

// SetAccessKey gives the caller a new access key, replacing the one it had:
// the old one stops working at once. It needs a fresh check, except from a
// recovery session, whose purpose it is; that session is then replaced by
// an ordinary one, in the same transaction. The old key's locked copy of
// the sealed-chat backup key goes with it (docs/SEALED.md section 13.6).
func (s *Service) SetAccessKey(ctx context.Context, who Who, auth, unlockPublic, label string) (model.AccessKey, *SignIn, error) {
	if err := person(who); err != nil {
		return model.AccessKey{}, nil, err
	}
	if who.Session.PendingSecondFactor {
		return model.AccessKey{}, nil, model.ErrSecondFactorRequired
	}
	acc := who.Account
	if who.Session.Recovery {
		pol, err := s.policy(ctx, s.store)
		if err != nil {
			return model.AccessKey{}, nil, err
		}
		if !pol.passkeyAllowed(acc.Role) {
			return model.AccessKey{}, nil, model.ErrForbidden.With("Accounts on this server sign in through the company's sign-in. Link your identity instead.")
		}
	} else if err := s.fresh(who); err != nil {
		return model.AccessKey{}, nil, err
	}
	hash, err := accessKeyHash(auth)
	if err != nil {
		return model.AccessKey{}, nil, err
	}
	unlock, err := accessKeyUnlock(unlockPublic)
	if err != nil {
		return model.AccessKey{}, nil, err
	}
	allowed, err := accessKeysAllowed(ctx, s.store)
	if err != nil {
		return model.AccessKey{}, nil, err
	}
	now := s.now()
	var (
		in      *SignIn
		ended   []store.SessionRef
		dropped bool
	)
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		_, had, err := tx.Passkeys().AccessKey(ctx, acc.ID)
		if err != nil {
			return err
		}
		if !had && !allowed {
			return errAccessKeysOff
		}
		if err := tx.Passkeys().SetAccessKey(ctx, acc.ID, hash, unlock, now); err != nil {
			return err
		}
		if dropped, err = dropAccessKeyCopy(ctx, tx, acc.ID); err != nil {
			return err
		}
		action := "access_key.created"
		if had {
			action = "access_key.replaced"
		}
		if err := s.audit(ctx, tx, acc.ID, action, "account", string(acc.ID), nil); err != nil {
			return err
		}
		if who.Session.Recovery {
			// A token is never reused across a change of state.
			if err := tx.Sessions().Revoke(ctx, who.Session.ID, now); err != nil {
				return err
			}
			ended = append(ended, store.SessionRef{ID: who.Session.ID, AccountID: acc.ID})
			ses, token, err := s.newSessionByTx(ctx, tx, acc.ID, label, signedInAccessKey)
			if err != nil {
				return err
			}
			in = &SignIn{Session: ses, Account: acc, Token: token}
		}
		return nil
	})
	if err != nil {
		return model.AccessKey{}, nil, err
	}
	s.sessionsEnded(ctx, "replaced", ended...)
	if in != nil {
		s.sessionStarted(in.Session)
	}
	s.accessKeyChanged(ctx, acc.ID, dropped)
	return model.AccessKey{CreatedAt: now, UnlockPublic: unlock}, in, nil
}

// RemoveAccessKey removes the caller's access key. An account always keeps
// a way in: it must have a passkey or a linked company identity.
func (s *Service) RemoveAccessKey(ctx context.Context, who Who) error {
	if err := s.fresh(who); err != nil {
		return err
	}
	dropped := false
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		n, err := tx.Passkeys().Count(ctx, who.Account.ID)
		if err != nil {
			return err
		}
		if n == 0 {
			linked, err := tx.Identities().CountForAccount(ctx, who.Account.ID)
			if err != nil {
				return err
			}
			if linked == 0 {
				return model.ErrConflict.With("Your access key is your only way in. Add a passkey before removing it.")
			}
		}
		removed, err := tx.Passkeys().DeleteAccessKey(ctx, who.Account.ID)
		if err != nil {
			return err
		}
		if !removed {
			return model.ErrNotFound.With("This account has no access key.")
		}
		if dropped, err = dropAccessKeyCopy(ctx, tx, who.Account.ID); err != nil {
			return err
		}
		return s.audit(ctx, tx, who.Account.ID, "access_key.removed", "account", string(who.Account.ID), nil)
	})
	if err != nil {
		return err
	}
	s.accessKeyChanged(ctx, who.Account.ID, dropped)
	return nil
}

// CheckAccessKey takes the caller's access key as a fresh check. It is not
// a second factor: a pending session is refused.
func (s *Service) CheckAccessKey(ctx context.Context, who Who, auth string) (SignIn, error) {
	if err := complete(who); err != nil {
		return SignIn{}, err
	}
	hash, err := accessKeyHash(auth)
	if err != nil {
		return SignIn{}, err
	}
	key := string(who.Account.ID)
	if err := blocked(s.accessKeyLimits.check, key); err != nil {
		return SignIn{}, err
	}
	ok, err := s.matches(ctx, who.Account.ID, hash)
	if err != nil {
		return SignIn{}, err
	}
	if !ok {
		failed(s.accessKeyLimits.check, key)
		return SignIn{}, errAccessKeyWrong
	}
	var out *SignIn
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Passkeys().AccessKeyUsed(ctx, who.Account.ID, s.now()); err != nil {
			return err
		}
		var err error
		out, err = s.proofGiven(ctx, tx, who, signedInAccessKey)
		return err
	})
	if err != nil {
		return SignIn{}, err
	}
	return *out, nil
}

// FinishSessionUnlockWithAccessKey unlocks the session an unlock ceremony
// was begun for, with the account's access key. Like the passkey's, it
// needs no credential: the ceremony names the session, and only the
// account's key answers it. A wrong key leaves the ceremony in place, so
// the person can correct it, and counts against the account together with
// wrong passkey assertions.
func (s *Service) FinishSessionUnlockWithAccessKey(ctx context.Context, ceremonyID, auth string) error {
	hash, err := accessKeyHash(auth)
	if err != nil {
		return err
	}
	st := s.lockInit()
	c, ok := s.peekCeremony(ceremonyID, "unlock")
	if !ok {
		return model.ErrNotFound.With("That took too long. Press Unlock again.")
	}
	key := string(c.account)
	if err := blocked(st.wrong, key); err != nil {
		return err
	}
	acc, err := s.store.Accounts().Get(ctx, c.account)
	if err != nil || !acc.Active() {
		return model.ErrUnauthenticated
	}
	match, err := s.matches(ctx, acc.ID, hash)
	if err != nil {
		return err
	}
	if !match {
		failed(st.wrong, key)
		return errAccessKeyWrong
	}
	if _, ok := s.takeCeremony(ceremonyID, "unlock"); !ok {
		// Used meanwhile, by the passkey or another try.
		return model.ErrNotFound.With("That took too long. Press Unlock again.")
	}
	st.options.drop(ceremonyID)
	ses, err := s.store.Sessions().Get(ctx, c.sessionID)
	if err != nil {
		return err
	}
	now := s.now()
	if !ses.RevokedAt.IsZero() || (!ses.ExpiresAt.IsZero() && !ses.ExpiresAt.After(now)) {
		return model.ErrUnauthenticated.With("This session has ended. Sign in again.")
	}
	if err := s.store.Passkeys().AccessKeyUsed(ctx, acc.ID, now); err != nil {
		return err
	}
	if _, err := s.store.Sessions().SetLocked(ctx, ses.ID, 0); err != nil {
		return err
	}
	return s.store.Sessions().SetFresh(ctx, ses.ID, now)
}
