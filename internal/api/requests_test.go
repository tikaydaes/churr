package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Chat requests, closing, and a block as deaf ears (rulings R77, R78, R79;
// docs/FEATURES.md section 20). The servers here keep new accounts'
// preferences: new direct and sealed chats are requests.

func requestServer(t *testing.T) *testkit.TestServer {
	return testkit.NewServer(t, testkit.Options{ChatRequests: true})
}

// ask starts a direct chat with one person and returns the conversation.
func ask(c *testkit.Client, want int, who *testkit.Client) map[string]any {
	return obj(c.Must(want, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(who.ID)}}), "conversation")
}

func requestsOf(c *testkit.Client) []any {
	return arr(c.Must(200, "GET", "/api/v1/me/chat-requests", nil), "requests")
}

func chatOf(c *testkit.Client, id string) map[string]any {
	return obj(c.Must(200, "GET", "/api/v1/conversations/"+id, nil), "conversation")
}

func autoApprove(c *testkit.Client) {
	c.Must(200, "PATCH", "/api/v1/me/prefs", map[string]any{"auto_approve_direct": true, "auto_approve_sealed": true})
}

// quiet reads a stream until nothing arrives for the wait and fails on any
// frame bad says should not have come. Reading with a deadline ends the
// connection, so it is the last use of the stream.
func quiet(t *testing.T, s *testkit.Stream, wait time.Duration, bad func(f map[string]any) bool) {
	t.Helper()
	for {
		f, ok := s.Next(wait)
		if !ok {
			return
		}
		if bad(f) {
			t.Fatalf("a frame arrived that should not have: %v", f)
		}
	}
}

// mentions reports whether a frame names an account or a conversation
// anywhere in it.
func mentions(f map[string]any, ids ...string) bool {
	b, _ := json.Marshal(f)
	for _, id := range ids {
		if id != "" && strings.Contains(string(b), id) {
			return true
		}
	}
	return false
}

func TestChatRequestAccepted(t *testing.T) {
	ts := requestServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	annS, _ := ann.Events(nil)
	bobS, _ := bob.Events(nil)

	c := ask(ann, 201, bob)
	id := c["id"].(string)
	if c["chat_state"] != "waiting" || c["member_count"] != float64(2) || !hasID(arr(c, "member_ids"), string(bob.ID)) {
		t.Fatalf("the requester's waiting chat: %v", c)
	}
	// No first message, nothing written until it is accepted.
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+id+"/messages", map[string]any{"text": "hi"})
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+id+"/typing", map[string]any{})
	// The person asked is not in it, and sees a request: who, not what.
	bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+id, nil)
	got := bobS.Expect("chat_request.received")
	req := obj(got, "request")
	acc := obj(req, "account")
	if got["notify"] != true || req["kind"] != "direct" || acc["id"] != string(ann.ID) || acc["username"] != "ann" || acc["display_name"] != "ann" {
		t.Fatalf("chat_request.received: %v", got)
	}
	if mentions(got, id) {
		t.Fatalf("the request names the conversation: %v", got)
	}
	list := requestsOf(bob)
	if len(list) != 1 || list[0].(map[string]any)["id"] != req["id"] {
		t.Fatalf("bob's requests: %v", list)
	}
	if len(requestsOf(ann)) != 0 {
		t.Fatal("the requester has no request in their own list")
	}
	// Asking again gives the same waiting chat.
	if again := ask(ann, 200, bob); again["id"] != id {
		t.Fatalf("one request per pair: %v", again)
	}

	open := obj(bob.Must(200, "POST", "/api/v1/me/chat-requests/"+req["id"].(string)+"/accept", nil), "conversation")
	if open["id"] != id || open["chat_state"] != nil || open["member_count"] != float64(2) {
		t.Fatalf("accepted: %v", open)
	}
	if e := bobS.Expect("chat_request.removed"); e["id"] != req["id"] {
		t.Fatalf("chat_request.removed: %v", e)
	}
	upd := annS.ExpectWhere("conversation.updated", func(f map[string]any) bool { return f["conversation_id"] == id })
	if obj(upd, "conversation")["chat_state"] != nil {
		t.Fatalf("the requester is told it opened: %v", upd)
	}
	ann.Post(id, "hello bob")
	if m := bobS.ExpectWhere("message.created", func(f map[string]any) bool { return f["conversation_id"] == id }); obj(m, "message")["text"] != "hello bob" {
		t.Fatalf("bob gets the message: %v", m)
	}
	if len(requestsOf(bob)) != 0 {
		t.Fatal("an accepted request leaves the list")
	}
	bob.Refused(404, model.CodeNotFound, "POST", "/api/v1/me/chat-requests/"+req["id"].(string)+"/accept", nil)
}

