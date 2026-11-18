package modhost

import (
	"crypto/rand"
	"math/big"

	"github.com/tikaydaes/churr/internal/model"
)

// Host is the toolkit a module is handed with each call, and all it may
// reach: the verified actor and conversation, and randomness. It is a type
// of its own, not an interface, so that what it offers can grow without any
// module changing.
type Host struct {
	actor model.Account
	conv  model.ConversationID
	intn  func(n int) int
}

// HostOptions is what a Host is made from.
type HostOptions struct {
	// Actor is the person who invoked the command or sent the object, as
	// the server verified them.
	Actor model.Account
	// Conversation is where.
	Conversation model.ConversationID
	// Intn gives a whole number from 0 to n-1. Left out, it reads
	// crypto/rand, so that a result cannot be foreseen or steered.
	Intn func(n int) int
}

// NewHost makes a Host. The registry makes one for every call; a module's
// own tests make one with a fixed Intn.
func NewHost(o HostOptions) *Host {
	if o.Intn == nil {
		o.Intn = cryptoIntn
	}
	return &Host{actor: o.Actor, conv: o.Conversation, intn: o.Intn}
}

// Actor is the person the call is for.
func (h *Host) Actor() model.Account { return h.actor }

// Conversation is where the call was made.
func (h *Host) Conversation() model.ConversationID { return h.conv }

// Intn gives a whole number from 0 to n-1, for n of at least 1.
func (h *Host) Intn(n int) int { return h.intn(n) }

// cryptoIntn gives a whole number from 0 to n-1 from crypto/rand.
func cryptoIntn(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		// The system's random source failing is not something to carry
		// on from.
		panic("modhost: crypto/rand failed: " + err.Error())
	}
	return int(v.Int64())
}
