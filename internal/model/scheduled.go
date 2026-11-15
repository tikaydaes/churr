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
	// Action and Chance: see Message (ruling R67). A chance result is
	// drawn when the message is posted.
	Action bool         `json:"action,omitempty"`
	Chance *ChanceInput `json:"chance,omitempty"`
	// TimeZone and InstantHints say what the dates and times in the text
	// mean (ruling R69): the author's IANA zone, and phrases the author
	// made into instants.
	TimeZone     string        `json:"time_zone,omitempty"`
	InstantHints []InstantHint `json:"instant_hints,omitempty"`
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
// (docs/FEATURES.md section 12.3, ruling R67), as the command menu shows
// them. Clients carry them out; the server only lists them and keeps their
// names from being taken (ReservedCommandName, which also knows the names
// held back). testdata/commands/commands.json is the same table, which the
// clients hold too; a test keeps the two equal.
var BuiltinCommands = []Command{
	{Name: "poll", Kind: CommandBuiltin, Description: "Posts a poll", Hint: "Question? | Option | Option … [flags]"},
	{Name: "rsvp", Kind: CommandBuiltin, Description: "Posts an RSVP", Hint: "What | When"},
	{Name: "signup", Kind: CommandBuiltin, Description: "Posts a sign-up sheet", Hint: "What | Slot | Slot … [flags]"},
	{Name: "approve", Kind: CommandBuiltin, Description: "Posts an approval", Hint: "What [| @group] [flags]"},
	{Name: "form", Kind: CommandBuiltin, Description: "Opens the form builder"},
	{Name: "remind", Kind: CommandBuiltin, Description: "Sets a reminder", Hint: "[target] when | what", Sealed: true},
	{Name: "me", Kind: CommandBuiltin, Description: "Says what you are doing, after your name", Hint: "what you are doing", Sealed: true},
	{Name: "shrug", Kind: CommandBuiltin, Description: "Adds ¯\\_(ツ)_/¯ to the text", Hint: "[text]", Sealed: true},
	{Name: "tableflip", Kind: CommandBuiltin, Description: "Adds (╯°□°)╯︵ ┻━┻ to the text", Hint: "[text]", Sealed: true},
	{Name: "unflip", Kind: CommandBuiltin, Description: "Adds ┬─┬ノ( º _ ºノ) to the text", Hint: "[text]", Sealed: true},
	{Name: "spoiler", Kind: CommandBuiltin, Description: "Hides the text until the reader selects it", Hint: "text", Sealed: true},
	{Name: "roll", Kind: CommandBuiltin, Description: "Rolls dice, rolled by the server: d20, 2d6+3, 4d6kh3, adv", Hint: "[dice]"},
	{Name: "flip", Kind: CommandBuiltin, Description: "Flips a coin, flipped by the server"},
	{Name: "pick", Kind: CommandBuiltin, Description: "Picks one, picked by the server", Hint: "a | b | c"},
	{Name: "shuffle", Kind: CommandBuiltin, Description: "Shuffles them, shuffled by the server", Hint: "a | b | c"},
	{Name: "draw", Kind: CommandBuiltin, Description: "Draws cards, drawn by the server: standard, jokers, tarot, hanafuda and other decks", Hint: "[count] [deck]"},
	{Name: "tarot", Kind: CommandBuiltin, Description: "Reads tarot cards, drawn by the server: three, celtic, horseshoe, relationship", Hint: "[spread]"},
	{Name: "mahjongg", Kind: CommandBuiltin, Description: "Draws mahjong tiles, drawn by the server", Hint: "[count]"},
	{Name: "mahjong", Kind: CommandBuiltin, Description: "Draws mahjong tiles, drawn by the server (as /mahjongg)", Hint: "[count]"},
	{Name: "join", Kind: CommandBuiltin, Description: "Joins a channel", Hint: "#channel", Sealed: true},
	{Name: "leave", Kind: CommandBuiltin, Description: "Leaves this channel, or the one named", Hint: "[#channel]"},
	{Name: "part", Kind: CommandBuiltin, Description: "Leaves this channel, or the one named (as /leave)", Hint: "[#channel]"},
	{Name: "msg", Kind: CommandBuiltin, Description: "Opens your chat with someone and sends them the text", Hint: "@person [text]", Sealed: true},
	{Name: "topic", Kind: CommandBuiltin, Description: "Shows the channel's topic, or sets it", Hint: "[text]"},
	{Name: "invite", Kind: CommandBuiltin, Description: "Adds someone to this channel, or the one named", Hint: "@person [#channel]"},
	{Name: "kick", Kind: CommandBuiltin, Description: "Removes someone from this channel", Hint: "@person"},
	{Name: "ban", Kind: CommandBuiltin, Description: "Bans someone from the server (admins)", Hint: "@person [reason]"},
	{Name: "mute", Kind: CommandBuiltin, Description: "Stops someone posting here for a time (default 1h)", Hint: "@person [duration]"},
	{Name: "op", Kind: CommandBuiltin, Description: "Makes someone a manager of this channel", Hint: "@person"},
	{Name: "deop", Kind: CommandBuiltin, Description: "Makes a manager of this channel a member", Hint: "@person"},
	{Name: "names", Kind: CommandBuiltin, Description: "Lists who is in this conversation"},
	{Name: "list", Kind: CommandBuiltin, Description: "Browses the channels", Sealed: true},
	{Name: "whois", Kind: CommandBuiltin, Description: "Shows someone's profile", Hint: "@person", Sealed: true},
	{Name: "ignore", Kind: CommandBuiltin, Description: "Blocks someone: their messages are collapsed for you", Hint: "@person", Sealed: true},
	{Name: "nick", Kind: CommandBuiltin, Description: "Changes your display name", Hint: "name", Sealed: true},
	{Name: "away", Kind: CommandBuiltin, Description: "Appears away while you are active", Hint: "[duration]", Sealed: true},
	{Name: "back", Kind: CommandBuiltin, Description: "Stops appearing away", Sealed: true},
	{Name: "dnd", Kind: CommandBuiltin, Description: "Turns do not disturb on, or off", Hint: "[duration | off]", Sealed: true},
	{Name: "quiet", Kind: CommandBuiltin, Description: "Mutes this conversation for a time (default 1h), or unmutes it", Hint: "[duration | off]", Sealed: true},
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

	// Sealed: a built-in offered in a sealed chat too.
	Sealed bool `json:"-"`
}

// heldBackCommands are names kept free so commands can be added later
// without breaking anyone's (decision F17).
var heldBackCommands = map[string]bool{
	"help": true, "later": true, "status": true,
}

// BuiltinCommand reports whether name is one of the built-in commands.
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
