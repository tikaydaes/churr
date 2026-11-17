package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Watches: "tell me when this person is around" (rulings R76 and R77).

const fWatches = "/api/v1/me/watches/"

// wWaiting is the caller's waiting watches.
func wWaiting(c *testkit.Client) []map[string]any {
	var out []map[string]any
	for _, raw := range arr(c.Must(200, "GET", fScheduled+"?kind=watch", nil), "scheduled") {
		it := raw.(map[string]any)
		if it["next_run_at"] != nil {
			out = append(out, it)
		}
	}
	return out
}

// wFired waits for a watch's message: the scheduled.fired of the item and
// the reminder it posted, which comes first. It returns the message.created
// frame.
func wFired(t *testing.T, s *testkit.Stream, id string) map[string]any {
	t.Helper()
	var fired map[string]any
	created := map[any]map[string]any{}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f, ok := s.Next(time.Until(deadline))
		if !ok {
			break
		}
		switch f["type"] {
		case "message.created":
			created[obj(f, "message")["id"]] = f
		case "scheduled.fired":
			if obj(f, "scheduled")["id"] == id {
				fired = f
			}
		}
		if fired != nil {
			if fired["message_id"] == nil {
				t.Fatalf("the watch did not post: %v", fired)
			}
			if m := created[fired["message_id"]]; m != nil {
				return m
			}
		}
	}
	t.Fatalf("the watch %s did not fire (fired: %v)", id, fired)
	return nil
}

