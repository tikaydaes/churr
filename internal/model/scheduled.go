package model

// This file holds the types of forms' answers, scheduled items, saved
// replies and commands, and the event names that go with them.

// Event names of forms and scheduled items (docs/EVENTS.md sections 8, 9).
const (
	EventFormAnswered        = "form.answered"
	EventFormAnswerWithdrawn = "form.answer_withdrawn"
	EventFormOptionAdded     = "form.option_added"
	EventFormClosed          = "form.closed"
	EventScheduledCreated    = "scheduled.created"
	EventScheduledUpdated    = "scheduled.updated"
	EventScheduledCancelled  = "scheduled.cancelled"
	EventScheduledFired      = "scheduled.fired"
)

// The form actions (docs/MESSAGE-FORMAT.md section 7).
const (
	FormTally   = "tally"
	FormCollect = "collect"
	FormThread  = "thread"
	FormChannel = "channel"
	FormGroup   = "group"
	FormBot     = "bot"
)

// The values of a form's `results` setting.
const (
	ResultsLive        = "live"
	ResultsAfterAnswer = "after_answer"
	ResultsAfterClose  = "after_close"
)

// ButtonField is the field id a button press is tallied under. No field may
// use it as its id.
const ButtonField = "_button"

// FormAnswer is one stored answer to a form (schema FormAnswer). AccountID
// is emptied before an answer to an anonymous form leaves the server.
type FormAnswer struct {
	ID        string         `json:"id"`
	AccountID AccountID      `json:"account_id,omitempty"`
	ButtonID  string         `json:"button_id,omitempty"`
	Answers   map[string]any `json:"answers"`
	CreatedAt Time           `json:"created_at"`
	UpdatedAt Time           `json:"updated_at,omitempty"`

	// Stored only.
	FormID          FormID    `json:"-"`
	ResultMessageID MessageID `json:"-"`
}

// MessageInput is a message as a client sends it (schema MessageInput),
// which is also the shape a scheduled item keeps its content in. The sealed
// fields are not here: nothing sealed can be scheduled.
type MessageInput struct {
	ClientID string     `json:"client_id,omitempty"`
	Text     string     `json:"text,omitempty"`
	FileIDs  []FileID   `json:"file_ids,omitempty"`
	ParentID MessageID  `json:"parent_id,omitempty"`
	Quote    *Quote     `json:"quote,omitempty"`
	Cards    []Card     `json:"cards,omitempty"`
	Form     *FormInput `json:"form,omitempty"`
}

// The kinds of scheduled item.
const (
	ScheduledMessage  = "message"
	ScheduledReminder = "reminder"
	ScheduledForm     = "form"
)

// MaxWaitingScheduled is how many waiting scheduled items one account may
// have (docs/FEATURES.md decision F13).
const MaxWaitingScheduled = 200

// Repeat says when a scheduled item runs again (schema Repeat).
type Repeat struct {
	Every    string `json:"every"`          // day | week | month
	Days     string `json:"days,omitempty"` // for day: seven of 0 and 1, Monday first
	At       string `json:"at"`             // HH:MM
	TimeZone string `json:"time_zone"`      // IANA name

	// Anchor is what the first run fixed and the contract does not carry:
	// the weekday (1 Monday to 7 Sunday) for `week`, the day of the month
	// for `month`. It is stored with the rule and never sent.
	Anchor int `json:"-"`
}

// ScheduledItem is a message to send later, a reminder or a recurring form
// (schema ScheduledItem).
type ScheduledItem struct {
	ID             string         `json:"id"`
	AccountID      AccountID      `json:"account_id"`
	ConversationID ConversationID `json:"conversation_id"`
	Kind           string         `json:"kind"`
	Content        MessageInput   `json:"content"`
	AboutMessageID MessageID      `json:"about_message_id,omitempty"`
	NextRunAt      Time           `json:"next_run_at,omitempty"`
	Repeat         *Repeat        `json:"repeat,omitempty"`
	LastRunAt      Time           `json:"last_run_at,omitempty"`
	FailedAt       Time           `json:"failed_at,omitempty"`
	// Failure is the error code of the failed run. The sentence that went
	// with it is stored beside it and sent in scheduled.fired.
	Failure   string `json:"failure,omitempty"`
	CreatedAt Time   `json:"created_at"`

	// Stored only.
	FailureMessage string `json:"-"`
	CancelledAt    Time   `json:"-"`
}

