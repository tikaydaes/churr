package chat

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/tikaydaes/churr/internal/fair"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modhost"
)

// Provably fair chance (docs/FAIR.md). Every chance outcome a module makes
// in a message comes from the message's one draw: an ECVRF proof, by this
// server's VRF key, over the input the author's client gave it, the second
// draw_t and the message's client_id as the nonce. The draw is made the first
// time something in the message asks for chance, before the transaction,
// by the server's one Drawer, which refuses a second outside its window and
// a pair taken for another request. A post sent again with the same
// client_id is given the message it made (Post's client_id rule, which comes
// first); one that failed before it was stored draws again, to the same
// result. So the server cannot re-roll by failing a request.

// SetFair hands over what draws are made with: this server's name and its
// VRF key, which the Drawer holds. Until it is called a module that asks for
// chance is refused.
func (s *Service) SetFair(owner string, p fair.Prover) {
	s.fair = fair.NewDrawer(p, owner, func() time.Time { return s.now().Time() })
}

// errNoProver is chance asked for on a server that draws none.
var errNoProver = errors.New("chat: this server has no VRF key to draw with")

// errNoClientID is chance asked for by a post with no client_id to draw by.
var errNoClientID = model.Invalid("client_id", "A message that takes chance (a roll, a coin, cards) needs a client_id and draw_t: a UUID your client makes for it and the second it sends it at, both sent again unchanged if the post is retried.")

// drawer is one message's draw, made when first asked for (modhost.Draws).
type drawer struct {
	s        *Service
	clientID string
	t        int64
	// who names the request, for the Drawer to tell a retry from another
	// use of the pair: the author, the conversation and what was asked.
	who   string
	block *model.FairBlock
	objs  *fair.Objects
	// err is why the draw was refused, when it was: inline dice swallow it
	// and stay text, and Post refuses the message with it.
	err error
}

func (s *Service) newDrawer(actor model.Account, conv model.ConversationID, in NewMessage) *drawer {
	who := string(actor.ID) + "|" + string(conv) + "|" + requestOf(in)
	return &drawer{s: s, clientID: in.ClientID, t: in.DrawT, who: who}
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

// draw makes the message's draw: the one its second and client_id fix.
func (d *drawer) draw() error {
	if d.s.fair == nil {
		return errNoProver
	}
	if d.err != nil {
		return d.err
	}
	if d.clientID == "" || d.t == 0 {
		d.err = errNoClientID
		return d.err
	}
	b, err := d.s.fair.Draw(d.t, d.clientID, d.who)
	if err != nil {
		d.err = err
		return err
	}
	d.block, d.objs = &b, fair.NewObjects(b.Beta)
	return nil
}

// requestOf is what a post asks for: the command and what was typed after
// it, or the text, hashed.
func requestOf(in NewMessage) string {
	h := sha256.New()
	if in.Command != nil {
		h.Write([]byte("command\x00" + in.Command.Name + "\x00" + in.Command.Text))
	} else {
		h.Write([]byte("text\x00" + in.Text))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// noDraw is the draws of what is read for nothing, such as an edit, which
// resolves no object and throws away what the module made: never a draw.
func noDraw() modhost.Draws { return fair.NewObjects(make([]byte, 32)) }

// ModuleCommand is a built-in module's command for Post to run: its name as
// the module gave it, and what was typed after it.
type ModuleCommand struct {
	Name, Text string
}
