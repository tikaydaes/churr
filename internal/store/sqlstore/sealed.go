package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type sealed struct{ *queries }

// Sealed returns the sealed-chat area.
func (q *queries) Sealed() store.Sealed { return sealed{q} }

// ---------------------------------------------------------- device lists

func scanDeviceList(s scanner) (store.SignedDeviceList, error) {
	var l store.SignedDeviceList
	err := s.Scan(str(&l.AccountID), &l.Version, blob(&l.Signed), tm(&l.CreatedAt))
	return l, err
}

func (s sealed) PutDeviceList(ctx context.Context, l store.SignedDeviceList) error {
	if l.CreatedAt.IsZero() {
		l.CreatedAt = model.Now()
	}
	// One statement, so two devices publishing the same version at once
	// cannot both pass the check: the second finds the first's row, or
	// meets the primary key, and either way stores nothing.
	n, err := s.affected(s.wr.ExecContext(ctx, `
		INSERT INTO device_lists (account_id, version, signed_list, created_at)
		SELECT ?, ?, ?, ?
		WHERE (SELECT COALESCE(MAX(version), 0) FROM device_lists WHERE account_id = ?) = ?
		ON CONFLICT (account_id, version) DO NOTHING`,
		string(l.AccountID), l.Version, l.Signed, int64(l.CreatedAt), string(l.AccountID), l.Version-1))
	if err != nil {
		return err
	}
	if n == 0 {
		return model.ErrConflict.With("That is not the next version of the device list.")
	}
	return nil
}

func (s sealed) PutFirstDeviceList(ctx context.Context, l store.SignedDeviceList) error {
	if l.CreatedAt.IsZero() {
		l.CreatedAt = model.Now()
	}
	n, err := s.affected(s.wr.ExecContext(ctx, `
		INSERT INTO device_lists (account_id, version, signed_list, created_at)
		SELECT ?, ?, ?, ?
		WHERE NOT EXISTS (SELECT 1 FROM device_lists WHERE account_id = ?)
		ON CONFLICT (account_id, version) DO NOTHING`,
		string(l.AccountID), l.Version, l.Signed, int64(l.CreatedAt), string(l.AccountID)))
	if err != nil {
		return err
	}
	if n == 0 {
		return model.ErrConflict.With("That account already has a device list.")
	}
	return nil
}

func (s sealed) DeviceLists(ctx context.Context, account model.AccountID, after int64, limit int) ([]store.SignedDeviceList, error) {
	rows, err := s.rd.QueryContext(ctx, `
		SELECT account_id, version, signed_list, created_at FROM device_lists
		WHERE account_id = ? AND version > ? ORDER BY version LIMIT ?`, string(account), after, limit)
	return collect(rows, err, scanDeviceList)
}

func (s sealed) DeviceList(ctx context.Context, account model.AccountID, version int64) (store.SignedDeviceList, bool, error) {
	l, err := scanDeviceList(s.rd.QueryRowContext(ctx, `
		SELECT account_id, version, signed_list, created_at FROM device_lists WHERE account_id = ? AND version = ?`,
		string(account), version))
	if errors.Is(err, sql.ErrNoRows) {
		return l, false, nil
	}
	return l, err == nil, err
}

func (s sealed) NewestDeviceList(ctx context.Context, account model.AccountID) (store.SignedDeviceList, bool, error) {
	l, err := scanDeviceList(s.rd.QueryRowContext(ctx, `
		SELECT account_id, version, signed_list, created_at FROM device_lists
		WHERE account_id = ? ORDER BY version DESC LIMIT 1`, string(account)))
	if errors.Is(err, sql.ErrNoRows) {
		return l, false, nil
	}
	return l, err == nil, err
}

