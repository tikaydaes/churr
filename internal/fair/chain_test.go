package fair

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/tikaydaes/churr/internal/ecvrf"
	"github.com/tikaydaes/churr/internal/model"
)

type testProver struct{ sk, pk []byte }

func (p testProver) VRFProve(alpha []byte) ([]byte, []byte, error) {
	pi, err := ecvrf.Prove(p.sk, alpha)
	if err != nil {
		return nil, nil, err
	}
	beta, err := ecvrf.ProofToHash(pi)
	return pi, beta, err
}

func (p testProver) VRFPublic() []byte { return p.pk }

func prover(t *testing.T, seed string) testProver {
	sk := sha256.Sum256([]byte(seed))
	pk, err := ecvrf.DerivePublicKey(sk[:])
	if err != nil {
		t.Fatal(err)
	}
	return testProver{sk[:], pk}
}

// Alpha's layout, worked by hand (docs/FAIR.md section 6).
func TestAlphaLayout(t *testing.T) {
	prev := bytes.Repeat([]byte{9}, 32)
	want := append([]byte("churr-fair-1\x00\x03a.b\x00\x02c1\x00\x00\x00\x00\x00\x00\x01\x02"), prev...)
	if got := Alpha("a.b", "c1", 258, prev); !bytes.Equal(got, want) {
		t.Fatalf("alpha %x, want %x", got, want)
	}
	if c, ok := ChainOfAlpha(want); !ok || c != "c1" {
		t.Fatalf("the chain read back as %q", c)
	}
	if _, ok := ChainOfAlpha(want[:len(want)-1]); ok {
		t.Error("a short alpha was read")
	}
}

// A block checks out only as it was made: by the owner, on its chain, under
// the key the owner had at the time.
func TestCheckBlock(t *testing.T) {
	p, other := prover(t, "one"), prover(t, "two")
	history := []Key{{Key: other.pk, Began: 1000}, {Key: p.pk, Began: 2000}}
	b, err := Make(p, "a.example", "chain", 2, PrevOf([]byte("proof 1")))
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckBlock(&b, "a.example", "chain", 2500, history); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Beta, mustHash(t, b.Proof)) || b.V != BlockVersion || b.Derive != Derive {
		t.Fatalf("the block %+v", b)
	}
	bad := func(what string, b model.FairBlock, owner, chain string, at model.Time) {
		t.Helper()
		if err := CheckBlock(&b, owner, chain, at, history); !errors.Is(err, ErrBlock) {
			t.Errorf("%s: %v", what, err)
		}
	}
	bad("another owner", b, "b.example", "chain", 2500)
	bad("another chain", b, "a.example", "other", 2500)
	bad("before the key began", b, "a.example", "chain", 1500)
	moved := b
	moved.N = 3
	bad("another n", moved, "a.example", "chain", 2500)
	beta := b
	beta.Beta = bytes.Repeat([]byte{1}, 32)
	bad("another beta", beta, "a.example", "chain", 2500)
	forged, _ := Make(prover(t, "three"), "a.example", "chain", 2, b.Prev)
	bad("a key not in the history", forged, "a.example", "chain", 2500)
}

func mustHash(t *testing.T, pi []byte) []byte {
	b, err := ecvrf.ProofToHash(pi)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
