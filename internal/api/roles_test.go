package api_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The role model of rulings R97 and R99: moderators keep the peace in every
// channel, managers hold it, and an instance's Admins and Owners are neither
// unless made one. docs/PERMISSIONS.md cases 300 to 330.

func level(c *testkit.Client, conversation string, who model.AccountID) string {
	for _, m := range arr(c.Must(200, "GET", conv+conversation+"/members", nil), "members") {
		if m.(map[string]any)["account_id"] == string(who) {
			return m.(map[string]any)["level"].(string)
		}
	}
	return ""
}

func setLevel(c *testkit.Client, conversation string, who *testkit.Client, l string) {
	c.Must(200, "PATCH", conv+conversation+"/members/"+string(who.ID), map[string]any{"level": l})
}

// Cases 300 to 312: a manager appoints a moderator, who answers knocks,
// adds, removes, bans, deletes others' messages, times out, sets slow mode
// and locks, and does none of the manager's own things.
func TestModerators(t *testing.T) {
	ts := testkit.NewServer(t)
	mgr, mo, bob, cat, dan := ts.Member("mgr"), ts.Member("mo"), ts.Member("bob"), ts.Member("cat"), ts.Member("dan")
	room := convOf(mgr.Must(201, "POST", "/api/v1/channels", map[string]any{"name": "room", "access": "knock",
		"account_ids": []string{string(mo.ID), string(bob.ID)}}))["id"].(string)
	mo.AcceptInvitation(room)
	bob.AcceptInvitation(room)

	// 312: only a manager appoints; a moderator appoints nobody.
	bob.Refused(403, model.CodeForbidden, "PATCH", conv+room+"/members/"+string(mo.ID), map[string]any{"level": "moderator"})
	stream, _ := mo.Events(nil)
	setLevel(mgr, room, mo, "moderator")
	if e := stream.Expect("member.level_changed"); obj(e, "member")["level"] != "moderator" {
		t.Fatalf("member.level_changed: %v", e)
	}
	mo.Refused(403, model.CodeForbidden, "PATCH", conv+room+"/members/"+string(bob.ID), map[string]any{"level": "moderator"})

	// 300: a knock reaches the moderator at once, and the moderator
	// answers it.
	knock := obj(cat.Must(200, "POST", conv+room+"/knocks", nil), "knock")
	if e := stream.Expect("knock.created"); obj(e, "knock")["id"] != knock["id"] {
		t.Fatalf("knock.created to a moderator: %v", e)
	}
	bob.Refused(403, model.CodeForbidden, "GET", conv+room+"/knocks", nil)
	if got := arr(mo.Must(200, "GET", conv+room+"/knocks", nil), "knocks"); len(got) != 1 {
		t.Fatalf("the moderator's list: %v", got)
	}
	mo.Must(200, "POST", conv+room+"/knocks/"+knock["id"].(string), map[string]any{"accept": true})
	if level(mgr, room, cat.ID) != "member" {
		t.Fatal("300: the knock was not let in")
	}

	// 301: a moderator adds people, not bots; the level they come in at
	// is the managers' to choose.
	mo.Must(200, "POST", conv+room+"/members", map[string]any{"account_ids": []string{string(dan.ID)}})
	dan.AcceptInvitation(conv + room)
	mo.Refused(403, model.CodeForbidden, "POST", conv+room+"/members", map[string]any{"account_ids": []string{string(ts.Member("eve").ID)}, "level": "manager"})
	bot := ts.Owner.Bot("helper")
	mo.Refused(403, model.CodeForbidden, "POST", conv+room+"/members", map[string]any{"account_ids": []string{string(bot.ID)}})

	// 303: deletes another's message; 304: times out, slow mode.
	m := bob.Post(room, "rude")["id"].(string)
	mo.Must(200, "DELETE", msgs+m, nil)
	mo.Must(201, "POST", conv+room+"/timeouts", map[string]any{"account_id": bob.ID, "seconds": 600})
	mo.Must(200, "PATCH", conv+room, map[string]any{"slow_mode_seconds": 30})
	mo.Post(room, "a moderator does not wait")
	mo.Post(room, "nor twice")

	// 302: removes and bans a member, never a manager or another
	// moderator.
	mo.Refused(403, model.CodeForbidden, "DELETE", conv+room+"/members/"+string(mgr.ID), nil)
	mo.Refused(403, model.CodeForbidden, "POST", conv+room+"/bans", map[string]any{"account_id": string(mgr.ID)})
	mo.Refused(403, model.CodeForbidden, "POST", conv+room+"/timeouts", map[string]any{"account_id": mgr.ID, "seconds": 600})
	mo.Must(204, "DELETE", conv+room+"/members/"+string(dan.ID), nil)
	mo.Must(201, "POST", conv+room+"/bans", map[string]any{"account_id": string(cat.ID)})
	if got := arr(mo.Must(200, "GET", conv+room+"/bans", nil), "bans"); len(got) != 1 {
		t.Fatalf("the moderator sees the bans: %v", got)
	}
	mo.Must(204, "DELETE", conv+room+"/bans/"+string(cat.ID), nil)

	// 307: none of the manager's own.
	for field, v := range map[string]any{"topic": "x", "name": "y", "welcome": "z", "announcement": true, "access": "private"} {
		mo.Refused(403, model.CodeForbidden, "PATCH", conv+room, map[string]any{field: v})
	}
	pin := mgr.Post(room, "pin me")["id"].(string)
	mo.Refused(403, model.CodeForbidden, "PUT", conv+room+"/pins/"+pin, nil)
	mo.Refused(403, model.CodeForbidden, "POST", conv+room+"/archive", nil)

	// 306: reports reach the moderator.
	r := bob.Must(201, "POST", msgs+pin+"/report", map[string]any{"reason": "x"})["report_id"].(string)
	found := false
	for _, x := range arr(mo.Must(200, "GET", "/api/v1/reports", nil), "reports") {
		found = found || x.(map[string]any)["id"] == r
	}
	if !found {
		t.Fatal("306: the moderator is not given the report")
	}
	mo.Must(200, "POST", "/api/v1/reports/"+r+"/handle", map[string]any{"outcome": "dismissed"})

	// 305: a moderator locks. The channel is a knock channel, so private.
	mo.Must(200, "POST", conv+room+"/lock", nil)

	// Dismissed, the moderator keeps the peace no more.
	setLevel(mgr, room, mo, "member")
	mo.Refused(403, model.CodeForbidden, "DELETE", conv+room+"/members/"+string(bob.ID), nil)
}

