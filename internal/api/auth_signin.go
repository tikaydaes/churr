package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/tikaydaes/churr/internal/auth"
	"github.com/tikaydaes/churr/internal/model"
)

// Company sign-in, recovery, the second factor and the fresh check
// (docs/AUTH.md sections 6 to 10), and app sign-in (section 11).

// Pages draws the server's own pages. The web package implements it; api
// may not import a package of its own layer, so server hands it over.
type Pages interface {
	Render(w http.ResponseWriter, status int, name string, data map[string]any)
}

func init() {
	register(func(a *API) {
		a.handle("startSso", a.startSso)
		a.handle("finishOidcSso", a.finishOidcSso)
		a.handle("finishSamlSso", a.finishSamlSso)
		a.handle("getSamlMetadata", a.getSamlMetadata)
		a.handle("getSecondFactor", a.getSecondFactor)
		a.handle("recoverWithCode", a.recoverWithCode)
		a.handle("recoverWithLink", a.recoverWithLink)
		a.handle("beginPasskeyCheck", a.beginPasskeyCheck)
		a.handle("finishPasskeyCheck", a.finishPasskeyCheck)
		a.handle("checkTotp", a.checkTotp)

		a.handle("getOAuthMetadata", a.getOAuthMetadata)
		a.handle("authorize", a.authorize)
		a.handle("exchangeToken", a.exchangeToken)
		a.handle("revokeOAuthToken", a.revokeOAuthToken)
		a.handle("startPairing", a.startPairing)
		a.handle("getPairing", a.getPairing)
		a.handle("confirmPairing", a.confirmPairing)

		// Two steps of these flows are form posts from the server's own
		// pages, which the contract does not list as operations: the
		// approval of an app (docs/AUTH.md section 11.2 step 5) and the
		// username of a first company sign-in (section 7.4). They get
		// the same machinery as any operation.
		if a.mux == nil {
			return // a table built only to list handlers has no routes
		}
		a.mux.Handle("POST /oauth/authorize", a.wrap(Operation{ID: "approveAuthorization", Method: "POST", Path: "/oauth/authorize",
			Auth: AuthOptional, Area: "OAuth", Success: http.StatusFound}, a.approveAuthorization))
		a.mux.Handle("POST /sso/register", a.wrap(Operation{ID: "finishSsoRegistration", Method: "POST", Path: "/sso/register",
			Auth: AuthNone, Area: "Sign-in", Success: http.StatusOK}, a.finishSsoRegistration))
	})
}

// page draws one of the server's own pages.
func (a *API) page(r *Request, status int, name string, data map[string]any) error {
	if a.Pages == nil {
		return model.ErrUnavailable.With("This server's pages are not built in.")
	}
	a.Pages.Render(r.W, status, name, data)
	return nil
}

// problemPage shows an error as a page, for the steps a person reaches in a
// browser and not through a client.
func (a *API) problemPage(r *Request, title string, err error) error {
	e, ok := model.AsError(err)
	if !ok {
		a.Log.Error("request failed", "err", err)
		e = model.ErrInternal
	}
	if e.RetryAfterSeconds > 0 {
		r.W.Header().Set("Retry-After", strconv.Itoa(e.RetryAfterSeconds))
	}
	return a.page(r, e.Status(), "message", map[string]any{"Title": title, "Message": e.Message})
}

// signInBody is the answer of a sign-in step (schema SignIn). It sets the
// cookie when the step made or changed the session.
func (a *API) signInBody(r *Request, in auth.SignIn) (map[string]any, error) {
	out := map[string]any{"session": in.Session}
	if in.Session.PendingSecondFactor {
		st := in.SecondFactor
		if st == nil {
			got, err := a.Auth.SecondFactor(r.Ctx(), auth.Who{Session: in.Session, Account: in.Account})
			if err != nil {
				return nil, err
			}
			st = &got
		}
		out["second_factor"] = st
	} else {
		me, err := a.People.Me(r.Ctx(), in.Account)
		if err != nil {
			return nil, err
		}
		out["account"] = me
	}
	if len(in.RecoveryCodes) > 0 {
		out["recovery_codes"] = in.RecoveryCodes
	}
	if in.Token != "" {
		r.W.Header().Add("Set-Cookie", a.Auth.Cookie(in.Token, in.Session.ExpiresAt).String())
	}
	return out, nil
}

func (a *API) writeSignIn(r *Request, in auth.SignIn) error {
	body, err := a.signInBody(r, in)
	if err != nil {
		return err
	}
	return r.JSON(body)
}

