package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/store"
)

type draws struct{ *queries }

// Draws returns the pairs the VRF key has drawn for.
func (q *queries) Draws() store.Draws { return draws{q} }

func (d draws) Take(ctx context.Context, u store.DrawUsed) error {
	_, err := d.wr.ExecContext(ctx, `INSERT INTO draws_used (second, nonce, who) VALUES (?, ?, ?) ON CONFLICT (second, nonce) DO NOTHING`,
		u.Second, u.Nonce, u.Who)
	return d.mapErr(err)
}

func (d draws) All(ctx context.Context) ([]store.DrawUsed, error) {
	rows, err := d.rd.QueryContext(ctx, `SELECT second, nonce, who FROM draws_used ORDER BY second`)
	if err != nil {
		return nil, d.mapErr(err)
	}
	defer rows.Close()
	var out []store.DrawUsed
	for rows.Next() {
		var u store.DrawUsed
		if err := rows.Scan(&u.Second, str(&u.Nonce), &u.Who); err != nil {
			return nil, d.mapErr(err)
		}
		out = append(out, u)
	}
	return out, d.mapErr(rows.Err())
}

func (d draws) Forget(ctx context.Context, before int64) error {
	_, err := d.wr.ExecContext(ctx, `DELETE FROM draws_used WHERE second < ?`, before)
	return d.mapErr(err)
}
