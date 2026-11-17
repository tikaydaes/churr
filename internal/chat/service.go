// Package chat is the conversation service. Every change to a conversation
// goes through it, whoever asked: a handler, a bot, a scheduled job, the
// importer or another server. It checks perm, makes the change with its
// event in one transaction, and publishes the event to the live hub.
//
// Service.Tx is how any package makes a change that produces conversation
// events; ../server/README.md describes the pattern.
package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/timing"
)

// Hub is what chat needs of the live hub. live.Hub implements it; tests may
// use something smaller.
type Hub interface {
	Publish(ctx context.Context, e model.Event)
	Send(account model.AccountID, v any)
	Presence(account model.AccountID) model.Presence
	Tailored(ctx context.Context, e model.Event, to model.AccountID) json.RawMessage
}

// Blobs is what chat needs of the file store: removing the content of files
// whose rows are gone.
type Blobs interface {
	Delete(ctx context.Context, key string) error
}

// Moderator is the hook the mod package implements: checks made when
// someone posts, after perm has said yes (slow mode, the word filter). A nil
// Moderator allows everything. moderation.go says how it is called.
type Moderator interface {
	// Check is made before a new message is stored: slow mode, then the
	// word filter.
	Check(ctx context.Context, st perm.State, text string) error
	// CheckEdit is made before an edit is stored: the word filter only.
	CheckEdit(ctx context.Context, st perm.State, text string) error
	// Stored is called in the transaction that stored a new message or an
	// edit: it starts the slow-mode clock and files the word filter's
	// report. What it returns is run after the commit.
	Stored(ctx context.Context, tx store.Tx, st perm.State, m model.Message, edit bool) (after func(), err error)
}

// Deps is what a Service is built from.
type Deps struct {
	Store store.Store
	Perm  *perm.Checker
	Hub   Hub
	Blobs Blobs
	Mod   Moderator
	Log   *slog.Logger
}

// Service is the conversation service.
type Service struct {
	store store.Store
	perm  *perm.Checker
	hub   Hub
	// ownersTold is told when an override is used (SetOwnersTold).
	ownersTold func(ctx context.Context, kind string, by model.AccountID, about string)
	blobs      Blobs
	mod        Moderator
	log        *slog.Logger
	now        func() model.Time

	// order makes events of one conversation reach the hub in seq order;
	// see Recorder.
	orderMu sync.Mutex
	order   map[model.ConversationID][]*turn

	// mentions caches the mention rows of the event being fanned out, so
	// working out `notify` for a thousand recipients is one query.
	mentionsMu  sync.Mutex
	mentionsFor model.MessageID
	mentions    map[model.AccountID]string

	// fresh holds, for a message Post has stored and not yet published,
	// what Post knows of it that the fan-out would otherwise read back
	// (*notify.Known, by message id). Post puts it there and takes it
	// away again if the message is never published.
	fresh sync.Map

	// previews is the preview package, set with SetPreviewer (previews.go).
	previews Previewer
	// themeLinks makes theme cards, set with SetThemeLinks (themecards.go).
	themeLinks ThemeLinks
	// origin is the server's own origin, for links to its messages
	// (messagelinks.go).
	origin string
	// tmpDir is where uploads are held while they are looked at, set with
	// SetTmpDir (files.go); empty means the system's.
	tmpDir string

	// forms is the forms package, set with SetForms (forms.go).
	forms Forms

	// notifier is the notify package, set with SetNotifier (notifier.go).
	notifier Notifier
	// calls is the calls package, set with SetCalls (calls.go).
	calls LiveCalls
	// outbound is the link package, set with SetOutbound (shared.go).
	outbound Outbound
	// badges collects, while an event is fanned out, who it counted for.
	badgesMu sync.Mutex
	badges   map[model.MessageID][]model.AccountID
	// fan is what is held while events are fanned out (fanout.go).
	fan fanState

	// broadLimit and addLimit are the per-person limits on broad mentions
	// and on adding people (limits.go).
	broadLimit, addLimit hourly

	// waker is the push package and sealed the sealed package, set with
	// SetWaker and SetSealed (sealed.go).
	waker  Waker
	sealed SealedChats

	// remoteAround is told when another server says one of its accounts
	// is online, set with OnRemoteAround (watch.go).
	remoteAround func(account model.AccountID)

	// showAccount shows the person asked who is asking for a chat, set
	// with SetAccountShower (requests.go).
	showAccount AccountShower
	// requestSkew moves the clock of chat requests' waits and caps, in
	// nanoseconds, for tests (requests.go).
	requestSkew atomic.Int64
}

