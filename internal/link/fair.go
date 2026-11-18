package link

import (
	"encoding/json"

	"github.com/tikaydaes/churr/internal/fair"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
)

// A message's fair block on receipt (docs/FAIR.md section 9). The server that
// drew it is the author's: its name must be the block's owner. The block is
// checked under the key that server had at the message's time, by the VRF key
// history this server holds for it (its own; the direct link's pin; the hub
// channel's record), and its input must be the message's own second and
// client_id. A block that fails
// is dropped, and with it every value that took chance from the draw, which
// then shows as its text; the message still arrives, and the drop is
// audited. Outcomes are not derived again here beyond each kind's own check
// (model.CheckValue): the clients re-derive them.

// fairOf checks a wire message's block and says what of it is kept: the
// block, or nil when there was none or it failed, with ok false then for a
// block that failed or values that claim a draw the message does not carry.
func (a *apply) fairOf(w wireMessage) (*model.FairBlock, bool) {
	drew := w.View != nil && w.View.Draw != nil
	for _, e := range w.Entities {
		if e.Object != nil && e.Object.Draw != nil {
			drew = true
		}
	}
	if w.Fair == nil {
		return nil, !drew
	}
	// The author's server drew it. An author this server does not hold
	// names no server, and the block fails.
	owner := ""
	if author, err := a.tx.Accounts().Get(a.ctx, w.AuthorID); err == nil {
		owner = author.ServerName
		if author.Local() {
			owner = a.s.self.Name
		}
	}
	// Nothing of the conversation is in the input: the message's own
	// second and client_id are, and the author's server's key says whose
	// draw it is. That server's clock may be off from this one's, so the
	// second is held to the looser LinkWindow of the message's time.
	err := fair.CheckBlock(w.Fair, owner, w.ClientID, w.CreatedAt, fair.LinkWindow, a.vrfHistory(w.Fair.Owner))
	if err != nil {
		// Written in the event's transaction: the message is stored
		// with its chance values as text, and this says why.
		a.s.audit(a.ctx, a.tx, "", "link.fair_block_dropped", "stream", string(a.st.ID), map[string]any{"message_id": w.ID,
			"origin": a.origin.Name, "owner": w.Fair.Owner, "why": err.Error()})
		return nil, false
	}
	return w.Fair, true
}

// canonical holds a message whose block checked out to the canonical object
// indexes (docs/FAIR.md section 3), as stored here: its view, and its object
// entities after their kinds' checks, so an object that fell back to text
// leaves a gap and fails it too. One that fails is treated as a failed
// block: dropped, with its values, and audited.
func (a *apply) canonical(w wireMessage, m *model.Message) bool {
	view, objs := fair.IndexesOf(m.View, m.Entities)
	err := fair.CheckIndexes(view, objs, !w.EditedAt.IsZero())
	if err == nil {
		return true
	}
	a.s.audit(a.ctx, a.tx, "", "link.fair_block_dropped", "stream", string(a.st.ID), map[string]any{"message_id": w.ID,
		"origin": a.origin.Name, "owner": w.Fair.Owner, "why": err.Error()})
	return false
}

// vrfHistory is the VRF key history this server holds for a server, by name.
func (a *apply) vrfHistory(name string) []fair.Key {
	var keys []wire.VRFKey
	switch {
	case name == a.s.self.Name:
		var out []fair.Key
		for _, k := range a.s.keys.VRFHistory() {
			out = append(out, fair.Key{Key: k.Key, Began: k.Began})
		}
		return out
	case hubOwned(a.st):
		rec, err := a.s.channelOf(a.ctx, a.tx, a.st.ID)
		if err != nil {
			return nil
		}
		keys = rec.VRFKeys[name]
	default:
		sv, err := a.tx.Peers().ServerByName(a.ctx, name)
		if err != nil || len(sv.VRFKeys) == 0 || json.Unmarshal(sv.VRFKeys, &keys) != nil {
			return nil
		}
	}
	out := make([]fair.Key, len(keys))
	for i, k := range keys {
		out[i] = fair.Key{Key: k.Key, Began: k.Began}
	}
	return out
}

// dropDrawn takes away every value of a message that took chance from a draw
// that did not check out: the message's view, and object entities, whose
// characters are then plain text.
func dropDrawn(m *model.Message) {
	if m.View != nil && m.View.Draw != nil {
		m.View = nil
	}
	var kept []model.Entity
	for _, e := range m.Entities {
		if e.Type == model.EntityObject && e.Object != nil && e.Object.Draw != nil {
			continue
		}
		kept = append(kept, e)
	}
	m.Entities = kept
}
