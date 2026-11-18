package modhost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/wirepb"
)

// Registry is the built-in modules a server runs, and the one way to call
// them: it finds the module of a command or a claimed kind, makes the Host
// for the call, and checks what comes back.
type Registry struct {
	mods   []*Module
	cmds   map[string]*Module
	claims []ClaimRule
	intn   func(n int) int
	store  Store
	// switches say where modules are off; nil is everywhere on.
	switches Switches
	// deadline is how long a call may take; failed hears of the calls
	// that panicked or ran out of it (guard.go).
	deadline time.Duration
	failed   func(ctx context.Context, f Failure)
}

// Options are how a registry is made.
type Options struct {
	// Intn backs every Host's randomness; left out, crypto/rand. Tests
	// that must know the outcome give one of their own.
	Intn func(n int) int
	// Store keeps the modules' values (host.go); left out, they keep
	// nothing.
	Store Store
	// Switches says where modules are switched off; left out, every
	// module is on everywhere.
	Switches Switches
	// Failed is told of every call into a module that panicked or gave no
	// answer within Deadline: the server logs and audits it. Its context
	// outlives the request.
	Failed func(ctx context.Context, f Failure)
	// Deadline replaces the Deadline of every call, for tests that cannot
	// wait two seconds.
	Deadline time.Duration
}

// ClaimRule is a claim as the reader of text applies it: the kind, its
// marks, and the pattern its inside must match in full.
type ClaimRule struct {
	Kind        string
	Open, Close string
	Inside      *regexp.Regexp
	// Module is the name of the module that claims the kind.
	Module string
	module *Module
}

// ListedCommand is a module's command as the command menu lists it.
type ListedCommand struct {
	Module string
	Command
}

// Made is a module's reply as the host checked it: the message's text, and
// its view with the level and the module's name stamped on.
type Made struct {
	Text string
	View *model.ValueEnvelope
}

var moduleName = regexp.MustCompile(`^[a-z0-9-]{2,32}$`)

// New checks the modules and registers them: each has a name and a handler;
// no two share a name, a command or a claimed kind; every command name is
// one a command may have; every claimed kind is in the catalogue, with marks
// and a pattern Go compiles.
func New(o Options, mods ...Module) (*Registry, error) {
	r := &Registry{cmds: map[string]*Module{}, intn: o.Intn, store: o.Store, switches: o.Switches, failed: o.Failed, deadline: o.Deadline}
	if r.deadline <= 0 {
		r.deadline = Deadline
	}
	names := map[string]bool{}
	kinds := map[string]bool{}
	for i := range mods {
		m := &mods[i]
		if !moduleName.MatchString(m.Name) || names[m.Name] {
			return nil, fmt.Errorf("modhost: a module named %q is not allowed or not alone", m.Name)
		}
		if m.Handler == nil {
			return nil, fmt.Errorf("modhost: module %s has no handler", m.Name)
		}
		names[m.Name] = true
		for _, c := range m.Commands {
			if !model.ValidCommandName(c.Name) || r.cmds[c.Name] != nil {
				return nil, fmt.Errorf("modhost: module %s: command %q is not allowed or not alone", m.Name, c.Name)
			}
			r.cmds[c.Name] = m
		}
		for _, c := range m.Claims {
			if _, ok := wirepb.KindLevel(c.Kind); !ok || kinds[c.Kind] {
				return nil, fmt.Errorf("modhost: module %s: the kind %q is not in the catalogue or is claimed twice", m.Name, c.Kind)
			}
			if c.Open == "" || c.Close == "" {
				return nil, fmt.Errorf("modhost: module %s: the claim of %s has no marks", m.Name, c.Kind)
			}
			re, err := regexp.Compile(`^(?:` + c.Pattern + `)$`)
			if err != nil {
				return nil, fmt.Errorf("modhost: module %s: the claim of %s: %w", m.Name, c.Kind, err)
			}
			kinds[c.Kind] = true
			r.claims = append(r.claims, ClaimRule{Kind: c.Kind, Open: c.Open, Close: c.Close, Inside: re, Module: m.Name, module: m})
		}
		r.mods = append(r.mods, m)
	}
	return r, nil
}

// Switches says which modules are switched off (docs/DESIGN-MODULES.md
// section 6.5): for the whole instance, by an admin, and in a conversation,
// by its managers. It is the server's store of them (store.Modules).
type Switches interface {
	Off(ctx context.Context, conv model.ConversationID) (instance, here []string, err error)
}

// Off is the set of modules switched off in a conversation, the instance's
// switches and the conversation's together ("" asks the instance's alone).
// An off module's commands are neither offered nor run there, the objects
// it claims are not resolved (their text stays text), and the values it
// made are served as their text.
func (r *Registry) Off(ctx context.Context, conv model.ConversationID) (map[string]bool, error) {
	out := map[string]bool{}
	if r == nil || r.switches == nil {
		return out, nil
	}
	instance, here, err := r.switches.Off(ctx, conv)
	for _, m := range append(instance, here...) {
		out[m] = true
	}
	return out, err
}

// List is the modules, in the order they were registered, as what they
// declare: the handlers are the host's to call.
func (r *Registry) List() []Module {
	if r == nil {
		return nil
	}
	out := make([]Module, len(r.mods))
	for i, m := range r.mods {
		out[i] = *m
		out[i].Handler = nil
	}
	return out
}

