package calls

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Events about a call that go to every member of its conversation
// (docs/EVENTS.md section 8), and the ones for participants and for people
// being rung (section 11).
const (
	EventStarted           = "call.started"
	EventEnded             = "call.ended"
	EventParticipantJoined = "call.participant_joined"
	EventParticipantLeft   = "call.participant_left"
	EventTrackPublished    = "call.track_published"
	EventTrackUnpublished  = "call.track_unpublished"
	EventMuteChanged       = "call.mute_changed"
	EventSpeakersChanged   = "call.speakers_changed"
	EventLayerChanged      = "call.layer_changed"
	EventDemandChanged     = "call.demand_changed"
	EventDeclined          = "call.declined"
	EventRingStarted       = "call.ring_started"
	EventRingStopped       = "call.ring_stopped"
)

// Why a call ended, and why a participant left (docs/CALLS.md section 2).
const (
	EndedEmpty   = "empty"
	EndedAlone   = "alone"
	EndedTooLong = "too_long"
	EndedClosed  = "closed"
	EndedServer  = "server"

	LeftLeft            = "left"
	LeftTimeout         = "timeout"
	LeftJoinedElsewhere = "joined_elsewhere"
	LeftRemoved         = "removed"
	LeftSignedOut       = "signed_out"
)

// maxSubscriptions is how many video tracks one client may subscribe to.
const maxSubscriptions = 16

// maxNoticeParticipants is how many accounts a `call_ended` notice lists.
const maxNoticeParticipants = 50

// Timings are the durations of docs/CALLS.md section 12. The zero value
// gives the documented ones; tests shorten them.
type Timings struct {
	// Ticket is how long a join ticket, or a pass, is good for.
	Ticket time.Duration
	// Hold is how long a participant is kept after its signalling and
	// its media have both gone.
	Hold time.Duration
	// Alone is how long one participant may be alone before the call ends.
	Alone time.Duration
	// Longest is how long a call may last.
	Longest time.Duration
	// Ring is how long a ring lasts.
	Ring time.Duration
	// MediaTimeout is how long after joining a client has to make a media
	// connection before it is told `media_timeout`.
	MediaTimeout time.Duration
	// Unjoined is how long a call that nobody has entered is kept: its
	// starter got a ticket and never used it.
	Unjoined time.Duration
	// HostSilence is how long a call hosted by another server is kept
	// without a word from that server.
	HostSilence time.Duration
	// MediaLost is how long a media connection may hear nothing, neither
	// media nor the client's keep-alive checks, before it counts as gone.
	MediaLost time.Duration
	// Tick is how often rooms are looked at for the rules above and for
	// who is speaking. Watch is how often each participant's right to be
	// in the call is checked again.
	Tick  time.Duration
	Watch time.Duration
}

func (t *Timings) defaults() {
	set := func(d *time.Duration, v time.Duration) {
		if *d == 0 {
			*d = v
		}
	}
	set(&t.Ticket, 60*time.Second)
	set(&t.Hold, 30*time.Second)
	set(&t.Alone, 60*time.Minute)
	set(&t.Longest, 24*time.Hour)
	set(&t.Ring, 45*time.Second)
	set(&t.MediaTimeout, 15*time.Second)
	set(&t.Unjoined, 90*time.Second)
	set(&t.HostSilence, 90*time.Second)
	set(&t.MediaLost, 5*time.Second)
	set(&t.Tick, 250*time.Millisecond)
	set(&t.Watch, 5*time.Second)
}

// Hub is what calls needs of the live hub. live.Hub implements it.
type Hub interface {
	// Publish sends a conversation event to the conversation's members.
	Publish(ctx context.Context, e model.Event)
	// Send sends a frame to every connection of an account.
	Send(account model.AccountID, v any)
	// SendTo sends a frame to one connection.
	SendTo(account model.AccountID, connectionID string, v any) bool
	// SendExcept sends a frame to an account's other connections.
	SendExcept(account model.AccountID, exceptConnectionID string, v any)
}

// Ringer says who a starting call rings (docs/CALLS.md section 15).
// notify.Service implements it.
type Ringer interface {
	Rings(ctx context.Context, call model.Call) ([]model.AccountID, error)
}

