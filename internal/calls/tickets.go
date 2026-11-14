package calls

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// newSecret makes a random 256-bit value as 43 characters of base64url: a
// join ticket, a resume token or a pass.
func newSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("calls: no randomness: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// hashSecret is what is kept of a secret: enough to recognise it, not
// enough to use it.
func hashSecret(s string) [32]byte { return sha256.Sum256([]byte(s)) }

// ticket is the right to enter one call once (docs/CALLS.md section 2). A
// ticket made by the join operation is tied to the session that asked; a
// pass given to another server (section 16) is tied to that server.
type ticket struct {
	call    model.CallID
	account model.AccountID
	// session is the session a local ticket is for.
	session model.SessionID
	// server is the server a pass was given to, or "".
	server  string
	expires time.Time
}

// tickets holds the tickets that have been given out and not yet used. It
// is in memory only: a restart ends every call, and with it every ticket.
type tickets struct {
	mu   sync.Mutex
	open map[[32]byte]ticket
}

func newTickets() *tickets { return &tickets{open: map[[32]byte]ticket{}} }

// issue makes a ticket good until expires.
func (t *tickets) issue(tk ticket) string {
	secret := newSecret()
	t.mu.Lock()
	now := time.Now()
	for h, old := range t.open {
		if now.After(old.expires) {
			delete(t.open, h)
		}
	}
	t.open[hashSecret(secret)] = tk
	t.mu.Unlock()
	return secret
}

// use takes a ticket out, whatever happens next: a ticket works once. ok is
// false for one that is unknown, used or too old.
func (t *tickets) use(secret string, now time.Time) (ticket, bool) {
	h := hashSecret(secret)
	t.mu.Lock()
	tk, found := t.open[h]
	delete(t.open, h)
	t.mu.Unlock()
	if !found || now.After(tk.expires) {
		return ticket{}, false
	}
	return tk, true
}
