package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type sessions struct{ *queries }

// Sessions returns the sessions area.
func (q *queries) Sessions() store.Sessions { return sessions{q} }

// sessionCols lists the columns of a sessions row that are ever read back.
// The token hashes are write-only: a session is found by them and never
// returns them.
const sessionCols = `id, account_id, kind, label, signed_in_by, second_factor, pending_second_factor, created_at,
	last_seen_at, expires_at, access_expires_at, max_expires_at, fresh_at, rotated_at, revoked_at`

// recoverySession reports whether a session came from a recovery code or
// link, which limits what it may do (docs/AUTH.md section 6).
func recoverySession(signedInBy string) bool {
	return signedInBy == "recovery_code" || signedInBy == "recovery_link"
}

func scanSession(s scanner) (model.Session, error) {
	var out model.Session
	err := s.Scan(str(&out.ID), str(&out.AccountID), &out.Kind, &out.Label, str(&out.SignedInBy), str(&out.SecondFactor),
		flag(&out.PendingSecondFactor), tm(&out.CreatedAt), tm(&out.LastSeenAt), tm(&out.ExpiresAt), tm(&out.AccessExpiresAt),
		tm(&out.MaxExpiresAt), tm(&out.FreshAt), tm(&out.RotatedAt), tm(&out.RevokedAt))
	out.Recovery = recoverySession(out.SignedInBy)
	return out, err
}

func (s sessions) Create(ctx context.Context, ses *model.Session, tokenHash, refreshHash []byte) error {
	if len(tokenHash) == 0 {
		// An empty hash would be stored as an empty value that the next
		// empty one collides with, and that no token could ever match.
		return errors.New("sqlstore: a session needs the hash of its token")
	}
	if ses.ID == "" {
		ses.ID = model.NewID[model.SessionID]()
	}
	if ses.CreatedAt.IsZero() {
		ses.CreatedAt = model.Now()
	}
	_, err := s.wr.ExecContext(ctx, `
		INSERT INTO sessions (id, account_id, kind, token_hash, refresh_hash, access_expires_at, max_expires_at, fresh_at,
			label, signed_in_by, second_factor, pending_second_factor, created_at, last_seen_at, expires_at, revoked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(ses.ID), string(ses.AccountID), ses.Kind, tokenHash, bin(refreshHash), ms(ses.AccessExpiresAt),
		ms(ses.MaxExpiresAt), ms(ses.FreshAt), ses.Label, id(ses.SignedInBy), id(ses.SecondFactor), ses.PendingSecondFactor,
		int64(ses.CreatedAt), ms(ses.LastSeenAt), ms(ses.ExpiresAt), ms(ses.RevokedAt))
	if err != nil {
		return s.mapErr(err)
	}
	ses.Recovery = recoverySession(ses.SignedInBy)
	return nil
}

func (s sessions) Get(ctx context.Context, session model.SessionID) (model.Session, error) {
	out, err := scanSession(s.rd.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions WHERE id = ?`, string(session)))
	return out, s.mapErr(err)
}

