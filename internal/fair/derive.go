package fair

import (
	"errors"
	"fmt"
)

// The primitives of derive 1 (docs/FAIR.md section 4): the closed list of
// outcomes a caller may ask a draw for. Each takes words from the object's
// stream in the order written here and nowhere else, so a verifier who has
// the draw and the primitive re-derives the result.
const (
	OpInt     = "int"     // a whole number from min to max
	OpInts    = "ints"    // count of them, one after another
	OpPick    = "pick"    // k of n positions, with or without replacement, with or without weights
	OpShuffle = "shuffle" // the n positions in a random order (Fisher–Yates)
	OpDeal    = "deal"    // hands dealt from a deck of n without replacement
	OpYesNo   = "yesno"   // yes with probability num/den
	OpDice    = "dice"    // rows of dice terms (dice.go)
)

// The limits of a primitive. They are not part of how an outcome is derived;
// they bound the work a verifier is asked to do.
const (
	MaxN     = 10000         // positions of a pick, shuffle or deal
	MaxCount = 1000          // results of ints, or of a pick with replacement
	MinInt   = -(1 << 31)    // the least min of int and ints
	MaxInt   = (1 << 31) - 1 // the greatest max
)

// Primitive is one outcome asked of a draw, as it is recorded beside its
// result. Fields a primitive does not use are left out.
type Primitive struct {
	Op string `json:"op"`
	// Min and Max bound int and ints, both included.
	Min int64 `json:"min,omitempty"`
	Max int64 `json:"max,omitempty"`
	// Count is how many ints.
	Count int `json:"count,omitempty"`
	// N is the positions of a pick, shuffle or deal; K how many a pick
	// takes. Replace lets a pick take a position again. Weights, when
	// given, are one whole number of at least 0 per position.
	N       int      `json:"n,omitempty"`
	K       int      `json:"k,omitempty"`
	Replace bool     `json:"replace,omitempty"`
	Weights []uint64 `json:"weights,omitempty"`
	// Hands are the sizes of the hands a deal deals, in order.
	Hands []int `json:"hands,omitempty"`
	// Num and Den are yesno's probability, num/den.
	Num uint64 `json:"num,omitempty"`
	Den uint64 `json:"den,omitempty"`
	// Rows are the dice a dice primitive rolls.
	Rows []DiceRow `json:"rows,omitempty"`
}

// Result is a primitive's outcome. Only the field of its op is set.
type Result struct {
	Value  *int64    `json:"value,omitempty"`  // int
	Values []int64   `json:"values,omitempty"` // ints
	Picked []int     `json:"picked,omitempty"` // pick: positions, in the order picked
	Order  []int     `json:"order,omitempty"`  // shuffle: positions, in their new order
	Dealt  [][]int   `json:"dealt,omitempty"`  // deal: each hand's positions, in the order dealt
	Yes    *bool     `json:"yes,omitempty"`    // yesno
	Rows   []DiceRow `json:"rows,omitempty"`   // dice: the rows with their rolls
}

// ErrPrimitive is a primitive that is not one derive 1 can draw.
var ErrPrimitive = errors.New("fair: not a primitive of derive 1")

func bad(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrPrimitive, fmt.Sprintf(format, a...))
}

// Check says whether a primitive is one derive 1 draws, within its limits.
func Check(p Primitive) error {
	switch p.Op {
	case OpInt, OpInts:
		if p.Min < MinInt || p.Max > MaxInt || p.Min > p.Max {
			return bad("min and max are whole numbers from %d to %d, min no greater than max", int64(MinInt), int64(MaxInt))
		}
		if p.Op == OpInts && (p.Count < 1 || p.Count > MaxCount) {
			return bad("ints takes a count from 1 to %d", MaxCount)
		}
	case OpPick:
		if p.N < 1 || p.N > MaxN {
			return bad("pick takes n from 1 to %d", MaxN)
		}
		if p.K < 1 || (!p.Replace && p.K > p.N) || (p.Replace && p.K > MaxCount) {
			return bad("pick takes k of at least 1, at most n without replacement and %d with it", MaxCount)
		}
		if p.Weights != nil {
			if len(p.Weights) != p.N {
				return bad("pick takes one weight per position")
			}
			var total uint64
			positive := 0
			for _, w := range p.Weights {
				if w > span {
					return bad("a weight is at most 2^32")
				}
				total += w
				if w > 0 {
					positive++
				}
			}
			if total < 1 || total > span {
				return bad("the weights add up to 1 to 2^32")
			}
			if !p.Replace && p.K > positive {
				return bad("pick without replacement takes at most as many as have weight")
			}
		}
	case OpShuffle:
		if p.N < 1 || p.N > MaxN {
			return bad("shuffle takes n from 1 to %d", MaxN)
		}
	case OpDeal:
		if p.N < 1 || p.N > MaxN || len(p.Hands) < 1 || len(p.Hands) > MaxN {
			return bad("deal takes n from 1 to %d and at least one hand", MaxN)
		}
		total := 0
		for _, h := range p.Hands {
			if h < 1 {
				return bad("a hand is at least one")
			}
			total += h
			if total > p.N {
				return bad("the hands take at most the deck")
			}
		}
	case OpYesNo:
		if p.Den < 1 || p.Den > span || p.Num > p.Den {
			return bad("yesno takes num/den with den from 1 to 2^32 and num no greater than den")
		}
	case OpDice:
		return checkDice(p.Rows)
	default:
		return bad("no op %q", p.Op)
	}
	return nil
}

