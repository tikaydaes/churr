// Package server builds a running server out of the other packages and
// connects them. The commands in cmd/churr are thin wrappers over it, and
// tests start whole servers with it. README.md in this directory describes
// how the packages are wired together and what a feature package provides.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/tikaydaes/churr/internal/api"
	"github.com/tikaydaes/churr/internal/auth"
	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/bots"
	"github.com/tikaydaes/churr/internal/calls"
	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/forms"
	"github.com/tikaydaes/churr/internal/janitor"
	"github.com/tikaydaes/churr/internal/link"
	"github.com/tikaydaes/churr/internal/live"
	"github.com/tikaydaes/churr/internal/memguard"
	"github.com/tikaydaes/churr/internal/mod"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/notify"
	"github.com/tikaydaes/churr/internal/people"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/preview"
	"github.com/tikaydaes/churr/internal/push"
	"github.com/tikaydaes/churr/internal/sched"
	"github.com/tikaydaes/churr/internal/sealed"
	"github.com/tikaydaes/churr/internal/search"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
	_ "github.com/tikaydaes/churr/internal/store/postgres" // makes store.Open able to open PostgreSQL
	_ "github.com/tikaydaes/churr/internal/store/sqlite"   // and SQLite
	"github.com/tikaydaes/churr/internal/timing"
	"github.com/tikaydaes/churr/internal/web"
)

// Options are for callers that are not the command line: tests.
type Options struct {
	// Listener is used instead of listening on cfg.HTTP.
	Listener net.Listener
	// Log replaces the logger made from the configuration.
	Log *slog.Logger
	// Live tunes the WebSocket hub; tests shorten its times.
	Live live.Options
	// RatePerSecond and RateBurst replace the default per-account limit.
	RatePerSecond float64
	RateBurst     int
	// BotLimits replaces the limits on bots and webhooks of docs/BOTS.md
	// section 8.
	BotLimits *bots.Limits
	// HTTPClient is used to reach identity providers. Tests give one that
	// trusts their in-process provider.
	HTTPClient *http.Client
	// PushRelayHTTP and PushEndpointHTTP replace the clients wake-ups
	// are sent with, and PushRetry the waits between attempts; tests
	// point them at in-process relays and endpoints.
	PushRelayHTTP    *http.Client
	PushEndpointHTTP *http.Client
	PushRetry        []time.Duration
	// LinkHTTPClient carries the link's dials to other servers and hubs.
	// Tests give one that takes their made-up host names to loopback. Nil
	// means HTTPClient.
	LinkHTTPClient *http.Client
	// LinkNow replaces the link's clock, for tests of servers whose clocks
	// disagree. Nil means the system's.
	LinkNow func() model.Time
	// CallTimings replaces the durations of docs/CALLS.md section 12, so
	// tests need not wait a minute for a ticket to run out.
	CallTimings calls.Timings
	// MemorySource replaces where the memory guard reads how much memory
	// there is (memory.go). Nil means the system's.
	MemorySource memguard.Source
}

// EnvLiveWrites names the environment variable that pins how many writes a
// second the server makes to its live connections' sockets in all: a
// number, or "off" for no pace at all. Unset, the server finds the pace
// itself, and sets one only when the machine is losing packets on the way
// out (docs/OPERATIONS.md).
const EnvLiveWrites = "CHURR_LIVE_WRITES_PER_SECOND"

// Server is one running Churr server. The fields are the services it is
// made of, exported so tests and commands can reach them; nothing else
// should.
type Server struct {
	Config config.Serve
	Log    *slog.Logger
	Store  store.Store
	Keys   *secret.Keys
	Blobs  blob.Blobs
	Perm   *perm.Checker
	Live   *live.Hub
	People *people.Service
	Notify *notify.Service
	Chat   *chat.Service
	Search *search.Service
	Auth   *auth.Service
	API    *api.API

	// Forms, Sched and Commands are forms and polls, the scheduler, and
	// saved replies with the command menu (forms.go).
	Forms    *forms.Service
	Sched    *sched.Service
	Commands *sched.Commands
	// Bots is bots, their commands and interactions, and webhooks.
	Bots *bots.Service
	// Calls is the calls service and the media server behind it (calls.go).
	Calls *calls.Server
	// Link is the link to other servers (link.go).
	Link *link.Service
	// Mod, Previews and Janitor are moderation, link previews and the
	// hourly clean-up (chatmore.go).
	Mod      *mod.Service
	Previews *preview.Service
	Janitor  *janitor.Janitor
	// Sealed is what the server keeps for sealed chats; Push is devices
	// and phone wake-ups (sealed.go).
	Sealed *sealed.Service
	Push   *push.Service
	// Memory is the guard that refuses and sheds live connections when
	// memory runs short (memory.go). Nil where there is nothing to read.
	Memory *memguard.Guard
	// stopSched stops the scheduler's loop, once it has been started.
	stopSched context.CancelFunc

	// SetupLink is the one-time link that makes the owner, made at this
	// start; "" once an owner exists, or when SetupLinkKept.
	SetupLink string
	// SetupLinkKept is true when the server has no owner and the setup
	// link made at an earlier start is unused: it still works, but only
	// its hash is stored, so it cannot be shown again.
	SetupLinkKept bool

	opts Options
	http *http.Server
}

