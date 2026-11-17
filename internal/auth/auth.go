// Package auth gets from a request to a session: first-run setup, passkey
// registration and sign-in, sessions by cookie and by bearer token, the
// origin check, and sign-out (docs/AUTH.md).
//
// Passkey ceremonies and rate-limit counters live in memory: a server is
// one process, and a restart only makes people who were halfway through
// signing in start again.
package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/people"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// CookieName is the session cookie.
const CookieName = "churr_session"

// Lifetimes from docs/AUTH.md section 10.
const (
	ceremonyLife     = 5 * time.Minute
	maxCeremonies    = 10000
	passkeySeconds   = 30 * 24 * 3600
	freshFor         = 10 * time.Minute
	seenEvery        = time.Minute
	slideEvery       = time.Hour
	recoveryCodes    = 8
	closeCredential  = 4001
	defaultTokenName = "Personal token"
)

// Hub is what auth needs of the live hub: closing the connections of a
// session that has ended.
type Hub interface {
	CloseSession(account model.AccountID, session model.SessionID, code int, reason string)
	// Send sends an event that belongs to no conversation to every
	// connection of one account.
	Send(account model.AccountID, v any)
}

// IntegrationLimiter counts one request of a server integration, by the
// integration's identifier, and says whether it may go ahead.
type IntegrationLimiter interface {
	TakeIntegration(integration string) ratelimit.Result
}

// Config is what auth needs to know about the server.
type Config struct {
	// Hostname is the relying-party ID: the server's host name, exactly.
	Hostname string
	// Origin is the server's own origin, fixed by configuration and never
	// taken from a request.
	Origin string
	// SecureCookies is false only for the local trial at http://localhost.
	SecureCookies bool
}

// Deps is what a Service is built from.
type Deps struct {
	Store  store.Store
	Perm   *perm.Checker
	People *people.Service
	Chat   *chat.Service
	Hub    Hub
	Config Config
	Log    *slog.Logger
	// Keys seals the secrets the server must be able to use again:
	// authenticator secrets, OIDC client secrets, SAML signing keys.
	Keys *secret.Keys
	// HTTP is the client used to reach identity providers. Nil gives one
	// with a ten-second timeout.
	HTTP *http.Client
	// Blobs stores the community's icon.
	Blobs blob.Blobs
	// Integrations limits the requests made with an integration token
	// (docs/BOTS.md section 8). The bots service implements it; nil
	// means no limit.
	Integrations IntegrationLimiter
	// TestStorage, when set, is asked to try an object store before the
	// file-store settings are changed to it.
	TestStorage func(ctx context.Context, settings map[string]json.RawMessage) error
}

// Service is the sign-in service.
type Service struct {
	// hasOwner is set once the server is seen to have an owner
	// (SetupNeeded).
	hasOwner atomic.Bool

	store  store.Store
	perm   *perm.Checker
	people *people.Service
	chat   *chat.Service
	hub    Hub
	cfg    Config
	log    *slog.Logger
	wa     *webauthn.WebAuthn
	now    func() model.Time

	mu         sync.Mutex
	ceremonies map[string]ceremony

	// Limits per address on the steps reachable without being signed in
	// (docs/AUTH.md section 14).
	limitSetup, limitRegister, limitLogin, limitBadCredential, limitInvite *ratelimit.Limiter
	// Wrong access keys (accesskey.go).
	accessKeyLimits accessKeyLimits

	// Everything the rest of sign-in keeps: sealing keys, the client for
	// identity providers, and the short-lived state held in memory.
	state
}

// New makes the service.
func New(d Deps) (*Service, error) {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          d.Config.Hostname,
		RPDisplayName: "Churr",
		RPOrigins:     []string{d.Config.Origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			// Every passkey is discoverable, so sign-in needs no
			// username, and every use checks a PIN or biometric.
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		},
	})
	if err != nil {
		return nil, err
	}
	s := &Service{
		store: d.Store, perm: d.Perm, people: d.People, chat: d.Chat, hub: d.Hub, cfg: d.Config, log: d.Log, wa: wa,
		now: model.Now, ceremonies: map[string]ceremony{},
		limitSetup:         ratelimit.New(ratelimit.Per(5, time.Hour), 5),
		limitRegister:      ratelimit.New(ratelimit.Per(10, time.Hour), 10),
		limitLogin:         ratelimit.New(ratelimit.Per(30, 5*time.Minute), 30),
		limitBadCredential: ratelimit.New(ratelimit.Per(30, 5*time.Minute), 30),
		limitInvite:        ratelimit.New(ratelimit.Per(10, time.Hour), 10),
		accessKeyLimits:    newAccessKeyLimits(),
	}
	s.initState(d)
	return s, nil
}

// limited answers rate_limited when an address has used a step too often.
// An IPv6 address is counted with the rest of its /64.
func limited(l *ratelimit.Limiter, addr string) error {
	if ok, retry := l.Allow(ratelimit.AddressKey(addr)); !ok {
		return model.RateLimited(int(retry/time.Second) + 1)
	}
	return nil
}

