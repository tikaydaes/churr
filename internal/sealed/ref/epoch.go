package ref

import (
	"crypto/ed25519"
	"encoding/binary"
	"fmt"
	"sort"
)

// Key epochs (docs/SEALED.md section 5): the record that announces one,
// what is derived from its secret, and the checks a device makes before it
// accepts one.

// AccountRef says which device list of an account the maker used.
type AccountRef struct {
	AccountID   UUID
	ListVersion uint64   // 0 if the account has no list yet
	ListHash    [32]byte // zero bytes if ListVersion is 0
}

// Recipient is one device the epoch secret is sealed to.
type Recipient struct {
	AccountID UUID
	DeviceID  UUID
	// Sealed is seal(that device's box_key, "churr seal epoch v1",
	// conversation_id || key_epoch || device_id, epoch secret): 80 bytes.
	Sealed []byte
}

// EpochRecord is the record of section 5.1.
type EpochRecord struct {
	Format         byte
	ConversationID UUID
	KeyEpoch       uint64
	Prev           [32]byte
	MakerAccountID UUID
	MakerDeviceID  UUID
	Accounts       []AccountRef // exactly two, sorted by account_id
	Check          [32]byte
	Recipients     []Recipient // sorted by device_id
	At             uint64
}

const (
	accountRefSize = 16 + 8 + 32
	recipientSize  = 16 + 16 + 80
	// SignatureSize is the 64 bytes that follow an epoch record in its
	// stored form, and a sealed message's content.
	SignatureSize = 64
)

// Encode writes the record, without the maker's signature.
func (e *EpochRecord) Encode() ([]byte, error) {
	if len(e.Accounts) != 2 {
		return nil, ErrCount
	}
	var w Writer
	w.U8(e.Format)
	w.UUID(e.ConversationID)
	w.U64(e.KeyEpoch)
	w.Raw(e.Prev[:])
	w.UUID(e.MakerAccountID)
	w.UUID(e.MakerDeviceID)
	w.U32(uint32(len(e.Accounts)))
	for i, a := range e.Accounts {
		if i > 0 && compare(e.Accounts[i-1].AccountID[:], a.AccountID[:]) >= 0 {
			return nil, ErrOrder
		}
		w.UUID(a.AccountID)
		w.U64(a.ListVersion)
		w.Raw(a.ListHash[:])
	}
	w.Raw(e.Check[:])
	w.U32(uint32(len(e.Recipients)))
	for i, r := range e.Recipients {
		if len(r.Sealed) != 80 {
			return nil, fmt.Errorf("ref: a sealed epoch secret is 80 bytes")
		}
		if i > 0 && compare(e.Recipients[i-1].DeviceID[:], r.DeviceID[:]) >= 0 {
			return nil, ErrOrder
		}
		w.UUID(r.AccountID)
		w.UUID(r.DeviceID)
		w.Raw(r.Sealed)
	}
	w.Time(e.At)
	return w.Bytes()
}

// DecodeEpochRecord reads a record without its signature.
func DecodeEpochRecord(b []byte) (*EpochRecord, error) {
	r := NewReader(b)
	e := &EpochRecord{}
	e.Format = r.Choice(1)
	e.ConversationID = r.UUID()
	e.KeyEpoch = r.U64()
	copy(e.Prev[:], r.Raw(32))
	e.MakerAccountID = r.UUID()
	e.MakerDeviceID = r.UUID()
	n := r.Count(accountRefSize)
	if n != 2 {
		r.Fail(ErrCount)
	}
	for i := 0; i < n && r.Err() == nil; i++ {
		var a AccountRef
		a.AccountID = r.UUID()
		a.ListVersion = r.U64()
		copy(a.ListHash[:], r.Raw(32))
		if i > 0 && compare(e.Accounts[i-1].AccountID[:], a.AccountID[:]) >= 0 {
			r.Fail(ErrOrder)
		}
		e.Accounts = append(e.Accounts, a)
	}
	copy(e.Check[:], r.Raw(32))
	n = r.Count(recipientSize)
	for i := 0; i < n && r.Err() == nil; i++ {
		var rc Recipient
		rc.AccountID = r.UUID()
		rc.DeviceID = r.UUID()
		rc.Sealed = r.Raw(80)
		if i > 0 && compare(e.Recipients[i-1].DeviceID[:], rc.DeviceID[:]) >= 0 {
			r.Fail(ErrOrder)
		}
		e.Recipients = append(e.Recipients, rc)
	}
	e.At = r.U64()
	if err := r.Done(); err != nil {
		return nil, err
	}
	return e, nil
}

