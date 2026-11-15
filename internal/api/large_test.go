package api_test

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Large channels, announcement channels and approximate presence (ruling
// R66).

// framesFor reads frames for a while and returns them.
func framesFor(s *testkit.Stream, wait time.Duration) []map[string]any {
	var out []map[string]any
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		f, ok := s.Next(time.Until(deadline))
		if !ok {
			break
		}
		out = append(out, f)
	}
	return out
}

func framesOf(frames []map[string]any, frameType string) []map[string]any {
	var out []map[string]any
	for _, f := range frames {
		if f["type"] == frameType {
			out = append(out, f)
		}
	}
	return out
}

func TestAnnouncementChannel(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	manager, member, allowed, admin := ts.Member("manager"), ts.Member("member"), ts.Member("allowed"), ts.Admin("admin")
	// Made as an announcement channel.
	out := owner.Must(201, "POST", "/api/v1/channels", map[string]any{"name": "news", "private": false, "announcement": true,
		"account_ids": []string{string(manager.ID), string(member.ID), string(allowed.ID), string(admin.ID)}})
	room := out["conversation"].(map[string]any)["id"].(string)
	if c := out["conversation"].(map[string]any); c["announcement"] != true || c["announcement_replies"] != false || c["large"] != true || c["lazy_delivery"] != "server" {
		t.Fatalf("the new channel: %v", c)
	}
	owner.Must(200, "PATCH", convPath(room)+"/members/"+string(manager.ID), map[string]any{"level": "manager"})
	guest := ts.Guest("guest", room)
	bot := owner.Bot("robot")
	owner.Add(room, "member", bot)
	post := convPath(room) + "/messages"

	// Its managers and the admins post; nobody else does.
	first := say(manager, room, "an announcement")["message"].(map[string]any)["id"].(string)
	say(admin, room, "from an admin")
	say(owner, room, "from the owner")
	for name, c := range map[string]*testkit.Client{"member": member, "guest": guest, "bot": bot, "allowed": allowed} {
		c.Refused(403, model.CodeForbidden, "POST", post, map[string]any{"text": "hello from " + name})
		// Replies are off until a manager turns them on.
		c.Refused(403, model.CodeForbidden, "POST", post, map[string]any{"text": "a reply", "parent_id": first})
		// Everyone reads and reacts.
		c.Must(200, "GET", post, nil)
		c.Must(200, "PUT", "/api/v1/messages/"+first+"/reactions/👍", nil)
		// Nobody but the managers is shown who is in it.
		c.Refused(403, model.CodeForbidden, "GET", convPath(room)+"/members", nil)
		if conv := own(c, room); conv["announcement"] != true || conv["member_count"] != float64(7) || conv["member_ids"] != nil {
			t.Fatalf("%s sees %v", name, conv)
		}
	}
	for name, c := range map[string]*testkit.Client{"manager": manager, "admin": admin, "owner": owner} {
		if got := c.Must(200, "GET", convPath(room)+"/members", nil)["members"].([]any); len(got) != 7 {
			t.Fatalf("%s is shown %d members", name, len(got))
		}
	}

	// A manager allows someone; a member cannot allow anyone.
	mayPost := convPath(room) + "/members/" + string(allowed.ID) + "/may-post"
	member.Refused(403, model.CodeForbidden, "PUT", mayPost, map[string]any{"may_post": true})
	manager.Refused(400, model.CodeInvalidValue, "PUT", mayPost, map[string]any{})
	stream, _ := allowed.Events(nil)
	if got := manager.Must(200, "PUT", mayPost, map[string]any{"may_post": true})["member"].(map[string]any); got["may_post"] != true {
		t.Fatalf("the member after being allowed: %v", got)
	}
	if ms := stream.Expect(model.EventMembershipUpdated)["membership"].(map[string]any); ms["may_post"] != true {
		t.Fatalf("the member was told %v", ms)
	}
	say(allowed, room, "I may post")
	if own(allowed, room)["membership"].(map[string]any)["may_post"] != true || own(member, room)["membership"].(map[string]any)["may_post"] != false {
		t.Fatal("may_post is not on the right membership")
	}
	// Still not shown the members, and a bot and a guest can be allowed too.
	allowed.Refused(403, model.CodeForbidden, "GET", convPath(room)+"/members", nil)
	for _, c := range []*testkit.Client{bot, guest} {
		manager.Must(200, "PUT", convPath(room)+"/members/"+string(c.ID)+"/may-post", map[string]any{"may_post": true})
		say(c, room, "allowed")
		manager.Must(200, "PUT", convPath(room)+"/members/"+string(c.ID)+"/may-post", map[string]any{"may_post": false})
		c.Refused(403, model.CodeForbidden, "POST", post, map[string]any{"text": "no longer"})
	}
	manager.Refused(404, model.CodeNotFound, "PUT", convPath(room)+"/members/"+string(ts.Member("outsider").ID)+"/may-post", map[string]any{"may_post": true})

	// Nobody is shown typing.
	watching, _ := member.Events(nil)
	manager.Must(204, "POST", convPath(room)+"/typing", nil)
	if got := framesOf(framesFor(watching, 300*time.Millisecond), model.EventTypingStarted); len(got) != 0 {
		t.Fatalf("typing was shown in an announcement channel: %v", got)
	}

	// Replies, when a manager turns them on: in threads only.
	member.Refused(403, model.CodeForbidden, "PATCH", convPath(room), map[string]any{"announcement_replies": true})
	if c := manager.Must(200, "PATCH", convPath(room), map[string]any{"announcement_replies": true})["conversation"].(map[string]any); c["announcement_replies"] != true {
		t.Fatalf("after turning replies on: %v", c)
	}
	for _, c := range []*testkit.Client{member, guest, bot} {
		c.Must(201, "POST", post, map[string]any{"text": "a reply", "parent_id": first})
		c.Refused(403, model.CodeForbidden, "POST", post, map[string]any{"text": "not a reply"})
	}

	// Made an ordinary channel again: everyone posts, the list is shown,
	// and the replies switch does not wait for the next time.
	member.Refused(403, model.CodeForbidden, "PATCH", convPath(room), map[string]any{"announcement": false})
	if c := manager.Must(200, "PATCH", convPath(room), map[string]any{"announcement": false})["conversation"].(map[string]any); c["announcement"] != false || c["announcement_replies"] != false || c["large"] != false {
		t.Fatalf("after making it ordinary: %v", c)
	}
	say(member, room, "ordinary again")
	member.Must(200, "GET", convPath(room)+"/members", nil)
	// And converted back by an admin who is not a manager of it.
	if c := admin.Must(200, "PATCH", convPath(room), map[string]any{"announcement": true})["conversation"].(map[string]any); c["announcement"] != true {
		t.Fatalf("after converting: %v", c)
	}
	member.Refused(403, model.CodeForbidden, "POST", post, map[string]any{"text": "no"})
	// Not in a chat between people.
	chat := member.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(allowed.ID)}})["conversation"].(map[string]any)["id"].(string)
	member.Refused(403, model.CodeForbidden, "PATCH", convPath(chat), map[string]any{"announcement": true})
}

