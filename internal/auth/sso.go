package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/people"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// SsoCookie binds a company sign-in answer to the browser that started it
// (docs/AUTH.md section 7.1).
const SsoCookie = "churr_sso"

// The purposes a company sign-in can be started for.
const (
	PurposeSignIn = "sign_in"
	PurposeLink   = "link"
)

// waiting is a company sign-in request waiting for its answer: kept ten
// minutes in memory and usable once.
type waiting struct {
	provider string
	// binding is the SHA-256 of the value in the churr_sso cookie.
	binding []byte
	// OIDC: nonce and PKCE verifier. SAML: the AuthnRequest ID.
	nonce, verifier, requestID string
	purpose, invite, returnTo  string
	// For linking: the account and session that asked.
	account  model.AccountID
	session  model.SessionID
	recovery bool
}

// asserted is what a validated answer from a provider says.
type asserted struct {
	by       string // oidc | saml
	subject  string
	username string
	name     string
	// email is compared against imported accounts and then dropped. It is
	// never stored or logged.
	email     string
	groups    []string
	hasGroups bool
	// mfa is the provider's own statement that it used more than one factor.
	mfa bool
	// sessionEnd is SAML's SessionNotOnOrAfter, when it is given.
	sessionEnd model.Time
}

// registration is a first company sign-in waiting for the person to choose
// a username.
type registration struct {
	provider model.SsoProvider
	who      asserted
	w        waiting
	invite   model.InviteID
	role     model.Role
}

// grant is what an app session inherits from the browser session that
// approved or confirmed it.
type grant struct {
	signedInBy, secondFactor string
	max                      model.Time
}

// errBanned refuses a banned sign-in identity (docs/AUTH.md section 13).
var errBanned = model.ErrForbidden.With("This identity is banned from this server.")

// SsoStart is where to send the browser, and the cookie to set on it.
type SsoStart struct {
	Location string
	Cookie   *http.Cookie
}

// SsoResult is what a provider's answer led to.
type SsoResult struct {
	// SignIn is set when a session was made; its token goes in the cookie.
	SignIn *SignIn
	// Next is the path on this server to load afterwards.
	Next string
	// Register is set when the person must first choose a username.
	Register *RegistrationPage
	// ProviderName is the provider's name, for the failure page.
	ProviderName string
}

// RegistrationPage is the page on which a new person confirms a username.
type RegistrationPage struct {
	Token, Username, DisplayName, ProviderName, Error string
}

// callbackURL is the provider's fixed address on this server.
func (s *Service) callbackURL(p model.SsoProvider) string {
	if p.Kind == model.ProviderSAML {
		return s.cfg.Origin + "/api/v1/sso/" + p.ID + "/acs"
	}
	return s.cfg.Origin + "/api/v1/sso/" + p.ID + "/callback"
}

func (s *Service) callbackPath(p model.SsoProvider) string {
	return strings.TrimPrefix(s.callbackURL(p), s.cfg.Origin)
}

// ssoCookie makes the binding cookie, or with an empty value clears it.
// OIDC sends the browser back with a top-level GET, which carries a Lax
// cookie; SAML sends it back with a cross-site POST, which does not, so
// that one is SameSite=None.
func (s *Service) ssoCookie(p model.SsoProvider, value string) *http.Cookie {
	c := &http.Cookie{Name: SsoCookie, Value: value, Path: s.callbackPath(p), HttpOnly: true, Secure: s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode, MaxAge: int(waitingLife / time.Second)}
	if p.Kind == model.ProviderSAML && s.cfg.SecureCookies {
		c.SameSite = http.SameSiteNoneMode
	}
	if value == "" {
		c.MaxAge = -1
	}
	return c
}

// ClearSsoCookie is the cookie that deletes the binding after the callback.
func (s *Service) ClearSsoCookie(ctx context.Context, provider string) *http.Cookie {
	p, err := s.store.Identities().GetProvider(ctx, provider)
	if err != nil {
		return nil
	}
	return s.ssoCookie(p, "")
}

// localPath accepts only a path on this server as the place to go next.
func localPath(p string) string {
	if p == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.ContainsAny(p, "\\\r\n") {
		return "/"
	}
	if u, err := url.Parse(p); err != nil || u.Host != "" || u.Scheme != "" {
		return "/"
	}
	return p
}

// ssoFailed logs why a company sign-in failed and returns the one answer the
// person sees. The details are not shown, and nothing goes to the audit log,
// so an outsider cannot fill it.
func (s *Service) ssoFailed(p model.SsoProvider, addr, reason string, err error) error {
	s.log.Warn("sign-in failed", "method", p.Kind, "provider", p.Name, "address", addr, "reason", reason, "err", err)
	return model.ErrUnauthenticated.With("Sign-in through " + p.Name + " failed.")
}