// TestWatch: a watch fires once when its person is next around, and at once
// says so if they are around already; the refusals; and the cases that keep
// it waiting.
func TestWatch(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("dan")
	// cat is a guest of a channel ann is not in: they share nothing.
	cat := ts.Guest("cat", ts.Owner.Channel("lobby", false))
	gus := ts.Member("gus")
	bot := ts.Owner.Bot("helper")
	room := ann.Channel("room", false, bob, dan, gus)
	// hal is a guest of room alone, so ann sees his presence only there.
	hal := ts.Guest("hal", room)
	ann.Add(room, "", bot)

	// Who may be watched.
	ann.Refused(403, model.CodeForbidden, "PUT", fWatches+string(ann.ID), nil)
	ann.Refused(403, model.CodeForbidden, "PUT", fWatches+string(cat.ID), nil) // shares no conversation
	ann.Refused(403, model.CodeForbidden, "PUT", fWatches+string(bot.ID), nil)
	ann.Refused(404, model.CodeNotFound, "PUT", fWatches+model.NewID[string](), nil)
	bot.Refused(403, model.CodeForbidden, "PUT", fWatches+string(ann.ID), nil)

	annS, _ := ann.Events(nil)
	made := ann.Must(201, "PUT", fWatches+string(bob.ID), nil)
	w := obj(made, "scheduled")
	if made["around"] != false || w["kind"] != "watch" || w["watch_account_id"] != string(bob.ID) || w["account_id"] != string(ann.ID) {
		t.Fatalf("a watch: %v", made)
	}
	if e := annS.Expect("scheduled.created"); obj(e, "scheduled")["id"] != w["id"] {
		t.Fatalf("scheduled.created: %v", e)
	}
	expires, _ := time.Parse(time.RFC3339, w["next_run_at"].(string))
	if d := time.Until(expires); d < 47*time.Hour || d > 49*time.Hour {
		t.Fatalf("a watch expires in 48 hours, this one in %v", d)
	}
	if list := wWaiting(ann); len(list) != 1 || list[0]["id"] != w["id"] {
		t.Fatalf("the watch is listed with the reminders: %v", list)
	}
	ann.Refused(409, model.CodeConflict, "PUT", fWatches+string(bob.ID), nil) // one per person
	ann.Refused(409, model.CodeConflict, "PATCH", fScheduled+"/"+w["id"].(string), map[string]any{"run_at": fIn(time.Hour)})
	ann.Refused(409, model.CodeConflict, "POST", fScheduled+"/"+w["id"].(string)+"/send", nil)
	bob.Refused(404, model.CodeNotFound, "GET", fScheduled+"/"+w["id"].(string), nil) // bob never sees it

	// bob comes online: ann is told once, by a reminder in her own channel
	// that notifies her.
	bobS, _ := bob.Events(nil)
	got := wFired(t, annS, w["id"].(string))
	m := obj(got, "message")
	if m["text"] != "@bob is around" || obj(m, "via")["kind"] != "reminder" || got["notify"] != "notify" || m["author_id"] != string(ann.ID) {
		t.Fatalf("the watch's message: %v", got)
	}
	if len(wWaiting(ann)) != 0 {
		t.Fatal("a fired watch still waits")
	}
	// Around already: said at once, nothing stored.
	if out := ann.Must(200, "PUT", fWatches+string(bob.ID), nil); out["around"] != true || out["scheduled"] != nil {
		t.Fatalf("watching someone around: %v", out)
	}
	if len(wWaiting(ann)) != 0 {
		t.Fatal("watching someone around stored a watch")
	}
	// Appearing away and back does not fire it again.
	bob.Must(200, "PUT", "/api/v1/me/away", nil)
	bob.Must(204, "DELETE", "/api/v1/me/away", nil)
	bobS.Close()

	// dan has blocked ann (R77): to ann dan is always online, so the
	// answer is the one for someone around, the same in every byte.
	dan.Must(204, "PUT", "/api/v1/me/blocks/"+string(ann.ID), nil)
	status, blocked := ann.Do("PUT", fWatches+string(dan.ID), nil)
	if status != 200 || len(blocked) != 1 || blocked["around"] != true {
		t.Fatalf("watching someone who blocked the watcher: %d %v", status, blocked)
	}
	if len(wWaiting(ann)) != 0 {
		t.Fatal("a watch was stored for someone who blocked the watcher")
	}

	// ann blocks gus: her watch for him goes, and a new one is refused
	// plainly.
	gw := obj(ann.Must(201, "PUT", fWatches+string(gus.ID), nil), "scheduled")
	ann.Must(204, "PUT", "/api/v1/me/blocks/"+string(gus.ID), nil)
	if e := annS.Expect("scheduled.cancelled"); obj(e, "scheduled")["id"] != gw["id"] {
		t.Fatalf("the watch for someone blocked did not go: %v", e)
	}
	refused := ann.Refused(403, model.CodeForbidden, "PUT", fWatches+string(gus.ID), nil)
	if msg := refused["message"]; msg != "You are ignoring gus." {
		t.Fatalf("the refusal says %q", msg)
	}
	ann.Must(204, "DELETE", "/api/v1/me/blocks/"+string(gus.ID), nil)

	// Appearing away: gus is connected and active but shown away, so the
	// watch waits; when he stops appearing away it fires.
	gw = obj(ann.Must(201, "PUT", fWatches+string(gus.ID), nil), "scheduled")
	gus.Must(200, "PUT", "/api/v1/me/away", nil)
	gusS, _ := gus.Events(nil)
	gusS.Send(map[string]any{"type": "activity"})
	time.Sleep(300 * time.Millisecond)
	if len(wWaiting(ann)) != 1 {
		t.Fatal("a watch fired for someone appearing away")
	}
	gus.Must(204, "DELETE", "/api/v1/me/away", nil)
	wFired(t, annS, gw["id"].(string))
	gusS.Close()

	// A block made after the watch: it waits, never fires.
	cat2 := ts.Member("cat2")
	ann.Add(room, "", cat2)
	cw := obj(ann.Must(201, "PUT", fWatches+string(cat2.ID), nil), "scheduled")
	cat2.Must(204, "PUT", "/api/v1/me/blocks/"+string(ann.ID), nil)
	catS, _ := cat2.Events(nil)
	catS.Send(map[string]any{"type": "activity"})

	// Removed from every conversation they shared: hal's presence is no
	// longer ann's to see, so the watch waits.
	hw := obj(ann.Must(201, "PUT", fWatches+string(hal.ID), nil), "scheduled")
	ann.Must(204, "DELETE", "/api/v1/conversations/"+room+"/members/"+string(hal.ID), nil)
	halS, _ := hal.Events(nil)
	halS.Send(map[string]any{"type": "activity"})
	time.Sleep(500 * time.Millisecond)
	left := map[any]bool{}
	for _, it := range wWaiting(ann) {
		left[it["id"]] = true
	}
	if !left[cw["id"]] || !left[hw["id"]] || len(left) != 2 {
		t.Fatalf("waiting: %v; want the watches of cat2 (who blocked ann) and hal (removed)", left)
	}

	// Stopping: /unwatch, and cancelling the item, are the same.
	ann.Must(204, "DELETE", fWatches+string(hal.ID), nil)
	ann.Refused(404, model.CodeNotFound, "DELETE", fWatches+string(hal.ID), nil)
	ann.Must(204, "DELETE", fScheduled+"/"+cw["id"].(string), nil)
	if len(wWaiting(ann)) != 0 {
		t.Fatal("stopped watches still wait")
	}
	catS.Close()
	halS.Close()
}

