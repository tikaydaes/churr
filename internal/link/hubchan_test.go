package link_test

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/hub"
	"github.com/tikaydaes/churr/internal/link"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Channels a hub owns (rulings R56 and R59; docs/LINK-PROTOCOL.md section
// 23a). Every test runs two or three servers and a hub in one process.

// hubChannel is the hub's record of a channel as a client is shown it.
func hubChannel(c *testkit.Client, stream string) map[string]any {
	return c.Must(200, "GET", "/api/v1/streams/"+stream+"/channel", nil)["channel"].(map[string]any)
}

// moderate does one moderator's action and returns the record afterwards.
func moderate(c *testkit.Client, stream string, body map[string]any) map[string]any {
	return c.Must(200, "POST", "/api/v1/streams/"+stream+"/channel/actions", body)["channel"].(map[string]any)
}

func names(v any) string {
	var out []string
	for _, x := range arr(v) {
		out = append(out, fmt.Sprint(x))
	}
	return strings.Join(out, ",")
}

// directory is a hub's directory as server i's owner is shown it, by name.
func directory(l *testkit.Linked, i int) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, raw := range arr(l.Servers[i].Owner.Must(200, "GET", "/api/v1/admin/peers/"+l.Peers[i]+"/directory", nil)["channels"]) {
		e := raw.(map[string]any)
		out[e["name"].(string)] = e
	}
	return out
}

// joinListed has server i join, or ask to join, a channel from the
// directory, and returns its new local conversation.
func joinListed(l *testkit.Linked, i int, stream, name string) string {
	out := l.Servers[i].Owner.Must(200, "POST", "/api/v1/admin/peers/"+l.Peers[i]+"/directory/join", map[string]any{"stream_id": stream, "name": name})
	return out["conversation"].(map[string]any)["id"].(string)
}

func streamState(l *testkit.Linked, i int, stream string) string {
	if st := l.StreamOn(i, stream); st != nil {
		return st["state"].(string)
	}
	return ""
}

// A server that is not on a channel's list receives nothing of it: no
// envelope, no record, no log, and it is not let in by asking.
func TestHubChannelReachesOnlyItsServers(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	ch := a.Owner.Channel("launch", false)
	stream, local := l.Share(0, ch, "launch", nil, 1)
	sid := model.StreamID(stream)
	for i := 0; i < 5; i++ {
		a.Owner.Post(ch, fmt.Sprintf("only for a and b %d", i))
		b.Owner.Post(local[1], fmt.Sprintf("b answers %d", i))
	}
	b.Owner.HasMessage(local[1], "only for a and b 4")
	a.Owner.HasMessage(ch, "b answers 4")
	a.Owner.Must(204, "POST", "/api/v1/conversations/"+ch+"/typing", map[string]any{})

	// The hub did carry it: this is not a test of a hub that carries nothing.
	if log := l.Hub.Hub.TestLog(stream); len(log) < 10 {
		t.Fatalf("the hub's log holds %d envelopes", len(log))
	}
	stats := c.Server.Link.StatsNow()
	if stats.Delivered[sid] != 0 || stats.Ephemeral[sid] != 0 {
		t.Fatalf("c was delivered %d envelopes and %d ephemeral frames of a channel it is not in", stats.Delivered[sid], stats.Ephemeral[sid])
	}
	if _, err := c.Server.Store.Streams().Get(ctx, sid); err == nil {
		t.Fatal("c holds a stream it was never invited to")
	}
	if _, _, _, held := c.Server.Link.TestChannel(sid); held {
		t.Fatal("c was sent the record of a channel it is not in")
	}
	if dir := directory(l, 2); len(dir) != 0 {
		t.Fatalf("an unlisted channel is in the directory c is shown: %v", dir)
	}
	// Asking does not help: not for the log, and not to be let in.
	peer := model.PeerID(l.Peers[2])
	answer, err := c.Server.Link.TestHubFrame(peer, wire.Frame{Frame: wire.FCatchUp, StreamID: stream, Have: map[string]int64{}}, 2*time.Second)
	if err != nil || answer.Code != wire.ENotInStream {
		t.Fatalf("c asked for the log: %+v %v", answer, err)
	}
	join := c.Server.Link.TestSignBlock(wire.FJoin, wire.ChannelAction{StreamID: stream, ActionID: model.NewID[string](), Server: "c.localhost", At: model.Now()})
	if answer, err = c.Server.Link.TestHubFrame(peer, join, 2*time.Second); err != nil || answer.Code != wire.EForbidden {
		t.Fatalf("c asked to join an unlisted channel: %+v %v", answer, err)
	}
	time.Sleep(200 * time.Millisecond)
	if n := c.Server.Link.StatsNow().Delivered[sid]; n != 0 {
		t.Fatalf("asking brought c %d envelopes", n)
	}
	if rec := hubChannel(a.Owner, stream); names(rec["servers"]) != "a.localhost,b.localhost" || names(rec["requests"]) != "" {
		t.Fatalf("the record changed: %v", rec)
	}
}

