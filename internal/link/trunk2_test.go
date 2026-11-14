package link_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// A server let into a channel on the home side while the cluster is split.
// The far side went on talking under the key of the epoch before, which the
// new server was never given. After the rejoin those envelopes reach it, and
// it ends with what they said all the same: a server that holds the old key
// opens each for it (docs/LINK-PROTOCOL.md sections 10 and 23b). Before, it
// rejected them quietly (before_joining) and showed nothing of the far side
// until its owner asked for history by hand.
func TestAdmittedDuringASplitReadsTheFarSide(t *testing.T) {
	l := testkit.NewClusterLinked(t, "line", 2, []int{0, 1, 0})
	owner := func(i int) *testkit.Client { return l.Servers[i].Owner }
	ch := owner(0).Channel("launch", false)
	stream, local := l.Share(0, ch, "launch", nil, 1)
	owner(0).Post(ch, "before the split")
	owner(1).HasMessage(local[1], "before the split")

	l.Cluster.Cut(0, 1)
	// The far side talks: messages, a reply, a reaction.
	first := owner(1).Post(local[1], "far 0")
	owner(1).Must(201, "POST", "/api/v1/conversations/"+local[1]+"/messages", map[string]any{"text": "far reply", "parent_id": first["id"]})
	owner(1).Must(200, "PUT", "/api/v1/messages/"+first["id"].(string)+"/reactions/👍", nil)
	owner(1).Post(local[1], "far 1")
	// The home side lets server 2 in: a new key epoch begins there.
	moderate(owner(0), stream, map[string]any{"action": "invite", "server": testkit.ServerName(2)})
	local[2] = l.Accept(2, stream, "launch")
	l.Joined(2, stream)
	owner(0).Post(ch, "home, after the new server")
	owner(2).HasMessage(local[2], "home, after the new server")
	absent(t, owner(2), local[2], "far 0")

	l.Cluster.Restore(0, 1)

	// The new server ends with everything the far side said, the reply in
	// its thread and the reaction on its message, and nobody asked.
	for _, text := range []string{"far 0", "far reply", "far 1"} {
		owner(2).HasMessage(local[2], text)
	}
	testkit.Eventually(t, "the far side's reaction is on the new server's copy", func() bool { return reactionsOn(owner(2), local[2], "far 0") == 1 })
	if m := owner(2).HasMessage(local[2], "far reply"); m["parent_id"] != first["id"] {
		t.Fatalf("the reply on the new server: %v", m)
	}
	st := l.Servers[2].Server.Link.StatsNow()
	if st.Reopened < 4 {
		t.Fatalf("the new server had %d envelopes opened for it, want the four of the split", st.Reopened)
	}
	// And the far side, which hears of the new epoch at the rejoin, goes on
	// under it: what it says now the new server reads by itself.
	testkit.Eventually(t, "the far side posts under the new epoch", func() bool {
		status, _, err := owner(1).Try("POST", "/api/v1/conversations/"+local[1]+"/messages", map[string]any{"text": "far, after the rejoin"})
		return err == nil && status == 201
	})
	for i := range l.Servers {
		owner(i).HasMessage(local[i], "far, after the rejoin")
	}
	// Every server shows the same messages of the split and after it.
	want := fmt.Sprint([]string{"far 0", "far reply", "far 1", "home, after the new server", "far, after the rejoin"})
	testkit.Eventually(t, "every server shows the same messages", func() bool {
		for i := range l.Servers {
			if fmt.Sprint(among(sortedTexts(owner(i), local[i]), "far 0", "far reply", "far 1", "home, after the new server", "far, after the rejoin")) != want {
				return false
			}
		}
		return true
	})
}

