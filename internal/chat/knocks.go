package chat

import (
	"context"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Knocks (docs/FEATURES.md section 12.5, rulings R74 and R79): someone asks
// the moderators and managers of a knock channel, which they can see and not read, to add
// them. They see the request and accept or decline it; the one who
// knocked is told which, and nothing about who answered.

// KnocksUnacceptedPerHour is how many knocks one account may make in a
// rolling hour that were not accepted: waiting or declined (ruling R85). A
// knock that was accepted does not count, so knocking again and again after
// declines cannot flood them while being let in costs nothing.
const KnocksUnacceptedPerHour = 10

// Knock asks the moderators and managers of a knock channel the actor can see and is not
// in to add them. Asking again while a request waits changes nothing.
func (s *Service) Knock(ctx context.Context, actor model.Account, id model.ConversationID) (model.Knock, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.Knock); err != nil {
		return model.Knock{}, err
	}
	st, err := s.seen(ctx, actor, id)
	if err != nil {
		return model.Knock{}, err
	}
	c := st.Conversation
	switch {
	case st.IsMember():
		return model.Knock{}, model.ErrConflict.With("You are in this channel already.")
	case c.Public():
		return model.Knock{}, model.ErrConflict.With("Anyone may join this channel: join it.")
	case !c.IsKnock():
		return model.Knock{}, model.ErrForbidden.With("This channel does not take requests to join.")
	case c.Archived():
		return model.Knock{}, model.ErrArchived
	case c.Locked():
		return model.Knock{}, model.ErrLocked
	}
	for _, w := range mustKnocks(s.store.Members().Knocks(ctx, id)) {
		if w.AccountID == actor.ID {
			return w, nil // the one waiting already
		}
	}
	now := s.now()
	n, oldest, err := s.store.Members().UnacceptedKnocks(ctx, actor.ID, now.Add(-time.Hour))
	if err != nil {
		return model.Knock{}, err
	}
	if n >= KnocksUnacceptedPerHour {
		wait := int(oldest.Add(time.Hour).Sub(now).Seconds()) + 1
		return model.Knock{}, model.RateLimited(max(wait, 1))
	}
	k := model.Knock{ConversationID: id, AccountID: actor.ID, CreatedAt: s.now()}
	created, err := s.store.Members().Knock(ctx, &k)
	if err != nil {
		return model.Knock{}, err
	}
	if !created {
		// The one waiting already.
		for _, w := range mustKnocks(s.store.Members().Knocks(ctx, id)) {
			if w.AccountID == actor.ID {
				return w, nil
			}
		}
		return k, nil
	}
	to, err := s.store.Members().Keepers(ctx, id)
	if err != nil {
		s.log.Warn("could not work out who to tell about a knock", "err", err)
	}
	s.tell(to, model.EventKnockCreated, map[string]any{"knock": k, "conversation_name": c.Name})
	return k, nil
}

// TellKnock tells the moderators and managers given of a new request to
// join, as Knock does: the link, for a request that arrived from another
// instance of a private channel a hub owns.
func (s *Service) TellKnock(c model.Conversation, k model.Knock, to []model.AccountID) {
	s.tell(to, model.EventKnockCreated, map[string]any{"knock": k, "conversation_name": c.Name})
}

func mustKnocks(list []model.Knock, err error) []model.Knock {
	if err != nil {
		return nil
	}
	return list
}

// Knocks lists the knocks waiting in a channel, for whoever may answer them:
// its moderators and managers.
func (s *Service) Knocks(ctx context.Context, actor model.Account, id model.ConversationID) ([]model.Knock, error) {
	st, err := s.seen(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if err := perm.Check(st, perm.ListBans); err != nil {
		return nil, err
	}
	if !st.Conversation.Named() {
		return []model.Knock{}, nil
	}
	return s.store.Members().Knocks(ctx, id)
}

// AnswerKnock accepts or declines a knock. Accepting adds the account at the
// channel's default level, as adding them would, with every rule of adding.
// The one who knocked is told the outcome and the channel's name; the
// moderators and managers are told it was answered, so their lists drop it.
func (s *Service) AnswerKnock(ctx context.Context, actor model.Account, id model.ConversationID, knockID string, accept bool) (model.Knock, error) {
	st, err := s.seen(ctx, actor, id)
	if err != nil {
		return model.Knock{}, err
	}
	if err := perm.Check(st, perm.AnswerKnock); err != nil {
		return model.Knock{}, err
	}
	outcome := model.KnockDeclined
	if accept {
		outcome = model.KnockAccepted
	}
	var k model.Knock
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		var found bool
		var err error
		if k, found, err = tx.Members().AnswerKnock(ctx, id, knockID, actor.ID, outcome, s.now()); err != nil {
			return err
		}
		if !found {
			return model.ErrNotFound.With("That request has been answered already, or was never made.")
		}
		if accept {
			_, member, err := tx.Members().Get(ctx, id, k.AccountID)
			if err != nil {
				return err
			}
			if !member {
				if err := s.addTx(ctx, tx, rec, actor, id, []model.AccountID{k.AccountID}, "", false); err != nil {
					return err
				}
			}
		}
		return s.audit(ctx, tx, st.Conversation, actor.ID, "knock."+outcome, "account", string(k.AccountID),
			map[string]any{"conversation_id": id, "knock_id": k.ID})
	})
	if err != nil {
		return model.Knock{}, hide(err)
	}
	to, err := s.store.Members().Keepers(ctx, id)
	if err != nil {
		s.log.Warn("could not work out who to tell about a knock", "err", err)
	}
	s.tell(append(to, actor.ID), model.EventKnockAnswered, map[string]any{"knock": k, "conversation_name": st.Conversation.Name})
	// The one who knocked is not told who answered.
	told := k
	told.AnsweredByID = ""
	s.tell([]model.AccountID{k.AccountID}, model.EventKnockAnswered, map[string]any{"knock": told, "conversation_name": st.Conversation.Name})
	return k, nil
}
