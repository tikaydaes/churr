package sqlstore

import (
	"strconv"
	"time"

	"github.com/tikaydaes/churr/internal/timing"
)

// Where the time of a transaction goes, for the summary the timing package
// logs when it is on (CHURR_DEBUG_TIMING). For each transaction: `begin`
// (taking a connection from the pool and BEGIN), then for each statement in
// turn `gapN` (from the answer to the one before it, or to BEGIN, until
// this one was sent: time the transaction was open with the server doing
// something else) and `stmtN` (sent until answered), then `gap.commit`,
// `commit` and `total`. They are logged under `seqtx.` for a transaction
// that took a conversation's seq, where everything from stmt of the seq to
// the commit is time other writers of that conversation wait, and under
// `tx.` for any other. A statement outside a transaction is `db.single`,
// which includes its wait for a connection.

// txTimes is the timings of one transaction, logged when it ends.
type txTimes struct {
	last  time.Time
	n     int
	parts []txPart
}

type txPart struct {
	name string
	d    time.Duration
}

func ordinal(n int) string {
	if n > 6 {
		return "7+"
	}
	return strconv.Itoa(n)
}

// timed is called as a statement is sent and returns what to call when it
// has been answered, or nil when timing is off.
func (b bound) timed() func() {
	if !timing.On() {
		return nil
	}
	start := time.Now()
	if b.st == nil {
		return func() { timing.Observe("db.single", time.Since(start)) }
	}
	tm := b.st.tm
	if tm == nil {
		return nil
	}
	tm.n++
	n := ordinal(tm.n)
	tm.parts = append(tm.parts, txPart{"gap" + n, start.Sub(tm.last)})
	return func() {
		now := time.Now()
		tm.parts = append(tm.parts, txPart{"stmt" + n, now.Sub(start)})
		tm.last = now
	}
}

func (tm *txTimes) report(marks *touches, began time.Time) {
	prefix := "tx."
	if len(marks.taken) > 0 {
		prefix = "seqtx."
	}
	for _, p := range tm.parts {
		timing.Observe(prefix+p.name, p.d)
	}
	timing.Observe(prefix+"total", time.Since(began))
}

// PoolNumbers is what database/sql says of the connections writes and
// transactions use (on PostgreSQL, everything): the most it opens, how many
// are in use and idle now, and since the start how many times a caller had
// to wait for one and for how long in all.
func (d *DB) PoolNumbers() (max, inUse, idle int, waits int64, waited time.Duration) {
	st := d.w.Stats()
	return st.MaxOpenConnections, st.InUse, st.Idle, st.WaitCount, st.WaitDuration
}
