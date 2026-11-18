package sched

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modhost"
	"github.com/tikaydaes/churr/internal/perm"
)

// The command table (docs/DESIGN-MODULES.md section 6.5): where two sources
// have a name, the admin's order of the sources says which one it is; the
// admin gives a module's command another name where it clashes. A protected
// built-in always wins. The list (List) and the running (Run) both follow
// the table, and the list carries the final names the clients parse by.

// The settings of the table (docs/SCHEMA.md section 2).
const (
	SettingCommandOrder   = "commands.order"
	SettingCommandRenames = "commands.renames"
)

// settings reads the table's settings. An order that is not a whole order
// of the four sources is the default's; a rename of a command no module
// has any more is left out.
func (c *Commands) settings(ctx context.Context) (model.CommandSettings, error) {
	out := model.CommandSettings{Order: slices.Clone(model.DefaultCommandOrder), Renames: []model.CommandRename{}}
	raw, found, err := c.store.Settings().Get(ctx, SettingCommandOrder)
	if err != nil {
		return out, err
	}
	var order []string
	if found && json.Unmarshal(raw, &order) == nil && wholeOrder(order) {
		out.Order = order
	}
	raw, found, err = c.store.Settings().Get(ctx, SettingCommandRenames)
	if err != nil {
		return out, err
	}
	var renames []model.CommandRename
	if found && json.Unmarshal(raw, &renames) == nil {
		for _, r := range renames {
			if c.declares(r.Module, r.Command) {
				out.Renames = append(out.Renames, r)
			}
		}
	}
	return out, nil
}

// wholeOrder reports whether an order names each source once.
func wholeOrder(order []string) bool {
	if len(order) != len(model.DefaultCommandOrder) {
		return false
	}
	for _, s := range model.DefaultCommandOrder {
		if !slices.Contains(order, s) {
			return false
		}
	}
	return true
}

// declares reports whether a module has a command of a name.
func (c *Commands) declares(module, command string) bool {
	m, ok := c.modules.Module(module)
	return ok && slices.ContainsFunc(m.Commands, func(x modhost.Command) bool { return x.Name == command })
}

// moduleCommand is a module's command under its final name.
type moduleCommand struct {
	module modhost.Module
	cmd    modhost.Command
	name   string
}

// moduleCommands is every module's command by its final name here: the
// admin's name for it, or its own.
func (c *Commands) moduleCommands(set model.CommandSettings) map[string]moduleCommand {
	renamed := map[[2]string]string{}
	for _, r := range set.Renames {
		renamed[[2]string{r.Module, r.Command}] = r.Name
	}
	out := map[string]moduleCommand{}
	for _, m := range c.modules.List() {
		for _, cmd := range m.Commands {
			name := cmd.Name
			if to, ok := renamed[[2]string{m.Name, cmd.Name}]; ok {
				name = to
			}
			out[name] = moduleCommand{module: m, cmd: cmd, name: name}
		}
	}
	return out
}

// listed is a module's command as the menu lists it: its final name, its
// forms, and its help with the names it has here written in.
func (mc moduleCommand) listed(set model.CommandSettings) model.Command {
	names := map[string]string{}
	for _, cmd := range mc.module.Commands {
		names[cmd.Name] = cmd.Name
	}
	for _, r := range set.Renames {
		if r.Module == mc.module.Name {
			names[r.Command] = r.Name
		}
	}
	write := func(s string) string {
		s = strings.ReplaceAll(s, "{cmd}", mc.name)
		for from, to := range names {
			s = strings.ReplaceAll(s, "{cmd:"+from+"}", to)
		}
		return s
	}
	h := mc.cmd.Help
	help := model.CommandHelp{Description: write(h.Description), Where: write(h.Where), Who: write(h.Who)}
	for _, a := range h.Args {
		help.Args = append(help.Args, model.CommandHelpArg{Name: a.Name, About: write(a.About)})
	}
	for _, e := range h.Examples {
		help.Examples = append(help.Examples, model.CommandExample{Type: write(e.Type), Then: write(e.Then)})
	}
	return model.Command{Name: mc.name, Kind: model.CommandModule, Description: mc.cmd.Summary, Hint: mc.cmd.Hint,
		Module: mc.module.Name, Forms: mc.cmd.Forms, Help: &help}
}

// CommandSettings returns the command table's settings, for an admin.
func (c *Commands) CommandSettings(ctx context.Context, actor model.Account) (model.CommandSettings, error) {
	if err := c.perm.CanAdminister(ctx, actor, perm.ManageModules); err != nil {
		return model.CommandSettings{}, err
	}
	return c.settings(ctx)
}

// SetCommandSettings changes the order of the sources, the renames, or both
// (nil leaves one as it is). An order names each source once. A rename
// names a module's command and a name a command may have that no other
// module's command has here, and is refused a protected built-in's; a
// rename to the command's own name is dropped.
func (c *Commands) SetCommandSettings(ctx context.Context, actor model.Account, order []string, renames []model.CommandRename) (model.CommandSettings, error) {
	if err := c.perm.CanAdminister(ctx, actor, perm.ManageModules); err != nil {
		return model.CommandSettings{}, err
	}
	set, err := c.settings(ctx)
	if err != nil {
		return set, err
	}
	if order != nil {
		if !wholeOrder(order) {
			return set, model.Invalid("order", "The order names each of saved_reply, bot, module and builtin once.")
		}
		set.Order = order
	}
	if renames != nil {
		kept := []model.CommandRename{}
		for _, r := range renames {
			if !c.declares(r.Module, r.Command) {
				return set, model.Invalid("renames", "No module "+r.Module+" has a command /"+r.Command+".")
			}
			if r.Name == r.Command {
				continue
			}
			if !model.ValidCommandName(r.Name) {
				return set, model.Invalid("renames", "A command name is 1 to 32 characters of a-z, 0-9, _ and -.")
			}
			if model.ReservedCommandName(r.Name) {
				return set, model.ErrNameTaken
			}
			if model.ProtectedCommand(r.Name) {
				return set, model.ProtectedCommandError(r.Name)
			}
			kept = append(kept, r)
		}
		set.Renames = kept
		// Each command of a module once, and no two with one name.
		seen, finals := map[[2]string]bool{}, c.moduleCommands(set)
		count := 0
		for _, m := range c.modules.List() {
			count += len(m.Commands)
		}
		for _, r := range kept {
			if seen[[2]string{r.Module, r.Command}] {
				return set, model.Invalid("renames", "/"+r.Command+" of "+r.Module+" is renamed twice.")
			}
			seen[[2]string{r.Module, r.Command}] = true
		}
		if len(finals) != count {
			return set, model.ErrNameTaken.With("Two modules' commands would have the same name.")
		}
	}
	if order != nil {
		if err := c.store.Settings().Set(ctx, SettingCommandOrder, set.Order, actor.ID); err != nil {
			return set, err
		}
	}
	if renames != nil {
		if err := c.store.Settings().Set(ctx, SettingCommandRenames, set.Renames, actor.ID); err != nil {
			return set, err
		}
	}
	return set, c.store.Audit().Append(ctx, model.AuditEntry{ActorID: string(actor.ID), ActorKind: "account", Action: "commands.updated",
		TargetKind: "settings", TargetID: "commands", Detail: map[string]any{"order": set.Order, "renames": set.Renames}})
}
