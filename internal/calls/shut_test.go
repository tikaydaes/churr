package calls

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// A participant's worker that looked at the room just before the
// participant left, and gets to its media after they were closed, makes
// no new connection: one made then would never be closed, and it and the
// track forwarded on it would hold their goroutines for good.
func TestNoMediaAfterLeaving(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m, err := openMedia(Config{Listen: "127.0.0.1:0"}, log, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.close)
	s := &Server{log: log, media: m}
	r := newRoom(s, &liveCall{id: model.NewID[model.CallID]()})
	join := func() *participant {
		return &participant{room: r, id: model.NewID[model.AccountID](), tracks: map[string]*pubTrack{}, picker: newPicker(),
			shown: map[string]choice{}, downs: map[string]*downTrack{}, kick: make(chan struct{}, 1), stop: make(chan struct{})}
	}
	ann, bob := join(), join()
	mic := &pubTrack{id: "t1", kind: kindAudio, codec: codecOpus, owner: ann, mid: "0", source: sourceMicrophone}
	ann.tracks["0"] = mic
	r.parts[ann.id], r.parts[bob.id] = ann, bob
	r.tracks[mic.id] = mic

	// Bob's worker is past its look at the room (he is not yet marked
	// gone) when his media is closed.
	bob.shut()
	bob.sync()

	bob.pcMu.Lock()
	recv, downs := bob.recvPC, len(bob.downs)
	bob.pcMu.Unlock()
	if recv != nil {
		recv.Close()
		t.Fatal("a recv connection was made after the participant's media was closed")
	}
	if downs != 0 || mic.subs.Load() != nil && len(*mic.subs.Load()) != 0 {
		t.Fatalf("%d tracks are forwarded to a participant whose media was closed", downs)
	}
}
