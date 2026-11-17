package perm

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

func form(action string, change ...func(*model.FormInput)) model.FormInput {
	f := model.FormInput{Action: model.FormAction{Type: action}}
	for _, fn := range change {
		fn(&f)
	}
	return f
}

func limited(f *model.FormInput) { f.AnswererGroupID = "group" }

// The numbered cases are those of docs/PERMISSIONS.md section 17.
func TestFormRules(t *testing.T) {
	const badRequest = model.CodeInvalidRequest
	cases := []struct {
		name string
		st   State
		form model.FormInput
		want model.Code
	}{
		{"a member posts a poll", state(model.RoleMember, "PUB", model.LevelMember), form("tally"), ok},
		{"a member posts a collect form", state(model.RoleMember, "PRIV", model.LevelMember), form("collect"), ok},
		{"a reader posts a poll", state(model.RoleMember, "PUB", model.LevelReader), form("tally"), forbid},
		{"someone reading without having joined posts a poll", state(model.RoleMember, "PUB", ""), form("tally"), forbid},
		{"an outsider posts a poll in PRIV", state(model.RoleMember, "PRIV", ""), form("tally"), notFound},
		{"61: a member posts a channel form", state(model.RoleMember, "PUB", model.LevelMember), form("channel"), forbid},
		{"a manager posts a channel form", state(model.RoleMember, "PUB", model.LevelManager), form("channel"), ok},
		{"62: a manager who is not an admin posts a group form", state(model.RoleMember, "PUB", model.LevelManager), form("group"), forbid},
		{"an admin in the channel posts a group form", state(model.RoleAdmin, "PUB", model.LevelMember), form("group"), ok},
		{"an admin outside the channel posts a group form", state(model.RoleAdmin, "PUB", ""), form("group"), forbid},
		{"63: a person posts a bot form", state(model.RoleMember, "PUB", model.LevelManager), form("bot"), forbid},
		{"a bot posts a bot form", state(model.RoleBot, "PUB", model.LevelMember), form("bot"), ok},
		{"a bot posts a poll", state(model.RoleBot, "PUB", model.LevelMember), form("tally"), ok},
		{"a webhook posts a thread form", state(model.RoleBot, "PUB", model.LevelMember, webhook), form("thread"), ok},
		{"a webhook posts a bot form", state(model.RoleBot, "PUB", model.LevelMember, webhook), form("bot"), forbid},
		{"a webhook limits a form to a group", state(model.RoleBot, "PUB", model.LevelManager, webhook), form("tally", limited), forbid},
		{"147: a form in a sealed chat", state(model.RoleMember, "SEALED", model.LevelMember), form("tally"), badRequest},
		{"an unknown action", state(model.RoleMember, "PUB", model.LevelMember), form("magic"), model.CodeInvalidValue},
		{"a poll in an archived channel", state(model.RoleMember, "PUB", model.LevelMember, archived), form("tally"), isArch},
		{"a poll while timed out", state(model.RoleMember, "PUB", model.LevelMember, timedOut), form("tally"), isTimed},
		{"199: a poll in a shared channel", state(model.RoleMember, "PUB", model.LevelMember, shared), form("tally"), ok},
		{"199: an anonymous poll in a shared channel", state(model.RoleMember, "PUB", model.LevelMember, shared),
			form("tally", func(f *model.FormInput) { f.Anonymous = true }), ok},
		{"150: a collect form in a shared channel", state(model.RoleMember, "PUB", model.LevelMember, shared), form("collect"), forbid},
		{"150: a thread form in a shared channel", state(model.RoleMember, "PUB", model.LevelMember, shared), form("thread"), forbid},
		{"150: a channel form in a shared channel", state(model.RoleMember, "PUB", model.LevelManager, shared), form("channel"), forbid},
		{"150: a group form in a shared channel", state(model.RoleAdmin, "PUB", model.LevelManager, shared), form("group"), forbid},
		{"150: a bot form in a shared channel", state(model.RoleBot, "PUB", model.LevelMember, shared), form("bot"), forbid},
		{"202: a webhook's collect form in a shared channel", state(model.RoleBot, "PUB", model.LevelMember, webhook, shared), form("collect"), forbid},
		{"203: a thread form in a shared ad-hoc channel", state(model.RoleMember, "ADHOC", model.LevelMember, shared), form("thread"), forbid},
		{"209: a member limits a form to a group", state(model.RoleMember, "PUB", model.LevelMember), form("tally", limited), forbid},
		{"209: a manager limits a form to a group", state(model.RoleMember, "PUB", model.LevelManager), form("tally", limited), ok},
		{"an admin who is in a public channel limits a form to a group", state(model.RoleAdmin, "PUB", model.LevelMember), form("tally", limited), forbid},
		{"a manager limits a poll to a group in a shared channel", state(model.RoleMember, "PUB", model.LevelManager, shared), form("tally", limited), forbid},
	}
	for _, c := range cases {
		want(t, c.name, CheckPostForm(c.st, c.form), c.want)
	}
}

