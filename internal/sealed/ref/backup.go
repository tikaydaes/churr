package ref

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// The key backup (docs/SEALED.md section 13): the account record, backup
// records, locked copies, the unlock keys they are sealed to, and the
// hand-overs to an app.

// The kinds of way a backup key can be unlocked. The first two appear in an
// account record's `ways`; all three in a locked copy's way tag.
const (
	WayPasskey byte = 1
	WayPhrase  byte = 2
	WayDevice  byte = 3
)

// WayNames gives the API's word for each kind of locked copy.
var WayNames = map[byte]string{WayPasskey: "passkey", WayPhrase: "phrase", WayDevice: "device"}

// Way is one public unlock key the backup key may be locked to.
type Way struct {
	Kind         byte   // 1 passkey, 2 phrase
	ID           UUID   // the passkey's identifier; 16 zero bytes for the phrase
	UnlockPublic []byte // X25519 public key, 32 bytes
}

// AccountRecord is the content of the account record of section 13.1: the
// account key, and the account's own authenticated list of the unlock keys
// its backup key may be locked to.
type AccountRecord struct {
	Format            byte
	AccountID         UUID
	KeyVersion        uint32
	Revision          uint64
	AccountKey        []byte // Ed25519 public key
	AccountKeyPrivate []byte // its Ed25519 seed
	Ways              []Way  // sorted by kind, then id
	At                uint64
}

const waySize = 1 + 16 + 32

// wayKey is the sort key of a way: kind, then id.
func wayKey(w Way) []byte { return append([]byte{w.Kind}, w.ID[:]...) }

// Encode writes the record's content.
func (a *AccountRecord) Encode() ([]byte, error) {
	if len(a.AccountKey) != 32 || len(a.AccountKeyPrivate) != 32 {
		return nil, fmt.Errorf("ref: an account key and its seed are 32 bytes each")
	}
	var w Writer
	w.U8(a.Format)
	w.UUID(a.AccountID)
	w.U32(a.KeyVersion)
	w.U64(a.Revision)
	w.Raw(a.AccountKey)
	w.Raw(a.AccountKeyPrivate)
	w.U32(uint32(len(a.Ways)))
	for i, way := range a.Ways {
		if len(way.UnlockPublic) != 32 {
			return nil, fmt.Errorf("ref: an unlock public key is 32 bytes")
		}
		if i > 0 && compare(wayKey(a.Ways[i-1]), wayKey(way)) >= 0 {
			return nil, ErrOrder
		}
		w.U8(way.Kind)
		w.UUID(way.ID)
		w.Raw(way.UnlockPublic)
	}
	w.Time(a.At)
	return w.Bytes()
}

// DecodeAccountRecord reads the record's content.
func DecodeAccountRecord(b []byte) (*AccountRecord, error) {
	r := NewReader(b)
	a := &AccountRecord{}
	a.Format = r.Choice(1)
	a.AccountID = r.UUID()
	a.KeyVersion = r.U32()
	a.Revision = r.U64()
	a.AccountKey = r.Raw(32)
	a.AccountKeyPrivate = r.Raw(32)
	n := r.Count(waySize)
	for i := 0; i < n && r.Err() == nil; i++ {
		var w Way
		w.Kind = r.Choice(WayPasskey, WayPhrase)
		w.ID = r.UUID()
		w.UnlockPublic = r.Raw(32)
		if i > 0 && compare(wayKey(a.Ways[i-1]), wayKey(w)) >= 0 {
			r.Fail(ErrOrder)
		}
		a.Ways = append(a.Ways, w)
	}
	a.At = r.U64()
	if err := r.Done(); err != nil {
		return nil, err
	}
	return a, nil
}

// accountRecordContext is account_id || key_version || revision.
func accountRecordContext(account UUID, keyVersion uint32, revision uint64) []byte {
	b := binary.BigEndian.AppendUint32(append([]byte{}, account[:]...), keyVersion)
	return binary.BigEndian.AppendUint64(b, revision)
}

// LockAccountRecord encrypts an account record under the backup key, as
// stored in account_records.record.
func LockAccountRecord(backupKey []byte, a *AccountRecord, salt []byte) ([]byte, error) {
	content, err := a.Encode()
	if err != nil {
		return nil, err
	}
	return Lock(backupKey, LabelLockAccountRecord, accountRecordContext(a.AccountID, a.KeyVersion, a.Revision), content, salt), nil
}

