package link_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/link"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Trunks between hubs (docs/LINK-PROTOCOL.md section 23b, ruling R58), with
// real servers on the hubs. The hubs by themselves, driven by simulated
// servers, are tested in internal/hub.

// clustered starts three hubs in a shape with one server on each, shares a
// channel from the first server to the other two, and returns the stream and
// each server's local conversation.
func clustered(t *testing.T, shape string) (*testkit.ClusterLinked, string, map[int]string) {
	t.Helper()
	l := testkit.NewClusterLinked(t, shape, 3, []int{0, 1, 2})
	ch := l.Servers[0].Owner.Channel("launch", false)
	stream, local := l.Share(0, ch, "launch", nil, 1, 2)
	return l, stream, local
}

// sameOrder waits until every server shows the same messages in the same
// order, and returns that order as texts.
func sameOrder(t *testing.T, l *testkit.ClusterLinked, local map[int]string, want int) []string {
	t.Helper()
	var texts []string
	testkit.Eventually(t, fmt.Sprintf("every server shows the same %d messages in one order", want), func() bool {
		var first []string
		for i, ts := range l.Servers {
			// As a client shows them: sorted by `order`.
			got := orderOf(ts.Owner, local[i])
			if len(got) != want {
				return false
			}
			if i == 0 {
				first = got
			} else if fmt.Sprint(got) != fmt.Sprint(first) {
				return false
			}
		}
		texts = first
		return true
	})
	return texts
}

func TestTrunkShare(t *testing.T) {
	for _, shape := range []string{"line", "star"} {
		t.Run(shape, func(t *testing.T) {
			l, _, local := clustered(t, shape)
			n := 0
			for round := 0; round < 4; round++ {
				for i, ts := range l.Servers {
					ts.Owner.Post(local[i], fmt.Sprintf("%d from %d", round, i))
					n++
				}
			}
			sameOrder(t, l, local, n)
		})
	}
}

// splitShape says, for a shape, which trunk is cut and which hub the far
// servers are on. The channel's home is hub 0 in both.
//
//	line: a - b | c      star: b - a | c
var splitShape = map[string][2]int{"line": {1, 2}, "star": {0, 2}}

