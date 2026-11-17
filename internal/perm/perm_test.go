package perm

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// The tests in this file are tables read off docs/PERMISSIONS.md. "case N"
// in a name is a numbered case of its section 17. Cases that need an API, a
// live connection or search results are covered in internal/api; the
// permission half of each is here where there is one.

const now = model.Time(2_000_000_000_000)

func account(role model.Role) model.Account {
	a := model.Account{ID: model.AccountID("acct-" + string(role)), Username: string(role), Role: role, CreatedAt: 1, ClaimedAt: 1}
	if role == model.RoleRemote {
		a.ServerID = "srv"
	}
	return a
}

// conv makes a conversation of one of the kinds the document names.
func conv(kind string) model.Conversation {
	c := model.Conversation{ID: model.ConversationID("conv-" + kind), Kind: model.KindChannel, DefaultLevel: model.LevelMember}
	switch kind {
	case "PUB":
		c.Name = "pub"
	case "PRIV":
		c.Name, c.Private = "priv", true
	case "ADHOC":
		c.Private, c.MemberKey = true, "a:abc"
	case "SEALED":
		c.Kind, c.Private, c.MemberKey = model.KindSealed, true, "s:a:b"
	case "LIBRARY":
		c.Private, c.MemberKey = true, model.LibraryMemberKey
	}
	return c
}

// state builds a State. level "" means not a member.
func state(role model.Role, kind string, level model.Level, change ...func(*State)) State {
	st := State{Actor: account(role), Conversation: conv(kind), Settings: DefaultSettings(), Now: now}
	if level != "" {
		st.Member = &model.Member{ConversationID: st.Conversation.ID, AccountID: st.Actor.ID, Level: level, DirectLevel: level}
	}
	if role == model.RoleBot {
		st.Bot = &store.BotInfo{AccountID: st.Actor.ID}
	}
	for _, fn := range change {
		fn(&st)
	}
	return st
}

func archived(st *State)     { st.Conversation.ArchivedAt = 5 }
func locked(st *State)       { st.Conversation.LockedAt = 5 }
func timedOut(st *State)     { st.TimedOutUntil = now + 1000 }
func shared(st *State)       { st.Conversation.StreamID = "stream" }
func viaGroup(st *State)     { st.Member.ViaGroup, st.Member.DirectLevel = true, "" }
func removed(st *State)      { st.Actor.RemovedAt = 5 }
func unclaimed(st *State)    { st.Actor.ClaimedAt = 0 }
func webhook(st *State)      { st.Bot.WebhookOnly = true }
func managersOnly(st *State) { st.Settings.BroadcastManagersOnly = true }
func hubModerator(st *State) { st.HubManager = true }

// A copy of a channel a hub owns; on its manager list; on its moderator
// list; private (section 14.1, ruling R94).
func hubOwned(st *State)     { st.Conversation.StreamID, st.HubOwned = "stream", true }
func onManagers(st *State)   { hubOwned(st); st.HubManager = true }
func onModerators(st *State) { hubOwned(st); st.HubModerator = true }
func hubPrivate(st *State)   { hubOwned(st); st.HubPrivate = true }

// members, broadLimit and broad set what ruling R64's rule reads: how many
// members the channel has, the server's limit and the channel's own answer.
func members(n int64) func(*State)    { return func(st *State) { st.Conversation.MemberCount = n } }
func broadLimit(n int64) func(*State) { return func(st *State) { st.Settings.BroadMentionLimit = n } }
func broad(v string) func(*State)     { return func(st *State) { st.Conversation.BroadMentions = v } }
func callsOff(st *State)              { st.Settings.CallsEnabled = false }

// What ruling R66's rules read: the channel is an announcement channel,
// lets members reply in threads, the member has been allowed to post, and
// the channel is shared with other servers.
func announce(st *State)      { st.Conversation.Announcement = true }
func replies(st *State)       { st.Conversation.AnnouncementReplies = true }
func mayPost(st *State)       { st.Member.MayPost = true }
func sharedChannel(st *State) { st.Conversation.StreamID = "0193a0c7-0000-7000-8000-00000000beef" }

func want(t *testing.T, name string, err error, code model.Code) {
	t.Helper()
	if got := model.CodeOf(err); got != code {
		if code == "" {
			t.Errorf("%s: refused with %s (%v), want allowed", name, got, err)
		} else {
			t.Errorf("%s: got %q, want %s", name, got, code)
		}
	}
}

const (
	ok       = model.Code("")
	notFound = model.CodeNotFound
	forbid   = model.CodeForbidden
	isArch   = model.CodeArchived
	isLocked = model.CodeLocked
	isTimed  = model.CodeTimedOut
	conflict = model.CodeConflict
	unauth   = model.CodeUnauthenticated
	featOff  = model.CodeFeatureOff
)

