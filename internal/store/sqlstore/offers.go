package sqlstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Channel offers and default channels (ruling R103). A group added to a
// channel gives its people access, not a place: each is offered the
// channel and is a member once they accept. A default channel is joined at
// sign-up; one made a default channel later is offered to the people who
// are already here. recompute (membership.go) keeps the group offers in
// step with the groups; this file answers them and keeps the default ones.

// groupLevelsOf returns the levels of the groups added to a channel that
// contain an account.
func (q *queries) groupLevelsOf(ctx context.Context, conv model.ConversationID, account model.AccountID) ([]model.Level, error) {
	rows, err := q.wr.QueryContext(ctx, `
		SELECT cg.level FROM channel_groups cg JOIN groups g ON g.id = cg.group_id
		WHERE cg.conversation_id = ? AND `+groupContains,
		string(conv), string(account), string(account), string(account))
	return collect(rows, err, func(s scanner) (model.Level, error) {
		var l model.Level
		err := s.Scan(str(&l))
		return l, err
	})
}

// eligible is Members.Eligible on a handle that sees the transaction. It
// returns the level joining brings and the offer's source, or "" for both.
func (q *queries) eligible(ctx context.Context, conv model.ConversationID, account model.AccountID) (model.Level, string, error) {
	var kind, defaultLevel string
	var name, stream *string
	err := q.wr.QueryRowContext(ctx, `SELECT kind, name, default_level, stream_id FROM conversations WHERE id = ?`, string(conv)).
		Scan(&kind, &name, &defaultLevel, &stream)
	if err != nil {
		return "", "", q.mapErr(err)
	}
	if kind != model.KindChannel || name == nil {
		return "", "", nil
	}
	if banned, err := q.channelBanned(ctx, conv, account, model.Now()); err != nil || banned {
		return "", "", err
	}
	levels, err := q.groupLevelsOf(ctx, conv, account)
	if err != nil {
		return "", "", err
	}
	if len(levels) > 0 {
		var role string
		if err := q.wr.QueryRowContext(ctx, `SELECT role FROM accounts WHERE id = ?`, string(account)).Scan(&role); err != nil {
			return "", "", q.mapErr(err)
		}
		level := model.HighestLevel(levels...)
		if role == string(model.RoleGuest) {
			if stream != nil {
				return "", "", nil
			}
			if level.AtLeast(model.LevelModerator) {
				level = model.LevelMember
			}
		}
		return level, model.OfferGroup, nil
	}
	o, found, err := q.offerRow(ctx, conv, account)
	if err != nil || !found || o.Source != model.OfferDefault {
		return "", "", err
	}
	return model.Level(defaultLevel), model.OfferDefault, nil
}

func (m members) Offer(ctx context.Context, conv model.ConversationID, account model.AccountID) (model.ChannelOffer, bool, error) {
	return m.offerRow(ctx, conv, account)
}

func (m members) Eligible(ctx context.Context, conv model.ConversationID, account model.AccountID) (model.Level, bool, error) {
	level, _, err := m.eligible(ctx, conv, account)
	return level, level != "", err
}

func (m members) AnswerOffer(ctx context.Context, conv model.ConversationID, account model.AccountID, accept bool, at model.Time) (store.MembershipResult, error) {
	var res store.MembershipResult
	if at == 0 {
		at = model.Now()
	}
	err := m.atomic(ctx, func(q *queries) error {
		c, err := q.conversationState(ctx, conv)
		if err != nil {
			return err
		}
		level, source, err := q.eligible(ctx, conv, account)
		if err != nil {
			return err
		}
		if level == "" {
			return model.ErrNotFound
		}
		if accept && c.Locked() {
			return model.ErrLocked
		}
		state := model.OfferDeclined
		if accept {
			state = model.OfferAccepted
		}
		if err := q.putOffer(ctx, conv, account, source, state, at); err != nil {
			return err
		}
		var setDirect *model.Level
		if source == model.OfferDefault && accept {
			cur, found, err := q.memberRow(ctx, conv, account)
			if err != nil {
				return err
			}
			if !found || cur.DirectLevel == "" {
				setDirect = &level
			}
		}
		ch, err := q.recompute(ctx, conv, account, setDirect, at)
		if err != nil {
			return err
		}
		if ch != nil {
			res.Changes = append(res.Changes, *ch)
		}
		return q.settle(ctx, conv, &res)
	})
	return res, err
}

func (m members) Offers(ctx context.Context, account model.AccountID) ([]model.ChannelOffer, error) {
	rows, err := m.wr.QueryContext(ctx, `
		SELECT o.conversation_id, o.source, o.created_at FROM channel_offers o JOIN conversations c ON c.id = o.conversation_id
		WHERE o.account_id = ? AND o.state = 'offered' AND c.archived_at IS NULL AND c.locked_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM members m WHERE m.conversation_id = o.conversation_id AND m.account_id = o.account_id)
		ORDER BY o.created_at, o.conversation_id`, string(account))
	list, err := collect(rows, err, func(s scanner) (model.ChannelOffer, error) {
		o := model.ChannelOffer{AccountID: account, State: model.OfferOffered}
		err := s.Scan(str(&o.ConversationID), &o.Source, tm(&o.CreatedAt))
		return o, err
	})
	if err != nil {
		return nil, err
	}
	out := []model.ChannelOffer{}
	for _, o := range list {
		level, _, err := m.eligible(ctx, o.ConversationID, account)
		if err != nil {
			return nil, err
		}
		if level == "" {
			continue
		}
		o.Level = level
		out = append(out, o)
	}
	return out, nil
}

