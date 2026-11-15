package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type accounts struct{ *queries }

// Accounts returns the accounts area.
func (q *queries) Accounts() store.Accounts { return accounts{q} }

// accountCols lists the columns of an accounts row with the alias a, and the
// name of its home server (NULL for a local account) from the alias sv.
const accountCols = `a.id, a.username, a.server_id, sv.name, a.role, a.display_name, a.avatar_file_id, a.title, a.pronouns,
	a.time_zone, a.webauthn_handle, a.created_at, a.claimed_at, a.removed_at, a.removed_by, a.profile_version,
	a.status_emoji, a.status_text, a.status_until, a.status_dnd, a.status_away,
	a.availability, a.availability_until, a.availability_by, a.away_until, a.dnd_until,
	a.quiet_start, a.quiet_end, a.quiet_days, a.quiet_skip_until, a.allow_notify_anyway, a.prefs`

const accountFrom = ` FROM accounts a LEFT JOIN servers sv ON sv.id = a.server_id `

func scanAccount(s scanner) (model.Account, error) {
	var a model.Account
	err := s.Scan(str(&a.ID), &a.Username, str(&a.ServerID), str(&a.ServerName), str(&a.Role), &a.DisplayName,
		str(&a.AvatarFileID), str(&a.Title), str(&a.Pronouns), str(&a.TimeZone), blob(&a.WebAuthnHandle),
		tm(&a.CreatedAt), tm(&a.ClaimedAt), tm(&a.RemovedAt), str(&a.RemovedBy), &a.ProfileVersion,
		str(&a.StatusEmoji), str(&a.StatusText), tm(&a.StatusUntil), flag(&a.StatusDND), flag(&a.StatusAway),
		str(&a.AvailabilityIs), tm(&a.AvailabilityEnd), str(&a.AvailabilityBy), tmPtr(&a.AwayUntil), tmPtr(&a.DNDUntil),
		str(&a.QuietStart), str(&a.QuietEnd), str(&a.QuietDays), tm(&a.QuietSkipUntil), flag(&a.AllowNotifyAnyway), js(&a.PrefsJSON))
	return a, err
}

// usernameFree checks the half of the namespace rule the database cannot:
// a local username is never a group handle or a reserved name. The unique
// index on local usernames covers the other half.
func (q *queries) usernameFree(ctx context.Context, username string) error {
	if model.ReservedName(username) {
		return model.ErrNameTaken
	}
	var one int
	err := q.wr.QueryRowContext(ctx, `SELECT 1 FROM groups WHERE handle = ?`, username).Scan(&one)
	if err == nil {
		return model.ErrNameTaken
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func (a accounts) insert(ctx context.Context, q *queries, acc *model.Account, claimed bool) error {
	if acc.ID == "" {
		acc.ID = model.NewID[model.AccountID]()
	}
	if acc.CreatedAt.IsZero() {
		acc.CreatedAt = model.Now()
	}
	if claimed && acc.ClaimedAt.IsZero() {
		acc.ClaimedAt = acc.CreatedAt
	}
	if !claimed {
		acc.ClaimedAt = 0
	}
	if acc.Local() {
		if err := q.usernameFree(ctx, acc.Username); err != nil {
			return err
		}
	}
	if acc.Role == model.RoleOwner {
		var n int
		if err := q.wr.QueryRowContext(ctx, `SELECT count(*) FROM accounts WHERE role = 'owner'`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return model.ErrConflict.With("This server already has an owner.")
		}
	}
	_, err := q.wr.ExecContext(ctx, `
		INSERT INTO accounts (id, username, server_id, role, display_name, avatar_file_id, title, pronouns, time_zone,
			webauthn_handle, created_at, claimed_at, profile_version, allow_notify_anyway, prefs)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(acc.ID), acc.Username, id(acc.ServerID), string(acc.Role), acc.DisplayName, id(acc.AvatarFileID),
		id(acc.Title), id(acc.Pronouns), id(acc.TimeZone), bin(acc.WebAuthnHandle), int64(acc.CreatedAt), ms(acc.ClaimedAt),
		acc.ProfileVersion, true, jsonObject(acc.PrefsJSON))
	acc.AllowNotifyAnyway = true
	q.touchAccount(acc.ID)
	return q.taken(err)
}

func (a accounts) Create(ctx context.Context, acc *model.Account) (store.MembershipResult, error) {
	var res store.MembershipResult
	err := a.atomic(ctx, func(q *queries) error {
		if err := a.insert(ctx, q, acc, true); err != nil {
			return err
		}
		var err error
		res, err = q.recomputeAccountEverywhere(ctx, acc.ID, false, acc.CreatedAt)
		return err
	})
	return res, err
}

func (a accounts) CreateUnclaimed(ctx context.Context, acc *model.Account) error {
	return a.atomic(ctx, func(q *queries) error { return a.insert(ctx, q, acc, false) })
}

func (a accounts) Claim(ctx context.Context, account model.AccountID, at model.Time) (store.MembershipResult, error) {
	var res store.MembershipResult
	err := a.atomic(ctx, func(q *queries) error {
		err := q.changed(q.wr.ExecContext(ctx,
			`UPDATE accounts SET claimed_at = ? WHERE id = ? AND claimed_at IS NULL AND removed_at IS NULL`, int64(at), string(account)))
		if err != nil {
			return err
		}
		q.touchAccount(account)
		// Claiming also ends the importer's hold on the account.
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM import_claims WHERE account_id = ?`, string(account)); err != nil {
			return err
		}
		res, err = q.recomputeAccountEverywhere(ctx, account, false, at)
		return err
	})
	return res, err
}

