package perm

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Watches: "tell me when this person is around" (ruling R76,
// docs/PERMISSIONS.md section 13a). A watch is for someone whose presence
// the caller is already shown, which is someone they share a conversation
// with (section 4.3), so it tells them nothing presence does not.

// CanWatch says whether actor may ask to be told when target is next around.
// Nothing about the target's own choices decides it: whether the target has
// blocked the actor plays no part here, so no refusal can show it. (Such a
// target always appears online to the actor, ruling R77, so the scheduler
// answers that they are around.)
func (c *Checker) CanWatch(ctx context.Context, actor, target model.Account) error {
	if !actor.Active() {
		return model.ErrUnauthenticated
	}
	if actor.Role == model.RoleBot {
		return forbidden("A bot cannot watch for anyone.")
	}
	if err := c.CanSeeAccount(ctx, actor, target); err != nil {
		return err
	}
	if target.ID == actor.ID {
		return forbidden("You cannot watch for yourself.")
	}
	if target.Role == model.RoleBot {
		return forbidden("A bot is around whenever it is connected; there is nothing to watch for.")
	}
	if target.Removed() {
		return forbidden(name(target) + " is no longer a member here.")
	}
	shares, err := c.sharesOrGhost(ctx, actor.ID, target.ID)
	if err != nil {
		return err
	}
	if !shares {
		return forbidden("You can watch only for someone you share a conversation with.")
	}
	set, err := c.Settings(ctx)
	if err != nil {
		return err
	}
	// The caller's own block is the caller's to know: refused plainly.
	if mine, err := c.blockedBy(ctx, set, target.ID, actor.ID); err != nil {
		return err
	} else if mine {
		return forbidden("You are ignoring " + name(target) + ".")
	}
	return nil
}

// WatchMayFire says whether a watch of setter's for target may be told now,
// when target is around: the setter is still shown target's presence (they
// share a conversation, and target has not been removed or banned), and
// neither has blocked the other. When it says no, the watch stays waiting
// and its owner is told nothing. A block made by a person of another server
// is not known here, so it cannot stop a watch for them.
func (c *Checker) WatchMayFire(ctx context.Context, setter, target model.Account) (bool, error) {
	if !setter.Active() || target.Removed() || target.Role == model.RoleBot {
		return false, nil
	}
	shares, err := c.s.Accounts().SharesConversation(ctx, setter.ID, target.ID)
	if err != nil || !shares {
		return false, err
	}
	set, err := c.Settings(ctx)
	if err != nil {
		return false, err
	}
	for _, pair := range [][2]model.AccountID{{setter.ID, target.ID}, {target.ID, setter.ID}} {
		if blocked, err := c.blockedBy(ctx, set, pair[0], pair[1]); err != nil || blocked {
			return false, err
		}
	}
	return true, nil
}

// HasBlocked reports whether blocker has blocked blocked, while blocking is
// on for the server.
func (c *Checker) HasBlocked(ctx context.Context, blocker, blocked model.AccountID) (bool, error) {
	set, err := c.Settings(ctx)
	if err != nil {
		return false, err
	}
	return c.blockedBy(ctx, set, blocked, blocker)
}

// name is how a refusal names an account.
func name(a model.Account) string {
	if a.DisplayName != "" {
		return a.DisplayName
	}
	return a.Username
}

// sharesOrGhost reports whether viewer shares a conversation with target, or
// is shown target as a member of an open-looking chat target is not in: one
// with someone who blocked the viewer (ruling R77). Either way target's
// presence is shown to the viewer, online in the second case.
func (c *Checker) sharesOrGhost(ctx context.Context, viewer, target model.AccountID) (bool, error) {
	shares, err := c.s.Accounts().SharesConversation(ctx, viewer, target)
	if err != nil || shares {
		return shares, err
	}
	ghosts, err := c.s.ChatRequests().GhostPeers(ctx, viewer)
	if err != nil {
		return false, err
	}
	for _, g := range ghosts {
		if g == target {
			return true, nil
		}
	}
	return false, nil
}
