package link_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/sealed/ref"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// shows waits until a device shows exactly these texts in a sealed chat.
func shows(t *testing.T, who string, c *testkit.SealedChat, want ...string) {
	t.Helper()
	var got []string
	testkit.Eventually(t, who+" shows the chat", func() bool {
		c.Sync()
		got = c.Texts()
		return reflect.DeepEqual(got, want)
	})
	_ = got
}

func sealedBody(out *ref.Outgoing) map[string]any {
	return map[string]any{"client_id": out.ClientID.String(), "quiet": out.Quiet,
		"sealed": map[string]any{"key_epoch": out.KeyEpoch, "ciphertext": model.EncodeBytes(out.Ciphertext)}}
}

// Cases 56, 57 and 57a, and the sealed half of 55: a sealed chat between
// accounts on two linked servers, end to end with the reference client,
// directly and through a hub, including a device added on one side.
func TestSealedAcrossServers(t *testing.T) {
	for _, how := range []string{"direct", "hub", "cluster"} {
		t.Run(how, func(t *testing.T) {
			hub := how != "direct"
			s := shareBy(t, how)
			a, b := s.Servers[0], s.Servers[1]
			sam, lee := s.ann, b.Owner
			samPhone := sam.Sealed("android")
			phrase := samPhone.CreateBackup()
			leePhone := lee.Sealed("ios")
			leePhone.CreateBackup()

			// 57: not with a bot, and not with someone sam shares no
			// channel with.
			bot := lee.Bot("robo")
			lee.Add(s.conv[1], "member", bot)
			zed := b.Member("zed")
			lee.Add(s.conv[1], "member", zed)
			testkit.Eventually(t, "a knows the bot and zed", func() bool { return s.isMember(0, bot.ID) && s.isMember(0, zed.ID) })
			zed.Must(204, "POST", "/api/v1/conversations/"+s.conv[1]+"/leave", nil)
			testkit.Eventually(t, "a knows zed left", func() bool { return !s.isMember(0, zed.ID) })
			if status, out := sam.Do("POST", "/api/v1/sealed", map[string]any{"account_id": zed.ID, "epoch_record": model.EncodeBytes([]byte("x"))}); status != 403 && status != 404 {
				t.Fatalf("a sealed chat with someone sam shares no channel with: %d %v", status, out)
			}
			// A server does not know which of another server's accounts
			// are bots. The bot's own server says no: to the request for
			// its device list, which comes first, and to the offer.
			if err := a.Server.Link.RequestDeviceLists(ctx, bot.ID, sam.ID, 0); model.CodeOf(err) != model.CodeForbidden {
				t.Fatalf("a bot's device list was asked for: %v", err)
			}

			// Starting it: lee's device list is fetched across the link,
			// epoch record 1 is sealed to both people's devices, and b
			// makes its copy under the same identifier.
			sa := samPhone.Open(lee.ID)
			conv := model.ConversationID(sa.ID)
			testkit.Eventually(t, "b holds the chat in an active stream", func() bool {
				c, err := b.Server.Store.Conversations().Get(ctx, conv)
				if err != nil || !c.Sealed() || c.KeyEpoch == nil {
					return false
				}
				st, err := b.Server.Store.Streams().Get(ctx, c.StreamID)
				return err == nil && st.State == store.StreamActive && st.Kind == store.StreamSealed
			})
			la := leePhone.Chat(sa.ID, sam.ID)
			// 57a: epoch record 1 is held by both servers, each under its
			// own arrival number.
			epoch := func(i int, key int64) model.EpochRecord {
				row, _, _ := s.Servers[i].Server.Store.Sealed().Epoch(ctx, conv, key)
				return row
			}
			testkit.Eventually(t, "both servers hold epoch record 1", func() bool {
				ea, eb := epoch(0, 1), epoch(1, 1)
				return ea.Seq != 0 && eb.Seq != 0 && len(ea.Record) > 0 && bytes.Equal(ea.Record, eb.Record)
			})

			sa.Say("the first secret")
			shows(t, "lee", la, "the first secret")
			la.Say("the second secret")
			shows(t, "sam", sa, "the first secret", "the second secret")

			// 57: one sealed chat per pair. From the other side the same
			// chat comes back; a second offer for the pair is declined.
			if again := leePhone.Open(sam.ID); again.ID != sa.ID {
				t.Fatalf("lee opening a sealed chat with sam got %s, not %s", again.ID, sa.ID)
			}
			profile := map[string]any{"id": sam.ID, "server": "a.localhost", "username": "ann", "display_name": "ann", "version": 1}
			kind, body, err := s.link(0).TestAskReply(model.PeerID(s.Peers[0]), "b.localhost", "offer", map[string]any{
				"stream_id": model.NewID[model.StreamID](), "stream_kind": "sealed", "owner": "a.localhost", "servers": []string{"a.localhost"},
				"invited": []string{"b.localhost"}, "max_blob_bytes": 1 << 20, "by_account": profile, "account_ids": []string{string(lee.ID)},
				"members":      []any{profile, map[string]any{"id": lee.ID, "server": "b.localhost", "username": "owner", "version": 1}},
				"epoch_record": model.EncodeBytes(epoch(0, 1).Record)})
			if err != nil || kind != "decline" || body["reason"] != "not_allowed" {
				t.Fatalf("a second sealed chat for the pair: %s %v %v", kind, body, err)
			}
			kind, body, err = s.link(0).TestAskReply(model.PeerID(s.Peers[0]), "b.localhost", "offer", map[string]any{
				"stream_id": model.NewID[model.StreamID](), "stream_kind": "sealed", "owner": "a.localhost", "servers": []string{"a.localhost"},
				"invited": []string{"b.localhost"}, "max_blob_bytes": 1 << 20, "by_account": profile, "account_ids": []string{string(bot.ID)},
				"members":      []any{profile, map[string]any{"id": bot.ID, "server": "b.localhost", "username": "robo", "bot": true, "version": 1}},
				"epoch_record": model.EncodeBytes(epoch(0, 1).Record)})
			if err != nil || kind != "decline" {
				t.Fatalf("a sealed chat offered for a bot: %s %v %v", kind, body, err)
			}

			// A device is added on sam's side: its list version reaches b,
			// it makes epoch 2, and everyone reads on.
			samWeb := sam.NewSession().Sealed("web")
			if err := samWeb.UnlockWithPhrase(phrase); err != nil {
				t.Fatal(err)
			}
			sw := samWeb.Chat(sa.ID, lee.ID)
			samWeb.Join(sw)
			testkit.Eventually(t, "both servers hold epoch record 2", func() bool {
				ea, eb := epoch(0, 2), epoch(1, 2)
				return ea.Seq != 0 && eb.Seq != 0 && bytes.Equal(ea.Record, eb.Record)
			})
			sw.Say("from the new device")
			shows(t, "lee", la, "the first secret", "the second secret", "from the new device")
			// A message lee's device sealed under epoch 2 and never sent,
			// for later.
			la.Sync()
			late, err := la.C.Seal(ref.MessageContent("too late", nil, nil, nil, nil))
			if err != nil {
				t.Fatal(err)
			}
			la.C.Unsend(late)
			la.Say("welcome")
			for who, c := range map[string]*testkit.SealedChat{"sam's phone": sa, "sam's browser": sw, "lee": la} {
				shows(t, who, c, "the first secret", "the second secret", "from the new device", "welcome")
			}

			// 57a: two devices make the next epoch. The record that
			// reaches a server first stands there and the other is
			// rejected, as is a message under an epoch that is not the
			// chat's current one. Here one reaches both servers before the
			// other is made known; LINK-PROTOCOL.md section 16 says what is
			// not yet settled when the two cross.
			sa.Sync()
			la.Sync()
			r1, k1, err1 := sa.C.MakeEpoch()
			r2, k2, err2 := la.C.MakeEpoch()
			if err1 != nil || err2 != nil || k1 != k2 {
				t.Fatalf("making two next epochs: %d %v, %d %v", k1, err1, k2, err2)
			}
			k := int64(k1)
			// The first is made on a, as a device of sam's would have it
			// made: stored there at once, and sent.
			if err := s.originateIn(0, sa.ID, "sealed.epoch_started", map[string]any{"key_epoch": k, "account_id": sam.ID, "device_id": samPhone.ID, "record": model.EncodeBytes(r1)}); err != nil {
				t.Fatal(err)
			}
			testkit.Eventually(t, "the first of the two stands on both servers", func() bool {
				ea, eb := epoch(0, k), epoch(1, k)
				return ea.Seq != 0 && eb.Seq != 0 && bytes.Equal(ea.Record, r1) && bytes.Equal(eb.Record, r1)
			})
			// The second is refused by b itself, which holds the first by
			// now, and goes nowhere; and by a, when b sends it all the
			// same.
			second := map[string]any{"key_epoch": k, "account_id": lee.ID, "device_id": leePhone.ID, "record": model.EncodeBytes(r2)}
			if err := s.originateIn(1, sa.ID, "sealed.epoch_started", second); err == nil {
				t.Fatal("b made a second record for a key epoch it already holds one for")
			}
			s.rejected(1, "sealed_epoch_lost", 1)
			s.submitTo(1, sa.ID, "sealed.epoch_started", second)
			s.rejected(0, "sealed_epoch_lost", 1)
			if !bytes.Equal(epoch(0, k).Record, r1) || !bytes.Equal(epoch(1, k).Record, r1) {
				t.Fatal("the later record for the key epoch replaced the earlier one")
			}
			s.submitTo(1, sa.ID, "message.created", map[string]any{"message": map[string]any{"id": model.NewID[string](), "author_id": lee.ID,
				"client_id": late.ClientID.String(), "quiet": late.Quiet, "created_at": model.Now(),
				"sealed": map[string]any{"key_epoch": late.KeyEpoch, "ciphertext": model.EncodeBytes(late.Ciphertext)}}, "blobs": []any{}})
			s.rejected(0, "sealed_message_stale", 1)
			// The devices take the record that stood, and go on.
			la.Say("after the race")
			shows(t, "sam", sa, "the first secret", "the second secret", "from the new device", "welcome", "after the race")

			// 57b: the two cross. Each device makes the next epoch while
			// its server cannot hear the other, so each server stores its
			// own. Nobody numbers events; the rule is that the record with
			// the earlier stamp stands, and replaces a later one already
			// held. Lee's is made first, on b, which is not the stream's
			// owner or its time reference: neither gives a record standing.
			sa.Sync()
			la.Sync()
			sw.Sync()
			r3, k3, err3 := sa.C.MakeEpoch()
			r4, k4, err4 := la.C.MakeEpoch()
			if err3 != nil || err4 != nil || k3 != k4 || int64(k3) != k+1 {
				t.Fatalf("making two next epochs: %d %v, %d %v", k3, err3, k4, err4)
			}
			k = int64(k3)
			rejoin := s.cut(1)
			if err := s.originateIn(1, sa.ID, "sealed.epoch_started", map[string]any{"key_epoch": k, "account_id": lee.ID, "device_id": leePhone.ID, "record": model.EncodeBytes(r4)}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(60 * time.Millisecond)
			if err := s.originateIn(0, sa.ID, "sealed.epoch_started", map[string]any{"key_epoch": k, "account_id": sam.ID, "device_id": samPhone.ID, "record": model.EncodeBytes(r3)}); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(epoch(0, k).Record, r3) || !bytes.Equal(epoch(1, k).Record, r4) {
				t.Fatal("each server should hold its own device's record while they cannot hear each other")
			}
			lost, replaced := s.link(1).StatsNow().Rejected["sealed_epoch_lost"], s.link(0).StatsNow().Replaced["sealed_epoch"]
			rejoin()
			testkit.Eventually(t, "the record with the earlier stamp stands on both servers", func() bool {
				return bytes.Equal(epoch(0, k).Record, r4) && bytes.Equal(epoch(1, k).Record, r4)
			})
			testkit.Eventually(t, "a replaced its record and b refused a's", func() bool {
				return s.link(0).StatsNow().Replaced["sealed_epoch"] == replaced+1 && s.link(1).StatsNow().Rejected["sealed_epoch_lost"] == lost+1
			})
			if s.link(1).StatsNow().Replaced["sealed_epoch"] != 0 {
				t.Fatal("b replaced the record that stands")
			}
			// The device whose record gave way takes the one that stands.
			la.Say("after the tie")
			shows(t, "sam", sa, "the first secret", "the second secret", "from the new device", "welcome", "after the race", "after the tie")

			// 56: no server's database and no hub's log holds the text,
			// and search finds nothing of it.
			secrets := []string{"the first secret", "the second secret", "from the new device", "after the race", "after the tie"}
			for i := 0; i < 2; i++ {
				msgs, _, err := s.Servers[i].Server.Store.Messages().List(ctx, conv, store.MessageList{SeqPage: store.SeqPage{Limit: 100}})
				if err != nil || len(msgs) < 5 {
					t.Fatalf("%s holds %d sealed messages: %v", testkit.ServerName(i), len(msgs), err)
				}
				for _, m := range msgs {
					if m.Text != "" || m.Sealed == nil {
						t.Fatalf("%s holds a sealed chat's message in plain: %+v", testkit.ServerName(i), m)
					}
					for _, secret := range secrets {
						if bytes.Contains(m.Sealed.Ciphertext, []byte(secret)) {
							t.Fatalf("%s holds %q readable", testkit.ServerName(i), secret)
						}
					}
				}
				who := []*testkit.Client{sam, lee}[i]
				_, _, raw := who.Get("/api/v1/search?q=secret", nil)
				if strings.Contains(string(raw), "secret") && !strings.Contains(string(raw), `"query"`) || strings.Contains(string(raw), "first secret") {
					t.Fatalf("search on %s found the sealed chat: %s", testkit.ServerName(i), raw)
				}
			}
			if hub {
				stored := readDir(t, s.Hub.Dir)
				for _, secret := range secrets {
					if bytes.Contains(stored, []byte(secret)) {
						t.Fatalf("the hub's log holds %q", secret)
					}
				}
				if bytes.Contains(stored, []byte(sa.ID)) || bytes.Contains(stored, []byte(sam.ID)) {
					t.Fatal("the hub's log names the sealed chat or an account in it")
				}
			}

			// 55: the last shared channel between them ends. The chat is
			// read-only on both servers, a server rejects what it can see
			// should not have been sent, and it is writable again when a
			// shared channel returns.
			sa.Sync()
			out, err := sa.C.Seal(ref.MessageContent("anyone?", nil, nil, nil, nil))
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Server.Link.Close(ctx, a.Owner.ID, s.stream); err != nil {
				t.Fatal(err)
			}
			testkit.Eventually(t, "the sealed chat is read-only on a", func() bool { return readOnly(sam, sa.ID) })
			testkit.Eventually(t, "the sealed chat is read-only on b", func() bool { return readOnly(lee, sa.ID) })
			sam.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+sa.ID+"/messages", sealedBody(out))
			s.submitTo(0, sa.ID, "message.created", map[string]any{"message": map[string]any{"id": model.NewID[string](), "author_id": sam.ID,
				"client_id": out.ClientID.String(), "quiet": out.Quiet, "created_at": model.Now(),
				"sealed": map[string]any{"key_epoch": out.KeyEpoch, "ciphertext": model.EncodeBytes(out.Ciphertext)}}, "blobs": []any{}})
			s.rejected(1, "no_shared_channel", 1)
			sa.C.Unsend(out)
			// A device list is no longer asked for or answered.
			if err := a.Server.Link.RequestDeviceLists(ctx, lee.ID, sam.ID, 0); model.CodeOf(err) != model.CodeForbidden {
				t.Fatalf("a device list request with no shared channel: %v", err)
			}
			back := a.Owner.Channel("back", false, sam)
			s.Share(0, back, "back", nil, 1)
			testkit.Eventually(t, "the sealed chat is writable again on a", func() bool { return !readOnly(sam, sa.ID) })
			testkit.Eventually(t, "the sealed chat is writable again on b", func() bool { return !readOnly(lee, sa.ID) })
			sa.Say("back again")
			la.Sync()
			if texts := la.Texts(); len(texts) == 0 {
				t.Fatal("lee's device lost the chat")
			}
			testkit.Eventually(t, "lee reads on", func() bool {
				la.Sync()
				texts := la.Texts()
				return texts[len(texts)-1] == "back again"
			})
		})
	}
}
