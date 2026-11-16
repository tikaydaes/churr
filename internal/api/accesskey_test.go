package api_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Access keys (ruling R80, docs/AUTH.md section 5.6): accounts that sign in
// with a long random code instead of a passkey.

func TestAccessKeyRegisterAndSignIn(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.MemberWithAccessKey("ann")
	me := signedInAs(t, ann, "ann")
	if me["has_access_key"] != true || me["passkey_count"] != float64(0) {
		t.Fatalf("an access key account: %v", me)
	}
	if s := at(ann.Must(200, "GET", "/api/v1/me/sessions", nil)); s == nil {
		t.Fatal("no sessions")
	}
	var by string
	for _, s := range ann.Must(200, "GET", "/api/v1/me/sessions", nil)["sessions"].([]any) {
		if m := s.(map[string]any); m["current"] == true {
			by, _ = m["signed_in_by"].(string)
		}
	}
	if by != "access_key" {
		t.Fatalf("signed_in_by = %q", by)
	}
	if e := wantAudit(t, ts, "account.registered"); e.Detail["how"] != "access_key" {
		t.Fatalf("audit: %+v", e)
	}
	// No recovery codes come with an access key.
	if left := ann.Must(200, "GET", "/api/v1/me/recovery-codes", nil)["remaining"]; left != float64(0) {
		t.Fatalf("recovery codes: %v", left)
	}

	ann.Must(204, "POST", "/api/v1/logout", nil)
	ann.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	if st, out := ann.AccessKeyLogin("ANN "); st != 200 || at(out, "account")["username"] != "ann" {
		t.Fatalf("sign in with the access key: %d %v", st, out)
	}
	signedInAs(t, ann, "ann")

	// A wrong key and an unknown username get the same answer.
	b := ts.Browser()
	wrong := testkit.AccessKeyAuth(secret.NewAccessKey())
	e1 := b.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/access-key/login", map[string]any{"username": "ann", "access_key_auth": wrong})
	e2 := b.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/access-key/login", map[string]any{"username": "nobody", "access_key_auth": ann.AccessKey()})
	if fmt.Sprint(e1) != fmt.Sprint(e2) {
		t.Fatalf("the answers differ: %v and %v", e1, e2)
	}
	// Another account's key does not sign in as ann.
	bob := ts.MemberWithAccessKey("bob")
	b.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/access-key/login", map[string]any{"username": "ann", "access_key_auth": bob.AccessKey()})
	// A passkey account has no access key to match.
	b.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/access-key/login", map[string]any{"username": ts.Owner.Username, "access_key_auth": wrong})
	// The value must be 32 bytes.
	b.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/access-key/login", map[string]any{"username": "ann", "access_key_auth": "c2hvcnQ"})
	b.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/access-key/login", map[string]any{"username": "ann", "access_key_auth": ann.AccessKeyText()})

	// A removed account cannot sign in.
	ts.Owner.Check()
	ts.Owner.Must(204, "DELETE", "/api/v1/admin/accounts/"+string(bob.ID), map[string]any{"reason": "test"})
	if st, _ := bob.AccessKeyLogin("bob"); st != 401 {
		t.Fatalf("a removed account signed in: %d", st)
	}
}

