package api_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

const msgs = "/api/v1/messages/"

func mkchat(c *testkit.Client, with ...*testkit.Client) string {
	ids := []string{}
	for _, w := range with {
		ids = append(ids, string(w.ID))
	}
	_, out := c.Do("POST", "/api/v1/chats", map[string]any{"account_ids": ids})
	return convOf(out)["id"].(string)
}

func sub(c *testkit.Client, name string, private bool, parent string) string {
	out := c.Must(201, "POST", "/api/v1/channels", map[string]any{"name": name, "private": private, "parent_id": parent})
	return convOf(out)["id"].(string)
}

// Sub-channels: docs/PERMISSIONS.md section 7.2, cases 217 to 232.
func TestSubChannels(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	admin := ts.Admin("adm")
	pub := ann.Channel("pub", false, bob)
	pub2 := bob.Channel("pub2", false)
	priv := ann.Channel("priv", true)
	stream, _ := bob.Events(nil)

	// 217: a manager of PUB2 who is a member of PUB sets PUB as its parent.
	out := convOf(bob.Must(200, "PATCH", conv+pub2, map[string]any{"parent_id": pub}))
	if out["parent_id"] != pub {
		t.Fatalf("217: %v", out)
	}
	if e := stream.Expect("conversation.updated"); obj(e, "conversation")["parent_id"] != pub {
		t.Fatalf("conversation.updated carries the parent for someone who can see it: %v", e)
	}
	// Clearing with null.
	if out = convOf(bob.Must(200, "PATCH", conv+pub2, map[string]any{"parent_id": nil})); out["parent_id"] != nil {
		t.Fatalf("cleared: %v", out)
	}
	// 218: not a member of the parent; 219: a private parent they are not in.
	catCh := cat.Channel("cats", false)
	cat.Refused(403, model.CodeForbidden, "PATCH", conv+catCh, map[string]any{"parent_id": pub})
	cat.Refused(404, model.CodeNotFound, "PATCH", conv+catCh, map[string]any{"parent_id": priv})
	cat.Refused(404, model.CodeNotFound, "PATCH", conv+catCh, map[string]any{"parent_id": string(model.NewID[model.ConversationID]())})
	// 220: a member-level account in both.
	bob.Add(pub2, "member", cat)
	cat.Must(200, "POST", conv+pub+"/join", nil)
	cat.Refused(403, model.CodeForbidden, "PATCH", conv+pub2, map[string]any{"parent_id": pub})
	// 221: an admin in neither: from the console, not as a manager (ruling
	// R99).
	admin.Refused(403, model.CodeForbidden, "PATCH", conv+pub2, map[string]any{"parent_id": pub})
	admin.Must(200, "PATCH", "/api/v1/admin/channels/"+pub2, map[string]any{"parent_id": pub})
	// 222: a sub-channel as parent; 223: a channel with sub-channels given one.
	third := bob.Channel("third", false)
	bob.Refused(409, model.CodeConflict, "PATCH", conv+third, map[string]any{"parent_id": pub2})
	ann.Must(200, "POST", conv+third+"/join", nil)
	ann.Refused(409, model.CodeConflict, "PATCH", conv+pub, map[string]any{"parent_id": third})
	ts.Owner.Refused(409, model.CodeConflict, "PATCH", "/api/v1/admin/channels/"+pub, map[string]any{"parent_id": third})
	// 224: ad-hoc channels are never parents or children.
	chat := mkchat(ann, bob)
	ann.Refused(400, model.CodeInvalidValue, "PATCH", conv+chat, map[string]any{"parent_id": pub})
	bob.Refused(400, model.CodeInvalidValue, "PATCH", conv+third, map[string]any{"parent_id": chat})
	// 225: its own parent.
	bob.Refused(400, model.CodeInvalidValue, "PATCH", conv+third, map[string]any{"parent_id": third})
	bob.Refused(400, model.CodeInvalidValue, "PATCH", conv+third, map[string]any{"parent_id": "not-an-id"})

	// At creation: a member of the parent, or an admin. 237: the same name
	// under the same parent, any number of times.
	hidden := sub(ann, "ops", true, pub)
	open1 := sub(ann, "ops", false, pub)
	sub(bob, "ops", false, pub)
	dan := ts.Member("dan")
	dan.Refused(403, model.CodeForbidden, "POST", "/api/v1/channels", map[string]any{"name": "x", "private": false, "parent_id": pub})
	dan.Refused(404, model.CodeNotFound, "POST", "/api/v1/channels", map[string]any{"name": "x", "private": false, "parent_id": priv})
	bob.Refused(409, model.CodeConflict, "POST", "/api/v1/channels", map[string]any{"name": "x", "private": false, "parent_id": open1})
	sub(admin, "from the admin", false, pub)

	// 226: a member of PUB is told nothing of a private sub-channel they
	// are not in.
	list := arr(bob.Must(200, "GET", "/api/v1/channels?parent_id="+pub, nil), "conversations")
	for _, c := range list {
		if c.(map[string]any)["id"] == hidden {
			t.Fatal("226: a private sub-channel was listed to someone not in it")
		}
		if c.(map[string]any)["parent_id"] != pub {
			t.Fatalf("a listed sub-channel carries its parent: %v", c)
		}
	}
	if len(list) != 4 { // pub2, two public "ops", the admin's
		t.Fatalf("bob's list of PUB's sub-channels: %d", len(list))
	}
	if own := arr(ann.Must(200, "GET", "/api/v1/channels?parent_id="+pub, nil), "conversations"); len(own) != 5 {
		t.Fatalf("ann's list holds the private one too: %d", len(own))
	}
	bob.Refused(404, model.CodeNotFound, "GET", conv+hidden, nil)
	// 227: even for a manager of the parent, and its messages.
	ann.Must(200, "PATCH", conv+pub+"/members/"+string(bob.ID), map[string]any{"level": "manager"})
	bob.Refused(404, model.CodeNotFound, "GET", conv+hidden+"/messages", nil)
	// The sub-channels of a channel the caller cannot see: none.
	privSub := sub(ann, "under priv", false, priv)
	if got := arr(dan.Must(200, "GET", "/api/v1/channels?parent_id="+priv, nil), "conversations"); len(got) != 0 {
		t.Fatalf("sub-channels of a private channel, to an outsider: %v", got)
	}
	// 228: being in a sub-channel gives nothing in its parent.
	dan.Must(200, "POST", conv+privSub+"/join", nil)
	dan.Refused(404, model.CodeNotFound, "POST", conv+priv+"/messages", map[string]any{"text": "x"})
	dan.Refused(403, model.CodeForbidden, "POST", conv+pub+"/messages", map[string]any{"text": "x"})
	// 229: joining a parent joins none of its sub-channels.
	dan.Must(200, "POST", conv+pub+"/join", nil)
	mine := arr(dan.Must(200, "GET", "/api/v1/conversations", nil), "conversations")
	for _, c := range mine {
		if id := c.(map[string]any)["id"]; id == open1 || id == pub2 || id == hidden {
			t.Fatalf("229: joining PUB joined a sub-channel: %v", c)
		}
	}
	// 230: a public sub-channel of PRIV, fetched by someone not in PRIV,
	// carries no parent_id; nor does it in their lists or events.
	if got := convOf(dan.Must(200, "GET", conv+privSub, nil)); got["parent_id"] != nil {
		t.Fatalf("230: %v", got)
	}
	for _, c := range mine {
		if c.(map[string]any)["id"] == privSub && c.(map[string]any)["parent_id"] != nil {
			t.Fatalf("230: in the list: %v", c)
		}
	}
	if got := convOf(ann.Must(200, "GET", conv+privSub, nil)); got["parent_id"] != priv {
		t.Fatalf("a member of the parent is given it: %v", got)
	}
	danStream, _ := dan.Events(nil)
	annStream, _ := ann.Events(nil)
	ann.Must(200, "PATCH", conv+privSub, map[string]any{"topic": "news"})
	if e := danStream.Expect("conversation.updated"); obj(e, "conversation")["parent_id"] != nil || obj(e, "conversation")["topic"] != "news" {
		t.Fatalf("230: the event, for someone who cannot see the parent: %v", e)
	}
	if e := annStream.Expect("conversation.updated"); obj(e, "conversation")["parent_id"] != priv {
		t.Fatalf("the same event, for someone who can: %v", e)
	}
	// The admin view names the parent, and filters by it.
	if got := arr(admin.Must(200, "GET", "/api/v1/admin/channels?parent_id="+priv, nil), "channels"); len(got) != 1 || got[0].(map[string]any)["parent_id"] != priv {
		t.Fatalf("admin list by parent: %v", got)
	}
	// An admin sets a parent from the console, on a private channel they
	// are not in.
	lone := ann.Channel("lone", true)
	if got := obj(admin.Must(200, "PATCH", "/api/v1/admin/channels/"+lone, map[string]any{"parent_id": pub}), "channel"); got["parent_id"] != pub {
		t.Fatalf("admin sets a parent: %v", got)
	}
	if got := obj(admin.Must(200, "PATCH", "/api/v1/admin/channels/"+lone, map[string]any{"parent_id": nil}), "channel"); got["parent_id"] != nil {
		t.Fatalf("admin clears a parent: %v", got)
	}

	// 232: archiving, locking or making a parent private does nothing to
	// its sub-channels.
	ann.Must(200, "PATCH", conv+pub, map[string]any{"private": true})
	ann.Must(200, "POST", conv+pub+"/lock", nil)
	ann.Must(200, "POST", conv+pub+"/archive", nil)
	ann.Post(open1, "still open")
	if got := convOf(ann.Must(200, "GET", conv+open1, nil)); got["private"] != false || got["locked_at"] != nil || got["archived_at"] != nil {
		t.Fatalf("232: %v", got)
	}

	// 231: when PRIV's last member leaves, PRIV is deleted and its
	// sub-channel remains, with members and messages, and no parent.
	dan.Post(privSub, "kept")
	ann.Must(204, "POST", conv+priv+"/leave", nil)
	ann.Refused(404, model.CodeNotFound, "GET", conv+priv, nil)
	left := convOf(ann.Must(200, "GET", conv+privSub, nil))
	if left["parent_id"] != nil || left["member_count"] != float64(2) {
		t.Fatalf("231: %v", left)
	}
	if m := arr(dan.Must(200, "GET", conv+privSub+"/messages", nil), "messages"); len(m) == 0 {
		t.Fatal("231: the sub-channel's messages went with its parent")
	}
}

