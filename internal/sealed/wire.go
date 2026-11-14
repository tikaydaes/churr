package sealed

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
)

// The server's reader for the parts of sealed-chat records it is allowed to
// see (docs/SEALED.md sections 4.5, 5.5 and 8.2). It reads the record
// encoding of section 2.3, verifies signatures, and computes hashes. It
// holds no key and cannot open anything: the code that handles secrets is
// the clients', and its Go reference in the ref package is linked only by
// tests.
//
// This reader is written apart from the reference on purpose, so each is a
// check on the other.

// The labels the server needs: the ones signatures and hashes are made
// under. Every label is ASCII and is followed by one zero byte.
const (
	labelDeviceProof  = "churr device proof v1"
	labelDeviceList   = "churr device list v1"
	labelListRollover = "churr device list rollover v1"
	labelListHash     = "churr device list hash v1"
	labelEpoch        = "churr epoch v1"
	labelEpochHash    = "churr epoch hash v1"
	labelDeviceAttach = "churr device attach v1"
)

// maxRecord is the longest record: 64 KB.
const maxRecord = 64 * 1024

// maxCiphertext is the most a sealed message may be: 128 KB.
const maxCiphertext = 128 * 1024

// maxListDevices is the most devices a list may hold.
const maxListDevices = 50

// errRecord is any failure to read a record: too short, bytes left over, a
// choice out of range, a list out of order. The handlers turn it into
// invalid_value with the field that carried the record.
var errRecord = errors.New("sealed: the record is not in its one valid form")

func labelled(label string, record []byte) []byte {
	out := make([]byte, 0, len(label)+1+len(record))
	out = append(out, label...)
	out = append(out, 0)
	return append(out, record...)
}

// verify checks an Ed25519 signature over label || 0x00 || record.
func verify(public []byte, label string, record, sig []byte) bool {
	return len(public) == ed25519.PublicKeySize && len(sig) == ed25519.SignatureSize &&
		ed25519.Verify(ed25519.PublicKey(public), labelled(label, record), sig)
}

// VerifyAttach checks the signature a device makes over the server's
// challenge when the same app signs in again (section 4.1).
func VerifyAttach(signKey, challenge, sig []byte) bool {
	return verify(signKey, labelDeviceAttach, challenge, sig)
}

func hash(label string, record []byte) [32]byte { return sha256.Sum256(labelled(label, record)) }

// reader takes a record apart. The first failure sticks.
type reader struct {
	b   []byte
	bad bool
}

func (r *reader) take(n int) []byte {
	if r.bad || n < 0 || len(r.b) < n {
		r.bad = true
		return make([]byte, max(n, 0))
	}
	out := r.b[:n]
	r.b = r.b[n:]
	return out
}

func (r *reader) u8() byte    { return r.take(1)[0] }
func (r *reader) u32() uint32 { return binary.BigEndian.Uint32(r.take(4)) }
func (r *reader) u64() uint64 { return binary.BigEndian.Uint64(r.take(8)) }

