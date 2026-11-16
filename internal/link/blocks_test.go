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
