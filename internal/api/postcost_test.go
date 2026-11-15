package api_test

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// What one post costs the database, statement by statement. On a database
// across a network every statement is a round trip, and the ones sent after
// the conversation's seq is taken are sent while every other writer of that
// conversation waits. So the test below holds both numbers: the statements
// of the request, and the statements under the conversation's lock.

// stmt is one statement a store sent.
type stmt struct {
	sql  string
	inTx bool
	// after says the request had already been answered: the fan-out, the
	// notifications and the link read on their own time.
	after bool
}

// tracer records what a server's store sends.
type tracer struct {
	t  testing.TB
	ts *testkit.TestServer
	db interface {
		Statements() int64
		Trace(func(stmt string, inTx bool))
	}
}

func newTracer(t testing.TB, ts *testkit.TestServer) *tracer {
	t.Helper()
	db, ok := ts.Server.Store.(interface {
		Statements() int64
		Trace(func(stmt string, inTx bool))
	})
	if !ok {
		t.Fatal("the store does not trace its statements")
	}
	return &tracer{t: t, ts: ts, db: db}
}

// settle waits until the store has been quiet for a moment, so that the
// work that follows one request is not counted against the next.
func (tr *tracer) settle() {
	for last := int64(-1); last != tr.db.Statements(); {
		last = tr.db.Statements()
		time.Sleep(150 * time.Millisecond)
	}
}

// record runs do and returns every statement sent until the store is quiet
// again, those sent after do returned marked so.
func (tr *tracer) record(do func()) []stmt {
	tr.t.Helper()
	tr.settle()
	var mu sync.Mutex
	var out []stmt
	done := false
	tr.db.Trace(func(q string, inTx bool) {
		mu.Lock()
		out = append(out, stmt{sql: strings.Join(strings.Fields(q), " "), inTx: inTx, after: done})
		mu.Unlock()
	})
	do()
	mu.Lock()
	done = true
	mu.Unlock()
	tr.settle()
	tr.db.Trace(nil)
	mu.Lock()
	defer mu.Unlock()
	return out
}

// cost is the numbers held for one piece of work.
type cost struct {
	// request is every statement sent before the answer; locked is those
	// sent from the one that takes the conversation's seq to the end of
	// its transaction, that one included; after is the rest.
	request, locked, after int
}

func costOf(list []stmt) cost {
	var c cost
	held := false
	for _, s := range list {
		if s.after {
			c.after++
			continue
		}
		c.request++
		if strings.Contains(s.sql, "SET next_seq") {
			held = true
		}
		if held && s.inTx {
			c.locked++
		}
	}
	return c
}

// show prints a recorded list when POSTCOST_LIST is set or the test fails.
func show(t *testing.T, name string, list []stmt) {
	t.Helper()
	c := costOf(list)
	t.Logf("%s: %d statements in the request, %d of them holding the conversation; %d afterwards", name, c.request, c.locked, c.after)
	if os.Getenv("POSTCOST_LIST") == "" {
		return
	}
	var b strings.Builder
	for i, s := range list {
		where := "    "
		if s.inTx {
			where = "tx  "
		}
		if s.after {
			where = "late"
		}
		q := s.sql
		if len(q) > 150 {
			q = q[:150] + "…"
		}
		fmt.Fprintf(&b, "%3d %s %s\n", i+1, where, q)
	}
	t.Logf("%s, statement by statement:\n%s", name, b.String())
}

// postCostBounds are, for each database, the most statements one post may
// send before it is answered (the fan-out's reads included, which run
// before the answer), and the most it may send from the statement that
// takes the conversation's seq to the end of that transaction: every other
// writer of the conversation waits for those, and for the commit after
// them. BEGIN and COMMIT are not counted. The numbers are what was measured
// when this was written, not a target with room in it: a post that needs
// more has grown a round trip, so find it before raising one.
//
// Before the post path was shortened the same posts took, on SQLite: 29
// (18 holding the conversation), 34 (19), 38 (23), 29 (18), 30 (18), and
// 45 (26) from a linked server.
var postCostBounds = map[string]map[string]cost{
	"sqlite": {
		"plain post":                         {request: 7, locked: 5},
		"post with a client_id":              {request: 8, locked: 5},
		"post with a mention":                {request: 12, locked: 6},
		"reply in a thread":                  {request: 14, locked: 8},
		"post in a channel of 5,000":         {request: 7, locked: 5},
		"bot post":                           {request: 7, locked: 5},
		"post arriving from a linked server": {request: 31, locked: 14},
	},
	"postgres": {
		"plain post":                         {request: 4, locked: 2},
		"post with a client_id":              {request: 5, locked: 2},
		"post with a mention":                {request: 8, locked: 2},
		"reply in a thread":                  {request: 12, locked: 6},
		"post in a channel of 5,000":         {request: 4, locked: 2},
		"bot post":                           {request: 4, locked: 2},
		"post arriving from a linked server": {request: 29, locked: 12},
	},
}

