package people

import (
	"context"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// InCall is what people needs of the calls package: whether an account is
// in a call right now, for the in-a-call mark beside its presence
// (docs/CALLS.md section 15). calls.Server implements it.
type InCall interface {
	InCall(account model.AccountID) bool
}

// SetCalls gives the service the calls package. Until then nobody is shown
// as in a call.
func (s *Service) SetCalls(c InCall) { s.calls = c }

// inCall reports whether the account is in a call right now.
func (s *Service) inCall(account model.AccountID) bool {
	return s.calls != nil && s.calls.InCall(account)
}

// CallChanged is called by the calls package when an account joins or
// leaves a call. The accounts that can see it are told with
// presence.changed, whose `in_call` is what changed.
func (s *Service) CallChanged(account model.AccountID) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, err := s.store.Accounts().Get(ctx, account)
	if err != nil || a.Removed() {
		return
	}
	v := s.viewOf(a, s.now())
	s.tellPresence(ctx, account, v.presence)
}