// StartSso begins a company sign-in at a provider: it keeps the waiting
// request, and returns where to send the browser and the cookie that binds
// the answer to it. who is nil when nobody is signed in.
func (s *Service) StartSso(ctx context.Context, addr string, who *Who, providerID, purpose, invite, returnTo string) (SsoStart, error) {
	if err := limited(s.limitSsoStart, addr); err != nil {
		return SsoStart{}, err
	}
	if purpose == "" {
		purpose = PurposeSignIn
	}
	if purpose != PurposeSignIn && purpose != PurposeLink {
		return SsoStart{}, model.Invalid("purpose", "purpose is sign_in or link.")
	}
	p, err := s.store.Identities().GetProvider(ctx, providerID)
	if err != nil {
		return SsoStart{}, err
	}
	if !p.Enabled {
		return SsoStart{}, model.ErrNotFound
	}
	pol, err := s.policy(ctx, s.store)
	if err != nil {
		return SsoStart{}, err
	}
	if !pol.ssoOffered() {
		return SsoStart{}, model.ErrFeatureOff.With("Company sign-in is not offered on this server.")
	}
	value, hash := secret.NewLinkValue()
	w := waiting{provider: p.ID, binding: hash, purpose: purpose, returnTo: localPath(returnTo)}
	if purpose == PurposeLink {
		// A signed-in person with a fresh check, or a person in a
		// recovery session made by a link (docs/AUTH.md section 7.5).
		if who == nil {
			return SsoStart{}, model.ErrForbidden.With("Sign in before linking an identity.")
		}
		if err := person(*who); err != nil {
			return SsoStart{}, err
		}
		if who.Session.Recovery {
			if who.Session.SignedInBy != "recovery_link" {
				return SsoStart{}, model.ErrForbidden.With("Only a recovery link leads to linking an identity.")
			}
		} else if err := s.fresh(*who); err != nil {
			return SsoStart{}, err
		}
		w.account, w.session, w.recovery = who.Account.ID, who.Session.ID, who.Session.Recovery
	} else {
		w.invite = invite
	}
	handle, _ := secret.NewLinkValue()
	var location string
	switch p.Kind {
	case model.ProviderOIDC:
		location, err = s.oidcStart(ctx, p, handle, &w, pol)
	case model.ProviderSAML:
		location, err = s.samlStart(p, handle, &w)
	default:
		err = errors.New("auth: unknown provider kind " + p.Kind)
	}
	if err != nil {
		var me *model.Error
		if errors.As(err, &me) {
			return SsoStart{}, err
		}
		s.log.Warn("company sign-in could not be started", "provider", p.Name, "err", err)
		return SsoStart{}, model.ErrUnavailable.With("Sign-in through " + p.Name + " is not working at the moment.")
	}
	s.waiting.put(handle, w)
	return SsoStart{Location: location, Cookie: s.ssoCookie(p, value)}, nil
}

// takeWaiting takes a waiting request out of memory, so it cannot be
// answered twice, and checks that it was made for this provider by this
// browser. Without the browser check an attacker could finish a sign-in
// with their own identity and feed the answer to someone else's browser.
func (s *Service) takeWaiting(handle, provider, cookie string) (waiting, error) {
	if handle == "" {
		return waiting{}, errors.New("no request handle")
	}
	w, ok := s.waiting.take(handle)
	if !ok {
		return waiting{}, errors.New("no waiting request: unknown, expired or already used")
	}
	if w.provider != provider {
		return waiting{}, errors.New("the request was made for another provider")
	}
	sum := sha256.Sum256([]byte(cookie))
	if cookie == "" || subtle.ConstantTimeCompare(sum[:], w.binding) != 1 {
		return waiting{}, errors.New("the binding cookie does not match")
	}
	return w, nil
}

// finishSso is what follows a validated answer, with a provider and a
// subject in hand (docs/AUTH.md section 7.1).
func (s *Service) finishSso(ctx context.Context, addr string, p model.SsoProvider, w waiting, who asserted, label string) (SsoResult, error) {
	res := SsoResult{Next: w.returnTo, ProviderName: p.Name}
	// 1. A banned subject is refused before any account is found or made.
	banned, err := s.store.Accounts().IdentityBanned(ctx, p.ID, who.subject)
	if err != nil {
		return res, err
	}
	if banned {
		s.store.Audit().Append(ctx, model.AuditEntry{At: s.now(), ActorKind: "server", Action: "ban.sign_in_refused",
			TargetKind: "sso_provider", TargetID: p.ID, Detail: map[string]any{"provider": p.Name}})
		return res, errBanned
	}
	// 2. Find the account.
	identity, err := s.store.Identities().BySubject(ctx, p.ID, who.subject)
	found := err == nil
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return res, err
	}
	if w.purpose == PurposeLink {
		return s.finishLink(ctx, p, w, who, identity, found, label)
	}
	if found {
		acc, err := s.store.Accounts().Get(ctx, identity.AccountID)
		if err != nil || !acc.Active() {
			return res, s.ssoFailed(p, addr, "the linked account cannot sign in", err)
		}
		in, err := s.signInAs(ctx, p, acc, who, label, nil)
		if err != nil {
			return res, err
		}
		res.SignIn = &in
		return res, nil
	}
	return s.firstSignIn(ctx, addr, p, w, who, label)
}

