package api_test

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Company sign-in over OIDC against a provider that runs in the test
// process (docs/AUTH.md sections 7 to 9).

// oidcSetup adds an OIDC provider as the owner and offers company sign-in.
func oidcSetup(t *testing.T, ts *testkit.TestServer, extra map[string]any) (*testkit.FakeIdP, string) {
	t.Helper()
	idp := testkit.NewIdP(t)
	body := map[string]any{"kind": "oidc", "name": "Acme ID", "secret": idp.ClientSecret,
		"config": map[string]any{"issuer": idp.Issuer, "client_id": idp.ClientID}}
	for k, v := range extra {
		body[k] = v
	}
	p := at(ts.Owner.Must(201, "POST", "/api/v1/admin/sso", body), "provider")
	ts.Owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"auth.member_sign_in": "either", "registration.mode": "open"})
	return idp, p["id"].(string)
}

// local turns an address on the server's origin into a path, so the test
// browser's cookies (kept for the listening address) go with it.
func local(ts *testkit.TestServer, addr string) string { return strings.TrimPrefix(addr, ts.Origin) }

// ssoStart begins a company sign-in and returns where the browser is sent.
func ssoStart(t *testing.T, c *testkit.Client, provider, query string) string {
	t.Helper()
	p := c.Fetch("GET", "/api/v1/sso/"+provider+"/start"+query, nil)
	if p.Status != 302 || p.Location() == "" {
		t.Fatalf("start = %d %s", p.Status, p.Body)
	}
	return p.Location()
}

var hiddenToken = regexp.MustCompile(`name="token" value="([^"]+)"`)

// oidcSignIn takes a browser through the provider and back, choosing the
// username on a first sign-in. It returns the last page.
func oidcSignIn(t *testing.T, ts *testkit.TestServer, c *testkit.Client, idp *testkit.FakeIdP, provider, query string, user testkit.IdPUser) testkit.Page {
	t.Helper()
	back := idp.Approve(ssoStart(t, c, provider, query), user)
	page := c.Fetch("GET", local(ts, back), nil)
	if m := hiddenToken.FindStringSubmatch(page.Body); m != nil && page.Status == 200 {
		page = c.Fetch("POST", "/sso/register", url.Values{"token": {m[1]}, "username": {user.Username}, "display_name": {user.Name}})
	}
	return page
}

func signedInAs(t *testing.T, c *testkit.Client, username string) map[string]any {
	t.Helper()
	me := at(c.Must(200, "GET", "/api/v1/me", nil), "account")
	if me["username"] != username {
		t.Fatalf("signed in as %v, want %s", me["username"], username)
	}
	return me
}

func TestSsoProviders(t *testing.T) {
	ts := testkit.NewServer(t)
	owner, ann := ts.Owner, ts.Member("ann")
	idp := testkit.NewIdP(t)
	body := map[string]any{"kind": "oidc", "name": "Acme ID", "secret": idp.ClientSecret, "group_claim": "groups", "admin_group": "churr-admins",
		"config": map[string]any{"issuer": idp.Issuer, "client_id": idp.ClientID, "scopes": []string{"groups"}, "acr_values": []string{"mfa"}}}

	// Admins only, with a fresh check.
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/sso", body)
	ann.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/sso", nil)
	owner.Stale()
	owner.Refused(403, model.CodeFreshCheckRequired, "POST", "/api/v1/admin/sso", body)
	owner.Check()

	// The config is checked, and the discovery document fetched now.
	bad := func(code model.Code, status int, change func(m map[string]any)) {
		t.Helper()
		b := map[string]any{"kind": "oidc", "name": "X", "secret": "s", "config": map[string]any{"issuer": idp.Issuer, "client_id": idp.ClientID}}
		change(b)
		owner.Refused(status, code, "POST", "/api/v1/admin/sso", b)
	}
	bad(model.CodeInvalidValue, 400, func(m map[string]any) { m["kind"] = "ldap" })
	bad(model.CodeInvalidValue, 400, func(m map[string]any) { delete(m, "name") })
	bad(model.CodeInvalidValue, 400, func(m map[string]any) { delete(m, "config") })
	bad(model.CodeInvalidValue, 400, func(m map[string]any) {
		m["config"] = map[string]any{"issuer": "http://plain.example", "client_id": "x"}
	})
	bad(model.CodeInvalidValue, 400, func(m map[string]any) { m["config"] = map[string]any{"issuer": idp.Issuer} })
	bad(model.CodeInvalidValue, 400, func(m map[string]any) {
		m["config"] = map[string]any{"issuer": idp.Issuer, "client_id": "x", "surprise": 1}
	})
	// The issuer in the document must equal the configured one exactly.
	bad(model.CodeUnavailable, 503, func(m map[string]any) { m["config"] = map[string]any{"issuer": idp.Issuer + "/", "client_id": "x"} })
	bad(model.CodeInvalidRequest, 400, func(m map[string]any) { m["surprise"] = true })

	p := at(owner.Must(201, "POST", "/api/v1/admin/sso", body), "provider")
	id := p["id"].(string)
	if p["kind"] != "oidc" || p["has_secret"] != true || p["enabled"] != true || p["admin_group"] != "churr-admins" ||
		p["callback_url"] != ts.Origin+"/api/v1/sso/"+id+"/callback" || p["secret"] != nil {
		t.Fatalf("provider: %v", p)
	}
	if e := wantAudit(t, ts, "sso_provider.added"); strings.Contains(strings.ToLower(e.Detail["name"].(string)+" "+strings.Join(anyStrings(e.Detail["fields"]), " ")), idp.ClientSecret) {
		t.Fatalf("the audit log must not hold the secret: %+v", e)
	}
	// The stored secret is sealed.
	stored, err := ts.Server.Store.Identities().GetProvider(context.Background(), id)
	if err != nil || len(stored.Secret) == 0 || strings.Contains(string(stored.Secret), idp.ClientSecret) {
		t.Fatalf("stored secret: %v", err)
	}

	if list := owner.Must(200, "GET", "/api/v1/admin/sso", nil)["providers"].([]any); len(list) != 1 {
		t.Fatalf("list: %v", list)
	}
	owner.Must(200, "GET", "/api/v1/admin/sso/"+id, nil)
	owner.Refused(404, model.CodeNotFound, "GET", "/api/v1/admin/sso/"+string(model.NewID[model.AccountID]()), nil)
	if out := owner.Must(200, "POST", "/api/v1/admin/sso/"+id+"/test", nil); out["ok"] != true {
		t.Fatalf("test: %v", out)
	}

	// Nothing is offered while member sign-in is `passkey`.
	if list := at(ts.Browser().Must(200, "GET", "/api/v1/server", nil), "server", "sign_in")["providers"].([]any); len(list) != 0 {
		t.Fatalf("providers offered under passkey policy: %v", list)
	}
	ts.Browser().Refused(403, model.CodeFeatureOff, "GET", "/api/v1/sso/"+id+"/start", nil)
	// `sso` needs an enabled provider.
	owner.Must(200, "PATCH", "/api/v1/admin/sso/"+id, map[string]any{"enabled": false})
	owner.Refused(409, model.CodeConflict, "PATCH", "/api/v1/admin/settings", map[string]any{"auth.member_sign_in": "sso"})
	changed := at(owner.Must(200, "PATCH", "/api/v1/admin/sso/"+id, map[string]any{"enabled": true, "name": "Acme"}), "provider")
	if changed["name"] != "Acme" || changed["has_secret"] != true {
		t.Fatalf("changed: %v", changed)
	}
	wantAudit(t, ts, "sso_provider.changed")
	owner.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/admin/sso/"+id, map[string]any{"kind": "saml"})
	owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"auth.member_sign_in": "sso"})
	list := at(ts.Browser().Must(200, "GET", "/api/v1/server", nil), "server", "sign_in")["providers"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["name"] != "Acme" || list[0].(map[string]any)["id"] != id {
		t.Fatalf("providers offered: %v", list)
	}
	ts.Browser().Refused(404, model.CodeNotFound, "GET", "/api/v1/sso/"+string(model.NewID[model.AccountID]())+"/start", nil)

	// The last enabled provider cannot go while members must use it.
	owner.Refused(409, model.CodeConflict, "DELETE", "/api/v1/admin/sso/"+id, nil)
	owner.Refused(409, model.CodeConflict, "PATCH", "/api/v1/admin/sso/"+id, map[string]any{"enabled": false})
	owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"auth.member_sign_in": "either"})
	owner.Stale()
	owner.Refused(403, model.CodeFreshCheckRequired, "DELETE", "/api/v1/admin/sso/"+id, nil)
	owner.Check()
	owner.Must(204, "DELETE", "/api/v1/admin/sso/"+id, nil)
	wantAudit(t, ts, "sso_provider.removed")
	owner.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/admin/sso/"+id, nil)
}

