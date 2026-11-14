package server

import "testing"

// The API and the pages share one address space. An address is the API's
// by its prefix, except where the pages ship a file there.
func TestIsAPIPath(t *testing.T) {
	for path, want := range map[string]bool{
		"/api/v1/me":            true,
		"/api/v1/files/x/thumb": true,
		"/oauth/authorize":      true,
		"/oauth/token":          true,
		"/oauth/pair/abc":       true,
		"/hooks/churr_hook_x":   true,
		"/sso/register":         true,
		"/.well-known/churr":    true,
		"/healthz":              true,
		"/link/v1":              true,
		"/linkage":              false,
		"/oauth/authorize.js":   false,
		"/":                     false,
		"/login":                false,
		"/chat/":                false,
		"/chat/app.js":          false,
		"/admin/people":         false,
		"/shared/base.css":      false,
		"/apiary":               false,
	} {
		if got := isAPIPath(path); got != want {
			t.Errorf("isAPIPath(%q) = %v, want %v", path, got, want)
		}
	}
}
