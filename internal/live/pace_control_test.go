package live

import "testing"

// The controller is fed what a machine would show: a network card that
// passes `ceiling` packets a second and loses the rest, which TCP sends
// again; and a server that wants to make `demand` writes a second.
func runFake(c *paceControl, seconds int, demand, ceiling, otherLoss float64) (rate float64, said []string) {
	for i := 0; i < seconds; i++ {
		writes := demand
		if c.rate > 0 {
			writes = min(demand, c.rate)
		}
		again := otherLoss * writes
		if writes > ceiling {
			again += writes - ceiling
		}
		var word string
		rate, word = c.step(writes, writes+again, again)
		if word != "" {
			said = append(said, word)
		}
	}
	return rate, said
}

func TestPaceIsFoundBelowTheCeiling(t *testing.T) {
	var c paceControl
	rate, said := runFake(&c, 300, 284000, 225000, 0)
	if rate <= 0 || rate > 225000 || rate < 150000 {
		t.Fatalf("with 284,000 writes wanted and 225,000 passed, the pace settled at %.0f; it said %v", rate, said)
	}
	if len(said) == 0 || said[0] != "set" || len(said) > 3 {
		t.Fatalf("the pace was changed %v: want it set once and then left near where it works", said)
	}
	// It stays there: no loss again and again from trying above.
	before := rate
	rate, said = runFake(&c, 200, 284000, 225000, 0)
	if rate > 225000 || rate < before || len(said) != 0 {
		t.Fatalf("after settling at %.0f the pace went to %.0f, saying %v", before, rate, said)
	}
	// Long without loss, it tries higher once more, and is put back.
	rate, _ = runFake(&c, paceForget+60, 284000, 225000, 0)
	// (It may be caught in the second it is one step over.)
	if rate > 225000*paceUp || rate < 150000 {
		t.Fatalf("after trying above, the pace is %.0f", rate)
	}
}

func TestNoPaceWhereNoneIsNeeded(t *testing.T) {
	// A small server whose clients' networks lose packets.
	var c paceControl
	if rate, said := runFake(&c, 600, 5000, 1e9, 0.05); rate != 0 || len(said) != 0 {
		t.Fatalf("a server making 5,000 writes a second was paced at %.0f (%v)", rate, said)
	}
	// A large one that loses nothing.
	c = paceControl{}
	if rate, said := runFake(&c, 600, 300000, 1e9, 0.001); rate != 0 || len(said) != 0 {
		t.Fatalf("a server losing nothing was paced at %.0f (%v)", rate, said)
	}
}

func TestPaceNeverStallsAndIsLifted(t *testing.T) {
	// Loss that no pace cures (the clients' networks), on a busy server:
	// the pace is not chased down. It is lowered only while the server
	// writes at it, and never below the floor.
	var c paceControl
	rate, _ := runFake(&c, 3600, 100000, 1e9, 0.05)
	if rate < paceFloor {
		t.Fatalf("the pace fell to %.0f, under the floor of %d", rate, paceFloor)
	}
	// The load goes away: the pace is let go of.
	rate, said := runFake(&c, paceIdle+10, 1000, 1e9, 0)
	if rate != 0 || len(said) == 0 || said[len(said)-1] != "lifted" {
		t.Fatalf("with nothing to pace the pace is still %.0f (%v)", rate, said)
	}
}