func (a accounts) Get(ctx context.Context, account model.AccountID) (model.Account, error) {
	read := func() (model.Account, error) {
		acc, err := scanAccount(a.rd.QueryRowContext(ctx, `SELECT `+accountCols+accountFrom+`WHERE a.id = ?`, string(account)))
		return acc, a.mapErr(err)
	}
	if a.inTx {
		// A transaction sees its own writes, which nobody else may.
		return read()
	}
	// Every request reads the account it acts as, so the rows of this
	// server's own accounts are kept in memory (kept.go); touchAccount
	// marks them.
	c := &a.db.roster
	acc, seq, ok := c.keptAccount(account)
	if ok {
		return acc, nil
	}
	acc, err := read()
	if err == nil {
		c.keepAccount(acc, seq)
	}
	return acc, err
}

func (a accounts) GetMany(ctx context.Context, ids []model.AccountID) ([]model.Account, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := a.rd.QueryContext(ctx, `SELECT `+accountCols+accountFrom+`WHERE a.id IN (`+marks(len(ids))+`)`, anys(ids)...)
	return collect(rows, err, scanAccount)
}

func (a accounts) ByUsername(ctx context.Context, username string) (model.Account, error) {
	acc, err := scanAccount(a.rd.QueryRowContext(ctx,
		`SELECT `+accountCols+accountFrom+`WHERE a.username = ? AND a.server_id IS NULL`, username))
	return acc, a.mapErr(err)
}

func (a accounts) ByWebAuthnHandle(ctx context.Context, handle []byte) (model.Account, error) {
	if len(handle) == 0 {
		return model.Account{}, model.ErrNotFound
	}
	acc, err := scanAccount(a.rd.QueryRowContext(ctx, `SELECT `+accountCols+accountFrom+`WHERE a.webauthn_handle = ?`, handle))
	return acc, a.mapErr(err)
}

func (a accounts) Owner(ctx context.Context) (model.Account, bool, error) {
	acc, err := scanAccount(a.rd.QueryRowContext(ctx, `SELECT `+accountCols+accountFrom+`WHERE a.role = 'owner'`))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Account{}, false, nil
	}
	return acc, err == nil, err
}

// accountCursor is the sort key of the last row of a page.
type accountCursor struct {
	Username string `json:"u"`
	ID       string `json:"i"`
}

