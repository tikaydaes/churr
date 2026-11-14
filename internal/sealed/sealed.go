// Package sealed is what the server keeps and enforces for sealed chats
// (docs/SEALED.md section 16): device lists, epoch records, sealed
// messages, the key backup and unlock requests. It stores and serves all of
// them and can open none.
//
// The few rules here are the ones the document gives the server: a device
// list goes up one version at a time and names live devices; a chat has
// one epoch record per key epoch and only the next number is accepted; a
// message must be in the chat's current key epoch, and that epoch must name
// the newest device lists; a backup is swapped as a whole. They keep
// honest clients from tripping each other up. Security does not rest on
// them, since devices check everything themselves.
//
// wire.go is the reader for the parts of records the server may see. The
// client side of the scheme is in the ref package, which only tests link.
package sealed

import (
	"context"
	"log/slog"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Hub is what this package needs of the live hub: events that belong to no
// conversation, to an account's connections or to some of them.
type Hub interface {
	Send(account model.AccountID, v any)
	SendToSession(account model.AccountID, session model.SessionID, v any)
	SendExceptSession(account model.AccountID, session model.SessionID, v any)
}

// Outbound is what carries sealed-chat things to linked servers. The link
// package implements it; nil means this server has no links.
type Outbound interface {
	// DeviceListPublished is called after a local account published a
	// new version of its device list, so the version can be sent to the
	// home server of each remote account it has a sealed chat with.
	DeviceListPublished(ctx context.Context, account model.AccountID, version int64, signedList []byte)
	// RequestDeviceLists asks a remote account's home server for the
	// versions of its device list above `after`, on behalf of a local
	// member, and stores what comes back through StoreRemoteList.
	RequestDeviceLists(ctx context.Context, remote, by model.AccountID, after int64) error
	// StartSealed puts a stream behind a sealed chat that was just made
	// with an account of another server, and offers it to that server
	// with epoch record 1 (docs/LINK-PROTOCOL.md section 16).
	StartSealed(ctx context.Context, by model.Account, conv model.Conversation, other model.Account, record []byte) error
}

// Deps is what a Service is built from.
type Deps struct {
	Store store.Store
	Perm  *perm.Checker
	Chat  *chat.Service
	Hub   Hub
	// Hostname is this server's name, which a local account's device
	// list must carry.
	Hostname string
	Log      *slog.Logger
}

// Service is the sealed-chat service.
type Service struct {
	store    store.Store
	perm     *perm.Checker
	chat     *chat.Service
	hub      Hub
	hostname string
	log      *slog.Logger
	now      func() model.Time
	outbound Outbound
}

// New makes the service.
func New(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Service{store: d.Store, perm: d.Perm, chat: d.Chat, hub: d.Hub, hostname: d.Hostname, log: d.Log, now: model.Now}
}

// SetOutbound connects the link package. It is called once while the
// server is being put together.
func (s *Service) SetOutbound(o Outbound) { s.outbound = o }

// Caller is who is asking: the account and the session it came in on. Most
// of what a device does for sealed chats must come from the session its
// device row is attached to.
type Caller struct {
	Account model.Account
	Session model.Session
}

// event builds an event that belongs to no conversation.
func (s *Service) event(eventType string, fields map[string]any) model.Event {
	return model.Event{Type: eventType, At: s.now(), Payload: fields}
}

// person refuses callers that hold no device keys: bots, and tokens made
// for scripts (docs/AUTH.md section 12.1).
func person(c Caller) error {
	if !c.Account.Active() {
		return model.ErrUnauthenticated
	}
	if c.Account.Role == model.RoleBot || c.Account.Role == model.RoleRemote ||
		(c.Session.Kind != model.SessionWeb && c.Session.Kind != model.SessionApp) {
		return model.ErrForbidden.With("Sealed chats need a person signed in on a device.")
	}
	return nil
}

// device returns the device attached to the caller's session. Operations
// that say "from the session a device is attached to" start here.
func (s *Service) device(ctx context.Context, a store.Areas, c Caller) (model.Device, error) {
	if err := person(c); err != nil {
		return model.Device{}, err
	}
	d, found, err := a.Devices().BySession(ctx, c.Session.ID)
	if err != nil {
		return d, err
	}
	if !found || !d.RetiredAt.IsZero() {
		return d, model.ErrForbidden.With("Register this device first.")
	}
	return d, nil
}

// keyed is device for operations that need the device's sealed-chat keys.
func (s *Service) keyed(ctx context.Context, a store.Areas, c Caller) (model.Device, error) {
	d, err := s.device(ctx, a, c)
	if err != nil {
		return d, err
	}
	if len(d.SignKey) != 32 || len(d.BoxKey) != 32 {
		return d, model.ErrForbidden.With("Register this device's keys first.")
	}
	return d, nil
}

// enabled refuses with feature_off while sealed chats are switched off.
func (s *Service) enabled(ctx context.Context) error {
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return err
	}
	if !set.SealedEnabled {
		return model.ErrFeatureOff.With("Sealed chats are switched off on this server.")
	}
	return nil
}

// otherMember returns the other account of a sealed chat.
func (s *Service) otherMember(ctx context.Context, a store.Areas, conv model.ConversationID, me model.AccountID) (model.Account, error) {
	ids, err := a.Members().IDs(ctx, conv)
	if err != nil {
		return model.Account{}, err
	}
	for _, id := range ids {
		if id != me {
			return a.Accounts().Get(ctx, id)
		}
	}
	// A sealed chat always has two members; one whose other member's row
	// is gone accepts nothing new.
	return model.Account{}, nil
}

// Janitor is the hourly clean-up of docs/SEALED.md section 13.2: epoch
// records and backup records of the oldest key epochs that retention has
// emptied. It returns how many of each it deleted.
func (s *Service) Janitor(ctx context.Context) (epochs, records int64, err error) {
	return s.store.Sealed().DeleteEmptyEpochs(ctx)
}
