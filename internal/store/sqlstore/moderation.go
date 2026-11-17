package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type moderation struct{ *queries }

// Moderation returns the moderation area.
func (q *queries) Moderation() store.Moderation { return moderation{q} }

// ---------------------------------------------------------------- time-outs

const timeoutCols = `id, account_id, conversation_id, until, reason, created_by, created_at, lifted_at`

func scanTimeout(s scanner) (model.Timeout, error) {
	var t model.Timeout
	err := s.Scan(str(&t.ID), str(&t.AccountID), str(&t.ConversationID), tm(&t.Until), &t.Reason, str(&t.CreatedByID),
		tm(&t.CreatedAt), tm(&t.LiftedAt))
	return t, err
}

func (m moderation) CreateTimeout(ctx context.Context, t *model.Timeout) error {
	if t.ID == "" {
		t.ID = model.NewID[string]()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = model.Now()
	}
	_, err := m.wr.ExecContext(ctx, `
		INSERT INTO timeouts (`+timeoutCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, string(t.AccountID), id(t.ConversationID), int64(t.Until), t.Reason, id(t.CreatedByID), int64(t.CreatedAt),
		ms(t.LiftedAt))
	return m.mapErr(err)
}

func (m moderation) ReplaceTimeout(ctx context.Context, t *model.Timeout) ([]model.Timeout, error) {
	var lifted []model.Timeout
	err := m.atomic(ctx, func(q *queries) error {
		if t.CreatedAt.IsZero() {
			t.CreatedAt = model.Now()
		}
		// The same account and the same scope: this conversation, or the
		// whole server. A server-wide time-out does not replace one set in
		// a channel, nor the other way round.
		where := `account_id = ? AND lifted_at IS NULL AND until > ? AND conversation_id IS NULL`
		args := []any{int64(t.CreatedAt), string(t.AccountID), int64(t.CreatedAt)}
		if t.ConversationID != "" {
			where = `account_id = ? AND lifted_at IS NULL AND until > ? AND conversation_id = ?`
			args = append(args, string(t.ConversationID))
		}
		rows, err := q.wr.QueryContext(ctx, `UPDATE timeouts SET lifted_at = ? WHERE `+where+` RETURNING `+timeoutCols, args...)
		if lifted, err = collect(rows, err, scanTimeout); err != nil {
			return q.mapErr(err)
		}
		return moderation{q}.CreateTimeout(ctx, t)
	})
	return lifted, err
}

func (m moderation) GetTimeout(ctx context.Context, timeout string) (model.Timeout, error) {
	t, err := scanTimeout(m.rd.QueryRowContext(ctx, `SELECT `+timeoutCols+` FROM timeouts WHERE id = ?`, timeout))
	return t, m.mapErr(err)
}

func (m moderation) LiftTimeout(ctx context.Context, timeout string, at model.Time) error {
	return m.atomic(ctx, func(q *queries) error {
		n, err := q.affected(q.wr.ExecContext(ctx,
			`UPDATE timeouts SET lifted_at = ? WHERE id = ? AND lifted_at IS NULL`, int64(at), timeout))
		if err != nil || n > 0 {
			return err
		}
		// Nothing changed: either it was lifted before, which keeps the
		// time it was first lifted, or there is no such time-out.
		var one int
		err = q.wr.QueryRowContext(ctx, `SELECT 1 FROM timeouts WHERE id = ?`, timeout).Scan(&one)
		return q.mapErr(err)
	})
}

func (m moderation) TimedOutUntil(ctx context.Context, account model.AccountID, conv model.ConversationID, now model.Time) (model.Time, bool, error) {
	// A time-out is in force while its end is ahead and nobody lifted it.
	// A server-wide one (no conversation) counts everywhere.
	where := `account_id = ? AND lifted_at IS NULL AND until > ?`
	args := []any{string(account), int64(now)}
	if conv == "" {
		where += ` AND conversation_id IS NULL`
	} else {
		where += ` AND (conversation_id IS NULL OR conversation_id = ?)`
		args = append(args, string(conv))
	}
	var until model.Time
	err := m.rd.QueryRowContext(ctx, `SELECT MAX(until) FROM timeouts WHERE `+where, args...).Scan(tm(&until))
	if err != nil {
		return 0, false, err
	}
	return until, !until.IsZero(), nil
}

// newestCursor is the sort key of the lists here that are newest first.
type newestCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

