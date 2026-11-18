package fair

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// cid is a client_id for tests, as text and as alpha takes it.
const cid = "01a1ee00-0000-7000-8000-0000000000c1"

func cidBytes(t *testing.T, id string) string {
	t.Helper()
	if !Canonical(id) {
		t.Fatalf("%q is not a client_id", id)
	}
	return id
}

// at is a moment for tests: a second and the message time inside it.
const (
	atSecs = 1790000000
	atTime = model.Time(atSecs*1000 + 400)
)

// fixed is a clock that stands at a second.
func fixed(secs int64) func() time.Time { return func() time.Time { return time.Unix(secs, 0) } }

// draw makes a draw with a drawer that started a minute before.
func draw(t *testing.T, p Prover, owner string, secs int64, id string) model.FairBlock {
	t.Helper()
	d := NewDrawer(p, owner, fixed(secs-60))
	d.now = fixed(secs)
	b, err := d.Draw(secs, cidBytes(t, id), "test")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Alpha's spelling, worked by hand (docs/FAIR.md section 6): the second in
// decimal, a bar, the nonce as it is; and one spelling of a nonce only.
func TestAlphaLayout(t *testing.T) {
	const want = "1790000000|01a1ee00-0000-7000-8000-0000000000c1"
	if got := Alpha(atSecs, cidBytes(t, cid)); got != want {
		t.Fatalf("alpha %q, want %q", got, want)
	}
	for _, bad := range []string{"", "01a1ee00000070008000000000000c1", "01a1ee00-0000-7000-8000-0000000000cg", "{01a1ee00-0000-7000-8000-0000000000c1}",
		"01A1EE00-0000-7000-8000-0000000000C1", " 01a1ee00-0000-7000-8000-0000000000c1", "01a1ee00-0000-7000-8000-0000000000c1 "} {
		if Canonical(bad) {
			t.Errorf("%q was taken as a nonce", bad)
		}
	}
}

// A draw depends on its second and its nonce and on nothing else, nothing of
// any other draw included: made again it is the same, and either changed
// gives another alpha. An ECVRF proof is not unique (internal/ecvrf's
// TestProofsAreNotUniqueOutputsAre makes a second valid proof of one alpha
// with a nonce of its choosing), so no proof is ever fed into an input: the
// key holder could choose among them.
func TestADrawDependsOnlyOnItsInput(t *testing.T) {
	p := prover(t, "one")
	a, b := draw(t, p, "a.example", atSecs, cid), draw(t, p, "a.example", atSecs, cid)
	if a.Alpha != Alpha(atSecs, cidBytes(t, cid)) || !bytes.Equal(a.Beta, b.Beta) || a.T != atSecs {
		t.Fatalf("the draw is not fixed by its input: %x %x", a.Beta, b.Beta)
	}
	if x := Alpha(atSecs+1, cidBytes(t, cid)); x == a.Alpha {
		t.Fatal("two seconds share an alpha")
	}
	if x := Alpha(atSecs, cidBytes(t, "01a1ee00-0000-7000-8000-0000000000c2")); x == a.Alpha {
		t.Fatal("two nonces share an alpha")
	}
}

// The drawer takes a pair of second and nonce once: the same request made
// again is drawn again, to the same result, and anyone else's use of the pair
// is refused; a second outside the window is refused with the server's time;
// a second's pairs are let go once it has left the window; and for the
// window after a start, a second no later than the start's is refused.
func TestTheDrawerTakesAPairOnce(t *testing.T) {
	p := prover(t, "one")
	clock := int64(atSecs)
	d := NewDrawer(p, "a.example", func() time.Time { return time.Unix(clock, 0) })
	id := cidBytes(t, cid)
	// Just after the start.
	if _, err := d.Draw(atSecs, id, "ann"); !errors.Is(err, model.ErrClock) {
		t.Fatalf("a draw in the second the drawer started: %v", err)
	}
	var e *model.Error
	if _, err := d.Draw(atSecs-1, id, "ann"); !errors.As(err, &e) || e.Code != model.CodeClock || e.ServerTime.IsZero() {
		t.Fatalf("a draw before the start: %v", err)
	}
	first, err := d.Draw(atSecs+1, id, "ann")
	if err != nil {
		t.Fatal(err)
	}
	again, err := d.Draw(atSecs+1, id, "ann")
	if err != nil || !bytes.Equal(again.Beta, first.Beta) {
		t.Fatalf("the same request again: %v", err)
	}
	if _, err := d.Draw(atSecs+1, id, "bob"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("someone else's use of the pair: %v", err)
	}
	// The nonce is taken for the whole window, under any second.
	if _, err := d.Draw(atSecs+2, id, "ann"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("the nonce again with another second: %v", err)
	}
	if _, err := d.Draw(atSecs+2, id, "bob"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("the nonce for another second by someone else: %v", err)
	}
	// Outside the window, either way.
	clock = atSecs + 30
	for _, secs := range []int64{clock - Window - 1, clock + Window + 1} {
		if _, err := d.Draw(secs, id, "ann"); !errors.As(err, &e) || e.Code != model.CodeClock || e.ServerTime != model.FromTime(time.Unix(clock, 0)) {
			t.Fatalf("a second %d from the clock: %v", secs-clock, err)
		}
	}
	// The window has moved on: the early seconds' pairs are gone, and the
	// nonce draws again, under another second, for anyone.
	if _, err := d.Draw(clock, id, "bob"); err != nil {
		t.Fatal(err)
	}
	if n := d.pairs(); n != 1 {
		t.Fatalf("the drawer keeps %d seconds of pairs, not the one in its window", n)
	}
}

// A block checks out only as it was made: by the owner, for this message's
// second and client_id, under the key the owner had at the time, its second
// near the message's time.
func TestCheckBlock(t *testing.T) {
	p, other := prover(t, "one"), prover(t, "two")
	history := []Key{{Key: other.pk, Began: atTime - 10000}, {Key: p.pk, Began: atTime - 5000}}
	b := draw(t, p, "a.example", atSecs, cid)
	if err := CheckBlock(&b, "a.example", cid, atTime, Window, history); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Beta, mustHash(t, b.Proof)) || b.V != BlockVersion || b.Derive != Derive {
		t.Fatalf("the block %+v", b)
	}
	bad := func(what string, b model.FairBlock, owner, id string, at model.Time) {
		t.Helper()
		if err := CheckBlock(&b, owner, id, at, Window, history); !errors.Is(err, ErrBlock) {
			t.Errorf("%s: %v", what, err)
		}
	}
	bad("another owner", b, "b.example", cid, atTime)
	bad("before the key began", b, "a.example", cid, atTime-7000)
	bad("a proof moved to another message", b, "a.example", "01a1ee00-0000-7000-8000-0000000000c2", atTime)
	bad("a message with no client_id", b, "a.example", "", atTime)
	bad("a second far from the message's time", b, "a.example", cid, atTime+(Window+1)*1000)
	moved := b
	moved.T++
	bad("another second", moved, "a.example", cid, atTime)
	beta := b
	beta.Beta = bytes.Repeat([]byte{1}, 32)
	bad("another beta", beta, "a.example", cid, atTime)
	forged := draw(t, prover(t, "three"), "a.example", atSecs, cid)
	bad("a key not in the history", forged, "a.example", cid, atTime)
}

