package ref

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"sort"
)

// A Device is one installed app or browser profile playing its whole part
// in sealed chats: its two key pairs, the account's backup key once it is
// unlocked, the device lists it has pinned, and its sealed chats (chat.go).
//
// It talks to nobody. Its methods return the bytes to send to the server
// and take the bytes the server gave back, so the same code is driven by
// the tests through the real API and by the transcript in the vectors.

// Device is the state of one device.
type Device struct {
	// Account, Server and Platform say whose device this is. ID is the
	// identifier the server gave its row.
	Account  UUID
	Server   string
	ID       UUID
	Platform byte
	AddedAt  uint64

	// Sign and Box are the device's own keys. They never leave it and are
	// never backed up.
	Sign ed25519.PrivateKey
	Box  []byte

	// Rand supplies every random value; Now the device's clock in
	// milliseconds. Trace, if set, is told of every random value drawn
	// and what it was for, which is how the vectors list their inputs.
	Rand  io.Reader
	Now   func() uint64
	Trace func(what string, value []byte)

	// FetchLists, if set, is how the device asks its server for versions
	// of an account's device list above `after`, when an epoch record
	// names one it has not checked.
	FetchLists func(account UUID, after uint64) ([]SignedList, error)

	// The unlocked state: the account's backup key and account key, and
	// the account record as last seen. All nil while the device is
	// locked.
	BackupKey  []byte
	KeyVersion uint32
	AccountKey ed25519.PrivateKey
	Record     *AccountRecord

	// Pins holds, per account, the device lists this device has accepted.
	Pins map[UUID]*Pin
	// Chats holds its sealed chats by conversation.
	Chats map[UUID]*Chat
	// Notices are what the device has shown its owner, in order.
	Notices []Notice
}

// NewDevice makes a device and its two key pairs. The keys are made before
// the device signs in; the server's identifier is set once it has one.
func NewDevice(account UUID, server string, platform byte, rnd io.Reader, now func() uint64) *Device {
	d := &Device{Account: account, Server: server, Platform: platform, Rand: rnd, Now: now,
		Pins: map[UUID]*Pin{}, Chats: map[UUID]*Chat{}}
	d.Sign = SigningKey(d.random("device_sign_seed", 32))
	d.Box = d.random("device_box_private", 32)
	d.AddedAt = now()
	return d
}

// random draws n random bytes for one purpose.
func (d *Device) random(what string, n int) []byte {
	b := make([]byte, n)
	if _, err := io.ReadFull(d.Rand, b); err != nil {
		panic("ref: no randomness: " + err.Error())
	}
	if d.Trace != nil {
		d.Trace(what, b)
	}
	return b
}

// SignPublic is the device's Ed25519 public key.
func (d *Device) SignPublic() []byte { return Public(d.Sign) }

// BoxPublic is the device's X25519 public key.
func (d *Device) BoxPublic() []byte { return X25519Public(d.Box) }

// Unlocked reports whether the device holds the account's backup key.
func (d *Device) Unlocked() bool { return d.BackupKey != nil }

// entry is the device's own entry for its account's device list.
func (d *Device) entry() ListDevice {
	return ListDevice{DeviceID: d.ID, SignKey: d.SignPublic(), BoxKey: d.BoxPublic(), Platform: d.Platform, AddedAt: d.AddedAt,
		Proof: DeviceProof(d.Sign, d.Account, d.ID, d.BoxPublic())}
}

// AttachSignature proves the signing key to the server when the same app
// signs in again: a signature over the server's challenge, label
// "churr device attach v1" (section 4.1).
func (d *Device) AttachSignature(challenge []byte) []byte {
	return Sign(d.Sign, LabelDeviceAttach, challenge)
}

// Notice is something a device shows its owner. Notices are drawn by the
// client; they are not messages and the server does not know they were
// shown.
type Notice struct {
	// Kind is device_added, device_removed, key_change, reset,
	// list_refused or out_of_step.
	Kind string
	// Account is whose device list changed, or whose chat it is about.
	Account UUID
	// Platform is the kind of device added.
	Platform byte
	// Conversation is set for out_of_step.
	Conversation UUID
}

// The kinds of notice.
const (
	NoticeDeviceAdded   = "device_added"
	NoticeDeviceRemoved = "device_removed"
	NoticeKeyChange     = "key_change"
	NoticeReset         = "reset"
	NoticeListRefused   = "list_refused"
	NoticeOutOfStep     = "out_of_step"
)

