package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type recovery struct{ *queries }

// Recovery returns the recovery area.
func (q *queries) Recovery() store.Recovery { return recovery{q} }

func (r recovery) ReplaceCodes(ctx context.Context, account model.AccountID, hashes [][]byte) error {
	return r.atomic(ctx, func(q *queries) error {
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM recovery_codes WHERE account_id = ?`, string(account)); err != nil {
			return q.mapErr(err)
		}
		for _, h := range hashes {
			if len(h) == 0 {
				return errors.New("sqlstore: a recovery code is stored by its hash, and one was empty")
			}
			// The same hash given twice is one code.
			_, err := q.wr.ExecContext(ctx, `
				INSERT INTO recovery_codes (account_id, code_hash) VALUES (?, ?)
				ON CONFLICT (account_id, code_hash) DO NOTHING`, string(account), h)
			if err != nil {
				return q.mapErr(err)
			}
		}
		return nil
	})
}

func (r recovery) UseCode(ctx context.Context, account model.AccountID, hash []byte, at model.Time) (bool, error) {
	if len(hash) == 0 {
		return false, nil
	}
	// One conditional update: of two requests with the same code, the
	// database lets exactly one find the row unused.
	n, err := r.affected(r.wr.ExecContext(ctx, `
		UPDATE recovery_codes SET used_at = ? WHERE account_id = ? AND code_hash = ? AND used_at IS NULL`,
		int64(at), string(account), hash))
	return n > 0, err
}

func (r recovery) CountCodes(ctx context.Context, account model.AccountID) (int, int, error) {
	var total, unused int64
	err := r.rd.QueryRowContext(ctx, `
		SELECT count(*), COALESCE(SUM(CASE WHEN used_at IS NULL THEN 1 ELSE 0 END), 0)
		FROM recovery_codes WHERE account_id = ?`, string(account)).Scan(&total, &unused)
	return int(total), int(unused), err
}

func (r recovery) CreateLink(ctx context.Context, tokenHash []byte, account, createdBy model.AccountID, expiresAt model.Time) error {
	if len(tokenHash) == 0 {
		return errors.New("sqlstore: a recovery link is stored by the hash of its value, and it was empty")
	}
	return r.atomic(ctx, func(q *queries) error {
		// Making a link withdraws any earlier one that was never used
		// (docs/AUTH.md section 6.2), so only the newest link works.
		if _, err := q.wr.ExecContext(ctx,
			`DELETE FROM recovery_links WHERE account_id = ? AND used_at IS NULL`, string(account)); err != nil {
			return q.mapErr(err)
		}
		n, err := q.affected(q.wr.ExecContext(ctx, `
			INSERT INTO recovery_links (token_hash, account_id, created_by, expires_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (token_hash) DO NOTHING`, tokenHash, string(account), id(createdBy), int64(expiresAt)))
		if err != nil {
			return err
		}
		if n == 0 {
			return model.ErrAlreadyExists
		}
		return nil
	})
}

func (r recovery) UseLink(ctx context.Context, tokenHash []byte, at model.Time) (model.AccountID, bool, error) {
	if len(tokenHash) == 0 {
		return "", false, nil
	}
	var account model.AccountID
	err := r.wr.QueryRowContext(ctx, `
		UPDATE recovery_links SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?
		RETURNING account_id`, int64(at), tokenHash, int64(at)).Scan(str(&account))
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return account, err == nil, err
}

func (r recovery) DeleteLinksEnded(ctx context.Context, before model.Time) (int64, error) {
	return r.affected(r.wr.ExecContext(ctx, `
		DELETE FROM recovery_links WHERE (used_at IS NOT NULL AND used_at < ?) OR expires_at < ?`,
		int64(before), int64(before)))
}