func TestChatRequestDeclined(t *testing.T) {
	ts := requestServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	annS, _ := ann.Events(nil)
	id := ask(ann, 201, bob)["id"].(string)
	rid := requestsOf(bob)[0].(map[string]any)["id"].(string)
	// Only the person asked answers.
	ann.Refused(404, model.CodeNotFound, "POST", "/api/v1/me/chat-requests/"+rid+"/decline", nil)
	bob.Must(204, "POST", "/api/v1/me/chat-requests/"+rid+"/decline", nil)
	upd := annS.ExpectWhere("conversation.updated", func(f map[string]any) bool { return f["conversation_id"] == id })
	if obj(upd, "conversation")["chat_state"] != "declined" {
		t.Fatalf("the requester sees it was declined: %v", upd)
	}
	if c := chatOf(ann, id); c["chat_state"] != "declined" {
		t.Fatalf("declined: %v", c)
	}
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+id+"/messages", map[string]any{"text": "please"})
	// Not again for a day, then as before.
	bob2 := ts.Member("bob2")
	e := ann.Refused(429, model.CodeRateLimited, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID)}})
	if !strings.Contains(e["message"].(string), "a day") {
		t.Fatalf("the refusal: %v", e)
	}
	ask(ann, 201, bob2) // someone else is fine
	ts.Server.Chat.AdvanceRequestClock(25 * time.Hour)
	if again := ask(ann, 201, bob); again["id"] == id || again["chat_state"] != "waiting" {
		t.Fatalf("after a day, a new request: %v", again)
	}
	if c := chatOf(ann, id); c["chat_state"] != "declined" {
		t.Fatalf("the declined one stays: %v", c)
	}
}

func TestChatRequestDismissedAndBlocked(t *testing.T) {
	ts := requestServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	id := ask(ann, 201, bob)["id"].(string)
	before := chatOf(ann, id)
	rid := requestsOf(bob)[0].(map[string]any)["id"].(string)
	bob.Must(204, "POST", "/api/v1/me/chat-requests/"+rid+"/dismiss", nil)
	if len(requestsOf(bob)) != 0 {
		t.Fatal("dismissed leaves the list")
	}
	if after := chatOf(ann, id); !reflect.DeepEqual(before, after) {
		t.Fatalf("dismissing changes nothing for the requester:\n%v\n%v", before, after)
	}
	// It waits for ever: asking again gives the same chat, no new request.
	if again := ask(ann, 200, bob); again["id"] != id || again["chat_state"] != "waiting" {
		t.Fatalf("dismissed, asked again: %v", again)
	}
	if len(requestsOf(bob)) != 0 {
		t.Fatal("asking again does not bring a dismissed request back")
	}

	// Block from the request: the same for the requester.
	id2 := ask(cat, 201, bob)["id"].(string)
	before = chatOf(cat, id2)
	rid2 := requestsOf(bob)[0].(map[string]any)["id"].(string)
	bobS, _ := bob.Events(nil)
	bob.Must(204, "POST", "/api/v1/me/chat-requests/"+rid2+"/block", nil)
	if e := bobS.Expect("block.added"); e["account_id"] != string(cat.ID) {
		t.Fatalf("block.added: %v", e)
	}
	if e := bobS.Expect("chat_request.removed"); e["id"] != rid2 {
		t.Fatalf("chat_request.removed: %v", e)
	}
	if len(requestsOf(bob)) != 0 {
		t.Fatal("blocked leaves the list")
	}
	if after := chatOf(cat, id2); !reflect.DeepEqual(before, after) {
		t.Fatalf("blocking changes nothing for the requester:\n%v\n%v", before, after)
	}
	// Unblocked: the waiting request is back, without a second notification.
	bob.Must(204, "DELETE", "/api/v1/me/blocks/"+string(cat.ID), nil)
	back := bobS.Expect("chat_request.received")
	if obj(back, "request")["id"] != rid2 || back["notify"] != false {
		t.Fatalf("back after unblocking: %v", back)
	}
	if list := requestsOf(bob); len(list) != 1 {
		t.Fatalf("bob's list after unblocking: %v", list)
	}
	bob.Must(200, "POST", "/api/v1/me/chat-requests/"+rid2+"/accept", nil)
	cat.Post(id2, "finally")
}

