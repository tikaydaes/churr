package live

import "testing"

// The controller is fed what a machine would show: a network card that
// carries `ceiling` packets a second, in front of it a queue that holds
// `room` and drops what does not fit, and a server that wants to make
// `demand` writes a second. sentAgain is loss elsewhere, on the path or at
// the receivers: TCP sends those segments again, and nothing in this
// machine's queues shows it, so the controller is not told.
type fakeMachine struct {
	demand, ceiling, room float64
	sentAgain             float64
	queued                float64
}

func (m *fakeMachine) run(c *paceControl, seconds int) (rate float64, said []string) {
	for i := 0; i < seconds; i++ {
		writes := m.demand
		if c.rate > 0 {
			writes = min(m.demand, c.rate)
		}
		// Segments lost elsewhere are sent again: more packets, which
		// the card must carry too.
		m.queued += writes*(1+m.sentAgain) - m.ceiling
		m.queued = max(m.queued, 0)
		dropped := max(m.queued-m.room, 0)
		m.queued -= dropped
		var word string
		rate, word = c.step(writes, dropped, m.queued)
		if word != "" {
			said = append(said, word)
		}
	}
	return rate, said
}

// A full transmit queue on this machine sets a pace below what the card
// carries, once, and it stays there.
func TestLocalQueuePressureSetsAPace(t *testing.T) {
	var c paceControl
	m := &fakeMachine{demand: 284000, ceiling: 225000, room: 10240}
	rate, said := m.run(&c, 300)
	if rate <= 0 || rate > 225000 || rate < 150000 {
		t.Fatalf("with 284,000 writes wanted and 225,000 carried, the pace settled at %.0f; it said %v", rate, said)
	}
	if len(said) == 0 || said[0] != "set" || len(said) > 3 {
		t.Fatalf("the pace was changed %v: want it set once and then left near where it works", said)
	}
	// Set in a second in which little had been written yet (a burst's
	// first moment), it still finds what the card carries, and soon.
	c = paceControl{}
	c.step(60000, 30000, 2000)
	if rate, _ = m.run(&c, 60); rate < 150000 || rate > 225000 {
		t.Fatalf("set at the start of a burst, the pace is %.0f a minute later", rate)
	}
	before := rate
	rate, said = m.run(&c, 200)
	if rate > 225000 || rate < before || len(said) != 0 {
		t.Fatalf("after settling at %.0f the pace went to %.0f, saying %v", before, rate, said)
	}
	// Long at ease, it tries higher once more, and is put back. (It may
	// be caught in the seconds it is over.)
	rate, _ = m.run(&c, paceForget+60)
	if rate > 225000*1.15 || rate < 150000 {
		t.Fatalf("after trying above, the pace is %.0f", rate)
	}
}

// A queue too deep to drop anything, standing full, is the same sign.
func TestAStandingQueueSetsAPace(t *testing.T) {
	var c paceControl
	m := &fakeMachine{demand: 284000, ceiling: 225000, room: 400000}
	rate, said := m.run(&c, 120)
	if rate <= 0 || rate > 225000 || len(said) == 0 || said[0] != "set" {
		t.Fatalf("with a deep queue filling, the pace is %.0f (%v)", rate, said)
	}
	// A burst that passes through within the second is not.
	c = paceControl{}
	for i := 0; i < 600; i++ {
		waiting := 0.0
		if i%3 == 0 {
			waiting = 9000 // seen once, gone by the next look
		}
		if rate, _ := c.step(250000, 0, waiting); rate != 0 {
			t.Fatalf("a queue that empties between looks set a pace of %.0f", rate)
		}
	}
}

// Segments sent again because of loss on the path or at the receivers,
// with this machine's queues at ease, never set a pace, however many: on a
// small server or a large one.
func TestLossElsewhereSetsNoPace(t *testing.T) {
	for _, demand := range []float64{5000, 22700, 100000, 200000} {
		var c paceControl
		m := &fakeMachine{demand: demand, ceiling: 1e9, room: 10240, sentAgain: 0.3}
		if rate, said := m.run(&c, 900); rate != 0 || len(said) != 0 {
			t.Fatalf("a server making %.0f writes a second, losing nothing in its own queues while 30%% of segments are sent again, was paced at %.0f (%v)", demand, rate, said)
		}
	}
	// And one that drops a few packets while writing little is left
	// alone too: that is not this program's doing.
	var c paceControl
	for i := 0; i < 600; i++ {
		if rate, _ := c.step(3000, 500, 0); rate != 0 {
			t.Fatalf("a server making 3,000 writes a second was paced at %.0f", rate)
		}
	}
}

func TestPaceHasAFloorAndIsLifted(t *testing.T) {
	// A card that carries next to nothing: the pace stops at the floor.
	var c paceControl
	m := &fakeMachine{demand: 100000, ceiling: 1000, room: 10240}
	rate, _ := m.run(&c, 3600)
	if rate != paceFloor {
		t.Fatalf("the pace is %.0f, want the floor of %d", rate, paceFloor)
	}
	// The load goes away: the pace is let go of.
	m = &fakeMachine{demand: 1000, ceiling: 1e9, room: 10240}
	rate, said := m.run(&c, paceIdle+10)
	if rate != 0 || len(said) == 0 || said[len(said)-1] != "lifted" {
		t.Fatalf("with nothing to pace the pace is still %.0f (%v)", rate, said)
	}
}
