package api_test

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/auth"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Tests of sign-in beyond the first passkey (docs/AUTH.md): recovery, the
// authenticator app, sessions and tokens. Company sign-in, app sign-in,
// settings and integrations have files of their own.

// at walks into nested objects of a decoded answer.
func at(m map[string]any, path ...string) map[string]any {
	for _, k := range path {
		m, _ = m[k].(map[string]any)
	}
	return m
}

// audited returns the audit entries with one action, newest first.
func audited(t *testing.T, ts *testkit.TestServer, action string) []model.AuditEntry {
	t.Helper()
	list, _, err := ts.Server.Store.Audit().List(context.Background(), store.AuditFilter{Action: action}, store.Page{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// wantAudit fails the test unless at least one entry with the action exists.
func wantAudit(t *testing.T, ts *testkit.TestServer, action string) model.AuditEntry {
	t.Helper()
	list := audited(t, ts, action)
	if len(list) == 0 {
		t.Fatalf("nothing in the audit log for %s", action)
	}
	return list[0]
}

// newCodes replaces the account's recovery codes and returns the new ones.
func newCodes(c *testkit.Client) []string {
	out := c.Must(201, "POST", "/api/v1/me/recovery-codes", nil)
	var codes []string
	for _, v := range out["recovery_codes"].([]any) {
		codes = append(codes, v.(string))
	}
	return codes
}

var codeShape = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}(-[0-9A-HJKMNP-TV-Z]{4}){3}$`)

func TestRecoveryCode(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	codes := newCodes(ann)
	if len(codes) != 8 || !codeShape.MatchString(codes[0]) {
		t.Fatalf("recovery codes: %v", codes)
	}
	wantAudit(t, ts, "recovery.codes_replaced")
	stream, _ := ann.Events(nil)

	lost := ts.Browser()
	// A wrong code and an unknown username give the same answer.
	e1 := lost.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/recovery/code", map[string]any{"username": "ann", "code": "0000-0000-0000-0000"})
	e2 := lost.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/recovery/code", map[string]any{"username": "nobody", "code": codes[0]})
	if e1["message"] != e2["message"] {
		t.Fatalf("failures must not say which part was wrong: %q and %q", e1["message"], e2["message"])
	}
	lost.Refused(400, model.CodeInvalidRequest, "POST", "/api/v1/recovery/code", map[string]any{"username": "ann"})

	// Case, spaces and hyphens are ignored, and O is read as 0.
	typed := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(codes[0], "-", " "), "0", "o"))
	in := lost.Must(200, "POST", "/api/v1/recovery/code", map[string]any{"username": " Ann ", "code": typed})
	ses := at(in, "session")
	if ses["recovery"] != true || ses["signed_in_by"] != "recovery_code" || at(in, "account")["username"] != "ann" {
		t.Fatalf("recovery session: %v", in)
	}
	exp, _ := time.Parse(time.RFC3339Nano, ses["expires_at"].(string))
	if d := time.Until(exp); d < 55*time.Minute || d > 61*time.Minute {
		t.Fatalf("a recovery session lasts one hour, got %v", d)
	}
	// The account's other sessions see it start at once.
	if e := stream.Expect("session.created"); at(e, "session")["recovery"] != true {
		t.Fatalf("session.created: %v", e)
	}
	if e := wantAudit(t, ts, "recovery.code_used"); e.Detail["codes_left"] != float64(7) {
		t.Fatalf("audit: %+v", e)
	}

	// A recovery session reads the profile, lists passkeys and sessions,
	// and creates a passkey. Everything else is forbidden.
	lost.Must(200, "GET", "/api/v1/me", nil)
	lost.Must(200, "GET", "/api/v1/me/passkeys", nil)
	lost.Must(200, "GET", "/api/v1/me/sessions", nil)
	lost.Refused(403, model.CodeForbidden, "GET", "/api/v1/conversations", nil)
	lost.Refused(403, model.CodeForbidden, "POST", "/api/v1/me/recovery-codes", nil)
	lost.Refused(403, model.CodeForbidden, "POST", "/api/v1/me/tokens", map[string]any{"label": "x"})
	lost.Refused(403, model.CodeForbidden, "POST", "/api/v1/checks/totp", map[string]any{"code": "123456"})
	lost.Refused(403, model.CodeForbidden, "POST", "/api/v1/me/totp", nil)

	// Creating the passkey replaces the recovery session with an
	// ordinary one, with a new cookie.
	before := lost.Cookie("churr_session", "/")
	begin := lost.Must(200, "POST", "/api/v1/me/passkeys/begin", nil)
	added := lost.Must(201, "POST", "/api/v1/me/passkeys/finish", lost.CreatePasskey(begin))
	now := at(added, "sign_in", "session")
	if now["recovery"] == true || now["signed_in_by"] != "passkey" || at(added, "passkey")["name"] != "Test passkey" {
		t.Fatalf("after the passkey: %v", added)
	}
	if after := lost.Cookie("churr_session", "/"); after == before || after == "" {
		t.Fatal("the recovery token must be replaced")
	}
	if at(added, "sign_in")["recovery_codes"] != nil {
		t.Fatal("an account that has codes keeps them")
	}
	lost.Must(200, "GET", "/api/v1/conversations", nil)
	wantAudit(t, ts, "passkey.added")
	// The new passkey signs in.
	if status, _ := lost.Login(); status != 200 {
		t.Fatalf("signing in with the new passkey: %d", status)
	}

	// A code works once.
	ts.Browser().Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/recovery/code", map[string]any{"username": "ann", "code": codes[0]})
	if left := lost.Must(200, "GET", "/api/v1/me/recovery-codes", nil)["remaining"]; left != float64(7) {
		t.Fatalf("remaining = %v, want 7", left)
	}

	// The lost passkey is removed; the last one cannot be.
	keys := lost.Must(200, "GET", "/api/v1/me/passkeys", nil)["passkeys"].([]any)
	if len(keys) != 2 {
		t.Fatalf("passkeys: %v", keys)
	}
	old, newer := keys[0].(map[string]any)["id"].(string), keys[1].(map[string]any)["id"].(string)
	lost.Must(204, "DELETE", "/api/v1/me/passkeys/"+old, nil)
	lost.Refused(409, model.CodeConflict, "DELETE", "/api/v1/me/passkeys/"+newer, nil)
	wantAudit(t, ts, "passkey.removed")
	if status, _ := ann.Login(); status != 401 {
		t.Fatalf("the removed passkey still signs in: %d", status)
	}
}

func TestRecoveryCodePolicyAndLimits(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	annCodes, ownerCodes := newCodes(ann), newCodes(ts.Owner)

	// Under `sso` a member's recovery code is refused, with the usual
	// answer; the owner's still works, because admins and the owner can
	// always use a passkey.
	ts.Set("auth.member_sign_in", "sso")
	ts.Browser().Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/recovery/code", map[string]any{"username": "ann", "code": annCodes[0]})
	ts.Browser().Must(200, "POST", "/api/v1/recovery/code", map[string]any{"username": "owner", "code": ownerCodes[0]})
	ts.Set("auth.member_sign_in", "passkey")
	// The refused attempt did not use the code up.
	ts.Browser().Must(200, "POST", "/api/v1/recovery/code", map[string]any{"username": "ann", "code": annCodes[0]})

	// Five failures an hour per address; then even a good code waits.
	b := ts.Browser()
	for i := 0; i < 4; i++ { // one failure above already counted
		b.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/recovery/code", map[string]any{"username": "ann", "code": "1111-1111-1111-1111"})
	}
	e := b.Refused(429, model.CodeRateLimited, "POST", "/api/v1/recovery/code", map[string]any{"username": "ann", "code": annCodes[1]})
	if e["retry_after_seconds"] == nil {
		t.Fatalf("rate_limited must say when to try again: %v", e)
	}
}

func TestRecoveryLink(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	admin, other := ts.Admin("adm"), ts.Admin("adm2")
	ann := ts.Member("ann")
	link := func(c *testkit.Client, target model.AccountID) string {
		return "/api/v1/admin/accounts/" + string(target) + "/recovery-link"
	}

	// Who may make one: admins for members, the owner for admins, nobody
	// through the API for the owner.
	ann.Refused(403, model.CodeForbidden, "POST", link(ann, admin.ID), nil)
	admin.Refused(403, model.CodeForbidden, "POST", link(admin, other.ID), nil)
	admin.Refused(403, model.CodeForbidden, "POST", link(admin, owner.ID), nil)
	owner.Refused(403, model.CodeForbidden, "POST", link(owner, owner.ID), nil)
	owner.Refused(404, model.CodeNotFound, "POST", link(owner, model.NewID[model.AccountID]()), nil)
	owner.Must(201, "POST", link(owner, admin.ID), nil)

	// It needs a fresh check.
	admin.Stale()
	admin.Refused(403, model.CodeFreshCheckRequired, "POST", link(admin, ann.ID), nil)
	admin.Check()
	first := admin.Must(201, "POST", link(admin, ann.ID), nil)
	exp, _ := time.Parse(time.RFC3339Nano, first["expires_at"].(string))
	if d := time.Until(exp); d < 23*time.Hour || d > 25*time.Hour {
		t.Fatalf("an admin's link lasts 24 hours, got %v", d)
	}
	if !strings.HasPrefix(first["url"].(string), ts.Origin+"/recover/") {
		t.Fatalf("url: %v", first["url"])
	}
	if e := wantAudit(t, ts, "recovery.link_created"); e.ActorID != string(admin.ID) || e.TargetID != string(ann.ID) {
		t.Fatalf("audit: %+v", e)
	}
	// A personal token cannot make one.
	admin.Token().Refused(403, model.CodeForbidden, "POST", link(admin, ann.ID), nil)

	// Making another withdraws the first.
	second := admin.Must(201, "POST", link(admin, ann.ID), nil)
	value := func(m map[string]any) string { u := m["url"].(string); return u[strings.LastIndex(u, "/")+1:] }
	b := ts.Browser()
	b.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/recovery/link", map[string]any{"token": value(first)})
	b.Refused(400, model.CodeInvalidRequest, "POST", "/api/v1/recovery/link", map[string]any{})
	in := b.Must(200, "POST", "/api/v1/recovery/link", map[string]any{"token": value(second)})
	if s := at(in, "session"); s["recovery"] != true || s["signed_in_by"] != "recovery_link" {
		t.Fatalf("recovery session: %v", in)
	}
	wantAudit(t, ts, "recovery.link_used")
	// Used once.
	ts.Browser().Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/recovery/link", map[string]any{"token": value(second)})
	// It leads to a passkey, as a code does.
	begin := b.Must(200, "POST", "/api/v1/me/passkeys/begin", nil)
	b.Must(201, "POST", "/api/v1/me/passkeys/finish", b.CreatePasskey(begin))
	b.Must(200, "GET", "/api/v1/conversations", nil)

	// Where the role must use company sign-in, the recovery session
	// cannot make a passkey: it can only link an identity.
	third := admin.Must(201, "POST", link(admin, ann.ID), nil)
	ts.Set("auth.member_sign_in", "sso")
	c := ts.Browser()
	c.Must(200, "POST", "/api/v1/recovery/link", map[string]any{"token": value(third)})
	c.Refused(403, model.CodeForbidden, "POST", "/api/v1/me/passkeys/begin", nil)
}

func TestTotp(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	if at(ann.Must(200, "GET", "/api/v1/me/totp", nil), "totp")["enabled"] != false {
		t.Fatal("no authenticator app yet")
	}
	// Setting one up needs a fresh check.
	ann.Stale()
	ann.Refused(403, model.CodeFreshCheckRequired, "POST", "/api/v1/me/totp", nil)
	ann.Check()
	enrol := ann.Must(201, "POST", "/api/v1/me/totp", nil)
	secret := enrol["secret"].(string)
	want := "otpauth://totp/"
	if u := enrol["otpauth_url"].(string); !strings.HasPrefix(u, want) || !strings.Contains(u, "secret="+secret) ||
		!strings.Contains(u, "algorithm=SHA1&digits=6&period=30") || !strings.Contains(u, "ann") {
		t.Fatalf("otpauth_url: %s", u)
	}
	// An unconfirmed app is not a proof.
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/checks/totp", map[string]any{"code": testkit.TOTPCode(secret, 0)})
	ann.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/me/totp/confirm", map[string]any{"code": "000000"})
	ann.Refused(400, model.CodeInvalidRequest, "POST", "/api/v1/me/totp/confirm", map[string]any{})
	// Confirmed with the code of the step before, so the steps after it
	// are still to come in this test.
	done := ann.Must(200, "POST", "/api/v1/me/totp/confirm", map[string]any{"code": testkit.TOTPCode(secret, -1)})
	if at(done, "totp")["enabled"] != true || done["sign_in"] != nil {
		t.Fatalf("confirm: %v", done)
	}
	wantAudit(t, ts, "totp.enrolled")
	ann.Refused(404, model.CodeNotFound, "POST", "/api/v1/me/totp/confirm", map[string]any{"code": testkit.TOTPCode(secret, 0)})
	if me := at(ann.Must(200, "GET", "/api/v1/me", nil), "account"); me["has_totp"] != true {
		t.Fatalf("has_totp: %v", me["has_totp"])
	}

	// A code is a fresh check, and works once: the step it used and
	// every earlier one are dead.
	ann.Stale()
	ann.Refused(403, model.CodeFreshCheckRequired, "POST", "/api/v1/me/recovery-codes", nil)
	ann.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/checks/totp", map[string]any{"code": testkit.TOTPCode(secret, -1)})
	code := testkit.TOTPCode(secret, 0)
	in := ann.Must(200, "POST", "/api/v1/checks/totp", map[string]any{"code": code})
	if at(in, "account")["username"] != "ann" {
		t.Fatalf("check: %v", in)
	}
	ann.Must(201, "POST", "/api/v1/me/recovery-codes", nil)
	ann.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/checks/totp", map[string]any{"code": code})
	ann.Must(200, "POST", "/api/v1/checks/totp", map[string]any{"code": testkit.TOTPCode(secret, 1)})

	// The secret is sealed at rest: the stored bytes are not the secret.
	row, found, err := ts.Server.Store.Identities().GetTotp(context.Background(), ann.ID)
	if err != nil || !found || strings.Contains(string(row.Secret), secret) || len(row.Secret) < 32 {
		t.Fatalf("stored secret: found=%v err=%v", found, err)
	}

	// An admin resets it; a member cannot, and an admin not another admin's.
	admin := ts.Admin("adm")
	ann.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/accounts/"+string(admin.ID)+"/totp", nil)
	admin.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/accounts/"+string(ts.Owner.ID)+"/totp", nil)
	admin.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/admin/accounts/"+string(model.NewID[model.AccountID]())+"/totp", nil)
	admin.Must(204, "DELETE", "/api/v1/admin/accounts/"+string(ann.ID)+"/totp", nil)
	wantAudit(t, ts, "totp.reset")
	if at(ann.Must(200, "GET", "/api/v1/me/totp", nil), "totp")["enabled"] != false {
		t.Fatal("the reset did not remove the app")
	}

	// Removing one's own needs a fresh check.
	secret = ann.Must(201, "POST", "/api/v1/me/totp", nil)["secret"].(string)
	ann.Must(200, "POST", "/api/v1/me/totp/confirm", map[string]any{"code": testkit.TOTPCode(secret, -1)})
	ann.Stale()
	ann.Refused(403, model.CodeFreshCheckRequired, "DELETE", "/api/v1/me/totp", nil)
	ann.Check()
	ann.Must(204, "DELETE", "/api/v1/me/totp", nil)
	wantAudit(t, ts, "totp.removed")

	// Five wrong codes in 15 minutes per account, then refusals.
	secret = ann.Must(201, "POST", "/api/v1/me/totp", nil)["secret"].(string)
	ann.Must(200, "POST", "/api/v1/me/totp/confirm", map[string]any{"code": testkit.TOTPCode(secret, -1)})
	for i := 0; i < 2; i++ { // three wrong codes were given above
		ann.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/checks/totp", map[string]any{"code": "999999"})
	}
	ann.Refused(429, model.CodeRateLimited, "POST", "/api/v1/checks/totp", map[string]any{"code": testkit.TOTPCode(secret, 0)})

	// A personal token has nothing to do with the authenticator app
	// (docs/PERMISSIONS.md section 17, case 96 and decision P1).
	ann.Check()
	pat := ann.Token()
	pat.Refused(403, model.CodeForbidden, "GET", "/api/v1/me/totp", nil)
	pat.Refused(403, model.CodeForbidden, "POST", "/api/v1/me/passkeys/begin", nil)
	pat.Refused(403, model.CodeForbidden, "POST", "/api/v1/checks/passkey/begin", nil)
}

func TestPasskeysAndFreshCheck(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	keys := ann.Must(200, "GET", "/api/v1/me/passkeys", nil)["passkeys"].([]any)
	first := keys[0].(map[string]any)
	if len(keys) != 1 || first["has_prf"] != true || first["can_unlock"] != false {
		t.Fatalf("passkeys: %v", keys)
	}
	id := first["id"].(string)

	// Rename, and note that a later use gave no PRF value.
	got := at(ann.Must(200, "PATCH", "/api/v1/me/passkeys/"+id, map[string]any{"name": " Laptop ", "has_prf": false}), "passkey")
	if got["name"] != "Laptop" || got["has_prf"] != false {
		t.Fatalf("renamed: %v", got)
	}
	ann.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/me/passkeys/"+id, map[string]any{"has_prf": true})
	ann.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/me/passkeys/"+id, map[string]any{"name": "  "})
	// Somebody else's passkey could not be known to exist.
	bob.Refused(404, model.CodeNotFound, "PATCH", "/api/v1/me/passkeys/"+id, map[string]any{"name": "x"})
	bob.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/passkeys/"+id, nil)
	ann.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/passkeys/not-an-id", nil)

	// Adding needs a fresh check; the check is a passkey assertion
	// limited to the account's own passkeys.
	ann.Stale()
	ann.Refused(403, model.CodeFreshCheckRequired, "POST", "/api/v1/me/passkeys/begin", nil)
	ann.Refused(403, model.CodeFreshCheckRequired, "DELETE", "/api/v1/me/passkeys/"+id, nil)
	begin := ann.Must(200, "POST", "/api/v1/checks/passkey/begin", nil)
	allow := at(begin, "public_key")["allowCredentials"].([]any)
	if len(allow) != 1 || at(begin, "public_key")["userVerification"] != "required" {
		t.Fatalf("the check must be limited to the account's own passkeys with user verification: %v", begin)
	}
	// Another account's passkey does not answer it, and a ceremony works once.
	stolen := bob.Assert(begin)
	ann.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/checks/passkey/finish", stolen)
	ann.Refused(404, model.CodeNotFound, "POST", "/api/v1/checks/passkey/finish", ann.Assert(begin))
	// Bob cannot finish a check Ann began.
	begin = ann.Must(200, "POST", "/api/v1/checks/passkey/begin", nil)
	bob.Refused(404, model.CodeNotFound, "POST", "/api/v1/checks/passkey/finish", bob.Assert(begin))
	in := ann.Check()
	if at(in, "account")["username"] != "ann" {
		t.Fatalf("check: %v", in)
	}

	begin = ann.Must(200, "POST", "/api/v1/me/passkeys/begin", nil)
	if ex := at(begin, "public_key")["excludeCredentials"].([]any); len(ex) != 1 {
		t.Fatalf("the account's passkeys are excluded from a new creation: %v", begin)
	}
	if at(begin, "public_key", "extensions")["prf"] == nil {
		t.Fatalf("PRF is asked for on every creation: %v", begin)
	}
	body := ann.CreatePasskey(begin)
	bob.Refused(404, model.CodeNotFound, "POST", "/api/v1/me/passkeys/finish", body)
	begin = ann.Must(200, "POST", "/api/v1/me/passkeys/begin", nil)
	added := ann.Must(201, "POST", "/api/v1/me/passkeys/finish", ann.CreatePasskey(begin))
	if added["sign_in"] != nil {
		t.Fatalf("adding a passkey to a signed-in session changes no session: %v", added)
	}
	if n := len(ann.Must(200, "GET", "/api/v1/me/passkeys", nil)["passkeys"].([]any)); n != 2 {
		t.Fatalf("%d passkeys, want 2", n)
	}
	ann.Must(204, "DELETE", "/api/v1/me/passkeys/"+id, nil)
}

func TestSessionsAndTokens(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	list := ann.Must(200, "GET", "/api/v1/me/sessions", nil)["sessions"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["current"] != true || list[0].(map[string]any)["kind"] != "web" {
		t.Fatalf("sessions: %v", list)
	}
	if _, has := list[0].(map[string]any)["token_hash"]; has {
		t.Fatal("token hashes are never returned")
	}
	first := ann.SessionID()

	// Sign in again elsewhere: a new row and a new token.
	laptop := ts.Browser()
	clone(laptop, ann)
	if status, _ := laptop.Login(); status != 200 {
		t.Fatalf("second sign-in: %d", status)
	}
	stream, _ := laptop.Events(nil)
	pat := ann.Token()
	tokens := ann.Must(200, "GET", "/api/v1/me/tokens", nil)["tokens"].([]any)
	if len(tokens) != 1 || tokens[0].(map[string]any)["label"] != "test" {
		t.Fatalf("tokens: %v", tokens)
	}
	tokenID := tokens[0].(map[string]any)["id"].(string)
	if n := len(ann.Must(200, "GET", "/api/v1/me/sessions", nil)["sessions"].([]any)); n != 3 {
		t.Fatalf("%d sessions, want 3 (two browsers and a token)", n)
	}
	// A personal token cannot look at sessions or tokens.
	pat.Refused(403, model.CodeForbidden, "GET", "/api/v1/me/sessions", nil)
	pat.Refused(403, model.CodeForbidden, "GET", "/api/v1/me/tokens", nil)
	pat.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/me/tokens/"+tokenID, nil)
	pat.Must(200, "GET", "/api/v1/me", nil)

	// Somebody else's session is not found.
	bob := ts.Member("bob")
	bob.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/sessions/"+string(first), nil)
	bob.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/tokens/"+tokenID, nil)
	// A browser session is not a token.
	ann.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/tokens/"+string(first), nil)

	// Ending a session closes its live connection at once.
	ann.Must(204, "DELETE", "/api/v1/me/sessions/"+string(laptop.SessionID()), nil)
	if code := stream.CloseCode(5 * time.Second); code != 4001 {
		t.Fatalf("the ended session's connection closed with %d, want 4001", code)
	}
	laptop.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)

	ann.Must(204, "DELETE", "/api/v1/me/tokens/"+tokenID, nil)
	pat.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	wantAudit(t, ts, "token.revoked")
	ann.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/tokens/"+tokenID, nil)

	// End every session but this one.
	if status, _ := laptop.Login(); status != 200 {
		t.Fatal("sign in again")
	}
	pat = ann.Token()
	ann.Must(204, "DELETE", "/api/v1/me/sessions", nil)
	laptop.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	pat.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	ann.Must(200, "GET", "/api/v1/me", nil)
}

// clone gives a new browser the passkey of another, as a synced passkey is
// on two devices.
func clone(to, from *testkit.Client) { to.CopyPasskey(from) }

func TestAdminSignOut(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	admin, other := ts.Admin("adm"), ts.Admin("adm2")
	ann, bob := ts.Member("ann"), ts.Member("bob")
	annPat := ann.Token()
	out := func(id model.AccountID) string { return "/api/v1/admin/accounts/" + string(id) + "/sessions" }

	// Admins sign members out; only the owner signs an admin out.
	ann.Refused(403, model.CodeForbidden, "DELETE", out(bob.ID), nil)
	admin.Refused(403, model.CodeForbidden, "DELETE", out(other.ID), nil)
	admin.Refused(403, model.CodeForbidden, "DELETE", out(owner.ID), nil)
	admin.Refused(404, model.CodeNotFound, "DELETE", out(model.NewID[model.AccountID]()), nil)
	stream, _ := ann.Events(nil)
	admin.Must(204, "DELETE", out(ann.ID), nil)
	if code := stream.CloseCode(5 * time.Second); code != 4001 {
		t.Fatalf("close code %d, want 4001", code)
	}
	// Every session and token of the account.
	ann.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	annPat.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	if e := wantAudit(t, ts, "sessions.revoked_all"); e.TargetID != string(ann.ID) || e.ActorID != string(admin.ID) {
		t.Fatalf("audit: %+v", e)
	}
	owner.Must(204, "DELETE", out(other.ID), nil)
	other.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)

	// Sign everyone out: needs a fresh check, ends every browser and app
	// session but the caller's own, and leaves personal tokens alone.
	bobPat := bob.Token()
	bob.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/sessions/sign-out-all", nil)
	admin.Stale()
	admin.Refused(403, model.CodeFreshCheckRequired, "POST", "/api/v1/admin/sessions/sign-out-all", nil)
	admin.Check()
	admin.Must(204, "POST", "/api/v1/admin/sessions/sign-out-all", nil)
	bob.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	owner.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	admin.Must(200, "GET", "/api/v1/me", nil)
	bobPat.Must(200, "GET", "/api/v1/me", nil)
	if e := wantAudit(t, ts, "sessions.revoked_all"); e.TargetID != "everyone" {
		t.Fatalf("audit: %+v", e)
	}
}

// Ruling R42: where company sign-in is required for the account's role, its
// personal tokens stop working 30 days after its last company sign-in.
func TestPersonalTokenUnderRequiredSso(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	pat, ownerPat := ann.Token(), ts.Owner.Token()
	pat.Must(200, "GET", "/api/v1/me", nil)
	ts.Set("auth.member_sign_in", "sso")
	// Never signed in through a provider: paused.
	pat.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	// The owner's role does not have to use company sign-in.
	ownerPat.Must(200, "GET", "/api/v1/me", nil)

	ctx := context.Background()
	p := model.SsoProvider{Kind: model.ProviderOIDC, Name: "Acme", Config: []byte(`{}`), Enabled: true}
	if err := ts.Server.Store.Identities().CreateProvider(ctx, &p); err != nil {
		t.Fatal(err)
	}
	link := func(last time.Duration) {
		ts.Exec(`DELETE FROM identities WHERE account_id = ?`, string(ann.ID))
		err := ts.Server.Store.Identities().Link(ctx, model.Identity{ProviderID: p.ID, Subject: "ann", AccountID: ann.ID, LastUsedAt: model.Now().Add(-last)})
		if err != nil {
			t.Fatal(err)
		}
	}
	link(29 * 24 * time.Hour)
	pat.Must(200, "GET", "/api/v1/me", nil)
	link(31 * 24 * time.Hour)
	pat.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	// It works again once the person has signed in.
	link(time.Minute)
	pat.Must(200, "GET", "/api/v1/me", nil)
	// And under any other policy the rule does not apply.
	link(31 * 24 * time.Hour)
	ts.Set("auth.member_sign_in", "either")
	pat.Must(200, "GET", "/api/v1/me", nil)
}

// `churr recovery <username>` makes a one-hour link for any local person,
// the owner included, with the command line as the actor.
func TestCommandLineRecoveryLink(t *testing.T) {
	ts := testkit.NewServer(t)
	ctx := context.Background()
	if _, err := auth.CLIRecoveryLink(ctx, ts.Server.Store, ts.Origin, "nobody"); model.CodeOf(err) != model.CodeNotFound {
		t.Fatalf("an unknown username: %v", err)
	}
	link, err := auth.CLIRecoveryLink(ctx, ts.Server.Store, ts.Origin, " Owner ")
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(link.ExpiresAt.Time()); d > time.Hour || d < 55*time.Minute {
		t.Fatalf("a command-line link lasts one hour, got %v", d)
	}
	if e := wantAudit(t, ts, "recovery.link_created"); e.ActorKind != "cli" || e.ActorID != "" || e.TargetID != string(ts.Owner.ID) {
		t.Fatalf("audit: %+v", e)
	}
	b := ts.Browser()
	in := b.Must(200, "POST", "/api/v1/recovery/link", map[string]any{"token": link.URL[strings.LastIndex(link.URL, "/")+1:]})
	if at(in, "account")["role"] != "owner" || at(in, "session")["recovery"] != true {
		t.Fatalf("recovery session: %v", in)
	}
}
