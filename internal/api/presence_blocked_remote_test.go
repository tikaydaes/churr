package api_test

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// blockedAcross is two linked servers: ann on a, lee and bob on b, all in one
// shared channel. ann has blocked lee, and b has been told.
type blockedAcross struct {
	l             *testkit.Linked
	a, b          *testkit.TestServer
	ann, lee, bob *testkit.Client
	chA, chB      string
}

func newBlockedAcross(t *testing.T) *blockedAcross {
	t.Helper()
	l := testkit.NewLinked(t, 2, false)
	x := &blockedAcross{l: l, a: l.Servers[0], b: l.Servers[1]}
	x.ann = x.a.Member("ann")
	x.chA = x.a.Owner.Channel("bridge", false, x.ann)
	_, local := l.Share(0, x.chA, "bridge", nil, 1)
	x.chB = local[1]
	x.lee, x.bob = x.b.Member("lee"), x.b.Member("bob")
	x.lee.Must(200, "POST", "/api/v1/conversations/"+x.chB+"/join", nil)
	x.bob.Must(200, "POST", "/api/v1/conversations/"+x.chB+"/join", nil)
	testkit.Eventually(t, "a knows lee and bob", func() bool {
		_, l1, _ := x.a.Server.Store.Members().Get(fctx, model.ConversationID(x.chA), x.lee.ID)
		_, l2, _ := x.a.Server.Store.Members().Get(fctx, model.ConversationID(x.chA), x.bob.ID)
		return l1 && l2
	})
	testkit.Eventually(t, "b knows ann", func() bool {
		_, found, _ := x.b.Server.Store.Members().Get(fctx, model.ConversationID(x.chB), x.ann.ID)
		return found
	})
	return x
}

func (x *blockedAcross) block(t *testing.T, on bool) {
	t.Helper()
	if on {
		x.ann.Must(204, "PUT", "/api/v1/me/blocks/"+string(x.lee.ID), nil)
	} else {
		x.ann.Must(204, "DELETE", "/api/v1/me/blocks/"+string(x.lee.ID), nil)
	}
	testkit.Eventually(t, "b hears of the block", func() bool {
		return x.b.Server.Link.DeafViewers(x.ann.ID)[x.lee.ID] == on
	})
}

// subscribe opens an event stream that shows ann, once the subscription
// has been taken. What it is first told of ann is still to be read.
func (x *blockedAcross) subscribe(t *testing.T, c *testkit.Client) *testkit.Stream {
	t.Helper()
	s, _ := c.Events(nil)
	s.Send(map[string]any{"type": "presence.subscribe", "id": "s", "account_ids": []string{string(x.ann.ID)}})
	return s
}

// answer is the presence a new subscription is first told of ann, when b
// holds some (a read that waits in vain closes the stream).
func (x *blockedAcross) answer(t *testing.T, c *testkit.Client) string {
	t.Helper()
	s := x.subscribe(t, c)
	defer s.Close()
	return presenceOf(s, string(x.ann.ID), 5*time.Second)
}

func aboutAnn(ann string) func(f map[string]any) bool {
	return func(f map[string]any) bool {
		switch f["type"] {
		case "presence.changed", "typing.started":
			return f["account_id"] == ann
		case "presence.batch":
			return mentions(f, ann)
		}
		return false
	}
}