// Pin is what a device keeps for one account: every version of its device
// list it has accepted, the newest of which is the pin proper.
type Pin struct {
	Versions map[uint64]*PinnedList
	Lowest   uint64
	Newest   uint64
	// LastReset is the highest accepted version that was a reset; 0 if
	// none. Nothing from before it is ever shared with the account.
	LastReset uint64
	// Verified is the mark set when the two people compared codes. A
	// reset removes it; Hold is then set until the person acknowledges
	// the notice, and nothing is sent meanwhile.
	Verified bool
	Hold     bool
}

// PinnedList is one accepted version.
type PinnedList struct {
	Signed SignedList
	List   *DeviceList
}

// NewestList returns the newest accepted version, or nil.
func (p *Pin) NewestList() *PinnedList {
	if p == nil {
		return nil
	}
	return p.Versions[p.Newest]
}

// ListOf returns the record of one accepted version of one account's list,
// or nil.
func (d *Device) ListOf(account UUID, version uint64) []byte {
	if p := d.Pins[account]; p != nil {
		if v := p.Versions[version]; v != nil {
			return v.Signed.List
		}
	}
	return nil
}

// LearnLists takes versions of one account's device list from the server
// and checks them (section 4.4).
//
// With no pin, the newest one given is pinned: trust on first use. Versions
// above the pin are accepted one step at a time, each checked against the
// one before, and a notice is shown for each change. Versions below what
// the device holds are history: they are accepted only if they chain,
// step by step, into what it holds, and they move no pin. A version the
// device already holds must be byte for byte the same.
//
// On a refusal the device keeps its pin, shows "The server sent a device
// list that does not check out", and makes no epoch because of it.
func (d *Device) LearnLists(account UUID, lists []SignedList) error {
	err := d.learnLists(account, lists)
	if err != nil {
		d.Notices = append(d.Notices, Notice{Kind: NoticeListRefused, Account: account})
	}
	return err
}

func (d *Device) learnLists(account UUID, lists []SignedList) error {
	type parsed struct {
		s SignedList
		l *DeviceList
	}
	var in []parsed
	for _, s := range lists {
		l, err := checkListAlone(s)
		if err != nil {
			return err
		}
		if l.AccountID != account {
			return listErr(1, "the list is another account's")
		}
		in = append(in, parsed{s, l})
	}
	sort.Slice(in, func(i, j int) bool { return in[i].l.Version < in[j].l.Version })
	if len(in) == 0 {
		return nil
	}
	p := d.Pins[account]
	if p == nil {
		// First contact: the newest list given is pinned.
		first := in[len(in)-1]
		if _, err := CheckFirstList(first.s); err != nil {
			return err
		}
		p = &Pin{Versions: map[uint64]*PinnedList{first.l.Version: {first.s, first.l}}, Lowest: first.l.Version, Newest: first.l.Version}
		if first.l.Reset == 1 {
			p.LastReset = first.l.Version
		}
		d.Pins[account] = p
	}
	// Upwards from the pin.
	for _, x := range in {
		v := x.l.Version
		if have := p.Versions[v]; have != nil {
			if string(have.Signed.List) != string(x.s.List) {
				return listErr(1, "the server sent a different list for a version already accepted")
			}
			continue
		}
		if v < p.Lowest {
			continue // history, below
		}
		if v != p.Newest+1 {
			return listErr(1, "a version is missing between the pin and this list")
		}
		prev := p.Versions[p.Newest]
		l, step, err := CheckListStep(prev.Signed.List, x.s)
		if err != nil {
			return err
		}
		p.Versions[v] = &PinnedList{x.s, l}
		p.Newest = v
		d.noteStep(account, p, prev.List, l, step)
	}
	// Downwards, for history: each older version must step into the one
	// above it.
	for i := len(in) - 1; i >= 0; i-- {
		x := in[i]
		v := x.l.Version
		if v >= p.Lowest {
			continue
		}
		if v != p.Lowest-1 {
			return listErr(1, "a version is missing below what this device holds")
		}
		above := p.Versions[p.Lowest]
		l, step, err := CheckListStep(x.s.List, above.Signed)
		if err != nil {
			return err
		}
		_ = l
		p.Versions[v] = &PinnedList{x.s, x.l}
		p.Lowest = v
		if step == StepReset && above.List.Version > p.LastReset {
			p.LastReset = above.List.Version
		}
	}
	return nil
}