func TestAnsweringRules(t *testing.T) {
	for _, a := range []Action{AnswerForm, AddFormOption} {
		want(t, "52: a reader answers", Check(state(model.RoleMember, "PUB", model.LevelReader), a), ok)
		want(t, "a guest answers", Check(state(model.RoleGuest, "PRIV", model.LevelReader), a), ok)
		want(t, "82: a bot answers", Check(state(model.RoleBot, "PUB", model.LevelMember), a), forbid)
		want(t, "205: a member who has not joined answers", Check(state(model.RoleMember, "PUB", ""), a), forbid)
		want(t, "207: an admin who has not joined answers", Check(state(model.RoleAdmin, "PUB", ""), a), forbid)
		want(t, "an outsider answers in PRIV", Check(state(model.RoleMember, "PRIV", ""), a), notFound)
		want(t, "125: answering in an archived channel", Check(state(model.RoleMember, "PUB", model.LevelMember, archived), a), isArch)
		want(t, "answering while timed out", Check(state(model.RoleMember, "PUB", model.LevelMember, timedOut), a), isTimed)
		want(t, "answering in a sealed chat", Check(state(model.RoleMember, "SEALED", model.LevelMember), a), forbid)
	}
	want(t, "51: a reader ticks", Check(state(model.RoleMember, "PUB", model.LevelReader), Tick), forbid)
	want(t, "204: a member who has not joined ticks", Check(state(model.RoleMember, "PUB", ""), Tick), forbid)
	want(t, "206: a member who joined ticks", Check(state(model.RoleMember, "PUB", model.LevelMember), Tick), ok)
}

func TestSeesFormResults(t *testing.T) {
	cases := []struct {
		results          string
		closed, answered bool
		want             bool
	}{
		{"live", false, false, true},
		{"", false, false, true},
		{"after_answer", false, false, false},
		{"after_answer", false, true, true},
		{"after_answer", true, false, true},
		{"after_close", false, true, false},
		{"after_close", true, false, true},
		{"something else", false, true, false},
	}
	for _, c := range cases {
		if got := SeesFormResults(c.results, c.closed, c.answered); got != c.want {
			t.Errorf("results %q closed %v answered %v: got %v", c.results, c.closed, c.answered, got)
		}
	}
}

