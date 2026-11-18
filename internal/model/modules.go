package model

// The store of the built-in modules (docs/DESIGN-MODULES.md section 6.2): one
// table of keys and bytes, kept apart per module and per scope. A module
// never names the conversation or account of a value; the host fills them
// in from the call, so a module reaches only where it was called.

// ModuleScope is how widely a module's value is kept.
type ModuleScope string

// The scopes of a module's values.
const (
	// ModuleScopeInstance is one value for the whole instance.
	ModuleScopeInstance ModuleScope = "instance"
	// ModuleScopeConversation is one value per conversation; it goes with
	// the conversation.
	ModuleScopeConversation ModuleScope = "conversation"
	// ModuleScopeAccount is one value per person; it goes when the
	// account is removed.
	ModuleScopeAccount ModuleScope = "account"
)

// ModuleKey names one value in a module's store. Conversation is set for
// ModuleScopeConversation and Account for ModuleScopeAccount; neither for
// ModuleScopeInstance.
type ModuleKey struct {
	Module       string
	Scope        ModuleScope
	Conversation ConversationID
	Account      AccountID
	Key          string
}

// BuiltinModule is a built-in module as the admin console and a channel's
// settings list it (schema Module): what it declares, and whether it is on.
type BuiltinModule struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	// On is whether it is on where it was asked about: the instance, or a
	// conversation.
	On bool `json:"on"`
	// ServerOn is whether the instance has it on; a channel cannot switch
	// on what the instance has off.
	ServerOn bool            `json:"server_on"`
	Commands []ModuleCommand `json:"commands"`
	// Claims are the kinds it resolves when they are sent.
	Claims []string `json:"claims"`
}

// ModuleCommand is a command a module declares (schema ModuleCommand).
type ModuleCommand struct {
	Name    string `json:"name"`
	Hint    string `json:"hint,omitempty"`
	Summary string `json:"summary"`
}

// CommandForm is one way a command may be typed (schema CommandForm;
// docs/DESIGN-MODULES.md section 5.1): its slots in order. A command with no
// arguments has a form with none.
type CommandForm struct {
	Slots []CommandSlot `json:"slots"`
}

// CommandSlot is one argument of a form (schema CommandSlot): its name, a
// hint for the person typing, and its kind: `text`, which takes the rest of
// the line, a kind of the catalogue, or a type the module declares.
type CommandSlot struct {
	Name string `json:"name"`
	Hint string `json:"hint,omitempty"`
	Kind string `json:"kind"`
}

// CommandHelp is what /help says of a module's command (schema
// CommandHelp), in the shape of testdata/commands/help.json's entries. A
// module writes its command's name as {cmd}, and another of its commands as
// {cmd:name}; the server writes in the names they have here.
type CommandHelp struct {
	Description string           `json:"description"`
	Args        []CommandHelpArg `json:"args,omitempty"`
	Examples    []CommandExample `json:"examples,omitempty"`
	Where       string           `json:"where,omitempty"`
	Who         string           `json:"who,omitempty"`
}

// CommandHelpArg is one argument's help.
type CommandHelpArg struct {
	Name  string `json:"name"`
	About string `json:"about"`
}

// CommandExample is something to type and what it does.
type CommandExample struct {
	Type string `json:"type"`
	Then string `json:"then"`
}

// The sources of commands, in the order the admin puts them
// (CommandSettings.Order).
const (
	SourceSavedReply = "saved_reply"
	SourceBot        = "bot"
	SourceModule     = "module"
	SourceBuiltin    = "builtin"
)

// DefaultCommandOrder is the order of the sources before an admin changes
// it: a person's saved replies and the custom commands, then bots'
// commands, then the modules', then the built-ins.
var DefaultCommandOrder = []string{SourceSavedReply, SourceBot, SourceModule, SourceBuiltin}

// CommandSettings is the admin's command table (schema CommandSettings;
// docs/DESIGN-MODULES.md section 6.5): which source of a name wins where
// two have it, and the names given to modules' commands. A protected
// built-in always wins, whatever the order.
type CommandSettings struct {
	Order   []string        `json:"order"`
	Renames []CommandRename `json:"renames"`
}

// CommandRename gives a module's command another name on this instance.
type CommandRename struct {
	Module  string `json:"module"`
	Command string `json:"command"`
	Name    string `json:"name"`
}
