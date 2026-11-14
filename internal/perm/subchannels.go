package perm

import "github.com/tikaydaes/churr/internal/model"

// SetParent is setting or clearing a channel's parent (section 7.2).
const SetParent Action = "set_parent"

func init() {
	// An account acting as manager of the sub-channel; who else must hold
	// is in CheckSetParent.
	rules[SetParent] = rule{need: needManager, archived: true, timedOut: true}
}

// SubChannelParent says which channel a channel the server makes out of c
// (a thread moved out, a ticket opened by a form) sits under: c itself, or
// c's own parent when c is a sub-channel, so that there is only ever one
// level; and none when c is not a named channel. The server does this by
// itself and it needs no right over the parent (section 7.2).
func SubChannelParent(c model.Conversation) model.ConversationID {
	switch {
	case !c.Named():
		return ""
	case c.ParentID != "":
		return c.ParentID
	}
	return c.ID
}

// SeesParent reports whether the actor of st, which is the actor's state in
// the PARENT channel, may be told that a sub-channel sits under it: whoever
// can see the parent, and an admin for any named channel, since the admin
// view shows those. Everyone else is given the sub-channel with no
// parent_id, so a private parent's existence is not revealed.
func SeesParent(parent State) bool {
	if !parent.Actor.Active() || parent.Conversation.IsLibrary() {
		return false
	}
	return parent.CanSee() || (parent.Actor.Role.IsAdmin() && parent.Conversation.Named())
}

// parentFits applies what must hold of a parent whoever asks: it can be
// seen by the actor, is a named channel, is not the child, and is not
// itself a sub-channel. childID is "" for a channel being created.
func parentFits(parent State, childID model.ConversationID) error {
	if !SeesParent(parent) {
		// Naming a private channel as parent cannot be used to test
		// whether it exists.
		return model.ErrNotFound
	}
	c := parent.Conversation
	if !c.Named() {
		return model.Invalid("parent_id", "Only a named channel can have sub-channels.")
	}
	if childID != "" && c.ID == childID {
		return model.Invalid("parent_id", "A channel cannot be its own parent.")
	}
	if c.ParentID != "" {
		return model.ErrConflict.With("That channel is itself a sub-channel. Sub-channels go one level deep.")
	}
	return nil
}

// CheckSetParent answers whether the actor may set or clear a channel's
// parent (section 7.2). child is the actor's state in the channel; parent
// is its state in the channel to sit under, or nil to clear. childHasSubs
// says the channel has sub-channels of its own. viaAdmin is the admin
// console's path, where an admin acts on a named channel without being
// able to read it.
//
//   - an account acting as manager of the sub-channel that is also a member
//     of the parent may set and clear;
//   - one that is not a member of the parent may only clear;
//   - an admin may set and clear for channels the admin can see.
func CheckSetParent(child State, parent *State, childHasSubs, viaAdmin bool) error {
	if !child.Actor.Active() {
		return model.ErrUnauthenticated
	}
	if viaAdmin {
		if err := Check(child, AdminView); err != nil {
			return err
		}
	} else {
		if !child.CanSee() {
			return model.ErrNotFound
		}
		if !child.Conversation.Named() {
			return model.Invalid("parent_id", "Only a named channel can be a sub-channel.")
		}
		if err := Check(child, SetParent); err != nil {
			return err
		}
	}
	if child.Conversation.Archived() {
		return model.ErrArchived
	}
	if parent == nil {
		return nil
	}
	if err := parentFits(*parent, child.Conversation.ID); err != nil {
		return err
	}
	if childHasSubs {
		return model.ErrConflict.With("This channel has sub-channels of its own. Sub-channels go one level deep.")
	}
	if !child.Actor.Role.IsAdmin() && !parent.IsMember() {
		return forbidden("Join that channel first: only its members can put a channel under it.")
	}
	return nil
}

// CheckCreateUnder answers whether the actor of parent, its state in the
// channel named as parent, may create a channel under it: a member of that
// parent, or an admin. That the actor may create channels at all is
// CheckServer's.
func CheckCreateUnder(parent State) error {
	if !parent.Actor.Active() {
		return model.ErrUnauthenticated
	}
	if err := parentFits(parent, ""); err != nil {
		return err
	}
	if !parent.Actor.Role.IsAdmin() && !parent.IsMember() {
		return forbidden("Join that channel first: only its members can put a channel under it.")
	}
	return nil
}