func TestChatRequestWithdrawn(t *testing.T) {
	ts := requestServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	bobS, _ := bob.Events(nil)
	id := ask(ann, 201, bob)["id"].(string)
	rid := obj(bobS.Expect("chat_request.received"), "request")["id"]
	bob.Refused(404, model.CodeNotFound, "POST", "/api/v1/conversations/"+id+"/withdraw", nil)
	ann.Refused(409, model.CodeConflict, "POST", "/api/v1/conversations/"+id+"/close", nil)
	ann.Must(204, "POST", "/api/v1/conversations/"+id+"/withdraw", nil)
	if e := bobS.Expect("chat_request.removed"); e["id"] != rid {
		t.Fatalf("chat_request.removed: %v", e)
	}
	ann.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+id, nil)
	if len(requestsOf(bob)) != 0 {
		t.Fatal("withdrawn leaves the list")
	}
	ann.Refused(429, model.CodeRateLimited, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID)}})
	ts.Server.Chat.AdvanceRequestClock(25 * time.Hour)
	ask(ann, 201, bob)
}

func TestChatRequestAutoApprove(t *testing.T) {
	ts := requestServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	if p := obj(bob.Must(200, "GET", "/api/v1/me/prefs", nil), "prefs"); p["auto_approve_direct"] != false || p["auto_approve_sealed"] != false {
		t.Fatalf("both preferences start off: %v", p)
	}
	autoApprove(bob)
	bobS, _ := bob.Events(nil)
	c := ask(ann, 201, bob)
	if c["chat_state"] != nil {
		t.Fatalf("opens at once: %v", c)
	}
	ann.Post(c["id"].(string), "straight in")
	if m := bobS.ExpectWhere("message.created", func(f map[string]any) bool { return f["conversation_id"] == c["id"] }); obj(m, "message")["text"] != "straight in" {
		t.Fatalf("bob: %v", m)
	}
	if len(requestsOf(bob)) != 0 {
		t.Fatal("no request when chats open at once")
	}
	// Starting a chat with someone whose request waits accepts it.
	cat := ts.Member("cat")
	id := ask(cat, 201, ann)["id"].(string)
	if got := ask(ann, 200, cat); got["id"] != id || got["chat_state"] != nil {
		t.Fatalf("ann answering cat by starting a chat: %v", got)
	}
	cat.Post(id, "hi ann")
}

// people makes n accounts straight in the store, for tests that need
// more people than registration lets in at once. auto turns their
// auto-approve preferences on.
func people(t *testing.T, ts *testkit.TestServer, tag string, n int, auto bool) []model.AccountID {
	t.Helper()
	ctx := context.Background()
	var out []model.AccountID
	err := ts.Server.Store.Tx(ctx, func(tx store.Tx) error {
		for i := 0; i < n; i++ {
			acc := model.Account{Username: fmt.Sprintf("%s%d", tag, i), DisplayName: tag, Role: model.RoleMember}
			if auto {
				acc.PrefsJSON = json.RawMessage(`{"auto_approve_direct":true,"auto_approve_sealed":true}`)
			}
			if _, err := tx.Accounts().Create(ctx, &acc); err != nil {
				return err
			}
			out = append(out, acc.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func askID(c *testkit.Client, want int, who model.AccountID) map[string]any {
	return obj(c.Must(want, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(who)}}), "conversation")
}

func TestChatRequestCaps(t *testing.T) {
	ts := requestServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	asked := people(t, ts, "p", model.RequestsUnapprovedPerDay, false)
	// Some of them have blocked ann, one dismisses: all count the same.
	for _, id := range asked[:2] {
		if err := ts.Server.Store.Blocks().Add(context.Background(), id, ann.ID, model.Now()); err != nil {
			t.Fatal(err)
		}
	}
	ask(ann, 201, bob)
	for _, id := range asked[:model.RequestsUnapprovedPerDay-1] {
		askID(ann, 201, id)
	}
	rid := requestsOf(bob)[0].(map[string]any)["id"].(string)
	bob.Must(204, "POST", "/api/v1/me/chat-requests/"+rid+"/dismiss", nil)
	last := asked[model.RequestsUnapprovedPerDay-1]
	e := ann.Refused(429, model.CodeRateLimited, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(last)}})
	// Someone who opens chats at once is under the other cap.
	auto := people(t, ts, "auto", 1, true)
	askID(ann, 201, auto[0])
	// The answer does not depend on who was asked: the same refusal for
	// someone who blocked ann.
	if e2 := ann.Refused(429, model.CodeRateLimited, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(people(t, ts, "q", 1, false)[0])}}); !reflect.DeepEqual(e, e2) {
		t.Fatalf("refusals differ: %v %v", e, e2)
	}
	ts.Server.Chat.AdvanceRequestClock(25 * time.Hour)
	askID(ann, 201, last)
}

