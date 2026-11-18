package fair

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

// The vectors file all three derivations read (docs/FAIR.md section 7). It
// is written from the cases below by
//
//	CHURR_WRITE_FAIR_VECTORS=1 go test -run '^TestDeriveVectors$' ./internal/fair
//
// once, when derive 1 was made; after that it is frozen, and a change to
// what it holds is a new derive number, never an edit.
const vectorsFile = "../../testdata/fair/derive.json"

type streamVector struct {
	Beta  string `json:"beta"`
	I     uint32 `json:"i"`
	K     uint32 `json:"k"`
	Block string `json:"block"`
}

type belowVector struct {
	Beta   string   `json:"beta"`
	I      uint32   `json:"i"`
	Ns     []uint64 `json:"ns"`
	Values []uint64 `json:"values"`
	Words  int      `json:"words"`
}

type caseVector struct {
	Label   string      `json:"label"`
	Beta    string      `json:"beta"`
	I       uint32      `json:"i"`
	Draws   []Primitive `json:"draws"`
	Results []Result    `json:"results"`
	Words   int         `json:"words"`
}

type invalidVector struct {
	Label string    `json:"label"`
	Draw  Primitive `json:"draw"`
}

type vectors struct {
	Description string          `json:"description"`
	Derive      int             `json:"derive"`
	Streams     []streamVector  `json:"streams"`
	Below       []belowVector   `json:"below"`
	Cases       []caseVector    `json:"cases"`
	Invalid     []invalidVector `json:"invalid"`
}

func beta(label string) string {
	h := sha256.Sum256([]byte("churr derive 1 vector: " + label))
	return hex.EncodeToString(h[:])
}

func term(sign, count, sides int) DiceTerm { return DiceTerm{Sign: sign, Count: count, Sides: sides} }

func dice(rows ...[]DiceTerm) Primitive {
	p := Primitive{Op: OpDice}
	for _, r := range rows {
		p.Rows = append(p.Rows, DiceRow{Terms: r})
	}
	return p
}