func (s sealed) NewestDeviceListVersion(ctx context.Context, account model.AccountID) (int64, error) {
	var v int64
	err := s.rd.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM device_lists WHERE account_id = ?`, string(account)).Scan(&v)
	return v, err
}

// ---------------------------------------------------------------- epochs

const epochCols = `conversation_id, key_epoch, seq, device_id, record, created_at, ord`

func scanEpoch(s scanner) (model.EpochRecord, error) {
	var e model.EpochRecord
	var rec []byte
	err := s.Scan(str(&e.ConversationID), &e.KeyEpoch, &e.Seq, str(&e.DeviceID), blob(&rec), tm(&e.CreatedAt), &e.Ord)
	e.Record = rec
	return e, err
}

func (s sealed) PutEpoch(ctx context.Context, e model.EpochRecord) error {
	return s.atomic(ctx, func(q *queries) error {
		// The compare-and-set on the conversation decides the race
		// between two devices making the same epoch: the update locks
		// the row, and the loser changes nothing.
		n, err := q.affected(q.wr.ExecContext(ctx, `
			UPDATE conversations SET key_epoch = ? WHERE id = ? AND kind = 'sealed' AND COALESCE(key_epoch, 0) = ?`,
			e.KeyEpoch, string(e.ConversationID), e.KeyEpoch-1))
		if err != nil {
			return err
		}
		if n == 0 {
			return model.ErrConflict.With("That is not this chat's next key epoch.")
		}
		_, err = q.wr.ExecContext(ctx, `
			INSERT INTO sealed_epochs (conversation_id, key_epoch, seq, device_id, record, created_at, ord) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			string(e.ConversationID), e.KeyEpoch, e.Seq, string(e.DeviceID), []byte(e.Record), int64(e.CreatedAt), e.Ord)
		return q.mapErr(err)
	})
}

func (s sealed) SetEpochOrder(ctx context.Context, conv model.ConversationID, keyEpoch int64, ord string) error {
	_, err := s.wr.ExecContext(ctx, `UPDATE sealed_epochs SET ord = ? WHERE conversation_id = ? AND key_epoch = ?`, ord, string(conv), keyEpoch)
	return s.mapErr(err)
}

func (s sealed) ReplaceEpoch(ctx context.Context, e model.EpochRecord) error {
	n, err := s.affected(s.wr.ExecContext(ctx, `
		UPDATE sealed_epochs SET seq = ?, device_id = ?, record = ?, created_at = ?, ord = ? WHERE conversation_id = ? AND key_epoch = ?`,
		e.Seq, string(e.DeviceID), []byte(e.Record), int64(e.CreatedAt), e.Ord, string(e.ConversationID), e.KeyEpoch))
	if err != nil {
		return err
	}
	if n == 0 {
		return model.ErrNotFound
	}
	return nil
}

func (s sealed) Epoch(ctx context.Context, conv model.ConversationID, keyEpoch int64) (model.EpochRecord, bool, error) {
	e, err := scanEpoch(s.rd.QueryRowContext(ctx, `SELECT `+epochCols+` FROM sealed_epochs WHERE conversation_id = ? AND key_epoch = ?`,
		string(conv), keyEpoch))
	if errors.Is(err, sql.ErrNoRows) {
		return e, false, nil
	}
	return e, err == nil, err
}

func (s sealed) Epochs(ctx context.Context, conv model.ConversationID, from int64, limit int) ([]model.EpochRecord, bool, error) {
	rows, err := s.rd.QueryContext(ctx, `
		SELECT `+epochCols+` FROM sealed_epochs WHERE conversation_id = ? AND key_epoch >= ? ORDER BY key_epoch LIMIT ?`,
		string(conv), from, limit+1)
	list, err := collect(rows, err, scanEpoch)
	if err != nil {
		return nil, false, err
	}
	if len(list) > limit {
		return list[:limit], true, nil
	}
	return list, false, nil
}

// emptyEpoch is the condition, for a row of table t, that its key epoch is
// older than the chat's current one and that no message with ciphertext is
// left in it or in any epoch before it. Epochs that pass are the empty
// oldest ones: deleting them leaves an unbroken run.
func emptyEpoch(t string) string {
	return t + `.key_epoch < (SELECT c.key_epoch FROM conversations c WHERE c.id = ` + t + `.conversation_id)
		AND NOT EXISTS (SELECT 1 FROM messages m
			WHERE m.conversation_id = ` + t + `.conversation_id AND m.ciphertext IS NOT NULL AND m.key_epoch <= ` + t + `.key_epoch)`
}

