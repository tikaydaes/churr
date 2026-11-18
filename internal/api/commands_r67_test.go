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
// mark, and the chance commands, which are modules' that the server runs.

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
	owner.Refused(400, model.CodeInvalidValue, "POST", msgs, map[string]any{"action": true})

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
		if c["dice"] != "4d6kh3+2" || len(rows) != 1 || len(rolls) != 4 || len(dice["dropped"].([]any)) != 1 {
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
	sam.Refused(400, model.CodeInvalidRequest, "PATCH", "/api/v1/messages/"+r["id"].(string), map[string]any{"view": map[string]any{"kind": "coin_flip"}})
	sam.Refused(409, model.CodeConflict, "PATCH", "/api/v1/messages/"+r["id"].(string), map[string]any{"text": "for initiative"})
	got := owner.Must(200, "GET", "/api/v1/messages/"+r["id"].(string), nil)["message"].(map[string]any)
	if got["text"] != r["text"] || !reflect.DeepEqual(got["view"], r["view"]) || rollOf(t, r["view"].(map[string]any))["dice"] != "1d20" {
		t.Fatalf("read back: %v, posted %v", got, r)
	}

	// The others, each a module's: the outcome is the message's view and
	// its text the outcome in words (fair_test.go holds each to its proof).
	view := func(m map[string]any) map[string]any { return m["view"].(map[string]any)["data"].(map[string]any) }
	flip := rolled(sam, room, "flip", "")
	if o := view(flip)["outcome"]; (o != "heads" && o != "tails") || flip["text"] != "Flipped a coin: "+o.(string) {
		t.Fatalf("flip: %v", flip)
	}
	pick := view(rolled(sam, room, "pick", " tea | coffee"))
	if p := pick["picked"].(float64); (p != 0 && p != 1) || pick["options"].([]any)[0] != "tea" {
		t.Fatalf("pick: %v", pick)
	}
	if sh := view(rolled(sam, room, "shuffle", "a | b | c")); len(sh["order"].([]any)) != 3 {
		t.Fatalf("shuffle: %v", sh)
	}
	if draw := view(rolled(sam, room, "draw", "3 Tarot")); draw["deck"] != "tarot" || len(draw["cards"].([]any)) != 3 || len(draw["reversed"].([]any)) != 3 {
		t.Fatalf("draw: %v", draw)
	}

	// Refusals: the grammar's limits, a request for a result in a post.
	for _, c := range []struct{ name, text string }{{"pick", "one"}, {"draw", "2 nonsense"}, {"draw", "60"}, {"flip", "now"}, {"tarot", "pyramid"}} {
		sam.Refused(400, model.CodeInvalidValue, "POST", cmdPath, map[string]any{"name": c.name, "text": c.text})
	}
	sam.Refused(400, model.CodeInvalidValue, "POST", msgs, map[string]any{"action": true})
	sam.Refused(400, model.CodeInvalidRequest, "POST", msgs, map[string]any{"chance": map[string]any{"kind": "flip"}})
	bot := owner.Bot("dealer")
	owner.Add(room, "member", bot)
	for _, n := range []string{"flip", "pick", "draw", "tarot"} {
		bot.Refused(400, model.CodeInvalidValue, "POST", cmdPath, map[string]any{"name": n, "text": "a | b"})
	}

	// A bot gets no roll either.
	bot.Refused(400, model.CodeInvalidValue, "POST", convPath(room)+"/commands", map[string]any{"name": "roll", "text": "d20"})

	// The menu lists the new built-ins, and a built-in no community
	// command has taken is the app's to carry out; /roll is the dice
	// module's, which the server runs.
	cmds := names(sam.Must(200, "GET", "/api/v1/commands?conversation_id="+room, nil)["commands"].([]any))
	for _, n := range []string{"me", "join", "quiet", "poll"} {
		if !cmds[n] {
			t.Fatalf("/%s is not listed: %v", n, cmds)
		}
	}
	menu := menuOf(sam, room)
	for _, n := range []string{"roll", "dice", "flip", "pick", "shuffle", "draw", "tarot", "mahjongg", "mahjong"} {
		if menu[n] != model.CommandModule || cmds[n] {
			t.Fatalf("/%s is a module's: %v", n, menu)
		}
	}
	if m := rolled(sam, room, "roll", "d20"); rollOf(t, m["view"].(map[string]any))["dice"] != "1d20" || m["author_id"] != string(sam.ID) {
		t.Fatalf("/roll through the server: %v", m)
	}
}

func TestChanceRefusedInSealedChat(t *testing.T) {
	w := newSealedWorld(t)
	chat := w.annPhone.Open(w.bob.ID)
	for _, n := range []string{"roll", "flip", "pick", "shuffle", "draw", "tarot", "mahjongg"} {
		w.ann.Refused(403, model.CodeForbidden, "POST", convPath(chat.ID)+"/commands", map[string]any{"name": n, "text": "a | b"})
	}
	w.ann.Refused(400, model.CodeInvalidRequest, "POST", convPath(chat.ID)+"/messages", map[string]any{"text": "waves", "action": true})
	cmds := names(w.ann.Must(200, "GET", "/api/v1/commands?conversation_id="+chat.ID, nil)["commands"].([]any))
	if !cmds["me"] || !cmds["shrug"] || !cmds["remind"] || cmds["roll"] || cmds["flip"] || cmds["draw"] || cmds["poll"] || cmds["kick"] {
		t.Fatalf("in a sealed chat the menu offers: %v", cmds)
	}
}
