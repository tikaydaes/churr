package api

import (
	"encoding/json"
	"strings"

	"github.com/tikaydaes/churr/internal/auth"
	"github.com/tikaydaes/churr/internal/model"
)

func init() {
	register(func(a *API) {
		a.handle("beginSetup", a.beginSetup)
		a.handle("finishSetup", a.finishSetup)
		a.handle("checkInvite", a.checkInvite)
		a.handle("beginPasskeyRegistration", a.beginPasskeyRegistration)
		a.handle("finishPasskeyRegistration", a.finishPasskeyRegistration)
		a.handle("beginPasskeyLogin", a.beginPasskeyLogin)
		a.handle("finishPasskeyLogin", a.finishPasskeyLogin)
		a.handle("logout", a.logout)
		a.handle("createToken", a.createToken)
	})
}

// passkeyBody is the body of every step that ends a passkey ceremony
// (schemas CeremonyResult and PasskeyCreated).
type passkeyBody struct {
	CeremonyID  string          `json:"ceremony_id"`
	Credential  json.RawMessage `json:"credential"`
	PasskeyName string          `json:"passkey_name"`
	PRFEnabled  bool            `json:"prf_enabled"`
}

func (b passkeyBody) finish() (auth.Finish, error) {
	if b.CeremonyID == "" || len(b.Credential) == 0 {
		return auth.Finish{}, model.BadRequest("ceremony_id and credential are needed.")
	}
	return auth.Finish{CeremonyID: b.CeremonyID, Credential: b.Credential, PasskeyName: b.PasskeyName, PRFEnabled: b.PRFEnabled}, nil
}

// label names a session after the browser that made it.
func label(r *Request) string {
	ua := r.R.UserAgent()
	for _, known := range []string{"Firefox", "Edg", "Chrome", "Safari"} {
		if strings.Contains(ua, known) {
			if known == "Edg" {
				return "Edge"
			}
			return known
		}
	}
	return "Browser"
}

// signedIn writes the answer of a sign-in step and sets the cookie.
func (a *API) signedIn(r *Request, status int, in auth.SignIn) error {
	me, err := a.People.Me(r.Ctx(), in.Account)
	if err != nil {
		return err
	}
	r.W.Header().Add("Set-Cookie", a.Auth.Cookie(in.Token, in.Session.ExpiresAt).String())
	out := map[string]any{"session": in.Session, "account": me}
	if len(in.RecoveryCodes) > 0 {
		out["recovery_codes"] = in.RecoveryCodes
	}
	return r.JSONStatus(status, out)
}

func (a *API) beginSetup(r *Request) error {
	var body struct {
		SetupToken  string `json:"setup_token"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	c, err := a.Auth.BeginSetup(r.Ctx(), r.Addr, body.SetupToken, body.Username, body.DisplayName)
	if err != nil {
		return err
	}
	return r.JSON(c)
}

func (a *API) finishSetup(r *Request) error {
	var body passkeyBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	fin, err := body.finish()
	if err != nil {
		return err
	}
	in, err := a.Auth.FinishSetup(r.Ctx(), fin, label(r))
	if err != nil {
		return err
	}
	return a.signedIn(r, r.Op.Success, in)
}

func (a *API) checkInvite(r *Request) error {
	inv, err := a.People.CheckInvite(r.Ctx(), r.R.PathValue("code"))
	if err != nil {
		return err
	}
	out := map[string]any{"role": inv.Role}
	// A claim link says whose imported account it takes over.
	name, err := a.Auth.ClaimsUsername(r.Ctx(), inv)
	if err != nil {
		return err
	}
	if name != "" {
		out["claims_username"] = name
	}
	return r.JSON(out)
}

func (a *API) beginPasskeyRegistration(r *Request) error {
	var body struct {
		InviteCode  string `json:"invite_code"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	c, err := a.Auth.BeginRegistration(r.Ctx(), r.Addr, body.InviteCode, body.Username, body.DisplayName)
	if err != nil {
		return err
	}
	return r.JSON(c)
}

func (a *API) finishPasskeyRegistration(r *Request) error {
	var body passkeyBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	fin, err := body.finish()
	if err != nil {
		return err
	}
	in, err := a.Auth.FinishRegistration(r.Ctx(), fin, label(r))
	if err != nil {
		return err
	}
	return a.signedIn(r, r.Op.Success, in)
}

func (a *API) beginPasskeyLogin(r *Request) error {
	c, err := a.Auth.BeginLogin(r.Ctx(), r.Addr)
	if err != nil {
		return err
	}
	return r.JSON(c)
}

func (a *API) finishPasskeyLogin(r *Request) error {
	var body struct {
		CeremonyID string          `json:"ceremony_id"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	fin, err := passkeyBody{CeremonyID: body.CeremonyID, Credential: body.Credential}.finish()
	if err != nil {
		return err
	}
	in, err := a.Auth.FinishLogin(r.Ctx(), r.Addr, fin, label(r))
	if err != nil {
		return err
	}
	return a.signedIn(r, r.Op.Success, in)
}

func (a *API) logout(r *Request) error {
	if err := a.Auth.Logout(r.Ctx(), r.Who); err != nil {
		return err
	}
	if r.Who.Cookie {
		r.W.Header().Add("Set-Cookie", a.Auth.Cookie("", 0).String())
	}
	return r.NoContent()
}

func (a *API) createToken(r *Request) error {
	var body struct {
		Label            string `json:"label"`
		ExpiresInSeconds int64  `json:"expires_in_seconds"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	ses, token, err := a.Auth.CreatePersonalToken(r.Ctx(), r.Who, body.Label, body.ExpiresInSeconds)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{
		"token":  map[string]any{"id": ses.ID, "label": ses.Label, "created_at": ses.CreatedAt, "expires_at": ses.ExpiresAt},
		"secret": token,
	})
}
