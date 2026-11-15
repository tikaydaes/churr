package notify

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// TestReaderAgreesWithDecide checks that the rule worked out for one
// account and many messages at once gives each message the answer it gets
// when asked about alone.
func TestReaderAgreesWithDecide(t *testing.T) {
	k := newCrowd(t)
	w := k.w
	// A second conversation, which only some are in, so one batch spans
	// two; and a notify-anyway request that reached the one in do not
	// disturb.
	other := w.channel("other", false, k.ann, k.members[:3]...)
	inOther := k.store("", k.mentions()...)
	inOther.ConversationID = other.ID
	second := k.store(k.parent.ID)
	second.ID = model.NewID[model.MessageID]()

	named := []string{"top-level", "mentions", "a reply", "a reply that mentions", "a system notice",
		"a quiet message", "a reminder", "in another conversation", "pushed through", "not stored", "by the reader"}
	msgs := []model.Message{k.store(""), k.store("", k.mentions()...), k.store(k.parent.ID), k.store(k.parent.ID, k.mentions()...)}
	sys := msgs[0]
	sys.System = map[string]any{"kind": "joined"}
	quiet := msgs[1]
	quiet.Quiet = true
	rem := msgs[0]
	rem.Via = &model.Via{Kind: "reminder"}
	pushed := k.store("", k.mentions()...)
	for _, a := range k.everyone {
		if a.Username == "dnd" {
			_, _, err := w.s.NotifyAnyway().Record(ctx, pushed.ID, a.ID, k.ann.ID, model.Now(), true)
			w.must(err)
		}
	}
	msgs = append(msgs, sys, quiet, rem, inOther, pushed, second)

	compare := func(setting string) {
		t.Helper()
		levels := map[Level]int{}
		for _, a := range k.everyone {
			// One of the reader's own, so step 3 is covered for everyone.
			own := msgs[0]
			own.AuthorID = a.ID
			all := append(append([]model.Message{}, msgs...), own)
			got := w.n.Reader(a.ID).Decide(ctx, all)
			if len(got) != len(all) {
				t.Fatalf("%s, %s: %d answers for %d messages", setting, a.Username, len(got), len(all))
			}
			for i, m := range all {
				want := w.n.Decide(ctx, m, a.ID)
				if got[i] != want {
					t.Errorf("%s, %s, %s: Reader says %s, Decide says %s", setting, a.Username, named[i], got[i], want)
				}
				levels[want]++
			}
			// The same when the caller has already read the account's
			// place in the conversations, as a catch-up has.
			told := w.n.Reader(a.ID)
			for _, conv := range []model.ConversationID{k.c.ID, other.ID} {
				st, err := w.n.perm.Load(ctx, a, conv)
				w.must(err)
				told.Know(st)
			}
			for i, level := range told.Decide(ctx, all) {
				if level != got[i] {
					t.Errorf("%s, %s, %s: told what is known it says %s, reading for itself %s", setting, a.Username, named[i], level, got[i])
				}
			}
			// One at a time, and in two batches with one Reader, the same.
			r := w.n.Reader(a.ID)
			for i, half := range [][]model.Message{all[:5], all[5:]} {
				for j, level := range r.Decide(ctx, half) {
					if level != got[i*5+j] {
						t.Errorf("%s, %s, %s: a second batch says %s, the whole says %s", setting, a.Username, named[i*5+j], level, got[i*5+j])
					}
				}
			}
		}
		for _, level := range []Level{None, Badge, Notify} {
			if levels[level] == 0 {
				t.Fatalf("%s: no answer is %q; the comparison proves too little", setting, level)
			}
		}
	}
	compare("blocking on")
	w.must(w.s.Settings().Set(ctx, store.SettingBlockingEnabled, false, ""))
	compare("blocking off")

	if got := w.n.Reader(k.ann.ID).Decide(ctx, nil); len(got) != 0 {
		t.Errorf("no messages, %d answers", len(got))
	}
	gone := w.n.Reader(model.NewID[model.AccountID]()).Decide(ctx, msgs)
	for i, level := range gone {
		if level != None {
			t.Errorf("an account that does not exist, %s: %s", named[i], level)
		}
	}
}
