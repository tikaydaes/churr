package sqlstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"time"

	"github.com/tikaydaes/churr/internal/timing"
)

// Transactions on a database across a network, in as few round trips as
// they can be.
//
// A round trip costs more than the network: the goroutine that made it gives
// the processor away, and on a busy one-processor server it waits its turn
// to get it back, tens of milliseconds behind a fan-out, with the
// transaction open, its connection out of the pool, and, once it has taken
// a conversation's seq, every other writer of that conversation waiting.
// Measured so, a post of four round trips inside its transaction held the
// row for two of those waits and the connection for four, and the pool ran
// dry.
//
// So where the dialect can send several statements as one batch
// (Dialect.Batch; PostgreSQL), a transaction here is not database/sql's. It
// runs on one connection and:
//
//   - BEGIN is not sent by itself. It goes with the first statement when
//     that is the taking of a seq (Streams.TakeSeq, TakeMessageSeq), which
//     is how a post and most other changes to a conversation start; ahead
//     of any other first statement it is sent alone.
//   - Writes held back (kept.go), which now include the event's row, go
//     with COMMIT as one batch.
//
// A post is then two round trips: BEGIN with the seq, and the message, its
// mentions and its event with COMMIT. The row is held from the first
// answer until the second batch is run, which is one wait for the
// processor and not two or three. A transaction that sent nothing sends
// nothing at all.
//
// SQLite makes no round trips and keeps database/sql's transaction.

// BatchStmt is one statement of a batch, written with the database's own
// placeholders. Scan, if set, takes the columns of the one row it returns;
// no row is sql.ErrNoRows.
type BatchStmt struct {
	SQL  string
	Args []any
	Scan []any
}

// pipe is the connection of one such transaction.
type pipe struct {
	conn  *sql.Conn
	begun bool
}

// begin sends BEGIN alone, ahead of a first statement that cannot carry it.
func (b bound) begin(ctx context.Context) {
	if b.st == nil || b.st.pipe == nil || b.st.pipe.begun {
		return
	}
	b.st.pipe.begun = true
	b.rt.Add(1)
	done := b.timed()
	if _, err := b.ex.ExecContext(ctx, "BEGIN"); err != nil && b.st.failed == nil {
		b.st.failed = err
	}
	if done != nil {
		done()
	}
}

// seqRow runs the statement that takes a conversation's seq and scans its
// one row. As the first statement of a piped transaction it carries BEGIN.
func (q *queries) seqRow(ctx context.Context, query string, args []any, dest ...any) error {
	b, ok := q.wr.(bound)
	if !ok || b.st == nil || b.st.pipe == nil || b.st.pipe.begun || len(b.st.held) > 0 {
		return q.wr.QueryRowContext(ctx, query, args...).Scan(dest...)
	}
	b.st.pipe.begun = true
	b.count(query)
	b.rt.Add(1)
	done := b.timed()
	err := q.db.dialect.Batch(ctx, b.st.pipe.conn, []BatchStmt{{SQL: "BEGIN"}, {SQL: rebind(true, query), Args: args, Scan: dest}})
	if done != nil {
		done()
	}
	return err
}

// pipedTx is DB.tx on a dialect that batches.
func (d *DB) pipedTx(ctx context.Context, fn func(q *queries) error) (err error) {
	var tm *txTimes
	began := timing.Now()
	conn, err := d.w.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close() // back to the pool
	if !began.IsZero() {
		tm = &txTimes{last: time.Now()}
		tm.parts = append(tm.parts, txPart{"begin", tm.last.Sub(began)})
	}
	p := &pipe{conn: conn}
	marks := &touches{tm: tm, pipe: p}
	b := bound{ex: conn, pg: true, n: &d.statements, tr: &d.trace, rt: &d.roundTrips, tx: true, st: marks}
	ended := false
	rollback := func() {
		ended = true
		if !p.begun {
			return
		}
		// The request may be over, and its context with it; the
		// transaction must still be ended before the connection is
		// used again. If it cannot be, the connection is thrown away.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		d.roundTrips.Add(1)
		if _, rerr := conn.ExecContext(rctx, "ROLLBACK"); rerr != nil {
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}
	defer func() {
		// A panic in fn must not leave the transaction open.
		if !ended {
			rollback()
		}
	}()
	err = fn(&queries{db: d, rd: b, wr: b, inTx: true, touched: marks})
	if marks.failed != nil {
		// A held write, or BEGIN, was refused. Whatever failed after it
		// failed because of it, so this is the error the caller is given.
		err = d.mapErr(marks.failed)
	}
	if err != nil {
		rollback()
		return err
	}
	// What was held back goes with COMMIT; and with BEGIN, if nothing
	// was sent before.
	var batch []BatchStmt
	if len(marks.held) > 0 {
		if !p.begun {
			batch = append(batch, BatchStmt{SQL: "BEGIN"})
			p.begun = true
		}
		q, args := together(marks.held, nil)
		marks.held = nil
		b.count(q)
		batch = append(batch, BatchStmt{SQL: rebind(true, q), Args: args})
	}
	if !p.begun {
		// Nothing was sent: there is nothing to commit.
		ended = true
		d.roster.apply(marks)
		d.kept.apply(marks)
		return nil
	}
	batch = append(batch, BatchStmt{SQL: "COMMIT"})
	var committing time.Time
	if tm != nil {
		committing = time.Now()
		tm.parts = append(tm.parts, txPart{"gap.commit", committing.Sub(tm.last)})
	}
	d.roundTrips.Add(1)
	if len(batch) == 1 {
		_, err = conn.ExecContext(ctx, "COMMIT")
	} else {
		err = d.dialect.Batch(ctx, conn, batch)
	}
	if err != nil {
		rollback()
		return d.mapErr(err)
	}
	ended = true
	if tm != nil {
		tm.parts = append(tm.parts, txPart{"commit", time.Since(committing)})
		tm.report(marks, began)
	}
	d.roster.apply(marks)
	d.kept.apply(marks)
	return nil
}

// RoundTrips is how many times this store has sent something to its
// database and waited for the answer, since it was opened: a statement, a
// batch of them, BEGIN, COMMIT. Tests hold a post to a number of them.
func (d *DB) RoundTrips() int64 { return d.roundTrips.Load() }
