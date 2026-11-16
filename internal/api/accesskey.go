package api

// Access keys (ruling R80, docs/AUTH.md section 5.6): making an account or
// the owner with one, signing in with one, the fresh check, and the
// caller's own key. Every body carries `access_key_auth`, the value the
// page derives from the key; the key itself never reaches the server.
// Unlocking a session with it is in lock.go.

func init() {
	register(func(a *API) {
		a.handle("setupWithAccessKey", a.setupWithAccessKey)
		a.handle("registerWithAccessKey", a.registerWithAccessKey)
		a.handle("signInWithAccessKey", a.signInWithAccessKey)
		a.handle("checkAccessKey", a.checkAccessKey)
		a.handle("getAccessKey", a.getAccessKey)
		a.handle("setAccessKey", a.setAccessKey)
		a.handle("removeAccessKey", a.removeAccessKey)
	})
}

func (a *API) setupWithAccessKey(r *Request) error {
	var body struct {
		SetupToken    string `json:"setup_token"`
		Username      string `json:"username"`
		DisplayName   string `json:"display_name"`
		AccessKeyAuth string `json:"access_key_auth"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	in, err := a.Auth.SetupWithAccessKey(r.Ctx(), r.Addr, body.SetupToken, body.Username, body.DisplayName, body.AccessKeyAuth, label(r))
	if err != nil {
		return err
	}
	return a.signedIn(r, r.Op.Success, in)
}

func (a *API) registerWithAccessKey(r *Request) error {
	var body struct {
		InviteCode    string `json:"invite_code"`
		Username      string `json:"username"`
		DisplayName   string `json:"display_name"`
		AccessKeyAuth string `json:"access_key_auth"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	in, err := a.Auth.RegisterWithAccessKey(r.Ctx(), r.Addr, body.InviteCode, body.Username, body.DisplayName, body.AccessKeyAuth, label(r))
	if err != nil {
		return err
	}
	return a.signedIn(r, r.Op.Success, in)
}

func (a *API) signInWithAccessKey(r *Request) error {
	var body struct {
		Username      string `json:"username"`
		AccessKeyAuth string `json:"access_key_auth"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	in, err := a.Auth.SignInWithAccessKey(r.Ctx(), r.Addr, body.Username, body.AccessKeyAuth, label(r))
	if err != nil {
		return err
	}
	return a.signedIn(r, r.Op.Success, in)
}

func (a *API) checkAccessKey(r *Request) error {
	var body struct {
		AccessKeyAuth string `json:"access_key_auth"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	in, err := a.Auth.CheckAccessKey(r.Ctx(), r.Who, body.AccessKeyAuth)
	if err != nil {
		return err
	}
	return a.writeSignIn(r, in)
}

func (a *API) getAccessKey(r *Request) error {
	st, err := a.Auth.AccessKey(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	out := map[string]any{"may_create": st.MayCreate}
	if st.Key != nil {
		out["access_key"] = st.Key
	}
	return r.JSON(out)
}

func (a *API) setAccessKey(r *Request) error {
	var body struct {
		AccessKeyAuth string `json:"access_key_auth"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	k, in, err := a.Auth.SetAccessKey(r.Ctx(), r.Who, body.AccessKeyAuth, label(r))
	if err != nil {
		return err
	}
	out := map[string]any{"access_key": k}
	if in != nil {
		if out["sign_in"], err = a.signInBody(r, *in); err != nil {
			return err
		}
	}
	return r.JSON(out)
}

func (a *API) removeAccessKey(r *Request) error {
	if err := a.Auth.RemoveAccessKey(r.Ctx(), r.Who); err != nil {
		return err
	}
	return r.NoContent()
}
