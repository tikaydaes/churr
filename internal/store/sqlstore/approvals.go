package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type approvals struct{ *queries }

// Approvals returns the approvals area.
func (q *queries) Approvals() store.Approvals { return approvals{q} }

const approvalCols = `id, kind, requester_id, note, payload, state, reason, decided_by, decided_at, created_at, expires_at`

func scanApproval(s scanner) (model.Approval, error) {
	var a model.Approval
	var payload string
	err := s.Scan(&a.ID, &a.Kind, str(&a.RequesterID), &a.Note, &payload, &a.State, &a.Reason, str(&a.DecidedBy), tm(&a.DecidedAt),
		tm(&a.CreatedAt), tm(&a.ExpiresAt))
	a.Payload = []byte(payload)
	return a, err
}

func (q approvals) Create(ctx context.Context, a *model.Approval) error {
	if a.ID == "" {
		a.ID = model.NewID[string]()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = model.Now()
	}
	if a.ExpiresAt.IsZero() {
		a.ExpiresAt = a.CreatedAt.Add(model.ApprovalLife)
	}
	if len(a.Payload) == 0 {
		a.Payload = []byte("{}")
	}
	a.State = model.ApprovalWaiting
	_, err := q.wr.ExecContext(ctx, `INSERT INTO approvals (id, kind, requester_id, note, payload, state, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, 'waiting', ?, ?)`,
		a.ID, a.Kind, string(a.RequesterID), a.Note, string(a.Payload), int64(a.CreatedAt), int64(a.ExpiresAt))
	return q.mapErr(err)
}

func (q approvals) Get(ctx context.Context, id string) (model.Approval, error) {
	if !model.ValidID(id) {
		return model.Approval{}, model.ErrNotFound
	}
	a, err := scanApproval(q.rd.QueryRowContext(ctx, `SELECT `+approvalCols+` FROM approvals WHERE id = ?`, id))
	return a, q.mapErr(err)
}

func kindArgs(kinds []string) []any {
	out := make([]any, len(kinds))
	for i, k := range kinds {
		out[i] = k
	}
	return out
}

func (q approvals) Waiting(ctx context.Context, kinds []string, now model.Time, limit int) ([]model.Approval, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > model.MaxLimit {
		limit = model.MaxLimit
	}
	args := append(kindArgs(kinds), int64(now), limit)
	rows, err := q.rd.QueryContext(ctx, `SELECT `+approvalCols+` FROM approvals
		WHERE kind IN (`+marks(len(kinds))+`) AND state = 'waiting' AND expires_at > ?
		ORDER BY created_at, id LIMIT ?`, args...)
	out, err := collect(rows, err, scanApproval)
	return out, q.mapErr(err)
}

func (q approvals) CountWaiting(ctx context.Context, kinds []string, now model.Time) (map[string]int, error) {
	out := map[string]int{}
	if len(kinds) == 0 {
		return out, nil
	}
	rows, err := q.rd.QueryContext(ctx, `SELECT kind, COUNT(*) FROM approvals
		WHERE kind IN (`+marks(len(kinds))+`) AND state = 'waiting' AND expires_at > ? GROUP BY kind`,
		append(kindArgs(kinds), int64(now))...)
	if err != nil {
		return nil, q.mapErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, q.mapErr(rows.Err())
}

func (q approvals) Latest(ctx context.Context, requester model.AccountID, kind string) (model.Approval, bool, error) {
	a, err := scanApproval(q.rd.QueryRowContext(ctx, `SELECT `+approvalCols+` FROM approvals
		WHERE requester_id = ? AND kind = ? ORDER BY created_at DESC, id DESC LIMIT 1`, string(requester), kind))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Approval{}, false, nil
	}
	return a, err == nil, q.mapErr(err)
}

func (q approvals) Decide(ctx context.Context, approval, state string, by model.AccountID, reason string, at model.Time) (model.Approval, bool, error) {
	if state != model.ApprovalApproved && state != model.ApprovalDeclined {
		return model.Approval{}, false, model.Invalid("state", "An approval is approved or declined.")
	}
	if !model.ValidID(approval) {
		return model.Approval{}, false, nil
	}
	var out model.Approval
	found := false
	err := q.atomic(ctx, func(q *queries) error {
		res, err := q.wr.ExecContext(ctx, `UPDATE approvals SET state = ?, decided_by = ?, decided_at = ?, reason = ?
			WHERE id = ? AND state = 'waiting' AND expires_at > ?`, state, id(by), int64(at), reason, approval, int64(at))
		if err != nil {
			return q.mapErr(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		found = true
		out, err = scanApproval(q.wr.QueryRowContext(ctx, `SELECT `+approvalCols+` FROM approvals WHERE id = ?`, approval))
		return q.mapErr(err)
	})
	return out, found, err
}

func (q approvals) SetApprover(ctx context.Context, account model.AccountID, may bool, by model.AccountID, at model.Time) error {
	if !may {
		_, err := q.wr.ExecContext(ctx, `DELETE FROM approvers WHERE account_id = ?`, string(account))
		return q.mapErr(err)
	}
	_, err := q.wr.ExecContext(ctx, `INSERT INTO approvers (account_id, granted_by, granted_at) VALUES (?, ?, ?)
		ON CONFLICT (account_id) DO NOTHING`, string(account), id(by), int64(at))
	return q.mapErr(err)
}

func (q approvals) IsApprover(ctx context.Context, account model.AccountID) (bool, error) {
	var n int
	err := q.rd.QueryRowContext(ctx, `SELECT COUNT(*) FROM approvers WHERE account_id = ?`, string(account)).Scan(&n)
	return n > 0, q.mapErr(err)
}

func (q approvals) Approvers(ctx context.Context) ([]model.AccountID, error) {
	return collectIDs[model.AccountID](q.rd.QueryContext(ctx, `SELECT p.account_id FROM approvers p JOIN accounts a ON a.id = p.account_id
		WHERE a.removed_at IS NULL ORDER BY p.granted_at, p.account_id`))
}
