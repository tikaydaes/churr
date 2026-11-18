package fair

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/tikaydaes/churr/internal/ecvrf"
	"github.com/tikaydaes/churr/internal/model"
)

// The chain (docs/FAIR.md section 6): each conversation's draws, numbered from
// 1, each proved over an alpha that nobody chooses: the owner's name, the
// chain's name, the draw's number and the hash of the draw before's proof.

// BlockVersion is the version of the fair block's layout.
const BlockVersion = 1

// alphaLabel opens every alpha of chains under this layout.
const alphaLabel = "churr-fair-1"

// NoPrev is prev for draw 1: 32 zero bytes.
var NoPrev = make([]byte, 32)

// Alpha is what draw n of a chain is proved over:
//
//	"churr-fair-1" ‖ u16be(len owner) ‖ owner ‖ u16be(len chain) ‖ chain ‖ u64be(n) ‖ prev
//
// owner is the drawing server's name and chain the chain's name (the
// conversation id, or the stream id of a conversation shared over the
// link), both as UTF-8; prev is SHA-256 of draw n-1's proof, or NoPrev for
// draw 1.
func Alpha(owner, chain string, n int64, prev []byte) []byte {
	b := make([]byte, 0, len(alphaLabel)+4+len(owner)+len(chain)+8+32)
	b = append(b, alphaLabel...)
	b = binary.BigEndian.AppendUint16(b, uint16(len(owner)))
	b = append(b, owner...)
	b = binary.BigEndian.AppendUint16(b, uint16(len(chain)))
	b = append(b, chain...)
	b = binary.BigEndian.AppendUint64(b, uint64(n))
	return append(b, prev...)
}

// PrevOf is the prev of the draw after one with this proof.
func PrevOf(proof []byte) []byte {
	h := sha256.Sum256(proof)
	return h[:]
}

// Prover proves an alpha with a server's current VRF key (secret.Keys).
type Prover interface {
	VRFProve(alpha []byte) (proof, beta []byte, err error)
	VRFPublic() []byte
}

// Make draws number n of a chain: the block a message carries.
func Make(p Prover, owner, chain string, n int64, prev []byte) (model.FairBlock, error) {
	alpha := Alpha(owner, chain, n, prev)
	proof, beta, err := p.VRFProve(alpha)
	if err != nil {
		return model.FairBlock{}, err
	}
	return model.FairBlock{V: BlockVersion, Derive: Derive, Owner: owner, N: n, PK: p.VRFPublic(),
		Alpha: alpha, Beta: beta, Proof: proof, Prev: append([]byte(nil), prev...)}, nil
}

// Key is one entry of a server's VRF key history: the key and when it began.
type Key struct {
	Key   []byte
	Began model.Time
}

// KeyAt is the key of a history in force at a moment: the last to have begun
// by then. ok is false before the first began.
func KeyAt(history []Key, at model.Time) ([]byte, bool) {
	var out []byte
	for _, k := range history {
		if k.Began <= at {
			out = k.Key
		}
	}
	return out, out != nil
}

// ErrBlock is a fair block that does not check out.
var ErrBlock = errors.New("fair: the block does not check out")

// CheckBlock checks a block as a receiver does (docs/FAIR.md section 9): its
// layout and rule are known; it is the owner's, of this chain; alpha is
// rebuilt from owner, chain, n and prev; pk is the owner's key at the
// message's time by its history; the proof verifies under pk and gives beta.
func CheckBlock(b *model.FairBlock, owner, chain string, at model.Time, history []Key) error {
	bad := func(why string) error { return fmt.Errorf("%w: %s", ErrBlock, why) }
	switch {
	case b.V != BlockVersion:
		return bad("unknown layout")
	case b.Derive != Derive:
		return bad("unknown derive rule")
	case b.Owner != owner:
		return bad("not drawn by the server it came from")
	case b.N < 1 || len(b.Prev) != 32:
		return bad("no place in a chain")
	case !bytes.Equal(b.Alpha, Alpha(owner, chain, b.N, b.Prev)):
		return bad("alpha is not this chain's")
	}
	pk, ok := KeyAt(history, at)
	if !ok || !bytes.Equal(pk, b.PK) {
		return bad("pk is not the owner's key at the time")
	}
	valid, beta := ecvrf.Verify(b.PK, b.Proof, b.Alpha)
	if !valid || !bytes.Equal(beta, b.Beta) {
		return bad("the proof does not verify")
	}
	return nil
}

// ChainOfAlpha reads the chain's name out of an alpha of this layout.
func ChainOfAlpha(alpha []byte) (string, bool) {
	b, ok := bytes.CutPrefix(alpha, []byte(alphaLabel))
	if !ok || len(b) < 2 {
		return "", false
	}
	n := int(binary.BigEndian.Uint16(b))
	b = b[2:]
	if len(b) < n+2 {
		return "", false
	}
	b = b[n:]
	m := int(binary.BigEndian.Uint16(b))
	b = b[2:]
	if len(b) != m+8+32 {
		return "", false
	}
	return string(b[:m]), true
}
