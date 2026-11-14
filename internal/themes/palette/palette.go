// Package palette holds the colour arithmetic of themes (ruling R61,
// docs/STYLE.md "Themes"): the base colours a theme stores, the colours
// every client derives from them, and the contrast check that decides
// whether a theme may be saved. It is pure: no I/O, no state.
//
// The web chat and the apps derive and check with the same rules, written
// out in docs/STYLE.md, so a theme the server accepts looks the same and
// passes the same check everywhere. Change one and change all three.
package palette

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Keys are the base token names a theme stores, in the order they are
// listed and checked: the three ground steps, ink, dim, and the four role
// colours.
var Keys = []string{"bg", "panel", "raise", "ink", "dim", "lav", "cyan", "pink", "yel"}

// The two kinds of theme.
const (
	Dark  = "dark"
	Light = "light"
)

// The contrast a pairing needs.
const (
	TextRatio = 4.5 // text against what it sits on
	EdgeRatio = 3.0 // control edges, icons, presence shapes, the focus ring
)

// RGB is one colour, 0 to 255 per channel.
type RGB [3]int

// ParseHex reads "#rrggbb" (either case).
func ParseHex(s string) (RGB, bool) {
	if len(s) != 7 || s[0] != '#' {
		return RGB{}, false
	}
	var c RGB
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(s[1+2*i:3+2*i], 16, 8)
		if err != nil {
			return RGB{}, false
		}
		c[i] = int(v)
	}
	return c, true
}

// Hex writes the colour as lowercase "#rrggbb".
func (c RGB) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2]) }

