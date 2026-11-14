package storetest

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() {
	register("identities", testIdentities)
	register("integrations", testIntegrations)
	register("devices", testDevices)
	register("sessions_states", testSessionStates)
}

func testIdentities(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	now := model.Now()
	ids := s.Identities()

	okta := model.SsoProvider{Kind: model.ProviderOIDC, Name: "Okta", Config: json.RawMessage(`{"issuer":"https://id.example"}`),
		Secret: []byte("sealed"), GroupClaim: "groups", AdminGroup: "admins", Enabled: true}
	must(t, ids.CreateProvider(ctx, &okta))
	saml := model.SsoProvider{Kind: model.ProviderSAML, Name: "Entra", Config: json.RawMessage(`{}`), CreatedAt: now.Add(time.Second)}
	must(t, ids.CreateProvider(ctx, &saml))

	t.Run("providers", func(t *testing.T) {
		got, err := ids.GetProvider(ctx, okta.ID)
		must(t, err)
		eq(t, "name", got.Name, "Okta")
		eq(t, "secret", string(got.Secret), "sealed")
		eq(t, "has secret", got.HasSecret, true)
		eq(t, "group claim", got.GroupClaim, "groups")
		eq(t, "enabled", got.Enabled, true)
		list, err := ids.ListProviders(ctx)
		must(t, err)
		eq(t, "count", len(list), 2)
		eq(t, "oldest first", list[0].ID, okta.ID)
		eq(t, "no secret", list[1].HasSecret, false)
		got.Name, got.Enabled, got.AdminGroup, got.Secret = "Okta 2", false, "", nil
		must(t, ids.UpdateProvider(ctx, got))
		got, err = ids.GetProvider(ctx, okta.ID)
		must(t, err)
		eq(t, "renamed", got.Name, "Okta 2")
		eq(t, "disabled", got.Enabled, false)
		eq(t, "admin group cleared", got.AdminGroup, "")
		eq(t, "secret cleared", got.HasSecret, false)
		_, err = ids.GetProvider(ctx, model.NewID[string]())
		wantErr(t, err, model.ErrNotFound)
		_, err = ids.GetProvider(ctx, "nonsense")
		wantErr(t, err, model.ErrNotFound)
		wantErr(t, ids.DeleteProvider(ctx, model.NewID[string]()), model.ErrNotFound)
	})

	t.Run("identities", func(t *testing.T) {
		must(t, ids.Link(ctx, model.Identity{ProviderID: okta.ID, Subject: "sub-ann", AccountID: ann.ID, LinkedAt: now}))
		// A subject belongs to one account.
		wantErr(t, ids.Link(ctx, model.Identity{ProviderID: okta.ID, Subject: "sub-ann", AccountID: bob.ID}), model.ErrAlreadyExists)
		// The same subject text at another provider is another identity.
		must(t, ids.Link(ctx, model.Identity{ProviderID: saml.ID, Subject: "sub-ann", AccountID: bob.ID, LinkedAt: now}))
		got, err := ids.BySubject(ctx, okta.ID, "sub-ann")
		must(t, err)
		eq(t, "account", got.AccountID, ann.ID)
		eq(t, "provider name", got.ProviderName, "Okta 2")
		_, err = ids.BySubject(ctx, okta.ID, "Sub-Ann")
		wantErr(t, err, model.ErrNotFound) // compared exactly
		last, err := ids.LastUsed(ctx, ann.ID)
		must(t, err)
		eq(t, "never used", last.IsZero(), true)
		must(t, ids.Touch(ctx, okta.ID, "sub-ann", now.Add(time.Minute)))
		last, err = ids.LastUsed(ctx, ann.ID)
		must(t, err)
		eq(t, "last used", last, now.Add(time.Minute))
		list, err := ids.ForAccount(ctx, ann.ID)
		must(t, err)
		eq(t, "ann has one", len(list), 1)
		n, err := ids.CountForAccount(ctx, bob.ID)
		must(t, err)
		eq(t, "bob has one", n, int64(1))

		// Removing a provider removes its identities.
		must(t, ids.DeleteProvider(ctx, saml.ID))
		n, _ = ids.CountForAccount(ctx, bob.ID)
		eq(t, "bob has none", n, int64(0))
	})

	t.Run("totp", func(t *testing.T) {
		_, found, err := ids.GetTotp(ctx, ann.ID)
		must(t, err)
		eq(t, "none yet", found, false)
		must(t, ids.PutTotp(ctx, ann.ID, []byte("s1")))
		ok, err := ids.AdvanceTotp(ctx, ann.ID, 10)
		must(t, err)
		eq(t, "an unconfirmed row accepts nothing", ok, false)
		must(t, ids.PutTotp(ctx, ann.ID, []byte("s2"))) // beginning again replaces
		ok, err = ids.ConfirmTotp(ctx, ann.ID, now, 100)
		must(t, err)
		eq(t, "confirmed", ok, true)
		ok, _ = ids.ConfirmTotp(ctx, ann.ID, now, 101)
		eq(t, "only once", ok, false)
		got, found, err := ids.GetTotp(ctx, ann.ID)
		must(t, err)
		eq(t, "found", found, true)
		eq(t, "secret", string(got.Secret), "s2")
		eq(t, "confirmed at", got.ConfirmedAt, now)
		eq(t, "step", got.LastStep, int64(100))
		ok, _ = ids.AdvanceTotp(ctx, ann.ID, 100)
		eq(t, "the same step again is refused", ok, false)
		ok, _ = ids.AdvanceTotp(ctx, ann.ID, 99)
		eq(t, "an earlier step is refused", ok, false)
		ok, _ = ids.AdvanceTotp(ctx, ann.ID, 101)
		eq(t, "a later step is accepted", ok, true)
		ok, err = ids.DeleteTotp(ctx, ann.ID)
		must(t, err)
		eq(t, "deleted", ok, true)
		ok, _ = ids.DeleteTotp(ctx, ann.ID)
		eq(t, "nothing left", ok, false)

		// The janitor's part: a set-up never confirmed goes once it is
		// old enough; a confirmed one stays however old it is.
		must(t, ids.PutTotp(ctx, ann.ID, []byte("s3")))
		must(t, ids.PutTotp(ctx, bob.ID, []byte("s4")))
		ok, err = ids.ConfirmTotp(ctx, bob.ID, now, 5)
		must(t, err)
		eq(t, "bob confirmed", ok, true)
		gone, err := ids.DeleteTotpUnconfirmed(ctx, model.Now().Add(-time.Hour))
		must(t, err)
		eq(t, "too young to remove", gone, int64(0))
		gone, err = ids.DeleteTotpUnconfirmed(ctx, model.Now().Add(time.Minute))
		must(t, err)
		eq(t, "the unconfirmed row went", gone, int64(1))
		_, found, _ = ids.GetTotp(ctx, ann.ID)
		eq(t, "ann's is gone", found, false)
		_, found, _ = ids.GetTotp(ctx, bob.ID)
		eq(t, "bob's confirmed row stays", found, true)
		_, err = ids.DeleteTotp(ctx, bob.ID)
		must(t, err)
	})

	t.Run("codes", func(t *testing.T) {
		later := now.Add(2 * time.Minute)
		must(t, ids.CreateCode(ctx, []byte("auth-1"), store.OAuthCode{AccountID: ann.ID, Kind: store.CodeAuthorization, ClientName: "Pixel",
			RedirectURI: "http://127.0.0.1:1/cb", PKCEChallenge: "chal", Platform: "android", BoxKey: []byte("box"), ExpiresAt: later}))
		c, ok, err := ids.UseCode(ctx, []byte("auth-1"), now)
		must(t, err)
		eq(t, "used", ok, true)
		eq(t, "account", c.AccountID, ann.ID)
		eq(t, "redirect", c.RedirectURI, "http://127.0.0.1:1/cb")
		eq(t, "challenge", c.PKCEChallenge, "chal")
		eq(t, "box key", string(c.BoxKey), "box")
		_, ok, _ = ids.UseCode(ctx, []byte("auth-1"), now)
		eq(t, "a code works once", ok, false)
		must(t, ids.CreateCode(ctx, []byte("auth-old"), store.OAuthCode{AccountID: ann.ID, Kind: store.CodeAuthorization, ExpiresAt: now}))
		_, ok, _ = ids.UseCode(ctx, []byte("auth-old"), now)
		eq(t, "an expired code is refused", ok, false)
		_, ok, _ = ids.UseCode(ctx, []byte("unknown"), now)
		eq(t, "an unknown code is refused", ok, false)

		must(t, ids.CreateCode(ctx, []byte("pair-1"), store.OAuthCode{Kind: store.CodePairing, ExpiresAt: later}))
		ok, _ = ids.ConfirmPairing(ctx, []byte("pair-1"), ann.ID, now)
		eq(t, "nothing to confirm before a device claims it", ok, false)
		_, ok, _ = ids.UseCode(ctx, []byte("pair-1"), now)
		eq(t, "unconfirmed pairing gives nothing", ok, false)
		ok, err = ids.ClaimPairing(ctx, []byte("pair-1"), "Phone", "ios", []byte("k"), now)
		must(t, err)
		eq(t, "claimed", ok, true)
		ok, _ = ids.ClaimPairing(ctx, []byte("pair-1"), "Thief", "ios", nil, now)
		eq(t, "only the first device is recorded", ok, false)
		_, ok, _ = ids.UseCode(ctx, []byte("pair-1"), now)
		eq(t, "claimed but unconfirmed gives nothing", ok, false)
		ok, err = ids.ConfirmPairing(ctx, []byte("pair-1"), ann.ID, now)
		must(t, err)
		eq(t, "confirmed", ok, true)
		ok, _ = ids.ConfirmPairing(ctx, []byte("pair-1"), bob.ID, now)
		eq(t, "confirmed once", ok, false)
		got, err := ids.GetCode(ctx, []byte("pair-1"))
		must(t, err)
		eq(t, "name", got.ClientName, "Phone")
		eq(t, "account", got.AccountID, ann.ID)
		c, ok, _ = ids.UseCode(ctx, []byte("pair-1"), now)
		eq(t, "confirmed pairing is used", ok, true)
		eq(t, "platform", c.Platform, "ios")
		_, err = ids.GetCode(ctx, []byte("nope"))
		wantErr(t, err, model.ErrNotFound)
		n, err := ids.DeleteCodesEnded(ctx, now.Add(time.Second))
		must(t, err)
		eq(t, "the expired one is deleted", n, int64(1))
	})

	t.Run("import claims", func(t *testing.T) {
		has, err := ids.HasImportClaims(ctx)
		must(t, err)
		eq(t, "none", has, false)
		must(t, ids.AddImportClaim(ctx, bob.ID, []byte("hash-bob")))
		has, _ = ids.HasImportClaims(ctx)
		eq(t, "one", has, true)
		_, found, _ := ids.TakeImportClaim(ctx, []byte("hash-other"))
		eq(t, "no match", found, false)
		acc, found, err := ids.TakeImportClaim(ctx, []byte("hash-bob"))
		must(t, err)
		eq(t, "match", found, true)
		eq(t, "account", acc, bob.ID)
		_, found, _ = ids.TakeImportClaim(ctx, []byte("hash-bob"))
		eq(t, "taken once", found, false)
	})
}

