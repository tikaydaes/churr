package choose

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
)

// The longest text the module can write fits a message: the most options,
// each as long as an option may be, picked or shuffled. Post checks no
// length after a draw (docs/FAIR.md section 6), so this is what keeps it
// within.
func TestTheLongestTextFits(t *testing.T) {
	opts := make([]string, model.MaxChanceOption)
	order := make([]int, len(opts))
	for i := range opts {
		opts[i] = strings.Repeat("\U0001F600", model.MaxOptionText)
		order[i] = i
	}
	if _, err := Options(strings.Join(opts, "|")); err != nil {
		t.Fatalf("the longest options are refused: %v", err)
	}
	for what, text := range map[string]string{
		"pick":    model.Pick{Options: opts, Picked: 0}.Text(),
		"shuffle": model.Shuffle{Options: opts, Order: order}.Text(),
	} {
		if n := utf8.RuneCountInString(text); n > model.MaxMessageText {
			t.Errorf("the longest %s: %d characters", what, n)
		}
	}
}
