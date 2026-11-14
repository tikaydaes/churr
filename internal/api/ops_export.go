package api

import (
	"net/http"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
)

// The owner's export (docs/SCHEMA.md section 18): started, polled and
// downloaded in three calls. It includes private and ad-hoc channels, which
// the owner cannot read through any other operation, and nothing of a sealed
// chat. Only the owner may, and each start is written to the audit log.

func init() {
	register(func(a *API) {
		a.handle("startExport", a.startExport)
		a.handle("getExport", a.getExport)
		a.handle("downloadExport", a.downloadExport)
	})
}

// mayExport answers forbidden for anyone but the owner.
func (a *API) mayExport(r *Request) error {
	if err := perm.CheckServer(r.Actor(), perm.RunExport, perm.DefaultSettings()); err != nil {
		return err
	}
	if a.Export == nil {
		return model.ErrUnavailable.With("This server cannot make an export.")
	}
	return nil
}

func (a *API) startExport(r *Request) error {
	if err := a.mayExport(r); err != nil {
		return err
	}
	st, err := a.Export.Start(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"export": st})
}

func (a *API) getExport(r *Request) error {
	if err := a.mayExport(r); err != nil {
		return err
	}
	// The contract makes `export` optional: it is absent until one has
	// been started.
	if st, ok := a.Export.Latest(); ok {
		return r.JSON(map[string]any{"export": st})
	}
	return r.JSON(map[string]any{})
}

func (a *API) downloadExport(r *Request) error {
	if err := a.mayExport(r); err != nil {
		return err
	}
	f, st, err := a.Export.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	h := r.W.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", `attachment; filename="churr-export-`+st.StartedAt.Time().UTC().Format("2006-01-02")+`.zip"`)
	// Everything a community has said is in this file.
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(r.W, r.R, "", st.FinishedAt.Time(), f)
	return nil
}
