package palette

import (
	"bytes"
	"encoding/json"
	"flag"
	"math"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The guide's own themes (docs/STYLE.md, "Tokens"): a theme made of their
// base colours must pass.
var (
	guideDark = map[string]string{"bg": "#0d1026", "panel": "#161a3d", "raise": "#1d2250", "ink": "#efeaff", "dim": "#a09dc9",
		"lav": "#b9a0ff", "cyan": "#5df0e4", "pink": "#ff5ccf", "yel": "#ffd95e"}
	guideLight = map[string]string{"bg": "#f5f2ff", "panel": "#ffffff", "raise": "#ebe6ff", "ink": "#15173a", "dim": "#55527f",
		"lav": "#5b3fd6", "cyan": "#006e66", "pink": "#b3127f", "yel": "#ffd95e"}
)

func with(base map[string]string, over ...string) map[string]string {
	m := map[string]string{}
	for k, v := range base {
		m[k] = v
	}
	for i := 0; i+1 < len(over); i += 2 {
		if over[i+1] == "" {
			delete(m, over[i])
		} else {
			m[over[i]] = over[i+1]
		}
	}
	return m
}

func TestRatio(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want string
	}{
		{"#000000", "#ffffff", "21.00"},
		{"#ffffff", "#ffffff", "1.00"},
		{"#777777", "#ffffff", "4.47"}, // the classic just-too-light grey
		{"#767676", "#ffffff", "4.54"},
		{"#0d1026", "#efeaff", "15.97"},
	} {
		a, _ := ParseHex(c.a)
		b, _ := ParseHex(c.b)
		if got := (Failure{Ratio: Ratio(a, b)}).Measured(); got != c.want {
			t.Errorf("%s on %s: %s, want %s", c.a, c.b, got, c.want)
		}
	}
}

func TestMix(t *testing.T) {
	a, _ := ParseHex("#000000")
	b, _ := ParseHex("#ff0a01")
	for _, c := range []struct {
		k    int
		want string
	}{{0, "#000000"}, {100, "#ff0a01"}, {50, "#800501"}, {25, "#400300"}} {
		if got := Mix(a, b, c.k).Hex(); got != c.want {
			t.Errorf("mix %d: %s, want %s", c.k, got, c.want)
		}
	}
}

func TestParse(t *testing.T) {
	for _, c := range []struct {
		name  string
		raw   map[string]string
		field string
	}{
		{"guide dark", guideDark, ""},
		{"upper case is fine", with(guideDark, "bg", "#0D1026"), ""},
		{"missing", with(guideDark, "yel", ""), "yel"},
		{"extra", with(guideDark, "accent", "#ffffff"), "accent"},
		{"short", with(guideDark, "ink", "#fff"), "ink"},
		{"no hash", with(guideDark, "dim", "a09dc9ff"), "dim"},
		{"not hex", with(guideDark, "dim", "#a09dcg"), "dim"},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, field, msg := Parse(c.raw)
			if field != c.field || (c.field == "" && (msg != "" || len(got) != len(Keys))) {
				t.Fatalf("field %q (%s), want %q", field, msg, c.field)
			}
		})
	}
	if _, _, msg := Parse(nil); msg == "" {
		t.Fatal("no colours was accepted")
	}
	c, _, _ := Parse(with(guideDark, "bg", "#0D1026"))
	if c.Hexes()["bg"] != "#0d1026" {
		t.Fatal("colours are not written back in lower case")
	}
}

