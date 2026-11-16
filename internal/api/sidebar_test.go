package api_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// A person's own sidebar (ruling R83): read whole, changed by small ops
// against a version, kept for every device, told to the others live.
func TestSidebar(t *testing.T) {
	ts := testkit.NewServer(t)
	adm := ts.Admin("ada")
	ann, bob := ts.Member("ann"), ts.Member("bob")
	town := ann.Channel("town", false)
	den := ann.Channel("den", true)
	bobs := bob.Channel("bobs", true)
	sec := str(obj(adm.Must(201, "POST", "/api/v1/admin/sections", map[string]any{"name": "Teams"}), "section"), "id")

	const path = "/api/v1/me/sidebar"
	change := func(c *testkit.Client, version float64, ops ...map[string]any) map[string]any {
		return obj(c.Must(200, "POST", path, map[string]any{"version": version, "ops": ops}), "sidebar")
	}
	l := obj(ann.Must(200, "GET", path, nil), "sidebar")
	if l["version"] != float64(0) || len(arr(l, "sections")) != 0 || len(arr(l, "stars")) != 0 {
		t.Fatalf("an untouched sidebar is the server's default at version 0: %v", l)
	}

	// Two sessions of ann: what one does, the other sees at once.
	other, _ := ann.Events(nil)
	l = change(ann, 0,
		map[string]any{"op": "create_section", "name": "Focus"},
		map[string]any{"op": "move", "conversation_id": den, "key": "p:1"},
		map[string]any{"op": "move", "conversation_id": town, "key": "p:1", "ids": []string{den, town}},
		map[string]any{"op": "set_sort", "key": "chats", "sort": "alphabetical"},
		map[string]any{"op": "move_section", "key": "s:" + sec, "index": 0},
	)
	if l["version"] != float64(1) || arr(l, "sections")[0].(map[string]any)["name"] != "Focus" ||
		len(arr(l, "placements")) != 2 || fmt.Sprint(arr(l, "headings")) != "[s:"+sec+" p:1 s:]" {
		t.Fatalf("after the first change: %v", l)
	}
	e := other.Expect("sidebar.changed")
	if obj(e, "sidebar")["version"] != float64(1) {
		t.Fatalf("sidebar.changed: %v", e)
	}
	if got := obj(ann.Must(200, "GET", path, nil), "sidebar"); got["version"] != float64(1) || len(arr(got, "groups")) != 2 {
		t.Fatalf("kept: %v", got)
	}
	if got := obj(bob.Must(200, "GET", path, nil), "sidebar"); got["version"] != float64(0) {
		t.Fatalf("nobody else's sidebar moves: %v", got)
	}

	// A change made against an older version is refused, and changes
	// nothing.
	ann.Refused(409, model.CodeConflict, "POST", path, map[string]any{"version": 0, "ops": []any{map[string]any{"op": "reset_all"}}})
	ann.Refused(400, model.CodeInvalidValue, "POST", path, map[string]any{"ops": []any{map[string]any{"op": "reset_all"}}})

	// Refusals: unknown headings, other people's conversations, bad names
	// and sorts, and only one's own sections renamed or deleted.
	for _, op := range []map[string]any{
		{"op": "move", "conversation_id": bobs, "key": "p:1"},
		{"op": "move", "conversation_id": town, "key": "p:9"},
		{"op": "reorder", "key": "p:1", "ids": []string{bobs}},
		{"op": "create_section", "name": strings.Repeat("x", 65)},
		{"op": "create_section", "name": "  "},
		{"op": "set_sort", "key": "p:1", "sort": "random"},
		{"op": "rename_section", "key": "s:" + sec, "name": "Mine"},
		{"op": "delete_section", "key": "chats"},
		{"op": "move_section", "key": "sealed", "index": 0},
		{"op": "fly"},
	} {
		ann.Refused(400, model.CodeInvalidValue, "POST", path, map[string]any{"version": 1, "ops": []any{op}})
	}
	ann.Refused(400, model.CodeInvalidValue, "POST", path, map[string]any{"version": 1, "ops": []any{}})

	// Starring keeps the order of stars, and changes the version.
	ann.Must(200, "PATCH", "/api/v1/conversations/"+town+"/membership", map[string]any{"starred": true})
	ann.Must(200, "PATCH", "/api/v1/conversations/"+den+"/membership", map[string]any{"starred": true})
	l = obj(other.Expect("sidebar.changed"), "sidebar")
	l = obj(other.Expect("sidebar.changed"), "sidebar")
	if fmt.Sprint(arr(l, "stars")) != fmt.Sprint([]any{town, den}) || l["version"] != float64(3) {
		t.Fatalf("stars in the order starred: %v", l)
	}
	l = change(ann, 3, map[string]any{"op": "order_stars", "ids": []string{den}})
	if fmt.Sprint(arr(l, "stars")) != fmt.Sprint([]any{den, town}) {
		t.Fatalf("stars reordered: %v", l)
	}

	// Deleting a section sends its conversations back; reset_all goes back
	// to the default and keeps the stars.
	l = change(ann, 4, map[string]any{"op": "rename_section", "key": "p:1", "name": "Deep work"},
		map[string]any{"op": "reset_section", "key": "chats"})
	if arr(l, "sections")[0].(map[string]any)["name"] != "Deep work" || len(arr(l, "groups")) != 1 {
		t.Fatalf("renamed, chats reset: %v", l)
	}
	l = change(ann, 5, map[string]any{"op": "delete_section", "key": "p:1"})
	if len(arr(l, "sections")) != 0 || len(arr(l, "placements")) != 0 || len(arr(l, "groups")) != 0 {
		t.Fatalf("deleted with its placements: %v", l)
	}
	l = change(ann, 6, map[string]any{"op": "reset_all"})
	if len(arr(l, "headings")) != 0 || len(arr(l, "stars")) != 2 {
		t.Fatalf("reset: %v", l)
	}

	// Leaving a conversation drops it at the next change.
	l = change(ann, 7, map[string]any{"op": "create_section", "name": "A"}, map[string]any{"op": "move", "conversation_id": town, "key": "p:1"})
	ann.Must(204, "POST", "/api/v1/conversations/"+town+"/leave", nil)
	l = change(ann, 8, map[string]any{"op": "create_section", "name": "B"})
	if len(arr(l, "placements")) != 0 || fmt.Sprint(arr(l, "stars")) != fmt.Sprint([]any{den}) {
		t.Fatalf("a conversation left is dropped: %v", l)
	}

	// At most 50 sections of one's own, and 50 changes a call.
	var ops []any
	for i := 0; i < 48; i++ {
		ops = append(ops, map[string]any{"op": "create_section", "name": fmt.Sprint("S", i)})
	}
	l = change(ann, 9, toMaps(ops)...)
	if len(arr(l, "sections")) != 50 {
		t.Fatalf("50 sections: %d", len(arr(l, "sections")))
	}
	ann.Refused(400, model.CodeInvalidValue, "POST", path, map[string]any{"version": 10, "ops": []any{map[string]any{"op": "create_section", "name": "one more"}}})
	ann.Refused(400, model.CodeInvalidValue, "POST", path, map[string]any{"version": 10, "ops": append(ops, ops...)})
}

func toMaps(in []any) []map[string]any {
	out := make([]map[string]any, len(in))
	for i, v := range in {
		out[i] = v.(map[string]any)
	}
	return out
}
