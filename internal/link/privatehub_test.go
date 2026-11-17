package link_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// A private channel shared through a hub (ruling R94; docs/LINK-PROTOCOL.md
// section 23a, "Private channels"): never listed, and its people admitted on
// every instance by its moderators and managers, from any instance in it.

// joinNotices counts the member_joined notices of an account in a
// conversation as a client is shown them.
func joinNotices(c *testkit.Client, conv string, who model.AccountID) int {
	n := 0
	for _, m := range c.Messages(conv) {
		if sys, ok := m["system"].(map[string]any); ok && sys["kind"] == "member_joined" && m["author_id"] == string(who) {
			n++
		}
	}
	return n
}

func isMemberOf(ts *testkit.TestServer, conv string, who model.AccountID) bool {
	_, found, err := ts.Server.Store.Members().Get(ctx, model.ConversationID(conv), who)
	return err == nil && found
}

func levelOf(ts *testkit.TestServer, conv string, who model.AccountID) model.Level {
	m, found, err := ts.Server.Store.Members().Get(ctx, model.ConversationID(conv), who)
	if err != nil || !found {
		return ""
	}
	return m.Level
}

func memberAction(c *testkit.Client, stream, action string, who model.AccountID) (int, map[string]any) {
	return c.Do("POST", "/api/v1/streams/"+stream+"/channel/members", map[string]any{"action": action, "account_id": who})
}

func requestsOf(c *testkit.Client, stream string) string {
	var out []string
	for _, raw := range arr(c.Must(200, "GET", "/api/v1/streams/"+stream+"/channel/requests", nil)["requests"]) {
		out = append(out, raw.(map[string]any)["username"].(string)+"@"+raw.(map[string]any)["server"].(string))
	}
	return strings.Join(out, ",")
}

func accountNames(v any) string {
	var out []string
	for _, raw := range arr(v) {
		m := raw.(map[string]any)
		out = append(out, m["username"].(string)+"@"+m["server"].(string))
	}
	return strings.Join(out, ",")
}

