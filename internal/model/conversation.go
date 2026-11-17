package model

// Level is an account's level in a conversation.
type Level string

// The levels, lowest first.
const (
	LevelReader  Level = "reader"
	LevelMember  Level = "member"
	LevelManager Level = "manager"
)

// Rank orders levels: reader < member < manager. An unknown level ranks 0.
func (l Level) Rank() int {
	switch l {
	case LevelReader:
		return 1
	case LevelMember:
		return 2
	case LevelManager:
		return 3
	}
	return 0
}

// Valid reports whether l is one of the three levels.
func (l Level) Valid() bool { return l.Rank() > 0 }

// AtLeast reports whether l is the given level or higher.
func (l Level) AtLeast(min Level) bool { return l.Rank() >= min.Rank() && l.Rank() > 0 }

// HighestLevel returns the highest of the levels given, or "" for none.
func HighestLevel(levels ...Level) Level {
	var best Level
	for _, l := range levels {
		if l.Rank() > best.Rank() {
			best = l
		}
	}
	return best
}

// Mode is what a conversation notifies one of its members about.
type Mode string

// The three modes (ruling R46). A mute is not a mode: it is muted_until
// beside the mode (ruling R40), and behaves as ModeNothing while it lasts.
// Whether the conversation is in the person's sidebar is Show, which is
// independent of this.
const (
	ModeAny      Mode = "any"
	ModeMentions Mode = "mentions"
	ModeNothing  Mode = "nothing"
)

// Valid reports whether m is one of the three modes.
func (m Mode) Valid() bool {
	switch m {
	case ModeAny, ModeMentions, ModeNothing:
		return true
	}
	return false
}

// The values of a membership's `show`: whether the conversation is in the
// person's sidebar (ruling R46). The empty string is "the app's own
// default", sent as null. The server stores and returns it and works
// nothing out from it: a hidden conversation notifies and counts like any
// other.
const (
	ShowAlways       = "always"
	ShowActivity     = "activity"
	ShowNotification = "notification"
	ShowHidden       = "hidden"
)

// ValidShow reports whether s is one of the four values of `show`.
func ValidShow(s string) bool {
	switch s {
	case ShowAlways, ShowActivity, ShowNotification, ShowHidden:
		return true
	}
	return false
}

// The kinds of conversation.
const (
	KindChannel = "channel"
	KindSealed  = "sealed"
)

// LibraryMemberKey is the member_key of the one library conversation, which
// holds avatar and emoji files and is visible to nobody.
const LibraryMemberKey = "library"

