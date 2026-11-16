package server

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/calls"
	"github.com/tikaydaes/churr/internal/forms"
	"github.com/tikaydaes/churr/internal/link"
	"github.com/tikaydaes/churr/internal/model"
)

// wireLink builds the link to other servers and connects it:
//
//   - chat hands local changes in shared conversations to it and takes what
//     arrives from other servers back, through chat.Outbound and the Apply
//     methods of chat;
//   - the live hub tells it, as well as people, when someone's presence
//     changes, so presence can go to the servers that share a channel with
//     them.
//
// The server's name on the link is its host name, and other servers dial
// wss://name/link/v1 (ws:// in a local trial, with the port).
//
// It runs after live, people and chat exist. Nothing connects until runLink.
func (s *Server) wireLink() error {
	address := "ws" + strings.TrimPrefix(s.Config.Origin(), "http") + "/link/v1"
	client := s.opts.LinkHTTPClient
	if client == nil {
		client = s.opts.HTTPClient
	}
	var err error
	s.Link, err = link.New(link.Deps{
		Store: s.Store, Keys: s.Keys, Chat: s.Chat, Perm: s.Perm, Blobs: s.Blobs, Log: s.Log,
		Name: s.Config.Hostname, URL: address, HTTPClient: client, Presence: shownPresence{s}, TmpDir: s.Config.TmpDir(),
		Now: s.opts.LinkNow, ClockSynced: s.Config.ClockSynced,
	})
	if err != nil {
		return err
	}
	s.Chat.SetOutbound(s.Link)
	s.People.SetLink(s.Link)
	// Presence from other servers goes to the connections showing the
	// account; one that starts showing it is told what was last heard.
	s.People.SetRemotePresence(func(account model.AccountID) (model.Presence, bool, bool) {
		p, ok := s.Link.PresenceOf(account)
		return model.Presence(p.Presence), p.InCall, ok
	})
	// Other servers are sent what people here are shown of someone's
	// presence, appearing away included, and are sent it when that
	// changes, whether by connecting or by appearing away or back.
	s.Live.OnPresence(s.People.PresenceChanged)
	s.People.OnShownPresence(s.Link.PresenceChanged)
	// A block is deaf ears across servers too (ruling R77,
	// docs/LINK-PROTOCOL.md section 17a): a block of an account of another
	// server is sent to that server, and what other servers send of blocks
	// their accounts made of accounts here is honoured wherever presence
	// or typing is shown.
	s.People.OnBlock(func(ctx context.Context, blocker, blocked model.AccountID, on bool) {
		s.Chat.BlockChanged(ctx, blocker, blocked, on)
		go s.Link.BlockChanged(context.WithoutCancel(ctx), blocked)
	})
	s.Perm.SetRemoteDeaf(s.Link.DeafViewers)
	s.Link.OnDeafEars(s.People.RemoteBlockChanged)
	return nil
}

// shownPresence is the presence the link sends other servers: what people
// here are shown (people.Service.PresenceShown), not only what the
// connections say, so someone who appears away appears away there too.
type shownPresence struct{ s *Server }

func (p shownPresence) Presence(account model.AccountID) model.Presence {
	return p.s.People.PresenceShown(account)
}

// callLink is the calls package's view of the link, and the link's view of
// the calls package: the two do not import each other, so the shapes each
// declares are turned into the other's here.
type callLink struct{ s *Server }

func (c callLink) RequestPass(ctx context.Context, host string, req calls.PassRequest) (calls.Pass, error) {
	p, err := c.s.Link.RequestPass(ctx, host, link.CallPassRequest{ConversationID: req.ConversationID, CallID: req.CallID, AccountID: req.AccountID})
	return calls.Pass{CallID: p.CallID, AccountID: p.AccountID, Pass: p.Pass, ExpiresAt: p.ExpiresAt,
		Media: calls.PassMedia{Host: p.Media.Host, UDPPort: p.Media.UDPPort, TCPPort: p.Media.TCPPort}}, err
}

func (c callLink) Signal(ctx context.Context, to string, sig calls.Signal) error {
	return c.s.Link.Signal(ctx, to, link.CallSignal{ConversationID: sig.ConversationID, CallID: sig.CallID, AccountID: sig.AccountID,
		Pass: sig.Pass, Signal: sig.Signal})
}

