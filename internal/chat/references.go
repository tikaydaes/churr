package chat

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Reference patterns (ruling R73, docs/FEATURES.md section 2.9): the
// server's, which its admins define, and a channel's, which its managers
// define. The store keeps the whole table in memory, so reading it for
// every post costs the database nothing, and a change is seen by the next
// post. Messages already written keep what they resolved.

// ObjectHints is what a posting client says the author chose for ranges of
// the text: plain text, or one kind of object where a range could be two.
type ObjectHints = []model.ObjectHint

// Patterns implements markdown.PatternSource: the server's patterns and
// those of the conversation being written in.
func (r *resolver) Patterns() []model.ReferencePattern {
	all, err := r.a.ReferencePatterns().All(r.ctx)
	if err != nil {
		r.fail(err)
		return nil
	}
	out := all[:0:0]
	for _, p := range all {
		if p.ConversationID == "" || p.ConversationID == r.st.Conversation.ID {
			out = append(out, p)
		}
	}
	return out
}

// ObjectHints implements markdown.HintSource.
func (r *resolver) ObjectHints() []model.ObjectHint { return r.hints }

// ReferencePatterns lists the server's patterns and, when conv is given
// and the actor can see it, that conversation's.
func (s *Service) ReferencePatterns(ctx context.Context, actor model.Account, conv model.ConversationID) ([]model.ReferencePattern, error) {
	if !actor.Active() {
		return nil, model.ErrUnauthenticated
	}
	if conv != "" {
		if _, err := s.perm.Can(ctx, actor, perm.See, conv); err != nil {
			return nil, hide(err)
		}
	}
	return s.patternsOf(ctx, conv, true)
}

// patternsOf returns the patterns of one conversation, with the server's
// too if asked, or the server's alone for "".
func (s *Service) patternsOf(ctx context.Context, conv model.ConversationID, withServer bool) ([]model.ReferencePattern, error) {
	all, err := s.store.ReferencePatterns().All(ctx)
	if err != nil {
		return nil, err
	}
	out := []model.ReferencePattern{}
	for _, p := range all {
		if p.ConversationID == conv || withServer && p.ConversationID == "" {
			out = append(out, p)
		}
	}
	return out, nil
}

// ReferencePatternInput is the fields of a pattern. Nil fields of a change
// stay as they are.
type ReferencePatternInput struct {
	Name, Prefix, Expression, URLTemplate *string
}

// applyPattern checks the fields given and puts them on p.
func applyPattern(p *model.ReferencePattern, in ReferencePatternInput) error {
	if in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if in.Prefix != nil {
		p.Prefix = *in.Prefix
	}
	if in.Expression != nil {
		p.Expression = *in.Expression
	}
	if in.URLTemplate != nil {
		p.URLTemplate = strings.TrimSpace(*in.URLTemplate)
	}
	if p.Name == "" || utf8.RuneCountInString(p.Name) > model.MaxRefName || strings.ContainsFunc(p.Name, func(r rune) bool { return r < 0x20 || r == 0x7F }) {
		return model.Invalid("name", "A pattern's name is 1 to 64 characters.")
	}
	if !model.ValidRefPrefix(p.Prefix) {
		return model.Invalid("prefix", "A prefix is 1 to 16 letters, digits and # _ . / : -, such as PROJ- or #. It may not end in a digit, and one with no letter in it starts with #.")
	}
	if ok, why := model.CheckRefExpression(p.Expression); !ok {
		return model.Invalid("expression", why)
	}
	if !model.ValidRefURL(p.URLTemplate) {
		return model.Invalid("url_template", "The address starts with https:// or http://, is at most 500 characters, and has {value} where the matched value goes, after the host.")
	}
	return nil
}

// mayDefine checks that the actor may change the patterns of a
// conversation, or the server's for "".
func (s *Service) mayDefine(ctx context.Context, actor model.Account, conv model.ConversationID) error {
	if conv == "" {
		return s.perm.CanAdminister(ctx, actor, perm.DefineReferencePatterns)
	}
	_, err := s.perm.Can(ctx, actor, perm.SetReferencePatterns, conv)
	return hide(err)
}