// Conversation is a channel (named or ad-hoc) or a sealed chat (schema
// Conversation), with the stored columns that are not sent.
type Conversation struct {
	ID      ConversationID `json:"id"`
	Kind    string         `json:"kind"`
	Private bool           `json:"private"`
	// Access is how someone outside comes in (ruling R79): AccessPublic
	// (join freely), AccessKnock (seen, asked to join) or AccessPrivate
	// (invitation only). It is worked out from Private and Knock when the
	// conversation is shown.
	Access string `json:"access,omitempty"`
	// Knock is the stored column: a private channel that owner, admin and
	// member accounts can see and ask to join (ruling R79).
	Knock bool `json:"-"`
	// ManagerIDs are the managers of a knock channel, sent only to someone
	// outside it, who is shown nothing else of who is in it.
	ManagerIDs []AccountID `json:"manager_ids,omitempty"`
	Name       string      `json:"name,omitempty"`
	Topic      string      `json:"topic"`
	Welcome    string      `json:"welcome,omitempty"`
	SectionID  SectionID   `json:"section_id,omitempty"`
	// ParentID is the named channel this one sits under (ruling R49). It is
	// sent only to a reader who can see that parent.
	ParentID         ConversationID `json:"parent_id,omitempty"`
	DefaultLevel     Level          `json:"default_level"`
	RetentionSeconds *int64         `json:"retention_seconds,omitempty"`
	SlowModeSeconds  int            `json:"slow_mode_seconds"`
	// BroadMentions is the channel's own answer to who may use @channel,
	// @here and @everyone in it: BroadServer, BroadAnyone or BroadManagers
	// (ruling R64).
	BroadMentions string `json:"broad_mentions"`
	// BroadMentionsAllowed says whether the reader may use them here now.
	// It is worked out for one reader and absent where the conversation is
	// sent to everyone.
	BroadMentionsAllowed *bool `json:"broad_mentions_allowed,omitempty"`
	// LazyDelivery is the channel's own answer to whether it is delivered
	// in full only to the connections that have it open (ruling R66):
	// LazyServer follows the server's large_channel_limit, LazyOn and
	// LazyOff decide whatever its size.
	LazyDelivery string `json:"lazy_delivery"`
	// Announcement says only the channel's managers and the members they
	// allow post here; the others read and react, are not shown who else
	// is here, and nobody is shown typing or presence. With
	// AnnouncementReplies anyone who could otherwise post may reply in
	// threads.
	Announcement        bool `json:"announcement"`
	AnnouncementReplies bool `json:"announcement_replies"`
	// Large says the conversation has more members than the server's
	// large_feature_limit, or is an announcement channel: no typing
	// notices and no presence of single members; an online count instead.
	// It is worked out when the conversation is shown.
	Large              bool        `json:"large"`
	LockedAt           Time        `json:"locked_at,omitempty"`
	CreatedByID        AccountID   `json:"created_by_id,omitempty"`
	CreatedAt          Time        `json:"created_at"`
	ArchivedAt         Time        `json:"archived_at,omitempty"`
	LastSeq            int64       `json:"last_seq"`
	LastMessageAt      Time        `json:"last_message_at,omitempty"`
	MemberCount        int64       `json:"member_count"`
	MemberIDs          []AccountID `json:"member_ids,omitempty"`
	MovedFromMessageID MessageID   `json:"moved_from_message_id,omitempty"`
	TicketNumber       int         `json:"ticket_number,omitempty"`
	Shared             *Shared     `json:"shared,omitempty"`
	ReadOnly           bool        `json:"read_only,omitempty"`
	// ChatState is set on a direct or sealed chat that cannot be written
	// in: ChatWaiting while the request that started it waits, ChatDeclined
	// after the person asked declined, ChatClosed once either person closed
	// it (rulings R78, R79). It never says who closed it.
	ChatState  string      `json:"chat_state,omitempty"`
	KeyEpoch   *int64      `json:"key_epoch,omitempty"`
	Call       *Call       `json:"call,omitempty"`
	Membership *Membership `json:"membership,omitempty"`

	// Stored only.
	// ClosedAt is when the chat was closed; RequestState is ChatWaiting,
	// ChatDeclined or "".
	ClosedAt     Time      `json:"-"`
	RequestState string    `json:"-"`
	LockedBy     AccountID `json:"-"`
	MemberKey    string    `json:"-"`
	NextSeq      int64     `json:"-"`
	StreamID     StreamID  `json:"-"`
	// OrderFloor is 0 unless the conversation was shared once and no longer
	// is (LocalOrder).
	OrderFloor int64 `json:"-"`
}

// Who may use broad mentions in a channel (ruling R64): whoever the server's
// settings allow, anyone who can post whatever its size, or its managers.
const (
	BroadServer   = "server"
	BroadAnyone   = "anyone"
	BroadManagers = "managers"
)

// Whether a channel is delivered lazily (ruling R66).
const (
	LazyServer = "server"
	LazyOn     = "on"
	LazyOff    = "off"
)

// ValidLazyDelivery reports whether v is one of the three answers.
func ValidLazyDelivery(v string) bool {
	return v == LazyServer || v == LazyOn || v == LazyOff
}

// DefaultLargeChannelLimit is the server's large_channel_limit until an
// admin changes it: a channel with more members is delivered in full only
// to the connections that have it open. DefaultLargeFeatureLimit is its
// large_feature_limit: a conversation with more members has no typing
// notices and no presence of single members (ruling R66).
const (
	DefaultLargeChannelLimit = 1000
	DefaultLargeFeatureLimit = 50
)

// DefaultBroadMentionLimit is the server's broad_mention_limit until an
// admin changes it: above this many members only managers and admins use
// broad mentions. BroadConfirmAbove is the number of recipients above which
// a client asks the author to confirm.
const (
	DefaultBroadMentionLimit = 250
	BroadConfirmAbove        = 30
)

// ValidBroadMentions reports whether v is one of the three answers.
func ValidBroadMentions(v string) bool {
	return v == BroadServer || v == BroadAnyone || v == BroadManagers
}

// Named reports whether the conversation is a named channel.
func (c Conversation) Named() bool { return c.Kind == KindChannel && c.Name != "" }

// Adhoc reports whether the conversation is an ad-hoc channel: a channel with
// no name. The library conversation also has no name; IsLibrary tells it apart.
func (c Conversation) Adhoc() bool {
	return c.Kind == KindChannel && c.Name == "" && c.MemberKey != LibraryMemberKey
}

// Public reports whether the conversation is a public channel.
func (c Conversation) Public() bool { return c.Kind == KindChannel && !c.Private }

