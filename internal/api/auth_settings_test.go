package api_test

import (
	"bytes"
	"context"
	"image"
	pngenc "image/png"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/auth"
	"github.com/tikaydaes/churr/internal/bots"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

func TestSettings(t *testing.T) {
	ts := testkit.NewServer(t)
	owner, admin, ann := ts.Owner, ts.Admin("adm"), ts.Member("ann")
	const path = "/api/v1/admin/settings"

	// Admins read every key, with its default; secrets never.
	ann.Refused(403, model.CodeForbidden, "GET", path, nil)
	ann.Refused(403, model.CodeForbidden, "PATCH", path, map[string]any{"server.name": "Mine"})
	set := at(admin.Must(200, "GET", path, nil), "settings")
	for key, want := range map[string]any{
		"registration.mode": "invite", "invites.members_may_invite": false, "auth.member_sign_in": "passkey", "auth.second_factor": "off",
		"sessions.passkey_seconds": float64(2592000), "sessions.sso_seconds": float64(86400), "messages.quick_correction_seconds": float64(30),
		"mentions.broadcast_managers_only": false, "mentions.broad_mention_limit": float64(250), "emoji.members_may_add": false, "blocking.enabled": true, "webhooks.managers_may_approve": false,
		"files.max_bytes": float64(104857600), "files.store": "disk", "files.s3_secret_set": false, "push.relay_enabled": false,
		"push.unifiedpush_enabled": false, "push.send_badge": true, "previews.enabled": false, "sealed.enabled": true, "calls.enabled": true,
		"calls.max_participants": float64(50), "calls.max_cameras": float64(12), "links.send_presence": true,
		"links.default_max_file_bytes": float64(26214400), "server.time_zone": "UTC", "welcome.text": "",
	} {
		if set[key] != want {
			t.Errorf("%s = %v, want %v", key, set[key], want)
		}
	}
	for _, key := range []string{"messages.edit_window_seconds", "retention.seconds", "server.icon_file_id", "files.s3"} {
		if v, present := set[key]; !present || v != nil {
			t.Errorf("%s = %v, want null and present", key, v)
		}
	}
	if got := set["auth.second_factor_accepted"].([]any); len(got) != 3 {
		t.Errorf("auth.second_factor_accepted = %v", got)
	}
	if _, has := set["files.s3_secret"]; has {
		t.Error("the secret key is never returned")
	}
	if _, has := set["setup.done_at"]; has {
		t.Error("the server's own rows are not settings")
	}

	// Values are checked, and nothing is changed by a request with a bad one.
	for key, bad := range map[string]any{
		"server.name": "", "server.time_zone": "Nowhere/Land", "registration.mode": "anyone", "invites.members_may_invite": "yes",
		"sessions.sso_seconds": 5, "retention.seconds": 30, "files.max_bytes": 0, "calls.max_participants": 1,
		"push.relay_url": "http://relay.example", "messages.quick_correction_seconds": -1, "files.store": "tape",
		"auth.second_factor_accepted": []string{"sms"}, "blocking.enabled": nil, "server.icon_file_id": "nope",
	} {
		e := admin.Refused(400, model.CodeInvalidValue, "PATCH", path, map[string]any{"welcome.text": "changed", key: bad})
		if e["field"] != key {
			t.Errorf("%s: the error names %v", key, e["field"])
		}
	}
	admin.Refused(400, model.CodeInvalidRequest, "PATCH", path, map[string]any{"no.such.setting": true})
	admin.Refused(400, model.CodeInvalidRequest, "PATCH", path, map[string]any{"files.s3_secret_set": true})
	admin.Refused(400, model.CodeInvalidRequest, "PATCH", path, map[string]any{"setup.done_at": 0})
	if at(admin.Must(200, "GET", path, nil), "settings")["welcome.text"] != "" {
		t.Fatal("a refused request changed a setting")
	}

	// A change takes effect at once, is audited with the old and new
	// value, and is announced to every connection.
	stream, _ := ann.Events(nil)
	out := at(admin.Must(200, "PATCH", path, map[string]any{"server.name": "Acme Chat", "blocking.enabled": false, "messages.edit_window_seconds": 600,
		"emoji.members_may_add": false}), "settings")
	if out["server.name"] != "Acme Chat" || out["blocking.enabled"] != false || out["messages.edit_window_seconds"] != float64(600) {
		t.Fatalf("after the change: %v", out)
	}
	e := stream.Expect("server.updated")
	if at(e, "server")["community_name"] != "Acme Chat" || at(e, "server", "features")["blocking"] != false || at(e, "server", "limits")["edit_window_seconds"] != float64(600) {
		t.Fatalf("server.updated: %v", e)
	}
	srv := at(ts.Browser().Must(200, "GET", "/api/v1/server", nil), "server")
	if srv["community_name"] != "Acme Chat" || at(srv, "features")["blocking"] != false {
		t.Fatalf("getServer: %v", srv)
	}
	changes := audited(t, ts, "setting.changed")
	// Only what changed is written: emoji.members_may_add stayed false.
	if len(changes) != 3 {
		t.Fatalf("%d audit entries, want 3: %+v", len(changes), changes)
	}
	for _, c := range changes {
		if c.TargetID == "server.name" && (c.Detail["new"] != "Acme Chat" || c.ActorID != string(admin.ID)) {
			t.Fatalf("audit: %+v", c)
		}
		if c.TargetID == "blocking.enabled" && (c.Detail["old"] != true || c.Detail["new"] != false) {
			t.Fatalf("audit: %+v", c)
		}
	}
	// Null puts a nullable setting back to "no limit".
	if at(admin.Must(200, "PATCH", path, map[string]any{"messages.edit_window_seconds": nil}), "settings")["messages.edit_window_seconds"] != nil {
		t.Fatal("null did not clear the edit window")
	}

	// The sign-in settings need a fresh check; the rest do not.
	admin.Stale()
	admin.Must(200, "PATCH", path, map[string]any{"welcome.text": "Hello"})
	for key, v := range map[string]any{"registration.mode": "open", "invites.members_may_invite": true, "auth.member_sign_in": "either",
		"auth.second_factor": "required", "auth.second_factor_accepted": []string{"totp"}, "sessions.passkey_seconds": 86400, "sessions.sso_seconds": 3600} {
		admin.Refused(403, model.CodeFreshCheckRequired, "PATCH", path, map[string]any{key: v})
	}
	// A personal token reads and changes ordinary settings, and can
	// never change the ones that need a fresh check.
	pat := owner.Token()
	pat.Must(200, "GET", path, nil)
	pat.Must(200, "PATCH", path, map[string]any{"previews.enabled": true})
	pat.Refused(403, model.CodeForbidden, "PATCH", path, map[string]any{"registration.mode": "open"})
	admin.Check()
	admin.Must(200, "PATCH", path, map[string]any{"registration.mode": "open", "invites.members_may_invite": true, "sessions.passkey_seconds": 86400})

	// `sso` needs an enabled provider; a required second factor needs
	// something accepted.
	admin.Refused(409, model.CodeConflict, "PATCH", path, map[string]any{"auth.member_sign_in": "sso"})
	admin.Refused(409, model.CodeConflict, "PATCH", path, map[string]any{"auth.second_factor": "required", "auth.second_factor_accepted": []string{}})
	admin.Must(200, "PATCH", path, map[string]any{"auth.second_factor": "required"})
	admin.Refused(409, model.CodeConflict, "PATCH", path, map[string]any{"auth.second_factor_accepted": []string{}})
	admin.Must(200, "PATCH", path, map[string]any{"auth.second_factor": "off", "auth.second_factor_accepted": []string{}})

	// The passkey session length is read at the next sign-in.
	b := ts.Browser()
	b.CopyPasskey(ann)
	_, in := b.Login()
	exp, _ := time.Parse(time.RFC3339Nano, at(in, "session")["expires_at"].(string))
	if d := time.Until(exp); d > 25*time.Hour || d < 23*time.Hour {
		t.Fatalf("sessions.passkey_seconds of a day gave a session of %v", d)
	}

	// The object store's secret is write-only and sealed.
	s3 := map[string]any{"endpoint": "https://s3.example", "region": "eu-1", "bucket": "churr", "prefix": "", "path_style": true, "access_key_id": "AKIA"}
	admin.Refused(409, model.CodeConflict, "PATCH", path, map[string]any{"files.store": "s3"})
	out = at(admin.Must(200, "PATCH", path, map[string]any{"files.s3": s3, "files.s3_secret": "very-secret"}), "settings")
	if out["files.s3_secret_set"] != true || out["files.s3_secret"] != nil || at(out, "files.s3")["bucket"] != "churr" {
		t.Fatalf("after setting the object store: %v", out)
	}
	raw, _, _ := ts.Server.Store.Settings().Get(context.Background(), "files.s3_secret")
	if strings.Contains(string(raw), "very-secret") {
		t.Fatal("the secret is stored unsealed")
	}
	if key, found, err := auth.S3Secret(context.Background(), ts.Server.Store, ts.Server.Keys); err != nil || !found || key != "very-secret" {
		t.Fatalf("the sealed secret reads back as %q (%v, %v)", key, found, err)
	}
	for _, c := range audited(t, ts, "setting.changed") {
		if c.TargetID == "files.s3_secret" && (c.Detail["new"] != nil || c.Detail["old"] != nil) {
			t.Fatalf("the audit log must not hold the secret: %+v", c)
		}
	}
}

func TestServerIcon(t *testing.T) {
	ts := testkit.NewServer(t)
	admin, ann := ts.Admin("adm"), ts.Member("ann")
	var img bytes.Buffer
	if err := pngenc.Encode(&img, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	const path = "/api/v1/admin/settings/icon"
	if status, out := ann.SendForm("PUT", path, "file", "icon.png", img.Bytes()); status != 403 {
		t.Fatalf("a member set the icon: %d %v", status, out)
	}
	if status, out := admin.SendForm("PUT", path, "file", "icon.png", []byte("just some text, not an image")); status != 400 {
		t.Fatalf("not an image: %d %v", status, out)
	}
	if status, out := admin.SendForm("PUT", path, "other", "icon.png", img.Bytes()); status != 400 {
		t.Fatalf("no part named file: %d %v", status, out)
	}
	status, out := admin.SendForm("PUT", path, "file", "icon.png", img.Bytes())
	first, _ := at(out, "settings")["server.icon_file_id"].(string)
	if status != 200 || !model.ValidID(first) {
		t.Fatalf("setting the icon: %d %v", status, out)
	}
	// A new icon replaces the old one, whose file goes.
	_, out = admin.SendForm("PUT", path, "file", "icon.png", img.Bytes())
	second, _ := at(out, "settings")["server.icon_file_id"].(string)
	if second == first || !model.ValidID(second) {
		t.Fatalf("replacing the icon: %v", out)
	}
	if _, err := ts.Server.Store.Files().Get(context.Background(), model.FileID(first)); err == nil {
		t.Fatal("the old icon's file was kept")
	}
	wantAudit(t, ts, "setting.changed")
}

func TestIntegrations(t *testing.T) {
	ts := testkit.NewServer(t)
	owner, admin, ann, bob := ts.Owner, ts.Admin("adm"), ts.Member("ann"), ts.Member("bob")
	shared := ann.Channel("ops", true, bob)
	_ = shared
	const path = "/api/v1/admin/integrations"

	// Admins create them, with a fresh check.
	ann.Refused(403, model.CodeForbidden, "POST", path, map[string]any{"name": "Calendar", "allows": []string{"availability"}})
	ann.Refused(403, model.CodeForbidden, "GET", path, nil)
	admin.Refused(400, model.CodeInvalidValue, "POST", path, map[string]any{"name": "", "allows": []string{"availability"}})
	admin.Refused(400, model.CodeInvalidValue, "POST", path, map[string]any{"name": "Calendar", "allows": []string{"messages"}})
	admin.Stale()
	admin.Refused(403, model.CodeFreshCheckRequired, "POST", path, map[string]any{"name": "Calendar", "allows": []string{"availability"}})
	admin.Check()
	made := admin.Must(201, "POST", path, map[string]any{"name": "Calendar", "allows": []string{"availability"}})
	secret := made["secret"].(string)
	in := at(made, "integration")
	if !strings.HasPrefix(secret, "churr_int_") || in["name"] != "Calendar" || in["created_by_id"] != string(admin.ID) || in["allows"].([]any)[0] != "availability" {
		t.Fatalf("created: %v", made)
	}
	wantAudit(t, ts, "integration.created")
	plain := at(owner.Must(201, "POST", path, map[string]any{"name": "Nothing ticked", "allows": []string{}}), "integration")
	plainSecret := owner.Must(201, "POST", path, map[string]any{"name": "Also nothing", "allows": []string{}})["secret"].(string)
	list := owner.Must(200, "GET", path, nil)["integrations"].([]any)
	if len(list) != 3 || list[0].(map[string]any)["secret"] != nil {
		t.Fatalf("list: %v", list)
	}

	cal, none := ts.App(secret), ts.App(plainSecret)
	// An integration token is not an account: it fetches no
	// conversations, messages or accounts (case 94).
	cal.Refused(403, model.CodeForbidden, "GET", "/api/v1/conversations", nil)
	cal.Refused(403, model.CodeForbidden, "GET", "/api/v1/accounts", nil)
	cal.Refused(403, model.CodeForbidden, "GET", "/api/v1/me", nil)
	cal.Refused(403, model.CodeForbidden, "GET", "/api/v1/conversations/"+shared+"/messages", nil)
	// And nothing else is an integration token.
	find := "/api/v1/integration/accounts"
	ann.Token().Refused(401, model.CodeUnauthenticated, "GET", find+"?username=ann", nil)
	ann.Refused(401, model.CodeUnauthenticated, "GET", find+"?username=ann", nil)
	ts.App("churr_int_made-up").Refused(401, model.CodeUnauthenticated, "GET", find+"?username=ann", nil)

	// Finding an account: by username, and nothing but id and username.
	got := cal.Must(200, "GET", find+"?username=Ann", nil)
	if acc := at(got, "account"); acc["id"] != string(ann.ID) || acc["username"] != "ann" || len(acc) != 2 {
		t.Fatalf("found: %v", got)
	}
	cal.Refused(404, model.CodeNotFound, "GET", find+"?username=nobody", nil)
	cal.Refused(400, model.CodeInvalidRequest, "GET", find, nil)
	cal.Refused(400, model.CodeInvalidRequest, "GET", find+"?username=ann&subject=x", nil)
	cal.Refused(400, model.CodeInvalidValue, "GET", find+"?provider_id=nope&subject=x", nil)
	// By the identifier at a company sign-in provider.
	ctx := context.Background()
	p := model.SsoProvider{Kind: model.ProviderOIDC, Name: "Acme", Config: []byte(`{}`), Enabled: true}
	if err := ts.Server.Store.Identities().CreateProvider(ctx, &p); err != nil {
		t.Fatal(err)
	}
	if err := ts.Server.Store.Identities().Link(ctx, model.Identity{ProviderID: p.ID, Subject: "sub-bob", AccountID: bob.ID}); err != nil {
		t.Fatal(err)
	}
	if acc := at(cal.Must(200, "GET", find+"?provider_id="+p.ID+"&subject=sub-bob", nil), "account"); acc["username"] != "bob" {
		t.Fatalf("found by subject: %v", acc)
	}
	cal.Refused(404, model.CodeNotFound, "GET", find+"?provider_id="+p.ID+"&subject=sub-nobody", nil)
	// Without the availability tick, nothing (case 95).
	set := func(id model.AccountID) string { return find + "/" + string(id) + "/availability" }
	until := time.Now().Add(time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
	none.Refused(403, model.CodeForbidden, "GET", find+"?username=ann", nil)
	none.Refused(403, model.CodeForbidden, "PUT", set(ann.ID), map[string]any{"availability": "meeting", "until": until})

	// Setting availability: a meeting needs its end, at most 24 hours ahead.
	cal.Refused(400, model.CodeInvalidValue, "PUT", set(ann.ID), map[string]any{"availability": "meeting"})
	cal.Refused(400, model.CodeInvalidValue, "PUT", set(ann.ID), map[string]any{"availability": "meeting", "until": time.Now().Add(25 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")})
	cal.Refused(400, model.CodeInvalidValue, "PUT", set(ann.ID), map[string]any{"availability": "meeting", "until": time.Now().Add(-time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")})
	cal.Refused(400, model.CodeInvalidValue, "PUT", set(ann.ID), map[string]any{"availability": "asleep"})
	cal.Refused(400, model.CodeInvalidRequest, "PUT", set(ann.ID), map[string]any{"availability": "meeting", "until": until, "status": "x"})
	cal.Refused(404, model.CodeNotFound, "PUT", set(model.NewID[model.AccountID]()), map[string]any{"availability": "meeting", "until": until})
	stream, _ := bob.Events(nil)
	out := cal.Must(200, "PUT", set(ann.ID), map[string]any{"availability": "meeting", "until": until})
	if av := at(out, "availability"); out["id"] != string(ann.ID) || av["value"] != "meeting" || av["set_by"] != "integration" {
		t.Fatalf("set: %v", out)
	}
	// The people who can see the account are told.
	e := stream.ExpectWhere("availability.changed", func(f map[string]any) bool { return f["account_id"] == string(ann.ID) })
	if at(e, "availability")["value"] != "meeting" || at(e, "availability")["set_by"] != "integration" {
		t.Fatalf("availability.changed: %v", e)
	}
	if av := at(ann.Must(200, "GET", "/api/v1/me", nil), "account", "availability"); av["value"] != "meeting" {
		t.Fatalf("the account's availability: %v", av)
	}
	// Every use is in the audit log, with the integration as the actor.
	if u := wantAudit(t, ts, "integration.used"); u.ActorKind != "integration" || u.ActorID != in["id"] || u.TargetID != string(ann.ID) {
		t.Fatalf("audit: %+v", u)
	}
	// An integration posts nothing and is in no conversation: the tick
	// lets it clear what it set itself, and nothing else.
	out = cal.Must(200, "PUT", set(ann.ID), map[string]any{"availability": nil})
	if out["availability"] != nil {
		t.Fatalf("cleared: %v", out)
	}
	// Clearing when there is nothing to clear is not an error.
	cal.Must(200, "PUT", set(ann.ID), map[string]any{"availability": nil})
	// What the person set is left alone.
	ann.Must(200, "PUT", "/api/v1/me/availability", map[string]any{"value": "out_of_office"})
	cal.Refused(409, model.CodeConflict, "PUT", set(ann.ID), map[string]any{"availability": nil})
	if av := at(ann.Must(200, "GET", "/api/v1/me", nil), "account", "availability"); av["value"] != "out_of_office" || av["set_by"] != "self" {
		t.Fatalf("the person's own availability was touched: %v", av)
	}
	// So is what another integration set.
	second := ts.App(owner.Must(201, "POST", path, map[string]any{"name": "HR", "allows": []string{"availability"}})["secret"].(string))
	second.Must(200, "PUT", set(bob.ID), map[string]any{"availability": "out_of_office"})
	cal.Refused(409, model.CodeConflict, "PUT", set(bob.ID), map[string]any{"availability": nil})
	second.Must(200, "PUT", set(bob.ID), map[string]any{"availability": nil})
	// Setting replaces whatever was there.
	cal.Must(200, "PUT", set(ann.ID), map[string]any{"availability": "meeting", "until": until})

	// Revoking ends the token.
	ann.Refused(403, model.CodeForbidden, "DELETE", path+"/"+in["id"].(string), nil)
	admin.Refused(404, model.CodeNotFound, "DELETE", path+"/"+string(model.NewID[model.AccountID]()), nil)
	admin.Must(204, "DELETE", path+"/"+in["id"].(string), nil)
	cal.Refused(401, model.CodeUnauthenticated, "GET", find+"?username=ann", nil)
	wantAudit(t, ts, "integration.revoked")
	for _, i := range owner.Must(200, "GET", path, nil)["integrations"].([]any) {
		if m := i.(map[string]any); m["id"] == in["id"] && m["revoked_at"] == nil {
			t.Fatalf("the revoked integration is listed as live: %v", m)
		}
	}
	_ = plain
}

// Each integration has its own allowance of requests (docs/BOTS.md section
// 8); past it the answer is rate_limited with when to try again.
func TestIntegrationRateLimit(t *testing.T) {
	limits := bots.DefaultLimits()
	limits.IntegrationRequestsPerMinute = 3
	ts := testkit.NewServer(t, testkit.Options{BotLimits: &limits})
	ts.Member("ann")
	make := func(name string) *testkit.Client {
		return ts.App(ts.Owner.Must(201, "POST", "/api/v1/admin/integrations", map[string]any{"name": name, "allows": []string{"availability"}})["secret"].(string))
	}
	cal, hr := make("Calendar"), make("HR")
	const find = "/api/v1/integration/accounts?username=ann"
	for i := 0; i < 3; i++ {
		cal.Must(200, "GET", find, nil)
	}
	status, header, out := cal.DoFull("GET", find, nil)
	e, _ := out["error"].(map[string]any)
	if status != 429 || e["code"] != "rate_limited" || e["retry_after_seconds"] == nil || header.Get("Retry-After") == "" {
		t.Fatalf("past the allowance: %d %v", status, out)
	}
	// Another integration has its own allowance, and a token that
	// matches nothing uses up nobody's.
	hr.Must(200, "GET", find, nil)
	ts.App("churr_int_made-up").Refused(401, model.CodeUnauthenticated, "GET", find, nil)
}
