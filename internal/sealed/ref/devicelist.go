package ref

import (
	"crypto/ed25519"
	"errors"
	"fmt"
)

// Device lists (docs/SEALED.md section 4): the signed list of an account's
// devices, its chain from version to version, and the checks a client makes
// before it trusts one.

// The platforms a device list names.
const (
	PlatformAndroid byte = 1
	PlatformIOS     byte = 2
	PlatformMacOS   byte = 3
	PlatformWindows byte = 4
	PlatformLinux   byte = 5
	PlatformWeb     byte = 6
)

// PlatformNames gives the API's word for each platform number.
var PlatformNames = map[byte]string{
	PlatformAndroid: "android", PlatformIOS: "ios", PlatformMacOS: "macos",
	PlatformWindows: "windows", PlatformLinux: "linux", PlatformWeb: "web",
}

// PlatformNumber is the inverse of PlatformNames; 0 for an unknown word.
func PlatformNumber(name string) byte {
	for n, s := range PlatformNames {
		if s == name {
			return n
		}
	}
	return 0
}

// MaxDevices is the most devices a list may hold.
const MaxDevices = 50

// ListDevice is one device in a list.
type ListDevice struct {
	DeviceID UUID
	SignKey  []byte // Ed25519 public key, 32 bytes
	BoxKey   []byte // X25519 public key, 32 bytes
	Platform byte
	AddedAt  uint64
	// Proof is the device's own signature over account_id || device_id
	// || sign_key || box_key, label "churr device proof v1".
	Proof []byte
}

// DeviceList is the list record of section 4.3.
type DeviceList struct {
	Format     byte
	AccountID  UUID
	Server     string
	Version    uint64
	Prev       [32]byte
	Reset      byte
	KeyVersion uint32
	AccountKey []byte // Ed25519 public key, 32 bytes
	Devices    []ListDevice
	At         uint64
}

// Encode writes the record. The devices must already be sorted by
// device_id.
func (l *DeviceList) Encode() ([]byte, error) {
	if len(l.AccountKey) != 32 {
		return nil, fmt.Errorf("ref: an account key is 32 bytes")
	}
	if len(l.Devices) > MaxDevices {
		return nil, ErrCount
	}
	var w Writer
	w.U8(l.Format)
	w.UUID(l.AccountID)
	w.Text(l.Server)
	w.U64(l.Version)
	w.Raw(l.Prev[:])
	w.U8(l.Reset)
	w.U32(l.KeyVersion)
	w.Raw(l.AccountKey)
	w.U32(uint32(len(l.Devices)))
	for i, d := range l.Devices {
		if len(d.SignKey) != 32 || len(d.BoxKey) != 32 || len(d.Proof) != 64 {
			return nil, fmt.Errorf("ref: a device needs a 32-byte sign key, a 32-byte box key and a 64-byte proof")
		}
		if i > 0 && compare(l.Devices[i-1].DeviceID[:], d.DeviceID[:]) >= 0 {
			return nil, ErrOrder
		}
		w.UUID(d.DeviceID)
		w.Raw(d.SignKey)
		w.Raw(d.BoxKey)
		w.U8(d.Platform)
		w.Time(d.AddedAt)
		w.Raw(d.Proof)
	}
	w.Time(l.At)
	return w.Bytes()
}

// listDeviceSize is the encoded size of one device.
const listDeviceSize = 16 + 32 + 32 + 1 + 8 + 64

