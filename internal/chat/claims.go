package chat

import (
	"context"
	"unicode/utf16"

	"github.com/tikaydaes/churr/internal/markdown"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modhost"
	"github.com/tikaydaes/churr/internal/store"
)

// Objects that built-in modules claim (docs/DESIGN-MODULES.md section 4.4):
// inline dice, `[[2d6+3]]` in a person's message (ruling R73), are the dice
// module's claim of kind dice_roll. The module resolves such an object when
// the message is posted, and its value is stored over those characters as
// an object entity. Nothing is resolved on an edit: an edit keeps the value
// of an object it leaves exactly as it was (the same characters, in the
// same order among the message's resolved objects); objects it writes or
// changes are marked removed. Bots, webhooks, imports and sealed chats get
// none. Commands of modules are run through the same host (commands.go).

// SetModules hands over the built-in modules, which the server registers.
// Until it is called no object is resolved and no module command runs.
func (s *Service) SetModules(m *modhost.Registry) { s.modules = m }

// The ways claimed objects in a text are treated (Instants.Claims).
const (
	claimsOff  = ""     // left as text: a bot, a webhook, an import
	claimsSend = "send" // resolved by their module: a person posting
	claimsEdit = "edit" // marked removed, nothing resolved: an edit
)

// claimsMode is how a new message's claimed objects are treated: resolved
// for a person writing it now or through a schedule, left as text
// otherwise.
func claimsMode(actor model.Account, via *model.Via) string {
	if actor.Role == model.RoleBot {
		return claimsOff
	}
	if via != nil && via.Kind != "scheduled" {
		return claimsOff
	}
	return claimsSend
}

// Claims implements markdown.ClaimSource: the claims of the modules, where
// the text's objects are resolved or marked. Whether each module is on in
// the conversation is asked only when the text holds what it claims
// (Claimed): the claims of a module switched off there are never resolved,
// and their text stays text, and a text with nothing claimed in it costs no
// read of the switches.
func (r *resolver) Claims() []markdown.Claim {
	if r.claims == claimsOff || len(r.modules.Claims()) == 0 {
		return nil
	}
	var out []markdown.Claim
	for _, c := range r.modules.Claims() {
		out = append(out, markdown.Claim{Kind: c.Kind, Open: c.Open, Close: c.Close, Inside: c.Inside})
	}
	return out
}

// claimOn reports whether some module that claims a kind is on in the
// conversation; the switches are read once per text, at the first claim.
func (r *resolver) claimOn(kind string) bool {
	if !r.offRead {
		r.offRead = true
		off, err := modulesOff(r.ctx, r.a, r.st.Conversation.ID)
		if err != nil {
			r.err = err
		}
		r.off = off
	}
	for _, c := range r.modules.Claims() {
		if c.Kind == kind && !r.off[c.Module] {
			return true
		}
	}
	return false
}

// modulesOff is the set of modules switched off in a conversation: by an
// admin for the whole instance, or by the conversation's managers
// (docs/DESIGN-MODULES.md section 6.5).
func modulesOff(ctx context.Context, a store.Areas, conv model.ConversationID) (map[string]bool, error) {
	instance, here, err := a.Modules().Off(ctx, conv)
	out := map[string]bool{}
	for _, m := range append(instance, here...) {
		out[m] = true
	}
	return out, err
}

// madeBy reports whether a message holds anything a module made: its view,
// or an object a module resolved.
func madeBy(m model.Message) bool {
	if m.View != nil && m.View.By != "" {
		return true
	}
	for _, e := range m.Entities {
		if isResolved(e) {
			return true
		}
	}
	return false
}

// servedAsText serves what switched-off modules made as its text, for
// messages of one conversation: the view of a module's message is not
// served, so its text, the module's words, is the message; an object a
// module resolved is not served, so its characters stay as typed. Nothing
// stored changes, so switching the module on again brings them back
// (docs/DESIGN-MODULES.md section 6.5; question 18).
func servedAsText(ctx context.Context, a store.Areas, conv model.ConversationID, msgs []model.Message) error {
	any := false
	for i := range msgs {
		if madeBy(msgs[i]) {
			any = true
			break
		}
	}
	if !any {
		return nil
	}
	off, err := modulesOff(ctx, a, conv)
	if err != nil || len(off) == 0 {
		return err
	}
	for i := range msgs {
		m := &msgs[i]
		if m.View != nil && off[m.View.By] {
			m.View = nil
		}
		var kept []model.Entity
		dropped := false
		for _, e := range m.Entities {
			if isResolved(e) && off[e.Object.By] {
				dropped = true
				continue
			}
			kept = append(kept, e)
		}
		if dropped {
			m.Entities = kept
		}
	}
	return nil
}

// Claimed implements markdown.ClaimSource. On an edit the module still reads
// the object, so that only what it would resolve is marked, and what it
// made is thrown away: an edit resolves nothing.
func (r *resolver) Claimed(kind, inside string) (model.ValueEnvelope, bool) {
	if !r.claimOn(kind) {
		return model.ValueEnvelope{}, false
	}
	// An edit rolls for nothing: what it makes is thrown away. Anything
	// else takes the message's draw, and with none the object stays text.
	draws := r.draws
	if r.claims == claimsEdit {
		draws = noDraw()
	}
	v, ok := r.modules.Resolve(r.ctx, r.st.Actor, r.st.Conversation.ID, kind, inside, draws)
	if ok && r.claims == claimsEdit {
		return r.modules.Unresolved(kind)
	}
	return v, ok
}

// isResolved reports whether an entity is an object a module resolved or
// marked: one that keepResolved carries over an edit.
func isResolved(e model.Entity) bool {
	return e.Type == model.EntityObject && e.Object != nil && e.Object.By != ""
}

// keepResolved gives the resolved objects of an edited text the values they
// had, where the same characters stand in the same place among the
// message's resolved objects; the rest stay marked removed.
func keepResolved(oldText string, old []model.Entity, newText string, now []model.Entity) []model.Entity {
	ou, nu := utf16.Encode([]rune(oldText)), utf16.Encode([]rune(newText))
	typed := func(u []uint16, e model.Entity) string {
		if e.At < 0 || e.At+e.Len > len(u) {
			return ""
		}
		return string(utf16.Decode(u[e.At : e.At+e.Len]))
	}
	var before []model.Entity
	for _, e := range old {
		if isResolved(e) {
			before = append(before, e)
		}
	}
	out := make([]model.Entity, len(now))
	n := 0
	for i, e := range now {
		out[i] = e
		if !isResolved(e) {
			continue
		}
		if n < len(before) && !before[n].Object.Removed && before[n].Object.Kind == e.Object.Kind && typed(ou, before[n]) == typed(nu, e) {
			kept := before[n]
			kept.At, kept.Len = e.At, e.Len
			out[i] = kept
		}
		n++
	}
	return out
}
