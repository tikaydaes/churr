// Package push wakes phones (docs/PUSH.md). It keeps each device's push
// registration, decides through the notify package whether an event wakes
// an account's phones, makes the wake-up (an opaque event identifier, a
// collapse key, a priority and perhaps a badge number), and delivers it:
// to the relay for iPhones and Play Store Android, or straight to a
// UnifiedPush endpoint.
//
// A wake-up never contains text, a name or a conversation. It says "ask
// your server about this" and nothing else; the phone then fetches what to
// show from its own server with the event identifier (Resolve).
package push

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/notify"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/preview"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// Settings of docs/PUSH.md section 14 that have no constant in store yet.
const (
	SettingRelayURL  = "push.relay_url"
	SettingSendBadge = "push.send_badge"
)

// Hub is what push needs of the live hub: whether a client of the account
// is connected and active, in which case that client shows the
// notification itself and no phone is woken (ruling R6).
type Hub interface {
	Active(account model.AccountID) bool
}

// judge is a Decider that can answer Wake for a message and everyone it
// might concern at once. notify.Service is one.
type judge interface {
	Judge(ctx context.Context, m model.Message, also []model.AccountID) (*notify.Fanout, error)
}

// Decider is what push needs of the notify package: the one rule for what
// a message means to a person, and the badge total.
type Decider interface {
	// Wake says whether a new message notifies an account (steps 1 to 4
	// of docs/PUSH.md section 2: who the person is, blocks, the mode, do
	// not disturb and notify anyway), and whether the wake-up is of high
	// priority.
	Wake(ctx context.Context, m model.Message, to model.AccountID) (wake, high bool)
	// Totals returns the account's totals, for the badge.
	Totals(ctx context.Context, account model.AccountID) (model.Totals, error)
}

// Deps is what a Service is built from.
type Deps struct {
	Store  store.Store
	Perm   *perm.Checker
	Hub    Hub
	Notify Decider
	Keys   *secret.Keys
	Log    *slog.Logger
	// AllowPrivate lets UnifiedPush endpoints sit on private addresses,
	// for an operator whose distributor is on their own network
	// (-unifiedpush-allow-private).
	AllowPrivate bool
	// RelayHTTP is the client used to reach the relay, and EndpointHTTP
	// the one for UnifiedPush endpoints. Nil gives the real ones; tests
	// replace them.
	RelayHTTP    *http.Client
	EndpointHTTP *http.Client
	// Retry is the wait before each further attempt of a wake-up that
	// failed. Nil gives 5 seconds, 30 seconds and 2 minutes.
	Retry []time.Duration
}

// Service is the push service.
type Service struct {
	store  store.Store
	perm   *perm.Checker
	hub    Hub
	notify Decider
	keys   *secret.Keys
	log    *slog.Logger
	now    func() model.Time

	relayHTTP    *http.Client
	endpointHTTP *http.Client
	retry        []time.Duration

	// challenges holds the reattach challenges handed out and not yet
	// answered, by session (devices.go).
	mu         sync.Mutex
	challenges map[model.SessionID]challenge

	// rung remembers which devices were woken for a call that is
	// ringing, so they can be told when the ring stops.
	rung map[model.CallID][]rungDevice

	// The wake-ups waiting to be tried again (deliver.go).
	waitMu  sync.Mutex
	waiting []*waitingWake
	// loggedBug keeps "the relay refused a request as malformed" to one
	// log line.
	loggedBug sync.Once
	// sem bounds how many deliveries run at once.
	sem  chan struct{}
	stop chan struct{}
	wg   sync.WaitGroup
}

