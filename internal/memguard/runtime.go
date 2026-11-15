package memguard

import (
	"runtime/debug"
	"runtime/metrics"
	"sync"
)

// goRuntime is the real Go runtime. It reads runtime/metrics, which does
// not stop the program as runtime.ReadMemStats does.
type goRuntime struct{}

var heapSamples = struct {
	mu sync.Mutex
	s  []metrics.Sample
	// The collection count and the bytes allocated when it was last seen
	// to change: what has been allocated since is not yet known to be
	// garbage, and is counted as in use.
	cycles, allocsThen uint64
}{s: []metrics.Sample{
	{Name: "/memory/classes/total:bytes"},
	{Name: "/memory/classes/heap/released:bytes"},
	{Name: "/memory/classes/heap/objects:bytes"},
	{Name: "/memory/classes/heap/free:bytes"},
	{Name: "/memory/classes/heap/unused:bytes"},
	{Name: "/gc/heap/live:bytes"},
	{Name: "/gc/heap/allocs:bytes"},
	{Name: "/gc/cycles/total:gc-cycles"},
}}

func (goRuntime) Heap() Heap {
	h := &heapSamples
	h.mu.Lock()
	defer h.mu.Unlock()
	metrics.Read(h.s)
	v := func(i int) uint64 {
		if h.s[i].Value.Kind() != metrics.KindUint64 {
			return 0
		}
		return h.s[i].Value.Uint64()
	}
	total, released, objects, free, unused, live, allocs, cycles := v(0), v(1), v(2), v(3), v(4), v(5), v(6), v(7)
	if cycles != h.cycles || h.allocsThen > allocs {
		h.cycles, h.allocsThen = cycles, allocs
	}
	held := int64(total - released)
	// What is not heap: goroutine stacks, the runtime's own structures.
	other := held - int64(objects+free+unused)
	// In use: what the last collection found, and everything since,
	// which cannot be more than the objects there are. A twentieth is
	// added for the room objects leave unused between them.
	inUse := min(live+(allocs-h.allocsThen), objects)
	return Heap{Held: held, Floor: other + int64(inUse) + int64(inUse)/20}
}

func (goRuntime) Collect() { debug.FreeOSMemory() }

func (goRuntime) SetLimit(bytes int64) { debug.SetMemoryLimit(bytes) }
