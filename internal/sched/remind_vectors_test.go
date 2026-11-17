package sched

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// TestRemindVectors checks that the server takes every repeat rule the
// clients read from /remind (testdata/commands/remind.json, which both
// clients are held to), with the account's time zone they carry.
func TestRemindVectors(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/commands/remind.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Cases []struct {
			Args string `json:"args"`
			Want *struct {
				Repeat *model.Repeat `json:"repeat"`
			} `json:"want"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range v.Cases {
		if c.Want == nil || c.Want.Repeat == nil {
			continue
		}
		r := *c.Want.Repeat
		before := r
		if err := checkRepeat(&r); err != nil {
			t.Errorf("/remind %s: the server refuses the repeat: %v", c.Args, err)
		}
		if r != before {
			t.Errorf("/remind %s: the server filled in %+v, so the client left something out", c.Args, r)
		}
		n++
	}
	if n < 5 {
		t.Fatalf("only %d repeats", n)
	}
}
