package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type devices struct{ *queries }

// Devices returns the devices area.
func (q *queries) Devices() store.Devices { return devices{q} }

const deviceCols = `id, account_id, session_id, name, platform, push_kind, push_address, push_failed_at, push_public_key, push_auth,
	sign_key, box_key, retired_at, created_at, last_seen_at`

func scanDevice(s scanner) (model.Device, error) {
	var d model.Device
	err := s.Scan(str(&d.ID), str(&d.AccountID), str(&d.SessionID), &d.Name, &d.Platform, str(&d.PushKind), str(&d.PushAddress),
		tm(&d.PushFailedAt), blob(&d.PushPublicKey), blob(&d.PushAuth),
		blob(&d.SignKey), blob(&d.BoxKey), tm(&d.RetiredAt), tm(&d.CreatedAt), tm(&d.LastSeenAt))
	return d, err
}

func (d devices) Create(ctx context.Context, dev *model.Device) error {
	if dev.ID == "" {
		dev.ID = model.NewID[model.DeviceID]()
	}
	if dev.CreatedAt.IsZero() {
		dev.CreatedAt = model.Now()
	}
	_, err := d.wr.ExecContext(ctx, `
		INSERT INTO devices (id, account_id, session_id, name, platform, push_kind, push_address, push_public_key, push_auth,
			sign_key, box_key, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(dev.ID), string(dev.AccountID), id(dev.SessionID), dev.Name, dev.Platform, id(dev.PushKind), id(dev.PushAddress),
		bin(dev.PushPublicKey), bin(dev.PushAuth), bin(dev.SignKey), bin(dev.BoxKey),
		int64(dev.CreatedAt), ms(dev.LastSeenAt))
	return d.mapErr(err)
}

func (d devices) Get(ctx context.Context, device model.DeviceID) (model.Device, error) {
	dev, err := scanDevice(d.rd.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE id = ?`, string(device)))
	return dev, d.mapErr(err)
}

func (d devices) BySession(ctx context.Context, session model.SessionID) (model.Device, bool, error) {
	dev, err := scanDevice(d.rd.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE session_id = ?`, string(session)))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Device{}, false, nil
	}
	return dev, err == nil, err
}

func (d devices) ForAccount(ctx context.Context, account model.AccountID) ([]model.Device, error) {
	rows, err := d.rd.QueryContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE account_id = ? ORDER BY created_at, id`, string(account))
	return collect(rows, err, scanDevice)
}

func (d devices) Update(ctx context.Context, device model.DeviceID, fn func(dev *model.Device) error) (model.Device, error) {
	var out model.Device
	err := d.atomic(ctx, func(q *queries) error {
		dev, err := scanDevice(q.wr.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE id = ?`+q.forUpdate(), string(device)))
		if err != nil {
			return q.mapErr(err)
		}
		if err := fn(&dev); err != nil {
			return err
		}
		_, err = q.wr.ExecContext(ctx, `
			UPDATE devices SET name = ?, platform = ?, push_kind = ?, push_address = ?, push_failed_at = ?, push_public_key = ?,
				push_auth = ?, sign_key = ?, box_key = ?, last_seen_at = ?
			WHERE id = ?`,
			dev.Name, dev.Platform, id(dev.PushKind), id(dev.PushAddress), ms(dev.PushFailedAt), bin(dev.PushPublicKey),
			bin(dev.PushAuth), bin(dev.SignKey), bin(dev.BoxKey), ms(dev.LastSeenAt), string(device))
		out = dev
		return q.mapErr(err)
	})
	return out, err
}

func (d devices) Attach(ctx context.Context, device model.DeviceID, session model.SessionID, at model.Time) (model.Device, error) {
	var out model.Device
	err := d.atomic(ctx, func(q *queries) error {
		// Asked first, so the UNIQUE on session_id never fails inside a
		// transaction PostgreSQL would then abort.
		var n int
		if err := q.wr.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE session_id = ? AND id <> ?`,
			string(session), string(device)).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return model.ErrConflict.With("This session already has a device.")
		}
		dev, err := scanDevice(q.wr.QueryRowContext(ctx, `
			UPDATE devices SET session_id = ?, last_seen_at = ? WHERE id = ? AND retired_at IS NULL RETURNING `+deviceCols,
			string(session), int64(at), string(device)))
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrConflict.With("A retired device cannot be attached again.")
		}
		out = dev
		return q.mapErr(err)
	})
	return out, err
}

