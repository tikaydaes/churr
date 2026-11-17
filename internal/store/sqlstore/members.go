package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type members struct{ *queries }

// Members returns the members area. The rules that keep its rows true are
// in membership.go.
func (q *queries) Members() store.Members { return members{q} }

func (m members) Get(ctx context.Context, conv model.ConversationID, account model.AccountID) (model.Member, bool, error) {
	out, err := scanMember(m.rd.QueryRowContext(ctx,
		`SELECT `+memberCols+` FROM members WHERE conversation_id = ? AND account_id = ?`, string(conv), string(account)))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Member{}, false, nil
	}
	return out, err == nil, err
}

// memberCursor is the sort key of List: longest-standing first.
type memberCursor struct {
	JoinedAt int64  `json:"j"`
	ID       string `json:"i"`
}

func (m members) List(ctx context.Context, conv model.ConversationID, level model.Level, p store.Page) ([]model.Member, string, error) {
	where := "conversation_id = ?"
	args := []any{string(conv)}
	if level != "" {
		where += " AND level = ?"
		args = append(args, string(level))
	}
	if p.Cursor != "" {
		var c memberCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		where += " AND (joined_at > ? OR (joined_at = ? AND account_id > ?))"
		args = append(args, c.JoinedAt, c.JoinedAt, c.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := m.rd.QueryContext(ctx, `SELECT `+memberCols+` FROM members WHERE `+where+` ORDER BY joined_at, account_id LIMIT ?`, args...)
	out, err := collect(rows, err, scanMember)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		next = store.EncodeCursor(memberCursor{JoinedAt: int64(last.JoinedAt), ID: string(last.AccountID)})
	}
	return out, next, nil
}

func (m members) IDs(ctx context.Context, conv model.ConversationID) ([]model.AccountID, error) {
	return collectIDs[model.AccountID](m.rd.QueryContext(ctx,
		`SELECT account_id FROM members WHERE conversation_id = ? ORDER BY joined_at, account_id`, string(conv)))
}

func (m members) Count(ctx context.Context, conv model.ConversationID) (int64, error) {
	var n int64
	err := m.rd.QueryRowContext(ctx, `SELECT count(*) FROM members WHERE conversation_id = ?`, string(conv)).Scan(&n)
	return n, err
}

func (m members) OfAccount(ctx context.Context, account model.AccountID) ([]model.Member, error) {
	rows, err := m.rd.QueryContext(ctx, `SELECT `+memberCols+` FROM members WHERE account_id = ?`, string(account))
	return collect(rows, err, scanMember)
}

func (m members) Keepers(ctx context.Context, conv model.ConversationID) ([]model.AccountID, error) {
	return collectIDs[model.AccountID](m.rd.QueryContext(ctx,
		`SELECT account_id FROM members WHERE conversation_id = ? AND level IN ('moderator', 'manager') ORDER BY joined_at, account_id`, string(conv)))
}

func (m members) Managers(ctx context.Context, conv model.ConversationID) ([]model.AccountID, error) {
	return collectIDs[model.AccountID](m.rd.QueryContext(ctx,
		`SELECT account_id FROM members WHERE conversation_id = ? AND level = 'manager' ORDER BY joined_at, account_id`, string(conv)))
}

// conversationState reads what the membership rules need to know about a
// conversation, locking the row on PostgreSQL so two changes to the same
// conversation's members run one after the other.
func (q *queries) conversationState(ctx context.Context, conv model.ConversationID) (model.Conversation, error) {
	var c model.Conversation
	err := q.wr.QueryRowContext(ctx,
		`SELECT id, kind, private, name, locked_at, member_key, stream_id FROM conversations WHERE id = ?`+q.forUpdate(), string(conv)).
		Scan(str(&c.ID), &c.Kind, flag(&c.Private), str(&c.Name), tm(&c.LockedAt), str(&c.MemberKey), str(&c.StreamID))
	return c, q.mapErr(err)
}

func (m members) AddDirect(ctx context.Context, conv model.ConversationID, account model.AccountID, level model.Level, at model.Time) (store.MembershipResult, error) {
	return m.addDirect(ctx, conv, account, level, at, false)
}

func (m members) AddByOverride(ctx context.Context, conv model.ConversationID, account model.AccountID, level model.Level, at model.Time) (store.MembershipResult, error) {
	return m.addDirect(ctx, conv, account, level, at, true)
}

func (m members) addDirect(ctx context.Context, conv model.ConversationID, account model.AccountID, level model.Level, at model.Time, override bool) (store.MembershipResult, error) {
	var res store.MembershipResult
	if !level.Valid() {
		return res, model.Invalid("level", "A level is reader, member, moderator or manager.")
	}
	err := m.atomic(ctx, func(q *queries) error {
		c, err := q.conversationState(ctx, conv)
		if err != nil {
			return err
		}
		if c.IsLibrary() {
			return model.ErrNotFound
		}
		cur, found, err := q.memberRow(ctx, conv, account)
		if err != nil {
			return err
		}
		if found && cur.DirectLevel != "" {
			return model.ErrAlreadyExists.With("That account is already in this conversation.")
		}
		// Nobody is in a locked channel through a group, so any add
		// there would insert a row, and no row is inserted, but for an
		// Owner's override.
		if c.Locked() && !override {
			return model.ErrLocked
		}
		ch, err := q.recompute(ctx, conv, account, &level, at)
		if err != nil {
			return err
		}
		if ch != nil {
			res.Changes = append(res.Changes, *ch)
		}
		return q.settle(ctx, conv, &res)
	})
	return res, err
}

func (m members) SetDirectLevel(ctx context.Context, conv model.ConversationID, account model.AccountID, level model.Level) (store.MembershipResult, error) {
	var res store.MembershipResult
	if !level.Valid() {
		return res, model.Invalid("level", "A level is reader, member, moderator or manager.")
	}
	err := m.atomic(ctx, func(q *queries) error {
		if _, err := q.conversationState(ctx, conv); err != nil {
			return err
		}
		_, found, err := q.memberRow(ctx, conv, account)
		if err != nil {
			return err
		}
		if !found {
			return model.ErrNotFound
		}
		ch, err := q.recompute(ctx, conv, account, &level, 0)
		if err != nil {
			return err
		}
		if ch != nil {
			res.Changes = append(res.Changes, *ch)
		}
		// Leaving promotes someone; lowering a level must not. A
		// channel's last manager cannot step down by this route, unless a
		// hub owns it: its managers are those on the hub's list, wherever
		// they are (docs/PERMISSIONS.md section 14.1).
		var managers int64
		var hubOwned bool
		err = q.wr.QueryRowContext(ctx, `
			SELECT (SELECT count(*) FROM members WHERE conversation_id = ? AND level = 'manager'),
				EXISTS (SELECT 1 FROM stream_channels sc JOIN conversations c ON c.stream_id = sc.stream_id WHERE c.id = ?)`,
			string(conv), string(conv)).Scan(&managers, flag(&hubOwned))
		if err != nil {
			return err
		}
		if managers == 0 && !hubOwned {
			return model.ErrConflict.With("A channel needs at least one manager.")
		}
		return nil
	})
	if err != nil {
		return store.MembershipResult{}, err
	}
	return res, nil
}

func (m members) RemoveDirect(ctx context.Context, conv model.ConversationID, account model.AccountID) (store.MembershipResult, error) {
	var res store.MembershipResult
	err := m.atomic(ctx, func(q *queries) error {
		if _, err := q.conversationState(ctx, conv); err != nil {
			return err
		}
		cur, found, err := q.memberRow(ctx, conv, account)
		if err != nil {
			return err
		}
		if !found || cur.DirectLevel == "" {
			return model.ErrNotFound
		}
		none := model.Level("")
		ch, err := q.recompute(ctx, conv, account, &none, 0)
		if err != nil {
			return err
		}
		if ch != nil {
			res.Changes = append(res.Changes, *ch)
		}
		return q.settle(ctx, conv, &res)
	})
	if err != nil {
		return store.MembershipResult{}, err
	}
	return res, nil
}

func (m members) SetRead(ctx context.Context, conv model.ConversationID, account model.AccountID, seq int64, flat bool) (model.Member, error) {
	var out model.Member
	err := m.atomic(ctx, func(q *queries) error {
		var nextSeq int64
		if err := q.wr.QueryRowContext(ctx, `SELECT next_seq FROM conversations WHERE id = ?`, string(conv)).Scan(&nextSeq); err != nil {
			return q.mapErr(err)
		}
		// The position is never beyond the latest event.
		if seq > nextSeq-1 {
			seq = nextSeq - 1
		}
		if seq < 0 {
			seq = 0
		}
		err := q.changed(q.wr.ExecContext(ctx,
			`UPDATE members SET read_seq = ? WHERE conversation_id = ? AND account_id = ?`, seq, string(conv), string(account)))
		if err != nil {
			return err
		}
		if flat {
			// In the flat view replies are read in the conversation
			// itself, so thread positions follow it upwards.
			_, err := q.wr.ExecContext(ctx, `
				UPDATE thread_states SET read_seq = ?
				WHERE account_id = ? AND read_seq < ? AND parent_id IN (SELECT id FROM messages WHERE conversation_id = ?)`,
				seq, string(account), seq, string(conv))
			if err != nil {
				return err
			}
		}
		var found bool
		out, found, err = q.memberRow(ctx, conv, account)
		if err == nil && !found {
			err = model.ErrNotFound
		}
		return err
	})
	return out, err
}

func (m members) UpdateOwn(ctx context.Context, conv model.ConversationID, account model.AccountID, fn func(*model.Member) error) (model.Member, error) {
	var out model.Member
	err := m.atomic(ctx, func(q *queries) error {
		cur, err := scanMember(q.wr.QueryRowContext(ctx,
			`SELECT `+memberCols+` FROM members WHERE conversation_id = ? AND account_id = ?`+q.forUpdate(), string(conv), string(account)))
		if err != nil {
			return q.mapErr(err)
		}
		if err := fn(&cur); err != nil {
			return err
		}
		_, err = q.wr.ExecContext(ctx, `
			UPDATE members SET mode = ?, show = ?, mode_before_mute = ?, muted_until = ?, starred = ?, thread_view = ?, last_post_at = ?,
				ignore_broad = ?
			WHERE conversation_id = ? AND account_id = ?`,
			string(cur.Mode), id(cur.Show), id(cur.ModeBeforeMute), ms(cur.MutedUntil), cur.Starred, id(cur.ThreadView), ms(cur.LastPostAt),
			cur.IgnoreBroad,
			string(conv), string(account))
		out = cur
		q.touchMember(conv, account)
		return q.mapErr(err)
	})
	return out, err
}

func (m members) SetMayPost(ctx context.Context, conv model.ConversationID, account model.AccountID, may bool) (model.Member, error) {
	var out model.Member
	err := m.atomic(ctx, func(q *queries) error {
		if _, err := q.wr.ExecContext(ctx, `UPDATE members SET may_post = ? WHERE conversation_id = ? AND account_id = ?`,
			may, string(conv), string(account)); err != nil {
			return q.mapErr(err)
		}
		row, found, err := q.memberRow(ctx, conv, account)
		if err == nil && !found {
			err = model.ErrNotFound
		}
		out = row
		return err
	})
	return out, err
}

func (m members) Unread(ctx context.Context, conv model.ConversationID, account model.AccountID) (store.UnreadSets, error) {
	var u store.UnreadSets
	var readSeq int64
	err := m.rd.QueryRowContext(ctx,
		`SELECT read_seq FROM members WHERE conversation_id = ? AND account_id = ?`, string(conv), string(account)).Scan(&readSeq)
	if err != nil {
		return u, m.mapErr(err)
	}
	// A reply can be unread below the conversation's position if its
	// thread's own position is lower, so the scan starts at the lowest
	// position the account holds here.
	var lowest sql.NullInt64
	err = m.rd.QueryRowContext(ctx, `
		SELECT MIN(ts.read_seq) FROM thread_states ts JOIN messages p ON p.id = ts.parent_id
		WHERE ts.account_id = ? AND p.conversation_id = ?`, string(account), string(conv)).Scan(&lowest)
	if err != nil {
		return u, err
	}
	from := readSeq
	if lowest.Valid && lowest.Int64 < from {
		from = lowest.Int64
	}

	// One pass over the messages above that point. For each: is it
	// counted, is it unread at its own position, which set is it in, and
	// does the account have a mention row for it. isIgn is written so that
	// it is never NULL: for a reply in a thread the account has no state
	// for, ts.state is NULL, and a NULL there would make hasMen NULL too
	// and leave out a mention that counts.
	const (
		isTop   = `m.parent_id IS NULL`
		isFol   = `(m.parent_id IS NOT NULL AND ts.state = 'following')`
		isNei   = `(m.parent_id IS NOT NULL AND ts.state IS NULL)`
		isIgn   = `(m.parent_id IS NOT NULL AND COALESCE(ts.state, '') = 'ignored')`
		hasMen  = `(mn.account_id IS NOT NULL AND (NOT ` + isIgn + ` OR mn.kind = 'account'))`
		countIf = `COALESCE(SUM(CASE WHEN `
		endIf   = ` THEN 1 ELSE 0 END), 0)`
	)
	err = m.rd.QueryRowContext(ctx, `
		SELECT `+countIf+isTop+endIf+`, `+countIf+isFol+endIf+`, `+countIf+isNei+endIf+`,
			`+countIf+hasMen+endIf+`, `+countIf+`mn.kind = 'account'`+endIf+`,
			`+countIf+hasMen+` AND `+isTop+endIf+`, `+countIf+hasMen+` AND `+isFol+endIf+`,
			`+countIf+hasMen+` AND `+isNei+endIf+`, `+countIf+hasMen+` AND `+isIgn+endIf+`,
			COALESCE((SELECT c.next_seq - 1 FROM conversations c WHERE c.id = ?), 0)
		FROM messages m
		LEFT JOIN thread_states ts ON ts.parent_id = m.parent_id AND ts.account_id = ?
		LEFT JOIN mentions mn ON mn.message_id = m.id AND mn.account_id = ?
		WHERE m.conversation_id = ? AND m.seq > ?
			AND m.deleted_at IS NULL AND m.system IS NULL AND m.quiet = ?
			AND (m.author_id <> ? OR m.via_kind = 'reminder')
			AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.account_id = ? AND b.blocked_id = m.author_id)
			AND m.seq > CASE WHEN m.parent_id IS NULL THEN ? ELSE COALESCE(ts.read_seq, ?) END`,
		string(conv), string(account), string(account), string(conv), from, false, string(account), string(account), readSeq, readSeq).
		Scan(&u.Top, &u.Fol, &u.Nei, &u.Men, &u.Nam, &u.MenTop, &u.MenFol, &u.MenNei, &u.MenIgn, &u.AsOf)
	return u, err
}

func (m members) AddGroup(ctx context.Context, cg model.ChannelGroup) (store.MembershipResult, bool, error) {
	var res store.MembershipResult
	added := false
	if !cg.Level.Valid() {
		return res, false, model.Invalid("level", "A level is reader, member, moderator or manager.")
	}
	if cg.AddedAt.IsZero() {
		cg.AddedAt = model.Now()
	}
	err := m.atomic(ctx, func(q *queries) error {
		c, err := q.conversationState(ctx, cg.ConversationID)
		if err != nil {
			return err
		}
		if c.Kind != model.KindChannel || c.Name == "" {
			// An ad-hoc channel's identity is its member list, a sealed
			// chat is two people, and the library has no members.
			return model.ErrConflict.With("Groups can be added to named channels only.")
		}
		if c.Locked() {
			return model.ErrLocked
		}
		var one int
		err = q.wr.QueryRowContext(ctx, `SELECT 1 FROM channel_groups WHERE conversation_id = ? AND group_id = ?`,
			string(cg.ConversationID), string(cg.GroupID)).Scan(&one)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			added = true
			_, err = q.wr.ExecContext(ctx, `
				INSERT INTO channel_groups (conversation_id, group_id, level, added_by, added_at) VALUES (?, ?, ?, ?, ?)`,
				string(cg.ConversationID), string(cg.GroupID), string(cg.Level), id(cg.AddedByID), int64(cg.AddedAt))
		case err == nil:
			_, err = q.wr.ExecContext(ctx, `UPDATE channel_groups SET level = ? WHERE conversation_id = ? AND group_id = ?`,
				string(cg.Level), string(cg.ConversationID), string(cg.GroupID))
		}
		if err != nil {
			if q.db.dialect.ForeignKeyViolation(err) {
				return model.ErrNotFound
			}
			return q.mapErr(err)
		}
		ids, err := q.groupMemberIDs(ctx, cg.GroupID)
		if err != nil {
			return err
		}
		if err := q.recomputeMany(ctx, cg.ConversationID, ids, cg.AddedAt, &res); err != nil {
			return err
		}
		return q.settle(ctx, cg.ConversationID, &res)
	})
	if err != nil {
		return store.MembershipResult{}, false, err
	}
	return res, added, nil
}

