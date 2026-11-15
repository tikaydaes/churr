package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// TestReplayTailoredAllAtOnceIsTheSame holds the two ways of filling in the
// events of a catch-up against each other: one event at a time
// (live.Hub.Tailored, which asks the notification rule about one message
// and one reader) and a page at a time for one reader
// (live.Hub.TailoredFor, which a resume and GET .../events use). The bytes
// must be the same for every reader and every event of a server on which
// readers differ in every way the events show: mode, do not disturb,
// blocks, threads followed and ignored, mentions, reactions, poll answers,
// what they are members of and what they may see.
func TestReplayTailoredAllAtOnceIsTheSame(t *testing.T) {
	ts := testkit.NewServer(t)
	ctx := context.Background()
	st := ts.Server.Store
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	names := []string{"ann", "bob", "cat", "dan", "eve", "fay", "gus", "hal", "ivy"}
	people := map[string]*testkit.Client{}
	var all []*testkit.Client
	for _, n := range names {
		people[n] = ts.Member(n)
		all = append(all, people[n])
	}
	ann, bob, cat, dan := people["ann"], people["bob"], people["cat"], people["dan"]
	// The owner is in none of the channels and the guest only in one.
	// Bots are members too, so that more people react to a message than
	// an event lists by name.
	ops := ann.Channel("ops", false, all[1:]...)
	guest := ts.Guest("guest", ops)
	var bots []*testkit.Client
	for i := 0; i < 4; i++ {
		b := ts.Owner.Bot(fmt.Sprintf("bot%d", i))
		fAdd(t, ts, ops, b, model.LevelMember)
		bots = append(bots, b)
	}
	side := ann.Channel("side", false, bob, cat)
	priv := ann.Channel("den", true, bob)
	child := sub(ann, "den-news", false, priv)
	for _, c := range []*testkit.Client{bob, cat, dan} {
		c.Must(200, "POST", "/api/v1/conversations/"+child+"/join", nil)
	}

	// Readers that differ.
	mode := func(conv string, c *testkit.Client, fn func(m *model.Member)) {
		t.Helper()
		_, err := st.Members().UpdateOwn(ctx, model.ConversationID(conv), c.ID, func(m *model.Member) error { fn(m); return nil })
		must(err)
	}
	for _, c := range all {
		mode(ops, c, func(m *model.Member) { m.Mode = model.ModeAny })
	}
	mode(ops, cat, func(m *model.Member) { m.Mode = model.ModeMentions })
	mode(ops, dan, func(m *model.Member) { m.Mode = model.ModeNothing })
	mode(ops, people["eve"], func(m *model.Member) { m.ThreadView = "flat" })
	mode(side, cat, func(m *model.Member) { m.Mode = model.ModeMentions })
	_, err := st.Accounts().Update(ctx, people["fay"].ID, func(a *model.Account) error { zero := model.Time(0); a.DNDUntil = &zero; return nil })
	must(err)
	must(st.Blocks().Add(ctx, people["gus"].ID, ann.ID, model.Now()))

	react := func(c *testkit.Client, id, emoji string) {
		t.Helper()
		c.Must(200, "PUT", "/api/v1/messages/"+id+"/reactions/"+url.PathEscape(emoji), nil)
	}
	thumb, party := "👍", "🎉"

	// Events of every kind that is filled in per reader.
	root := ann.Post(ops, "plain")["id"].(string)
	ann.Post(ops, "hey @bob and @cat")
	bob.Post(ops, "everyone: @channel")
	dan.Post(ops, "from dan to @ann")
	ann.Must(201, "POST", fMsgs(ops), map[string]any{"text": "a reply", "parent_id": root})
	// hal follows the thread, ivy ignores it; bob follows by replying.
	_, err = st.Threads().Set(ctx, model.MessageID(root), people["hal"].ID, model.ThreadFollowing, 0, model.Now())
	must(err)
	_, err = st.Threads().Set(ctx, model.MessageID(root), people["ivy"].ID, model.ThreadIgnored, 0, model.Now())
	must(err)
	bob.Must(201, "POST", fMsgs(ops), map[string]any{"text": "another reply", "parent_id": root})
	ann.Must(201, "POST", fMsgs(ops), map[string]any{"text": "a reply for @ivy and @dan", "parent_id": root})
	cat.Must(201, "POST", fMsgs(ops), map[string]any{"text": "a reply to @channel", "parent_id": root})
	// More people react than an event lists by name.
	for _, c := range append(append([]*testkit.Client{}, all...), bots...) {
		react(c, root, thumb)
	}
	react(bob, root, party)
	ann.Must(200, "PATCH", "/api/v1/messages/"+root, map[string]any{"text": "plain, edited"})
	bob.Must(204, "DELETE", "/api/v1/messages/"+root+"/reactions/"+url.PathEscape(party), nil)
	poll := fID(obj(ann.Must(201, "POST", fMsgs(ops), map[string]any{"text": "vote", "form": fPoll(map[string]any{"results": "after_answer"})}), "message"))
	bob.Must(200, "PUT", fForm(poll)+"/answer", fPick("o1"))
	cat.Must(200, "PUT", fForm(poll)+"/answer", fPick("o2"))
	react(cat, poll, thumb)
	ann.Must(200, "PATCH", "/api/v1/messages/"+poll, map[string]any{"text": "vote now"})
	gone := bob.Post(ops, "to be deleted")["id"].(string)
	react(cat, gone, thumb)
	bob.Must(200, "DELETE", "/api/v1/messages/"+gone, nil)
	// A message pushed through to someone in do not disturb.
	pushed := ann.Post(ops, "urgent, @fay")["id"].(string)
	_, _, err = st.NotifyAnyway().Record(ctx, model.MessageID(pushed), people["fay"].ID, ann.ID, model.Now(), true)
	must(err)
	for i := 0; i < 5; i++ {
		ann.Post(side, fmt.Sprintf("side %d @cat", i))
		bob.Post(side, fmt.Sprintf("side again %d", i))
	}
	ann.Must(200, "PATCH", "/api/v1/conversations/"+child, map[string]any{"topic": "news"})
	ann.Post(child, "in the sub-channel, @dan")
	ann.Post(priv, "in private")
	ann.Post(ops, "the end")

	convs := []string{ops, side, priv, child}
	sort.Strings(convs)
	log := map[string][]model.Event{}
	for _, conv := range convs {
		after := int64(0)
		for {
			page, more, err := st.Events().List(ctx, model.ConversationID(conv), after, 0, 50)
			must(err)
			log[conv] = append(log[conv], page...)
			if !more || len(page) == 0 {
				break
			}
			after = page[len(page)-1].Seq
		}
		if len(log[conv]) == 0 {
			t.Fatalf("no events in %s", conv)
		}
	}

	hub := ts.Server.Live
	readers := append(append([]*testkit.Client{}, all...), guest, ts.Owner, bots[0])
	notifies, changed, total := map[string]int{}, 0, 0
	compare := func(when string) {
		t.Helper()
		for _, r := range readers {
			for _, conv := range convs {
				events := log[conv]
				var want [][]byte
				for _, e := range events {
					want = append(want, hub.Tailored(ctx, e, r.ID))
				}
				// All at once, and in pages that cut the log at odd places,
				// with one function for the whole catch-up as a resume has.
				for _, size := range []int{len(events), 7, 1} {
					fill := hub.TailoredFor(r.ID)
					var got [][]byte
					for from := 0; from < len(events); from += size {
						for _, b := range fill(ctx, events[from:min(from+size, len(events))]) {
							got = append(got, b)
						}
					}
					if len(got) != len(want) {
						t.Fatalf("%d frames for %d events", len(got), len(want))
					}
					for i := range want {
						if !bytes.Equal(got[i], want[i]) {
							t.Errorf("%s: reader %s, %s seq %d, pages of %d:\n all at once: %s\n one by one:  %s", when, r.ID, events[i].Type, events[i].Seq, size, got[i], want[i])
						}
					}
				}
				for i, e := range events {
					total++
					_, base, err := e.Normalize()
					must(err)
					if !bytes.Equal(base, want[i]) {
						changed++
					}
					if e.Type == model.EventMessageCreated {
						var f map[string]any
						must(json.Unmarshal(want[i], &f))
						notifies[fmt.Sprint(f["notify"])]++
					}
				}
			}
		}
	}
	compare("blocking on")
	// The comparison must be of something.
	if notifies["badge"] < 20 || notifies["none"] < 20 || notifies["notify"] != 0 {
		t.Fatalf("message.created on catch-up: %v; the fixture proves too little", notifies)
	}
	if changed < total/4 {
		t.Fatalf("only %d of %d frames differ from the shared event; the fixture proves too little", changed, total)
	}

	// The same through the door clients use: a resume from nothing gets,
	// before `ready`, each conversation's events one after the other, the
	// conversations in the order of their ids.
	for _, r := range []*testkit.Client{ann, bob, cat, dan, people["fay"], people["gus"], people["ivy"], guest} {
		positions := map[string]int64{}
		var want []map[string]any
		for _, conv := range convs {
			if _, isMember, err := st.Members().Get(ctx, model.ConversationID(conv), r.ID); err != nil || !isMember {
				continue
			}
			positions[conv] = 0
			for _, e := range log[conv] {
				var f map[string]any
				must(json.Unmarshal(hub.Tailored(ctx, e, r.ID), &f))
				want = append(want, f)
			}
		}
		s, frames := r.Events(positions)
		s.Close()
		frames = frames[:len(frames)-1] // ready
		if len(frames) != len(want) {
			t.Fatalf("reader %s: a resume sent %d frames, the log has %d", r.ID, len(frames), len(want))
		}
		for i := range want {
			if !reflect.DeepEqual(frames[i], want[i]) {
				t.Errorf("reader %s, frame %d of a resume:\n got  %v\n want %v", r.ID, i, frames[i], want[i])
			}
		}
		// And over HTTP.
		for conv := range positions {
			out := r.Must(200, "GET", fmt.Sprintf("/api/v1/conversations/%s/events?after=0&limit=200", conv), nil)
			var wantHTTP []any
			for _, e := range log[conv] {
				var f map[string]any
				must(json.Unmarshal(hub.Tailored(ctx, e, r.ID), &f))
				wantHTTP = append(wantHTTP, f)
			}
			if !reflect.DeepEqual(arr(out, "events"), wantHTTP) {
				t.Errorf("reader %s: GET events of %s differs from the events one by one", r.ID, conv)
			}
		}
	}
	ts.Set(store.SettingBlockingEnabled, false)
	compare("blocking off")
}

