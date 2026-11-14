package search

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/store"
)

// snippetLen is about how many characters a snippet holds.
const snippetLen = 160

// Snippet returns a short plain-text excerpt of a message's indexed text
// around the first place a search term matches, for the result list. It is
// cut from messages.search_text, the same text the index holds, so what is
// shown is what was found. With no positive term, or none that can be
// located, it is the start of the text. The excerpt is cut at word
// boundaries and marked with an ellipsis where text was left out.
func Snippet(text string, terms []store.SearchTerm) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= snippetLen {
		return text
	}
	at := matchAt(text, terms)
	// A third of the excerpt before the match, the rest after it.
	start := utf8.RuneCountInString(text[:at]) - snippetLen/3
	if start < 0 {
		start = 0
	}
	end := start + snippetLen
	if end > len(runes) {
		end = len(runes)
		start = end - snippetLen
	}
	// Do not start or stop in the middle of a word.
	for start > 0 && start < end && !unicode.IsSpace(runes[start-1]) {
		start++
	}
	for end < len(runes) && end > start && !unicode.IsSpace(runes[end]) {
		end--
	}
	out := strings.TrimSpace(string(runes[start:end]))
	if out == "" {
		// One word longer than a snippet: cut it anyway.
		out = string(runes[:snippetLen])
		start, end = 0, snippetLen
	}
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}

// matchAt finds the byte offset of the earliest match of any positive term,
// without regard to case, at the start of a word. 0 if there is none.
func matchAt(text string, terms []store.SearchTerm) int {
	low := strings.ToLower(text)
	best := -1
	for _, t := range terms {
		if t.Exclude {
			continue
		}
		words := strings.Fields(strings.ToLower(t.Text))
		if len(words) == 0 {
			continue
		}
		needle := words[0]
		if t.Phrase {
			needle = strings.Join(words, " ")
		}
		for from := 0; from < len(low); {
			i := strings.Index(low[from:], needle)
			if i < 0 {
				break
			}
			i += from
			prev, _ := utf8.DecodeLastRuneInString(low[:i])
			if i == 0 || !(unicode.IsLetter(prev) || unicode.IsDigit(prev)) {
				if best < 0 || i < best {
					best = i
				}
				break
			}
			from = i + len(needle)
		}
	}
	// Lower-casing can change a string's length in bytes; an offset that
	// no longer fits the original is not worth a wrong cut.
	if best < 0 || best > len(text) || !utf8.RuneStart(text[min(best, len(text)-1)]) {
		return 0
	}
	return best
}
