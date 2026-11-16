package api

// The lock (ruling R75, docs/AUTH.md section 10.6): a session its holder
// locked answers only these operations, and logout, until a passkey
// assertion or the access key unlocks it. The refusal of everything else is in serve (api.go).

func init() {
	register(func(a *API) {
		a.handle("lockSession", a.lockSession)
		a.handle("beginSessionUnlock", a.beginSessionUnlock)
		a.handle("getSessionUnlockOptions", a.getSessionUnlockOptions)
		a.handle("finishSessionUnlock", a.finishSessionUnlock)
		a.handle("finishSessionUnlockWithAccessKey", a.finishSessionUnlockWithAccessKey)
	})
}

// lockedAllowed is what a locked session may still do. The two unlock
// operations that need no credential are not listed: they are open to
// anyone holding the ceremony.
var lockedAllowed = map[string]bool{"lockSession": true, "beginSessionUnlock": true, "logout": true}

func (a *API) lockSession(r *Request) error {
	if err := a.Auth.LockSession(r.Ctx(), r.Who); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) beginSessionUnlock(r *Request) error {
	u, err := a.Auth.BeginSessionUnlock(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(u)
}

func (a *API) getSessionUnlockOptions(r *Request) error {
	var body struct {
		CeremonyID string `json:"ceremony_id"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	opts, err := a.Auth.SessionUnlockOptions(body.CeremonyID)
	if err != nil {
		return err
	}
	return r.JSON(opts)
}

func (a *API) finishSessionUnlock(r *Request) error {
	var body passkeyBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	fin, err := body.finish()
	if err != nil {
		return err
	}
	if err := a.Auth.FinishSessionUnlock(r.Ctx(), fin); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) finishSessionUnlockWithAccessKey(r *Request) error {
	var body struct {
		CeremonyID    string `json:"ceremony_id"`
		AccessKeyAuth string `json:"access_key_auth"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if err := a.Auth.FinishSessionUnlockWithAccessKey(r.Ctx(), body.CeremonyID, body.AccessKeyAuth); err != nil {
		return err
	}
	return r.NoContent()
}
