package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Devices is the `devices` table: one row per installed app or browser
// profile signed in to one account. A row carries the device's push
// registration (docs/PUSH.md section 3) and its two public keys for sealed
// chats (docs/SEALED.md section 4.1). It outlives its session: the session
// column is empty between sessions, and retired_at is set for good once the
// session was revoked or the device went thirty days without one.
type Devices interface {
	// Create stores a device. An empty ID and zero CreatedAt are filled in.
	Create(ctx context.Context, d *model.Device) error
	// Get returns one device, retired or not.
	Get(ctx context.Context, id model.DeviceID) (model.Device, error)
	// BySession returns the device a session belongs to. found is false
	// if it has none.
	BySession(ctx context.Context, session model.SessionID) (d model.Device, found bool, err error)
	// ForAccount returns every device of an account, retired ones
	// included, oldest first.
	ForAccount(ctx context.Context, account model.AccountID) ([]model.Device, error)
	// Update reads one device (locking it on PostgreSQL), lets fn change
	// it, and writes back its name, platform, push registration, push
	// failure mark, keys and last-seen time. The session and the retired
	// mark have their own methods.
	Update(ctx context.Context, id model.DeviceID, fn func(d *model.Device) error) (model.Device, error)
	// Attach gives a device that is not retired a new session, as when
	// the same app signs in again after its session ended by itself.
	// model.ErrConflict if the device is retired, or the session already
	// has a device.
	Attach(ctx context.Context, id model.DeviceID, session model.SessionID, at model.Time) (model.Device, error)
	// Delete removes a device row outright. It is for the keyless row an
	// app session is given at sign-in, when the app turns out to be a
	// device the server already knows and is attached to that one instead.
	Delete(ctx context.Context, id model.DeviceID) error
	// RetireForSession marks the device of a session retired, when the
	// session is revoked. found is false if the session has no device or
	// it was retired before.
	RetireForSession(ctx context.Context, session model.SessionID, at model.Time) (d model.Device, found bool, err error)
	// Retire marks one device retired and forgets its push address.
	// found is false if it was retired before.
	Retire(ctx context.Context, id model.DeviceID, at model.Time) (d model.Device, found bool, err error)
	// RetireIdle retires every device that has gone thirty days without
	// a session, where `before` is thirty days ago (docs/SEALED.md
	// section 4.1): a device whose session row still exists is retired
	// once that session ended (was revoked or expired) at or before
	// `before`; a device whose session row is gone is retired once the
	// device itself was last seen (or, never seen, was created) at or
	// before `before`. A device with a live session, or one whose
	// session ended more recently, stays. It returns the devices it
	// retired, for the janitor to announce.
	RetireIdle(ctx context.Context, before, at model.Time) ([]model.Device, error)

	// Wakeable returns the devices of an account that a wake-up can be
	// sent to: not retired, with a push kind of apns, fcm or unifiedpush
	// and an address that has not been forgotten.
	Wakeable(ctx context.Context, account model.AccountID) ([]model.Device, error)
	// WithWakeable returns which of the accounts given have a device
	// Wakeable would return, in one read for every two thousand of them:
	// a message that notifies thousands asks this once and not Wakeable
	// of each.
	WithWakeable(ctx context.Context, accounts []model.AccountID) (map[model.AccountID]bool, error)
	// PushSucceeded clears a device's failure mark.
	PushSucceeded(ctx context.Context, id model.DeviceID) error
	// PushFailed sets the failure mark if it is not set: it holds the
	// first failure of the current run of failures.
	PushFailed(ctx context.Context, id model.DeviceID, at model.Time) error
	// ForgetPush clears a device's push address at once and sets the
	// failure mark: the relay or the endpoint said the address is dead.
	ForgetPush(ctx context.Context, id model.DeviceID, at model.Time) error
	// ForgetFailedPush clears the address of every device whose failure
	// mark is older than `before` (seven days of failures running), for
	// the janitor. It returns how many it cleared.
	ForgetFailedPush(ctx context.Context, before model.Time) (int64, error)
}
