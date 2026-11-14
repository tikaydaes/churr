package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type groups struct{ *queries }

// Groups returns the groups area.
func (q *queries) Groups() store.Groups { return groups{q} }

// groupCols lists the columns of a groups row with the alias g, how many
// accounts it contains, and whether one given account is among them. It
// takes that account's id three times, as groupContains does.
const groupCols = `g.id, g.handle, g.name, g.description, g.color, g.open, g.builtin, g.provider_id, g.provider_group, g.created_at,
	CASE
		WHEN g.builtin = 'everyone' THEN (SELECT count(*) FROM accounts a WHERE ` + inEveryone + `)
		WHEN g.builtin = 'admins' THEN (SELECT count(*) FROM accounts a WHERE ` + inAdmins + `)
		ELSE (SELECT count(*) FROM group_members gm WHERE gm.group_id = g.id)
	END,
	CASE WHEN ` + groupContains + ` THEN 1 ELSE 0 END`

func scanGroup(s scanner) (model.Group, error) {
	var g model.Group
	var isMember int64
	err := s.Scan(str(&g.ID), &g.Handle, &g.Name, &g.Description, str(&g.Color), flag(&g.Open), str(&g.Builtin),
		str(&g.ProviderID), str(&g.ProviderGroup), tm(&g.CreatedAt), &g.MemberCount, &isMember)
	g.IsMember = isMember != 0
	return g, err
}

func (g groups) EnsureBuiltin(ctx context.Context) error {
	return g.atomic(ctx, func(q *queries) error {
		for _, b := range []struct{ handle, name string }{
			{model.GroupEveryone, "Everyone"},
			{model.GroupAdmins, "Admins"},
		} {
			_, err := q.wr.ExecContext(ctx, `
				INSERT INTO groups (id, handle, name, description, open, builtin, created_at)
				VALUES (?, ?, ?, '', ?, ?, ?) ON CONFLICT (builtin) DO NOTHING`,
				model.NewID[string](), b.handle, b.name, false, b.handle, int64(model.Now()))
			if err != nil {
				return q.mapErr(err)
			}
		}
		return nil
	})
}

