package link_test

import (
	"testing"
)

// A shared channel is a conversation of each server like any other: a
// person puts it in their own sidebar sections on their own server (ruling
// R83).
func TestSidebarHoldsSharedChannels(t *testing.T) {
	s := share(t, 2, false, nil)
	owner := s.Servers[1].Owner
	local := s.conv[1]
	out := owner.Must(200, "POST", "/api/v1/me/sidebar", map[string]any{"version": 0, "ops": []any{
		map[string]any{"op": "create_section", "name": "Partners"},
		map[string]any{"op": "move", "conversation_id": local, "key": "p:1"},
	}})
	l := out["sidebar"].(map[string]any)
	p := l["placements"].([]any)
	if len(p) != 1 || p[0].(map[string]any)["conversation_id"] != local {
		t.Fatalf("the shared channel is placed: %v", l)
	}
}