// Thread to channel: docs/FEATURES.md section 1.6, ruling R20, cases 178,
// 233 and 239.
func TestMoveThread(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("cat"), ts.Member("dan")
	old := ann.Channel("old", true, bob, cat, dan)
	reply := func(c *testkit.Client, parent, text string, extra map[string]any) string {
		body := map[string]any{"text": text, "parent_id": parent}
		for k, v := range extra {
			body[k] = v
		}
		return obj(c.Must(201, "POST", conv+old+"/messages", body), "message")["id"].(string)
	}
	p := bob.Post(old, "shall we plan the offsite?")["id"].(string)
	r1 := reply(cat, p, "yes, in March", map[string]any{"quote": map[string]any{"message_id": p}})
	r2 := reply(bob, p, "booked", nil)
	cat.Must(200, "PUT", msgs+r2+"/reactions/%F0%9F%8E%89", nil)
	single := bob.Post(old, "no replies here")["id"].(string)
	move := "/move-to-channel"

	// Refusals.
	bob.Refused(400, model.CodeInvalidRequest, "POST", msgs+r2+move, map[string]any{"name": "x"})
	bob.Refused(400, model.CodeInvalidRequest, "POST", msgs+single+move, map[string]any{"name": "x"})
	bob.Refused(400, model.CodeInvalidValue, "POST", msgs+p+move, map[string]any{"name": ""})
	// Not the thread's starter and not a manager.
	cat.Refused(403, model.CodeForbidden, "POST", msgs+p+move, map[string]any{"name": "x"})
	ts.Member("out").Refused(404, model.CodeNotFound, "POST", msgs+p+move, map[string]any{"name": "x"})
	ts.Admin("adm").Refused(404, model.CodeNotFound, "POST", msgs+p+move, map[string]any{"name": "x"})

	annStream, _ := ann.Events(nil)
	catStream, _ := cat.Events(nil)
	// 239: under a name already in use. The starter moves it.
	d := convOf(bob.Must(201, "POST", msgs+p+move, map[string]any{"name": "old"}))
	did := d["id"].(string)
	// 233: the new channel is a sub-channel of the channel the thread was
	// in, and private like it.
	if d["name"] != "old" || d["private"] != true || d["parent_id"] != old || d["moved_from_message_id"] != p || obj(d, "membership")["level"] != "manager" {
		t.Fatalf("the new channel: %v", d)
	}
	// In the old channel: one thread.moved event, the parent untouched,
	// the pointer in place of the replies.
	e := annStream.Expect("thread.moved")
	pointer := obj(e, "pointer")
	if e["to_conversation_id"] != did || obj(e, "parent")["id"] != p || obj(e, "parent", "thread")["reply_count"] != float64(1) ||
		pointer["parent_id"] != p || obj(pointer, "system")["kind"] != "thread_moved" || obj(pointer, "system")["conversation_id"] != did ||
		pointer["seq"] != e["seq"] || e["by_id"] != string(bob.ID) {
		t.Fatalf("thread.moved: %v", e)
	}
	// Cat replied, so followed, so is a first member, told with the
	// conversation; ann and dan did not and are not.
	told := false
	for i := 0; i < 2 && !told; i++ {
		// One for each first member; cat's own carries the conversation.
		j := catStream.Expect("member.joined")
		told = obj(j, "member")["account_id"] == string(cat.ID) && obj(j, "conversation")["id"] == did
	}
	if !told {
		t.Fatal("cat was not told of joining the new channel, with the conversation")
	}
	thread := ann.Must(200, "GET", msgs+p+"/thread", nil)
	if rs := arr(thread, "messages"); len(rs) != 1 || rs[0].(map[string]any)["id"] != pointer["id"] || obj(thread, "parent")["text"] != "shall we plan the offsite?" {
		t.Fatalf("the old thread: %v", thread)
	}
	// The old thread accepts no more replies; the thread cannot move again.
	ann.Refused(409, model.CodeConflict, "POST", conv+old+"/messages", map[string]any{"text": "late", "parent_id": p})
	bob.Refused(409, model.CodeConflict, "POST", msgs+p+move, map[string]any{"name": "again"})

	// The new channel: a copy of the parent, then the replies, keeping
	// authors, times, quotes and reactions.
	list := arr(cat.Must(200, "GET", conv+did+"/messages", nil), "messages")
	if len(list) != 3 {
		t.Fatalf("the new channel's messages: %v", list)
	}
	first, m1, m2 := list[0].(map[string]any), list[1].(map[string]any), list[2].(map[string]any)
	if first["id"] == p || first["text"] != "shall we plan the offsite?" || first["author_id"] != string(bob.ID) || first["seq"] != float64(1) {
		t.Fatalf("the copy: %v", first)
	}
	if m1["id"] != r1 || m1["author_id"] != string(cat.ID) || m1["parent_id"] != nil || obj(m1, "quote")["message_id"] != first["id"] {
		t.Fatalf("the first reply, now top-level, quoting the copy: %v", m1)
	}
	if m2["id"] != r2 || len(arr(m2, "reactions")) != 1 {
		t.Fatalf("the second reply with its reaction: %v", m2)
	}
	// Moved replies are ordinary messages and can have threads.
	cat.Must(201, "POST", conv+did+"/messages", map[string]any{"text": "a thread of its own", "parent_id": r1})
	// Dan was in the old channel and did not follow: not a member, but
	// may see and join the new private channel from the pointer.
	dan.Refused(404, model.CodeNotFound, "GET", conv+did+"/messages", nil)
	dan.Must(200, "POST", conv+did+"/join", nil)
	dan.Must(200, "GET", conv+did+"/messages", nil)
	// Someone who was never in the old channel cannot.
	ts.Member("eve").Refused(404, model.CodeNotFound, "POST", conv+did+"/join", nil)
	// 178: nor can a member of the old channel once it is locked.
	bob.Must(200, "POST", conv+did+"/lock", nil)
	ann.Refused(403, model.CodeLocked, "POST", conv+did+"/join", nil)

	// A manager moves someone else's thread; it is in the audit log. Out
	// of a sub-channel the new channel is a sibling (233).
	p2 := cat.Post(did, "a thread in the sub-channel")["id"].(string)
	dan.Must(201, "POST", conv+did+"/messages", map[string]any{"text": "reply", "parent_id": p2})
	sib := convOf(bob.Must(201, "POST", msgs+p2+move, map[string]any{"name": "sibling"}))
	if sib["parent_id"] != old || sib["locked_at"] != nil {
		t.Fatalf("233: out of a sub-channel, a sibling that is not locked: %v", sib)
	}
	if !auditedFor(t, ts, "thread.moved", p2) || auditedFor(t, ts, "thread.moved", p) {
		t.Error("a move by someone other than the starter is audited, and only that")
	}
	// Out of an ad-hoc channel: private, and no parent.
	chat := mkchat(ann, bob)
	p3 := ann.Post(chat, "chat thread")["id"].(string)
	bob.Must(201, "POST", conv+chat+"/messages", map[string]any{"text": "reply", "parent_id": p3})
	fromChat := convOf(ann.Must(201, "POST", msgs+p3+move, map[string]any{"name": "from a chat"}))
	if fromChat["parent_id"] != nil || fromChat["private"] != true {
		t.Fatalf("233: out of an ad-hoc channel: %v", fromChat)
	}
	// Not out of an archived channel.
	p4 := ann.Post(old, "another")["id"].(string)
	bob.Must(201, "POST", conv+old+"/messages", map[string]any{"text": "reply", "parent_id": p4})
	ann.Must(200, "POST", conv+old+"/archive", nil)
	ann.Refused(403, model.CodeArchived, "POST", msgs+p4+move, map[string]any{"name": "x"})
}

