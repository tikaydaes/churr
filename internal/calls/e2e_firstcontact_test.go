package calls_test

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/calls/calltest"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Calls until you accept do not go to you (ruling R103): someone invited
// to a group chat or a channel who has not accepted is not rung, and is
// rung once they have.
func TestTheInvitedAreNotRung(t *testing.T) {
	ts := testkit.NewServer(t, testkit.Options{CallTimings: fast(), ChatRequests: true})
	ann, bob, cy := ts.Member("ann"), ts.Member("bob"), ts.Member("cy")
	bob.Must(200, "PATCH", "/api/v1/me/prefs", map[string]any{"auto_approve_direct": true})
	cyDesk, _ := cy.Events(nil)
	bobDesk, _ := bob.Events(nil)

	out := ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID), string(cy.ID)}})
	chat := out["conversation"].(map[string]any)["id"].(string)
	req := cyDesk.Expect("chat_request.received")
	a := enter(t, ann, chat, calltest.Options{})
	bobDesk.ExpectWhere("call.ring_started", func(f map[string]any) bool { return f["call_id"] == a.CallID })
	for {
		f, ok := cyDesk.Next(2 * time.Second)
		if !ok {
			break
		}
		if f["type"] == "call.ring_started" || f["conversation_id"] == chat {
			t.Fatalf("someone invited and not yet joined heard of the chat: %v", f)
		}
	}
	a.Leave()

	id := req["request"].(map[string]any)["id"].(string)
	cy.Must(200, "POST", "/api/v1/me/chat-requests/"+id+"/accept", nil)
	cyDesk2, _ := cy.Events(nil)
	a = enter(t, ann, chat, calltest.Options{})
	cyDesk2.ExpectWhere("call.ring_started", func(f map[string]any) bool { return f["call_id"] == a.CallID })
	a.Leave()
}