// newestPage adds the cursor condition and the limit of a newest-first list
// over the columns at and idCol.
func newestPage(where []string, args []any, at, idCol string, p store.Page) ([]string, []any, int, error) {
	if p.Cursor != "" {
		var c newestCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, nil, 0, err
		}
		where = append(where, "("+at+" < ? OR ("+at+" = ? AND "+idCol+" < ?))")
		args = append(args, c.At, c.At, c.ID)
	}
	limit := p.Clamp()
	return where, append(args, limit+1), limit, nil
}

func (m moderation) Timeouts(ctx context.Context, f store.TimeoutFilter, now model.Time, p store.Page) ([]model.Timeout, string, error) {
	where := []string{"t.lifted_at IS NULL", "t.until > ?"}
	args := []any{int64(now)}
	switch {
	case f.AdminView:
		where = append(where, `(t.conversation_id IS NULL OR EXISTS (
			SELECT 1 FROM conversations c WHERE c.id = t.conversation_id AND c.kind = 'channel' AND c.private = ?))`)
		args = append(args, false)
	case f.ConversationID != "":
		where = append(where, "t.conversation_id = ?")
		args = append(args, string(f.ConversationID))
	}
	where, args, limit, err := newestPage(where, args, "t.created_at", "t.id", p)
	if err != nil {
		return nil, "", err
	}
	rows, err := m.rd.QueryContext(ctx, `SELECT t.`+strings.ReplaceAll(timeoutCols, ", ", ", t.")+` FROM timeouts t
		WHERE `+strings.Join(where, " AND ")+` ORDER BY t.created_at DESC, t.id DESC LIMIT ?`, args...)
	out, err := collect(rows, err, scanTimeout)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = store.EncodeCursor(newestCursor{At: int64(out[limit-1].CreatedAt), ID: out[limit-1].ID})
	}
	return out, next, nil
}

// ------------------------------------------------------------------ reports

const reportCols = `r.id, r.message_id, r.conversation_id, r.reported_by, r.source, r.reason, r.created_at, r.handled_by, r.handled_at, r.outcome`

func scanReport(s scanner) (model.Report, error) {
	var r model.Report
	err := s.Scan(str(&r.ID), str(&r.MessageID), str(&r.ConversationID), str(&r.ReportedByID), &r.Source, &r.Reason,
		tm(&r.CreatedAt), str(&r.HandledByID), tm(&r.HandledAt), str(&r.Outcome))
	return r, err
}

