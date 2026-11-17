package api_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The quiet routes into a private channel (review finding H3) are loud:
// each act by which an Owner or Admin could come to read a private channel
// as someone else posts a notice there that nobody can turn off, naming
// who did it, and is written to the audit log.

// notices returns the system notices of a kind in a conversation, as c
// sees it.
func noticesOf(c *testkit.Client, conv, kind string) []map[string]any {
	var out []map[string]any
	for _, m := range c.Messages(conv) {
		if s, ok := m["system"].(map[string]any); ok && s["kind"] == kind {
			out = append(out, m)
		}
	}
	return out
}

func wantNotice(t *testing.T, c *testkit.Client, conv, kind string, author, by model.AccountID) map[string]any {
	t.Helper()
	var list []map[string]any
	for _, m := range noticesOf(c, conv, kind) {
		if m["author_id"] == string(author) {
			list = append(list, m)
		}
	}
	if len(list) != 1 {
		t.Fatalf("%s notices about %s in the channel: %v", kind, author, c.Messages(conv))
	}
	m := list[0]
	s := m["system"].(map[string]any)
	if by != "" && s["by_id"] != string(by) {
		t.Fatalf("%s notice by: %v", kind, m)
	}
	return m
}

func TestBotTokenInPrivateChannelIsAnnounced(t *testing.T) {
	ts := testkit.NewServer(t)
	admin := ts.Admin("ada")
	ann := ts.Member("ann")
	den := ann.Channel("den", true)
	pub := ann.Channel("square", false)
	bot := admin.Bot("helper")
	ann.Add(den, "", bot)
	ann.Add(pub, "", bot)

	tok := admin.Must(201, "POST", "/api/v1/admin/bots/"+string(bot.ID)+"/tokens", map[string]any{"label": "again"})
	wantNotice(t, ann, den, "bot_token_made", bot.ID, admin.ID)
	// A public channel is anyone's to read already.
	if n := noticesOf(ann, pub, "bot_token_made"); len(n) != 0 {
		t.Fatalf("a public channel is told: %v", n)
	}
	if e := wantAudit(t, ts, "token.created"); e.TargetID != tok["token"].(map[string]any)["id"] {
		t.Fatalf("audit: %+v", e)
	}
}

func TestRecoveryLinkUseIsAnnounced(t *testing.T) {
	ts := testkit.NewServer(t)
	admin := ts.Admin("ada")
	ann, bob := ts.Member("ann"), ts.Member("bob")
	den := ann.Channel("den", true, bob)

	link := admin.Must(201, "POST", "/api/v1/admin/accounts/"+string(bob.ID)+"/recovery-link", nil)
	u := link["url"].(string)
	// Making the link says nothing yet: it may never be used.
	if n := noticesOf(ann, den, "account_recovered"); len(n) != 0 {
		t.Fatalf("told before use: %v", n)
	}
	bobStream, _ := bob.Events(nil)
	ts.Browser().Must(200, "POST", "/api/v1/recovery/link", map[string]any{"token": u[strings.LastIndex(u, "/")+1:]})
	wantNotice(t, ann, den, "account_recovered", bob.ID, admin.ID)
	// Bob's connections are told at once.
	if e := bobStream.Expect("notice.created"); obj(e, "notice")["kind"] != "recovery_link_used" {
		t.Fatalf("the live notice: %v", e)
	}
	if e := wantAudit(t, ts, "recovery.link_used"); e.Detail["link_made_by"] != string(admin.ID) {
		t.Fatalf("audit: %+v", e)
	}
	// Bob is told, and it is kept.
	got := bob.Must(200, "GET", "/api/v1/me/notices", nil)
	list := arr(got, "notices")
	if len(list) != 1 || got["unseen"] != float64(1) {
		t.Fatalf("bob's notices: %v", got)
	}
	n := list[0].(map[string]any)
	if n["kind"] != "recovery_link_used" || n["by_id"] != string(admin.ID) || n["seen_at"] != nil {
		t.Fatalf("bob's notice: %v", n)
	}
	bob.Must(204, "POST", "/api/v1/me/notices/seen", nil)
	got = bob.Must(200, "GET", "/api/v1/me/notices", nil)
	if got["unseen"] != float64(0) || len(arr(got, "notices")) != 1 || arr(got, "notices")[0].(map[string]any)["seen_at"] == nil {
		t.Fatalf("after seen: %v", got)
	}
	// Nobody else's notices.
	if got := ann.Must(200, "GET", "/api/v1/me/notices", nil); len(arr(got, "notices")) != 0 {
		t.Fatalf("ann's notices: %v", got)
	}
}

func TestClaimIsAnnounced(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	ann := ts.Member("ann")
	den := ann.Channel("den", true)
	imported := ts.Imported("ivy")
	if _, err := ts.Server.Store.Members().AddDirect(context.Background(), model.ConversationID(den), imported, model.LevelMember, model.Now()); err != nil {
		t.Fatal(err)
	}
	code := obj(owner.Must(201, "POST", "/api/v1/admin/accounts/"+string(imported)+"/claim-link", nil), "invite")["code"].(string)
	ivy := ts.Browser()
	ivy.Must(201, "POST", "/api/v1/access-key/register", map[string]any{"invite_code": code, "access_key_auth": ivy.NewAccessKey()})
	wantNotice(t, ann, den, "account_claimed", imported, owner.ID)
	if e := wantAudit(t, ts, "account.claimed"); e.Detail["link_made_by"] != string(owner.ID) {
		t.Fatalf("audit: %+v", e)
	}
}

func TestAdminIntoGroupOfPrivateChannelIsAnnounced(t *testing.T) {
	ts := testkit.NewServer(t)
	admin := ts.Admin("ada")
	ann, bob := ts.Member("ann"), ts.Member("bob")
	den := ann.Channel("den", true)
	group := obj(admin.Must(201, "POST", "/api/v1/admin/groups", map[string]any{"handle": "crew", "name": "Crew"}), "group")["id"].(string)
	ann.Must(200, "PUT", "/api/v1/conversations/"+den+"/groups/"+group, map[string]any{"level": "member"})

	// An admin putting themself in: the group only offers the channel
	// (ruling R103), so nothing is gained and nothing is said yet.
	admin.Must(204, "PUT", "/api/v1/admin/groups/"+group+"/members/"+string(admin.ID), nil)
	if n := noticesOf(ann, den, "group_access"); len(n) != 0 {
		t.Fatalf("a notice before anyone arrived: %v", n)
	}
	admin.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+den+"/messages", nil)
	// Accepting is an arrival, which every member is shown.
	admin.AcceptOffer(den)
	wantNotice(t, ann, den, "member_joined", admin.ID, "")
	admin.Must(204, "PUT", "/api/v1/admin/groups/"+group+"/members/"+string(bob.ID), nil)
	bob.AcceptOffer(den)
	wantNotice(t, ann, den, "member_joined", bob.ID, "")
	wantAudit(t, ts, "group.member_added")
}