// New builds every service and connects them. It opens the data directory
// and the database, runs migrations, and creates what every server has: the
// two built-in groups and the library conversation.
func New(cfg config.Serve) (*Server, error) { return NewWith(cfg, Options{}) }

// NewWith is New for tests.
func NewWith(cfg config.Serve, opts Options) (*Server, error) {
	ctx := context.Background()
	s := &Server{Config: cfg, Log: opts.Log, opts: opts}
	if s.Log == nil {
		s.Log = newLogger(cfg)
	}
	for _, dir := range []string{cfg.Data, cfg.FilesDir(), cfg.TmpDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("making the data directory: %w", err)
		}
	}
	var err error
	if s.Keys, err = secret.Open(cfg.KeysDir()); err != nil {
		return nil, err
	}
	if s.Store, err = store.Open(ctx, cfg.Data, cfg.DB); err != nil {
		return nil, fmt.Errorf("opening the database: %w", err)
	}
	fail := func(err error) (*Server, error) {
		s.Store.Close()
		return nil, err
	}
	if err := s.Store.Groups().EnsureBuiltin(ctx); err != nil {
		return fail(err)
	}
	if _, err := s.Store.Conversations().EnsureLibrary(ctx); err != nil {
		return fail(err)
	}
	// Disk, object storage or both, as the files.store setting says
	// (chatmore.go).
	if s.Blobs, err = s.openBlobs(); err != nil {
		return fail(err)
	}

	// The layers, bottom up (docs/STRUCTURE.md section 3). Each service is
	// given the ones below it; where a lower one needs something from a
	// higher one, it declares a small interface and is handed the
	// implementation here.
	s.Perm = perm.New(s.Store)
	opts.Live.Log = s.Log
	// The pace of writes to live connections is the hub's to find
	// (live/pace.go); an operator may pin it, or turn it off.
	if v := os.Getenv(EnvLiveWrites); v != "" && opts.Live.WritesPerSecond == 0 {
		switch n, err := strconv.Atoi(v); {
		case v == "off":
			opts.Live.WritesPerSecond = -1
		case err == nil && n > 0:
			opts.Live.WritesPerSecond = n
		default:
			s.Log.Warn(EnvLiveWrites+" is neither a number of writes a second nor \"off\"; it is ignored", "value", v)
		}
	}
	// Short of memory, the hub turns new connections away (memory.go).
	if s.Memory = s.newMemoryGuard(); s.Memory != nil && opts.Live.Admit == nil {
		opts.Live.Admit = s.Memory.AdmitWait
	}
	s.Live = live.New(s.Perm, s.Store, opts.Live)
	s.People = people.New(s.Store, s.Perm, s.Live, cfg.Origin())
	s.Chat = chat.New(chat.Deps{Store: s.Store, Perm: s.Perm, Hub: s.Live, Blobs: s.Blobs, Log: s.Log})
	// live fills in the per-reader parts of events through chat, which is
	// built after it.
	s.Live.SetTailor(s.Chat)
	// notify, and what people needs of the services above it (people.go).
	s.wirePeople()
	// forms, the scheduler and commands (forms.go).
	s.wireForms()
	// bots, their commands and interactions, and webhooks (bots.go).
	s.wireBots()
	// moderation, link previews and the janitor (chatmore.go).
	s.wireModeration()
	// calls: signalling and the media port (calls.go).
	if err := s.wireCalls(); err != nil {
		return fail(err)
	}
	// the link to other servers (link.go).
	if err := s.wireLink(); err != nil {
		return fail(err)
	}
	s.Search = search.New(s.Store, s.Perm)
	if s.Auth, err = auth.New(auth.Deps{
		Store: s.Store, Perm: s.Perm, People: s.People, Chat: s.Chat, Hub: s.Live, Log: s.Log,
		Config: auth.Config{Hostname: cfg.Hostname, Origin: cfg.Origin(), SecureCookies: cfg.SecureCookies()},
		Keys:   s.Keys, HTTP: opts.HTTPClient, Blobs: s.Blobs,
		// The bots service holds the limit on integration tokens.
		Integrations: s.Bots,
	}); err != nil {
		return fail(err)
	}
	if cfg.Dev && opts.RatePerSecond == 0 {
		// A local trial has one person at many browsers at once, and the
		// browser tests have five on one account; the default limit is
		// sized for a person's ordinary devices.
		opts.RatePerSecond, opts.RateBurst = 20, 1200
	}
	s.API = api.New(api.Deps{
		Auth: s.Auth, People: s.People, Notify: s.Notify, Chat: s.Chat, Search: s.Search, Live: s.Live, Keys: s.Keys, Store: s.Store,
		Hostname: cfg.Hostname, Origin: cfg.Origin(), Log: s.Log, TrustedProxies: cfg.TrustedProxies,
		RatePerSecond: opts.RatePerSecond, RateBurst: opts.RateBurst,
	})
	s.API.Forms, s.API.Sched, s.API.Commands = s.Forms, s.Sched, s.Commands
	s.API.Bots = s.Bots
	s.API.Calls, s.API.Overview.Calls = s.Calls, overviewCalls{s.Calls}
	s.API.Link, s.API.Overview.Links = s.Link, s.Link
	s.API.Mod, s.API.Janitor, s.API.Storage = s.Mod, s.Janitor, s
	// The pages sign-in draws itself: approving an app, the return from
	// company sign-in.
	s.API.Pages = web.NewPages()
	// The owner's export (ops.go).
	s.wireOps()
	// sealed chats, devices and phone wake-ups (sealed.go).
	s.wireSealed()
	// colour themes and theme cards (themes.go).
	s.wireThemes()
	if s.SetupLink, s.SetupLinkKept, err = s.Auth.EnsureSetupLink(ctx); err != nil {
		return fail(err)
	}
	return s, nil
}