// turn is one transaction's place in the line of a conversation: the
// transactions that have taken a seq there and not yet published.
type turn struct {
	// first is closed when the turn reaches the head of the line.
	first chan struct{}
}

// New makes the service.
func New(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Service{store: d.Store, perm: d.Perm, hub: d.Hub, blobs: d.Blobs, mod: d.Mod, log: d.Log,
		now: model.Now, order: map[model.ConversationID][]*turn{}}
}

// joinLine puts a transaction at the end of a conversation's line. It never
// waits. The caller holds the conversation's row lock, so the places in the
// line are in seq order.
func (s *Service) joinLine(conv model.ConversationID) *turn {
	t := &turn{first: make(chan struct{})}
	s.orderMu.Lock()
	if len(s.order[conv]) == 0 {
		close(t.first)
	}
	s.order[conv] = append(s.order[conv], t)
	s.orderMu.Unlock()
	return t
}

// leaveLine takes a transaction out of a conversation's line, wherever it
// stands (one that was rolled back leaves from the middle), and lets the
// next one through if it was at the head.
func (s *Service) leaveLine(conv model.ConversationID, t *turn) {
	s.orderMu.Lock()
	defer s.orderMu.Unlock()
	line := s.order[conv]
	for i, other := range line {
		if other != t {
			continue
		}
		line = append(line[:i], line[i+1:]...)
		if len(line) == 0 {
			delete(s.order, conv)
			return
		}
		s.order[conv] = line
		if i == 0 {
			close(line[0].first)
		}
		return
	}
}

// Recorder collects, inside one transaction, the conversation events the
// transaction produces, and what must happen once it has committed.
//
// It also keeps a conversation's events in order on the way out. Taking a
// seq locks the conversation's row until commit, so numbers are handed out
// in commit order; but two goroutines that have both committed could still
// reach the hub in either order. So the first Emit for a conversation also
// joins that conversation's line, without waiting, and once the transaction
// has committed it publishes only when everything ahead of it in the line
// has been published.
//
// Nothing waits for another writer's fan-out while its own transaction is
// open. A transaction that did would keep its database connection, and the
// row lock of the conversation, for as long as a fan-out to thousands of
// people takes; with a few writers in one busy channel that uses up the
// connection pool, the fan-out in front can no longer read what it needs,
// and everything stops until requests time out.
type Recorder struct {
	s      *Service
	tx     store.Tx
	events []model.Event
	held   map[model.ConversationID]*turn
	blobs  []string
	after  []func()
	// shared is set when the link itself runs the transaction (shared.go).
	shared *sharedTx
	// stamps holds, for each event emitted in a shared conversation, where
	// it stands in that conversation's order (shared.go).
	stamps map[stampKey]Stamp
	// founding is set while a channel is being created: the people it
	// starts with are not announced as newcomers.
	founding bool
	// override is set while an Owner adds someone by owner override: the
	// arrival is announced with an owner_override notice instead of the
	// ordinary one (docs/PERMISSIONS.md section 6.3).
	override bool
	// loud, when set, is the access alert each arrival in a conversation
	// that is not public is announced with instead of member_joined,
	// naming who caused it even when they let themselves in (alerts.go).
	loud *Alert
	// loudKind is that alert's kind.
	loudKind string
}

// Tx runs fn in one transaction and, if it commits, publishes the events fn
// emitted, removes the blobs it handed over, and runs its After functions.
// Any package that changes something with conversation events uses it.
func (s *Service) Tx(ctx context.Context, fn func(tx store.Tx, rec *Recorder) error) error {
	return s.tx(ctx, nil, fn)
}

