package people

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Profiles is what people needs of the link to other servers: to be told
// when the profile of one of this server's accounts changes, so the servers
// that share a channel with the account get its new profile
// (docs/LINK-PROTOCOL.md section 14). The link package implements it.
type Profiles interface {
	AccountChanged(ctx context.Context, account model.AccountID)
}

// SetLink connects the link package.
func (s *Service) SetLink(p Profiles) { s.link = p }

// profileChanged tells the link, if there is one.
func (s *Service) profileChanged(ctx context.Context, account model.AccountID) {
	if s.link != nil {
		// The link may wait for a sequencer; the caller does not.
		go s.link.AccountChanged(context.WithoutCancel(ctx), account)
	}
}

// RemotePresence answers with the newest presence another server sent for
// one of its accounts, and ok false for an account it has none for. The
// link package has it (link.Service.PresenceOf, through the server).
type RemotePresence func(account model.AccountID) (presence model.Presence, inCall, ok bool)

// SetRemotePresence connects what is known of accounts on other servers.
// Their changes go only to the connections showing them, so a connection
// that starts showing one must be told where it stands, as for a local
// account.
func (s *Service) SetRemotePresence(fn RemotePresence) { s.remote = fn }
