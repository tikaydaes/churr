package preview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/store"
)

// The limits of docs/FEATURES.md section 23.3, exactly.
const (
	// MaxRedirects is how many redirects are followed.
	MaxRedirects = 3
	// PageTimeout is the time for the page in all; ImageTimeout for the
	// image.
	PageTimeout  = 5 * time.Second
	ImageTimeout = 5 * time.Second
	// MaxPageBytes of the page are read and the rest ignored.
	MaxPageBytes = 1 << 20
	// MaxImageBytes is the largest image fetched.
	MaxImageBytes = 5 << 20
	// MaxAtOnce is how many fetches run at once for the whole server; one
	// runs at a time per host.
	MaxAtOnce = 4
	// UserAgent names Churr and not the server.
	UserAgent = "Churr-LinkPreview/1.0"

	// CardFresh is how long a cached card answers, and EmptyFresh how long
	// a cached failure does (section 23.2).
	CardFresh  = 24 * time.Hour
	EmptyFresh = time.Hour

	maxTitle = 200
	maxText  = 300
)

// Deps is what a Service is built from.
type Deps struct {
	Store store.Store
	Blobs blob.Blobs
	// Dialer is the guarded dialer; nil means SafeDialer().
	Dialer *Dialer
	// Own is the server's own host name: links to it get no preview.
	Own string
	Log *slog.Logger
}

// Service looks previews up: in the cache, or by fetching.
type Service struct {
	store  store.Store
	blobs  blob.Blobs
	dialer *Dialer
	own    string
	log    *slog.Logger
	client *http.Client
	now    func() model.Time

	slots   chan struct{}      // MaxAtOnce fetches for the server
	limiter *ratelimit.Limiter // fetches per posting account

	mu     sync.Mutex
	hosts  map[string]*sync.Mutex // one fetch per host
	flight map[string]*call       // two requests for one address share a fetch
}

type call struct {
	done chan struct{}
	out  Preview
	err  error
}

// Preview is what a look-up gives: the card, and the cache's own image file
// for the caller to copy onto the message. Card is nil when the address has
// nothing usable.
type Preview struct {
	Card  *model.Card
	Image *model.File
}

// New makes the service.
func New(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Dialer == nil {
		d.Dialer = SafeDialer()
	}
	s := &Service{store: d.Store, blobs: d.Blobs, dialer: d.Dialer, own: strings.ToLower(d.Own), log: d.Log, now: model.Now,
		slots: make(chan struct{}, MaxAtOnce), hosts: map[string]*sync.Mutex{}, flight: map[string]*call{},
		// A burst of ten, then one every six seconds, for each account.
		limiter: ratelimit.New(ratelimit.Per(10, time.Minute), 10)}
	s.client = &http.Client{
		Transport: &http.Transport{
			// Never through a proxy from the environment: the proxy would
			// do the connecting, and the guard would check the proxy.
			Proxy:       nil,
			DialContext: d.Dialer.DialContext,
			// One connection per request, so every request, each redirect
			// included, is dialled and checked afresh.
			DisableKeepAlives:      true,
			TLSHandshakeTimeout:    5 * time.Second,
			ResponseHeaderTimeout:  5 * time.Second,
			MaxResponseHeaderBytes: 64 << 10,
		},
		// No cookie jar: nothing is remembered between requests.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > MaxRedirects {
				return fmt.Errorf("%w: more than %d redirects", ErrRefused, MaxRedirects)
			}
			// Each target goes through every rule again; the dialer
			// checks its address when the connection is made.
			if _, err := checkURL(req.URL); err != nil {
				return err
			}
			req.Header.Del("Referer")
			return nil
		},
	}
	return s
}

// Enabled reports whether an admin has turned link previews on.
func (s *Service) Enabled(ctx context.Context) bool {
	on, err := store.SettingBool(ctx, s.store, store.SettingPreviewsEnabled, false)
	return err == nil && on
}

