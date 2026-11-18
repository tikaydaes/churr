package link

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file moves a channel that two servers share over a direct link onto
// a hub (section 21a of docs/LINK-PROTOCOL.md; rulings R55 and R56, on the
// shared-clock ordering of R57). What is handed over is the channel's
// ownership, its time reference, its relay and log, and its key epoch. The
// stream keeps its identifier and each server keeps its one local channel.
//
// The shape of it. The owner server proposes; the other server's owner
// accepts, with a signed block the hub can check. The owner server has the
// hub prepare the channel: the hub records it and carries nothing in it.
// Then the owner server commits, in ONE local transaction, which
//
//   - appends stream.moved as its own last event in the direct log,
//   - re-points its copy of the stream at the hub, and
//   - records the move as committed, with a signed commit block.
//
// That transaction is the commit point. Before it the move can be abandoned
// and nothing has changed anywhere that anyone acts on; after it the move
// cannot be abandoned. Everybody else follows the owner server's decision:
//
//   - The other server switches in the transaction that applies
//     stream.moved. It applies an origin's events in the order of that
//     origin's counter and with no gaps, so when it reaches stream.moved it
//     holds everything the owner server made before it. The event names
//     how far the owner server took the other server's own events: what
//     the other server still has waiting below that mark was taken, and is
//     not sent again; what is above it was not, and goes to the hub.
//   - The hub activates the channel when it is handed the commit block,
//     which either server may do, so it does not wait on a server that
//     committed and went away.
//
// Nothing is numbered, so nothing has to be frozen: an event's place is its
// stamp, a stamp anyone else has seen never changes, and every server drops
// an event it already holds.

// The direct messages of a move.
const (
	kMovePropose = "move_propose"
	kMoveAccept  = "move_accept"
	kMoveDecline = "move_decline"
	kMoveAbandon = "move_abandon"
)

// evMoved is the owner server's last event in the direct log of a stream it
// has moved onto a hub.
const evMoved = "stream.moved"

// EventMoveChanged tells a channel's local members that a move of it onto a
// hub was proposed, answered, committed, finished or abandoned.
const EventMoveChanged = "stream.move_changed"

const (
	moveLife    = 24 * time.Hour     // a proposal's life unless told otherwise
	moveLifeMax = 7 * 24 * time.Hour // and the longest it may be given
	moveLifeMin = time.Minute
	// maxMoveSkew is how far ahead of the hub's clock, in milliseconds, the
	// owner server's may be at the commit. The stream's times were the
	// owner server's and become the hub's; each server stamps its next
	// event after everything it holds, so for as long as the old clock was
	// ahead, new events stand just after the last old one and so ahead of
	// the hub's clock, which refuses a time more than 30 seconds ahead.
	maxMoveSkew = 15 * 1000
)

var errMoving = model.ErrConflict.With("That channel is being moved onto a hub. Wait until the move has finished or been abandoned.")

// Move is a move as the API shows it (schema StreamMove).
type Move struct {
	ID             string         `json:"id"`
	StreamID       model.StreamID `json:"stream_id"`
	Direction      string         `json:"direction"` // outgoing | incoming
	OtherServer    string         `json:"other_server"`
	HubName        string         `json:"hub_name"`
	HubFingerprint string         `json:"hub_fingerprint"`
	HubPeerID      model.PeerID   `json:"hub_peer_id,omitempty"`
	State          string         `json:"state"`
	Reason         string         `json:"reason,omitempty"`
	CreatedAt      model.Time     `json:"created_at"`
	ExpiresAt      model.Time     `json:"expires_at"`
	UpdatedAt      model.Time     `json:"updated_at"`
}

// outgoing reports whether this server proposed the move: it is, or was,
// the stream's owner server.
func outgoing(mv store.StreamMove) bool { return mv.ProposedBy != "" }

func (s *Service) moveView(ctx context.Context, mv store.StreamMove) Move {
	v := Move{ID: mv.ID, StreamID: mv.StreamID, Direction: "incoming", HubName: mv.HubName, HubFingerprint: mv.HubFingerprint,
		HubPeerID: mv.HubPeerID, State: mv.State, Reason: mv.Reason, CreatedAt: mv.CreatedAt, ExpiresAt: mv.ExpiresAt, UpdatedAt: mv.UpdatedAt}
	if outgoing(mv) {
		v.Direction = "outgoing"
	}
	if other, err := s.otherOf(ctx, mv); err == nil {
		v.OtherServer = other.Name
	}
	return v
}

// Moves lists every move this server knows of, newest first.
func (s *Service) Moves(ctx context.Context) ([]Move, error) {
	list, err := s.store.Streams().Moves(ctx, "")
	if err != nil {
		return nil, err
	}
	out := []Move{}
	for _, mv := range list {
		out = append(out, s.moveView(ctx, mv))
	}
	return out, nil
}

// openMove returns the move of a stream that is still in progress, if any.
func (s *Service) openMove(ctx context.Context, a store.Areas, id model.StreamID) (store.StreamMove, bool) {
	list, err := a.Streams().Moves(ctx, id)
	if err != nil {
		return store.StreamMove{}, false
	}
	for _, mv := range list {
		if mv.Open() {
			return mv, true
		}
	}
	return store.StreamMove{}, false
}

// moving reports whether things that would pull the ground from under a move
// are to be refused on this server: on the server that proposed it from the
// proposal on, on the other from its acceptance on, until the move is done
// or abandoned.
func (s *Service) moving(ctx context.Context, a store.Areas, id model.StreamID) bool {
	mv, ok := s.openMove(ctx, a, id)
	return ok && (outgoing(mv) || mv.State != store.MoveProposed)
}

// movingOn reports whether a move in progress runs over a peer, as the
// direct link it leaves or the hub it goes to.
func (s *Service) movingOn(ctx context.Context, peer model.PeerID) bool {
	list, err := s.store.Streams().Moves(ctx, "")
	if err != nil {
		return false
	}
	for _, mv := range list {
		if mv.Open() && (mv.FromPeerID == peer || mv.HubPeerID == peer) && (outgoing(mv) || mv.State != store.MoveProposed) {
			return true
		}
	}
	return false
}

// tellMove tells the channel's local members what has become of a move.
func (s *Service) tellMove(ctx context.Context, mv store.StreamMove) {
	st, err := s.store.Streams().Get(ctx, mv.StreamID)
	if err != nil || st.ConversationID == "" {
		return
	}
	s.chat.TellLocal(ctx, st.ConversationID, EventMoveChanged, map[string]any{"stream_id": mv.StreamID, "move": s.moveView(ctx, mv)})
}

// errMoveState ends a change to a move that found it in another state than
// the change is for.
var errMoveState = model.ErrConflict.With("That move has gone on without this.")

// setMove changes a move under the lock of its stream's row, which is what
// keeps a change of state and the commit apart: whichever takes the lock
// second sees what the first did.
func (s *Service) setMove(ctx context.Context, id string, fn func(mv *store.StreamMove) error) (store.StreamMove, error) {
	var out store.StreamMove
	err := s.again(ctx, func() error {
		return s.store.Tx(ctx, func(tx store.Tx) error {
			mv, err := tx.Streams().Move(ctx, id)
			if err != nil {
				return err
			}
			if _, err := s.lockStream(ctx, tx, mv.StreamID); err != nil {
				return err
			}
			if mv, err = tx.Streams().Move(ctx, id); err != nil {
				return err
			}
			if err := fn(&mv); err != nil {
				return err
			}
			mv.UpdatedAt = s.now()
			out = mv
			return tx.Streams().PutMove(ctx, mv)
		})
	})
	return out, err
}

