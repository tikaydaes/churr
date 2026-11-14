package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// The one combination every authenticator app accepts (docs/AUTH.md
// section 9.4): RFC 6238 with HMAC-SHA-1, six digits, a 30-second step.
const (
	totpPeriod     = 30
	totpDigits     = 6
	totpSecretSize = 20
)

var totpBase32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// totpCode is the code for one time step (RFC 4226 section 5.3).
func totpCode(key []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, n%1000000)
}

// totpStep is the time step a moment falls in.
func totpStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// totpMatch compares a typed code with the codes of the current step and
// the one before and after it, in constant time, and returns the step that
// matched. Every candidate is always computed and compared.
func totpMatch(key []byte, typed string, now time.Time) (step int64, ok bool) {
	typed = strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, typed)
	cur := totpStep(now)
	for _, st := range []int64{cur - 1, cur, cur + 1} {
		if subtle.ConstantTimeCompare([]byte(totpCode(key, st)), []byte(typed)) == 1 && !ok {
			step, ok = st, true
		}
	}
	return step, ok
}

// totpPlace is where an account's authenticator secret is sealed.
func (s *Service) sealTotp(account model.AccountID, key []byte) ([]byte, error) {
	return s.keys.Seal(key, "totp", "secret", string(account))
}

func (s *Service) unsealTotp(t store.Totp) ([]byte, error) {
	return s.keys.Unseal(t.Secret, "totp", "secret", string(t.AccountID))
}

// TotpState says whether the caller has a confirmed authenticator app.
func (s *Service) TotpState(ctx context.Context, who Who) (model.Totp, error) {
	if err := person(who); err != nil {
		return model.Totp{}, err
	}
	t, found, err := s.store.Identities().GetTotp(ctx, who.Account.ID)
	if err != nil || !found || t.ConfirmedAt.IsZero() {
		return model.Totp{}, err
	}
	return model.Totp{Enabled: true, ConfirmedAt: t.ConfirmedAt}, nil
}

// TotpEnrolment is the secret shown once when enrolment begins.
type TotpEnrolment struct {
	Secret     string `json:"secret"`
	OtpauthURL string `json:"otpauth_url"`
}

// mayEnrol decides whether the caller may set up a first or new proof of the
// given kind: a pending session only when the account has no proof yet and
// the admin accepts this kind; anyone else after a fresh check.
func (s *Service) mayEnrol(ctx context.Context, who Who, proof string) error {
	if err := person(who); err != nil {
		return err
	}
	if who.Session.PendingSecondFactor {
		st, err := s.secondFactorState(ctx, who.Account.ID)
		if err != nil {
			return err
		}
		if !contains(st.Accepted, proof) {
			return model.ErrForbidden.With("This server does not accept that as a second step.")
		}
		if len(st.SetUp) > 0 {
			return model.ErrForbidden.With("Use the second step this account already has.")
		}
		return nil
	}
	return s.fresh(who)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// BeginTotp starts setting up an authenticator app, replacing any earlier
// enrolment. The secret is shown once.
func (s *Service) BeginTotp(ctx context.Context, who Who) (TotpEnrolment, error) {
	if who.Session.Recovery {
		return TotpEnrolment{}, model.ErrForbidden.With("A recovery session can only set up a new passkey.")
	}
	if err := s.mayEnrol(ctx, who, "totp"); err != nil {
		return TotpEnrolment{}, err
	}
	key := secret.RandomBytes(totpSecretSize)
	sealed, err := s.sealTotp(who.Account.ID, key)
	if err != nil {
		return TotpEnrolment{}, err
	}
	if err := s.store.Identities().PutTotp(ctx, who.Account.ID, sealed); err != nil {
		return TotpEnrolment{}, err
	}
	name, err := store.SettingString(ctx, s.store, store.SettingServerName, s.cfg.Hostname)
	if err != nil {
		return TotpEnrolment{}, err
	}
	text := totpBase32.EncodeToString(key)
	label := url.PathEscape(name + ":" + who.Account.Username)
	q := "secret=" + text + "&issuer=" + url.QueryEscape(name) + "&algorithm=SHA1&digits=6&period=30"
	return TotpEnrolment{Secret: text, OtpauthURL: "otpauth://totp/" + label + "?" + q}, nil
}

// errWrongCode is the answer to a code that is wrong, old or already used.
var errWrongCode = model.Invalid("code", "That code is not right. Wait for the next one and try again.")

// ConfirmTotp finishes enrolment with the app's first code. From a pending
// session it also counts as the proof and completes the sign-in.
func (s *Service) ConfirmTotp(ctx context.Context, who Who, code string) (model.Totp, *SignIn, error) {
	if err := person(who); err != nil {
		return model.Totp{}, nil, err
	}
	acc := who.Account.ID
	if err := blocked(s.limitTotp, string(acc)); err != nil {
		return model.Totp{}, nil, err
	}
	t, found, err := s.store.Identities().GetTotp(ctx, acc)
	if err != nil {
		return model.Totp{}, nil, err
	}
	if !found || !t.ConfirmedAt.IsZero() {
		return model.Totp{}, nil, model.ErrNotFound.With("No authenticator app is waiting to be confirmed.")
	}
	key, err := s.unsealTotp(t)
	if err != nil {
		return model.Totp{}, nil, err
	}
	now := s.now()
	step, ok := totpMatch(key, code, now.Time())
	if !ok {
		failed(s.limitTotp, string(acc))
		return model.Totp{}, nil, s.wrongProof(ctx, who, errWrongCode)
	}
	var in *SignIn
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		ok, err := tx.Identities().ConfirmTotp(ctx, acc, now, step)
		if err != nil {
			return err
		}
		if !ok {
			return model.ErrNotFound.With("No authenticator app is waiting to be confirmed.")
		}
		if err := s.audit(ctx, tx, acc, "totp.enrolled", "account", string(acc), nil); err != nil {
			return err
		}
		in, err = s.proofGiven(ctx, tx, who, "totp")
		return err
	})
	if err != nil {
		return model.Totp{}, nil, err
	}
	return model.Totp{Enabled: true, ConfirmedAt: now}, in, nil
}

