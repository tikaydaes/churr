package preview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

func TestPublic(t *testing.T) {
	for _, s := range []string{"93.184.216.34", "8.8.8.8", "2606:4700:4700::1111", "1.1.1.1"} {
		if !Public(netip.MustParseAddr(s)) {
			t.Errorf("%s is a public address", s)
		}
	}
	// Every kind section 23.3 names, and the IPv4-mapped form of each.
	for _, s := range []string{
		"127.0.0.1", "127.8.9.10", "::1", // loopback
		"10.0.0.1", "172.16.5.4", "172.31.255.255", "192.168.1.1", // private
		"169.254.169.254", "fe80::1", // link-local, and the cloud metadata address
		"100.64.0.1", "100.127.255.254", // carrier-grade NAT
		"fc00::1", "fd12:3456::1", // unique-local
		"fec0::1",                                 // site-local
		"224.0.0.1", "ff02::1", "239.255.255.250", // multicast
		"0.0.0.0", "::", "0.1.2.3", // unspecified
		"255.255.255.255", "240.0.0.1", "192.0.2.1", "198.18.0.1", "64:ff9b::a00:1", "2002:a00:1::1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254", "::ffff:192.168.0.1", "::ffff:100.64.0.1",
	} {
		if Public(netip.MustParseAddr(s)) {
			t.Errorf("%s must be refused", s)
		}
	}
	if Public(netip.Addr{}) {
		t.Errorf("no address at all must be refused")
	}
}

// world is a pretend internet for the guarded dialer: names resolve as the
// test says, and a connection to a "public" address lands on a local
// listener. The guard itself, resolution check and address check, is the
// real one.
type world struct {
	mu    sync.Mutex
	names map[string][]netip.Addr // host name -> what it resolves to
	route map[string]string       // "public ip:port" -> local listener address
	dials []string                // every address actually connected to
}

func (w *world) dialer() *Dialer {
	return &Dialer{
		Resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
			w.mu.Lock()
			defer w.mu.Unlock()
			if a, ok := w.names[host]; ok {
				return a, nil
			}
			return nil, fmt.Errorf("no such host %s", host)
		},
		Connect: func(ctx context.Context, network, address string) (net.Conn, error) {
			w.mu.Lock()
			w.dials = append(w.dials, address)
			to, ok := w.route[address]
			w.mu.Unlock()
			if !ok {
				return nil, fmt.Errorf("nothing listens at %s", address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, to)
		},
	}
}

// serve starts a local listener and makes it reachable at a public address
// under a name.
func (w *world) serve(t *testing.T, name, ip string, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.names == nil {
		w.names, w.route = map[string][]netip.Addr{}, map[string]string{}
	}
	w.names[name] = []netip.Addr{netip.MustParseAddr(ip)}
	w.route[ip+":80"] = srv.Listener.Addr().String()
	return srv
}

func newService(t *testing.T, w *world) (*Service, store.Store) {
	t.Helper()
	st := storetest.OpenSQLite(t)
	blobs, err := blob.Disk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(Deps{Store: st, Blobs: blobs, Dialer: w.dialer(), Own: "chat.example", Log: quiet}), st
}