// hold measures do and fails if it costs more than its bound. The server
// has work of its own on a timer (the link's outbox, for one), and a
// statement of that landing inside a measurement is not the post's: so a
// measurement over the bound is taken again, twice at most, and the post is
// held to the cheapest.
func (tr *tracer) hold(t *testing.T, name string, do func()) {
	t.Helper()
	want, ok := postCostBounds[tr.ts.Server.Store.Dialect()][name]
	if !ok {
		t.Errorf("%s: no bound is set for it on %s", name, tr.ts.Server.Store.Dialect())
		return
	}
	var list []stmt
	var got cost
	for try := 0; try < 3; try++ {
		list = tr.record(do)
		got = costOf(list)
		if got.request <= want.request && got.locked <= want.locked {
			break
		}
	}
	show(t, name, list)
	if got.request > want.request {
		t.Errorf("%s: %d statements in the request, at most %d allowed", name, got.request, want.request)
	}
	if got.locked > want.locked {
		t.Errorf("%s: %d statements while holding the conversation, at most %d allowed", name, got.locked, want.locked)
	}
}

func TestPostCost(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	ann, bob := ts.Member("ann"), ts.Member("bob")
	room := owner.Channel("town", false, ann, bob)
	crowd(t, ts, room, "t", 750)
	large := owner.Channel("large", false, ann, bob)
	crowd(t, ts, large, "l", 5000)
	bot := owner.Bot("robot")
	owner.Add(room, "member", bot)
	tr := newTracer(t, ts)

	// Twice each to fill what is kept in memory: the first request of a
	// session also marks it seen, which has it read again.
	var first string
	for i := 0; i < 2; i++ {
		first = say(ann, room, "warm")["message"].(map[string]any)["id"].(string)
		say(ann, room, "warm @bob")
		say(bob, room, "warm too")
		say(ann, large, "warm")
		say(bot, room, "warm")
		ann.Must(201, "POST", convPath(room)+"/messages", map[string]any{"text": "warm reply", "parent_id": first})
		bob.Must(201, "POST", convPath(room)+"/messages", map[string]any{"text": "warm reply", "parent_id": first})
	}

	tr.hold(t, "plain post", func() { say(ann, room, "hello") })
	tr.hold(t, "post with a client_id", func() {
		ann.Must(201, "POST", convPath(room)+"/messages", map[string]any{"text": "hello", "client_id": string(model.NewID[model.MessageID]())})
	})
	tr.hold(t, "post with a mention", func() { say(ann, room, "hello @bob") })
	tr.hold(t, "reply in a thread", func() {
		ann.Must(201, "POST", convPath(room)+"/messages", map[string]any{"text": "a reply", "parent_id": first})
	})
	tr.hold(t, "post in a channel of 5,000", func() { say(ann, large, "hello") })
	tr.hold(t, "bot post", func() { say(bot, room, "beep") })

	// The next most frequent writes, shown and not bounded.
	target := say(ann, room, "react to this")["message"].(map[string]any)["id"].(string)
	show(t, "add a reaction", tr.record(func() { bob.Must(200, "PUT", "/api/v1/messages/"+target+"/reactions/👍", nil) }))
	show(t, "edit", tr.record(func() { ann.Must(200, "PATCH", "/api/v1/messages/"+target, map[string]any{"text": "edited"}) }))
	seq := say(ann, room, "read up to here")["message"].(map[string]any)["seq"]
	show(t, "move the read position", tr.record(func() {
		bob.Must(200, "PUT", convPath(room)+"/read", map[string]any{"seq": seq})
	}))
}

// A message that arrives from a linked server takes the same path. Its cost
// here includes the link's own bookkeeping around it.
func TestPostCostLinked(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	room := a.Owner.Channel("shared", false)
	_, local := l.Share(0, room, "shared", nil, 1)
	say(a.Owner, room, "warm")
	b.Owner.HasMessage(local[1], "warm")
	say(a.Owner, room, "warm again")
	b.Owner.HasMessage(local[1], "warm again")
	tr := newTracer(t, b)
	n := 0
	tr.hold(t, "post arriving from a linked server", func() {
		n++
		say(a.Owner, room, fmt.Sprintf("across the link %d", n))
		// Arrival is not a request of this server. It is over when the
		// store falls quiet; asking the API would add reads of its own.
		time.Sleep(100 * time.Millisecond)
		tr.settle()
	})
	b.Owner.HasMessage(local[1], fmt.Sprintf("across the link %d", n))
}
