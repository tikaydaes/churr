package modhost

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/tikaydaes/churr/internal/fair"
	"github.com/tikaydaes/churr/internal/model"
)

// fake answers with whatever it is set to.
type fake struct {
	reply Reply
	value Value
	err   error
	seen  []string
}

func (f *fake) Command(_ context.Context, h *Host, in Invocation) (Reply, error) {
	f.seen = append(f.seen, in.Command+" "+in.Text)
	return f.reply, f.err
}

func (f *fake) Resolve(_ context.Context, h *Host, in Marked) (Value, error) {
	f.seen = append(f.seen, in.Kind+" "+in.Text)
	return f.value, f.err
}

// noArgs is the one form of a command that takes nothing.
var noArgs = []model.CommandForm{{}}

// roll is a roll of 1d6 that came up n.
func roll(n int) model.DiceRoll {
	return model.DiceRoll{Dice: "1d6", Rows: []model.DiceRow{{Dice: "1d6", Total: n,
		Terms: []model.DiceTerm{{Sign: 1, Count: 1, Sides: 6, Rolls: []int{n}, Value: n}}}}}
}

// The registry refuses modules it could not tell apart or could not run.
func TestRegistryRefusesWhatItCannotHost(t *testing.T) {
	ok := Module{Name: "dice", Handler: &fake{}, Commands: []Command{{Name: "roll", Forms: noArgs}}}
	for name, mods := range map[string][]Module{
		"a bad name":        {{Name: "Dice!", Handler: &fake{}}},
		"no handler":        {{Name: "dice"}},
		"the same name":     {ok, {Name: "dice", Handler: &fake{}}},
		"the same command":  {ok, {Name: "other", Handler: &fake{}, Commands: []Command{{Name: "roll", Forms: noArgs}}}},
		"a bad command":     {{Name: "dice", Handler: &fake{}, Commands: []Command{{Name: "Roll Dice", Forms: noArgs}}}},
		"an unknown kind":   {{Name: "dice", Handler: &fake{}, Claims: []Claim{{Kind: "no_such_kind", Open: "[[", Close: "]]", Pattern: "x"}}}},
		"no marks":          {{Name: "dice", Handler: &fake{}, Claims: []Claim{{Kind: model.KindDiceRoll, Pattern: "x"}}}},
		"a bad pattern":     {{Name: "dice", Handler: &fake{}, Claims: []Claim{{Kind: model.KindDiceRoll, Open: "[[", Close: "]]", Pattern: "("}}}},
		"no forms":          {{Name: "dice", Handler: &fake{}, Commands: []Command{{Name: "roll"}}}},
		"a slot of no kind": {{Name: "dice", Handler: &fake{}, Commands: []Command{{Name: "roll", Forms: []model.CommandForm{{Slots: []model.CommandSlot{{Name: "dice", Kind: "dice"}}}}}}}},
		"text not last":     {{Name: "dice", Handler: &fake{}, Commands: []Command{{Name: "roll", Forms: []model.CommandForm{{Slots: []model.CommandSlot{{Name: "a", Kind: "text"}, {Name: "b", Kind: "text"}}}}}}}},
		"a bad type":        {{Name: "dice", Handler: &fake{}, Types: []DerivedType{{Name: "dice", Pattern: "("}}}},
	} {
		if _, err := New(Options{}, mods...); err == nil {
			t.Errorf("%s was registered", name)
		}
	}
	if _, err := New(Options{}, ok); err != nil {
		t.Fatal(err)
	}
	typed := Module{Name: "dice", Handler: &fake{}, Types: []DerivedType{{Name: "dice", Pattern: "[0-9d]+"}},
		Commands: []Command{{Name: "roll", Forms: []model.CommandForm{{Slots: []model.CommandSlot{{Name: "dice", Kind: "dice"}}}, {}}}}}
	if _, err := New(Options{}, typed); err != nil {
		t.Fatal(err)
	}
}

