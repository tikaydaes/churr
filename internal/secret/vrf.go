package secret

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/tikaydaes/churr/internal/ecvrf"
	"github.com/tikaydaes/churr/internal/model"
)

// The VRF key (docs/DESIGN-FAIR.md): the secp256k1 key every chance
// outcome on this server is proved with. It is made once, at first start,
// and never replaced while the server runs. Its history lists every VRF key
// the server has had, each with the time it began, oldest first; the last
// is the key in vrf.key. Losing the key and starting a new one is an
// operator's act that appends to the history (docs/OPERATIONS.md section
// 4), so a proof made under an old key can still be checked against the
// time it was made.
//
//	vrf.key       the 32-byte secp256k1 secret scalar, big-endian, in (0, n)
//	vrf.history   the history as JSON: [{"key": hex, "began": time}, ...]
const (
	vrfFile        = "vrf.key"
	vrfHistoryFile = "vrf.history"
	vrfLen         = 32
	vrfPublicLen   = 33
)

// VRFKey is one entry of the VRF key history: a public key (33 bytes,
// compressed) and the moment it began to be used.
type VRFKey struct {
	Key   []byte
	Began model.Time
}

// vrfEntry is how an entry is written in vrf.history.
type vrfEntry struct {
	Key   string     `json:"key"`
	Began model.Time `json:"began"`
}

// openVRF reads the VRF key and its history in dir, making both on first
// use. A history that names a key whose secret is missing is refused: the
// server never makes a second key by itself.
func openVRF(dir string, now model.Time) ([]byte, []VRFKey, error) {
	histPath := filepath.Join(dir, vrfHistoryFile)
	hist, err := readVRFHistory(histPath)
	if err != nil {
		return nil, nil, err
	}
	keyPath := filepath.Join(dir, vrfFile)
	if hist != nil {
		if _, err := os.Stat(keyPath); errors.Is(err, fs.ErrNotExist) {
			return nil, nil, fmt.Errorf("secret: %s is missing, but %s says this server has a VRF key; restore %s from a backup. "+
				"Starting a new VRF key is an operator's decision, never made at start (docs/OPERATIONS.md section 4)",
				keyPath, histPath, vrfFile)
		}
	}
	sk, err := loadOrMake(dir, vrfFile, vrfLen, func(int) []byte { return newVRFKey() })
	if err != nil {
		return nil, nil, err
	}
	pk, err := ecvrf.DerivePublicKey(sk)
	if err != nil {
		return nil, nil, fmt.Errorf("secret: the key file %s is not a secp256k1 key; restore it from a backup", keyPath)
	}
	if hist == nil {
		// First start, or a first start interrupted between the two files:
		// the key begins now.
		hist = []VRFKey{{Key: pk, Began: now}}
		if err := writeVRFHistory(histPath, hist); err != nil {
			return nil, nil, err
		}
		// Two servers started on one folder at once: whichever wrote the
		// history first, both go on with what is on the disk.
		if hist, err = readVRFHistory(histPath); err != nil {
			return nil, nil, err
		}
	}
	if !bytes.Equal(hist[len(hist)-1].Key, pk) {
		return nil, nil, fmt.Errorf("secret: %s is not the key %s ends with; restore the two together from one backup", keyPath, histPath)
	}
	return sk, hist, nil
}

// newVRFKey draws a secret scalar in (0, n). Almost every 32 random bytes are
// one; the rest are drawn again.
func newVRFKey() []byte {
	for {
		sk := RandomBytes(vrfLen)
		if _, err := ecvrf.DerivePublicKey(sk); err == nil {
			return sk
		}
	}
}

// readVRFHistory reads vrf.history; nil, nil if there is none.
func readVRFHistory(path string) ([]VRFKey, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("secret: reading %s: %w", path, err)
	}
	var entries []vrfEntry
	if err := json.Unmarshal(raw, &entries); err != nil || len(entries) == 0 {
		return nil, fmt.Errorf("secret: %s is damaged; restore it from a backup", path)
	}
	hist := make([]VRFKey, len(entries))
	for i, e := range entries {
		key, err := hex.DecodeString(e.Key)
		if err != nil || len(key) != vrfPublicLen || e.Began.IsZero() || (i > 0 && e.Began <= hist[i-1].Began) {
			return nil, fmt.Errorf("secret: %s is damaged at entry %d; restore it from a backup", path, i+1)
		}
		hist[i] = VRFKey{Key: key, Began: e.Began}
	}
	return hist, nil
}

// writeVRFHistory writes a new vrf.history. It never overwrites one: an
// existing file is left as it is.
func writeVRFHistory(path string, hist []VRFKey) error {
	entries := make([]vrfEntry, len(hist))
	for i, k := range hist {
		entries[i] = vrfEntry{Key: hex.EncodeToString(k.Key), Began: k.Began}
	}
	b, _ := json.MarshalIndent(entries, "", "  ")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("secret: making %s: %w", path, err)
	}
	_, werr := f.Write(append(b, '\n'))
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(path)
		return fmt.Errorf("secret: writing %s: %w", path, werr)
	}
	return nil
}

// VRFPublic is the current VRF public key, 33 bytes compressed: the last
// entry of the history.
func (k *Keys) VRFPublic() []byte {
	return bytes.Clone(k.vrfHistory[len(k.vrfHistory)-1].Key)
}

// VRFHistory is every VRF key this server has had, oldest first, each with
// the time it began. The last is VRFPublic.
func (k *Keys) VRFHistory() []VRFKey {
	out := make([]VRFKey, len(k.vrfHistory))
	for i, e := range k.vrfHistory {
		out[i] = VRFKey{Key: bytes.Clone(e.Key), Began: e.Began}
	}
	return out
}

// VRFProve proves alpha under the current VRF key: the proof, 81 bytes, and
// its output beta, 32 (docs/FAIR.md section 6).
func (k *Keys) VRFProve(alpha []byte) (proof, beta []byte, err error) {
	proof, err = ecvrf.Prove(k.vrf, alpha)
	if err != nil {
		return nil, nil, err
	}
	beta, err = ecvrf.ProofToHash(proof)
	return proof, beta, err
}
