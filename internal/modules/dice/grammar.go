package dice

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/fair"
	"github.com/tikaydaes/churr/internal/model"
)

// The grammar of dice (docs/FEATURES.md section 12.4), which lives here and
// nowhere else: /roll, /dice and inline dice are read by this module alone.
// testdata/commands/dice.json holds its cases. The shape of a roll, and the
// check any server makes of one that came from somewhere else, are the
// catalogue's kind dice_roll (model.DiceRoll).

// The limits of the grammar; the kind's own (rows, terms, dice, sides) are
// the model's.
const (
	MaxDiceText   = 100   // characters of a dice expression
	MaxDiceNumber = 10000 // a plain number in a roll
)

func diceErr(msg string) error { return model.Invalid("text", msg) }

// diceSyntax is said when an expression cannot be read.
const diceSyntax = "Write dice like 2d6+3, d20, 4d6kh3, 2d20kl1, adv or dis."

// ParseRows reads a roll of one or more rows separated by commas
// (FEATURES.md section 12.4), each an expression Parse reads; spaces
// may stand around the commas. Nothing at all is 1d20; an empty row
// between commas is refused. The limits on characters and dice are for
// the whole roll, and a roll has at most model.MaxDiceRows rows. It returns the
// rows with nothing rolled and the roll written out in full, rows
// separated by ", ".
func ParseRows(expr string) ([]model.DiceRow, string, error) {
	if utf8.RuneCountInString(expr) > MaxDiceText {
		return nil, "", diceErr(fmt.Sprintf("A roll is at most %d characters.", MaxDiceText))
	}
	parts := strings.Split(expr, ",")
	if len(parts) > model.MaxDiceRows {
		return nil, "", diceErr(fmt.Sprintf("A roll has at most %d rows, separated by commas.", model.MaxDiceRows))
	}
	rows := make([]model.DiceRow, 0, len(parts))
	written := make([]string, 0, len(parts))
	dice := 0
	for _, p := range parts {
		if len(parts) > 1 && strings.TrimSpace(p) == "" {
			return nil, "", diceErr("A row between commas cannot be empty.")
		}
		terms, text, err := Parse(p)
		if err != nil {
			return nil, "", err
		}
		for _, t := range terms {
			dice += t.Count
		}
		if dice > model.MaxDice {
			return nil, "", diceErr(fmt.Sprintf("A roll has at most %d dice.", model.MaxDice))
		}
		rows = append(rows, model.DiceRow{Dice: text, Terms: terms})
		written = append(written, text)
	}
	return rows, strings.Join(written, ", "), nil
}

// D6 turns what follows /dice into a roll (FEATURES.md section 12.4):
// in each row, a first term that is a bare number is that many d6
// (`4` is `4d6`, `4+3` is `4d6+3`, `4k3` is `4d6k3`); everything else is
// as written (`2d8` stays `2d8`, a later number is added). Nothing at all
// is 1d6. Rows are separated by ", ".
func D6(expr string) string {
	if strings.TrimSpace(expr) == "" {
		return "1d6"
	}
	parts := strings.Split(expr, ",")
	for i, p := range parts {
		q := strings.TrimSpace(p)
		j := 0
		for j < len(q) && q[j] >= '0' && q[j] <= '9' {
			j++
		}
		if j > 0 && (j == len(q) || (q[j] != 'd' && q[j] != 'D')) {
			q = q[:j] + "d6" + q[j:]
		}
		parts[i] = q
	}
	return strings.Join(parts, ", ")
}

