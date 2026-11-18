package fair

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/tikaydaes/churr/internal/ecvrf"
	"github.com/tikaydaes/churr/internal/model"
)

// The chain (docs/FAIR.md section 6): each conversation's draws, numbered from
// 1, each proved over an alpha that nobody chooses: the owner's name, the
// chain's name and the draw's number. Each draw stands alone. It is not
// chained to the proof before it: an ECVRF proof is not unique (a prover
// holding the key can make many valid proofs of one alpha, by its choice of
// nonce), only its output is, so a hash of the previous proof in alpha would
// let the key holder choose among next draws. n, used exactly once per
// chain, is all the order a chain needs; a gap in n is what shows a draw
// skipped or never sent.

// BlockVersion is the version of the fair block's layout.
const BlockVersion = 1

// alphaLabel opens every alpha of chains under this layout.
const alphaLabel = "churr-fair-1"

// Alpha is what draw n of a chain is proved over:
//
//	"churr-fair-1" ‖ u16be(len owner) ‖ owner ‖ u16be(len chain) ‖ chain ‖ u64be(n)
//
// owner is the drawing server's name and chain the chain's name (the
// conversation id, or the stream id of a conversation shared over the
// link), both as UTF-8.
func Alpha(owner, chain string, n int64) []byte {
	b := make([]byte, 0, len(alphaLabel)+4+len(owner)+len(chain)+8)
	b = append(b, alphaLabel...)
	b = binary.BigEndian.AppendUint16(b, uint16(len(owner)))
	b = append(b, owner...)
	b = binary.BigEndian.AppendUint16(b, uint16(len(chain)))
	b = append(b, chain...)
	return binary.BigEndian.AppendUint64(b, uint64(n))
}

// Prover proves an alpha with a server's current VRF key (secret.Keys).
type Prover interface {
	VRFProve(alpha []byte) (proof, beta []byte, err error)
	VRFPublic() []byte
}

// Make draws number n of a chain: the block a message carries.
func Make(p Prover, owner, chain string, n int64) (model.FairBlock, error) {
	alpha := Alpha(owner, chain, n)
	proof, beta, err := p.VRFProve(alpha)
	if err != nil {
		return model.FairBlock{}, err
	}
	return model.FairBlock{V: BlockVersion, Derive: Derive, Owner: owner, N: n, PK: p.VRFPublic(),
		Alpha: alpha, Beta: beta, Proof: proof}, nil
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
// rebuilt from owner, chain and n; pk is the owner's key at the
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
	case b.N < 1:
		return bad("no place in a chain")
	case !bytes.Equal(b.Alpha, Alpha(owner, chain, b.N)):
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
	if len(b) != m+8 {
		return "", false
	}
	return string(b[:m]), true
}
