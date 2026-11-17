package people

import (
	"context"
	"io"
	"log/slog"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Hub is what people needs of the live hub to tell others about an account
// and to end a removed account's connections. live.Hub implements it.
type Hub interface {
	Presence(account model.AccountID) model.Presence
	Send(account model.AccountID, v any)
	CloseAccount(account model.AccountID, code int, reason string)
}

// Conversations is what people needs of the chat service: a way to change
// who is in conversations (through a group, a role, a removal) so that the
// change and its member events happen together and in order.
type Conversations interface {
	// ChangeMembership runs fn in one transaction and turns the result
	// into member.joined, member.left and member.level_changed. by is who
	// caused it ("" for the account itself or a group); reason is the
	// reason of member.left.
	ChangeMembership(ctx context.Context, by model.AccountID, reason string, fn func(tx store.Tx) (store.MembershipResult, error)) error
	// ChangeMembershipLoud is ChangeMembership for a change an Owner or
	// an Admin makes from the console: each arrival in a private channel
	// is announced with the access alert kind, naming by and name.
	ChangeMembershipLoud(ctx context.Context, by model.AccountID, reason, kind, name string, fn func(tx store.Tx) (store.MembershipResult, error)) error
}

// Blobs is what people needs of the file store, for avatars.
type Blobs interface {
	Put(ctx context.Context, r io.Reader) (key string, size int64, sha256 []byte, err error)
	Delete(ctx context.Context, key string) error
}

// Totals is told when an account's numbers may have changed without a
// conversation event (a block, a preference). notify.Service implements it.
type Totals interface {
	Forget(accounts ...model.AccountID)
}

// Deps is what Connect supplies: the services built after this one.
type Deps struct {
	Hub    Hub
	Chat   Conversations
	Blobs  Blobs
	Totals Totals
	Log    *slog.Logger
}

// Connect hands the service what it needs from the services built after it.
// It is called once while the server is being put together.
func (s *Service) Connect(d Deps) {
	if d.Hub != nil {
		s.hub, s.presence = d.Hub, d.Hub
	}
	s.convs, s.blobs, s.totals = d.Chat, d.Blobs, d.Totals
	if d.Log != nil {
		s.log = d.Log
	}
}

// noHub stands in until Connect: nobody is connected and nothing is sent.
type noHub struct{}

func (noHub) Presence(model.AccountID) model.Presence { return model.PresenceOffline }
func (noHub) Send(model.AccountID, any)               {}
func (noHub) CloseAccount(model.AccountID, int, string) {
}

// changeMembershipLoud is changeMembership with arrivals announced by an
// access alert (Conversations.ChangeMembershipLoud).
func (s *Service) changeMembershipLoud(ctx context.Context, by model.AccountID, reason, kind, name string, fn func(tx store.Tx) (store.MembershipResult, error)) error {
	if s.convs != nil {
		return s.convs.ChangeMembershipLoud(ctx, by, reason, kind, name, fn)
	}
	return s.changeMembership(ctx, by, reason, fn)
}

// changeMembership runs a membership change through the chat service, or
// plainly in a transaction when there is none (a service built by itself in
// a test), in which case no member events are sent.
func (s *Service) changeMembership(ctx context.Context, by model.AccountID, reason string, fn func(tx store.Tx) (store.MembershipResult, error)) error {
	if s.convs != nil {
		return s.convs.ChangeMembership(ctx, by, reason, fn)
	}
	return s.store.Tx(ctx, func(tx store.Tx) error {
		_, err := fn(tx)
		return err
	})
}

func (s *Service) forget(accounts ...model.AccountID) {
	if s.totals != nil {
		s.totals.Forget(accounts...)
	}
}

// event builds an account event: `type`, `at` and the fields given.
func (s *Service) event(eventType string, fields map[string]any) model.Event {
	return model.Event{Type: eventType, At: s.now(), Payload: fields}
}

// audit writes one line of the audit log inside a transaction.
func (s *Service) audit(ctx context.Context, tx store.Areas, actor model.Account, action, targetKind, targetID string, detail map[string]any) error {
	return tx.Audit().Append(ctx, model.AuditEntry{At: s.now(), ActorID: string(actor.ID), ActorKind: "account",
		Action: action, TargetKind: targetKind, TargetID: targetID, Detail: detail})
}