// What is not taken as a moderator's action, by the hub or by any server:
// one a server signs in the name of another server's moderator, one in the
// name of an account that is not on the list, and a record that an accepted
// hub key did not sign.
func TestManagerActionsAndRecordsAreChecked(t *testing.T) {
	s := share(t, 3, true, nil)
	a, b, c := s.Servers[0], s.Servers[1], s.Servers[2]
	stream := string(s.stream)
	before := hubChannel(a.Owner, stream)
	mod := wire.ChannelAccount{AccountID: string(a.Owner.ID), Server: "a.localhost", Username: "owner"}
	peerB := model.PeerID(s.Peers[1])
	unchanged := func(what string) {
		t.Helper()
		time.Sleep(100 * time.Millisecond)
		for i, ts := range s.Servers {
			if now := hubChannel(ts.Owner, stream); now["version"] != before["version"] || now["name"] != "launch" || names(now["servers"]) != names(before["servers"]) {
				t.Fatalf("%s: the record on %s changed: %v", what, testkit.ServerName(i), now)
			}
		}
	}

	// Forged at the hub: b signs an action in the name of a's moderator.
	name := "taken over"
	forged := s.link(1).TestSignBlock(wire.FChannelAction, wire.ChannelAction{StreamID: stream, ActionID: model.NewID[string](), Action: wire.ActDescribe,
		By: mod, Name: &name, At: model.Now()})
	if answer, err := s.link(1).TestHubFrame(peerB, forged, 2*time.Second); err != nil || answer.Code != wire.EForbidden {
		t.Fatalf("an action b signed for a's moderator: %+v %v", answer, err)
	}
	// The same account id, claimed as an account of b.
	claimed := mod
	claimed.Server = "b.localhost"
	forged = s.link(1).TestSignBlock(wire.FChannelAction, wire.ChannelAction{StreamID: stream, ActionID: model.NewID[string](), Action: wire.ActClose,
		By: claimed, At: model.Now()})
	if answer, err := s.link(1).TestHubFrame(peerB, forged, 2*time.Second); err != nil || answer.Code != wire.EForbidden {
		t.Fatalf("a's moderator claimed as b's account: %+v %v", answer, err)
	}
	// A frame whose signature is not the sending server's at all.
	bad := s.link(1).TestSignBlock(wire.FChannelAction, wire.ChannelAction{StreamID: stream, ActionID: model.NewID[string](), Action: wire.ActClose,
		By: wire.ChannelAccount{AccountID: string(b.Owner.ID), Server: "b.localhost"}, At: model.Now()})
	bad.Sig[3] ^= 1
	if answer, err := s.link(1).TestHubFrame(peerB, bad, 2*time.Second); err != nil || answer.Code != wire.EBadSignature {
		t.Fatalf("an action with a bad signature: %+v %v", answer, err)
	}
	unchanged("forged actions")

	// An account that is not on the list: b's owner, and a member of a.
	// Their own server refuses, and signs nothing for them: the hub is not
	// even asked.
	var asked atomic.Int64
	s.Hub.Hub.TestTap(func(server string, raw []byte) {
		if bytes.Contains(raw, []byte(`"frame":"channel_action"`)) {
			asked.Add(1)
		}
	})
	for _, who := range []*testkit.Client{b.Owner, c.Owner, s.ann} {
		who.Refused(403, model.CodeForbidden, "POST", "/api/v1/streams/"+stream+"/channel/actions", map[string]any{"action": "describe", "name": "mine now"})
		who.Refused(403, model.CodeForbidden, "POST", "/api/v1/streams/"+stream+"/channel/actions", map[string]any{"action": "remove_server", "server": "c.localhost"})
	}
	time.Sleep(50 * time.Millisecond)
	if n := asked.Load(); n != 0 {
		t.Fatalf("servers signed %d actions for accounts that are not moderators", n)
	}
	s.Hub.Hub.TestTap(nil)
	// And straight to the hub, signed honestly by b for b's own owner.
	honest := s.link(1).TestSignBlock(wire.FChannelAction, wire.ChannelAction{StreamID: stream, ActionID: model.NewID[string](), Action: wire.ActClose,
		By: wire.ChannelAccount{AccountID: string(b.Owner.ID), Server: "b.localhost", Username: "owner"}, At: model.Now()})
	if answer, err := s.link(1).TestHubFrame(peerB, honest, 2*time.Second); err != nil || answer.Code != wire.EForbidden {
		t.Fatalf("an action by an account that is not a moderator: %+v %v", answer, err)
	}
	unchanged("actions by accounts not on the list")

	// In the stream, where the hub cannot look: a removal for everyone that
	// b signs for a's moderator, and one for its own owner. Every other
	// server checks the hub-signed list itself.
	target := s.ann.Post(s.conv[0], "this one stays")
	b.Owner.HasMessage(s.conv[1], "this one stays")
	c.Owner.HasMessage(s.conv[2], "this one stays")
	s.submit(1, "message.removed", map[string]any{"message_id": target["id"], "removed_by_id": a.Owner.ID, "removed_at": model.Now()})
	s.submit(1, "message.removed", map[string]any{"message_id": target["id"], "removed_by_id": b.Owner.ID, "removed_at": model.Now()})
	for _, i := range []int{0, 2} {
		s.rejected(i, "not_moderator", 2)
	}
	s.settle("after the forged removals")
	for i, ts := range s.Servers {
		if m := ts.Owner.HasMessage(s.conv[i], "this one stays"); m["deleted_at"] != nil {
			t.Fatalf("%s took a forged removal: %v", testkit.ServerName(i), m)
		}
	}

	// A record the hub did not sign. First the hub's own record with one
	// thing changed: the signature no longer covers it.
	rec, signed, sig, ok := s.link(1).TestChannel(s.stream)
	if !ok {
		t.Fatal("b holds no record")
	}
	evil := rec
	evil.Version, evil.Managers = rec.Version+1, append([]wire.ChannelAccount{{AccountID: string(b.Owner.ID), Server: "b.localhost", Username: "owner"}}, rec.Managers...)
	tampered, _ := wire.Marshal("ChannelRecord", evil)
	err := s.link(1).TestInjectChannel(peerB, wire.Frame{Frame: wire.FChannel, Signed: tampered, Sig: sig})
	if wire.AsError(err).Code != wire.EBadSignature {
		t.Fatalf("a record changed after signing: %v", err)
	}
	// Then one signed properly, by a key that is not the hub's: c's own,
	// named in the record as the hub's key.
	evil.HubKey = wire.Bytes(c.Server.Link.Identity().Keys.SignPublic())
	selfSigned := s.link(2).TestSignBlock(wire.FChannel, evil)
	err = s.link(1).TestInjectChannel(peerB, selfSigned)
	if wire.AsError(err).Code != wire.EBadSignature {
		t.Fatalf("a record signed by a key that is not an accepted hub's: %v", err)
	}
	// The real one, as sent, still verifies: the refusals above are of the
	// forgeries and not of records in general.
	if err := s.link(1).TestInjectChannel(peerB, wire.Frame{Frame: wire.FChannel, Signed: signed, Sig: sig}); err != nil {
		t.Fatalf("the hub's own record: %v", err)
	}
	if now, _, _, _ := s.link(1).TestChannel(s.stream); now.Version != rec.Version || len(now.Managers) != len(rec.Managers) {
		t.Fatalf("b's record changed: %+v", now)
	}
	if b.Owner.Must(200, "GET", "/api/v1/streams/"+stream+"/channel", nil)["manager"] != false {
		t.Fatal("b's owner became a moderator")
	}
	b.Owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/streams/"+stream+"/channel/actions", map[string]any{"action": "close"})
	if s.link(1).StatsNow().Dropped["channel_bad_signature"] != 2 {
		t.Fatalf("b counted %d records with a bad signature", s.link(1).StatsNow().Dropped["channel_bad_signature"])
	}
}

