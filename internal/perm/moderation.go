package perm

import (
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// CheckFile answers whether the actor of st may download a file that lives
// in st's conversation (docs/PERMISSIONS.md section 4.2, "Files"). msg is
// the message the file is on, or nil for a file on none. Everything refused
// here is not_found: a file the actor may not fetch does not exist for them.
func CheckFile(st State, f model.File, msg *model.Message) error {
	if !st.Actor.Active() {
		return model.ErrUnauthenticated
	}
	if f.ConversationID != st.Conversation.ID {
		return model.ErrNotFound
	}
	if st.Conversation.IsLibrary() {
		// Avatars and emoji: any signed-in account (decision P5). The
		// preview cache's own images are never linked from anything.
		if f.Purpose == model.PurposeAvatar || f.Purpose == model.PurposeEmoji {
			return nil
		}
		return model.ErrNotFound
	}
	if f.MessageID == "" && f.Purpose == model.PurposeCard {
		// The image or icon of a card a webhook or bot posted: it is on
		// no message row, and is read with the conversation.
		if !st.CanRead() {
			return model.ErrNotFound
		}
		return nil
	}
	if f.MessageID == "" {
		// An upload not yet attached can be fetched only by its uploader,
		// and only while they can still read where it was uploaded.
		if f.UploaderID != st.Actor.ID || !st.CanRead() {
			return model.ErrNotFound
		}
		return nil
	}
	if !st.CanRead() {
		return model.ErrNotFound
	}
	if !f.DetachedAt.IsZero() {
		// Taken off its message by an edit or a delete: it is part of
		// the message's history and follows that rule (section 8.2).
		if msg == nil || CheckMessage(st, OpenHistory, *msg) != nil {
			return model.ErrNotFound
		}
	}
	return nil
}

// CheckTimeOut answers whether the actor of st may time target out in st's
// conversation (section 11): an account acting as manager, but a manager
// cannot time out an admin or the owner, an admin cannot time out another
// admin, and nobody times out the owner.
func CheckTimeOut(st State, target model.Account) error {
	if err := Check(st, TimeOutMember); err != nil {
		return err
	}
	return timeOutTarget(st.Actor, target)
}

func timeOutTarget(actor, target model.Account) error {
	switch {
	case target.ID == actor.ID:
		return forbidden("You cannot time yourself out.")
	case target.Role == model.RoleOwner:
		return forbidden("Nobody can time out the owner.")
	case target.Role == model.RoleAdmin && actor.Role != model.RoleOwner:
		return forbidden("Only the owner can time out an admin.")
	}
	return nil
}

// CheckRetention answers whether the actor of st may give st's channel a
// retention period of its own (docs/FEATURES.md section 6). seconds is the
// period asked for; 0 clears the channel's own period, which anyone acting
// as manager may do. An admin may set any period. A manager may set one
// shorter than the server's, or any if the server has none.
func CheckRetention(st State, seconds int64) error {
	if err := Check(st, ShortenRetention); err != nil {
		return err
	}
	if seconds == 0 || Check(st, SetRetention) == nil {
		return nil
	}
	if server := st.Settings.RetentionSeconds; server > 0 && seconds >= server {
		return forbidden("Only an admin can set a retention period as long as the server's or longer.")
	}
	return nil
}

// CheckRemoveEmoji answers whether actor may remove a custom emoji (section
// 9.1): an admin, or the member who added it while members may add emoji.
func CheckRemoveEmoji(actor model.Account, e model.Emoji, set Settings) error {
	if err := CheckServer(actor, RemoveAnyEmoji, set); err == nil {
		return nil
	}
	if !actor.Active() {
		return model.ErrUnauthenticated
	}
	if e.CreatedByID == actor.ID && CheckServer(actor, AddEmoji, set) == nil {
		return nil
	}
	return forbidden("Only an admin, or whoever added it, can remove a custom emoji.")
}

// ReportScope describes whose reports the actor may handle, for the list of
// reports to join against (section 8.4): conversations where its stored
// level is manager, and for an admin every public channel as well.
func ReportScope(actor model.Account) store.ReportScope {
	if !actor.Active() {
		return store.ReportScope{}
	}
	return store.ReportScope{AccountID: actor.ID, PublicChannels: actor.Role.IsAdmin()}
}
