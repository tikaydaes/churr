package calls

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Calls in a conversation shared with other servers (docs/CALLS.md section
// 16, docs/LINK-PROTOCOL.md section 18). The call lives on the server of
// whoever started it, the host. A member of another server asks its own
// server to join; that server gets a pass from the host over the link,
// hands it to the client as the ticket, and from then on relays the
// client's signalling to the host and the host's back. Media goes straight
// from the member's device to the host's media port.
//
// The link package owns the transport. This file says what calls needs of
// it (Link), what it must call here when something arrives (PassRequested,
// Signalled, StreamEvent), and holds both sides of the exchange.

// Link is what calls needs of the link between servers. The link package
// implements it; SetLink hands it over.
type Link interface {
	// RequestPass sends the host a `call_pass_request` for a local
	// account and waits for its `call_pass`. An `error` answer comes back
	// as a *model.Error with the answer's code; a host that cannot be
	// reached as any other error.
	RequestPass(ctx context.Context, host string, req PassRequest) (Pass, error)
	// Signal sends one `call_signal` to a server as a sealed direct
	// frame: not numbered, never stored. It returns an error when the
	// server cannot be reached now; nothing is held for later.
	Signal(ctx context.Context, to string, sig Signal) error
	// Submit submits a call event of a call this server hosts to the
	// conversation's stream, in the order given. It must not wait for
	// the network: the stream's outbox is what carries it.
	Submit(ctx context.Context, conv model.ConversationID, e StreamEvent) error
	// LastHeard is when anything last arrived from a server over the
	// link, a keep-alive included. The zero time means never.
	LastHeard(server string) time.Time
}

// PassRequest is the body of `call_pass_request`: "my account wants to join
// this call". The link names the conversation by its stream on the wire and
// by its local identifier here.
type PassRequest struct {
	ConversationID model.ConversationID
	CallID         model.CallID
	AccountID      model.AccountID
}

// Pass is the body of `call_pass`: 32 random bytes, good for sixty seconds
// and one use, tied to the account and the call.
type Pass struct {
	CallID    model.CallID
	AccountID model.AccountID
	Pass      string
	ExpiresAt model.Time
	Media     PassMedia
}

// PassMedia says where the host takes media.
type PassMedia struct {
	Host    string `json:"host"`
	UDPPort int    `json:"udp_port"`
	TCPPort int    `json:"tcp_port"`
}

// Signal is the body of `call_signal`, in either direction. Signal is the
// same object a client and its own server exchange for a local call. The
// first one from a member's server carries the pass.
type Signal struct {
	ConversationID model.ConversationID
	CallID         model.CallID
	AccountID      model.AccountID
	Pass           string
	Signal         json.RawMessage
}

// StreamEvent is one of the four call events a host submits to a shared
// conversation's stream: call.started, call.participant_joined,
// call.participant_left and call.ended.
type StreamEvent struct {
	Type string
	At   model.Time
	// Call is the call, on call.started.
	Call *model.Call
	// CallID names the call on the other three.
	CallID model.CallID
	// AccountID and Participant: who joined or left.
	AccountID   model.AccountID
	Participant *model.CallParticipant
	// Reason is why a participant left or the call ended.
	Reason          string
	DurationSeconds int64
}

// SetLink gives the service the link between servers. Without one, calls in
// shared conversations are local to this server.
func (s *Server) SetLink(l Link) {
	s.mu.Lock()
	s.link = l
	s.mu.Unlock()
}

func (s *Server) theLink() Link {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.link
}

// submit sends a call event of a call this server hosts to the stream of a
// shared conversation.
func (s *Server) submit(ctx context.Context, lc *liveCall, e StreamEvent) {
	l := s.theLink()
	if l == nil || !lc.shared || lc.room == nil {
		return
	}
	if err := l.Submit(ctx, lc.conv, e); err != nil {
		s.log.Warn("a call event could not be sent to the other servers", "call", lc.id, "type", e.Type, "err", err)
	}
}

// ---- The host's side.

// PassRequested answers a `call_pass_request` from another server. The
// link calls it with the name of the server that asked, which it has
// already authenticated. The host checks that the call is live here, that
// the account's home server is the one asking, and that the account is a
// member of the conversation at level `member` or above in its own copy.
// The error, if any, is a *model.Error to send back as the `error` answer.
func (s *Server) PassRequested(ctx context.Context, from string, req PassRequest) (Pass, error) {
	if err := s.enabled(ctx); err != nil {
		return Pass{}, err
	}
	lc := s.callByID(req.CallID)
	if lc == nil || lc.room == nil || (req.ConversationID != "" && lc.conv != req.ConversationID) {
		return Pass{}, model.ErrNotFound.With("That call is not in progress here.")
	}
	acc, err := s.store.Accounts().Get(ctx, req.AccountID)
	if errors.Is(err, model.ErrNotFound) || (err == nil && (acc.Local() || acc.ServerName != from)) {
		// Only an account's own server may ask for it.
		return Pass{}, model.ErrForbidden.With("That account does not belong to the server asking.")
	}
	if err != nil {
		return Pass{}, err
	}
	if err := s.perm.CanJoinCallRemote(ctx, acc, lc.conv); err != nil {
		return Pass{}, err
	}
	people, _ := s.limits(ctx)
	if lc.room.full(acc.ID, people) {
		return Pass{}, model.ErrConflict.With("The call is full.")
	}
	expires := time.Now().Add(s.t.Ticket)
	pass := s.tickets.issue(ticket{call: lc.id, account: acc.ID, server: from, expires: expires})
	return Pass{CallID: lc.id, AccountID: acc.ID, Pass: pass, ExpiresAt: model.Time(expires.UnixMilli()),
		Media: PassMedia{Host: s.cfg.Hostname, UDPPort: s.media.port, TCPPort: s.media.port}}, nil
}

// Signalled takes a `call_signal` that arrived from another server. If this
// server hosts the call, it is a frame from a member of that server; if
// that server hosts it, it is the host's frame for one of this server's own
// clients. Anything else is dropped: the host answers no signal for an
// account without a pass.
func (s *Server) Signalled(ctx context.Context, from string, sig Signal) {
	lc := s.callByID(sig.CallID)
	if lc == nil {
		return
	}
	if lc.away != nil {
		if lc.away.host == from {
			lc.away.touch()
			s.deliver(lc, sig.AccountID, sig.Signal)
		}
		return
	}
	var head struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if json.Unmarshal(sig.Signal, &head) != nil || head.Type == "" {
		return
	}
	c := caller{account: sig.AccountID, server: from}
	q := request{c: c, id: head.ID}
	switch head.Type {
	case frameJoin:
		// The pass is the ticket. Until it has been checked there is no
		// participant, and so nothing to answer through but this.
		c.out = s.sinkTo(lc, from, sig.AccountID)
		q.c = c
	case frameDecline:
		return // ringing is each server's own
	default:
		p := lc.room.get(sig.AccountID)
		if p == nil {
			return
		}
		lc.room.mu.Lock()
		theirs := p.remote == from
		lc.room.mu.Unlock()
		if !theirs {
			return
		}
		if head.Type == frameResume {
			c.out = s.sinkTo(lc, from, sig.AccountID)
		} else {
			p.outMu.Lock()
			c.out = p.out
			p.outMu.Unlock()
		}
		q.c = c
	}
	if q.c.out == nil {
		return
	}
	s.process(ctx, lc, q, head.Type, sig.Signal)
}

// remoteSink carries the host's frames for one participant back to that
// participant's own server, in order, without making anything wait for the
// link.
type remoteSink struct {
	s       *Server
	to      string
	conv    model.ConversationID
	call    model.CallID
	account model.AccountID
	queue   chan any
}

// sinkTo returns the way back to one account on one server, making it the
// first time. There is one per server and account for the life of the call,
// however many times that server sends `call.join` for the account.
func (s *Server) sinkTo(lc *liveCall, to string, account model.AccountID) *remoteSink {
	key := to + "|" + string(account)
	lc.sinkMu.Lock()
	defer lc.sinkMu.Unlock()
	if r := lc.sinks[key]; r != nil {
		return r
	}
	r := &remoteSink{s: s, to: to, conv: lc.conv, call: lc.id, account: account, queue: make(chan any, 256)}
	if lc.sinks == nil {
		lc.sinks = map[string]*remoteSink{}
	}
	lc.sinks[key] = r
	go r.run(lc.over)
	return r
}

func (r *remoteSink) send(v any) {
	select {
	case r.queue <- v:
	default:
		// The link is not keeping up. Signalling for this participant is
		// lost; its client will find out and join again.
		r.s.log.Warn("signalling for a participant on another server was dropped", "call", r.call, "server", r.to)
	}
}

// run sends what is queued until the call is over, and then what is left:
// the last frames of a call are the ones that say it ended.
func (r *remoteSink) run(over <-chan struct{}) {
	deliver := func(v any) {
		raw, err := json.Marshal(v)
		l := r.s.theLink()
		if err != nil || l == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := l.Signal(ctx, r.to, Signal{ConversationID: r.conv, CallID: r.call, AccountID: r.account, Signal: raw}); err != nil {
			r.s.log.Warn("a call signal could not be sent over the link", "call", r.call, "server", r.to, "err", err)
		}
	}
	for {
		select {
		case v := <-r.queue:
			deliver(v)
		case <-over:
			for {
				select {
				case v := <-r.queue:
					deliver(v)
				default:
					return
				}
			}
		}
	}
}

// ---- The other servers' side.

// awayCall is what a server knows of a call another server hosts: who is in
// it, and which of its own clients are in it through which connection.
type awayCall struct {
	host   string
	hostID model.ServerID

	mu    sync.Mutex
	parts map[model.AccountID]model.CallParticipant
	// relays are this server's own accounts in the call: the live
	// connection each one's signalling is relayed for.
	relays map[model.AccountID]relayed
	// entering are connections whose `call.join` or `call.resume` has
	// gone to the host and not been answered yet. Only the host knows
	// whether the pass or the token was good.
	entering map[model.AccountID]relayed
	// left are this server's own accounts that the stream has said are out
	// of the call, each with the connection it was relayed for: the host's
	// answer to the `call.leave` that took it out may still be on its way
	// (deliver).
	left map[model.AccountID]relayed
	last time.Time
}

type relayed struct {
	conn    string
	session model.SessionID
	// request is the `id` of the join or resume frame, while entering.
	request string
}

func (a *awayCall) list() []model.CallParticipant {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]model.CallParticipant, 0, len(a.parts))
	for _, p := range a.parts {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].JoinedAt != out[j].JoinedAt {
			return out[i].JoinedAt < out[j].JoinedAt
		}
		return out[i].AccountID < out[j].AccountID
	})
	return out
}