// The three listing modes: the directory, joining an open channel, asking
// to join a listed one (approved and declined), and an unlisted one, which
// is joined by invitation and no other way.
func TestListingModesAndJoining(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	shareAs := func(name, listing string) (string, string) {
		ch := a.Owner.Channel(name, false)
		a.Owner.Must(200, "PATCH", "/api/v1/conversations/"+ch, map[string]any{"topic": "about " + name})
		stream, _ := l.Share(0, ch, name, map[string]any{"listing": listing, "history_days": 30})
		return stream, ch
	}
	open, openCh := shareAs("town-square", "open")
	ask, askCh := shareAs("committee", "request")
	quiet, quietCh := shareAs("back-room", "unlisted")
	a.Owner.Post(openCh, "said before anyone joined")

	// The directory, as the hub signs it for b and for c: the listed
	// channels with name, description, mode, moderators and a count.
	for _, i := range []int{1, 2} {
		dir := directory(l, i)
		if len(dir) != 2 || dir["back-room"] != nil {
			t.Fatalf("%s's directory: %v", testkit.ServerName(i), dir)
		}
		e := dir["town-square"]
		mods := arr(e["managers"])
		if e["stream_id"] != open || e["listing"] != "open" || e["description"] != "about town-square" || e["servers"] != float64(1) ||
			e["state"] != "none" || len(mods) != 1 || mods[0].(map[string]any)["server"] != "a.localhost" ||
			mods[0].(map[string]any)["account_id"] != string(a.Owner.ID) {
			t.Fatalf("%s's entry for the open channel: %v", testkit.ServerName(i), e)
		}
		if dir["committee"]["listing"] != "request" {
			t.Fatalf("%s's entry for the listed channel: %v", testkit.ServerName(i), dir["committee"])
		}
	}

	// Open: b joins without anyone's leave. The longest-standing member
	// server makes the key, and b gets the history the channel gives.
	openB := joinListed(l, 1, open, "square")
	l.Joined(1, open)
	if rec := hubChannel(a.Owner, open); names(rec["servers"]) != "a.localhost,b.localhost" || rec["key_maker"] != "a.localhost" {
		t.Fatalf("after an open join: %v", rec)
	}
	b.Owner.HasMessage(openB, "said before anyone joined")
	b.Owner.Post(openB, "b walked in")
	a.Owner.HasMessage(openCh, "b walked in")
	if directory(l, 1)["town-square"]["state"] != "member" || directory(l, 2)["town-square"]["servers"] != float64(2) {
		t.Fatal("the directory does not show the join")
	}

	// Request: b and c ask. Nothing is theirs until a moderator says so.
	askB := joinListed(l, 1, ask, "committee-b")
	askC := joinListed(l, 2, ask, "committee-c")
	testkit.Eventually(t, "the moderator sees both requests", func() bool {
		return names(hubChannel(a.Owner, ask)["requests"]) == "b.localhost,c.localhost"
	})
	a.Owner.Post(askCh, "not for those still asking")
	time.Sleep(150 * time.Millisecond)
	for _, i := range []int{1, 2} {
		if streamState(l, i, ask) != "offered" || l.Servers[i].Server.Link.StatsNow().Delivered[model.StreamID(ask)] != 0 {
			t.Fatalf("%s is in before it was approved", testkit.ServerName(i))
		}
	}
	if directory(l, 1)["committee"]["state"] != "requested" {
		t.Fatal("b's request does not show in its directory")
	}
	b.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/peers/"+l.Peers[1]+"/directory/join", map[string]any{"stream_id": ask, "name": "again"})
	// Approved: the approving moderator's home server makes the key.
	rec := moderate(a.Owner, ask, map[string]any{"action": "approve", "server": "b.localhost"})
	if names(rec["servers"]) != "a.localhost,b.localhost" || names(rec["requests"]) != "c.localhost" || rec["key_maker"] != "a.localhost" {
		t.Fatalf("after approving b: %v", rec)
	}
	l.Joined(1, ask)
	a.Owner.Post(askCh, "for the committee")
	b.Owner.HasMessage(askB, "for the committee")
	// Declined: c's stream ends, and its local channel is left archived.
	moderate(a.Owner, ask, map[string]any{"action": "decline", "server": "c.localhost"})
	testkit.Eventually(t, "c learns it was declined", func() bool { return streamState(l, 2, ask) == "removed" })
	if conv := c.Owner.Must(200, "GET", "/api/v1/conversations/"+askC, nil)["conversation"].(map[string]any); conv["archived_at"] == nil || conv["shared"] != nil {
		t.Fatalf("c's local channel after the decline: %v", conv)
	}
	absent(t, c.Owner, askC, "for the committee")
	a.Owner.Refused(404, model.CodeNotFound, "POST", "/api/v1/streams/"+ask+"/channel/actions", map[string]any{"action": "approve", "server": "c.localhost"})

	// Unlisted: not in the directory, not joined by asking, joined when a
	// moderator invites.
	c.Owner.Refused(404, model.CodeNotFound, "POST", "/api/v1/admin/peers/"+l.Peers[2]+"/directory/join", map[string]any{"stream_id": quiet, "name": "sneak"})
	if _, err := c.Server.Store.Streams().Get(ctx, model.StreamID(quiet)); err == nil {
		t.Fatal("c holds something of the unlisted channel")
	}
	rec = moderate(a.Owner, quiet, map[string]any{"action": "invite", "server": "c.localhost"})
	if names(rec["invited"]) != "c.localhost" || names(rec["servers"]) != "a.localhost" {
		t.Fatalf("after inviting c: %v", rec)
	}
	quietC := l.Accept(2, quiet, "back-room-c")
	l.Joined(2, quiet)
	a.Owner.Post(quietCh, "between a and c")
	c.Owner.HasMessage(quietC, "between a and c")
	if streamState(l, 1, quiet) != "" {
		t.Fatal("b holds something of the unlisted channel")
	}
	// An invitation that is declined is gone from the record.
	moderate(a.Owner, quiet, map[string]any{"action": "invite", "server": "b.localhost"})
	testkit.Eventually(t, "the invitation reaches b", func() bool { return streamState(l, 1, quiet) == "offered" })
	b.Owner.Must(204, "POST", "/api/v1/admin/streams/"+quiet+"/decline", nil)
	testkit.Eventually(t, "the hub drops the invitation", func() bool { return names(hubChannel(a.Owner, quiet)["invited"]) == "" })

	// Changing the mode changes what the directory shows.
	moderate(a.Owner, quiet, map[string]any{"action": "set_listing", "listing": "request"})
	moderate(a.Owner, open, map[string]any{"action": "set_listing", "listing": "unlisted"})
	if dir := directory(l, 2); dir["back-room"] == nil || dir["town-square"] != nil {
		t.Fatalf("the directory after the modes changed: %v", dir)
	}
	a.Owner.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/streams/"+quiet+"/channel/actions", map[string]any{"action": "set_listing", "listing": "secret"})
}

