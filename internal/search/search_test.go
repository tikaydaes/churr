package search

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/store"
)

// TestParse reads the grammar of docs/FEATURES.md section 24.1.
func TestParse(t *testing.T) {
	type T = store.SearchTerm
	for _, c := range []struct {
		q    string
		want []T
	}{
		{"", nil},
		{"   ", nil},
		{"deploy friday", []T{{Text: "deploy"}, {Text: "friday"}}},
		{`"deploy plan"`, []T{{Text: "deploy plan", Phrase: true}}},
		{`say "deploy plan" now`, []T{{Text: "say"}, {Text: "deploy plan", Phrase: true}, {Text: "now"}}},
		{"depl*", []T{{Text: "depl", Prefix: true}}},
		{"d*", []T{{Text: "d*"}}}, // a prefix needs at least two letters
		{"-staging", []T{{Text: "staging", Exclude: true}}},
		{"-", []T{{Text: "-"}}},
		{"deploy -stag*", []T{{Text: "deploy"}, {Text: "stag", Exclude: true, Prefix: true}}},
		{`an "unclosed quote`, []T{{Text: "an"}, {Text: "unclosed"}, {Text: "quote"}}},
		{`""`, nil},
		// Anything that means something to a database is just text.
		{"a OR b", []T{{Text: "a"}, {Text: "OR"}, {Text: "b"}}},
		{"NEAR(x) col:y", []T{{Text: "NEAR(x)"}, {Text: "col:y"}}},
	} {
		if got := Parse(c.q); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q) = %+v, want %+v", c.q, got, c.want)
		}
	}
}

func TestSnippet(t *testing.T) {
	long := strings.Repeat("filler words go here and there ", 10) + "then the Zeppelin Hangar opens at noon " + strings.Repeat("and more filler follows after ", 10)
	s := Snippet(long, Parse("zeppelin"))
	if !strings.Contains(s, "Zeppelin Hangar") || !strings.HasPrefix(s, "…") || !strings.HasSuffix(s, "…") || len([]rune(s)) > snippetLen+2 {
		t.Errorf("snippet around the match: %q", s)
	}
	if strings.HasPrefix(strings.TrimPrefix(s, "…"), " ") || strings.Contains(s, "  ") {
		t.Errorf("snippet is tidy: %q", s)
	}
	if got := Snippet("short  text\nwith a line break", Parse("text")); got != "short text with a line break" {
		t.Errorf("a short text is the whole snippet, on one line: %q", got)
	}
	if got := Snippet(long, Parse("-zeppelin nothere")); !strings.HasPrefix(got, "filler words") {
		t.Errorf("with nothing to locate, the start: %q", got)
	}
	if got := Snippet(long, Parse(`"hangar opens"`)); !strings.Contains(got, "Hangar opens") {
		t.Errorf("a phrase: %q", got)
	}
	if Snippet("", Parse("x")) != "" {
		t.Errorf("an empty text has no snippet")
	}
	// A match must start a word: "pen" is not found inside "opens" first.
	if got := Snippet(strings.Repeat("opens ", 60)+"pen pal", Parse("pen")); !strings.Contains(got, "pen pal") {
		t.Errorf("word start: %q", got)
	}
	Snippet(strings.Repeat("語", 500), Parse("語")) // one long word must not loop or panic
}
