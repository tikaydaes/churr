package api_test

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/live"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

func obj(m map[string]any, keys ...string) map[string]any {
	for _, k := range keys {
		m, _ = m[k].(map[string]any)
	}
	return m
}

func arr(m map[string]any, key string) []any { l, _ := m[key].([]any); return l }

// TestPasskeyAsksForPRF checks that every creation asks for the PRF
// extension and that what the browser reported is recorded.
func TestPasskeyAsksForPRF(t *testing.T) {
	ts := testkit.NewServer(t, testkit.Options{NoOwner: true})
	b := ts.Browser()
	begin := b.Must(200, "POST", "/api/v1/setup/begin", map[string]any{"setup_token": ts.SetupToken(), "username": "owner", "display_name": "Owner"})
	pk := obj(begin, "public_key")
	if _, asked := obj(pk, "extensions")["prf"]; !asked {
		t.Fatalf("creation options do not ask for prf: %v", pk["extensions"])
	}
	if obj(pk, "authenticatorSelection")["userVerification"] != "required" || obj(pk, "authenticatorSelection")["residentKey"] != "required" {
		t.Fatalf("user verification and a resident key must be required: %v", pk["authenticatorSelection"])
	}
	ts.Owner = ts.Browser()
	ts.Owner.Setup("owner")
	keys, err := ts.Server.Store.Passkeys().ForAccount(context.Background(), ts.Owner.ID)
	if err != nil || len(keys) != 1 || !keys[0].HasPRF {
		t.Fatalf("has_prf should be recorded as the browser reported it: %v %v", keys, err)
	}
}

