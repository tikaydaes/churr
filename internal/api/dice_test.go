package api_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/testkit"
)

// Inline dice (ruling R73): `[[2d6+3]]` in a person's message is rolled
// by the server on post and stored on the entity; an edit rolls nothing,
// keeping the roll of dice it leaves alone and marking changed dice
// removed; bots get none.

func TestInlineDice(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	room := owner.Channel("table", false)
	msgs := convPath(room) + "/messages"

	m := owner.Post(room, "I hit for [[2d6+3]] and [[1d4]]")
	dice := entitiesOfType(m, "dice")
	if len(dice) != 2 {
		t.Fatalf("want two dice, got %v", m["entities"])
	}
	d := dice[0]
	total := int(d["total"].(float64))
	if d["value"] != "2d6+3" || total < 5 || total > 15 || len(arr(d, "terms")) != 2 || int(d["at"].(float64)) != 10 {
		t.Fatalf("the roll: %v", d)
	}
	if m["text"] != "I hit for [[2d6+3]] and [[1d4]]" {
		t.Fatalf("the text stays as typed: %v", m["text"])
	}

	// An edit that moves the first dice keeps their roll; the second,
	// changed, is marked removed with nothing rolled.
	id := m["id"].(string)
	e := obj(owner.Must(200, "PATCH", "/api/v1/messages/"+id, map[string]any{"text": "Oh, I hit for [[2d6+3]] and [[1d6]]"}), "message")
	after := entitiesOfType(e, "dice")
	if len(after) != 2 {
		t.Fatalf("after the edit: %v", e["entities"])
	}
	if int(after[0]["total"].(float64)) != total || int(after[0]["at"].(float64)) != 14 {
		t.Fatalf("the kept roll: %v (was %d)", after[0], total)
	}
	if after[1]["removed"] != true || after[1]["total"] != nil {
		t.Fatalf("changed dice are marked removed: %v", after[1])
	}

	// Dice added in an edit are not rolled either.
	plain := owner.Post(room, "no dice yet")
	e2 := obj(owner.Must(200, "PATCH", "/api/v1/messages/"+plain["id"].(string), map[string]any{"text": "now [[1d20]]"}), "message")
	if got := entitiesOfType(e2, "dice"); len(got) != 1 || got[0]["removed"] != true {
		t.Fatalf("dice added by an edit: %v", e2["entities"])
	}

	// Bots get none: the text stays text.
	bot := owner.Bot("dealer")
	owner.Add(room, "", bot)
	b := bot.Must(201, "POST", msgs, map[string]any{"text": "rolling [[1d6]]"})["message"].(map[string]any)
	if got := entitiesOfType(b, "dice"); len(got) != 0 {
		t.Fatalf("a bot's dice: %v", got)
	}

	// Kept plain, nothing is rolled.
	p := owner.Must(201, "POST", msgs, map[string]any{"text": "[[1d6]]",
		"object_hints": []map[string]any{{"start": 0, "end": 7, "kind": "plain"}}})["message"].(map[string]any)
	if got := entitiesOfType(p, "dice"); len(got) != 0 {
		t.Fatalf("kept plain: %v", got)
	}
}