func pngImage(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 200, 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

const page = `<!doctype html><html><head>
<meta charset="utf-8">
<!-- <meta property="og:title" content="in a comment"> -->
<script>var x = '<meta property="og:title" content="in a script">';</script>
<title>Plain &amp; simple title</title>
<meta name="description" content="A plain description.">
<meta property="og:title" content="The &quot;Open Graph&quot; title">
<meta property='og:description' content='Described   in
two lines.'>
<meta content="Example Site" property="og:site_name"/>
<meta property=og:image content=/img/card.png>
</head><body><meta property="og:title" content="in the body"><h1>Hello</h1></body></html>`

func TestFetchCard(t *testing.T) {
	w := &world{}
	var agent, cookie, referer atomic.Value
	w.serve(t, "site.example", "93.184.216.34", http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		agent.Store(r.UserAgent())
		cookie.Store(r.Header.Get("Cookie") + r.Header.Get("Authorization"))
		referer.Store(r.Header.Get("Referer"))
		switch r.URL.Path {
		case "/article":
			rw.Header().Set("Content-Type", "text/html; charset=utf-8")
			rw.Header().Set("Set-Cookie", "tracked=yes")
			fmt.Fprint(rw, page)
		case "/img/card.png":
			rw.Header().Set("Content-Type", "image/png")
			rw.Write(pngImage(1440, 360))
		case "/untitled":
			rw.Header().Set("Content-Type", "text/html")
			fmt.Fprint(rw, "<html><head><meta name=description content=x></head></html>")
		case "/data.json":
			rw.Header().Set("Content-Type", "application/json")
			fmt.Fprint(rw, `{"title":"no"}`)
		case "/huge":
			rw.Header().Set("Content-Type", "text/html")
			fmt.Fprint(rw, "<html><head>"+strings.Repeat(" ", MaxPageBytes)+"<title>too far down</title></head></html>")
		case "/latin1":
			rw.Header().Set("Content-Type", "text/html; charset=ISO-8859-1")
			rw.Write([]byte("<title>Caf\xe9</title>"))
		default:
			http.NotFound(rw, r)
		}
	}))
	s, _ := newService(t, w)
	ctx := context.Background()

	card, img, err := s.Fetch(ctx, "http://site.example/article")
	if err != nil {
		t.Fatal(err)
	}
	if card.Title != `The "Open Graph" title` || card.Text != "Described in two lines." || card.Footer != "Example Site" ||
		card.Color != "grey" || card.URL != "http://site.example/article" {
		t.Errorf("card: %+v", card)
	}
	if img == nil || img.Width != 720 || img.Height != 180 {
		t.Errorf("the image is re-encoded to at most 720 on the long edge: %+v", img)
	}
	if agent.Load() != UserAgent || !strings.Contains(UserAgent, "Churr") {
		t.Errorf("User-Agent: %v", agent.Load())
	}
	if cookie.Load() != "" || referer.Load() != "" {
		t.Errorf("a cookie, credential or referrer was sent: %q %q", cookie.Load(), referer.Load())
	}

	if _, _, err := s.Fetch(ctx, "http://site.example/untitled"); err == nil {
		t.Errorf("a page with no title gives no card")
	}
	if _, _, err := s.Fetch(ctx, "http://site.example/data.json"); err == nil {
		t.Errorf("a page that is not text/html gives no card")
	}
	if _, _, err := s.Fetch(ctx, "http://site.example/huge"); err == nil {
		t.Errorf("only the first megabyte is read")
	}
	if _, _, err := s.Fetch(ctx, "http://site.example/missing"); err == nil {
		t.Errorf("a 404 gives no card")
	}
	card, _, err = s.Fetch(ctx, "http://site.example/latin1")
	if err != nil || card.Title != "Café" || card.Footer != "site.example" {
		t.Errorf("latin-1 page: %+v %v", card, err)
	}
}