// Parse reads a dice expression (FEATURES.md section 12.4): terms
// joined by + and -, each NdM (N defaults to 1; d% is d100), NdM followed
// by kh, kl, dh or dl and an optional count (k alone is kh; the count
// defaults to 1), a plain number, or adv and dis for 2d20kh1 and 2d20kl1.
// Case and spaces do not matter. Nothing at all is 1d20. It returns the
// terms with nothing rolled and the expression written out in full.
func Parse(expr string) ([]model.DiceTerm, string, error) {
	if utf8.RuneCountInString(expr) > MaxDiceText {
		return nil, "", diceErr(fmt.Sprintf("A roll is at most %d characters.", MaxDiceText))
	}
	// Spaces may stand beside + and -, and nowhere else: "2d6 3" is not
	// read as 2d63.
	fields := strings.Fields(strings.ToLower(expr))
	for k := 1; k < len(fields); k++ {
		a, b := fields[k-1], fields[k]
		if !strings.ContainsAny(a[len(a)-1:], "+-") && !strings.ContainsAny(b[:1], "+-") {
			return nil, "", diceErr(diceSyntax)
		}
	}
	s := strings.Join(fields, "")
	if s == "" {
		s = "1d20"
	}
	var terms []model.DiceTerm
	dice := 0
	i := 0
	for i < len(s) {
		sign := 1
		switch s[i] {
		case '+':
			i++
		case '-':
			sign = -1
			i++
		default:
			if len(terms) > 0 {
				return nil, "", diceErr(diceSyntax)
			}
		}
		if i >= len(s) {
			return nil, "", diceErr(diceSyntax)
		}
		t, n, err := diceTerm(s[i:])
		if err != nil {
			return nil, "", err
		}
		i += n
		t.Sign = sign
		terms = append(terms, t)
		if len(terms) > model.MaxDiceTerms {
			return nil, "", diceErr(fmt.Sprintf("A roll has at most %d terms.", model.MaxDiceTerms))
		}
		dice += t.Count
		if dice > model.MaxDice {
			return nil, "", diceErr(fmt.Sprintf("A roll has at most %d dice.", model.MaxDice))
		}
	}
	return terms, Written(terms), nil
}

// diceTerm reads one term from the start of s and says how many bytes it
// took.
func diceTerm(s string) (model.DiceTerm, int, error) {
	for _, w := range []struct {
		word string
		keep string
	}{{"adv", "kh"}, {"dis", "kl"}} {
		if strings.HasPrefix(s, w.word) {
			return model.DiceTerm{Count: 2, Sides: 20, Keep: w.keep, KeepN: 1}, len(w.word), nil
		}
	}
	i := 0
	num := func() (int, bool, error) {
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return 0, false, nil
		}
		if i-start > 6 {
			return 0, true, diceErr(fmt.Sprintf("A number in a roll is at most %d.", MaxDiceNumber))
		}
		n, _ := strconv.Atoi(s[start:i])
		return n, true, nil
	}
	count, hasCount, err := num()
	if err != nil {
		return model.DiceTerm{}, 0, err
	}
	if i >= len(s) || s[i] != 'd' {
		// A plain number.
		if !hasCount {
			return model.DiceTerm{}, 0, diceErr(diceSyntax)
		}
		if count > MaxDiceNumber {
			return model.DiceTerm{}, 0, diceErr(fmt.Sprintf("A number in a roll is at most %d.", MaxDiceNumber))
		}
		return model.DiceTerm{Value: count}, i, nil
	}
	i++ // the d
	if !hasCount {
		count = 1
	}
	if count < 1 {
		return model.DiceTerm{}, 0, diceErr("Roll at least one die.")
	}
	if count > model.MaxDice {
		return model.DiceTerm{}, 0, diceErr(fmt.Sprintf("A roll has at most %d dice.", model.MaxDice))
	}
	var sides int
	if i < len(s) && s[i] == '%' {
		sides = 100
		i++
	} else {
		var has bool
		if sides, has, err = num(); err != nil || !has {
			if err == nil {
				err = diceErr(diceSyntax)
			}
			if has {
				err = diceErr(fmt.Sprintf("A die has 2 to %d sides.", model.MaxDieSides))
			}
			return model.DiceTerm{}, 0, err
		}
	}
	if sides < 2 || sides > model.MaxDieSides {
		return model.DiceTerm{}, 0, diceErr(fmt.Sprintf("A die has 2 to %d sides.", model.MaxDieSides))
	}
	t := model.DiceTerm{Count: count, Sides: sides}
	if i < len(s) && s[i] == 'k' || i+1 < len(s) && s[i] == 'd' && (s[i+1] == 'h' || s[i+1] == 'l') {
		mode := s[i : i+1]
		i++
		switch {
		case i < len(s) && s[i] == 'h':
			mode += "h"
			i++
		case i < len(s) && s[i] == 'l':
			mode += "l"
			i++
		default:
			mode += "h" // k alone keeps the highest
		}
		n, has, err := num()
		if err != nil {
			return model.DiceTerm{}, 0, err
		}
		if !has {
			n = 1
		}
		t.Keep, t.KeepN = mode, n
		if mode[0] == 'k' && (n < 1 || n > count) {
			return model.DiceTerm{}, 0, diceErr(fmt.Sprintf("Keep between 1 and %d of %dd%d.", count, count, sides))
		}
		if mode[0] == 'd' && (n < 1 || n >= count) {
			if count == 1 {
				return model.DiceTerm{}, 0, diceErr("Dropping needs at least two dice.")
			}
			return model.DiceTerm{}, 0, diceErr(fmt.Sprintf("Drop between 1 and %d of %dd%d.", count-1, count, sides))
		}
	}
	return t, i, nil
}

