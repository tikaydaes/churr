package dice

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modhost"
)

// The shared cases of testdata/commands/dice.json: the grammar, its limits,
// which dice it drops, and the text of a roll.
type diceVectors struct {
	Parse []struct {
		Input string           `json:"input"`
		Dice  string           `json:"dice"`
		Terms []model.DiceTerm `json:"terms"`
	} `json:"parse"`
	Errors []struct {
		Input string `json:"input"`
		Error string `json:"error"`
	} `json:"errors"`
	Rolls []struct {
		Dice  string `json:"dice"`
		Rolls []int  `json:"rolls"`
		Terms []struct {
			Rolls   []int `json:"rolls"`
			Dropped []int `json:"dropped"`
			Value   int   `json:"value"`
		} `json:"terms"`
		Total int `json:"total"`
	} `json:"rolls"`
	Rows []struct {
		Input string   `json:"input"`
		Dice  string   `json:"dice"`
		Rows  []string `json:"rows"`
	} `json:"rows"`
	RowErrors []struct {
		Input string `json:"input"`
		Error string `json:"error"`
	} `json:"row_errors"`
	D6 []struct {
		Input string `json:"input"`
		Dice  string `json:"dice"`
	} `json:"d6"`
	// The fallback cases with "kind": "roll" are a roll and its text; the
	// rest are the other chance results, the model's.
	Fallback []struct {
		Chance json.RawMessage `json:"chance"`
		Text   string          `json:"text"`
	} `json:"fallback"`
}

