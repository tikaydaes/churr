package api_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/api"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The lock (ruling R75, docs/AUTH.md section 10.6): a locked session is
// answered only to unlock or sign out, its live connection is closed and
// refused, and only a user-verified assertion by one of the account's own
// passkeys unlocks it.

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// lockedMayStill is what a locked session is still answered.
var lockedMayStill = map[string]bool{"lockSession": true, "beginSessionUnlock": true, "logout": true}

func TestLockRefusesEverything(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	ann.Check()
	pat := ann.Token()
	laptop := ts.Browser()
	laptop.CopyPasskey(ann)
	if st, out := laptop.Login(); st != 200 {
		t.Fatalf("second browser: %d %v", st, out)
	}

	stream, _, err := ann.Dial()
	if err != nil {
		t.Fatal(err)
	}
	ann.Must(204, "POST", "/api/v1/session/lock", nil)
	if code := stream.CloseCode(5 * time.Second); code != 4002 {
		t.Fatalf("the live connection of a locked session closes with 4002, got %d", code)
	}
	if _, resp, err := ann.Dial(); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("a locked session cannot open the live connection: %v %v", resp, err)
	}

	// Every operation a signed-in session can call, from every area, is
	// refused, except unlocking and signing out.
	areas := map[string]int{}
	for _, op := range api.Operations {
		if op.Auth != api.AuthUser && op.Auth != api.AuthWeb && op.Auth != api.AuthPending {
			continue
		}
		if lockedMayStill[op.ID] || op.ID == "openEvents" {
			continue
		}
		path := pathParam.ReplaceAllString(op.Path, string(model.NewID[model.ConversationID]()))
		var body any
		if op.Method != "GET" && op.Method != "DELETE" {
			body = map[string]any{}
		}
		got, out := ann.Do(op.Method, path, body)
		e, _ := out["error"].(map[string]any)
		if got != 401 || e["code"] != string(model.CodeSessionLocked) {
			t.Errorf("%s %s (%s) while locked = %d %v", op.Method, path, op.ID, got, out)
		}
		areas[op.Area]++
	}
	if len(areas) < 10 {
		t.Fatalf("too few areas were tried: %v", areas)
	}

	// An operation that also works signed out treats the locked session
	// as nobody.
	if me := ann.Must(200, "GET", "/api/v1/server", nil); me["server"] == nil {
		t.Fatalf("server: %v", me)
	}
	// Locking again changes nothing.
	ann.Must(204, "POST", "/api/v1/session/lock", nil)

	// Each session locks on its own: the account's other browser and its
	// token are untouched, and the list shows which one is locked.
	pat.Must(200, "GET", "/api/v1/me", nil)
	var locked int
	for _, s := range laptop.Must(200, "GET", "/api/v1/me/sessions", nil)["sessions"].([]any) {
		if s.(map[string]any)["locked_at"] != nil {
			locked++
		}
	}
	if locked != 1 {
		t.Fatalf("exactly the one session is locked, got %d", locked)
	}
	// A token cannot lock (it has no screen to lock).
	pat.Refused(403, model.CodeForbidden, "POST", "/api/v1/session/lock", nil)

	// Signing out works while locked.
	ann.Must(204, "POST", "/api/v1/logout", nil)
	ann.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
}

