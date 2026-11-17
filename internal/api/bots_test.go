package api_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	stdpng "image/png"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/bots"
	"github.com/tikaydaes/churr/internal/live"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Tests of bots, their commands and interactions, and webhooks
// (docs/BOTS.md). Cases of docs/PERMISSIONS.md section 17 are cited by
// number.

// hookSend posts a JSON body to a webhook address and decodes the answer.
func hookSend(ts *testkit.TestServer, path string, body any, header http.Header) (int, http.Header, map[string]any) {
	raw, _ := json.Marshal(body)
	if header == nil {
		header = http.Header{}
	}
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/json")
	}
	status, hd, resp := ts.Raw("POST", path, header, raw)
	out := map[string]any{}
	if len(resp) > 0 && json.Unmarshal(resp, &out) != nil {
		out = map[string]any{"_raw": string(resp)}
	}
	return status, hd, out
}

// hookRefused posts to a webhook address and fails unless it is refused
// with this status and code, in Churr's error shape.
func hookRefused(t *testing.T, ts *testkit.TestServer, status int, code model.Code, path string, body any) {
	t.Helper()
	got, _, out := hookSend(ts, path, body, nil)
	if got != status || obj(out, "error")["code"] != string(code) {
		t.Fatalf("POST %s = %d %v, want %d %s", path, got, out, status, code)
	}
}

// hookPosted posts to a webhook's native address and returns what the
// answer says of the message.
func hookPosted(t *testing.T, ts *testkit.TestServer, path string, body any) map[string]any {
	t.Helper()
	status, _, out := hookSend(ts, path, body, nil)
	m := obj(out, "message")
	if status != 200 || m["id"] == nil || m["seq"] == nil || m["created_at"] == nil || len(out) != 1 || len(m) != 3 {
		t.Fatalf("POST %s = %d %v, want 200 with the message named and nothing else", path, status, out)
	}
	return m
}

// botsPNG is a small PNG image.
func botsPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	stdpng.Encode(&buf, img)
	return buf.Bytes()
}

type botsPart struct {
	name, filename string
	data           []byte
}

// botsMultipart builds a multipart body.
func botsMultipart(parts ...botsPart) (http.Header, []byte) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		h := textproto.MIMEHeader{}
		disposition := `form-data; name="` + p.name + `"`
		if p.filename != "" {
			disposition += `; filename="` + p.filename + `"`
		}
		h.Set("Content-Disposition", disposition)
		w, _ := mw.CreatePart(h)
		w.Write(p.data)
	}
	mw.Close()
	return http.Header{"Content-Type": {mw.FormDataContentType()}}, buf.Bytes()
}

// messageOf fetches a message as an account sees it.
func messageOf(c *testkit.Client, id string) map[string]any {
	return obj(c.Must(200, "GET", "/api/v1/messages/"+id, nil), "message")
}

func TestBotAdmin(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")

	// Case 88: a member with no admin role creates no bot.
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/bots", map[string]any{"username": "helper", "display_name": "Helper"})
	ann.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/bots", nil)

	// A token cannot make a bot: that needs a fresh check.
	ts.Owner.Token().Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/bots", map[string]any{"username": "helper", "display_name": "Helper"})

	ts.Owner.Refused(400, model.CodeInvalidRequest, "POST", "/api/v1/admin/bots", map[string]any{"username": "helper"})
	ts.Owner.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/bots", map[string]any{"username": "Not Valid", "display_name": "x"})
	ts.Owner.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/bots",
		map[string]any{"username": "helper", "display_name": "Helper", "description": strings.Repeat("d", 251)})
	ts.Owner.Refused(409, model.CodeNameTaken, "POST", "/api/v1/admin/bots", map[string]any{"username": "ann", "display_name": "Ann again"})

	made := ts.Owner.Must(201, "POST", "/api/v1/admin/bots", map[string]any{"username": "helper", "display_name": "Helper", "description": "Helps"})
	bot := obj(made, "bot")
	id := obj(bot, "account")["id"].(string)
	secret := made["secret"].(string)
	if obj(bot, "account")["role"] != "bot" || bot["description"] != "Helps" || bot["created_by_id"] != string(ts.Owner.ID) || bot["webhook_only"] != false {
		t.Fatalf("the bot as made: %v", bot)
	}
	if !strings.HasPrefix(secret, "churr_bot_") || len(secret) != len("churr_bot_")+43 {
		t.Fatalf("a bot token is churr_bot_ and 256 bits in base64url: %q", secret)
	}
	first := obj(made, "token")
	if first["id"] == nil || first["label"] == "" || first["created_at"] == nil {
		t.Fatalf("the first token: %v", first)
	}

	// The bot signs in with its token and is who it is.
	helper := ts.Owner.Bot("second")
	me := obj(helper.Must(200, "GET", "/api/v1/me", nil), "account")
	if me["role"] != "bot" || me["username"] != "second" {
		t.Fatalf("a bot asks who it is: %v", me)
	}
	// A bot cannot manage bots, nor make or revoke its own tokens.
	helper.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/bots", nil)
	helper.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/bots/"+string(helper.ID)+"/tokens", map[string]any{})

	got := obj(ts.Owner.Must(200, "GET", "/api/v1/admin/bots/"+id, nil), "bot")
	if obj(got, "account")["username"] != "helper" {
		t.Fatalf("getBot: %v", got)
	}
	// An account that is not a bot is not found among the bots.
	ts.Owner.Refused(404, model.CodeNotFound, "GET", "/api/v1/admin/bots/"+string(ann.ID), nil)
	ts.Owner.Refused(404, model.CodeNotFound, "GET", "/api/v1/admin/bots/not-an-id", nil)

	listed := arr(ts.Owner.Must(200, "GET", "/api/v1/admin/bots", nil), "bots")
	if len(listed) != 2 {
		t.Fatalf("two bots listed, got %d", len(listed))
	}
	page := ts.Owner.Must(200, "GET", "/api/v1/admin/bots?limit=1", nil)
	if len(arr(page, "bots")) != 1 || page["next_cursor"] == nil {
		t.Fatalf("a page of one with more to come: %v", page)
	}

	// Changing a bot.
	changed := obj(ts.Owner.Must(200, "PATCH", "/api/v1/admin/bots/"+id, map[string]any{"display_name": "Helper two", "description": "Helps more"}), "bot")
	if obj(changed, "account")["display_name"] != "Helper two" || changed["description"] != "Helps more" {
		t.Fatalf("updateBot: %v", changed)
	}
	ts.Owner.Refused(409, model.CodeNameTaken, "PATCH", "/api/v1/admin/bots/"+id, map[string]any{"username": "ann"})
	ts.Owner.Refused(400, model.CodeInvalidRequest, "PATCH", "/api/v1/admin/bots/"+id, map[string]any{"role": "admin"})
	ann.Refused(403, model.CodeForbidden, "PATCH", "/api/v1/admin/bots/"+id, map[string]any{"description": "x"})

	// Its avatar.
	status, out := ts.Owner.SendForm("PUT", "/api/v1/admin/bots/"+id+"/avatar", "file", "a.png", botsPNG())
	if status != 200 || obj(out, "bot", "account")["avatar_file_id"] == nil {
		t.Fatalf("setBotAvatar = %d %v", status, out)
	}
	if status, out := ts.Owner.SendForm("PUT", "/api/v1/admin/bots/"+id+"/avatar", "file", "a.txt", []byte("not an image")); status != 400 {
		t.Fatalf("an avatar that is not an image = %d %v", status, out)
	}
	if status, _ := ann.SendForm("PUT", "/api/v1/admin/bots/"+id+"/avatar", "file", "a.png", botsPNG()); status != 403 {
		t.Fatalf("a member sets a bot's avatar = %d", status)
	}

	// Tokens: several, so one can be replaced without downtime.
	base := "/api/v1/admin/bots/" + id + "/tokens"
	ann.Refused(403, model.CodeForbidden, "POST", base, map[string]any{}) // case 88
	ann.Refused(403, model.CodeForbidden, "GET", base, nil)
	ts.Owner.Token().Refused(403, model.CodeForbidden, "POST", base, map[string]any{})
	second := ts.Owner.Must(201, "POST", base, map[string]any{"label": "deploy 2"})
	if obj(second, "token")["label"] != "deploy 2" || !strings.HasPrefix(second["secret"].(string), "churr_bot_") {
		t.Fatalf("createBotToken: %v", second)
	}
	tokens := arr(ts.Owner.Must(200, "GET", base, nil), "tokens")
	if len(tokens) != 2 {
		t.Fatalf("two tokens listed, got %v", tokens)
	}
	for _, tok := range tokens {
		if _, leaked := tok.(map[string]any)["secret"]; leaked {
			t.Fatal("a listed token shows its secret")
		}
	}
	// Revoking a token closes, at once, the connection opened with it.
	old := &testkit.Client{Bearer: secret}
	*old = *helper
	old.Bearer, old.ID = secret, model.AccountID(id)
	stream, _ := old.Events(nil)
	ts.Owner.Must(204, "DELETE", base+"/"+first["id"].(string), nil)
	if code := stream.CloseCode(5 * time.Second); code != live.CloseCredential {
		t.Fatalf("the revoked token's connection closed with %d, want %d", code, live.CloseCredential)
	}
	old.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	ts.Owner.Refused(404, model.CodeNotFound, "DELETE", base+"/"+first["id"].(string), nil)
	ts.Owner.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/admin/bots/"+string(helper.ID)+"/tokens/"+obj(second, "token")["id"].(string), nil)
	// The other token still works.
	old.Bearer = second["secret"].(string)
	old.Must(200, "GET", "/api/v1/me", nil)

	// At most ten live tokens.
	for i := 0; i < 9; i++ {
		ts.Owner.Must(201, "POST", base, map[string]any{})
	}
	ts.Owner.Refused(409, model.CodeConflict, "POST", base, map[string]any{})

	// Every one of these actions is written to the audit log.
	for _, action := range []string{"bot.created", "bot.updated", "token.created", "token.revoked"} {
		entries, _, err := ts.Server.Store.Audit().List(context.Background(), store.AuditFilter{Action: action}, store.Page{})
		if err != nil || len(entries) == 0 {
			t.Errorf("no audit entry for %s (%v)", action, err)
		}
	}

	// Removing the bot ends its tokens (case 134) and its place among
	// the bots.
	ann.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/bots/"+id, nil)
	ts.Owner.Must(204, "DELETE", "/api/v1/admin/bots/"+id, nil)
	old.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	ts.Owner.Refused(404, model.CodeNotFound, "GET", "/api/v1/admin/bots/"+id, nil)
	ts.Owner.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/admin/bots/"+id, nil)
}