// Marks is told when an account enters or leaves a call, so its presence
// can carry the in-a-call mark. people.Service implements it.
type Marks interface {
	CallChanged(account model.AccountID)
}

// Deps is what a Server is built from.
type Deps struct {
	Config Config
	Store  store.Store
	Perm   *perm.Checker
	Hub    Hub
	Chat   *chat.Service
	Rings  Ringer
	Marks  Marks
	Log    *slog.Logger
	// Timings replace the documented durations, in tests.
	Timings Timings
}

// Server is the calls service and the media server behind it.
type Server struct {
	cfg   Config
	store store.Store
	perm  *perm.Checker
	hub   Hub
	chat  *chat.Service
	rings Ringer
	marks Marks
	log   *slog.Logger
	t     Timings
	// media is nil when the server runs without calls (-media off).
	media   *media
	tickets *tickets

	// startMu makes starting and ending calls one at a time, so the row
	// of an ended call is always closed before the next one is opened.
	startMu sync.Mutex

	mu     sync.Mutex
	byConv map[model.ConversationID]*liveCall
	byID   map[model.CallID]*liveCall
	// conns is, per live connection, the call it is the signalling
	// connection of.
	conns map[string]*liveCall
	// in counts, per account, the calls it is in right now.
	in         map[model.AccountID]int
	ringing    map[model.AccountID][]*ring
	onRing     []func(Ring)
	onRingStop []func(RingStop)
	link       Link
	closed     bool

	stop chan struct{}
	done sync.WaitGroup
	// telling counts the goroutines telling people of an in-a-call mark.
	telling sync.WaitGroup
}

// liveCall is a call in progress as this server knows it: one it carries
// itself, with a room, or one another server carries, of which it knows
// who is in it.
type liveCall struct {
	id        model.CallID
	conv      model.ConversationID
	messageID model.MessageID
	startedBy model.AccountID
	startedAt model.Time
	sealed    bool
	public    bool
	// shared is true when the conversation is shared with other servers.
	shared  bool
	created time.Time

	// room is the media room, when this server carries the call.
	room *room
	// away is what is known of a call another server carries.
	away *awayCall

	// sinks are the ways back to participants on other servers, one per
	// server and account, and over is closed when the call ends (link.go).
	sinkMu sync.Mutex
	sinks  map[string]*remoteSink
	over   chan struct{}

	endOnce sync.Once
}

// New builds the service. With a media address configured it opens the
// media port, and fails if it cannot. It also ends, in the database, calls
// a previous run left live: rooms are in memory, so those calls are over.
func New(d Deps) (*Server, error) {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	d.Timings.defaults()
	s := &Server{cfg: d.Config, store: d.Store, perm: d.Perm, hub: d.Hub, chat: d.Chat, rings: d.Rings, marks: d.Marks, log: d.Log, t: d.Timings,
		tickets: newTickets(), byConv: map[model.ConversationID]*liveCall{}, byID: map[model.CallID]*liveCall{},
		conns: map[string]*liveCall{}, in: map[model.AccountID]int{}, ringing: map[model.AccountID][]*ring{}, stop: make(chan struct{})}
	if d.Config.Listen != "" {
		m, err := openMedia(d.Config, d.Log, d.Timings.MediaLost)
		if err != nil {
			return nil, err
		}
		s.media = m
	}
	if err := s.endStale(context.Background()); err != nil {
		if s.media != nil {
			s.media.close()
		}
		return nil, err
	}
	s.done.Add(2)
	go s.tickLoop()
	go s.watchLoop()
	return s, nil
}

// Port is the media port in use, or 0 when the server runs without calls.
func (s *Server) Port() int {
	if s.media == nil {
		return 0
	}
	return s.media.port
}

// endStale ends calls that have no end in the database. Nothing is live
// when the server starts.
func (s *Server) endStale(ctx context.Context) error {
	stale, err := s.store.Calls().AllLive(ctx)
	if err != nil {
		return err
	}
	now := model.Now()
	for _, c := range stale {
		if err := s.finish(ctx, c.ID, c.MessageID, c.StartedAt, now); err != nil {
			return err
		}
		s.log.Info("a call left live by the last run was ended", "call", c.ID)
	}
	return nil
}