// DecodeDeviceList reads a list record, refusing anything that is not its
// one valid form.
func DecodeDeviceList(b []byte) (*DeviceList, error) {
	r := NewReader(b)
	l := &DeviceList{}
	l.Format = r.Choice(1)
	l.AccountID = r.UUID()
	l.Server = r.Text()
	l.Version = r.U64()
	copy(l.Prev[:], r.Raw(32))
	l.Reset = r.Choice(0, 1)
	l.KeyVersion = r.U32()
	l.AccountKey = r.Raw(32)
	n := r.Count(listDeviceSize)
	if n > MaxDevices {
		r.Fail(ErrCount)
	}
	for i := 0; i < n && r.Err() == nil; i++ {
		var d ListDevice
		d.DeviceID = r.UUID()
		d.SignKey = r.Raw(32)
		d.BoxKey = r.Raw(32)
		d.Platform = r.Choice(PlatformAndroid, PlatformIOS, PlatformMacOS, PlatformWindows, PlatformLinux, PlatformWeb)
		d.AddedAt = r.U64()
		d.Proof = r.Raw(64)
		if i > 0 && compare(l.Devices[i-1].DeviceID[:], d.DeviceID[:]) >= 0 {
			r.Fail(ErrOrder)
		}
		l.Devices = append(l.Devices, d)
	}
	l.At = r.U64()
	if err := r.Done(); err != nil {
		return nil, err
	}
	return l, nil
}

// Device finds a device in the list.
func (l *DeviceList) Device(id UUID) (ListDevice, bool) {
	for _, d := range l.Devices {
		if d.DeviceID == id {
			return d, true
		}
	}
	return ListDevice{}, false
}

// ProofBytes is what a device proof signs: account_id || device_id ||
// sign_key || box_key.
func ProofBytes(account, device UUID, signKey, boxKey []byte) []byte {
	out := make([]byte, 0, 96)
	out = append(out, account[:]...)
	out = append(out, device[:]...)
	out = append(out, signKey...)
	return append(out, boxKey...)
}

// DeviceProof is a device's own signature that it holds its signing key
// and belongs to this account. It stays valid when the account key changes.
func DeviceProof(sign ed25519.PrivateKey, account, device UUID, boxKey []byte) []byte {
	return Sign(sign, LabelDeviceProof, ProofBytes(account, device, Public(sign), boxKey))
}

// ListHash is the hash of a list record, which the next version names as
// `prev` and an epoch record names as `list_hash`.
func ListHash(list []byte) [32]byte { return Hash(LabelDeviceListHash, list) }

// SignedList is what is stored and sent for one version: the list record,
// the signature by its own account key and, when the account key changed
// without a reset, the signature by the previous one.
type SignedList struct {
	List    []byte
	Sig     []byte
	PrevSig []byte // nil unless has_prev_sig is 1
}

// Encode writes the stored form: blob list, sig, has_prev_sig, prev_sig.
func (s SignedList) Encode() ([]byte, error) {
	if len(s.Sig) != 64 || (s.PrevSig != nil && len(s.PrevSig) != 64) {
		return nil, fmt.Errorf("ref: a signature is 64 bytes")
	}
	var w Writer
	w.Blob(s.List)
	w.Raw(s.Sig)
	if s.PrevSig != nil {
		w.U8(1)
		w.Raw(s.PrevSig)
	} else {
		w.U8(0)
	}
	return w.Bytes()
}

// DecodeSignedList reads the stored form.
func DecodeSignedList(b []byte) (SignedList, error) {
	r := NewReader(b)
	var s SignedList
	s.List = r.Blob()
	s.Sig = r.Raw(64)
	if r.Choice(0, 1) == 1 {
		s.PrevSig = r.Raw(64)
	}
	if err := r.Done(); err != nil {
		return SignedList{}, err
	}
	return s, nil
}

// SignList signs a list record with its account key and, for a key change,
// with the previous version's account key too. previous is nil otherwise.
func SignList(list []byte, accountKey, previous ed25519.PrivateKey) SignedList {
	s := SignedList{List: list, Sig: Sign(accountKey, LabelDeviceList, list)}
	if previous != nil {
		s.PrevSig = Sign(previous, LabelDeviceListRollover, list)
	}
	return s
}

// ErrList is the refusal of a device list that does not check out. The
// client keeps its pin, shows "The server sent a device list that does not
// check out", and makes no epoch because of it.
type ErrList struct {
	// Rule is the number of the rule of section 4.4 that failed: 1 the
	// chain, 2 the proofs and repeats, 3 the signatures and the kind of
	// step. 0 is a list that does not even parse.
	Rule int
	Why  string
}

