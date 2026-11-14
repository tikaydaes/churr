package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type threads struct{ *queries }

// Threads returns the thread states area.
func (q *queries) Threads() store.Threads { return threads{q} }

// threadUnread is the condition, on a reply r of the thread of a
// thread_states row ts, that makes it an unread counted reply
// (docs/FEATURES.md section 2.1): above the thread's position, not deleted,
// not a system notice, not written by the account itself unless it is a
// reminder, and not by someone the account has blocked.
const threadUnread = `r.parent_id = ts.parent_id AND r.seq > ts.read_seq
		AND r.deleted_at IS NULL AND r.system IS NULL AND (r.author_id <> ts.account_id OR r.via_kind = 'reminder')
		AND NOT EXISTS (SELECT 1 FROM blocks bl WHERE bl.account_id = ts.account_id AND bl.blocked_id = r.author_id)`

// threadCols lists a thread_states row with the alias ts, the conversation
// of its parent (alias p) and how many replies above its position are
// unread and counted.
const threadCols = `ts.parent_id, ts.account_id, ts.state, ts.read_seq, ts.updated_at, p.conversation_id,
	(SELECT count(*) FROM messages r WHERE ` + threadUnread + `)`

const threadFrom = ` FROM thread_states ts JOIN messages p ON p.id = ts.parent_id `

func scanThread(s scanner) (model.ThreadState, error) {
	var t model.ThreadState
	err := s.Scan(str(&t.ParentID), str(&t.AccountID), &t.State, &t.ReadSeq, tm(&t.UpdatedAt), str(&t.ConversationID), &t.UnreadCount)
	return t, err
}

func (t threads) get(ctx context.Context, ex execer, parent model.MessageID, account model.AccountID) (model.ThreadState, bool, error) {
	st, err := scanThread(ex.QueryRowContext(ctx, `SELECT `+threadCols+threadFrom+`WHERE ts.parent_id = ? AND ts.account_id = ?`,
		string(parent), string(account)))
	if errors.Is(err, sql.ErrNoRows) {
		return model.ThreadState{}, false, nil
	}
	return st, err == nil, err
}

func (t threads) Get(ctx context.Context, parent model.MessageID, account model.AccountID) (model.ThreadState, bool, error) {
	return t.get(ctx, t.rd, parent, account)
}

func (t threads) ForParents(ctx context.Context, account model.AccountID, parents []model.MessageID) ([]model.ThreadState, error) {
	if len(parents) == 0 {
		return nil, nil
	}
	args := append([]any{string(account)}, anys(parents)...)
	rows, err := t.rd.QueryContext(ctx, `SELECT `+threadCols+threadFrom+`WHERE ts.account_id = ? AND ts.parent_id IN (`+marks(len(parents))+`)
		ORDER BY ts.parent_id`, args...)
	return collect(rows, err, scanThread)
}

type threadCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func (t threads) ForAccount(ctx context.Context, account model.AccountID, state string, p store.Page) ([]model.ThreadState, string, error) {
	where := "ts.account_id = ?"
	args := []any{string(account)}
	if state != "" {
		where += " AND ts.state = ?"
		args = append(args, state)
	}
	if p.Cursor != "" {
		var c threadCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		where += " AND (ts.updated_at < ? OR (ts.updated_at = ? AND ts.parent_id < ?))"
		args = append(args, c.At, c.At, c.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := t.rd.QueryContext(ctx, `SELECT `+threadCols+threadFrom+`WHERE `+where+` ORDER BY ts.updated_at DESC, ts.parent_id DESC LIMIT ?`, args...)
	out, err := collect(rows, err, scanThread)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		next = store.EncodeCursor(threadCursor{At: int64(last.UpdatedAt), ID: string(last.ParentID)})
	}
	return out, next, nil
}

func (t threads) Set(ctx context.Context, parent model.MessageID, account model.AccountID, state string, readSeq int64, at model.Time) (model.ThreadState, error) {
	var out model.ThreadState
	err := t.atomic(ctx, func(q *queries) error {
		_, err := q.wr.ExecContext(ctx, `
			INSERT INTO thread_states (parent_id, account_id, state, read_seq, updated_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (parent_id, account_id) DO UPDATE SET state = excluded.state, updated_at = excluded.updated_at`,
			string(parent), string(account), state, readSeq, int64(at))
		if err != nil {
			if q.db.dialect.ForeignKeyViolation(err) {
				return model.ErrNotFound
			}
			return q.mapErr(err)
		}
		out, _, err = threads{q}.get(ctx, q.wr, parent, account)
		return err
	})
	return out, err
}

func (t threads) Follow(ctx context.Context, parent model.MessageID, account model.AccountID, readSeq, raiseTo int64, evenIfIgnored bool, at model.Time) (model.ThreadState, bool, error) {
	var out model.ThreadState
	changed := false
	err := t.atomic(ctx, func(q *queries) error {
		cur, found, err := threads{q}.get(ctx, q.wr, parent, account)
		if err != nil {
			return err
		}
		switch {
		case !found:
			if raiseTo > readSeq {
				readSeq = raiseTo
			}
			_, err = q.wr.ExecContext(ctx, `
				INSERT INTO thread_states (parent_id, account_id, state, read_seq, updated_at) VALUES (?, ?, 'following', ?, ?)`,
				string(parent), string(account), readSeq, int64(at))
			changed = true
		case cur.State == model.ThreadIgnored && !evenIfIgnored:
			out = cur
			return nil
		case cur.State != model.ThreadFollowing || raiseTo > cur.ReadSeq:
			if raiseTo < cur.ReadSeq {
				raiseTo = cur.ReadSeq
			}
			_, err = q.wr.ExecContext(ctx, `
				UPDATE thread_states SET state = 'following', read_seq = ?, updated_at = ? WHERE parent_id = ? AND account_id = ?`,
				raiseTo, int64(at), string(parent), string(account))
			changed = true
		default:
			out = cur
			return nil
		}
		if err != nil {
			return q.mapErr(err)
		}
		out, _, err = threads{q}.get(ctx, q.wr, parent, account)
		return err
	})
	return out, changed, err
}

func (t threads) SetRead(ctx context.Context, parent model.MessageID, account model.AccountID, seq int64, at model.Time) (model.ThreadState, bool, error) {
	var out model.ThreadState
	found := false
	err := t.atomic(ctx, func(q *queries) error {
		n, err := q.affected(q.wr.ExecContext(ctx, `
			UPDATE thread_states SET read_seq = ?, updated_at = ? WHERE parent_id = ? AND account_id = ?`,
			seq, int64(at), string(parent), string(account)))
		if err != nil || n == 0 {
			return err
		}
		out, found, err = threads{q}.get(ctx, q.wr, parent, account)
		return err
	})
	return out, found, err
}

func (t threads) Delete(ctx context.Context, parent model.MessageID, account model.AccountID) error {
	_, err := t.wr.ExecContext(ctx, `DELETE FROM thread_states WHERE parent_id = ? AND account_id = ?`, string(parent), string(account))
	return err
}

type unreadThreadCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

// threadActivity orders the Threads view: by the latest reply, or for a
// thread that has lost its replies by when the state was last touched.
const threadActivity = `COALESCE(p.last_reply_at, ts.updated_at)`

func (t threads) Unread(ctx context.Context, account model.AccountID, all bool, p store.Page) ([]model.ThreadState, string, error) {
	where := "ts.account_id = ? AND ts.state = 'following'"
	args := []any{string(account)}
	if !all {
		where += " AND EXISTS (SELECT 1 FROM messages r WHERE " + threadUnread + ")"
	}
	if p.Cursor != "" {
		var c unreadThreadCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		where += " AND (" + threadActivity + " < ? OR (" + threadActivity + " = ? AND ts.parent_id < ?))"
		args = append(args, c.At, c.At, c.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := t.rd.QueryContext(ctx, `SELECT `+threadCols+`, `+threadActivity+threadFrom+`WHERE `+where+
		` ORDER BY `+threadActivity+` DESC, ts.parent_id DESC LIMIT ?`, args...)
	type row struct {
		st model.ThreadState
		at int64
	}
	list, err := collect(rows, err, func(s scanner) (row, error) {
		var r row
		t := &r.st
		err := s.Scan(str(&t.ParentID), str(&t.AccountID), &t.State, &t.ReadSeq, tm(&t.UpdatedAt), str(&t.ConversationID), &t.UnreadCount, &r.at)
		return r, err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(list) > limit {
		list = list[:limit]
		last := list[limit-1]
		next = store.EncodeCursor(unreadThreadCursor{At: last.at, ID: string(last.st.ParentID)})
	}
	out := make([]model.ThreadState, len(list))
	for i, r := range list {
		out[i] = r.st
	}
	return out, next, nil
}

func (t threads) CountUnread(ctx context.Context, account model.AccountID) (int64, error) {
	var n int64
	err := t.rd.QueryRowContext(ctx, `
		SELECT count(*) FROM thread_states ts
		WHERE ts.account_id = ? AND ts.state = 'following' AND EXISTS (SELECT 1 FROM messages r WHERE `+threadUnread+`)`,
		string(account)).Scan(&n)
	return n, err
}