func TestTimeoutsAndSlowMode(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	admin, admin2 := ts.Admin("adm"), ts.Admin("adm2")
	ch := ann.Channel("room", false, bob)
	other := ann.Channel("other", false, bob)
	mine := bob.Post(ch, "before")["id"].(string)
	bobStream, _ := bob.Events(nil)
	annStream, _ := ann.Events(nil)
	touts := conv + ch + "/timeouts"

	// Refusals.
	bob.Refused(403, model.CodeForbidden, "POST", touts, map[string]any{"account_id": ann.ID, "seconds": 600})
	ann.Refused(400, model.CodeInvalidValue, "POST", touts, map[string]any{"account_id": bob.ID, "seconds": 59})
	ann.Refused(400, model.CodeInvalidValue, "POST", touts, map[string]any{"account_id": bob.ID, "seconds": 29 * 86400})
	ann.Refused(403, model.CodeForbidden, "POST", touts, map[string]any{"account_id": ts.Owner.ID, "seconds": 600})
	// An admin is no manager (ruling R99), and is timed out here like
	// anyone.
	admin.Refused(403, model.CodeForbidden, "POST", touts, map[string]any{"account_id": admin2.ID, "seconds": 600})
	admin.Refused(403, model.CodeForbidden, "POST", touts, map[string]any{"account_id": bob.ID, "seconds": 600})
	ann.Refused(404, model.CodeNotFound, "POST", touts, map[string]any{"account_id": model.NewID[model.AccountID](), "seconds": 600})

	out := obj(ann.Must(201, "POST", touts, map[string]any{"account_id": bob.ID, "seconds": 600, "reason": "cool off"}), "timeout")
	if out["account_id"] != string(bob.ID) || out["conversation_id"] != ch || out["reason"] != "cool off" || out["created_by_id"] != string(ann.ID) {
		t.Fatalf("timeout: %v", out)
	}
	// The account is told, with the reason; so are the managers.
	if e := bobStream.Expect("timeout.started"); obj(e, "timeout")["reason"] != "cool off" {
		t.Fatalf("timeout.started: %v", e)
	}
	annStream.Expect("timeout.started")
	if !auditedFor(t, ts, "timeout.started", string(bob.ID)) {
		t.Error("a time-out is written to the audit log")
	}
	// What is refused while it lasts, and what is not (PERMISSIONS 11).
	bob.Refused(403, model.CodeTimedOut, "POST", conv+ch+"/messages", map[string]any{"text": "x"})
	bob.Refused(403, model.CodeTimedOut, "PUT", msgs+mine+"/reactions/%F0%9F%91%8D", nil)
	bob.Refused(403, model.CodeTimedOut, "PATCH", msgs+mine, map[string]any{"text": "edited"})
	if s, _ := bob.Form("POST", conv+ch+"/files", testkit.Part{Field: "file", FileName: "a.txt", Content: []byte("x")}); s != 403 {
		t.Fatalf("upload while timed out: %d", s)
	}
	bob.Must(200, "GET", conv+ch+"/messages", nil)
	bob.Must(201, "POST", msgs+mine+"/report", nil)
	bob.Post(other, "another channel is not covered")
	bob.Must(200, "DELETE", msgs+mine, nil)

	if list := arr(ann.Must(200, "GET", touts, nil), "timeouts"); len(list) != 1 {
		t.Fatalf("time-outs in force: %v", list)
	}
	bob.Refused(403, model.CodeForbidden, "GET", touts, nil)
	// A new time-out replaces the one in force.
	second := obj(ann.Must(201, "POST", touts, map[string]any{"account_id": bob.ID, "seconds": 3600}), "timeout")["id"].(string)
	bobStream.Expect("timeout.lifted")
	bobStream.Expect("timeout.started")
	if list := arr(ann.Must(200, "GET", touts, nil), "timeouts"); len(list) != 1 || list[0].(map[string]any)["id"] != second {
		t.Fatalf("replaced: %v", list)
	}
	bob.Refused(403, model.CodeForbidden, "DELETE", touts+"/"+second, nil)
	ann.Refused(404, model.CodeNotFound, "DELETE", conv+other+"/timeouts/"+second, nil)
	ann.Must(204, "DELETE", touts+"/"+second, nil)
	if e := bobStream.Expect("timeout.lifted"); obj(e, "timeout")["lifted_at"] == nil {
		t.Fatalf("timeout.lifted: %v", e)
	}
	bob.Post(ch, "back")

	// Server-wide, by an admin.
	bob.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/timeouts", map[string]any{"account_id": ann.ID, "seconds": 600})
	admin.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/timeouts", map[string]any{"account_id": admin2.ID, "seconds": 600})
	admin.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/timeouts", map[string]any{"account_id": ts.Owner.ID, "seconds": 600})
	admin.Refused(404, model.CodeNotFound, "POST", "/api/v1/admin/timeouts", map[string]any{"account_id": model.NewID[model.AccountID](), "seconds": 600})
	ts.Owner.Must(201, "POST", "/api/v1/admin/timeouts", map[string]any{"account_id": admin2.ID, "seconds": 600})
	wide := obj(admin.Must(201, "POST", "/api/v1/admin/timeouts", map[string]any{"account_id": bob.ID, "seconds": 600}), "timeout")
	if wide["conversation_id"] != nil {
		t.Fatalf("a server-wide time-out names no conversation: %v", wide)
	}
	bob.Refused(403, model.CodeTimedOut, "POST", conv+other+"/messages", map[string]any{"text": "x"})
	bob.Refused(403, model.CodeTimedOut, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(ann.ID)}})
	priv := ann.Channel("priv", true, bob)
	ann.Must(201, "POST", conv+priv+"/timeouts", map[string]any{"account_id": bob.ID, "seconds": 600})
	listed := arr(admin.Must(200, "GET", "/api/v1/admin/timeouts", nil), "timeouts")
	if len(listed) != 2 { // bob's and admin2's server-wide ones; not the private channel's
		t.Fatalf("admin list: %v", listed)
	}
	bob.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/timeouts", nil)
	bob.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/timeouts/"+wide["id"].(string), nil)
	admin.Must(204, "DELETE", "/api/v1/admin/timeouts/"+wide["id"].(string), nil)
	admin.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/admin/timeouts/"+string(model.NewID[model.AccountID]()), nil)
	bob.Post(other, "free again")

	// Slow mode: managers, bots and webhooks are exempt; edits are not
	// held to it.
	ann.Must(200, "PATCH", conv+other, map[string]any{"slow_mode_seconds": 30})
	first := bob.Post(other, "one")["id"].(string)
	status, hdr, body := bob.DoFull("POST", conv+other+"/messages", map[string]any{"text": "two"})
	e := obj(body, "error")
	if status != 429 || e["code"] != "slow_mode" || e["retry_after_seconds"] == nil || e["retry_after_seconds"].(float64) > 30 || hdr.Get("Retry-After") == "" {
		t.Fatalf("slow mode: %d %v %v", status, body, hdr.Get("Retry-After"))
	}
	bob.Refused(429, model.CodeSlowMode, "POST", conv+other+"/messages", map[string]any{"text": "a reply too", "parent_id": first})
	bob.Must(200, "PATCH", msgs+first, map[string]any{"text": "one, edited"})
	bob.Must(200, "PUT", msgs+first+"/reactions/%F0%9F%91%8D", nil)
	ann.Post(other, "manager one")
	ann.Post(other, "manager two")
	bot := ts.Owner.Bot("robo")
	ann.Add(other, "member", bot)
	bot.Post(other, "bot one")
	bot.Post(other, "bot two")
	ann.Must(200, "PATCH", conv+other, map[string]any{"slow_mode_seconds": 0})
	bob.Post(other, "off again")
}

