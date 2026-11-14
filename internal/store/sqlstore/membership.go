package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file keeps the `members` invariants of docs/SCHEMA.md section 5.
// Every area that changes an input to a member row (a direct level, who is
// in a group, which groups are in a channel, a role, a claim, a removal)
// ends by calling recompute for the rows concerned and settle for the
// conversations concerned, inside its transaction.

// Who the two built-in groups contain, as conditions on an accounts row
// with the alias a.
const (
	inEveryone = `a.server_id IS NULL AND a.role IN ('owner', 'admin', 'member') AND a.claimed_at IS NOT NULL AND a.removed_at IS NULL`
	inAdmins   = `a.server_id IS NULL AND a.role IN ('owner', 'admin') AND a.claimed_at IS NOT NULL AND a.removed_at IS NULL`
)

// groupContains is a condition on a groups row with the alias g that is true
// when the group contains one account. It takes the account id three times.
const groupContains = `(
	(g.builtin IS NULL AND EXISTS (SELECT 1 FROM group_members gm WHERE gm.group_id = g.id AND gm.account_id = ?))
	OR (g.builtin = 'everyone' AND EXISTS (SELECT 1 FROM accounts a WHERE a.id = ? AND ` + inEveryone + `))
	OR (g.builtin = 'admins' AND EXISTS (SELECT 1 FROM accounts a WHERE a.id = ? AND ` + inAdmins + `)))`

const memberCols = `conversation_id, account_id, level, direct_level, via_group, joined_at, read_seq, mode, show, mode_before_mute, muted_until, starred, thread_view, last_post_at`

func memberTargets(m *model.Member) []any {
	return []any{str(&m.ConversationID), str(&m.AccountID), str(&m.Level), str(&m.DirectLevel), flag(&m.ViaGroup),
		tm(&m.JoinedAt), &m.ReadSeq, str(&m.Mode), str(&m.Show), str(&m.ModeBeforeMute), tm(&m.MutedUntil), flag(&m.Starred),
		str(&m.ThreadView), tm(&m.LastPostAt)}
}

func scanMember(s scanner) (model.Member, error) {
	var m model.Member
	err := s.Scan(memberTargets(&m)...)
	return m, err
}

// memberRow reads one member row through the write handle, so that inside a
// transaction it sees what the transaction has done.
func (q *queries) memberRow(ctx context.Context, conv model.ConversationID, account model.AccountID) (model.Member, bool, error) {
	m, err := scanMember(q.wr.QueryRowContext(ctx,
		`SELECT `+memberCols+` FROM members WHERE conversation_id = ? AND account_id = ?`, string(conv), string(account)))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Member{}, false, nil
	}
	return m, err == nil, err
}