// linear is each channel value's linear light, the inner step of WCAG 2
// relative luminance: for v from 0 to 255, with x = v / 255, the exact
// real value of x / 12.92 when x <= 0.03928 (v <= 10), otherwise
// ((x + 0.055) / 1.055) ^ 2.4, rounded once to the nearest float64 (ties
// to even). Each literal is the shortest decimal that reads back as that
// float64, so Go, JavaScript and Dart all read the same bits.
//
// Written out, not computed, so no client's power function (math.Pow
// differs from the browsers' Math.pow in the last bit) takes part. The
// web chat (static/shared/palette.js) and the apps
// (app/lib/core/themes/palette.dart) hold the same table, character for
// character. TestLinearTable is the generator: it works every entry out
// again in exact rational arithmetic (math/big) and prints the table to
// paste here if any entry differs.
var linear = [256]float64{
	0, 0.0003035269835488375, 0.000607053967097675, 0.0009105809506465125,
	0.00121410793419535, 0.0015176349177441874, 0.001821161901293025, 0.0021246888848418626,
	0.0024282158683907, 0.0027317428519395373, 0.003035269835488375, 0.0033465357638991586,
	0.003676507324047435, 0.004024717018496304, 0.0043914420374102925, 0.0047769534806937275,
	0.005181516702338385, 0.005605391624202721, 0.006048833022857052, 0.006512090792594472,
	0.006995410187265385, 0.00749903204322617, 0.008023192985384993, 0.008568125618069302,
	0.009134058702220785, 0.009721217320237844, 0.010329823029626936, 0.01096009400648824,
	0.011612245179743881, 0.012286488356915867, 0.012983032342173007, 0.013702083047289683,
	0.014443843596092541, 0.015208514422912706, 0.015996293365509628, 0.016807375752887377,
	0.017641954488384078, 0.01850022012837969, 0.019382360956935723, 0.02028856305665239,
	0.021219010376003555, 0.022173884793387375, 0.023153366178110403, 0.02415763244850475,
	0.025186859627361623, 0.02624122189484989, 0.02732089163907489, 0.028426039504420787,
	0.029556834437808797, 0.03071344373299362, 0.03189603307301152, 0.03310476657088505,
	0.03433980680868216, 0.03560131487502032, 0.03688945040110002, 0.03820437159534648,
	0.03954623527673283, 0.04091519690685317, 0.042311410620809654, 0.04373502925697345,
	0.04518620438567554, 0.04666508633688008, 0.048171824226889405, 0.04970656598412722,
	0.051269458374043224, 0.05286064702318025, 0.054480276442442355, 0.05612849004960008,
	0.05780543019106721, 0.059511238162981185, 0.061246054231617594, 0.06301001765316766,
	0.06480326669290576, 0.06662593864377288, 0.06847816984440015, 0.07036009569659588,
	0.07227185068231748, 0.07421356838014961, 0.07618538148130781, 0.07818742180518633,
	0.08021982031446831, 0.0822827071298148, 0.08437621154414877, 0.08650046203654974,
	0.08865558628577294, 0.09084171118340768, 0.09305896284668742, 0.09530746663096466,
	0.09758734714186242, 0.09989872824711389, 0.10224173308810128, 0.10461648409110416,
	0.10702310297826759, 0.10946171077829933, 0.11193242783690557, 0.11443537382697372,
	0.11697066775851081, 0.1195384279883456, 0.12213877222960184, 0.12477181756095046,
	0.12743768043564743, 0.13013647669036427, 0.13286832155381792, 0.13563332965520564,
	0.13843161503245183, 0.14126329114027164, 0.14412847085805772, 0.14702726649759498,
	0.14995978981060853, 0.15292615199615014, 0.15592646370782734, 0.15896083506088035,
	0.162029375639111, 0.1651321945016676, 0.1682694001896907, 0.17144110073282254,
	0.17464740365558498, 0.17788841598362912, 0.18116424424986013, 0.1844749945004409,
	0.18782077230067779, 0.19120168274079136, 0.1946178304415757, 0.1980693195599488,
	0.20155625379439707, 0.2050787363903169, 0.20863687014525567, 0.2122307574140551,
	0.21586050011389915, 0.21952619972926918, 0.22322795731680842, 0.22696587351009834,
	0.23074004852434896, 0.23455058216100508, 0.23839757381227095, 0.24228112246555472,
	0.2462013267078354, 0.2501582847299533, 0.2541520943308267, 0.25818285292159576,
	0.262250657529696, 0.2663556048028623, 0.27049779101306576, 0.27467731206038454,
	0.27889426347681034, 0.28314874042999194, 0.2874408377269174, 0.29177064981753587,
	0.29613827079832095, 0.3005437944157764, 0.3049873140698861, 0.30946892281750843,
	0.3139887133757175, 0.31854677812509175, 0.3231432091129507, 0.32777809805654207,
	0.3324515363461792, 0.33716361504833026, 0.34191442490866075, 0.3467040563550295,
	0.3515325995004392, 0.3564001441459434, 0.36130677978350945, 0.3662525955988394,
	0.37123768047414896, 0.3762621229909062, 0.38132601143253, 0.3864294337870489,
	0.3915724777497231, 0.3967552307256268, 0.4019777798321956, 0.40724021190173665,
	0.4125426134839036, 0.41788507084813725, 0.4232676699860715, 0.42869049661390657,
	0.4341536361747488, 0.43965717384091874, 0.44520119451622775, 0.45078578283822335,
	0.4564110231804045, 0.46207699965440685, 0.4677837961121588, 0.4735314961480093,
	0.47932018310082664, 0.4851499400560704, 0.49102084984783545, 0.49693299506087035,
	0.5028864580325684, 0.5088813208549335, 0.5149176653765213, 0.5209955732043541,
	0.527115125705813, 0.533276404010505, 0.539479489012107, 0.5457244613701866,
	0.5520114015119999, 0.5583403896342677, 0.5647115057049289, 0.5711248294648729,
	0.5775804404296505, 0.584078417891164, 0.5906188409193368, 0.5972017883637631,
	0.6038273388553375, 0.6104955708078647, 0.6172065624196509, 0.6239603916750759,
	0.6307571363461467, 0.6375968739940324, 0.644479681970582, 0.6514056374198239,
	0.6583748172794482, 0.6653872982822719, 0.6724431569576873, 0.6795424696330937,
	0.6866853124353132, 0.6938717612919898, 0.7011018919329731, 0.7083757798916867,
	0.7156935005064805, 0.7230551289219689, 0.7304607400903533, 0.7379104087727307,
	0.7454042095403872, 0.7529422167760778, 0.7605245046752922, 0.7681511472475069,
	0.7758222183174234, 0.7835377915261932, 0.79129794033263, 0.7991027380144087,
	0.8069522576692514, 0.8148465722161011, 0.8227857543962833, 0.8307698767746545,
	0.8387990117407399, 0.8468732315098577, 0.8549926081242336, 0.863157213454102,
	0.8713671191987971, 0.8796223968878317, 0.8879231178819664, 0.8962693533742666,
	0.9046611743911491, 0.9130986517934189, 0.9215818562772945, 0.9301108583754234,
	0.9386857284578878, 0.9473065367331996, 0.955973353249286, 0.9646862478944651,
	0.9734452903984123, 0.982250550333117, 0.9911020971138297, 1,
}

// Linear is the linear light of one channel value, 0 to 255: the table
// above.
func Linear(v int) float64 { return linear[v] }

