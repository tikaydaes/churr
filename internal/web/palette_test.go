package web

// The web pages derive and check themes in JavaScript (static/shared/
// palette.js) by the rules the server applies in Go (internal/themes/
// palette), both written out in docs/STYLE.md "Themes". This test keeps
// the two identical: it works out a table of cases with the Go package,
// keeps it in testdata/palette_cases.json, and runs the JavaScript over
// the same table under Node. e2e/palette.mjs runs the JavaScript half
// again in the browser tests' run, with no Go needed.
//
// Every luminance and ratio must come out the same in JavaScript as in Go,
// bit for bit, not merely close: the arithmetic is written so that it
// does on every machine (docs/STYLE.md "Themes", Arithmetic).
//
// The table is checked against Go on every run, so it cannot go stale
// without failing here. After a deliberate change of the rules:
//
//	go test ./internal/web -run TestPaletteCases -update
//
// Test-only: the web package itself still imports nothing of Churr's but
// the version.

import (
	"bytes"
	"encoding/json"
	"flag"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/themes/palette"
)

var updatePalette = flag.Bool("update", false, "rewrite testdata/palette_cases.json from internal/themes/palette")

type paletteCase struct {
	Name     string            `json:"name"`
	Kind     string            `json:"kind"`
	Colours  map[string]string `json:"colours"`
	KindFits bool              `json:"kind_fits"`
	Derived  map[string]string `json:"derived"`
	Refusal  string            `json:"refusal"` // palette.Check's message, "" when the theme passes
	// Ratios holds every pairing's ratio, in the order of
	// palette.Pairings, for the first cases only (the file stays small).
	Ratios []float64 `json:"ratios,omitempty"`
	// Luminance of each base colour, by key.
	Luminance map[string]float64 `json:"luminance"`
}

type paletteTable struct {
	Note string `json:"note"`
	// Linear is palette.Linear for every channel value 0 to 255: the
	// table each client holds in its source.
	Linear [256]float64 `json:"linear"`
	// Channel holds, for every channel value 0 to 255, the luminance of
	// that value alone in red, in green and in blue.
	Channel  [256][3]float64         `json:"channel"`
	Mix      []mixCase               `json:"mix"`
	Pairings map[string][]pairingOut `json:"pairings"` // by kind, in order
	Cases    []paletteCase           `json:"cases"`
}

type pairingOut struct {
	Fore string  `json:"fore"`
	Back string  `json:"back"`
	Need float64 `json:"need"`
}

type mixCase struct {
	A, B string
	K    int
	Want string
}

var (
	guideDark = map[string]string{"bg": "#0d1026", "panel": "#161a3d", "raise": "#1d2250", "ink": "#efeaff", "dim": "#a09dc9",
		"lav": "#b9a0ff", "cyan": "#5df0e4", "pink": "#ff5ccf", "yel": "#ffd95e"}
	guideLight = map[string]string{"bg": "#f5f2ff", "panel": "#ffffff", "raise": "#ebe6ff", "ink": "#15173a", "dim": "#55527f",
		"lav": "#5b3fd6", "cyan": "#006e66", "pink": "#b3127f", "yel": "#ffd95e"}
)

func withColours(base map[string]string, over ...string) map[string]string {
	m := map[string]string{}
	for k, v := range base {
		m[k] = v
	}
	for i := 0; i+1 < len(over); i += 2 {
		m[over[i]] = over[i+1]
	}
	return m
}

