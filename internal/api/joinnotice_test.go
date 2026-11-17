package api_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/testkit"
)

// Joins and additions are announced in a private channel, whatever the
// route; the channel's founders are not; public channels post nothing.
func TestPrivateChannelAnnouncesNewcomers(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("cat"), ts.Member("dan")
	council := ann.Channel("council", true, cat)
	notices := func(c *testkit.Client, id string) []map[string]any {
		var out []map[string]any
		for _, raw := range arr(c.Must(200, "GET", conv+id+"/messages", nil), "messages") {
			m := raw.(map[string]any)
			if sys, ok := m["system"].(map[string]any); ok && sys["kind"] == "member_joined" {
				out = append(out, m)
			}
		}
		return out
	}
	// Cat was invited with the channel and arrived when she accepted
	// (ruling R103): a newcomer like any other.
	if n := notices(ann, council); len(n) != 1 || n[0]["author_id"] != string(cat.ID) {
		t.Fatalf("cat's arrival: %v", n)
	}
	ann.Must(200, "POST", conv+council+"/members", map[string]any{"account_ids": []string{string(bob.ID)}})
	bob.AcceptInvitation(council)
	got := notices(cat, council)[1:]
	if len(got) != 1 || got[0]["author_id"] != string(bob.ID) || got[0]["system"].(map[string]any)["by_id"] != string(ann.ID) {
		t.Fatalf("bob's addition: %v", got)
	}
	if len(notices(bob, council)) != 1 {
		t.Fatal("bob does not see his own arrival")
	}

	// Through a group: dan is in it.
	g := obj(ts.Owner.Must(201, "POST", "/api/v1/admin/groups", map[string]any{"handle": "late", "name": "Late"}), "group")["id"].(string)
	ts.Owner.Must(204, "PUT", "/api/v1/admin/groups/"+g+"/members/"+string(dan.ID), nil)
	ann.Must(200, "PUT", conv+council+"/groups/"+g, map[string]any{"level": "member"})
	dan.AcceptOffer(council)
	got = notices(cat, council)
	if len(got) != 3 || got[2]["author_id"] != string(dan.ID) {
		t.Fatalf("dan's arrival through a group: %v", got)
	}

	// A public channel posts none.
	lobby := ann.Channel("lobby", false)
	bob.Must(200, "POST", conv+lobby+"/join", nil)
	if n := notices(ann, lobby); len(n) != 0 {
		t.Fatalf("a public channel announced: %v", n)
	}
}