func TestAccessKeySetupOpenAndClaim(t *testing.T) {
	ts := testkit.NewServer(t, testkit.Options{NoOwner: true})
	owner := ts.Browser()
	owner.Refused(404, model.CodeNotFound, "POST", "/api/v1/setup/access-key", map[string]any{"setup_token": "wrong", "username": "olga", "display_name": "Olga", "access_key_auth": owner.NewAccessKey()})
	out := owner.Must(201, "POST", "/api/v1/setup/access-key", map[string]any{"setup_token": ts.SetupToken(), "username": "olga", "display_name": "Olga", "access_key_auth": owner.AccessKey()})
	if at(out, "account")["role"] != "owner" || out["recovery_codes"] != nil {
		t.Fatalf("setup: %v", out)
	}
	if e := wantAudit(t, ts, "owner.created"); e.Detail["how"] != "access_key" {
		t.Fatalf("audit: %+v", e)
	}
	// The setup link is spent and #general exists.
	ts.Browser().Refused(404, model.CodeNotFound, "POST", "/api/v1/setup/access-key", map[string]any{"setup_token": ts.SetupToken(), "username": "eve", "display_name": "Eve", "access_key_auth": owner.AccessKey()})
	if convs := owner.Must(200, "GET", "/api/v1/conversations", nil); !strings.Contains(fmt.Sprint(convs), "general") {
		t.Fatalf("no #general: %v", convs)
	}
	owner.Must(204, "POST", "/api/v1/logout", nil)
	if st, _ := owner.AccessKeyLogin("olga"); st != 200 {
		t.Fatalf("the owner signs in with the key: %d", st)
	}

	// Open registration needs no invite.
	c := ts.Browser()
	c.Refused(403, model.CodeForbidden, "POST", "/api/v1/access-key/register", map[string]any{"username": "pia", "display_name": "Pia", "access_key_auth": c.NewAccessKey()})
	owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"registration.mode": "open"})
	c.Must(201, "POST", "/api/v1/access-key/register", map[string]any{"username": "pia", "display_name": "Pia", "access_key_auth": c.AccessKey()})
	signedInAs(t, c, "pia")
	// A taken name is refused, and nothing is made.
	d := ts.Browser()
	d.Refused(409, model.CodeNameTaken, "POST", "/api/v1/access-key/register", map[string]any{"username": "pia", "display_name": "Pia", "access_key_auth": d.NewAccessKey()})

	// A claim link takes over an imported account with an access key.
	imported := ts.Imported("ivy")
	code := obj(owner.Must(201, "POST", "/api/v1/admin/accounts/"+string(imported)+"/claim-link", nil), "invite")["code"].(string)
	owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"registration.mode": "closed"})
	ivy := ts.Browser()
	ivy.Must(201, "POST", "/api/v1/access-key/register", map[string]any{"invite_code": code, "access_key_auth": ivy.NewAccessKey()})
	if me := signedInAs(t, ivy, "ivy"); me["id"] != string(imported) || me["has_access_key"] != true {
		t.Fatalf("claimed: %v", me)
	}
	if e := wantAudit(t, ts, "account.claimed"); e.Detail["how"] != "access_key" {
		t.Fatalf("audit: %+v", e)
	}
}

func TestAccessKeyManagement(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.MemberWithAccessKey("ann")
	st := ann.Must(200, "GET", "/api/v1/me/access-key", nil)
	if st["access_key"] == nil || st["may_create"] != true {
		t.Fatalf("state: %v", st)
	}
	// A personal token cannot see or touch it.
	ann.Token().Refused(403, model.CodeForbidden, "GET", "/api/v1/me/access-key", nil)

	// Making a new one needs a fresh check, which the key itself gives.
	ann.Stale()
	old := ann.AccessKey()
	ann.Refused(403, model.CodeFreshCheckRequired, "PUT", "/api/v1/me/access-key", map[string]any{"access_key_auth": ann.NewAccessKey()})
	ann.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/checks/access-key", map[string]any{"access_key_auth": ann.AccessKey()})
	ann.Must(200, "POST", "/api/v1/checks/access-key", map[string]any{"access_key_auth": old})
	ann.Must(200, "PUT", "/api/v1/me/access-key", map[string]any{"access_key_auth": ann.AccessKey()})
	wantAudit(t, ts, "access_key.replaced")
	// The old one stops working; the new one works.
	b := ts.Browser()
	b.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/access-key/login", map[string]any{"username": "ann", "access_key_auth": old})
	b.Must(200, "POST", "/api/v1/access-key/login", map[string]any{"username": "ann", "access_key_auth": ann.AccessKey()})

	// The last way in cannot be removed.
	ann.Refused(409, model.CodeConflict, "DELETE", "/api/v1/me/access-key", nil)
	// A passkey can be added to an access key account, and then the key
	// can go.
	begin := ann.Must(200, "POST", "/api/v1/me/passkeys/begin", nil)
	added := ann.Must(201, "POST", "/api/v1/me/passkeys/finish", ann.CreatePasskey(begin))
	if codes := at(added, "sign_in")["recovery_codes"]; codes == nil {
		t.Fatalf("the first passkey makes recovery codes: %v", added)
	}
	if st, _ := ann.Login(); st != 200 {
		t.Fatalf("the new passkey signs in: %d", st)
	}
	ann.Must(204, "DELETE", "/api/v1/me/access-key", nil)
	wantAudit(t, ts, "access_key.removed")
	ann.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/access-key", nil)
	b.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/access-key/login", map[string]any{"username": "ann", "access_key_auth": ann.AccessKey()})
	// Now the passkey is the last way in.
	pk := ann.Must(200, "GET", "/api/v1/me/passkeys", nil)["passkeys"].([]any)[0].(map[string]any)
	ann.Refused(409, model.CodeConflict, "DELETE", "/api/v1/me/passkeys/"+pk["id"].(string), nil)
	// With an access key again, the last passkey may go.
	ann.Must(200, "PUT", "/api/v1/me/access-key", map[string]any{"access_key_auth": ann.NewAccessKey()})
	wantAudit(t, ts, "access_key.created")
	ann.Must(204, "DELETE", "/api/v1/me/passkeys/"+pk["id"].(string), nil)
	if me := signedInAs(t, ann, "ann"); me["passkey_count"] != float64(0) || me["has_access_key"] != true {
		t.Fatalf("after: %v", me)
	}

	// A check with an access key is not a second factor for a pending
	// session, and an account with no key cannot use it.
	ts.Owner.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/checks/access-key", map[string]any{"access_key_auth": ann.AccessKey()})
}

