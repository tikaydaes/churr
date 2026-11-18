package cards

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/fair"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modhost"
)

// at is a draw whose one object is object i of a draw whose output is beta,
// as a message's object i would be.
type at struct {
	beta []byte
	i    uint32
}

func (a at) Stream() (*fair.Stream, error) { return fair.NewStream(a.beta, a.i), nil }

// kindCases runs the module's commands on the draws of testdata/fair/
// kinds.json (docs/FAIR.md section 5a): each listed case, typed as given,
// makes the case's value exactly, its text the value's words, and the value
// records the case's object and the primitives it asked, in order. Every
// refusal of testdata/commands/chance.json for its commands is an invalid
// value with the words given.
func kindCases(t *testing.T, mod modhost.Module, typed map[string][2]string) {
	t.Helper()
	ctx := context.Background()
	r, err := modhost.New(modhost.Options{}, mod)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Cases []struct {
			Label string                `json:"label"`
			Kind  string                `json:"kind"`
			Beta  string                `json:"beta"`
			I     uint32                `json:"i"`
			Asks  []model.FairPrimitive `json:"asks"`
			Data  json.RawMessage       `json:"data"`
		} `json:"cases"`
	}
	raw, err := os.ReadFile("../../../testdata/fair/kinds.json")
	if err == nil {
		err = json.Unmarshal(raw, &file)
	}
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, c := range file.Cases {
		cmd, ok := typed[c.Label]
		if !ok {
			continue
		}
		seen++
		beta, _ := hex.DecodeString(c.Beta)
		made, err := r.Command(ctx, model.Account{}, "", cmd[0], cmd[1], at{beta, c.I})
		if err != nil {
			t.Fatalf("%s: /%s %s: %v", c.Label, cmd[0], cmd[1], err)
		}
		v := made.View
		if v == nil || v.Kind != c.Kind || v.By != mod.Name || v.Draw == nil || v.Draw.I != int(c.I) {
			t.Fatalf("%s: made %+v", c.Label, made)
		}
		var got, want any
		_ = json.Unmarshal(v.Data, &got)
		_ = json.Unmarshal(c.Data, &want)
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		if !bytes.Equal(g, w) {
			t.Errorf("%s: made %s, want %s", c.Label, g, w)
		}
		ga, _ := json.Marshal(v.Draw.Primitives)
		wa, _ := json.Marshal(c.Asks)
		if !bytes.Equal(ga, wa) {
			t.Errorf("%s: recorded %s, want %s", c.Label, ga, wa)
		}
		if ok, err := fair.Rederive(v.Kind, v.Data, beta, c.I); !ok || err != nil {
			t.Errorf("%s: does not re-derive: %v", c.Label, err)
		}
		if made.Text == "" {
			t.Errorf("%s: no text", c.Label)
		}
	}
	if seen != len(typed) {
		t.Fatalf("%d of %d cases found", seen, len(typed))
	}
	var refusals struct {
		Refusals []struct {
			Command string `json:"command"`
			Text    string `json:"text"`
			Error   string `json:"error"`
		} `json:"refusals"`
	}
	raw, err = os.ReadFile("../../../testdata/commands/chance.json")
	if err == nil {
		err = json.Unmarshal(raw, &refusals)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range refusals.Refusals {
		if !r.HasCommand(c.Command) {
			continue
		}
		_, err := r.Command(ctx, model.Account{}, "", c.Command, c.Text, at{make([]byte, 32), 0})
		var e *model.Error
		if err == nil || !strings.Contains(err.Error(), c.Error) || !errors.As(err, &e) || e.Code != model.CodeInvalidValue {
			t.Errorf("/%s %q: %v, want an invalid value saying %q", c.Command, c.Text, err, c.Error)
		}
	}
	// A bot draws its own.
	for _, c := range mod.Commands {
		if _, err := r.Command(ctx, model.Account{Role: model.RoleBot}, "", c.Name, "", at{make([]byte, 32), 0}); err == nil {
			t.Errorf("/%s ran for a bot", c.Name)
		}
	}
}

// /draw, /mahjongg and /tarot: the cards from the draw.
func TestCards(t *testing.T) {
	kindCases(t, Module(), map[string][2]string{
		"draw standard one": {"draw", ""}, "draw standard five": {"draw", "5"}, "draw tarot three": {"draw", "3 Tarot"},
		"draw pinochle twelve": {"draw", "12 pinochle"}, "draw hanafuda all": {"draw", "48 hanafuda"},
		"mahjongg thirteen": {"mahjong", "13"}, "tarot three": {"tarot", ""}, "tarot celtic": {"tarot", "Celtic-Cross"},
		"tarot relationship": {"tarot", "love"},
	})
}
