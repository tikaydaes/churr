package live

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// presenceQueued is the presence frames queued for connections so far and
// their bytes, once no more are being added.
func presenceQueued(h *Hub) (frames, bytes int64) {
	read := func() (int64, int64) {
		q := h.Stats().Queued
		a, b := q[model.EventPresenceChanged], q[EventPresenceBatch]
		return a.Frames + b.Frames, a.Bytes + b.Bytes
	}
	frames, bytes = read()
	for quiet := 0; quiet < 10; {
		time.Sleep(30 * time.Millisecond)
		f, b := read()
		if f == frames && b == bytes {
			quiet++
		} else {
			frames, bytes, quiet = f, b, 0
		}
	}
	return frames, bytes
}

// connectAll connects n accounts one after another. Each says, as a client
// does once it is ready, that it is showing the watch accounts after it;
// each coming online is told to the connections showing it.
func connectAll(t *testing.T, n, watch int, byInterest bool) (frames, bytes int64) {
	c := newCrowd(t, n, Options{})
	if byInterest {
		c.hub.OnPresence(func(account model.AccountID) {
			c.hub.SendPresenceToInterested(account, presenceEvent(account, c.hub.Presence(account)), nil, nil)
		})
	} else {
		c.everyoneSeesEveryone()
	}
	for i := 0; i < n; i++ {
		c.dial(i)
		var ids []model.AccountID
		for k := 1; k <= watch; k++ {
			ids = append(ids, c.ids[(i+k)%n])
		}
		conns := c.hub.connsOf(c.ids[i])
		if len(conns) != 1 {
			t.Fatalf("account %d has %d connections", i, len(conns))
		}
		if _, ok := c.hub.SetPresenceInterest(c.ids[i], conns[0].id, ids); !ok {
			t.Fatalf("connection %d could not subscribe", i)
		}
	}
	return presenceQueued(c.hub)
}

// TestConnectingCostsPresenceInProportion: the presence sent while N
// members connect grows with N, not with its square. Told to everyone who
// may see them, as it was, twice the members cost four times the bytes.
func TestConnectingCostsPresenceInProportion(t *testing.T) {
	const n, watch = 60, 5
	f1, b1 := connectAll(t, n, watch, true)
	f2, b2 := connectAll(t, 2*n, watch, true)
	t.Logf("by interest: %d members: %d frames, %d bytes; %d members: %d frames, %d bytes", n, f1, b1, 2*n, f2, b2)
	if f1 == 0 || b1 == 0 {
		t.Fatal("nobody was told anything")
	}
	// Each account's coming online is told to itself and to at most
	// `watch` connections showing it.
	if f1 > int64(n*(watch+1)) || f2 > int64(2*n*(watch+1)) {
		t.Fatalf("%d and %d presence frames, more than one for each connection showing each account", f1, f2)
	}
	if float64(b2) > 2.3*float64(b1) {
		t.Fatalf("twice the members cost %.1f times the presence bytes", float64(b2)/float64(b1))
	}
	_, o1 := connectAll(t, n, watch, false)
	_, o2 := connectAll(t, 2*n, watch, false)
	t.Logf("to everyone: %d members: %d bytes; %d members: %d bytes", n, o1, 2*n, o2)
	if float64(o2) < 3*float64(o1) {
		t.Fatalf("told to everyone, twice the members cost only %.1f times the bytes: this test no longer shows the difference", float64(o2)/float64(o1))
	}
}

// TestPresenceGoesToThoseShowingTheAccount: own connections, connections
// showing the account whose owner may see it, and connections near it that
// have never subscribed; and a connection that closes is forgotten.
func TestPresenceGoesToThoseShowingTheAccount(t *testing.T) {
	c := newCrowd(t, 6, Options{})
	cl := make([]*client, 6)
	for i := range cl {
		cl[i] = c.dial(i)
	}
	id := func(i int) string { return c.hub.connsOf(c.ids[i])[0].id }
	about := c.ids[0]
	// 1 and 2 show account 0; 2 may not see it. 3 shows someone else.
	// 4 and 5 have never subscribed; 4 is near.
	c.hub.SetPresenceInterest(c.ids[1], id(1), []model.AccountID{about})
	c.hub.SetPresenceInterest(c.ids[2], id(2), []model.AccountID{about})
	c.hub.SetPresenceInterest(c.ids[3], id(3), []model.AccountID{c.ids[5]})
	maySee := func(a model.AccountID) bool { return a != c.ids[2] }
	c.hub.SendPresenceToInterested(about, presenceEvent(about, model.PresenceAway), []model.AccountID{c.ids[3], c.ids[4]}, maySee)
	got := func(i int) string {
		cl[i].mu.Lock()
		defer cl[i].mu.Unlock()
		return cl[i].presence[string(about)]
	}
	deadline := time.Now().Add(5 * time.Second)
	for (got(0) == "" || got(1) == "" || got(4) == "") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	for i, want := range []string{"away", "away", "", "", "away", ""} {
		if got(i) != want {
			t.Errorf("connection %d holds %q for the account, want %q", i, got(i), want)
		}
	}
	// A new set takes the place of the old; a closed connection is gone.
	if added, _ := c.hub.SetPresenceInterest(c.ids[1], id(1), []model.AccountID{about, c.ids[5]}); len(added) != 1 || added[0] != c.ids[5] {
		t.Errorf("adding one account reported %v as new", added)
	}
	c.hub.SetPresenceInterest(c.ids[1], id(1), nil)
	cl[2].ws.CloseNow()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.hub.interest.mu.RLock()
		left := len(c.hub.interest.by[about])
		c.hub.interest.mu.RUnlock()
		if left == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the account is still being shown by connections that dropped it or closed")
}