func (a *awayCall) touch() {
	a.mu.Lock()
	a.last = time.Now()
	a.mu.Unlock()
}

func (a *awayCall) heard() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.last
}

func (a *awayCall) close() []model.AccountID {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := make([]model.AccountID, 0, len(a.parts))
	for id := range a.parts {
		ids = append(ids, id)
	}
	a.parts, a.relays, a.entering = map[model.AccountID]model.CallParticipant{}, map[model.AccountID]relayed{}, map[model.AccountID]relayed{}
	a.left = nil
	return ids
}

// deliver passes one of the host's frames on to the client it is for. The
// answer to a `call.join` or `call.resume` goes to the connection that sent
// it, and only a `call.ok` makes that connection the account's signalling
// connection: an account is in a call on one device, and the device it was
// on before is told the call moved.
func (s *Server) deliver(lc *liveCall, account model.AccountID, frame json.RawMessage) {
	a := lc.away
	var head struct {
		Type    string `json:"type"`
		ReplyTo string `json:"reply_to"`
	}
	json.Unmarshal(frame, &head)
	a.mu.Lock()
	rl, relaying := a.relays[account]
	if in, ok := a.entering[account]; ok && head.ReplyTo == in.request && (head.Type == "call.ok" || head.Type == "call.error") {
		delete(a.entering, account)
		if head.Type == "call.ok" {
			in.request = ""
			a.relays[account] = in
			delete(a.left, account)
		}
		a.mu.Unlock()
		if head.Type == "call.ok" {
			if relaying && rl.conn != in.conn {
				s.unbind(rl.conn, lc)
				s.hub.SendTo(account, rl.conn, event(EventParticipantLeft, lc, map[string]any{"account_id": account, "reason": LeftJoinedElsewhere}))
			}
			s.bind(in.conn, lc)
			s.stopRings(lc.id, account, RingJoinedElsewhere, in.conn)
		}
		s.hub.SendTo(account, in.conn, frame)
		return
	}
	// The host answers a `call.leave` and then says in the stream that the
	// participant has left. The two travel separately, and when the stream
	// was the quicker the answer found nobody to give it to: the client
	// that had asked to leave was never answered. An answer still goes to
	// the connection that asked.
	gone, wasIn := a.left[account]
	a.mu.Unlock()
	switch {
	case relaying:
		s.hub.SendTo(account, rl.conn, frame)
	case wasIn && head.ReplyTo != "":
		s.hub.SendTo(account, gone.conn, frame)
	}
}

