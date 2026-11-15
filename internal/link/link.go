// Package link shares conversations between Churr servers, directly or
// through a hub, as docs/LINK-PROTOCOL.md describes. The protocol itself
// (frames, envelopes, the handshake) is in the wire package below this one,
// which the hub program shares; this package is what a server does with it:
// peers, streams, the stream keys, the outbox, what is accepted from whom,
// and handing what is accepted to the conversation service.
package link

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/version"
)

// The settings this package reads (docs/SCHEMA.md section 2).
const (
	SettingSendPresence    = "links.send_presence"
	SettingDefaultMaxBytes = "links.default_max_file_bytes"
	defaultMaxFileBytes    = 26214400
)

// The waits and windows of the protocol.
const (
	submitWait      = 5 * time.Second
	inviteLife      = 7 * 24 * time.Hour
	offerLife       = 7 * 24 * time.Hour
	directSkew      = 5 * time.Minute
	directMemory    = 10 * time.Minute
	eventMaxAge     = 35 * 24 * time.Hour
	logLife         = 30 * 24 * time.Hour
	keyWait         = 60 * time.Second
	ackEvery        = time.Second
	maxInFlight     = 1000
	outboxFirstWait = 5 * time.Second
	outboxMaxWait   = 5 * time.Minute
)

// Presence is what link needs to know about who is online, to send presence
// to other servers. live.Hub provides it.
type Presence interface {
	Presence(account model.AccountID) model.Presence
}

// Deps is what a Service is built from.
type Deps struct {
	Store store.Store
	Keys  *secret.Keys
	Chat  *chat.Service
	Perm  *perm.Checker
	Blobs blob.Blobs
	Log   *slog.Logger
	// Name is this server's name: the host name of its public address.
	Name string
	// URL is where other servers dial this one: wss://name/link/v1.
	URL string
	// HTTPClient carries dials and /.well-known/churr fetches. Nil means
	// the default client. Tests pass one that resolves their made-up
	// names to loopback.
	HTTPClient *http.Client
	// Presence says who is online. Nil means nobody's presence is sent.
	Presence Presence
	// TmpDir is where arriving files are held while they are checked.
	TmpDir string
	// Now is the clock; tests move it.
	Now func() model.Time
	// ClockSynced is the owner's declaration (-clock-synced) that this
	// server's clock and those of the servers linked to it directly are
	// synchronised. It is never worked out by the server itself. With it,
	// an offset this server measures beyond a small bound is reported as
	// the path's asymmetry and not applied (clock.go).
	ClockSynced bool
}

