// Package bots is how a program takes part in a server (docs/BOTS.md): bot
// accounts and their tokens, the commands bots offer, interactions (a
// command, a button press, an answer to a bot's form or dialog, and the
// bot's response), and webhooks from the request for one to the messages
// posted through its address, including the converters for Slack, Discord
// and GitHub payloads.
//
// A bot is an ordinary account as far as the rest of the server goes: it
// signs in with a token, and perm decides what it may do exactly as for
// anyone else. This package only adds what a person does not have.
package bots

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/people"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// The events of docs/EVENTS.md sections 9 and 10 that this package sends.
// None is stored: each goes straight to the connections of one account.
const (
	eventCommandInvoked       = "command.invoked"
	eventFormAnswered         = "form.answered"
	eventInteractionResponded = "interaction.responded"
	eventWebhookRequested     = "webhook.requested"
	eventWebhookDecided       = "webhook.decided"
	eventPreviewAdded         = "preview.added"
)

// Close codes of docs/EVENTS.md section 6 used here.
const (
	closeCredential = 4001
	closeRemoved    = 4003
)

// Hub is what bots needs of the live hub: sending an event to one account,
// knowing whether a bot is connected, and closing connections whose token
// or account has gone. live.Hub implements it.
type Hub interface {
	Send(account model.AccountID, v any)
	Presence(account model.AccountID) model.Presence
	CloseSession(account model.AccountID, session model.SessionID, code int, reason string)
	CloseAccount(account model.AccountID, code int, reason string)
}

// Dialogs is what bots needs of the forms package, which owns what a form
// may look like, what an answer to one may hold, and where a dialog's form
// is kept. forms.Service implements it. This package decides whether a bot
// may show someone a dialog now, and remembers where that person was; the
// answer comes back through FormAnswered. Without one, a bot cannot open a
// dialog.
type Dialogs interface {
	// OpenDialog checks and stores a form a bot shows one person.
	OpenDialog(ctx context.Context, bot model.Account, to model.AccountID, conv model.ConversationID, in model.FormInput) (model.Dialog, error)
	// SubmitDialog checks the person's answer, hands it to the bot
	// through FormAnswered and deletes the dialog. It returns the new
	// interaction's identifier.
	SubmitDialog(ctx context.Context, actor model.Account, id model.FormID, conv model.ConversationID, parent model.MessageID, button string, answers map[string]any) (string, error)
	// DismissDialog deletes a dialog the person closed.
	DismissDialog(ctx context.Context, actor model.Account, id model.FormID) error
}

// Images is the guarded way out to another site that link previews use
// (docs/BOTS.md section 11): public addresses only, ports 80 and 443, the
// address checked again after every redirect, and limits on time, size and
// redirects. The preview package implements it. Without one, or with link
// previews switched off, an image a webhook names by address is dropped.
type Images interface {
	// Enabled reports whether link previews are on.
	Enabled(ctx context.Context) bool
	// FetchImage fetches one image and returns it re-encoded as PNG,
	// JPEG, GIF or WebP.
	FetchImage(ctx context.Context, address string) ([]byte, error)
}

// Deps is what a Service is built from.
type Deps struct {
	Store  store.Store
	Perm   *perm.Checker
	People *people.Service
	Chat   *chat.Service
	Hub    Hub
	Blobs  blob.Blobs
	Keys   *secret.Keys
	// Origin is the server's own origin, which a webhook's address starts
	// with.
	Origin string
	// Dialogs and Images may be nil; see their types.
	Dialogs Dialogs
	Images  Images
	// Limits replaces the defaults of docs/BOTS.md section 8.
	Limits *Limits
	Log    *slog.Logger
}

// Service is the bots service.
type Service struct {
	store  store.Store
	perm   *perm.Checker
	people *people.Service
	chat   *chat.Service
	hub    Hub
	blobs  blob.Blobs
	keys   *secret.Keys
	origin string
	log    *slog.Logger
	now    func() model.Time
	limits *limiters

	// forms and images are set while the server is put together.
	depMu  sync.RWMutex
	forms  Dialogs
	images Images

	// Interactions and dialogs are held in memory (docs/BOTS.md section
	// 5): a restart ends them.
	mu           sync.Mutex
	interactions map[string]*interaction
	dialogs      map[model.FormID]*dialog
	swept        time.Time

	// fetched remembers images fetched for webhooks for a day, by the
	// hash of their address.
	fetchedMu sync.Mutex
	fetched   map[[32]byte]fetchedImage

	// background counts the image fetches still running, so a test or a
	// shutdown can wait for them.
	background sync.WaitGroup
}

// New makes the service.
func New(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	lim := DefaultLimits()
	if d.Limits != nil {
		lim = *d.Limits
	}
	return &Service{
		store: d.Store, perm: d.Perm, people: d.People, chat: d.Chat, hub: d.Hub, blobs: d.Blobs, keys: d.Keys,
		origin: strings.TrimRight(d.Origin, "/"), log: d.Log, now: model.Now, limits: newLimiters(lim),
		forms: d.Dialogs, images: d.Images,
		interactions: map[string]*interaction{}, dialogs: map[model.FormID]*dialog{},
		fetched: map[[32]byte]fetchedImage{},
	}
}

// SetDialogs hands the service the forms package. It is called while the
// server is being put together.
func (s *Service) SetDialogs(f Dialogs) {
	s.depMu.Lock()
	s.forms = f
	s.depMu.Unlock()
}

// SetImages hands the service the preview fetcher's guarded path.
func (s *Service) SetImages(i Images) {
	s.depMu.Lock()
	s.images = i
	s.depMu.Unlock()
}

func (s *Service) dialogStore() Dialogs {
	s.depMu.RLock()
	defer s.depMu.RUnlock()
	return s.forms
}

func (s *Service) imageFetcher() Images {
	s.depMu.RLock()
	defer s.depMu.RUnlock()
	return s.images
}

// Wait returns once every image fetch started so far has finished. Tests
// and an orderly shutdown call it.
func (s *Service) Wait() { s.background.Wait() }

// connected reports whether a bot has a live connection, which is the only
// way an interaction can reach it.
func (s *Service) connected(bot model.AccountID) bool {
	return s.hub.Presence(bot) != model.PresenceOffline
}

// errNotConnected is what the person is told when the bot they addressed
// has no live connection. Nothing is queued (docs/BOTS.md section 4).
var errNotConnected = model.ErrUnavailable.With("That bot is not connected right now.")

// audit writes one entry of the audit log for something an account did.
func audit(ctx context.Context, a store.Areas, actor model.Account, action, targetKind, targetID string, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	return a.Audit().Append(ctx, model.AuditEntry{ActorID: string(actor.ID), ActorKind: "account", Action: action,
		TargetKind: targetKind, TargetID: targetID, Detail: detail})
}

// notFound turns "no such row" into the one answer an outsider gets.
func notFound(err error) error {
	if model.CodeOf(err) == model.CodeNotFound {
		return model.ErrNotFound
	}
	return err
}
