package model

// Types of the people area that the API returns: saved statuses, bookmarks,
// bans, the Threads view, what admins see of an account, and the totals of
// the status endpoint.

// More event names (docs/EVENTS.md section 9). None of these is stored.
const (
	EventStatusChanged       = "status.changed"
	EventAvailabilityChanged = "availability.changed"
	EventDndChanged          = "dnd.changed"
	EventMeUpdated           = "me.updated"
	EventAccountRemoved      = "account.removed"
	EventGroupCreated        = "group.created"
	EventGroupUpdated        = "group.updated"
	EventGroupDeleted        = "group.deleted"
	EventGroupMemberAdded    = "group.member_added"
	EventGroupMemberRemoved  = "group.member_removed"
	EventBlockAdded          = "block.added"
	EventBlockRemoved        = "block.removed"
	// A chat request arrived for the account, or left its list (ruling
	// R78).
	EventChatRequestReceived = "chat_request.received"
	EventChatRequestRemoved  = "chat_request.removed"
	// A named channel was offered to the account, or the offer went
	// (ruling R103).
	EventChannelOfferAdded   = "channel_offer.added"
	EventChannelOfferRemoved = "channel_offer.removed"
	EventBookmarkAdded       = "bookmark.added"
	EventBookmarkRemoved     = "bookmark.removed"
	EventBadgeChanged        = "badge.changed"
	EventTypingStarted       = "typing.started"
)

// The two availabilities.
const (
	AvailabilityMeeting     = "meeting"
	AvailabilityOutOfOffice = "out_of_office"
)

// StatusPreset is a saved status (schema StatusPreset).
type StatusPreset struct {
	ID                 string `json:"id"`
	Community          bool   `json:"community"`
	Emoji              string `json:"emoji,omitempty"`
	Text               string `json:"text"`
	DurationSeconds    *int64 `json:"duration_seconds,omitempty"`
	PauseNotifications bool   `json:"pause_notifications"`
	AppearAway         bool   `json:"appear_away"`
	Availability       string `json:"availability,omitempty"`
	Position           int64  `json:"position"`

	// OwnerID is empty for a community preset.
	OwnerID AccountID `json:"-"`
}

// Bookmark is a message a person saved for themselves (schema Bookmark).
type Bookmark struct {
	MessageID MessageID `json:"message_id"`
	CreatedAt Time      `json:"created_at"`
	Message   *Message  `json:"message,omitempty"`
}

// Ban is a ban on an account or a sign-in identity (schema Ban).
type Ban struct {
	ID          string    `json:"id"`
	AccountID   AccountID `json:"account_id,omitempty"`
	ProviderID  string    `json:"provider_id,omitempty"`
	Subject     string    `json:"subject,omitempty"`
	Reason      string    `json:"reason"`
	CreatedByID AccountID `json:"created_by_id"`
	CreatedAt   Time      `json:"created_at"`
	LiftedAt    Time      `json:"lifted_at,omitempty"`
}

// ThreadEntry is a row of the Threads view (schema ThreadEntry).
type ThreadEntry struct {
	ThreadState ThreadState `json:"thread_state"`
	Parent      Message     `json:"parent"`
}

// AdminAccount is an account with what admins see about it (schema
// AdminAccount). Never its sessions.
type AdminAccount struct {
	Account
	PasskeyCount int64     `json:"passkey_count"`
	HasAccessKey bool      `json:"has_access_key"`
	HasTotp      bool      `json:"has_totp"`
	ProviderIDs  []string  `json:"provider_ids"`
	LastSeenAt   Time      `json:"last_seen_at,omitempty"`
	ClaimedAtOut Time      `json:"claimed_at,omitempty"`
	RemovedByID  AccountID `json:"removed_by_id,omitempty"`
	Banned       bool      `json:"banned"`
	// MayApprove: the account holds the flag "may approve requests"
	// (docs/FEATURES.md section 31).
	MayApprove bool `json:"may_approve"`
	// Applying: the account waits for its registration to be approved.
	Applying bool `json:"applying,omitempty"`
}

// Totals are an account's numbers across its conversations: the `total` of
// badge.changed and read_position.moved, and the first four fields of the
// status endpoint (docs/PUSH.md section 11).
type Totals struct {
	Badge               int64 `json:"badge"`
	Mentions            int64 `json:"mentions"`
	UnreadConversations int64 `json:"unread_conversations"`
	UnreadThreads       int64 `json:"unread_threads"`
}

// StatusSummary is what the status endpoint returns (schema StatusSummary).
// It holds no clock time except the end of do not disturb, so it changes
// only when something changed.
type StatusSummary struct {
	Totals
	Newest        string `json:"newest,omitempty"`
	RingingCallID CallID `json:"ringing_call_id,omitempty"`
	Dnd           bool   `json:"dnd"`
	DndUntil      Time   `json:"dnd_until,omitempty"`
	// Broadcast is the server broadcast showing (ruling R74), so that whoever
	// connects while it shows sees it too.
	Broadcast *Broadcast `json:"broadcast,omitempty"`
}