func TestLargeFeatureLimit(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	a, b := ts.Member("ann"), ts.Member("bob")
	room := owner.Channel("town", false, a, b)
	set := owner.Must(200, "GET", settingsPath, nil)["settings"].(map[string]any)
	if set["channels.large_channel_limit"] != float64(1000) || set["channels.large_feature_limit"] != float64(50) {
		t.Fatalf("the defaults: %v, %v", set["channels.large_channel_limit"], set["channels.large_feature_limit"])
	}
	owner.Refused(400, model.CodeInvalidValue, "PATCH", settingsPath, map[string]any{"channels.large_feature_limit": -1})
	a.Refused(403, model.CodeForbidden, "PATCH", settingsPath, map[string]any{"channels.large_channel_limit": 3})

	// Three members, limit fifty: typing is shown.
	watching, _ := a.Events(nil)
	b.Must(204, "POST", convPath(room)+"/typing", nil)
	if f := watching.Expect(model.EventTypingStarted); f["account_id"] != string(b.ID) {
		t.Fatalf("typing: %v", f)
	}
	if own(a, room)["large"] != false {
		t.Fatal("a small channel says it is large")
	}
	// Three members, limit two: it is not.
	owner.Must(200, "PATCH", settingsPath, map[string]any{"channels.large_feature_limit": 2})
	b.Must(204, "POST", convPath(room)+"/typing", nil)
	if got := framesOf(framesFor(watching, 300*time.Millisecond), model.EventTypingStarted); len(got) != 0 {
		t.Fatalf("typing was shown above the limit: %v", got)
	}
	if own(a, room)["large"] != true {
		t.Fatal("a channel above the limit does not say it is large")
	}
	// A person's own read position still reaches their other devices.
	say(owner, room, "read me")
	other, _ := a.Events(nil)
	seq := own(a, room)["last_seq"]
	a.Must(200, "PUT", convPath(room)+"/read", map[string]any{"seq": seq})
	if f := other.Expect(model.EventReadPositionMoved); f["read_seq"] != seq {
		t.Fatalf("the read position on another device: %v", f)
	}
	// And nobody else is told where anyone has read to.
	bs, _ := b.Events(nil)
	a.Must(200, "PUT", convPath(room)+"/read", map[string]any{"seq": seq})
	if got := framesOf(framesFor(bs, 300*time.Millisecond), model.EventReadPositionMoved); len(got) != 0 {
		t.Fatalf("someone else was told a read position: %v", got)
	}
}