// made are the cases the vectors were written from.
func made() vectors {
	v := vectors{
		Description: "The vectors of derive 1 (docs/FAIR.md): the stream's blocks, whole numbers below n by rejection, every primitive and every form of dice, consumed in order from one object's stream, and primitives every implementation refuses. Go (internal/fair), JavaScript (internal/web/static/shared/fair/derive.js) and Dart (app/lib/core/fair/derive.dart) all read this file. Frozen: a change is a new derive number. beta and block are hex; words is how many 32-bit words the case takes.",
		Derive:      Derive,
	}
	for _, b := range []string{"stream a", "stream b"} {
		for _, i := range []uint32{0, 1, 7, 4294967295} {
			for _, k := range []uint32{0, 1, 2} {
				v.Streams = append(v.Streams, streamVector{Beta: beta(b), I: i, K: k})
			}
		}
	}
	v.Below = []belowVector{
		{Beta: beta("below small"), I: 0, Ns: []uint64{1, 2, 3, 6, 20, 100, 1000, 1, 2, 7, 52, 1 << 31, 1 << 32}},
		// 3·2^30 rejects every word from 0xC0000000 up: a quarter of them.
		{Beta: beta("below reject"), I: 3, Ns: []uint64{3 << 30, 3 << 30, 3 << 30, 3 << 30, 3 << 30, 3 << 30, 3 << 30, 3 << 30, 3 << 30, 3 << 30, 3 << 30, 3 << 30}},
		{Beta: beta("below odd"), I: 2, Ns: []uint64{(1 << 32) - 1, (1 << 31) + 1, 4294967291, 999999937, 12345}},
	}
	w := func(ws ...uint64) []uint64 { return ws }
	kh := func(t DiceTerm, keep string, n int) DiceTerm { t.Keep, t.KeepN = keep, n; return t }
	ex := func(t DiceTerm) DiceTerm { t.Explode = true; return t }
	su := func(t DiceTerm, s int) DiceTerm { t.Success = s; return t }
	plain := func(sign, n int) DiceTerm { return DiceTerm{Sign: sign, Value: n} }
	cases := []struct {
		label string
		i     uint32
		draws []Primitive
	}{
		{"int: a d6 and a range across zero", 0, []Primitive{{Op: OpInt, Min: 1, Max: 6}, {Op: OpInt, Min: -50, Max: 50}, {Op: OpInt, Min: 7, Max: 7}}},
		{"int: the whole signed 32-bit range", 1, []Primitive{{Op: OpInt, Min: MinInt, Max: MaxInt}, {Op: OpInt, Min: MinInt, Max: MaxInt}}},
		{"ints: ten d10 then five coins", 0, []Primitive{{Op: OpInts, Min: 1, Max: 10, Count: 10}, {Op: OpInts, Min: 0, Max: 1, Count: 5}}},
		{"ints: rejection taken in its stride", 4, []Primitive{{Op: OpInts, Min: MinInt, Max: (1 << 30) - 1, Count: 20}}},
		{"pick: one of five, two of five, all five", 0, []Primitive{{Op: OpPick, N: 5, K: 1}, {Op: OpPick, N: 5, K: 2}, {Op: OpPick, N: 5, K: 5}}},
		{"pick: with replacement", 2, []Primitive{{Op: OpPick, N: 3, K: 12, Replace: true}}},
		{"pick: weighted, with replacement", 0, []Primitive{{Op: OpPick, N: 4, K: 10, Replace: true, Weights: w(1, 2, 3, 4)}}},
		{"pick: weighted, without replacement, a weight of 0 never taken", 5, []Primitive{{Op: OpPick, N: 5, K: 4, Weights: w(5, 0, 1, 10, 3)}}},
		{"pick: weights up to 2^32", 6, []Primitive{{Op: OpPick, N: 2, K: 1, Weights: w(1<<31, 1<<31)}, {Op: OpPick, N: 3, K: 3, Weights: w(1, 1<<20, 7)}}},
		{"shuffle: one, two, ten, fifty-two", 0, []Primitive{{Op: OpShuffle, N: 1}, {Op: OpShuffle, N: 2}, {Op: OpShuffle, N: 10}, {Op: OpShuffle, N: 52}}},
		{"deal: two hands of five from 52, then the rest of a small deck", 1, []Primitive{{Op: OpDeal, N: 52, Hands: []int{5, 5}}, {Op: OpDeal, N: 4, Hands: []int{1, 3}}}},
		{"yesno: a half, a third, never, always, one in 2^32", 0, []Primitive{{Op: OpYesNo, Num: 1, Den: 2}, {Op: OpYesNo, Num: 1, Den: 3}, {Op: OpYesNo, Num: 0, Den: 5}, {Op: OpYesNo, Num: 5, Den: 5}, {Op: OpYesNo, Num: 1, Den: 1 << 32}, {Op: OpYesNo, Num: 1, Den: 1}}},
		{"dice: d20", 0, []Primitive{dice([]DiceTerm{term(1, 1, 20)})}},
		{"dice: 2d6+3", 0, []Primitive{dice([]DiceTerm{term(1, 2, 6), plain(1, 3)})}},
		{"dice: d20+1d4-1", 1, []Primitive{dice([]DiceTerm{term(1, 1, 20), term(1, 1, 4), plain(-1, 1)})}},
		{"dice: 4d6kh3", 0, []Primitive{dice([]DiceTerm{kh(term(1, 4, 6), "kh", 3)})}},
		{"dice: keep and drop, highest and lowest, with ties", 9, []Primitive{dice(
			[]DiceTerm{kh(term(1, 8, 3), "kh", 3)}, []DiceTerm{kh(term(1, 8, 3), "kl", 3)},
			[]DiceTerm{kh(term(1, 8, 3), "dh", 2)}, []DiceTerm{kh(term(1, 8, 3), "dl", 2)})}},
		{"dice: advantage and disadvantage (2d20kh1, 2d20kl1)", 0, []Primitive{dice([]DiceTerm{kh(term(1, 2, 20), "kh", 1)}, []DiceTerm{kh(term(1, 2, 20), "kl", 1)})}},
		{"dice: d% (1d100) and a d2", 0, []Primitive{dice([]DiceTerm{term(1, 1, 100)}, []DiceTerm{term(1, 3, 2)})}},
		{"dice: rows, as /roll 4d4+d6, 3d10k2", 3, []Primitive{dice([]DiceTerm{term(1, 4, 4), term(1, 1, 6)}, []DiceTerm{kh(term(1, 3, 10), "kh", 2)})}},
		{"dice: exploding d6 and d2", 0, []Primitive{dice([]DiceTerm{ex(term(1, 6, 6))}, []DiceTerm{ex(term(1, 4, 2))})}},
		{"dice: an exploding d2 meets the cap", 0, []Primitive{dice([]DiceTerm{ex(term(1, 1000, 2))})}},
		{"dice: successes, kept and exploding", 2, []Primitive{dice([]DiceTerm{su(term(1, 10, 10), 7)}, []DiceTerm{su(kh(term(1, 6, 6), "kh", 4), 5)}, []DiceTerm{su(ex(term(1, 5, 10)), 8), plain(-1, 1)})}},
		{"dice: a thousand-sided die and a hundred dice", 0, []Primitive{dice([]DiceTerm{term(1, 1, 1000), term(-1, 100, 6)})}},
		{"mixed: one object's stream, primitives one after another", 11, []Primitive{{Op: OpYesNo, Num: 2, Den: 3}, dice([]DiceTerm{term(1, 3, 8)}), {Op: OpShuffle, N: 6}, {Op: OpInt, Min: 0, Max: 99}}},
	}
	for _, c := range cases {
		v.Cases = append(v.Cases, caseVector{Label: c.label, Beta: beta(c.label), I: c.i, Draws: c.draws})
	}
	v.Invalid = []invalidVector{
		{"no such op", Primitive{Op: "coin"}},
		{"int: min above max", Primitive{Op: OpInt, Min: 2, Max: 1}},
		{"int: below the signed 32-bit range", Primitive{Op: OpInt, Min: MinInt - 1, Max: 0}},
		{"ints: no count", Primitive{Op: OpInts, Min: 1, Max: 6}},
		{"ints: too many", Primitive{Op: OpInts, Min: 1, Max: 6, Count: MaxCount + 1}},
		{"pick: more than n without replacement", Primitive{Op: OpPick, N: 3, K: 4}},
		{"pick: weights not one per position", Primitive{Op: OpPick, N: 3, K: 1, Weights: w(1, 2)}},
		{"pick: weights all 0", Primitive{Op: OpPick, N: 2, K: 1, Weights: w(0, 0)}},
		{"pick: weights over 2^32 in all", Primitive{Op: OpPick, N: 2, K: 1, Weights: w(1<<32, 1)}},
		{"pick: more than have weight, without replacement", Primitive{Op: OpPick, N: 3, K: 3, Weights: w(1, 0, 1)}},
		{"shuffle: none", Primitive{Op: OpShuffle, N: 0}},
		{"shuffle: too many", Primitive{Op: OpShuffle, N: MaxN + 1}},
		{"deal: more than the deck", Primitive{Op: OpDeal, N: 5, Hands: []int{3, 3}}},
		{"deal: an empty hand", Primitive{Op: OpDeal, N: 5, Hands: []int{0}}},
		{"yesno: num above den", Primitive{Op: OpYesNo, Num: 3, Den: 2}},
		{"yesno: den 0", Primitive{Op: OpYesNo, Num: 0, Den: 0}},
		{"dice: no rows", Primitive{Op: OpDice}},
		{"dice: a one-sided die", dice([]DiceTerm{term(1, 1, 1)})},
		{"dice: keep more than rolled", dice([]DiceTerm{kh(term(1, 2, 6), "kh", 3)})},
		{"dice: drop all", dice([]DiceTerm{kh(term(1, 2, 6), "dl", 2)})},
		{"dice: a sign of 0", dice([]DiceTerm{term(0, 1, 6)})},
		{"dice: success above the sides", dice([]DiceTerm{su(term(1, 1, 6), 7)})},
		{"dice: a plain number that explodes", dice([]DiceTerm{{Sign: 1, Value: 3, Explode: true}})},
	}
	return v
}

