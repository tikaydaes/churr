package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// The rest of moderation (small.go holds the time-out basics perm needs),
// custom emoji, the preview cache, the admin list of channels, the thread
// move and the janitor's deletions.

func init() {
	register("timeouts", testTimeouts)
	register("reports", testReports)
	register("purge_requests", testPurgeRequests)
	register("word_filters", testWordFilters)
	register("emoji", testEmoji)
	register("previews", testPreviews)
	register("admin_channels", testAdminChannels)
	register("thread_move", testThreadMove)
	register("janitor", testJanitor)
	register("card_files", testCardFiles)
}

func testTimeouts(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	pub := newChannel(t, s, "pub", false, ann.ID)
	priv := newChannel(t, s, "priv", true, ann.ID)
	now := model.Now()

	first := model.Timeout{AccountID: bob.ID, ConversationID: pub.ID, Until: now.Add(time.Hour), CreatedByID: ann.ID, Reason: "one"}
	lifted, err := s.Moderation().ReplaceTimeout(ctx, &first)
	must(t, err)
	eq(t, "nothing to replace", len(lifted), 0)
	wide := model.Timeout{AccountID: bob.ID, Until: now.Add(time.Hour), CreatedByID: ann.ID}
	lifted, err = s.Moderation().ReplaceTimeout(ctx, &wide)
	must(t, err)
	eq(t, "another scope is left alone", len(lifted), 0)
	second := model.Timeout{AccountID: bob.ID, ConversationID: pub.ID, Until: now.Add(2 * time.Hour), CreatedByID: ann.ID}
	lifted, err = s.Moderation().ReplaceTimeout(ctx, &second)
	must(t, err)
	eq(t, "the one in force is lifted", len(lifted) == 1 && lifted[0].ID == first.ID && !lifted[0].LiftedAt.IsZero(), true)
	got, err := s.Moderation().GetTimeout(ctx, first.ID)
	must(t, err)
	eq(t, "kept with lifted_at", got.Reason == "one" && !got.LiftedAt.IsZero(), true)
	_, err = s.Moderation().GetTimeout(ctx, model.NewID[string]())
	wantErr(t, err, model.ErrNotFound)

	hidden := model.Timeout{AccountID: bob.ID, ConversationID: priv.ID, Until: now.Add(time.Hour), CreatedByID: ann.ID}
	must(t, s.Moderation().CreateTimeout(ctx, &hidden))
	in, _, err := s.Moderation().Timeouts(ctx, store.TimeoutFilter{ConversationID: pub.ID}, now, store.Page{})
	must(t, err)
	eq(t, "in force in the channel", len(in) == 1 && in[0].ID == second.ID, true)
	admin, _, err := s.Moderation().Timeouts(ctx, store.TimeoutFilter{AdminView: true}, now, store.Page{})
	must(t, err)
	eq(t, "the admin view has no private channel's", len(admin), 2)
	page, next, err := s.Moderation().Timeouts(ctx, store.TimeoutFilter{AdminView: true}, now, store.Page{Limit: 1})
	must(t, err)
	eq(t, "paged", len(page) == 1 && next != "", true)
	rest, next, _ := s.Moderation().Timeouts(ctx, store.TimeoutFilter{AdminView: true}, now, store.Page{Limit: 1, Cursor: next})
	eq(t, "second page", len(rest) == 1 && next == "" && rest[0].ID != page[0].ID, true)
	later, _, _ := s.Moderation().Timeouts(ctx, store.TimeoutFilter{AdminView: true}, now.Add(3*time.Hour), store.Page{})
	eq(t, "ended ones are not in force", len(later), 0)
}

func testReports(t *testing.T, s store.Store) {
	owner := newAccount(t, s, "owner", model.RoleOwner)
	ann := newAccount(t, s, "ann", model.RoleMember)
	bob := newAccount(t, s, "bob", model.RoleMember)
	pub := newChannel(t, s, "pub", false, ann.ID)
	priv := newChannel(t, s, "priv", true, ann.ID)
	join(t, s, priv.ID, owner.ID, model.LevelMember)
	join(t, s, priv.ID, bob.ID, model.LevelMember)
	mp := post(t, s, pub.ID, ann.ID, "", "public")
	mv := post(t, s, priv.ID, bob.ID, "", "private")

	r := model.Report{MessageID: mp.ID, ConversationID: pub.ID, ReportedByID: bob.ID, Reason: "rude"}
	created, err := s.Moderation().CreateReport(ctx, &r)
	must(t, err)
	eq(t, "created", created && r.Source == model.ReportByPerson && r.Waiting(), true)
	again := model.Report{MessageID: mp.ID, ConversationID: pub.ID, ReportedByID: bob.ID, Reason: "again"}
	created, err = s.Moderation().CreateReport(ctx, &again)
	must(t, err)
	eq(t, "a second report by the same person returns the first", !created && again.ID == r.ID && again.Reason == "rude", true)

	// A flag has no reporter (ruling R27), and there is one per message.
	flag := model.Report{MessageID: mv.ID, ConversationID: priv.ID, Source: model.ReportByFilter, Reason: "filter-id"}
	created, err = s.Moderation().CreateReport(ctx, &flag)
	must(t, err)
	eq(t, "flagged", created && flag.ReportedByID == "", true)
	flag2 := model.Report{MessageID: mv.ID, ConversationID: priv.ID, Source: model.ReportByFilter}
	created, _ = s.Moderation().CreateReport(ctx, &flag2)
	eq(t, "one flag per message", !created && flag2.ID == flag.ID, true)
	_, err = s.Moderation().CreateReport(ctx, &model.Report{MessageID: mv.ID, ConversationID: priv.ID, Source: model.ReportByFilter, ReportedByID: bob.ID})
	eq(t, "a filter's report with a reporter is refused", err != nil, true)

	list := func(scope store.ReportScope, f store.ReportFilter) []model.Report {
		f.Scope = scope
		out, _, err := s.Moderation().Reports(ctx, f, store.Page{})
		must(t, err)
		return out
	}
	// The stored level decides in a private channel: the owner is in it as
	// a member and sees nothing of it.
	eq(t, "an admin sees public channels' reports only", len(list(store.ReportScope{AccountID: owner.ID, PublicChannels: true}, store.ReportFilter{})), 1)
	eq(t, "the manager sees both", len(list(store.ReportScope{AccountID: ann.ID}, store.ReportFilter{})), 2)
	eq(t, "a member sees none", len(list(store.ReportScope{AccountID: bob.ID}, store.ReportFilter{})), 0)
	eq(t, "by conversation", len(list(store.ReportScope{AccountID: ann.ID}, store.ReportFilter{ConversationID: priv.ID})), 1)
	eq(t, "nobody asking", len(list(store.ReportScope{}, store.ReportFilter{})), 0)
	n, err := s.Moderation().WaitingReports(ctx)
	must(t, err)
	eq(t, "waiting in public channels", n, int64(1))

	settled, err := s.Moderation().SettleReports(ctx, mp.ID, model.ReportDismissed, ann.ID, model.Now())
	must(t, err)
	eq(t, "settled", len(settled) == 1 && settled[0].Outcome == model.ReportDismissed && settled[0].HandledByID == ann.ID, true)
	eq(t, "waiting only", len(list(store.ReportScope{AccountID: ann.ID}, store.ReportFilter{Waiting: true})), 1)
	got, err := s.Moderation().GetReport(ctx, r.ID)
	must(t, err)
	eq(t, "read back", got.Outcome == model.ReportDismissed && !got.HandledAt.IsZero(), true)
	// Once the first is handled the same person may report again.
	third := model.Report{MessageID: mp.ID, ConversationID: pub.ID, ReportedByID: bob.ID}
	created, _ = s.Moderation().CreateReport(ctx, &third)
	eq(t, "a new report after the first was handled", created, true)
}