// hostHeard is when the host was last heard from: a signal or event about
// this call, or anything at all over the link.
func (s *Server) hostHeard(lc *liveCall) time.Time {
	last := lc.away.heard()
	if l := s.theLink(); l != nil {
		if t := l.LastHeard(lc.away.host); t.After(last) {
			last = t
		}
	}
	return last
}

// requestPass asks the host of a call for a pass for one of this server's
// accounts. The pass is what the client is given as its ticket.
func (s *Server) requestPass(ctx context.Context, lc *liveCall, actor model.Account) (string, error) {
	l := s.theLink()
	if l == nil {
		return "", model.ErrUnavailable.With("The server hosting this call cannot be reached.")
	}
	pass, err := l.RequestPass(ctx, lc.away.host, PassRequest{ConversationID: lc.conv, CallID: lc.id, AccountID: actor.ID})
	if err != nil {
		if e, ok := model.AsError(err); ok {
			// The host's own refusal: full, no longer a member there,
			// calls switched off.
			return "", e
		}
		s.log.Warn("a pass could not be had from the server hosting a call", "call", lc.id, "host", lc.away.host, "err", err)
		return "", model.ErrUnavailable.With("The server hosting this call cannot be reached.")
	}
	lc.away.touch()
	return pass.Pass, nil
}

