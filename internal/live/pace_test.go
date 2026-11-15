package live

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The pace lets a burst go at once and then holds everyone together to its
// rate, however many ask; with no rate nobody is held.
func TestWritePace(t *testing.T) {
	p := newPacer(20000, 500)
	began := time.Now()
	var taken atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for taken.Load() < 4500 {
				n := p.take(64)
				if n < 1 || n > 64 {
					t.Errorf("take(64) allowed %d writes", n)
					return
				}
				taken.Add(int64(n))
			}
		}()
	}
	wg.Wait()
	// 500 at once and 4,000 more at 20,000 a second: 200 ms.
	if took := time.Since(began); took < 150*time.Millisecond || took > 2*time.Second {
		t.Fatalf("%d writes at 20,000 a second after a burst of 500 took %v, want about 200ms", taken.Load(), took)
	}
	p.rate.Store(0)
	began = time.Now()
	for i := 0; i < 100000; i++ {
		if p.take(64) != 64 {
			t.Fatal("held with no pace set")
		}
	}
	if took := time.Since(began); took > time.Second {
		t.Fatalf("with no pace, asking took %v", took)
	}
}