func (s sealed) DeleteEmptyEpochs(ctx context.Context) (epochs, records int64, err error) {
	err = s.atomic(ctx, func(q *queries) error {
		var err error
		if records, err = q.affected(q.wr.ExecContext(ctx, `DELETE FROM key_backups WHERE `+emptyEpoch("key_backups"))); err != nil {
			return err
		}
		epochs, err = q.affected(q.wr.ExecContext(ctx, `DELETE FROM sealed_epochs WHERE `+emptyEpoch("sealed_epochs")))
		return err
	})
	return epochs, records, err
}

// ---------------------------------------------------------------- backup

const copyCols = `id, account_id, kind, passkey_id, device_id, key_version, locked_key, created_at`

func scanCopy(s scanner) (model.LockedCopy, error) {
	var c model.LockedCopy
	var key []byte
	err := s.Scan(str(&c.ID), str(&c.AccountID), &c.Kind, str(&c.PasskeyID), str(&c.DeviceID), &c.KeyVersion, blob(&key), tm(&c.CreatedAt))
	c.LockedKey = key
	return c, err
}

func (q *queries) accountRecord(ctx context.Context, ex execer, account model.AccountID) (model.AccountRecord, bool, error) {
	var r model.AccountRecord
	var rec []byte
	err := ex.QueryRowContext(ctx, `
		SELECT account_id, key_version, revision, record, replacement_due, updated_at FROM account_records WHERE account_id = ?`,
		string(account)).Scan(str(&r.AccountID), &r.KeyVersion, &r.Revision, blob(&rec), flag(&r.ReplacementDue), tm(&r.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	r.Record = rec
	return r, err == nil, err
}

func (s sealed) Backup(ctx context.Context, account model.AccountID) (model.Backup, bool, error) {
	rec, found, err := s.accountRecord(ctx, s.rd, account)
	if err != nil || !found {
		return model.Backup{}, false, err
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT `+copyCols+` FROM backup_keys WHERE account_id = ? ORDER BY created_at, id`, string(account))
	copies, err := collect(rows, err, scanCopy)
	if err != nil {
		return model.Backup{}, false, err
	}
	if copies == nil {
		copies = []model.LockedCopy{}
	}
	return model.Backup{KeyVersion: rec.KeyVersion, AccountRecord: rec, LockedCopies: copies, ReplacementDue: rec.ReplacementDue}, true, nil
}

// insertCopy stores one locked copy after removing the earlier one for the
// same way.
func (q *queries) insertCopy(ctx context.Context, c *model.LockedCopy) error {
	if c.ID == "" {
		c.ID = model.NewID[string]()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = model.Now()
	}
	var err error
	switch c.Kind {
	case model.CopyPasskey:
		_, err = q.wr.ExecContext(ctx, `DELETE FROM backup_keys WHERE account_id = ? AND kind = ? AND passkey_id = ?`,
			string(c.AccountID), c.Kind, string(c.PasskeyID))
	case model.CopyDevice:
		_, err = q.wr.ExecContext(ctx, `DELETE FROM backup_keys WHERE account_id = ? AND kind = ? AND device_id = ?`,
			string(c.AccountID), c.Kind, string(c.DeviceID))
	default:
		_, err = q.wr.ExecContext(ctx, `DELETE FROM backup_keys WHERE account_id = ? AND kind = ?`, string(c.AccountID), c.Kind)
	}
	if err != nil {
		return err
	}
	_, err = q.wr.ExecContext(ctx, `
		INSERT INTO backup_keys (id, account_id, kind, passkey_id, device_id, key_version, locked_key, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, string(c.AccountID), c.Kind, id(c.PasskeyID), id(c.DeviceID), c.KeyVersion, []byte(c.LockedKey), int64(c.CreatedAt))
	return q.mapErr(err)
}

func (s sealed) CreateBackup(ctx context.Context, account model.AccountID, rec model.AccountRecord, copies []model.LockedCopy, at model.Time) error {
	return s.atomic(ctx, func(q *queries) error {
		n, err := q.affected(q.wr.ExecContext(ctx, `
			INSERT INTO account_records (account_id, key_version, revision, record, replacement_due, updated_at)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (account_id) DO NOTHING`,
			string(account), rec.KeyVersion, rec.Revision, []byte(rec.Record), false, int64(at)))
		if err != nil {
			return err
		}
		if n == 0 {
			return model.ErrAlreadyExists.With("This account already has a backup.")
		}
		for i := range copies {
			c := copies[i]
			c.AccountID, c.CreatedAt = account, at
			if err := q.insertCopy(ctx, &c); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s sealed) PutLockedCopy(ctx context.Context, c *model.LockedCopy) error {
	return s.atomic(ctx, func(q *queries) error { return q.insertCopy(ctx, c) })
}

func (s sealed) LockedCopy(ctx context.Context, copyID string) (model.LockedCopy, error) {
	c, err := scanCopy(s.rd.QueryRowContext(ctx, `SELECT `+copyCols+` FROM backup_keys WHERE id = ?`, copyID))
	return c, s.mapErr(err)
}

func (s sealed) DeleteLockedCopy(ctx context.Context, account model.AccountID, copyID string) error {
	return s.changed(s.wr.ExecContext(ctx, `DELETE FROM backup_keys WHERE id = ? AND account_id = ?`, copyID, string(account)))
}

func (s sealed) PutAccountRecord(ctx context.Context, account model.AccountID, rec model.AccountRecord, at model.Time) error {
	return s.atomic(ctx, func(q *queries) error {
		n, err := q.affected(q.wr.ExecContext(ctx, `
			UPDATE account_records SET revision = ?, record = ?, updated_at = ?
			WHERE account_id = ? AND key_version = ? AND revision = ?`,
			rec.Revision, []byte(rec.Record), int64(at), string(account), rec.KeyVersion, rec.Revision-1))
		if err != nil || n > 0 {
			return err
		}
		if _, found, err := q.accountRecord(ctx, q.wr, account); err != nil {
			return err
		} else if !found {
			return model.ErrNotFound
		}
		return model.ErrConflict.With("The account record has moved on. Fetch the backup and try again.")
	})
}

func (s sealed) SetReplacementDue(ctx context.Context, account model.AccountID, due bool) (bool, error) {
	n, err := s.affected(s.wr.ExecContext(ctx, `UPDATE account_records SET replacement_due = ? WHERE account_id = ? AND replacement_due <> ?`,
		due, string(account), due))
	return n > 0, err
}

func (s sealed) ReplaceBackup(ctx context.Context, account model.AccountID, replaces int64, rec model.AccountRecord, copies []model.LockedCopy, records []model.BackupRecord, at model.Time) error {
	return s.atomic(ctx, func(q *queries) error {
		// The update is the compare-and-set: a second replacement of the
		// same version finds the version already changed.
		n, err := q.affected(q.wr.ExecContext(ctx, `
			UPDATE account_records SET key_version = ?, revision = ?, record = ?, replacement_due = ?, updated_at = ?
			WHERE account_id = ? AND key_version = ?`,
			rec.KeyVersion, rec.Revision, []byte(rec.Record), false, int64(at), string(account), replaces))
		if err != nil {
			return err
		}
		if n == 0 {
			return model.ErrConflict.With("That key version has already been replaced.")
		}
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM backup_keys WHERE account_id = ?`, string(account)); err != nil {
			return err
		}
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM key_backups WHERE account_id = ?`, string(account)); err != nil {
			return err
		}
		for i := range copies {
			c := copies[i]
			c.AccountID, c.CreatedAt = account, at
			if err := q.insertCopy(ctx, &c); err != nil {
				return err
			}
		}
		for _, r := range records {
			// A chat that was deleted meanwhile has no row to hang a
			// record from, and the foreign key would abort the swap.
			if _, err := q.wr.ExecContext(ctx, `
				INSERT INTO key_backups (account_id, conversation_id, key_epoch, key_version, record, created_at)
				SELECT ?, ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM conversations WHERE id = ?)
				ON CONFLICT (account_id, conversation_id, key_epoch) DO NOTHING`,
				string(account), string(r.ConversationID), r.KeyEpoch, r.KeyVersion, []byte(r.Record), int64(at),
				string(r.ConversationID)); err != nil {
				return q.mapErr(err)
			}
		}
		return nil
	})
}

func scanBackupRecord(s scanner) (model.BackupRecord, error) {
	var r model.BackupRecord
	var rec []byte
	err := s.Scan(str(&r.AccountID), str(&r.ConversationID), &r.KeyEpoch, &r.KeyVersion, blob(&rec), tm(&r.CreatedAt))
	r.Record = rec
	return r, err
}

const backupRecordCols = `account_id, conversation_id, key_epoch, key_version, record, created_at`

func (s sealed) PutBackupRecord(ctx context.Context, r model.BackupRecord) (model.BackupRecord, bool, error) {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = model.Now()
	}
	var created bool
	err := s.atomic(ctx, func(q *queries) error {
		n, err := q.affected(q.wr.ExecContext(ctx, `
			INSERT INTO key_backups (account_id, conversation_id, key_epoch, key_version, record, created_at)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (account_id, conversation_id, key_epoch) DO NOTHING`,
			string(r.AccountID), string(r.ConversationID), r.KeyEpoch, r.KeyVersion, []byte(r.Record), int64(r.CreatedAt)))
		if err != nil {
			return err
		}
		if created = n > 0; created {
			return nil
		}
		r, err = scanBackupRecord(q.wr.QueryRowContext(ctx, `
			SELECT `+backupRecordCols+` FROM key_backups WHERE account_id = ? AND conversation_id = ? AND key_epoch = ?`,
			string(r.AccountID), string(r.ConversationID), r.KeyEpoch))
		return q.mapErr(err)
	})
	return r, created, err
}

// backupCursor is the sort key of the last backup record of a page.
type backupCursor struct {
	Conv  string `json:"c"`
	Epoch int64  `json:"e"`
}

func (s sealed) BackupRecords(ctx context.Context, account model.AccountID, conv model.ConversationID, p store.Page) ([]model.BackupRecord, string, error) {
	where, args := `account_id = ?`, []any{string(account)}
	if conv != "" {
		where += ` AND conversation_id = ?`
		args = append(args, string(conv))
	}
	if p.Cursor != "" {
		var c backupCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		where += ` AND (conversation_id > ? OR (conversation_id = ? AND key_epoch > ?))`
		args = append(args, c.Conv, c.Conv, c.Epoch)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := s.rd.QueryContext(ctx, `
		SELECT `+backupRecordCols+` FROM key_backups WHERE `+where+` ORDER BY conversation_id, key_epoch LIMIT ?`, args...)
	list, err := collect(rows, err, scanBackupRecord)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(list) > limit {
		list = list[:limit]
		last := list[limit-1]
		next = store.EncodeCursor(backupCursor{Conv: string(last.ConversationID), Epoch: last.KeyEpoch})
	}
	return list, next, nil
}

// -------------------------------------------------------- unlock requests

const unlockCols = `id, account_id, device_id, request, answer, created_at, expires_at, answered_at`

func scanUnlock(s scanner) (model.UnlockRequest, error) {
	var r model.UnlockRequest
	var req, ans []byte
	err := s.Scan(str(&r.ID), str(&r.AccountID), str(&r.DeviceID), blob(&req), blob(&ans), tm(&r.CreatedAt), tm(&r.ExpiresAt), tm(&r.AnsweredAt))
	r.Request, r.Answer = req, ans
	return r, err
}

func (s sealed) CreateUnlockRequest(ctx context.Context, r *model.UnlockRequest) error {
	if r.ID == "" {
		r.ID = model.NewID[string]()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = model.Now()
	}
	_, err := s.wr.ExecContext(ctx, `
		INSERT INTO unlock_requests (id, account_id, device_id, request, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		r.ID, string(r.AccountID), string(r.DeviceID), []byte(r.Request), int64(r.CreatedAt), int64(r.ExpiresAt))
	return s.mapErr(err)
}

func (s sealed) UnlockRequest(ctx context.Context, requestID string) (model.UnlockRequest, error) {
	r, err := scanUnlock(s.rd.QueryRowContext(ctx, `SELECT `+unlockCols+` FROM unlock_requests WHERE id = ?`, requestID))
	return r, s.mapErr(err)
}

func (s sealed) UnlockRequests(ctx context.Context, account model.AccountID, now model.Time) ([]model.UnlockRequest, error) {
	rows, err := s.rd.QueryContext(ctx, `
		SELECT `+unlockCols+` FROM unlock_requests
		WHERE account_id = ? AND answered_at IS NULL AND expires_at > ? ORDER BY created_at, id`, string(account), int64(now))
	return collect(rows, err, scanUnlock)
}

func (s sealed) AnswerUnlockRequest(ctx context.Context, requestID string, answer []byte, now model.Time) (model.UnlockRequest, error) {
	var out model.UnlockRequest
	err := s.atomic(ctx, func(q *queries) error {
		r, err := scanUnlock(q.wr.QueryRowContext(ctx, `
			UPDATE unlock_requests SET answer = ?, answered_at = ?
			WHERE id = ? AND answered_at IS NULL AND expires_at > ? RETURNING `+unlockCols,
			answer, int64(now), requestID, int64(now)))
		if errors.Is(err, sql.ErrNoRows) {
			var n int
			if err := q.wr.QueryRowContext(ctx, `SELECT COUNT(*) FROM unlock_requests WHERE id = ?`, requestID).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return model.ErrNotFound
			}
			return model.ErrConflict.With("That request has been answered or has expired.")
		}
		out = r
		return q.mapErr(err)
	})
	return out, err
}

// -------------------------------------------------------------- messages

func (s sealed) EraseMessage(ctx context.Context, msg model.MessageID, by model.AccountID, at model.Time) (model.Message, []string, error) {
	var out model.Message
	var keys []string
	err := s.atomic(ctx, func(q *queries) error {
		m, err := q.message(ctx, q.wr, msg, true)
		if err != nil {
			return err
		}
		if m.Deleted() {
			return model.ErrConflict.With("That message is already deleted.")
		}
		if keys, err = q.deleteFiles(ctx, "message_id = ?", string(msg)); err != nil {
			return err
		}
		// The event that created the message carries its ciphertext in
		// the catch-up log; it goes too.
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM events WHERE message_id = ?`, string(msg)); err != nil {
			return err
		}
		if _, err := q.wr.ExecContext(ctx, `UPDATE messages SET ciphertext = NULL, deleted_at = ?, deleted_by = ? WHERE id = ?`,
			int64(at), id(by), string(msg)); err != nil {
			return q.mapErr(err)
		}
		m.Sealed, m.Files = nil, nil
		m.DeletedAt, m.DeletedByID = at, by
		out = m
		return nil
	})
	return out, keys, err
}

func (s sealed) SetHandover(ctx context.Context, codeHash, handover []byte, now model.Time) error {
	return s.changed(s.wr.ExecContext(ctx, `
		UPDATE oauth_codes SET handover = ? WHERE code_hash = ? AND used_at IS NULL AND expires_at > ?`,
		handover, codeHash, int64(now)))
}

// ----------------------------------------------------------------- chats

func (s sealed) Partners(ctx context.Context, account model.AccountID) ([]model.AccountID, error) {
	rows, err := s.rd.QueryContext(ctx, `
		SELECT DISTINCT other.account_id FROM members mine
		JOIN conversations c ON c.id = mine.conversation_id AND c.kind = 'sealed'
		JOIN members other ON other.conversation_id = c.id AND other.account_id <> mine.account_id
		WHERE mine.account_id = ? ORDER BY other.account_id`, string(account))
	return collectIDs[model.AccountID](rows, err)
}

func (s sealed) Chats(ctx context.Context, account model.AccountID) ([]model.ConversationID, error) {
	rows, err := s.rd.QueryContext(ctx, `
		SELECT c.id FROM members m JOIN conversations c ON c.id = m.conversation_id AND c.kind = 'sealed'
		WHERE m.account_id = ? ORDER BY c.id`, string(account))
	return collectIDs[model.ConversationID](rows, err)
}
