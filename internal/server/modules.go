package server

import (
	"fmt"

	"github.com/tikaydaes/churr/internal/modhost"
	"github.com/tikaydaes/churr/internal/modules/dice"
)

// wireModules registers the built-in modules (docs/DESIGN-MODULES.md
// section 6.2) and hands the host to the two places that call them: chat,
// which resolves the objects they claim when a message is posted (inline
// dice), and commands, which runs their commands (/roll, /dice). This is the
// one place that imports a module.
//
// It runs after chat and commands exist.
func (s *Server) wireModules() error {
	reg, err := modhost.New(modhost.Options{}, dice.Module())
	if err != nil {
		return fmt.Errorf("server: the built-in modules: %w", err)
	}
	s.Modules = reg
	s.Chat.SetModules(reg)
	s.Commands.SetModules(reg)
	return nil
}
