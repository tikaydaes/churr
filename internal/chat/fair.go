package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/fair"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modhost"
	"github.com/tikaydaes/churr/internal/store"
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
	s.fair = fair.NewDrawer(p, owner, func() time.Time { return s.now().Time() }, ledger{s.store})
}

// ledger keeps the Drawer's pairs in the store's draws_used, so that a
// restart or a crash never lets a pair draw twice (docs/FAIR.md section 6).
// It is written outside any transaction: a draw is made before the post's.
type ledger struct{ st store.Store }

func (l ledger) Take(ctx context.Context, u fair.Used) error {
	return l.st.Draws().Take(ctx, store.DrawUsed{Second: u.Second, Nonce: u.Nonce, Who: u.Who})
}

func (l ledger) All(ctx context.Context) ([]fair.Used, error) {
	rows, err := l.st.Draws().All(ctx)
	out := make([]fair.Used, len(rows))
	for i, r := range rows {
		out[i] = fair.Used{Second: r.Second, Nonce: r.Nonce, Who: r.Who}
	}
	return out, err
}

func (l ledger) Forget(ctx context.Context, before int64) error {
	return l.st.Draws().Forget(ctx, before)
}

// refusedAuditGap is how often one account's refused draws are audited at
// most: a client that keeps sending them is noted, not allowed to fill the
// log.
const refusedAuditGap = time.Minute

// refusals remembers when each account's last refused draw was audited.
type refusals struct {
	mu   sync.Mutex
	last map[model.AccountID]time.Time
}

// due reports whether a refused draw of an account is audited now, and
// notes it.
func (r *refusals) due(who model.AccountID, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last == nil {
		r.last = map[model.AccountID]time.Time{}
	}
	if t, ok := r.last[who]; ok && now.Sub(t) < refusedAuditGap {
		return false
	}
	if len(r.last) > 10000 {
		for a, t := range r.last {
			if now.Sub(t) >= refusedAuditGap {
				delete(r.last, a)
			}
		}
	}
	r.last[who] = now
	return true
}

// auditRefused writes fair.draw_refused for a draw refused as a reused nonce
// or a second outside the window: an honest client is never refused, so it
// is a suspected attempt to reuse or steer a draw (docs/FAIR.md section 6).
func (s *Service) auditRefused(ctx context.Context, actor model.Account, conv model.ConversationID, err error) {
	e, ok := model.AsError(err)
	if !ok || (e.Code != model.CodeConflict && e.Code != model.CodeClock) {
		return
	}
	now := s.now()
	if !s.drawRefusals.due(actor.ID, now.Time()) {
		return
	}
	reason := "nonce_reused"
	if e.Code == model.CodeClock {
		reason = "second_outside_window"
	}
	_ = s.store.Audit().Append(ctx, model.AuditEntry{At: now, ActorID: string(actor.ID), ActorKind: "account",
		Action: "fair.draw_refused", TargetKind: "conversation", TargetID: string(conv),
		Detail: map[string]any{"conversation_id": conv, "reason": reason}})
}

// errNoProver is chance asked for on a server that draws none.
var errNoProver = errors.New("chat: this server has no VRF key to draw with")

// errNoClientID is chance asked for by a post with no client_id to draw by.
var errNoClientID = model.Invalid("client_id", "A message that takes chance (a roll, a coin, cards) needs a client_id and draw_t: a UUID your client makes for it and the second it sends it at, both sent again unchanged if the post is retried.")

// drawer is one message's draw, made when first asked for (modhost.Draws).
type drawer struct {
	ctx      context.Context
	s        *Service
	actor    model.Account
	conv     model.ConversationID
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

func (s *Service) newDrawer(ctx context.Context, actor model.Account, conv model.ConversationID, in NewMessage) *drawer {
	who := string(actor.ID) + "|" + string(conv) + "|" + requestOf(in)
	return &drawer{ctx: ctx, s: s, actor: actor, conv: conv, clientID: in.ClientID, t: in.DrawT, who: who}
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

// Return implements modhost.Returner: an object that took a stream and made
// no value gives its index back.
func (d *drawer) Return(s *fair.Stream) {
	if d.objs != nil {
		d.objs.Return(s)
	}
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
	b, err := d.s.fair.Draw(d.ctx, d.t, d.clientID, d.who)
	if err != nil {
		d.err = err
		d.s.auditRefused(d.ctx, d.actor, d.conv, err)
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

// mayDraw reports whether a text holds the opening of something a module
// resolves with chance (inline dice): a post that may draw is checked in full
// before it draws.
func (s *Service) mayDraw(text string) bool {
	for _, c := range s.modules.Claims() {
		if c.Open != "" && strings.Contains(text, c.Open) {
			return true
		}
	}
	return false
}

// filesFree checks the files a post names as Attach will: each an upload to
// this conversation by this author on no message yet, none named twice.
func (s *Service) filesFree(ctx context.Context, conv model.ConversationID, uploader model.AccountID, ids []model.FileID) error {
	seen := map[model.FileID]bool{}
	for _, id := range ids {
		if seen[id] {
			return model.ErrNotFound
		}
		seen[id] = true
	}
	files, err := s.store.Files().GetMany(ctx, ids)
	if err != nil {
		return err
	}
	if len(files) != len(ids) {
		return model.ErrNotFound
	}
	for _, f := range files {
		if f.ConversationID != conv || f.UploaderID != uploader || f.MessageID != "" {
			return model.ErrNotFound
		}
	}
	return nil
}
