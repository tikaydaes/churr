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
