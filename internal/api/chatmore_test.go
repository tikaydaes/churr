package api_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Tests of the rest of conversations: settings, privacy, the lock,
// archiving, levels, groups, pins, sub-channels and moving a thread out.
// Numbers in comments are the cases of docs/PERMISSIONS.md section 17.

const conv = "/api/v1/conversations/"

func convOf(out map[string]any) map[string]any { return obj(out, "conversation") }

// auditedFor reports whether the audit log has an entry of an action for a
// target.
func auditedFor(t *testing.T, ts *testkit.TestServer, action, target string) bool {
	t.Helper()
	list, _, err := ts.Server.Store.Audit().List(context.Background(), store.AuditFilter{Action: action, TargetID: target}, store.Page{})
	if err != nil {
		t.Fatal(err)
	}
	return len(list) > 0
}

func setting(t *testing.T, ts *testkit.TestServer, key string, value any) {
	t.Helper()
	if err := ts.Server.Store.Settings().Set(context.Background(), key, value, ""); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateConversation(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, eve := ts.Member("ann"), ts.Member("bob"), ts.Member("eve")
	admin := ts.Admin("adm")
	ch := ann.Channel("ops", false, bob)
	priv := ann.Channel("plans", true)
	stream, _ := bob.Events(nil)

	sec := obj(ts.Owner.Must(201, "POST", "/api/v1/admin/sections", map[string]any{"name": "Work"}), "section")
	out := convOf(ann.Must(200, "PATCH", conv+ch, map[string]any{
		"name": "  Ops   Room ", "topic": "Deploys", "welcome": "Hello", "default_level": "reader",
		"slow_mode_seconds": 10, "section_id": sec["id"], "retention_seconds": 86400}))
	if out["name"] != "Ops Room" || out["topic"] != "Deploys" || out["welcome"] != "Hello" || out["default_level"] != "reader" ||
		out["slow_mode_seconds"] != float64(10) || out["section_id"] != sec["id"] || out["retention_seconds"] != float64(86400) {
		t.Fatalf("updated: %v", out)
	}
	e := stream.Expect("conversation.updated")
	if obj(e, "conversation")["name"] != "Ops Room" || e["by_id"] != string(ann.ID) {
		t.Fatalf("conversation.updated: %v", e)
	}
	if !auditedFor(t, ts, "channel.retention_changed", ch) {
		t.Error("changing a retention period is written to the audit log")
	}
	// 0 clears the channel's own period; null clears the section.
	out = convOf(ann.Must(200, "PATCH", conv+ch, map[string]any{"retention_seconds": 0, "section_id": nil}))
	if _, has := out["retention_seconds"]; has || out["section_id"] != nil {
		t.Fatalf("cleared: %v", out)
	}
	// 238: renaming to a name another channel has, public or private.
	ann.Must(200, "PATCH", conv+ch, map[string]any{"name": "plans"})

	// Refusals.
	bob.Refused(403, model.CodeForbidden, "PATCH", conv+ch, map[string]any{"topic": "x"})
	eve.Refused(404, model.CodeNotFound, "PATCH", conv+priv, map[string]any{"topic": "x"})
	admin.Refused(404, model.CodeNotFound, "PATCH", conv+priv, map[string]any{"topic": "x"})
	// An admin is no manager of a channel (ruling R99).
	admin.Refused(403, model.CodeForbidden, "PATCH", conv+ch, map[string]any{"topic": "admins are not managers"})
	for field, bad := range map[string]any{"name": "", "topic": strings.Repeat("x", 251), "default_level": "manager",
		"slow_mode_seconds": 3, "section_id": string(model.NewID[model.SectionID]()), "retention_seconds": -1} {
		ann.Refused(400, model.CodeInvalidValue, "PATCH", conv+ch, map[string]any{field: bad})
	}
	ann.Refused(400, model.CodeInvalidRequest, "PATCH", conv+ch, map[string]any{"nonsense": 1})
	// A manager may only shorten retention; an admin may set any, from the
	// console, or here as one of its managers.
	setting(t, ts, store.SettingRetention, 7*86400)
	ann.Refused(403, model.CodeForbidden, "PATCH", conv+ch, map[string]any{"retention_seconds": 30 * 86400})
	ann.Must(200, "PATCH", conv+ch, map[string]any{"retention_seconds": 86400})
	admin.Must(200, "PATCH", "/api/v1/admin/channels/"+ch, map[string]any{"retention_seconds": 30 * 86400})
	ann.Must(200, "POST", conv+ch+"/members", map[string]any{"account_ids": []string{string(admin.ID)}, "level": "manager"})
	admin.Must(200, "PATCH", conv+ch, map[string]any{"retention_seconds": 30 * 86400})

	// Naming an ad-hoc channel makes it a named private channel.
	chat := convOf(ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID)}}))["id"].(string)
	ann.Refused(403, model.CodeForbidden, "PATCH", conv+chat, map[string]any{"welcome": "named channels only"})
	named := convOf(ann.Must(200, "PATCH", conv+chat, map[string]any{"name": "was a chat"}))
	if named["name"] != "was a chat" || named["private"] != true {
		t.Fatalf("named: %v", named)
	}
	ann.Must(200, "PATCH", conv+chat, map[string]any{"welcome": "now it is named"})
}

