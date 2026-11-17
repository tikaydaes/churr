package link_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// A private channel stays private on every server it is shared with
// (ruling R87): the copy is private however it was accepted, and cannot be
// made public there. Through a hub: privatehub_test.go.
func TestSharedPrivateChannelStaysPrivate(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	ch := a.Owner.Channel("vault", true)
	_, local := l.Share(0, ch, "vault-here", map[string]any{"history_days": 30}, 1)
	c := b.Owner.Must(200, "GET", "/api/v1/conversations/"+local[1], nil)["conversation"].(map[string]any)
	if c["private"] != true {
		t.Fatalf("b's copy of a private channel: %v", c)
	}
	b.Owner.Refused(403, model.CodeForbidden, "PATCH", "/api/v1/conversations/"+local[1], map[string]any{"access": "public"})
	b.Owner.Refused(403, model.CodeForbidden, "PATCH", "/api/v1/conversations/"+local[1], map[string]any{"access": "knock"})
}
