package link_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/link"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The cases of docs/LINK-PROTOCOL.md section 25 that need whole servers.
// Each test names the cases it proves. A server that misbehaves is played
// with a real server's own keys through the test-only doors of
// export_test.go: what it submits is validly signed and wrong.

var ctx = context.Background()

// arr reads a list that the API leaves out when it is empty.
func arr(v any) []any {
	a, _ := v.([]any)
	return a
}

// shared is two servers on a direct link (A owns, and so is the time
// reference) or three through a hub, with one channel shared between all of
// them.
type shared struct {
	*testkit.Linked
	t      *testing.T
	stream model.StreamID
	conv   map[int]string
	ann    *testkit.Client // a member of A, in the channel
}

func share(t *testing.T, n int, hub bool, body map[string]any) *shared {
	t.Helper()
	return shareOver(t, testkit.NewLinked(t, n, hub), n, body)
}

// shareBy is share by the name of how the servers are linked: "direct",
// "hub", or "cluster", which is two hubs joined by a trunk with the first
// and third servers on one and the second on the other, so that whatever
// the first two servers do with each other crosses the trunk.
func shareBy(t *testing.T, how string) *shared {
	t.Helper()
	switch how {
	case "direct":
		return share(t, 2, false, nil)
	case "hub":
		return share(t, 3, true, nil)
	}
	return shareOver(t, testkit.NewClusterLinked(t, "line", 2, []int{0, 1, 0}).Linked, 3, nil)
}

// shareOver is share over servers that are linked already.
func shareOver(t *testing.T, linked *testkit.Linked, n int, body map[string]any) *shared {
	t.Helper()
	s := &shared{Linked: linked, t: t}
	s.ann = s.Servers[0].Member("ann")
	ch := s.Servers[0].Owner.Channel("launch", false, s.ann)
	var to []int
	for i := 1; i < n; i++ {
		to = append(to, i)
	}
	stream, conv := s.Share(0, ch, "launch", body, to...)
	s.stream, s.conv = model.StreamID(stream), conv
	// Every server knows every other's members before a test begins.
	for i := 1; i < n; i++ {
		owner := s.Servers[i].Owner
		testkit.Eventually(t, "a knows the owner of "+testkit.ServerName(i), func() bool {
			_, found, _ := s.Servers[0].Server.Store.Members().Get(ctx, model.ConversationID(conv[0]), owner.ID)
			return found
		})
		testkit.Eventually(t, testkit.ServerName(i)+" knows ann", func() bool {
			_, found, _ := s.Servers[i].Server.Store.Members().Get(ctx, model.ConversationID(conv[i]), s.ann.ID)
			return found
		})
	}
	return s
}

func (s *shared) link(i int) *link.Service { return s.Servers[i].Server.Link }

// keeper returns server i's owner as someone who may add people to its
// copy of the channel. An owner is no manager of a channel by being the
// owner (ruling R99): of a channel a hub owns the managers are those on the
// hub's list, so there server 0's owner, its first manager, puts them on it.
func (s *shared) keeper(i int) *testkit.Client {
	s.t.Helper()
	o := s.Servers[i].Owner
	if status, _ := s.Servers[0].Owner.Do("GET", "/api/v1/streams/"+string(s.stream)+"/channel", nil); status != 200 || i == 0 {
		return o
	}
	s.Servers[0].Owner.Must(200, "POST", "/api/v1/streams/"+string(s.stream)+"/channel/actions",
		map[string]any{"action": "add_manager", "account_id": o.ID})
	testkit.Eventually(s.t, testkit.ServerName(i)+"'s owner manages its copy", func() bool {
		m, found, _ := s.Servers[i].Server.Store.Members().Get(ctx, model.ConversationID(s.conv[i]), o.ID)
		return found && m.Level == model.LevelManager
	})
	return o
}

// message is the fields of a message.created event.
func message(author model.AccountID, text string) (map[string]any, string) {
	id := model.NewID[string]()
	return map[string]any{"message": map[string]any{"id": id, "author_id": author, "text": text, "created_at": model.Now()},
		"blobs": []any{}}, id
}

// submit makes server i submit an event of its own making, stamped as a
// well-behaved server would stamp it. The time reference has nothing to say
// against the stamp, so it takes the envelope: what is wrong with the event
// is inside, where only the servers can see.
func (s *shared) submit(i int, typ string, fields map[string]any) wire.Envelope {
	s.t.Helper()
	env, err := s.link(i).TestSealNext(s.stream, typ, fields, 0)
	if err != nil {
		s.t.Fatal(err)
	}
	answer, err := s.link(i).TestSubmit(s.stream, env)
	if err != nil {
		s.t.Fatal(err)
	}
	if !link.Took(answer, env) {
		s.t.Fatalf("the reference refused a soundly stamped envelope: %s: %s", answer.Code, answer.Message)
	}
	return env
}

// order returns the texts of a conversation's messages on server i as its
// members are shown them: sorted by `order`.
func (s *shared) order(i int) []string {
	s.t.Helper()
	return orderOf(s.Servers[i].Owner, s.conv[i])
}

// orderOf returns the texts of a conversation's messages sorted by `order`,
// which is how a client shows them.
func orderOf(c *testkit.Client, conv string) []string {
	msgs := c.Messages(conv)
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i]["order"].(string) < msgs[j]["order"].(string) })
	var out []string
	for _, m := range msgs {
		if text, _ := m["text"].(string); text != "" {
			out = append(out, text)
		}
	}
	return out
}

// rejected waits until server i has rejected at least n envelopes for a
// reason.
func (s *shared) rejected(i int, reason string, n int64) {
	s.t.Helper()
	testkit.Eventually(s.t, fmt.Sprintf("%s rejects %d for %s", testkit.ServerName(i), n, reason), func() bool {
		return s.link(i).StatsNow().Rejected[reason] >= n
	})
}

func (s *shared) dropped(i int, reason string) {
	s.t.Helper()
	testkit.Eventually(s.t, fmt.Sprintf("%s drops one for %s", testkit.ServerName(i), reason), func() bool {
		return s.link(i).StatsNow().Dropped[reason] >= 1
	})
}

// absent fails if a conversation on a server holds a message with this text.
func absent(t *testing.T, c *testkit.Client, conv, text string) {
	t.Helper()
	for _, m := range c.Messages(conv) {
		if m["text"] == text {
			t.Fatalf("%q is shown on a server that should have rejected it", text)
		}
	}
}

// countText is how many of a conversation's messages have this text.
func countText(c *testkit.Client, conv, text string) int {
	n := 0
	for _, m := range c.Messages(conv) {
		if m["text"] == text {
			n++
		}
	}
	return n
}

// settle posts a marker on A and waits for it everywhere: everything
// submitted before it has been dealt with by then.
func (s *shared) settle(marker string) {
	s.t.Helper()
	s.ann.Post(s.conv[0], marker)
	for i := 1; i < len(s.Servers); i++ {
		s.Servers[i].Owner.HasMessage(s.conv[i], marker)
	}
}

// --------------------------------------------------- identity, connection

// Cases 1 and 2: the published document has exactly the fields of section 3
// with a box_key_sig that verifies, and the fingerprint computed from its
// sign_key is the one in an invitation and the one the console shows.
func TestWellKnownAndFingerprints(t *testing.T) {
	a := testkit.NewServer(t, testkit.Options{Hostname: "a.localhost"})
	_, hdr, raw := a.Owner.Get("/.well-known/churr", nil)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"churr", "kind", "name", "sign_key", "box_key", "box_key_sig", "link"} {
		if _, ok := fields[f]; !ok {
			t.Errorf("the document lacks %s", f)
		}
		delete(fields, f)
	}
	if len(fields) != 0 || hdr.Get("Cache-Control") != "max-age=3600" {
		t.Errorf("extra fields %v, Cache-Control %q", fields, hdr.Get("Cache-Control"))
	}
	var doc wire.WellKnown
	json.Unmarshal(raw, &doc)
	if err := doc.Check("a.localhost"); err != nil {
		t.Fatal(err)
	}
	made := a.Owner.Must(201, "POST", "/api/v1/admin/peers/invitations", map[string]any{"expires_in_seconds": 3600})
	text := made["invitation"].(string)
	if !strings.HasPrefix(text, "churr_link_") {
		t.Fatalf("invitation %q", text)
	}
	inv, err := wire.ParseInvitation(text)
	if err != nil {
		t.Fatal(err)
	}
	fp := wire.KeyFingerprint(doc.SignKey)
	if wire.FingerprintText(inv.FP) != fp || a.Server.Link.Fingerprint() != fp {
		t.Fatalf("fingerprints differ: document %s, invitation %s, console %s", fp, wire.FingerprintText(inv.FP), a.Server.Link.Fingerprint())
	}
	// Another owner reading it sees the same before dialling.
	b := testkit.NewServer(t, testkit.Options{Hostname: "b.localhost"})
	seen := b.Owner.Must(200, "POST", "/api/v1/admin/peers/invitations/inspect", map[string]any{"invitation": text})
	if seen["fingerprint"] != fp || seen["name"] != "a.localhost" || seen["kind"] != "server" {
		t.Fatalf("inspect: %v", seen)
	}
	b.Owner.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/peers/invitations/inspect", map[string]any{"invitation": "churr_link_nonsense"})
	// Only the owner manages links.
	admin := a.Admin("adm")
	admin.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/peers", nil)
	admin.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/peers/invitations", nil)
	admin.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/streams", nil)
	// An unused invitation can be revoked.
	a.Owner.Must(204, "DELETE", "/api/v1/admin/peers/"+made["peer"].(map[string]any)["id"].(string), nil)
	b.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/peers", map[string]any{"invitation": text})
}

