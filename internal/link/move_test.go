package link_test

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/link"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Moving a channel that two servers share over a direct link onto a hub
// (rulings R55 and R56 on the shared clock of R57; docs/LINK-PROTOCOL.md
// section 21a). Three servers and a hub in one process: a and b share the
// channel directly, all three are on the hub, and c joins after the move.

type moveFix struct {
	*testkit.Linked
	t      *testing.T
	direct [2]string // the direct link's peer id on a and on b
	stream string
	conv   [3]string
	other  string // a second channel on the direct link, which stays there
	otherB string
	ann    *testkit.Client // a manager of a's copy
	amy    *testkit.Client // a member of a's copy
	bea    *testkit.Client // a manager of b's copy
	bo     *testkit.Client // a member of b's copy

	// hubName is the hub the channel moves onto: the owner server's.
	hubName string

	mu     sync.Mutex
	posted []string
	failed []string
	turn   map[string]*sync.Mutex // by conversation: see say
}

func newMove(t *testing.T) *moveFix {
	t.Helper()
	return newMoveOver(t, testkit.NewLinked(t, 3, true), "hub.localhost")
}

// newMoveOver is newMove over servers already on their hubs: one hub, or the
// hubs of a cluster with the two servers of the direct link on different
// ones.
func newMoveOver(t *testing.T, linked *testkit.Linked, hubName string) *moveFix {
	t.Helper()
	f := &moveFix{Linked: linked, t: t, hubName: hubName}
	a, b := f.Servers[0], f.Servers[1]
	made := a.Owner.Must(201, "POST", "/api/v1/admin/peers/invitations", nil)
	f.direct[0] = made["peer"].(map[string]any)["id"].(string)
	f.direct[1] = b.Owner.Must(201, "POST", "/api/v1/admin/peers", map[string]any{"invitation": made["invitation"]})["peer"].(map[string]any)["id"].(string)
	testkit.Eventually(t, "the direct link is up", func() bool {
		pa := a.Owner.Must(200, "GET", "/api/v1/admin/peers/"+f.direct[0], nil)["peer"].(map[string]any)
		pb := b.Owner.Must(200, "GET", "/api/v1/admin/peers/"+f.direct[1], nil)["peer"].(map[string]any)
		return pa["state"] == "active" && pb["state"] == "active" && pa["connected_at"] != nil && pb["connected_at"] != nil
	})
	f.ann, f.amy = a.Member("ann"), a.Member("amy")
	f.conv[0] = a.Owner.Channel("launch", false, f.ann, f.amy)
	a.Owner.Must(200, "PATCH", "/api/v1/conversations/"+f.conv[0]+"/members/"+string(f.ann.ID), map[string]any{"level": "manager"})
	f.stream, f.conv[1] = f.shareDirect(f.conv[0], "launch")
	f.bea, f.bo = b.Member("bea"), b.Member("bo")
	b.Owner.Add(f.conv[1], "manager", f.bea)
	b.Owner.Add(f.conv[1], "member", f.bo)
	f.other = a.Owner.Channel("stays-direct", false)
	_, f.otherB = f.shareDirect(f.other, "stays-direct")
	testkit.Eventually(t, "a knows b's people", func() bool {
		_, found, _ := a.Server.Store.Members().Get(ctx, model.ConversationID(f.conv[0]), f.bo.ID)
		return found
	})
	testkit.Eventually(t, "b knows a's people", func() bool {
		_, found, _ := b.Server.Store.Members().Get(ctx, model.ConversationID(f.conv[1]), f.amy.ID)
		return found
	})
	return f
}

// shareDirect shares a channel of a's with b over the direct link.
func (f *moveFix) shareDirect(conv, name string) (stream, onB string) {
	f.t.Helper()
	a, b := f.Servers[0], f.Servers[1]
	stream = a.Owner.Must(201, "POST", "/api/v1/admin/streams", map[string]any{"conversation_id": conv, "peer_id": f.direct[0], "history_days": 30})["stream"].(map[string]any)["id"].(string)
	testkit.Eventually(f.t, "the offer reaches b", func() bool { return f.StreamOn(1, stream) != nil })
	onB = b.Owner.Must(200, "POST", "/api/v1/admin/streams/"+stream+"/accept", map[string]any{"name": name})["conversation"].(map[string]any)["id"].(string)
	f.Joined(1, stream)
	return stream, onB
}

func (f *moveFix) lk(i int) *link.Service { return f.Servers[i].Server.Link }

// StreamOf is the stream a conversation of server i is in.
func (f *moveFix) StreamOf(i int, conv string) string {
	f.t.Helper()
	st, err := f.Servers[i].Server.Store.Streams().ByConversation(ctx, model.ConversationID(conv))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(st.ID)
}

func (f *moveFix) propose(body map[string]any) string {
	f.t.Helper()
	req := map[string]any{"peer_id": f.Peers[0]}
	for k, v := range body {
		req[k] = v
	}
	mv := f.Servers[0].Owner.Must(201, "POST", "/api/v1/admin/streams/"+f.stream+"/move", req)["move"].(map[string]any)
	if mv["direction"] != "outgoing" || mv["other_server"] != "b.localhost" || mv["hub_name"] != f.hubName || mv["state"] != "proposed" {
		f.t.Fatalf("the proposal as a shows it: %v", mv)
	}
	return mv["id"].(string)
}

// move is a move as server i's owner is shown it, or nil.
func (f *moveFix) move(i int, id string) map[string]any {
	f.t.Helper()
	for _, raw := range arr(f.Servers[i].Owner.Must(200, "GET", "/api/v1/admin/moves", nil)["moves"]) {
		if mv := raw.(map[string]any); mv["id"] == id {
			return mv
		}
	}
	return nil
}

func (f *moveFix) state(i int, id string) string {
	if mv := f.move(i, id); mv != nil {
		return mv["state"].(string)
	}
	return ""
}

// moveWait is how long a move is given to reach a state. A move that goes
// through while both servers go on posting is a race between how fast one
// server applies the other's backlog and how fast the backlog grows; under
// the race detector, where the database is many times slower, ten seconds
// was not always enough for it, though the move was never stuck.
const moveWait = 40 * time.Second

