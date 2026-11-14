package secret

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// The prefixes of docs/CONVENTIONS.md. A prefix says what a token is, so a
// leaked one can be recognised, and it names the only place the token works
// (docs/AUTH.md section 1).
const (
	PrefixSession     = "churr_ses_"  // web session; the cookie value
	PrefixApp         = "churr_app_"  // app access token
	PrefixRefresh     = "churr_ref_"  // app refresh token
	PrefixPersonal    = "churr_pat_"  // personal token
	PrefixBot         = "churr_bot_"  // bot token
	PrefixHook        = "churr_hook_" // webhook secret, in the path
	PrefixIntegration = "churr_int_"  // server integration token
	PrefixLink        = "churr_link_" // invitation to link two servers or join a hub
)

// prefixes lists every known prefix. None is the start of another, so at
// most one can match a string.
var prefixes = []string{
	PrefixSession, PrefixApp, PrefixRefresh, PrefixPersonal,
	PrefixBot, PrefixHook, PrefixIntegration, PrefixLink,
}

// tokenBytes is the randomness in a token or link value: 256 bits.
const tokenBytes = 32

// inviteBytes is the randomness in an invite code: 128 bits.
const inviteBytes = 16

// NewToken makes a credential: prefix followed by 256 random bits in
// base64url without padding. It returns the token, which is shown once and
// never stored, and the hash that is stored in its place.
func NewToken(prefix string) (token string, hash []byte) {
	token = prefix + base64.RawURLEncoding.EncodeToString(RandomBytes(tokenBytes))
	return token, HashToken(token)
}

// HashToken is the SHA-256 hash of the whole string, prefix included. It is
// what the database holds and what a presented token is looked up by. A
// plain hash is enough because the input has 256 random bits: nobody can
// search for it.
//
// The same hash serves for one-time link values, which have no prefix.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// TokenPrefix returns the known prefix token begins with, or "" when it
// begins with none. It lets a handler turn away a token of the wrong kind,
// such as a session token sent as a bearer token, without a database read.
func TokenPrefix(token string) string {
	for _, p := range prefixes {
		if strings.HasPrefix(token, p) {
			return p
		}
	}
	return ""
}

// NewLinkValue makes a one-time value for a link: the setup link, a
// recovery link, an OAuth authorization code, a pairing code. It is 256
// random bits in base64url, 43 characters, with no prefix, since it is not
// an API credential and works in one place only. The hash is what is
// stored, as with a token.
func NewLinkValue() (value string, hash []byte) {
	return NewToken("")
}

// NewInviteCode makes the code of an invite: 128 random bits in base64url,
// 22 characters. Unlike a token it is stored as it is, so an admin can copy
// the link again from the invite list (docs/AUTH.md section 4.2).
func NewInviteCode() string {
	return base64.RawURLEncoding.EncodeToString(RandomBytes(inviteBytes))
}
