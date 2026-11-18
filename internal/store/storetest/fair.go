package storetest

import (
	"bytes"
	"errors"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("fair", testFair) }

// testFair: a chain's head moves one draw at a time and only from where it
// stands, so two messages cannot both take a draw and none is skipped; a
// message's fair block is stored with it and stays when it is deleted.
func testFair(t *testing.T, s store.Store) {
	n, err := s.Fair().Head(ctx, "chain-a")
	must(t, err)
	eq(t, "a chain with no draw", n, int64(0))
	h1 := bytes.Repeat([]byte{1}, 32)
	must(t, s.Tx(ctx, func(tx store.Tx) error { return tx.Fair().Advance(ctx, "chain-a", 1) }))
	eq(t, "draw 1 again", errors.Is(s.Fair().Advance(ctx, "chain-a", 1), store.ErrFairMoved), true)
	eq(t, "draw 3 before 2", errors.Is(s.Fair().Advance(ctx, "chain-a", 3), store.ErrFairMoved), true)
	must(t, s.Fair().Advance(ctx, "chain-a", 2))
	n, err = s.Fair().Head(ctx, "chain-a")
	must(t, err)
	eq(t, "the head", n, int64(2))
	// A transaction that fails leaves the head where it was.
	failed := errors.New("the message could not be stored")
	err = s.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Fair().Advance(ctx, "chain-a", 3); err != nil {
			return err
		}
		return failed
	})
	eq(t, "the failed transaction", errors.Is(err, failed), true)
	n, _ = s.Fair().Head(ctx, "chain-a")
	eq(t, "the head after it", n, int64(2))
	n, _ = s.Fair().Head(ctx, "chain-b")
	eq(t, "another chain", n, int64(0))

	ann := newAccount(t, s, "ann", model.RoleOwner)
	c := newChannel(t, s, "dice", false, ann.ID)
	block := &model.FairBlock{V: 1, Derive: 1, Owner: "a.example", N: 7, PK: bytes.Repeat([]byte{3}, 33), Alpha: []byte("alpha"),
		Beta: h1, Proof: bytes.Repeat([]byte{4}, 81)}
	m := model.Message{ConversationID: c.ID, AuthorID: ann.ID, Text: "Rolled 1d6: [4] = 4", Fair: block}
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		seq, err := tx.Conversations().NextSeq(ctx, c.ID)
		if err != nil {
			return err
		}
		m.Seq = seq
		return tx.Messages().Insert(ctx, &m, nil)
	}))
	got, err := s.Messages().Get(ctx, m.ID)
	must(t, err)
	eq(t, "the block stored", got.Fair != nil && got.Fair.N == 7 && bytes.Equal(got.Fair.Proof, block.Proof), true)
	_, err = s.Messages().Delete(ctx, m.ID, ann.ID, model.Now())
	must(t, err)
	got, err = s.Messages().Get(ctx, m.ID)
	must(t, err)
	eq(t, "the block after a delete", got.Fair != nil && bytes.Equal(got.Fair.Beta, h1) && got.Text == "", true)
}