// The kinds of access to a channel (ruling R79).
const (
	AccessPublic  = "public"
	AccessKnock   = "knock"
	AccessPrivate = "private"
)

// IsKnock reports whether the conversation is a knock channel: named, and
// seen by owner, admin and member accounts outside it, who ask to join.
func (c Conversation) IsKnock() bool {
	return c.Kind == KindChannel && c.Private && c.Knock && c.Name != ""
}

// AccessKind is the channel's kind of access (ruling R79).
func (c Conversation) AccessKind() string {
	switch {
	case c.Public():
		return AccessPublic
	case c.IsKnock():
		return AccessKnock
	}
	return AccessPrivate
}

// Sealed reports whether the conversation is a sealed chat.
func (c Conversation) Sealed() bool { return c.Kind == KindSealed }

// IsLibrary reports whether this is the server's library conversation.
// HistoryFromJoin reports whether a newcomer reads only what is posted from
// their first join on (ruling R87): a named private channel that is not a
// knock channel.
func (c Conversation) HistoryFromJoin() bool { return c.Named() && c.Private && !c.Knock }

func (c Conversation) IsLibrary() bool { return c.MemberKey == LibraryMemberKey }

// Archived reports whether the conversation is archived.
func (c Conversation) Archived() bool { return !c.ArchivedAt.IsZero() }

// Locked reports whether the conversation is locked to new members.
func (c Conversation) Locked() bool { return !c.LockedAt.IsZero() }

// Closed reports whether a direct or sealed chat was closed (ruling R78).
func (c Conversation) Closed() bool { return !c.ClosedAt.IsZero() }

// Waiting reports whether a chat still waits on its request, or the
// request was declined: nothing can be written in it.
func (c Conversation) Waiting() bool { return c.RequestState != "" }

// State is the chat_state a conversation is shown with.
func (c Conversation) State() string {
	if c.Closed() {
		return ChatClosed
	}
	return c.RequestState
}

// IsShared reports whether the conversation is shared with other servers.
func (c Conversation) IsShared() bool { return c.StreamID != "" }

// Shared describes a conversation shared with other servers.
type Shared struct {
	StreamID     StreamID `json:"stream_id"`
	OwnerServer  string   `json:"owner_server"`
	Servers      []string `json:"servers"`
	MaxBlobBytes int64    `json:"max_blob_bytes,omitempty"`
}

// Membership is the caller's own place in a conversation, with the settings
// only they see (schema Membership).
type Membership struct {
	Level    Level `json:"level"`
	ViaGroup bool  `json:"via_group"`
	Direct   bool  `json:"direct,omitempty"`
	JoinedAt Time  `json:"joined_at"`
	ReadSeq  int64 `json:"read_seq"`
	Mode     Mode  `json:"mode"`
	// Show is absent for "the app's own default".
	Show       string `json:"show,omitempty"`
	MutedUntil Time   `json:"muted_until,omitempty"`
	Starred    bool   `json:"starred"`
	ThreadView string `json:"thread_view,omitempty"`
	// IgnoreBroadMentions: @channel, @here and @everyone do not notify
	// this person here. A mention by name or of another group still does.
	IgnoreBroadMentions bool `json:"ignore_broad_mentions"`
	// MayPost: a manager has allowed this person to post here while the
	// conversation is an announcement channel (ruling R66). Its managers
	// post without it.
	MayPost       bool  `json:"may_post"`
	UnreadCount   int64 `json:"unread_count"`
	MentionCount  int64 `json:"mention_count"`
	BadgeCount    int64 `json:"badge_count"`
	TimedOutUntil Time  `json:"timed_out_until,omitempty"`
}

// Member is an account in a conversation (schema Member), with the member's
// own settings, which are sent only to that member as a Membership.
type Member struct {
	ConversationID ConversationID `json:"conversation_id"`
	AccountID      AccountID      `json:"account_id"`
	Level          Level          `json:"level"`
	DirectLevel    Level          `json:"direct_level,omitempty"`
	ViaGroup       bool           `json:"via_group"`
	JoinedAt       Time           `json:"joined_at"`

	// Stored only.
	ReadSeq        int64  `json:"-"`
	Mode           Mode   `json:"-"`
	Show           string `json:"-"` // always | activity | notification | hidden | "" for the app's default
	ModeBeforeMute Mode   `json:"-"`
	MutedUntil     Time   `json:"-"`
	Starred        bool   `json:"-"`
	ThreadView     string `json:"-"` // threaded | flat | "" to follow the account preference
	LastPostAt     Time   `json:"-"`
	IgnoreBroad    bool   `json:"-"` // broad mentions do not notify (ruling R64)
	// VisibleFrom is the earliest message time the member may read: in a
	// named private channel, their first join there (ruling R87); zero
	// everywhere else.
	VisibleFrom Time `json:"-"`

	// MayPost says a manager has allowed this member to post in the
	// conversation while it is an announcement channel (ruling R66).
	MayPost bool `json:"may_post"`
}

