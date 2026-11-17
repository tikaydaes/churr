package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// How the apps sign in: OAuth 2.0 authorization code with PKCE in the
// system browser, or QR pairing from a signed-in web page (docs/AUTH.md
// section 11). Clients are public and unregistered, so the redirect address
// is what stops a web page from collecting codes.

// GrantPairing is the grant type of QR pairing.
const GrantPairing = "urn:churr:params:oauth:grant-type:pairing-code"

// AppScheme is the apps' own address scheme, for redirects on phones and
// for the text of the pairing QR code.
const AppScheme = "churr"

// OAuthMetadata is the authorization server metadata of RFC 8414.
func (s *Service) OAuthMetadata() map[string]any {
	o := s.cfg.Origin
	return map[string]any{
		"issuer":                                         o,
		"authorization_endpoint":                         o + "/oauth/authorize",
		"token_endpoint":                                 o + "/oauth/token",
		"revocation_endpoint":                            o + "/oauth/revoke",
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token", GrantPairing},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"revocation_endpoint_auth_methods_supported":     []string{"none"},
		"authorization_response_iss_parameter_supported": true,
	}
}

// AuthorizeRequest is a checked request to sign an app in.
type AuthorizeRequest struct {
	ClientID, RedirectURI, CodeChallenge, State, DeviceName, Platform string
	BoxKey                                                            []byte
	// Handover is the backup key sealed to BoxKey by the approval page,
	// when the person signed in there with a passkey that can unlock
	// (sealed.go). The server cannot read it.
	Handover []byte
}

var appPlatforms = map[string]bool{"android": true, "ios": true, "macos": true, "windows": true, "linux": true}

// redirectAllowed accepts exactly the forms of docs/AUTH.md section 11.1: a
// loopback address by number with a port, for desktop apps, or the apps' own
// scheme, for phones. No fragment in either, and never https.
func redirectAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || strings.Contains(raw, "#") || u.User != nil || u.Opaque != "" {
		return false
	}
	switch u.Scheme {
	case "http":
		host, port, err := net.SplitHostPort(u.Host)
		if err != nil || port == "" {
			return false
		}
		// The name `localhost` is not accepted: only the addresses.
		return host == "127.0.0.1" || host == "::1"
	case AppScheme:
		return true
	}
	return false
}

func deviceName(v string) string {
	v = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, v))
	if r := []rune(v); len(r) > 60 {
		v = string(r[:60])
	}
	if v == "" {
		v = "Churr app"
	}
	return v
}

func boxKey(v string) ([]byte, bool) {
	if v == "" {
		return nil, true
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(v, "="))
	return b, err == nil && len(b) == 32
}

// CheckAuthorize checks the redirect address and the PKCE values of an
// authorization request, before anything else is done with it. The page
// never redirects to an address this has not accepted.
func (s *Service) CheckAuthorize(addr string, q url.Values) (AuthorizeRequest, error) {
	if err := limited(s.limitAuthorize, addr); err != nil {
		return AuthorizeRequest{}, err
	}
	req := AuthorizeRequest{ClientID: q.Get("client_id"), RedirectURI: q.Get("redirect_uri"), CodeChallenge: q.Get("code_challenge"),
		State: q.Get("state"), DeviceName: deviceName(q.Get("device_name")), Platform: q.Get("platform")}
	if !redirectAllowed(req.RedirectURI) {
		return req, model.Invalid("redirect_uri", "An app is sent back to http://127.0.0.1:<port>/… or http://[::1]:<port>/…, or to the apps' own address scheme. Nothing else is accepted.")
	}
	if q.Get("response_type") != "code" {
		return req, model.Invalid("response_type", "response_type is code.")
	}
	if req.ClientID == "" || len(req.ClientID) > 200 {
		return req, model.Invalid("client_id", "client_id is needed.")
	}
	if q.Get("code_challenge_method") != "S256" {
		return req, model.Invalid("code_challenge_method", "code_challenge_method is S256.")
	}
	if n := len(req.CodeChallenge); n < 43 || n > 128 {
		return req, model.Invalid("code_challenge", "code_challenge is the base64url SHA-256 of the verifier, at least 43 characters.")
	}
	if req.Platform != "" && !appPlatforms[req.Platform] {
		return req, model.Invalid("platform", "platform is android, ios, macos, windows or linux.")
	}
	if len(req.State) > 1024 {
		return req, model.Invalid("state", "state is too long.")
	}
	var ok bool
	if req.BoxKey, ok = boxKey(q.Get("box_key")); !ok {
		return req, model.Invalid("box_key", "box_key is 32 bytes in base64url.")
	}
	return req, nil
}