func TestBotReach(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	pub := ann.Channel("pub", false)
	priv := ann.Channel("priv", true)
	bot := ts.Owner.Bot("deploybot")

	// Cases 9 and 11: a bot not in a channel sees nothing of it, public
	// ones included; case 81: it cannot join by itself.
	bot.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+pub, nil)
	bot.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv, nil)
	bot.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+pub+"/messages", nil)
	bot.Refused(404, model.CodeNotFound, "POST", "/api/v1/conversations/"+pub+"/join", nil)
	// Browsing shows a bot no channel it is not in, public ones included.
	if status, out := bot.Do("GET", "/api/v1/channels", nil); status != 403 && (status != 200 || len(arr(out, "conversations")) != 0) {
		t.Fatalf("a bot browsing = %d %v", status, out)
	}
	bot.Refused(403, model.CodeForbidden, "POST", "/api/v1/channels", map[string]any{"name": "mine", "private": false})
	if convs := arr(bot.Must(200, "GET", "/api/v1/conversations", nil), "conversations"); len(convs) != 0 {
		t.Fatalf("a new bot is in no conversation: %v", convs)
	}

	// Case 85: a bot added as reader cannot post.
	ann.Add(pub, "reader", bot)
	bot.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+pub+"/messages", map[string]any{"text": "hello"})
	bot.Must(200, "GET", "/api/v1/conversations/"+pub+"/messages", nil)

	// At level member it posts, and only a bot posts cards.
	ann.Add(priv, "member", bot)
	card := map[string]any{"color": "green", "title": "Deployed", "text": "All good @bob"}
	posted := bot.Must(201, "POST", "/api/v1/conversations/"+priv+"/messages", map[string]any{"text": "", "cards": []any{card}})
	if cards := arr(obj(posted, "message"), "cards"); len(cards) != 1 || cards[0].(map[string]any)["title"] != "Deployed" {
		t.Fatalf("a bot's card: %v", posted)
	}
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+priv+"/messages", map[string]any{"text": "x", "cards": []any{card}})

	// Ruling R7: a bot starts an ad-hoc channel only with accounts it can
	// already see. It shares a channel with ann and not with bob.
	bot.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(ann.ID)}})
	bot.Refused(404, model.CodeNotFound, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID)}})

	// A bot that leaves a channel gets nothing more from it (case 86).
	stream, _ := bot.Events(nil)
	ann.Post(priv, "while the bot is here")
	stream.Expect("message.created")
	bot.Must(204, "POST", "/api/v1/conversations/"+priv+"/leave", nil)
	stream.Expect("member.left")
	ann.Post(priv, "after the bot left")
	bot.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv, nil)
	stream.ExpectNone(300 * time.Millisecond)
}

