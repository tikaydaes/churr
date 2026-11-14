package perm

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// The rules of sealed chats that are not about one action in one
// conversation: who may start one with whom (docs/PERMISSIONS.md sections
// 5 note 3, 9.1 and 13), and when an existing one accepts nothing new
// (docs/SEALED.md sections 7.3 and 16).

// CheckStartSealed is the part of starting a sealed chat that depends only
// on the two accounts and the settings: sealed chats are on, the starter is
// a person, the other account is one too, and it is somebody else.
func CheckStartSealed(actor, other model.Account, set Settings) error {
	if err := CheckServer(actor, StartSealed, set); err != nil {
		return err
	}
	if other.ID == actor.ID {
		return model.Invalid("account_id", "A sealed chat is with somebody else.")
	}
	if other.Role == model.RoleBot {
		// Section 17, case 84.
		return forbidden("Bots cannot be in sealed chats.")
	}
	return nil
}

// CanStartSealed answers whether actor may start a sealed chat with other:
// CheckStartSealed, then what starting any chat with someone needs (the
// other account is visible to the actor, can act, has not blocked the
// actor, and a remote one shares a shared channel), and no server-wide
// time-out.
func (c *Checker) CanStartSealed(ctx context.Context, actor, other model.Account) error {
	set, err := c.Settings(ctx)
	if err != nil {
		return err
	}
	if err := CheckStartSealed(actor, other, set); err != nil {
		return err
	}
	if err := c.reachable(ctx, set, actor, other); err != nil {
		return err
	}
	_, timedOut, err := c.s.Moderation().TimedOutUntil(ctx, actor.ID, "", c.now())
	if err != nil {
		return err
	}
	if timedOut {
		return model.ErrTimedOut
	}
	return nil
}

// SealedReadOnly reports whether a sealed chat accepts nothing new: sealed
// chats are switched off for the server, or the other person was removed
// or banned. Reading continues either way.
func SealedReadOnly(set Settings, other model.Account) bool {
	return !set.SealedEnabled || sealedPartnerGone(other)
}

// sealedPartnerGone reports whether the other person of a sealed chat can
// no longer take part. For someone on another server that is all this
// server knows of them: that their account was removed. Whether the two
// still share a shared channel is the link's to say
// (docs/LINK-PROTOCOL.md section 16).
func sealedPartnerGone(other model.Account) bool {
	if other.Role == model.RoleRemote {
		return other.ID == "" || other.Removed()
	}
	return !other.Active()
}

// CheckSealedWrite refuses a message or an epoch record in a sealed chat
// that has become read-only. st is the writer's state in the chat and
// other the other member.
func CheckSealedWrite(st State, other model.Account) error {
	if !st.Conversation.Sealed() {
		return model.BadRequest("That is only for sealed chats.")
	}
	if !st.Settings.SealedEnabled {
		return forbidden("Sealed chats are switched off on this server. This one can be read but not written in.")
	}
	if sealedPartnerGone(other) {
		return forbidden("The other person is no longer on this server. This chat can be read but not written in.")
	}
	return nil
}

// CanSeeDeviceLists answers whether actor may fetch target's signed device
// lists: its own, those of an account it has a sealed chat with (the
// caller says so; it comes from the store), and those of an account it may
// start one with, since the first epoch record of a new chat is sealed to
// them. Anyone else is told the account has none to show.
func (c *Checker) CanSeeDeviceLists(ctx context.Context, actor, target model.Account, sealedPartner bool) error {
	if !actor.Active() {
		return model.ErrUnauthenticated
	}
	if actor.ID == target.ID || sealedPartner {
		return nil
	}
	if err := c.CanStartSealed(ctx, actor, target); err != nil {
		if model.CodeOf(err) == model.CodeFeatureOff {
			return err
		}
		// Whatever the reason, it must not be told apart from an
		// account that does not exist.
		return model.ErrNotFound
	}
	return nil
}
