package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// The reads a fan-out makes once for everyone, in place of one read for
// each recipient.

func (m members) All(ctx context.Context, conv model.ConversationID) ([]model.Member, error) {
	rows, err := m.rd.QueryContext(ctx, `SELECT `+memberCols+` FROM members WHERE conversation_id = ?`, string(conv))
	return collect(rows, err, scanMember)
}

func (t threads) States(ctx context.Context, parent model.MessageID) (map[model.AccountID]string, error) {
	rows, err := t.rd.QueryContext(ctx, `SELECT account_id, state FROM thread_states WHERE parent_id = ?`, string(parent))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[model.AccountID]string{}
	for rows.Next() {
		var account, state string
		if err := rows.Scan(&account, &state); err != nil {
			return nil, err
		}
		out[model.AccountID(account)] = state
	}
	return out, rows.Err()
}

func (n notifyAnyway) Reached(ctx context.Context, msg model.MessageID) ([]model.AccountID, error) {
	return collectIDs[model.AccountID](n.rd.QueryContext(ctx,
		`SELECT account_id FROM notify_anyway WHERE message_id = ? AND delivered = ?`, string(msg), true))
}
