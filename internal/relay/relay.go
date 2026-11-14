// Package relay is the churr-relay program (docs/PUSH.md sections 5 and
// 7): it turns a wake-up from any Churr server into a request to Apple's
// or Google's push service. It holds the apps' push keys, and nothing
// else: no database, no accounts, no access log.
//
// One endpoint, POST /v1/wake, with a strict request shape: the push
// address, the platform, an event identifier the relay does not look
// inside, a collapse key, a priority and perhaps a badge number. There is
// no field for text and an unknown field is refused, so a server cannot
// send content by mistake and a changed relay cannot quietly start taking
// more.
//
// The relay forwards before it answers, so the answer says what happened.
// It keeps rate-limit buckets and counters in memory, and never logs a
// push address, an event identifier, a collapse key or a calling address.
package relay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/acme"
	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/version"
)

// maxBody is the largest request body: 8 KB.
const maxBody = 8 << 10

// Wake is one wake-up that passed the shape check.
type Wake struct {
	// PushAddress is the APNs or FCM token exactly as the device
	// registered it.
	PushAddress string
	EventID     string
	CollapseKey string
	// High is the priority: high or normal.
	High bool
	// Badge is the number for the app icon, or nil for none.
	Badge *int
}

// What sending a wake-up came to.
const (
	// Sent: Apple or Google accepted it. Not proof it reached the phone.
	Sent = "sent"
	// Gone: the address is no longer valid, or belongs to another app.
	Gone = "gone"
	// RateLimited: Apple or Google asked the relay to slow down for this
	// device.
	RateLimited = "rate_limited"
	// UpstreamError: Apple or Google could not be reached, or answered
	// with a server error.
	UpstreamError = "upstream_error"
	// KeyError: the relay's own key was refused. It is counted as an
	// upstream error, and logged loudly.
	KeyError = "key_error"
)

// Result is the outcome of one send.
type Result struct {
	Outcome string
	// RetryAfter is how long to wait, for RateLimited, if the service
	// said.
	RetryAfter time.Duration
}

// Sender sends wake-ups to one platform's push service. The APNs and FCM
// clients implement it; tests put fakes behind them or in their place.
type Sender interface {
	Send(ctx context.Context, w Wake) Result
}

// Options are for callers that are not the command line: tests.
type Options struct {
	// Listener is used instead of listening on the configured address.
	Listener net.Listener
	Log      *slog.Logger
	// APNs and FCM replace the clients made from the configuration.
	APNs Sender
	FCM  Sender
	// Now replaces the clock of the rate limits.
	Now func() time.Time
}

// Relay is one running relay.
type Relay struct {
	cfg  config.Relay
	log  *slog.Logger
	apns Sender
	fcm  Sender
	dry  bool

	// salt is random and made at start: the rate-limit buckets are keyed
	// by a salted hash, so a memory dump does not list push addresses.
	salt     []byte
	perMin   *ratelimit.Limiter
	perDay   *ratelimit.Limiter
	perAddr  *ratelimit.Limiter
	inflight atomic.Int64

	counters counters
	opts     Options
	http     *http.Server
}

// New builds the relay from its configuration, reading the Apple and
// Google keys if there are any.
func New(cfg config.Relay) (*Relay, error) { return NewWith(cfg, Options{}) }

// NewWith is New for tests.
func NewWith(cfg config.Relay, opts Options) (*Relay, error) {
	r := &Relay{cfg: cfg, log: opts.Log, apns: opts.APNs, fcm: opts.FCM, dry: cfg.DryRun, opts: opts, salt: make([]byte, 32)}
	if r.log == nil {
		o := &slog.HandlerOptions{Level: cfg.SlogLevel()}
		if cfg.LogFormat == "json" {
			r.log = slog.New(slog.NewJSONHandler(os.Stderr, o))
		} else {
			r.log = slog.New(slog.NewTextHandler(os.Stderr, o))
		}
	}
	if _, err := rand.Read(r.salt); err != nil {
		return nil, err
	}
	positive := func(n, def int) int {
		if n <= 0 {
			return def
		}
		return n
	}
	var clock []ratelimit.Option
	if opts.Now != nil {
		clock = append(clock, ratelimit.WithClock(opts.Now))
	}
	deviceRate, deviceDaily := positive(cfg.DeviceRate, 60), positive(cfg.DeviceDaily, 2000)
	addressRate := positive(cfg.AddressRate, 600)
	r.cfg.MaxInflight = positive(cfg.MaxInflight, 2000)
	// A bucket that has not been touched for as long as it takes to fill
	// is dropped: a minute for the per-minute limits, a day for the daily
	// one.
	r.perMin = ratelimit.New(ratelimit.Per(deviceRate, time.Minute), deviceRate, clock...)
	r.perDay = ratelimit.New(ratelimit.Per(deviceDaily, 24*time.Hour), deviceDaily, clock...)
	r.perAddr = ratelimit.New(ratelimit.Per(addressRate, time.Minute), addressRate, clock...)
	if r.apns == nil && cfg.APNsKey != "" {
		a, err := NewAPNs(APNsConfig{KeyFile: cfg.APNsKey, KeyID: cfg.APNsKeyID, TeamID: cfg.APNsTeamID, Topic: cfg.APNsTopic, Sandbox: cfg.APNsSandbox})
		if err != nil {
			return nil, err
		}
		r.apns = a
	}
	if r.fcm == nil && cfg.FCMCredentials != "" {
		f, err := NewFCM(FCMConfig{CredentialsFile: cfg.FCMCredentials})
		if err != nil {
			return nil, err
		}
		r.fcm = f
	}
	r.counters.start = time.Now()
	return r, nil
}