// What a moderator can do, and that every server takes each change from the
// hub's signature: name and description, appointing and dismissing
// moderators, removing a server, closing the channel. The operator's last
// resort does the same from the hub's own machine.
func TestManagersChangeTheRecord(t *testing.T) {
	s := share(t, 3, true, nil)
	a, b, c := s.Servers[0], s.Servers[1], s.Servers[2]
	stream := string(s.stream)
	everywhere := func(what string, ok func(rec map[string]any) bool) {
		t.Helper()
		for i, ts := range s.Servers[:3] {
			if streamState(s.Linked, i, stream) != "active" {
				continue
			}
			testkit.Eventually(t, what+" on "+testkit.ServerName(i), func() bool { return ok(hubChannel(ts.Owner, stream)) })
		}
	}
	moderate(a.Owner, stream, map[string]any{"action": "describe", "name": "launch control", "description": "Getting 2.0 out"})
	everywhere("the new name", func(rec map[string]any) bool {
		return rec["name"] == "launch control" && rec["description"] == "Getting 2.0 out"
	})
	// Each server's own name for its copy stays its own.
	if conv := b.Owner.Must(200, "GET", "/api/v1/conversations/"+s.conv[1], nil)["conversation"].(map[string]any); conv["name"] != "launch" {
		t.Fatalf("b's local name changed: %v", conv["name"])
	}
	a.Owner.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/streams/"+stream+"/channel/actions", map[string]any{"action": "describe"})

	// b's owner is made a moderator and can then act, from b.
	b.Owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/streams/"+stream+"/channel/actions", map[string]any{"action": "describe", "description": "b's"})
	moderate(a.Owner, stream, map[string]any{"action": "add_manager", "account_id": b.Owner.ID})
	everywhere("two moderators", func(rec map[string]any) bool { return len(arr(rec["managers"])) == 2 })
	if b.Owner.Must(200, "GET", "/api/v1/streams/"+stream+"/channel", nil)["manager"] != true {
		t.Fatal("b's owner is not shown as a moderator")
	}
	moderate(b.Owner, stream, map[string]any{"action": "describe", "description": "b's words"})
	everywhere("b's description", func(rec map[string]any) bool { return rec["description"] == "b's words" })
	// An account of a server that is not in the channel cannot be one.
	a.Owner.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/streams/"+stream+"/channel/actions",
		map[string]any{"action": "add_manager", "account_id": model.NewID[string]()})

	// The operator appoints and dismisses, from the hub's machine.
	if err := hubOf(s).Appoint(stream, string(c.Owner.ID), "owner@c.localhost"); err != nil {
		t.Fatal(err)
	}
	everywhere("three moderators", func(rec map[string]any) bool { return len(arr(rec["managers"])) == 3 })
	moderate(c.Owner, stream, map[string]any{"action": "describe", "description": "c's words"})
	if err := hubOf(s).Dismiss(stream, string(c.Owner.ID)); err != nil {
		t.Fatal(err)
	}
	everywhere("two moderators again", func(rec map[string]any) bool { return len(arr(rec["managers"])) == 2 })
	c.Owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/streams/"+stream+"/channel/actions", map[string]any{"action": "describe", "description": "c again"})
	// b's moderator dismisses a's, and a's owner has no say any more:
	// owning the server that shared the channel gives nothing.
	moderate(b.Owner, stream, map[string]any{"action": "remove_manager", "account_id": a.Owner.ID})
	everywhere("one moderator", func(rec map[string]any) bool { return len(arr(rec["managers"])) == 1 })
	a.Owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/streams/"+stream+"/channel/actions", map[string]any{"action": "close"})
	a.Owner.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/streams/"+stream+"/servers/"+s.ServerID(0, 2), nil)
	b.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/streams/"+stream+"/channel/actions", map[string]any{"action": "remove_manager", "account_id": b.Owner.ID})

	// A moderator has a server removed. It gets nothing further, a new key
	// epoch starts (made by the moderator's home server), and its people
	// have left for the others.
	epoch := s.StreamOn(1, stream)["key_epoch"].(float64)
	rec := moderate(b.Owner, stream, map[string]any{"action": "remove_server", "server": "c.localhost"})
	if names(rec["servers"]) != "a.localhost,b.localhost" || rec["key_maker"] != "b.localhost" {
		t.Fatalf("after removing c: %v", rec)
	}
	testkit.Eventually(t, "c is out", func() bool { return streamState(s.Linked, 2, stream) == "removed" })
	testkit.Eventually(t, "a new epoch starts on a and b", func() bool {
		return s.StreamOn(0, stream)["key_epoch"].(float64) > epoch && s.StreamOn(1, stream)["key_epoch"].(float64) > epoch
	})
	testkit.Eventually(t, "c's owner has left on a", func() bool { return !s.isMember(0, c.Owner.ID) })
	s.ann.Post(s.conv[0], "after c was removed")
	b.Owner.HasMessage(s.conv[1], "after c was removed")
	absent(t, c.Owner, s.conv[2], "after c was removed")
	if keys := c.Server.Link.TestKeys(s.stream); keys[int64(s.StreamOn(0, stream)["key_epoch"].(float64))] != nil {
		t.Fatal("the removed server was given the new key")
	}

	// The operator expels, and a moderator closes.
	if err := hubOf(s).Expel(stream, "a.localhost"); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "a is out", func() bool { return streamState(s.Linked, 0, stream) == "removed" })
	moderate(b.Owner, stream, map[string]any{"action": "close"})
	testkit.Eventually(t, "the channel is closed for b", func() bool { return streamState(s.Linked, 1, stream) == "removed" })
	if list, _ := hubOf(s).Channels(); len(list) != 0 {
		t.Fatalf("the hub still lists the channel: %+v", list)
	}
}

