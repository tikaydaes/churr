package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Tests of the people area: groups, blocks, profile, preferences, status and
// do not disturb, modes and badges, thread states, bookmarks, notify anyway,
// and what admins do to accounts. Numbers in comments are the cases of
// docs/PERMISSIONS.md section 17.

var bg = context.Background()

func str(m map[string]any, key string) string { s, _ := m[key].(string); return s }

func hasID(list []any, id any) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// membershipOf fetches the caller's membership of a conversation.
func membershipOf(c *testkit.Client, conv string) map[string]any {
	return obj(c.Must(200, "GET", "/api/v1/conversations/"+conv, nil), "conversation", "membership")
}

// setMode sets the caller's mode in a conversation.
func setMode(c *testkit.Client, conv, mode string) map[string]any {
	return obj(c.Must(200, "PATCH", "/api/v1/conversations/"+conv+"/membership", map[string]any{"mode": mode}), "membership")
}

func TestGroups(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	admin := ts.Admin("adm")
	ann, bob := ts.Member("ann"), ts.Member("bob")
	priv := owner.Channel("vault", true)
	gus := ts.Guest("gus", priv)
	annS, _ := ann.Events(nil)
	gusS, _ := gus.Events(nil)

	// Admins create groups; nobody else does (case 108).
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/groups", map[string]any{"handle": "ops", "name": "Ops"})
	gus.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/groups", map[string]any{"handle": "ops", "name": "Ops"})
	g := obj(admin.Must(201, "POST", "/api/v1/admin/groups", map[string]any{"handle": "Ops", "name": "Operations", "description": "On call", "color": "cyan"}), "group")
	gid := str(g, "id")
	if g["handle"] != "ops" || g["open"] != false || g["member_count"] != float64(0) || g["is_member"] != false || g["color"] != "cyan" {
		t.Fatalf("created group: %v", g)
	}
	if e := annS.Expect("group.created"); obj(e, "group")["id"] != gid {
		t.Fatalf("group.created: %v", e)
	}
	// A handle shares its namespace with usernames, and the refusal says
	// nothing about why.
	admin.Refused(409, model.CodeNameTaken, "POST", "/api/v1/admin/groups", map[string]any{"handle": "ann", "name": "Ann"})
	admin.Refused(409, model.CodeNameTaken, "POST", "/api/v1/admin/groups", map[string]any{"handle": "ops", "name": "Again"})
	admin.Refused(409, model.CodeNameTaken, "POST", "/api/v1/admin/groups", map[string]any{"handle": "everyone", "name": "All"})
	admin.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/groups", map[string]any{"handle": "Not A Handle", "name": "x"})
	admin.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/groups", map[string]any{"handle": "blue", "name": "Blue", "color": "blue"})
	admin.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/groups", map[string]any{"handle": "nameless"})

	// Members see every group; a guest only its own and those added to
	// its conversations (decision P6).
	if n := len(arr(ann.Must(200, "GET", "/api/v1/groups", nil), "groups")); n != 3 { // everyone, admins, ops
		t.Fatalf("a member sees %d groups, want 3", n)
	}
	if n := len(arr(gus.Must(200, "GET", "/api/v1/groups", nil), "groups")); n != 0 {
		t.Fatalf("a guest sees %d groups, want 0", n)
	}
	gus.Refused(404, model.CodeNotFound, "GET", "/api/v1/groups/"+gid, nil)
	gus.Refused(404, model.CodeNotFound, "GET", "/api/v1/groups/"+gid+"/members", nil)
	ann.Refused(404, model.CodeNotFound, "GET", "/api/v1/groups/"+string(model.NewID[model.GroupID]()), nil)
	ann.Refused(404, model.CodeNotFound, "GET", "/api/v1/groups/not-an-id", nil)

	// A group that is not open cannot be joined by oneself (case 110).
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/groups/"+gid+"/members/me", nil)
	// Nobody edits the built-in groups (case 108).
	var everyone string
	for _, v := range arr(ann.Must(200, "GET", "/api/v1/groups", nil), "groups") {
		if v.(map[string]any)["builtin"] == "everyone" {
			everyone = str(v.(map[string]any), "id")
		}
	}
	ann.Refused(403, model.CodeForbidden, "PATCH", "/api/v1/admin/groups/"+gid, map[string]any{"name": "Mine"})
	ann.Refused(403, model.CodeForbidden, "PATCH", "/api/v1/admin/groups/"+everyone, map[string]any{"name": "Mine"})
	admin.Refused(403, model.CodeForbidden, "PATCH", "/api/v1/admin/groups/"+everyone, map[string]any{"name": "All"})
	admin.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/groups/"+everyone, nil)
	admin.Refused(403, model.CodeForbidden, "PUT", "/api/v1/admin/groups/"+everyone+"/members/"+string(gus.ID), nil)
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/groups/"+everyone+"/members/me", nil)
	if ids := arr(ann.Must(200, "GET", "/api/v1/groups/"+everyone+"/members", nil), "account_ids"); len(ids) != 4 || hasID(ids, string(gus.ID)) {
		t.Fatalf("@everyone is the owner, admins and members, not guests: %v", ids)
	}

	// Opened, it can be joined; the group is added to a private channel,
	// so joining brings ann in at the group's level.
	up := obj(admin.Must(200, "PATCH", "/api/v1/admin/groups/"+gid, map[string]any{"open": true}), "group")
	if up["open"] != true || up["name"] != "Operations" {
		t.Fatalf("updated group: %v", up)
	}
	annS.Expect("group.updated")
	_, _, err := ts.Server.Store.Members().AddGroup(bg, model.ChannelGroup{ConversationID: model.ConversationID(priv), GroupID: model.GroupID(gid),
		Level: model.LevelReader, AddedByID: owner.ID, AddedAt: model.Now()})
	if err != nil {
		t.Fatal(err)
	}
	// Now the guest, who is in that channel, can see the group.
	if n := len(arr(gus.Must(200, "GET", "/api/v1/groups", nil), "groups")); n != 1 {
		t.Fatalf("a guest sees the group added to its channel: %d", n)
	}
	gus.Must(200, "GET", "/api/v1/groups/"+gid, nil)
	// But cannot join by itself (decision P14).
	gus.Refused(403, model.CodeForbidden, "POST", "/api/v1/groups/"+gid+"/members/me", nil)

	ann.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv, nil)
	joined := obj(ann.Must(200, "POST", "/api/v1/groups/"+gid+"/members/me", nil), "group")
	if joined["is_member"] != true || joined["member_count"] != float64(1) {
		t.Fatalf("joined: %v", joined)
	}
	ann.Must(200, "POST", "/api/v1/groups/"+gid+"/members/me", nil) // again changes nothing
	if e := annS.Expect("member.joined"); e["conversation_id"] != priv || obj(e, "conversation", "membership")["via_group"] != true {
		t.Fatalf("joining the group joins its channels: %v", e)
	}
	if e := annS.Expect("group.member_added"); e["account_id"] != string(ann.ID) || obj(e, "group")["is_member"] != true {
		t.Fatalf("group.member_added to the one who joined: %v", e)
	}
	if e := gusS.Expect("group.member_added"); obj(e, "group")["is_member"] != false {
		t.Fatalf("each receiver gets its own is_member: %v", e)
	}
	ms := membershipOf(ann, priv)
	if ms["level"] != "reader" || ms["via_group"] != true {
		t.Fatalf("in the channel through the group, at its level: %v", ms)
	}
	if ids := arr(bob.Must(200, "GET", "/api/v1/groups/"+gid+"/members", nil), "account_ids"); len(ids) != 1 || ids[0] != string(ann.ID) {
		t.Fatalf("group members: %v", ids)
	}
	if me := obj(ann.Must(200, "GET", "/api/v1/me", nil), "account"); !hasID(arr(me, "group_ids"), gid) {
		t.Fatalf("the account lists its groups: %v", me["group_ids"])
	}
	// Someone in a channel through a group cannot leave it (case 101).
	ann.Refused(409, model.CodeConflict, "POST", "/api/v1/conversations/"+priv+"/leave", nil)

	// Leaving the group takes ann out of the channel (case 100).
	left := obj(ann.Must(200, "DELETE", "/api/v1/groups/"+gid+"/members/me", nil), "group")
	if left["is_member"] != false || left["member_count"] != float64(0) {
		t.Fatalf("left: %v", left)
	}
	if e := annS.Expect("member.left"); e["reason"] != "group" || e["account_id"] != string(ann.ID) {
		t.Fatalf("member.left with the reason: %v", e)
	}
	annS.Expect("group.member_removed")
	ann.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv, nil)

	// Admins put accounts in, guests included; highest level wins.
	ann.Refused(403, model.CodeForbidden, "PUT", "/api/v1/admin/groups/"+gid+"/members/"+string(bob.ID), nil)
	admin.Must(204, "PUT", "/api/v1/admin/groups/"+gid+"/members/"+string(bob.ID), nil)
	admin.Must(204, "PUT", "/api/v1/admin/groups/"+gid+"/members/"+string(bob.ID), nil) // already there
	admin.Must(204, "PUT", "/api/v1/admin/groups/"+gid+"/members/"+string(gus.ID), nil)
	admin.Refused(404, model.CodeNotFound, "PUT", "/api/v1/admin/groups/"+gid+"/members/"+string(model.NewID[model.AccountID]()), nil)
	if ms := membershipOf(gus, priv); ms["level"] != "member" || ms["via_group"] != true || ms["direct"] != true {
		t.Fatalf("a direct member also in through a group keeps the higher level: %v", ms) // case 97
	}
	if ms := membershipOf(bob, priv); ms["level"] != "reader" {
		t.Fatalf("bob came in through the group: %v", ms)
	}
	admin.Must(204, "DELETE", "/api/v1/admin/groups/"+gid+"/members/"+string(gus.ID), nil)
	admin.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/admin/groups/"+gid+"/members/"+string(gus.ID), nil)
	gus.Must(200, "GET", "/api/v1/conversations/"+priv, nil) // still in directly

	// A group filled from a provider is not set by hand, and is never open.
	provider := model.SsoProvider{Kind: "oidc", Name: "Corp", Config: json.RawMessage(`{}`)}
	if err := ts.Server.Store.Identities().CreateProvider(bg, &provider); err != nil {
		t.Fatal(err)
	}
	admin.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/groups", map[string]any{"handle": "eng", "name": "Eng",
		"open": true, "provider_id": provider.ID, "provider_group": "engineering"})
	admin.Refused(404, model.CodeNotFound, "POST", "/api/v1/admin/groups", map[string]any{"handle": "eng", "name": "Eng",
		"provider_id": string(model.NewID[model.GroupID]()), "provider_group": "engineering"})
	mapped := str(obj(admin.Must(201, "POST", "/api/v1/admin/groups", map[string]any{"handle": "eng", "name": "Eng",
		"provider_id": provider.ID, "provider_group": "engineering"}), "group"), "id")
	admin.Refused(409, model.CodeConflict, "PUT", "/api/v1/admin/groups/"+mapped+"/members/"+string(bob.ID), nil)
	admin.Refused(409, model.CodeConflict, "DELETE", "/api/v1/admin/groups/"+mapped+"/members/"+string(bob.ID), nil)

	// Deleting the group takes its members out of the channels they were
	// in only through it (case 103).
	ann.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/groups/"+gid, nil)
	admin.Must(204, "DELETE", "/api/v1/admin/groups/"+gid, nil)
	annS.Expect("group.deleted")
	bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv, nil)
	admin.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/admin/groups/"+gid, nil)
	ann.Refused(404, model.CodeNotFound, "GET", "/api/v1/groups/"+gid, nil)
}