func TestConversationRules(t *testing.T) {
	const (
		owner, admin, member, guest, bot, remote = model.RoleOwner, model.RoleAdmin, model.RoleMember, model.RoleGuest, model.RoleBot, model.RoleRemote
		none                                     = model.Level("")
		reader, lmember, manager                 = model.LevelReader, model.LevelMember, model.LevelManager
		moderator                                = model.LevelModerator
	)
	cases := []struct {
		name string
		st   State
		a    Action
		want model.Code
	}{
		// Section 4.1: seeing and reading.
		{"case 1/2 member not in PRIV reads", state(member, "PRIV", none), Read, notFound},
		{"case 1/2 member not in PRIV sees", state(member, "PRIV", none), See, notFound},
		{"case 7 member not in PRIV catches up", state(member, "PRIV", none), Read, notFound},
		{"case 8 guest not in PRIV", state(guest, "PRIV", none), Read, notFound},
		{"case 9 bot not in PRIV", state(bot, "PRIV", none), Read, notFound},
		{"case 10 guest not in PUB", state(guest, "PUB", none), Read, notFound},
		{"bot not in PUB", state(bot, "PUB", none), Read, notFound},
		{"remote not in PUB", state(remote, "PUB", none), Read, unauth},
		{"case 14 member not in ADHOC", state(member, "ADHOC", none), Read, notFound},
		{"case 15 member not in PUB reads", state(member, "PUB", none), Read, ok},
		{"case 15 member not in PUB searches", state(member, "PUB", none), Search, ok},
		{"case 16 member not in PUB gets no live events", state(member, "PUB", none), ReceiveLive, forbid},
		{"case 17 member not in PUB reacts", state(member, "PUB", none), React, forbid},
		{"case 18 member not in PUB answers a poll", state(member, "PUB", none), AnswerForm, forbid},
		{"case 19 member not in PUB posts", state(member, "PUB", none), Post, forbid},
		{"case 23 admin not in PRIV reads", state(admin, "PRIV", none), Read, notFound},
		{"case 27 admin not in PRIV joins", state(admin, "PRIV", none), Join, notFound},
		{"case 28 admin not in PRIV adds", state(admin, "PRIV", none), AddAccount, notFound},
		{"case 32 admin not in PRIV sets the topic", state(admin, "PRIV", none), SetTopic, notFound},
		{"case 34 owner not in ADHOC", state(owner, "ADHOC", none), See, notFound},
		{"case 144 third account and a sealed chat", state(owner, "SEALED", none), See, notFound},
		{"the library does not exist", state(owner, "LIBRARY", none), See, notFound},
		{"member of PRIV reads", state(member, "PRIV", reader), Read, ok},
		{"guest in PUB reads", state(guest, "PUB", reader), Read, ok},
		{"sealed chat is not searched", state(member, "SEALED", lmember), Search, forbid},
		{"no reports in a sealed chat", state(member, "SEALED", lmember), Report, forbid},
		{"non-member who reads PUB reports", state(member, "PUB", none), Report, ok},
		{"non-member who reads PUB cannot mark read", state(member, "PUB", none), MarkRead, forbid},

		// The admin console (4.1 note 1).
		{"case 22 admin views PRIV in the console", state(admin, "PRIV", none), AdminView, ok},
		{"case 29 admin appoints a manager from outside", state(admin, "PRIV", none), ConsoleAppointManager, ok},
		{"case 29 owner appoints a manager from outside", state(owner, "PRIV", none), ConsoleAppointManager, ok},
		{"case 29 owner appoints a manager of a locked channel", state(owner, "PRIV", none, locked), ConsoleAppointManager, ok},
		{"case 29 owner appoints in a hub's channel", state(owner, "PUB", none, hubOwned), ConsoleAppointManager, conflict},
		{"case 31 owner appoints in an archived channel", state(owner, "PRIV", none, archived), ConsoleAppointManager, isArch},
		{"case 33 owner and ADHOC in the console", state(owner, "ADHOC", none), AdminView, notFound},
		{"case 35 owner and a sealed chat in the console", state(owner, "SEALED", none), AdminView, notFound},
		{"member in the console", state(member, "PRIV", manager), AdminView, forbid},
		{"case 172 owner override into PRIV", state(owner, "PRIV", none), OverrideAdd, ok},
		{"R104 admin dismisses a manager", state(admin, "PRIV", none), ConsoleDismissManager, ok},
		{"R104 owner dismisses a manager", state(owner, "PUB", none), ConsoleDismissManager, ok},
		{"R104 member dismisses from the console", state(member, "PUB", none), ConsoleDismissManager, forbid},
		{"R104 member overrides", state(member, "PRIV", none), OverrideAdd, forbid},
		{"R104 dismissal in a hub's channel", state(admin, "PUB", none, hubOwned), ConsoleDismissManager, conflict},
		{"R104 dismissal in an archived channel", state(owner, "PRIV", none, archived), ConsoleDismissManager, isArch},
		{"case 172 owner override into a locked PRIV", state(owner, "PRIV", none, locked), OverrideAdd, ok},
		{"case 172 admin override", state(admin, "PRIV", none), OverrideAdd, ok},
		{"case 172 owner override into PUB", state(owner, "PUB", none), OverrideAdd, conflict},
		{"case 172 owner override into a shared PRIV", state(owner, "PRIV", none, shared), OverrideAdd, forbid},
		{"case 172 owner override into a hub's channel", state(owner, "PRIV", none, hubPrivate), OverrideAdd, conflict},

		// Admins are not members of everything.
		{"case 39 admin not in PUB pins (ruling R99)", state(admin, "PUB", none), Pin, forbid},
		{"case 40 admin not in PUB posts", state(admin, "PUB", none), Post, forbid},
		{"case 207 admin not in PUB reacts", state(admin, "PUB", none), React, forbid},
		{"case 42 admin at member level in PRIV handles reports", state(admin, "PRIV", lmember), HandleReports, forbid},
		{"case 122 admin not in PUB handles its reports", state(admin, "PUB", none), HandleReports, ok},
		{"manager of ADHOC handles reports", state(member, "ADHOC", manager), HandleReports, ok},
		{"case 121 owner in ADHOC at member level", state(owner, "ADHOC", lmember), HandleReports, forbid},
		{"admin who is a member of PRIV is no manager", state(admin, "PRIV", reader), SetTopic, forbid},

		// Section 5: levels.
		{"case 48 reader posts", state(member, "PUB", reader), Post, forbid},
		{"case 49 reader replies", state(member, "PUB", reader), Reply, forbid},
		{"case 50 reader uploads", state(member, "PUB", reader), Upload, forbid},
		{"case 51 reader ticks", state(member, "PUB", reader), Tick, forbid},
		{"case 52 reader reacts", state(member, "PUB", reader), React, ok},
		{"case 52 reader answers a poll", state(member, "PUB", reader), AnswerForm, ok},
		{"reader sends a command", state(member, "PUB", reader), SendCommand, ok},
		{"case 53 member pins", state(member, "PUB", lmember), Pin, forbid},
		{"case 54 member sets the topic", state(member, "PUB", lmember), SetTopic, forbid},
		{"case 55 member adds to PRIV", state(member, "PRIV", lmember), AddAccount, forbid},
		{"case 56 member adds to ADHOC", state(member, "ADHOC", lmember), AddAccount, ok},
		{"case 57 member removes from ADHOC", state(member, "ADHOC", lmember), RemoveMember, forbid},
		{"case 58 member deletes another's message", state(member, "PUB", lmember), DeleteOthersMessage, forbid},
		{"case 60 member sets slow mode", state(member, "PUB", lmember), SetSlowMode, forbid},
		{"case 60 member times out", state(member, "PUB", lmember), TimeOutMember, forbid},
		{"case 60 member archives", state(member, "PUB", lmember), Archive, forbid},
		{"case 61 member posts a channel form", state(member, "PUB", lmember), PostChannelForm, forbid},
		{"manager posts a channel form", state(member, "PUB", manager), PostChannelForm, ok},
		{"case 62 manager posts a group form", state(member, "PUB", manager), PostGroupForm, forbid},
		{"admin member posts a group form", state(admin, "PUB", lmember), PostGroupForm, ok},
		{"case 63 a person posts cards", state(member, "PUB", manager), PostCards, forbid},
		{"case 63 a person posts a bot form", state(member, "PUB", manager), PostBotForm, forbid},
		{"a bot posts cards", state(bot, "PUB", lmember), PostCards, ok},
		{"case 64 manager sets a longer retention", state(member, "PUB", manager), SetRetention, forbid},
		{"case 64 manager shortens retention", state(member, "PUB", manager), ShortenRetention, ok},
		{"case 65 manager of ADHOC and retention", state(member, "ADHOC", manager), ShortenRetention, forbid},
		{"case 69 member reads collect answers", state(member, "PUB", lmember), ReadFormAnswers, forbid},
		{"case 209 member sets an answerer group", state(member, "PUB", lmember), SetAnswererGroup, forbid},
		{"case 209 manager sets an answerer group", state(member, "PUB", manager), SetAnswererGroup, ok},
		{"broadcast by a member", state(member, "PUB", lmember), Broadcast, ok},
		{"broadcast limited to managers", state(member, "PUB", lmember, managersOnly), Broadcast, forbid},
		{"broadcast by a manager when limited", state(member, "PUB", manager, managersOnly), Broadcast, ok},
		// Ruling R64: above the server's limit only the channel's managers
		// and the admins; the channel may say otherwise.
		{"R64 member at the limit", state(member, "PUB", lmember, members(250)), Broadcast, ok},
		{"R64 member above the limit", state(member, "PUB", lmember, members(251)), Broadcast, forbid},
		{"R64 guest below the limit", state(guest, "PUB", lmember, members(10)), Broadcast, ok},
		{"R64 guest above the limit", state(guest, "PUB", lmember, members(251)), Broadcast, forbid},
		{"R64 bot below the limit", state(bot, "PUB", lmember, members(10)), Broadcast, ok},
		{"R64 bot above the limit", state(bot, "PUB", lmember, members(251)), Broadcast, forbid},
		{"R64 bot manager above the limit", state(bot, "PUB", manager, members(251)), Broadcast, ok},
		{"R64 manager above the limit", state(member, "PUB", manager, members(5000)), Broadcast, ok},
		{"R64 guest manager above the limit", state(guest, "PRIV", manager, members(5000)), Broadcast, ok},
		{"R64 admin member above the limit", state(admin, "PUB", lmember, members(5000)), Broadcast, forbid},
		{"R64 owner member of PRIV above the limit", state(owner, "PRIV", lmember, members(5000)), Broadcast, forbid},
		{"R64 admin reader above the limit", state(admin, "PUB", reader, members(5000)), Broadcast, forbid},
		{"R64 reader below the limit", state(member, "PUB", reader, members(3)), Broadcast, forbid},
		{"R64 a limit of 0 is no limit", state(member, "PUB", lmember, members(5000), broadLimit(0)), Broadcast, ok},
		{"R64 a lower limit", state(member, "PUB", lmember, members(11), broadLimit(10)), Broadcast, forbid},
		{"R64 channel says anyone, above the limit", state(member, "PUB", lmember, members(5000), broad(model.BroadAnyone)), Broadcast, ok},
		{"R64 channel says anyone, guest above the limit", state(guest, "PUB", lmember, members(5000), broad(model.BroadAnyone)), Broadcast, ok},
		{"R64 channel says managers, below the limit", state(member, "PUB", lmember, members(3), broad(model.BroadManagers)), Broadcast, forbid},
		{"R64 channel says managers, a manager", state(member, "PUB", manager, members(3), broad(model.BroadManagers)), Broadcast, ok},
		{"R64 channel says managers, an admin", state(admin, "PUB", lmember, members(3), broad(model.BroadManagers)), Broadcast, forbid},
		{"R64 channel says anyone, server says managers", state(member, "PUB", lmember, broad(model.BroadAnyone), managersOnly), Broadcast, forbid},
		{"R64 member sets the channel's answer", state(member, "PUB", lmember), SetBroadMentions, forbid},
		{"R64 manager sets the channel's answer", state(member, "PUB", manager), SetBroadMentions, ok},
		{"R64 admin sets the answer of a public channel", state(admin, "PUB", none), SetBroadMentions, forbid},
		{"calls switched off", state(member, "PUB", lmember, callsOff), StartCall, featOff},

		// Ruling R66, section 5.1: an announcement channel. Its managers
		// and those they allow post; everyone else reads and reacts.
		{"R66 member posts in an announcement channel", state(member, "PUB", lmember, announce), Post, forbid},
		{"R66 member replies", state(member, "PUB", lmember, announce), Reply, forbid},
		{"R66 member uploads", state(member, "PUB", lmember, announce), Upload, forbid},
		{"R66 member posts a form", state(member, "PUB", lmember, announce), PostForm, forbid},
		{"R66 member schedules a message", state(member, "PUB", lmember, announce), Schedule, forbid},
		{"R66 member starts a call", state(member, "PUB", lmember, announce), StartCall, forbid},
		{"R66 member reacts", state(member, "PUB", lmember, announce), React, ok},
		{"R66 member reads", state(member, "PUB", lmember, announce), Read, ok},
		{"R66 member answers a poll", state(member, "PUB", lmember, announce), AnswerForm, ok},
		{"R66 member lists the members", state(member, "PUB", lmember, announce), ListMembers, forbid},
		{"R66 member who is not in it lists the members", state(member, "PUB", none, announce), ListMembers, forbid},
		{"R66 reader posts", state(member, "PUB", reader, announce), Post, forbid},
		{"R66 reader reacts", state(member, "PUB", reader, announce), React, ok},
		{"R66 member allowed to post", state(member, "PUB", lmember, announce, mayPost), Post, ok},
		{"R66 member allowed to post replies", state(member, "PUB", lmember, announce, mayPost), Reply, ok},
		{"R66 member allowed to post uploads", state(member, "PUB", lmember, announce, mayPost), Upload, ok},
		{"R66 member allowed to post lists the members", state(member, "PUB", lmember, announce, mayPost), ListMembers, forbid},
		{"R66 reader allowed to post is still a reader", state(member, "PUB", reader, announce, mayPost), Post, forbid},
		{"R66 manager posts", state(member, "PUB", manager, announce), Post, ok},
		{"R66 manager lists the members", state(member, "PUB", manager, announce), ListMembers, ok},
		{"R66 manager mentions everyone", state(member, "PUB", manager, announce), Broadcast, ok},
		{"R66 admin who is a member posts", state(admin, "PUB", lmember, announce), Post, forbid},
		{"R66 admin outside a public one lists the members", state(admin, "PUB", none, announce), ListMembers, forbid},
		{"R66 admin member of a private one posts", state(admin, "PRIV", lmember, announce), Post, forbid},
		{"R66 guest posts", state(guest, "PUB", lmember, announce), Post, forbid},
		{"R66 guest reacts", state(guest, "PUB", lmember, announce), React, ok},
		{"R66 guest lists the members", state(guest, "PUB", lmember, announce), ListMembers, forbid},
		{"R66 guest allowed to post", state(guest, "PUB", lmember, announce, mayPost), Post, ok},
		{"R66 guest manager posts", state(guest, "PRIV", manager, announce), Post, ok},
		{"R66 bot posts", state(bot, "PUB", lmember, announce), Post, forbid},
		{"R66 bot posts cards", state(bot, "PUB", lmember, announce), PostCards, forbid},
		{"R66 bot reacts", state(bot, "PUB", lmember, announce), React, ok},
		{"R66 bot allowed to post", state(bot, "PUB", lmember, announce, mayPost), Post, ok},
		{"R66 bot manager posts", state(bot, "PUB", manager, announce), Post, ok},
		{"R66 webhook the managers approved posts", state(bot, "PUB", lmember, announce, webhook), Post, ok},
		{"R66 replies allowed: member replies", state(member, "PUB", lmember, announce, replies), Reply, ok},
		{"R66 replies allowed: member uploads", state(member, "PUB", lmember, announce, replies), Upload, ok},
		{"R66 replies allowed: member still cannot post", state(member, "PUB", lmember, announce, replies), Post, forbid},
		{"R66 replies allowed: guest replies", state(guest, "PUB", lmember, announce, replies), Reply, ok},
		{"R66 replies allowed: bot replies", state(bot, "PUB", lmember, announce, replies), Reply, ok},
		{"R66 replies allowed: reader does not reply", state(member, "PUB", reader, announce, replies), Reply, forbid},
		{"R66 archived announcement channel", state(member, "PUB", manager, announce, archived), Post, isArch},
		{"R66 member makes it an announcement channel", state(member, "PUB", lmember), SetAnnouncement, forbid},
		{"R66 manager makes it one", state(member, "PUB", manager), SetAnnouncement, ok},
		{"R66 admin makes a public channel one", state(admin, "PUB", none), SetAnnouncement, forbid},
		{"R66 guest manager makes it one", state(guest, "PRIV", manager), SetAnnouncement, ok},
		{"R66 a shared channel cannot be one", state(member, "PUB", manager, sharedChannel), SetAnnouncement, conflict},
		{"R66 an announcement channel cannot be shared", state(owner, "PUB", manager, announce), Share, conflict},
		{"R66 manager allows someone to post", state(member, "PUB", manager, announce), SetMayPost, ok},
		{"R66 member allows someone to post", state(member, "PUB", lmember, announce), SetMayPost, forbid},
		{"R66 member sets lazy delivery", state(member, "PUB", lmember), SetLazyDelivery, forbid},
		{"R66 manager sets lazy delivery", state(member, "PUB", manager), SetLazyDelivery, ok},
		{"R66 admin sets lazy delivery of a public channel", state(admin, "PUB", none), SetLazyDelivery, forbid},

		// Guests, bots, webhooks.
		{"case 74 guest joins PUB", state(guest, "PUB", none), Join, notFound},
		{"case 81 bot joins PUB", state(bot, "PUB", none), Join, notFound},
		{"member joins PUB", state(member, "PUB", none), Join, ok},
		{"case 82 bot answers a form", state(bot, "PUB", lmember), AnswerForm, forbid},
		{"case 83/198 bot pushes through do not disturb", state(bot, "PUB", lmember), NotifyAnyway, forbid},
		{"case 85 bot at reader level posts", state(bot, "PUB", reader), Post, forbid},
		{"a bot at member level posts", state(bot, "PUB", lmember), Post, ok},
		{"a webhook posts", state(bot, "PUB", lmember, webhook), Post, ok},
		{"a webhook reads nothing", state(bot, "PUB", lmember, webhook), Read, forbid},
		{"a webhook reacts to nothing", state(bot, "PUB", lmember, webhook), React, forbid},
		{"case 92 member asks for a webhook in ADHOC", state(member, "ADHOC", lmember), RequestWebhook, forbid},
		{"case 93 guest asks for a webhook in PUB", state(guest, "PUB", lmember), RequestWebhook, forbid},
		{"member asks for a webhook in PUB", state(member, "PUB", lmember), RequestWebhook, ok},
		{"case 109 manager of ADHOC adds a group", state(member, "ADHOC", manager), AddGroup, forbid},
		{"a guest manager cannot add a group", state(guest, "PUB", manager), AddGroup, forbid},
		{"a manager adds a group", state(member, "PUB", manager), AddGroup, ok},

		// Section 6: leaving.
		{"case 101 leave while in through a group", state(member, "PUB", lmember, viaGroup), Leave, conflict},
		{"leave", state(member, "PUB", lmember), Leave, ok},
		{"a sealed chat cannot be left", state(member, "SEALED", lmember), Leave, conflict},
		{"case 145 add to a sealed chat", state(member, "SEALED", lmember), AddAccount, forbid},

		// Section 10: archived.
		{"case 124 manager posts in an archived channel", state(member, "PUB", manager, archived), Post, isArch},
		{"case 125 react", state(member, "PUB", lmember, archived), React, isArch},
		{"case 125 tick", state(member, "PUB", lmember, archived), Tick, isArch},
		{"case 125 answer", state(member, "PUB", lmember, archived), AnswerForm, isArch},
		{"case 127 read an archived channel", state(member, "PUB", lmember, archived), Read, ok},
		{"case 127 search an archived channel", state(member, "PUB", lmember, archived), Search, ok},
		{"archived: mark read", state(member, "PUB", lmember, archived), MarkRead, ok},
		{"archived: leave", state(member, "PUB", lmember, archived), Leave, ok},
		{"archived: join", state(member, "PUB", none, archived), Join, isArch},
		{"archived: pin", state(member, "PUB", manager, archived), Pin, isArch},
		{"archived: manager deletes a message", state(member, "PUB", manager, archived), DeleteOthersMessage, ok},
		{"archived: unarchive", state(member, "PUB", manager, archived), Unarchive, ok},
		{"case 168 archived PUB made private", state(member, "PUB", manager, archived), MakePrivate, isArch},
		{"a reader posting in an archived channel is forbidden first", state(member, "PUB", reader, archived), Post, forbid},

		// Section 11: timed out.
		{"case 129 post", state(member, "PUB", lmember, timedOut), Post, isTimed},
		{"case 129 reply", state(member, "PUB", lmember, timedOut), Reply, isTimed},
		{"case 129 react", state(member, "PUB", lmember, timedOut), React, isTimed},
		{"case 129 upload", state(member, "PUB", lmember, timedOut), Upload, isTimed},
		{"case 129 tick", state(member, "PUB", lmember, timedOut), Tick, isTimed},
		{"case 129 answer", state(member, "PUB", lmember, timedOut), AnswerForm, isTimed},
		{"case 130 read while timed out", state(member, "PUB", lmember, timedOut), Read, ok},
		{"case 133 timed-out manager pins", state(member, "PUB", manager, timedOut), Pin, isTimed},
		{"timed out: mark read", state(member, "PUB", lmember, timedOut), MarkRead, ok},
		{"timed out: leave", state(member, "PUB", lmember, timedOut), Leave, ok},
		{"timed out: report", state(member, "PUB", lmember, timedOut), Report, ok},
		{"timed out: join", state(member, "PUB", none, timedOut), Join, ok},
		{"archived comes before timed out", state(member, "PUB", lmember, archived, timedOut), Post, isArch},

		// Section 12: removed and unclaimed accounts can do nothing.
		{"case 134 removed account", state(member, "PUB", lmember, removed), Read, unauth},
		{"unclaimed account", state(member, "PUB", lmember, unclaimed), Read, unauth},

		// Section 7.1: public to private.
		{"case 157 manager makes PUB private", state(member, "PUB", manager), MakePrivate, ok},
		{"case 158 admin outside makes PUB private", state(admin, "PUB", none), MakePrivate, forbid},
		{"case 165 manager makes PRIV public", state(member, "PRIV", manager), MakePrivate, forbid},
		{"case 165 owner makes PRIV public", state(owner, "PRIV", manager), MakePrivate, forbid},
		{"case 166 ADHOC made public", state(member, "ADHOC", manager), MakePrivate, forbid},
		{"case 167 member makes PUB private", state(member, "PUB", lmember), MakePrivate, forbid},

		// Section 10.1: locking.
		{"case 169 manager locks PRIV", state(member, "PRIV", manager), Lock, ok},
		{"case 170 manager locks ADHOC", state(member, "ADHOC", manager), Lock, ok},
		{"case 171 member locks PRIV", state(member, "PRIV", lmember), Lock, forbid},
		{"case 172 admin outside PRIV, ordinary operation", state(admin, "PRIV", none), Lock, notFound},
		{"admin who is a member locks PRIV", state(admin, "PRIV", lmember), Lock, forbid},
		{"case 173 manager locks PUB", state(member, "PUB", manager), Lock, conflict},
		{"case 174 manager adds to a locked PRIV", state(member, "PRIV", manager, locked), AddAccount, isLocked},
		{"case 175 member adds to a locked ADHOC", state(member, "ADHOC", lmember, locked), AddAccount, isLocked},
		{"case 176 bot added to a locked PRIV", state(member, "PRIV", manager, locked), AddBot, isLocked},
		{"case 176 group added to a locked PRIV", state(member, "PRIV", manager, locked), AddGroup, isLocked},
		{"case 177 admin member adds to a locked PRIV", state(admin, "PRIV", lmember, locked), AddAccount, forbid},
		{"case 179 webhook asked for in a locked PRIV", state(member, "PRIV", lmember, locked), RequestWebhook, isLocked},
		{"case 188 post in a locked PRIV", state(member, "PRIV", lmember, locked), Post, ok},
		{"case 188 react in a locked PRIV", state(member, "PRIV", lmember, locked), React, ok},
		{"case 188 pin in a locked PRIV", state(member, "PRIV", manager, locked), Pin, ok},
		{"locked: leave", state(member, "PRIV", lmember, locked), Leave, ok},
		{"locked: remove a member", state(member, "PRIV", manager, locked), RemoveMember, ok},
		{"locked: set levels", state(member, "PRIV", manager, locked), SetLevel, ok},
		{"case 189 lock a shared private channel", state(member, "PRIV", manager, shared), Lock, forbid},
		{"case 189 owner shares a locked PRIV", state(owner, "PRIV", manager, locked), Share, forbid},
		{"owner shares a channel", state(owner, "PRIV", manager), Share, ok},
		{"admin cannot share", state(admin, "PUB", manager), Share, forbid},

		// Section 14: shared conversations.
		{"case 149 manager moves a thread in a shared channel", state(member, "PUB", manager, shared), MoveAnyThread, forbid},
		{"own thread moved by a member", state(member, "PUB", lmember), MoveOwnThread, ok},

		// Section 14.1: a channel a hub owns. Its managers here are those
		// on the hub's manager list; its moderators keep the peace.
		{"a manager level not on the hub's list renames nothing", state(member, "PUB", manager, hubOwned), Rename, forbid},
		{"on the manager list at level member", state(member, "PUB", lmember, onManagers), Rename, ok},
		{"an admin is no manager of a copy", state(admin, "PUB", lmember, hubOwned), Rename, forbid},
		{"a moderator times someone out here", state(member, "PUB", lmember, onModerators), TimeOutMember, ok},
		{"a moderator sets slow mode here", state(member, "PUB", lmember, onModerators), SetSlowMode, ok},
		{"a moderator does not rename", state(member, "PUB", lmember, onModerators), Rename, forbid},
		{"a moderator outside a private copy times nobody out", state(member, "PRIV", none, onModerators), TimeOutMember, notFound},
		// Ruling R94: nobody adds people to a private channel a hub owns
		// but through its moderators and managers.
		{"a manager adds nobody to a private hub channel", state(member, "PRIV", manager, onManagers, hubPrivate), AddAccount, forbid},
		{"the owner adds nobody to a private hub channel", state(owner, "PRIV", lmember, hubPrivate), AddAccount, forbid},
		{"nor a bot", state(admin, "PRIV", lmember, hubPrivate), AddBot, forbid},
		{"nor a group", state(admin, "PRIV", lmember, hubPrivate), AddGroup, forbid},
		{"nobody joins a private hub channel", state(owner, "PRIV", lmember, hubPrivate), Join, forbid},
		{"a guest cannot create the channel a moved thread needs", state(guest, "PUB", lmember), MoveOwnThread, forbid},

		// Ruling R99: a moderator keeps the peace, a manager holds the
		// channel; an admin or owner is neither unless made one.
		{"case 300 moderator answers a knock", state(member, "PRIV", moderator), AnswerKnock, ok},
		{"case 300 moderator lists the bans and the knocks", state(member, "PRIV", moderator), ListBans, ok},
		{"case 300 member answers a knock", state(member, "PRIV", lmember), AnswerKnock, forbid},
		{"case 301 moderator adds to PRIV", state(member, "PRIV", moderator), AddAccount, ok},
		{"case 301 moderator adds to PUB", state(member, "PUB", moderator), AddAccount, ok},
		{"case 301 moderator adds a bot", state(member, "PRIV", moderator), AddBot, forbid},
		{"case 301 manager adds a bot", state(member, "PRIV", manager), AddBot, ok},
		{"case 301 moderator adds a group", state(member, "PRIV", moderator), AddGroup, forbid},
		{"case 302 moderator removes", state(member, "PUB", moderator), RemoveMember, ok},
		{"case 302 moderator bans", state(member, "PUB", moderator), BanMember, ok},
		{"case 303 moderator deletes another's message", state(member, "PUB", moderator), DeleteOthersMessage, ok},
		{"case 303 moderator removes a preview", state(member, "PUB", moderator), RemoveAnyPreview, ok},
		{"case 304 moderator times out", state(member, "PUB", moderator), TimeOutMember, ok},
		{"case 304 moderator sets slow mode", state(member, "PUB", moderator), SetSlowMode, ok},
		{"case 305 moderator locks PRIV", state(member, "PRIV", moderator), Lock, ok},
		{"case 306 moderator handles reports in PRIV", state(member, "PRIV", moderator), HandleReports, ok},
		{"case 306 moderator handles reports in ADHOC", state(member, "ADHOC", moderator), HandleReports, ok},
		{"case 307 moderator pins", state(member, "PUB", moderator), Pin, forbid},
		{"case 307 moderator sets the topic", state(member, "PUB", moderator), SetTopic, forbid},
		{"case 307 moderator renames", state(member, "PUB", moderator), Rename, forbid},
		{"case 307 moderator sets a level", state(member, "PUB", moderator), SetLevel, forbid},
		{"case 307 moderator archives", state(member, "PUB", moderator), Archive, forbid},
		{"case 307 moderator makes it private", state(member, "PUB", moderator), MakePrivate, forbid},
		{"case 307 moderator sets reference patterns", state(member, "PUB", moderator), SetReferencePatterns, forbid},
		{"case 307 moderator sets the section", state(member, "PUB", moderator), SetSection, forbid},
		{"case 307 moderator shortens retention", state(member, "PUB", moderator), ShortenRetention, forbid},
		{"case 307 moderator moves another's thread", state(member, "PUB", moderator), MoveAnyThread, forbid},
		{"case 307 moderator mentions everyone in a large channel", state(member, "PUB", moderator, members(5000)), Broadcast, forbid},
		{"case 308 moderator posts in an announcement channel", state(member, "PUB", moderator, announce), Post, forbid},
		{"case 308 moderator lists an announcement channel's members", state(member, "PUB", moderator, announce), ListMembers, ok},
		{"case 309 moderator posts", state(member, "PUB", moderator), Post, ok},
		{"case 309 moderator timed out keeps no peace", state(member, "PUB", moderator, timedOut), TimeOutMember, isTimed},
		{"case 310 admin not in PUB removes", state(admin, "PUB", none), RemoveMember, forbid},
		{"case 310 admin member of PUB deletes another's", state(admin, "PUB", lmember), DeleteOthersMessage, forbid},
		{"case 310 owner member of PRIV answers a knock", state(owner, "PRIV", lmember), AnswerKnock, forbid},
		{"case 310 owner member of PRIV renames", state(owner, "PRIV", lmember), Rename, forbid},
		{"case 310 admin who is a moderator times out", state(admin, "PUB", moderator), TimeOutMember, ok},
		{"case 311 admin handles reports of PUB", state(admin, "PUB", none), HandleReports, ok},
		{"case 311 admin member of PRIV handles reports", state(owner, "PRIV", lmember), HandleReports, forbid},
	}
	for _, c := range cases {
		want(t, c.name, Check(c.st, c.a), c.want)
	}
}

