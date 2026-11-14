package testkit

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/descope/virtualwebauthn"
	dsig "github.com/russellhaering/goxmldsig"

	"github.com/tikaydaes/churr/internal/model"
)

// This file gives tests what sign-in needs beyond a passkey: identity
// providers that run in the test process (an OIDC provider over TLS and a
// SAML signer), authenticator-app codes, requests that follow no redirect,
// and a few steps of the passkey API that the first helpers did not need.

// idpHTTPClient is the client test servers use to reach identity providers.
// Every httptest TLS server presents the same certificate, so trusting it
// once covers every provider a test starts.
var idpHTTPClient = sync.OnceValue(func() *http.Client {
	probe := httptest.NewUnstartedServer(http.NotFoundHandler())
	probe.StartTLS()
	defer probe.Close()
	pool := x509.NewCertPool()
	pool.AddCert(probe.Certificate())
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
})

// Page is the answer to a request made without following redirects and
// without assuming JSON: status, headers and the body as text.
type Page struct {
	Status int
	Header http.Header
	Body   string
}

// Location is where the answer redirects to.
func (p Page) Location() string { return p.Header.Get("Location") }

// JSON decodes the body.
func (p Page) JSON() map[string]any {
	out := map[string]any{}
	json.Unmarshal([]byte(p.Body), &out)
	return out
}

// Fetch sends a request as this browser (or script) and returns the answer
// as it is: no redirect is followed. form, if not nil, is sent as a form
// post. path may be a path on the server or a whole address.
func (c *Client) Fetch(method, path string, form url.Values) Page {
	c.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	target := path
	if strings.HasPrefix(path, "/") {
		target = c.ts.URL + path
	}
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		c.t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	hc := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if c.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.Bearer)
	} else {
		hc.Jar = c.http.Jar
		if !c.NoOrigin && method != "GET" {
			req.Header.Set("Origin", c.ts.Origin)
		}
	}
	resp, err := hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return Page{Status: resp.StatusCode, Header: resp.Header, Body: string(raw)}
}

// Cookie returns the value of one of this browser's cookies for a path on
// the server, or "".
func (c *Client) Cookie(name, path string) string {
	u, _ := url.Parse(c.ts.URL + path)
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == name {
			return ck.Value
		}
	}
	return ""
}

// App returns a client that sends an app's access token.
func (ts *TestServer) App(accessToken string) *Client {
	return &Client{ts: ts, t: ts.t, http: &http.Client{}, Bearer: accessToken}
}

// CreatePasskey answers a creation ceremony with a new passkey on this
// browser's authenticator, and returns the body for the finish request.
func (c *Client) CreatePasskey(begin map[string]any) map[string]any {
	c.t.Helper()
	if len(c.auth.Credentials) > 0 {
		// A further passkey for an account that has one already.
		c.cred = virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	}
	return c.create(begin)
}

// Assert answers an assertion ceremony (a sign-in or a passkey check) with
// this browser's passkey, and returns the body for the finish request.
func (c *Client) Assert(begin map[string]any) map[string]any {
	c.t.Helper()
	raw, _ := json.Marshal(map[string]any{"publicKey": begin["public_key"]})
	opts, err := virtualwebauthn.ParseAssertionOptions(string(raw))
	if err != nil {
		c.t.Fatal(err)
	}
	resp := virtualwebauthn.CreateAssertionResponse(c.ts.rp, c.auth, c.cred, *opts)
	return map[string]any{"ceremony_id": begin["ceremony_id"], "credential": json.RawMessage(resp)}
}

// Check passes a passkey check on this browser's session, which marks it
// freshly checked or completes a pending sign-in. It returns the answer.
func (c *Client) Check() map[string]any {
	c.t.Helper()
	begin := c.Must(200, "POST", "/api/v1/checks/passkey/begin", nil)
	return c.Must(200, "POST", "/api/v1/checks/passkey/finish", c.Assert(begin))
}

// SessionID is the identifier of the session this client is using.
func (c *Client) SessionID() model.SessionID {
	c.t.Helper()
	out := c.Must(200, "GET", "/api/v1/me/sessions", nil)
	for _, s := range out["sessions"].([]any) {
		if m := s.(map[string]any); m["current"] == true {
			return model.SessionID(m["id"].(string))
		}
	}
	c.t.Fatal("no current session in the list")
	return ""
}