// sortedTexts returns the texts of a conversation's messages, replies
// included, in the order of their stamps.
func sortedTexts(c *testkit.Client, conv string) []string {
	type row struct{ order, text string }
	var rows []row
	for _, m := range arr(c.Must(200, "GET", "/api/v1/conversations/"+conv+"/messages?limit=100", nil)["messages"]) {
		m := m.(map[string]any)
		rows = append(rows, row{fmt.Sprint(m["order"]), fmt.Sprint(m["text"])})
		if m["reply_count"] != nil && m["reply_count"].(float64) > 0 {
			for _, r := range arr(c.Must(200, "GET", "/api/v1/messages/"+m["id"].(string)+"/replies?limit=100", nil)["messages"]) {
				r := r.(map[string]any)
				rows = append(rows, row{fmt.Sprint(r["order"]), fmt.Sprint(r["text"])})
			}
		}
	}
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].order < rows[j-1].order; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
	var out []string
	for _, r := range rows {
		out = append(out, r.text)
	}
	return out
}

// The same race without a split: the far hub is slow to hear of the new
// epoch (its trunk delivers everything a moment late), and takes an envelope
// under the old key after a server has been let in on the home side. On one
// hub the hub refuses such an envelope (stale_epoch) and its origin sends it
// again under the new key; across hubs the origin's hub has already taken
// it. The new server has it opened, and shows it.
func TestOldEpochEnvelopeReachesAServerThatJustJoined(t *testing.T) {
	l := testkit.NewClusterLinked(t, "line", 2, []int{0, 1, 0})
	owner := func(i int) *testkit.Client { return l.Servers[i].Owner }
	ch := owner(0).Channel("launch", false)
	stream, local := l.Share(0, ch, "launch", nil, 1)
	owner(0).Post(ch, "before")
	owner(1).HasMessage(local[1], "before")

	// Everything hub b is sent over the trunk arrives 700 ms late.
	l.Cluster.Hubs[1].Hub.TestTrunkDelay(testkit.HubName(0), 700*time.Millisecond)
	moderate(owner(0), stream, map[string]any{"action": "invite", "server": testkit.ServerName(2)})
	local[2] = l.Accept(2, stream, "launch")
	l.Joined(2, stream)
	// Server 2 is in, with the key of the new epoch, and hub b has not heard
	// yet. Server 1 posts under the old one, and its hub takes it.
	if got := l.StreamOn(1, stream)["key_epoch"].(float64); got >= l.StreamOn(2, stream)["key_epoch"].(float64) {
		t.Skipf("the far side heard of the new epoch before it could post under the old one (epoch %v)", got)
	}
	posted := owner(1).Post(local[1], "under the old key")
	if posted["unsent"] == true {
		testkit.Eventually(t, "hub b takes it", func() bool { return owner(1).HasMessage(local[1], "under the old key")["unsent"] != true })
	}
	l.Cluster.Hubs[1].Hub.TestTrunkDelay(testkit.HubName(0), 0)
	owner(0).HasMessage(local[0], "under the old key")
	owner(2).HasMessage(local[2], "under the old key")
	if st := l.Servers[2].Server.Link.StatsNow(); st.Reopened < 1 {
		t.Fatalf("the new server read an old-epoch envelope without having it opened: %+v", st.Rejected)
	}
}

// A server keeps its clock against the other hubs of its hub's cluster
// across a restart (section 23b): it can post in a channel homed on another
// hub before its own hub has pinged it again, and the post is stamped by the
// figure it had written down. Before, the figure was in memory only and the
// post was refused (unavailable) until the hub's first ping.
func TestServerKeepsItsClockAgainstOtherHubsAcrossARestart(t *testing.T) {
	l := testkit.NewClusterLinked(t, "line", 2, []int{0, 1})
	owner := func(i int) *testkit.Client { return l.Servers[i].Owner }
	ch := owner(0).Channel("launch", false)
	_, local := l.Share(0, ch, "launch", nil, 1)
	// Server 1 is on hub b; the channel's home is hub a.
	owner(1).Post(local[1], "before")
	owner(0).HasMessage(ch, "before")

	// Hub b goes away, and server 1 restarts with nobody to ping it.
	l.Cluster.Hubs[1].Stop()
	l.Servers[1].Restart()
	status, out, err := l.Servers[1].Owner.Try("POST", "/api/v1/conversations/"+local[1]+"/messages", map[string]any{"text": "after the restart, before any ping"})
	if err != nil || status != 201 {
		t.Fatalf("a post in a channel homed on another hub, after a restart and before a ping: %d %v %v", status, out, err)
	}
	if m := out["message"].(map[string]any); m["unsent"] != true {
		t.Fatalf("the message is not waiting as unsent: %v", m)
	}
	// Stamped by the home hub's clock as this server last read it: within
	// the checks, so that it is taken when the hub returns.
	at := model.OrderAt(out["message"].(map[string]any)["order"].(string))
	if d := at - int64(model.Now()); d > 5000 || d < -5000 {
		t.Fatalf("the message is stamped %d ms from now", d)
	}
	l.Cluster.Hubs[1].Start()
	owner(0).HasMessage(ch, "after the restart, before any ping")
	testkit.Eventually(t, "it is acknowledged", func() bool {
		return l.Servers[1].Owner.HasMessage(local[1], "after the restart, before any ping")["unsent"] != true
	})
}

