package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/tikaydaes/churr/internal/auth"
	"github.com/tikaydaes/churr/internal/model"
)

// What admins do to sign-in: settings, providers, recovery links, signing
// people out, and server integrations; and what an integration itself does.

func init() {
	register(func(a *API) {
		a.handle("createRecoveryLink", a.createRecoveryLink)
		a.handle("adminRemoveTotp", a.adminRemoveTotp)
		a.handle("signOutAccount", a.signOutAccount)
		a.handle("signOutEveryone", a.signOutEveryone)

		a.handle("getSettings", a.getSettings)
		a.handle("updateSettings", a.updateSettings)
		a.handle("setServerIcon", a.setServerIcon)
		a.handle("listSsoProviders", a.listSsoProviders)
		a.handle("createSsoProvider", a.createSsoProvider)
		a.handle("getSsoProvider", a.getSsoProvider)
		a.handle("updateSsoProvider", a.updateSsoProvider)
		a.handle("deleteSsoProvider", a.deleteSsoProvider)
		a.handle("testSsoProvider", a.testSsoProvider)

		a.handle("listIntegrations", a.listIntegrations)
		a.handle("createIntegration", a.createIntegration)
		a.handle("revokeIntegration", a.revokeIntegration)
		a.handle("findAccountForIntegration", a.findAccountForIntegration)
		a.handle("setAvailabilityForAccount", a.setAvailabilityForAccount)
	})
}

func (a *API) createRecoveryLink(r *Request) error {
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	link, err := a.Auth.CreateRecoveryLink(r.Ctx(), r.Who, id)
	if err != nil {
		return err
	}
	return r.JSON(link)
}

func (a *API) adminRemoveTotp(r *Request) error {
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Auth.AdminRemoveTotp(r.Ctx(), r.Who, id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) signOutAccount(r *Request) error {
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Auth.SignOutAccount(r.Ctx(), r.Who, id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) signOutEveryone(r *Request) error {
	if err := a.Auth.SignOutEveryone(r.Ctx(), r.Who); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) getSettings(r *Request) error {
	set, err := a.Auth.Settings(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"settings": set})
}

// serverChanged tells every connection that something in getServer changed:
// a feature switch, a limit, the sign-in methods.
func (a *API) serverChanged(r *Request) {
	server, err := a.serverObject(r.Ctx(), false)
	if err != nil {
		a.Log.Warn("server.updated could not be built", "err", err)
		return
	}
	a.Live.Broadcast(model.Event{Type: model.EventServerUpdated, At: model.Now(), Payload: map[string]any{"server": server}})
}

func (a *API) updateSettings(r *Request) error {
	var patch map[string]json.RawMessage
	if err := r.Decode(&patch); err != nil {
		return err
	}
	if patch == nil {
		return model.BadRequest("The request body must be one JSON object.")
	}
	set, changed, err := a.Auth.UpdateSettings(r.Ctx(), r.Who, patch)
	if err != nil {
		return err
	}
	if changed {
		a.serverChanged(r)
	}
	// The hub keeps the sizes that decide lazy delivery (ruling R66): it
	// reads them again now and not at its next look.
	if a.Live != nil {
		a.Live.RefreshLimits(r.Ctx())
	}
	return r.JSON(map[string]any{"settings": set})
}

func (a *API) setServerIcon(r *Request) error {
	// Refused before the upload is read, not after.
	if err := a.Auth.MayChangeSettings(r.Ctx(), r.Who); err != nil {
		return err
	}
	mr, err := r.R.MultipartReader()
	if err != nil {
		return model.BadRequest("Send the image as multipart/form-data in a part named `file`.")
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return model.Invalid("file", "The upload has no part named `file`.")
		}
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				return model.ErrTooLarge.With("An icon is at most 1 MB.")
			}
			return model.BadRequest("The upload could not be read.")
		}
		if part.FormName() != "file" {
			continue
		}
		set, err := a.Auth.SetServerIcon(r.Ctx(), r.Who, part)
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				return model.ErrTooLarge.With("An icon is at most 1 MB.")
			}
			return err
		}
		a.serverChanged(r)
		return r.JSON(map[string]any{"settings": set})
	}
}

func (a *API) listSsoProviders(r *Request) error {
	list, err := a.Auth.ListProviders(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"providers": orEmpty(list)})
}

func (a *API) createSsoProvider(r *Request) error {
	var body auth.ProviderInput
	if err := r.Decode(&body); err != nil {
		return err
	}
	p, err := a.Auth.CreateProvider(r.Ctx(), r.Who, body)
	if err != nil {
		return err
	}
	a.serverChanged(r)
	return r.JSON(map[string]any{"provider": p})
}

func (a *API) getSsoProvider(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	p, err := a.Auth.GetProvider(r.Ctx(), r.Who, id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"provider": p})
}

func (a *API) updateSsoProvider(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	var body auth.ProviderInput
	if err := r.Decode(&body); err != nil {
		return err
	}
	p, err := a.Auth.UpdateProvider(r.Ctx(), r.Who, id, body)
	if err != nil {
		return err
	}
	a.serverChanged(r)
	return r.JSON(map[string]any{"provider": p})
}

func (a *API) deleteSsoProvider(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	if err := a.Auth.DeleteProvider(r.Ctx(), r.Who, id); err != nil {
		return err
	}
	a.serverChanged(r)
	return r.NoContent()
}

func (a *API) testSsoProvider(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	ok, problem, err := a.Auth.TestProvider(r.Ctx(), r.Who, id)
	if err != nil {
		return err
	}
	out := map[string]any{"ok": ok}
	if problem != "" {
		out["error"] = problem
	}
	return r.JSON(out)
}

func (a *API) listIntegrations(r *Request) error {
	list, err := a.Auth.ListIntegrations(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"integrations": orEmpty(list)})
}

func (a *API) createIntegration(r *Request) error {
	var body struct {
		Name   string   `json:"name"`
		Allows []string `json:"allows"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	in, token, err := a.Auth.CreateIntegration(r.Ctx(), r.Who, body.Name, body.Allows)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"integration": in, "secret": token})
}

func (a *API) revokeIntegration(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	if err := a.Auth.RevokeIntegration(r.Ctx(), r.Who, id); err != nil {
		return err
	}
	return r.NoContent()
}

// --- What an integration itself does. Its token is the only credential. ---

func (a *API) findAccountForIntegration(r *Request) error {
	in, err := a.Auth.Integration(r.R, r.Addr)
	if err != nil {
		return err
	}
	q := r.R.URL.Query()
	acc, err := a.Auth.FindAccountForIntegration(r.Ctx(), in, q.Get("username"), q.Get("provider_id"), q.Get("subject"))
	if err != nil {
		return err
	}
	// The identifier and username only.
	return r.JSON(map[string]any{"account": map[string]any{"id": acc.ID, "username": acc.Username}})
}

func (a *API) setAvailabilityForAccount(r *Request) error {
	in, err := a.Auth.Integration(r.R, r.Addr)
	if err != nil {
		return err
	}
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Availability *string    `json:"availability"`
		Until        model.Time `json:"until"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	value := ""
	if body.Availability != nil {
		if value = *body.Availability; value == "" {
			return model.Invalid("availability", "availability is meeting, out_of_office or null.")
		}
	}
	acc, err := a.Auth.SetAvailabilityForAccount(r.Ctx(), in, id, value, body.Until)
	if err != nil {
		return err
	}
	out := map[string]any{"id": acc.ID}
	if acc.Availability != nil {
		out["availability"] = acc.Availability
	}
	return r.JSON(out)
}