// managersOf returns the managers of a channel on this server as the
// managers they become when the channel is shared to or moves onto a hub:
// every active person's account whose level is manager. An Owner or an
// Admin is a manager only as anyone is (docs/PERMISSIONS.md section 2.4).
func (s *Service) managersOf(ctx context.Context, a store.Areas, conv model.ConversationID) ([]wire.ChannelAccount, error) {
	ids, err := a.Members().IDs(ctx, conv)
	if err != nil {
		return nil, err
	}
	managers, err := a.Members().Managers(ctx, conv)
	if err != nil {
		return nil, err
	}
	level := map[model.AccountID]bool{}
	for _, id := range managers {
		level[id] = true
	}
	accounts, err := a.Accounts().GetMany(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := []wire.ChannelAccount{}
	for _, acc := range accounts {
		if !acc.Local() || !acc.Active() || acc.Role == model.RoleBot {
			continue
		}
		if level[acc.ID] {
			out = append(out, wire.ChannelAccount{AccountID: string(acc.ID), Server: s.id.Name, Username: acc.Username})
		}
	}
	return out, nil
}

// otherOf is the other server of the direct link a move leaves.
func (s *Service) otherOf(ctx context.Context, mv store.StreamMove) (store.Server, error) {
	peer, err := s.store.Peers().Get(ctx, mv.FromPeerID)
	if err != nil {
		return store.Server{}, err
	}
	return s.store.Peers().Server(ctx, peer.ServerID)
}

// hubPeerFor finds this server's own way to the hub a move names, by name
// and key fingerprint: its link to that hub, or its link to another hub of
// that hub's cluster, whose roster lists the named hub under that key
// (section 23b). The other server must be reachable over it too.
func (s *Service) hubPeerFor(ctx context.Context, name, fingerprint string, other model.ServerID) (store.Peer, bool) {
	peers, err := s.store.Peers().List(ctx)
	if err != nil {
		return store.Peer{}, false
	}
	for _, p := range peers {
		if p.Kind != wire.KindHub || p.State != store.PeerActive {
			continue
		}
		if key := s.clusterHubKey(p, name); len(key) > 0 && wire.KeyFingerprint(key) == fingerprint && s.reaches(ctx, p, other) {
			return p, true
		}
	}
	return store.Peer{}, false
}

// moveBody is what the direct messages of a move carry.
type moveBody struct {
	MoveID         string         `json:"move_id"`
	StreamID       model.StreamID `json:"stream_id"`
	Hub            string         `json:"hub"`
	HubFingerprint string         `json:"hub_fingerprint"`
	ExpiresAt      model.Time     `json:"expires_at"`
	Reason         string         `json:"reason"`
	Accept         wire.Bytes     `json:"accept"`
	AcceptSig      wire.Bytes     `json:"accept_sig"`
}

// ProposeMove proposes moving a channel this server owns and shares over a
// direct link onto a hub both servers are on. Nothing changes until the
// other server's owner accepts.
func (s *Service) ProposeMove(ctx context.Context, by model.Account, id model.StreamID, hubPeer model.PeerID, expiresIn time.Duration) (Move, error) {
	st, err := s.ownedChannel(ctx, id)
	if err != nil {
		return Move{}, err
	}
	from, err := s.store.Peers().Get(ctx, st.PeerID)
	if err != nil {
		return Move{}, err
	}
	if from.Kind != wire.KindServer || st.Sequencer != store.SequencerSelf {
		return Move{}, model.ErrConflict.With("Only a channel shared over a direct link can be moved onto a hub.")
	}
	hub, err := s.store.Peers().Get(ctx, hubPeer)
	if err != nil || hub.Kind != wire.KindHub || hub.State != store.PeerActive {
		return Move{}, model.Invalid("peer_id", "That is not an active link to a hub.")
	}
	other, err := s.store.Peers().Server(ctx, from.ServerID)
	if err != nil {
		return Move{}, err
	}
	if !s.reaches(ctx, hub, other.ID) {
		return Move{}, model.ErrConflict.With(other.Name + " is not on that hub. Both servers must be on it before the channel can move.")
	}
	if s.connOf(hub.ID) == nil {
		return Move{}, model.ErrUnavailable.With("The hub is not connected.")
	}
	if conv, err := s.store.Conversations().Get(ctx, st.ConversationID); err == nil && conv.Private {
		if err := s.noGroupsIn(ctx, conv.ID); err != nil {
			return Move{}, err
		}
	}
	if expiresIn == 0 {
		expiresIn = moveLife
	}
	if expiresIn < moveLifeMin || expiresIn > moveLifeMax {
		return Move{}, model.Invalid("expires_in_seconds", "A proposal stands for a minute to seven days.")
	}
	now := s.now()
	mv := store.StreamMove{ID: newID(), StreamID: id, FromPeerID: from.ID, HubPeerID: hub.ID, HubName: hub.HubName,
		HubFingerprint: wire.KeyFingerprint(hub.HubSignKey), State: store.MoveProposed, ProposedBy: by.ID, CreatedAt: now,
		ExpiresAt: now.Add(expiresIn), UpdatedAt: now}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if _, err := s.lockStream(ctx, tx, id); err != nil {
			return err
		}
		if _, busy := s.openMove(ctx, tx, id); busy {
			return errMoving
		}
		return tx.Streams().PutMove(ctx, mv)
	})
	if err != nil {
		return Move{}, err
	}
	s.audit(ctx, s.store, by.ID, "link.move_proposed", "stream", string(id), map[string]any{"move_id": mv.ID, "hub": mv.HubName, "to": other.Name})
	s.sendPropose(ctx, mv)
	s.tellMove(ctx, mv)
	return s.moveView(ctx, mv), nil
}

func (s *Service) sendPropose(ctx context.Context, mv store.StreamMove) {
	if other, err := s.otherOf(ctx, mv); err == nil {
		s.sendDirect(ctx, mv.FromPeerID, other, kMovePropose, "", map[string]any{"move_id": mv.ID, "stream_id": mv.StreamID, "hub": mv.HubName,
			"hub_fingerprint": mv.HubFingerprint, "expires_at": mv.ExpiresAt})
	}
}

func (s *Service) sendAccept(ctx context.Context, mv store.StreamMove) {
	if other, err := s.otherOf(ctx, mv); err == nil {
		s.sendDirect(ctx, mv.FromPeerID, other, kMoveAccept, "", map[string]any{"move_id": mv.ID, "stream_id": mv.StreamID,
			"accept": wire.Bytes(mv.Accept), "accept_sig": wire.Bytes(mv.AcceptSig)})
	}
}

func (s *Service) sendAbandon(ctx context.Context, mv store.StreamMove) {
	if other, err := s.otherOf(ctx, mv); err == nil {
		s.sendDirect(ctx, mv.FromPeerID, other, kMoveAbandon, "", map[string]any{"move_id": mv.ID, "stream_id": mv.StreamID, "reason": mv.Reason})
	}
}