func testPurgeRequests(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	c := newChannel(t, s, "c", false, ann.ID)
	m := post(t, s, c.ID, ann.ID, "", "text")
	_, found, err := s.Moderation().LatestPurgeRequest(ctx, m.ID)
	must(t, err)
	eq(t, "none yet", found, false)
	p := model.PurgeRequest{MessageID: m.ID, RequestedByID: ann.ID}
	must(t, s.Moderation().CreatePurgeRequest(ctx, &p))
	must(t, s.Messages().SetHistoryHidden(ctx, m.ID, model.Now()))
	got, err := s.Messages().Get(ctx, m.ID)
	must(t, err)
	eq(t, "history hidden", got.HistoryHiddenAt.IsZero(), false)
	latest, found, _ := s.Moderation().LatestPurgeRequest(ctx, m.ID)
	eq(t, "waiting", found && latest.ID == p.ID && latest.Waiting(), true)
	in, err := s.Moderation().WaitingPurgeRequestsIn(ctx, c.ID)
	must(t, err)
	eq(t, "waiting in the conversation", len(in), 1)
	n, _ := s.Moderation().WaitingPurgeRequests(ctx)
	eq(t, "counted", n, int64(1))

	done, err := s.Moderation().DecidePurgeRequest(ctx, p.ID, model.PurgeShown, ann.ID, model.Now())
	must(t, err)
	eq(t, "decided", done.Outcome == model.PurgeShown && done.DecidedByID == ann.ID, true)
	_, err = s.Moderation().DecidePurgeRequest(ctx, p.ID, model.PurgeErased, ann.ID, model.Now())
	wantErr(t, err, model.ErrConflict)
	_, err = s.Moderation().DecidePurgeRequest(ctx, model.NewID[string](), model.PurgeErased, ann.ID, model.Now())
	wantErr(t, err, model.ErrNotFound)
	must(t, s.Messages().SetHistoryHidden(ctx, m.ID, 0))
	got, _ = s.Messages().Get(ctx, m.ID)
	eq(t, "history shown again", got.HistoryHiddenAt.IsZero(), true)

	// The server decides when a channel goes private: nobody is named.
	p2 := model.PurgeRequest{MessageID: m.ID, RequestedByID: ann.ID, RequestedAt: model.Now().Add(time.Second)}
	must(t, s.Moderation().CreatePurgeRequest(ctx, &p2))
	done, err = s.Moderation().DecidePurgeRequest(ctx, p2.ID, model.PurgeErased, "", model.Now())
	must(t, err)
	eq(t, "decided by the server", done.DecidedByID == "" && done.Outcome == model.PurgeErased, true)
	all, _, err := s.Moderation().PurgeRequests(ctx, false, store.Page{})
	must(t, err)
	eq(t, "newest first", len(all) == 2 && all[0].ID == p2.ID, true)
	waiting, _, _ := s.Moderation().PurgeRequests(ctx, true, store.Page{})
	eq(t, "none waiting", len(waiting), 0)
	one, err := s.Moderation().GetPurgeRequest(ctx, p.ID)
	must(t, err)
	eq(t, "read back", one.Outcome, model.PurgeShown)
}

func testWordFilters(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	f := model.WordFilter{Pattern: "darn", Action: model.FilterBlock, CreatedByID: ann.ID}
	must(t, s.Moderation().CreateWordFilter(ctx, &f))
	g := model.WordFilter{Pattern: "h+eck", IsRegex: true, Action: model.FilterFlag, CreatedByID: ann.ID, CreatedAt: f.CreatedAt.Add(time.Second)}
	must(t, s.Moderation().CreateWordFilter(ctx, &g))
	all, err := s.Moderation().WordFilters(ctx)
	must(t, err)
	eq(t, "oldest first", len(all) == 2 && all[0].ID == f.ID && all[1].IsRegex, true)
	f.Action = model.FilterFlag
	must(t, s.Moderation().UpdateWordFilter(ctx, f))
	got, err := s.Moderation().GetWordFilter(ctx, f.ID)
	must(t, err)
	eq(t, "updated", got.Action, model.FilterFlag)
	must(t, s.Moderation().DeleteWordFilter(ctx, f.ID))
	wantErr(t, s.Moderation().DeleteWordFilter(ctx, f.ID), model.ErrNotFound)
	wantErr(t, s.Moderation().UpdateWordFilter(ctx, f), model.ErrNotFound)
}

