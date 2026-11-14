package api_test

import (
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// TestSetupAndSignIn covers first-run setup, registration by invite,
// sign-in, sessions by cookie and by bearer token, the origin check and
// sign-out.
func TestSetupAndSignIn(t *testing.T) {
	ts := testkit.NewServer(t, testkit.Options{NoOwner: true})
	anon := ts.Browser()

	// Until the owner exists only the server's description and setup work.
	out := anon.Must(200, "GET", "/api/v1/server", nil)
	if out["server"].(map[string]any)["setup_needed"] != true {
		t.Fatalf("setup_needed should be true before setup: %v", out)
	}
	anon.Refused(503, model.CodeUnavailable, "POST", "/api/v1/passkey/login/begin", nil)
	anon.Refused(404, model.CodeNotFound, "POST", "/api/v1/setup/begin", map[string]any{"setup_token": "wrong", "username": "eve", "display_name": "Eve"})
	anon.Refused(400, model.CodeInvalidRequest, "POST", "/api/v1/setup/begin", map[string]any{"setup_token": "x", "username": "eve", "display_name": "Eve", "surprise": 1})
	anon.Refused(409, model.CodeNameTaken, "POST", "/api/v1/setup/begin", map[string]any{"setup_token": ts.SetupToken(), "username": "everyone", "display_name": "E"})

	owner := ts.Browser()
	done := owner.Setup("kaity")
	if n := len(done["recovery_codes"].([]any)); n != 8 {
		t.Fatalf("got %d recovery codes, want 8", n)
	}
	acc := done["account"].(map[string]any)
	if acc["role"] != "owner" || acc["passkey_count"] != float64(1) {
		t.Fatalf("owner account: %v", acc)
	}
	ts.Owner = owner
	// The setup link is spent.
	anon.Refused(404, model.CodeNotFound, "POST", "/api/v1/setup/begin", map[string]any{"setup_token": ts.SetupToken(), "username": "eve", "display_name": "Eve"})
	out = anon.Must(200, "GET", "/api/v1/server", nil)
	if out["server"].(map[string]any)["setup_needed"] != false {
		t.Fatal("setup_needed should be false after setup")
	}

	// Setup made #general with @everyone in it.
	convs := owner.Must(200, "GET", "/api/v1/conversations", nil)["conversations"].([]any)
	if len(convs) != 1 || convs[0].(map[string]any)["name"] != "general" {
		t.Fatalf("the owner should be in #general: %v", convs)
	}
	general := convs[0].(map[string]any)["id"].(string)

	// me, by cookie; every authenticated answer carries the rate limit.
	status, hdr, me := owner.DoFull("GET", "/api/v1/me", nil)
	if status != 200 || me["account"].(map[string]any)["username"] != "kaity" {
		t.Fatalf("me: %d %v", status, me)
	}
	for _, h := range []string{"RateLimit-Limit", "RateLimit-Remaining", "RateLimit-Reset"} {
		if hdr.Get(h) == "" {
			t.Errorf("no %s header on an authenticated answer", h)
		}
	}
	anon.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)

	// Registration needs an invite, and the invite is counted once.
	anon.Refused(403, model.CodeForbidden, "POST", "/api/v1/passkey/register/begin", map[string]any{"username": "sam", "display_name": "Sam"})
	inv := owner.Must(201, "POST", "/api/v1/invites", map[string]any{})["invite"].(map[string]any)
	code := inv["code"].(string)
	if u, _ := inv["url"].(string); !strings.HasSuffix(u, "/join/"+code) { // the web join page lives at /join/
		t.Fatalf("invite url: %q", u)
	}
	if got := anon.Must(200, "GET", "/api/v1/invite-codes/"+code, nil); got["role"] != "member" {
		t.Fatalf("checkInvite: %v", got)
	}
	anon.Refused(409, model.CodeNameTaken, "POST", "/api/v1/passkey/register/begin", map[string]any{"invite_code": code, "username": "kaity", "display_name": "K"})
	anon.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/passkey/register/begin", map[string]any{"invite_code": code, "username": "Not Valid", "display_name": "K"})
	sam := ts.Browser()
	sam.Register("sam", code)
	anon.Refused(404, model.CodeNotFound, "GET", "/api/v1/invite-codes/"+code, nil)
	// A new member is in the channels @everyone is in, and nowhere else
	// (docs/PERMISSIONS.md case 104).
	convs = sam.Must(200, "GET", "/api/v1/conversations", nil)["conversations"].([]any)
	if len(convs) != 1 || convs[0].(map[string]any)["id"] != general {
		t.Fatalf("a new member should be in #general only: %v", convs)
	}

	// Sign out, then sign in again with the passkey.
	sam.Must(204, "POST", "/api/v1/logout", nil)
	sam.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	if status, out := sam.Login(); status != 200 || out["account"].(map[string]any)["username"] != "sam" {
		t.Fatalf("login: %d %v", status, out)
	}
	sam.Must(200, "GET", "/api/v1/me", nil)
	// A passkey nobody registered is refused, without saying why.
	if status, out := ts.Browser().Login(); status != 401 {
		t.Fatalf("login with an unknown passkey: %d %v", status, out)
	}

	// The origin check: a cookie request that changes something must come
	// from the server's own pages.
	sam.NoOrigin = true
	sam.Refused(403, model.CodeForbidden, "POST", "/api/v1/channels", map[string]any{"name": "nope", "private": false})
	sam.Must(200, "GET", "/api/v1/me", nil) // reads are not checked
	sam.NoOrigin = false

	// A personal token works as a bearer token, needs no origin, cannot
	// manage sign-in, and the prefixes are not interchangeable.
	script := sam.Token()
	if script.Bearer[:10] != "churr_pat_" {
		t.Fatalf("a personal token starts churr_pat_: %s", script.Bearer[:10])
	}
	if got := script.Must(200, "GET", "/api/v1/me", nil); got["account"].(map[string]any)["username"] != "sam" {
		t.Fatalf("me by token: %v", got)
	}
	script.Must(201, "POST", "/api/v1/channels", map[string]any{"name": "by-script", "private": false})
	script.Refused(403, model.CodeForbidden, "POST", "/api/v1/me/tokens", map[string]any{"label": "more"})
	bad := *script
	bad.Bearer = "churr_ses_" + script.Bearer[10:]
	bad.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	bad.Bearer = "churr_hook_" + script.Bearer[10:]
	bad.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil) // case 89

	// Invites: a member cannot make one unless the setting allows, and
	// sees only their own.
	sam.Refused(403, model.CodeForbidden, "POST", "/api/v1/invites", map[string]any{}) // case 79
	if n := len(sam.Must(200, "GET", "/api/v1/invites", nil)["invites"].([]any)); n != 0 {
		t.Fatalf("a member sees %d invites that are not theirs", n)
	}
	owner.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/invites", map[string]any{"role": "guest"})
	second := owner.Must(201, "POST", "/api/v1/invites", map[string]any{})["invite"].(map[string]any)
	sam.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/invites/"+second["id"].(string), nil)
	owner.Must(204, "DELETE", "/api/v1/invites/"+second["id"].(string), nil)
	anon.Refused(404, model.CodeNotFound, "GET", "/api/v1/invite-codes/"+second["code"].(string), nil)

	// Accounts: members see every local account.
	if n := len(sam.Must(200, "GET", "/api/v1/accounts", nil)["accounts"].([]any)); n != 2 {
		t.Fatalf("sam sees %d accounts, want 2", n)
	}
	got := sam.Must(200, "GET", "/api/v1/accounts/"+string(owner.ID), nil)["account"].(map[string]any)
	if got["username"] != "kaity" || got["presence"] != "offline" {
		t.Fatalf("getAccount: %v", got)
	}
	sam.Refused(404, model.CodeNotFound, "GET", "/api/v1/accounts/not-an-id", nil)
	// Unknown paths answer in the error shape.
	sam.Refused(404, model.CodeNotFound, "GET", "/api/v1/no/such/thing", nil)
}