func testIntegrations(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	now := model.Now()
	in := model.Integration{Name: "Calendar", Allows: []string{"availability"}, CreatedByID: ann.ID, CreatedAt: now}
	must(t, s.Integrations().Create(ctx, &in, []byte("tok-cal")))
	other := model.Integration{Name: "Other", CreatedByID: ann.ID, CreatedAt: now.Add(time.Second)}
	must(t, s.Integrations().Create(ctx, &other, []byte("tok-other")))
	wantErr(t, s.Integrations().Create(ctx, &model.Integration{Name: "Dup", CreatedByID: ann.ID}, []byte("tok-cal")), model.ErrAlreadyExists)

	got, err := s.Integrations().ByTokenHash(ctx, []byte("tok-cal"))
	must(t, err)
	eq(t, "id", got.ID, in.ID)
	eq(t, "allows", len(got.Allows) == 1 && got.Allows[0] == "availability", true)
	_, err = s.Integrations().ByTokenHash(ctx, []byte("nope"))
	wantErr(t, err, model.ErrNotFound)
	list, err := s.Integrations().List(ctx)
	must(t, err)
	eq(t, "count", len(list), 2)
	eq(t, "newest first", list[0].ID, other.ID)
	eq(t, "allows is never null", list[0].Allows != nil, true)
	must(t, s.Integrations().Used(ctx, in.ID, now.Add(time.Minute)))
	must(t, s.Integrations().Revoke(ctx, in.ID, now.Add(time.Hour)))
	must(t, s.Integrations().Revoke(ctx, in.ID, now.Add(2*time.Hour)))
	got, err = s.Integrations().Get(ctx, in.ID)
	must(t, err)
	eq(t, "last used", got.LastUsedAt, now.Add(time.Minute))
	eq(t, "the first revocation's time is kept", got.RevokedAt, now.Add(time.Hour))
	wantErr(t, s.Integrations().Revoke(ctx, model.NewID[string](), now), model.ErrNotFound)
}

