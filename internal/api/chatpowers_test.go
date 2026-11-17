package api_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// TestChatsHaveNoManager holds review finding H1 and PERMISSIONS.md section
// 2.4: a direct chat and a group chat (ad-hoc channels) have no manager and
// no moderator. Whoever opened one holds no power over the others in it: no
// deleting their messages, no time-outs, no removing or banning them, no
// levels, no lock. Everyone is a member and anyone may leave.
func TestChatsHaveNoManager(t *testing.T) {
	ts := requestServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	autoApprove(bob)
	direct := ask(ann, 201, bob)["id"].(string)
	group := mkchat(ann, bob, cat)
	cat.AcceptInvitation(group) // included in a group chat: a request (ruling R103)

	for _, chat := range []string{direct, group} {
		members := arr(ann.Must(200, "GET", conv+chat+"/members", nil), "members")
		for _, m := range members {
			if lvl := m.(map[string]any)["level"]; lvl != "member" {
				t.Fatalf("everyone in a chat is a member: %v", members)
			}
		}
		theirs := bob.Post(chat, "bob's own words")["id"].(string)
		ann.Refused(403, model.CodeForbidden, "DELETE", msgs+theirs, nil)
		ann.Refused(403, model.CodeForbidden, "POST", conv+chat+"/timeouts", map[string]any{"account_id": string(bob.ID), "seconds": 600})
		ann.Refused(403, model.CodeForbidden, "DELETE", conv+chat+"/members/"+string(bob.ID), nil)
		ann.Refused(403, model.CodeForbidden, "POST", conv+chat+"/bans", map[string]any{"account_id": string(bob.ID)})
		ann.Refused(403, model.CodeForbidden, "PATCH", conv+chat+"/members/"+string(bob.ID), map[string]any{"level": "reader"})
		ann.Refused(403, model.CodeForbidden, "POST", conv+chat+"/lock", nil)
		if m := obj(bob.Must(200, "GET", msgs+theirs, nil), "message"); m["deleted_at"] != nil {
			t.Fatalf("the opener deleted another's message: %v", m)
		}
	}
	// A chat of two is not named: naming would make it a channel with a
	// manager.
	ann.Refused(403, model.CodeForbidden, "PATCH", conv+direct, map[string]any{"name": "ours"})
	// The topic of a group chat is anyone's in it.
	cat.Must(200, "PATCH", conv+group, map[string]any{"topic": "lunch"})
	// Anyone may leave.
	cat.Must(204, "POST", conv+group+"/leave", nil)
}