func TestReadFormAnswers(t *testing.T) {
	st := state(model.RoleMember, "PUB", model.LevelMember)
	own := model.Message{ConversationID: st.Conversation.ID, AuthorID: st.Actor.ID}
	others := model.Message{ConversationID: st.Conversation.ID, AuthorID: "someone-else"}
	want(t, "the author reads a collect form", CheckReadFormAnswers(st, own, model.FormCollect, false), ok)
	want(t, "69: a member reads someone else's collect form", CheckReadFormAnswers(st, others, model.FormCollect, false), forbid)
	want(t, "a manager reads a collect form",
		CheckReadFormAnswers(state(model.RoleMember, "PUB", model.LevelManager), others, model.FormCollect, false), ok)
	want(t, "an admin reads a collect form in a public channel",
		CheckReadFormAnswers(state(model.RoleAdmin, "PUB", ""), others, model.FormCollect, false), forbid)
	want(t, "an admin outside a private channel", CheckReadFormAnswers(state(model.RoleAdmin, "PRIV", ""), others, model.FormCollect, false), notFound)
	want(t, "a poll whose results the reader may see", CheckReadFormAnswers(st, others, model.FormTally, true), ok)
	want(t, "a poll whose results are not shown yet", CheckReadFormAnswers(st, others, model.FormTally, false), forbid)
	want(t, "the author of a poll whose results are not shown yet", CheckReadFormAnswers(st, own, model.FormTally, false), forbid)
	want(t, "72: a member closes another account's poll", CheckMessage(st, CloseOwnForm, others), forbid)
	want(t, "the author closes their poll after being lowered to reader",
		CheckMessage(state(model.RoleMember, "PUB", model.LevelReader), CloseOwnForm, own), ok)
}

func TestScheduleRules(t *testing.T) {
	cases := []struct {
		name string
		st   State
		kind string
		want model.Code
	}{
		{"a member schedules a message", state(model.RoleMember, "PUB", model.LevelMember), model.ScheduledMessage, ok},
		{"a guest sets a reminder", state(model.RoleGuest, "ADHOC", model.LevelMember), model.ScheduledReminder, ok},
		{"a reader schedules a message", state(model.RoleMember, "PUB", model.LevelReader), model.ScheduledMessage, forbid},
		{"someone who has not joined", state(model.RoleMember, "PUB", ""), model.ScheduledMessage, forbid},
		{"an outsider", state(model.RoleMember, "PRIV", ""), model.ScheduledMessage, notFound},
		{"a bot", state(model.RoleBot, "PUB", model.LevelMember), model.ScheduledMessage, forbid},
		{"into a sealed chat", state(model.RoleMember, "SEALED", model.LevelMember), model.ScheduledReminder, forbid},
		{"into an archived channel", state(model.RoleMember, "PUB", model.LevelMember, archived), model.ScheduledMessage, isArch},
		{"while timed out", state(model.RoleMember, "PUB", model.LevelMember, timedOut), model.ScheduledMessage, isTimed},
		{"a member sets up a recurring form", state(model.RoleMember, "PUB", model.LevelMember), model.ScheduledForm, forbid},
		{"a manager sets up a recurring form", state(model.RoleMember, "PUB", model.LevelManager), model.ScheduledForm, ok},
		{"an admin member of a public channel sets one up", state(model.RoleAdmin, "PUB", model.LevelMember), model.ScheduledForm, forbid},
		{"a recurring form in a shared channel", state(model.RoleMember, "PUB", model.LevelManager, shared), model.ScheduledForm, forbid},
	}
	for _, c := range cases {
		want(t, c.name, CheckSchedule(c.st, c.kind), c.want)
	}

	manager := state(model.RoleMember, "PUB", model.LevelManager)
	member := state(model.RoleMember, "PUB", model.LevelMember)
	recurring := model.ScheduledItem{AccountID: "someone-else", ConversationID: manager.Conversation.ID, Kind: model.ScheduledForm}
	later := model.ScheduledItem{AccountID: "someone-else", ConversationID: manager.Conversation.ID, Kind: model.ScheduledMessage}
	mine := model.ScheduledItem{AccountID: member.Actor.ID, ConversationID: member.Conversation.ID, Kind: model.ScheduledMessage}
	for _, c := range []struct {
		name string
		st   State
		item model.ScheduledItem
		want bool
	}{
		{"the owner", member, mine, true},
		{"a manager and someone's recurring form", manager, recurring, true},
		{"a member and someone's recurring form", member, recurring, false},
		{"a manager and someone's scheduled message", manager, later, false},
		{"a manager of another channel", state(model.RoleMember, "PRIV", model.LevelManager), recurring, false},
	} {
		if got := MayManageScheduled(c.st, c.item); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
