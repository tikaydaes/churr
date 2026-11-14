package main_test

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/testkit"
)

// The commands that join hubs into a cluster, run as an operator runs them:
// the churr-hub program, built here, in a process of its own, on the data
// directory of a hub that is serving. Each command of
// docs/LINK-PROTOCOL.md section 23b is run at least once: trunk invite,
// trunk join, trunk cancel, cluster, cluster leave, cluster remove, rehome.
// The hubs themselves serve inside the test, so the test can see what the
// commands did to them.
func TestClusterCommandsThroughTheProgram(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the churr-hub program")
	}
	bin := filepath.Join(t.TempDir(), "churr-hub")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("building churr-hub: %v\n%s", err, out)
	}
	// run runs one command on a hub's data and returns what it printed and
	// whether it failed.
	run := func(h *testkit.TestHub, args ...string) (string, error) {
		t.Helper()
		cmd := exec.Command(bin, append(args, "-data", h.Dir, "-hostname", h.Name)...)
		var out, errs bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errs
		err := cmd.Run()
		t.Logf("churr-hub %s (on %s): %s%s", strings.Join(args, " "), h.Name, out.String(), errs.String())
		return strings.TrimSpace(out.String() + errs.String()), err
	}
	must := func(h *testkit.TestHub, args ...string) string {
		t.Helper()
		out, err := run(h, args...)
		if err != nil {
			t.Fatalf("churr-hub %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return out
	}
	lines := func(s string) []string { return strings.Split(strings.TrimSpace(s), "\n") }

	c := testkit.NewHubs(t, 3)
	a, b, cc := c.Hubs[0], c.Hubs[1], c.Hubs[2]

	// A hub alone is a cluster of one.
	if got := lines(must(a, "cluster")); len(got) != 1 || !strings.Contains(got[0], a.Name) || !strings.Contains(got[0], "this hub") {
		t.Fatalf("cluster on a hub that stands alone: %q", got)
	}

	// trunk invite, trunk join: hub b joins hub a.
	inv := must(a, "trunk", "invite")
	if out := must(b, "trunk", "join", inv); !strings.Contains(out, "dials within a second") {
		t.Fatalf("trunk join said: %s", out)
	}
	c.Joined(1, 0)
	got := must(b, "cluster")
	if n := len(lines(got)); n != 2 || !strings.Contains(got, "joined through "+a.Name) || !strings.Contains(got, "trunk active") {
		t.Fatalf("cluster on hub b after it joined:\n%s", got)
	}
	// A hub that is in a cluster cannot join it again, and is told why.
	if out, err := run(b, "trunk", "join", must(a, "trunk", "invite")); err == nil || !strings.Contains(out, "loop") {
		t.Fatalf("a second trunk into the same cluster: %v: %s", err, out)
	}

	// trunk cancel: hub c is told to join while it cannot reach hub a, and
	// its operator gives up.
	cc.Hub.TestCutTrunk(a.Name, true)
	named := must(a, "trunk", "invite", "--for", cc.Name, "--expires", "1h")
	must(cc, "trunk", "join", named)
	if got := must(cc, "cluster"); !strings.Contains(got, a.Name) || !strings.Contains(got, "not a member yet") || !strings.Contains(got, "trunk pending") {
		t.Fatalf("cluster on hub c while its join waits:\n%s", got)
	}
	must(cc, "trunk", "cancel")
	if got := must(cc, "cluster"); strings.Contains(got, "pending") {
		t.Fatalf("cluster on hub c after the join was cancelled:\n%s", got)
	}
	// The invitation was never used; with the way open again it still works.
	cc.Hub.TestCutTrunk(a.Name, false)
	must(cc, "trunk", "join", named)
	c.Joined(2, 0)
	if n := len(lines(must(a, "cluster"))); n != 3 {
		t.Fatalf("hub a lists %d hubs, want 3", n)
	}

	// rehome: a channel shared to hub a, with a server on hub b, moves its
	// home to hub b.
	l := testkit.LinkCluster(t, c, []int{0, 1})
	stream, _ := l.Share(0, l.Servers[0].Owner.Channel("launch", false), "launch", nil, 1)
	if got := must(a, "channels"); !strings.Contains(got, stream) || !strings.Contains(got, "home "+a.Name) {
		t.Fatalf("channels on hub a:\n%s", got)
	}
	if out, err := run(b, "rehome", stream, a.Name); err == nil {
		t.Fatalf("a hub that is not the channel's home moved it: %s", out)
	}
	if out := must(a, "rehome", stream, b.Name); !strings.Contains(out, "home is now "+b.Name) {
		t.Fatalf("rehome said: %s", out)
	}
	testkit.Eventually(t, "hub b lists the channel as its own", func() bool {
		out, err := run(b, "channels")
		return err == nil && strings.Contains(out, stream) && strings.Contains(out, "home "+b.Name)
	})

	// cluster leave: refused on the hub others joined through, done on hub c.
	if out, err := run(a, "cluster", "leave"); err == nil {
		t.Fatalf("the hub the others joined through left: %s", out)
	}
	must(cc, "cluster", "leave")
	testkit.Eventually(t, "hub c stands alone and hub a knows", func() bool {
		return len(cc.Hub.TestMembers()) == 1 && len(a.Hub.TestMembers()) == 2
	})
	if n := len(lines(must(cc, "cluster"))); n > 2 {
		t.Fatalf("hub c still lists %d hubs after leaving", n)
	}

	// cluster remove: only where the hub joined, and then it is out.
	if out, err := run(b, "cluster", "remove", a.Name); err == nil {
		t.Fatalf("a hub removed the hub it joined through: %s", out)
	}
	must(a, "cluster", "remove", b.Name)
	testkit.Eventually(t, "hub b is out", func() bool {
		return len(a.Hub.TestMembers()) == 1 && len(b.Hub.TestMembers()) == 1
	})
	if got := lines(must(a, "cluster")); len(got) > 2 || !strings.Contains(got[0], "this hub") {
		t.Fatalf("cluster on hub a at the end: %q", got)
	}
}