// TestBlockedPresenceAcrossServers: ann on a has blocked lee on b (ruling
// R77, docs/LINK-PROTOCOL.md section 17a). On b, by every path presence and
// typing reach a viewer, lee is shown ann online with nothing more and
// none of her typing, while bob sees what a says of her: away. After the
// block is lifted lee sees the truth again.
func TestBlockedPresenceAcrossServers(t *testing.T) {
	x := newBlockedAcross(t)
	ann := string(x.ann.ID)
	x.block(t, true)
	// Before b has heard anything of ann, she is offline to bob and online
	// to lee.
	bobS := x.subscribe(t, x.bob)
	if p := presenceOf(bobS, ann, 5*time.Second); p != "offline" {
		t.Fatalf("bob's first answer: %q", p)
	}
	leeS := x.subscribe(t, x.lee)
	if p := presenceOf(leeS, ann, 5*time.Second); p != "online" {
		t.Fatalf("lee's first answer: %q", p)
	}
	// ann appears away and connects: a sends away.
	x.ann.Must(200, "PUT", "/api/v1/me/away", nil)
	annS, _ := x.ann.Events(nil)
	defer annS.Close()

	// presence.changed from the link: to the connections showing ann, lee's
	// left out.
	if p := presenceOf(bobS, ann, 5*time.Second); p != "away" {
		t.Fatalf("bob was told %q of ann, want away", p)
	}
	quiet(t, leeS, 700*time.Millisecond, aboutAnn(ann))

	// A subscription's first answer.
	if p := x.answer(t, x.lee); p != "online" {
		t.Fatalf("lee's subscription answers %q for ann, want online", p)
	}
	if p := x.answer(t, x.bob); p != "away" {
		t.Fatalf("bob's subscription answers %q for ann, want away", p)
	}

	// The account by itself and in the list.
	seen := obj(x.lee.Must(200, "GET", "/api/v1/accounts/"+ann, nil), "account")
	truth := obj(x.bob.Must(200, "GET", "/api/v1/accounts/"+ann, nil), "account")
	if seen["presence"] != "online" || seen["status"] != nil || seen["dnd"] == true || seen["in_call"] == true || seen["availability"] != nil {
		t.Fatalf("lee's view of ann: %v", seen)
	}
	if truth["presence"] != "away" {
		t.Fatalf("bob's view of ann: %v", truth)
	}
	inList := func(c *testkit.Client) map[string]any {
		for _, a := range arr(c.Must(200, "GET", "/api/v1/accounts?q=ann", nil), "accounts") {
			if a := a.(map[string]any); a["id"] == ann {
				return a
			}
		}
		t.Fatal("ann is not in the list")
		return nil
	}
	if a := inList(x.lee); a["presence"] != "online" || a["in_call"] == true {
		t.Fatalf("ann in lee's list: %v", a)
	}
	if a := inList(x.bob); a["presence"] != "away" {
		t.Fatalf("ann in bob's list: %v", a)
	}

	// A watch: around at once for lee, as for anyone who blocked them;
	// bob's waits.
	if made := x.lee.Must(200, "PUT", fWatches+ann, nil); made["around"] != true {
		t.Fatalf("lee's watch for ann: %v", made)
	}
	if made := x.bob.Must(201, "PUT", fWatches+ann, nil); made["around"] != false {
		t.Fatalf("bob's watch for ann: %v", made)
	}

	// Typing in the shared channel, and a call change on b (presence.changed
	// from b's own people service): bob is told both, lee neither.
	leeS = x.subscribe(t, x.lee)
	if p := presenceOf(leeS, ann, 5*time.Second); p != "online" {
		t.Fatalf("lee's new subscription answers %q", p)
	}
	x.ann.Must(204, "POST", "/api/v1/conversations/"+x.chA+"/typing", map[string]any{})
	bobS.ExpectWhere("typing.started", func(f map[string]any) bool { return f["account_id"] == ann })
	x.b.Server.People.CallChanged(x.ann.ID)
	if p := presenceOf(bobS, ann, 5*time.Second); p != "away" {
		t.Fatalf("bob was told %q of ann at a call change", p)
	}
	quiet(t, leeS, 700*time.Millisecond, aboutAnn(ann))

	// Unblocked: lee is told the truth at once, and from then on as anyone.
	leeS = x.subscribe(t, x.lee)
	if p := presenceOf(leeS, ann, 5*time.Second); p != "online" {
		t.Fatalf("lee's new subscription answers %q", p)
	}
	x.block(t, false)
	if p := presenceOf(leeS, ann, 5*time.Second); p != "away" {
		t.Fatalf("after the unblock lee was told %q, want away", p)
	}
	if seen := obj(x.lee.Must(200, "GET", "/api/v1/accounts/"+ann, nil), "account"); seen["presence"] != "away" {
		t.Fatalf("lee's view of ann after the unblock: %v", seen)
	}
	if p := x.answer(t, x.lee); p != "away" {
		t.Fatalf("lee's subscription after the unblock answers %q", p)
	}
	x.ann.Must(204, "POST", "/api/v1/conversations/"+x.chA+"/typing", map[string]any{})
	leeS.ExpectWhere("typing.started", func(f map[string]any) bool { return f["account_id"] == ann })
}

// TestBlockedWatchAcrossServers: a watch lee set for ann before she blocked
// him never fires (it would say when she is really around), while bob's
// fires; and lee hears nothing of her coming online, having been told at
// the block that she is.
func TestBlockedWatchAcrossServers(t *testing.T) {
	x := newBlockedAcross(t)
	ann := string(x.ann.ID)
	lw := obj(x.lee.Must(201, "PUT", fWatches+ann, nil), "scheduled")
	bw := obj(x.bob.Must(201, "PUT", fWatches+ann, nil), "scheduled")
	leeS := x.subscribe(t, x.lee)
	if p := presenceOf(leeS, ann, 5*time.Second); p != "offline" {
		t.Fatalf("lee's first answer: %q", p)
	}
	bobS, _ := x.bob.Events(nil)
	x.block(t, true)
	if p := presenceOf(leeS, ann, 5*time.Second); p != "online" {
		t.Fatalf("at the block lee was told %q", p)
	}
	annS, _ := x.ann.Events(nil)
	defer annS.Close()
	wFired(t, bobS, bw["id"].(string))
	quiet(t, leeS, 700*time.Millisecond, func(f map[string]any) bool {
		return aboutAnn(ann)(f) || f["type"] == "scheduled.fired"
	})
	if list := wWaiting(x.lee); len(list) != 1 || list[0]["id"] != lw["id"] {
		t.Fatalf("lee's waiting watches: %v", list)
	}
}
