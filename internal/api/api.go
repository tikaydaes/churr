// Package api is the HTTP surface. Handlers decode a request, call a
// service and encode the answer; they hold no rules of their own.
//
// Every handler is registered by the operationId of docs/openapi.yaml
// against the generated table in operations_gen.go, so a path or method
// cannot drift from the contract. README.md explains how to add one.
package api

import (
	"errors"
	"fmt"
	"github.com/tikaydaes/churr/internal/approvals"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/auth"
	"github.com/tikaydaes/churr/internal/bots"
	"github.com/tikaydaes/churr/internal/broadcast"
	"github.com/tikaydaes/churr/internal/calls"
	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/export"
	"github.com/tikaydaes/churr/internal/forms"
	"github.com/tikaydaes/churr/internal/janitor"
	"github.com/tikaydaes/churr/internal/link"
	"github.com/tikaydaes/churr/internal/live"
	"github.com/tikaydaes/churr/internal/mod"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/notify"
	"github.com/tikaydaes/churr/internal/people"
	"github.com/tikaydaes/churr/internal/push"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/sched"
	"github.com/tikaydaes/churr/internal/sealed"
	"github.com/tikaydaes/churr/internal/search"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/themes"
	"github.com/tikaydaes/churr/internal/timing"
)

// Auth says how an operation authenticates, as the contract states it.
type Auth int

// The kinds of authentication in the contract.
const (
	// AuthNone: no credential is looked at.
	AuthNone Auth = iota
	// AuthOptional: the cookie or a bearer token if there is one.
	AuthOptional
	// AuthUser: the cookie, or an app, personal or bot token.
	AuthUser
	// AuthPending: as AuthUser, and also a session that still owes a
	// second factor.
	AuthPending
	// AuthWeb: the cookie only.
	AuthWeb
	// AuthIntegration: a server integration token.
	AuthIntegration
	// AuthHook: the webhook secret in the path.
	AuthHook
)

// Operation is one operation of the contract.
type Operation struct {
	// ID is the operationId.
	ID     string
	Method string
	// Path is the path as the contract writes it, which is also how
	// net/http patterns write it.
	Path string
	Auth Auth
	// Area is the contract's tag.
	Area string
	// Success is the status a successful answer carries. A few operations
	// answer with a second one as well (postMessage answers 200 for a
	// message that already existed); their handlers say so themselves.
	Success int
	// Paging is "cursor", "seq" or "".
	Paging string
}

// Deps is what the API is built from. Every service is reached through this
// struct, so a handler can only do what a service offers.
type Deps struct {
	Auth   *auth.Service
	People *people.Service
	// Notify is read for the status endpoint's totals.
	Notify *notify.Service
	Chat   *chat.Service
	Search *search.Service
	Live   *live.Hub
	Keys   *secret.Keys
	// Forms, Sched and Commands are forms and polls, scheduled items, and
	// saved replies with the command menu. server sets them once they
	// are built.
	Forms    *forms.Service
	Sched    *sched.Service
	Commands *sched.Commands
	// Bots is bots, their commands and interactions, and webhooks. server
	// sets it once it is built.
	Bots *bots.Service
	// Calls is the calls service and its media server. server sets it
	// once it is built (calls.go).
	Calls *calls.Server
	// Link is the link to other servers: the owner's admin operations, and
	// the two paths the link protocol fixes (link.go).
	Link *link.Service
	// Mod is moderation: time-outs, reports and the word filter. Janitor
	// is read for when it last ran, and Storage tests object storage
	// settings and says where files go. server sets them (chat_*.go).
	Mod *mod.Service
	// Approvals is the instance's approvals queue (approvals.go).
	Approvals *approvals.Service
	Janitor   *janitor.Janitor
	Storage   Storage
	// Export runs the owner's export (ops_export.go). Unset, the three
	// export operations answer `unavailable`.
	Export *export.Runner
	// Overview is what the admin Overview shows that comes from how the
	// server was started and from the calls and link packages
	// (ops_overview.go). Left unset, those parts read as none.
	Overview Overview
	// Store is read for the server's own description and for health; the
	// handlers of conversations and messages never touch it.
	Store    store.Store
	Hostname string
	Origin   string
	Log      *slog.Logger
	// TrustedProxies are the addresses whose X-Forwarded-For is believed.
	// Empty unless the server runs behind a proxy.
	TrustedProxies []netip.Prefix
	// RatePerSecond and RateBurst bound each account's requests. Zero
	// gives the defaults.
	RatePerSecond float64
	RateBurst     int
	// Pages draws the few pages sign-in needs: approving an app and the
	// return from company sign-in. server hands it the web package's.
	Pages Pages
	// Sealed is what the server keeps for sealed chats, and Push devices
	// and phone wake-ups. UnlockPage draws the page that unlocks sealed
	// chats for an app with a passkey. server sets all three
	// (sealed.go, devices.go).
	Sealed     *sealed.Service
	Push       *push.Service
	UnlockPage func(w http.ResponseWriter, status int, data map[string]any)
	// Themes is colour themes (themes.go). server sets it.
	Themes *themes.Service
	// Broadcast is the server broadcast (broadcast.go). server sets it.
	Broadcast *broadcast.Service
}