func TestCloseChat(t *testing.T) {
	ts := requestServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	autoApprove(bob)
	annS, _ := ann.Events(nil)
	bobS, _ := bob.Events(nil)
	id := ask(ann, 201, bob)["id"].(string)
	ann.Post(id, "before")
	closed := obj(bob.Must(200, "POST", "/api/v1/conversations/"+id+"/close", nil), "conversation")
	if closed["chat_state"] != "closed" {
		t.Fatalf("closed: %v", closed)
	}
	for _, s := range []*testkit.Stream{annS, bobS} {
		e := s.ExpectWhere("conversation.updated", func(f map[string]any) bool { return f["conversation_id"] == id })
		if obj(e, "conversation")["chat_state"] != "closed" || e["by_id"] != nil {
			t.Fatalf("both are told, not by whom: %v", e)
		}
	}
	// Both read it, neither writes.
	if len(ann.Messages(id)) == 0 || len(bob.Messages(id)) == 0 {
		t.Fatal("the history stays readable")
	}
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+id+"/messages", map[string]any{"text": "x"})
	bob.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+id+"/messages", map[string]any{"text": "x"})
	bob.Must(200, "POST", "/api/v1/conversations/"+id+"/close", nil) // again changes nothing
	// Ask to chat again: a new chat, at once, no wait.
	again := ask(ann, 201, bob)
	if again["id"] == id || again["chat_state"] != nil {
		t.Fatalf("asking again after a close: %v", again)
	}
	// Bob does not approve automatically any more: a request, no wait.
	bob.Must(200, "PATCH", "/api/v1/me/prefs", map[string]any{"auto_approve_direct": false})
	ann.Must(200, "POST", "/api/v1/conversations/"+again["id"].(string)+"/close", nil)
	if third := ask(bob, 201, ann); third["chat_state"] != "waiting" {
		t.Fatalf("a new request after a close: %v", third)
	}
	// Only direct and sealed chats close.
	group := obj(ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID), string(cat.ID)}}), "conversation")
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+group["id"].(string)+"/close", nil)
	ch := ann.Channel("den", true, bob)
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+ch+"/close", nil)
	cat.Refused(404, model.CodeNotFound, "POST", "/api/v1/conversations/"+id+"/close", nil)
}

// normal strips what differs between any two new chats: identifiers and
// times.
func normal(c map[string]any, ids ...string) string {
	b, _ := json.Marshal(c)
	s := string(b)
	for _, id := range ids {
		s = strings.ReplaceAll(s, id, "ID")
	}
	var m map[string]any
	json.Unmarshal([]byte(s), &m)
	delete(m, "id")
	delete(m, "created_at")
	if ms, ok := m["membership"].(map[string]any); ok {
		delete(ms, "joined_at")
	}
	if l, ok := m["member_ids"].([]any); ok {
		strs := make([]string, len(l))
		for i, v := range l {
			strs[i] = v.(string)
		}
		sort.Strings(strs)
		m["member_ids"] = strs
	}
	out, _ := json.Marshal(m)
	return string(out)
}

