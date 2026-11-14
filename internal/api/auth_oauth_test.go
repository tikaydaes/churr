package api_test

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// App sign-in: OAuth 2.0 authorization code with PKCE, refresh tokens with
// rotation, revocation, the metadata document and QR pairing (docs/AUTH.md
// section 11), settings, and server integrations.

const appRedirect = "http://127.0.0.1:53999/callback"

// pkce returns a verifier and its S256 challenge.
func pkce(verifier string) (string, string) {
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func authorizeQuery(challenge string, change func(q url.Values)) url.Values {
	q := url.Values{"response_type": {"code"}, "client_id": {"churr-app"}, "redirect_uri": {appRedirect}, "code_challenge": {challenge},
		"code_challenge_method": {"S256"}, "state": {"st-1"}, "device_name": {"Ann's Pixel"}, "platform": {"android"}}
	if change != nil {
		change(q)
	}
	return q
}

// approve presses Approve on the authorization page and returns the code.
func approve(t *testing.T, c *testkit.Client, q url.Values) string {
	t.Helper()
	form := url.Values{"decision": {"approve"}}
	for k, v := range q {
		form[k] = v
	}
	p := c.Fetch("POST", "/oauth/authorize", form)
	if p.Status != 302 {
		t.Fatalf("approve = %d %s", p.Status, p.Body)
	}
	u, err := url.Parse(p.Location())
	if err != nil || !strings.HasPrefix(p.Location(), appRedirect+"?") || u.Query().Get("state") != q.Get("state") || u.Query().Get("code") == "" {
		t.Fatalf("redirect: %s", p.Location())
	}
	return u.Query().Get("code")
}

func token(c *testkit.Client, form url.Values) testkit.Page {
	c.NoOrigin = true
	defer func() { c.NoOrigin = false }()
	return c.Fetch("POST", "/oauth/token", form)
}

func TestOAuthAuthorizationCode(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	app := ts.Browser() // the app's own HTTP client: no cookies of the browser
	verifier, challenge := pkce("a-verifier-of-at-least-forty-three-characters-long")

	// The metadata of RFC 8414.
	md := app.Must(200, "GET", "/.well-known/oauth-authorization-server", nil)
	if md["issuer"] != ts.Origin || md["token_endpoint"] != ts.Origin+"/oauth/token" || md["authorization_endpoint"] != ts.Origin+"/oauth/authorize" ||
		md["revocation_endpoint"] != ts.Origin+"/oauth/revoke" || md["authorization_response_iss_parameter_supported"] != true ||
		len(md["grant_types_supported"].([]any)) != 3 || md["code_challenge_methods_supported"].([]any)[0] != "S256" ||
		md["token_endpoint_auth_methods_supported"].([]any)[0] != "none" || md["response_types_supported"].([]any)[0] != "code" {
		t.Fatalf("metadata: %v", md)
	}

	// The redirect address and PKCE values are checked before anything
	// else, and the page shows the error itself: it never redirects to
	// an address it has not accepted.
	for name, change := range map[string]func(q url.Values){
		"an https redirect":   func(q url.Values) { q.Set("redirect_uri", "https://evil.example/cb") },
		"localhost by name":   func(q url.Values) { q.Set("redirect_uri", "http://localhost:8080/cb") },
		"a fragment":          func(q url.Values) { q.Set("redirect_uri", appRedirect+"#x") },
		"no redirect":         func(q url.Values) { q.Del("redirect_uri") },
		"no challenge":        func(q url.Values) { q.Del("code_challenge") },
		"a short challenge":   func(q url.Values) { q.Set("code_challenge", "short") },
		"the plain method":    func(q url.Values) { q.Set("code_challenge_method", "plain") },
		"no method":           func(q url.Values) { q.Del("code_challenge_method") },
		"another response":    func(q url.Values) { q.Set("response_type", "token") },
		"no client":           func(q url.Values) { q.Del("client_id") },
		"an unknown platform": func(q url.Values) { q.Set("platform", "amiga") },
		"a bad box key":       func(q url.Values) { q.Set("box_key", "abc") },
	} {
		p := ann.Fetch("GET", "/oauth/authorize?"+authorizeQuery(challenge, change).Encode(), nil)
		if p.Status != 400 || p.Location() != "" || strings.Contains(p.Body, "Approve") {
			t.Fatalf("%s: %d %s", name, p.Status, p.Body)
		}
		if name != "an https redirect" && name != "no challenge" {
			continue // the page is limited per address
		}
		form := authorizeQuery(challenge, change)
		form.Set("decision", "approve")
		if p := ann.Fetch("POST", "/oauth/authorize", form); p.Status != 400 || p.Location() != "" {
			t.Fatalf("%s, approved: %d %s", name, p.Status, p.Location())
		}
	}
	// The apps' own scheme and the IPv6 loopback are accepted.
	for _, ok := range []string{"churr://oauth/callback", "http://[::1]:4000/cb"} {
		if p := ann.Fetch("GET", "/oauth/authorize?"+authorizeQuery(challenge, func(q url.Values) { q.Set("redirect_uri", ok) }).Encode(), nil); p.Status != 200 {
			t.Fatalf("%s: %d", ok, p.Status)
		}
	}

	// Signed in: the page asks "Sign in <device> as <username>?".
	q := authorizeQuery(challenge, nil)
	page := ann.Fetch("GET", "/oauth/authorize?"+q.Encode(), nil)
	if page.Status != 200 || !strings.Contains(page.Body, "Ann&#39;s Pixel") || !strings.Contains(page.Body, "<strong>ann</strong>") ||
		!strings.Contains(page.Body, "android") || page.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("approval page: %d %s", page.Status, page.Body)
	}
	// The approval is a same-origin POST with the session cookie.
	ann.NoOrigin = true
	form := authorizeQuery(challenge, nil)
	form.Set("decision", "approve")
	if p := ann.Fetch("POST", "/oauth/authorize", form); p.Status != 403 {
		t.Fatalf("an approval from another origin: %d", p.Status)
	}
	ann.NoOrigin = false
	form.Set("decision", "deny")
	if p := ann.Fetch("POST", "/oauth/authorize", form); p.Status != 302 || !strings.Contains(p.Location(), "error=access_denied") || strings.Contains(p.Location(), "code=") {
		t.Fatalf("cancel: %d %s", p.Status, p.Location())
	}
	if p := ts.Browser().Fetch("POST", "/oauth/authorize", form); p.Status != 401 {
		t.Fatalf("approving signed out: %d", p.Status)
	}
	p := ann.Fetch("POST", "/oauth/authorize", url.Values{"decision": {"approve"}, "response_type": {"code"}, "client_id": {"churr-app"},
		"redirect_uri": {appRedirect}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {"st-1"}, "device_name": {"Ann's Pixel"}, "platform": {"android"}})
	back, _ := url.Parse(p.Location())
	if p.Status != 302 || back.Query().Get("iss") != ts.Origin || back.Query().Get("state") != "st-1" {
		t.Fatalf("approved: %d %s", p.Status, p.Location())
	}
	code := back.Query().Get("code")
	// A session that was already there stays.
	ann.Must(200, "GET", "/api/v1/me", nil)

	// The token endpoint speaks OAuth's error shape.
	wantErr := func(name string, p testkit.Page, status int, code string) {
		t.Helper()
		if p.Status != status || p.JSON()["error"] != code {
			t.Fatalf("%s: %d %s, want %d %s", name, p.Status, p.Body, status, code)
		}
	}
	wantErr("no grant type", token(app, url.Values{}), 400, "invalid_request")
	wantErr("unknown grant type", token(app, url.Values{"grant_type": {"password"}}), 400, "unsupported_grant_type")
	wantErr("missing fields", token(app, url.Values{"grant_type": {"authorization_code"}, "code": {code}}), 400, "invalid_request")
	wantErr("unknown code", token(app, url.Values{"grant_type": {"authorization_code"}, "code": {"made-up"}, "code_verifier": {verifier}, "redirect_uri": {appRedirect}}), 400, "invalid_grant")

	// A wrong verifier fails, and the code is spent even so.
	wantErr("wrong verifier", token(app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {"another-verifier-of-at-least-forty-three-chars"}, "redirect_uri": {appRedirect}}), 400, "invalid_grant")
	wantErr("the spent code", token(app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {appRedirect}}), 400, "invalid_grant")
	code = approve(t, ann, q)
	wantErr("another redirect", token(app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {"http://127.0.0.1:1/callback"}}), 400, "invalid_grant")

	code = approve(t, ann, q)
	got := token(app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {appRedirect}})
	tok := got.JSON()
	if got.Status != 200 || tok["token_type"] != "Bearer" || tok["expires_in"] != float64(3600) || !strings.HasPrefix(tok["access_token"].(string), "churr_app_") ||
		!strings.HasPrefix(tok["refresh_token"].(string), "churr_ref_") || tok["device_id"] == nil || got.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("tokens: %d %s", got.Status, got.Body)
	}
	wantErr("a code used twice", token(app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {appRedirect}}), 400, "invalid_grant")
	if e := wantAudit(t, ts, "device.added"); e.Detail["device"] != "Ann's Pixel" || e.Detail["platform"] != "android" {
		t.Fatalf("audit: %+v", e)
	}

	// The access token acts as the person; the refresh token does not,
	// and neither works as a cookie.
	phone := ts.App(tok["access_token"].(string))
	signedInAs(t, phone, "ann")
	ts.App(tok["refresh_token"].(string)).Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	asCookie := ts.Browser()
	asCookie.SetCookie("churr_session", tok["access_token"].(string))
	asCookie.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	var appSession map[string]any
	for _, s := range ann.Must(200, "GET", "/api/v1/me/sessions", nil)["sessions"].([]any) {
		if m := s.(map[string]any); m["kind"] == "app" {
			appSession = m
		}
	}
	if appSession == nil || appSession["label"] != "Ann's Pixel" || appSession["signed_in_by"] != "passkey" {
		t.Fatalf("the app's session: %v", appSession)
	}
	// An app session passes a fresh check like a browser's (it has none
	// to start with).
	phone.Refused(403, model.CodeFreshCheckRequired, "POST", "/api/v1/me/tokens", map[string]any{"label": "x"})

	// Rotation: a refresh token is used once and returns a new pair.
	refresh := func(tok string) testkit.Page {
		return token(app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok}})
	}
	first := tok["refresh_token"].(string)
	r1 := refresh(first)
	t1 := r1.JSON()
	if r1.Status != 200 || t1["refresh_token"] == first || t1["access_token"] == tok["access_token"] || t1["device_id"] != tok["device_id"] {
		t.Fatalf("refresh: %d %s", r1.Status, r1.Body)
	}
	phone.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil) // the old access token is replaced
	signedInAs(t, ts.App(t1["access_token"].(string)), "ann")
	// The token just replaced, again within 60 seconds: a retry after a
	// lost answer. It rotates again.
	r2 := refresh(first)
	t2 := r2.JSON()
	if r2.Status != 200 || t2["refresh_token"] == t1["refresh_token"] {
		t.Fatalf("a retry within the tolerance: %d %s", r2.Status, r2.Body)
	}
	wantErr("an access token as a refresh token", refresh(t2["access_token"].(string)), 400, "invalid_grant")
	wantErr("an unknown refresh token", refresh("churr_ref_made-up"), 400, "invalid_grant")
	signedInAs(t, ts.App(t2["access_token"].(string)), "ann")
	// Later than that, a spent token coming back means somebody else
	// holds it: the session is revoked.
	r3 := refresh(t2["refresh_token"].(string))
	t3 := r3.JSON()
	ts.Exec(`UPDATE sessions SET rotated_at = ? WHERE id = ?`, int64(model.Now().Add(-2*time.Minute)), appSession["id"].(string))
	wantErr("a spent token after the tolerance", refresh(t2["refresh_token"].(string)), 400, "invalid_grant")
	wantErr("the session is revoked", refresh(t3["refresh_token"].(string)), 400, "invalid_grant")
	ts.App(t3["access_token"].(string)).Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	if e := wantAudit(t, ts, "session.reuse_revoked"); e.Detail["device"] != "Ann's Pixel" {
		t.Fatalf("audit: %+v", e)
	}

	// Revocation (RFC 7009): either token ends the session; the answer
	// is always success.
	code = approve(t, ann, q)
	tok = token(app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {appRedirect}}).JSON()
	revoke := func(v string) int {
		app.NoOrigin = true
		defer func() { app.NoOrigin = false }()
		return app.Fetch("POST", "/oauth/revoke", url.Values{"token": {v}}).Status
	}
	if revoke("nonsense") != 200 || revoke(tok["refresh_token"].(string)) != 200 || revoke(tok["refresh_token"].(string)) != 200 {
		t.Fatal("revocation always answers success")
	}
	ts.App(tok["access_token"].(string)).Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	wantErr("refresh after revocation", refresh(tok["refresh_token"].(string)), 400, "invalid_grant")
}

