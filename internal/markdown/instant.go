package markdown

import (
	"strconv"

	"github.com/tikaydaes/churr/internal/instant"
)

// The grammar of a typed instant (ruling R69, docs/MESSAGE-FORMAT.md
// section 3.1). It is read from the shape of the text alone, like a mention:
// whether it becomes an `instant` entity is Resolve's answer.
//
//	instant = date sep time [ zone ]
//	date    = YYYY "-" MM "-" DD              a real day of the calendar
//	sep     = "T" | " "                       one space
//	time    = HH ":" MM [ ":" SS [ "." 1*9DIGIT ] ]   00:00 to 23:59:59
//	zone    = "Z" | offset | " " name
//	offset  = ("+" | "-") HH [ [":"] MM ]     hours to 14, minutes to 59
//	name    = an IANA name (a word with at least one "/") | "UTC" | "GMT"
//
// It stands on word boundaries: the character before is not a letter, a
// digit or one of . - + / : @ # \, and the character after is not a letter
// or a digit, nor one of : - + / . , – followed by a digit. A "-" offset
// counts only after "T" or after seconds, so that the range
// "2026-11-20 10:00-11:30" is not read as the offset -11:30: such a range
// stays plain text. A time followed by a space and something that looks
// like a zone abbreviation ("EST", "CEST") or a spaced offset ("+10:00") is
// plain text too, rather than being read in the author's zone when the
// author meant another. Escapes, inline code, code blocks, link text and
// addresses never hold an instant: the parser reaches none of them here.

// instant reads an instant at i, a digit. Local is the date and time as
// "YYYY-MM-DDTHH:MM[:SS[.f]]"; Zone is the zone exactly as written, or "".
func (s *segment) instant(i, a, b int) (Inline, bool) {
	src := s.src
	if prev, ok := runeBefore(src, i, a); ok && (isWord(prev) || isRuneIn(prev, ".-+/:@#\\")) {
		return Inline{}, false
	}
	digits := func(j, n int) bool {
		if j+n > b {
			return false
		}
		for k := j; k < j+n; k++ {
			if !isDigit(src[k]) {
				return false
			}
		}
		return true
	}
	if !digits(i, 4) || i+11 > b || src[i+4] != '-' || !digits(i+5, 2) || src[i+7] != '-' || !digits(i+8, 2) {
		return Inline{}, false
	}
	j := i + 10
	sepT := src[j] == 'T'
	if !sepT && src[j] != ' ' {
		return Inline{}, false
	}
	j++
	if !digits(j, 2) || j+5 > b || src[j+2] != ':' || !digits(j+3, 2) {
		return Inline{}, false
	}
	j += 5
	seconds := false
	if digits(j+1, 2) && src[j] == ':' {
		seconds = true
		j += 3
		if j+1 < b && src[j] == '.' && isDigit(src[j+1]) {
			k := j + 1
			for k < b && isDigit(src[k]) {
				k++
			}
			if k-(j+1) > 9 {
				return Inline{}, false
			}
			j = k
		}
	}
	local := []uint16(nil)
	local = append(local, src[i:j]...)
	local[10] = 'T'

	zone := ""
	if j < b {
		switch c := src[j]; c {
		case 'Z':
			zone = "Z"
			j++
		case '+', '-':
			if k, ok := offsetRun(src, j, b); ok {
				if c == '-' && !sepT && !seconds {
					return Inline{}, false
				}
				zone = unitsString(src[j:k])
				j = k
			}
		case ' ':
			k := j + 1
			switch {
			case k < b && isASCIILetter(src[k]):
				e := k
				for e < b && (isASCIILetter(src[e]) || isDigit(src[e]) || isRuneIn(rune(src[e]), "_+-/")) {
					e++
				}
				for e > k && isRuneIn(rune(src[e-1]), "+-/") {
					e--
				}
				name := unitsString(src[k:e])
				switch {
				case zoneShape(name), name == "UTC", name == "GMT":
					zone = name
					j = e
				case abbreviation(name):
					return Inline{}, false
				}
			case k+1 < b && (src[k] == '+' || src[k] == '-') && isDigit(src[k+1]):
				return Inline{}, false
			}
		}
	}
	if r, ok := runeAt(src, j, b); ok {
		if isWord(r) {
			return Inline{}, false
		}
		if isRuneIn(r, ":-+/.,–") {
			if r2, ok := runeAt(src, j+1, b); ok && r2 >= '0' && r2 <= '9' {
				return Inline{}, false
			}
		}
	}
	if !validLocal(local) {
		return Inline{}, false
	}
	return Inline{Kind: Instant, At: i, Len: j - i, Text: unitsString(src[i:j]), Local: unitsString(local), Zone: zone}, true
}

