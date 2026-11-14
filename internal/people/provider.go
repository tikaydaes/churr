package people

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// The two methods here are for changes to people that are made outside this
// package and already committed: company sign-in moves an account between
// groups and between member and admin from what the identity provider says
// (docs/AUTH.md section 7.6). The rows are changed in the sign-in's own
// transaction; these send the same events an admin's change would have.

// AnnounceAccount sends account.updated for an account whose role was
// changed by something other than an admin's request.
func (s *Service) AnnounceAccount(ctx context.Context, id model.AccountID) {
	s.forget(id)
	a, err := s.store.Accounts().Get(ctx, id)
	if err != nil {
		s.log.Error("account.updated could not be built", "err", err)
		return
	}
	s.tellProfile(ctx, a)
}

// AnnounceGroupMember sends group.member_added or group.member_removed for
// an account that was put in a group, or taken out, by something other than
// a request to this service. The account itself is always told, as it is
// when an admin does the same: taken out, it may no longer see the group.
func (s *Service) AnnounceGroupMember(ctx context.Context, group model.GroupID, account model.AccountID, added bool) {
	// Which conversations the account is in changed with the group.
	s.forget(account)
	g, err := s.store.Groups().Get(ctx, group, account)
	if err != nil {
		s.log.Error("a group event could not be built", "err", err)
		return
	}
	to, in := s.groupAudience(ctx, g)
	eventType := model.EventGroupMemberAdded
	if !added {
		eventType = model.EventGroupMemberRemoved
	}
	s.tellGroup(eventType, g, to, in, map[string]any{"group_id": g.ID, "account_id": account}, account)
}
