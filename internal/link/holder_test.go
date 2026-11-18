package link_test

import (
	"context"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// heldMessages is what the hub's holder keeps of a channel: its messages by
// text, as they stand now.
func heldMessages(t *testing.T, hub *testkit.TestHub, stream string) (model.Conversation, map[string]model.Message) {
	t.Helper()
	ctx := context.Background()
	st := hub.Hub.HolderStore()
	if st == nil {
		t.Fatal("the hub has no holder")
	}
	out := map[string]model.Message{}
	row, err := st.Streams().Get(ctx, model.StreamID(stream))
	if err != nil || row.ConversationID == "" {
		return model.Conversation{}, out
	}
	conv, err := st.Conversations().Get(ctx, row.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	msgs, _, err := st.Messages().List(ctx, conv.ID, store.MessageList{SeqPage: store.SeqPage{Limit: 200}})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		key := m.Text
		if m.Deleted() {
			key = "deleted:" + string(m.ID)
		}
		out[key] = m
	}
	return conv, out
}

// The home hub keeps a channel it owns whole, in a database of its own: what
// was said before the channel was shared (which the instance that shared it
// gives the hub), and every message since as it now stands, edited or
// deleted, with its reactions, the channel's members and their profiles. It
// is in no record's list of instances.
func TestTheHubHoldsTheWholeChannel(t *testing.T) {
	l := testkit.NewLinked(t, 2, true)
	a, b := l.Servers[0], l.Servers[1]
	ann := a.Member("ann")
	ch := a.Owner.Channel("ledger", false, ann)
	a.Owner.Post(ch, "said before the channel was shared")
	stream, local := l.Share(0, ch, "ledger", map[string]any{"listing": "open", "history_days": 1}, 1)
	ann.Post(ch, "from ann")
	edited := b.Owner.Post(local[1], "from b, before the edit")
	gone := a.Owner.Post(ch, "deleted for everyone")
	b.Owner.Must(200, "PATCH", "/api/v1/messages/"+edited["id"].(string), map[string]any{"text": "from b, edited"})
	b.Owner.HasMessage(local[1], "deleted for everyone")
	b.Owner.Must(200, "PUT", "/api/v1/messages/"+gone["id"].(string)+"/reactions/👍", nil)
	a.Owner.Must(200, "DELETE", "/api/v1/messages/"+gone["id"].(string), nil)
	reacted := b.Owner.HasMessage(local[1], "from ann")
	b.Owner.Must(200, "PUT", "/api/v1/messages/"+reacted["id"].(string)+"/reactions/🎉", nil)
	a.Owner.HasMessage(ch, "from b, edited")

	var held map[string]model.Message
	var conv model.Conversation
	testkit.Eventually(t, "the hub holds every message as it stands", func() bool {
		conv, held = heldMessages(t, l.Hub, stream)
		_, before := held["said before the channel was shared"]
		_, fromAnn := held["from ann"]
		_, edit := held["from b, edited"]
		_, del := held["deleted:"+gone["id"].(string)]
		return before && fromAnn && edit && del
	})
	if _, stale := held["from b, before the edit"]; stale {
		t.Error("the hub holds the text from before the edit as the message")
	}
	if held["from b, edited"].ID != model.MessageID(edited["id"].(string)) {
		t.Error("the edited message has another id on the hub")
	}
	ctx := context.Background()
	st := l.Hub.Hub.HolderStore()
	testkit.Eventually(t, "the hub holds the reaction", func() bool {
		rs, err := st.Reactions().List(ctx, held["from ann"].ID, "")
		return err == nil && len(rs) == 1 && rs[0].Emoji == "🎉"
	})
	members, err := st.Members().IDs(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[model.AccountID]bool{a.Owner.ID: true, ann.ID: true, b.Owner.ID: true}
	for _, id := range members {
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("the hub's copy lacks members %v; it has %v", want, members)
	}
	acc, err := st.Accounts().Get(ctx, ann.ID)
	if err != nil || acc.Username != "ann" || acc.ServerName != "a.localhost" {
		t.Fatalf("the hub holds ann as %+v (%v)", acc, err)
	}
	if rec := hubChannel(a.Owner, stream); names(rec["servers"]) != "a.localhost,b.localhost" {
		t.Fatalf("the channel's instances are %v", rec["servers"])
	}
	if r := l.Hub.Hub.Holder().StatsNow().Rejected; len(r) != 0 {
		t.Fatalf("the hub's holder rejected %v", r)
	}
}

// A joining instance is given its history by the channel's home hub, and by
// nobody else: with every other instance of the channel switched off, it is
// given all it is owed, and who said it.
func TestAJoinerTakesHistoryFromTheHubAlone(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	ch := a.Owner.Channel("annals", false)
	a.Owner.Post(ch, "from a, before it was shared")
	stream, local := l.Share(0, ch, "annals", map[string]any{"listing": "open", "history_days": 30}, 1)
	a.Owner.Post(ch, "from a, on the hub")
	said := b.Owner.Post(local[1], "from b, on the hub")
	a.Owner.HasMessage(ch, "from b, on the hub")
	testkit.Eventually(t, "the hub holds both", func() bool {
		_, held := heldMessages(t, l.Hub, stream)
		_, fromA := held["from a, on the hub"]
		_, fromB := held["from b, on the hub"]
		return fromA && fromB
	})
	a.Stop()
	b.Stop()
	convC := joinListed(l, 2, stream, "annals-c")
	l.Joined(2, stream)
	for _, text := range []string{"from a, before it was shared", "from a, on the hub", "from b, on the hub"} {
		c.Owner.HasMessage(convC, text)
	}
	author := c.Owner.Must(200, "GET", "/api/v1/accounts/"+said["author_id"].(string), nil)["account"].(map[string]any)
	if author["username"] != "owner" || author["server_name"] != "b.localhost" {
		t.Fatalf("c shows b's author as %v", author)
	}
	if d := c.Server.Link.StatsNow().Dropped; d["history_not_from_home"] != 0 || d["history_unasked"] != 0 {
		t.Fatalf("c dropped history: %v", d)
	}
}
