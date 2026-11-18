package api_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modules/dice"
	"github.com/tikaydaes/churr/internal/testkit"
	"github.com/tikaydaes/churr/internal/wirepb"
)

// Dice are values of the catalogue's kind dice_roll (docs/DESIGN-MODULES.md
// section 4.1). A roll asked for is a message whose text is the roll in
// words and whose view is the roll. Inline dice (ruling R73), `[[2d6+3]]` in
// a person's message, are rolled by the server on post and stored as an
// object entity over those characters; an edit rolls nothing, keeping the
// roll of dice it leaves alone and marking changed dice removed; bots get
// none.

// rollOf reads the roll a value of kind dice_roll carries.
func rollOf(t *testing.T, v map[string]any) map[string]any {
	t.Helper()
	if v["kind"] != model.KindDiceRoll || v["level"] != float64(1) || v["by"] != "dice" {
		t.Fatalf("not a roll: %v", v)
	}
	r, ok := v["data"].(map[string]any)
	if !ok {
		t.Fatalf("a roll's data is not the roll's object: %v", v["data"])
	}
	return r
}

// sameOverProtobuf asks for something in JSON and in protobuf and checks
// the protobuf answer is protobuf, not JSON fallen back to, and reads back
// as the JSON answer: a value's data is bytes on that wire and the kind's
// object on the other.
func sameOverProtobuf(t *testing.T, ts *testkit.TestServer, bearer, op, path string) {
	t.Helper()
	auth := "Bearer " + bearer
	_, _, want := ts.Raw("GET", path, http.Header{"Authorization": {auth}}, nil)
	status, h, pb := ts.Raw("GET", path, http.Header{"Authorization": {auth}, "Accept": {wirepb.MediaType}}, nil)
	if _, is := wirepb.BodyVersion(h.Get("Content-Type")); status != 200 || !is {
		_, why := wirepb.EncodeResponse(op, 200, want)
		t.Fatalf("%s was not answered in protobuf: %d %s (%v)", op, status, h.Get("Content-Type"), why)
	}
	got, err := wirepb.DecodeResponse(op, status, pb)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	json.Unmarshal(want, &a)
	json.Unmarshal(got, &b)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("%s differs over protobuf:\n got %s\nwant %s", op, got, want)
	}
}

// rolled runs a module's command, /roll or /dice, as a client does, and
// returns the message its answer was posted as.
func rolled(c *testkit.Client, room, name, text string) map[string]any {
	return c.Must(200, "POST", convPath(room)+"/commands", map[string]any{"name": name, "text": text})["message"].(map[string]any)
}