func (f *moveFix) await(i int, id, state string) {
	f.t.Helper()
	began := time.Now()
	for f.state(i, id) != state {
		if time.Since(began) > moveWait {
			f.t.Fatalf("timed out waiting until the move is %s on %s", state, testkit.ServerName(i))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if took := time.Since(began); took > 10*time.Second {
		f.t.Logf("the move took %v to be %s on %s", took.Round(time.Millisecond), state, testkit.ServerName(i))
	}
}

func (f *moveFix) accept(id string) {
	f.t.Helper()
	f.await(1, id, "proposed")
	mv := f.Servers[1].Owner.Must(200, "POST", "/api/v1/admin/moves/"+id+"/accept", nil)["move"].(map[string]any)
	if mv["direction"] != "incoming" || mv["state"] != "accepted" || mv["hub_peer_id"] != f.Peers[1] {
		f.t.Fatalf("the acceptance as b shows it: %v", mv)
	}
}

// say posts as a client and remembers the text; a refusal is remembered
// too, for a test that says messages are never refused.
//
// One thing is said at a time on each server. The chatter and the test
// itself both post there, and two requests in flight together have no
// order they were said in: the server may stamp the one whose answer comes
// back second first, and "posted" would then hold them the other way round.
func (f *moveFix) say(c *testkit.Client, conv, text string) {
	f.mu.Lock()
	if f.turn == nil {
		f.turn = map[string]*sync.Mutex{}
	}
	turn := f.turn[conv]
	if turn == nil {
		turn = &sync.Mutex{}
		f.turn[conv] = turn
	}
	f.mu.Unlock()
	turn.Lock()
	defer turn.Unlock()
	status, out, err := c.Try("POST", "/api/v1/conversations/"+conv+"/messages", map[string]any{"text": text})
	if err != nil {
		return // no answer at all: the server is restarting. Not a refusal.
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if status != 201 {
		f.failed = append(f.failed, fmt.Sprintf("%q: %d %v", text, status, out))
		return
	}
	f.posted = append(f.posted, text)
}

// chatter posts from both servers until the function it returns is called:
// messages flowing throughout.
func (f *moveFix) chatter(tag string) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i, c := range []*testkit.Client{f.amy, f.bo} {
		wg.Add(1)
		go func(i int, c *testkit.Client) {
			defer wg.Done()
			for n := 0; ; n++ {
				select {
				case <-done:
					return
				default:
				}
				f.say(c, f.conv[i], fmt.Sprintf("%s %s %03d", tag, testkit.ServerName(i)[:1], n))
				time.Sleep(15 * time.Millisecond)
			}
		}(i, c)
	}
	return func() {
		close(done)
		wg.Wait()
	}
}

// onHub checks that the move happened, the same on both servers and the
// hub: one stream, one channel each, the hub its owner, relay and time
// reference.
func (f *moveFix) onHub(id string) {
	f.t.Helper()
	for i := 0; i < 2; i++ {
		f.await(i, id, "done")
		st := f.StreamOn(i, f.stream)
		if st["peer_id"] != f.Peers[i] || st["reference"] != "hub" || st["owner_server"] != f.hubName || st["state"] != "active" ||
			st["conversation_id"] != f.conv[i] {
			f.t.Fatalf("%s's stream after the move: %v", testkit.ServerName(i), st)
		}
		n := 0
		all, _ := f.Servers[i].Server.Store.Streams().List(ctx, "")
		for _, s := range all {
			if string(s.ID) == f.stream || string(s.ConversationID) == f.conv[i] {
				n++
			}
		}
		if n != 1 {
			f.t.Fatalf("%s holds %d streams for the channel", testkit.ServerName(i), n)
		}
	}
	list, _ := f.Hub.Hub.Channels()
	found := false
	for _, c := range list {
		if c.ID == f.stream {
			found = true
			if strings.Join(c.Servers, ",") != "a.localhost,b.localhost" || c.KeyMaker != "a.localhost" || c.Name != "launch" || c.Listing != wire.ListingUnlisted {
				f.t.Fatalf("the hub's record: %+v", c)
			}
		}
	}
	if !found {
		f.t.Fatalf("the hub does not hold the channel: %+v", list)
	}
}

// onDirect checks that nothing happened, the same on both servers and the
// hub: the stream is where it was.
func (f *moveFix) onDirect(id, reason string) {
	f.t.Helper()
	for i := 0; i < 2; i++ {
		f.await(i, id, "abandoned")
		if mv := f.move(i, id); reason != "" && !strings.Contains(fmt.Sprint(mv["reason"]), reason) {
			f.t.Fatalf("%s gives the reason %q, not %q", testkit.ServerName(i), mv["reason"], reason)
		}
		st := f.StreamOn(i, f.stream)
		if st["peer_id"] != f.direct[i] || st["state"] != "active" || st["conversation_id"] != f.conv[i] {
			f.t.Fatalf("%s's stream after an abandoned move: %v", testkit.ServerName(i), st)
		}
	}
	if st := f.StreamOn(0, f.stream); st["reference"] != "self" || st["owner_server"] != "a.localhost" {
		f.t.Fatalf("a no longer owns the stream: %v", st)
	}
	if st := f.StreamOn(1, f.stream); st["reference"] != "owner" || st["owner_server"] != "a.localhost" {
		f.t.Fatalf("b's stream is no longer a's: %v", st)
	}
	testkit.Eventually(f.t, "the hub holds nothing of the channel", func() bool {
		list, _ := f.Hub.Hub.Channels()
		for _, c := range list {
			if c.ID == f.stream {
				return false
			}
		}
		streams, _ := f.Hub.Hub.Streams()
		for _, s := range streams {
			if s.ID == f.stream {
				return false
			}
		}
		return true
	})
}

// same waits until both servers (and c, once it has joined) hold every text
// that was posted, then checks that each is there exactly once, under one
// id and one order string, and that the texts stand in the same order
// everywhere: nothing lost, duplicated or reordered.
func (f *moveFix) same(servers ...int) {
	f.t.Helper()
	f.mu.Lock()
	want := append([]string(nil), f.posted...)
	failed := append([]string(nil), f.failed...)
	f.mu.Unlock()
	if len(failed) > 0 {
		f.t.Fatalf("%d messages were refused, the first: %s", len(failed), failed[0])
	}
	if len(servers) == 0 {
		servers = []int{0, 1}
	}
	type seen struct{ id, order string }
	views := map[int]map[string][]seen{}
	read := func(i int) map[string][]seen {
		out := map[string][]seen{}
		before := int64(0)
		for {
			msgs, _, err := f.Servers[i].Server.Store.Messages().List(ctx, model.ConversationID(f.conv[i]), store.MessageList{SeqPage: store.SeqPage{Before: before, Limit: 100}})
			if err != nil {
				f.t.Fatal(err)
			}
			if len(msgs) == 0 {
				return out
			}
			for _, m := range msgs {
				out[m.Text] = append(out[m.Text], seen{string(m.ID), m.Order})
				if before == 0 || m.Seq < before {
					before = m.Seq
				}
			}
		}
	}
	testkit.Eventually(f.t, fmt.Sprintf("all %d messages are on every server", len(want)), func() bool {
		for _, i := range servers {
			views[i] = read(i)
			for _, text := range want {
				if len(views[i][text]) == 0 {
					return false
				}
			}
		}
		return true
	})
	// Unsent is over everywhere: the reference has everything.
	for _, i := range servers {
		testkit.Eventually(f.t, "nothing waits on "+testkit.ServerName(i), func() bool { return f.lk(i).TestOutbox(model.StreamID(f.stream)) == 0 })
		views[i] = read(i)
	}
	first := servers[0]
	for _, text := range want {
		for _, i := range servers {
			got := views[i][text]
			if len(got) != 1 {
				f.t.Fatalf("%s holds %q %d times", testkit.ServerName(i), text, len(got))
			}
			if got[0] != views[first][text][0] {
				f.t.Fatalf("%q is %v on %s and %v on %s", text, got[0], testkit.ServerName(i), views[first][text][0], testkit.ServerName(first))
			}
		}
	}
	// Each author's own messages keep the order they were said in.
	for _, who := range []string{" a ", " b "} {
		var mine []string
		for _, text := range want {
			if strings.Contains(text, who) {
				mine = append(mine, text)
			}
		}
		byOrder := append([]string(nil), mine...)
		sort.SliceStable(byOrder, func(x, y int) bool { return views[first][byOrder[x]][0].order < views[first][byOrder[y]][0].order })
		if strings.Join(byOrder, "|") != strings.Join(mine, "|") {
			for k := range mine {
				if mine[k] != byOrder[k] {
					f.t.Logf("said %q (%s) where the order has %q (%s)", mine[k], views[first][mine[k]][0].order, byOrder[k], views[first][byOrder[k]][0].order)
				}
			}
			f.t.Fatalf("the messages of%sare not in the order they were said", who)
		}
	}
}

// The happy path, with messages flowing throughout, the moderators the move
// leaves, the direct link's other stream untouched, and a third server
// joining afterwards.
func TestMoveOntoHub(t *testing.T) {
	f := newMove(t)
	a, b, c := f.Servers[0], f.Servers[1], f.Servers[2]
	f.say(f.amy, f.conv[0], "before a 000")
	f.say(f.bo, f.conv[1], "before b 000")
	epoch := f.StreamOn(0, f.stream)["key_epoch"].(float64)
	stop := f.chatter("during")
	id := f.propose(nil)
	time.Sleep(60 * time.Millisecond)
	// Nothing changes until the other server's owner accepts.
	if st := f.StreamOn(0, f.stream); st["peer_id"] != f.direct[0] || f.state(0, id) != "proposed" {
		t.Fatalf("something changed before the acceptance: %v", st)
	}
	f.accept(id)
	f.onHub(id)
	time.Sleep(100 * time.Millisecond)
	stop()
	f.say(f.amy, f.conv[0], "after a 000")
	f.say(f.bo, f.conv[1], "after b 000")
	f.same()
	if len(f.posted) < 10 {
		t.Fatalf("only %d messages were posted while the move ran", len(f.posted))
	}

	// A new key epoch, made by the former owner server as the hub's first
	// key-maker; the hub activated the channel.
	testkit.Eventually(t, "a new key epoch on both", func() bool {
		return f.StreamOn(0, f.stream)["key_epoch"].(float64) == epoch+1 && f.StreamOn(1, f.stream)["key_epoch"].(float64) == epoch+1
	})
	// The moderators are every manager of the channel on either server at
	// the commit: both owners (admins who are members), ann and bea; not
	// amy or bo, who are plain members.
	for i, ts := range f.Servers[:2] {
		var got []string
		testkit.Eventually(t, "the activated record reaches "+testkit.ServerName(i), func() bool {
			got = nil
			for _, raw := range arr(hubChannel(ts.Owner, f.stream)["managers"]) {
				m := raw.(map[string]any)
				got = append(got, m["username"].(string)+"@"+m["server"].(string))
			}
			sort.Strings(got)
			return len(got) == 4
		})
		if strings.Join(got, ",") != "ann@a.localhost,bea@b.localhost,owner@a.localhost,owner@b.localhost" {
			t.Fatalf("the moderators as %s holds them: %v", testkit.ServerName(i), got)
		}
	}
	// The former owner server keeps no special role: its owner acts as a
	// moderator like the others, and owner-server operations are over.
	a.Owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/streams/"+f.stream+"/owner", map[string]any{"server_id": f.ServerID(0, 1)})
	a.Owner.Refused(403, model.CodeForbidden, "PATCH", "/api/v1/admin/streams/"+f.stream, map[string]any{"max_blob_bytes": 4096})
	f.amy.Refused(403, model.CodeForbidden, "POST", "/api/v1/streams/"+f.stream+"/channel/actions", map[string]any{"action": "close"})
	// A manager of b's copy, now a moderator, removes a message of a's for
	// everyone.
	doomed := f.amy.Post(f.conv[0], "amy's, removed by bea")
	f.bo.HasMessage(f.conv[1], "amy's, removed by bea")
	f.bea.Must(200, "DELETE", "/api/v1/messages/"+doomed["id"].(string), nil)
	testkit.Eventually(t, "bea's removal reaches a", func() bool {
		m, err := a.Server.Store.Messages().Get(ctx, model.MessageID(doomed["id"].(string)))
		return err == nil && m.Deleted() && m.DeletedByID == f.bea.ID
	})

	// The direct link remains, with its other stream on it.
	a.Owner.Post(f.other, "still direct")
	b.Owner.HasMessage(f.otherB, "still direct")
	if st := f.StreamOn(0, f.StreamOf(0, f.other)); st["peer_id"] != f.direct[0] || st["reference"] != "self" {
		t.Fatalf("the direct link's other stream moved too: %v", st)
	}
	// It can be removed now: the move is over.
	if st, out := a.Owner.Do("POST", "/api/v1/admin/streams/"+f.StreamOf(0, f.other)+"/close", nil); st != 204 {
		t.Fatalf("closing the other stream: %d %v", st, out)
	}

	// A third server joins through the hub, in the ordinary way, invited by
	// a moderator from the server that was never the owner.
	moderate(f.bea, f.stream, map[string]any{"action": "invite", "server": "c.localhost"})
	f.conv[2] = f.Accept(2, f.stream, "launch-c")
	testkit.Eventually(t, "c is in", func() bool {
		st := f.StreamOn(2, f.stream)
		return st != nil && st["state"] == "active" && st["key_epoch"].(float64) == epoch+2
	})
	if rec := hubChannel(a.Owner, f.stream); names(rec["servers"]) != "a.localhost,b.localhost,c.localhost" || rec["key_maker"] != "b.localhost" {
		t.Fatalf("after c joined: %v", rec)
	}
	f.say(f.amy, f.conv[0], "three a 000")
	f.say(f.bo, f.conv[1], "three b 000")
	c.Owner.HasMessage(f.conv[2], "three a 000")
	c.Owner.Post(f.conv[2], "c is here")
	f.amy.HasMessage(f.conv[0], "c is here")
	f.bo.HasMessage(f.conv[1], "c is here")
	// c has what the channel held before it joined, from the direct days
	// too, as history; and everything since, in the same order.
	f.same(0, 1, 2)
	// Nothing of the move was rejected anywhere. (An envelope that reaches
	// a server twice, once as it is delivered and once in answer to a
	// catch-up, is dropped the second time and counted as a duplicate: that
	// is the protocol working, and the checks above have shown each message
	// is held once.)
	for i := range f.Servers {
		r := f.lk(i).StatsNow().Rejected
		delete(r, "duplicate")
		if len(r) != 0 {
			t.Fatalf("%s rejected something: %v", testkit.ServerName(i), r)
		}
	}
	_ = b
}

// What is refused, by whom, and when: who may propose and answer, and what
// both servers hold still while a move is in progress. Messages are never
// among it.
func TestMoveRefusals(t *testing.T) {
	f := newMove(t)
	a, b := f.Servers[0], f.Servers[1]
	move := "/api/v1/admin/streams/" + f.stream + "/move"
	// Only the owner server proposes, only for a channel on a direct link,
	// only onto a hub both servers are on.
	b.Owner.Refused(403, model.CodeForbidden, "POST", move, map[string]any{"peer_id": f.Peers[1]})
	f.ann.Refused(403, model.CodeForbidden, "POST", move, map[string]any{"peer_id": f.Peers[0]})
	a.Owner.Refused(400, model.CodeInvalidValue, "POST", move, map[string]any{"peer_id": f.direct[0]})
	a.Owner.Refused(400, model.CodeInvalidValue, "POST", move, map[string]any{"peer_id": f.Peers[0], "expires_in_seconds": 5})
	onHub := a.Owner.Channel("already-there", false)
	hubStream, _ := f.Share(0, onHub, "already-there", nil)
	if st, out := a.Owner.Do("POST", "/api/v1/admin/streams/"+hubStream+"/move", map[string]any{"peer_id": f.Peers[0]}); st != 403 && st != 409 {
		t.Fatalf("moving a channel that is on a hub already: %d %v", st, out)
	}

	// Proposed. On the server that proposed, what would pull the ground
	// from under the move is refused from here on.
	id := f.propose(nil)
	f.await(1, id, "proposed")
	a.Owner.Refused(409, model.CodeConflict, "POST", move, map[string]any{"peer_id": f.Peers[0]})
	a.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/streams/"+f.stream+"/owner", map[string]any{"server_id": f.ServerID(0, 1)})
	a.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/streams/"+f.stream+"/close", nil)
	a.Owner.Refused(409, model.CodeConflict, "DELETE", "/api/v1/admin/streams/"+f.stream+"/servers/"+f.ServerID(0, 1), nil)
	a.Owner.Refused(409, model.CodeConflict, "DELETE", "/api/v1/admin/peers/"+f.direct[0], nil)
	a.Owner.Refused(409, model.CodeConflict, "DELETE", "/api/v1/admin/peers/"+f.Peers[0], nil)
	// Not the server that has not answered yet; and only its owner answers.
	a.Owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/moves/"+id+"/accept", nil)
	f.bea.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/moves/"+id+"/accept", nil)
	b.Owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/moves/"+id+"/cancel", nil)
	b.Owner.Must(200, "PATCH", "/api/v1/conversations/"+f.conv[1]+"/members/"+string(f.bo.ID), map[string]any{"level": "manager"})
	b.Owner.Must(200, "PATCH", "/api/v1/conversations/"+f.conv[1]+"/members/"+string(f.bo.ID), map[string]any{"level": "member"})
	// Declined: nothing changed, and everything is allowed again.
	b.Owner.Must(200, "POST", "/api/v1/admin/moves/"+id+"/decline", nil)
	f.await(0, id, "declined")
	b.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/moves/"+id+"/accept", nil)
	if st := f.StreamOn(0, f.stream); st["peer_id"] != f.direct[0] {
		t.Fatalf("a declined move changed the stream: %v", st)
	}

	// Accepted, and held there: a stops before it asks the hub.
	f.lk(0).TestStep(func(step string) bool { return step == "prepare" })
	id = f.propose(nil)
	f.accept(id)
	f.await(0, id, "accepted")
	b.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/moves/"+id+"/accept", nil)
	b.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/moves/"+id+"/decline", nil)
	b.Owner.Refused(409, model.CodeConflict, "DELETE", "/api/v1/admin/streams/"+f.stream, nil)
	b.Owner.Refused(409, model.CodeConflict, "DELETE", "/api/v1/admin/peers/"+f.direct[1], nil)
	b.Owner.Refused(409, model.CodeConflict, "DELETE", "/api/v1/admin/peers/"+f.Peers[1], nil)
	// Who is a manager of the channel on the server that agreed does not
	// change until the move is over: they are the moderators it signed for.
	b.Owner.Refused(409, model.CodeConflict, "PATCH", "/api/v1/conversations/"+f.conv[1]+"/members/"+string(f.bo.ID), map[string]any{"level": "manager"})
	b.Owner.Refused(409, model.CodeConflict, "PATCH", "/api/v1/conversations/"+f.conv[1]+"/members/"+string(f.bea.ID), map[string]any{"level": "member"})
	f.bea.Refused(409, model.CodeConflict, "POST", "/api/v1/conversations/"+f.conv[1]+"/leave", nil)
	// A member who is not a manager may leave, and messages are never
	// refused, on either server.
	bob := b.Member("bob")
	bob.Must(200, "POST", "/api/v1/conversations/"+f.conv[1]+"/join", nil)
	bob.Must(204, "POST", "/api/v1/conversations/"+f.conv[1]+"/leave", nil)
	f.say(f.amy, f.conv[0], "mid a 000")
	f.say(f.bo, f.conv[1], "mid b 000")
	f.same()

	// The proposer withdraws: nothing changed, on either server or the hub,
	// and what was refused is allowed again.
	a.Owner.Must(200, "POST", "/api/v1/admin/moves/"+id+"/cancel", nil)
	f.lk(0).TestStep(nil)
	f.onDirect(id, "cancelled")
	a.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/moves/"+id+"/cancel", nil)
	b.Owner.Must(200, "PATCH", "/api/v1/conversations/"+f.conv[1]+"/members/"+string(f.bo.ID), map[string]any{"level": "manager"})
	f.say(f.amy, f.conv[0], "mid a 001")
	f.say(f.bo, f.conv[1], "mid b 001")
	f.same()
	// And a new move goes through, with bo among the moderators now.
	id = f.propose(nil)
	f.accept(id)
	f.onHub(id)
	testkit.Eventually(t, "bo is a moderator", func() bool {
		return f.bo.Must(200, "GET", "/api/v1/streams/"+f.stream+"/channel", nil)["manager"] == true
	})
	// A move that is over cannot be withdrawn, and the channel cannot be
	// moved again.
	a.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/moves/"+id+"/cancel", nil)
	if st, out := a.Owner.Do("POST", move, map[string]any{"peer_id": f.Peers[0]}); st != 403 && st != 409 {
		t.Fatalf("moving a moved channel: %d %v", st, out)
	}
}

// A proposal expires, before or after it was accepted, and then nothing has
// changed.
func TestMoveExpires(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		t.Run(fmt.Sprintf("accepted=%v", accepted), func(t *testing.T) {
			f := newMove(t)
			f.say(f.amy, f.conv[0], "early a 000")
			var id string
			if accepted {
				// Accepted, and a gets no further: it stops before it asks
				// the hub, as it would if the hub could not be reached.
				f.lk(0).TestStep(func(step string) bool { return step == "prepare" })
				id = f.propose(map[string]any{"expires_in_seconds": 60})
				f.accept(id)
				f.await(0, id, "accepted")
			} else {
				id = f.propose(map[string]any{"expires_in_seconds": 60})
				f.await(1, id, "proposed")
			}
			// A minute and more passes on both servers.
			f.Skew(0, 90*time.Second)
			f.Skew(1, 90*time.Second)
			f.onDirect(id, "expired")
			f.Skew(0, 0)
			f.Skew(1, 0)
			f.lk(0).TestStep(nil)
			f.Servers[1].Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/moves/"+id+"/accept", nil)
			// The stream carries on directly as before.
			testkit.Eventually(t, "the clocks have been measured again", func() bool {
				status, _ := f.amy.Do("POST", "/api/v1/conversations/"+f.conv[0]+"/typing", map[string]any{})
				c, ok := f.lk(1).TestTold(model.PeerID(f.direct[1]))
				return status == 204 && ok && c.Offset > -1000 && c.Offset < 1000
			})
			f.say(f.amy, f.conv[0], "late a 000")
			f.say(f.bo, f.conv[1], "late b 000")
			f.same()
		})
	}
}

// Every failure at every step, including a restart of either server before
// and after the commit point. Whatever fails, both servers and the hub end
// the same way: the channel wholly on the hub, or wholly where it was; one
// stream and one channel on each server; and every message that was posted
// there once, in one order.
func TestMoveSurvivesFailures(t *testing.T) {
	type step struct {
		name string
		run  func(f *moveFix) (id string, hub bool, reason string)
	}
	paused := func(f *moveFix, i int, peer string) func() {
		f.t.Helper()
		path := "/api/v1/admin/peers/" + peer
		f.Servers[i].Owner.Must(200, "PATCH", path, map[string]any{"state": "paused"})
		testkit.Eventually(f.t, "the link is cut", func() bool { return !f.lk(i).TestConnected(model.PeerID(peer)) })
		return func() {
			f.t.Helper()
			f.Servers[i].Owner.Must(200, "PATCH", path, map[string]any{"state": "active"})
			testkit.Eventually(f.t, "the link is back", func() bool { return f.lk(i).TestConnected(model.PeerID(peer)) })
		}
	}
	stopAt := func(f *moveFix, i int, name string) {
		f.lk(i).TestStep(func(step string) bool { return step == name })
	}
	steps := []step{
		{"the proposal is lost, and sent again", func(f *moveFix) (string, bool, string) {
			back := paused(f, 1, f.direct[1])
			id := f.propose(nil)
			time.Sleep(100 * time.Millisecond)
			if f.move(1, id) != nil {
				f.t.Fatal("b has the proposal though the link was cut")
			}
			back()
			f.accept(id)
			return id, true, ""
		}},
		{"the acceptance is lost, and sent again", func(f *moveFix) (string, bool, string) {
			id := f.propose(nil)
			f.await(1, id, "proposed")
			back := paused(f, 0, f.direct[0])
			f.accept(id)
			time.Sleep(100 * time.Millisecond)
			if f.state(0, id) != "proposed" {
				f.t.Fatal("a has the acceptance though the link was cut")
			}
			back()
			return id, true, ""
		}},
		{"the hub refuses to prepare the channel", func(f *moveFix) (string, bool, string) {
			stopAt(f, 0, "prepare")
			id := f.propose(nil)
			f.accept(id)
			f.await(0, id, "accepted")
			// The hub has room for no more channels of a's: it holds one,
			// and one is all a is allowed from now on.
			f.Share(0, f.Servers[0].Owner.Channel("fills-the-quota", false), "quota", nil)
			if err := f.Hub.Hub.SetLimits("a.localhost", 0, 0, 1); err != nil {
				f.t.Fatal(err)
			}
			f.lk(0).TestStep(nil)
			return id, false, "the hub refused"
		}},
		{"the hub cannot be reached when the owner server is to prepare, and returns", func(f *moveFix) (string, bool, string) {
			stopAt(f, 0, "prepare")
			id := f.propose(nil)
			f.accept(id)
			f.await(0, id, "accepted")
			back := paused(f, 0, f.Peers[0])
			f.lk(0).TestStep(nil)
			time.Sleep(1500 * time.Millisecond) // a tries, and cannot
			if f.state(0, id) != "accepted" || f.StreamOn(0, f.stream)["peer_id"] != f.direct[0] {
				f.t.Fatal("something happened to the move while the hub could not be reached")
			}
			back()
			return id, true, ""
		}},
		{"the hub is not handed the commit for a while, by either server", func(f *moveFix) (string, bool, string) {
			stopAt(f, 0, "activate")
			stopAt(f, 1, "activate")
			id := f.propose(nil)
			f.accept(id)
			f.await(1, id, "done") // b has switched
			// Committed on both servers, and the hub still only holds the
			// channel prepared: it carries nothing. What is said waits.
			f.say(f.amy, f.conv[0], "waiting a 000")
			f.say(f.bo, f.conv[1], "waiting b 000")
			time.Sleep(300 * time.Millisecond)
			if rec, _, _, ok := f.lk(0).TestChannel(model.StreamID(f.stream)); !ok || !rec.Prepared {
				f.t.Fatalf("the hub activated the channel though nobody handed it the commit: %+v", rec)
			}
			if m := f.bo.HasMessage(f.conv[1], "waiting b 000"); m["unsent"] != true {
				f.t.Fatalf("b's message is not shown as unsent while the hub has not the commit: %v", m)
			}
			absent(f.t, f.amy, f.conv[0], "waiting b 000")
			f.lk(0).TestStep(nil)
			f.lk(1).TestStep(nil)
			return id, true, ""
		}},
		{"the owner server restarts before it asks the hub", func(f *moveFix) (string, bool, string) {
			stopAt(f, 0, "prepare")
			id := f.propose(nil)
			f.accept(id)
			f.await(0, id, "accepted")
			f.Servers[0].Restart()
			return id, true, ""
		}},
		{"the owner server restarts after the hub prepared and before the commit", func(f *moveFix) (string, bool, string) {
			stopAt(f, 0, "commit")
			id := f.propose(nil)
			f.accept(id)
			testkit.Eventually(f.t, "the hub has prepared the channel", func() bool {
				rec, _, _, ok := f.lk(0).TestChannel(model.StreamID(f.stream))
				return ok && rec.Prepared
			})
			if f.state(0, id) != "accepted" || f.StreamOn(0, f.stream)["peer_id"] != f.direct[0] {
				f.t.Fatal("a committed though it was stopped before the commit")
			}
			f.Servers[0].Restart()
			return id, true, ""
		}},
		{"the owner server withdraws after the hub prepared", func(f *moveFix) (string, bool, string) {
			stopAt(f, 0, "commit")
			id := f.propose(nil)
			f.accept(id)
			testkit.Eventually(f.t, "the hub has prepared the channel", func() bool {
				rec, _, _, ok := f.lk(0).TestChannel(model.StreamID(f.stream))
				return ok && rec.Prepared
			})
			f.Servers[0].Owner.Must(200, "POST", "/api/v1/admin/moves/"+id+"/cancel", nil)
			f.lk(0).TestStep(nil)
			return id, false, "cancelled"
		}},
		{"the owner server restarts right after the commit", func(f *moveFix) (string, bool, string) {
			f.lk(0).TestStep(func(step string) bool { return step == "deliver" || step == "activate" })
			id := f.propose(nil)
			f.accept(id)
			// Committed, and not a word to anyone: a neither sends b
			// stream.moved nor hands the hub the commit. (b may find out
			// all the same, by asking, when a refuses its next event.)
			testkit.Eventually(f.t, "a has committed", func() bool { s := f.state(0, id); return s == "committed" || s == "done" })
			f.Servers[0].Restart()
			return id, true, ""
		}},
		{"the owner server commits and never tells the hub: the other server does", func(f *moveFix) (string, bool, string) {
			stopAt(f, 0, "activate")
			id := f.propose(nil)
			f.accept(id)
			return id, true, ""
		}},
		{"the other server is cut off through the commit, on both links", func(f *moveFix) (string, bool, string) {
			stopAt(f, 0, "commit")
			id := f.propose(nil)
			f.accept(id)
			testkit.Eventually(f.t, "the hub has prepared the channel", func() bool {
				rec, _, _, ok := f.lk(0).TestChannel(model.StreamID(f.stream))
				return ok && rec.Prepared
			})
			direct, hub := paused(f, 1, f.direct[1]), paused(f, 1, f.Peers[1])
			f.lk(0).TestStep(nil)
			f.await(0, id, "committed")
			// The channel lives on the hub for a already; b says things it
			// cannot send yet.
			f.say(f.amy, f.conv[0], "apart a 000")
			f.say(f.bo, f.conv[1], "apart b 000")
			f.say(f.amy, f.conv[0], "apart a 001")
			if f.state(1, id) != "accepted" {
				f.t.Fatal("b learned of the commit while cut off")
			}
			hub()
			time.Sleep(150 * time.Millisecond)
			// The hub alone does not make b switch: stream.moved does, and
			// that is in the direct log.
			if f.StreamOn(1, f.stream)["peer_id"] != f.direct[1] {
				f.t.Fatal("b switched without stream.moved")
			}
			direct()
			return id, true, ""
		}},
		{"the other server fails at the switch, and restarts", func(f *moveFix) (string, bool, string) {
			stopAt(f, 1, "switch")
			id := f.propose(nil)
			f.accept(id)
			f.await(0, id, "committed")
			time.Sleep(300 * time.Millisecond)
			if f.state(1, id) != "accepted" || f.StreamOn(1, f.stream)["peer_id"] != f.direct[1] {
				f.t.Fatal("b switched though the switch was made to fail")
			}
			f.say(f.bo, f.conv[1], "stuck b 000")
			f.Servers[1].Restart()
			return id, true, ""
		}},
		{"the other server restarts before the commit", func(f *moveFix) (string, bool, string) {
			stopAt(f, 0, "commit")
			id := f.propose(nil)
			f.accept(id)
			f.await(0, id, "accepted")
			f.Servers[1].Restart()
			f.lk(0).TestStep(nil)
			return id, true, ""
		}},
		{"the other server restarts after the commit and before it switched", func(f *moveFix) (string, bool, string) {
			stopAt(f, 1, "switch")
			id := f.propose(nil)
			f.accept(id)
			f.await(0, id, "committed")
			f.Servers[1].Restart()
			return id, true, ""
		}},
	}
	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			f := newMove(t)
			f.t = t
			defer f.diagnose()
			f.say(f.amy, f.conv[0], "first a 000")
			f.say(f.bo, f.conv[1], "first b 000")
			stop := f.chatter("moving")
			id, hub, reason := st.run(f)
			if hub {
				f.onHub(id)
			} else {
				f.onDirect(id, reason)
			}
			time.Sleep(80 * time.Millisecond)
			stop()
			f.say(f.amy, f.conv[0], "last a 000")
			f.say(f.bo, f.conv[1], "last b 000")
			f.same()
			// One outcome, the same on both servers.
			if (f.StreamOn(0, f.stream)["reference"] == "hub") != (f.StreamOn(1, f.stream)["reference"] == "hub") {
				t.Fatal("the two servers ended differently")
			}
		})
	}
}

