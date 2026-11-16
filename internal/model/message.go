package model

import (
	"fmt"
	"net/url"
	"strconv"
)

// Quote is what a reply quotes. No range means the whole message.
type Quote struct {
	MessageID MessageID `json:"message_id"`
	Start     *int      `json:"start,omitempty"` // UTF-16 code units into the quoted text
	End       *int      `json:"end,omitempty"`
}

// The entity types of docs/MESSAGE-FORMAT.md section 3.
const (
	EntityAccount      = "account"
	EntityGroup        = "group"
	EntityBroadcast    = "broadcast"
	EntityConversation = "conversation"
	EntityEmoji        = "emoji"
	EntityLink         = "link"
	EntityTask         = "task"
	EntityInstant      = "instant" // a typed date and time (ruling R69)
	// The objects of ruling R73 (docs/MESSAGE-FORMAT.md section 3.2).
	EntityReference = "reference" // a match of a reference pattern
)

// Entity is something the server resolved in a message's text. At and Len
// count UTF-16 code units, which is how JavaScript and Dart index strings.
type Entity struct {
	Type           string         `json:"type"`
	At             int            `json:"at"`
	Len            int            `json:"len"`
	AccountID      AccountID      `json:"account_id,omitempty"`
	GroupID        GroupID        `json:"group_id,omitempty"`
	Scope          string         `json:"scope,omitempty"` // channel | here
	ConversationID ConversationID `json:"conversation_id,omitempty"`
	EmojiID        EmojiID        `json:"emoji_id,omitempty"`
	URL            string         `json:"url,omitempty"`
	ItemID         string         `json:"item_id,omitempty"`
	// An instant's moment in UTC, the precision it was typed with
	// (minute or second), and its zone as written, or the author's zone
	// when none was (docs/MESSAGE-FORMAT.md section 3.1). `at` is taken
	// by the position, so the moment is `time`.
	Time      string `json:"time,omitempty"`
	Precision string `json:"precision,omitempty"`
	Zone      string `json:"zone,omitempty"`
	// A reference's pattern and the value matched; its address, as the
	// pattern gave it when the text was written, is URL.
	PatternID string `json:"pattern_id,omitempty"`
	Value     string `json:"value,omitempty"`
}

// Task is the state of one checklist item (schema Task).
type Task struct {
	ID          string    `json:"id"`
	Checked     bool      `json:"checked"`
	CheckedByID AccountID `json:"checked_by_id,omitempty"`
	CheckedAt   Time      `json:"checked_at,omitempty"`
}

// TaskChange is one tick or untick of a checklist item.
type TaskChange struct {
	ItemID    string    `json:"item_id"`
	AccountID AccountID `json:"account_id"`
	Checked   bool      `json:"checked"`
	At        Time      `json:"at"`
}

// The kinds of file, decided from content (docs/MESSAGE-FORMAT.md section 5).
const (
	FileImage = "image"
	FileVideo = "video"
	FileAudio = "audio"
	FileText  = "text"
	FileOther = "file"
)

// File is an uploaded file (schema File), with the stored columns behind it.
type File struct {
	ID              FileID `json:"id"`
	Name            string `json:"name"`
	Size            int64  `json:"size"`
	MediaType       string `json:"media_type"`
	Kind            string `json:"kind"`
	Width           int    `json:"width,omitempty"`
	Height          int    `json:"height,omitempty"`
	DurationSeconds int    `json:"duration_seconds,omitempty"`
	Caption         string `json:"caption,omitempty"`
	PreviewText     string `json:"preview_text,omitempty"`
	URL             string `json:"url,omitempty"`
	ThumbURL        string `json:"thumb_url,omitempty"`
	PosterURL       string `json:"poster_url,omitempty"`
	// State is sent only when it is not `ready`.
	State string `json:"state,omitempty"`

	// Stored only.
	ConversationID ConversationID `json:"-"`
	MessageID      MessageID      `json:"-"`
	UploaderID     AccountID      `json:"-"`
	Purpose        string         `json:"-"` // attachment | card | avatar | emoji
	Position       int            `json:"-"`
	SHA256         []byte         `json:"-"`
	BlobKey        string         `json:"-"`
	PosterKey      string         `json:"-"`
	ThumbKey       string         `json:"-"`
	DetachedAt     Time           `json:"-"`
	CreatedAt      Time           `json:"-"`
}

// FillURLs sets the download addresses from the stored keys. Only a file
// whose content is here has a url.
func (f *File) FillURLs() {
	f.URL, f.ThumbURL, f.PosterURL = "", "", ""
	if f.BlobKey == "" {
		return
	}
	base := "/api/v1/files/" + string(f.ID)
	f.URL = base + "/" + url.PathEscape(f.Name)
	if f.ThumbKey != "" {
		f.ThumbURL = base + "/thumb"
	}
	if f.PosterKey != "" {
		f.PosterURL = base + "/poster"
	}
}

