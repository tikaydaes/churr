package sealed

import (
	"context"
	"errors"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// The key backup as the server holds it (docs/SEALED.md section 13): the
// account record, the locked copies of the backup key, the backup records,
// and unlock requests. The server can open none of it. What it checks is
// shape and bookkeeping: sizes, which way a copy is for, key versions and
// revisions going up in step.

// The fixed sizes of what is stored.
const (
	lockedCopySize   = 80 + 64 // the sealed backup key and the account key's signature
	backupRecordSize = 16 + 81 // salt, then 65 bytes of content with its tag
	// minAccountRecord is an account record with no ways: salt, 105
	// bytes of content, tag.
	minAccountRecord = 16 + 105 + 16
	// unlockRequestSize is the request record; pairingMACSize what may
	// follow it after QR pairing.
	unlockRequestSize = 170
	pairingMACSize    = 32
	// unlockRequestLife is how long an unlock request waits.
	unlockRequestLife = 10 * time.Minute
)

func checkAccountRecord(field string, rec model.AccountRecord) error {
	if rec.KeyVersion < 1 || rec.Revision < 1 {
		return model.Invalid(field, "key_version and revision start at 1.")
	}
	if len(rec.Record) < minAccountRecord || len(rec.Record) > maxRecord+32 {
		return model.Invalid(field, "That is not an account record.")
	}
	return nil
}

// checkCopy makes the checks on one locked copy: its size, its kind, and
// that the passkey or device it is for is the account's own.
func (s *Service) checkCopy(ctx context.Context, a store.Areas, account model.AccountID, keyVersion int64, c *model.LockedCopy) error {
	if len(c.LockedKey) != lockedCopySize {
		return model.Invalid("locked_key", "A locked copy is 144 bytes.")
	}
	if c.KeyVersion != keyVersion {
		return model.ErrConflict.With("The locked copy is for another key version than the backup's.")
	}
	c.AccountID = account
	switch c.Kind {
	case model.CopyPhrase:
		if c.PasskeyID != "" || c.DeviceID != "" {
			return model.Invalid("kind", "A phrase copy names no passkey and no device.")
		}
	case model.CopyPasskey:
		if c.DeviceID != "" || !model.ValidID(string(c.PasskeyID)) {
			return model.Invalid("passkey_id", "A passkey copy names its passkey.")
		}
		pk, err := a.Passkeys().Get(ctx, c.PasskeyID)
		if errors.Is(err, model.ErrNotFound) || (err == nil && pk.AccountID != account) {
			return model.Invalid("passkey_id", "That is not a passkey of this account.")
		}
		if err != nil {
			return err
		}
		if len(pk.UnlockPublic) == 0 {
			return model.Invalid("passkey_id", "That passkey has given no unlock key.")
		}
	case model.CopyDevice:
		if c.PasskeyID != "" || !model.ValidID(string(c.DeviceID)) {
			return model.Invalid("device_id", "A device copy names its device.")
		}
		d, err := a.Devices().Get(ctx, c.DeviceID)
		if errors.Is(err, model.ErrNotFound) || (err == nil && (d.AccountID != account || !d.RetiredAt.IsZero())) {
			return model.Invalid("device_id", "That is not a device of this account.")
		}
		if err != nil {
			return err
		}
	default:
		return model.Invalid("kind", "kind is passkey, phrase or device.")
	}
	return nil
}

func (s *Service) backupChanged(ctx context.Context, account model.AccountID) {
	b, found, err := s.store.Sealed().Backup(ctx, account)
	if err != nil || !found {
		return
	}
	s.hub.Send(account, s.event(model.EventBackupChanged, map[string]any{"key_version": b.KeyVersion, "replacement_due": b.ReplacementDue}))
}

// Backup returns the caller's locked copies and account record. Any
// complete web or app session of the account may read them; none of it
// opens without a way of unlocking.
func (s *Service) Backup(ctx context.Context, c Caller) (model.Backup, error) {
	if err := person(c); err != nil {
		return model.Backup{}, err
	}
	b, found, err := s.store.Sealed().Backup(ctx, c.Account.ID)
	if err == nil && !found {
		err = model.ErrNotFound.With("This account has no backup yet.")
	}
	return b, err
}

// CreateBackup stores the first backup of an account together with version
// 1 of its device list (section 13.1). If two devices do this at once the
// first is kept and the other is told already_exists.
func (s *Service) CreateBackup(ctx context.Context, c Caller, rec model.AccountRecord, copies []model.LockedCopy, list ListInput) (model.Backup, error) {
	none := model.Backup{}
	if _, err := s.keyed(ctx, s.store, c); err != nil {
		return none, err
	}
	if err := s.enabled(ctx); err != nil {
		return none, err
	}
	if err := checkAccountRecord("account_record", rec); err != nil {
		return none, err
	}
	if rec.KeyVersion != 1 || rec.Revision != 1 {
		return none, model.Invalid("account_record", "A new backup starts at key version 1, revision 1.")
	}
	now := s.now()
	stored := store.SignedDeviceList{AccountID: c.Account.ID, Version: list.Version, CreatedAt: now}
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		for i := range copies {
			if err := s.checkCopy(ctx, tx, c.Account.ID, 1, &copies[i]); err != nil {
				return err
			}
		}
		if err := tx.Sealed().CreateBackup(ctx, c.Account.ID, rec, copies, now); err != nil {
			return err
		}
		signed, l, err := s.checkOwnList(ctx, tx, c.Account.ID, list)
		if err != nil {
			return err
		}
		if l.KeyVersion != 1 {
			return model.Invalid("device_list", "The device list must carry key version 1.")
		}
		stored.Signed = signed
		return tx.Sealed().PutDeviceList(ctx, stored)
	})
	if err != nil {
		return none, err
	}
	s.announceList(ctx, stored)
	s.backupChanged(ctx, c.Account.ID)
	return s.Backup(ctx, c)
}