// onMovePropose records a proposal for this server's owner to answer. It
// comes from the stream's owner server, over the stream's own direct link.
func (s *Service) onMovePropose(ctx context.Context, m directMessage) {
	var b moveBody
	if json.Unmarshal(m.raw, &b) != nil || !wire.ValidID(b.MoveID) {
		return
	}
	st, err := s.store.Streams().Get(ctx, b.StreamID)
	if err != nil || st.PeerID != m.peer || st.OwnerServerID != m.from.ID || st.Kind != store.StreamChannel || st.State != store.StreamActive ||
		st.Sequencer != store.SequencerOwner {
		s.count(func(stats *Stats) { stats.Dropped["move_not_from_owner"]++ })
		return
	}
	if _, err := s.store.Streams().Move(ctx, b.MoveID); err == nil {
		return // known already; the owner sent it again
	}
	now := s.now()
	if b.ExpiresAt.Before(now) {
		return
	}
	if b.ExpiresAt.After(now.Add(moveLifeMax)) {
		b.ExpiresAt = now.Add(moveLifeMax)
	}
	mv := store.StreamMove{ID: b.MoveID, StreamID: st.ID, FromPeerID: m.peer, HubName: b.Hub, HubFingerprint: b.HubFingerprint,
		State: store.MoveProposed, CreatedAt: now, ExpiresAt: b.ExpiresAt, UpdatedAt: now}
	if hub, ok := s.hubPeerFor(ctx, b.Hub, b.HubFingerprint, m.from.ID); ok {
		mv.HubPeerID = hub.ID
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if _, err := s.lockStream(ctx, tx, st.ID); err != nil {
			return err
		}
		if _, busy := s.openMove(ctx, tx, st.ID); busy {
			return errMoving
		}
		return tx.Streams().PutMove(ctx, mv)
	})
	if err != nil {
		return
	}
	s.audit(ctx, s.store, "", "link.move_proposed_by", "stream", string(st.ID), map[string]any{"move_id": mv.ID, "hub": mv.HubName, "by": m.from.Name})
	s.tellMove(ctx, mv)
}

// incomingMove loads a proposal this server's owner may answer.
func (s *Service) incomingMove(ctx context.Context, id string) (store.StreamMove, error) {
	mv, err := s.store.Streams().Move(ctx, id)
	if err != nil {
		return mv, err
	}
	if outgoing(mv) {
		return mv, model.ErrForbidden.With("This server proposed that move; the other server's owner answers it.")
	}
	if mv.State != store.MoveProposed {
		return mv, model.ErrConflict.With("That proposal has already been answered.")
	}
	if s.now().After(mv.ExpiresAt) {
		return mv, model.ErrConflict.With("That proposal has lapsed.")
	}
	return mv, nil
}

// AcceptMove accepts a proposal to move a channel onto a hub. This server
// signs its agreement, naming the hub by name and key and the managers of
// its copy of the channel, who become moderators at the commit. From here
// until the owner server's word this server changes nothing that the move
// depends on.
func (s *Service) AcceptMove(ctx context.Context, by model.Account, id string) (Move, error) {
	mv, err := s.incomingMove(ctx, id)
	if err != nil {
		return Move{}, err
	}
	st, err := s.store.Streams().Get(ctx, mv.StreamID)
	if err != nil || st.State != store.StreamActive || st.Owned() || st.PeerID != mv.FromPeerID || st.ConversationID == "" {
		return Move{}, model.ErrConflict.With("This server is no longer in that channel over that link.")
	}
	owner, err := s.otherOf(ctx, mv)
	if err != nil {
		return Move{}, err
	}
	hub, ok := s.hubPeerFor(ctx, mv.HubName, mv.HubFingerprint, owner.ID)
	if !ok {
		return Move{}, model.ErrConflict.With("This server and " + owner.Name + " are not both on the hub " + mv.HubName + " (" + mv.HubFingerprint + "). Join it first.")
	}
	if _, told := s.toldBy(ctx, hub.ID); !told {
		return Move{}, model.ErrUnavailable.With("The hub has not yet told this server its clock offset. Try again in a moment.")
	}
	if conv, err := s.store.Conversations().Get(ctx, st.ConversationID); err == nil && conv.Private {
		if err := s.noGroupsIn(ctx, conv.ID); err != nil {
			return Move{}, err
		}
	}
	// The managers are read, the agreement signed and the move marked as
	// accepted in one transaction that holds the conversation's row, which
	// every change of a member's level takes too: so the managers signed
	// for are the managers at the moment changes to them begin to be
	// refused (managersHeld), and stay so until the move is over.
	err = s.again(ctx, func() error {
		return s.store.Tx(ctx, func(tx store.Tx) error {
			if _, err := s.lockStream(ctx, tx, mv.StreamID); err != nil {
				return err
			}
			x, err := tx.Streams().Move(ctx, id)
			if err != nil {
				return err
			}
			if x.State != store.MoveProposed {
				return errMoveState
			}
			managers, err := s.managersOf(ctx, tx, st.ConversationID)
			if err != nil {
				return err
			}
			// The hub named, under its key: this server's own hub, or the
			// hub of its cluster the owner server is on.
			hubKey := s.clusterHubKey(hub, x.HubName)
			if len(hubKey) == 0 || wire.KeyFingerprint(hubKey) != x.HubFingerprint {
				return model.ErrConflict.With("This server no longer reaches the hub " + x.HubName + " under the key the move names.")
			}
			signed, sig, err := wire.SignBytes(wire.BMoveAccept, wire.MoveAccept{MoveID: x.ID, StreamID: string(x.StreamID), Hub: x.HubName,
				HubKey: hubKey, Owner: owner.Name, Server: s.id.Name, Managers: managers, ExpiresAt: x.ExpiresAt, At: s.now()}, s.keys)
			if err != nil {
				return err
			}
			x.State, x.AnsweredBy, x.HubPeerID, x.Accept, x.AcceptSig, x.UpdatedAt = store.MoveAccepted, by.ID, hub.ID, signed, sig, s.now()
			mv = x
			return tx.Streams().PutMove(ctx, x)
		})
	})
	if err != nil {
		return Move{}, err
	}
	s.audit(ctx, s.store, by.ID, "link.move_accepted", "stream", string(mv.StreamID), map[string]any{"move_id": mv.ID, "hub": mv.HubName})
	// If the owner server is not connected this goes again on reconnection.
	s.sendAccept(ctx, mv)
	s.tellMove(ctx, mv)
	return s.moveView(ctx, mv), nil
}

// DeclineMove declines a proposal. Nothing changes.
func (s *Service) DeclineMove(ctx context.Context, by model.Account, id string) (Move, error) {
	mv, err := s.incomingMove(ctx, id)
	if err != nil {
		return Move{}, err
	}
	mv, err = s.setMove(ctx, id, func(x *store.StreamMove) error {
		if x.State != store.MoveProposed {
			return errMoveState
		}
		x.State, x.AnsweredBy, x.Reason = store.MoveDeclined, by.ID, "declined"
		return nil
	})
	if err != nil {
		return Move{}, err
	}
	s.audit(ctx, s.store, by.ID, "link.move_declined", "stream", string(mv.StreamID), map[string]any{"move_id": mv.ID})
	if other, err := s.otherOf(ctx, mv); err == nil {
		s.sendDirect(ctx, mv.FromPeerID, other, kMoveDecline, "", map[string]any{"move_id": mv.ID, "stream_id": mv.StreamID})
	}
	s.tellMove(ctx, mv)
	return s.moveView(ctx, mv), nil
}