func TestLazyDeliveryOverTheAPI(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	a, b, c := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	room := owner.Channel("big", false, a, b, c)
	a.Refused(403, model.CodeForbidden, "PATCH", convPath(room), map[string]any{"lazy_delivery": "on"})
	owner.Refused(400, model.CodeInvalidValue, "PATCH", convPath(room), map[string]any{"lazy_delivery": "sometimes"})
	if got := owner.Must(200, "PATCH", convPath(room), map[string]any{"lazy_delivery": "on"})["conversation"].(map[string]any); got["lazy_delivery"] != "on" {
		t.Fatalf("after turning lazy delivery on: %v", got)
	}

	resume := func(cl *testkit.Client, open any) *testkit.Stream {
		s, _, err := cl.Dial()
		if err != nil {
			t.Fatal(err)
		}
		if s.Hello["conversations_open_max"] != float64(8) {
			t.Fatalf("hello: %v", s.Hello)
		}
		f := map[string]any{"type": "resume", "positions": map[string]int64{}}
		if open != nil {
			f["open"] = open
		}
		s.Send(f)
		s.Expect("ready")
		return s
	}
	viewer := resume(a, []string{room})
	away := resume(b, []string{})
	old := resume(c, nil)
	// The numbers are kept once a client has asked for them.
	own(b, room)

	say(owner, room, "to everyone looking")
	if f := viewer.Expect(model.EventMessageCreated); f["message"].(map[string]any)["text"] != "to everyone looking" {
		t.Fatalf("the viewer: %v", f)
	}
	old.Expect(model.EventMessageCreated)
	sig := away.Expect("conversation.changed")
	if sig["conversation_id"] != room || sig["messages"] != float64(1) || sig["event"] != nil || sig["last_seq"] == nil {
		t.Fatalf("the signal: %v", sig)
	}
	last := sig["last_seq"].(float64)

	// A mention by name is sent whole, at once, inside a signal.
	say(owner, room, "@bob look at this")
	var named map[string]any
	for named == nil {
		f := away.Expect("conversation.changed")
		if ev, ok := f["event"].(map[string]any); ok {
			named = ev
		}
	}
	if named["type"] != model.EventMessageCreated || named["notify"] != "notify" || named["message"].(map[string]any)["text"] != "@bob look at this" {
		t.Fatalf("the mention: %v", named)
	}

	// Opening fetches the difference: both messages, in order, then the
	// answer; and from then on the stream.
	away.Send(map[string]any{"type": "conversations.open", "id": "o1", "positions": map[string]int64{room: int64(last) - 1}})
	var texts []string
	for {
		f, ok := away.Next(5 * time.Second)
		if !ok {
			t.Fatalf("no answer to conversations.open; got %v", texts)
		}
		if f["type"] == model.EventMessageCreated {
			texts = append(texts, f["message"].(map[string]any)["text"].(string))
		}
		if f["type"] == "ack" && f["reply_to"] == "o1" {
			break
		}
	}
	if len(texts) != 2 || texts[0] != "to everyone looking" || texts[1] != "@bob look at this" {
		t.Fatalf("opening brought %v", texts)
	}
	say(owner, room, "now live")
	if f := away.Expect(model.EventMessageCreated); f["message"].(map[string]any)["text"] != "now live" {
		t.Fatalf("after opening: %v", f)
	}
	// Closed again: signals, with the reader's numbers.
	away.Send(map[string]any{"type": "conversations.open", "id": "o2", "positions": map[string]int64{}})
	away.Expect("ack")
	time.Sleep(1100 * time.Millisecond)
	say(owner, room, "after closing")
	sig = away.Expect("conversation.changed")
	if sig["messages"] != float64(1) || sig["unread_count"] == nil {
		t.Fatalf("the signal after closing: %v", sig)
	}
	// Too many, or not a list of positions.
	many := map[string]int64{}
	for i := 0; i < 9; i++ {
		many[string(model.NewID[model.ConversationID]())] = -1
	}
	away.Send(map[string]any{"type": "conversations.open", "id": "o3", "positions": many})
	if f := away.Expect("error"); f["reply_to"] != "o3" {
		t.Fatalf("nine conversations: %v", f)
	}
}

