package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/store"
)

type fairChains struct{ *queries }

// Fair returns the heads of the chains of draws.
func (q *queries) Fair() store.Fair { return fairChains{q} }

func (f fairChains) Head(ctx context.Context, chain string) (int64, error) {
	var n int64
	err := f.rd.QueryRowContext(ctx, `SELECT n FROM fair_chains WHERE chain = ?`, chain).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, f.mapErr(err)
	}
	return n, nil
}

func (f fairChains) Advance(ctx context.Context, chain string, n int64) error {
	var res sql.Result
	var err error
	if n == 1 {
		res, err = f.wr.ExecContext(ctx, `INSERT INTO fair_chains (chain, n) VALUES (?, ?) ON CONFLICT (chain) DO NOTHING`, chain, n)
	} else {
		res, err = f.wr.ExecContext(ctx, `UPDATE fair_chains SET n = ? WHERE chain = ? AND n = ?`, n, chain, n-1)
	}
	if err != nil {
		return f.mapErr(err)
	}
	if k, err := res.RowsAffected(); err != nil {
		return err
	} else if k != 1 {
		return store.ErrFairMoved
	}
	return nil
}