// diagnose says, when a test has failed, where each server stands.
func (f *moveFix) diagnose() {
	if !f.t.Failed() {
		return
	}
	sid := model.StreamID(f.stream)
	for i := 0; i < 2; i++ {
		st, _ := f.lk(i).TestStream(sid)
		stats := f.lk(i).StatsNow()
		last := f.lk(i).TestLastError()
		f.t.Logf("%s: peer is hub %v, sequencer %s, epoch %d, outbox %d, have %v, rejected %v, refused %v, dropped %v, restamped %d, gaps %d, last error %s %q",
			testkit.ServerName(i), string(st.PeerID) == f.Peers[i], st.Sequencer, st.KeyEpoch, f.lk(i).TestOutbox(sid), f.lk(i).TestHave(sid),
			stats.Rejected, stats.Refused, stats.Dropped, stats.Restamped, stats.Gaps, last.Code, last.Message)
		held := map[string]bool{}
		before := int64(0)
		for {
			msgs, _, err := f.Servers[i].Server.Store.Messages().List(ctx, model.ConversationID(f.conv[i]), store.MessageList{SeqPage: store.SeqPage{Before: before, Limit: 100}})
			if err != nil || len(msgs) == 0 {
				break
			}
			for _, m := range msgs {
				held[m.Text] = true
				if before == 0 || m.Seq < before {
					before = m.Seq
				}
			}
		}
		var missing []string
		f.mu.Lock()
		for _, text := range f.posted {
			if !held[text] {
				missing = append(missing, text)
			}
		}
		f.mu.Unlock()
		if len(missing) > 8 {
			missing = append(missing[:8], fmt.Sprintf("and %d more", len(missing)-8))
		}
		f.t.Logf("%s lacks %v", testkit.ServerName(i), missing)
		for _, mv := range arr(f.Servers[i].Owner.Must(200, "GET", "/api/v1/admin/moves", nil)["moves"]) {
			f.t.Logf("%s: move %v", testkit.ServerName(i), mv)
		}
	}
	if rec, _, _, ok := f.lk(0).TestChannel(sid); ok {
		f.t.Logf("a's record: v%d prepared %v epoch %d maker %s", rec.Version, rec.Prepared, rec.Epoch, rec.KeyMaker)
	}
	f.t.Logf("the hub's log holds %d envelopes", len(f.Hub.Hub.TestLog(f.stream)))
}

