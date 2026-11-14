package api_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// themeColours are the guide's dark base colours (docs/STYLE.md), with
// changes.
func themeColours(over ...string) map[string]any {
	m := map[string]any{"bg": "#0d1026", "panel": "#161a3d", "raise": "#1d2250", "ink": "#efeaff", "dim": "#a09dc9",
		"lav": "#b9a0ff", "cyan": "#5df0e4", "pink": "#ff5ccf", "yel": "#ffd95e"}
	for i := 0; i+1 < len(over); i += 2 {
		if over[i+1] == "" {
			delete(m, over[i])
		} else {
			m[over[i]] = over[i+1]
		}
	}
	return m
}

func lightColours() map[string]any {
	return map[string]any{"bg": "#f5f2ff", "panel": "#ffffff", "raise": "#ebe6ff", "ink": "#15173a", "dim": "#55527f",
		"lav": "#5b3fd6", "cyan": "#006e66", "pink": "#b3127f", "yel": "#ffd95e"}
}

func newTheme(c *testkit.Client, name string) map[string]any {
	return obj(c.Must(201, "POST", "/api/v1/me/themes", map[string]any{"name": name, "kind": "dark", "colours": themeColours()}), "theme")
}

func TestThemesOwnAndRefusals(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	guest := ts.Guest("gus", ann.Channel("lobby", false))
	bot := ts.Owner.Bot("robo")

	th := newTheme(ann, "  Night  ")
	id := th["id"].(string)
	if th["name"] != "Night" || th["kind"] != "dark" || th["published"] != false || th["owner_id"] != string(ann.ID) ||
		obj(th, "colours")["bg"] != "#0d1026" || th["created_at"] == nil || th["updated_at"] == nil {
		t.Fatalf("created: %v", th)
	}
	// Upper case is written back in lower case.
	up := obj(ann.Must(200, "PATCH", "/api/v1/me/themes/"+id, map[string]any{"name": "Night 2", "colours": themeColours("pink", "#FF5CCF")}), "theme")
	if up["name"] != "Night 2" || obj(up, "colours")["pink"] != "#ff5ccf" || up["kind"] != "dark" {
		t.Fatalf("updated: %v", up)
	}
	if mine := arr(ann.Must(200, "GET", "/api/v1/me/themes", nil), "themes"); len(mine) != 1 {
		t.Fatalf("mine: %v", mine)
	}
	if mine := arr(bob.Must(200, "GET", "/api/v1/me/themes", nil), "themes"); len(mine) != 0 {
		t.Fatalf("bob's: %v", mine)
	}

	// Case 261: anyone signed in with the link sees it, published or not.
	for _, c := range []*testkit.Client{bob, guest, bot} {
		if got := obj(c.Must(200, "GET", "/api/v1/themes/"+id, nil), "theme"); got["name"] != "Night 2" {
			t.Fatalf("by link: %v", got)
		}
	}
	ts.Browser().Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/themes/"+id, nil)
	bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/themes/"+string(model.NewID[model.ThemeID]()), nil)
	bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/themes/not-an-id", nil)

	// Case 260: not the owner.
	bob.Refused(404, model.CodeNotFound, "PATCH", "/api/v1/me/themes/"+id, map[string]any{"name": "mine now"})
	bob.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/themes/"+id, nil)
	bob.Refused(404, model.CodeNotFound, "POST", "/api/v1/me/themes/"+id+"/publish", nil)
	bob.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/themes/"+id+"/publish", nil)
	ann.Refused(404, model.CodeNotFound, "PATCH", "/api/v1/me/themes/"+string(model.NewID[model.ThemeID]()), map[string]any{"name": "x"})

	// Case 258: a bot owns no themes; a guest does.
	bot.Refused(403, model.CodeForbidden, "POST", "/api/v1/me/themes", map[string]any{"name": "b", "kind": "dark", "colours": themeColours()})
	bot.Refused(403, model.CodeForbidden, "GET", "/api/v1/me/themes", nil)
	bot.Refused(403, model.CodeForbidden, "GET", "/api/v1/themes", nil)
	bot.Refused(403, model.CodeForbidden, "POST", "/api/v1/themes/"+id+"/add", nil)
	gth := newTheme(guest, "Guest's")
	// Case 259: a guest does not publish.
	guest.Refused(403, model.CodeForbidden, "POST", "/api/v1/me/themes/"+gth["id"].(string)+"/publish", nil)

	// Shape.
	for _, c := range []struct {
		body  map[string]any
		field string
	}{
		{map[string]any{"kind": "dark", "colours": themeColours()}, "name"},
		{map[string]any{"name": " ", "kind": "dark", "colours": themeColours()}, "name"},
		{map[string]any{"name": strings.Repeat("a", 65), "kind": "dark", "colours": themeColours()}, "name"},
		{map[string]any{"name": "x", "colours": themeColours()}, "kind"},
		{map[string]any{"name": "x", "kind": "sepia", "colours": themeColours()}, "kind"},
		{map[string]any{"name": "x", "kind": "dark"}, "colours"},
		{map[string]any{"name": "x", "kind": "dark", "colours": themeColours("yel", "")}, "colours.yel"},
		{map[string]any{"name": "x", "kind": "dark", "colours": themeColours("accent", "#ffffff")}, "colours.accent"},
		{map[string]any{"name": "x", "kind": "dark", "colours": themeColours("ink", "#fff")}, "colours.ink"},
		{map[string]any{"name": "x", "kind": "dark", "colours": themeColours("ink", "white")}, "colours.ink"},
		// A dark theme with a light ground, and the other way round.
		{map[string]any{"name": "x", "kind": "dark", "colours": lightColours()}, "kind"},
		{map[string]any{"name": "x", "kind": "light", "colours": themeColours()}, "kind"},
	} {
		e := ann.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/me/themes", c.body)
		if e["field"] != c.field {
			t.Fatalf("%v: field %v, want %s", c.body, e["field"], c.field)
		}
	}
	if s, _ := ann.Do("POST", "/api/v1/me/themes", map[string]any{"name": "x", "kind": "dark", "colours": themeColours(), "accent": "pink"}); s != 400 {
		t.Fatalf("an unknown field: %d", s)
	}

	// Contrast, with the measured ratio.
	for _, c := range []struct {
		colours map[string]any
		message string
	}{
		{themeColours("dim", "#6a6890"), "dim on bg is 3.57:1; text needs at least 4.5:1."},
		{themeColours("pink", "#c0207f"), "pink on bg is 3.35:1; text needs at least 4.5:1."},
		{themeColours("raise", "#343a75"), "dim on raise is 4.06:1; text needs at least 4.5:1."},
		{themeColours("lav", "#7060c0"), "lav on bg is 3.68:1; text needs at least 4.5:1."},
		{themeColours("yel", "#5a5a20"), "yel on bg is 2.60:1; a control edge needs at least 3:1."},
	} {
		e := ann.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/me/themes", map[string]any{"name": "x", "kind": "dark", "colours": c.colours})
		if e["field"] != "colours" || e["message"] != c.message {
			t.Fatalf("contrast: %v, want %q", e, c.message)
		}
	}
	// A change is checked as a whole: a pale ink against the dark ground.
	e := ann.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/me/themes/"+id, map[string]any{"colours": themeColours("ink", "#3a3d70")})
	if e["field"] != "kind" && e["field"] != "colours" {
		t.Fatalf("a failing change: %v", e)
	}
	if got := obj(ann.Must(200, "GET", "/api/v1/themes/"+id, nil), "theme"); obj(got, "colours")["ink"] != "#efeaff" {
		t.Fatalf("a refused change was kept: %v", got)
	}
	// The light guide colours pass as a light theme.
	ann.Must(201, "POST", "/api/v1/me/themes", map[string]any{"name": "Day", "kind": "light", "colours": lightColours()})

	// Delete.
	ann.Must(204, "DELETE", "/api/v1/me/themes/"+id, nil)
	ann.Refused(404, model.CodeNotFound, "GET", "/api/v1/themes/"+id, nil)
	ann.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/themes/"+id, nil)
}