// finishLink links the validated subject to the account that asked
// (docs/AUTH.md section 7.5).
func (s *Service) finishLink(ctx context.Context, p model.SsoProvider, w waiting, who asserted, identity model.Identity, found bool, label string) (SsoResult, error) {
	res := SsoResult{Next: w.returnTo, ProviderName: p.Name}
	ses, err := s.store.Sessions().Get(ctx, w.session)
	now := s.now()
	if err != nil || ses.AccountID != w.account || !ses.RevokedAt.IsZero() || (!ses.ExpiresAt.IsZero() && !ses.ExpiresAt.After(now)) {
		return res, model.ErrUnauthenticated.With("Your session ended before the identity could be linked. Sign in and try again.")
	}
	acc, err := s.store.Accounts().Get(ctx, w.account)
	if err != nil || !acc.Active() {
		return res, model.ErrUnauthenticated
	}
	if found && identity.AccountID != acc.ID {
		return res, model.ErrConflict.With("That identity is already linked to another account.")
	}
	link := func(tx store.Tx) error {
		if found {
			return nil
		}
		if err := tx.Identities().Link(ctx, model.Identity{ProviderID: p.ID, Subject: who.subject, AccountID: acc.ID, LinkedAt: now}); err != nil {
			if errors.Is(err, model.ErrAlreadyExists) {
				return model.ErrConflict.With("That identity is already linked to another account.")
			}
			return err
		}
		return s.audit(ctx, tx, acc.ID, "identity.linked", "account", string(acc.ID), map[string]any{"provider": p.Name})
	}
	if !w.recovery {
		return res, s.store.Tx(ctx, link)
	}
	// From a recovery link: the person has just passed the provider, so
	// this is their way back in. The recovery session is replaced.
	in, err := s.signInAs(ctx, p, acc, who, label, func(tx store.Tx) error {
		if err := link(tx); err != nil {
			return err
		}
		return tx.Sessions().Revoke(ctx, w.session, now)
	})
	if err != nil {
		return res, err
	}
	s.sessionsEnded(ctx, "replaced", store.SessionRef{ID: w.session, AccountID: acc.ID})
	res.SignIn = &in
	return res, nil
}

// emailHash is the keyed hash of an email address in `import_claims`, so a
// copy of the database alone cannot be tested against a list of addresses.
func (s *Service) emailHash(email string) []byte { return ImportEmailHashWith(s.keys, email) }

// ImportEmailHashWith is the same hash for a caller that has the server's
// keys and no running service: the Slack importer, which runs with the
// server stopped. Both must agree, or nobody could claim by company sign-in.
func ImportEmailHashWith(keys *secret.Keys, email string) []byte {
	return keys.MAC("import-email", []byte(strings.ToLower(strings.TrimSpace(email))))
}

// ImportEmailHash is the hash the importer stores for an account's address.
func (s *Service) ImportEmailHash(email string) []byte { return s.emailHash(email) }

// firstSignIn handles a subject no account has: it claims an imported
// account, or starts a registration, or says the server is not taking new
// accounts (docs/AUTH.md section 7.4).
func (s *Service) firstSignIn(ctx context.Context, addr string, p model.SsoProvider, w waiting, who asserted, label string) (SsoResult, error) {
	res := SsoResult{Next: w.returnTo, ProviderName: p.Name}
	if who.email != "" {
		if waitingClaims, err := s.store.Identities().HasImportClaims(ctx); err != nil {
			return res, err
		} else if waitingClaims {
			in, claimed, err := s.claimImported(ctx, p, who, label)
			if err != nil {
				return res, err
			}
			if claimed {
				res.SignIn = &in
				return res, nil
			}
		}
	}
	who.email = ""
	mode, err := store.SettingString(ctx, s.store, store.SettingRegistrationMode, "invite")
	if err != nil {
		return res, err
	}
	reg := registration{provider: p, who: who, w: w, role: model.RoleMember}
	if w.invite != "" {
		// A claim link carried through company sign-in takes over the
		// imported account it names, in every registration mode: no
		// account is made (claim.go).
		if inv, err := s.people.CheckInvite(ctx, w.invite); err == nil && inv.ClaimsAccountID != "" {
			in, err := s.claimByProvider(ctx, p, who, inv, label)
			if err != nil {
				return res, err
			}
			res.SignIn = &in
			return res, nil
		}
	}
	switch {
	case mode == "closed":
		return res, model.ErrForbidden.With("This server is not taking new accounts.")
	case w.invite != "":
		inv, err := s.people.CheckInvite(ctx, w.invite)
		if err != nil {
			return res, model.ErrForbidden.With("That invite can no longer be used.")
		}
		reg.invite, reg.role = inv.ID, inv.Role
	case mode != "open":
		return res, model.ErrForbidden.With("This server is not taking new accounts without an invite.")
	}
	token, _ := secret.NewLinkValue()
	s.registrations.put(token, reg)
	page := &RegistrationPage{Token: token, DisplayName: who.name, ProviderName: p.Name}
	// The provider's suggestion, lower-cased and cut to the username
	// rule, is filled in if it is free.
	if u := suggestUsername(who.username); u != "" && s.nameFree(ctx, u) == nil {
		page.Username = u
	}
	res.Register = page
	return res, nil
}