func testDevices(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	now := model.Now()
	ses := model.Session{AccountID: ann.ID, Kind: model.SessionApp, Label: "Pixel"}
	must(t, s.Sessions().Create(ctx, &ses, []byte("tok"), []byte("ref")))
	d := model.Device{AccountID: ann.ID, SessionID: ses.ID, Name: "Pixel", Platform: "android", BoxKey: []byte("box")}
	must(t, s.Devices().Create(ctx, &d))
	got, found, err := s.Devices().BySession(ctx, ses.ID)
	must(t, err)
	eq(t, "found", found, true)
	eq(t, "id", got.ID, d.ID)
	eq(t, "platform", got.Platform, "android")
	_, found, _ = s.Devices().BySession(ctx, model.NewID[model.SessionID]())
	eq(t, "no device", found, false)
	got, found, err = s.Devices().RetireForSession(ctx, ses.ID, now)
	must(t, err)
	eq(t, "retired", found, true)
	eq(t, "retired at", got.RetiredAt, now)
	_, found, _ = s.Devices().RetireForSession(ctx, ses.ID, now)
	eq(t, "retired once", found, false)
}

// testSessionStates covers completing a pending session and signing
// everyone out (docs/AUTH.md sections 9.3 and 8).
func testSessionStates(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	now := model.Now()
	mk := func(account model.AccountID, kind, token string, pending bool) model.Session {
		ses := model.Session{AccountID: account, Kind: kind, SignedInBy: "oidc", PendingSecondFactor: pending, CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute)}
		must(t, s.Sessions().Create(ctx, &ses, []byte(token), nil))
		return ses
	}
	pending := mk(ann.ID, model.SessionWeb, "pending", true)
	ok, err := s.Sessions().Complete(ctx, pending.ID, []byte("complete"), "totp", now.Add(24*time.Hour), now)
	must(t, err)
	eq(t, "completed", ok, true)
	_, err = s.Sessions().ByTokenHash(ctx, []byte("pending"))
	wantErr(t, err, model.ErrNotFound) // the pending token stops working
	got, err := s.Sessions().ByTokenHash(ctx, []byte("complete"))
	must(t, err)
	eq(t, "no longer pending", got.PendingSecondFactor, false)
	eq(t, "proof", got.SecondFactor, "totp")
	eq(t, "end", got.ExpiresAt, now.Add(24*time.Hour))
	eq(t, "fresh", got.FreshAt, now)
	ok, _ = s.Sessions().Complete(ctx, pending.ID, []byte("again"), "totp", now, now)
	eq(t, "completed once", ok, false)
	gone := mk(ann.ID, model.SessionWeb, "revoked-pending", true)
	must(t, s.Sessions().Revoke(ctx, gone.ID, now))
	ok, _ = s.Sessions().Complete(ctx, gone.ID, []byte("never"), "totp", now, now)
	eq(t, "a revoked session is not completed", ok, false)

	web := mk(bob.ID, model.SessionWeb, "bob-web", false)
	app := mk(bob.ID, model.SessionApp, "bob-app", false)
	pat := mk(bob.ID, model.SessionPersonal, "bob-pat", false)
	refs, err := s.Sessions().RevokeKinds(ctx, []string{model.SessionWeb, model.SessionApp}, pending.ID, now)
	must(t, err)
	ended := map[model.SessionID]model.AccountID{}
	for _, r := range refs {
		ended[r.ID] = r.AccountID
	}
	eq(t, "two ended", len(ended), 2)
	eq(t, "web ended", ended[web.ID], bob.ID)
	eq(t, "app ended", ended[app.ID], bob.ID)
	left, err := s.Sessions().Get(ctx, pat.ID)
	must(t, err)
	eq(t, "personal tokens are left alone", left.RevokedAt.IsZero(), true)
	mine, _ := s.Sessions().Get(ctx, pending.ID)
	eq(t, "the caller's own is kept", mine.RevokedAt.IsZero(), true)
	refs, err = s.Sessions().RevokeKinds(ctx, nil, "", now)
	must(t, err)
	eq(t, "no kinds, nothing", len(refs), 0)
}
