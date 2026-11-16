package sched

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// TestAroundCostsNothingForTheUnwatched: presence changes all the time, and
// a change for someone nobody watches must do no work beyond one lookup in
// memory. The service here has no store, no permissions and no chat: if the
// change of an unwatched account touched any of them it would panic.
func TestAroundCostsNothingForTheUnwatched(t *testing.T) {
	s := New(Deps{})
	watched, other := model.NewID[model.AccountID](), model.NewID[model.AccountID]()
	s.watches.add(watched, "w1")
	for i := 0; i < 100000; i++ {
		s.Around(other)
	}
	if n := s.watches.looked.Load(); n != 0 {
		t.Fatalf("%d changes of an unwatched account were looked into", n)
	}
	allocs := testing.AllocsPerRun(1000, func() { s.Around(other) })
	if allocs != 0 {
		t.Fatalf("a change of an unwatched account allocated %v times", allocs)
	}
	// The index forgets an account once its last watch goes.
	s.watches.remove(watched, "w1")
	if s.watches.has(watched) {
		t.Fatal("an account with no watches is still in the index")
	}
}