// CancelMove withdraws a move this server proposed. It is possible until
// the move has been committed.
func (s *Service) CancelMove(ctx context.Context, by model.Account, id string) (Move, error) {
	mv, err := s.store.Streams().Move(ctx, id)
	if err != nil {
		return Move{}, err
	}
	if !outgoing(mv) {
		return Move{}, model.ErrForbidden.With("Only the server that proposed a move can withdraw it.")
	}
	mv, err = s.abandonMove(ctx, mv, "cancelled")
	if err != nil {
		return Move{}, model.ErrConflict.With("That move can no longer be withdrawn: it has been committed, or is already over.")
	}
	s.audit(ctx, s.store, by.ID, "link.move_cancelled", "stream", string(mv.StreamID), map[string]any{"move_id": mv.ID})
	return s.moveView(ctx, mv), nil
}

// onMoveAccept is the owner server's side of an acceptance: the signed
// agreement is checked and kept, and the move runs.
func (s *Service) onMoveAccept(ctx context.Context, m directMessage) {
	var b moveBody
	if json.Unmarshal(m.raw, &b) != nil {
		return
	}
	mv, err := s.store.Streams().Move(ctx, b.MoveID)
	if err != nil || !outgoing(mv) || mv.FromPeerID != m.peer || mv.StreamID != b.StreamID {
		return
	}
	switch mv.State {
	case store.MoveAbandoned, store.MoveDeclined:
		// The other server still thinks the move is on: it is told again.
		s.sendAbandon(ctx, mv)
		return
	case store.MoveCommitted, store.MoveDone:
		s.deliverMoved(ctx, mv) // it learns the outcome from the log
		return
	case store.MoveAccepted:
		s.runMoveSoon(mv.ID)
		return
	}
	if s.now().After(mv.ExpiresAt) {
		s.abandonMove(ctx, mv, "expired")
		return
	}
	var acc wire.MoveAccept
	if err := wire.VerifyBytes(wire.BMoveAccept, b.Accept, b.AcceptSig, m.from.SignKey, &acc); err != nil {
		s.count(func(stats *Stats) { stats.Dropped["move_accept_bad_signature"]++ })
		return
	}
	ok := acc.MoveID == mv.ID && acc.StreamID == string(mv.StreamID) && acc.Hub == mv.HubName && wire.KeyFingerprint(acc.HubKey) == mv.HubFingerprint &&
		acc.Owner == s.id.Name && acc.Server == m.from.Name
	for _, mod := range acc.Managers {
		ok = ok && mod.Server == m.from.Name && wire.ValidID(mod.AccountID)
	}
	if !ok {
		s.count(func(stats *Stats) { stats.Dropped["move_accept_mismatch"]++ })
		return
	}
	mv, err = s.setMove(ctx, mv.ID, func(x *store.StreamMove) error {
		if x.State != store.MoveProposed {
			return errMoveState
		}
		x.State, x.Accept, x.AcceptSig = store.MoveAccepted, b.Accept, b.AcceptSig
		return nil
	})
	if err != nil {
		return
	}
	s.audit(ctx, s.store, "", "link.move_accepted_by", "stream", string(mv.StreamID), map[string]any{"move_id": mv.ID, "by": m.from.Name})
	s.tellMove(ctx, mv)
	s.runMoveSoon(mv.ID)
}

// onMoveDecline is the owner server's side of a refusal.
func (s *Service) onMoveDecline(ctx context.Context, m directMessage) {
	var b moveBody
	if json.Unmarshal(m.raw, &b) != nil {
		return
	}
	mv, err := s.store.Streams().Move(ctx, b.MoveID)
	if err != nil || !outgoing(mv) || mv.FromPeerID != m.peer {
		return
	}
	mv, err = s.setMove(ctx, mv.ID, func(x *store.StreamMove) error {
		if x.State != store.MoveProposed {
			return errMoveState
		}
		x.State, x.Reason = store.MoveDeclined, "declined"
		return nil
	})
	if err != nil {
		return
	}
	s.audit(ctx, s.store, "", "link.move_declined_by", "stream", string(mv.StreamID), map[string]any{"move_id": mv.ID, "by": m.from.Name})
	s.tellMove(ctx, mv)
}

// onMoveAbandon is the other server's side of an abandonment: the owner
// server's word that the move is off and the stream goes on as it was.
func (s *Service) onMoveAbandon(ctx context.Context, m directMessage) {
	var b moveBody
	if json.Unmarshal(m.raw, &b) != nil {
		return
	}
	mv, err := s.store.Streams().Move(ctx, b.MoveID)
	if err != nil || outgoing(mv) || mv.FromPeerID != m.peer {
		return
	}
	reason := b.Reason
	if reason == "" {
		reason = "abandoned"
	}
	mv, err = s.setMove(ctx, mv.ID, func(x *store.StreamMove) error {
		if x.State != store.MoveProposed && x.State != store.MoveAccepted {
			return errMoveState
		}
		x.State, x.Reason = store.MoveAbandoned, reason
		return nil
	})
	if err != nil {
		return
	}
	s.audit(ctx, s.store, "", "link.move_abandoned", "stream", string(mv.StreamID), map[string]any{"move_id": mv.ID, "reason": reason})
	s.tellMove(ctx, mv)
}

// step is asked before each step of a move. A test may stop the move
// there, which is what a server that fails at that step looks like.
func (s *Service) step(name string) bool {
	if hook := s.testStep.Load(); hook != nil {
		return (*hook)(name)
	}
	return false
}

func (s *Service) runMoveSoon(id string) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runMove(s.ctx, id)
	}()
}

// runMove takes a move this server proposed as far as it will go now. Every
// step can be taken again: after a failure, a disconnection or a restart
// the move goes on from what is written down.
func (s *Service) runMove(ctx context.Context, id string) {
	mv, err := s.store.Streams().Move(ctx, id)
	if err != nil || !outgoing(mv) {
		return
	}
	rt := s.rt(mv.StreamID)
	if !rt.moving.TryLock() {
		// One is running: it runs again before it lets go, and so takes in
		// whatever this run was asked for (an answer that has just come).
		rt.moveAgain.Store(true)
		if !rt.moving.TryLock() {
			return
		}
	}
	defer func() {
		rt.moving.Unlock()
		if rt.moveAgain.Swap(false) && ctx.Err() == nil {
			s.runMove(ctx, id)
		}
	}()
	rt.moveAgain.Store(false)
	if mv, err = s.store.Streams().Move(ctx, id); err != nil {
		return
	}
	if mv.State == store.MoveAccepted {
		if s.now().After(mv.ExpiresAt) {
			s.abandonMove(ctx, mv, "expired")
			return
		}
		if s.step("prepare") {
			return
		}
		rec, err := s.prepareMove(ctx, mv)
		if err != nil {
			if errors.Is(err, model.ErrUnavailable) || ctx.Err() != nil {
				return // the hub cannot be reached now; it is tried again until the proposal expires
			}
			s.abandonMove(ctx, mv, "the hub refused: "+messageOf(err))
			return
		}
		if s.step("commit") {
			return
		}
		why, err := s.commitMove(ctx, mv, rec)
		if why != "" {
			s.abandonMove(ctx, mv, why)
			return
		}
		if err != nil {
			s.log.Warn("link: a move could not be committed yet", "stream", mv.StreamID, "err", err)
			return
		}
		if mv, err = s.store.Streams().Move(ctx, id); err != nil {
			return
		}
		s.audit(ctx, s.store, "", "link.move_committed", "stream", string(mv.StreamID), map[string]any{"move_id": mv.ID, "hub": mv.HubName})
		s.tellMove(ctx, mv)
	}
	if mv.State == store.MoveCommitted {
		s.finishMove(ctx, mv)
	}
}

