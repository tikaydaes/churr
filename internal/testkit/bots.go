package testkit

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
)

// Helpers for tests of bots and webhooks.

// Bot creates a bot as this account, which must be an admin with a fresh
// session (the owner is, and so is an admin made by Admin), and returns a
// client that acts as the bot with its first token.
func (c *Client) Bot(username string) *Client {
	c.t.Helper()
	out := c.Must(201, "POST", "/api/v1/admin/bots", map[string]any{"username": username, "display_name": username})
	acc := out["bot"].(map[string]any)["account"].(map[string]any)
	return &Client{ts: c.ts, t: c.t, http: &http.Client{}, Bearer: out["secret"].(string),
		ID: model.AccountID(acc["id"].(string)), Username: username}
}

// Add adds accounts to a conversation as this account, at a level ("" for
// the conversation's default).
func (c *Client) Add(conversation, level string, who ...*Client) {
	c.t.Helper()
	ids := []string{}
	for _, w := range who {
		ids = append(ids, string(w.ID))
	}
	body := map[string]any{"account_ids": ids}
	if level != "" {
		body["level"] = level
	}
	c.Must(200, "POST", "/api/v1/conversations/"+conversation+"/members", body)
}

// Raw sends a request that carries no credential and nothing a browser
// adds: what a program posting to a webhook address sends. It returns the
// status, the response headers and the body as it came.
func (ts *TestServer) Raw(method, path string, header http.Header, body []byte) (int, http.Header, []byte) {
	ts.t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, bytes.NewReader(body))
	if err != nil {
		ts.t.Fatal(err)
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		ts.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, raw
}

// Webhook asks for a webhook in a conversation as this account, has the
// owner approve it, and collects its address. It returns the webhook's id,
// the path of its address (which starts /hooks/) and its signing secret.
func (c *Client) Webhook(conversation, name string) (id, path, signingSecret string) {
	c.t.Helper()
	out := c.Must(201, "POST", "/api/v1/conversations/"+conversation+"/webhook-requests", map[string]any{"name": name, "purpose": "testing"})
	w := out["webhook"].(map[string]any)
	id = w["id"].(string)
	if w["state"] == "waiting" {
		c.ts.Owner.Must(200, "POST", "/api/v1/webhooks/"+id+"/approve", nil)
	}
	got := c.Must(200, "POST", "/api/v1/webhooks/"+id+"/address", nil)
	address := got["url"].(string)
	return id, address[strings.Index(address, "/hooks/"):], got["signing_secret"].(string)
}
