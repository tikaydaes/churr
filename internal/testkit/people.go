package testkit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/tikaydaes/churr/internal/model"
)

// Helpers for tests of the people area.

// SendForm sends one file as multipart/form-data in the named part and
// decodes the JSON answer.
func (c *Client) SendForm(method, path, part, filename string, content []byte) (int, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	w, err := mw.CreateFormFile(part, filename)
	if err != nil {
		c.t.Fatal(err)
	}
	w.Write(content)
	mw.Close()
	req, err := http.NewRequest(method, c.ts.URL+path, &buf)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	hc := c.http
	if c.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.Bearer)
		hc = &http.Client{}
	} else if !c.NoOrigin {
		req.Header.Set("Origin", c.ts.Origin)
	}
	resp, err := hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	raw, _ := io.ReadAll(resp.Body)
	if len(raw) > 0 {
		json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out
}

// Get sends a GET with extra headers and returns the status, the response
// headers and the body as it arrived, for tests of caching.
func (c *Client) Get(path string, headers map[string]string) (int, http.Header, []byte) {
	c.t.Helper()
	req, err := http.NewRequest("GET", c.ts.URL+path, nil)
	if err != nil {
		c.t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	hc := c.http
	if c.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.Bearer)
		hc = &http.Client{}
	}
	resp, err := hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, raw
}

// Imported makes an account as the Slack importer leaves one: local, with
// the role member, and not yet claimed. Nothing in the API makes one.
func (ts *TestServer) Imported(username string) model.AccountID {
	ts.t.Helper()
	a := model.Account{Username: username, DisplayName: username, Role: model.RoleMember}
	if err := ts.Server.Store.Accounts().CreateUnclaimed(context.Background(), &a); err != nil {
		ts.t.Fatal(err)
	}
	return a.ID
}

// ExpectWhere reads frames until one of the wanted type satisfies ok, and
// returns it, failing the test if none does within half of Patience.
func (s *Stream) ExpectWhere(frameType string, ok func(f map[string]any) bool) map[string]any {
	s.t.Helper()
	for i := 0; i < 200; i++ {
		f := s.Expect(frameType)
		if ok(f) {
			return f
		}
	}
	s.t.Fatalf("no %s frame matched", frameType)
	return nil
}

// Close ends the connection from the client's side, as closing the app does.
func (s *Stream) Close() { s.conn.CloseNow() }
