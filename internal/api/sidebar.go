package api

import (
	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
)

// The caller's own sidebar (ruling R83): read whole, changed by small ops
// against a version.

func init() {
	register(func(a *API) {
		a.handle("getSidebar", a.getSidebar)
		a.handle("changeSidebar", a.changeSidebar)
	})
}

func (a *API) getSidebar(r *Request) error {
	l, err := a.Chat.Sidebar(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"sidebar": l})
}

func (a *API) changeSidebar(r *Request) error {
	var body struct {
		Version *int64           `json:"version"`
		Ops     []chat.SidebarOp `json:"ops"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Version == nil {
		return model.Invalid("version", "Give the version of the sidebar the change was made against.")
	}
	l, err := a.Chat.ChangeSidebar(r.Ctx(), r.Actor(), *body.Version, body.Ops)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"sidebar": l})
}
