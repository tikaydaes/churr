package api

import (
	"github.com/tikaydaes/churr/internal/model"
)

// The caller's own credentials: passkeys, recovery codes, the authenticator
// app, linked identities, sessions and personal tokens.

func init() {
	register(func(a *API) {
		a.handle("listPasskeys", a.listPasskeys)
		a.handle("beginAddPasskey", a.beginAddPasskey)
		a.handle("finishAddPasskey", a.finishAddPasskey)
		a.handle("renamePasskey", a.renamePasskey)
		a.handle("deletePasskey", a.deletePasskey)
		a.handle("getRecoveryCodes", a.getRecoveryCodes)
		a.handle("regenerateRecoveryCodes", a.regenerateRecoveryCodes)
		a.handle("getTotp", a.getTotp)
		a.handle("beginTotp", a.beginTotp)
		a.handle("confirmTotp", a.confirmTotp)
		a.handle("removeTotp", a.removeTotp)
		a.handle("listIdentities", a.listIdentities)
		a.handle("listSessions", a.listSessions)
		a.handle("endSession", a.endSession)
		a.handle("endOtherSessions", a.endOtherSessions)
		a.handle("listTokens", a.listTokens)
		a.handle("revokePersonalToken", a.revokePersonalToken)
	})
}

func (a *API) listPasskeys(r *Request) error {
	list, err := a.Auth.ListPasskeys(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"passkeys": orEmpty(list)})
}

func (a *API) beginAddPasskey(r *Request) error {
	c, err := a.Auth.BeginAddPasskey(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(c)
}

func (a *API) finishAddPasskey(r *Request) error {
	var body passkeyBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	fin, err := body.finish()
	if err != nil {
		return err
	}
	added, err := a.Auth.FinishAddPasskey(r.Ctx(), r.Who, fin, label(r))
	if err != nil {
		return err
	}
	out := map[string]any{"passkey": added.Passkey}
	if added.SignIn != nil {
		if out["sign_in"], err = a.signInBody(r, *added.SignIn); err != nil {
			return err
		}
	}
	return r.JSON(out)
}

func (a *API) renamePasskey(r *Request) error {
	id, err := PathID[model.PasskeyID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Name   *string `json:"name"`
		HasPRF *bool   `json:"has_prf"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	pk, err := a.Auth.RenamePasskey(r.Ctx(), r.Who, id, body.Name, body.HasPRF)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"passkey": pk})
}

func (a *API) deletePasskey(r *Request) error {
	id, err := PathID[model.PasskeyID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Auth.DeletePasskey(r.Ctx(), r.Who, id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) getRecoveryCodes(r *Request) error {
	left, err := a.Auth.RecoveryCodesLeft(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"remaining": left})
}

func (a *API) regenerateRecoveryCodes(r *Request) error {
	codes, err := a.Auth.RegenerateRecoveryCodes(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"recovery_codes": codes})
}

func (a *API) getTotp(r *Request) error {
	t, err := a.Auth.TotpState(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"totp": t})
}

func (a *API) beginTotp(r *Request) error {
	e, err := a.Auth.BeginTotp(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(e)
}

func (a *API) confirmTotp(r *Request) error {
	var body struct {
		Code string `json:"code"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Code == "" {
		return model.BadRequest("code is needed.")
	}
	t, in, err := a.Auth.ConfirmTotp(r.Ctx(), r.Who, body.Code)
	if err != nil {
		return err
	}
	out := map[string]any{"totp": t}
	// sign_in is sent when the call completed a sign-in.
	if in != nil && in.Token != "" {
		if out["sign_in"], err = a.signInBody(r, *in); err != nil {
			return err
		}
	}
	return r.JSON(out)
}

func (a *API) removeTotp(r *Request) error {
	if err := a.Auth.RemoveTotp(r.Ctx(), r.Who); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listIdentities(r *Request) error {
	list, err := a.Auth.ListIdentities(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"identities": orEmpty(list)})
}

func (a *API) listSessions(r *Request) error {
	list, err := a.Auth.ListSessions(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"sessions": orEmpty(list)})
}

func (a *API) endSession(r *Request) error {
	id, err := PathID[model.SessionID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Auth.EndSession(r.Ctx(), r.Who, id); err != nil {
		return err
	}
	if id == r.Who.Session.ID && r.Who.Cookie {
		r.W.Header().Add("Set-Cookie", a.Auth.Cookie("", 0).String())
	}
	return r.NoContent()
}

func (a *API) endOtherSessions(r *Request) error {
	if err := a.Auth.EndOtherSessions(r.Ctx(), r.Who); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listTokens(r *Request) error {
	list, err := a.Auth.ListTokens(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"tokens": orEmpty(list)})
}

func (a *API) revokePersonalToken(r *Request) error {
	id, err := PathID[model.SessionID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Auth.RevokePersonalToken(r.Ctx(), r.Who, id); err != nil {
		return err
	}
	return r.NoContent()
}