func (r *reader) uuid() string {
	h := hex.EncodeToString(r.take(16))
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func (r *reader) blob() []byte {
	n := r.u32()
	if uint64(n) > uint64(len(r.b)) {
		r.bad = true
		return nil
	}
	return r.take(int(n))
}

func (r *reader) choice(allowed ...byte) byte {
	v := r.u8()
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	r.bad = true
	return 0
}

// count reads a list's count, refusing one that could not fit in what is
// left given the size of one item.
func (r *reader) count(itemSize int) int {
	n := r.u32()
	if uint64(n)*uint64(itemSize) > uint64(len(r.b)) {
		r.bad = true
		return 0
	}
	return int(n)
}

func (r *reader) done() error {
	if r.bad || len(r.b) != 0 {
		return errRecord
	}
	return nil
}

// uuidBytes turns an identifier back into its 16 bytes.
func uuidBytes(id string) []byte {
	if !model.ValidID(id) {
		return make([]byte, 16)
	}
	b, _ := hex.DecodeString(id[0:8] + id[9:13] + id[14:18] + id[19:23] + id[24:36])
	return b
}

// listDevice is one device in a device list.
type listDevice struct {
	ID       model.DeviceID
	SignKey  []byte
	BoxKey   []byte
	Platform byte
	Proof    []byte
}

// deviceList is the readable content of a device list (section 4.3).
type deviceList struct {
	AccountID  model.AccountID
	Server     string
	Version    uint64
	Prev       [32]byte
	Reset      bool
	KeyVersion uint32
	AccountKey []byte
	Devices    []listDevice
}

// platformNames maps the platform numbers of a device list to the API's
// words.
var platformNames = map[byte]string{1: "android", 2: "ios", 3: "macos", 4: "windows", 5: "linux", 6: "web"}

func parseDeviceList(b []byte) (deviceList, error) {
	var l deviceList
	if len(b) > maxRecord {
		return l, errRecord
	}
	r := &reader{b: b}
	r.choice(1)
	l.AccountID = model.AccountID(r.uuid())
	server := r.blob()
	if !utf8.Valid(server) {
		r.bad = true
	}
	l.Server = string(server)
	l.Version = r.u64()
	copy(l.Prev[:], r.take(32))
	l.Reset = r.choice(0, 1) == 1
	l.KeyVersion = r.u32()
	l.AccountKey = r.take(32)
	n := r.count(16 + 32 + 32 + 1 + 8 + 64)
	if n > maxListDevices {
		r.bad = true
	}
	var last string
	for i := 0; i < n && !r.bad; i++ {
		var d listDevice
		id := r.uuid()
		// A list that stands for a set is sorted by its key with no key
		// twice; the hyphenated form sorts as the bytes do.
		if i > 0 && id <= last {
			r.bad = true
		}
		last = id
		d.ID = model.DeviceID(id)
		d.SignKey = r.take(32)
		d.BoxKey = r.take(32)
		d.Platform = r.choice(1, 2, 3, 4, 5, 6)
		r.u64() // added_at
		d.Proof = r.take(64)
		l.Devices = append(l.Devices, d)
	}
	r.u64() // at
	return l, r.done()
}

// signedList is the stored form of one list version: the list record, the
// signature by its own account key and, for a key change, the signature by
// the previous one.
type signedList struct {
	List    []byte
	Sig     []byte
	PrevSig []byte
}

func parseSignedList(b []byte) (signedList, error) {
	r := &reader{b: b}
	var s signedList
	s.List = r.blob()
	s.Sig = r.take(64)
	if r.choice(0, 1) == 1 {
		s.PrevSig = r.take(64)
	}
	return s, r.done()
}

func (s signedList) encode() []byte {
	out := binary.BigEndian.AppendUint32(nil, uint32(len(s.List)))
	out = append(out, s.List...)
	out = append(out, s.Sig...)
	if s.PrevSig == nil {
		return append(out, 0)
	}
	return append(append(out, 1), s.PrevSig...)
}

// errList is a device list that fails the structural checks of section
// 4.4. Its text is fit to show.
type errList string

func (e errList) Error() string { return string(e) }

// checkList makes the checks of section 4.4 that need no pin: the list's
// own account key signed it, every proof verifies, and no key repeats.
func checkList(s signedList) (deviceList, error) {
	l, err := parseDeviceList(s.List)
	if err != nil {
		return l, errList("The device list is not in its one valid form.")
	}
	if l.Version == 0 {
		return l, errList("A device list's version starts at 1.")
	}
	seen := map[string]bool{}
	for _, d := range l.Devices {
		proof := append(append(append(uuidBytes(string(l.AccountID)), uuidBytes(string(d.ID))...), d.SignKey...), d.BoxKey...)
		if !verify(d.SignKey, labelDeviceProof, proof, d.Proof) {
			return l, errList("A device's proof does not verify.")
		}
		for _, k := range []string{"s" + string(d.SignKey), "b" + string(d.BoxKey)} {
			if seen[k] {
				return l, errList("Two devices in the list have the same key.")
			}
			seen[k] = true
		}
	}
	if !verify(l.AccountKey, labelDeviceList, s.List, s.Sig) {
		return l, errList("The device list is not signed by its account key.")
	}
	return l, nil
}

// checkListStep makes the checks of section 4.4 from one version to the
// next. prev is nil for version 1.
func checkListStep(prev *signedList, next signedList) (deviceList, error) {
	l, err := checkList(next)
	if err != nil {
		return l, err
	}
	if prev == nil {
		if l.Version != 1 || l.Prev != [32]byte{} || next.PrevSig != nil || l.Reset {
			return l, errList("Version 1 of a device list has no previous version, no rollover signature and is not a reset.")
		}
		return l, nil
	}
	p, err := parseDeviceList(prev.List)
	if err != nil {
		return l, err
	}
	if l.AccountID != p.AccountID || l.Server != p.Server || l.Version != p.Version+1 || l.Prev != hash(labelListHash, prev.List) {
		return l, errList("The device list does not follow the version before it.")
	}
	sameKey := string(l.AccountKey) == string(p.AccountKey)
	switch {
	case l.Reset:
		if l.KeyVersion != p.KeyVersion+1 || next.PrevSig != nil {
			return l, errList("A reset raises the key version by one and carries no rollover signature.")
		}
	case sameKey && l.KeyVersion == p.KeyVersion:
		if next.PrevSig != nil {
			return l, errList("An ordinary change carries no rollover signature.")
		}
	case !sameKey && l.KeyVersion == p.KeyVersion+1:
		if !verify(p.AccountKey, labelListRollover, next.List, next.PrevSig) {
			return l, errList("The previous account key did not sign the key change.")
		}
	default:
		return l, errList("The account key and key version do not fit any kind of change.")
	}
	return l, nil
}

// epochRef is which device list of an account an epoch record names.
type epochRef struct {
	AccountID   model.AccountID
	ListVersion uint64
	ListHash    [32]byte
}

// epochRecipient is a device an epoch secret is sealed to. The sealed
// bytes themselves are not kept: the server has no use for them.
type epochRecipient struct {
	AccountID model.AccountID
	DeviceID  model.DeviceID
}

// epochRecord is the readable content of an epoch record (section 5.1).
type epochRecord struct {
	ConversationID model.ConversationID
	KeyEpoch       uint64
	Prev           [32]byte
	MakerAccountID model.AccountID
	MakerDeviceID  model.DeviceID
	Accounts       []epochRef
	Recipients     []epochRecipient
	// Record and Sig are the two parts of the stored form.
	Record []byte
	Sig    []byte
}

func parseEpoch(stored []byte) (epochRecord, error) {
	var e epochRecord
	if len(stored) < 64 || len(stored) > maxRecord+64 {
		return e, errRecord
	}
	e.Record, e.Sig = stored[:len(stored)-64], stored[len(stored)-64:]
	r := &reader{b: e.Record}
	r.choice(1)
	e.ConversationID = model.ConversationID(r.uuid())
	e.KeyEpoch = r.u64()
	copy(e.Prev[:], r.take(32))
	e.MakerAccountID = model.AccountID(r.uuid())
	e.MakerDeviceID = model.DeviceID(r.uuid())
	if n := r.count(16 + 8 + 32); n != 2 {
		r.bad = true
	}
	for i := 0; i < 2 && !r.bad; i++ {
		var a epochRef
		a.AccountID = model.AccountID(r.uuid())
		a.ListVersion = r.u64()
		copy(a.ListHash[:], r.take(32))
		if i > 0 && a.AccountID <= e.Accounts[0].AccountID {
			r.bad = true
		}
		e.Accounts = append(e.Accounts, a)
	}
	r.take(32) // check
	n := r.count(16 + 16 + 80)
	var last model.DeviceID
	for i := 0; i < n && !r.bad; i++ {
		rc := epochRecipient{AccountID: model.AccountID(r.uuid()), DeviceID: model.DeviceID(r.uuid())}
		r.take(80)
		if i > 0 && rc.DeviceID <= last {
			r.bad = true
		}
		last = rc.DeviceID
		e.Recipients = append(e.Recipients, rc)
	}
	r.u64() // at
	return e, r.done()
}

// epochHash is the hash of an epoch's stored bytes, which the next epoch
// names as prev.
func epochHash(stored []byte) [32]byte { return hash(labelEpochHash, stored) }

// messageHeader is the readable front of a sealed message (section 8.1).
type messageHeader struct {
	Quiet          bool
	ConversationID model.ConversationID
	KeyEpoch       uint64
	AccountID      model.AccountID
	DeviceID       model.DeviceID
	Reference      string
}

// The fixed sizes of a sealed message: an 82-byte header, a 16-byte salt,
// at least a 16-byte tag, and a 64-byte signature.
const (
	headerSize     = 82
	minMessageSize = headerSize + 16 + 16 + 64
)

func parseHeader(sealed []byte) (messageHeader, error) {
	var h messageHeader
	if len(sealed) < minMessageSize {
		return h, errRecord
	}
	r := &reader{b: sealed[:headerSize]}
	r.choice(1)
	flags := r.u8()
	if flags&^1 != 0 {
		return h, errRecord
	}
	h.Quiet = flags&1 == 1
	h.ConversationID = model.ConversationID(r.uuid())
	h.KeyEpoch = r.u64()
	h.AccountID = model.AccountID(r.uuid())
	h.DeviceID = model.DeviceID(r.uuid())
	r.u64() // counter
	h.Reference = r.uuid()
	return h, r.done()
}

// unlockRequest is the readable content of an unlock request (section
// 13.7).
type unlockRequest struct {
	AccountID model.AccountID
	DeviceID  model.DeviceID
	SignKey   []byte
	BoxKey    []byte
	Platform  byte
}

func parseUnlockRequest(b []byte) (unlockRequest, error) {
	var u unlockRequest
	r := &reader{b: b}
	r.choice(1)
	account, device := r.uuid(), r.uuid()
	u.AccountID, u.DeviceID = model.AccountID(account), model.DeviceID(device)
	u.SignKey = r.take(32)
	u.BoxKey = r.take(32)
	u.Platform = r.choice(1, 2, 3, 4, 5, 6)
	r.u64() // at
	proof := r.take(64)
	if err := r.done(); err != nil {
		return u, err
	}
	signed := append(append(append(uuidBytes(account), uuidBytes(device)...), u.SignKey...), u.BoxKey...)
	if !verify(u.SignKey, labelDeviceProof, signed, proof) {
		return u, errRecord
	}
	return u, nil
}
