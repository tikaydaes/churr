package push

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// Delivery of a wake-up: one request to the relay or to a UnifiedPush
// endpoint, the retries of docs/PUSH.md section 5, and the forgetting
// rules of section 6.

const (
	// attemptTimeout is how long one attempt may take.
	attemptTimeout = 10 * time.Second
	// maxWaiting is how many wake-ups may wait to be retried. Past it the
	// oldest is dropped first.
	maxWaiting = 10000
	// maxRetryAfter is the longest a relay may ask this server to wait
	// before the one further attempt a rate limit allows.
	maxRetryAfter = 2 * time.Minute
)

// wakeUp is one wake-up on its way to one device.
type wakeUp struct {
	device      model.Device
	eventID     string
	collapseKey string
	priority    string
	badge       *int64
	// relay is the relay's address, for apns and fcm.
	relay string
}

// RelayRequest is the body of POST /v1/wake, exactly the fields of
// docs/PUSH.md section 5 and no others. It carries no text, no name and no
// conversation identifier, and a ring looks the same as any other event.
type RelayRequest struct {
	PushAddress string `json:"push_address"`
	Platform    string `json:"platform"`
	EventID     string `json:"event_id"`
	CollapseKey string `json:"collapse_key"`
	Priority    string `json:"priority"`
	Badge       *int64 `json:"badge,omitempty"`
}

// EndpointBody is what is posted to a UnifiedPush endpoint: the same
// wake-up with the address and platform left off.
type EndpointBody struct {
	EventID     string `json:"event_id"`
	CollapseKey string `json:"collapse_key"`
	Priority    string `json:"priority"`
	Badge       *int64 `json:"badge,omitempty"`
}

// What one attempt came to.
type outcome int

const (
	// sent: accepted. The device's failure mark is cleared.
	sent outcome = iota
	// gone: the address is dead. It is forgotten at once.
	gone
	// again: a failure worth retrying on the schedule.
	again
	// later: a rate limit. One more attempt after the stated time.
	later
	// bug: the request was refused as malformed, or the relay has no key
	// for the platform. Logged once, never retried.
	bug
)

// waitingWake is a wake-up waiting for its next attempt.
type waitingWake struct {
	drop chan struct{}
}

// deliver sends a wake-up in the background and retries it as the
// document says.
func (s *Service) deliver(w wakeUp) {
	select {
	case <-s.stop:
		return
	default:
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		limited := false
		for attempt := 0; ; attempt++ {
			s.sem <- struct{}{}
			out, wait := s.attempt(w)
			<-s.sem
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			switch out {
			case sent:
				s.store.Devices().PushSucceeded(ctx, w.device.ID)
			case gone:
				s.store.Devices().ForgetPush(ctx, w.device.ID, s.now())
			case again, later:
				// The mark holds the first failure of the run; seven
				// days of failures forget the address (the janitor).
				s.store.Devices().PushFailed(ctx, w.device.ID, s.now())
			case bug:
				s.loggedBug.Do(func() {
					s.log.Error("a wake-up was refused as malformed or for a platform the relay has no key for; it is not retried")
				})
			}
			cancel()
			switch out {
			case again:
				if attempt >= len(s.retry) {
					return // tried on the whole schedule: dropped
				}
				wait = s.retry[attempt]
			case later:
				// One more attempt, only if the wait is under two
				// minutes; otherwise the wake-up is dropped.
				if limited || wait <= 0 || wait >= maxRetryAfter {
					return
				}
				limited = true
			default:
				return
			}
			if !s.sleep(wait) {
				return
			}
		}
	}()
}

// sleep waits before a retry. It reports false if the wake-up was dropped
// meanwhile: the server is stopping, or too many wake-ups are waiting and
// this is the oldest.
func (s *Service) sleep(d time.Duration) bool {
	me := &waitingWake{drop: make(chan struct{})}
	s.waitMu.Lock()
	s.waiting = append(s.waiting, me)
	for len(s.waiting) > maxWaiting {
		close(s.waiting[0].drop)
		s.waiting = s.waiting[1:]
	}
	s.waitMu.Unlock()
	t := time.NewTimer(d)
	defer t.Stop()
	ok := false
	select {
	case <-t.C:
		ok = true
	case <-me.drop:
		return false
	case <-s.stop:
	}
	s.waitMu.Lock()
	for i, w := range s.waiting {
		if w == me {
			s.waiting = append(s.waiting[:i], s.waiting[i+1:]...)
			break
		}
	}
	s.waitMu.Unlock()
	return ok
}

