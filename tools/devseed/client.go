package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"github.com/descope/virtualwebauthn"
)

// world is the server being seeded and the people in it.
type world struct {
	url, origin, hostname, setupLink string
	people                           map[string]*client
}

// client is one account: a browser with a cookie jar and a virtual passkey
// (how people are made), or a bearer token (the bot).
type client struct {
	w        *world
	http     *http.Client
	bearer   string
	auth     virtualwebauthn.Authenticator
	cred     virtualwebauthn.Credential
	id       string
	username string
}

func (w *world) browser() *client {
	jar, _ := cookiejar.New(nil)
	return &client{w: w, http: &http.Client{Jar: jar,
		// The OAuth approval answers with a redirect to the app's
		// address, which is read and not followed.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		auth: virtualwebauthn.NewAuthenticator(), cred: virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)}
}

// send makes one request and returns the status, the headers and the body.
func (c *client) send(method, path, contentType string, body io.Reader) (int, http.Header, []byte, error) {
	req, err := http.NewRequest(method, c.w.url+path, body)
	if err != nil {
		return 0, nil, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	} else {
		// A browser sends the page's origin with every request that
		// could change something, and the server checks it.
		req.Header.Set("Origin", c.w.origin)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, raw, err
}

// do sends JSON and decodes the JSON answer, failing unless the status is
// one of those wanted.
func (c *client) do(method, path string, body any, want ...int) (map[string]any, error) {
	var rd io.Reader
	ct := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd, ct = bytes.NewReader(b), "application/json"
	}
	status, _, raw, err := c.send(method, path, ct, rd)
	if err != nil {
		return nil, err
	}
	ok := len(want) == 0 && status < 300
	for _, s := range want {
		ok = ok || s == status
	}
	if !ok {
		return nil, fmt.Errorf("%s %s as %s: %d %s", method, path, c.username, status, raw)
	}
	out := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("%s %s: %v", method, path, err)
		}
	}
	return out, nil
}

// upload posts a multipart form with one file part and plain fields.
func (c *client) upload(path, part, filename string, content []byte, fields map[string]string) (map[string]any, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile(part, filename)
	fw.Write(content)
	mw.Close()
	status, _, raw, err := c.send("POST", path, mw.FormDataContentType(), &buf)
	if err != nil {
		return nil, err
	}
	if status != 201 {
		return nil, fmt.Errorf("upload %s: %d %s", path, status, raw)
	}
	out := map[string]any{}
	return out, json.Unmarshal(raw, &out)
}

// passkey answers a passkey creation ceremony with this browser's virtual
// authenticator and returns the body of the matching finish request.
func (c *client) passkey(begin map[string]any) (map[string]any, error) {
	pk := begin["public_key"].(map[string]any)
	raw, _ := json.Marshal(map[string]any{"publicKey": pk})
	opts, err := virtualwebauthn.ParseAttestationOptions(string(raw))
	if err != nil {
		return nil, err
	}
	handle := pk["user"].(map[string]any)["id"].(string)
	c.auth.Options.UserHandle, _ = base64.RawURLEncoding.DecodeString(handle)
	rp := virtualwebauthn.RelyingParty{Name: "Churr", ID: c.w.hostname, Origin: c.w.origin}
	resp := virtualwebauthn.CreateAttestationResponse(rp, c.auth, c.cred, *opts)
	c.auth.AddCredential(c.cred)
	return map[string]any{"ceremony_id": begin["ceremony_id"], "credential": json.RawMessage(resp), "passkey_name": "devseed passkey", "prf_enabled": true}, nil
}

func (c *client) signedIn(out map[string]any, display string) {
	acc := out["account"].(map[string]any)
	c.id = acc["id"].(string)
	c.username = acc["username"].(string)
}

// setup runs first-run setup, making the owner.
func (w *world) setup(username, display string) (*client, error) {
	c := w.browser()
	c.username = username
	token := w.setupLink[strings.LastIndex(w.setupLink, "/")+1:]
	begin, err := c.do("POST", "/api/v1/setup/begin", map[string]any{"setup_token": token, "username": username, "display_name": display}, 200)
	if err != nil {
		return nil, err
	}
	fin, err := c.passkey(begin)
	if err != nil {
		return nil, err
	}
	out, err := c.do("POST", "/api/v1/setup/finish", fin, 201)
	if err != nil {
		return nil, err
	}
	c.signedIn(out, display)
	return c, nil
}

// register makes a member through an invite and the real registration.
func (w *world) register(username, display, invite string) (*client, error) {
	c := w.browser()
	c.username = username
	begin, err := c.do("POST", "/api/v1/passkey/register/begin", map[string]any{"invite_code": invite, "username": username, "display_name": display}, 200)
	if err != nil {
		return nil, err
	}
	fin, err := c.passkey(begin)
	if err != nil {
		return nil, err
	}
	out, err := c.do("POST", "/api/v1/passkey/register/finish", fin, 201)
	if err != nil {
		return nil, err
	}
	c.signedIn(out, display)
	return c, nil
}

// appTokens signs this account in as an app, the way an app does it
// (docs/AUTH.md section 11.2): the browser approves the device on the
// authorization page and the code is exchanged with its PKCE verifier.
func (c *client) appTokens(deviceName string) (account, error) {
	var a account
	vb := make([]byte, 32)
	rand.Read(vb)
	verifier := base64.RawURLEncoding.EncodeToString(vb)
	sum := sha256.Sum256([]byte(verifier))
	redirect := "http://127.0.0.1:49152/callback"
	form := url.Values{"decision": {"approve"}, "response_type": {"code"}, "client_id": {"churr-app"},
		"redirect_uri": {redirect}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"}, "state": {"devseed"}, "device_name": {deviceName}, "platform": {"macos"}}
	status, hdr, raw, err := c.send("POST", "/oauth/authorize", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return a, err
	}
	if status != 302 {
		return a, fmt.Errorf("approving the app for %s: %d %s", c.username, status, raw)
	}
	back, err := url.Parse(hdr.Get("Location"))
	if err != nil || back.Query().Get("code") == "" {
		return a, fmt.Errorf("approving the app for %s: redirected to %q", c.username, hdr.Get("Location"))
	}
	// The token endpoint is called by the app, not the browser: no
	// cookie, no Origin.
	resp, err := http.PostForm(c.w.url+"/oauth/token", url.Values{"grant_type": {"authorization_code"},
		"code": {back.Query().Get("code")}, "code_verifier": {verifier}, "redirect_uri": {redirect}})
	if err != nil {
		return a, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return a, fmt.Errorf("exchanging the code for %s: %d %s", c.username, resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, &a); err != nil {
		return a, err
	}
	a.Username, a.ID = c.username, c.id
	return a, nil
}