// libraryFile stores a file in the library conversation.
func libraryFile(t testing.TB, s store.Store, by model.AccountID, purpose, name string) model.File {
	t.Helper()
	lib, err := s.Conversations().EnsureLibrary(ctx)
	must(t, err)
	f := model.File{ConversationID: lib.ID, UploaderID: by, Purpose: purpose, Name: name, Size: 3, MediaType: "image/png",
		Kind: model.FileImage, BlobKey: "disk:aa/bb/" + name + string(model.NewID[model.FileID]())}
	must(t, s.Files().Insert(ctx, &f))
	return f
}

func testEmoji(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	e := model.Emoji{Name: "party", FileID: libraryFile(t, s, ann.ID, model.PurposeEmoji, "party.png").ID, CreatedByID: ann.ID}
	must(t, s.Emoji().Create(ctx, &e))
	eq(t, "url", e.URL, "/api/v1/files/"+string(e.FileID)+"/party.png")
	dup := model.Emoji{Name: "party", FileID: libraryFile(t, s, ann.ID, model.PurposeEmoji, "p2.png").ID, CreatedByID: ann.ID}
	wantErr(t, s.Emoji().Create(ctx, &dup), model.ErrNameTaken)
	wave := model.Emoji{Name: "wave", FileID: dup.FileID, CreatedByID: ann.ID}
	must(t, s.Emoji().Create(ctx, &wave))
	got, err := s.Emoji().ByName(ctx, "party")
	must(t, err)
	eq(t, "by name", got.ID, e.ID)
	_, err = s.Emoji().ByName(ctx, "nope")
	wantErr(t, err, model.ErrNotFound)
	list, next, err := s.Emoji().List(ctx, store.Page{Limit: 1})
	must(t, err)
	eq(t, "by name, paged", len(list) == 1 && list[0].Name == "party" && next != "", true)
	list, next, _ = s.Emoji().List(ctx, store.Page{Limit: 1, Cursor: next})
	eq(t, "second page", len(list) == 1 && list[0].Name == "wave" && next == "", true)
	_, err = s.Emoji().Rename(ctx, wave.ID, "party")
	wantErr(t, err, model.ErrNameTaken)
	renamed, err := s.Emoji().Rename(ctx, wave.ID, "hello")
	must(t, err)
	eq(t, "renamed", renamed.Name, "hello")
	gone, err := s.Emoji().Delete(ctx, e.ID)
	must(t, err)
	eq(t, "the row is handed back for its file", gone.FileID, e.FileID)
	_, err = s.Emoji().Get(ctx, e.ID)
	wantErr(t, err, model.ErrNotFound)
	_, err = s.Emoji().Delete(ctx, e.ID)
	wantErr(t, err, model.ErrNotFound)
}

func testPreviews(t *testing.T, s store.Store) {
	hash := []byte("0123456789abcdef0123456789abcdef")
	_, found, err := s.Previews().Get(ctx, hash)
	must(t, err)
	eq(t, "nothing cached", found, false)
	now := model.Now()
	_, replaced, err := s.Previews().Put(ctx, store.PreviewEntry{Hash: hash, URL: "https://example.org/", FetchedAt: now})
	must(t, err)
	eq(t, "first", replaced, false)
	e, found, _ := s.Previews().Get(ctx, hash)
	eq(t, "a failure is cached with no card", found && e.Card == nil && e.URL == "https://example.org/", true)
	old, replaced, err := s.Previews().Put(ctx, store.PreviewEntry{Hash: hash, URL: "https://example.org/",
		Card: &model.Card{Title: "Example", PreviewURL: "https://example.org/"}, FetchedAt: now.Add(time.Second)})
	must(t, err)
	eq(t, "the replaced entry comes back", replaced && old.Card == nil, true)
	e, _, _ = s.Previews().Get(ctx, hash)
	eq(t, "card kept", e.Card != nil && e.Card.Title == "Example", true)

	empty := []byte("ffffffffffffffffffffffffffffffff")
	_, _, err = s.Previews().Put(ctx, store.PreviewEntry{Hash: empty, URL: "https://nothing.example/", FetchedAt: now})
	must(t, err)
	// A failure goes after an hour, a card after a day.
	stale, err := s.Previews().Stale(ctx, now.Add(-23*time.Hour), now.Add(time.Minute), 10)
	must(t, err)
	eq(t, "only the failure is stale", len(stale) == 1 && string(stale[0].Hash) == string(empty), true)
	stale, _ = s.Previews().Stale(ctx, now.Add(time.Hour), now.Add(time.Hour), 10)
	eq(t, "both", len(stale), 2)
	must(t, s.Previews().Delete(ctx, empty))
	_, found, _ = s.Previews().Get(ctx, empty)
	eq(t, "deleted", found, false)
}