func TestAccessKeySettingOff(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.MemberWithAccessKey("ann")
	owner := ts.Owner
	owner.Check()
	owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"auth.allow_access_keys": false})
	if e := wantAudit(t, ts, "setting.changed"); e.TargetID != "auth.allow_access_keys" {
		t.Fatalf("audit: %+v", e)
	}
	if signIn := at(owner.Must(200, "GET", "/api/v1/server", nil), "server", "sign_in"); signIn["access_keys"] != false {
		t.Fatalf("server: %v", signIn)
	}
	// No new account gets one.
	c := ts.Browser()
	c.Refused(403, model.CodeForbidden, "POST", "/api/v1/access-key/register", map[string]any{"invite_code": inviteCode(ts), "username": "cy", "display_name": "Cy", "access_key_auth": c.NewAccessKey()})
	// A passkey account cannot make a first one.
	bob := ts.Member("bob")
	bob.Check()
	if st := bob.Must(200, "GET", "/api/v1/me/access-key", nil); st["may_create"] != false {
		t.Fatalf("state: %v", st)
	}
	bob.Refused(403, model.CodeForbidden, "PUT", "/api/v1/me/access-key", map[string]any{"access_key_auth": bob.NewAccessKey()})
	// An existing one keeps working, and can be replaced.
	ann.Must(204, "POST", "/api/v1/logout", nil)
	if st, _ := ann.AccessKeyLogin("ann"); st != 200 {
		t.Fatalf("an existing key stops working: %d", st)
	}
	if st := ann.Must(200, "GET", "/api/v1/me/access-key", nil); st["may_create"] != true {
		t.Fatalf("state: %v", st)
	}
	ann.Must(200, "PUT", "/api/v1/me/access-key", map[string]any{"access_key_auth": ann.NewAccessKey()})
	// Admins see who has no passkey.
	list := arr(owner.Must(200, "GET", "/api/v1/admin/accounts", nil), "accounts")
	found := false
	for _, a := range list {
		m := a.(map[string]any)
		if m["username"] == "ann" {
			found = m["has_access_key"] == true && m["passkey_count"] == float64(0)
		}
	}
	if !found {
		t.Fatalf("the admin list does not show ann without a passkey: %v", list)
	}
}

func inviteCode(ts *testkit.TestServer) string {
	out := ts.Owner.Must(201, "POST", "/api/v1/invites", map[string]any{})
	return out["invite"].(map[string]any)["code"].(string)
}

func TestAccessKeyLimits(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.MemberWithAccessKey("ann")
	ann.Must(204, "POST", "/api/v1/logout", nil)
	wrong := testkit.AccessKeyAuth(secret.NewAccessKey())

	// Per address: ten failures, then even the right key waits.
	b := ts.Browser()
	for i := 0; i < 10; i++ {
		b.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/access-key/login", map[string]any{"username": "nobody", "access_key_auth": wrong})
	}
	b.Refused(429, model.CodeRateLimited, "POST", "/api/v1/access-key/login", map[string]any{"username": "ann", "access_key_auth": ann.AccessKey()})

	// Per account: thirty failures from many addresses, then the account
	// waits whatever the address, and the moment is audited once.
	ctx := context.Background()
	for i := 0; i < 30; i++ {
		_, err := ts.Server.Auth.SignInWithAccessKey(ctx, fmt.Sprintf("10.0.%d.1", i), "ann", wrong, "test")
		if e, ok := model.AsError(err); !ok || e.Code != model.CodeUnauthenticated {
			t.Fatalf("try %d: %v", i, err)
		}
	}
	_, err := ts.Server.Auth.SignInWithAccessKey(ctx, "10.1.0.1", "ann", ann.AccessKey(), "test")
	if e, ok := model.AsError(err); !ok || e.Code != model.CodeRateLimited {
		t.Fatalf("after thirty failures: %v", err)
	}
	if n := len(audited(t, ts, "access_key.limit_reached")); n != 1 {
		t.Fatalf("limit_reached written %d times", n)
	}
	// Other accounts are not held up.
	bob := ts.MemberWithAccessKey("bob")
	if _, err := ts.Server.Auth.SignInWithAccessKey(ctx, "10.1.0.2", "bob", bob.AccessKey(), "test"); err != nil {
		t.Fatalf("bob: %v", err)
	}

	// The fresh check: five wrong keys for the account in fifteen minutes.
	for i := 0; i < 5; i++ {
		bob.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/checks/access-key", map[string]any{"access_key_auth": wrong})
	}
	bob.Refused(429, model.CodeRateLimited, "POST", "/api/v1/checks/access-key", map[string]any{"access_key_auth": bob.AccessKey()})
}

