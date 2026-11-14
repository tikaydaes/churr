package notify

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// crowd is a channel whose members differ in every way the rule looks at.
type crowd struct {
	w        *world
	ann      model.Account
	c        model.Conversation
	everyone []model.Account
	members  []model.Account
	parent   model.Message
}

func newCrowd(t *testing.T) *crowd {
	w := newWorld(t)
	k := &crowd{w: w, ann: w.account("ann", model.RoleOwner)}
	names := []string{"any", "men", "non", "flat", "dnd", "blk", "fol", "ign", "muted"}
	by := map[string]model.Account{}
	for _, n := range names {
		by[n] = w.account(n, model.RoleMember)
		k.members = append(k.members, by[n])
	}
	bot := w.account("bot", model.RoleBot)
	k.members = append(k.members, bot, k.ann)
	k.c = w.channel("crowd", false, k.ann, k.members[:len(k.members)-1]...)
	// Two who are not members: one the messages mention, one they do not.
	eve, gus := w.account("eve", model.RoleMember), w.account("gus", model.RoleGuest)
	k.everyone = append(append([]model.Account{}, k.members...), eve, gus)

	for _, a := range k.members {
		w.mode(k.c, a, setMode(model.ModeAny))
	}
	w.mode(k.c, by["men"], setMode(model.ModeMentions))
	w.mode(k.c, by["non"], setMode(model.ModeNothing))
	w.mode(k.c, by["flat"], func(m *model.Member) { m.ThreadView = "flat" })
	w.mode(k.c, by["muted"], func(m *model.Member) {
		m.Mode, m.ModeBeforeMute, m.MutedUntil = model.ModeNothing, model.ModeAny, model.Now().Add(time.Hour)
	})
	w.update(by["dnd"], func(a *model.Account) { zero := model.Time(0); a.DNDUntil = &zero })
	w.must(w.s.Blocks().Add(ctx, by["blk"].ID, k.ann.ID, model.Now()))
	k.parent = w.post(k.c, k.ann, "")
	_, err := w.s.Threads().Set(ctx, k.parent.ID, by["fol"].ID, model.ThreadFollowing, 0, model.Now())
	w.must(err)
	_, err = w.s.Threads().Set(ctx, k.parent.ID, by["ign"].ID, model.ThreadIgnored, 0, model.Now())
	w.must(err)
	return k
}

// mentions names everyone once: some by name, the rest as @channel would.
func (k *crowd) mentions() []model.Mention {
	var out []model.Mention
	for i, a := range k.everyone {
		kind := model.MentionAccount
		if i%2 == 1 {
			kind = model.MentionChannel
		}
		out = append(out, mention(a, kind))
	}
	return out
}

// store writes a message without telling the notify service, which the
// caller does.
func (k *crowd) store(parent model.MessageID, mentions ...model.Mention) model.Message {
	k.w.t.Helper()
	m := model.Message{ID: model.NewID[model.MessageID](), ConversationID: k.c.ID, AuthorID: k.ann.ID, ParentID: parent, Text: "hello", CreatedAt: model.Now()}
	k.w.must(k.w.s.Tx(ctx, func(tx store.Tx) error {
		seq, err := tx.Conversations().NextSeq(ctx, k.c.ID)
		if err != nil {
			return err
		}
		m.Seq = seq
		for i := range mentions {
			mentions[i].MessageID = m.ID
		}
		return tx.Messages().Insert(ctx, &m, mentions)
	}))
	return m
}

