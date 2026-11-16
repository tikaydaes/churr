package testkit

import (
	"encoding/json"

	"github.com/descope/virtualwebauthn"
)

// AssertUnverified answers an assertion ceremony with this browser's
// passkey but without the user-verified flag: a touch with no PIN or
// biometric, which nothing in Churr accepts.
func (c *Client) AssertUnverified(begin map[string]any) map[string]any {
	c.t.Helper()
	raw, _ := json.Marshal(map[string]any{"publicKey": begin["public_key"]})
	opts, err := virtualwebauthn.ParseAssertionOptions(string(raw))
	if err != nil {
		c.t.Fatal(err)
	}
	auth := c.auth
	auth.Options.UserNotVerified = true
	resp := virtualwebauthn.CreateAssertionResponse(c.ts.rp, auth, c.cred, *opts)
	return map[string]any{"ceremony_id": begin["ceremony_id"], "credential": json.RawMessage(resp)}
}
