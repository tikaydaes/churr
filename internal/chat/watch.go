package chat

import "github.com/tikaydaes/churr/internal/model"

// OnRemoteAround sets who is told when another server says one of its
// accounts is online, in a stream this server shares with it: the watches
// (ruling R76), which this server keeps for its own people whoever they
// watch. It is set once while the server is being put together.
func (s *Service) OnRemoteAround(fn func(account model.AccountID)) { s.remoteAround = fn }

// tellRemoteAround passes on presence from another server that says online.
func (s *Service) tellRemoteAround(account model.AccountID, presence any) {
	if s.remoteAround == nil {
		return
	}
	if p, _ := presence.(string); model.Presence(p) == model.PresenceOnline {
		s.remoteAround(account)
	} else if p, _ := presence.(model.Presence); p == model.PresenceOnline {
		s.remoteAround(account)
	}
}