func TestDerive(t *testing.T) {
	for _, c := range []struct {
		name                              string
		raw                               map[string]string
		onLav, onYel, faint, edge, yelInk string
	}{
		// On dark every fill takes the ground; yellow is text by itself.
		{"dark", guideDark, "#0d1026", "#0d1026", "#8381a8", "#6d6c90", "#ffd95e"},
		// On light lavender takes the ground, yellow the ink, and yellow
		// as text is mixed towards the ink until it reads.
		{"light", guideLight, "#f5f2ff", "#15173a", "#6d6a92", "#8582a5", "#756749"},
	} {
		col, _, _ := Parse(c.raw)
		d := Derive(col)
		for _, x := range []struct{ what, got, want string }{
			{"on-lav", d.OnLav.Hex(), c.onLav}, {"on-yel", d.OnYel.Hex(), c.onYel}, {"faint", d.Faint.Hex(), c.faint},
			{"edge", d.Edge.Hex(), c.edge}, {"yel-ink", d.YelInk.Hex(), c.yelInk},
		} {
			if x.got != x.want {
				t.Errorf("%s %s: %s, want %s", c.name, x.what, x.got, x.want)
			}
		}
	}
}

func TestCheck(t *testing.T) {
	for _, c := range []struct {
		name, kind string
		raw        map[string]string
		want       string // the refusal, "" for none
	}{
		{"guide dark", Dark, guideDark, ""},
		{"guide light", Light, guideLight, ""},
		{"high distinction dark", Dark, with(guideDark, "lav", "#7fb2ff", "pink", "#ff9a4d", "yel", "#ffe066"), ""},
		{"high distinction light", Light, with(guideLight, "lav", "#1f56c4", "cyan", "#00626b", "pink", "#a8480a", "yel", "#ffe066"), ""},
		{"dim too dark", Dark, with(guideDark, "dim", "#6a6890"), "dim on bg is 3.57:1; text needs at least 4.5:1."},
		{"pink too dark", Dark, with(guideDark, "pink", "#c0207f"), "pink on bg is 3.35:1; text needs at least 4.5:1."},
		{"raise too light", Dark, with(guideDark, "raise", "#343a75"), "dim on raise is 4.06:1; text needs at least 4.5:1."},
		// Lavender is text in both clients (mentions, channel names,
		// labels, code), so it needs 4.5:1 on every ground step.
		{"lavender too dark for text", Dark, with(guideDark, "lav", "#7060c0"), "lav on bg is 3.68:1; text needs at least 4.5:1."},
		// Readable on the raised step, which here is the darkest, but not
		// on the panel: once a control edge was enough there.
		{"lavender too dark on panel", Dark, with(guideDark, "raise", "#0a0c1e", "lav", "#7a6aff"), "lav on panel is 4.26:1; text needs at least 4.5:1."},
		// Text on the yellow fill still reads (ink), but the focus ring
		// is lost against the ground.
		{"focus ring", Dark, with(guideDark, "yel", "#5a5a20"), "yel on bg is 2.60:1; a control edge needs at least 3:1."},
		{"light ink too pale", Light, with(guideLight, "ink", "#8a8aa0"), "ink on bg is 3.05:1; text needs at least 4.5:1."},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			col, _, msg := Parse(c.raw)
			if msg != "" {
				t.Fatal(msg)
			}
			if !KindFits(c.kind, col) {
				t.Fatalf("not a %s theme", c.kind)
			}
			f := Check(c.kind, col)
			got := ""
			if f != nil {
				got = f.Message()
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestKindFits(t *testing.T) {
	d, _, _ := Parse(guideDark)
	l, _, _ := Parse(guideLight)
	if !KindFits(Dark, d) || KindFits(Light, d) || !KindFits(Light, l) || KindFits(Dark, l) || KindFits("sepia", d) {
		t.Fatal("kind is not told from the ground and the ink")
	}
}

func TestEveryPairingNamesTokens(t *testing.T) {
	d, _, _ := Parse(guideDark)
	der := Derive(d)
	for _, kind := range []string{Dark, Light} {
		for _, p := range Pairings(kind) {
			for _, n := range []string{p.Fore, p.Back} {
				if Token(d, der, n) == (RGB{}) {
					t.Errorf("%s names %q, which is no token", p.Name(), n)
				}
			}
		}
	}
}

// exactLinear is a channel value's linear light worked out in exact
// arithmetic and rounded once to the nearest float64: v / 3294.6 for
// v <= 10 (x = v/255 <= 0.03928, x / 12.92), otherwise q^(12/5) for
// q = (1000v + 14025) / 269025 ((x + 0.055) / 1.055), as the fifth root of
// the exact rational q^12 by Newton's method at 320 bits. It fails rather
// than guess if the result lies too near the midpoint of two float64s to
// round with certainty.
func exactLinear(t *testing.T, v int) float64 {
	if v <= 10 {
		f, _ := new(big.Rat).SetFrac64(int64(v)*10, 32946).Float64()
		return f
	}
	const prec = 320
	q := new(big.Rat).SetFrac64(int64(1000*v+14025), 269025)
	a := new(big.Rat).SetInt64(1)
	for i := 0; i < 12; i++ {
		a.Mul(a, q)
	}
	nf := func() *big.Float { return new(big.Float).SetPrec(prec) }
	A := nf().SetRat(a)
	y := nf().SetFloat64(math.Pow(float64(1000*v+14025)/269025, 2.4))
	five := nf().SetInt64(5)
	for i := 0; i < 12; i++ { // quadratic: 53 bits to far past 320
		y4 := nf().Mul(y, y)
		y4.Mul(y4, y4)
		num := nf().Sub(nf().Mul(y4, y), A)
		y.Sub(y, num.Quo(num, nf().Mul(five, y4)))
	}
	eps := nf().SetMantExp(nf().SetInt64(1), -250)
	one := nf().SetInt64(1)
	f, _ := y.Float64()
	lo, _ := nf().Mul(y, nf().Sub(one, eps)).Float64()
	hi, _ := nf().Mul(y, nf().Add(one, eps)).Float64()
	if lo != f || hi != f {
		t.Fatalf("channel %d lies too near a rounding midpoint", v)
	}
	return f
}

// TestLinearTable is the generator of the linear table in palette.go: it
// works every entry out again from the definition, and if any differs it
// prints the whole table to paste in.
func TestLinearTable(t *testing.T) {
	var b strings.Builder
	bad := 0
	for v := 0; v < 256; v += 4 {
		b.WriteString("\t")
		for j := v; j < v+4; j++ {
			want := exactLinear(t, j)
			if math.Float64bits(Linear(j)) != math.Float64bits(want) {
				bad++
			}
			if j > v {
				b.WriteString(" ")
			}
			b.WriteString(strconv.FormatFloat(want, 'g', -1, 64) + ",")
		}
		b.WriteString("\n")
	}
	if bad > 0 {
		t.Fatalf("%d entries of linear differ from their definition; the table is:\n%s", bad, b.String())
	}
}

// op is one operation on two float64s in big.Float arithmetic at 53 bits,
// ties to even: one IEEE 754 operation, rounded once.
func op(f func(z, x, y *big.Float) *big.Float, x, y float64) float64 {
	z := new(big.Float).SetPrec(53).SetMode(big.ToNearestEven)
	r, _ := f(z, big.NewFloat(x), big.NewFloat(y)).Float64()
	return r
}

// TestNoFusedArithmetic holds Luminance and Ratio to their written
// order of operations, each rounded on its own, against arithmetic that
// cannot fuse (math/big at float64 precision). On arm64, where the Go
// compiler fuses x*y + z unless told not to, this fails if a conversion
// that prevents it is lost.
func TestNoFusedArithmetic(t *testing.T) {
	mul := (*big.Float).Mul
	add := (*big.Float).Add
	quo := (*big.Float).Quo
	lum := func(c RGB) float64 {
		r := op(mul, 0.2126, Linear(c[0]))
		g := op(mul, 0.7152, Linear(c[1]))
		b := op(mul, 0.0722, Linear(c[2]))
		return op(add, op(add, r, g), b)
	}
	rnd := rand.New(rand.NewSource(61))
	for i := 0; i < 20000; i++ {
		a := RGB{rnd.Intn(256), rnd.Intn(256), rnd.Intn(256)}
		b := RGB{rnd.Intn(256), rnd.Intn(256), rnd.Intn(256)}
		la, lb := lum(a), lum(b)
		if got := Luminance(a); math.Float64bits(got) != math.Float64bits(la) {
			t.Fatalf("luminance of %s: %v, want %v", a.Hex(), got, la)
		}
		hi, lo := la, lb
		if hi < lo {
			hi, lo = lo, hi
		}
		want := op(quo, op(add, hi, 0.05), op(add, lo, 0.05))
		if got := Ratio(a, b); math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("%s on %s: %v, want %v", a.Hex(), b.Hex(), got, want)
		}
	}
}

// The apps' copy of this package (app/lib/core/themes/palette.dart) is
// held to a corpus of this package's answers for 600 themes,
// app/test/core/themes/palette_corpus.json. TestAppCorpus keeps it
// current: it works every answer out again for the themes the file holds
// and fails if any differs. After a deliberate change of the rules:
//
//	go test ./internal/themes/palette -run TestAppCorpus -update-corpus
var updateCorpus = flag.Bool("update-corpus", false, "rewrite app/test/core/themes/palette_corpus.json from this package")

type corpusTheme struct {
	Kind      string             `json:"kind"`
	Colours   map[string]string  `json:"colours"`
	Derived   map[string]string  `json:"derived"`
	KindFits  bool               `json:"kind_fits"`
	Measured  []string           `json:"measured"`
	Refusal   string             `json:"refusal"`
	Luminance map[string]float64 `json:"luminance"` // of each base colour
	Ratios    []float64          `json:"ratios"`    // of each pairing, in order
}

type corpus struct {
	Source string        `json:"source"`
	Linear []float64     `json:"linear"` // Linear(v) for v from 0 to 255
	Themes []corpusTheme `json:"themes"`
}

func TestAppCorpus(t *testing.T) {
	path := filepath.Join("..", "..", "..", "app", "test", "core", "themes", "palette_corpus.json")
	have, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no app corpus here: %v", err)
	}
	var old corpus
	if err := json.Unmarshal(have, &old); err != nil {
		t.Fatal(err)
	}
	out := corpus{Source: "The themes (kind and colours) were made once from math/rand seed 61: a third near the guide's colours, a third further off, a third at random. Every answer is internal/themes/palette's, written by its TestAppCorpus (go test ./internal/themes/palette -run TestAppCorpus -update-corpus). Luminances and ratios are float64s, to be matched bit for bit."}
	for v := 0; v < 256; v++ {
		out.Linear = append(out.Linear, Linear(v))
	}
	for _, th := range old.Themes {
		c, _, msg := Parse(th.Colours)
		if msg != "" {
			t.Fatalf("%v: %s", th.Colours, msg)
		}
		d := Derive(c)
		e := corpusTheme{Kind: th.Kind, Colours: c.Hexes(), KindFits: KindFits(th.Kind, c), Luminance: map[string]float64{},
			Derived: map[string]string{"on-lav": d.OnLav.Hex(), "on-cyan": d.OnCyan.Hex(), "on-pink": d.OnPink.Hex(), "on-yel": d.OnYel.Hex(),
				"faint": d.Faint.Hex(), "edge": d.Edge.Hex(), "yel-ink": d.YelInk.Hex()}}
		for k, v := range c {
			e.Luminance[k] = Luminance(v)
		}
		for _, p := range Pairings(th.Kind) {
			r := Ratio(Token(c, d, p.Fore), Token(c, d, p.Back))
			e.Ratios = append(e.Ratios, r)
			e.Measured = append(e.Measured, (Failure{Ratio: r}).Measured())
		}
		if f := Check(th.Kind, c); f != nil {
			e.Refusal = f.Message()
		}
		out.Themes = append(out.Themes, e)
	}
	want, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if *updateCorpus {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
		have = want
	}
	if !bytes.Equal(have, want) {
		t.Fatalf("%s is not what this package works out now; if the rules changed on purpose, run with -update-corpus and change palette.dart to match", path)
	}
}