// suggestUsername makes a username out of what the provider suggested, or
// "" if nothing usable is left.
func suggestUsername(raw string) string {
	raw = strings.ToLower(raw)
	if i := strings.IndexByte(raw, '@'); i > 0 {
		raw = raw[:i]
	}
	var b strings.Builder
	for _, r := range raw {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (b.Len() > 0 && (r == '.' || r == '_' || r == '-'))
		if ok && b.Len() < 32 {
			b.WriteRune(r)
		}
	}
	if u := b.String(); model.ValidUsername(u) && !model.ReservedName(u) {
		return u
	}
	return ""
}

// claimImported claims an imported account whose stored address matches the
// one the provider vouches for. The address is then dropped.
func (s *Service) claimImported(ctx context.Context, p model.SsoProvider, who asserted, label string) (SignIn, bool, error) {
	hash := s.emailHash(who.email)
	who.email = ""
	var (
		in      SignIn
		claimed bool
	)
	err := s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		id, found, err := tx.Identities().TakeImportClaim(ctx, hash)
		if err != nil || !found {
			return err
		}
		acc, err := tx.Accounts().Get(ctx, id)
		if err != nil {
			return err
		}
		if !acc.ClaimedAt.IsZero() || acc.Removed() || !acc.Local() {
			// Claimed by link meanwhile, or removed: nothing to take over.
			return nil
		}
		now := s.now()
		joined, err := tx.Accounts().Claim(ctx, id, now)
		if err != nil {
			return err
		}
		if err := rec.Membership(ctx, joined, "", ""); err != nil {
			return err
		}
		if acc, err = tx.Accounts().Get(ctx, id); err != nil {
			return err
		}
		if err := tx.Identities().Link(ctx, model.Identity{ProviderID: p.ID, Subject: who.subject, AccountID: id, LinkedAt: now}); err != nil {
			return err
		}
		// Claimed by whoever the provider vouches for: said in the
		// account's conversations as a claim link's use is (claimAlert).
		if err := rec.Alert(ctx, chat.AlertClaimed, chat.Alert{Account: id}); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, id, "account.claimed", "account", string(id), map[string]any{"by": "provider", "provider": p.Name}); err != nil {
			return err
		}
		claimed = true
		in, err = s.signInTx(ctx, tx, rec, p, acc, who, label)
		return err
	})
	if err == nil && claimed {
		s.signedInBySso(ctx, &in)
	}
	return in, claimed, err
}

// CompleteRegistration makes the account for a first company sign-in, with
// the username the person chose, and signs it in. A name that cannot be
// used gives the page back with the reason.
func (s *Service) CompleteRegistration(ctx context.Context, addr, token, username, displayName, label string) (SsoResult, error) {
	reg, ok := s.registrations.get(token)
	if !ok {
		return SsoResult{}, model.ErrNotFound.With("That took too long. Start signing in again.")
	}
	p := reg.provider
	res := SsoResult{Next: reg.w.returnTo, ProviderName: p.Name}
	again := func(msg string) (SsoResult, error) {
		res.Register = &RegistrationPage{Token: token, Username: username, DisplayName: displayName, ProviderName: p.Name, Error: msg}
		return res, nil
	}
	if displayName == "" {
		displayName = reg.who.name
	}
	uname, dname, err := people.CleanNames(username, displayName)
	if err != nil {
		return again(messageOf(err))
	}
	if err := s.nameFree(ctx, uname); err != nil {
		return again(messageOf(err))
	}
	if reg.invite == "" {
		// Accounts made without an invite are limited per address.
		if err := limited(s.limitOpenAccounts, addr); err != nil {
			return res, err
		}
	}
	var in SignIn
	err = s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		now := s.now()
		// The identity may have been banned while the person chose a
		// name: a banned identity makes no account.
		if banned, err := tx.Accounts().IdentityBanned(ctx, p.ID, reg.who.subject); err != nil {
			return err
		} else if banned {
			return errBanned
		}
		var inv model.Invite
		if reg.invite != "" {
			if err := tx.Invites().Use(ctx, reg.invite, now); err != nil {
				return err
			}
			var err error
			if inv, err = tx.Invites().Get(ctx, reg.invite); err != nil {
				return err
			}
		}
		// No user handle, passkey or recovery code is made for an account
		// created this way; it gets them if it later adds a passkey.
		acc, joined, err := s.people.CreateTx(ctx, tx, people.NewAccount{Username: uname, DisplayName: dname, Role: reg.role})
		if err != nil {
			return err
		}
		if err := tx.Identities().Link(ctx, model.Identity{ProviderID: p.ID, Subject: reg.who.subject, AccountID: acc.ID, LinkedAt: now}); err != nil {
			return err
		}
		more, err := applyInvite(ctx, tx, inv, acc.ID, now)
		if err != nil {
			return err
		}
		joined.Merge(more)
		detail := map[string]any{"how": p.Name}
		if reg.invite != "" {
			detail["invite_id"] = reg.invite
		}
		if err := s.audit(ctx, tx, acc.ID, "account.registered", "account", string(acc.ID), detail); err != nil {
			return err
		}
		if err := rec.Membership(ctx, joined, inv.CreatedByID, ""); err != nil {
			return err
		}
		in, err = s.signInTx(ctx, tx, rec, p, acc, reg.who, label)
		return err
	})
	switch {
	case err == errBanned:
		s.registrations.drop(token)
		s.store.Audit().Append(ctx, model.AuditEntry{At: s.now(), ActorKind: "server", Action: "ban.sign_in_refused",
			TargetKind: "sso_provider", TargetID: p.ID, Detail: map[string]any{"provider": p.Name}})
		return res, err
	case errors.Is(err, model.ErrNameTaken):
		return again(messageOf(err))
	case errors.Is(err, model.ErrAlreadyExists):
		s.registrations.drop(token)
		return res, model.ErrConflict.With("That identity already has an account. Sign in again.")
	case errors.Is(err, model.ErrNotFound) && reg.invite != "":
		s.registrations.drop(token)
		return res, model.ErrForbidden.With("That invite can no longer be used.")
	case err != nil:
		return res, err
	}
	s.registrations.drop(token)
	s.signedInBySso(ctx, &in)
	res.SignIn = &in
	return res, nil
}