func TestMakePrivate(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, reader := ts.Member("ann"), ts.Member("bob"), ts.Member("reader")
	admin := ts.Admin("adm")
	ch := ann.Channel("pub", false, bob)
	msg := ann.Post(ch, "a rareword in public")["id"].(string)
	file := ann.Upload(ch, "note.txt", []byte("a note\n"))
	withFile := obj(ann.Must(201, "POST", conv+ch+"/messages", map[string]any{"text": "file", "file_ids": []any{file["id"]}}), "message")
	fileURL := arr(withFile, "files")[0].(map[string]any)["url"].(string)
	// A purge request waits in the channel (164).
	edited := ann.Post(ch, "first version")["id"].(string)
	setting(t, ts, store.SettingQuickCorrection, 0)
	ann.Must(200, "PATCH", "/api/v1/messages/"+edited, map[string]any{"text": "second version"})
	if out := ann.Must(200, "POST", "/api/v1/messages/"+edited+"/purge", nil); out["state"] != "requested" {
		t.Fatalf("purge in a public channel: %v", out)
	}
	// The reader reads it without joining, and bookmarks nothing.
	reader.Must(200, "GET", conv+ch+"/messages", nil)
	if s, _, _ := reader.Get(fileURL, nil); s != 200 {
		t.Fatalf("a member downloads from a public channel: %d", s)
	}

	// 167: a member-level account cannot; 168: not while archived.
	bob.Refused(403, model.CodeForbidden, "PATCH", conv+ch, map[string]any{"private": true})
	ann.Must(200, "POST", conv+ch+"/archive", nil)
	ann.Refused(403, model.CodeArchived, "PATCH", conv+ch, map[string]any{"private": true})
	ann.Must(200, "DELETE", conv+ch+"/archive", nil)

	stream, _ := bob.Events(nil)
	// 157: a manager makes it private; it is in the audit log.
	out := convOf(ann.Must(200, "PATCH", conv+ch, map[string]any{"private": true}))
	if out["private"] != true || !auditedFor(t, ts, "channel.made_private", ch) {
		t.Fatalf("made private: %v", out)
	}
	// message.purged for the request carried out, then conversation.updated.
	if e := stream.Expect("message.purged"); obj(e, "message")["id"] != edited {
		t.Fatalf("message.purged: %v", e)
	}
	stream.Expect("conversation.updated")

	// 159: everything answers not_found for someone who was only reading.
	reader.Refused(404, model.CodeNotFound, "GET", conv+ch, nil)
	reader.Refused(404, model.CodeNotFound, "GET", conv+ch+"/messages", nil)
	reader.Refused(404, model.CodeNotFound, "GET", "/api/v1/messages/"+msg+"/history", nil)
	if s, _, _ := reader.Get(fileURL, nil); s != 404 {
		t.Fatalf("159: a file after the channel went private: %d", s)
	}
	// 160: search finds nothing; 162: joining is not_found.
	if hits := arr(reader.Must(200, "GET", "/api/v1/search?q=rareword", nil), "results"); len(hits) != 0 {
		t.Fatalf("160: %v", hits)
	}
	reader.Refused(404, model.CodeNotFound, "POST", conv+ch+"/join", nil)
	// 163: a member reads and searches as before, at the same level.
	bob.Must(200, "GET", conv+ch+"/messages", nil)
	if hits := arr(bob.Must(200, "GET", "/api/v1/search?q=rareword", nil), "results"); len(hits) != 1 {
		t.Fatalf("163: %v", hits)
	}
	// 164: the waiting request was carried out as an erasure; nobody can
	// open the history, the owner included, and no request waits.
	if v := arr(ann.Must(200, "GET", "/api/v1/messages/"+edited+"/history", nil), "versions"); len(v) != 0 {
		t.Fatalf("164: history after erasure: %v", v)
	}
	reqs := arr(ts.Owner.Must(200, "GET", "/api/v1/admin/purge-requests", nil), "purge_requests")
	if len(reqs) != 1 || reqs[0].(map[string]any)["outcome"] != "erased" || reqs[0].(map[string]any)["decided_by_id"] != nil {
		t.Fatalf("164: the request: %v", reqs)
	}
	// 165: never back, for a manager, an admin and the owner.
	ann.Refused(403, model.CodeForbidden, "PATCH", conv+ch, map[string]any{"private": false})
	ann.Add(ch, "member", admin)
	admin.Refused(403, model.CodeForbidden, "PATCH", conv+ch, map[string]any{"private": false})
	ann.Add(ch, "member", ts.Owner)
	ts.Owner.Refused(403, model.CodeForbidden, "PATCH", conv+ch, map[string]any{"private": false})
	// 166: an ad-hoc channel is always private.
	chat := convOf(ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID)}}))["id"].(string)
	ann.Refused(403, model.CodeForbidden, "PATCH", conv+chat, map[string]any{"private": false})

	// 158: an admin who has not joined cannot make a channel private: an
	// admin is no manager (ruling R99).
	other := ann.Channel("other", false)
	admin.Refused(403, model.CodeForbidden, "PATCH", conv+other, map[string]any{"private": true})
}

