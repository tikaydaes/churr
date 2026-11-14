package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
)

// oidcConfig is the `config` of an OIDC provider (docs/AUTH.md section 7.2).
type oidcConfig struct {
	Issuer   string   `json:"issuer"`
	ClientID string   `json:"client_id"`
	Scopes   []string `json:"scopes,omitempty"`
	// The claims a new account's username and display name come from.
	UsernameClaim string `json:"username_claim,omitempty"`
	NameClaim     string `json:"name_claim,omitempty"`
	// ACRValues are the `acr` values that count as multi-factor.
	ACRValues []string `json:"acr_values,omitempty"`
}

// The signature algorithms an ID token may use. `none` and the HMAC
// algorithms are refused by not being here.
var oidcAlgs = []string{"RS256", "PS256", "ES256", "EdDSA"}

// oidcRefresh is how often the discovery document is fetched again.
const oidcRefresh = 24 * time.Hour

// oidcEntry is a provider's discovery document and key set, kept between
// sign-ins.
type oidcEntry struct {
	key      string
	provider *oidc.Provider
	algs     []string
	jwks     string
	at       time.Time
}

// strictJSON decodes one JSON object and refuses fields it does not know.
func strictJSON(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func httpsURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

// oidcConfigure checks a config an admin entered, fetches the provider's
// discovery document to prove it works, and returns the config to store.
func (s *Service) oidcConfigure(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c oidcConfig
	if err := strictJSON(raw, &c); err != nil {
		return nil, model.Invalid("config", "An OIDC config has issuer, client_id, and optionally scopes, username_claim, name_claim and acr_values.")
	}
	c.Issuer, c.ClientID = strings.TrimSpace(c.Issuer), strings.TrimSpace(c.ClientID)
	if !httpsURL(c.Issuer) {
		return nil, model.Invalid("config", "issuer must be an https address.")
	}
	if c.ClientID == "" {
		return nil, model.Invalid("config", "client_id is needed.")
	}
	for _, sc := range c.Scopes {
		if sc == "" || strings.ContainsAny(sc, " \t\r\n") {
			return nil, model.Invalid("config", "A scope is one word.")
		}
	}
	if _, err := s.oidcDiscover(ctx, c); err != nil {
		return nil, model.ErrUnavailable.With("The provider's discovery document could not be used: " + err.Error())
	}
	return json.Marshal(c)
}

// oidcDiscover fetches and checks the discovery document. The issuer in it
// must equal the configured one exactly (the library checks that), and
// every endpoint must be HTTPS.
func (s *Service) oidcDiscover(ctx context.Context, c oidcConfig) (*oidcEntry, error) {
	ctx, cancel := context.WithTimeout(oidc.ClientContext(ctx, s.httpc), 15*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, c.Issuer)
	if err != nil {
		return nil, err
	}
	var doc struct {
		JWKS string   `json:"jwks_uri"`
		Algs []string `json:"id_token_signing_alg_values_supported"`
	}
	if err := provider.Claims(&doc); err != nil {
		return nil, err
	}
	ep := provider.Endpoint()
	for name, addr := range map[string]string{"authorization endpoint": ep.AuthURL, "token endpoint": ep.TokenURL, "key set address": doc.JWKS} {
		if !httpsURL(addr) {
			return nil, fmt.Errorf("the %s is not an https address", name)
		}
	}
	var algs []string
	for _, a := range oidcAlgs {
		if contains(doc.Algs, a) {
			algs = append(algs, a)
		}
	}
	if len(algs) == 0 {
		return nil, errors.New("the provider signs ID tokens with none of RS256, PS256, ES256 or EdDSA")
	}
	return &oidcEntry{key: c.Issuer + "\x00" + c.ClientID, provider: provider, algs: algs, jwks: doc.JWKS, at: time.Now()}, nil
}

// oidcProvider returns the cached discovery document of a provider,
// fetching it again every 24 hours.
func (s *Service) oidcProvider(ctx context.Context, p model.SsoProvider) (*oidcEntry, oidcConfig, error) {
	var c oidcConfig
	if err := json.Unmarshal(p.Config, &c); err != nil {
		return nil, c, err
	}
	key := c.Issuer + "\x00" + c.ClientID
	s.oidcMu.Lock()
	e := s.oidcCache[p.ID]
	s.oidcMu.Unlock()
	if e != nil && e.key == key && time.Since(e.at) < oidcRefresh {
		return e, c, nil
	}
	fresh, err := s.oidcDiscover(ctx, c)
	if err != nil {
		if e != nil && e.key == key {
			// A provider that is briefly unreachable does not stop
			// sign-in while the last document is at hand.
			s.log.Warn("the discovery document could not be fetched again", "provider", p.Name, "err", err)
			return e, c, nil
		}
		return nil, c, err
	}
	s.oidcMu.Lock()
	s.oidcCache[p.ID] = fresh
	s.oidcMu.Unlock()
	return fresh, c, nil
}

func (s *Service) oidcForget(id string) {
	s.oidcMu.Lock()
	delete(s.oidcCache, id)
	s.oidcMu.Unlock()
}

func (s *Service) oauthConfig(p model.SsoProvider, e *oidcEntry, c oidcConfig, scopes []string) (*oauth2.Config, error) {
	sec, err := s.providerSecret(p)
	if err != nil {
		return nil, err
	}
	return &oauth2.Config{ClientID: c.ClientID, ClientSecret: string(sec), Endpoint: e.provider.Endpoint(),
		RedirectURL: s.callbackURL(p), Scopes: scopes}, nil
}

// oidcStart builds the address of the provider's authorization endpoint for
// a new waiting request: state, nonce and a PKCE verifier, each 256 random
// bits.
func (s *Service) oidcStart(ctx context.Context, p model.SsoProvider, state string, w *waiting, pol policy) (string, error) {
	e, c, err := s.oidcProvider(ctx, p)
	if err != nil {
		return "", err
	}
	w.nonce, _ = secret.NewLinkValue()
	w.verifier, _ = secret.NewLinkValue()
	scopes := append([]string{oidc.ScopeOpenID, "profile"}, c.Scopes...)
	// The email scope is asked for only while imported accounts wait to
	// be claimed.
	if has, err := s.store.Identities().HasImportClaims(ctx); err != nil {
		return "", err
	} else if has && !contains(scopes, "email") {
		scopes = append(scopes, "email")
	}
	conf, err := s.oauthConfig(p, e, c, scopes)
	if err != nil {
		return "", err
	}
	opts := []oauth2.AuthCodeOption{oidc.Nonce(w.nonce), oauth2.S256ChallengeOption(w.verifier)}
	if pol.SecondRequired && len(c.ACRValues) > 0 {
		// Only a request: the answer is always checked.
		opts = append(opts, oauth2.SetAuthURLParam("acr_values", strings.Join(c.ACRValues, " ")))
	}
	return conf.AuthCodeURL(state, opts...), nil
}

// oidcTest fetches the discovery document and the key set afresh.
func (s *Service) oidcTest(ctx context.Context, p model.SsoProvider) error {
	var c oidcConfig
	if err := json.Unmarshal(p.Config, &c); err != nil {
		return errors.New("the stored config cannot be read")
	}
	e, err := s.oidcDiscover(ctx, c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := newGet(ctx, e.jwks)
	if err != nil {
		return err
	}
	resp, err := s.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("the key set could not be fetched: %w", err)
	}
	defer resp.Body.Close()
	var set struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&set) != nil || len(set.Keys) == 0 {
		return errors.New("the key set address did not return any keys")
	}
	return nil
}