func anyStrings(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, x := range list {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

func TestOidcSignIn(t *testing.T) {
	ts := testkit.NewServer(t)
	idp, pid := oidcSetup(t, ts, nil)
	ctx := context.Background()

	// The request carries PKCE, a state and a nonce, and binds the
	// browser with a cookie limited to the callback.
	b := ts.Browser()
	loc := ssoStart(t, b, pid, "")
	q, _ := url.Parse(loc)
	if got := q.Query().Get("redirect_uri"); got != ts.Origin+"/api/v1/sso/"+pid+"/callback" {
		t.Fatalf("redirect_uri: %s", got)
	}
	if s := q.Query().Get("scope"); s != "openid profile" {
		t.Fatalf("scope %q: the email scope is asked for only while imported accounts wait", s)
	}
	if b.Cookie("churr_sso", "/api/v1/sso/"+pid+"/callback") == "" || b.Cookie("churr_sso", "/api/v1/me") != "" {
		t.Fatal("the binding cookie must be set, with its path limited to the callback")
	}

	// A first sign-in: the person confirms a username, the provider's
	// suggestion filled in.
	page := b.Fetch("GET", local(ts, idp.Approve(loc, testkit.IdPUser{Subject: "sub-ann", Username: "Ann.Lee", Name: "Ann Lee", Email: "ann@example.org", EmailVerified: true})), nil)
	if page.Status != 200 || !strings.Contains(page.Body, `value="ann.lee"`) || !strings.Contains(page.Body, "Choose a username") {
		t.Fatalf("registration page: %d %s", page.Status, page.Body)
	}
	if b.Cookie("churr_session", "/") != "" {
		t.Fatal("no session before the account exists")
	}
	token := hiddenToken.FindStringSubmatch(page.Body)[1]
	// A name that is taken gives the page back with the reason.
	again := b.Fetch("POST", "/sso/register", url.Values{"token": {token}, "username": {"owner"}})
	if again.Status != 200 || !strings.Contains(again.Body, "That name isn&#39;t available.") {
		t.Fatalf("taken name: %d %s", again.Status, again.Body)
	}
	done := b.Fetch("POST", "/sso/register", url.Values{"token": {token}, "username": {"ann"}, "display_name": {"Ann Lee"}})
	// The answer is a page from this server's own origin, not a redirect.
	if done.Status != 200 || done.Location() != "" || !strings.Contains(done.Body, `url=/"`) {
		t.Fatalf("after registering: %d %s", done.Status, done.Body)
	}
	me := signedInAs(t, b, "ann")
	if me["role"] != "member" || me["passkey_count"] != float64(0) || me["identity_count"] != float64(1) {
		t.Fatalf("account: %v", me)
	}
	if e := wantAudit(t, ts, "account.registered"); e.Detail["how"] != "Acme ID" {
		t.Fatalf("audit: %+v", e)
	}
	// The registration cannot be replayed.
	if p := ts.Browser().Fetch("POST", "/sso/register", url.Values{"token": {token}, "username": {"ann2"}}); p.Status != 404 {
		t.Fatalf("replayed registration: %d", p.Status)
	}

	// The subject is stored; no email address is, anywhere.
	ids := b.Must(200, "GET", "/api/v1/me/identities", nil)["identities"].([]any)
	if len(ids) != 1 || ids[0].(map[string]any)["provider_name"] != "Acme ID" || ids[0].(map[string]any)["subject"] != nil {
		t.Fatalf("identities: %v", ids)
	}
	annID := model.AccountID(me["id"].(string))
	stored, _ := ts.Server.Store.Identities().ForAccount(ctx, annID)
	if len(stored) != 1 || stored[0].Subject != "sub-ann" || stored[0].LastUsedAt.IsZero() {
		t.Fatalf("stored identity: %+v", stored)
	}
	for _, e := range append(audited(t, ts, "account.registered"), audited(t, ts, "identity.linked")...) {
		for _, v := range e.Detail {
			if s, _ := v.(string); strings.Contains(s, "example.org") {
				t.Fatalf("an email address reached the audit log: %+v", e)
			}
		}
	}

	// The session: signed in by oidc, 24 hours from the sign-in, fixed.
	sessions := b.Must(200, "GET", "/api/v1/me/sessions", nil)["sessions"].([]any)
	ses := sessions[0].(map[string]any)
	exp, _ := time.Parse(time.RFC3339Nano, ses["expires_at"].(string))
	if ses["signed_in_by"] != "oidc" || time.Until(exp) > 24*time.Hour || time.Until(exp) < 23*time.Hour {
		t.Fatalf("session: %v", ses)
	}
	// Use does not extend it, however long ago it was last seen.
	sid := b.SessionID()
	ts.Exec(`UPDATE sessions SET last_seen_at = ? WHERE id = ?`, int64(model.Now().Add(-2*time.Hour)), string(sid))
	b.Must(200, "GET", "/api/v1/me", nil)
	if after, _ := ts.Server.Store.Sessions().Get(ctx, sid); after.ExpiresAt != model.FromTime(exp) {
		t.Fatalf("a company session must not slide: %v then %v", exp, after.ExpiresAt.Time())
	}
	// A company sign-in counts as a fresh check for ten minutes.
	b.Must(201, "POST", "/api/v1/me/tokens", map[string]any{"label": "t"})

	// Signing in again finds the account: no page in between.
	b2 := ts.Browser()
	page = oidcSignIn(t, ts, b2, idp, pid, "?return_to=/somewhere%3Fx%3D1", testkit.IdPUser{Subject: "sub-ann", Username: "someone-else"})
	if page.Status != 200 || !strings.Contains(page.Body, `url=/somewhere?x=1"`) {
		t.Fatalf("second sign-in: %d %s", page.Status, page.Body)
	}
	signedInAs(t, b2, "ann")
	// Only a path on this server is somewhere to go next.
	b3 := ts.Browser()
	page = oidcSignIn(t, ts, b3, idp, pid, "?return_to=https://evil.example/", testkit.IdPUser{Subject: "sub-ann"})
	if !strings.Contains(page.Body, `url=/"`) {
		t.Fatalf("an outside return_to was kept: %s", page.Body)
	}

	// A banned sign-in identity is refused before any account is found
	// or made (docs/PERMISSIONS.md section 17, case 139).
	ts.Exec(`INSERT INTO bans (id, provider_id, subject, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		string(model.NewID[model.AccountID]()), pid, "sub-banned", string(ts.Owner.ID), int64(model.Now()))
	page = oidcSignIn(t, ts, ts.Browser(), idp, pid, "", testkit.IdPUser{Subject: "sub-banned", Username: "mallory"})
	if page.Status != 403 || strings.Contains(page.Body, "Choose a username") {
		t.Fatalf("banned identity: %d %s", page.Status, page.Body)
	}
	if e := wantAudit(t, ts, "ban.sign_in_refused"); e.Detail["provider"] != "Acme ID" {
		t.Fatalf("audit: %+v", e)
	}

	// Banned while choosing a username: no account is made.
	late := ts.Browser()
	page = late.Fetch("GET", local(ts, idp.Approve(ssoStart(t, late, pid, ""), testkit.IdPUser{Subject: "sub-late", Username: "late"})), nil)
	ts.Exec(`INSERT INTO bans (id, provider_id, subject, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		string(model.NewID[model.AccountID]()), pid, "sub-late", string(ts.Owner.ID), int64(model.Now()))
	page = late.Fetch("POST", "/sso/register", url.Values{"token": {hiddenToken.FindStringSubmatch(page.Body)[1]}, "username": {"late"}})
	if page.Status != 403 || late.Cookie("churr_session", "/") != "" {
		t.Fatalf("banned before registering: %d %s", page.Status, page.Body)
	}
	if _, err := ts.Server.Store.Accounts().ByUsername(ctx, "late"); err == nil {
		t.Fatal("a banned identity made an account")
	}
	// A linked identity that is banned later is refused too.
	ts.Exec(`INSERT INTO bans (id, provider_id, subject, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		string(model.NewID[model.AccountID]()), pid, "sub-ann", string(ts.Owner.ID), int64(model.Now()))
	if page = oidcSignIn(t, ts, ts.Browser(), idp, pid, "", testkit.IdPUser{Subject: "sub-ann"}); page.Status != 403 {
		t.Fatalf("a banned identity with an account: %d", page.Status)
	}
	ts.Exec(`UPDATE bans SET lifted_at = ? WHERE subject = ?`, int64(model.Now()), "sub-ann")

	// Registration modes: `invite` needs a usable invite, `closed` takes nobody.
	ts.Set("registration.mode", "invite")
	page = oidcSignIn(t, ts, ts.Browser(), idp, pid, "", testkit.IdPUser{Subject: "sub-bob", Username: "bob"})
	if page.Status != 403 || !strings.Contains(page.Body, "invite") {
		t.Fatalf("no invite: %d %s", page.Status, page.Body)
	}
	code := at(ts.Owner.Must(201, "POST", "/api/v1/invites", map[string]any{}), "invite")["code"].(string)
	bob := ts.Browser()
	page = oidcSignIn(t, ts, bob, idp, pid, "?invite_code="+code, testkit.IdPUser{Subject: "sub-bob", Username: "bob", Name: "Bob"})
	if page.Status != 200 {
		t.Fatalf("with an invite: %d %s", page.Status, page.Body)
	}
	signedInAs(t, bob, "bob")
	if e := audited(t, ts, "account.registered")[0]; e.Detail["invite_id"] == nil {
		t.Fatalf("the registration names its invite: %+v", e)
	}
	// The invite is used up.
	page = oidcSignIn(t, ts, ts.Browser(), idp, pid, "?invite_code="+code, testkit.IdPUser{Subject: "sub-carl", Username: "carl"})
	if page.Status != 403 {
		t.Fatalf("a spent invite: %d", page.Status)
	}
	ts.Set("registration.mode", "closed")
	page = oidcSignIn(t, ts, ts.Browser(), idp, pid, "", testkit.IdPUser{Subject: "sub-dan", Username: "dan"})
	if page.Status != 403 {
		t.Fatalf("closed registration: %d", page.Status)
	}
	// Existing accounts sign in as before.
	signedInAs(t, func() *testkit.Client {
		c := ts.Browser()
		oidcSignIn(t, ts, c, idp, pid, "", testkit.IdPUser{Subject: "sub-bob"})
		return c
	}(), "bob")

	// A removed account's identity is gone with it; a disabled provider
	// offers nothing.
	ts.Owner.Must(200, "PATCH", "/api/v1/admin/sso/"+pid, map[string]any{"enabled": false})
	if p := ts.Browser().Fetch("GET", "/api/v1/sso/"+pid+"/start", nil); p.Status != 404 {
		t.Fatalf("a disabled provider: %d", p.Status)
	}
}

// The checks on what comes back from the provider (docs/AUTH.md section
// 7.2): every one must refuse the sign-in and leave no session.
func TestOidcRefusals(t *testing.T) {
	ts := testkit.NewServer(t)
	idp, pid := oidcSetup(t, ts, nil)
	good := testkit.IdPUser{Subject: "sub-ann", Username: "ann", Name: "Ann"}
	refused := func(name string, page testkit.Page, c *testkit.Client) {
		t.Helper()
		if page.Status != 401 || !strings.Contains(page.Body, "Sign-in through Acme ID failed.") || c.Cookie("churr_session", "/") != "" {
			t.Fatalf("%s: got %d %s (session cookie %q)", name, page.Status, page.Body, c.Cookie("churr_session", "/"))
		}
		// The details are not shown.
		for _, leak := range []string{"nonce", "signature", "audience", "expired", "issuer", "alg"} {
			if strings.Contains(strings.ToLower(page.Body), leak) {
				t.Fatalf("%s: the page says why: %s", name, page.Body)
			}
		}
	}
	tamper := func(name string, user testkit.IdPUser) {
		t.Helper()
		c := ts.Browser()
		back := idp.Approve(ssoStart(t, c, pid, ""), user)
		refused(name, c.Fetch("GET", local(ts, back), nil), c)
	}
	with := func(change func(*testkit.IdPUser)) testkit.IdPUser { u := good; change(&u); return u }

	tamper("alg none", with(func(u *testkit.IdPUser) { u.Alg = "none" }))
	tamper("HMAC", with(func(u *testkit.IdPUser) { u.Alg = "HS256" }))
	tamper("wrong nonce", with(func(u *testkit.IdPUser) { u.Tamper = func(c map[string]any) { c["nonce"] = "another" } }))
	tamper("no nonce", with(func(u *testkit.IdPUser) { u.Tamper = func(c map[string]any) { delete(c, "nonce") } }))
	tamper("wrong audience", with(func(u *testkit.IdPUser) { u.Tamper = func(c map[string]any) { c["aud"] = "another-client" } }))
	tamper("several audiences without azp", with(func(u *testkit.IdPUser) {
		u.Tamper = func(c map[string]any) { c["aud"] = []string{idp.ClientID, "another-client"} }
	}))
	tamper("several audiences with another azp", with(func(u *testkit.IdPUser) {
		u.Tamper = func(c map[string]any) {
			c["aud"] = []string{idp.ClientID, "another-client"}
			c["azp"] = "another-client"
		}
	}))
	tamper("wrong issuer", with(func(u *testkit.IdPUser) { u.Tamper = func(c map[string]any) { c["iss"] = "https://evil.example" } }))
	tamper("expired", with(func(u *testkit.IdPUser) {
		u.Tamper = func(c map[string]any) { c["exp"] = time.Now().Add(-2 * time.Minute).Unix() }
	}))
	tamper("no expiry", with(func(u *testkit.IdPUser) { u.Tamper = func(c map[string]any) { delete(c, "exp") } }))
	tamper("issued in the future", with(func(u *testkit.IdPUser) {
		u.Tamper = func(c map[string]any) { c["iat"] = time.Now().Add(5 * time.Minute).Unix() }
	}))
	tamper("no subject", with(func(u *testkit.IdPUser) { u.Subject = "" }))
	tamper("a subject too long", with(func(u *testkit.IdPUser) { u.Subject = strings.Repeat("x", 256) }))

	// Clocks may differ by a minute, and no more.
	c := ts.Browser()
	page := oidcSignIn(t, ts, c, idp, pid, "", with(func(u *testkit.IdPUser) {
		u.Tamper = func(c map[string]any) {
			c["exp"] = time.Now().Add(-30 * time.Second).Unix()
			c["iat"] = time.Now().Add(30 * time.Second).Unix()
			c["aud"], c["azp"] = []string{idp.ClientID, "another-client"}, idp.ClientID
		}
	}))
	if page.Status != 200 {
		t.Fatalf("within the allowance: %d %s", page.Status, page.Body)
	}
	signedInAs(t, c, "ann")

	// The state: unknown, used twice, or answered in another browser. (A
	// second server, because starting a sign-in is limited per address.)
	ts = testkit.NewServer(t)
	idp, pid = oidcSetup(t, ts, nil)
	c = ts.Browser()
	back := idp.Approve(ssoStart(t, c, pid, ""), good)
	u, _ := url.Parse(back)
	bq := u.Query()
	bq.Set("state", "made-up")
	refused("unknown state", c.Fetch("GET", local(ts, u.Path+"?"+bq.Encode()), nil), c)

	c = ts.Browser()
	back = idp.Approve(ssoStart(t, c, pid, ""), good)
	thief := ts.Browser()
	// Without the binding cookie the answer is refused, and the request
	// is spent: the rightful browser cannot use it afterwards either.
	refused("another browser", thief.Fetch("GET", local(ts, back), nil), thief)
	refused("a state used twice", c.Fetch("GET", local(ts, back), nil), c)

	// An error from the provider, an answer from another issuer, no code.
	for name, change := range map[string]func(q url.Values){
		"provider error": func(q url.Values) { q.Set("error", "access_denied") },
		"another issuer": func(q url.Values) { q.Set("iss", "https://evil.example") },
		"no code":        func(q url.Values) { q.Del("code") },
		"a made-up code": func(q url.Values) { q.Set("code", "made-up") },
	} {
		c = ts.Browser()
		back = idp.Approve(ssoStart(t, c, pid, ""), good)
		u, _ = url.Parse(back)
		bq = u.Query()
		change(bq)
		refused(name, c.Fetch("GET", local(ts, u.Path+"?"+bq.Encode()), nil), c)
	}
	// Nothing an outsider can trigger at will fills the audit log.
	if n := len(audited(t, ts, "ban.sign_in_refused")); n != 0 {
		t.Fatalf("%d audit entries from failed sign-ins", n)
	}

	// A wrong client secret fails at the token endpoint.
	ts.Owner.Must(200, "PATCH", "/api/v1/admin/sso/"+pid, map[string]any{"secret": "wrong"})
	tamper("wrong client secret", good)
}

func TestSsoGroupsAndRole(t *testing.T) {
	ts := testkit.NewServer(t)
	idp, pid := oidcSetup(t, ts, map[string]any{"admin_group": "churr-admins"})
	ctx := context.Background()
	g := model.Group{Handle: "eng", Name: "Engineering", ProviderID: pid, ProviderGroup: "engineering"}
	if err := ts.Server.Store.Groups().Create(ctx, &g); err != nil {
		t.Fatal(err)
	}
	role := func(c *testkit.Client) any { return at(c.Must(200, "GET", "/api/v1/me", nil), "account")["role"] }
	inGroup := func(id model.AccountID) bool {
		ok, err := ts.Server.Store.Groups().IsMember(ctx, g.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	signIn := func(groups []string) *testkit.Client {
		c := ts.Browser()
		if p := oidcSignIn(t, ts, c, idp, pid, "", testkit.IdPUser{Subject: "sub-ann", Username: "ann", Groups: groups}); p.Status != 200 {
			t.Fatalf("sign-in: %d %s", p.Status, p.Body)
		}
		return c
	}
	// Listed in the admin group: admin. In the mapped group: a member of it.
	c := signIn([]string{"churr-admins", "engineering"})
	id := model.AccountID(signedInAs(t, c, "ann")["id"].(string))
	if role(c) != "admin" || !inGroup(id) {
		t.Fatalf("role %v, in group %v", role(c), inGroup(id))
	}
	if e := wantAudit(t, ts, "account.role_changed"); e.Detail["new"] != "admin" || e.Detail["cause"] != "provider group" {
		t.Fatalf("audit: %+v", e)
	}
	// An absent claim changes nothing: it is a misconfiguration, not "in no groups".
	c = signIn(nil)
	if role(c) != "admin" || !inGroup(id) {
		t.Fatalf("an absent claim changed something: role %v, in group %v", role(c), inGroup(id))
	}
	// Names are compared exactly.
	c = signIn([]string{"Churr-Admins", "Engineering"})
	if role(c) != "member" || inGroup(id) {
		t.Fatalf("not listed: role %v, in group %v", role(c), inGroup(id))
	}
	// An empty list is "in no groups".
	c = signIn([]string{"churr-admins"})
	if role(c) != "admin" {
		t.Fatal("listed again")
	}
	c = signIn([]string{})
	if role(c) != "member" {
		t.Fatal("an empty list means not listed")
	}

	// The owner's role is never changed by a provider, and a guest is
	// never promoted.
	owner := ts.Owner
	loc := ssoStart(t, owner, pid, "?purpose=link")
	if p := owner.Fetch("GET", local(ts, idp.Approve(loc, testkit.IdPUser{Subject: "sub-owner", Groups: []string{}})), nil); p.Status != 200 {
		t.Fatalf("linking: %d %s", p.Status, p.Body)
	}
	oc := ts.Browser()
	oidcSignIn(t, ts, oc, idp, pid, "", testkit.IdPUser{Subject: "sub-owner", Groups: []string{}})
	if role(oc) != "owner" {
		t.Fatalf("the owner became %v", role(oc))
	}
	if _, _, err := ts.Server.Store.Accounts().SetRole(ctx, id, model.RoleGuest); err != nil {
		t.Fatal(err)
	}
	c = signIn([]string{"churr-admins"})
	if role(c) != "guest" {
		t.Fatalf("a guest became %v", role(c))
	}
}

func TestSsoLink(t *testing.T) {
	ts := testkit.NewServer(t)
	idp, pid := oidcSetup(t, ts, nil)
	ann, bob := ts.Member("ann"), ts.Member("bob")

	// Linking needs someone signed in, with a fresh check.
	if p := ts.Browser().Fetch("GET", "/api/v1/sso/"+pid+"/start?purpose=link", nil); p.Status != 403 {
		t.Fatalf("linking signed out: %d", p.Status)
	}
	ann.Stale()
	if p := ann.Fetch("GET", "/api/v1/sso/"+pid+"/start?purpose=link", nil); p.Status != 403 || p.JSON()["error"].(map[string]any)["code"] != "fresh_check_required" {
		t.Fatalf("linking without a fresh check: %d %s", p.Status, p.Body)
	}
	if p := ann.Fetch("GET", "/api/v1/sso/"+pid+"/start?purpose=steal", nil); p.Status != 400 {
		t.Fatalf("an unknown purpose: %d", p.Status)
	}
	ann.Check()
	loc := ssoStart(t, ann, pid, "?purpose=link&return_to=/account")
	page := ann.Fetch("GET", local(ts, idp.Approve(loc, testkit.IdPUser{Subject: "sub-ann", Username: "whoever"})), nil)
	if page.Status != 200 || !strings.Contains(page.Body, `url=/account"`) {
		t.Fatalf("linked: %d %s", page.Status, page.Body)
	}
	if n := len(ann.Must(200, "GET", "/api/v1/me/identities", nil)["identities"].([]any)); n != 1 {
		t.Fatalf("%d identities, want 1", n)
	}
	if e := wantAudit(t, ts, "identity.linked"); e.ActorID != string(ann.ID) || e.Detail["provider"] != "Acme ID" {
		t.Fatalf("audit: %+v", e)
	}
	// The identity now signs in as the account.
	c := ts.Browser()
	oidcSignIn(t, ts, c, idp, pid, "", testkit.IdPUser{Subject: "sub-ann"})
	signedInAs(t, c, "ann")
	// With a linked identity the last passkey can go.
	keys := ann.Must(200, "GET", "/api/v1/me/passkeys", nil)["passkeys"].([]any)
	ann.Must(204, "DELETE", "/api/v1/me/passkeys/"+keys[0].(map[string]any)["id"].(string), nil)

	// A subject belongs to one account.
	loc = ssoStart(t, bob, pid, "?purpose=link")
	page = bob.Fetch("GET", local(ts, idp.Approve(loc, testkit.IdPUser{Subject: "sub-ann"})), nil)
	if page.Status != 409 {
		t.Fatalf("a subject linked twice: %d %s", page.Status, page.Body)
	}
	if n := len(bob.Must(200, "GET", "/api/v1/me/identities", nil)["identities"].([]any)); n != 0 {
		t.Fatal("bob has no identity")
	}

	// A recovery link leads to linking where the role must use company
	// sign-in: the provider's answer replaces the recovery session.
	link := ts.Owner.Must(201, "POST", "/api/v1/admin/accounts/"+string(bob.ID)+"/recovery-link", nil)["url"].(string)
	ts.Owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"auth.member_sign_in": "sso"})
	r := ts.Browser()
	r.Must(200, "POST", "/api/v1/recovery/link", map[string]any{"token": link[strings.LastIndex(link, "/")+1:]})
	loc = ssoStart(t, r, pid, "?purpose=link")
	page = r.Fetch("GET", local(ts, idp.Approve(loc, testkit.IdPUser{Subject: "sub-bob"})), nil)
	if page.Status != 200 {
		t.Fatalf("linking from recovery: %d %s", page.Status, page.Body)
	}
	signedInAs(t, r, "bob")
	for _, s := range r.Must(200, "GET", "/api/v1/me/sessions", nil)["sessions"].([]any) {
		m := s.(map[string]any)
		if m["recovery"] == true || (m["current"] == true && m["signed_in_by"] != "oidc") {
			t.Fatalf("sessions after linking from recovery: %v", m)
		}
	}
	r.Must(200, "GET", "/api/v1/conversations", nil)
	// A recovery code session does not lead there.
	codes := newCodes(ts.Owner)
	o := ts.Browser()
	o.Must(200, "POST", "/api/v1/recovery/code", map[string]any{"username": "owner", "code": codes[0]})
	if p := o.Fetch("GET", "/api/v1/sso/"+pid+"/start?purpose=link", nil); p.Status != 403 {
		t.Fatalf("linking from a recovery code: %d", p.Status)
	}
}

// The sign-in policy (docs/AUTH.md section 8): under `sso`, members cannot
// sign in or register with a passkey; admins and the owner always can.
func TestSignInPolicy(t *testing.T) {
	ts := testkit.NewServer(t)
	_, pid := oidcSetup(t, ts, nil)
	admin, ann := ts.Admin("adm"), ts.Member("ann")
	code := at(ts.Owner.Must(201, "POST", "/api/v1/invites", map[string]any{}), "invite")["code"].(string)
	ts.Owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"auth.member_sign_in": "sso"})

	if status, out := ann.Login(); status != 403 {
		t.Fatalf("a member's passkey under sso: %d %v", status, out)
	}
	if status, _ := admin.Login(); status != 200 {
		t.Fatalf("an admin's passkey under sso: %d", status)
	}
	if status, _ := ts.Owner.Login(); status != 200 {
		t.Fatalf("the owner's passkey under sso: %d", status)
	}
	ts.Browser().Refused(403, model.CodeForbidden, "POST", "/api/v1/passkey/register/begin", map[string]any{"invite_code": code, "username": "new", "display_name": "New"})
	// Changing the policy does not end sessions already there.
	ann.Must(200, "GET", "/api/v1/me", nil)
	// The check is made on the role at the moment of sign-in.
	if _, _, err := ts.Server.Store.Accounts().SetRole(context.Background(), ann.ID, model.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if status, _ := ann.Login(); status != 200 {
		t.Fatalf("promoted to admin, the passkey works: %d", status)
	}
	// An admin who would be locked out cannot turn it on: they need a passkey.
	ts.Owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"auth.member_sign_in": "passkey"})
	if p := ts.Browser().Fetch("GET", "/api/v1/sso/"+pid+"/start", nil); p.Status != 403 {
		t.Fatalf("company sign-in under passkey policy: %d", p.Status)
	}
}