// CheckTotp takes a current code from the caller's confirmed authenticator
// app: as the second factor of a pending session, or as a fresh check. A
// code works once.
func (s *Service) CheckTotp(ctx context.Context, who Who, code string) (SignIn, error) {
	if err := person(who); err != nil {
		return SignIn{}, err
	}
	if who.Session.Recovery {
		return SignIn{}, model.ErrForbidden.With("A recovery session can only set up a new passkey.")
	}
	acc := who.Account.ID
	if who.Session.PendingSecondFactor {
		st, err := s.secondFactorState(ctx, acc)
		if err != nil {
			return SignIn{}, err
		}
		if !contains(st.Accepted, "totp") {
			return SignIn{}, model.ErrForbidden.With("This server does not accept an authenticator app as a second step.")
		}
	}
	if err := blocked(s.limitTotp, string(acc)); err != nil {
		return SignIn{}, err
	}
	t, found, err := s.store.Identities().GetTotp(ctx, acc)
	if err != nil {
		return SignIn{}, err
	}
	if !found || t.ConfirmedAt.IsZero() {
		return SignIn{}, model.ErrForbidden.With("This account has no authenticator app.")
	}
	key, err := s.unsealTotp(t)
	if err != nil {
		return SignIn{}, err
	}
	step, ok := totpMatch(key, code, s.now().Time())
	var in *SignIn
	if ok {
		err = s.store.Tx(ctx, func(tx store.Tx) error {
			// Accepted only if its step is later than the last one
			// used, so a code seen over a shoulder is dead once its
			// owner has used it or a later one.
			if ok, err = tx.Identities().AdvanceTotp(ctx, acc, step); err != nil || !ok {
				return err
			}
			in, err = s.proofGiven(ctx, tx, who, "totp")
			return err
		})
		if err != nil {
			return SignIn{}, err
		}
	}
	if !ok {
		failed(s.limitTotp, string(acc))
		return SignIn{}, s.wrongProof(ctx, who, errWrongCode)
	}
	return *in, nil
}

// needsSecondFactor reports whether removing a proof would leave an account
// that signs in through a provider, on a server that requires a second
// factor, without any proof it could give itself.
func (s *Service) needsSecondFactor(ctx context.Context, account model.AccountID, losing string) (bool, error) {
	pol, err := s.policy(ctx, s.store)
	if err != nil || !pol.SecondRequired {
		return false, err
	}
	n, err := s.store.Identities().CountForAccount(ctx, account)
	if err != nil || n == 0 {
		return false, err
	}
	keys, err := s.store.Passkeys().Count(ctx, account)
	if err != nil {
		return false, err
	}
	t, found, err := s.store.Identities().GetTotp(ctx, account)
	if err != nil {
		return false, err
	}
	hasTotp := found && !t.ConfirmedAt.IsZero()
	switch losing {
	case "totp":
		hasTotp = false
	case "passkey":
		keys--
	}
	return !(pol.Accepted["passkey"] && keys > 0) && !(pol.Accepted["totp"] && hasTotp), nil
}

// RemoveTotp removes the caller's authenticator app.
func (s *Service) RemoveTotp(ctx context.Context, who Who) error {
	if err := s.fresh(who); err != nil {
		return err
	}
	if left, err := s.needsSecondFactor(ctx, who.Account.ID, "totp"); err != nil {
		return err
	} else if left {
		return model.ErrConflict.With("This server needs a second sign-in step. Add a passkey before removing the authenticator app.")
	}
	return s.store.Tx(ctx, func(tx store.Tx) error {
		ok, err := tx.Identities().DeleteTotp(ctx, who.Account.ID)
		if err != nil || !ok {
			return err
		}
		return s.audit(ctx, tx, who.Account.ID, "totp.removed", "account", string(who.Account.ID), map[string]any{"by": "self"})
	})
}

// AdminRemoveTotp resets an account's authenticator app. The person enrols
// again at their next sign-in, on the same trust as a first enrolment.
func (s *Service) AdminRemoveTotp(ctx context.Context, who Who, target model.AccountID) error {
	if err := complete(who); err != nil {
		return err
	}
	if err := s.admin(ctx, who, perm.UseAdminConsole); err != nil {
		return err
	}
	acc, err := s.store.Accounts().Get(ctx, target)
	if err != nil {
		return err
	}
	if err := perm.CheckOnAccount(who.Account, perm.ResetTotp, acc); err != nil {
		return err
	}
	return s.store.Tx(ctx, func(tx store.Tx) error {
		if _, err := tx.Identities().DeleteTotp(ctx, target); err != nil {
			return err
		}
		return s.audit(ctx, tx, who.Account.ID, "totp.reset", "account", string(target), map[string]any{"by": who.Account.Username})
	})
}