func (a *API) startSso(r *Request) error {
	q := r.R.URL.Query()
	var who *auth.Who
	if r.SignedIn {
		who = &r.Who
	}
	start, err := a.Auth.StartSso(r.Ctx(), r.Addr, who, r.R.PathValue("provider"), q.Get("purpose"), q.Get("invite_code"), q.Get("return_to"))
	if err != nil {
		return err
	}
	r.W.Header().Add("Set-Cookie", start.Cookie.String())
	r.W.Header().Set("Cache-Control", "no-store")
	http.Redirect(r.W, r.R, start.Location, http.StatusFound)
	return nil
}

// ssoCookie reads the binding cookie and clears it: it is used once.
func (a *API) ssoCookie(r *Request) string {
	value := ""
	if c, err := r.R.Cookie(auth.SsoCookie); err == nil {
		value = c.Value
	}
	if clear := a.Auth.ClearSsoCookie(r.Ctx(), r.R.PathValue("provider")); clear != nil {
		r.W.Header().Add("Set-Cookie", clear.String())
	}
	return value
}

// ssoDone answers the return from a provider with a page from this server's
// own origin, never a redirect: the session cookie is SameSite=Strict, and
// a browser does not send a Strict cookie along a redirect chain that began
// at another site.
func (a *API) ssoDone(r *Request, res auth.SsoResult, err error) error {
	if err != nil {
		return a.problemPage(r, "Sign-in failed", err)
	}
	if res.Register != nil {
		p := res.Register
		return a.page(r, http.StatusOK, "register", map[string]any{"Title": "Choose a username", "Token": p.Token, "Username": p.Username,
			"DisplayName": p.DisplayName, "ProviderName": p.ProviderName, "Error": p.Error})
	}
	if res.SignIn != nil && res.SignIn.Token != "" {
		r.W.Header().Add("Set-Cookie", a.Auth.Cookie(res.SignIn.Token, res.SignIn.Session.ExpiresAt).String())
	}
	next := res.Next
	if next == "" {
		next = "/"
	}
	return a.page(r, http.StatusOK, "continue", map[string]any{"Next": next})
}

func (a *API) finishOidcSso(r *Request) error {
	cookie := a.ssoCookie(r)
	res, err := a.Auth.OidcCallback(r.Ctx(), r.Addr, r.R.PathValue("provider"), r.R.URL.Query(), cookie, label(r))
	return a.ssoDone(r, res, err)
}

func (a *API) finishSamlSso(r *Request) error {
	cookie := a.ssoCookie(r)
	// The answer is at most 256 KB; base64 and the form add about a third.
	r.R.Body = http.MaxBytesReader(r.W, r.R.Body, 512<<10)
	if err := r.R.ParseForm(); err != nil {
		return a.problemPage(r, "Sign-in failed", model.BadRequest("The answer from the provider could not be read."))
	}
	res, err := a.Auth.SamlCallback(r.Ctx(), r.Addr, r.R.PathValue("provider"), r.R.PostForm.Get("SAMLResponse"), r.R.PostForm.Get("RelayState"), cookie, label(r))
	return a.ssoDone(r, res, err)
}

func (a *API) finishSsoRegistration(r *Request) error {
	if err := r.R.ParseForm(); err != nil {
		return a.problemPage(r, "Sign-in failed", model.BadRequest("The form could not be read."))
	}
	f := r.R.PostForm
	res, err := a.Auth.CompleteRegistration(r.Ctx(), r.Addr, f.Get("token"), f.Get("username"), f.Get("display_name"), label(r))
	return a.ssoDone(r, res, err)
}

func (a *API) getSamlMetadata(r *Request) error {
	doc, err := a.Auth.SamlMetadata(r.Ctx(), r.Addr, r.R.PathValue("provider"))
	if err != nil {
		return err
	}
	r.W.Header().Set("Content-Type", "application/samlmetadata+xml")
	r.W.Write(doc)
	return nil
}

func (a *API) getSecondFactor(r *Request) error {
	st, err := a.Auth.SecondFactor(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"second_factor": st})
}