// noteStep shows the notices for one accepted step and applies what a reset
// does to a verified mark.
func (d *Device) noteStep(account UUID, p *Pin, before, after *DeviceList, step string) {
	switch step {
	case StepReset:
		d.Notices = append(d.Notices, Notice{Kind: NoticeReset, Account: account})
		p.LastReset = after.Version
		if p.Verified {
			// Sending is held until the person acknowledges the notice.
			p.Verified, p.Hold = false, true
		}
		return
	case StepKeyChange:
		d.Notices = append(d.Notices, Notice{Kind: NoticeKeyChange, Account: account})
	}
	for _, dev := range after.Devices {
		if _, was := before.Device(dev.DeviceID); !was {
			d.Notices = append(d.Notices, Notice{Kind: NoticeDeviceAdded, Account: account, Platform: dev.Platform})
		}
	}
	for _, dev := range before.Devices {
		if _, still := after.Device(dev.DeviceID); !still {
			d.Notices = append(d.Notices, Notice{Kind: NoticeDeviceRemoved, Account: account})
		}
	}
}

// Acknowledge is the person acknowledging a reset notice for an account,
// which lets the device send again.
func (d *Device) Acknowledge(account UUID) {
	if p := d.Pins[account]; p != nil {
		p.Hold = false
	}
}

// MarkVerified is the person marking another as verified after their
// comparison codes matched. The mark stays on this device.
func (d *Device) MarkVerified(account UUID) {
	if p := d.Pins[account]; p != nil {
		p.Verified = true
	}
}

// CompareCodeWith is the 60 digits this device shows for its chat with
// another account, from the account keys it has pinned for the two.
func (d *Device) CompareCodeWith(other UUID, otherServer string) (string, error) {
	mine, theirs := d.Pins[d.Account].NewestList(), d.Pins[other].NewestList()
	if mine == nil || theirs == nil {
		return "", errors.New("ref: no device list is pinned for one of the two accounts")
	}
	return CompareCode(CompareHalf(mine.List.AccountKey, d.Account, d.Server), CompareHalf(theirs.List.AccountKey, other, otherServer)), nil
}

// ensureList makes sure the device has checked one version of an account's
// list, asking the server for versions if it must.
func (d *Device) ensureList(account UUID, version uint64) bool {
	if version == 0 || d.ListOf(account, version) != nil {
		return true
	}
	if d.FetchLists == nil {
		return false
	}
	lists, err := d.FetchLists(account, 0)
	if err != nil || d.LearnLists(account, lists) != nil {
		return false
	}
	return d.ListOf(account, version) != nil
}

// Copy is one locked copy to store.
type Copy struct {
	Kind   byte
	ID     UUID // the passkey or device; zero for the phrase
	Locked []byte
}

// StoredRecord is one backup record as the server files it.
type StoredRecord struct {
	ConversationID UUID
	KeyEpoch       uint64
	KeyVersion     uint32
	Locked         []byte
}

// Bundle is what a device hands the server in one operation when it
// creates, replaces or resets its account's backup.
type Bundle struct {
	KeyVersion uint32
	// Replaces is the key version a replacement takes the place of.
	Replaces uint32
	Revision uint64
	// AccountRecord is the locked account record.
	AccountRecord []byte
	Copies        []Copy
	// Records are every backup record locked again under the new key,
	// for a replacement.
	Records []StoredRecord
	// ListVersion and List are the device list version that goes with it.
	ListVersion uint64
	List        SignedList
	// Phrase is the twelve words, when new ones were made. They are shown
	// once and never sent to the server.
	Phrase string

	// What the device switches to once the server has accepted.
	backupKey  []byte
	accountKey ed25519.PrivateKey
	record     *AccountRecord
}

// Adopt switches the device to the keys of a bundle the server has
// accepted, and pins the device list that came with it.
func (d *Device) Adopt(b *Bundle) error {
	d.BackupKey, d.AccountKey, d.KeyVersion, d.Record = b.backupKey, b.accountKey, b.KeyVersion, b.record
	return d.LearnLists(d.Account, []SignedList{b.List})
}

// phraseWay is the way entry and unlock key pair of a phrase.
func phraseWay(entropy []byte, account UUID) Way {
	return Way{Kind: WayPhrase, UnlockPublic: X25519Public(UnlockKeyFromPhrase(entropy, account))}
}

func sortWays(ways []Way) {
	sort.Slice(ways, func(i, j int) bool { return compare(wayKey(ways[i]), wayKey(ways[j])) < 0 })
}