func TestWordFilter(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	admin := ts.Admin("adm")
	pub := ann.Channel("pub", false, bob)
	priv := ann.Channel("priv", true, bob, admin)
	wf := "/api/v1/admin/word-filters"

	bob.Refused(403, model.CodeForbidden, "GET", wf, nil)
	bob.Refused(403, model.CodeForbidden, "POST", wf, map[string]any{"pattern": "x", "action": "block"})
	admin.Refused(400, model.CodeInvalidValue, "POST", wf, map[string]any{"pattern": "", "action": "block"})
	admin.Refused(400, model.CodeInvalidValue, "POST", wf, map[string]any{"pattern": "x", "action": "shout"})
	// An expression that does not compile is refused when it is saved.
	admin.Refused(400, model.CodeInvalidValue, "POST", wf, map[string]any{"pattern": "a(b", "is_regex": true, "action": "block"})
	block := obj(admin.Must(201, "POST", wf, map[string]any{"pattern": "ass", "action": "block"}), "word_filter")
	if block["is_regex"] != false || block["created_by_id"] != string(admin.ID) {
		t.Fatalf("filter: %v", block)
	}
	flag := obj(admin.Must(201, "POST", wf, map[string]any{"pattern": `h+e+ck\b`, "is_regex": true, "action": "flag"}), "word_filter")
	if list := arr(admin.Must(200, "GET", wf, nil), "word_filters"); len(list) != 2 {
		t.Fatalf("list: %v", list)
	}
	if !auditedFor(t, ts, "word_filter.created", block["id"].(string)) {
		t.Error("changing the list is written to the audit log")
	}

	// A plain pattern matches at word boundaries, without regard to case.
	_, body := bob.Do("POST", conv+pub+"/messages", map[string]any{"text": "what an ASS."})
	if e := obj(body, "error"); e["code"] != "blocked_by_filter" || strings.Contains(e["message"].(string), "ass") {
		t.Fatalf("blocked, without quoting the pattern: %v", body)
	}
	bob.Refused(422, model.CodeBlockedByFilter, "POST", conv+pub+"/messages", map[string]any{"text": "ass"})
	ok := bob.Post(pub, "the class passes")["id"].(string)
	// Edits are checked too.
	bob.Refused(422, model.CodeBlockedByFilter, "PATCH", msgs+ok, map[string]any{"text": "you ass"})

	// A flag lets the message through and files the server's own report:
	// no reporter, source `filter`, the filter's id as the reason.
	adminStream, _ := admin.Events(nil)
	flagged := bob.Post(pub, "oh hheeck")["id"].(string)
	ev := adminStream.Expect("report.created")
	if r := obj(ev, "report"); r["source"] != "filter" || r["reported_by_id"] != nil || r["reason"] != flag["id"] || r["message_id"] != flagged {
		t.Fatalf("report.created for a flag: %v", ev)
	}
	reports := arr(admin.Must(200, "GET", "/api/v1/reports", nil), "reports")
	if len(reports) != 1 || reports[0].(map[string]any)["source"] != "filter" || obj(reports[0].(map[string]any), "message")["text"] != "oh hheeck" {
		t.Fatalf("the flag in the list: %v", reports)
	}
	// One flag per message, however often it matches again.
	setting(t, ts, store.SettingQuickCorrection, 0)
	bob.Must(200, "PATCH", msgs+flagged, map[string]any{"text": "oh heck heck"})
	if reports := arr(admin.Must(200, "GET", "/api/v1/reports", nil), "reports"); len(reports) != 1 {
		t.Fatalf("one flag per message: %d", len(reports))
	}
	// If both kinds match, block wins.
	bob.Refused(422, model.CodeBlockedByFilter, "POST", conv+pub+"/messages", map[string]any{"text": "heck, an ass"})
	// A flag in a private channel reaches its managers only and no admin,
	// not even one who is a member of it.
	bob.Post(priv, "heck in private")
	if got := arr(admin.Must(200, "GET", "/api/v1/reports?conversation_id="+priv, nil), "reports"); len(got) != 0 {
		t.Fatalf("an admin saw a private channel's flag: %v", got)
	}
	if got := arr(ann.Must(200, "GET", "/api/v1/reports?conversation_id="+priv, nil), "reports"); len(got) != 1 {
		t.Fatalf("the manager did not: %v", got)
	}

	// Update and delete.
	upd := obj(admin.Must(200, "PATCH", wf+"/"+block["id"].(string), map[string]any{"action": "flag"}), "word_filter")
	if upd["action"] != "flag" || upd["pattern"] != "ass" {
		t.Fatalf("updated: %v", upd)
	}
	admin.Refused(400, model.CodeInvalidValue, "PATCH", wf+"/"+block["id"].(string), map[string]any{"pattern": "(", "is_regex": true})
	bob.Post(pub, "ass is only flagged now")
	admin.Must(204, "DELETE", wf+"/"+block["id"].(string), nil)
	admin.Refused(404, model.CodeNotFound, "DELETE", wf+"/"+block["id"].(string), nil)
	admin.Refused(404, model.CodeNotFound, "PATCH", wf+"/"+block["id"].(string), map[string]any{"action": "flag"})
	bob.Refused(403, model.CodeForbidden, "DELETE", wf+"/"+flag["id"].(string), nil)
}

