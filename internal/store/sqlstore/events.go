package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type events struct{ *queries }

// Events returns the events area.
func (q *queries) Events() store.Events { return events{q} }

func (e events) Append(ctx context.Context, ev model.Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = e.wr.ExecContext(ctx, `
		INSERT INTO events (conversation_id, seq, type, message_id, payload, at) VALUES (?, ?, ?, ?, ?, ?)`,
		string(ev.ConversationID), ev.Seq, ev.Type, id(ev.MessageID), string(payload), int64(ev.At))
	return e.mapErr(err)
}

func (e events) List(ctx context.Context, conv model.ConversationID, after, until int64, limit int) ([]model.Event, bool, error) {
	limit = store.Page{Limit: limit}.Clamp()
	where := "conversation_id = ? AND seq > ?"
	args := []any{string(conv), after}
	if until > 0 {
		where += " AND seq <= ?"
		args = append(args, until)
	}
	args = append(args, limit+1)
	ex := e.rd
	if e.inTx {
		ex = e.wr
	}
	rows, err := ex.QueryContext(ctx, `SELECT payload, message_id FROM events WHERE `+where+` ORDER BY seq LIMIT ?`, args...)
	out, err := collect(rows, err, func(s scanner) (model.Event, error) {
		var raw json.RawMessage
		var ev model.Event
		var msg model.MessageID
		if err := s.Scan(js(&raw), str(&msg)); err != nil {
			return ev, err
		}
		err := json.Unmarshal(raw, &ev)
		ev.MessageID = msg
		return ev, err
	})
	if err != nil {
		return nil, false, err
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, nil
}

func (e events) Oldest(ctx context.Context, conv model.ConversationID) (int64, error) {
	var seq sql.NullInt64
	err := e.rd.QueryRowContext(ctx, `SELECT min(seq) FROM events WHERE conversation_id = ?`, string(conv)).Scan(&seq)
	return seq.Int64, err
}

func (e events) DeleteForMessage(ctx context.Context, msg model.MessageID) error {
	_, err := e.wr.ExecContext(ctx, `DELETE FROM events WHERE message_id = ?`, string(msg))
	return err
}

func (e events) DeleteBefore(ctx context.Context, before model.Time) (int64, error) {
	return e.affected(e.wr.ExecContext(ctx, `DELETE FROM events WHERE at < ?`, int64(before)))
}