// The second factor (docs/AUTH.md section 9).
func TestSecondFactor(t *testing.T) {
	ts := testkit.NewServer(t)
	idp, pid := oidcSetup(t, ts, nil)
	owner := ts.Owner
	user := testkit.IdPUser{Subject: "sub-ann", Username: "ann", Name: "Ann"}
	c := ts.Browser()
	oidcSignIn(t, ts, c, idp, pid, "", user)
	signedInAs(t, c, "ann")

	// Requiring one with nothing accepted is refused.
	owner.Refused(409, model.CodeConflict, "PATCH", "/api/v1/admin/settings", map[string]any{"auth.second_factor": "required", "auth.second_factor_accepted": []string{}})
	owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"auth.second_factor": "required", "auth.second_factor_accepted": []string{"provider"}})

	// Only the provider's own statement is ticked: without it the
	// sign-in is refused, with it the session starts complete.
	c = ts.Browser()
	page := oidcSignIn(t, ts, c, idp, pid, "", user)
	if page.Status != 403 || c.Cookie("churr_session", "/") != "" {
		t.Fatalf("no statement and nothing else ticked: %d %s", page.Status, page.Body)
	}
	c = ts.Browser()
	mfa := user
	mfa.AMR = []string{"pwd", "mfa"}
	oidcSignIn(t, ts, c, idp, pid, "", mfa)
	signedInAs(t, c, "ann")
	if s := c.Must(200, "GET", "/api/v1/me/sessions", nil)["sessions"].([]any)[0].(map[string]any); s["second_factor"] != "provider" {
		t.Fatalf("session: %v", s)
	}

	// The provider's statement is accepted only if the admin ticked it.
	owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"auth.second_factor_accepted": []string{"totp", "passkey"}})
	c = ts.Browser()
	oidcSignIn(t, ts, c, idp, pid, "", mfa)
	// The session is pending: it can do nothing but give its proof.
	c.Refused(401, model.CodeSecondFactorRequired, "GET", "/api/v1/me", nil)
	c.Refused(401, model.CodeSecondFactorRequired, "GET", "/api/v1/conversations", nil)
	c.Refused(401, model.CodeSecondFactorRequired, "GET", "/api/v1/me/sessions", nil)
	c.Refused(401, model.CodeSecondFactorRequired, "DELETE", "/api/v1/me/totp", nil)
	if _, resp, err := c.Dial(); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("a pending session opened the live connection: %v", err)
	}
	st := at(c.Must(200, "GET", "/api/v1/second-factor", nil), "second_factor")
	if len(st["accepted"].([]any)) != 2 || len(st["set_up"].([]any)) != 0 {
		t.Fatalf("second factor state: %v", st)
	}
	// Nothing set up yet: the first proof is enrolled now, without a
	// fresh check, and finishing the enrolment counts as the proof.
	c.Refused(403, model.CodeForbidden, "POST", "/api/v1/checks/totp", map[string]any{"code": "123456"})
	pending := c.Cookie("churr_session", "/")
	secret := c.Must(201, "POST", "/api/v1/me/totp", nil)["secret"].(string)
	done := c.Must(200, "POST", "/api/v1/me/totp/confirm", map[string]any{"code": testkit.TOTPCode(secret, -1)})
	ses := at(done, "sign_in", "session")
	if ses["pending_second_factor"] == true || ses["second_factor"] != "totp" || at(done, "sign_in", "account")["username"] != "ann" {
		t.Fatalf("after enrolling: %v", done)
	}
	exp, _ := time.Parse(time.RFC3339Nano, ses["expires_at"].(string))
	if time.Until(exp) < 23*time.Hour {
		t.Fatalf("the completed session lasts 24 hours from its creation, got %v", time.Until(exp))
	}
	// The pending token is replaced and stops working.
	if c.Cookie("churr_session", "/") == pending {
		t.Fatal("the token must change when the second factor is given")
	}
	old := ts.Browser()
	old.SetCookie("churr_session", pending)
	old.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	signedInAs(t, c, "ann")

	// From now on the account gives the proof it has; it cannot enrol
	// another from a pending session.
	c = ts.Browser()
	in := oidcSignIn(t, ts, c, idp, pid, "", user)
	if in.Status != 200 {
		t.Fatalf("sign-in: %d", in.Status)
	}
	st = at(c.Must(200, "GET", "/api/v1/second-factor", nil), "second_factor")
	if set := st["set_up"].([]any); len(set) != 1 || set[0] != "totp" {
		t.Fatalf("set up: %v", st)
	}
	c.Refused(403, model.CodeForbidden, "POST", "/api/v1/me/totp", nil)
	c.Refused(403, model.CodeForbidden, "POST", "/api/v1/me/passkeys/begin", nil)
	c.Refused(403, model.CodeForbidden, "POST", "/api/v1/checks/passkey/begin", nil)
	// The code that confirmed the app is spent.
	c.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/checks/totp", map[string]any{"code": testkit.TOTPCode(secret, -1)})
	full := c.Must(200, "POST", "/api/v1/checks/totp", map[string]any{"code": testkit.TOTPCode(secret, 0)})
	if at(full, "session")["second_factor"] != "totp" || at(full, "account")["username"] != "ann" {
		t.Fatalf("after the code: %v", full)
	}
	signedInAs(t, c, "ann")
	// Removing the only proof the account can give is refused.
	c.Refused(409, model.CodeConflict, "DELETE", "/api/v1/me/totp", nil)

	// Signing out is allowed while pending.
	c = ts.Browser()
	oidcSignIn(t, ts, c, idp, pid, "", user)
	c.Must(204, "POST", "/api/v1/logout", nil)
	c.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/second-factor", nil)

	// An admin's reset lets the person enrol again at the next sign-in.
	annID := model.AccountID(at(full, "account")["id"].(string))
	owner.Must(204, "DELETE", "/api/v1/admin/accounts/"+string(annID)+"/totp", nil)
	c = ts.Browser()
	oidcSignIn(t, ts, c, idp, pid, "", user)
	// This time with a passkey, which also makes the recovery codes.
	begin := c.Must(200, "POST", "/api/v1/me/passkeys/begin", nil)
	added := c.Must(201, "POST", "/api/v1/me/passkeys/finish", c.CreatePasskey(begin))
	if s := at(added, "sign_in", "session"); s["second_factor"] != "passkey" || s["pending_second_factor"] == true {
		t.Fatalf("enrolled a passkey: %v", added)
	}
	if n := len(at(added, "sign_in")["recovery_codes"].([]any)); n != 8 {
		t.Fatalf("%d recovery codes with the first passkey, want 8", n)
	}
	signedInAs(t, c, "ann")
	// Next time the passkey is the proof.
	c2 := ts.Browser()
	c2.CopyPasskey(c)
	oidcSignIn(t, ts, c2, idp, pid, "", user)
	c2.Refused(401, model.CodeSecondFactorRequired, "GET", "/api/v1/me", nil)
	out := c2.Check()
	if at(out, "session")["second_factor"] != "passkey" {
		t.Fatalf("passkey as the proof: %v", out)
	}
	signedInAs(t, c2, "ann")
	// A passkey sign-in already counts as two factors: no second step.
	c3 := ts.Browser()
	c3.CopyPasskey(c)
	if status, in := c3.Login(); status != 200 || at(in, "session")["pending_second_factor"] == true {
		t.Fatalf("passkey sign-in with a second factor required: %d %v", status, in)
	}
	c3.Must(200, "GET", "/api/v1/conversations", nil)
}