// checkURL applies the rule on schemes and ports: http on 80, https on 443,
// nothing else, and no credentials in the address.
func checkURL(u *url.URL) (*url.URL, error) {
	if u.User != nil {
		return nil, fmt.Errorf("%w: the address carries credentials", ErrRefused)
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("%w: no host", ErrRefused)
	}
	switch {
	case u.Scheme == "http" && (u.Port() == "" || u.Port() == "80"):
	case u.Scheme == "https" && (u.Port() == "" || u.Port() == "443"):
	default:
		return nil, fmt.Errorf("%w: only http on port 80 and https on port 443", ErrRefused)
	}
	return u, nil
}

// Normalize puts an address in the form the cache keys on: scheme and host
// in lower case, the fragment dropped. It refuses what checkURL refuses.
func Normalize(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%w: not an address", ErrRefused)
	}
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	u.Fragment, u.RawFragment = "", ""
	if _, err := checkURL(u); err != nil {
		return "", err
	}
	return u.String(), nil
}

// Lookup returns the preview of an address for a message posted by an
// account: from the cache while it is fresh, otherwise by fetching and
// caching. An address that gives nothing has a nil Card and no error; an
// error means the look-up itself could not be made (the database, or the
// account is asking too fast).
func (s *Service) Lookup(ctx context.Context, raw string, by model.AccountID) (Preview, error) {
	addr, err := Normalize(raw)
	if err != nil {
		return Preview{}, nil
	}
	if u, _ := url.Parse(addr); s.own != "" && strings.EqualFold(u.Hostname(), s.own) {
		return Preview{}, nil // links to this server get no preview
	}
	sum := sha256.Sum256([]byte(addr))
	hash := sum[:]
	if p, fresh, err := s.cached(ctx, hash); err != nil || fresh {
		return p, err
	}
	if !s.limiter.Take(string(by)).OK {
		return Preview{}, model.ErrRateLimited
	}
	// Two requests for the same address at once share one fetch.
	key := string(hash)
	s.mu.Lock()
	if c := s.flight[key]; c != nil {
		s.mu.Unlock()
		select {
		case <-c.done:
			return c.out, c.err
		case <-ctx.Done():
			return Preview{}, ctx.Err()
		}
	}
	c := &call{done: make(chan struct{})}
	s.flight[key] = c
	s.mu.Unlock()
	c.out, c.err = s.fill(ctx, addr, hash, by)
	s.mu.Lock()
	delete(s.flight, key)
	s.mu.Unlock()
	close(c.done)
	return c.out, c.err
}

// cached reads the cache. fresh is false when there is no row or it is too
// old to answer.
func (s *Service) cached(ctx context.Context, hash []byte) (Preview, bool, error) {
	e, found, err := s.store.Previews().Get(ctx, hash)
	if err != nil || !found {
		return Preview{}, false, err
	}
	age := s.now().Sub(e.FetchedAt)
	if e.Card == nil {
		return Preview{}, age < EmptyFresh, nil
	}
	if age >= CardFresh {
		return Preview{}, false, nil
	}
	p := Preview{Card: e.Card}
	if e.Card.ImageFileID != "" {
		if f, err := s.store.Files().Get(ctx, e.Card.ImageFileID); err == nil {
			p.Image = &f
		}
	}
	return p, true, nil
}