func TestDiceVectors(t *testing.T) {
	var v diceVectors
	raw, err := os.ReadFile("../../../testdata/commands/dice.json")
	if err == nil {
		err = json.Unmarshal(raw, &v)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range v.Parse {
		terms, text, err := Parse(c.Input)
		if err != nil {
			t.Errorf("%q: %v", c.Input, err)
			continue
		}
		if text != c.Dice || !reflect.DeepEqual(terms, c.Terms) {
			t.Errorf("%q: got %q %+v, want %q %+v", c.Input, text, terms, c.Dice, c.Terms)
		}
	}
	for _, c := range v.Errors {
		_, _, err := Parse(c.Input)
		if err == nil || !strings.Contains(err.Error(), c.Error) {
			t.Errorf("%q: got %v, want an error with %q", c.Input, err, c.Error)
		}
	}
	for _, c := range v.Rolls {
		terms, _, err := Parse(c.Dice)
		if err != nil {
			t.Fatal(err)
		}
		next := 0
		rolled, total := RollTerms(terms, func(sides int) int { r := c.Rolls[next]; next++; return r })
		if total != c.Total || next != len(c.Rolls) {
			t.Errorf("%s %v: total %d, want %d", c.Dice, c.Rolls, total, c.Total)
		}
		for i, w := range c.Terms {
			g := rolled[i]
			if len(g.Rolls) != len(w.Rolls) || (len(w.Rolls) > 0 && !reflect.DeepEqual(g.Rolls, w.Rolls)) ||
				len(g.Dropped) != len(w.Dropped) || (len(w.Dropped) > 0 && !reflect.DeepEqual(g.Dropped, w.Dropped)) || g.Value != w.Value {
				t.Errorf("%s %v term %d: got %+v, want %+v", c.Dice, c.Rolls, i, g, w)
			}
		}
		if !(model.DiceRoll{Dice: c.Dice, Rows: []model.DiceRow{{Dice: c.Dice, Terms: rolled, Total: total}}}).Valid() {
			t.Errorf("%s: a rolled result is not valid", c.Dice)
		}
	}
	for _, c := range v.Rows {
		rows, text, err := ParseRows(c.Input)
		if err != nil {
			t.Errorf("rows %q: %v", c.Input, err)
			continue
		}
		var got []string
		for _, r := range rows {
			got = append(got, r.Dice)
		}
		if text != c.Dice || !reflect.DeepEqual(got, c.Rows) {
			t.Errorf("rows %q: got %q %v, want %q %v", c.Input, text, got, c.Dice, c.Rows)
		}
	}
	for _, c := range v.RowErrors {
		_, _, err := ParseRows(c.Input)
		if err == nil || !strings.Contains(err.Error(), c.Error) {
			t.Errorf("rows %q: got %v, want an error with %q", c.Input, err, c.Error)
		}
	}
	for _, c := range v.D6 {
		if got := D6(c.Input); got != c.Dice {
			t.Errorf("/dice %q: got %q, want %q", c.Input, got, c.Dice)
		}
	}
	texts := 0
	for _, c := range v.Fallback {
		var kind struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(c.Chance, &kind); err != nil {
			t.Fatal(err)
		}
		if kind.Kind != "roll" {
			continue
		}
		var r model.DiceRoll
		if err := json.Unmarshal(c.Chance, &r); err != nil {
			t.Fatal(err)
		}
		if got := Text(r); got != c.Text {
			t.Errorf("roll text: got %q, want %q", got, c.Text)
		}
		if !r.Valid() {
			t.Errorf("%+v is not valid", r)
		}
		texts++
	}
	if len(v.Parse) == 0 || len(v.Rolls) == 0 || len(v.D6) == 0 || texts == 0 {
		t.Fatal("the cases were not read")
	}
}

// registry is the host with this module alone, every die falling as pick
// says.
func registry(t *testing.T, pick func(n int) int) *modhost.Registry {
	t.Helper()
	r, err := modhost.New(modhost.Options{Intn: pick}, Module())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// /roll and /dice, through the host as the server runs them, with the
// lowest and highest faces: the text is the roll in words, the view a value
// of kind dice_roll made by this module, and a roll the grammar refuses
// comes back as the grammar's sentence.
func TestCommands(t *testing.T) {
	ctx := context.Background()
	lowest, highest := func(int) int { return 0 }, func(n int) int { return n - 1 }
	for _, pick := range []func(int) int{lowest, highest} {
		r := registry(t, pick)
		for _, c := range []struct{ cmd, text, dice string }{
			{"roll", "", "1d20"}, {"roll", "100d1000", "100d1000"}, {"roll", " 4d4+d6, 3d10k2 ", "4d4+1d6, 3d10kh2"},
			{"roll", "adv", "2d20kh1"}, {"dice", "", "1d6"}, {"dice", "4+3", "4d6+3"}, {"dice", "4, 2d8", "4d6, 2d8"},
		} {
			made, err := r.Command(ctx, model.Account{}, "", c.cmd, c.text)
			if err != nil {
				t.Fatalf("/%s %s: %v", c.cmd, c.text, err)
			}
			var got model.DiceRoll
			if made.View == nil || made.View.Kind != model.KindDiceRoll || made.View.By != Name || json.Unmarshal(made.View.Data, &got) != nil {
				t.Fatalf("/%s %s made %+v", c.cmd, c.text, made)
			}
			if got.Dice != c.dice || !got.Valid() || made.Text != Text(got) || !strings.HasPrefix(made.Text, "Rolled "+c.dice+":") {
				t.Errorf("/%s %s rolled %+v as %q", c.cmd, c.text, got, made.Text)
			}
		}
	}
	r := registry(t, nil)
	for _, c := range []struct{ cmd, text, want string }{
		{"roll", "banana", "Write dice like"}, {"roll", "101d6", "at most 100 dice"}, {"roll", "1,2,3,4,5,6,7,8,9", "at most 8 rows"},
		{"dice", "0", "at least one die"},
	} {
		_, err := r.Command(ctx, model.Account{}, "", c.cmd, c.text)
		var e *model.Error
		if err == nil || !strings.Contains(err.Error(), c.want) || !asError(err, &e) || e.Code != model.CodeInvalidValue {
			t.Errorf("/%s %s: %v, want an invalid value saying %q", c.cmd, c.text, err, c.want)
		}
	}
}

// Inline dice: the claim offers `[[...]]` whose inside is made of the
// grammar's characters, and the module rolls what the grammar reads as one
// row; what it refuses stays text.
func TestInlineDice(t *testing.T) {
	ctx := context.Background()
	r := registry(t, func(n int) int { return n - 1 })
	claims := r.Claims()
	if len(claims) != 1 || claims[0].Kind != model.KindDiceRoll || claims[0].Open != "[[" || claims[0].Close != "]]" {
		t.Fatalf("claims: %+v", claims)
	}
	for _, in := range []string{"2d6+3", " 2D6 + 3 ", "d%", "DIS", "4d6kh3"} {
		if !claims[0].Inside.MatchString(in) {
			t.Errorf("%q is not offered as dice", in)
		}
	}
	for _, in := range []string{"", "banana", strings.Repeat("1", 101), "2d6\n"} {
		if claims[0].Inside.MatchString(in) {
			t.Errorf("%q is offered as dice", in)
		}
	}
	v, ok := r.Resolve(ctx, model.Account{}, "", model.KindDiceRoll, "2d6+3")
	var got model.DiceRoll
	if !ok || json.Unmarshal(v.Data, &got) != nil || got.Dice != "2d6+3" || len(got.Rows) != 1 || got.Rows[0].Total != 15 || v.By != Name {
		t.Fatalf("[[2d6+3]] resolved as %+v %v", v, ok)
	}
	for _, in := range []string{"1d6, 1d6", "dd", "2d1", " "} {
		if _, ok := r.Resolve(ctx, model.Account{}, "", model.KindDiceRoll, in); ok {
			t.Errorf("[[%s]] was rolled", in)
		}
	}
}

func asError(err error, out **model.Error) bool {
	e, ok := err.(*model.Error)
	*out = e
	return ok
}

// The help of /roll and /dice is the module's (it left help.json): each has
// a description, its argument explained, two or more examples written with
// {cmd}, and where and who; and both are typed in two forms, dice or
// nothing.
func TestHelpAndForms(t *testing.T) {
	m := Module()
	if !m.PeopleOnly {
		t.Error("dice are for people: a bot rolls its own")
	}
	for _, c := range m.Commands {
		h := c.Help
		if len(h.Description) < 40 || len(h.Args) != 1 || len(h.Examples) < 2 || h.Where == "" || h.Who == "" {
			t.Errorf("/%s: the help %+v", c.Name, h)
		}
		for _, e := range h.Examples {
			if !strings.HasPrefix(e.Type, "/{cmd}") || e.Then == "" {
				t.Errorf("/%s: the example %+v", c.Name, e)
			}
		}
		if len(c.Forms) != 2 || len(c.Forms[0].Slots) != 1 || c.Forms[0].Slots[0].Kind != TypeDice || len(c.Forms[1].Slots) != 0 {
			t.Errorf("/%s: the forms %+v", c.Name, c.Forms)
		}
	}
}