func TestLastSeenIsCoarse(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	a, b := ts.Member("ann"), ts.Member("bob")
	owner.Channel("town", false, a, b)
	stranger := ts.Guest("guest", owner.Channel("lobby", false))
	see := func(viewer, of *testkit.Client) map[string]any {
		return viewer.Must(200, "GET", "/api/v1/accounts/"+string(of.ID), nil)["account"].(map[string]any)
	}
	// Signed in a moment ago and not connected: recently.
	if got := see(a, b); got["last_seen"] != "recently" || got["last_seen_at"] != nil {
		t.Fatalf("someone who was here a moment ago: %v", got)
	}
	// Connected and active: online.
	b.Events(nil)
	if got := see(a, b); got["last_seen"] != "online" {
		t.Fatalf("someone connected: %v", got["last_seen"])
	}
	// Long ago, by the sessions' own record.
	ts.Exec(`UPDATE sessions SET last_seen_at = ? WHERE account_id = ?`, int64(model.Now())-5*24*60*60*1000, string(a.ID))
	if got := see(b, a); got["last_seen"] != "within_week" {
		t.Fatalf("five days ago: %v", got["last_seen"])
	}
	ts.Exec(`UPDATE sessions SET last_seen_at = ? WHERE account_id = ?`, int64(model.Now())-30*24*60*60*1000, string(a.ID))
	if got := see(b, a); got["last_seen"] != "long_ago" {
		t.Fatalf("a month ago: %v", got["last_seen"])
	}
	// Not in a list, and not for someone who shares nothing with them.
	for _, acc := range b.Must(200, "GET", "/api/v1/accounts", nil)["accounts"].([]any) {
		if acc.(map[string]any)["last_seen"] != nil {
			t.Fatalf("a list carries last_seen: %v", acc)
		}
	}
	if code, out := stranger.Do("GET", "/api/v1/accounts/"+string(b.ID), nil); code == 200 && out["account"].(map[string]any)["last_seen"] != nil {
		t.Fatalf("a guest who shares nothing with them is told: %v", out)
	}
	// Hidden by the person: nobody is told.
	if p := b.Must(200, "PATCH", "/api/v1/me/prefs", map[string]any{"hide_last_seen": true})["prefs"].(map[string]any); p["hide_last_seen"] != true {
		t.Fatalf("prefs: %v", p)
	}
	if got := see(a, b); got["last_seen"] != nil || got["presence"] != "online" {
		t.Fatalf("someone who hides it: last_seen %v, presence %v", got["last_seen"], got["presence"])
	}
	if p := b.Must(200, "GET", "/api/v1/me/prefs", nil)["prefs"].(map[string]any); p["hide_last_seen"] != true {
		t.Fatalf("prefs read back: %v", p)
	}
}
