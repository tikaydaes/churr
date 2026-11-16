package model

// Event names of moderation, files, previews, emoji and channel management
// (docs/EVENTS.md). The ones the foundation uses are in event.go.
const (
	EventMessageHistoryRestored = "message.history_restored"
	EventPreviewAdded           = "preview.added"
	EventPreviewRemoved         = "preview.removed"
	EventThreadMoved            = "thread.moved"
	EventConversationArchived   = "conversation.archived"
	EventConversationUnarchived = "conversation.unarchived"
	EventTimeoutStarted         = "timeout.started"
	EventTimeoutLifted          = "timeout.lifted"
	EventReportCreated          = "report.created"
	EventReportHandled          = "report.handled"
	EventPurgeRequestCreated    = "purge_request.created"
	EventPurgeRequestDecided    = "purge_request.decided"
	EventSectionsChanged        = "sections.changed"
	// The server's reference patterns, or one channel's, were changed
	// (ruling R73).
	EventReferencePatternsChanged = "reference_patterns.changed"
	EventEmojiAdded               = "emoji.added"
	EventEmojiUpdated             = "emoji.updated"
	EventEmojiRemoved             = "emoji.removed"
)

// Limits of moderation and custom emoji (docs/FEATURES.md sections 15, 16,
// 18 and 22).
const (
	MinTimeoutSeconds  = 60
	MaxTimeoutSeconds  = 28 * 24 * 3600
	MinSlowModeSeconds = 5
	MaxSlowModeSeconds = 6 * 3600
	MaxReportReason    = 1000
	MaxEmojiBytes      = 256 << 10
	MaxEmojiPixels     = 256
	MaxPreviewsPerPost = 3
)

// The sources of a report.
const (
	ReportByPerson = "person"
	ReportByFilter = "filter"
)

// The outcomes of a report.
const (
	ReportDismissed = "dismissed"
	ReportDeleted   = "deleted"
	ReportTimedOut  = "timed_out"
)

// Report is a report of a message (schema Report). One the word filter made
// has no reporter and the source `filter` (ruling R27).
type Report struct {
	ID             string         `json:"id"`
	MessageID      MessageID      `json:"message_id"`
	ConversationID ConversationID `json:"conversation_id"`
	Source         string         `json:"source"`
	ReportedByID   AccountID      `json:"reported_by_id,omitempty"`
	Reason         string         `json:"reason"`
	CreatedAt      Time           `json:"created_at"`
	HandledByID    AccountID      `json:"handled_by_id,omitempty"`
	HandledAt      Time           `json:"handled_at,omitempty"`
	Outcome        string         `json:"outcome,omitempty"`
	Message        *Message       `json:"message,omitempty"`
}

// Waiting reports whether nobody has handled the report yet.
func (r Report) Waiting() bool { return r.Outcome == "" }

// The outcomes of a purge request.
const (
	PurgeShown  = "shown"
	PurgeHidden = "hidden"
	PurgeErased = "erased"
)

// PurgeRequest is an author's request to purge their own message in a public
// channel (schema PurgeRequest). Only admins are ever given the outcome.
type PurgeRequest struct {
	ID            string    `json:"id"`
	MessageID     MessageID `json:"message_id"`
	RequestedByID AccountID `json:"requested_by_id"`
	RequestedAt   Time      `json:"requested_at"`
	DecidedByID   AccountID `json:"decided_by_id,omitempty"`
	DecidedAt     Time      `json:"decided_at,omitempty"`
	Outcome       string    `json:"outcome,omitempty"`
	Message       *Message  `json:"message,omitempty"`
}

// Waiting reports whether no admin has decided the request yet.
func (p PurgeRequest) Waiting() bool { return p.Outcome == "" }

// The actions of a word filter.
const (
	FilterBlock = "block"
	FilterFlag  = "flag"
)

// WordFilter is a word or pattern and what a match does (schema WordFilter).
type WordFilter struct {
	ID          string    `json:"id"`
	Pattern     string    `json:"pattern"`
	IsRegex     bool      `json:"is_regex"`
	Action      string    `json:"action"`
	CreatedByID AccountID `json:"created_by_id"`
	CreatedAt   Time      `json:"created_at"`
}

// Emoji is a custom emoji (schema Emoji).
type Emoji struct {
	ID          EmojiID   `json:"id"`
	Name        string    `json:"name"`
	FileID      FileID    `json:"file_id"`
	URL         string    `json:"url"`
	CreatedByID AccountID `json:"created_by_id"`
	CreatedAt   Time      `json:"created_at"`
	// ServerName is set on an emoji that came with a shared channel.
	ServerName string `json:"server_name,omitempty"`
}

// AdminChannel is what the admin console shows about a named channel (schema
// AdminChannel). It never carries content, and for a private channel not
// even the topic.
type AdminChannel struct {
	ID               ConversationID `json:"id"`
	Name             string         `json:"name"`
	Private          bool           `json:"private"`
	Topic            string         `json:"topic,omitempty"`
	SectionID        SectionID      `json:"section_id,omitempty"`
	ParentID         ConversationID `json:"parent_id,omitempty"`
	RetentionSeconds *int64         `json:"retention_seconds,omitempty"`
	MemberCount      int64          `json:"member_count"`
	ManagerIDs       []AccountID    `json:"manager_ids"`
	CreatedAt        Time           `json:"created_at"`
	ArchivedAt       Time           `json:"archived_at,omitempty"`
	Shared           bool           `json:"shared,omitempty"`
	// Announcement: only its managers and those they allow post there
	// (ruling R66).
	Announcement bool `json:"announcement,omitempty"`
}

// File purposes (docs/SCHEMA.md, files).
const (
	PurposeAttachment = "attachment"
	PurposeCard       = "card"
	PurposeAvatar     = "avatar"
	PurposeEmoji      = "emoji"
)
