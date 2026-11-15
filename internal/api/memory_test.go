package api_test

import (
	"math"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/live"
	"github.com/tikaydaes/churr/internal/memguard"
	"github.com/tikaydaes/churr/internal/testkit"
)

// shortMachine is a machine of 8 GB whose free memory the test sets.
type shortMachine struct{ left atomic.Int64 }

func (m *shortMachine) Read() (memguard.Reading, bool) {
	return memguard.Reading{Total: 8 << 30, Headroom: m.left.Load(), From: "a test"}, true
}

// A whole server that is short of memory: the live connection is refused
// with 4029 and nothing else is, the admin Overview says so, people already
// connected carry on, and when memory returns connections are taken again.
func TestShortOfMemory(t *testing.T) {
	// The guard sets the Go runtime's memory limit; the other tests of
	// this package get it back as it was.
	t.Cleanup(func() { debug.SetMemoryLimit(math.MaxInt64) })
	machine := &shortMachine{}
	machine.left.Store(6 << 30)
	ts := testkit.NewServer(t, testkit.Options{Memory: machine})
	guard := ts.Server.Memory
	if guard == nil {
		t.Fatal("the server has no memory guard")
	}
	ann, bob := ts.Member("ann"), ts.Member("bob")
	room := ann.Channel("room", false, bob)
	guard.Check()
	stream, _ := ann.Events(nil)

	machine.left.Store(1 << 30) // under a fifth of 8 GB
	guard.Check()
	if guard.State() != memguard.Tight {
		t.Fatalf("state %v, want tight", guard.State())
	}
	code, reason := bob.DialRefused()
	if code != live.CloseTooManySends || !strings.Contains(reason, "short of memory") {
		t.Fatalf("a new connection on a server short of memory: close %d %q, want 4029", code, reason)
	}
	// Everything else still works, for the one refused as well.
	say(bob, room, "still here")
	if f := stream.Expect("message.created"); f == nil {
		t.Fatal("the connection that was there did not hear the message")
	}
	ov := obj(ts.Owner.Must(200, "GET", "/api/v1/admin/overview", nil), "overview")
	found := false
	for _, w := range arr(ov, "warnings") {
		if s, _ := w.(string); strings.Contains(s, "short of memory (tight)") && strings.Contains(s, "1 refused") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the Overview does not say memory is short: %v", ov["warnings"])
	}

	machine.left.Store(6 << 30)
	deadline := time.Now().Add(10 * time.Second)
	for guard.State() != memguard.Normal && time.Now().Before(deadline) {
		guard.Check()
	}
	if guard.State() != memguard.Normal {
		t.Fatal("the guard did not go back to normal when memory returned")
	}
	bob.Events(nil)
	ov = obj(ts.Owner.Must(200, "GET", "/api/v1/admin/overview", nil), "overview")
	for _, w := range arr(ov, "warnings") {
		if s, _ := w.(string); strings.Contains(s, "short of memory") {
			t.Fatalf("the Overview still says memory is short: %v", s)
		}
	}
}