func stranger(t *testing.T, name string) wire.Identity {
	keys, err := secret.Open(filepath.Join(t.TempDir(), "keys"))
	if err != nil {
		t.Fatal(err)
	}
	return wire.Identity{Name: name, Kind: wire.KindServer, Keys: keys, Software: "churr/test"}
}

// Cases 4, 5, 7 and 8: an invitation works once; an expired one gets
// invite_expired; a known name with other keys gets key_mismatch; an unknown
// version gets version_unsupported.
func TestInvitationAndHandshakeRefusals(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a := l.Servers[0]
	client := testkit.LoopbackClient()
	// The invitation b used: find its secret again by making a fresh one
	// and using it twice.
	made := a.Owner.Must(201, "POST", "/api/v1/admin/peers/invitations", nil)
	inv, _ := wire.ParseInvitation(made["invitation"].(string))
	c1, err := wire.Dial(ctx, wire.DialOptions{URL: inv.URL, Identity: stranger(t, "c.localhost"), HTTPClient: client, InviteSecret: inv.Secret})
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	_, err = wire.Dial(ctx, wire.DialOptions{URL: inv.URL, Identity: stranger(t, "d.localhost"), HTTPClient: client, InviteSecret: inv.Secret})
	if wire.AsError(err).Code != wire.EInviteInvalid {
		t.Fatalf("a second server presenting a used invitation: %v", err)
	}
	// Expired.
	short := a.Owner.Must(201, "POST", "/api/v1/admin/peers/invitations", map[string]any{"expires_in_seconds": 1})
	old, _ := wire.ParseInvitation(short["invitation"].(string))
	time.Sleep(1100 * time.Millisecond)
	_, err = wire.Dial(ctx, wire.DialOptions{URL: old.URL, Identity: stranger(t, "e.localhost"), HTTPClient: client, InviteSecret: old.Secret})
	if wire.AsError(err).Code != wire.EInviteExpired {
		t.Fatalf("an expired invitation: %v", err)
	}
	// b.localhost is pinned; someone else under that name is refused.
	_, err = wire.Dial(ctx, wire.DialOptions{URL: inv.URL, Identity: stranger(t, "b.localhost"), HTTPClient: client})
	if wire.AsError(err).Code != wire.EKeyMismatch {
		t.Fatalf("a pinned name with other keys: %v", err)
	}
	_, err = wire.Dial(ctx, wire.DialOptions{URL: inv.URL, Identity: stranger(t, "f.localhost"), HTTPClient: client, Versions: []int{9}})
	if wire.AsError(err).Code != wire.EVersionUnsupported {
		t.Fatalf("an unknown version: %v", err)
	}
	// No invitation and not a peer.
	_, err = wire.Dial(ctx, wire.DialOptions{URL: inv.URL, Identity: stranger(t, "g.localhost"), HTTPClient: client})
	if wire.AsError(err).Code != wire.EInviteInvalid {
		t.Fatalf("no invitation: %v", err)
	}
}

// Case 10: after the connection is cut the dialler reconnects, with waits
// that grow; and a second connection replaces the first.
func TestReconnect(t *testing.T) {
	defer link.SetDialBase(20 * time.Millisecond)()
	for i := 1; i < 12; i++ {
		lo, hi := link.NextWait(i, 0), link.NextWait(i, 0.999)
		want := time.Second << (i - 1)
		if want > 5*time.Minute {
			want = 5 * time.Minute
		}
		if lo < want*8/10-time.Millisecond || hi > want*12/10+time.Millisecond {
			t.Fatalf("after %d failures the wait is %v to %v, want %v give or take a fifth", i, lo, hi, want)
		}
	}
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0].Server.Link, l.Servers[1].Server.Link
	peerA, peerB := model.PeerID(l.Peers[0]), model.PeerID(l.Peers[1])
	a.TestDrop(peerA)
	testkit.Eventually(t, "b dials again", func() bool {
		return b.StatsNow().Dials[peerB] >= 1 && a.TestConnected(peerA) && b.TestConnected(peerB)
	})
	// A second connection for the same peer replaces the first.
	conn, err := wire.Dial(ctx, wire.DialOptions{URL: a.Identity().URL, Identity: b.Identity(), HTTPClient: testkit.LoopbackClient()})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	testkit.Eventually(t, "the first connection is told it was replaced", func() bool {
		return b.StatsNow().Dropped["error:"+wire.EReplaced] >= 1
	})
}

// Case 11: a hub invitation made with --for is refused for another name.
// Case 67: the status page names no server and no stream. Case 63 is in
// TestWhatTheHubStores.
func TestHubInvitationForAnotherName(t *testing.T) {
	h := testkit.NewHub(t)
	a := testkit.NewServer(t, testkit.Options{Hostname: "a.localhost"})
	a.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/peers", map[string]any{"invitation": h.Invite("z.localhost")})
	if list, _ := h.Hub.Invites(); len(list) != 1 || list[0].For != "z.localhost" {
		t.Fatalf("the invitation should still be unused: %v", list)
	}
	if err := h.Hub.RevokeInvite(func() string { l, _ := h.Hub.Invites(); return l[0].ID }()); err != nil {
		t.Fatal(err)
	}
	a.Owner.Must(201, "POST", "/api/v1/admin/peers", map[string]any{"invitation": h.Invite("a.localhost")})
}

// ------------------------------------------------- sharing and membership

// Case 15, and the first half of the brief's definition of done: a server
// receives nothing, neither envelopes nor ephemeral frames, from a stream
// it is not in. Case 50: presence reaches only servers that share a channel
// with the account, and nothing is sent when sending is off.
func TestNothingFromAStreamAServerIsNotIn(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	ann := a.Member("ann")
	ch := a.Owner.Channel("private-to-a-and-b", false, ann)
	stream, local := l.Share(0, ch, "from-a", nil, 1)
	ann.Post(ch, "not for c")
	b.Owner.HasMessage(local[1], "not for c")
	ann.Must(204, "POST", "/api/v1/conversations/"+ch+"/typing", map[string]any{})
	a.Server.Link.PresenceChanged(ann.ID)
	id := model.StreamID(stream)
	testkit.Eventually(t, "typing and presence reach b", func() bool { return b.Server.Link.StatsNow().Ephemeral[id] >= 2 })
	if _, ok := b.Server.Link.PresenceOf(ann.ID); !ok {
		t.Fatal("b holds no presence for ann")
	}
	stats := c.Server.Link.StatsNow()
	if len(stats.Delivered) != 0 || len(stats.Ephemeral) != 0 {
		t.Fatalf("c was sent something from a stream it is not in: %+v", stats)
	}
	if l.StreamOn(2, stream) != nil {
		t.Fatal("c knows of the stream")
	}
	if list, _ := c.Server.Store.Streams().List(ctx, ""); len(list) != 0 {
		t.Fatalf("c holds stream rows: %v", list)
	}
	// c cannot have the log either: the hub serves it only to servers in
	// the stream's list.
	hubStreams, _ := l.Hub.Hub.Streams()
	if len(hubStreams) != 1 || strings.Join(hubStreams[0].Servers, ",") != "a.localhost,b.localhost" {
		t.Fatalf("the hub's list: %+v", hubStreams)
	}
	// The off switch.
	if err := a.Server.Store.Settings().Set(ctx, link.SettingSendPresence, false, ""); err != nil {
		t.Fatal(err)
	}
	before := b.Server.Link.StatsNow().Ephemeral[id]
	ann.Must(204, "POST", "/api/v1/conversations/"+ch+"/typing", map[string]any{})
	a.Server.Link.PresenceChanged(ann.ID)
	time.Sleep(200 * time.Millisecond)
	if after := b.Server.Link.StatsNow().Ephemeral[id]; after != before {
		t.Fatalf("presence or typing was sent with sending off: %d then %d", before, after)
	}
}