// Service is the link to other servers.
type Service struct {
	store    store.Store
	keys     *secret.Keys
	chat     *chat.Service
	perm     *perm.Checker
	blobs    blob.Blobs
	log      *slog.Logger
	http     *http.Client
	presence Presence
	tmpDir   string
	now      func() model.Time
	// clockSynced is Deps.ClockSynced.
	clockSynced bool

	id wire.Identity
	// self is this server's own row in `servers`. It is there so a
	// stream's server list can name this server and hold its counter.
	self store.Server

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// testSubmitting, when a test has set it, is given every submit frame
	// before it leaves and returns the frame to send in its place: a test
	// plays a server that stamps wrongly, or keeps envelopes back.
	testSubmitting atomic.Pointer[func(stream model.StreamID, f wire.Frame) wire.Frame]

	mu      sync.Mutex
	conns   map[model.PeerID]*peerConn
	dialers map[model.PeerID]context.CancelFunc
	rts     map[model.StreamID]*streamRT
	// seen is the replay window of direct messages: msg_id to when it
	// came.
	seen map[string]model.Time
	// replies are requests waiting for a direct message that answers them.
	replies map[string]chan directMessage
	// rosters holds the newest roster version applied per hub.
	rosters map[model.PeerID]int64
	// cluster is what this server holds of clusters of hubs (cluster.go).
	cluster clusterState
	// offers keeps what an offer said that has no column: its topic.
	offers map[model.StreamID]offer
	// opened remembers stream_open frames seen on direct links, so a
	// second one for the same stream is refused.
	opened map[model.StreamID]string
	// audited limits link.event_rejected lines to one per stream, origin
	// and reason per hour.
	audited map[string]model.Time
	// heard is when something verified last arrived from each server
	// reached through a hub.
	heard map[string]time.Time
	// presenceSent and presenceSeen are the newest presence sent and
	// received per account (ephemeral.go).
	presenceSent map[string]model.Time
	presenceSeen map[model.AccountID]RemotePresence
	// fetching marks files whose bytes are being fetched now.
	fetching map[model.FileID]bool
	// fetches is when each arriving file may be asked for again, and how
	// far its transfer has come.
	fetches map[model.FileID]*fetchState
	// invitedTo are the accounts a member.invited or an offer has named
	// in each ad-hoc stream (personal.go).
	invitedTo map[model.StreamID]map[model.AccountID]bool
	// polls is the forms package's rules for polls (forms.go).
	polls PollRules
	// tallies are the polls this server keeps the answers of whose counts
	// have changed and not been sent yet (forms.go).
	tallies map[model.FormID]*time.Timer
	// tallyFold replaces the second within which changes to one poll are
	// folded into one form.tallied. Tests set it.
	tallyFold time.Duration
	// lastNames is the name and topic last sent for each owned stream.
	lastNames map[model.StreamID][2]string
	// submitted says which stream each unanswered submit frame was for.
	submitted map[submitKey]model.StreamID
	// told is what each peer, as a time reference, last told this server
	// about its clock, and measured what this server last measured of each
	// direct peer's (clock.go). Both are also in the peer's row.
	told     map[model.PeerID]model.PeerClock
	measured map[model.PeerID]model.PeerClock

	limits struct {
		submit, ephemeral, direct *ratelimit.Limiter
		blob                      *ratelimit.Limiter
	}
	// refusing writes what those limits refuse to the log, and limited what
	// other servers and hubs refused of ours, both counted
	// (ratelimit.Refusals).
	refusing, limited *ratelimit.Refusals

	// stats counts what tests and the admin overview want to see.
	stats Stats

	// hooks are the pieces other packages plug in (calls.go, sealed.go).
	calls  CallHost
	sealed SealedStore

	// lastError is the last error frame another side sent.
	lastError wire.Frame

	// callStarts is, per shared conversation, the call this server takes to
	// be live there and the stamp of its call.started; callRivals a
	// call.started that lost to it (hooks.go).
	callStarts map[model.ConversationID]callStart
	callRivals map[model.ConversationID]callStart
	// frameWaits are requests to a hub waiting for the frame that answers
	// them, by peer and frame id (hubchan.go).
	frameWaits map[string]chan wire.Frame
	// testNoKeys makes this server a key-maker that makes no keys, and
	// testKeyMade is told every key it does make (hubchan.go). testStep,
	// when set, is asked before each step of a move and stops the move
	// there when it says so (move.go).
	// testDeaf makes this server lose every answer to its submits, as a
	// link that drops them would.
	testDeaf    atomic.Bool
	testNoKeys  atomic.Bool
	testKeyMade atomic.Pointer[func(stream model.StreamID, epoch int64, key []byte)]
	testStep    atomic.Pointer[func(step string) bool]

	// snapshotSpread is the time presence snapshots are spread over after
	// a reconnection, in nanoseconds: zero for the documented time, below
	// zero for none (content.go).
	snapshotSpread atomic.Int64

	// test hooks; nil outside tests.
	wellKnown func(ctx context.Context, name string) (wire.WellKnown, error)
	// testAnswers hands the answer to a submit a test sent itself back to
	// the test, by the submit frame's id. testNoClock makes the service
	// behave as if a peer had never told it its clock offset.
	testAnswers map[int64]chan wire.Frame
	testNoClock map[model.PeerID]bool
}

