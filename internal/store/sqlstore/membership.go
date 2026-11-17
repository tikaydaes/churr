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

const memberCols = `conversation_id, account_id, level, direct_level, via_group, joined_at, read_seq, mode, show, mode_before_mute, muted_until, starred, thread_view, last_post_at, ignore_broad, may_post, visible_from`

func memberTargets(m *model.Member) []any {
	return []any{str(&m.ConversationID), str(&m.AccountID), str(&m.Level), str(&m.DirectLevel), flag(&m.ViaGroup),
		tm(&m.JoinedAt), &m.ReadSeq, str(&m.Mode), str(&m.Show), str(&m.ModeBeforeMute), tm(&m.MutedUntil), flag(&m.Starred),
		str(&m.ThreadView), tm(&m.LastPostAt), flag(&m.IgnoreBroad), flag(&m.MayPost), tm(&m.VisibleFrom)}
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
	if level != "" {
		// A guest account is a guest in every channel it is in: it reads,
		// or takes part as a member, and is never in a conversation shared
		// with other servers, however it would arrive (ruling R99).
		var role string
		var stream sql.NullString
		err := q.wr.QueryRowContext(ctx, `SELECT a.role, c.stream_id FROM accounts a, conversations c WHERE a.id = ? AND c.id = ?`,
			string(account), string(conv)).Scan(&role, &stream)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, q.mapErr(err)
		}
		if role == string(model.RoleGuest) {
			if stream.Valid {
				level, direct, via = "", "", false
			} else if level.AtLeast(model.LevelModerator) {
				level = model.LevelMember
			}
		}
	}
	if level != "" {
		// A channel ban outranks every route in (FEATURES.md 19.1): the
		// banned account has no row, however it would arrive.
		banned, err := q.channelBanned(ctx, conv, account, at)
		if err != nil {
			return nil, err
		}
		if banned {
			level, direct, via = "", "", false
		}
	}

	switch {
	case !found && level == "":
		return nil, nil

	case found && level == "":
		if err := q.noteLeft(ctx, conv, cur, at); err != nil {
			return nil, err
		}
		if err := q.deleteMemberRow(ctx, conv, account); err != nil {
			return nil, err
		}
		return &store.MemberChange{Kind: store.MemberLeft, Member: cur}, nil

	case !found:
		var name sql.NullString
		var nextSeq int64
		var kind string
		var private, knock bool
		err := q.wr.QueryRowContext(ctx, `SELECT name, next_seq, kind, private, knock FROM conversations WHERE id = ?`, string(conv)).
			Scan(&name, &nextSeq, &kind, flag(&private), flag(&knock))
		if err != nil {
			return nil, q.mapErr(err)
		}
		visibleFrom, err := q.firstJoin(ctx, conv, account, at, kind == string(model.KindChannel) && private && !knock && name.Valid, nextSeq <= 1)
		if err != nil {
			return nil, err
		}
		m := model.Member{
			ConversationID: conv, AccountID: account, Level: level, DirectLevel: direct, ViaGroup: via,
			JoinedAt: at,
			// A newcomer is not handed the whole history as unread.
			ReadSeq:     nextSeq - 1,
			Mode:        model.ModeAny,
			VisibleFrom: visibleFrom,
		}
		if name.Valid {
			m.Mode = model.ModeMentions
		}
		_, err = q.wr.ExecContext(ctx, `
			INSERT INTO members (conversation_id, account_id, level, direct_level, via_group, joined_at, read_seq, mode, starred, visible_from)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(conv), string(account), string(m.Level), id(m.DirectLevel), m.ViaGroup, int64(m.JoinedAt), m.ReadSeq, string(m.Mode), false, int64(m.VisibleFrom))
		if err != nil {
			return nil, q.mapErr(err)
		}
		q.touchMember(conv, account)
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
	q.touchMember(conv, account)
	levelChanged := cur.Level != level
	cur.Level, cur.DirectLevel, cur.ViaGroup = level, direct, via
	if !levelChanged {
		// Only the route changed. Nobody else can see that, so there is
		// nothing to announce.
		return nil, nil
	}
	return &store.MemberChange{Kind: store.MemberLevelChanged, Member: cur}, nil
}

// firstJoin returns the earliest message time a newcomer may read (ruling
// R87). In a named private channel that is not a knock channel it is the
// account's first join there, recorded now if this is that join. Someone
// who comes back within model.RejoinWindow of leaving (or being removed)
// keeps it; someone who comes back later starts again from this join, as
// any newcomer does. Everywhere else it is zero. Someone who joins before
// anything has been posted (the people a channel starts with, or those an
// import brings in before its history) has nothing kept from them, and
// keeps zero.
func (q *queries) firstJoin(ctx context.Context, conv model.ConversationID, account model.AccountID, at model.Time, private, empty bool) (model.Time, error) {
	if !private {
		return 0, nil
	}
	if empty {
		at = 0
	}
	if at == 0 && !empty {
		at = model.Now()
	}
	var first int64
	var left sql.NullInt64
	err := q.wr.QueryRowContext(ctx, `SELECT at, left_at FROM first_joins WHERE conversation_id = ? AND account_id = ?`,
		string(conv), string(account)).Scan(&first, &left)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = q.wr.ExecContext(ctx, `INSERT INTO first_joins (conversation_id, account_id, at) VALUES (?, ?, ?)`,
			string(conv), string(account), int64(at))
		return at, q.mapErr(err)
	case err != nil:
		return 0, q.mapErr(err)
	}
	if left.Valid && at.Sub(model.Time(left.Int64)) > model.RejoinWindow {
		first = int64(at) // gone too long: a newcomer again
	}
	_, err = q.wr.ExecContext(ctx, `UPDATE first_joins SET at = ?, left_at = NULL WHERE conversation_id = ? AND account_id = ?`,
		first, string(conv), string(account))
	return model.Time(first), q.mapErr(err)
}

// noteLeft records, in a named private channel that is not a knock
// channel, when a member went, however they went, with the visibility they
// had: coming back within model.RejoinWindow restores it (ruling R87).
func (q *queries) noteLeft(ctx context.Context, conv model.ConversationID, m model.Member, at model.Time) error {
	var kind string
	var private, knock bool
	var name sql.NullString
	err := q.wr.QueryRowContext(ctx, `SELECT kind, private, knock, name FROM conversations WHERE id = ?`, string(conv)).
		Scan(&kind, flag(&private), flag(&knock), &name)
	if err != nil {
		return q.mapErr(err)
	}
	if kind != string(model.KindChannel) || !private || knock || !name.Valid {
		return nil
	}
	if at == 0 {
		at = model.Now()
	}
	_, err = q.wr.ExecContext(ctx, `
		INSERT INTO first_joins (conversation_id, account_id, at, left_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (conversation_id, account_id) DO UPDATE SET left_at = excluded.left_at`,
		string(conv), string(m.AccountID), int64(m.VisibleFrom), int64(at))
	return q.mapErr(err)
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
	q.touchMember(conv, account)
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
// manager, unless a hub owns it; a private or ad-hoc channel, or a sealed chat, with nobody left
// is deleted with its content; an ad-hoc channel's member key follows its
// members.
func (q *queries) settle(ctx context.Context, conv model.ConversationID, res *store.MembershipResult) error {
	var (
		kind      string
		private   bool
		name      sql.NullString
		memberKey sql.NullString
		hubOwned  bool
	)
	err := q.wr.QueryRowContext(ctx, `
		SELECT kind, private, name, member_key,
			EXISTS (SELECT 1 FROM stream_channels sc WHERE sc.stream_id = conversations.stream_id)
		FROM conversations WHERE id = ?`, string(conv)).
		Scan(&kind, flag(&private), &name, &memberKey, flag(&hubOwned))
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

	if kind == model.KindChannel && managers == 0 && !hubOwned {
		// The shadow of someone on another server is never a manager
		// here (docs/PERMISSIONS.md decision P19), nor is a guest (ruling
		// R99). A channel a hub owns has the managers on the hub's list
		// and nobody is promoted in its copy (section 14.1).
		var account model.AccountID
		err := q.wr.QueryRowContext(ctx, `
			SELECT m.account_id FROM members m JOIN accounts a ON a.id = m.account_id
			WHERE m.conversation_id = ? AND a.role NOT IN ('remote', 'guest')
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
//
// The members are those shown to its members: the ghosts a member is shown
// count (ghost_members, rulings R77 and R78), so that a chat that waits on
// a request, or one with someone who blocked the person who started it, is
// found again as the chat with those people. A closed chat holds no key, nor
// a declined one: asking again later is a new request.
func (q *queries) refreshMemberKey(ctx context.Context, conv model.ConversationID) error {
	var closed sql.NullInt64
	var state sql.NullString
	if err := q.wr.QueryRowContext(ctx, `SELECT closed_at, request_state FROM conversations WHERE id = ?`, string(conv)).Scan(&closed, &state); err != nil {
		return q.mapErr(err)
	}
	if closed.Valid || state.String == model.ChatDeclined {
		_, err := q.wr.ExecContext(ctx, `UPDATE conversations SET member_key = NULL WHERE id = ?`, string(conv))
		return q.mapErr(err)
	}
	ids, err := collectIDs[model.AccountID](q.wr.QueryContext(ctx, `
		SELECT account_id FROM members WHERE conversation_id = ?
		UNION
		SELECT g.account_id FROM ghost_members g
		WHERE g.conversation_id = ? AND EXISTS (SELECT 1 FROM members m WHERE m.conversation_id = g.conversation_id AND m.account_id = g.shown_to)`,
		string(conv), string(conv)))
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
	q.touchRoster(conv)
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
