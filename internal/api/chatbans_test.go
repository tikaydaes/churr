package api_test

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

func memberIDs(c *testkit.Client, convID string) map[string]bool {
	out := c.Must(200, "GET", conv+convID+"/members", nil)
	ids := map[string]bool{}
	for _, m := range out["members"].([]any) {
		ids[m.(map[string]any)["account_id"].(string)] = true
	}
	return ids
}

func banIDs(out map[string]any) map[string]bool {
	ids := map[string]bool{}
	for _, b := range out["bans"].([]any) {
		ids[b.(map[string]any)["account_id"].(string)] = true
	}
	return ids
}

// Channel bans: docs/FEATURES.md section 19.1, PERMISSIONS.md section 6.1
// and cases 281 to 290.
func TestChannelBans(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	admin := ts.Admin("adm")
	pub := ann.Channel("pub", false, bob, cat)
	bobStream, _ := bob.Events(nil)

	// 281: a member-level account bans nobody.
	cat.Refused(403, model.CodeForbidden, "POST", conv+pub+"/bans", map[string]any{"account_id": string(bob.ID)})
	// 283: a manager cannot ban an admin, nor themselves.
	ann.Refused(403, model.CodeForbidden, "POST", conv+pub+"/bans", map[string]any{"account_id": string(admin.ID)})
	ann.Refused(403, model.CodeForbidden, "POST", conv+pub+"/bans", map[string]any{"account_id": string(ann.ID)})
	ann.Refused(400, model.CodeInvalidValue, "POST", conv+pub+"/bans", map[string]any{"account_id": string(bob.ID), "seconds": 5})

	// The manager bans bob: he is out, and told once.
	b := obj(ann.Must(201, "POST", conv+pub+"/bans", map[string]any{"account_id": string(bob.ID), "reason": "spam"}), "ban")
	if b["account_id"] != string(bob.ID) || b["created_by_id"] != string(ann.ID) || b["reason"] != "spam" || b["expires_at"] != nil {
		t.Fatalf("ban: %v", b)
	}
	if e := bobStream.Expect("member.left"); e["reason"] != "banned" {
		t.Fatalf("member.left: %v", e)
	}
	if e := bobStream.Expect("channel_ban.created"); obj(e, "ban")["id"] != b["id"] || e["conversation_name"] != "pub" {
		t.Fatalf("channel_ban.created: %v", e)
	}
	if memberIDs(ann, pub)[string(bob.ID)] {
		t.Fatal("bob is still a member")
	}
	// The next message does not reach him (the member caches dropped him
	// with the ban).
	ann.Post(pub, "after the ban")
	bobStream.ExpectNone(400 * time.Millisecond)

	// 287: he cannot read, join, or be added; the channel is hidden from
	// the browser and search.
	bob.Refused(404, model.CodeNotFound, "GET", conv+pub+"/messages", nil)
	bob.Refused(404, model.CodeNotFound, "POST", conv+pub+"/join", nil)
	ann.Refused(403, model.CodeForbidden, "POST", conv+pub+"/members", map[string]any{"account_ids": []string{string(bob.ID)}})
	for _, c := range bob.Must(200, "GET", "/api/v1/channels", nil)["conversations"].([]any) {
		if c.(map[string]any)["id"] == pub {
			t.Fatal("the browser shows bob the channel")
		}
	}
	if len(bob.Must(200, "GET", "/api/v1/search?q=after", nil)["results"].([]any)) != 0 {
		t.Fatal("search shows bob the channel")
	}
	// 289: a group does not bring him back.
	group := obj(ts.Owner.Must(201, "POST", "/api/v1/admin/groups", map[string]any{"handle": "crew", "name": "Crew"}), "group")["id"].(string)
	ts.Owner.Must(204, "PUT", "/api/v1/admin/groups/"+group+"/members/"+string(bob.ID), nil)
	ann.Must(200, "PUT", conv+pub+"/groups/"+group, map[string]any{"level": "member"})
	if memberIDs(ann, pub)[string(bob.ID)] {
		t.Fatal("a group brought bob back")
	}

	// Who sees the list: the managers, and an admin acting as one.
	if !banIDs(ann.Must(200, "GET", conv+pub+"/bans", nil))[string(bob.ID)] {
		t.Fatal("the manager's list")
	}
	cat.Refused(403, model.CodeForbidden, "GET", conv+pub+"/bans", nil)
	if !banIDs(admin.Must(200, "GET", "/api/v1/admin/channel-bans", nil))[string(bob.ID)] {
		t.Fatal("the admin console's list")
	}
	cat.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/channel-bans", nil)

	// Lifted: he may join again, and was not brought back by it.
	cat.Refused(403, model.CodeForbidden, "DELETE", conv+pub+"/bans/"+string(bob.ID), nil)
	ann.Must(204, "DELETE", conv+pub+"/bans/"+string(bob.ID), nil)
	ann.Refused(404, model.CodeNotFound, "DELETE", conv+pub+"/bans/"+string(bob.ID), nil)
	if memberIDs(ann, pub)[string(bob.ID)] {
		t.Fatal("the group's member came back only when the group changes")
	}
	bob.Must(200, "POST", conv+pub+"/join", nil)

	// A ban with an end stops counting once past it.
	ann.Must(201, "POST", conv+pub+"/bans", map[string]any{"account_id": string(cat.ID), "seconds": 3600})
	cat.Refused(404, model.CodeNotFound, "POST", conv+pub+"/join", nil)
	ts.Exec(`UPDATE channel_bans SET expires_at = 1 WHERE account_id = ?`, string(cat.ID))
	cat.Must(200, "POST", conv+pub+"/join", nil)
	if len(ann.Must(200, "GET", conv+pub+"/bans", nil)["bans"].([]any)) != 0 {
		t.Fatal("a ban past its end is still listed")
	}
}

