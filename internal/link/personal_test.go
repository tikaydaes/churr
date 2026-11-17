package link_test

import (
	"fmt"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// personalConv finds server i's copy of the one conversation of a kind
// (adhoc or sealed) it holds a stream for.
func (s *shared) personalConv(i int, kind string) string {
	s.t.Helper()
	var conv string
	testkit.Eventually(s.t, fmt.Sprintf("%s has an active %s stream", testkit.ServerName(i), kind), func() bool {
		streams, _ := s.Servers[i].Server.Store.Streams().List(ctx, store.StreamActive)
		for _, st := range streams {
			if st.Kind == kind && st.ConversationID != "" {
				conv = string(st.ConversationID)
				return true
			}
		}
		return false
	})
	return conv
}

func readOnly(c *testkit.Client, conv string) bool {
	return c.Must(200, "GET", "/api/v1/conversations/"+conv, nil)["conversation"].(map[string]any)["read_only"] == true
}

// Cases 53, 54 and 55, directly, through a hub, and across a trunk between
// two hubs of a cluster.
func TestAdhocAcrossServers(t *testing.T) {
	for _, how := range []string{"direct", "hub", "cluster"} {
		t.Run(how, func(t *testing.T) {
			s := shareBy(t, how)
			a, b := s.Servers[0], s.Servers[1]
			lee := s.keeper(1)
			// zed is known on a (he was in the shared channel) and shares
			// no channel with ann any more.
			zed := b.Member("zed")
			lee.Add(s.conv[1], "member", zed)
			testkit.Eventually(t, "a knows zed", func() bool { return s.isMember(0, zed.ID) })
			zed.Must(204, "POST", "/api/v1/conversations/"+s.conv[1]+"/leave", nil)
			testkit.Eventually(t, "a knows zed left", func() bool { return !s.isMember(0, zed.ID) })

			// 53: with someone they share a channel with, and not without.
			if status, out := s.ann.Do("POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(zed.ID)}}); status != 403 && status != 404 {
				t.Fatalf("a chat with someone ann shares no channel with: %d %v", status, out)
			}
			streams, _ := a.Server.Store.Streams().List(ctx, "")
			for _, st := range streams {
				if st.Kind != store.StreamChannel {
					t.Fatalf("a refused chat left a stream behind: %+v", st)
				}
			}
			chatA := s.ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(lee.ID)}})["conversation"].(map[string]any)["id"].(string)
			s.ann.Post(chatA, "hello lee")
			chatB := s.personalConv(1, store.StreamAdhoc)
			lee.HasMessage(chatB, "hello lee")
			lee.Post(chatB, "hello ann")
			s.ann.HasMessage(chatA, "hello ann")
			if conv := lee.Must(200, "GET", "/api/v1/conversations/"+chatB, nil)["conversation"].(map[string]any); conv["shared"] == nil || conv["name"] != nil && conv["name"] != "" {
				t.Fatalf("b's copy of the chat: %v", conv)
			}
			// The same two people get the same chat again, from either side.
			again := lee.Must(200, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(s.ann.ID)}})["conversation"].(map[string]any)["id"]
			if again != chatB {
				t.Fatalf("lee opening the chat with ann got %v, not %s", again, chatB)
			}
			// It cannot be given a name, and a remote account cannot be
			// put in a named channel.
			if status, _ := s.ann.Do("PATCH", "/api/v1/conversations/"+chatA, map[string]any{"name": "plans"}); status/100 == 2 {
				t.Fatal("a cross-server ad-hoc channel was given a name")
			}
			other := a.Owner.Channel("other", false, s.ann)
			if status, _ := a.Owner.Do("POST", "/api/v1/conversations/"+other+"/members", map[string]any{"account_ids": []string{string(lee.ID)}}); status != 403 {
				t.Fatalf("adding a remote account to a channel that is not shared: %d", status)
			}

			// Adding someone later: a member of a whose home server is in
			// the stream is added at once; a member of b is invited, and
			// b's server joins them.
			amy := a.Member("amy")
			bea := b.Member("bea")
			a.Owner.Add(s.conv[0], "member", amy)
			lee.Add(s.conv[1], "member", bea)
			testkit.Eventually(t, "a knows bea", func() bool { return s.isMember(0, bea.ID) })
			testkit.Eventually(t, "b knows amy", func() bool { return s.isMember(1, amy.ID) })
			s.ann.Must(200, "POST", "/api/v1/conversations/"+chatA+"/members", map[string]any{"account_ids": []string{string(amy.ID), string(bea.ID)}})
			member := func(i int, conv string, who model.AccountID) bool {
				_, found, _ := s.Servers[i].Server.Store.Members().Get(ctx, model.ConversationID(conv), who)
				return found
			}
			for _, who := range []model.AccountID{amy.ID, bea.ID} {
				testkit.Eventually(t, "the added member is in the chat on a", func() bool { return member(0, chatA, who) })
				testkit.Eventually(t, "the added member is in the chat on b", func() bool { return member(1, chatB, who) })
			}
			bea.Post(chatB, "bea is here")
			amy.HasMessage(chatA, "bea is here")
			// zed is invited by ann's server misbehaving (ann shares no
			// channel with him): b does nothing.
			s.submitTo(0, chatA, "member.invited", map[string]any{"account_id": zed.ID, "by_account_id": s.ann.ID, "server": "b.localhost"})
			s.ann.Post(chatA, "after the invitation")
			bea.HasMessage(chatB, "after the invitation")
			if member(1, chatB, zed.ID) {
				t.Fatal("b joined a member who shares no channel with the adder")
			}
			// A join nobody invited is rejected.
			yan := b.Member("yan")
			s.submitTo(1, chatB, "member.joined", map[string]any{"account": map[string]any{"id": yan.ID, "server": "b.localhost", "username": "yan",
				"display_name": "yan", "version": 1}, "blobs": []any{}})
			s.rejected(0, "not_invited", 1)

			// 54: b declines an offer naming an adder who shares no channel
			// with b's member, even though a sent it.
			profile := map[string]any{"id": s.ann.ID, "server": "a.localhost", "username": "ann", "display_name": "ann", "version": 1}
			kind, body, err := s.link(0).TestAskReply(model.PeerID(s.Peers[0]), "b.localhost", "offer", map[string]any{
				"stream_id": model.NewID[model.StreamID](), "stream_kind": "adhoc", "owner": "a.localhost", "servers": []string{"a.localhost"},
				"invited": []string{"b.localhost"}, "max_blob_bytes": 1 << 20, "by_account": profile, "account_ids": []string{string(zed.ID)},
				"members": []any{profile, map[string]any{"id": zed.ID, "server": "b.localhost", "username": "zed", "version": 1}}})
			if err != nil || kind != "decline" || body["reason"] != "not_allowed" {
				t.Fatalf("an offer naming someone the adder shares no channel with: %s %v %v", kind, body, err)
			}
			if streams, _ := b.Server.Store.Streams().List(ctx, store.StreamOffered); len(streams) != 0 {
				t.Fatalf("a declined offer left a stream on b: %+v", streams)
			}

			// 55: the last shared channel between them ends, and the chat
			// is read-only on both servers, for everyone in it.
			if err := a.Server.Link.Close(ctx, a.Owner.ID, s.stream); err != nil {
				t.Fatal(err)
			}
			testkit.Eventually(t, "the chat is read-only on a", func() bool { return readOnly(s.ann, chatA) })
			testkit.Eventually(t, "the chat is read-only on b", func() bool { return readOnly(lee, chatB) })
			s.ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+chatA+"/messages", map[string]any{"text": "anyone?"})
			lee.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+chatB+"/messages", map[string]any{"text": "anyone?"})
			msg := s.ann.HasMessage(chatA, "hello ann")["id"].(string)
			if status, _ := s.ann.Do("PUT", "/api/v1/messages/"+msg+"/reactions/%F0%9F%91%8D", nil); status != 403 {
				t.Fatalf("reacting in a read-only chat: %d", status)
			}
			// The history stays readable.
			lee.HasMessage(chatB, "hello lee")
			// A shared channel returns: writable again.
			back := a.Owner.Channel("back", false, s.ann, amy)
			_, local := s.Share(0, back, "back", nil, 1)
			bea.Must(200, "POST", "/api/v1/conversations/"+local[1]+"/join", nil)
			testkit.Eventually(t, "the chat is writable again on a", func() bool { return !readOnly(s.ann, chatA) })
			testkit.Eventually(t, "the chat is writable again on b", func() bool { return !readOnly(lee, chatB) })
			s.ann.Post(chatA, "back again")
			lee.HasMessage(chatB, "back again")
			lee.Post(chatB, "so we are")
			s.ann.HasMessage(chatA, "so we are")
		})
	}
}
