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
