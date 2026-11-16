package link_test

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// A block is deaf ears across servers too (ruling R77): someone on another
// server whom lee has blocked starts a chat with him and it looks like any
// chat to them; lee's server takes the offer and leaves him out, and he
// never sees it.
func TestBlockAcrossServers(t *testing.T) {
	s := shareBy(t, "direct")
	a, b := s.Servers[0], s.Servers[1]
	lee := b.Owner
	lee.Must(204, "PUT", "/api/v1/me/blocks/"+string(s.ann.ID), nil)

	chat := s.ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(lee.ID)}})["conversation"].(map[string]any)
	id := chat["id"].(string)
	if !hasMember(chat, lee.ID) || chat["read_only"] == true || chat["chat_state"] != nil {
		t.Fatalf("ann's chat with lee: %v", chat)
	}
	s.ann.Post(id, "hello lee")
	// b took the offer: the stream is active there, as for any chat.
	conv := s.personalConv(1, store.StreamAdhoc)
	testkit.Eventually(t, "a's stream is active", func() bool {
		st, err := a.Server.Store.Streams().ByConversation(ctx, model.ConversationID(id))
		return err == nil && st.State == store.StreamActive
	})
	// ann can go on writing; nothing is read-only or taken back.
	s.ann.Post(id, "still there?")
	if c := s.ann.Must(200, "GET", "/api/v1/conversations/"+id, nil)["conversation"].(map[string]any); !hasMember(c, lee.ID) || c["read_only"] == true {
		t.Fatalf("ann's chat later: %v", c)
	}
	// lee is not in b's copy, and it is not among his conversations.
	lee.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+conv, nil)
	for _, c := range lee.Must(200, "GET", "/api/v1/conversations", nil)["conversations"].([]any) {
		if c.(map[string]any)["id"] == conv {
			t.Fatal("lee lists the chat of someone he blocked")
		}
	}
	if _, member, _ := b.Server.Store.Members().Get(ctx, model.ConversationID(conv), lee.ID); member {
		t.Fatal("lee was made a member")
	}
}

func hasMember(c map[string]any, id model.AccountID) bool {
	ids, _ := c["member_ids"].([]any)
	for _, x := range ids {
		if x == string(id) {
			return true
		}
	}
	return false
}

// In a chat of two across servers, someone who blocked the other person is
// never shown typing to them: the notice does not leave their server.
func TestBlockedTypingAcrossServers(t *testing.T) {
	s := shareBy(t, "direct")
	lee := s.Servers[1].Owner
	chatA := s.ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(lee.ID)}})["conversation"].(map[string]any)["id"].(string)
	s.ann.Post(chatA, "hi")
	chatB := s.personalConv(1, store.StreamAdhoc)
	lee.HasMessage(chatB, "hi")
	annS, _ := s.ann.Events(nil)
	lee.Must(204, "POST", "/api/v1/conversations/"+chatB+"/typing", map[string]any{})
	annS.ExpectWhere("typing.started", func(f map[string]any) bool { return f["conversation_id"] == chatA })
	lee.Must(204, "PUT", "/api/v1/me/blocks/"+string(s.ann.ID), nil)
	lee.Must(204, "POST", "/api/v1/conversations/"+chatB+"/typing", map[string]any{})
	for {
		f, ok := annS.Next(700 * time.Millisecond)
		if !ok {
			break
		}
		if f["type"] == "typing.started" {
			t.Fatalf("ann was shown typing from lee, who blocked her: %v", f)
		}
	}
}

// Section 17a: a block a person here makes of someone of another server
// goes to that server alone, in a deaf_ears message that says, whole, whom
// each person here has blocked of its people. Lifting the block, pausing
// the link, a restart of the receiver (whose link to the hub comes back
// while the sender's never dropped): what the receiver holds is always what
// the sender has.
func TestDeafEarsAcrossServers(t *testing.T) {
	s := shareBy(t, "hub")
	a, b, c := s.Servers[0], s.Servers[1], s.Servers[2]
	lee, cat := b.Owner, c.Owner
	deaf := func(i int, viewer model.AccountID) bool { return s.link(i).DeafViewers(s.ann.ID)[viewer] }
	s.ann.Must(204, "PUT", "/api/v1/me/blocks/"+string(lee.ID), nil)
	s.ann.Must(204, "PUT", "/api/v1/me/blocks/"+string(cat.ID), nil)
	testkit.Eventually(t, "b and c hear of the blocks", func() bool { return deaf(1, lee.ID) && deaf(2, cat.ID) })
	if len(s.link(1).DeafViewers(s.ann.ID)) != 1 || len(s.link(2).DeafViewers(s.ann.ID)) != 1 {
		t.Fatalf("each server holds only its own people: b %v, c %v", s.link(1).DeafViewers(s.ann.ID), s.link(2).DeafViewers(s.ann.ID))
	}

	// Unblocked: b's is gone, c's stays.
	s.ann.Must(204, "DELETE", "/api/v1/me/blocks/"+string(lee.ID), nil)
	testkit.Eventually(t, "b hears of the unblock", func() bool { return !deaf(1, lee.ID) })
	if !deaf(2, cat.ID) {
		t.Fatal("c lost a block that stands")
	}

	// Lifted while a's link is paused: b is told when it comes back.
	s.ann.Must(204, "PUT", "/api/v1/me/blocks/"+string(lee.ID), nil)
	testkit.Eventually(t, "b hears of the block", func() bool { return deaf(1, lee.ID) })
	peerA := model.PeerID(s.Peers[0])
	if _, err := s.link(0).SetPeerState(ctx, a.Owner.ID, peerA, store.PeerPaused); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "a's link is down", func() bool { return !s.link(0).TestConnected(peerA) })
	s.ann.Must(204, "DELETE", "/api/v1/me/blocks/"+string(lee.ID), nil)
	time.Sleep(300 * time.Millisecond)
	if !deaf(1, lee.ID) {
		t.Fatal("b heard of the unblock with a's link down")
	}
	if _, err := s.link(0).SetPeerState(ctx, a.Owner.ID, peerA, store.PeerActive); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "b is told when a's link comes back", func() bool { return !deaf(1, lee.ID) })
	if !deaf(2, cat.ID) {
		t.Fatal("c lost a block that stands at the reconnection")
	}

	// b restarts and remembers nothing; a's link never dropped. b asks.
	s.ann.Must(204, "PUT", "/api/v1/me/blocks/"+string(lee.ID), nil)
	testkit.Eventually(t, "b hears of the block", func() bool { return deaf(1, lee.ID) })
	b.Restart()
	testkit.Eventually(t, "b is told again after its restart", func() bool { return deaf(1, lee.ID) })
}