// Case 16: a declined offer leaves no stream on the invited server and no
// key with it.
func TestDeclinedOffer(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	ch := a.Owner.Channel("launch", false)
	stream := a.Owner.Must(201, "POST", "/api/v1/admin/streams", map[string]any{"conversation_id": ch, "peer_id": l.Peers[0]})["stream"].(map[string]any)["id"].(string)
	testkit.Eventually(t, "the offer reaches b", func() bool { return l.StreamOn(1, stream) != nil })
	offer := l.StreamOn(1, stream)
	if offer["state"] != "offered" || offer["offered_name"] != "launch" || offer["owner_server"] != "a.localhost" {
		t.Fatalf("the offer: %v", offer)
	}
	b.Owner.Must(204, "POST", "/api/v1/admin/streams/"+stream+"/decline", nil)
	if l.StreamOn(1, stream) != nil {
		t.Fatal("the declined stream is still listed")
	}
	if keys := b.Server.Link.TestKeys(model.StreamID(stream)); len(keys) != 0 {
		t.Fatal("b holds a key for a stream it declined")
	}
	b.Owner.Refused(404, model.CodeNotFound, "POST", "/api/v1/admin/streams/"+stream+"/accept", map[string]any{"name": "x"})
	testkit.Eventually(t, "a learns of the refusal", func() bool {
		for _, sv := range l.StreamOn(0, stream)["servers"].([]any) {
			if m := sv.(map[string]any); m["name"] == "b.localhost" && m["state"] == "removed" {
				return true
			}
		}
		return false
	})
}

