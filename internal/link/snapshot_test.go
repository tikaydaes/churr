package link_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/server"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Section 17, ruling R62: after a connection comes back a server sends a
// presence snapshot on each stream, but not all in the same moment: each
// goes after its own random delay, so a hub that restarts is not sent every
// server's snapshots at once. They do all arrive, and say what is true when
// they are sent.
func TestSnapshotsAreSpreadAfterAReconnection(t *testing.T) {
	const channels = 6
	const spread = 1500 * time.Millisecond
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	ann := a.Member("ann")
	var streams []model.StreamID
	for i := 0; i < channels; i++ {
		ch := a.Owner.Channel(fmt.Sprintf("room-%d", i), false, ann)
		stream, _ := l.Share(0, ch, fmt.Sprintf("room-%d", i), nil, 1)
		streams = append(streams, model.StreamID(stream))
	}
	// ann is connected, so every snapshot has someone to name.
	ann.Events(nil)
	testkit.Eventually(t, "b hears that ann is online", func() bool {
		p, ok := b.Server.Link.PresenceOf(ann.ID)
		return ok && p.Presence == "online"
	})
	// It hears so once on every stream, and has heard the first by now. The
	// rest must be in before the count below is taken: one that came after
	// it was taken for that stream's snapshot, and the test went on to
	// count what a had sent while a was still sending.
	testkit.Eventually(t, "b has heard it on every stream", func() bool {
		heard := b.Server.Link.StatsNow().Ephemeral
		for _, id := range streams {
			if heard[id] == 0 {
				return false
			}
		}
		return true
	})

	a.Server.Link.TestSnapshotSpread(spread)
	b.Server.Link.TestSnapshotSpread(-1) // b's own snapshots are not what is watched
	before := b.Server.Link.StatsNow().Ephemeral
	sentBefore := a.Server.Link.StatsNow().Snapshots
	peerA, peerB := model.PeerID(l.Peers[0]), model.PeerID(l.Peers[1])
	a.Server.Link.TestDrop(peerA)
	testkit.Eventually(t, "the connection is back", func() bool {
		return a.Server.Link.TestConnected(peerA) && b.Server.Link.TestConnected(peerB)
	})
	back := time.Now()

	// When each stream's snapshot reached b, measured from the reconnection.
	arrived := map[model.StreamID]time.Duration{}
	for deadline := back.Add(spread + 5*time.Second); len(arrived) < channels && time.Now().Before(deadline); {
		now := b.Server.Link.StatsNow().Ephemeral
		for _, id := range streams {
			if _, seen := arrived[id]; !seen && now[id] > before[id] {
				arrived[id] = time.Since(back)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(arrived) != channels {
		t.Fatalf("%d of %d snapshots arrived after the reconnection", len(arrived), channels)
	}
	// a counts a snapshot after it has handed it to the connection, which
	// may be a moment after b has it.
	testkit.Eventually(t, "a has counted every snapshot it sent", func() bool {
		return a.Server.Link.StatsNow().Snapshots-sentBefore >= channels
	})
	if got := a.Server.Link.StatsNow().Snapshots - sentBefore; got != channels {
		t.Fatalf("a sent %d snapshots for %d streams", got, channels)
	}
	first, last := spread*10, time.Duration(0)
	for _, d := range arrived {
		first, last = min(first, d), max(last, d)
	}
	t.Logf("%d snapshots arrived between %v and %v after the reconnection (spread %v)", channels, first.Round(time.Millisecond), last.Round(time.Millisecond), spread)
	// Six random delays over a second and a half that all fell within a
	// tenth of it would be a chance of a few in a hundred thousand; sent
	// at once, as before, they arrive within a few milliseconds of each
	// other.
	if last-first < spread/10 {
		t.Fatalf("the snapshots arrived within %v of each other: they were not spread", last-first)
	}
	if last > spread+2*time.Second {
		t.Fatalf("a snapshot took %v, with a spread of %v", last, spread)
	}
}

// rollData is the data of a roll of 1d6 that came up n, which holds
// together for n from 1 to 6.
func rollData(n int) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"dice":"1d6","rows":[{"dice":"1d6","terms":[{"sign":1,"count":1,"sides":6,"rolls":[%d],"value":%d}],"total":%d}]}`, n, n, n))
}

// insertAs stores a message on a server the way the service stores one,
// values and all, unchecked.
func insertAs(t *testing.T, ts *server.Server, m model.Message) {
	t.Helper()
	m.ID, m.CreatedAt = model.NewID[model.MessageID](), model.Now()
	err := ts.Store.Tx(ctx, func(tx store.Tx) error {
		seq, err := tx.Conversations().NextSeq(ctx, m.ConversationID)
		if err != nil {
			return err
		}
		m.Seq = seq
		return tx.Messages().Insert(ctx, &m, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// valuesOf are a message's view and the objects of its entities, as a
// client is shown them.
func valuesOf(m map[string]any) (view map[string]any, objects []map[string]any) {
	view, _ = m["view"].(map[string]any)
	for _, raw := range arr(m["entities"]) {
		e := raw.(map[string]any)
		if o, ok := e["object"].(map[string]any); ok && e["type"] == model.EntityObject {
			objects = append(objects, o)
		}
	}
	return view, objects
}

// Values travel (docs/DESIGN-MODULES.md section 4): a message's view and its
// object entities reach every instance of a channel on a hub, the hub's
// holder and an instance that joins later, which takes them from the hub in
// the joining snapshot. Each receiver holds a value to its kind's check: one
// that fails falls back a step, to its alt or to the text, and the message
// still arrives; a kind the receiver does not know is carried unchanged.
func TestValuesTravelInTheSnapshotAndAreChecked(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	ch := a.Owner.Channel("table", false)
	msgs := "/api/v1/conversations/" + ch + "/messages"
	rolled := a.Owner.Must(201, "POST", msgs, map[string]any{"chance": map[string]any{"kind": "roll", "dice": "2d6+3"}})["message"].(map[string]any)
	inline := a.Owner.Post(ch, "I hit for [[1d4]]")
	conv := model.ConversationID(ch)
	view := func(data json.RawMessage, alt *model.ValueEnvelope) *model.ValueEnvelope {
		return &model.ValueEnvelope{Kind: model.KindDiceRoll, Level: 1, By: "dice", Data: data, Alt: alt}
	}
	// A kind this server cannot read it holds as the bytes it came as, in
	// base64url text (here a field 3 of 7).
	future := &model.ValueEnvelope{Kind: "tarot_spread", Level: 4, Data: json.RawMessage(`"GAc"`)}
	// Rolls a modified instance might send: dice that do not add up, with
	// and without an alt; inline dice that came up 9 on a d6; and a value
	// of a kind nobody here knows.
	insertAs(t, a.Server, model.Message{ConversationID: conv, AuthorID: a.Owner.ID, Text: "Rolled 1d6: [9] = 9",
		View: view(rollData(9), nil)})
	insertAs(t, a.Server, model.Message{ConversationID: conv, AuthorID: a.Owner.ID, Text: "Rolled 1d6: [7] = 7, or so",
		View: view(rollData(7), view(rollData(5), nil))})
	insertAs(t, a.Server, model.Message{ConversationID: conv, AuthorID: a.Owner.ID, Text: "fixed [[1d6]] and fair [[1d6]]",
		Entities: []model.Entity{{Type: model.EntityObject, At: 6, Len: 7, Object: view(rollData(9), nil)},
			{Type: model.EntityObject, At: 23, Len: 7, Object: view(rollData(2), nil)}}})
	insertAs(t, a.Server, model.Message{ConversationID: conv, AuthorID: a.Owner.ID, Text: "a spread from the future", View: future})

	stream, local := l.Share(0, ch, "table", map[string]any{"listing": "open", "history_days": 30}, 1)
	check := func(on string, has func(text string) map[string]any) {
		t.Helper()
		v, _ := valuesOf(has(rolled["text"].(string)))
		want, _ := valuesOf(rolled)
		if v == nil || fmt.Sprint(v) != fmt.Sprint(want) {
			t.Fatalf("%s: the roll's view is %v, want %v", on, v, want)
		}
		if _, objs := valuesOf(has("I hit for [[1d4]]")); len(objs) != 1 || fmt.Sprint(objs) != fmt.Sprint(func() []map[string]any { _, o := valuesOf(inline); return o }()) {
			t.Fatalf("%s: the inline dice are %v", on, objs)
		}
		if v, _ := valuesOf(has("Rolled 1d6: [9] = 9")); v != nil {
			t.Fatalf("%s: a roll that does not hold together kept its view: %v", on, v)
		}
		if v, _ := valuesOf(has("Rolled 1d6: [7] = 7, or so")); v == nil || fmt.Sprint(v["data"]) != fmt.Sprint(map[string]any{"dice": "1d6", "rows": []any{map[string]any{"dice": "1d6", "total": 5.0,
			"terms": []any{map[string]any{"sign": 1.0, "count": 1.0, "sides": 6.0, "rolls": []any{5.0}, "value": 5.0}}}}}) {
			t.Fatalf("%s: a failed roll did not fall back to its alt: %v", on, v)
		}
		if _, objs := valuesOf(has("fixed [[1d6]] and fair [[1d6]]")); len(objs) != 1 {
			t.Fatalf("%s: of the inline dice, only the fair ones stay: %v", on, objs)
		}
		if v, _ := valuesOf(has("a spread from the future")); v == nil || v["kind"] != "tarot_spread" || v["level"] != 4.0 || v["data"] != "GAc" {
			t.Fatalf("%s: an unknown kind was not carried unchanged: %v", on, v)
		}
	}
	check("b", func(text string) map[string]any { return b.Owner.HasMessage(local[1], text) })

	// The hub's holder keeps what an instance keeps.
	testkit.Eventually(t, "the hub holds the history", func() bool {
		_, held := heldMessages(t, l.Hub, stream)
		_, has := held["a spread from the future"]
		return has
	})
	_, held := heldMessages(t, l.Hub, stream)
	if m := held[rolled["text"].(string)]; m.View == nil || m.View.Kind != model.KindDiceRoll {
		t.Fatalf("the holder keeps the roll as %+v", m.View)
	}
	if m := held["Rolled 1d6: [9] = 9"]; m.View != nil {
		t.Fatalf("the holder kept a roll that does not hold together: %+v", m.View)
	}

	// c joins with a away, and is given all of it by the hub.
	a.Stop()
	convC := joinListed(l, 2, stream, "table-c")
	l.Joined(2, stream)
	check("c", func(text string) map[string]any { return c.Owner.HasMessage(convC, text) })
}

// Values travel live too: a roll and inline dice posted on one instance
// reach the others with their values, and a roll that does not hold
// together arrives as its text.
func TestValuesTravelLiveAndAreChecked(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	ch := a.Owner.Channel("table", false)
	stream, local := l.Share(0, ch, "table", nil, 1)
	msgs := "/api/v1/conversations/" + local[1] + "/messages"
	rolled := b.Owner.Must(201, "POST", msgs, map[string]any{"chance": map[string]any{"kind": "roll", "dice": "d20"}})["message"].(map[string]any)
	inline := b.Owner.Post(local[1], "a check: [[d20+2]]")
	if got, _ := valuesOf(a.Owner.HasMessage(ch, rolled["text"].(string))); fmt.Sprint(got) != fmt.Sprint(rolled["view"]) {
		t.Fatalf("the roll arrived as %v, want %v", got, rolled["view"])
	}
	_, want := valuesOf(inline)
	if _, got := valuesOf(a.Owner.HasMessage(ch, "a check: [[d20+2]]")); len(got) != 1 || fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("the inline dice arrived as %v, want %v", got, want)
	}

	// b's link sends a roll whose die came up 9 on a d6.
	bad := map[string]any{"kind": model.KindDiceRoll, "level": 1, "by": "dice", "data": rollData(9)}
	err := b.Server.Link.TestOriginate(model.StreamID(stream), "message.created", map[string]any{"message": map[string]any{
		"id": model.NewID[model.MessageID](), "author_id": b.Owner.ID, "text": "Rolled 1d6: [9] = 9", "view": bad,
		"entities":   []any{map[string]any{"type": "object", "at": 7, "len": 3, "object": bad}},
		"created_at": model.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	if v, objs := valuesOf(a.Owner.HasMessage(ch, "Rolled 1d6: [9] = 9")); v != nil || len(objs) != 0 {
		t.Fatalf("a kept a roll that does not hold together: %v %v", v, objs)
	}
}
