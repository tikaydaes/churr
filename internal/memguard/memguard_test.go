package memguard

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const mb = 1 << 20

// fake is a machine whose memory the test sets, and a runtime with no
// garbage unless the test says so.
type fake struct {
	total, left int64
	unreadable  bool
	held, floor int64
	collected   int
	limit       int64
	// onCollect is what a collection does to the machine.
	onCollect func(*fake)
}

func (f *fake) Read() (Reading, bool) {
	return Reading{Total: f.total, Headroom: f.left, From: "a test"}, !f.unreadable
}
func (f *fake) Heap() Heap { return Heap{Held: f.held, Floor: f.floor} }
func (f *fake) Collect() {
	f.collected++
	if f.onCollect != nil {
		f.onCollect(f)
	}
}
func (f *fake) SetLimit(n int64) { f.limit = n }

type harness struct {
	*fake
	g       *Guard
	now     time.Time
	log     bytes.Buffer
	trimmed int
	shedFor []int64
}

func newHarness(t *testing.T) *harness {
	h := &harness{fake: &fake{total: 1000 * mb, left: 800 * mb, held: 100 * mb, floor: 100 * mb}, now: time.Unix(1000, 0)}
	h.g = New(Options{
		Log:    slog.New(slog.NewTextHandler(&h.log, nil)),
		Source: h.fake, Runtime: h.fake,
		Trim: func() { h.trimmed++ },
		Shed: func(need int64) int { h.shedFor = append(h.shedFor, need); return 3 },
		now:  func() time.Time { return h.now },
	})
	return h
}

// tick is one look, a second after the last.
func (h *harness) tick() {
	h.now = h.now.Add(time.Second)
	h.g.Check()
}

func (h *harness) lines(level, about string) int {
	n := 0
	for _, l := range strings.Split(h.log.String(), "\n") {
		if strings.Contains(l, "level="+level) && strings.Contains(l, about) {
			n++
		}
	}
	return n
}

// The thresholds are a fifth and a sixteenth of the limit: 200 and 62.5 of
// 1000 here.
func TestAdmissionIsRefusedWhenTight(t *testing.T) {
	h := newHarness(t)
	if !h.g.Admit() {
		t.Fatal("a guard that has not looked yet must admit")
	}
	h.tick()
	if h.g.State() != Normal || !h.g.Admit() {
		t.Fatalf("with 800 of 1000 left: state %v", h.g.State())
	}
	h.left = 199 * mb
	h.tick()
	if h.g.State() != Tight {
		t.Fatalf("with 199 of 1000 left: state %v, want tight", h.g.State())
	}
	if h.g.Admit() || h.g.Admit() {
		t.Fatal("a tight guard admitted a connection")
	}
	if h.trimmed != 1 {
		t.Fatalf("trimmed %d times on entering tight, want 1", h.trimmed)
	}
	if got := h.g.Snapshot().Refused; got != 2 {
		t.Fatalf("refused %d, want 2", got)
	}
	// Said once, however long it lasts.
	for range 10 {
		h.tick()
	}
	if n := h.lines("WARN", "memory is tight"); n != 1 {
		t.Fatalf("%d warnings for one tight spell, want 1\n%s", n, h.log.String())
	}
	if len(h.shedFor) != 0 {
		t.Fatal("connections were shed while only tight")
	}
}

// Between two looks no more are admitted than there is room for above the
// tight threshold, so a crowd cannot run past it in one second.
func TestAdmissionIsBoundedBetweenLooks(t *testing.T) {
	h := newHarness(t)
	h.left = 200*mb + 10*PerConnection
	h.tick()
	admitted := 0
	for range 100 {
		if h.g.Admit() {
			admitted++
		}
	}
	if admitted != 10 {
		t.Fatalf("admitted %d with room for 10", admitted)
	}
	if h.g.State() != Normal {
		t.Fatalf("state %v", h.g.State())
	}
	h.tick()
	if !h.g.Admit() {
		t.Fatal("the allowance was not renewed at the next look")
	}
}

// Garbage the runtime could give back is not shortage: only what would be
// left after a collection counts.
func TestGarbageIsNotShortage(t *testing.T) {
	h := newHarness(t)
	h.left, h.held, h.floor = 100*mb, 600*mb, 300*mb // 400 after a collection
	h.tick()
	if h.g.State() != Normal {
		t.Fatalf("state %v with 300 MB of garbage to give back", h.g.State())
	}
	// And a figure that looks short is made exact by a collection before
	// anyone is refused on it.
	h.left, h.floor = 100*mb, 590*mb
	h.onCollect = func(f *fake) { f.floor, f.onCollect = 300*mb, nil }
	h.now = h.now.Add(time.Minute)
	h.tick()
	if h.collected == 0 || h.g.State() != Normal {
		t.Fatalf("collected %d times, state %v: want a collection and normal", h.collected, h.g.State())
	}
}