func TestBlocks(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ch := ann.Channel("town", false, bob)
	setMode(ann, ch, "any")
	annS, _ := ann.Events(nil)
	bobS, _ := bob.Events(nil)

	ann.Refused(404, model.CodeNotFound, "PUT", "/api/v1/me/blocks/"+string(model.NewID[model.AccountID]()), nil)
	ann.Refused(404, model.CodeNotFound, "PUT", "/api/v1/me/blocks/not-an-id", nil)
	ann.Refused(400, model.CodeInvalidRequest, "PUT", "/api/v1/me/blocks/"+string(ann.ID), nil)

	bob.Post(ch, "before the block")
	if e := annS.Expect("message.created"); e["notify"] != "notify" {
		t.Fatalf("before the block bob's message notifies: %v", e["notify"])
	}
	if ms := membershipOf(ann, ch); ms["unread_count"] != float64(1) || ms["badge_count"] != float64(1) {
		t.Fatalf("before the block: %v", ms)
	}

	ann.Must(204, "PUT", "/api/v1/me/blocks/"+string(bob.ID), nil)
	ann.Must(204, "PUT", "/api/v1/me/blocks/"+string(bob.ID), nil) // again changes nothing
	if e := annS.Expect("block.added"); e["account_id"] != string(bob.ID) {
		t.Fatalf("block.added: %v", e)
	}
	if ids := arr(ann.Must(200, "GET", "/api/v1/me/blocks", nil), "account_ids"); len(ids) != 1 || ids[0] != string(bob.ID) {
		t.Fatalf("block list: %v", ids)
	}
	if me := obj(ann.Must(200, "GET", "/api/v1/me", nil), "account"); !hasID(arr(me, "blocked_ids"), string(bob.ID)) {
		t.Fatalf("me.blocked_ids: %v", me["blocked_ids"])
	}
	if n := len(arr(bob.Must(200, "GET", "/api/v1/me/blocks", nil), "account_ids")); n != 0 {
		t.Fatalf("the blocked account has no list of who blocked it: %d", n)
	}

	// Case 142: the blocked account's messages and mentions never notify
	// or count; it is told nothing.
	if ms := membershipOf(ann, ch); ms["unread_count"] != float64(0) || ms["badge_count"] != float64(0) {
		t.Fatalf("a blocked author's unread messages stop counting: %v", ms)
	}
	posted := bob.Must(201, "POST", "/api/v1/conversations/"+ch+"/messages", map[string]any{"text": "hey @ann"})
	if posted["cannot_read"] != nil {
		t.Fatalf("the author learns nothing from the answer: %v", posted)
	}
	if e := annS.Expect("message.created"); e["notify"] != "none" {
		t.Fatalf("a blocked author's mention: notify = %v", e["notify"])
	}
	if ms := membershipOf(ann, ch); ms["unread_count"] != float64(0) || ms["mention_count"] != float64(0) || ms["badge_count"] != float64(0) {
		t.Fatalf("no badge change from a blocked author: %v", ms)
	}
	// Case 141 (ruling R77): bob's chat with ann looks normal to him and
	// never reaches her.
	ghost := obj(bob.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(ann.ID)}}), "conversation")
	if ids := arr(ghost, "member_ids"); len(ids) != 2 || !hasID(ids, string(ann.ID)) {
		t.Fatalf("bob is shown ann in the chat: %v", ghost)
	}
	ann.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+ghost["id"].(string), nil)

	// Typing goes neither way between them.
	bob.Must(204, "POST", "/api/v1/conversations/"+ch+"/typing", map[string]any{})
	ann.Must(204, "POST", "/api/v1/conversations/"+ch+"/typing", nil)
	for {
		f, ok := bobS.Next(300 * time.Millisecond)
		if !ok {
			break
		}
		if f["type"] == "typing.started" {
			t.Fatalf("bob was shown typing from ann, who blocked him: %v", f)
		}
	}

	// Unblocking: what is still unread counts again.
	ann.Must(204, "DELETE", "/api/v1/me/blocks/"+string(bob.ID), nil)
	if e := annS.Expect("block.removed"); e["account_id"] != string(bob.ID) {
		t.Fatalf("block.removed: %v", e)
	}
	if ms := membershipOf(ann, ch); ms["unread_count"] != float64(2) {
		t.Fatalf("after unblocking, unread messages count again: %v", ms)
	}
	ann.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/blocks/"+string(model.NewID[model.AccountID]()), nil)

	// Case 143: with blocking off, new blocks are refused and stored ones
	// have no effect.
	ann.Must(204, "PUT", "/api/v1/me/blocks/"+string(bob.ID), nil)
	if err := ts.Server.Store.Settings().Set(bg, store.SettingBlockingEnabled, false, ""); err != nil {
		t.Fatal(err)
	}
	ann.Refused(403, model.CodeFeatureOff, "PUT", "/api/v1/me/blocks/"+string(bob.ID), nil)
	bob.Post(ch, "blocking is off")
	if e := annS.ExpectWhere("message.created", func(f map[string]any) bool { return obj(f, "message")["text"] == "blocking is off" }); e["notify"] != "notify" {
		t.Fatalf("with blocking off a stored block has no effect: %v", e["notify"])
	}
	if status, out := bob.Do("POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(ann.ID)}}); status != 200 && status != 201 {
		t.Fatalf("with blocking off bob can start a chat with ann: %d %v", status, out)
	}
}

// png is a 1x1 PNG image.
var png = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R', 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 0x1f, 0x15, 0xc4, 0x89,
	0, 0, 0, 0x0d, 'I', 'D', 'A', 'T', 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0xf0, 0x1f, 0, 5, 0, 1, 0xff, 0x89, 0x99, 0x3d, 0x1d, 0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82}

func TestProfileAndPrefs(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	priv := ann.Channel("den", true, bob)
	_ = priv
	annS, _ := ann.Events(nil)
	bobS, _ := bob.Events(nil)
	catGuest := ts.Guest("gus", ts.Owner.Channel("lobby", true))
	gusS, _ := catGuest.Events(nil)

	me := obj(ann.Must(200, "PATCH", "/api/v1/me", map[string]any{"display_name": " Ann Lee ", "title": "Engineer", "pronouns": "she/her", "time_zone": "Europe/Berlin"}), "account")
	if me["display_name"] != "Ann Lee" || me["title"] != "Engineer" || me["pronouns"] != "she/her" || me["time_zone"] != "Europe/Berlin" {
		t.Fatalf("updated profile: %v", me)
	}
	if e := bobS.Expect("account.updated"); obj(e, "account")["display_name"] != "Ann Lee" || obj(e, "account")["prefs"] != nil {
		t.Fatalf("account.updated to someone who shares a conversation, in the public shape: %v", e)
	}
	if e := annS.Expect("account.updated"); obj(e, "account")["id"] != string(ann.ID) {
		t.Fatalf("account.updated to own connections: %v", e)
	}
	if got := obj(cat.Must(200, "GET", "/api/v1/accounts/"+string(ann.ID), nil), "account"); got["title"] != "Engineer" {
		t.Fatalf("others see the profile: %v", got)
	}
	// A username: lower-cased, checked, and unique with group handles.
	me = obj(ann.Must(200, "PATCH", "/api/v1/me", map[string]any{"username": "Annie"}), "account")
	if me["username"] != "annie" {
		t.Fatalf("username: %v", me["username"])
	}
	ann.Refused(409, model.CodeNameTaken, "PATCH", "/api/v1/me", map[string]any{"username": "bob"})
	ann.Refused(409, model.CodeNameTaken, "PATCH", "/api/v1/me", map[string]any{"username": "everyone"})
	ann.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/me", map[string]any{"username": "a"})
	ann.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/me", map[string]any{"display_name": ""})
	ann.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/me", map[string]any{"time_zone": "Mars/Olympus"})
	ann.Refused(400, model.CodeInvalidRequest, "PATCH", "/api/v1/me", map[string]any{"role": "owner"})
	ts.Browser().Refused(401, model.CodeUnauthenticated, "PATCH", "/api/v1/me", map[string]any{"title": "x"})
	// The old name is held for ann for 30 days (review finding H8);
	// nobody else's events leaked to a guest who shares nothing with ann.
	bob.Refused(409, model.CodeNameTaken, "PATCH", "/api/v1/me", map[string]any{"username": "ann"})

	// Preferences are the account's own.
	prefs := obj(ann.Must(200, "GET", "/api/v1/me/prefs", nil), "prefs")
	if prefs["thread_view"] != "threaded" || prefs["meeting_pauses"] != false || prefs["allow_notify_anyway"] != true {
		t.Fatalf("default prefs: %v", prefs)
	}
	prefs = obj(ann.Must(200, "PATCH", "/api/v1/me/prefs", map[string]any{"thread_view": "flat", "allow_notify_anyway": false}), "prefs")
	if prefs["thread_view"] != "flat" || prefs["allow_notify_anyway"] != false || prefs["meeting_pauses"] != false {
		t.Fatalf("updated prefs: %v", prefs)
	}
	if e := annS.Expect("me.updated"); obj(e, "account", "prefs")["thread_view"] != "flat" {
		t.Fatalf("me.updated: %v", e)
	}
	ann.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/me/prefs", map[string]any{"thread_view": "sideways"})
	if got := obj(bob.Must(200, "GET", "/api/v1/accounts/"+string(ann.ID), nil), "account"); got["prefs"] != nil {
		t.Fatalf("nobody else sees preferences: %v", got)
	}

	// The avatar.
	tok := ann.Token()
	status, out := tok.SendForm("PUT", "/api/v1/me/avatar", "file", "me.png", png)
	if status != 200 || str(obj(out, "account"), "avatar_file_id") == "" || str(obj(out, "account"), "avatar_url") == "" {
		t.Fatalf("avatar upload: %d %v", status, out)
	}
	first := str(obj(out, "account"), "avatar_file_id")
	bobS.ExpectWhere("account.updated", func(f map[string]any) bool { return obj(f, "account")["avatar_file_id"] == first })
	status, out = tok.SendForm("PUT", "/api/v1/me/avatar", "file", "me.png", png)
	if status != 200 || str(obj(out, "account"), "avatar_file_id") == first {
		t.Fatalf("a second upload replaces the first: %d %v", status, out)
	}
	if _, err := ts.Server.Store.Files().Get(bg, model.FileID(first)); model.CodeOf(err) != model.CodeNotFound {
		t.Fatalf("the replaced avatar's file is gone: %v", err)
	}
	if status, out = tok.SendForm("PUT", "/api/v1/me/avatar", "file", "notes.txt", []byte("not an image")); status != 400 || obj(out, "error")["code"] != "invalid_value" {
		t.Fatalf("an avatar must be an image: %d %v", status, out)
	}
	if status, out = tok.SendForm("PUT", "/api/v1/me/avatar", "other", "me.png", png); status != 400 {
		t.Fatalf("the part must be called file: %d %v", status, out)
	}
	if status, out = tok.SendForm("PUT", "/api/v1/me/avatar", "file", "big.png", append(append([]byte{}, png...), make([]byte, 2<<20)...)); status != 413 {
		t.Fatalf("an avatar over the limit: %d %v", status, out)
	}
	ann.Must(204, "DELETE", "/api/v1/me/avatar", nil)
	ann.Must(204, "DELETE", "/api/v1/me/avatar", nil) // none left: still fine
	if me := obj(ann.Must(200, "GET", "/api/v1/me", nil), "account"); me["avatar_file_id"] != nil {
		t.Fatalf("avatar removed: %v", me)
	}
	// The guest shares no conversation with ann and heard none of this.
	gusS.ExpectNone(200 * time.Millisecond)
}

