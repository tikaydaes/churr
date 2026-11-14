package web

import (
	"io/fs"
	"net/http"
)

// UnlockPage writes the page an app opens to unlock sealed chats with a
// passkey (docs/SEALED.md section 13.3). The API answers GET /oauth/unlock
// with it. It is the same file the browser is given at /unlock/, with the
// headers of a page that handles secrets: never cached, never framed, no
// referrer. Everything it does happens in the browser, in
// static/unlock/unlock.js.
func UnlockPage(w http.ResponseWriter, status int, _ map[string]any) {
	page, err := fs.ReadFile(Files(), "unlock/index.html")
	if err != nil {
		http.Error(w, "the unlock page is not built in", http.StatusServiceUnavailable)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), display-capture=(), geolocation=()")
	if h.Get("Content-Security-Policy") == "" {
		plain, _ := policies("")
		h.Set("Content-Security-Policy", plain)
	}
	w.WriteHeader(status)
	w.Write(page)
}
