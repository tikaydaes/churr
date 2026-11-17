package chat

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Access alerts (docs/PERMISSIONS.md section 9.2, review finding H3). Some
// acts of an Owner or an Admin let them, or whoever they hand something to,
// read a conversation that is not public as somebody else: a new token for
// a bot in it, a recovery link or a claim link used on an account in it,
// putting someone into a group that brings them in, sending the channel to
// another instance. Each posts one of these notices in every conversation
// concerned, as owner_override does: clients show it as wanting attention,
// nobody can turn it off, and it names who did it (by_id) where that is
// known.
const (
	AlertBotToken      = "bot_token_made"
	AlertRecovered     = "account_recovered"
	AlertClaimed       = "account_claimed"
	AlertGroupAccess   = "group_access"
	AlertChannelShared = "channel_shared"
)

// alertable is whether a conversation is told of an access alert: anything
// that is not open to every account of the instance anyway, and not a sealed
// chat, which the instance cannot read whoever holds the account.
func alertable(c model.Conversation) bool {
	return !c.Public() && !c.Sealed() && !c.IsLibrary() && !c.Archived() && !c.Closed() && !c.Waiting()
}

// Alert is who an access alert is about: Account, whose conversations are
// told and who is the author of each notice; By, who did it ("" when
// unknown, such as the command line); and Name, shown with it (a group's or
// an instance's name) when not empty. An empty Account posts nothing.
type Alert struct {
	Account model.AccountID
	By      model.AccountID
	Name    string
}

// WithAlert runs fn and, in the same transaction, posts an access alert of
// kind in every conversation account is in that is told of one
// (alertable): the alert is made exactly when the act is. fn may be nil.
func (s *Service) WithAlert(ctx context.Context, account, by model.AccountID, kind, name string, fn func(tx store.Tx) error) error {
	return s.Alerting(ctx, kind, func(tx store.Tx) (Alert, error) {
		if fn != nil {
			if err := fn(tx); err != nil {
				return Alert{}, err
			}
		}
		return Alert{Account: account, By: by, Name: name}, nil
	})
}

// Alerting is WithAlert for an act that learns inside its transaction whom
// it concerns, such as a recovery link that names its account only when it
// is used.
func (s *Service) Alerting(ctx context.Context, kind string, fn func(tx store.Tx) (Alert, error)) error {
	return s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		a, err := fn(tx)
		if err != nil {
			return err
		}
		return rec.Alert(ctx, kind, a)
	})
}

// Alert posts, in this transaction, an access alert of kind in every
// conversation a.Account is in that is told of one (alertable).
func (r *Recorder) Alert(ctx context.Context, kind string, a Alert) error {
	if a.Account == "" {
		return nil
	}
	ids, err := r.tx.Members().ConversationIDs(ctx, a.Account)
	if err != nil || len(ids) == 0 {
		return err
	}
	convs, err := r.tx.Conversations().GetMany(ctx, ids)
	if err != nil {
		return err
	}
	for _, c := range convs {
		if !alertable(c) {
			continue
		}
		if err := r.s.alert(ctx, r.tx, r, c, a.Account, a.By, kind, a.Name); err != nil {
			return err
		}
	}
	return nil
}

// AlertConversation posts an access alert in one conversation, if it is one
// that is told (alertable).
func (s *Service) AlertConversation(ctx context.Context, id model.ConversationID, author, by model.AccountID, kind, name string) error {
	c, err := s.store.Conversations().Get(ctx, id)
	if err != nil || !alertable(c) {
		return err
	}
	return s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		return s.alert(ctx, tx, rec, c, author, by, kind, name)
	})
}

func (s *Service) alert(ctx context.Context, tx store.Tx, rec *Recorder, c model.Conversation, author, by model.AccountID, kind, name string) error {
	system := map[string]any{"kind": kind}
	if by != "" {
		system["by_id"] = string(by)
	}
	if name != "" {
		system["name"] = name
	}
	_, err := s.systemNotice(ctx, tx, rec, c, author, system)
	return err
}
