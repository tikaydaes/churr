package api_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

func menuOf(c *testkit.Client, convID string) map[string]string {
	out := map[string]string{}
	for _, raw := range c.Must(200, "GET", "/api/v1/commands?conversation_id="+convID, nil)["commands"].([]any) {
		m := raw.(map[string]any)
		out[m["name"].(string)] = m["kind"].(string)
	}
	return out
}

// A community's command with a built-in's name wins on this server, and the
// built-in of that name is hidden (ruling R72, model.HiddenBuiltins). The
// server bans are offered to admins alone.
func TestCommandNameClash(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	ann, bob := ts.Member("ann"), ts.Member("bob")
	admin := ts.Admin("adm")
	room := ann.Channel("room", false, bob)
	other := ann.Channel("other", false, bob)

	// A custom command named /kick.
	owner.Must(201, "POST", "/api/v1/admin/saved-replies", map[string]any{"name": "kick", "text": "Please keep it civil."})
	menu := menuOf(bob, room)
	if menu["kick"] != model.CommandSavedReply {
		t.Fatalf("the custom command is listed in place of the built-in: %v", menu)
	}
	m := obj(bob.Must(200, "POST", conv+room+"/commands", map[string]any{"name": "kick"}), "message")
	if m["text"] != "Please keep it civil." {
		t.Fatalf("/kick ran the custom command: %v", m)
	}

	// A bot's /topic, where the bot is; the built-in elsewhere.
	bot := owner.Bot("topics")
	bot.Must(200, "PUT", "/api/v1/commands", map[string]any{"commands": []any{map[string]any{"name": "topic", "description": "Suggests a topic"}}})
	ann.Add(room, "member", bot)
	if menu := menuOf(bob, room); menu["topic"] != model.CommandBot {
		t.Fatalf("the bot's /topic: %v", menu)
	}
	if menu := menuOf(bob, other); menu["topic"] != model.CommandBuiltin {
		t.Fatalf("where the bot is not, /topic is the built-in: %v", menu)
	}
	bob.Refused(400, model.CodeInvalidValue, "POST", conv+other+"/commands", map[string]any{"name": "topic"})

	// A personal saved reply named like a built-in wins for its owner.
	ann.Must(201, "POST", "/api/v1/saved-replies", map[string]any{"name": "flip", "text": "(flips the table politely)"})
	if menuOf(ann, room)["flip"] != model.CommandSavedReply || menuOf(bob, room)["flip"] != model.CommandBuiltin {
		t.Fatal("a personal /flip is ann's alone")
	}

	// /exile and /unexile are in the menu of admins only; /ban and /unban
	// are for everyone, and refused by the server where they may not.
	if menu := menuOf(bob, room); menu["exile"] != "" || menu["unexile"] != "" || menu["ban"] != model.CommandBuiltin || menu["unban"] != model.CommandBuiltin {
		t.Fatalf("a member's menu: %v", menu)
	}
	if menu := menuOf(admin, room); menu["exile"] != model.CommandBuiltin || menu["unexile"] != model.CommandBuiltin || menu["kline"] != "" {
		t.Fatalf("an admin's menu: %v", menu)
	}
}