func TestThemesLimit(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	first := newTheme(ann, "t0")
	for i := 1; i < model.MaxThemesPerAccount; i++ {
		newTheme(ann, fmt.Sprintf("t%d", i))
	}
	ann.Refused(409, model.CodeConflict, "POST", "/api/v1/me/themes", map[string]any{"name": "one more", "kind": "dark", "colours": themeColours()})
	// Adding a copy counts too.
	ann.Refused(409, model.CodeConflict, "POST", "/api/v1/themes/"+first["id"].(string)+"/add", nil)
	ann.Must(204, "DELETE", "/api/v1/me/themes/"+first["id"].(string), nil)
	newTheme(ann, "room again")
}

func TestThemesLibrary(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	guest := ts.Guest("gus", ann.Channel("lobby", false))
	admin := ts.Admin("adm")
	stream, _ := bob.Events(nil)

	ocean := newTheme(ann, "Ocean Night")
	forest := newTheme(ann, "Forest")
	newTheme(ann, "Private one")
	pub := obj(ann.Must(200, "POST", "/api/v1/me/themes/"+ocean["id"].(string)+"/publish", nil), "theme")
	if pub["published"] != true || pub["published_at"] == nil {
		t.Fatalf("published: %v", pub)
	}
	if e := stream.Expect("theme.published"); obj(e, "theme")["id"] != ocean["id"] {
		t.Fatalf("theme.published: %v", e)
	}
	// Publishing again changes nothing.
	ann.Must(200, "POST", "/api/v1/me/themes/"+ocean["id"].(string)+"/publish", nil)
	time.Sleep(5 * time.Millisecond)
	ann.Must(200, "POST", "/api/v1/me/themes/"+forest["id"].(string)+"/publish", nil)
	if e := stream.Expect("theme.published"); obj(e, "theme")["id"] != forest["id"] {
		t.Fatalf("the second theme.published (the repeat sent one?): %v", e)
	}

	// Paged, newest first, and searchable; the guest browses too.
	page := guest.Must(200, "GET", "/api/v1/themes?limit=1", nil)
	if l := arr(page, "themes"); len(l) != 1 || l[0].(map[string]any)["id"] != forest["id"] || page["next_cursor"] == nil {
		t.Fatalf("first page: %v", page)
	}
	page = guest.Must(200, "GET", "/api/v1/themes?limit=1&cursor="+page["next_cursor"].(string), nil)
	if l := arr(page, "themes"); len(l) != 1 || l[0].(map[string]any)["id"] != ocean["id"] || page["next_cursor"] != nil {
		t.Fatalf("second page: %v", page)
	}
	if l := arr(bob.Must(200, "GET", "/api/v1/themes?q=ocean", nil), "themes"); len(l) != 1 || l[0].(map[string]any)["id"] != ocean["id"] {
		t.Fatalf("search: %v", l)
	}
	if l := arr(bob.Must(200, "GET", "/api/v1/themes?q=private", nil), "themes"); len(l) != 0 {
		t.Fatalf("an unpublished theme in the library: %v", l)
	}
	bob.Refused(400, model.CodeInvalidValue, "GET", "/api/v1/themes?cursor=nonsense", nil)

	// Case 262: a guest adds a copy, which is theirs and not published.
	cp := obj(guest.Must(201, "POST", "/api/v1/themes/"+ocean["id"].(string)+"/add", nil), "theme")
	if cp["id"] == ocean["id"] || cp["owner_id"] != string(guest.ID) || cp["published"] != false || cp["name"] != "Ocean Night" ||
		obj(cp, "colours")["bg"] != "#0d1026" {
		t.Fatalf("copy: %v", cp)
	}
	guest.Refused(404, model.CodeNotFound, "POST", "/api/v1/themes/"+string(model.NewID[model.ThemeID]())+"/add", nil)
	// Any theme by its link, published or not.
	priv := newTheme(ann, "Linked only")
	bob.Must(201, "POST", "/api/v1/themes/"+priv["id"].(string)+"/add", nil)

	// A change to a published theme is news to the library.
	ann.Must(200, "PATCH", "/api/v1/me/themes/"+forest["id"].(string), map[string]any{"name": "Forest 2"})
	if e := stream.Expect("theme.published"); obj(e, "theme")["name"] != "Forest 2" {
		t.Fatalf("a change: %v", e)
	}
	// Unpublish.
	out := obj(ann.Must(200, "DELETE", "/api/v1/me/themes/"+forest["id"].(string)+"/publish", nil), "theme")
	if out["published"] != false || out["published_at"] != nil {
		t.Fatalf("unpublished: %v", out)
	}
	if e := stream.Expect("theme.unpublished"); e["theme_id"] != forest["id"] {
		t.Fatalf("theme.unpublished: %v", e)
	}

	// Case 263: removing from the library is for admins, and audited.
	bob.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/themes/"+ocean["id"].(string), nil)
	admin.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/admin/themes/"+forest["id"].(string), nil) // not in the library
	admin.Must(204, "DELETE", "/api/v1/admin/themes/"+ocean["id"].(string), nil)
	if e := stream.Expect("theme.unpublished"); e["theme_id"] != ocean["id"] {
		t.Fatalf("removed: %v", e)
	}
	if !auditedFor(t, ts, "theme.removed", ocean["id"].(string)) {
		t.Fatal("the removal is not in the audit log")
	}
	if got := obj(ann.Must(200, "GET", "/api/v1/themes/"+ocean["id"].(string), nil), "theme"); got["published"] != false {
		t.Fatalf("the owner keeps it, unpublished: %v", got)
	}
	// Deleting a published theme takes it out of the library.
	ann.Must(200, "POST", "/api/v1/me/themes/"+forest["id"].(string)+"/publish", nil)
	stream.Expect("theme.published")
	ann.Must(204, "DELETE", "/api/v1/me/themes/"+forest["id"].(string), nil)
	if e := stream.Expect("theme.unpublished"); e["theme_id"] != forest["id"] {
		t.Fatalf("deleted: %v", e)
	}
	if l := arr(bob.Must(200, "GET", "/api/v1/themes", nil), "themes"); len(l) != 0 {
		t.Fatalf("the library after: %v", l)
	}
}

