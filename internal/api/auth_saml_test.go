package api_test

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Company sign-in over SAML 2.0 against an identity provider made in the
// test: a good response signs in, and every hostile response of
// docs/AUTH.md section 7.3 is refused.

func samlSetup(t *testing.T, ts *testkit.TestServer, config map[string]any, extra map[string]any) (*testkit.FakeSAML, string) {
	t.Helper()
	idp := testkit.NewSAML(t)
	cfg := map[string]any{"entity_id": idp.EntityID, "sso_url": idp.SSOURL, "certificates": []string{idp.CertPEM}}
	for k, v := range config {
		cfg[k] = v
	}
	body := map[string]any{"kind": "saml", "name": "Acme SAML", "config": cfg}
	for k, v := range extra {
		body[k] = v
	}
	p := at(ts.Owner.Must(201, "POST", "/api/v1/admin/sso", body), "provider")
	ts.Owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"auth.member_sign_in": "either", "registration.mode": "open"})
	return idp, p["id"].(string)
}

// samlPost posts a response to the provider's callback as the browser.
func samlPost(c *testkit.Client, provider, response, relayState string) testkit.Page {
	c.NoOrigin = true // the provider's page posts it, cross-site
	defer func() { c.NoOrigin = false }()
	return c.Fetch("POST", "/api/v1/sso/"+provider+"/acs", url.Values{"SAMLResponse": {response}, "RelayState": {relayState}})
}