func TestCriticalShedsUntilClear(t *testing.T) {
	h := newHarness(t)
	h.tick()
	h.left = 50 * mb
	h.tick()
	if h.g.State() != Critical {
		t.Fatalf("with 50 of 1000 left: state %v, want critical", h.g.State())
	}
	if h.g.Admit() {
		t.Fatal("a critical guard admitted a connection")
	}
	// Asked for enough to be a quarter clear of the threshold.
	want := int64(1000*mb/16) + int64(1000*mb/16)/4 - 50*mb
	if len(h.shedFor) != 1 || h.shedFor[0] != want {
		t.Fatalf("shed asked for %v, want [%d]", h.shedFor, want)
	}
	h.tick()
	if len(h.shedFor) != 2 {
		t.Fatalf("still critical, and shed was asked %d times in two looks", len(h.shedFor))
	}
	if got := h.g.Snapshot().Shed; got != 6 {
		t.Fatalf("counted %d shed, want 6", got)
	}
	// Memory returns part of the way: no more shedding, still refusing.
	h.left = 150 * mb
	for range calmChecks {
		if h.g.State() != Critical {
			t.Fatal("left critical before it had been clear for a few looks")
		}
		h.tick()
	}
	if h.g.State() != Tight || h.g.Admit() {
		t.Fatalf("state %v, want tight and refusing", h.g.State())
	}
	if len(h.shedFor) != 2 {
		t.Fatalf("shed %d times, want none after memory returned", len(h.shedFor))
	}
	if h.lines("WARN", "memory is critical") != 1 || h.lines("INFO", "no longer critical") != 1 {
		t.Fatalf("want one warning in and one info out:\n%s", h.log.String())
	}
}

func TestRecoveryWhenMemoryReturns(t *testing.T) {
	h := newHarness(t)
	h.left = 150 * mb
	h.tick()
	if h.g.State() != Tight {
		t.Fatalf("state %v", h.g.State())
	}
	// Just over the threshold is not enough: a quarter past it is.
	h.left = 220 * mb
	for range 3 * calmChecks {
		h.tick()
	}
	if h.g.State() != Tight {
		t.Fatal("left tight at 220, inside the margin of 250")
	}
	h.left = 300 * mb
	for range calmChecks - 1 {
		h.tick()
	}
	if h.g.State() != Tight {
		t.Fatal("left tight before it had been clear for a few looks")
	}
	h.tick()
	if h.g.State() != Normal || !h.g.Admit() {
		t.Fatalf("state %v, want normal and admitting", h.g.State())
	}
	if h.lines("INFO", "back to normal") != 1 {
		t.Fatalf("want one info line on leaving:\n%s", h.log.String())
	}
}

func TestInactiveWhenNoLimitCanBeRead(t *testing.T) {
	h := newHarness(t)
	h.unreadable = true
	h.left = 0
	for range 3 {
		h.tick()
	}
	if h.g.State() != Normal || !h.g.Admit() || h.g.Snapshot().Active {
		t.Fatal("a guard that can read nothing must do nothing")
	}
	if h.limit != 0 || h.collected != 0 || h.trimmed != 0 {
		t.Fatal("a guard that can read nothing touched the runtime")
	}
	if h.lines("INFO", "the guard is off") != 1 {
		t.Fatalf("want it said once:\n%s", h.log.String())
	}
	// No source at all is no guard, and a nil guard admits.
	var none *Guard = New(Options{})
	if none != nil || !none.Admit() || none.State() != Normal {
		t.Fatal("no source must give a nil guard that admits")
	}
	none.Check()
}

// The runtime is told to stay where holding that much leaves the critical
// reserve free.
func TestRuntimeLimitFollowsTheBudget(t *testing.T) {
	h := newHarness(t)
	h.tick()
	if want := int64(100*mb + 800*mb - 1000*mb/16); h.limit != want {
		t.Fatalf("limit %d, want %d", h.limit, want)
	}
	// Something else on the machine takes 300 MB: the limit comes down.
	h.left = 500 * mb
	h.tick()
	if want := int64(100*mb + 500*mb - 1000*mb/16); h.limit != want {
		t.Fatalf("limit %d, want %d", h.limit, want)
	}
	// An operator's own limit is left alone.
	k := newHarness(t)
	k.g.opts.KeepLimit = true
	k.tick()
	if k.limit != 0 {
		t.Fatal("the runtime's limit was set over the operator's")
	}
}

