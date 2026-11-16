package api

import (
	"net/http"

	"github.com/tikaydaes/churr/internal/model"
)

// Watches: "tell me when this person is around" (ruling R76). The scheduler
// keeps them; a watch is listed and cancelled with the other scheduled items.

func init() {
	register(func(a *API) {
		a.handle("watchAccount", a.watchAccount)
		a.handle("unwatchAccount", a.unwatchAccount)
	})
}

func (a *API) watchAccount(r *Request) error {
	id, err := PathID[model.AccountID](r, "account_id")
	if err != nil {
		return err
	}
	item, around, err := a.Sched.Watch(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	if around {
		// Nothing was stored: the person is around now, as the caller
		// may see them.
		return r.JSON(map[string]any{"around": true})
	}
	return r.JSONStatus(http.StatusCreated, map[string]any{"around": false, "scheduled": item})
}

func (a *API) unwatchAccount(r *Request) error {
	id, err := PathID[model.AccountID](r, "account_id")
	if err != nil {
		return err
	}
	if err := a.Sched.Unwatch(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}