// fill fetches an address and writes the cache row, whatever came of it.
func (s *Service) fill(ctx context.Context, addr string, hash []byte, by model.AccountID) (Preview, error) {
	card, image, err := s.Fetch(ctx, addr)
	if err != nil {
		// A failure of any rule gives a null card. The reason is for the
		// operator and is not shown to members.
		s.log.Info("no link preview", "host", hostOf(addr), "reason", err.Error())
	}
	var out Preview
	entry := store.PreviewEntry{Hash: hash, URL: addr, FetchedAt: s.now()}
	if card != nil {
		if image != nil {
			if f, err := s.keepImage(ctx, image, by); err != nil {
				s.log.Warn("a preview image could not be stored", "err", err)
			} else {
				card.ImageFileID, out.Image = f.ID, &f
			}
		}
		entry.Card, out.Card = card, card
	}
	old, replaced, err := s.store.Previews().Put(ctx, entry)
	if err != nil {
		if out.Image != nil {
			s.dropImage(ctx, out.Image.ID)
		}
		return Preview{}, err
	}
	if replaced && old.Card != nil && old.Card.ImageFileID != "" {
		s.dropImage(ctx, old.Card.ImageFileID)
	}
	return out, nil
}

// keepImage stores a fetched image as a file of purpose `card` in the
// library conversation, owned by the cache row.
func (s *Service) keepImage(ctx context.Context, t *blob.Thumb, by model.AccountID) (model.File, error) {
	lib, err := s.store.Conversations().EnsureLibrary(ctx)
	if err != nil {
		return model.File{}, err
	}
	key, size, sum, err := s.blobs.Put(ctx, bytes.NewReader(t.Data))
	if err != nil {
		return model.File{}, err
	}
	name := "preview.jpg"
	if t.MediaType == blob.TypePNG {
		name = "preview.png"
	}
	f := model.File{ConversationID: lib.ID, UploaderID: by, Purpose: model.PurposeCard, Name: name, Size: size, MediaType: t.MediaType,
		Kind: model.FileImage, Width: t.Width, Height: t.Height, SHA256: sum, BlobKey: key, CreatedAt: s.now()}
	if err := s.store.Files().Insert(ctx, &f); err != nil {
		s.blobs.Delete(ctx, key)
		return model.File{}, err
	}
	return f, nil
}

// dropImage deletes a cache image: its row, then its blob.
func (s *Service) dropImage(ctx context.Context, id model.FileID) {
	f, err := s.store.Files().Delete(ctx, id)
	if err != nil {
		return
	}
	for _, k := range store.BlobKeys(f) {
		if err := s.blobs.Delete(ctx, k); err != nil {
			s.log.Warn("a preview image's blob could not be removed", "err", err)
		}
	}
}

// Expire deletes cache rows that are past their time, and their images:
// 24 hours after the fetch, one hour for a failure. The janitor calls it.
func (s *Service) Expire(ctx context.Context) (int, error) {
	now := s.now()
	n := 0
	for {
		stale, err := s.store.Previews().Stale(ctx, now.Add(-CardFresh), now.Add(-EmptyFresh), 200)
		if err != nil || len(stale) == 0 {
			return n, err
		}
		for _, e := range stale {
			if err := s.store.Previews().Delete(ctx, e.Hash); err != nil {
				return n, err
			}
			if e.Card != nil && e.Card.ImageFileID != "" {
				s.dropImage(ctx, e.Card.ImageFileID)
			}
			n++
		}
	}
}

