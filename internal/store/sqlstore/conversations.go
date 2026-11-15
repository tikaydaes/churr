package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type conversations struct{ *queries }

// Conversations returns the conversations area.
func (q *queries) Conversations() store.Conversations { return conversations{q} }

// conversationCols lists the columns of a conversations row with the alias
// c, and its member count.
const conversationCols = `c.id, c.kind, c.private, c.name, c.topic, c.welcome, c.section_id, c.default_level,
	c.retention_seconds, c.slow_mode_seconds, c.locked_at, c.locked_by, c.member_key, c.created_by, c.created_at,
	c.archived_at, c.next_seq, c.last_message_at, c.moved_from_message_id, c.ticket_number, c.stream_id, c.key_epoch,
	c.parent_id, c.order_floor, c.broad_mentions, (SELECT count(*) FROM members mc WHERE mc.conversation_id = c.id)`

func scanConversation(s scanner) (model.Conversation, error) {
	var c model.Conversation
	if err := scanConversationInto(s, &c); err != nil {
		return c, err
	}
	return c, nil
}

func conversationTargets(c *model.Conversation) []any {
	return []any{str(&c.ID), &c.Kind, flag(&c.Private), str(&c.Name), &c.Topic, &c.Welcome, str(&c.SectionID), str(&c.DefaultLevel),
		numPtr(&c.RetentionSeconds), &c.SlowModeSeconds, tm(&c.LockedAt), str(&c.LockedBy), str(&c.MemberKey), str(&c.CreatedByID),
		tm(&c.CreatedAt), tm(&c.ArchivedAt), &c.NextSeq, tm(&c.LastMessageAt), str(&c.MovedFromMessageID), num(&c.TicketNumber),
		str(&c.StreamID), numPtr(&c.KeyEpoch), str(&c.ParentID), &c.OrderFloor, str(&c.BroadMentions), &c.MemberCount}
}

func scanConversationInto(s scanner, c *model.Conversation) error {
	if err := s.Scan(conversationTargets(c)...); err != nil {
		return err
	}
	c.LastSeq = c.NextSeq - 1
	if c.BroadMentions == "" {
		// Null in the column: the channel follows the server.
		c.BroadMentions = model.BroadServer
	}
	return nil
}

// broadColumn is conversations.broad_mentions for a conversation's answer:
// null unless the channel has one of its own.
func broadColumn(v string) any {
	if v == model.BroadAnyone || v == model.BroadManagers {
		return v
	}
	return nil
}