// Stats are counters kept since the server started.
type Stats struct {
	// Rejected counts rejected envelopes by reason.
	Rejected map[string]int64
	// Delivered counts envelopes received in deliver frames, and
	// Ephemeral ephemeral frames, both per stream.
	Delivered map[model.StreamID]int64
	Ephemeral map[model.StreamID]int64
	// Dropped counts frames and direct messages dropped unprocessed, by
	// reason.
	Dropped map[string]int64
	// Dials counts connection attempts per peer.
	Dials map[model.PeerID]int64
	// Gaps counts deliveries that showed something of an origin had been
	// missed.
	Gaps int64
	// Refused counts, by code, the envelopes this server refused to take
	// into the log of a stream it is the time reference of.
	Refused map[string]int64
	// Restamped counts events of this server's own that were refused for
	// their stamp and stamped again.
	Restamped int64
	// Replaced counts, by kind, the things this server held that gave way
	// to one made elsewhere at the same moment with an earlier stamp: a
	// sealed chat's epoch record, a call.
	Replaced map[string]int64
	// Held counts events put aside until the message they refer to arrived,
	// and HeldExpired those whose message never did. Reopened counts
	// envelopes under a key from before this server joined that another
	// server opened for it, and HistoryAsked the times a stretch of such
	// envelopes was asked for as history instead.
	Held, HeldExpired, Reopened, HistoryAsked int64
	// Snapshots counts presence snapshots sent.
	Snapshots int64
}

// New builds the service. Nothing connects until Start.
func New(d Deps) (*Service, error) {
	if d.Name == "" || d.Keys == nil || d.Store == nil || d.Chat == nil {
		return nil, errors.New("link: a name, keys, a store and the chat service are needed")
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Now == nil {
		d.Now = model.Now
	}
	if d.HTTPClient == nil {
		d.HTTPClient = http.DefaultClient
	}
	s := &Service{store: d.Store, keys: d.Keys, chat: d.Chat, perm: d.Perm, blobs: d.Blobs, log: d.Log, http: d.HTTPClient,
		presence: d.Presence, tmpDir: d.TmpDir, now: d.Now, clockSynced: d.ClockSynced,
		id:    wire.Identity{Name: d.Name, Kind: wire.KindServer, Keys: d.Keys, URL: d.URL, Software: "churr/" + version.String()},
		conns: map[model.PeerID]*peerConn{}, dialers: map[model.PeerID]context.CancelFunc{}, rts: map[model.StreamID]*streamRT{},
		seen: map[string]model.Time{}, replies: map[string]chan directMessage{}, rosters: map[model.PeerID]int64{},
		offers: map[model.StreamID]offer{}, opened: map[model.StreamID]string{}, audited: map[string]model.Time{},
		heard: map[string]time.Time{}, presenceSent: map[string]model.Time{}, presenceSeen: map[model.AccountID]RemotePresence{},
		fetching: map[model.FileID]bool{}, fetches: map[model.FileID]*fetchState{}, lastNames: map[model.StreamID][2]string{},
		told: map[model.PeerID]model.PeerClock{}, measured: map[model.PeerID]model.PeerClock{},
		submitted: map[submitKey]model.StreamID{}, frameWaits: map[string]chan wire.Frame{},
		callStarts: map[model.ConversationID]callStart{}, callRivals: map[model.ConversationID]callStart{},
		stats: Stats{Rejected: map[string]int64{}, Delivered: map[model.StreamID]int64{}, Ephemeral: map[model.StreamID]int64{},
			Dropped: map[string]int64{}, Dials: map[model.PeerID]int64{}, Refused: map[string]int64{}, Replaced: map[string]int64{}},
	}
	s.refusing = ratelimit.NewRefusals(s.log, "link: frames from another server were refused: it is over a rate limit", 0)
	s.limited = ratelimit.NewRefusals(s.log, "link: this server's envelopes were refused as over a rate limit; the stream's events wait", 0)
	lim := wire.DefaultLimits()
	s.limits.submit = ratelimit.New(ratelimit.Per(lim.SubmitsPerMinute, time.Minute), lim.SubmitsPerMinute)
	s.limits.ephemeral = ratelimit.New(ratelimit.Per(lim.EphemeralPerMinute, time.Minute), lim.EphemeralPerMinute)
	s.limits.direct = ratelimit.New(ratelimit.Per(lim.DirectPerMinute, time.Minute), lim.DirectPerMinute)
	s.ctx, s.cancel = context.WithCancel(context.Background())
	return s, nil
}

// Identity is who this server is on the link.
func (s *Service) Identity() wire.Identity { return s.id }

// Fingerprint is this server's own fingerprint as an admin console shows it.
func (s *Service) Fingerprint() string { return wire.KeyFingerprint(s.keys.SignPublic()) }

// Start makes sure this server's own row exists, then connects to every
// peer this server dials and begins the background work: the outbox, files
// still arriving, offers that lapse.
func (s *Service) Start(ctx context.Context) error {
	self, _, err := s.store.Peers().PinServer(ctx, store.Server{Name: s.id.Name, SignKey: s.keys.SignPublic(), BoxKey: s.keys.BoxPublic()})
	if err != nil {
		return err
	}
	s.self = self
	peers, err := s.store.Peers().List(ctx)
	if err != nil {
		return err
	}
	for _, p := range peers {
		if p.Dials && p.State == store.PeerActive {
			s.startDialling(p)
		}
	}
	s.wg.Add(1)
	go s.background()
	return nil
}

// Stop says goodbye on every connection and ends the background work.
func (s *Service) Stop() {
	s.mu.Lock()
	conns := make([]*peerConn, 0, len(s.conns))
	for _, c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		c.conn.CloseWith(wire.Frame{Frame: wire.FBye, Reason: "shutdown"})
	}
	s.cancel()
	s.wg.Wait()
}

