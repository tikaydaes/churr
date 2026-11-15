package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Channel bans (docs/FEATURES.md section 19.1): the `channel_bans` table,
// part of the moderation area. While a ban is in force, recompute gives the
// account no row in the channel (membership.go), and the lists that show a
// public channel to anyone who may read it leave it out for that account
// (notBanned).

const channelBanCols = `id, conversation_id, account_id, reason, created_by, created_at, expires_at, lifted_at`

// banInForce is the condition, over a channel_bans row called cb, that the
// ban holds at a moment passed as the one argument.
const banInForce = `cb.lifted_at IS NULL AND (cb.expires_at IS NULL OR cb.expires_at > ?)`

func scanChannelBan(s scanner) (model.ChannelBan, error) {
	var b model.ChannelBan
	err := s.Scan(str(&b.ID), str(&b.ConversationID), str(&b.AccountID), &b.Reason, str(&b.CreatedByID), tm(&b.CreatedAt),
		tm(&b.ExpiresAt), tm(&b.LiftedAt))
	return b, err
}

// notBanned is a condition that the account is not banned, now, from the
// conversation whose id is the column convCol, with its arguments.
func notBanned(convCol string, account model.AccountID, now model.Time) (string, []any) {
	return `NOT EXISTS (SELECT 1 FROM channel_bans cb WHERE cb.conversation_id = ` + convCol + ` AND cb.account_id = ? AND ` + banInForce + `)`,
		[]any{string(account), int64(now)}
}

// channelBanned reports whether an account is banned from a conversation at
// a moment. recompute asks it before giving the account a row.
func (q *queries) channelBanned(ctx context.Context, conv model.ConversationID, account model.AccountID, at model.Time) (bool, error) {
	if at.IsZero() {
		// Some callers of recompute pass no moment; a ban's end is still
		// measured against the clock.
		at = model.Now()
	}
	var n int
	err := q.wr.QueryRowContext(ctx, `SELECT COUNT(*) FROM channel_bans cb WHERE cb.conversation_id = ? AND cb.account_id = ? AND `+banInForce,
		string(conv), string(account), int64(at)).Scan(&n)
	return n > 0, q.mapErr(err)
}

func (m moderation) BanFromChannel(ctx context.Context, b *model.ChannelBan) error {
	if b.ID == "" {
		b.ID = model.NewID[string]()
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = model.Now()
	}
	return m.atomic(ctx, func(q *queries) error {
		if _, err := q.wr.ExecContext(ctx, `UPDATE channel_bans SET lifted_at = ? WHERE conversation_id = ? AND account_id = ? AND lifted_at IS NULL`,
			int64(b.CreatedAt), string(b.ConversationID), string(b.AccountID)); err != nil {
			return q.mapErr(err)
		}
		_, err := q.wr.ExecContext(ctx, `INSERT INTO channel_bans (`+channelBanCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			b.ID, string(b.ConversationID), string(b.AccountID), b.Reason, string(b.CreatedByID), int64(b.CreatedAt),
			ms(b.ExpiresAt), ms(b.LiftedAt))
		return q.mapErr(err)
	})
}

func (m moderation) LiftChannelBan(ctx context.Context, conv model.ConversationID, account model.AccountID, at model.Time) (model.ChannelBan, bool, error) {
	var b model.ChannelBan
	found := false
	err := m.atomic(ctx, func(q *queries) error {
		var err error
		b, found, err = moderation{q}.ChannelBan(ctx, conv, account, at)
		if err != nil || !found {
			return err
		}
		b.LiftedAt = at
		_, err = q.wr.ExecContext(ctx, `UPDATE channel_bans SET lifted_at = ? WHERE id = ?`, int64(at), b.ID)
		return q.mapErr(err)
	})
	return b, found, err
}

func (m moderation) ChannelBan(ctx context.Context, conv model.ConversationID, account model.AccountID, now model.Time) (model.ChannelBan, bool, error) {
	rows, err := m.wr.QueryContext(ctx, `SELECT `+channelBanCols+` FROM channel_bans cb
		WHERE cb.conversation_id = ? AND cb.account_id = ? AND `+banInForce+` ORDER BY cb.created_at DESC LIMIT 1`,
		string(conv), string(account), int64(now))
	list, err := collect(rows, err, scanChannelBan)
	if err != nil || len(list) == 0 {
		return model.ChannelBan{}, false, m.mapErr(err)
	}
	return list[0], true, nil
}

func (m moderation) ChannelBans(ctx context.Context, conv model.ConversationID, now model.Time) ([]model.ChannelBan, error) {
	where := banInForce
	args := []any{int64(now)}
	if conv != "" {
		where += ` AND cb.conversation_id = ?`
		args = append(args, string(conv))
	} else {
		// The admin console's list: named channels only. What is done
		// inside an ad-hoc channel stays with its members.
		where += ` AND EXISTS (SELECT 1 FROM conversations c WHERE c.id = cb.conversation_id AND c.kind = 'channel' AND c.name IS NOT NULL)`
	}
	rows, err := m.rd.QueryContext(ctx, `SELECT `+channelBanCols+` FROM channel_bans cb WHERE `+where+
		` ORDER BY cb.created_at DESC, cb.id DESC LIMIT 1000`, args...)
	out, err := collect(rows, err, scanChannelBan)
	return out, m.mapErr(err)
}

func (m moderation) ChannelBansOf(ctx context.Context, account model.AccountID, now model.Time) ([]model.ConversationID, error) {
	rows, err := m.rd.QueryContext(ctx, `SELECT DISTINCT cb.conversation_id FROM channel_bans cb WHERE cb.account_id = ? AND `+banInForce,
		string(account), int64(now))
	out, err := collectIDs[model.ConversationID](rows, err)
	return out, m.mapErr(err)
}