func TestThemeDefaults(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	admin := ts.Admin("adm")
	stream, _ := ann.Events(nil)
	dark := newTheme(ann, "House dark")
	light := obj(ann.Must(201, "POST", "/api/v1/me/themes", map[string]any{"name": "House light", "kind": "light", "colours": lightColours()}), "theme")

	before := obj(ts.Browser().Must(200, "GET", "/api/v1/server", nil), "server")
	if d, ok := before["default_themes"].(map[string]any); !ok || len(d) != 0 {
		t.Fatalf("no defaults yet: %v", before["default_themes"])
	}

	// Case 263.
	ann.Refused(403, model.CodeForbidden, "PUT", "/api/v1/admin/theme-defaults", map[string]any{"kind": "dark", "theme_id": dark["id"]})
	if e := admin.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/admin/theme-defaults", map[string]any{"kind": "dark", "theme_id": light["id"]}); e["field"] != "theme_id" {
		t.Fatalf("a light theme as the dark default: %v", e)
	}
	if e := admin.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/admin/theme-defaults", map[string]any{"kind": "sepia"}); e["field"] != "kind" {
		t.Fatalf("kind: %v", e)
	}
	admin.Refused(404, model.CodeNotFound, "PUT", "/api/v1/admin/theme-defaults", map[string]any{"kind": "dark", "theme_id": string(model.NewID[model.ThemeID]())})

	out := admin.Must(200, "PUT", "/api/v1/admin/theme-defaults", map[string]any{"kind": "dark", "theme_id": dark["id"]})
	if d := obj(out, "default_themes", "dark"); d["id"] != dark["id"] || d["name"] != "House dark" || d["author_id"] != string(ann.ID) {
		t.Fatalf("set: %v", out)
	}
	if e := stream.Expect("server.updated"); obj(e, "server", "default_themes", "dark")["id"] != dark["id"] {
		t.Fatalf("server.updated: %v", e)
	}
	admin.Must(200, "PUT", "/api/v1/admin/theme-defaults", map[string]any{"kind": "light", "theme_id": light["id"]})
	stream.Expect("server.updated")
	if !auditedFor(t, ts, "theme.default_set", dark["id"].(string)) {
		t.Fatal("not in the audit log")
	}

	// Before sign-in, with the colours.
	srv := obj(ts.Browser().Must(200, "GET", "/api/v1/server", nil), "server")
	d, l := obj(srv, "default_themes", "dark"), obj(srv, "default_themes", "light")
	if d["kind"] != "dark" || obj(d, "colours")["bg"] != "#0d1026" || l["kind"] != "light" || obj(l, "colours")["bg"] != "#f5f2ff" {
		t.Fatalf("defaults in the description: %v", srv["default_themes"])
	}

	// A copy: it stays when its theme goes.
	ann.Must(204, "DELETE", "/api/v1/me/themes/"+dark["id"].(string), nil)
	if d := obj(ts.Browser().Must(200, "GET", "/api/v1/server", nil), "server", "default_themes", "dark"); d["name"] != "House dark" {
		t.Fatalf("the default went with its theme: %v", d)
	}

	// Cleared.
	out = admin.Must(200, "PUT", "/api/v1/admin/theme-defaults", map[string]any{"kind": "dark"})
	if _, still := obj(out, "default_themes")["dark"]; still {
		t.Fatalf("cleared: %v", out)
	}
	if e := stream.Expect("server.updated"); obj(e, "server", "default_themes")["dark"] != nil {
		t.Fatalf("server.updated after clearing: %v", e)
	}
	if obj(ts.Browser().Must(200, "GET", "/api/v1/server", nil), "server", "default_themes")["dark"] != nil {
		t.Fatal("still a dark default")
	}
	list, _, err := ts.Server.Store.Audit().List(context.Background(), store.AuditFilter{Action: "theme.default_cleared"}, store.Page{})
	if err != nil || len(list) != 1 || list[0].Detail["kind"] != "dark" {
		t.Fatalf("clearing is not in the audit log: %v %v", list, err)
	}
}

