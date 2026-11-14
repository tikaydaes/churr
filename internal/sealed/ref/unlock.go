package ref

import (
	"crypto/sha256"
	"strings"
)

// Approval from another device (docs/SEALED.md section 13.7): the unlock
// request a new, locked device posts, and the code that stops the server
// from swapping the new device's keys for its own.

// UnlockRequest is the record a new device posts to ask an unlocked device
// of the same account for the backup key.
type UnlockRequest struct {
	Format    byte
	AccountID UUID
	DeviceID  UUID
	SignKey   []byte // Ed25519 public key
	BoxKey    []byte // X25519 public key
	Platform  byte
	At        uint64
	Proof     []byte // the device proof of section 4.3
}

// Encode writes the record: 170 bytes.
func (u *UnlockRequest) Encode() ([]byte, error) {
	if len(u.SignKey) != 32 || len(u.BoxKey) != 32 || len(u.Proof) != 64 {
		return nil, ErrShort
	}
	var w Writer
	w.U8(u.Format)
	w.UUID(u.AccountID)
	w.UUID(u.DeviceID)
	w.Raw(u.SignKey)
	w.Raw(u.BoxKey)
	w.U8(u.Platform)
	w.Time(u.At)
	w.Raw(u.Proof)
	return w.Bytes()
}

// DecodeUnlockRequest reads the record. It does not check the proof;
// CheckUnlockRequest does.
func DecodeUnlockRequest(b []byte) (*UnlockRequest, error) {
	r := NewReader(b)
	u := &UnlockRequest{}
	u.Format = r.Choice(1)
	u.AccountID = r.UUID()
	u.DeviceID = r.UUID()
	u.SignKey = r.Raw(32)
	u.BoxKey = r.Raw(32)
	u.Platform = r.Choice(PlatformAndroid, PlatformIOS, PlatformMacOS, PlatformWindows, PlatformLinux, PlatformWeb)
	u.At = r.U64()
	u.Proof = r.Raw(64)
	if err := r.Done(); err != nil {
		return nil, err
	}
	return u, nil
}

// CheckUnlockRequest reads an unlock request and checks its device proof.
func CheckUnlockRequest(b []byte) (*UnlockRequest, error) {
	u, err := DecodeUnlockRequest(b)
	if err != nil {
		return nil, err
	}
	if !Verify(u.SignKey, LabelDeviceProof, ProofBytes(u.AccountID, u.DeviceID, u.SignKey, u.BoxKey), u.Proof) {
		return nil, listErr(2, "the device's proof does not verify")
	}
	return u, nil
}

// crockford is Crockford's base32 alphabet: no I, L, O or U.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ApprovalCode is the code the new device shows and the unlocked device
// computes from the request it received through the server: the first 100
// bits of SHA-256("churr device approval v1" || 0x00 || request record), as
// 20 characters of Crockford base32 in four groups of five, joined by
// hyphens.
func ApprovalCode(request []byte) string {
	sum := sha256.Sum256(labelled(LabelDeviceApproval, request))
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		v := 0
		for bit := i * 5; bit < (i+1)*5; bit++ {
			v = v<<1 | int(sum[bit/8]>>(7-bit%8)&1)
		}
		if i > 0 && i%5 == 0 {
			sb.WriteByte('-')
		}
		sb.WriteByte(crockford[v])
	}
	return sb.String()
}

// NormalizeApprovalCode puts a typed code in the form ApprovalCode gives:
// upper case, hyphens and spaces dropped and put back, and the letters
// Crockford's alphabet leaves out read as the digits they look like (O as
// 0; I and L as 1).
func NormalizeApprovalCode(typed string) string {
	var chars []byte
	for _, c := range []byte(strings.ToUpper(typed)) {
		switch c {
		case '-', ' ':
			continue
		case 'O':
			c = '0'
		case 'I', 'L':
			c = '1'
		}
		chars = append(chars, c)
	}
	var sb strings.Builder
	for i, c := range chars {
		if i > 0 && i%5 == 0 {
			sb.WriteByte('-')
		}
		sb.WriteByte(c)
	}
	return sb.String()
}
