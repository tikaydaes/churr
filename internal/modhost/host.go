package modhost

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/fair"
	"github.com/tikaydaes/churr/internal/model"
)

// Host is the toolkit a module is handed with each call, and all it may
// reach: the verified actor and conversation, its share of the message's
// draw, and the module's own store. It is a type of its own, not an interface, so that what it
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
	draws  Draws
	store  Store
	// stream is the call's share of the draw once it has asked for it,
	// and drawn the primitives it asked for (Draw).
	stream *fair.Stream
	drawn  []model.FairPrimitive
}

// Draws hands out the message's one draw (docs/FAIR.md): each chance object
// of a message, in order of appearance, gets the stream of the next object
// index. The first call makes the draw. The chat service gives one per
// message; tests give fair.Objects over a beta of their own.
type Draws interface {
	Stream() (*fair.Stream, error)
}

// errNoDraw is chance asked for where no draw is to be had.
var errNoDraw = errors.New("modhost: this call has no draw to take chance from")

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
	// Draws is where the call's chance comes from: the message's draw.
	// Left out, the call has none, and asking for chance fails.
	Draws Draws
	// Store keeps the module's values. Left out, the module keeps nothing
	// and every use of the store is refused.
	Store Store
	// Context is the call's: its deadline is the call's. Left out, the
	// call has none.
	Context context.Context
}

// NewHost makes a Host. The registry makes one for every call; a module's
// own tests make one with draws of their own.
func NewHost(o HostOptions) *Host {
	if o.Context == nil {
		o.Context = context.Background()
	}
	return &Host{ctx: o.Context, module: o.Module, actor: o.Actor, conv: o.Conversation, draws: o.Draws, store: o.Store}
}

// Actor is the person the call is for.
func (h *Host) Actor() model.Account { return h.actor }

// Conversation is where the call was made.
func (h *Host) Conversation() model.ConversationID { return h.conv }

// Random is the call's stream: its object's share of the message's draw
// (docs/FAIR.md section 3), the only chance a module has. A module that reads
// it derives its outcomes by its kind's own frozen rule (the dice, by
// fair.Roll), so a verifier re-derives them. The first ask takes the next
// object index; later asks in the same call give the same stream.
func (h *Host) Random() (*fair.Stream, error) {
	if h.stream != nil {
		return h.stream, nil
	}
	if h.draws == nil {
		return nil, errNoDraw
	}
	s, err := h.draws.Stream()
	if err != nil {
		return nil, err
	}
	h.stream = s
	return s, nil
}

// Draw derives declared outcomes from the call's stream, in order (docs/
// FAIR.md section 4), and records the primitives in the value the call makes,
// so that any client verifies them without knowing the module.
func (h *Host) Draw(primitives ...model.FairPrimitive) ([]fair.Result, error) {
	s, err := h.Random()
	if err != nil {
		return nil, err
	}
	out := make([]fair.Result, 0, len(primitives))
	for _, mp := range primitives {
		var p fair.Primitive
		raw, err := json.Marshal(mp)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		r, err := fair.Draw(s, p)
		if err != nil {
			return nil, model.Invalid("draw", err.Error())
		}
		h.drawn = append(h.drawn, mp)
		out = append(out, r)
	}
	return out, nil
}

// drew is what the value the call made records of the draw: nil when it
// took no chance.
func (h *Host) drew() *model.ValueDraw {
	if h.stream == nil {
		return nil
	}
	return &model.ValueDraw{I: int(h.stream.Object()), Primitives: h.drawn}
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