// Stale makes this client's session look as if its last passkey or
// authenticator check was long ago, so the next operation that needs a
// fresh check is refused.
func (c *Client) Stale() {
	c.t.Helper()
	if err := c.ts.Server.Store.Sessions().SetFresh(context.Background(), c.SessionID(), model.Now().Add(-time.Hour)); err != nil {
		c.t.Fatal(err)
	}
}

// Exec runs one SQL statement straight on the server's database, with "?"
// placeholders, for state the API cannot make: a ban before bans are built,
// or a time in the past.
func (ts *TestServer) Exec(query string, args ...any) {
	ts.t.Helper()
	hook, ok := ts.Server.Store.(interface {
		ExecForTest(ctx context.Context, query string, args ...any) (int64, error)
	})
	if !ok {
		ts.t.Fatal("the store has no test hook for raw statements")
	}
	if _, err := hook.ExecForTest(context.Background(), query, args...); err != nil {
		ts.t.Fatal(err)
	}
}

// Set stores a server setting directly.
func (ts *TestServer) Set(key string, value any) {
	ts.t.Helper()
	if err := ts.Server.Store.Settings().Set(context.Background(), key, value, ""); err != nil {
		ts.t.Fatal(err)
	}
}

// TOTPCode is the authenticator-app code for a base32 secret, for the
// current 30-second step plus offset steps. It waits out the last moments
// of a step, so the server sees the same step the test did.
func TOTPCode(secret string, offset int64) string {
	for time.Now().Unix()%30 >= 28 {
		time.Sleep(200 * time.Millisecond)
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		panic(err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(time.Now().Unix()/30+offset))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7fffffff)%1000000)
}

// --- An OIDC provider ---

// FakeIdP is an OpenID Connect provider running in the test process over
// TLS: discovery, a key set and a token endpoint. A test plays the person
// at the provider by calling Approve with the address the server sent the
// browser to.
type FakeIdP struct {
	t   testing.TB
	srv *httptest.Server
	// Issuer is the provider's address.
	Issuer                 string
	ClientID, ClientSecret string
	key                    *rsa.PrivateKey

	mu    sync.Mutex
	codes map[string]idpCode
	// Requests counts the token requests the provider answered.
	Requests int
}

type idpCode struct {
	user      IdPUser
	nonce     string
	challenge string
	redirect  string
}

// IdPUser is who the provider says signed in, and how a test bends the ID
// token it issues.
type IdPUser struct {
	Subject, Username, Name string
	Email                   string
	EmailVerified           bool
	// Groups is the group claim; nil leaves the claim out.
	Groups []string
	AMR    []string
	ACR    string
	// Alg signs the ID token another way: "none" or "HS256".
	Alg string
	// Tamper changes the claims just before the token is signed.
	Tamper func(claims map[string]any)
}