// A split with traffic on both sides (section 23b). Server 0 is on the home
// hub, server 1 on a hub that stays with it, servers 2 and 3 on the hub
// that is cut off.
func TestTrunkSplit(t *testing.T) {
	for _, shape := range []string{"line", "star"} {
		t.Run(shape, func(t *testing.T) {
			l := testkit.NewClusterLinked(t, shape, 3, []int{0, 1, 2, 2})
			ch := l.Servers[0].Owner.Channel("launch", false)
			stream, local := l.Share(0, ch, "launch", nil, 1, 2, 3)
			owner := func(i int) *testkit.Client { return l.Servers[i].Owner }
			for i := range l.Servers {
				owner(i).Post(local[i], fmt.Sprintf("before, from %d", i))
			}
			sameOrder(t, l, local, 4)
			// Server 2's owner is made a moderator, so that what stops it
			// during the split is the split and nothing else.
			testkit.Eventually(t, "server 0 knows server 2's owner", func() bool {
				status, _, err := owner(0).Try("POST", "/api/v1/streams/"+stream+"/channel/actions", map[string]any{"action": "add_manager", "account_id": owner(2).ID})
				return err == nil && status == 200
			})
			testkit.Eventually(t, "server 2's owner is a moderator", func() bool {
				return owner(2).Must(200, "GET", "/api/v1/streams/"+stream+"/channel", nil)["manager"] == true
			})

			cut := splitShape[shape]
			l.Cluster.Cut(cut[0], cut[1])

			// Both sides talk, in turn, so that each side's messages fall
			// between the other's by time. Each side delivers among its own
			// servers: a message is shown on the other server of its side
			// before that server's next turn. A message that arrived after
			// that server had posted is late there (it arrived below its
			// own), which is right, and not what the marks below look for.
			partner := map[int]int{0: 1, 1: 0, 2: 3, 3: 2}
			turn := func(i int, text string) {
				owner(i).Post(local[i], text)
				owner(partner[i]).HasMessage(local[partner[i]], text)
				time.Sleep(15 * time.Millisecond)
			}
			for round := 0; round < 3; round++ {
				turn(2, fmt.Sprintf("far %d", round))
				turn(0, fmt.Sprintf("home %d", round))
				turn(3, fmt.Sprintf("far too %d", round))
				turn(1, fmt.Sprintf("home too %d", round))
			}
			time.Sleep(200 * time.Millisecond)
			if n := len(orderOf(owner(0), local[0])); n != 10 {
				t.Fatalf("the home side shows %d messages during the split, want its own 10", n)
			}
			if n := len(orderOf(owner(2), local[2])); n != 10 {
				t.Fatalf("the far side shows %d messages during the split, want its own 10", n)
			}

			// Changes to the channel: refused on the far side, as
			// unavailable, and nothing changes; made on the home side.
			actions := "/api/v1/streams/" + stream + "/channel/actions"
			for _, body := range []map[string]any{
				{"action": "describe", "description": "changed from the far side"},
				{"action": "set_listing", "listing": "open"},
				{"action": "remove_server", "server": testkit.ServerName(3)},
				{"action": "add_manager", "account_id": owner(3).ID},
			} {
				// Refused by the hub at once, which knows it cannot reach
				// the home hub; not by this server giving up on an answer
				// that never comes, which takes five seconds.
				asked := time.Now()
				owner(2).Refused(503, model.CodeUnavailable, "POST", actions, body)
				if took := time.Since(asked); took > 2*time.Second {
					t.Fatalf("%v was refused only after %v: the hub did not answer", body["action"], took)
				}
			}
			if got := hubChannel(owner(2), stream); got["description"] == "changed from the far side" || got["listing"] != "unlisted" || len(arr(got["servers"])) != 4 {
				t.Fatalf("the far side changed the channel during the split: %v", got)
			}
			moderate(owner(0), stream, map[string]any{"action": "describe", "description": "changed from the home side"})
			testkit.Eventually(t, "the home side's other server sees the change", func() bool {
				return hubChannel(owner(1), stream)["description"] == "changed from the home side"
			})

			l.Cluster.Restore(cut[0], cut[1])

			// Everyone converges on one order of the same 16 messages.
			texts := sameOrder(t, l, local, 16)
			// By time: the turns were taken in this order.
			var want []string
			for i := 0; i < 4; i++ {
				want = append(want, fmt.Sprintf("before, from %d", i))
			}
			for round := 0; round < 3; round++ {
				want = append(want, fmt.Sprintf("far %d", round), fmt.Sprintf("home %d", round), fmt.Sprintf("far too %d", round), fmt.Sprintf("home too %d", round))
			}
			if fmt.Sprint(texts) != fmt.Sprint(want) {
				t.Fatalf("after the rejoin the order is\n%v\nwant, by time,\n%v", texts, want)
			}
			// Late marks. On a home-side server every far-side message
			// arrived below something already shown, and its own side's
			// did not. On a far-side server the home side's did, but for
			// the very last, which nothing shown there came after.
			late := func(i int, text string) bool { return owner(i).HasMessage(local[i], text)["late"] == true }
			for round := 0; round < 3; round++ {
				for _, i := range []int{0, 1} {
					if !late(i, fmt.Sprintf("far %d", round)) || !late(i, fmt.Sprintf("far too %d", round)) {
						t.Fatalf("server %d does not mark the far side's round %d late", i, round)
					}
					if late(i, fmt.Sprintf("home %d", round)) || late(i, fmt.Sprintf("home too %d", round)) {
						t.Fatalf("server %d marks its own side's round %d late", i, round)
					}
				}
				for _, i := range []int{2, 3} {
					if !late(i, fmt.Sprintf("home %d", round)) || late(i, fmt.Sprintf("far %d", round)) || late(i, fmt.Sprintf("far too %d", round)) {
						t.Fatalf("server %d: late marks of round %d are wrong", i, round)
					}
					if got := late(i, fmt.Sprintf("home too %d", round)); got != (round < 2) {
						t.Fatalf("server %d: \"home too %d\" late=%v", i, round, got)
					}
				}
			}
			// Nothing twice.
			for i := range l.Servers {
				seen := map[string]bool{}
				for _, text := range orderOf(owner(i), local[i]) {
					if seen[text] {
						t.Fatalf("server %d shows %q twice", i, text)
					}
					seen[text] = true
				}
			}
			// The change made on the home side reaches the far side, and
			// the far side can change the channel again.
			testkit.Eventually(t, "the far side sees the home side's change", func() bool {
				return hubChannel(owner(2), stream)["description"] == "changed from the home side"
			})
			testkit.Eventually(t, "the far side's moderator can act again", func() bool {
				status, _, err := owner(2).Try("POST", actions, map[string]any{"action": "describe", "description": "after the rejoin"})
				return err == nil && status == 200
			})
			testkit.Eventually(t, "and the home side sees it", func() bool { return hubChannel(owner(0), stream)["description"] == "after the rejoin" })
		})
	}
}