func TestBotCommands(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	bot, other := ts.Owner.Bot("deploybot"), ts.Owner.Bot("otherbot")
	list := func(names ...string) map[string]any {
		cmds := []any{}
		for _, n := range names {
			cmds = append(cmds, map[string]any{"name": n, "description": "Does " + n, "hint": "<what>"})
		}
		return map[string]any{"commands": cmds}
	}

	// Only a bot registers commands.
	ann.Refused(403, model.CodeForbidden, "PUT", "/api/v1/commands", list("deploy"))
	ts.Owner.Refused(403, model.CodeForbidden, "PUT", "/api/v1/commands", list("deploy"))

	out := bot.Must(200, "PUT", "/api/v1/commands", list("deploy", "build"))
	cmds := arr(out, "commands")
	if len(cmds) != 2 || cmds[0].(map[string]any)["name"] != "build" || cmds[0].(map[string]any)["kind"] != "bot" ||
		cmds[0].(map[string]any)["bot_id"] != string(bot.ID) || cmds[1].(map[string]any)["hint"] != "<what>" {
		t.Fatalf("setCommands: %v", out)
	}
	// The same list again is fine: a bot sends it every time it starts.
	bot.Must(200, "PUT", "/api/v1/commands", list("deploy", "build"))

	bot.Refused(400, model.CodeInvalidRequest, "PUT", "/api/v1/commands", map[string]any{})
	bot.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/commands", list("Not Valid"))
	bot.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/commands", list("twice", "twice"))
	bot.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/commands", map[string]any{"commands": []any{map[string]any{"name": "bare"}}})
	bot.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/commands",
		map[string]any{"commands": []any{map[string]any{"name": "long", "description": strings.Repeat("d", 101)}}})
	many := []string{}
	for i := 0; i < 51; i++ {
		many = append(many, "c"+strings.Repeat("x", i%20)+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	bot.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/commands", list(many...))

	// A name is unique on the server: not a held-back name, not another
	// bot's. A built-in's name may be taken, and the bot's command then
	// wins where the bot is (ruling R72; commands_clash_test.go).
	for _, taken := range []string{"help", "later", "status"} {
		e := bot.Refused(409, model.CodeNameTaken, "PUT", "/api/v1/commands", list("deploy", taken))
		if msg, _ := e["message"].(string); !strings.Contains(msg, "/"+taken) {
			t.Errorf("the refusal names the command: %v", e)
		}
	}
	other.Refused(409, model.CodeNameTaken, "PUT", "/api/v1/commands", list("kudos", "deploy"))
	if left := arr(obj(ts.Owner.Must(200, "GET", "/api/v1/admin/bots/"+string(other.ID), nil), "bot"), "commands"); len(left) != 0 {
		t.Fatalf("a refused list registers nothing: %v", left)
	}
	if kept := arr(obj(ts.Owner.Must(200, "GET", "/api/v1/admin/bots/"+string(bot.ID), nil), "bot"), "commands"); len(kept) != 2 {
		t.Fatalf("the bot's commands as an admin sees them: %v", kept)
	}

	// The list replaces the list; an empty one removes them all, and the
	// names are free again.
	if got := arr(bot.Must(200, "PUT", "/api/v1/commands", list("deploy")), "commands"); len(got) != 1 {
		t.Fatalf("replaced: %v", got)
	}
	other.Must(200, "PUT", "/api/v1/commands", list("build"))
	if got := arr(bot.Must(200, "PUT", "/api/v1/commands", list()), "commands"); len(got) != 0 {
		t.Fatalf("emptied: %v", got)
	}
	other.Must(200, "PUT", "/api/v1/commands", list("build", "deploy"))
}

func TestInteractions(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, eve := ts.Member("ann"), ts.Member("bob"), ts.Member("eve")
	ops := ann.Channel("ops", false, bob)
	elsewhere := ann.Channel("elsewhere", false)
	bot, other := ts.Owner.Bot("deploybot"), ts.Owner.Bot("otherbot")
	ann.Add(ops, "member", bot)
	bot.Must(200, "PUT", "/api/v1/commands", map[string]any{"commands": []any{
		map[string]any{"name": "deploy", "description": "Deploy a service", "hint": "<service> to <environment>"}}})
	run := "/api/v1/conversations/" + ops + "/commands"

	// The command is offered where the bot is, and nowhere else.
	offered := func(conv string) bool {
		for _, c := range arr(ann.Must(200, "GET", "/api/v1/commands?conversation_id="+conv, nil), "commands") {
			if c.(map[string]any)["name"] == "deploy" && c.(map[string]any)["bot_id"] == string(bot.ID) {
				return true
			}
		}
		return false
	}
	if !offered(ops) || offered(elsewhere) {
		t.Fatal("a bot's commands are listed in the conversations the bot is in, and only there")
	}

	// A bot with no live connection: the person is told at once and
	// nothing is queued.
	ann.Refused(503, model.CodeUnavailable, "POST", run, map[string]any{"name": "deploy", "text": "api"})

	botStream, _ := bot.Events(nil)
	annStream, _ := ann.Events(nil)
	bobStream, _ := bob.Events(nil)

	// Case 87: run where the bot is not, the bot receives nothing and
	// the asker gets not_found.
	ann.Refused(404, model.CodeNotFound, "POST", "/api/v1/conversations/"+elsewhere+"/commands", map[string]any{"name": "deploy"})
	ann.Refused(404, model.CodeNotFound, "POST", run, map[string]any{"name": "nothing"})
	eve.Refused(403, model.CodeForbidden, "POST", run, map[string]any{"name": "deploy"}) // not a member: join first
	ann.Refused(400, model.CodeInvalidValue, "POST", run, map[string]any{"name": "deploy", "text": strings.Repeat("x", 4001)})

	// Sending posts nothing: the bot gets an event, with no seq, and the
	// asker gets the interaction's identifier at once.
	out := ann.Must(200, "POST", run, map[string]any{"name": "deploy", "text": "  api to staging @bob  "})
	interaction, _ := out["interaction_id"].(string)
	if !model.ValidID(interaction) || out["message"] != nil {
		t.Fatalf("runCommand: %v", out)
	}
	e := botStream.Expect("command.invoked")
	if e["interaction_id"] != interaction || e["conversation_id"] != ops || e["account_id"] != string(ann.ID) || e["command"] != "deploy" ||
		e["text"] != "api to staging @bob" || e["seq"] != nil || e["parent_id"] != nil {
		t.Fatalf("command.invoked: %v", e)
	}
	ents := arr(e, "entities")
	if len(ents) != 1 || ents[0].(map[string]any)["type"] != "account" || ents[0].(map[string]any)["account_id"] != string(bob.ID) {
		t.Fatalf("the entities of the text after the name: %v", e["entities"])
	}
	respond := "/api/v1/interactions/" + interaction + "/responses"

	// Only the bot it was sent to can respond.
	other.Refused(404, model.CodeNotFound, "POST", respond, map[string]any{"type": "none"})
	ann.Refused(404, model.CodeNotFound, "POST", respond, map[string]any{"type": "none"})
	bot.Refused(404, model.CodeNotFound, "POST", "/api/v1/interactions/"+string(model.NewID[model.MessageID]())+"/responses", map[string]any{"type": "none"})
	bot.Refused(404, model.CodeNotFound, "POST", "/api/v1/interactions/nonsense/responses", map[string]any{"type": "none"})
	bot.Refused(400, model.CodeInvalidRequest, "POST", respond, map[string]any{})
	bot.Refused(400, model.CodeInvalidValue, "POST", respond, map[string]any{"type": "shout"})
	bot.Refused(400, model.CodeInvalidValue, "POST", respond, map[string]any{"type": "message"})
	bot.Refused(400, model.CodeInvalidValue, "POST", respond, map[string]any{"type": "private"})
	bot.Refused(400, model.CodeInvalidValue, "POST", respond, map[string]any{"type": "dialog"})
	bot.Refused(400, model.CodeInvalidValue, "POST", respond, map[string]any{"type": "private", "text": "x", "cards": []any{map[string]any{"color": "blue", "title": "t"}}})
	bot.Refused(400, model.CodeInvalidRequest, "POST", respond, map[string]any{"type": "private", "text": "x", "files": []any{}})

	// private: to the asker only, on every connection, and not stored.
	bot.Must(200, "POST", respond, map[string]any{"type": "private", "text": "Working on it… @bob",
		"cards": []any{map[string]any{"color": "yellow", "title": "Queued"}}})
	r := annStream.Expect("interaction.responded")
	resp := obj(r, "response")
	if r["interaction_id"] != interaction || r["conversation_id"] != ops || r["bot_id"] != string(bot.ID) || r["seq"] != nil ||
		resp["type"] != "private" || resp["text"] != "Working on it… @bob" || len(arr(resp, "cards")) != 1 || len(arr(resp, "entities")) != 1 {
		t.Fatalf("interaction.responded for a private response: %v", r)
	}

	// message: the answer for everyone, marked with who made the bot speak.
	posted := bot.Must(200, "POST", respond, map[string]any{"type": "message", "message": map[string]any{
		"text": "", "cards": []any{map[string]any{"color": "yellow", "title": "Deploy api to staging?", "text": "Asked by @ann"}}}})
	msg := obj(posted, "message")
	via := obj(msg, "via")
	if msg["author_id"] != string(bot.ID) || via["kind"] != "command" || via["name"] != "deploy" || via["account_id"] != string(ann.ID) {
		t.Fatalf("a message response: %v", msg)
	}
	r = annStream.Expect("interaction.responded")
	if obj(r, "response")["type"] != "message" || obj(r, "response")["message_id"] != msg["id"] {
		t.Fatalf("interaction.responded for a message response: %v", r)
	}
	created := bobStream.Expect("message.created")
	if obj(created, "message", "via")["account_id"] != string(ann.ID) {
		t.Fatalf("everyone sees who asked: %v", created)
	}

	// none, twice, and then the five responses are used up.
	bot.Must(200, "POST", respond, map[string]any{"type": "none"})
	if r = annStream.Expect("interaction.responded"); obj(r, "response")["type"] != "none" {
		t.Fatalf("interaction.responded for none: %v", r)
	}
	bot.Must(200, "POST", respond, map[string]any{"type": "none"})
	annStream.Expect("interaction.responded")
	// A refused response does not use one up.
	bot.Refused(400, model.CodeInvalidValue, "POST", respond, map[string]any{"type": "private", "cards": []any{map[string]any{"footer": "nothing else"}}})
	bot.Must(200, "POST", respond, map[string]any{"type": "none"})
	annStream.Expect("interaction.responded")
	bot.Refused(404, model.CodeNotFound, "POST", respond, map[string]any{"type": "none"})

	// Typed in a thread: the event says so, and a message response goes
	// into that thread unless it says otherwise.
	parent := ann.Post(ops, "thread parent")["id"].(string)
	inThread := ann.Must(200, "POST", run, map[string]any{"name": "deploy", "text": "api", "parent_id": parent})["interaction_id"].(string)
	if e = botStream.Expect("command.invoked"); e["parent_id"] != parent || len(arr(e, "entities")) != 0 || e["entities"] == nil {
		t.Fatalf("command.invoked in a thread: %v", e)
	}
	reply := obj(bot.Must(200, "POST", "/api/v1/interactions/"+inThread+"/responses",
		map[string]any{"type": "message", "message": map[string]any{"text": "Done"}}), "message")
	if reply["parent_id"] != parent {
		t.Fatalf("the answer goes into the thread: %v", reply)
	}
	if r = annStream.Expect("interaction.responded"); r["parent_id"] != parent {
		t.Fatalf("interaction.responded names the thread: %v", r)
	}
	ann.Refused(404, model.CodeNotFound, "POST", run, map[string]any{"name": "deploy", "parent_id": string(model.NewID[model.MessageID]())})

	// A bot at level reader can answer privately but not for everyone.
	quiet := ann.Channel("quiet", false)
	ann.Add(quiet, "reader", bot)
	q := ann.Must(200, "POST", "/api/v1/conversations/"+quiet+"/commands", map[string]any{"name": "deploy"})["interaction_id"].(string)
	botStream.Expect("command.invoked")
	bot.Refused(403, model.CodeForbidden, "POST", "/api/v1/interactions/"+q+"/responses", map[string]any{"type": "message", "message": map[string]any{"text": "no"}})
	bot.Must(200, "POST", "/api/v1/interactions/"+q+"/responses", map[string]any{"type": "private", "text": "Only you"})
	annStream.Expect("interaction.responded")

	// Other members of the conversation saw none of it.
	for {
		f, ok := bobStream.Next(200 * time.Millisecond)
		if !ok {
			break
		}
		if f["type"] == "command.invoked" || f["type"] == "interaction.responded" || f["type"] == "form.answered" {
			t.Fatalf("an event addressed to one account reached another: %v", f)
		}
	}
}

func TestDialogs(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ops := ann.Channel("ops", false, bob)
	bot := ts.Owner.Bot("deploybot")
	ann.Add(ops, "member", bot)
	bot.Must(200, "PUT", "/api/v1/commands", map[string]any{"commands": []any{map[string]any{"name": "rollback", "description": "Roll back"}}})
	botStream, _ := bot.Events(nil)
	annStream, _ := ann.Events(nil)
	ask := func() string {
		t.Helper()
		id := ann.Must(200, "POST", "/api/v1/conversations/"+ops+"/commands", map[string]any{"name": "rollback"})["interaction_id"].(string)
		botStream.Expect("command.invoked")
		return "/api/v1/interactions/" + id + "/responses"
	}
	form := map[string]any{"title": "Roll back api",
		"fields":  []any{map[string]any{"id": "reason", "type": "longtext", "label": "Why?", "required": true}},
		"buttons": []any{map[string]any{"id": "ok", "label": "Roll back", "style": "danger"}}}

	respond := ask()
	// The settings of a form are refused in a dialog.
	for _, setting := range []string{"anonymous", "results", "allow_added_options", "closes_at", "answerer_group_id", "action"} {
		bad := map[string]any{}
		for k, v := range form {
			bad[k] = v
		}
		bad[setting] = "x"
		bot.Refused(400, model.CodeInvalidRequest, "POST", respond, map[string]any{"type": "dialog", "form": bad})
	}
	bot.Refused(400, model.CodeInvalidValue, "POST", respond, map[string]any{"type": "dialog",
		"form": map[string]any{"fields": []any{map[string]any{"id": "Bad Id", "type": "text", "label": "x"}}}})

	out := bot.Must(200, "POST", respond, map[string]any{"type": "dialog", "form": form})
	d := obj(out, "dialog")
	dialog, _ := d["id"].(string)
	if !model.ValidID(dialog) || d["bot_id"] != string(bot.ID) || d["conversation_id"] != ops || d["title"] != "Roll back api" || len(arr(d, "fields")) != 1 {
		t.Fatalf("a dialog response: %v", out)
	}
	r := annStream.Expect("interaction.responded")
	if obj(r, "response")["type"] != "dialog" || obj(r, "response", "dialog")["id"] != dialog {
		t.Fatalf("interaction.responded for a dialog: %v", r)
	}
	// One dialog per interaction.
	bot.Refused(409, model.CodeConflict, "POST", respond, map[string]any{"type": "dialog", "form": form})

	// Only the person it was shown to can answer it.
	submit := "/api/v1/dialogs/" + dialog + "/submit"
	bob.Refused(404, model.CodeNotFound, "POST", submit, map[string]any{"answers": map[string]any{"reason": "x"}})
	bob.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/dialogs/"+dialog, nil)
	ann.Refused(400, model.CodeInvalidRequest, "POST", submit, map[string]any{"button_id": "ok"})
	// The answer is checked against the form before the bot sees it.
	ann.Refused(400, model.CodeInvalidValue, "POST", submit, map[string]any{"answers": map[string]any{}})
	ann.Refused(400, model.CodeInvalidValue, "POST", submit, map[string]any{"answers": map[string]any{"reason": "x", "other": "y"}})
	ann.Refused(400, model.CodeInvalidValue, "POST", submit, map[string]any{"button_id": "nope", "answers": map[string]any{"reason": "x"}})

	answered := ann.Must(200, "POST", submit, map[string]any{"button_id": "ok", "answers": map[string]any{"reason": "Hotfix for the login bug"}})
	next, _ := answered["interaction_id"].(string)
	e := botStream.Expect("form.answered")
	if e["interaction_id"] != next || !model.ValidID(next) || e["dialog"] != true || e["form_id"] != dialog || e["message_id"] != nil ||
		e["account_id"] != string(ann.ID) || e["button_id"] != "ok" || obj(e, "answers")["reason"] != "Hotfix for the login bug" ||
		e["conversation_id"] != ops || e["seq"] != nil {
		t.Fatalf("form.answered for a dialog: %v", e)
	}
	// Answered: the dialog is gone.
	ann.Refused(404, model.CodeNotFound, "POST", submit, map[string]any{"answers": map[string]any{"reason": "again"}})
	// The bot can respond to the answer in turn, with a second step.
	step2 := bot.Must(200, "POST", "/api/v1/interactions/"+next+"/responses", map[string]any{"type": "dialog",
		"form": map[string]any{"fields": []any{}, "buttons": []any{map[string]any{"id": "sure", "label": "I am sure"}}}})
	annStream.Expect("interaction.responded")
	second := obj(step2, "dialog")["id"].(string)

	// Dismissing sends the bot nothing.
	ann.Must(204, "DELETE", "/api/v1/dialogs/"+second, nil)
	ann.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/dialogs/"+second, nil)
	ann.Refused(404, model.CodeNotFound, "POST", "/api/v1/dialogs/"+second+"/submit", map[string]any{"answers": map[string]any{}})
	ann.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/dialogs/nonsense", nil)

	// A dialog whose bot has gone away stays, so the person can try
	// again when it is back.
	third := obj(bot.Must(200, "POST", ask(), map[string]any{"type": "dialog", "form": form}), "dialog")["id"].(string)
	botStream.ExpectNone(200 * time.Millisecond) // nothing came of the dismissal; this also ends the connection
	deadline := time.Now().Add(5 * time.Second)
	for ts.Server.Live.Connections(bot.ID) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	ann.Refused(503, model.CodeUnavailable, "POST", "/api/v1/dialogs/"+third+"/submit", map[string]any{"answers": map[string]any{"reason": "x"}})
	again, _ := bot.Events(nil)
	ann.Must(200, "POST", "/api/v1/dialogs/"+third+"/submit", map[string]any{"answers": map[string]any{"reason": "x"}})
	if e = again.Expect("form.answered"); e["button_id"] != nil {
		t.Fatalf("submitted without a button: %v", e)
	}
}

func TestBotForms(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ops := ann.Channel("ops", false, bob)
	bot, other := ts.Owner.Bot("deploybot"), ts.Owner.Bot("otherbot")
	ann.Add(ops, "member", bot, other)
	buttons := map[string]any{"fields": []any{}, "action": map[string]any{"type": "bot"},
		"buttons": []any{map[string]any{"id": "go", "label": "Deploy", "style": "primary"}, map[string]any{"id": "cancel", "label": "Cancel"}}}

	// Case 63: only a bot posts a form with the bot action.
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+ops+"/messages", map[string]any{"text": "x", "form": buttons})
	// A bot form cannot be anonymous.
	anon := map[string]any{"anonymous": true}
	for k, v := range buttons {
		anon[k] = v
	}
	if status, out := bot.Do("POST", "/api/v1/conversations/"+ops+"/messages", map[string]any{"text": "x", "form": anon}); status != 400 {
		t.Fatalf("an anonymous bot form = %d %v, want 400", status, out)
	}

	msg := obj(bot.Must(201, "POST", "/api/v1/conversations/"+ops+"/messages", map[string]any{"text": "Deploy api?", "form": buttons}), "message")
	id := msg["id"].(string)
	press := "/api/v1/messages/" + id + "/form/buttons/go"

	// With the bot away the person is told so.
	bob.Refused(503, model.CodeUnavailable, "POST", press, map[string]any{})

	botStream, _ := bot.Events(nil)
	bobStream, _ := bob.Events(nil)
	// Case 82: a bot answers no form and presses no button.
	other.Refused(403, model.CodeForbidden, "POST", press, map[string]any{})

	out := bob.Must(200, "POST", press, map[string]any{})
	e := botStream.Expect("form.answered")
	interaction, _ := e["interaction_id"].(string)
	if !model.ValidID(interaction) || e["message_id"] != id || e["form_id"] != obj(msg, "form")["id"] || e["account_id"] != string(bob.ID) ||
		e["button_id"] != "go" || len(obj(e, "answers")) != 0 || e["answers"] == nil || e["dialog"] != nil || e["seq"] != nil {
		t.Fatalf("form.answered for a button press: %v", e)
	}
	if got, _ := out["interaction_id"].(string); got != "" && got != interaction {
		t.Fatalf("the press names another interaction than the bot was sent: %v", out)
	}
	// The bot answers only the person who pressed, and a message response
	// to a press is not marked as a command's.
	respond := "/api/v1/interactions/" + interaction + "/responses"
	bot.Must(200, "POST", respond, map[string]any{"type": "private", "text": "Only people in @deployers can deploy."})
	if r := bobStream.Expect("interaction.responded"); obj(r, "response")["text"] != "Only people in @deployers can deploy." {
		t.Fatalf("the private answer to a press: %v", r)
	}
	plain := obj(bot.Must(200, "POST", respond, map[string]any{"type": "message", "message": map[string]any{"text": "Deploying"}}), "message")
	if plain["via"] != nil {
		t.Fatalf("a response to a press is not via a command: %v", plain)
	}
	// Nothing is stored: a person may press any number of times.
	bob.Must(200, "POST", press, map[string]any{})
	botStream.Expect("form.answered")
}