// Draw derives a primitive's result from a stream. A primitive Check refuses
// is refused, having taken nothing.
func Draw(s *Stream, p Primitive) (Result, error) {
	if err := Check(p); err != nil {
		return Result{}, err
	}
	switch p.Op {
	case OpInt:
		v := intIn(s, p.Min, p.Max)
		return Result{Value: &v}, nil
	case OpInts:
		vs := make([]int64, p.Count)
		for j := range vs {
			vs[j] = intIn(s, p.Min, p.Max)
		}
		return Result{Values: vs}, nil
	case OpPick:
		return Result{Picked: pick(s, p)}, nil
	case OpShuffle:
		return Result{Order: shuffle(s, p.N)}, nil
	case OpDeal:
		total := 0
		for _, h := range p.Hands {
			total += h
		}
		taken := pickPlain(s, p.N, total)
		out := make([][]int, len(p.Hands))
		at := 0
		for j, h := range p.Hands {
			out[j] = taken[at : at+h : at+h]
			at += h
		}
		return Result{Dealt: out}, nil
	case OpYesNo:
		yes := s.Below(p.Den) < p.Num
		return Result{Yes: &yes}, nil
	case OpDice:
		return Result{Rows: rollRows(s, p.Rows)}, nil
	}
	return Result{}, bad("no op %q", p.Op)
}

// intIn is min + Below(max - min + 1).
func intIn(s *Stream, lo, hi int64) int64 {
	return lo + int64(s.Below(uint64(hi-lo)+1))
}

// shuffle is Fisher–Yates as Durstenfeld wrote it: the positions 0 to n-1 in
// order; then for j from n-1 down to 1, swap position j with position
// Below(j+1). The result is the list as it then stands.
func shuffle(s *Stream, n int) []int {
	a := make([]int, n)
	for j := range a {
		a[j] = j
	}
	for j := n - 1; j >= 1; j-- {
		r := int(s.Below(uint64(j) + 1))
		a[j], a[r] = a[r], a[j]
	}
	return a
}

// pickPlain takes k of n positions without replacement, by the first k steps
// of the same Fisher–Yates: for step t from 0, j = n-1-t, swap position j
// with position Below(j+1), and position j is the t-th picked. With k = n
// the last step takes Below(1), which takes no word.
func pickPlain(s *Stream, n, k int) []int {
	a := make([]int, n)
	for j := range a {
		a[j] = j
	}
	out := make([]int, k)
	for t := 0; t < k; t++ {
		j := n - 1 - t
		r := int(s.Below(uint64(j) + 1))
		a[j], a[r] = a[r], a[j]
		out[t] = a[j]
	}
	return out
}

// pick takes k positions: each with replacement is Below(n), or, weighted,
// the position whose run holds Below(total) when the positions' weights are
// laid end to end in order; without replacement a weighted pick lays out
// only the positions not yet taken, in order, and the unweighted one is
// pickPlain.
func pick(s *Stream, p Primitive) []int {
	if p.Weights == nil {
		if !p.Replace {
			return pickPlain(s, p.N, p.K)
		}
		out := make([]int, p.K)
		for t := range out {
			out[t] = int(s.Below(uint64(p.N)))
		}
		return out
	}
	w := append([]uint64(nil), p.Weights...)
	out := make([]int, p.K)
	for t := range out {
		var total uint64
		for _, x := range w {
			total += x
		}
		r := s.Below(total)
		j := 0
		for ; j < len(w); j++ {
			if r < w[j] {
				break
			}
			r -= w[j]
		}
		out[t] = j
		if !p.Replace {
			w[j] = 0
		}
	}
	return out
}