// copiesFor makes one locked copy of a backup key for each way in an
// account record and for each device given.
func (d *Device) copiesFor(backupKey []byte, accountKey ed25519.PrivateKey, keyVersion uint32, ways []Way, devices []ListDevice) ([]Copy, error) {
	var out []Copy
	for _, w := range ways {
		locked, err := MakeLockedCopy(w.UnlockPublic, d.Account, keyVersion, w.Kind, w.ID, backupKey, accountKey,
			d.random("locked_copy_one_time:"+WayNames[w.Kind]+":"+w.ID.String(), 32))
		if err != nil {
			return nil, err
		}
		out = append(out, Copy{Kind: w.Kind, ID: w.ID, Locked: locked})
	}
	for _, dev := range devices {
		locked, err := MakeLockedCopy(dev.BoxKey, d.Account, keyVersion, WayDevice, dev.DeviceID, backupKey, accountKey,
			d.random("locked_copy_one_time:device:"+dev.DeviceID.String(), 32))
		if err != nil {
			return nil, err
		}
		out = append(out, Copy{Kind: WayDevice, ID: dev.DeviceID, Locked: locked})
	}
	return out, nil
}

// CreateBackup is what the first device of an account does when it enters
// a sealed chat and finds the account has no backup (section 13.1): it
// makes the backup key, the account key pair and the recovery phrase, and
// version 1 of the device list holding itself.
func (d *Device) CreateBackup() (*Bundle, error) { return d.CreateBackupWithAccessKey(nil) }

// CreateBackupWithAccessKey is CreateBackup for an account that has an
// access key with an unlock public key (ruling R85): the access-key way is
// in the first account record and gets its locked copy at once.
func (d *Device) CreateBackupWithAccessKey(accessKeyPublic []byte) (*Bundle, error) {
	backupKey := d.random("backup_key", 32)
	accountKey := SigningKey(d.random("account_key_seed", 32))
	entropy := d.random("phrase_entropy", 16)
	ways := []Way{phraseWay(entropy, d.Account)}
	if accessKeyPublic != nil {
		ways = append(ways, Way{Kind: WayAccessKey, UnlockPublic: accessKeyPublic})
	}
	rec := &AccountRecord{Format: 1, AccountID: d.Account, KeyVersion: 1, Revision: 1, AccountKey: Public(accountKey),
		AccountKeyPrivate: accountKey.Seed(), Ways: ways, At: d.Now()}
	return d.bundle(backupKey, accountKey, rec, Phrase(entropy), listPlan{version: 1, devices: []ListDevice{d.entry()}}, nil)
}

// listPlan says what the device list of a bundle looks like.
type listPlan struct {
	version uint64
	prev    [32]byte
	reset   bool
	devices []ListDevice
	// previous is the old account key, which also signs a key change.
	previous ed25519.PrivateKey
}

func (d *Device) bundle(backupKey []byte, accountKey ed25519.PrivateKey, rec *AccountRecord, phrase string, lp listPlan, records []StoredRecord) (*Bundle, error) {
	locked, err := LockAccountRecord(backupKey, rec, d.random("account_record_salt", SaltSize))
	if err != nil {
		return nil, err
	}
	// A device copy is made for every listed device but this one, which
	// holds the key already.
	var others []ListDevice
	for _, dev := range lp.devices {
		if dev.DeviceID != d.ID {
			others = append(others, dev)
		}
	}
	copies, err := d.copiesFor(backupKey, accountKey, rec.KeyVersion, rec.Ways, others)
	if err != nil {
		return nil, err
	}
	list, err := d.signedList(lp, rec.KeyVersion, accountKey)
	if err != nil {
		return nil, err
	}
	return &Bundle{KeyVersion: rec.KeyVersion, Revision: rec.Revision, AccountRecord: locked, Copies: copies, Records: records,
		ListVersion: lp.version, List: list, Phrase: phrase, backupKey: backupKey, accountKey: accountKey, record: rec}, nil
}

func (d *Device) signedList(lp listPlan, keyVersion uint32, accountKey ed25519.PrivateKey) (SignedList, error) {
	devices := append([]ListDevice{}, lp.devices...)
	sort.Slice(devices, func(i, j int) bool { return compare(devices[i].DeviceID[:], devices[j].DeviceID[:]) < 0 })
	l := DeviceList{Format: 1, AccountID: d.Account, Server: d.Server, Version: lp.version, Prev: lp.prev, KeyVersion: keyVersion,
		AccountKey: Public(accountKey), Devices: devices, At: d.Now()}
	if lp.reset {
		l.Reset = 1
	}
	raw, err := l.Encode()
	if err != nil {
		return SignedList{}, err
	}
	return SignList(raw, accountKey, lp.previous), nil
}