// AddCopy stores a locked copy, replacing the earlier one for the same way,
// and, when the ways changed, the next revision of the account record.
//
// It needs the session a device is attached to, with one exception: a
// `device` copy may come from any complete browser session, because the
// server's unlock page makes one for an app after a passkey touch.
func (s *Service) AddCopy(ctx context.Context, c Caller, cp model.LockedCopy, rec *model.AccountRecord) (model.Backup, error) {
	none := model.Backup{}
	if err := person(c); err != nil {
		return none, err
	}
	if !(cp.Kind == model.CopyDevice && c.Session.Kind == model.SessionWeb) {
		if _, err := s.device(ctx, s.store, c); err != nil {
			return none, err
		}
	}
	if rec != nil {
		if err := checkAccountRecord("account_record", *rec); err != nil {
			return none, err
		}
	}
	now := s.now()
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		b, found, err := tx.Sealed().Backup(ctx, c.Account.ID)
		if err != nil {
			return err
		}
		if !found {
			return model.ErrNotFound.With("This account has no backup yet.")
		}
		if err := s.checkCopy(ctx, tx, c.Account.ID, b.KeyVersion, &cp); err != nil {
			return err
		}
		if rec != nil {
			if err := tx.Sealed().PutAccountRecord(ctx, c.Account.ID, *rec, now); err != nil {
				return err
			}
		}
		cp.ID, cp.CreatedAt = "", now
		return tx.Sealed().PutLockedCopy(ctx, &cp)
	})
	if err != nil {
		return none, err
	}
	s.backupChanged(ctx, c.Account.ID)
	return s.Backup(ctx, c)
}

