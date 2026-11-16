package api_test

import (
	"testing"
	"time"

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

// Knocks: docs/FEATURES.md section 12.5, PERMISSIONS.md section 6.2 and
// cases 291 to 297 (ruling R74).
func TestKnocks(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("cat"), ts.Member("dan")
	secret := ann.Channel("Secret Garden", true, cat)
	open := ann.Channel("lobby", false)
	guest := ts.Guest("gil", open)
	annStream, _ := ann.Events(nil)
	bobQuiet, _ := bob.Events(nil)

	// 291: the answer is the same whether or not the channel exists, and
	// whether or not one is waiting already; it names nothing bob cannot see.
	none := bob.Must(200, "POST", "/api/v1/knocks", map[string]any{"name": "no such channel"})
	first := bob.Must(200, "POST", "/api/v1/knocks", map[string]any{"name": "#secret garden"})
	again := bob.Must(200, "POST", "/api/v1/knocks", map[string]any{"name": "Secret Garden"})
	for _, out := range []map[string]any{none, first, again} {
		if len(out) != 1 || len(out["joinable_ids"].([]any)) != 0 {
			t.Fatalf("a knock said more than nothing: %v", out)
		}
	}
	// The managers are told once, with the name; bob is told nothing yet.
	e := annStream.Expect("knock.created")
	k := obj(e, "knock")
	if k["account_id"] != string(bob.ID) || k["conversation_id"] != secret || k["outcome"] != "pending" || e["conversation_name"] != "Secret Garden" {
		t.Fatalf("knock.created: %v", e)
	}
	// (Reading with a deadline ends a stream, so a fresh one follows.)
	bobQuiet.ExpectNone(300 * time.Millisecond)
	bobStream, _ := bob.Events(nil)

	// 292: a public channel of the name is pointed to, not knocked on.
	out := bob.Must(200, "POST", "/api/v1/knocks", map[string]any{"name": "LOBBY"})
	if ids := out["joinable_ids"].([]any); len(ids) != 1 || ids[0] != open {
		t.Fatalf("joinable: %v", out)
	}
	if got := knockIDs(ann.Must(200, "GET", conv+open+"/knocks", nil)); len(got) != 0 {
		t.Fatalf("a public channel was knocked on: %v", got)
	}

	// 293: guests and bots do not knock; the name must be one.
	guest.Refused(403, model.CodeForbidden, "POST", "/api/v1/knocks", map[string]any{"name": "Secret Garden"})
	bob.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/knocks", map[string]any{"name": "  "})

	// 294: only those acting as manager list and answer; outsiders are told
	// the channel does not exist.
	cat.Refused(403, model.CodeForbidden, "GET", conv+secret+"/knocks", nil)
	bob.Refused(404, model.CodeNotFound, "GET", conv+secret+"/knocks", nil)
	waiting := knockIDs(ann.Must(200, "GET", conv+secret+"/knocks", nil))
	if len(waiting) != 1 || waiting[string(bob.ID)] != k["id"] {
		t.Fatalf("waiting: %v", waiting)
	}
	id := k["id"].(string)
	cat.Refused(403, model.CodeForbidden, "POST", conv+secret+"/knocks/"+id, map[string]any{"accept": true})
	bob.Refused(404, model.CodeNotFound, "POST", conv+secret+"/knocks/"+id, map[string]any{"accept": true})

	// 295: accepted, bob is a member at the default level and told, without
	// being told who answered.
	got := obj(ann.Must(200, "POST", conv+secret+"/knocks/"+id, map[string]any{"accept": true}), "knock")
	if got["outcome"] != "accepted" || got["answered_by_id"] != string(ann.ID) {
		t.Fatalf("answer: %v", got)
	}
	told := bobStream.ExpectWhere("knock.answered", func(f map[string]any) bool { return true })
	if tk := obj(told, "knock"); tk["outcome"] != "accepted" || tk["answered_by_id"] != nil || told["conversation_name"] != "Secret Garden" {
		t.Fatalf("knock.answered to bob: %v", told)
	}
	if !memberIDs(ann, secret)[string(bob.ID)] {
		t.Fatal("bob was not added")
	}
	ann.Refused(404, model.CodeNotFound, "POST", conv+secret+"/knocks/"+id, map[string]any{"accept": false})
	if !auditedFor(t, ts, "knock.accepted", string(bob.ID)) {
		t.Fatal("not audited")
	}

	// 296: declined, dan is told and stays out; he may ask again.
	dan.Must(200, "POST", "/api/v1/knocks", map[string]any{"name": "secret garden"})
	did := knockIDs(ann.Must(200, "GET", conv+secret+"/knocks", nil))[string(dan.ID)]
	danStream, _ := dan.Events(nil)
	ann.Must(200, "POST", conv+secret+"/knocks/"+did, map[string]any{"accept": false})
	if tk := obj(danStream.Expect("knock.answered"), "knock"); tk["outcome"] != "declined" {
		t.Fatalf("declined: %v", tk)
	}
	if memberIDs(ann, secret)[string(dan.ID)] {
		t.Fatal("a declined knock added dan")
	}
	dan.Must(200, "POST", "/api/v1/knocks", map[string]any{"name": "secret garden"})

	// 297: someone banned from the channel knocks to no effect, with the same
	// answer.
	eve := ts.Member("eve")
	ann.Must(200, "POST", conv+secret+"/members", map[string]any{"account_ids": []string{string(eve.ID)}})
	ann.Must(201, "POST", conv+secret+"/bans", map[string]any{"account_id": string(eve.ID)})
	if out := eve.Must(200, "POST", "/api/v1/knocks", map[string]any{"name": "secret garden"}); len(out["joinable_ids"].([]any)) != 0 {
		t.Fatalf("banned: %v", out)
	}
	if _, ok := knockIDs(ann.Must(200, "GET", conv+secret+"/knocks", nil))[string(eve.ID)]; ok {
		t.Fatal("a banned account's knock waits")
	}
}

// TestKnockLimit: ten knocks an hour, counted whatever the name.
func TestKnockLimit(t *testing.T) {
	ts := testkit.NewServer(t)
	bob := ts.Member("bob")
	for i := 0; i < 10; i++ {
		bob.Must(200, "POST", "/api/v1/knocks", map[string]any{"name": "nowhere"})
	}
	bob.Refused(429, model.CodeRateLimited, "POST", "/api/v1/knocks", map[string]any{"name": "nowhere"})
}
