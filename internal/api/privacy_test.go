package api_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// pause lets the clock move on, so that what follows is stamped later than
// what came before.
func pause() { time.Sleep(5 * time.Millisecond) }

// texts returns the text of each message in a list.
func texts(list []any) string {
	var out []string
	for _, raw := range list {
		m := raw.(map[string]any)
		if s, _ := m["text"].(string); s != "" {
			out = append(out, s)
		} else if sys, ok := m["system"].(map[string]any); ok {
			out = append(out, "["+sys["kind"].(string)+"]")
		}
	}
	return strings.Join(out, " | ")
}

// The contract of a private channel (ruling R87): someone invited reads
// only what was posted from their first join on, on every read path, and
// nobody is excused; leaving and coming back keeps the first join; the
// members are told who arrived.
func TestPrivateChannelHistoryFromFirstJoin(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	council := ann.Channel("council", true, cat)

	// Before bob: a message with a thread, a file, a pin, a reaction, a
	// poll and an edit.
	old := ann.Post(council, "the old secret plan")
	oldID := old["id"].(string)
	reply := obj(ann.Must(201, "POST", conv+council+"/messages", map[string]any{"text": "an old reply", "parent_id": oldID}), "message")
	file := ann.Upload(council, "plan.txt", []byte("the plan in full"))
	withFile := obj(ann.Must(201, "POST", conv+council+"/messages", map[string]any{"text": "old attachment", "file_ids": []any{file["id"]}}), "message")
	pinnedFile := ann.Upload(council, "rules.txt", []byte("the house rules"))
	pinned := obj(ann.Must(201, "POST", conv+council+"/messages", map[string]any{"text": "the house rules, pinned", "file_ids": []any{pinnedFile["id"]}}), "message")
	pinnedID := pinned["id"].(string)
	ann.Must(201, "POST", conv+council+"/messages", map[string]any{"text": "an old word on the rules", "parent_id": pinnedID})
	ann.Must(204, "PUT", conv+council+"/pins/"+pinnedID, nil)
	ann.Must(200, "PUT", "/api/v1/messages/"+oldID+"/reactions/"+url.PathEscape("👍"), nil)
	poll := fPost(ann, council, fPoll(map[string]any{"results": "live"}))
	ann.Must(200, "PATCH", "/api/v1/messages/"+oldID, map[string]any{"text": "the old secret plan, revised"})
	pause()

	// bob is added by ann, as an admin of the server would be: no
	// override either way.
	ann.Must(200, "POST", conv+council+"/members", map[string]any{"account_ids": []string{string(bob.ID)}})
	bob.AcceptInvitation(council)
	pause()
	fresh := ann.Post(council, "fresh news")

	t.Run("history", func(t *testing.T) {
		got := texts(arr(bob.Must(200, "GET", conv+council+"/messages", nil), "messages"))
		if strings.Contains(got, "old") || !strings.Contains(got, "fresh news") || !strings.Contains(got, "[member_joined]") {
			t.Fatalf("bob's history: %s", got)
		}
		around := texts(arr(bob.Must(200, "GET", conv+council+"/messages?around="+fmt.Sprint(old["seq"]), nil), "messages"))
		if strings.Contains(around, "old") {
			t.Fatalf("around an old message: %s", around)
		}
		if got := texts(arr(cat.Must(200, "GET", conv+council+"/messages", nil), "messages")); !strings.Contains(got, "old secret") {
			t.Fatalf("cat, there from the start, lost history: %s", got)
		}
	})

	t.Run("one message, its thread, history, reactions and links", func(t *testing.T) {
		for _, id := range []string{oldID, reply["id"].(string), withFile["id"].(string), fID(poll)} {
			bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/messages/"+id, nil)
		}
		bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/messages/"+oldID+"/thread", nil)
		bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/messages/"+reply["id"].(string)+"/thread", nil)
		bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/messages/"+oldID+"/history", nil)
		bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/messages/"+oldID+"/reactions", nil)
		bob.Refused(404, model.CodeNotFound, "PUT", "/api/v1/messages/"+oldID+"/reactions/"+url.PathEscape("👍"), nil)
		bob.Refused(404, model.CodeNotFound, "PUT", "/api/v1/messages/"+oldID+"/bookmark", nil)
		bob.Refused(404, model.CodeNotFound, "PUT", "/api/v1/messages/"+oldID+"/thread/state", map[string]any{"state": "following"})
		bob.Refused(404, model.CodeNotFound, "POST", conv+council+"/messages", map[string]any{"text": "reply into the past", "parent_id": oldID})
		bob.Must(200, "GET", "/api/v1/messages/"+fresh["id"].(string), nil)
	})

	t.Run("a newer reply quoting an old message", func(t *testing.T) {
		q := obj(ann.Must(201, "POST", conv+council+"/messages", map[string]any{"text": "agreed", "parent_id": oldID,
			"quote": map[string]any{"message_id": oldID, "start": 4, "end": 14}}), "message")
		var seen map[string]any
		for _, raw := range arr(bob.Must(200, "GET", conv+council+"/messages", nil), "messages") {
			if m := raw.(map[string]any); m["id"] == q["id"] {
				seen = m
			}
		}
		if seen == nil || obj(seen, "quote")["text"] != "old secret" {
			t.Fatalf("bob's view of the quoting reply: %v", seen)
		}
		// The quoted message itself stays out of reach.
		bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/messages/"+oldID, nil)
	})

	t.Run("files", func(t *testing.T) {
		if s, _, _ := bob.Get("/api/v1/files/"+file["id"].(string)+"/plan.txt", nil); s != 404 {
			t.Fatalf("bob fetched an old file: %d", s)
		}
		if s, _, _ := cat.Get("/api/v1/files/"+file["id"].(string)+"/plan.txt", nil); s != 200 {
			t.Fatalf("cat could not fetch it: %d", s)
		}
	})

	t.Run("pins", func(t *testing.T) {
		pins := arr(bob.Must(200, "GET", conv+council+"/pins", nil), "pins")
		if len(pins) != 1 || pins[0].(map[string]any)["outside_history"] != true || obj(pins[0].(map[string]any), "message")["text"] != "the house rules, pinned" {
			t.Fatalf("bob's pins: %v", pins)
		}
		if p := arr(cat.Must(200, "GET", conv+council+"/pins", nil), "pins"); len(p) != 1 || p[0].(map[string]any)["outside_history"] != nil {
			t.Fatalf("cat's pins: %v", p)
		}
		// The pinned message opens on its own, with its file; not its old
		// replies, and it is not in history or search.
		bob.Must(200, "GET", "/api/v1/messages/"+pinnedID, nil)
		if s, _, _ := bob.Get("/api/v1/files/"+pinnedFile["id"].(string)+"/rules.txt", nil); s != 200 {
			t.Fatalf("bob cannot fetch the pinned message's file: %d", s)
		}
		// Its thread started before his join: it does not open.
		bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/messages/"+pinnedID+"/thread", nil)
		if got := texts(arr(bob.Must(200, "GET", conv+council+"/messages", nil), "messages")); strings.Contains(got, "house rules") {
			t.Fatalf("the pinned message is in bob's history: %s", got)
		}
		if n := len(arr(bob.Must(200, "GET", "/api/v1/search?q=rules", nil), "results")); n != 0 {
			t.Fatalf("bob finds the pinned message: %d", n)
		}
		bob.Refused(404, model.CodeNotFound, "PUT", "/api/v1/messages/"+pinnedID+"/reactions/"+url.PathEscape("👍"), nil)
		// Unpinned, it is gone for him.
		ann.Must(204, "DELETE", conv+council+"/pins/"+pinnedID, nil)
		bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/messages/"+pinnedID, nil)
		if s, _, _ := bob.Get("/api/v1/files/"+pinnedFile["id"].(string)+"/rules.txt", nil); s != 404 {
			t.Fatalf("bob still fetches the unpinned message's file: %d", s)
		}
		if n := len(arr(bob.Must(200, "GET", conv+council+"/pins", nil), "pins")); n != 0 {
			t.Fatalf("bob still sees a pin: %d", n)
		}
	})

	t.Run("forms", func(t *testing.T) {
		bob.Refused(404, model.CodeNotFound, "GET", fForm(fID(poll))+"/results", nil)
		bob.Refused(404, model.CodeNotFound, "POST", fForm(fID(poll))+"/answer", fPick("o1"))
	})

	t.Run("search", func(t *testing.T) {
		for _, q := range []string{"secret", "reply", "attachment"} {
			if n := len(arr(bob.Must(200, "GET", "/api/v1/search?q="+q, nil), "results")); n != 0 {
				t.Fatalf("bob found %q: %d", q, n)
			}
		}
		if n := len(arr(bob.Must(200, "GET", "/api/v1/search?q=fresh", nil), "results")); n != 1 {
			t.Fatalf("bob should find the fresh message: %d", n)
		}
		if n := len(arr(cat.Must(200, "GET", "/api/v1/search?q=secret", nil), "results")); n != 1 {
			t.Fatalf("cat should find the old one: %d", n)
		}
	})

	t.Run("catch-up", func(t *testing.T) {
		raw := arr(bob.Must(200, "GET", conv+council+"/events?after=0", nil), "events")
		b, _ := json.Marshal(raw)
		for _, needle := range []string{"secret plan", "an old reply", "old attachment", "Lunch?"} {
			if strings.Contains(string(b), needle) {
				t.Fatalf("bob's catch-up carries %q", needle)
			}
		}
		if !strings.Contains(string(b), "fresh news") {
			t.Fatal("bob's catch-up lost the fresh message")
		}
		// The live connection's catch-up from the start.
		s, frames := bob.Events(map[string]int64{council: 0})
		defer s.Close()
		b, _ = json.Marshal(frames)
		if strings.Contains(string(b), "secret plan") || strings.Contains(string(b), "Lunch?") {
			t.Fatal("the live catch-up carries old messages")
		}
	})

	t.Run("live", func(t *testing.T) {
		s, _ := bob.Events(map[string]int64{})
		defer s.Close()
		ann.Must(200, "PUT", "/api/v1/messages/"+oldID+"/reactions/"+url.PathEscape("🎉"), nil)
		ann.Must(200, "PATCH", "/api/v1/messages/"+oldID, map[string]any{"text": "the old secret plan, again"})
		ann.Post(council, "ping")
		for {
			f, ok := s.Next(5 * time.Second)
			if !ok {
				t.Fatal("the fresh message never arrived")
			}
			if f["message_id"] == oldID || strings.Contains(fmt.Sprint(f), "secret plan") {
				t.Fatalf("bob was sent an event about an old message: %v", f)
			}
			if m, _ := f["message"].(map[string]any); f["type"] == "message.created" && m["text"] == "ping" {
				break
			}
		}
	})

	t.Run("back within three hours keeps the first join", func(t *testing.T) {
		bob.Must(204, "POST", conv+council+"/leave", nil)
		pause()
		ann.Post(council, "while bob was away")
		pause()
		ann.Must(200, "POST", conv+council+"/members", map[string]any{"account_ids": []string{string(bob.ID)}})
		bob.AcceptInvitation(conv + council)
		got := texts(arr(bob.Must(200, "GET", conv+council+"/messages", nil), "messages"))
		if strings.Contains(got, "old") || !strings.Contains(got, "fresh news") || !strings.Contains(got, "while bob was away") {
			t.Fatalf("after coming back: %s", got)
		}
	})

	t.Run("back after three hours: a newcomer again", func(t *testing.T) {
		bob.Must(204, "POST", conv+council+"/leave", nil)
		ts.Exec(`UPDATE first_joins SET left_at = left_at - ? WHERE account_id = ?`, int64((model.RejoinWindow+time.Minute)/time.Millisecond), string(bob.ID))
		pause()
		ann.Must(200, "POST", conv+council+"/members", map[string]any{"account_ids": []string{string(bob.ID)}})
		bob.AcceptInvitation(conv + council)
		pause()
		ann.Post(council, "after the long absence")
		got := texts(arr(bob.Must(200, "GET", conv+council+"/messages", nil), "messages"))
		if strings.Contains(got, "fresh news") || strings.Contains(got, "while bob was away") || !strings.Contains(got, "after the long absence") {
			t.Fatalf("after a long absence: %s", got)
		}
		// Not even what he wrote himself before.
		if n := len(arr(bob.Must(200, "GET", "/api/v1/search?q=fresh", nil), "results")); n != 0 {
			t.Fatalf("bob still finds the old messages: %d", n)
		}
	})
}