func TestStatusAndPresence(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ann.Channel("den", true, bob)
	// Every member shares #general; a guest in a channel of its own
	// shares nothing with ann.
	cat := ts.Guest("cat", ts.Owner.Channel("lobby", true))
	see := func(c *testkit.Client) map[string]any {
		return obj(c.Must(200, "GET", "/api/v1/accounts/"+string(ann.ID), nil), "account")
	}

	// Presence follows connections, for those who share a conversation.
	if got := see(bob); got["presence"] != "offline" {
		t.Fatalf("nobody connected: %v", got["presence"])
	}
	bobS, _ := bob.Events(nil)
	annS, _ := ann.Events(nil)
	if e := bobS.ExpectWhere("presence.changed", func(f map[string]any) bool { return f["account_id"] == string(ann.ID) }); e["presence"] != "online" {
		t.Fatalf("presence.changed when ann connects: %v", e)
	}
	if got := see(bob); got["presence"] != "online" {
		t.Fatalf("bob shares a conversation with ann: %v", got["presence"])
	}
	// Someone who shares no conversation with ann is shown nothing of her.
	cat.Refused(404, model.CodeNotFound, "GET", "/api/v1/accounts/"+string(ann.ID), nil)
	catS, _ := cat.Events(nil)

	// A status with both switches shows as the status, away, and the
	// do not disturb mark.
	me := obj(ann.Must(200, "PUT", "/api/v1/me/status", map[string]any{"emoji": "🌴", "text": "On holiday", "duration_seconds": 3600,
		"pause_notifications": true, "appear_away": true}), "account")
	if obj(me, "status")["text"] != "On holiday" || obj(me, "status")["until"] == nil || me["presence"] != "away" || me["dnd"] != true {
		t.Fatalf("own account after setting a status: %v", me)
	}
	if st := obj(me, "dnd_state"); st["on"] != true || len(arr(st, "sources")) != 1 || arr(st, "sources")[0] != "status" || st["until"] == nil {
		t.Fatalf("dnd_state: %v", st)
	}
	if e := bobS.ExpectWhere("presence.changed", func(f map[string]any) bool { return f["presence"] == "away" }); e["account_id"] != string(ann.ID) {
		t.Fatalf("appear away shows to others: %v", e)
	}
	if e := bobS.Expect("status.changed"); obj(e, "status")["emoji"] != "🌴" || obj(e, "status")["appear_away"] != true {
		t.Fatalf("status.changed: %v", e)
	}
	if e := bobS.Expect("dnd.changed"); e["dnd"] != true || e["dnd_state"] != nil {
		t.Fatalf("others get the mark and never why: %v", e)
	}
	if e := annS.Expect("dnd.changed"); obj(e, "dnd_state")["on"] != true {
		t.Fatalf("own connections get the detail: %v", e)
	}
	if got := see(bob); got["presence"] != "away" || got["dnd"] != true || obj(got, "status")["text"] != "On holiday" {
		t.Fatalf("what bob sees: %v", got)
	}
	cat.Refused(404, model.CodeNotFound, "GET", "/api/v1/accounts/"+string(ann.ID), nil)

	// Clearing it ends both switches.
	ann.Must(204, "DELETE", "/api/v1/me/status", nil)
	if e := bobS.ExpectWhere("presence.changed", func(f map[string]any) bool { return f["presence"] == "online" }); e["account_id"] != string(ann.ID) {
		t.Fatalf("online again: %v", e)
	}
	if e := bobS.Expect("status.changed"); e["status"] != nil {
		t.Fatalf("status.changed without a status when cleared: %v", e)
	}
	if e := bobS.Expect("dnd.changed"); e["dnd"] != false {
		t.Fatalf("dnd off: %v", e)
	}
	if got := see(bob); got["presence"] != "online" || got["dnd"] == true || got["status"] != nil {
		t.Fatalf("after clearing: %v", got)
	}

	// Refusals.
	ann.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/me/status", map[string]any{})
	ann.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/me/status", map[string]any{"text": string(make([]rune, 101))})
	long := ""
	for i := 0; i < 101; i++ {
		long += "x"
	}
	ann.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/me/status", map[string]any{"text": long})
	ann.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/me/status", map[string]any{"text": "x", "until": "2020-01-01T00:00:00Z"})
	ann.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/me/status", map[string]any{"text": "x", "duration_seconds": -5})
	ann.Refused(400, model.CodeInvalidRequest, "PUT", "/api/v1/me/status", map[string]any{"text": "x", "duration_seconds": 5, "until": "2099-01-01T00:00:00Z"})
	ann.Refused(404, model.CodeNotFound, "PUT", "/api/v1/me/status", map[string]any{"preset_id": string(model.NewID[model.GroupID]())})

	// Availability: out of office shows as away; a meeting is a mark.
	me = obj(ann.Must(200, "PUT", "/api/v1/me/availability", map[string]any{"value": "out_of_office", "until": "2099-01-01T00:00:00Z"}), "account")
	if obj(me, "availability")["value"] != "out_of_office" || obj(me, "availability")["set_by"] != "self" || me["presence"] != "away" {
		t.Fatalf("out of office: %v", me)
	}
	if e := bobS.Expect("availability.changed"); obj(e, "availability")["value"] != "out_of_office" {
		t.Fatalf("availability.changed: %v", e)
	}
	me = obj(ann.Must(200, "PUT", "/api/v1/me/availability", map[string]any{"value": "meeting"}), "account")
	if obj(me, "availability")["value"] != "meeting" || me["presence"] != "online" || me["dnd"] == true {
		t.Fatalf("in a meeting, without the preference: %v", me)
	}
	ann.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/me/availability", map[string]any{"value": "lunch"})
	ann.Must(204, "DELETE", "/api/v1/me/availability", nil)
	if e := bobS.ExpectWhere("availability.changed", func(f map[string]any) bool { return f["availability"] == nil }); e["account_id"] != string(ann.ID) {
		t.Fatalf("availability cleared: %v", e)
	}

	// Appear away, by hand.
	me = obj(ann.Must(200, "PUT", "/api/v1/me/away", nil), "account")
	if me["presence"] != "away" || obj(me, "appear_away")["on"] != true || obj(me, "appear_away")["until"] != nil {
		t.Fatalf("appear away until turned off: %v", me)
	}
	bobS.ExpectWhere("presence.changed", func(f map[string]any) bool { return f["presence"] == "away" })
	me = obj(ann.Must(200, "PUT", "/api/v1/me/away", map[string]any{"duration_seconds": 600}), "account")
	if obj(me, "appear_away")["until"] == nil {
		t.Fatalf("appear away for a duration: %v", me)
	}
	ann.Must(204, "DELETE", "/api/v1/me/away", nil)
	bobS.ExpectWhere("presence.changed", func(f map[string]any) bool { return f["presence"] == "online" })
	if me := obj(ann.Must(200, "GET", "/api/v1/me", nil), "account"); me["appear_away"] != nil || me["presence"] != "online" {
		t.Fatalf("no longer away: %v", me)
	}

	// Nothing about ann reached someone who shares no conversation with
	// her: only cat's own presence did.
	for {
		f, ok := catS.Next(200 * time.Millisecond)
		if !ok {
			break
		}
		if f["account_id"] == string(ann.ID) {
			t.Fatalf("an event about ann reached a stranger: %v", f)
		}
	}
	// Disconnecting shows as offline to those who share a conversation.
	annS.Close()
	if e := bobS.ExpectWhere("presence.changed", func(f map[string]any) bool { return f["presence"] == "offline" }); e["account_id"] != string(ann.ID) {
		t.Fatalf("offline when the last connection goes: %v", e)
	}
	if got := see(bob); got["presence"] != "offline" {
		t.Fatalf("offline: %v", got["presence"])
	}
}

func TestPresets(t *testing.T) {
	ts := testkit.NewServer(t)
	admin := ts.Admin("adm")
	ann, bob := ts.Member("ann"), ts.Member("bob")

	// Community presets are the admins'.
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/status-presets", map[string]any{"text": "Lunch"})
	lunch := obj(admin.Must(201, "POST", "/api/v1/admin/status-presets", map[string]any{"emoji": "🍜", "text": "Lunch", "duration_seconds": 3600, "position": 1}), "preset")
	if lunch["community"] != true || lunch["duration_seconds"] != float64(3600) || lunch["pause_notifications"] != false {
		t.Fatalf("community preset: %v", lunch)
	}
	holiday := obj(admin.Must(201, "POST", "/api/v1/admin/status-presets", map[string]any{"emoji": "🌴", "text": "On holiday", "appear_away": true,
		"pause_notifications": true, "availability": "out_of_office", "duration_seconds": 7200, "position": 2}), "preset")
	admin.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/status-presets", map[string]any{})
	admin.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/status-presets", map[string]any{"text": "x", "availability": "napping"})

	// Personal ones are their owner's.
	mine := obj(ann.Must(201, "POST", "/api/v1/me/status-presets", map[string]any{"text": "Deep work", "pause_notifications": true}), "preset")
	if mine["community"] != false || mine["duration_seconds"] != nil {
		t.Fatalf("personal preset: %v", mine)
	}
	if n := len(arr(ann.Must(200, "GET", "/api/v1/status-presets", nil), "presets")); n != 3 {
		t.Fatalf("ann sees the community's and her own: %d", n)
	}
	list := arr(bob.Must(200, "GET", "/api/v1/status-presets", nil), "presets")
	if len(list) != 2 || list[0].(map[string]any)["id"] != lunch["id"] {
		t.Fatalf("bob sees the community's, in order: %v", list)
	}
	bob.Refused(404, model.CodeNotFound, "PATCH", "/api/v1/me/status-presets/"+str(mine, "id"), map[string]any{"text": "Mine now"})
	bob.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/status-presets/"+str(mine, "id"), nil)
	// A community preset is not changed through the personal operations,
	// nor a personal one through the admin ones.
	admin.Refused(404, model.CodeNotFound, "PATCH", "/api/v1/me/status-presets/"+str(lunch, "id"), map[string]any{"text": "x"})
	admin.Refused(404, model.CodeNotFound, "PATCH", "/api/v1/admin/status-presets/"+str(mine, "id"), map[string]any{"text": "x"})
	ann.Refused(403, model.CodeForbidden, "PATCH", "/api/v1/admin/status-presets/"+str(lunch, "id"), map[string]any{"text": "x"})
	ann.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/status-presets/"+str(lunch, "id"), nil)

	up := obj(ann.Must(200, "PATCH", "/api/v1/me/status-presets/"+str(mine, "id"), map[string]any{"text": "Deeper work", "duration_seconds": 1800}), "preset")
	if up["text"] != "Deeper work" || up["duration_seconds"] != float64(1800) || up["pause_notifications"] != true {
		t.Fatalf("a change leaves the other fields: %v", up)
	}
	ann.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/me/status-presets/"+str(mine, "id"), map[string]any{"text": "", "emoji": ""})

	// Applying a preset copies its text, switches, duration and
	// availability.
	me := obj(bob.Must(200, "PUT", "/api/v1/me/status", map[string]any{"preset_id": holiday["id"]}), "account")
	st, av := obj(me, "status"), obj(me, "availability")
	if st["text"] != "On holiday" || st["emoji"] != "🌴" || st["appear_away"] != true || st["pause_notifications"] != true || st["until"] == nil {
		t.Fatalf("status from a preset: %v", st)
	}
	if av["value"] != "out_of_office" || av["until"] != st["until"] {
		t.Fatalf("the preset's availability, with the same end: %v %v", av, st)
	}
	bob.Refused(404, model.CodeNotFound, "PUT", "/api/v1/me/status", map[string]any{"preset_id": mine["id"]}) // someone else's
	bob.Refused(400, model.CodeInvalidRequest, "PUT", "/api/v1/me/status", map[string]any{"preset_id": lunch["id"], "text": "and more"})
	// Later changes to the preset do not touch a status set from it.
	admin.Must(200, "PATCH", "/api/v1/admin/status-presets/"+str(holiday, "id"), map[string]any{"text": "Away"})
	if me := obj(bob.Must(200, "GET", "/api/v1/me", nil), "account"); obj(me, "status")["text"] != "On holiday" {
		t.Fatalf("the status keeps what it was set to: %v", me["status"])
	}

	ann.Must(204, "DELETE", "/api/v1/me/status-presets/"+str(mine, "id"), nil)
	ann.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/status-presets/"+str(mine, "id"), nil)
	admin.Must(204, "DELETE", "/api/v1/admin/status-presets/"+str(lunch, "id"), nil)
	if n := len(arr(ann.Must(200, "GET", "/api/v1/status-presets", nil), "presets")); n != 1 {
		t.Fatalf("after deleting: %d", n)
	}
}