// CardField is a label and value on a card.
type CardField struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Short bool   `json:"short,omitempty"`
}

// CardAuthor is the small line above a card's title.
type CardAuthor struct {
	Name       string `json:"name"`
	IconFileID FileID `json:"icon_file_id,omitempty"`
}

// Card is a fixed layout for structured information (schema Card).
type Card struct {
	Color       string      `json:"color,omitempty"`
	Author      *CardAuthor `json:"author,omitempty"`
	Title       string      `json:"title,omitempty"`
	URL         string      `json:"url,omitempty"`
	Text        string      `json:"text,omitempty"`
	Fields      []CardField `json:"fields,omitempty"`
	ImageFileID FileID      `json:"image_file_id,omitempty"`
	Footer      string      `json:"footer,omitempty"`
	At          Time        `json:"at,omitempty"`
	PreviewURL  string      `json:"preview_url,omitempty"`
	// Theme is set on the card the server attaches for a link to one of
	// its own themes (a theme card); such a card has a PreviewURL too.
	Theme *CardTheme `json:"theme,omitempty"`
}

// CardColors are the colours a card may have.
var CardColors = []string{"lavender", "cyan", "pink", "yellow", "green", "red", "grey"}

// Reaction is one emoji on a message with the accounts behind it (schema
// Reaction). AccountIDs holds at most the first ten.
type Reaction struct {
	Emoji      string      `json:"emoji"`
	Count      int         `json:"count"`
	Me         bool        `json:"me"`
	AccountIDs []AccountID `json:"account_ids"`
}

// ThreadSummary is the thread information on a top-level message that has
// replies.
type ThreadSummary struct {
	ReplyCount     int         `json:"reply_count"`
	LastReplySeq   int64       `json:"last_reply_seq"`
	LastReplyOrder string      `json:"last_reply_order"` // of the reply at LastReplySeq: where the thread's marker goes
	LastReplyAt    Time        `json:"last_reply_at"`
	ParticipantIDs []AccountID `json:"participant_ids"`
}

// Via says how a message was posted, if not typed by its author.
type Via struct {
	Kind       string    `json:"kind"` // webhook | reminder | scheduled | command | import | system
	Name       string    `json:"name,omitempty"`
	AccountID  AccountID `json:"account_id,omitempty"`
	IconFileID FileID    `json:"icon_file_id,omitempty"`
}

// SealedPayload is the content of a sealed-chat message, which only the two
// people's devices can open.
type SealedPayload struct {
	KeyEpoch   int64  `json:"key_epoch"`
	Ciphertext []byte `json:"ciphertext"`
}

// Message is one post in a conversation, in the shape
// docs/MESSAGE-FORMAT.md defines, with the stored columns that are not sent.
//
// Action marks text typed with /me: it is drawn after the author's name.
// Chance is a result the server drew for /roll, /flip, /pick, /shuffle or
// /draw, which no edit changes (ruling R67).
type Message struct {
	ID             MessageID      `json:"id"`
	ConversationID ConversationID `json:"conversation_id"`
	Seq            int64          `json:"seq,omitempty"`
	AuthorID       AccountID      `json:"author_id"`
	// ClientID is sent only in a sealed chat. For any other conversation
	// the service empties it before the message leaves the server; it is
	// the author's own business, returned to them beside the message.
	ClientID       string         `json:"client_id,omitempty"`
	ParentID       MessageID      `json:"parent_id,omitempty"`
	Quote          *Quote         `json:"quote,omitempty"`
	Text           string         `json:"text,omitempty"`
	Entities       []Entity       `json:"entities,omitempty"`
	Tasks          []Task         `json:"tasks,omitempty"`
	Files          []File         `json:"files,omitempty"`
	Cards          []Card         `json:"cards,omitempty"`
	Form           *Form          `json:"form,omitempty"`
	Action         bool           `json:"action,omitempty"`
	Chance         *Chance        `json:"chance,omitempty"`
	Fallback       string         `json:"fallback,omitempty"`
	Reactions      []Reaction     `json:"reactions,omitempty"`
	Thread         *ThreadSummary `json:"thread,omitempty"`
	Via            *Via           `json:"via,omitempty"`
	System         map[string]any `json:"system,omitempty"`
	Sealed         *SealedPayload `json:"sealed,omitempty"`
	Quiet          bool           `json:"quiet,omitempty"`
	Pinned         bool           `json:"pinned,omitempty"`
	NotifiedAnyway bool           `json:"notified_anyway,omitempty"`
	HasHistory     bool           `json:"has_history,omitempty"`
	// Unsent: in a conversation shared with other servers, the link has
	// not yet acknowledged this message. It is shown in its place all the
	// same.
	Unsent bool `json:"unsent,omitempty"`
	// Order says where the message is shown: clients sort a conversation by
	// it, as plain text. In a conversation that is not shared it follows
	// Seq. In one that is, it is made from the stamp of the event that
	// created the message, so it is the same on every server (ruling R57).
	Order string `json:"order"`
	// Late: the message arrived here after messages ordered later than it
	// had already been delivered. It is unread like any new message.
	Late        bool      `json:"late,omitempty"`
	CreatedAt   Time      `json:"created_at"`
	EditedAt    Time      `json:"edited_at,omitempty"`
	DeletedAt   Time      `json:"deleted_at,omitempty"`
	DeletedByID AccountID `json:"deleted_by_id,omitempty"`

	// Stored only.
	// SearchText is what the search index holds for the message: its plain
	// text with each channel link written as the channel's name at the
	// moment it was stored, then the fallback. Empty for sealed and
	// deleted messages. Snippets are cut from it.
	SearchText      string `json:"-"`
	Version         int    `json:"-"`
	HistoryHiddenAt Time   `json:"-"`
	PurgedAt        Time   `json:"-"`
	ReplyCount      int    `json:"-"` // the thread summary columns, turned into Thread when shown
	LastReplySeq    int64  `json:"-"`
	LastReplyAt     Time   `json:"-"`
	LastReplyOrder  string `json:"-"`
	// EditOrder is, in a shared conversation, the order string of the event
	// that last set the message's content: of two edits the later stands.
	EditOrder string `json:"-"`
}