// hubSigned is a record signed with the hub's own key.
func hubSigned(t *testing.T, l *testkit.Linked, rec wire.ChannelRecord) wire.Frame {
	t.Helper()
	f, err := wire.SignBlock(wire.FChannel, rec, l.Hub.Hub.TestKeys())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPrivateChannelThroughAHub(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	ann, max := a.Member("ann"), a.Member("max")
	ch := a.Owner.Channel("vault", true, ann, max)
	a.Owner.Must(200, "PATCH", "/api/v1/conversations/"+ch+"/members/"+string(max.ID), map[string]any{"level": "manager"})
	ann.Post(ch, "said before sharing")
	noticesBefore := len(a.Owner.Messages(ch))

	// A private channel is never shared listed.
	other := a.Owner.Channel("other-vault", true)
	a.Owner.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/streams",
		map[string]any{"conversation_id": other, "peer_id": l.Peers[0], "listing": "open"})

	stream, local := l.Share(0, ch, "vault-here", map[string]any{"history_days": 30}, 1, 2)
	sid := model.StreamID(stream)

	// The record: private, unlisted; the channel's managers came with it
	// and are its managers on the hub, and nobody is a moderator yet.
	rec := hubChannel(a.Owner, stream)
	if rec["private"] != true || rec["listing"] != "unlisted" || accountNames(rec["managers"]) != "owner@a.localhost,max@a.localhost" ||
		len(arr(rec["moderators"])) != 0 {
		t.Fatalf("the record of a private channel: %v", rec)
	}
	// On a nothing else changed: the same people at the same levels, and
	// nobody announced again.
	if levelOf(a, ch, ann.ID) != model.LevelMember || levelOf(a, ch, max.ID) != model.LevelManager || len(a.Owner.Messages(ch)) != noticesBefore {
		t.Fatalf("sharing changed a's channel: ann %s, max %s, %d messages", levelOf(a, ch, ann.ID), levelOf(a, ch, max.ID), len(a.Owner.Messages(ch)))
	}
	// The other instances' copies are private and hold none of their own
	// people: not even the owner who accepted.
	for _, i := range []int{1, 2} {
		conv, err := l.Servers[i].Server.Store.Conversations().Get(ctx, model.ConversationID(local[i]))
		if err != nil || !conv.Private {
			t.Fatalf("%s's copy: %+v %v", testkit.ServerName(i), conv, err)
		}
		if isMemberOf(l.Servers[i], local[i], l.Servers[i].Owner.ID) {
			t.Fatalf("%s's owner is in the copy without being admitted", testkit.ServerName(i))
		}
	}

	// Never listed: not by a manager, not straight at the hub, and never in
	// the directory.
	a.Owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/streams/"+stream+"/channel/actions", map[string]any{"action": "set_listing", "listing": "open"})
	honest := l.Servers[0].Server.Link.TestSignBlock(wire.FChannelAction, wire.ChannelAction{StreamID: stream, ActionID: model.NewID[string](),
		Action: wire.ActSetListing, Listing: wire.ListingRequest, By: wire.ChannelAccount{AccountID: string(a.Owner.ID), Server: "a.localhost", Username: "owner"},
		At: model.Now()})
	if answer, err := l.Servers[0].Server.Link.TestHubFrame(model.PeerID(l.Peers[0]), honest, 2*time.Second); err != nil || answer.Code != wire.EForbidden {
		t.Fatalf("the hub took set_listing for a private channel: %+v %v", answer, err)
	}
	for _, i := range []int{0, 1, 2} {
		if dir := directory(l, i); len(dir) != 0 {
			t.Fatalf("%s's directory: %v", testkit.ServerName(i), dir)
		}
	}

	// A record that would make it listed or not private, even signed by the
	// hub's own key, is refused by an instance that holds it as private.
	held, _, _, _ := b.Server.Link.TestChannel(sid)
	open := held
	open.Version, open.Private, open.Listing = held.Version+1, false, wire.ListingOpen
	err := b.Server.Link.TestInjectChannel(model.PeerID(l.Peers[1]), hubSigned(t, l, open))
	if wire.AsError(err).Code != wire.EForbidden {
		t.Fatalf("a record that drops the private flag: %v", err)
	}
	listed := held
	listed.Version, listed.Listing = held.Version+1, wire.ListingOpen
	err = b.Server.Link.TestInjectChannel(model.PeerID(l.Peers[1]), hubSigned(t, l, listed))
	if wire.AsError(err).Code != wire.EBadFrame {
		t.Fatalf("a record that lists a private channel: %v", err)
	}
	if now, _, _, _ := b.Server.Link.TestChannel(sid); now.Version != held.Version || !now.Private {
		t.Fatalf("b's record changed: %+v", now)
	}

	// Nobody but a moderator or manager adds people: not b's owner, who is
	// no manager of the channel, and not a manager of a's copy who is
	// taken off the hub's list (and with it stops being one here).
	bea, cid, cal := b.Member("bea"), c.Member("cid"), c.Member("cal")
	nia := a.Member("nia")
	moderate(a.Owner, stream, map[string]any{"action": "remove_manager", "account_id": max.ID})
	testkit.Eventually(t, "max is no longer a manager of a's copy", func() bool { return levelOf(a, ch, max.ID) == model.LevelMember })
	max.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+ch+"/members", map[string]any{"account_ids": []string{string(nia.ID)}})
	a.Owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+ch+"/members", map[string]any{"account_ids": []string{string(nia.ID)}})
	a.Owner.Refused(409, model.CodeConflict, "PATCH", "/api/v1/conversations/"+ch+"/members/"+string(ann.ID), map[string]any{"level": "manager"})
	if got, _ := b.Owner.Do("POST", "/api/v1/conversations/"+local[1]+"/members", map[string]any{"account_ids": []string{string(bea.ID)}}); got != http.StatusForbidden && got != http.StatusNotFound {
		t.Fatalf("b's owner added bea to b's copy: %d", got)
	}
	if got, _ := memberAction(b.Owner, stream, "admit", bea.ID); got != http.StatusForbidden {
		t.Fatalf("b's owner admitted bea: %d", got)
	}
	if isMemberOf(b, local[1], bea.ID) || isMemberOf(a, ch, nia.ID) {
		t.Fatal("someone was added without a moderator")
	}

	// Asking. bea has the join code (the stream's identifier); the request
	// reaches a, whose manager admits her.
	bea.Must(200, "POST", "/api/v1/streams/"+stream+"/channel/requests", nil)
	testkit.Eventually(t, "a's manager sees bea's request", func() bool { return requestsOf(a.Owner, stream) == "bea@b.localhost" })
	if got, out := memberAction(a.Owner, stream, "admit", bea.ID); got != http.StatusNoContent {
		t.Fatalf("admitting bea: %d %v", got, out)
	}
	testkit.Eventually(t, "bea is in b's copy", func() bool { return isMemberOf(b, local[1], bea.ID) })
	testkit.Eventually(t, "a and c know bea is in", func() bool { return isMemberOf(a, ch, bea.ID) && isMemberOf(c, local[2], bea.ID) })
	if requestsOf(a.Owner, stream) != "" {
		t.Fatal("bea's request still waits")
	}

	// bea is made a moderator; from b she admits someone of c.
	moderate(a.Owner, stream, map[string]any{"action": "add_moderator", "account_id": bea.ID})
	testkit.Eventually(t, "b knows bea is a moderator", func() bool {
		return b.Owner.Must(200, "GET", "/api/v1/streams/"+stream+"/channel", nil)["channel"].(map[string]any)["moderators"] != nil &&
			accountNames(hubChannel(b.Owner, stream)["moderators"]) == "bea@b.localhost"
	})
	if bea.Must(200, "GET", "/api/v1/streams/"+stream+"/channel", nil)["moderator"] != true {
		t.Fatal("bea is not shown as a moderator")
	}
	// A moderator cannot do a manager's work.
	bea.Refused(403, model.CodeForbidden, "POST", "/api/v1/streams/"+stream+"/channel/actions", map[string]any{"action": "describe", "name": "beas"})

	cid.Must(200, "POST", "/api/v1/streams/"+stream+"/channel/requests", nil)
	testkit.Eventually(t, "bea sees cid's request on b", func() bool { return requestsOf(bea, stream) == "cid@c.localhost" })
	if got, _ := memberAction(bea, stream, "admit", cid.ID); got != http.StatusNoContent {
		t.Fatalf("bea admitting cid: %d", got)
	}
	testkit.Eventually(t, "cid is in c's copy", func() bool { return isMemberOf(c, local[2], cid.ID) })
	ann.Post(ch, "said once cid was in")
	cid.HasMessage(local[2], "said once cid was in")
	bea.HasMessage(local[1], "said once cid was in")
	// Announced once on every instance, with who admitted.
	testkit.Eventually(t, "a shows cid's arrival", func() bool { return joinNotices(ann, ch, cid.ID) == 1 })
	for i, who := range map[int]*testkit.Client{1: bea, 2: cid} {
		if n := joinNotices(who, local[i], cid.ID); n != 1 {
			t.Fatalf("%s shows %d notices of cid's arrival", testkit.ServerName(i), n)
		}
	}
	for _, m := range cid.Messages(local[2]) {
		if sys, ok := m["system"].(map[string]any); ok && sys["kind"] == "member_joined" && m["author_id"] == string(cid.ID) && sys["by_id"] != string(bea.ID) {
			t.Fatalf("cid's notice does not name bea: %v", sys)
		}
	}
	// Each reads from their own first join on their own instance.
	for i, who := range map[int]*testkit.Client{1: bea, 2: cid} {
		if got := textsOf(who.Messages(local[i])); strings.Contains(got, "before sharing") {
			t.Fatalf("%s reads from before they joined: %s", testkit.ServerName(i), got)
		}
	}

	// Forged admissions: by an account on no list, and in the name of a's
	// manager from another instance. Every instance refuses them.
	s := &shared{Linked: l, t: t, stream: sid, conv: local, ann: ann}
	s.submit(2, "member.admitted", map[string]any{"account_id": cal.ID, "server": "c.localhost", "by_account_id": cid.ID})
	s.submit(1, "member.admitted", map[string]any{"account_id": cal.ID, "server": "c.localhost", "by_account_id": a.Owner.ID})
	s.rejected(0, "not_moderator", 2)
	s.rejected(1, "not_moderator", 1)
	s.rejected(2, "not_moderator", 1)
	time.Sleep(100 * time.Millisecond)
	if isMemberOf(c, local[2], cal.ID) {
		t.Fatal("a forged admission let cal in")
	}

	// Declined: cal asks; a's owner declines; the request is gone on b too.
	cal.Must(200, "POST", "/api/v1/streams/"+stream+"/channel/requests", nil)
	testkit.Eventually(t, "bea sees cal's request", func() bool { return requestsOf(bea, stream) == "cal@c.localhost" })
	if got, _ := memberAction(a.Owner, stream, "decline", cal.ID); got != http.StatusNoContent {
		t.Fatalf("declining cal: %d", got)
	}
	testkit.Eventually(t, "the request is gone on b", func() bool { return requestsOf(bea, stream) == "" })

	// Removed by a moderator on another instance: on every instance.
	if got, _ := memberAction(bea, stream, "remove", ann.ID); got != http.StatusNoContent {
		t.Fatalf("bea removing ann: %d", got)
	}
	testkit.Eventually(t, "ann is out everywhere", func() bool {
		return !isMemberOf(a, ch, ann.ID) && !isMemberOf(b, local[1], ann.ID) && !isMemberOf(c, local[2], ann.ID)
	})

	// After a restart b still holds it as private, and still adds nobody.
	b.Restart()
	l.Joined(1, stream)
	if rec := hubChannel(bea, stream); rec["private"] != true || rec["listing"] != "unlisted" {
		t.Fatalf("b's record after a restart: %v", rec)
	}
	if got, _ := b.Owner.Do("POST", "/api/v1/conversations/"+local[1]+"/members", map[string]any{"account_ids": []string{string(b.Owner.ID)}}); got != http.StatusForbidden && got != http.StatusNotFound {
		t.Fatalf("b's owner added themselves after a restart: %d", got)
	}
	bea.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+local[1]+"/members", map[string]any{"account_ids": []string{string(b.Owner.ID)}})
}