// isSelf reports whether a server row is this server's own.
func (s *Service) isSelf(id model.ServerID) bool { return id == s.self.ID }

// count adds one to a counter of Stats.
func (s *Service) count(fn func(st *Stats)) {
	s.mu.Lock()
	fn(&s.stats)
	s.mu.Unlock()
}

// StatsNow returns a copy of the counters.
func (s *Service) StatsNow() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Stats{Rejected: map[string]int64{}, Delivered: map[model.StreamID]int64{}, Ephemeral: map[model.StreamID]int64{},
		Dropped: map[string]int64{}, Dials: map[model.PeerID]int64{}, Gaps: s.stats.Gaps, Refused: map[string]int64{},
		Restamped: s.stats.Restamped, Replaced: map[string]int64{},
		Held: s.stats.Held, HeldExpired: s.stats.HeldExpired, Reopened: s.stats.Reopened, HistoryAsked: s.stats.HistoryAsked,
		Snapshots: s.stats.Snapshots}
	for k, v := range s.stats.Replaced {
		out.Replaced[k] = v
	}
	for k, v := range s.stats.Rejected {
		out.Rejected[k] = v
	}
	for k, v := range s.stats.Refused {
		out.Refused[k] = v
	}
	for k, v := range s.stats.Delivered {
		out.Delivered[k] = v
	}
	for k, v := range s.stats.Ephemeral {
		out.Ephemeral[k] = v
	}
	for k, v := range s.stats.Dropped {
		out.Dropped[k] = v
	}
	for k, v := range s.stats.Dials {
		out.Dials[k] = v
	}
	return out
}

// Overview is what the admin overview shows of the links.
type Overview struct {
	Peers          int   `json:"peers"`
	PeersConnected int   `json:"peers_connected"`
	Streams        int   `json:"streams"`
	OutboxWaiting  int64 `json:"outbox_waiting"`
}

// Overview counts peers, connected peers, active streams and envelopes
// waiting in the outbox, for getOverview.
func (s *Service) Overview(ctx context.Context) (Overview, error) {
	var o Overview
	peers, err := s.store.Peers().List(ctx)
	if err != nil {
		return o, err
	}
	for _, p := range peers {
		if p.State == store.PeerActive || p.State == store.PeerPaused {
			o.Peers++
			if s.connOf(p.ID) != nil {
				o.PeersConnected++
			}
		}
	}
	streams, err := s.store.Streams().List(ctx, store.StreamActive)
	if err != nil {
		return o, err
	}
	o.Streams = len(streams)
	o.OutboxWaiting, err = s.store.Streams().OutboxCount(ctx)
	return o, err
}

// Allowed says whether an account may manage links: the owner only
// (docs/PERMISSIONS.md). Every admin operation of this package asks it
// first.
func (s *Service) Allowed(ctx context.Context, actor model.Account) error {
	if s.perm == nil {
		if actor.Role != model.RoleOwner {
			return model.ErrForbidden
		}
		return nil
	}
	return s.perm.CanAdminister(ctx, actor, perm.ManageLinks)
}

// OverviewPeers returns the linked servers and hubs in the contract's Peer
// shape, for getOverview.
func (s *Service) OverviewPeers(ctx context.Context) ([]any, error) {
	peers, err := s.Peers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(peers))
	for _, p := range peers {
		if p.State == store.PeerActive || p.State == store.PeerPaused {
			out = append(out, p)
		}
	}
	return out, nil
}

