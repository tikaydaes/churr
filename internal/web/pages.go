package web

import (
	"embed"
	"html/template"
	"net/http"
	"net/url"
	"strings"
)

//go:embed templates/*.html
var templateFiles embed.FS

// Pages renders the few pages the server draws itself instead of handing
// the browser a file: approving an app, the return from company sign-in,
// choosing a username on a first company sign-in, and a plain message. They
// are drawn on the server because they are the answer to a request that
// carries one-time values, or to a form post.
//
// They use the same stylesheets as every other page and one script file,
// oauth/authorize.js. Nothing is written inline, so they pass the same
// content security policy.
type Pages struct {
	t *template.Template
}

// NewPages parses the page templates.
func NewPages() *Pages {
	return &Pages{t: template.Must(template.New("pages").ParseFS(templateFiles, "templates/*.html"))}
}

// Render writes one page. name is "message", "continue", "register" or
// "authorize"; data holds what the page shows.
func (p *Pages) Render(w http.ResponseWriter, status int, name string, data map[string]any) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	// These pages carry one-time values and ask for approvals: they are
	// never cached, never framed, and send no referrer.
	h.Set("Cache-Control", "no-store")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", referrerPolicy(name))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), display-capture=(), geolocation=()")
	if h.Get("Content-Security-Policy") == "" {
		// The API's handlers call this directly, outside Handler. These
		// pages only ever talk to the API over HTTP, so 'self' is all
		// the connection they need.
		plain, _ := policies("")
		if name == "authorize" {
			plain = allowReturn(plain, data)
		}
		h.Set("Content-Security-Policy", plain)
	}
	w.WriteHeader(status)
	if err := p.t.ExecuteTemplate(w, name, data); err != nil {
		// The status is already sent; there is nothing more to say.
		return
	}
}

// referrerPolicy is the policy a page is sent with. Every page sends no
// referrer, with one exception that the pages with a plain form post need.
//
// A browser that submits a form from a page whose policy is `no-referrer`
// sends `Origin: null` with it, and the API refuses a request that carries
// the session cookie unless its Origin is the server's own (docs/AUTH.md
// section 10.3). The approval of an app is such a form. `same-origin`
// keeps the Origin on requests to this server and still sends nothing at
// all to any other site, which is what the rule is for: the one-time
// values in these pages' addresses never leave the server.
func referrerPolicy(page string) string {
	switch page {
	case "authorize", "register":
		return "same-origin"
	}
	return "no-referrer"
}

// allowReturn lets the approval form's answer take the browser back to the
// app. Approving posts a form to this server, which answers with a redirect
// to the app's own address: a loopback address by number on desktop, the
// apps' address scheme on phones. A browser applies `form-action` to that
// redirect as well as to the post, so with 'self' alone the approval would
// be blocked. The one address added is the redirect address of this very
// request, which the API checked before it drew the page.
func allowReturn(policy string, data map[string]any) string {
	params, _ := data["Params"].([]map[string]string)
	for _, p := range params {
		if p["name"] != "redirect_uri" {
			continue
		}
		u, err := url.Parse(p["value"])
		if err != nil || u.Scheme == "" || strings.ContainsAny(u.Scheme+u.Host, " ;,'\"") {
			return policy
		}
		target := u.Scheme + ":"
		if u.Host != "" {
			target = u.Scheme + "://" + u.Host
		}
		return strings.Replace(policy, "form-action 'self'", "form-action 'self' "+target, 1)
	}
	return policy
}