func TestSamlSignIn(t *testing.T) {
	ts := testkit.NewServer(t)
	idp, pid := samlSetup(t, ts, map[string]any{
		"username_attribute": "uid", "name_attribute": "displayName", "mfa_contexts": []string{"urn:example:mfa"},
	}, map[string]any{"group_claim": "memberOf", "admin_group": "churr-admins"})

	// The provider as an admin sees it, and this server's metadata.
	p := at(ts.Owner.Must(200, "GET", "/api/v1/admin/sso/"+pid, nil), "provider")
	if p["callback_url"] != ts.Origin+"/api/v1/sso/"+pid+"/acs" || p["has_secret"] != true || !strings.Contains(at(p, "config")["sp_certificate"].(string), "BEGIN CERTIFICATE") {
		t.Fatalf("provider: %v", p)
	}
	md := ts.Browser().Fetch("GET", "/api/v1/sso/"+pid+"/metadata", nil)
	if md.Status != 200 || md.Header.Get("Content-Type") != "application/samlmetadata+xml" ||
		!strings.Contains(md.Body, `entityID="`+ts.Origin+"/api/v1/sso/"+pid+`/metadata"`) || !strings.Contains(md.Body, ts.Origin+"/api/v1/sso/"+pid+"/acs") {
		t.Fatalf("metadata: %d %s", md.Status, md.Body)
	}
	if out := ts.Owner.Must(200, "POST", "/api/v1/admin/sso/"+pid+"/test", nil); out["ok"] != true {
		t.Fatalf("test: %v", out)
	}
	// A SAML provider's config is checked.
	for _, cfg := range []map[string]any{
		{"entity_id": idp.EntityID, "sso_url": idp.SSOURL},                                                      // no certificate
		{"entity_id": idp.EntityID, "sso_url": "http://idp.example/sso", "certificates": []string{idp.CertPEM}}, // not https
		{"sso_url": idp.SSOURL, "certificates": []string{idp.CertPEM}},                                          // no entity id
		{"entity_id": idp.EntityID, "sso_url": idp.SSOURL, "certificates": []string{"not a certificate"}},
		{"metadata_xml": "<nonsense/>"},
	} {
		ts.Owner.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/sso", map[string]any{"kind": "saml", "name": "Bad", "config": cfg})
	}
	ts.Owner.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/admin/sso", map[string]any{"kind": "saml", "name": "Bad", "secret": "x",
		"config": map[string]any{"entity_id": idp.EntityID, "sso_url": idp.SSOURL, "certificates": []string{idp.CertPEM}}})

	// The request: sent by redirect, with a long random ID, this server
	// as issuer, the callback as consumer, and the handle as RelayState.
	c := ts.Browser()
	loc := ssoStart(t, c, pid, "?return_to=/after")
	req := idp.Request(loc)
	if len(req.ID) < 40 || req.ACS != ts.Origin+"/api/v1/sso/"+pid+"/acs" || req.Issuer != ts.Origin+"/api/v1/sso/"+pid+"/metadata" || req.Destination != idp.SSOURL {
		t.Fatalf("AuthnRequest: %+v", req)
	}
	if c.Cookie("churr_sso", "/api/v1/sso/"+pid+"/acs") == "" {
		t.Fatal("the binding cookie must be set for the callback")
	}

	// A good response: a first sign-in asks for the username, with the
	// provider's attribute filled in.
	user := testkit.SAMLUser{NameID: "saml-ann", Attributes: map[string][]string{"uid": {"Ann"}, "displayName": {"Ann Lee"}, "memberOf": {"staff", "churr-admins"}}}
	page := samlPost(c, pid, idp.Build(req, user).SignAssertion().Encode(), req.RelayState)
	if page.Status != 200 || !strings.Contains(page.Body, `value="ann"`) {
		t.Fatalf("registration page: %d %s", page.Status, page.Body)
	}
	page = c.Fetch("POST", "/sso/register", url.Values{"token": {hiddenToken.FindStringSubmatch(page.Body)[1]}, "username": {"ann"}})
	if page.Status != 200 || !strings.Contains(page.Body, `url=/after"`) {
		t.Fatalf("after registering: %d %s", page.Status, page.Body)
	}
	me := signedInAs(t, c, "ann")
	// The admin role came from the provider's group attribute.
	if me["role"] != "admin" || me["display_name"] != "Ann Lee" {
		t.Fatalf("account: %v", me)
	}
	ses := c.Must(200, "GET", "/api/v1/me/sessions", nil)["sessions"].([]any)[0].(map[string]any)
	if ses["signed_in_by"] != "saml" {
		t.Fatalf("session: %v", ses)
	}

	// The response cannot be replayed: its request was taken out of memory.
	replay := ts.Browser()
	if p := samlPost(replay, pid, idp.Build(req, user).SignAssertion().Encode(), req.RelayState); p.Status != 401 {
		t.Fatalf("a replayed response: %d", p.Status)
	}

	// SessionNotOnOrAfter earlier than 24 hours ahead ends the session then.
	c = ts.Browser()
	req = idp.Request(ssoStart(t, c, pid, ""))
	doc := idp.Build(req, user)
	end := time.Now().Add(2 * time.Hour).UTC()
	find(doc.Assertion, "AuthnStatement").CreateAttr("SessionNotOnOrAfter", end.Format("2006-01-02T15:04:05.000Z"))
	if p := samlPost(c, pid, doc.SignAssertion().Encode(), req.RelayState); p.Status != 200 {
		t.Fatalf("sign-in: %d %s", p.Status, p.Body)
	}
	ses = c.Must(200, "GET", "/api/v1/me/sessions", nil)["sessions"].([]any)[0].(map[string]any)
	exp, _ := time.Parse(time.RFC3339Nano, ses["expires_at"].(string))
	if d := exp.Sub(end); d > time.Second || d < -time.Second {
		t.Fatalf("the session ends at %v, want the provider's %v", exp, end)
	}

	// The provider's own multi-factor statement: its context class counts
	// only when it is one the admin configured, and only if ticked.
	ts.Owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"auth.second_factor": "required", "auth.second_factor_accepted": []string{"provider"}})
	c = ts.Browser()
	req = idp.Request(ssoStart(t, c, pid, ""))
	if p := samlPost(c, pid, idp.Build(req, user).SignAssertion().Encode(), req.RelayState); p.Status != 403 {
		t.Fatalf("no multi-factor statement: %d", p.Status)
	}
	c = ts.Browser()
	req = idp.Request(ssoStart(t, c, pid, ""))
	mfa := user
	mfa.AuthnContext = "urn:example:mfa"
	if p := samlPost(c, pid, idp.Build(req, mfa).SignAssertion().Encode(), req.RelayState); p.Status != 200 {
		t.Fatalf("with the statement: %d %s", p.Status, p.Body)
	}
	if s := c.Must(200, "GET", "/api/v1/me/sessions", nil)["sessions"].([]any)[0].(map[string]any); s["second_factor"] != "provider" {
		t.Fatalf("session: %v", s)
	}
}

