package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Sessions is the `sessions` table: every credential that acts as an
// account. Only hashes of tokens are stored (docs/AUTH.md section 10).
type Sessions interface {
	// Create inserts a session with the hash of its token, and for an app
	// session the hash of its refresh token (nil otherwise). An empty ID
	// and zero CreatedAt are filled in.
	Create(ctx context.Context, s *model.Session, tokenHash, refreshHash []byte) error
	// Get returns one session, whatever its state.
	Get(ctx context.Context, id model.SessionID) (model.Session, error)
	// ByTokenHash finds the session a cookie value, access token, personal
	// token or bot token belongs to. It returns the row whatever its
	// state; the caller checks revocation, expiry and kind.
	ByTokenHash(ctx context.Context, tokenHash []byte) (model.Session, error)
	// ByRefreshHash finds an app session by its current refresh token, or
	// by the one just replaced (previous is then true, and the caller
	// applies the 60-second tolerance against RotatedAt).
	ByRefreshHash(ctx context.Context, refreshHash []byte) (s model.Session, previous bool, err error)
	// ForAccount returns an account's sessions that are neither revoked
	// nor expired at `now`, newest first.
	ForAccount(ctx context.Context, account model.AccountID, now model.Time) ([]model.Session, error)
	// Touch records use: last_seen_at, and the new expires_at of a sliding
	// session (zero leaves it as it is).
	Touch(ctx context.Context, id model.SessionID, seenAt, expiresAt model.Time) error
	// SetFresh records a passkey or authenticator check on this session.
	SetFresh(ctx context.Context, id model.SessionID, at model.Time) error
	// SetLocked locks a session (at set) or unlocks it (at zero), and
	// says whether it changed anything: locking a locked session, or
	// unlocking one that is not locked, does not. A revoked session is
	// never changed (ruling R75).
	SetLocked(ctx context.Context, id model.SessionID, at model.Time) (changed bool, err error)
	// Rotate replaces an app session's tokens: the old refresh hash is
	// kept as the previous one with the time, and the new access and
	// refresh hashes and access expiry are stored.
	Rotate(ctx context.Context, id model.SessionID, tokenHash, refreshHash []byte, accessExpiresAt, at model.Time) error
	// Revoke ends a session. Revoking one that is already revoked changes
	// nothing.
	Revoke(ctx context.Context, id model.SessionID, at model.Time) error
	// RevokeForAccount ends all of an account's live sessions except one
	// (pass "" to end them all) and returns the ones it ended.
	RevokeForAccount(ctx context.Context, account model.AccountID, except model.SessionID, at model.Time) ([]model.SessionID, error)
	// Complete finishes a session that owed a second factor, in one
	// update that only succeeds while it is still pending and not
	// revoked: the pending mark is cleared, the proof recorded, the end
	// moved, the session marked freshly checked, and the token replaced,
	// so the pending token stops working. ok is false otherwise.
	Complete(ctx context.Context, id model.SessionID, tokenHash []byte, secondFactor string, expiresAt, at model.Time) (ok bool, err error)
	// RevokeKinds ends every live session of the given kinds on the whole
	// server except one (pass "" to end them all) and returns them, for
	// "sign everyone out".
	RevokeKinds(ctx context.Context, kinds []string, except model.SessionID, at model.Time) ([]SessionRef, error)
	// DeleteEnded deletes sessions that expired or were revoked before the
	// given moment, for the janitor.
	DeleteEnded(ctx context.Context, before model.Time) (int64, error)
}

// SessionRef names a session and the account it belongs to.
type SessionRef struct {
	ID        model.SessionID
	AccountID model.AccountID
}