func testAdminChannels(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	newChannel(t, s, "alpha", false, ann.ID)
	priv := newChannel(t, s, "beta", true, bob.ID)
	arch := newChannel(t, s, "gamma", false, ann.ID)
	_, err := s.Conversations().Update(ctx, arch.ID, func(c *model.Conversation) error { c.ArchivedAt = model.Now(); return nil })
	must(t, err)
	newAdhoc(t, s, ann.ID, bob.ID)
	_, err = s.Conversations().EnsureLibrary(ctx)
	must(t, err)

	list, _, err := s.Conversations().AdminList(ctx, store.AdminChannelFilter{}, store.Page{})
	must(t, err)
	eq(t, "named channels only, by name", len(list) == 2 && list[0].Name == "alpha" && list[1].ID == priv.ID, true)
	list, _, _ = s.Conversations().AdminList(ctx, store.AdminChannelFilter{IncludeArchived: true}, store.Page{})
	eq(t, "with archived", len(list), 3)
	yes, no := true, false
	list, _, _ = s.Conversations().AdminList(ctx, store.AdminChannelFilter{Private: &yes}, store.Page{})
	eq(t, "only private", len(list) == 1 && list[0].Name == "beta", true)
	list, _, _ = s.Conversations().AdminList(ctx, store.AdminChannelFilter{Private: &no}, store.Page{})
	eq(t, "only public", len(list) == 1 && list[0].Name == "alpha", true)
	list, _, _ = s.Conversations().AdminList(ctx, store.AdminChannelFilter{Query: "ET"}, store.Page{})
	eq(t, "by name", len(list) == 1 && list[0].Name == "beta", true)
	page, next, _ := s.Conversations().AdminList(ctx, store.AdminChannelFilter{}, store.Page{Limit: 1})
	eq(t, "paged", len(page) == 1 && next != "", true)
	page, next, _ = s.Conversations().AdminList(ctx, store.AdminChannelFilter{}, store.Page{Limit: 1, Cursor: next})
	eq(t, "second page", len(page) == 1 && page[0].Name == "beta" && next == "", true)
}

func testThreadMove(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	cat := newAccount(t, s, "cat", model.RoleMember)
	dan := newAccount(t, s, "dan", model.RoleMember)
	c := newChannel(t, s, "old", true, ann.ID)
	for _, a := range []model.Account{bob, cat, dan} {
		join(t, s, c.ID, a.ID, model.LevelMember)
	}
	before := post(t, s, c.ID, ann.ID, "", "before")
	p := post(t, s, c.ID, bob.ID, "", "the thread searchword")
	r1 := post(t, s, c.ID, cat.ID, p.ID, "first reply movedword", model.Mention{AccountID: bob.ID, Kind: model.MentionAccount},
		model.Mention{AccountID: dan.ID, Kind: model.MentionAccount})
	r2 := post(t, s, c.ID, bob.ID, p.ID, "second reply")
	_, err := s.Reactions().Add(ctx, p.ID, cat.ID, "👍", model.Now())
	must(t, err)
	_, err = s.Reactions().Add(ctx, r1.ID, ann.ID, "🎉", model.Now())
	must(t, err)
	_, err = s.Pins().Add(ctx, c.ID, r2.ID, ann.ID, model.Now())
	must(t, err)
	// Bob follows and has read the first reply; cat follows and has read
	// nothing; dan does not follow.
	_, err = s.Threads().Set(ctx, p.ID, bob.ID, model.ThreadFollowing, r1.Seq, model.Now())
	must(t, err)
	_, err = s.Threads().Set(ctx, p.ID, cat.ID, model.ThreadFollowing, 0, model.Now())
	must(t, err)

	move := func(parent model.MessageID, name string) (store.ThreadMoved, error) {
		var out store.ThreadMoved
		err := s.Tx(ctx, func(tx store.Tx) error {
			seq, err := tx.Conversations().NextSeq(ctx, c.ID)
			if err != nil {
				return err
			}
			out, err = tx.Messages().MoveThread(ctx, store.ThreadMove{ParentID: parent, By: ann.ID, PointerSeq: seq,
				Channel: model.Conversation{Name: name, Private: true, CreatedByID: ann.ID}, CopyID: model.NewID[model.MessageID](),
				CopyFiles: []model.File{{UploaderID: bob.ID, Name: "copy.txt", Size: 1, MediaType: "text/plain", Kind: model.FileText, BlobKey: "disk:cc/dd/" + name}},
				At:        model.Now()})
			return err
		})
		return out, err
	}
	_, err = move(r1.ID, "x1")
	wantErr(t, err, model.ErrInvalidRequest)
	_, err = move(before.ID, "x2")
	wantErr(t, err, model.ErrInvalidRequest)
	indexed, _ := s.Search().Indexed(ctx)

	// Under a name already in use: names are not unique (ruling R50).
	out, err := move(p.ID, "old")
	must(t, err)
	d := out.Channel
	eq(t, "the new channel", d.Name == "old" && d.Private && d.MovedFromMessageID == p.ID && d.LastSeq == 3, true)
	eq(t, "the copy starts it", out.Copy.Seq == 1 && out.Copy.ConversationID == d.ID && out.Copy.AuthorID == bob.ID && out.Copy.Text == p.Text && out.Copy.ID != p.ID, true)
	msgs, _, err := s.Messages().List(ctx, d.ID, store.MessageList{})
	must(t, err)
	eq(t, "copy, then the replies in order", len(msgs) == 3 && msgs[1].ID == r1.ID && msgs[2].ID == r2.ID && msgs[1].Seq == 2 && msgs[2].Seq == 3, true)
	eq(t, "replies are top-level and keep author and time", msgs[1].ParentID == "" && msgs[1].AuthorID == cat.ID && msgs[1].CreatedAt == r1.CreatedAt, true)
	must(t, s.Messages().Hydrate(ctx, msgs, ""))
	eq(t, "the copy has the parent's reactions and its own file", len(msgs[0].Reactions) == 1 && len(msgs[0].Files) == 1, true)
	eq(t, "a reply keeps its reactions", len(msgs[1].Reactions), 1)
	eq(t, "a moved reply is no longer pinned in the old channel", msgs[2].Pinned, false)

	// The old channel: the parent stays, with the pointer as its one reply.
	parent, err := s.Messages().Get(ctx, p.ID)
	must(t, err)
	eq(t, "the parent stays", parent.ConversationID == c.ID && parent.Text == p.Text && parent.ReplyCount == 1 && parent.LastReplySeq == out.Pointer.Seq, true)
	eq(t, "the pointer", out.Pointer.ParentID == p.ID && out.Pointer.System["kind"] == "thread_moved" && out.Pointer.System["conversation_id"] == string(d.ID), true)
	closed, err := s.Messages().ThreadMovedOut(ctx, p.ID)
	must(t, err)
	eq(t, "the thread is closed", closed, true)
	open, _ := s.Messages().ThreadMovedOut(ctx, before.ID)
	eq(t, "another is not", open, false)
	_, err = move(p.ID, "again")
	wantErr(t, err, model.ErrConflict)

	// Members: the mover as manager, followers at member, read positions
	// carried over. Dan did not follow and is not in it.
	eq(t, "mover is manager", level(t, s, d.ID, ann.ID), model.LevelManager)
	eq(t, "followers are members", level(t, s, d.ID, bob.ID) == model.LevelMember && level(t, s, d.ID, cat.ID) == model.LevelMember, true)
	eq(t, "others are not", level(t, s, d.ID, dan.ID), model.Level(""))
	bm, _, _ := s.Members().Get(ctx, d.ID, bob.ID)
	cm, _, _ := s.Members().Get(ctx, d.ID, cat.ID)
	eq(t, "bob had read the first reply", bm.ReadSeq == 2 && bm.Mode == model.ModeMentions, true)
	eq(t, "cat had read none", cm.ReadSeq, int64(1))
	eq(t, "joined events for each", len(out.Members.Changes), 3)
	_, found, _ := s.Threads().Get(ctx, p.ID, bob.ID)
	eq(t, "thread states are cleared", found, false)

	// Mentions follow the replies, for those who came along.
	mentions, _ := s.Messages().Mentions(ctx, r1.ID)
	eq(t, "mention kept for a member, dropped for dan", len(mentions) == 1 && mentions[0].AccountID == bob.ID, true)
	u, err := s.Members().Unread(ctx, d.ID, cat.ID)
	must(t, err)
	eq(t, "unread counts in the new channel", u.Top, int64(1)) // r2 by bob; r1 is cat's own

	// The old channel's log no longer carries the replies.
	events, _, err := s.Events().List(ctx, c.ID, 0, 0, 100)
	must(t, err)
	for _, e := range events {
		if e.MessageID == r1.ID || e.MessageID == r2.ID {
			t.Errorf("an event of a moved reply is still in the old channel's log")
		}
	}
	// Search: the same messages are indexed, plus the copy.
	after, _ := s.Search().Indexed(ctx)
	eq(t, "index holds the copy too", after, indexed+1)
	hits, _, err := s.Search().Messages(ctx, store.SearchQuery{Terms: []store.SearchTerm{{Text: "movedword"}},
		Scope: store.Scope{AccountID: bob.ID}, In: d.ID}, store.Page{})
	must(t, err)
	eq(t, "a moved reply is found in the new channel", len(hits) == 1 && hits[0].ID == r1.ID, true)
	hits, _, _ = s.Search().Messages(ctx, store.SearchQuery{Terms: []store.SearchTerm{{Text: "movedword"}},
		Scope: store.Scope{AccountID: dan.ID}}, store.Page{})
	eq(t, "and not by someone only in the old one", len(hits), 0)
}