// Module is the module of a name, as List gives it.
func (r *Registry) Module(name string) (Module, bool) {
	for _, m := range r.List() {
		if m.Name == name {
			return m, true
		}
	}
	return Module{}, false
}

// Commands lists every module's commands, in the order they were
// registered.
func (r *Registry) Commands() []ListedCommand {
	var out []ListedCommand
	for _, m := range r.mods {
		for _, c := range m.Commands {
			out = append(out, ListedCommand{Module: m.Name, Command: c})
		}
	}
	return out
}

// HasCommand reports whether a module answers /name.
func (r *Registry) HasCommand(name string) bool { return r != nil && r.cmds[name] != nil }

// CommandModule is the name of the module that answers /name, or "".
func (r *Registry) CommandModule(name string) string {
	if !r.HasCommand(name) {
		return ""
	}
	return r.cmds[name].Name
}

// Command runs /name for an actor in a conversation, with what was typed
// after the name, and checks the reply. A module's refusal comes back as it
// gave it: the words are the module's, for the person who typed the
// command.
func (r *Registry) Command(ctx context.Context, actor model.Account, conv model.ConversationID, name, text string) (Made, error) {
	m := r.cmds[name]
	if m == nil {
		return Made{}, model.ErrNotFound.With("There is no command called /" + name + " here.")
	}
	if off, err := r.Off(ctx, conv); err != nil {
		return Made{}, err
	} else if off[m.Name] {
		return Made{}, model.ErrFeatureOff.With("The " + title(m) + " module is switched off here, so /" + name + " cannot run.")
	}
	var reply Reply
	err := r.call(ctx, m, actor, conv, "/"+name, func(ctx context.Context, h *Host) error {
		var err error
		reply, err = m.Handler.Command(ctx, h, Invocation{Command: name, Text: text})
		return err
	})
	if err != nil {
		return Made{}, err
	}
	if reply.Text == "" {
		return Made{}, fmt.Errorf("modhost: module %s answered /%s with no text", m.Name, name)
	}
	out := Made{Text: reply.Text}
	if reply.View != nil {
		v, err := r.envelope(m, *reply.View)
		if err != nil {
			return Made{}, err
		}
		out.View = &v
	}
	return out, nil
}

// Claims are the kinds the modules resolve on send, for the reader of text
// to find.
func (r *Registry) Claims() []ClaimRule {
	if r == nil {
		return nil
	}
	return r.claims
}

// claim finds the claim of a kind.
func (r *Registry) claim(kind string) (ClaimRule, bool) {
	for _, c := range r.Claims() {
		if c.Kind == kind {
			return c, true
		}
	}
	return ClaimRule{}, false
}

// Resolve gives the value of a marked object of a claimed kind, its inside
// being text, as its module makes it and the host checks it. false leaves
// the object as text.
func (r *Registry) Resolve(ctx context.Context, actor model.Account, conv model.ConversationID, kind, text string) (model.ValueEnvelope, bool) {
	c, ok := r.claim(kind)
	if !ok || !c.Inside.MatchString(text) {
		return model.ValueEnvelope{}, false
	}
	if off, err := r.Off(ctx, conv); err != nil || off[c.Module] {
		return model.ValueEnvelope{}, false
	}
	var v Value
	err := r.call(ctx, c.module, actor, conv, kind, func(ctx context.Context, h *Host) error {
		var err error
		v, err = c.module.Handler.Resolve(ctx, h, Marked{Kind: kind, Text: text})
		return err
	})
	if err != nil || v.Kind != kind {
		return model.ValueEnvelope{}, false
	}
	env, err := r.envelope(c.module, v)
	return env, err == nil
}

// Unresolved is the value of a marked object of a claimed kind that nothing
// was resolved for: one an edit wrote or changed.
func (r *Registry) Unresolved(kind string) (model.ValueEnvelope, bool) {
	c, ok := r.claim(kind)
	if !ok {
		return model.ValueEnvelope{}, false
	}
	level, _ := wirepb.KindLevel(kind)
	return model.ValueEnvelope{Kind: kind, Level: level, By: c.module.Name, Removed: true}, true
}

// envelope turns a module's value into the value that is stored: its data
// as the kind's JSON, held to the kind's schema (so every reader on either
// wire gets it in full) and to the kind's check, with the kind's level and
// the module's name.
func (r *Registry) envelope(m *Module, v Value) (model.ValueEnvelope, error) {
	level, ok := wirepb.KindLevel(v.Kind)
	if !ok {
		return model.ValueEnvelope{}, fmt.Errorf("modhost: module %s made a value of %q, which is not in the catalogue", m.Name, v.Kind)
	}
	raw, err := json.Marshal(v.Data)
	if err != nil {
		return model.ValueEnvelope{}, err
	}
	var data any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		return model.ValueEnvelope{}, err
	}
	if _, err := wirepb.EncodeKind(v.Kind, data); err != nil {
		return model.ValueEnvelope{}, fmt.Errorf("modhost: module %s: %w", m.Name, err)
	}
	env := &model.ValueEnvelope{Kind: v.Kind, Level: level, Data: raw, By: m.Name}
	if model.CheckValue(env) != env {
		return model.ValueEnvelope{}, fmt.Errorf("modhost: module %s made a value of %s that fails the kind's check", m.Name, v.Kind)
	}
	return *env, nil
}
