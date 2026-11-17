package link_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// A guest is never in a channel shared with other instances (ruling R99):
// a channel with a guest in it is not shared until the guest is removed,
// and a guest is not added to a shared one, nor named on an invite to one.
func TestGuestsAndSharing(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a := l.Servers[0]
	owner := a.Owner
	room := owner.Channel("room", false)
	gil := a.Guest("gil", room)
	owner.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/streams", map[string]any{"conversation_id": room, "peer_id": l.Peers[0]})
	owner.Must(204, "DELETE", "/api/v1/conversations/"+room+"/members/"+string(gil.ID), nil)
	l.Share(0, room, "room", nil, 1)
	owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+room+"/members", map[string]any{"account_ids": []string{string(gil.ID)}})
	owner.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/invites", map[string]any{"role": "guest", "conversation_ids": []string{room}})
}
