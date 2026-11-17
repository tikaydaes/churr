package link_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Sending a private channel to another instance is a manager's decision
// (ruling R96), and it is said in the channel (review finding H3): the
// people in it learn that what they say now goes elsewhere, and who sent
// it. An Owner who is only a member there may not.
func TestSharingAPrivateChannelIsAManagersAndIsAnnounced(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a := l.Servers[0]
	owner := a.Owner
	ann := a.Member("ann")
	den := ann.Channel("den", true, owner)
	share := map[string]any{"conversation_id": den, "peer_id": l.Peers[0]}
	owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/streams", share)

	ann.Must(200, "PATCH", "/api/v1/conversations/"+den+"/members/"+string(owner.ID), map[string]any{"level": "manager"})
	l.Share(0, den, "den", nil, 1)
	var found map[string]any
	for _, m := range ann.Messages(den) {
		if s, ok := m["system"].(map[string]any); ok && s["kind"] == "channel_shared" {
			found = m
		}
	}
	if found == nil || found["author_id"] != string(owner.ID) || found["system"].(map[string]any)["by_id"] != string(owner.ID) ||
		found["system"].(map[string]any)["name"] != testkit.ServerName(1) {
		t.Fatalf("the notice in the channel: %v", ann.Messages(den))
	}
}
