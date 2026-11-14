package markdown

import (
	"unicode"
	"unicode/utf16"
)

// Positions everywhere in this package count UTF-16 code units, because that
// is how the JavaScript and Dart clients index strings and what entities are
// stored in. The parser therefore works on the text as UTF-16, so that an
// index into its buffer is already the position to report.

// toUnits turns a Go string into UTF-16 code units. A byte that is not valid
// UTF-8 becomes U+FFFD, one unit, so no input can make the parser fail.
func toUnits(s string) []uint16 { return utf16.Encode([]rune(s)) }

// unitsString turns UTF-16 code units back into a Go string.
func unitsString(u []uint16) string { return string(utf16.Decode(u)) }

// utf16Len is the length of s in UTF-16 code units: what `s.length` is in
// JavaScript and Dart.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func isHigh(c uint16) bool { return c >= 0xD800 && c <= 0xDBFF }
func isLow(c uint16) bool  { return c >= 0xDC00 && c <= 0xDFFF }

// runeBefore returns the character that ends just before position i, looking
// no further back than lo. ok is false at the start.
func runeBefore(src []uint16, i, lo int) (r rune, ok bool) {
	if i <= lo {
		return 0, false
	}
	c := src[i-1]
	if isLow(c) && i-2 >= lo && isHigh(src[i-2]) {
		return utf16.DecodeRune(rune(src[i-2]), rune(c)), true
	}
	return rune(c), true
}

// runeAt returns the character that starts at position i, looking no further
// than hi. ok is false at the end.
func runeAt(src []uint16, i, hi int) (r rune, ok bool) {
	if i >= hi {
		return 0, false
	}
	c := src[i]
	if isHigh(c) && i+1 < hi && isLow(src[i+1]) {
		return utf16.DecodeRune(rune(c), rune(src[i+1])), true
	}
	return rune(c), true
}

// isWhite reports whether r is white space. The list is spelled out (it is the
// Unicode White_Space property) so that the three parsers agree exactly and do
// not depend on what each language's own "is space" happens to include.
func isWhite(r rune) bool {
	switch {
	case r >= 0x0009 && r <= 0x000D, r == 0x0020, r == 0x0085, r == 0x00A0,
		r == 0x1680, r >= 0x2000 && r <= 0x200A, r == 0x2028, r == 0x2029,
		r == 0x202F, r == 0x205F, r == 0x3000:
		return true
	}
	return false
}

// isWord reports whether r is part of a word: a letter or a number in any
// script. This is what "the middle of a word" means for `_`, mentions,
// channel names, emoji codes and bare links.
//
// Combining marks are deliberately not counted. The marks that matter in
// practice are the ones that end an emoji (a variation selector, a keycap),
// and `⚠️@sam` has to mention sam just as `🎉@sam` does.
func isWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

// isPunct reports whether c is one of the 32 ASCII punctuation characters, the
// ones a backslash can escape.
func isPunct(c uint16) bool {
	return c >= '!' && c <= '/' || c >= ':' && c <= '@' || c >= '[' && c <= '`' || c >= '{' && c <= '~'
}

func isLowerAlnum(c uint16) bool { return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' }
func isDigit(c uint16) bool      { return c >= '0' && c <= '9' }
func isBlank(c uint16) bool      { return c == ' ' || c == '\t' }

// hasPrefixFold reports whether the units start with the ASCII string prefix,
// without regard to case. prefix must be lowercase.
func hasPrefixFold(u []uint16, prefix string) bool {
	if len(u) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		c := u[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != uint16(prefix[i]) {
			return false
		}
	}
	return true
}

// allowedURL reports whether a link may point at u. Only these four schemes
// are links; everything else (javascript:, data:, relative paths) is not, so a
// client never has to decide whether an address is safe to open.
func allowedURL(u []uint16) bool {
	for _, scheme := range [...]string{"https://", "http://", "mailto:", "tel:"} {
		if hasPrefixFold(u, scheme) && len(u) > len(scheme) {
			return true
		}
	}
	return false
}

// languages maps every accepted name of a code block language, in lowercase,
// to the name clients highlight with (docs/MESSAGE-FORMAT.md section 2.3).
var languages = func() map[string]string {
	list := map[string][]string{
		"bash": {"sh", "shell", "zsh"}, "c": nil, "cpp": {"c++", "cc"},
		"csharp": {"cs", "c#"}, "css": nil, "dart": nil, "diff": {"patch"},
		"dockerfile": {"docker"}, "elixir": nil, "go": {"golang"},
		"graphql": nil, "haskell": {"hs"}, "html": nil, "ini": nil,
		"java": nil, "javascript": {"js"}, "json": nil, "kotlin": {"kt"},
		"lua": nil, "makefile": {"make"}, "markdown": {"md"}, "nginx": nil,
		"objectivec": {"objc"}, "perl": nil, "php": nil,
		"plaintext": {"text", "txt"}, "powershell": {"ps1"},
		"protobuf": {"proto"}, "python": {"py"}, "r": nil, "ruby": {"rb"},
		"rust": {"rs"}, "scala": nil, "scss": nil, "sql": nil, "swift": nil,
		"toml": nil, "typescript": {"ts"}, "xml": nil, "yaml": {"yml"},
		"zig": nil,
	}
	m := make(map[string]string)
	for name, aliases := range list {
		m[name] = name
		for _, a := range aliases {
			m[a] = name
		}
	}
	return m
}()

// language returns the highlighting name for the word after an opening fence,
// or "" if the word is not a known language.
func language(word []uint16) string {
	b := make([]byte, 0, len(word))
	for _, c := range word {
		if c > 0x7F {
			return ""
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b = append(b, byte(c))
	}
	return languages[string(b)]
}
