package modhost

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
)

// Host is the toolkit a module is handed with each call, and all it may
// reach: the verified actor and conversation, randomness, and the module's
// own store. It is a type of its own, not an interface, so that what it
// offers can grow (the clock and timers, cards of its own, private parts)
// without any module changing.
//
// A Host lasts for one call. Once the call has answered or run out of time,
// its store refuses.
type Host struct {
	ctx    context.Context
	module string
	actor  model.Account
	conv   model.ConversationID
	intn   func(n int) int
	store  Store
}

// HostOptions is what a Host is made from.
type HostOptions struct {
	// Module is the name of the module the Host is for: its store is that
	// module's alone.
	Module string
	// Actor is the person who invoked the command or sent the object, as
	// the server verified them.
	Actor model.Account
	// Conversation is where.
	Conversation model.ConversationID
	// Intn gives a whole number from 0 to n-1. Left out, it reads
	// crypto/rand, so that a result cannot be foreseen or steered.
	Intn func(n int) int
	// Store keeps the module's values. Left out, the module keeps nothing
	// and every use of the store is refused.
	Store Store
	// Context is the call's: its deadline is the call's. Left out, the
	// call has none.
	Context context.Context
}

// NewHost makes a Host. The registry makes one for every call; a module's
// own tests make one with a fixed Intn.
func NewHost(o HostOptions) *Host {
	if o.Intn == nil {
		o.Intn = cryptoIntn
	}
	if o.Context == nil {
		o.Context = context.Background()
	}
	return &Host{ctx: o.Context, module: o.Module, actor: o.Actor, conv: o.Conversation, intn: o.Intn, store: o.Store}
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

// Store is where the host keeps the modules' values: the server's store of
// them (store.Modules), which keeps the totals under the limits it is given.
type Store interface {
	Value(ctx context.Context, k model.ModuleKey) ([]byte, bool, error)
	PutValue(ctx context.Context, k model.ModuleKey, value []byte, perConversation, perModule int64) error
	DeleteValue(ctx context.Context, k model.ModuleKey) error
}

// The limits of a module's store (docs/DESIGN-MODULES.md section 6.2).
const (
	// MaxKeyBytes is the longest key.
	MaxKeyBytes = 200
	// MaxValueBytes is the largest value.
	MaxValueBytes = 64 << 10
	// MaxConversationBytes is the most a module keeps for one
	// conversation.
	MaxConversationBytes = 1 << 20
	// MaxModuleBytes is the most a module keeps in all.
	MaxModuleBytes = 64 << 20
)

// errNoStore is a store asked for where none was given.
var errNoStore = errors.New("modhost: this host keeps nothing")

// key is the full name of a key of the module's, in a scope: the
// conversation or the person is the call's, never the module's to choose.
func (h *Host) key(scope model.ModuleScope, key string) (model.ModuleKey, error) {
	if h.store == nil {
		return model.ModuleKey{}, errNoStore
	}
	if err := h.ctx.Err(); err != nil {
		return model.ModuleKey{}, err
	}
	if key == "" || len(key) > MaxKeyBytes || !utf8.ValidString(key) {
		return model.ModuleKey{}, model.Invalid("key", "A module's key is 1 to 200 bytes of text.")
	}
	k := model.ModuleKey{Module: h.module, Scope: scope, Key: key}
	switch scope {
	case model.ModuleScopeInstance:
	case model.ModuleScopeConversation:
		if h.conv == "" {
			return model.ModuleKey{}, model.Invalid("scope", "This call is in no conversation.")
		}
		k.Conversation = h.conv
	case model.ModuleScopeAccount:
		if h.actor.ID == "" {
			return model.ModuleKey{}, model.Invalid("scope", "This call is for nobody.")
		}
		k.Account = h.actor.ID
	default:
		return model.ModuleKey{}, model.Invalid("scope", "A module keeps values for the instance, a conversation or a person.")
	}
	return k, nil
}

// Get gives the value kept under key in a scope: for the whole instance,
// for this conversation, or for this person. found is false when there is
// none.
func (h *Host) Get(scope model.ModuleScope, key string) (value []byte, found bool, err error) {
	k, err := h.key(scope, key)
	if err != nil {
		return nil, false, err
	}
	return h.store.Value(h.ctx, k)
}

// Put keeps value under key in a scope, replacing what was there. It is
// refused with model.ErrTooLarge for a value over MaxValueBytes, or one that
// would take the module past MaxConversationBytes in this conversation or
// MaxModuleBytes in all.
func (h *Host) Put(scope model.ModuleScope, key string, value []byte) error {
	k, err := h.key(scope, key)
	if err != nil {
		return err
	}
	if len(value) > MaxValueBytes {
		return model.ErrTooLarge.With("A module's value is at most 64 KB.")
	}
	return h.store.PutValue(h.ctx, k, value, MaxConversationBytes, MaxModuleBytes)
}

// Delete removes what is kept under key in a scope.
func (h *Host) Delete(scope model.ModuleScope, key string) error {
	k, err := h.key(scope, key)
	if err != nil {
		return err
	}
	return h.store.DeleteValue(h.ctx, k)
}
