package api_test

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// First contact (ruling R103; docs/FEATURES.md sections 14.4 and 20.2):
// nobody ends up in a conversation they did not accept.

// requestOf waits for the chat request of a kind on a stream.
func requestOf(s *testkit.Stream, kind string) map[string]any {
	f := s.ExpectWhere("chat_request.received", func(f map[string]any) bool { return obj(f, "request")["kind"] == kind })
	return obj(f, "request")
}

// nothingAbout fails if a frame about the conversation reaches the stream.
func nothingAbout(t *testing.T, s *testkit.Stream, conv string) {
	t.Helper()
	quiet(t, s, 1500*time.Millisecond, func(f map[string]any) bool {
		return f["conversation_id"] == conv || mentions(f, conv) && f["type"] != "chat_request.received" && f["type"] != "chat_request.removed"
	})
}

func TestGroupChatIsARequest(t *testing.T) {
	ts := requestServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	autoApprove(bob)
	catS, _ := cat.Events(nil)

	c := obj(ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID), string(cat.ID)}}), "conversation")
	id := c["id"].(string)
	if !hasID(arr(c, "member_ids"), string(bob.ID)) || hasID(arr(c, "member_ids"), string(cat.ID)) || !hasID(arr(c, "invited_ids"), string(cat.ID)) {
		t.Fatalf("bob, who approves automatically, is in; cat is invited: %v", c)
	}
	req := requestOf(catS, "group")
	if req["conversation_id"] != id || len(arr(req, "people")) != 2 || obj(req, "account")["id"] != string(ann.ID) {
		t.Fatalf("the group chat request: %v", req)
	}
	// Not a member: nothing to read, nothing delivered, and the chat goes on.
	cat.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+id, nil)
	ann.Post(id, "before cat")
	bob.Post(id, "hi ann")
	nothingAbout(t, catS, id)
	members := arr(ann.Must(200, "GET", "/api/v1/conversations/"+id+"/members", nil), "members")
	last := members[len(members)-1].(map[string]any)
	if len(members) != 3 || last["account_id"] != string(cat.ID) || last["invited"] != true {
		t.Fatalf("the members list shows cat invited, not yet joined: %v", members)
	}
	// Starting the same chat finds it, and asks nobody twice.
	if again := obj(ann.Must(200, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID), string(cat.ID)}}), "conversation"); again["id"] != id {
		t.Fatalf("the same chat: %v", again)
	}
	if n := len(requestsOf(cat)); n != 1 {
		t.Fatalf("one request: %d", n)
	}

	// Accepting: a member from now, reading from the join.
	time.Sleep(5 * time.Millisecond)
	open := obj(cat.Must(200, "POST", "/api/v1/me/chat-requests/"+req["id"].(string)+"/accept", nil), "conversation")
	if open["id"] != id || hasID(arr(open, "invited_ids"), string(cat.ID)) {
		t.Fatalf("accepted: %v", open)
	}
	for _, m := range arr(cat.Must(200, "GET", "/api/v1/conversations/"+id+"/messages", nil), "messages") {
		if m.(map[string]any)["text"] == "before cat" {
			t.Fatal("cat reads what was said before she joined")
		}
	}
	ann.Post(id, "welcome cat")
	if !hasText(cat, id, "welcome cat") {
		t.Fatal("cat does not read what is said after she joined")
	}
}

func hasText(c *testkit.Client, conv, text string) bool {
	for _, m := range arr(c.Must(200, "GET", "/api/v1/conversations/"+conv+"/messages", nil), "messages") {
		if m.(map[string]any)["text"] == text {
			return true
		}
	}
	return false
}

func TestGroupChatAddLaterAndDecline(t *testing.T) {
	ts := requestServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	autoApprove(bob)
	catS, _ := cat.Events(nil)
	c := obj(ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID)}}), "conversation")
	id := c["id"].(string)
	// A two-person chat that opened; adding cat later is a request too.
	got := arr(ann.Must(200, "POST", "/api/v1/conversations/"+id+"/members", map[string]any{"account_ids": []string{string(cat.ID)}}), "members")
	if len(got) != 1 || got[0].(map[string]any)["invited"] != true {
		t.Fatalf("returned as invited: %v", got)
	}
	req := requestOf(catS, "group")
	cat.Must(204, "POST", "/api/v1/me/chat-requests/"+req["id"].(string)+"/decline", nil)
	if hasID(arr(chatOf(ann, id), "invited_ids"), string(cat.ID)) {
		t.Fatal("a decline takes her off the invited list")
	}
	// A day's wait for ann, and the caps.
	ann.Refused(429, model.CodeRateLimited, "POST", "/api/v1/conversations/"+id+"/members", map[string]any{"account_ids": []string{string(cat.ID)}})
	ts.Server.Chat.AdvanceRequestClock(25 * time.Hour)
	ann.Must(200, "POST", "/api/v1/conversations/"+id+"/members", map[string]any{"account_ids": []string{string(cat.ID)}})
	// Removing someone invited withdraws it.
	ann.Must(204, "DELETE", "/api/v1/conversations/"+id+"/members/"+string(cat.ID), nil)
	if len(requestsOf(cat)) != 0 || hasID(arr(chatOf(ann, id), "invited_ids"), string(cat.ID)) {
		t.Fatal("withdrawn")
	}
}

