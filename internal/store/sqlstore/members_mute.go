package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

func (m members) ExpiredMutes(ctx context.Context, now model.Time, limit int) ([]model.Member, error) {
	rows, err := m.rd.QueryContext(ctx, `SELECT `+memberCols+` FROM members
		WHERE muted_until IS NOT NULL AND muted_until <= ? ORDER BY muted_until, conversation_id, account_id LIMIT ?`,
		int64(now), limit)
	return collect(rows, err, scanMember)
}