func (a accounts) List(ctx context.Context, f store.AccountFilter, p store.Page) ([]model.Account, string, error) {
	where := []string{"1 = 1"}
	var args []any
	switch f.State {
	case "active":
		where = append(where, "a.removed_at IS NULL AND (a.claimed_at IS NOT NULL OR a.server_id IS NOT NULL)")
	case "removed":
		where = append(where, "a.removed_at IS NOT NULL")
	case "unclaimed":
		where = append(where, "a.removed_at IS NULL AND a.claimed_at IS NULL AND a.server_id IS NULL")
	default:
		if !f.IncludeRemoved {
			where = append(where, "a.removed_at IS NULL")
		}
	}
	if f.Query != "" {
		where = append(where, "(LOWER(a.username) LIKE ?"+likeEscape+" OR LOWER(a.display_name) LIKE ?"+likeEscape+")")
		args = append(args, like(f.Query), like(f.Query))
	}
	if f.Role != "" {
		where = append(where, "a.role = ?")
		args = append(args, string(f.Role))
	}
	if f.GroupID != "" {
		where = append(where, `EXISTS (SELECT 1 FROM groups g WHERE g.id = ? AND (
			(g.builtin IS NULL AND EXISTS (SELECT 1 FROM group_members gm WHERE gm.group_id = g.id AND gm.account_id = a.id))
			OR (g.builtin = 'everyone' AND `+inEveryone+`)
			OR (g.builtin = 'admins' AND `+inAdmins+`)))`)
		args = append(args, string(f.GroupID))
	}
	if len(f.IDs) > 0 {
		where = append(where, "a.id IN ("+marks(len(f.IDs))+")")
		args = append(args, anys(f.IDs)...)
	}
	const shares = `EXISTS (SELECT 1 FROM members m1 JOIN members m2 ON m2.conversation_id = m1.conversation_id
		WHERE m1.account_id = a.id AND m2.account_id = ?)`
	if f.SharedWith != "" {
		where = append(where, "(a.id = ? OR "+shares+")")
		args = append(args, string(f.SharedWith), string(f.SharedWith))
	}
	if f.LocalOrSharedWith != "" {
		where = append(where, "(a.server_id IS NULL OR "+shares+")")
		args = append(args, string(f.LocalOrSharedWith))
	}
	if p.Cursor != "" {
		var c accountCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		where = append(where, "(a.username > ? OR (a.username = ? AND a.id > ?))")
		args = append(args, c.Username, c.Username, c.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := a.rd.QueryContext(ctx, `SELECT `+accountCols+accountFrom+`WHERE `+strings.Join(where, " AND ")+
		` ORDER BY a.username, a.id LIMIT ?`, args...)
	out, err := collect(rows, err, scanAccount)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		next = store.EncodeCursor(accountCursor{Username: last.Username, ID: string(last.ID)})
	}
	return out, next, nil
}

func (a accounts) Update(ctx context.Context, account model.AccountID, fn func(*model.Account) error) (model.Account, error) {
	var out model.Account
	err := a.atomic(ctx, func(q *queries) error {
		acc, err := scanAccount(q.wr.QueryRowContext(ctx,
			`SELECT `+accountCols+accountFrom+`WHERE a.id = ?`+q.forUpdateOf("a"), string(account)))
		if err != nil {
			return q.mapErr(err)
		}
		before := acc.Username
		if err := fn(&acc); err != nil {
			return err
		}
		if acc.Username != before && acc.Local() {
			if err := q.usernameFree(ctx, acc.Username); err != nil {
				return err
			}
		}
		_, err = q.wr.ExecContext(ctx, `
			UPDATE accounts SET username = ?, display_name = ?, avatar_file_id = ?, title = ?, pronouns = ?, time_zone = ?,
				webauthn_handle = ?, profile_version = ?,
				status_emoji = ?, status_text = ?, status_until = ?, status_dnd = ?, status_away = ?,
				availability = ?, availability_until = ?, availability_by = ?, away_until = ?, dnd_until = ?,
				quiet_start = ?, quiet_end = ?, quiet_days = ?, quiet_skip_until = ?, allow_notify_anyway = ?, prefs = ?
			WHERE id = ?`,
			acc.Username, acc.DisplayName, id(acc.AvatarFileID), id(acc.Title), id(acc.Pronouns), id(acc.TimeZone),
			bin(acc.WebAuthnHandle), acc.ProfileVersion,
			id(acc.StatusEmoji), id(acc.StatusText), ms(acc.StatusUntil), acc.StatusDND, acc.StatusAway,
			id(acc.AvailabilityIs), ms(acc.AvailabilityEnd), id(acc.AvailabilityBy), msPtr(acc.AwayUntil), msPtr(acc.DNDUntil),
			id(acc.QuietStart), id(acc.QuietEnd), id(acc.QuietDays), ms(acc.QuietSkipUntil), acc.AllowNotifyAnyway, jsonObject(acc.PrefsJSON),
			string(account))
		out = acc
		q.touchAccount(account)
		return q.taken(err)
	})
	return out, err
}

// forUpdateOf is forUpdate for a query that joins: PostgreSQL will not lock
// the nullable side of an outer join, so the lock names the one table meant.
func (q *queries) forUpdateOf(alias string) string {
	if q.pg() {
		return " FOR UPDATE OF " + alias
	}
	return ""
}

func (a accounts) SetRole(ctx context.Context, account model.AccountID, role model.Role) (model.Account, store.MembershipResult, error) {
	var (
		out model.Account
		res store.MembershipResult
	)
	if role != model.RoleAdmin && role != model.RoleMember && role != model.RoleGuest {
		return out, res, model.ErrConflict.With("A role can be changed to admin, member or guest only.")
	}
	err := a.atomic(ctx, func(q *queries) error {
		acc, err := scanAccount(q.wr.QueryRowContext(ctx,
			`SELECT `+accountCols+accountFrom+`WHERE a.id = ?`+q.forUpdateOf("a"), string(account)))
		if err != nil {
			return q.mapErr(err)
		}
		switch acc.Role {
		case model.RoleAdmin, model.RoleMember, model.RoleGuest:
		default:
			return model.ErrConflict.With("This account's role cannot be changed this way.")
		}
		if _, err := q.wr.ExecContext(ctx, `UPDATE accounts SET role = ? WHERE id = ?`, string(role), string(account)); err != nil {
			return err
		}
		acc.Role = role
		out = acc
		q.touchAccount(account)
		res, err = q.recomputeAccountEverywhere(ctx, account, false, model.Now())
		return err
	})
	return out, res, err
}

func (a accounts) TransferOwnership(ctx context.Context, to model.AccountID) (store.MembershipResult, error) {
	var res store.MembershipResult
	err := a.atomic(ctx, func(q *queries) error {
		var owner model.AccountID
		if err := q.wr.QueryRowContext(ctx, `SELECT id FROM accounts WHERE role = 'owner'`+q.forUpdate()).Scan(str(&owner)); err != nil {
			return q.mapErr(err)
		}
		if owner == to {
			return nil
		}
		// The old owner steps down first, so there is never a moment
		// with two.
		if _, err := q.wr.ExecContext(ctx, `UPDATE accounts SET role = 'admin' WHERE id = ?`, string(owner)); err != nil {
			return err
		}
		err := q.changed(q.wr.ExecContext(ctx, `
			UPDATE accounts SET role = 'owner'
			WHERE id = ? AND server_id IS NULL AND claimed_at IS NOT NULL AND removed_at IS NULL AND role IN ('admin', 'member')`, string(to)))
		if err != nil {
			return err
		}
		q.touchAccount(owner, to)
		now := model.Now()
		for _, acc := range []model.AccountID{owner, to} {
			r, err := q.recomputeAccountEverywhere(ctx, acc, false, now)
			if err != nil {
				return err
			}
			res.Merge(r)
		}
		return nil
	})
	return res, err
}

func (a accounts) Remove(ctx context.Context, account, by model.AccountID, at model.Time) (store.RemovedAccount, error) {
	var out store.RemovedAccount
	err := a.atomic(ctx, func(q *queries) error {
		acc, err := scanAccount(q.wr.QueryRowContext(ctx,
			`SELECT `+accountCols+accountFrom+`WHERE a.id = ?`+q.forUpdateOf("a"), string(account)))
		if err != nil {
			return q.mapErr(err)
		}
		if acc.Removed() {
			return model.ErrConflict.With("This account has already been removed.")
		}
		out.SessionIDs, err = collectIDs[model.SessionID](q.wr.QueryContext(ctx,
			`SELECT id FROM sessions WHERE account_id = ?`, string(account)))
		if err != nil {
			return err
		}

		// The row stays, with its username, display name, role and
		// created_at; everything personal is cleared.
		q.touchAccount(account)
		_, err = q.wr.ExecContext(ctx, `
			UPDATE accounts SET removed_at = ?, removed_by = ?, avatar_file_id = NULL, title = NULL, pronouns = NULL,
				time_zone = NULL, webauthn_handle = NULL,
				status_emoji = NULL, status_text = NULL, status_until = NULL, status_dnd = ?, status_away = ?,
				availability = NULL, availability_until = NULL, availability_by = NULL, away_until = NULL, dnd_until = NULL,
				quiet_start = NULL, quiet_end = NULL, quiet_days = NULL, quiet_skip_until = NULL, prefs = '{}'
			WHERE id = ?`, int64(at), id(by), false, false, string(account))
		if err != nil {
			return err
		}

		// Its sessions go below, and which ones is not asked.
		q.touchSessions()
		// Everything docs/SCHEMA.md section 14.3 lists.
		for _, stmt := range []string{
			`DELETE FROM devices WHERE account_id = ?`,
			`DELETE FROM sessions WHERE account_id = ?`,
			`DELETE FROM passkeys WHERE account_id = ?`,
			`DELETE FROM totp WHERE account_id = ?`,
			`DELETE FROM recovery_codes WHERE account_id = ?`,
			`DELETE FROM recovery_links WHERE account_id = ?`,
			`DELETE FROM oauth_codes WHERE account_id = ?`,
			`DELETE FROM identities WHERE account_id = ?`,
			`DELETE FROM import_claims WHERE account_id = ?`,
			`DELETE FROM device_lists WHERE account_id = ?`,
			`DELETE FROM backup_keys WHERE account_id = ?`,
			`DELETE FROM key_backups WHERE account_id = ?`,
			`DELETE FROM account_records WHERE account_id = ?`,
			`DELETE FROM unlock_requests WHERE account_id = ?`,
			`DELETE FROM notify_anyway WHERE account_id = ?`,
			`DELETE FROM group_members WHERE account_id = ?`,
			`DELETE FROM thread_states WHERE account_id = ?`,
			`DELETE FROM mentions WHERE account_id = ?`,
			`DELETE FROM bookmarks WHERE account_id = ?`,
			`DELETE FROM timeouts WHERE account_id = ?`,
			`DELETE FROM scheduled WHERE account_id = ?`,
			`DELETE FROM status_presets WHERE owner_id = ?`,
			`DELETE FROM saved_replies WHERE owner_id = ?`,
			`DELETE FROM themes WHERE owner_id = ?`,
			`DELETE FROM commands WHERE bot_id = ?`,
			`UPDATE webhooks SET state = 'revoked', token_hash = NULL, signing_secret = NULL WHERE bot_id = ?`,
		} {
			if _, err := q.wr.ExecContext(ctx, stmt, string(account)); err != nil {
				return q.mapErr(err)
			}
		}
		q.touchBlocks()
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM blocks WHERE account_id = ? OR blocked_id = ?`, string(account), string(account)); err != nil {
			return err
		}
		_, err = q.wr.ExecContext(ctx, `
			UPDATE invites SET revoked_at = ?
			WHERE created_by = ? AND revoked_at IS NULL AND (max_uses = 0 OR uses < max_uses)`, int64(at), string(account))
		if err != nil {
			return err
		}

		// The avatar is a file of its own in the library conversation;
		// it goes with the profile.
		if acc.AvatarFileID != "" {
			var f model.File
			err := q.wr.QueryRowContext(ctx, `DELETE FROM files WHERE id = ? AND purpose = 'avatar' RETURNING blob_key, thumb_key, poster_key`,
				string(acc.AvatarFileID)).Scan(str(&f.BlobKey), str(&f.ThumbKey), str(&f.PosterKey))
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			out.BlobKeys = store.BlobKeys(f)
		}

		// With removed_at set the account is in no built-in group, and
		// its group_members rows are gone, so clearing the direct level
		// takes it out of every conversation. The last-manager and
		// last-member rules follow.
		out.MembershipResult, err = q.recomputeAccountEverywhere(ctx, account, true, at)
		if err != nil {
			return err
		}
		acc.RemovedAt, acc.RemovedBy = at, by
		acc.AvatarFileID, acc.Title, acc.Pronouns, acc.TimeZone, acc.WebAuthnHandle = "", "", "", "", nil
		acc.StatusEmoji, acc.StatusText, acc.StatusUntil, acc.StatusDND, acc.StatusAway = "", "", 0, false, false
		acc.AvailabilityIs, acc.AvailabilityEnd, acc.AvailabilityBy, acc.AwayUntil, acc.DNDUntil = "", 0, "", nil, nil
		acc.QuietStart, acc.QuietEnd, acc.QuietDays, acc.QuietSkipUntil, acc.PrefsJSON = "", "", "", 0, nil
		out.Account = acc
		return nil
	})
	return out, err
}

func (a accounts) SharesConversation(ctx context.Context, x, y model.AccountID) (bool, error) {
	if x == y {
		return true, nil
	}
	var one int
	err := a.rd.QueryRowContext(ctx, `
		SELECT 1 FROM members m1 JOIN members m2 ON m2.conversation_id = m1.conversation_id
		WHERE m1.account_id = ? AND m2.account_id = ? LIMIT 1`, string(x), string(y)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
