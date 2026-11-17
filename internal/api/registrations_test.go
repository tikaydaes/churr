package api_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// TestOpenRegistrationsADay holds review finding H8: with registration
// open, one address makes at most five accounts a day without an invite,
// whatever the way in: a passkey, as here, an access key or company
// sign-in. An invite is the admins' own door and is not counted.
func TestOpenRegistrationsADay(t *testing.T) {
	ts := testkit.NewServer(t)
	ts.Owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"registration.mode": "open"})
	for i := 1; i <= 5; i++ {
		ts.Browser().Register(fmt.Sprintf("open%d", i), "")
	}
	b := ts.Browser()
	if status, out := b.Do("POST", "/api/v1/passkey/register/begin", map[string]any{"username": "open6", "display_name": "open6"}); status != 429 {
		t.Fatalf("a sixth account in a day from one address: %d %v", status, out)
	}
	// An invite still works.
	inv := ts.Owner.Must(201, "POST", "/api/v1/invites", map[string]any{})["invite"].(map[string]any)["code"].(string)
	ts.Browser().Register("invited", inv)
}

// TestRenamedUsernameIsHeld holds review finding H8: a username given up by
// renaming is not anyone else's for 30 days, by rename or by registration;
// the person who gave it up may take it back. The rename is audited.
func TestRenamedUsernameIsHeld(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ann.Must(200, "PATCH", "/api/v1/me", map[string]any{"username": "annie"})
	if e := wantAudit(t, ts, "account.renamed"); e.ActorID != string(ann.ID) || e.Detail["from"] != "ann" || e.Detail["to"] != "annie" {
		t.Fatalf("the rename in the audit log: %+v", e)
	}
	bob.Refused(409, model.CodeNameTaken, "PATCH", "/api/v1/me", map[string]any{"username": "ann"})
	inv := ts.Owner.Must(201, "POST", "/api/v1/invites", map[string]any{})["invite"].(map[string]any)["code"].(string)
	ts.Browser().Refused(409, model.CodeNameTaken, "POST", "/api/v1/passkey/register/begin",
		map[string]any{"invite_code": inv, "username": "ann", "display_name": "Not Ann"})
	// Ann takes it back; then "annie" is held for her in turn.
	ann.Must(200, "PATCH", "/api/v1/me", map[string]any{"username": "ann"})
	bob.Refused(409, model.CodeNameTaken, "PATCH", "/api/v1/me", map[string]any{"username": "annie"})
	// After the hold, it is free.
	ts.Exec("UPDATE username_holds SET released_at = ?", int64(model.Now().Add(-model.UsernameHold-time.Minute)))
	bob.Must(200, "PATCH", "/api/v1/me", map[string]any{"username": "annie"})
}

// TestInviteGuessesAreLimited holds docs/AUTH.md section 14 (review
// finding L4: the limit was declared and never used): ten wrong invite
// codes an hour from one address, after which even a right one waits.
func TestInviteGuessesAreLimited(t *testing.T) {
	ts := testkit.NewServer(t)
	inv := ts.Owner.Must(201, "POST", "/api/v1/invites", map[string]any{})["invite"].(map[string]any)["code"].(string)
	b := ts.Browser()
	b.Must(200, "GET", "/api/v1/invite-codes/"+inv, nil)
	for i := 0; i < 10; i++ {
		b.Refused(404, model.CodeNotFound, "GET", fmt.Sprintf("/api/v1/invite-codes/wrong%d", i), nil)
	}
	b.Refused(429, model.CodeRateLimited, "GET", "/api/v1/invite-codes/"+inv, nil)
	b.Refused(429, model.CodeRateLimited, "POST", "/api/v1/passkey/register/begin", map[string]any{"invite_code": inv, "username": "late", "display_name": "Late"})
}