func unhex(t *testing.T, s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// compute fills in what each vector gives.
func compute(t *testing.T, v *vectors) {
	for j := range v.Streams {
		s := &v.Streams[j]
		b := Block(unhex(t, s.Beta), s.I, s.K)
		s.Block = hex.EncodeToString(b[:])
	}
	for j := range v.Below {
		b := &v.Below[j]
		st := NewStream(unhex(t, b.Beta), b.I)
		b.Values = nil
		for _, n := range b.Ns {
			b.Values = append(b.Values, st.Below(n))
		}
		b.Words = st.Used()
	}
	for j := range v.Cases {
		c := &v.Cases[j]
		st := NewStream(unhex(t, c.Beta), c.I)
		c.Results = nil
		for _, p := range c.Draws {
			r, err := Draw(st, p)
			if err != nil {
				t.Fatalf("%s: %v", c.Label, err)
			}
			c.Results = append(c.Results, r)
		}
		c.Words = st.Used()
	}
}

func TestDeriveVectors(t *testing.T) {
	if os.Getenv("CHURR_WRITE_FAIR_VECTORS") == "1" {
		v := made()
		compute(t, &v)
		out, err := json.MarshalIndent(v, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vectorsFile, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(vectorsFile)
	if err != nil {
		t.Fatal(err)
	}
	var file vectors
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if file.Derive != Derive || len(file.Cases) == 0 || len(file.Invalid) == 0 {
		t.Fatal("the vectors file is not derive 1's")
	}
	got := file
	got.Streams = append([]streamVector(nil), file.Streams...)
	got.Below = append([]belowVector(nil), file.Below...)
	got.Cases = append([]caseVector(nil), file.Cases...)
	compute(t, &got)
	for j := range file.Streams {
		if got.Streams[j].Block != file.Streams[j].Block {
			t.Errorf("stream %d: got %s", j, got.Streams[j].Block)
		}
	}
	for j := range file.Below {
		if !reflect.DeepEqual(got.Below[j].Values, file.Below[j].Values) || got.Below[j].Words != file.Below[j].Words {
			t.Errorf("below %d: got %v in %d words", j, got.Below[j].Values, got.Below[j].Words)
		}
	}
	for j := range file.Cases {
		a, _ := json.Marshal(got.Cases[j].Results)
		b, _ := json.Marshal(file.Cases[j].Results)
		if string(a) != string(b) || got.Cases[j].Words != file.Cases[j].Words {
			t.Errorf("%s: got %s in %d words", file.Cases[j].Label, a, got.Cases[j].Words)
		}
	}
	for _, c := range file.Invalid {
		st := NewStream(make([]byte, 32), 0)
		if _, err := Draw(st, c.Draw); !errors.Is(err, ErrPrimitive) || st.Used() != 0 {
			t.Errorf("%s: not refused (%v)", c.Label, err)
		}
	}
	// The cases the file holds are the cases made here.
	if m := made(); len(m.Cases) != len(file.Cases) || len(m.Invalid) != len(file.Invalid) {
		t.Error("the vectors file and the cases here differ; derive 1 is frozen")
	}
}

// The stream's encoding, worked by hand: SHA-256 of beta, then i and k as
// four bytes each, big-endian.
func TestBlockEncoding(t *testing.T) {
	beta := []byte{1, 2, 3}
	want := sha256.Sum256([]byte{1, 2, 3, 0, 0, 0, 5, 0, 0, 1, 0})
	if Block(beta, 5, 256) != want {
		t.Fatal("block encoding")
	}
	s := NewStream(beta, 5)
	b0 := Block(beta, 5, 0)
	for j := 0; j < 8; j++ {
		w := s.Word()
		if w != uint32(b0[4*j])<<24|uint32(b0[4*j+1])<<16|uint32(b0[4*j+2])<<8|uint32(b0[4*j+3]) {
			t.Fatalf("word %d", j)
		}
	}
	b1 := Block(beta, 5, 1)
	if s.Word()>>24 != uint32(b1[0]) {
		t.Fatal("the ninth word is the next block's first")
	}
}

// Every outcome of a small range comes up, about as often as the others:
// a coarse check that rejection keeps nothing out.
func TestBelowSpread(t *testing.T) {
	s := NewStream([]byte("spread"), 0)
	counts := make([]int, 6)
	for j := 0; j < 6000; j++ {
		counts[s.Below(6)]++
	}
	for v, c := range counts {
		if c < 850 || c > 1150 {
			t.Errorf("%d came up %d times in 6000", v+1, c)
		}
	}
}
