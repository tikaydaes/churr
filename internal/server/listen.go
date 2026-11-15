package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/acme"
	"github.com/tikaydaes/churr/internal/api"
	"github.com/tikaydaes/churr/internal/live"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store/migrations"
	"github.com/tikaydaes/churr/internal/version"
)

// This file is how the server meets the network (docs/OPERATIONS.md sections
// 2, 6 and 7). There are three ways to run:
//
//   - alone: HTTPS on -https with a certificate the server fetches or was
//     given, and plain HTTP on -http for the certificate authority's
//     challenges and a redirect;
//   - behind a reverse proxy: plain HTTP on -http, which is the whole
//     service, with the proxy's forwarding headers believed from the
//     addresses in -trusted-proxies;
//   - a local trial (-dev): plain HTTP on localhost.
//
// Call media has its own port and its own listener, which belongs to the
// calls package.

// listeners is what Run serves on.
type listeners struct {
	// service carries the API and the pages. TLS is added by the HTTP
	// server when certs is set.
	service net.Listener
	// plain is the port 80 listener of a server running alone; nil
	// otherwise.
	plain net.Listener
	// certs holds the certificate of a server running alone.
	certs *acme.Manager
}

// alone reports whether the server terminates TLS itself.
func (s *Server) alone() bool { return !s.Config.Dev && !s.Config.BehindProxy }

// listen opens the listeners the configuration asks for. A test hands in
// its own listener, which is then the whole service.
func (s *Server) listen() (*listeners, error) {
	if s.opts.Listener != nil {
		return &listeners{service: s.opts.Listener}, nil
	}
	if !s.alone() {
		ln, err := net.Listen("tcp", s.Config.HTTP)
		if err != nil {
			return nil, listenError("-http", s.Config.HTTP, err)
		}
		return &listeners{service: ln}, nil
	}
	certs, err := acme.New(s.Config.Common, s.Log)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", s.Config.HTTPS)
	if err != nil {
		return nil, listenError("-https", s.Config.HTTPS, err)
	}
	plain, err := net.Listen("tcp", s.Config.HTTP)
	if err != nil {
		ln.Close()
		return nil, listenError("-http", s.Config.HTTP, err)
	}
	return &listeners{service: ln, plain: plain, certs: certs}, nil
}

// listenError says which flag named the address that could not be opened,
// and the usual reason.
func listenError(flag, addr string, err error) error {
	msg := fmt.Sprintf("cannot listen on %s (%s): %v", addr, flag, err)
	if errors.Is(err, os.ErrPermission) {
		msg += ". Ports under 1024 need permission: see the systemd unit in docs/OPERATIONS.md section 7, or run behind a reverse proxy with -behind-proxy"
	}
	return errors.New(msg)
}

// serve runs the HTTP servers on the listeners until one fails or ctx ends,
// then stops them: no new requests, and ten seconds for those in progress.
// Live connections are closed by the caller first; a hijacked connection is
// not the HTTP server's to wait for.
func (s *Server) serve(ctx context.Context, l *listeners) error {
	s.http = &http.Server{
		Handler:           s.logRequests(s.Handler()),
		ReadHeaderTimeout: 10 * time.Second,
		// No write timeout: downloads, uploads and the live connection
		// all run long. Idle keep-alive connections are let go.
		IdleTimeout: 2 * time.Minute,
		ErrorLog:    slog.NewLogLogger(s.Log.Handler(), slog.LevelDebug),
	}
	errc := make(chan error, 2)
	var plain *http.Server
	if l.certs != nil {
		s.http.TLSConfig = l.certs.TLSConfig()
		// TLS ends here, over sockets the live hub's writers can be
		// offered a write on (live.Offerable).
		go func() { errc <- s.http.ServeTLS(live.Offerable(l.service), "", "") }()
		_, httpsPort, _ := net.SplitHostPort(s.Config.HTTPS)
		plain = &http.Server{
			Handler:           l.certs.HTTPHandler(httpsPort),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       30 * time.Second,
			ErrorLog:          slog.NewLogLogger(s.Log.Handler(), slog.LevelDebug),
		}
		go func() { errc <- plain.Serve(l.plain) }()
		// Fetch the certificate now, not when the first visitor waits.
		go func() {
			warm, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			l.certs.Warm(warm)
		}()
	} else {
		go func() { errc <- s.http.Serve(l.service) }()
	}

	var failed error
	select {
	case failed = <-errc:
	case <-ctx.Done():
	}
	// Clients are told to come back before their connections go, so they
	// reconnect promptly to whatever starts next.
	s.Live.Shutdown()
	shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.http.Shutdown(shut)
	if plain != nil {
		plain.Shutdown(shut)
	}
	if errors.Is(failed, http.ErrServerClosed) {
		failed = nil
	}
	return failed
}

