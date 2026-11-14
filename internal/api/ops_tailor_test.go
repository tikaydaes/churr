package api_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/testkit"
)

// drain reads a stream until the message.created that carries the marker
// text and returns every frame before it.
func drain(t *testing.T, s *testkit.Stream, marker string) []map[string]any {
	t.Helper()
	var out []map[string]any
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		f, ok := s.Next(time.Until(deadline))
		if !ok {
			break
		}
		if f["type"] == "message.created" && obj(f, "message")["text"] == marker {
			return out
		}
		out = append(out, f)
	}
	t.Fatalf("the marker %q never arrived", marker)
	return nil
}

// ofType returns the frames of one type about one message or conversation.
func ofType(frames []map[string]any, typ string) []map[string]any {
	var out []map[string]any
	for _, f := range frames {
		if f["type"] == typ {
			out = append(out, f)
		}
	}
	return out
}

// mine lists the emoji of a message's reactions marked `me`.
func mine(msg map[string]any) string {
	var out []string
	for _, r := range arr(msg, "reactions") {
		if e := r.(map[string]any); e["me"] == true {
			out = append(out, e["emoji"].(string))
		}
	}
	return strings.Join(out, ",")
}

// TestNothingPerReaderCrosses holds the rule that an event is shared by
// everyone it is fanned out to and that what differs per reader is put on
// copies. Three people receive the same events, each with something of
// their own on them (a reaction, a poll answer, a thread they follow, a
// parent channel only some can see); nobody is sent what belongs to another,
// live or on catch-up.
func TestNothingPerReaderCrosses(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	people := map[string]*testkit.Client{"ann": ann, "bob": bob, "cat": cat}
	ops := ann.Channel("ops", false, bob, cat)
	// A private channel ann and bob are in, with a public sub-channel all
	// three are in: cat cannot see the parent.
	priv := ann.Channel("den", true, bob)
	child := sub(ann, "den-news", false, priv)
	bob.Must(200, "POST", "/api/v1/conversations/"+child+"/join", nil)
	cat.Must(200, "POST", "/api/v1/conversations/"+child+"/join", nil)

	streams := map[string]*testkit.Stream{}
	for name, c := range people {
		streams[name], _ = c.Events(nil)
	}

	thumb, party := "👍", "🎉"
	msg := ann.Post(ops, "plain")["id"].(string)
	react := func(c *testkit.Client, id, emoji string) {
		c.Must(200, "PUT", "/api/v1/messages/"+id+"/reactions/"+url.PathEscape(emoji), nil)
	}
	react(ann, msg, thumb)
	react(bob, msg, party)
	ann.Must(200, "PATCH", "/api/v1/messages/"+msg, map[string]any{"text": "plain, edited"})

	// A poll whose result is only for those who have answered. bob and
	// cat answer differently; ann does not answer.
	poll := fID(obj(ann.Must(201, "POST", fMsgs(ops), map[string]any{"text": "vote", "form": fPoll(map[string]any{"results": "after_answer"})}), "message"))
	bob.Must(200, "PUT", fForm(poll)+"/answer", fPick("o1"))
	cat.Must(200, "PUT", fForm(poll)+"/answer", fPick("o2"))
	react(cat, poll, thumb)
	ann.Must(200, "PATCH", "/api/v1/messages/"+poll, map[string]any{"text": "vote now"})

	// A thread: bob replies under ann's message, so ann and bob follow it
	// and cat does not.
	bob.Must(201, "POST", fMsgs(ops), map[string]any{"text": "a reply", "parent_id": msg})

	ann.Must(200, "PATCH", "/api/v1/conversations/"+child, map[string]any{"topic": "news"})
	ann.Post(ops, "the end")

	check := func(how, who string, frames []map[string]any) {
		t.Helper()
		var edits, answered, updated []map[string]any
		for _, f := range frames {
			switch {
			case f["type"] == "message.edited" && f["conversation_id"] == ops:
				edits = append(edits, f)
			case f["type"] == "form.answered":
				answered = append(answered, f)
			case f["type"] == "conversation.updated" && f["conversation_id"] == child:
				updated = append(updated, f)
			}
			// No event about a message carries anyone's thread state.
			if b, _ := json.Marshal(f); f["type"] != "thread_state.changed" && strings.Contains(string(b), `"thread_state"`) {
				t.Fatalf("%s, %s: a shared event carries a thread state: %s", how, who, b)
			}
		}
		if len(edits) != 2 || len(answered) != 2 || len(updated) != 1 {
			t.Fatalf("%s, %s: %d edits, %d answers, %d updates", how, who, len(edits), len(answered), len(updated))
		}
		// reactions[].me on message.edited.
		wantMine := map[string]string{"ann": thumb, "bob": party, "cat": ""}[who]
		if got := mine(obj(edits[0], "message")); got != wantMine {
			t.Fatalf("%s, %s: reactions marked me on message.edited: %q, want %q", how, who, got, wantMine)
		}
		for _, f := range ofType(frames, "reaction.added") {
			r := obj(f, "reaction")
			if (r["me"] == true) != (f["account_id"] == string(people[who].ID)) {
				t.Fatalf("%s, %s: me on someone's reaction.added: %v", how, who, f)
			}
		}
		// The poll on message.edited: my_answer and the result belong to
		// those who answered.
		form := obj(edits[1], "message", "form")
		wantPick := map[string]string{"ann": "", "bob": "o1", "cat": "o2"}[who]
		checkForm := func(what string, form map[string]any, answeredYet bool) {
			t.Helper()
			pick := ""
			if c := arr(obj(form, "my_answer"), "choice"); len(c) == 1 {
				pick = c[0].(string)
			}
			want := wantPick
			if !answeredYet {
				want = ""
			}
			if pick != want {
				t.Fatalf("%s, %s: my_answer on %s is %q, want %q: %v", how, who, what, pick, want, form)
			}
			if hasTally := form["tally"] != nil; hasTally != (want != "") {
				t.Fatalf("%s, %s: the result on %s, shown=%v to someone whose answer is %q: %v", how, who, what, hasTally, want, form)
			}
		}
		checkForm("message.edited", form, true)
		if got := mine(obj(edits[1], "message")); got != map[string]string{"ann": "", "bob": "", "cat": thumb}[who] {
			t.Fatalf("%s, %s: reactions marked me on the poll: %q", how, who, got)
		}
		if how == "live" {
			// The first answer was bob's: cat had not answered then. On
			// catch-up the form is read as it is now, so only live holds
			// this.
			checkForm("the first form.answered", obj(answered[0], "form"), who == "bob")
		}
		checkForm("the second form.answered", obj(answered[1], "form"), true)
		// parent_id of a conversation whose parent only some can see.
		if got, want := obj(updated[0], "conversation")["parent_id"], map[string]any{"ann": priv, "bob": priv, "cat": nil}[who]; got != want {
			t.Fatalf("%s, %s: parent_id on conversation.updated is %v, want %v", how, who, got, want)
		}
	}

	for who, s := range streams {
		frames := drain(t, s, "the end")
		check("live", who, frames)
		// Thread state goes to the person it belongs to and nobody else.
		states := ofType(frames, "thread_state.changed")
		if want := map[string]int{"ann": 1, "bob": 1, "cat": 0}[who]; len(states) != want {
			t.Fatalf("live, %s: %d thread_state.changed frames, want %d: %v", who, len(states), want, states)
		}
	}
	for who, c := range people {
		var frames []map[string]any
		for _, conv := range []string{ops, child} {
			out := c.Must(200, "GET", fmt.Sprintf("/api/v1/conversations/%s/events?after=0&limit=200", conv), nil)
			for _, e := range arr(out, "events") {
				frames = append(frames, e.(map[string]any))
			}
		}
		check("catch-up", who, frames)
	}
}
