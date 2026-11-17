package model

// Notices (docs/FEATURES.md section 21.13): something done that concerns an
// account, kept for it until it is removed and shown in its clients with
// an unseen count, whether or not it was connected at the time. A notice
// is not a message and belongs to no conversation; only its holder reads
// it.

// The kinds of notice. Clients word each themselves.
const (
	// NoticeRecoveryLinkUsed: a recovery link was used to sign in to this
	// account; ByID made the link ("" for the command line).
	NoticeRecoveryLinkUsed = "recovery_link_used"
	// To every Owner: SubjectID was made an Owner by ByID, or stepped
	// down.
	NoticeOwnerMade        = "owner_made"
	NoticeOwnerSteppedDown = "owner_stepped_down"
	// To every Owner: ByID started the full export, or downloaded it.
	NoticeExportStarted    = "export_started"
	NoticeExportDownloaded = "export_downloaded"
	// To every Owner: ByID added SubjectID to ConversationID by override.
	NoticeOverrideUsed = "override_used"
)

// NoticeKinds are the kinds a notice may have.
var NoticeKinds = []string{NoticeRecoveryLinkUsed, NoticeOwnerMade, NoticeOwnerSteppedDown, NoticeExportStarted, NoticeExportDownloaded, NoticeOverrideUsed}

// EventNoticeCreated is sent to the account's connections when a notice is
// left for it, with the notice.
const EventNoticeCreated = "notice.created"

// Notice is one kept notice (schema Notice).
type Notice struct {
	ID             string         `json:"id"`
	AccountID      AccountID      `json:"-"`
	Kind           string         `json:"kind"`
	ByID           AccountID      `json:"by_id,omitempty"`
	SubjectID      AccountID      `json:"subject_id,omitempty"`
	ConversationID ConversationID `json:"conversation_id,omitempty"`
	CreatedAt      Time           `json:"created_at"`
	SeenAt         Time           `json:"seen_at,omitempty"`
}