func (a *API) recoverWithCode(r *Request) error {
	var body struct {
		Username string `json:"username"`
		Code     string `json:"code"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	in, err := a.Auth.RecoverWithCode(r.Ctx(), r.Addr, body.Username, body.Code, label(r))
	if err != nil {
		return err
	}
	return a.writeSignIn(r, in)
}

func (a *API) recoverWithLink(r *Request) error {
	var body struct {
		Token string `json:"token"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	in, err := a.Auth.RecoverWithLink(r.Ctx(), r.Addr, body.Token, label(r))
	if err != nil {
		return err
	}
	return a.writeSignIn(r, in)
}

func (a *API) beginPasskeyCheck(r *Request) error {
	c, err := a.Auth.BeginPasskeyCheck(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(c)
}

func (a *API) finishPasskeyCheck(r *Request) error {
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
	in, err := a.Auth.FinishPasskeyCheck(r.Ctx(), r.Who, fin)
	if err != nil {
		return err
	}
	return a.writeSignIn(r, in)
}

func (a *API) checkTotp(r *Request) error {
	var body struct {
		Code string `json:"code"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Code == "" {
		return model.BadRequest("code is needed.")
	}
	in, err := a.Auth.CheckTotp(r.Ctx(), r.Who, body.Code)
	if err != nil {
		return err
	}
	return a.writeSignIn(r, in)
}

// --- OAuth for the apps ---

func (a *API) getOAuthMetadata(r *Request) error {
	if err := a.Auth.LimitWellKnown(r.Addr); err != nil {
		return err
	}
	return r.JSON(a.Auth.OAuthMetadata())
}

// newSessionCookie marks a browser that reached the authorization page
// without a session. A session made after that moment exists only to
// approve the app, and is revoked once the code is issued.
const newSessionCookie = "churr_oauth_new"

// authorizeParams are the query parameters of an authorization request.
var authorizeParams = []string{"response_type", "client_id", "redirect_uri", "code_challenge", "code_challenge_method", "state", "platform", "box_key"}

// authorize draws the page an app opens in the system browser. It checks
// the redirect address and PKCE values before anything else and never
// redirects to an address it has not accepted.
func (a *API) authorize(r *Request) error {
	q := r.R.URL.Query()
	req, err := a.Auth.CheckAuthorize(r.Addr, q)
	if err != nil {
		return a.problemPage(r, "This app cannot be signed in", err)
	}
	data := map[string]any{"Title": "Sign in to Churr", "DeviceName": req.DeviceName, "Platform": req.Platform}
	if r.SignedIn && r.Who.Cookie && r.Who.Session.Kind == model.SessionWeb {
		params := []map[string]string{}
		for _, name := range authorizeParams {
			if v := q.Get(name); v != "" {
				params = append(params, map[string]string{"name": name, "value": v})
			}
		}
		data["State"], data["Title"], data["Username"], data["Params"] = "approve", "Approve this app", r.Who.Account.Username, params
		return a.page(r, http.StatusOK, "authorize", data)
	}
	// A session that still owes its second step finishes it here.
	if who, err := a.Auth.Authenticate(r.R, r.Addr); err == nil && who.Cookie && who.Session.PendingSecondFactor {
		st, err := a.Auth.SecondFactor(r.Ctx(), who)
		if err != nil {
			return a.problemPage(r, "Sign-in failed", err)
		}
		has := func(list []string, v string) bool {
			for _, x := range list {
				if x == v {
					return true
				}
			}
			return false
		}
		data["State"] = "second_factor"
		data["HasPasskey"], data["HasTotp"] = has(st.SetUp, "passkey"), has(st.SetUp, "totp")
		data["Enrol"] = len(st.SetUp) == 0
		data["MayPasskey"], data["MayTotp"] = has(st.Accepted, "passkey"), has(st.Accepted, "totp")
		return a.page(r, http.StatusOK, "authorize", data)
	}
	server, err := a.serverObject(r.Ctx(), false)
	if err != nil {
		return a.problemPage(r, "Sign-in failed", err)
	}
	signIn, _ := server["sign_in"].(map[string]any)
	data["State"], data["Providers"], data["ReturnTo"] = "sign_in", signIn["providers"], r.R.URL.RequestURI()
	// Admins can always use a passkey, so the button is always there;
	// the server refuses a member's passkey when the policy says so.
	data["Passkey"] = true
	http.SetCookie(r.W, &http.Cookie{Name: newSessionCookie, Value: strconv.FormatInt(int64(model.Now()), 10), Path: "/oauth",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	return a.page(r, http.StatusOK, "authorize", data)
}

// approveAuthorization is the button on the approval page: a same-origin
// POST with the session cookie.
func (a *API) approveAuthorization(r *Request) error {
	if err := r.R.ParseForm(); err != nil {
		return a.problemPage(r, "This app cannot be signed in", model.BadRequest("The form could not be read."))
	}
	f := r.R.PostForm
	req, err := a.Auth.CheckAuthorize(r.Addr, f)
	if err != nil {
		return a.problemPage(r, "This app cannot be signed in", err)
	}
	if !r.SignedIn || !r.Who.Cookie {
		return a.problemPage(r, "This app cannot be signed in", model.ErrUnauthenticated.With("Your session ended. Go back to the app and start again."))
	}
	if f.Get("decision") != "approve" {
		http.Redirect(r.W, r.R, a.Auth.Deny(req), http.StatusFound)
		return nil
	}
	// The browser session was made only for this if it began after the
	// page was first shown to a browser with no session.
	made := false
	if c, err := r.R.Cookie(newSessionCookie); err == nil {
		if at, perr := strconv.ParseInt(c.Value, 10, 64); perr == nil && int64(r.Who.Session.CreatedAt) >= at {
			made = true
		}
	}
	// The page may hand over the backup key, sealed to the app's box key
	// (docs/SEALED.md section 13.3). The server stores it with the code
	// and cannot open it.
	if h := f.Get("handover"); h != "" {
		raw, derr := model.DecodeBytes(h)
		if derr != nil {
			return a.problemPage(r, "This app cannot be signed in", model.Invalid("handover", "handover is base64url."))
		}
		if req, err = req.WithHandover(raw); err != nil {
			return a.problemPage(r, "This app cannot be signed in", err)
		}
	}
	to, err := a.Auth.Approve(r.Ctx(), r.Who, req, made)
	if err != nil {
		return a.problemPage(r, "This app cannot be signed in", err)
	}
	http.SetCookie(r.W, &http.Cookie{Name: newSessionCookie, Value: "", Path: "/oauth", HttpOnly: true, MaxAge: -1})
	if made {
		r.W.Header().Add("Set-Cookie", a.Auth.Cookie("", 0).String())
	}
	r.W.Header().Set("Cache-Control", "no-store")
	http.Redirect(r.W, r.R, to, http.StatusFound)
	return nil
}

// oauthJSON writes a token endpoint answer. Errors use the OAuth error
// shape, not the API's, because OAuth libraries expect it.
func oauthJSON(w http.ResponseWriter, status int, v any) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write(b)
}

func (a *API) exchangeToken(r *Request) error {
	if err := r.R.ParseForm(); err != nil {
		oauthJSON(r.W, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": "The form could not be read."})
		return nil
	}
	tokens, err := a.Auth.Token(r.Ctx(), r.Addr, r.R.PostForm)
	var oe *auth.OAuthError
	switch {
	case err == nil:
		oauthJSON(r.W, http.StatusOK, tokens)
	case errors.As(err, &oe):
		if oe.RetryAfter > 0 {
			r.W.Header().Set("Retry-After", strconv.Itoa(oe.RetryAfter))
		}
		oauthJSON(r.W, oe.Status, oe)
	default:
		a.Log.Error("the token endpoint failed", "err", err)
		oauthJSON(r.W, http.StatusInternalServerError, map[string]string{"error": "server_error"})
	}
	return nil
}

func (a *API) revokeOAuthToken(r *Request) error {
	if err := r.R.ParseForm(); err == nil {
		a.Auth.RevokeToken(r.Ctx(), r.R.PostForm.Get("token"))
	}
	// RFC 7009: the answer is always success.
	oauthJSON(r.W, http.StatusOK, map[string]any{})
	return nil
}

func (a *API) startPairing(r *Request) error {
	p, err := a.Auth.StartPairing(r.Ctx(), r.Who)
	if err != nil {
		return err
	}
	return r.JSON(p)
}

func (a *API) getPairing(r *Request) error {
	st, err := a.Auth.GetPairing(r.Ctx(), r.Who, r.R.PathValue("code"))
	if err != nil {
		return err
	}
	return r.JSON(st)
}

func (a *API) confirmPairing(r *Request) error {
	// An unlocked web chat may send the two-layer hand-over with its
	// confirmation (docs/SEALED.md section 13.7). The body is optional.
	var body struct {
		Handover model.Bytes `json:"handover"`
	}
	if r.R.ContentLength != 0 {
		if err := r.Decode(&body); err != nil {
			return err
		}
	}
	if err := a.Auth.ConfirmPairingWithHandover(r.Ctx(), r.Who, r.R.PathValue("code"), body.Handover); err != nil {
		return err
	}
	return r.NoContent()
}
