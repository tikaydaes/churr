package api

import (
	"github.com/tikaydaes/churr/internal/model"
)

// Notices kept for the caller (docs/FEATURES.md section 21.13): only the
// caller's own, newest first, with how many are unseen.

func init() {
	register(func(a *API) {
		a.handle("listNotices", a.listNotices)
		a.handle("markNoticesSeen", a.markNoticesSeen)
	})
}

func (a *API) listNotices(r *Request) error {
	p, err := r.Page()
	if err != nil {
		return err
	}
	me := r.Actor().ID
	items, next, err := a.Store.Notices().List(r.Ctx(), me, p)
	if err != nil {
		return err
	}
	unseen, err := a.Store.Notices().Unseen(r.Ctx(), me)
	if err != nil {
		return err
	}
	out := list("notices", orEmpty(items), next)
	out["unseen"] = unseen
	return r.JSON(out)
}

func (a *API) markNoticesSeen(r *Request) error {
	if err := a.Store.Notices().MarkSeen(r.Ctx(), r.Actor().ID, model.Now()); err != nil {
		return err
	}
	return r.NoContent()
}