// TestJudgeAgreesWithDecide checks that the rule worked out for everyone at
// once gives each account the answer it gets when asked about alone.
func TestJudgeAgreesWithDecide(t *testing.T) {
	k := newCrowd(t)
	w := k.w
	msgs := map[string]model.Message{
		"top-level":             k.store(""),
		"mentions":              k.store("", k.mentions()...),
		"a reply":               k.store(k.parent.ID),
		"a reply that mentions": k.store(k.parent.ID, k.mentions()...),
	}
	sys := msgs["top-level"]
	sys.System = map[string]any{"kind": "joined"}
	msgs["a system notice"] = sys
	quiet := msgs["mentions"]
	quiet.Quiet = true
	msgs["a quiet message"] = quiet
	rem := msgs["top-level"]
	rem.Via = &model.Via{Kind: "reminder"}
	msgs["a reminder"] = rem

	compare := func(setting string) {
		t.Helper()
		for name, m := range msgs {
			var also []model.AccountID
			for _, a := range k.everyone {
				also = append(also, a.ID)
			}
			f, err := w.n.Judge(ctx, m, also)
			w.must(err)
			if len(f.Accounts()) != len(k.everyone) {
				t.Fatalf("%s: judged %d accounts, want %d", name, len(f.Accounts()), len(k.everyone))
			}
			notified := 0
			for _, a := range k.everyone {
				got, known := f.Level(a.ID)
				if want := w.n.Decide(ctx, m, a.ID); !known || got != want {
					t.Errorf("%s, %s, %s: Judge says %s (known %v), Decide says %s", setting, name, a.Username, got, known, want)
				}
				wake, high := f.Wake(a.ID)
				wantWake, wantHigh := w.n.Wake(ctx, m, a.ID)
				if wake != wantWake || high != wantHigh {
					t.Errorf("%s, %s, %s: Judge wakes %v/%v, Wake says %v/%v", setting, name, a.Username, wake, high, wantWake, wantHigh)
				}
				if got == Notify {
					notified++
				}
			}
			if name == "mentions" && notified == 0 {
				t.Fatalf("%s: nobody is notified of a message that mentions everyone; the comparison proves nothing", setting)
			}
		}
	}
	compare("blocking on")
	w.must(w.s.Settings().Set(ctx, store.SettingBlockingEnabled, false, ""))
	compare("blocking off")

	if _, known := (&Fanout{}).Level(k.ann.ID); known {
		t.Error("an account the fan-out did not cover must be reported as not known")
	}
}

// TestCreatedMovesKeptNumbers checks that numbers moved along with each new
// message, without being read again, stay equal to what the store says.
func TestCreatedMovesKeptNumbers(t *testing.T) {
	k := newCrowd(t)
	w := k.w
	// What the store says, through a service that keeps nothing yet.
	fresh := func(a model.Account) (Counts, model.Totals) {
		t.Helper()
		c, total, _, err := New(w.s, perm.New(w.s)).Numbers(ctx, a.ID, k.c.ID)
		w.must(err)
		return c, total
	}
	keep := func() {
		t.Helper()
		for _, a := range k.members {
			_, err := w.n.Totals(ctx, a.ID)
			w.must(err)
		}
	}
	created := func(m model.Message) {
		t.Helper()
		f, err := w.n.Judge(ctx, m, nil)
		w.must(err)
		w.n.Created(model.Event{Type: model.EventMessageCreated, ConversationID: k.c.ID, Seq: m.Seq, MessageID: m.ID, At: m.CreatedAt}, f)
	}
	check := func(what string, fromMemory bool) {
		t.Helper()
		for _, a := range k.members {
			wantC, wantT := fresh(a)
			c, total, listed, ok := w.n.Kept(a.ID, k.c.ID)
			// A reply in a followed thread has the thread count read
			// again; everything else must come from memory.
			if fromMemory && !ok && a.Username != "fol" {
				t.Errorf("%s, %s: the numbers were not kept, so every message would be a read", what, a.Username)
			}
			if ok && (!listed || c != wantC || total != wantT) {
				t.Errorf("%s, %s: kept %+v %+v, the store says %+v %+v", what, a.Username, c, total, wantC, wantT)
			}
			c, total, _, err := w.n.Numbers(ctx, a.ID, k.c.ID)
			w.must(err)
			if c != wantC || total != wantT {
				t.Errorf("%s, %s: Numbers %+v %+v, the store says %+v %+v", what, a.Username, c, total, wantC, wantT)
			}
		}
	}

	keep()
	check("before anything", true)
	for i := 0; i < 3; i++ {
		created(k.store(""))
		check("a top-level message", true)
	}
	created(k.store("", k.mentions()...))
	check("a message that mentions everyone", true)
	created(k.store(k.parent.ID))
	check("a reply", true)
	created(k.store(k.parent.ID, k.mentions()...))
	check("a reply that mentions everyone", true)

	// Numbers read after a message was stored count it already: its
	// event, arriving late, must not count it twice.
	late := k.store("")
	w.n.Forget(k.members[0].ID)
	keep()
	created(late)
	check("an event that arrives after the numbers were read", true)

	// A message whose event never came leaves a gap the next one sees:
	// the numbers are then read, not guessed.
	k.store("")
	created(k.store(""))
	check("after an event went missing", false)
	if _, _, _, ok := w.n.Kept(k.members[0].ID, k.c.ID); !ok {
		t.Error("the numbers should be kept again once they have been read")
	}

	// Anything else that happens in the conversation still marks them.
	created(k.store(""))
	w.n.Changed(model.Event{Type: model.EventMessageDeleted, ConversationID: k.c.ID, Seq: 99})
	if _, _, _, ok := w.n.Kept(k.members[0].ID, k.c.ID); ok {
		t.Error("another event in the conversation must make the numbers be read again")
	}
}