// The guard: private, loopback and link-local addresses are refused after
// resolution and again after every redirect.
func TestGuardedDialer(t *testing.T) {
	w := &world{}
	// The target an attacker wants reached: a listener on this machine.
	var reached atomic.Int32
	secret := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		rw.Header().Set("Content-Type", "text/html")
		fmt.Fprint(rw, "<title>internal admin page</title>")
	}))
	defer secret.Close()
	_, secretPort, _ := net.SplitHostPort(secret.Listener.Addr().String())

	hops := 0
	w.serve(t, "public.example", "93.184.216.34", http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-loopback":
			http.Redirect(rw, r, "http://127.0.0.1/", http.StatusFound)
		case "/to-loopback-port":
			http.Redirect(rw, r, secret.URL+"/", http.StatusFound)
		case "/to-private-name":
			http.Redirect(rw, r, "http://intranet.example/", http.StatusFound)
		case "/to-metadata":
			http.Redirect(rw, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
		case "/to-file":
			http.Redirect(rw, r, "file:///etc/passwd", http.StatusFound)
		case "/loop":
			hops++
			http.Redirect(rw, r, "/loop", http.StatusFound)
		case "/twice":
			http.Redirect(rw, r, "/once", http.StatusFound)
		case "/once":
			http.Redirect(rw, r, "/page", http.StatusFound)
		case "/page":
			rw.Header().Set("Content-Type", "text/html")
			fmt.Fprint(rw, "<title>fine</title>")
		case "/image-inside":
			rw.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(rw, `<title>with an inside image</title><meta property="og:image" content="http://127.0.0.1:%s/logo.png">`, secretPort)
		}
	}))
	w.mu.Lock()
	w.names["intranet.example"] = []netip.Addr{netip.MustParseAddr("10.1.2.3")}
	w.names["rebind.example"] = []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("127.0.0.1")}
	w.names["mapped.example"] = []netip.Addr{netip.MustParseAddr("::ffff:127.0.0.1")}
	w.names["linklocal.example"] = []netip.Addr{netip.MustParseAddr("fe80::1")}
	// Even if something did listen there, these must never be connected to.
	w.route["127.0.0.1:80"] = secret.Listener.Addr().String()
	w.route["10.1.2.3:80"] = secret.Listener.Addr().String()
	w.route["169.254.169.254:80"] = secret.Listener.Addr().String()
	w.mu.Unlock()
	s, _ := newService(t, w)
	ctx := context.Background()

	refused := func(what, addr string) {
		t.Helper()
		card, _, err := s.Fetch(ctx, addr)
		if err == nil || card != nil {
			t.Errorf("%s: fetched %s, got %+v", what, addr, card)
		}
		if !errors.Is(err, ErrRefused) {
			t.Errorf("%s: refused for another reason: %v", what, err)
		}
	}
	// After resolution.
	refused("loopback literal", "http://127.0.0.1/")
	refused("loopback literal with the listener's port", secret.URL+"/")
	refused("IPv6 loopback", "http://[::1]/")
	refused("a private literal", "http://10.1.2.3/")
	refused("link-local: the metadata address", "http://169.254.169.254/latest/meta-data/")
	refused("a name that resolves to a private address", "http://intranet.example/")
	refused("a name with one public and one loopback address", "http://rebind.example/")
	refused("an IPv4-mapped loopback address", "http://mapped.example/")
	refused("IPv6 link-local", "http://linklocal.example/")
	refused("another port", "http://public.example:8080/page")
	refused("another scheme", "ftp://public.example/page")
	refused("credentials in the address", "http://user:pass@public.example/page")
	// Again after every redirect.
	refused("a redirect to loopback", "http://public.example/to-loopback")
	refused("a redirect to the local listener's own port", "http://public.example/to-loopback-port")
	refused("a redirect to a name that resolves privately", "http://public.example/to-private-name")
	refused("a redirect to the metadata address", "http://public.example/to-metadata")
	refused("a redirect to another scheme", "http://public.example/to-file")
	refused("more than three redirects", "http://public.example/loop")
	if hops != MaxRedirects+1 {
		t.Errorf("followed %d requests in a redirect loop, want %d", hops, MaxRedirects+1)
	}
	if n := reached.Load(); n != 0 {
		t.Fatalf("the internal listener was reached %d times", n)
	}
	w.mu.Lock()
	for _, d := range w.dials {
		if d != "93.184.216.34:80" {
			t.Errorf("a connection was made to %s", d)
		}
	}
	w.mu.Unlock()

	// What is allowed still works: two redirects on a public host.
	card, _, err := s.Fetch(ctx, "http://public.example/twice")
	if err != nil || card.Title != "fine" {
		t.Errorf("two redirects: %+v %v", card, err)
	}
	// The image is fetched under the same rules: the card comes without it.
	card, img, err := s.Fetch(ctx, "http://public.example/image-inside")
	if err != nil || card == nil || img != nil {
		t.Errorf("an image at a private address: %+v %v %v", card, img, err)
	}
	if n := reached.Load(); n != 0 {
		t.Fatalf("the internal listener was reached for an image")
	}

	// The real dialer, with nothing replaced, refuses the local listener
	// too: by its port, and with the port allowed, by its address.
	plain := New(Deps{Store: storetest.OpenSQLite(t), Dialer: &Dialer{Ports: []string{secretPort, "80"}}})
	if _, err := plain.dialer.DialContext(ctx, "tcp", secret.Listener.Addr().String()); !errors.Is(err, ErrRefused) {
		t.Errorf("the real dialer connected to loopback: %v", err)
	}
	if _, err := plain.dialer.DialContext(ctx, "tcp", "localhost:80"); !errors.Is(err, ErrRefused) {
		t.Errorf("the real dialer connected to localhost: %v", err)
	}
	if _, err := SafeDialer().DialContext(ctx, "tcp", "93.184.216.34:22"); !errors.Is(err, ErrRefused) {
		t.Errorf("a port other than 80 and 443: %v", err)
	}
	if n := reached.Load(); n != 0 {
		t.Fatalf("the internal listener was reached")
	}
	// The server's own addresses are refused like private ones.
	own := &Dialer{Own: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	if _, err := own.Check(ctx, "93.184.216.34"); !errors.Is(err, ErrRefused) {
		t.Errorf("the server's own address: %v", err)
	}
}