// Cases 310 and 311: an Admin, and an Owner, who is not a moderator or a
// manager of a channel has none of their powers in it, public or private,
// in it or not. The instance's own powers stay theirs.
func TestAdminsAreNotManagers(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	admin := ts.Admin("adm")
	pub := ann.Channel("pub", false, bob, admin)
	priv := ann.Channel("priv", true, bob, admin, ts.Owner)
	m := bob.Post(pub, "in public")["id"].(string)
	for _, c := range []*testkit.Client{admin, ts.Owner} {
		for _, ch := range []string{pub, priv} {
			c.Refused(403, model.CodeForbidden, "PATCH", conv+ch, map[string]any{"topic": "x"})
			c.Refused(403, model.CodeForbidden, "DELETE", conv+ch+"/members/"+string(bob.ID), nil)
			c.Refused(403, model.CodeForbidden, "POST", conv+ch+"/bans", map[string]any{"account_id": string(bob.ID)})
			c.Refused(403, model.CodeForbidden, "PATCH", conv+ch+"/members/"+string(bob.ID), map[string]any{"level": "reader"})
			c.Refused(403, model.CodeForbidden, "POST", conv+ch+"/archive", nil)
		}
		c.Refused(403, model.CodeForbidden, "DELETE", msgs+m, nil)
	}
	admin.Refused(403, model.CodeForbidden, "POST", conv+priv+"/lock", nil)
	// The instance's own: an admin still exiles, purges and handles a
	// public channel's report.
	r := ann.Must(201, "POST", msgs+m+"/report", nil)["report_id"].(string)
	admin.Must(200, "POST", "/api/v1/reports/"+r+"/handle", map[string]any{"outcome": "deleted"})
	if got := obj(ann.Must(200, "GET", msgs+m, nil), "message"); got["deleted_at"] == nil {
		t.Fatalf("an admin's report outcome deletes by purging: %v", got)
	}
	admin.Must(201, "POST", "/api/v1/admin/bans", map[string]any{"account_id": bob.ID})
	// Made a manager, an admin is one like anyone.
	setLevel(ann, pub, admin, "manager")
	admin.Must(200, "PATCH", conv+pub, map[string]any{"topic": "as a manager"})
}

