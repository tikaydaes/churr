package api_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tikaydaes/churr/internal/api"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The images of a webhook's cards are files of the message they are on,
// whether they came as parts of the request or were fetched from an
// address, so whatever removes the message removes them.
func TestWebhookCardImagesBelongToTheMessage(t *testing.T) {
	ts := testkit.NewServer(t)
	ctx := context.Background()
	ann := ts.Member("ann")
	ops := ann.Channel("ops", false)
	_, path, _ := ann.Webhook(ops, "CI")
	ts.Server.Bots.SetImages(&fakeImages{on: true})
	files := ts.Server.Store.Files()

	check := func(what string, msg model.MessageID, want int) {
		t.Helper()
		images, err := files.CardImages(ctx, msg)
		if err != nil || len(images) != want {
			t.Fatalf("%s: %d card images on the message (%v), want %d", what, len(images), err, want)
		}
		// Retention removes the message, and with it every file it has.
		keys, deleted, err := ts.Server.Store.Janitor().ExpireMessage(ctx, msg, model.Now())
		if err != nil || !deleted {
			t.Fatalf("%s: expiring the message: %v, deleted=%v", what, err, deleted)
		}
		if len(keys) < want {
			t.Fatalf("%s: %d blobs handed back for removal, want at least %d", what, len(keys), want)
		}
		for _, f := range images {
			if _, err := files.Get(ctx, f.ID); model.CodeOf(err) != model.CodeNotFound {
				t.Fatalf("%s: a card image outlived its message: %v", what, err)
			}
		}
	}

	// Sent as parts of the request.
	raw, _ := json.Marshal(map[string]any{
		"text":  "A graph",
		"cards": []any{map[string]any{"image_part": "file1", "author": map[string]any{"name": "CI", "icon_part": "file2"}}},
		"via":   map[string]any{"name": "CI", "icon_part": "file3"},
	})
	header, body := botsMultipart(botsPart{name: "payload", data: raw}, botsPart{"file1", "graph.png", botsPNG()},
		botsPart{"file2", "icon.png", botsPNG()}, botsPart{"file3", "via.png", botsPNG()})
	status, _, resp := ts.Raw("POST", path, header, body)
	out := map[string]any{}
	json.Unmarshal(resp, &out)
	if status != 200 {
		t.Fatalf("a multipart post = %d %s", status, resp)
	}
	check("parts", model.MessageID(obj(out, "message")["id"].(string)), 3)

	// Named by address and fetched after the message was posted.
	m := hookPosted(t, ts, path, map[string]any{"text": "Graphs", "cards": []any{
		map[string]any{"title": "Good", "image_url": "https://ci.example/graph.png"}}})
	ts.Server.Bots.Wait()
	check("fetched", model.MessageID(m["id"].(string)), 1)

	// A message that is nothing but a fetched image: the image is fetched
	// before the message exists.
	m = hookPosted(t, ts, path, map[string]any{"cards": []any{map[string]any{"image_url": "https://ci.example/alone.png"}}})
	ts.Server.Bots.Wait()
	check("fetched first", model.MessageID(m["id"].(string)), 1)
}