// TestResumeWhereTheLogHasGaps holds what a resume does when the log does
// not simply continue from where the client left off: the start of the log
// is looked up only then, and the answers are the ones it always gave.
func TestResumeWhereTheLogHasGaps(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	seqs := func(conv string, texts ...string) []int64 {
		var out []int64
		for _, text := range texts {
			out = append(out, int64(ann.Post(conv, text)["seq"].(float64)))
		}
		return out
	}
	// Whole: nothing is missing. Hole: the event after bob's position is
	// gone (its message was removed) but the log starts before it.
	// Trimmed: the log starts after bob's position. Empty: it holds
	// nothing any more.
	whole, hole, trimmed, empty := ann.Channel("whole", false, bob), ann.Channel("hole", false, bob), ann.Channel("trimmed", false, bob), ann.Channel("empty", false, bob)
	w := seqs(whole, "one", "two", "three")
	h := seqs(hole, "one", "two", "three", "four")
	tr := seqs(trimmed, "one", "two", "three")
	e := seqs(empty, "one", "two")
	ts.Exec(`DELETE FROM events WHERE conversation_id = ? AND seq = ?`, hole, h[1])
	ts.Exec(`DELETE FROM events WHERE conversation_id = ? AND seq <= ?`, trimmed, tr[1])
	ts.Exec(`DELETE FROM events WHERE conversation_id = ?`, empty)

	s, frames := bob.Events(map[string]int64{whole: w[0], hole: h[0], trimmed: tr[0], empty: e[0]})
	s.Close()
	got := map[string][]int64{}
	reloads := map[string]map[string]any{}
	for _, f := range frames {
		conv, _ := f["conversation_id"].(string)
		switch f["type"] {
		case "reload":
			reloads[conv] = f
		case "message.created":
			got[conv] = append(got[conv], int64(f["seq"].(float64)))
		}
	}
	if !reflect.DeepEqual(got[whole], w[1:]) || reloads[whole] != nil {
		t.Errorf("a log with nothing missing: events %v, reload %v", got[whole], reloads[whole])
	}
	if !reflect.DeepEqual(got[hole], h[2:]) || reloads[hole] != nil {
		t.Errorf("a log with one event gone: events %v, reload %v", got[hole], reloads[hole])
	}
	for name, c := range map[string]struct {
		conv string
		last int64
	}{"a log that starts too late": {trimmed, tr[2]}, "a log that holds nothing": {empty, e[1]}} {
		r := reloads[c.conv]
		if len(got[c.conv]) != 0 || r == nil || r["reason"] != "too_far_behind" || r["last_seq"] != float64(c.last) {
			t.Errorf("%s: events %v, reload %v (last seq %d)", name, got[c.conv], r, c.last)
		}
	}
}