func TestAccessKeyUnlock(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.MemberWithAccessKey("ann"), ts.MemberWithAccessKey("bob")
	ann.Must(204, "POST", "/api/v1/session/lock", nil)
	begin := ann.Must(200, "POST", "/api/v1/session/unlock/begin", nil)
	if begin["public_key"] != nil || begin["access_key"] != true {
		t.Fatalf("an account with only an access key: %v", begin)
	}
	id := begin["ceremony_id"].(string)
	// A passkey answer cannot unlock a ceremony with no passkey in it.
	ann.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/session/unlock/finish", map[string]any{"ceremony_id": id, "credential": map[string]any{"id": "x"}})
	begin = ann.Must(200, "POST", "/api/v1/session/unlock/begin", nil)
	id = begin["ceremony_id"].(string)
	// Another account's key does not unlock, and the ceremony stays for a
	// second try.
	ann.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/session/unlock/access-key", map[string]any{"ceremony_id": id, "access_key_auth": bob.AccessKey()})
	ann.Refused(404, model.CodeNotFound, "POST", "/api/v1/session/unlock/access-key", map[string]any{"ceremony_id": "made-up", "access_key_auth": ann.AccessKey()})
	ann.Must(204, "POST", "/api/v1/session/unlock/access-key", map[string]any{"ceremony_id": id, "access_key_auth": ann.AccessKey()})
	signedInAs(t, ann, "ann")
	// Used once.
	ann.Refused(404, model.CodeNotFound, "POST", "/api/v1/session/unlock/access-key", map[string]any{"ceremony_id": id, "access_key_auth": ann.AccessKey()})
	// The unlock counts as a fresh check.
	ann.Must(201, "POST", "/api/v1/me/tokens", map[string]any{"label": "after unlock"})

	// The page an app opens learns from the options that the key works.
	ann.Must(204, "POST", "/api/v1/session/lock", nil)
	begin = ann.Must(200, "POST", "/api/v1/session/unlock/begin", nil)
	page := ts.Browser()
	opts := page.Must(200, "POST", "/api/v1/session/unlock/options", map[string]any{"ceremony_id": begin["ceremony_id"]})
	if opts["access_key"] != true || opts["public_key"] != nil {
		t.Fatalf("options: %v", opts)
	}
	// Wrong keys count against the account with wrong assertions: two so
	// far, three more make five, then unlocking waits.
	wrong := testkit.AccessKeyAuth(secret.NewAccessKey())
	for i := 0; i < 3; i++ {
		page.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/session/unlock/access-key", map[string]any{"ceremony_id": begin["ceremony_id"], "access_key_auth": wrong})
	}
	page.Refused(429, model.CodeRateLimited, "POST", "/api/v1/session/unlock/access-key", map[string]any{"ceremony_id": begin["ceremony_id"], "access_key_auth": ann.AccessKey()})
	ann.Refused(429, model.CodeRateLimited, "POST", "/api/v1/session/unlock/begin", nil)

	// An account with both offers both.
	cat := ts.Member("cat")
	cat.Check()
	cat.Must(200, "PUT", "/api/v1/me/access-key", map[string]any{"access_key_auth": cat.NewAccessKey()})
	cat.Must(204, "POST", "/api/v1/session/lock", nil)
	begin = cat.Must(200, "POST", "/api/v1/session/unlock/begin", nil)
	if begin["public_key"] == nil || begin["access_key"] != true {
		t.Fatalf("both: %v", begin)
	}
	cat.Must(204, "POST", "/api/v1/session/unlock/access-key", map[string]any{"ceremony_id": begin["ceremony_id"], "access_key_auth": cat.AccessKey()})
	signedInAs(t, cat, "cat")
}

