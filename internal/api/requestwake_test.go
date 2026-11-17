package api_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// A chat request wakes the phones of the person asked with the same
// anonymous wake-up as a message: no name, no content, nothing that says it
// is a request. The fetch tells the app who is asking and nothing of the
// chat; once answered, it says the request is no longer waiting.
func TestChatRequestWakesPhones(t *testing.T) {
	ts := requestServer(t)
	relay := newTestRelay(t, ts)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	bob.Must(200, "PUT", "/api/v1/me/devices/current", map[string]any{"platform": "ios", "push_kind": "apns", "push_address": "bobs-phone"})

	chat := ask(ann, 201, bob)
	wake := relay.take(t, 1)[0]
	keys := []string{}
	for k := range wake {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "badge,collapse_key,event_id,platform,priority,push_address" || wake["priority"] != "high" {
		t.Fatalf("the wake-up for a request: %v", wake)
	}
	for _, leak := range []string{"ann", "request", string(ann.ID), chat["id"].(string)} {
		if strings.Contains(relay.raw[len(relay.raw)-1], leak) {
			t.Fatalf("the relay was told %q: %s", leak, relay.raw[len(relay.raw)-1])
		}
	}
	path := "/api/v1/wake-ups/" + wake["event_id"].(string)
	got := bob.Must(200, "GET", path, nil)["wake_up"].(map[string]any)
	req, _ := got["request"].(map[string]any)
	if got["kind"] != "request" || got["read"] != false || len(arr(got, "messages")) != 0 || req["from_name"] != "ann" || req["kind"] != "direct" {
		t.Fatalf("the fetched wake-up: %v", got)
	}
	// Nobody else can read it, not even ann.
	ann.Refused(404, model.CodeNotFound, "GET", path, nil)
	cat.Refused(404, model.CodeNotFound, "GET", path, nil)

	// Declined, it no longer waits.
	id := requestsOf(bob)[0].(map[string]any)["id"].(string)
	bob.Must(204, "POST", "/api/v1/me/chat-requests/"+id+"/decline", nil)
	if got := bob.Must(200, "GET", path, nil)["wake_up"].(map[string]any); got["read"] != true {
		t.Fatalf("after declining: %v", got)
	}

	// An account that approves chats automatically gets an ordinary chat,
	// and no request wake-up.
	cat.Must(200, "PATCH", "/api/v1/me/prefs", map[string]any{"auto_approve_direct": true})
	cat.Must(200, "PUT", "/api/v1/me/devices/current", map[string]any{"platform": "ios", "push_kind": "apns", "push_address": "cats-phone"})
	ask(ann, 201, cat)
	relay.none(t, "cat approves chats automatically")
}