func (m members) RemoveGroup(ctx context.Context, conv model.ConversationID, group model.GroupID) (store.MembershipResult, error) {
	var res store.MembershipResult
	err := m.atomic(ctx, func(q *queries) error {
		if _, err := q.conversationState(ctx, conv); err != nil {
			return err
		}
		err := q.changed(q.wr.ExecContext(ctx,
			`DELETE FROM channel_groups WHERE conversation_id = ? AND group_id = ?`, string(conv), string(group)))
		if err != nil {
			return err
		}
		ids, err := q.groupMemberIDs(ctx, group)
		if err != nil {
			return err
		}
		if err := q.recomputeMany(ctx, conv, ids, 0, &res); err != nil {
			return err
		}
		return q.settle(ctx, conv, &res)
	})
	if err != nil {
		return store.MembershipResult{}, err
	}
	return res, nil
}

func scanChannelGroup(s scanner) (model.ChannelGroup, error) {
	var cg model.ChannelGroup
	err := s.Scan(str(&cg.ConversationID), str(&cg.GroupID), str(&cg.Level), str(&cg.AddedByID), tm(&cg.AddedAt))
	return cg, err
}

func (m members) Groups(ctx context.Context, conv model.ConversationID) ([]model.ChannelGroup, error) {
	rows, err := m.rd.QueryContext(ctx, `
		SELECT conversation_id, group_id, level, added_by, added_at FROM channel_groups
		WHERE conversation_id = ? ORDER BY added_at, group_id`, string(conv))
	return collect(rows, err, scanChannelGroup)
}