// Written writes terms out in full: 2d6+1d4kh1-3.
func Written(terms []model.DiceTerm) string {
	var b strings.Builder
	for i, t := range terms {
		if t.Sign < 0 {
			b.WriteByte('-')
		} else if i > 0 {
			b.WriteByte('+')
		}
		if t.Count == 0 {
			b.WriteString(strconv.Itoa(t.Value))
			continue
		}
		fmt.Fprintf(&b, "%dd%d", t.Count, t.Sides)
		if t.Keep != "" {
			fmt.Fprintf(&b, "%s%d", t.Keep, t.KeepN)
		}
	}
	return b.String()
}

// RollTerms rolls the dice of terms with die, which gives a whole number from
// 1 to sides, and returns the terms with their rolls and the total. Which
// dice count, and what a term comes to, are derive 1's (fair.Dropped and
// fair.Score), the rule every verifier holds a roll to. A roll's dice come
// from the message's draw (roll); this takes them from anywhere, for the
// cases of testdata/commands/dice.json.
func RollTerms(terms []model.DiceTerm, die func(sides int) int) ([]model.DiceTerm, int) {
	out := make([]model.DiceTerm, len(terms))
	total := 0
	for i, t := range terms {
		if t.Count > 0 {
			t.Rolls = make([]int, t.Count)
			for j := range t.Rolls {
				t.Rolls[j] = die(t.Sides)
			}
			t.Dropped = fair.Dropped(t.Keep, t.KeepN, t.Rolls)
			t.Value = fair.Score(fair.DiceTerm{Rolls: t.Rolls, Dropped: t.Dropped})
		}
		total += t.Sign * t.Value
		out[i] = t
	}
	return out, total
}

// Text is a roll in words, the text of the message that carries it, which
// reads correctly with no client support and is what search reads:
// "Rolled 2d6+3: [4, 1] + 3 = 8". Dice that do not count are in
// parentheses; rows are separated by "; ".
func Text(r model.DiceRoll) string {
	var b strings.Builder
	b.WriteString("Rolled " + r.Dice + ":")
	for k, row := range r.Rows {
		if k > 0 {
			b.WriteString(";")
		}
		writeDiceRow(&b, row)
	}
	return b.String()
}

// writeDiceRow writes one row of a roll's text: " [4, 1] + 3 = 8".
func writeDiceRow(b *strings.Builder, row model.DiceRow) {
	for i, t := range row.Terms {
		switch {
		case t.Sign < 0:
			b.WriteString(" - ")
		case i > 0:
			b.WriteString(" + ")
		default:
			b.WriteString(" ")
		}
		if t.Count == 0 {
			b.WriteString(strconv.Itoa(t.Value))
			continue
		}
		drop := map[int]bool{}
		for _, j := range t.Dropped {
			drop[j] = true
		}
		parts := make([]string, len(t.Rolls))
		for j, r := range t.Rolls {
			parts[j] = strconv.Itoa(r)
			if drop[j] {
				parts[j] = "(" + parts[j] + ")"
			}
		}
		b.WriteString("[" + strings.Join(parts, ", ") + "]")
	}
	fmt.Fprintf(b, " = %d", row.Total)
}