// API is the HTTP handler.
type API struct {
	Deps
	handlers map[string]handler
	mux      *http.ServeMux
	limiter  *ratelimit.Limiter
	// refused writes requests refused over the rate limit to the log,
	// counted and not one line each (ratelimit.Refusals).
	refusedOnce sync.Once
	refused     *ratelimit.Refusals
	// untrusted are the peers whose forwarding header was ignored
	// (proxies.go).
	untrusted untrustedProxies
}

// handler answers one operation. It returns the error to send, or nil after
// writing the answer itself.
type handler func(r *Request) error

// registrations collects each area file's handlers. An area file adds to it
// from init, so adding an area never means editing this file.
var registrations []func(a *API)

// register is called from the init of each area file.
func register(fn func(a *API)) { registrations = append(registrations, fn) }

// handle registers the handler of one operation by its operationId. It
// panics for an identifier the contract does not have, or one registered
// twice: both are mistakes to find at start, not in production.
func (a *API) handle(id string, h handler) {
	if _, known := operationByID[id]; !known {
		panic("api: no operation in the contract is called " + id)
	}
	if _, dup := a.handlers[id]; dup {
		panic("api: operation registered twice: " + id)
	}
	a.handlers[id] = h
}

var operationByID = func() map[string]Operation {
	m := make(map[string]Operation, len(Operations))
	for _, op := range Operations {
		m[op.ID] = op
	}
	return m
}()

// New builds the handler: every operation of the contract on its method and
// path, and /healthz.
func New(d Deps) *API {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.RatePerSecond == 0 {
		d.RatePerSecond, d.RateBurst = 2, 120
	}
	a := &API{Deps: d, handlers: map[string]handler{}, mux: http.NewServeMux(), limiter: ratelimit.New(d.RatePerSecond, d.RateBurst)}
	for _, fn := range registrations {
		fn(a)
	}
	for id := range valuable {
		if _, known := operationByID[id]; !known {
			panic("api: no operation in the contract is called " + id)
		}
	}
	for _, op := range Operations {
		// Operations nobody has built yet are registered too: the path
		// is then known to be free of conflicts, and a client gets a
		// clear answer.
		a.mux.Handle(op.Method+" "+op.Path, a.wrap(op, a.handlers[op.ID]))
	}
	a.mux.HandleFunc("GET /healthz", a.healthz)
	return a
}

// Unimplemented lists the operations of the contract that have no handler.
func (a *API) Unimplemented() []Operation {
	var out []Operation
	for _, op := range Operations {
		if a.handlers[op.ID] == nil {
			out = append(out, op)
		}
	}
	return out
}

// ServeHTTP answers a request. A path the contract does not have gets the
// usual error shape, not the standard library's plain text.
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, pattern := a.mux.Handler(r); pattern == "" {
		writeError(w, a.Log, model.ErrNotFound)
		return
	}
	a.mux.ServeHTTP(w, r)
}

func (a *API) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 2*time.Second)
	defer cancel()
	if err := a.Store.Ping(ctx); err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

// maxBody is the largest JSON body accepted.
const maxBody = 1 << 20

// openBeforeSetup are the operations that work before the owner exists.
var openBeforeSetup = map[string]bool{"getServer": true, "getWellKnown": true, "beginSetup": true, "finishSetup": true, "setupWithAccessKey": true}

