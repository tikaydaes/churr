package perm

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Rules about groups and about what the owner and admins do to other
// accounts that go beyond the plain rows of section 9.1.

// CanSeeGroup is the group half of section 4.3: owner, admin and member see
// every group; a guest or bot sees only the groups it is in and the groups
// added to its conversations (decision P6). A group the actor may not see
// does not exist for them.
func (c *Checker) CanSeeGroup(ctx context.Context, actor model.Account, group model.GroupID) error {
	if !actor.Active() {
		return model.ErrUnauthenticated
	}
	if actor.Role.IsFullMember() {
		return nil
	}
	if actor.Role != model.RoleGuest && actor.Role != model.RoleBot {
		return model.ErrNotFound
	}
	seen, err := c.s.Groups().SeenBy(ctx, group, actor.ID)
	if err != nil {
		return err
	}
	if !seen {
		return model.ErrNotFound
	}
	return nil
}

// CheckJoinGroup answers whether actor may join or leave a group by
// themselves: an owner, admin or member, and only a group that is open,
// which a built-in or provider-mapped group never is (sections 9.1 and 9.3,
// docs/FEATURES.md section 14).
func CheckJoinGroup(actor model.Account, g model.Group, set Settings) error {
	if err := CheckServer(actor, JoinOpenGroup, set); err != nil {
		return err
	}
	if g.Builtin != "" || g.ProviderID != "" || !g.Open {
		return forbidden("This group is not one people join or leave by themselves.")
	}
	return nil
}

// CheckEditGroup answers whether actor may change, delete or set the
// members of a group. Admins manage ordinary groups; nobody edits the two
// built-in ones (case 108).
func CheckEditGroup(actor model.Account, g model.Group, set Settings) error {
	if err := CheckServer(actor, ManageGroups, set); err != nil {
		return err
	}
	if g.Builtin != "" {
		return forbidden("The built-in groups follow roles and cannot be changed.")
	}
	return nil
}

// CheckSetRole answers whether actor may give target a role (section 9.1,
// "Change an account's role; hand over ownership"). Only the owner may.
// The role is admin, member or guest, or owner to hand the server over. A
// bot's role cannot be changed, nor that of a remote, removed or unclaimed
// account, and the owner's own changes only by handing over.
func CheckSetRole(actor, target model.Account, role model.Role, set Settings) error {
	if err := CheckServer(actor, ChangeRole, set); err != nil {
		return err
	}
	switch role {
	case model.RoleOwner, model.RoleAdmin, model.RoleMember, model.RoleGuest:
	default:
		return model.Invalid("role", "A role is owner, admin, member or guest.")
	}
	switch {
	case target.Role == model.RoleBot:
		return model.ErrConflict.With("A bot's role cannot be changed.")
	case target.Role == model.RoleRemote || !target.Active():
		return model.ErrConflict.With("Only an active account on this server can be given a role.")
	case target.ID == actor.ID:
		return model.ErrConflict.With("To stop being the owner, hand the server over to someone else.")
	case role == model.RoleOwner && target.Role == model.RoleGuest:
		return model.ErrConflict.With("The server can be handed to an admin or a member.")
	}
	return nil
}