// tx is Tx with the mark of a transaction the link runs, which is nil for an
// ordinary call (shared.go).
func (s *Service) tx(ctx context.Context, sh *sharedTx, fn func(tx store.Tx, rec *Recorder) error) error {
	rec := &Recorder{s: s, held: map[model.ConversationID]*turn{}, shared: sh}
	defer func() {
		for conv, t := range rec.held {
			s.leaveLine(conv, t)
		}
	}()
	// With CHURR_DEBUG_TIMING the parts of a change are timed (the timing
	// package): chat.tx is the transaction, chat.line the wait for the
	// writers ahead in the conversation to finish publishing, and
	// chat.publish this change's own fan-out.
	t0 := timing.Now()
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		rec.tx = tx
		if err := fn(tx, rec); err != nil {
			return err
		}
		return s.sharedCommit(ctx, tx, rec)
	})
	if err != nil {
		return err
	}
	timing.Since("chat.tx", t0)
	t0 = timing.Now()
	defer func() { timing.Since("chat.publish", t0) }()
	// The transaction is over and its connection given back; now wait for
	// the writers ahead in each conversation to finish publishing. Lines
	// are in commit order, so two transactions never wait for each other.
	for _, t := range rec.held {
		<-t.first
	}
	if len(rec.held) > 0 {
		timing.Since("chat.line", t0)
		t0 = timing.Now()
	}
	// What is committed is told to everyone, whether or not the request
	// that caused it is still waiting for its answer.
	ctx = context.WithoutCancel(ctx)
	for _, e := range rec.events {
		s.publish(ctx, e)
	}
	for _, key := range rec.blobs {
		if err := s.blobs.Delete(ctx, key); err != nil {
			// The row is gone; the janitor's sweep finds a blob with
			// no row.
			s.log.Warn("a blob could not be removed", "err", err)
		}
	}
	for _, fn := range rec.after {
		fn()
	}
	return nil
}

// Emit gives an event its seq, writes it to the catch-up log in the
// transaction, and queues it for the hub. The caller sets Type,
// ConversationID, Payload and, where they apply, MessageID, Only and AlsoTo.
func (r *Recorder) Emit(ctx context.Context, e model.Event) (model.Event, error) {
	seq, err := r.nextSeq(ctx, e.ConversationID)
	if err != nil {
		return e, err
	}
	if _, held := r.held[e.ConversationID]; !held {
		r.held[e.ConversationID] = r.s.joinLine(e.ConversationID)
	}
	e.Seq = seq
	if e.At.IsZero() {
		e.At = r.s.now()
	}
	if err := r.tx.Events().Append(ctx, e); err != nil {
		return e, err
	}
	r.events = append(r.events, e)
	return e, nil
}

// Seq takes the next seq of a conversation for a message that is about to be
// stored, with the same ordering guarantee as Emit. Pass the result to
// EmitAt.
func (r *Recorder) Seq(ctx context.Context, conv model.ConversationID) (int64, error) {
	seq, err := r.nextSeq(ctx, conv)
	if err != nil {
		return 0, err
	}
	if _, held := r.held[conv]; !held {
		r.held[conv] = r.s.joinLine(conv)
	}
	return seq, nil
}

// SeqAt is Seq for a message created at `at` that this transaction stores
// next with Messages.Insert: the statement that takes the seq also moves the
// conversation's last_message_at there, so storing the message asks nothing
// more of the conversation's row. Everything else is as Seq.
func (r *Recorder) SeqAt(ctx context.Context, conv model.ConversationID, at model.Time) (int64, error) {
	seq, err := r.nextSeqAt(ctx, conv, at)
	if err != nil {
		return 0, err
	}
	if _, held := r.held[conv]; !held {
		r.held[conv] = r.s.joinLine(conv)
	}
	return seq, nil
}

// EmitAt is Emit for an event whose seq was already taken with Seq: the
// event that creates a message has the message's seq.
func (r *Recorder) EmitAt(ctx context.Context, e model.Event) error {
	if e.At.IsZero() {
		e.At = r.s.now()
	}
	if err := r.tx.Events().Append(ctx, e); err != nil {
		return err
	}
	r.events = append(r.events, e)
	return nil
}

// After runs fn once the transaction has committed: for events that belong
// to no conversation, which are sent and not stored.
func (r *Recorder) After(fn func()) { r.after = append(r.after, fn) }

// RemoveBlobs hands over blobs whose rows the transaction deleted.
func (r *Recorder) RemoveBlobs(keys ...string) { r.blobs = append(r.blobs, keys...) }