// buildPaletteTable works the table out with the Go package. It is
// deterministic: the random themes come from a fixed seed.
func buildPaletteTable() paletteTable {
	t := paletteTable{Note: "Made by internal/web/palette_test.go from internal/themes/palette. Do not edit by hand.", Pairings: map[string][]pairingOut{}}
	for v := 0; v < 256; v++ {
		t.Linear[v] = palette.Linear(v)
		t.Channel[v] = [3]float64{palette.Luminance(palette.RGB{v, 0, 0}), palette.Luminance(palette.RGB{0, v, 0}), palette.Luminance(palette.RGB{0, 0, v})}
	}
	for _, kind := range []string{palette.Dark, palette.Light} {
		for _, p := range palette.Pairings(kind) {
			t.Pairings[kind] = append(t.Pairings[kind], pairingOut{p.Fore, p.Back, p.Need})
		}
	}
	r := rand.New(rand.NewSource(61))
	rgb := func() palette.RGB { return palette.RGB{r.Intn(256), r.Intn(256), r.Intn(256)} }
	for i := 0; i < 60; i++ {
		a, b, k := rgb(), rgb(), r.Intn(101)
		t.Mix = append(t.Mix, mixCase{a.Hex(), b.Hex(), k, palette.Mix(a, b, k).Hex()})
	}

	add := func(name, kind string, raw map[string]string) {
		c, _, msg := palette.Parse(raw)
		if msg != "" {
			panic(name + ": " + msg)
		}
		d := palette.Derive(c)
		pc := paletteCase{Name: name, Kind: kind, Colours: c.Hexes(), KindFits: palette.KindFits(kind, c), Luminance: map[string]float64{},
			Derived: map[string]string{"on-lav": d.OnLav.Hex(), "on-cyan": d.OnCyan.Hex(), "on-pink": d.OnPink.Hex(), "on-yel": d.OnYel.Hex(),
				"faint": d.Faint.Hex(), "edge": d.Edge.Hex(), "yel-ink": d.YelInk.Hex()}}
		for k, v := range c {
			pc.Luminance[k] = palette.Luminance(v)
		}
		if f := palette.Check(kind, c); f != nil {
			pc.Refusal = f.Message()
		}
		if len(t.Cases) < 160 {
			for _, p := range palette.Pairings(kind) {
				pc.Ratios = append(pc.Ratios, palette.Ratio(palette.Token(c, d, p.Fore), palette.Token(c, d, p.Back)))
			}
		}
		t.Cases = append(t.Cases, pc)
	}

	// The guide's own sets and the cases palette_test.go names.
	add("guide dark", palette.Dark, guideDark)
	add("guide light", palette.Light, guideLight)
	add("high distinction dark", palette.Dark, withColours(guideDark, "lav", "#7fb2ff", "pink", "#ff9a4d", "yel", "#ffe066"))
	add("high distinction light", palette.Light, withColours(guideLight, "lav", "#1f56c4", "cyan", "#00626b", "pink", "#a8480a", "yel", "#ffe066"))
	add("dim too dark", palette.Dark, withColours(guideDark, "dim", "#6a6890"))
	add("pink too dark", palette.Dark, withColours(guideDark, "pink", "#c0207f"))
	add("raise too light", palette.Dark, withColours(guideDark, "raise", "#343a75"))
	add("lavender too dark for text", palette.Dark, withColours(guideDark, "lav", "#7060c0"))
	add("lavender too dark on panel", palette.Dark, withColours(guideDark, "raise", "#0a0c1e", "lav", "#7a6aff"))
	add("focus ring", palette.Dark, withColours(guideDark, "yel", "#5a5a20"))
	add("light ink too pale", palette.Light, withColours(guideLight, "ink", "#8a8aa0"))
	add("upper case", palette.Dark, withColours(guideDark, "bg", "#0D1026"))
	add("dark colours as light", palette.Light, guideDark)
	add("grey 777 on white", palette.Light, withColours(guideLight, "bg", "#ffffff", "panel", "#ffffff", "raise", "#ffffff", "dim", "#777777"))
	add("grey 767676 on white", palette.Light, withColours(guideLight, "bg", "#ffffff", "panel", "#ffffff", "raise", "#ffffff", "dim", "#767676"))

	// Each base colour of each guide set nudged a little, one at a time:
	// a mix of themes that pass and that fail at every pairing.
	for _, g := range []struct {
		kind string
		raw  map[string]string
	}{{palette.Dark, guideDark}, {palette.Light, guideLight}} {
		for i := 0; i < 180; i++ {
			key := palette.Keys[r.Intn(len(palette.Keys))]
			c, _ := palette.ParseHex(g.raw[key])
			for j := range c {
				c[j] = min(255, max(0, c[j]+r.Intn(121)-60))
			}
			add(g.kind+" nudged "+key, g.kind, withColours(g.raw, key, c.Hex()))
		}
		// Every colour nudged a little at once.
		for i := 0; i < 120; i++ {
			raw := map[string]string{}
			for _, key := range palette.Keys {
				c, _ := palette.ParseHex(g.raw[key])
				for j := range c {
					c[j] = min(255, max(0, c[j]+r.Intn(41)-20))
				}
				raw[key] = c.Hex()
			}
			add(g.kind+" all nudged", g.kind, raw)
		}
	}
	// Anything at all, of either kind.
	for i := 0; i < 120; i++ {
		raw := map[string]string{}
		for _, key := range palette.Keys {
			raw[key] = rgb().Hex()
		}
		kind := palette.Dark
		if i%2 == 1 {
			kind = palette.Light
		}
		add("random", kind, raw)
	}
	return t
}

func TestPaletteCases(t *testing.T) {
	path := filepath.Join("testdata", "palette_cases.json")
	built := buildPaletteTable()
	want, err := json.MarshalIndent(built, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if *updatePalette {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	have, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (make it with -update)", err)
	}
	if !bytes.Equal(have, want) {
		t.Fatalf("%s is not what internal/themes/palette works out now; if the rules changed on purpose, run with -update and change static/shared/palette.js to match", path)
	}
	pass, fail := 0, 0
	for _, c := range built.Cases {
		if c.Refusal == "" && c.KindFits {
			pass++
		} else {
			fail++
		}
	}
	t.Logf("%d cases: %d pass, %d refused", len(built.Cases), pass, fail)

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; static/shared/palette.js is not checked against the table here (e2e/palette.mjs does it in the browser tests' run)")
	}
	script, _ := filepath.Abs("testdata/palette_vectors.mjs")
	module, _ := filepath.Abs("static/shared/palette.js")
	table, _ := filepath.Abs(path)
	out, err := exec.Command(node, script, module, table).CombinedOutput()
	if err != nil {
		t.Fatalf("static/shared/palette.js does not agree with internal/themes/palette: %v\n%s", err, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(out)), "ok ") {
		t.Fatalf("unexpected output: %s", out)
	}
	t.Logf("node: %s", strings.TrimSpace(string(out)))
}