func TestGroupChatCap(t *testing.T) {
	ts := requestServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	autoApprove(bob)
	asked := people(t, ts, "p", model.RequestsUnapprovedPerDay+1, false)
	for _, id := range asked[:model.RequestsUnapprovedPerDay] {
		ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID), string(id)}})
	}
	last := asked[model.RequestsUnapprovedPerDay]
	ann.Refused(429, model.CodeRateLimited, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID), string(last)}})
	// Someone who approves automatically is not a request, within the cap
	// of chats kept.
	auto := people(t, ts, "q", 1, true)
	ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID), string(auto[0])}})
}

// Someone who has blocked the one inviting is invited for ever, and the
// one inviting cannot tell it from someone who never answers.
func TestGroupChatBlockedIsInvitedForEver(t *testing.T) {
	ts := requestServer(t)
	ann, bob, cat, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("cat"), ts.Member("dan")
	autoApprove(bob)
	cat.Must(204, "PUT", "/api/v1/me/blocks/"+string(ann.ID), nil)
	catS, _ := cat.Events(nil)
	blocked := obj(ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID), string(cat.ID)}}), "conversation")
	plain := obj(ann.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID), string(dan.ID)}}), "conversation")
	for _, pair := range [][2]any{{blocked, cat.ID}, {plain, dan.ID}} {
		c := pair[0].(map[string]any)
		if !hasID(arr(c, "invited_ids"), string(pair[1].(model.AccountID))) || len(arr(c, "member_ids")) != 2 {
			t.Fatalf("shown the same: %v", c)
		}
	}
	nothingAbout(t, catS, blocked["id"].(string))
	if len(requestsOf(cat)) != 0 {
		t.Fatal("the blocker is shown the request")
	}
}

func TestChannelInvitation(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	bobS, _ := bob.Events(nil)
	pub := ann.Channel("lobby", false)
	priv := ann.Channel("vault", true)
	ann.AddOnly(pub, "", bob)
	ann.AddOnly(priv, "", bob)
	r1, r2 := requestOf(bobS, "channel"), requestOf(bobS, "channel")
	if obj(r1, "channel")["name"] != "lobby" || r1["level"] != "member" || obj(r2, "channel")["access"] != "private" {
		t.Fatalf("the invitations: %v %v", r1, r2)
	}
	if hasID(arr(chatOf(ann, priv), "member_ids"), string(bob.ID)) || !hasID(arr(chatOf(ann, priv), "invited_ids"), string(bob.ID)) {
		t.Fatal("bob is invited, not in")
	}
	ann.Post(priv, "secret")
	ann.Post(pub, "hello")
	// Preview: the public channel reads, the private one does not.
	if !hasText(bob, pub, "hello") {
		t.Fatal("a public channel he is invited to can be previewed")
	}
	bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv+"/messages", nil)
	nothingAbout(t, bobS, priv)

	bob.Must(200, "POST", "/api/v1/me/chat-requests/"+r2["id"].(string)+"/accept", nil)
	if hasText(bob, priv, "secret") {
		t.Fatal("he reads the private channel from his join only")
	}
	// Joining the public one himself answers its invitation.
	bob.Must(200, "POST", "/api/v1/conversations/"+pub+"/join", nil)
	if len(requestsOf(bob)) != 0 {
		t.Fatal("joining answered the invitation")
	}
}

func TestGroupOffers(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	ann, bob := ts.Member("ann"), ts.Member("bob")
	annS, _ := ann.Events(nil)
	priv := owner.Channel("ops", true)
	group := obj(owner.Must(201, "POST", "/api/v1/admin/groups", map[string]any{"handle": "crew", "name": "Crew"}), "group")["id"].(string)
	owner.Must(204, "PUT", "/api/v1/admin/groups/"+group+"/members/"+string(ann.ID), nil)
	owner.Must(200, "PUT", "/api/v1/conversations/"+priv+"/groups/"+group, map[string]any{"level": "member"})
	f := annS.Expect("channel_offer.added")
	if o := obj(f, "offer"); o["conversation_id"] != priv || o["source"] != "group" || obj(o, "channel")["name"] != "ops" {
		t.Fatalf("channel_offer.added: %v", f)
	}
	offers := arr(ann.Must(200, "GET", "/api/v1/me/channel-offers", nil), "offers")
	if len(offers) != 1 {
		t.Fatalf("offers: %v", offers)
	}
	ann.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv+"/messages", nil)
	owner.Post(priv, "before ann")
	nothingAbout(t, annS, priv)

	// Decline, then join later from Browse, though it is private.
	ann.Must(204, "POST", "/api/v1/me/channel-offers/"+priv+"/decline", nil)
	if len(arr(ann.Must(200, "GET", "/api/v1/me/channel-offers", nil), "offers")) != 0 {
		t.Fatal("declined offers leave the left panel")
	}
	listed := false
	for _, c := range arr(ann.Must(200, "GET", "/api/v1/channels", nil), "conversations") {
		listed = listed || c.(map[string]any)["id"] == priv
	}
	if !listed {
		t.Fatal("Browse shows the channel her group has access to")
	}
	for _, c := range arr(bob.Must(200, "GET", "/api/v1/channels", nil), "conversations") {
		if c.(map[string]any)["id"] == priv {
			t.Fatal("Browse shows a private channel to someone with no access")
		}
	}
	ann.Must(200, "POST", "/api/v1/conversations/"+priv+"/join", nil)
	if hasText(ann, priv, "before ann") {
		t.Fatal("a private channel reads from the join")
	}
	// She may leave it like anyone, and the group's removal takes her out.
	ann.Must(204, "POST", "/api/v1/conversations/"+priv+"/leave", nil)
	ann.Must(200, "POST", "/api/v1/conversations/"+priv+"/join", nil)
	owner.Must(204, "DELETE", "/api/v1/conversations/"+priv+"/groups/"+group, nil)
	ann.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv, nil)
	bob.Refused(404, model.CodeNotFound, "POST", "/api/v1/me/channel-offers/"+priv+"/accept", nil)
}