// recoveryAllowed is what a session made by a recovery code or link may do
// (docs/AUTH.md section 6.1).
var recoveryAllowed = map[string]bool{
	"getMe": true, "listPasskeys": true, "deletePasskey": true, "listSessions": true, "endSession": true,
	"beginAddPasskey": true, "finishAddPasskey": true, "logout": true, "getServer": true, "startSso": true,
	"getAccessKey": true, "setAccessKey": true,
}

// personalDenied is what a personal token may not do: anything that manages
// sign-in, and sealed chats (docs/PERMISSIONS.md decision P1, docs/AUTH.md
// section 12.1). Listed by area so new operations there are covered.
var personalDeniedAreas = map[string]bool{"Sealed chats": true, "OAuth": true, "Sign-in": true}
var personalDenied = map[string]bool{
	"listPasskeys": true, "beginAddPasskey": true, "finishAddPasskey": true, "renamePasskey": true, "deletePasskey": true,
	"setPasskeyUnlockKey": true, "getRecoveryCodes": true, "regenerateRecoveryCodes": true, "getTotp": true, "beginTotp": true,
	"confirmTotp": true, "removeTotp": true, "listIdentities": true, "listSessions": true, "endSession": true,
	"endOtherSessions": true, "listTokens": true, "createToken": true, "revokePersonalToken": true,
	"getAccessKey": true, "setAccessKey": true, "removeAccessKey": true,
}

// valuable are the acts worth more to a thief than a session (docs/AUTH.md
// section 10.4, docs/PERMISSIONS.md section 9.1): who holds a role, the
// full export, taking an account away or into one's hands, credentials for
// bots and integrations, sending channels to other instances, and putting
// people into channels from the console. Done by an Owner or an Admin, each
// needs a browser or app session checked in the last ten minutes, so a
// personal token never may. For anyone else the operation refuses on its
// own terms. Sign-in settings that weaken sign-in are checked the same way
// by the settings themselves (auth/settings.go).
var valuable = map[string]bool{
	"setAccountRole": true, "removeAccount": true, "createBan": true, "createRecoveryLink": true, "adminRemoveTotp": true,
	"createClaimLink": true, "startExport": true, "createExportDownload": true,
	"createBot": true, "createBotToken": true, "createIntegration": true,
	"createPeerInvitation": true, "acceptPeerInvitation": true, "shareChannel": true, "addStreamServer": true,
	"transferStream": true, "proposeStreamMove": true, "acceptStreamMove": true, "joinHubChannel": true,
	"appointManager": true, "ownerOverrideAdd": true,
}

// Valuable says whether an operation is one of the valuable acts.
func Valuable(id string) bool { return valuable[id] }

// wrap puts the shared machinery around one operation's handler: panic
// recovery, the setup gate, authentication for the kind the contract names,
// the origin check for cookie requests, what recovery sessions and personal
// tokens may do, rate limits with their headers, and the error shape.
func (a *API) wrap(op Operation, h handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, hr *http.Request) {
		// Protocol Buffers for a client that asks (protobuf.go): the
		// handler below reads and writes JSON either way.
		w, sent, ok := a.negotiate(op, w, hr)
		if !ok {
			notAcceptable(w)
			return
		}
		defer sent()
		r := &Request{W: w, R: hr, Op: op, api: a}
		defer func() {
			if p := recover(); p != nil {
				a.Log.Error("a handler panicked", "operation", op.ID, "panic", fmt.Sprint(p))
				writeError(w, a.Log, model.ErrInternal)
			}
		}()
		if err := a.serve(r, h); err != nil {
			writeError(w, a.Log, err)
		}
	})
}

// timedOps are the operations whose whole time is in the timing summary.
var timedOps = map[string]bool{"postMessage": true, "setReadPosition": true, "addReaction": true, "editMessage": true, "listMessages": true}