// New makes the service.
func New(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	s := &Service{store: d.Store, perm: d.Perm, hub: d.Hub, notify: d.Notify, keys: d.Keys, log: d.Log, now: model.Now,
		relayHTTP: d.RelayHTTP, endpointHTTP: d.EndpointHTTP, retry: d.Retry,
		challenges: map[model.SessionID]challenge{}, rung: map[model.CallID][]rungDevice{},
		sem: make(chan struct{}, 16), stop: make(chan struct{})}
	if s.retry == nil {
		s.retry = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute}
	}
	if s.relayHTTP == nil {
		// A small pool of connections to the relay, kept open. Wake-ups
		// go one request each and are never batched: a batch would tell
		// the relay which devices were woken together.
		s.relayHTTP = &http.Client{Timeout: attemptTimeout, Transport: &http.Transport{
			MaxIdleConns: 8, MaxIdleConnsPerHost: 8, IdleConnTimeout: 5 * time.Minute, ForceAttemptHTTP2: true}}
	}
	if s.endpointHTTP == nil {
		tr := &http.Transport{MaxIdleConns: 16, IdleConnTimeout: time.Minute, ForceAttemptHTTP2: true}
		if !d.AllowPrivate {
			// The endpoint is an address a member supplied, so it is
			// treated as a link to preview is: public addresses only,
			// checked again at connection time, port 443 only.
			dialer := preview.SafeDialer()
			dialer.Ports = []string{"443"}
			tr.DialContext = dialer.DialContext
		}
		s.endpointHTTP = &http.Client{Timeout: attemptTimeout, Transport: tr,
			// No redirects are followed.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return s
}

// Close stops retries that are waiting and waits for deliveries in
// progress. Wake-ups waiting to be retried are lost, as a restart loses
// them: the message is on the server and the app catches up when opened.
func (s *Service) Close() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	s.wg.Wait()
}

// relayURL returns the relay to use, or "" when wake-ups through the relay
// are off or no relay is set.
func (s *Service) relayURL(ctx context.Context) string {
	on, err := store.SettingBool(ctx, s.store, store.SettingPushRelayEnabled, true)
	if err != nil || !on {
		return ""
	}
	url, _ := store.SettingString(ctx, s.store, SettingRelayURL, "")
	return url
}

func (s *Service) unifiedPushOn(ctx context.Context) bool {
	on, err := store.SettingBool(ctx, s.store, store.SettingUnifiedPushEnabled, false)
	return err == nil && on
}

// Kinds lists the push kinds this server accepts right now, for its public
// description: `apns` and `fcm` while the relay is on and set, and
// `unifiedpush` while that is switched on. `none` is always accepted.
func (s *Service) Kinds(ctx context.Context) []string {
	out := []string{}
	if s.relayURL(ctx) != "" {
		out = append(out, model.PushAPNs, model.PushFCM)
	}
	if s.unifiedPushOn(ctx) {
		out = append(out, model.PushUnifiedPush)
	}
	return append(out, model.PushNone)
}

// MessageCreated is told of every new message once it has been published.
// It works out, for each local person who can read the conversation,
// whether their phones are woken (docs/PUSH.md section 2) and sends the
// wake-ups. It returns at once; the work happens in the background.
func (s *Service) MessageCreated(m model.Message) {
	if m.ID == "" || m.IsSystem() || m.Quiet {
		// System notices never wake a phone, and neither does a sealed
		// message its sender marked quiet.
		return
	}
	s.background(func(ctx context.Context) {
		// The rule is worked out for everyone at once where it can be:
		// asked one account at a time it reads a dozen rows for each,
		// which for a large channel is tens of thousands of queries a
		// message.
		if all, ok := s.notify.(judge); ok {
			f, err := all.Judge(ctx, m, nil)
			if err == nil {
				err = f.Notified(func(to model.AccountID, high bool) { s.wakeJudged(ctx, m, to, high) })
			}
			if err == nil {
				return
			}
			s.log.Warn("who to wake could not be worked out for everyone at once", "err", err)
		}
		accounts, err := s.perm.Recipients(ctx, m.ConversationID)
		if err != nil {
			s.log.Warn("who to wake could not be read", "err", err)
			return
		}
		// Someone mentioned in a public channel they have not joined is
		// notified too, once.
		if mentions, err := s.store.Messages().Mentions(ctx, m.ID); err == nil {
			for _, mn := range mentions {
				accounts = append(accounts, mn.AccountID)
			}
		}
		seen := map[model.AccountID]bool{}
		for _, to := range accounts {
			if seen[to] {
				continue
			}
			seen[to] = true
			s.wakeForMessage(ctx, m, to)
		}
	})
}

// MessageJudged is MessageCreated for a message whose rule has been worked
// out already, by the chat service for the live delivery: the same answers
// decide the wake-ups, so nothing is read or judged a second time. Only the
// accounts the message can notify are looked at, however large the
// conversation is.
func (s *Service) MessageJudged(m model.Message, f *notify.Fanout) {
	if f == nil {
		s.MessageCreated(m)
		return
	}
	if m.ID == "" || m.IsSystem() || m.Quiet {
		return
	}
	s.background(func(ctx context.Context) {
		err := f.Notified(func(to model.AccountID, high bool) { s.wakeJudged(ctx, m, to, high) })
		if err != nil {
			s.log.Warn("who to wake could not be worked out", "err", err)
		}
	})
}

