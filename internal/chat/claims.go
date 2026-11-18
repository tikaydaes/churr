package chat

import (
	"unicode/utf16"

	"github.com/tikaydaes/churr/internal/markdown"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modhost"
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

// Claims implements markdown.ClaimSource: the modules' claims, where the
// text's objects are resolved or marked.
func (r *resolver) Claims() []markdown.Claim {
	if r.claims == claimsOff {
		return nil
	}
	var out []markdown.Claim
	for _, c := range r.modules.Claims() {
		out = append(out, markdown.Claim{Kind: c.Kind, Open: c.Open, Close: c.Close, Inside: c.Inside})
	}
	return out
}

// Claimed implements markdown.ClaimSource. On an edit the module still reads
// the object, so that only what it would resolve is marked, and what it
// made is thrown away: an edit resolves nothing.
func (r *resolver) Claimed(kind, inside string) (model.ValueEnvelope, bool) {
	v, ok := r.modules.Resolve(r.ctx, r.st.Actor, r.st.Conversation.ID, kind, inside)
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