// finish closes a call's row and changes its notice from `call_started` to
// `call_ended`. It returns the call's length in seconds.
func (s *Server) finish(ctx context.Context, id model.CallID, msg model.MessageID, startedAt, now model.Time) error {
	return s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		ended, err := tx.Calls().End(ctx, id, now)
		if err != nil || !ended || msg == "" {
			return err
		}
		ids, err := tx.Calls().EverIn(ctx, id, maxNoticeParticipants)
		if err != nil {
			return err
		}
		if ids == nil {
			ids = []model.AccountID{}
		}
		_, err = s.chat.CallNoticeChanged(ctx, tx, rec, msg, map[string]any{
			"kind": "call_ended", "call_id": string(id), "duration_seconds": seconds(startedAt, now), "participant_ids": ids})
		return err
	})
}

func seconds(from, to model.Time) int64 {
	d := int64(to.Sub(from) / time.Second)
	if d < 0 {
		d = 0
	}
	return d
}

// Close ends every call with reason `server`, stops ringing and closes the
// media port. The server is restarting.
func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	all := make([]*liveCall, 0, len(s.byID))
	for _, lc := range s.byID {
		all = append(all, lc)
	}
	s.mu.Unlock()
	close(s.stop)
	s.done.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, lc := range all {
		s.end(ctx, lc, EndedServer)
	}
	s.telling.Wait()
	if s.media != nil {
		s.media.close()
	}
}

// enabled reports whether calls can be made at all: the server has a media
// port and the admin switch is on.
func (s *Server) enabled(ctx context.Context) error {
	if s.media == nil {
		return model.ErrFeatureOff.With("Calls are switched off on this server.")
	}
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return err
	}
	if !set.CallsEnabled {
		return model.ErrFeatureOff.With("Calls are switched off on this server.")
	}
	return nil
}

// limits reads the two limits an admin can change.
func (s *Server) limits(ctx context.Context) (people, cameras int) {
	p, err := store.SettingIntOr(ctx, s.store, store.SettingCallsMaxPeople, 50)
	if err != nil || p < 1 {
		p = 50
	}
	c, err := store.SettingIntOr(ctx, s.store, store.SettingCallsMaxCameras, 12)
	if err != nil || c < 0 {
		c = 12
	}
	return int(p), int(c)
}

func (s *Server) callIn(conv model.ConversationID) *liveCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byConv[conv]
}

func (s *Server) callByID(id model.CallID) *liveCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[id]
}

func (s *Server) allCalls() []*liveCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*liveCall, 0, len(s.byID))
	for _, lc := range s.byID {
		out = append(out, lc)
	}
	return out
}

// object is the call as the API returns it. tracks adds each participant's
// tracks, for copies sent to participants.
func (lc *liveCall) object(tracks bool) model.Call {
	c := model.Call{ID: lc.id, ConversationID: lc.conv, MessageID: lc.messageID, StartedByID: lc.startedBy, StartedAt: lc.startedAt,
		Sealed: lc.sealed, Participants: []model.CallParticipant{}}
	switch {
	case lc.room != nil:
		c.Participants = lc.room.list(tracks)
	case lc.away != nil:
		c.HostedBy = lc.away.host
		c.Participants = lc.away.list()
	}
	return c
}

// LiveCall returns the call in progress in a conversation as someone who is
// not in it sees it, or nil. It answers from memory, so it is safe to call
// inside a transaction. chat calls it to show a conversation.
func (s *Server) LiveCall(_ context.Context, conv model.ConversationID) *model.Call {
	lc := s.callIn(conv)
	if lc == nil {
		return nil
	}
	c := lc.object(false)
	return &c
}

// InCall reports whether an account is in a call right now.
func (s *Server) InCall(account model.AccountID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.in[account] > 0
}

// Count is how many calls are in progress, for the admin overview.
func (s *Server) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byID)
}

// mark changes the count of calls an account is in and tells people.
func (s *Server) mark(account model.AccountID, delta int) {
	s.mu.Lock()
	before := s.in[account] > 0
	s.in[account] += delta
	if s.in[account] <= 0 {
		delete(s.in, account)
	}
	after := s.in[account] > 0
	// A server that is stopping tells nobody: every connection is about
	// to close, and the database with them.
	tell := before != after && s.marks != nil && !s.closed
	if tell {
		// Counted while the lock is held, so Close, which sets closed
		// under the same lock, waits for every one that was started.
		s.telling.Add(1)
	}
	s.mu.Unlock()
	if tell {
		// Off this goroutine: telling everyone who shares a conversation
		// with the account reads the database.
		go func() {
			defer s.telling.Done()
			s.marks.CallChanged(account)
		}()
	}
}