// relay passes a client's frame on to the host of its call.
func (s *Server) relay(ctx context.Context, lc *liveCall, q request, frameType string, frame json.RawMessage) {
	a := lc.away
	c := q.c
	sig := Signal{ConversationID: lc.conv, CallID: lc.id, AccountID: c.account, Signal: frame}
	switch frameType {
	case frameJoin, frameResume:
		if q.id == "" {
			q.fail(errInvalidRequest, "A request has an `id`.")
			return
		}
		if frameType == frameJoin {
			var f struct {
				Ticket string `json:"ticket"`
			}
			json.Unmarshal(frame, &f)
			sig.Pass = f.Ticket
		}
		// The host's answer decides whether this connection becomes the
		// account's signalling connection (deliver).
		a.mu.Lock()
		a.entering[c.account] = relayed{conn: c.conn, session: c.session, request: q.id}
		a.mu.Unlock()
	default:
		a.mu.Lock()
		rl, ok := a.relays[c.account]
		a.mu.Unlock()
		if !ok || rl.conn != c.conn {
			q.fail(errNotInCall, "You are not in this call.")
			return
		}
	}
	l := s.theLink()
	if l == nil {
		q.fail(errHostUnreachable, "The server hosting this call cannot be reached.")
		return
	}
	if err := l.Signal(ctx, a.host, sig); err != nil {
		q.fail(errHostUnreachable, "The server hosting this call cannot be reached.")
	}
}

// relayLost is called when the live connection of a relayed participant
// closes. The host holds the participant as it would one of its own, and is
// told so with a frame only servers send each other.
func (s *Server) relayLost(lc *liveCall, account model.AccountID, conn string) {
	a := lc.away
	a.mu.Lock()
	rl, ok := a.relays[account]
	a.mu.Unlock()
	if !ok || rl.conn != conn {
		return
	}
	s.tellHost(lc, account, map[string]any{"type": frameDisconnected})
}

func (s *Server) tellHost(lc *liveCall, account model.AccountID, frame map[string]any) bool {
	l := s.theLink()
	if l == nil {
		return false
	}
	raw, _ := json.Marshal(frame)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return l.Signal(ctx, lc.away.host, Signal{ConversationID: lc.conv, CallID: lc.id, AccountID: account, Signal: raw}) == nil
}

// leaveAway is the leave operation for a call another server hosts.
func (s *Server) leaveAway(ctx context.Context, lc *liveCall, account model.AccountID) bool {
	a := lc.away
	a.mu.Lock()
	rl, ok := a.relays[account]
	delete(a.relays, account)
	a.mu.Unlock()
	if !ok {
		return false
	}
	s.unbind(rl.conn, lc)
	s.tellHost(lc, account, map[string]any{"type": frameLeave, "call_id": lc.id})
	return true
}

