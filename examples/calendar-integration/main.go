// calendar-integration tells a Churr server who is in a meeting and who is
// out of office (docs/BOTS.md section 15). The server never reads a
// calendar: a program beside it does, and says only "in a meeting until
// 10:30".
//
// THE CALENDAR IS A STUB. A real one would ask a calendar service; this
// example reads a JSON file instead (see calendar.example.json), again on
// every pass, so it can be edited while the program runs. Replace
// readCalendar() to make it real.
//
// It uses a server integration token (churr_int_…), which is not a bot and
// not an account: it is in no conversation, reads nothing, and can only do
// what an admin ticked when creating it, here setting availability.
//
//	CHURR_URL=http://localhost:8080 CHURR_INTEGRATION_TOKEN=churr_int_… \
//	  go run ./examples/calendar-integration examples/calendar-integration/calendar.example.json
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// entry is one line of the stub calendar: someone is busy from Start to
// End. Kind is "meeting" or "out_of_office".
type entry struct {
	Username string    `json:"username"`
	Kind     string    `json:"kind"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
}

// readCalendar STANDS IN FOR A CALENDAR SERVICE: it returns everyone's
// entries from a file.
func readCalendar(path string) ([]entry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []entry
	return out, json.Unmarshal(raw, &out)
}

// want is what one person's availability should be now.
type want struct {
	availability string // "", "meeting" or "out_of_office"
	until        time.Time
}

// wanted works out what a person's entries mean at this moment. Out of
// office wins over a meeting, and a meeting can be set at most 24 hours
// ahead.
func wanted(entries []entry, username string, now time.Time) want {
	var w want
	for _, e := range entries {
		if e.Username != username || now.Before(e.Start) || !now.Before(e.End) {
			continue
		}
		if e.Kind == "out_of_office" {
			return want{"out_of_office", e.End}
		}
		if e.Kind == "meeting" && e.End.After(w.until) {
			w = want{"meeting", e.End}
		}
	}
	if max := now.Add(24 * time.Hour); w.availability == "meeting" && w.until.After(max) {
		w.until = max
	}
	return w
}

type client struct {
	url, token string
	http       *http.Client
}

// refusal is the server's error shape.
type refusal struct {
	Status            int
	Code              string `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
}

func (r *refusal) Error() string { return fmt.Sprintf("%d %s: %s", r.Status, r.Code, r.Message) }

func (c *client) call(method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.url+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var e struct {
			Error refusal `json:"error"`
		}
		json.Unmarshal(raw, &e)
		e.Error.Status = resp.StatusCode
		return &e.Error
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// sync is the program's state: who it knows at Churr, and what it last set
// for each of them.
type sync struct {
	c *client
	// accounts maps a calendar user to a Churr account id. Someone not
	// found is remembered as "" and looked up again tomorrow.
	accounts map[string]string
	lookedUp map[string]time.Time
	last     map[string]want
}

// account finds the Churr account of a calendar user. The answer is the
// account's identifier and username and nothing more. (With company
// sign-in, look up by provider_id and subject instead of username.)
func (s *sync) account(username string, now time.Time) string {
	if at, tried := s.lookedUp[username]; tried && (s.accounts[username] != "" || now.Sub(at) < 24*time.Hour) {
		return s.accounts[username]
	}
	var out struct {
		Account struct {
			ID string `json:"id"`
		} `json:"account"`
	}
	err := s.c.call("GET", "/api/v1/integration/accounts?username="+url.QueryEscape(username), nil, &out)
	if err != nil {
		log.Printf("looking up %s: %v", username, err)
	}
	s.accounts[username], s.lookedUp[username] = out.Account.ID, now
	return out.Account.ID
}

// pass looks at the calendar once and tells the server what changed. It
// returns how long the server asked it to wait, if it did.
func (s *sync) pass(entries []entry, now time.Time) time.Duration {
	people := map[string]bool{}
	for _, e := range entries {
		people[e.Username] = true
	}
	for username := range s.last {
		people[username] = true // someone whose last entry has gone
	}
	for username := range people {
		w := wanted(entries, username, now)
		if w == s.last[username] {
			continue // one request per change, not per minute
		}
		id := s.account(username, now)
		if id == "" {
			continue
		}
		body := map[string]any{"availability": nil}
		if w.availability != "" {
			body = map[string]any{"availability": w.availability, "until": w.until.UTC().Format("2006-01-02T15:04:05.000Z")}
		}
		err := s.c.call("PUT", "/api/v1/integration/accounts/"+id+"/availability", body, nil)
		if r, ok := err.(*refusal); ok {
			switch {
			case r.Code == "rate_limited":
				return time.Duration(r.RetryAfterSeconds) * time.Second
			case r.Code == "conflict":
				// Clearing clears only what this integration set. The
				// person set something themselves; leave it alone.
				err = nil
			case r.Status == http.StatusNotFound:
				// The account has gone: forget it and look again
				// tomorrow.
				s.accounts[username] = ""
				continue
			}
		}
		if err != nil {
			log.Printf("setting %s: %v", username, err)
			continue
		}
		s.last[username] = w
		log.Printf("%s: %s", username, map[bool]string{true: "cleared", false: w.availability + " until " + w.until.Format(time.Kitchen)}[w.availability == ""])
	}
	return 0
}

func main() {
	token := os.Getenv("CHURR_INTEGRATION_TOKEN")
	if os.Getenv("CHURR_URL") == "" || !strings.HasPrefix(token, "churr_int_") || len(os.Args) != 2 {
		log.Fatal("usage: CHURR_URL=… CHURR_INTEGRATION_TOKEN=churr_int_… calendar-integration <calendar.json>")
	}
	s := &sync{c: &client{url: strings.TrimRight(os.Getenv("CHURR_URL"), "/"), token: token, http: &http.Client{Timeout: 30 * time.Second}},
		accounts: map[string]string{}, lookedUp: map[string]time.Time{}, last: map[string]want{}}
	for {
		wait := time.Minute
		entries, err := readCalendar(os.Args[1])
		if err != nil {
			log.Printf("reading the calendar: %v", err)
		} else if asked := s.pass(entries, time.Now()); asked > 0 {
			wait = asked
		}
		time.Sleep(wait)
	}
}