// TestDoNotDisturb covers the four sources, that they are independent, and
// ruling R37: turning do not disturb off clears all four as narrowly as it
// can.
func TestDoNotDisturb(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	dndOf := func(out map[string]any) (bool, string) {
		d := obj(out, "dnd")
		s := ""
		for _, v := range arr(d, "sources") {
			s += v.(string) + " "
		}
		return d["on"] == true, s
	}
	me := func() map[string]any { return obj(ann.Must(200, "GET", "/api/v1/me", nil), "account") }

	// Manual: until turned off, for a duration, until a time.
	out := ann.Must(200, "PUT", "/api/v1/me/dnd", nil)
	if on, src := dndOf(out); !on || src != "manual " || obj(out, "dnd")["manual_until"] != nil || obj(out, "dnd")["until"] != nil {
		t.Fatalf("until turned off: %v", out)
	}
	out = ann.Must(200, "PUT", "/api/v1/me/dnd", map[string]any{"duration_seconds": 1800})
	if obj(out, "dnd")["manual_until"] == nil || obj(out, "dnd")["until"] != obj(out, "dnd")["manual_until"] {
		t.Fatalf("for a duration: %v", out)
	}
	ann.Must(200, "PUT", "/api/v1/me/dnd", map[string]any{"until": "2099-01-01T00:00:00Z"})
	ann.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/me/dnd", map[string]any{"until": "2001-01-01T00:00:00Z"})
	ann.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/me/dnd", map[string]any{"duration_seconds": 0})
	if m := me(); m["dnd"] != true || obj(m, "dnd_state")["on"] != true {
		t.Fatalf("me shows it: %v", m)
	}
	out = ann.Must(200, "DELETE", "/api/v1/me/dnd", nil)
	if on, src := dndOf(out); on || src != "" {
		t.Fatalf("off: %v", out)
	}

	// Quiet hours: none at first.
	if out := ann.Must(200, "GET", "/api/v1/me/quiet-hours", nil); out["quiet_hours"] != nil {
		t.Fatalf("no schedule: %v", out)
	}
	ann.Refused(404, model.CodeNotFound, "PUT", "/api/v1/me/quiet-hours/skip", nil)
	ann.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/quiet-hours/skip", nil)
	for _, bad := range []map[string]any{
		{"start": "22:00", "end": "22:00", "days": "1111111"},
		{"start": "25:00", "end": "07:00", "days": "1111111"},
		{"start": "22:00", "end": "7am", "days": "1111111"},
		{"start": "22:00", "end": "07:00", "days": "11111"},
	} {
		ann.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/me/quiet-hours", bad)
	}

	// A schedule that is quiet right now, every day, in UTC.
	ann.Must(200, "PATCH", "/api/v1/me", map[string]any{"time_zone": "UTC"})
	now := time.Now().UTC()
	hhmm := func(t time.Time) string { return t.Format("15:04") }
	q := obj(ann.Must(200, "PUT", "/api/v1/me/quiet-hours", map[string]any{"start": hhmm(now.Add(-2 * time.Hour)), "end": hhmm(now.Add(2 * time.Hour)), "days": "1111111"}), "quiet_hours")
	if q["time_zone"] != "UTC" || q["days"] != "1111111" || q["skip_until"] != nil {
		t.Fatalf("quiet hours: %v", q)
	}
	if m := me(); m["dnd"] != true || arr(obj(m, "dnd_state"), "sources")[0] != "quiet" || obj(m, "quiet_hours")["start"] != q["start"] {
		t.Fatalf("inside quiet hours: %v", m)
	}

	// Manual and quiet are independent: turning the manual one on and
	// letting it be the only thing cleared is not what "off" means, so
	// set every source and turn do not disturb off once (ruling R37).
	ann.Must(200, "PUT", "/api/v1/me/dnd", map[string]any{"duration_seconds": 60})
	ann.Must(200, "PUT", "/api/v1/me/status", map[string]any{"text": "Heads down", "pause_notifications": true, "appear_away": true})
	ann.Must(200, "PATCH", "/api/v1/me/prefs", map[string]any{"meeting_pauses": true})
	ann.Must(200, "PUT", "/api/v1/me/availability", map[string]any{"value": "meeting", "until": "2099-01-01T00:00:00Z"})
	m := me()
	if got := fmt.Sprint(arr(obj(m, "dnd_state"), "sources")); got != "[manual status meeting quiet]" {
		t.Fatalf("all four sources: %v", got)
	}
	out = ann.Must(200, "DELETE", "/api/v1/me/dnd", nil)
	if on, src := dndOf(out); on || src != "" {
		t.Fatalf("R37: turning do not disturb off leaves no source on: %v", out)
	}
	m = me()
	if m["dnd"] == true {
		t.Fatalf("available now: %v", m)
	}
	// Each clearing is as narrow as it can be.
	if st := obj(m, "status"); st["text"] != "Heads down" || st["pause_notifications"] != false || st["appear_away"] != true {
		t.Fatalf("the status keeps its text and its appear-away switch: %v", st)
	}
	if obj(m, "availability")["value"] != "meeting" || obj(m, "prefs")["meeting_pauses"] != true {
		t.Fatalf("the meeting mark and the preference stay: %v", m)
	}
	if obj(m, "quiet_hours")["skip_until"] == nil {
		t.Fatalf("only the current quiet period is skipped: %v", m["quiet_hours"])
	}
	// The next meeting pauses again (decision F5).
	ann.Must(200, "PUT", "/api/v1/me/availability", map[string]any{"value": "meeting"})
	if m := me(); fmt.Sprint(arr(obj(m, "dnd_state"), "sources")) != "[meeting]" || obj(m, "dnd_state")["until"] != nil {
		t.Fatalf("a new meeting pauses again, with no known end: %v", m["dnd_state"])
	}
	ann.Must(204, "DELETE", "/api/v1/me/availability", nil)

	// Skipping the next period: the answer says what is still on.
	out = ann.Must(200, "PUT", "/api/v1/me/quiet-hours/skip", nil)
	if obj(out, "quiet_hours")["skip_until"] == nil || out["dnd"] == nil {
		t.Fatalf("skip: %v", out)
	}
	out = ann.Must(200, "DELETE", "/api/v1/me/quiet-hours/skip", nil)
	if on := me()["dnd"]; on == true {
		t.Fatalf("un-skipping the next period does not bring back the one that was ended: %v", out)
	}
	// Setting the manual one again is independent of quiet hours.
	out = ann.Must(200, "PUT", "/api/v1/me/dnd", map[string]any{"duration_seconds": 60})
	if _, src := dndOf(out); src != "manual " {
		t.Fatalf("manual alone: %v", out)
	}
	ann.Must(200, "DELETE", "/api/v1/me/dnd", nil)

	// Switching the schedule off clears it and any skip.
	ann.Must(204, "DELETE", "/api/v1/me/quiet-hours", nil)
	if out := ann.Must(200, "GET", "/api/v1/me/quiet-hours", nil); out["quiet_hours"] != nil {
		t.Fatalf("schedule off: %v", out)
	}
	if m := me(); m["quiet_hours"] != nil || m["dnd"] == true {
		t.Fatalf("after switching off: %v", m)
	}
}

// statusOf fetches the status summary.
func statusOf(c *testkit.Client) map[string]any { return c.Must(200, "GET", "/api/v1/status", nil) }

// sumOfBadges adds up the conversation list the way the status endpoint
// must agree with.
func sumOfBadges(t *testing.T, c *testkit.Client) (badge, mentions, unread float64) {
	t.Helper()
	for _, v := range arr(c.Must(200, "GET", "/api/v1/conversations", nil), "conversations") {
		ms := obj(v.(map[string]any), "membership")
		badge += ms["badge_count"].(float64)
		mentions += ms["mention_count"].(float64)
		if ms["unread_count"].(float64) > 0 {
			unread++
		}
	}
	return
}

