package api_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// TestRemotePresenceByInterest: presence that arrives from another server
// for an account in a shared conversation follows the rule of presence here
// (ruling R62). It goes to the connections that are showing the account and
// to nobody else in the conversation, however many are in it; and a client
// that never said whom it shows is told only of those it shares a small
// conversation with.
func TestRemotePresenceByInterest(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("dan")
	// far stands for an account of another server: it is in the shared
	// channel and never connects here.
	far := ts.Member("far")
	members := []*testkit.Client{ann, bob, dan, far}
	for i := 0; i < 6; i++ { // eleven with the owner
		members = append(members, ts.Member(fmt.Sprintf("m%d", i)))
	}
	shared := ts.Owner.Channel("shared", false, members...) // too large to be "near"

	annS, _ := ann.Events(nil) // shows far
	bobS, _ := bob.Events(nil) // subscribes, but not to far
	danS, _ := dan.Events(nil) // a client from before subscriptions
	annS.Send(map[string]any{"type": "presence.subscribe", "id": "a", "account_ids": []string{string(far.ID)}})
	for {
		f, ok := annS.Next(5 * time.Second)
		if !ok {
			t.Fatal("the subscription was not answered")
		}
		if f["type"] == "ack" {
			break
		}
	}
	bobS.Send(map[string]any{"type": "presence.subscribe", "id": "b", "account_ids": []string{string(ann.ID)}})
	for {
		f, ok := bobS.Next(5 * time.Second)
		if !ok {
			t.Fatal("the subscription was not answered")
		}
		if f["type"] == "ack" {
			break
		}
	}

	// What the link does with presence.changed from the other server.
	ts.Server.Chat.TellLocal(context.Background(), model.ConversationID(shared), model.EventPresenceChanged,
		map[string]any{"account_id": far.ID, "presence": "away", "in_call": false})

	if p := presenceOf(annS, string(far.ID), 5*time.Second); p != "away" {
		t.Fatalf("the connection showing the remote account was told %q, want away", p)
	}
	if p := presenceOf(bobS, string(far.ID), time.Second); p != "" {
		t.Fatalf("a connection that is not showing the remote account was told %q of it", p)
	}
	if p := presenceOf(danS, string(far.ID), time.Second); p != "" {
		t.Fatalf("a client that never subscribed was told %q of a remote account it shares only a large channel with", p)
	}
}
