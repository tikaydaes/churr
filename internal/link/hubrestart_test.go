package link_test

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// A hub restarts. What it needs is in its database: its servers and their
// keys, their clocks, the channels it owns and the log. These are the tests
// the hub's testkit could not run before it could restart a hub.

func TestHubRestart(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0].Owner, l.Servers[1].Owner, l.Servers[2].Owner
	ch := a.Channel("launch", false)
	stream, local := l.Share(0, ch, "launch", nil, 1, 2)
	a.Post(ch, "before")
	b.HasMessage(local[1], "before")
	c.HasMessage(local[2], "before")
	held := l.Hub.Hub.TestStored(stream)

	l.Hub.Stop()
	// Said while the hub is down: shown at once where it was said, unsent.
	if m := a.Post(ch, "while the hub was down"); m["unsent"] != true {
		t.Fatalf("a message posted with the hub down: %v", m)
	}
	b.Post(local[1], "b too")
	l.Hub.Start()

	// The servers dial again, are measured again, and what waited goes.
	c.HasMessage(local[2], "while the hub was down")
	c.HasMessage(local[2], "b too")
	a.HasMessage(ch, "b too")
	b.HasMessage(local[1], "while the hub was down")
	testkit.Eventually(t, "nothing is unsent any more", func() bool {
		return a.HasMessage(ch, "while the hub was down")["unsent"] != true
	})
	// The restarted hub still owns the channel: its record is there, the
	// log goes on from where it was, and a moderator's action is answered.
	list, err := l.Hub.Hub.Channels()
	if err != nil || len(list) != 1 || list[0].ID != stream || len(list[0].Servers) != 3 || len(list[0].Managers) != 1 {
		t.Fatalf("the restarted hub's channels: %+v %v", list, err)
	}
	testkit.Eventually(t, "the log holds the two more", func() bool { return l.Hub.Hub.TestStored(stream) == held+2 })
	seen := map[string]bool{}
	for _, env := range l.Hub.Hub.TestLog(stream) {
		if seen[env.OriginEventID] {
			t.Fatal("the restarted hub logged an event twice")
		}
		seen[env.OriginEventID] = true
	}
	testkit.Eventually(t, "a moderator's action is answered by the restarted hub", func() bool {
		status, _, err := a.Try("POST", "/api/v1/streams/"+stream+"/channel/actions", map[string]any{"action": "describe", "description": "still here"})
		return err == nil && status == 200
	})
	testkit.Eventually(t, "and reaches the others", func() bool { return hubChannel(c, stream)["description"] == "still here" })
	// A server that was away through the restart catches up from the log.
	l.Servers[2].Restart()
	a.Post(ch, "after")
	l.Servers[2].Owner.HasMessage(local[2], "after")
	if got := orderOf(l.Servers[2].Owner, local[2]); len(got) != 4 {
		t.Fatalf("c shows %v", got)
	}
}

// A hub restarted between preparing a moved channel and being handed the
// commit (section 21a): the prepared channel is in its database, so the
// move completes when the servers reach it again. (A hub that loses its
// database at that point is another matter, and is not recovered: see
// docs/LINK-PROTOCOL.md section 27.)
func TestMoveSurvivesAHubRestartBeforeTheCommit(t *testing.T) {
	f := newMove(t)
	f.lk(0).TestStep(func(step string) bool { return step == "commit" })
	id := f.propose(nil)
	f.accept(id)
	testkit.Eventually(t, "the hub has prepared the channel", func() bool {
		rec, _, _, ok := f.lk(0).TestChannel(model.StreamID(f.stream))
		return ok && rec.Prepared
	})
	if f.state(0, id) != "accepted" {
		t.Fatalf("the owner server is at %q before the commit", f.state(0, id))
	}
	stop := f.chatter("through the hub's restart")
	f.Hub.Restart()
	// The restarted hub holds the prepared channel and tells nobody of it.
	list, err := f.Hub.Hub.Channels()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.ID == f.stream {
			t.Fatal("a channel that is only prepared is listed as open")
		}
	}
	f.lk(0).TestStep(nil)
	stop()
	f.onHub(id)
	f.same(0, 1)
}

// A hub that no longer holds the channel it prepared for a move, when the
// commit comes: its database was lost, or put back from a copy older than
// the move. The owner server has committed, both servers are bound to the
// hub, and the hub knows nothing of the channel. The commit carries the
// prepared record the hub itself signed, and the hub prepares the channel
// again from that and activates it (section 21a). Before, the hub answered
// unknown_stream for good, and both servers' events waited unsent with no
// way back.
func TestMoveSurvivesAHubThatLostThePreparedChannel(t *testing.T) {
	f := newMove(t)
	for i := 0; i < 2; i++ {
		f.lk(i).TestStep(func(step string) bool { return step == "activate" })
	}
	id := f.propose(nil)
	f.accept(id)
	// The owner server commits, the other server switches, and neither
	// hands the hub the commit yet.
	f.await(0, id, "committed")
	f.await(1, id, "done")
	if rec, _, _, ok := f.lk(0).TestChannel(model.StreamID(f.stream)); !ok || !rec.Prepared {
		t.Fatal("the hub activated the channel before the test could take it away")
	}
	stop := f.chatter("while the hub has lost the channel")
	// The hub forgets the channel entirely.
	f.Hub.Hub.TestForgetStream(f.stream)
	for _, c := range func() []string {
		var ids []string
		list, _ := f.Hub.Hub.Streams()
		for _, st := range list {
			ids = append(ids, st.ID)
		}
		return ids
	}() {
		if c == f.stream {
			t.Fatal("the hub still holds the stream")
		}
	}
	time.Sleep(300 * time.Millisecond)
	for i := 0; i < 2; i++ {
		f.lk(i).TestStep(nil)
	}
	stop()
	f.onHub(id)
	f.same(0, 1)
	// The channel is the hub's again, with both servers and its moderators,
	// and a third server can be brought in as after any move.
	list, err := f.Hub.Hub.Channels()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range list {
		if c.ID == f.stream {
			found = true
			if len(c.Servers) != 2 || len(c.Managers) == 0 {
				t.Fatalf("the channel as prepared again: %+v", c)
			}
		}
	}
	if !found {
		t.Fatal("the hub does not list the channel after the commit")
	}
}