// OrderString is the text clients sort by for an event stamped (at, origin,
// n): the time of the conversation's reference clock in milliseconds, the
// name of the server that made the event, and that server's counter. Texts
// made by it compare, byte by byte, as the three compare in turn. The
// separator sorts before every character a server's name can hold, so a
// name that begins another sorts first.
func OrderString(at int64, origin string, n int64) string {
	return fmt.Sprintf("%016x!%s!%016x", at, origin, n)
}

// LocalOrder is the order string of a message that no stamp places: it
// follows seq. floor is 0 in a conversation that was never shared; in one
// that was and no longer is, it is one more than the highest `at` the
// conversation held, so what is posted now comes after all of that.
func LocalOrder(floor, seq int64) string { return OrderString(floor, "", seq) }

// OrderAt reads the time back out of an order string, 0 if it is not one.
func OrderAt(order string) int64 {
	if len(order) < 16 {
		return 0
	}
	at, err := strconv.ParseInt(order[:16], 16, 64)
	if err != nil {
		return 0
	}
	return at
}

// Deleted reports whether the message has been deleted.
func (m Message) Deleted() bool { return !m.DeletedAt.IsZero() }

// IsReply reports whether the message hangs under a parent.
func (m Message) IsReply() bool { return m.ParentID != "" }

// IsSystem reports whether the server posted the message itself.
func (m Message) IsSystem() bool { return m.System != nil }

// MessageVersion is an earlier version of a message, or the last content of
// a deleted one (schema MessageVersion).
type MessageVersion struct {
	Version      int       `json:"version"`
	Text         string    `json:"text"`
	Entities     []Entity  `json:"entities"`
	Cards        []Card    `json:"cards"`
	Files        []File    `json:"files"`
	ReplacedAt   Time      `json:"replaced_at"`
	ReplacedByID AccountID `json:"replaced_by_id"`
	Reason       string    `json:"reason"` // edit | delete

	FileIDs []FileID `json:"-"`
}

// The two thread states. No row means "neither".
const (
	ThreadFollowing = "following"
	ThreadIgnored   = "ignored"
)

// ThreadState is one account's state for one thread (schema ThreadState).
type ThreadState struct {
	ParentID       MessageID      `json:"parent_id"`
	ConversationID ConversationID `json:"conversation_id"`
	State          string         `json:"state"`
	ReadSeq        int64          `json:"read_seq"`
	UnreadCount    int64          `json:"unread_count"`

	AccountID AccountID `json:"-"`
	UpdatedAt Time      `json:"-"`
}

// Mention kinds, in the order that decides which one a row keeps when more
// than one applies (docs/SCHEMA.md, mentions).
const (
	MentionAccount = "account"
	MentionGroup   = "group"
	MentionChannel = "channel"
	MentionHere    = "here"
)

// Mention is one account a message mentioned and that could be told.
type Mention struct {
	MessageID MessageID
	AccountID AccountID
	Kind      string
}

// Pin is a pinned message (schema Pin).
type Pin struct {
	MessageID  MessageID `json:"message_id"`
	PinnedByID AccountID `json:"pinned_by_id"`
	PinnedAt   Time      `json:"pinned_at"`
	Message    *Message  `json:"message,omitempty"`
}