// remarshal turns a JSON value into a Go value through its JSON.
func remarshal(t *testing.T, v any, out any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err == nil {
		err = json.Unmarshal(raw, out)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// diceIn is the inline dice of a message: its object entities of kind
// dice_roll.
func diceIn(m map[string]any) []map[string]any {
	var out []map[string]any
	for _, e := range entitiesOfType(m, model.EntityObject) {
		if o, ok := e["object"].(map[string]any); ok && o["kind"] == model.KindDiceRoll {
			out = append(out, e)
		}
	}
	return out
}

func TestInlineDice(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	room := owner.Channel("table", false)
	msgs := convPath(room) + "/messages"

	m := owner.Post(room, "I hit for [[2d6+3]] and [[1d4]]")
	dice := diceIn(m)
	if len(dice) != 2 || len(entitiesOfType(m, "dice")) != 0 {
		t.Fatalf("want two dice, got %v", m["entities"])
	}
	d := dice[0]
	roll := rollOf(t, d["object"].(map[string]any))
	rows := arr(roll, "rows")
	row := rows[0].(map[string]any)
	total := row["total"]
	if roll["dice"] != "2d6+3" || len(rows) != 1 || row["dice"] != "2d6+3" || len(arr(row, "terms")) != 2 || int(d["at"].(float64)) != 10 || int(d["len"].(float64)) != 9 {
		t.Fatalf("the roll: %v %v", d, roll)
	}
	if n, _ := total.(float64); n < 5 || n > 15 {
		t.Fatalf("the total: %v", roll)
	}
	if m["text"] != "I hit for [[2d6+3]] and [[1d4]]" {
		t.Fatalf("the text stays as typed: %v", m["text"])
	}

	// An edit that moves the first dice keeps their roll; the second,
	// changed, is marked removed with nothing rolled.
	id := m["id"].(string)
	e := obj(owner.Must(200, "PATCH", "/api/v1/messages/"+id, map[string]any{"text": "Oh, I hit for [[2d6+3]] and [[1d6]]"}), "message")
	after := diceIn(e)
	if len(after) != 2 {
		t.Fatalf("after the edit: %v", e["entities"])
	}
	kept := after[0]["object"].(map[string]any)
	if !reflect.DeepEqual(kept["data"], d["object"].(map[string]any)["data"]) || int(after[0]["at"].(float64)) != 14 {
		t.Fatalf("the kept roll: %v (was %v)", after[0], d)
	}
	if gone := after[1]["object"].(map[string]any); gone["removed"] != true || gone["data"] != nil {
		t.Fatalf("changed dice are marked removed: %v", after[1])
	}

	// Dice added in an edit are not rolled either.
	plain := owner.Post(room, "no dice yet")
	e2 := obj(owner.Must(200, "PATCH", "/api/v1/messages/"+plain["id"].(string), map[string]any{"text": "now [[1d20]]"}), "message")
	if got := diceIn(e2); len(got) != 1 || got[0]["object"].(map[string]any)["removed"] != true {
		t.Fatalf("dice added by an edit: %v", e2["entities"])
	}

	// Bots get none: the text stays text.
	bot := owner.Bot("dealer")
	owner.Add(room, "", bot)
	b := bot.Must(201, "POST", msgs, map[string]any{"text": "rolling [[1d6]]"})["message"].(map[string]any)
	if got := diceIn(b); len(got) != 0 {
		t.Fatalf("a bot's dice: %v", got)
	}

	// Kept plain, nothing is rolled.
	p := owner.Must(201, "POST", msgs, map[string]any{"text": "[[1d6]]",
		"object_hints": []map[string]any{{"start": 0, "end": 7, "kind": "plain"}}})["message"].(map[string]any)
	if got := diceIn(p); len(got) != 0 {
		t.Fatalf("kept plain: %v", got)
	}
}

// TestARollIsTextAndView: a roll asked for is stored and sent as text that
// reads correctly with no client support, and the roll as the message's
// view; its text is the server's, which no edit changes.
func TestARollIsTextAndView(t *testing.T) {
	ts := testkit.NewServer(t)
	owner, sam := ts.Owner, ts.Member("sam")
	room := owner.Channel("games", false, sam)
	msgs := convPath(room) + "/messages"

	m := rolled(sam, room, "roll", "4d6kh3+2, d20")
	if m["chance"] != nil || m["fallback"] != nil {
		t.Fatalf("a roll is no chance result: %v", m)
	}
	roll := rollOf(t, m["view"].(map[string]any))
	rows := arr(roll, "rows")
	first := rows[0].(map[string]any)
	term := arr(first, "terms")[0].(map[string]any)
	if roll["dice"] != "4d6kh3+2, 1d20" || len(rows) != 2 || len(arr(term, "rolls")) != 4 || len(arr(term, "dropped")) != 1 {
		t.Fatalf("the roll: %v", roll)
	}
	var r model.DiceRoll
	remarshal(t, roll, &r)
	if !r.Valid() || m["text"] != dice.Text(r) || len(dice.Text(r)) < len("Rolled 4d6kh3+2, 1d20: [") {
		t.Fatalf("the text %q is not the roll %+v in words", m["text"], r)
	}

	// Read back, the same; the text is searched.
	got := owner.Must(200, "GET", "/api/v1/messages/"+m["id"].(string), nil)["message"].(map[string]any)
	if got["text"] != m["text"] || !reflect.DeepEqual(got["view"], m["view"]) {
		t.Fatalf("read back: %v", got)
	}

	// The same on both wires, the view and an inline roll beside it.
	owner.Post(room, "and [[1d6]] more")
	bearer := owner.Token().Bearer
	sameOverProtobuf(t, ts, bearer, "getMessage", "/api/v1/messages/"+m["id"].(string))
	sameOverProtobuf(t, ts, bearer, "listMessages", msgs)

	// No edit reaches it.
	sam.Refused(409, model.CodeConflict, "PATCH", "/api/v1/messages/"+m["id"].(string), map[string]any{"text": "Rolled 1d20: [20] = 20"})
	again := owner.Must(200, "GET", "/api/v1/messages/"+m["id"].(string), nil)["message"].(map[string]any)
	if again["text"] != m["text"] {
		t.Fatalf("after a refused edit: %v", again)
	}
	sam.Refused(400, model.CodeInvalidValue, "POST", convPath(room)+"/commands", map[string]any{"name": "roll", "text": "1,2,3,4,5,6,7,8,9"})
	// A roll is no chance result.
	sam.Refused(400, model.CodeInvalidRequest, "POST", msgs, map[string]any{"chance": map[string]any{"kind": "roll"}})
}
