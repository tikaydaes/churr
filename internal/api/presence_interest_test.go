package api_test

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/testkit"
)

// presenceOf reads frames until one says the presence of an account, alone
// or in a batch, and returns it; "" if none does within the wait.
func presenceOf(s *testkit.Stream, account string, wait time.Duration) string {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		f, ok := s.Next(time.Until(deadline))
		if !ok {
			return ""
		}
		changes := []any{f}
		if f["type"] == "presence.batch" {
			changes, _ = f["changes"].([]any)
		} else if f["type"] != "presence.changed" {
			continue
		}
		for _, c := range changes {
			if m, _ := c.(map[string]any); m["account_id"] == account {
				return m["presence"].(string)
			}
		}
	}
	return ""
}

// TestPresenceByInterest: a connection hears of the presence of the
// accounts it says it is showing, gets where they stand when it says so,
// and hears of nobody else; it cannot subscribe its way to an account it
// may not see; and a client that never subscribes still hears of the people
// it shares a small conversation with. (That it hears of nobody else is
// tested where the hub decides it, in internal/live.)
func TestPresenceByInterest(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	// dan is someone ann shares a direct conversation with.
	dan := ts.Member("dan")
	ann.Channel("pair", true, dan)
	// eve is a guest in a channel ann is not in: ann may not see her.
	eve := ts.Guest("eve", ts.Owner.Channel("lobby", true))

	bobS, _ := bob.Events(nil)
	eveS, _ := eve.Events(nil)
	_, _ = bobS, eveS

	// A client from before subscriptions: it is told about dan, whom it
	// shares a small conversation with.
	oldS, _ := ann.Events(nil)
	if max, _ := oldS.Hello["presence_subscribe_max"].(float64); max != 500 {
		t.Fatalf("hello says presence_subscribe_max %v, want 500", oldS.Hello["presence_subscribe_max"])
	}
	// A client that subscribes: to bob, who is online, and to eve, whom it
	// may not see.
	newS, _ := ann.Events(nil)
	// An account's own connections are told of its own presence, and
	// ann's coming online is worked out a moment after oldS connected:
	// on a slow machine newS is connected by then and is told too. That
	// is not about anyone else, and is let by wherever it comes.
	own := func(f map[string]any) bool {
		return f["type"] == "presence.changed" && f["account_id"] == string(ann.ID)
	}
	newS.Send(map[string]any{"type": "presence.subscribe", "id": "s1", "account_ids": []string{string(bob.ID), string(eve.ID)}})
	// One account: bob, as he stands.
	snap := newS.ExpectWhere("presence.changed", func(f map[string]any) bool { return !own(f) })
	if snap["account_id"] != string(bob.ID) || snap["presence"] != "online" {
		t.Fatalf("the snapshot: %v", snap)
	}
	if ack := newS.Expect("ack"); ack["reply_to"] != "s1" {
		t.Fatalf("the answer: %v", ack)
	}

	catS, _ := cat.Events(nil)
	danS, _ := dan.Events(nil)
	_ = catS
	if p := presenceOf(oldS, string(dan.ID), 5*time.Second); p != "online" {
		t.Fatalf("a client that never subscribed was told %q of someone it shares a small conversation with, want online", p)
	}
	// bob leaves: the subscriber hears. Everything it was sent before
	// that is looked at on the way: none of it may be about cat, dan or
	// eve, whom it did not ask about or may not see.
	bobS.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		f, ok := newS.Next(time.Until(deadline))
		if !ok {
			t.Fatal("the subscriber never heard that bob went offline")
		}
		if f["type"] == "presence.batch" {
			t.Fatalf("an unexpected batch: %v", f)
		}
		if f["type"] != "presence.changed" || own(f) {
			continue
		}
		if f["account_id"] != string(bob.ID) {
			t.Fatalf("the subscriber was told of an account it is not showing: %v", f)
		}
		if f["presence"] == "offline" {
			break
		}
	}
	danS.Close()
	if p := presenceOf(oldS, string(dan.ID), 10*time.Second); p != "offline" {
		t.Fatalf("the old client was told %q when dan left, want offline", p)
	}

	// The set is replaced, not added to: with bob dropped from it, his
	// coming back is not told. More than 500 accounts is refused.
	newS.Send(map[string]any{"type": "presence.subscribe", "id": "s2", "account_ids": []string{}})
	newS.Expect("ack")
	bob.Events(nil)
	ids := make([]string, 501)
	for i := range ids {
		ids[i] = string(bob.ID)
	}
	newS.Send(map[string]any{"type": "presence.subscribe", "id": "s3", "account_ids": ids})
	for {
		f, ok := newS.Next(5 * time.Second)
		if !ok {
			t.Fatal("no answer to a subscription that was too long")
		}
		if (f["type"] == "presence.changed" && !own(f)) || f["type"] == "presence.batch" {
			t.Fatalf("told of presence after dropping the subscription: %v", f)
		}
		if f["type"] == "error" && f["reply_to"] == "s3" {
			break
		}
	}
}
