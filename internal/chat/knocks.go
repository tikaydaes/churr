package chat

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/store"
)

// Knocks (docs/FEATURES.md section 12.5, ruling R74): someone asks the
// managers of a private channel to add them, by its name. The one who knocks
// is given the same answer whether or not such a channel exists, whether
// they already asked, and however many channels have the name, so a private
// channel they cannot see cannot be found out by knocking. Its managers see
// the request and accept or decline it; the one who knocked is told which,
// and nothing about who answered.

// knockLimiter allows each account ten knocks an hour, counted whatever
// became of them, so that the limit says nothing about the channels either.
var knockLimiter = ratelimit.New(ratelimit.Per(10, time.Hour), 10)

// Knock asks the managers of every private channel of this name that the
// actor cannot see, is not in and is not banned from to add them. It returns
// the channels of the name the actor can see and join already, which they
// are told to join instead; nothing about the others.
func (s *Service) Knock(ctx context.Context, actor model.Account, name string) ([]model.ConversationID, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.Knock); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "#"))
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return nil, model.Invalid("name", "A channel's name is 1 to 64 characters.")
	}
	if res := knockLimiter.Take(string(actor.ID)); !res.OK {
		return nil, model.RateLimited(res.RetryAfterSeconds())
	}
	targets, err := s.store.Members().KnockTargets(ctx, name)
	if err != nil {
		return nil, err
	}
	var joinable []model.ConversationID
	for _, c := range targets {
		st, err := s.perm.Load(ctx, actor, c.ID)
		if err != nil {
			continue
		}
		switch {
		case st.IsMember() || st.ChannelBanned:
			continue
		case st.CanSee():
			if perm.Check(st, perm.Join) == nil {
				joinable = append(joinable, c.ID)
			}
			continue
		case !c.Private || c.Locked() || c.IsLibrary():
			// A locked channel takes nobody new, so nobody could answer.
			continue
		}
		k := model.Knock{ConversationID: c.ID, AccountID: actor.ID, CreatedAt: s.now()}
		created, err := s.store.Members().Knock(ctx, &k)
		if err != nil {
			return nil, err
		}
		if !created {
			continue
		}
		to, err := s.store.Members().Managers(ctx, c.ID)
		if err != nil {
			s.log.Warn("could not work out who to tell about a knock", "err", err)
		}
		s.tell(to, model.EventKnockCreated, map[string]any{"knock": k, "conversation_name": c.Name})
	}
	return joinable, nil
}

// Knocks lists the knocks waiting in a channel, for whoever may answer them:
// those acting as its manager.
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
// managers are told it was answered, so their lists drop it.
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
				if err := s.addTx(ctx, tx, rec, actor, id, []model.AccountID{k.AccountID}, ""); err != nil {
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
	to, err := s.store.Members().Managers(ctx, id)
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
