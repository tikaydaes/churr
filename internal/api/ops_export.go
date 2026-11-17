package api

import (
	"net/http"
	"net/url"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
)

// The owner's export (docs/SCHEMA.md section 18): started, polled and
// downloaded in three calls. It includes private and ad-hoc channels, which
// the owner cannot read through any other operation, and nothing of a sealed
// chat. Only an Owner may, from a session checked in the last ten minutes
// (valuable, api.go). Each start, and each download, is written to the
// audit log.

func init() {
	register(func(a *API) {
		a.handle("startExport", a.startExport)
		a.handle("getExport", a.getExport)
		a.handle("createExportDownload", a.createExportDownload)
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
	if a.People != nil {
		a.People.TellOwners(r.Ctx(), "export_started", r.Actor().ID, st.ID)
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

// createExportDownload hands out the one-time address the download is made
// with. It is the step that is checked and audited: the address is good for
// a minute and for nobody else.
func (a *API) createExportDownload(r *Request) error {
	if err := a.mayExport(r); err != nil {
		return err
	}
	value, st, until, err := a.Export.Ticket(r.Actor().ID)
	if err != nil {
		return err
	}
	err = a.Store.Audit().Append(r.Ctx(), model.AuditEntry{At: model.Now(), ActorKind: "account", ActorID: string(r.Actor().ID),
		Action: "export.downloaded", TargetKind: "export", TargetID: st.ID, Detail: map[string]any{"size": st.Size}})
	if err != nil {
		return err
	}
	if a.People != nil {
		a.People.TellOwners(r.Ctx(), "export_downloaded", r.Actor().ID, st.ID)
	}
	return r.JSON(map[string]any{"url": "/api/v1/admin/export/download?ticket=" + url.QueryEscape(value), "expires_at": model.FromTime(until)})
}

func (a *API) downloadExport(r *Request) error {
	if err := a.mayExport(r); err != nil {
		return err
	}
	f, st, err := a.Export.Redeem(r.R.URL.Query().Get("ticket"), r.Actor().ID)
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