// OutboxWaiting is how many events wait to be acknowledged, for getOverview.
func (s *Service) OutboxWaiting(ctx context.Context) (int64, error) {
	return s.store.Streams().OutboxCount(ctx)
}

// audit writes a line to the audit log. actor is the owner who acted, or ""
// for something the server did by itself.
func (s *Service) audit(ctx context.Context, a store.Areas, actor model.AccountID, action, targetKind, targetID string, detail map[string]any) {
	e := model.AuditEntry{At: s.now(), ActorID: string(actor), ActorKind: "account", Action: action,
		TargetKind: targetKind, TargetID: targetID, Detail: detail}
	if actor == "" {
		e.ActorKind = "server"
	}
	if err := a.Audit().Append(ctx, e); err != nil {
		s.log.Warn("link: an audit line could not be written", "action", action, "err", err)
	}
}

// auditRejected writes link.event_rejected, at most one line per stream,
// origin and reason per hour.
func (s *Service) auditRejected(ctx context.Context, st store.Stream, origin, reason string, detail map[string]any) {
	key := string(st.ID) + "|" + origin + "|" + reason
	now := s.now()
	s.mu.Lock()
	last, seen := s.audited[key]
	if seen && now.Sub(last) < time.Hour {
		s.mu.Unlock()
		return
	}
	s.audited[key] = now
	s.mu.Unlock()
	if detail == nil {
		detail = map[string]any{}
	}
	detail["origin"], detail["reason"] = origin, reason
	s.audit(ctx, s.store, "", "link.event_rejected", "stream", string(st.ID), detail)
}

// maxBlobDefault is the largest file a new stream carries unless its owner
// says otherwise.
func (s *Service) maxBlobDefault(ctx context.Context) int64 {
	v, err := store.SettingIntOr(ctx, s.store, SettingDefaultMaxBytes, defaultMaxFileBytes)
	if err != nil || v <= 0 {
		return defaultMaxFileBytes
	}
	return v
}

// submitKey names a submit frame: frame ids start again on every connection,
// so the peer is part of the name.
type submitKey struct {
	peer model.PeerID
	id   int64
}

// streamRT is what the service keeps in memory about one stream while it
// runs. Everything in it can be lost at a restart: what matters is in the
// database.
type streamRT struct {
	id model.StreamID
	// work serialises the owner's changes to the stream's server list, and
	// record the taking of a hub's records of a channel, one at a time.
	work   sync.Mutex
	record sync.Mutex
	// moving is held while a move of the stream onto a hub is being run
	// (move.go): one run at a time.
	moving sync.Mutex
	// jobs is the stream's queue: delivered envelopes are applied one at a
	// time, as they come, by one goroutine, off the connection's reader.
	jobs chan func()
	// send is held while envelopes of this server's log leave for the other
	// server, so a replay of the log and what is newly written do not mix.
	send sync.Mutex
	// submit is held while this server's waiting events are read and sent.
	submit sync.Mutex

	mu sync.Mutex
	// catching: a catch_up has been sent and not answered. asked is what
	// it said this server held of each origin, and askedAt the `at` of the
	// last event held from each: what the answer is measured against.
	catching bool
	asked    map[string]int64
	askedAt  map[string]int64
	// catchingAt is when that catch_up left, catchingID its frame id and
	// catchingPeer the peer it went to: an error frame that answers it ends
	// it, and one never answered stops counting after a while.
	catchingAt   time.Time
	catchingID   int64
	catchingPeer model.PeerID
	// hold is what the stream holds of events whose message has not arrived
	// (hold.go). Only the stream's own goroutine touches it.
	hold *holdState
	// openFailed is until when no server is asked to open an envelope under
	// an old key, after none could; unreadFrom the earliest time of such
	// envelopes not yet asked for as history, and unreadTimer what asks.
	openFailed  time.Time
	unreadFrom  int64
	unreadTimer *time.Timer
	// watchers are the link's own events waiting to be acknowledged, by
	// origin event id; each is closed when its event is.
	watchers map[string]chan struct{}
	// inflight is the id of the submit frame that has not been answered,
	// and inflightAt when it left.
	inflight   int64
	inflightAt time.Time
	// awaitClock: an event was refused for its time though its offset was
	// the one the reference holds, so this server's clock has moved since
	// it was measured. Stamping again waits for the reference to say so.
	awaitClock bool
	// keyArrived is closed and replaced whenever a key for the stream
	// arrives. keyAsked are the epochs whose key this server has asked for.
	keyArrived chan struct{}
	keyAsked   map[int64]bool
	// history is how a server that has just joined waits, before applying
	// anything after the event that let it in, for the snapshot that tells
	// it who the members are: historyHere is set when the last part has
	// been stored, and historyWait is closed then if anyone waits.
	historyHere bool
	historyWait chan struct{}
	// firstAsked are this server's own requests for the snapshot a joining
	// server is sent, by msg_id (hubchan.go).
	firstAsked map[string]bool
	// sent and acked are, for a stream this server keeps the log of, the
	// highest log position sent to and acknowledged by the other server.
	// cursor is, for any other stream, the last position of the
	// deliverer's log this server has taken.
	sent, acked int64
	stalled     bool
	cursor      int64
	ackTimer    *time.Timer
	// ackPeer is the peer the pending acknowledgement is for: a stream
	// that has moved onto a hub is delivered by another peer than before.
	ackPeer model.PeerID
	// forged counts rejections for forged authorship per origin in the
	// current hour, for the warning on the stream's page.
	forged     map[string]int
	forgedHour int64
}