// ceremony is a passkey step waiting for the browser's answer.
type ceremony struct {
	kind    string // setup | register | login | add | check
	session webauthn.SessionData
	// For add and check: the account and session the step belongs to.
	account   model.AccountID
	sessionID model.SessionID
	// For setup and register: the account to be made.
	username, displayName string
	role                  model.Role
	handle                []byte
	inviteID              model.InviteID
	// claims is the imported account a claim link takes over, in place
	// of making a new one (claim.go).
	claims model.AccountID
	// applying is set for a registration without an invite while new
	// accounts wait for approval; note is what the applicant said
	// (docs/FEATURES.md section 31).
	applying bool
	note     string
	// addr is where a registration came from, for the day's count of
	// accounts made without an invite (registerTx).
	addr    string
	expires time.Time
}

func (s *Service) putCeremony(c ceremony) string {
	id, _ := secret.NewLinkValue()
	c.expires = time.Now().Add(ceremonyLife)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.ceremonies {
		if now.After(v.expires) {
			delete(s.ceremonies, k)
		}
	}
	// Everything in memory has a cap; past it the oldest go first.
	for len(s.ceremonies) >= maxCeremonies {
		var oldest string
		for k, v := range s.ceremonies {
			if oldest == "" || v.expires.Before(s.ceremonies[oldest].expires) {
				oldest = k
			}
		}
		delete(s.ceremonies, oldest)
	}
	s.ceremonies[id] = c
	return id
}

// peekCeremony returns a ceremony without removing it.
func (s *Service) peekCeremony(id, kind string) (ceremony, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, found := s.ceremonies[id]
	return c, found && c.kind == kind && time.Now().Before(c.expires)
}

// takeCeremony removes a ceremony, so it can be used once.
func (s *Service) takeCeremony(id, kind string) (ceremony, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, found := s.ceremonies[id]
	delete(s.ceremonies, id)
	return c, found && c.kind == kind && time.Now().Before(c.expires)
}

// waUser adapts an account and its passkeys to the WebAuthn library.
type waUser struct {
	handle      []byte
	name        string
	displayName string
	creds       []webauthn.Credential
}