// What a module answers is checked and stamped by the host: a reply needs
// text; a value is held to its kind's schema and check and carries the
// kind's level and the module's name; a module's refusal comes back as it
// gave it.
func TestRegistryChecksWhatModulesAnswer(t *testing.T) {
	ctx := context.Background()
	f := &fake{}
	r, err := New(Options{}, Module{Name: "dice", Handler: f, Commands: []Command{{Name: "roll", Forms: noArgs}},
		Claims: []Claim{{Kind: model.KindDiceRoll, Open: "[[", Close: "]]", Pattern: `[0-9d]{1,10}`}}})
	if err != nil {
		t.Fatal(err)
	}
	actor := model.Account{ID: model.NewID[model.AccountID]()}

	f.reply = Reply{Text: "Rolled 1d6: [4] = 4", View: &Value{Kind: model.KindDiceRoll, Data: roll(4)}}
	made, err := r.Command(ctx, actor, "", "roll", "d6", nil)
	if err != nil || made.Text != f.reply.Text || made.View == nil || made.View.Level != 1 || made.View.By != "dice" {
		t.Fatalf("made %+v, %v", made, err)
	}
	var back model.DiceRoll
	if json.Unmarshal(made.View.Data, &back) != nil || back.Rows[0].Total != 4 {
		t.Fatalf("the data: %s", made.View.Data)
	}
	f.reply = Reply{View: f.reply.View}
	if _, err := r.Command(ctx, actor, "", "roll", "", nil); err == nil {
		t.Error("a reply with no text was taken")
	}
	f.reply = Reply{Text: "Rolled 1d6: [9] = 9", View: &Value{Kind: model.KindDiceRoll, Data: roll(9)}}
	if _, err := r.Command(ctx, actor, "", "roll", "", nil); err == nil {
		t.Error("a value that fails its kind's check was taken")
	}
	f.reply = Reply{Text: "x", View: &Value{Kind: model.KindDiceRoll, Data: map[string]any{"dice": 7}}}
	if _, err := r.Command(ctx, actor, "", "roll", "", nil); err == nil {
		t.Error("a value that does not fit its kind's schema was taken")
	}
	refusal := model.Invalid("text", "Write dice like 2d6+3.")
	f.err = refusal
	if _, err := r.Command(ctx, actor, "", "roll", "banana", nil); !errors.Is(err, refusal) {
		t.Errorf("the module's refusal came back as %v", err)
	}
	if _, err := r.Command(ctx, actor, "", "nothing", "", nil); err == nil {
		t.Error("a command no module answers was run")
	}

	f.err, f.value = nil, Value{Kind: model.KindDiceRoll, Data: roll(2)}
	f.seen = nil
	if v, ok := r.Resolve(ctx, actor, "", model.KindDiceRoll, "1d6", nil); !ok || v.By != "dice" || v.Removed {
		t.Fatalf("resolved %+v %v", v, ok)
	}
	if _, ok := r.Resolve(ctx, actor, "", model.KindDiceRoll, "banana", nil); ok || len(f.seen) != 1 {
		t.Errorf("an inside the claim's pattern does not match reached the module: %v", f.seen)
	}
	if v, ok := r.Unresolved(model.KindDiceRoll); !ok || !v.Removed || v.By != "dice" || v.Level != 1 || v.Data != nil {
		t.Errorf("unresolved: %+v", v)
	}
}

// A host's chance is its share of the message's draw, and nothing else: the
// first ask takes the next object's stream and later asks give the same; a
// declared outcome is derived from it and recorded for the value; a call
// with no draw has no chance at all.
func TestHostRandomness(t *testing.T) {
	beta := make([]byte, 32)
	objs := fair.NewObjects(beta)
	first := NewHost(HostOptions{Draws: objs})
	s, err := first.Random()
	if err != nil || s.Object() != 0 {
		t.Fatalf("first object: %v %v", s, err)
	}
	if again, _ := first.Random(); again != s {
		t.Error("a second ask in one call took another stream")
	}
	second := NewHost(HostOptions{Draws: objs})
	p := model.FairPrimitive{Op: "int", Min: 1, Max: 6}
	got, err := second.Draw(p)
	want, _ := fair.Draw(fair.NewStream(beta, 1), fair.Primitive{Op: "int", Min: 1, Max: 6})
	if err != nil || len(got) != 1 || *got[0].Value != *want.Value {
		t.Fatalf("drew %+v %v, want %v", got, err, *want.Value)
	}
	if d := second.drew(); d == nil || d.I != 1 || len(d.Primitives) != 1 || d.Primitives[0].Op != "int" {
		t.Errorf("recorded %+v", d)
	}
	if _, err := second.Draw(model.FairPrimitive{Op: "coin"}); err == nil {
		t.Error("a primitive derive 1 does not know was drawn")
	}
	if NewHost(HostOptions{}).drew() != nil {
		t.Error("a call that took no chance records a draw")
	}
	if _, err := NewHost(HostOptions{}).Random(); err == nil {
		t.Error("a call with no draw had chance")
	}
}

// A module that answers people only refuses a bot; any other answers bots
// too. It is the module's to say, not the host's.
func TestPeopleOnly(t *testing.T) {
	ctx := context.Background()
	f := &fake{reply: Reply{Text: "ok"}}
	r, err := New(Options{}, Module{Name: "people", PeopleOnly: true, Handler: f, Commands: []Command{{Name: "mine", Forms: noArgs}}},
		Module{Name: "anyone", Handler: f, Commands: []Command{{Name: "ours", Forms: noArgs}}})
	if err != nil {
		t.Fatal(err)
	}
	bot := model.Account{ID: model.NewID[model.AccountID](), Role: model.RoleBot}
	if _, err := r.Command(ctx, bot, "", "mine", "", nil); !errors.As(err, new(*model.Error)) || err.(*model.Error).Code != model.CodeInvalidValue {
		t.Errorf("a bot ran a people-only command: %v", err)
	}
	if _, err := r.Command(ctx, bot, "", "ours", "", nil); err != nil {
		t.Errorf("a bot was refused a command for anyone: %v", err)
	}
}