func hostOf(addr string) string {
	u, err := url.Parse(addr)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// hold takes one of the server's fetch slots and the host's turn, and
// returns the function that gives both back.
func (s *Service) hold(ctx context.Context, host string) (func(), error) {
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	s.mu.Lock()
	m := s.hosts[host]
	if m == nil {
		m = &sync.Mutex{}
		s.hosts[host] = m
		if len(s.hosts) > 10000 {
			s.hosts = map[string]*sync.Mutex{host: m}
		}
	}
	s.mu.Unlock()
	m.Lock()
	return func() {
		m.Unlock()
		<-s.slots
	}, nil
}

// get makes one guarded GET and returns at most max bytes of the body,
// with the address the answer finally came from. With exact set, a body
// longer than max is an error and not cut.
func (s *Service) get(ctx context.Context, addr string, max int64, exact bool) (body []byte, header http.Header, final *url.URL, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	if _, err := checkURL(req.URL); err != nil {
		return nil, nil, nil, err
	}
	// A plain GET: a fixed User-Agent, no cookies, no credentials, no
	// referrer.
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,image/*;q=0.9,*/*;q=0.1")
	res, err := s.client.Do(req)
	if err != nil {
		return nil, nil, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, nil, nil, fmt.Errorf("preview: %s answered %d", req.URL.Hostname(), res.StatusCode)
	}
	body, err = io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil && len(body) == 0 {
		return nil, nil, nil, err
	}
	if int64(len(body)) > max {
		if exact {
			return nil, nil, nil, fmt.Errorf("preview: more than %d bytes", max)
		}
		body = body[:max]
	}
	return body, res.Header, res.Request.URL, nil
}

// Fetch reads the page at an address under the rules of section 23.3 and
// makes its card, with the page's image re-encoded if it has one. A page
// with no title gives an error and no card. The card's image is returned
// beside it, not stored.
func (s *Service) Fetch(ctx context.Context, addr string) (*model.Card, *blob.Thumb, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return nil, nil, err
	}
	if _, err := checkURL(u); err != nil {
		return nil, nil, err
	}
	release, err := s.hold(ctx, strings.ToLower(u.Hostname()))
	if err != nil {
		return nil, nil, err
	}
	defer release()

	pageCtx, cancel := context.WithTimeout(ctx, PageTimeout)
	body, header, final, err := s.get(pageCtx, addr, MaxPageBytes, false)
	cancel()
	if err != nil {
		return nil, nil, err
	}
	mediaType, params, _ := mime.ParseMediaType(header.Get("Content-Type"))
	if mediaType != "text/html" {
		return nil, nil, fmt.Errorf("preview: the page is %q, not text/html", mediaType)
	}
	page := readHead(decode(body, params["charset"]))
	title := clip(first(page["og:title"], page["title"]), maxTitle)
	if title == "" {
		return nil, nil, errors.New("preview: the page has no title")
	}
	card := &model.Card{
		Title:  title,
		Text:   clip(first(page["og:description"], page["description"]), maxText),
		URL:    addr,
		Footer: clip(first(page["og:site_name"], u.Hostname()), maxTitle),
		Color:  "grey",
	}
	img := page["og:image"]
	if img == "" {
		return card, nil, nil
	}
	imgURL, err := final.Parse(img)
	if err != nil {
		return card, nil, nil
	}
	imgCtx, cancel := context.WithTimeout(ctx, ImageTimeout)
	defer cancel()
	data, _, _, err := s.get(imgCtx, imgURL.String(), MaxImageBytes, true)
	if err != nil {
		s.log.Info("no image for a link preview", "host", imgURL.Hostname(), "reason", err.Error())
		return card, nil, nil
	}
	// Re-encoded to at most 720 pixels on the long edge with metadata
	// removed; anything that does not decode as PNG, JPEG, GIF or WebP is
	// dropped.
	thumb, err := blob.MakeThumbnail(bytes.NewReader(data))
	if err != nil {
		return card, nil, nil
	}
	return card, thumb, nil
}