// TestConversations covers creating, browsing, joining, ad-hoc chats,
// members, leaving and the read position.
func TestConversations(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	ann, bob := ts.Member("ann"), ts.Member("bob")

	pub := ann.Channel("ops", false)
	priv := ann.Channel("secret-plans", true)
	// Channel names are not unique (ruling R50). Case 236: a name a private
	// channel has, which the caller cannot see, is created like any other,
	// and nothing in the answer differs.
	ann.Must(201, "POST", "/api/v1/channels", map[string]any{"name": "ops", "private": false})
	same := obj(ts.Member("cat").Must(201, "POST", "/api/v1/channels", map[string]any{"name": "secret-plans", "private": true}), "conversation")
	if same["name"] != "secret-plans" || same["id"] == priv {
		t.Fatalf("a second channel of the same name: %v", same)
	}
	ann.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/channels", map[string]any{"name": "bad\u0007name", "private": false})

	// Browsing shows every public channel and the private ones one is in.
	names := func(c *testkit.Client) map[string]bool {
		out := map[string]bool{}
		for _, v := range arr(c.Must(200, "GET", "/api/v1/channels", nil), "conversations") {
			out[v.(map[string]any)["name"].(string)] = true
		}
		return out
	}
	if n := names(bob); !n["ops"] || !n["general"] || n["secret-plans"] {
		t.Fatalf("bob browses %v", n)
	}
	if n := names(ann); !n["secret-plans"] {
		t.Fatalf("ann browses %v", n)
	}

	// Reading a public channel needs no membership; taking part does.
	c := obj(bob.Must(200, "GET", "/api/v1/conversations/"+pub, nil), "conversation")
	if c["membership"] != nil {
		t.Fatal("a non-member has no membership")
	}
	bob.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+pub+"/messages", map[string]any{"text": "hi"}) // case 19
	joined := obj(bob.Must(200, "POST", "/api/v1/conversations/"+pub+"/join", nil), "conversation")
	if obj(joined, "membership")["level"] != "member" || obj(joined, "membership")["mode"] != "mentions" {
		t.Fatalf("membership after joining: %v", joined["membership"])
	}
	bob.Must(200, "POST", "/api/v1/conversations/"+pub+"/join", nil) // joining again changes nothing
	bob.Post(pub, "hello ops")

	// A private channel does not exist for an outsider, the owner included.
	for _, who := range []*testkit.Client{bob, owner} {
		who.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv, nil)                                                                 // case 2
		who.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv+"/messages", nil)                                                     // cases 1, 23
		who.Refused(404, model.CodeNotFound, "POST", "/api/v1/conversations/"+priv+"/join", nil)                                                        // case 27
		who.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv+"/members", nil)                                                      //
		who.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv+"/events?after=0", nil)                                               // case 7
		who.Refused(404, model.CodeNotFound, "POST", "/api/v1/conversations/"+priv+"/members", map[string]any{"account_ids": []string{string(who.ID)}}) // case 28
		who.Refused(404, model.CodeNotFound, "PUT", "/api/v1/conversations/"+priv+"/read", map[string]any{"seq": 1})
	}

	// Members: ann adds bob to the private channel; bob is then in.
	added := arr(ann.Must(200, "POST", "/api/v1/conversations/"+priv+"/members", map[string]any{"account_ids": []string{string(bob.ID)}}), "members")
	if len(added) != 1 || added[0].(map[string]any)["level"] != "member" || added[0].(map[string]any)["invited"] != true {
		t.Fatalf("addMembers: %v", added)
	}
	bob.AcceptInvitation(priv)
	bob.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+priv+"/members", map[string]any{"account_ids": []string{string(owner.ID)}}) // case 55
	members := arr(bob.Must(200, "GET", "/api/v1/conversations/"+priv+"/members", nil), "members")
	if len(members) != 2 || members[0].(map[string]any)["level"] != "manager" {
		t.Fatalf("members: %v", members)
	}
	if n := len(arr(bob.Must(200, "GET", "/api/v1/conversations/"+priv+"/members?level=manager", nil), "members")); n != 1 {
		t.Fatalf("managers: %d", n)
	}

	// Read position and counts.
	ann.Post(priv, "one")
	ann.Post(priv, "two @bob")
	conv := obj(bob.Must(200, "GET", "/api/v1/conversations/"+priv, nil), "conversation")
	ms := obj(conv, "membership")
	if ms["unread_count"] != float64(2) || ms["mention_count"] != float64(1) || ms["badge_count"] != float64(1) {
		t.Fatalf("counts before reading: %v", ms)
	}
	ms = obj(bob.Must(200, "PUT", "/api/v1/conversations/"+priv+"/read", map[string]any{"seq": conv["last_seq"]}), "membership")
	if ms["unread_count"] != float64(0) || ms["read_seq"] != conv["last_seq"] {
		t.Fatalf("counts after reading: %v", ms)
	}

	// Leaving: the last manager going promotes the longest-standing
	// member (case 67); the last member going deletes a private channel.
	ann.Must(204, "POST", "/api/v1/conversations/"+priv+"/leave", nil)
	ann.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv, nil) // case 20's shape
	if lvl := obj(bob.Must(200, "GET", "/api/v1/conversations/"+priv, nil), "conversation", "membership")["level"]; lvl != "manager" {
		t.Fatalf("bob should have become manager, is %v", lvl)
	}
	bob.Must(204, "POST", "/api/v1/conversations/"+priv+"/leave", nil)
	if _, err := ts.Server.Store.Conversations().Get(context.Background(), model.ConversationID(priv)); model.CodeOf(err) != model.CodeNotFound {
		t.Fatalf("a private channel with nobody left should be deleted: %v", err)
	}
	// The name is free again.
	ann.Channel("secret-plans", true)

	// Ad-hoc chats are found by who is in them.
	status, out := ann.Do("POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID)}})
	if status != 201 {
		t.Fatalf("openChat: %d %v", status, out)
	}
	chat := obj(out, "conversation")
	if chat["private"] != true || chat["name"] != nil || len(arr(chat, "member_ids")) != 2 {
		t.Fatalf("ad-hoc chat: %v", chat)
	}
	status, out = bob.Do("POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(ann.ID)}})
	if status != 200 || obj(out, "conversation")["id"] != chat["id"] {
		t.Fatalf("the same people should find the same chat: %d %v", status, out)
	}
	status, _ = ann.Do("POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID), string(owner.ID)}})
	if status != 201 {
		t.Fatalf("a different set of people is a different chat: %d", status)
	}
	owner.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+chat["id"].(string), nil) // case 34
	ann.Refused(404, model.CodeNotFound, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(model.NewID[model.AccountID]())}})

	// Guests: only what they are in (cases 10, 13, 74, 75).
	guest := ts.Guest("gil", pub)
	if n := names(guest); len(n) != 1 || !n["ops"] {
		t.Fatalf("a guest browses %v", n)
	}
	general := ""
	for _, v := range arr(owner.Must(200, "GET", "/api/v1/conversations", nil), "conversations") {
		general = v.(map[string]any)["id"].(string)
	}
	guest.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+general+"/messages", nil)
	guest.Refused(404, model.CodeNotFound, "POST", "/api/v1/conversations/"+general+"/join", nil)
	guest.Refused(403, model.CodeForbidden, "POST", "/api/v1/channels", map[string]any{"name": "guests", "private": false})
	guest.Refused(403, model.CodeForbidden, "POST", "/api/v1/invites", map[string]any{})
	guest.Refused(404, model.CodeNotFound, "GET", "/api/v1/accounts/"+string(owner.ID), nil)
	guest.Must(200, "GET", "/api/v1/accounts/"+string(bob.ID), nil) // they share #ops
	guest.Post(pub, "hello from a guest")
}