// prepareMove has the hub prepare the channel and returns the record the
// hub signed. The hub checks the other server's signed agreement itself.
func (s *Service) prepareMove(ctx context.Context, mv store.StreamMove) (wire.ChannelRecord, error) {
	if rec, err := s.channelOf(ctx, s.store, mv.StreamID); err == nil && rec.Prepared && !rec.Closed {
		return rec, nil // prepared already, before a failure or a restart
	}
	st, err := s.store.Streams().Get(ctx, mv.StreamID)
	if err != nil {
		return wire.ChannelRecord{}, err
	}
	conv, err := s.store.Conversations().Get(ctx, st.ConversationID)
	if err != nil {
		return wire.ChannelRecord{}, err
	}
	other, err := s.otherOf(ctx, mv)
	if err != nil {
		return wire.ChannelRecord{}, err
	}
	var acc wire.MoveAccept
	if err := wire.VerifyBytes(wire.BMoveAccept, mv.Accept, mv.AcceptSig, other.SignKey, &acc); err != nil {
		return wire.ChannelRecord{}, err
	}
	mine, err := s.managersOf(ctx, s.store, st.ConversationID)
	if err != nil {
		return wire.ChannelRecord{}, err
	}
	creator := wire.ChannelAccount{AccountID: string(mv.ProposedBy), Server: s.id.Name}
	if by, err := s.store.Accounts().Get(ctx, mv.ProposedBy); err == nil {
		creator.Username = by.Username
	}
	cc := wire.ChannelCreate{StreamID: string(mv.StreamID), Name: conv.Name, Description: conv.Topic, Listing: wire.ListingUnlisted, Creator: creator,
		MoveID: mv.ID, Servers: []string{s.id.Name, other.Name}, Managers: append(mine, acc.Managers...), Epoch: st.KeyEpoch + 1,
		Accept: mv.Accept, AcceptSig: mv.AcceptSig, At: s.now(), Private: conv.Private, HistoryDays: min(max(st.HistoryDays, 0), 90)}
	f, err := wire.SignBlock(wire.FChannelCreate, cc, s.keys)
	if err != nil {
		return wire.ChannelRecord{}, err
	}
	if err := s.sendAndAwaitRecord(ctx, mv.HubPeerID, mv.StreamID, f, 0); err != nil {
		return wire.ChannelRecord{}, err
	}
	rec, err := s.channelOf(ctx, s.store, mv.StreamID)
	if err == nil && (!rec.Prepared || rec.Closed) {
		err = model.ErrUnavailable.With("The hub's record of the channel is not the one this move prepared.")
	}
	return rec, err
}

// commitMove is the commit point: one transaction on the owner server. It
// returns a reason when the move must be abandoned instead, and an error
// when the commit could not be made now and may be tried again.
func (s *Service) commitMove(ctx context.Context, mv store.StreamMove, rec wire.ChannelRecord) (abandon string, err error) {
	told, known := s.toldBy(ctx, mv.HubPeerID)
	if !known {
		return "", model.ErrUnavailable.With("The hub has not told this server its clock offset.")
	}
	if told.Offset > maxMoveSkew {
		return "this server's clock is too far ahead of the hub's", nil
	}
	hub, err := s.store.Peers().Get(ctx, mv.HubPeerID)
	if err != nil || hub.State != store.PeerActive {
		return "this server is no longer on the hub", nil
	}
	other, err := s.otherOf(ctx, mv)
	if err != nil {
		return "", err
	}
	if !rec.Has(s.id.Name) || !rec.Has(other.Name) || len(rec.Servers) != 2 || rec.Hub != mv.HubName {
		return "the hub's record does not hold the channel's two servers", nil
	}
	recRow, err := s.store.Streams().Channel(ctx, mv.StreamID)
	if err != nil {
		return "", err
	}
	var stale bool
	var marker wire.Envelope
	err = s.again(ctx, func() error {
		stale, abandon = false, ""
		return s.chat.RemoteTx(ctx, func(tx store.Tx, r *chat.Recorder) error {
			cur, err := s.lockStream(ctx, tx, mv.StreamID)
			if err != nil {
				return err
			}
			now, err := tx.Streams().Move(ctx, mv.ID)
			if err != nil {
				return err
			}
			switch {
			case now.State != store.MoveAccepted:
				stale = true // withdrawn, or committed by another run
				return nil
			case s.now().After(now.ExpiresAt):
				abandon = "expired"
				return nil
			case cur.State != store.StreamActive || !cur.Owned() || cur.Sequencer != store.SequencerSelf || cur.PeerID != now.FromPeerID:
				abandon = "the stream is no longer this server's over that link"
				return nil
			}
			// The owner server's last event of the stream's direct life.
			// Its stamp is after everything this server has made or taken.
			stamp, err := s.stamp(ctx, tx, cur)
			if err != nil {
				return err
			}
			servers, err := tx.Streams().Servers(ctx, cur.ID)
			if err != nil {
				return err
			}
			have, haveAt := map[string]int64{s.id.Name: stamp.N}, map[string]int64{s.id.Name: stamp.At}
			for _, ss := range servers {
				if ss.Name == other.Name {
					// How far this server took the other server's events:
					// the last of them in the log, which this transaction
					// holds closed.
					have[ss.Name], haveAt[ss.Name] = ss.LoggedN, ss.LastAt
				}
			}
			mods, err := s.managersOf(ctx, tx, cur.ConversationID)
			if err != nil {
				return err
			}
			commit, commitSig, err := wire.SignBytes(wire.FChannelCommit, wire.MoveCommit{MoveID: now.ID, StreamID: string(cur.ID), Hub: now.HubName,
				Owner: s.id.Name, Have: have, HaveAt: haveAt, Epoch: cur.KeyEpoch, Managers: mods, At: s.now()}, s.keys)
			if err != nil {
				return err
			}
			eventID := newID()
			plain, err := s.plainOf(evMoved, map[string]any{"move_id": now.ID, "hub": now.HubName, "record": wire.Bytes(recRow.Record),
				"record_sig": wire.Bytes(recRow.Sig), "commit": commit, "commit_sig": commitSig})
			if err != nil {
				return err
			}
			env, err := s.sealPlain(ctx, tx, cur, eventID, plain, stamp, 0)
			if err != nil {
				return err
			}
			if _, err := tx.Streams().MarkSeen(ctx, cur.ID, eventID, 0, s.now()); err != nil {
				return err
			}
			if env, err = s.appendLog(ctx, tx, cur.ID, env); err != nil {
				return err
			}
			marker = env
			// From here the hub is the stream's owner, relay and time
			// reference for this server.
			hubRow, err := s.hubServer(ctx, tx, hub)
			if err != nil {
				return err
			}
			if _, err := tx.Streams().Update(ctx, cur.ID, func(x *store.Stream) error {
				x.PeerID, x.OwnerServerID, x.Sequencer, x.HomeHub, x.LastSeq = hub.ID, hubRow.ID, store.SequencerHub, now.HubName, 0
				x.ServersVersion = rec.Version
				return nil
			}); err != nil {
				return err
			}
			now.State, now.CommitSeq, now.CommitEventID, now.Commit, now.CommitSig = store.MoveCommitted, env.Cursor, eventID, commit, commitSig
			now.UpdatedAt = s.now()
			if err := tx.Streams().PutMove(ctx, now); err != nil {
				return err
			}
			return s.chat.ApplyConversation(ctx, tx, r, cur.ConversationID, nil, nil)
		})
	})
	if err != nil || abandon != "" || stale {
		return abandon, err
	}
	// What the stream's runtime remembered of the direct link is void.
	rt := s.rt(mv.StreamID)
	rt.mu.Lock()
	rt.inflight, rt.catching, rt.stalled = 0, false, false
	rt.mu.Unlock()
	_ = marker
	return "", nil
}