// PutAccountRecord stores the next revision of the account record, when
// the ways of unlocking change without a new locked copy.
func (s *Service) PutAccountRecord(ctx context.Context, c Caller, rec model.AccountRecord) (model.Backup, error) {
	none := model.Backup{}
	if _, err := s.device(ctx, s.store, c); err != nil {
		return none, err
	}
	if err := checkAccountRecord("record", rec); err != nil {
		return none, err
	}
	if err := s.store.Sealed().PutAccountRecord(ctx, c.Account.ID, rec, s.now()); err != nil {
		return none, err
	}
	s.backupChanged(ctx, c.Account.ID)
	return s.Backup(ctx, c)
}

// DeleteCopy removes one locked copy of the caller's account.
func (s *Service) DeleteCopy(ctx context.Context, c Caller, id string) error {
	if _, err := s.device(ctx, s.store, c); err != nil {
		return err
	}
	if err := s.store.Sealed().DeleteLockedCopy(ctx, c.Account.ID, id); err != nil {
		return err
	}
	s.backupChanged(ctx, c.Account.ID)
	return nil
}

// Records lists the caller's backup records, for one chat if conv is set.
func (s *Service) Records(ctx context.Context, c Caller, conv model.ConversationID, p store.Page) ([]model.BackupRecord, string, error) {
	if _, err := s.device(ctx, s.store, c); err != nil {
		return nil, "", err
	}
	return s.store.Sealed().BackupRecords(ctx, c.Account.ID, conv, p)
}

// PutRecord stores a backup record for one of the caller's sealed chats and
// one key epoch. The first is kept and later ones ignored: created is
// false and the record returned is the one already stored.
func (s *Service) PutRecord(ctx context.Context, c Caller, r model.BackupRecord) (model.BackupRecord, bool, error) {
	none := model.BackupRecord{}
	if _, err := s.device(ctx, s.store, c); err != nil {
		return none, false, err
	}
	st, err := s.perm.Can(ctx, c.Account, perm.Read, r.ConversationID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			err = model.ErrNotFound
		}
		return none, false, err
	}
	conv := st.Conversation
	if !conv.Sealed() || conv.KeyEpoch == nil || r.KeyEpoch < 1 || r.KeyEpoch > *conv.KeyEpoch {
		return none, false, model.ErrNotFound.With("That chat has no such key epoch.")
	}
	if len(r.Record) != backupRecordSize {
		return none, false, model.Invalid("record", "A backup record is 97 bytes.")
	}
	b, found, err := s.store.Sealed().Backup(ctx, c.Account.ID)
	if err != nil {
		return none, false, err
	}
	if !found {
		return none, false, model.ErrNotFound.With("This account has no backup yet.")
	}
	if r.KeyVersion != b.KeyVersion {
		return none, false, model.Invalid("key_version", "key_version is not the backup's current key version.")
	}
	r.AccountID, r.CreatedAt = c.Account.ID, s.now()
	return s.store.Sealed().PutBackupRecord(ctx, r)
}

// MaxReplaceRecords bounds how many backup records one replacement may
// carry.
const MaxReplaceRecords = 100000

// Replace swaps the caller's whole backup for one under a new backup key
// and account key, together with the device list version signed by the
// new account key (section 13.6). A second replacement of the same key
// version is refused with `conflict`.
func (s *Service) Replace(ctx context.Context, c Caller, replaces int64, rec model.AccountRecord, copies []model.LockedCopy, records []model.BackupRecord, list ListInput) (model.Backup, error) {
	none := model.Backup{}
	if _, err := s.keyed(ctx, s.store, c); err != nil {
		return none, err
	}
	if err := checkAccountRecord("account_record", rec); err != nil {
		return none, err
	}
	if rec.KeyVersion != replaces+1 {
		return none, model.Invalid("account_record", "The new key version is one above the one it replaces.")
	}
	if len(records) > MaxReplaceRecords {
		return none, model.ErrTooLarge.With("That is more backup records than one replacement may carry.")
	}
	for _, r := range records {
		if !model.ValidID(string(r.ConversationID)) || r.KeyEpoch < 1 || len(r.Record) != backupRecordSize || r.KeyVersion != rec.KeyVersion {
			return none, model.Invalid("records", "Every backup record names its chat and key epoch, carries the new key version, and is 97 bytes.")
		}
	}
	stored, err := s.swap(ctx, c, replaces, rec, copies, records, list, false)
	if err != nil {
		return none, err
	}
	s.announceList(ctx, stored)
	s.backupChanged(ctx, c.Account.ID)
	return s.Backup(ctx, c)
}