// TestBlockIsDeafEars: whatever the blocked person does looks exactly as it
// does with anyone else, field by field, and nothing at all reaches the
// person who blocked them; unblocking brings back what waits.
func TestBlockIsDeafEars(t *testing.T) {
	ts := requestServer(t)
	yan, xia, zoe := ts.Member("yan"), ts.Member("xia"), ts.Member("zoe")
	xia.Must(204, "PUT", "/api/v1/me/blocks/"+string(yan.ID), nil)
	xiaS, _ := xia.Events(nil)

	// A request: the same as one to zoe, who has not blocked anyone.
	toX := ask(yan, 201, xia)
	toZ := ask(yan, 201, zoe)
	if a, b := normal(toX, string(xia.ID), string(zoe.ID)), normal(toZ, string(xia.ID), string(zoe.ID)); a != b {
		t.Fatalf("the blocked request differs:\n%s\n%s", a, b)
	}
	if a, b := normal(ask(yan, 200, xia), string(xia.ID), string(zoe.ID)), normal(ask(yan, 200, zoe), string(xia.ID), string(zoe.ID)); a != b {
		t.Fatalf("asking again differs:\n%s\n%s", a, b)
	}
	// The same refusals, word for word.
	ex := yan.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+toX["id"].(string)+"/messages", map[string]any{"text": "hi"})
	ez := yan.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+toZ["id"].(string)+"/messages", map[string]any{"text": "hi"})
	if !reflect.DeepEqual(ex, ez) {
		t.Fatalf("refusals differ: %v %v", ex, ez)
	}
	if len(requestsOf(xia)) != 0 {
		t.Fatal("xia has no request from yan")
	}

	// A sealed chat waits the same way, and a direct chat with someone who
	// approves automatically appears open.
	autoApprove(xia)
	autoApprove(zoe)
	yan.Must(204, "POST", "/api/v1/conversations/"+toX["id"].(string)+"/withdraw", nil)
	yan.Must(204, "POST", "/api/v1/conversations/"+toZ["id"].(string)+"/withdraw", nil)
	ts.Server.Chat.AdvanceRequestClock(25 * time.Hour)
	openX := ask(yan, 201, xia)
	openZ := ask(yan, 201, zoe)
	if a, b := normal(openX, string(xia.ID), string(zoe.ID)), normal(openZ, string(xia.ID), string(zoe.ID)); a != b {
		t.Fatalf("the blocked open chat differs:\n%s\n%s", a, b)
	}
	ghostID := openX["id"].(string)
	// In the chat yan is shown, xia is online as anyone sharing a chat
	// with him is shown, by every path.
	if seen := obj(yan.Must(200, "GET", "/api/v1/accounts/"+string(xia.ID), nil), "account"); seen["presence"] != "online" || seen["last_seen"] != "online" || seen["status"] != nil {
		t.Fatalf("yan's view of xia in a chat that looks open: %v", seen)
	}
	sub, _ := yan.Events(nil)
	sub.Send(map[string]any{"type": "presence.subscribe", "id": "p", "account_ids": []string{string(xia.ID)}})
	if p := sub.ExpectWhere("presence.changed", func(f map[string]any) bool { return f["account_id"] == string(xia.ID) }); p["presence"] != "online" {
		t.Fatalf("yan's subscription to xia: %v", p)
	}
	sub.Close()
	px := yan.Post(ghostID, "anyone there?")
	pz := yan.Post(openZ["id"].(string), "anyone there?")
	for _, m := range []map[string]any{px, pz} {
		delete(m, "id")
		delete(m, "conversation_id")
		delete(m, "created_at")
	}
	if !reflect.DeepEqual(px, pz) {
		t.Fatalf("posting differs: %v %v", px, pz)
	}
	mx, _, _ := yan.Try("GET", "/api/v1/conversations/"+ghostID+"/members", nil)
	if mx != 200 {
		t.Fatalf("members: %d", mx)
	}
	members := arr(yan.Must(200, "GET", "/api/v1/conversations/"+ghostID+"/members", nil), "members")
	if len(members) != 2 {
		t.Fatalf("yan is shown xia as a member: %v", members)
	}
	xia.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+ghostID, nil)

	// Groups: started with xia in it, and xia added to one.
	group := obj(yan.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(xia.ID), string(zoe.ID)}}), "conversation")
	gid := group["id"].(string)
	if len(arr(group, "member_ids")) != 3 {
		t.Fatalf("yan is shown all three: %v", group)
	}
	if z := chatOf(zoe, gid); len(arr(z, "member_ids")) != 2 {
		t.Fatalf("zoe is shown who is really there: %v", z)
	}
	xia.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+gid, nil)
	pair := obj(yan.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(zoe.ID), string(ts.Owner.ID)}}), "conversation")
	added := arr(yan.Must(200, "POST", "/api/v1/conversations/"+pair["id"].(string)+"/members", map[string]any{"account_ids": []string{string(xia.ID)}}), "members")
	if len(added) != 1 || added[0].(map[string]any)["account_id"] != string(xia.ID) {
		t.Fatalf("adding xia looks done: %v", added)
	}
	if c := chatOf(yan, pair["id"].(string)); len(arr(c, "member_ids")) != 4 {
		t.Fatalf("yan is shown xia added: %v", c)
	}
	xia.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+pair["id"].(string), nil)

	// Nothing of any of it reached xia.
	if len(requestsOf(xia)) != 0 {
		t.Fatal("xia has no request")
	}
	list := arr(xia.Must(200, "GET", "/api/v1/conversations", nil), "conversations")
	for _, c := range list {
		if id := c.(map[string]any)["id"]; id == ghostID || id == gid || id == pair["id"] {
			t.Fatalf("xia lists yan's chat: %v", c)
		}
	}
	quiet(t, xiaS, 500*time.Millisecond, func(f map[string]any) bool {
		// Her own account lists whom she blocked; nothing else may name
		// yan or his chats.
		// yan's own presence may reach her: both are in #general, and
		// her block hides her from him, not him from her.
		switch f["type"] {
		case "me.updated", "presence.changed", "presence.batch":
			return false
		}
		return mentions(f, ghostID, gid, pair["id"].(string), string(yan.ID))
	})

	// Unblocked: the open-looking chat is a request, and its messages
	// appear only once xia accepts.
	xia.Must(204, "DELETE", "/api/v1/me/blocks/"+string(yan.ID), nil)
	list = requestsOf(xia)
	if len(list) != 1 || obj(list[0].(map[string]any), "account")["id"] != string(yan.ID) {
		t.Fatalf("xia's requests after unblocking: %v", list)
	}
	xia.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+ghostID+"/messages", nil)
	xia.Must(200, "POST", "/api/v1/me/chat-requests/"+list[0].(map[string]any)["id"].(string)+"/accept", nil)
	if xia.HasMessage(ghostID, "anyone there?") == nil {
		t.Fatal("after accepting, xia reads what yan wrote")
	}
}