// NewIdP starts an OIDC provider.
func NewIdP(t testing.TB) *FakeIdP {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &FakeIdP{t: t, ClientID: "churr-test", ClientSecret: "s3cret-for-tests", key: key, codes: map[string]idpCode{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": p.Issuer, "authorization_endpoint": p.Issuer + "/authorize", "token_endpoint": p.Issuer + "/token",
			"jwks_uri": p.Issuer + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_basic"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
		b64 := base64.RawURLEncoding.EncodeToString
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
			"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "test-key",
			"n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("POST /token", p.token)
	p.srv = httptest.NewTLSServer(mux)
	p.Issuer = p.srv.URL
	t.Cleanup(p.srv.Close)
	return p
}

// Approve plays the person signing in at the provider. location is where
// the server sent the browser; the result is where the provider sends it
// back, with a code and the state.
func (p *FakeIdP) Approve(location string, user IdPUser) string {
	p.t.Helper()
	u, err := url.Parse(location)
	if err != nil || !strings.HasPrefix(location, p.Issuer+"/authorize?") {
		p.t.Fatalf("the server did not send the browser to the provider: %q", location)
	}
	q := u.Query()
	if q.Get("client_id") != p.ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" ||
		len(q.Get("code_challenge")) < 43 || q.Get("state") == "" || q.Get("nonce") == "" || !strings.Contains(q.Get("scope"), "openid") {
		p.t.Fatalf("the authorization request is not what OIDC with PKCE needs: %v", q)
	}
	code := make([]byte, 16)
	rand.Read(code)
	c := base64.RawURLEncoding.EncodeToString(code)
	p.mu.Lock()
	p.codes[c] = idpCode{user: user, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
	p.mu.Unlock()
	back, _ := url.Parse(q.Get("redirect_uri"))
	bq := url.Values{"code": {c}, "state": {q.Get("state")}}
	back.RawQuery = bq.Encode()
	return back.String()
}

func (p *FakeIdP) token(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	idDecoded, _ := url.QueryUnescape(id)
	secretDecoded, _ := url.QueryUnescape(secret)
	p.mu.Lock()
	c, found := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code"))
	p.Requests++
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if idDecoded != p.ClientID || secretDecoded != p.ClientSecret || !found || r.PostForm.Get("redirect_uri") != c.redirect ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"access_token": "at-" + r.PostForm.Get("code"), "token_type": "Bearer", "expires_in": 300,
		"id_token": p.idToken(c)})
}

func (p *FakeIdP) idToken(c idpCode) string {
	now := time.Now()
	u := c.user
	claims := map[string]any{"iss": p.Issuer, "sub": u.Subject, "aud": p.ClientID, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "nonce": c.nonce}
	if u.Username != "" {
		claims["preferred_username"] = u.Username
	}
	if u.Name != "" {
		claims["name"] = u.Name
	}
	if u.Email != "" {
		claims["email"], claims["email_verified"] = u.Email, u.EmailVerified
	}
	if u.Groups != nil {
		claims["groups"] = u.Groups
	}
	if u.AMR != nil {
		claims["amr"] = u.AMR
	}
	if u.ACR != "" {
		claims["acr"] = u.ACR
	}
	if u.Tamper != nil {
		u.Tamper(claims)
	}
	alg := u.Alg
	if alg == "" {
		alg = "RS256"
	}
	b64 := base64.RawURLEncoding.EncodeToString
	hb, _ := json.Marshal(map[string]any{"alg": alg, "kid": "test-key", "typ": "JWT"})
	cb, _ := json.Marshal(claims)
	signed := b64(hb) + "." + b64(cb)
	var sig []byte
	switch alg {
	case "none":
	case "HS256":
		// The classic confusion: an HMAC keyed with something public.
		mac := hmac.New(sha256.New, []byte(p.ClientSecret))
		mac.Write([]byte(signed))
		sig = mac.Sum(nil)
	default:
		sum := sha256.Sum256([]byte(signed))
		sig, _ = rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, sum[:])
	}
	return signed + "." + b64(sig)
}

// --- A SAML identity provider ---

// FakeSAML is a SAML identity provider that exists only as a key, a
// certificate and the ability to write responses. A test reads the request
// the server made out of its redirect, builds a response, bends it if it
// wants a hostile one, and posts it to the server itself.
type FakeSAML struct {
	t testing.TB
	// EntityID and SSOURL are what an admin enters for this provider.
	EntityID, SSOURL string
	// CertPEM is the provider's signing certificate.
	CertPEM string
	key     *rsa.PrivateKey
	cert    []byte
}

// NewSAML makes a SAML identity provider with a fresh key.
func NewSAML(t testing.TB) *FakeSAML {
	key, der := samlKey(t, "fake-idp")
	return &FakeSAML{t: t, EntityID: "https://idp.example.test/saml", SSOURL: "https://idp.example.test/sso", key: key, cert: der,
		CertPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
}

func samlKey(t testing.TB, name string) (*rsa.PrivateKey, []byte) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return key, der
}

// GetKeyPair makes the provider a key store for the signing library.
func (f *FakeSAML) GetKeyPair() (*rsa.PrivateKey, []byte, error) { return f.key, f.cert, nil }