// A private channel two instances share over a direct link moves onto a
// hub whole: both copies keep their people, levels and first joins; the
// managers of both become its managers on the hub; it is private there.
func TestPrivateChannelMovesOntoAHub(t *testing.T) {
	linked := testkit.NewLinked(t, 3, true)
	f := &moveFix{Linked: linked, t: t, hubName: "hub.localhost"}
	a, b := f.Servers[0], f.Servers[1]
	made := a.Owner.Must(201, "POST", "/api/v1/admin/peers/invitations", nil)
	f.direct[0] = made["peer"].(map[string]any)["id"].(string)
	f.direct[1] = b.Owner.Must(201, "POST", "/api/v1/admin/peers", map[string]any{"invitation": made["invitation"]})["peer"].(map[string]any)["id"].(string)
	testkit.Eventually(t, "the direct link is up", func() bool {
		pa := a.Owner.Must(200, "GET", "/api/v1/admin/peers/"+f.direct[0], nil)["peer"].(map[string]any)
		return pa["state"] == "active" && pa["connected_at"] != nil
	})
	f.ann, f.amy = a.Member("ann"), a.Member("amy")
	f.conv[0] = a.Owner.Channel("launch", true, f.ann, f.amy)
	f.amy.Post(f.conv[0], "before the share")
	f.stream, f.conv[1] = f.shareDirect(f.conv[0], "launch")
	f.bea, f.bo = b.Member("bea"), b.Member("bo")
	b.Owner.Must(200, "POST", "/api/v1/conversations/"+f.conv[1]+"/members", map[string]any{"account_ids": []string{string(f.bea.ID), string(f.bo.ID)}})
	b.Owner.Must(200, "PATCH", "/api/v1/conversations/"+f.conv[1]+"/members/"+string(f.bea.ID), map[string]any{"level": "manager"})
	f.amy.Post(f.conv[0], "before the move")
	f.bo.HasMessage(f.conv[1], "before the move")
	firstJoin := func(i int, conv string, who model.AccountID) model.Time {
		m, _, _ := f.Servers[i].Server.Store.Members().Get(ctx, model.ConversationID(conv), who)
		return m.VisibleFrom
	}
	boFrom, amyFrom := firstJoin(1, f.conv[1], f.bo.ID), firstJoin(0, f.conv[0], f.amy.ID)
	before := [2]int{len(a.Owner.Messages(f.conv[0])), len(b.Owner.Messages(f.conv[1]))}

	id := f.propose(nil)
	f.accept(id)
	f.onHub(id)
	rec := hubChannel(a.Owner, f.stream)
	if rec["private"] != true || rec["listing"] != "unlisted" {
		t.Fatalf("the record of a moved private channel: %v", rec)
	}
	// Managers on both sides stay managers: a's owner (an admin in it) and
	// b's owner and bea.
	got := accountNames(rec["managers"])
	for _, want := range []string{"owner@a.localhost", "owner@b.localhost", "bea@b.localhost"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the managers after the move: %s", got)
		}
	}
	if levelOf(b, f.conv[1], f.bea.ID) != model.LevelManager || levelOf(b, f.conv[1], f.bo.ID) != model.LevelMember ||
		levelOf(a, f.conv[0], f.amy.ID) != model.LevelMember {
		t.Fatal("levels changed in the move")
	}
	if firstJoin(1, f.conv[1], f.bo.ID) != boFrom || firstJoin(0, f.conv[0], f.amy.ID) != amyFrom {
		t.Fatal("first joins changed in the move")
	}
	if got := textsOf(f.bo.Messages(f.conv[1])); !strings.Contains(got, "before the move") || strings.Contains(got, "before the share") {
		t.Fatalf("what bo reads after the move: %s", got)
	}
	// Nobody is announced again; the only new messages are what the move
	// itself says.
	f.amy.Post(f.conv[0], "after the move")
	f.bo.HasMessage(f.conv[1], "after the move")
	for i, conv := range []string{f.conv[0], f.conv[1]} {
		for _, m := range f.Servers[i].Owner.Messages(conv)[before[i]:] {
			if sys, ok := m["system"].(map[string]any); ok && sys["kind"] == "member_joined" {
				t.Fatalf("%s announced an arrival at the move: %v", testkit.ServerName(i), m)
			}
		}
	}
	// And it is a private channel of the hub's from here on.
	a.Owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/streams/"+f.stream+"/channel/actions", map[string]any{"action": "set_listing", "listing": "open"})
	f.bea.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+f.conv[1]+"/members", map[string]any{"account_ids": []string{string(b.Owner.ID)}})
}
