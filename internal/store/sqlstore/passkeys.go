package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type passkeys struct{ *queries }

// Passkeys returns the passkeys area.
func (q *queries) Passkeys() store.Passkeys { return passkeys{q} }

// passkeyCols lists the columns of a passkeys row with the alias p, and
// whether a locked copy of the backup key exists for it, which is what lets
// the passkey unlock sealed history.
const passkeyCols = `p.id, p.account_id, p.credential_id, p.credential, p.name, p.has_prf, p.unlock_public, p.created_at, p.last_used_at,
	EXISTS (SELECT 1 FROM backup_keys bk WHERE bk.passkey_id = p.id AND bk.kind = 'passkey')`

func scanPasskey(s scanner) (model.Passkey, error) {
	var p model.Passkey
	err := s.Scan(str(&p.ID), str(&p.AccountID), blob(&p.CredentialID), js(&p.Credential), &p.Name, flag(&p.HasPRF),
		blob(&p.UnlockPublic), tm(&p.CreatedAt), tm(&p.LastUsedAt), flag(&p.CanUnlock))
	return p, err
}

func (p passkeys) Add(ctx context.Context, pk *model.Passkey) error {
	if pk.ID == "" {
		pk.ID = model.NewID[model.PasskeyID]()
	}
	if pk.CreatedAt.IsZero() {
		pk.CreatedAt = model.Now()
	}
	// A credential registered twice is answered without a failed
	// statement, so a transaction this runs in can go on.
	n, err := p.affected(p.wr.ExecContext(ctx, `
		INSERT INTO passkeys (id, account_id, credential_id, credential, name, has_prf, unlock_public, created_at, last_used_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (credential_id) DO NOTHING`,
		string(pk.ID), string(pk.AccountID), bin(pk.CredentialID), jsonObject(pk.Credential), pk.Name, pk.HasPRF,
		bin(pk.UnlockPublic), int64(pk.CreatedAt), ms(pk.LastUsedAt)))
	if err != nil {
		return err
	}
	if n == 0 {
		return model.ErrAlreadyExists.With("That passkey is already registered.")
	}
	// No copy of the backup key can exist for a passkey made this moment.
	pk.CanUnlock = false
	return nil
}

func (p passkeys) Get(ctx context.Context, passkey model.PasskeyID) (model.Passkey, error) {
	out, err := scanPasskey(p.rd.QueryRowContext(ctx, `SELECT `+passkeyCols+` FROM passkeys p WHERE p.id = ?`, string(passkey)))
	return out, p.mapErr(err)
}

func (p passkeys) ByCredentialID(ctx context.Context, credentialID []byte) (model.Passkey, error) {
	if len(credentialID) == 0 {
		return model.Passkey{}, model.ErrNotFound
	}
	out, err := scanPasskey(p.rd.QueryRowContext(ctx, `SELECT `+passkeyCols+` FROM passkeys p WHERE p.credential_id = ?`, credentialID))
	return out, p.mapErr(err)
}

func (p passkeys) ForAccount(ctx context.Context, account model.AccountID) ([]model.Passkey, error) {
	rows, err := p.rd.QueryContext(ctx, `
		SELECT `+passkeyCols+` FROM passkeys p WHERE p.account_id = ? ORDER BY p.created_at, p.id`, string(account))
	return collect(rows, err, scanPasskey)
}

func (p passkeys) Count(ctx context.Context, account model.AccountID) (int64, error) {
	// Through the write handle: removing a passkey asks, inside its
	// transaction, whether it would be the account's last.
	var n int64
	err := p.wr.QueryRowContext(ctx, `SELECT count(*) FROM passkeys WHERE account_id = ?`, string(account)).Scan(&n)
	return n, err
}

func (p passkeys) Used(ctx context.Context, passkey model.PasskeyID, credential json.RawMessage, at model.Time) error {
	return p.changed(p.wr.ExecContext(ctx, `UPDATE passkeys SET credential = ?, last_used_at = ? WHERE id = ?`,
		jsonObject(credential), ms(at), string(passkey)))
}

func (p passkeys) Rename(ctx context.Context, passkey model.PasskeyID, name string) error {
	return p.changed(p.wr.ExecContext(ctx, `UPDATE passkeys SET name = ? WHERE id = ?`, name, string(passkey)))
}

func (p passkeys) SetPRF(ctx context.Context, passkey model.PasskeyID, hasPRF bool, unlockPublic []byte) error {
	// COALESCE keeps the stored key when none is given.
	return p.changed(p.wr.ExecContext(ctx, `
		UPDATE passkeys SET has_prf = ?, unlock_public = COALESCE(?, unlock_public) WHERE id = ?`,
		hasPRF, bin(unlockPublic), string(passkey)))
}

func (p passkeys) Delete(ctx context.Context, passkey model.PasskeyID) error {
	// The passkey's backup_keys row goes by cascade.
	return p.changed(p.wr.ExecContext(ctx, `DELETE FROM passkeys WHERE id = ?`, string(passkey)))
}

func (p passkeys) SetAccessKey(ctx context.Context, account model.AccountID, authHash, unlockPublic []byte, at model.Time) error {
	if len(authHash) == 0 {
		return errors.New("sqlstore: an access key is stored by its hash, and it was empty")
	}
	_, err := p.wr.ExecContext(ctx, `
		INSERT INTO access_keys (account_id, auth_hash, unlock_public, created_at, last_used_at) VALUES (?, ?, ?, ?, NULL)
		ON CONFLICT (account_id) DO UPDATE SET auth_hash = excluded.auth_hash, unlock_public = excluded.unlock_public,
			created_at = excluded.created_at, last_used_at = NULL`,
		string(account), authHash, bin(unlockPublic), int64(at))
	return p.mapErr(err)
}

func (p passkeys) AccessKey(ctx context.Context, account model.AccountID) (store.AccessKey, bool, error) {
	// Through the write handle: replacing or removing a key asks, inside
	// its transaction, what the account has.
	var k store.AccessKey
	err := p.wr.QueryRowContext(ctx, `SELECT auth_hash, unlock_public, created_at, last_used_at FROM access_keys WHERE account_id = ?`,
		string(account)).Scan(blob(&k.AuthHash), blob(&k.UnlockPublic), tm(&k.CreatedAt), tm(&k.LastUsedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return k, false, nil
	}
	return k, err == nil, err
}

func (p passkeys) AccessKeyUsed(ctx context.Context, account model.AccountID, at model.Time) error {
	return p.changed(p.wr.ExecContext(ctx, `UPDATE access_keys SET last_used_at = ? WHERE account_id = ?`, int64(at), string(account)))
}

func (p passkeys) DeleteAccessKey(ctx context.Context, account model.AccountID) (bool, error) {
	n, err := p.affected(p.wr.ExecContext(ctx, `DELETE FROM access_keys WHERE account_id = ?`, string(account)))
	return n > 0, err
}

// ExecForTest runs one statement as written, with "?" placeholders, through
// the write handle, and returns how many rows it touched. It is not part of
// store.Store and no server code may call it: the storage tests reach it by
// type assertion to make and count rows in tables that no area reaches yet
// (a `servers` row behind a remote account, the `backup_keys` row that makes
// a passkey able to unlock). It lives in this file only because the passkey
// tests were the first to need it, and it goes once Peers and Sealed have
// methods of their own.
func (d *DB) ExecForTest(ctx context.Context, query string, args ...any) (int64, error) {
	return d.affected(d.wr.ExecContext(ctx, query, args...))
}