// TestMessages covers posting, paging, editing inside and after the
// quick-correction window, deleting, reactions, threads and search.
func TestMessages(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ch := ann.Channel("talk", false, bob)
	path := "/api/v1/conversations/" + ch + "/messages"

	// Entities are resolved by the server, positions in UTF-16 units.
	posted := ann.Must(201, "POST", path, map[string]any{"text": "🎉 **Deploy** at 5. @bob see <#" + ch + "> and #talk and https://churr.example/x\n\n- [ ] tag\n- [x] tell @nobody"})
	msg := obj(posted, "message")
	types := map[string]map[string]any{}
	for _, e := range arr(msg, "entities") {
		types[e.(map[string]any)["type"].(string)] = e.(map[string]any)
	}
	if types["account"]["account_id"] != string(bob.ID) || types["account"]["at"] != float64(20) {
		t.Fatalf("account entity: %v", types["account"])
	}
	if types["conversation"]["conversation_id"] != ch || types["link"]["url"] != "https://churr.example/x" {
		t.Fatalf("entities: %v", msg["entities"])
	}
	tasks := arr(msg, "tasks")
	if len(tasks) != 2 || tasks[0].(map[string]any)["id"] != "t1" || tasks[1].(map[string]any)["checked"] != true {
		t.Fatalf("tasks: %v", tasks)
	}
	if msg["client_id"] != nil || msg["seq"] == nil {
		t.Fatalf("message: %v", msg)
	}
	id := msg["id"].(string)

	// Sending the same client_id again returns the first message.
	cid := string(model.NewID[model.MessageID]())
	first := obj(ann.Must(201, "POST", path, map[string]any{"text": "once", "client_id": cid}), "message")
	again := obj(ann.Must(200, "POST", path, map[string]any{"text": "once", "client_id": cid}), "message")
	if first["id"] != again["id"] {
		t.Fatal("a repeated client_id made a second message")
	}
	ann.Refused(400, model.CodeInvalidRequest, "POST", path, map[string]any{})
	ann.Refused(400, model.CodeInvalidRequest, "POST", path, map[string]any{"text": "x", "colour": "red"})
	ann.Refused(403, model.CodeForbidden, "POST", path, map[string]any{"text": "x", "cards": []any{map[string]any{"title": "no"}}}) // case 63

	// A mention of someone who cannot read is drawn and reported.
	priv := ann.Channel("quiet", true)
	out := ann.Must(201, "POST", "/api/v1/conversations/"+priv+"/messages", map[string]any{"text": "ask @bob"})
	if ids := arr(out, "cannot_read_ids"); len(ids) != 1 || ids[0] != string(bob.ID) {
		t.Fatalf("cannot_read_ids: %v", out["cannot_read_ids"])
	}

	// Paging by seq.
	for i := 0; i < 5; i++ {
		ann.Post(ch, fmt.Sprintf("filler %d", i))
	}
	page := bob.Must(200, "GET", path+"?limit=3", nil)
	if len(arr(page, "messages")) != 3 || page["has_more"] != true {
		t.Fatalf("newest page: %v", page)
	}
	oldest := arr(page, "messages")[0].(map[string]any)["seq"].(float64)
	older := bob.Must(200, "GET", fmt.Sprintf("%s?limit=200&before=%d", path, int(oldest)), nil)
	if older["has_more"] != false || len(arr(older, "messages"))+3 != 7 {
		t.Fatalf("older page: %d messages, has_more %v", len(arr(older, "messages")), older["has_more"])
	}
	bob.Refused(400, model.CodeInvalidValue, "GET", path+"?limit=abc", nil)

	// Reactions: anyone in the channel; the entry is the reader's own view.
	thumb := url.PathEscape("👍")
	r := obj(bob.Must(200, "PUT", "/api/v1/messages/"+id+"/reactions/"+thumb, nil), "reaction")
	if r["count"] != float64(1) || r["me"] != true {
		t.Fatalf("reaction: %v", r)
	}
	bob.Must(200, "PUT", "/api/v1/messages/"+id+"/reactions/"+thumb, nil) // again changes nothing
	got := obj(ann.Must(200, "GET", "/api/v1/messages/"+id, nil), "message")
	rs := arr(got, "reactions")
	if len(rs) != 1 || rs[0].(map[string]any)["me"] != false || rs[0].(map[string]any)["count"] != float64(1) {
		t.Fatalf("reactions on the message as ann sees it: %v", rs)
	}
	if n := len(arr(ann.Must(200, "GET", "/api/v1/messages/"+id+"/reactions", nil), "reactions")); n != 1 {
		t.Fatalf("listReactions: %d", n)
	}
	bob.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/messages/"+id+"/reactions/abc", nil)
	bob.Must(204, "DELETE", "/api/v1/messages/"+id+"/reactions/"+thumb, nil)
	bob.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/messages/"+id+"/reactions/"+thumb, nil)
	// Someone reading a public channel without having joined cannot react.
	cat := ts.Member("cat")
	cat.Must(200, "GET", "/api/v1/messages/"+id, nil)                                             // case 15
	cat.Refused(403, model.CodeForbidden, "PUT", "/api/v1/messages/"+id+"/reactions/"+thumb, nil) // case 17

	// Threads: a reply names any message of the thread; the summary is on
	// the parent; the replier follows.
	reply := obj(bob.Must(201, "POST", path, map[string]any{"text": "on it", "parent_id": id, "quote": map[string]any{"message_id": id, "start": 3, "end": 13}}), "message")
	if reply["parent_id"] != id || obj(reply, "quote")["message_id"] != id {
		t.Fatalf("reply: %v", reply)
	}
	second := obj(ann.Must(201, "POST", path, map[string]any{"text": "thanks", "parent_id": reply["id"]}), "message")
	if second["parent_id"] != id {
		t.Fatal("a reply to a reply belongs to the same thread")
	}
	bob.Refused(400, model.CodeInvalidValue, "POST", path, map[string]any{"text": "x", "parent_id": id, "quote": map[string]any{"message_id": id, "start": 0, "end": 9999}})
	th := bob.Must(200, "GET", "/api/v1/messages/"+reply["id"].(string)+"/thread", nil)
	summary := obj(th, "parent", "thread")
	if summary["reply_count"] != float64(2) || summary["last_reply_seq"] != second["seq"] || len(arr(summary, "participant_ids")) != 2 {
		t.Fatalf("thread summary: %v", summary)
	}
	if len(arr(th, "messages")) != 2 || obj(th, "thread_state")["state"] != "following" {
		t.Fatalf("thread: %v", th)
	}
	top := bob.Must(200, "GET", path+"?top_level=true&limit=200", nil)
	for _, m := range arr(top, "messages") {
		if m.(map[string]any)["parent_id"] != nil {
			t.Fatal("top_level should leave replies out")
		}
	}
	if len(arr(top, "thread_states")) != 1 {
		t.Fatalf("thread_states beside the page: %v", top["thread_states"])
	}

	// Editing: a quick correction leaves no trace; a later edit keeps the
	// earlier version.
	bob.Refused(403, model.CodeForbidden, "PATCH", "/api/v1/messages/"+id, map[string]any{"text": "mine now"})
	fixed := obj(ann.Must(200, "PATCH", "/api/v1/messages/"+id, map[string]any{"text": "Deploy at 6.\n\n- [ ] tag\n- [ ] reworded\n- [ ] brand new"}), "message")
	if fixed["edited_at"] != nil || fixed["has_history"] != nil {
		t.Fatalf("a quick correction is not marked: %v", fixed)
	}
	// An unchanged item keeps its identifier; a reworded one keeps it and
	// its state if it is in the same position; a new one gets a new one.
	if ts := arr(fixed, "tasks"); len(ts) != 3 || ts[0].(map[string]any)["id"] != "t1" || ts[1].(map[string]any)["id"] != "t2" ||
		ts[1].(map[string]any)["checked"] != true || ts[2].(map[string]any)["id"] != "t3" {
		t.Fatalf("checklist items after an edit: %v", ts)
	}
	ctx := context.Background()
	if err := ts.Server.Store.Settings().Set(ctx, "messages.quick_correction_seconds", 0, ""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	edited := obj(ann.Must(200, "PATCH", "/api/v1/messages/"+id, map[string]any{"text": "Deploy at 7."}), "message")
	if edited["edited_at"] == nil || edited["has_history"] != true {
		t.Fatalf("an edit after the window is marked and keeps history: %v", edited)
	}
	versions := arr(bob.Must(200, "GET", "/api/v1/messages/"+id+"/history", nil), "versions")
	if len(versions) != 1 || versions[0].(map[string]any)["reason"] != "edit" {
		t.Fatalf("history: %v", versions)
	}
	ann.Refused(400, model.CodeInvalidRequest, "PATCH", "/api/v1/messages/"+id, map[string]any{"text": ""})
	// The edit window closes editing for good (case 71).
	if err := ts.Server.Store.Settings().Set(ctx, "messages.edit_window_seconds", 1, ""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	ann.Refused(403, model.CodeForbidden, "PATCH", "/api/v1/messages/"+id, map[string]any{"text": "too late"})

	// Search finds what the asker may read and nothing else.
	find := func(c *testkit.Client, q string) int {
		t.Helper()
		return len(arr(c.Must(200, "GET", "/api/v1/search?q="+url.QueryEscape(q), nil), "results"))
	}
	ann.Post(priv, "the zebra is in the quiet channel")
	if find(ann, "zebra") != 1 || find(bob, "zebra") != 0 || find(ts.Owner, "zebra") != 0 { // cases 3, 24
		t.Fatal("search must not reach into a private channel the asker is not in")
	}
	if find(cat, "deploy") != 1 { // a public channel, not joined (case 15)
		t.Fatal("a member searches public channels they have not joined")
	}
	if find(bob, `"deploy at 7"`) != 1 || find(bob, "depl*") != 1 || find(bob, "deploy -7") != 0 || find(bob, "Deploy at 6") != 0 {
		t.Fatal("search grammar")
	}
	bob.Refused(400, model.CodeInvalidValue, "GET", "/api/v1/search", nil)
	if n := len(arr(bob.Must(200, "GET", "/api/v1/search?q=zebra&in="+priv, nil), "results")); n != 0 {
		t.Fatal("a filter naming an unreadable conversation gives an empty result")
	}

	// Deleting: a manager may delete another's message; the marker stays.
	bob.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/messages/"+id, nil) // case 58
	del := obj(ann.Must(200, "DELETE", "/api/v1/messages/"+second["id"].(string), nil), "message")
	if del["deleted_at"] == nil || del["text"] != nil || del["deleted_by_id"] != string(ann.ID) {
		t.Fatalf("deleted marker: %v", del)
	}
	summary = obj(bob.Must(200, "GET", "/api/v1/messages/"+id, nil), "message", "thread")
	if summary["reply_count"] != float64(1) {
		t.Fatalf("the summary should count live replies only: %v", summary)
	}
	if find(bob, "thanks") != 0 {
		t.Fatal("a deleted message is not found")
	}
	// The author deletes their own reply: with the window at zero it is
	// an ordinary delete, kept as history.
	bob.Must(200, "DELETE", "/api/v1/messages/"+reply["id"].(string), nil)
	hist := arr(ann.Must(200, "GET", "/api/v1/messages/"+reply["id"].(string)+"/history", nil), "versions")
	if len(hist) != 1 || hist[0].(map[string]any)["reason"] != "delete" || hist[0].(map[string]any)["text"] != "on it" {
		t.Fatalf("the last content of a deleted message is its history: %v", hist)
	}
	bob.Refused(409, model.CodeConflict, "DELETE", "/api/v1/messages/"+reply["id"].(string), nil)
	// Inside the window, with no replies, a delete leaves no trace.
	if err := ts.Server.Store.Settings().Set(ctx, "messages.quick_correction_seconds", 30, ""); err != nil {
		t.Fatal(err)
	}
	oops := bob.Post(ch, "oops wrong channel")
	bob.Must(200, "DELETE", "/api/v1/messages/"+oops["id"].(string), nil)
	bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/messages/"+oops["id"].(string), nil)
}

// TestLive covers the WebSocket: hello, resume and ready, fan-out by
// membership, per-reader fields, catch-up from the log, reload, heartbeats
// and the refusals on the way in.
func TestLive(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	pub := ann.Channel("town", false, bob)
	priv := ann.Channel("den", true)

	// The way in: a credential, the subprotocol, and for a cookie the
	// server's own origin.
	if _, resp, err := ts.Browser().Dial(); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("an unauthenticated upgrade should be refused with 401: %v %v", resp, err)
	}

	annS, frames := ann.Events(nil)
	if annS.Hello["account_id"] != string(ann.ID) || annS.Hello["heartbeat_seconds"] != float64(30) {
		t.Fatalf("hello: %v", annS.Hello)
	}
	ready := frames[len(frames)-1]
	if len(arr(ready, "new_conversation_ids")) != 3 { // general, town, den
		t.Fatalf("ready on a first connection lists every conversation: %v", ready)
	}
	bobS, _ := bob.Token().Events(nil) // a bearer token opens one too
	catS, _ := cat.Events(nil)         // cat can read #town and has not joined it
	ownerS, _ := ts.Owner.Events(nil)  // the owner is in neither

	// A message reaches the members and nobody else.
	cid := string(model.NewID[model.MessageID]())
	posted := obj(ann.Must(201, "POST", "/api/v1/conversations/"+pub+"/messages", map[string]any{"text": "hello @bob", "client_id": cid}), "message")
	mine := annS.Expect("message.created")
	if mine["client_id"] != cid || mine["notify"] != "none" || mine["seq"] != posted["seq"] {
		t.Fatalf("the author's own frame: %v", mine)
	}
	theirs := bobS.Expect("message.created")
	if theirs["client_id"] != nil || theirs["notify"] != "notify" || obj(theirs, "message")["id"] != posted["id"] {
		t.Fatalf("a member's frame: %v", theirs)
	}
	if theirs["conversation_id"] != pub || theirs["at"] == nil {
		t.Fatalf("an event carries its conversation and time: %v", theirs)
	}

	// Reactions carry the receiver's own `me`.
	thumb := url.PathEscape("👍")
	bob.Must(200, "PUT", "/api/v1/messages/"+posted["id"].(string)+"/reactions/"+thumb, nil)
	if r := obj(bobS.Expect("reaction.added"), "reaction"); r["me"] != true || r["count"] != float64(1) {
		t.Fatalf("the reactor's frame: %v", r)
	}
	if r := obj(annS.Expect("reaction.added"), "reaction"); r["me"] != false {
		t.Fatalf("another member's frame: %v", r)
	}

	// Member events: cat joins and gets the conversation with its
	// membership as its first event from there.
	cat.Must(200, "POST", "/api/v1/conversations/"+pub+"/join", nil)
	j := catS.Expect("member.joined")
	if obj(j, "conversation", "membership")["level"] != "member" || obj(j, "member")["account_id"] != string(cat.ID) {
		t.Fatalf("the joiner's frame: %v", j)
	}
	if j := annS.Expect("member.joined"); j["conversation"] != nil {
		t.Fatalf("others do not get the conversation: %v", j)
	}
	cat.Must(204, "POST", "/api/v1/conversations/"+pub+"/leave", nil)
	if l := catS.Expect("member.left"); l["account_id"] != string(cat.ID) || l["reason"] != "left" {
		t.Fatalf("the leaver gets member.left as its last event: %v", l)
	}

	// Nothing from a private channel reaches someone who is not in it:
	// not a member, not the owner (cases 6 and 26). And nothing from a
	// public channel reaches someone who only reads it (case 16), or who
	// has left it (case 86's shape).
	ann.Post(priv, "for the den only")
	if e := annS.Expect("message.created"); e["conversation_id"] != priv {
		t.Fatalf("ann should get her own private post: %v", e)
	}
	last := ann.Post(pub, "after cat left")
	// The next message bob sees is the public one: the private one never
	// came his way.
	if e := bobS.Expect("message.created"); e["conversation_id"] != pub || obj(e, "message")["id"] != last["id"] {
		t.Fatalf("bob got an event he should not have: %v", e)
	}
	catS.ExpectNone(300 * time.Millisecond)
	ownerS.ExpectNone(300 * time.Millisecond)

	// Control frames: ping gets pong, an id gets an ack, rubbish gets an
	// error and is otherwise ignored.
	annS.Send(map[string]any{"type": "ping", "id": "7"})
	if p := annS.Expect("pong"); p["reply_to"] != "7" || p["server_time"] == nil {
		t.Fatalf("pong: %v", p)
	}
	annS.Send(map[string]any{"type": "activity", "id": "8"})
	if a := annS.Expect("ack"); a["reply_to"] != "8" {
		t.Fatalf("ack: %v", a)
	}
	annS.Send(map[string]any{"type": "subscribe", "id": "9"})
	if e := annS.Expect("error"); e["reply_to"] != "9" || obj(e, "error")["code"] != "invalid_request" {
		t.Fatalf("error frame: %v", e)
	}
	// While connected and active, ann is online to those who share a
	// conversation with her.
	if p := obj(bob.Must(200, "GET", "/api/v1/accounts/"+string(ann.ID), nil), "account")["presence"]; p != "online" {
		t.Fatalf("ann's presence as bob sees it: %v", p)
	}

	// Signing out closes the session's connections with 4001.
	ann.Must(204, "POST", "/api/v1/logout", nil)
	if code := annS.CloseCode(5 * time.Second); code != live.CloseCredential {
		t.Fatalf("close code after sign-out: %d", code)
	}

	// A cookie connection must come from the server's own origin.
	bob.NoOrigin = true
	if _, resp, err := bob.Dial(); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Fatalf("a cookie upgrade from elsewhere should be refused with 403: %v %v", resp, err)
	}
}

