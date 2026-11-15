package perm

import "github.com/tikaydaes/churr/internal/model"

// CheckChannelBan answers whether the actor of st may ban target from st's
// channel (section 6.1, ruling R72): whoever may remove members there, so an
// account acting as manager of a named channel and a manager of an ad-hoc
// one. Never in a sealed chat. A manager cannot ban an admin, nobody bans the
// owner or themselves, and a channel shared with other servers takes no new
// bans: they are this server's alone for now.
func CheckChannelBan(st State, target model.Account) error {
	if err := Check(st, BanMember); err != nil {
		return err
	}
	if st.Conversation.IsShared() {
		return model.ErrConflict.With("A channel shared with other servers cannot ban anyone yet. Remove them instead.")
	}
	switch {
	case target.ID == st.Actor.ID:
		return forbidden("You cannot ban yourself.")
	case target.Role == model.RoleOwner:
		return forbidden("Nobody can ban the owner from a channel.")
	case target.Role.IsAdmin() && !st.Actor.Role.IsAdmin():
		return forbidden("A manager cannot ban an admin.")
	case !target.Local():
		return forbidden("Someone on another server cannot be banned from a channel here.")
	}
	return nil
}

// CheckChannelUnban answers whether the actor of st may lift a ban in st's
// channel, or see its bans: whoever could have set one. A ban made before
// the channel was shared can still be lifted after.
func CheckChannelUnban(st State) error { return Check(st, BanMember) }