func TestLocking(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("cat"), ts.Member("dan")
	admin := ts.Admin("adm")
	priv := ann.Channel("priv", true, bob)
	pub := ann.Channel("pub", false)

	// A group brings cat in.
	group := obj(ts.Owner.Must(201, "POST", "/api/v1/admin/groups", map[string]any{"handle": "crew", "name": "Crew"}), "group")["id"].(string)
	ts.Owner.Must(204, "PUT", "/api/v1/admin/groups/"+group+"/members/"+string(cat.ID), nil)
	ann.Must(200, "PUT", conv+priv+"/groups/"+group, map[string]any{"level": "member"})

	// 171: a member-level account cannot lock; 173: not a public channel.
	bob.Refused(403, model.CodeForbidden, "POST", conv+priv+"/lock", nil)
	ann.Refused(409, model.CodeConflict, "POST", conv+pub+"/lock", nil)
	admin.Refused(404, model.CodeNotFound, "POST", conv+priv+"/lock", nil)

	stream, _ := bob.Events(nil)
	// 169: a manager locks it and locked_at is set.
	out := convOf(ann.Must(200, "POST", conv+priv+"/lock", nil))
	if out["locked_at"] == nil || !auditedFor(t, ts, "channel.locked", priv) {
		t.Fatalf("169: %v", out)
	}
	if e := stream.Expect("conversation.updated"); obj(e, "conversation")["locked_at"] == nil {
		t.Fatalf("conversation.updated on lock: %v", e)
	}
	// Locking again changes nothing.
	ann.Must(200, "POST", conv+priv+"/lock", nil)

	// 181: the group's members are still members at the same level, and
	// the channel has no group.
	if g := arr(ann.Must(200, "GET", conv+priv+"/groups", nil), "groups"); len(g) != 0 {
		t.Fatalf("181: groups after locking: %v", g)
	}
	members := arr(ann.Must(200, "GET", conv+priv+"/members", nil), "members")
	var catRow map[string]any
	for _, m := range members {
		if m.(map[string]any)["account_id"] == string(cat.ID) {
			catRow = m.(map[string]any)
		}
	}
	if catRow == nil || catRow["level"] != "member" || catRow["via_group"] != false {
		t.Fatalf("181: cat after locking: %v", catRow)
	}
	// 182: someone who joins the group afterwards is not in the channel.
	ts.Owner.Must(204, "PUT", "/api/v1/admin/groups/"+group+"/members/"+string(dan.ID), nil)
	dan.Refused(404, model.CodeNotFound, "GET", conv+priv, nil)
	// 183: leaving the group no longer takes cat out.
	ts.Owner.Must(204, "DELETE", "/api/v1/admin/groups/"+group+"/members/"+string(cat.ID), nil)
	cat.Must(200, "GET", conv+priv, nil)

	// 174, 176, 177: nobody can be added, by anyone.
	ann.Refused(403, model.CodeLocked, "POST", conv+priv+"/members", map[string]any{"account_ids": []string{string(dan.ID)}})
	ann.Refused(403, model.CodeLocked, "PUT", conv+priv+"/groups/"+group, map[string]any{"level": "member"})
	// 188: everything else works as before.
	m := bob.Post(priv, "still posting")["id"].(string)
	bob.Must(200, "PUT", "/api/v1/messages/"+m+"/reactions/"+"%F0%9F%91%8D", nil)
	ann.Must(204, "PUT", conv+priv+"/pins/"+m, nil)
	// 184, 185: cat leaves like anyone else and cannot come back.
	cat.Must(204, "POST", conv+priv+"/leave", nil)
	ann.Refused(403, model.CodeLocked, "POST", conv+priv+"/members", map[string]any{"account_ids": []string{string(cat.ID)}})
	// 186: no operation unlocks; nothing in an update clears the lock.
	ann.Refused(400, model.CodeInvalidRequest, "PATCH", conv+priv, map[string]any{"locked_at": nil})

	// 170, 175: an ad-hoc channel, from inside.
	chat := convOf(ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID)}}))["id"].(string)
	bob.Refused(403, model.CodeForbidden, "POST", conv+chat+"/lock", nil)
	ann.Must(200, "POST", conv+chat+"/lock", nil)
	bob.Refused(403, model.CodeLocked, "POST", conv+chat+"/members", map[string]any{"account_ids": []string{string(dan.ID)}})
	if auditedFor(t, ts, "channel.locked", chat) {
		t.Error("nothing done in an ad-hoc channel is written to the audit log")
	}

	// 172: the admin console locks nothing (ruling R99); a moderator
	// locks from inside.
	second := ann.Channel("second", true, cat)
	admin.Refused(404, model.CodeNotFound, "POST", "/api/v1/admin/channels/"+second+"/lock", nil)
	cat.Refused(403, model.CodeForbidden, "POST", conv+second+"/lock", nil)
	ann.Must(200, "PATCH", conv+second+"/members/"+string(cat.ID), map[string]any{"level": "moderator"})
	cat.Must(200, "POST", conv+second+"/lock", nil)
	if !auditedFor(t, ts, "channel.locked", second) {
		t.Error("172: a moderator's lock is in the audit log")
	}
	// An archived channel is not locked.
	third := ann.Channel("third", true)
	ann.Must(200, "POST", conv+third+"/archive", nil)
	ann.Refused(403, model.CodeArchived, "POST", conv+third+"/lock", nil)

	// 187: when the last member of a locked channel leaves it is deleted
	// with its content (ruling R21).
	bob.Must(204, "POST", conv+priv+"/leave", nil)
	ann.Must(204, "POST", conv+priv+"/leave", nil)
	if _, err := ts.Server.Store.Conversations().Get(context.Background(), model.ConversationID(priv)); err == nil {
		t.Error("187: the channel outlived its last member")
	}
	if _, err := ts.Server.Store.Messages().Get(context.Background(), model.MessageID(m)); err == nil {
		t.Error("187: a message outlived its channel")
	}
}