// hubOf is the hub of a shared fixture, for the operator's commands.
func hubOf(s *shared) *hub.Hub { return s.Hub.Hub }

// A manager deletes any message in the channel for everyone, from their
// own server; an owner on neither list deletes none.
func TestManagerDeletesForEveryone(t *testing.T) {
	s := share(t, 3, true, nil)
	a, b, c := s.Servers[0], s.Servers[1], s.Servers[2]
	stream := string(s.stream)
	// mo is an ordinary member of b: no role, and level `member` in b's
	// copy of the channel. She will be the moderator.
	mo := b.Member("mo")
	mo.Must(200, "POST", "/api/v1/conversations/"+s.conv[1]+"/join", nil)
	testkit.Eventually(t, "a knows mo", func() bool { return s.isMember(0, mo.ID) })
	fromA := s.ann.Post(s.conv[0], "ann's, to be removed")
	fromC := c.Owner.Post(s.conv[2], "c's, to be removed")
	kept := s.ann.Post(s.conv[0], "ann's, to be hidden on b only")
	s.settle("all there")

	// Before she is a moderator she cannot: she holds no manager level.
	member, _, _ := b.Server.Store.Members().Get(ctx, model.ConversationID(s.conv[1]), mo.ID)
	if member.Level != model.LevelMember {
		t.Fatalf("mo's level on b is %q", member.Level)
	}
	mo.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/messages/"+fromA["id"].(string), nil)
	moderate(a.Owner, stream, map[string]any{"action": "add_manager", "account_id": mo.ID})
	testkit.Eventually(t, "b holds the record that names mo", func() bool {
		return mo.Must(200, "GET", "/api/v1/streams/"+stream+"/channel", nil)["manager"] == true
	})
	// Now she can, and her level in b's copy follows the hub's list
	// (section 14.1).
	mo.Must(200, "DELETE", "/api/v1/messages/"+fromA["id"].(string), nil)
	mo.Must(200, "DELETE", "/api/v1/messages/"+fromC["id"].(string), nil)
	testkit.Eventually(t, "mo manages b's copy", func() bool {
		member, _, _ = b.Server.Store.Members().Get(ctx, model.ConversationID(s.conv[1]), mo.ID)
		return member.Level == model.LevelManager
	})
	for i, ts := range s.Servers {
		for _, id := range []any{fromA["id"], fromC["id"]} {
			testkit.Eventually(t, "the removal reaches "+testkit.ServerName(i), func() bool {
				m, err := ts.Server.Store.Messages().Get(ctx, model.MessageID(id.(string)))
				return err == nil && m.Deleted() && m.DeletedByID == mo.ID
			})
		}
	}
	// b's owner is on neither list, and so no manager of b's copy: an
	// owner is no manager by being one (ruling R99), and nobody is made
	// one there but by the hub's list.
	lee := b.Owner
	if lee.Must(200, "GET", "/api/v1/streams/"+stream+"/channel", nil)["manager"] != false {
		t.Fatal("b's owner is a manager")
	}
	lee.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/messages/"+kept["id"].(string), nil)
	// Dismissed, she cannot any more.
	another := s.ann.Post(s.conv[0], "after mo was dismissed")
	b.Owner.HasMessage(s.conv[1], "after mo was dismissed")
	moderate(a.Owner, stream, map[string]any{"action": "remove_manager", "account_id": mo.ID})
	testkit.Eventually(t, "b holds the record without mo", func() bool {
		return mo.Must(200, "GET", "/api/v1/streams/"+stream+"/channel", nil)["manager"] == false
	})
	mo.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/messages/"+another["id"].(string), nil)
	_ = a
}