func (a *API) serve(r *Request, h handler) error {
	op := r.Op
	// With CHURR_DEBUG_TIMING: http.auth is everything before the
	// handler, and http.<operation> the whole request as the server sees
	// it, for the operations a busy server spends its time on.
	began := timing.Now()
	if !began.IsZero() && timedOps[op.ID] {
		defer func() { timing.Since("http."+op.ID, began) }()
	}
	ctx := r.R.Context()
	r.Addr = a.clientAddr(r.R)
	if !largeBody[op.ID] {
		// Uploads carry a file; their handlers apply the file size limit.
		r.R.Body = http.MaxBytesReader(r.W, r.R.Body, maxBody)
	}

	if !openBeforeSetup[op.ID] {
		need, err := a.Auth.SetupNeeded(ctx)
		if err != nil {
			return err
		}
		if need {
			return model.ErrUnavailable.With("This server has not been set up yet. Open the setup link from its log.")
		}
	}

	switch op.Auth {
	case AuthUser, AuthPending, AuthWeb, AuthOptional:
		who, err := a.Auth.Authenticate(r.R, r.Addr)
		switch {
		case errors.Is(err, auth.ErrNoCredential):
			if op.Auth != AuthOptional {
				return model.ErrUnauthenticated
			}
		case err != nil:
			if op.Auth != AuthOptional {
				return err
			}
		default:
			r.Who, r.SignedIn = who, true
			// To an operation that also works signed out, a session
			// that still owes its second step, or a recovery session
			// with no business there, is nobody: a person whose
			// sign-in stalled must be able to start again.
			// A locked session is nobody there too (the lock, lock.go).
			if op.Auth == AuthOptional && (who.Session.PendingSecondFactor || !who.Session.LockedAt.IsZero() || (who.Session.Recovery && !recoveryAllowed[op.ID])) {
				r.Who, r.SignedIn = auth.Who{}, false
			}
		}
		if r.SignedIn {
			if op.Auth == AuthWeb && !who.Cookie {
				return model.ErrUnauthenticated.With("This needs a browser session.")
			}
			// A browser attaches the cookie by itself, so a request
			// that changes something must prove where it came from.
			if who.Cookie && r.R.Method != http.MethodGet && r.R.Method != http.MethodHead && !a.Auth.OriginOK(r.R) {
				return model.ErrForbidden.With("This request did not come from this server's own pages.")
			}
			if who.Session.PendingSecondFactor && op.Auth != AuthPending {
				return model.ErrSecondFactorRequired
			}
			if !who.Session.LockedAt.IsZero() && !lockedAllowed[op.ID] {
				return model.ErrSessionLocked
			}
			if who.Session.Recovery && !recoveryAllowed[op.ID] {
				return model.ErrForbidden.With("A recovery session can only set up a new passkey or access key.")
			}
			if who.Session.Kind == model.SessionPersonal && (personalDenied[op.ID] || personalDeniedAreas[op.Area]) {
				return model.ErrForbidden.With("A personal token cannot manage sign-in.")
			}
			if valuable[op.ID] && who.Account.Role.IsAdmin() {
				if who.Session.Kind == model.SessionPersonal {
					return model.ErrForbidden.With("A personal token cannot do this. Use a browser or the app.")
				}
				if err := a.Auth.Fresh(who); err != nil {
					return err
				}
			}
			// Limits are per account for people and bots.
			res := a.takeRequest(who.Account)
			hd := r.W.Header()
			hd.Set("RateLimit-Limit", strconv.Itoa(res.Limit))
			hd.Set("RateLimit-Remaining", strconv.Itoa(res.Remaining))
			hd.Set("RateLimit-Reset", strconv.Itoa(res.ResetSeconds()))
			if !res.OK {
				// One line for all accounts together: a crowd over the
				// limit must not be a line each.
				a.refusedOnce.Do(func() {
					a.refused = ratelimit.NewRefusals(a.Log, "API requests were refused: accounts are over the request rate limit", 0)
				})
				a.refused.Note("requests of one account", "", int(res.RetryAfter/time.Second)+1)
				return model.RateLimited(int(res.RetryAfter/time.Second) + 1)
			}
			// A bot has limits of its own on what it posts, uploads,
			// reacts to and holds open (bots.go).
			if err := a.botLimits(r); err != nil {
				return err
			}
		}
	case AuthIntegration, AuthHook:
		// TODO(bots): integration tokens and webhook secrets are checked
		// by their own handlers when those areas are built.
	}

	if h == nil {
		return model.ErrUnavailable.With("This operation is not built yet.")
	}
	timing.Since("http.auth", began)
	return h(r)
}