// Recipient finds a device among the recipients.
func (e *EpochRecord) Recipient(device UUID) (Recipient, bool) {
	for _, r := range e.Recipients {
		if r.DeviceID == device {
			return r, true
		}
	}
	return Recipient{}, false
}

// HasAccount reports whether any recipient belongs to the account.
func (e *EpochRecord) HasAccount(account UUID) bool {
	for _, r := range e.Recipients {
		if r.AccountID == account {
			return true
		}
	}
	return false
}

// Ref returns the account reference for one account.
func (e *EpochRecord) Ref(account UUID) (AccountRef, bool) {
	for _, a := range e.Accounts {
		if a.AccountID == account {
			return a, true
		}
	}
	return AccountRef{}, false
}

// SplitStoredEpoch splits the stored form into the record and the maker's
// signature that follows it.
func SplitStoredEpoch(stored []byte) (record, sig []byte, err error) {
	if len(stored) > MaxRecord+SignatureSize {
		return nil, nil, ErrTooLong
	}
	if len(stored) < SignatureSize {
		return nil, nil, ErrShort
	}
	return stored[:len(stored)-SignatureSize], stored[len(stored)-SignatureSize:], nil
}

// EpochHash is the hash of an epoch's stored bytes, signature included,
// which the next epoch names as `prev`.
func EpochHash(stored []byte) [32]byte { return Hash(LabelEpochHash, stored) }

// epochSalt is the salt of every derivation of section 5.2:
// conversation_id || key_epoch, 24 bytes.
func epochSalt(conv UUID, keyEpoch uint64) []byte {
	return binary.BigEndian.AppendUint64(append([]byte{}, conv[:]...), keyEpoch)
}

// HistorySecret derives the history secret from the epoch secret. It is
// what a device keeps for an epoch and what goes in the backup.
func HistorySecret(epochSecret []byte, conv UUID, keyEpoch uint64) []byte {
	return HKDF(epochSecret, epochSalt(conv, keyEpoch), []byte(LabelHistory), 32)
}

// ContentKey derives the content key from the history secret. It never
// encrypts directly.
func ContentKey(history []byte, conv UUID, keyEpoch uint64) []byte {
	return HKDF(history, epochSalt(conv, keyEpoch), []byte(LabelContent), 32)
}

// EpochCheck derives the value published in the epoch record, against
// which a secret from a backup or from the other person is checked.
func EpochCheck(history []byte, conv UUID, keyEpoch uint64) [32]byte {
	var out [32]byte
	copy(out[:], HKDF(history, epochSalt(conv, keyEpoch), []byte(LabelEpochCheck), 32))
	return out
}

// SenderKey derives one device's sender key from the content key.
func SenderKey(contentKey []byte, conv UUID, keyEpoch uint64, device UUID) []byte {
	return HKDF(contentKey, epochSalt(conv, keyEpoch), labelled(LabelSender, device[:]), 32)
}

// EpochSealContext is the context an epoch secret is sealed under for one
// device: conversation_id || key_epoch || device_id.
func EpochSealContext(conv UUID, keyEpoch uint64, device UUID) []byte {
	return append(epochSalt(conv, keyEpoch), device[:]...)
}

// EpochList names one account's device list for MakeEpoch: the list record
// of the version to seal to, or nil when the account has no list yet.
type EpochList struct {
	AccountID UUID
	List      []byte
}

// NewEpoch is what MakeEpoch builds a record from.
type NewEpoch struct {
	ConversationID UUID
	KeyEpoch       uint64
	// Prev is the hash of the previous epoch's stored record; zero for
	// epoch 1.
	Prev           [32]byte
	MakerAccountID UUID
	MakerDeviceID  UUID
	MakerSign      ed25519.PrivateKey
	// Lists are the two accounts' lists, in any order.
	Lists [2]EpochList
	// Secret is the fresh 32-byte epoch secret.
	Secret []byte
	// OneTime gives the 32-byte one-time private key for sealing to each
	// recipient device.
	OneTime func(device UUID) []byte
	At      uint64
}