func TestModesBadgesAndStatus(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	a := ann.Channel("alpha", false, bob)
	b := ann.Channel("beta", false, bob)
	c := ann.Channel("gamma", false, bob)

	agree := func(what string) map[string]any {
		t.Helper()
		st := statusOf(bob)
		badge, mentions, unread := sumOfBadges(t, bob)
		if st["badge"] != badge || st["mentions"] != mentions || st["unread_conversations"] != unread {
			t.Fatalf("%s: status %v does not equal the conversation list (badge %v, mentions %v, unread %v)", what, st, badge, mentions, unread)
		}
		return st
	}

	// A new member of a named channel is on Mentions.
	if ms := membershipOf(bob, a); ms["mode"] != "mentions" || ms["starred"] != false || ms["muted_until"] != nil {
		t.Fatalf("default membership: %v", ms)
	}
	st := agree("nothing yet")
	if st["badge"] != float64(0) || st["dnd"] != false || st["unread_threads"] != float64(0) {
		t.Fatalf("empty status: %v", st)
	}

	bobS, _ := bob.Events(nil)
	if ms := setMode(bob, a, "any"); ms["mode"] != "any" {
		t.Fatalf("mode set: %v", ms)
	}
	if e := bobS.Expect("membership.updated"); e["conversation_id"] != a || obj(e, "membership")["mode"] != "any" {
		t.Fatalf("membership.updated: %v", e)
	}
	// Hidden from the sidebar: stored and returned, and nothing else
	// (ruling R46). gamma stays on Mentions.
	if ms := obj(bob.Must(200, "PATCH", "/api/v1/conversations/"+c+"/membership", map[string]any{"show": "hidden"}), "membership"); ms["show"] != "hidden" || ms["mode"] != "mentions" {
		t.Fatalf("show is independent of mode: %v", ms)
	}
	if e := bobS.Expect("membership.updated"); obj(e, "membership")["show"] != "hidden" {
		t.Fatalf("membership.updated with show: %v", e)
	}

	ann.Post(a, "one")
	if e := bobS.Expect("message.created"); e["notify"] != "notify" {
		t.Fatalf("Any activity: %v", e["notify"])
	}
	bc := bobS.Expect("badge.changed")
	if bc["conversation_id"] != a || bc["badge_count"] != float64(1) || bc["unread_count"] != float64(1) || obj(bc, "total")["badge"] != float64(1) || obj(bc, "total")["unread_conversations"] != float64(1) {
		t.Fatalf("badge.changed with the totals: %v", bc)
	}
	ann.Post(b, "plain")
	if e := bobS.Expect("message.created"); e["notify"] != "none" {
		t.Fatalf("Mentions, no mention: %v", e["notify"])
	}
	ann.Post(b, "hello @bob")
	if e := bobS.Expect("message.created"); e["notify"] != "notify" {
		t.Fatalf("Mentions, a mention: %v", e["notify"])
	}
	if bc := bobS.Expect("badge.changed"); bc["conversation_id"] != b || bc["mention_count"] != float64(1) || obj(bc, "total")["badge"] != float64(2) || obj(bc, "total")["mentions"] != float64(1) {
		t.Fatalf("badge.changed for a mention: %v", bc)
	}
	ann.Post(c, "you there @bob?")
	if e := bobS.Expect("message.created"); e["notify"] != "notify" {
		t.Fatalf("a hidden conversation still notifies: %v", e["notify"])
	}
	bobS.Expect("badge.changed")
	st = agree("three conversations")
	if st["badge"] != float64(3) || st["mentions"] != float64(2) || st["unread_conversations"] != float64(3) || st["newest"] == nil {
		t.Fatalf("a hidden conversation counts in the totals: %v", st)
	}
	if ms := membershipOf(bob, c); ms["badge_count"] != float64(1) || ms["mention_count"] != float64(1) || ms["show"] != "hidden" {
		t.Fatalf("a hidden conversation keeps its badge: %v", ms)
	}
	for _, show := range []string{"always", "activity", "notification"} {
		if ms := obj(bob.Must(200, "PATCH", "/api/v1/conversations/"+c+"/membership", map[string]any{"show": show}), "membership"); ms["show"] != show {
			t.Fatalf("show %s: %v", show, ms)
		}
	}
	if ms := obj(bob.Must(200, "PATCH", "/api/v1/conversations/"+c+"/membership", map[string]any{"show": nil}), "membership"); ms["show"] != nil {
		t.Fatalf("null is the app's own default: %v", ms)
	}
	bob.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/conversations/"+c+"/membership", map[string]any{"show": "sometimes"})
	bob.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/conversations/"+c+"/membership", map[string]any{"mode": "silence"})
	bob.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/conversations/"+c+"/membership", map[string]any{"mode": "hide"})

	// Nothing: a mention by name counts for the badge and does not notify.
	setMode(bob, b, "nothing")
	if ms := membershipOf(bob, b); ms["badge_count"] != float64(1) || ms["mention_count"] != float64(1) || ms["unread_count"] != float64(2) {
		t.Fatalf("nothing keeps the name mention: %v", ms)
	}
	ann.Post(b, "@bob again")
	if e := bobS.ExpectWhere("message.created", func(f map[string]any) bool { return obj(f, "message")["text"] == "@bob again" }); e["notify"] != "badge" {
		t.Fatalf("Nothing, a mention by name: %v", e["notify"])
	}
	agree("with beta on nothing")

	// Mute: behaves as nothing; `mode` is what returns afterwards.
	ms := obj(bob.Must(200, "PATCH", "/api/v1/conversations/"+a+"/membership", map[string]any{"muted_until": "2099-01-01T00:00:00.000Z"}), "membership")
	if ms["mode"] != "any" || ms["muted_until"] != "2099-01-01T00:00:00.000Z" || ms["badge_count"] != float64(0) {
		t.Fatalf("muted: %v", ms)
	}
	ann.Post(a, "while muted")
	if e := bobS.ExpectWhere("message.created", func(f map[string]any) bool { return obj(f, "message")["text"] == "while muted" }); e["notify"] != "none" {
		t.Fatalf("muted: %v", e["notify"])
	}
	agree("while alpha is muted")
	ms = obj(bob.Must(200, "PATCH", "/api/v1/conversations/"+a+"/membership", map[string]any{"muted_until": nil}), "membership")
	if ms["mode"] != "any" || ms["muted_until"] != nil || ms["badge_count"] != float64(2) {
		t.Fatalf("unmuted, the mode from before returns: %v", ms)
	}
	// Setting a mode ends a mute.
	bob.Must(200, "PATCH", "/api/v1/conversations/"+a+"/membership", map[string]any{"muted_until": "2099-01-01T00:00:00Z"})
	if ms := setMode(bob, a, "mentions"); ms["mode"] != "mentions" || ms["muted_until"] != nil {
		t.Fatalf("a mode ends a mute: %v", ms)
	}
	setMode(bob, a, "any")
	// A mute that has run out is swept and its owner's clients are told.
	if _, err := ts.Server.Store.Members().UpdateOwn(bg, model.ConversationID(a), bob.ID, func(m *model.Member) error {
		m.ModeBeforeMute, m.Mode, m.MutedUntil = model.ModeAny, model.ModeNothing, model.Now().Add(-time.Minute)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if ms := membershipOf(bob, a); ms["mode"] != "any" || ms["muted_until"] != nil || ms["badge_count"] != float64(2) {
		t.Fatalf("a mute that ran out reads as unmuted at once: %v", ms)
	}
	if err := ts.Server.Chat.SweepMutes(bg); err != nil {
		t.Fatal(err)
	}
	if e := bobS.ExpectWhere("membership.updated", func(f map[string]any) bool {
		return f["conversation_id"] == a && obj(f, "membership")["muted_until"] == nil
	}); obj(e, "membership")["mode"] != "any" {
		t.Fatalf("membership.updated when a mute ran out: %v", e)
	}

	// Star and thread view.
	ms = obj(bob.Must(200, "PATCH", "/api/v1/conversations/"+a+"/membership", map[string]any{"starred": true, "thread_view": "flat"}), "membership")
	if ms["starred"] != true || ms["thread_view"] != "flat" || ms["mode"] != "any" {
		t.Fatalf("star and view: %v", ms)
	}
	ms = obj(bob.Must(200, "PATCH", "/api/v1/conversations/"+a+"/membership", map[string]any{"thread_view": nil}), "membership")
	if ms["thread_view"] != nil || ms["starred"] != true {
		t.Fatalf("null follows the account preference: %v", ms)
	}

	// Refusals.
	bob.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/conversations/"+a+"/membership", map[string]any{"mode": "loud"})
	bob.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/conversations/"+a+"/membership", map[string]any{"muted_until": "2001-01-01T00:00:00Z"})
	bob.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/conversations/"+a+"/membership", map[string]any{"thread_view": "sideways"})
	bob.Refused(400, model.CodeInvalidRequest, "PATCH", "/api/v1/conversations/"+a+"/membership", map[string]any{"level": "manager"})
	priv := ann.Channel("den", true)
	bob.Refused(404, model.CodeNotFound, "PATCH", "/api/v1/conversations/"+priv+"/membership", map[string]any{"mode": "any"})
	bob.Refused(404, model.CodeNotFound, "POST", "/api/v1/conversations/"+priv+"/typing", nil)

	// Reading brings the numbers down, with the totals alongside.
	last := obj(bob.Must(200, "GET", "/api/v1/conversations/"+a, nil), "conversation")["last_seq"]
	bob.Must(200, "PUT", "/api/v1/conversations/"+a+"/read", map[string]any{"seq": last})
	moved := bobS.Expect("read_position.moved")
	if moved["badge_count"] != float64(0) || obj(moved, "total") == nil {
		t.Fatalf("read_position.moved carries the totals: %v", moved)
	}
	st = agree("after reading alpha")
	if obj(moved, "total")["badge"] != st["badge"] || obj(moved, "total")["unread_conversations"] != st["unread_conversations"] {
		t.Fatalf("the event's totals %v are the status endpoint's %v", moved["total"], st)
	}

	// Do not disturb: badges keep counting, nothing notifies, and the
	// status endpoint says so.
	bob.Must(200, "PUT", "/api/v1/me/dnd", map[string]any{"duration_seconds": 600})
	ann.Post(a, "are you there")
	if e := bobS.ExpectWhere("message.created", func(f map[string]any) bool { return obj(f, "message")["text"] == "are you there" }); e["notify"] != "badge" {
		t.Fatalf("in do not disturb: %v", e["notify"])
	}
	st = agree("in do not disturb")
	if st["dnd"] != true || st["dnd_until"] == nil {
		t.Fatalf("status in do not disturb: %v", st)
	}
	bob.Must(200, "DELETE", "/api/v1/me/dnd", nil)

	// The status endpoint: a strong ETag, 304 when nothing changed, and
	// a new tag when something did.
	tok := bob.Token()
	status, h, body := tok.Get("/api/v1/status", nil)
	etag := h.Get("ETag")
	if status != 200 || etag == "" || etag[0] != '"' || h.Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("status headers: %d %v", status, h)
	}
	var first map[string]any
	if err := json.Unmarshal(body, &first); err != nil || first["badge"] == nil {
		t.Fatalf("status body: %s", body)
	}
	if status, _, body := tok.Get("/api/v1/status", map[string]string{"If-None-Match": etag}); status != 304 || len(body) != 0 {
		t.Fatalf("nothing changed: %d %q", status, body)
	}
	ann.Post(a, "something new")
	status, h, _ = tok.Get("/api/v1/status", map[string]string{"If-None-Match": etag})
	if status != 200 || h.Get("ETag") == etag {
		t.Fatalf("something changed: %d %s", status, h.Get("ETag"))
	}
	agree("at the end")
	ts.Browser().Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/status", nil)

	// Its own limit: 30 a minute per session.
	limited := false
	for i := 0; i < 40; i++ {
		if status, _, _ := tok.Get("/api/v1/status", nil); status == 429 {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("the status endpoint has its own limit of 30 a minute")
	}
}

func TestThreadStatesAndTheThreadsView(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	ch := ann.Channel("plans", false, bob)
	setMode(bob, ch, "any")
	parent := ann.Post(ch, "Deploy plan for Friday")
	pid := str(parent, "id")
	reply := func(c *testkit.Client, text string) map[string]any {
		return obj(c.Must(201, "POST", "/api/v1/conversations/"+ch+"/messages", map[string]any{"text": text, "parent_id": pid}), "message")
	}
	bobS, _ := bob.Events(nil)

	// Neither: replies are shown without notifying, and nothing is stored
	// for a read position.
	r1 := reply(ann, "first reply")
	if e := bobS.Expect("message.created"); e["notify"] != "none" {
		t.Fatalf("a reply in a thread bob neither follows nor ignores: %v", e["notify"])
	}
	out := bob.Must(200, "PUT", "/api/v1/messages/"+pid+"/thread/read", map[string]any{"seq": r1["seq"]})
	if obj(out, "thread_state")["state"] != "none" {
		t.Fatalf("nothing is stored for neither: %v", out)
	}
	if n := len(arr(bob.Must(200, "GET", "/api/v1/threads", nil), "threads")); n != 0 {
		t.Fatalf("no followed threads yet: %d", n)
	}

	// Following: by hand, then replies notify and the thread is listed.
	ts1 := obj(bob.Must(200, "PUT", "/api/v1/messages/"+pid+"/thread/state", map[string]any{"state": "following"}), "thread_state")
	if ts1["state"] != "following" || ts1["parent_id"] != pid || ts1["conversation_id"] != ch {
		t.Fatalf("following: %v", ts1)
	}
	if e := bobS.Expect("thread_state.changed"); obj(e, "thread_state")["state"] != "following" || e["parent_id"] != pid || e["conversation_id"] != ch {
		t.Fatalf("thread_state.changed: %v", e)
	}
	r2 := reply(ann, "second reply")
	if e := bobS.Expect("message.created"); e["notify"] != "notify" {
		t.Fatalf("a reply in a followed thread: %v", e["notify"])
	}
	threads := arr(bob.Must(200, "GET", "/api/v1/threads", nil), "threads")
	if len(threads) != 1 || obj(threads[0].(map[string]any), "parent")["id"] != pid || obj(threads[0].(map[string]any), "thread_state")["unread_count"] == float64(0) {
		t.Fatalf("the Threads view: %v", threads)
	}
	if st := statusOf(bob); st["unread_threads"] != float64(1) {
		t.Fatalf("unread_threads: %v", st)
	}

	// Reading the thread takes it off the list.
	read := obj(bob.Must(200, "PUT", "/api/v1/messages/"+pid+"/thread/read", map[string]any{"seq": r2["seq"]}), "thread_state")
	if read["unread_count"] != float64(0) || read["read_seq"] != r2["seq"] {
		t.Fatalf("thread read: %v", read)
	}
	if e := bobS.ExpectWhere("thread_state.changed", func(f map[string]any) bool { return obj(f, "thread_state")["read_seq"] == r2["seq"] }); e["parent_id"] != pid {
		t.Fatalf("thread_state.changed on reading: %v", e)
	}
	if n := len(arr(bob.Must(200, "GET", "/api/v1/threads", nil), "threads")); n != 0 {
		t.Fatalf("read threads leave the list: %d", n)
	}
	if n := len(arr(bob.Must(200, "GET", "/api/v1/threads?all=true", nil), "threads")); n != 1 {
		t.Fatalf("with all, followed threads with nothing unread: %d", n)
	}
	if st := statusOf(bob); st["unread_threads"] != float64(0) {
		t.Fatalf("unread_threads after reading: %v", st)
	}

	// Ignored: replies do not notify; a mention by name still does.
	bob.Must(200, "PUT", "/api/v1/messages/"+pid+"/thread/state", map[string]any{"state": "ignored"})
	reply(ann, "third reply")
	if e := bobS.ExpectWhere("message.created", func(f map[string]any) bool { return obj(f, "message")["text"] == "third reply" }); e["notify"] != "none" {
		t.Fatalf("a reply in an ignored thread: %v", e["notify"])
	}
	reply(ann, "@bob please look")
	if e := bobS.ExpectWhere("message.created", func(f map[string]any) bool { return obj(f, "message")["text"] == "@bob please look" }); e["notify"] != "notify" {
		t.Fatalf("a mention by name in an ignored thread: %v", e["notify"])
	}
	// Back to neither: the row goes, and the event has no state.
	out = bob.Must(200, "PUT", "/api/v1/messages/"+pid+"/thread/state", map[string]any{"state": "none"})
	if out["thread_state"] != nil {
		t.Fatalf("neither has no state: %v", out)
	}
	if e := bobS.ExpectWhere("thread_state.changed", func(f map[string]any) bool { return f["thread_state"] == nil }); e["parent_id"] != pid {
		t.Fatalf("thread_state.changed for neither: %v", e)
	}
	// A reply names its thread as well as the parent does.
	bob.Must(200, "PUT", "/api/v1/messages/"+str(r1, "id")+"/thread/state", map[string]any{"state": "following"})

	// Refusals.
	bob.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/messages/"+pid+"/thread/state", map[string]any{"state": "muted"})
	bob.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/messages/"+pid+"/thread/read", map[string]any{"seq": -1})
	bob.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/messages/"+pid+"/thread/read", map[string]any{})
	bob.Refused(404, model.CodeNotFound, "PUT", "/api/v1/messages/"+string(model.NewID[model.MessageID]())+"/thread/state", map[string]any{"state": "following"})
	// cat can read the public channel but has not joined it.
	cat.Refused(403, model.CodeForbidden, "PUT", "/api/v1/messages/"+pid+"/thread/state", map[string]any{"state": "following"})
	priv := ann.Channel("den", true)
	hidden := str(ann.Post(priv, "secret"), "id")
	bob.Refused(404, model.CodeNotFound, "PUT", "/api/v1/messages/"+hidden+"/thread/state", map[string]any{"state": "following"})
	bob.Refused(404, model.CodeNotFound, "PUT", "/api/v1/messages/"+hidden+"/thread/read", map[string]any{"seq": 1})
}

func TestBookmarks(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	pub := ann.Channel("town", false)
	priv := ann.Channel("den", true, bob)
	m1 := str(ann.Post(pub, "in public"), "id")
	m2 := str(ann.Post(priv, "in private"), "id")
	bobS, _ := bob.Events(nil)
	annS, _ := ann.Events(nil)

	// Anything one can read can be bookmarked: a public channel one has
	// not joined, and a private one one is in.
	bob.Must(204, "PUT", "/api/v1/messages/"+m1+"/bookmark", nil)
	if e := bobS.Expect("bookmark.added"); e["message_id"] != m1 || e["created_at"] == nil {
		t.Fatalf("bookmark.added: %v", e)
	}
	bob.Must(204, "PUT", "/api/v1/messages/"+m1+"/bookmark", nil) // again changes nothing
	time.Sleep(2 * time.Millisecond)
	bob.Must(204, "PUT", "/api/v1/messages/"+m2+"/bookmark", nil)
	cat.Refused(404, model.CodeNotFound, "PUT", "/api/v1/messages/"+m2+"/bookmark", nil)
	bob.Refused(404, model.CodeNotFound, "PUT", "/api/v1/messages/"+string(model.NewID[model.MessageID]())+"/bookmark", nil)

	list := arr(bob.Must(200, "GET", "/api/v1/bookmarks", nil), "bookmarks")
	if len(list) != 2 || list[0].(map[string]any)["message_id"] != m2 || obj(list[0].(map[string]any), "message")["text"] != "in private" {
		t.Fatalf("newest first, each with its message: %v", list)
	}
	page := bob.Must(200, "GET", "/api/v1/bookmarks?limit=1", nil)
	if len(arr(page, "bookmarks")) != 1 || page["next_cursor"] == nil {
		t.Fatalf("paged: %v", page)
	}
	rest := bob.Must(200, "GET", "/api/v1/bookmarks?limit=1&cursor="+str(page, "next_cursor"), nil)
	if len(arr(rest, "bookmarks")) != 1 || rest["next_cursor"] != nil || arr(rest, "bookmarks")[0].(map[string]any)["message_id"] != m1 {
		t.Fatalf("second page: %v", rest)
	}
	// Nobody else sees them, and nobody else was told.
	if n := len(arr(ann.Must(200, "GET", "/api/v1/bookmarks", nil), "bookmarks")); n != 0 {
		t.Fatalf("ann sees bob's bookmarks: %d", n)
	}

	// A bookmark of a message one can no longer read is left out, and can
	// still be removed.
	bob.Must(204, "POST", "/api/v1/conversations/"+priv+"/leave", nil)
	list = arr(bob.Must(200, "GET", "/api/v1/bookmarks", nil), "bookmarks")
	if len(list) != 1 || list[0].(map[string]any)["message_id"] != m1 {
		t.Fatalf("after leaving the private channel: %v", list)
	}
	bob.Must(204, "DELETE", "/api/v1/messages/"+m2+"/bookmark", nil)
	if e := bobS.Expect("bookmark.removed"); e["message_id"] != m2 {
		t.Fatalf("bookmark.removed: %v", e)
	}
	bob.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/messages/"+m2+"/bookmark", nil)
	bob.Must(204, "DELETE", "/api/v1/messages/"+m1+"/bookmark", nil)
	if n := len(arr(bob.Must(200, "GET", "/api/v1/bookmarks", nil), "bookmarks")); n != 0 {
		t.Fatalf("all removed: %d", n)
	}
	// ann heard of the leaving and nothing about bookmarks.
	annS.Expect("member.left")
	annS.ExpectNone(200 * time.Millisecond)
}

// scrub removes what differs between two messages for reasons that have
// nothing to do with their targets.
func scrub(m map[string]any) string {
	cp := map[string]any{}
	for k, v := range m {
		switch k {
		case "id", "seq", "order", "created_at", "text", "entities":
		default:
			cp[k] = v
		}
	}
	b, _ := json.Marshal(cp)
	return string(b)
}

// TestNotifyAnyway is ruling R4 and cases 190 to 197: it always answers as
// if it worked, and nothing in any response or event says whether the
// target was reached.
func TestNotifyAnyway(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	reached, silenced, hidden, blocker, off, awake, outsider := ts.Member("reached"), ts.Member("silenced"), ts.Member("hidden"),
		ts.Member("blocker"), ts.Member("off"), ts.Member("awake"), ts.Member("outsider")
	targets := []*testkit.Client{reached, silenced, hidden, blocker, off, awake}
	ch := ann.Channel("ops", true, targets...)
	for _, c := range targets {
		setMode(c, ch, "any")
		if c != awake {
			c.Must(200, "PUT", "/api/v1/me/dnd", nil)
		}
	}
	setMode(silenced, ch, "nothing")
	// Hidden from the sidebar only: it still notifies (ruling R46).
	hidden.Must(200, "PATCH", "/api/v1/conversations/"+ch+"/membership", map[string]any{"show": "hidden"})
	blocker.Must(204, "PUT", "/api/v1/me/blocks/"+string(ann.ID), nil)
	off.Must(200, "PATCH", "/api/v1/me/prefs", map[string]any{"allow_notify_anyway": false})

	streams := map[*testkit.Client]*testkit.Stream{}
	for _, c := range targets {
		streams[c], _ = c.Events(nil)
	}
	annS, _ := ann.Events(nil)

	// One message per target, so each answer and each set of events can
	// be compared with the others.
	type result struct {
		answer string
		events []string
	}
	results := map[*testkit.Client]result{}
	delivered := func(msg string, to *testkit.Client) bool {
		got, err := ts.Server.Store.NotifyAnyway().Delivered(bg, model.MessageID(msg), to.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	all := append(append([]*testkit.Client{}, targets...), outsider)
	ids := map[*testkit.Client]string{}
	for _, c := range all {
		posted := ann.Post(ch, "ping")
		id := str(posted, "id")
		ids[c] = id
		if posted["notified_anyway"] != nil {
			t.Fatalf("no mark before it is used: %v", posted)
		}
		annS.ExpectWhere("message.created", func(f map[string]any) bool { return obj(f, "message")["id"] == id })
		out := ann.Must(200, "POST", "/api/v1/messages/"+id+"/notify-anyway", map[string]any{"account_id": c.ID})
		msg := obj(out, "message")
		if msg["notified_anyway"] != true || len(out) != 1 {
			t.Fatalf("the answer is the message with its mark and nothing else: %v", out)
		}
		// What the sender's own connection is sent because of it.
		var events []string
		e := annS.ExpectWhere("message.edited", func(f map[string]any) bool { return obj(f, "message")["id"] == id })
		events = append(events, e["type"].(string)+" "+scrub(obj(e, "message")))
		results[c] = result{answer: scrub(msg), events: events}
		// Asking again changes nothing and answers the same.
		again := ann.Must(200, "POST", "/api/v1/messages/"+id+"/notify-anyway", map[string]any{"account_id": c.ID})
		if scrub(obj(again, "message")) != scrub(msg) {
			t.Fatalf("asking again answers the same: %v", again)
		}
		// Anyone who fetches the message sees the mark and nothing more.
		fetched := obj(awake.Must(200, "GET", "/api/v1/messages/"+id, nil), "message")
		if fetched["notified_anyway"] != true || scrub(fetched) != scrub(msg) {
			t.Fatalf("case 196: the fetched message: %v", fetched)
		}
	}
	// Cases 190 to 195: the same answer and the same events every time.
	want := results[reached]
	for c, got := range results {
		if got.answer != want.answer || fmt.Sprint(got.events) != fmt.Sprint(want.events) {
			t.Fatalf("the answer or the sender's events differ for %s:\n%v\n%v", c.Username, got, want)
		}
	}
	// Only in the table that is never given out does it differ.
	for c, wantDelivered := range map[*testkit.Client]bool{reached: true, silenced: false, hidden: true, blocker: false, off: false, awake: false, outsider: false} {
		if got := delivered(ids[c], c); got != wantDelivered {
			t.Errorf("notify_anyway.delivered for %s = %v, want %v", c.Username, got, wantDelivered)
		}
	}

	// Case 190: the target who is reached is notified as if not in do not
	// disturb; first the message arrived as a badge.
	first := streams[reached].ExpectWhere("message.created", func(f map[string]any) bool { return obj(f, "message")["id"] == ids[reached] })
	if first["notify"] != "badge" {
		t.Fatalf("in do not disturb the message first only counts: %v", first["notify"])
	}
	pushed := streams[reached].ExpectWhere("message.created", func(f map[string]any) bool { return obj(f, "message")["id"] == ids[reached] })
	if pushed["notify"] != "notify" || obj(pushed, "message")["notified_anyway"] != true {
		t.Fatalf("pushed through: %v", pushed)
	}
	// Nobody who was not reached is sent the message again.
	for _, c := range []*testkit.Client{silenced, blocker, off} {
		n := 0
		for {
			f, ok := streams[c].Next(300 * time.Millisecond)
			if !ok {
				break
			}
			if f["type"] == "message.created" && obj(f, "message")["id"] == ids[c] {
				n++
				if f["notify"] == "notify" {
					t.Fatalf("%s was notified: %v", c.Username, f)
				}
			}
		}
		if n != 1 {
			t.Fatalf("%s got its message %d times, want once", c.Username, n)
		}
	}
	// On catch-up the message is never `notify`, and nothing differs.
	evs := arr(reached.Must(200, "GET", "/api/v1/conversations/"+ch+"/events?after=0", nil), "events")
	for _, v := range evs {
		if e := v.(map[string]any); e["type"] == "message.created" && e["notify"] == "notify" {
			t.Fatalf("catch-up never says notify: %v", e)
		}
	}

	// The refusals are about the caller, never the target.
	own := str(ann.Post(ch, "mine"), "id")
	reached.Refused(403, model.CodeForbidden, "POST", "/api/v1/messages/"+own+"/notify-anyway", map[string]any{"account_id": silenced.ID}) // case 197
	outsider.Refused(404, model.CodeNotFound, "POST", "/api/v1/messages/"+own+"/notify-anyway", map[string]any{"account_id": reached.ID})
	ann.Refused(404, model.CodeNotFound, "POST", "/api/v1/messages/"+own+"/notify-anyway", map[string]any{"account_id": model.NewID[model.AccountID]()})
	ann.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/messages/"+own+"/notify-anyway", map[string]any{"account_id": "nobody"})
	ann.Refused(400, model.CodeInvalidRequest, "POST", "/api/v1/messages/"+own+"/notify-anyway", map[string]any{"account_ids": []string{}})
	ann.Refused(404, model.CodeNotFound, "POST", "/api/v1/messages/"+string(model.NewID[model.MessageID]())+"/notify-anyway", map[string]any{"account_id": reached.ID})
	// A guest who cannot see the named account gets not_found for it.
	lobby := ts.Owner.Channel("lobby", true, ann)
	gus := ts.Guest("gus", lobby)
	theirs := str(gus.Post(lobby, "hello"), "id")
	gus.Refused(404, model.CodeNotFound, "POST", "/api/v1/messages/"+theirs+"/notify-anyway", map[string]any{"account_id": reached.ID})
	gus.Must(200, "POST", "/api/v1/messages/"+theirs+"/notify-anyway", map[string]any{"account_id": ann.ID})
}

func TestTyping(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	ch := ann.Channel("town", false, bob)
	parent := str(ann.Post(ch, "a thread"), "id")
	annS, _ := ann.Events(nil)
	bobS, _ := bob.Events(nil)

	bob.Must(204, "POST", "/api/v1/conversations/"+ch+"/typing", map[string]any{"parent_id": parent})
	if e := annS.Expect("typing.started"); e["account_id"] != string(bob.ID) || e["parent_id"] != parent || e["conversation_id"] != ch || e["seq"] != nil {
		t.Fatalf("typing.started in a thread: %v", e)
	}
	// The live frame does the same.
	annS.Send(map[string]any{"type": "typing", "conversation_id": ch, "id": "t1"})
	if e := bobS.Expect("typing.started"); e["account_id"] != string(ann.ID) || e["parent_id"] != nil {
		t.Fatalf("typing.started from the frame: %v", e)
	}
	annS.Expect("ack")
	annS.Send(map[string]any{"type": "typing", "id": "t2"})
	if e := annS.Expect("error"); e["reply_to"] != "t2" {
		t.Fatalf("a typing frame without a conversation: %v", e)
	}
	// Not for someone who has not joined, nor with a parent from elsewhere.
	cat.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+ch+"/typing", nil)
	other := str(ann.Post(ann.Channel("elsewhere", false), "x"), "id")
	bob.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/conversations/"+ch+"/typing", map[string]any{"parent_id": other})
	bob.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/conversations/"+ch+"/typing", map[string]any{"parent_id": "nope"})
	// The typist is not told, and it is never stored.
	bobS.ExpectNone(200 * time.Millisecond)
	for _, v := range arr(ann.Must(200, "GET", "/api/v1/conversations/"+ch+"/events?after=0", nil), "events") {
		if v.(map[string]any)["type"] == "typing.started" {
			t.Fatal("typing is not stored")
		}
	}
}

func TestAdminAccounts(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	admin, admin2 := ts.Admin("adm"), ts.Admin("adm2")
	ann, bob := ts.Member("ann"), ts.Member("bob")
	general := ""
	for _, v := range arr(owner.Must(200, "GET", "/api/v1/conversations", nil), "conversations") {
		if v.(map[string]any)["name"] == "general" {
			general = str(v.(map[string]any), "id")
		}
	}
	priv := owner.Channel("den", true, bob)
	imported := ts.Imported("oldtimer")

	// The admin list: every account, with sign-in details and no sessions.
	ann.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/accounts", nil)
	ann.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/accounts/"+string(bob.ID), nil)
	list := arr(admin.Must(200, "GET", "/api/v1/admin/accounts", nil), "accounts")
	if len(list) != 6 {
		t.Fatalf("the admin list has %d accounts, want 6", len(list))
	}
	got := obj(admin.Must(200, "GET", "/api/v1/admin/accounts/"+string(bob.ID), nil), "account")
	if got["passkey_count"] != float64(1) || got["has_totp"] != false || got["banned"] != false || got["provider_ids"] == nil || got["claimed_at"] == nil || got["last_seen_at"] == nil || got["sessions"] != nil {
		t.Fatalf("admin account: %v", got)
	}
	if n := len(arr(admin.Must(200, "GET", "/api/v1/admin/accounts?state=unclaimed", nil), "accounts")); n != 1 {
		t.Fatalf("unclaimed: %d", n)
	}
	if n := len(arr(admin.Must(200, "GET", "/api/v1/admin/accounts?role=admin", nil), "accounts")); n != 2 {
		t.Fatalf("by role: %d", n)
	}
	if n := len(arr(admin.Must(200, "GET", "/api/v1/admin/accounts?q=AN", nil), "accounts")); n != 1 {
		t.Fatalf("by name: %d", n)
	}
	page := admin.Must(200, "GET", "/api/v1/admin/accounts?limit=4", nil)
	if len(arr(page, "accounts")) != 4 || page["next_cursor"] == nil {
		t.Fatalf("paged: %v", page)
	}
	admin.Refused(400, model.CodeInvalidValue, "GET", "/api/v1/admin/accounts?state=asleep", nil)
	admin.Refused(400, model.CodeInvalidValue, "GET", "/api/v1/admin/accounts?role=king", nil)
	admin.Refused(404, model.CodeNotFound, "GET", "/api/v1/admin/accounts/"+string(model.NewID[model.AccountID]()), nil)

	// Claim links: only for an imported account nobody has claimed.
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/accounts/"+string(imported)+"/claim-link", nil)
	admin.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/accounts/"+string(bob.ID)+"/claim-link", nil)
	admin.Refused(404, model.CodeNotFound, "POST", "/api/v1/admin/accounts/"+string(model.NewID[model.AccountID]())+"/claim-link", nil)
	link := obj(admin.Must(201, "POST", "/api/v1/admin/accounts/"+string(imported)+"/claim-link", nil), "invite")
	if link["claims_account_id"] != string(imported) || link["max_uses"] != float64(1) || link["expires_at"] == nil || link["url"] == nil {
		t.Fatalf("claim link: %v", link)
	}
	second := obj(admin.Must(201, "POST", "/api/v1/admin/accounts/"+string(imported)+"/claim-link", nil), "invite")
	if inv, err := ts.Server.Store.Invites().Get(bg, model.InviteID(str(link, "id"))); err != nil || inv.RevokedAt.IsZero() || second["code"] == link["code"] {
		t.Fatalf("a fresh link revokes the earlier one: %v %v", inv, err)
	}

	// Roles: the owner only.
	role := func(c *testkit.Client, id model.AccountID, r string) (int, map[string]any) {
		return c.Do("PUT", "/api/v1/admin/accounts/"+string(id)+"/role", map[string]any{"role": r})
	}
	admin.Refused(403, model.CodeForbidden, "PUT", "/api/v1/admin/accounts/"+string(ann.ID)+"/role", map[string]any{"role": "admin"})
	ann.Refused(403, model.CodeForbidden, "PUT", "/api/v1/admin/accounts/"+string(bob.ID)+"/role", map[string]any{"role": "guest"})
	owner.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/admin/accounts/"+string(ann.ID)+"/role", map[string]any{"role": "bot"})
	owner.Refused(404, model.CodeNotFound, "PUT", "/api/v1/admin/accounts/"+string(model.NewID[model.AccountID]())+"/role", map[string]any{"role": "admin"})
	owner.Refused(409, model.CodeConflict, "PUT", "/api/v1/admin/accounts/"+string(owner.ID)+"/role", map[string]any{"role": "admin"}) // the last owner
	owner.Refused(409, model.CodeConflict, "PUT", "/api/v1/admin/accounts/"+string(imported)+"/role", map[string]any{"role": "admin"})

	bobS, _ := bob.Events(nil)
	ownerS, _ := owner.Events(nil)
	// Case 106: member to guest leaves the channels it was in only
	// through @everyone, and keeps the ones it is in directly.
	bob.Must(200, "GET", "/api/v1/conversations/"+general, nil)
	if status, out := role(owner, bob.ID, "guest"); status != 200 || obj(out, "account")["role"] != "guest" {
		t.Fatalf("member to guest: %d %v", status, out)
	}
	if e := bobS.ExpectWhere("member.left", func(f map[string]any) bool { return f["conversation_id"] == general }); e["reason"] != "group" {
		t.Fatalf("leaving the @everyone channel: %v", e)
	}
	if e := bobS.Expect("account.updated"); obj(e, "account")["role"] != "guest" {
		t.Fatalf("account.updated with the new role: %v", e)
	}
	if e := ownerS.ExpectWhere("account.updated", func(f map[string]any) bool { return obj(f, "account")["id"] == string(bob.ID) }); obj(e, "account")["role"] != "guest" {
		t.Fatalf("those who share a conversation hear of the role: %v", e)
	}
	bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+general, nil)
	bob.Must(200, "GET", "/api/v1/conversations/"+priv, nil)
	// And back: a member is in @everyone again.
	role(owner, bob.ID, "member")
	bob.Must(200, "GET", "/api/v1/conversations/"+general, nil)
	if status, out := role(owner, ann.ID, "admin"); status != 200 || obj(out, "account")["role"] != "admin" {
		t.Fatalf("member to admin: %d %v", status, out)
	}
	ann.Must(200, "GET", "/api/v1/admin/accounts", nil)
	role(owner, ann.ID, "member")
	ann.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/accounts", nil)

	// Removal: who may remove whom (decision P15).
	ann.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/accounts/"+string(bob.ID), nil)
	admin.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/accounts/"+string(admin2.ID), nil)
	admin.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/accounts/"+string(owner.ID), nil)
	owner.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/accounts/"+string(owner.ID), nil)
	admin.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/admin/accounts/"+string(model.NewID[model.AccountID]()), nil)

	// Removing bob: his sessions and tokens end, his connection closes,
	// he leaves every conversation, his messages stay, his name stays
	// taken.
	msg := str(bob.Post(priv, "I was here"), "id")
	token := bob.Token()
	token.Must(200, "GET", "/api/v1/me", nil)
	admin.Must(204, "DELETE", "/api/v1/admin/accounts/"+string(bob.ID), nil)
	if code := bobS.CloseCode(5 * time.Second); code != 4003 {
		t.Fatalf("case 135: the removed account's connection closes with 4003, got %d", code)
	}
	bob.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)   // case 134
	token.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil) // case 134
	if e := ownerS.ExpectWhere("member.left", func(f map[string]any) bool { return f["conversation_id"] == priv }); e["reason"] != "account_removed" || e["account_id"] != string(bob.ID) || e["by_id"] != string(admin.ID) {
		t.Fatalf("member.left in each conversation: %v", e)
	}
	if e := ownerS.Expect("account.removed"); obj(e, "account")["id"] != string(bob.ID) || obj(e, "account")["removed_at"] == nil {
		t.Fatalf("account.removed to those who could see it: %v", e)
	}
	for _, v := range arr(owner.Must(200, "GET", "/api/v1/conversations/"+priv+"/members", nil), "members") {
		if v.(map[string]any)["account_id"] == string(bob.ID) {
			t.Fatal("case 136: a removed account is still listed as a member")
		}
	}
	if kept := obj(owner.Must(200, "GET", "/api/v1/messages/"+msg, nil), "message"); kept["text"] != "I was here" || kept["author_id"] != string(bob.ID) {
		t.Fatalf("case 136: its messages stay: %v", kept)
	}
	if former := obj(owner.Must(200, "GET", "/api/v1/accounts/"+string(bob.ID), nil), "account"); former["removed_at"] == nil || former["username"] != "bob" {
		t.Fatalf("shown as a former member: %v", former)
	}
	// Case 138: the username stays taken.
	code := str(obj(owner.Must(201, "POST", "/api/v1/invites", map[string]any{}), "invite"), "code")
	ts.Browser().Refused(409, model.CodeNameTaken, "POST", "/api/v1/passkey/register/begin", map[string]any{"invite_code": code, "username": "bob", "display_name": "Bob Two"})
	// Case 137: it cannot be chatted with.
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID)}})
	removed := obj(admin.Must(200, "GET", "/api/v1/admin/accounts/"+string(bob.ID), nil), "account")
	if removed["removed_at"] == nil || removed["removed_by_id"] != string(admin.ID) || removed["passkey_count"] != float64(0) {
		t.Fatalf("the admin view of a removed account: %v", removed)
	}
	if n := len(arr(admin.Must(200, "GET", "/api/v1/admin/accounts?state=removed", nil), "accounts")); n != 1 {
		t.Fatalf("removed: %d", n)
	}
	admin.Must(204, "DELETE", "/api/v1/admin/accounts/"+string(bob.ID), nil) // already removed: nothing more to do
	// Only the owner removes an admin.
	owner.Must(204, "DELETE", "/api/v1/admin/accounts/"+string(admin2.ID), nil)
	admin2.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)

	// Making another owner: the caller stays one (ruling R99; cases 315
	// to 322 are in roles_test.go).
	if status, out := role(owner, ann.ID, "owner"); status != 200 || obj(out, "account")["role"] != "owner" {
		t.Fatalf("making an owner: %d %v", status, out)
	}
	if me := obj(owner.Must(200, "GET", "/api/v1/me", nil), "account"); me["role"] != "owner" {
		t.Fatalf("the first owner is still one: %v", me["role"])
	}
	if status, _ := role(ann, admin.ID, "member"); status != 200 {
		t.Fatalf("the new owner changes roles: %d", status)
	}
	audit, _, err := ts.Server.Store.Audit().List(bg, store.AuditFilter{Action: "owner.made"}, store.Page{})
	if err != nil || len(audit) != 1 {
		t.Fatalf("making an owner is in the audit log: %v %v", audit, err)
	}
}