func (w waUser) WebAuthnID() []byte                         { return w.handle }
func (w waUser) WebAuthnName() string                       { return w.name }
func (w waUser) WebAuthnDisplayName() string                { return w.displayName }
func (w waUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

// Ceremony is the start of a passkey step (schema Ceremony).
type Ceremony struct {
	CeremonyID string `json:"ceremony_id"`
	PublicKey  any    `json:"public_key"`
}

// Finish is the end of a passkey step (schemas CeremonyResult and
// PasskeyCreated).
type Finish struct {
	CeremonyID  string
	Credential  []byte
	PasskeyName string
	PRFEnabled  bool
}

// SignIn is the result of a sign-in step. Token goes in the cookie and
// nowhere else.
type SignIn struct {
	Session       model.Session
	Account       model.Account
	RecoveryCodes []string
	Token         string
	// SecondFactor is set while the session still owes a second step.
	SecondFactor *model.SecondFactorState
	// Applied is set, and nothing else but Account, when the account was
	// made to wait for its registration to be approved: it is not signed
	// in.
	Applied *model.Approval
}

// SetupNeeded reports whether the server has no owner yet. Until it has,
// every operation but setup answers `unavailable`.
func (s *Service) SetupNeeded(ctx context.Context) (bool, error) {
	// Every request asks this. A server that has an owner has one for
	// good (ownership is handed over, never given up), so once the answer
	// is no it is not read again.
	if s.hasOwner.Load() {
		return false, nil
	}
	_, found, err := s.store.Accounts().Owner(ctx)
	if err == nil && found {
		s.hasOwner.Store(true)
	}
	return !found, err
}

// EnsureSetupLink makes the one-time setup link when the server has no
// owner and no unused link yet, and returns it. An unused link from an
// earlier start is kept and keeps working until it is used: link is then ""
// and kept is true, since only its hash is stored and it cannot be shown
// again. Both are empty once an owner exists.
func (s *Service) EnsureSetupLink(ctx context.Context) (link string, kept bool, err error) {
	need, err := s.SetupNeeded(ctx)
	if err != nil || !need {
		return "", false, err
	}
	have, err := store.SettingString(ctx, s.store, store.SettingSetupTokenHash, "")
	if err != nil {
		return "", false, err
	}
	if have != "" {
		return "", true, nil
	}
	link, err = NewSetupLink(ctx, s.store, s.cfg.Origin)
	return link, false, err
}

// NewSetupLink makes a new one-time setup link and returns it; the link
// made before, if any, stops working. It is `churr setup-link`, for an
// operator who lost the first one. It refuses once the server has an owner.
func NewSetupLink(ctx context.Context, st store.Areas, origin string) (string, error) {
	if _, found, err := st.Accounts().Owner(ctx); err != nil {
		return "", err
	} else if found {
		return "", model.ErrConflict.With("This server already has an owner, so it has no setup link. A locked-out owner uses churr recovery.")
	}
	value, hash := secret.NewLinkValue()
	if err := st.Settings().Set(ctx, store.SettingSetupTokenHash, base64.StdEncoding.EncodeToString(hash), ""); err != nil {
		return "", err
	}
	return origin + "/setup/" + value, nil
}

// nameFree refuses a username that is taken or reserved, before a passkey
// is made for it. The store checks again when the account is created.
func (s *Service) nameFree(ctx context.Context, username string) error {
	if model.ReservedName(username) {
		return model.ErrNameTaken
	}
	if _, err := s.store.Accounts().ByUsername(ctx, username); err == nil {
		return model.ErrNameTaken
	} else if !errors.Is(err, model.ErrNotFound) {
		return err
	}
	if _, err := s.store.Groups().ByHandle(ctx, username); err == nil {
		return model.ErrNameTaken
	} else if !errors.Is(err, model.ErrNotFound) {
		return err
	}
	// A name someone gave up by renaming is held for them for a while.
	if held, err := s.store.Accounts().UsernameHeld(ctx, username, ""); err != nil || held {
		if err == nil {
			err = model.ErrNameTaken
		}
		return err
	}
	return nil
}

// beginCreate starts a passkey creation for an account that does not exist
// yet. PRF is requested on every creation: an authenticator decides then
// whether a credential supports it, and it cannot be switched on later.
func (s *Service) beginCreate(c ceremony) (Ceremony, error) {
	c.handle = secret.RandomBytes(32)
	return s.beginCreateFor(c, nil)
}

// beginCreateFor is beginCreate for an account that may already have a user
// handle and passkeys: its existing credentials are excluded, so the same
// authenticator is not registered twice.
func (s *Service) beginCreateFor(c ceremony, existing []webauthn.Credential) (Ceremony, error) {
	exclude := make([]protocol.CredentialDescriptor, 0, len(existing))
	for _, cred := range existing {
		exclude = append(exclude, cred.Descriptor())
	}
	opts, session, err := s.wa.BeginRegistration(
		waUser{handle: c.handle, name: c.username, displayName: c.displayName},
		webauthn.WithExclusions(exclude),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
		webauthn.WithCredentialParameters([]protocol.CredentialParameter{
			{Type: protocol.PublicKeyCredentialType, Algorithm: webauthncose.AlgES256},
			{Type: protocol.PublicKeyCredentialType, Algorithm: webauthncose.AlgEdDSA},
			{Type: protocol.PublicKeyCredentialType, Algorithm: webauthncose.AlgRS256},
		}),
		webauthn.WithExtensions(webauthn.WithExtensionPRFSupport()),
	)
	if err != nil {
		return Ceremony{}, err
	}
	c.session = *session
	return Ceremony{CeremonyID: s.putCeremony(c), PublicKey: opts.Response}, nil
}

// BeginSetup starts making the owner account, for whoever holds the setup
// link.
func (s *Service) BeginSetup(ctx context.Context, addr, token, username, displayName string) (Ceremony, error) {
	c, err := s.prepareSetup(ctx, addr, token, username, displayName)
	if err != nil {
		return Ceremony{}, err
	}
	return s.beginCreate(c)
}

// prepareSetup checks the setup link and the names and returns what the
// owner account will be made of.
func (s *Service) prepareSetup(ctx context.Context, addr, token, username, displayName string) (ceremony, error) {
	need, err := s.SetupNeeded(ctx)
	if err != nil {
		return ceremony{}, err
	}
	want, err := store.SettingString(ctx, s.store, store.SettingSetupTokenHash, "")
	if err != nil {
		return ceremony{}, err
	}
	got := base64.StdEncoding.EncodeToString(secret.HashToken(token))
	if !need || want == "" || got != want {
		// Only wrong guesses count against the address.
		if err := limited(s.limitSetup, addr); err != nil {
			return ceremony{}, err
		}
		return ceremony{}, model.ErrNotFound
	}
	username, displayName, err = people.CleanNames(username, displayName)
	if err != nil {
		return ceremony{}, err
	}
	if err := s.nameFree(ctx, username); err != nil {
		return ceremony{}, err
	}
	return ceremony{kind: "setup", username: username, displayName: displayName, role: model.RoleOwner}, nil
}

// BeginRegistration starts registering a new account with a passkey. note
// is what the person says of why they would like to join, kept with their
// application while registrations wait for approval.
func (s *Service) BeginRegistration(ctx context.Context, addr, inviteCode, username, displayName, note string) (Ceremony, error) {
	c, err := s.prepareRegistration(ctx, addr, inviteCode, username, displayName, note)
	if err != nil {
		return Ceremony{}, err
	}
	return s.beginCreate(c)
}

// prepareRegistration checks the registration mode, the invite or claim
// link, the sign-in policy and the names, and returns what the account will
// be made of: a new one, or the imported one a claim link takes over.
func (s *Service) prepareRegistration(ctx context.Context, addr, inviteCode, username, displayName, note string) (ceremony, error) {
	if err := limited(s.limitRegister, addr); err != nil {
		return ceremony{}, err
	}
	mode, err := store.SettingString(ctx, s.store, store.SettingRegistrationMode, "invite")
	if err != nil {
		return ceremony{}, err
	}
	c := ceremony{kind: "register", role: model.RoleMember}
	if inviteCode != "" {
		inv, err := s.CheckInviteFrom(ctx, addr, inviteCode)
		if err != nil {
			return ceremony{}, err
		}
		if inv.ClaimsAccountID != "" {
			// A claim link takes over an imported account, in every
			// registration mode: no account is made (claim.go).
			policy, err := store.SettingString(ctx, s.store, store.SettingMemberSignIn, "passkey")
			if err != nil {
				return ceremony{}, err
			}
			if policy == "sso" {
				return ceremony{}, model.ErrForbidden.With("Accounts on this server sign in through the company's sign-in.")
			}
			return s.prepareClaim(ctx, inv, username, displayName)
		}
		c.inviteID, c.role = inv.ID, inv.Role
	} else if mode != "open" {
		if mode == "closed" {
			return ceremony{}, model.ErrForbidden.With("This server is not accepting new accounts.")
		}
		return ceremony{}, model.ErrForbidden.With("You need an invite to join this server.")
	}
	if mode == "closed" {
		return ceremony{}, model.ErrForbidden.With("This server is not accepting new accounts.")
	}
	if c.applying, err = s.applying(ctx, inviteCode, note); err != nil {
		return ceremony{}, err
	}
	c.note = strings.TrimSpace(note)
	policy, err := store.SettingString(ctx, s.store, store.SettingMemberSignIn, "passkey")
	if err != nil {
		return ceremony{}, err
	}
	if policy == "sso" {
		return ceremony{}, model.ErrForbidden.With("Accounts on this server sign in through the company's sign-in.")
	}
	if c.username, c.displayName, err = people.CleanNames(username, displayName); err != nil {
		return ceremony{}, err
	}
	if err := s.nameFree(ctx, c.username); err != nil {
		return ceremony{}, err
	}
	// Accounts made without an invite are counted per address, a day at a
	// time, whatever the way in (registerTx takes the count). A sixth is
	// refused here already, before a passkey is made for nothing.
	c.addr = addr
	if c.inviteID == "" {
		if err := blocked(s.limitOpenAccounts, addr); err != nil {
			return ceremony{}, err
		}
	}
	return c, nil
}

// verifyCreate checks the browser's answer to a creation and returns the
// passkey row to store.
func (s *Service) verifyCreate(c ceremony, in Finish) (model.Passkey, error) {
	parsed, err := protocol.ParseCredentialCreationResponseBytes(in.Credential)
	if err != nil {
		return model.Passkey{}, model.BadRequest("The passkey response could not be read.")
	}
	cred, err := s.wa.CreateCredential(waUser{handle: c.handle, name: c.username, displayName: c.displayName}, c.session, parsed)
	if err != nil {
		s.log.Warn("passkey creation refused", "err", err)
		return model.Passkey{}, model.ErrUnauthenticated.With("The passkey could not be verified.")
	}
	record, err := jsonMarshal(cred)
	if err != nil {
		return model.Passkey{}, err
	}
	name := strings.TrimSpace(in.PasskeyName)
	if name == "" {
		name = "Passkey added " + time.Now().Format("2 Jan 2006")
	}
	if len(name) > 100 {
		name = name[:100]
	}
	// has_prf records what the browser reported. It is a hint for the
	// account page, not a security decision.
	return model.Passkey{CredentialID: cred.ID, Credential: record, Name: name, HasPRF: in.PRFEnabled}, nil
}

// firstWay is how a new account will sign in: its first passkey, or the
// hash of its access key's sign-in value (accesskey.go).
type firstWay struct {
	passkey      *model.Passkey
	accessKey    []byte
	unlockPublic []byte // the access key's, when the page gave it
}

// how names the way for the audit log and the session.
func (w firstWay) how() string {
	if w.passkey != nil {
		return signedInPasskey
	}
	return signedInAccessKey
}

// addTx stores the way for an account. A first passkey comes with eight
// recovery codes, returned to be shown once; an access key comes with none.
func (w firstWay) addTx(ctx context.Context, tx store.Tx, account model.AccountID, now model.Time) ([]string, error) {
	if w.passkey == nil {
		return nil, tx.Passkeys().SetAccessKey(ctx, account, w.accessKey, w.unlockPublic, now)
	}
	pk := *w.passkey
	pk.AccountID = account
	if err := tx.Passkeys().Add(ctx, &pk); err != nil {
		return nil, err
	}
	return newRecoveryCodes(ctx, tx, account)
}

// newAccountTx makes the rows every new account that signs in by itself
// gets: the account, its first way in (with recovery codes for a passkey)
// and a web session.
func (s *Service) newAccountTx(ctx context.Context, tx store.Tx, c ceremony, way firstWay, label string) (SignIn, store.MembershipResult, error) {
	var out SignIn
	handle := c.handle
	if way.passkey == nil {
		// An account gets its user handle with its first passkey.
		handle = nil
	}
	acc, res, err := s.people.CreateTx(ctx, tx, people.NewAccount{Username: c.username, DisplayName: c.displayName, Role: c.role, WebAuthnHandle: handle})
	if err != nil {
		return out, res, err
	}
	if out.RecoveryCodes, err = way.addTx(ctx, tx, acc.ID, s.now()); err != nil {
		return out, res, err
	}
	out.Account = acc
	out.Session, out.Token, err = s.newSessionByTx(ctx, tx, acc.ID, label, way.how())
	return out, res, err
}

// newSessionTx makes a web session for a passkey sign-in. A new sign-in
// always gets a new row and a new token.
func (s *Service) newSessionTx(ctx context.Context, a store.Areas, account model.AccountID, label string) (model.Session, string, error) {
	return s.newSessionByTx(ctx, a, account, label, signedInPasskey)
}

// The two ways a person signs in by themselves. Both make a session that
// lasts sessions.passkey_seconds and slides with use.
const (
	signedInPasskey   = "passkey"
	signedInAccessKey = "access_key"
)

// slides reports whether a session made this way moves its end with use.
func slides(by string) bool { return by == signedInPasskey || by == signedInAccessKey }

// newSessionByTx makes a web session for a passkey or access key sign-in.
func (s *Service) newSessionByTx(ctx context.Context, a store.Areas, account model.AccountID, label, by string) (model.Session, string, error) {
	life, err := store.SettingIntOr(ctx, a, store.SettingPasskeySeconds, passkeySeconds)
	if err != nil {
		return model.Session{}, "", err
	}
	now := s.now()
	token, hash := secret.NewToken(secret.PrefixSession)
	ses := model.Session{AccountID: account, Kind: model.SessionWeb, Label: label, SignedInBy: by,
		CreatedAt: now, LastSeenAt: now, FreshAt: now, ExpiresAt: now.Add(time.Duration(life) * time.Second)}
	err = a.Sessions().Create(ctx, &ses, hash, nil)
	ses.Current = true
	return ses, token, err
}

// FinishSetup makes the owner with its first passkey and recovery codes,
// signs it in, and finishes first-run setup: #general, the default channel
// everyone joins at sign-up, and the setup link spent.
func (s *Service) FinishSetup(ctx context.Context, in Finish, label string) (SignIn, error) {
	c, ok := s.takeCeremony(in.CeremonyID, "setup")
	if !ok {
		return SignIn{}, model.ErrNotFound.With("That took too long. Start again.")
	}
	pk, err := s.verifyCreate(c, in)
	if err != nil {
		return SignIn{}, err
	}
	return s.setupTx(ctx, c, firstWay{passkey: &pk}, label)
}

// setupTx makes the owner with its first way in, signs it in, and finishes
// first-run setup: #general with @everyone in it, and the setup link spent.
func (s *Service) setupTx(ctx context.Context, c ceremony, way firstWay, label string) (SignIn, error) {
	var out SignIn
	err := s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		// From the moment an owner exists the setup link is refused.
		if _, found, err := tx.Accounts().Owner(ctx); err != nil {
			return err
		} else if found {
			return model.ErrNotFound
		}
		var res store.MembershipResult
		var err error
		if out, res, err = s.newAccountTx(ctx, tx, c, way, label); err != nil {
			return err
		}
		now := s.now()
		// docs/FEATURES.md section 26, step 6. The contract has no
		// operation of its own for that step, so it happens here.
		general := model.Conversation{Kind: model.KindChannel, Name: "general", CreatedByID: out.Account.ID, CreatedAt: now}
		if err := tx.Conversations().Create(ctx, &general); err != nil {
			return err
		}
		r, err := tx.Members().AddDirect(ctx, general.ID, out.Account.ID, model.LevelManager, now)
		if err != nil {
			return err
		}
		res.Merge(r)
		// #general is the instance's one default channel: everyone who
		// signs up from now on joins it (ruling R103).
		if err := tx.Settings().Set(ctx, store.SettingDefaultChannels, []model.ConversationID{general.ID}, ""); err != nil {
			return err
		}
		if err := tx.Settings().Delete(ctx, store.SettingSetupTokenHash); err != nil {
			return err
		}
		if err := tx.Settings().Set(ctx, store.SettingSetupDoneAt, int64(now), ""); err != nil {
			return err
		}
		if err := tx.Audit().Append(ctx, model.AuditEntry{At: now, ActorKind: "server", Action: "owner.created",
			TargetKind: "account", TargetID: string(out.Account.ID), Detail: map[string]any{"how": way.how()}}); err != nil {
			return err
		}
		return rec.Membership(ctx, res, "", "")
	})
	if err != nil {
		return SignIn{}, err
	}
	s.log.Info("owner account created; the setup link no longer works", "username", out.Account.Username)
	return out, nil
}