// ModeInForce is the mode that applies now: a mute whose time has passed is
// read as the mode from before it (docs/FEATURES.md section 2.3).
func (m Member) ModeInForce(now Time) Mode {
	if !m.MutedUntil.IsZero() && !m.MutedUntil.After(now) && m.ModeBeforeMute != "" {
		return m.ModeBeforeMute
	}
	return m.Mode
}

// ChannelGroup is a group added to a channel at a level (schema ChannelGroup).
type ChannelGroup struct {
	ConversationID ConversationID `json:"conversation_id"`
	GroupID        GroupID        `json:"group_id"`
	Level          Level          `json:"level"`
	AddedByID      AccountID      `json:"added_by_id,omitempty"`
	AddedAt        Time           `json:"added_at"`
}

// Call is a call in a conversation (schema Call).
type Call struct {
	ID             CallID            `json:"id"`
	ConversationID ConversationID    `json:"conversation_id"`
	MessageID      MessageID         `json:"message_id,omitempty"`
	StartedByID    AccountID         `json:"started_by_id"`
	StartedAt      Time              `json:"started_at"`
	EndedAt        Time              `json:"ended_at,omitempty"`
	Sealed         bool              `json:"sealed"`
	HostedBy       string            `json:"hosted_by,omitempty"`
	Participants   []CallParticipant `json:"participants"`
}

// CallParticipant is an account in a call. DeviceID is the device the
// account is in the call on, when the server knows it: in a sealed chat it
// names whose call sender key decrypts the participant's frames.
type CallParticipant struct {
	AccountID AccountID `json:"account_id"`
	DeviceID  DeviceID  `json:"device_id,omitempty"`
	JoinedAt  Time      `json:"joined_at"`
	Tracks    []any     `json:"tracks,omitempty"`
}

// Peer is a link to one other server, or to a hub (schema Peer).
type Peer struct {
	ID              PeerID    `json:"id"`
	Kind            string    `json:"kind"` // server | hub
	Name            string    `json:"name,omitempty"`
	Fingerprint     string    `json:"fingerprint,omitempty"`
	Dials           bool      `json:"dials"`
	State           string    `json:"state"` // invited | active | paused | removed
	InviteExpiresAt Time      `json:"invite_expires_at,omitempty"`
	CreatedByID     AccountID `json:"created_by_id"`
	CreatedAt       Time      `json:"created_at"`
	ConnectedAt     Time      `json:"connected_at,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
	// ToldClock is what the peer, as a time reference, has measured of this
	// server's clock and told it: what this server stamps events with in
	// the conversations the peer is the reference of. MeasuredClock is what
	// this server, as a time reference, has measured of the peer's clock,
	// on a direct link. Both are kept while disconnected.
	ToldClock     *PeerClock `json:"told_clock,omitempty"`
	MeasuredClock *PeerClock `json:"measured_clock,omitempty"`
}

// Stream is one conversation shared over a peer (schema Stream).
type Stream struct {
	ID             StreamID       `json:"id"`
	PeerID         PeerID         `json:"peer_id"`
	ConversationID ConversationID `json:"conversation_id,omitempty"`
	OwnerServer    string         `json:"owner_server,omitempty"`
	Reference      string         `json:"reference"` // self | owner | hub: who is the stream's time reference
	Kind           string         `json:"kind"`      // channel | adhoc | sealed
	State          string         `json:"state"`     // offered | active | left | removed
	KeyEpoch       int64          `json:"key_epoch"`
	OfferedName    string         `json:"offered_name,omitempty"`
	OfferedAt      Time           `json:"offered_at,omitempty"`
	HistoryDays    int            `json:"history_days"`
	MaxBlobBytes   int64          `json:"max_blob_bytes,omitempty"`
	CreatedAt      Time           `json:"created_at"`
}

// Timeout is a time-out: the account can read but cannot post or react
// (schema Timeout).
type Timeout struct {
	ID             string         `json:"id"`
	AccountID      AccountID      `json:"account_id"`
	ConversationID ConversationID `json:"conversation_id,omitempty"` // empty for the whole server
	Until          Time           `json:"until"`
	Reason         string         `json:"reason"`
	CreatedByID    AccountID      `json:"created_by_id"`
	CreatedAt      Time           `json:"created_at"`
	LiftedAt       Time           `json:"lifted_at,omitempty"`
}