// Waiting reports whether the item will still run.
func (s ScheduledItem) Waiting() bool { return s.CancelledAt.IsZero() && !s.NextRunAt.IsZero() }

// SavedReply is a `/name` command that posts preset text (schema
// SavedReply). OwnerID is empty for a community one.
type SavedReply struct {
	ID        string `json:"id"`
	Community bool   `json:"community"`
	Name      string `json:"name"`
	Text      string `json:"text"`
	CreatedAt Time   `json:"created_at"`

	OwnerID AccountID `json:"-"`
}

// BuiltinCommands is the full list of built-in slash commands
// (docs/FEATURES.md section 12.3), as the command menu shows them. Clients
// carry them out; the server only lists them and keeps their names from
// being taken (ReservedCommandName, which also knows the names held back).
var BuiltinCommands = []Command{
	{Name: "poll", Kind: CommandBuiltin, Description: "Posts a poll", Hint: "Question? | Option | Option … [flags]"},
	{Name: "rsvp", Kind: CommandBuiltin, Description: "Posts an RSVP", Hint: "What | When"},
	{Name: "signup", Kind: CommandBuiltin, Description: "Posts a sign-up sheet", Hint: "What | Slot | Slot … [flags]"},
	{Name: "approve", Kind: CommandBuiltin, Description: "Posts an approval", Hint: "What [| @group] [flags]"},
	{Name: "form", Kind: CommandBuiltin, Description: "Opens the form builder"},
	{Name: "remind", Kind: CommandBuiltin, Description: "Sets a reminder", Hint: "[target] when | what"},
}

// Dialog is a form a bot shows one person (schema Dialog). It is not a
// message and nobody else sees it.
type Dialog struct {
	ID             FormID         `json:"id"`
	BotID          AccountID      `json:"bot_id"`
	ConversationID ConversationID `json:"conversation_id"`
	Title          string         `json:"title,omitempty"`
	Fields         []FormField    `json:"fields"`
	Buttons        []FormButton   `json:"buttons,omitempty"`
}

// The kinds of command.
const (
	CommandBuiltin    = "builtin"
	CommandSavedReply = "saved_reply"
	CommandBot        = "bot"
)

// Command is something that can be typed as `/name` in a conversation
// (schema Command).
type Command struct {
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	Description  string    `json:"description"`
	Hint         string    `json:"hint,omitempty"`
	BotID        AccountID `json:"bot_id,omitempty"`
	SavedReplyID string    `json:"saved_reply_id,omitempty"`
}

// heldBackCommands are names kept free so commands can be added later
// without breaking anyone's (decision F17).
var heldBackCommands = map[string]bool{
	"help": true, "later": true, "status": true, "dnd": true, "away": true,
	"mute": true, "topic": true, "invite": true, "join": true, "leave": true,
}

// BuiltinCommand reports whether name is one of the six built-in commands.
func BuiltinCommand(name string) bool {
	for _, c := range BuiltinCommands {
		if c.Name == name {
			return true
		}
	}
	return false
}

// ReservedCommandName reports whether a saved reply, custom command or bot
// command may not have this name: the built-ins and the names held back.
func ReservedCommandName(name string) bool { return BuiltinCommand(name) || heldBackCommands[name] }

// FormSubmission is an answer or a button press on a form with the `bot`
// action, or on a dialog a bot opened, as the forms package hands it to the
// bots package to deliver. The answers have already been checked against
// the form.
type FormSubmission struct {
	// BotID is the bot that posted the form or opened the dialog.
	BotID AccountID
	// AccountID is who answered.
	AccountID AccountID
	// ConversationID and MessageID say where the form is; both are empty
	// for a dialog. ParentID is the thread the form's message is in.
	ConversationID ConversationID
	MessageID      MessageID
	ParentID       MessageID
	FormID         FormID
	// ButtonID is the button pressed, or "".
	ButtonID string
	Answers  map[string]any
	// Dialog is true for a dialog's answer.
	Dialog bool
}