// callFields is a call event as it travels: the fields of
// docs/LINK-PROTOCOL.md section 11, and beside them what the calls package
// on the other server can use and any other receiver ignores.
type callFields struct {
	CallID          model.CallID           `json:"call_id"`
	StartedByID     model.AccountID        `json:"started_by_id,omitempty"`
	Host            string                 `json:"host,omitempty"`
	MessageID       model.MessageID        `json:"message_id,omitempty"`
	Call            *model.Call            `json:"call,omitempty"`
	AccountID       model.AccountID        `json:"account_id,omitempty"`
	Participant     *model.CallParticipant `json:"participant,omitempty"`
	EndedAt         model.Time             `json:"ended_at,omitempty"`
	Reason          string                 `json:"reason,omitempty"`
	DurationSeconds int64                  `json:"duration_seconds,omitempty"`
}

func (c callLink) Submit(ctx context.Context, conv model.ConversationID, e calls.StreamEvent) error {
	f := callFields{CallID: e.CallID, AccountID: e.AccountID, Participant: e.Participant, Reason: e.Reason, DurationSeconds: e.DurationSeconds}
	switch e.Type {
	case calls.EventStarted:
		if e.Call == nil {
			return nil
		}
		f.CallID, f.StartedByID, f.MessageID, f.Call, f.Host = e.Call.ID, e.Call.StartedByID, e.Call.MessageID, e.Call, c.s.Config.Hostname
	case calls.EventEnded:
		f.EndedAt = e.At
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	return c.s.Link.SubmitCallEvent(ctx, conv, e.Type, e.At, fields)
}

func (c callLink) LastHeard(server string) time.Time { return c.s.Link.LastHeard(server) }

func (c callLink) PassRequested(ctx context.Context, from string, req link.CallPassRequest) (link.CallPass, error) {
	p, err := c.s.Calls.PassRequested(ctx, from, calls.PassRequest{ConversationID: req.ConversationID, CallID: req.CallID, AccountID: req.AccountID})
	return link.CallPass{CallID: p.CallID, AccountID: p.AccountID, Pass: p.Pass, ExpiresAt: p.ExpiresAt,
		Media: link.CallMedia{Host: p.Media.Host, UDPPort: p.Media.UDPPort, TCPPort: p.Media.TCPPort}}, err
}

func (c callLink) Signalled(ctx context.Context, from string, sig link.CallSignal) {
	c.s.Calls.Signalled(ctx, from, calls.Signal{ConversationID: sig.ConversationID, CallID: sig.CallID, AccountID: sig.AccountID,
		Pass: sig.Pass, Signal: sig.Signal})
}

func (c callLink) StreamEvent(ctx context.Context, host string, hostID model.ServerID, conv model.ConversationID, eventType string, at model.Time, fields json.RawMessage) {
	var f callFields
	if json.Unmarshal(fields, &f) != nil {
		return
	}
	e := calls.StreamEvent{Type: eventType, At: at, Call: f.Call, CallID: f.CallID, AccountID: f.AccountID, Participant: f.Participant,
		Reason: f.Reason, DurationSeconds: f.DurationSeconds}
	if eventType == calls.EventStarted && e.Call == nil {
		e.Call = &model.Call{ID: f.CallID, ConversationID: conv, MessageID: f.MessageID, StartedByID: f.StartedByID, StartedAt: at}
	}
	if e.Call != nil {
		// The call is in this server's copy of the conversation.
		e.Call.ConversationID, e.Call.HostedBy = conv, host
	}
	c.s.Calls.StreamEvent(ctx, host, hostID, conv, e)
}

// runLink connects the link to the packages that are built after it (calls,
// sealed chats), then connects to the peers this server dials and starts
// the link's background work.
func (s *Server) runLink(ctx context.Context) {
	if s.Calls != nil {
		s.Calls.SetLink(callLink{s})
		s.Link.SetCalls(callLink{s})
	}
	if s.Sealed != nil {
		// Device lists cross the link between the home servers of two
		// people (docs/LINK-PROTOCOL.md section 16).
		s.Sealed.SetOutbound(s.Link)
		s.Link.SetSealed(s.Sealed)
	}
	if s.Forms != nil {
		// The rules a poll's home server holds every server's answers to
		// (docs/LINK-PROTOCOL.md section 19).
		s.Link.SetForms(forms.Shared{})
	}
	if err := s.Link.Start(ctx); err != nil {
		s.Log.Error("the link to other servers could not start", "err", err)
	}
}

// closeLink says goodbye to every peer.
func (s *Server) closeLink() {
	if s.Link != nil {
		s.Link.Stop()
	}
}
