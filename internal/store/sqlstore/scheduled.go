package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type scheduled struct{ *queries }

// Scheduled returns the scheduled area.
func (q *queries) Scheduled() store.Scheduled { return scheduled{q} }

const scheduledCols = `id, account_id, conversation_id, kind, content, about_message_id, next_run_at, repeat, last_run_at,
	failed_at, failure, created_at, cancelled_at`

// repeatRow is how a repeat rule is stored: the contract's fields and the
// anchor the first run fixed.
type repeatRow struct {
	model.Repeat
	Anchor int `json:"anchor,omitempty"`
}

// failureRow is how a failure is stored in the one text column: the error
// code and the sentence that went with it.
type failureRow struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// contentRow is how the content column is stored: the message to post and,
// for a watch, the person it waits for (ruling R76). The watched person is
// kept here rather than in a column of its own so that no migration is
// needed; nothing looks a row up by it, as the scheduler holds the watches
// in memory by the person watched.
type contentRow struct {
	model.MessageInput
	Watch model.AccountID `json:"watch_account_id,omitempty"`
}

func contentArg(s model.ScheduledItem) any {
	return jsonObject(contentRow{MessageInput: s.Content, Watch: s.WatchAccountID})
}

func repeatArg(r *model.Repeat) any {
	if r == nil {
		return nil
	}
	return jsonArg(repeatRow{Repeat: *r, Anchor: r.Anchor})
}

func failureArg(s model.ScheduledItem) any {
	if s.Failure == "" {
		return nil
	}
	return jsonArg(failureRow{Code: s.Failure, Message: s.FailureMessage})
}

func scanScheduled(s scanner) (model.ScheduledItem, error) {
	var it model.ScheduledItem
	var repeat, failure string
	var content contentRow
	err := s.Scan(str(&it.ID), str(&it.AccountID), str(&it.ConversationID), &it.Kind, js(&content), str(&it.AboutMessageID),
		tm(&it.NextRunAt), str(&repeat), tm(&it.LastRunAt), tm(&it.FailedAt), str(&failure), tm(&it.CreatedAt), tm(&it.CancelledAt))
	if err != nil {
		return it, err
	}
	it.Content, it.WatchAccountID = content.MessageInput, content.Watch
	if repeat != "" && repeat != "null" {
		var r repeatRow
		if err := json.Unmarshal([]byte(repeat), &r); err != nil {
			return it, err
		}
		r.Repeat.Anchor = r.Anchor
		it.Repeat = &r.Repeat
	}
	if failure != "" {
		var f failureRow
		if json.Unmarshal([]byte(failure), &f) == nil {
			it.Failure, it.FailureMessage = f.Code, f.Message
		} else {
			it.Failure = failure
		}
	}
	return it, nil
}

func (sc scheduled) Create(ctx context.Context, s *model.ScheduledItem) error {
	if s.ID == "" {
		s.ID = model.NewID[string]()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = model.Now()
	}
	_, err := sc.wr.ExecContext(ctx, `
		INSERT INTO scheduled (`+scheduledCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, string(s.AccountID), string(s.ConversationID), s.Kind, contentArg(*s), id(s.AboutMessageID),
		ms(s.NextRunAt), repeatArg(s.Repeat), ms(s.LastRunAt), ms(s.FailedAt), failureArg(*s), int64(s.CreatedAt), ms(s.CancelledAt))
	return sc.mapErr(err)
}

func (sc scheduled) Get(ctx context.Context, itemID string) (model.ScheduledItem, error) {
	ex := sc.rd
	if sc.inTx {
		ex = sc.wr
	}
	s, err := scanScheduled(ex.QueryRowContext(ctx, `SELECT `+scheduledCols+` FROM scheduled WHERE id = ?`, itemID))
	return s, sc.mapErr(err)
}