// find returns the first element with this tag below el.
func find(el *etree.Element, tag string) *etree.Element {
	if el.Tag == tag {
		return el
	}
	for _, c := range el.ChildElements() {
		if f := find(c, tag); f != nil {
			return f
		}
	}
	return nil
}

func TestSamlRefusals(t *testing.T) {
	ts := testkit.NewServer(t)
	idp, pid := samlSetup(t, ts, nil, nil)
	good := testkit.SAMLUser{NameID: "saml-ann"}
	evil := testkit.SAMLUser{NameID: "saml-owner"}
	started := 0

	// attempt starts a sign-in, lets the case build its response, posts
	// it, and requires a refusal that leaves no session and says nothing
	// of why.
	attempt := func(name string, build func(req testkit.SAMLRequest) string) {
		t.Helper()
		if started++; started%15 == 0 {
			// Starting a sign-in is limited per address.
			ts = testkit.NewServer(t)
			idp, pid = samlSetup(t, ts, nil, nil)
		}
		c := ts.Browser()
		req := idp.Request(ssoStart(t, c, pid, ""))
		page := samlPost(c, pid, build(req), req.RelayState)
		if page.Status != 401 || !strings.Contains(page.Body, "Sign-in through Acme SAML failed.") || c.Cookie("churr_session", "/") != "" ||
			strings.Contains(page.Body, "Choose a username") {
			t.Fatalf("%s: got %d %s", name, page.Status, page.Body)
		}
	}
	// before signs a response after the case changed the unsigned assertion.
	before := func(change func(d *testkit.SAMLDoc, req testkit.SAMLRequest)) func(testkit.SAMLRequest) string {
		return func(req testkit.SAMLRequest) string {
			d := idp.Build(req, good)
			change(d, req)
			return d.SignAssertion().Encode()
		}
	}
	// after changes a correctly signed response.
	after := func(change func(d *testkit.SAMLDoc, req testkit.SAMLRequest)) func(testkit.SAMLRequest) string {
		return func(req testkit.SAMLRequest) string {
			d := idp.Build(req, good).SignAssertion()
			change(d, req)
			return d.Encode()
		}
	}
	encode := func(xml string) string { return base64.StdEncoding.EncodeToString([]byte(xml)) }
	past := time.Now().Add(-10 * time.Minute).UTC().Format("2006-01-02T15:04:05.000Z")
	future := time.Now().Add(10 * time.Minute).UTC().Format("2006-01-02T15:04:05.000Z")

	// The control: the same machinery with nothing bent signs in.
	c := ts.Browser()
	req := idp.Request(ssoStart(t, c, pid, ""))
	if p := samlPost(c, pid, after(func(*testkit.SAMLDoc, testkit.SAMLRequest) {})(req), req.RelayState); p.Status != 200 || !strings.Contains(p.Body, "Choose a username") {
		t.Fatalf("a good response: %d %s", p.Status, p.Body)
	}

	// Signatures.
	attempt("unsigned", func(req testkit.SAMLRequest) string { return idp.Build(req, good).Encode() })
	attempt("signed response with unsigned assertion", func(req testkit.SAMLRequest) string {
		d := idp.Build(req, good)
		d.Response = idp.SignWith(idp, d.Response, false)
		return d.Encode()
	})
	attempt("signature removed", after(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		// The signing library appends the signature without making it
		// a child in etree's sense, so it is taken out by hand.
		kept := d.Assertion.Child[:0]
		for _, tok := range d.Assertion.Child {
			if el, ok := tok.(*etree.Element); !ok || el.Tag != "Signature" {
				kept = append(kept, tok)
			}
		}
		if len(kept) == len(d.Assertion.Child) {
			t.Fatal("the test found no signature to remove")
		}
		d.Assertion.Child = kept
	}))
	attempt("signed by another key", func(req testkit.SAMLRequest) string {
		d := idp.Build(req, good)
		d.Replace(idp.SignWith(idp.OtherKey(), d.Assertion, false))
		return d.Encode()
	})
	attempt("signed with SHA-1", func(req testkit.SAMLRequest) string {
		d := idp.Build(req, good)
		d.Replace(idp.SignWith(idp, d.Assertion, true))
		return d.Encode()
	})
	attempt("changed after signing", after(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		find(d.Assertion, "NameID").SetText("saml-owner")
	}))
	attempt("an attribute added after signing", after(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		d.Assertion.CreateElement("saml:AttributeStatement").CreateElement("saml:Attribute").CreateAttr("Name", "memberOf")
	}))

	// Wrapping: a second assertion before, after and inside the signed one.
	attempt("wrapped: forged assertion before the signed one", after(func(d *testkit.SAMLDoc, req testkit.SAMLRequest) {
		d.Response.InsertChildAt(d.Assertion.Index(), idp.Build(req, evil).Assertion)
	}))
	attempt("wrapped: forged assertion after the signed one", after(func(d *testkit.SAMLDoc, req testkit.SAMLRequest) {
		d.Response.AddChild(idp.Build(req, evil).Assertion)
	}))
	attempt("wrapped: forged assertion inside the signed one", after(func(d *testkit.SAMLDoc, req testkit.SAMLRequest) {
		d.Assertion.AddChild(idp.Build(req, evil).Assertion)
	}))
	attempt("wrapped: signed assertion inside a forged one", after(func(d *testkit.SAMLDoc, req testkit.SAMLRequest) {
		signed := d.Assertion
		forged := idp.Build(req, evil).Assertion
		d.Replace(forged)
		forged.CreateElement("saml:Advice").AddChild(signed)
	}))
	attempt("wrapped: forged assertion with the signed one's ID, original tucked in the signature", after(func(d *testkit.SAMLDoc, req testkit.SAMLRequest) {
		signed := d.Assertion
		forged := idp.Build(req, evil).Assertion
		forged.CreateAttr("ID", signed.SelectAttrValue("ID", ""))
		sig := find(signed, "Signature").Copy()
		sig.CreateElement("ds:Object").AddChild(signed.Copy())
		forged.AddChild(sig)
		d.Replace(forged)
	}))
	attempt("duplicate IDs", before(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		d.Response.CreateAttr("ID", d.Assertion.SelectAttrValue("ID", ""))
	}))
	attempt("a reference to another element", func(req testkit.SAMLRequest) string {
		// A signature that is good, but over something else.
		d := idp.Build(req, good)
		other := idp.SignWith(idp, idp.Build(req, evil).Assertion, false)
		d.Assertion.AddChild(find(other, "Signature"))
		return d.Encode()
	})
	attempt("a reference with no URI", after(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		find(d.Assertion, "Reference").CreateAttr("URI", "")
	}))
	attempt("a comment inside the NameID", after(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		n := find(d.Assertion, "NameID")
		n.SetText("saml-")
		n.CreateComment("x")
		n.CreateText("ann")
	}))
	attempt("a document type declaration", func(req testkit.SAMLRequest) string {
		return encode(`<!DOCTYPE r [<!ENTITY x "saml-owner">]>` + idp.Build(req, good).SignAssertion().XML())
	})
	attempt("an encrypted assertion", after(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		d.Response.CreateElement("saml:EncryptedAssertion")
	}))
	attempt("not XML", func(testkit.SAMLRequest) string { return encode("<samlp:Response") })
	attempt("not base64", func(testkit.SAMLRequest) string { return "!!!" })
	attempt("too large", func(req testkit.SAMLRequest) string {
		d := idp.Build(req, testkit.SAMLUser{NameID: "saml-ann", Attributes: map[string][]string{"pad": {strings.Repeat("x", 300<<10)}}})
		return d.SignAssertion().Encode()
	})

	// Addressed to someone else, or answering something else.
	attempt("wrong audience", before(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		find(d.Assertion, "Audience").SetText("https://other.example/metadata")
	}))
	attempt("no audience", before(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		c := find(d.Assertion, "Conditions")
		c.RemoveChild(find(c, "AudienceRestriction"))
	}))
	attempt("wrong recipient", before(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		find(d.Assertion, "SubjectConfirmationData").CreateAttr("Recipient", "https://other.example/acs")
	}))
	attempt("wrong destination", before(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		d.Response.CreateAttr("Destination", "https://other.example/acs")
	}))
	attempt("wrong InResponseTo on the response", before(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		d.Response.CreateAttr("InResponseTo", "id-another")
	}))
	attempt("wrong InResponseTo on the confirmation", before(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		find(d.Assertion, "SubjectConfirmationData").CreateAttr("InResponseTo", "id-another")
	}))
	attempt("wrong issuer", before(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		find(d.Assertion, "Issuer").SetText("https://evil.example/saml")
	}))
	attempt("status not success", before(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		find(d.Response, "StatusCode").CreateAttr("Value", "urn:oasis:names:tc:SAML:2.0:status:Requester")
	}))

	// Time.
	attempt("expired", before(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		find(d.Assertion, "Conditions").CreateAttr("NotOnOrAfter", past)
	}))
	attempt("expired confirmation", before(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		find(d.Assertion, "SubjectConfirmationData").CreateAttr("NotOnOrAfter", past)
	}))
	attempt("not yet valid", before(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		find(d.Assertion, "Conditions").CreateAttr("NotBefore", future)
	}))

	// Content.
	attempt("a transient NameID", func(req testkit.SAMLRequest) string {
		u := good
		u.NameIDFormat = "urn:oasis:names:tc:SAML:2.0:nameid-format:transient"
		return idp.Build(req, u).SignAssertion().Encode()
	})
	attempt("no authentication statement", before(func(d *testkit.SAMLDoc, _ testkit.SAMLRequest) {
		d.Assertion.RemoveChild(find(d.Assertion, "AuthnStatement"))
	}))

	// The browser: an answer nobody asked for, and one given to another browser.
	c = ts.Browser()
	req = idp.Request(ssoStart(t, c, pid, ""))
	response := idp.Build(req, good).SignAssertion().Encode()
	if p := samlPost(c, pid, response, ""); p.Status != 401 {
		t.Fatalf("no RelayState: %d", p.Status)
	}
	if p := samlPost(c, pid, response, "made-up"); p.Status != 401 {
		t.Fatalf("unknown RelayState: %d", p.Status)
	}
	thief := ts.Browser()
	if p := samlPost(thief, pid, response, req.RelayState); p.Status != 401 || thief.Cookie("churr_session", "/") != "" {
		t.Fatalf("another browser: %d", p.Status)
	}
	// An OIDC callback does not take a SAML provider's answers.
	if p := c.Fetch("GET", "/api/v1/sso/"+pid+"/callback?state=x&code=y", nil); p.Status != 404 {
		t.Fatalf("the OIDC callback of a SAML provider: %d", p.Status)
	}
}