// The channel's home hub holds every key epoch's key, sealed at rest, and the
// other hubs of the cluster hold none. No hub's log holds a key in the clear
// or a word of content, and neither does any frame a hub was sent by a
// server or by another hub (the keys the home hub sends travel sealed to
// each server). The home hub holds the channel whole in its holder; a hub
// that only carries it holds none of it.
func TestClusterHubsHoldNoContentAndOnlyTheHomeHoldsKeys(t *testing.T) {
	l := testkit.NewClusterLinked(t, "line", 3, []int{0, 1, 2})
	var mu sync.Mutex
	var seen []byte
	for _, h := range l.Cluster.Hubs {
		h.Hub.TestTap(func(_ string, raw []byte) {
			mu.Lock()
			seen = append(seen, raw...)
			mu.Unlock()
		})
		h.Hub.TestTrunkTap(func(_ string, _ bool, f wire.Frame) {
			raw, _ := wire.EncodeFrame(f)
			mu.Lock()
			seen = append(seen, raw...)
			mu.Unlock()
		})
	}
	var keys [][]byte
	ann := l.Servers[0].Member("zebediah")
	ch := l.Servers[0].Owner.Channel("vault", false, ann)
	stream, local := l.Share(0, ch, "vault", map[string]any{"history_days": 30}, 1, 2)
	ann.Post(ch, "the quarterly numbers are XYZZY-PLUGH")
	l.Servers[1].Owner.Post(local[1], "replying with FROBOZZ-MAGIC")
	l.Servers[2].Owner.Post(local[2], "and from the third hub, QUUX-XYZZX")
	sameOrder(t, l, local, 3)
	// A split and a rejoin, so that the catch-up between hubs has run too.
	l.Cluster.Cut(1, 2)
	l.Servers[2].Owner.Post(local[2], "said while cut off: BLORB-NITFOL")
	ann.Post(ch, "said on the home side: GNUSTO-REZROV")
	l.Cluster.Restore(1, 2)
	sameOrder(t, l, local, 5)
	home := l.Cluster.Hubs[0].Hub.TestStreamKeys(stream)
	// A server on another hub asks the home hub for a key: its own hub
	// carries the request there, and the answer back, unopened.
	cur, _ := l.Servers[2].Server.Link.TestStream(model.StreamID(stream))
	if key, err := l.Servers[2].Server.Link.TestAskHubKey(model.StreamID(stream), cur.KeyEpoch); err != nil || !bytes.Equal(key, home[cur.KeyEpoch]) {
		t.Fatalf("server 2 asked the home hub two trunks away for the key of epoch %d: %v", cur.KeyEpoch, err)
	}

	mu.Lock()
	defer mu.Unlock()
	for i, ts := range l.Servers {
		for epoch, key := range ts.Server.Link.TestKeys(model.StreamID(stream)) {
			if !bytes.Equal(home[epoch], key) {
				t.Errorf("server %d holds a key for epoch %d that is not the home hub's", i, epoch)
			}
			keys = append(keys, key)
		}
	}
	if len(keys) < 2 || len(home) < 2 {
		t.Fatalf("the test knows %d stream keys and the home hub %d; there should be every epoch's", len(keys), len(home))
	}
	for i, h := range l.Cluster.Hubs[1:] {
		if held := h.Hub.TestStreamKeys(stream); len(held) != 0 {
			t.Errorf("hub %d, which only carries the channel, holds %d of its keys", i+1, len(held))
		}
	}
	for i, h := range l.Cluster.Hubs {
		stored := readDir(t, h.Dir)
		if h.Hub.TestStored(stream) < 5 || !bytes.Contains(stored, []byte(stream)) {
			t.Fatalf("hub %d carries the channel and holds %d envelopes of it", i, h.Hub.TestStored(stream))
		}
		all := append(append([]byte{}, stored...), seen...)
		for _, text := range []string{"XYZZY-PLUGH", "FROBOZZ-MAGIC", "QUUX-XYZZX", "BLORB-NITFOL", "GNUSTO-REZROV", "zebediah", "message.created", "author_id"} {
			if bytes.Contains(all, []byte(text)) {
				t.Errorf("hub %d holds, or some hub was sent, %q", i, text)
			}
		}
		for _, key := range keys {
			for _, form := range [][]byte{key, []byte(wire.Encode(key)), []byte(hex.EncodeToString(key)), []byte(base64.StdEncoding.EncodeToString(key))} {
				if bytes.Contains(all, form) {
					t.Errorf("hub %d holds, or some hub was sent, a stream key", i)
				}
			}
		}
	}
	if len(seen) < 10000 {
		t.Fatalf("the hubs were shown only %d bytes of frames", len(seen))
	}
	testkit.Eventually(t, "the home hub's holder holds the channel", func() bool {
		return bytes.Contains(readHeld(t, l.Cluster.Hubs[0].Dir), []byte("XYZZY-PLUGH"))
	})
	for i, h := range l.Cluster.Hubs[1:] {
		if _, err := h.Hub.HolderStore().Streams().Get(context.Background(), model.StreamID(stream)); err == nil {
			t.Errorf("hub %d, which only carries the channel, holds a copy of it", i+1)
		}
	}
}