// A change company sign-in makes from the provider's groups sends the events
// the same change made by an admin sends: account.updated for the role, and
// the group member events for the groups.
func TestProviderChangesAreAnnounced(t *testing.T) {
	ts := testkit.NewServer(t)
	idp, pid := oidcSetup(t, ts, map[string]any{"admin_group": "churr-admins"})
	ctx := context.Background()
	g := model.Group{Handle: "eng", Name: "Engineering", ProviderID: pid, ProviderGroup: "engineering"}
	if err := ts.Server.Store.Groups().Create(ctx, &g); err != nil {
		t.Fatal(err)
	}
	signIn := func(groups []string) *testkit.Client {
		c := ts.Browser()
		if p := oidcSignIn(t, ts, c, idp, pid, "", testkit.IdPUser{Subject: "sub-ann", Username: "ann", Groups: groups}); p.Status != 200 {
			t.Fatalf("sign-in: %d %s", p.Status, p.Body)
		}
		return c
	}
	// The account exists first, as a plain member in no mapped group.
	first := signIn([]string{})
	id := signedInAs(t, first, "ann")["id"].(string)
	bob := ts.Member("bob")
	watch, _ := bob.Events(nil)
	own, _ := first.Events(nil)

	signIn([]string{"churr-admins", "engineering"})
	for who, s := range map[string]*testkit.Stream{"another member": watch, "the account itself": own} {
		e := s.ExpectWhere("group.member_added", func(f map[string]any) bool { return f["group_id"] == string(g.ID) })
		// Each reader's copy says whether that reader is in the group.
		if e["account_id"] != id || obj(e, "group")["is_member"] != (who == "the account itself") {
			t.Fatalf("%s: group.member_added: %v", who, e)
		}
		e = s.ExpectWhere("account.updated", func(f map[string]any) bool { return obj(f, "account")["id"] == id })
		if obj(e, "account")["role"] != "admin" {
			t.Fatalf("%s: account.updated after the provider made an admin: %v", who, e)
		}
	}

	signIn([]string{})
	for who, s := range map[string]*testkit.Stream{"another member": watch, "the account itself": own} {
		e := s.ExpectWhere("group.member_removed", func(f map[string]any) bool { return f["group_id"] == string(g.ID) })
		if e["account_id"] != id {
			t.Fatalf("%s: group.member_removed: %v", who, e)
		}
		e = s.ExpectWhere("account.updated", func(f map[string]any) bool { return obj(f, "account")["id"] == id })
		if obj(e, "account")["role"] != "member" {
			t.Fatalf("%s: account.updated after the provider took admin away: %v", who, e)
		}
	}
}

type fakeCalls struct{}

func (fakeCalls) LiveCalls() int       { return 3 }
func (fakeCalls) MediaAddress() string { return "203.0.113.20:24877" }

type fakeLinks struct{}

func (fakeLinks) OverviewPeers(context.Context) ([]any, error) {
	return []any{map[string]any{"id": "p1"}}, nil
}
func (fakeLinks) OutboxWaiting(context.Context) (int64, error) { return 7, nil }

// The Overview carries real numbers: the database's size, files, accounts,
// the webhook queue, and each outside connection with whether it is on. What
// calls and links report is read through interfaces that count as nothing
// until the server sets them.
func TestOverviewNumbers(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	ops := ann.Channel("ops", false)
	ann.Post(ops, "hello")
	ann.Upload(ops, "notes.txt", []byte("twelve bytes"))
	ts.Guest("gus", ops)
	ts.Owner.Bot("robot")
	ann.Must(201, "POST", "/api/v1/conversations/"+ops+"/webhook-requests", map[string]any{"name": "CI", "purpose": "testing"})

	get := func() map[string]any {
		return obj(ts.Owner.Must(200, "GET", "/api/v1/admin/overview", nil), "overview")
	}
	ov := get()
	if kind := obj(ov, "database")["kind"]; kind != ts.Server.Store.Dialect() {
		t.Fatalf("database kind: %v", kind)
	}
	if size, _ := obj(ov, "database")["size"].(float64); size < 4096 {
		t.Fatalf("the database's size: %v", ov["database"])
	}
	if f := obj(ov, "files"); f["count"] != 1.0 || f["size"] != 12.0 || f["store"] != "disk" {
		t.Fatalf("files: %v", f)
	}
	// Two bots: the one the owner made, and the account that stands behind
	// the webhook request.
	if a := obj(ov, "accounts"); a["people"] != 2.0 || a["guests"] != 1.0 || a["bots"] != 2.0 || a["remote"] != 0.0 {
		t.Fatalf("accounts: %v", a)
	}
	if w := obj(ov, "waiting"); w["webhooks"] != 1.0 {
		t.Fatalf("one webhook request is waiting: %v", w)
	}
	// With nothing going on: no calls, no peers, an empty outbox. The calls
	// package is built in now, so the media address of the test server
	// (a spare port on the loopback address) is reported.
	if addr, _ := ov["media_address"].(string); ov["live_calls"] != 0.0 || len(arr(ov, "peers")) != 0 || ov["peers"] == nil || ov["outbox"] != 0.0 || len(addr) < 11 || addr[:10] != "127.0.0.1:" {
		t.Fatalf("calls and links before anything reports them: %v", ov)
	}
	// Every outside connection is listed; a new server has only the push
	// relay on (docs/OPERATIONS.md section 5).
	states := func(ov map[string]any) map[string]map[string]any {
		out := map[string]map[string]any{}
		for _, c := range arr(ov, "outside") {
			out[c.(map[string]any)["name"].(string)] = c.(map[string]any)
		}
		return out
	}
	outside := states(ov)
	if len(outside) != 7 {
		t.Fatalf("the seven outside connections: %v", ov["outside"])
	}
	for name, c := range outside {
		on := name == api.OutsidePushRelay
		if c["enabled"] != on || (c["state"] == "off") == on {
			t.Fatalf("%s on a new server: %v", name, c)
		}
	}
	if len(arr(ov, "warnings")) != 0 || ov["warnings"] == nil {
		t.Fatalf("warnings: %v", ov["warnings"])
	}

	// Switches change what is on; the packages report through the
	// interfaces; a failing connection is a warning.
	ts.Set("previews.enabled", true)
	ts.Set("push.relay_enabled", false)
	ts.Server.API.Overview.Calls = fakeCalls{}
	ts.Server.API.Overview.Links = fakeLinks{}
	okAt := model.Now()
	ts.Server.API.Overview.Probes = map[string]api.OutsideProbe{
		api.OutsideLinkPreviews: func(context.Context) api.OutsideHealth { return api.OutsideHealth{LastOK: okAt} },
		api.OutsidePeers: func(context.Context) api.OutsideHealth {
			return api.OutsideHealth{LastError: "chat.example.org refused the connection"}
		},
		// A probe for something that is off is not asked.
		api.OutsidePushRelay: func(context.Context) api.OutsideHealth { return api.OutsideHealth{LastError: "never shown"} },
	}
	ov = get()
	outside = states(ov)
	if c := outside[api.OutsideLinkPreviews]; c["enabled"] != true || c["state"] != "ok" || c["last_ok_at"] == nil {
		t.Fatalf("link previews switched on: %v", c)
	}
	if c := outside[api.OutsidePushRelay]; c["enabled"] != false || c["state"] != "off" || c["last_error"] != nil {
		t.Fatalf("the relay switched off: %v", c)
	}
	if c := outside[api.OutsidePeers]; c["enabled"] != true || c["state"] != "failing" || c["last_error"] != "chat.example.org refused the connection" {
		t.Fatalf("a failing peer: %v", c)
	}
	if ov["live_calls"] != 3.0 || ov["media_address"] != "203.0.113.20:24877" || len(arr(ov, "peers")) != 1 || ov["outbox"] != 7.0 {
		t.Fatalf("what calls and links report: %v", ov)
	}
	if w := arr(ov, "warnings"); len(w) != 1 {
		t.Fatalf("a failing connection is a warning: %v", w)
	}
}