// FinishRegistration makes the account, counts the invite, puts the account
// where the invite says, and signs it in, all together.
func (s *Service) FinishRegistration(ctx context.Context, in Finish, label string) (SignIn, error) {
	c, ok := s.takeCeremony(in.CeremonyID, "register")
	if !ok {
		return SignIn{}, model.ErrNotFound.With("That took too long. Start again.")
	}
	pk, err := s.verifyCreate(c, in)
	if err != nil {
		return SignIn{}, err
	}
	return s.registerTx(ctx, c, firstWay{passkey: &pk}, label)
}

// registerTx makes the account with its first way in (or claims the
// imported one), counts the invite, puts the account where the invite
// says, and signs it in, all together.
func (s *Service) registerTx(ctx context.Context, c ceremony, way firstWay, label string) (SignIn, error) {
	if c.claims == "" && c.inviteID == "" {
		// Five accounts a day per address, an IPv6 /64 counted as one
		// (docs/AUTH.md section 14), for a passkey and an access key alike.
		if err := limited(s.limitOpenAccounts, c.addr); err != nil {
			return SignIn{}, err
		}
	}
	var out SignIn
	err := s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		if c.claims != "" {
			// A claim link: the imported account is taken over.
			var err error
			out, err = s.claimTx(ctx, tx, rec, c, way, label)
			return err
		}
		if c.applying {
			var err error
			out, err = s.applyTx(ctx, tx, c, way)
			return err
		}
		now := s.now()
		var inv model.Invite
		if c.inviteID != "" {
			// Counted with an update that only succeeds while the
			// invite is usable: two people racing for the last use
			// cannot both win.
			if err := tx.Invites().Use(ctx, c.inviteID, now); err != nil {
				return err
			}
			var err error
			if inv, err = tx.Invites().Get(ctx, c.inviteID); err != nil {
				return err
			}
		}
		var res store.MembershipResult
		var err error
		if out, res, err = s.newAccountTx(ctx, tx, c, way, label); err != nil {
			return err
		}
		for _, g := range inv.GroupIDs {
			r, err := tx.Groups().AddMember(ctx, g, out.Account.ID, inv.CreatedByID, now)
			if errors.Is(err, model.ErrNotFound) {
				continue // the group was deleted since the invite was made
			}
			if err != nil {
				return err
			}
			res.Merge(r)
		}
		for _, cid := range inv.ConversationIDs {
			conv, err := tx.Conversations().Get(ctx, cid)
			if errors.Is(err, model.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			r, err := tx.Members().AddDirect(ctx, cid, out.Account.ID, conv.DefaultLevel, now)
			// The invite works; a locked channel is skipped
			// (docs/PERMISSIONS.md section 10.1).
			if errors.Is(err, model.ErrLocked) || errors.Is(err, model.ErrAlreadyExists) {
				continue
			}
			if err != nil {
				return err
			}
			res.Merge(r)
		}
		detail := map[string]any{"how": way.how()}
		if c.inviteID != "" {
			detail["invite_id"] = c.inviteID
		}
		if err := tx.Audit().Append(ctx, model.AuditEntry{At: now, ActorID: string(out.Account.ID), ActorKind: "account",
			Action: "account.registered", TargetKind: "account", TargetID: string(out.Account.ID), Detail: detail}); err != nil {
			return err
		}
		return rec.Membership(ctx, res, inv.CreatedByID, "")
	})
	if errors.Is(err, model.ErrNotFound) && c.inviteID != "" {
		return SignIn{}, model.ErrNotFound.With("That invite can no longer be used.")
	}
	return out, err
}