// finishMove is what follows the commit on the owner server, as often as it
// takes: the other server is given stream.moved, and the hub the commit.
// The move is done when the hub has activated the channel and the other
// server has acknowledged stream.moved.
func (s *Service) finishMove(ctx context.Context, mv store.StreamMove) {
	if !s.step("deliver") {
		s.deliverMoved(ctx, mv)
	}
	if s.step("activate") {
		return
	}
	if rec, err := s.channelOf(ctx, s.store, mv.StreamID); err == nil && rec.Prepared {
		s.sendCommit(ctx, mv)
	}
}

// deliverMoved sends the other server stream.moved from the direct log, over
// the direct link. If it has missed anything before it, it asks, and is
// served from the log this server still keeps (onCatchUp).
func (s *Service) deliverMoved(ctx context.Context, mv store.StreamMove) {
	pc := s.connOf(mv.FromPeerID)
	if pc == nil || mv.CommitSeq == 0 {
		return
	}
	rows, err := s.store.Streams().Log(ctx, mv.StreamID, mv.CommitSeq-1, 1)
	if err != nil || len(rows) != 1 || rows[0].Seq != mv.CommitSeq {
		return
	}
	s.deliverRow(pc, rows[0], -1)
}

// sendCommit hands the hub the owner server's signed commit, which
// activates the channel there. Either of the two servers may.
func (s *Service) sendCommit(ctx context.Context, mv store.StreamMove) {
	pc := s.connOf(mv.HubPeerID)
	if pc == nil || len(mv.Commit) == 0 || s.step("activate") {
		return
	}
	f := wire.Frame{Frame: wire.FChannelCommit, ID: pc.conn.NextID(), Signed: mv.Commit, Sig: mv.CommitSig}
	// With it goes the prepared record the hub itself signed. A hub that
	// has lost what it prepared (its database gone, or put back from before
	// the move) prepares the channel again from that and this commit; a hub
	// that holds the channel does not look at it. Without it the two
	// servers would hold a committed move with nowhere to send it.
	if row, err := s.store.Streams().Channel(ctx, mv.StreamID); err == nil && len(row.Record) > 0 {
		f.Inner, _ = wire.EncodeFrame(wire.Frame{Frame: wire.FChannel, Signed: row.Record, Sig: row.Sig})
	}
	pc.conn.Send(f)
}

// movedFrom returns the move that took a stream off a direct link, on the
// server that committed it: the log of the stream's direct life is still
// served to the other server over that link.
func (s *Service) movedFrom(ctx context.Context, id model.StreamID, peer model.PeerID) (store.StreamMove, bool) {
	list, err := s.store.Streams().Moves(ctx, id)
	if err != nil {
		return store.StreamMove{}, false
	}
	for _, mv := range list {
		if mv.FromPeerID == peer && mv.CommitSeq != 0 && (mv.State == store.MoveCommitted || mv.State == store.MoveDone) {
			return mv, true
		}
	}
	return store.StreamMove{}, false
}

// movedAcked is the other server's acknowledgement of the direct log, after
// the commit: once it covers stream.moved and the hub has activated the
// channel, the move is done.
func (s *Service) movedAcked(ctx context.Context, mv store.StreamMove, cursor int64) {
	if mv.State != store.MoveCommitted || cursor < mv.CommitSeq {
		return
	}
	if rec, err := s.channelOf(ctx, s.store, mv.StreamID); err != nil || rec.Prepared {
		return
	}
	done, err := s.setMove(ctx, mv.ID, func(x *store.StreamMove) error {
		if x.State != store.MoveCommitted {
			return errMoveState
		}
		x.State = store.MoveDone
		return nil
	})
	if err == nil {
		s.audit(ctx, s.store, "", "link.move_done", "stream", string(done.StreamID), map[string]any{"move_id": done.ID})
		s.tellMove(ctx, done)
	}
}

// abandonMove ends a move this server proposed, before its commit, and
// tells the other server and the hub. It fails if the move has been
// committed: from then on there is nothing to abandon.
func (s *Service) abandonMove(ctx context.Context, mv store.StreamMove, reason string) (store.StreamMove, error) {
	mv, err := s.setMove(ctx, mv.ID, func(x *store.StreamMove) error {
		if x.State != store.MoveProposed && x.State != store.MoveAccepted {
			return errMoveState
		}
		x.State, x.Reason = store.MoveAbandoned, reason
		return nil
	})
	if err != nil {
		return mv, err
	}
	s.audit(ctx, s.store, "", "link.move_abandoned", "stream", string(mv.StreamID), map[string]any{"move_id": mv.ID, "reason": reason})
	s.sendAbandon(ctx, mv)
	s.closePrepared(ctx, mv)
	s.tellMove(ctx, mv)
	return mv, nil
}

// closePrepared takes back from the hub a channel it prepared for a move
// that was abandoned. It is asked again until the hub says it is closed.
func (s *Service) closePrepared(ctx context.Context, mv store.StreamMove) {
	rec, err := s.channelOf(ctx, s.store, mv.StreamID)
	if err != nil {
		return
	}
	st, err := s.store.Streams().Get(ctx, mv.StreamID)
	if err != nil || st.PeerID == mv.HubPeerID || !rec.Prepared || rec.Closed {
		return
	}
	a := wire.ChannelAction{StreamID: string(mv.StreamID), ActionID: newID(), Action: wire.ActClose,
		By: wire.ChannelAccount{AccountID: string(mv.ProposedBy), Server: s.id.Name}, At: s.now()}
	f, err := wire.SignBlock(wire.FChannelAction, a, s.keys)
	if err != nil {
		return
	}
	if pc := s.connOf(mv.HubPeerID); pc != nil {
		f.ID = pc.conn.NextID()
		pc.conn.Send(f)
	}
}