func messageOf(err error) string {
	if e, ok := model.AsError(err); ok {
		return e.Message
	}
	return "That could not be used."
}

// applyInvite puts a new account where its invite says: the invite's groups,
// and for a guest the named channels at each channel's default level.
func applyInvite(ctx context.Context, tx store.Tx, inv model.Invite, account model.AccountID, now model.Time) (store.MembershipResult, error) {
	var res store.MembershipResult
	for _, g := range inv.GroupIDs {
		r, err := tx.Groups().AddMember(ctx, g, account, inv.CreatedByID, now)
		if errors.Is(err, model.ErrNotFound) {
			continue // the group was deleted since the invite was made
		}
		if err != nil {
			return res, err
		}
		res.Merge(r)
	}
	for _, cid := range inv.ConversationIDs {
		conv, err := tx.Conversations().Get(ctx, cid)
		if errors.Is(err, model.ErrNotFound) {
			continue
		}
		if err != nil {
			return res, err
		}
		r, err := tx.Members().AddDirect(ctx, cid, account, conv.DefaultLevel, now)
		if errors.Is(err, model.ErrLocked) || errors.Is(err, model.ErrAlreadyExists) {
			continue
		}
		if err != nil {
			return res, err
		}
		res.Merge(r)
	}
	return res, nil
}

// signInAs runs signInTx in a transaction of its own, after before (if any).
func (s *Service) signInAs(ctx context.Context, p model.SsoProvider, acc model.Account, who asserted, label string, before func(tx store.Tx) error) (SignIn, error) {
	var in SignIn
	err := s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		if before != nil {
			if err := before(tx); err != nil {
				return err
			}
		}
		var err error
		in, err = s.signInTx(ctx, tx, rec, p, acc, who, label)
		return err
	})
	if err == nil {
		s.signedInBySso(ctx, &in)
	}
	return in, err
}

// signedInBySso finishes a company sign-in after its transaction: the
// second-factor state for a pending session, and the account's other
// connections told.
func (s *Service) signedInBySso(ctx context.Context, in *SignIn) {
	if in.Session.PendingSecondFactor {
		if st, err := s.secondFactorState(ctx, in.Account.ID); err == nil {
			in.SecondFactor = &st
		}
	}
	s.sessionStarted(in.Session)
}