// BeginLogin starts a passkey sign-in. Nothing in the reply depends on who
// is signing in.
func (s *Service) BeginLogin(ctx context.Context, addr string) (Ceremony, error) {
	if err := limited(s.limitLogin, addr); err != nil {
		return Ceremony{}, err
	}
	opts, session, err := s.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return Ceremony{}, err
	}
	return Ceremony{CeremonyID: s.putCeremony(ceremony{kind: "login", session: *session}), PublicKey: opts.Response}, nil
}

// errNotRegistered is the one answer to every failed passkey sign-in.
var errNotRegistered = model.ErrUnauthenticated.With("That passkey is not registered here.")

// FinishLogin finishes a passkey sign-in.
func (s *Service) FinishLogin(ctx context.Context, addr string, in Finish, label string) (SignIn, error) {
	if err := limited(s.limitLogin, addr); err != nil {
		return SignIn{}, err
	}
	c, ok := s.takeCeremony(in.CeremonyID, "login")
	if !ok {
		return SignIn{}, model.ErrNotFound.With("That took too long. Start again.")
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(in.Credential)
	if err != nil {
		return SignIn{}, model.BadRequest("The passkey response could not be read.")
	}
	var (
		acc  model.Account
		keys []model.Passkey
	)
	cred, err := s.wa.ValidateDiscoverableLogin(func(rawID, handle []byte) (webauthn.User, error) {
		a, err := s.store.Accounts().ByWebAuthnHandle(ctx, handle)
		if err != nil {
			return nil, err
		}
		ks, err := s.store.Passkeys().ForAccount(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		u := waUser{handle: a.WebAuthnHandle, name: a.Username, displayName: a.DisplayName}
		for _, k := range ks {
			var wc webauthn.Credential
			if jsonUnmarshal(k.Credential, &wc) == nil {
				u.creds = append(u.creds, wc)
			}
		}
		acc, keys = a, ks
		return u, nil
	}, c.session, parsed)
	if err != nil {
		s.log.Warn("sign-in failed", "method", "passkey", "address", addr, "err", err)
		return SignIn{}, errNotRegistered
	}
	if !acc.Active() {
		if err := s.application(ctx, acc); err != nil {
			return SignIn{}, err
		}
		return SignIn{}, errNotRegistered
	}
	if !acc.Role.IsAdmin() {
		policy, err := store.SettingString(ctx, s.store, store.SettingMemberSignIn, "passkey")
		if err != nil {
			return SignIn{}, err
		}
		if policy == "sso" {
			return SignIn{}, model.ErrForbidden.With("Sign in through the company's sign-in.")
		}
	}
	var used model.Passkey
	for _, k := range keys {
		if string(k.CredentialID) == string(cred.ID) {
			used = k
		}
	}
	if used.ID == "" {
		return SignIn{}, errNotRegistered
	}
	now := s.now()
	if cred.Authenticator.CloneWarning {
		// A counter that went backwards suggests a copied authenticator.
		s.store.Audit().Append(ctx, model.AuditEntry{At: now, ActorID: string(acc.ID), ActorKind: "account",
			Action: "passkey.clone_suspected", TargetKind: "account", TargetID: string(acc.ID), Detail: map[string]any{"passkey": used.Name}})
		return SignIn{}, errNotRegistered
	}
	record, err := jsonMarshal(cred)
	if err != nil {
		return SignIn{}, err
	}
	out := SignIn{Account: acc}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Passkeys().Used(ctx, used.ID, record, now); err != nil {
			return err
		}
		var err error
		out.Session, out.Token, err = s.newSessionTx(ctx, tx, acc.ID, label)
		return err
	})
	if err == nil {
		s.sessionStarted(out.Session)
	}
	return out, err
}