// Case 17: a server joining with history_days of 30 holds messages from the
// last 30 days in the place they have on the owner and nothing older. Case 18: it
// cannot decrypt an envelope from before it joined, even when handed the
// ciphertext.
func TestHistoryOnJoining(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	ann := a.Member("ann")
	ch := a.Owner.Channel("launch", false, ann)
	// A message from forty days ago, stored the way the service stores one.
	old := model.Message{ID: model.NewID[model.MessageID](), ConversationID: model.ConversationID(ch), AuthorID: ann.ID,
		Text: "forty days old", CreatedAt: model.Now().Add(-40 * 24 * time.Hour)}
	err := a.Server.Store.Tx(ctx, func(tx store.Tx) error {
		seq, err := tx.Conversations().NextSeq(ctx, old.ConversationID)
		if err != nil {
			return err
		}
		old.Seq = seq
		return tx.Messages().Insert(ctx, &old, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	recent := ann.Post(ch, "this month")
	stream, local := l.Share(0, ch, "launch", map[string]any{"history_days": 30}, 1)
	got := b.Owner.HasMessage(local[1], "this month")
	if got["order"] != recent["order"] || got["id"] != recent["id"] {
		t.Fatalf("history kept neither id nor order: %v, want %v", got, recent)
	}
	absent(t, b.Owner, local[1], "forty days old")
	// Members came with the snapshot, as remote accounts with the same id.
	acc := b.Owner.Must(200, "GET", "/api/v1/accounts/"+string(ann.ID), nil)["account"].(map[string]any)
	if acc["username"] != "ann" || acc["server_name"] != "a.localhost" || acc["role"] != "remote" {
		t.Fatalf("ann on b: %v", acc)
	}

	// What a wrote before b joined is under epoch 1, which b was never
	// given.
	id := model.StreamID(stream)
	keysB := b.Server.Link.TestKeys(id)
	if _, has := keysB[1]; has || len(keysB) == 0 {
		t.Fatalf("b's epochs: %v", keysB)
	}
	before, err := a.Server.Link.TestSeal(id, "message.created", map[string]any{}, wire.Stamp{At: int64(model.Now()), N: 1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	for epoch, key := range keysB {
		if _, err := before.Open(key); err == nil {
			t.Fatalf("b's key for epoch %d opens an envelope from before it joined", epoch)
		}
	}
	// And one it can open, to show the check means something.
	after, _ := a.Server.Link.TestSeal(id, "message.created", map[string]any{}, wire.Stamp{At: int64(model.Now()), N: 1}, 0)
	if _, err := after.Open(keysB[after.KeyEpoch]); err != nil {
		t.Fatalf("b cannot open the current epoch: %v", err)
	}
}

// Cases 19 and 20: a second stream_open for an existing stream is refused,
// and a control frame whose version is not the next one is refused with
// conflict: by a hub, and by the other server of a direct link.
func TestControlFramesRefused(t *testing.T) {
	for _, hub := range []bool{true, false} {
		n := 2
		s := share(t, n, hub, nil)
		st, _ := s.link(0).TestStream(s.stream)
		a := s.link(0)
		conflicts := func() int64 { return a.StatsNow().Dropped["error:"+wire.EConflict] }
		was := conflicts()
		if !a.TestControl(s.stream, wire.FStreamOpen, wire.Control{Owner: "a.localhost", Servers: []string{"a.localhost"}, Version: 1}) {
			t.Fatal("the frame did not leave")
		}
		testkit.Eventually(t, "a second stream_open is refused", func() bool { return conflicts() == was+1 })
		a.TestControl(s.stream, wire.FServerLeft, wire.Control{Owner: "a.localhost", Server: "c.localhost", Reason: "removed",
			Servers: []string{"a.localhost", "b.localhost"}, Version: st.ServersVersion + 5})
		if hub {
			// A channel on a hub is the hub's: no server's control frame
			// changes its list, whatever version it names (ruling R56).
			notOwner := func() int64 { return a.StatsNow().Dropped["error:"+wire.ENotOwner] }
			testkit.Eventually(t, "a server's change to a hub's channel is refused", func() bool { return notOwner() >= 1 })
		} else {
			testkit.Eventually(t, "a version that is not the next is refused", func() bool { return conflicts() == was+2 })
		}
		// Nothing changed for b.
		cur, _ := s.link(1).TestStream(s.stream)
		if cur.ServersVersion != st.ServersVersion || cur.State != store.StreamActive {
			t.Fatalf("b's stream moved: %+v", cur)
		}
	}
}

// ------------------------------------------------- envelopes and authority

// Cases 21, 22, 23, 24 (first half), 25, 26, 28, 29, 51, 61 and 62a (second
// half), on a direct link and through a hub: what a malicious peer can sign
// and nobody accepts. Case 22 is the second half of the brief's definition
// of done: an event signed by the wrong server is rejected.
func TestWhoMaySayWhat(t *testing.T) {
	for _, hub := range []bool{false, true} {
		name := "direct"
		n := 2
		if hub {
			name, n = "hub", 3
		}
		t.Run(name, func(t *testing.T) {
			s := share(t, n, hub, nil)
			a, b := s.Servers[0], s.Servers[1]
			real := s.ann.Post(s.conv[0], "- [ ] a real message with a task")
			b.Owner.HasMessage(s.conv[1], "- [ ] a real message with a task")
			receivers := []int{0}
			if hub {
				receivers = []int{0, 2}
			}
			each := func(reason string, n int64) {
				for _, r := range receivers {
					s.rejected(r, reason, n)
				}
			}

			// 22: validly signed by b, authored by an account of a.
			forged, _ := message(s.ann.ID, "forged in ann's name")
			s.submit(1, "message.created", forged)
			each("forged_author", 1)
			// 23: a reaction, a tick and a form answer naming ann.
			s.submit(1, "reaction.added", map[string]any{"message_id": real["id"], "account_id": s.ann.ID, "emoji": "👍"})
			s.submit(1, "task.changed", map[string]any{"message_id": real["id"], "item_id": "t1", "checked": true, "account_id": s.ann.ID})
			s.submit(1, "form.answered", map[string]any{"message_id": real["id"], "form_id": model.NewID[string](), "account_id": s.ann.ID, "answer_box": ""})
			each("forged_author", 4)
			// A profile for an account b claims is its own but is a's.
			s.submit(1, "account.updated", map[string]any{"account": map[string]any{"id": s.ann.ID, "server": "b.localhost",
				"username": "ann", "display_name": "Not Ann", "version": 1 << 60}})
			each("forged_author", 5)
			// 24, 25, 26, 61: the owner's powers.
			s.submit(1, "message.removed", map[string]any{"message_id": real["id"], "removed_by_id": b.Owner.ID, "removed_at": model.Now()})
			s.submit(1, "conversation.updated", map[string]any{"name": "renamed by b", "topic": "b's topic"})
			s.submit(1, "stream.servers_changed", map[string]any{"version": 99, "servers": []string{"b.localhost"}, "joined": []string{},
				"left": []string{"a.localhost"}, "epoch": 99})
			s.submit(1, "messages.expired", map[string]any{"through_at": int64(model.Now()), "scope": "all"})
			s.submit(1, "stream.closed", map[string]any{})
			s.submit(1, "stream.limits_changed", map[string]any{"max_blob_bytes": 1})
			if hub {
				// A channel on a hub has no owner server. Removing a message
				// for everyone is a moderator's, and b's owner is not one;
				// an epoch is begun by the hub, by no server; the rest is
				// nobody's.
				each("not_moderator", 1)
				each("hub_channel", 1)
				each("not_owner", 4)
			} else {
				each("not_owner", 6)
			}
			// 62a: a form that is not a poll.
			withForm, _ := message(b.Owner.ID, "a form")
			withForm["message"].(map[string]any)["form"] = map[string]any{"id": model.NewID[string](), "action": map[string]any{"type": "collect"}, "fields": []any{}}
			s.submit(1, "message.created", withForm)
			each("form_not_a_poll", 1)
			// 29: a type nobody knows is held and changes nothing, and
			// later events still apply.
			// The schema has no such event, so it travels as the JSON it is
			// inside the Event message (wire.EncodeEvent): said here, since
			// every other event of these tests must travel typed.
			link.AllowUntypedForTest("future.thing")
			s.submit(1, "future.thing", map[string]any{"anything": true})
			fine, _ := message(b.Owner.ID, "b's own, and fine")
			s.submit(1, "message.created", fine)
			s.ann.HasMessage(s.conv[0], "b's own, and fine")

			// 21: a wrong signature. The time reference refuses to take
			// it.
			bad, _ := s.link(1).TestSealNext(s.stream, "message.created", fine, 0)
			bad.Sig[0] ^= 1
			if answer, err := s.link(1).TestSubmit(s.stream, bad); err != nil || answer.Code != wire.EBadSignature || link.Took(answer, bad) {
				t.Fatalf("a wrong signature: %+v %v", answer, err)
			}
			// 28: an origin other than the submitting server.
			other, _ := s.link(1).TestSealNext(s.stream, "message.created", fine, 0)
			other.Origin = "a.localhost"
			if answer, err := s.link(1).TestSubmit(s.stream, other); err != nil || answer.Code != wire.ENotInStream || link.Took(answer, other) {
				t.Fatalf("another server's origin: %+v %v", answer, err)
			}
			// 26: a key from a server that is not the owner changes nothing.
			if _, err := s.link(1).TestDirect(model.PeerID(s.Peers[1]), "a.localhost", "key", map[string]any{"stream_id": s.stream, "epoch": 50,
				"key": wire.Bytes(bytes.Repeat([]byte{7}, 32)), "version": 9}, model.Now(), nil); err != nil {
				t.Fatal(err)
			}
			s.dropped(0, "key_not_from_owner")
			// 51: typing and presence naming another server's account.
			typing, _ := s.link(1).TestSeal(s.stream, "typing.started", map[string]any{"account_id": s.ann.ID}, wire.Stamp{}, 0)
			s.link(1).TestEphemeral(s.stream, typing)
			presence, _ := s.link(1).TestSeal(s.stream, "presence.changed", map[string]any{"account_id": s.ann.ID, "presence": "offline"}, wire.Stamp{}, 0)
			s.link(1).TestEphemeral(s.stream, presence)
			testkit.Eventually(t, "forged typing and presence are dropped", func() bool {
				return s.link(0).StatsNow().Dropped["ephemeral:forged"] >= 2
			})
			if _, ok := s.link(0).PresenceOf(s.ann.ID); ok {
				t.Error("a holds presence for its own account from another server")
			}

			// Nothing of it shows anywhere, and nothing changed.
			s.settle("after all that")
			for i, ts := range s.Servers {
				absent(t, ts.Owner, s.conv[i], "forged in ann's name")
				absent(t, ts.Owner, s.conv[i], "a form")
				for _, m := range ts.Owner.Messages(s.conv[i]) {
					if m["id"] == real["id"] && (m["deleted_at"] != nil || len(arr(m["reactions"])) != 0) {
						t.Errorf("%s: the real message was touched: %v", testkit.ServerName(i), m)
					}
				}
			}
			conv := a.Owner.Must(200, "GET", "/api/v1/conversations/"+s.conv[0], nil)["conversation"].(map[string]any)
			if conv["name"] != "launch" || len(conv["shared"].(map[string]any)["servers"].([]any)) != n {
				t.Errorf("the channel changed: %v", conv)
			}
			st, _ := s.link(0).TestStream(s.stream)
			if st.State != store.StreamActive || st.KeyEpoch == 99 || len(s.link(0).TestKeys(s.stream)) > int(st.KeyEpoch) {
				t.Errorf("the stream changed: %+v", st)
			}
			if acc, _ := a.Server.Store.Accounts().Get(ctx, s.ann.ID); acc.DisplayName == "Not Ann" {
				t.Error("a forged profile was applied")
			}
			// The audit log names both servers of a forgery.
			audit := a.Owner.Must(200, "GET", "/api/v1/admin/audit?action=link.event_rejected", nil)
			if !strings.Contains(fmt.Sprint(audit), "forged_author") || !strings.Contains(fmt.Sprint(audit), "b.localhost") {
				t.Errorf("the audit log does not report the forgery: %v", audit)
			}

			// 24, second half: from the owner it removes the message
			// everywhere.
			mine := b.Owner.Post(s.conv[1], "b's message, removed by the owner server")
			s.ann.HasMessage(s.conv[0], "b's message, removed by the owner server")
			a.Owner.Must(200, "DELETE", "/api/v1/messages/"+mine["id"].(string), nil)
			for i, ts := range s.Servers {
				testkit.Eventually(t, "the message is removed on "+testkit.ServerName(i), func() bool {
					for _, m := range ts.Owner.Messages(s.conv[i]) {
						if m["id"] == mine["id"] {
							return m["deleted_at"] != nil && m["text"] == nil
						}
					}
					return false
				})
			}
		})
	}
}

// The cases of order, unsent messages, duplicates and catching up are in
// order_test.go.

// ------------------------------------------------------------------- keys

// Case 38: removing a server starts a new epoch; the removed server is
// delivered nothing further and cannot decrypt a later envelope. Case 39:
// an envelope under the old epoch that is submitted after the change is
// refused by the time reference, and its origin's resubmission under the new
// epoch is taken once. Case 40: a server that asks for a key it was in the list
// for gets it; a server that was never in the stream gets an error. Case
// 65: `churr-hub remove` disconnects the server, a new roster reaches the
// others, and the owners of its streams remove it.
func TestEpochsAndRemoval(t *testing.T) {
	s := share(t, 3, true, nil)
	a, b, c := s.Servers[0], s.Servers[1], s.Servers[2]
	s.settle("all three")
	before, _ := s.link(0).TestStream(s.stream)
	// 40: b may ask the hub, which made every key, for the current epoch's;
	// a server never in the stream (b, for a stream of a's it was not
	// offered) may not, nor b for an epoch from before it joined.
	if key, err := s.link(1).TestAskHubKey(s.stream, before.KeyEpoch); err != nil || len(key) != 32 {
		t.Fatalf("b was refused a key it is entitled to: %v", err)
	}
	other := a.Owner.Channel("other", false)
	only, _ := s.Share(0, other, "other", nil, 2)
	if _, err := s.link(1).TestAskHubKey(model.StreamID(only), 1, model.PeerID(s.Peers[1])); wire.AsError(err).Code != wire.EForbidden {
		t.Fatalf("a server never in the stream asked for its key: %v", err)
	}
	if _, err := s.link(1).TestAskHubKey(s.stream, 1); wire.AsError(err).Code != wire.EForbidden {
		t.Fatalf("b asked for an epoch from before it joined: %v", err)
	}

	// 39 needs an envelope b made before the change and submitted after.
	late, _ := message(b.Owner.ID, "made under the old epoch")
	stale, _ := s.link(1).TestSealNext(s.stream, "message.created", late, 0)

	// 38: a removes c.
	cID := s.ServerID(0, 2)
	delivered := c.Server.Link.StatsNow().Delivered[s.stream]
	out := a.Owner.Must(200, "DELETE", "/api/v1/admin/streams/"+string(s.stream)+"/servers/"+cID, nil)["stream"].(map[string]any)
	if out["key_epoch"].(float64) <= float64(before.KeyEpoch) {
		t.Fatalf("no new epoch: %v", out)
	}
	testkit.Eventually(t, "c learns it was removed", func() bool {
		st := s.StreamOn(2, string(s.stream))
		return st != nil && st["state"] == "removed"
	})
	testkit.Eventually(t, "b is on the new epoch", func() bool {
		st, _ := s.link(1).TestStream(s.stream)
		return st.KeyEpoch == int64(out["key_epoch"].(float64))
	})
	// The hub began the new epoch when it signed the record that took c
	// out: it refuses the old.
	if answer, err := s.link(1).TestSubmit(s.stream, stale); err != nil || answer.Code != wire.EStaleEpoch || link.Took(answer, stale) {
		t.Fatalf("an envelope under an epoch that has ended: %+v %v", answer, err)
	}
	absent(t, s.ann, s.conv[0], "made under the old epoch")
	// Its origin encrypts the same event again under the new epoch, with
	// the stamp it had, and that is taken, once.
	again, err := s.link(1).TestSeal(s.stream, "message.created", late, stale.Stamp(), 0)
	if err != nil || again.KeyEpoch <= stale.KeyEpoch {
		t.Fatalf("sealing again: epoch %d after %d: %v", again.KeyEpoch, stale.KeyEpoch, err)
	}
	for i := 0; i < 2; i++ {
		if answer, err := s.link(1).TestSubmit(s.stream, again); err != nil || !link.Took(answer, again) {
			t.Fatalf("the same event under the new epoch: %+v %v", answer, err)
		}
	}
	s.ann.HasMessage(s.conv[0], "made under the old epoch")
	s.ann.Post(s.conv[0], "after c was removed")
	b.Owner.HasMessage(s.conv[1], "after c was removed")
	if n := len(s.Hub.Hub.TestLog(string(s.stream))); n == 0 {
		t.Fatal("the hub's log is empty")
	}
	logged := 0
	for _, env := range s.Hub.Hub.TestLog(string(s.stream)) {
		if env.OriginEventID == again.OriginEventID {
			logged++
		}
	}
	if n := countText(s.ann, s.conv[0], "made under the old epoch"); n != 1 || logged != 1 {
		t.Fatalf("a holds the resubmitted message %d times and the hub's log %d times", n, logged)
	}
	// c was delivered nothing further, kept its copy as an archived
	// channel, and holds no key that opens what came after.
	time.Sleep(200 * time.Millisecond)
	if now := c.Server.Link.StatsNow().Delivered[s.stream]; now > delivered+2 {
		t.Fatalf("c was delivered %d envelopes after its removal", now-delivered)
	}
	absent(t, c.Owner, s.conv[2], "after c was removed")
	c.Owner.HasMessage(s.conv[2], "all three")
	conv := c.Owner.Must(200, "GET", "/api/v1/conversations/"+s.conv[2], nil)["conversation"].(map[string]any)
	if conv["archived_at"] == nil || conv["shared"] != nil {
		t.Fatalf("c's copy should be an archived local channel: %v", conv)
	}
	later, _ := s.link(0).TestSeal(s.stream, "message.created", late, wire.Stamp{At: int64(model.Now()), N: 1}, 0)
	for epoch, key := range c.Server.Link.TestKeys(s.stream) {
		if _, err := later.Open(key); err == nil {
			t.Fatalf("c's key for epoch %d opens an envelope from after its removal", epoch)
		}
	}
	// The others show c's accounts as having left, and keep its messages.
	members := fmt.Sprint(a.Owner.Must(200, "GET", "/api/v1/conversations/"+s.conv[0]+"/members", nil))
	if strings.Contains(members, string(c.Owner.ID)) {
		t.Fatal("c's owner is still a member on a")
	}

	// 65: the hub's operator removes b.
	if err := s.Hub.Hub.Remove("b.localhost"); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "b is disconnected and a removes it from its stream", func() bool {
		if s.link(1).TestConnected(model.PeerID(s.Peers[1])) {
			return false
		}
		for _, sv := range s.StreamOn(0, string(s.stream))["servers"].([]any) {
			if m := sv.(map[string]any); m["name"] == "b.localhost" && m["state"] != "removed" {
				return false
			}
		}
		peer := a.Owner.Must(200, "GET", "/api/v1/admin/peers/"+s.Peers[0], nil)["peer"].(map[string]any)
		return !strings.Contains(fmt.Sprint(peer["servers"]), "b.localhost")
	})
}

// Case 42: a direct message with a clock six minutes out, or one replayed,
// is dropped. (After ten minutes the replay is forgotten, and the clock
// check is what stops it: the two windows overlap.)
func TestDirectMessageReplay(t *testing.T) {
	s := share(t, 2, false, nil)
	peer := model.PeerID(s.Peers[1])
	fields := map[string]any{"stream_id": s.stream, "epoch": 1}
	if _, err := s.link(1).TestDirect(peer, "a.localhost", "key_request", fields, model.Now().Add(-6*time.Minute), nil); err != nil {
		t.Fatal(err)
	}
	s.dropped(0, "direct:clock")
	if _, err := s.link(1).TestDirect(peer, "a.localhost", "key_request", fields, model.Now().Add(6*time.Minute), nil); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "a clock ahead is dropped too", func() bool { return s.link(0).StatsNow().Dropped["direct:clock"] >= 2 })
	f, err := s.link(1).TestDirect(peer, "a.localhost", "key_request", fields, model.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.link(1).TestDirect(peer, "", "", nil, 0, &f)
	s.dropped(0, "direct:replayed")
}

// ------------------------------------------------ people, files, presence

// Case 43: a remote member appears as name@server with the same account
// identifier as at home, and a profile change reaches the other server
// once however many streams they share.
func TestRemoteAccounts(t *testing.T) {
	s := share(t, 2, false, nil)
	a, b := s.Servers[0], s.Servers[1]
	second := a.Owner.Channel("second", false, s.ann)
	s.Share(0, second, "second", nil, 1)
	acc := b.Owner.Must(200, "GET", "/api/v1/accounts/"+string(s.ann.ID), nil)["account"].(map[string]any)
	if acc["id"] != string(s.ann.ID) || acc["username"] != "ann" || acc["server_name"] != "a.localhost" || acc["role"] != "remote" {
		t.Fatalf("ann on b: %v", acc)
	}
	// A shadow account cannot sign in or be given powers there.
	stored, _ := b.Server.Store.Accounts().Get(ctx, s.ann.ID)
	if stored.Active() || stored.Local() {
		t.Fatalf("a shadow account is not a local one: %+v", stored)
	}
	s.ann.Must(200, "PATCH", "/api/v1/me", map[string]any{"display_name": "Ann Example", "title": "Launch lead"})
	testkit.Eventually(t, "the new profile reaches b", func() bool {
		got, _ := b.Server.Store.Accounts().Get(ctx, s.ann.ID)
		return got.DisplayName == "Ann Example" && got.Title == "Launch lead"
	})
	// Two streams carried it; it was applied once, and the second copy
	// was simply not newer.
	s.settle("after the profile")
	if r := s.link(1).StatsNow().Rejected; len(r) != 0 {
		t.Fatalf("the second copy of a profile is ignored, not rejected: %v", r)
	}
	// A new member of a joins the channel there and appears on b.
	bob := a.Member("bob")
	bob.Must(200, "POST", "/api/v1/conversations/"+s.conv[0]+"/join", nil)
	testkit.Eventually(t, "bob is a member on b", func() bool {
		_, found, _ := b.Server.Store.Members().Get(ctx, model.ConversationID(s.conv[1]), bob.ID)
		return found
	})
	bob.Must(204, "POST", "/api/v1/conversations/"+s.conv[0]+"/leave", nil)
	testkit.Eventually(t, "bob has left on b", func() bool {
		_, found, _ := b.Server.Store.Members().Get(ctx, model.ConversationID(s.conv[1]), bob.ID)
		return !found
	})
}

// Case 44: a file posted on A arrives on B in chunks, matches its hash, and
// is served by B with nothing fetched from A when a member of B opens it.
// Case 45: a file that does not match its hash is discarded. Case 46: a
// file over max_blob_bytes is refused on the sending server and never
// fetched by a receiver.
func TestFiles(t *testing.T) {
	s := share(t, 3, true, map[string]any{"max_blob_bytes": 700000})
	b := s.Servers[1]
	content := bytes.Repeat([]byte("churr file content, more than two chunks of it. "), 12000) // about 576 kB
	file := s.ann.Upload(s.conv[0], "notes.bin", content)
	s.ann.Must(201, "POST", "/api/v1/conversations/"+s.conv[0]+"/messages", map[string]any{"text": "with a file", "file_ids": []any{file["id"]}})
	var url string
	testkit.Eventually(t, "the file arrives on b", func() bool {
		for _, m := range b.Owner.Messages(s.conv[1]) {
			if m["text"] != "with a file" {
				continue
			}
			f := arr(m["files"])[0].(map[string]any)
			if f["id"] != file["id"] {
				t.Fatalf("the file changed its id: %v", f)
			}
			url, _ = f["url"].(string)
			return url != "" && f["state"] == nil
		}
		return false
	})
	// a goes away; b serves its own copy.
	s.link(0).Stop()
	status, _, got := b.Owner.Get(url, nil)
	if status != 200 || !bytes.Equal(got, content) {
		t.Fatalf("b served %d bytes with status %d, want %d", len(got), status, len(content))
	}
	sum := sha256.Sum256(content)
	row, _ := b.Server.Store.Files().Get(ctx, model.FileID(file["id"].(string)))
	if !bytes.Equal(row.SHA256, sum[:]) || row.BlobKey == "" {
		t.Fatalf("b's row: %+v", row)
	}
	// The hub carried the chunks and kept none of them.
	if bytes.Contains(readDir(t, s.Hub.Dir), []byte("churr file content")) {
		t.Fatal("the hub's storage holds file content")
	}
}

func TestFilesThatMustNotArrive(t *testing.T) {
	s := share(t, 2, false, map[string]any{"max_blob_bytes": 1000})
	b := s.Servers[1]
	// 46: over the stream's limit, refused where it is posted.
	big := s.ann.Upload(s.conv[0], "big.bin", bytes.Repeat([]byte("x"), 5000))
	s.ann.Refused(413, model.CodeTooLarge, "POST", "/api/v1/conversations/"+s.conv[0]+"/messages", map[string]any{"text": "too big", "file_ids": []any{big["id"]}})
	wireFile := func(id any, size int64, hash [32]byte) map[string]any {
		return map[string]any{"id": id, "name": "f.bin", "size": size, "media_type": "application/octet-stream", "kind": "file", "sha256": wire.Bytes(hash[:])}
	}
	// A sender that ignores the limit: the receiver never fetches.
	over, _ := message(s.ann.ID, "over the limit anyway")
	over["message"].(map[string]any)["files"] = []any{wireFile(big["id"], 5000, sha256.Sum256(bytes.Repeat([]byte("x"), 5000)))}
	s.submit(0, "message.created", over)
	// 45: the bytes that come do not match the hash in the signed event,
	// which is all a receiver can know of a file changed on the way.
	loose := s.ann.Upload(s.conv[0], "loose.txt", []byte("small enough"))
	changed, _ := message(s.ann.ID, "changed on the way")
	changed["message"].(map[string]any)["files"] = []any{wireFile(loose["id"], 12, sha256.Sum256([]byte("small enougH")))}
	s.submit(0, "message.created", changed)
	state := func(text string) string {
		for _, m := range b.Owner.Messages(s.conv[1]) {
			if m["text"] == text && len(arr(m["files"])) == 1 {
				st, _ := arr(m["files"])[0].(map[string]any)["state"].(string)
				return st
			}
		}
		return "?"
	}
	testkit.Eventually(t, "the oversized file is marked too large", func() bool { return state("over the limit anyway") == "too_large" })
	s.dropped(1, "blob_mismatch")
	if got := state("changed on the way"); got != "arriving" {
		t.Fatalf("a file that failed its hash is in state %q", got)
	}
	row, _ := b.Server.Store.Files().Get(ctx, model.FileID(loose["id"].(string)))
	if row.BlobKey != "" {
		t.Fatal("a file that failed its hash was stored")
	}
	if row, _ := b.Server.Store.Files().Get(ctx, model.FileID(big["id"].(string))); row.BlobKey != "" || row.State != "too_large" {
		t.Fatalf("a file over the limit was fetched: %+v", row)
	}
}

// readDir is every byte a hub keeps of the link in its data directory: its
// database, without its key files and without its holder's database, which
// is the hub's copy of the channels it owns (readHeld).
func readDir(t *testing.T, dir string) []byte {
	t.Helper()
	return readTree(t, dir, filepath.Join(dir, "holder"))
}

// readHeld is every byte of a hub's holder: the channels it owns, whole.
func readHeld(t *testing.T, dir string) []byte {
	t.Helper()
	return readTree(t, filepath.Join(dir, "holder"), "")
}

func readTree(t *testing.T, dir, skip string) []byte {
	t.Helper()
	var all []byte
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() && skip != "" && path == skip {
			return filepath.SkipDir
		}
		if err != nil || info.IsDir() || strings.Contains(path, string(filepath.Separator)+"keys"+string(filepath.Separator)) {
			return err
		}
		b, err := os.ReadFile(path)
		all = append(all, b...)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return all
}

// Case 63: the hub's own storage of the link (its log, the channels'
// records and keys), searched byte for byte, contains no message text, file
// content or username: an envelope is kept as it travels, sealed. The hub
// owns the channel and reads it, and keeps it whole in its holder's
// database, which holds every message. Case 66: the hub's log
// holds nothing older than 30 days after the janitor runs. Case 67: the
// status page names no server and no stream. Case 64: a server over the
// hub's submit limit gets rate_limited with retry_after_seconds, and an
// oversized envelope gets too_large.
func TestWhatTheHubStores(t *testing.T) {
	s := share(t, 3, true, nil)
	b := s.Servers[1]
	s.ann.Post(s.conv[0], "the quarterly numbers are XYZZY-PLUGH")
	// A member whose name would be found if it were anywhere: he joins on
	// a, so his profile crosses the hub, and posts.
	zeb := s.Servers[0].Member("zebediah")
	zeb.Must(200, "PATCH", "/api/v1/me", map[string]any{"display_name": "Zeb Example"})
	zeb.Must(200, "POST", "/api/v1/conversations/"+s.conv[0]+"/join", nil)
	zeb.Post(s.conv[0], "zeb says hello")
	b.Owner.HasMessage(s.conv[1], "zeb says hello")
	b.Owner.Post(s.conv[1], "replying with FROBOZZ-MAGIC")
	s.settle("done-with-secrets")
	s.ann.Must(204, "POST", "/api/v1/conversations/"+s.conv[0]+"/typing", map[string]any{})
	list, err := s.Hub.Hub.Streams()
	if err != nil || len(list) != 1 || list[0].LogBytes == 0 || list[0].Cursor == 0 || list[0].HomeHub != "hub.localhost" {
		t.Fatalf("the hub carries nothing? %+v %v", list, err)
	}
	stored := readDir(t, s.Hub.Dir)
	if len(stored) < 1000 {
		t.Fatalf("the hub stored only %d bytes", len(stored))
	}
	// The channel's name is not in this list: a hub records the name,
	// description, listing mode, servers and moderators of a channel it owns
	// (ruling R56), and its operator can read them. Nothing else of the
	// channel is there: no message, no member who is not a moderator, no
	// event type.
	for _, secretText := range []string{"XYZZY-PLUGH", "FROBOZZ-MAGIC", "done-with-secrets", "zebediah", "Zeb Example", "message.created", "author_id"} {
		if bytes.Contains(stored, []byte(secretText)) {
			t.Errorf("the hub's storage contains %q", secretText)
		}
	}
	// What it does hold is what it is said to see: server names and the
	// stream's identifier.
	if !bytes.Contains(stored, []byte("a.localhost")) || !bytes.Contains(stored, []byte(s.stream)) {
		t.Error("the hub's storage lacks what it is supposed to hold")
	}
	// And its holder holds the channel whole: every message, and who said
	// it.
	held := readHeld(t, s.Hub.Dir)
	for _, text := range []string{"XYZZY-PLUGH", "FROBOZZ-MAGIC", "zebediah", "Zeb Example"} {
		if !bytes.Contains(held, []byte(text)) {
			t.Errorf("the hub's holder does not hold %q", text)
		}
	}

	// 67.
	page := httptest.NewRecorder()
	s.Hub.Hub.StatusHandler().ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
	body := strings.ReplaceAll(page.Body.String(), "hub.localhost", "the hub itself")
	if page.Code != 200 || !strings.Contains(body, "Servers joined: 3") || !strings.Contains(body, "Streams open: 1") || strings.Contains(body, "<script") {
		t.Fatalf("the status page: %d %s", page.Code, body)
	}
	for _, name := range []string{"a.localhost", "b.localhost", "c.localhost", string(s.stream)} {
		if strings.Contains(body, name) {
			t.Errorf("the status page names %s", name)
		}
	}

	// 64.
	if err := s.Hub.Hub.SetLimits("b.localhost", 2, 0, 0); err != nil {
		t.Fatal(err)
	}
	s.Hub.Hub.Reconcile()
	limited := false
	for i := 0; i < 6 && !limited; i++ {
		fields, _ := message(b.Owner.ID, fmt.Sprintf("flood %d", i))
		env, _ := s.link(1).TestSealNext(s.stream, "message.created", fields, 0)
		answer, err := s.link(1).TestSubmit(s.stream, env)
		if err != nil {
			t.Fatal(err)
		}
		if answer.Code != "" {
			if answer.Code != wire.ERateLimited || answer.RetryAfterSeconds <= 0 || link.Took(answer, env) {
				t.Fatalf("over the limit: %+v", answer)
			}
			limited = true
		}
	}
	if !limited {
		t.Fatal("six envelopes passed a limit of two a minute")
	}
	link.AllowUntypedForTest("future.thing") // a kind no schema has, sealed on purpose
	huge, _ := s.link(1).TestSealNext(s.stream, "future.thing", map[string]any{}, 0)
	huge.Ciphertext = append(huge.Ciphertext, make([]byte, wire.MaxEnvelopeBytes)...)
	if answer, err := s.link(1).TestSubmit(s.stream, huge); err != nil || answer.Code != wire.ETooLarge {
		t.Fatalf("an oversized envelope: %+v %v", answer, err)
	}

	// 66.
	if n := s.Hub.Hub.Sweep(); n != 0 {
		t.Fatalf("the janitor removed %d fresh entries", n)
	}
	s.Hub.Skew(31 * 24 * time.Hour)
	if n := s.Hub.Hub.Sweep(); n == 0 {
		t.Fatal("the janitor removed nothing after 31 days")
	}
	list, _ = s.Hub.Hub.Streams()
	if list[0].LogBytes != 0 {
		t.Fatalf("the log still holds %d bytes older than 30 days", list[0].LogBytes)
	}
}

// --------------------------------------------- leaving, closing, purging

// Case 58: after a server leaves, its copy is an archived local channel,
// the others show its accounts as having left, and its messages remain.
// Case 59: after stream_close, every server detaches and keeps its copy.
// Case 60: message.purged from the author's home server erases the
// message's content from the database and search index of every other
// server. Case 62: thread-to-channel, making the channel private and
// locking are each refused in a shared channel, on the owner server and on
// the others. Case 62a, first half: a form that is not a poll is refused at
// posting.
func TestLeavingClosingAndWhatASharedChannelCannotDo(t *testing.T) {
	s := share(t, 3, true, nil)
	a, b, c := s.Servers[0], s.Servers[1], s.Servers[2]
	parent := s.ann.Post(s.conv[0], "a thread starts here")
	s.ann.Must(201, "POST", "/api/v1/conversations/"+s.conv[0]+"/messages", map[string]any{"text": "a reply", "parent_id": parent["id"]})
	fromB := b.Owner.Post(s.conv[1], "b was here")
	s.settle("everyone level")

	// 62 and 62a.
	for i, ts := range s.Servers {
		conv := "/api/v1/conversations/" + s.conv[i]
		if st, out := ts.Owner.Do("PATCH", conv, map[string]any{"private": true}); st < 400 {
			t.Errorf("%s made a shared channel private: %v", testkit.ServerName(i), out)
		}
		if st, out := ts.Owner.Do("POST", conv+"/lock", nil); st < 400 {
			t.Errorf("%s locked a shared channel: %v", testkit.ServerName(i), out)
		}
		if st, out := ts.Owner.Do("POST", "/api/v1/messages/"+parent["id"].(string)+"/move-to-channel", map[string]any{"name": "moved"}); st < 400 {
			t.Errorf("%s moved a thread out of a shared channel: %v", testkit.ServerName(i), out)
		}
		form := map[string]any{"text": "sign up", "form": map[string]any{
			"fields": []any{map[string]any{"id": "f1", "type": "text", "label": "Name"}}, "action": map[string]any{"type": "collect"}}}
		if st, out := ts.Owner.Do("POST", conv+"/messages", form); st < 400 {
			t.Errorf("%s posted a form that is not a poll in a shared channel: %v", testkit.ServerName(i), out)
		}
		got := ts.Owner.Must(200, "GET", conv, nil)["conversation"].(map[string]any)
		if got["private"] == true || got["locked_at"] != nil || got["shared"] == nil {
			t.Errorf("%s: the channel changed: %v", testkit.ServerName(i), got)
		}
	}

	// 60: ann deletes her own message at once, which leaves no trace at
	// home and asks the others to keep none either.
	doomed := s.ann.Post(s.conv[0], "ERASEME-QUUX is in this one")
	b.Owner.HasMessage(s.conv[1], "ERASEME-QUUX is in this one")
	c.Owner.HasMessage(s.conv[2], "ERASEME-QUUX is in this one")
	s.ann.Must(200, "DELETE", "/api/v1/messages/"+doomed["id"].(string), nil)
	for i := 1; i < 3; i++ {
		ts := s.Servers[i]
		testkit.Eventually(t, "the content is erased on "+testkit.ServerName(i), func() bool {
			m, err := ts.Server.Store.Messages().Get(ctx, model.MessageID(doomed["id"].(string)))
			if err != nil {
				return true
			}
			versions, _ := ts.Server.Store.Messages().Versions(ctx, m.ID)
			return m.Text == "" && m.SearchText == "" && len(versions) == 0
		})
		found := ts.Owner.Must(200, "GET", "/api/v1/search?q=ERASEME-QUUX", nil)
		if strings.Contains(fmt.Sprint(found), doomed["id"].(string)) {
			t.Errorf("%s still finds the purged message: %v", testkit.ServerName(i), found)
		}
	}

	// 58: b leaves.
	b.Owner.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/admin/streams/"+model.NewID[string](), nil)
	// (On a hub no server owns the channel, so any of them may leave, the
	// one that shared it included. The owner server of a direct-link stream
	// cannot: TestStreamAdministration.)
	b.Owner.Must(204, "DELETE", "/api/v1/admin/streams/"+string(s.stream), nil)
	left := b.Owner.Must(200, "GET", "/api/v1/conversations/"+s.conv[1], nil)["conversation"].(map[string]any)
	if left["archived_at"] == nil || left["shared"] != nil {
		t.Fatalf("b's copy should be an archived local channel: %v", left)
	}
	if st := s.StreamOn(1, string(s.stream)); st["state"] != "left" {
		t.Fatalf("b's stream: %v", st)
	}
	b.Owner.HasMessage(s.conv[1], "everyone level")
	for _, i := range []int{0, 2} {
		ts := s.Servers[i]
		testkit.Eventually(t, "b's accounts have left on "+testkit.ServerName(i), func() bool {
			_, found, _ := ts.Server.Store.Members().Get(ctx, model.ConversationID(s.conv[i]), b.Owner.ID)
			return !found
		})
		got := ts.Owner.HasMessage(s.conv[i], "b was here")
		if got["id"] != fromB["id"] {
			t.Fatalf("b's message did not remain on %s", testkit.ServerName(i))
		}
	}
	// What a posts now does not reach b.
	s.ann.Post(s.conv[0], "after b left")
	c.Owner.HasMessage(s.conv[2], "after b left")
	absent(t, b.Owner, s.conv[1], "after b left")

	// 59: a stops sharing.
	b.Owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/streams/"+string(s.stream)+"/close", nil)
	c.Owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/streams/"+string(s.stream)+"/close", nil)
	a.Owner.Must(204, "POST", "/api/v1/admin/streams/"+string(s.stream)+"/close", nil)
	for _, i := range []int{0, 2} {
		ts := s.Servers[i]
		testkit.Eventually(t, testkit.ServerName(i)+" detaches", func() bool {
			st := s.StreamOn(i, string(s.stream))
			return st != nil && st["state"] == "removed"
		})
		conv := ts.Owner.Must(200, "GET", "/api/v1/conversations/"+s.conv[i], nil)["conversation"].(map[string]any)
		if conv["shared"] != nil || conv["archived_at"] == nil {
			t.Fatalf("%s: %v", testkit.ServerName(i), conv)
		}
		ts.Owner.HasMessage(s.conv[i], "after b left")
	}
	// Nothing is left in flight, and the keys stay so the copy can be read.
	if n, _ := c.Server.Store.Streams().OutboxCount(ctx); n != 0 || len(c.Server.Link.TestKeys(s.stream)) == 0 {
		t.Fatalf("c: %d in the outbox, keys %v", n, c.Server.Link.TestKeys(s.stream))
	}
}

// Changing a shared channel's limit, handing it over, pausing a peer and
// removing one: the admin operations not covered above.
func TestStreamAdministration(t *testing.T) {
	defer link.SetDialBase(20 * time.Millisecond)()
	s := share(t, 2, false, nil)
	a, b := s.Servers[0], s.Servers[1]
	sid := string(s.stream)
	b.Owner.Refused(403, model.CodeForbidden, "PATCH", "/api/v1/admin/streams/"+sid, map[string]any{"max_blob_bytes": 5})
	a.Owner.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/admin/streams/"+sid, map[string]any{"max_blob_bytes": 0})
	a.Owner.Refused(409, model.CodeConflict, "DELETE", "/api/v1/admin/streams/"+sid, nil) // the owner server cannot leave
	a.Owner.Must(200, "PATCH", "/api/v1/admin/streams/"+sid, map[string]any{"max_blob_bytes": 4096})
	testkit.Eventually(t, "the new limit reaches b", func() bool { return s.StreamOn(1, sid)["max_blob_bytes"] == float64(4096) })
	// Sharing what cannot be shared.
	a.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/streams", map[string]any{"conversation_id": s.conv[0], "peer_id": s.Peers[0]})
	a.Owner.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/streams", map[string]any{"conversation_id": a.Owner.Channel("x", false), "peer_id": s.Peers[0], "history_days": 91})
	a.Owner.Refused(404, model.CodeNotFound, "POST", "/api/v1/admin/streams", map[string]any{"conversation_id": model.NewID[string](), "peer_id": s.Peers[0]})
	// A rename by the owner server reaches the others; one elsewhere is
	// that server's own business.
	a.Owner.Must(200, "PATCH", "/api/v1/conversations/"+s.conv[0], map[string]any{"topic": "Getting 2.0 out"})
	testkit.Eventually(t, "the topic reaches b", func() bool {
		return b.Owner.Must(200, "GET", "/api/v1/conversations/"+s.conv[1], nil)["conversation"].(map[string]any)["topic"] == "Getting 2.0 out"
	})
	b.Owner.Must(200, "PATCH", "/api/v1/conversations/"+s.conv[1], map[string]any{"topic": "b's own"})
	s.settle("after the topics")
	if got := a.Owner.Must(200, "GET", "/api/v1/conversations/"+s.conv[0], nil)["conversation"].(map[string]any)["topic"]; got != "Getting 2.0 out" {
		t.Fatalf("b changed the owner's topic: %v", got)
	}
	// Reactions and ticks cross both ways.
	task := s.ann.Post(s.conv[0], "- [ ] ship it")
	onB := b.Owner.HasMessage(s.conv[1], "- [ ] ship it")
	b.Owner.Must(200, "PUT", "/api/v1/messages/"+onB["id"].(string)+"/reactions/🎉", nil)
	testkit.Eventually(t, "b's reaction shows on a", func() bool {
		m := s.ann.Must(200, "GET", "/api/v1/messages/"+task["id"].(string), nil)["message"].(map[string]any)
		r := arr(m["reactions"])
		return len(r) == 1 && r[0].(map[string]any)["emoji"] == "🎉"
	})
	// Handing over: b becomes the owner and, on a direct link, the time
	// reference.
	bID := s.ServerID(0, 1)
	a.Owner.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/streams/"+sid+"/owner", map[string]any{"server_id": model.NewID[string]()})
	a.Owner.Must(200, "POST", "/api/v1/admin/streams/"+sid+"/owner", map[string]any{"server_id": bID})
	testkit.Eventually(t, "b owns the stream and is its time reference", func() bool {
		st := s.StreamOn(1, sid)
		return st["owner_server"] == "b.localhost" && st["reference"] == "self" && s.StreamOn(0, sid)["reference"] == "owner"
	})
	testkit.Eventually(t, "the new owner's epoch reaches a", func() bool {
		sa, _ := s.link(0).TestStream(s.stream)
		sb, _ := s.link(1).TestStream(s.stream)
		return sa.KeyEpoch == sb.KeyEpoch && sa.KeyEpoch >= 3
	})
	p1 := s.ann.Post(s.conv[0], "b keeps the log now")
	g1 := b.Owner.HasMessage(s.conv[1], "b keeps the log now")
	p2 := b.Owner.Post(s.conv[1], "and b's own")
	g2 := s.ann.HasMessage(s.conv[0], "and b's own")
	if g1["id"] != p1["id"] || g2["id"] != p2["id"] || g1["order"] != p1["order"] || g2["order"] != p2["order"] ||
		p1["unsent"] != true || p2["unsent"] == true {
		t.Fatalf("after the handover: %v %v, as posted %v %v", g1, g2, p1, p2)
	}
	// Removing a peer: the server that owns a stream on it must close or
	// hand it over first; the other simply leaves.
	b.Owner.Refused(409, model.CodeConflict, "DELETE", "/api/v1/admin/peers/"+s.Peers[1], nil)
	a.Owner.Must(204, "DELETE", "/api/v1/admin/peers/"+s.Peers[0], nil)
	testkit.Eventually(t, "both sides mark the peer removed", func() bool {
		pa := a.Owner.Must(200, "GET", "/api/v1/admin/peers/"+s.Peers[0], nil)["peer"].(map[string]any)
		pb := b.Owner.Must(200, "GET", "/api/v1/admin/peers/"+s.Peers[1], nil)["peer"].(map[string]any)
		return pa["state"] == "removed" && pb["state"] == "removed"
	})
	if st := s.StreamOn(0, sid); st["state"] != "left" {
		t.Fatalf("a's stream after removing the peer: %v", st)
	}
	// The pinned keys stay, and the server can be blocked.
	servers := a.Owner.Must(200, "GET", "/api/v1/admin/servers", nil)["servers"].([]any)
	if len(servers) != 1 || servers[0].(map[string]any)["name"] != "b.localhost" || servers[0].(map[string]any)["fingerprint"] != b.Server.Link.Fingerprint() {
		t.Fatalf("known servers: %v", servers)
	}
	a.Owner.Must(204, "PUT", "/api/v1/admin/servers/"+bID+"/block", nil)
	if got := a.Owner.Must(200, "GET", "/api/v1/admin/servers", nil)["servers"].([]any)[0].(map[string]any); got["blocked_at"] == nil {
		t.Fatalf("not blocked: %v", got)
	}
	a.Owner.Must(204, "DELETE", "/api/v1/admin/servers/"+bID+"/block", nil)
	a.Owner.Refused(404, model.CodeNotFound, "PUT", "/api/v1/admin/servers/"+model.NewID[string]()+"/block", nil)
}

// What this package does not prove. It is skipped with where it is proved,
// so the list of section 25 is whole.
func TestProvedElsewhere(t *testing.T) {
	for _, c := range []struct{ cases, why string }{
		{"52 (the calls package's half)", "a pass that is expired, reused or for another account is refused by the calls package, which issues and checks passes; TestCallsOverTheLink covers what the link carries"},
	} {
		t.Run("cases "+c.cases, func(t *testing.T) { t.Skip(c.why) })
	}
}

// submitTo makes server i submit an event of its own making in the stream
// behind one of its conversations.
func (s *shared) submitTo(i int, conv, typ string, fields map[string]any) {
	s.t.Helper()
	c, err := s.Servers[i].Server.Store.Conversations().Get(ctx, model.ConversationID(conv))
	if err != nil {
		s.t.Fatal(err)
	}
	env, err := s.link(i).TestSealNext(c.StreamID, typ, fields, 0)
	if err != nil {
		s.t.Fatal(err)
	}
	if answer, err := s.link(i).TestSubmit(c.StreamID, env); err != nil || !link.Took(answer, env) {
		s.t.Fatalf("the reference did not take a soundly stamped envelope: %+v %v", answer, err)
	}
}

// originateIn makes server i make an event of its own in the stream behind
// one of its conversations, as it makes any event: applied there at once,
// then sent. The error is the server's own refusal to make it.
func (s *shared) originateIn(i int, conv, typ string, fields map[string]any) error {
	s.t.Helper()
	c, err := s.Servers[i].Server.Store.Conversations().Get(ctx, model.ConversationID(conv))
	if err != nil {
		s.t.Fatal(err)
	}
	return s.link(i).TestOriginate(c.StreamID, typ, fields)
}

// TestKnockChannelNotShared: a knock channel (ruling R79) is this server's
// alone for now, and is refused a stream.
func TestKnockChannelNotShared(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a := l.Servers[0]
	ch := a.Owner.Must(201, "POST", "/api/v1/channels", map[string]any{"name": "council", "access": "knock"})["conversation"].(map[string]any)["id"].(string)
	a.Owner.Refused(409, model.CodeConflict, "POST", "/api/v1/admin/streams", map[string]any{"conversation_id": ch, "peer_id": l.Peers[0]})
	a.Owner.Must(200, "PATCH", "/api/v1/conversations/"+ch, map[string]any{"access": "public"})
	a.Owner.Must(201, "POST", "/api/v1/admin/streams", map[string]any{"conversation_id": ch, "peer_id": l.Peers[0]})
}
