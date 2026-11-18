package chat

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modhost"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// The switches of the built-in modules (docs/DESIGN-MODULES.md section 6.5).
// An admin switches a module on or off for the whole instance; a channel's
// managers switch off there what the instance has on, and on again. Where a
// module is off its commands are neither offered nor run, the objects it
// claims stay text, and what it made is served as its text (claims.go):
// nothing stored changes, so switching it on again brings its cards back.

// listedModule is a registered module as listed, on where off does not name it.
func listedModule(m modhost.Module, serverOn, on bool) model.BuiltinModule {
	out := model.BuiltinModule{Name: m.Name, Title: m.Title, On: on, ServerOn: serverOn,
		Commands: []model.ModuleCommand{}, Claims: []string{}}
	for _, c := range m.Commands {
		out.Commands = append(out.Commands, model.ModuleCommand{Name: c.Name, Hint: c.Hint, Summary: c.Summary})
	}
	for _, c := range m.Claims {
		out.Claims = append(out.Claims, c.Kind)
	}
	return out
}

// nameSet makes a list of names a set.
func nameSet(names []string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}
	return out
}

// Modules lists the built-in modules for an admin, with whether the
// instance has each on.
func (s *Service) Modules(ctx context.Context, actor model.Account) ([]model.BuiltinModule, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.ManageModules); err != nil {
		return nil, err
	}
	instance, _, err := s.store.Modules().Off(ctx, "")
	if err != nil {
		return nil, err
	}
	off := nameSet(instance)
	out := []model.BuiltinModule{}
	for _, m := range s.modules.List() {
		out = append(out, listedModule(m, !off[m.Name], !off[m.Name]))
	}
	return out, nil
}

// SwitchModule switches a built-in module on or off for the whole
// instance. Channels that switched it off themselves stay as they are.
func (s *Service) SwitchModule(ctx context.Context, actor model.Account, name string, on bool) (model.BuiltinModule, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.ManageModules); err != nil {
		return model.BuiltinModule{}, err
	}
	m, ok := s.modules.Module(name)
	if !ok {
		return model.BuiltinModule{}, model.ErrNotFound.With("There is no built-in module called " + name + ".")
	}
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Modules().SetOff(ctx, name, "", !on); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{ActorID: string(actor.ID), ActorKind: "account",
			Action: switched(on), TargetKind: "module", TargetID: name, Detail: map[string]any{}})
	})
	return listedModule(m, on, on), err
}

// switched is the audit's name for a switch.
func switched(on bool) string {
	if on {
		return "module.switched_on"
	}
	return "module.switched_off"
}

// ConversationModules lists the built-in modules as they stand in a
// conversation, for anyone who can read it. A sealed chat runs none.
func (s *Service) ConversationModules(ctx context.Context, actor model.Account, conv model.ConversationID) ([]model.BuiltinModule, error) {
	st, err := s.perm.Can(ctx, actor, perm.Read, conv)
	if err != nil {
		return nil, err
	}
	instance, here, err := s.store.Modules().Off(ctx, conv)
	if err != nil {
		return nil, err
	}
	serverOff, hereOff := nameSet(instance), nameSet(here)
	out := []model.BuiltinModule{}
	for _, m := range s.modules.List() {
		on := !serverOff[m.Name] && !hereOff[m.Name] && !st.Conversation.Sealed()
		out = append(out, listedModule(m, !serverOff[m.Name], on))
	}
	return out, nil
}

// SwitchConversationModule switches a built-in module off in a channel, or
// on again: its managers' choice. It cannot be switched on where the
// instance has it off.
func (s *Service) SwitchConversationModule(ctx context.Context, actor model.Account, conv model.ConversationID, name string, on bool) (model.BuiltinModule, error) {
	if _, err := s.perm.Can(ctx, actor, perm.SetModules, conv); err != nil {
		return model.BuiltinModule{}, err
	}
	m, ok := s.modules.Module(name)
	if !ok {
		return model.BuiltinModule{}, model.ErrNotFound.With("There is no built-in module called " + name + ".")
	}
	instance, _, err := s.store.Modules().Off(ctx, "")
	if err != nil {
		return model.BuiltinModule{}, err
	}
	serverOn := !nameSet(instance)[name]
	if on && !serverOn {
		return model.BuiltinModule{}, model.ErrFeatureOff.With("The " + m.Title + " module is switched off on this server, so no channel can switch it on.")
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Modules().SetOff(ctx, name, conv, !on); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{ActorID: string(actor.ID), ActorKind: "account",
			Action: switched(on), TargetKind: "module", TargetID: name, Detail: map[string]any{"conversation_id": conv}})
	})
	return listedModule(m, serverOn, on && serverOn), err
}