// ErrNoCredential is returned by Authenticate when a request carries
// neither the session cookie nor a bearer token.
var ErrNoCredential = errors.New("auth: the request carries no credential")

// Who is an authenticated request.
type Who struct {
	Session model.Session
	Account model.Account
	// Cookie is true when the credential was the session cookie, which is
	// what makes the origin check apply.
	Cookie bool
}

// kinds says which session kind each token prefix belongs to. A prefix
// names the only place a token works (docs/AUTH.md section 1).
var bearerKinds = map[string]string{
	secret.PrefixApp:      model.SessionApp,
	secret.PrefixPersonal: model.SessionPersonal,
	secret.PrefixBot:      model.SessionBot,
}

// Authenticate finds the session and account behind a request. It returns
// ErrNoCredential when there is none, and model.ErrUnauthenticated when
// there is one that does not work.
func (s *Service) Authenticate(r *http.Request, addr string) (Who, error) {
	var (
		token, kind string
		cookie      bool
	)
	if h := r.Header.Get("Authorization"); h != "" {
		t, ok := strings.CutPrefix(h, "Bearer ")
		if !ok {
			return Who{}, model.ErrUnauthenticated
		}
		token = strings.TrimSpace(t)
		if secret.TokenPrefix(token) == secret.PrefixIntegration {
			// An integration token does only what was ticked for it,
			// on the integration operations (docs/PERMISSIONS.md
			// section 17, case 94). It is not an account.
			return Who{}, model.ErrForbidden.With("An integration token works only on the integration operations.")
		}
		if kind = bearerKinds[secret.TokenPrefix(token)]; kind == "" {
			// A cookie value, a webhook secret or anything else is not
			// a bearer token, whatever it would open elsewhere.
			return Who{}, s.bad(addr)
		}
	} else if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		token, kind, cookie = c.Value, model.SessionWeb, true
		if secret.TokenPrefix(token) != secret.PrefixSession {
			return Who{}, s.bad(addr)
		}
	} else {
		return Who{}, ErrNoCredential
	}
	ctx := r.Context()
	ses, err := s.store.Sessions().ByTokenHash(ctx, secret.HashToken(token))
	if errors.Is(err, model.ErrNotFound) {
		return Who{}, s.bad(addr)
	}
	if err != nil {
		return Who{}, err
	}
	now := s.now()
	if ses.Kind != kind || !ses.RevokedAt.IsZero() || (!ses.ExpiresAt.IsZero() && !ses.ExpiresAt.After(now)) ||
		(kind == model.SessionApp && !ses.AccessExpiresAt.IsZero() && !ses.AccessExpiresAt.After(now)) {
		return Who{}, model.ErrUnauthenticated
	}
	// The role and the removal mark are read on every request, so a
	// demotion or removal takes effect at once.
	acc, err := s.store.Accounts().Get(ctx, ses.AccountID)
	if err != nil || !acc.Active() {
		return Who{}, model.ErrUnauthenticated
	}
	if kind == model.SessionPersonal {
		if err := s.personalTokenAllowed(ctx, acc, now); err != nil {
			return Who{}, err
		}
	}
	// A passkey session slides. To spare the database the row is written
	// at most once a minute for last_seen_at and once an hour for the
	// new end.
	if now.Sub(ses.LastSeenAt) >= seenEvery {
		var until model.Time
		if slides(ses.SignedInBy) && ses.MaxExpiresAt.IsZero() && !ses.ExpiresAt.IsZero() {
			life, err := store.SettingIntOr(ctx, s.store, store.SettingPasskeySeconds, passkeySeconds)
			if err == nil {
				if next := now.Add(time.Duration(life) * time.Second); next.Sub(ses.ExpiresAt) >= slideEvery {
					until, ses.ExpiresAt = next, next
				}
			}
		}
		if err := s.store.Sessions().Touch(ctx, ses.ID, now, until); err != nil {
			s.log.Warn("a session could not be marked seen", "err", err)
		}
		ses.LastSeenAt = now
	}
	ses.Current = true
	return Who{Session: ses, Account: acc, Cookie: cookie}, nil
}