func (e *ErrList) Error() string { return "ref: device list refused: " + e.Why }

func listErr(rule int, why string) error { return &ErrList{Rule: rule, Why: why} }

// checkListAlone makes the checks one version allows by itself: it parses,
// its own account key signed it, every proof verifies and no device
// identifier or key repeats. This is all that can be checked at first
// contact.
func checkListAlone(s SignedList) (*DeviceList, error) {
	l, err := DecodeDeviceList(s.List)
	if err != nil {
		return nil, &ErrList{Rule: 0, Why: err.Error()}
	}
	if l.Version == 0 {
		return nil, listErr(1, "a list's version starts at 1")
	}
	seen := map[string]bool{}
	for _, d := range l.Devices {
		if !Verify(d.SignKey, LabelDeviceProof, ProofBytes(l.AccountID, d.DeviceID, d.SignKey, d.BoxKey), d.Proof) {
			return nil, listErr(2, "a device's proof does not verify")
		}
		for _, k := range []string{"s" + string(d.SignKey), "b" + string(d.BoxKey)} {
			if seen[k] {
				return nil, listErr(2, "two devices have the same key")
			}
			seen[k] = true
		}
	}
	if !Verify(l.AccountKey, LabelDeviceList, s.List, s.Sig) {
		return nil, listErr(3, "the list is not signed by its account key")
	}
	return l, nil
}

// CheckFirstList checks a list a client has no pin for: trust on first use.
// Nothing proves the list is really the other person's; comparing codes is
// what closes that.
func CheckFirstList(s SignedList) (*DeviceList, error) {
	l, err := checkListAlone(s)
	if err != nil {
		return nil, err
	}
	if l.Version == 1 {
		if l.Prev != [32]byte{} {
			return nil, listErr(1, "version 1 has a previous hash")
		}
		if s.PrevSig != nil {
			return nil, listErr(3, "version 1 has a rollover signature")
		}
	}
	return l, nil
}

// The three kinds of step from one version to the next.
const (
	StepOrdinary  = "ordinary"
	StepKeyChange = "key_change"
	StepReset     = "reset"
)

// CheckListStep checks the step from one accepted version to the next
// (section 4.4, "Later") and says which of the three kinds it is. prev is
// the previous version's list record, exactly as it was signed.
func CheckListStep(prev []byte, next SignedList) (*DeviceList, string, error) {
	p, err := DecodeDeviceList(prev)
	if err != nil {
		return nil, "", errors.New("ref: the previous list does not parse")
	}
	l, err := checkListAlone(next)
	if err != nil {
		return nil, "", err
	}
	// 1. The chain.
	if l.AccountID != p.AccountID || l.Server != p.Server {
		return nil, "", listErr(1, "the account or server changed")
	}
	if l.Version != p.Version+1 {
		return nil, "", listErr(1, "the version is not one higher")
	}
	if l.Prev != ListHash(prev) {
		return nil, "", listErr(1, "prev is not the hash of the previous version")
	}
	// 3. One of the three kinds of step. checkListAlone has verified sig
	// under the list's own key.
	sameKey := string(l.AccountKey) == string(p.AccountKey)
	switch {
	case l.Reset == 1:
		if l.KeyVersion != p.KeyVersion+1 {
			return nil, "", listErr(3, "a reset must raise the key version by one")
		}
		if next.PrevSig != nil {
			return nil, "", listErr(3, "a reset carries no rollover signature")
		}
		return l, StepReset, nil
	case sameKey && l.KeyVersion == p.KeyVersion:
		if next.PrevSig != nil {
			return nil, "", listErr(3, "an ordinary change carries no rollover signature")
		}
		return l, StepOrdinary, nil
	case !sameKey && l.KeyVersion == p.KeyVersion+1:
		if next.PrevSig == nil || !Verify(p.AccountKey, LabelDeviceListRollover, next.List, next.PrevSig) {
			return nil, "", listErr(3, "the previous account key did not sign the key change")
		}
		return l, StepKeyChange, nil
	}
	return nil, "", listErr(3, "the account key and key version do not fit any kind of step")
}