// Reset starts the caller's account again when no way of unlocking is left
// (section 13.8): a new backup with the next key version and a device list
// marked `reset` that holds only the calling device. The old locked
// copies, account record and backup records are deleted. The API has asked
// for a fresh check before calling this.
func (s *Service) Reset(ctx context.Context, c Caller, rec model.AccountRecord, copies []model.LockedCopy, list ListInput) (model.Backup, error) {
	none := model.Backup{}
	if _, err := s.keyed(ctx, s.store, c); err != nil {
		return none, err
	}
	if err := checkAccountRecord("account_record", rec); err != nil {
		return none, err
	}
	stored, err := s.swap(ctx, c, rec.KeyVersion-1, rec, copies, nil, list, true)
	if err != nil {
		return none, err
	}
	s.announceList(ctx, stored)
	s.backupChanged(ctx, c.Account.ID)
	return s.Backup(ctx, c)
}

// swap is what Replace and Reset share: in one transaction the backup rows
// are swapped and the device list version that goes with them is stored.
func (s *Service) swap(ctx context.Context, c Caller, replaces int64, rec model.AccountRecord, copies []model.LockedCopy, records []model.BackupRecord, list ListInput, reset bool) (store.SignedDeviceList, error) {
	now := s.now()
	stored := store.SignedDeviceList{AccountID: c.Account.ID, Version: list.Version, CreatedAt: now}
	dev, err := s.keyed(ctx, s.store, c)
	if err != nil {
		return stored, err
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		signed, l, err := s.checkOwnList(ctx, tx, c.Account.ID, list)
		if err != nil {
			return err
		}
		if int64(l.KeyVersion) != rec.KeyVersion {
			return model.Invalid("device_list", "The device list must carry the new key version.")
		}
		if l.Reset != reset {
			if reset {
				return model.Invalid("device_list", "A reset needs a device list marked reset.")
			}
			return model.Invalid("device_list", "A replacement's device list is a key change, not a reset.")
		}
		if reset && (len(l.Devices) != 1 || l.Devices[0].ID != dev.ID) {
			return model.Invalid("device_list", "A reset's device list holds only the device that makes it.")
		}
		for i := range copies {
			if err := s.checkCopy(ctx, tx, c.Account.ID, rec.KeyVersion, &copies[i]); err != nil {
				return err
			}
		}
		_, found, err := tx.Sealed().Backup(ctx, c.Account.ID)
		if err != nil {
			return err
		}
		if !found && reset {
			// The rows were lost with everything else; the list alone
			// says which key version this follows.
			err = tx.Sealed().CreateBackup(ctx, c.Account.ID, rec, copies, now)
		} else {
			err = tx.Sealed().ReplaceBackup(ctx, c.Account.ID, replaces, rec, copies, records, now)
		}
		if err != nil {
			return err
		}
		stored.Signed = signed
		return tx.Sealed().PutDeviceList(ctx, stored)
	})
	return stored, err
}

// MarkReplacementDue records that the account's backup key must be
// replaced by the next unlocked device: a passkey that had a locked copy
// was removed, or a device was revoked, where no unlocked device was at
// hand to do it there and then (section 13.6). The account's devices are
// told.
func (s *Service) MarkReplacementDue(ctx context.Context, account model.AccountID) error {
	changed, err := s.store.Sealed().SetReplacementDue(ctx, account, true)
	if err != nil || !changed {
		return err
	}
	s.backupChanged(ctx, account)
	return nil
}