// OidcCallback handles the browser's return from an OIDC provider: it takes
// the waiting request, exchanges the code, validates the ID token, and then
// finds, claims, creates or links the account.
func (s *Service) OidcCallback(ctx context.Context, addr, providerID string, q url.Values, cookie, label string) (SsoResult, error) {
	if err := limited(s.limitSsoCallback, addr); err != nil {
		return SsoResult{}, err
	}
	p, err := s.store.Identities().GetProvider(ctx, providerID)
	if err != nil {
		return SsoResult{}, err
	}
	res := SsoResult{ProviderName: p.Name}
	if p.Kind != model.ProviderOIDC || !p.Enabled {
		return res, model.ErrNotFound
	}
	// 1. The waiting request, by state, and the browser it was made in.
	w, err := s.takeWaiting(q.Get("state"), p.ID, cookie)
	if err != nil {
		return res, s.ssoFailed(p, addr, "state", err)
	}
	e, c, err := s.oidcProvider(ctx, p)
	if err != nil {
		return res, s.ssoFailed(p, addr, "discovery", err)
	}
	// 2. An error from the provider, or an answer from another issuer.
	if q.Get("error") != "" {
		return res, s.ssoFailed(p, addr, "the provider answered with an error", errors.New(q.Get("error")))
	}
	if iss := q.Get("iss"); iss != "" && iss != c.Issuer {
		return res, s.ssoFailed(p, addr, "iss parameter", errors.New("the answer names another issuer"))
	}
	if q.Get("code") == "" {
		return res, s.ssoFailed(p, addr, "code", errors.New("the answer carries no code"))
	}
	// 3. The code is exchanged with the verifier and the client secret.
	conf, err := s.oauthConfig(p, e, c, nil)
	if err != nil {
		return res, err
	}
	xctx, cancel := context.WithTimeout(oidc.ClientContext(ctx, s.httpc), 15*time.Second)
	defer cancel()
	tok, err := conf.Exchange(xctx, q.Get("code"), oauth2.VerifierOption(w.verifier))
	if err != nil {
		return res, s.ssoFailed(p, addr, "code exchange", err)
	}
	raw, _ := tok.Extra("id_token").(string)
	// Access and refresh tokens from the provider are dropped here:
	// Churr holds no standing access to it.
	tok = nil
	who, err := s.oidcValidate(xctx, p, e, c, raw, w.nonce)
	if err != nil {
		return res, s.ssoFailed(p, addr, "ID token", err)
	}
	return s.finishSso(ctx, addr, p, w, who, label)
}