// What the owner server took of the other server's events before the commit
// is not sent again after it, even when the other server never heard that
// it was taken. If it were, it would reach the hub with a new stamp and
// stand in one place on the owner server and in another everywhere else.
func TestMoveDoesNotResendWhatTheOwnerTook(t *testing.T) {
	f := newMove(t)
	a, b := f.Servers[0], f.Servers[1]
	sid := model.StreamID(f.stream)
	f.say(f.bo, f.conv[1], "heard b 000")
	f.same()
	// b stops hearing a's answers. a takes what b says; b goes on thinking
	// it unsent.
	f.lk(1).TestDeaf(true)
	f.say(f.bo, f.conv[1], "unheard b 001")
	f.say(f.bo, f.conv[1], "unheard b 002")
	taken := f.amy.HasMessage(f.conv[0], "unheard b 002")
	f.amy.HasMessage(f.conv[0], "unheard b 001")
	if n := f.lk(1).TestOutbox(sid); n != 2 {
		t.Fatalf("b holds %d events it thinks unsent, want 2", n)
	}
	if m := f.bo.HasMessage(f.conv[1], "unheard b 002"); m["unsent"] != true {
		t.Fatalf("b does not show the message as unsent: %v", m)
	}
	id := f.propose(nil)
	f.accept(id)
	f.onHub(id)
	f.lk(1).TestDeaf(false)
	f.say(f.amy, f.conv[0], "later a 000")
	f.say(f.bo, f.conv[1], "later b 003")
	f.same()
	// It stands where it stood, on both, and is no longer unsent on b.
	onA, onB := f.amy.HasMessage(f.conv[0], "unheard b 002"), f.bo.HasMessage(f.conv[1], "unheard b 002")
	if onA["order"] != taken["order"] || onB["order"] != taken["order"] || onB["unsent"] == true {
		t.Fatalf("the message moved or is still unsent: was %v, a %v, b %v unsent %v", taken["order"], onA["order"], onB["order"], onB["unsent"])
	}
	// And the hub was never sent it.
	for _, env := range f.Hub.Hub.TestLog(f.stream) {
		if env.OriginEventID == taken["id"] {
			t.Fatal("b sent the hub an event the owner server had taken")
		}
	}
	if r := f.lk(1).StatsNow().Restamped; r != 0 {
		t.Fatalf("b stamped %d events again", r)
	}
	_, _ = a, b
}