// offsetRun reads a numeric offset at i: a sign, two digits, then
// optionally two more with or without a colon. It reports the end and
// whether the offset is one the grammar allows.
func offsetRun(src []uint16, i, b int) (int, bool) {
	j := i + 1
	if j+2 > b || !isDigit(src[j]) || !isDigit(src[j+1]) {
		return 0, false
	}
	j += 2
	switch {
	case j+3 <= b && src[j] == ':' && isDigit(src[j+1]) && isDigit(src[j+2]):
		j += 3
	case j+2 <= b && isDigit(src[j]) && isDigit(src[j+1]):
		j += 2
	}
	_, ok := instant.Offset(unitsString(src[i:j]))
	return j, ok
}

// zoneShape reports whether a word looks like an IANA name: parts joined
// by "/", each starting with a letter. Whether the database knows it is
// the resolver's question; one that it does not know stays plain text.
func zoneShape(name string) bool {
	parts := 0
	start := true
	for k := 0; k < len(name); k++ {
		c := name[k]
		if c == '/' {
			if start {
				return false
			}
			parts++
			start = true
			continue
		}
		if start && !isASCIILetter(uint16(c)) {
			return false
		}
		start = false
	}
	return parts > 0 && !start
}

// abbreviation reports whether a word looks like a zone abbreviation:
// one to four capitals and a final T ("EST", "CEST", "AEDT").
func abbreviation(name string) bool {
	if len(name) < 2 || len(name) > 5 || name[len(name)-1] != 'T' {
		return false
	}
	for k := 0; k < len(name); k++ {
		if name[k] < 'A' || name[k] > 'Z' {
			return false
		}
	}
	return true
}

// validLocal checks the numbers of "YYYY-MM-DDTHH:MM[:SS…]".
func validLocal(u []uint16) bool {
	n := func(a, b int) int {
		v, _ := strconv.Atoi(unitsString(u[a:b]))
		return v
	}
	if !instant.ValidDate(n(0, 4), n(5, 7), n(8, 10)) || n(11, 13) > 23 || n(14, 16) > 59 {
		return false
	}
	return len(u) < 19 || n(17, 19) <= 59
}

func isASCIILetter(c uint16) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func isRuneIn(r rune, set string) bool {
	for _, c := range set {
		if r == c {
			return true
		}
	}
	return false
}

// Span is a range of a text in UTF-16 code units.
type Span struct{ At, Len int }

// Opaque returns the ranges of a text that an instant given as a hint may
// not overlap (ruling R69): code blocks, inline code, and links written as
// `[text](address)` or `![alt](address)`. Bare links are entities already.
func Opaque(doc Doc) []Span {
	var out []Span
	var spans func(in []Inline)
	spans = func(in []Inline) {
		for _, s := range in {
			switch s.Kind {
			case Code, Link, NoLink, Image:
				out = append(out, Span{s.At, s.Len})
			default:
				spans(s.Children)
			}
		}
	}
	var blocks func(bs []Block)
	blocks = func(bs []Block) {
		for _, b := range bs {
			switch b.Kind {
			case CodeBlock:
				out = append(out, Span{b.At, b.Len})
			case Paragraph, Heading:
				spans(b.Inlines)
			case Quote:
				blocks(b.Blocks)
			case List:
				for _, it := range b.Items {
					spans(it.Inlines)
					blocks(it.Lists)
				}
			case Table:
				for _, c := range b.Head {
					spans(c.Inlines)
				}
				for _, r := range b.Rows {
					for _, c := range r {
						spans(c.Inlines)
					}
				}
			}
		}
	}
	blocks(doc.Blocks)
	return out
}
