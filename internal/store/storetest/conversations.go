package storetest

import (
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("conversations", testConversations) }

func testConversations(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	cat := newAccount(t, s, "cat", model.RoleMember)

	lib, err := s.Conversations().EnsureLibrary(ctx)
	must(t, err)
	again, err := s.Conversations().EnsureLibrary(ctx)
	must(t, err)
	eq(t, "one library, however often it is ensured", lib.ID, again.ID)
	eq(t, "the library is recognised", lib.IsLibrary() && lib.Private && lib.Name == "", true)

	pub := newChannel(t, s, "pub", false, ann.ID)
	priv := newChannel(t, s, "priv", true, ann.ID)
	eq(t, "defaults", pub.DefaultLevel == model.LevelMember && pub.NextSeq == 1 && pub.LastSeq == 0, true)
	got, err := s.Conversations().ByName(ctx, "pub")
	must(t, err)
	eq(t, "by name, with member count", got.ID == pub.ID && got.MemberCount == 1, true)
	many, err := s.Conversations().GetMany(ctx, []model.ConversationID{pub.ID, priv.ID, model.NewID[model.ConversationID]()})
	must(t, err)
	eq(t, "GetMany", len(many), 2)

	t.Run("sequence numbers", func(t *testing.T) {
		c := newChannel(t, s, "seq", false, ann.ID)
		for want := int64(1); want <= 3; want++ {
			seq, err := s.Conversations().NextSeq(ctx, c.ID)
			must(t, err)
			eq(t, "in order", seq, want)
		}
		// A failed transaction does not use a number up.
		err := s.Tx(ctx, func(tx store.Tx) error {
			if _, err := tx.Conversations().NextSeq(ctx, c.ID); err != nil {
				return err
			}
			return model.ErrConflict
		})
		wantErr(t, err, model.ErrConflict)
		seq, _ := s.Conversations().NextSeq(ctx, c.ID)
		eq(t, "the number comes round again", seq, int64(4))

		// The property the whole event log rests on: writers running at
		// once never get the same number, and none is skipped.
		const writers, each = 20, 5
		var wg sync.WaitGroup
		var mu sync.Mutex
		seen := map[int64]int{}
		errs := make(chan error, writers*each)
		for w := 0; w < writers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < each; i++ {
					err := s.Tx(ctx, func(tx store.Tx) error {
						seq, err := tx.Conversations().NextSeq(ctx, c.ID)
						if err != nil {
							return err
						}
						mu.Lock()
						seen[seq]++
						mu.Unlock()
						return tx.Events().Append(ctx, model.Event{Type: "test.happened", At: model.Now(), ConversationID: c.ID, Seq: seq})
					})
					if err != nil {
						errs <- err
					}
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		eq(t, "numbers handed out", len(seen), writers*each)
		for seq := int64(5); seq < 5+writers*each; seq++ {
			if seen[seq] != 1 {
				t.Fatalf("seq %d was handed out %d times", seq, seen[seq])
			}
		}
		c2, _ := s.Conversations().Get(ctx, c.ID)
		eq(t, "last_seq", c2.LastSeq, int64(4+writers*each))
		_, err = s.Conversations().NextSeq(ctx, model.NewID[model.ConversationID]())
		wantErr(t, err, model.ErrNotFound)
	})

	t.Run("update", func(t *testing.T) {
		sec := model.Section{Name: "Teams", Position: 1}
		must(t, s.Sections().Create(ctx, &sec))
		keep := int64(86400)
		out, err := s.Conversations().Update(ctx, pub.ID, func(c *model.Conversation) error {
			c.Topic, c.Welcome, c.SectionID, c.DefaultLevel = "All things public", "Hello!", sec.ID, model.LevelReader
			c.RetentionSeconds, c.SlowModeSeconds, c.ArchivedAt = &keep, 10, 77
			c.NextSeq, c.LockedAt = 999, 5 // not Update's to change
			return nil
		})
		must(t, err)
		eq(t, "returned", out.Topic, "All things public")
		got, _ := s.Conversations().Get(ctx, pub.ID)
		eq(t, "stored", got.Welcome == "Hello!" && got.SectionID == sec.ID && got.DefaultLevel == model.LevelReader && *got.RetentionSeconds == 86400 && got.SlowModeSeconds == 10 && got.ArchivedAt == 77, true)
		eq(t, "next_seq and the lock are untouched", got.NextSeq == 1 && !got.Locked(), true)
		_, err = s.Conversations().Update(ctx, priv.ID, func(c *model.Conversation) error { c.Private = false; return nil })
		wantErr(t, err, model.ErrConflict)
		_, err = s.Conversations().Update(ctx, pub.ID, func(c *model.Conversation) error { c.ArchivedAt, c.RetentionSeconds = 0, nil; return nil })
		must(t, err)
		must(t, s.Sections().Delete(ctx, sec.ID))
		got, _ = s.Conversations().Get(ctx, pub.ID)
		eq(t, "a deleted section leaves the channel without one", got.SectionID, model.SectionID(""))
	})

	t.Run("public to private", func(t *testing.T) {
		c := newChannel(t, s, "going-private", false, ann.ID)
		join(t, s, c.ID, bob.ID, model.LevelMember)
		m := post(t, s, c.ID, ann.ID, "", "@bob @cat",
			model.Mention{AccountID: bob.ID, Kind: model.MentionAccount}, model.Mention{AccountID: cat.ID, Kind: model.MentionAccount})
		out, err := s.Conversations().MakePrivate(ctx, c.ID)
		must(t, err)
		eq(t, "private now", out.Private, true)
		mentions, _ := s.Messages().Mentions(ctx, m.ID)
		eq(t, "only members keep their mentions", len(mentions) == 1 && mentions[0].AccountID == bob.ID, true)
		eq(t, "levels unchanged", level(t, s, c.ID, bob.ID), model.LevelMember)
	})

	t.Run("locking", func(t *testing.T) {
		_, err := s.Conversations().Lock(ctx, pub.ID, ann.ID, model.Now())
		wantErr(t, err, model.ErrConflict)
		at := model.Now()
		out, err := s.Conversations().Lock(ctx, priv.ID, ann.ID, at)
		must(t, err)
		eq(t, "locked", out.LockedAt == at && out.LockedBy == ann.ID, true)
		again, err := s.Conversations().Lock(ctx, priv.ID, bob.ID, model.Now().Add(time.Second))
		must(t, err)
		eq(t, "locking again changes nothing", again.LockedAt == at && again.LockedBy == ann.ID, true)
		// Someone who leaves cannot return; the last member leaving
		// deletes it like any private channel (cases 185, 187).
		res, err := s.Members().RemoveDirect(ctx, priv.ID, ann.ID)
		must(t, err)
		eq(t, "deleted with its last member", len(res.Deleted), 1)
	})

	t.Run("member keys", func(t *testing.T) {
		key := store.AdhocMemberKey([]model.AccountID{bob.ID, ann.ID})
		eq(t, "the key ignores order", key, store.AdhocMemberKey([]model.AccountID{ann.ID, bob.ID, ann.ID}))
		eq(t, "a sealed key ignores order", store.SealedMemberKey(ann.ID, bob.ID), store.SealedMemberKey(bob.ID, ann.ID))
		older := newAdhoc(t, s, ann.ID, bob.ID)
		found, err := s.Conversations().ByMemberKey(ctx, key)
		must(t, err)
		eq(t, "found by who is in it", found.ID, older.ID)
		clash := model.Conversation{Kind: model.KindChannel, Private: true, MemberKey: key}
		wantErr(t, s.Conversations().Create(ctx, &clash), model.ErrAlreadyExists)

		// Decision S8: a second ad-hoc channel comes to have the same
		// members; the older keeps the key and the newer has none.
		newer := newAdhoc(t, s, ann.ID, bob.ID, cat.ID)
		_, err = s.Members().RemoveDirect(ctx, newer.ID, cat.ID)
		must(t, err)
		n, _ := s.Conversations().Get(ctx, newer.ID)
		eq(t, "the newer channel's key is cleared", n.MemberKey, "")
		found, _ = s.Conversations().ByMemberKey(ctx, key)
		eq(t, "the older keeps the key", found.ID, older.ID)
		// Members change, the key follows.
		join(t, s, older.ID, cat.ID, model.LevelMember)
		o, _ := s.Conversations().Get(ctx, older.ID)
		eq(t, "the key follows the members", o.MemberKey, store.AdhocMemberKey([]model.AccountID{ann.ID, bob.ID, cat.ID}))
		must(t, s.Conversations().RefreshMemberKey(ctx, pub.ID)) // nothing to do for a named channel
		_, _, err = s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: older.ID, GroupID: model.NewID[model.GroupID](), Level: model.LevelMember})
		wantErr(t, err, model.ErrConflict)
	})

	t.Run("lists", func(t *testing.T) {
		archived := newChannel(t, s, "archived", false, bob.ID)
		_, err := s.Conversations().Update(ctx, archived.ID, func(c *model.Conversation) error { c.ArchivedAt = model.Now(); return nil })
		must(t, err)
		quiet := newChannel(t, s, "quiet-room", true, bob.ID)
		loud := newChannel(t, s, "loud-room", false, bob.ID)
		post(t, s, loud.ID, bob.ID, "", "most recent")

		mine, next, err := s.Conversations().ForAccount(ctx, bob.ID, store.ConversationFilter{}, store.Page{Limit: 2})
		must(t, err)
		eq(t, "most recently active first", mine[0].Conversation.ID, loud.ID)
		eq(t, "with the member row", mine[0].Member.AccountID == bob.ID && mine[0].Member.Level == model.LevelManager, true)
		var all []store.ConversationMember
		all = append(all, mine...)
		for next != "" {
			var page []store.ConversationMember
			page, next, err = s.Conversations().ForAccount(ctx, bob.ID, store.ConversationFilter{}, store.Page{Limit: 2, Cursor: next})
			must(t, err)
			all = append(all, page...)
		}
		ids := map[model.ConversationID]bool{}
		for _, cm := range all {
			if ids[cm.Conversation.ID] {
				t.Fatal("a conversation appears on two pages")
			}
			ids[cm.Conversation.ID] = true
			if cm.Conversation.IsLibrary() {
				t.Fatal("the library appears in a list")
			}
		}
		eq(t, "archived left out by default", ids[archived.ID], false)
		eq(t, "its own channels are there", ids[quiet.ID] && ids[loud.ID], true)
		withArchived, _, _ := s.Conversations().ForAccount(ctx, bob.ID, store.ConversationFilter{IncludeArchived: true}, store.Page{Limit: 200})
		eq(t, "archived when asked", len(withArchived), len(all)+1)
		sealed, _, _ := s.Conversations().ForAccount(ctx, bob.ID, store.ConversationFilter{Kind: model.KindSealed}, store.Page{})
		eq(t, "by kind", len(sealed), 0)

		browse := func(f store.BrowseFilter) map[string]bool {
			out := map[string]bool{}
			cursor := ""
			for {
				page, next, err := s.Conversations().Browse(ctx, f, store.Page{Limit: 2, Cursor: cursor})
				must(t, err)
				for _, c := range page {
					if out[c.Name] {
						t.Fatalf("%s appears on two pages", c.Name)
					}
					out[c.Name] = true
				}
				if cursor = next; next == "" {
					return out
				}
			}
		}
		asMember := browse(store.BrowseFilter{AccountID: cat.ID, Public: true})
		eq(t, "public channels are browsable", asMember["loud-room"] && asMember["pub"], true)
		eq(t, "private ones one is not in are not", asMember["quiet-room"], false)
		eq(t, "archived ones are left out", asMember["archived"], false)
		eq(t, "ad-hoc channels are never browsed", asMember[""], false)
		asGuest := browse(store.BrowseFilter{AccountID: cat.ID})
		eq(t, "without Public, only what one is in", len(asGuest), 0)
		eq(t, "own private channel", browse(store.BrowseFilter{AccountID: bob.ID, Public: true})["quiet-room"], true)
		eq(t, "by query", len(browse(store.BrowseFilter{AccountID: bob.ID, Public: true, Query: "ROOM"})), 2)
		eq(t, "archived when asked", browse(store.BrowseFilter{AccountID: cat.ID, Public: true, IncludeArchived: true})["archived"], true)
	})
}