func TestAccessKeyRecovery(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.MemberWithAccessKey("ann")
	ts.Owner.Check()
	link := ts.Owner.Must(201, "POST", "/api/v1/admin/accounts/"+string(ann.ID)+"/recovery-link", nil)
	u := link["url"].(string)
	b := ts.Browser()
	b.Must(200, "POST", "/api/v1/recovery/link", map[string]any{"token": u[strings.LastIndex(u, "/")+1:]})
	if st := b.Must(200, "GET", "/api/v1/me/access-key", nil); st["access_key"] == nil {
		t.Fatalf("a recovery session sees the key exists: %v", st)
	}
	// A recovery session can make a new access key with no fresh check;
	// it is then replaced by an ordinary session.
	out := b.Must(200, "PUT", "/api/v1/me/access-key", map[string]any{"access_key_auth": b.NewAccessKey()})
	if s := at(out, "sign_in", "session"); s["recovery"] == true || s["signed_in_by"] != "access_key" {
		t.Fatalf("after: %v", out)
	}
	b.Must(200, "GET", "/api/v1/conversations", nil)
	// The old key is dead; the new one signs in.
	ts.Browser().Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/access-key/login", map[string]any{"username": "ann", "access_key_auth": ann.AccessKey()})
	ts.Browser().Must(200, "POST", "/api/v1/access-key/login", map[string]any{"username": "ann", "access_key_auth": b.AccessKey()})
}

func TestAccessKeyUnderCompanySignIn(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.MemberWithAccessKey("ann")
	ann.Must(204, "POST", "/api/v1/logout", nil)
	ts.Set("auth.member_sign_in", "sso")
	// A member must use company sign-in, as with a passkey.
	if st, _ := ann.AccessKeyLogin("ann"); st != 403 {
		t.Fatalf("a member under sso: %d", st)
	}
	ts.Browser().Refused(403, model.CodeForbidden, "POST", "/api/v1/access-key/register", map[string]any{"invite_code": inviteCode(ts), "username": "cy", "display_name": "Cy", "access_key_auth": testkit.AccessKeyAuth(secret.NewAccessKey())})
	// An admin may always sign in by themselves.
	adm := ts.Browser()
	ts.Set("auth.member_sign_in", "passkey")
	adm.RegisterWithAccessKey("adm", inviteCode(ts))
	if _, _, err := ts.Server.Store.Accounts().SetRole(context.Background(), adm.ID, model.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	ts.Set("auth.member_sign_in", "sso")
	adm.Must(204, "POST", "/api/v1/logout", nil)
	if st, _ := adm.AccessKeyLogin("adm"); st != 200 {
		t.Fatalf("an admin under sso: %d", st)
	}
}

// Every action that needs a fresh check takes the access key as the check
// for an account with no passkey (docs/AUTH.md section 10.4).
func TestAccessKeyGivesTheFreshCheck(t *testing.T) {
	ts := testkit.NewServer(t)
	adm := ts.Browser()
	adm.RegisterWithAccessKey("adm", inviteCode(ts))
	if _, _, err := ts.Server.Store.Accounts().SetRole(context.Background(), adm.ID, model.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	ann := ts.Member("ann")
	check := func() {
		t.Helper()
		adm.Stale()
		out := adm.Must(200, "POST", "/api/v1/checks/access-key", map[string]any{"access_key_auth": adm.AccessKey()})
		if s := at(out, "session"); s["pending_second_factor"] == true {
			t.Fatalf("check: %v", out)
		}
	}
	for _, step := range []struct {
		name, method, path string
		body               any
		status             int
	}{
		{"a personal token", "POST", "/api/v1/me/tokens", map[string]any{"label": "t"}, 201},
		{"adding a passkey", "POST", "/api/v1/me/passkeys/begin", nil, 200},
		{"recovery codes", "POST", "/api/v1/me/recovery-codes", nil, 201},
		{"an authenticator app", "POST", "/api/v1/me/totp", nil, 201},
		{"a new access key", "PUT", "/api/v1/me/access-key", map[string]any{"access_key_auth": adm.AccessKey()}, 200},
		{"sign-in settings", "PATCH", "/api/v1/admin/settings", map[string]any{"auth.allow_access_keys": true}, 200},
		{"a recovery link", "POST", "/api/v1/admin/accounts/" + string(ann.ID) + "/recovery-link", nil, 201},
		{"pairing a device", "POST", "/oauth/pair", nil, 201},
	} {
		adm.Stale()
		adm.Refused(403, model.CodeFreshCheckRequired, step.method, step.path, step.body)
		check()
		if got, out := adm.Do(step.method, step.path, step.body); got != step.status {
			t.Fatalf("%s after the access key check: %d %v", step.name, got, out)
		}
	}
}