func testCardFiles(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	c := newChannel(t, s, "cards", false, ann.ID)
	m := post(t, s, c.ID, ann.ID, "", "https://example.org")
	img := model.File{ConversationID: c.ID, MessageID: m.ID, UploaderID: ann.ID, Purpose: model.PurposeCard, Name: "preview.png",
		Size: 9, MediaType: "image/png", Kind: model.FileImage, BlobKey: "disk:aa/bb/card"}
	must(t, s.Files().Insert(ctx, &img))
	got, err := s.Messages().SetCards(ctx, m.ID, []model.Card{{Title: "Example", PreviewURL: "https://example.org", ImageFileID: img.ID}})
	must(t, err)
	eq(t, "cards set without an edit", len(got.Cards) == 1 && got.EditedAt.IsZero() && got.Version == 1, true)
	msgs := []model.Message{got}
	must(t, s.Messages().Hydrate(ctx, msgs, ""))
	eq(t, "a card's image is not one of the message's files", len(msgs[0].Files) == 0 && !msgs[0].HasHistory, true)
	list, _ := s.Files().ForMessage(ctx, m.ID, true)
	eq(t, "nor listed as attached", len(list), 0)
	cards, err := s.Files().CardImages(ctx, m.ID)
	must(t, err)
	eq(t, "it is a card image", len(cards) == 1 && cards[0].ID == img.ID, true)
	gone, _ := s.Files().Detach(ctx, m.ID, nil, model.Now())
	eq(t, "an edit that takes every file off leaves it", len(gone), 0)
	known, err := s.Files().KeyKnown(ctx, "disk:aa/bb/card")
	must(t, err)
	unknown, _ := s.Files().KeyKnown(ctx, "disk:zz/zz/none")
	eq(t, "keys known", known && !unknown, true)
	count, bytes, err := s.Files().Totals(ctx)
	must(t, err)
	eq(t, "totals", count == 1 && bytes == 9, true)
}