// signInTx is steps 3 to 6 of docs/AUTH.md section 7.1 for an account that
// has just been identified by a provider: groups and the admin role from
// the provider, the identity's last use, the second factor, and the session.
func (s *Service) signInTx(ctx context.Context, tx store.Tx, rec *chat.Recorder, p model.SsoProvider, acc model.Account, who asserted, label string) (SignIn, error) {
	pol, err := s.policy(ctx, tx)
	if err != nil {
		return SignIn{}, err
	}
	if !pol.ssoOffered() {
		return SignIn{}, model.ErrForbidden.With("Company sign-in is not offered on this server.")
	}
	now := s.now()
	if acc, err = s.syncFromProvider(ctx, tx, rec, p, acc, who, now); err != nil {
		return SignIn{}, err
	}
	if err := tx.Identities().Touch(ctx, p.ID, who.subject, now); err != nil {
		return SignIn{}, err
	}
	life, err := s.ssoLife(ctx, tx)
	if err != nil {
		return SignIn{}, err
	}
	// The hard end: fixed from the sign-in, never moved by use, and
	// earlier if SAML says the provider's own session ends sooner.
	end := now.Add(life)
	if !who.sessionEnd.IsZero() && who.sessionEnd.Before(end) {
		end = who.sessionEnd
	}
	ses := model.Session{AccountID: acc.ID, Label: label, SignedInBy: who.by, CreatedAt: now, ExpiresAt: end, MaxExpiresAt: end}
	if pol.SecondRequired {
		switch {
		case pol.Accepted["provider"] && who.mfa:
			// The provider's own statement counts only because the
			// admin ticked it.
			ses.SecondFactor = "provider"
		case !pol.Accepted["passkey"] && !pol.Accepted["totp"]:
			return SignIn{}, model.ErrForbidden.With("This server needs a second sign-in step, and " + p.Name +
				" did not say it used one. Ask an admin: the provider must state multi-factor sign-in, or another second step must be accepted.")
		default:
			ses.PendingSecondFactor = true
			if pend := now.Add(pendingLife); pend.Before(end) {
				ses.ExpiresAt = pend
			}
		}
	}
	if !ses.PendingSecondFactor {
		ses.FreshAt = now
	}
	token, err := s.webSession(ctx, tx, &ses)
	return SignIn{Session: ses, Account: acc, Token: token}, err
}

// syncFromProvider brings group membership and the admin role into line
// with the provider's group names (docs/AUTH.md section 7.6). An absent
// claim changes nothing: it is treated as a misconfiguration, not as "in
// no groups", so one bad answer cannot demote every admin.
func (s *Service) syncFromProvider(ctx context.Context, tx store.Tx, rec *chat.Recorder, p model.SsoProvider, acc model.Account, who asserted, now model.Time) (model.Account, error) {
	groups, err := tx.Groups().List(ctx, "")
	if err != nil {
		return acc, err
	}
	var mapped []model.Group
	for _, g := range groups {
		if g.ProviderID == p.ID && g.ProviderGroup != "" {
			mapped = append(mapped, g)
		}
	}
	if len(mapped) == 0 && p.AdminGroup == "" {
		return acc, nil
	}
	if !who.hasGroups {
		s.log.Warn("the provider's answer carried no group claim; groups and roles are left as they are", "provider", p.Name)
		return acc, nil
	}
	listed := map[string]bool{}
	for _, g := range who.groups {
		listed[g] = true // names are compared exactly
	}
	var res store.MembershipResult
	for _, g := range mapped {
		is, err := tx.Groups().IsMember(ctx, g.ID, acc.ID)
		if err != nil {
			return acc, err
		}
		var r store.MembershipResult
		switch {
		case listed[g.ProviderGroup] && !is:
			r, err = tx.Groups().AddMember(ctx, g.ID, acc.ID, "", now)
		case !listed[g.ProviderGroup] && is:
			r, err = tx.Groups().RemoveMember(ctx, g.ID, acc.ID)
		}
		if err != nil {
			return acc, err
		}
		res.Merge(r)
		if added := listed[g.ProviderGroup]; added != is {
			// Everyone who sees the group is told, as when an admin
			// changes who is in it.
			group, account := g.ID, acc.ID
			rec.After(func() { s.people.AnnounceGroupMember(context.WithoutCancel(ctx), group, account, added) })
		}
	}
	// The owner's role is never changed, and a guest is never promoted.
	if p.AdminGroup != "" && (acc.Role == model.RoleAdmin || acc.Role == model.RoleMember) {
		want := model.RoleMember
		if listed[p.AdminGroup] {
			want = model.RoleAdmin
		}
		if want != acc.Role {
			old := acc.Role
			updated, r, err := tx.Accounts().SetRole(ctx, acc.ID, want)
			if err != nil {
				return acc, err
			}
			res.Merge(r)
			acc = updated
			if err := tx.Audit().Append(ctx, model.AuditEntry{At: now, ActorKind: "server", Action: "account.role_changed",
				TargetKind: "account", TargetID: string(acc.ID),
				Detail: map[string]any{"old": old, "new": want, "cause": "provider group", "provider": p.Name}}); err != nil {
				return acc, err
			}
			// account.updated, as for a role an admin changed.
			account := acc.ID
			rec.After(func() { s.people.AnnounceAccount(context.WithoutCancel(ctx), account) })
		}
	}
	return acc, rec.Membership(ctx, res, "", "")
}

// --- Providers, for admins ---

// ProviderInput is the body of creating or changing a provider (schema
// SsoProviderInput). A nil field is left as it is.
type ProviderInput struct {
	Kind       *string         `json:"kind"`
	Name       *string         `json:"name"`
	Config     json.RawMessage `json:"config"`
	Secret     *string         `json:"secret"`
	GroupClaim *string         `json:"group_claim"`
	AdminGroup *string         `json:"admin_group"`
	Enabled    *bool           `json:"enabled"`
}

func (s *Service) shown(p model.SsoProvider) model.SsoProvider {
	p.CallbackURL = s.callbackURL(p)
	p.HasSecret = len(p.Secret) > 0
	if len(p.Config) == 0 {
		p.Config = json.RawMessage(`{}`)
	}
	return p
}

