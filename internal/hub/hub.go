// Package hub is the churr-hub program: the link protocol with a log
// (docs/LINK-PROTOCOL.md section 23). It accepts the servers its operator
// invited, is the time reference of the streams opened on it, checks the
// stamp of every envelope its servers submit, forwards the envelopes, and
// keeps them for thirty days so a server that was offline can catch up. It
// orders nothing: where an event stands is in the stamp its origin signed,
// which the hub can refuse and cannot change. It forwards sealed direct
// messages between servers without being able to open them.
//
// It has no accounts, no channels and no search, and it never holds a stream
// key or a server's private key: everything it stores is names of servers,
// public keys, stream identifiers, and ciphertext.
package hub

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite" // the hub's own small database

	"github.com/tikaydaes/churr/internal/acme"
	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/version"
)

// The defaults of section 23, each settable per server with `limits`.
const (
	// defaultSubmits is fifty envelopes a second, what -server-rate says
	// when it is not given. It was 600 a minute: ten a second, which one
	// busy shared channel passes, and a bulk change of members (an envelope
	// each) then held every message behind it for minutes.
	defaultSubmits   = 3000    // envelopes a minute
	defaultEphemeral = 1200    // ephemeral frames a minute
	defaultDirect    = 600     // direct messages a minute, file chunks apart
	defaultBlobBytes = 1 << 30 // file chunk bytes an hour
	defaultStreams   = 1000    // streams a server may own
	maxStreamServers = 100     // servers in one stream
	// A direct message this large or larger is taken to be a file chunk.
	// The hub cannot see what a direct message is, only how big it is.
	chunkThreshold = 64 << 10
)