// tree writes files under a made-up root.
func tree(t *testing.T, files map[string]string) string {
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const meminfo = "MemTotal:         980000 kB\nMemFree:           50000 kB\nMemAvailable:     700000 kB\nBuffers: 1 kB\n"

func TestSourceReadsTheMachine(t *testing.T) {
	// A systemd service with no limit of its own: every memory.max says
	// "max", so the machine is the limit.
	root := tree(t, map[string]string{
		"proc/meminfo":     meminfo,
		"proc/self/cgroup": "0::/system.slice/churr.service\n",
		"sys/fs/cgroup/system.slice/churr.service/memory.max":     "max\n",
		"sys/fs/cgroup/system.slice/churr.service/memory.high":    "max\n",
		"sys/fs/cgroup/system.slice/churr.service/memory.current": "500000000\n",
		"sys/fs/cgroup/system.slice/memory.max":                   "max\n",
		"sys/fs/cgroup/system.slice/memory.current":               "600000000\n",
	})
	r, ok := newProcSource(root, 0).Read()
	if !ok || r.Total != 980000<<10 || r.Headroom != 700000<<10 {
		t.Fatalf("got %+v %v", r, ok)
	}
}

func TestSourceReadsCgroupV2(t *testing.T) {
	// MemoryHigh on the service and a larger MemoryMax on the slice
	// above: the lower holds, and file pages not in use are not counted.
	root := tree(t, map[string]string{
		"proc/meminfo":     meminfo,
		"proc/self/cgroup": "0::/system.slice/churr.service\n",
		"sys/fs/cgroup/system.slice/churr.service/memory.max":     "max\n",
		"sys/fs/cgroup/system.slice/churr.service/memory.high":    "419430400\n",
		"sys/fs/cgroup/system.slice/churr.service/memory.current": "314572800\n",
		"sys/fs/cgroup/system.slice/churr.service/memory.stat":    "anon 200000000\ninactive_file 104857600\nactive_file 5\n",
		"sys/fs/cgroup/system.slice/memory.max":                   "838860800\n",
		"sys/fs/cgroup/system.slice/memory.current":               "314572800\n",
	})
	r, ok := newProcSource(root, 0).Read()
	if !ok || r.Total != 400*mb || r.Headroom != 200*mb || !strings.Contains(r.From, "cgroup") {
		t.Fatalf("got %+v %v", r, ok)
	}
}

func TestSourceReadsCgroupV1AndContainers(t *testing.T) {
	// In a container the path is the host's and is not there: the top
	// directory is the container's own.
	root := tree(t, map[string]string{
		"proc/meminfo":     meminfo,
		"proc/self/cgroup": "12:cpu,cpuacct:/docker/abc\n9:memory:/docker/abc\n",
		"sys/fs/cgroup/memory/memory.limit_in_bytes": "536870912\n",
		"sys/fs/cgroup/memory/memory.usage_in_bytes": "136314880\n",
		"sys/fs/cgroup/memory/memory.stat":           "cache 1\ntotal_inactive_file 31457280\n",
	})
	r, ok := newProcSource(root, 0).Read()
	if !ok || r.Total != 512*mb || r.Headroom != 412*mb {
		t.Fatalf("got %+v %v", r, ok)
	}
	// Version 1 with no limit set holds a number near the top of an int64.
	root = tree(t, map[string]string{
		"proc/meminfo":     meminfo,
		"proc/self/cgroup": "9:memory:/\n",
		"sys/fs/cgroup/memory/memory.limit_in_bytes": "9223372036854771712\n",
		"sys/fs/cgroup/memory/memory.usage_in_bytes": "136314880\n",
	})
	if r, ok := newProcSource(root, 0).Read(); !ok || r.Total != 980000<<10 {
		t.Fatalf("got %+v %v", r, ok)
	}
}

func TestSourceOverrideAndNothing(t *testing.T) {
	root := tree(t, map[string]string{
		"proc/meminfo":    meminfo,
		"proc/self/statm": "50000 25600 3000 1 0 20000 0\n",
	})
	s := newProcSource(root, 300*mb)
	s.pageSize = 4096
	r, ok := s.Read()
	if !ok || r.Total != 300*mb || r.Headroom != 200*mb || r.From != EnvLimit {
		t.Fatalf("got %+v %v", r, ok)
	}
	// Nothing to read at all: no guard.
	if _, ok := newProcSource(t.TempDir(), 0).Read(); ok {
		t.Fatal("an empty system gave a reading")
	}
}

func TestParseLimit(t *testing.T) {
	for in, want := range map[string]int64{"700M": 700 * mb, "700MiB": 700 * mb, "1.5g": 3 << 29, "1048576": mb, " 64 mb ": 64 * mb, "": 0} {
		if got, off, err := ParseLimit(in); err != nil || off || got != want {
			t.Errorf("%q: got %d %v %v, want %d", in, got, off, err, want)
		}
	}
	if _, off, err := ParseLimit("off"); !off || err != nil {
		t.Error("off was not understood")
	}
	for _, in := range []string{"lots", "-5M", "5T", "0"} {
		if _, _, err := ParseLimit(in); err == nil {
			t.Errorf("%q was taken for a size", in)
		}
	}
}

// The real runtime's account adds up: it holds something, and would hold
// no more after a collection than it does now.
func TestGoRuntimeHeap(t *testing.T) {
	keep := make([]byte, 8*mb)
	hp := goRuntime{}.Heap()
	if hp.Held <= 0 || hp.Floor <= 0 || hp.Floor > hp.Held+hp.Held/10 {
		t.Fatalf("held %d, floor %d", hp.Held, hp.Floor)
	}
	_ = keep[0]
}
