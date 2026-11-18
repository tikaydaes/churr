package api

import (
	"github.com/tikaydaes/churr/internal/model"
)

// The switches of the built-in modules (docs/DESIGN-MODULES.md section 6.5):
// the admin's for the whole server, and a channel's managers' for theirs.

func init() {
	register(func(a *API) {
		a.handle("adminListModules", a.adminListModules)
		a.handle("setModule", a.setModule)
		a.handle("listConversationModules", a.listConversationModules)
		a.handle("setConversationModule", a.setConversationModule)
		a.handle("getCommandSettings", a.getCommandSettings)
		a.handle("updateCommandSettings", a.updateCommandSettings)
	})
}

// moduleSwitch reads the body of a switch: {"on": bool}, which is needed.
func moduleSwitch(r *Request) (bool, error) {
	var body struct {
		On *bool `json:"on"`
	}
	if err := r.Decode(&body); err != nil {
		return false, err
	}
	if body.On == nil {
		return false, model.BadRequest("on is needed: true or false.")
	}
	return *body.On, nil
}

func (a *API) adminListModules(r *Request) error {
	mods, err := a.Chat.Modules(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"modules": mods})
}

func (a *API) setModule(r *Request) error {
	on, err := moduleSwitch(r)
	if err != nil {
		return err
	}
	m, err := a.Chat.SwitchModule(r.Ctx(), r.Actor(), r.R.PathValue("name"), on)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"module": m})
}

func (a *API) listConversationModules(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	mods, err := a.Chat.ConversationModules(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"modules": mods})
}

func (a *API) setConversationModule(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	on, err := moduleSwitch(r)
	if err != nil {
		return err
	}
	m, err := a.Chat.SwitchConversationModule(r.Ctx(), r.Actor(), id, r.R.PathValue("name"), on)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"module": m})
}

func (a *API) getCommandSettings(r *Request) error {
	set, err := a.Commands.CommandSettings(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"settings": set})
}

func (a *API) updateCommandSettings(r *Request) error {
	var body struct {
		Order   []string              `json:"order"`
		Renames []model.CommandRename `json:"renames"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	set, err := a.Commands.SetCommandSettings(r.Ctx(), r.Actor(), body.Order, body.Renames)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"settings": set})
}