// StreamEvent takes a call event that arrived on a shared conversation's
// stream from the server hosting the call. The link calls it, after the
// stream's own rules have passed the event (docs/LINK-PROTOCOL.md section
// 12): `call.started` only from the home server of whoever started the
// call and only when no call is live in the conversation, the other three
// only from the call's host. hostID is that server's row in `servers`.
//
// Each server tells its own members, and decides its own ringing from its
// own copy of each person's mode and do not disturb.
func (s *Server) StreamEvent(ctx context.Context, host string, hostID model.ServerID, conv model.ConversationID, e StreamEvent) {
	switch e.Type {
	case EventStarted:
		if e.Call == nil {
			return
		}
		if mine := s.callIn(conv); mine != nil {
			if mine.id == e.Call.ID {
				return
			}
			// Two people on different servers started at the same
			// moment, and the stream put this one first: it is the
			// call. The local one ends, and members are offered the
			// real one.
			s.end(ctx, mine, EndedServer)
		}
		s.adopt(ctx, host, hostID, conv, *e.Call)
	case EventParticipantJoined, EventParticipantLeft, EventEnded:
		lc := s.callByID(e.CallID)
		if lc == nil || lc.away == nil || lc.away.host != host || lc.conv != conv {
			return
		}
		a := lc.away
		a.touch()
		switch e.Type {
		case EventParticipantJoined:
			who := model.CallParticipant{AccountID: e.AccountID, JoinedAt: e.At}
			if e.Participant != nil {
				who = *e.Participant
				who.Tracks = nil
			}
			a.mu.Lock()
			_, had := a.parts[who.AccountID]
			a.parts[who.AccountID] = who
			a.mu.Unlock()
			if !had {
				s.mark(who.AccountID, 1)
			}
			s.hub.Publish(ctx, model.Event{Type: EventParticipantJoined, At: e.At, ConversationID: conv,
				Payload: map[string]any{"call_id": lc.id, "participant": who}})
		case EventParticipantLeft:
			a.mu.Lock()
			_, had := a.parts[e.AccountID]
			delete(a.parts, e.AccountID)
			rl, relaying := a.relays[e.AccountID]
			delete(a.relays, e.AccountID)
			if relaying {
				if a.left == nil {
					a.left = map[model.AccountID]relayed{}
				}
				a.left[e.AccountID] = rl
			}
			a.mu.Unlock()
			if relaying {
				s.unbind(rl.conn, lc)
			}
			if had {
				s.mark(e.AccountID, -1)
			}
			reason := e.Reason
			if reason == "" {
				reason = LeftLeft
			}
			s.hub.Publish(ctx, model.Event{Type: EventParticipantLeft, At: e.At, ConversationID: conv, AlsoTo: []model.AccountID{e.AccountID},
				Payload: map[string]any{"call_id": lc.id, "account_id": e.AccountID, "reason": reason}})
		case EventEnded:
			reason := e.Reason
			if reason == "" {
				reason = EndedEmpty
			}
			s.end(ctx, lc, reason)
		}
	}
}

// adopt records a call another server has started in a shared conversation:
// a row with the host's server, the call in memory, `call.started` to this
// server's own members, and this server's own ring.
func (s *Server) adopt(ctx context.Context, host string, hostID model.ServerID, conv model.ConversationID, call model.Call) {
	s.startMu.Lock()
	lc := &liveCall{id: call.ID, conv: conv, messageID: call.MessageID, startedBy: call.StartedByID, startedAt: call.StartedAt, sealed: call.Sealed,
		created: time.Now(), over: make(chan struct{}), away: &awayCall{host: host, hostID: hostID, parts: map[model.AccountID]model.CallParticipant{},
			relays: map[model.AccountID]relayed{}, entering: map[model.AccountID]relayed{}, last: time.Now()}}
	row := store.StoredCall{ID: call.ID, ConversationID: conv, MessageID: call.MessageID, StartedBy: call.StartedByID, StartedAt: call.StartedAt, HostServerID: hostID}
	started, err := s.store.Calls().Start(ctx, row)
	if err == nil && !started {
		// A row left live by a call that is over here.
		if old, found, lerr := s.store.Calls().Live(ctx, conv); lerr == nil && found && old.ID != call.ID {
			s.store.Calls().End(ctx, old.ID, model.Now())
			_, err = s.store.Calls().Start(ctx, row)
		}
	}
	if err != nil {
		s.startMu.Unlock()
		s.log.Error("a call hosted by another server could not be recorded", "call", call.ID, "err", err)
		return
	}
	for _, p := range call.Participants {
		p.Tracks = nil
		lc.away.parts[p.AccountID] = p
	}
	s.mu.Lock()
	s.byConv[conv], s.byID[lc.id] = lc, lc
	s.mu.Unlock()
	s.startMu.Unlock()
	for _, p := range call.Participants {
		s.mark(p.AccountID, 1)
	}
	obj := lc.object(false)
	s.hub.Publish(ctx, model.Event{Type: EventStarted, At: call.StartedAt, ConversationID: conv, Payload: map[string]any{"call": obj}})
	s.ring(ctx, lc, obj)
}