func (s *Service) rt(id model.StreamID) *streamRT {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rts[id]
	if r == nil {
		r = &streamRT{id: id, watchers: map[string]chan struct{}{}, keyArrived: make(chan struct{}), keyAsked: map[int64]bool{}, firstAsked: map[string]bool{}, forged: map[string]int{},
			jobs: make(chan func(), 4096)}
		s.rts[id] = r
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			for {
				select {
				case <-s.ctx.Done():
					return
				case job := <-r.jobs:
					job()
				}
			}
		}()
	}
	return r
}

// newID makes a UUIDv7 for an event or a message.
func newID() string { return model.NewID[string]() }

// mustEnvelope writes an envelope as the log keeps it: an Envelope message
// of the link's schema. Only envelopes that were read off the wire or
// sealed here get this far, so it cannot fail.
func mustEnvelope(env wire.Envelope) []byte {
	b, err := wire.MarshalEnvelope(env)
	if err != nil {
		panic("link: " + err.Error())
	}
	return b
}

// mustJSON marshals something this package built itself.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("link: " + err.Error())
	}
	return b
}

// background runs the periodic work until the service stops.
func (s *Service) background() {
	defer s.wg.Done()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	n := 0
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
			n++
			s.retryOutbox(s.ctx)
			s.tickMoves(s.ctx, n)
			if n%10 == 0 {
				s.fetchArriving(s.ctx)
				s.forgetDirect()
			}
			if n%60 == 0 {
				s.resendOffers(s.ctx)
				s.sweepPresence()
			}
			if n%3600 == 0 {
				if _, err := s.Sweep(s.ctx); err != nil {
					s.log.Warn("link: the hourly sweep failed", "err", err)
				}
			}
		}
	}
}

// Sweep is the janitor's part of this package: stream_seen rows older than
// 35 days, the log of streams this server is the time reference of older
// than 30 days, and offers nobody answered in seven. It returns how many
// rows went.
func (s *Service) Sweep(ctx context.Context) (int64, error) {
	now := s.now()
	seen, err := s.store.Streams().PruneSeen(ctx, now.Add(-eventMaxAge))
	if err != nil {
		return 0, err
	}
	logged, err := s.store.Streams().PruneLog(ctx, now.Add(-logLife))
	if err != nil {
		return seen, err
	}
	offered, err := s.store.Streams().List(ctx, store.StreamOffered)
	if err != nil {
		return seen + logged, err
	}
	lapsed := int64(0)
	for _, st := range offered {
		if !st.OfferedAt.IsZero() && now.Sub(st.OfferedAt) > offerLife {
			if err := s.store.Streams().Delete(ctx, st.ID); err == nil {
				lapsed++
			}
		}
	}
	return seen + logged + lapsed, nil
}