// TestCatchUp covers resume: replay from the log in order, reload frames,
// and the same over HTTP.
func TestCatchUp(t *testing.T) {
	ts := testkit.NewServer(t, testkit.Options{Live: live.Options{ReplayLimit: 20}})
	ann, bob := ts.Member("ann"), ts.Member("bob")
	pub := ann.Channel("town", false, bob)
	priv := ann.Channel("den", true)
	busy := ann.Channel("busy", false, bob)

	first := ann.Post(pub, "one")
	ann.Post(pub, "two @bob")
	m3 := ann.Post(pub, "three")
	bob.Must(200, "PUT", "/api/v1/messages/"+m3["id"].(string)+"/reactions/"+url.PathEscape("🎉"), nil)
	for i := 0; i < 25; i++ {
		ann.Post(busy, "noise")
	}

	// bob was away since `first`. He holds #town at that seq, claims to
	// hold the private channel he is not in, and is far behind in #busy.
	seq := int64(first["seq"].(float64))
	_, frames := bob.Events(map[string]int64{pub: seq, priv: 3, busy: 1})
	var replayed []map[string]any
	reloads := map[string]map[string]any{}
	for _, f := range frames {
		switch {
		case f["type"] == "reload":
			reloads[f["conversation_id"].(string)] = f
		case f["conversation_id"] == pub:
			replayed = append(replayed, f)
		}
	}
	if len(replayed) != 3 {
		t.Fatalf("replay of #town: %v", replayed)
	}
	for i, f := range replayed {
		if f["seq"] != float64(seq+int64(i)+1) {
			t.Fatalf("replayed events must be in seq order with no gap: %v", replayed)
		}
	}
	// What happened while away never pops up: a mention counts as a badge.
	if replayed[0]["notify"] != "badge" || replayed[1]["notify"] != "none" {
		t.Fatalf("notify on catch-up: %v, %v", replayed[0]["notify"], replayed[1]["notify"])
	}
	if r := obj(replayed[2], "reaction"); replayed[2]["type"] != "reaction.added" || r["me"] != true {
		t.Fatalf("the reader's own reaction on catch-up: %v", replayed[2])
	}
	// One answer for a conversation that does not exist and one the
	// account may not know exists.
	if r := reloads[priv]; r == nil || r["reason"] != "unknown" || r["last_seq"] != nil {
		t.Fatalf("reload for a private channel bob is not in: %v", r)
	}
	if r := reloads[busy]; r == nil || r["reason"] != "too_far_behind" || r["last_seq"] == nil {
		t.Fatalf("reload when too far behind: %v", r)
	}
	ready := frames[len(frames)-1]
	if ids := arr(ready, "new_conversation_ids"); len(ids) != 1 { // #general, which bob did not name
		t.Fatalf("new_conversation_ids: %v", ids)
	}

	// The same log over HTTP, also for someone who reads without joining.
	cat := ts.Member("cat")
	out := cat.Must(200, "GET", fmt.Sprintf("/api/v1/conversations/%s/events?after=%d&limit=2", pub, seq), nil)
	if len(arr(out, "events")) != 2 || out["has_more"] != true {
		t.Fatalf("listEvents: %v", out)
	}
	out = cat.Must(200, "GET", fmt.Sprintf("/api/v1/conversations/%s/events?after=%d&until=%d", pub, seq, seq+1), nil)
	if len(arr(out, "events")) != 1 || out["has_more"] != false {
		t.Fatalf("listEvents with until: %v", out)
	}
	cat.Refused(400, model.CodeInvalidValue, "GET", "/api/v1/conversations/"+pub+"/events", nil)
	cat.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv+"/events?after=0", nil)
}

// TestLiveLimits covers two of the things that close a connection: no
// resume and no heartbeat. A client that reads too slowly is tested in the
// live package, where the writer can be held back.
func TestLiveLimits(t *testing.T) {
	ts := testkit.NewServer(t, testkit.Options{Live: live.Options{
		Heartbeat: 100 * time.Millisecond, ResumeWithin: 200 * time.Millisecond,
	}})
	bob := ts.Member("bob")

	silent, _, err := bob.Dial()
	if err != nil {
		t.Fatal(err)
	}
	if code := silent.CloseCode(3 * time.Second); code != live.CloseNoResume {
		t.Fatalf("no resume: closed with %d, want %d", code, live.CloseNoResume)
	}
	idle, _ := bob.Events(nil)
	if code := idle.CloseCode(3 * time.Second); code != live.CloseNoHeartbeat {
		t.Fatalf("no heartbeat: closed with %d, want %d", code, live.CloseNoHeartbeat)
	}
}
