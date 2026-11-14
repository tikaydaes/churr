package calls_test

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/calls/calltest"
)

// offerNames reports whether a `call.offer` names a track as coming from
// an account's device.
func offerNames(track, account, device string) func(map[string]any) bool {
	return func(f map[string]any) bool {
		list, _ := f["tracks"].([]any)
		for _, x := range list {
			t, _ := x.(map[string]any)
			if t["track_id"] != track {
				continue
			}
			got, _ := t["device_id"].(string)
			return t["account_id"] == account && got == device
		}
		return false
	}
}

// deviceIn finds an account's `device_id` in a call object's participants;
// present says whether the field was there at all.
func deviceIn(t *testing.T, call map[string]any, account string) (id string, present bool) {
	t.Helper()
	list, _ := call["participants"].([]any)
	for _, x := range list {
		p, _ := x.(map[string]any)
		if p["account_id"] == account {
			v, has := p["device_id"]
			id, _ = v.(string)
			return id, has
		}
	}
	t.Fatalf("%s is not among the participants: %v", account, call["participants"])
	return "", false
}

// The server names the device each participant is in the call on: in the
// answer to `call.join`, in `call.participant_joined`, in
// `call.track_published` and beside each track of a `call.offer`. A
// sealed chat's call needs it to pick the call sender key for a track
// (docs/CALL-FRAMES.md section 1). A session with no device has none, and
// when the call moves to the account's other device the tracks published
// from there name that one.
func TestCallNamesDevices(t *testing.T) {
	ts := server(t, fast())
	ann, bob, cy := ts.Member("ann"), ts.Member("bob"), ts.Member("cy")
	annDev, bobDev := ann.Sealed("macos"), bob.Sealed("web")
	chat := sealedChat(t, ts, ann.ID, bob.ID)

	watch, _ := bob.Events(nil)
	a := enter(t, ann, chat, calltest.Options{FrameEncryption: true})
	// Everyone in the conversation is told who joined, and on what device.
	for {
		f, ok := watch.Next(10 * time.Second)
		if !ok {
			t.Fatal("bob was not told ann joined")
		}
		if f["type"] != "call.participant_joined" {
			continue
		}
		p := obj(f, "participant")
		if p["account_id"] != string(ann.ID) || p["device_id"] != annDev.ID {
			t.Fatalf("call.participant_joined: %v", f)
		}
		break
	}

	stream, _ := bob.Events(nil)
	b := calltest.New(t, stream, calltest.Options{FrameEncryption: true})
	t.Cleanup(b.Close)
	id, tk := ticket(t, bob, chat)
	joined := b.MustJoin(id, tk)
	call := obj(joined, "call")
	if got, _ := deviceIn(t, call, string(ann.ID)); got != annDev.ID {
		t.Fatalf("ann's device in the answer to call.join: %q, want %q", got, annDev.ID)
	}
	if got, _ := deviceIn(t, call, string(bob.ID)); got != bobDev.ID {
		t.Fatalf("bob's device in the answer to call.join: %q, want %q", got, bobDev.ID)
	}
	// The conversation's call object says the same to someone not in it.
	conv := ann.Must(200, "GET", convPath+chat, nil)
	if got, _ := deviceIn(t, obj(obj(conv, "conversation"), "call"), string(bob.ID)); got != bobDev.ID {
		t.Fatalf("bob's device in the conversation's call: %q, want %q", got, bobDev.ID)
	}

	a.Publish("microphone")
	mic := a.TrackIDs["microphone"]
	pub := b.Expect("call.track_published", func(f map[string]any) bool { return obj(f, "track")["id"] == mic })
	if pub["account_id"] != string(ann.ID) || pub["device_id"] != annDev.ID {
		t.Fatalf("call.track_published: %v", pub)
	}
	b.Expect("call.offer", offerNames(mic, string(ann.ID), annDev.ID))

	// The call moves to ann's second device: what it publishes names it.
	ann2 := ann.NewSession()
	ann2Dev := ann2.Sealed("web")
	if ann2Dev.ID == annDev.ID {
		t.Fatal("the second session was given the first one's device")
	}
	a2 := enter(t, ann2, chat, calltest.Options{FrameEncryption: true})
	a2.Publish("microphone")
	mic2 := a2.TrackIDs["microphone"]
	pub = b.Expect("call.track_published", func(f map[string]any) bool { return obj(f, "track")["id"] == mic2 })
	if pub["device_id"] != ann2Dev.ID {
		t.Fatalf("call.track_published after the call moved: %v", pub)
	}
	b.Expect("call.offer", offerNames(mic2, string(ann.ID), ann2Dev.ID))
	conv = bob.Must(200, "GET", convPath+chat, nil)
	if got, _ := deviceIn(t, obj(obj(conv, "conversation"), "call"), string(ann.ID)); got != ann2Dev.ID {
		t.Fatalf("ann's device after the call moved: %q, want %q", got, ann2Dev.ID)
	}

	// A session with no device: the field is left out, not empty.
	ch := ann.Channel("standup", false, cy)
	c := calltest.New(t, func() calltest.Signaller { s, _ := cy.Events(nil); return s }(), calltest.Options{})
	t.Cleanup(c.Close)
	id, tk = ticket(t, cy, ch)
	if got, present := deviceIn(t, obj(c.MustJoin(id, tk), "call"), string(cy.ID)); present {
		t.Fatalf("a session with no device was given device_id %q", got)
	}
	c.Publish("microphone")
	pub = c.Expect("call.track_published", nil)
	if _, present := pub["device_id"]; present {
		t.Fatalf("call.track_published for a session with no device: %v", pub)
	}
}