// setupLinkLost says what to do when the setup link from an earlier start
// is lost.
const setupLinkLost = "It is not printed again. If it is lost, run churr setup-link with the flags churr serve is run with: it prints a new link and the old one stops working."

func newLogger(cfg config.Serve) *slog.Logger {
	o := &slog.HandlerOptions{Level: cfg.SlogLevel()}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, o))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, o))
}

// Handler is everything the server answers over HTTP: the API, and the
// embedded pages for every address that is not the API's (web.go).
func (s *Server) Handler() http.Handler { return s.handler() }

// Run listens and serves until ctx ends, then stops cleanly: it stops
// accepting requests, closes live connections with the "restarting" code so
// clients reconnect promptly, and closes the database.
func (s *Server) Run(ctx context.Context) error {
	// The listeners the configuration asks for: HTTPS with the server's
	// own certificate and the challenge port, or plain HTTP for a reverse
	// proxy or a local trial (listen.go).
	l, err := s.listen()
	if err != nil {
		s.Close()
		return err
	}
	s.emptyTmp()
	s.wireOverview(l)
	// The memory guard looks once a second from here on (memory.go).
	s.runMemory(ctx)
	// With CHURR_DEBUG_TIMING set, where the time of requests goes is
	// logged once a minute (the timing package).
	timing.Start(ctx, s.Log, func() []any {
		p, ok := s.Store.(interface {
			PoolNumbers() (max, inUse, idle int, waits int64, waited time.Duration)
		})
		if !ok {
			return nil
		}
		max, inUse, idle, waits, waited := p.PoolNumbers()
		return []any{"pool_max", max, "pool_in_use", inUse, "pool_idle", idle,
			"pool_waits_total", waits, "pool_wait_total_ms", waited.Milliseconds()}
	})
	s.announce(ctx, l)
	switch {
	case s.SetupLink != "":
		// Printed to standard output as well as logged: it is the one
		// way to become owner, and the operator must be able to find it.
		if s.opts.Listener == nil {
			fmt.Printf("Churr has no owner yet. Open this link to set it up:\n  %s\nThe link works once, and keeps working across restarts until it is used.\n", s.SetupLink)
		}
		s.Log.Info("setup link", "url", s.SetupLink)
	case s.SetupLinkKept:
		if s.opts.Listener == nil {
			fmt.Printf("Churr has no owner yet. The setup link printed when it first started still works.\n%s\n", setupLinkLost)
		}
		s.Log.Info("no owner yet: the setup link printed at an earlier start still works. " + setupLinkLost)
	}
	s.runPeople(ctx)
	s.runForms(ctx)
	s.runLink(ctx)
	// The janitor: retention and the rest of the clean-up, hourly.
	go s.Janitor.Run(ctx)
	// serve returns when ctx ends or a listener fails, after closing live
	// connections with the "restarting" code and letting requests in
	// progress finish (listen.go).
	failed := s.serve(ctx, l)
	if failed != nil {
		s.Log.Error("a listener failed", "err", failed)
	} else {
		s.Log.Info("shutting down")
	}
	if err := s.Close(); failed == nil {
		failed = err
	}
	return failed
}

// Close releases the database. Run calls it; a test that never calls Run
// calls it itself.
func (s *Server) Close() error {
	s.closeForms()
	s.closeOps()
	s.closeSealed()
	s.closeLink()
	// Live calls end before the connections they are signalled on close.
	if s.Calls != nil {
		s.Calls.Close()
	}
	s.Live.Shutdown()
	return s.Store.Close()
}
