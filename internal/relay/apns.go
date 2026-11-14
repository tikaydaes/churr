package relay

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"
)

// Apple's push service (docs/PUSH.md section 7, "APNs"): HTTP/2 to
// api.push.apple.com with token-based authentication.

const (
	apnsProduction = "https://api.push.apple.com"
	apnsSandbox    = "https://api.sandbox.push.apple.com"
	// apnsTokenLife is how long one signed token is reused. Apple refuses
	// tokens older than an hour and a relay that makes new ones too often.
	apnsTokenLife = 50 * time.Minute
	// pushLife is the fixed lifetime of every push: three days. The
	// phone, not the relay, decides that an old ring is a missed call.
	pushLife = 3 * 24 * time.Hour
)

// APNsConfig is what the APNs client is made from.
type APNsConfig struct {
	// KeyFile is the path to Apple's .p8 signing key; Key the key itself,
	// which tests give instead.
	KeyFile string
	Key     *ecdsa.PrivateKey
	// KeyID is that key's identifier and TeamID the developer team.
	KeyID, TeamID string
	// Topic is the iPhone app's bundle identifier.
	Topic string
	// Sandbox selects Apple's development push service.
	Sandbox bool
	// URL replaces the service's address and HTTP the client; tests
	// point them at an in-process Apple.
	URL  string
	HTTP *http.Client
	Now  func() time.Time
}

// APNs sends wake-ups to Apple.
type APNs struct {
	cfg  APNsConfig
	http *http.Client
	now  func() time.Time

	mu      sync.Mutex
	token   string
	tokenAt time.Time
}

// NewAPNs makes the client and reads the signing key.
func NewAPNs(c APNsConfig) (*APNs, error) {
	if c.Key == nil {
		raw, err := os.ReadFile(c.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("reading the Apple signing key: %w", err)
		}
		block, _ := pem.Decode(raw)
		if block == nil {
			return nil, errors.New("the Apple signing key is not a .p8 file")
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, errors.New("the Apple signing key could not be read")
		}
		key, ok := parsed.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errors.New("the Apple signing key is not an elliptic-curve key")
		}
		c.Key = key
	}
	if c.KeyID == "" || c.TeamID == "" || c.Topic == "" {
		return nil, errors.New("the Apple key identifier, team identifier and topic are all needed")
	}
	if c.URL == "" {
		c.URL = apnsProduction
		if c.Sandbox {
			c.URL = apnsSandbox
		}
	}
	a := &APNs{cfg: c, http: c.HTTP, now: c.Now}
	if a.http == nil {
		// One HTTP/2 connection, kept open.
		a.http = &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{ForceAttemptHTTP2: true, MaxIdleConnsPerHost: 2, IdleConnTimeout: time.Hour}}
	}
	if a.now == nil {
		a.now = time.Now
	}
	return a, nil
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// bearer returns the signed token, making a new one when the last is fifty
// minutes old: ES256, with the key identifier in the header and the team
// identifier and issue time in the body.
func (a *APNs) bearer() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if a.token != "" && now.Sub(a.tokenAt) < apnsTokenLife {
		return a.token, nil
	}
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": a.cfg.KeyID})
	claims, _ := json.Marshal(map[string]any{"iss": a.cfg.TeamID, "iat": now.Unix()})
	signed := b64url(header) + "." + b64url(claims)
	digest := sha256.Sum256([]byte(signed))
	r, s, err := ecdsa.Sign(rand.Reader, a.cfg.Key, digest[:])
	if err != nil {
		return "", err
	}
	// The signature of a JSON web token is r and s, 32 bytes each.
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	a.token, a.tokenAt = signed+"."+b64url(sig), now
	return a.token, nil
}

// apnsPayload is the exact payload: a placeholder alert the app's
// notification extension replaces, the event identifier and the collapse
// key. The relay holds no wording: loc-key names a string in the app.
func apnsPayload(w Wake) []byte {
	aps := map[string]any{"alert": map[string]string{"loc-key": "WAKE_FALLBACK"}, "mutable-content": 1, "sound": "default"}
	if w.Badge != nil {
		aps["badge"] = *w.Badge
	}
	b, _ := json.Marshal(map[string]any{"aps": aps, "e": w.EventID, "c": w.CollapseKey})
	return b
}

// Send sends one wake-up to Apple and maps the answer.
func (a *APNs) Send(ctx context.Context, w Wake) Result {
	token, err := a.bearer()
	if err != nil {
		return Result{Outcome: KeyError}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.URL+"/3/device/"+url.PathEscape(w.PushAddress), bytes.NewReader(apnsPayload(w)))
	if err != nil {
		return Result{Outcome: Gone} // an address that cannot be put in a path is no address
	}
	priority := "5"
	if w.High {
		priority = "10"
	}
	req.Header.Set("authorization", "bearer "+token)
	req.Header.Set("apns-topic", a.cfg.Topic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", priority)
	req.Header.Set("apns-collapse-id", w.CollapseKey)
	req.Header.Set("apns-expiration", strconv.FormatInt(a.now().Add(pushLife).Unix(), 10))
	req.Header.Set("content-type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return Result{Outcome: UpstreamError}
	}
	defer resp.Body.Close()
	var answer struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&answer)
	switch {
	case resp.StatusCode == http.StatusOK:
		return Result{Outcome: Sent}
	case resp.StatusCode == http.StatusGone:
		return Result{Outcome: Gone}
	case resp.StatusCode == http.StatusBadRequest && (answer.Reason == "BadDeviceToken" || answer.Reason == "DeviceTokenNotForTopic"):
		return Result{Outcome: Gone}
	case resp.StatusCode == http.StatusTooManyRequests:
		return Result{Outcome: RateLimited}
	case resp.StatusCode == http.StatusForbidden:
		// The relay's own key is wrong. The token is dropped so the next
		// request signs a new one.
		a.mu.Lock()
		a.token = ""
		a.mu.Unlock()
		return Result{Outcome: KeyError}
	}
	return Result{Outcome: UpstreamError}
}