// ErrLocked is returned by anything that needs the backup key on a device
// that does not hold it.
var ErrLocked = errors.New("ref: the device is locked")

// nextOwnList builds the next ordinary version of the device's own
// account's list with a changed set of devices.
func (d *Device) nextOwnList(change func(devices []ListDevice) []ListDevice) (SignedList, error) {
	if !d.Unlocked() {
		return SignedList{}, ErrLocked
	}
	cur := d.Pins[d.Account].NewestList()
	if cur == nil {
		return SignedList{}, errors.New("ref: the device has not pinned its own account's list")
	}
	if string(cur.List.AccountKey) != string(Public(d.AccountKey)) {
		return SignedList{}, errors.New("ref: the device's account key is not the one in the list; it must unlock again")
	}
	return d.signedList(listPlan{version: cur.List.Version + 1, prev: ListHash(cur.Signed.List),
		devices: change(append([]ListDevice{}, cur.List.Devices...))}, d.KeyVersion, d.AccountKey)
}

// ListWithSelf is the next version of the account's list with this device
// in it: what a device publishes when it has just been unlocked
// (section 7.2).
func (d *Device) ListWithSelf() (SignedList, error) {
	return d.nextOwnList(func(devices []ListDevice) []ListDevice {
		for _, dev := range devices {
			if dev.DeviceID == d.ID {
				return devices
			}
		}
		return append(devices, d.entry())
	})
}

// ListWithout is the next version of the account's list without some
// devices: this device signing out, or a retired one being cleared away
// (section 7.3).
func (d *Device) ListWithout(gone ...UUID) (SignedList, error) {
	return d.nextOwnList(func(devices []ListDevice) []ListDevice {
		var keep []ListDevice
		for _, dev := range devices {
			drop := false
			for _, g := range gone {
				drop = drop || dev.DeviceID == g
			}
			if !drop {
				keep = append(keep, dev)
			}
		}
		return keep
	})
}

// StoredBackup is what the server gives a complete session of an account to
// unlock with: the account record as filed, and one locked copy.
type StoredBackup struct {
	KeyVersion    uint32
	Revision      uint64
	AccountRecord []byte
}

// ErrUnlock is the refusal of a way of unlocking: the copy does not open,
// its signature fails, the account record is older than one the device has
// seen, or the account key is not the one in the account's device list.
var ErrUnlock = errors.New("ref: the backup key could not be unlocked")

func unlockErr(why string) error { return fmt.Errorf("%w: %s", ErrUnlock, why) }

// adoptBackupKey makes the checks every way of unlocking ends with
// (section 13.3, "Why the signature") and, if they pass, unlocks the
// device.
func (d *Device) adoptBackupKey(key []byte, sb StoredBackup, copyKind byte, copyID UUID, copyStored []byte) error {
	if d.Record != nil && (sb.KeyVersion < d.Record.KeyVersion || (sb.KeyVersion == d.Record.KeyVersion && sb.Revision < d.Record.Revision)) {
		return unlockErr("the server gave an account record older than one this device has seen")
	}
	rec, err := OpenAccountRecord(key, d.Account, sb.KeyVersion, sb.Revision, sb.AccountRecord)
	if err != nil {
		return unlockErr("the account record does not open with that key")
	}
	if copyStored != nil && !VerifyLockedCopy(rec.AccountKey, d.Account, sb.KeyVersion, copyKind, copyID, copyStored) {
		return unlockErr("the locked copy is not signed by the account key")
	}
	if pin := d.Pins[d.Account].NewestList(); pin != nil {
		if string(pin.List.AccountKey) != string(rec.AccountKey) || pin.List.KeyVersion != rec.KeyVersion {
			return unlockErr("the account key is not the one in the account's device list")
		}
	}
	d.BackupKey, d.KeyVersion, d.AccountKey, d.Record = key, rec.KeyVersion, SigningKey(rec.AccountKeyPrivate), rec
	return nil
}

// UnlockWithCopy opens a locked copy with an unlock key's private half and
// unlocks the device with what it holds.
func (d *Device) UnlockWithCopy(unlockPrivate []byte, kind byte, id UUID, copyStored []byte, sb StoredBackup) error {
	key, err := OpenLockedCopy(unlockPrivate, d.Account, sb.KeyVersion, kind, id, copyStored)
	if err != nil {
		return unlockErr("the locked copy does not open")
	}
	return d.adoptBackupKey(key, sb, kind, id, copyStored)
}