func (m members) OfferDefault(ctx context.Context, conv model.ConversationID, at model.Time) (store.MembershipResult, error) {
	var res store.MembershipResult
	if at == 0 {
		at = model.Now()
	}
	err := m.atomic(ctx, func(q *queries) error {
		var level string
		if err := q.wr.QueryRowContext(ctx, `SELECT default_level FROM conversations WHERE id = ?`, string(conv)).Scan(&level); err != nil {
			return q.mapErr(err)
		}
		ids, err := collectIDs[model.AccountID](q.wr.QueryContext(ctx, `
			SELECT a.id FROM accounts a WHERE `+inEveryone+`
			AND NOT EXISTS (SELECT 1 FROM members m WHERE m.conversation_id = ? AND m.account_id = a.id)
			AND NOT EXISTS (SELECT 1 FROM channel_offers o WHERE o.conversation_id = ? AND o.account_id = a.id)
			ORDER BY a.id`, string(conv), string(conv)))
		if err != nil {
			return err
		}
		for _, a := range ids {
			if banned, err := q.channelBanned(ctx, conv, a, at); err != nil {
				return err
			} else if banned {
				continue
			}
			if err := q.putOffer(ctx, conv, a, model.OfferDefault, model.OfferOffered, at); err != nil {
				return err
			}
			res.Changes = append(res.Changes, store.MemberChange{Kind: store.MemberOffered,
				Member: model.Member{ConversationID: conv, AccountID: a, Level: model.Level(level), JoinedAt: at}})
		}
		return nil
	})
	return res, err
}

func (m members) WithdrawDefault(ctx context.Context, conv model.ConversationID) (store.MembershipResult, error) {
	var res store.MembershipResult
	err := m.atomic(ctx, func(q *queries) error {
		waiting, err := collectIDs[model.AccountID](q.wr.QueryContext(ctx, `
			SELECT o.account_id FROM channel_offers o WHERE o.conversation_id = ? AND o.source = 'default' AND o.state = 'offered'
			AND NOT EXISTS (SELECT 1 FROM members m WHERE m.conversation_id = o.conversation_id AND m.account_id = o.account_id)
			ORDER BY o.account_id`, string(conv)))
		if err != nil {
			return err
		}
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM channel_offers WHERE conversation_id = ? AND source = 'default'`, string(conv)); err != nil {
			return q.mapErr(err)
		}
		for _, a := range waiting {
			res.Changes = append(res.Changes, store.MemberChange{Kind: store.MemberOfferGone,
				Member: model.Member{ConversationID: conv, AccountID: a}})
		}
		return nil
	})
	return res, err
}

// joinDefaults puts a new account into the instance's default channels
// (ruling R103): an owner, admin or member, when it is created or claimed.
// Default channels apply at sign-up only; a channel that is gone, archived,
// locked or not a named channel is passed over.
func (q *queries) joinDefaults(ctx context.Context, account model.AccountID, at model.Time, res *store.MembershipResult) error {
	var full bool
	if err := q.wr.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM accounts a WHERE a.id = ? AND `+inEveryone+`)`, string(account)).
		Scan(flag(&full)); err != nil {
		return q.mapErr(err)
	}
	if !full {
		return nil
	}
	raw, found, err := q.Settings().Get(ctx, store.SettingDefaultChannels)
	if err != nil || !found {
		return err
	}
	var ids []model.ConversationID
	if json.Unmarshal(raw, &ids) != nil {
		return nil
	}
	for _, conv := range ids {
		var level string
		var locked, archived *int64
		err := q.wr.QueryRowContext(ctx, `SELECT default_level, locked_at, archived_at FROM conversations
			WHERE id = ? AND kind = 'channel' AND name IS NOT NULL`, string(conv)).Scan(&level, &locked, &archived)
		if errors.Is(q.mapErr(err), model.ErrNotFound) {
			continue
		}
		if err != nil {
			return q.mapErr(err)
		}
		if locked != nil || archived != nil {
			continue
		}
		cur, found, err := q.memberRow(ctx, conv, account)
		if err != nil {
			return err
		}
		if found && cur.DirectLevel != "" {
			continue
		}
		l := model.Level(level)
		ch, err := q.recompute(ctx, conv, account, &l, at)
		if err != nil {
			return err
		}
		if ch != nil {
			res.Changes = append(res.Changes, *ch)
		}
		if err := q.settle(ctx, conv, res); err != nil {
			return err
		}
	}
	return nil
}