// Luminance is WCAG 2 relative luminance, worked out so that every
// machine and every client gets the same float64, bit for bit
// (docs/STYLE.md, "Themes", Arithmetic):
//
//	r = 0.2126 * Linear(R)   each product rounded to float64 on its own
//	g = 0.7152 * Linear(G)
//	b = 0.0722 * Linear(B)
//	L = (r + g) + b          added in this order
//
// The explicit float64 conversions are what make it so: the Go
// specification lets the compiler fuse x*y + z into one fused
// multiply-add (it does on arm64, not on amd64) unless the product is
// converted, which rounds it.
func Luminance(c RGB) float64 {
	r := float64(0.2126 * linear[c[0]])
	g := float64(0.7152 * linear[c[1]])
	b := float64(0.0722 * linear[c[2]])
	return float64(r+g) + b
}

// Ratio is the WCAG contrast ratio of two colours, 1 to 21:
// (Lhigh + 0.05) / (Llow + 0.05), two additions and a division, each
// rounded to float64. There is no product here to fuse.
func Ratio(a, b RGB) float64 {
	x, y := Luminance(a), Luminance(b)
	if x < y {
		x, y = y, x
	}
	return float64(x+0.05) / float64(y+0.05)
}

// Mix is b laid over a at k per cent, channel by channel, in whole numbers:
// floor((a*(100-k) + b*k + 50) / 100). Integer arithmetic, so every client
// gets the same colour.
func Mix(a, b RGB, k int) RGB {
	var c RGB
	for i := range c {
		c[i] = (a[i]*(100-k) + b[i]*k + 50) / 100
	}
	return c
}

// Colours are a theme's base colours by key.
type Colours map[string]RGB

// Parse checks a theme's stored colours: exactly the keys of Keys, each a
// six-digit hex colour. It returns the colours and, on a fault, the key
// at fault (or "" for the object as a whole) and what is wrong.
func Parse(raw map[string]string) (Colours, string, string) {
	if len(raw) == 0 {
		return nil, "", "Give the theme's colours."
	}
	known := map[string]bool{}
	for _, k := range Keys {
		known[k] = true
	}
	for k := range raw {
		if !known[k] {
			return nil, k, fmt.Sprintf("%q is not a theme colour. The colours are %s.", k, strings.Join(Keys, ", "))
		}
	}
	out := Colours{}
	for _, k := range Keys {
		v, ok := raw[k]
		if !ok {
			return nil, k, fmt.Sprintf("The colour %q is missing. The colours are %s.", k, strings.Join(Keys, ", "))
		}
		c, ok := ParseHex(v)
		if !ok {
			return nil, k, fmt.Sprintf("The colour %q must be six hex digits after #, like #0d1026.", k)
		}
		out[k] = c
	}
	return out, "", ""
}

// Hexes is the colours as the stored object: each key to "#rrggbb".
func (c Colours) Hexes() map[string]string {
	out := make(map[string]string, len(c))
	for k, v := range c {
		out[k] = v.Hex()
	}
	return out
}

// Derived are the colours a client works out from the base colours.
type Derived struct {
	OnLav, OnCyan, OnPink, OnYel RGB // the partner text colour of each role fill
	Faint                        RGB // section names, times, placeholders
	Edge                         RGB // the edge of a control
	YelInk                       RGB // yellow where it must be text or a thin line
}

// smallest returns the colour Mix(from, to, k) for the smallest k from 0 to
// 100 at which it reaches need against every surface, or to if none does.
func smallest(from, to RGB, need float64, surfaces ...RGB) RGB {
	for k := 0; k <= 100; k++ {
		c := Mix(from, to, k)
		ok := true
		for _, s := range surfaces {
			if Ratio(c, s) < need {
				ok = false
				break
			}
		}
		if ok {
			return c
		}
	}
	return to
}

// Derive works out the derived colours (docs/STYLE.md, "Themes"):
//
//   - on-X for each role fill X is the theme's ink or its ground (bg),
//     whichever has the higher contrast against X; ink on a tie.
//   - faint is Mix(bg, dim, k) for the smallest k that reaches 4.5:1
//     against bg and panel.
//   - edge is Mix(bg, dim, k) for the smallest k that reaches 3:1 against
//     bg, panel and raise.
//   - yel-ink is yel if it reaches 4.5:1 against bg, panel and raise, and
//     otherwise Mix(yel, ink, k) for the smallest k that does.
//
// When no k reaches the goal the colour is the end of the mix (dim, or
// ink), and the check then refuses the theme.
func Derive(c Colours) Derived {
	bg, panel, raise, ink, dim := c["bg"], c["panel"], c["raise"], c["ink"], c["dim"]
	partner := func(fill RGB) RGB {
		if Ratio(ink, fill) >= Ratio(bg, fill) {
			return ink
		}
		return bg
	}
	return Derived{
		OnLav: partner(c["lav"]), OnCyan: partner(c["cyan"]), OnPink: partner(c["pink"]), OnYel: partner(c["yel"]),
		Faint:  smallest(bg, dim, TextRatio, bg, panel),
		Edge:   smallest(bg, dim, EdgeRatio, bg, panel, raise),
		YelInk: smallest(c["yel"], ink, TextRatio, bg, panel, raise),
	}
}