// UnlockWithPhrase unlocks with the twelve words.
func (d *Device) UnlockWithPhrase(typed string, copyStored []byte, sb StoredBackup) error {
	entropy, err := ReadPhrase(typed)
	if err != nil {
		return err
	}
	return d.UnlockWithCopy(UnlockKeyFromPhrase(entropy, d.Account), WayPhrase, UUID{}, copyStored, sb)
}

// UnlockWithPRF unlocks with a passkey's PRF output, as the page that asked
// for it does.
func (d *Device) UnlockWithPRF(prfOutput, credentialID []byte, passkey UUID, copyStored []byte, sb StoredBackup) error {
	return d.UnlockWithCopy(UnlockKeyFromPRF(prfOutput, d.Account, credentialID), WayPasskey, passkey, copyStored, sb)
}

// UnlockWithAccessKey unlocks with the account's access key, given as its
// 32 random bytes (ruling R85).
func (d *Device) UnlockWithAccessKey(random, copyStored []byte, sb StoredBackup) error {
	return d.UnlockWithCopy(UnlockKeyFromAccessKey(AccessKeyWrap(random)), WayAccessKey, UUID{}, copyStored, sb)
}

// UnlockWithDeviceCopy unlocks with a copy sealed to this device's own box
// key: the answer to its unlock request, or its copy after the backup key
// was replaced.
func (d *Device) UnlockWithDeviceCopy(copyStored []byte, sb StoredBackup) error {
	return d.UnlockWithCopy(d.Box, WayDevice, d.ID, copyStored, sb)
}

// UnlockWithHandover unlocks an app with the hand-over that came with its
// tokens after a passkey sign-in on the server's page.
func (d *Device) UnlockWithHandover(handover []byte, codeChallenge string, sb StoredBackup) error {
	key, err := OpenHandover(d.Box, d.Account, codeChallenge, handover)
	if err != nil {
		return unlockErr("the hand-over does not open")
	}
	return d.adoptBackupKey(key, sb, 0, UUID{}, nil)
}

// UnlockWithPairingHandover unlocks an app that was signed in by scanning a
// QR code, with the two-layer hand-over and the secret from the code.
func (d *Device) UnlockWithPairingHandover(handover, pairingSecret []byte, sb StoredBackup) error {
	key, err := OpenPairingHandover(d.Box, pairingSecret, d.Account, handover)
	if err != nil {
		return unlockErr("the hand-over does not open")
	}
	return d.adoptBackupKey(key, sb, 0, UUID{}, nil)
}

// Lock makes the device forget the backup key and account key, as when a
// way of unlocking was refused half way.
func (d *Device) Lock() {
	d.BackupKey, d.AccountKey, d.Record, d.KeyVersion = nil, nil, nil, 0
}

// HandoverFor is what an unlocked page makes for an app after signing the
// person in with a passkey (section 13.3).
func (d *Device) HandoverFor(appBoxKey []byte, codeChallenge string) ([]byte, error) {
	if !d.Unlocked() {
		return nil, ErrLocked
	}
	return SealHandover(appBoxKey, d.Account, codeChallenge, d.BackupKey, d.random("handover_one_time", 32))
}

// PairingHandoverFor is what an unlocked web chat makes when the person
// confirms a device that scanned its QR code (section 13.7).
func (d *Device) PairingHandoverFor(appBoxKey, pairingSecret []byte) ([]byte, error) {
	if !d.Unlocked() {
		return nil, ErrLocked
	}
	return SealPairingHandover(appBoxKey, pairingSecret, d.Account, d.BackupKey, d.random("pairing_one_time", 32), d.random("pairing_salt", SaltSize))
}

// NewUnlockRequest is what a new, locked device posts to ask an unlocked
// one for the backup key, and the code it shows.
func (d *Device) NewUnlockRequest() (request []byte, code string, err error) {
	u := UnlockRequest{Format: 1, AccountID: d.Account, DeviceID: d.ID, SignKey: d.SignPublic(), BoxKey: d.BoxPublic(),
		Platform: d.Platform, At: d.Now(), Proof: DeviceProof(d.Sign, d.Account, d.ID, d.BoxPublic())}
	if request, err = u.Encode(); err != nil {
		return nil, "", err
	}
	return request, ApprovalCode(request), nil
}