func testJanitor(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	now := model.Now()
	old := func(conv model.ConversationID, parent model.MessageID, text string, age time.Duration) model.Message {
		m := model.Message{ID: model.NewID[model.MessageID](), ConversationID: conv, AuthorID: ann.ID, ParentID: parent, Text: text,
			CreatedAt: now.Add(-age)}
		must(t, s.Tx(ctx, func(tx store.Tx) error {
			seq, err := tx.Conversations().NextSeq(ctx, conv)
			if err != nil {
				return err
			}
			m.Seq = seq
			if err := tx.Messages().Insert(ctx, &m, nil); err != nil {
				return err
			}
			return tx.Events().Append(ctx, model.Event{Type: model.EventMessageCreated, At: m.CreatedAt, ConversationID: conv, Seq: seq,
				MessageID: m.ID, Payload: map[string]any{"message": m}})
		}))
		return m
	}
	const day = 24 * time.Hour
	follows := newChannel(t, s, "follows", false, ann.ID) // the server's period
	short := newChannel(t, s, "short", false, ann.ID)     // one day of its own
	forever := newChannel(t, s, "forever", false, ann.ID) // 0: keep for ever
	set := func(c model.ConversationID, seconds int64) {
		_, err := s.Conversations().Update(ctx, c, func(c *model.Conversation) error { c.RetentionSeconds = &seconds; return nil })
		must(t, err)
	}
	set(short.ID, 86400)
	set(forever.ID, 0)
	adhoc := newAdhoc(t, s, ann.ID, bob.ID)

	f1 := old(follows.ID, "", "ten days old", 10*day)
	f2 := old(follows.ID, "", "one day old", day+time.Hour)
	s1 := old(short.ID, "", "two days in the short channel", 2*day)
	s2 := old(short.ID, "", "an hour in the short channel", time.Hour)
	k1 := old(forever.ID, "", "a year in the keep-for-ever channel", 365*day)
	a1 := old(adhoc.ID, "", "ten days in a chat", 10*day)
	parent := old(follows.ID, "", "an old parent", 9*day)
	reply := old(follows.ID, parent.ID, "a young reply", time.Hour)

	ids := func(server int64) map[model.MessageID]bool {
		list, err := s.Janitor().ExpiredMessages(ctx, server, now, 100)
		must(t, err)
		out := map[model.MessageID]bool{}
		for _, id := range list {
			out[id] = true
		}
		return out
	}
	none := ids(0)
	eq(t, "with no server period only the channel's own applies", len(none) == 1 && none[s1.ID], true)
	week := ids(7 * 86400)
	eq(t, "a week", len(week) == 4 && week[f1.ID] && week[s1.ID] && week[a1.ID] && week[parent.ID], true)
	eq(t, "younger, kept for ever, or under a longer period", week[f2.ID] || week[s2.ID] || week[k1.ID] || week[reply.ID], false)

	file := model.File{ConversationID: follows.ID, MessageID: f1.ID, UploaderID: ann.ID, Name: "a.txt", Size: 1, MediaType: "text/plain",
		Kind: model.FileText, BlobKey: "disk:aa/bb/expired"}
	must(t, s.Files().Insert(ctx, &file))
	indexed, _ := s.Search().Indexed(ctx)
	keys, deleted, err := s.Janitor().ExpireMessage(ctx, f1.ID, now)
	must(t, err)
	eq(t, "deleted with its blob handed back", deleted && len(keys) == 1 && keys[0] == "disk:aa/bb/expired", true)
	_, err = s.Messages().Get(ctx, f1.ID)
	wantErr(t, err, model.ErrNotFound)
	after, _ := s.Search().Indexed(ctx)
	eq(t, "search entry gone", after, indexed-1)
	events, _, _ := s.Events().List(ctx, follows.ID, 0, 0, 100)
	for _, e := range events {
		if e.MessageID == f1.ID {
			t.Errorf("an event of an expired message is still in the log")
		}
	}

	// A parent with a younger reply is emptied and kept, and not offered
	// again until the reply has gone.
	_, deleted, err = s.Janitor().ExpireMessage(ctx, parent.ID, now)
	must(t, err)
	bare, err := s.Messages().Get(ctx, parent.ID)
	must(t, err)
	eq(t, "bare record", !deleted && bare.Text == "" && bare.Deleted() && bare.DeletedByID == "" && !bare.PurgedAt.IsZero(), true)
	eq(t, "not offered again", ids(7 * 86400)[parent.ID], false)
	_, deleted, err = s.Janitor().ExpireMessage(ctx, reply.ID, now)
	must(t, err)
	eq(t, "the reply goes", deleted, true)
	eq(t, "then the parent is offered", ids(7 * 86400)[parent.ID], true)
	_, deleted, _ = s.Janitor().ExpireMessage(ctx, parent.ID, now)
	eq(t, "and goes", deleted, true)

	// The rest of the janitor's table.
	sess := model.Session{AccountID: ann.ID, Kind: model.SessionWeb, ExpiresAt: now.Add(-31 * day)}
	must(t, s.Sessions().Create(ctx, &sess, []byte("old-session-hash"), nil))
	live := model.Session{AccountID: ann.ID, Kind: model.SessionWeb, ExpiresAt: now.Add(day)}
	must(t, s.Sessions().Create(ctx, &live, []byte("live-session-hash"), nil))
	ended := model.Timeout{AccountID: bob.ID, Until: now.Add(-31 * day), CreatedByID: ann.ID, CreatedAt: now.Add(-40 * day)}
	must(t, s.Moderation().CreateTimeout(ctx, &ended))
	recent := model.Timeout{AccountID: bob.ID, Until: now.Add(-day), CreatedByID: ann.ID, CreatedAt: now.Add(-2 * day)}
	must(t, s.Moderation().CreateTimeout(ctx, &recent))
	withID := model.Message{ID: model.NewID[model.MessageID](), ConversationID: follows.ID, AuthorID: ann.ID, Text: "client id",
		ClientID: model.NewID[string](), CreatedAt: now.Add(-25 * time.Hour)}
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		seq, err := tx.Conversations().NextSeq(ctx, follows.ID)
		if err != nil {
			return err
		}
		withID.Seq = seq
		return tx.Messages().Insert(ctx, &withID, nil)
	}))
	counts, err := s.Janitor().Tidy(ctx, now)
	must(t, err)
	eq(t, "one session, one time-out, one client id", counts["sessions"] == 1 && counts["timeouts"] == 1 && counts["messages.client_id"] == 1, true)
	_, err = s.Moderation().GetTimeout(ctx, recent.ID)
	must(t, err)
	_, found, _ := s.Messages().ByClientID(ctx, follows.ID, ann.ID, withID.ClientID)
	eq(t, "the client id is forgotten", found, false)
	counts, err = s.Janitor().Tidy(ctx, now)
	must(t, err)
	eq(t, "a second run finds nothing", len(counts), 0)
}

