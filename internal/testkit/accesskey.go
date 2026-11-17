package testkit

import (
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/sealed/ref"
	"github.com/tikaydaes/churr/internal/secret"
)

// Access keys (docs/AUTH.md section 5.6) as a page uses them: the key is
// made here and only the value derived from it is sent.

// AccessKeyAuth is what a page sends for an access key's 32 random bytes.
func AccessKeyAuth(random []byte) string {
	return model.EncodeBytes(secret.AccessKeyAuth(random))
}

// NewAccessKey makes a new access key for this browser to keep, replacing
// any it had, and returns the value to send for it.
func (c *Client) NewAccessKey() string {
	c.accessKey = secret.NewAccessKey()
	return AccessKeyAuth(c.accessKey)
}

// AccessKey is the value to send for this browser's access key.
func (c *Client) AccessKey() string {
	if c.accessKey == nil {
		c.t.Fatal("this browser has no access key")
	}
	return AccessKeyAuth(c.accessKey)
}

// AccessKeyUnlockPublic is the unlock public key a page derives from this
// browser's access key for sealed chats (docs/SEALED.md section 13.3).
func (c *Client) AccessKeyUnlockPublic() string {
	return model.EncodeBytes(ref.X25519Public(ref.UnlockKeyFromAccessKey(ref.AccessKeyWrap(c.accessKey))))
}

// UseAccessKeyOf has this browser hold the same access key as another, as
// a person who types their key on a second device does.
func (c *Client) UseAccessKeyOf(o *Client) { c.accessKey = o.accessKey }

// AccessKeyText is this browser's access key as a person sees it.
func (c *Client) AccessKeyText() string { return secret.FormatAccessKey(c.accessKey) }

// RegisterWithAccessKey registers a new account in this browser with an
// access key in place of a passkey.
func (c *Client) RegisterWithAccessKey(username, inviteCode string) map[string]any {
	c.t.Helper()
	out := c.Must(201, "POST", "/api/v1/access-key/register", map[string]any{"invite_code": inviteCode, "username": username,
		"display_name": username, "access_key_auth": c.NewAccessKey(), "unlock_public": c.AccessKeyUnlockPublic()})
	c.signedIn(out)
	return out
}

// AccessKeyLogin signs this browser in with its access key and returns the
// status and the answer.
func (c *Client) AccessKeyLogin(username string) (int, map[string]any) {
	c.t.Helper()
	return c.Do("POST", "/api/v1/access-key/login", map[string]any{"username": username, "access_key_auth": c.AccessKey()})
}

// MemberWithAccessKey makes a member who has an access key and no passkey.
func (ts *TestServer) MemberWithAccessKey(name string) *Client {
	ts.t.Helper()
	c := ts.Browser()
	c.RegisterWithAccessKey(name, ts.invite(map[string]any{}))
	return c
}