// bad counts a credential that matches nothing against its address.
func (s *Service) bad(addr string) error {
	if err := limited(s.limitBadCredential, addr); err != nil {
		return err
	}
	return model.ErrUnauthenticated
}

// OriginOK is the defence against cross-site request forgery for requests
// that carry the cookie: the Origin header must be the server's own; if it
// is missing, Sec-Fetch-Site must be same-origin; if both are missing the
// request is refused (docs/AUTH.md section 10.3).
func (s *Service) OriginOK(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		return o == s.cfg.Origin
	}
	return r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

// Cookie returns the session cookie for a token, or with an empty token the
// cookie that clears it.
func (s *Service) Cookie(token string, expires model.Time) *http.Cookie {
	c := &http.Cookie{Name: CookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode}
	switch {
	case token == "":
		c.MaxAge = -1
	case !expires.IsZero():
		c.MaxAge = int(expires.Sub(s.now()) / time.Second)
	}
	return c
}

// Logout ends a session and closes its live connections.
func (s *Service) Logout(ctx context.Context, who Who) error {
	if err := s.store.Sessions().Revoke(ctx, who.Session.ID, s.now()); err != nil {
		return err
	}
	s.hub.CloseSession(who.Account.ID, who.Session.ID, closeCredential, "signed out")
	return nil
}

