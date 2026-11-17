package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// More lifetimes from docs/AUTH.md.
const (
	recoveryLife     = time.Hour
	pendingLife      = 10 * time.Minute
	waitingLife      = 10 * time.Minute
	ssoSeconds       = 24 * 3600
	adminLinkLife    = 24 * time.Hour
	cliLinkLife      = time.Hour
	codeLife         = 2 * time.Minute
	accessLife       = time.Hour
	refreshTolerance = 60 * time.Second
	clockAllowance   = 60 * time.Second
	personalIdle     = 30 * 24 * time.Hour
	maxWrongProofs   = 5
	maxInMemory      = 10000
)

// vault holds short-lived sign-in state in memory. Everything in it has an
// expiry and the whole has a size cap; past the cap the oldest entries go
// first (docs/AUTH.md section 1).
type vault[T any] struct {
	mu   sync.Mutex
	m    map[string]vaultEntry[T]
	life time.Duration
}

type vaultEntry[T any] struct {
	v       T
	expires time.Time
}

func newVault[T any](life time.Duration) *vault[T] {
	return &vault[T]{m: map[string]vaultEntry[T]{}, life: life}
}

// put stores v under key for the vault's lifetime.
func (v *vault[T]) put(key string, val T) { v.putUntil(key, val, time.Now().Add(v.life)) }

func (v *vault[T]) putUntil(key string, val T, expires time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := time.Now()
	for k, e := range v.m {
		if now.After(e.expires) {
			delete(v.m, k)
		}
	}
	for len(v.m) >= maxInMemory {
		var oldest string
		for k, e := range v.m {
			if oldest == "" || e.expires.Before(v.m[oldest].expires) {
				oldest = k
			}
		}
		delete(v.m, oldest)
	}
	v.m[key] = vaultEntry[T]{val, expires}
}

// get returns the value under key if it has not expired.
func (v *vault[T]) get(key string) (T, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	e, ok := v.m[key]
	if !ok || time.Now().After(e.expires) {
		var zero T
		return zero, false
	}
	return e.v, true
}

// take is get that also removes the entry, so it can be used once.
func (v *vault[T]) take(key string) (T, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	e, ok := v.m[key]
	delete(v.m, key)
	if !ok || time.Now().After(e.expires) {
		var zero T
		return zero, false
	}
	return e.v, true
}

func (v *vault[T]) drop(key string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.m, key)
}

// state is what sign-in keeps beyond the passkey ceremonies of auth.go.
type state struct {
	keys        *secret.Keys
	httpc       *http.Client
	testStorage func(ctx context.Context, settings map[string]json.RawMessage) error
	blobs       blob.Blobs
	intLimit    IntegrationLimiter

	// Company sign-in requests waiting for their answer, registrations
	// waiting for a username, and SAML assertion identifiers already seen.
	waiting       *vault[waiting]
	registrations *vault[registration]
	assertions    *vault[struct{}]
	// What an app session inherits from the browser session that approved
	// or confirmed it, by the hex hash of the one-time code.
	grants *vault[grant]
	// Who started each pairing code.
	pairings *vault[model.AccountID]
	// Wrong proofs given by each pending session.
	wrongProofs *vault[int]
	// The lock's unlock ceremonies and wrong assertions (lock.go).
	lock lockState

	oidcMu    sync.Mutex
	oidcCache map[string]*oidcEntry

	// Limits of docs/AUTH.md section 14 that auth.go does not already have.
	limitRecoveryAddr, limitRecoveryUser, limitRecoveryLink, limitSsoStart, limitSsoCallback,
	limitTotp, limitAuthorize, limitToken, limitPairPoll, limitWellKnown, limitOpenAccounts *ratelimit.Limiter
}