// TestBlockedSealedChatLooksNormal: a sealed chat with someone who has
// blocked the asker waits like any other, and one with someone who approves
// automatically takes messages, epochs included, that never reach them.
func TestBlockedSealedChatLooksNormal(t *testing.T) {
	ts := requestServer(t)
	yan, xia, zoe := ts.Member("yan"), ts.Member("xia"), ts.Member("zoe")
	for _, c := range []*testkit.Client{xia, zoe} {
		d := c.Sealed("web")
		d.CreateBackup()
	}
	yd := yan.Sealed("web")
	yd.CreateBackup()
	xia.Must(204, "PUT", "/api/v1/me/blocks/"+string(yan.ID), nil)
	cx, cz := yd.Open(xia.ID), yd.Open(zoe.ID)
	gx, gz := chatOf(yan, cx.ID), chatOf(yan, cz.ID)
	if a, b := normal(gx, string(xia.ID), string(zoe.ID)), normal(gz, string(xia.ID), string(zoe.ID)); a != b || gx["chat_state"] != "waiting" {
		t.Fatalf("the blocked sealed request differs:\n%s\n%s", a, b)
	}
	if len(requestsOf(xia)) != 0 || len(requestsOf(zoe)) != 1 {
		t.Fatal("only zoe is asked")
	}
	autoApprove(xia)
	yan.Must(204, "POST", "/api/v1/conversations/"+cx.ID+"/withdraw", nil)
	ts.Server.Chat.AdvanceRequestClock(25 * time.Hour)
	open := yd.Open(xia.ID)
	if c := chatOf(yan, open.ID); c["chat_state"] != nil || c["key_epoch"] != float64(1) {
		t.Fatalf("appears open: %v", c)
	}
	open.Say("sealed hello")
	if !open.MakeEpoch() {
		t.Fatal("a new epoch is taken as in any chat")
	}
	open.Say("after the epoch")
	xia.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+open.ID, nil)
}

