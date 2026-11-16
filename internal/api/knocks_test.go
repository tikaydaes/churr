package api_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

func knockIDs(out map[string]any) map[string]string {
	ids := map[string]string{}
	for _, k := range out["knocks"].([]any) {
		m := k.(map[string]any)
		ids[m["account_id"].(string)] = m["id"].(string)
	}
	return ids
}

// knockChannel makes a knock channel as c, with members, and returns its id.
func knockChannel(c *testkit.Client, name string, members ...*testkit.Client) string {
	ids := []string{}
	for _, m := range members {
		ids = append(ids, string(m.ID))
	}
	out := c.Must(201, "POST", "/api/v1/channels", map[string]any{"name": name, "access": "knock", "account_ids": ids})
	return obj(out, "conversation")["id"].(string)
}

func browsed(c *testkit.Client, id string) map[string]any {
	for _, raw := range c.Must(200, "GET", "/api/v1/channels", nil)["conversations"].([]any) {
		if m := raw.(map[string]any); m["id"] == id {
			return m
		}
	}
	return nil
}

// Knock channels: docs/FEATURES.md section 12.5, PERMISSIONS.md section 6.2
// and cases 291 to 297 (rulings R74, R79). What someone outside sees of one,
// and that they cannot read or join it.
func TestKnockChannels(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	lobby := ann.Channel("lobby", false)
	guest := ts.Guest("gil", lobby)
	council := knockChannel(ann, "Council", cat)
	ann.Post(council, "the secret plan")
	ann.Must(200, "PATCH", conv+council, map[string]any{"welcome": "Welcome, members"})

	// 291: bob, a member of the server, sees its name, topic, member count
	// and managers in the browser and by link; never its content.
	c := browsed(bob, council)
	if c == nil || c["access"] != "knock" || c["private"] != true || c["member_count"] != float64(2) || c["membership"] != nil || c["welcome"] != nil {
		t.Fatalf("browse: %v", c)
	}
	if ids, _ := c["manager_ids"].([]any); len(ids) != 1 || ids[0] != string(ann.ID) {
		t.Fatalf("managers: %v", c)
	}
	if got := obj(bob.Must(200, "GET", conv+council, nil), "conversation"); got["access"] != "knock" || got["welcome"] != nil {
		t.Fatalf("by link: %v", got)
	}
	bob.Refused(404, model.CodeNotFound, "GET", conv+council+"/messages", nil)
	bob.Refused(404, model.CodeNotFound, "GET", conv+council+"/members", nil)
	bob.Refused(403, model.CodeForbidden, "POST", conv+council+"/join", nil)
	if len(bob.Must(200, "GET", "/api/v1/search?q=secret", nil)["results"].([]any)) != 0 {
		t.Fatal("search shows bob the knock channel's messages")
	}
	// Members see it whole, without the outsider's list of managers.
	if got := obj(cat.Must(200, "GET", conv+council, nil), "conversation"); got["manager_ids"] != nil || got["membership"] == nil {
		t.Fatalf("a member's view: %v", got)
	}

	// 292: guests and bots do not see it, so cannot knock.
	guest.Refused(404, model.CodeNotFound, "GET", conv+council, nil)
	guest.Refused(403, model.CodeForbidden, "POST", conv+council+"/knocks", nil)
	if browsed(guest, council) != nil {
		t.Fatal("a guest's browser shows the knock channel")
	}

	// 293: a private channel cannot be knocked on, or seen; a public one is
	// joined instead.
	secret := ann.Channel("secret", true)
	bob.Refused(404, model.CodeNotFound, "POST", conv+secret+"/knocks", nil)
	if browsed(bob, secret) != nil {
		t.Fatal("bob sees a private channel")
	}
	bob.Refused(409, model.CodeConflict, "POST", conv+lobby+"/knocks", nil)
	cat.Refused(409, model.CodeConflict, "POST", conv+council+"/knocks", nil)
}