// oidcValidate checks an ID token and reads the claims from it. Every check
// of docs/AUTH.md section 7.2 step 4 must pass.
func (s *Service) oidcValidate(ctx context.Context, p model.SsoProvider, e *oidcEntry, c oidcConfig, raw, nonce string) (asserted, error) {
	var who asserted
	if raw == "" {
		return who, errors.New("the token answer carries no ID token")
	}
	// The algorithm: one of ours, and one the provider says it uses.
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return who, errors.New("the ID token is not a signed JWT")
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return who, errors.New("the ID token header cannot be read")
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if json.Unmarshal(hb, &header) != nil || !contains(e.algs, header.Alg) {
		return who, fmt.Errorf("the ID token is signed with %q, which is not accepted", header.Alg)
	}
	// The library verifies the signature under the provider's key named
	// by kid, the issuer exactly, and that aud contains the client id.
	// Expiry is checked below with the allowance the document gives.
	verifier := e.provider.Verifier(&oidc.Config{ClientID: c.ClientID, SupportedSigningAlgs: e.algs, SkipExpiryCheck: true})
	tok, err := verifier.Verify(ctx, raw)
	if err != nil {
		return who, err
	}
	var claims map[string]any
	if err := tok.Claims(&claims); err != nil {
		return who, err
	}
	if tok.Issuer != c.Issuer {
		return who, errors.New("iss is not the configured issuer")
	}
	if !contains(tok.Audience, c.ClientID) {
		return who, errors.New("aud does not name this client")
	}
	if len(tok.Audience) > 1 {
		if azp, _ := claims["azp"].(string); azp != c.ClientID {
			return who, errors.New("the token names several audiences and azp is not this client")
		}
	}
	now := time.Now()
	if _, has := claims["exp"]; !has || !tok.Expiry.After(now.Add(-clockAllowance)) {
		return who, errors.New("the ID token has expired")
	}
	if tok.IssuedAt.After(now.Add(clockAllowance)) {
		return who, errors.New("the ID token was issued in the future")
	}
	if tok.Nonce == "" || !same(tok.Nonce, nonce) {
		return who, errors.New("nonce does not match the request")
	}
	if tok.Subject == "" || len(tok.Subject) > 255 {
		return who, errors.New("sub is missing or too long")
	}
	// Claims come from the ID token only; UserInfo is never called.
	str := func(name, def string) string {
		if name == "" {
			name = def
		}
		v, _ := claims[name].(string)
		return v
	}
	who = asserted{by: "oidc", subject: tok.Subject, username: str(c.UsernameClaim, "preferred_username"), name: str(c.NameClaim, "name")}
	if verified, _ := claims["email_verified"].(bool); verified {
		who.email, _ = claims["email"].(string)
	}
	groupClaim := p.GroupClaim
	if groupClaim == "" {
		groupClaim = "groups"
	}
	who.groups, who.hasGroups = stringList(claims[groupClaim])
	// The provider's own statement about multi-factor: amr contains mfa,
	// or acr is one of the configured values.
	if amr, ok := stringList(claims["amr"]); ok && contains(amr, "mfa") {
		who.mfa = true
	}
	if acr, _ := claims["acr"].(string); acr != "" && contains(c.ACRValues, acr) {
		who.mfa = true
	}
	return who, nil
}

// stringList reads a claim as a list of strings; a single string is a list
// of one. ok is false when the claim is absent or something else.
func stringList(v any) (list []string, ok bool) {
	switch t := v.(type) {
	case string:
		return []string{t}, true
	case []any:
		list = []string{}
		for _, x := range t {
			if str, isStr := x.(string); isStr {
				list = append(list, str)
			}
		}
		return list, true
	}
	return nil, false
}