func TestActsAsManager(t *testing.T) {
	for _, c := range []struct {
		name string
		st   State
		want bool
	}{
		{"manager", state(model.RoleMember, "PRIV", model.LevelManager), true},
		{"member", state(model.RoleMember, "PRIV", model.LevelMember), false},
		{"moderator", state(model.RoleMember, "PRIV", model.LevelModerator), false},
		{"admin outside a public channel (ruling R99)", state(model.RoleAdmin, "PUB", ""), false},
		{"admin outside a private channel", state(model.RoleAdmin, "PRIV", ""), false},
		{"admin inside a private channel", state(model.RoleAdmin, "PRIV", model.LevelMember), false},
		{"owner inside a public channel", state(model.RoleOwner, "PUB", model.LevelMember), false},
		{"admin who is a manager", state(model.RoleAdmin, "PUB", model.LevelManager), true},
		{"owner outside an ad-hoc channel", state(model.RoleOwner, "ADHOC", ""), false},
		{"nobody in a sealed chat", state(model.RoleOwner, "SEALED", model.LevelMember), false},
	} {
		if got := c.st.ActsAsManager(); got != c.want {
			t.Errorf("%s: acts as manager = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestKeepsPeace(t *testing.T) {
	for _, c := range []struct {
		name string
		st   State
		want bool
	}{
		{"manager", state(model.RoleMember, "PRIV", model.LevelManager), true},
		{"moderator", state(model.RoleMember, "PRIV", model.LevelModerator), true},
		{"member", state(model.RoleMember, "PRIV", model.LevelMember), false},
		{"admin outside", state(model.RoleAdmin, "PUB", ""), false},
		{"owner member", state(model.RoleOwner, "PUB", model.LevelMember), false},
		{"on a hub's moderator list, public copy", state(model.RoleMember, "PUB", "", onModerators), true},
		{"on a hub's moderator list, private copy, not in it", state(model.RoleMember, "PRIV", "", onModerators), false},
		{"local moderator level in a hub's copy, not on its list", state(model.RoleMember, "PUB", model.LevelModerator, hubOwned), false},
		{"nobody in a sealed chat", state(model.RoleMember, "SEALED", model.LevelModerator), false},
	} {
		if got := c.st.KeepsPeace(); got != c.want {
			t.Errorf("%s: keeps the peace = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMessageRules(t *testing.T) {
	msg := func(st State, own bool, change ...func(*model.Message)) model.Message {
		m := model.Message{ID: "m", ConversationID: st.Conversation.ID, AuthorID: "someone-else", CreatedAt: now - 60_000}
		if own {
			m.AuthorID = st.Actor.ID
		}
		for _, fn := range change {
			fn(&m)
		}
		return m
	}
	hidden := func(m *model.Message) { m.HistoryHiddenAt = 5 }
	pinned := func(m *model.Message) { m.Pinned = true }
	newcomer := func(st *State) { st.Member.VisibleFrom = now }
	window := func(st *State) { st.Settings.EditWindowSeconds = 30 }
	const member, admin, owner, bot = model.RoleMember, model.RoleAdmin, model.RoleOwner, model.RoleBot
	const lm, mgr, rd = model.LevelMember, model.LevelManager, model.LevelReader
	type tc struct {
		name string
		st   State
		a    MessageAction
		own  bool
		mod  []func(*model.Message)
		want model.Code
	}
	cases := []tc{
		{"case 20 a message in a channel the author left", state(member, "PRIV", ""), ReadMessage, true, nil, notFound},
		// Section 4.4: a pin opens an older message on its own and
		// nothing else of it (review finding H4).
		{"a newcomer reads an older message", state(member, "PRIV", lm, newcomer), ReadMessage, false, nil, notFound},
		{"a newcomer opens an older pinned message", state(member, "PRIV", lm, newcomer), OpenMessage, false, []func(*model.Message){pinned}, ok},
		{"a pin opens nothing more of it", state(member, "PRIV", lm, newcomer), ReadMessage, false, []func(*model.Message){pinned}, notFound},
		{"nor reacting to it", state(member, "PRIV", lm, newcomer), ReactTo, false, []func(*model.Message){pinned}, notFound},
		{"an older unpinned message does not open", state(member, "PRIV", lm, newcomer), OpenMessage, false, nil, notFound},
		{"edit own", state(member, "PUB", lm), EditMessage, true, nil, ok},
		{"case 41 admin edits another's message", state(admin, "PUB", mgr), EditMessage, false, nil, forbid},
		{"owner edits another's message", state(owner, "PUB", mgr), EditMessage, false, nil, forbid},
		{"case 71 edit after the window closed", state(member, "PUB", lm, window), EditMessage, true, nil, forbid},
		{"a bot is not held to the edit window", state(bot, "PUB", lm, window), EditMessage, true, nil, ok},
		{"edit own after being lowered to reader (decision P8)", state(member, "PUB", rd), EditMessage, true, nil, forbid},
		{"case 125 edit in an archived channel", state(member, "PUB", lm, archived), EditMessage, true, nil, isArch},
		{"case 129 edit while timed out", state(member, "PUB", lm, timedOut), EditMessage, true, nil, isTimed},
		{"case 128 delete own in an archived channel", state(member, "PUB", lm, archived), DeleteMessage, true, nil, ok},
		{"delete own while timed out", state(member, "PUB", lm, timedOut), DeleteMessage, true, nil, ok},
		{"delete own as a reader", state(member, "PUB", rd), DeleteMessage, true, nil, ok},
		{"delete own after leaving a public channel one can still read", state(member, "PUB", ""), DeleteMessage, true, nil, ok},
		{"case 58 member deletes another's", state(member, "PUB", lm), DeleteMessage, false, nil, forbid},
		{"manager deletes another's", state(member, "PUB", mgr), DeleteMessage, false, nil, ok},
		{"admin outside PUB deletes another's", state(admin, "PUB", ""), DeleteMessage, false, nil, forbid},
		{"case 303 moderator deletes another's", state(member, "PUB", model.LevelModerator), DeleteMessage, false, nil, ok},
		{"only the author deletes in a sealed chat", state(member, "SEALED", lm), DeleteMessage, false, nil, forbid},
		// Section 14.1: a moderator of a channel a hub owns. Authority is
		// the hub-signed list, not the level here.
		{"case 250 a moderator at level member deletes another's", state(member, "PUB", lm, shared, hubModerator), DeleteMessage, false, nil, ok},
		{"a moderator at level reader deletes another's", state(member, "PUB", rd, shared, hubModerator), DeleteMessage, false, nil, ok},
		{"a moderator who has not joined a public copy deletes another's", state(member, "PUB", "", shared, hubModerator), DeleteMessage, false, nil, ok},
		{"case 251 the same account when it is not a moderator", state(member, "PUB", lm, shared), DeleteMessage, false, nil, forbid},
		{"case 252 a moderator who is timed out", state(member, "PUB", lm, shared, hubModerator, timedOut), DeleteMessage, false, nil, isTimed},
		{"case 253 a moderator who cannot see the conversation", state(member, "PRIV", "", shared, hubModerator), DeleteMessage, false, nil, notFound},
		{"case 254 a bot on the moderator list", state(bot, "PUB", lm, shared, hubModerator), DeleteMessage, false, nil, forbid},
		{"case 255 a moderator mark means nothing in a sealed chat", state(member, "SEALED", lm, hubModerator), DeleteMessage, false, nil, forbid},
		{"a removed account that was a moderator", state(member, "PUB", lm, shared, hubModerator, removed), DeleteMessage, false, nil, model.CodeUnauthenticated},
		{"on the moderator list, not the manager list", state(member, "PUB", lm, onModerators), DeleteMessage, false, nil, ok},
		{"case 256 a manager who is not a moderator deletes another's", state(member, "PUB", mgr, shared), DeleteMessage, false, nil, ok},
		{"a moderator's own message", state(member, "PUB", lm, shared, hubModerator, timedOut), DeleteMessage, true, nil, ok},
		{"history open to a reader", state(member, "PUB", ""), OpenHistory, false, nil, ok},
		{"case 112 hidden history and a non-admin", state(member, "PUB", mgr), OpenHistory, false, []func(*model.Message){hidden}, forbid},
		{"case 113 hidden history and its author", state(member, "PUB", lm), OpenHistory, true, []func(*model.Message){hidden}, forbid},
		{"case 112 hidden history and an admin", state(admin, "PUB", ""), OpenHistory, false, []func(*model.Message){hidden}, ok},
		{"case 117 purge asked by someone else", state(member, "PUB", mgr), AskPurge, false, nil, forbid},
		{"purge asked by the author", state(member, "PUB", rd), AskPurge, true, nil, ok},
		{"case 119 purge in a sealed chat", state(member, "SEALED", lm), AskPurge, true, nil, forbid},
		{"case 59 a manager who is not an admin purges", state(member, "PUB", mgr), PurgeDirectly, false, nil, forbid},
		{"an admin purges what they can read", state(admin, "PUB", ""), PurgeDirectly, false, nil, ok},
		{"an admin cannot purge what they cannot read", state(admin, "PRIV", ""), PurgeDirectly, false, nil, notFound},
		{"case 72 member closes another's poll", state(member, "PUB", lm), CloseOwnForm, false, nil, forbid},
		{"the author closes their own form", state(member, "PUB", rd), CloseOwnForm, true, nil, ok},
		{"case 73 member moves someone else's thread", state(member, "PUB", lm), MoveThread, false, nil, forbid},
		{"the starter moves their thread", state(member, "PUB", lm), MoveThread, true, nil, ok},
		{"react to a deleted message", state(member, "PUB", lm), ReactTo, false, []func(*model.Message){func(m *model.Message) { m.DeletedAt = 5 }}, conflict},
		{"case 204 tick without having joined", state(member, "PUB", ""), TickOn, false, nil, forbid},
	}
	for _, c := range cases {
		want(t, c.name, CheckMessage(c.st, c.a, msg(c.st, c.own, c.mod...)), c.want)
	}

	// Section 6 notes 6 and 7.
	mgrState := state(member, "PUB", mgr)
	modState := state(member, "PUB", model.LevelModerator)
	other := func(role model.Role) model.Account { a := account(role); a.ID += "-2"; return a }
	someone := model.Member{Level: lm, DirectLevel: lm}
	keeper := model.Member{Level: model.LevelModerator, DirectLevel: model.LevelModerator}
	boss := model.Member{Level: mgr, DirectLevel: mgr}
	want(t, "manager removes a member", CheckRemoveMember(mgrState, other(model.RoleMember), someone), ok)
	want(t, "case 302 moderator removes a member", CheckRemoveMember(modState, other(model.RoleMember), someone), ok)
	want(t, "case 302 moderator removes a moderator", CheckRemoveMember(modState, other(model.RoleMember), keeper), forbid)
	want(t, "case 302 moderator removes a manager", CheckRemoveMember(modState, other(model.RoleMember), boss), forbid)
	want(t, "manager removes a manager", CheckRemoveMember(mgrState, other(model.RoleMember), boss), ok)
	want(t, "case 310 manager removes an admin (ruling R99)", CheckRemoveMember(mgrState, other(model.RoleAdmin), someone), ok)
	want(t, "manager removes an owner", CheckRemoveMember(mgrState, other(model.RoleOwner), someone), forbid)
	want(t, "admin removes the owner", CheckRemoveMember(state(admin, "PUB", ""), other(model.RoleOwner), someone), forbid)
	want(t, "removing oneself is leaving", CheckRemoveMember(mgrState, account(member), boss), forbid)
	want(t, "case 102 remove someone who is in through a group", CheckRemoveMember(mgrState, other(model.RoleMember), model.Member{Level: lm, ViaGroup: true}), conflict)
	want(t, "case 151 a remote account made manager", CheckSetLevel(mgrState, account(model.RoleRemote), model.LevelManager), forbid)
	want(t, "case 151 a remote account made moderator", CheckSetLevel(mgrState, account(model.RoleRemote), model.LevelModerator), forbid)
	want(t, "a member made manager", CheckSetLevel(mgrState, other(model.RoleMember), model.LevelManager), ok)
	want(t, "case 312 a member made moderator", CheckSetLevel(mgrState, other(model.RoleMember), model.LevelModerator), ok)
	want(t, "case 312 a moderator appoints a moderator", CheckSetLevel(modState, other(model.RoleMember), model.LevelModerator), forbid)
	want(t, "case 313 a guest made moderator", CheckSetLevel(mgrState, account(model.RoleGuest), model.LevelModerator), forbid)
	want(t, "case 313 a guest made reader", CheckSetLevel(mgrState, account(model.RoleGuest), model.LevelReader), ok)
	want(t, "a bot made moderator", CheckSetLevel(mgrState, account(model.RoleBot), model.LevelModerator), ok)
	want(t, "a member made manager of a hub channel's copy", CheckSetLevel(state(member, "PUB", mgr, onManagers), other(model.RoleMember), model.LevelManager), conflict)
	want(t, "a member made moderator of a hub channel's copy", CheckSetLevel(state(member, "PUB", mgr, onManagers), other(model.RoleMember), model.LevelModerator), conflict)
	want(t, "a member made reader there", CheckSetLevel(state(member, "PUB", mgr, onManagers), other(model.RoleMember), model.LevelReader), ok)
}

func TestServerRules(t *testing.T) {
	set := DefaultSettings()
	invites := set
	invites.MembersMayInvite = true
	sealedOff := set
	sealedOff.SealedEnabled = false
	blockingOff := set
	blockingOff.BlockingEnabled = false
	const owner, admin, member, guest, bot, remote = model.RoleOwner, model.RoleAdmin, model.RoleMember, model.RoleGuest, model.RoleBot, model.RoleRemote
	for _, c := range []struct {
		name string
		role model.Role
		a    ServerAction
		set  Settings
		want model.Code
	}{
		{"member browses", member, Browse, set, ok},
		{"case 75 guest creates a channel", guest, CreateChannel, set, forbid},
		{"bot creates a channel", bot, CreateChannel, set, forbid},
		{"guest starts an ad-hoc channel (decision P13)", guest, StartAdhoc, set, ok},
		{"bot starts an ad-hoc channel (ruling R7)", bot, StartAdhoc, set, ok},
		{"bot starts a sealed chat", bot, StartSealed, set, forbid},
		{"case 148 sealed chats off", member, StartSealed, sealedOff, featOff},
		{"case 76 guest creates an invite", guest, CreateMemberInvite, invites, forbid},
		{"case 79 member creates an invite, setting off", member, CreateMemberInvite, set, forbid},
		{"member creates an invite, setting on", member, CreateMemberInvite, invites, ok},
		{"case 80 member creates a guest invite", member, CreateGuestInvite, invites, forbid},
		{"admin creates a guest invite", admin, CreateGuestInvite, set, ok},
		{"case 77 guest joins an open group", guest, JoinOpenGroup, set, forbid},
		{"case 108 member edits a group", member, ManageGroups, set, forbid},
		{"case 88 member creates a bot", member, ManageBots, set, forbid},
		{"case 43 admin runs the export", admin, RunExport, set, forbid},
		{"case 43 owner runs the export", owner, RunExport, set, ok},
		{"case 44 admin changes a role (between guest and member, CheckSetRole)", admin, ChangeRole, set, ok},
		{"case 47 admin links servers", admin, ManageLinks, set, forbid},
		{"case 114 member lists purge requests", member, DecidePurgeRequests, set, forbid},
		{"admin reads the audit log", admin, ReadAuditLog, set, ok},
		{"guest makes a personal token", guest, MakePersonalToken, set, forbid},
		{"member makes a personal token", member, MakePersonalToken, set, ok},
		{"bot registers commands", bot, RegisterCommands, set, ok},
		{"owner registers commands", owner, RegisterCommands, set, forbid},
		{"remote account and its profile", remote, OwnProfile, set, unauth},
		{"case 143 block while blocking is off", member, CreateBlock, blockingOff, featOff},
	} {
		want(t, c.name, CheckServer(account(c.role), c.a, c.set), c.want)
	}
	for _, c := range []struct {
		name          string
		actor, target model.Role
		a             AccountAction
		want          model.Code
	}{
		{"admin removes a member", admin, member, RemoveAccount, ok},
		{"case 45 admin removes an admin", admin, admin, RemoveAccount, forbid},
		{"owner removes an admin", owner, admin, RemoveAccount, ok},
		{"case 46 admin removes the owner", admin, owner, RemoveAccount, forbid},
		{"case 46 owner times out the owner", owner, owner, TimeOutAccount, forbid},
		{"member removes a guest", member, guest, RemoveAccount, forbid},
		{"case 107 admin adds a remote account to a group", admin, remote, AddToGroup, forbid},
		{"admin adds a guest to a group", admin, guest, AddToGroup, ok},
	} {
		want(t, c.name, CheckOnAccount(account(c.actor), c.a, account(c.target)), c.want)
	}
	if f := AccountFilter(account(guest)); f.SharedWith == "" || f.LocalOrSharedWith != "" {
		t.Errorf("a guest sees only accounts it shares a conversation with: %+v", f)
	}
	if f := AccountFilter(account(member)); f.LocalOrSharedWith == "" || f.SharedWith != "" {
		t.Errorf("a member sees every local account: %+v", f)
	}
}
