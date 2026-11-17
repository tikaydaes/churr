package sqlstore

import (
	"context"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
)

// Knocks (ruling R74): the `knocks` table, part of the members area. A
// partial unique index keeps one knock waiting per account and channel.

const knockCols = `k.id, k.conversation_id, k.account_id, k.created_at, k.outcome, k.answered_by, k.answered_at`

func scanKnock(s scanner) (model.Knock, error) {
	var k model.Knock
	err := s.Scan(str(&k.ID), str(&k.ConversationID), str(&k.AccountID), tm(&k.CreatedAt), &k.Outcome, str(&k.AnsweredByID), tm(&k.AnsweredAt))
	return k, err
}

func (m members) Knock(ctx context.Context, k *model.Knock) (bool, error) {
	if k.ID == "" {
		k.ID = model.NewID[string]()
	}
	if k.CreatedAt.IsZero() {
		k.CreatedAt = model.Now()
	}
	k.Outcome = model.KnockPending
	created := false
	err := m.atomic(ctx, func(q *queries) error {
		var n int
		if err := q.wr.QueryRowContext(ctx, `SELECT COUNT(*) FROM knocks WHERE conversation_id = ? AND account_id = ? AND outcome = 'pending'`,
			string(k.ConversationID), string(k.AccountID)).Scan(&n); err != nil {
			return q.mapErr(err)
		}
		if n > 0 {
			return nil
		}
		_, err := q.wr.ExecContext(ctx, `INSERT INTO knocks (id, conversation_id, account_id, created_at, outcome) VALUES (?, ?, ?, ?, 'pending')`,
			k.ID, string(k.ConversationID), string(k.AccountID), int64(k.CreatedAt))
		created = err == nil
		return q.mapErr(err)
	})
	if errors.Is(err, model.ErrAlreadyExists) {
		// Two knocks at once: the other one is the one waiting.
		return false, nil
	}
	return created, err
}

func (m members) Knocks(ctx context.Context, conv model.ConversationID) ([]model.Knock, error) {
	rows, err := m.rd.QueryContext(ctx, `SELECT `+knockCols+` FROM knocks k JOIN accounts a ON a.id = k.account_id
		WHERE k.conversation_id = ? AND k.outcome = 'pending' AND a.removed_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM members mb WHERE mb.conversation_id = k.conversation_id AND mb.account_id = k.account_id)
		ORDER BY k.created_at, k.id LIMIT 500`, string(conv))
	out, err := collect(rows, err, scanKnock)
	return out, m.mapErr(err)
}

func (m members) UnacceptedKnocks(ctx context.Context, account model.AccountID, since model.Time) (int, model.Time, error) {
	var n int
	var oldest model.Time
	err := m.wr.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MIN(created_at), 0) FROM knocks
		WHERE account_id = ? AND created_at >= ? AND outcome <> 'accepted'`,
		string(account), int64(since)).Scan(&n, tm(&oldest))
	return n, oldest, m.mapErr(err)
}

func (m members) AnswerKnock(ctx context.Context, conv model.ConversationID, id string, by model.AccountID, outcome string, at model.Time) (model.Knock, bool, error) {
	var k model.Knock
	found := false
	err := m.atomic(ctx, func(q *queries) error {
		rows, err := q.wr.QueryContext(ctx, `SELECT `+knockCols+` FROM knocks k WHERE k.id = ? AND k.conversation_id = ? AND k.outcome = 'pending'`,
			id, string(conv))
		list, err := collect(rows, err, scanKnock)
		if err != nil || len(list) == 0 {
			return q.mapErr(err)
		}
		k, found = list[0], true
		k.Outcome, k.AnsweredByID, k.AnsweredAt = outcome, by, at
		_, err = q.wr.ExecContext(ctx, `UPDATE knocks SET outcome = ?, answered_by = ?, answered_at = ? WHERE id = ?`,
			outcome, string(by), int64(at), id)
		return q.mapErr(err)
	})
	return k, found, err
}
