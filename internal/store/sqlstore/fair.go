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

func (f fairChains) Head(ctx context.Context, chain string) (int64, []byte, error) {
	var n int64
	var prev []byte
	err := f.rd.QueryRowContext(ctx, `SELECT n, prev FROM fair_chains WHERE chain = ?`, chain).Scan(&n, &prev)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, f.mapErr(err)
	}
	return n, prev, nil
}

func (f fairChains) Advance(ctx context.Context, chain string, n int64, prev []byte) error {
	var res sql.Result
	var err error
	if n == 1 {
		res, err = f.wr.ExecContext(ctx, `INSERT INTO fair_chains (chain, n, prev) VALUES (?, ?, ?) ON CONFLICT (chain) DO NOTHING`, chain, n, prev)
	} else {
		res, err = f.wr.ExecContext(ctx, `UPDATE fair_chains SET n = ?, prev = ? WHERE chain = ? AND n = ?`, n, prev, chain, n-1)
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
