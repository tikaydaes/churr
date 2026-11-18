package api_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The built-in commands of ruling R67 that reach the server: /me's action
// mark and the chance results the server draws.

func names(cmds []any) map[string]bool {
	out := map[string]bool{}
	for _, c := range cmds {
		m := c.(map[string]any)
		if m["kind"] == "builtin" {
			out[m["name"].(string)] = true
		}
	}
	return out
}

func TestActionAndChance(t *testing.T) {
	ts := testkit.NewServer(t)
	owner, sam := ts.Owner, ts.Member("sam")
	room := owner.Channel("games", false, sam)
	msgs := convPath(room) + "/messages"

	// /me: the mark is stored, survives an edit, travels in the event.
	act := owner.Must(201, "POST", msgs, map[string]any{"text": "waves", "action": true})["message"].(map[string]any)
	if act["action"] != true || act["text"] != "waves" {
		t.Fatalf("action: %v", act)
	}
	edited := owner.Must(200, "PATCH", "/api/v1/messages/"+act["id"].(string), map[string]any{"text": "waves hello"})["message"].(map[string]any)
	if edited["action"] != true {
		t.Fatalf("the action mark after an edit: %v", edited)
	}
	owner.Refused(400, model.CodeInvalidValue, "POST", msgs, map[string]any{"action": true, "chance": map[string]any{"kind": "flip"}})

	// A roll: /roll runs through the dice module on the server, which rolls
	// every die and posts the total as the message's text and view
	// (dice_test.go).
	for i := 0; i < 20; i++ {
		m := rolled(sam, room, "roll", "4d6kh3+2")
		c := rollOf(t, m["view"].(map[string]any))
		rows := c["rows"].([]any)
		row := rows[0].(map[string]any)
		terms := row["terms"].([]any)
		dice := terms[0].(map[string]any)
		rolls := dice["rolls"].([]any)
		if c["dice"] != "4d6kh3+2" || len(rows) != 1 || len(rolls) != 4 || len(dice["dropped"].([]any)) != 1 || m["chance"] != nil {
			t.Fatalf("roll: %v", m)
		}
		total := row["total"].(float64)
		if total < 5 || total > 20 || total != dice["value"].(float64)+2 {
			t.Fatalf("roll total %v: %v", total, c)
		}
		if text, _ := m["text"].(string); !strings.HasPrefix(text, "Rolled 4d6kh3+2: [") {
			t.Fatalf("a roll's text is the roll in words: %v", m)
		}
	}
	// Rows: each rolled on its own, with its own total.
	two := rollOf(t, rolled(sam, room, "roll", "4d4+d6, 3d10k2")["view"].(map[string]any))
	if rs := two["rows"].([]any); two["dice"] != "4d4+1d6, 3d10kh2" || len(rs) != 2 || rs[1].(map[string]any)["dice"] != "3d10kh2" ||
		len(rs[1].(map[string]any)["terms"].([]any)[0].(map[string]any)["dropped"].([]any)) != 1 {
		t.Fatalf("rows: %v", two)
	}
	// /dice: a bare first number of a row is that many d6.
	six := rollOf(t, rolled(sam, room, "dice", "4+3")["view"].(map[string]any))
	if six["dice"] != "4d6+3" || rollOf(t, rolled(sam, room, "dice", "")["view"].(map[string]any))["dice"] != "1d6" {
		t.Fatalf("/dice: %v", six)
	}
	cmdPath := convPath(room) + "/commands"
	// A roll the grammar refuses comes back as its sentence.
	for _, c := range []struct{ text, says string }{{"1,2,3,4,5,6,7,8,9", "at most 8 rows"}, {"101d6", "at most 100 dice"}, {"2x6", "Write dice like"}} {
		if out := sam.Refused(400, model.CodeInvalidValue, "POST", cmdPath, map[string]any{"name": "roll", "text": c.text}); !strings.Contains(fmt.Sprint(out), c.says) {
			t.Fatalf("/roll %s was refused with %v", c.text, out)
		}
	}
	// No edit reaches the result.
	r := rolled(sam, room, "roll", "")
	sam.Refused(400, model.CodeInvalidRequest, "PATCH", "/api/v1/messages/"+r["id"].(string), map[string]any{"chance": map[string]any{"kind": "flip"}})
	sam.Refused(409, model.CodeConflict, "PATCH", "/api/v1/messages/"+r["id"].(string), map[string]any{"text": "for initiative"})
	got := owner.Must(200, "GET", "/api/v1/messages/"+r["id"].(string), nil)["message"].(map[string]any)
	if got["text"] != r["text"] || !reflect.DeepEqual(got["view"], r["view"]) || rollOf(t, r["view"].(map[string]any))["dice"] != "1d20" {
		t.Fatalf("read back: %v, posted %v", got, r)
	}

	// The others.
	flip := sam.Must(201, "POST", msgs, map[string]any{"chance": map[string]any{"kind": "flip"}})["message"].(map[string]any)["chance"].(map[string]any)
	if o := flip["outcome"]; o != "heads" && o != "tails" {
		t.Fatalf("flip: %v", flip)
	}
	pick := sam.Must(201, "POST", msgs, map[string]any{"chance": map[string]any{"kind": "pick", "options": []string{" tea ", "coffee"}}})["message"].(map[string]any)["chance"].(map[string]any)
	if p := pick["picked"].(float64); (p != 0 && p != 1) || pick["options"].([]any)[0] != "tea" {
		t.Fatalf("pick: %v", pick)
	}
	sh := sam.Must(201, "POST", msgs, map[string]any{"chance": map[string]any{"kind": "shuffle", "options": []string{"a", "b", "c"}}})["message"].(map[string]any)["chance"].(map[string]any)
	if len(sh["order"].([]any)) != 3 {
		t.Fatalf("shuffle: %v", sh)
	}
	draw := sam.Must(201, "POST", msgs, map[string]any{"chance": map[string]any{"kind": "draw", "count": 3, "deck": "Tarot"}})["message"].(map[string]any)["chance"].(map[string]any)
	if draw["deck"] != "tarot" || len(draw["cards"].([]any)) != 3 || len(draw["reversed"].([]any)) != 3 {
		t.Fatalf("draw: %v", draw)
	}

	// Refusals: the grammar's limits, text beside a result, a bot.
	for _, body := range []map[string]any{
		{"chance": map[string]any{"kind": "roll"}},
		{"chance": map[string]any{"kind": "pick", "options": []string{"one"}}},
		{"chance": map[string]any{"kind": "draw", "deck": "nonsense"}},
		{"chance": map[string]any{"kind": "draw", "count": 60}},
		{"chance": map[string]any{"kind": "dice"}},
		{"text": "hi", "chance": map[string]any{"kind": "flip"}},
		{"action": true},
	} {
		sam.Refused(400, model.CodeInvalidValue, "POST", msgs, body)
	}
	bot := owner.Bot("dealer")
	owner.Add(room, "member", bot)
	bot.Refused(400, model.CodeInvalidValue, "POST", msgs, map[string]any{"chance": map[string]any{"kind": "flip"}})

	// A bot gets no roll either.
	bot.Refused(400, model.CodeInvalidValue, "POST", convPath(room)+"/commands", map[string]any{"name": "roll", "text": "d20"})

	// The menu lists the new built-ins, and a built-in no community
	// command has taken is the app's to carry out; /roll is the dice
	// module's, which the server runs.
	cmds := names(sam.Must(200, "GET", "/api/v1/commands?conversation_id="+room, nil)["commands"].([]any))
	for _, n := range []string{"me", "draw", "join", "quiet", "poll"} {
		if !cmds[n] {
			t.Fatalf("/%s is not listed: %v", n, cmds)
		}
	}
	if menu := menuOf(sam, room); menu["roll"] != model.CommandModule || menu["dice"] != model.CommandModule {
		t.Fatalf("/roll and /dice are the module's: %v", menu)
	}
	sam.Refused(400, model.CodeInvalidValue, "POST", convPath(room)+"/commands", map[string]any{"name": "draw", "text": "3"})
	if m := rolled(sam, room, "roll", "d20"); rollOf(t, m["view"].(map[string]any))["dice"] != "1d20" || m["author_id"] != string(sam.ID) {
		t.Fatalf("/roll through the server: %v", m)
	}
}

func TestChanceRefusedInSealedChat(t *testing.T) {
	w := newSealedWorld(t)
	chat := w.annPhone.Open(w.bob.ID)
	w.ann.Refused(400, model.CodeInvalidValue, "POST", convPath(chat.ID)+"/messages", map[string]any{"chance": map[string]any{"kind": "flip"}})
	w.ann.Refused(403, model.CodeForbidden, "POST", convPath(chat.ID)+"/commands", map[string]any{"name": "roll", "text": "d20"})
	w.ann.Refused(400, model.CodeInvalidRequest, "POST", convPath(chat.ID)+"/messages", map[string]any{"text": "waves", "action": true})
	cmds := names(w.ann.Must(200, "GET", "/api/v1/commands?conversation_id="+chat.ID, nil)["commands"].([]any))
	if !cmds["me"] || !cmds["shrug"] || !cmds["remind"] || cmds["roll"] || cmds["poll"] || cmds["kick"] {
		t.Fatalf("in a sealed chat the menu offers: %v", cmds)
	}
}
