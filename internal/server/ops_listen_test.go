package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/tikaydaes/churr/internal/config"
)

// freePort finds a port nothing is listening on.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// testCert writes a self-signed certificate for a name and returns the
// paths and a pool that trusts it.
func testCert(t *testing.T, dir, host string) (certPath, keyPath string, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	certPath, keyPath = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
	pool = x509.NewCertPool()
	leaf, _ := x509.ParseCertificate(der)
	pool.AddCert(leaf)
	return certPath, keyPath, pool
}

// syncBuffer is a log destination a test can read while the server writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// A server running alone, with a certificate given as files: HTTPS carries
// the service, the plain port only redirects, the log says what a start
// should say and hides the secret in a path, and an interrupt stops it
// cleanly.
func TestServeAlone(t *testing.T) {
	dir := t.TempDir()
	const host = "chat.churr.test"
	certPath, keyPath, pool := testCert(t, dir, host)
	httpsAddr, httpAddr := freePort(t), freePort(t)
	cfg, err := config.ParseServe([]string{"-hostname", host, "-data", filepath.Join(dir, "data"), "-https", httpsAddr, "-http", httpAddr,
		"-tls-cert", certPath, "-tls-key", keyPath, "-media", "off", "-log-level", "debug"}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	srv, err := NewWith(cfg, Options{Log: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	if err != nil {
		t.Fatal(err)
	}
	// Something an interrupted upload left behind.
	leftover := filepath.Join(cfg.TmpDir(), "upload-123")
	os.WriteFile(leftover, []byte("half"), 0o600)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	// Every request goes to the test's listeners whatever name it uses.
	dial := func(to string) func(ctx context.Context, network, addr string) (net.Conn, error) {
		return func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, to)
		}
	}
	_, httpsPort, _ := net.SplitHostPort(httpsAddr)
	secure := &http.Client{Transport: &http.Transport{DialContext: dial(httpsAddr), TLSClientConfig: &tls.Config{RootCAs: pool}, ForceAttemptHTTP2: true}}
	plain := &http.Client{Transport: &http.Transport{DialContext: dial(httpAddr)},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	var resp *http.Response
	for i := 0; i < 100; i++ {
		if resp, err = secure.Get("https://" + host + ":" + httpsPort + "/healthz"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("health over HTTPS: %v\n%s", err, logs.String())
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "ok" || resp.ProtoMajor != 2 {
		t.Fatalf("health over HTTPS: %d %q HTTP/%d", resp.StatusCode, body, resp.ProtoMajor)
	}
	if resp.Header.Get("Strict-Transport-Security") == "" {
		t.Fatal("a server that holds its own certificate sends Strict-Transport-Security")
	}
	// The setup link is for the public origin, port included.
	if want := "https://" + host + ":" + httpsPort + "/setup/"; !strings.HasPrefix(srv.SetupLink, want) {
		t.Fatalf("the setup link %q does not start %q", srv.SetupLink, want)
	}
	resp, err = secure.Get(srv.SetupLink)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp, err = plain.Get("http://" + host + "/setup/secret-token?x=1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if want := "https://" + host + ":" + httpsPort + "/setup/secret-token?x=1"; resp.StatusCode != 301 || resp.Header.Get("Location") != want {
		t.Fatalf("the plain port: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatal("tmp/ was not emptied at start")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stopping: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the server did not stop")
	}
	if _, err := secure.Get("https://" + host + ":" + httpsPort + "/healthz"); err == nil {
		t.Fatal("still listening after it stopped")
	}
	// The database is closed: a second server can open the same data.
	again, err := NewWith(cfg, Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatalf("opening the data directory again: %v", err)
	}
	again.Close()

	out := logs.String()
	for _, want := range []string{"listening", "https=" + httpsAddr, "http=" + httpAddr, "media=off", "kind=sqlite", "schema=", "outside connections", "on=none", "shutting down", `path=/healthz`, "path=/setup/…"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log has no %q", want)
		}
	}
	token := strings.TrimPrefix(srv.SetupLink, "https://"+host+":"+httpsPort+"/setup/")
	if strings.Contains(out, "path=/setup/"+token) || strings.Contains(out, "x=1") {
		t.Error("a request line logged the secret in a path, or a query")
	}
	if t.Failed() {
		t.Log(out)
	}
}

// Running alone without a certificate needs ports the test cannot have, so
// only what happens before listening is held here: the address in use is
// named with its flag.
func TestListenErrorsNameTheFlag(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	cfg, err := config.ParseServe([]string{"-behind-proxy", "-hostname", "chat.churr.test", "-data", t.TempDir(), "-http", busy.Addr().String(), "-media", "off"},
		func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewWith(cfg, Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	err = srv.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "-http") || !strings.Contains(err.Error(), busy.Addr().String()) {
		t.Fatalf("an address in use: %v", err)
	}
}

// The request log sits in front of everything, the live connection
// included, so a WebSocket must still be able to take its connection over.
func TestRequestLogLetsWebSocketsThrough(t *testing.T) {
	logs := &syncBuffer{}
	s := &Server{Log: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	ts := httptest.NewServer(s.logRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		c.Write(r.Context(), websocket.MessageText, []byte("hello"))
		c.Close(websocket.StatusNormalClosure, "")
	})))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/hooks/secret-address", nil)
	if err != nil {
		t.Fatalf("a WebSocket through the request log: %v", err)
	}
	if _, data, err := c.Read(ctx); err != nil || string(data) != "hello" {
		t.Fatalf("reading: %q %v", data, err)
	}
	c.CloseNow()
	for i := 0; i < 100 && !strings.Contains(logs.String(), "request"); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if out := logs.String(); !strings.Contains(out, "status=101") || !strings.Contains(out, "path=/hooks/…") || strings.Contains(out, "secret-address") {
		t.Fatalf("the request line: %s", out)
	}

	// At any other level the handler is not wrapped at all.
	quiet := &Server{Log: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))}
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if fmt.Sprintf("%p", quiet.logRequests(h)) != fmt.Sprintf("%p", h) {
		t.Fatal("the request log is in the way when it is not wanted")
	}
}
