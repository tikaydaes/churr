package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// The methods of store.Bans, which is part of the accounts area.

const banCols = `id, account_id, provider_id, subject, reason, created_by, created_at, lifted_at`

func scanBan(s scanner) (model.Ban, error) {
	var b model.Ban
	err := s.Scan(&b.ID, str(&b.AccountID), str(&b.ProviderID), str(&b.Subject), &b.Reason, str(&b.CreatedByID),
		tm(&b.CreatedAt), tm(&b.LiftedAt))
	return b, err
}

func (q *queries) insertBan(ctx context.Context, b *model.Ban) error {
	if b.ID == "" {
		b.ID = model.NewID[string]()
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = model.Now()
	}
	_, err := q.wr.ExecContext(ctx, `INSERT INTO bans (`+banCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, id(b.AccountID), id(b.ProviderID), id(b.Subject), b.Reason, string(b.CreatedByID), int64(b.CreatedAt), ms(b.LiftedAt))
	return q.mapErr(err)
}

func (a accounts) Ban(ctx context.Context, account, by model.AccountID, reason string, at model.Time) ([]model.Ban, store.RemovedAccount, error) {
	var (
		bans    []model.Ban
		removed store.RemovedAccount
	)
	err := a.atomic(ctx, func(q *queries) error {
		acc, err := accounts{q}.Get(ctx, account)
		if err != nil {
			return err
		}
		// The identities are copied before removal deletes them.
		type identity struct{ provider, subject string }
		rows, err := q.wr.QueryContext(ctx,
			`SELECT provider_id, subject FROM identities WHERE account_id = ? ORDER BY provider_id, subject`, string(account))
		ids, err := collect(rows, err, func(s scanner) (identity, error) {
			var i identity
			err := s.Scan(str(&i.provider), &i.subject)
			return i, err
		})
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			ids = []identity{{}}
		}
		for _, i := range ids {
			b := model.Ban{AccountID: account, ProviderID: i.provider, Subject: i.subject, Reason: reason, CreatedByID: by, CreatedAt: at}
			if err := q.insertBan(ctx, &b); err != nil {
				return err
			}
			bans = append(bans, b)
		}
		if acc.Removed() {
			removed.Account = acc
			return nil
		}
		removed, err = accounts{q}.Remove(ctx, account, by, at)
		return err
	})
	return bans, removed, err
}

func (a accounts) BanIdentity(ctx context.Context, b *model.Ban) error {
	return a.insertBan(ctx, b)
}

func (a accounts) GetBan(ctx context.Context, ban string) (model.Ban, error) {
	b, err := scanBan(a.rd.QueryRowContext(ctx, `SELECT `+banCols+` FROM bans WHERE id = ?`, ban))
	return b, a.mapErr(err)
}

type banCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func (a accounts) ListBans(ctx context.Context, p store.Page) ([]model.Ban, string, error) {
	where := "1 = 1"
	var args []any
	if p.Cursor != "" {
		var c banCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		where = "(created_at < ? OR (created_at = ? AND id < ?))"
		args = append(args, c.At, c.At, c.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := a.rd.QueryContext(ctx, `SELECT `+banCols+` FROM bans WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT ?`, args...)
	out, err := collect(rows, err, scanBan)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		next = store.EncodeCursor(banCursor{At: int64(last.CreatedAt), ID: last.ID})
	}
	return out, next, nil
}

func (a accounts) LiftBan(ctx context.Context, ban string, at model.Time) error {
	return a.atomic(ctx, func(q *queries) error {
		var one int
		if err := q.wr.QueryRowContext(ctx, `SELECT 1 FROM bans WHERE id = ?`, ban).Scan(&one); err != nil {
			return q.mapErr(err)
		}
		_, err := q.wr.ExecContext(ctx, `UPDATE bans SET lifted_at = ? WHERE id = ? AND lifted_at IS NULL`, int64(at), ban)
		return err
	})
}

func (a accounts) IdentityBanned(ctx context.Context, providerID, subject string) (bool, error) {
	if providerID == "" || subject == "" {
		return false, nil
	}
	var one int
	err := a.rd.QueryRowContext(ctx, `SELECT 1 FROM bans WHERE provider_id = ? AND subject = ? AND lifted_at IS NULL LIMIT 1`,
		providerID, subject).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (a accounts) AdminDetails(ctx context.Context, ids []model.AccountID) (map[model.AccountID]store.AdminDetail, error) {
	out := make(map[model.AccountID]store.AdminDetail, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	in := `(` + marks(len(ids)) + `)`
	// Four small grouped queries, each over the page of accounts.
	each := func(query string, apply func(d *store.AdminDetail, s scanner, id *model.AccountID) error) error {
		rows, err := a.rd.QueryContext(ctx, query, anys(ids)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var acc model.AccountID
			var d store.AdminDetail
			// The row is scanned into a scratch value first, because the
			// account it belongs to is one of its columns.
			if err := apply(&d, rows, &acc); err != nil {
				return err
			}
			cur := out[acc]
			cur.PasskeyCount += d.PasskeyCount
			cur.HasTotp = cur.HasTotp || d.HasTotp
			cur.ProviderIDs = append(cur.ProviderIDs, d.ProviderIDs...)
			if d.LastSeenAt.After(cur.LastSeenAt) {
				cur.LastSeenAt = d.LastSeenAt
			}
			cur.Banned = cur.Banned || d.Banned
			out[acc] = cur
		}
		return rows.Err()
	}
	if err := each(`SELECT account_id, count(*) FROM passkeys WHERE account_id IN `+in+` GROUP BY account_id`,
		func(d *store.AdminDetail, s scanner, acc *model.AccountID) error {
			return s.Scan(str(acc), &d.PasskeyCount)
		}); err != nil {
		return nil, err
	}
	if err := each(`SELECT account_id FROM totp WHERE confirmed_at IS NOT NULL AND account_id IN `+in,
		func(d *store.AdminDetail, s scanner, acc *model.AccountID) error {
			d.HasTotp = true
			return s.Scan(str(acc))
		}); err != nil {
		return nil, err
	}
	if err := each(`SELECT DISTINCT account_id, provider_id FROM identities WHERE account_id IN `+in+` ORDER BY provider_id`,
		func(d *store.AdminDetail, s scanner, acc *model.AccountID) error {
			var p string
			err := s.Scan(str(acc), str(&p))
			d.ProviderIDs = []string{p}
			return err
		}); err != nil {
		return nil, err
	}
	if err := each(`SELECT account_id, MAX(last_seen_at) FROM sessions WHERE account_id IN `+in+` GROUP BY account_id`,
		func(d *store.AdminDetail, s scanner, acc *model.AccountID) error {
			return s.Scan(str(acc), tm(&d.LastSeenAt))
		}); err != nil {
		return nil, err
	}
	if err := each(`SELECT DISTINCT account_id FROM bans WHERE lifted_at IS NULL AND account_id IN `+in,
		func(d *store.AdminDetail, s scanner, acc *model.AccountID) error {
			d.Banned = true
			return s.Scan(str(acc))
		}); err != nil {
		return nil, err
	}
	return out, nil
}

func (a accounts) ClaimLinkFor(ctx context.Context, inv *model.Invite) error {
	return a.atomic(ctx, func(q *queries) error {
		acc, err := accounts{q}.Get(ctx, inv.ClaimsAccountID)
		if err != nil {
			return err
		}
		if !acc.Local() || acc.Removed() || !acc.ClaimedAt.IsZero() {
			return model.ErrConflict.With("Only an imported account that nobody has claimed can be given a claim link.")
		}
		now := inv.CreatedAt
		if now.IsZero() {
			now = model.Now()
		}
		if _, err := q.wr.ExecContext(ctx, `
			UPDATE invites SET revoked_at = ? WHERE claims_account_id = ? AND revoked_at IS NULL`, int64(now), string(acc.ID)); err != nil {
			return err
		}
		return invites{q}.Create(ctx, inv)
	})
}

func (a accounts) SharedWith(ctx context.Context, account model.AccountID) ([]model.AccountID, error) {
	return collectIDs[model.AccountID](a.rd.QueryContext(ctx, `
		SELECT DISTINCT m2.account_id FROM members m1 JOIN members m2 ON m2.conversation_id = m1.conversation_id
		WHERE m1.account_id = ? AND m2.account_id <> ?`, string(account), string(account)))
}

func (a accounts) Timed(ctx context.Context) ([]model.Account, error) {
	rows, err := a.rd.QueryContext(ctx, `SELECT `+accountCols+accountFrom+`
		WHERE a.removed_at IS NULL AND (a.status_until IS NOT NULL OR a.availability_until IS NOT NULL
			OR a.away_until > 0 OR a.dnd_until > 0 OR a.quiet_start IS NOT NULL OR a.quiet_skip_until IS NOT NULL)
		ORDER BY a.id`)
	return collect(rows, err, scanAccount)
}