// Cases 315 to 322: several owners. An owner makes another; nobody demotes
// an owner, another owner included; an owner steps down only while another
// remains. Every change is in the audit log.
func TestOwners(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	ann, bob := ts.Member("ann"), ts.Member("bob")
	admin := ts.Admin("adm")
	set := func(c *testkit.Client, who model.AccountID, r string) (int, map[string]any) {
		return c.Do("PUT", "/api/v1/admin/accounts/"+string(who)+"/role", map[string]any{"role": r})
	}
	// 315: an admin makes nobody an owner.
	admin.Refused(403, model.CodeForbidden, "PUT", "/api/v1/admin/accounts/"+string(ann.ID)+"/role", map[string]any{"role": "owner"})
	// 316: the last owner cannot step down.
	owner.Refused(409, model.CodeConflict, "PUT", "/api/v1/admin/accounts/"+string(owner.ID)+"/role", map[string]any{"role": "admin"})
	// 317: an owner makes ann an owner.
	if status, out := set(owner, ann.ID, "owner"); status != 200 || obj(out, "account")["role"] != "owner" {
		t.Fatalf("made an owner: %d %v", status, out)
	}
	if !auditedFor(t, ts, "owner.made", string(ann.ID)) {
		t.Error("317: making an owner is in the audit log")
	}
	// 318: ann, an owner, may now make people admins and owners too.
	if status, _ := set(ann, bob.ID, "admin"); status != 200 {
		t.Fatalf("a second owner changes roles: %d", status)
	}
	// 319: nobody demotes an owner, another owner included.
	ann.Refused(403, model.CodeForbidden, "PUT", "/api/v1/admin/accounts/"+string(owner.ID)+"/role", map[string]any{"role": "admin"})
	owner.Refused(403, model.CodeForbidden, "PUT", "/api/v1/admin/accounts/"+string(ann.ID)+"/role", map[string]any{"role": "member"})
	// Nor removes, bans or times one out.
	owner.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/accounts/"+string(ann.ID), nil)
	owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/bans", map[string]any{"account_id": ann.ID})
	// 320: nor makes a recovery link for one.
	owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/accounts/"+string(ann.ID)+"/recovery-link", nil)
	// 321: the first owner steps down, which ann's being one allows.
	if status, out := set(owner, owner.ID, "admin"); status != 200 || obj(out, "account")["role"] != "admin" {
		t.Fatalf("stepping down: %d %v", status, out)
	}
	if !auditedFor(t, ts, "owner.stepped_down", string(owner.ID)) {
		t.Error("321: stepping down is in the audit log")
	}
	// 322: ann is now the last, and cannot.
	ann.Refused(409, model.CodeConflict, "PUT", "/api/v1/admin/accounts/"+string(ann.ID)+"/role", map[string]any{"role": "member"})
	owner.Refused(403, model.CodeForbidden, "PUT", "/api/v1/admin/accounts/"+string(bob.ID)+"/role", map[string]any{"role": "member"})
	// The owners are listed by role.
	list := arr(ann.Must(200, "GET", "/api/v1/admin/accounts?role=owner", nil), "accounts")
	if len(list) != 1 || list[0].(map[string]any)["id"] != string(ann.ID) {
		t.Fatalf("the owners: %v", list)
	}
}

