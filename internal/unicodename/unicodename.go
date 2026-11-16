// Package unicodename holds the names and general categories of Unicode
// code points, from the Unicode Character Database of Version
// (tools/unicode-gen), for lookupCodePoints. It is read into memory the
// first time a name is asked for.
package unicodename

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Version is the Unicode version the names are of.
const Version = "16.0.0"

//go:embed ucd.txt.gz
var blob []byte

type entry struct{ name, cat string }

type span struct {
	first, last rune
	cat, prefix string
}

var (
	once   sync.Once
	named  map[rune]entry
	ranges []span
)

func load() {
	named = make(map[rune]entry, 40000)
	zr, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		panic("unicodename: " + err.Error())
	}
	sc := bufio.NewScanner(zr)
	var at rune
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "=") {
			f := strings.SplitN(line[1:], ";", 4)
			first, _ := strconv.ParseUint(f[0], 16, 32)
			last, _ := strconv.ParseUint(f[1], 16, 32)
			ranges = append(ranges, span{rune(first), rune(last), f[2], f[3]})
			continue
		}
		f := strings.SplitN(line, ";", 3)
		d, _ := strconv.ParseUint(f[0], 16, 32)
		at += rune(d)
		named[at] = entry{f[2], f[1]}
	}
	if err := sc.Err(); err != nil {
		panic("unicodename: " + err.Error())
	}
}

// Lookup gives a code point's name ("" when it has none) and its general
// category ("Cn" when it is unassigned).
func Lookup(cp rune) (name, category string) {
	once.Do(load)
	if e, ok := named[cp]; ok {
		return e.name, e.cat
	}
	for _, s := range ranges {
		if cp >= s.first && cp <= s.last {
			switch s.prefix {
			case "":
				return "", s.cat
			case "*hangul":
				return hangul(cp), s.cat
			}
			return fmt.Sprintf("%s%04X", s.prefix, cp), s.cat
		}
	}
	return "", "Cn"
}

// The jamo of Hangul syllable names (Unicode chapter 3.12).
var (
	jamoL = []string{"G", "GG", "N", "D", "DD", "R", "M", "B", "BB", "S", "SS", "", "J", "JJ", "C", "K", "T", "P", "H"}
	jamoV = []string{"A", "AE", "YA", "YAE", "EO", "E", "YEO", "YE", "O", "WA", "WAE", "OE", "YO", "U", "WEO", "WE", "WI", "YU", "EU", "YI", "I"}
	jamoT = []string{"", "G", "GG", "GS", "N", "NJ", "NH", "D", "L", "LG", "LM", "LB", "LS", "LT", "LP", "LH", "M", "B", "BS", "S", "SS", "NG", "J", "C", "K", "T", "P", "H"}
)

func hangul(cp rune) string {
	s := int(cp - 0xAC00)
	return "HANGUL SYLLABLE " + jamoL[s/588] + jamoV[s%588/28] + jamoT[s%28]
}

// CategoryName is a general category in words: "So" is "Other Symbol".
func CategoryName(cat string) string { return categories[cat] }

var categories = map[string]string{
	"Lu": "Uppercase Letter", "Ll": "Lowercase Letter", "Lt": "Titlecase Letter", "Lm": "Modifier Letter", "Lo": "Other Letter",
	"Mn": "Nonspacing Mark", "Mc": "Spacing Mark", "Me": "Enclosing Mark",
	"Nd": "Decimal Number", "Nl": "Letter Number", "No": "Other Number",
	"Pc": "Connector Punctuation", "Pd": "Dash Punctuation", "Ps": "Open Punctuation", "Pe": "Close Punctuation",
	"Pi": "Initial Punctuation", "Pf": "Final Punctuation", "Po": "Other Punctuation",
	"Sm": "Math Symbol", "Sc": "Currency Symbol", "Sk": "Modifier Symbol", "So": "Other Symbol",
	"Zs": "Space Separator", "Zl": "Line Separator", "Zp": "Paragraph Separator",
	"Cc": "Control", "Cf": "Format", "Cs": "Surrogate", "Co": "Private Use", "Cn": "Unassigned",
}