// ProviderSummaries lists the providers offered on the sign-in page: the
// enabled ones, and none while member sign-in is `passkey`.
func (s *Service) ProviderSummaries(ctx context.Context) ([]map[string]any, error) {
	out := []map[string]any{}
	pol, err := s.policy(ctx, s.store)
	if err != nil || !pol.ssoOffered() {
		return out, err
	}
	list, err := s.store.Identities().ListProviders(ctx)
	for _, p := range list {
		if p.Enabled {
			out = append(out, map[string]any{"id": p.ID, "kind": p.Kind, "name": p.Name})
		}
	}
	return out, err
}

// ListProviders returns every provider, for admins.
func (s *Service) ListProviders(ctx context.Context, who Who) ([]model.SsoProvider, error) {
	if err := s.adminOnly(ctx, who); err != nil {
		return nil, err
	}
	list, err := s.store.Identities().ListProviders(ctx)
	out := make([]model.SsoProvider, 0, len(list))
	for _, p := range list {
		out = append(out, s.shown(p))
	}
	return out, err
}

// adminOnly is an account that may change settings. Reading them is open to
// an admin's personal token; every change that matters asks for a fresh
// check, which a token cannot give.
func (s *Service) adminOnly(ctx context.Context, who Who) error {
	return s.admin(ctx, who, perm.ChangeSettings)
}

// GetProvider returns one provider, for admins.
func (s *Service) GetProvider(ctx context.Context, who Who, id string) (model.SsoProvider, error) {
	if err := s.adminOnly(ctx, who); err != nil {
		return model.SsoProvider{}, err
	}
	p, err := s.store.Identities().GetProvider(ctx, id)
	return s.shown(p), err
}

func cleanName(v string, max int) (string, bool) {
	v = strings.TrimSpace(v)
	return v, v != "" && len(v) <= max && !strings.ContainsAny(v, "\r\n\x00")
}

// sealProviderSecret seals a provider's secret to its row.
func (s *Service) sealProviderSecret(id string, plain []byte) ([]byte, error) {
	return s.keys.Seal(plain, "sso_providers", "secret", id)
}

func (s *Service) providerSecret(p model.SsoProvider) ([]byte, error) {
	if len(p.Secret) == 0 {
		return nil, nil
	}
	return s.keys.Unseal(p.Secret, "sso_providers", "secret", p.ID)
}

// CreateProvider adds a provider. For OIDC the discovery document is
// fetched now; for SAML the metadata is loaded once and not refreshed.
func (s *Service) CreateProvider(ctx context.Context, who Who, in ProviderInput) (model.SsoProvider, error) {
	if err := s.adminOnly(ctx, who); err != nil {
		return model.SsoProvider{}, err
	}
	if err := s.fresh(who); err != nil {
		return model.SsoProvider{}, err
	}
	if in.Kind == nil || (*in.Kind != model.ProviderOIDC && *in.Kind != model.ProviderSAML) {
		return model.SsoProvider{}, model.Invalid("kind", "kind is oidc or saml.")
	}
	if in.Name == nil {
		return model.SsoProvider{}, model.Invalid("name", "A provider needs a name of 1 to 64 characters.")
	}
	p := model.SsoProvider{ID: model.NewID[string](), Kind: *in.Kind, Enabled: true, CreatedAt: s.now()}
	changed, err := s.applyProviderInput(ctx, &p, in, true)
	if err != nil {
		return model.SsoProvider{}, err
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Identities().CreateProvider(ctx, &p); err != nil {
			return err
		}
		return s.audit(ctx, tx, who.Account.ID, "sso_provider.added", "sso_provider", p.ID, map[string]any{"name": p.Name, "kind": p.Kind, "fields": changed})
	})
	return s.shown(p), err
}