// ErrCode is returned when the code the person typed or scanned is not the
// code of the request that came through the server: the server may have
// swapped the new device's keys for its own.
var ErrCode = errors.New("ref: the code does not match the request")

// AnswerUnlockRequest is what an unlocked device does with an unlock
// request after its owner scanned or typed the new device's code: it
// proceeds only if the code it computes from the request is that code, and
// then makes a locked copy for the new device's box key.
func (d *Device) AnswerUnlockRequest(request []byte, code string) ([]byte, error) {
	if !d.Unlocked() {
		return nil, ErrLocked
	}
	u, err := CheckUnlockRequest(request)
	if err != nil {
		return nil, err
	}
	if u.AccountID != d.Account {
		return nil, errors.New("ref: the request is for another account")
	}
	if NormalizeApprovalCode(code) != ApprovalCode(request) {
		return nil, ErrCode
	}
	return MakeLockedCopy(u.BoxKey, d.Account, d.KeyVersion, WayDevice, u.DeviceID, d.BackupKey, d.AccountKey,
		d.random("locked_copy_one_time:device:"+u.DeviceID.String(), 32))
}

// DeviceCopyFor makes a locked copy of the backup key for another device's
// box key, as the server's unlock page does for an app after a passkey
// touch.
func (d *Device) DeviceCopyFor(device UUID, boxKey []byte) ([]byte, error) {
	if !d.Unlocked() {
		return nil, ErrLocked
	}
	return MakeLockedCopy(boxKey, d.Account, d.KeyVersion, WayDevice, device, d.BackupKey, d.AccountKey,
		d.random("locked_copy_one_time:device:"+device.String(), 32))
}

// RecordUpdate is a new revision of the account record with the locked
// copy that goes with it.
type RecordUpdate struct {
	Revision      uint64
	AccountRecord []byte
	Copy          Copy
	record        *AccountRecord
}

// ApprovePasskey is what an unlocked device does when its owner says yes
// to "Let the passkey unlock sealed chats?" (section 13.4): a locked copy
// for the passkey's unlock key, and the way added to the account record
// with the next revision.
func (d *Device) ApprovePasskey(passkey UUID, unlockPublic []byte) (*RecordUpdate, error) {
	return d.approveWay(WayPasskey, passkey, unlockPublic)
}

// ApproveAccessKey lets the account's access key unlock (ruling R85): the
// access-key way, replacing any earlier one, in the next revision of the
// account record, and its locked copy.
func (d *Device) ApproveAccessKey(unlockPublic []byte) (*RecordUpdate, error) {
	return d.approveWay(WayAccessKey, UUID{}, unlockPublic)
}

func (d *Device) approveWay(kind byte, id UUID, unlockPublic []byte) (*RecordUpdate, error) {
	if !d.Unlocked() {
		return nil, ErrLocked
	}
	rec := *d.Record
	rec.Ways = nil
	for _, w := range d.Record.Ways {
		if !(w.Kind == kind && w.ID == id) {
			rec.Ways = append(rec.Ways, w)
		}
	}
	rec.Ways = append(rec.Ways, Way{Kind: kind, ID: id, UnlockPublic: unlockPublic})
	sortWays(rec.Ways)
	rec.Revision++
	rec.At = d.Now()
	locked, err := LockAccountRecord(d.BackupKey, &rec, d.random("account_record_salt", SaltSize))
	if err != nil {
		return nil, err
	}
	cp, err := MakeLockedCopy(unlockPublic, d.Account, d.KeyVersion, kind, id, d.BackupKey, d.AccountKey,
		d.random("locked_copy_one_time:"+WayNames[kind]+":"+id.String(), 32))
	if err != nil {
		return nil, err
	}
	return &RecordUpdate{Revision: rec.Revision, AccountRecord: locked, Copy: Copy{Kind: kind, ID: id, Locked: cp}, record: &rec}, nil
}

// AdoptRecord switches the device to an account record revision the server
// has accepted.
func (d *Device) AdoptRecord(u *RecordUpdate) { d.Record = u.record }

// Replacement says what a key replacement takes away.
type Replacement struct {
	// Devices are devices being revoked: they are left out of the new
	// list and get no copy.
	Devices []UUID
	// Passkeys are passkeys whose way is removed.
	Passkeys []UUID
	// NewPhrase replaces the phrase with new words.
	NewPhrase bool
	// DropAccessKey removes the access-key way: the key was replaced or
	// removed.
	DropAccessKey bool
}

