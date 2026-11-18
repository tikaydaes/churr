package storetest

import (
	"bytes"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("fair", testFair) }

// testFair: a message's fair block is stored with it, and stays, with the
// client_id it was drawn by, when it is deleted and when it is purged.
func testFair(t *testing.T, s store.Store) {
	h1 := bytes.Repeat([]byte{1}, 32)
	ann := newAccount(t, s, "ann", model.RoleOwner)
	c := newChannel(t, s, "dice", false, ann.ID)
	block := &model.FairBlock{V: 1, Derive: 1, Owner: "a.example", PK: bytes.Repeat([]byte{3}, 33), Alpha: "1790000000|01a1ee00-0000-7000-8000-0000000000c1",
		Beta: h1, Proof: bytes.Repeat([]byte{4}, 81), T: 1790000000}
	const id = "01a1ee00-0000-7000-8000-0000000000c1"
	m := model.Message{ConversationID: c.ID, AuthorID: ann.ID, ClientID: id, Text: "Rolled 1d6: [4] = 4", Fair: block}
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
	eq(t, "the block stored", got.Fair != nil && bytes.Equal(got.Fair.Proof, block.Proof), true)
	_, err = s.Messages().Delete(ctx, m.ID, ann.ID, model.Now())
	must(t, err)
	got, err = s.Messages().Get(ctx, m.ID)
	must(t, err)
	eq(t, "the block after a delete", got.Fair != nil && bytes.Equal(got.Fair.Beta, h1) && got.Fair.T == 1790000000 && got.Text == "", true)
	_, err = s.Messages().Purge(ctx, m.ID, model.Now())
	must(t, err)
	got, err = s.Messages().Get(ctx, m.ID)
	must(t, err)
	eq(t, "the block and its client_id after a purge", got.Fair != nil && got.ClientID == id, true)
}