func (s sessions) ByTokenHash(ctx context.Context, tokenHash []byte) (model.Session, error) {
	if len(tokenHash) == 0 {
		return model.Session{}, model.ErrNotFound
	}
	out, err := scanSession(s.rd.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions WHERE token_hash = ?`, tokenHash))
	return out, s.mapErr(err)
}

func (s sessions) ByRefreshHash(ctx context.Context, refreshHash []byte) (model.Session, bool, error) {
	if len(refreshHash) == 0 {
		return model.Session{}, false, model.ErrNotFound
	}
	// A refresh reads the session here and then calls Rotate, in one
	// transaction. Locking the row makes a second refresh with the same
	// token wait for the first and then find its token is the previous
	// one, so a token is never accepted as current twice
	// (docs/AUTH.md section 11.3). Outside a transaction a lock would be
	// dropped at once, so none is asked for.
	lock := ""
	if s.inTx {
		lock = s.forUpdate()
	}
	out, err := scanSession(s.rd.QueryRowContext(ctx,
		`SELECT `+sessionCols+` FROM sessions WHERE refresh_hash = ?`+lock, refreshHash))
	if err == nil {
		return out, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return model.Session{}, false, err
	}
	out, err = scanSession(s.rd.QueryRowContext(ctx,
		`SELECT `+sessionCols+` FROM sessions WHERE prev_refresh_hash = ? ORDER BY rotated_at DESC, id LIMIT 1`+lock, refreshHash))
	if err != nil {
		return model.Session{}, false, s.mapErr(err)
	}
	return out, true, nil
}

func (s sessions) ForAccount(ctx context.Context, account model.AccountID, now model.Time) ([]model.Session, error) {
	rows, err := s.rd.QueryContext(ctx, `
		SELECT `+sessionCols+` FROM sessions
		WHERE account_id = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)
		ORDER BY created_at DESC, id DESC`, string(account), int64(now))
	return collect(rows, err, scanSession)
}

func (s sessions) Touch(ctx context.Context, session model.SessionID, seenAt, expiresAt model.Time) error {
	if expiresAt.IsZero() {
		return s.changed(s.wr.ExecContext(ctx,
			`UPDATE sessions SET last_seen_at = COALESCE(?, last_seen_at) WHERE id = ?`, ms(seenAt), string(session)))
	}
	// Use never moves a session past its hard end.
	return s.changed(s.wr.ExecContext(ctx, `
		UPDATE sessions SET last_seen_at = COALESCE(?, last_seen_at),
			expires_at = CASE WHEN max_expires_at IS NOT NULL AND max_expires_at < ? THEN max_expires_at ELSE ? END
		WHERE id = ?`, ms(seenAt), int64(expiresAt), int64(expiresAt), string(session)))
}

func (s sessions) SetFresh(ctx context.Context, session model.SessionID, at model.Time) error {
	return s.changed(s.wr.ExecContext(ctx, `UPDATE sessions SET fresh_at = ? WHERE id = ?`, ms(at), string(session)))
}

func (s sessions) Rotate(ctx context.Context, session model.SessionID, tokenHash, refreshHash []byte, accessExpiresAt, at model.Time) error {
	if len(tokenHash) == 0 || len(refreshHash) == 0 {
		return errors.New("sqlstore: rotating a session needs the hashes of both new tokens")
	}
	// Every right-hand side reads the row as it was, so the previous hash
	// is the one being replaced whatever the order of the assignments.
	return s.changed(s.wr.ExecContext(ctx, `
		UPDATE sessions SET prev_refresh_hash = refresh_hash, refresh_hash = ?, token_hash = ?, access_expires_at = ?, rotated_at = ?
		WHERE id = ?`, refreshHash, tokenHash, ms(accessExpiresAt), ms(at), string(session)))
}

func (s sessions) Revoke(ctx context.Context, session model.SessionID, at model.Time) error {
	return s.atomic(ctx, func(q *queries) error {
		n, err := q.affected(q.wr.ExecContext(ctx,
			`UPDATE sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, int64(at), string(session)))
		if err != nil || n > 0 {
			return err
		}
		// Nothing changed: either it was revoked before, which keeps
		// the time it was first ended, or there is no such session.
		var one int
		err = q.wr.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id = ?`, string(session)).Scan(&one)
		return q.mapErr(err)
	})
}

func (s sessions) RevokeForAccount(ctx context.Context, account model.AccountID, except model.SessionID, at model.Time) ([]model.SessionID, error) {
	// Live means neither revoked nor expired. A session that has run out
	// is already ended and has no connection to close.
	query := `UPDATE sessions SET revoked_at = ?
		WHERE account_id = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`
	args := []any{int64(at), string(account), int64(at)}
	if except != "" {
		query += ` AND id <> ?`
		args = append(args, string(except))
	}
	return collectIDs[model.SessionID](s.wr.QueryContext(ctx, query+` RETURNING id`, args...))
}

func (s sessions) DeleteEnded(ctx context.Context, before model.Time) (int64, error) {
	return s.affected(s.wr.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE (revoked_at IS NOT NULL AND revoked_at < ?) OR (expires_at IS NOT NULL AND expires_at < ?)`,
		int64(before), int64(before)))
}

func (s sessions) Complete(ctx context.Context, session model.SessionID, tokenHash []byte, secondFactor string, expiresAt, at model.Time) (bool, error) {
	if len(tokenHash) == 0 {
		return false, errors.New("sqlstore: completing a session needs the hash of its new token")
	}
	n, err := s.affected(s.wr.ExecContext(ctx, `
		UPDATE sessions SET pending_second_factor = ?, second_factor = ?, expires_at = ?, fresh_at = ?, token_hash = ?
		WHERE id = ? AND pending_second_factor = ? AND revoked_at IS NULL`,
		false, id(secondFactor), ms(expiresAt), int64(at), tokenHash, string(session), true))
	return n > 0, err
}

func (s sessions) RevokeKinds(ctx context.Context, kinds []string, except model.SessionID, at model.Time) ([]store.SessionRef, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	query := `UPDATE sessions SET revoked_at = ?
		WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?) AND kind IN (` + marks(len(kinds)) + `)`
	args := append([]any{int64(at), int64(at)}, anys(kinds)...)
	if except != "" {
		query += ` AND id <> ?`
		args = append(args, string(except))
	}
	rows, err := s.wr.QueryContext(ctx, query+` RETURNING id, account_id`, args...)
	return collect(rows, err, func(sc scanner) (store.SessionRef, error) {
		var r store.SessionRef
		err := sc.Scan(str(&r.ID), str(&r.AccountID))
		return r, err
	})
}