// Only the Drawer applies the key: no file of the server but this
// package's draw.go calls VRFProve, and none but internal/secret's calls
// ecvrf.Prove, which holds the key (docs/FAIR.md section 6).
func TestOnlyTheDrawerProves(t *testing.T) {
	err := filepath.WalkDir("../..", func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			switch e.Name() {
			case "app", "node_modules", ".git", "testdata", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(path)
		if bytes.Contains(src, []byte(".VRFProve(")) && !strings.HasSuffix(rel, "internal/fair/draw.go") {
			t.Errorf("%s calls VRFProve: only the Drawer applies the key", rel)
		}
		if bytes.Contains(src, []byte("ecvrf.Prove(")) && !strings.HasSuffix(rel, "internal/secret/vrf.go") {
			t.Errorf("%s calls ecvrf.Prove: only the key's holder does, for the Drawer", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
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
// it pinned. Written with the derive vectors
// (CHURR_WRITE_FAIR_VECTORS=1).
const messageFile = "../../testdata/fair/message.json"

type messageVector struct {
	Description string         `json:"description"`
	Server      string         `json:"server"`
	Keys        []keyVector    `json:"keys"`
	Message     map[string]any `json:"message"`
}

type keyVector struct {
	Key   model.Bytes `json:"key"`
	Began model.Time  `json:"began"`
}

func makeMessageVector(t *testing.T) messageVector {
	p := prover(t, "the message vector's key")
	old := prover(t, "an older key")
	b := draw(t, p, "a.example", 1790000000, "01a1ee00-0000-7000-8000-0000000000c3")
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
		Description: "A message whose roll and inline dice took chance from its draw, as a client is handed it, with the VRF key history the client pinned for its server. Its draw's input is its own second (fair.t) and client_id. A client's check passes on it and fails on it changed (docs/FAIR.md section 9).",
		Server:      "a.example",
		Keys:        []keyVector{{Key: old.pk, Began: 1767225600000}, {Key: p.pk, Began: 1777593600000}},
		Message: map[string]any{"id": "01a1ee00-0000-7000-8000-000000000002", "conversation_id": "01a1ee00-0000-7000-8000-000000000001",
			"client_id":  "01a1ee00-0000-7000-8000-0000000000c3",
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
		Message struct {
			ClientID  string          `json:"client_id"`
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
	if err := CheckBlock(&v.Message.Fair, v.Server, v.Message.ClientID, v.Message.CreatedAt, Window, keys); err != nil {
		t.Fatal(err)
	}
}
