package modhost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"

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
}

// Options are how a registry is made.
type Options struct {
	// Intn backs every Host's randomness; left out, crypto/rand. Tests
	// that must know the outcome give one of their own.
	Intn func(n int) int
}

// ClaimRule is a claim as the reader of text applies it: the kind, its
// marks, and the pattern its inside must match in full.
type ClaimRule struct {
	Kind        string
	Open, Close string
	Inside      *regexp.Regexp
	module      *Module
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
	r := &Registry{cmds: map[string]*Module{}, intn: o.Intn}
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
			r.claims = append(r.claims, ClaimRule{Kind: c.Kind, Open: c.Open, Close: c.Close, Inside: re, module: m})
		}
		r.mods = append(r.mods, m)
	}
	return r, nil
}

// host makes the Host of one call.
func (r *Registry) host(actor model.Account, conv model.ConversationID) *Host {
	return NewHost(HostOptions{Actor: actor, Conversation: conv, Intn: r.intn})
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

// Command runs /name for an actor in a conversation, with what was typed
// after the name, and checks the reply. A module's refusal comes back as it
// gave it: the words are the module's, for the person who typed the
// command.
func (r *Registry) Command(ctx context.Context, actor model.Account, conv model.ConversationID, name, text string) (Made, error) {
	m := r.cmds[name]
	if m == nil {
		return Made{}, model.ErrNotFound.With("There is no command called /" + name + " here.")
	}
	reply, err := m.Handler.Command(ctx, r.host(actor, conv), Invocation{Command: name, Text: text})
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
	v, err := c.module.Handler.Resolve(ctx, r.host(actor, conv), Marked{Kind: kind, Text: text})
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