// TestBlockedPresence: to someone they have blocked, a person is online with
// no status, availability, do not disturb, call or last seen, by every path
// presence reaches a viewer; anyone else sees the truth.
func TestBlockedPresence(t *testing.T) {
	ts := requestServer(t)
	xia, yan, zoe := ts.Member("xia"), ts.Member("yan"), ts.Member("zoe")
	ch := xia.Channel("room", true, yan, zoe)
	_ = ch
	xia.Must(200, "PUT", "/api/v1/me/status", map[string]any{"emoji": "🌴", "text": "away for a bit"})
	xia.Must(200, "PUT", "/api/v1/me/dnd", map[string]any{"until": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	yanS, _ := yan.Events(nil)
	zoeS, _ := zoe.Events(nil)

	xia.Must(204, "PUT", "/api/v1/me/blocks/"+string(yan.ID), nil)
	// At the block, yan is told xia is online with no status and no do
	// not disturb: as if xia had come back and cleared them.
	if e := yanS.ExpectWhere("status.changed", func(f map[string]any) bool { return f["account_id"] == string(xia.ID) }); e["status"] != nil {
		t.Fatalf("status at the block: %v", e)
	}
	if e := yanS.ExpectWhere("dnd.changed", func(f map[string]any) bool { return f["account_id"] == string(xia.ID) }); e["dnd"] != false {
		t.Fatalf("dnd at the block: %v", e)
	}

	// The account fetched by itself.
	seen := obj(yan.Must(200, "GET", "/api/v1/accounts/"+string(xia.ID), nil), "account")
	truth := obj(zoe.Must(200, "GET", "/api/v1/accounts/"+string(xia.ID), nil), "account")
	if seen["presence"] != "online" || seen["status"] != nil || seen["dnd"] == true || seen["availability"] != nil || seen["in_call"] == true {
		t.Fatalf("yan's view of xia: %v", seen)
	}
	if ls, ok := seen["last_seen"]; ok && ls != "online" {
		t.Fatalf("last seen: %v", seen)
	}
	if truth["status"] == nil || truth["dnd"] != true {
		t.Fatalf("zoe's view of xia: %v", truth)
	}
	// The list of accounts.
	for _, a := range arr(yan.Must(200, "GET", "/api/v1/accounts?q=xia", nil), "accounts") {
		if a := a.(map[string]any); a["id"] == string(xia.ID) && (a["presence"] != "online" || a["status"] != nil || a["dnd"] == true) {
			t.Fatalf("xia in yan's list: %v", a)
		}
	}
	// A subscription's first answer: online, even after xia leaves.
	sub, _ := yan.Events(nil)
	sub.Send(map[string]any{"type": "presence.subscribe", "id": "s", "account_ids": []string{string(xia.ID)}})
	if p := sub.Expect("presence.changed"); p["account_id"] != string(xia.ID) || p["presence"] != "online" || p["in_call"] != false {
		t.Fatalf("the subscription's answer: %v", p)
	}

	// Changes: zoe hears each; yan hears none.
	xia.Must(200, "PUT", "/api/v1/me/status", map[string]any{"emoji": "☕", "text": "coffee"})
	xia.Must(200, "PATCH", "/api/v1/me", map[string]any{"display_name": "Xia X"})
	zoeS.ExpectWhere("status.changed", func(f map[string]any) bool { return f["account_id"] == string(xia.ID) })
	if e := zoeS.ExpectWhere("account.updated", func(f map[string]any) bool { return obj(f, "account")["id"] == string(xia.ID) }); obj(e, "account")["status"] == nil {
		t.Fatalf("zoe's account.updated: %v", e)
	}
	if e := yanS.ExpectWhere("account.updated", func(f map[string]any) bool { return obj(f, "account")["id"] == string(xia.ID) }); obj(e, "account")["status"] != nil ||
		obj(e, "account")["presence"] != "online" || obj(e, "account")["display_name"] != "Xia X" {
		t.Fatalf("yan's account.updated shows the new name and nothing more: %v", e)
	}
	ts.Server.People.CallChanged(xia.ID)
	quiet(t, yanS, 700*time.Millisecond, func(f map[string]any) bool {
		switch f["type"] {
		case "presence.changed", "status.changed", "availability.changed", "dnd.changed", "typing.started":
			return f["account_id"] == string(xia.ID)
		case "presence.batch":
			return mentions(f, string(xia.ID))
		}
		return false
	})

	// Unblocked: yan sees the truth again.
	xia.Must(204, "DELETE", "/api/v1/me/blocks/"+string(yan.ID), nil)
	if seen := obj(yan.Must(200, "GET", "/api/v1/accounts/"+string(xia.ID), nil), "account"); seen["status"] == nil || seen["dnd"] != true {
		t.Fatalf("after unblocking: %v", seen)
	}
}

// TestBlockedTiming: the blocked path takes no measurable shortcut. The
// bound is loose so a busy machine does not fail it; a path that skipped
// the work would be several times faster.
func TestBlockedTiming(t *testing.T) {
	ts := requestServer(t)
	yan := ts.Member("yan")
	const n = 8
	xs, zs := people(t, ts, "x", n, false), people(t, ts, "z", n, false)
	for _, x := range xs {
		if err := ts.Server.Store.Blocks().Add(context.Background(), x, yan.ID, model.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var blocked, plain []time.Duration
	for i := 0; i < n; i++ {
		start := time.Now()
		askID(yan, 201, xs[i])
		blocked = append(blocked, time.Since(start))
		start = time.Now()
		askID(yan, 201, zs[i])
		plain = append(plain, time.Since(start))
		if i == 3 {
			ts.Server.Chat.AdvanceRequestClock(25 * time.Hour)
		}
	}
	med := func(d []time.Duration) time.Duration {
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		return d[len(d)/2]
	}
	if b, p := med(blocked), med(plain); b*4 < p {
		t.Fatalf("the blocked path is measurably faster: %v against %v", b, p)
	}
}

// TestGhostPeerPresence: two people who share nothing. In the chat a
// blocked person is shown open, the one who blocked them is online to
// them by every path, as in a chat they really shared; a watch answers at
// once that they are around.
func TestGhostPeerPresence(t *testing.T) {
	ts := requestServer(t)
	yan, xia := ts.Member("yan"), ts.Member("xia")
	// Everyone is in the starting channels through a group: taken out, the
	// two share nothing.
	for _, conv := range arr(ts.Owner.Must(200, "GET", "/api/v1/conversations", nil), "conversations") {
		id := conv.(map[string]any)["id"].(string)
		for _, g := range arr(ts.Owner.Must(200, "GET", "/api/v1/conversations/"+id+"/groups", nil), "groups") {
			ts.Owner.Must(204, "DELETE", "/api/v1/conversations/"+id+"/groups/"+g.(map[string]any)["group_id"].(string), nil)
		}
	}
	xia.Must(204, "PUT", "/api/v1/me/blocks/"+string(yan.ID), nil)
	autoApprove(xia)
	before := obj(yan.Must(200, "GET", "/api/v1/accounts/"+string(xia.ID), nil), "account")
	if before["presence"] != "offline" || before["last_seen"] != nil {
		t.Fatalf("sharing nothing, yan is shown nothing of xia: %v", before)
	}
	ask(yan, 201, xia)
	seen := obj(yan.Must(200, "GET", "/api/v1/accounts/"+string(xia.ID), nil), "account")
	if seen["presence"] != "online" || seen["last_seen"] != "online" {
		t.Fatalf("in the chat that looks open: %v", seen)
	}
	for _, a := range arr(yan.Must(200, "GET", "/api/v1/accounts?q=xia", nil), "accounts") {
		if a := a.(map[string]any); a["id"] == string(xia.ID) && a["presence"] != "online" {
			t.Fatalf("xia in yan's list: %v", a)
		}
	}
	sub, _ := yan.Events(nil)
	sub.Send(map[string]any{"type": "presence.subscribe", "id": "p", "account_ids": []string{string(xia.ID)}})
	if p := sub.ExpectWhere("presence.changed", func(f map[string]any) bool { return f["account_id"] == string(xia.ID) }); p["presence"] != "online" {
		t.Fatalf("yan's subscription to xia: %v", p)
	}
	if out := yan.Must(200, "PUT", "/api/v1/me/watches/"+string(xia.ID), nil); out["around"] != true {
		t.Fatalf("a watch for xia answers that she is around: %v", out)
	}
}
