package api_test

import (
	"fmt"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Reference patterns (ruling R73): admins define the server's, a channel's
// managers define the channel's, and text such as `PROJ-12` posted after
// becomes a `reference` entity with the address the pattern gave it then.

// referencesOf returns a message's reference entities.
func referencesOf(m map[string]any) []map[string]any {
	var out []map[string]any
	ents, _ := m["entities"].([]any)
	for _, e := range ents {
		if em := e.(map[string]any); em["type"] == "reference" {
			out = append(out, em)
		}
	}
	return out
}

func wantReference(t *testing.T, what string, m map[string]any, at, n int, value, url string) {
	t.Helper()
	got := referencesOf(m)
	if len(got) != 1 {
		t.Fatalf("%s: want one reference, got %v (entities %v)", what, got, m["entities"])
	}
	e := got[0]
	if int(e["at"].(float64)) != at || int(e["len"].(float64)) != n || e["value"] != value || e["url"] != url || e["pattern_id"] == nil {
		t.Fatalf("%s: got %v, want at %d len %d value %s url %s", what, e, at, n, value, url)
	}
}

const refPath = "/api/v1/reference-patterns"

func TestReferencePatternsManaged(t *testing.T) {
	ts := testkit.NewServer(t)
	owner, admin, sam, bob := ts.Owner, ts.Admin("ada"), ts.Member("sam"), ts.Member("bob")
	room := sam.Channel("work", false, bob) // sam manages it
	other := owner.Channel("elsewhere", true)
	stream, _, err := bob.Dial()
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	server := map[string]any{"name": "Tracker", "prefix": "PROJ-", "url_template": "https://track.example/browse/PROJ-{value}"}
	// Only admins define the server's patterns.
	sam.Refused(403, model.CodeForbidden, "POST", refPath, server)
	p := obj(admin.Must(201, "POST", refPath, server), "pattern")
	if p["prefix"] != "PROJ-" || p["conversation_id"] != nil || p["id"] == nil {
		t.Fatalf("created: %v", p)
	}
	if ev := stream.Expect("reference_patterns.changed"); len(arr(ev, "patterns")) != 1 || ev["conversation_id"] != nil {
		t.Fatalf("reference_patterns.changed: %v", ev)
	}

	// A channel's managers define the channel's; its other members, and
	// people outside it, may not.
	mine := map[string]any{"conversation_id": room, "name": "Issues", "prefix": "#", "url_template": "https://git.example/issues/{value}"}
	bob.Refused(403, model.CodeForbidden, "POST", refPath, mine)
	sam.Refused(404, model.CodeNotFound, "POST", refPath, map[string]any{"conversation_id": other, "name": "x", "prefix": "X-", "url_template": "https://x.example/{value}"})
	c := obj(sam.Must(201, "POST", refPath, mine), "pattern")
	if c["conversation_id"] != room {
		t.Fatalf("a channel's pattern: %v", c)
	}
	if ev := stream.Expect("reference_patterns.changed"); ev["conversation_id"] != room || len(arr(ev, "patterns")) != 1 {
		t.Fatalf("a channel's reference_patterns.changed: %v", ev)
	}

	// The list: the server's, and a channel's for those who can see it.
	if got := arr(bob.Must(200, "GET", refPath, nil), "patterns"); len(got) != 1 {
		t.Fatalf("server's only: %v", got)
	}
	if got := arr(bob.Must(200, "GET", refPath+"?conversation_id="+room, nil), "patterns"); len(got) != 2 {
		t.Fatalf("with the channel's: %v", got)
	}
	bob.Refused(404, model.CodeNotFound, "GET", refPath+"?conversation_id="+other, nil)

	// Fields are checked.
	bad := []map[string]any{
		{"name": "", "prefix": "A-", "url_template": "https://a.example/{value}"},
		{"name": "n", "prefix": "", "url_template": "https://a.example/{value}"},
		{"name": "n", "prefix": "A1", "url_template": "https://a.example/{value}"},
		{"name": "n", "prefix": "-", "url_template": "https://a.example/{value}"},
		{"name": "n", "prefix": "A-", "url_template": "https://a.example/"},
		{"name": "n", "prefix": "A-", "url_template": "ftp://a.example/{value}"},
		{"name": "n", "prefix": "A-", "url_template": "https://{value}.example/"},
		{"name": "n", "prefix": "A-", "expression": "(a|b)", "url_template": "https://a.example/{value}"},
		{"name": "n", "prefix": "A-", "expression": "a.*", "url_template": "https://a.example/{value}"},
		{"name": "n", "prefix": "A-", "expression": "a?", "url_template": "https://a.example/{value}"},
	}
	for _, b := range bad {
		admin.Refused(400, model.CodeInvalidValue, "POST", refPath, b)
	}

	// A change, by those who may; a pattern stays where it was defined.
	id := p["id"].(string)
	sam.Refused(403, model.CodeForbidden, "PATCH", refPath+"/"+id, map[string]any{"name": "Mine"})
	u := obj(owner.Must(200, "PATCH", refPath+"/"+id, map[string]any{"expression": "[0-9]{1,5}", "conversation_id": room}), "pattern")
	if u["expression"] != "[0-9]{1,5}" || u["name"] != "Tracker" || u["conversation_id"] != nil {
		t.Fatalf("updated: %v", u)
	}
	stream.Expect("reference_patterns.changed")
	admin.Refused(400, model.CodeInvalidValue, "PATCH", refPath+"/"+id, map[string]any{"prefix": "9"})
	admin.Refused(404, model.CodeNotFound, "PATCH", refPath+"/0193a0c7-0000-7000-8000-000000000000", map[string]any{"name": "x"})

	// Removal.
	bob.Refused(403, model.CodeForbidden, "DELETE", refPath+"/"+c["id"].(string), nil)
	sam.Must(204, "DELETE", refPath+"/"+c["id"].(string), nil)
	stream.Expect("reference_patterns.changed")
	sam.Refused(404, model.CodeNotFound, "DELETE", refPath+"/"+c["id"].(string), nil)

	// At most 50 in one place.
	for i := 0; i < model.MaxRefPatterns; i++ {
		sam.Must(201, "POST", refPath, map[string]any{"conversation_id": room, "name": "n", "prefix": fmt.Sprintf("P%c%c-", 'A'+i/26, 'A'+i%26), "url_template": "https://a.example/{value}"})
	}
	sam.Refused(409, model.CodeConflict, "POST", refPath, map[string]any{"conversation_id": room, "name": "n", "prefix": "ZZ-", "url_template": "https://a.example/{value}"})
}

func TestReferencesResolved(t *testing.T) {
	ts := testkit.NewServer(t)
	owner, sam := ts.Owner, ts.Member("sam")
	room := owner.Channel("work", false, sam)
	plain := owner.Channel("plain", false, sam)
	msgs := convPath(room) + "/messages"
	post := func(body map[string]any) map[string]any {
		return owner.Must(201, "POST", msgs, body)["message"].(map[string]any)
	}
	srv := obj(owner.Must(201, "POST", refPath, map[string]any{"name": "Tracker", "prefix": "#", "url_template": "https://track.example/t/{value}"}), "pattern")

	m := post(map[string]any{"text": "See #4521, please."})
	wantReference(t, "server's", m, 4, 5, "4521", "https://track.example/t/4521")
	if m["text"] != "See #4521, please." {
		t.Fatalf("the text must stay as typed: %v", m["text"])
	}

	// A channel's pattern with the same match wins in that channel only.
	owner.Must(201, "POST", refPath, map[string]any{"conversation_id": room, "name": "Issues", "prefix": "#", "url_template": "https://git.example/i/{value}"})
	wantReference(t, "channel's wins", post(map[string]any{"text": "#7"}), 0, 2, "7", "https://git.example/i/7")
	elsewhere := owner.Must(201, "POST", convPath(plain)+"/messages", map[string]any{"text": "#7"})["message"].(map[string]any)
	wantReference(t, "server's elsewhere", elsewhere, 0, 2, "7", "https://track.example/t/7")

	// Not in code, links or escapes, not inside words, and not where the
	// author made the range plain.
	for _, body := range []map[string]any{
		{"text": "`#12`"},
		{"text": "[#12](https://a.example)"},
		{"text": "\\#12"},
		{"text": "a#12"},
		{"text": "#12x"},
		{"text": "#12", "object_hints": []map[string]any{{"start": 0, "end": 3, "kind": "plain"}}},
	} {
		if got := referencesOf(post(body)); len(got) != 0 {
			t.Fatalf("%v: %v", body, got)
		}
	}
	// A bad hint is ignored.
	wantReference(t, "bad hint", post(map[string]any{"text": "#12", "object_hints": []map[string]any{{"start": 5, "end": 1, "kind": "nonsense"}}}),
		0, 3, "12", "https://git.example/i/12")

	// Bots get references too.
	bot := owner.Bot("helper")
	owner.Add(room, "", bot)
	b := bot.Must(201, "POST", msgs, map[string]any{"text": "fixed #99"})["message"].(map[string]any)
	wantReference(t, "bot", b, 6, 3, "99", "https://git.example/i/99")

	// Old messages keep the address they resolved; an edit that leaves the
	// text alone keeps it too; a new post uses the pattern as it is now.
	old := post(map[string]any{"text": "PR-5 done"})
	if len(referencesOf(old)) != 0 {
		t.Fatalf("no pattern yet: %v", old)
	}
	owner.Must(200, "PATCH", refPath+"/"+srv["id"].(string), map[string]any{"prefix": "PR-", "url_template": "https://new.example/{value}"})
	read := sam.Must(200, "GET", "/api/v1/messages/"+m["id"].(string), nil)["message"].(map[string]any)
	wantReference(t, "kept", read, 4, 5, "4521", "https://track.example/t/4521")
	path := "/api/v1/messages/" + m["id"].(string)
	same := owner.Must(200, "PATCH", path, map[string]any{"text": "See #4521, please."})["message"].(map[string]any)
	wantReference(t, "same text", same, 4, 5, "4521", "https://track.example/t/4521")
	keptPlain := owner.Must(200, "PATCH", "/api/v1/messages/"+old["id"].(string), map[string]any{"text": "PR-5 done"})["message"].(map[string]any)
	if len(referencesOf(keptPlain)) != 0 {
		t.Fatalf("an edit that leaves the text alone resolves nothing new: %v", keptPlain)
	}
	wantReference(t, "now", post(map[string]any{"text": "PR-6"}), 0, 4, "6", "https://new.example/6")
	edited := owner.Must(200, "PATCH", "/api/v1/messages/"+old["id"].(string), map[string]any{"text": "PR-5 done!"})["message"].(map[string]any)
	wantReference(t, "edited text", edited, 0, 4, "5", "https://new.example/5")
}
