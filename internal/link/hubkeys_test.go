package link_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/link"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The hub makes every key epoch's key of a channel it owns and keeps each,
// sealed at rest: every server holds the hub's keys and no other, a server
// holds none from before it joined or after it was removed, and nothing the
// hub stores or is sent holds a key in the clear, its holder's database
// included. Its log keeps the envelopes sealed, as they travel; what it reads
// of them its holder keeps (TestTheHubHoldsTheWholeChannel).
func TestHubMakesKeysAndKeepsThemSealed(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	var mu sync.Mutex
	var received []byte
	frames := map[string]int{}
	l.Hub.Hub.TestTap(func(server string, raw []byte) {
		f, _ := wire.DecodeFrame(raw)
		mu.Lock()
		received = append(append(received, raw...), '\n')
		frames[f.Frame]++
		mu.Unlock()
	})
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	ch := a.Owner.Channel("vault", false)
	stream, local := l.Share(0, ch, "vault", map[string]any{"listing": "open", "history_days": 30}, 1)
	sid := model.StreamID(stream)
	a.Owner.Post(ch, "PLAINTEXT-ALPHA from a")
	b.Owner.Post(local[1], "PLAINTEXT-BRAVO from b")
	a.Owner.HasMessage(ch, "PLAINTEXT-BRAVO from b")
	// c joins (a new epoch), posts, and is removed (another).
	convC := joinListed(l, 2, stream, "vault-c")
	l.Joined(2, stream)
	c.Owner.HasMessage(convC, "PLAINTEXT-ALPHA from a") // history, which travels sealed
	c.Owner.Post(convC, "PLAINTEXT-CHARLIE from c")
	b.Owner.HasMessage(local[1], "PLAINTEXT-CHARLIE from c")
	joinedAt, _ := c.Server.Link.TestStream(sid)
	moderate(a.Owner, stream, map[string]any{"action": "remove_server", "server": "c.localhost"})
	testkit.Eventually(t, "a new epoch on a and b after the removal", func() bool {
		for _, i := range []int{0, 1} {
			if cur, _ := l.Servers[i].Server.Link.TestStream(sid); cur.KeyEpoch <= joinedAt.KeyEpoch {
				return false
			}
		}
		return true
	})
	a.Owner.Post(ch, "PLAINTEXT-DELTA after the removal")
	b.Owner.HasMessage(local[1], "PLAINTEXT-DELTA after the removal")

	hubKeys := l.Hub.Hub.TestStreamKeys(stream)
	cur, _ := a.Server.Link.TestStream(sid)
	if len(hubKeys) < 3 || hubKeys[cur.KeyEpoch] == nil {
		t.Fatalf("the hub holds %d keys and none for the current epoch %d", len(hubKeys), cur.KeyEpoch)
	}
	keys := map[string][]byte{}
	for i, ts := range l.Servers {
		for epoch, key := range ts.Server.Link.TestKeys(sid) {
			if !bytes.Equal(hubKeys[epoch], key) {
				t.Errorf("%s holds a key for epoch %d that is not the hub's", testkit.ServerName(i), epoch)
			}
			keys[fmt.Sprintf("%s holds epoch %d", testkit.ServerName(i), epoch)] = key
		}
	}
	held := c.Server.Link.TestKeys(sid)
	if _, ok := held[cur.KeyEpoch]; ok {
		t.Error("c holds the key of the epoch that began when it was removed")
	}
	if _, ok := held[1]; ok {
		t.Error("c holds the key of an epoch from before it joined")
	}
	mu.Lock()
	defer mu.Unlock()
	if frames[wire.FSubmit] < 4 || frames[wire.FChannelCreate] != 1 || frames[wire.FJoin] < 1 || frames[wire.FChannelAction] < 1 {
		t.Fatalf("the hub received %v", frames)
	}
	stored := readDir(t, l.Hub.Dir)
	holder := readHeld(t, l.Hub.Dir)
	if len(stored) < 1000 || len(received) < 1000 || !bytes.Contains(holder, []byte("PLAINTEXT-ALPHA")) {
		t.Fatalf("the hub stored %d bytes and received %d", len(stored), len(received))
	}
	for where, hay := range map[string][]byte{"storage": stored, "frames received": received, "holder": holder} {
		for what, key := range keys {
			for enc, needle := range map[string][]byte{
				"raw":       key,
				"base64url": []byte(base64.RawURLEncoding.EncodeToString(key)),
				"base64":    []byte(base64.StdEncoding.EncodeToString(key)),
				"hex":       []byte(hex.EncodeToString(key)),
			} {
				if bytes.Contains(hay, needle) {
					t.Errorf("the hub's %s contains the key %s in the clear (%s)", where, what, enc)
				}
			}
		}
		for _, text := range []string{"PLAINTEXT-ALPHA", "PLAINTEXT-BRAVO", "PLAINTEXT-CHARLIE", "PLAINTEXT-DELTA", "message.created", "author_id"} {
			if where != "holder" && bytes.Contains(hay, []byte(text)) {
				t.Errorf("the hub's %s contains %q", where, text)
			}
		}
	}
}

