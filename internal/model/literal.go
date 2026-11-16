package model

import (
	"strings"
	"unicode"
)

// LiteralSlash is the literal slash of ruling R74: text that starts with
// whitespace and then "/" is never a command. It is sent as a message that
// starts with "/", the whitespace dropped. ok is false for any other text,
// which is read as usual: a command when it starts with "/", a message
// otherwise. Text in a code span or block starts with a backtick, so it is
// never a command either. Every client holds the same rule
// (testdata/commands/parse.json, literal_cases).
func LiteralSlash(text string) (literal string, ok bool) {
	rest := strings.TrimLeftFunc(text, unicode.IsSpace)
	if rest == text || !strings.HasPrefix(rest, "/") {
		return "", false
	}
	return rest, true
}
