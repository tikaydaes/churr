package model

import (
	"strconv"
	"strings"
)

// Reference patterns (ruling R73, docs/MESSAGE-FORMAT.md section 3.2):
// text such as `PROJ-123` or `#4521` that an admin, or a channel's
// managers, have said is a reference to something kept elsewhere. A match
// becomes a `reference` entity that opens the pattern's address with the
// matched value in it.

// The limits of a reference pattern. testdata/markdown/references.json
// holds cases at each of them.
const (
	MaxRefName       = 64  // characters of a pattern's name
	MaxRefPrefix     = 16  // characters of a prefix
	MaxRefExpression = 64  // characters of an expression
	MaxRefValue      = 64  // characters a value may run to
	MaxRefRepeat     = 32  // the most a {n,m} may ask for
	MaxRefOpen       = 3   // parts of an expression whose length may vary
	MaxRefURL        = 500 // characters of an address template
	MaxRefPatterns   = 50  // patterns of the server, and of one channel
)

// RefDigits is the expression of a pattern that gives none: a number.
const RefDigits = "[0-9]{1,12}"

// RefValueMark is where the matched value goes in an address template.
const RefValueMark = "{value}"

// ReferencePattern is one pattern (schema ReferencePattern). A pattern
// with no ConversationID is the server's and applies everywhere; one with
// applies in that channel, ahead of the server's.
//
// A reference is the prefix followed at once by a value. The value is what
// Expression matches, or a number when there is none.
type ReferencePattern struct {
	ID             string         `json:"id"`
	ConversationID ConversationID `json:"conversation_id,omitempty"`
	Name           string         `json:"name"`
	Prefix         string         `json:"prefix"`
	Expression     string         `json:"expression,omitempty"`
	URLTemplate    string         `json:"url_template"`
	CreatedByID    AccountID      `json:"created_by_id,omitempty"`
	CreatedAt      Time           `json:"created_at"`
}

// ValueExpression is the expression a value must match.
func (p ReferencePattern) ValueExpression() string {
	if p.Expression == "" {
		return RefDigits
	}
	return p.Expression
}

// URL is the address a value resolves to. A value holds only letters,
// digits and `_ . -`, so it goes into the address as it is.
func (p ReferencePattern) URL(value string) string {
	return strings.ReplaceAll(p.URLTemplate, RefValueMark, value)
}

