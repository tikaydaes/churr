package perm

import "github.com/tikaydaes/churr/internal/model"

// The rules of webhooks that depend on more than a conversation and an
// action: who decides a request for a channel they may not be in, and who
// may revoke or reset a webhook (docs/PERMISSIONS.md sections 7 and 15,
// docs/BOTS.md section 9).

// CheckDecideWebhook answers whether the actor of st may approve (or, with
// approving false, decline) a request for a webhook in st's conversation.
//
// An admin decides every request, also for a private channel the admin is
// not in: the request came from a member who is, and names the channel. The
// managers of the channel decide too when the admin setting hands that to
// them. Approving adds the webhook's account, so it is refused in an
// archived channel and in a locked one; declining adds nobody and is not.
func CheckDecideWebhook(st State, approving bool) error {
	if !st.Actor.Active() {
		return model.ErrUnauthenticated
	}
	c := st.Conversation
	if c.IsLibrary() {
		return model.ErrNotFound
	}
	switch {
	case st.Actor.Role.IsAdmin():
	case !st.CanSee():
		return model.ErrNotFound
	case st.Settings.WebhooksManagersMayApprove && st.Level() == model.LevelManager && st.Actor.Role.IsFullMember():
	default:
		return forbidden("Only an admin can decide a webhook request.")
	}
	if !c.Named() {
		// Ruling R8: a webhook exists only in a named channel.
		return forbidden("That is for named channels only.")
	}
	if approving {
		if c.Archived() {
			return model.ErrArchived
		}
		if c.Locked() {
			return model.ErrLocked
		}
	}
	return nil
}

// CheckManageWebhook answers whether the actor of st may revoke a webhook of
// st's conversation or replace its address: the account that asked for it,
// a manager of the channel, or an admin. Someone who cannot see the channel
// and is none of those is told there is no such webhook.
func CheckManageWebhook(st State, requester model.AccountID) error {
	if !st.Actor.Active() {
		return model.ErrUnauthenticated
	}
	switch {
	case st.Actor.Role.IsAdmin():
		return nil
	case st.Actor.ID == requester && st.Actor.Role.IsFullMember():
		return nil
	case !st.CanSee() || st.webhook():
		return model.ErrNotFound
	case st.Level() == model.LevelManager && st.Actor.Role != model.RoleBot:
		return nil
	}
	return forbidden("Only the person who asked for it, a manager of the channel or an admin can do that.")
}

// CheckListWebhooks answers whether the actor of st may see which webhooks
// st's conversation has: its members, and admins. Nobody sees an address.
func CheckListWebhooks(st State) error {
	if !st.Actor.Active() {
		return model.ErrUnauthenticated
	}
	if st.Conversation.IsLibrary() || st.webhook() {
		return model.ErrNotFound
	}
	if st.IsMember() || (st.Actor.Role.IsAdmin() && st.CanSee()) {
		return nil
	}
	if st.CanSee() {
		return forbidden("Join the channel first.")
	}
	return model.ErrNotFound
}