// The hub never holds a stream key and never sees content: nothing it
// stores and nothing it is sent contains a key or a message, in any of the
// encodings the protocol uses.
func TestKeyAndContentNeverReachTheHub(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	var mu sync.Mutex
	var received []byte
	frames := map[string]int{}
	l.Hub.Hub.TestTap(func(server string, raw []byte) {
		f, _ := wire.DecodeFrame(raw)
		mu.Lock()
		received = append(append(received, raw...), '\n')
		frames[f.Frame]++
		mu.Unlock()
	})
	keys := map[string][]byte{}
	for i, ts := range l.Servers {
		i := i
		ts.Server.Link.TestKeyMade(func(stream model.StreamID, epoch int64, key []byte) {
			mu.Lock()
			keys[fmt.Sprintf("%s epoch %d", testkit.ServerName(i), epoch)] = append([]byte(nil), key...)
			mu.Unlock()
		})
	}
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	ch := a.Owner.Channel("vault", false)
	stream, local := l.Share(0, ch, "vault", map[string]any{"listing": "open", "history_days": 30}, 1)
	sid := model.StreamID(stream)
	a.Owner.Post(ch, "PLAINTEXT-ALPHA from a")
	b.Owner.Post(local[1], "PLAINTEXT-BRAVO from b")
	a.Owner.HasMessage(ch, "PLAINTEXT-BRAVO from b")
	// c joins (a third key), posts, and is removed (a fourth).
	convC := joinListed(l, 2, stream, "vault-c")
	l.Joined(2, stream)
	c.Owner.HasMessage(convC, "PLAINTEXT-ALPHA from a") // history, which travels sealed
	c.Owner.Post(convC, "PLAINTEXT-CHARLIE from c")
	b.Owner.HasMessage(local[1], "PLAINTEXT-CHARLIE from c")
	before := a.Server.Link.TestStream
	st, _ := before(sid)
	moderate(a.Owner, stream, map[string]any{"action": "remove_server", "server": "c.localhost"})
	testkit.Eventually(t, "a new epoch after the removal", func() bool { cur, _ := before(sid); return cur.KeyEpoch > st.KeyEpoch })
	a.Owner.Post(ch, "PLAINTEXT-DELTA after the removal")
	b.Owner.HasMessage(local[1], "PLAINTEXT-DELTA after the removal")

	// Every key any server holds for the stream, and every key a maker
	// made: epoch 1 was made at sharing, the rest as key-maker.
	for i, ts := range l.Servers {
		for epoch, key := range ts.Server.Link.TestKeys(sid) {
			keys[fmt.Sprintf("%s holds epoch %d", testkit.ServerName(i), epoch)] = key
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keys) < 6 {
		t.Fatalf("only %d keys were gathered: the test would prove little", len(keys))
	}
	// The hub was in the middle of all of it.
	if frames[wire.FDirect] < 4 || frames[wire.FSubmit] < 6 || frames[wire.FChannelCreate] != 1 || frames[wire.FJoin] < 2 || frames[wire.FChannelAction] < 2 {
		t.Fatalf("the hub received %v", frames)
	}
	stored := readDir(t, l.Hub.Dir)
	if len(stored) < 1000 || len(received) < 1000 {
		t.Fatalf("the hub stored %d bytes and received %d", len(stored), len(received))
	}
	everything := map[string][]byte{"storage": stored, "frames received": received}
	for where, hay := range everything {
		for what, key := range keys {
			if len(key) != 32 {
				t.Fatalf("%s is %d bytes", what, len(key))
			}
			for enc, needle := range map[string][]byte{
				"raw":       key,
				"base64url": []byte(base64.RawURLEncoding.EncodeToString(key)),
				"base64":    []byte(base64.StdEncoding.EncodeToString(key)),
				"hex":       []byte(hex.EncodeToString(key)),
			} {
				if bytes.Contains(hay, needle) {
					t.Errorf("the hub's %s contains the key %s (%s)", where, what, enc)
				}
			}
		}
		for _, text := range []string{"PLAINTEXT-ALPHA", "PLAINTEXT-BRAVO", "PLAINTEXT-CHARLIE", "PLAINTEXT-DELTA", "message.created", "stream.servers_changed", "author_id"} {
			if bytes.Contains(hay, []byte(text)) {
				t.Errorf("the hub's %s contains %q", where, text)
			}
		}
	}
	// What it does hold is what it is said to hold: the channel's record.
	if !bytes.Contains(stored, []byte("vault")) || !bytes.Contains(stored, []byte(stream)) {
		t.Error("the hub's storage does not hold the channel's name and identifier")
	}
}

// The server the hub names makes the key; if it does not in time, the hub
// names the next in line, and the channel goes on with that server's key.
func TestKeyMakerFallback(t *testing.T) {
	defer hub.SetMakerWait(400 * time.Millisecond)()
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	ch := a.Owner.Channel("relay", false)
	stream, local := l.Share(0, ch, "relay", map[string]any{"listing": "open", "history_days": 30}, 1)
	sid := model.StreamID(stream)
	a.Owner.Post(ch, "before c")
	b.Owner.HasMessage(local[1], "before c")
	made := map[string]int64{}
	var mu sync.Mutex
	for i, ts := range l.Servers {
		i := i
		ts.Server.Link.TestKeyMade(func(_ model.StreamID, epoch int64, _ []byte) {
			mu.Lock()
			made[testkit.ServerName(i)] = epoch
			mu.Unlock()
		})
	}
	// a stops making keys. c joins the open channel: the hub names a, the
	// longest-standing member, and a does nothing.
	a.Server.Link.TestNoKeys(true)
	stA, _ := a.Server.Link.TestStream(sid)
	convC := joinListed(l, 2, stream, "relay-c")
	testkit.Eventually(t, "the hub names a first", func() bool {
		rec := hubChannel(a.Owner, stream)
		return names(rec["servers"]) == "a.localhost,b.localhost,c.localhost"
	})
	defer func() {
		if t.Failed() {
			for i, ts := range l.Servers {
				cur, _ := ts.Server.Link.TestStream(sid)
				rec, _, _, _ := ts.Server.Link.TestChannel(sid)
				t.Logf("%s: state %s epoch %d keys %d record v%d epoch %d maker %s makers %v rejected %v dropped %v outbox %d last %+v", testkit.ServerName(i), cur.State, cur.KeyEpoch,
					len(ts.Server.Link.TestKeys(sid)), rec.Version, rec.Epoch, rec.KeyMaker, rec.Makers, ts.Server.Link.StatsNow().Rejected, ts.Server.Link.StatsNow().Dropped,
					ts.Server.Link.TestOutbox(sid), ts.Server.Link.TestLastError().Code)
			}
		}
	}()
	// Then b, with a new epoch number; b makes the key for all three.
	testkit.Eventually(t, "the hub names b", func() bool { return hubChannel(a.Owner, stream)["key_maker"] == "b.localhost" })
	l.Joined(2, stream)
	testkit.Eventually(t, "every server is on b's epoch", func() bool {
		mu.Lock()
		epoch := made["b.localhost"]
		mu.Unlock()
		for _, ts := range l.Servers {
			cur, _ := ts.Server.Link.TestStream(sid)
			if epoch == 0 || cur.KeyEpoch != epoch {
				return false
			}
		}
		return true
	})
	mu.Lock()
	if made["a.localhost"] != 0 || made["b.localhost"] != stA.KeyEpoch+2 {
		t.Fatalf("keys were made by %v; a stood at epoch %d", made, stA.KeyEpoch)
	}
	epoch := made["b.localhost"]
	mu.Unlock()
	// One key, b's, on all three; and the hub never had it to hand on.
	keyB := b.Server.Link.TestKeys(sid)[epoch]
	for i, ts := range l.Servers {
		if !bytes.Equal(ts.Server.Link.TestKeys(sid)[epoch], keyB) || len(keyB) != 32 {
			t.Fatalf("%s does not hold b's key for epoch %d", testkit.ServerName(i), epoch)
		}
		if _, skipped := ts.Server.Link.TestKeys(sid)[epoch-1]; skipped {
			t.Fatalf("%s holds a key for the epoch nobody made", testkit.ServerName(i))
		}
	}
	// The channel works, the newcomer has its history from b, and a's
	// people are known to c.
	c.Owner.HasMessage(convC, "before c")
	c.Owner.Post(convC, "c is in")
	a.Owner.HasMessage(ch, "c is in")
	b.Owner.HasMessage(local[1], "c is in")
	a.Owner.Post(ch, "a still talks")
	c.Owner.HasMessage(convC, "a still talks")
	// a comes back to its duties: a key it makes now, for the epoch it was
	// named for and let pass, is taken by nobody.
	a.Server.Link.TestNoKeys(false)
	if _, err := a.Server.Link.TestDirect(model.PeerID(l.Peers[0]), "c.localhost", "key", map[string]any{"stream_id": stream, "epoch": epoch + 1,
		"key": wire.Bytes(bytes.Repeat([]byte{9}, 32))}, model.Now(), nil); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "c drops a key from a server the hub did not name", func() bool {
		return c.Server.Link.StatsNow().Dropped["key_not_from_owner"] >= 1
	})
	if _, held := c.Server.Link.TestKeys(sid)[epoch+1]; held {
		t.Fatal("c took a key for an epoch from a server the hub had not named for it")
	}
}