func (c conversations) EnsureLibrary(ctx context.Context) (model.Conversation, error) {
	var out model.Conversation
	err := c.atomic(ctx, func(q *queries) error {
		conv, err := scanConversation(q.wr.QueryRowContext(ctx,
			`SELECT `+conversationCols+` FROM conversations c WHERE c.member_key = ?`, model.LibraryMemberKey))
		if err == nil {
			out = conv
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		out = model.Conversation{Kind: model.KindChannel, Private: true, DefaultLevel: model.LevelMember, MemberKey: model.LibraryMemberKey}
		return conversations{q}.Create(ctx, &out)
	})
	return out, err
}

func (c conversations) Create(ctx context.Context, conv *model.Conversation) error {
	if conv.ID == "" {
		conv.ID = model.NewID[model.ConversationID]()
	}
	if conv.CreatedAt.IsZero() {
		conv.CreatedAt = model.Now()
	}
	if conv.Kind == "" {
		conv.Kind = model.KindChannel
	}
	if conv.DefaultLevel == "" {
		conv.DefaultLevel = model.LevelMember
	}
	conv.NextSeq, conv.LastSeq = 1, 0
	return c.atomic(ctx, func(q *queries) error {
		// Channel names are not unique (ruling R50): nothing is checked.
		_, err := q.wr.ExecContext(ctx, `
			INSERT INTO conversations (id, kind, private, name, topic, welcome, section_id, default_level, retention_seconds,
				slow_mode_seconds, member_key, created_by, created_at, next_seq, moved_from_message_id, ticket_number,
				stream_id, key_epoch, parent_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?)`,
			string(conv.ID), conv.Kind, conv.Private, id(conv.Name), conv.Topic, conv.Welcome, id(conv.SectionID),
			string(conv.DefaultLevel), nullInt(conv.RetentionSeconds), conv.SlowModeSeconds, id(conv.MemberKey),
			id(conv.CreatedByID), int64(conv.CreatedAt), id(conv.MovedFromMessageID), zeroNull(int64(conv.TicketNumber)),
			id(conv.StreamID), nullInt(conv.KeyEpoch), id(conv.ParentID))
		return q.mapErr(err)
	})
}

func (c conversations) Get(ctx context.Context, conv model.ConversationID) (model.Conversation, error) {
	out, err := scanConversation(c.rd.QueryRowContext(ctx, `SELECT `+conversationCols+` FROM conversations c WHERE c.id = ?`, string(conv)))
	return out, c.mapErr(err)
}

func (c conversations) GetMany(ctx context.Context, ids []model.ConversationID) ([]model.Conversation, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := c.rd.QueryContext(ctx, `SELECT `+conversationCols+` FROM conversations c WHERE c.id IN (`+marks(len(ids))+`)`, anys(ids)...)
	return collect(rows, err, scanConversation)
}

func (c conversations) ByName(ctx context.Context, name string) (model.Conversation, error) {
	out, err := scanConversation(c.rd.QueryRowContext(ctx, `SELECT `+conversationCols+` FROM conversations c WHERE c.name = ? ORDER BY c.created_at, c.id LIMIT 1`, name))
	return out, c.mapErr(err)
}

func (c conversations) ByMemberKey(ctx context.Context, key string) (model.Conversation, error) {
	if key == "" {
		return model.Conversation{}, model.ErrNotFound
	}
	out, err := scanConversation(c.rd.QueryRowContext(ctx, `SELECT `+conversationCols+` FROM conversations c WHERE c.member_key = ?`, key))
	return out, c.mapErr(err)
}

func (c conversations) Update(ctx context.Context, conv model.ConversationID, fn func(*model.Conversation) error) (model.Conversation, error) {
	var out model.Conversation
	err := c.atomic(ctx, func(q *queries) error {
		cur, err := scanConversation(q.wr.QueryRowContext(ctx,
			`SELECT `+conversationCols+` FROM conversations c WHERE c.id = ?`+q.forUpdate(), string(conv)))
		if err != nil {
			return q.mapErr(err)
		}
		before := cur
		if err := fn(&cur); err != nil {
			return err
		}
		if before.Private && !cur.Private {
			return model.ErrConflict.With("A private channel can never be made public.")
		}
		_, err = q.wr.ExecContext(ctx, `
			UPDATE conversations SET private = ?, name = ?, topic = ?, welcome = ?, section_id = ?, default_level = ?,
				retention_seconds = ?, slow_mode_seconds = ?, archived_at = ?, member_key = ?, last_message_at = ?,
				moved_from_message_id = ?, ticket_number = ?, stream_id = ?, key_epoch = ?, parent_id = ?,
				broad_mentions = ?
			WHERE id = ?`,
			cur.Private, id(cur.Name), cur.Topic, cur.Welcome, id(cur.SectionID), string(cur.DefaultLevel),
			nullInt(cur.RetentionSeconds), cur.SlowModeSeconds, ms(cur.ArchivedAt), id(cur.MemberKey), ms(cur.LastMessageAt),
			id(cur.MovedFromMessageID), zeroNull(int64(cur.TicketNumber)), id(cur.StreamID), nullInt(cur.KeyEpoch),
			id(cur.ParentID), broadColumn(cur.BroadMentions), string(conv))
		// These are not Update's to change.
		cur.NextSeq, cur.LastSeq, cur.LockedAt, cur.LockedBy = before.NextSeq, before.LastSeq, before.LockedAt, before.LockedBy
		out = cur
		return q.mapErr(err)
	})
	return out, err
}

func (c conversations) NextSeq(ctx context.Context, conv model.ConversationID) (int64, error) {
	// One statement reads and advances the counter. On PostgreSQL the
	// update locks the row until the transaction ends, so a second writer
	// waits and then sees the new value; on SQLite there is one writer.
	var seq int64
	err := c.wr.QueryRowContext(ctx,
		`UPDATE conversations SET next_seq = next_seq + 1 WHERE id = ? RETURNING next_seq - 1`, string(conv)).Scan(&seq)
	return seq, c.mapErr(err)
}

func (c conversations) MakePrivate(ctx context.Context, conv model.ConversationID) (model.Conversation, error) {
	var out model.Conversation
	err := c.atomic(ctx, func(q *queries) error {
		if _, err := q.wr.ExecContext(ctx, `UPDATE conversations SET private = ? WHERE id = ?`, true, string(conv)); err != nil {
			return err
		}
		// People who are not members lose the channel at once, and the
		// mentions stored for them with it.
		_, err := q.wr.ExecContext(ctx, `
			DELETE FROM mentions WHERE conversation_id = ?
			AND account_id NOT IN (SELECT account_id FROM members WHERE conversation_id = ?)`, string(conv), string(conv))
		if err != nil {
			return err
		}
		out, err = scanConversation(q.wr.QueryRowContext(ctx, `SELECT `+conversationCols+` FROM conversations c WHERE c.id = ?`, string(conv)))
		return q.mapErr(err)
	})
	return out, err
}

func (c conversations) Lock(ctx context.Context, conv model.ConversationID, by model.AccountID, at model.Time) (model.Conversation, error) {
	var out model.Conversation
	err := c.atomic(ctx, func(q *queries) error {
		cur, err := scanConversation(q.wr.QueryRowContext(ctx,
			`SELECT `+conversationCols+` FROM conversations c WHERE c.id = ?`+q.forUpdate(), string(conv)))
		if err != nil {
			return q.mapErr(err)
		}
		if cur.Locked() {
			out = cur
			return nil
		}
		if cur.Kind != model.KindChannel || !cur.Private || cur.IsLibrary() {
			return model.ErrConflict.With("Only a private channel can be locked. Make it private first.")
		}
		if cur.IsShared() {
			return model.ErrConflict.With("A channel shared with other servers cannot be locked.")
		}
		// The people the groups had brought in stay, as direct members at
		// the level they had; the groups bring in nobody new.
		for _, stmt := range []string{
			`UPDATE members SET direct_level = level, via_group = ? WHERE conversation_id = ? AND via_group = ?`,
		} {
			if _, err := q.wr.ExecContext(ctx, stmt, false, string(conv), true); err != nil {
				return err
			}
		}
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM channel_groups WHERE conversation_id = ?`, string(conv)); err != nil {
			return err
		}
		if _, err := q.wr.ExecContext(ctx, `UPDATE conversations SET locked_at = ?, locked_by = ? WHERE id = ?`, int64(at), id(by), string(conv)); err != nil {
			return err
		}
		cur.LockedAt, cur.LockedBy = at, by
		out = cur
		return nil
	})
	return out, err
}

func (c conversations) RefreshMemberKey(ctx context.Context, conv model.ConversationID) error {
	return c.atomic(ctx, func(q *queries) error {
		var kind string
		var name sql.NullString
		var key sql.NullString
		err := q.wr.QueryRowContext(ctx, `SELECT kind, name, member_key FROM conversations WHERE id = ?`, string(conv)).Scan(&kind, &name, &key)
		if err != nil {
			return q.mapErr(err)
		}
		if kind != model.KindChannel || name.Valid || key.String == model.LibraryMemberKey {
			return nil
		}
		return q.refreshMemberKey(ctx, conv)
	})
}

func (c conversations) Delete(ctx context.Context, conv model.ConversationID) (store.DeletedConversation, error) {
	var out store.DeletedConversation
	err := c.atomic(ctx, func(q *queries) error {
		var err error
		out, err = q.deleteConversation(ctx, conv)
		return err
	})
	return out, err
}

// activityCursor is the sort key of ForAccount: most recently active first.
type activityCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func (c conversations) ForAccount(ctx context.Context, account model.AccountID, f store.ConversationFilter, p store.Page) ([]store.ConversationMember, string, error) {
	where := []string{"m.account_id = ?"}
	args := []any{string(account)}
	if f.Kind != "" {
		where = append(where, "c.kind = ?")
		args = append(args, f.Kind)
	}
	if !f.IncludeArchived {
		where = append(where, "c.archived_at IS NULL")
	}
	const activity = `COALESCE(c.last_message_at, c.created_at)`
	if p.Cursor != "" {
		var cur activityCursor
		if err := store.DecodeCursor(p.Cursor, &cur); err != nil {
			return nil, "", err
		}
		where = append(where, "("+activity+" < ? OR ("+activity+" = ? AND c.id < ?))")
		args = append(args, cur.At, cur.At, cur.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := c.rd.QueryContext(ctx, `
		SELECT `+conversationCols+`, m.`+strings.ReplaceAll(memberCols, ", ", ", m.")+`
		FROM members m JOIN conversations c ON c.id = m.conversation_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY `+activity+` DESC, c.id DESC LIMIT ?`, args...)
	out, err := collect(rows, err, func(s scanner) (store.ConversationMember, error) {
		var cm store.ConversationMember
		targets := append(conversationTargets(&cm.Conversation), memberTargets(&cm.Member)...)
		err := s.Scan(targets...)
		cm.Conversation.LastSeq = cm.Conversation.NextSeq - 1
		if cm.Conversation.BroadMentions == "" {
			// As scanConversationInto: null means the channel follows the
			// server, and the contract has no empty value.
			cm.Conversation.BroadMentions = model.BroadServer
		}
		return cm, err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1].Conversation
		at := last.LastMessageAt
		if at.IsZero() {
			at = last.CreatedAt
		}
		next = store.EncodeCursor(activityCursor{At: int64(at), ID: string(last.ID)})
	}
	return out, next, nil
}

// nameCursor is the sort key of Browse.
type nameCursor struct {
	// Name is the name as stored. The two databases do not agree on how
	// to fold case outside ASCII, so the order is the plain one and
	// clients sort without regard to case themselves (ruling R51).
	Name string `json:"n"`
	// ID breaks ties: names are not unique (ruling R50).
	ID string `json:"i"`
}

func (c conversations) Browse(ctx context.Context, f store.BrowseFilter, p store.Page) ([]model.Conversation, string, error) {
	where := []string{"c.kind = 'channel'", "c.name IS NOT NULL"}
	var args []any
	const member = `EXISTS (SELECT 1 FROM members m WHERE m.conversation_id = c.id AND m.account_id = ?)`
	if f.Public {
		where = append(where, "(c.private = ? OR "+member+")")
		args = append(args, false, string(f.AccountID))
	} else {
		where = append(where, member)
		args = append(args, string(f.AccountID))
	}
	if f.Query != "" {
		where = append(where, "(LOWER(c.name) LIKE ?"+likeEscape+" OR LOWER(c.topic) LIKE ?"+likeEscape+")")
		args = append(args, like(f.Query), like(f.Query))
	}
	if f.SectionID != "" {
		where = append(where, "c.section_id = ?")
		args = append(args, string(f.SectionID))
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
