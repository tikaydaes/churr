package server

import (
	"context"

	"github.com/tikaydaes/churr/internal/calls"
)

// wireCalls builds the calls service, which opens the media port, and
// connects it:
//
//   - the hub hands it the `call.` frames of live connections and tells it
//     when a connection closes, so a participant whose signalling dropped is
//     held for thirty seconds;
//   - chat shows a conversation's live call through it, and posts and
//     changes the call's one notice message for it;
//   - notify says who a starting call rings; people marks who is in a call;
//   - the status endpoint and the admin Overview read from it.
//
// It runs after live, people, chat and notify exist. Calls in shared
// conversations need the link package too: whoever builds the link hands
// it over with s.Calls.SetLink.
func (s *Server) wireCalls() error {
	c, err := calls.New(calls.Deps{
		Config: calls.Config{Listen: s.Config.Media, Addresses: s.Config.MediaAddress, Hostname: s.Config.Hostname},
		Store:  s.Store, Perm: s.Perm, Hub: s.Live, Chat: s.Chat, Rings: s.Notify, Marks: s.People, Log: s.Log,
		Timings: s.opts.CallTimings,
	})
	if err != nil {
		return err
	}
	s.Calls = c
	s.Chat.SetCalls(c)
	s.People.SetCalls(c)
	s.Live.Handle("call.", c.HandleClient)
	s.Live.OnClose(c.ConnectionClosed)
	return nil
}

// overviewCalls is the calls service as the admin Overview asks about it.
type overviewCalls struct{ c *calls.Server }

func (o overviewCalls) LiveCalls() int       { return o.c.Count() }
func (o overviewCalls) MediaAddress() string { return o.c.MediaAddress(context.Background()) }