// The review's H2: @everyone on a channel makes nobody a member.
func TestEveryoneOffersAndCanBeLeft(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	pub := ann.Channel("town", false)
	everyone := ""
	for _, g := range arr(ann.Must(200, "GET", "/api/v1/groups", nil), "groups") {
		if g.(map[string]any)["handle"] == "everyone" {
			everyone = g.(map[string]any)["id"].(string)
		}
	}
	ann.Must(200, "PUT", "/api/v1/conversations/"+pub+"/groups/"+everyone, map[string]any{"level": "member"})
	bob := ts.Member("bob") // signs up afterwards: offered, not placed
	if got := chatOf(ann, pub); got["member_count"] != float64(1) {
		t.Fatalf("nobody was placed: %v", got)
	}
	if len(arr(bob.Must(200, "GET", "/api/v1/me/channel-offers", nil), "offers")) != 1 {
		t.Fatal("bob is offered it")
	}
	bob.Must(200, "POST", "/api/v1/me/channel-offers/"+pub+"/accept", nil)
	bob.Must(204, "POST", "/api/v1/conversations/"+pub+"/leave", nil)
}

func TestDefaultChannels(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	general := ""
	for _, c := range arr(owner.Must(200, "GET", "/api/v1/channels", nil), "conversations") {
		if c.(map[string]any)["name"] == "general" {
			general = c.(map[string]any)["id"].(string)
		}
	}
	set := obj(owner.Must(200, "GET", "/api/v1/admin/settings", nil), "settings")
	if d := arr(set, "channels.default"); len(d) != 1 || d[0] != general {
		t.Fatalf("a fresh instance's default channel is #general: %v", set["channels.default"])
	}
	ann := ts.Member("ann")
	if chatOf(ann, general)["membership"] == nil {
		t.Fatal("a new account joins #general at sign-up")
	}
	ann.Must(204, "POST", "/api/v1/conversations/"+general+"/leave", nil)

	news := owner.Channel("news", false)
	annS, _ := ann.Events(nil)
	owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"channels.default": []string{general, news}})
	if o := obj(annS.Expect("channel_offer.added"), "offer"); o["conversation_id"] != news || o["source"] != "default" {
		t.Fatalf("offered the new default channel: %v", o)
	}
	if chatOf(ann, news)["membership"] != nil {
		t.Fatal("not placed in a default channel added later")
	}
	bob := ts.Member("bob")
	if chatOf(bob, news)["membership"] == nil || chatOf(bob, general)["membership"] == nil {
		t.Fatal("a new account joins every default channel")
	}
	owner.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/admin/settings", map[string]any{"channels.default": []string{string(ann.ID)}})
	owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"channels.default": []string{general}})
	if len(arr(ann.Must(200, "GET", "/api/v1/me/channel-offers", nil), "offers")) != 0 {
		t.Fatal("no longer a default channel: no longer offered")
	}
}

func TestOwnerOverrideIsAnInvitation(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	ann, bob := ts.Member("ann"), ts.Member("bob")
	priv := ann.Channel("vault", true)
	bobS, _ := bob.Events(nil)
	m := obj(owner.Must(200, "PUT", "/api/v1/admin/channels/"+priv+"/members/"+string(bob.ID), nil), "member")
	if m["invited"] != true {
		t.Fatalf("an invitation: %v", m)
	}
	req := requestOf(bobS, "channel")
	if req["override"] != true {
		t.Fatalf("it says it is an override: %v", req)
	}
	bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+priv, nil)
	bob.Must(200, "POST", "/api/v1/me/chat-requests/"+req["id"].(string)+"/accept", nil)
	found := false
	for _, msg := range arr(ann.Must(200, "GET", "/api/v1/conversations/"+priv+"/messages", nil), "messages") {
		if sys, _ := msg.(map[string]any)["system"].(map[string]any); sys != nil && sys["kind"] == "owner_override" {
			found = true
		}
	}
	if !found {
		t.Fatal("joining posts the owner_override notice")
	}
}
