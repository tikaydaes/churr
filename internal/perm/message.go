package perm

import (
	"github.com/tikaydaes/churr/internal/model"
)

// MessageAction is something done to one message.
type MessageAction string

// What can be done to a message (sections 5 and 8).
const (
	ReadMessage      MessageAction = "read_message"
	EditMessage      MessageAction = "edit_message"
	DeleteMessage    MessageAction = "delete_message"
	OpenHistory      MessageAction = "open_history"
	AskPurge         MessageAction = "ask_purge"
	PurgeDirectly    MessageAction = "purge_directly"
	CloseOwnForm     MessageAction = "close_own_form"
	RemoveOwnPreview MessageAction = "remove_own_preview"
	ReactTo          MessageAction = "react_to"
	TickOn           MessageAction = "tick_on"
	MoveThread       MessageAction = "move_thread"
)

// CheckMessage answers for one message in the conversation of st. A message
// in a conversation the actor cannot read does not exist for them.
func CheckMessage(st State, a MessageAction, m model.Message) error {
	if !st.Actor.Active() {
		return model.ErrUnauthenticated
	}
	if m.ConversationID != st.Conversation.ID || !st.CanRead() {
		return model.ErrNotFound
	}
	own := m.AuthorID == st.Actor.ID
	c := st.Conversation
	switch a {
	case ReadMessage:
		return nil

	case ReactTo:
		if err := Check(st, React); err != nil {
			return err
		}
		if m.Deleted() {
			return model.ErrConflict.With("That message has been deleted.")
		}
		return nil

	case TickOn:
		return Check(st, Tick)

	case EditMessage:
		// Section 5 note 5: nobody edits another account's message,
		// whatever their role; the author needs level member, an open
		// channel, no time-out, and an open edit window (bots excepted).
		if !own {
			return forbidden("Only its author can edit a message.")
		}
		if st.webhook() {
			return forbidden("A webhook can only post.")
		}
		if !st.IsMember() || !st.Level().AtLeast(model.LevelMember) {
			return forbidden("You can no longer edit here.")
		}
		if c.Archived() {
			return model.ErrArchived
		}
		if !st.TimedOutUntil.IsZero() {
			return model.ErrTimedOut
		}
		if w := st.Settings.EditWindowSeconds; w > 0 && st.Actor.Role != model.RoleBot {
			if st.Now.Sub(m.CreatedAt).Seconds() > float64(w) {
				return forbidden("The time for editing this message has passed.")
			}
		}
		return nil

	case DeleteMessage:
		// Section 8.1. The author keeps this right while they can read
		// the conversation (decision P8), archived or timed out or not.
		if st.webhook() {
			return forbidden("A webhook can only post.")
		}
		if own {
			return nil
		}
		if c.Sealed() {
			return forbidden("Only its author can delete a message in a sealed chat.")
		}
		if st.HubModerator {
			// Section 14.1: a moderator of a hub-owned channel deletes any
			// message in it, for every server, whatever their level here.
			return moderatorMay(st)
		}
		return Check(st, DeleteOthersMessage)

	case OpenHistory:
		// A sealed chat has no history on the server: an edit there is
		// a new sealed message (docs/SEALED.md section 8.3).
		if c.Sealed() {
			return forbidden("Sealed chats keep no history on the server.")
		}
		// Sections 8.2 and 8.3: while a purge has been asked for, only
		// admins who can read the conversation, and not the author.
		if !m.HistoryHiddenAt.IsZero() && !st.Actor.Role.IsAdmin() {
			return forbidden("This message's history is not available.")
		}
		return nil

	case AskPurge:
		if c.Sealed() {
			return forbidden("Sealed chats keep no history on the server.")
		}
		if !own {
			return forbidden("Only its author can ask for a message to be purged.")
		}
		return nil

	case PurgeDirectly:
		// Any message an admin can read. A manager who is not an admin
		// can delete and cannot purge.
		if c.Sealed() || !st.Actor.Role.IsAdmin() {
			return forbidden("Only an admin can purge a message.")
		}
		return nil

	case CloseOwnForm:
		if own {
			if c.Archived() {
				return model.ErrArchived
			}
			return nil
		}
		return Check(st, CloseAnyForm)

	case RemoveOwnPreview:
		if own {
			return Check(st, Post)
		}
		return Check(st, RemoveAnyPreview)

	case MoveThread:
		// The thread's starter is the author of its parent.
		if own {
			return Check(st, MoveOwnThread)
		}
		return Check(st, MoveAnyThread)
	}
	return forbidden("You may not do this.")
}

// CheckSetLevel answers whether the actor of st may set target's direct
// level in st's conversation (section 6 note 7, decision P19).
func CheckSetLevel(st State, target model.Account, newLevel model.Level) error {
	if err := Check(st, SetLevel); err != nil {
		return err
	}
	if target.Role == model.RoleRemote && newLevel == model.LevelManager {
		return forbidden("Someone on another server cannot be a manager here.")
	}
	return nil
}

// CheckRemoveMember answers whether the actor of st may remove target, whose
// member row is targetMember, from st's conversation (section 6 note 6).
func CheckRemoveMember(st State, target model.Account, targetMember model.Member) error {
	if err := Check(st, RemoveMember); err != nil {
		return err
	}
	if !st.Actor.Role.IsAdmin() && target.Role.IsAdmin() {
		return forbidden("A manager cannot remove an admin.")
	}
	if target.Role == model.RoleOwner && st.Actor.Role != model.RoleOwner {
		return forbidden("The owner cannot be removed.")
	}
	if targetMember.ViaGroup {
		return model.ErrConflict.With("That account is in the channel through a group. Remove the group, or take them out of it.")
	}
	return nil
}

// moderatorMay is what a moderator of a hub-owned channel still needs from
// their own server before it signs a removal in their name (section 14.1):
// an active account of a person, that can see the conversation, and is not
// timed out here. Their level in the local copy does not come into it, and
// they need not be a member of it.
func moderatorMay(st State) error {
	switch {
	case !st.Actor.Active():
		return model.ErrUnauthenticated
	case st.Actor.Role == model.RoleBot || st.Actor.Role == model.RoleRemote:
		return forbidden("Only a person's account moderates a channel.")
	case !st.CanSee():
		return model.ErrNotFound
	case !st.TimedOutUntil.IsZero():
		return model.ErrTimedOut
	}
	return nil
}