// The receiver keeps what a deaf_ears message says only of its own people,
// only as the blocker's home server says it, and never an older message
// over a newer one.
func TestDeafEarsRefused(t *testing.T) {
	s := shareBy(t, "hub")
	b, c := s.Servers[1], s.Servers[2]
	lee := b.Owner
	bName := testkit.ServerName(1)
	s.ann.Must(204, "PUT", "/api/v1/me/blocks/"+string(lee.ID), nil)
	testkit.Eventually(t, "b hears of the block", func() bool { return s.link(1).DeafViewers(s.ann.ID)[lee.ID] })

	// c says ann has blocked no one, and that c's owner has blocked lee
	// and an account of a: b keeps c's word about c's owner and b's own
	// people only, and ann's block stands.
	peerC := model.PeerID(s.Peers[2])
	list := []map[string]any{
		{"account_id": s.ann.ID, "account_ids": []string{}},
		{"account_id": c.Owner.ID, "account_ids": []string{string(lee.ID), string(s.Servers[0].Owner.ID)}},
	}
	if _, err := s.link(2).TestDirect(peerC, bName, "deaf_ears", map[string]any{"deaf_ears": list}, model.Now(), nil); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "b takes c's word about c's own account", func() bool { return s.link(1).DeafViewers(c.Owner.ID)[lee.ID] })
	if v := s.link(1).DeafViewers(c.Owner.ID); len(v) != 1 {
		t.Fatalf("b holds an account of another server as blocked: %v", v)
	}
	if !s.link(1).DeafViewers(s.ann.ID)[lee.ID] {
		t.Fatal("c's word undid a block made on a")
	}
	// c says ann blocked lee: not c's to say. a's own, unblocking, stands.
	forged := []map[string]any{{"account_id": s.ann.ID, "account_ids": []string{string(lee.ID)}}}
	s.ann.Must(204, "DELETE", "/api/v1/me/blocks/"+string(lee.ID), nil)
	testkit.Eventually(t, "b hears of the unblock", func() bool { return !s.link(1).DeafViewers(s.ann.ID)[lee.ID] })
	if _, err := s.link(2).TestDirect(peerC, bName, "deaf_ears", map[string]any{"deaf_ears": forged}, model.Now(), nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if s.link(1).DeafViewers(s.ann.ID)[lee.ID] {
		t.Fatal("b took c's word for a block made on a")
	}
	// An older message from a itself does not undo a newer one.
	peerA := model.PeerID(s.Peers[0])
	s.ann.Must(204, "PUT", "/api/v1/me/blocks/"+string(lee.ID), nil)
	testkit.Eventually(t, "b hears of the block", func() bool { return s.link(1).DeafViewers(s.ann.ID)[lee.ID] })
	old := model.Now().Add(-30 * time.Second)
	if _, err := s.link(0).TestDirect(peerA, bName, "deaf_ears", map[string]any{"deaf_ears": []map[string]any{}}, old, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if !s.link(1).DeafViewers(s.ann.ID)[lee.ID] {
		t.Fatal("an older deaf_ears undid a newer one")
	}
}

// A server that joins a stream is sent deaf_ears there and then, whatever
// it held before; and every server is sent them again when blocking is
// turned off or on.
func TestDeafEarsOnJoin(t *testing.T) {
	s := shareBy(t, "direct")
	b := s.Servers[1]
	lee := b.Owner
	s.ann.Must(204, "PUT", "/api/v1/me/blocks/"+string(lee.ID), nil)
	testkit.Eventually(t, "b hears of the block", func() bool { return s.link(1).DeafViewers(s.ann.ID)[lee.ID] })
	s.link(1).TestForgetDeaf()
	ch := s.Servers[0].Owner.Channel("second", false, s.ann)
	s.Share(0, ch, "second", nil, 1)
	testkit.Eventually(t, "b is told again as it joins the new stream", func() bool { return s.link(1).DeafViewers(s.ann.ID)[lee.ID] })

	// Blocking turned off on a: no block of a's counts, and b is told so;
	// turned on again, they count again.
	s.Servers[0].Owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"blocking.enabled": false})
	testkit.Eventually(t, "b hears that blocking is off on a", func() bool { return !s.link(1).DeafViewers(s.ann.ID)[lee.ID] })
	s.Servers[0].Owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"blocking.enabled": true})
	testkit.Eventually(t, "b hears that blocking is on again", func() bool { return s.link(1).DeafViewers(s.ann.ID)[lee.ID] })
}