// OpenAccountRecord opens a stored account record. The key version and
// revision are the ones the server filed it under; the record inside must
// say the same, and its private half must be the seed of its public half.
func OpenAccountRecord(backupKey []byte, account UUID, keyVersion uint32, revision uint64, stored []byte) (*AccountRecord, error) {
	content, err := Unlock(backupKey, LabelLockAccountRecord, accountRecordContext(account, keyVersion, revision), stored)
	if err != nil {
		return nil, err
	}
	a, err := DecodeAccountRecord(content)
	if err != nil {
		return nil, err
	}
	if a.AccountID != account || a.KeyVersion != keyVersion || a.Revision != revision {
		return nil, errors.New("ref: the account record is not the one it was filed as")
	}
	if string(Public(SigningKey(a.AccountKeyPrivate))) != string(a.AccountKey) {
		return nil, errors.New("ref: the account record's two key halves do not belong together")
	}
	return a, nil
}

// BackupRecord is the content of a backup record of section 13.2: one
// history secret, for one sealed chat and one key epoch.
type BackupRecord struct {
	Format         byte
	ConversationID UUID
	KeyEpoch       uint64
	Secret         []byte // the history secret, 32 bytes
	At             uint64
}

// Encode writes the record's 65 bytes of content.
func (b *BackupRecord) Encode() ([]byte, error) {
	if len(b.Secret) != 32 {
		return nil, fmt.Errorf("ref: a history secret is 32 bytes")
	}
	var w Writer
	w.U8(b.Format)
	w.UUID(b.ConversationID)
	w.U64(b.KeyEpoch)
	w.Raw(b.Secret)
	w.Time(b.At)
	return w.Bytes()
}

// DecodeBackupRecord reads the record's content.
func DecodeBackupRecord(raw []byte) (*BackupRecord, error) {
	r := NewReader(raw)
	b := &BackupRecord{}
	b.Format = r.Choice(1)
	b.ConversationID = r.UUID()
	b.KeyEpoch = r.U64()
	b.Secret = r.Raw(32)
	b.At = r.U64()
	if err := r.Done(); err != nil {
		return nil, err
	}
	return b, nil
}

// backupRecordContext is account_id || conversation_id || key_epoch ||
// key_version.
func backupRecordContext(account, conv UUID, keyEpoch uint64, keyVersion uint32) []byte {
	b := append(append([]byte{}, account[:]...), conv[:]...)
	b = binary.BigEndian.AppendUint64(b, keyEpoch)
	return binary.BigEndian.AppendUint32(b, keyVersion)
}

// LockBackupRecord encrypts a backup record under the backup key, as stored
// in key_backups.record: 16 bytes of salt and 81 of ciphertext.
func LockBackupRecord(backupKey []byte, account UUID, keyVersion uint32, b *BackupRecord, salt []byte) ([]byte, error) {
	content, err := b.Encode()
	if err != nil {
		return nil, err
	}
	return Lock(backupKey, LabelLockBackupRecord, backupRecordContext(account, b.ConversationID, b.KeyEpoch, keyVersion), content, salt), nil
}

// OpenBackupRecord opens a stored backup record for the row it was filed
// in. The chat and epoch inside must match the row. The caller then checks
// the secret against the epoch record's `check`.
func OpenBackupRecord(backupKey []byte, account, conv UUID, keyEpoch uint64, keyVersion uint32, stored []byte) (*BackupRecord, error) {
	content, err := Unlock(backupKey, LabelLockBackupRecord, backupRecordContext(account, conv, keyEpoch, keyVersion), stored)
	if err != nil {
		return nil, err
	}
	b, err := DecodeBackupRecord(content)
	if err != nil {
		return nil, err
	}
	if b.ConversationID != conv || b.KeyEpoch != keyEpoch {
		return nil, errors.New("ref: the backup record is not the one it was filed as")
	}
	return b, nil
}

// LockedCopySize is the stored size of a locked copy: the sealed backup key
// (80 bytes) and the account key's signature (64).
const LockedCopySize = 80 + 64

// WayTag is kind (u8) || id (uuid): what names the way a locked copy is
// for. id is the passkey, 16 zero bytes for the phrase, or the device.
func WayTag(kind byte, id UUID) []byte { return append([]byte{kind}, id[:]...) }

// lockedCopyContext is account_id || key_version || way tag.
func lockedCopyContext(account UUID, keyVersion uint32, kind byte, id UUID) []byte {
	b := binary.BigEndian.AppendUint32(append([]byte{}, account[:]...), keyVersion)
	return append(b, WayTag(kind, id)...)
}

// MakeLockedCopy seals the backup key to an unlock key's public half and
// signs the result with the account key (section 13.3).
func MakeLockedCopy(unlockPublic []byte, account UUID, keyVersion uint32, kind byte, id UUID, backupKey []byte, accountKey ed25519.PrivateKey, oneTime []byte) ([]byte, error) {
	ctx := lockedCopyContext(account, keyVersion, kind, id)
	sealed, err := Seal(unlockPublic, LabelSealBackupKey, ctx, backupKey, oneTime)
	if err != nil {
		return nil, err
	}
	return append(sealed, Sign(accountKey, LabelLockedCopy, append(ctx, sealed...))...), nil
}

