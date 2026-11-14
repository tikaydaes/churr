package chat

import (
	"context"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is the seam between the conversation service and the link to
// other servers (docs/LINK-PROTOCOL.md). A conversation that is shared is
// ordered by a clock every server shares (ruling R57): each event carries a
// stamp made by the server it comes from, (at, origin, n), and every server
// shows the conversation in the order of the stamps. Nobody hands out
// positions, so a local change in a shared conversation is made exactly as
// any other local change, at once, with the conversation's own next seq. The
// seq is this server's arrival number: it serves the events log, clients'
// catch-up, read positions and unread counts, and no longer says where a
// message is shown. Two things are added:
//
//   - Every event a transaction emits in a shared conversation is stamped as
//     it is emitted (Outbound.Stamp), in the transaction, so stamps are in
//     the order the events were made. A message stored by the transaction
//     takes its order string from its event's stamp (Recorder.Order).
//
//   - At the end of the transaction the link is handed the events and their
//     stamps (Outbound.Commit) and writes what must travel: to the stream's
//     log when this server is the stream's time reference, to the outbox
//     otherwise. A message is "unsent" while its event waits in the outbox:
//     the reference has not acknowledged it. It is shown in its place all
//     the same.
//
// Events that arrive from other servers are applied through RemoteTx and the
// Apply methods of remote.go, with the stamp their origin gave them. They
// take this server's next seq like anything else.

// Stamp is where an event stands in a shared conversation: the time of the
// conversation's reference clock in milliseconds as the server that made the
// event read it, that server's name, and its counter in the stream.
type Stamp struct {
	At     int64
	Origin string
	N      int64
	// Offset is the clock offset the origin used to work out At.
	Offset int64
	// Unsent: the event waits for the stream's time reference to
	// acknowledge it. False when this server is the reference.
	Unsent bool
}

// Order is the stamp as the order string clients sort by.
func (st Stamp) Order() string { return model.OrderString(st.At, st.Origin, st.N) }

// Stamped is an event a transaction produced in a shared conversation, with
// its stamp.
type Stamped struct {
	Event model.Event
	Stamp Stamp
}

// Outbound is what chat needs of the link package. link.Service implements
// it; server connects the two.
type Outbound interface {
	// Stamp gives the next event this server makes in a stream its stamp.
	// The recorder calls it for every event a transaction emits in a
	// shared conversation, in the order they are emitted, with the
	// transaction. shared is false when the stream is not one events
	// travel in (an offer not yet taken up), and the event is then an
	// ordinary local one. model.ErrUnavailable if this server has not been
	// told its clock offset by the stream's time reference; model.
	// ErrForbidden if the conversation is a read-only copy.
	Stamp(ctx context.Context, tx store.Tx, stream model.StreamID) (stamp Stamp, shared bool, err error)
	// Commit is called at the end of a transaction with the events it
	// produced in one shared conversation, in order, each with its stamp.
	// Through tx it writes what must travel. after, if not nil, runs once
	// the transaction has committed.
	Commit(ctx context.Context, tx store.Tx, conv model.Conversation, events []Stamped) (after func(), err error)
	// Unsent returns, of a shared conversation's messages, the ones whose
	// events still wait for the time reference to acknowledge them. It
	// reads through a, which may be a transaction.
	Unsent(ctx context.Context, a store.Areas, conv model.Conversation) map[model.MessageID]bool
	// Typing passes on that a local member is typing in a shared
	// conversation. It is never stored and may be dropped.
	Typing(ctx context.Context, conv model.Conversation, account model.AccountID, parent model.MessageID)
	// Shared describes the stream a conversation is in, for the API.
	Shared(ctx context.Context, conv model.Conversation) *model.Shared
	// ReadOnly reports whether a cross-server ad-hoc channel or sealed
	// chat is read-only for an account (section 16).
	ReadOnly(ctx context.Context, conv model.Conversation, account model.AccountID) bool
	// CanReach checks, before an ad-hoc channel with accounts of other
	// servers is made, that it can run over one link: one on which the
	// starter shares a shared channel with each of them (section 16).
	CanReach(ctx context.Context, by model.AccountID, remote []model.AccountID) error
	// StartAdhoc puts a stream behind an ad-hoc channel that was just made
	// with members from other servers, and offers it to their servers.
	StartAdhoc(ctx context.Context, by model.Account, conv model.Conversation) error
	// Invite is adding an account of another server to a cross-server
	// ad-hoc channel: it checks the rule and, once the transaction has
	// committed, submits member.invited. The account becomes a member
	// when its own server says it has joined.
	Invite(ctx context.Context, tx store.Tx, rec *Recorder, conv model.Conversation, by, target model.Account) error
}

// SetOutbound connects the link package.
func (s *Service) SetOutbound(o Outbound) { s.outbound = o }

// sharedTx is the state of a transaction that the link itself runs.
type sharedTx struct {
	// remote: the transaction applies events that came over the link, or
	// is the link's own bookkeeping. Nothing it emits is stamped or handed
	// back to the link.
	remote bool
}

// stampKey names one event of a transaction: its conversation and seq.
type stampKey struct {
	conv model.ConversationID
	seq  int64
}

// nextSeq gives a recorder a conversation's next number, and stamps the
// event that will carry it if the conversation is shared.
func (r *Recorder) nextSeq(ctx context.Context, conv model.ConversationID) (int64, error) {
	seq, stream, err := r.tx.Streams().TakeSeq(ctx, conv)
	if err != nil {
		return 0, err
	}
	if stream == "" || r.s.outbound == nil || (r.shared != nil && r.shared.remote) {
		return seq, nil
	}
	stamp, shared, err := r.s.outbound.Stamp(ctx, r.tx, stream)
	if err != nil {
		return 0, err
	}
	if shared {
		if r.stamps == nil {
			r.stamps = map[stampKey]Stamp{}
		}
		r.stamps[stampKey{conv, seq}] = stamp
	}
	return seq, nil
}

// Order returns the order string of the event that took a seq with Seq in
// this transaction: what a message stored under that seq is shown by. It is
// "" in a conversation that is not shared, where the store works the order
// out from the seq.
func (r *Recorder) Order(conv model.ConversationID, seq int64) string {
	if st, ok := r.stamps[stampKey{conv, seq}]; ok {
		return st.Order()
	}
	return ""
}

// Unsent reports whether the event that took a seq in this transaction will
// wait for a time reference to acknowledge it.
func (r *Recorder) Unsent(conv model.ConversationID, seq int64) bool {
	return r.stamps[stampKey{conv, seq}].Unsent
}

// Store returns the transaction the recorder belongs to.
func (r *Recorder) Store() store.Tx { return r.tx }

// RemoteTx is Tx for events that came over the link, and for the link's own
// bookkeeping around them. Nothing emitted inside is stamped or handed to
// the link again; each event takes this server's next seq.
func (s *Service) RemoteTx(ctx context.Context, fn func(tx store.Tx, rec *Recorder) error) error {
	return s.tx(ctx, &sharedTx{remote: true}, fn)
}

// sharedCommit runs at the end of every transaction, inside it: the events
// the transaction stamped are handed to the link, conversation by
// conversation, in the order they were emitted.
func (s *Service) sharedCommit(ctx context.Context, tx store.Tx, rec *Recorder) error {
	if s.outbound == nil || len(rec.stamps) == 0 {
		return nil
	}
	var order []model.ConversationID
	by := map[model.ConversationID][]Stamped{}
	for _, e := range rec.events {
		st, ok := rec.stamps[stampKey{e.ConversationID, e.Seq}]
		if !ok {
			continue
		}
		if _, seen := by[e.ConversationID]; !seen {
			order = append(order, e.ConversationID)
		}
		by[e.ConversationID] = append(by[e.ConversationID], Stamped{Event: e, Stamp: st})
	}
	for _, id := range order {
		c, err := tx.Conversations().Get(ctx, id)
		if errors.Is(err, model.ErrNotFound) {
			continue // its last member went, and the conversation with them
		}
		if err != nil {
			return err
		}
		after, err := s.outbound.Commit(ctx, tx, c, by[id])
		if err != nil {
			return err
		}
		if after != nil {
			rec.After(after)
		}
	}
	return nil
}

// local returns the accounts on this server that receive a conversation's
// events.
func (s *Service) localRecipients(ctx context.Context, conv model.ConversationID) []model.AccountID {
	to, err := s.perm.Recipients(ctx, conv)
	if err != nil {
		s.log.Warn("the recipients of a shared conversation could not be read", "err", err)
		return nil
	}
	return to
}

// The events of docs/EVENTS.md that exist only in a shared conversation.
const (
	// EventMessageSent (stored): the stream's time reference has
	// acknowledged a message this server made. It is no longer unsent.
	EventMessageSent = "message.sent"
	// EventMessagePlaced (stored): a message's order string changed,
	// because its stamp was refused and it was stamped again.
	EventMessagePlaced = "message.placed"
)

// MessageSent tells local members that a message of this server's has been
// acknowledged by its stream's time reference. The event is stored, with
// this server's next seq and nothing sent over the link, so a client that
// was away when it happened gets it on catch-up like any other change and
// clears `unsent` on the message it holds. A message that is gone by now
// needs no news. The link calls it outside any transaction.
func (s *Service) MessageSent(ctx context.Context, conv model.ConversationID, id model.MessageID) {
	err := s.RemoteTx(ctx, func(tx store.Tx, rec *Recorder) error {
		m, err := tx.Messages().Get(ctx, id)
		if errors.Is(err, model.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if m.ConversationID != conv || m.Deleted() {
			return nil
		}
		_, err = rec.Emit(ctx, model.Event{Type: EventMessageSent, ConversationID: conv, MessageID: id,
			Payload: map[string]any{"message_id": id}})
		return err
	})
	if err != nil {
		s.log.Warn("message.sent could not be stored", "conversation", conv, "err", err)
	}
}

// PlaceMessage gives a message of this server's a new order string, after
// its stamp was refused and it was stamped again, and tells local members
// with message.placed. The link calls it inside RemoteTx.
func (s *Service) PlaceMessage(ctx context.Context, tx store.Tx, rec *Recorder, id model.MessageID, order string) error {
	m, err := tx.Messages().Get(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		return nil // deleted meanwhile
	}
	if err != nil {
		return err
	}
	if err := tx.Streams().SetOrder(ctx, id, order); err != nil {
		return err
	}
	m.Order = order
	conv, err := tx.Conversations().Get(ctx, m.ConversationID)
	if err != nil {
		return err
	}
	out, err := s.neutral(ctx, tx, conv, m)
	if err != nil {
		return err
	}
	out.Unsent = true
	_, err = rec.Emit(ctx, model.Event{Type: EventMessagePlaced, ConversationID: conv.ID, MessageID: id, Payload: map[string]any{"message": out}})
	return err
}

// withUnsent marks, in a shared conversation, the messages the stream's time
// reference has not acknowledged yet.
func (s *Service) withUnsent(ctx context.Context, a store.Areas, conv model.Conversation, msgs []model.Message) {
	if s.outbound == nil || !conv.IsShared() || len(msgs) == 0 {
		return
	}
	unsent := s.outbound.Unsent(ctx, a, conv)
	if len(unsent) == 0 {
		return
	}
	for i := range msgs {
		if unsent[msgs[i].ID] {
			msgs[i].Unsent = true
		}
	}
}

// withShared fills in what the API says about a shared conversation.
func (s *Service) withShared(ctx context.Context, actor model.Account, c model.Conversation) model.Conversation {
	if s.outbound == nil || !c.IsShared() {
		return c
	}
	c.Shared = s.outbound.Shared(ctx, c)
	if actor.ID != "" && s.outbound.ReadOnly(ctx, c, actor.ID) {
		c.ReadOnly = true
	}
	return c
}

// actorKey is the context key of WithActor.
type actorKey struct{}

// WithActor notes in a context who is making the change a transaction is
// about to make. An event does not always say (nobody is told who answered
// an anonymous poll), and the link needs it to send the change on in that
// account's name.
func WithActor(ctx context.Context, account model.AccountID) context.Context {
	return context.WithValue(ctx, actorKey{}, account)
}

// ActorOf returns the account WithActor noted, or "".
func ActorOf(ctx context.Context) model.AccountID {
	id, _ := ctx.Value(actorKey{}).(model.AccountID)
	return id
}