func TestUnlockNeedsAUserVerifiedAssertion(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ann.Refused(409, model.CodeConflict, "POST", "/api/v1/session/unlock/begin", nil)
	ann.Must(204, "POST", "/api/v1/session/lock", nil)

	begin := ann.Must(200, "POST", "/api/v1/session/unlock/begin", nil)
	pk := at(begin, "public_key")
	if pk["userVerification"] != "required" || len(pk["allowCredentials"].([]any)) != 1 {
		t.Fatalf("unlocking is limited to the account's passkeys with user verification: %v", begin)
	}
	if !strings.HasPrefix(begin["unlock_url"].(string), ts.Origin+"/locked/#ceremony=") {
		t.Fatalf("unlock_url: %v", begin["unlock_url"])
	}
	// A touch without user verification does not unlock, and the ceremony
	// is spent.
	ann.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/session/unlock/finish", ann.AssertUnverified(begin))
	ann.Refused(404, model.CodeNotFound, "POST", "/api/v1/session/unlock/finish", ann.Assert(begin))
	ann.Refused(401, model.CodeSessionLocked, "GET", "/api/v1/me", nil)
	// Another account's passkey does not either.
	begin = ann.Must(200, "POST", "/api/v1/session/unlock/begin", nil)
	ann.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/session/unlock/finish", bob.Assert(begin))
	// A passkey check's ceremony is not an unlock, nor the other way round.
	check := bob.Must(200, "POST", "/api/v1/checks/passkey/begin", nil)
	ann.Refused(404, model.CodeNotFound, "POST", "/api/v1/session/unlock/finish", bob.Assert(check))

	begin = ann.Must(200, "POST", "/api/v1/session/unlock/begin", nil)
	ann.Must(204, "POST", "/api/v1/session/unlock/finish", ann.Assert(begin))
	signedInAs(t, ann, "ann")
	if _, _, err := ann.Dial(); err != nil {
		t.Fatalf("after unlocking the live connection opens: %v", err)
	}
	// The unlock counts as a fresh check.
	ann.Must(201, "POST", "/api/v1/me/tokens", map[string]any{"label": "after unlock"})
	// Bob was never locked.
	signedInAs(t, bob, "bob")
}

func TestUnlockFailuresAreLimited(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ann.Must(204, "POST", "/api/v1/session/lock", nil)
	for i := 0; i < 5; i++ {
		begin := ann.Must(200, "POST", "/api/v1/session/unlock/begin", nil)
		ann.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/session/unlock/finish", bob.Assert(begin))
	}
	ann.Refused(429, model.CodeRateLimited, "POST", "/api/v1/session/unlock/begin", nil)
	ann.Refused(401, model.CodeSessionLocked, "GET", "/api/v1/me", nil)
}

func TestUnlockAnAppThroughTheServersPage(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	app := ts.Browser()
	verifier, challenge := pkce("a-verifier-of-at-least-forty-three-characters-long")
	code := approve(t, ann, authorizeQuery(challenge, nil))
	tok := token(app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {appRedirect}}).JSON()
	phone := ts.App(tok["access_token"].(string))
	signedInAs(t, phone, "ann")

	phone.Must(204, "POST", "/api/v1/session/lock", nil)
	phone.Refused(401, model.CodeSessionLocked, "GET", "/api/v1/me", nil)
	// The browser session that approved it is another session: not locked.
	signedInAs(t, ann, "ann")
	// The app may still refresh its tokens, and stays locked.
	fresh := token(app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok["refresh_token"].(string)}}).JSON()
	phone = ts.App(fresh["access_token"].(string))
	phone.Refused(401, model.CodeSessionLocked, "GET", "/api/v1/me", nil)

	begin := phone.Must(200, "POST", "/api/v1/session/unlock/begin", nil)
	id := strings.TrimPrefix(begin["unlock_url"].(string), ts.Origin+"/locked/#ceremony=")
	if id != begin["ceremony_id"] {
		t.Fatalf("unlock_url carries the ceremony after the #: %v", begin)
	}
	// The page in the system browser holds no session: it fetches the
	// options by the ceremony and sends the assertion of the person's
	// passkey.
	page := ts.Browser()
	page.Refused(404, model.CodeNotFound, "POST", "/api/v1/session/unlock/options", map[string]any{"ceremony_id": "made-up"})
	opts := page.Must(200, "POST", "/api/v1/session/unlock/options", map[string]any{"ceremony_id": id})
	page.Must(204, "POST", "/api/v1/session/unlock/finish", ann.Assert(map[string]any{"ceremony_id": id, "public_key": opts["public_key"]}))
	signedInAs(t, phone, "ann")
}

func TestLockedSessionEndedElsewhere(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	ann.Must(204, "POST", "/api/v1/session/lock", nil)
	// Revoked from elsewhere: the locked browser is now simply signed out,
	// and is told so (unauthenticated, not session_locked).
	ts.Owner.Check()
	ts.Owner.Must(204, "DELETE", "/api/v1/admin/accounts/"+string(ann.ID)+"/sessions", nil)
	ann.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	ann.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/session/unlock/begin", nil)
}
