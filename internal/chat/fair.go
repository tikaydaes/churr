package chat

import (
	"context"
	"errors"

	"github.com/tikaydaes/churr/internal/fair"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modhost"
	"github.com/tikaydaes/churr/internal/store"
)

// Provably fair chance (docs/FAIR.md). Every chance outcome a module makes
// in a message comes from the message's one draw: an ECVRF proof, by this
// server's VRF key, over the alpha of the next draw of the conversation's
// chain. The draw is made the first time something in the message asks for
// chance, before the transaction; the transaction moves the chain's head to
// it along with storing the message, and only if the head is still where the
// draw was made from. Otherwise another message took that draw, and the post
// is made again from the start with the next one: a draw is used exactly
// once and none is skipped, and as alpha is fixed by the chain alone, a post
// made again gets what the chain gives, never a choice.
//
// The owner of a chain is the server that draws, by its name. A hub channel's
// messages are drawn by their author's instance as anything else is, the
// chain named by the channel's stream; a hub drawing for its channels is not
// built.

// SetFair hands over what draws are made with: this server's name, which
// owns its chains, and its VRF key. Until it is called a module that asks
// for chance is refused.
func (s *Service) SetFair(owner string, p fair.Prover) { s.fairOwner, s.prover = owner, p }

// maxFairTries is how many times a post is made again because another
// message took its draw first.
const maxFairTries = 8

// errNoProver is chance asked for on a server that draws none.
var errNoProver = errors.New("chat: this server has no VRF key to draw with")

// drawer is one message's draw, made when first asked for (modhost.Draws).
type drawer struct {
	s     *Service
	ctx   context.Context
	conv  model.ConversationID
	chain string
	block *model.FairBlock
	objs  *fair.Objects
}

func (s *Service) newDrawer(ctx context.Context, conv model.ConversationID) *drawer {
	return &drawer{s: s, ctx: ctx, conv: conv}
}

// Stream implements modhost.Draws: the next chance object's stream of the
// message's draw.
func (d *drawer) Stream() (*fair.Stream, error) {
	if d.objs == nil {
		if err := d.draw(); err != nil {
			return nil, err
		}
	}
	return d.objs.Stream()
}

// draw makes the message's draw: the next of its chain.
func (d *drawer) draw() error {
	if d.s.prover == nil {
		return errNoProver
	}
	chain, err := chainOf(d.ctx, d.s.store, d.conv)
	if err != nil {
		return err
	}
	n, prev, err := d.s.store.Fair().Head(d.ctx, chain)
	if err != nil {
		return err
	}
	if prev == nil {
		prev = fair.NoPrev
	}
	b, err := fair.Make(d.s.prover, d.s.fairOwner, chain, n+1, prev)
	if err != nil {
		return err
	}
	d.chain, d.block, d.objs = chain, &b, fair.NewObjects(b.Beta)
	return nil
}

// advance moves the chain's head to the message's draw, in the transaction
// that stores the message; nothing when the message drew nothing.
func (d *drawer) advance(ctx context.Context, tx store.Tx) error {
	if d == nil || d.block == nil {
		return nil
	}
	return tx.Fair().Advance(ctx, d.chain, d.block.N, fair.PrevOf(d.block.Proof))
}

// chainOf names a conversation's chain: the stream it is shared over, which
// every server of the stream knows it by, or its own id.
func chainOf(ctx context.Context, a store.Areas, conv model.ConversationID) (string, error) {
	st, err := a.Streams().ByConversation(ctx, conv)
	switch {
	case err == nil:
		return string(st.ID), nil
	case errors.Is(err, model.ErrNotFound):
		return string(conv), nil
	}
	return "", err
}

// ChainOf is the name of a conversation's chain, for the receiver of a
// message to check its fair block against (link).
func ChainOf(ctx context.Context, a store.Areas, conv model.ConversationID) (string, error) {
	return chainOf(ctx, a, conv)
}

// noDraw is the draws of what is read for nothing, such as an edit, which
// resolves no object and throws away what the module made: never a draw of
// the chain.
func noDraw() modhost.Draws { return fair.NewObjects(make([]byte, 32)) }

// ModuleCommand is a built-in module's command for Post to run: its name as
// the module gave it, and what was typed after it.
type ModuleCommand struct {
	Name, Text string
}