// SAMLRequest is the AuthnRequest the server sent, read out of its redirect.
type SAMLRequest struct {
	ID, ACS, Issuer, RelayState, Destination string
}

// Request reads the AuthnRequest out of the address the server sent the
// browser to.
func (f *FakeSAML) Request(location string) SAMLRequest {
	f.t.Helper()
	u, err := url.Parse(location)
	if err != nil || !strings.HasPrefix(location, f.SSOURL+"?") {
		f.t.Fatalf("the server did not send the browser to the provider: %q", location)
	}
	raw, err := base64.StdEncoding.DecodeString(u.Query().Get("SAMLRequest"))
	if err != nil {
		f.t.Fatal(err)
	}
	xmlBytes, err := io.ReadAll(flate.NewReader(bytes.NewReader(raw)))
	if err != nil {
		f.t.Fatal(err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(xmlBytes); err != nil {
		f.t.Fatal(err)
	}
	root := doc.Root()
	req := SAMLRequest{ID: root.SelectAttrValue("ID", ""), ACS: root.SelectAttrValue("AssertionConsumerServiceURL", ""),
		Destination: root.SelectAttrValue("Destination", ""), RelayState: u.Query().Get("RelayState")}
	for _, c := range root.ChildElements() {
		if c.Tag == "Issuer" {
			req.Issuer = c.Text()
		}
	}
	if req.ID == "" || req.ACS == "" || req.Issuer == "" || req.RelayState == "" {
		f.t.Fatalf("the AuthnRequest is incomplete: %+v", req)
	}
	return req
}

// SAMLUser is who the provider says signed in.
type SAMLUser struct {
	NameID string
	// NameIDFormat defaults to the persistent format.
	NameIDFormat string
	Attributes   map[string][]string
	// AuthnContext defaults to password over a protected transport.
	AuthnContext string
}

// SAMLDoc is a response under construction: a test changes its elements
// before signing or encoding it.
type SAMLDoc struct {
	f                   *FakeSAML
	Response, Assertion *etree.Element
}

func samlInstant(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// Build writes an unsigned response answering req for user: every field as
// a correct provider would set it.
func (f *FakeSAML) Build(req SAMLRequest, user SAMLUser) *SAMLDoc {
	now := time.Now()
	id := func() string {
		b := make([]byte, 16)
		rand.Read(b)
		return "_" + fmt.Sprintf("%x", b)
	}
	format := user.NameIDFormat
	if format == "" {
		format = "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"
	}
	actx := user.AuthnContext
	if actx == "" {
		actx = "urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport"
	}
	resp := etree.NewElement("samlp:Response")
	resp.CreateAttr("xmlns:samlp", "urn:oasis:names:tc:SAML:2.0:protocol")
	resp.CreateAttr("xmlns:saml", "urn:oasis:names:tc:SAML:2.0:assertion")
	resp.CreateAttr("ID", id())
	resp.CreateAttr("Version", "2.0")
	resp.CreateAttr("IssueInstant", samlInstant(now))
	resp.CreateAttr("Destination", req.ACS)
	resp.CreateAttr("InResponseTo", req.ID)
	resp.CreateElement("saml:Issuer").SetText(f.EntityID)
	resp.CreateElement("samlp:Status").CreateElement("samlp:StatusCode").CreateAttr("Value", "urn:oasis:names:tc:SAML:2.0:status:Success")

	a := etree.NewElement("saml:Assertion")
	a.CreateAttr("xmlns:saml", "urn:oasis:names:tc:SAML:2.0:assertion")
	a.CreateAttr("ID", id())
	a.CreateAttr("Version", "2.0")
	a.CreateAttr("IssueInstant", samlInstant(now))
	a.CreateElement("saml:Issuer").SetText(f.EntityID)
	subj := a.CreateElement("saml:Subject")
	nameID := subj.CreateElement("saml:NameID")
	nameID.CreateAttr("Format", format)
	nameID.SetText(user.NameID)
	conf := subj.CreateElement("saml:SubjectConfirmation")
	conf.CreateAttr("Method", "urn:oasis:names:tc:SAML:2.0:cm:bearer")
	data := conf.CreateElement("saml:SubjectConfirmationData")
	data.CreateAttr("InResponseTo", req.ID)
	data.CreateAttr("Recipient", req.ACS)
	data.CreateAttr("NotOnOrAfter", samlInstant(now.Add(5*time.Minute)))
	cond := a.CreateElement("saml:Conditions")
	cond.CreateAttr("NotBefore", samlInstant(now.Add(-time.Minute)))
	cond.CreateAttr("NotOnOrAfter", samlInstant(now.Add(5*time.Minute)))
	cond.CreateElement("saml:AudienceRestriction").CreateElement("saml:Audience").SetText(req.Issuer)
	stmt := a.CreateElement("saml:AuthnStatement")
	stmt.CreateAttr("AuthnInstant", samlInstant(now))
	stmt.CreateElement("saml:AuthnContext").CreateElement("saml:AuthnContextClassRef").SetText(actx)
	if len(user.Attributes) > 0 {
		as := a.CreateElement("saml:AttributeStatement")
		for name, values := range user.Attributes {
			at := as.CreateElement("saml:Attribute")
			at.CreateAttr("Name", name)
			for _, v := range values {
				at.CreateElement("saml:AttributeValue").SetText(v)
			}
		}
	}
	resp.AddChild(a)
	return &SAMLDoc{f: f, Response: resp, Assertion: a}
}

// SignWith signs an element with an enveloped signature and returns the
// signed copy. sha1 uses the algorithm the server must refuse.
func (f *FakeSAML) SignWith(ks dsig.X509KeyStore, el *etree.Element, sha1 bool) *etree.Element {
	f.t.Helper()
	ctx := dsig.NewDefaultSigningContext(ks)
	ctx.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	method := dsig.RSASHA256SignatureMethod
	if sha1 {
		ctx.Hash, method = crypto.SHA1, dsig.RSASHA1SignatureMethod
	}
	if err := ctx.SetSignatureMethod(method); err != nil {
		f.t.Fatal(err)
	}
	signed, err := ctx.SignEnveloped(el)
	if err != nil {
		f.t.Fatal(err)
	}
	return signed
}

// SignAssertion signs the assertion as a correct provider does, replacing
// the unsigned one inside the response.
func (d *SAMLDoc) SignAssertion() *SAMLDoc {
	signed := d.f.SignWith(d.f, d.Assertion, false)
	d.Replace(signed)
	return d
}

// Replace puts another element where the assertion is.
func (d *SAMLDoc) Replace(a *etree.Element) {
	d.Response.RemoveChild(d.Assertion)
	d.Response.AddChild(a)
	d.Assertion = a
}

// Encode writes the response as the SAMLResponse form field.
func (d *SAMLDoc) Encode() string {
	doc := etree.NewDocument()
	doc.SetRoot(d.Response.Copy())
	b, err := doc.WriteToBytes()
	if err != nil {
		d.f.t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// XML writes the response as text, for tests that bend the text itself.
func (d *SAMLDoc) XML() string {
	doc := etree.NewDocument()
	doc.SetRoot(d.Response.Copy())
	s, err := doc.WriteToString()
	if err != nil {
		d.f.t.Fatal(err)
	}
	return s
}

// OtherKey is a key store with a key the server was never told about, for
// responses signed by someone who is not the provider.
func (f *FakeSAML) OtherKey() dsig.X509KeyStore {
	key, der := samlKey(f.t, "not-the-idp")
	return &otherKey{key, der}
}

type otherKey struct {
	key  *rsa.PrivateKey
	cert []byte
}

func (o *otherKey) GetKeyPair() (*rsa.PrivateKey, []byte, error) { return o.key, o.cert, nil }

// CopyPasskey gives this browser the passkey of another, as a synced
// passkey is on two devices.
func (c *Client) CopyPasskey(from *Client) {
	c.auth, c.cred, c.ID, c.Username = from.auth, from.cred, from.ID, from.Username
}

// SetCookie puts a cookie in this browser's jar, for tests that replay one.
func (c *Client) SetCookie(name, value string) {
	u, _ := url.Parse(c.ts.URL)
	c.http.Jar.SetCookies(u, []*http.Cookie{{Name: name, Value: value, Path: "/"}})
}
