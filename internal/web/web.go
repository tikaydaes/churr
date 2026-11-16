// Package web holds the pages the server itself serves: sign-in, first-run
// setup, joining, the account page, the admin console and the web chat
// (docs/CLIENTS.md part two). They are plain HTML, CSS and JavaScript
// modules, embedded in the binary as the files they are in the repository.
// There is no build step.
//
// The pages talk to the API like any client and hold no rules of their own.
// This package only serves files, chooses which HTML file answers an address,
// and sets the headers every page must carry. It imports nothing of Churr's
// but the version.
package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/version"
)

//go:embed static
var staticFiles embed.FS

// Files is the tree of files the browser is given, rooted so that
// "shared/base.css" is served at /shared/base.css.
func Files() fs.FS {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err) // the directory is embedded above; this cannot happen
	}
	return sub
}

// SessionCookie is the name of the browser session cookie (docs/AUTH.md
// section 10.2). It is only looked at to choose where "/" leads; whether
// the session is any good is the API's to say.
const SessionCookie = "churr_session"

// Options are what the handler needs to know about the server it is in.
type Options struct {
	// Origin is the server's own origin, "https://chat.example.org". The
	// content security policy names the WebSocket address made from it.
	Origin string
	// TLS says the server terminates TLS itself, so the pages carry
	// Strict-Transport-Security. Behind a proxy that header is the
	// proxy's to send.
	TLS bool
	// CommunityName gives the community's name for the install manifest.
	// Nil, or an empty answer, leaves the name as "Churr".
	CommunityName func(ctx context.Context) string
	// Dev serves the pages under /dev/, which exist for trying things by
	// eye (the chance reels of ruling R67). A real server never does.
	Dev bool
}

// area is one of the single-page areas: any address under its prefix that
// is not a file is answered with its HTML file, and the page reads the
// address itself (docs/CLIENTS.md section 17).
type area struct {
	prefix string // "/admin"
	file   string // "admin/index.html"
}

var areas = []area{
	{"/setup", "setup/index.html"},
	{"/login", "login/index.html"},
	{"/join", "join/index.html"},
	{"/recover", "recover/index.html"},
	{"/unlock", "unlock/index.html"},
	{"/locked", "locked/index.html"},
	{"/account", "account/index.html"},
	{"/admin", "admin/index.html"},
	{"/chat", "chat/index.html"},
	// A theme's link, {origin}/themes/{id} (internal/themes.PagePath).
	{"/themes", "themes/index.html"},
}

// contentTypes are spelled out so that what a file is served as never
// depends on the machine's own table of types.
var contentTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".json":        "application/json",
	".webmanifest": "application/manifest+json",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".woff2":       "font/woff2",
	".txt":         "text/plain; charset=utf-8",
	// The ring of an incoming call (static/sounds, from art/sounds).
	".m4a": "audio/mp4",
	".wav": "audio/wav",
}

// file is one embedded file, read and hashed once.
type file struct {
	body []byte
	etag string
	kind string
}

var (
	loadOnce sync.Once
	loaded   map[string]file
)

// files reads every embedded file once and works out its ETag. The files
// cannot change while the program runs, so neither can the tags.
func files() map[string]file {
	loadOnce.Do(func() {
		loaded = map[string]file{}
		root := Files()
		err := fs.WalkDir(root, ".", func(name string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			body, err := fs.ReadFile(root, name)
			if err != nil {
				return err
			}
			kind, ok := contentTypes[path.Ext(name)]
			if !ok {
				// Licence files have no extension that says so.
				kind = "text/plain; charset=utf-8"
			}
			sum := sha256.Sum256(body)
			loaded[name] = file{body: body, etag: `"` + hex.EncodeToString(sum[:12]) + `"`, kind: kind}
			return nil
		})
		if err != nil {
			panic("web: reading the embedded pages: " + err.Error())
		}
	})
	return loaded
}

type handler struct {
	opts  Options
	pages *Pages
	// csp and chatCSP are the two policies, built once.
	csp, chatCSP string
}

// Handler serves the embedded pages with the headers of docs/CLIENTS.md
// section 19. It answers everything the API does not.
func Handler(o Options) http.Handler {
	h := &handler{opts: o, pages: NewPages()}
	h.csp, h.chatCSP = policies(o.Origin)
	return h
}

// policies builds the content security policy of the web chat, which needs
// the most, and the one every other page gets: the same without `blob:`,
// media and workers.
func policies(origin string) (plain, chat string) {
	// The live connection. 'self' covers it in current browsers; the
	// address is named as well because older ones did not match ws: and
	// wss: against 'self'.
	connect := "'self'"
	if u, err := url.Parse(origin); err == nil && u.Host != "" {
		scheme := "wss"
		if u.Scheme == "http" {
			scheme = "ws"
		}
		connect += " " + scheme + "://" + u.Host
	}
	common := func(img, extra string) string {
		return strings.Join([]string{
			"default-src 'none'",
			"script-src 'self'",
			"style-src 'self'",
			"img-src " + img,
			extra + "font-src 'self'",
			"connect-src " + connect,
			"manifest-src 'self'",
			"form-action 'self'",
			"frame-ancestors 'none'",
			"base-uri 'none'",
		}, "; ")
	}
	plain = common("'self'", "")
	chat = common("'self' blob:", "media-src 'self' blob:; worker-src 'self'; ")
	return plain, chat
}

