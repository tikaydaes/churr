package api_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The server broadcast: docs/FEATURES.md section 12.6, PERMISSIONS.md
// section 9.1 and cases 305 to 307 (ruling R74).
func TestBroadcast(t *testing.T) {
	ts := testkit.NewServer(t)
	admin, bob := ts.Admin("adm"), ts.Member("bob")
	guest := ts.Guest("gil", bob.Channel("room", false))
	bobStream, _ := bob.Events(nil)
	guestStream, _ := guest.Events(nil)

	// 305: admins only; one line of 1 to 500 characters; a minute to a day.
	bob.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/broadcast", map[string]any{"text": "hello"})
	bob.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/broadcast", nil)
	admin.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/broadcast", map[string]any{"text": "  "})
	admin.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/broadcast", map[string]any{"text": "two\nlines"})
	admin.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/broadcast", map[string]any{"text": strings.Repeat("x", 501)})
	admin.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/broadcast", map[string]any{"text": "hi", "seconds": 86401})
	admin.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/broadcast", map[string]any{"text": "hi", "seconds": 30})
	admin.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/admin/broadcast", nil)
	if s := bob.Must(200, "GET", "/api/v1/status", nil); s["broadcast"] != nil {
		t.Fatalf("a broadcast before any: %v", s)
	}

	// 306: everyone connected gets it at once, guests too; it lasts ten
	// minutes unless told; whoever connects later finds it in the summary.
	before := time.Now()
	b := obj(admin.Must(200, "POST", "/api/v1/admin/broadcast", map[string]any{"text": "Upgrading in **5 minutes**"}), "broadcast")
	until, _ := time.Parse(time.RFC3339, b["until"].(string))
	if b["text"] != "Upgrading in **5 minutes**" || b["by_id"] != string(admin.ID) || until.Sub(before) < 599*time.Second || until.Sub(before) > 610*time.Second {
		t.Fatalf("broadcast: %v", b)
	}
	for _, s := range []*testkit.Stream{bobStream, guestStream} {
		if e := s.Expect("server.broadcast"); obj(e, "broadcast")["id"] != b["id"] {
			t.Fatalf("server.broadcast: %v", e)
		}
	}
	if got := obj(bob.Must(200, "GET", "/api/v1/status", nil), "broadcast"); got["id"] != b["id"] {
		t.Fatalf("status: %v", got)
	}
	// It is not a message anywhere.
	for _, m := range bob.Messages(bob.Channel("quiet", false)) {
		t.Fatalf("a broadcast became a message: %v", m)
	}
	// A new one replaces it, for the time given.
	b2 := obj(admin.Must(200, "POST", "/api/v1/admin/broadcast", map[string]any{"text": "Back in 15m", "seconds": 900}), "broadcast")
	if e := bobStream.Expect("server.broadcast"); obj(e, "broadcast")["id"] != b2["id"] {
		t.Fatalf("the second: %v", e)
	}

	// 307: an admin takes it down for everyone; it is in the audit log.
	ts.Owner.Must(204, "DELETE", "/api/v1/admin/broadcast", nil)
	if e := bobStream.Expect("server.broadcast"); e["broadcast"] != nil {
		t.Fatalf("cleared: %v", e)
	}
	if s := bob.Must(200, "GET", "/api/v1/status", nil); s["broadcast"] != nil {
		t.Fatalf("a cleared broadcast in the summary: %v", s)
	}
	if !auditedFor(t, ts, "broadcast.sent", b["id"].(string)) || !auditedFor(t, ts, "broadcast.cleared", b2["id"].(string)) {
		t.Fatal("not audited")
	}

	// Ten an hour for each admin.
	for i := 0; i < 8; i++ {
		admin.Must(200, "POST", "/api/v1/admin/broadcast", map[string]any{"text": "again"})
	}
	admin.Refused(429, model.CodeRateLimited, "POST", "/api/v1/admin/broadcast", map[string]any{"text": "once more"})
}
