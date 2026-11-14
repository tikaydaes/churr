package storetest

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("messages", testMessages) }

// messagesIndexable counts, independently of the index, the messages that
// should be in it: read through the API of the store, not through SQL.
func messagesIndexable(t testing.TB, s store.Store, convs ...model.ConversationID) int64 {
	t.Helper()
	var n int64
	for _, c := range convs {
		msgs, _, err := s.Messages().List(ctx, c, store.MessageList{SeqPage: store.SeqPage{Limit: 200}})
		must(t, err)
		for _, m := range msgs {
			if !m.Deleted() && (m.Text != "" || m.Fallback != "") {
				n++
			}
		}
	}
	return n
}

func messagesIndexInStep(t testing.TB, s store.Store, convs ...model.ConversationID) {
	t.Helper()
	got, err := s.Search().Indexed(ctx)
	must(t, err)
	eq(t, "search index size", got, messagesIndexable(t, s, convs...))
}

func testMessages(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	c := newChannel(t, s, "talk", false, ann.ID)
	join(t, s, c.ID, bob.ID, model.LevelMember)
	// Every conversation the subtests make, so the independent count of
	// what should be indexed covers all of them.
	all := []model.ConversationID{c.ID}

	t.Run("insert, get, client id, last_message_at", func(t *testing.T) {
		m := model.Message{ConversationID: c.ID, AuthorID: ann.ID, Text: "first", ClientID: model.NewID[string](),
			Entities: []model.Entity{{Type: model.EntityLink, At: 0, Len: 5, URL: "https://x.example"}}}
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
		eq(t, "text", got.Text, "first")
		eq(t, "seq", got.Seq, int64(1))
		eq(t, "version", got.Version, 1)
		eq(t, "entities", len(got.Entities), 1)
		eq(t, "entity url", got.Entities[0].URL, "https://x.example")
		again, found, err := s.Messages().ByClientID(ctx, c.ID, ann.ID, m.ClientID)
		must(t, err)
		eq(t, "found by client id", found && again.ID == m.ID, true)
		dup := model.Message{ConversationID: c.ID, AuthorID: ann.ID, Text: "again", ClientID: m.ClientID, Seq: 99}
		wantErr(t, s.Messages().Insert(ctx, &dup, nil), model.ErrAlreadyExists)
		conv, err := s.Conversations().Get(ctx, c.ID)
		must(t, err)
		eq(t, "last_message_at", conv.LastMessageAt, m.CreatedAt)
		_, err = s.Messages().Get(ctx, model.NewID[model.MessageID]())
		wantErr(t, err, model.ErrNotFound)
	})

	t.Run("a failed transaction leaves nothing, the index included", func(t *testing.T) {
		before, err := s.Search().Indexed(ctx)
		must(t, err)
		err = s.Tx(ctx, func(tx store.Tx) error {
			seq, err := tx.Conversations().NextSeq(ctx, c.ID)
			if err != nil {
				return err
			}
			m := model.Message{ConversationID: c.ID, AuthorID: ann.ID, Text: "ghost", Seq: seq}
			if err := tx.Messages().Insert(ctx, &m, nil); err != nil {
				return err
			}
			return model.ErrConflict
		})
		wantErr(t, err, model.ErrConflict)
		after, err := s.Search().Indexed(ctx)
		must(t, err)
		eq(t, "index unchanged", after, before)
		conv, err := s.Conversations().Get(ctx, c.ID)
		must(t, err)
		eq(t, "the number was not used up", conv.LastSeq, int64(1))
	})

	t.Run("thread summary follows replies", func(t *testing.T) {
		p := post(t, s, c.ID, ann.ID, "", "parent")
		r1 := post(t, s, c.ID, bob.ID, p.ID, "one")
		r2 := post(t, s, c.ID, ann.ID, p.ID, "two")
		got, err := s.Messages().Get(ctx, p.ID)
		must(t, err)
		eq(t, "reply_count", got.ReplyCount, 2)
		eq(t, "last_reply_seq", got.LastReplySeq, r2.Seq)
		eq(t, "last_reply_order is the newest reply's order", got.LastReplyOrder, r2.Order)
		_, err = s.Messages().Delete(ctx, r2.ID, ann.ID, model.Now())
		must(t, err)
		got, _ = s.Messages().Get(ctx, p.ID)
		eq(t, "after deleting the newest", got.ReplyCount, 1)
		eq(t, "last_reply_seq falls back", got.LastReplySeq, r1.Seq)
		eq(t, "last_reply_order falls back", got.LastReplyOrder, r1.Order)
		hyd := []model.Message{got}
		must(t, s.Messages().Hydrate(ctx, hyd, bob.ID))
		eq(t, "the summary carries the order", hyd[0].Thread != nil && hyd[0].Thread.LastReplyOrder == r1.Order, true)
		_, err = s.Messages().DeleteOutright(ctx, r1.ID)
		must(t, err)
		got, _ = s.Messages().Get(ctx, p.ID)
		eq(t, "after the last live reply went", got.ReplyCount, 0)
		eq(t, "no last reply", got.LastReplySeq, int64(0))
		eq(t, "no last reply order", got.LastReplyOrder, "")

		msgs := []model.Message{got}
		must(t, s.Messages().Hydrate(ctx, msgs, bob.ID))
		eq(t, "no thread summary without replies", msgs[0].Thread == nil, true)
		post(t, s, c.ID, bob.ID, p.ID, "three")
		got, _ = s.Messages().Get(ctx, p.ID)
		msgs = []model.Message{got}
		must(t, s.Messages().Hydrate(ctx, msgs, bob.ID))
		eq(t, "participants", len(msgs[0].Thread.ParticipantIDs), 1)
		eq(t, "participant", msgs[0].Thread.ParticipantIDs[0], bob.ID)
		messagesIndexInStep(t, s, all...)
	})

	t.Run("paging", func(t *testing.T) {
		pc := newChannel(t, s, "pages", false, ann.ID)
		all = append(all, pc.ID)
		var parent model.Message
		for i := 0; i < 12; i++ {
			if i == 5 {
				post(t, s, pc.ID, ann.ID, parent.ID, "reply")
				continue
			}
			m := post(t, s, pc.ID, ann.ID, "", "m")
			if i == 0 {
				parent = m
			}
		}
		// Seq 1 is member.joined's place? No: the fixture writes no
		// event for the join, so messages are seq 1 to 12.
		seqs := func(o store.MessageList) ([]int64, bool) {
			msgs, more, err := s.Messages().List(ctx, pc.ID, o)
			must(t, err)
			var out []int64
			for _, m := range msgs {
				out = append(out, m.Seq)
			}
			return out, more
		}
		got, more := seqs(store.MessageList{SeqPage: store.SeqPage{Limit: 5}})
		eq(t, "newest page", len(got) == 5 && got[0] == 8 && got[4] == 12, true)
		eq(t, "older exist", more, true)
		got, more = seqs(store.MessageList{SeqPage: store.SeqPage{Limit: 5, Before: 4}})
		eq(t, "before", len(got) == 3 && got[0] == 1 && got[2] == 3, true)
		eq(t, "nothing older", more, false)
		got, more = seqs(store.MessageList{SeqPage: store.SeqPage{Limit: 5, After: 9}})
		eq(t, "after", len(got) == 3 && got[0] == 10, true)
		eq(t, "nothing newer", more, false)
		got, more = seqs(store.MessageList{SeqPage: store.SeqPage{Limit: 4}, Around: 6})
		eq(t, "around includes the seq", len(got) == 4 && got[0] <= 6 && got[3] >= 6, true)
		eq(t, "around has more", more, true)
		got, _ = seqs(store.MessageList{SeqPage: store.SeqPage{Limit: 50}, TopLevel: true})
		eq(t, "top level leaves the reply out", len(got), 11)
		replies, more, err := s.Messages().Replies(ctx, parent.ID, store.SeqPage{})
		must(t, err)
		eq(t, "replies", len(replies) == 1 && !more, true)
	})

	t.Run("edit, versions, delete, purge", func(t *testing.T) {
		ec := newChannel(t, s, "edits", true, ann.ID)
		m := post(t, s, ec.ID, ann.ID, "", "teh text", model.Mention{AccountID: bob.ID, Kind: model.MentionAccount})
		quick, err := s.Messages().Edit(ctx, store.MessageEdit{ID: m.ID, Text: "the text", By: ann.ID, At: model.Now()})
		must(t, err)
		eq(t, "quick correction keeps the version", quick.Version, 1)
		eq(t, "not marked edited", quick.EditedAt.IsZero(), true)
		vs, err := s.Messages().Versions(ctx, m.ID)
		must(t, err)
		eq(t, "no version kept", len(vs), 0)
		mn, err := s.Messages().Mentions(ctx, m.ID)
		must(t, err)
		eq(t, "mentions replaced by none", len(mn), 0)

		at := model.Now()
		edited, err := s.Messages().Edit(ctx, store.MessageEdit{ID: m.ID, Text: "new text", KeepVersion: true, By: ann.ID, At: at,
			Mentions: []model.Mention{{AccountID: bob.ID, Kind: model.MentionAccount}}})
		must(t, err)
		eq(t, "version", edited.Version, 2)
		eq(t, "edited_at", edited.EditedAt, at)
		vs, _ = s.Messages().Versions(ctx, m.ID)
		eq(t, "one version", len(vs), 1)
		eq(t, "it holds the earlier text", vs[0].Text, "the text")
		eq(t, "reason", vs[0].Reason, "edit")
		msgs := []model.Message{edited}
		must(t, s.Messages().Hydrate(ctx, msgs, ann.ID))
		eq(t, "has_history", msgs[0].HasHistory, true)
		messagesIndexInStep(t, s, append(all, ec.ID)...)

		// Purging a live message erases its history and keeps it.
		_, err = s.Messages().Purge(ctx, m.ID, model.Now())
		must(t, err)
		vs, _ = s.Messages().Versions(ctx, m.ID)
		eq(t, "history erased", len(vs), 0)
		live, _ := s.Messages().Get(ctx, m.ID)
		eq(t, "still reads", live.Text, "new text")
		eq(t, "purged_at", live.PurgedAt.IsZero(), false)

		_, err = s.Reactions().Add(ctx, m.ID, bob.ID, "👍", model.Now())
		must(t, err)
		deleted, err := s.Messages().Delete(ctx, m.ID, ann.ID, model.Now())
		must(t, err)
		eq(t, "emptied", deleted.Text, "")
		eq(t, "deleted_by", deleted.DeletedByID, ann.ID)
		_, err = s.Messages().Delete(ctx, m.ID, ann.ID, model.Now())
		wantErr(t, err, model.ErrConflict)
		_, err = s.Messages().Edit(ctx, store.MessageEdit{ID: m.ID, Text: "x", By: ann.ID, At: model.Now()})
		wantErr(t, err, model.ErrConflict)
		vs, _ = s.Messages().Versions(ctx, m.ID)
		eq(t, "last content kept", len(vs) == 1 && vs[0].Reason == "delete" && vs[0].Text == "new text", true)
		rs, _ := s.Reactions().List(ctx, m.ID, bob.ID)
		eq(t, "reactions gone", len(rs), 0)
		mn, _ = s.Messages().Mentions(ctx, m.ID)
		eq(t, "mentions gone", len(mn), 0)
		messagesIndexInStep(t, s, append(all, ec.ID)...)

		// Purging a deleted message leaves a bare record.
		_, err = s.Messages().Purge(ctx, m.ID, model.Now())
		must(t, err)
		vs, _ = s.Messages().Versions(ctx, m.ID)
		eq(t, "nothing left to open", len(vs), 0)
		bare, err := s.Messages().Get(ctx, m.ID)
		must(t, err)
		eq(t, "the record stays", bare.AuthorID == ann.ID && bare.Deleted(), true)
		evs, _, err := s.Events().List(ctx, ec.ID, 0, 0, 50)
		must(t, err)
		eq(t, "its content events are gone", len(evs), 0)

		// Deleting the conversation takes its search entries along.
		post(t, s, ec.ID, ann.ID, "", "to be swept away")
		_, err = s.Conversations().Delete(ctx, ec.ID)
		must(t, err)
		messagesIndexInStep(t, s, all...)
	})

	t.Run("outright delete removes the row, its events and its entry", func(t *testing.T) {
		m := post(t, s, c.ID, ann.ID, "", "oops")
		_, err := s.Messages().DeleteOutright(ctx, m.ID)
		must(t, err)
		_, err = s.Messages().Get(ctx, m.ID)
		wantErr(t, err, model.ErrNotFound)
		evs, _, err := s.Events().List(ctx, c.ID, m.Seq-1, m.Seq, 10)
		must(t, err)
		eq(t, "event gone", len(evs), 0)
		messagesIndexInStep(t, s, all...)
	})
}