func (s *Service) initState(d Deps) {
	s.keys, s.httpc, s.testStorage, s.blobs, s.intLimit = d.Keys, d.HTTP, d.TestStorage, d.Blobs, d.Integrations
	if s.httpc == nil {
		s.httpc = &http.Client{Timeout: 10 * time.Second}
	}
	s.waiting = newVault[waiting](waitingLife)
	s.registrations = newVault[registration](waitingLife)
	s.assertions = newVault[struct{}](waitingLife)
	s.grants = newVault[grant](codeLife)
	s.pairings = newVault[model.AccountID](codeLife)
	s.wrongProofs = newVault[int](pendingLife)
	s.oidcCache = map[string]*oidcEntry{}
	s.limitRecoveryAddr = ratelimit.New(ratelimit.Per(5, time.Hour), 5)
	s.limitRecoveryUser = ratelimit.New(ratelimit.Per(10, 24*time.Hour), 10)
	s.limitRecoveryLink = ratelimit.New(ratelimit.Per(10, time.Hour), 10)
	s.limitSsoStart = ratelimit.New(ratelimit.Per(20, 5*time.Minute), 20)
	s.limitSsoCallback = ratelimit.New(ratelimit.Per(30, 5*time.Minute), 30)
	s.limitTotp = ratelimit.New(ratelimit.Per(5, 15*time.Minute), 5)
	s.limitAuthorize = ratelimit.New(ratelimit.Per(30, 5*time.Minute), 30)
	s.limitToken = ratelimit.New(ratelimit.Per(30, time.Minute), 30)
	// A pairing code may be polled once every two seconds; the burst of
	// two lets the first poll follow the claim at once.
	s.limitPairPoll = ratelimit.New(ratelimit.Per(1, 2*time.Second), 2)
	s.limitWellKnown = ratelimit.New(ratelimit.Per(60, time.Minute), 60)
	s.limitOpenAccounts = ratelimit.New(ratelimit.Per(5, 24*time.Hour), 5)
}

// failed counts one failure against a limit that counts failures only. A
// key that is an IPv6 address is counted with the rest of its /64.
func failed(l *ratelimit.Limiter, key string) { l.Take(ratelimit.AddressKey(key)) }

// blocked answers rate_limited when key has already failed too often.
func blocked(l *ratelimit.Limiter, key string) error {
	if res := l.Peek(ratelimit.AddressKey(key)); !res.OK {
		return model.RateLimited(res.RetryAfterSeconds())
	}
	return nil
}

// CheckInviteFrom checks an invite or claim code for someone not signed
// in, held to 10 wrong codes an hour per address (docs/AUTH.md section 14):
// on an instance where joining needs an invite, guessing codes is the way
// in that is left.
func (s *Service) CheckInviteFrom(ctx context.Context, addr, code string) (model.Invite, error) {
	if err := blocked(s.limitInvite, addr); err != nil {
		return model.Invite{}, err
	}
	inv, err := s.people.CheckInvite(ctx, code)
	if errors.Is(err, model.ErrNotFound) {
		failed(s.limitInvite, addr)
	}
	return inv, err
}

// LimitWellKnown is the limit on the documents anyone may fetch.
func (s *Service) LimitWellKnown(addr string) error { return limited(s.limitWellKnown, addr) }

// policy is the sign-in policy of docs/AUTH.md section 2, as stored now.
type policy struct {
	// MemberSignIn is passkey, either or sso.
	MemberSignIn string
	// SecondRequired is auth.second_factor == required.
	SecondRequired bool
	// Accepted are the ticked proofs.
	Accepted map[string]bool
}

var allProofs = []string{"passkey", "totp", "provider"}

