package ref

import (
	"crypto/ed25519"
	"crypto/sha256"
)

// Every label of docs/SEALED.md. Each is ASCII, begins "churr " and holds
// no zero byte, and no value made under one is accepted under another.
const (
	// Signature labels (section 2.4).
	LabelDeviceProof        = "churr device proof v1"
	LabelDeviceList         = "churr device list v1"
	LabelDeviceListRollover = "churr device list rollover v1"
	LabelEpoch              = "churr epoch v1"
	LabelMessage            = "churr sealed message v1"
	LabelLockedCopy         = "churr locked copy v1"
	LabelDeviceAttach       = "churr device attach v1"

	// Hash labels.
	LabelDeviceListHash = "churr device list hash v1"
	LabelEpochHash      = "churr epoch hash v1"
	LabelDeviceApproval = "churr device approval v1"
	LabelCompare        = "churr compare v1"
	LabelSealedUnlock   = "churr sealed unlock v1"

	// Seal labels (section 2.5).
	LabelSealEpoch     = "churr seal epoch v1"
	LabelSealBackupKey = "churr seal backup key v1"
	LabelSealHandover  = "churr seal handover v1"
	LabelSealPairing   = "churr seal pairing v1"

	// Lock labels (section 2.6).
	LabelLockMessage       = "churr lock message v1"
	LabelLockAccountRecord = "churr lock account record v1"
	LabelLockBackupRecord  = "churr lock backup record v1"
	LabelLockPairing       = "churr lock pairing v1"

	// Derivation labels (sections 5.2 and 13.3).
	LabelHistory       = "churr history v1"
	LabelContent       = "churr content v1"
	LabelEpochCheck    = "churr epoch check v1"
	LabelSender        = "churr sender v1"
	LabelCall          = "churr call v1"
	LabelCallSender    = "churr call sender v1"
	LabelUnlockPasskey = "churr unlock passkey v1"
	LabelUnlockPhrase  = "churr unlock phrase v1"

	// The associated data of every file chunk (section 9).
	LabelFile = "churr file v1"
)

// SignatureLabels is every label a signature is made under, for the vectors
// that prove a signature under one is refused under each of the others.
var SignatureLabels = []string{
	LabelDeviceProof, LabelDeviceList, LabelDeviceListRollover, LabelEpoch, LabelMessage, LabelLockedCopy, LabelDeviceAttach,
}

// labelled is label || 0x00 || the rest, joined: the bytes that are signed,
// hashed or used as HKDF info and associated data.
func labelled(label string, rest ...[]byte) []byte {
	n := len(label) + 1
	for _, r := range rest {
		n += len(r)
	}
	out := make([]byte, 0, n)
	out = append(out, label...)
	out = append(out, 0)
	for _, r := range rest {
		out = append(out, r...)
	}
	return out
}

// Sign makes an Ed25519 signature over label || 0x00 || record.
func Sign(key ed25519.PrivateKey, label string, record []byte) []byte {
	return ed25519.Sign(key, labelled(label, record))
}

// Verify checks a signature made by Sign under the same label. A signature
// is only ever verified, never compared with another: not every platform
// signs deterministically.
func Verify(public []byte, label string, record, sig []byte) bool {
	if len(public) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(public), labelled(label, record), sig)
}

// Hash is SHA-256(label || 0x00 || record).
func Hash(label string, record []byte) [32]byte {
	return sha256.Sum256(labelled(label, record))
}
