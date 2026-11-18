package storetest

import (
	"testing"

	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("draws", testDraws) }

// The pairs the VRF key has drawn for: kept with who asked, a pair taken
// again keeps its first who, and the seconds before one given are forgotten.
func testDraws(t *testing.T, s store.Store) {
	const a, b = "01a1ee00-0000-7000-8000-0000000000c1", "01a1ee00-0000-7000-8000-0000000000c2"
	must(t, s.Draws().Take(ctx, store.DrawUsed{Second: 100, Nonce: a, Who: "ann"}))
	must(t, s.Draws().Take(ctx, store.DrawUsed{Second: 100, Nonce: a, Who: "bob"}))
	must(t, s.Draws().Take(ctx, store.DrawUsed{Second: 101, Nonce: b, Who: "bob"}))
	must(t, s.Draws().Take(ctx, store.DrawUsed{Second: 120, Nonce: a, Who: "cy"}))
	all, err := s.Draws().All(ctx)
	must(t, err)
	if len(all) != 3 || all[0] != (store.DrawUsed{Second: 100, Nonce: a, Who: "ann"}) || all[1].Second != 101 || all[2].Who != "cy" {
		t.Fatalf("the pairs kept: %+v", all)
	}
	must(t, s.Draws().Forget(ctx, 101))
	all, err = s.Draws().All(ctx)
	must(t, err)
	if len(all) != 2 || all[0].Second != 101 {
		t.Fatalf("after forgetting the seconds before 101: %+v", all)
	}
}