// applyProviderInput validates the fields given and writes them into p. It
// returns the names of the fields that were given, for the audit log, which
// never holds the secret.
func (s *Service) applyProviderInput(ctx context.Context, p *model.SsoProvider, in ProviderInput, creating bool) ([]string, error) {
	changed := []string{}
	if in.Kind != nil && *in.Kind != p.Kind {
		return nil, model.Invalid("kind", "A provider's kind cannot be changed. Add a new provider instead.")
	}
	if in.Name != nil {
		name, ok := cleanName(*in.Name, 64)
		if !ok {
			return nil, model.Invalid("name", "A provider needs a name of 1 to 64 characters.")
		}
		p.Name = name
		changed = append(changed, "name")
	}
	for _, f := range []struct {
		name string
		in   *string
		to   *string
	}{{"group_claim", in.GroupClaim, &p.GroupClaim}, {"admin_group", in.AdminGroup, &p.AdminGroup}} {
		if f.in == nil {
			continue
		}
		v := strings.TrimSpace(*f.in)
		if len(v) > 255 {
			return nil, model.Invalid(f.name, "That is too long.")
		}
		*f.to = v
		changed = append(changed, f.name)
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
		changed = append(changed, "enabled")
	}
	if len(in.Config) > 0 && !bytes.Equal(bytes.TrimSpace(in.Config), []byte("null")) {
		changed = append(changed, "config")
	} else if creating {
		return nil, model.Invalid("config", "A provider needs its config.")
	}
	var err error
	switch p.Kind {
	case model.ProviderOIDC:
		if in.Secret != nil {
			if p.Secret, err = s.sealProviderSecret(p.ID, []byte(*in.Secret)); err != nil {
				return nil, err
			}
			if *in.Secret == "" {
				p.Secret = nil
			}
			changed = append(changed, "secret")
		}
		if contains(changed, "config") {
			if p.Config, err = s.oidcConfigure(ctx, in.Config); err != nil {
				return nil, err
			}
			s.oidcForget(p.ID)
		}
	case model.ProviderSAML:
		if in.Secret != nil {
			return nil, model.Invalid("secret", "A SAML provider has no secret to enter. The server makes its own signing key.")
		}
		if contains(changed, "config") {
			if p.Config, p.Secret, err = s.samlConfigure(ctx, *p, in.Config); err != nil {
				return nil, err
			}
		}
	}
	return changed, nil
}

// lastEnabledUnderSso reports whether taking this provider away would leave
// a server whose members must use company sign-in with no provider.
func (s *Service) lastEnabledUnderSso(ctx context.Context, a store.Areas, id string) (bool, error) {
	pol, err := s.policy(ctx, a)
	if err != nil || pol.MemberSignIn != "sso" {
		return false, err
	}
	list, err := a.Identities().ListProviders(ctx)
	if err != nil {
		return false, err
	}
	for _, p := range list {
		if p.Enabled && p.ID != id {
			return false, nil
		}
	}
	return true, nil
}

// UpdateProvider changes a provider.
func (s *Service) UpdateProvider(ctx context.Context, who Who, id string, in ProviderInput) (model.SsoProvider, error) {
	if err := s.adminOnly(ctx, who); err != nil {
		return model.SsoProvider{}, err
	}
	p, err := s.store.Identities().GetProvider(ctx, id)
	if err != nil {
		return model.SsoProvider{}, err
	}
	if err := s.fresh(who); err != nil {
		return model.SsoProvider{}, err
	}
	wasEnabled := p.Enabled
	changed, err := s.applyProviderInput(ctx, &p, in, false)
	if err != nil {
		return model.SsoProvider{}, err
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if wasEnabled && !p.Enabled {
			if last, err := s.lastEnabledUnderSso(ctx, tx, p.ID); err != nil {
				return err
			} else if last {
				return model.ErrConflict.With("Members must sign in through a provider, and this is the last one switched on.")
			}
		}
		if err := tx.Identities().UpdateProvider(ctx, p); err != nil {
			return err
		}
		return s.audit(ctx, tx, who.Account.ID, "sso_provider.changed", "sso_provider", p.ID, map[string]any{"name": p.Name, "fields": changed})
	})
	return s.shown(p), err
}

// DeleteProvider removes a provider and its identities.
func (s *Service) DeleteProvider(ctx context.Context, who Who, id string) error {
	if err := s.adminOnly(ctx, who); err != nil {
		return err
	}
	p, err := s.store.Identities().GetProvider(ctx, id)
	if err != nil {
		return err
	}
	if err := s.fresh(who); err != nil {
		return err
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if p.Enabled {
			if last, err := s.lastEnabledUnderSso(ctx, tx, p.ID); err != nil {
				return err
			} else if last {
				return model.ErrConflict.With("Members must sign in through a provider, and this is the last one switched on.")
			}
		}
		if err := tx.Identities().DeleteProvider(ctx, p.ID); err != nil {
			return err
		}
		return s.audit(ctx, tx, who.Account.ID, "sso_provider.removed", "sso_provider", p.ID, map[string]any{"name": p.Name})
	})
	s.oidcForget(p.ID)
	return err
}

// TestProvider fetches the discovery document and key set (OIDC) or checks
// the stored metadata (SAML), and says what it found. It signs nobody in.
func (s *Service) TestProvider(ctx context.Context, who Who, id string) (ok bool, problem string, err error) {
	if err := s.adminOnly(ctx, who); err != nil {
		return false, "", err
	}
	p, err := s.store.Identities().GetProvider(ctx, id)
	if err != nil {
		return false, "", err
	}
	var terr error
	if p.Kind == model.ProviderOIDC {
		terr = s.oidcTest(ctx, p)
	} else {
		terr = s.samlTest(p)
	}
	if terr != nil {
		return false, terr.Error(), nil
	}
	return true, "", nil
}

// ListIdentities returns the caller's linked identities.
func (s *Service) ListIdentities(ctx context.Context, who Who) ([]model.Identity, error) {
	if err := complete(who); err != nil {
		return nil, err
	}
	return s.store.Identities().ForAccount(ctx, who.Account.ID)
}
