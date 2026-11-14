// Package acme gives a program that faces the internet directly its
// certificate: fetched from a certificate authority and renewed by itself
// (golang.org/x/crypto/acme/autocert), or read from files the operator
// names and read again when they change. It also provides what listens on
// the plain HTTP port in that mode: the authority's challenges, and a
// redirect to HTTPS for everything else.
//
// The server, the hub and the relay all use it (docs/OPERATIONS.md section
// 7). None of them uses it behind a reverse proxy, where the proxy holds
// the certificate.
//
// Fetching makes one of the outside connections of the table in BRIEF.md:
// "Certificate renewal, when the server runs without a reverse proxy". No
// contact address is sent to the authority: Churr never asks for one.
package acme

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"

	"github.com/tikaydaes/churr/internal/config"
)

// Manager holds the certificate for one host name.
type Manager struct {
	hostname string
	log      *slog.Logger

	// auto fetches and renews. It is nil when the operator gave files.
	auto *autocert.Manager
	// files is the certificate read from -tls-cert and -tls-key.
	files *fileCert

	mu        sync.Mutex
	lastOK    time.Time
	lastError string
	notAfter  time.Time
}

// New makes the manager for a program's configuration. With -tls-cert and
// -tls-key it loads them now and fails if they cannot be used; otherwise
// nothing is fetched until the first connection asks for the certificate.
func New(c config.Common, log *slog.Logger) (*Manager, error) {
	if log == nil {
		log = slog.Default()
	}
	if c.Hostname == "" {
		return nil, errors.New("a certificate needs the public host name (-hostname)")
	}
	m := &Manager{hostname: c.Hostname, log: log}
	if c.TLSCert != "" {
		m.files = &fileCert{certPath: c.TLSCert, keyPath: c.TLSKey}
		cert, err := m.files.get()
		if err != nil {
			return nil, fmt.Errorf("reading -tls-cert and -tls-key: %w", err)
		}
		m.note(cert, nil)
		return m, nil
	}
	if err := os.MkdirAll(c.CertsDir(), 0o700); err != nil {
		return nil, fmt.Errorf("making the certificates folder: %w", err)
	}
	m.auto = &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		// One name only. A connection that asks for any other gets no
		// certificate, so nobody can make this server request
		// certificates for names of their choosing.
		HostPolicy: autocert.HostWhitelist(c.Hostname),
		Cache:      autocert.DirCache(c.CertsDir()),
		Client:     &acme.Client{DirectoryURL: c.ACMEDirectory},
	}
	return m, nil
}

// Fetches reports whether the certificate comes from a certificate
// authority, which is an outside connection, rather than from files.
func (m *Manager) Fetches() bool { return m.auto != nil }

// TLSConfig is the configuration for the HTTPS listener.
func (m *Manager) TLSConfig() *tls.Config {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"h2", "http/1.1"}}
	if m.auto != nil {
		// The authority may also prove the name over TLS itself.
		cfg.NextProtos = append(cfg.NextProtos, acme.ALPNProto)
	}
	cfg.GetCertificate = m.getCertificate
	return cfg
}

func (m *Manager) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if m.files != nil {
		cert, err := m.files.get()
		m.note(cert, err)
		return cert, err
	}
	cert, err := m.auto.GetCertificate(hello)
	// A connection that asked for some other name is refused by the host
	// policy. That is the policy working, not the authority failing, and
	// anyone on the internet can cause it.
	if err != nil && !sameHost(hello.ServerName, m.hostname) {
		return nil, err
	}
	m.note(cert, err)
	return cert, err
}

func sameHost(a, b string) bool {
	return strings.EqualFold(strings.TrimSuffix(a, "."), b)
}

// note records how the last attempt to have the certificate went.
func (m *Manager) note(cert *tls.Certificate, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		if msg := err.Error(); msg != m.lastError {
			m.log.Warn("the certificate could not be had", "host", m.hostname, "err", msg)
			m.lastError = msg
		}
		return
	}
	m.lastOK, m.lastError = time.Now(), ""
	if cert == nil {
		return
	}
	leaf := cert.Leaf
	if leaf == nil && len(cert.Certificate) > 0 {
		leaf, _ = x509.ParseCertificate(cert.Certificate[0])
	}
	if leaf != nil {
		m.notAfter = leaf.NotAfter
	}
}

