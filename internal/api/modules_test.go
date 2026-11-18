package api_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// TestModuleSwitches: an admin switches a built-in module on and off for the
// server, and a channel's managers switch off in their channel what the
// server has on (docs/DESIGN-MODULES.md section 6.5). Where it is off its
// commands are not offered and not run, the objects it claims stay text,
// and what it made is served as its text; switched on again, all of it is
// served as before.
func TestModuleSwitches(t *testing.T) {
	ts := testkit.NewServer(t)
	owner, sam, kim := ts.Owner, ts.Member("sam"), ts.Member("kim")
	room := sam.Channel("games", false, kim, owner)
	elsewhere := sam.Channel("lobby", false, kim)
	admin := "/api/v1/admin/modules"
	here := convPath(room) + "/modules"
	commands := "/api/v1/commands?conversation_id=" + room
	get := func(m map[string]any) map[string]any {
		t.Helper()
		return kim.Must(200, "GET", "/api/v1/messages/"+m["id"].(string), nil)["message"].(map[string]any)
	}
	diceModule := func(list []any) map[string]any {
		t.Helper()
		for _, m := range list {
			if m := m.(map[string]any); m["name"] == "dice" {
				return m
			}
		}
		t.Fatalf("no dice module in %v", list)
		return nil
	}

	// The admin's list says what the module declares.
	d := diceModule(arr(owner.Must(200, "GET", admin, nil), "modules"))
	if d["on"] != true || d["server_on"] != true || d["title"] != "Dice" || len(arr(d, "commands")) != 2 ||
		arr(d, "claims")[0] != model.KindDiceRoll {
		t.Fatalf("the dice module: %v", d)
	}
	sam.Refused(403, model.CodeForbidden, "GET", admin, nil)
	sam.Refused(403, model.CodeForbidden, "PUT", admin+"/dice", map[string]any{"on": false})
	owner.Refused(404, model.CodeNotFound, "PUT", admin+"/nothing", map[string]any{"on": false})

	roll := rolled(kim, room, "roll", "2d6")
	inline := kim.Post(room, "I hit for [[1d8]]")
	if roll["view"] == nil || len(diceIn(inline)) != 1 {
		t.Fatalf("made while on: %v %v", roll, inline)
	}

	// Off on the server.
	off := obj(owner.Must(200, "PUT", admin+"/dice", map[string]any{"on": false}), "module")
	if off["on"] != false || off["server_on"] != false {
		t.Fatalf("switched off: %v", off)
	}
	if got := get(roll); got["view"] != nil || got["text"] != roll["text"] {
		t.Fatalf("a roll while off is served as its text: %v", got)
	}
	if got := get(inline); len(diceIn(got)) != 0 || got["text"] != inline["text"] {
		t.Fatalf("inline dice while off stay text: %v", got)
	}
	kim.Refused(403, model.CodeFeatureOff, "POST", convPath(room)+"/commands", map[string]any{"name": "roll", "text": "d20"})
	if cmds := names(arr(kim.Must(200, "GET", commands, nil), "commands")); cmds["roll"] || cmds["dice"] || !cmds["flip"] {
		t.Fatalf("offered while off: %v", cmds)
	}
	if typed := kim.Post(room, "and [[1d6]]"); len(diceIn(typed)) != 0 || typed["text"] != "and [[1d6]]" {
		t.Fatalf("typed while off: %v", typed)
	}
	if d := diceModule(arr(kim.Must(200, "GET", here, nil), "modules")); d["on"] != false || d["server_on"] != false {
		t.Fatalf("the channel's list while the server has it off: %v", d)
	}
	sam.Refused(403, model.CodeFeatureOff, "PUT", here+"/dice", map[string]any{"on": true})

	// On again: what it made is served as before.
	owner.Must(200, "PUT", admin+"/dice", map[string]any{"on": true})
	if got := get(roll); got["view"] == nil {
		t.Fatalf("the roll's view did not come back: %v", got)
	}
	if got := get(inline); len(diceIn(got)) != 1 {
		t.Fatalf("the inline dice did not come back: %v", got)
	}

	// Off in one channel, by its manager; a member may not.
	kim.Refused(403, model.CodeForbidden, "PUT", here+"/dice", map[string]any{"on": false})
	sam.Refused(400, model.CodeInvalidRequest, "PUT", here+"/dice", map[string]any{})
	if d := obj(sam.Must(200, "PUT", here+"/dice", map[string]any{"on": false}), "module"); d["on"] != false || d["server_on"] != true {
		t.Fatalf("switched off in the channel: %v", d)
	}
	kim.Refused(403, model.CodeFeatureOff, "POST", convPath(room)+"/commands", map[string]any{"name": "dice", "text": "2"})
	if get(roll)["view"] != nil {
		t.Fatal("a roll in the channel that switched dice off kept its view")
	}
	if rolled(kim, elsewhere, "roll", "d4")["view"] == nil {
		t.Fatal("another channel lost the module")
	}
	// The server's switch leaves the channel's as its managers set it.
	owner.Must(200, "PUT", admin+"/dice", map[string]any{"on": false})
	owner.Must(200, "PUT", admin+"/dice", map[string]any{"on": true})
	if d := diceModule(arr(kim.Must(200, "GET", here, nil), "modules")); d["on"] != false || d["server_on"] != true {
		t.Fatalf("the channel's own switch: %v", d)
	}
	sam.Must(200, "PUT", here+"/dice", map[string]any{"on": true})
	if rolled(kim, room, "roll", "d4")["view"] == nil || get(roll)["view"] == nil {
		t.Fatal("on again in the channel")
	}
}