// TestKnocks: asking, answering and being told (cases 294 to 297).
func TestKnocks(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("cat"), ts.Member("dan")
	council := knockChannel(ann, "Council", cat)
	annStream, _ := ann.Events(nil)
	bobStream, _ := bob.Events(nil)

	// 294: one request waits; asking again returns it.
	k := obj(bob.Must(200, "POST", conv+council+"/knocks", nil), "knock")
	again := obj(bob.Must(200, "POST", conv+council+"/knocks", nil), "knock")
	if k["outcome"] != "pending" || again["id"] != k["id"] {
		t.Fatalf("knock: %v, again %v", k, again)
	}
	if e := annStream.Expect("knock.created"); obj(e, "knock")["id"] != k["id"] || e["conversation_name"] != "Council" {
		t.Fatalf("knock.created: %v", e)
	}

	// 295: only those acting as manager list and answer.
	cat.Refused(403, model.CodeForbidden, "GET", conv+council+"/knocks", nil)
	bob.Refused(403, model.CodeForbidden, "GET", conv+council+"/knocks", nil)
	id := k["id"].(string)
	cat.Refused(403, model.CodeForbidden, "POST", conv+council+"/knocks/"+id, map[string]any{"accept": true})
	if got := knockIDs(ann.Must(200, "GET", conv+council+"/knocks", nil)); got[string(bob.ID)] != id {
		t.Fatalf("waiting: %v", got)
	}

	// 296: accepted, bob is a member at the default level, reads, and is
	// told without who answered.
	got := obj(ann.Must(200, "POST", conv+council+"/knocks/"+id, map[string]any{"accept": true}), "knock")
	if got["outcome"] != "accepted" || got["answered_by_id"] != string(ann.ID) {
		t.Fatalf("answer: %v", got)
	}
	told := bobStream.ExpectWhere("knock.answered", func(map[string]any) bool { return true })
	if tk := obj(told, "knock"); tk["outcome"] != "accepted" || tk["answered_by_id"] != nil || told["conversation_name"] != "Council" {
		t.Fatalf("knock.answered to bob: %v", told)
	}
	ann.Post(council, "welcome bob")
	bob.Must(200, "GET", conv+council+"/messages", nil)
	ann.Refused(404, model.CodeNotFound, "POST", conv+council+"/knocks/"+id, map[string]any{"accept": false})
	if !auditedFor(t, ts, "knock.accepted", string(bob.ID)) {
		t.Fatal("not audited")
	}

	// 297: declined, dan is told plainly and stays out; he may ask again.
	dk := obj(dan.Must(200, "POST", conv+council+"/knocks", nil), "knock")
	danStream, _ := dan.Events(nil)
	ann.Must(200, "POST", conv+council+"/knocks/"+dk["id"].(string), map[string]any{"accept": false})
	if tk := obj(danStream.Expect("knock.answered"), "knock"); tk["outcome"] != "declined" {
		t.Fatalf("declined: %v", tk)
	}
	if memberIDs(ann, council)[string(dan.ID)] {
		t.Fatal("a declined knock added dan")
	}
	dan.Must(200, "POST", conv+council+"/knocks", nil)

	// A banned account no longer sees the channel at all.
	eve := ts.Member("eve")
	ann.Must(201, "POST", conv+council+"/bans", map[string]any{"account_id": string(eve.ID)})
	eve.Refused(404, model.CodeNotFound, "POST", conv+council+"/knocks", nil)
}

// TestKnockAccess: who changes a channel's kind of access, and which way.
func TestKnockAccess(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ch := ann.Channel("open", false)
	sub := obj(ann.Must(201, "POST", "/api/v1/channels", map[string]any{"name": "sub", "private": false, "parent_id": ch}), "conversation")["id"].(string)
	set := func(c *testkit.Client, id, access string) map[string]any {
		return obj(c.Must(200, "PATCH", conv+id, map[string]any{"access": access}), "conversation")
	}
	bob.Refused(403, model.CodeForbidden, "PATCH", conv+ch, map[string]any{"access": "knock"})
	if c := set(ann, ch, "knock"); c["access"] != "knock" || c["private"] != true {
		t.Fatalf("public to knock: %v", c)
	}
	if browsed(bob, ch) == nil {
		t.Fatal("bob does not see the knock channel")
	}
	if c := set(ann, ch, "public"); c["access"] != "public" || c["private"] != false {
		t.Fatalf("knock to public: %v", c)
	}
	if c := set(ann, ch, "private"); c["access"] != "private" {
		t.Fatalf("public to private: %v", c)
	}
	ann.Refused(403, model.CodeForbidden, "PATCH", conv+ch, map[string]any{"access": "knock"})
	ann.Refused(403, model.CodeForbidden, "PATCH", conv+ch, map[string]any{"access": "public"})
	ann.Refused(400, model.CodeInvalidValue, "PATCH", conv+sub, map[string]any{"access": "knock"})
	ann.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/channels", map[string]any{"name": "x", "access": "knock", "parent_id": sub})
	ann.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/channels", map[string]any{"name": "x", "access": "sideways"})
}

// TestKnockLimit: ten knocks an hour.
func TestKnockLimit(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	for i := 0; i < 10; i++ {
		id := knockChannel(ann, "k")
		bob.Must(200, "POST", conv+id+"/knocks", nil)
	}
	bob.Refused(429, model.CodeRateLimited, "POST", conv+knockChannel(ann, "k")+"/knocks", nil)
}
