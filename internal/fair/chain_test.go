package fair

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
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

// messageFile is a message with a fair block for the clients' checks
// (e2e/fair.mjs, the app's fair test): what a client is handed, the history
// it pinned, and the chain's names. Written with the derive vectors
// (CHURR_WRITE_FAIR_VECTORS=1).
const messageFile = "../../testdata/fair/message.json"

type messageVector struct {
	Description string         `json:"description"`
	Server      string         `json:"server"`
	Keys        []keyVector    `json:"keys"`
	Chains      []string       `json:"chains"`
	Message     map[string]any `json:"message"`
}

type keyVector struct {
	Key   model.Bytes `json:"key"`
	Began model.Time  `json:"began"`
}

func makeMessageVector(t *testing.T) messageVector {
	p := prover(t, "the message vector's key")
	old := prover(t, "an older key")
	b, err := Make(p, "a.example", "01a1ee00-0000-7000-8000-000000000001", 3, PrevOf([]byte("draw 2's proof")))
	if err != nil {
		t.Fatal(err)
	}
	roll := func(i uint32, rows []DiceRow) map[string]any {
		got, err := Roll(NewStream(b.Beta, i), rows)
		if err != nil {
			t.Fatal(err)
		}
		return map[string]any{"kind": "dice_roll", "level": 1, "by": "dice", "draw": map[string]any{"i": i},
			"data": map[string]any{"dice": got[0].Dice, "rows": got}}
	}
	view := roll(0, []DiceRow{{Dice: "4d6kh3", Terms: []DiceTerm{{Sign: 1, Count: 4, Sides: 6, Keep: "kh", KeepN: 3}}}})
	inline := roll(1, []DiceRow{{Dice: "2d6+3", Terms: []DiceTerm{{Sign: 1, Count: 2, Sides: 6}, {Sign: 1, Value: 3}}}})
	return messageVector{
		Description: "A message whose roll and inline dice took chance from its draw, as a client is handed it, with the VRF key history the client pinned for its server and the names the conversation's chain may have. A client's check passes on it and fails on it changed (docs/FAIR.md section 9).",
		Server:      "a.example",
		Keys:        []keyVector{{Key: old.pk, Began: 1767225600000}, {Key: p.pk, Began: 1777593600000}},
		Chains:      []string{"01a1ee00-0000-7000-8000-000000000001", "01a1ee00-0000-7000-8000-0000000000ff"},
		Message: map[string]any{"id": "01a1ee00-0000-7000-8000-000000000002", "conversation_id": "01a1ee00-0000-7000-8000-000000000001",
			"created_at": model.Time(1790000000000), "text": "Rolled 4d6kh3: …", "fair": b, "view": view,
			"entities": []any{map[string]any{"type": "object", "at": 0, "len": 9, "object": inline}}},
	}
}

func TestMessageVector(t *testing.T) {
	if os.Getenv("CHURR_WRITE_FAIR_VECTORS") == "1" {
		out, err := json.MarshalIndent(makeMessageVector(t), "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(messageFile, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(messageFile)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Server  string      `json:"server"`
		Keys    []keyVector `json:"keys"`
		Chains  []string    `json:"chains"`
		Message struct {
			CreatedAt model.Time      `json:"created_at"`
			Fair      model.FairBlock `json:"fair"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	var keys []Key
	for _, k := range v.Keys {
		keys = append(keys, Key{Key: k.Key, Began: k.Began})
	}
	if err := CheckBlock(&v.Message.Fair, v.Server, v.Chains[0], v.Message.CreatedAt, keys); err != nil {
		t.Fatal(err)
	}
}
