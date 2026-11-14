package sqlstore

import (
	"context"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func (c conversations) AdminList(ctx context.Context, f store.AdminChannelFilter, p store.Page) ([]model.Conversation, string, error) {
	// A named channel has a name. Ad-hoc channels, sealed chats and the
	// library have none, so they can never be listed here.
	where := []string{"c.kind = 'channel'", "c.name IS NOT NULL"}
	var args []any
	if f.Query != "" {
		where = append(where, "LOWER(c.name) LIKE ?"+likeEscape)
		args = append(args, like(f.Query))
	}
	if f.Private != nil {
		where = append(where, "c.private = ?")
		args = append(args, *f.Private)
	}
	if f.ParentID != "" {
		where = append(where, "c.parent_id = ?")
		args = append(args, string(f.ParentID))
	}
	if !f.IncludeArchived {
		where = append(where, "c.archived_at IS NULL")
	}
	if p.Cursor != "" {
		var cur nameCursor
		if err := store.DecodeCursor(p.Cursor, &cur); err != nil {
			return nil, "", err
		}
		where = append(where, "(c.name > ? OR (c.name = ? AND c.id > ?))")
		args = append(args, cur.Name, cur.Name, cur.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := c.rd.QueryContext(ctx, `SELECT `+conversationCols+` FROM conversations c WHERE `+strings.Join(where, " AND ")+
		` ORDER BY c.name, c.id LIMIT ?`, args...)
	out, err := collect(rows, err, scanConversation)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = store.EncodeCursor(nameCursor{Name: out[limit-1].Name, ID: string(out[limit-1].ID)})
	}
	return out, next, nil
}

func (c conversations) ByNameAll(ctx context.Context, name string) ([]model.Conversation, error) {
	if name == "" {
		return nil, nil
	}
	rows, err := c.rd.QueryContext(ctx,
		`SELECT `+conversationCols+` FROM conversations c WHERE c.kind = 'channel' AND c.name = ? ORDER BY c.created_at, c.id`, name)
	return collect(rows, err, scanConversation)
}

func (c conversations) SubChannelCount(ctx context.Context, parent model.ConversationID) (int64, error) {
	var n int64
	err := c.rd.QueryRowContext(ctx, `SELECT count(*) FROM conversations WHERE parent_id = ?`, string(parent)).Scan(&n)
	return n, err
}
