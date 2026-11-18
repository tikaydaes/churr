package fair

// Dice under derive 1 (docs/FAIR.md section 5): rows of terms, as the dice
// module reads them and the catalogue's dice_roll carries them. The JSON
// names are dice_roll's, so a roll's rows are handed to Roll as they are
// stored, and what comes back is compared with them.

// The limits of a dice primitive; the dice module's grammar is narrower.
const (
	MaxDiceRows  = 100
	MaxDiceTerms = 100
	MaxDiceCount = 1000    // dice in a term, before explosions
	MaxDiceTotal = 10000   // dice in all, before explosions
	MaxDieSides  = 1000000 // sides of a die; the least is 2
	// MaxExplode is how many extra dice a term's explosions add at most;
	// a die that comes up its highest after that does not explode.
	MaxExplode = 100
)

// DiceRow is a row of dice: its terms and, once rolled, its total, the sum
// of each term's value with its sign. Dice is the row written out, carried
// for the reader and not read here.
type DiceRow struct {
	Dice  string     `json:"dice,omitempty"`
	Terms []DiceTerm `json:"terms"`
	Total int        `json:"total"`
}

// DiceTerm is one term: Count dice of Sides sides, or, with Count 0, the
// plain number Value. Keep is kh, kl, dh or dl with KeepN the number kept or
// dropped. Explode rolls another die each time one comes up Sides. Success,
// when above 0, makes the term's value the number of counted dice that came
// up Success or more, in place of their sum. Rolls, Dropped and Value are
// what the roll gives.
type DiceTerm struct {
	Sign    int    `json:"sign"`
	Count   int    `json:"count,omitempty"`
	Sides   int    `json:"sides,omitempty"`
	Keep    string `json:"keep,omitempty"`
	KeepN   int    `json:"keep_n,omitempty"`
	Explode bool   `json:"explode,omitempty"`
	Success int    `json:"success,omitempty"`
	Rolls   []int  `json:"rolls,omitempty"`
	Dropped []int  `json:"dropped,omitempty"`
	Value   int    `json:"value"`
}

func checkDice(rows []DiceRow) error {
	if len(rows) < 1 || len(rows) > MaxDiceRows {
		return bad("dice take 1 to %d rows", MaxDiceRows)
	}
	total := 0
	for _, r := range rows {
		if len(r.Terms) < 1 || len(r.Terms) > MaxDiceTerms {
			return bad("a row takes 1 to %d terms", MaxDiceTerms)
		}
		for _, t := range r.Terms {
			if t.Sign != 1 && t.Sign != -1 {
				return bad("a term's sign is 1 or -1")
			}
			if t.Count == 0 {
				if t.Keep != "" || t.Explode || t.Success != 0 || t.Sides != 0 {
					return bad("a plain number has no dice")
				}
				continue
			}
			if t.Count < 1 || t.Count > MaxDiceCount || t.Sides < 2 || t.Sides > MaxDieSides {
				return bad("a term rolls 1 to %d dice of 2 to %d sides", MaxDiceCount, MaxDieSides)
			}
			total += t.Count
			if total > MaxDiceTotal {
				return bad("dice roll at most %d dice", MaxDiceTotal)
			}
			switch t.Keep {
			case "":
				if t.KeepN != 0 {
					return bad("keep_n goes with keep")
				}
			case "kh", "kl":
				if t.KeepN < 1 || t.KeepN > t.Count {
					return bad("keep 1 to count")
				}
			case "dh", "dl":
				if t.KeepN < 1 || t.KeepN >= t.Count {
					return bad("drop 1 to count-1")
				}
			default:
				return bad("keep is kh, kl, dh or dl")
			}
			if t.Success < 0 || t.Success > t.Sides {
				return bad("success is 1 to sides, or 0 for none")
			}
		}
	}
	return nil
}

// rollRows rolls every row in order, every term of a row in order, and every
// die of a term in order: a die is 1 + Below(sides). An exploding term rolls
// another die straight after each die that came up sides, until it has added
// MaxExplode; the extra dice stand in Rolls after the die that exploded.
// Plain numbers take nothing.
func rollRows(s *Stream, rows []DiceRow) []DiceRow {
	out := make([]DiceRow, len(rows))
	for i, r := range rows {
		row := DiceRow{Dice: r.Dice, Terms: make([]DiceTerm, len(r.Terms))}
		for j, t := range r.Terms {
			t.Rolls, t.Dropped = nil, nil
			if t.Count > 0 {
				extra := 0
				for d := 0; d < t.Count; d++ {
					v := 1 + int(s.Below(uint64(t.Sides)))
					t.Rolls = append(t.Rolls, v)
					for t.Explode && v == t.Sides && extra < MaxExplode {
						extra++
						v = 1 + int(s.Below(uint64(t.Sides)))
						t.Rolls = append(t.Rolls, v)
					}
				}
				t.Dropped = Dropped(t.Keep, t.KeepN, t.Rolls)
				t.Value = Score(t)
			}
			row.Terms[j] = t
			row.Total += t.Sign * t.Value
		}
		out[i] = row
	}
	return out
}

// Score is a rolled term's value: the sum of its rolls that are not dropped,
// or, with Success, how many of them came up Success or more.
func Score(t DiceTerm) int {
	skip := map[int]bool{}
	for _, j := range t.Dropped {
		skip[j] = true
	}
	v := 0
	for j, r := range t.Rolls {
		switch {
		case skip[j]:
		case t.Success > 0:
			if r >= t.Success {
				v++
			}
		default:
			v += r
		}
	}
	return v
}

// Dropped says which rolls do not count, by position, in increasing order. kh
// N and dl N drop the lowest (all but N, or N of them); kl N and dh N drop the
// highest. Of equal rolls the later one is dropped first.
func Dropped(keep string, n int, rolls []int) []int {
	if keep == "" {
		return nil
	}
	drop := n
	if keep[0] == 'k' {
		drop = len(rolls) - n
	}
	if drop <= 0 {
		return nil
	}
	lowest := keep == "dl" || keep == "kh"
	pos := make([]int, len(rolls))
	for i := range pos {
		pos[i] = i
	}
	before := func(a, b int) bool {
		ra, rb := rolls[a], rolls[b]
		if ra != rb {
			if lowest {
				return ra < rb
			}
			return ra > rb
		}
		return a > b
	}
	for i := 1; i < len(pos); i++ {
		for j := i; j > 0 && before(pos[j], pos[j-1]); j-- {
			pos[j], pos[j-1] = pos[j-1], pos[j]
		}
	}
	out := append([]int(nil), pos[:drop]...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Roll rolls rows of dice from a stream: the dice primitive, for a module
// that holds its stream (Host.Random).
func Roll(s *Stream, rows []DiceRow) ([]DiceRow, error) {
	r, err := Draw(s, Primitive{Op: OpDice, Rows: rows})
	return r.Rows, err
}
