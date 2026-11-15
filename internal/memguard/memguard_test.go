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
	// Ninety were turned away without the state changing: the log says
	// so at the next look, once, and the snapshot the Overview reads
	// says so for as long as it goes on.
	h.tick()
	if n := h.lines("WARN", "are being refused"); n != 1 || !strings.Contains(h.log.String(), "refused_so_far=90") {
		t.Fatalf("%d warnings for refusals in the normal state, want 1 with the count\n%s", n, h.log.String())
	}
	if s := h.g.Snapshot(); !s.Refusing || s.State != Normal || s.Refused != 90 {
		t.Fatalf("snapshot %+v, want refusing, normal, 90 refused", s)
	}
	if !h.g.Admit() {
		t.Fatal("the allowance was not renewed at the next look")
	}
	for range 30 {
		for range 12 {
			h.g.Admit() // more than there is room for, each second: 61 refused in all
		}
		h.tick()
	}
	if n := h.lines("WARN", "are being refused"); n != 1 {
		t.Fatalf("%d warnings for one spell of refusing, want 1", n)
	}
	// It stops; a minute later that is said, with the total.
	for range 59 {
		h.tick()
	}
	if h.lines("INFO", "no live connection has been refused") != 0 || !h.g.Snapshot().Refusing {
		t.Fatal("the spell was called over before a minute without refusals")
	}
	h.tick()
	if h.lines("INFO", "no live connection has been refused") != 1 || !strings.Contains(h.log.String(), "refused=151 ") || h.g.Snapshot().Refusing {
		t.Fatalf("want one line with the total when the refusing has stopped:\n%s", h.log.String())
	}
}

// Whatever the state, a refusal is in the log by the next look.
func TestEveryRefusalIsLogged(t *testing.T) {
	for _, left := range []int64{200*mb + PerConnection, 150 * mb, 50 * mb} {
		h := newHarness(t)
		h.left = left
		h.tick()
		for h.g.Admit() {
		}
		h.tick()
		if h.g.Snapshot().Refused == 0 || h.lines("WARN", "are being refused") != 1 || !h.g.Snapshot().Refusing {
			t.Fatalf("with %d MB left (%v): a refusal with no line in the log\n%s", left/mb, h.g.State(), h.log.String())
		}
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

// A systemd service on cgroup v2 with MemoryMax=700M, as read off a real
// machine (Ubuntu 26.04.1, kernel 7.0, unit churr.service running as root):
// /proc/self/cgroup, the unit's memory.max, memory.high, memory.current and
// memory.stat (testdata, its first forty lines) and /proc/meminfo are that
// machine's own, just after the server started. The slice above holds
// "max", and the root cgroup has no memory.max file at all.
func TestSourceReadsASystemdUnit(t *testing.T) {
	stat, err := os.ReadFile("testdata/ubuntu2604-unit-memory.stat")
	if err != nil {
		t.Fatal(err)
	}
	unit := "sys/fs/cgroup/system.slice/churr.service/"
	files := map[string]string{
		"proc/meminfo":                              "MemTotal:         949304 kB\nMemFree:          464580 kB\nMemAvailable:     754948 kB\n",
		"proc/self/cgroup":                          "0::/system.slice/churr.service\n",
		"sys/fs/cgroup/cgroup.controllers":          "cpuset cpu io memory hugetlb pids rdma misc dmem\n",
		"sys/fs/cgroup/memory.stat":                 "anon 150000000\nfile 200000000\ninactive_file 100000000\n",
		"sys/fs/cgroup/system.slice/memory.max":     "max\n",
		"sys/fs/cgroup/system.slice/memory.high":    "max\n",
		"sys/fs/cgroup/system.slice/memory.current": "101112064\n",
		unit + "memory.max":                         "734003200\n",
		unit + "memory.high":                        "max\n",
		unit + "memory.current":                     "5689344\n",
		unit + "memory.peak":                        "5689344\n",
		unit + "memory.stat":                        string(stat),
	}
	root := tree(t, files)
	src := newProcSource(root, 0)
	r, ok := src.Read()
	// The unit's 700 MiB, not the machine's 927; used is memory.current
	// less the 32768 bytes of inactive_file.
	if !ok || r.Total != 734003200 || r.Headroom != 734003200-(5689344-32768) {
		t.Fatalf("got %+v %v, want the unit's limit of 734003200", r, ok)
	}
	if !strings.Contains(r.From, "cgroup") || !strings.Contains(r.From, "system.slice/churr.service") {
		t.Fatalf("the reading does not say where the limit was found: %q", r.From)
	}

	// The same unit as it stood under 12,000 connections: 624 MiB in
	// use of 700, and the machine with less to spare than at the start.
	// The files are read afresh at every look.
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(unit+"memory.current", "654311424\n")
	write("proc/meminfo", "MemTotal:         949304 kB\nMemFree:           60000 kB\nMemAvailable:     210000 kB\n")
	const left = 734003200 - (654311424 - 32768) // 76 MiB
	if r, ok = src.Read(); !ok || r.Total != 734003200 || r.Headroom != left {
		t.Fatalf("got %+v %v, want %d left of the unit's limit", r, ok, left)
	}

	// The guard on that reading. With nothing for the runtime to give
	// back it is tight (the line is 140 MiB) and refuses; the log names
	// the limit and where it is from, at the start and on going tight.
	rt := &fake{held: 580 * mb, floor: 580 * mb}
	var log bytes.Buffer
	g := New(Options{Log: slog.New(slog.NewTextHandler(&log, nil)), Source: src, Runtime: rt})
	g.Check()
	if g.State() != Tight || g.Admit() {
		t.Fatalf("76 MiB left of 700 and no garbage: state %v, want tight and refusing", g.State())
	}
	for _, line := range []string{"watching memory against this limit", "memory is tight"} {
		if !strings.Contains(log.String(), line) || strings.Count(log.String(), `limit="700 MiB"`) != 2 || strings.Count(log.String(), "system.slice/churr.service") != 2 {
			t.Fatalf("the log does not say %q with the limit and its place:\n%s", line, log.String())
		}
	}
	// With 200 MiB of garbage in the runtime it is not tight: 276 MiB
	// would be left after a collection. That is said once, since the
	// system's own figure is under the line.
	rt = &fake{held: 580 * mb, floor: 380 * mb}
	log.Reset()
	g = New(Options{Log: slog.New(slog.NewTextHandler(&log, nil)), Source: src, Runtime: rt})
	g.Check()
	g.Check()
	if g.State() != Normal || !g.Admit() {
		t.Fatalf("state %v with 200 MiB to give back, want normal", g.State())
	}
	if strings.Count(log.String(), "less is left than the tight line") != 1 || !strings.Contains(log.String(), `runtime_can_give_back="200 MiB"`) {
		t.Fatalf("want it said once that the system's figure is under the line:\n%s", log.String())
	}
	// And the runtime's limit holds it to the critical reserve: what it
	// holds, plus what is left, less a sixteenth of the limit.
	if want := int64(580*mb + left - 734003200/16); rt.limit != want {
		t.Fatalf("runtime limit %d, want %d", rt.limit, want)
	}
}