func TestWebhookFlow(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ops := ann.Channel("ops", false, bob)
	priv := ann.Channel("priv", true)
	guest := ts.Guest("gil", ops)
	bot := ts.Owner.Bot("deploybot")
	ann.Add(ops, "manager", bot)
	ask := func(conv string) string { return "/api/v1/conversations/" + conv + "/webhook-requests" }

	// Case 93: a guest cannot ask. A bot cannot either.
	guest.Refused(403, model.CodeForbidden, "POST", ask(ops), map[string]any{"name": "CI"})
	bot.Refused(403, model.CodeForbidden, "POST", ask(ops), map[string]any{"name": "CI"})
	// Case 92, rulings R8 and P21: never in an ad-hoc channel.
	chat := obj(ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID)}}), "conversation")["id"].(string)
	ann.Refused(403, model.CodeForbidden, "POST", ask(chat), map[string]any{"name": "CI"})
	// Not for a channel the asker is not in.
	bob.Refused(404, model.CodeNotFound, "POST", ask(priv), map[string]any{"name": "CI"})
	ts.Member("eve").Refused(403, model.CodeForbidden, "POST", ask(ops), map[string]any{"name": "CI"})
	ann.Refused(400, model.CodeInvalidRequest, "POST", ask(ops), map[string]any{"purpose": "no name"})
	ann.Refused(400, model.CodeInvalidValue, "POST", ask(ops), map[string]any{"name": "  "})
	ann.Refused(400, model.CodeInvalidValue, "POST", ask(ops), map[string]any{"name": "CI", "purpose": strings.Repeat("p", 251)})

	ownerStream, _ := ts.Owner.Events(nil)
	annStream, _ := ann.Events(nil)
	bobStream, _ := bob.Events(nil)

	w := obj(ann.Must(201, "POST", ask(ops), map[string]any{"name": "CI", "purpose": "Build results"}), "webhook")
	id := w["id"].(string)
	if w["state"] != "waiting" || w["has_secret"] != false || w["name"] != "CI" || w["purpose"] != "Build results" ||
		w["requested_by_id"] != string(ann.ID) || w["conversation_id"] != ops || w["decided_by_id"] != nil || w["bot_id"] == nil {
		t.Fatalf("a waiting request: %v", w)
	}
	for _, secret := range []string{"url", "token", "token_hash", "signing_secret"} {
		if _, leaked := w[secret]; leaked {
			t.Fatalf("a webhook shows %s", secret)
		}
	}
	// Whoever may approve is told. The account behind the webhook exists,
	// under a generated username, and is in no channel yet.
	if e := ownerStream.Expect("webhook.requested"); obj(e, "webhook")["id"] != id {
		t.Fatalf("webhook.requested: %v", e)
	}
	hookBot := obj(ts.Owner.Must(200, "GET", "/api/v1/admin/bots/"+w["bot_id"].(string), nil), "bot")
	username := obj(hookBot, "account")["username"].(string)
	if hookBot["webhook_only"] != true || len(username) != len("hook-")+8 || !strings.HasPrefix(username, "hook-") ||
		obj(hookBot, "account")["display_name"] != "CI" || len(arr(hookBot, "channel_ids")) != 0 {
		t.Fatalf("the account behind a waiting webhook: %v", hookBot)
	}
	if n := len(arr(ts.Owner.Must(200, "GET", "/api/v1/admin/bots", nil), "bots")); n != 1 {
		t.Fatalf("webhook accounts are listed only when asked for; got %d bots", n)
	}
	if n := len(arr(ts.Owner.Must(200, "GET", "/api/v1/admin/bots?webhooks=true", nil), "bots")); n != 2 {
		t.Fatalf("with webhooks: %d bots", n)
	}
	// It has no token and can have none.
	ts.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/bots/"+w["bot_id"].(string)+"/tokens", map[string]any{})

	// Until approved it has no address.
	ann.Refused(409, model.CodeConflict, "POST", "/api/v1/webhooks/"+id+"/address", nil)
	ann.Refused(409, model.CodeConflict, "POST", "/api/v1/webhooks/"+id+"/reset", nil)

	// An admin approves; a member cannot, nor can a manager of the
	// channel while the setting is off, nor someone outside the channel.
	bob.Refused(403, model.CodeForbidden, "POST", "/api/v1/webhooks/"+id+"/approve", nil)
	bob.Refused(403, model.CodeForbidden, "POST", "/api/v1/webhooks/"+id+"/decline", nil)
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/webhooks/"+id+"/approve", nil)
	ts.Owner.Refused(404, model.CodeNotFound, "POST", "/api/v1/webhooks/"+string(model.NewID[model.WebhookID]())+"/approve", nil)
	approved := obj(ts.Owner.Must(200, "POST", "/api/v1/webhooks/"+id+"/approve", nil), "webhook")
	if approved["state"] != "approved" || approved["decided_by_id"] != string(ts.Owner.ID) || approved["decided_at"] == nil || approved["has_secret"] != false {
		t.Fatalf("approved: %v", approved)
	}
	// Its account joins the channel with the usual notice, and the
	// requester is told.
	if e := bobStream.Expect("member.joined"); obj(e, "member")["account_id"] != w["bot_id"] || obj(e, "member")["level"] != "member" {
		t.Fatalf("the webhook's account joining: %v", e)
	}
	if e := annStream.Expect("webhook.decided"); obj(e, "webhook")["state"] != "approved" {
		t.Fatalf("webhook.decided: %v", e)
	}
	ts.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/webhooks/"+id+"/approve", nil)
	ts.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/webhooks/"+id+"/decline", nil)

	// The address is shown to the requester, once.
	bob.Refused(403, model.CodeForbidden, "POST", "/api/v1/webhooks/"+id+"/address", nil)
	ts.Owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/webhooks/"+id+"/address", nil)
	hookSend(ts, "/hooks/churr_hook_"+strings.Repeat("A", 43), map[string]any{"text": "too early"}, nil)
	got := ann.Must(200, "POST", "/api/v1/webhooks/"+id+"/address", nil)
	address, signing := got["url"].(string), got["signing_secret"].(string)
	if !strings.HasPrefix(address, ts.Origin+"/hooks/churr_hook_") || len(address) != len(ts.Origin+"/hooks/churr_hook_")+43 {
		t.Fatalf("the address: %q", address)
	}
	if len(signing) != 43 || strings.Contains(address, signing) || strings.HasPrefix(signing, "churr_") {
		t.Fatalf("the signing secret is 256 bits in base64url, with no prefix, and never part of the address: %q", signing)
	}
	ann.Refused(409, model.CodeConflict, "POST", "/api/v1/webhooks/"+id+"/address", nil)
	path := strings.TrimPrefix(address, ts.Origin)
	hookPosted(t, ts, path, map[string]any{"text": "It works"})

	// Members of the channel see which webhooks it has; nobody sees an
	// address.
	seen := arr(bob.Must(200, "GET", "/api/v1/conversations/"+ops+"/webhooks", nil), "webhooks")
	if len(seen) != 1 || seen[0].(map[string]any)["has_secret"] != true || seen[0].(map[string]any)["last_used_at"] == nil {
		t.Fatalf("the channel's webhooks: %v", seen)
	}
	if raw, _ := json.Marshal(seen); strings.Contains(string(raw), "churr_hook_") || strings.Contains(string(raw), signing) {
		t.Fatalf("a listed webhook shows a secret: %s", raw)
	}
	bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv+"/webhooks", nil)
	if none := arr(ann.Must(200, "GET", "/api/v1/conversations/"+priv+"/webhooks", nil), "webhooks"); none == nil || len(none) != 0 {
		t.Fatalf("a channel with no webhooks lists []: %v", none)
	}

	// The whole server's webhooks, for admins.
	ann.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/webhooks", nil)
	ts.Owner.Refused(400, model.CodeInvalidValue, "GET", "/api/v1/admin/webhooks?state=odd", nil)
	if all := arr(ts.Owner.Must(200, "GET", "/api/v1/admin/webhooks?state=approved", nil), "webhooks"); len(all) != 1 {
		t.Fatalf("approved webhooks: %v", all)
	}
	if none := arr(ts.Owner.Must(200, "GET", "/api/v1/admin/webhooks?state=waiting", nil), "webhooks"); len(none) != 0 {
		t.Fatalf("waiting webhooks: %v", none)
	}

	// Ruling R11: a reset replaces the address and the signing secret
	// together, and shows them once to whoever reset it.
	bob.Refused(403, model.CodeForbidden, "POST", "/api/v1/webhooks/"+id+"/reset", nil)
	fresh := ts.Owner.Must(200, "POST", "/api/v1/webhooks/"+id+"/reset", nil)
	if fresh["url"] == address || fresh["signing_secret"] == signing {
		t.Fatalf("a reset replaces both: %v", fresh)
	}
	hookRefused(t, ts, 404, model.CodeNotFound, path, map[string]any{"text": "the old address"})
	path = strings.TrimPrefix(fresh["url"].(string), ts.Origin)
	hookPosted(t, ts, path, map[string]any{"text": "the new address"})

	// A manager of the channel may reset and revoke too. bob is not one.
	bob.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/webhooks/"+id, nil)
	ann.Must(200, "POST", "/api/v1/webhooks/"+id+"/reset", nil)
	hookRefused(t, ts, 404, model.CodeNotFound, path, map[string]any{"text": "replaced again"})

	// Revoking: the address stops, the account leaves, the messages stay.
	ann.Must(204, "DELETE", "/api/v1/webhooks/"+id, nil)
	if e := bobStream.Expect("member.left"); e["account_id"] != w["bot_id"] {
		t.Fatalf("the revoked webhook's account leaving: %v", e)
	}
	ann.Must(204, "DELETE", "/api/v1/webhooks/"+id, nil) // already revoked: nothing to do
	ann.Refused(409, model.CodeConflict, "POST", "/api/v1/webhooks/"+id+"/reset", nil)
	if left := arr(bob.Must(200, "GET", "/api/v1/conversations/"+ops+"/webhooks", nil), "webhooks"); len(left) != 0 {
		t.Fatalf("a revoked webhook is not among the channel's: %v", left)
	}
	if all := arr(ts.Owner.Must(200, "GET", "/api/v1/admin/webhooks?state=revoked", nil), "webhooks"); len(all) != 1 {
		t.Fatalf("revoked webhooks: %v", all)
	}
	if msgs := arr(bob.Must(200, "GET", "/api/v1/conversations/"+ops+"/messages", nil), "messages"); len(msgs) < 2 {
		t.Fatalf("a revoked webhook's messages stay: %v", msgs)
	}

	// Declining removes the waiting account.
	w2 := obj(bob.Must(201, "POST", ask(ops), map[string]any{"name": "Alerts"}), "webhook")
	declined := obj(ts.Owner.Must(200, "POST", "/api/v1/webhooks/"+w2["id"].(string)+"/decline", nil), "webhook")
	if declined["state"] != "declined" || declined["decided_by_id"] != string(ts.Owner.ID) {
		t.Fatalf("declined: %v", declined)
	}
	ts.Owner.Refused(404, model.CodeNotFound, "GET", "/api/v1/admin/bots/"+w2["bot_id"].(string), nil)
	if all := arr(ts.Owner.Must(200, "GET", "/api/v1/admin/webhooks?state=declined", nil), "webhooks"); len(all) != 1 {
		t.Fatalf("a declined request stays declined: %v", all)
	}
	bob.Refused(409, model.CodeConflict, "POST", "/api/v1/webhooks/"+w2["id"].(string)+"/address", nil)

	// At most three requests waiting; the requester can withdraw one.
	var waiting []string
	for _, name := range []string{"one", "two", "three"} {
		waiting = append(waiting, obj(bob.Must(201, "POST", ask(ops), map[string]any{"name": name}), "webhook")["id"].(string))
	}
	bob.Refused(409, model.CodeConflict, "POST", ask(ops), map[string]any{"name": "four"})
	bob.Must(204, "DELETE", "/api/v1/webhooks/"+waiting[0], nil)
	bob.Must(201, "POST", ask(ops), map[string]any{"name": "four"})

	// An approver who is also the requester approves their own request
	// in the same step.
	ts.Owner.Must(200, "POST", "/api/v1/conversations/"+ops+"/join", nil)
	own := obj(ts.Owner.Must(201, "POST", ask(ops), map[string]any{"name": "Owner's"}), "webhook")
	if own["state"] != "approved" || own["decided_by_id"] != string(ts.Owner.ID) {
		t.Fatalf("an admin's own request: %v", own)
	}

	// The setting that hands the decision to the channel's managers.
	if err := ts.Server.Store.Settings().Set(context.Background(), store.SettingWebhooksManagersMay, true, ts.Owner.ID); err != nil {
		t.Fatal(err)
	}
	if got := obj(ann.Must(200, "POST", "/api/v1/webhooks/"+waiting[1]+"/approve", nil), "webhook"); got["state"] != "approved" {
		t.Fatalf("a manager approving with the setting on: %v", got)
	}
	bob.Refused(403, model.CodeForbidden, "POST", "/api/v1/webhooks/"+waiting[2]+"/approve", nil)
	// And a manager's own request is then approved as it is made.
	if got := obj(ann.Must(201, "POST", ask(priv), map[string]any{"name": "Private hook"}), "webhook"); got["state"] != "approved" {
		t.Fatalf("a manager's own request with the setting on: %v", got)
	}

	// A requester who is no longer in the channel when collecting loses
	// the webhook.
	bob.Must(204, "POST", "/api/v1/conversations/"+ops+"/leave", nil)
	ts.Owner.Must(200, "POST", "/api/v1/webhooks/"+waiting[2]+"/approve", nil)
	bob.Refused(409, model.CodeConflict, "POST", "/api/v1/webhooks/"+waiting[2]+"/address", nil)
	if all := arr(ts.Owner.Must(200, "GET", "/api/v1/admin/webhooks?state=revoked", nil), "webhooks"); len(all) != 3 {
		t.Fatalf("revoked webhooks after the requester left: %d", len(all))
	}

	// Requests, decisions, collections, resets and revocations are in
	// the audit log.
	for _, action := range []string{"webhook.requested", "webhook.approved", "webhook.declined", "webhook.collected", "webhook.reset", "webhook.revoked"} {
		entries, _, err := ts.Server.Store.Audit().List(context.Background(), store.AuditFilter{Action: action}, store.Page{})
		if err != nil || len(entries) == 0 {
			t.Errorf("no audit entry for %s (%v)", action, err)
		}
	}
}