func TestLookupCaches(t *testing.T) {
	w := &world{}
	var pages, images atomic.Int32
	w.serve(t, "site.example", "93.184.216.34", http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			pages.Add(1)
			time.Sleep(50 * time.Millisecond)
			rw.Header().Set("Content-Type", "text/html")
			fmt.Fprint(rw, `<title>A</title><meta property="og:image" content="/a.png">`)
		case "/a.png":
			images.Add(1)
			rw.Write(pngImage(64, 64))
		default:
			pages.Add(1)
			http.NotFound(rw, r)
		}
	}))
	s, st := newService(t, w)
	ctx := context.Background()
	ann := model.Account{Username: "ann", DisplayName: "Ann", Role: model.RoleOwner}
	if _, err := st.Accounts().Create(ctx, &ann); err != nil {
		t.Fatal(err)
	}

	// Two requests for the same address at once share one fetch; the
	// fragment and the case of the host do not make it another address.
	var wg sync.WaitGroup
	results := make([]Preview, 4)
	for i, addr := range []string{"http://site.example/a", "http://SITE.example/a#top", "HTTP://site.example/a", "http://site.example/a"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := s.Lookup(ctx, addr, ann.ID)
			if err != nil {
				t.Error(err)
			}
			results[i] = p
		}()
	}
	wg.Wait()
	if pages.Load() != 1 || images.Load() != 1 {
		t.Errorf("fetched the page %d times and the image %d times, want once each", pages.Load(), images.Load())
	}
	for _, p := range results {
		if p.Card == nil || p.Card.Title != "A" || p.Image == nil || p.Card.ImageFileID != p.Image.ID {
			t.Fatalf("preview: %+v", p)
		}
	}
	img := results[0].Image
	lib, _ := st.Conversations().EnsureLibrary(ctx)
	if img.Purpose != model.PurposeCard || img.ConversationID != lib.ID || img.MessageID != "" {
		t.Errorf("the cached image is a card file in the library: %+v", img)
	}

	// A failure is cached too, for an hour.
	if p, err := s.Lookup(ctx, "http://site.example/gone", ann.ID); err != nil || p.Card != nil {
		t.Errorf("a failure: %+v %v", p, err)
	}
	before := pages.Load()
	s.Lookup(ctx, "http://site.example/gone", ann.ID)
	if pages.Load() != before {
		t.Errorf("a cached failure was fetched again")
	}
	// Links to this server, and addresses that break the rules, are not
	// looked up at all.
	for _, addr := range []string{"https://chat.example/c/ops", "http://127.0.0.1/", "mailto:a@b.example", "http://site.example:8080/a"} {
		if p, err := s.Lookup(ctx, addr, ann.ID); err != nil || p.Card != nil {
			t.Errorf("%s: %+v %v", addr, p, err)
		}
	}

	// An hour later the failure is asked for again and the card is not; a
	// day later both are, and the janitor removes what is stale.
	real := s.now
	s.now = func() model.Time { return real().Add(61 * time.Minute) }
	before = pages.Load()
	s.Lookup(ctx, "http://site.example/gone", ann.ID)
	s.Lookup(ctx, "http://site.example/a", ann.ID)
	if pages.Load() != before+1 {
		t.Errorf("after an hour: %d fetches, want 1", pages.Load()-before)
	}
	s.now = func() model.Time { return real().Add(25 * time.Hour) }
	n, err := s.Expire(ctx)
	// The card, the failure, and the loopback address, which was refused
	// and cached as a failure like any other.
	if err != nil || n != 3 {
		t.Errorf("expired %d entries (%v), want 3", n, err)
	}
	if _, err := st.Files().Get(ctx, img.ID); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("the cached image outlived its entry: %v", err)
	}
	if _, err := s.blobs.Get(ctx, img.BlobKey); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("the cached image's blob outlived its entry: %v", err)
	}

	// Fetches are rate-limited per posting account.
	s.now = real
	limited := false
	for i := 0; i < 30 && !limited; i++ {
		_, err := s.Lookup(ctx, fmt.Sprintf("http://site.example/n%d", i), ann.ID)
		limited = errors.Is(err, model.ErrRateLimited)
	}
	if !limited {
		t.Errorf("thirty fetches in a row by one account were all allowed")
	}
}

func TestEnabled(t *testing.T) {
	s, st := newService(t, &world{})
	ctx := context.Background()
	if s.Enabled(ctx) {
		t.Errorf("previews are off until an admin turns them on")
	}
	if err := st.Settings().Set(ctx, store.SettingPreviewsEnabled, true, ""); err != nil {
		t.Fatal(err)
	}
	if !s.Enabled(ctx) {
		t.Errorf("previews were turned on")
	}
}