// A link to one of the server's own themes gets a theme card through the
// link-preview path, with no fetch (link previews are off on a new server),
// and it is removed like a preview.
func TestThemeCardOnALink(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ch := ann.Channel("looks", false, bob)
	th := newTheme(ann, "Neon dusk")
	id := th["id"].(string)
	link := ts.Origin + "/themes/" + id
	stream, _ := bob.Events(nil)

	m := ann.Post(ch, "try this: "+link+" and https://elsewhere.example/themes/"+id+" and "+ts.Origin+"/themes/"+string(model.NewID[model.ThemeID]()))
	e := stream.ExpectWhere("preview.added", func(f map[string]any) bool { return obj(f, "message")["id"] == m["id"] })
	cards := arr(obj(e, "message"), "cards")
	if len(cards) != 1 {
		t.Fatalf("one theme card, nothing for the others: %v", cards)
	}
	card := cards[0].(map[string]any)
	tc := obj(card, "theme")
	if card["preview_url"] != link || card["title"] != "Neon dusk" || card["url"] != link ||
		tc["id"] != id || tc["name"] != "Neon dusk" || tc["kind"] != "dark" || tc["author_id"] != string(ann.ID) ||
		obj(tc, "colours")["cyan"] != "#5df0e4" || len(obj(tc, "colours")) != 9 {
		t.Fatalf("theme card: %v", card)
	}
	// Stored on the message.
	got := obj(bob.Must(200, "GET", "/api/v1/messages/"+m["id"].(string), nil), "message")
	if len(arr(got, "cards")) != 1 {
		t.Fatalf("stored: %v", got)
	}
	// Nobody posts a theme card of their own, not even a bot, which may
	// post cards.
	robo := ts.Owner.Bot("robo")
	ann.Add(ch, "", robo)
	robo.Refused(400, model.CodeInvalidValue, "POST", conv+ch+"/messages", map[string]any{"cards": []any{map[string]any{"title": "fake", "theme": tc}}})
	// Removed like a preview: not by bob, by the author.
	bob.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/messages/"+m["id"].(string)+"/previews/0", nil)
	ann.Must(200, "DELETE", "/api/v1/messages/"+m["id"].(string)+"/previews/0", nil)
	stream.Expect("preview.removed")
	// Sealed chats get none: the server cannot read them. A plain chat does.
	dm := convOf(ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID)}}))["id"].(string)
	m2 := ann.Post(dm, link)
	e = stream.ExpectWhere("preview.added", func(f map[string]any) bool { return obj(f, "message")["id"] == m2["id"] })
	if obj(arr(obj(e, "message"), "cards")[0].(map[string]any), "theme")["id"] != id {
		t.Fatalf("in a chat: %v", e)
	}
}

// Fix: the thread summary carries the order of the newest reply.
func TestThreadSummaryCarriesLastReplyOrder(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ch := ann.Channel("threads", false, bob)
	p := ann.Post(ch, "parent")
	r1 := obj(bob.Must(201, "POST", conv+ch+"/messages", map[string]any{"text": "one", "parent_id": p["id"]}), "message")
	r2 := obj(ann.Must(201, "POST", conv+ch+"/messages", map[string]any{"text": "two", "parent_id": p["id"]}), "message")
	th := obj(bob.Must(200, "GET", "/api/v1/messages/"+p["id"].(string), nil), "message", "thread")
	if th["last_reply_order"] != r2["order"] || th["last_reply_seq"] != r2["seq"] {
		t.Fatalf("summary: %v, reply %v", th, r2)
	}
	ann.Must(200, "DELETE", "/api/v1/messages/"+r2["id"].(string), nil)
	th = obj(bob.Must(200, "GET", "/api/v1/messages/"+p["id"].(string), nil), "message", "thread")
	if th["last_reply_order"] != r1["order"] {
		t.Fatalf("after deleting the newest: %v, want %v", th, r1["order"])
	}
}
