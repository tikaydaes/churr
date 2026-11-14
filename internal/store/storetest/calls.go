package storetest

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("calls", testCalls) }

func testCalls(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	c := newChannel(t, s, "standup", false, ann.ID)
	other := newChannel(t, s, "elsewhere", false, ann.ID)

	_, found, err := s.Calls().Live(ctx, c.ID)
	must(t, err)
	eq(t, "no call yet", found, false)

	first := store.StoredCall{ID: model.NewID[model.CallID](), ConversationID: c.ID, StartedBy: ann.ID, StartedAt: 1000}
	started, err := s.Calls().Start(ctx, first)
	must(t, err)
	eq(t, "the first start", started, true)
	// Two people starting at the same moment end up in one call.
	second := store.StoredCall{ID: model.NewID[model.CallID](), ConversationID: c.ID, StartedBy: bob.ID, StartedAt: 1001}
	started, err = s.Calls().Start(ctx, second)
	must(t, err)
	eq(t, "the second start does nothing", started, false)
	_, err = s.Calls().Get(ctx, second.ID)
	wantErr(t, err, model.ErrNotFound)
	// Another conversation has its own.
	elsewhere := store.StoredCall{ID: model.NewID[model.CallID](), ConversationID: other.ID, StartedBy: ann.ID, StartedAt: 1002}
	started, err = s.Calls().Start(ctx, elsewhere)
	must(t, err)
	eq(t, "a call in another conversation", started, true)

	live, found, err := s.Calls().Live(ctx, c.ID)
	must(t, err)
	eq(t, "the live call", found && live.ID == first.ID && live.StartedBy == ann.ID && live.StartedAt == 1000 && live.Live(), true)
	eq(t, "hosted here", live.HostServerID == "" && live.HostName == "", true)
	all, err := s.Calls().AllLive(ctx)
	must(t, err)
	eq(t, "every live call, oldest first", len(all) == 2 && all[0].ID == first.ID && all[1].ID == elsewhere.ID, true)

	// The notice message, and its change when the call ends.
	notice := post(t, s, c.ID, ann.ID, "", "")
	must(t, s.Calls().SetMessage(ctx, first.ID, notice.ID))
	wantErr(t, s.Calls().SetMessage(ctx, second.ID, notice.ID), model.ErrNotFound)
	got, err := s.Calls().Get(ctx, first.ID)
	must(t, err)
	eq(t, "the notice is recorded", got.MessageID, notice.ID)
	conv, err := s.Calls().SetNotice(ctx, notice.ID, map[string]any{"kind": "call_ended", "call_id": string(first.ID), "duration_seconds": 12})
	must(t, err)
	eq(t, "the notice's conversation", conv, c.ID)
	m, err := s.Messages().Get(ctx, notice.ID)
	must(t, err)
	eq(t, "the notice changed", m.System["kind"] == "call_ended" && m.System["duration_seconds"] == float64(12), true)
	eq(t, "and is not marked as edited", m.EditedAt.IsZero(), true)
	_, err = s.Calls().SetNotice(ctx, model.NewID[model.MessageID](), map[string]any{"kind": "call_ended"})
	wantErr(t, err, model.ErrNotFound)

	// Stays: one open stay per account, a new row for someone who returns.
	joined, err := s.Calls().Join(ctx, first.ID, ann.ID, 1100)
	must(t, err)
	eq(t, "ann joins", joined, true)
	joined, err = s.Calls().Join(ctx, first.ID, ann.ID, 1150)
	must(t, err)
	eq(t, "and cannot be in twice", joined, false)
	joined, _ = s.Calls().Join(ctx, first.ID, bob.ID, 1200)
	eq(t, "bob joins", joined, true)
	left, err := s.Calls().Leave(ctx, first.ID, bob.ID, 1300)
	must(t, err)
	eq(t, "bob leaves", left, true)
	left, _ = s.Calls().Leave(ctx, first.ID, bob.ID, 1301)
	eq(t, "and cannot leave twice", left, false)
	// Back in the very millisecond the first stay began: the key still
	// holds.
	joined, err = s.Calls().Join(ctx, first.ID, bob.ID, 1200)
	must(t, err)
	eq(t, "bob returns", joined, true)
	present, err := s.Calls().Present(ctx, first.ID)
	must(t, err)
	eq(t, "who is in it, in the order joined", len(present) == 2 && present[0].AccountID == ann.ID && present[0].JoinedAt == 1100 &&
		present[1].AccountID == bob.ID && present[1].LeftAt.IsZero(), true)
	ever, err := s.Calls().EverIn(ctx, first.ID, 50)
	must(t, err)
	eq(t, "everyone who was in it, each once", len(ever) == 2 && ever[0] == ann.ID && ever[1] == bob.ID, true)
	ever, _ = s.Calls().EverIn(ctx, first.ID, 1)
	eq(t, "capped", len(ever), 1)

	// Ending closes the stays, once.
	ended, err := s.Calls().End(ctx, first.ID, 5000)
	must(t, err)
	eq(t, "ended", ended, true)
	ended, err = s.Calls().End(ctx, first.ID, 6000)
	must(t, err)
	eq(t, "not twice", ended, false)
	got, _ = s.Calls().Get(ctx, first.ID)
	eq(t, "the end is kept", got.EndedAt == 5000 && !got.Live(), true)
	present, _ = s.Calls().Present(ctx, first.ID)
	eq(t, "nobody is in an ended call", len(present), 0)
	_, found, _ = s.Calls().Live(ctx, c.ID)
	eq(t, "no live call now", found, false)
	// A new call can start once the old one has ended.
	started, err = s.Calls().Start(ctx, second)
	must(t, err)
	eq(t, "a new call after the old one", started, true)

	// Inside a transaction, as the service uses it.
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		if _, err := tx.Calls().End(ctx, second.ID, 7000); err != nil {
			return err
		}
		third := store.StoredCall{ID: model.NewID[model.CallID](), ConversationID: c.ID, StartedBy: ann.ID, StartedAt: 7001}
		ok, err := tx.Calls().Start(ctx, third)
		if err == nil && !ok {
			t.Error("a start in the transaction that ended the last call did nothing")
		}
		return err
	}))

	// The rows go with the conversation.
	_, err = s.Conversations().Delete(ctx, other.ID)
	must(t, err)
	_, err = s.Calls().Get(ctx, elsewhere.ID)
	wantErr(t, err, model.ErrNotFound)
}