// Pairing is one thing drawn on another that must reach a contrast.
type Pairing struct {
	Fore, Back string // token names, as docs/STYLE.md writes them
	Need       float64
}

// Name is how the pairing is written in a refusal: "dim on raise".
func (p Pairing) Name() string { return p.Fore + " on " + p.Back }

// Pairings are what Check measures, in order. Every role colour that the
// web chat or the apps draw as text (cyan, pink, lav, and yellow as
// yel-ink; yel itself is never text) must reach 4.5:1 on each ground
// step; lavender's icons and edges need only 3:1, which that covers. A
// dark theme also has the yellow focus ring on each ground; on a light
// theme the ring carries a dark halo, so ink on each ground (already a
// text pairing) covers it.
func Pairings(kind string) []Pairing {
	var out []Pairing
	on := func(fore string, need float64, backs ...string) {
		for _, b := range backs {
			out = append(out, Pairing{fore, b, need})
		}
	}
	all := []string{"bg", "panel", "raise"}
	// Text: 4.5:1.
	on("ink", TextRatio, all...)
	on("dim", TextRatio, all...)
	on("faint", TextRatio, "bg", "panel") // faint is never text on raise
	on("cyan", TextRatio, all...)         // links
	on("pink", TextRatio, all...)         // problems, danger in a menu
	on("yel-ink", TextRatio, all...)      // yellow as text
	on("lav", TextRatio, all...)          // mentions, channel names, labels, code
	on("on-lav", TextRatio, "lav")
	on("on-cyan", TextRatio, "cyan")
	on("on-pink", TextRatio, "pink")
	on("on-yel", TextRatio, "yel")
	// Control edges, icons and shapes: 3:1.
	on("edge", EdgeRatio, all...)
	on("faint", EdgeRatio, "raise") // the offline light, a switch that is off
	if kind == Dark {
		on("yel", EdgeRatio, all...) // the focus ring
	}
	return out
}

// Failure is the first pairing below its need.
type Failure struct {
	Pairing Pairing
	Ratio   float64
}

// Measured is the ratio as a refusal and the editor show it: cut, not
// rounded, to two decimals, so a pairing that fails never reads as 4.50.
func (f Failure) Measured() string { return fmt.Sprintf("%.2f", math.Floor(f.Ratio*100)/100) }

// Message says what failed, for `invalid_value`.
func (f Failure) Message() string {
	what := "text"
	if f.Pairing.Need == EdgeRatio {
		what = "a control edge"
	}
	return fmt.Sprintf("%s is %s:1; %s needs at least %g:1.", f.Pairing.Name(), f.Measured(), what, f.Pairing.Need)
}

// Token returns a base or derived colour by its docs/STYLE.md name.
func Token(c Colours, d Derived, name string) RGB {
	switch name {
	case "on-lav":
		return d.OnLav
	case "on-cyan":
		return d.OnCyan
	case "on-pink":
		return d.OnPink
	case "on-yel":
		return d.OnYel
	case "faint":
		return d.Faint
	case "edge":
		return d.Edge
	case "yel-ink":
		return d.YelInk
	}
	return c[name]
}

// KindFits reports whether the colours are of the kind: a dark theme's
// ground is darker than its ink, a light theme's lighter.
func KindFits(kind string, c Colours) bool {
	g, i := Luminance(c["bg"]), Luminance(c["ink"])
	switch kind {
	case Dark:
		return g < i
	case Light:
		return g > i
	}
	return false
}

// Check measures every pairing of a theme and returns the first below its
// need, or nil when the theme may be saved.
func Check(kind string, c Colours) *Failure {
	d := Derive(c)
	for _, p := range Pairings(kind) {
		r := Ratio(Token(c, d, p.Fore), Token(c, d, p.Back))
		if r < p.Need {
			return &Failure{Pairing: p, Ratio: r}
		}
	}
	return nil
}