// The hub's status page gives counts and no names (ruling R59); names are
// the operator's, by command, and member servers', through the directory.
// Ad-hoc channels across a hub are not the hub's: no record, no listing.
func TestHubStatusCountsAndWhatIsNotAChannel(t *testing.T) {
	s := share(t, 2, true, map[string]any{"listing": "open"})
	a, b := s.Servers[0], s.Servers[1]
	second := a.Owner.Channel("ZANZIBAR-PLANS", false)
	s.Share(0, second, "zanzibar", map[string]any{"listing": "request"})
	third := a.Owner.Channel("QUIETROOM", false)
	s.Share(0, third, "quiet", nil)
	// An ad-hoc channel between ann and b's owner crosses the hub too.
	chat := s.ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(b.Owner.ID)}})["conversation"].(map[string]any)
	s.ann.Post(chat["id"].(string), "ad hoc, through the hub")
	testkit.Eventually(t, "the ad-hoc stream is on the hub", func() bool {
		list, _ := s.Hub.Hub.Streams()
		return len(list) == 4
	})

	st := s.Hub.Hub.Status()
	if st.ChannelsOpen != 1 || st.ChannelsRequest != 1 || st.ChannelsUnlisted != 1 || st.StreamsOpen != 4 {
		t.Fatalf("status: %+v", st)
	}
	w := httptest.NewRecorder()
	s.Hub.Hub.StatusHandler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	page := w.Body.String()
	if !strings.Contains(page, "Channels: 1 unlisted, 1 by request, 1 open") {
		t.Fatalf("the status page does not give the counts:\n%s", page)
	}
	// (b.localhost is not looked for: it is inside the hub's own name.)
	for _, name := range []string{"launch", "ZANZIBAR-PLANS", "QUIETROOM", "a.localhost", "owner", string(a.Owner.ID), string(s.stream)} {
		if strings.Contains(page, name) {
			t.Errorf("the status page names %q", name)
		}
	}
	// The operator sees every channel's name, description, moderators and
	// servers; and of the ad-hoc stream only what a blind relay sees.
	list, err := s.Hub.Hub.Channels()
	if err != nil || len(list) != 3 {
		t.Fatalf("the operator's list: %+v %v", list, err)
	}
	seen := map[string]hub.ChannelInfo{}
	for _, c := range list {
		seen[c.Name] = c
	}
	if c := seen["launch"]; strings.Join(c.Servers, ",") != "a.localhost,b.localhost" || len(c.Managers) != 1 || c.Managers[0].Username != "owner" || c.Listing != "open" {
		t.Fatalf("the operator's view of launch: %+v", c)
	}
	if _, ok := seen["QUIETROOM"]; !ok {
		t.Fatal("the operator does not see the unlisted channel")
	}
	streams, _ := s.Hub.Hub.Streams()
	owners := map[string]int{}
	for _, st := range streams {
		owners[st.Owner]++
	}
	if owners["hub.localhost"] != 3 || owners["a.localhost"] != 1 {
		t.Fatalf("who owns the hub's streams: %v", owners)
	}
	// The ad-hoc stream has no record anywhere and no moderators' actions.
	var adhoc store.Stream
	all, _ := a.Server.Store.Streams().List(ctx, "")
	for _, st := range all {
		if st.Kind == store.StreamAdhoc {
			adhoc = st
		}
	}
	if adhoc.ID == "" || !adhoc.Owned() {
		t.Fatalf("a does not own the ad-hoc stream: %+v", adhoc)
	}
	s.ann.Refused(404, model.CodeNotFound, "GET", "/api/v1/streams/"+string(adhoc.ID)+"/channel", nil)
	a.Owner.Refused(404, model.CodeNotFound, "POST", "/api/v1/streams/"+string(adhoc.ID)+"/channel/actions", map[string]any{"action": "close"})
	if dir := directory(s.Linked, 1); len(dir) != 2 {
		t.Fatalf("b's directory: %v", dir)
	}
	_ = link.EventChannelChanged
}

