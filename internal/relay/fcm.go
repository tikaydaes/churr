package relay

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Google's push service (docs/PUSH.md section 7, "FCM"): the HTTP v1
// interface with an access token got from a service account.

const (
	fcmBase  = "https://fcm.googleapis.com"
	fcmScope = "https://www.googleapis.com/auth/firebase.messaging"
	// fcmRenewBefore is how long before it runs out an access token is
	// replaced.
	fcmRenewBefore = 5 * time.Minute
)

// FCMConfig is what the FCM client is made from.
type FCMConfig struct {
	// CredentialsFile is the path to the Google service account file;
	// Credentials its contents, which tests give instead.
	CredentialsFile string
	Credentials     []byte
	// URL replaces the service's address and HTTP the client; tests
	// point them at an in-process Google. The token address comes from
	// the credentials.
	URL  string
	HTTP *http.Client
	Now  func() time.Time
}

// serviceAccount is the part of a Google service account file the relay
// reads.
type serviceAccount struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// FCM sends wake-ups to Google.
type FCM struct {
	cfg     FCMConfig
	account serviceAccount
	key     *rsa.PrivateKey
	http    *http.Client
	now     func() time.Time

	mu      sync.Mutex
	access  string
	expires time.Time
}

// NewFCM makes the client and reads the service account.
func NewFCM(c FCMConfig) (*FCM, error) {
	if c.Credentials == nil {
		raw, err := os.ReadFile(c.CredentialsFile)
		if err != nil {
			return nil, fmt.Errorf("reading the Google service account file: %w", err)
		}
		c.Credentials = raw
	}
	f := &FCM{cfg: c, http: c.HTTP, now: c.Now}
	if err := json.Unmarshal(c.Credentials, &f.account); err != nil {
		return nil, errors.New("the Google service account file is not JSON")
	}
	if f.account.ProjectID == "" || f.account.ClientEmail == "" || f.account.TokenURI == "" {
		return nil, errors.New("the Google service account file needs project_id, client_email, private_key and token_uri")
	}
	block, _ := pem.Decode([]byte(f.account.PrivateKey))
	if block == nil {
		return nil, errors.New("the Google service account file holds no private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("the Google service account's private key could not be read")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("the Google service account's private key is not an RSA key")
	}
	f.key = key
	if f.cfg.URL == "" {
		f.cfg.URL = fcmBase
	}
	if f.http == nil {
		f.http = &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{ForceAttemptHTTP2: true, MaxIdleConnsPerHost: 4, IdleConnTimeout: 10 * time.Minute}}
	}
	if f.now == nil {
		f.now = time.Now
	}
	return f, nil
}

// token returns an access token with the firebase.messaging scope,
// exchanging a signed assertion for a new one before the last runs out.
func (f *FCM) token(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	if f.access != "" && now.Before(f.expires.Add(-fcmRenewBefore)) {
		return f.access, nil
	}
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{"iss": f.account.ClientEmail, "scope": fcmScope, "aud": f.account.TokenURI,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})
	signed := b64url(header) + "." + b64url(claims)
	digest := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {signed + "." + b64url(sig)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.account.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := f.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var answer struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&answer); err != nil || resp.StatusCode != http.StatusOK || answer.AccessToken == "" {
		return "", errKey{status: resp.StatusCode}
	}
	if answer.ExpiresIn <= 0 {
		answer.ExpiresIn = 3600
	}
	f.access, f.expires = answer.AccessToken, now.Add(time.Duration(answer.ExpiresIn)*time.Second)
	return f.access, nil
}

// errKey is Google refusing the relay's own credentials.
type errKey struct{ status int }

func (e errKey) Error() string {
	return "the token exchange was refused with status " + strconv.Itoa(e.status)
}

// fcmBody is the exact body: a data message with no notification part, so
// Android shows nothing by itself and hands the message to the app. The
// collapse key travels as data, not as FCM's own collapse key.
func fcmBody(w Wake) []byte {
	data := map[string]string{"e": w.EventID, "c": w.CollapseKey}
	if w.Badge != nil {
		data["b"] = strconv.Itoa(*w.Badge)
	}
	priority := "NORMAL"
	if w.High {
		priority = "HIGH"
	}
	b, _ := json.Marshal(map[string]any{"message": map[string]any{
		"token": w.PushAddress, "data": data, "android": map[string]string{"priority": priority, "ttl": "259200s"},
	}})
	return b
}

// Send sends one wake-up to Google and maps the answer.
func (f *FCM) Send(ctx context.Context, w Wake) Result {
	token, err := f.token(ctx)
	if err != nil {
		var refused errKey
		if errors.As(err, &refused) && refused.status >= 400 && refused.status < 500 {
			return Result{Outcome: KeyError}
		}
		return Result{Outcome: UpstreamError}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.cfg.URL+"/v1/projects/"+url.PathEscape(f.account.ProjectID)+"/messages:send", bytes.NewReader(fcmBody(w)))
	if err != nil {
		return Result{Outcome: UpstreamError}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.http.Do(req)
	if err != nil {
		return Result{Outcome: UpstreamError}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return Result{Outcome: Sent}
	}
	var answer struct {
		Error struct {
			Status  string `json:"status"`
			Message string `json:"message"`
			Details []struct {
				ErrorCode       string `json:"errorCode"`
				FieldViolations []struct {
					Field string `json:"field"`
				} `json:"fieldViolations"`
			} `json:"details"`
		} `json:"error"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&answer)
	code, namesToken := answer.Error.Status, strings.Contains(strings.ToLower(answer.Error.Message), "token")
	for _, d := range answer.Error.Details {
		if d.ErrorCode != "" {
			code = d.ErrorCode
		}
		for _, v := range d.FieldViolations {
			namesToken = namesToken || strings.HasSuffix(v.Field, "token")
		}
	}
	switch {
	case code == "UNREGISTERED" || resp.StatusCode == http.StatusNotFound:
		return Result{Outcome: Gone}
	case code == "SENDER_ID_MISMATCH":
		// The token belongs to a different app.
		return Result{Outcome: Gone}
	case code == "INVALID_ARGUMENT" && namesToken:
		return Result{Outcome: Gone}
	case code == "QUOTA_EXCEEDED" || resp.StatusCode == http.StatusTooManyRequests:
		res := Result{Outcome: RateLimited}
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
			res.RetryAfter = time.Duration(secs) * time.Second
		}
		return res
	case resp.StatusCode == http.StatusUnauthorized || (resp.StatusCode == http.StatusForbidden && code != "SENDER_ID_MISMATCH"):
		// The access token was refused: the relay's own credentials.
		f.mu.Lock()
		f.access = ""
		f.mu.Unlock()
		return Result{Outcome: KeyError}
	}
	// UNAVAILABLE, INTERNAL, and anything unexpected.
	return Result{Outcome: UpstreamError}
}