func codeKey(hash []byte) string { return hex.EncodeToString(hash) }

// redirectWith adds parameters to an accepted redirect address.
func redirectWith(raw string, params map[string]string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Approve issues an authorization code for an app the person approved on
// the server's own page, and returns the address to send the browser to.
// endSession says the browser session was made only for this, and is
// revoked now: signing an app in does not leave a browser signed in.
func (s *Service) Approve(ctx context.Context, who Who, req AuthorizeRequest, endSession bool) (string, error) {
	if !who.Cookie {
		return "", model.ErrUnauthenticated.With("This needs a browser session.")
	}
	if err := complete(who); err != nil {
		return "", err
	}
	if !redirectAllowed(req.RedirectURI) {
		return "", model.Invalid("redirect_uri", "That redirect address is not accepted.")
	}
	code, hash := secret.NewLinkValue()
	now := s.now()
	err := s.store.Identities().CreateCode(ctx, hash, store.OAuthCode{AccountID: who.Account.ID, Kind: store.CodeAuthorization,
		ClientName: req.DeviceName, RedirectURI: req.RedirectURI, PKCEChallenge: req.CodeChallenge, Platform: req.Platform,
		BoxKey: req.BoxKey, Handover: req.Handover, ExpiresAt: now.Add(codeLife)})
	if err != nil {
		return "", err
	}
	s.grants.put(codeKey(hash), grant{signedInBy: who.Session.SignedInBy, secondFactor: who.Session.SecondFactor, max: who.Session.MaxExpiresAt})
	if endSession {
		if err := s.Logout(ctx, who); err != nil {
			s.log.Warn("the browser session made for an app sign-in could not be ended", "err", err)
		}
	}
	return redirectWith(req.RedirectURI, map[string]string{"code": code, "state": req.State, "iss": s.cfg.Origin}), nil
}

// Deny is where the browser goes when the person says no.
func (s *Service) Deny(req AuthorizeRequest) string {
	return redirectWith(req.RedirectURI, map[string]string{"error": "access_denied", "state": req.State, "iss": s.cfg.Origin})
}

// OAuthError is a token endpoint error in the shape OAuth libraries expect.
type OAuthError struct {
	Status      int    `json:"-"`
	Code        string `json:"error"`
	Description string `json:"error_description,omitempty"`
	RetryAfter  int    `json:"-"`
}

func (e *OAuthError) Error() string { return "oauth: " + e.Code + ": " + e.Description }

func oauthErr(code, desc string) *OAuthError {
	return &OAuthError{Status: 400, Code: code, Description: desc}
}

var (
	errInvalidGrant = oauthErr("invalid_grant", "That code or token is not valid, has expired, or has been used.")
	errPending      = oauthErr("authorization_pending", "Waiting for the person to confirm on the signed-in page.")
)

// OAuthTokens is the token response of RFC 6749, with two additions (schema
// OAuthTokens).
type OAuthTokens struct {
	AccessToken  string         `json:"access_token"`
	TokenType    string         `json:"token_type"`
	ExpiresIn    int64          `json:"expires_in"`
	RefreshToken string         `json:"refresh_token"`
	DeviceID     model.DeviceID `json:"device_id,omitempty"`
	// Handover is the backup key sealed to the app's box key, when the
	// page that approved could open it. The server cannot read it.
	Handover string `json:"handover,omitempty"`
}

// Token is the token endpoint: it exchanges an authorization code, a
// refresh token or a confirmed pairing for tokens. Errors are *OAuthError.
func (s *Service) Token(ctx context.Context, addr string, form url.Values) (OAuthTokens, error) {
	if ok, retry := s.limitToken.Allow(ratelimit.AddressKey(addr)); !ok {
		return OAuthTokens{}, &OAuthError{Status: 429, Code: "slow_down", Description: "Too many requests.", RetryAfter: int(retry/time.Second) + 1}
	}
	switch form.Get("grant_type") {
	case "authorization_code":
		return s.exchangeCode(ctx, form)
	case "refresh_token":
		return s.refresh(ctx, form.Get("refresh_token"))
	case GrantPairing:
		return s.exchangePairing(ctx, form)
	case "":
		return OAuthTokens{}, oauthErr("invalid_request", "grant_type is needed.")
	}
	return OAuthTokens{}, oauthErr("unsupported_grant_type", "grant_type is authorization_code, refresh_token or "+GrantPairing+".")
}

func (s *Service) exchangeCode(ctx context.Context, form url.Values) (OAuthTokens, error) {
	code, verifier, redirect := form.Get("code"), form.Get("code_verifier"), form.Get("redirect_uri")
	if code == "" || verifier == "" || redirect == "" {
		return OAuthTokens{}, oauthErr("invalid_request", "code, code_verifier and redirect_uri are needed.")
	}
	hash := secret.HashToken(code)
	// The code is spent here, even if the checks after it fail.
	c, ok, err := s.store.Identities().UseCode(ctx, hash, s.now())
	if err != nil {
		return OAuthTokens{}, err
	}
	g, granted := s.grants.take(codeKey(hash))
	if !ok || c.Kind != store.CodeAuthorization || !granted {
		return OAuthTokens{}, errInvalidGrant
	}
	sum := sha256.Sum256([]byte(verifier))
	if !same(base64.RawURLEncoding.EncodeToString(sum[:]), c.PKCEChallenge) || redirect != c.RedirectURI {
		return OAuthTokens{}, errInvalidGrant
	}
	return s.issueApp(ctx, c, g)
}

// issueApp makes the app session and its device, and returns the tokens.
func (s *Service) issueApp(ctx context.Context, c store.OAuthCode, g grant) (OAuthTokens, error) {
	acc, err := s.store.Accounts().Get(ctx, c.AccountID)
	if err != nil || !acc.Active() {
		return OAuthTokens{}, errInvalidGrant
	}
	now := s.now()
	access, accessHash := secret.NewToken(secret.PrefixApp)
	refresh, refreshHash := secret.NewToken(secret.PrefixRefresh)
	ses := model.Session{AccountID: acc.ID, Kind: model.SessionApp, Label: c.ClientName, SignedInBy: g.signedInBy, SecondFactor: g.secondFactor,
		CreatedAt: now, LastSeenAt: now, AccessExpiresAt: now.Add(accessLife), MaxExpiresAt: g.max}
	dev := model.Device{AccountID: acc.ID, Name: c.ClientName, Platform: c.Platform, BoxKey: c.BoxKey, CreatedAt: now, LastSeenAt: now}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		var err error
		if ses.ExpiresAt, err = s.appEnd(ctx, tx, g.max, now); err != nil {
			return err
		}
		if !ses.ExpiresAt.After(now) {
			return errInvalidGrant
		}
		if err := tx.Sessions().Create(ctx, &ses, accessHash, refreshHash); err != nil {
			return err
		}
		dev.SessionID = ses.ID
		if err := tx.Devices().Create(ctx, &dev); err != nil {
			return err
		}
		return s.audit(ctx, tx, acc.ID, "device.added", "device", string(dev.ID), map[string]any{"device": dev.Name, "platform": dev.Platform})
	})
	if err != nil {
		return OAuthTokens{}, err
	}
	s.sessionStarted(ses)
	out := OAuthTokens{AccessToken: access, TokenType: "Bearer", ExpiresIn: int64(accessLife / time.Second), RefreshToken: refresh, DeviceID: dev.ID}
	if len(c.Handover) > 0 {
		out.Handover = base64.RawURLEncoding.EncodeToString(c.Handover)
	}
	return out, nil
}