func TestBans(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	admin, admin2 := ts.Admin("adm"), ts.Admin("adm2")
	ann, bob := ts.Member("ann"), ts.Member("bob")
	bobS, _ := bob.Events(nil)

	ann.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/bans", nil)
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/bans", map[string]any{"account_id": bob.ID})
	admin.Refused(400, model.CodeInvalidRequest, "POST", "/api/v1/admin/bans", map[string]any{"reason": "nobody named"})
	admin.Refused(400, model.CodeInvalidRequest, "POST", "/api/v1/admin/bans", map[string]any{"account_id": bob.ID, "provider_id": model.NewID[model.GroupID](), "subject": "x"})
	admin.Refused(400, model.CodeInvalidRequest, "POST", "/api/v1/admin/bans", map[string]any{"provider_id": model.NewID[model.GroupID]()})
	admin.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/bans", map[string]any{"account_id": "bob"})
	admin.Refused(404, model.CodeNotFound, "POST", "/api/v1/admin/bans", map[string]any{"account_id": model.NewID[model.AccountID]()})
	// Only the owner bans an admin; nobody bans the owner.
	admin.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/bans", map[string]any{"account_id": admin2.ID})
	admin.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/bans", map[string]any{"account_id": owner.ID})
	owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/bans", map[string]any{"account_id": owner.ID})

	// A ban is a removal plus a row.
	ban := obj(admin.Must(201, "POST", "/api/v1/admin/bans", map[string]any{"account_id": bob.ID, "reason": "spam"}), "ban")
	if ban["account_id"] != string(bob.ID) || ban["reason"] != "spam" || ban["created_by_id"] != string(admin.ID) || ban["lifted_at"] != nil || ban["provider_id"] != nil {
		t.Fatalf("ban: %v", ban)
	}
	if code := bobS.CloseCode(5 * time.Second); code != 4003 {
		t.Fatalf("a banned account's connection closes with 4003, got %d", code)
	}
	bob.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	if got := obj(admin.Must(200, "GET", "/api/v1/admin/accounts/"+string(bob.ID), nil), "account"); got["banned"] != true || got["removed_at"] == nil {
		t.Fatalf("the account shows as banned and removed: %v", got)
	}
	// An identity that has no account.
	provider := string(model.NewID[model.GroupID]())
	idBan := obj(admin.Must(201, "POST", "/api/v1/admin/bans", map[string]any{"provider_id": provider, "subject": "user-17", "reason": "known"}), "ban")
	if idBan["provider_id"] != provider || idBan["subject"] != "user-17" || idBan["account_id"] != nil {
		t.Fatalf("identity ban: %v", idBan)
	}
	if banned, err := ts.Server.Store.Accounts().IdentityBanned(bg, provider, "user-17"); err != nil || !banned {
		t.Fatalf("case 139: the identity is refused at the door: %v %v", banned, err)
	}
	owner.Must(201, "POST", "/api/v1/admin/bans", map[string]any{"account_id": admin2.ID})

	list := admin.Must(200, "GET", "/api/v1/admin/bans?limit=2", nil)
	if len(arr(list, "bans")) != 2 || list["next_cursor"] == nil {
		t.Fatalf("bans, paged: %v", list)
	}
	rest := admin.Must(200, "GET", "/api/v1/admin/bans?limit=2&cursor="+str(list, "next_cursor"), nil)
	if len(arr(rest, "bans")) != 1 || arr(rest, "bans")[0].(map[string]any)["id"] != ban["id"] {
		t.Fatalf("newest first: %v", rest)
	}

	// Lifting a ban does not bring the account back.
	ann.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/bans/"+str(idBan, "id"), nil)
	admin.Must(204, "DELETE", "/api/v1/admin/bans/"+str(idBan, "id"), nil)
	admin.Must(204, "DELETE", "/api/v1/admin/bans/"+str(ban, "id"), nil)
	admin.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/admin/bans/"+string(model.NewID[model.GroupID]()), nil)
	if banned, _ := ts.Server.Store.Accounts().IdentityBanned(bg, provider, "user-17"); banned {
		t.Fatal("a lifted ban no longer refuses")
	}
	if got := obj(admin.Must(200, "GET", "/api/v1/admin/accounts/"+string(bob.ID), nil), "account"); got["banned"] != false || got["removed_at"] == nil {
		t.Fatalf("lifted: not banned, still removed: %v", got)
	}
	bob.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	if lifted := arr(admin.Must(200, "GET", "/api/v1/admin/bans", nil), "bans"); len(lifted) != 3 {
		t.Fatalf("lifted bans stay in the list: %d", len(lifted))
	}
}