// recompute works out one account's row in one conversation from its routes
// in: the level it holds directly and the groups added to the conversation
// that contain it. If setDirect is not nil the direct level is first set to
// it (a pointer to "" takes the direct level away). It inserts, updates or
// deletes the row as needed and returns what changed, or nil.
func (q *queries) recompute(ctx context.Context, conv model.ConversationID, account model.AccountID, setDirect *model.Level, at model.Time) (*store.MemberChange, error) {
	cur, found, err := q.memberRow(ctx, conv, account)
	if err != nil {
		return nil, err
	}
	direct := cur.DirectLevel
	if setDirect != nil {
		direct = *setDirect
	}

	rows, err := q.wr.QueryContext(ctx, `
		SELECT cg.level FROM channel_groups cg JOIN groups g ON g.id = cg.group_id
		WHERE cg.conversation_id = ? AND `+groupContains,
		string(conv), string(account), string(account), string(account))
	groupLevels, err := collect(rows, err, func(s scanner) (model.Level, error) {
		var l model.Level
		err := s.Scan(str(&l))
		return l, err
	})
	if err != nil {
		return nil, err
	}
	via := len(groupLevels) > 0
	level := model.HighestLevel(append(groupLevels, direct)...)

	switch {
	case !found && level == "":
		return nil, nil

	case found && level == "":
		if err := q.deleteMemberRow(ctx, conv, account); err != nil {
			return nil, err
		}
		return &store.MemberChange{Kind: store.MemberLeft, Member: cur}, nil

	case !found:
		var name sql.NullString
		var nextSeq int64
		err := q.wr.QueryRowContext(ctx, `SELECT name, next_seq FROM conversations WHERE id = ?`, string(conv)).Scan(&name, &nextSeq)
		if err != nil {
			return nil, q.mapErr(err)
		}
		m := model.Member{
			ConversationID: conv, AccountID: account, Level: level, DirectLevel: direct, ViaGroup: via,
			JoinedAt: at,
			// A newcomer is not handed the whole history as unread.
			ReadSeq: nextSeq - 1,
			Mode:    model.ModeAny,
		}
		if name.Valid {
			m.Mode = model.ModeMentions
		}
		_, err = q.wr.ExecContext(ctx, `
			INSERT INTO members (conversation_id, account_id, level, direct_level, via_group, joined_at, read_seq, mode, starred)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(conv), string(account), string(m.Level), id(m.DirectLevel), m.ViaGroup, int64(m.JoinedAt), m.ReadSeq, string(m.Mode), false)
		if err != nil {
			return nil, q.mapErr(err)
		}
		return &store.MemberChange{Kind: store.MemberJoined, Member: m}, nil
	}

	if cur.Level == level && cur.DirectLevel == direct && cur.ViaGroup == via {
		return nil, nil
	}
	_, err = q.wr.ExecContext(ctx, `
		UPDATE members SET level = ?, direct_level = ?, via_group = ? WHERE conversation_id = ? AND account_id = ?`,
		string(level), id(direct), via, string(conv), string(account))
	if err != nil {
		return nil, q.mapErr(err)
	}
	levelChanged := cur.Level != level
	cur.Level, cur.DirectLevel, cur.ViaGroup = level, direct, via
	if !levelChanged {
		// Only the route changed. Nobody else can see that, so there is
		// nothing to announce.
		return nil, nil
	}
	return &store.MemberChange{Kind: store.MemberLevelChanged, Member: cur}, nil
}

// deleteMemberRow removes a member row and what belonged to the membership:
// the account's thread states, mentions and scheduled items there.
func (q *queries) deleteMemberRow(ctx context.Context, conv model.ConversationID, account model.AccountID) error {
	for _, stmt := range []string{
		`DELETE FROM members WHERE conversation_id = ? AND account_id = ?`,
		`DELETE FROM thread_states WHERE parent_id IN (SELECT id FROM messages WHERE conversation_id = ?) AND account_id = ?`,
		`DELETE FROM mentions WHERE conversation_id = ? AND account_id = ?`,
		`DELETE FROM scheduled WHERE conversation_id = ? AND account_id = ?`,
	} {
		if _, err := q.wr.ExecContext(ctx, stmt, string(conv), string(account)); err != nil {
			return q.mapErr(err)
		}
	}
	return nil
}

// recomputeMany recomputes several accounts in one conversation and adds
// what changed to res.
func (q *queries) recomputeMany(ctx context.Context, conv model.ConversationID, accounts []model.AccountID, at model.Time, res *store.MembershipResult) error {
	for _, a := range accounts {
		ch, err := q.recompute(ctx, conv, a, nil, at)
		if err != nil {
			return err
		}
		if ch != nil {
			res.Changes = append(res.Changes, *ch)
		}
	}
	return nil
}

// settle applies the rules that follow from who is left in a conversation:
// a channel with members and no manager gets its longest-standing member as
// manager; a private or ad-hoc channel, or a sealed chat, with nobody left
// is deleted with its content; an ad-hoc channel's member key follows its
// members.
func (q *queries) settle(ctx context.Context, conv model.ConversationID, res *store.MembershipResult) error {
	var (
		kind      string
		private   bool
		name      sql.NullString
		memberKey sql.NullString
	)
	err := q.wr.QueryRowContext(ctx, `SELECT kind, private, name, member_key FROM conversations WHERE id = ?`, string(conv)).
		Scan(&kind, flag(&private), &name, &memberKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // already deleted in this transaction
	}
	if err != nil {
		return err
	}
	if memberKey.String == model.LibraryMemberKey {
		return nil
	}
	var members, managers int64
	err = q.wr.QueryRowContext(ctx, `
		SELECT count(*), COALESCE(SUM(CASE WHEN level = 'manager' THEN 1 ELSE 0 END), 0)
		FROM members WHERE conversation_id = ?`, string(conv)).Scan(&members, &managers)
	if err != nil {
		return err
	}

	if members == 0 {
		if !private {
			return nil // a public channel with no members stays and can be joined
		}
		del, err := q.deleteConversation(ctx, conv)
		if err != nil {
			return err
		}
		res.Deleted = append(res.Deleted, del)
		return nil
	}

	if kind == model.KindChannel && managers == 0 {
		// The shadow of someone on another server is never a manager
		// here (docs/PERMISSIONS.md decision P19).
		var account model.AccountID
		err := q.wr.QueryRowContext(ctx, `
			SELECT m.account_id FROM members m JOIN accounts a ON a.id = m.account_id
			WHERE m.conversation_id = ? AND a.role <> 'remote'
			ORDER BY m.joined_at, m.account_id LIMIT 1`, string(conv)).Scan(str(&account))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			manager := model.LevelManager
			ch, err := q.recompute(ctx, conv, account, &manager, 0)
			if err != nil {
				return err
			}
			if ch != nil {
				res.Changes = append(res.Changes, *ch)
			}
		}
	}

	if kind == model.KindChannel && !name.Valid {
		return q.refreshMemberKey(ctx, conv)
	}
	return nil
}

// settleAll settles each conversation once.
func (q *queries) settleAll(ctx context.Context, convs []model.ConversationID, res *store.MembershipResult) error {
	seen := map[model.ConversationID]bool{}
	for _, c := range convs {
		if seen[c] {
			continue
		}
		seen[c] = true
		if err := q.settle(ctx, c, res); err != nil {
			return err
		}
	}
	return nil
}

// refreshMemberKey works out an ad-hoc channel's member key from its
// members. If another ad-hoc channel holds that key already, this one's is
// set to null and the older channel keeps it (docs/SCHEMA.md decision S8).
func (q *queries) refreshMemberKey(ctx context.Context, conv model.ConversationID) error {
	ids, err := collectIDs[model.AccountID](q.wr.QueryContext(ctx,
		`SELECT account_id FROM members WHERE conversation_id = ?`, string(conv)))
	if err != nil {
		return err
	}
	key := store.AdhocMemberKey(ids)
	var holder model.ConversationID
	err = q.wr.QueryRowContext(ctx, `SELECT id FROM conversations WHERE member_key = ?`, key).Scan(str(&holder))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = q.wr.ExecContext(ctx, `UPDATE conversations SET member_key = ? WHERE id = ? AND kind = 'channel' AND name IS NULL`, key, string(conv))
	case err != nil:
	case holder != conv:
		_, err = q.wr.ExecContext(ctx, `UPDATE conversations SET member_key = NULL WHERE id = ? AND kind = 'channel' AND name IS NULL`, string(conv))
	}
	return q.mapErr(err)
}

// deleteConversation deletes a conversation and everything that hangs from
// it, and returns the blobs its files held.
func (q *queries) deleteConversation(ctx context.Context, conv model.ConversationID) (store.DeletedConversation, error) {
	del := store.DeletedConversation{ID: conv}
	rows, err := q.wr.QueryContext(ctx, `SELECT blob_key, thumb_key, poster_key FROM files WHERE conversation_id = ?`, string(conv))
	if err != nil {
		return del, err
	}
	for rows.Next() {
		var f model.File
		if err := rows.Scan(str(&f.BlobKey), str(&f.ThumbKey), str(&f.PosterKey)); err != nil {
			rows.Close()
			return del, err
		}
		del.BlobKeys = append(del.BlobKeys, store.BlobKeys(f)...)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return del, err
	}
	if !q.pg() {
		// PostgreSQL's search column goes with the rows. SQLite's index
		// is a table of its own with no foreign key.
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM messages_fts WHERE conversation_id = ?`, string(conv)); err != nil {
			return del, err
		}
	}
	return del, q.changed(q.wr.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, string(conv)))
}