// appEnd is when an app session ends: from a passkey sign-in each refresh
// moves it the passkey lifetime ahead; from a company sign-in it has the
// hard end of that sign-in (docs/AUTH.md section 11.3).
func (s *Service) appEnd(ctx context.Context, a store.Areas, max, now model.Time) (model.Time, error) {
	if !max.IsZero() {
		return max, nil
	}
	life, err := store.SettingIntOr(ctx, a, store.SettingPasskeySeconds, passkeySeconds)
	return now.Add(time.Duration(life) * time.Second), err
}

func (s *Service) refresh(ctx context.Context, token string) (OAuthTokens, error) {
	if secret.TokenPrefix(token) != secret.PrefixRefresh {
		return OAuthTokens{}, errInvalidGrant
	}
	var (
		out     OAuthTokens
		refused bool
		reused  *model.Session
	)
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		// The row is locked, so a second refresh with the same token
		// waits for the first and then finds its token is the previous one.
		ses, previous, err := tx.Sessions().ByRefreshHash(ctx, secret.HashToken(token))
		if errors.Is(err, model.ErrNotFound) {
			refused = true
			return nil
		}
		if err != nil {
			return err
		}
		now := s.now()
		if ses.Kind != model.SessionApp || !ses.RevokedAt.IsZero() || (!ses.ExpiresAt.IsZero() && !ses.ExpiresAt.After(now)) ||
			(!ses.MaxExpiresAt.IsZero() && !ses.MaxExpiresAt.After(now)) {
			refused = true
			return nil
		}
		if previous && now.Sub(ses.RotatedAt) > refreshTolerance {
			// A token that was already spent came back too late to be a
			// retry after a lost answer: somebody else holds it.
			refused, reused = true, &ses
			if err := tx.Sessions().Revoke(ctx, ses.ID, now); err != nil {
				return err
			}
			return s.audit(ctx, tx, ses.AccountID, "session.reuse_revoked", "session", string(ses.ID), map[string]any{"device": ses.Label})
		}
		acc, err := tx.Accounts().Get(ctx, ses.AccountID)
		if err != nil || !acc.Active() {
			refused = true
			return nil
		}
		access, accessHash := secret.NewToken(secret.PrefixApp)
		next, nextHash := secret.NewToken(secret.PrefixRefresh)
		if err := tx.Sessions().Rotate(ctx, ses.ID, accessHash, nextHash, now.Add(accessLife), now); err != nil {
			return err
		}
		end, err := s.appEnd(ctx, tx, ses.MaxExpiresAt, now)
		if err != nil {
			return err
		}
		if err := tx.Sessions().Touch(ctx, ses.ID, now, end); err != nil {
			return err
		}
		out = OAuthTokens{AccessToken: access, TokenType: "Bearer", ExpiresIn: int64(accessLife / time.Second), RefreshToken: next}
		if d, found, err := tx.Devices().BySession(ctx, ses.ID); err != nil {
			return err
		} else if found {
			out.DeviceID = d.ID
		}
		return nil
	})
	if err != nil {
		return OAuthTokens{}, err
	}
	if reused != nil {
		s.sessionsEnded(ctx, "a spent refresh token came back", store.SessionRef{ID: reused.ID, AccountID: reused.AccountID})
	}
	if refused {
		return OAuthTokens{}, errInvalidGrant
	}
	return out, nil
}