// Reports: cases 120 to 123.
func TestReports(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	admin := ts.Admin("adm")
	pub := ann.Channel("pub", false, bob)
	priv := ann.Channel("priv", true, bob, admin)
	chat := mkchat(ann, bob, ts.Owner)
	inPub := bob.Post(pub, "rude in public")["id"].(string)
	inPriv := bob.Post(priv, "rude in private")["id"].(string)
	inChat := bob.Post(chat, "rude in a chat")["id"].(string)
	annStream, _ := ann.Events(nil)
	bobStream, _ := bob.Events(nil)

	// Anyone who can read a public channel can report; 123: someone who
	// cannot read a private one gets not_found.
	id := cat.Must(201, "POST", msgs+inPub+"/report", map[string]any{"reason": "unkind"})["report_id"].(string)
	cat.Refused(409, model.CodeAlreadyExists, "POST", msgs+inPub+"/report", map[string]any{"reason": "again"})
	cat.Refused(404, model.CodeNotFound, "POST", msgs+inPriv+"/report", nil)
	cat.Refused(400, model.CodeInvalidValue, "POST", msgs+inPub+"/report", map[string]any{"reason": strings.Repeat("x", 1001)})
	if e := annStream.Expect("report.created"); obj(e, "report")["id"] != id || obj(e, "report")["reported_by_id"] != string(cat.ID) {
		t.Fatalf("report.created to a handler: %v", e)
	}
	// The author is not told.
	bobStream.ExpectNone(200 * time.Millisecond)
	ann.Must(201, "POST", msgs+inPriv+"/report", nil)
	ann.Must(201, "POST", msgs+inChat+"/report", nil)

	ids := func(c *testkit.Client, query string) map[string]bool {
		out := map[string]bool{}
		for _, r := range arr(c.Must(200, "GET", "/api/v1/reports"+query, nil), "reports") {
			out[r.(map[string]any)["message_id"].(string)] = true
		}
		return out
	}
	// 122: an admin who has not joined PUB sees its report. 120: an admin
	// who is a member but not a manager of PRIV does not see PRIV's.
	// 121: the owner, in the ad-hoc channel as a member, does not see its.
	if got := ids(admin, ""); !got[inPub] || got[inPriv] || got[inChat] {
		t.Fatalf("120, 122: the admin's list: %v", got)
	}
	if got := ids(ts.Owner, ""); !got[inPub] || got[inPriv] || got[inChat] {
		t.Fatalf("121: the owner's list: %v", got)
	}
	if got := ids(ann, "?waiting=true"); len(got) != 3 {
		t.Fatalf("the manager of all three: %v", got)
	}
	if got := ids(bob, ""); len(got) != 0 {
		t.Fatalf("a member handles nothing: %v", got)
	}
	if got := ids(ann, "?conversation_id="+priv); len(got) != 1 || !got[inPriv] {
		t.Fatalf("by conversation: %v", got)
	}
	// A report about a handler's own message is left out if another
	// handler exists.
	annMsg := ann.Post(pub, "the manager's own")["id"].(string)
	cat.Must(201, "POST", msgs+annMsg+"/report", nil)
	if got := ids(ann, ""); got[annMsg] {
		t.Fatal("a handler saw the report about their own message")
	}
	if got := ids(admin, ""); !got[annMsg] {
		t.Fatal("another handler did not")
	}

	// Handling.
	privID := ""
	for _, r := range arr(ann.Must(200, "GET", "/api/v1/reports?conversation_id="+priv, nil), "reports") {
		privID = r.(map[string]any)["id"].(string)
	}
	// Not for someone who may not handle it: the report does not exist.
	bob.Refused(404, model.CodeNotFound, "POST", "/api/v1/reports/"+id+"/handle", map[string]any{"outcome": "dismissed"})
	admin.Refused(404, model.CodeNotFound, "POST", "/api/v1/reports/"+privID+"/handle", map[string]any{"outcome": "dismissed"})
	ann.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/reports/"+id+"/handle", map[string]any{"outcome": "applaud"})
	ann.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/reports/"+id+"/handle", map[string]any{"outcome": "timed_out"})
	// `deleted` deletes the message, by the handler.
	done := obj(admin.Must(200, "POST", "/api/v1/reports/"+id+"/handle", map[string]any{"outcome": "deleted"}), "report")
	if done["outcome"] != "deleted" || done["handled_by_id"] != string(admin.ID) {
		t.Fatalf("handled: %v", done)
	}
	if m := obj(bob.Must(200, "GET", msgs+inPub, nil), "message"); m["deleted_by_id"] != string(admin.ID) {
		t.Fatalf("the reported message: %v", m)
	}
	annStream.Expect("report.handled")
	admin.Refused(409, model.CodeConflict, "POST", "/api/v1/reports/"+id+"/handle", map[string]any{"outcome": "dismissed"})
	if !auditedFor(t, ts, "report.handled", inPub) {
		t.Error("handling is written to the audit log")
	}
	// `timed_out` times the author out in that channel.
	ann.Must(200, "POST", "/api/v1/reports/"+privID+"/handle", map[string]any{"outcome": "timed_out", "timeout_seconds": 600})
	bob.Refused(403, model.CodeTimedOut, "POST", conv+priv+"/messages", map[string]any{"text": "x"})
	// Not for an ad-hoc channel in the audit log.
	for _, r := range arr(ann.Must(200, "GET", "/api/v1/reports?conversation_id="+chat, nil), "reports") {
		ann.Must(200, "POST", "/api/v1/reports/"+r.(map[string]any)["id"].(string)+"/handle", map[string]any{"outcome": "dismissed"})
	}
	if auditedFor(t, ts, "report.handled", inChat) {
		t.Error("handling in an ad-hoc channel is not written to the audit log")
	}
	if got := ids(ann, "?waiting=true"); len(got) != 0 {
		t.Fatalf("nothing waits for ann: %v", got)
	}
}