func TestArchiveLevelsGroupsPins(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	admin := ts.Admin("adm")
	ch := ann.Channel("room", false, bob)
	stream, _ := bob.Events(nil)

	// Archive and unarchive.
	bob.Refused(403, model.CodeForbidden, "POST", conv+ch+"/archive", nil)
	cat.Refused(403, model.CodeForbidden, "POST", conv+ch+"/archive", nil)
	if out := convOf(ann.Must(200, "POST", conv+ch+"/archive", nil)); out["archived_at"] == nil {
		t.Fatalf("archived: %v", out)
	}
	stream.Expect("conversation.archived")
	bob.Refused(403, model.CodeArchived, "POST", conv+ch+"/messages", map[string]any{"text": "frozen"})
	bob.Must(200, "GET", conv+ch+"/messages", nil)
	if out := convOf(ann.Must(200, "DELETE", conv+ch+"/archive", nil)); out["archived_at"] != nil {
		t.Fatalf("unarchived: %v", out)
	}
	stream.Expect("conversation.unarchived")
	ts.Member("out").Refused(404, model.CodeNotFound, "POST", conv+ann.Channel("hidden", true)+"/archive", nil)

	// Levels.
	member := obj(ann.Must(200, "PATCH", conv+ch+"/members/"+string(bob.ID), map[string]any{"level": "reader"}), "member")
	if member["level"] != "reader" {
		t.Fatalf("level: %v", member)
	}
	if e := stream.Expect("member.level_changed"); obj(e, "member")["level"] != "reader" {
		t.Fatalf("member.level_changed: %v", e)
	}
	bob.Refused(403, model.CodeForbidden, "POST", conv+ch+"/messages", map[string]any{"text": "readers cannot post"})
	bob.Refused(403, model.CodeForbidden, "PATCH", conv+ch+"/members/"+string(ann.ID), map[string]any{"level": "reader"})
	ann.Refused(400, model.CodeInvalidValue, "PATCH", conv+ch+"/members/"+string(bob.ID), map[string]any{"level": "boss"})
	ann.Refused(404, model.CodeNotFound, "PATCH", conv+ch+"/members/"+string(cat.ID), map[string]any{"level": "member"})
	// A channel's last manager cannot lower their own level.
	ann.Refused(409, model.CodeConflict, "PATCH", conv+ch+"/members/"+string(ann.ID), map[string]any{"level": "member"})
	ann.Must(200, "PATCH", conv+ch+"/members/"+string(bob.ID), map[string]any{"level": "member"})
	stream.Expect("member.level_changed")

	// Groups: add, change, list, remove.
	group := obj(ts.Owner.Must(201, "POST", "/api/v1/admin/groups", map[string]any{"handle": "crew", "name": "Crew"}), "group")["id"].(string)
	ts.Owner.Must(204, "PUT", "/api/v1/admin/groups/"+group+"/members/"+string(cat.ID), nil)
	catStream, _ := cat.Events(nil)
	bob.Refused(403, model.CodeForbidden, "PUT", conv+ch+"/groups/"+group, map[string]any{"level": "reader"})
	added := obj(ann.Must(200, "PUT", conv+ch+"/groups/"+group, map[string]any{"level": "reader"}), "group")
	if added["group_id"] != group || added["level"] != "reader" || added["added_by_id"] != string(ann.ID) {
		t.Fatalf("group added: %v", added)
	}
	stream.Expect("channel_group.added")
	// The person brought in gets the event with the conversation beside it.
	if e := catStream.Expect("channel_group.added"); obj(e, "conversation", "membership")["level"] != "reader" {
		t.Fatalf("the event for someone the group brought in: %v", e)
	}
	ann.Must(200, "PUT", conv+ch+"/groups/"+group, map[string]any{"level": "member"})
	stream.Expect("channel_group.changed")
	if g := arr(bob.Must(200, "GET", conv+ch+"/groups", nil), "groups"); len(g) != 1 || g[0].(map[string]any)["level"] != "member" {
		t.Fatalf("groups: %v", g)
	}
	// In through a group: cannot be removed one at a time.
	ann.Refused(409, model.CodeConflict, "DELETE", conv+ch+"/members/"+string(cat.ID), nil)
	ann.Refused(404, model.CodeNotFound, "PUT", conv+ch+"/groups/"+string(model.NewID[model.GroupID]()), map[string]any{"level": "member"})
	chat := convOf(ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID)}}))["id"].(string)
	ann.Refused(403, model.CodeForbidden, "PUT", conv+chat+"/groups/"+group, map[string]any{"level": "member"})
	ann.Must(204, "DELETE", conv+ch+"/groups/"+group, nil)
	stream.Expect("channel_group.removed")
	if e := catStream.Expect("member.left"); e["reason"] != "group" {
		t.Fatalf("member.left for someone a group took out: %v", e)
	}
	ann.Refused(404, model.CodeNotFound, "DELETE", conv+ch+"/groups/"+group, nil)

	// Removal.
	ann.Add(ch, "member", admin)
	stream.Expect("member.joined")
	bob.Refused(403, model.CodeForbidden, "DELETE", conv+ch+"/members/"+string(ann.ID), nil)
	ann.Refused(403, model.CodeForbidden, "DELETE", conv+ch+"/members/"+string(ann.ID), nil)
	// An admin is removed like anyone (ruling R99); an owner is not.
	ann.Must(204, "DELETE", conv+ch+"/members/"+string(admin.ID), nil)
	stream.Expect("member.left")
	ann.Add(ch, "member", ts.Owner)
	stream.Expect("member.joined")
	ann.Refused(403, model.CodeForbidden, "DELETE", conv+ch+"/members/"+string(ts.Owner.ID), nil)
	ann.Must(204, "DELETE", conv+ch+"/members/"+string(bob.ID), nil)
	if e := stream.Expect("member.left"); e["reason"] != "removed" || e["by_id"] != string(ann.ID) {
		t.Fatalf("member.left: %v", e)
	}
	ann.Refused(404, model.CodeNotFound, "DELETE", conv+ch+"/members/"+string(bob.ID), nil)
	ann.Add(ch, "member", bob)

	// Pins.
	stream, _ = bob.Events(nil)
	m := bob.Post(ch, "pin me")["id"].(string)
	bob.Refused(403, model.CodeForbidden, "PUT", conv+ch+"/pins/"+m, nil)
	ann.Must(204, "PUT", conv+ch+"/pins/"+m, nil)
	stream.Expect("message.created")
	if e := stream.Expect("pin.added"); obj(e, "pin")["message_id"] != m || obj(e, "pin")["pinned_by_id"] != string(ann.ID) {
		t.Fatalf("pin.added: %v", e)
	}
	if e := stream.Expect("message.created"); obj(e, "message", "system")["kind"] != "pinned" {
		t.Fatalf("the pinned notice: %v", e)
	}
	ann.Must(204, "PUT", conv+ch+"/pins/"+m, nil) // already pinned: nothing more
	pins := arr(bob.Must(200, "GET", conv+ch+"/pins", nil), "pins")
	if len(pins) != 1 || obj(pins[0].(map[string]any), "message")["text"] != "pin me" || obj(pins[0].(map[string]any), "message")["pinned"] != true {
		t.Fatalf("pins: %v", pins)
	}
	other := ann.Post(ann.Channel("elsewhere", false), "not here")["id"].(string)
	ann.Refused(404, model.CodeNotFound, "PUT", conv+ch+"/pins/"+other, nil)
	gone := bob.Post(ch, "to delete")["id"].(string)
	bob.Must(200, "DELETE", "/api/v1/messages/"+gone, nil)
	ann.Refused(404, model.CodeNotFound, "PUT", conv+ch+"/pins/"+gone, nil) // deleted outright inside the window
	bob.Refused(403, model.CodeForbidden, "DELETE", conv+ch+"/pins/"+m, nil)
	ann.Must(204, "DELETE", conv+ch+"/pins/"+m, nil)
	if e := stream.Expect("pin.removed"); e["message_id"] != m {
		t.Fatalf("pin.removed: %v", e)
	}
	ann.Refused(404, model.CodeNotFound, "DELETE", conv+ch+"/pins/"+m, nil)
	// Deleting a pinned message unpins it.
	ann.Must(204, "PUT", conv+ch+"/pins/"+m, nil)
	setting(t, ts, store.SettingQuickCorrection, 0)
	bob.Must(200, "DELETE", "/api/v1/messages/"+m, nil)
	if pins := arr(bob.Must(200, "GET", conv+ch+"/pins", nil), "pins"); len(pins) != 0 {
		t.Fatalf("pins after the message was deleted: %v", pins)
	}
}
