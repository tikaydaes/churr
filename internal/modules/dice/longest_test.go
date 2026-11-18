package dice

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
)

// The longest text the module can write fits a message. Every expression the
// grammar takes is at most MaxDiceText characters and MaxDice dice; this
// tries the shapes that write the most (the most dice of the most sides, in
// the most rows and terms, with the largest plain numbers), every die at its
// highest, which has the most digits. Post checks no length after a draw
// (docs/FAIR.md section 6), so this is what keeps it within.
func TestTheLongestTextFits(t *testing.T) {
	var exprs []string
	for rows := 1; rows <= 8; rows++ {
		for terms := 1; terms <= 10; terms++ {
			for _, n := range []int{1, 2, 5, 10, 12, 20, 25, 50, 100} {
				term := strconv.Itoa(n) + "d1000"
				row := strings.TrimSuffix(strings.Repeat(term+"+", terms), "+")
				exprs = append(exprs, strings.TrimSuffix(strings.Repeat(row+",", rows), ","))
				exprs = append(exprs, strings.TrimSuffix(strings.Repeat(row+"+9999,", rows), ","))
			}
		}
	}
	longest, tried := 0, 0
	for _, e := range exprs {
		rows, written, err := ParseRows(e)
		if err != nil {
			continue
		}
		tried++
		r := model.DiceRoll{Dice: written, Rows: rows}
		for i := range r.Rows {
			total := 0
			for j := range r.Rows[i].Terms {
				tm := &r.Rows[i].Terms[j]
				if tm.Count == 0 {
					total += tm.Sign * tm.Value
					continue
				}
				tm.Rolls = make([]int, tm.Count)
				for k := range tm.Rolls {
					tm.Rolls[k] = tm.Sides
				}
				tm.Value = tm.Count * tm.Sides
				total += tm.Sign * tm.Value
			}
			r.Rows[i].Total = total
		}
		if n := utf8.RuneCountInString(Text(r)); n > longest {
			longest = n
		}
	}
	if tried < 20 {
		t.Fatalf("only %d of the shapes were taken by the grammar", tried)
	}
	if longest > model.MaxMessageText {
		t.Fatalf("a roll's text can be %d characters", longest)
	}
	t.Logf("the longest roll's text: %d characters, of %d shapes", longest, tried)
}