// A hub leaves its cluster: its servers' copies of channels homed on other
// hubs stop, and the servers are told so by their hub. Each copy becomes an
// archived local channel with what it held, as when a server is removed.
func TestHubLeavesAndItsServersAreTold(t *testing.T) {
	l := testkit.NewClusterLinked(t, "line", 2, []int{0, 1})
	owner := func(i int) *testkit.Client { return l.Servers[i].Owner }
	ch := owner(0).Channel("launch", false)
	stream, local := l.Share(0, ch, "launch", nil, 1)
	owner(0).Post(ch, "before")
	owner(1).HasMessage(local[1], "before")

	if err := l.Cluster.Hubs[1].Hub.ClusterLeave(); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "server 1's copy has stopped", func() bool {
		st := l.StreamOn(1, stream)
		return st != nil && st["state"] == "removed"
	})
	conv := owner(1).Must(200, "GET", "/api/v1/conversations/"+local[1], nil)["conversation"].(map[string]any)
	if conv["shared"] != nil || conv["archived_at"] == nil {
		t.Fatalf("server 1's copy after its hub left: %v", conv)
	}
	owner(1).HasMessage(local[1], "before")
	// And the home hub has taken it out.
	testkit.Eventually(t, "the home side lists one server", func() bool { return len(arr(hubChannel(owner(0), stream)["servers"])) == 1 })
}

// A channel two servers share over a direct link moves onto a hub when the
// two servers are on different hubs of one cluster (section 21a): onto the
// owner server's hub, which becomes its home. The other server agrees to
// that hub by name and key, as its own hub's roster lists it; hands the
// commit to its own hub, which carries it to the home; and is in the channel
// through its own hub from then on. Nothing is lost, duplicated or
// reordered, and a third server joins afterwards as after any move. Before,
// the owner server's hub refused to prepare the channel (unknown_server).
func TestMoveOntoAHubAcrossACluster(t *testing.T) {
	cl := testkit.NewClusterLinked(t, "line", 2, []int{0, 1, 1})
	f := newMoveOver(t, cl.Linked, testkit.HubName(0))
	f.say(f.amy, f.conv[0], "first a 000")
	f.say(f.bo, f.conv[1], "first b 000")
	stop := f.chatter("moving")
	id := f.propose(nil)
	f.accept(id)
	f.onHub(id)
	time.Sleep(80 * time.Millisecond)
	stop()
	f.say(f.amy, f.conv[0], "last a 000")
	f.say(f.bo, f.conv[1], "last b 000")
	f.same(0, 1)
	// The home is the owner server's hub, and the other server's hub
	// carries the channel for it.
	for i, h := range cl.Cluster.Hubs {
		found := false
		list, _ := h.Hub.Channels()
		for _, c := range list {
			if c.ID == f.stream {
				found = c.Home == testkit.HubName(0)
			}
		}
		if !found {
			t.Fatalf("%s does not hold the channel with its home on %s", testkit.HubName(i), testkit.HubName(0))
		}
	}
	// A third server, on the other hub, is brought in by a moderator.
	moderate(f.bea, f.stream, map[string]any{"action": "invite", "server": testkit.ServerName(2)})
	f.conv[2] = f.Accept(2, f.stream, "launch")
	f.Joined(2, f.stream)
	f.say(f.amy, f.conv[0], "to all three")
	f.Servers[2].Owner.HasMessage(f.conv[2], "to all three")
}
