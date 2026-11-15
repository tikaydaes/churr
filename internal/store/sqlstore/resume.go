package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// The reads a catch-up makes once for one reader and many messages or
// conversations, in place of one read for each of them: the other side of
// fanout.go, which reads once for one message and many readers.

func (ms messages) MentionKinds(ctx context.Context, account model.AccountID, ids []model.MessageID) (map[model.MessageID]string, error) {
	out := map[model.MessageID]string{}
	if len(ids) == 0 {
		return out, nil
	}
	args := append([]any{string(account)}, anys(ids)...)
	rows, err := ms.rd.QueryContext(ctx, `SELECT message_id, kind FROM mentions WHERE account_id = ? AND message_id IN (`+marks(len(ids))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var msg model.MessageID
		var kind string
		if err := rows.Scan(str(&msg), &kind); err != nil {
			return nil, err
		}
		out[msg] = kind
	}
	return out, rows.Err()
}

func (t threads) StatesOf(ctx context.Context, account model.AccountID, parents []model.MessageID) (map[model.MessageID]string, error) {
	out := map[model.MessageID]string{}
	if len(parents) == 0 {
		return out, nil
	}
	args := append([]any{string(account)}, anys(parents)...)
	rows, err := t.rd.QueryContext(ctx, `SELECT parent_id, state FROM thread_states WHERE account_id = ? AND parent_id IN (`+marks(len(parents))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var parent model.MessageID
		var state string
		if err := rows.Scan(str(&parent), &state); err != nil {
			return nil, err
		}
		out[parent] = state
	}
	return out, rows.Err()
}

func (m moderation) TimedOutEverywhere(ctx context.Context, account model.AccountID, now model.Time) (model.Time, map[model.ConversationID]model.Time, error) {
	// The same rows TimedOutUntil reads, for every conversation at once.
	rows, err := m.rd.QueryContext(ctx, `SELECT conversation_id, MAX(until) FROM timeouts
		WHERE account_id = ? AND lifted_at IS NULL AND until > ? GROUP BY conversation_id`, string(account), int64(now))
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	var server model.Time
	by := map[model.ConversationID]model.Time{}
	for rows.Next() {
		var conv model.ConversationID
		var until model.Time
		if err := rows.Scan(str(&conv), tm(&until)); err != nil {
			return 0, nil, err
		}
		if conv == "" {
			server = until
		} else {
			by[conv] = until
		}
	}
	return server, by, rows.Err()
}