// SecurityHeaders sets the headers every page carries. chat says the page
// is the web chat, the one place the camera, the microphone and screen
// capture are allowed.
func (h *handler) securityHeaders(w http.ResponseWriter, chat bool) {
	hd := w.Header()
	if chat {
		hd.Set("Content-Security-Policy", h.chatCSP)
		hd.Set("Permissions-Policy", "camera=(self), microphone=(self), display-capture=(self), geolocation=()")
	} else {
		hd.Set("Content-Security-Policy", h.csp)
		hd.Set("Permissions-Policy", "camera=(), microphone=(), display-capture=(), geolocation=()")
	}
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Referrer-Policy", "no-referrer")
	hd.Set("Cross-Origin-Opener-Policy", "same-origin")
	// frame-ancestors says the same; this is for browsers that predate it.
	hd.Set("X-Frame-Options", "DENY")
	if h.opts.TLS {
		hd.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	chat := p == "/chat" || strings.HasPrefix(p, "/chat/")
	h.securityHeaders(w, chat)

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		h.pages.Render(w, http.StatusMethodNotAllowed, "message", map[string]any{
			"Title": "That cannot be done here", "Message": "This address only shows a page."})
		return
	}

	switch p {
	case "/":
		// Where "/" leads depends only on whether the browser holds a
		// session cookie. A cookie that has gone stale is found out by
		// the chat page's first request, which sends the person on to
		// sign in.
		to := "/login"
		if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
			to = "/chat/"
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, to, http.StatusFound)
		return
	case "/chat":
		// The manifest's scope and the page's relative addresses both
		// want the slash.
		http.Redirect(w, r, "/chat/", http.StatusMovedPermanently)
		return
	case "/favicon.ico":
		h.serveFile(w, r, "icons/favicon.png")
		return
	case "/chat/manifest.webmanifest":
		h.manifest(w, r)
		return
	}

	// Invite links are printed by the server as /invite/<code>, while the
	// join page lives at /join/<code> (docs/CLIENTS.md section 15). Until
	// the two agree, the printed address leads to the page.
	if code, ok := strings.CutPrefix(p, "/invite/"); ok && code != "" && !strings.Contains(code, "/") {
		http.Redirect(w, r, "/join/"+url.PathEscape(code), http.StatusFound)
		return
	}

	name := strings.TrimPrefix(path.Clean(p), "/")
	if strings.HasPrefix(name, "dev/") || name == "dev" {
		if !h.opts.Dev {
			h.pages.Render(w, http.StatusNotFound, "message", map[string]any{
				"Title": "Nothing here", "Message": "There is no page at this address.", "Home": true})
			return
		}
		if name == "dev/chance" {
			h.serveFile(w, r, "dev/chance.html")
			return
		}
	}
	if f, ok := files()[name]; ok && !strings.HasSuffix(name, ".html") {
		h.serve(w, r, f)
		return
	}
	for _, a := range areas {
		if p == a.prefix || strings.HasPrefix(p, a.prefix+"/") {
			// A file that is missing must not be answered with a page:
			// a script tag would then try to run HTML.
			if ext := path.Ext(p); ext != "" && contentTypes[ext] != "" {
				break
			}
			h.serveFile(w, r, a.file)
			return
		}
	}
	h.pages.Render(w, http.StatusNotFound, "message", map[string]any{
		"Title": "Nothing here", "Message": "There is no page at this address.", "Home": true})
}

func (h *handler) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	f, ok := files()[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.serve(w, r, f)
}

// serve writes one embedded file. Every file carries an ETag and `no-cache`:
// the browser keeps its copy and asks each time whether it is still current,
// so a new server version is picked up on the next load with no build step
// naming files by hash (docs/CLIENTS.md section 17).
func (h *handler) serve(w http.ResponseWriter, r *http.Request, f file) {
	hd := w.Header()
	hd.Set("Content-Type", f.kind)
	hd.Set("ETag", f.etag)
	hd.Set("Cache-Control", "no-cache")
	// ServeContent answers If-None-Match from the ETag set above.
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(f.body))
}

// manifest writes the install manifest of the web chat, with the
// community's name in it (docs/CLIENTS.md section 20).
func (h *handler) manifest(w http.ResponseWriter, r *http.Request) {
	name := "Churr"
	if h.opts.CommunityName != nil {
		if c := strings.TrimSpace(h.opts.CommunityName(r.Context())); c != "" && c != "Churr" {
			name = "Churr · " + c
		}
	}
	icon := func(src, sizes, purpose string) map[string]string {
		return map[string]string{"src": src, "sizes": sizes, "type": "image/png", "purpose": purpose}
	}
	body, _ := json.MarshalIndent(map[string]any{
		"name":             name,
		"short_name":       "Churr",
		"id":               "/chat/",
		"start_url":        "/chat/",
		"scope":            "/chat/",
		"display":          "standalone",
		"background_color": "#0d1026",
		"theme_color":      "#0d1026",
		"icons": []map[string]string{
			icon("/icons/icon-192.png", "192x192", "any"),
			icon("/icons/icon-512.png", "512x512", "any"),
			icon("/icons/maskable-512.png", "512x512", "maskable"),
		},
	}, "", "  ")
	hd := w.Header()
	hd.Set("Content-Type", contentTypes[".webmanifest"])
	hd.Set("Cache-Control", "no-cache")
	w.Write(body)
}

// Serves reports whether a path is one of the embedded files other than a
// page: a script, a stylesheet, a font, a picture. The server asks before
// it hands an address in the API's space to the API, because one script,
// /oauth/authorize.js, sits beside the page the API draws at
// /oauth/authorize.
func Serves(p string) bool {
	name := strings.TrimPrefix(path.Clean("/"+p), "/")
	_, ok := files()[name]
	return ok && !strings.HasSuffix(name, ".html")
}

// Version is the version shown in the foot of the pages the server draws.
func Version() string { return version.String() }
