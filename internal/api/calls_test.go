package api_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/calls/calltest"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The call operations over HTTP, with each refusal the contract lists. What
// happens on the live connection and on the media port is tested in
// internal/calls.
func TestCallOperations(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cy := ts.Member("ann"), ts.Member("bob"), ts.Member("cy")
	priv := ann.Channel("private", true, bob)
	base := conv + priv + "/call"

	// getCall: anyone who can read the conversation; nobody else.
	if out := bob.Must(200, "GET", base, nil); out["call"] != nil {
		t.Fatalf("no call yet: %v", out)
	}
	cy.Refused(404, model.CodeNotFound, "GET", base, nil)
	ann.Refused(404, model.CodeNotFound, "GET", conv+"not-an-id/call", nil)
	ts.Browser().Refused(401, model.CodeUnauthenticated, "GET", base, nil)

	// joinCall: starts the call, answers with the call and a ticket.
	cy.Refused(404, model.CodeNotFound, "POST", base+"/join", nil)
	ts.Browser().Refused(401, model.CodeUnauthenticated, "POST", base+"/join", nil)
	out := ann.Must(200, "POST", base+"/join", nil)
	call := obj(out, "call")
	if call["id"] == nil || call["conversation_id"] != priv || call["started_by_id"] != string(ann.ID) || call["sealed"] != false ||
		call["participants"] == nil || out["ticket"] == "" {
		t.Fatalf("joinCall: %v", out)
	}
	// Asking again joins the same call, with another ticket.
	again := bob.Must(200, "POST", base+"/join", nil)
	if obj(again, "call")["id"] != call["id"] || again["ticket"] == out["ticket"] {
		t.Fatalf("a second joinCall: %v", again)
	}
	if got := obj(bob.Must(200, "GET", base, nil), "call"); got["id"] != call["id"] {
		t.Fatalf("getCall: %v", got)
	}
	// A reader may see it and not join it.
	ann.Must(200, "PATCH", conv+priv+"/members/"+string(bob.ID), map[string]any{"level": "reader"})
	bob.Refused(403, model.CodeForbidden, "POST", base+"/join", nil)
	bob.Must(200, "GET", base, nil)

	// leaveCall: only someone who is in the call, which takes call.join.
	ann.Refused(404, model.CodeNotFound, "POST", base+"/leave", nil)
	cy.Refused(404, model.CodeNotFound, "POST", base+"/leave", nil)
	stream, _ := ann.Events(nil)
	a := calltest.New(t, stream, calltest.Options{})
	t.Cleanup(a.Close)
	a.MustJoin(call["id"].(string), out["ticket"].(string))

	// listLiveCalls: admins; numbers, not people; the conversation only
	// for a call in a public channel.
	ann.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/calls", nil)
	pub := ann.Channel("public", false, cy)
	cy.Must(200, "POST", conv+pub+"/call/join", nil)
	list := ts.Owner.Must(200, "GET", "/api/v1/admin/calls", nil)
	rows := arr(list, "calls")
	if len(rows) != 2 || !strings.HasPrefix(list["media_address"].(string), "127.0.0.1:") {
		t.Fatalf("listLiveCalls: %v", list)
	}
	first, second := rows[0].(map[string]any), rows[1].(map[string]any)
	if first["id"] != call["id"] || first["participant_count"] != 1.0 || first["camera_count"] != 0.0 || first["conversation_id"] != nil || first["started_at"] == nil {
		t.Fatalf("a call in a private channel, to an admin: %v", first)
	}
	if second["conversation_id"] != pub || second["participant_count"] != 0.0 {
		t.Fatalf("a call in a public channel, to an admin: %v", second)
	}
	if ov := obj(ts.Owner.Must(200, "GET", "/api/v1/admin/overview", nil), "overview"); ov["live_calls"] != 2.0 {
		t.Fatalf("the overview's count of calls: %v", ov["live_calls"])
	}

	ann.Must(204, "POST", base+"/leave", nil)
	ann.Refused(404, model.CodeNotFound, "POST", base+"/leave", nil)
	if out := ann.Must(200, "GET", base, nil); out["call"] != nil {
		t.Fatalf("the call after its last participant left: %v", out)
	}

	// testCalls: the admin's browser makes a media connection with its
	// own offer and sees which of UDP and TCP worked, and where to.
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/calls/test", map[string]any{"sdp": "v=0"})
	ts.Owner.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/calls/test", map[string]any{"sdp": ""})
	ts.Owner.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/calls/test", map[string]any{"sdp": "not an offer"})
	ts.Owner.Refused(400, model.CodeInvalidRequest, "POST", "/api/v1/admin/calls/test", map[string]any{"sdp": "v=0", "extra": true})
	for _, tcpOnly := range []bool{false, true} {
		var told string
		proto, addr, err := calltest.Probe(tcpOnly, func(offer string) (string, error) {
			status, out := ts.Owner.Do("POST", "/api/v1/admin/calls/test", map[string]any{"sdp": offer})
			if status != 200 {
				return "", errors.New("the test was refused")
			}
			told, _ = out["media_address"].(string)
			return out["sdp"].(string), nil
		})
		want := "udp"
		if tcpOnly {
			want = "tcp"
		}
		if err != nil || proto != want || addr != told || !strings.HasPrefix(told, "127.0.0.1:") {
			t.Fatalf("the call test (tcp only: %v): %s %s, told %s, %v", tcpOnly, proto, addr, told, err)
		}
	}

	// With calls switched off: nothing can be joined or tested.
	ts.Owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"calls.enabled": false})
	ann.Refused(403, model.CodeFeatureOff, "POST", base+"/join", nil)
	ts.Owner.Refused(403, model.CodeFeatureOff, "POST", "/api/v1/admin/calls/test", map[string]any{"sdp": "v=0"})
	ann.Must(200, "GET", base, nil)
}