// MakeEpoch builds and signs an epoch record (section 5.3) and returns its
// stored form: the record followed by the maker's signature. The device set
// is not chosen: it is every device in the two lists.
func MakeEpoch(n NewEpoch) ([]byte, error) {
	if len(n.Secret) != 32 {
		return nil, fmt.Errorf("ref: an epoch secret is 32 bytes")
	}
	e := EpochRecord{Format: 1, ConversationID: n.ConversationID, KeyEpoch: n.KeyEpoch, Prev: n.Prev,
		MakerAccountID: n.MakerAccountID, MakerDeviceID: n.MakerDeviceID, At: n.At}
	for _, in := range n.Lists {
		ref := AccountRef{AccountID: in.AccountID}
		if in.List != nil {
			l, err := DecodeDeviceList(in.List)
			if err != nil {
				return nil, err
			}
			if l.AccountID != in.AccountID {
				return nil, fmt.Errorf("ref: a list is not that account's")
			}
			ref.ListVersion, ref.ListHash = l.Version, ListHash(in.List)
			for _, d := range l.Devices {
				sealed, err := Seal(d.BoxKey, LabelSealEpoch, EpochSealContext(n.ConversationID, n.KeyEpoch, d.DeviceID), n.Secret, n.OneTime(d.DeviceID))
				if err != nil {
					return nil, err
				}
				e.Recipients = append(e.Recipients, Recipient{AccountID: in.AccountID, DeviceID: d.DeviceID, Sealed: sealed})
			}
		}
		e.Accounts = append(e.Accounts, ref)
	}
	sort.Slice(e.Accounts, func(i, j int) bool { return compare(e.Accounts[i].AccountID[:], e.Accounts[j].AccountID[:]) < 0 })
	sort.Slice(e.Recipients, func(i, j int) bool { return compare(e.Recipients[i].DeviceID[:], e.Recipients[j].DeviceID[:]) < 0 })
	e.Check = EpochCheck(HistorySecret(n.Secret, n.ConversationID, n.KeyEpoch), n.ConversationID, n.KeyEpoch)
	record, err := e.Encode()
	if err != nil {
		return nil, err
	}
	return append(record, Sign(n.MakerSign, LabelEpoch, record)...), nil
}

// ErrEpoch is the refusal of an epoch record. A record that fails is not
// accepted: the device uses no key from it, draws no message sent in it,
// shows "This chat's keys are out of step", and makes the next epoch itself.
type ErrEpoch struct {
	// Rule is the number of the rule of section 5.4 that failed, 1 to 7.
	Rule int
	Why  string
}

func (e *ErrEpoch) Error() string {
	return fmt.Sprintf("ref: epoch record refused (rule %d): %s", e.Rule, e.Why)
}

func epochErr(rule int, why string) error { return &ErrEpoch{Rule: rule, Why: why} }

// EpochCheckInput is what a device checks an epoch record against.
type EpochCheckInput struct {
	// Stored is the record with its signature, exactly as received.
	Stored []byte
	// ConversationID and Accounts are the chat's.
	ConversationID UUID
	Accounts       [2]UUID
	// Previous is the stored record before this one in the chat, whether
	// or not the device accepted it; nil if this is the oldest record the
	// device was given, which is then taken as the starting point.
	Previous []byte
	// List returns the list record of one version of one account, once
	// the device has checked that account's chain up to it; nil if it has
	// not.
	List func(account UUID, version uint64) []byte
	// DeviceID and Box are the checking device's own, for rule 7. Box may
	// be nil for a device that only checks the chain (a device that has
	// just joined does this for epochs made before it existed).
	DeviceID UUID
	Box      []byte
}

// AcceptedEpoch is an epoch record that passed, and what the device got
// from it.
type AcceptedEpoch struct {
	Record *EpochRecord
	// Devices maps each recipient device to its entry in the list version
	// the record names, which is where the signing keys for the epoch's
	// messages are taken from.
	Devices map[UUID]ListDevice
	// Secret is the epoch secret, if the checking device is a recipient.
	Secret []byte
}