// Membership turns what a store method did to memberships into events:
// member.joined, member.left and member.level_changed in each conversation
// concerned. by is who caused it ("" when the account acted for itself or a
// group brought it in); reason is the `reason` of member.left.
func (r *Recorder) Membership(ctx context.Context, res store.MembershipResult, by model.AccountID, reason string) error {
	gone := map[model.ConversationID]bool{}
	var announce []joined
	for _, d := range res.Deleted {
		// Nobody is left to tell, and the log went with the conversation.
		gone[d.ID] = true
		r.RemoveBlobs(d.BlobKeys...)
	}
	for _, ch := range res.Changes {
		m := ch.Member
		if gone[m.ConversationID] {
			continue
		}
		e := model.Event{ConversationID: m.ConversationID, Payload: map[string]any{}}
		if by != "" && by != m.AccountID {
			e.Payload["by_id"] = by
		}
		switch ch.Kind {
		case store.MemberJoined:
			e.Type = model.EventMemberJoined
			e.Payload["member"] = m
			// The joiner's other clients show the conversation at once.
			conv, err := r.tx.Conversations().Get(ctx, m.ConversationID)
			if err == nil {
				announce = append(announce, joined{conv, m.AccountID})
				e.Only = map[model.AccountID]map[string]any{m.AccountID: {"conversation": r.s.ShownToArrival(ctx, r.tx, conv, m)}}
			}
		case store.MemberLeft:
			e.Type = model.EventMemberLeft
			e.Payload["account_id"] = m.AccountID
			e.Payload["reason"] = reason
			// The one who left gets this as its last event from here.
			e.AlsoTo = []model.AccountID{m.AccountID}
		case store.MemberLevelChanged:
			e.Type = model.EventMemberLevelChanged
			e.Payload["member"] = m
		default:
			continue
		}
		if _, err := r.Emit(ctx, e); err != nil {
			return err
		}
	}
	return r.announce(ctx, announce, by)
}

// announce posts a member_joined notice for each arrival in a private
// channel, by whatever route it came, and nobody can turn it off (ruling
// R87): the members are told who can now read what they say from here on.
// by, when it is someone else, is who added them. The people a channel is
// created with are not newcomers.
func (r *Recorder) announce(ctx context.Context, announce []joined, by model.AccountID) error {
	for _, j := range announce {
		c := j.conv
		if r.founding || !c.Named() || !c.Private {
			continue
		}
		if c.IsShared() {
			// In a channel shared with other servers the notice is posted
			// by the arrival's own server, as one of its messages, and
			// travels in the stream's order like any other; a join that
			// came over the link was announced by the server it came from.
			if r.shared != nil && r.shared.remote {
				continue
			}
			acc, err := r.tx.Accounts().Get(ctx, j.account)
			if err != nil {
				return err
			}
			if !acc.Local() {
				continue
			}
		}
		system := map[string]any{"kind": "member_joined"}
		if r.override {
			system["kind"] = "owner_override"
		}
		if by != "" && by != j.account {
			system["by_id"] = string(by)
		}
		if r.loud != nil {
			system["kind"] = r.loudKind
			if r.loud.By != "" {
				system["by_id"] = string(r.loud.By)
			}
			if r.loud.Name != "" {
				system["name"] = r.loud.Name
			}
		}
		if _, err := r.s.systemNotice(ctx, r.tx, r, c, j.account, system); err != nil {
			return err
		}
	}
	return nil
}

// Founding marks the transaction as creating a conversation: the people it
// starts with are not announced as newcomers (Membership).
func (r *Recorder) Founding() { r.founding = true }

// joined is one arrival that Membership may announce.
type joined struct {
	conv    model.Conversation
	account model.AccountID
}

// ShownToArrival returns a conversation as someone who has just arrived in
// it is sent it, beside the event that says so (member.joined, or
// channel_group.added for the people a group brought): with their own
// membership, and without its parent if they cannot see the parent. A
// client takes a conversation that comes without a membership for one the
// reader is not in, so every such event carries it.
func (s *Service) ShownToArrival(ctx context.Context, a store.Areas, c model.Conversation, m model.Member) model.Conversation {
	c = s.parentForID(ctx, a, m.AccountID, c)
	c.Membership = membership(m, store.UnreadSets{}, s.now(), false)
	return c
}

// membership builds the caller's own view of its member row.
func membership(m model.Member, u store.UnreadSets, now model.Time, flat bool) *model.Membership {
	mode := m.ModeInForce(now)
	unread, mentions, badge := u.Counts(mode, flat)
	out := &model.Membership{
		Level: m.Level, ViaGroup: m.ViaGroup, Direct: m.DirectLevel != "", JoinedAt: m.JoinedAt, ReadSeq: m.ReadSeq,
		Mode: m.Mode, Show: m.Show, Starred: m.Starred, ThreadView: m.ThreadView, IgnoreBroadMentions: m.IgnoreBroad,
		MayPost:     m.MayPost,
		UnreadCount: unread, MentionCount: mentions, BadgeCount: badge,
	}
	if m.MutedUntil.After(now) {
		// While muted the stored mode is nothing; what returns afterwards
		// is what the person chose.
		out.MutedUntil = m.MutedUntil
		out.Mode = m.ModeBeforeMute
	} else if mode != m.Mode {
		out.Mode = mode
	}
	return out
}