func isRefWord(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// ValidRefPrefix reports whether s may be a pattern's prefix: 1 to 16 of
// letters, digits and `# _ . / : -`, not ending in a digit (the value
// would run into it), and not `/` or `:` alone. `PROJ-`, `#`, `RFC`,
// `gh:` are prefixes.
func ValidRefPrefix(s string) bool {
	if s == "" || len(s) > MaxRefPrefix {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !isRefWord(c) && !strings.ContainsRune("#_./:-", rune(c)) {
			return false
		}
	}
	if last := s[len(s)-1]; last >= '0' && last <= '9' {
		return false
	}
	// A prefix that is all punctuation must start with `#`: anything else
	// would turn ordinary punctuation before a number into a reference.
	for i := 0; i < len(s); i++ {
		if isRefWord(s[i]) {
			return true
		}
	}
	return s[0] == '#'
}

// CheckRefExpression checks that s is an expression in the restricted
// form, and returns why not in words for the person who typed it.
//
// The form is a small part of regular expressions, chosen so that the
// server (RE2), the web chat and the apps read one the same way and none
// can be made slow by it:
//
//	expression = 1*( atom [ repeat ] )
//	atom       = letter | digit | "_" | "-" | "\." | "\-" | "\d" | "\w" | class
//	class      = "[" 1*( member | member "-" member ) "]"
//	member     = letter | digit | "_" | "." | "\-" | "\."
//	repeat     = "?" | "+" | "*" | "{" n "}" | "{" n "," m "}"      m ≤ 32
//
// No groups, no alternatives, no `.`, no negated class, no anchors: a value
// is therefore made of letters, digits and `_ . -` only. At most three
// parts may vary in length, and the whole must not be able to match
// nothing.
func CheckRefExpression(s string) (ok bool, why string) {
	if s == "" {
		return true, ""
	}
	if len(s) > MaxRefExpression {
		return false, "An expression is at most " + strconv.Itoa(MaxRefExpression) + " characters."
	}
	const form = "An expression is made of letters, digits, _ and -, classes such as [A-Z0-9], \\d and \\w, each optionally repeated with ?, +, * or {n,m}. It has no groups, no | and no dot."
	i, open, least := 0, 0, 0
	for i < len(s) {
		// An atom.
		switch c := s[i]; {
		case isRefWord(c) || c == '_' || c == '-':
			i++
		case c == '\\':
			if i+1 >= len(s) || !strings.ContainsRune(".-dw", rune(s[i+1])) {
				return false, form
			}
			i += 2
		case c == '[':
			j := i + 1
			members := 0
			for j < len(s) && s[j] != ']' {
				m := s[j]
				switch {
				case isRefWord(m) || m == '_' || m == '.':
					j++
				case m == '\\' && j+1 < len(s) && (s[j+1] == '-' || s[j+1] == '.'):
					j += 2
				case m == '-' && members > 0 && j+1 < len(s) && isRefWord(s[j+1]) && isRefWord(s[j-1]) && s[j-1] <= s[j+1] && (j < 2 || s[j-2] != '-'):
					// A range between two letters or digits, low to high.
					j += 2
				default:
					return false, form
				}
				members++
			}
			if j >= len(s) || members == 0 {
				return false, form
			}
			i = j + 1
		default:
			return false, form
		}
		// Its repeat.
		lo, hi := 1, 1
		if i < len(s) {
			switch s[i] {
			case '?':
				lo, hi, i = 0, 1, i+1
			case '+':
				lo, hi, i = 1, MaxRefValue, i+1
			case '*':
				lo, hi, i = 0, MaxRefValue, i+1
			case '{':
				end := strings.IndexByte(s[i:], '}')
				if end < 0 {
					return false, form
				}
				a, b, both := strings.Cut(s[i+1:i+end], ",")
				n, err := strconv.Atoi(a)
				if err != nil || a != strconv.Itoa(n) {
					return false, form
				}
				m := n
				if both {
					if m, err = strconv.Atoi(b); err != nil || b != strconv.Itoa(m) {
						return false, form
					}
				}
				if n < 0 || m < n || m < 1 || m > MaxRefRepeat {
					return false, "A repeat is written {n} or {n,m}, with m from 1 to " + strconv.Itoa(MaxRefRepeat) + "."
				}
				lo, hi, i = n, m, i+end+1
			}
		}
		if i < len(s) && strings.ContainsRune("?+*{", rune(s[i])) {
			return false, form // `a+?`, `a**`: one repeat for each part
		}
		if lo != hi {
			open++
		}
		least += lo
	}
	if open > MaxRefOpen {
		return false, "At most " + strconv.Itoa(MaxRefOpen) + " parts of an expression may vary in length."
	}
	if least < 1 {
		return false, "An expression must match at least one character."
	}
	return true, ""
}

// ValidRefURL reports whether s may be an address template: an http or
// https address of at most 500 characters, with no white space or control
// characters, that says where the value goes.
func ValidRefURL(s string) bool {
	if len(s) > MaxRefURL || !strings.Contains(s, RefValueMark) {
		return false
	}
	rest, ok := strings.CutPrefix(s, "https://")
	if !ok {
		if rest, ok = strings.CutPrefix(s, "http://"); !ok {
			return false
		}
	}
	// The host is the template's own: a value may not choose where the
	// address goes.
	host, _, _ := strings.Cut(rest, "/")
	if host == "" || strings.ContainsAny(host, "{}?#@") {
		return false
	}
	for _, r := range s {
		if r <= 0x20 || r == 0x7F || r == '"' || r == '<' || r == '>' || r == '`' || r == '\\' {
			return false
		}
	}
	return true
}

// ObjectHint is what the author chose in the composer for a range of the
// text (ruling R73, docs/MESSAGE-FORMAT.md section 3.2): Kind is `plain`
// for a range in which nothing is to be recognised, or the entity type the
// range is to be read as where it could be more than one. Start and End
// count UTF-16 code units. A hint that does not fit the text is ignored.
type ObjectHint struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Kind  string `json:"kind"`
}

// ObjectPlain is the kind of a hint that keeps a range as plain text.
const ObjectPlain = "plain"

// MaxObjectHints bounds the hints one message may carry; the rest are
// not read.
const MaxObjectHints = 50
