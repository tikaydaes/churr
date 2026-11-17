// Package notices leaves kept notices for accounts (model.Notice,
// docs/FEATURES.md section 21.13): something done that concerns an account,
// which it hears of when it next looks even if it was not connected, and
// which is not a message in any conversation.
//
// A notice is written with the act, in its transaction (Add), and the
// account's connections are told once that has committed (Tell). Notify
// does both for an act that is not in a transaction.
package notices

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Hub is what is needed of the live hub: sending an account an event that
// belongs to no conversation. live.Hub implements it.
type Hub interface {
	Send(account model.AccountID, v any)
}

// About is what a notice is about, beyond its kind and who did it: an
// account other than its holder, and a conversation. Either may be empty.
type About struct {
	AccountID      model.AccountID
	ConversationID model.ConversationID
}

// Add stores a notice of kind for to, done by by ("" when unknown), in a's
// transaction. Call Tell with what it returns once the transaction has
// committed.
func Add(ctx context.Context, a store.Areas, to model.AccountID, kind string, by model.AccountID, about About) (model.Notice, error) {
	n := model.Notice{AccountID: to, Kind: kind, ByID: by, SubjectID: about.AccountID, ConversationID: about.ConversationID, CreatedAt: model.Now()}
	return n, a.Notices().Add(ctx, &n)
}

// AddForOwners stores a notice of kind for every active Owner of the
// instance, in a's transaction, and returns them for Tell.
func AddForOwners(ctx context.Context, a store.Areas, kind string, by model.AccountID, about About) ([]model.Notice, error) {
	owners, err := Owners(ctx, a)
	if err != nil {
		return nil, err
	}
	out := make([]model.Notice, 0, len(owners))
	for _, o := range owners {
		n, err := Add(ctx, a, o, kind, by, about)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// Owners lists the active Owners of the instance.
func Owners(ctx context.Context, a store.Areas) ([]model.AccountID, error) {
	var ids []model.AccountID
	p := store.Page{Limit: model.MaxLimit}
	for {
		list, next, err := a.Accounts().List(ctx, store.AccountFilter{Role: model.RoleOwner}, p)
		if err != nil {
			return nil, err
		}
		for _, acc := range list {
			if acc.Active() {
				ids = append(ids, acc.ID)
			}
		}
		if next == "" {
			return ids, nil
		}
		p.Cursor = next
	}
}

// Tell sends each notice to its holder's connections as notice.created.
// A notice with no holder (the zero value) is skipped, so a caller may
// pass what an Add that did not happen left behind.
func Tell(hub Hub, list ...model.Notice) {
	if hub == nil {
		return
	}
	for _, n := range list {
		if n.AccountID == "" {
			continue
		}
		hub.Send(n.AccountID, model.Event{Type: model.EventNoticeCreated, At: n.CreatedAt, Payload: map[string]any{"notice": n}})
	}
}

// Notify stores a notice and tells its holder at once, for an act that is
// not in a transaction of its own.
func Notify(ctx context.Context, s store.Store, hub Hub, to model.AccountID, kind string, by model.AccountID, about About) error {
	n, err := Add(ctx, s, to, kind, by, about)
	if err != nil {
		return err
	}
	Tell(hub, n)
	return nil
}