// Get returns the call in progress in a conversation, for any account that
// can read the conversation. nil means there is none.
func (s *Server) Get(ctx context.Context, actor model.Account, conv model.ConversationID) (*model.Call, error) {
	if _, err := s.perm.Can(ctx, actor, perm.Read, conv); err != nil {
		return nil, hide(err)
	}
	return s.LiveCall(ctx, conv), nil
}

// hide gives a missing conversation and a hidden one the same answer.
func hide(err error) error {
	if errors.Is(err, model.ErrNotFound) {
		return model.ErrNotFound
	}
	return err
}

// Join is the call join operation: it checks that the actor may be in the
// conversation's call, starts the call if there is none, and answers with
// the call as it stands and a ticket, good for sixty seconds and one use,
// tied to the account, the session and the call. The client then sends
// `call.join` with the ticket on its live connection.
func (s *Server) Join(ctx context.Context, actor model.Account, session model.Session, conv model.ConversationID) (model.Call, string, error) {
	if s.media == nil {
		return model.Call{}, "", model.ErrFeatureOff.With("Calls are switched off on this server.")
	}
	st, err := s.perm.Can(ctx, actor, perm.StartCall, conv)
	if err != nil {
		return model.Call{}, "", hide(err)
	}
	lc := s.callIn(conv)
	if lc == nil {
		if lc, err = s.start(ctx, actor, st.Conversation); err != nil {
			return model.Call{}, "", err
		}
	}
	if lc.away != nil {
		// Another server carries this call: it gives the pass, and the
		// pass is the ticket (docs/CALLS.md section 16).
		pass, err := s.requestPass(ctx, lc, actor)
		if err != nil {
			return model.Call{}, "", err
		}
		return lc.object(false), pass, nil
	}
	people, _ := s.limits(ctx)
	if lc.room.full(actor.ID, people) {
		return model.Call{}, "", model.ErrConflict.With("The call is full.")
	}
	tk := s.tickets.issue(ticket{call: lc.id, account: actor.ID, session: session.ID, expires: time.Now().Add(s.t.Ticket)})
	return lc.object(false), tk, nil
}

// start makes a new call in a conversation with no call in progress: the
// row, the notice message, the room, `call.started` to every member, and
// the ring. If someone else started one in the same moment, that call is
// returned instead.
func (s *Server) start(ctx context.Context, actor model.Account, conv model.Conversation) (*liveCall, error) {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if lc := s.callIn(conv.ID); lc != nil {
		return lc, nil
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, model.ErrUnavailable.With("The server is restarting.")
	}
	now := model.Now()
	lc := &liveCall{id: model.NewID[model.CallID](), conv: conv.ID, startedBy: actor.ID, startedAt: now, sealed: conv.Sealed(),
		public: conv.Public(), shared: conv.IsShared(), created: time.Now(), over: make(chan struct{})}
	err := s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		row := store.StoredCall{ID: lc.id, ConversationID: conv.ID, StartedBy: actor.ID, StartedAt: now}
		started, err := tx.Calls().Start(ctx, row)
		if err != nil {
			return err
		}
		if !started {
			// A live row this server holds no room for: left by a call
			// that could not be closed. It is over; close it and go on.
			old, found, err := tx.Calls().Live(ctx, conv.ID)
			if err != nil {
				return err
			}
			if found {
				if _, err := tx.Calls().End(ctx, old.ID, now); err != nil {
					return err
				}
			}
			if started, err = tx.Calls().Start(ctx, row); err != nil {
				return err
			}
			if !started {
				return model.ErrConflict.With("A call is already starting here. Try again.")
			}
		}
		msg, err := s.chat.CallNotice(ctx, tx, rec, conv, actor.ID, map[string]any{"kind": "call_started", "call_id": string(lc.id)})
		if err != nil {
			return err
		}
		lc.messageID = msg.ID
		return tx.Calls().SetMessage(ctx, lc.id, msg.ID)
	})
	if err != nil {
		return nil, err
	}
	lc.room = newRoom(s, lc)
	s.mu.Lock()
	s.byConv[conv.ID], s.byID[lc.id] = lc, lc
	s.mu.Unlock()
	s.log.Info("call started", "call", lc.id, "conversation", conv.ID)
	call := lc.object(false)
	s.hub.Publish(ctx, model.Event{Type: EventStarted, At: now, ConversationID: conv.ID, Payload: map[string]any{"call": call}})
	s.submit(ctx, lc, StreamEvent{Type: EventStarted, At: now, Call: &call})
	s.ring(ctx, lc, call)
	return lc, nil
}

