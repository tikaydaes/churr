package api_test

import (
	"context"
	"fmt"
	"strings"
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

	// A custom command named /shrug.
	owner.Must(201, "POST", "/api/v1/admin/saved-replies", map[string]any{"name": "shrug", "text": "Please keep it civil."})
	menu := menuOf(bob, room)
	if menu["shrug"] != model.CommandSavedReply {
		t.Fatalf("the custom command is listed in place of the built-in: %v", menu)
	}
	m := obj(bob.Must(200, "POST", conv+room+"/commands", map[string]any{"name": "shrug"}), "message")
	if m["text"] != "Please keep it civil." {
		t.Fatalf("/shrug ran the custom command: %v", m)
	}

	// A bot's /pick, where the bot is; the built-in elsewhere.
	bot := owner.Bot("picker")
	bot.Must(200, "PUT", "/api/v1/commands", map[string]any{"commands": []any{map[string]any{"name": "pick", "description": "Picks a lunch place"}}})
	ann.Add(room, "member", bot)
	if menu := menuOf(bob, room); menu["pick"] != model.CommandBot {
		t.Fatalf("the bot's /pick: %v", menu)
	}
	if menu := menuOf(bob, other); menu["pick"] != model.CommandBuiltin {
		t.Fatalf("where the bot is not, /pick is the built-in: %v", menu)
	}
	bob.Refused(400, model.CodeInvalidValue, "POST", conv+other+"/commands", map[string]any{"name": "pick"})

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

// listedCommand is the entry of one name in a conversation's command list.
func listedCommand(t *testing.T, c *testkit.Client, convID, name string) map[string]any {
	t.Helper()
	for _, raw := range arr(c.Must(200, "GET", "/api/v1/commands?conversation_id="+convID, nil), "commands") {
		if m := raw.(map[string]any); m["name"] == name {
			return m
		}
	}
	return nil
}

// The command table (docs/DESIGN-MODULES.md section 6.5): modules' commands
// are listed with their forms and help, of a kind of their own; the admin
// orders the sources, which settles a clash in the list and in the running
// alike, and renames a module's command; a protected built-in always wins.
func TestCommandTable(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	ann, bob := ts.Member("ann"), ts.Member("bob")
	room := ann.Channel("room", false, bob)
	table := "/api/v1/admin/commands"

	// /roll and /dice are the dice module's, with their forms and help.
	roll := listedCommand(t, bob, room, "roll")
	if roll == nil || roll["kind"] != model.CommandModule || roll["module"] != "dice" {
		t.Fatalf("/roll: %v", roll)
	}
	forms := arr(roll, "forms")
	slot := arr(forms[0].(map[string]any), "slots")[0].(map[string]any)
	if len(forms) != 2 || slot["kind"] != "dice" || slot["name"] != "dice" || len(arr(forms[1].(map[string]any), "slots")) != 0 {
		t.Fatalf("the forms of /roll: %v", forms)
	}
	help := obj(roll, "help")
	if help["description"] == "" || arr(help, "examples")[1].(map[string]any)["type"] != "/roll 2d6+3" {
		t.Fatalf("the help of /roll: %v", help)
	}
	set := obj(owner.Must(200, "GET", table, nil), "settings")
	if fmt.Sprint(set["order"]) != "[saved_reply bot module builtin]" || len(arr(set, "renames")) != 0 {
		t.Fatalf("the default table: %v", set)
	}
	bob.Refused(403, model.CodeForbidden, "GET", table, nil)
	bob.Refused(403, model.CodeForbidden, "PATCH", table, map[string]any{"order": []string{"module", "saved_reply", "bot", "builtin"}})

	// A custom command named /roll comes first by default, in the list and
	// when run.
	owner.Must(201, "POST", "/api/v1/admin/saved-replies", map[string]any{"name": "roll", "text": "No dice here."})
	if menuOf(bob, room)["roll"] != model.CommandSavedReply {
		t.Fatal("the custom /roll is not listed")
	}
	if m := obj(bob.Must(200, "POST", conv+room+"/commands", map[string]any{"name": "roll"}), "message"); m["text"] != "No dice here." {
		t.Fatalf("/roll ran %v", m)
	}

	// Modules first: the module's /roll is listed and runs.
	owner.Must(200, "PATCH", table, map[string]any{"order": []string{"module", "saved_reply", "bot", "builtin"}})
	if menuOf(bob, room)["roll"] != model.CommandModule {
		t.Fatal("the module's /roll is not listed first")
	}
	if m := rolled(bob, room, "roll", "d6"); m["view"] == nil {
		t.Fatalf("/roll did not roll: %v", m)
	}

	// Renamed, it answers to its new name, and its help and the help of
	// /dice say so; /roll is the custom command again.
	owner.Refused(409, model.CodeNameTaken, "PATCH", table, map[string]any{"renames": []any{map[string]any{"module": "dice", "command": "roll", "name": "ban"}}})
	owner.Refused(409, model.CodeNameTaken, "PATCH", table, map[string]any{"renames": []any{map[string]any{"module": "dice", "command": "roll", "name": "dice"}}})
	owner.Refused(400, model.CodeInvalidValue, "PATCH", table, map[string]any{"renames": []any{map[string]any{"module": "dice", "command": "nothing", "name": "r"}}})
	owner.Refused(400, model.CodeInvalidValue, "PATCH", table, map[string]any{"order": []string{"module", "module", "bot", "builtin"}})
	set = obj(owner.Must(200, "PATCH", table, map[string]any{"renames": []any{map[string]any{"module": "dice", "command": "roll", "name": "r"}}}), "settings")
	if len(arr(set, "renames")) != 1 || fmt.Sprint(set["order"]) != "[module saved_reply bot builtin]" {
		t.Fatalf("renamed: %v", set)
	}
	menu := menuOf(bob, room)
	if menu["r"] != model.CommandModule || menu["roll"] != model.CommandSavedReply {
		t.Fatalf("after the rename: %v", menu)
	}
	if ex := arr(obj(listedCommand(t, bob, room, "r"), "help"), "examples")[0].(map[string]any); ex["type"] != "/r" {
		t.Fatalf("the help of /r: %v", ex)
	}
	if d := obj(listedCommand(t, bob, room, "dice"), "help"); !strings.Contains(d["description"].(string), "/r 4d6") || strings.Contains(d["description"].(string), "{cmd") {
		t.Fatalf("the help of /dice: %v", d)
	}
	if m := rolled(bob, room, "r", "2d4"); m["view"] == nil {
		t.Fatalf("/r did not roll: %v", m)
	}
	if m := obj(bob.Must(200, "POST", conv+room+"/commands", map[string]any{"name": "roll"}), "message"); m["text"] != "No dice here." {
		t.Fatalf("/roll after the rename ran %v", m)
	}

	// Built-ins first: a replaceable built-in wins over the custom command
	// of its name, and is the app's to carry out; a protected one wins
	// whatever the order.
	owner.Must(201, "POST", "/api/v1/admin/saved-replies", map[string]any{"name": "shrug", "text": "Please keep it civil."})
	if menuOf(bob, room)["shrug"] != model.CommandSavedReply {
		t.Fatal("the custom /shrug is not listed before the built-ins come first")
	}
	owner.Must(200, "PATCH", table, map[string]any{"order": []string{"builtin", "module", "saved_reply", "bot"}, "renames": []any{}})
	if menu := menuOf(bob, room); menu["shrug"] != model.CommandBuiltin || menu["roll"] != model.CommandModule || menu["kick"] != model.CommandBuiltin {
		t.Fatalf("built-ins first: %v", menu)
	}
	bob.Refused(400, model.CodeInvalidValue, "POST", conv+room+"/commands", map[string]any{"name": "shrug"})

	// A bot is not offered the dice, which answer people only.
	bot := owner.Bot("dealer")
	ann.Add(room, "member", bot)
	if menu := menuOf(bot, room); menu["dice"] != "" {
		t.Fatalf("a bot's menu: %v", menu)
	}
}

// A protected built-in (every one but the fun and games) can never be
// replaced: its name and its aliases are
// refused to saved replies, custom commands and bot commands, naming the
// command, and any made before it was protected are ignored.
func TestProtectedCommands(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	ann, bob := ts.Member("ann"), ts.Member("bob")
	room := ann.Channel("room", false, bob)
	const why = " is a built-in command that cannot be replaced."

	for _, name := range []string{"ban", "kline", "msg", "part", "me", "poll", "topic", "remind", "who"} {
		e := owner.Refused(409, model.CodeNameTaken, "POST", "/api/v1/admin/saved-replies", map[string]any{"name": name, "text": "x"})
		if e["message"] != "/"+name+why {
			t.Errorf("custom /%s: %v", name, e)
		}
		ann.Refused(409, model.CodeNameTaken, "POST", "/api/v1/saved-replies", map[string]any{"name": name, "text": "x"})
	}
	mine := obj(ann.Must(201, "POST", "/api/v1/saved-replies", map[string]any{"name": "hi", "text": "x"}), "saved_reply")
	ann.Refused(409, model.CodeNameTaken, "PATCH", "/api/v1/saved-replies/"+mine["id"].(string), map[string]any{"name": "away"})
	custom := obj(owner.Must(201, "POST", "/api/v1/admin/saved-replies", map[string]any{"name": "rules", "text": "x"}), "saved_reply")
	owner.Refused(409, model.CodeNameTaken, "PATCH", "/api/v1/admin/saved-replies/"+custom["id"].(string), map[string]any{"name": "unexile"})
	bot := owner.Bot("mod")
	e := bot.Refused(409, model.CodeNameTaken, "PUT", "/api/v1/commands", map[string]any{"commands": []any{map[string]any{"name": "deploy", "description": "Deploys"}, map[string]any{"name": "mute", "description": "Mutes"}}})
	if e["message"] != "/mute"+why {
		t.Errorf("a bot's /mute: %v", e)
	}
	// A replaceable one is still allowed.
	bot.Must(200, "PUT", "/api/v1/commands", map[string]any{"commands": []any{map[string]any{"name": "roll", "description": "Rolls"}}})

	// Made before the name was protected: not listed and not run, so the
	// built-in wins; its text can still be changed until it is renamed.
	ctx := context.Background()
	old := model.SavedReply{Name: "kick", Text: "Gotcha"}
	if err := ts.Server.Store.Scheduled().CreateReply(ctx, &old); err != nil {
		t.Fatal(err)
	}
	if err := ts.Server.Store.Bots().SetCommands(ctx, bot.ID, []model.Command{{Name: "whois", Kind: model.CommandBot, Description: "Looks up", BotID: bot.ID}}); err != nil {
		t.Fatal(err)
	}
	ann.Add(room, "member", bot)
	if menu := menuOf(bob, room); menu["kick"] != model.CommandBuiltin || menu["whois"] != model.CommandBuiltin {
		t.Fatalf("the built-ins win: %v", menu)
	}
	bob.Refused(400, model.CodeInvalidValue, "POST", conv+room+"/commands", map[string]any{"name": "kick"})
	bob.Refused(400, model.CodeInvalidValue, "POST", conv+room+"/commands", map[string]any{"name": "whois"})
	owner.Must(200, "PATCH", "/api/v1/admin/saved-replies/"+old.ID, map[string]any{"text": "Renamed soon"})
	owner.Must(200, "PATCH", "/api/v1/admin/saved-replies/"+old.ID, map[string]any{"name": "kick-info"})
}