const schema = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS servers (
  name TEXT PRIMARY KEY, sign_key BLOB NOT NULL, box_key BLOB NOT NULL, box_key_sig BLOB NOT NULL,
  joined_at INTEGER NOT NULL, state TEXT NOT NULL,          -- active | removed
  submits INTEGER, blob_bytes INTEGER, streams INTEGER       -- limits; null = the default
);
CREATE TABLE IF NOT EXISTS invites (
  id TEXT PRIMARY KEY, hash BLOB NOT NULL, for_name TEXT, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS streams (
  id TEXT PRIMARY KEY, owner TEXT NOT NULL, version INTEGER NOT NULL, next_seq INTEGER NOT NULL,
  state TEXT NOT NULL, closed_at INTEGER, created_at INTEGER NOT NULL, told INTEGER NOT NULL DEFAULT 1,
  home_hub TEXT NOT NULL DEFAULT '',                         -- the hub that is the stream's home and time reference
  epoch INTEGER NOT NULL DEFAULT 0                           -- the highest key epoch the owner has used
);
CREATE TABLE IF NOT EXISTS stream_servers (stream_id TEXT NOT NULL, name TEXT NOT NULL, PRIMARY KEY (stream_id, name));
CREATE TABLE IF NOT EXISTS log (                             -- seq is the position in the log: a delivery cursor, not an order
  stream_id TEXT NOT NULL, seq INTEGER NOT NULL, origin_event_id TEXT NOT NULL, key_epoch INTEGER NOT NULL,
  envelope BLOB NOT NULL, at INTEGER NOT NULL,              -- envelope: an Envelope message, as it will be delivered
  origin TEXT NOT NULL DEFAULT '', n INTEGER NOT NULL DEFAULT 0, prev_n INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (stream_id, seq)
);
CREATE INDEX IF NOT EXISTS log_event ON log (stream_id, origin_event_id, key_epoch);
CREATE INDEX IF NOT EXISTS log_at ON log (at);
CREATE TABLE IF NOT EXISTS origins (                         -- per stream and origin: the last counter and time taken into the log
  stream_id TEXT NOT NULL, name TEXT NOT NULL, last_n INTEGER NOT NULL, last_at INTEGER NOT NULL, PRIMARY KEY (stream_id, name)
);
CREATE TABLE IF NOT EXISTS away (                            -- the last few stretches each server was disconnected, by the hub's clock
  name TEXT NOT NULL, from_at INTEGER NOT NULL, to_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS away_name ON away (name, to_at);
CREATE TABLE IF NOT EXISTS channels (
  stream_id TEXT PRIMARY KEY, record BLOB NOT NULL,          -- the channel record at its newest version: a ChannelRecord message
  creator TEXT NOT NULL,                                     -- the server that shared it to the hub
  inviters TEXT NOT NULL DEFAULT '{}',                       -- invited server -> the home server of the manager who invited it
  maker_deadline INTEGER NOT NULL DEFAULT 0,                 -- when the named key-maker must have used the new epoch; 0 = nothing awaited
  told INTEGER NOT NULL DEFAULT 1, also TEXT NOT NULL DEFAULT '[]', -- 0: the record has yet to be sent; also: servers outside it to tell
  move_id TEXT NOT NULL DEFAULT '',                          -- for a channel brought from a direct link: the move that brought it
  declined TEXT NOT NULL DEFAULT '[]',                       -- servers to tell, when they next connect, that their request to join was declined
  joined_have TEXT NOT NULL DEFAULT '{}'                     -- member server -> what the log held of each origin when it was admitted: where it starts from
);
CREATE TABLE IF NOT EXISTS clocks (
  name TEXT PRIMARY KEY, clock TEXT NOT NULL                 -- what the hub last measured of each server's clock, as JSON; kept while it is away
);
CREATE TABLE IF NOT EXISTS counters (
  name TEXT NOT NULL, hour INTEGER NOT NULL, envelopes INTEGER NOT NULL DEFAULT 0, bytes INTEGER NOT NULL DEFAULT 0,
  refusals INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (name, hour)
);
-- Trunks between hubs (section 23b).
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS cluster (                         -- the cluster roster: one row per admission of a hub, kept for good
  hub TEXT NOT NULL, joined_at INTEGER NOT NULL, entry BLOB NOT NULL, -- the entry with its signatures: a ClusterEntry message
  PRIMARY KEY (hub, joined_at)
);
CREATE TABLE IF NOT EXISTS trunks (                          -- the hubs this one is joined to directly
  name TEXT PRIMARY KEY, url TEXT NOT NULL DEFAULT '', sign_key BLOB, dials INTEGER NOT NULL DEFAULT 0,
  invite TEXT NOT NULL DEFAULT '',                           -- on the side that dials: the invitation, until it has been used
  state TEXT NOT NULL,                                       -- pending | active | removed
  clock TEXT,                                                -- the neighbour's clock against this hub's, as measured or as told
  away TEXT NOT NULL DEFAULT '[]',                           -- the last few stretches the trunk was down, by this hub's clock
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS hub_rosters (                     -- each other hub's own servers, as that hub last signed them
  hub TEXT PRIMARY KEY, version INTEGER NOT NULL, signed BLOB NOT NULL, sig BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS hub_servers (                     -- the servers on the other hubs of the cluster
  name TEXT PRIMARY KEY, hub TEXT NOT NULL, sign_key BLOB NOT NULL, box_key BLOB NOT NULL, box_key_sig BLOB NOT NULL, joined_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS hub_clocks (                      -- this hub's clock minus each other hub's, composed along the trunks
  hub TEXT PRIMARY KEY, offset_ms INTEGER NOT NULL, uncertainty_ms INTEGER NOT NULL, measured_at INTEGER NOT NULL, via TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS stream_ctl (                      -- a stream a server owns that crosses a trunk: its owner's last control frame, as relayed
  stream_id TEXT PRIMARY KEY, frame BLOB NOT NULL,
  floors TEXT NOT NULL DEFAULT '{}'                          -- member server -> what the home hub's log held of each origin when it joined
);
CREATE TABLE IF NOT EXISTS held (                            -- a word for a server of this hub that was not connected when it came
  id INTEGER PRIMARY KEY AUTOINCREMENT, server TEXT NOT NULL, frame BLOB NOT NULL, at INTEGER NOT NULL
);
`

// Options is what a test or the command adds to the configuration.
type Options struct {
	// Listener is used instead of listening on the configured address.
	Listener net.Listener
	// URL is where servers dial the hub, if it is not wss://hostname/link/v1:
	// a local trial, a test.
	URL string
	Log *slog.Logger
	// Now is the clock; tests move it.
	Now func() model.Time
	// HTTPClient carries the dial of a trunk, for tests whose hubs have
	// made-up names. Nil means the default client.
	HTTPClient *http.Client
}

// Hub is a running hub, or the hub's data opened for an operator command.
type Hub struct {
	cfg  config.Hub
	opts Options
	log  *slog.Logger
	keys *secret.Keys
	db   *sql.DB
	id   wire.Identity
	now  func() model.Time

	started time.Time

	mu sync.Mutex
	// conns are the connected servers, by name.
	conns map[string]*serverConn
	// streams serialises each stream's log: one envelope at a time.
	streams map[string]*sync.Mutex
	// sentRoster is the roster version last sent to everyone.
	sentRoster int64
	limiters   map[string]*serverLimits
	// refused writes the refusals of those limits to the log, counted
	// (ratelimit.Refusals).
	refused *ratelimit.Refusals

	http *http.Server

	// testDrop, when a test has set it, is asked about every frame a
	// server sends; a frame it claims is dropped unanswered.
	testDrop atomic.Pointer[func(server string, f wire.Frame) bool]
	// testTap, when a test has set it, is shown the bytes of every frame
	// the hub receives, as received.
	testTap atomic.Pointer[func(server string, raw []byte)]

	// What follows is the hub as one of a cluster (trunk.go, cluster.go,
	// ship.go). trunks are the connected neighbours, by name; diallers the
	// trunks this hub is dialling; topo the roster as last checked.
	trunks   map[string]*trunkConn
	diallers map[string]context.CancelFunc
	topo     atomic.Pointer[topology]
	// routes is, per stream, which servers here and which trunks an
	// envelope goes to. It is worked out when asked for and forgotten
	// whenever a server list, a roster or the cluster changes.
	routes map[string]*route
	// keys remembers signing keys by server name between roster changes.
	signKeys map[string][]byte
	// hubClocks is this hub's clock against each other hub's.
	hubClocks map[string]hubClock
	// logMu puts every write to the log in one line, so that the numbers
	// rows are shipped by (lid) are handed out in the order rows appear.
	logMu sync.Mutex
	lid   int64
	ring  []logRow
	// runCtx ends when Run does.
	runCtx context.Context
	// trunkStats counts what crossed each trunk, for the operator and for
	// tests.
	trunkStats map[string]*TrunkStats
	// What tests hold down or slow: a trunk that is cut, a delay on what
	// arrives over one, a hub that skips the checks on time (a dishonest
	// edge), and the batch and window sizes.
	testCut   map[string]bool
	testDelay map[string]time.Duration
	// testDelayed is set once a test has slowed any trunk: from then on
	// each trunk's frames are read as they arrive and handled their delay
	// later (trunkReader).
	testDelayed  atomic.Bool
	testNoStamp  atomic.Bool
	testNoWindow atomic.Bool
	testHold     atomic.Int64
	testTrunkTap atomic.Pointer[func(trunk string, out bool, f wire.Frame)]
	// testTrunkDrop, when a test has set it, is asked about every frame that
	// arrives over a trunk; a frame it claims is lost without a word.
	testTrunkDrop atomic.Pointer[func(trunk string, f wire.Frame) bool]

	// eph counts ephemeral frames passed on and dropped (ephemeral.go).
	eph ephemeralCounts
	// saidClocks is what each neighbour last said of its clock against the
	// hubs on its side; seenCluster the cluster's tables as last looked at.
	// reach is, per neighbour, the hubs beyond it that it says it can
	// reach now.
	reach map[string]map[string]bool
	// aways is, per trunk, the last few stretches it was down for.
	aways       map[string][][2]int64
	saidClocks  map[string]map[string]wire.HubClock
	seenCluster atomic.Int64

	// commits is the line of submit frames waiting for the next transaction
	// (group.go); clocks what has been measured of each server's clock, as
	// the database also holds it.
	commits committer
	clocks  sync.Map
}

// TestTap shows fn the bytes of every frame the hub receives from a server,
// exactly as they came. Nil stops it. It is for tests that look for what a
// hub must never be sent.
func (h *Hub) TestTap(fn func(server string, raw []byte)) {
	if fn == nil {
		h.testTap.Store(nil)
		return
	}
	h.testTap.Store(&fn)
}

// TestDrop makes the hub lose, without a word, the frames fn claims: what a
// hub that drops things looks like to its servers. Nil puts it back. It is
// for tests.
func (h *Hub) TestDrop(fn func(server string, f wire.Frame) bool) {
	if fn == nil {
		h.testDrop.Store(nil)
		return
	}
	h.testDrop.Store(&fn)
}

// TestLog returns the envelopes in the hub's log of a stream, in log order,
// each with its place in the log beside it. It is for tests.
func (h *Hub) TestLog(streamID string) []wire.Envelope {
	rows, err := h.db.Query(`SELECT seq, prev_n, envelope FROM log WHERE stream_id = ? ORDER BY seq`, streamID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []wire.Envelope
	for rows.Next() {
		var pos, prev int64
		var raw []byte
		var env wire.Envelope
		if rows.Scan(&pos, &prev, &raw) == nil && readEnvelope(raw, &env) == nil {
			env.Cursor, env.Prev = pos, prev
			out = append(out, env)
		}
	}
	return out
}

// TestAway records a stretch a server was disconnected for, by the hub's
// clock, as if it had happened. It is for tests.
func (h *Hub) TestAway(name string, from, to model.Time) {
	h.db.Exec(`INSERT INTO away (name, from_at, to_at) VALUES (?, ?, ?)`, name, int64(from), int64(to))
}

type serverLimits struct {
	submits, ephemeral, direct *ratelimit.Limiter
	blobBytes                  int64
	blobUsed                   int64
	blobHour                   int64
	perMinute                  int
}

type serverConn struct {
	name string
	conn *wire.Conn
	key  []byte
	// pinger measures the server's clock (clock.go).
	pinger *wire.Pinger
}

// New opens the hub's data directory: its key and its database. Nothing
// listens until Run.
func New(cfg config.Hub) (*Hub, error) { return NewWith(cfg, Options{}) }

// NewWith is New with options.
func NewWith(cfg config.Hub, opts Options) (*Hub, error) {
	if cfg.Hostname == "" {
		return nil, errors.New("hub: a host name is needed")
	}
	if cfg.KeepDays <= 0 {
		cfg.KeepDays = 30
	}
	if err := os.MkdirAll(cfg.Data, 0o700); err != nil {
		return nil, fmt.Errorf("making the data directory: %w", err)
	}
	keys, err := secret.Open(cfg.KeysDir())
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+cfg.DBPath()+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	// One writer at a time is all SQLite allows, and the hub's work is
	// small: one connection keeps every statement in order.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening the hub's database: %w", err)
	}
	// A database made before these columns existed is given them.
	for _, c := range [][3]string{
		{"streams", "home_hub", "TEXT NOT NULL DEFAULT ''"}, {"streams", "epoch", "INTEGER NOT NULL DEFAULT 0"},
		{"channels", "move_id", "TEXT NOT NULL DEFAULT ''"}, {"channels", "declined", "TEXT NOT NULL DEFAULT '[]'"},
		{"channels", "joined_have", "TEXT NOT NULL DEFAULT '{}'"},
		{"log", "origin", "TEXT NOT NULL DEFAULT ''"}, {"log", "n", "INTEGER NOT NULL DEFAULT 0"}, {"log", "prev_n", "INTEGER NOT NULL DEFAULT 0"},
		// Trunks: lid numbers the log's rows across streams, in the order
		// they were written, and is what a trunk ships by; via is the trunk
		// a row came over, empty for a server of this hub; win_from and
		// win_to the stretch of disconnection that excused an old time, by
		// the home hub's clock.
		{"log", "lid", "INTEGER NOT NULL DEFAULT 0"}, {"log", "via", "TEXT NOT NULL DEFAULT ''"},
		{"log", "win_from", "INTEGER NOT NULL DEFAULT 0"}, {"log", "win_to", "INTEGER NOT NULL DEFAULT 0"},
		{"invites", "kind", "TEXT NOT NULL DEFAULT 'server'"},
		// last_n is the last counter of an origin this hub has seen,
		// taken or refused; logged_n the last it wrote to its log.
		{"origins", "logged_n", "INTEGER NOT NULL DEFAULT 0"},
		// For a channel whose home is another hub: the record as that hub
		// signed it. handovers are the records by which the home changed
		// hands; rehome is where the operator has asked it to go.
		{"channels", "signed", "BLOB"}, {"channels", "sig", "BLOB"},
		{"channels", "handovers", "TEXT NOT NULL DEFAULT '[]'"}, {"channels", "rehome", "TEXT NOT NULL DEFAULT ''"},
		// The roster a hub that has left its cluster still owes the
		// neighbour it left through, until it has been able to send it.
		{"trunks", "farewell", "BLOB"},
		// A channel's home offered to another hub and not yet answered: the
		// offer, as JSON. While it is there the record does not change.
		{"channels", "offer", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := ensureColumn(db, c[0], c[1], c[2]); err != nil {
			db.Close()
			return nil, fmt.Errorf("opening the hub's database: %w", err)
		}
	}
	if err := checkStoredForm(db); err != nil {
		db.Close()
		return nil, err
	}
	for _, index := range []string{`CREATE INDEX IF NOT EXISTS log_origin ON log (stream_id, origin, n)`, `CREATE INDEX IF NOT EXISTS log_lid ON log (lid)`} {
		if _, err := db.Exec(index); err != nil {
			db.Close()
			return nil, fmt.Errorf("opening the hub's database: %w", err)
		}
	}
	h := &Hub{cfg: cfg, opts: opts, log: opts.Log, keys: keys, db: db, now: opts.Now, started: time.Now(),
		conns: map[string]*serverConn{}, streams: map[string]*sync.Mutex{}, limiters: map[string]*serverLimits{},
		trunks: map[string]*trunkConn{}, diallers: map[string]context.CancelFunc{}, routes: map[string]*route{}, signKeys: map[string][]byte{},
		hubClocks: map[string]hubClock{}, trunkStats: map[string]*TrunkStats{}, testCut: map[string]bool{}, testDelay: map[string]time.Duration{},
		runCtx: context.Background(), aways: map[string][][2]int64{}, reach: map[string]map[string]bool{}}
	if h.log == nil {
		h.log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.SlogLevel()}))
	}
	h.refused = ratelimit.NewRefusals(h.log, "frames from a server were refused: it is over a rate limit", 0)
	if h.now == nil {
		h.now = model.Now
	}
	url := opts.URL
	if url == "" {
		url = "wss://" + cfg.Hostname + "/link/v1"
	}
	h.id = wire.Identity{Name: cfg.Hostname, Kind: wire.KindHub, Keys: keys, URL: url, Software: "churr-hub/" + version.String()}
	h.sentRoster = h.meta("roster_version")
	if err := h.openCluster(); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening the hub's cluster: %w", err)
	}
	return h, nil
}

// storedForm is the form of what the database keeps as bytes off the link:
// envelopes, frames, records, roster entries. It is the link protocol's
// version, 3: Protocol Buffers (ruling R70).
const storedForm = 3

// checkStoredForm notes the form in a new database and refuses one that
// holds anything in another form. The only other form there has been is the
// JSON of link versions 1 and 2, which were never released; what such a
// database holds was signed as JSON by other parties and cannot be turned
// into what they would sign now, so there is nothing to convert it to.
func checkStoredForm(db *sql.DB) error {
	var form int64
	err := db.QueryRow(`SELECT value FROM meta WHERE key = 'stored_form'`).Scan(&form)
	if err == nil && form == storedForm {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("opening the hub's database: %w", err)
	}
	for _, table := range []string{"log", "channels", "cluster", "stream_ctl", "held", "hub_rosters"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
			return fmt.Errorf("opening the hub's database: %w", err)
		}
		if n > 0 {
			return fmt.Errorf("the hub's database was written by an earlier, unreleased link protocol (form %d; this hub keeps form %d) and cannot be read: start the hub with an empty data directory", form, storedForm)
		}
	}
	_, err = db.Exec(`INSERT INTO meta (key, value) VALUES ('stored_form', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, storedForm)
	return err
}

// ensureColumn adds a column to a table that does not have it yet.
func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	rows.Close()
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}

// Close closes the database. A running hub is stopped by ending Run's
// context.
func (h *Hub) Close() error { return h.db.Close() }

// Name is the hub's name, and Fingerprint its fingerprint as an operator
// reads it out.
func (h *Hub) Name() string        { return h.id.Name }
func (h *Hub) Fingerprint() string { return wire.KeyFingerprint(h.keys.SignPublic()) }

// meta reads a number kept in the database, 0 if it is not there.
func (h *Hub) meta(key string) int64 {
	var v int64
	h.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	return v
}

func setMeta(tx *sql.Tx, key string, value int64) error {
	_, err := tx.Exec(`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// tx runs fn in one transaction.
func (h *Hub) tx(fn func(tx *sql.Tx) error) error {
	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// bumpRoster makes the roster one version newer. Whoever is serving sends it
// to every connected server on its next tick.
func bumpRoster(tx *sql.Tx) error {
	_, err := tx.Exec(`INSERT INTO meta (key, value) VALUES ('roster_version', 1) ON CONFLICT (key) DO UPDATE SET value = value + 1`)
	return err
}

// Handler serves what the hub serves at its public address: /link/v1 and
// /.well-known/churr. The status page is StatusHandler, on its own address.
func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /link/v1", h.serveLink)
	mux.HandleFunc("GET /.well-known/churr", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Write(mustJSON(h.id.WellKnown()))
	})
	return mux
}

// Run serves until ctx ends: the link, the status page if it is on, and the
// once-a-second look at what an operator command may have changed.
func (h *Hub) Run(ctx context.Context) error {
	// Alone, the hub holds its own certificate, as the server and the
	// relay do (docs/OPERATIONS.md section 3): fetched and renewed, or
	// read from -tls-cert and -tls-key. Port 80 then serves the
	// authority's challenges and a redirect.
	ln := h.opts.Listener
	var certs *acme.Manager
	if ln == nil {
		address := h.cfg.HTTP
		if !h.cfg.BehindProxy {
			var err error
			if certs, err = acme.New(h.cfg.Common, h.log); err != nil {
				return err
			}
			address = h.cfg.HTTPS
		}
		var err error
		if ln, err = net.Listen("tcp", address); err != nil {
			return err
		}
	}
	h.mu.Lock()
	h.runCtx = ctx
	h.mu.Unlock()
	h.http = &http.Server{Handler: h.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 3)
	var plain *http.Server
	if certs != nil {
		h.http.TLSConfig = certs.TLSConfig()
		_, port, _ := net.SplitHostPort(h.cfg.HTTPS)
		plain = &http.Server{Addr: h.cfg.HTTP, Handler: certs.HTTPHandler(port), ReadHeaderTimeout: 10 * time.Second}
		go func() { errc <- h.http.ServeTLS(ln, "", "") }()
		go func() {
			if err := plain.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
		go certs.Warm(ctx)
		defer plain.Close()
	} else {
		go func() { errc <- h.http.Serve(ln) }()
	}
	var status *http.Server
	if h.cfg.Status != "" {
		status = &http.Server{Addr: h.cfg.Status, Handler: h.StatusHandler(), ReadHeaderTimeout: 10 * time.Second}
		go func() {
			if err := status.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				h.log.Warn("the status page could not be served", "err", err)
			}
		}()
	}
	h.log.Info("the hub is listening", "address", ln.Addr().String(), "name", h.id.Name, "fingerprint", h.Fingerprint())
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	n := 0
	var failed error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case failed = <-errc:
			break loop
		case <-tick.C:
			h.Reconcile()
			if n++; n%3600 == 0 {
				h.Sweep()
			}
		}
	}
	h.mu.Lock()
	conns := make([]*serverConn, 0, len(h.conns))
	for _, c := range h.conns {
		conns = append(conns, c)
	}
	trunks := make([]*trunkConn, 0, len(h.trunks))
	for _, t := range h.trunks {
		trunks = append(trunks, t)
	}
	for name, stop := range h.diallers {
		stop()
		delete(h.diallers, name)
	}
	h.mu.Unlock()
	for _, c := range conns {
		c.conn.CloseWith(wire.Frame{Frame: wire.FBye, Reason: "shutdown"})
	}
	for _, t := range trunks {
		t.conn.CloseWith(wire.Frame{Frame: wire.FBye, Reason: "shutdown"})
	}
	// Nothing the hub serves is worth waiting for: the servers have been
	// told, and they dial again.
	h.http.Close()
	if status != nil {
		status.Close()
	}
	if errors.Is(failed, http.ErrServerClosed) {
		failed = nil
	}
	return failed
}

// Sweep is the hub's janitor: log entries, counters and closed streams older
// than the time envelopes are kept (30 days). It returns how many log
// entries went.
func (h *Hub) Sweep() int64 {
	before := int64(h.now().Add(-time.Duration(h.cfg.KeepDays) * 24 * time.Hour))
	// The numbering of the log's rows must outlive the rows themselves.
	h.logMu.Lock()
	h.db.Exec(`INSERT INTO meta (key, value) VALUES ('log_lid', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, h.lid)
	h.logMu.Unlock()
	h.db.Exec(`DELETE FROM held WHERE at < ?`, before)
	res, err := h.db.Exec(`DELETE FROM log WHERE at < ?`, before)
	if err != nil {
		h.log.Warn("the log could not be swept", "err", err)
		return 0
	}
	n, _ := res.RowsAffected()
	h.db.Exec(`DELETE FROM counters WHERE hour < ?`, before/3600000)
	h.db.Exec(`DELETE FROM stream_servers WHERE stream_id IN (SELECT id FROM streams WHERE state = 'closed' AND closed_at < ?)`, before)
	h.db.Exec(`DELETE FROM origins WHERE stream_id IN (SELECT id FROM streams WHERE state = 'closed' AND closed_at < ?)`, before)
	h.db.Exec(`DELETE FROM away WHERE to_at < ?`, int64(h.now().Add(-awayLife)))
	h.db.Exec(`DELETE FROM channels WHERE stream_id IN (SELECT id FROM streams WHERE state = 'closed' AND closed_at < ?)`, before)
	h.db.Exec(`DELETE FROM stream_ctl WHERE stream_id IN (SELECT id FROM streams WHERE state = 'closed' AND closed_at < ?)`, before)
	h.db.Exec(`DELETE FROM streams WHERE state = 'closed' AND closed_at < ?`, before)
	h.db.Exec(`DELETE FROM invites WHERE expires_at < ?`, int64(h.now()))
	return n
}

// count adds to a server's counters for the current hour.
func (h *Hub) count(name string, envelopes, bytes, refusals int64) {
	hour := int64(h.now()) / 3600000
	h.db.Exec(`INSERT INTO counters (name, hour, envelopes, bytes, refusals) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (name, hour) DO UPDATE SET envelopes = envelopes + excluded.envelopes, bytes = bytes + excluded.bytes,
			refusals = refusals + excluded.refusals`, name, hour, envelopes, bytes, refusals)
}