func init() { register("audit_filters", testAuditFilters) }

func testAuditFilters(t *testing.T, s store.Store) {
	for i, action := range []string{"account.removed", "account_x.other", "channel.locked", "account.banned"} {
		must(t, s.Audit().Append(ctx, model.AuditEntry{At: model.Time(1000 + i), ActorKind: "account", Action: action}))
	}
	list := func(f store.AuditFilter) int {
		out, _, err := s.Audit().List(ctx, f, store.Page{})
		must(t, err)
		return len(out)
	}
	eq(t, "by prefix; the underscore is not a wildcard", list(store.AuditFilter{ActionPrefix: "account."}), 2)
	eq(t, "since is inclusive", list(store.AuditFilter{Since: 1002}), 2)
	eq(t, "until is exclusive", list(store.AuditFilter{Until: 1002}), 2)
	eq(t, "together", list(store.AuditFilter{ActionPrefix: "account.", Since: 1001, Until: 1004}), 1)
}

func init() {
	register("subchannels", testSubChannels)
	register("search_text", testSearchText)
}

func testSubChannels(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	parent := newChannel(t, s, "Board Games", true, ann.ID)
	other := newChannel(t, s, "other", false, ann.ID)
	child := func(name string, private bool, by model.AccountID) model.Conversation {
		c := model.Conversation{Kind: model.KindChannel, Name: name, Private: private, CreatedByID: by, ParentID: parent.ID}
		must(t, s.Conversations().Create(ctx, &c))
		_, err := s.Members().AddDirect(ctx, c.ID, by, model.LevelManager, model.Now())
		must(t, err)
		return c
	}
	// Any number of channels may have the same name, under the same
	// parent too (ruling R50).
	a := child("ops", false, ann.ID)
	b := child("ops", false, ann.ID)
	hidden := child("ops", true, ann.ID)
	got, err := s.Conversations().Get(ctx, a.ID)
	must(t, err)
	eq(t, "the parent is stored", got.ParentID, parent.ID)
	same, err := s.Conversations().ByNameAll(ctx, "ops")
	must(t, err)
	eq(t, "all three by name", len(same), 3)
	none, _ := s.Conversations().ByNameAll(ctx, "Ops")
	eq(t, "a name is matched exactly here", len(none), 0)
	n, err := s.Conversations().SubChannelCount(ctx, parent.ID)
	must(t, err)
	eq(t, "counted", n, int64(3))
	n, _ = s.Conversations().SubChannelCount(ctx, a.ID)
	eq(t, "a sub-channel has none", n, int64(0))

	// The list of a channel's sub-channels holds only what the caller can
	// see: bob is in none of them and sees the two public ones.
	list, _, err := s.Conversations().Browse(ctx, store.BrowseFilter{AccountID: bob.ID, Public: true, ParentID: parent.ID}, store.Page{})
	must(t, err)
	eq(t, "bob sees the public sub-channels", len(list) == 2 && list[0].ID != hidden.ID && list[1].ID != hidden.ID, true)
	list, _, _ = s.Conversations().Browse(ctx, store.BrowseFilter{AccountID: ann.ID, Public: true, ParentID: parent.ID}, store.Page{})
	eq(t, "ann, a member of all, sees three", len(list), 3)
	list, _, _ = s.Conversations().Browse(ctx, store.BrowseFilter{AccountID: ann.ID, Public: true, ParentID: other.ID}, store.Page{})
	eq(t, "a channel with none", len(list), 0)
	// Paging does not lose or repeat channels that share a name.
	seen := map[model.ConversationID]bool{}
	p := store.Page{Limit: 1}
	for i := 0; i < 5; i++ {
		page, next, err := s.Conversations().Browse(ctx, store.BrowseFilter{AccountID: ann.ID, Public: true, ParentID: parent.ID}, p)
		must(t, err)
		for _, c := range page {
			if seen[c.ID] {
				t.Errorf("a channel came twice while paging")
			}
			seen[c.ID] = true
		}
		if next == "" {
			break
		}
		p.Cursor = next
	}
	eq(t, "every channel of the same name came once", len(seen), 3)
	all, _, _ := s.Conversations().Browse(ctx, store.BrowseFilter{AccountID: ann.ID, Public: true, Query: "board games"}, store.Page{})
	eq(t, "browsing matches without regard to case", len(all) == 1 && all[0].ID == parent.ID, true)

	// Setting and clearing through Update; renaming to a name in use.
	moved, err := s.Conversations().Update(ctx, b.ID, func(c *model.Conversation) error { c.ParentID, c.Name = "", "other"; return nil })
	must(t, err)
	eq(t, "cleared and renamed to a name another channel has", moved.ParentID == "" && moved.Name == "other", true)
	moved, err = s.Conversations().Update(ctx, b.ID, func(c *model.Conversation) error { c.ParentID = other.ID; return nil })
	must(t, err)
	eq(t, "set", moved.ParentID, other.ID)

	// When a parent is deleted its sub-channels become top-level, with
	// their members and messages untouched (case 231).
	post(t, s, a.ID, ann.ID, "", "still here")
	res, err := s.Members().RemoveDirect(ctx, parent.ID, ann.ID)
	must(t, err)
	eq(t, "the private parent went with its last member", len(res.Deleted) == 1 && res.Deleted[0].ID == parent.ID, true)
	for _, c := range []model.Conversation{a, hidden} {
		after, err := s.Conversations().Get(ctx, c.ID)
		must(t, err)
		eq(t, "top-level now", after.ParentID, model.ConversationID(""))
		eq(t, "members kept", after.MemberCount, int64(1))
	}
	msgs, _, _ := s.Messages().List(ctx, a.ID, store.MessageList{})
	eq(t, "messages kept", len(msgs), 1)
}