// scheduledCursor is the sort key of the last item of a page.
type scheduledCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func (sc scheduled) List(ctx context.Context, f store.ScheduledFilter, p store.Page) ([]model.ScheduledItem, string, error) {
	where := []string{"cancelled_at IS NULL"}
	var args []any
	add := func(clause, v string) {
		if v != "" {
			where = append(where, clause)
			args = append(args, v)
		}
	}
	if f.AlsoForms && f.AccountID != "" {
		where = append(where, "(account_id = ? OR kind = 'form')")
		args = append(args, string(f.AccountID))
	} else {
		add("account_id = ?", string(f.AccountID))
	}
	add("conversation_id = ?", string(f.ConversationID))
	add("kind = ?", f.Kind)
	if p.Cursor != "" {
		var c scheduledCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		where = append(where, "(created_at < ? OR (created_at = ? AND id < ?))")
		args = append(args, c.At, c.At, c.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := sc.rd.QueryContext(ctx, `
		SELECT `+scheduledCols+` FROM scheduled WHERE `+strings.Join(where, " AND ")+`
		ORDER BY created_at DESC, id DESC LIMIT ?`, args...)
	out, err := collect(rows, err, scanScheduled)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		next = store.EncodeCursor(scheduledCursor{At: int64(last.CreatedAt), ID: last.ID})
	}
	return out, next, nil
}

func (sc scheduled) Update(ctx context.Context, itemID string, fn func(s *model.ScheduledItem) error) (model.ScheduledItem, error) {
	var out model.ScheduledItem
	err := sc.atomic(ctx, func(q *queries) error {
		s, err := scanScheduled(q.wr.QueryRowContext(ctx, `SELECT `+scheduledCols+` FROM scheduled WHERE id = ?`+q.forUpdate(), itemID))
		if err != nil {
			return q.mapErr(err)
		}
		if err := fn(&s); err != nil {
			return err
		}
		_, err = q.wr.ExecContext(ctx, `
			UPDATE scheduled SET account_id = ?, content = ?, about_message_id = ?, next_run_at = ?, repeat = ?, last_run_at = ?,
				failed_at = ?, failure = ?, cancelled_at = ? WHERE id = ?`,
			string(s.AccountID), contentArg(s), id(s.AboutMessageID), ms(s.NextRunAt), repeatArg(s.Repeat), ms(s.LastRunAt),
			ms(s.FailedAt), failureArg(s), ms(s.CancelledAt), itemID)
		out = s
		return q.mapErr(err)
	})
	return out, err
}

func (sc scheduled) Delete(ctx context.Context, itemID string) error {
	return sc.changed(sc.wr.ExecContext(ctx, `DELETE FROM scheduled WHERE id = ?`, itemID))
}

func (sc scheduled) CountWaiting(ctx context.Context, account model.AccountID) (int, error) {
	ex := sc.rd
	if sc.inTx {
		ex = sc.wr
	}
	var n int
	err := ex.QueryRowContext(ctx, `
		SELECT count(*) FROM scheduled WHERE account_id = ? AND cancelled_at IS NULL AND next_run_at IS NOT NULL`,
		string(account)).Scan(&n)
	return n, err
}

func (sc scheduled) Waiting(ctx context.Context, kind string, account model.AccountID) ([]model.ScheduledItem, error) {
	ex := sc.rd
	if sc.inTx {
		ex = sc.wr
	}
	where, args := "kind = ? AND cancelled_at IS NULL AND next_run_at IS NOT NULL", []any{kind}
	if account != "" {
		where += " AND account_id = ?"
		args = append(args, string(account))
	}
	rows, err := ex.QueryContext(ctx, `SELECT `+scheduledCols+` FROM scheduled WHERE `+where+` ORDER BY created_at, id`, args...)
	return collect(rows, err, scanScheduled)
}

func (sc scheduled) Due(ctx context.Context, now model.Time, limit int) ([]model.ScheduledItem, error) {
	rows, err := sc.rd.QueryContext(ctx, `
		SELECT `+scheduledCols+` FROM scheduled
		WHERE next_run_at IS NOT NULL AND next_run_at <= ? AND cancelled_at IS NULL
		ORDER BY next_run_at, id LIMIT ?`, int64(now), limit)
	return collect(rows, err, scanScheduled)
}

func (sc scheduled) NextRun(ctx context.Context) (model.Time, bool, error) {
	var at model.Time
	err := sc.rd.QueryRowContext(ctx, `
		SELECT min(next_run_at) FROM scheduled WHERE next_run_at IS NOT NULL AND cancelled_at IS NULL`).Scan(tm(&at))
	return at, !at.IsZero(), err
}

func (sc scheduled) DeleteFinishedBefore(ctx context.Context, before model.Time) (int64, error) {
	return sc.affected(sc.wr.ExecContext(ctx, `
		DELETE FROM scheduled WHERE (cancelled_at IS NOT NULL AND cancelled_at < ?)
			OR (cancelled_at IS NULL AND next_run_at IS NULL AND failed_at IS NULL AND last_run_at IS NOT NULL AND last_run_at < ?)`,
		int64(before), int64(before)))
}

const replyCols = `id, owner_id, name, text, created_at`

func scanReply(s scanner) (model.SavedReply, error) {
	var r model.SavedReply
	err := s.Scan(str(&r.ID), str(&r.OwnerID), &r.Name, &r.Text, tm(&r.CreatedAt))
	r.Community = r.OwnerID == ""
	return r, err
}

func (sc scheduled) CreateReply(ctx context.Context, r *model.SavedReply) error {
	if r.ID == "" {
		r.ID = model.NewID[string]()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = model.Now()
	}
	r.Community = r.OwnerID == ""
	_, err := sc.wr.ExecContext(ctx, `INSERT INTO saved_replies (`+replyCols+`) VALUES (?, ?, ?, ?, ?)`,
		r.ID, id(r.OwnerID), r.Name, r.Text, int64(r.CreatedAt))
	return sc.taken(err)
}

func (sc scheduled) Reply(ctx context.Context, replyID string) (model.SavedReply, error) {
	r, err := scanReply(sc.rd.QueryRowContext(ctx, `SELECT `+replyCols+` FROM saved_replies WHERE id = ?`, replyID))
	return r, sc.mapErr(err)
}

func (sc scheduled) UpdateReply(ctx context.Context, replyID, name, text string) (model.SavedReply, error) {
	r, err := scanReply(sc.wr.QueryRowContext(ctx,
		`UPDATE saved_replies SET name = ?, text = ? WHERE id = ? RETURNING `+replyCols, name, text, replyID))
	return r, sc.taken(err)
}

func (sc scheduled) DeleteReply(ctx context.Context, replyID string) error {
	return sc.changed(sc.wr.ExecContext(ctx, `DELETE FROM saved_replies WHERE id = ?`, replyID))
}

func (sc scheduled) Replies(ctx context.Context, owner model.AccountID) ([]model.SavedReply, error) {
	rows, err := sc.rd.QueryContext(ctx, `SELECT `+replyCols+` FROM saved_replies WHERE owner_id IS NULL ORDER BY name`)
	out, err := collect(rows, err, scanReply)
	if err != nil || owner == "" {
		return out, err
	}
	rows, err = sc.rd.QueryContext(ctx, `SELECT `+replyCols+` FROM saved_replies WHERE owner_id = ? ORDER BY name`, string(owner))
	own, err := collect(rows, err, scanReply)
	if err != nil {
		return nil, err
	}
	out = append(out, own...)
	// By name; the stable sort keeps the community's first where a
	// person's own has the same name.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (sc scheduled) ReplyNamed(ctx context.Context, owner model.AccountID, name string) (model.SavedReply, bool, error) {
	where, args := "owner_id IS NULL AND name = ?", []any{name}
	if owner != "" {
		where, args = "owner_id = ? AND name = ?", []any{string(owner), name}
	}
	r, err := scanReply(sc.rd.QueryRowContext(ctx, `SELECT `+replyCols+` FROM saved_replies WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	return r, err == nil, err
}