// Health reports when the certificate was last served and what the last
// failure was, or "" if it has worked since.
func (m *Manager) Health() (lastOK time.Time, lastError string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastOK, m.lastError
}

// warnWithin is how close to its end a certificate must be to be worth a
// warning. A fetched certificate is renewed thirty days before its end, so
// one this close has failed to renew for weeks.
const warnWithin = 14 * 24 * time.Hour

// Warning returns a plain sentence when the certificate in use is near its
// end or past it, and "" otherwise.
func (m *Manager) Warning() string {
	m.mu.Lock()
	end := m.notAfter
	m.mu.Unlock()
	if end.IsZero() {
		return ""
	}
	left := time.Until(end)
	switch {
	case left <= 0:
		return "The certificate for " + m.hostname + " has expired."
	case left < warnWithin:
		days := int(left.Hours() / 24)
		if m.auto != nil {
			return fmt.Sprintf("The certificate for %s ends in %d days and has not been renewed. Check that the certificate authority can reach ports 80 and 443.", m.hostname, days)
		}
		return fmt.Sprintf("The certificate for %s ends in %d days. Replace the files given as -tls-cert and -tls-key.", m.hostname, days)
	}
	return ""
}

// HTTPHandler is what the plain HTTP port serves when the program holds its
// own certificate: the authority's challenges, and for everything else a
// redirect to the same address over HTTPS. httpsPort is the port of the
// HTTPS listener, "" or "443" for the usual one.
func (m *Manager) HTTPHandler(httpsPort string) http.Handler {
	redirect := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// A request with a body is not replayed over HTTPS by being
			// redirected; say what is wrong instead.
			http.Error(w, "Use HTTPS.", http.StatusBadRequest)
			return
		}
		// Always to the server's own name, never to whatever Host the
		// request carried: this must not be usable to bounce a browser
		// somewhere else.
		target := "https://" + m.hostname
		if httpsPort != "" && httpsPort != "443" {
			target = "https://" + net.JoinHostPort(m.hostname, httpsPort)
		}
		w.Header().Set("Connection", "close")
		http.Redirect(w, r, target+r.URL.RequestURI(), http.StatusMovedPermanently)
	})
	if m.auto == nil {
		return redirect
	}
	return m.auto.HTTPHandler(redirect)
}

// Warm asks for the certificate once, so a program that has just started
// fetches it before the first visitor waits for it, and a name that cannot
// be certified is in the log at once. It is safe to skip.
func (m *Manager) Warm(ctx context.Context) {
	if m.auto == nil {
		return
	}
	hello := &tls.ClientHelloInfo{ServerName: m.hostname, SupportedProtos: []string{"h2", "http/1.1"}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.getCertificate(hello)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// fileCert is a certificate and key in files, read again when either file
// changes, so a renewal by some other tool is picked up without a restart.
type fileCert struct {
	certPath, keyPath string

	mu       sync.Mutex
	cert     *tls.Certificate
	stamp    [2]time.Time
	checked  time.Time
	lastFail error
}

// recheck is how often the files are looked at for a change.
const recheck = 30 * time.Second

func (f *fileCert) get() (*tls.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cert != nil && time.Since(f.checked) < recheck {
		return f.cert, nil
	}
	f.checked = time.Now()
	var stamp [2]time.Time
	for i, p := range []string{f.certPath, f.keyPath} {
		st, err := os.Stat(p)
		if err != nil {
			return f.keep(err)
		}
		stamp[i] = st.ModTime()
	}
	if f.cert != nil && stamp == f.stamp {
		return f.cert, nil
	}
	cert, err := tls.LoadX509KeyPair(f.certPath, f.keyPath)
	if err != nil {
		// Caught halfway through being replaced, most likely. The stamp
		// is not kept, so the next look reads the files again.
		return f.keep(err)
	}
	f.cert, f.stamp, f.lastFail = &cert, stamp, nil
	return f.cert, nil
}

// keep answers with the certificate already loaded when the files cannot be
// read just now, and with the error when there is none.
func (f *fileCert) keep(err error) (*tls.Certificate, error) {
	f.lastFail = err
	if f.cert != nil {
		return f.cert, nil
	}
	return nil, err
}