// The hub prepares a moved channel only on the other server's own signed
// agreement to that move, of that stream, onto that hub.
func TestHubChecksTheAgreementToAMove(t *testing.T) {
	f := newMove(t)
	a, b := f.Servers[0], f.Servers[1]
	// A real agreement, held where a has not yet asked the hub.
	f.lk(0).TestStep(func(step string) bool { return step == "prepare" })
	id := f.propose(nil)
	f.accept(id)
	f.await(0, id, "accepted")
	mv, err := a.Server.Store.Streams().Move(ctx, id)
	if err != nil || len(mv.Accept) == 0 {
		t.Fatalf("a holds no agreement: %v", err)
	}
	var agreed wire.MoveAccept
	if err := wire.PeekBytes(wire.BMoveAccept, mv.Accept, &agreed); err != nil || len(agreed.Managers) != 2 {
		t.Fatalf("b signed for %d managers: %v", len(agreed.Managers), err)
	}
	creator := wire.ChannelAccount{AccountID: string(a.Owner.ID), Server: "a.localhost", Username: "owner"}
	// What a would rightly send: every case below changes one thing in it.
	base := wire.ChannelCreate{StreamID: f.stream, Name: "launch", Listing: wire.ListingUnlisted, Creator: creator, MoveID: id,
		Servers: []string{"a.localhost", "b.localhost"}, Managers: append([]wire.ChannelAccount{creator}, agreed.Managers...), Epoch: 3,
		Accept: mv.Accept, AcceptSig: mv.AcceptSig, At: model.Now()}
	peer := model.PeerID(f.Peers[0])
	try := func(what, code string, change func(cc *wire.ChannelCreate)) {
		t.Helper()
		cc := base
		cc.Managers = append([]wire.ChannelAccount(nil), base.Managers...)
		change(&cc)
		answer, err := f.lk(0).TestHubFrame(peer, f.lk(0).TestSignBlock(wire.FChannelCreate, cc), 2*time.Second)
		if err != nil || answer.Code != code {
			t.Fatalf("%s: %+v %v, want %s", what, answer, err, code)
		}
		if list, _ := f.Hub.Hub.Channels(); len(list) != 0 {
			t.Fatalf("%s: the hub holds a channel", what)
		}
		if rec, _, _, held := f.lk(0).TestChannel(model.StreamID(cc.StreamID)); held && !rec.Closed {
			t.Fatalf("%s: the hub prepared the channel: %+v", what, rec)
		}
	}
	// No agreement at all; one a signed itself; b's agreement to this move
	// used for another move, and for another stream; a moderator for b that
	// b did not name; b's managers left out; and one of them swapped.
	try("no agreement", wire.EBadSignature, func(cc *wire.ChannelCreate) { cc.Accept, cc.AcceptSig = nil, nil })
	try("an agreement a signed for b", wire.EBadSignature, func(cc *wire.ChannelCreate) {
		forged := f.lk(0).TestSignBlock(wire.BMoveAccept, wire.MoveAccept{MoveID: id, StreamID: f.stream, Hub: "hub.localhost", Owner: "a.localhost",
			Server: "b.localhost", Managers: agreed.Managers, HubKey: agreed.HubKey, ExpiresAt: model.Now().Add(time.Hour), At: model.Now()})
		cc.Accept, cc.AcceptSig = forged.Signed, forged.Sig
	})
	try("the agreement to another move", wire.EForbidden, func(cc *wire.ChannelCreate) { cc.MoveID = model.NewID[string]() })
	try("the agreement for another stream", wire.EForbidden, func(cc *wire.ChannelCreate) { cc.StreamID = model.NewID[string]() })
	try("a moderator b did not name", wire.EForbidden, func(cc *wire.ChannelCreate) {
		cc.Managers = append(cc.Managers, wire.ChannelAccount{AccountID: string(f.bo.ID), Server: "b.localhost", Username: "bo"})
	})
	try("b's managers left out", wire.EForbidden, func(cc *wire.ChannelCreate) { cc.Managers = []wire.ChannelAccount{creator} })
	try("one of b's managers swapped for an account b did not name", wire.EForbidden, func(cc *wire.ChannelCreate) {
		cc.Managers = []wire.ChannelAccount{creator, agreed.Managers[0], {AccountID: string(f.bo.ID), Server: "b.localhost", Username: "bo"}}
	})
	// With b's managers as b signed for them it is prepared; and the real
	// move then goes through on the same agreement.
	f.lk(0).TestStep(nil)
	f.onHub(id)
	_ = b
}