// RevokeToken is RFC 7009: either token of an app session ends it. The
// answer is always success, so nothing is returned.
func (s *Service) RevokeToken(ctx context.Context, token string) {
	var ses model.Session
	var err error
	switch secret.TokenPrefix(token) {
	case secret.PrefixApp:
		ses, err = s.store.Sessions().ByTokenHash(ctx, secret.HashToken(token))
	case secret.PrefixRefresh:
		ses, _, err = s.store.Sessions().ByRefreshHash(ctx, secret.HashToken(token))
	default:
		return
	}
	if err != nil || ses.Kind != model.SessionApp || !ses.RevokedAt.IsZero() {
		return
	}
	if err := s.store.Sessions().Revoke(ctx, ses.ID, s.now()); err != nil {
		s.log.Warn("an app session could not be revoked", "err", err)
		return
	}
	s.sessionsEnded(ctx, "signed out", store.SessionRef{ID: ses.ID, AccountID: ses.AccountID})
}

// Pairing is a pairing code made to be shown as a QR code.
type Pairing struct {
	Code      string     `json:"pairing_code"`
	QRText    string     `json:"qr_text"`
	ExpiresAt model.Time `json:"expires_at"`
}

// StartPairing makes a two-minute pairing code for a phone beside a browser
// that is already signed in. The account is held back until the person
// confirms the device by name.
func (s *Service) StartPairing(ctx context.Context, who Who) (Pairing, error) {
	if !who.Cookie {
		return Pairing{}, model.ErrUnauthenticated.With("This needs a browser session.")
	}
	if err := s.fresh(who); err != nil {
		return Pairing{}, err
	}
	code, hash := secret.NewLinkValue()
	out := Pairing{Code: code, ExpiresAt: s.now().Add(codeLife)}
	out.QRText = AppScheme + "://pair?server=" + url.QueryEscape(s.cfg.Origin) + "&code=" + code
	if err := s.store.Identities().CreateCode(ctx, hash, store.OAuthCode{Kind: store.CodePairing, ExpiresAt: out.ExpiresAt}); err != nil {
		return Pairing{}, err
	}
	s.pairings.put(codeKey(hash), who.Account.ID)
	return out, nil
}

// PairingState is what the page waiting on a pairing code sees.
type PairingState struct {
	State      string     `json:"state"` // waiting | claimed | confirmed
	DeviceName string     `json:"device_name,omitempty"`
	Platform   string     `json:"platform,omitempty"`
	ExpiresAt  model.Time `json:"expires_at"`
}