// Purge: cases 111 to 118.
func TestPurge(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	admin := ts.Admin("adm")
	pub := ann.Channel("pub", false, bob)
	priv := ann.Channel("priv", true, bob)
	setting(t, ts, store.SettingQuickCorrection, 0)
	edit := func(c *testkit.Client, channel string) string {
		id := c.Post(channel, "the secret first version")["id"].(string)
		// With the quick-correction window at 0 an edit in the very same
		// millisecond would still be a correction and keep no history.
		time.Sleep(3 * time.Millisecond)
		c.Must(200, "PATCH", msgs+id, map[string]any{"text": "the second version"})
		return id
	}
	history := func(c *testkit.Client, id string) (int, []any) {
		status, out := c.Do("GET", msgs+id+"/history", nil)
		return status, arr(out, "versions")
	}

	// Nothing to purge.
	plain := bob.Post(pub, "never edited")["id"].(string)
	bob.Refused(409, model.CodeConflict, "POST", msgs+plain+"/purge", nil)
	// 117: only the author may ask.
	theirs := edit(bob, pub)
	ann.Refused(403, model.CodeForbidden, "POST", msgs+theirs+"/purge", nil)
	ts.Member("out").Refused(404, model.CodeNotFound, "POST", msgs+edit(bob, priv)+"/purge", nil)

	// 111: in a private channel the history is erased at once; no request
	// is stored and nothing is written to the audit log.
	p := edit(bob, priv)
	annStream, _ := ann.Events(nil)
	out := bob.Must(200, "POST", msgs+p+"/purge", nil)
	if out["state"] != "purged" || obj(out, "message")["text"] != "the second version" || obj(out, "message")["has_history"] != nil {
		t.Fatalf("111: %v", out)
	}
	if e := annStream.Expect("message.purged"); e["erased"] != false || obj(e, "message")["id"] != p {
		t.Fatalf("message.purged: %v", e)
	}
	if _, v := history(ann, p); len(v) != 0 {
		t.Fatalf("111: history after a purge: %v", v)
	}
	if reqs := arr(admin.Must(200, "GET", "/api/v1/admin/purge-requests", nil), "purge_requests"); len(reqs) != 0 {
		t.Fatalf("111: a request was stored: %v", reqs)
	}
	if auditedFor(t, ts, "message.purged", p) {
		t.Error("111: an author's purge in a private channel was audited")
	}
	// A deleted message becomes a bare record.
	gone := edit(bob, priv)
	bob.Must(200, "DELETE", msgs+gone, nil)
	if out := bob.Must(200, "POST", msgs+gone+"/purge", nil); out["state"] != "purged" {
		t.Fatalf("purge of a deleted message: %v", out)
	}
	if e := annStream.Expect("message.deleted"); e == nil {
		t.Fatal("no message.deleted")
	}
	if e := annStream.Expect("message.purged"); e["erased"] != true {
		t.Fatalf("erased: %v", e)
	}
	bob.Refused(409, model.CodeConflict, "POST", msgs+gone+"/purge", nil)

	// 112: in a public channel the history is hidden from everyone but
	// admins at once, and a request waits.
	adminStream, _ := admin.Events(nil)
	q := edit(bob, pub)
	if s, v := history(ann, q); s != 200 || len(v) != 1 {
		t.Fatalf("history before: %d %v", s, v)
	}
	out = bob.Must(200, "POST", msgs+q+"/purge", nil)
	if out["state"] != "requested" || out["outcome"] != nil || obj(out, "message")["has_history"] != nil {
		t.Fatalf("112: %v", out)
	}
	req := obj(adminStream.Expect("purge_request.created"), "purge_request")
	if req["message_id"] != q || req["requested_by_id"] != string(bob.ID) || req["outcome"] != nil {
		t.Fatalf("purge_request.created: %v", req)
	}
	if s, _ := history(ann, q); s != 403 {
		t.Fatalf("112: a non-admin opening hidden history: %d", s)
	}
	// 113: nor the author.
	if s, _ := history(bob, q); s != 403 {
		t.Fatalf("113: the author opening hidden history: %d", s)
	}
	if s, v := history(admin, q); s != 200 || len(v) != 1 {
		t.Fatalf("112: an admin still can: %d %v", s, v)
	}
	// Asking twice returns the waiting request.
	if out := bob.Must(200, "POST", msgs+q+"/purge", nil); out["state"] != "requested" {
		t.Fatalf("asking twice: %v", out)
	}
	// 114: a non-admin cannot list or decide.
	bob.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/purge-requests", nil)
	bob.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/purge-requests/"+req["id"].(string)+"/decide", map[string]any{"outcome": "erased"})
	waiting := arr(admin.Must(200, "GET", "/api/v1/admin/purge-requests?waiting=true", nil), "purge_requests")
	if len(waiting) != 1 || obj(waiting[0].(map[string]any), "message")["id"] != q {
		t.Fatalf("waiting: %v", waiting)
	}
	decide := func(id, outcome string) map[string]any {
		return obj(admin.Must(200, "POST", "/api/v1/admin/purge-requests/"+id+"/decide", map[string]any{"outcome": outcome}), "purge_request")
	}
	admin.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/purge-requests/"+req["id"].(string)+"/decide", map[string]any{"outcome": "maybe"})

	// Show again: the history is back for everyone; 116: the author
	// cannot ask again until the message changes (ruling R38).
	bobStream, _ := bob.Events(nil)
	if d := decide(req["id"].(string), "shown"); d["outcome"] != "shown" || d["decided_by_id"] != string(admin.ID) {
		t.Fatalf("decided: %v", d)
	}
	bobStream.Expect("message.history_restored")
	adminStream.Expect("purge_request.decided")
	admin.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/purge-requests/"+req["id"].(string)+"/decide", map[string]any{"outcome": "erased"})
	if s, v := history(ann, q); s != 200 || len(v) != 1 {
		t.Fatalf("shown again: %d %v", s, v)
	}
	// 115: the author is never given the outcome: the refusal says only
	// that a purge was already asked for.
	_, again := bob.Do("POST", msgs+q+"/purge", nil)
	if e := obj(again, "error"); e["code"] != "conflict" || strings.Contains(e["message"].(string), "shown") || again["outcome"] != nil {
		t.Fatalf("116: %v", again)
	}
	bob.Must(200, "PATCH", msgs+q, map[string]any{"text": "the third version"})
	if out := bob.Must(200, "POST", msgs+q+"/purge", nil); out["state"] != "requested" {
		t.Fatalf("after an edit the author may ask again: %v", out)
	}
	req2 := obj(adminStream.Expect("purge_request.created"), "purge_request")["id"].(string)

	// Keep hidden: nothing changes, and no event tells the channel.
	bobStream, _ = bob.Events(nil)
	decide(req2, "hidden")
	bobStream.ExpectNone(200 * time.Millisecond)
	if s, _ := history(bob, q); s != 403 {
		t.Fatalf("kept hidden: %d", s)
	}
	if s, v := history(admin, q); s != 200 || len(v) != 2 {
		t.Fatalf("kept for admins: %d %v", s, v)
	}

	// Erase: carried out, with no further event to the channel. 118:
	// nobody, the owner included, can open the history or find its text.
	r := edit(bob, pub)
	bob.Must(200, "POST", msgs+r+"/purge", nil)
	req3 := obj(adminStream.Expect("purge_request.created"), "purge_request")["id"].(string)
	bobStream, _ = bob.Events(nil)
	decide(req3, "erased")
	bobStream.ExpectNone(200 * time.Millisecond)
	if s, v := history(ts.Owner, r); s != 200 || len(v) != 0 {
		t.Fatalf("118: history after erasure: %d %v", s, v)
	}
	// History is never searched, and the word was only ever in history.
	if hits := arr(ts.Owner.Must(200, "GET", "/api/v1/search?q=secret&in="+pub, nil), "results"); len(hits) != 0 {
		t.Fatalf("118: the text of an earlier version was found: %v", hits)
	}
	events, _, _ := ts.Server.Store.Events().List(context.Background(), model.ConversationID(pub), 0, 0, 200)
	for _, e := range events {
		if e.MessageID == model.MessageID(r) && e.Type == model.EventMessageEdited {
			t.Error("118: an event carrying an earlier version is still in the log")
		}
	}
	if !auditedFor(t, ts, "purge_request.decided", r) {
		t.Error("a decision is written to the audit log")
	}

	// An admin purges someone else's message directly: deleted and purged
	// in one step, one audit row, no request.
	victim := edit(bob, pub)
	out = admin.Must(200, "POST", msgs+victim+"/purge", nil)
	if out["state"] != "purged" || obj(out, "message")["deleted_at"] == nil || obj(out, "message")["text"] != nil {
		t.Fatalf("an admin's purge: %v", out)
	}
	if !auditedFor(t, ts, "message.purged", victim) {
		t.Error("an admin's purge is written to the audit log")
	}
	// The same through delete with purge=true; only an admin may pass it.
	v2 := edit(bob, pub)
	ann.Refused(403, model.CodeForbidden, "DELETE", msgs+v2+"?purge=true", nil)
	bob.Refused(403, model.CodeForbidden, "DELETE", msgs+v2+"?purge=true", nil)
	if m := obj(admin.Must(200, "DELETE", msgs+v2+"?purge=true", nil), "message"); m["deleted_at"] == nil {
		t.Fatalf("delete and purge: %v", m)
	}
	if _, v := history(admin, v2); len(v) != 0 {
		t.Fatalf("history after delete and purge: %v", v)
	}
	// Not in a private channel the admin is not in.
	admin.Refused(404, model.CodeNotFound, "POST", msgs+edit(bob, priv)+"/purge", nil)
}
