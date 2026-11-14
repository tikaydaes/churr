package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type invites struct{ *queries }

// Invites returns the invites area.
func (q *queries) Invites() store.Invites { return invites{q} }

const inviteCols = `id, code, role, group_ids, conversation_ids, claims_account_id, max_uses, uses, expires_at,
	created_by, created_at, revoked_at`

func scanInvite(s scanner) (model.Invite, error) {
	var i model.Invite
	err := s.Scan(str(&i.ID), &i.Code, str(&i.Role), js(&i.GroupIDs), js(&i.ConversationIDs), str(&i.ClaimsAccountID),
		&i.MaxUses, &i.Uses, tm(&i.ExpiresAt), str(&i.CreatedByID), tm(&i.CreatedAt), tm(&i.RevokedAt))
	// The API sends these two as lists, never as null.
	if i.GroupIDs == nil {
		i.GroupIDs = []model.GroupID{}
	}
	if i.ConversationIDs == nil {
		i.ConversationIDs = []model.ConversationID{}
	}
	return i, err
}

func (i invites) Create(ctx context.Context, inv *model.Invite) error {
	if inv.ID == "" {
		inv.ID = model.NewID[model.InviteID]()
	}
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = model.Now()
	}
	if inv.Role == "" {
		inv.Role = model.RoleMember
	}
	// A code that exists is answered without a failed statement, so the
	// caller can make another code and try again in the same transaction.
	n, err := i.affected(i.wr.ExecContext(ctx, `
		INSERT INTO invites (id, code, role, group_ids, conversation_ids, claims_account_id, max_uses, uses, expires_at,
			created_by, created_at, revoked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (code) DO NOTHING`,
		string(inv.ID), inv.Code, string(inv.Role), jsonList(inv.GroupIDs), jsonList(inv.ConversationIDs),
		id(inv.ClaimsAccountID), inv.MaxUses, inv.Uses, ms(inv.ExpiresAt), id(inv.CreatedByID), int64(inv.CreatedAt),
		ms(inv.RevokedAt)))
	if err != nil {
		return err
	}
	if n == 0 {
		return model.ErrAlreadyExists
	}
	return nil
}

func (i invites) Get(ctx context.Context, invite model.InviteID) (model.Invite, error) {
	out, err := scanInvite(i.rd.QueryRowContext(ctx, `SELECT `+inviteCols+` FROM invites WHERE id = ?`, string(invite)))
	return out, i.mapErr(err)
}

func (i invites) ByCode(ctx context.Context, code string) (model.Invite, error) {
	// Through the write handle: registration looks the invite up inside
	// the transaction that goes on to count the use.
	out, err := scanInvite(i.wr.QueryRowContext(ctx, `SELECT `+inviteCols+` FROM invites WHERE code = ?`, code))
	return out, i.mapErr(err)
}

// inviteCursor is the sort key of the last row of a page: newest first, with
// the id to break ties between invites made in the same millisecond.
type inviteCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func (i invites) List(ctx context.Context, createdBy model.AccountID, p store.Page) ([]model.Invite, string, error) {
	where := "1 = 1"
	var args []any
	if createdBy != "" {
		where += " AND created_by = ?"
		args = append(args, string(createdBy))
	}
	if p.Cursor != "" {
		var c inviteCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		where += " AND (created_at < ? OR (created_at = ? AND id < ?))"
		args = append(args, c.At, c.At, c.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := i.rd.QueryContext(ctx,
		`SELECT `+inviteCols+` FROM invites WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT ?`, args...)
	out, err := collect(rows, err, scanInvite)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		next = store.EncodeCursor(inviteCursor{At: int64(last.CreatedAt), ID: string(last.ID)})
	}
	return out, next, nil
}

func (i invites) Use(ctx context.Context, invite model.InviteID, now model.Time) error {
	// One conditional update: of two registrations racing for the last
	// use, the database lets exactly one find the invite still usable.
	return i.changed(i.wr.ExecContext(ctx, `
		UPDATE invites SET uses = uses + 1
		WHERE id = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?) AND (max_uses = 0 OR uses < max_uses)`,
		string(invite), int64(now)))
}

func (i invites) Revoke(ctx context.Context, invite model.InviteID, at model.Time) error {
	// An invite withdrawn before keeps the time it was first withdrawn.
	return i.changed(i.wr.ExecContext(ctx,
		`UPDATE invites SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, int64(at), string(invite)))
}

func (i invites) DeleteEnded(ctx context.Context, before model.Time) (int64, error) {
	// No column says when an invite was used up, so a used-up invite
	// goes by its expiry like any other. One with no expiry stays until
	// it is revoked: deleting it by guesswork could take a row away
	// sooner than the 30 days docs/SCHEMA.md section 14.4 promises.
	return i.affected(i.wr.ExecContext(ctx, `
		DELETE FROM invites
		WHERE (revoked_at IS NOT NULL AND revoked_at < ?) OR (expires_at IS NOT NULL AND expires_at < ?)`,
		int64(before), int64(before)))
}
