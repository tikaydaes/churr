// Package perm is the one place that answers "may this account do this
// here". Handlers, search, downloads and the live fan-out all ask it, and
// nothing else decides. docs/PERMISSIONS.md is the table this package
// implements; section numbers in comments point into that document.
//
// There are two layers. The rules are pure functions over a State (this
// file, message.go, server.go): no I/O and no clock, so the same question
// always has the same answer and the rules can be tested as tables. The
// Checker (checker.go) loads a State from the store and asks them.
package perm

import (
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Settings are the server settings rules read, with the defaults of
// docs/SCHEMA.md section 2.
type Settings struct {
	MembersMayInvite      bool
	MembersMayAddEmoji    bool
	BroadcastManagersOnly bool
	// BroadMentionLimit is the number of members above which only a
	// channel's managers and the admins use broad mentions; 0 is no limit.
	BroadMentionLimit int64
	// LargeChannelLimit is the number of members above which a channel is
	// delivered in full only to the connections that have it open, and
	// LargeFeatureLimit the number above which a conversation has no
	// typing notices and no presence of single members (ruling R66); 0 is
	// no limit.
	LargeChannelLimit          int64
	LargeFeatureLimit          int64
	BlockingEnabled            bool
	SealedEnabled              bool
	CallsEnabled               bool
	WebhooksManagersMayApprove bool
	// EditWindowSeconds is how long an author may edit; 0 means no limit.
	EditWindowSeconds int64
	// RetentionSeconds is the server's retention period; 0 means keep.
	RetentionSeconds int64
}

// DefaultSettings are the settings of a server nobody has changed.
func DefaultSettings() Settings {
	return Settings{BlockingEnabled: true, SealedEnabled: true, CallsEnabled: true,
		BroadMentionLimit: model.DefaultBroadMentionLimit,
		LargeChannelLimit: model.DefaultLargeChannelLimit, LargeFeatureLimit: model.DefaultLargeFeatureLimit}
}

// State is everything the rules need to know about one actor and one
// conversation, loaded once.
type State struct {
	Actor model.Account
	// Bot is set when the actor's role is bot.
	Bot          *store.BotInfo
	Conversation model.Conversation
	// Member is nil when the actor is not a member.
	Member *model.Member
	// TimedOutUntil is not zero while the actor is timed out here, by a
	// time-out on this conversation or on the whole server.
	TimedOutUntil model.Time
	Settings      Settings
	Now           model.Time
	// FromMovedThreadOf is true when the conversation was made by moving
	// a thread out of a channel the actor is a member of (section 4.1
	// note 2).
	FromMovedThreadOf bool
	// HubModerator is true when the conversation is this server's copy of
	// a channel a hub owns and the actor is an account of this server on
	// the moderator list the hub signed (section 14.1). The authority is
	// the hub's list, not the actor's role or level here.
	HubModerator bool
	// ChannelBanned is true when the actor is banned from the channel
	// (section 6.1). It is read only for an actor that is not a member,
	// since a ban takes its account out and keeps it out.
	ChannelBanned bool
}

// IsMember reports whether the actor is a member of the conversation.
func (st State) IsMember() bool { return st.Member != nil }

// Level is the actor's level, or "" when it is not a member.
func (st State) Level() model.Level {
	if st.Member == nil {
		return ""
	}
	return st.Member.Level
}

// webhook reports whether the actor is the account behind a webhook.
func (st State) webhook() bool { return st.Bot != nil && st.Bot.WebhookOnly }

// ActsAsManager is section 2.4: the actor's level is manager, or it is an
// admin and the channel is public, or it is an admin and a member. It never
// applies in a sealed chat, which has no manager.
func (st State) ActsAsManager() bool {
	c := st.Conversation
	if c.Kind != model.KindChannel || c.IsLibrary() {
		return false
	}
	if st.Level() == model.LevelManager {
		return true
	}
	if st.Actor.Role.IsAdmin() && st.Actor.Active() {
		return c.Public() || st.IsMember()
	}
	return false
}

// CanSee is section 4.1: members see their conversations; owner, admin and
// member see public channels; nobody sees the library. The admin view of a
// private channel is not "seeing" here: ordinary operations answer an admin
// outside it like anyone else (decision P22).
func (st State) CanSee() bool {
	c := st.Conversation
	if c.IsLibrary() || !st.Actor.Active() {
		return false
	}
	if st.ChannelBanned && !st.IsMember() {
		// A channel ban hides the channel from the one banned, public or
		// not (section 6.1).
		return false
	}
	if st.IsMember() {
		return true
	}
	if c.Public() && st.Actor.Role.IsFullMember() {
		return true
	}
	// A knock channel is seen, not read, by whoever would see a public one
	// (ruling R79, section 6.2): to be asked to join.
	if c.IsKnock() && st.Actor.Role.IsFullMember() {
		return true
	}
	return st.FromMovedThreadOf && st.Actor.Role.IsFullMember()
}

// CanRead is section 4.1: whoever can see a conversation through membership
// or a public channel can read it. The moved-thread exception lets someone
// see and join, not read.
func (st State) CanRead() bool {
	if !st.CanSee() {
		return false
	}
	return st.IsMember() || (st.Conversation.Public() && st.Actor.Role.IsFullMember())
}

// VisibleFrom is the earliest message time the actor may read here: in a
// private channel, their first join (ruling R87); zero otherwise. Nobody
// is excused, admins and the owner included.
func (st State) VisibleFrom() model.Time {
	if st.Member == nil {
		return 0
	}
	return st.Member.VisibleFrom
}

// Sees reports whether a message in the conversation was posted late
// enough for the actor to read it (ruling R87).
func (st State) Sees(m model.Message) bool { return !m.CreatedAt.Before(st.VisibleFrom()) }

// Action is something done to or in a conversation.
type Action string

// Reading (section 4.2).
const (
	See         Action = "see"
	Read        Action = "read"
	Search      Action = "search"
	ReceiveLive Action = "receive_live"
	ListMembers Action = "list_members"
	MarkRead    Action = "mark_read"
	SetMode     Action = "set_mode"
	Bookmark    Action = "bookmark"
	Report      Action = "report"
)

// Taking part (section 5).
const (
	React           Action = "react"
	AnswerForm      Action = "answer_form"
	AddFormOption   Action = "add_form_option"
	Post            Action = "post"
	Reply           Action = "reply"
	Upload          Action = "upload"
	Tick            Action = "tick"
	Typing          Action = "typing"
	PostForm        Action = "post_form"
	PostChannelForm Action = "post_channel_form"
	PostGroupForm   Action = "post_group_form"
	PostBotForm     Action = "post_bot_form"
	PostCards       Action = "post_cards"
	Broadcast       Action = "broadcast"
	NotifyAnyway    Action = "notify_anyway"
	SendCommand     Action = "send_command"
	RunSavedReply   Action = "run_saved_reply"
	Schedule        Action = "schedule"
	StartCall       Action = "start_call"
	FollowThread    Action = "follow_thread"
	MoveOwnThread   Action = "move_own_thread"
	Leave           Action = "leave"
)

// Membership (section 6).
const (
	Join            Action = "join"
	AddAccount      Action = "add_account"
	AddBot          Action = "add_bot"
	AddGroup        Action = "add_group"
	RemoveGroup     Action = "remove_group"
	RemoveMember    Action = "remove_member"
	SetLevel        Action = "set_level"
	SetDefaultLevel Action = "set_default_level"
	BanMember       Action = "ban_member"
	ListBans        Action = "list_bans"
	AnswerKnock     Action = "answer_knock"
)

// Managing (section 7).
const (
	SetTopic            Action = "set_topic"
	Rename              Action = "rename"
	SetWelcome          Action = "set_welcome"
	SetSection          Action = "set_section"
	Pin                 Action = "pin"
	RemoveAnyPreview    Action = "remove_any_preview"
	CloseAnyForm        Action = "close_any_form"
	ReadFormAnswers     Action = "read_form_answers"
	DeleteOthersMessage Action = "delete_others_message"
	SetSlowMode         Action = "set_slow_mode"
	SetBroadMentions    Action = "set_broad_mentions"
	SetLazyDelivery     Action = "set_lazy_delivery"
	SetAnnouncement     Action = "set_announcement"
	SetMayPost          Action = "set_may_post"
	// SetReferencePatterns: a channel's own reference patterns (R73).
	SetReferencePatterns Action = "set_reference_patterns"
	TimeOutMember        Action = "time_out_member"
	HandleReports        Action = "handle_reports"
	ShortenRetention     Action = "shorten_retention"
	SetRetention         Action = "set_retention"
	Archive              Action = "archive"
	Unarchive            Action = "unarchive"
	MakePrivate          Action = "make_private"
	Lock                 Action = "lock"
	MoveAnyThread        Action = "move_any_thread"
	RequestWebhook       Action = "request_webhook"
	ApproveWebhook       Action = "approve_webhook"
	Share                Action = "share"
	SetAnswererGroup     Action = "set_answerer_group"
)

// The admin console (section 4.1 note 1): what an admin may do to a named
// channel without being able to read it. These do not pass through "can the
// actor see it"; every ordinary action does.
const (
	AdminView           Action = "admin_view"
	AdminAppointManager Action = "admin_appoint_manager"
	AdminArchive        Action = "admin_archive"
	AdminUnarchive      Action = "admin_unarchive"
	AdminLock           Action = "admin_lock"
	AdminSetRetention   Action = "admin_set_retention"
)

// need says who may do an action, before the conversation's state is
// looked at.
type need int

const (
	needSee     need = iota // anyone who can see the conversation
	needRead                // anyone who can read it
	needAny                 // a member at any level
	needMember              // a member at level member or above
	needManager             // an account acting as manager
	needSpecial             // decided in code below
)

// rule is one row of the matrices of sections 4 to 7, with the columns of
// sections 10, 10.1 and 11 beside it.
type rule struct {
	need need
	// archived, locked and timedOut say whether that state refuses the
	// action.
	archived, locked, timedOut bool
	// sealed says whether the action exists in a sealed chat.
	sealed bool
	// named says the action is for named channels only.
	named bool
	// people says bots (and webhooks) may never do it.
	people bool
}

var rules = map[Action]rule{
	See:         {need: needSee, sealed: true},
	Read:        {need: needRead, sealed: true},
	Search:      {need: needRead},
	ListMembers: {need: needRead, sealed: true},
	Bookmark:    {need: needRead, sealed: true},
	Report:      {need: needRead}, // not in a sealed chat (decision P4)
	ReceiveLive: {need: needAny, sealed: true},
	MarkRead:    {need: needAny, sealed: true},
	SetMode:     {need: needAny, sealed: true},

	React:         {need: needAny, archived: true, timedOut: true},
	AnswerForm:    {need: needAny, archived: true, timedOut: true, people: true},
	AddFormOption: {need: needAny, archived: true, timedOut: true, people: true},
	SendCommand:   {need: needAny, archived: true, timedOut: true},
	FollowThread:  {need: needAny},
	Leave:         {need: needSpecial, sealed: true},

	Post:            {need: needMember, archived: true, timedOut: true, sealed: true},
	Reply:           {need: needMember, archived: true, timedOut: true, sealed: true},
	Upload:          {need: needMember, archived: true, timedOut: true, sealed: true},
	Tick:            {need: needMember, archived: true, timedOut: true},
	Typing:          {need: needMember, archived: true, timedOut: true, sealed: true},
	PostForm:        {need: needMember, archived: true, timedOut: true},
	PostChannelForm: {need: needManager, archived: true, timedOut: true},
	PostGroupForm:   {need: needSpecial, archived: true, timedOut: true},
	PostBotForm:     {need: needSpecial, archived: true, timedOut: true},
	PostCards:       {need: needSpecial, archived: true, timedOut: true},
	Broadcast:       {need: needSpecial, archived: true, timedOut: true},
	NotifyAnyway:    {need: needMember, people: true, sealed: true},
	RunSavedReply:   {need: needMember, archived: true, timedOut: true, sealed: true},
	Schedule:        {need: needMember, archived: true, timedOut: true, sealed: true},
	StartCall:       {need: needMember, archived: true, timedOut: true, sealed: true},
	MoveOwnThread:   {need: needSpecial, archived: true, timedOut: true},

	Join:            {need: needSpecial, archived: true, locked: true},
	AddAccount:      {need: needSpecial, archived: true, locked: true, timedOut: true},
	AddBot:          {need: needSpecial, archived: true, locked: true, timedOut: true},
	AddGroup:        {need: needSpecial, archived: true, locked: true, timedOut: true, named: true},
	RemoveGroup:     {need: needManager, archived: true, timedOut: true, named: true},
	RemoveMember:    {need: needManager, archived: true, timedOut: true},
	SetLevel:        {need: needManager, archived: true, timedOut: true},
	SetDefaultLevel: {need: needManager, archived: true, timedOut: true, named: true},
	BanMember:       {need: needManager, archived: true, timedOut: true},
	ListBans:        {need: needManager},
	AnswerKnock:     {need: needManager, archived: true, locked: true, timedOut: true, named: true},

	SetTopic:             {need: needManager, archived: true, timedOut: true},
	Rename:               {need: needManager, archived: true, timedOut: true},
	SetWelcome:           {need: needManager, archived: true, timedOut: true, named: true},
	SetSection:           {need: needManager, archived: true, timedOut: true, named: true},
	Pin:                  {need: needManager, archived: true, timedOut: true},
	RemoveAnyPreview:     {need: needManager, archived: true, timedOut: true},
	CloseAnyForm:         {need: needManager, archived: true, timedOut: true},
	ReadFormAnswers:      {need: needManager},
	DeleteOthersMessage:  {need: needManager, timedOut: true},
	SetSlowMode:          {need: needManager, archived: true, timedOut: true},
	SetBroadMentions:     {need: needManager, archived: true, timedOut: true},
	SetLazyDelivery:      {need: needManager, archived: true, timedOut: true, named: true},
	SetAnnouncement:      {need: needSpecial, archived: true, timedOut: true, named: true},
	SetMayPost:           {need: needManager, archived: true, timedOut: true, named: true},
	SetReferencePatterns: {need: needManager, archived: true, timedOut: true, named: true},
	TimeOutMember:        {need: needManager, timedOut: true},
	HandleReports:        {need: needSpecial, timedOut: true},
	ShortenRetention:     {need: needManager, timedOut: true, named: true},
	SetRetention:         {need: needSpecial, timedOut: true, named: true},
	Archive:              {need: needManager, timedOut: true},
	Unarchive:            {need: needManager, timedOut: true},
	MakePrivate:          {need: needSpecial, archived: true, timedOut: true},
	Lock:                 {need: needSpecial, timedOut: true},
	MoveAnyThread:        {need: needSpecial, archived: true, timedOut: true},
	RequestWebhook:       {need: needSpecial, archived: true, locked: true, timedOut: true, named: true},
	ApproveWebhook:       {need: needSpecial, archived: true, locked: true, named: true},
	Share:                {need: needSpecial, named: true},
	SetAnswererGroup:     {need: needManager, archived: true, timedOut: true},
}

// forbidden is the refusal for step 4, with a sentence that fits.
func forbidden(sentence string) error { return model.ErrForbidden.With(sentence) }

// Check answers one question about a conversation: nil, or the *model.Error
// to send. It applies the steps of section 1 in order and the first failure
// is the answer. In particular everything about a conversation the actor
// cannot see is not_found, never forbidden.
func Check(st State, a Action) error {
	// Step 1: the account can act at all.
	if !st.Actor.Active() {
		return model.ErrUnauthenticated
	}
	c := st.Conversation
	if c.IsLibrary() {
		return model.ErrNotFound
	}
	switch a {
	case AdminView, AdminAppointManager, AdminArchive, AdminUnarchive, AdminLock, AdminSetRetention:
		return checkAdmin(st, a)
	}
	r, known := rules[a]
	if !known {
		return forbidden("That is not something this server knows how to allow.")
	}
	// Step 2: the actor can see that the conversation exists.
	if !st.CanSee() {
		return model.ErrNotFound
	}
	// Step 3: the feature is switched on.
	if a == StartCall && !st.Settings.CallsEnabled {
		return model.ErrFeatureOff
	}
	// Step 4: role and level.
	if err := allowed(st, a, r); err != nil {
		return err
	}
	if err := announcement(st, a); err != nil {
		return err
	}
	// Step 5: archived (section 10).
	if r.archived && c.Archived() {
		return model.ErrArchived
	}
	// A direct or sealed chat that was closed, or whose request waits or
	// was declined, is read and not written in (rulings R78, R79). Closing
	// never says who closed it.
	if r.archived && c.Closed() {
		return forbidden("This chat is closed. Ask to chat again to start a new one.")
	}
	if r.archived && c.Waiting() {
		if c.RequestState == model.ChatDeclined {
			return forbidden("This chat request was declined.")
		}
		return forbidden("This chat opens when the other person accepts it.")
	}
	// Step 6: locked, for actions that bring someone in (section 10.1).
	if r.locked && c.Locked() {
		return model.ErrLocked
	}
	// Step 7: timed out (section 11).
	if r.timedOut && !st.TimedOutUntil.IsZero() {
		return model.ErrTimedOut
	}
	return nil
}

// MayAnnounce reports whether the actor posts in an announcement channel:
// its managers (and an admin acting as one), the members a manager has
// allowed, and a webhook the managers approved for it.
func (st State) MayAnnounce() bool {
	return st.ActsAsManager() || (st.Member != nil && st.Member.MayPost) || (st.webhook() && st.IsMember())
}

// announcement is the rest of step 4 in an announcement channel (ruling
// R66, section 5.1): only its managers and those they allow post; the
// others read and react, and may reply in threads if the channel says so;
// only the managers are shown who is in it. It is asked after the plain
// rule, so a reader is still told that readers cannot post.
func announcement(st State, a Action) error {
	c := st.Conversation
	if !c.Announcement || c.Kind != model.KindChannel {
		return nil
	}
	switch a {
	case ListMembers:
		if st.ActsAsManager() {
			return nil
		}
		return forbidden("An announcement channel does not show who is in it.")
	case Reply, Upload:
		// A reply may carry a file, so uploading follows replying.
		if c.AnnouncementReplies {
			return nil
		}
	case Post, PostForm, PostGroupForm, PostBotForm, PostCards, Schedule, RunSavedReply, StartCall, Broadcast:
	default:
		return nil
	}
	if st.MayAnnounce() {
		return nil
	}
	return forbidden("Only this channel's managers and the people they allow can post here.")
}

// allowed is step 4.
func allowed(st State, a Action, r rule) error {
	c := st.Conversation
	if c.Sealed() && !r.sealed {
		return forbidden("That is not possible in a sealed chat.")
	}
	if r.named && !c.Named() {
		return forbidden("That is for named channels only.")
	}
	if r.people && st.Actor.Role == model.RoleBot {
		return forbidden("Bots cannot do that.")
	}
	// A webhook's account posts into its one channel and does nothing
	// else (section 15).
	if st.webhook() && a != Post && a != Reply && a != PostForm && a != PostCards && a != Upload && a != See {
		return forbidden("A webhook can only post.")
	}
	switch r.need {
	case needSee:
		return nil
	case needRead:
		if !st.CanRead() {
			return model.ErrNotFound
		}
		return nil
	case needAny:
		if !st.IsMember() {
			return forbidden("Join the channel first.")
		}
		return nil
	case needMember:
		if !st.IsMember() {
			return forbidden("You are not in this channel.")
		}
		if !st.Level().AtLeast(model.LevelMember) {
			return forbidden("Readers of this channel cannot do that.")
		}
		return nil
	case needManager:
		if !st.ActsAsManager() {
			return forbidden("Only the channel's managers can do that.")
		}
		return nil
	}
	return special(st, a)
}

// special holds the rows of the matrices that are not one of the four
// plain needs.
func special(st State, a Action) error {
	c := st.Conversation
	role := st.Actor.Role
	atLeastMember := st.IsMember() && st.Level().AtLeast(model.LevelMember)
	switch a {
	case Leave:
		// Section 6 notes 9 and 10.
		if !st.IsMember() {
			return forbidden("You are not in this channel.")
		}
		if c.Sealed() {
			return model.ErrConflict.With("A sealed chat cannot be left. You can hide it.")
		}
		if st.Member.ViaGroup {
			return model.ErrConflict.With("You are in this channel through a group. Leave the group, or hide the channel.")
		}
		return nil

	case Join:
		// Section 6: owner, admin and member join public channels by
		// themselves; a private channel only through a moved thread.
		if !role.IsFullMember() {
			return forbidden("You must be added to channels.")
		}
		if c.Public() || st.FromMovedThreadOf {
			return nil
		}
		return forbidden("You must be added to this channel.")

	case AddAccount, AddBot:
		if c.Sealed() {
			return forbidden("Nobody can be added to a sealed chat.")
		}
		if c.Adhoc() {
			if !st.IsMember() {
				return forbidden("You are not in this channel.")
			}
			return nil
		}
		if !st.ActsAsManager() {
			return forbidden("Only the channel's managers can add people.")
		}
		return nil

	case AddGroup:
		// Section 6 note 4: guests and bots cannot add groups even where
		// they are managers.
		if !st.ActsAsManager() || !role.IsFullMember() {
			return forbidden("Only the channel's managers can add groups.")
		}
		return nil

	case PostGroupForm:
		if !role.IsAdmin() || !atLeastMember {
			return forbidden("Only an admin who is in the channel can post that form.")
		}
		return nil

	case PostBotForm, PostCards:
		if role != model.RoleBot || !atLeastMember {
			return forbidden("Only a bot can post that.")
		}
		return nil

	case Broadcast:
		// Section 5 note 4.
		if !atLeastMember {
			return forbidden("You cannot mention everyone here.")
		}
		if st.ActsAsManager() {
			// A channel's managers, and an admin, which acts as one.
			return nil
		}
		// The server-wide switch is the admins' and no channel loosens it.
		if st.Settings.BroadcastManagersOnly {
			return forbidden("Only managers can mention everyone on this server.")
		}
		// Ruling R64: the channel's own answer, and where it has none,
		// the server's limit on how large a channel anyone may call.
		switch c.BroadMentions {
		case model.BroadAnyone:
			return nil
		case model.BroadManagers:
			return forbidden("Only this channel's managers can mention everyone here.")
		}
		if limit := st.Settings.BroadMentionLimit; limit > 0 && c.MemberCount > limit {
			return forbidden("This channel is too large for everyone to be mentioned by anyone but its managers.")
		}
		return nil

	case MoveOwnThread, MoveAnyThread:
		// Section 5 note 7. Whether the actor started the thread is the
		// message's business (CheckMessage); here: level, the role to
		// create channels, and the kinds of conversation that refuse.
		if c.Sealed() || c.IsShared() {
			return forbidden("A thread cannot be moved out of this conversation.")
		}
		if a == MoveAnyThread && !st.ActsAsManager() {
			return forbidden("Only the channel's managers can move someone else's thread.")
		}
		if !atLeastMember && a == MoveOwnThread {
			return forbidden("You are not in this channel.")
		}
		if !role.IsFullMember() {
			return forbidden("You cannot create channels.")
		}
		return nil

	case HandleReports:
		// Section 8.4: in a private or ad-hoc channel only the stored
		// level counts, whatever the role.
		if c.Sealed() {
			return forbidden("Sealed chats have no reports.")
		}
		if c.Public() {
			if !st.ActsAsManager() {
				return forbidden("Only the channel's managers and admins handle its reports.")
			}
			return nil
		}
		if st.Level() != model.LevelManager {
			return forbidden("Only the channel's managers handle its reports.")
		}
		return nil

	case SetRetention:
		if !role.IsAdmin() || !st.ActsAsManager() {
			return forbidden("Only an admin can set a retention period longer than the server's.")
		}
		return nil

	case MakePrivate:
		// Section 7.1. Private to public is refused for everyone, and
		// that is the only change there could be to a private channel. A
		// knock channel's managers may change its access every way but to
		// private and back (ruling R79).
		if !c.Public() && !c.IsKnock() {
			return forbidden("A private channel can never be made public.")
		}
		if !st.ActsAsManager() {
			return forbidden("Only the channel's managers can make it private.")
		}
		return nil

	case Lock:
		// Section 10.1.
		if c.Sealed() {
			return forbidden("A sealed chat has nothing to lock.")
		}
		if !st.ActsAsManager() {
			return forbidden("Only the channel's managers can lock it.")
		}
		if c.IsShared() {
			return forbidden("A channel shared with other servers cannot be locked.")
		}
		if c.Public() {
			return model.ErrConflict.With("Only a private channel can be locked. Make it private first.")
		}
		return nil

	case RequestWebhook:
		// Section 9.1: owner, admin or member, in a named channel they
		// are in (ruling R8).
		if !role.IsFullMember() || !st.IsMember() {
			return forbidden("You cannot ask for a webhook here.")
		}
		return nil

	case ApproveWebhook:
		if role.IsAdmin() {
			return nil
		}
		if st.Settings.WebhooksManagersMayApprove && st.Level() == model.LevelManager {
			return nil
		}
		return forbidden("Only an admin can approve a webhook.")

	case SetAnnouncement:
		if !st.ActsAsManager() {
			return forbidden("Only the channel's managers can do that.")
		}
		// Ruling R66: local-only for now. The other servers of a shared
		// channel would not know the rule, and their members could post.
		if c.IsShared() {
			return model.ErrConflict.With("A channel shared with other servers cannot be an announcement channel yet.")
		}
		return nil

	case Share:
		// Section 7: the owner only, who must be able to read it; never
		// a locked channel (decision P25).
		if role != model.RoleOwner || !st.CanRead() {
			return forbidden("Only the owner can share a channel with other servers.")
		}
		if c.Announcement {
			return model.ErrConflict.With("An announcement channel cannot be shared with other servers yet.")
		}
		if c.Locked() {
			return forbidden("A locked channel cannot be shared.")
		}
		return nil
	}
	return forbidden("You may not do this.")
}

// checkAdmin is the admin console: an admin sees every named channel's
// name, members and managers, and may appoint a manager, archive or
// unarchive, lock and set retention there without being able to read it.
// Ad-hoc channels and sealed chats do not exist as far as it is concerned.
func checkAdmin(st State, a Action) error {
	if !st.Actor.Role.IsAdmin() {
		return forbidden("Only admins can use the admin console.")
	}
	c := st.Conversation
	if !c.Named() {
		return model.ErrNotFound
	}
	switch a {
	case AdminLock:
		if c.IsShared() {
			return forbidden("A channel shared with other servers cannot be locked.")
		}
		if c.Public() {
			return model.ErrConflict.With("Only a private channel can be locked. Make it private first.")
		}
	}
	return nil
}