// attempt makes one attempt at one wake-up.
func (s *Service) attempt(w wakeUp) (outcome, time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), attemptTimeout)
	defer cancel()
	if w.device.PushKind == model.PushUnifiedPush {
		return s.attemptEndpoint(ctx, w)
	}
	return s.attemptRelay(ctx, w)
}

func (s *Service) attemptRelay(ctx context.Context, w wakeUp) (outcome, time.Duration) {
	platform := "ios"
	if w.device.PushKind == model.PushFCM {
		platform = "android"
	}
	body, _ := json.Marshal(RelayRequest{PushAddress: w.device.PushAddress, Platform: platform, EventID: w.eventID,
		CollapseKey: w.collapseKey, Priority: w.priority, Badge: w.badge})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(w.relay, "/")+"/v1/wake", bytes.NewReader(body))
	if err != nil {
		return bug, 0
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.relayHTTP.Do(req)
	if err != nil {
		return again, 0 // a timeout or a network error
	}
	defer resp.Body.Close()
	var answer struct {
		Error struct {
			Code              string `json:"code"`
			RetryAfterSeconds int    `json:"retry_after_seconds"`
			PlatformOff       bool   `json:"platform_off"`
		} `json:"error"`
		// The relay may also put these beside the error.
		RetryAfterSeconds int  `json:"retry_after_seconds"`
		PlatformOff       bool `json:"platform_off"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 8<<10)).Decode(&answer)
	switch resp.StatusCode {
	case http.StatusOK:
		return sent, 0
	case http.StatusGone:
		return gone, 0
	case http.StatusTooManyRequests:
		secs := max(answer.Error.RetryAfterSeconds, answer.RetryAfterSeconds)
		return later, time.Duration(secs) * time.Second
	case http.StatusBadGateway:
		return again, 0
	case http.StatusServiceUnavailable:
		if answer.Error.PlatformOff || answer.PlatformOff {
			return bug, 0
		}
		return again, 0
	}
	if resp.StatusCode >= 500 {
		return again, 0
	}
	// 400 and 413: this is a bug, and trying again would not help.
	return bug, 0
}

func (s *Service) attemptEndpoint(ctx context.Context, w wakeUp) (outcome, time.Duration) {
	u, err := url.Parse(w.device.PushAddress)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return gone, 0
	}
	body, _ := json.Marshal(EndpointBody{EventID: w.eventID, CollapseKey: w.collapseKey, Priority: w.priority, Badge: w.badge})
	contentType := "application/json"
	encrypted := false
	if len(w.device.PushPublicKey) > 0 && len(w.device.PushAuth) > 0 {
		// Some distributors accept nothing but Web Push. The body is
		// content-free either way.
		sealed, err := webPushEncrypt(rand.Reader, w.device.PushPublicKey, w.device.PushAuth, body)
		if err != nil {
			return gone, 0 // keys that cannot be encrypted to never will be
		}
		body, contentType, encrypted = sealed, "application/octet-stream", true
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.device.PushAddress, bytes.NewReader(body))
	if err != nil {
		return gone, 0
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("TTL", "259200")
	req.Header.Set("Urgency", w.priority)
	if encrypted {
		req.Header.Set("Content-Encoding", "aes128gcm")
	}
	resp, err := s.endpointHTTP.Do(req)
	if err != nil {
		return again, 0
	}
	defer resp.Body.Close()
	// The answer's body is not read beyond 1 KB.
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return sent, 0
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return gone, 0
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return again, 0
	}
	// A redirect (not followed) or another refusal: counted as a failure
	// of this attempt, and tried again on the schedule.
	return again, 0
}
