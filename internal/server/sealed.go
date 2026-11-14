package server

import (
	"context"

	"github.com/tikaydaes/churr/internal/calls"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/push"
	"github.com/tikaydaes/churr/internal/sealed"
	"github.com/tikaydaes/churr/internal/web"
)

// wireSealed builds the sealed-chat service and the push service, and
// connects them:
//
//   - chat asks the sealed service whether a sealed chat is read-only, and
//     tells push of every new message, which decides through notify who is
//     woken;
//   - the calls service tells push when a ring starts and stops;
//   - the janitor deletes the records of emptied key epochs through the
//     sealed service, and announces the devices it retires;
//   - the API gets both, and the unlock page from the web package.
//
// It runs after live, notify, chat, calls, the janitor and the API exist.
func (s *Server) wireSealed() {
	s.Sealed = sealed.New(sealed.Deps{Store: s.Store, Perm: s.Perm, Chat: s.Chat, Hub: s.Live, Hostname: s.Config.Hostname, Log: s.Log})
	s.Push = push.New(push.Deps{Store: s.Store, Perm: s.Perm, Hub: s.Live, Notify: s.Notify, Keys: s.Keys, Log: s.Log,
		AllowPrivate: s.Config.UnifiedPushAllowPrivate,
		RelayHTTP:    s.opts.PushRelayHTTP, EndpointHTTP: s.opts.PushEndpointHTTP, Retry: s.opts.PushRetry})
	s.Chat.SetSealed(s.Sealed)
	s.Chat.SetWaker(s.Push)
	if s.Calls != nil {
		// A ring reaches a phone as an ordinary wake-up.
		s.Calls.OnRing(func(r calls.Ring) {
			s.Push.RingStarted(push.Ring{CallID: r.CallID, ConversationID: r.ConversationID, AccountID: r.AccountID})
		})
		s.Calls.OnRingStopped(func(r calls.RingStop) {
			s.Push.RingStopped(push.Ring{CallID: r.CallID, ConversationID: r.ConversationID, AccountID: r.AccountID})
		})
	}
	if s.Janitor != nil {
		s.Janitor.SetSealed(s.Sealed, func(ctx context.Context, devices []model.Device) {
			// An unlocked device of the account then publishes a
			// device list without each (docs/EVENTS.md section 9).
			for _, d := range devices {
				s.Live.Send(d.AccountID, model.Event{Type: model.EventDeviceRetired, At: model.Now(), Payload: map[string]any{"device": d}})
			}
		})
	}
	s.API.Sealed, s.API.Push, s.API.UnlockPage = s.Sealed, s.Push, web.UnlockPage
}

// closeSealed stops wake-ups that are waiting to be retried.
func (s *Server) closeSealed() {
	if s.Push != nil {
		s.Push.Close()
	}
}