func TestWebhookLockedAndPrivate(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	priv := ann.Channel("priv", true)
	ctx := context.Background()

	// For a private channel the approving admin need not be in it.
	w := obj(ann.Must(201, "POST", "/api/v1/conversations/"+priv+"/webhook-requests", map[string]any{"name": "CI"}), "webhook")
	ts.Owner.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv, nil)
	ts.Owner.Must(200, "POST", "/api/v1/webhooks/"+w["id"].(string)+"/approve", nil)
	path := strings.TrimPrefix(ann.Must(200, "POST", "/api/v1/webhooks/"+w["id"].(string)+"/address", nil)["url"].(string), ts.Origin)

	// Case 179: a locked private channel takes no new members, so a
	// request for one cannot be made or approved.
	waiting := obj(ann.Must(201, "POST", "/api/v1/conversations/"+priv+"/webhook-requests", map[string]any{"name": "Later"}), "webhook")
	if _, err := ts.Server.Store.Conversations().Lock(ctx, model.ConversationID(priv), ann.ID, model.Now()); err != nil {
		t.Fatal(err)
	}
	ann.Refused(403, model.CodeLocked, "POST", "/api/v1/conversations/"+priv+"/webhook-requests", map[string]any{"name": "Too late"})
	ts.Owner.Refused(403, model.CodeLocked, "POST", "/api/v1/webhooks/"+waiting["id"].(string)+"/approve", nil)
	// Declining adds nobody, and the webhooks it already has keep working.
	ts.Owner.Must(200, "POST", "/api/v1/webhooks/"+waiting["id"].(string)+"/decline", nil)
	hookPosted(t, ts, path, map[string]any{"text": "still working"})
}