// A server that is not connected at the moment it is admitted is sent
// neither its key nor its history: nobody keeps a direct message for it.
// When it returns it has the hub's record, asks for the key of the epoch it
// joined at and for the snapshot a joining server is owed, and is then in
// the channel like any other.
func TestServerAdmittedWhileAway(t *testing.T) {
	defer link.SetFirstHistoryWait(300 * time.Millisecond)()
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	ann := a.Member("ann")
	ch := a.Owner.Channel("committee", false, ann)
	stream, local := l.Share(0, ch, "committee", map[string]any{"listing": "request", "history_days": 30}, 1)
	sid := model.StreamID(stream)
	ann.Post(ch, "said before c asked")
	convC := joinListed(l, 2, stream, "committee-c")
	testkit.Eventually(t, "the moderator sees the request", func() bool { return names(hubChannel(a.Owner, stream)["requests"]) == "c.localhost" })
	// c goes away; then it is approved, and the channel goes on without it.
	path := "/api/v1/admin/peers/" + l.Peers[2]
	c.Owner.Must(200, "PATCH", path, map[string]any{"state": "paused"})
	testkit.Eventually(t, "c is off the hub", func() bool { return !c.Server.Link.TestConnected(model.PeerID(l.Peers[2])) })
	moderate(a.Owner, stream, map[string]any{"action": "approve", "server": "c.localhost"})
	testkit.Eventually(t, "the epoch c joined at has begun", func() bool {
		sa, _ := a.Server.Link.TestStream(sid)
		sb, _ := b.Server.Link.TestStream(sid)
		return sa.KeyEpoch == 3 && sb.KeyEpoch == 3
	})
	ann.Post(ch, "said while c was away")
	b.Owner.HasMessage(local[1], "said while c was away")
	if streamState(l, 2, stream) != "offered" || len(c.Server.Link.TestKeys(sid)) != 0 {
		t.Fatal("c has something of the channel though it was away")
	}
	// It returns.
	c.Owner.Must(200, "PATCH", path, map[string]any{"state": "active"})
	l.Joined(2, stream)
	c.Owner.HasMessage(convC, "said before c asked")   // history, asked for
	c.Owner.HasMessage(convC, "said while c was away") // the log, under a key it asked for
	testkit.Eventually(t, "c knows ann", func() bool {
		_, found, _ := c.Server.Store.Members().Get(ctx, model.ConversationID(convC), ann.ID)
		return found
	})
	c.Owner.Post(convC, "c is back and in")
	ann.HasMessage(ch, "c is back and in")
	b.Owner.HasMessage(local[1], "c is back and in")
	ann.Post(ch, "and hears what follows")
	c.Owner.HasMessage(convC, "and hears what follows")
}

// A named channel on a hub is the hub's: no server offers one as its own
// over a hub, and one that does is refused and leaves nothing behind.
func TestChannelOfferOverAHubIsRefused(t *testing.T) {
	l := testkit.NewLinked(t, 2, true)
	a, b := l.Servers[0], l.Servers[1]
	id := model.NewID[model.StreamID]()
	kind, body, err := b.Server.Link.TestAskReply(model.PeerID(l.Peers[1]), "a.localhost", "offer", map[string]any{
		"stream_id": id, "stream_kind": "channel", "owner": "b.localhost", "name": "mine-to-run", "topic": "",
		"servers": []string{"b.localhost"}, "invited": []string{"a.localhost"}, "history_days": 0, "max_blob_bytes": 1 << 20,
		"expires_at": model.Now().Add(time.Hour)})
	if err != nil || kind != "decline" || body["reason"] != "not_allowed" {
		t.Fatalf("a channel offered by a server over a hub: %s %v %v", kind, body, err)
	}
	if _, err := a.Server.Store.Streams().Get(ctx, id); err == nil {
		t.Fatal("the offer left a stream behind on a")
	}
	if l.StreamOn(0, string(id)) != nil {
		t.Fatal("a's owner is shown the offer")
	}
}

// An event the hub took, submitted again under a newer key epoch because its
// origin never heard that it was taken, is acknowledged again and not
// refused. A refusal would have the origin stamp it anew, and the event
// would then stand at one stamp on the servers that were delivered it and
// at another on its own.
func TestEventTakenUnderAnOlderEpochIsAcknowledgedAgain(t *testing.T) {
	s := share(t, 3, true, nil)
	a, b := s.Servers[0], s.Servers[1]
	stream := string(s.stream)
	st, _ := s.link(1).TestStream(s.stream)
	stamp, err := s.link(1).TestStamp(s.stream)
	if err != nil {
		t.Fatal(err)
	}
	fields, _ := message(b.Owner.ID, "taken once, sent twice")
	id := model.NewID[string]()
	first, err := s.link(1).TestSealAs(s.stream, id, "message.created", fields, stamp, st.KeyEpoch)
	if err != nil {
		t.Fatal(err)
	}
	if answer, err := s.link(1).TestSubmit(s.stream, first); err != nil || !link.Took(answer, first) {
		t.Fatalf("the hub did not take the event: %+v %v", answer, err)
	}
	s.ann.HasMessage(s.conv[0], "taken once, sent twice")
	// The key epoch moves on.
	moderate(a.Owner, stream, map[string]any{"action": "remove_server", "server": "c.localhost"})
	testkit.Eventually(t, "b is on the new epoch", func() bool {
		cur, _ := s.link(1).TestStream(s.stream)
		return cur.KeyEpoch == st.KeyEpoch+1
	})
	again, err := s.link(1).TestSealAs(s.stream, id, "message.created", fields, stamp, st.KeyEpoch+1)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := s.link(1).TestSubmit(s.stream, again)
	if err != nil || answer.Code != "" || !link.Took(answer, again) {
		t.Fatalf("the same event under the next epoch: %+v %v", answer, err)
	}
	held := 0
	for _, env := range s.Hub.Hub.TestLog(stream) {
		if env.OriginEventID == id {
			held++
		}
	}
	if held != 1 {
		t.Fatalf("the hub's log holds the event %d times", held)
	}
	n := 0
	for _, m := range s.ann.Messages(s.conv[0]) {
		if m["text"] == "taken once, sent twice" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("a holds the message %d times", n)
	}
}
