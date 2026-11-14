package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Sealed is every sealed-chat table: `device_lists`, `sealed_epochs`,
// `backup_keys`, `account_records`, `key_backups` and `unlock_requests`
// (docs/SEALED.md section 23). The store treats what they hold as opaque
// bytes. It keeps the orderings the document asks of the server: device
// list versions go up by exactly one, a chat has one record per key epoch
// and only the next number is accepted, and an account's backup is swapped
// as a whole.
type Sealed interface {
	// PutDeviceList stores the next version of an account's signed device
	// list. It is accepted only when version is exactly one above the
	// newest stored (1 when there is none); anything else is
	// model.ErrConflict.
	PutDeviceList(ctx context.Context, l SignedDeviceList) error
	// PutFirstDeviceList stores the first version held for an account
	// whatever its number: a remote account's chain is taken up where it
	// stands when the link is made (docs/SEALED.md section 17).
	// model.ErrConflict if the account already has a version.
	PutFirstDeviceList(ctx context.Context, l SignedDeviceList) error
	// DeviceLists returns the versions of an account's list above
	// `after`, in order, at most limit of them.
	DeviceLists(ctx context.Context, account model.AccountID, after int64, limit int) ([]SignedDeviceList, error)
	// DeviceList returns one version. found is false if there is none.
	DeviceList(ctx context.Context, account model.AccountID, version int64) (l SignedDeviceList, found bool, err error)
	// NewestDeviceList returns the newest version. found is false if the
	// account has never published one.
	NewestDeviceList(ctx context.Context, account model.AccountID) (l SignedDeviceList, found bool, err error)
	// NewestDeviceListVersion is the version number alone: 0 for none.
	NewestDeviceListVersion(ctx context.Context, account model.AccountID) (int64, error)

	// PutEpoch stores the record of a chat's next key epoch and makes
	// that epoch the chat's current one, together. It is accepted only
	// when e.KeyEpoch is exactly one above conversations.key_epoch (1
	// while that is null); anything else is model.ErrConflict, and
	// nothing is stored. The caller has already taken e.Seq from the
	// conversation in the same transaction.
	PutEpoch(ctx context.Context, e model.EpochRecord) error
	// SetEpochOrder records, for a chat shared with another server, the
	// order string of the stamp of the event that carried a record.
	SetEpochOrder(ctx context.Context, conv model.ConversationID, keyEpoch int64, ord string) error
	// ReplaceEpoch puts another record in the place of the one held for
	// e.KeyEpoch: the same key epoch, made on another server with an
	// earlier stamp (LINK-PROTOCOL.md section 11). The chat's current key
	// epoch does not move. model.ErrNotFound if no record is held for it.
	ReplaceEpoch(ctx context.Context, e model.EpochRecord) error
	// Epoch returns the record of one key epoch. found is false if the
	// chat has none with that number (it never had, or the janitor took
	// it once its messages had gone).
	Epoch(ctx context.Context, conv model.ConversationID, keyEpoch int64) (e model.EpochRecord, found bool, err error)
	// Epochs returns a chat's records from key epoch `from` on, in
	// order, and whether there are more than limit.
	Epochs(ctx context.Context, conv model.ConversationID, from int64, limit int) (list []model.EpochRecord, hasMore bool, err error)
	// DeleteEmptyEpochs is the janitor's clean-up of docs/SEALED.md
	// section 13.2: in every sealed chat it deletes the records of the
	// oldest key epochs that have no message left, and both accounts'
	// backup records for them, oldest first, stopping at the first epoch
	// that still has a message and never touching the current one, so
	// what remains is an unbroken run. It returns how many epoch records
	// and backup records went.
	DeleteEmptyEpochs(ctx context.Context) (epochs, records int64, err error)

	// Backup returns an account's account record and locked copies.
	// found is false if the account has no backup yet.
	Backup(ctx context.Context, account model.AccountID) (b model.Backup, found bool, err error)
	// CreateBackup stores an account's first account record and locked
	// copies. model.ErrAlreadyExists if the account has a backup: of two
	// devices doing this at once the first is kept.
	CreateBackup(ctx context.Context, account model.AccountID, rec model.AccountRecord, copies []model.LockedCopy, at model.Time) error
	// PutLockedCopy stores a locked copy, replacing the earlier one for
	// the same way (the same passkey, the same device, or the phrase).
	// An empty ID and zero CreatedAt are filled in.
	PutLockedCopy(ctx context.Context, c *model.LockedCopy) error
	// LockedCopy returns one locked copy.
	LockedCopy(ctx context.Context, id string) (model.LockedCopy, error)
	// DeleteLockedCopy removes one locked copy of an account.
	// model.ErrNotFound if the account has none with that id.
	DeleteLockedCopy(ctx context.Context, account model.AccountID, id string) error
	// PutAccountRecord replaces the account record. It is accepted only
	// when rec.Revision is exactly one above the stored one and
	// rec.KeyVersion is the stored one; anything else is
	// model.ErrConflict. model.ErrNotFound if the account has no backup.
	PutAccountRecord(ctx context.Context, account model.AccountID, rec model.AccountRecord, at model.Time) error
	// SetReplacementDue sets or clears the mark that the backup key must
	// be replaced at the next unlock. changed is false if the account has
	// no backup or the mark already had that value.
	SetReplacementDue(ctx context.Context, account model.AccountID, due bool) (changed bool, err error)
	// ReplaceBackup swaps an account's whole backup for one under a new
	// key: the account record, every locked copy and every backup record
	// are deleted and the given ones stored, and "replacement due" is
	// cleared. It is accepted only while the stored key_version is
	// `replaces`; otherwise model.ErrConflict, so the same version cannot
	// be replaced twice. A record for a chat that no longer exists is
	// left out.
	ReplaceBackup(ctx context.Context, account model.AccountID, replaces int64, rec model.AccountRecord, copies []model.LockedCopy, records []model.BackupRecord, at model.Time) error

	// PutBackupRecord stores a backup record for one chat and key epoch.
	// The first one for an account, chat and epoch is kept and later ones
	// are ignored: created is false and stored is the one already there.
	PutBackupRecord(ctx context.Context, r model.BackupRecord) (stored model.BackupRecord, created bool, err error)
	// BackupRecords lists an account's backup records, for one chat if
	// conv is not empty, ordered by chat and key epoch.
	BackupRecords(ctx context.Context, account model.AccountID, conv model.ConversationID, p Page) (list []model.BackupRecord, next string, err error)

	// CreateUnlockRequest stores an unlock request. An empty ID and zero
	// CreatedAt are filled in.
	CreateUnlockRequest(ctx context.Context, r *model.UnlockRequest) error
	// UnlockRequest returns one unlock request, whatever its state.
	UnlockRequest(ctx context.Context, id string) (model.UnlockRequest, error)
	// UnlockRequests returns an account's waiting requests: not answered
	// and not expired at `now`, oldest first.
	UnlockRequests(ctx context.Context, account model.AccountID, now model.Time) ([]model.UnlockRequest, error)
	// AnswerUnlockRequest stores the answer to a request that is still
	// waiting at `now`. model.ErrConflict if it has been answered or has
	// expired.
	AnswerUnlockRequest(ctx context.Context, id string, answer []byte, now model.Time) (model.UnlockRequest, error)

	// EraseMessage deletes a sealed message the way docs/SEALED.md
	// section 8.3 asks: its ciphertext is erased at once, its files are
	// deleted (their blob keys are returned for the caller to remove
	// from the file store), the events that carried it are deleted from
	// the catch-up log, and the row stays as a marker with deleted_at
	// and deleted_by set. No version is kept: the server holds no
	// history of a sealed chat. model.ErrConflict if it is already
	// deleted.
	EraseMessage(ctx context.Context, msg model.MessageID, by model.AccountID, at model.Time) (m model.Message, blobKeys []string, err error)

	// SetHandover stores the sealed hand-over to an app on a one-time
	// code that has not been used or expired at `now`: the backup key
	// sealed to the app's box key by the page that approved it
	// (oauth_codes.handover). model.ErrNotFound if there is no such code.
	SetHandover(ctx context.Context, codeHash, handover []byte, now model.Time) error

	// Partners returns the other account of each sealed chat an account
	// is in: who is told when its device list changes.
	Partners(ctx context.Context, account model.AccountID) ([]model.AccountID, error)
	// Chats returns the sealed chats an account is in.
	Chats(ctx context.Context, account model.AccountID) ([]model.ConversationID, error)
}

// SignedDeviceList is one row of `device_lists`: a version of an account's
// device list as it was signed (docs/SEALED.md section 4.3).
type SignedDeviceList struct {
	AccountID model.AccountID
	Version   int64
	// Signed is the list record with its signatures, exactly as stored.
	Signed    []byte
	CreatedAt model.Time
}
