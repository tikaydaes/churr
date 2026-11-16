package web

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

func get(t *testing.T, h http.Handler, path string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func testHandler() http.Handler {
	return Handler(Options{Origin: "https://chat.example.org", TLS: true,
		CommunityName: func(context.Context) string { return "Night Shift" }})
}

// Every page carries the headers of docs/CLIENTS.md section 19.
func TestHeaders(t *testing.T) {
	h := testHandler()
	w := get(t, h, "/login")
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("/login: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	want := "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; " +
		"connect-src 'self' wss://chat.example.org; manifest-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
	if got := w.Header().Get("Content-Security-Policy"); got != want {
		t.Errorf("policy of /login:\n got %s\nwant %s", got, want)
	}
	for k, v := range map[string]string{
		"X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer", "Cross-Origin-Opener-Policy": "same-origin",
		"X-Frame-Options": "DENY", "Cache-Control": "no-cache",
	} {
		if got := w.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if !strings.Contains(w.Header().Get("Strict-Transport-Security"), "max-age=") {
		t.Error("no Strict-Transport-Security although the server terminates TLS")
	}
	if p := w.Header().Get("Permissions-Policy"); !strings.Contains(p, "camera=()") || !strings.Contains(p, "microphone=()") {
		t.Errorf("Permissions-Policy outside the chat allows devices: %s", p)
	}

	// The web chat gets blob:, media and workers, and may use the devices.
	c := get(t, h, "/chat/")
	csp := c.Header().Get("Content-Security-Policy")
	for _, part := range []string{"img-src 'self' blob:", "media-src 'self' blob:", "worker-src 'self'", "script-src 'self'", "style-src 'self'"} {
		if !strings.Contains(csp, part) {
			t.Errorf("the chat's policy lacks %q: %s", part, csp)
		}
	}
	if strings.Contains(csp, "unsafe") {
		t.Errorf("the chat's policy allows something unsafe: %s", csp)
	}
	if p := c.Header().Get("Permissions-Policy"); !strings.Contains(p, "camera=(self)") || !strings.Contains(p, "display-capture=(self)") {
		t.Errorf("Permissions-Policy on /chat/: %s", p)
	}

	// Behind a proxy the header is the proxy's to send; a local trial
	// names ws: for the live connection.
	dev := Handler(Options{Origin: "http://localhost:8472"})
	d := get(t, dev, "/login")
	if d.Header().Get("Strict-Transport-Security") != "" {
		t.Error("Strict-Transport-Security sent without TLS")
	}
	if !strings.Contains(d.Header().Get("Content-Security-Policy"), "connect-src 'self' ws://localhost:8472;") {
		t.Errorf("local trial policy: %s", d.Header().Get("Content-Security-Policy"))
	}
}

func TestRoutes(t *testing.T) {
	h := testHandler()
	if w := get(t, h, "/"); w.Code != 302 || w.Header().Get("Location") != "/login" {
		t.Errorf("/ signed out: %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := get(t, h, "/", "Cookie", SessionCookie+"=churr_ses_x"); w.Code != 302 || w.Header().Get("Location") != "/chat/" {
		t.Errorf("/ signed in: %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := get(t, h, "/chat"); w.Code != 301 || w.Header().Get("Location") != "/chat/" {
		t.Errorf("/chat: %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := get(t, h, "/invite/abc_DEF-123"); w.Code != 302 || w.Header().Get("Location") != "/join/abc_DEF-123" {
		t.Errorf("/invite/…: %d %s", w.Code, w.Header().Get("Location"))
	}
	// Any address under an area is answered with that area's page.
	for path, marker := range map[string]string{
		"/setup/some-token": "/setup/setup.js", "/login": "/login/login.js", "/join/code": "/join/join.js", "/join": "/join/join.js",
		"/recover/token": "/recover/recover.js", "/unlock": "/unlock/unlock.js", "/account": "/account/account.js",
		"/account/sessions": "/account/account.js", "/admin": "/admin/admin.js", "/admin/people": "/admin/admin.js",
		"/chat/": "/chat/app.js", "/chat/c/0193a000-0000-7000-8000-000000000001": "/chat/app.js",
		"/themes/0193a000-0000-7000-8000-000000000002": "/themes/page.js", "/chat/themes/library": "/chat/app.js",
	} {
		w := get(t, h, path)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `src="`+marker+`"`) {
			t.Errorf("%s: status %d, or the page does not load %s", path, w.Code, marker)
		}
	}
	// A missing file is never answered with a page, and an HTML file is
	// reached only through its address.
	for _, path := range []string{"/chat/nothing.js", "/admin/missing.css", "/login/index.html", "/nowhere", "/shared/../web.go"} {
		if w := get(t, h, path); w.Code != 404 {
			t.Errorf("%s: %d, want 404", path, w.Code)
		}
	}
	for path, kind := range map[string]string{
		"/shared/base.css": "text/css", "/shared/dom.js": "text/javascript", "/shared/chevron.svg": "image/svg+xml",
		"/shared/fonts/archivo.woff2": "font/woff2", "/icons/icon-192.png": "image/png", "/favicon.ico": "image/png",
		"/shared/highlight/LICENSE": "text/plain", "/shared/emoji/emoji.json": "application/json",
	} {
		w := get(t, h, path)
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), kind) {
			t.Errorf("%s: %d %s, want %s", path, w.Code, w.Header().Get("Content-Type"), kind)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /login: %d", w.Code)
	}
}

// Files carry an ETag and no-cache, and a matching If-None-Match gets 304.
func TestETag(t *testing.T) {
	h := testHandler()
	w := get(t, h, "/shared/dom.js")
	tag := w.Header().Get("ETag")
	if tag == "" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("ETag %q, Cache-Control %q", tag, w.Header().Get("Cache-Control"))
	}
	if again := get(t, h, "/shared/dom.js", "If-None-Match", tag); again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Errorf("If-None-Match: %d with %d bytes", again.Code, again.Body.Len())
	}
}

func TestManifest(t *testing.T) {
	w := get(t, testHandler(), "/chat/manifest.webmanifest")
	var m struct {
		Name, Display, StartURL string
		Icons                   []struct{ Src, Sizes, Purpose string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Content-Type") != "application/manifest+json" || !strings.Contains(m.Name, "Night Shift") || m.Display != "standalone" {
		t.Errorf("manifest: %s %+v", w.Header().Get("Content-Type"), m)
	}
	maskable := false
	for _, i := range m.Icons {
		if _, ok := files()[strings.TrimPrefix(i.Src, "/")]; !ok {
			t.Errorf("the manifest names %s, which is not embedded", i.Src)
		}
		maskable = maskable || i.Purpose == "maskable"
	}
	if len(m.Icons) < 3 || !maskable {
		t.Errorf("icons: %+v", m.Icons)
	}
}

// The pages the API draws pass the same policy: nothing inline.
func TestPages(t *testing.T) {
	p := NewPages()
	for name, data := range map[string]map[string]any{
		"message":  {"Title": "Nothing here", "Message": "<b>not markup</b>"},
		"continue": {"Next": "/chat/"},
		"register": {"Title": "Choose a username", "Token": "t", "ProviderName": "Example"},
		"authorize": {"Title": "Sign in to Churr", "State": "second_factor", "DeviceName": "Kai's laptop", "HasPasskey": true,
			"HasTotp": true, "Enrol": true, "MayPasskey": true, "MayTotp": true},
	} {
		w := httptest.NewRecorder()
		p.Render(w, 200, name, data)
		body := w.Body.String()
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: headers %v", name, w.Header())
		}
		wantPolicy := "no-referrer"
		if strings.Contains(body, "<form method=\"post\"") {
			// A plain form post from a no-referrer page would carry
			// `Origin: null`, which the API's origin check refuses.
			wantPolicy = "same-origin"
		}
		if name == "authorize" || name == "register" {
			wantPolicy = "same-origin"
		}
		if got := w.Header().Get("Referrer-Policy"); got != wantPolicy {
			t.Errorf("%s: Referrer-Policy %q, want %q", name, got, wantPolicy)
		}
		checkHTML(t, "template "+name, body)
		if name == "message" && strings.Contains(body, "<b>not markup</b>") {
			t.Error("a message was written into the page as markup")
		}
		if name == "authorize" && !strings.Contains(body, `src="/oauth/authorize.js"`) {
			t.Error("the authorize page does not load its script")
		}
	}
}

var (
	inlineScript = regexp.MustCompile(`(?is)<script(\s[^>]*)?>`)
	scriptSrc    = regexp.MustCompile(`(?i)\ssrc="/[^"/][^"]*"`)
	inlineHandle = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
	elsewhere    = regexp.MustCompile(`(?i)(src|href|action)\s*=\s*"(https?:)?//`)
)

// checkHTML fails for anything the content security policy would refuse:
// inline script or style, an event attribute, or an address on another
// origin.
func checkHTML(t *testing.T, name, body string) {
	t.Helper()
	for _, tag := range inlineScript.FindAllString(body, -1) {
		if !scriptSrc.MatchString(tag) || !strings.Contains(tag, `type="module"`) {
			t.Errorf("%s: a script that is not a module file of this server: %s", name, tag)
		}
	}
	lower := strings.ToLower(body)
	if strings.Contains(lower, "<style") || strings.Contains(lower, " style=") {
		t.Errorf("%s: inline style", name)
	}
	if inlineHandle.MatchString(body) {
		t.Errorf("%s: an inline event attribute", name)
	}
	if elsewhere.MatchString(body) {
		t.Errorf("%s: loads something from another origin", name)
	}
}

// The files in the repository are the files the browser gets: no inline
// script or style in any page, nothing parsed as HTML in any script of
// ours, no third-party origin in any stylesheet, and no build machinery.
func TestStaticFiles(t *testing.T) {
	forbidden := []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "DOMParser", "eval(", "new Function"}
	pages := 0
	for name, f := range files() {
		body := string(f.body)
		switch {
		case strings.HasSuffix(name, ".html"):
			pages++
			checkHTML(t, name, body)
		case strings.HasSuffix(name, ".js"):
			// The vendored highlight.js files are the one exception to
			// "our own code only"; hl.js never calls the parts of the
			// core that write HTML.
			if strings.HasPrefix(name, "shared/highlight/languages/") || name == "shared/highlight/core.js" {
				continue
			}
			for _, word := range forbidden {
				for _, line := range strings.Split(body, "\n") {
					if strings.Contains(line, word) && !strings.HasPrefix(strings.TrimSpace(line), "//") {
						t.Errorf("%s uses %s: %s", name, word, strings.TrimSpace(line))
					}
				}
			}
		case strings.HasSuffix(name, ".css"):
			if regexp.MustCompile(`(?i)url\(\s*["']?(https?:)?//`).MatchString(body) || strings.Contains(body, "@import") {
				t.Errorf("%s loads something from elsewhere", name)
			}
		}
		base := name[strings.LastIndex(name, "/")+1:]
		if base == "package.json" || strings.Contains(name, "node_modules/") || strings.HasPrefix(name, "dist/") {
			t.Errorf("%s: there is no package manager and no build output under internal/web", name)
		}
	}
	if pages < 7 {
		t.Fatalf("found only %d pages; the embed has gone wrong", pages)
	}
	// The same for the templates, as files.
	entries, err := fs.ReadDir(templateFiles, "templates")
	if err != nil || len(entries) < 4 {
		t.Fatalf("templates: %v, %d files", err, len(entries))
	}
	if _, err := os.Stat("static/shared/highlight/LICENSE"); err != nil {
		t.Error("the vendored highlight.js has lost its licence file")
	}
	for _, lic := range []string{"shared/fonts/OFL-archivo.txt", "shared/fonts/OFL-martian-mono.txt", "shared/emoji/UNICODE-LICENSE.txt"} {
		if _, ok := files()[lic]; !ok {
			t.Errorf("%s is missing", lic)
		}
	}
}

// TestComposerCaret: the message box shows the browser's own caret and
// nothing else. No stylesheet draws a decorative cursor after an empty
// editor or keeps a blinking animation for one.
func TestComposerCaret(t *testing.T) {
	cursor := regexp.MustCompile(`\.editor[^{,]*::after|@keyframes\s+blink|animation:\s*blink`)
	for name, f := range files() {
		if !strings.HasSuffix(name, ".css") {
			continue
		}
		if m := cursor.Find(f.body); m != nil {
			t.Errorf("%s still draws a decorative cursor: %s", name, m)
		}
	}
}

func TestServes(t *testing.T) {
	for path, want := range map[string]bool{
		"/oauth/authorize.js": true, "/shared/dom.js": true, "/icons/favicon.png": true,
		"/login/index.html": false, "/oauth/authorize": false, "/api/v1/me": false, "/": false, "/shared/../web.go": false,
	} {
		if got := Serves(path); got != want {
			t.Errorf("Serves(%q) = %v, want %v", path, got, want)
		}
	}
}

// The approval form's answer is a redirect to the app. The policy of that
// one page names the address, and nothing else is loosened.
func TestAuthorizeFormAction(t *testing.T) {
	p := NewPages()
	for redirect, want := range map[string]string{
		"http://127.0.0.1:49152/callback": "form-action 'self' http://127.0.0.1:49152;",
		"http://[::1]:5000/cb":            "form-action 'self' http://[::1]:5000;",
		"churr://callback":                "form-action 'self' churr://callback;",
		"churr:callback":                  "form-action 'self' churr:;",
		"":                                "form-action 'self';",
		// Only the scheme and host are ever written into the policy, so
		// an address cannot smuggle a directive in. (Whether the address
		// is one an app may use is the API's check, made before the page
		// is drawn.)
		"http://odd.example/;script-src *": "form-action 'self' http://odd.example;",
	} {
		w := httptest.NewRecorder()
		params := []map[string]string{{"name": "state", "value": "s"}}
		if redirect != "" {
			params = append(params, map[string]string{"name": "redirect_uri", "value": redirect})
		}
		p.Render(w, 200, "authorize", map[string]any{"Title": "Approve this app", "State": "approve", "DeviceName": "Mac", "Username": "kai", "Params": params})
		csp := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, want) {
			t.Errorf("redirect %q: policy %q lacks %q", redirect, csp, want)
		}
		if strings.Contains(csp, "script-src *") || !strings.Contains(csp, "script-src 'self';") {
			t.Errorf("redirect %q loosened the policy: %s", redirect, csp)
		}
	}
	// No other page gets it.
	w := httptest.NewRecorder()
	p.Render(w, 200, "register", map[string]any{"Title": "x", "Params": []map[string]string{{"name": "redirect_uri", "value": "http://127.0.0.1:1/"}}})
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "form-action 'self';") {
		t.Error("a page other than the approval got a wider form-action")
	}
}