func TestWebhookNative(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ops := ann.Channel("ops", false, bob)
	other := ann.Channel("other", false)
	id, path, _ := ann.Webhook(ops, "CI")
	stream, _ := bob.Events(nil)

	// The message is the webhook account's, marked via the webhook under
	// its own name; text is resolved as anyone's and mentions notify.
	m := hookPosted(t, ts, path, map[string]any{"text": "Build **#482** failed on `main`. @bob"})
	e := stream.Expect("message.created")
	msg := obj(e, "message")
	hooks := arr(ann.Must(200, "GET", "/api/v1/conversations/"+ops+"/webhooks", nil), "webhooks")
	if msg["id"] != m["id"] || msg["author_id"] != hooks[0].(map[string]any)["bot_id"] || obj(msg, "via")["kind"] != "webhook" || obj(msg, "via")["name"] != "CI" {
		t.Fatalf("a webhook's message: %v", msg)
	}
	// #482 is a colour like any three hex digits (ruling R87).
	if ents := arr(msg, "entities"); len(ents) != 2 || ents[0].(map[string]any)["type"] != "colour" || ents[1].(map[string]any)["account_id"] != string(bob.ID) {
		t.Fatalf("a webhook's text is resolved like anyone's: %v", msg["entities"])
	}
	if e["notify"] != "notify" {
		t.Fatalf("a mention by a webhook notifies: notify = %v", e["notify"])
	}

	// A via override, cards, and a mention inside a card, which never
	// notifies.
	m = hookPosted(t, ts, path, map[string]any{
		"text": "",
		"cards": []any{map[string]any{"color": "red", "title": "Build #482 failed", "url": "https://ci.example/builds/482", "text": "3 tests failing, @bob",
			"fields": []any{map[string]any{"label": "Branch", "value": "main", "short": true}}, "footer": "ci.example", "at": "2026-11-13T09:28:10.000Z",
			"author": map[string]any{"name": "Jenkins"}}},
		"via": map[string]any{"name": "Jenkins"},
	})
	e = stream.Expect("message.created")
	msg = obj(e, "message")
	card, _ := arr(msg, "cards")[0].(map[string]any)
	if obj(msg, "via")["name"] != "Jenkins" || card["title"] != "Build #482 failed" || card["color"] != "red" || obj(card, "author")["name"] != "Jenkins" ||
		card["at"] != "2026-11-13T09:28:10.000Z" || card["preview_url"] != nil {
		t.Fatalf("a webhook's card: %v", msg)
	}
	if e["notify"] == "notify" {
		t.Fatal("a mention inside a card notified")
	}

	// A reply in a thread of its own channel; a parent elsewhere is not
	// found (case 90).
	reply := hookPosted(t, ts, path, map[string]any{"text": "More detail", "parent_id": m["id"]})
	if got := messageOf(bob, reply["id"].(string)); got["parent_id"] != m["id"] {
		t.Fatalf("a webhook's reply: %v", got)
	}
	elsewhere := ann.Post(other, "in another channel")["id"].(string)
	hookRefused(t, ts, 404, model.CodeNotFound, path, map[string]any{"text": "x", "parent_id": elsewhere})
	hookRefused(t, ts, 400, model.CodeInvalidValue, path, map[string]any{"text": "x", "parent_id": "nonsense"})

	// The same client_id again returns the first message and posts
	// nothing.
	cid := string(model.NewID[model.MessageID]())
	first := hookPosted(t, ts, path, map[string]any{"text": "once", "client_id": cid})
	if again := hookPosted(t, ts, path, map[string]any{"text": "once", "client_id": cid}); again["id"] != first["id"] || again["seq"] != first["seq"] {
		t.Fatalf("a repeated client_id: %v, first %v", again, first)
	}
	hookRefused(t, ts, 400, model.CodeInvalidValue, path, map[string]any{"text": "x", "client_id": "nonsense"})

	// What a webhook may not send.
	hookRefused(t, ts, 400, model.CodeInvalidRequest, path, map[string]any{})
	hookRefused(t, ts, 400, model.CodeInvalidRequest, path, map[string]any{"text": "x", "unknown": true})
	hookRefused(t, ts, 400, model.CodeInvalidRequest, path, map[string]any{"text": "x", "file_ids": []string{}})
	hookRefused(t, ts, 400, model.CodeInvalidRequest, path, map[string]any{"text": "x", "quote": map[string]any{"message_id": m["id"]}})
	hookRefused(t, ts, 400, model.CodeInvalidRequest, path, map[string]any{"text": "x", "notify_anyway": []string{string(bob.ID)}})
	hookRefused(t, ts, 400, model.CodeInvalidRequest, path, map[string]any{"text": "x", "via": map[string]any{"name": "x", "kind": "command"}})
	hookRefused(t, ts, 400, model.CodeInvalidRequest, path, map[string]any{"cards": []any{map[string]any{"title": "x", "image_file_id": m["id"]}}})
	hookRefused(t, ts, 400, model.CodeInvalidValue, path, map[string]any{"text": strings.Repeat("x", 16001)})
	hookRefused(t, ts, 400, model.CodeInvalidValue, path, map[string]any{"cards": []any{map[string]any{"footer": "only a footer"}}})
	hookRefused(t, ts, 400, model.CodeInvalidValue, path, map[string]any{"cards": []any{map[string]any{"title": "x", "color": "blue"}}})
	hookRefused(t, ts, 400, model.CodeInvalidValue, path, map[string]any{"text": "x", "via": map[string]any{"name": strings.Repeat("n", 65)}})
	six := []any{}
	for i := 0; i < 6; i++ {
		six = append(six, map[string]any{"title": "card"})
	}
	hookRefused(t, ts, 400, model.CodeInvalidValue, path, map[string]any{"cards": six})
	hookRefused(t, ts, 400, model.CodeInvalidValue, path, map[string]any{"text": "x", "files": []any{map[string]any{"part": "file1"}}})

	// A webhook's form may be tally, collect or thread. It has no
	// connection to receive answers on and is never a manager.
	field := []any{map[string]any{"id": "q", "type": "choice", "label": "Ship it?", "options": []any{
		map[string]any{"id": "y", "label": "Yes"}, map[string]any{"id": "n", "label": "No"}}}}
	for _, action := range []string{"bot", "channel", "group"} {
		hookRefused(t, ts, 400, model.CodeInvalidValue, path, map[string]any{"form": map[string]any{"fields": field, "action": map[string]any{"type": action}}})
	}
	hookRefused(t, ts, 400, model.CodeInvalidValue, path, map[string]any{"form": map[string]any{"fields": field, "action": map[string]any{"type": "tally"},
		"answerer_group_id": string(model.NewID[model.GroupID]())}})
	poll := hookPosted(t, ts, path, map[string]any{"form": map[string]any{"fields": field, "action": map[string]any{"type": "tally"}}})
	if form := obj(messageOf(bob, poll["id"].(string)), "form"); obj(form, "action")["type"] != "tally" {
		t.Fatalf("a webhook's poll: %v", form)
	}
	// A webhook can only post: it reads nothing, with any credential.
	bob.Must(200, "PUT", "/api/v1/messages/"+poll["id"].(string)+"/form/answer", map[string]any{"answers": map[string]any{"q": []string{"y"}}})

	// Link previews are off for images here (nothing can fetch): a card
	// that loses its image gets the address in its footer, a card left
	// with nothing goes, and a message left with nothing is refused
	// before anything is stored.
	lost := hookPosted(t, ts, path, map[string]any{"cards": []any{
		map[string]any{"title": "Graph", "image_url": "https://ci.example/graph.png", "footer": "ci.example",
			"author": map[string]any{"name": "CI", "icon_url": "https://ci.example/icon.png"}},
		map[string]any{"image_url": "https://ci.example/only-an-image.png"}},
		"via": map[string]any{"name": "CI", "icon_url": "https://ci.example/icon.png"}})
	cards := arr(messageOf(bob, lost["id"].(string)), "cards")
	if len(cards) != 1 || cards[0].(map[string]any)["footer"] != "ci.example · https://ci.example/graph.png" || cards[0].(map[string]any)["image_file_id"] != nil ||
		obj(cards[0].(map[string]any), "author")["icon_file_id"] != nil {
		t.Fatalf("cards whose images were dropped: %v", cards)
	}
	hookRefused(t, ts, 400, model.CodeInvalidRequest, path, map[string]any{"cards": []any{map[string]any{"image_url": "https://ci.example/only.png"}}})
	hookRefused(t, ts, 400, model.CodeInvalidValue, path, map[string]any{"cards": []any{map[string]any{"title": "x", "image_url": "file:///etc/passwd"}}})

	// The token in the path is the only credential, and works nowhere
	// else (case 89).
	token := strings.TrimPrefix(path, "/hooks/")
	asBearer := &testkit.Client{}
	*asBearer = *ann
	asBearer.Bearer = token
	asBearer.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me", nil)
	asBearer.Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/conversations/"+ops+"/messages", map[string]any{"text": "x"})
	hookRefused(t, ts, 404, model.CodeNotFound, "/hooks/churr_hook_"+strings.Repeat("x", 43), map[string]any{"text": "x"})
	hookRefused(t, ts, 404, model.CodeNotFound, "/hooks/"+ann.Token().Bearer, map[string]any{"text": "x"})
	hookRefused(t, ts, 404, model.CodeNotFound, "/hooks/"+strings.TrimPrefix(token, "churr_hook_"), map[string]any{"text": "x"})
	if status, _, _ := ts.Raw("GET", path, nil, nil); status != 404 {
		t.Fatalf("GET on a webhook address = %d, want 404: these addresses take POST only", status)
	}

	// The body is at most 1 MB.
	big, _ := json.Marshal(map[string]any{"text": strings.Repeat("x", 1<<20)})
	status, _, raw := ts.Raw("POST", path, http.Header{"Content-Type": {"application/json"}}, big)
	if status != 413 || !strings.Contains(string(raw), `"too_large"`) {
		t.Fatalf("a body over 1 MB = %d %s", status, raw)
	}

	// Case 126: a webhook in an archived channel.
	if _, err := ts.Server.Store.Conversations().Update(context.Background(), model.ConversationID(ops), func(c *model.Conversation) error {
		c.ArchivedAt = model.Now()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	hookRefused(t, ts, 403, model.CodeArchived, path, map[string]any{"text": "x"})
	if _, err := ts.Server.Store.Conversations().Update(context.Background(), model.ConversationID(ops), func(c *model.Conversation) error {
		c.ArchivedAt = 0
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Case 91 (the contract answers not_found for it): a revoked
	// webhook posts nothing.
	ann.Must(204, "DELETE", "/api/v1/webhooks/"+id, nil)
	hookRefused(t, ts, 404, model.CodeNotFound, path, map[string]any{"text": "x"})
}

func TestWebhookFiles(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	ops := ann.Channel("ops", false)
	_, path, _ := ann.Webhook(ops, "CI")
	post := func(payload any, parts ...botsPart) (int, map[string]any) {
		raw, _ := json.Marshal(payload)
		header, body := botsMultipart(append([]botsPart{{name: "payload", data: raw}}, parts...)...)
		status, _, resp := ts.Raw("POST", path, header, body)
		out := map[string]any{}
		json.Unmarshal(resp, &out)
		return status, out
	}

	// A file attached to the message, a card's image, a card author's
	// icon and the via icon, all sent in the same request.
	status, out := post(map[string]any{
		"text":  "The log and a graph",
		"files": []any{map[string]any{"part": "file1", "name": "build.log", "caption": "Build log"}, map[string]any{"part": "file2"}},
		"cards": []any{map[string]any{"image_part": "file3", "author": map[string]any{"name": "CI", "icon_part": "file4"}}},
		"via":   map[string]any{"name": "CI", "icon_part": "file5"},
	}, botsPart{"file1", "ignored.txt", []byte("line one\nline two\n")}, botsPart{"file2", "photo.png", botsPNG()},
		botsPart{"file3", "graph.png", botsPNG()}, botsPart{"file4", "icon.png", botsPNG()}, botsPart{"file5", "via.png", botsPNG()})
	if status != 200 {
		t.Fatalf("a multipart post = %d %v", status, out)
	}
	msg := messageOf(ann, obj(out, "message")["id"].(string))
	files := arr(msg, "files")
	if len(files) != 2 {
		t.Fatalf("two files on the message: %v", msg)
	}
	log, photo := files[0].(map[string]any), files[1].(map[string]any)
	if log["name"] != "build.log" || log["caption"] != "Build log" || log["kind"] != "text" || log["size"] != float64(18) {
		t.Fatalf("a text file part: %v", log)
	}
	// Kind and media type come from the content, and an image gets its
	// size and a thumbnail.
	if photo["name"] != "photo.png" || photo["kind"] != "image" || photo["media_type"] != "image/png" || photo["width"] != float64(8) || photo["thumb_url"] == nil {
		t.Fatalf("an image file part: %v", photo)
	}
	card := arr(msg, "cards")[0].(map[string]any)
	if !model.ValidID(card["image_file_id"].(string)) || !model.ValidID(obj(card, "author")["icon_file_id"].(string)) ||
		!model.ValidID(obj(msg, "via")["icon_file_id"].(string)) {
		t.Fatalf("images sent as parts: %v", msg)
	}

	// A part the JSON does not name is refused; so is a name it gives
	// that is not there, and an image that is not one.
	if status, out := post(map[string]any{"text": "x"}, botsPart{"file1", "a.txt", []byte("x")}); status != 400 || obj(out, "error")["code"] != "invalid_request" {
		t.Fatalf("an unused part = %d %v", status, out)
	}
	if status, out := post(map[string]any{"text": "x"}, botsPart{"other", "a.txt", []byte("x")}); status != 400 || obj(out, "error")["code"] != "invalid_request" {
		t.Fatalf("a part with another name = %d %v", status, out)
	}
	if status, out := post(map[string]any{"files": []any{map[string]any{"part": "file2"}}}, botsPart{"file1", "a.txt", []byte("x")}); status != 400 {
		t.Fatalf("a missing part = %d %v", status, out)
	}
	if status, out := post(map[string]any{"cards": []any{map[string]any{"title": "x", "image_part": "file1"}}}, botsPart{"file1", "a.txt", []byte("not an image")}); status != 400 || obj(out, "error")["code"] != "invalid_value" {
		t.Fatalf("a card image that is not an image = %d %v", status, out)
	}
	header, body := botsMultipart(botsPart{"file1", "a.txt", []byte("x")})
	if status, _, _ := ts.Raw("POST", path, header, body); status != 400 {
		t.Fatalf("a multipart request with no payload = %d", status)
	}
	// The whole request is still limited to 1 MB.
	if status, out := post(map[string]any{"files": []any{map[string]any{"part": "file1"}}}, botsPart{"file1", "big.bin", bytes.Repeat([]byte{7}, 1<<20)}); status != 413 {
		t.Fatalf("a multipart request over 1 MB = %d %v", status, out)
	}
	// Nothing was stored for the refused requests.
	if msgs := arr(ann.Must(200, "GET", "/api/v1/conversations/"+ops+"/messages", nil), "messages"); len(msgs) != 2 {
		// the webhook's join notice is not a message; only the one post
		// and nothing else should be here beside it
		count := 0
		for _, m := range msgs {
			if m.(map[string]any)["via"] != nil {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("one message posted through the webhook, got %d", count)
		}
	}
}

// fakeImages stands in for the preview fetcher's guarded path.
type fakeImages struct {
	on      bool
	fetched []string
}

func (f *fakeImages) Enabled(context.Context) bool { return f.on }

func (f *fakeImages) FetchImage(_ context.Context, address string) ([]byte, error) {
	f.fetched = append(f.fetched, address)
	if strings.Contains(address, "broken") {
		return nil, errors.New("no such image")
	}
	if strings.Contains(address, "page") {
		return []byte("<html>not an image</html>"), nil
	}
	return botsPNG(), nil
}

func TestWebhookImageAddresses(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	ops := ann.Channel("ops", false)
	_, path, _ := ann.Webhook(ops, "CI")
	images := &fakeImages{on: true}
	ts.Server.Bots.SetImages(images)
	stream, _ := ann.Events(nil)

	// The message is posted at once without the images; each address is
	// then fetched once and added with the event that adds a link
	// preview. That is not an edit.
	m := hookPosted(t, ts, path, map[string]any{
		"text": "Graphs",
		"cards": []any{
			map[string]any{"title": "Good", "image_url": "https://ci.example/graph.png", "author": map[string]any{"name": "CI", "icon_url": "https://ci.example/icon.png"}},
			map[string]any{"title": "Broken", "image_url": "https://ci.example/broken.png", "footer": "ci.example"},
			map[string]any{"image_url": "https://ci.example/broken-and-alone.png"},
			map[string]any{"title": "Not an image", "image_url": "https://ci.example/page"}},
		"via": map[string]any{"name": "CI", "icon_url": "https://ci.example/icon.png"}})
	created := obj(stream.Expect("message.created"), "message")
	// A card that is nothing but an image waits for it.
	if cards := arr(created, "cards"); len(cards) != 3 || cards[0].(map[string]any)["image_file_id"] != nil || obj(created, "via")["icon_file_id"] != nil {
		t.Fatalf("the message as first posted has no images yet: %v", created)
	}
	added := stream.Expect("preview.added")
	ts.Server.Bots.Wait()
	msg := obj(added, "message")
	cards := arr(msg, "cards")
	if msg["id"] != m["id"] || added["seq"] == nil || len(cards) != 3 {
		t.Fatalf("preview.added: %v", added)
	}
	good, broken, page := cards[0].(map[string]any), cards[1].(map[string]any), cards[2].(map[string]any)
	if !model.ValidID(good["image_file_id"].(string)) || !model.ValidID(obj(good, "author")["icon_file_id"].(string)) || !model.ValidID(obj(msg, "via")["icon_file_id"].(string)) {
		t.Fatalf("the fetched images: %v", msg)
	}
	// A failed fetch drops the image and leaves the address in the
	// footer; a card left with nothing is removed.
	if broken["image_file_id"] != nil || broken["footer"] != "ci.example · https://ci.example/broken.png" || page["footer"] != "https://ci.example/page" {
		t.Fatalf("the images that could not be had: %v", cards)
	}
	if msg["edited_at"] != nil || msg["has_history"] == true {
		t.Fatalf("adding fetched images is not an edit: %v", msg)
	}
	if got := messageOf(ann, m["id"].(string)); len(arr(got, "cards")) != 3 {
		t.Fatalf("the stored message: %v", got)
	}
	// A fetched address is remembered for a day: the icon, used twice in
	// one message and again in the next, is fetched once.
	hookPosted(t, ts, path, map[string]any{"text": "Again", "via": map[string]any{"name": "CI", "icon_url": "https://ci.example/icon.png"}})
	stream.Expect("preview.added")
	ts.Server.Bots.Wait()
	icons := 0
	for _, a := range images.fetched {
		if a == "https://ci.example/icon.png" {
			icons++
		}
	}
	if icons != 1 {
		t.Fatalf("the icon was fetched %d times, want once: %v", icons, images.fetched)
	}

	// With link previews off nothing is fetched, as with no fetcher.
	images.on, images.fetched = false, nil
	hookRefused(t, ts, 400, model.CodeInvalidRequest, path, map[string]any{"cards": []any{map[string]any{"image_url": "https://ci.example/graph2.png"}}})
	if len(images.fetched) != 0 {
		t.Fatalf("an image was fetched with link previews off: %v", images.fetched)
	}
}

func TestWebhookSlackAndDiscord(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	ops := ann.Channel("ops", false)
	_, path, _ := ann.Webhook(ops, "Alerts")
	last := func() map[string]any {
		msgs := arr(ann.Must(200, "GET", "/api/v1/conversations/"+ops+"/messages", nil), "messages")
		return msgs[len(msgs)-1].(map[string]any)
	}

	// Slack: the example of BOTS.md section 12, answered `ok` as Slack does.
	slack := map[string]any{"text": "Deploy finished", "username": "Jenkins", "unknown_field": true,
		"attachments": []any{map[string]any{"color": "#36a64f", "pretext": "Pipeline <https://ci.example/7|#7>", "title": "api 2.4.1",
			"title_link": "https://ci.example/7", "text": "*Passed* in 4m", "fields": []any{map[string]any{"title": "Env", "value": "staging", "short": true}},
			"footer": "ci.example", "ts": 1794562090}}}
	raw, _ := json.Marshal(slack)
	status, hd, body := ts.Raw("POST", path+"/slack", http.Header{"Content-Type": {"application/json"}}, raw)
	if status != 200 || string(body) != "ok" || !strings.HasPrefix(hd.Get("Content-Type"), "text/plain") {
		t.Fatalf("a Slack payload = %d %q (%s)", status, body, hd.Get("Content-Type"))
	}
	msg := last()
	card := arr(msg, "cards")[0].(map[string]any)
	if msg["text"] != "Deploy finished\n\nPipeline [#7](https://ci.example/7)" || obj(msg, "via")["name"] != "Jenkins" || obj(msg, "via")["kind"] != "webhook" ||
		card["color"] != "green" || card["title"] != "api 2.4.1" || card["url"] != "https://ci.example/7" || card["text"] != "**Passed** in 4m" ||
		card["footer"] != "ci.example" || card["at"] != "2026-11-13T09:28:10.000Z" {
		t.Fatalf("the Slack example as posted: %v", msg)
	}
	// Form-encoded, with the JSON in a field named payload.
	form := url.Values{"payload": {`{"text": "From a *form*"}`}}.Encode()
	status, _, body = ts.Raw("POST", path+"/slack", http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, []byte(form))
	if status != 200 || string(body) != "ok" || last()["text"] != "From a **form**" || obj(last(), "via")["name"] != "Alerts" {
		t.Fatalf("a form-encoded Slack payload = %d %q, posted %v", status, body, last())
	}
	// Errors use Churr's error shape on every address.
	hookRefused(t, ts, 400, model.CodeInvalidRequest, path+"/slack", map[string]any{"icon_emoji": ":ghost:"})
	status, _, body = ts.Raw("POST", path+"/slack", http.Header{"Content-Type": {"application/json"}}, []byte("not json"))
	if status != 400 || !strings.Contains(string(body), `"invalid_request"`) {
		t.Fatalf("a body that is not JSON = %d %s", status, body)
	}
	hookRefused(t, ts, 404, model.CodeNotFound, "/hooks/churr_hook_"+strings.Repeat("y", 43)+"/slack", slack)

	// Discord: the example of section 13, answered 204 with no body.
	discord := map[string]any{"content": "Build failed <@&165511591545143296>", "username": "CI", "tts": false,
		"embeds": []any{map[string]any{"title": "Build #482", "url": "https://ci.example/482", "description": "3 tests failing", "color": 15158332,
			"fields": []any{map[string]any{"name": "Branch", "value": "main", "inline": true}}, "timestamp": "2026-11-13T09:28:10.000Z"}}}
	raw, _ = json.Marshal(discord)
	status, _, body = ts.Raw("POST", path+"/discord", http.Header{"Content-Type": {"application/json"}}, raw)
	if status != 204 || len(body) != 0 {
		t.Fatalf("a Discord payload = %d %q", status, body)
	}
	msg = last()
	card = arr(msg, "cards")[0].(map[string]any)
	if msg["text"] != "Build failed `@&165511591545143296`" || obj(msg, "via")["name"] != "CI" || card["color"] != "red" || card["title"] != "Build #482" ||
		card["at"] != "2026-11-13T09:28:10.000Z" || arr(card, "fields")[0].(map[string]any)["short"] != true {
		t.Fatalf("the Discord example as posted: %v", msg)
	}
	// With ?wait=true the message is named.
	status, _, out := hookSend(ts, path+"/discord?wait=true&thread_id=1", map[string]any{"content": "waited for"}, nil)
	if status != 200 || out["id"] != last()["id"] {
		t.Fatalf("a Discord payload with wait = %d %v", status, out)
	}
	hookRefused(t, ts, 400, model.CodeInvalidRequest, path+"/discord", map[string]any{"content": "  ", "components": []any{}})

	// Multipart: the JSON in payload_json, files in files[n]. A part an
	// embed names with attachment:// is its image and nothing is fetched;
	// the rest are files on the message, named by `attachments`.
	payload, _ := json.Marshal(map[string]any{"content": "Chart attached",
		"embeds":      []any{map[string]any{"title": "Chart", "image": map[string]any{"url": "attachment://chart.png"}}},
		"attachments": []any{map[string]any{"id": 1, "filename": "build.log", "description": "The build log"}}})
	header, mp := botsMultipart(botsPart{"payload_json", "", payload}, botsPart{"files[0]", "chart.png", botsPNG()}, botsPart{"files[1]", "upload", []byte("log line\n")})
	status, _, body = ts.Raw("POST", path+"/discord", header, mp)
	if status != 204 {
		t.Fatalf("a multipart Discord payload = %d %s", status, body)
	}
	msg = last()
	files := arr(msg, "files")
	if len(files) != 1 || files[0].(map[string]any)["name"] != "build.log" || files[0].(map[string]any)["caption"] != "The build log" ||
		!model.ValidID(arr(msg, "cards")[0].(map[string]any)["image_file_id"].(string)) {
		t.Fatalf("a multipart Discord payload as posted: %v", msg)
	}
}

func TestWebhookGitHub(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	ops := ann.Channel("ops", false)
	id, path, secret := ann.Webhook(ops, "GitHub")
	path += "/github"
	sign := func(key string, body []byte) string {
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write(body)
		return "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}
	send := func(event, signature, delivery, contentType string, body []byte) (int, map[string]any) {
		h := http.Header{"Content-Type": {contentType}}
		if event != "" {
			h.Set("X-GitHub-Event", event)
		}
		if signature != "" {
			h.Set("X-Hub-Signature-256", signature)
		}
		if delivery != "" {
			h.Set("X-GitHub-Delivery", delivery)
		}
		status, _, raw := ts.Raw("POST", path, h, body)
		out := map[string]any{}
		json.Unmarshal(raw, &out)
		return status, out
	}
	push, _ := json.Marshal(map[string]any{"ref": "refs/heads/main", "compare": "https://github.com/acme/api/compare/a...b",
		"repository": map[string]any{"full_name": "acme/api"}, "sender": map[string]any{"login": "sam"},
		"commits": []any{map[string]any{"id": "4f2c91a8d0e7b6c5", "url": "https://github.com/acme/api/commit/4f2c91a", "message": "Fix login", "author": map[string]any{"username": "sam"}}}})

	// A missing header, a header in another form, or a value that does
	// not match: unauthenticated, and nothing is posted. The token in
	// the address is not the signing secret (ruling R11).
	token := strings.TrimSuffix(strings.TrimPrefix(path, "/hooks/"), "/github")
	for name, signature := range map[string]string{
		"no signature":          "",
		"another form":          "sha1=" + strings.Repeat("0", 40),
		"not hex":               "sha256=zz",
		"a wrong value":         sign("not the secret", push),
		"signed with the token": sign(token, push),
		"signed another body":   sign(secret, []byte("{}")),
	} {
		if status, out := send("push", signature, "", "application/json", push); status != 401 || obj(out, "error")["code"] != "unauthenticated" {
			t.Fatalf("%s = %d %v, want 401 unauthenticated", name, status, out)
		}
	}
	if msgs := arr(ann.Must(200, "GET", "/api/v1/conversations/"+ops+"/messages", nil), "messages"); len(msgs) != 0 {
		for _, m := range msgs {
			if m.(map[string]any)["via"] != nil {
				t.Fatalf("a delivery with a bad signature was posted: %v", m)
			}
		}
	}

	// A good signature: one message with one card and no text.
	delivery := string(model.NewID[model.MessageID]())
	status, out := send("push", sign(secret, push), delivery, "application/json", push)
	if status != 200 || obj(out, "message")["id"] == nil {
		t.Fatalf("a signed push = %d %v", status, out)
	}
	msg := messageOf(ann, obj(out, "message")["id"].(string))
	card := arr(msg, "cards")[0].(map[string]any)
	if msg["text"] != nil || card["title"] != "1 commit pushed to main" || card["color"] != "lavender" || card["footer"] != "acme/api" ||
		obj(card, "author")["name"] != "sam" || card["text"] != "- [`4f2c91a`](https://github.com/acme/api/commit/4f2c91a) Fix login — sam" {
		t.Fatalf("a push as posted: %v", msg)
	}
	// A delivery GitHub sends again is not posted twice.
	if status, again := send("push", sign(secret, push), delivery, "application/json", push); status != 200 || obj(again, "message")["id"] != obj(out, "message")["id"] {
		t.Fatalf("a repeated delivery = %d %v", status, again)
	}

	// The form-encoded content type: the signature is over the raw body.
	form := []byte(url.Values{"payload": {string(push)}}.Encode())
	if status, out := send("push", sign(secret, form), "", "application/x-www-form-urlencoded", form); status != 200 {
		t.Fatalf("a form-encoded delivery = %d %v", status, out)
	}
	if status, _ := send("push", sign(secret, push), "", "application/x-www-form-urlencoded", form); status != 401 {
		t.Fatalf("a form-encoded delivery signed over the JSON alone = %d, want 401", status)
	}

	// The event name is needed; a listed event whose action posts
	// nothing is answered 204; an unlisted event is one plain card.
	if status, out := send("", sign(secret, push), "", "application/json", push); status != 400 || obj(out, "error")["code"] != "invalid_request" {
		t.Fatalf("no X-GitHub-Event = %d %v", status, out)
	}
	labeled := []byte(`{"action": "labeled", "pull_request": {"number": 1, "title": "x"}}`)
	if status, out := send("pull_request", sign(secret, labeled), "", "application/json", labeled); status != 204 || len(out) != 0 {
		t.Fatalf("an action that posts nothing = %d %v", status, out)
	}
	star := []byte(`{"action": "created", "repository": {"full_name": "acme/api", "html_url": "https://github.com/acme/api"}, "sender": {"login": "lee"}}`)
	status, out = send("star", sign(secret, star), "", "application/json", star)
	if status != 200 {
		t.Fatalf("an unlisted event = %d %v", status, out)
	}
	if card := arr(messageOf(ann, obj(out, "message")["id"].(string)), "cards")[0].(map[string]any); card["title"] != "star created" || card["color"] != "grey" {
		t.Fatalf("an unlisted event as posted: %v", card)
	}
	bad := []byte("not json")
	if status, out := send("push", sign(secret, bad), "", "application/json", bad); status != 400 {
		t.Fatalf("a signed body that is not JSON = %d %v", status, out)
	}

	// A reset replaces the signing secret with the address.
	fresh := ann.Must(200, "POST", "/api/v1/webhooks/"+id+"/reset", nil)
	path = strings.TrimPrefix(fresh["url"].(string), ts.Origin) + "/github"
	if status, _ := send("push", sign(secret, push), "", "application/json", push); status != 401 {
		t.Fatalf("the old signing secret after a reset = %d, want 401", status)
	}
	if status, out := send("push", sign(fresh["signing_secret"].(string), push), "", "application/json", push); status != 200 {
		t.Fatalf("the new signing secret = %d %v", status, out)
	}
}

func TestBotAndWebhookLimits(t *testing.T) {
	limits := bots.DefaultLimits()
	limits.MessagesPerMinuteInOne, limits.MessagesPerMinute = 2, 3
	limits.ReactionsPerMinute, limits.CommandSetsPerHour, limits.Connections, limits.WebhookPostsPerMinute = 1, 2, 2, 2
	ts := testkit.NewServer(t, testkit.Options{BotLimits: &limits})
	ann := ts.Member("ann")
	one, two := ann.Channel("one", false), ann.Channel("two", false)
	bot := ts.Owner.Bot("deploybot")
	ann.Add(one, "member", bot)
	ann.Add(two, "member", bot)
	limitedFor := func(out map[string]any) float64 {
		n, _ := obj(out, "error")["retry_after_seconds"].(float64)
		return n
	}

	// Messages: so many a minute in one conversation, and so many in all.
	// The limits are per bot account, whichever token is used.
	post := func(c *testkit.Client, conv string) (int, http.Header, map[string]any) {
		return c.DoFull("POST", "/api/v1/conversations/"+conv+"/messages", map[string]any{"text": "hello"})
	}
	first := obj(bot.Must(201, "POST", "/api/v1/conversations/"+one+"/messages", map[string]any{"text": "hello"}), "message")["id"].(string)
	bot.Must(201, "POST", "/api/v1/conversations/"+one+"/messages", map[string]any{"text": "hello"})
	status, hd, out := post(bot, one)
	if status != 429 || obj(out, "error")["code"] != "rate_limited" || limitedFor(out) < 1 || hd.Get("Retry-After") == "" || hd.Get("RateLimit-Limit") != "600" {
		t.Fatalf("a third message in one conversation = %d %v %v", status, out, hd)
	}
	second := ts.Owner.Must(201, "POST", "/api/v1/admin/bots/"+string(bot.ID)+"/tokens", map[string]any{})
	sameBot := &testkit.Client{}
	*sameBot = *bot
	sameBot.Bearer = second["secret"].(string)
	if status, _, _ := post(sameBot, one); status != 429 {
		t.Fatalf("another token of the same bot = %d, want 429", status)
	}
	if status, _, out := post(bot, two); status != 201 {
		t.Fatalf("a message in another conversation = %d %v", status, out)
	}
	// An edit counts as a message; the fourth in all is refused.
	if status, out := bot.Do("PATCH", "/api/v1/messages/"+first, map[string]any{"text": "edited"}); status != 429 {
		t.Fatalf("an edit over the limit = %d %v", status, out)
	}
	// A person is not held to a bot's limits.
	for i := 0; i < 5; i++ {
		ann.Post(one, "people type as they like")
	}

	// Reactions.
	bot.Must(200, "PUT", "/api/v1/messages/"+first+"/reactions/%F0%9F%91%8D", nil)
	bot.Refused(429, model.CodeRateLimited, "DELETE", "/api/v1/messages/"+first+"/reactions/%F0%9F%91%8D", nil)

	// Setting the command list.
	cmds := map[string]any{"commands": []any{}}
	bot.Must(200, "PUT", "/api/v1/commands", cmds)
	bot.Must(200, "PUT", "/api/v1/commands", cmds)
	if out := bot.Refused(429, model.CodeRateLimited, "PUT", "/api/v1/commands", cmds); out["retry_after_seconds"].(float64) < 60 {
		t.Fatalf("the wait for an hourly limit: %v", out)
	}

	// Live connections at once.
	bot.Events(nil)
	bot.Events(nil)
	if _, resp, err := bot.Dial(); err == nil || resp == nil || resp.StatusCode != 429 {
		t.Fatalf("a third live connection: %v %v, want 429", resp, err)
	}

	// Webhook posts, per webhook, with Retry-After.
	_, path, _ := ann.Webhook(one, "CI")
	_, otherPath, _ := ann.Webhook(two, "CI too")
	hookPosted(t, ts, path, map[string]any{"text": "1"})
	hookPosted(t, ts, path, map[string]any{"text": "2"})
	status, hd, out = hookSend(ts, path, map[string]any{"text": "3"}, nil)
	if status != 429 || obj(out, "error")["code"] != "rate_limited" || limitedFor(out) < 1 || hd.Get("Retry-After") == "" || hd.Get("RateLimit-Limit") != "2" {
		t.Fatalf("a third post through one webhook = %d %v %v", status, out, hd)
	}
	if status, _, _ := ts.Raw("POST", path+"/slack", http.Header{"Content-Type": {"application/json"}}, []byte(`{"text": "4"}`)); status != 429 {
		t.Fatalf("the limit is the webhook's, whichever address: %d", status)
	}
	hookPosted(t, ts, otherPath, map[string]any{"text": "another webhook has its own"})
}