// patternsChanged tells the people a list of patterns applies to: every
// local account for the server's, the members for a channel's.
func (s *Service) patternsChanged(ctx context.Context, conv model.ConversationID) {
	list, err := s.patternsOf(ctx, conv, false)
	if err != nil {
		s.log.Warn("could not read the reference patterns to send them", "err", err)
		return
	}
	if conv == "" {
		s.tellLocals(ctx, model.EventReferencePatternsChanged, map[string]any{"patterns": list})
		return
	}
	roster, err := s.store.Members().Roster(ctx, conv)
	if err != nil {
		s.log.Warn("could not list a channel's members to send its reference patterns", "err", err)
		return
	}
	// The channel goes in the envelope, as for every event that belongs
	// to a conversation (docs/EVENTS.md).
	e := model.Event{Type: model.EventReferencePatternsChanged, At: s.now(), ConversationID: conv,
		Payload: map[string]any{"patterns": list}}
	for _, id := range roster.IDs {
		s.hub.Send(id, e)
	}
}

// CreateReferencePattern defines a pattern: the server's with no
// conversation (admins), a channel's with one (its managers).
func (s *Service) CreateReferencePattern(ctx context.Context, actor model.Account, conv model.ConversationID, in ReferencePatternInput) (model.ReferencePattern, error) {
	p := model.ReferencePattern{ConversationID: conv, CreatedByID: actor.ID, CreatedAt: s.now()}
	if err := s.mayDefine(ctx, actor, conv); err != nil {
		return p, err
	}
	if err := applyPattern(&p, in); err != nil {
		return p, err
	}
	have, err := s.patternsOf(ctx, conv, false)
	if err != nil {
		return p, err
	}
	if len(have) >= model.MaxRefPatterns {
		return p, model.ErrConflict.With("There are already 50 patterns here. Remove one first.")
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.ReferencePatterns().Create(ctx, &p); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{ActorID: string(actor.ID), ActorKind: "account",
			Action: "reference_pattern.created", TargetKind: "reference_pattern", TargetID: p.ID,
			Detail: map[string]any{"name": p.Name, "prefix": p.Prefix, "conversation_id": conv}})
	})
	if err != nil {
		return p, err
	}
	s.patternsChanged(ctx, conv)
	return p, nil
}

// UpdateReferencePattern changes a pattern. Messages already written keep
// the address they resolved.
func (s *Service) UpdateReferencePattern(ctx context.Context, actor model.Account, id string, in ReferencePatternInput) (model.ReferencePattern, error) {
	p, err := s.store.ReferencePatterns().Get(ctx, id)
	if err != nil {
		return p, hide(err)
	}
	if err := s.mayDefine(ctx, actor, p.ConversationID); err != nil {
		return p, err
	}
	if err := applyPattern(&p, in); err != nil {
		return p, err
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.ReferencePatterns().Update(ctx, p); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{ActorID: string(actor.ID), ActorKind: "account",
			Action: "reference_pattern.updated", TargetKind: "reference_pattern", TargetID: p.ID,
			Detail: map[string]any{"name": p.Name, "prefix": p.Prefix, "conversation_id": p.ConversationID}})
	})
	if err != nil {
		return p, hide(err)
	}
	s.patternsChanged(ctx, p.ConversationID)
	return p, nil
}

// DeleteReferencePattern removes a pattern.
func (s *Service) DeleteReferencePattern(ctx context.Context, actor model.Account, id string) error {
	p, err := s.store.ReferencePatterns().Get(ctx, id)
	if err != nil {
		return hide(err)
	}
	if err := s.mayDefine(ctx, actor, p.ConversationID); err != nil {
		return err
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.ReferencePatterns().Delete(ctx, id); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{ActorID: string(actor.ID), ActorKind: "account",
			Action: "reference_pattern.deleted", TargetKind: "reference_pattern", TargetID: id,
			Detail: map[string]any{"name": p.Name, "conversation_id": p.ConversationID}})
	})
	if err != nil {
		return hide(err)
	}
	s.patternsChanged(ctx, p.ConversationID)
	return nil
}
