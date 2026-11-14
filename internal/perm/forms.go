package perm

import (
	"github.com/tikaydaes/churr/internal/model"
)

// This file holds the rules of forms, scheduled items and commands that are
// not one row of a matrix: they depend on what the form or the item is.

// formAction maps a form's action to the row of section 5 that allows
// posting it.
var formAction = map[string]Action{
	model.FormTally:   PostForm,
	model.FormCollect: PostForm,
	model.FormThread:  PostForm,
	model.FormChannel: PostChannelForm,
	model.FormGroup:   PostGroupForm,
	model.FormBot:     PostBotForm,
}

// CheckPostForm answers whether the actor of st may post this form in st's
// conversation (section 5 notes 2 and 3, section 14, section 15). The shape
// of the form is the forms package's business; this is only who may use
// which action and setting where.
func CheckPostForm(st State, f model.FormInput) error {
	if !st.Actor.Active() {
		return model.ErrUnauthenticated
	}
	if !st.CanSee() {
		return model.ErrNotFound
	}
	if st.Conversation.Sealed() {
		// A sealed chat carries no forms at all (section 17, case 147).
		return model.BadRequest("A sealed chat carries no forms.")
	}
	action, known := formAction[f.Action.Type]
	if !known {
		return model.Invalid("form.action.type", "A form's action is tally, collect, thread, channel, group or bot.")
	}
	if err := Check(st, action); err != nil {
		return err
	}
	if st.Conversation.IsShared() && f.Action.Type != model.FormTally {
		// Ruling R12: the answers of a poll go sealed to its home
		// server; nothing else can work across servers.
		return forbidden("In a conversation shared with other servers only polls are allowed.")
	}
	if f.AnswererGroupID != "" {
		if st.Conversation.IsShared() {
			return forbidden("In a conversation shared with other servers a form cannot be limited to a group.")
		}
		// Ruling R33: only an account acting as manager may say who can
		// answer.
		if err := Check(st, SetAnswererGroup); err != nil {
			return err
		}
	}
	return nil
}

// SeesFormResults says whether a reader may see a poll's tally and voters:
// `live` shows them to all, `after_answer` to those with an answer,
// `after_close` to nobody until the form is closed. Once a form is closed
// its result is on the message for everyone.
func SeesFormResults(results string, closed, answered bool) bool {
	if closed {
		return true
	}
	switch results {
	case "", model.ResultsLive:
		return true
	case model.ResultsAfterAnswer:
		return answered
	}
	return false
}

// CheckReadFormAnswers answers whether the actor of st may list the answers
// to the form on message m (section 7). For a poll: anyone who can read the
// conversation, when its `results` setting lets them see the result (sees).
// For anything else: the form's author while they can read the conversation,
// and accounts acting as manager. Nobody learns who answered an anonymous
// form; that is not decided here but by never sending it.
func CheckReadFormAnswers(st State, m model.Message, action string, sees bool) error {
	if err := CheckMessage(st, ReadMessage, m); err != nil {
		return err
	}
	if action == model.FormTally {
		if sees {
			return nil
		}
		return forbidden("This poll's results are not shown yet.")
	}
	if m.AuthorID == st.Actor.ID {
		return nil
	}
	if err := Check(st, ReadFormAnswers); err != nil {
		return forbidden("Only the form's author and the channel's managers can read its answers.")
	}
	return nil
}

// CheckSchedule answers whether the actor of st may schedule an item of this
// kind into st's conversation (section 5; docs/FEATURES.md sections 9 to 11).
// A message or a reminder needs what posting needs; a recurring form needs
// an account acting as manager. Bots schedule nothing, and nothing can be
// scheduled into a sealed chat. The same check is made again when the item
// runs.
func CheckSchedule(st State, kind string) error {
	if !st.Actor.Active() {
		return model.ErrUnauthenticated
	}
	if !st.CanSee() {
		return model.ErrNotFound
	}
	if st.Actor.Role == model.RoleBot {
		return forbidden("Bots cannot schedule anything.")
	}
	if st.Conversation.Sealed() {
		return forbidden("Nothing can be scheduled into a sealed chat.")
	}
	if err := Check(st, Schedule); err != nil {
		return err
	}
	if kind == model.ScheduledForm {
		if st.Conversation.IsShared() {
			return forbidden("A recurring form cannot be set up in a conversation shared with other servers.")
		}
		if !st.ActsAsManager() {
			return forbidden("Only the channel's managers can set up a recurring form.")
		}
	}
	return nil
}

// MayManageScheduled says whether the actor of st may see, change and cancel
// a scheduled item in st's conversation: its owner always, and for a
// recurring form any account acting as manager of the channel. Anyone else
// is told it does not exist.
func MayManageScheduled(st State, item model.ScheduledItem) bool {
	if !st.Actor.Active() {
		return false
	}
	if item.AccountID == st.Actor.ID {
		return true
	}
	return item.Kind == model.ScheduledForm && item.ConversationID == st.Conversation.ID && st.CanSee() && st.ActsAsManager()
}