// OpenLockedCopy opens a locked copy with an unlock key's private half and
// returns the backup key it holds. The copy is not trusted yet: anyone who
// knows the unlock public key could have sealed a key of their own choosing
// to it. The caller opens the account record with the key, takes the
// account key from it, and calls VerifyLockedCopy.
func OpenLockedCopy(unlockPrivate []byte, account UUID, keyVersion uint32, kind byte, id UUID, stored []byte) ([]byte, error) {
	if len(stored) != LockedCopySize {
		return nil, ErrOpen
	}
	key, err := Open(unlockPrivate, LabelSealBackupKey, lockedCopyContext(account, keyVersion, kind, id), stored[:80])
	if err != nil || len(key) != 32 {
		return nil, ErrOpen
	}
	return key, nil
}

// VerifyLockedCopy checks a locked copy's signature under the account key.
func VerifyLockedCopy(accountKey []byte, account UUID, keyVersion uint32, kind byte, id UUID, stored []byte) bool {
	if len(stored) != LockedCopySize {
		return false
	}
	return Verify(accountKey, LabelLockedCopy, append(lockedCopyContext(account, keyVersion, kind, id), stored[:80]...), stored[80:])
}

// PRFInput is the 32 bytes given as the first evaluation input of the
// WebAuthn PRF extension: SHA-256("churr sealed unlock v1"). It is the same
// for every account.
func PRFInput() []byte {
	sum := sha256.Sum256([]byte(LabelSealedUnlock))
	return sum[:]
}

// UnlockKeyFromPRF derives a passkey's unlock private key from its PRF
// output, the account and the WebAuthn credential identifier.
func UnlockKeyFromPRF(prfOutput []byte, account UUID, credentialID []byte) []byte {
	return HKDF(prfOutput, make([]byte, 32), labelled(LabelUnlockPasskey, account[:], credentialID), 32)
}

// UnlockKeyFromPhrase derives the phrase's unlock private key from the 16
// bytes the twelve words stand for. There is no slow function: the phrase
// is 128 random bits, not something a person chose.
func UnlockKeyFromPhrase(entropy []byte, account UUID) []byte {
	return HKDF(entropy, make([]byte, 32), labelled(LabelUnlockPhrase, account[:]), 32)
}

// HandoverSize is the size of the hand-over to an app at sign-in.
const HandoverSize = 80

// SealHandover is what the server's sign-in page makes for an app after a
// passkey sign-in: the backup key sealed to the app's box key, bound to the
// account and to the ASCII bytes of the request's PKCE code challenge.
func SealHandover(appBoxKey []byte, account UUID, codeChallenge string, backupKey, oneTime []byte) ([]byte, error) {
	return Seal(appBoxKey, LabelSealHandover, append(append([]byte{}, account[:]...), codeChallenge...), backupKey, oneTime)
}

// OpenHandover is the app's side of SealHandover.
func OpenHandover(appBox []byte, account UUID, codeChallenge string, handover []byte) ([]byte, error) {
	key, err := Open(appBox, LabelSealHandover, append(append([]byte{}, account[:]...), codeChallenge...), handover)
	if err != nil || len(key) != 32 {
		return nil, ErrOpen
	}
	return key, nil
}

// PairingHandoverSize is the size of the two-layer hand-over of QR pairing.
const PairingHandoverSize = SaltSize + 80 + 16

// SealPairingHandover is the hand-over made when an app is signed in by
// scanning a QR code from an unlocked web chat: the backup key sealed to
// the app's box key, then locked under the pairing secret from the QR code,
// which the server never sees. It is 112 bytes.
func SealPairingHandover(appBoxKey, pairingSecret []byte, account UUID, backupKey, oneTime, salt []byte) ([]byte, error) {
	inner, err := Seal(appBoxKey, LabelSealPairing, account[:], backupKey, oneTime)
	if err != nil {
		return nil, err
	}
	return Lock(pairingSecret, LabelLockPairing, account[:], inner, salt), nil
}

// OpenPairingHandover is the app's side of SealPairingHandover.
func OpenPairingHandover(appBox, pairingSecret []byte, account UUID, handover []byte) ([]byte, error) {
	inner, err := Unlock(pairingSecret, LabelLockPairing, account[:], handover)
	if err != nil {
		return nil, ErrOpen
	}
	key, err := Open(appBox, LabelSealPairing, account[:], inner)
	if err != nil || len(key) != 32 {
		return nil, ErrOpen
	}
	return key, nil
}
