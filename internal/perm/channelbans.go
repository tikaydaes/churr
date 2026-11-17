package perm

import "github.com/tikaydaes/churr/internal/model"

// CheckChannelBan answers whether the actor of st may ban target from st's
// channel (section 6.1, ruling R72): whoever may remove members there, the
// channel's moderators and managers. Never in a sealed chat. A moderator
// does not ban a moderator or a manager of the channel, nobody bans an Owner
// of the instance or themselves, and a channel shared with other servers
// takes no new bans: they are this server's alone for now. targetLevel is
// the target's level in the channel, or "" if it is not in it.
func CheckChannelBan(st State, target model.Account, targetLevel model.Level) error {
	if err := Check(st, BanMember); err != nil {
		return err
	}
	if st.Conversation.IsShared() {
		return model.ErrConflict.With("A channel shared with other servers cannot ban anyone yet. Remove them instead.")
	}
	if !target.Local() {
		return forbidden("Someone on another server cannot be banned from a channel here.")
	}
	return againstKeeper(st, target, targetLevel, "ban")
}

// CheckChannelUnban answers whether the actor of st may lift a ban in st's
// channel, or see its bans: whoever could have set one. A ban made before
// the channel was shared can still be lifted after.
func CheckChannelUnban(st State) error { return Check(st, BanMember) }