// A claim link takes over an imported account: the holder makes a passkey
// and the account that was there, with whatever it was given by the import,
// becomes theirs. No account is made, and it works in every registration
// mode (docs/AUTH.md section 4.4).
func TestClaimLink(t *testing.T) {
	ts := testkit.NewServer(t)
	ctx := context.Background()
	admin := ts.Admin("ada")
	ann := ts.Member("ann")
	// Something the import gave the account: a message in a channel.
	imported := ts.Imported("ivy")
	old := ann.Channel("old-days", false)
	if _, err := ts.Server.Store.Members().AddDirect(ctx, model.ConversationID(old), imported, model.LevelMember, model.Now()); err != nil {
		t.Fatal(err)
	}
	link := func(id model.AccountID) string {
		return obj(admin.Must(201, "POST", "/api/v1/admin/accounts/"+string(id)+"/claim-link", nil), "invite")["code"].(string)
	}
	code := link(imported)

	// The page that opens the link is told whose account it is.
	nobody := ts.Browser()
	if got := nobody.Must(200, "GET", "/api/v1/invite-codes/"+code, nil); got["claims_username"] != "ivy" || got["role"] != "member" {
		t.Fatalf("checking a claim link: %v", got)
	}
	// An ordinary invite names nobody.
	plain := obj(admin.Must(201, "POST", "/api/v1/invites", map[string]any{"role": "member"}), "invite")["code"].(string)
	if got := nobody.Must(200, "GET", "/api/v1/invite-codes/"+plain, nil); got["claims_username"] != nil {
		t.Fatalf("an ordinary invite: %v", got)
	}

	// Claiming works with registration closed, because no account is made.
	ts.Set("registration.mode", "closed")
	// A username someone has is refused before any passkey is made.
	nobody.Refused(409, model.CodeNameTaken, "POST", "/api/v1/passkey/register/begin", map[string]any{"invite_code": code, "username": "ann", "display_name": "Ivy"})
	stream, _ := ann.Events(nil)
	ivy := ts.Browser()
	out := ivy.Register("ivy-lee", code)
	acc := obj(out, "account")
	if acc["id"] != string(imported) || acc["username"] != "ivy-lee" || len(arr(out, "recovery_codes")) != 8 {
		t.Fatalf("the claimed account is the imported one, with the name chosen: %v", out)
	}
	stored, err := ts.Server.Store.Accounts().Get(ctx, imported)
	if err != nil || stored.ClaimedAt.IsZero() || len(stored.WebAuthnHandle) == 0 {
		t.Fatalf("the stored account after the claim: %+v %v", stored, err)
	}
	// It is in @everyone now, and so in #general; others are told.
	stream.ExpectWhere("member.joined", func(f map[string]any) bool {
		return f["account_id"] == string(imported) || obj(f, "member")["account_id"] == string(imported)
	})
	// It keeps what the import gave it, and signs in again with its passkey.
	ivy.Must(201, "POST", "/api/v1/conversations/"+old+"/messages", map[string]any{"text": "I am back"})
	if status, _ := ivy.Login(); status != 200 {
		t.Fatalf("signing in with the passkey made by the claim: %d", status)
	}
	if e := wantAudit(t, ts, "account.claimed"); e.Detail["by"] != "link" || e.TargetID != string(imported) {
		t.Fatalf("audit: %+v", e)
	}
	// The link worked once.
	other := ts.Browser()
	other.Refused(404, model.CodeNotFound, "GET", "/api/v1/invite-codes/"+code, nil)
	other.Refused(404, model.CodeNotFound, "POST", "/api/v1/passkey/register/begin", map[string]any{"invite_code": code, "username": "mallory", "display_name": "M"})

	// With an empty username the imported one is kept.
	second := ts.Imported("sam")
	code = link(second)
	sam := ts.Browser()
	begin := sam.Must(200, "POST", "/api/v1/passkey/register/begin", map[string]any{"invite_code": code, "username": "", "display_name": ""})
	done := sam.Must(201, "POST", "/api/v1/passkey/register/finish", sam.CreatePasskey(begin))
	if obj(done, "account")["username"] != "sam" || obj(done, "account")["id"] != string(second) {
		t.Fatalf("a claim that keeps the imported name: %v", done)
	}

	// Where members must use company sign-in, a claim link cannot be used
	// to make a passkey account.
	third := ts.Imported("tess")
	code = link(third)
	ts.Set("auth.member_sign_in", "sso")
	ts.Browser().Refused(403, model.CodeForbidden, "POST", "/api/v1/passkey/register/begin", map[string]any{"invite_code": code, "username": "tess", "display_name": "Tess"})
}

