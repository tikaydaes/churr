package calls_test

import (
	"context"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/calls"
	"github.com/tikaydaes/churr/internal/calls/calltest"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

const convPath = "/api/v1/conversations/"

func obj(m map[string]any, key string) map[string]any {
	o, _ := m[key].(map[string]any)
	return o
}

// notice reads the call's one system message.
func notice(t *testing.T, c *testkit.Client, conv, id string) map[string]any {
	t.Helper()
	return obj(obj(c.Must(200, "GET", "/api/v1/messages/"+id, nil), "message"), "system")
}

// The life of a call: started by the first join, one notice message, the
// events every member receives, and its end when the last person leaves.
func TestLifeOfACall(t *testing.T) {
	ts := server(t, fast())
	ann, bob, cy := ts.Member("ann"), ts.Member("bob"), ts.Member("cy")
	ch := ann.Channel("standup", false, bob, cy)
	// Cy is a member who never joins the call: the header events reach
	// her all the same.
	cyStream, _ := cy.Events(nil)
	if out := ann.Must(200, "GET", convPath+ch+"/call", nil); out["call"] != nil {
		t.Fatalf("a call before anyone started one: %v", out)
	}

	annStream, _ := ann.Events(nil)
	a := calltest.New(t, annStream, calltest.Options{})
	t.Cleanup(a.Close)
	out := ann.Must(200, "POST", convPath+ch+"/call/join", nil)
	call := obj(out, "call")
	id := call["id"].(string)
	if tk, _ := out["ticket"].(string); len(tk) != 43 {
		t.Fatalf("the ticket: %q", out["ticket"])
	}
	if call["conversation_id"] != ch || call["started_by_id"] != string(ann.ID) || call["sealed"] != false || call["started_at"] == nil ||
		call["message_id"] == nil || call["hosted_by"] != nil || len(call["participants"].([]any)) != 0 || call["ended_at"] != nil {
		t.Fatalf("the call object: %v", call)
	}
	// Every member hears that a call is on, and the notice is posted.
	started := cyStream.Expect("call.started")
	if obj(started, "call")["id"] != id || started["conversation_id"] != ch || started["seq"] != nil || started["at"] == nil {
		t.Fatalf("call.started: %v", started)
	}
	msgID := call["message_id"].(string)
	if sys := notice(t, cy, ch, msgID); sys["kind"] != "call_started" || sys["call_id"] != id {
		t.Fatalf("the notice while the call is live: %v", sys)
	}
	if m := obj(cy.Must(200, "GET", "/api/v1/messages/"+msgID, nil), "message"); m["author_id"] != string(ann.ID) {
		t.Fatalf("the notice's author: %v", m)
	}

	// Joining takes the ticket, on the live connection.
	ok := a.MustJoin(id, out["ticket"].(string))
	limits := obj(ok, "limits")
	if limits["max_participants"] != 50.0 || limits["max_cameras"] != 12.0 || limits["max_subscriptions"] != 16.0 || limits["audio_kbps"] != 32.0 ||
		limits["screen_kbps"] != 2500.0 || obj(limits, "layers")["h"] != 500.0 || len(ok["resume_token"].(string)) != 43 {
		t.Fatalf("the answer to call.join: %v", ok)
	}
	joined := cyStream.Expect("call.participant_joined")
	if joined["call_id"] != id || obj(joined, "participant")["account_id"] != string(ann.ID) || obj(joined, "participant")["joined_at"] == nil {
		t.Fatalf("call.participant_joined: %v", joined)
	}
	// A second start joins the first call.
	bobStream, _ := bob.Events(nil)
	b := calltest.New(t, bobStream, calltest.Options{})
	t.Cleanup(b.Close)
	bid, btk := ticket(t, bob, ch)
	if bid != id {
		t.Fatal("a second person asking to join started a second call")
	}
	okB := b.MustJoin(bid, btk)
	a.Publish("microphone")
	// A participant's copy of the call has everyone's tracks.
	if parts := obj(okB, "call")["participants"].([]any); len(parts) != 2 || parts[0].(map[string]any)["account_id"] != string(ann.ID) {
		t.Fatalf("the call as a participant gets it: %v", okB["call"])
	}
	// Someone who looks later finds the call on the conversation, without
	// tracks, and with the same operation by itself.
	conv := obj(cy.Must(200, "GET", convPath+ch, nil), "conversation")
	live := obj(conv, "call")
	if live["id"] != id || len(live["participants"].([]any)) != 2 || live["participants"].([]any)[0].(map[string]any)["tracks"] != nil {
		t.Fatalf("the conversation's call: %v", conv["call"])
	}
	if got := obj(cy.Must(200, "GET", convPath+ch+"/call", nil), "call"); got["id"] != id {
		t.Fatalf("getCall: %v", got)
	}
	// Both are marked as in a call.
	eventually(t, "in_call on ann", func() bool {
		return obj(cy.Must(200, "GET", "/api/v1/accounts/"+string(ann.ID), nil), "account")["in_call"] == true
	})

	// Bob leaves with the frame; Ann with the operation.
	if f := b.Leave(); f["type"] != "call.ok" {
		t.Fatalf("call.leave: %v", f)
	}
	left := cyStream.ExpectWhere("call.participant_left", is("account_id", string(bob.ID)))
	if left["reason"] != "left" || left["call_id"] != id {
		t.Fatalf("call.participant_left: %v", left)
	}
	b.Refused("not_in_call", map[string]any{"type": "call.subscribe", "video": []any{}})
	bob.Refused(404, model.CodeNotFound, "POST", convPath+ch+"/call/leave", nil)
	time.Sleep(1100 * time.Millisecond) // so the call has a length to speak of
	ann.Must(204, "POST", convPath+ch+"/call/leave", nil)

	// The last one out ends the call.
	ended := cyStream.Expect("call.ended")
	if ended["call_id"] != id || ended["reason"] != "empty" || ended["duration_seconds"].(float64) < 1 || ended["conversation_id"] != ch {
		t.Fatalf("call.ended: %v", ended)
	}
	sys := notice(t, cy, ch, msgID)
	if sys["kind"] != "call_ended" || sys["call_id"] != id || sys["duration_seconds"].(float64) < 1 || len(sys["participant_ids"].([]any)) != 2 ||
		sys["participant_ids"].([]any)[0] != string(ann.ID) {
		t.Fatalf("the notice after the call: %v", sys)
	}
	// The same message, changed: not edited, no history.
	m := obj(cy.Must(200, "GET", "/api/v1/messages/"+msgID, nil), "message")
	if m["edited_at"] != nil || m["has_history"] == true {
		t.Fatalf("the notice was marked as edited: %v", m)
	}
	if out := ann.Must(200, "GET", convPath+ch+"/call", nil); out["call"] != nil {
		t.Fatalf("a call after it ended: %v", out)
	}
	if conv := obj(cy.Must(200, "GET", convPath+ch, nil), "conversation"); conv["call"] != nil {
		t.Fatalf("the conversation still shows a call: %v", conv["call"])
	}
	// What is stored: the call with its end, and who was in it when.
	stored, err := ts.Server.Store.Calls().Get(context.Background(), model.CallID(id))
	if err != nil || stored.Live() || stored.MessageID != model.MessageID(msgID) || stored.StartedBy != ann.ID {
		t.Fatalf("the stored call: %+v %v", stored, err)
	}
	eventually(t, "in_call cleared", func() bool {
		return obj(cy.Must(200, "GET", "/api/v1/accounts/"+string(ann.ID), nil), "account")["in_call"] != true
	})
	// Anyone can start a new one, with a new notice.
	nid, _ := ticket(t, bob, ch)
	if nid == id {
		t.Fatal("a new call has the old call's identifier")
	}
}

// Who may join: members at level member or above, not timed out
// (docs/CALLS.md section 3, docs/PERMISSIONS.md section 7).
func TestWhoMayJoin(t *testing.T) {
	ts := server(t, fast())
	ann, bob, cy, dee := ts.Member("ann"), ts.Member("bob"), ts.Member("cy"), ts.Member("dee")
	priv := ann.Channel("private", true, bob, cy)
	pub := ann.Channel("public", false, bob)
	join := func(conv string) string { return convPath + conv + "/call/join" }

	// A non-member: of a private channel, which does not exist for them;
	// of a public one, which they must join first.
	dee.Refused(404, model.CodeNotFound, "POST", join(priv), nil)
	dee.Refused(404, model.CodeNotFound, "GET", convPath+priv+"/call", nil)
	dee.Refused(403, model.CodeForbidden, "POST", join(pub), nil)
	// A reader sees that a call is on and cannot join it.
	ann.Must(200, "PATCH", convPath+priv+"/members/"+string(cy.ID), map[string]any{"level": "reader"})
	a := enter(t, ann, priv, calltest.Options{})
	cy.Refused(403, model.CodeForbidden, "POST", join(priv), nil)
	if got := obj(cy.Must(200, "GET", convPath+priv+"/call", nil), "call"); got["id"] != a.CallID || len(got["participants"].([]any)) != 1 {
		t.Fatalf("a reader's view of the call: %v", got)
	}
	// A timed-out member is refused.
	ann.Must(201, "POST", convPath+priv+"/timeouts", map[string]any{"account_id": bob.ID, "seconds": 600, "reason": "cool off"})
	bob.Refused(403, model.CodeTimedOut, "POST", join(priv), nil)
	// No call was started by any of the refusals.
	if out := ann.Must(200, "GET", convPath+pub+"/call", nil); out["call"] != nil {
		t.Fatalf("a refused join started a call: %v", out)
	}

	// The ticket: one use, this account, this session, this call.
	id, tk := ticket(t, bob, pub)
	stream, _ := bob.Events(nil)
	b := calltest.New(t, stream, calltest.Options{})
	t.Cleanup(b.Close)
	b.Refused("not_in_call", map[string]any{"type": "call.mute", "source": "microphone", "muted": true})
	b.Refused("not_in_call", map[string]any{"type": "call.offer", "pc": "send", "sdp": "v=0", "ice_restart": false, "tracks": []any{}})
	// Ann cannot use Bob's ticket.
	annPub, _ := ann.Events(nil)
	a2 := calltest.New(t, annPub, calltest.Options{})
	t.Cleanup(a2.Close)
	if f := a2.Join(id, tk); f["code"] != "ticket_invalid" {
		t.Fatalf("another account's ticket: %v", f)
	}
	// Trying it used it up, as any use does.
	if f := b.Join(id, tk); f["code"] != "ticket_invalid" {
		t.Fatalf("a ticket someone else tried: %v", f)
	}
	// Another session of the same account cannot use it either.
	_, tk = ticket(t, bob, pub)
	other, _ := bob.Token().Events(nil)
	b2 := calltest.New(t, other, calltest.Options{})
	t.Cleanup(b2.Close)
	if f := b2.Join(id, tk); f["code"] != "ticket_invalid" {
		t.Fatalf("another session's ticket: %v", f)
	}
	// A ticket for one call does not open another.
	_, tk = ticket(t, bob, pub)
	if f := b.Join(a.CallID, tk); f["code"] != "ticket_invalid" {
		t.Fatalf("a ticket for another call: %v", f)
	}
	// A good one works, once.
	_, tk = ticket(t, bob, pub)
	b.MustJoin(id, tk)
	if f := b.Join(id, tk); f["code"] != "ticket_invalid" {
		t.Fatalf("a ticket used twice: %v", f)
	}
	b.Refused("invalid_request", map[string]any{"type": "call.join", "call_id": id, "ticket": "x", "surprise": true})
	b.Refused("invalid_request", map[string]any{"type": "call.join", "call_id": "not-an-id", "ticket": "x"})
	b.Refused("ticket_invalid", map[string]any{"type": "call.join", "call_id": "0193b100-2c1e-7b3a-9f0e-2a4d5c6b7e8f", "ticket": "x"})

	// Permission is checked again when the ticket is used: Bob is timed
	// out in the public channel between asking and joining.
	b.Leave()
	_, tk = ticket(t, bob, pub)
	ann.Must(201, "POST", convPath+pub+"/timeouts", map[string]any{"account_id": bob.ID, "seconds": 600, "reason": "cool off"})
	if f := b.Join(id, tk); f["code"] != "forbidden" && f["code"] != "ticket_invalid" {
		// The call ended when Bob left it empty, so either answer is right.
		t.Fatalf("joining after being timed out: %v", f)
	}
}

// A ticket is good for sixty seconds.
func TestTicketRunsOut(t *testing.T) {
	tm := fast()
	tm.Ticket = 300 * time.Millisecond
	ts := server(t, tm)
	ann := ts.Member("ann")
	ch := ann.Channel("standup", false)
	stream, _ := ann.Events(nil)
	a := calltest.New(t, stream, calltest.Options{})
	t.Cleanup(a.Close)
	id, tk := ticket(t, ann, ch)
	time.Sleep(400 * time.Millisecond)
	if f := a.Join(id, tk); f["code"] != "ticket_invalid" {
		t.Fatalf("a ticket past its time: %v", f)
	}
	_, tk = ticket(t, ann, ch)
	a.MustJoin(id, tk)
}

// A participant leaves when they are removed from the conversation, lose
// level member, are timed out there, or their session ends.
func TestParticipantsAreRemoved(t *testing.T) {
	ts := server(t, fast())
	ann, bob, cy, dee := ts.Member("ann"), ts.Member("bob"), ts.Member("cy"), ts.Member("dee")
	ch := ann.Channel("standup", true, bob, cy, dee)
	watch, _ := ann.Events(nil)
	a := enter(t, ann, ch, calltest.Options{})
	b := enter(t, bob, ch, calltest.Options{})
	c := enter(t, cy, ch, calltest.Options{})
	d := enter(t, dee, ch, calltest.Options{})
	a.Publish("microphone")
	left := func(who *testkit.Client, reason string) {
		t.Helper()
		f := watch.ExpectWhere("call.participant_left", is("account_id", string(who.ID)))
		if f["reason"] != reason {
			t.Fatalf("call.participant_left: %v, want reason %s", f, reason)
		}
	}
	// Timed out in the conversation.
	ann.Must(201, "POST", convPath+ch+"/timeouts", map[string]any{"account_id": bob.ID, "seconds": 600, "reason": "cool off"})
	left(bob, "removed")
	b.Refused("not_in_call", map[string]any{"type": "call.subscribe", "video": []any{}})
	// Lowered to reader.
	ann.Must(200, "PATCH", convPath+ch+"/members/"+string(cy.ID), map[string]any{"level": "reader"})
	left(cy, "removed")
	// The one removed hears of it on their own connection too.
	c.Expect("call.participant_left", is("account_id", string(cy.ID)))
	// Their session ends.
	ts.Owner.Must(204, "DELETE", "/api/v1/admin/accounts/"+string(dee.ID)+"/sessions", nil)
	left(dee, "signed_out")
	d.Close()
	// Removed from the conversation.
	eve := ts.Member("eve")
	ann.Must(200, "POST", convPath+ch+"/members", map[string]any{"account_ids": []string{string(eve.ID)}})
	e := enter(t, eve, ch, calltest.Options{})
	ann.Must(204, "DELETE", convPath+ch+"/members/"+string(eve.ID), nil)
	left(eve, "removed")
	// She is no longer a member, and is still told she left the call.
	e.Expect("call.participant_left", is("reason", "removed"))
	// Ann is still in it, alone.
	if got := obj(ann.Must(200, "GET", convPath+ch+"/call", nil), "call"); len(got["participants"].([]any)) != 1 {
		t.Fatalf("who is left: %v", got)
	}
}

// The rules that end a call (docs/CALLS.md section 2).
func TestCallEndsByEachRule(t *testing.T) {
	ended := func(t *testing.T, s *testkit.Stream, reason string) {
		t.Helper()
		if f := s.Expect("call.ended"); f["reason"] != reason {
			t.Fatalf("call.ended: %v, want reason %s", f, reason)
		}
	}
	setup := func(t *testing.T, tm calls.Timings) (*testkit.TestServer, *testkit.Client, string, *testkit.Stream, *calltest.Client) {
		ts := server(t, tm)
		ann, bob := ts.Member("ann"), ts.Member("bob")
		ch := ann.Channel("standup", false, bob)
		watch, _ := bob.Events(nil)
		return ts, ann, ch, watch, enter(t, ann, ch, calltest.Options{})
	}
	t.Run("the last participant leaves", func(t *testing.T) {
		_, _, _, watch, a := setup(t, fast())
		a.Leave()
		ended(t, watch, "empty")
	})
	t.Run("the last participant drops and does not return", func(t *testing.T) {
		ts, ann, ch, watch, a := setup(t, fast())
		a.Publish("microphone")
		eventually(t, "media connected", func() bool { return a.SendState().String() == "connected" })
		// Signalling and media both go, with no word.
		a.Detach()
		a.DropMedia()
		ts.Server.Live.CloseAccount(ann.ID, 1000, "the network went")
		left := watch.Expect("call.participant_left")
		if left["reason"] != "timeout" {
			t.Fatalf("call.participant_left: %v", left)
		}
		ended(t, watch, "empty")
		if out := ann.Must(200, "GET", convPath+ch+"/call", nil); out["call"] != nil {
			t.Fatalf("the call is still on: %v", out)
		}
	})
	t.Run("one participant alone for too long", func(t *testing.T) {
		tm := fast()
		tm.Alone = 500 * time.Millisecond
		_, _, _, watch, a := setup(t, tm)
		at := time.Now()
		ended(t, watch, "alone")
		if time.Since(at) < 300*time.Millisecond {
			t.Fatal("ended as alone too soon")
		}
		// The one who was in it is told on the same connection.
		a.Expect("call.ended", is("reason", "alone"))
	})
	t.Run("two people are not alone", func(t *testing.T) {
		tm := fast()
		tm.Alone = 400 * time.Millisecond
		ts := server(t, tm)
		ann, bob := ts.Member("ann"), ts.Member("bob")
		ch := ann.Channel("standup", false, bob)
		watch, _ := ts.Owner.Events(nil)
		ts.Owner.Must(200, "POST", convPath+ch+"/join", nil)
		a := enter(t, ann, ch, calltest.Options{})
		b := enter(t, bob, ch, calltest.Options{})
		time.Sleep(900 * time.Millisecond)
		if out := ann.Must(200, "GET", convPath+ch+"/call", nil); out["call"] == nil {
			t.Fatal("a call of two ended as if one were alone")
		}
		// One leaves: the clock starts for the other.
		b.Leave()
		ended(t, watch, "alone")
		_ = a
	})
	t.Run("the call has lasted too long", func(t *testing.T) {
		tm := fast()
		tm.Longest = 600 * time.Millisecond
		ts := server(t, tm)
		ann, bob := ts.Member("ann"), ts.Member("bob")
		ch := ann.Channel("standup", false, bob)
		watch, _ := ts.Owner.Events(nil)
		ts.Owner.Must(200, "POST", convPath+ch+"/join", nil)
		enter(t, ann, ch, calltest.Options{})
		enter(t, bob, ch, calltest.Options{})
		ended(t, watch, "too_long")
	})
	t.Run("the conversation is archived", func(t *testing.T) {
		_, ann, ch, watch, _ := setup(t, fast())
		ann.Must(200, "POST", convPath+ch+"/archive", nil)
		ended(t, watch, "closed")
		ann.Refused(403, model.CodeArchived, "POST", convPath+ch+"/call/join", nil)
	})
	t.Run("calls are switched off", func(t *testing.T) {
		ts, ann, ch, watch, _ := setup(t, fast())
		ts.Owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"calls.enabled": false})
		ended(t, watch, "server")
		ann.Refused(403, model.CodeFeatureOff, "POST", convPath+ch+"/call/join", nil)
		if f := obj(ann.Must(200, "GET", "/api/v1/server", nil), "server"); obj(f, "features")["calls"] != false {
			t.Fatalf("the server's description: %v", f["features"])
		}
	})
	t.Run("the server is restarting", func(t *testing.T) {
		ts, _, _, watch, a := setup(t, fast())
		id := a.CallID
		ts.Server.Calls.Close()
		ended(t, watch, "server")
		stored, err := ts.Server.Store.Calls().Get(context.Background(), model.CallID(id))
		if err != nil || stored.Live() {
			t.Fatalf("the stored call after the server stopped: %+v %v", stored, err)
		}
	})
	t.Run("the last participant leaves and its connection closes at once", func(t *testing.T) {
		// The context a leave arrives with is the connection's, and a
		// client that leaves closes the connection as soon as it is
		// answered. The call must still be stored as ended.
		ts, ann, ch, watch, a := setup(t, fast())
		id := a.CallID
		gone, cancel := context.WithCancel(context.Background())
		cancel()
		if err := ts.Server.Calls.Leave(gone, model.Account{ID: model.AccountID(ann.ID)}, model.ConversationID(ch)); err != nil {
			t.Fatal(err)
		}
		ended(t, watch, "empty")
		stored, err := ts.Server.Store.Calls().Get(context.Background(), model.CallID(id))
		if err != nil || stored.Live() {
			t.Fatalf("the stored call after the last one left: %+v %v", stored, err)
		}
	})
	t.Run("nobody ever enters", func(t *testing.T) {
		tm := fast()
		tm.Unjoined = 400 * time.Millisecond
		ts := server(t, tm)
		ann := ts.Member("ann")
		ch := ann.Channel("standup", false)
		watch, _ := ann.Events(nil)
		ticket(t, ann, ch) // and the ticket is never used
		ended(t, watch, "empty")
	})
}