// The floor holds against what other members do (review finding H4): a
// newcomer to a private channel is not handed an older thread by being
// mentioned in it, not carried along when it is moved, not sent the older
// parent by the move's event, not replayed the channel's events from
// before their join, and gets from a pin of an older message the message
// and its files and nothing else.
func TestPrivateChannelFloorHoldsAgainstOthers(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("cat"), ts.Member("dan")
	vault := ann.Channel("vault", true, cat)
	reply := func(c *testkit.Client, parent, text string) map[string]any {
		return obj(c.Must(201, "POST", conv+vault+"/messages", map[string]any{"text": text, "parent_id": parent}), "message")
	}

	// Before bob: two threads, a topic, someone who came and went, and a
	// pinned checklist with a reaction, a tick and a reply.
	plan := ann.Post(vault, "the old vault plan")["id"].(string)
	reply(cat, plan, "an old vault reply")
	other := ann.Post(vault, "the other old thread")["id"].(string)
	reply(cat, other, "an old word on it")
	ann.Must(200, "PATCH", conv+vault, map[string]any{"topic": "the old vault topic"})
	ann.Must(200, "POST", conv+vault+"/members", map[string]any{"account_ids": []string{string(dan.ID)}})
	ann.Must(204, "DELETE", conv+vault+"/members/"+string(dan.ID), nil)
	list := ann.Post(vault, "Old steps:\n\n- [ ] Open the old vault")["id"].(string)
	ann.Must(200, "PUT", "/api/v1/messages/"+list+"/checklist/t1", map[string]any{"checked": true})
	cat.Must(200, "PUT", "/api/v1/messages/"+list+"/reactions/"+url.PathEscape("👍"), nil)
	reply(cat, list, "an old reply to the steps")
	ann.Must(204, "PUT", conv+vault+"/pins/"+list, nil)
	// Events that are about no message are from before a join when they
	// are more than a second older (chat.Hidden).
	time.Sleep(1100 * time.Millisecond)

	ann.Must(200, "POST", conv+vault+"/members", map[string]any{"account_ids": []string{string(bob.ID)}})
	pause()
	// A member mentions bob in the older thread.
	reply(ann, plan, "@bob have a look at this")

	t.Run("the Threads view", func(t *testing.T) {
		for _, all := range []string{"", "?all=true"} {
			b, _ := json.Marshal(bob.Must(200, "GET", "/api/v1/threads"+all, nil))
			if strings.Contains(string(b), "old vault plan") || strings.Contains(string(b), plan) {
				t.Fatalf("bob's Threads view%s carries the older thread: %s", all, b)
			}
		}
		if st := statusOf(bob); st["unread_threads"] != nil && st["unread_threads"] != float64(0) {
			t.Fatalf("bob counts the older thread: %v", st["unread_threads"])
		}
		// A follow made some other way does not open it either.
		ts.Exec(`INSERT INTO thread_states (parent_id, account_id, state, read_seq, updated_at) VALUES (?, ?, 'following', 0, ?)`,
			other, string(bob.ID), int64(model.Now()))
		b, _ := json.Marshal(bob.Must(200, "GET", "/api/v1/threads?all=true", nil))
		if strings.Contains(string(b), "other old thread") {
			t.Fatalf("bob's Threads view carries a thread he follows from before his join: %s", b)
		}
	})

	t.Run("catch-up starts at the join", func(t *testing.T) {
		b, _ := json.Marshal(arr(bob.Must(200, "GET", conv+vault+"/events?after=0", nil), "events"))
		for _, needle := range []string{"old vault topic", string(dan.ID), "old vault plan"} {
			if strings.Contains(string(b), needle) {
				t.Fatalf("bob's catch-up carries %q: %s", needle, b)
			}
		}
		s, frames := bob.Events(map[string]int64{vault: 0})
		s.Close()
		b, _ = json.Marshal(frames)
		if strings.Contains(string(b), "old vault topic") || strings.Contains(string(b), string(dan.ID)) {
			t.Fatalf("the live catch-up carries events from before bob's join: %s", b)
		}
		// cat, there from the start, keeps them.
		b, _ = json.Marshal(arr(cat.Must(200, "GET", conv+vault+"/events?after=0", nil), "events"))
		if !strings.Contains(string(b), "old vault topic") {
			t.Fatal("cat lost the channel's earlier events")
		}
	})

	t.Run("a pin of an older message", func(t *testing.T) {
		m := obj(bob.Must(200, "GET", "/api/v1/messages/"+list, nil), "message")
		if m["reactions"] != nil || m["thread"] != nil {
			t.Fatalf("the pinned message shows bob more than itself: %v", m)
		}
		pins := arr(bob.Must(200, "GET", conv+vault+"/pins", nil), "pins")
		if len(pins) != 1 || obj(pins[0].(map[string]any), "message")["reactions"] != nil || obj(pins[0].(map[string]any), "message")["thread"] != nil {
			t.Fatalf("bob's pins: %v", pins)
		}
		bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/messages/"+list+"/reactions", nil)
		bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/messages/"+list+"/checklist/t1/history", nil)
		bob.Refused(404, model.CodeNotFound, "PUT", "/api/v1/messages/"+list+"/checklist/t1", map[string]any{"checked": false})
		bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/messages/"+list+"/thread", nil)
		bob.Refused(404, model.CodeNotFound, "PUT", "/api/v1/messages/"+list+"/thread/state", map[string]any{"state": "following"})
		bob.Refused(404, model.CodeNotFound, "PUT", "/api/v1/messages/"+list+"/bookmark", nil)
		if m := obj(cat.Must(200, "GET", "/api/v1/messages/"+list, nil), "message"); m["reactions"] == nil || m["thread"] == nil {
			t.Fatalf("cat's view of the message lost its reactions or thread: %v", m)
		}
	})

	t.Run("pinning an older message is audited", func(t *testing.T) {
		if n := len(audited(t, ts, "message.pinned_for_newcomers")); n != 0 {
			t.Fatalf("a pin made before anyone joined later was audited: %d", n)
		}
		ann.Must(204, "PUT", conv+vault+"/pins/"+other, nil)
		if e := wantAudit(t, ts, "message.pinned_for_newcomers"); e.ActorID != string(ann.ID) || e.TargetID != other {
			t.Fatalf("the audit entry: %+v", e)
		}
		// A pin of a message bob can read is not.
		ann.Must(204, "PUT", conv+vault+"/pins/"+ann.Post(vault, "a newer note")["id"].(string), nil)
		if n := len(audited(t, ts, "message.pinned_for_newcomers")); n != 1 {
			t.Fatalf("pins of newer messages are audited too: %d", n)
		}
	})

	t.Run("moving the older thread", func(t *testing.T) {
		// bob follows it (made so above, by any route); the move must not
		// bring him into a channel that holds all of it.
		ts.Exec(`INSERT INTO thread_states (parent_id, account_id, state, read_seq, updated_at) VALUES (?, ?, 'following', 0, ?)
			ON CONFLICT (parent_id, account_id) DO UPDATE SET state = 'following'`, plan, string(bob.ID), int64(model.Now()))
		s, _ := bob.Events(nil)
		defer s.Close()
		moved := convOf(ann.Must(201, "POST", "/api/v1/messages/"+plan+"/move-to-channel", map[string]any{"name": "vault-plan"}))
		bob.Refused(404, model.CodeNotFound, "GET", conv+moved["id"].(string)+"/messages", nil)
		ann.Post(vault, "after the move")
		for {
			f, ok := s.Next(5 * time.Second)
			if !ok {
				t.Fatal("the message after the move never arrived")
			}
			if f["type"] == "thread.moved" || strings.Contains(fmt.Sprint(f), "old vault plan") {
				t.Fatalf("bob was sent the older parent with the move: %v", f)
			}
			if m, _ := f["message"].(map[string]any); f["type"] == "message.created" && m["text"] == "after the move" {
				break
			}
		}
		b, _ := json.Marshal(arr(bob.Must(200, "GET", conv+vault+"/events?after=0", nil), "events"))
		if strings.Contains(string(b), "old vault plan") {
			t.Fatalf("bob's catch-up carries the moved parent: %s", b)
		}
		// cat followed it from the start and comes along, reading all of it.
		got := texts(arr(cat.Must(200, "GET", conv+moved["id"].(string)+"/messages", nil), "messages"))
		if !strings.Contains(got, "an old vault reply") {
			t.Fatalf("cat in the moved channel: %s", got)
		}
	})
}