func first(vs ...string) string {
	for _, v := range vs {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// clip makes plain one-line text of at most n characters.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// decode turns a page's bytes into text. UTF-8 is assumed; the two
// single-byte encodings old pages still declare are converted, and anything
// else has its invalid bytes dropped.
func decode(body []byte, charset string) string {
	charset = strings.ToLower(strings.TrimSpace(charset))
	if charset == "" && !utf8.Valid(body) {
		// A page that does not say and is not UTF-8: look for its own
		// declaration.
		low := strings.ToLower(string(body[:min(len(body), 2048)]))
		for _, c := range []string{"iso-8859-1", "windows-1252", "latin1"} {
			if strings.Contains(low, "charset="+c) || strings.Contains(low, `charset="`+c) {
				charset = c
			}
		}
	}
	switch charset {
	case "iso-8859-1", "latin1", "windows-1252", "cp1252":
		r := make([]rune, len(body))
		for i, c := range body {
			r[i] = rune(c)
		}
		return string(r)
	}
	return strings.ToValidUTF8(string(body), "")
}

// readHead reads what a preview needs from an HTML page: the Open Graph
// tags, the description meta tag and the title. It is a small scanner over
// tags, not an HTML parser: it reads <title> and <meta> up to the end of
// the head and nothing else, and what it finds is only ever shown as plain
// text.
func readHead(page string) map[string]string {
	out := map[string]string{}
	low := strings.ToLower(page)
	for i := 0; i < len(page); {
		lt := strings.IndexByte(page[i:], '<')
		if lt < 0 {
			break
		}
		i += lt
		switch {
		case strings.HasPrefix(low[i:], "<!--"):
			end := strings.Index(low[i:], "-->")
			if end < 0 {
				return out
			}
			i += end + 3
		case strings.HasPrefix(low[i:], "</head"), strings.HasPrefix(low[i:], "<body"):
			return out
		case strings.HasPrefix(low[i:], "<script"), strings.HasPrefix(low[i:], "<style"):
			name := "</script"
			if strings.HasPrefix(low[i:], "<style") {
				name = "</style"
			}
			end := strings.Index(low[i:], name)
			if end < 0 {
				return out
			}
			i += end + len(name)
		case strings.HasPrefix(low[i:], "<title"):
			gt := strings.IndexByte(page[i:], '>')
			end := strings.Index(low[i:], "</title")
			if gt < 0 || end < gt {
				return out
			}
			if _, have := out["title"]; !have {
				out["title"] = html.UnescapeString(page[i+gt+1 : i+end])
			}
			i += end + 7
		case strings.HasPrefix(low[i:], "<meta"):
			gt := tagEnd(page[i:])
			if gt < 0 {
				return out
			}
			attrs := attributes(page[i+5 : i+gt])
			key := strings.ToLower(first(attrs["property"], attrs["name"]))
			switch key {
			case "og:title", "og:description", "og:image", "og:site_name", "description":
				if _, have := out[key]; !have && attrs["content"] != "" {
					out[key] = attrs["content"]
				}
			}
			i += gt + 1
		default:
			i++
		}
	}
	return out
}

// tagEnd finds the '>' that ends a tag, skipping any inside quoted values.
func tagEnd(s string) int {
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return i
		}
	}
	return -1
}

// attributes reads the attributes of a tag: name="value", name='value' or
// name=value, names in lower case and values unescaped.
func attributes(s string) map[string]string {
	out := map[string]string{}
	ws := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }
	i := 0
	for i < len(s) {
		// Between attributes: spaces, and the slash of a self-closing tag.
		for i < len(s) && (ws(s[i]) || s[i] == '/') {
			i++
		}
		start := i
		for i < len(s) && !ws(s[i]) && s[i] != '=' && s[i] != '/' {
			i++
		}
		name := strings.ToLower(s[start:i])
		for i < len(s) && ws(s[i]) {
			i++
		}
		if name == "" {
			i++
			continue
		}
		if i >= len(s) || s[i] != '=' {
			out[name] = ""
			continue
		}
		i++
		for i < len(s) && ws(s[i]) {
			i++
		}
		var value string
		if i < len(s) && (s[i] == '"' || s[i] == '\'') {
			q := s[i]
			end := strings.IndexByte(s[i+1:], q)
			if end < 0 {
				value, i = s[i+1:], len(s)
			} else {
				value, i = s[i+1:i+1+end], i+end+2
			}
		} else {
			start := i
			for i < len(s) && !ws(s[i]) {
				i++
			}
			value = s[start:i]
		}
		if _, have := out[name]; !have {
			out[name] = html.UnescapeString(value)
		}
	}
	return out
}