// A server that starts and finds calls with no end ends them: rooms are in
// memory, so those calls are over.
func TestStaleCallsAreEndedAtStart(t *testing.T) {
	ts := server(t, fast())
	ann := ts.Member("ann")
	ch := ann.Channel("standup", false)
	ctx := context.Background()
	// A call as a crash would leave it: a live row and a `call_started`
	// notice, and no room.
	id, _ := ticket(t, ann, ch)
	stored, err := ts.Server.Store.Calls().Get(ctx, model.CallID(id))
	if err != nil {
		t.Fatal(err)
	}
	// A second service on the same database is "the next run".
	next, err := calls.New(calls.Deps{Store: ts.Server.Store, Perm: ts.Server.Perm, Hub: ts.Server.Live, Chat: ts.Server.Chat,
		Rings: ts.Server.Notify, Marks: ts.Server.People, Timings: fast()})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	after, err := ts.Server.Store.Calls().Get(ctx, model.CallID(id))
	if err != nil || after.Live() {
		t.Fatalf("the stale call after the next start: %+v %v", after, err)
	}
	if sys := notice(t, ann, ch, string(stored.MessageID)); sys["kind"] != "call_ended" {
		t.Fatalf("the stale call's notice: %v", sys)
	}
	// Without a media port the service runs and calls are off.
	if next.Port() != 0 {
		t.Fatal("a service with no media address opened a port")
	}
}