// Handler is everything the relay answers: POST /v1/wake, GET /healthz and
// GET /. There is no status page and no way to read the counters from
// outside.
func (r *Relay) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/wake", r.wake)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "ok")
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "This is a Churr push relay ("+version.String()+"). It passes content-free wake-ups from Churr servers to "+
			"Apple's and Google's push services so that phones know to ask their own server what happened. "+
			"It stores nothing and has nothing to show.\n")
	})
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if _, pattern := mux.Handler(req); pattern == "" {
			writeError(w, http.StatusNotFound, apiError{Code: "not_found", Message: "Not found."})
			return
		}
		mux.ServeHTTP(w, req)
	})
}

// apiError is the error shape of docs/CONVENTIONS.md, with the fields the
// relay adds.
type apiError struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	Field             string `json:"field,omitempty"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
	// Scope says which limit was hit: device or address.
	Scope string `json:"scope,omitempty"`
	// PlatformOff is true when the relay has no key for the platform.
	PlatformOff bool `json:"platform_off,omitempty"`
}

func writeError(w http.ResponseWriter, status int, e apiError) {
	b, _ := json.Marshal(map[string]any{"error": e})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if e.RetryAfterSeconds > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(e.RetryAfterSeconds))
	}
	w.WriteHeader(status)
	w.Write(b)
}

// hashed is the rate-limit key of a push address or a calling address.
func (r *Relay) hashed(kind, value string) string {
	h := sha256.New()
	h.Write(r.salt)
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// caller is the calling address limits are counted against: an IPv4
// address, or the /64 of an IPv6 one. Behind a proxy it is taken from
// X-Forwarded-For, believed only from the trusted proxies.
func (r *Relay) caller(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	addr = addr.Unmap()
	for _, p := range r.cfg.TrustedProxies {
		if p.Contains(addr) {
			if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
				parts := strings.Split(xff, ",")
				if fwd, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
					addr = fwd.Unmap()
				}
			}
			break
		}
	}
	if addr.Is6() {
		if prefix, err := addr.Prefix(64); err == nil {
			return prefix.String()
		}
	}
	return addr.String()
}

// wake is POST /v1/wake.
func (r *Relay) wake(w http.ResponseWriter, req *http.Request) {
	r.counters.received()
	if n := r.inflight.Add(1); n > int64(r.cfg.MaxInflight) {
		r.inflight.Add(-1)
		writeError(w, http.StatusServiceUnavailable, apiError{Code: "unavailable", Message: "The relay is overloaded. Try again shortly."})
		return
	}
	defer r.inflight.Add(-1)

	// The calling address's limit comes first: it costs nothing to check
	// and protects everything after it.
	if res := r.perAddr.Take(r.hashed("address", r.caller(req))); !res.OK {
		r.counters.result("", RateLimited)
		writeError(w, http.StatusTooManyRequests, apiError{Code: "rate_limited", Message: "Too many requests from this address.",
			RetryAfterSeconds: res.RetryAfterSeconds(), Scope: "address"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, maxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			r.counters.result("", invalid)
			writeError(w, http.StatusRequestEntityTooLarge, apiError{Code: "too_large", Message: "A wake-up is at most 8 KB."})
			return
		}
		r.counters.result("", invalid)
		writeError(w, http.StatusBadRequest, apiError{Code: "invalid_request", Message: "The request could not be read."})
		return
	}
	wake, platform, bad := parseWake(req.Header.Get("Content-Type"), body)
	if bad != nil {
		r.counters.result(platform, invalid)
		writeError(w, http.StatusBadRequest, *bad)
		return
	}
	// The per-device limits: a minute and a day.
	key := r.hashed("device", wake.PushAddress)
	for _, l := range []*ratelimit.Limiter{r.perMin, r.perDay} {
		if res := l.Take(key); !res.OK {
			r.counters.result(platform, RateLimited)
			writeError(w, http.StatusTooManyRequests, apiError{Code: "rate_limited", Message: "Too many wake-ups for this device.",
				RetryAfterSeconds: res.RetryAfterSeconds(), Scope: "device"})
			return
		}
	}
	sender := r.apns
	if platform == "android" {
		sender = r.fcm
	}
	var res Result
	switch {
	case r.dry:
		// Development: accepted and counted, sent nowhere.
		res = Result{Outcome: Sent}
	case sender == nil:
		r.counters.result(platform, UpstreamError)
		writeError(w, http.StatusServiceUnavailable, apiError{Code: "unavailable", Message: "This relay has no key for that platform.", PlatformOff: true})
		return
	default:
		ctx, cancel := context.WithTimeout(req.Context(), 9*time.Second)
		res = sender.Send(ctx, wake)
		cancel()
	}
	switch res.Outcome {
	case Sent:
		r.counters.result(platform, Sent)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		io.WriteString(w, `{"result":"sent"}`)
	case Gone:
		r.counters.result(platform, Gone)
		writeError(w, http.StatusGone, apiError{Code: "gone", Message: "That push address is no longer valid for this app."})
	case RateLimited:
		r.counters.result(platform, RateLimited)
		secs := int((res.RetryAfter + time.Second - 1) / time.Second)
		if secs <= 0 {
			secs = 60
		}
		writeError(w, http.StatusTooManyRequests, apiError{Code: "rate_limited", Message: "The push service asked for fewer wake-ups to this device.",
			RetryAfterSeconds: secs, Scope: "device"})
	case KeyError:
		// The relay's own key is wrong. Nobody but its operator can fix
		// that, so it is said loudly, without naming any device.
		r.counters.result(platform, UpstreamError)
		r.log.Error("the push service refused this relay's own key; wake-ups for the platform are failing", "platform", platform)
		writeError(w, http.StatusServiceUnavailable, apiError{Code: "unavailable", Message: "The relay cannot reach the push service just now."})
	default:
		r.counters.result(platform, UpstreamError)
		writeError(w, http.StatusBadGateway, apiError{Code: "unavailable", Message: "The push service could not be reached, or failed."})
	}
}

// Run listens and serves until ctx ends, and logs one line of counters
// each hour.
func (r *Relay) Run(ctx context.Context) error {
	srv := &http.Server{Handler: r.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 20 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 16 << 10,
		// The standard library's own log would name calling addresses.
		ErrorLog: slog.NewLogLogger(discard{}, slog.LevelError)}
	r.http = srv
	errc := make(chan error, 2)
	var plain *http.Server
	switch {
	case r.opts.Listener != nil:
		go func() { errc <- srv.Serve(r.opts.Listener) }()
	case r.cfg.BehindProxy:
		ln, err := net.Listen("tcp", r.cfg.HTTP)
		if err != nil {
			return err
		}
		go func() { errc <- srv.Serve(ln) }()
	default:
		certs, err := acme.New(r.cfg.Common, r.log)
		if err != nil {
			return err
		}
		ln, err := net.Listen("tcp", r.cfg.HTTPS)
		if err != nil {
			return err
		}
		srv.TLSConfig = certs.TLSConfig()
		_, port, _ := net.SplitHostPort(r.cfg.HTTPS)
		plain = &http.Server{Addr: r.cfg.HTTP, Handler: certs.HTTPHandler(port), ReadHeaderTimeout: 10 * time.Second}
		go func() { errc <- srv.ServeTLS(ln, "", "") }()
		go func() {
			if err := plain.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
		go certs.Warm(ctx)
	}
	mode := "sending"
	if r.dry {
		mode = "dry run: wake-ups are accepted and counted, and nothing is sent"
	}
	r.log.Info("the relay has started", "version", version.String(), "apns", r.apns != nil, "fcm", r.fcm != nil, "mode", mode)
	hourly := time.NewTicker(time.Hour)
	defer hourly.Stop()
	for {
		select {
		case <-hourly.C:
			r.LogCounters()
		case err := <-errc:
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			return err
		case <-ctx.Done():
			shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			srv.Shutdown(shut)
			if plain != nil {
				plain.Shutdown(shut)
			}
			cancel()
			r.LogCounters()
			r.log.Info("the relay has stopped")
			return nil
		}
	}
}

type discard struct{ slog.Handler }

func (discard) Enabled(context.Context, slog.Level) bool  { return false }
func (discard) Handle(context.Context, slog.Record) error { return nil }
func (d discard) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discard) WithGroup(string) slog.Handler           { return d }