// A browser session made only to approve an app is revoked once the code is
// issued: signing an app in does not leave a browser signed in behind it.
func TestOAuthSignInOnThePage(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	verifier, challenge := pkce("b-verifier-of-at-least-forty-three-characters-long")
	q := authorizeQuery(challenge, nil)

	sys := ts.Browser() // the system browser the app opens: not signed in
	sys.CopyPasskey(ann)
	page := sys.Fetch("GET", "/oauth/authorize?"+q.Encode(), nil)
	if page.Status != 200 || !strings.Contains(page.Body, "Sign in with a passkey") || strings.Contains(page.Body, `value="approve"`) {
		t.Fatalf("sign-in page: %d %s", page.Status, page.Body)
	}
	if status, _ := sys.Login(); status != 200 {
		t.Fatal("signing in on the page")
	}
	page = sys.Fetch("GET", "/oauth/authorize?"+q.Encode(), nil)
	if !strings.Contains(page.Body, `value="approve"`) {
		t.Fatalf("approval page: %s", page.Body)
	}
	code := approve(t, sys, q)
	sys.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	// The person's own browser, signed in before, is untouched.
	ann.Must(200, "GET", "/api/v1/me", nil)
	app := ts.Browser()
	got := token(app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {appRedirect}})
	if got.Status != 200 {
		t.Fatalf("tokens: %d %s", got.Status, got.Body)
	}
	signedInAs(t, ts.App(got.JSON()["access_token"].(string)), "ann")

	// With company sign-in offered, the page links to each provider and
	// comes back to itself; a pending session finishes its second step
	// there, and the app's session has the company session's hard end.
	idp, pid := oidcSetup(t, ts, nil)
	user := testkit.IdPUser{Subject: "sub-bea", Username: "bea"}
	first := ts.Browser()
	oidcSignIn(t, ts, first, idp, pid, "", user)
	secret := first.Must(201, "POST", "/api/v1/me/totp", nil)["secret"].(string)
	first.Must(200, "POST", "/api/v1/me/totp/confirm", map[string]any{"code": testkit.TOTPCode(secret, -1)})
	ts.Owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"auth.second_factor": "required", "auth.second_factor_accepted": []string{"totp"}})

	sys = ts.Browser()
	page = sys.Fetch("GET", "/oauth/authorize?"+q.Encode(), nil)
	want := "/api/v1/sso/" + pid + "/start?return_to="
	if !strings.Contains(page.Body, want) || !strings.Contains(page.Body, "Sign in through Acme ID") {
		t.Fatalf("the page offers the provider: %s", page.Body)
	}
	back := oidcSignIn(t, ts, sys, idp, pid, "?return_to="+url.QueryEscape("/oauth/authorize?"+q.Encode()), user)
	if !strings.Contains(back.Body, "url=/oauth/authorize?") {
		t.Fatalf("back to the page: %s", back.Body)
	}
	page = sys.Fetch("GET", "/oauth/authorize?"+q.Encode(), nil)
	if !strings.Contains(page.Body, "One more step") || strings.Contains(page.Body, `value="approve"`) {
		t.Fatalf("a pending session sees the second step, not the approval: %s", page.Body)
	}
	// A pending session cannot approve.
	form := authorizeQuery(challenge, nil)
	form.Set("decision", "approve")
	if p := sys.Fetch("POST", "/oauth/authorize", form); p.Status != 401 || p.Location() != "" {
		t.Fatalf("approving while pending: %d", p.Status)
	}
	sys.Must(200, "POST", "/api/v1/checks/totp", map[string]any{"code": testkit.TOTPCode(secret, 0)})
	code = approve(t, sys, q)
	sys.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	tok := token(app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {appRedirect}}).JSON()
	phone := ts.App(tok["access_token"].(string))
	signedInAs(t, phone, "bea")
	for _, s := range phone.Must(200, "GET", "/api/v1/me/sessions", nil)["sessions"].([]any) {
		m := s.(map[string]any)
		if m["current"] != true {
			continue
		}
		exp, _ := time.Parse(time.RFC3339Nano, m["expires_at"].(string))
		if m["signed_in_by"] != "oidc" || m["second_factor"] != "totp" || time.Until(exp) > 24*time.Hour {
			t.Fatalf("the app session from a company sign-in: %v", m)
		}
		// When the hard end is reached the refresh is refused.
		ts.Exec(`UPDATE sessions SET max_expires_at = ?, expires_at = ? WHERE id = ?`, int64(model.Now().Add(-time.Minute)), int64(model.Now().Add(-time.Minute)), m["id"].(string))
	}
	if p := token(app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok["refresh_token"].(string)}}); p.Status != 400 || p.JSON()["error"] != "invalid_grant" {
		t.Fatalf("refresh past the hard end: %d %s", p.Status, p.Body)
	}
}