// CreatePersonalToken makes a token a person's own scripts act as them
// with. It needs a browser or app session that was freshly checked.
func (s *Service) CreatePersonalToken(ctx context.Context, who Who, label string, expiresInSeconds int64) (model.Session, string, error) {
	if err := s.perm.CanAdminister(ctx, who.Account, perm.MakePersonalToken); err != nil {
		return model.Session{}, "", err
	}
	if who.Session.Kind != model.SessionWeb && who.Session.Kind != model.SessionApp {
		// A token cannot make tokens (docs/PERMISSIONS.md decision P1).
		return model.Session{}, "", model.ErrForbidden.With("Sign in to make a token.")
	}
	now := s.now()
	if who.Session.FreshAt.IsZero() || now.Sub(who.Session.FreshAt) > freshFor {
		return model.Session{}, "", model.ErrFreshCheckRequired
	}
	if expiresInSeconds < 0 {
		return model.Session{}, "", model.Invalid("expires_in_seconds", "expires_in_seconds is 0 for no expiry, or a positive number.")
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = defaultTokenName
	}
	token, hash := secret.NewToken(secret.PrefixPersonal)
	ses := model.Session{AccountID: who.Account.ID, Kind: model.SessionPersonal, Label: label, CreatedAt: now}
	if expiresInSeconds > 0 {
		ses.ExpiresAt = now.Add(time.Duration(expiresInSeconds) * time.Second)
	}
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Sessions().Create(ctx, &ses, hash, nil); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{At: now, ActorID: string(who.Account.ID), ActorKind: "account",
			Action: "token.created", TargetKind: "session", TargetID: string(ses.ID), Detail: map[string]any{"kind": "personal", "label": label}})
	})
	return ses, token, err
}