// withDevice attaches the asking device to an unlock request, so the
// device that answers can say which one it is unlocking.
func (s *Service) withDevice(ctx context.Context, r model.UnlockRequest) model.UnlockRequest {
	if d, err := s.store.Devices().Get(ctx, r.DeviceID); err == nil {
		d.PushAddress, d.WakeRoute = "", ""
		r.Device = &d
	}
	return r
}

// CreateUnlockRequest stores a new, locked device's request for the backup
// key and tells the account's other connections (section 13.7).
func (s *Service) CreateUnlockRequest(ctx context.Context, c Caller, request []byte) (model.UnlockRequest, error) {
	none := model.UnlockRequest{}
	dev, err := s.keyed(ctx, s.store, c)
	if err != nil {
		return none, err
	}
	if err := s.enabled(ctx); err != nil {
		return none, err
	}
	if len(request) != unlockRequestSize && len(request) != unlockRequestSize+pairingMACSize {
		return none, model.Invalid("request", "That is not an unlock request.")
	}
	u, err := parseUnlockRequest(request[:unlockRequestSize])
	if err != nil {
		return none, model.Invalid("request", "That is not an unlock request, or its proof does not verify.")
	}
	if u.AccountID != c.Account.ID || u.DeviceID != dev.ID || string(u.SignKey) != string(dev.SignKey) ||
		string(u.BoxKey) != string(dev.BoxKey) || platformNames[u.Platform] != dev.Platform {
		return none, model.Invalid("request", "The request must be for this device, with the keys it registered.")
	}
	now := s.now()
	r := model.UnlockRequest{AccountID: c.Account.ID, DeviceID: dev.ID, Request: request, CreatedAt: now, ExpiresAt: now.Add(unlockRequestLife)}
	if err := s.store.Sealed().CreateUnlockRequest(ctx, &r); err != nil {
		return none, err
	}
	r = s.withDevice(ctx, r)
	s.hub.SendExceptSession(c.Account.ID, c.Session.ID, s.event(model.EventUnlockRequestCreated, map[string]any{"unlock_request": r}))
	return r, nil
}

// UnlockRequests lists the account's waiting unlock requests.
func (s *Service) UnlockRequests(ctx context.Context, c Caller) ([]model.UnlockRequest, error) {
	if err := person(c); err != nil {
		return nil, err
	}
	list, err := s.store.Sealed().UnlockRequests(ctx, c.Account.ID, s.now())
	out := make([]model.UnlockRequest, 0, len(list))
	for _, r := range list {
		out = append(out, s.withDevice(ctx, r))
	}
	return out, err
}

// UnlockRequest returns one of the account's unlock requests. The answer is
// given only to the session of the device that asked.
func (s *Service) UnlockRequest(ctx context.Context, c Caller, id string) (model.UnlockRequest, error) {
	if err := person(c); err != nil {
		return model.UnlockRequest{}, err
	}
	r, err := s.store.Sealed().UnlockRequest(ctx, id)
	if err != nil {
		return r, err
	}
	if r.AccountID != c.Account.ID {
		return model.UnlockRequest{}, model.ErrNotFound
	}
	r = s.withDevice(ctx, r)
	if r.Device == nil || r.Device.SessionID != c.Session.ID {
		r.Answer = nil
	}
	return r, nil
}