// conversationsOfBuiltinGroups lists the channels a built-in group has been
// added to: the ones a role change, a claim or a new account can move an
// account into or out of.
func (q *queries) conversationsOfBuiltinGroups(ctx context.Context) ([]model.ConversationID, error) {
	return collectIDs[model.ConversationID](q.wr.QueryContext(ctx, `
		SELECT DISTINCT cg.conversation_id FROM channel_groups cg JOIN groups g ON g.id = cg.group_id
		WHERE g.builtin IS NOT NULL`))
}

// recomputeAccountEverywhere recomputes one account in every conversation it
// is in or could be in through a built-in group, then settles them. It is
// what a new account, a role change, a claim and a removal all need.
func (q *queries) recomputeAccountEverywhere(ctx context.Context, account model.AccountID, clearDirect bool, at model.Time) (store.MembershipResult, error) {
	var res store.MembershipResult
	convs, err := q.conversationsOfBuiltinGroups(ctx)
	if err != nil {
		return res, err
	}
	in, err := collectIDs[model.ConversationID](q.wr.QueryContext(ctx,
		`SELECT conversation_id FROM members WHERE account_id = ?`, string(account)))
	if err != nil {
		return res, err
	}
	convs = append(convs, in...)
	var setDirect *model.Level
	if clearDirect {
		none := model.Level("")
		setDirect = &none
	}
	seen := map[model.ConversationID]bool{}
	for _, c := range convs {
		if seen[c] {
			continue
		}
		seen[c] = true
		ch, err := q.recompute(ctx, c, account, setDirect, at)
		if err != nil {
			return res, err
		}
		if ch != nil {
			res.Changes = append(res.Changes, *ch)
		}
	}
	return res, q.settleAll(ctx, convs, &res)
}