// end ends a call: the room closes, the row gets its end, the notice
// changes to `call_ended`, and every member of the conversation is told.
func (s *Server) end(ctx context.Context, lc *liveCall, reason string) {
	// What ends here is ended whatever becomes of the request that caused
	// it: see removeParticipant.
	ctx = context.WithoutCancel(ctx)
	lc.endOnce.Do(func() {
		s.startMu.Lock()
		defer s.startMu.Unlock()
		close(lc.over)
		s.mu.Lock()
		if s.byID[lc.id] == lc {
			delete(s.byID, lc.id)
		}
		if s.byConv[lc.conv] == lc {
			delete(s.byConv, lc.conv)
		}
		s.mu.Unlock()
		now := model.Now()
		var were []model.AccountID
		if lc.room != nil {
			were = lc.room.close()
		}
		if lc.away != nil {
			were = lc.away.close()
		}
		for _, id := range were {
			s.mark(id, -1)
		}
		if lc.room != nil {
			if err := s.finish(ctx, lc.id, lc.messageID, lc.startedAt, now); err != nil {
				s.log.Error("a call's end could not be stored", "call", lc.id, "err", err)
			}
		} else if _, err := s.store.Calls().End(ctx, lc.id, now); err != nil {
			// The host changes the notice; this server only closes its row.
			s.log.Error("a call's end could not be stored", "call", lc.id, "err", err)
		}
		length := seconds(lc.startedAt, now)
		s.hub.Publish(ctx, model.Event{Type: EventEnded, At: now, ConversationID: lc.conv,
			Payload: map[string]any{"call_id": lc.id, "reason": reason, "duration_seconds": length}})
		if lc.room != nil {
			s.submit(ctx, lc, StreamEvent{Type: EventEnded, At: now, CallID: lc.id, Reason: reason, DurationSeconds: length})
		}
		s.stopRings(lc.id, "", RingEnded, "")
		s.log.Info("call ended", "call", lc.id, "reason", reason, "seconds", length, "participants", len(were))
	})
}

// Leave is the leave operation: the same as the `call.leave` frame, for a
// client whose live connection has gone.
func (s *Server) Leave(ctx context.Context, actor model.Account, conv model.ConversationID) error {
	lc := s.callIn(conv)
	if lc == nil {
		return model.ErrNotFound.With("There is no call here that you are in.")
	}
	if lc.away != nil {
		if !s.leaveAway(ctx, lc, actor.ID) {
			return model.ErrNotFound.With("There is no call here that you are in.")
		}
		return nil
	}
	if !s.removeParticipant(ctx, lc, actor.ID, LeftLeft) {
		return model.ErrNotFound.With("There is no call here that you are in.")
	}
	return nil
}

// removeParticipant takes an account out of a call this server carries,
// tells the conversation, and ends the call if it was the last. It reports
// false when the account was not in the call.
func (s *Server) removeParticipant(ctx context.Context, lc *liveCall, account model.AccountID, reason string) bool {
	// A client that leaves closes its connection as soon as it is answered,
	// which cancels the context its `call.leave` came with. The leaving,
	// and the call's end if this was the last one in it, are stored and
	// told all the same: the participant is already out of the room.
	ctx = context.WithoutCancel(ctx)
	p, left := lc.room.remove(account)
	if p == nil {
		return false
	}
	now := model.Now()
	lc.room.mu.Lock()
	conn := p.conn
	lc.room.mu.Unlock()
	s.unbind(conn, lc)
	p.shut()
	if _, err := s.store.Calls().Leave(ctx, lc.id, account, now); err != nil {
		s.log.Error("a participant's leaving could not be stored", "call", lc.id, "err", err)
	}
	s.mark(account, -1)
	// The one who left hears of it too, even if it is no longer a member
	// of the conversation.
	s.hub.Publish(ctx, model.Event{Type: EventParticipantLeft, At: now, ConversationID: lc.conv, AlsoTo: []model.AccountID{account},
		Payload: map[string]any{"call_id": lc.id, "account_id": account, "reason": reason}})
	s.submit(ctx, lc, StreamEvent{Type: EventParticipantLeft, At: now, CallID: lc.id, AccountID: account, Reason: reason})
	lc.room.kickAll()
	if left == 0 {
		s.end(ctx, lc, EndedEmpty)
	}
	return true
}