func (d devices) Delete(ctx context.Context, device model.DeviceID) error {
	return d.changed(d.wr.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, string(device)))
}

func (d devices) RetireForSession(ctx context.Context, session model.SessionID, at model.Time) (model.Device, bool, error) {
	dev, err := scanDevice(d.wr.QueryRowContext(ctx, `
		UPDATE devices SET retired_at = ? WHERE session_id = ? AND retired_at IS NULL RETURNING `+deviceCols,
		int64(at), string(session)))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Device{}, false, nil
	}
	return dev, err == nil, err
}

func (d devices) Retire(ctx context.Context, device model.DeviceID, at model.Time) (model.Device, bool, error) {
	dev, err := scanDevice(d.wr.QueryRowContext(ctx, `
		UPDATE devices SET retired_at = ?, push_address = NULL WHERE id = ? AND retired_at IS NULL RETURNING `+deviceCols,
		int64(at), string(device)))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Device{}, false, nil
	}
	return dev, err == nil, err
}

func (d devices) RetireIdle(ctx context.Context, before, at model.Time) ([]model.Device, error) {
	// A device outlives its session by up to thirty days. While the
	// session row exists it says when the session ended. Once the row is
	// gone the device's own last-seen time is all there is to go by: a
	// device is seen whenever its app registers, so it is never later
	// than the end of the session that was lost.
	rows, err := d.wr.QueryContext(ctx, `
		UPDATE devices SET retired_at = ?, push_address = NULL
		WHERE retired_at IS NULL AND (
			(session_id IS NULL AND COALESCE(last_seen_at, created_at) <= ?)
			OR session_id IN (
				SELECT id FROM sessions
				WHERE (revoked_at IS NOT NULL AND revoked_at <= ?) OR (expires_at IS NOT NULL AND expires_at <= ?)))
		RETURNING `+deviceCols,
		int64(at), int64(before), int64(before), int64(before))
	return collect(rows, err, scanDevice)
}

func (d devices) Wakeable(ctx context.Context, account model.AccountID) ([]model.Device, error) {
	rows, err := d.rd.QueryContext(ctx, `
		SELECT `+deviceCols+` FROM devices
		WHERE account_id = ? AND retired_at IS NULL AND push_address IS NOT NULL
			AND push_kind IN ('apns', 'fcm', 'unifiedpush')
		ORDER BY created_at, id`, string(account))
	return collect(rows, err, scanDevice)
}

func (d devices) PushSucceeded(ctx context.Context, device model.DeviceID) error {
	_, err := d.wr.ExecContext(ctx, `UPDATE devices SET push_failed_at = NULL WHERE id = ? AND push_failed_at IS NOT NULL`, string(device))
	return d.mapErr(err)
}

func (d devices) PushFailed(ctx context.Context, device model.DeviceID, at model.Time) error {
	_, err := d.wr.ExecContext(ctx, `UPDATE devices SET push_failed_at = ? WHERE id = ? AND push_failed_at IS NULL`, int64(at), string(device))
	return d.mapErr(err)
}

func (d devices) ForgetPush(ctx context.Context, device model.DeviceID, at model.Time) error {
	_, err := d.wr.ExecContext(ctx, `
		UPDATE devices SET push_address = NULL, push_failed_at = COALESCE(push_failed_at, ?) WHERE id = ?`, int64(at), string(device))
	return d.mapErr(err)
}

func (d devices) ForgetFailedPush(ctx context.Context, before model.Time) (int64, error) {
	return d.affected(d.wr.ExecContext(ctx, `
		UPDATE devices SET push_address = NULL
		WHERE push_address IS NOT NULL AND push_failed_at IS NOT NULL AND push_failed_at < ?`, int64(before)))
}