// The same link carried through company sign-in claims the account for the
// identity the provider vouches for.
func TestClaimLinkThroughCompanySignIn(t *testing.T) {
	ts := testkit.NewServer(t)
	idp, pid := oidcSetup(t, ts, nil)
	admin := ts.Admin("ada")
	imported := ts.Imported("ivy")
	code := obj(admin.Must(201, "POST", "/api/v1/admin/accounts/"+string(imported)+"/claim-link", nil), "invite")["code"].(string)
	ts.Set("registration.mode", "closed")

	c := ts.Browser()
	if p := oidcSignIn(t, ts, c, idp, pid, "?invite_code="+code, testkit.IdPUser{Subject: "sub-ivy", Username: "someone-else", Name: "Ivy"}); p.Status != 200 {
		t.Fatalf("claiming through the provider: %d %s", p.Status, p.Body)
	}
	if me := signedInAs(t, c, "ivy"); me["id"] != string(imported) {
		t.Fatalf("signed in as another account: %v", me)
	}
	if e := wantAudit(t, ts, "account.claimed"); e.Detail["by"] != "link" || e.Detail["provider"] == nil {
		t.Fatalf("audit: %+v", e)
	}
	// The next sign-in finds the account by its identity; the link is spent.
	again := ts.Browser()
	oidcSignIn(t, ts, again, idp, pid, "", testkit.IdPUser{Subject: "sub-ivy", Username: "x", Name: "Ivy"})
	signedInAs(t, again, "ivy")
	ts.Browser().Refused(404, model.CodeNotFound, "GET", "/api/v1/invite-codes/"+code, nil)
}