func testSearchText(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	c := newChannel(t, s, "general", false, ann.ID)
	linked := newChannel(t, s, "Zeppelin Hangar", false, ann.ID)
	text := "meet in <#" + string(linked.ID) + "> at noon"
	insert := func(text string, entities []model.Entity) model.Message {
		m := model.Message{ID: model.NewID[model.MessageID](), ConversationID: c.ID, AuthorID: ann.ID, Text: text, Entities: entities,
			Fallback: "Poll: lunchplace", CreatedAt: model.Now()}
		must(t, s.Tx(ctx, func(tx store.Tx) error {
			seq, err := tx.Conversations().NextSeq(ctx, c.ID)
			if err != nil {
				return err
			}
			m.Seq = seq
			return tx.Messages().Insert(ctx, &m, nil)
		}))
		return m
	}
	m := insert(text, []model.Entity{{Type: model.EntityConversation, At: 8, Len: 39, ConversationID: linked.ID}})
	got, err := s.Messages().Get(ctx, m.ID)
	must(t, err)
	eq(t, "the name stands in place of the link, then the fallback", got.SearchText, "meet in Zeppelin Hangar at noon\nPoll: lunchplace")
	find := func(word string) int {
		hits, _, err := s.Search().Messages(ctx, store.SearchQuery{Terms: []store.SearchTerm{{Text: word}}, Scope: store.Scope{AccountID: ann.ID}}, store.Page{})
		must(t, err)
		return len(hits)
	}
	eq(t, "found by the linked channel's name", find("zeppelin"), 1)
	eq(t, "and by the fallback", find("lunchplace"), 1)
	eq(t, "not by the identifier", find(string(linked.ID)), 0)
	eq(t, "nor by a piece of it", find(string(linked.ID)[24:]), 0)

	// A link with no entity is one the author could not see: its name is
	// not written.
	unseen := insert(text, nil)
	got, _ = s.Messages().Get(ctx, unseen.ID)
	eq(t, "unknown channel", got.SearchText, "meet in unknown channel at noon\nPoll: lunchplace")
	eq(t, "still one hit for the name", find("zeppelin"), 1)

	// An edit replaces it; a delete clears it.
	edited, err := s.Messages().Edit(ctx, store.MessageEdit{ID: m.ID, Text: "moved to the Lighthouse", By: ann.ID, At: model.Now()})
	must(t, err)
	_ = edited
	got, _ = s.Messages().Get(ctx, m.ID)
	eq(t, "replaced by an edit", got.SearchText, "moved to the Lighthouse")
	eq(t, "the old name is no longer found", find("zeppelin"), 0)
	eq(t, "the new text is", find("lighthouse"), 1)
	_, err = s.Messages().Delete(ctx, m.ID, ann.ID, model.Now())
	must(t, err)
	got, _ = s.Messages().Get(ctx, m.ID)
	eq(t, "cleared by a delete", got.SearchText, "")
	eq(t, "and not found", find("lighthouse"), 0)
}

func init() { register("loose_card_files", testLooseCardFiles) }

// A webhook's card images are files of purpose `card` on no message row.
// They go when the message that names them is purged, expired or deleted
// outright, and not before.
func testLooseCardFiles(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	c := newChannel(t, s, "hooks", false, ann.ID)
	loose := func(name string) model.File {
		f := model.File{ConversationID: c.ID, UploaderID: ann.ID, Purpose: model.PurposeCard, Name: name, Size: 1, MediaType: "image/png",
			Kind: model.FileImage, BlobKey: "disk:aa/bb/" + name}
		must(t, s.Files().Create(ctx, &f))
		return f
	}
	card := func(text string) (model.Message, model.File, model.File) {
		img, icon := loose(text+"-image"), loose(text+"-icon")
		m := post(t, s, c.ID, ann.ID, "", text)
		_, err := s.Messages().SetCards(ctx, m.ID, []model.Card{{Title: text, ImageFileID: img.ID, Author: &model.CardAuthor{Name: "CI", IconFileID: icon.ID}}})
		must(t, err)
		return m, img, icon
	}
	gone := func(f model.File) bool {
		_, err := s.Files().Get(ctx, f.ID)
		return model.CodeOf(err) == model.CodeNotFound
	}
	// Delete keeps them, as history; the purge that follows removes them.
	m, img, icon := card("purged")
	other, otherImg, _ := card("stays")
	_, err := s.Messages().Delete(ctx, m.ID, ann.ID, model.Now())
	must(t, err)
	eq(t, "kept while the deleted message has history", gone(img), false)
	keys, err := s.Messages().Purge(ctx, m.ID, model.Now())
	must(t, err)
	eq(t, "gone with the purge, blobs handed back", gone(img) && gone(icon) && len(keys) == 2, true)
	eq(t, "another message's are untouched", gone(otherImg), false)
	// Retention.
	keys, _, err = s.Janitor().ExpireMessage(ctx, other.ID, model.Now())
	must(t, err)
	eq(t, "gone with retention", gone(otherImg) && len(keys) == 2, true)
	// A delete inside the quick-correction window.
	q, qImg, _ := card("outright")
	keys, err = s.Messages().DeleteOutright(ctx, q.ID)
	must(t, err)
	eq(t, "gone with an outright delete", gone(qImg) && len(keys) == 2, true)
	// The janitor's sweep of unattached uploads never takes one.
	_, live, _ := card("live")
	stale, err := s.Files().Unattached(ctx, model.Now().Add(time.Hour), 100)
	must(t, err)
	for _, f := range stale {
		if f.ID == live.ID {
			t.Errorf("a card image was offered as an orphaned upload")
		}
	}
}
