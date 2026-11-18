package fair

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"

	"github.com/tikaydaes/churr/internal/ecvrf"
	"github.com/tikaydaes/churr/internal/model"
)

// A draw (docs/FAIR.md section 6). Every draw a server's VRF key makes, of
// any kind, is proved over an input of one shape, a text:
//
//	<t>|<nonce>
//
// t being a second in Unix time, in decimal with no leading zeros, and nonce
// a UUID in its canonical spelling, lowercase and hyphenated; the VRF is
// given the text's UTF-8 bytes. There is one spelling for a pair, so a pair
// has one input. Every draw goes through the one tracker of used pairs
// (Drawer) before the key is applied: no pair is ever proved twice, and the
// key is used for nothing else. For a message's draw the client gives both:
// t, by its clock corrected to the server's, and the message's client_id as
// the nonce, made before sending it. The person commits to the request
// without being able to work out its result, which needs the server's key;
// the server cannot know or arrange the result before the request arrives,
// cannot choose among seconds (they are the client's), and, a VRF having one
// output for a key and an input, cannot change it after. The key says whose
// draw it is; nothing of the conversation is in the input.
//
// Nothing of another draw goes into an input. An ECVRF proof is not unique:
// a prover holding the key can make many valid proofs of one input, by its
// choice of nonce; only the output is unique. Feeding a proof into an input
// would let the key holder choose.

// BlockVersion is the version of the fair block's layout.
const BlockVersion = 1

// Window is how far a draw's second may be from the server's clock, either
// way, when the draw is made; a verifier holds a message's second to the
// same distance from the message's time.
const Window = 10 // seconds

// LinkWindow is the looser distance a server and a client allow between a
// draw's second and the time of a message that came over the link from
// another server, whose clock may be off from this one's.
const LinkWindow = 120 // seconds

// Alpha is the input of a draw: "<t>|<nonce>". The caller has checked that
// the nonce is canonical (Canonical) and t above 0.
func Alpha(t int64, nonce string) string {
	return strconv.FormatInt(t, 10) + "|" + nonce
}

// Canonical reports whether a nonce is a UUID in its one spelling:
// lowercase hexadecimal, hyphenated 8-4-4-4-12, nothing around it.
func Canonical(nonce string) bool {
	if len(nonce) != 36 {
		return false
	}
	for i, c := range nonce {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
	}
	return true
}

// Prover proves an alpha with a server's current VRF key (secret.Keys).
// Only Drawer.Draw calls it (TestOnlyTheDrawerProves).
type Prover interface {
	VRFProve(alpha []byte) (proof, beta []byte, err error)
	VRFPublic() []byte
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
// layout and rule are known; it is the owner's; alpha is exactly the one
// spelt from the block's second and this message's own client_id, so a
// proof cannot be moved to another message; the second is within window
// seconds of the message's time; pk is the owner's key at the message's time
// by its history; the proof verifies under pk and gives beta.
func CheckBlock(b *model.FairBlock, owner, clientID string, at model.Time, window int64, history []Key) error {
	bad := func(why string) error { return fmt.Errorf("%w: %s", ErrBlock, why) }
	secs := int64(at) / 1000
	switch {
	case b.V != BlockVersion:
		return bad("unknown layout")
	case b.Derive != Derive:
		return bad("unknown derive rule")
	case b.Owner != owner:
		return bad("not drawn by the server it came from")
	case !Canonical(clientID):
		return bad("the message has no client_id to draw by")
	case b.T < 1 || b.T < secs-window || b.T > secs+window:
		return bad("its second is not the message's time")
	case b.Alpha != Alpha(b.T, clientID):
		return bad("alpha is not this message's")
	}
	pk, ok := KeyAt(history, at)
	if !ok || !bytes.Equal(pk, b.PK) {
		return bad("pk is not the owner's key at the time")
	}
	valid, beta := ecvrf.Verify(b.PK, b.Proof, []byte(b.Alpha))
	if !valid || !bytes.Equal(beta, b.Beta) {
		return bad("the proof does not verify")
	}
	return nil
}
