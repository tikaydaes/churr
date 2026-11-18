// Package modhost is the host of the built-in modules (docs/DESIGN-MODULES.md
// section 6.2): our own reviewed Go, compiled in, that answers commands and
// resolves the objects it claims with values of the catalogue. A module sees
// only what this package hands it: the contract below, the Host's narrow
// toolkit and the model's types. It reaches no database, no socket and no
// other feature; the server registers the modules and connects the host to
// posting.
//
// The host does what a module cannot be trusted to do for itself, though the
// code is ours: it refuses an answer with no text, stamps each value with its
// kind's level and the module's name, and holds every value to the kind's
// schema and check before anything is stored.
package modhost

import (
	"context"
)

// Module is one built-in module: what it declares, and its handler.
type Module struct {
	// Name is the module's own name, [a-z0-9-]{2,32}; it is the `by` of
	// every value it makes.
	Name  string
	Title string
	// Commands are the slash commands it answers.
	Commands []Command
	// Claims are the kinds it resolves when a marked object of that kind
	// is sent (inline dice).
	Claims  []Claim
	Handler Handler
}

// Command is a slash command a module answers.
type Command struct {
	// Name is typed after the slash; Hint is what follows it, as the menu
	// shows it ("[dice]"); Summary is the menu's one line.
	Name    string
	Hint    string
	Summary string
}

// Claim is a kind a module resolves on send: text marked by Open, then a
// run that Pattern matches whole, then Close, standing on the edges of an
// object. The span's characters stay the text; the module gives its value.
type Claim struct {
	Kind        string
	Open, Close string
	// Pattern is in Go's syntax and matches the inside, between Open and
	// Close, in full.
	Pattern string
}

// Handler is a module's code. A module that declares commands answers
// Command; one that declares claims answers Resolve. Further entry points
// (a press on a card the module made, a timer) are interfaces of their
// own, which a handler implements when it needs them, so a module that
// has no use for them never changes.
type Handler interface {
	// Command is called when one of its commands is invoked.
	Command(ctx context.Context, h *Host, in Invocation) (Reply, error)
	// Resolve is called when a marked object of a kind it claims is sent.
	// An error leaves the span as text, with no message to anyone.
	Resolve(ctx context.Context, h *Host, in Marked) (Value, error)
}

// Invocation is a command as it was invoked: the name it was typed with and
// everything typed after it.
type Invocation struct {
	Command string
	Text    string
}

// Marked is a marked object of a kind the module claims: the kind and the
// characters between the claim's Open and Close.
type Marked struct {
	Kind string
	Text string
}

// Reply is a module's answer to a command: a message whose text is the
// message, in words, and whose view, when there is one, is how a client that
// knows the kind draws it. A reply with no text is refused.
type Reply struct {
	Text string
	View *Value
}

// Value is a value of the catalogue as a module makes it: its kind and the
// kind's data, which the host turns into JSON and holds to the kind's
// schema and check. The level and the module's name are the host's to add.
type Value struct {
	Kind string
	Data any
}