// Five wrong proofs end a pending session.
func TestSecondFactorWrongProofs(t *testing.T) {
	ts := testkit.NewServer(t)
	idp, pid := oidcSetup(t, ts, nil)
	user := testkit.IdPUser{Subject: "sub-ann", Username: "ann"}
	c := ts.Browser()
	oidcSignIn(t, ts, c, idp, pid, "", user)
	secret := c.Must(201, "POST", "/api/v1/me/totp", nil)["secret"].(string)
	c.Must(200, "POST", "/api/v1/me/totp/confirm", map[string]any{"code": testkit.TOTPCode(secret, -1)})
	ts.Owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"auth.second_factor": "required", "auth.second_factor_accepted": []string{"totp"}})

	c = ts.Browser()
	oidcSignIn(t, ts, c, idp, pid, "", user)
	for i := 0; i < 4; i++ {
		c.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/checks/totp", map[string]any{"code": "000000"})
	}
	c.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/checks/totp", map[string]any{"code": "000000"})
	// The session is gone: even the right code does nothing now.
	c.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/checks/totp", map[string]any{"code": testkit.TOTPCode(secret, 0)})
	c.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/second-factor", nil)
}

// An imported account is claimed without a link when the provider's
// verified email matches (docs/AUTH.md section 7.4).
func TestSsoClaimsImportedAccount(t *testing.T) {
	ts := testkit.NewServer(t)
	idp, pid := oidcSetup(t, ts, nil)
	ctx := context.Background()
	id := ts.Imported("old.ann")
	if err := ts.Server.Store.Identities().AddImportClaim(ctx, id, ts.Server.Auth.ImportEmailHash("Ann@Example.org")); err != nil {
		t.Fatal(err)
	}
	// While imported accounts wait, the email scope is asked for.
	b := ts.Browser()
	loc := ssoStart(t, b, pid, "")
	if u, _ := url.Parse(loc); !strings.Contains(u.Query().Get("scope"), "email") {
		t.Fatalf("scope: %s", u.Query().Get("scope"))
	}
	// An address the provider does not vouch for claims nothing.
	page := b.Fetch("GET", local(ts, idp.Approve(loc, testkit.IdPUser{Subject: "sub-eve", Username: "eve", Email: "ann@example.org"})), nil)
	if !strings.Contains(page.Body, "Choose a username") {
		t.Fatalf("an unverified address: %d %s", page.Status, page.Body)
	}
	c := ts.Browser()
	page = oidcSignIn(t, ts, c, idp, pid, "", testkit.IdPUser{Subject: "sub-ann", Username: "ann", Email: "ann@example.org", EmailVerified: true})
	if page.Status != 200 || strings.Contains(page.Body, "Choose a username") {
		t.Fatalf("claiming: %d %s", page.Status, page.Body)
	}
	me := signedInAs(t, c, "old.ann")
	if me["id"] != string(id) {
		t.Fatalf("claimed %v, want %s", me["id"], id)
	}
	if e := wantAudit(t, ts, "account.claimed"); e.Detail["by"] != "provider" {
		t.Fatalf("audit: %+v", e)
	}
	// The claim row is gone, and with it the hash of the address.
	if has, _ := ts.Server.Store.Identities().HasImportClaims(ctx); has {
		t.Fatal("the import claim was not deleted")
	}
}
