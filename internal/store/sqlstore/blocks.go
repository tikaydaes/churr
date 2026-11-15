package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type blocks struct{ *queries }

// Blocks returns the blocks area.
func (q *queries) Blocks() store.Blocks { return blocks{q} }

func (b blocks) Add(ctx context.Context, account, blocked model.AccountID, at model.Time) error {
	if at.IsZero() {
		at = model.Now()
	}
	// Blocking someone already blocked keeps the first row and its time.
	_, err := b.wr.ExecContext(ctx, `
		INSERT INTO blocks (account_id, blocked_id, created_at) VALUES (?, ?, ?)
		ON CONFLICT (account_id, blocked_id) DO NOTHING`, string(account), string(blocked), int64(at))
	// Who has blocked whom is kept in memory (kept.go); marked after the
	// write.
	b.touchBlocks()
	return b.mapErr(err)
}

func (b blocks) Remove(ctx context.Context, account, blocked model.AccountID) error {
	// Unblocking someone who is not blocked is not a mistake worth
	// reporting: the outcome asked for already holds.
	_, err := b.wr.ExecContext(ctx, `DELETE FROM blocks WHERE account_id = ? AND blocked_id = ?`, string(account), string(blocked))
	b.touchBlocks()
	return b.mapErr(err)
}

func (b blocks) Has(ctx context.Context, account, blocked model.AccountID) (bool, error) {
	var one int
	err := b.rd.QueryRowContext(ctx, `SELECT 1 FROM blocks WHERE account_id = ? AND blocked_id = ?`,
		string(account), string(blocked)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (b blocks) List(ctx context.Context, account model.AccountID) ([]model.AccountID, error) {
	return collectIDs[model.AccountID](b.rd.QueryContext(ctx,
		`SELECT blocked_id FROM blocks WHERE account_id = ? ORDER BY created_at DESC, blocked_id`, string(account)))
}

func (b blocks) Blockers(ctx context.Context, blocked model.AccountID, among []model.AccountID) ([]model.AccountID, error) {
	if len(among) == 0 {
		return nil, nil
	}
	args := append([]any{string(blocked)}, anys(among)...)
	return collectIDs[model.AccountID](b.rd.QueryContext(ctx,
		`SELECT account_id FROM blocks WHERE blocked_id = ? AND account_id IN (`+marks(len(among))+`)`, args...))
}