// onMovedRecord is a hub's record of a channel this server still holds on a
// direct link: the hub's side of a move in progress.
func (s *Service) onMovedRecord(ctx context.Context, pc *peerConn, st store.Stream, f wire.Frame, rec wire.ChannelRecord) error {
	list, err := s.store.Streams().Moves(ctx, st.ID)
	if err != nil || len(list) == 0 || list[0].HubPeerID != pc.peer.ID {
		return wire.Errf(wire.EUnknownStream, "No such stream here.")
	}
	mv := list[0]
	switch {
	case outgoing(mv) && rec.Closed:
		// The hub has let go of a channel it prepared for a move that was
		// abandoned.
		return s.store.Streams().DeleteChannel(ctx, st.ID)
	case outgoing(mv) && mv.State == store.MoveAccepted && rec.Prepared && rec.Has(s.id.Name):
		// Prepared: the commit can follow (runMove waits for this row).
		return s.storeChannel(ctx, s.store, f.Signed, f.Sig, rec)
	case outgoing(mv) && mv.State == store.MoveAbandoned && rec.Prepared:
		if err := s.storeChannel(ctx, s.store, f.Signed, f.Sig, rec); err != nil {
			return err
		}
		s.closePrepared(ctx, mv)
		return nil
	case !outgoing(mv) && mv.State == store.MoveAccepted && !rec.Prepared && rec.Has(s.id.Name):
		// The hub has activated the channel, so the owner server has
		// committed. This server switches only on stream.moved, which is
		// in the direct log: it asks for it.
		if rec.HomeHub == mv.HubName {
			s.keepAhead(ctx, s.rt(st.ID), pc, f, rec)
		}
		s.catchUp(ctx, st, true)
		return nil
	}
	return nil
}

// aheadRecord is a hub-signed channel record kept as it came.
type aheadRecord struct {
	signed, sig []byte
	rec         wire.ChannelRecord
}

// keepAhead keeps a record of a channel that the hub has activated and that
// reached the other server of the move before it switched; rt.record is
// held. The hub sends each version once, and stream.moved carries only the
// prepared record: without this the switch would leave this server on the
// prepared record, under the key epoch the direct life ended with, and its
// own posts would be refused as stale for good. Across a cluster nothing
// sends the record again. It is kept in memory, not written, because the
// switch's transaction may be writing the same row at this moment; it is
// stored once the switch has committed (takeAhead). If the switch has
// already committed, it is stored now.
func (s *Service) keepAhead(ctx context.Context, rt *streamRT, pc *peerConn, f wire.Frame, rec wire.ChannelRecord) {
	if rt.aheadRec == nil || rt.aheadRec.rec.Version < rec.Version {
		rt.aheadRec = &aheadRecord{signed: f.Signed, sig: f.Sig, rec: rec}
	}
	if st, err := s.store.Streams().Get(ctx, rt.id); err == nil && st.PeerID == pc.peer.ID {
		s.takeAheadLocked(ctx, rt)
	}
}

// takeAhead stores the record keepAhead kept, if it is newer than the one
// held, once this server has switched to the hub.
func (s *Service) takeAhead(ctx context.Context, rt *streamRT) {
	rt.record.Lock()
	s.takeAheadLocked(ctx, rt)
	rt.record.Unlock()
}

func (s *Service) takeAheadLocked(ctx context.Context, rt *streamRT) {
	e := rt.aheadRec
	if e == nil {
		return
	}
	rt.aheadRec = nil
	held, err := s.store.Streams().Channel(ctx, rt.id)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return
	}
	if err == nil && held.Version >= e.rec.Version {
		return
	}
	if err := s.storeChannel(ctx, s.store, e.signed, e.sig, e.rec); err != nil {
		s.log.Warn("link: a channel record that came before the switch could not be kept", "stream", rt.id, "err", err)
		return
	}
	s.adoptEpoch(ctx, rt.id)
}

// moved applies stream.moved on the other server of a moved stream: the
// switch. It runs in the transaction that applies the event, so this
// server's copy of the stream changes hands exactly when it has everything
// the owner server made before the event, and at no other time.
func (a *apply) moved(isOwner bool) (*rejection, error) {
	var b struct {
		MoveID    string     `json:"move_id"`
		Hub       string     `json:"hub"`
		Record    wire.Bytes `json:"record"`
		RecordSig wire.Bytes `json:"record_sig"`
		Commit    wire.Bytes `json:"commit"`
		CommitSig wire.Bytes `json:"commit_sig"`
	}
	if rej := a.decode(&b); rej != nil {
		return rej, nil
	}
	s, st := a.s, a.st
	if s.isSelf(a.origin.ServerID) {
		return nil, nil // this server's own: the commit made it
	}
	if !isOwner {
		return reject("not_owner"), nil
	}
	mv, err := a.tx.Streams().Move(a.ctx, b.MoveID)
	if errors.Is(err, model.ErrNotFound) || (err == nil && (mv.StreamID != st.ID || outgoing(mv))) {
		return reject("move_unknown"), nil
	}
	if err != nil {
		return nil, err
	}
	if mv.State != store.MoveAccepted {
		// The owner server commits only what this server agreed to, and the
		// hub checked the agreement; so this is not reached by servers that
		// keep the protocol.
		return &rejection{reason: "move_not_accepted", detail: map[string]any{"move_id": mv.ID, "state": mv.State}}, nil
	}
	hub, err := a.tx.Peers().Get(a.ctx, mv.HubPeerID)
	if err != nil {
		return reject("move_no_hub"), nil
	}
	var rec wire.ChannelRecord
	if err := s.verifyHubBlock(hub, wire.FChannel, b.Record, b.RecordSig, func() []byte { return rec.HubKey }, &rec); err != nil {
		return reject("move_bad_record"), nil
	}
	if rec.StreamID != string(st.ID) || rec.Hub != mv.HubName || wire.KeyFingerprint(rec.HubKey) != mv.HubFingerprint ||
		!rec.Has(s.id.Name) || !rec.Has(a.origin.Name) || len(rec.Servers) != 2 || rec.Closed {
		return reject("move_bad_record"), nil
	}
	owner, err := a.tx.Peers().Server(a.ctx, a.origin.ServerID)
	if err != nil {
		return nil, err
	}
	var mc wire.MoveCommit
	if err := wire.VerifyBytes(wire.FChannelCommit, b.Commit, b.CommitSig, owner.SignKey, &mc); err != nil ||
		mc.MoveID != mv.ID || mc.StreamID != string(st.ID) || mc.Hub != mv.HubName || mc.Owner != a.origin.Name {
		return reject("move_bad_commit"), nil
	}
	if s.step("switch") {
		return nil, errors.New("link: a test stopped the switch")
	}
	// What the owner server took of this server's own events, it took: it
	// is not sent again, least of all with another stamp. What it did not
	// take waits on, for the hub.
	taken := mc.Have[s.id.Name]
	rows, err := a.tx.Streams().Outbox(a.ctx, st.ID)
	if err != nil {
		return nil, err
	}
	var acked, kept []waiting
	for _, row := range rows {
		var w outboxRecord
		if json.Unmarshal(row.Envelope, &w) != nil || w.N == 0 {
			continue
		}
		if w.N > taken {
			kept = append(kept, waiting{row, w})
			continue
		}
		if _, err := a.tx.Streams().DeleteOutbox(a.ctx, row.ID); err != nil {
			return nil, err
		}
		if err := a.tx.Streams().Acked(a.ctx, st.ID, w.At); err != nil {
			return nil, err
		}
		acked = append(acked, waiting{row, w})
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].rec.N < kept[j].rec.N })
	// The channel's owner from here is the hub the move names: this
	// server's own hub, or the hub of its cluster the owner server is on.
	hubRow, err := s.hubRow(a.ctx, a.tx, hub, mv.HubName)
	if err != nil {
		return nil, err
	}
	moved, err := a.tx.Streams().Update(a.ctx, st.ID, func(x *store.Stream) error {
		x.PeerID, x.OwnerServerID, x.Sequencer, x.HomeHub, x.LastSeq = hub.ID, hubRow.ID, store.SequencerHub, mv.HubName, 0
		x.ServersVersion = rec.Version
		return nil
	})
	if err != nil {
		return nil, err
	}
	// What the owner server did not take is stamped again here, by the
	// hub's clock, in the transaction that hands the stream to the hub: no
	// frame can have carried these events to the hub under their old
	// stamps, because until this transaction commits this server sends the
	// hub nothing of the stream. They are stamped after stream.moved, which
	// this server holds as of now, and in the order they were made.
	if err := a.tx.Streams().Applied(a.ctx, st.ID, a.origin.ServerID, a.env.N, a.env.At); err != nil {
		return nil, err
	}
	if err := s.restampRows(a.ctx, a.tx, a.rec, moved, kept, true); err != nil {
		return nil, err
	}
	if held, err := a.tx.Streams().Channel(a.ctx, st.ID); err != nil || held.Version < rec.Version {
		if err := s.storeChannel(a.ctx, a.tx, b.Record, b.RecordSig, rec); err != nil {
			return nil, err
		}
	}
	mv.State, mv.CommitEventID, mv.Commit, mv.CommitSig, mv.UpdatedAt = store.MoveDone, a.env.OriginEventID, b.Commit, b.CommitSig, s.now()
	if err := a.tx.Streams().PutMove(a.ctx, mv); err != nil {
		return nil, err
	}
	a.rec.After(func() { go s.switched(s.ctx, mv, acked) })
	return nil, s.chat.ApplyConversation(a.ctx, a.tx, a.rec, a.conv.ID, nil, nil)
}