// AnswerUnlockRequest stores an unlocked device's answer to an unlock
// request: a locked copy of kind `device` for the device that asked. The
// answering device has first checked the code the new device shows against
// the request it received; the server cannot make that check and takes no
// part in it.
func (s *Service) AnswerUnlockRequest(ctx context.Context, c Caller, id string, cp model.LockedCopy) (model.UnlockRequest, error) {
	none := model.UnlockRequest{}
	if _, err := s.device(ctx, s.store, c); err != nil {
		return none, err
	}
	r, err := s.store.Sealed().UnlockRequest(ctx, id)
	if err != nil {
		return none, err
	}
	if r.AccountID != c.Account.ID {
		return none, model.ErrNotFound
	}
	if cp.Kind != model.CopyDevice || cp.DeviceID != r.DeviceID {
		return none, model.Invalid("locked_copy", "The answer is a locked copy of kind device for the device that asked.")
	}
	now := s.now()
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		b, found, err := tx.Sealed().Backup(ctx, c.Account.ID)
		if err != nil {
			return err
		}
		if !found {
			return model.ErrConflict.With("This account has no backup to unlock.")
		}
		if err := s.checkCopy(ctx, tx, c.Account.ID, b.KeyVersion, &cp); err != nil {
			return err
		}
		if r, err = tx.Sealed().AnswerUnlockRequest(ctx, id, cp.LockedKey, now); err != nil {
			return err
		}
		cp.ID, cp.CreatedAt = "", now
		return tx.Sealed().PutLockedCopy(ctx, &cp)
	})
	if err != nil {
		return none, err
	}
	r = s.withDevice(ctx, r)
	if r.Device != nil && r.Device.SessionID != "" {
		s.hub.SendToSession(c.Account.ID, r.Device.SessionID, s.event(model.EventUnlockRequestAnswered, map[string]any{"unlock_request": r}))
	}
	s.backupChanged(ctx, c.Account.ID)
	// The answering device is not the asking one: it is not given the
	// answer back.
	r.Answer = nil
	return r, nil
}

// SetPasskeyUnlockKey stores the unlock public key a page derived from a
// passkey's PRF output (section 13.4). The passkey can then be given a
// locked copy by an unlocked device, once its owner has said yes. A key
// that differs from the one stored makes the passkey's existing copy
// useless, so that copy is deleted. The account's devices are told.
func (s *Service) SetPasskeyUnlockKey(ctx context.Context, c Caller, id model.PasskeyID, key []byte) (model.Passkey, error) {
	none := model.Passkey{}
	if err := person(c); err != nil {
		return none, err
	}
	if len(key) != 32 {
		return none, model.Invalid("unlock_public", "unlock_public is a 32-byte X25519 public key.")
	}
	pk, err := s.store.Passkeys().Get(ctx, id)
	if err != nil {
		return none, err
	}
	if pk.AccountID != c.Account.ID {
		return none, model.ErrNotFound
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if len(pk.UnlockPublic) != 0 && string(pk.UnlockPublic) != string(key) {
			b, found, err := tx.Sealed().Backup(ctx, c.Account.ID)
			if err != nil {
				return err
			}
			for _, cp := range b.LockedCopies {
				if found && cp.Kind == model.CopyPasskey && cp.PasskeyID == id {
					if err := tx.Sealed().DeleteLockedCopy(ctx, c.Account.ID, cp.ID); err != nil {
						return err
					}
				}
			}
		}
		return tx.Passkeys().SetPRF(ctx, id, true, key)
	})
	if err != nil {
		return none, err
	}
	// Unlocked devices ask their owner whether this passkey may unlock.
	if b, found, err := s.store.Sealed().Backup(ctx, c.Account.ID); err == nil && found {
		s.hub.Send(c.Account.ID, s.event(model.EventBackupChanged, map[string]any{"key_version": b.KeyVersion, "replacement_due": b.ReplacementDue}))
	}
	return s.store.Passkeys().Get(ctx, id)
}

// Listed returns the devices in the newest version of an account's device
// list. A retired device stays in the account's device list until an
// unlocked device has published a version without it.
func (s *Service) Listed(ctx context.Context, account model.AccountID) (map[model.DeviceID]bool, error) {
	out := map[model.DeviceID]bool{}
	newest, found, err := s.store.Sealed().NewestDeviceList(ctx, account)
	if err != nil || !found {
		return out, err
	}
	signed, err := parseSignedList(newest.Signed)
	if err != nil {
		return out, nil
	}
	l, err := parseDeviceList(signed.List)
	if err != nil {
		return out, nil
	}
	for _, d := range l.Devices {
		out[d.ID] = true
	}
	return out, nil
}