// Ad-hoc, private and shared channels; an admin outside a private channel.
func TestChannelBanKinds(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	admin := ts.Admin("adm")

	// 285: in an ad-hoc channel whoever may remove there may ban.
	chat := mkchat(ann, bob, cat)
	bob.Refused(403, model.CodeForbidden, "POST", conv+chat+"/bans", map[string]any{"account_id": string(cat.ID)})
	ann.Must(201, "POST", conv+chat+"/bans", map[string]any{"account_id": string(cat.ID)})
	ann.Refused(403, model.CodeForbidden, "POST", conv+chat+"/members", map[string]any{"account_ids": []string{string(cat.ID)}})
	cat.Refused(404, model.CodeNotFound, "GET", conv+chat+"/messages", nil)

	// 282: an admin outside a private channel cannot ban there, but may
	// see and lift its bans from the console.
	priv := ann.Channel("priv", true, bob)
	admin.Refused(404, model.CodeNotFound, "POST", conv+priv+"/bans", map[string]any{"account_id": string(bob.ID)})
	ann.Must(201, "POST", conv+priv+"/bans", map[string]any{"account_id": string(bob.ID)})
	bob.Refused(404, model.CodeNotFound, "GET", conv+priv, nil)
	if !banIDs(admin.Must(200, "GET", conv+priv+"/bans", nil))[string(bob.ID)] {
		t.Fatal("the admin console reads a private channel's bans")
	}
	admin.Must(204, "DELETE", conv+priv+"/bans/"+string(bob.ID), nil)
	ann.Must(200, "POST", conv+priv+"/members", map[string]any{"account_ids": []string{string(bob.ID)}})

	// 290: a sub-channel does not inherit its parent's ban.
	pub := ann.Channel("pub", false, cat)
	subID := sub(ann, "pub-sub", false, pub)
	ann.Must(201, "POST", conv+pub+"/bans", map[string]any{"account_id": string(cat.ID)})
	cat.Must(200, "POST", conv+subID+"/join", nil)

	// 286: a channel shared with other servers takes no new bans.
	ts.Exec(`UPDATE conversations SET stream_id = ? WHERE id = ?`, string(model.NewID[model.ConversationID]()), pub)
	ann.Refused(409, model.CodeConflict, "POST", conv+pub+"/bans", map[string]any{"account_id": string(bob.ID)})
}