func TestPairing(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	phone := ts.Browser()
	pair := func(form url.Values) testkit.Page {
		form.Set("grant_type", "urn:churr:params:oauth:grant-type:pairing-code")
		return token(phone, form)
	}
	wantErr := func(name string, p testkit.Page, code string) {
		t.Helper()
		if p.Status != 400 || p.JSON()["error"] != code {
			t.Fatalf("%s: %d %s, want %s", name, p.Status, p.Body, code)
		}
	}

	// Made from a signed-in web page, with a fresh check; never by a token.
	ann.Token().Refused(401, model.CodeUnauthenticated, "POST", "/oauth/pair", nil)
	ts.Browser().Refused(401, model.CodeUnauthenticated, "POST", "/oauth/pair", nil)
	ann.Stale()
	ann.Refused(403, model.CodeFreshCheckRequired, "POST", "/oauth/pair", nil)
	ann.Check()
	made := ann.Must(201, "POST", "/oauth/pair", nil)
	code := made["pairing_code"].(string)
	if qr := made["qr_text"].(string); !strings.HasPrefix(qr, "churr://pair?server=") || !strings.HasSuffix(qr, "&code="+code) {
		t.Fatalf("qr_text: %s", qr)
	}
	exp, _ := time.Parse(time.RFC3339Nano, made["expires_at"].(string))
	if d := time.Until(exp); d > 2*time.Minute || d < time.Minute {
		t.Fatalf("a pairing code lasts two minutes, got %v", d)
	}
	path := "/oauth/pair/" + code
	if st := ann.Must(200, "GET", path, nil); st["state"] != "waiting" || st["device_name"] != nil {
		t.Fatalf("waiting: %v", st)
	}
	// Only the person who made it holds it.
	bob.Refused(404, model.CodeNotFound, "GET", path, nil)
	bob.Refused(404, model.CodeNotFound, "POST", path+"/confirm", nil)
	ann.Refused(404, model.CodeNotFound, "GET", "/oauth/pair/made-up", nil)
	// Nothing to confirm before a device has scanned it.
	ann.Refused(409, model.CodeConflict, "POST", path+"/confirm", nil)

	// The app posts the code with its name, and waits.
	wantErr("no code", pair(url.Values{}), "invalid_request")
	wantErr("an unknown code", pair(url.Values{"pairing_code": {"made-up"}}), "invalid_grant")
	wantErr("the first request", pair(url.Values{"pairing_code": {code}, "device_name": {"Ann's iPhone"}, "platform": {"ios"}}), "authorization_pending")
	if st := ann.Must(200, "GET", path, nil); st["state"] != "claimed" || st["device_name"] != "Ann's iPhone" || st["platform"] != "ios" {
		t.Fatalf("claimed: %v", st)
	}
	// Someone who photographed the screen and posts second is not the
	// device the person is asked to confirm.
	time.Sleep(2100 * time.Millisecond)
	wantErr("a second device", pair(url.Values{"pairing_code": {code}, "device_name": {"Thief"}, "platform": {"android"}}), "authorization_pending")
	if st := ann.Must(200, "GET", path, nil); st["device_name"] != "Ann's iPhone" {
		t.Fatalf("the first device's name is what is confirmed: %v", st)
	}
	// Asking faster than every two seconds is told to slow down.
	pair(url.Values{"pairing_code": {code}})
	if p := pair(url.Values{"pairing_code": {code}}); p.JSON()["error"] != "slow_down" {
		t.Fatalf("polling too fast: %d %s", p.Status, p.Body)
	}

	// The confirmation cannot be skipped; after it the app gets tokens.
	ann.NoOrigin = true
	ann.Refused(403, model.CodeForbidden, "POST", path+"/confirm", nil)
	ann.NoOrigin = false
	ann.Must(204, "POST", path+"/confirm", nil)
	ann.Refused(409, model.CodeConflict, "POST", path+"/confirm", nil)
	if st := ann.Must(200, "GET", path, nil); st["state"] != "confirmed" {
		t.Fatalf("confirmed: %v", st)
	}
	time.Sleep(4100 * time.Millisecond)
	got := pair(url.Values{"pairing_code": {code}})
	tok := got.JSON()
	if got.Status != 200 || !strings.HasPrefix(tok["access_token"].(string), "churr_app_") || tok["device_id"] == nil {
		t.Fatalf("tokens: %d %s", got.Status, got.Body)
	}
	app := ts.App(tok["access_token"].(string))
	signedInAs(t, app, "ann")
	for _, s := range app.Must(200, "GET", "/api/v1/me/sessions", nil)["sessions"].([]any) {
		if m := s.(map[string]any); m["current"] == true && (m["signed_in_by"] != "pairing" || m["label"] != "Ann's iPhone" || m["kind"] != "app") {
			t.Fatalf("the paired session: %v", m)
		}
	}
	// Single use.
	time.Sleep(2100 * time.Millisecond)
	wantErr("a used code", pair(url.Values{"pairing_code": {code}}), "invalid_grant")
	ann.Refused(404, model.CodeNotFound, "GET", path, nil)
}