// pairing finds a live pairing code that this account started.
func (s *Service) pairing(ctx context.Context, who Who, code string) (store.OAuthCode, []byte, error) {
	if !who.Cookie {
		return store.OAuthCode{}, nil, model.ErrUnauthenticated.With("This needs a browser session.")
	}
	if err := complete(who); err != nil {
		return store.OAuthCode{}, nil, err
	}
	hash := secret.HashToken(code)
	c, err := s.store.Identities().GetCode(ctx, hash)
	if err != nil {
		return c, hash, err
	}
	starter, known := s.pairings.get(codeKey(hash))
	if c.Kind != store.CodePairing || !c.ExpiresAt.After(s.now()) || !c.UsedAt.IsZero() || !known || starter != who.Account.ID {
		return c, hash, model.ErrNotFound
	}
	return c, hash, nil
}

// GetPairing says whether a pairing code has been claimed, and by what.
func (s *Service) GetPairing(ctx context.Context, who Who, code string) (PairingState, error) {
	c, _, err := s.pairing(ctx, who, code)
	if err != nil {
		return PairingState{}, err
	}
	out := PairingState{State: "waiting", ExpiresAt: c.ExpiresAt}
	if c.ClientName != "" {
		out.State, out.DeviceName, out.Platform = "claimed", c.ClientName, c.Platform
	}
	if c.AccountID != "" {
		out.State = "confirmed"
	}
	return out, nil
}

// ConfirmPairing approves the device that claimed a pairing code, after the
// person has checked its name. This step is the defence against someone
// who photographed the screen, which is why it cannot be skipped.
func (s *Service) ConfirmPairing(ctx context.Context, who Who, code string) error {
	c, hash, err := s.pairing(ctx, who, code)
	if err != nil {
		return err
	}
	if c.ClientName == "" {
		return model.ErrConflict.With("No device has scanned this code yet.")
	}
	if c.AccountID != "" {
		return model.ErrConflict.With("This device has already been confirmed.")
	}
	ok, err := s.store.Identities().ConfirmPairing(ctx, hash, who.Account.ID, s.now())
	if err != nil {
		return err
	}
	if !ok {
		return model.ErrConflict.With("This code can no longer be confirmed.")
	}
	// The new session takes the confirming session's hard end, so a
	// device paired from a company session ends when that session would.
	s.grants.put(codeKey(hash), grant{signedInBy: "pairing", secondFactor: who.Session.SecondFactor, max: who.Session.MaxExpiresAt})
	return nil
}

func (s *Service) exchangePairing(ctx context.Context, form url.Values) (OAuthTokens, error) {
	code := form.Get("pairing_code")
	if code == "" {
		return OAuthTokens{}, oauthErr("invalid_request", "pairing_code is needed.")
	}
	platform := form.Get("platform")
	if platform != "" && !appPlatforms[platform] {
		return OAuthTokens{}, oauthErr("invalid_request", "platform is android, ios, macos, windows or linux.")
	}
	key, ok := boxKey(form.Get("box_key"))
	if !ok {
		return OAuthTokens{}, oauthErr("invalid_request", "box_key is 32 bytes in base64url.")
	}
	hash := secret.HashToken(code)
	if ok, retry := s.limitPairPoll.Allow(codeKey(hash)); !ok {
		return OAuthTokens{}, &OAuthError{Status: 400, Code: "slow_down", Description: "Ask again every two seconds.", RetryAfter: int(retry/time.Second) + 1}
	}
	now := s.now()
	c, err := s.store.Identities().GetCode(ctx, hash)
	if errors.Is(err, model.ErrNotFound) {
		return OAuthTokens{}, errInvalidGrant
	}
	if err != nil {
		return OAuthTokens{}, err
	}
	if c.Kind != store.CodePairing || !c.UsedAt.IsZero() || !c.ExpiresAt.After(now) {
		return OAuthTokens{}, errInvalidGrant
	}
	if c.ClientName == "" {
		// The first request records the device's name, which is what
		// the person is asked to confirm.
		if _, err := s.store.Identities().ClaimPairing(ctx, hash, deviceName(form.Get("device_name")), platform, key, now); err != nil {
			return OAuthTokens{}, err
		}
		return OAuthTokens{}, errPending
	}
	if c.AccountID == "" {
		return OAuthTokens{}, errPending
	}
	c, used, err := s.store.Identities().UseCode(ctx, hash, now)
	if err != nil {
		return OAuthTokens{}, err
	}
	g, granted := s.grants.take(codeKey(hash))
	if !used || !granted {
		return OAuthTokens{}, errInvalidGrant
	}
	return s.issueApp(ctx, c, g)
}
