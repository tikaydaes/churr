package server

import (
	"context"
	"fmt"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modhost"
	"github.com/tikaydaes/churr/internal/modules/cards"
	"github.com/tikaydaes/churr/internal/modules/choose"
	"github.com/tikaydaes/churr/internal/modules/dice"
)

// wireModules registers the built-in modules (docs/DESIGN-MODULES.md
// section 6.2) and hands the host to the two places that call them: chat,
// which resolves the objects they claim when a message is posted (inline
// dice), and commands, which runs their commands (/roll, /dice, /flip,
// /pick, /shuffle, /draw, /tarot, /mahjongg), with what
// chat draws their chance with. This is the one place that imports a module.
//
// It runs after chat and commands exist.
func (s *Server) wireModules() error {
	reg, err := modhost.New(modhost.Options{Store: s.Store.Modules(), Switches: s.Store.Modules(), Failed: s.moduleFailed},
		dice.Module(), choose.Module(), cards.Module())
	if err != nil {
		return fmt.Errorf("server: the built-in modules: %w", err)
	}
	s.Modules = reg
	s.Chat.SetModules(reg)
	s.Commands.SetModules(reg)
	// Their chance is drawn by this server's VRF key, its chains owned by
	// its name (docs/FAIR.md).
	s.Chat.SetFair(s.Config.Hostname, s.Keys)
	return nil
}

// moduleFailed is told of a module that panicked or gave no answer in time.
// The person already has their refusal; this logs it with its stack and
// puts it in the audit log, where the admins see it. A module that panicked
// is switched off for the whole instance (docs/DESIGN-MODULES.md section
// 6.2) until an admin switches it on again: what it made is then served as
// text. The writes are made on their own, so that a failure in the posting
// path never waits on the database's writer.
func (s *Server) moduleFailed(ctx context.Context, f modhost.Failure) {
	s.Log.Error("modules: "+f.String(), "module", f.Module, "entry", f.Entry, "conversation", f.Conversation, "stack", f.Stack)
	detail := map[string]any{"entry": f.Entry, "conversation_id": f.Conversation}
	action := "module.timed_out"
	if f.Panic != nil {
		action = "module.panicked"
		detail["panic"] = fmt.Sprint(f.Panic)
		detail["switched_off"] = true
	}
	entry := model.AuditEntry{ActorID: string(f.Actor.ID), ActorKind: "account", Action: action,
		TargetKind: "module", TargetID: f.Module, Detail: detail}
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if f.Panic != nil {
			if err := s.Store.Modules().SetOff(ctx, f.Module, "", true); err != nil {
				s.Log.Error("modules: switching off a module that panicked", "module", f.Module, "err", err)
			}
		}
		if err := s.Store.Audit().Append(ctx, entry); err != nil {
			s.Log.Error("modules: auditing a failure", "module", f.Module, "err", err)
		}
	}()
}