// NotifiedAnyway is told when a notify-anyway request reached its target:
// the wake-up it causes is sent then, and looks like any other (ruling R4).
func (s *Service) NotifiedAnyway(m model.Message, to model.AccountID) {
	s.background(func(ctx context.Context) { s.wakeForMessage(ctx, m, to) })
}

// wakeForMessage is steps 1 to 6 for one message and one account.
func (s *Service) wakeForMessage(ctx context.Context, m model.Message, to model.AccountID) {
	// Steps 1 to 4, by the one rule.
	wake, high := s.notify.Wake(ctx, m, to)
	if !wake {
		return
	}
	s.wakeJudged(ctx, m, to, high)
}

// wakeJudged is steps 5 and 6, for an account the rule said to notify.
func (s *Service) wakeJudged(ctx context.Context, m model.Message, to model.AccountID, high bool) {
	// Step 5: a client that is connected and active shows it itself.
	if s.hub.Active(to) {
		return
	}
	priority := PriorityNormal
	if high {
		priority = PriorityHigh
	}
	s.wakeAccount(ctx, to, Ref{Kind: KindMessage, ConversationID: m.ConversationID, Seq: m.Seq}, priority, true)
}

// Ring is one account being rung for one call. The decision to ring (the
// mode, blocks, do not disturb) has been made already, by the one rule.
type Ring struct {
	CallID         model.CallID
	ConversationID model.ConversationID
	AccountID      model.AccountID
}

// RingStarted wakes the phones of an account a call rings, unless one of
// its clients is connected and active. To the relay a ring looks the same
// as any other wake-up.
func (s *Service) RingStarted(r Ring) {
	s.background(func(ctx context.Context) {
		if s.hub.Active(r.AccountID) {
			return
		}
		woken := s.wakeAccount(ctx, r.AccountID, Ref{Kind: KindRing, ConversationID: r.ConversationID, CallID: r.CallID}, PriorityHigh, true)
		if len(woken) == 0 {
			return
		}
		s.mu.Lock()
		for _, d := range woken {
			s.rung[r.CallID] = append(s.rung[r.CallID], rungDevice{account: r.AccountID, device: d})
		}
		s.mu.Unlock()
	})
}

// RingStopped tells the phones that were woken for a ring that it is over:
// one more wake-up with the conversation's collapse key and no badge. When
// the phone fetches it, the answer says the call no longer rings.
func (s *Service) RingStopped(r Ring) {
	s.mu.Lock()
	var mine, rest []rungDevice
	for _, d := range s.rung[r.CallID] {
		if d.account == r.AccountID {
			mine = append(mine, d)
		} else {
			rest = append(rest, d)
		}
	}
	if len(rest) == 0 {
		delete(s.rung, r.CallID)
	} else {
		s.rung[r.CallID] = rest
	}
	s.mu.Unlock()
	if len(mine) == 0 {
		return
	}
	s.background(func(ctx context.Context) {
		ref := Ref{Kind: KindRing, ConversationID: r.ConversationID, CallID: r.CallID}
		for _, d := range mine {
			if dev, err := s.store.Devices().Get(ctx, d.device.ID); err == nil {
				s.wakeDevice(ctx, dev, ref, PriorityHigh, nil)
			}
		}
	})
}

type rungDevice struct {
	account model.AccountID
	device  model.Device
}

// wakeAccount is step 6: one wake-up to each device of the account that
// can be woken. It returns the devices it sent to.
func (s *Service) wakeAccount(ctx context.Context, account model.AccountID, ref Ref, priority string, withBadge bool) []model.Device {
	devices, err := s.store.Devices().Wakeable(ctx, account)
	if err != nil || len(devices) == 0 {
		return nil
	}
	var badge *int64
	if withBadge {
		if on, err := store.SettingBool(ctx, s.store, SettingSendBadge, true); err == nil && on {
			if t, err := s.notify.Totals(ctx, account); err == nil {
				b := min(max(t.Badge, 0), 99999)
				badge = &b
			}
		}
	}
	var sent []model.Device
	for _, d := range devices {
		if s.wakeDevice(ctx, d, ref, priority, badge) {
			sent = append(sent, d)
		}
	}
	return sent
}

// background runs fn off the caller's path, unless the service is closing.
func (s *Service) background(fn func(ctx context.Context)) {
	select {
	case <-s.stop:
		return
	default:
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		fn(ctx)
	}()
}