// A server takes a hub-owned channel's key from the home hub and from nobody
// else: a key a member server sends it changes nothing. The hub answers a
// server's key_request for an epoch it was in, and refuses one from before
// it joined.
func TestHubChannelKeysComeOnlyFromTheHub(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	ch := a.Owner.Channel("relay", false)
	stream, local := l.Share(0, ch, "relay", map[string]any{"listing": "open", "history_days": 30}, 1)
	sid := model.StreamID(stream)
	a.Owner.Post(ch, "before c")
	b.Owner.HasMessage(local[1], "before c")
	convC := joinListed(l, 2, stream, "relay-c")
	l.Joined(2, stream)
	c.Owner.HasMessage(convC, "before c")
	cur, _ := c.Server.Link.TestStream(sid)
	epoch := cur.KeyEpoch
	// a, a member server, sends c a key for the next epoch: c drops it.
	if _, err := a.Server.Link.TestDirect(model.PeerID(l.Peers[0]), "c.localhost", "key", map[string]any{"stream_id": stream, "epoch": epoch + 1,
		"key": wire.Bytes(bytes.Repeat([]byte{9}, 32))}, model.Now(), nil); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "c drops a key from a member server", func() bool {
		return c.Server.Link.StatsNow().Dropped["key_not_from_owner"] >= 1
	})
	if _, held := c.Server.Link.TestKeys(sid)[epoch+1]; held {
		t.Fatal("c took a key from a member server")
	}
	// Asked, the hub answers with its key of an epoch c is in, and
	// refuses epoch 1, which began before c joined.
	key, err := c.Server.Link.TestAskHubKey(sid, epoch)
	if err != nil || !bytes.Equal(key, l.Hub.Hub.TestStreamKeys(stream)[epoch]) {
		t.Fatalf("the hub's answer for epoch %d: %v", epoch, err)
	}
	if key, err := c.Server.Link.TestAskHubKey(sid, 1); err == nil || len(key) != 0 {
		t.Fatal("the hub gave c the key of an epoch from before it joined")
	}
	if key, err := a.Server.Link.TestAskHubKey(sid, 1); err != nil || len(key) != 32 {
		t.Fatalf("the hub refused a, which shared the channel at epoch 1: %v", err)
	}
	c.Owner.Post(convC, "c is in")
	a.Owner.HasMessage(ch, "c is in")
	b.Owner.HasMessage(local[1], "c is in")
}

// What is said just after an instance is let in reaches it by the hub's log,
// and the snapshot it asked for is cut later still: such a message is in
// both. The instance takes it once, from the log, and rejects nothing.
func TestSaidWhileTheSnapshotIsCutIsTakenOnce(t *testing.T) {
	defer link.SetHistoryAnswerDelay(1500 * time.Millisecond)()
	l := testkit.NewLinked(t, 3, true)
	a, c := l.Servers[0], l.Servers[2]
	ch := a.Owner.Channel("busy", false)
	stream, local := l.Share(0, ch, "busy", map[string]any{"listing": "open", "history_days": 30}, 1)
	a.Owner.Post(ch, "before c")
	l.Servers[1].Owner.HasMessage(local[1], "before c")
	convC := joinListed(l, 2, stream, "busy-c")
	testkit.Eventually(t, "c is in", func() bool { return streamState(l, 2, stream) == "active" })
	a.Owner.Post(ch, "just after c was let in")
	l.Servers[1].Owner.Post(local[1], "and from b")
	c.Owner.HasMessage(convC, "before c")
	c.Owner.HasMessage(convC, "just after c was let in")
	c.Owner.HasMessage(convC, "and from b")
	for _, text := range []string{"before c", "just after c was let in", "and from b"} {
		if n := countText(c.Owner, convC, text); n != 1 {
			t.Fatalf("c holds %q %d times", text, n)
		}
	}
	r := c.Server.Link.StatsNow().Rejected
	delete(r, "duplicate")
	if len(r) != 0 {
		t.Fatalf("c rejected %v", r)
	}
}

// An instance leaves a hub-owned channel for good, and an instance that joins
// afterwards is still given everything that instance's people wrote, under
// their names: the history comes from an instance that remains.
func TestHistoryOutlivesAnInstanceThatLeft(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	ch := a.Owner.Channel("archive", false)
	stream, local := l.Share(0, ch, "archive", map[string]any{"listing": "open", "history_days": 30}, 1)
	a.Owner.Post(ch, "from a, kept")
	said := b.Owner.Post(local[1], "from b, kept after b is gone")
	a.Owner.HasMessage(ch, "from b, kept after b is gone")
	author := said["author_id"].(string)
	name := a.Owner.Must(200, "GET", "/api/v1/accounts/"+author, nil)["account"].(map[string]any)["username"]
	// b leaves the channel for good.
	b.Owner.Must(204, "DELETE", "/api/v1/admin/streams/"+stream, nil)
	testkit.Eventually(t, "the hub has taken b out", func() bool { return names(hubChannel(a.Owner, stream)["servers"]) == "a.localhost" })
	a.Owner.HasMessage(ch, "from b, kept after b is gone")
	// c joins afterwards and is given all of it.
	convC := joinListed(l, 2, stream, "archive-c")
	l.Joined(2, stream)
	c.Owner.HasMessage(convC, "from a, kept")
	c.Owner.HasMessage(convC, "from b, kept after b is gone")
	// b's person is shown by name on a and on c, though no longer a member
	// anywhere: whoever is shown a message is shown its author.
	for i, ts := range []*testkit.TestServer{a, c} {
		acc := ts.Owner.Must(200, "GET", "/api/v1/accounts/"+author, nil)["account"].(map[string]any)
		if name == nil || acc["username"] != name {
			t.Fatalf("on %d b's person is %v, not %v", i, acc["username"], name)
		}
	}
	stored, err := c.Server.Store.Accounts().Get(context.Background(), model.AccountID(author))
	if err != nil || stored.Username != name {
		t.Fatalf("c holds b's person as %q (%v)", stored.Username, err)
	}
}
