package forms

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// TestPresetVectors checks that the server takes every form the clients
// build from command text (testdata/commands/presets.json, which both
// clients are held to): the server does not read command text, so the
// shared cases are where the two meet.
func TestPresetVectors(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/commands/presets.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Now   string `json:"now"`
		Cases []struct {
			Command string         `json:"command"`
			Args    string         `json:"args"`
			Want    map[string]any `json:"want"`
			Error   string         `json:"error"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	const layout = "2006-01-02T15:04"
	now, err := time.Parse(layout, v.Now)
	if err != nil {
		t.Fatal(err)
	}
	built := 0
	for _, c := range v.Cases {
		if c.Want == nil {
			continue
		}
		if s, ok := c.Want["closes_at"].(string); ok {
			at, err := time.Parse(layout, s)
			if err != nil {
				t.Fatalf("/%s %s: %v", c.Command, c.Args, err)
			}
			c.Want["closes_at"] = at.UTC().Format(time.RFC3339)
		}
		b, _ := json.Marshal(c.Want)
		var in model.FormInput
		if err := json.Unmarshal(b, &in); err != nil {
			t.Errorf("/%s %s: %v", c.Command, c.Args, err)
			continue
		}
		if in.Preset != c.Command {
			t.Errorf("/%s %s: preset %q", c.Command, c.Args, in.Preset)
		}
		if err := Validate(in, false, model.FromTime(now)); err != nil {
			t.Errorf("/%s %s: the server refuses the form: %v", c.Command, c.Args, err)
		}
		built++
	}
	if built < 10 {
		t.Fatalf("only %d forms", built)
	}
}