// CheckEpochRecord applies the seven rules of section 5.4. A device checks
// them itself and does not rely on the server having done so.
func CheckEpochRecord(in EpochCheckInput) (*AcceptedEpoch, error) {
	// 1. It parses, with nothing left over, and is this chat's.
	record, sig, err := SplitStoredEpoch(in.Stored)
	if err != nil {
		return nil, epochErr(1, err.Error())
	}
	e, err := DecodeEpochRecord(record)
	if err != nil {
		return nil, epochErr(1, err.Error())
	}
	if e.ConversationID != in.ConversationID {
		return nil, epochErr(1, "the record is for another chat")
	}
	// 2. It is the next number and names the hash of the one before.
	var before *EpochRecord
	if in.Previous != nil {
		if prec, _, err := SplitStoredEpoch(in.Previous); err == nil {
			before, _ = DecodeEpochRecord(prec)
		}
		if before != nil && e.KeyEpoch != before.KeyEpoch+1 {
			return nil, epochErr(2, "the key epoch is not one above the previous record's")
		}
		if e.Prev != EpochHash(in.Previous) {
			return nil, epochErr(2, "prev is not the hash of the previous record")
		}
	} else if e.KeyEpoch == 0 {
		return nil, epochErr(2, "a chat's first key epoch is 1")
	} else if e.KeyEpoch == 1 && e.Prev != [32]byte{} {
		return nil, epochErr(2, "epoch 1 has a previous hash")
	}
	// 3. It names exactly the chat's two accounts.
	want := in.Accounts
	if compare(want[0][:], want[1][:]) > 0 {
		want[0], want[1] = want[1], want[0]
	}
	if e.Accounts[0].AccountID != want[0] || e.Accounts[1].AccountID != want[1] {
		return nil, epochErr(3, "the record does not name the chat's two accounts")
	}
	// 4. The lists it names are ones the device has checked, with those
	// hashes, and never older than the previous record named.
	devices := map[UUID]ListDevice{}
	owner := map[UUID]UUID{}
	for _, a := range e.Accounts {
		if before != nil {
			if was, ok := before.Ref(a.AccountID); ok && a.ListVersion < was.ListVersion {
				return nil, epochErr(4, "a device list went backwards")
			}
		}
		if a.ListVersion == 0 {
			if a.ListHash != [32]byte{} {
				return nil, epochErr(4, "an account with no list has a list hash")
			}
			continue
		}
		raw := in.List(a.AccountID, a.ListVersion)
		if raw == nil {
			return nil, epochErr(4, "the device has not checked a device list the record names")
		}
		if ListHash(raw) != a.ListHash {
			return nil, epochErr(4, "a list hash is not the hash of that version")
		}
		l, err := DecodeDeviceList(raw)
		if err != nil {
			return nil, epochErr(4, "a device list does not parse")
		}
		for _, d := range l.Devices {
			devices[d.DeviceID] = d
			owner[d.DeviceID] = a.AccountID
		}
	}
	// 5. The recipients are exactly those lists' devices, each under its
	// right account.
	if len(e.Recipients) != len(devices) {
		return nil, epochErr(5, "the recipients are not exactly the devices of the two lists")
	}
	for _, r := range e.Recipients {
		if acc, ok := owner[r.DeviceID]; !ok || acc != r.AccountID {
			return nil, epochErr(5, "the recipients are not exactly the devices of the two lists")
		}
	}
	// 6. The maker is a recipient, and signed.
	maker, ok := devices[e.MakerDeviceID]
	if !ok || owner[e.MakerDeviceID] != e.MakerAccountID {
		return nil, epochErr(6, "the maker is not one of the recipients")
	}
	if !Verify(maker.SignKey, LabelEpoch, record, sig) {
		return nil, epochErr(6, "the maker's signature does not verify")
	}
	out := &AcceptedEpoch{Record: e, Devices: devices}
	// 7. If the device is a recipient, its sealed value opens to the
	// secret the record announces.
	if mine, ok := e.Recipient(in.DeviceID); ok && in.Box != nil {
		secret, err := Open(in.Box, LabelSealEpoch, EpochSealContext(e.ConversationID, e.KeyEpoch, in.DeviceID), mine.Sealed)
		if err != nil || len(secret) != 32 {
			return nil, epochErr(7, "the sealed epoch secret does not open")
		}
		if EpochCheck(HistorySecret(secret, e.ConversationID, e.KeyEpoch), e.ConversationID, e.KeyEpoch) != e.Check {
			return nil, epochErr(7, "the secret is not the one the record announces")
		}
		out.Secret = secret
	}
	return out, nil
}

// CheckHistorySecret reports whether a history secret that came from a
// backup or from the other person is the one an epoch record announced.
func CheckHistorySecret(history []byte, e *EpochRecord) bool {
	return len(history) == 32 && EpochCheck(history, e.ConversationID, e.KeyEpoch) == e.Check
}