// handleFree checks that a handle is not reserved and not a local username.
// The unique index on handles covers other groups.
func (q *queries) handleFree(ctx context.Context, handle string) error {
	if model.ReservedName(handle) {
		return model.ErrNameTaken
	}
	var one int
	err := q.wr.QueryRowContext(ctx, `SELECT 1 FROM accounts WHERE username = ? AND server_id IS NULL`, handle).Scan(&one)
	if err == nil {
		return model.ErrNameTaken
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func (g groups) Create(ctx context.Context, grp *model.Group) error {
	if grp.ID == "" {
		grp.ID = model.NewID[model.GroupID]()
	}
	if grp.CreatedAt.IsZero() {
		grp.CreatedAt = model.Now()
	}
	grp.Builtin = ""
	return g.atomic(ctx, func(q *queries) error {
		if err := q.handleFree(ctx, grp.Handle); err != nil {
			return err
		}
		_, err := q.wr.ExecContext(ctx, `
			INSERT INTO groups (id, handle, name, description, color, open, provider_id, provider_group, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(grp.ID), grp.Handle, grp.Name, grp.Description, id(grp.Color), grp.Open, id(grp.ProviderID),
			id(grp.ProviderGroup), int64(grp.CreatedAt))
		if err != nil && q.db.dialect.ForeignKeyViolation(err) {
			// The provider the group is to be filled from does not exist.
			return model.ErrNotFound.With("There is no such sign-in provider.")
		}
		return q.taken(err)
	})
}

func (g groups) one(ctx context.Context, ex execer, where string, viewer model.AccountID, arg any) (model.Group, error) {
	// An empty viewer is passed as NULL: PostgreSQL will not take "" for
	// an identifier, and NULL is in no group.
	v := id(viewer)
	out, err := scanGroup(ex.QueryRowContext(ctx, `SELECT `+groupCols+` FROM groups g WHERE `+where, v, v, v, arg))
	return out, g.mapErr(err)
}

func (g groups) Get(ctx context.Context, group model.GroupID, viewer model.AccountID) (model.Group, error) {
	return g.one(ctx, g.rd, "g.id = ?", viewer, string(group))
}

func (g groups) ByHandle(ctx context.Context, handle string) (model.Group, error) {
	return g.one(ctx, g.rd, "g.handle = ?", "", handle)
}

func (g groups) Builtin(ctx context.Context, name string) (model.Group, error) {
	return g.one(ctx, g.rd, "g.builtin = ?", "", name)
}

func (g groups) List(ctx context.Context, viewer model.AccountID) ([]model.Group, error) {
	v := id(viewer)
	rows, err := g.rd.QueryContext(ctx, `SELECT `+groupCols+` FROM groups g ORDER BY g.handle`, v, v, v)
	return collect(rows, err, scanGroup)
}

func (g groups) Update(ctx context.Context, group model.GroupID, fn func(*model.Group) error) (model.Group, error) {
	var out model.Group
	err := g.atomic(ctx, func(q *queries) error {
		cur, err := groups{q}.one(ctx, q.wr, "g.id = ?"+q.forUpdate(), "", string(group))
		if err != nil {
			return err
		}
		if cur.Builtin != "" {
			return model.ErrConflict.With("A built-in group cannot be changed.")
		}
		before := cur.Handle
		if err := fn(&cur); err != nil {
			return err
		}
		if cur.Handle != before {
			if err := q.handleFree(ctx, cur.Handle); err != nil {
				return err
			}
		}
		_, err = q.wr.ExecContext(ctx, `
			UPDATE groups SET handle = ?, name = ?, description = ?, color = ?, open = ?, provider_id = ?, provider_group = ?
			WHERE id = ?`,
			cur.Handle, cur.Name, cur.Description, id(cur.Color), cur.Open, id(cur.ProviderID), id(cur.ProviderGroup), string(group))
		out = cur
		if err != nil && q.db.dialect.ForeignKeyViolation(err) {
			// The provider the group is to be filled from does not exist.
			return model.ErrNotFound.With("There is no such sign-in provider.")
		}
		return q.taken(err)
	})
	return out, err
}

// groupMemberIDs returns who is in a group, working the built-in ones out
// from roles.
func (q *queries) groupMemberIDs(ctx context.Context, group model.GroupID) ([]model.AccountID, error) {
	var builtin sql.NullString
	err := q.rd.QueryRowContext(ctx, `SELECT builtin FROM groups WHERE id = ?`, string(group)).Scan(&builtin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // deleted in this transaction; nobody is in it
	}
	if err != nil {
		return nil, err
	}
	switch builtin.String {
	case model.GroupEveryone:
		return collectIDs[model.AccountID](q.rd.QueryContext(ctx, `SELECT a.id FROM accounts a WHERE `+inEveryone+` ORDER BY a.id`))
	case model.GroupAdmins:
		return collectIDs[model.AccountID](q.rd.QueryContext(ctx, `SELECT a.id FROM accounts a WHERE `+inAdmins+` ORDER BY a.id`))
	}
	return collectIDs[model.AccountID](q.rd.QueryContext(ctx,
		`SELECT account_id FROM group_members WHERE group_id = ? ORDER BY added_at, account_id`, string(group)))
}

// groupChannels returns the conversations a group has been added to.
func (q *queries) groupChannels(ctx context.Context, group model.GroupID) ([]model.ConversationID, error) {
	return collectIDs[model.ConversationID](q.wr.QueryContext(ctx,
		`SELECT conversation_id FROM channel_groups WHERE group_id = ?`, string(group)))
}

// ordinaryGroup checks that a group exists and is not built in.
func (q *queries) ordinaryGroup(ctx context.Context, group model.GroupID) error {
	var builtin sql.NullString
	err := q.wr.QueryRowContext(ctx, `SELECT builtin FROM groups WHERE id = ?`+q.forUpdate(), string(group)).Scan(&builtin)
	if err != nil {
		return q.mapErr(err)
	}
	if builtin.Valid {
		return model.ErrConflict.With("The built-in groups follow roles and cannot be changed.")
	}
	return nil
}

func (g groups) Delete(ctx context.Context, group model.GroupID) (store.MembershipResult, error) {
	var res store.MembershipResult
	err := g.atomic(ctx, func(q *queries) error {
		if err := q.ordinaryGroup(ctx, group); err != nil {
			return err
		}
		ids, err := q.groupMemberIDs(ctx, group)
		if err != nil {
			return err
		}
		convs, err := q.groupChannels(ctx, group)
		if err != nil {
			return err
		}
		// group_members and channel_groups go by cascade; the member
		// rows the group alone held up are then worked out again.
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM groups WHERE id = ?`, string(group)); err != nil {
			return err
		}
		for _, c := range convs {
			if err := q.recomputeMany(ctx, c, ids, 0, &res); err != nil {
				return err
			}
		}
		return q.settleAll(ctx, convs, &res)
	})
	if err != nil {
		return store.MembershipResult{}, err
	}
	return res, nil
}

// afterGroupChange recomputes one account in every channel a group is in.
func (q *queries) afterGroupChange(ctx context.Context, group model.GroupID, account model.AccountID, at model.Time, res *store.MembershipResult) error {
	convs, err := q.groupChannels(ctx, group)
	if err != nil {
		return err
	}
	for _, c := range convs {
		if err := q.recomputeMany(ctx, c, []model.AccountID{account}, at, res); err != nil {
			return err
		}
	}
	return q.settleAll(ctx, convs, res)
}

func (g groups) AddMember(ctx context.Context, group model.GroupID, account, by model.AccountID, at model.Time) (store.MembershipResult, error) {
	var res store.MembershipResult
	err := g.atomic(ctx, func(q *queries) error {
		if err := q.ordinaryGroup(ctx, group); err != nil {
			return err
		}
		var one int
		err := q.wr.QueryRowContext(ctx, `
			SELECT 1 FROM accounts a WHERE a.id = ? AND a.server_id IS NULL AND a.claimed_at IS NOT NULL AND a.removed_at IS NULL`,
			string(account)).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrConflict.With("Only a local, active account can be in a group.")
		}
		if err != nil {
			return err
		}
		n, err := q.affected(q.wr.ExecContext(ctx, `
			INSERT INTO group_members (group_id, account_id, added_by, added_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (group_id, account_id) DO NOTHING`, string(group), string(account), id(by), int64(at)))
		if err != nil {
			return err
		}
		if n == 0 {
			return model.ErrAlreadyExists.With("That account is already in the group.")
		}
		return q.afterGroupChange(ctx, group, account, at, &res)
	})
	if err != nil {
		return store.MembershipResult{}, err
	}
	return res, nil
}

func (g groups) RemoveMember(ctx context.Context, group model.GroupID, account model.AccountID) (store.MembershipResult, error) {
	var res store.MembershipResult
	err := g.atomic(ctx, func(q *queries) error {
		if err := q.ordinaryGroup(ctx, group); err != nil {
			return err
		}
		err := q.changed(q.wr.ExecContext(ctx,
			`DELETE FROM group_members WHERE group_id = ? AND account_id = ?`, string(group), string(account)))
		if err != nil {
			return err
		}
		return q.afterGroupChange(ctx, group, account, 0, &res)
	})
	if err != nil {
		return store.MembershipResult{}, err
	}
	return res, nil
}

func (g groups) MemberIDs(ctx context.Context, group model.GroupID) ([]model.AccountID, error) {
	return g.groupMemberIDs(ctx, group)
}

func (g groups) IsMember(ctx context.Context, group model.GroupID, account model.AccountID) (bool, error) {
	var one int
	a := string(account)
	err := g.rd.QueryRowContext(ctx, `SELECT 1 FROM groups g WHERE g.id = ? AND `+groupContains, string(group), a, a, a).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (g groups) OfAccount(ctx context.Context, account model.AccountID) ([]model.GroupID, error) {
	return collectIDs[model.GroupID](g.rd.QueryContext(ctx, `
		SELECT gm.group_id FROM group_members gm JOIN groups g ON g.id = gm.group_id
		WHERE gm.account_id = ? ORDER BY g.handle`, string(account)))
}

func (g groups) Channels(ctx context.Context, group model.GroupID) ([]model.ChannelGroup, error) {
	rows, err := g.rd.QueryContext(ctx, `
		SELECT conversation_id, group_id, level, added_by, added_at FROM channel_groups
		WHERE group_id = ? ORDER BY added_at, conversation_id`, string(group))
	return collect(rows, err, scanChannelGroup)
}

func (g groups) SeenBy(ctx context.Context, group model.GroupID, account model.AccountID) (bool, error) {
	in, err := g.IsMember(ctx, group, account)
	if err != nil || in {
		return in, err
	}
	var one int
	err = g.rd.QueryRowContext(ctx, `
		SELECT 1 FROM channel_groups cg JOIN members m ON m.conversation_id = cg.conversation_id
		WHERE cg.group_id = ? AND m.account_id = ? LIMIT 1`, string(group), string(account)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