// TestWatchLimits: five waiting at most, twenty asked for a day, and a watch
// that waits 48 hours expires.
func TestWatchLimits(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	var people []*testkit.Client
	for _, n := range []string{"p1", "p2", "p3", "p4", "p5", "p6"} {
		people = append(people, ts.Member(n))
	}
	ann.Channel("all", false, people...)
	annS, _ := ann.Events(nil)
	var first string
	for i, p := range people[:5] {
		id := obj(ann.Must(201, "PUT", fWatches+string(p.ID), nil), "scheduled")["id"].(string)
		if i == 0 {
			first = id
		}
	}
	ann.Refused(409, model.CodeConflict, "PUT", fWatches+string(people[5].ID), nil)

	// Expiry: the time is moved on in the row, and the loop is woken.
	_, err := ts.Server.Store.Scheduled().Update(context.Background(), first, func(it *model.ScheduledItem) error {
		it.NextRunAt = model.Now().Add(-time.Second)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ts.Server.Sched.Wake()
	if e := annS.Expect("scheduled.cancelled"); obj(e, "scheduled")["id"] != first {
		t.Fatalf("expiry: %v", e)
	}
	if len(wWaiting(ann)) != 4 {
		t.Fatal("an expired watch still waits")
	}
	// Its person coming online now tells ann nothing.
	p1S, _ := people[0].Events(nil)
	p1S.Close()

	// Twenty a day, whatever became of them: 6 asked so far.
	ann.Must(201, "PUT", fWatches+string(people[5].ID), nil)
	annS.Expect("scheduled.created")
	for i := 0; i < 13; i++ {
		ann.Refused(403, model.CodeForbidden, "PUT", fWatches+string(ann.ID), nil)
	}
	ann.Refused(429, model.CodeRateLimited, "PUT", fWatches+string(people[0].ID), nil)
	annS.ExpectNone(300 * time.Millisecond)
}

// TestWatchRestart: a watch outlives a restart of the server.
func TestWatchRestart(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ann.Channel("room", false, bob)
	id := obj(ann.Must(201, "PUT", fWatches+string(bob.ID), nil), "scheduled")["id"].(string)
	ts.Restart()
	annS, _ := ann.Events(nil)
	bob.Events(nil)
	wFired(t, annS, id)
}

// TestWatchAcrossServers: a person on b watches someone of a, through a hub.
// b keeps the watch and fires it from the presence a sends over the link;
// nothing about it reaches a. Someone appearing away on a is shown away on
// b, so a watch for them waits.
func TestWatchAcrossServers(t *testing.T) {
	l := testkit.NewLinked(t, 2, true)
	a, b := l.Servers[0], l.Servers[1]
	ann, cat := a.Member("ann"), a.Member("cat")
	ch := a.Owner.Channel("bridge", false, ann, cat)
	_, local := l.Share(0, ch, "bridge", nil, 1)
	bob := b.Member("bob")
	bob.Must(200, "POST", "/api/v1/conversations/"+local[1]+"/join", nil)

	bobS, _ := bob.Events(nil)
	w := obj(bob.Must(201, "PUT", fWatches+string(ann.ID), nil), "scheduled")
	cw := obj(bob.Must(201, "PUT", fWatches+string(cat.ID), nil), "scheduled")
	cat.Must(200, "PUT", "/api/v1/me/away", nil)
	catS, _ := cat.Events(nil)
	annS, _ := ann.Events(nil)
	got := wFired(t, bobS, w["id"].(string))
	if m := obj(got, "message"); m["text"] != "@ann@a.localhost is around" {
		t.Fatalf("the watch's message: %v", m)
	}
	testkit.Eventually(t, "b hears that cat is away", func() bool {
		p, ok := b.Server.Link.PresenceOf(cat.ID)
		return ok && p.Presence == "away"
	})
	time.Sleep(300 * time.Millisecond)
	if list := wWaiting(bob); len(list) != 1 || list[0]["id"] != cw["id"] {
		t.Fatalf("waiting: %v; want the watch for cat, who appears away", list)
	}
	// a holds nothing about it.
	if items, err := a.Server.Store.Scheduled().Waiting(context.Background(), model.ScheduledWatch, ""); err != nil || len(items) != 0 {
		t.Fatalf("a holds watches: %v %v", items, err)
	}
	annS.Close()
	catS.Close()
}