// switched is what follows the switch on the other server: whoever waited
// for what the owner server had taken is told, the hub is handed the commit
// in case the owner server has not done so, and the stream catches up with
// the hub and sends it what waits.
func (s *Service) switched(ctx context.Context, mv store.StreamMove, acked []waiting) {
	st, err := s.store.Streams().Get(ctx, mv.StreamID)
	if err != nil {
		return
	}
	rt := s.rt(st.ID)
	rt.mu.Lock()
	rt.inflight, rt.catching = 0, false
	rt.mu.Unlock()
	for _, w := range acked {
		s.acknowledged(ctx, st, w)
	}
	// A record of the hub's that came before the switch is the channel's
	// record from here, if it is newer than the one stream.moved carried.
	s.takeAhead(ctx, rt)
	s.audit(ctx, s.store, "", "link.move_done", "stream", string(st.ID), map[string]any{"move_id": mv.ID, "hub": mv.HubName})
	s.sendCommit(ctx, mv)
	s.catchUp(ctx, st, true)
	s.kickStream(ctx, st.ID, true)
	s.tellMove(ctx, mv)
}

// afterActivation is what a server does when the hub's record of a moved
// channel stops being merely prepared: the channel is live on the hub, so
// what waited goes, and what the hub took meanwhile comes.
func (s *Service) afterActivation(ctx context.Context, id model.StreamID) {
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil || st.State != store.StreamActive {
		return
	}
	s.catchUp(ctx, st, true)
	rt := s.rt(id)
	rt.mu.Lock()
	rt.inflight = 0
	rt.mu.Unlock()
	s.kickStream(ctx, id, true)
}

// resendMoves sends again, after a reconnection, what a move still owes the
// peer that has just connected.
func (s *Service) resendMoves(ctx context.Context, peer store.Peer) {
	list, err := s.store.Streams().Moves(ctx, "")
	if err != nil {
		return
	}
	recent := s.now().Add(-moveLifeMax)
	for _, mv := range list {
		switch {
		case mv.FromPeerID == peer.ID && outgoing(mv) && mv.State == store.MoveProposed:
			s.sendPropose(ctx, mv)
		case mv.FromPeerID == peer.ID && outgoing(mv) && mv.State == store.MoveCommitted:
			s.deliverMoved(ctx, mv)
		case mv.FromPeerID == peer.ID && outgoing(mv) && mv.State == store.MoveAbandoned && len(mv.Accept) > 0 && mv.UpdatedAt.After(recent):
			s.sendAbandon(ctx, mv)
		case mv.FromPeerID == peer.ID && !outgoing(mv) && mv.State == store.MoveAccepted:
			// The answer pulls the outcome: the owner server acts on it,
			// says the move is off, or sends stream.moved again.
			s.sendAccept(ctx, mv)
		case mv.HubPeerID == peer.ID && outgoing(mv) && (mv.State == store.MoveAccepted || mv.State == store.MoveCommitted):
			s.runMoveSoon(mv.ID)
		case mv.HubPeerID == peer.ID && mv.State == store.MoveDone && mv.UpdatedAt.After(recent):
			if rec, err := s.channelOf(ctx, s.store, mv.StreamID); err == nil && rec.Prepared {
				s.sendCommit(ctx, mv)
			}
		case mv.HubPeerID == peer.ID && outgoing(mv) && mv.State == store.MoveAbandoned && mv.UpdatedAt.After(recent):
			s.closePrepared(ctx, mv)
		}
	}
}

// tickMoves is the moves' share of the background work, once a second:
// proposals lapse, a move that was interrupted goes on, and what a finished
// move still owes someone is sent again.
func (s *Service) tickMoves(ctx context.Context, n int) {
	list, err := s.store.Streams().Moves(ctx, "")
	if err != nil {
		return
	}
	now := s.now()
	recent := now.Add(-time.Hour)
	for _, mv := range list {
		switch {
		case mv.State == store.MoveProposed && now.After(mv.ExpiresAt):
			if outgoing(mv) {
				s.abandonMove(ctx, mv, "expired")
				continue
			}
			if gone, err := s.setMove(ctx, mv.ID, func(x *store.StreamMove) error {
				if x.State != store.MoveProposed {
					return errMoveState
				}
				x.State, x.Reason = store.MoveAbandoned, "expired"
				return nil
			}); err == nil {
				s.tellMove(ctx, gone)
			}
		case outgoing(mv) && mv.State == store.MoveAccepted:
			s.runMove(ctx, mv.ID)
		case outgoing(mv) && mv.State == store.MoveCommitted && n%3 == 0:
			s.runMove(ctx, mv.ID)
		case !outgoing(mv) && mv.State == store.MoveAccepted && n%30 == 0:
			s.sendAccept(ctx, mv)
		case mv.State == store.MoveDone && mv.UpdatedAt.After(recent) && n%3 == 0:
			if rec, err := s.channelOf(ctx, s.store, mv.StreamID); err == nil && rec.Prepared {
				s.sendCommit(ctx, mv)
			}
		case outgoing(mv) && mv.State == store.MoveAbandoned && mv.UpdatedAt.After(recent) && n%3 == 0:
			s.closePrepared(ctx, mv)
		}
	}
}

// TestStep has fn asked before each step of a move, by name; where it says
// true the move stops, as a server that fails at that step does. Nil puts
// it back. It is for tests.
func (s *Service) TestStep(fn func(step string) bool) {
	if fn == nil {
		s.testStep.Store(nil)
		return
	}
	s.testStep.Store(&fn)
}
