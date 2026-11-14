package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/web"
)

// The embedded pages (docs/CLIENTS.md part two). The API answers the
// addresses of the contract; the web package answers every other address
// with a page or a file.

// apiPrefixes are the address spaces that belong to the API. Everything
// else is a page.
var apiPrefixes = []string{"/api/", "/oauth/", "/hooks/", "/sso/", "/link/", "/.well-known/"}

// isAPIPath reports whether an address is the API's to answer. A file the
// pages ship is not, wherever it sits: the script of the page that approves
// an app is /oauth/authorize.js, inside the API's own address space.
func isAPIPath(p string) bool {
	if p == "/healthz" {
		return true
	}
	if web.Serves(p) {
		return false
	}
	for _, prefix := range apiPrefixes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// handler puts the API and the pages behind one address space.
func (s *Server) handler() http.Handler {
	// The server terminates TLS itself unless it is a local trial or sits
	// behind a proxy; only then is Strict-Transport-Security its to send.
	tls := !s.Config.Dev && !s.Config.BehindProxy
	pages := web.Handler(web.Options{
		Origin: s.Config.Origin(),
		TLS:    tls,
		CommunityName: func(ctx context.Context) string {
			name, _ := store.SettingString(ctx, s.Store, store.SettingServerName, "")
			return name
		},
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			if tls {
				// The pages the API draws itself (approving an app,
				// the return from company sign-in) and its JSON carry
				// it too: the header is about the host, not the page.
				w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			s.API.ServeHTTP(w, r)
			return
		}
		pages.ServeHTTP(w, r)
	})
}