func (m moderation) CreateReport(ctx context.Context, r *model.Report) (bool, error) {
	if r.ID == "" {
		r.ID = model.NewID[string]()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = model.Now()
	}
	if r.Source == "" {
		r.Source = model.ReportByPerson
	}
	if (r.Source == model.ReportByFilter) != (r.ReportedByID == "") {
		return false, errors.New("sqlstore: a report has a reporter if and only if a person made it")
	}
	created := false
	err := m.atomic(ctx, func(q *queries) error {
		// At most one waiting row per message and reporter, and one from
		// the filter.
		where := `r.message_id = ? AND r.outcome IS NULL AND r.source = ? AND r.reported_by IS NULL`
		args := []any{string(r.MessageID), r.Source}
		if r.ReportedByID != "" {
			where = `r.message_id = ? AND r.outcome IS NULL AND r.source = ? AND r.reported_by = ?`
			args = append(args, string(r.ReportedByID))
		}
		old, err := scanReport(q.wr.QueryRowContext(ctx, `SELECT `+reportCols+` FROM reports r WHERE `+where, args...))
		if err == nil {
			*r = old
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = q.wr.ExecContext(ctx, `
			INSERT INTO reports (id, message_id, conversation_id, reported_by, source, reason, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			r.ID, string(r.MessageID), string(r.ConversationID), id(r.ReportedByID), r.Source, r.Reason, int64(r.CreatedAt))
		created = err == nil
		return q.mapErr(err)
	})
	return created, err
}

func (m moderation) GetReport(ctx context.Context, report string) (model.Report, error) {
	r, err := scanReport(m.rd.QueryRowContext(ctx, `SELECT `+reportCols+` FROM reports r WHERE r.id = ?`, report))
	return r, m.mapErr(err)
}

func (m moderation) Reports(ctx context.Context, f store.ReportFilter, p store.Page) ([]model.Report, string, error) {
	if f.Scope.AccountID == "" {
		return nil, "", nil
	}
	// The channel's moderators and managers; an admin also handles every
	// public channel's (docs/PERMISSIONS.md 8.4).
	const manager = `EXISTS (SELECT 1 FROM members m WHERE m.conversation_id = r.conversation_id AND m.account_id = ? AND m.level IN ('moderator', 'manager'))`
	where := []string{manager}
	args := []any{string(f.Scope.AccountID)}
	if f.Scope.PublicChannels {
		where = []string{"(" + manager + ` OR EXISTS (
			SELECT 1 FROM conversations c WHERE c.id = r.conversation_id AND c.kind = 'channel' AND c.private = ?))`}
		args = append(args, false)
	}
	if f.ConversationID != "" {
		where = append(where, "r.conversation_id = ?")
		args = append(args, string(f.ConversationID))
	}
	if f.Waiting {
		where = append(where, "r.outcome IS NULL")
	}
	where, args, limit, err := newestPage(where, args, "r.created_at", "r.id", p)
	if err != nil {
		return nil, "", err
	}
	rows, err := m.rd.QueryContext(ctx, `SELECT `+reportCols+` FROM reports r
		WHERE `+strings.Join(where, " AND ")+` ORDER BY r.created_at DESC, r.id DESC LIMIT ?`, args...)
	out, err := collect(rows, err, scanReport)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = store.EncodeCursor(newestCursor{At: int64(out[limit-1].CreatedAt), ID: out[limit-1].ID})
	}
	return out, next, nil
}

func (m moderation) SettleReports(ctx context.Context, msg model.MessageID, outcome string, by model.AccountID, at model.Time) ([]model.Report, error) {
	rows, err := m.wr.QueryContext(ctx, `
		UPDATE reports SET outcome = ?, handled_by = ?, handled_at = ? WHERE message_id = ? AND outcome IS NULL
		RETURNING id, message_id, conversation_id, reported_by, source, reason, created_at, handled_by, handled_at, outcome`,
		outcome, id(by), int64(at), string(msg))
	out, err := collect(rows, err, scanReport)
	return out, m.mapErr(err)
}

func (m moderation) WaitingReports(ctx context.Context) (int64, error) {
	var n int64
	err := m.rd.QueryRowContext(ctx, `
		SELECT count(*) FROM reports r JOIN conversations c ON c.id = r.conversation_id
		WHERE r.outcome IS NULL AND c.kind = 'channel' AND c.private = ?`, false).Scan(&n)
	return n, err
}

// ----------------------------------------------------------- purge requests

const purgeCols = `id, message_id, requested_by, requested_at, decided_by, decided_at, outcome`

func scanPurge(s scanner) (model.PurgeRequest, error) {
	var p model.PurgeRequest
	err := s.Scan(str(&p.ID), str(&p.MessageID), str(&p.RequestedByID), tm(&p.RequestedAt), str(&p.DecidedByID),
		tm(&p.DecidedAt), str(&p.Outcome))
	return p, err
}

func (m moderation) CreatePurgeRequest(ctx context.Context, p *model.PurgeRequest) error {
	if p.ID == "" {
		p.ID = model.NewID[string]()
	}
	if p.RequestedAt.IsZero() {
		p.RequestedAt = model.Now()
	}
	_, err := m.wr.ExecContext(ctx, `INSERT INTO purge_requests (id, message_id, requested_by, requested_at) VALUES (?, ?, ?, ?)`,
		p.ID, string(p.MessageID), string(p.RequestedByID), int64(p.RequestedAt))
	return m.mapErr(err)
}

func (m moderation) GetPurgeRequest(ctx context.Context, request string) (model.PurgeRequest, error) {
	p, err := scanPurge(m.rd.QueryRowContext(ctx, `SELECT `+purgeCols+` FROM purge_requests WHERE id = ?`, request))
	return p, m.mapErr(err)
}

func (m moderation) LatestPurgeRequest(ctx context.Context, msg model.MessageID) (model.PurgeRequest, bool, error) {
	// A waiting one first: there is at most one, and it is what a second
	// ask returns.
	p, err := scanPurge(m.rd.QueryRowContext(ctx, `SELECT `+purgeCols+` FROM purge_requests WHERE message_id = ?
		ORDER BY CASE WHEN outcome IS NULL THEN 0 ELSE 1 END, requested_at DESC, id DESC LIMIT 1`, string(msg)))
	if errors.Is(err, sql.ErrNoRows) {
		return p, false, nil
	}
	return p, err == nil, err
}

func (m moderation) PurgeRequests(ctx context.Context, waiting bool, p store.Page) ([]model.PurgeRequest, string, error) {
	where := []string{"1 = 1"}
	var args []any
	if waiting {
		where = append(where, "outcome IS NULL")
	}
	where, args, limit, err := newestPage(where, args, "requested_at", "id", p)
	if err != nil {
		return nil, "", err
	}
	rows, err := m.rd.QueryContext(ctx, `SELECT `+purgeCols+` FROM purge_requests
		WHERE `+strings.Join(where, " AND ")+` ORDER BY requested_at DESC, id DESC LIMIT ?`, args...)
	out, err := collect(rows, err, scanPurge)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = store.EncodeCursor(newestCursor{At: int64(out[limit-1].RequestedAt), ID: out[limit-1].ID})
	}
	return out, next, nil
}

func (m moderation) WaitingPurgeRequestsIn(ctx context.Context, conv model.ConversationID) ([]model.PurgeRequest, error) {
	rows, err := m.rd.QueryContext(ctx, `
		SELECT p.id, p.message_id, p.requested_by, p.requested_at, p.decided_by, p.decided_at, p.outcome
		FROM purge_requests p JOIN messages m ON m.id = p.message_id
		WHERE m.conversation_id = ? AND p.outcome IS NULL ORDER BY p.requested_at, p.id`, string(conv))
	return collect(rows, err, scanPurge)
}

func (m moderation) DecidePurgeRequest(ctx context.Context, request, outcome string, by model.AccountID, at model.Time) (model.PurgeRequest, error) {
	var out model.PurgeRequest
	err := m.atomic(ctx, func(q *queries) error {
		p, err := scanPurge(q.wr.QueryRowContext(ctx, `
			UPDATE purge_requests SET outcome = ?, decided_by = ?, decided_at = ? WHERE id = ? AND outcome IS NULL
			RETURNING `+purgeCols, outcome, id(by), int64(at), request))
		if err == nil {
			out = p
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return q.mapErr(err)
		}
		var one int
		if err := q.wr.QueryRowContext(ctx, `SELECT 1 FROM purge_requests WHERE id = ?`, request).Scan(&one); err != nil {
			return q.mapErr(err)
		}
		return model.ErrConflict.With("That request has already been decided.")
	})
	return out, err
}

func (m moderation) WaitingPurgeRequests(ctx context.Context) (int64, error) {
	var n int64
	err := m.rd.QueryRowContext(ctx, `SELECT count(*) FROM purge_requests WHERE outcome IS NULL`).Scan(&n)
	return n, err
}

// ------------------------------------------------------------- word filters

const filterCols = `id, pattern, is_regex, action, created_by, created_at`

func scanFilter(s scanner) (model.WordFilter, error) {
	var f model.WordFilter
	err := s.Scan(str(&f.ID), &f.Pattern, flag(&f.IsRegex), &f.Action, str(&f.CreatedByID), tm(&f.CreatedAt))
	return f, err
}

func (m moderation) WordFilters(ctx context.Context) ([]model.WordFilter, error) {
	return m.allFilters(ctx)
}

func (m moderation) GetWordFilter(ctx context.Context, filter string) (model.WordFilter, error) {
	f, err := scanFilter(m.rd.QueryRowContext(ctx, `SELECT `+filterCols+` FROM word_filters WHERE id = ?`, filter))
	return f, m.mapErr(err)
}

func (m moderation) CreateWordFilter(ctx context.Context, f *model.WordFilter) error {
	if f.ID == "" {
		f.ID = model.NewID[string]()
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = model.Now()
	}
	_, err := m.wr.ExecContext(ctx, `INSERT INTO word_filters (`+filterCols+`) VALUES (?, ?, ?, ?, ?, ?)`,
		f.ID, f.Pattern, f.IsRegex, f.Action, string(f.CreatedByID), int64(f.CreatedAt))
	m.touchFilters()
	return m.mapErr(err)
}

func (m moderation) UpdateWordFilter(ctx context.Context, f model.WordFilter) error {
	// Marked after the write: a reader in between would keep the old list.
	defer m.touchFilters()
	return m.changed(m.wr.ExecContext(ctx, `UPDATE word_filters SET pattern = ?, is_regex = ?, action = ? WHERE id = ?`,
		f.Pattern, f.IsRegex, f.Action, f.ID))
}

func (m moderation) DeleteWordFilter(ctx context.Context, filter string) error {
	defer m.touchFilters()
	return m.changed(m.wr.ExecContext(ctx, `DELETE FROM word_filters WHERE id = ?`, filter))
}