// stream.moved makes the other server switch only when it agreed to that
// move and the record in it is the agreed hub's.
func TestStreamMovedIsChecked(t *testing.T) {
	f := newMove(t)
	a, b := f.Servers[0], f.Servers[1]
	sid := model.StreamID(f.stream)
	hubPeer, err := b.Server.Store.Peers().Get(ctx, model.PeerID(f.Peers[1]))
	if err != nil {
		t.Fatal(err)
	}
	hubKey := wire.Bytes(a.Server.Link.Identity().Keys.SignPublic())
	forge := func(move string) map[string]any {
		// A record of the right shape that the hub never signed: a signs
		// it, and names as the hub's key first its own, later the hub's.
		rec := f.lk(0).TestSignBlock(wire.FChannel, wire.ChannelRecord{StreamID: f.stream, Hub: "hub.localhost",
			HubKey: hubKey, HomeHub: "hub.localhost", Version: 1, Name: "launch",
			Listing: wire.ListingUnlisted, Servers: []string{"a.localhost", "b.localhost"}, Prepared: true, At: model.Now()})
		commit := f.lk(0).TestSignBlock(wire.FChannelCommit, wire.MoveCommit{MoveID: move, StreamID: f.stream, Hub: "hub.localhost", Owner: "a.localhost",
			Have: map[string]int64{"b.localhost": 1 << 40}, Epoch: 2, At: model.Now()})
		return map[string]any{"move_id": move, "hub": "hub.localhost", "record": rec.Signed, "record_sig": rec.Sig, "commit": commit.Signed, "commit_sig": commit.Sig}
	}
	still := func(what string) {
		t.Helper()
		f.say(f.amy, f.conv[0], "probe a "+what)
		f.bo.HasMessage(f.conv[1], "probe a "+what)
		if st := f.StreamOn(1, f.stream); st["peer_id"] != f.direct[1] || st["reference"] != "owner" {
			t.Fatalf("%s: b switched: %v", what, st)
		}
	}
	// No move at all.
	if err := f.lk(0).TestOriginate(sid, "stream.moved", forge(model.NewID[string]())); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "b rejects a move it never heard of", func() bool { return f.lk(1).StatsNow().Rejected["move_unknown"] == 1 })
	still("unknown")
	// A move b has been asked about and has not agreed to.
	id := f.propose(nil)
	f.await(1, id, "proposed")
	if err := f.lk(0).TestOriginate(sid, "stream.moved", forge(id)); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "b rejects a move it has not agreed to", func() bool { return f.lk(1).StatsNow().Rejected["move_not_accepted"] == 1 })
	still("unanswered")
	// A move b has agreed to, with a record the hub did not sign.
	f.lk(0).TestStep(func(step string) bool { return step == "prepare" })
	f.accept(id)
	f.await(0, id, "accepted")
	if err := f.lk(0).TestOriginate(sid, "stream.moved", forge(id)); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "b rejects a record the hub did not sign", func() bool { return f.lk(1).StatsNow().Rejected["move_bad_record"] == 1 })
	still("forged")
	// The same, naming the hub's real key: it is the key b agreed to, and
	// it did not sign this.
	hubKey = wire.Bytes(hubPeer.HubSignKey)
	if err := f.lk(0).TestOriginate(sid, "stream.moved", forge(id)); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "b rejects a record that only names the hub's key", func() bool { return f.lk(1).StatsNow().Rejected["move_bad_record"] == 2 })
	still("misattributed")
	if f.state(1, id) != "accepted" || f.lk(1).TestOutbox(sid) != 0 {
		t.Fatal("the forgery changed b's move, or took something from its outbox")
	}
	// The real one, afterwards, is taken.
	f.lk(0).TestStep(nil)
	f.onHub(id)
	f.same()
	_ = b
}