func (s *Service) policy(ctx context.Context, a store.Areas) (policy, error) {
	var p policy
	var err error
	if p.MemberSignIn, err = store.SettingString(ctx, a, store.SettingMemberSignIn, "passkey"); err != nil {
		return p, err
	}
	second, err := store.SettingString(ctx, a, store.SettingSecondFactor, "off")
	if err != nil {
		return p, err
	}
	p.SecondRequired = second == "required"
	accepted := allProofs
	raw, found, err := a.Settings().Get(ctx, SettingSecondFactorAccepted)
	if err != nil {
		return p, err
	}
	if found {
		var list []string
		if json.Unmarshal(raw, &list) == nil {
			accepted = list
		}
	}
	p.Accepted = map[string]bool{}
	for _, v := range accepted {
		p.Accepted[v] = true
	}
	return p, nil
}

// passkeyAllowed reports whether an account of this role may sign in with a
// passkey. The owner and admins always may, so a broken provider cannot lock
// the server (docs/AUTH.md section 8).
func (p policy) passkeyAllowed(role model.Role) bool {
	switch role {
	case model.RoleOwner, model.RoleAdmin:
		return true
	case model.RoleMember, model.RoleGuest:
		return p.MemberSignIn != "sso"
	}
	return false
}

// ssoOffered reports whether company sign-in is offered at all.
func (p policy) ssoOffered() bool { return p.MemberSignIn == "either" || p.MemberSignIn == "sso" }

// person refuses anything but a browser or app session: personal tokens
// and bots do not manage sign-in.
func person(who Who) error {
	if who.Session.Kind != model.SessionWeb && who.Session.Kind != model.SessionApp {
		return model.ErrForbidden.With("This needs a signed-in browser or app.")
	}
	return nil
}

// complete refuses a session that is pending or came from recovery.
func complete(who Who) error {
	if err := person(who); err != nil {
		return err
	}
	if who.Session.PendingSecondFactor {
		return model.ErrSecondFactorRequired
	}
	if who.Session.Recovery {
		return model.ErrForbidden.With("A recovery session can only set up a new passkey.")
	}
	return nil
}

// fresh refuses a session that was not checked in the last ten minutes
// (docs/AUTH.md section 10.4).
func (s *Service) fresh(who Who) error {
	if err := complete(who); err != nil {
		return err
	}
	if who.Session.FreshAt.IsZero() || s.now().Sub(who.Session.FreshAt) > freshFor {
		return model.ErrFreshCheckRequired
	}
	return nil
}

// admin refuses anyone who may not use the admin console.
func (s *Service) admin(ctx context.Context, who Who, action perm.ServerAction) error {
	return s.perm.CanAdminister(ctx, who.Account, action)
}

// audit writes one entry for something an account did.
func (s *Service) audit(ctx context.Context, a store.Areas, actor model.AccountID, action, targetKind, targetID string, detail map[string]any) error {
	e := model.AuditEntry{At: s.now(), ActorKind: "account", ActorID: string(actor), Action: action, TargetKind: targetKind, TargetID: targetID, Detail: detail}
	if actor == "" {
		e.ActorKind = "server"
	}
	return a.Audit().Append(ctx, e)
}

// accountEvent builds an event that belongs to no conversation.
func (s *Service) accountEvent(eventType string, fields map[string]any) model.Event {
	return model.Event{Type: eventType, At: s.now(), Payload: fields}
}

// personalTokenAllowed applies ruling R42: where company sign-in is required
// for the account's role, its personal tokens stop working 30 days after
// its last company sign-in, and work again once it has signed in.
func (s *Service) personalTokenAllowed(ctx context.Context, acc model.Account, now model.Time) error {
	if acc.Role != model.RoleMember && acc.Role != model.RoleGuest {
		return nil
	}
	mode, err := store.SettingString(ctx, s.store, store.SettingMemberSignIn, "passkey")
	if err != nil || mode != "sso" {
		return err
	}
	last, err := s.store.Identities().LastUsed(ctx, acc.ID)
	if err != nil {
		return err
	}
	if last.IsZero() || now.Sub(last) > personalIdle {
		return model.ErrUnauthenticated.With("This token is paused until you sign in through the company's sign-in again.")
	}
	return nil
}