// event builds one of the events that go to a call's participants: `type`,
// `at`, `conversation_id` and `call_id`, with no `seq`.
func event(eventType string, lc *liveCall, fields map[string]any) model.Event {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["call_id"] = lc.id
	return model.Event{Type: eventType, At: model.Now(), ConversationID: lc.conv, Payload: fields}
}

// tickLoop looks at every room a few times a second: who has dropped, who
// is alone, which call has run too long, and who is speaking.
func (s *Server) tickLoop() {
	defer s.done.Done()
	t := time.NewTicker(s.t.Tick)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-t.C:
			for _, lc := range s.allCalls() {
				s.tick(lc, now)
			}
		}
	}
}

func (s *Server) tick(lc *liveCall, now time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if lc.away != nil {
		if now.Sub(s.hostHeard(lc)) >= s.t.HostSilence {
			// Nothing from the host for too long: the call is over here.
			s.end(ctx, lc, EndedServer)
		}
		return
	}
	v := lc.room.tick(now, s.t)
	for _, id := range v.dropped {
		s.removeParticipant(ctx, lc, id, LeftTimeout)
	}
	switch {
	case v.count == 0 && now.Sub(lc.created) >= s.t.Unjoined:
		s.end(ctx, lc, EndedEmpty)
	case now.Sub(lc.created) >= s.t.Longest:
		s.end(ctx, lc, EndedTooLong)
	case v.alone >= s.t.Alone:
		s.end(ctx, lc, EndedAlone)
	}
}

// watchLoop checks, every few seconds, that each participant may still be
// in its call: still a member at level `member` or above, not timed out,
// its session not ended; and that the conversation and calls themselves are
// still there.
func (s *Server) watchLoop() {
	defer s.done.Done()
	t := time.NewTicker(s.t.Watch)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			for _, lc := range s.allCalls() {
				s.watch(lc)
			}
		}
	}
}

func (s *Server) watch(lc *liveCall) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.enabled(ctx); errors.Is(err, model.ErrFeatureOff) {
		s.end(ctx, lc, EndedServer)
		return
	}
	conv, err := s.store.Conversations().Get(ctx, lc.conv)
	if errors.Is(err, model.ErrNotFound) || (err == nil && conv.Archived()) {
		s.end(ctx, lc, EndedClosed)
		return
	}
	if err != nil || lc.room == nil {
		return
	}
	for _, who := range lc.room.who() {
		acc, err := s.store.Accounts().Get(ctx, who.account)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			continue // a database failure removes nobody
		}
		if err != nil {
			s.removeParticipant(ctx, lc, who.account, LeftRemoved)
			continue
		}
		if who.remote != "" {
			// Someone on another server: this server's own copy of the
			// conversation says whether they may stay. Their sessions
			// are their own server's to watch.
			err = s.perm.CanJoinCallRemote(ctx, acc, lc.conv)
		} else {
			_, err = s.perm.Can(ctx, acc, perm.StartCall, lc.conv)
		}
		if err != nil {
			if _, known := model.AsError(err); known {
				s.removeParticipant(ctx, lc, who.account, LeftRemoved)
			}
			continue
		}
		if who.remote != "" || who.session == "" {
			continue
		}
		ses, err := s.store.Sessions().Get(ctx, who.session)
		gone := errors.Is(err, model.ErrNotFound)
		if err == nil {
			gone = !ses.RevokedAt.IsZero() || (!ses.ExpiresAt.IsZero() && ses.ExpiresAt.Before(model.Now()))
		}
		if gone {
			s.removeParticipant(ctx, lc, who.account, LeftSignedOut)
		}
	}
}