// A server takes the cluster's roster and a channel's record only when they
// are what they claim.
func TestServerRefusesForgedRosterAndRecord(t *testing.T) {
	l, stream, local := clustered(t, "line")
	a, hubC := l.Servers[2], l.Cluster.Hubs[2] // server 2 is on hub c; the channel's home is hub a
	lk := a.Server.Link
	before := hubChannel(a.Owner, stream)
	entries := hubC.Hub.TestClusterEntries()
	first, _ := entries[0].Member()

	evilDir := filepath.Join(t.TempDir(), "keys")
	evil, err := secret.Open(evilDir)
	if err != nil {
		t.Fatal(err)
	}
	// A record for the channel signed by a key that is in no roster.
	forgedRecord := func(hub string, key wire.Signer, pub []byte) wire.Frame {
		rec, _, _, ok := lk.TestChannel(model.StreamID(stream))
		if !ok {
			t.Fatal("no record held")
		}
		rec.Version += 5
		rec.Hub, rec.HubKey, rec.Description = hub, pub, "forged"
		rec.Managers = []wire.ChannelAccount{{AccountID: string(a.Owner.ID), Server: testkit.ServerName(2), Username: "owner"}}
		f, err := wire.SignBlock(wire.FChannel, rec, key)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	dropped := func(what string) int64 { return lk.StatsNow().Dropped[what] }

	hubC.Hub.TestSendToServer(testkit.ServerName(2), forgedRecord("evil.localhost", evil, evil.SignPublic()))
	testkit.Eventually(t, "the record under an unknown key is refused", func() bool { return dropped("channel_bad_signature") >= 1 })

	// The server's own hub now sends a roster that lists the forger, "let
	// in" by hub a but countersigned by hub c: refused whole.
	forged, _ := wire.SignMember(wire.ClusterMember{Cluster: first.Cluster, Hub: "evil.localhost", SignKey: wire.Bytes(evil.SignPublic()), BoxKey: evil.BoxPublic(),
		Via: testkit.HubName(0), JoinedAt: model.Now()}, evil)
	forged = wire.Countersign(forged, hubC.Hub.TestKeys())
	hubC.Hub.TestSendCluster(append(append([]wire.ClusterEntry{}, entries...), forged))
	testkit.Eventually(t, "the forged roster is refused", func() bool { return dropped("cluster_refused") >= 1 })
	// So the forger's record is still refused.
	hubC.Hub.TestSendToServer(testkit.ServerName(2), forgedRecord("evil.localhost", evil, evil.SignPublic()))
	testkit.Eventually(t, "the forger's record is still refused", func() bool { return dropped("channel_bad_signature") >= 2 })
	// A real member's name with the forger's key is no better.
	hubC.Hub.TestSendToServer(testkit.ServerName(2), forgedRecord(testkit.HubName(0), evil, evil.SignPublic()))
	testkit.Eventually(t, "the home hub's name under another key is refused", func() bool { return dropped("channel_bad_signature") >= 3 })
	// And a real member that is not the channel's home, signing with its
	// own good key: the server's own hub, even.
	hubC.Hub.TestSendToServer(testkit.ServerName(2), forgedRecord(hubC.Name, hubC.Hub.TestKeys(), hubKey(t, hubC)))
	testkit.Eventually(t, "a record signed by a hub that is not the home is refused", func() bool { return dropped("channel_not_home") >= 1 })
	// And the same hub signing with its own good key under the home hub's
	// name: a name goes with the roster's key for it and no other.
	hubC.Hub.TestSendToServer(testkit.ServerName(2), forgedRecord(testkit.HubName(0), hubC.Hub.TestKeys(), hubKey(t, hubC)))
	testkit.Eventually(t, "the home hub's name under another member's key is refused", func() bool { return dropped("channel_bad_signature") >= 4 })

	after := hubChannel(a.Owner, stream)
	if after["description"] == "forged" || fmt.Sprint(after["version"]) != fmt.Sprint(before["version"]) || fmt.Sprint(after["managers"]) != fmt.Sprint(before["managers"]) {
		t.Fatalf("a forged record changed the channel: %v", after)
	}
	if a.Owner.Must(200, "GET", "/api/v1/streams/"+stream+"/channel", nil)["manager"] == true {
		t.Fatal("a forged record made this server's owner a moderator")
	}
	// The real thing still gets through.
	moderate(l.Servers[0].Owner, stream, map[string]any{"action": "describe", "description": "the real one"})
	testkit.Eventually(t, "the home hub's own record is taken", func() bool { return hubChannel(a.Owner, stream)["description"] == "the real one" })
	_ = local
}

func hubKey(t *testing.T, h *testkit.TestHub) []byte {
	t.Helper()
	for _, e := range h.Hub.TestClusterEntries() {
		if m, _ := e.Member(); m.Hub == h.Name {
			return m.SignKey
		}
	}
	t.Fatal("the hub is not in its own roster")
	return nil
}

// The operator moves a channel's home; real servers follow the new home.
func TestTrunkRehome(t *testing.T) {
	l, stream, local := clustered(t, "star")
	to := testkit.HubName(2)
	home := l.Cluster.Hubs[0]
	if err := home.Hub.Rehome(stream, to); err != nil {
		t.Fatal(err)
	}
	home.Hub.Reconcile()
	// (The old home steps down when the new home's answer is in its hands.)
	testkit.Eventually(t, "the move is done", func() bool { return home.Hub.RehomeResult(stream) == "done" })
	for i, ts := range l.Servers {
		i, ts := i, ts
		testkit.Eventually(t, fmt.Sprintf("server %d holds a record signed by the new home", i), func() bool {
			rec, _, _, ok := ts.Server.Link.TestChannel(model.StreamID(stream))
			st, err := ts.Server.Link.TestStream(model.StreamID(stream))
			return ok && rec.Hub == to && rec.HomeHub == to && err == nil && st.HomeHub == to
		})
	}
	// Messages go on, timed by the new home's clock, in one order; the
	// channel is changed through the new home.
	for i, ts := range l.Servers {
		ts.Owner.Post(local[i], fmt.Sprintf("after the move, from %d", i))
	}
	sameOrder(t, l, local, 3)
	moderate(l.Servers[0].Owner, stream, map[string]any{"action": "describe", "description": "under new management"})
	testkit.Eventually(t, "every server sees the change the new home signed", func() bool {
		for _, ts := range l.Servers {
			rec, _, _, _ := ts.Server.Link.TestChannel(model.StreamID(stream))
			if rec.Description != "under new management" || rec.Hub != to {
				return false
			}
		}
		return true
	})
	// The new home began a key epoch of its own when it took the channel:
	// every server is on it, under the new home's key. The old home handed
	// it every earlier key, sealed to it, and now, only a hub that carries
	// the channel, holds none.
	newKeys := l.Cluster.Hubs[2].Hub.TestStreamKeys(stream)
	rec, _, _, _ := l.Servers[0].Server.Link.TestChannel(model.StreamID(stream))
	if newKeys[rec.Epoch] == nil || len(newKeys) != int(rec.Epoch) {
		t.Fatalf("the new home holds keys of epochs %v; the record calls for %d", newKeys, rec.Epoch)
	}
	for epoch := int64(1); epoch < rec.Epoch; epoch++ {
		for i, ts := range l.Servers {
			if held := ts.Server.Link.TestKeys(model.StreamID(stream))[epoch]; held != nil && !bytes.Equal(held, newKeys[epoch]) {
				t.Fatalf("server %d's key of epoch %d is not the one the new home was handed", i, epoch)
			}
		}
	}
	if key, err := l.Servers[0].Server.Link.TestAskHubKey(model.StreamID(stream), 1); err != nil || !bytes.Equal(key, newKeys[1]) {
		t.Fatalf("the new home does not give out the key of epoch 1, from before it was the home: %v", err)
	}
	for i, ts := range l.Servers {
		ts := ts
		testkit.Eventually(t, fmt.Sprintf("server %d is on the new home's epoch", i), func() bool {
			st, _ := ts.Server.Link.TestStream(model.StreamID(stream))
			return st.KeyEpoch == rec.Epoch && bytes.Equal(ts.Server.Link.TestKeys(model.StreamID(stream))[rec.Epoch], newKeys[rec.Epoch])
		})
	}
	if held := home.Hub.TestStreamKeys(stream); len(held) != 0 {
		t.Fatalf("the old home still holds %d of the channel's keys", len(held))
	}
	// Nothing the rightful home sent was dropped for arriving ahead of the
	// record that made it the home: the new home's key, sent to a server on
	// another hub over a different path from the handover, and the first
	// key of a channel, sent to the holder of its hub ahead of the record.
	for i, ts := range l.Servers {
		if d := ts.Server.Link.StatsNow().Dropped; d["key_not_from_owner"] != 0 || d["history_not_from_home"] != 0 {
			t.Fatalf("server %d dropped %v", i, d)
		}
	}
	for i, h := range l.Cluster.Hubs {
		if d := h.Hub.Holder().StatsNow().Dropped; d["key_not_from_owner"] != 0 {
			t.Fatalf("hub %d's holder dropped %v", i, d)
		}
	}
}

// A server that joined a channel just before its home was handed to another
// hub, and is still waiting for its history, asks the new home for it as
// soon as it learns of it. What the old home sent it as the answer to its
// request is not dropped either, should it arrive after: the old home was
// the home when it was asked.
func TestTrunkRehomeWhileHistoryIsOnItsWay(t *testing.T) {
	defer link.SetHistoryAnswerDelay(time.Second)()
	l := testkit.NewClusterLinked(t, "star", 3, []int{0, 1, 2})
	a := l.Servers[0].Owner
	ch := a.Channel("launch", false)
	a.Post(ch, "said before the channel was shared")
	stream, local := l.Share(0, ch, "launch", map[string]any{"history_days": 30}, 1, 2)
	home := l.Cluster.Hubs[0]
	if err := home.Hub.Rehome(stream, testkit.HubName(2)); err != nil {
		t.Fatal(err)
	}
	home.Hub.Reconcile()
	testkit.Eventually(t, "the move is done", func() bool { return home.Hub.RehomeResult(stream) == "done" })
	moved := time.Now()
	l.Servers[2].Owner.HasMessage(local[2], "said before the channel was shared")
	// Asked of the new home as soon as the server learned of it, and not
	// after the wait for an answer that will not come.
	if took := time.Since(moved); took > 4*time.Second {
		t.Fatalf("the history reached the server %v after the move", took)
	}
	for i, ts := range l.Servers {
		if d := ts.Server.Link.StatsNow().Dropped; d["history_not_from_home"] != 0 {
			t.Fatalf("server %d dropped %v", i, d)
		}
	}
}

// A channel's home hub goes away and comes back, with real servers.
func TestTrunkHomeHubFailure(t *testing.T) {
	l, stream, local := clustered(t, "line")
	home := l.Cluster.Hubs[0]
	b, c := l.Servers[1].Owner, l.Servers[2].Owner
	// Server 2's owner is a moderator: what stops it below is the home hub
	// being gone, and nothing else.
	testkit.Eventually(t, "server 0 knows server 2's owner", func() bool {
		status, _, err := l.Servers[0].Owner.Try("POST", "/api/v1/streams/"+stream+"/channel/actions", map[string]any{"action": "add_manager", "account_id": c.ID})
		return err == nil && status == 200
	})
	testkit.Eventually(t, "server 2's owner is a moderator", func() bool {
		return c.Must(200, "GET", "/api/v1/streams/"+stream+"/channel", nil)["manager"] == true
	})
	home.Stop()
	b.Post(local[1], "the home hub is down")
	c.HasMessage(local[2], "the home hub is down")
	c.Post(local[2], "and still we talk")
	b.HasMessage(local[1], "and still we talk")
	// Read and post only. The server's own hub says so at once.
	asked := time.Now()
	c.Refused(503, model.CodeUnavailable, "POST", "/api/v1/streams/"+stream+"/channel/actions", map[string]any{"action": "describe", "description": "x"})
	if took := time.Since(asked); took > 2*time.Second {
		t.Fatalf("the change was refused only after %v: the hub did not answer", took)
	}
	home.Start()
	l.Cluster.Whole()
	a := l.Servers[0].Owner
	a.HasMessage(local[0], "the home hub is down")
	a.HasMessage(local[0], "and still we talk")
	a.Post(local[0], "back")
	sameOrder(t, l, local, 3)
}