// Cases 323 to 330: an owner appoints a manager of any channel without being
// in it, and adds someone to a private channel, locked or not, by owner
// override, which every member is shown and the owner gains nothing by.
func TestOwnerAppointsAndOverrides(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	ann, bob, cat, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("cat"), ts.Member("dan")
	admin := ts.Admin("adm")
	ac := "/api/v1/admin/channels/"
	pub := ann.Channel("pub", false, bob)
	priv := ann.Channel("priv", true, bob)
	early := ann.Post(priv, "before cat")["id"].(string)
	guest := ts.Guest("gil", pub)

	// 323: in a public channel anyone of the instance, who joins with it.
	admin.Refused(403, model.CodeForbidden, "PUT", ac+pub+"/managers/"+string(cat.ID), nil)
	if m := obj(owner.Must(200, "PUT", ac+pub+"/managers/"+string(cat.ID), nil), "member"); m["level"] != "manager" || m["invited"] != true {
		t.Fatalf("323: invited as a manager (ruling R103): %v", m)
	}
	cat.AcceptInvitation(pub)
	cat.Must(200, "PATCH", conv+pub, map[string]any{"topic": "cat manages it"})
	// 324: never a guest, a bot or someone on another server.
	owner.Refused(403, model.CodeForbidden, "PUT", ac+pub+"/managers/"+string(guest.ID), nil)
	// 325: in a private channel, a member of it.
	if m := obj(owner.Must(200, "PUT", ac+priv+"/managers/"+string(bob.ID), nil), "member"); m["level"] != "manager" {
		t.Fatalf("325: %v", m)
	}
	// 326: someone not in it only by owner override, said so.
	owner.Refused(409, model.CodeConflict, "PUT", ac+priv+"/managers/"+string(dan.ID), nil)
	annStream, _ := ann.Events(nil)
	if m := obj(owner.Must(200, "PUT", ac+priv+"/managers/"+string(dan.ID), map[string]any{"override": true}), "member"); m["level"] != "manager" || m["invited"] != true {
		t.Fatalf("326: %v", m)
	}
	// Nobody is placed unasked (ruling R103): dan accepts, and the notice
	// is posted then.
	dan.AcceptInvitation(priv)
	notice := annStream.ExpectWhere("message.created", func(f map[string]any) bool {
		sys, _ := obj(f, "message")["system"].(map[string]any)
		return sys != nil && sys["kind"] == "owner_override"
	})
	if sys := obj(notice, "message", "system"); sys["by_id"] != string(owner.ID) || obj(notice, "message")["author_id"] != string(dan.ID) {
		t.Fatalf("326: the notice: %v", notice)
	}
	if !auditedFor(t, ts, "channel.owner_override", string(dan.ID)) || !auditedFor(t, ts, "channel.manager_appointed", string(dan.ID)) {
		t.Error("326: the override and the appointment are in the audit log")
	}
	// 327: the newcomer reads from their join; the owner reads nothing.
	dan.Refused(404, model.CodeNotFound, "GET", msgs+early, nil)
	owner.Refused(404, model.CodeNotFound, "GET", conv+priv+"/messages", nil)

	// 328: an override without appointing, into a locked channel too.
	ann.Must(200, "POST", conv+priv+"/lock", nil)
	admin.Refused(403, model.CodeForbidden, "PUT", ac+priv+"/members/"+string(cat.ID), nil)
	ann.Refused(403, model.CodeLocked, "POST", conv+priv+"/members", map[string]any{"account_ids": []string{string(cat.ID)}})
	if m := obj(owner.Must(200, "PUT", ac+priv+"/members/"+string(cat.ID), nil), "member"); m["level"] != "member" || m["invited"] != true {
		t.Fatalf("328: %v", m)
	}
	cat.AcceptInvitation(priv)
	cat.Refused(404, model.CodeNotFound, "GET", msgs+early, nil)
	owner.Refused(409, model.CodeAlreadyExists, "PUT", ac+priv+"/members/"+string(cat.ID), nil)
	// The owner gains nothing: not their own way in.
	owner.Refused(403, model.CodeForbidden, "PUT", ac+priv+"/members/"+string(owner.ID), nil)
	owner.Refused(403, model.CodeForbidden, "PUT", ac+priv+"/managers/"+string(owner.ID), map[string]any{"override": true})
	// 329: not into a public channel; never a guest into a shared one
	// (TestGuestsAndSharing).
	owner.Refused(409, model.CodeConflict, "PUT", ac+pub+"/members/"+string(dan.ID), nil)
	// 330: someone banned from the channel stays out.
	other := ann.Channel("other", true, bob)
	ann.Must(201, "POST", conv+other+"/bans", map[string]any{"account_id": string(cat.ID)})
	owner.Refused(403, model.CodeForbidden, "PUT", ac+other+"/members/"+string(cat.ID), nil)
}

// Cases 313 and 314: a guest is a guest in every channel: never a moderator
// or a manager, however a level would come.
func TestGuestsAreGuests(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	pub := ann.Channel("pub", false)
	guest := ts.Guest("gil", pub)
	ann.Refused(403, model.CodeForbidden, "PATCH", conv+pub+"/members/"+string(guest.ID), map[string]any{"level": "moderator"})
	ann.Refused(403, model.CodeForbidden, "PATCH", conv+pub+"/members/"+string(guest.ID), map[string]any{"level": "manager"})
	setLevel(ann, pub, guest, "reader")
	// 314: a group that gives managers gives a guest member.
	group := obj(ts.Owner.Must(201, "POST", "/api/v1/admin/groups", map[string]any{"handle": "leads", "name": "Leads"}), "group")["id"].(string)
	ts.Owner.Must(204, "PUT", "/api/v1/admin/groups/"+group+"/members/"+string(guest.ID), nil)
	ann.Must(200, "PUT", conv+pub+"/groups/"+group, map[string]any{"level": "manager"})
	guest.AcceptOffer(pub)
	if got := level(ann, pub, guest.ID); got != "member" {
		t.Fatalf("314: a guest through a managers' group: %s", got)
	}
	ann.Must(200, "PUT", conv+pub+"/groups/"+group, map[string]any{"level": "moderator"})
	if got := level(ann, pub, guest.ID); got != "member" {
		t.Fatalf("314: a guest through a moderators' group: %s", got)
	}
}