// announce writes the start-up lines of the log (docs/OPERATIONS.md section
// 15): what is listening where, the database, and which outside connections
// are on.
func (s *Server) announce(ctx context.Context, l *listeners) {
	args := []any{"version", version.String()}
	if l.certs != nil {
		args = append(args, "https", l.service.Addr().String(), "http", l.plain.Addr().String())
	} else {
		args = append(args, "http", l.service.Addr().String())
	}
	media := s.Config.Media
	if media == "" {
		media = "off"
	}
	s.Log.Info("listening", append(args, "media", media, "origin", s.Config.Origin())...)
	schema, _ := migrations.Latest(migrations.Dialect(s.Store.Dialect()))
	s.Log.Info("database", "kind", s.Store.Dialect(), "schema", schema)
	if on, err := s.API.OutsideOn(ctx); err == nil {
		if len(on) == 0 {
			on = []string{"none"}
		}
		s.Log.Info("outside connections", "on", strings.Join(on, ","))
	}
	switch {
	case s.Config.Dev:
		s.Log.Warn("this is a local trial (-dev): plain HTTP on localhost. Never use it on a network")
	case s.Config.BehindProxy:
		s.Log.Info("behind a reverse proxy: forwarding headers are believed from", "trusted_proxies", fmt.Sprint(s.Config.TrustedProxies))
	}
}

// wireOverview tells the API what the admin Overview shows that only the
// running server knows: whether it fetches its own certificate, and how
// that is going.
func (s *Server) wireOverview(l *listeners) {
	if l.certs == nil {
		return
	}
	s.API.Overview.FetchesCertificate = l.certs.Fetches()
	if s.API.Overview.Probes == nil {
		s.API.Overview.Probes = map[string]api.OutsideProbe{}
	}
	s.API.Overview.Probes[api.OutsideCertificate] = func(context.Context) api.OutsideHealth {
		ok, failure := l.certs.Health()
		h := api.OutsideHealth{LastError: failure}
		if !ok.IsZero() {
			h.LastOK = model.FromTime(ok)
		}
		return h
	}
	earlier := s.API.Overview.Warnings
	s.API.Overview.Warnings = func(ctx context.Context) []string {
		var out []string
		if earlier != nil {
			out = earlier(ctx)
		}
		if w := l.certs.Warning(); w != "" {
			out = append(out, w)
		}
		return out
	}
}

// emptyTmp removes what a server that stopped in the middle of an upload
// left behind (docs/OPERATIONS.md section 4: "tmp/, emptied at start").
func (s *Server) emptyTmp() {
	entries, err := os.ReadDir(s.Config.TmpDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(s.Config.TmpDir(), e.Name())); err != nil {
			s.Log.Warn("a leftover upload could not be removed", "err", err)
		}
	}
}

// secretPaths are the address prefixes after which comes something that
// must not reach the log: a webhook's address, a setup, recovery, claim or
// invite link.
var secretPaths = []string{"/hooks/", "/setup/", "/recover/", "/join/", "/claim/"}

// loggedPath is a request's path as it may be logged: without its query,
// and with any secret in it replaced.
func loggedPath(p string) string {
	for _, prefix := range secretPaths {
		if strings.HasPrefix(p, prefix) && len(p) > len(prefix) {
			return prefix + "…"
		}
	}
	return p
}

// statusWriter remembers the status a handler wrote.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the connection underneath, so
// the live connection can still take it over and downloads can still flush.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Hijack hands the connection over, for the live connection's WebSocket.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.status == 0 {
		w.status = http.StatusSwitchingProtocols
	}
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

// Flush sends what has been written so far.
func (w *statusWriter) Flush() { http.NewResponseController(w.ResponseWriter).Flush() }

// logRequests writes one line per request at the debug level: method, path,
// status, duration and address. At any other level it adds nothing at all.
// A query string is never logged; it can carry a code.
func (s *Server) logRequests(next http.Handler) http.Handler {
	if !s.Log.Enabled(context.Background(), slog.LevelDebug) {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		s.Log.Debug("request", "method", r.Method, "path", loggedPath(r.URL.Path), "status", sw.status,
			"took", time.Since(start).Round(time.Millisecond).String(), "address", host)
	})
}
