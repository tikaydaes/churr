package acme

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/config"
)

// selfSigned writes a certificate and key for a name into dir and returns
// the two paths. The serial tells two certificates apart.
func selfSigned(t *testing.T, dir, host string, serial int64, notAfter time.Time) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	// Written beside and renamed, as a careful renewal tool does.
	write := func(path, kind string, b []byte) {
		tmp := path + ".new"
		if err := os.WriteFile(tmp, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: b}), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, path); err != nil {
			t.Fatal(err)
		}
	}
	write(certPath, "CERTIFICATE", der)
	write(keyPath, "EC PRIVATE KEY", keyDER)
	return certPath, keyPath
}

func serial(t *testing.T, m *Manager) int64 {
	t.Helper()
	cert, err := m.TLSConfig().GetCertificate(&tls.ClientHelloInfo{ServerName: "chat.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return leaf.SerialNumber.Int64()
}

// A certificate given as files is used as it is, read again when the files
// change, and kept when they cannot be read for a moment.
func TestCertificateFromFiles(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := selfSigned(t, dir, "chat.example.com", 1, time.Now().Add(90*24*time.Hour))
	m, err := New(config.Common{Hostname: "chat.example.com", Data: dir, TLSCert: certPath, TLSKey: keyPath}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Fetches() {
		t.Fatal("a certificate from files is not an outside connection")
	}
	if serial(t, m) != 1 {
		t.Fatal("the certificate given was not the one served")
	}
	if ok, failure := m.Health(); ok.IsZero() || failure != "" {
		t.Fatalf("health after serving: %v %q", ok, failure)
	}
	if w := m.Warning(); w != "" {
		t.Fatalf("a certificate with ninety days left warned: %s", w)
	}

	// Replaced on disk: not noticed until the files are looked at again,
	// then served.
	selfSigned(t, dir, "chat.example.com", 2, time.Now().Add(5*24*time.Hour))
	future := time.Now().Add(time.Hour)
	os.Chtimes(certPath, future, future)
	if serial(t, m) != 1 {
		t.Fatal("the files were read again before the wait was over")
	}
	m.files.mu.Lock()
	m.files.checked = time.Time{}
	m.files.mu.Unlock()
	if serial(t, m) != 2 {
		t.Fatal("a replaced certificate was not picked up")
	}
	if w := m.Warning(); !strings.Contains(w, "ends in") {
		t.Fatalf("a certificate with five days left did not warn: %q", w)
	}

	// Unreadable for a moment: the one in memory keeps serving.
	os.WriteFile(certPath, []byte("half written"), 0o600)
	later := future.Add(time.Hour)
	os.Chtimes(certPath, later, later)
	m.files.mu.Lock()
	m.files.checked = time.Time{}
	m.files.mu.Unlock()
	if serial(t, m) != 2 {
		t.Fatal("a broken file took the working certificate away")
	}

	// Files that cannot be used at start are an error at start.
	if _, err := New(config.Common{Hostname: "chat.example.com", Data: dir, TLSCert: certPath, TLSKey: keyPath}, nil); err == nil {
		t.Fatal("a broken certificate file was accepted at start")
	}
}

// The plain HTTP port redirects to the server's own name over HTTPS,
// whatever Host the request carried, and answers nothing else.
func TestPlainPortRedirects(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := selfSigned(t, dir, "chat.example.com", 1, time.Now().Add(time.Hour))
	m, err := New(config.Common{Hostname: "chat.example.com", Data: dir, TLSCert: certPath, TLSKey: keyPath}, nil)
	if err != nil {
		t.Fatal(err)
	}
	get := func(h http.Handler, method, host, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://"+host+path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := get(m.HTTPHandler("443"), "GET", "evil.example", "/chat/?x=1")
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "https://chat.example.com/chat/?x=1" {
		t.Fatalf("redirect: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := get(m.HTTPHandler("8443"), "GET", "chat.example.com", "/"); w.Header().Get("Location") != "https://chat.example.com:8443/" {
		t.Fatalf("redirect to another HTTPS port: %q", w.Header().Get("Location"))
	}
	if w := get(m.HTTPHandler("443"), "POST", "chat.example.com", "/api/v1/sign-in"); w.Code != http.StatusBadRequest {
		t.Fatalf("a POST over plain HTTP: %d", w.Code)
	}
}

// With no files the certificate is fetched, for the server's own name and
// no other. Nothing here reaches a certificate authority: a name that is
// not the server's is refused before anything is asked of one, and a
// challenge nobody issued is not found.
func TestFetchedCertificateIsForOneName(t *testing.T) {
	dir := t.TempDir()
	m, err := New(config.Common{Hostname: "chat.example.com", Data: dir, ACMEDirectory: "http://127.0.0.1:1/directory"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Fetches() {
		t.Fatal("with no files given, the certificate is fetched")
	}
	if st, err := os.Stat(filepath.Join(dir, "certs")); err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("the certificates folder: %v %v", st, err)
	}
	cfg := m.TLSConfig()
	if _, err := cfg.GetCertificate(&tls.ClientHelloInfo{ServerName: "other.example.com"}); err == nil {
		t.Fatal("a certificate was offered for a name that is not the server's")
	}
	// Someone else's name in a handshake is not the authority failing.
	if _, failure := m.Health(); failure != "" {
		t.Fatalf("a refused name was recorded as a failure: %q", failure)
	}
	has := false
	for _, p := range cfg.NextProtos {
		has = has || p == "acme-tls/1"
	}
	if !has {
		t.Fatal("the TLS challenge protocol is not offered")
	}
	r := httptest.NewRequest("GET", "http://chat.example.com/.well-known/acme-challenge/nobody-issued-this", nil)
	w := httptest.NewRecorder()
	m.HTTPHandler("443").ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("an unknown challenge: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "http://chat.example.com/setup/abc", nil)
	w = httptest.NewRecorder()
	m.HTTPHandler("443").ServeHTTP(w, r)
	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("everything else is redirected: %d", w.Code)
	}
}