// ReplaceBackup replaces the backup key and the account key together
// (section 13.6). records are every backup record of the account,
// downloaded from the server; each is opened and locked again under the new
// key. The ways are taken from the old account record, never from the
// server's rows.
func (d *Device) ReplaceBackup(r Replacement, records []StoredRecord) (*Bundle, error) {
	if !d.Unlocked() {
		return nil, ErrLocked
	}
	cur := d.Pins[d.Account].NewestList()
	if cur == nil {
		return nil, errors.New("ref: the device has not pinned its own account's list")
	}
	backupKey := d.random("backup_key", 32)
	accountKey := SigningKey(d.random("account_key_seed", 32))
	rec := &AccountRecord{Format: 1, AccountID: d.Account, KeyVersion: d.KeyVersion + 1, Revision: d.Record.Revision + 1,
		AccountKey: Public(accountKey), AccountKeyPrivate: accountKey.Seed(), At: d.Now()}
	phrase := ""
	for _, w := range d.Record.Ways {
		drop := w.Kind == WayAccessKey && r.DropAccessKey
		for _, pk := range r.Passkeys {
			drop = drop || (w.Kind == WayPasskey && w.ID == pk)
		}
		if w.Kind == WayPhrase && r.NewPhrase {
			entropy := d.random("phrase_entropy", 16)
			w, phrase = phraseWay(entropy, d.Account), Phrase(entropy)
		}
		if !drop {
			rec.Ways = append(rec.Ways, w)
		}
	}
	sortWays(rec.Ways)
	var relocked []StoredRecord
	for _, old := range records {
		b, err := OpenBackupRecord(d.BackupKey, d.Account, old.ConversationID, old.KeyEpoch, old.KeyVersion, old.Locked)
		if err != nil {
			// A record that does not open under the current key was
			// left by an earlier key and is lost already.
			continue
		}
		locked, err := LockBackupRecord(backupKey, d.Account, rec.KeyVersion, b,
			d.random(fmt.Sprintf("backup_record_salt:%s:%d", old.ConversationID, old.KeyEpoch), SaltSize))
		if err != nil {
			return nil, err
		}
		relocked = append(relocked, StoredRecord{ConversationID: old.ConversationID, KeyEpoch: old.KeyEpoch, KeyVersion: rec.KeyVersion, Locked: locked})
	}
	var devices []ListDevice
	for _, dev := range cur.List.Devices {
		drop := false
		for _, g := range r.Devices {
			drop = drop || dev.DeviceID == g
		}
		if !drop {
			devices = append(devices, dev)
		}
	}
	b, err := d.bundle(backupKey, accountKey, rec, phrase, listPlan{version: cur.List.Version + 1, prev: ListHash(cur.Signed.List),
		devices: devices, previous: d.AccountKey}, relocked)
	if err != nil {
		return nil, err
	}
	b.Replaces = d.KeyVersion
	return b, nil
}

// ResetBackup is what a device does when no way of unlocking is left
// (section 13.8): a new backup key, account key and phrase with the next
// key version, and a device list marked `reset` holding only itself.
// Earlier sealed messages cannot be read again on any device of the
// account.
func (d *Device) ResetBackup() (*Bundle, error) {
	cur := d.Pins[d.Account].NewestList()
	if cur == nil {
		return nil, errors.New("ref: the device has not pinned its own account's list")
	}
	backupKey := d.random("backup_key", 32)
	accountKey := SigningKey(d.random("account_key_seed", 32))
	entropy := d.random("phrase_entropy", 16)
	rec := &AccountRecord{Format: 1, AccountID: d.Account, KeyVersion: cur.List.KeyVersion + 1, Revision: 1, AccountKey: Public(accountKey),
		AccountKeyPrivate: accountKey.Seed(), Ways: []Way{phraseWay(entropy, d.Account)}, At: d.Now()}
	b, err := d.bundle(backupKey, accountKey, rec, Phrase(entropy), listPlan{version: cur.List.Version + 1, prev: ListHash(cur.Signed.List),
		reset: true, devices: []ListDevice{d.entry()}}, nil)
	if err != nil {
		return nil, err
	}
	b.Replaces = cur.List.KeyVersion
	// Nothing the device held from before the reset is of use.
	for _, c := range d.Chats {
		for _, e := range c.Epochs {
			e.History, e.Secret = nil, nil
		}
	}
	return b, nil
}
