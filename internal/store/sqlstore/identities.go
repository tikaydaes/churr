package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type identities struct{ *queries }

// Identities returns the identities area.
func (q *queries) Identities() store.Identities { return identities{q} }

const providerCols = `id, kind, name, config, secret, group_claim, admin_group, enabled, created_at`

func scanProvider(s scanner) (model.SsoProvider, error) {
	var p model.SsoProvider
	err := s.Scan(str(&p.ID), &p.Kind, &p.Name, js(&p.Config), blob(&p.Secret), str(&p.GroupClaim), str(&p.AdminGroup),
		flag(&p.Enabled), tm(&p.CreatedAt))
	p.HasSecret = len(p.Secret) > 0
	return p, err
}

func (i identities) CreateProvider(ctx context.Context, p *model.SsoProvider) error {
	if p.ID == "" {
		p.ID = model.NewID[string]()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = model.Now()
	}
	_, err := i.wr.ExecContext(ctx, `
		INSERT INTO sso_providers (id, kind, name, config, secret, group_claim, admin_group, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Kind, p.Name, jsonObject(p.Config), bin(p.Secret), id(p.GroupClaim), id(p.AdminGroup), p.Enabled, int64(p.CreatedAt))
	p.HasSecret = len(p.Secret) > 0
	return i.mapErr(err)
}

func (i identities) GetProvider(ctx context.Context, provider string) (model.SsoProvider, error) {
	if !model.ValidID(provider) {
		return model.SsoProvider{}, model.ErrNotFound
	}
	p, err := scanProvider(i.rd.QueryRowContext(ctx, `SELECT `+providerCols+` FROM sso_providers WHERE id = ?`, provider))
	return p, i.mapErr(err)
}

func (i identities) ListProviders(ctx context.Context) ([]model.SsoProvider, error) {
	rows, err := i.rd.QueryContext(ctx, `SELECT `+providerCols+` FROM sso_providers ORDER BY created_at, id`)
	return collect(rows, err, scanProvider)
}

func (i identities) UpdateProvider(ctx context.Context, p model.SsoProvider) error {
	return i.changed(i.wr.ExecContext(ctx, `
		UPDATE sso_providers SET name = ?, config = ?, secret = ?, group_claim = ?, admin_group = ?, enabled = ? WHERE id = ?`,
		p.Name, jsonObject(p.Config), bin(p.Secret), id(p.GroupClaim), id(p.AdminGroup), p.Enabled, p.ID))
}

func (i identities) DeleteProvider(ctx context.Context, provider string) error {
	if !model.ValidID(provider) {
		return model.ErrNotFound
	}
	return i.changed(i.wr.ExecContext(ctx, `DELETE FROM sso_providers WHERE id = ?`, provider))
}

func (i identities) Link(ctx context.Context, in model.Identity) error {
	if in.LinkedAt.IsZero() {
		in.LinkedAt = model.Now()
	}
	// Answered without a failed statement, so the transaction this runs
	// in can go on.
	n, err := i.affected(i.wr.ExecContext(ctx, `
		INSERT INTO identities (provider_id, subject, account_id, linked_at, last_used_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (provider_id, subject) DO NOTHING`,
		in.ProviderID, in.Subject, string(in.AccountID), int64(in.LinkedAt), ms(in.LastUsedAt)))
	if err != nil {
		return err
	}
	if n == 0 {
		return model.ErrAlreadyExists.With("That sign-in identity is already linked to an account.")
	}
	return nil
}

const identityCols = `i.provider_id, i.subject, i.account_id, i.linked_at, i.last_used_at, p.name`

func scanIdentity(s scanner) (model.Identity, error) {
	var out model.Identity
	err := s.Scan(str(&out.ProviderID), &out.Subject, str(&out.AccountID), tm(&out.LinkedAt), tm(&out.LastUsedAt), &out.ProviderName)
	return out, err
}

func (i identities) BySubject(ctx context.Context, provider, subject string) (model.Identity, error) {
	out, err := scanIdentity(i.rd.QueryRowContext(ctx, `
		SELECT `+identityCols+` FROM identities i JOIN sso_providers p ON p.id = i.provider_id
		WHERE i.provider_id = ? AND i.subject = ?`, provider, subject))
	return out, i.mapErr(err)
}

func (i identities) ForAccount(ctx context.Context, account model.AccountID) ([]model.Identity, error) {
	rows, err := i.rd.QueryContext(ctx, `
		SELECT `+identityCols+` FROM identities i JOIN sso_providers p ON p.id = i.provider_id
		WHERE i.account_id = ? ORDER BY i.linked_at, i.provider_id, i.subject`, string(account))
	return collect(rows, err, scanIdentity)
}

func (i identities) CountForAccount(ctx context.Context, account model.AccountID) (int64, error) {
	var n int64
	err := i.rd.QueryRowContext(ctx, `SELECT COUNT(*) FROM identities WHERE account_id = ?`, string(account)).Scan(&n)
	return n, err
}

func (i identities) Touch(ctx context.Context, provider, subject string, at model.Time) error {
	return i.changed(i.wr.ExecContext(ctx,
		`UPDATE identities SET last_used_at = ? WHERE provider_id = ? AND subject = ?`, int64(at), provider, subject))
}

func (i identities) LastUsed(ctx context.Context, account model.AccountID) (model.Time, error) {
	var t model.Time
	err := i.rd.QueryRowContext(ctx, `SELECT MAX(last_used_at) FROM identities WHERE account_id = ?`, string(account)).Scan(tm(&t))
	return t, err
}

func (i identities) PutTotp(ctx context.Context, account model.AccountID, sealed []byte) error {
	if len(sealed) == 0 {
		return errors.New("sqlstore: an authenticator row needs its sealed secret")
	}
	_, err := i.wr.ExecContext(ctx, `
		INSERT INTO totp (account_id, secret, confirmed_at, last_step, created_at) VALUES (?, ?, NULL, NULL, ?)
		ON CONFLICT (account_id) DO UPDATE SET secret = excluded.secret, confirmed_at = NULL, last_step = NULL,
			created_at = excluded.created_at`,
		string(account), sealed, int64(model.Now()))
	return i.mapErr(err)
}

func (i identities) DeleteTotpUnconfirmed(ctx context.Context, before model.Time) (int64, error) {
	return i.affected(i.wr.ExecContext(ctx, `DELETE FROM totp WHERE confirmed_at IS NULL AND created_at < ?`, int64(before)))
}

func (i identities) GetTotp(ctx context.Context, account model.AccountID) (store.Totp, bool, error) {
	t := store.Totp{AccountID: account}
	err := i.rd.QueryRowContext(ctx, `SELECT secret, confirmed_at, last_step FROM totp WHERE account_id = ?`, string(account)).
		Scan(blob(&t.Secret), tm(&t.ConfirmedAt), num(&t.LastStep))
	if errors.Is(err, sql.ErrNoRows) {
		return store.Totp{}, false, nil
	}
	return t, err == nil, err
}

func (i identities) ConfirmTotp(ctx context.Context, account model.AccountID, at model.Time, step int64) (bool, error) {
	n, err := i.affected(i.wr.ExecContext(ctx,
		`UPDATE totp SET confirmed_at = ?, last_step = ? WHERE account_id = ? AND confirmed_at IS NULL`,
		int64(at), step, string(account)))
	return n > 0, err
}

func (i identities) AdvanceTotp(ctx context.Context, account model.AccountID, step int64) (bool, error) {
	n, err := i.affected(i.wr.ExecContext(ctx, `
		UPDATE totp SET last_step = ?
		WHERE account_id = ? AND confirmed_at IS NOT NULL AND (last_step IS NULL OR last_step < ?)`,
		step, string(account), step))
	return n > 0, err
}

func (i identities) DeleteTotp(ctx context.Context, account model.AccountID) (bool, error) {
	n, err := i.affected(i.wr.ExecContext(ctx, `DELETE FROM totp WHERE account_id = ?`, string(account)))
	return n > 0, err
}

const codeCols = `account_id, kind, client_name, redirect_uri, pkce_challenge, platform, box_key, handover, expires_at, used_at`

func scanCode(s scanner) (store.OAuthCode, error) {
	var c store.OAuthCode
	err := s.Scan(str(&c.AccountID), &c.Kind, &c.ClientName, str(&c.RedirectURI), str(&c.PKCEChallenge), str(&c.Platform),
		blob(&c.BoxKey), blob(&c.Handover), tm(&c.ExpiresAt), tm(&c.UsedAt))
	return c, err
}

func (i identities) CreateCode(ctx context.Context, hash []byte, c store.OAuthCode) error {
	if len(hash) == 0 {
		return errors.New("sqlstore: a one-time code needs its hash")
	}
	_, err := i.wr.ExecContext(ctx, `
		INSERT INTO oauth_codes (code_hash, account_id, kind, client_name, redirect_uri, pkce_challenge, platform, box_key, handover, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		hash, id(c.AccountID), c.Kind, c.ClientName, id(c.RedirectURI), id(c.PKCEChallenge), id(c.Platform),
		bin(c.BoxKey), bin(c.Handover), int64(c.ExpiresAt))
	return i.mapErr(err)
}

func (i identities) GetCode(ctx context.Context, hash []byte) (store.OAuthCode, error) {
	if len(hash) == 0 {
		return store.OAuthCode{}, model.ErrNotFound
	}
	c, err := scanCode(i.rd.QueryRowContext(ctx, `SELECT `+codeCols+` FROM oauth_codes WHERE code_hash = ?`, hash))
	return c, i.mapErr(err)
}

func (i identities) UseCode(ctx context.Context, hash []byte, at model.Time) (store.OAuthCode, bool, error) {
	if len(hash) == 0 {
		return store.OAuthCode{}, false, nil
	}
	c, err := scanCode(i.wr.QueryRowContext(ctx, `
		UPDATE oauth_codes SET used_at = ?
		WHERE code_hash = ? AND used_at IS NULL AND expires_at > ? AND account_id IS NOT NULL
		RETURNING `+codeCols, int64(at), hash, int64(at)))
	if errors.Is(err, sql.ErrNoRows) {
		return store.OAuthCode{}, false, nil
	}
	return c, err == nil, err
}

func (i identities) ClaimPairing(ctx context.Context, hash []byte, clientName, platform string, boxKey []byte, at model.Time) (bool, error) {
	if len(hash) == 0 || clientName == "" {
		return false, nil
	}
	n, err := i.affected(i.wr.ExecContext(ctx, `
		UPDATE oauth_codes SET client_name = ?, platform = ?, box_key = ?
		WHERE code_hash = ? AND kind = 'pairing' AND client_name = '' AND used_at IS NULL AND expires_at > ?`,
		clientName, id(platform), bin(boxKey), hash, int64(at)))
	return n > 0, err
}

func (i identities) ConfirmPairing(ctx context.Context, hash []byte, account model.AccountID, at model.Time) (bool, error) {
	if len(hash) == 0 {
		return false, nil
	}
	n, err := i.affected(i.wr.ExecContext(ctx, `
		UPDATE oauth_codes SET account_id = ?
		WHERE code_hash = ? AND kind = 'pairing' AND client_name <> '' AND account_id IS NULL AND used_at IS NULL AND expires_at > ?`,
		string(account), hash, int64(at)))
	return n > 0, err
}

func (i identities) DeleteCodesEnded(ctx context.Context, before model.Time) (int64, error) {
	return i.affected(i.wr.ExecContext(ctx, `DELETE FROM oauth_codes WHERE expires_at < ?`, int64(before)))
}

func (i identities) AddImportClaim(ctx context.Context, account model.AccountID, emailHash []byte) error {
	_, err := i.wr.ExecContext(ctx, `
		INSERT INTO import_claims (account_id, email_hash) VALUES (?, ?)
		ON CONFLICT (account_id) DO UPDATE SET email_hash = excluded.email_hash`, string(account), emailHash)
	return i.mapErr(err)
}

func (i identities) HasImportClaims(ctx context.Context) (bool, error) {
	var one int
	err := i.rd.QueryRowContext(ctx, `SELECT 1 FROM import_claims LIMIT 1`).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (i identities) TakeImportClaim(ctx context.Context, emailHash []byte) (model.AccountID, bool, error) {
	if len(emailHash) == 0 {
		return "", false, nil
	}
	var account model.AccountID
	err := i.wr.QueryRowContext(ctx, `DELETE FROM import_claims WHERE email_hash = ? RETURNING account_id`, emailHash).Scan(str(&account))
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return account, err == nil, err
}
