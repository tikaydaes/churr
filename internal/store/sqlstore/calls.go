package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type calls struct{ *queries }

// Calls returns the calls area.
func (q *queries) Calls() store.Calls { return calls{q} }

const callColumns = `c.id, c.conversation_id, c.message_id, c.started_by, c.started_at, c.ended_at, c.host_server_id, s.name`

const callFrom = ` FROM calls c LEFT JOIN servers s ON s.id = c.host_server_id `

func scanCall(s scanner) (store.StoredCall, error) {
	var c store.StoredCall
	err := s.Scan(str(&c.ID), str(&c.ConversationID), str(&c.MessageID), str(&c.StartedBy), tm(&c.StartedAt), tm(&c.EndedAt),
		str(&c.HostServerID), str(&c.HostName))
	return c, err
}

func (c calls) Start(ctx context.Context, call store.StoredCall) (bool, error) {
	// The unique index on live calls is what makes two starts at the same
	// moment one call: the second insert does nothing.
	n, err := c.affected(c.wr.ExecContext(ctx, `
		INSERT INTO calls (id, conversation_id, message_id, started_by, started_at, host_server_id) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		string(call.ID), string(call.ConversationID), id(call.MessageID), string(call.StartedBy), int64(call.StartedAt), id(call.HostServerID)))
	return n > 0, err
}

func (c calls) SetMessage(ctx context.Context, call model.CallID, msg model.MessageID) error {
	return c.changed(c.wr.ExecContext(ctx, `UPDATE calls SET message_id = ? WHERE id = ?`, id(msg), string(call)))
}

func (c calls) Get(ctx context.Context, call model.CallID) (store.StoredCall, error) {
	out, err := scanCall(c.rd.QueryRowContext(ctx, `SELECT `+callColumns+callFrom+`WHERE c.id = ?`, string(call)))
	return out, c.mapErr(err)
}

func (c calls) Live(ctx context.Context, conv model.ConversationID) (store.StoredCall, bool, error) {
	rows, err := c.rd.QueryContext(ctx, `SELECT `+callColumns+callFrom+`WHERE c.conversation_id = ? AND c.ended_at IS NULL`, string(conv))
	list, err := collect(rows, err, scanCall)
	if err != nil || len(list) == 0 {
		return store.StoredCall{}, false, err
	}
	return list[0], true, nil
}

func (c calls) AllLive(ctx context.Context) ([]store.StoredCall, error) {
	rows, err := c.rd.QueryContext(ctx, `SELECT `+callColumns+callFrom+`WHERE c.ended_at IS NULL ORDER BY c.started_at, c.id`)
	return collect(rows, err, scanCall)
}

func (c calls) End(ctx context.Context, call model.CallID, at model.Time) (bool, error) {
	var ended bool
	err := c.atomic(ctx, func(q *queries) error {
		n, err := q.affected(q.wr.ExecContext(ctx, `UPDATE calls SET ended_at = ? WHERE id = ? AND ended_at IS NULL`, int64(at), string(call)))
		if err != nil || n == 0 {
			return err
		}
		ended = true
		_, err = q.wr.ExecContext(ctx, `UPDATE call_participants SET left_at = ? WHERE call_id = ? AND left_at IS NULL`, int64(at), string(call))
		return q.mapErr(err)
	})
	return ended, err
}

func (c calls) Join(ctx context.Context, call model.CallID, account model.AccountID, at model.Time) (bool, error) {
	var joined bool
	err := c.atomic(ctx, func(q *queries) error {
		var open int
		if err := q.wr.QueryRowContext(ctx, `SELECT count(*) FROM call_participants WHERE call_id = ? AND account_id = ? AND left_at IS NULL`,
			string(call), string(account)).Scan(&open); err != nil {
			return q.mapErr(err)
		}
		if open > 0 {
			return nil
		}
		// The key includes the time joined. Someone who left and came
		// back within the same millisecond gets the next one.
		for i := 0; i < 5 && !joined; i++ {
			n, err := q.affected(q.wr.ExecContext(ctx, `
				INSERT INTO call_participants (call_id, account_id, joined_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`,
				string(call), string(account), int64(at)+int64(i)))
			if err != nil {
				return err
			}
			joined = n > 0
		}
		return nil
	})
	return joined, err
}

func (c calls) Leave(ctx context.Context, call model.CallID, account model.AccountID, at model.Time) (bool, error) {
	n, err := c.affected(c.wr.ExecContext(ctx, `UPDATE call_participants SET left_at = ? WHERE call_id = ? AND account_id = ? AND left_at IS NULL`,
		int64(at), string(call), string(account)))
	return n > 0, err
}

func (c calls) Present(ctx context.Context, call model.CallID) ([]store.CallStay, error) {
	rows, err := c.rd.QueryContext(ctx, `
		SELECT account_id, joined_at, left_at FROM call_participants WHERE call_id = ? AND left_at IS NULL ORDER BY joined_at, account_id`,
		string(call))
	return collect(rows, err, func(s scanner) (store.CallStay, error) {
		var st store.CallStay
		err := s.Scan(str(&st.AccountID), tm(&st.JoinedAt), tm(&st.LeftAt))
		return st, err
	})
}

func (c calls) EverIn(ctx context.Context, call model.CallID, limit int) ([]model.AccountID, error) {
	rows, err := c.rd.QueryContext(ctx, `
		SELECT account_id FROM call_participants WHERE call_id = ? GROUP BY account_id ORDER BY min(joined_at), account_id LIMIT ?`,
		string(call), limit)
	return collectIDs[model.AccountID](rows, err)
}

func (c calls) SetNotice(ctx context.Context, msg model.MessageID, system map[string]any) (model.ConversationID, error) {
	var conv model.ConversationID
	err := c.wr.QueryRowContext(ctx, `UPDATE messages SET system = ? WHERE id = ? RETURNING conversation_id`, jsonArg(system), string(msg)).Scan(str(&conv))
	return conv, c.mapErr(err)
}
