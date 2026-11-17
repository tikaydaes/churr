package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() {
	register("peers", testPeers)
	register("streams", testStreams)
}

// streamsServer pins a server with made-up keys.
func streamsServer(t *testing.T, s store.Areas, name string) store.Server {
	t.Helper()
	sv, created, err := s.Peers().PinServer(ctx, store.Server{Name: name, SignKey: []byte("sign-" + name), BoxKey: []byte("box-" + name)})
	must(t, err)
	eq(t, "a new name is pinned", created, true)
	return sv
}

func testPeers(t *testing.T, s store.Store) {
	a := streamsServer(t, s, "a.example")
	again, created, err := s.Peers().PinServer(ctx, store.Server{Name: "a.example", SignKey: []byte("other"), BoxKey: []byte("other")})
	must(t, err)
	eq(t, "a pinned name keeps its keys", !created && string(again.SignKey) == "sign-a.example" && again.ID == a.ID, true)
	hub, _, err := s.Peers().PinServer(ctx, store.Server{Name: "hub.example", SignKey: []byte("k")})
	must(t, err)
	eq(t, "a hub has no box key", len(hub.BoxKey), 0)
	got, err := s.Peers().ServerByName(ctx, "a.example")
	must(t, err)
	eq(t, "by name", got.ID, a.ID)
	_, err = s.Peers().ServerByName(ctx, "nobody.example")
	wantErr(t, err, model.ErrNotFound)
	_, err = s.Peers().Server(ctx, model.NewID[model.ServerID]())
	wantErr(t, err, model.ErrNotFound)

	at := model.Now()
	must(t, s.Peers().SetServerBlocked(ctx, a.ID, at))
	got, _ = s.Peers().Server(ctx, a.ID)
	eq(t, "blocked", got.Blocked() && got.BlockedAt == at, true)
	must(t, s.Peers().SetServerBlocked(ctx, a.ID, 0))
	got, _ = s.Peers().Server(ctx, a.ID)
	eq(t, "unblocked", got.Blocked(), false)
	wantErr(t, s.Peers().SetServerBlocked(ctx, model.NewID[model.ServerID](), at), model.ErrNotFound)
	all, err := s.Peers().Servers(ctx)
	must(t, err)
	eq(t, "servers by name", len(all) == 2 && all[0].Name == "a.example", true)

	owner := newAccount(t, s, "owner", model.RoleOwner)
	p := store.Peer{Kind: "server", Dials: false, State: store.PeerInvited, InviteHash: []byte("hash"),
		InviteExpiresAt: model.Now().Add(time.Hour), CreatedBy: owner.ID}
	must(t, s.Peers().Create(ctx, &p))
	back, err := s.Peers().Get(ctx, p.ID)
	must(t, err)
	eq(t, "an invitation waits", back.State == store.PeerInvited && string(back.InviteHash) == "hash" && back.ServerID == "" && back.URL == "", true)
	up, err := s.Peers().Update(ctx, p.ID, func(p *store.Peer) error {
		p.State, p.ServerID, p.InviteHash, p.InviteExpiresAt, p.ConnectedAt, p.LastError = store.PeerActive, a.ID, nil, 0, at, "x"
		return nil
	})
	must(t, err)
	eq(t, "accepted", up.State == store.PeerActive && up.ServerID == a.ID, true)
	back, _ = s.Peers().Get(ctx, p.ID)
	eq(t, "stored", back.ServerID == a.ID && back.InviteHash == nil && back.InviteExpiresAt.IsZero() && back.ConnectedAt == at && back.LastError == "x", true)
	_, err = s.Peers().Update(ctx, model.NewID[model.PeerID](), func(*store.Peer) error { return nil })
	wantErr(t, err, model.ErrNotFound)

	h := store.Peer{Kind: "hub", HubName: "hub.example", HubSignKey: []byte("k"), URL: "wss://hub.example/link/v1", Dials: true,
		State: store.PeerActive, CreatedBy: owner.ID}
	must(t, s.Peers().Create(ctx, &h))
	b := streamsServer(t, s, "b.example")
	must(t, s.Peers().SetReachable(ctx, h.ID, []model.ServerID{a.ID, b.ID}))
	must(t, s.Peers().SetReachable(ctx, h.ID, []model.ServerID{b.ID}))
	reach, err := s.Peers().Reachable(ctx, h.ID)
	must(t, err)
	eq(t, "the roster replaces what was there", len(reach) == 1 && reach[0].Name == "b.example", true)
	list, err := s.Peers().List(ctx)
	must(t, err)
	eq(t, "both peers", len(list), 2)

	// The clock figures are kept with the peer and survive an Update that
	// knows nothing of them.
	eq(t, "no clock until one is told or measured", list[0].ClockTold == nil && list[0].ClockMeasured == nil, true)
	told := model.PeerClock{Offset: -180000, Latency: 40, Jitter: 3, Uncertainty: 21, Forward: 20, ForwardJitter: 1, Back: 20, BackJitter: 2,
		Samples: 16, MeasuredAt: at}
	measured := model.PeerClock{Offset: 7, Latency: 12, Uncertainty: 7, Asymmetry: 30, Synced: true, Samples: 4, MeasuredAt: at}
	must(t, s.Peers().SetClockTold(ctx, h.ID, told))
	must(t, s.Peers().SetClockMeasured(ctx, h.ID, measured))
	_, err = s.Peers().Update(ctx, h.ID, func(p *store.Peer) error {
		p.LastError, p.ClockTold = "y", nil
		return nil
	})
	must(t, err)
	back, err = s.Peers().Get(ctx, h.ID)
	must(t, err)
	eq(t, "the clock told is kept", back.ClockTold != nil && *back.ClockTold == told, true)
	eq(t, "the clock measured is kept", back.ClockMeasured != nil && *back.ClockMeasured == measured, true)
	for i := int64(1); i <= 5; i++ {
		must(t, s.Peers().AddAway(ctx, h.ID, 100*i, 100*i+50, 3))
	}
	back, _ = s.Peers().Get(ctx, h.ID)
	eq(t, "the newest stretches of disconnection are kept", len(back.Away) == 3 && back.Away[0] == [2]int64{300, 350} && back.Away[2] == [2]int64{500, 550}, true)
	wantErr(t, s.Peers().AddAway(ctx, model.NewID[model.PeerID](), 1, 2, 3), model.ErrNotFound)
	wantErr(t, s.Peers().SetClockTold(ctx, model.NewID[model.PeerID](), told), model.ErrNotFound)
	wantErr(t, s.Peers().SetClockMeasured(ctx, model.NewID[model.PeerID](), told), model.ErrNotFound)

	st := store.Stream{ID: model.NewID[model.StreamID](), PeerID: h.ID, Sequencer: store.SequencerHub, Kind: store.StreamChannel, State: store.StreamOffered}
	must(t, s.Streams().Create(ctx, &st))
	wantErr(t, s.Peers().Delete(ctx, h.ID), model.ErrConflict)
	must(t, s.Peers().Delete(ctx, p.ID))
	_, err = s.Peers().Get(ctx, p.ID)
	wantErr(t, err, model.ErrNotFound)
}

func testStreams(t *testing.T, s store.Store) {
	owner := newAccount(t, s, "owner", model.RoleOwner)
	self := streamsServer(t, s, "a.example")
	other := streamsServer(t, s, "b.example")
	peer := store.Peer{Kind: "server", ServerID: other.ID, State: store.PeerActive, CreatedBy: owner.ID}
	must(t, s.Peers().Create(ctx, &peer))
	ch := newChannel(t, s, "launch", false, owner.ID)

	st := store.Stream{ID: model.NewID[model.StreamID](), PeerID: peer.ID, ConversationID: ch.ID, Sequencer: store.SequencerSelf,
		Kind: store.StreamChannel, State: store.StreamActive, KeyEpoch: 1, ServersVersion: 1, MaxBlobBytes: 1000, HistoryDays: 30}
	must(t, s.Streams().Create(ctx, &st))
	wantErr(t, s.Streams().Create(ctx, &st), model.ErrAlreadyExists)
	got, err := s.Streams().Get(ctx, st.ID)
	must(t, err)
	eq(t, "stored", got.Owned() && got.MaxBlobBytes == 1000 && got.HistoryDays == 30 && got.ConversationID == ch.ID, true)
	by, err := s.Streams().ByConversation(ctx, ch.ID)
	must(t, err)
	eq(t, "by conversation", by.ID, st.ID)
	up, err := s.Streams().Update(ctx, st.ID, func(s *store.Stream) error { s.LastSeq, s.KeyEpoch = 9, 2; return nil })
	must(t, err)
	eq(t, "updated", up.LastSeq == 9 && up.KeyEpoch == 2, true)
	offered, _ := s.Streams().List(ctx, store.StreamOffered)
	active, _ := s.Streams().List(ctx, "")
	eq(t, "list by state", len(offered) == 0 && len(active) == 1, true)

	// The server list, and each origin's counter.
	now := model.Now()
	must(t, s.Streams().PutServer(ctx, store.StreamServer{StreamID: st.ID, ServerID: self.ID, State: store.StreamServerActive, JoinedAt: now}))
	must(t, s.Streams().PutServer(ctx, store.StreamServer{StreamID: st.ID, ServerID: other.ID, State: store.StreamServerInvited, JoinedAt: now}))
	n, err := s.Streams().NextN(ctx, st.ID, self.ID)
	must(t, err)
	n2, _ := s.Streams().NextN(ctx, st.ID, self.ID)
	eq(t, "our own counter grows by one", n == 1 && n2 == 2, true)
	must(t, s.Streams().SetLastN(ctx, st.ID, other.ID, 7))
	must(t, s.Streams().PutServer(ctx, store.StreamServer{StreamID: st.ID, ServerID: other.ID, State: store.StreamServerActive, JoinedAt: now}))
	servers, err := s.Streams().Servers(ctx, st.ID)
	must(t, err)
	eq(t, "the list, by name", len(servers) == 2 && servers[0].Name == "a.example" && servers[1].Name == "b.example", true)
	eq(t, "a change of state keeps the counter", servers[1].State == store.StreamServerActive && servers[1].LastN == 7 && servers[0].LastN == 2, true)

	// Keys.
	must(t, s.Streams().PutKey(ctx, st.ID, store.StreamKey{Epoch: 1, Sealed: []byte("one")}))
	must(t, s.Streams().PutKey(ctx, st.ID, store.StreamKey{Epoch: 2, Sealed: []byte("two")}))
	must(t, s.Streams().PutKey(ctx, st.ID, store.StreamKey{Epoch: 1, Sealed: []byte("uno")}))
	k, err := s.Streams().Key(ctx, st.ID, 1)
	must(t, err)
	eq(t, "a key is replaced in place", string(k.Sealed), "uno")
	_, err = s.Streams().Key(ctx, st.ID, 3)
	wantErr(t, err, model.ErrNotFound)
	keys, _ := s.Streams().Keys(ctx, st.ID)
	eq(t, "every epoch is kept", len(keys) == 2 && keys[1].Epoch == 2, true)

	testStreamMoves(t, s, st, peer, owner.ID, now)

	// The outbox.
	e1, e2 := model.NewID[string](), model.NewID[string]()
	must(t, s.Streams().PutOutbox(ctx, store.OutboxRow{ID: e1, StreamID: st.ID, Envelope: []byte("first"), CreatedAt: now}))
	must(t, s.Streams().PutOutbox(ctx, store.OutboxRow{ID: e2, StreamID: st.ID, Envelope: []byte("second"), CreatedAt: now.Add(time.Second)}))
	must(t, s.Streams().OutboxTried(ctx, e1, 3, now.Add(time.Hour)))
	due, err := s.Streams().OutboxDue(ctx, now.Add(time.Minute), 10)
	must(t, err)
	eq(t, "only what is due", len(due) == 1 && due[0].ID == e2, true)
	must(t, s.Streams().PutOutbox(ctx, store.OutboxRow{ID: e1, StreamID: st.ID, Envelope: []byte("again"), CreatedAt: now.Add(time.Hour)}))
	rows, _ := s.Streams().Outbox(ctx, st.ID)
	eq(t, "a replaced row keeps its place and starts over", len(rows) == 2 && rows[0].ID == e1 && string(rows[0].Envelope) == "again" && rows[0].Attempts == 0, true)
	one, err := s.Streams().GetOutbox(ctx, e2)
	must(t, err)
	eq(t, "one row", string(one.Envelope), "second")
	count, _ := s.Streams().OutboxCount(ctx)
	eq(t, "count", count, int64(2))
	was, _ := s.Streams().DeleteOutbox(ctx, e2)
	wasnt, _ := s.Streams().DeleteOutbox(ctx, e2)
	eq(t, "deleted once", was && !wasnt, true)

	// Duplicates.
	fresh, err := s.Streams().MarkSeen(ctx, st.ID, e1, 5, now.Add(-40*24*time.Hour))
	must(t, err)
	dup, _ := s.Streams().MarkSeen(ctx, st.ID, e1, 6, now)
	eq(t, "an origin event id is seen once", fresh && !dup, true)
	seq, found, _ := s.Streams().Seen(ctx, st.ID, e1)
	eq(t, "under the seq it was first applied at", found && seq == 5, true)
	_, found, _ = s.Streams().Seen(ctx, st.ID, e2)
	eq(t, "an unseen id", found, false)
	pruned, _ := s.Streams().PruneSeen(ctx, now.Add(-35*24*time.Hour))
	eq(t, "old rows are pruned", pruned, int64(1))

	// The log: positions are given in the order rows are written, each row
	// knows the counter of its origin's row before it, and catching up is
	// by origin and counter.
	ids := map[int64]string{}
	for i := int64(1); i <= 5; i++ {
		origin := "a.example"
		if i%2 == 0 {
			origin = "b.example"
		}
		ids[i] = model.NewID[string]()
		row, err := s.Streams().AppendLog(ctx, st.ID, store.LogRow{Origin: origin, N: 10 * i, OriginEventID: ids[i], Envelope: []byte{byte(i)},
			At: now.Add(time.Duration(i-40*24) * time.Hour)})
		must(t, err)
		eq(t, "the next position", row.Seq, 9+i)
		wantPrev := int64(0)
		if i > 2 {
			wantPrev = 10 * (i - 2)
		}
		eq(t, "the origin's counter before it", row.Prev, wantPrev)
	}
	last, err := s.Streams().AppendLog(ctx, st.ID, store.LogRow{Origin: "a.example", N: 60, OriginEventID: model.NewID[string](), Envelope: []byte{6}, At: now})
	must(t, err)
	eq(t, "the sixth row", last.Seq == 15 && last.Prev == 50, true)
	got, _ = s.Streams().Get(ctx, st.ID)
	eq(t, "the stream's cursor follows its log", got.LastSeq, int64(15))
	log, err := s.Streams().Log(ctx, st.ID, 11, 3)
	must(t, err)
	eq(t, "the log in order after a position", len(log) == 3 && log[0].Seq == 12 && log[2].Seq == 14 && log[0].Origin == "a.example" && log[0].N == 30, true)
	since, err := s.Streams().LogSince(ctx, st.ID, "a.example", 10, 10)
	must(t, err)
	eq(t, "one origin's rows above a counter", len(since) == 3 && since[0].N == 30 && since[1].N == 50 && since[2].N == 60 && since[0].Prev == 10, true)
	none, _ := s.Streams().LogSince(ctx, st.ID, "b.example", 40, 10)
	eq(t, "nothing above the newest", len(none), 0)
	has, _ := s.Streams().LogHas(ctx, st.ID, ids[3])
	hasnt, _ := s.Streams().LogHas(ctx, st.ID, model.NewID[string]())
	eq(t, "an envelope is in the log once", has && !hasnt, true)
	floor, err := s.Streams().LogFloor(ctx, st.ID)
	must(t, err)
	eq(t, "nothing pruned yet", len(floor) == 2 && floor["a.example"] == 0 && floor["b.example"] == 0, true)
	pruned, _ = s.Streams().PruneLog(ctx, now.Add(-30*24*time.Hour))
	floor, _ = s.Streams().LogFloor(ctx, st.ID)
	log, _ = s.Streams().Log(ctx, st.ID, 0, 10)
	eq(t, "the log keeps 30 days", pruned == 5 && len(log) == 1 && log[0].Seq == 15, true)
	eq(t, "and says how far back each origin still reaches", len(floor) == 1 && floor["a.example"] == 50, true)

	// Stamps. An event is stamped after everything applied in the stream
	// and after the server's own last, whatever the clock says.
	at1, n1, err := s.Streams().Stamp(ctx, st.ID, self.ID, 1_000_000)
	must(t, err)
	at2, n2b, _ := s.Streams().Stamp(ctx, st.ID, self.ID, 1_000_000)
	at3, n3, _ := s.Streams().Stamp(ctx, st.ID, self.ID, 999_000) // the clock went back
	eq(t, "the counter grows by one", n1 == 3 && n2b == 4 && n3 == 5, true)
	eq(t, "at only grows", at1 == 1_000_000 && at2 == 1_000_001 && at3 == 1_000_002, true)
	must(t, s.Streams().Applied(ctx, st.ID, other.ID, 9, 2_000_000))
	must(t, s.Streams().Applied(ctx, st.ID, other.ID, 8, 1_500_000)) // an older one changes nothing
	at4, _, _ := s.Streams().Stamp(ctx, st.ID, self.ID, 1_000_500)
	eq(t, "a reply is stamped after what it answers", at4, int64(2_000_001))
	servers, _ = s.Streams().Servers(ctx, st.ID)
	eq(t, "each origin's counter and at", servers[1].LastN == 9 && servers[1].LastAt == 2_000_000 && servers[0].LastN == 6 && servers[0].LastAt == 2_000_001, true)
	got, _ = s.Streams().Get(ctx, st.ID)
	eq(t, "the stream's highest at", got.MaxAt, int64(2_000_001))
	must(t, s.Streams().Applied(ctx, st.ID, other.ID, 10, 0)) // held but not applied: the counter moves, the time does not
	servers, _ = s.Streams().Servers(ctx, st.ID)
	eq(t, "a counter may move without a time", servers[1].LastN == 10 && servers[1].LastAt == 2_000_000, true)
	_, _, err = s.Streams().Stamp(ctx, model.NewID[model.StreamID](), self.ID, 1)
	wantErr(t, err, model.ErrNotFound)
	// A stamp the reference refused is void, and so is every one given
	// after it: stamping goes back to what stands.
	wild, _, _ := s.Streams().Stamp(ctx, st.ID, self.ID, 9_000_000) // a clock that jumped
	eq(t, "a clock that jumped stamps far ahead", wild, int64(9_000_000))
	must(t, s.Streams().Acked(ctx, st.ID, at3))
	must(t, s.Streams().Acked(ctx, st.ID, at1)) // an older acknowledgement changes nothing
	must(t, s.Streams().Rewind(ctx, st.ID, self.ID))
	got, _ = s.Streams().Get(ctx, st.ID)
	eq(t, "rewound to the last at that stands: the other server's", got.MaxAt, int64(2_000_000))
	again, _, _ := s.Streams().Stamp(ctx, st.ID, self.ID, 1_000_600)
	eq(t, "and stamped from there", again, int64(2_000_001))
	at4 = again

	// Where a message is shown.
	first := post(t, s, ch.ID, owner.ID, "", "first")
	eq(t, "in a conversation never shared the order follows seq", first.Order, model.LocalOrder(0, first.Seq))
	stamped := model.Message{ConversationID: ch.ID, AuthorID: owner.ID, Text: "stamped", Order: model.OrderString(at4, "a.example", 6), Late: true}
	stamped.Seq, _, err = s.Streams().TakeSeq(ctx, ch.ID)
	must(t, err)
	must(t, s.Messages().Insert(ctx, &stamped, nil))
	placed, err := s.Messages().Get(ctx, stamped.ID)
	must(t, err)
	eq(t, "a stamped message keeps its order string and its lateness", placed.Order == model.OrderString(at4, "a.example", 6) && placed.Late, true)
	eq(t, "the time can be read back out of it", model.OrderAt(placed.Order), at4)
	must(t, s.Streams().SetOrder(ctx, stamped.ID, model.OrderString(at4+5, "a.example", 7)))
	must(t, s.Streams().SetEditOrder(ctx, stamped.ID, "x"))
	placed, _ = s.Messages().Get(ctx, stamped.ID)
	eq(t, "restamped", placed.Order == model.OrderString(at4+5, "a.example", 7) && placed.EditOrder == "x", true)
	wantErr(t, s.Streams().SetOrder(ctx, model.NewID[model.MessageID](), "x"), model.ErrNotFound)
	// A stamped reply puts its order in the parent's summary, and a new
	// stamp moves it.
	reply := model.Message{ConversationID: ch.ID, AuthorID: owner.ID, ParentID: first.ID, Text: "re", Order: model.OrderString(at4+6, "a.example", 8)}
	reply.Seq, _, err = s.Streams().TakeSeq(ctx, ch.ID)
	must(t, err)
	must(t, s.Messages().Insert(ctx, &reply, nil))
	parent, _ := s.Messages().Get(ctx, first.ID)
	eq(t, "the parent has the reply's order", parent.LastReplyOrder, model.OrderString(at4+6, "a.example", 8))
	must(t, s.Streams().SetOrder(ctx, reply.ID, model.OrderString(at4+7, "a.example", 9)))
	parent, _ = s.Messages().Get(ctx, first.ID)
	eq(t, "and follows a new stamp", parent.LastReplyOrder, model.OrderString(at4+7, "a.example", 9))
	must(t, s.Streams().RaiseTop(ctx, st.ID, "b"))
	must(t, s.Streams().RaiseTop(ctx, st.ID, "a"))
	got, _ = s.Streams().Get(ctx, st.ID)
	eq(t, "the top order string only rises", got.TopOrd, "b")
	// A conversation that was shared and no longer is orders what is
	// posted now after everything from that time.
	must(t, s.Streams().SetOrderFloor(ctx, ch.ID, at4+6))
	after := post(t, s, ch.ID, owner.ID, "", "after sharing ended")
	eq(t, "the floor is in the order string", after.Order, model.LocalOrder(at4+6, after.Seq))
	eq(t, "local before, stamped, local after: in that order", first.Order < placed.Order && placed.Order < after.Order, true)
	conv, _ := s.Conversations().Get(ctx, ch.ID)
	eq(t, "the conversation knows its floor", conv.OrderFloor, at4+6)
	seq2, in, err := s.Streams().TakeSeq(ctx, ch.ID)
	must(t, err)
	eq(t, "taking a seq says which stream the conversation is in", seq2 == after.Seq+1 && in == conv.StreamID, true)
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Streams().LockConversation(ctx, ch.ID); err != nil {
			return err
		}
		return tx.Streams().LockConversation(ctx, model.NewID[model.ConversationID]()) // gone: not an error
	}))
	waiting, _ := s.Streams().OutboxIDs(ctx, st.ID)
	eq(t, "what waits in the outbox, by id", len(waiting) == 1 && waiting[0] == e1, true)

	// Shadow accounts.
	lee := model.Account{ID: model.NewID[model.AccountID](), ServerID: other.ID, Username: "lee", DisplayName: "Lee", ProfileVersion: 3}
	changed, err := s.Streams().PutShadow(ctx, &lee)
	must(t, err)
	eq(t, "created", changed, true)
	acc, err := s.Accounts().Get(ctx, lee.ID)
	must(t, err)
	eq(t, "a shadow is remote, with its home server", acc.Role == model.RoleRemote && acc.ServerID == other.ID && acc.ServerName == "b.example" && !acc.Active(), true)
	older := lee
	older.DisplayName, older.ProfileVersion = "Old", 2
	changed, _ = s.Streams().PutShadow(ctx, &older)
	eq(t, "an older profile is ignored", changed, false)
	newer := lee
	newer.DisplayName, newer.ProfileVersion = "Lee Park", 4
	changed, _ = s.Streams().PutShadow(ctx, &newer)
	acc, _ = s.Accounts().Get(ctx, lee.ID)
	eq(t, "a newer one is applied", changed && acc.DisplayName == "Lee Park" && acc.ProfileVersion == 4, true)
	shadow, err := s.Streams().Shadow(ctx, other.ID, "lee")
	must(t, err)
	eq(t, "a shadow is found by its name at home", shadow.ID, lee.ID)
	_, err = s.Streams().Shadow(ctx, other.ID, "owner")
	wantErr(t, err, model.ErrNotFound)
	forged := model.Account{ID: owner.ID, ServerID: other.ID, Username: "owner", DisplayName: "x", ProfileVersion: 99}
	_, err = s.Streams().PutShadow(ctx, &forged)
	wantErr(t, err, model.ErrConflict)
	// The same username on this server and on that one do not collide.
	twin := model.Account{ID: model.NewID[model.AccountID](), ServerID: other.ID, Username: "owner", DisplayName: "Other owner", ProfileVersion: 1}
	_, err = s.Streams().PutShadow(ctx, &twin)
	must(t, err)

	// Files that are still arriving.
	f := model.File{ID: model.NewID[model.FileID](), ConversationID: ch.ID, UploaderID: lee.ID, Name: "a.png", Size: 3, MediaType: "application/octet-stream",
		Kind: "file", SHA256: []byte("sum"), State: "arriving"}
	must(t, s.Files().Insert(ctx, &f))
	arriving, err := s.Streams().ArrivingFiles(ctx, 10)
	must(t, err)
	eq(t, "arriving, with where to ask", len(arriving) == 1 && arriving[0].ServerID == other.ID && arriving[0].StreamID == st.ID, true)
	_, ok, _ := s.Streams().FileInStream(ctx, f.ID, ch.ID)
	eq(t, "a file with no bytes cannot be served", ok, false)
	f.BlobKey, f.MediaType, f.Kind, f.Width = "disk:x", "image/png", "image", 4
	must(t, s.Streams().FileArrived(ctx, f))
	back, _ := s.Files().Get(ctx, f.ID)
	eq(t, "arrived", back.BlobKey == "disk:x" && back.Kind == "image" && back.Width == 4 && back.State == "", true)
	_, ok, _ = s.Streams().FileInStream(ctx, f.ID, ch.ID)
	_, elsewhere, _ := s.Streams().FileInStream(ctx, f.ID, parentlessChannel(t, s, owner.ID))
	eq(t, "a file belongs to its conversation only", ok && !elsewhere, true)
	must(t, s.Streams().SetFileState(ctx, f.ID, "unavailable"))

	// Emoji from another server sit beside local ones of the same name.
	must(t, s.Streams().PutRemoteEmoji(ctx, model.Emoji{ID: model.NewID[model.EmojiID](), Name: "party", FileID: f.ID, CreatedByID: lee.ID}, other.ID))
	local := model.Emoji{Name: "party", FileID: f.ID, CreatedByID: owner.ID}
	must(t, s.Emoji().Create(ctx, &local))
	em, err := s.Streams().RemoteEmoji(ctx, other.ID, "party")
	must(t, err)
	eq(t, "found with its server", em.ServerName, "b.example")

	// The rule of section 16: both in an active shared channel.
	both, _ := s.Streams().SharedWith(ctx, owner.ID, lee.ID)
	eq(t, "no shared channel yet", len(both), 0)
	join(t, s, ch.ID, lee.ID, model.LevelMember)
	both, _ = s.Streams().SharedWith(ctx, owner.ID, lee.ID)
	mine, _ := s.Streams().AccountStreams(ctx, lee.ID)
	eq(t, "one shared channel", len(both) == 1 && len(mine) == 1, true)

	// Detaching keeps the keys and drops what was in flight.
	must(t, s.Streams().Detach(ctx, st.ID))
	rows, _ = s.Streams().Outbox(ctx, st.ID)
	log, _ = s.Streams().Log(ctx, st.ID, 0, 10)
	keys, _ = s.Streams().Keys(ctx, st.ID)
	eq(t, "detached", len(rows) == 0 && len(log) == 0 && len(keys) == 2, true)
	_, err = s.Streams().Update(ctx, st.ID, func(s *store.Stream) error { s.State = store.StreamLeft; return nil })
	must(t, err)
	_, err = s.Streams().ByConversation(ctx, ch.ID)
	wantErr(t, err, model.ErrNotFound)
	must(t, s.Streams().Delete(ctx, st.ID))
	_, err = s.Streams().Get(ctx, st.ID)
	wantErr(t, err, model.ErrNotFound)
}

// parentlessChannel makes one more channel and returns its id.
func parentlessChannel(t *testing.T, s store.Areas, creator model.AccountID) model.ConversationID {
	return newChannel(t, s, "elsewhere", false, creator).ID
}

// testStreamMoves covers the stream_moves and stream_channels tables, and
// the re-pointing of a stream that a move ends in.
func testStreamMoves(t *testing.T, s store.Store, st store.Stream, peer store.Peer, owner model.AccountID, now model.Time) {
	// Moves onto a hub.
	hubPeer := store.Peer{Kind: "hub", HubName: "hub.example", State: store.PeerActive, CreatedBy: owner}
	must(t, s.Peers().Create(ctx, &hubPeer))
	mv := store.StreamMove{ID: model.NewID[string](), StreamID: st.ID, FromPeerID: peer.ID, HubPeerID: hubPeer.ID, HubName: "hub.example",
		HubFingerprint: "AAAA-BBBB", State: store.MoveProposed, ProposedBy: owner, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	must(t, s.Streams().PutMove(ctx, mv))
	gotMove, err := s.Streams().Move(ctx, mv.ID)
	must(t, err)
	eq(t, "a move is stored", gotMove.State == store.MoveProposed && gotMove.HubPeerID == hubPeer.ID && gotMove.FromPeerID == peer.ID &&
		gotMove.ExpiresAt == mv.ExpiresAt && gotMove.CommitSeq == 0 && gotMove.CommitEventID == "" && gotMove.Accept == nil && gotMove.Open(), true)
	mv.State, mv.CommitSeq, mv.CommitEventID, mv.UpdatedAt = store.MoveCommitted, 9, model.NewID[string](), now.Add(time.Second)
	mv.Accept, mv.AcceptSig, mv.Commit, mv.CommitSig = []byte("yes"), []byte("s1"), []byte("done"), []byte("s2")
	must(t, s.Streams().PutMove(ctx, mv))
	gotMove, _ = s.Streams().Move(ctx, mv.ID)
	eq(t, "a move is replaced in place", gotMove.State == store.MoveCommitted && gotMove.CommitSeq == 9 &&
		string(gotMove.Accept) == "yes" && string(gotMove.AcceptSig) == "s1" && string(gotMove.Commit) == "done" && string(gotMove.CommitSig) == "s2" &&
		gotMove.CommitEventID == mv.CommitEventID && gotMove.CreatedAt == now && gotMove.UpdatedAt == now.Add(time.Second), true)
	older := store.StreamMove{ID: model.NewID[string](), StreamID: st.ID, FromPeerID: peer.ID, HubName: "hub.example", HubFingerprint: "AAAA-BBBB",
		State: store.MoveAbandoned, Reason: "expired", CreatedAt: now.Add(-time.Hour), ExpiresAt: now}
	must(t, s.Streams().PutMove(ctx, older))
	moves, err := s.Streams().Moves(ctx, st.ID)
	must(t, err)
	eq(t, "a stream's moves, newest first", len(moves) == 2 && moves[0].ID == mv.ID && moves[1].Reason == "expired" && !moves[1].Open() &&
		moves[1].HubPeerID == "", true)
	allMoves, _ := s.Streams().Moves(ctx, "")
	eq(t, "every move", len(allMoves), 2)
	_, err = s.Streams().Move(ctx, model.NewID[string]())
	wantErr(t, err, model.ErrNotFound)
	// The record of a hub-owned channel.
	_, err = s.Streams().Channel(ctx, st.ID)
	wantErr(t, err, model.ErrNotFound)
	must(t, s.Streams().PutChannel(ctx, store.ChannelRecord{StreamID: st.ID, Version: 1, HomeHub: "hub.example", Record: []byte("one"), Sig: []byte("s1")}))
	must(t, s.Streams().PutChannel(ctx, store.ChannelRecord{StreamID: st.ID, Version: 2, HomeHub: "hub.example", Record: []byte("two"), Sig: []byte("s2"),
		Makers: []byte(`{"2":"a.example"}`), Managers: []model.AccountID{owner}, Private: true}))
	ch, err := s.Streams().Channel(ctx, st.ID)
	must(t, err)
	eq(t, "a channel record is replaced in place", ch.Version == 2 && string(ch.Record) == "two" && string(ch.Sig) == "s2" && ch.Private &&
		string(ch.Makers) == `{"2":"a.example"}` && ch.HomeHub == "hub.example" && len(ch.Managers) == 1 && ch.Managers[0] == owner, true)
	must(t, s.Streams().PutChannel(ctx, store.ChannelRecord{StreamID: st.ID, Version: 3, HomeHub: "hub.example", Record: []byte("three"), Sig: []byte("s3")}))
	ch, _ = s.Streams().Channel(ctx, st.ID)
	eq(t, "and its local moderators with it", ch.Version == 3 && len(ch.Managers) == 0 && !ch.Private, true)

	must(t, s.Streams().DeleteChannel(ctx, st.ID))
	_, err = s.Streams().Channel(ctx, st.ID)
	wantErr(t, err, model.ErrNotFound)
	must(t, s.Streams().DeleteChannel(ctx, st.ID)) // and again, with nothing there

	// A move re-points a stream: Update writes the peer, the owner and the
	// time reference.
	moved, err := s.Streams().Update(ctx, st.ID, func(s *store.Stream) error {
		s.PeerID, s.Sequencer, s.HomeHub = hubPeer.ID, store.SequencerHub, "hub.example"
		return nil
	})
	must(t, err)
	reread, _ := s.Streams().Get(ctx, st.ID)
	eq(t, "a stream is re-pointed", moved.PeerID == hubPeer.ID && reread.PeerID == hubPeer.ID && reread.Sequencer == store.SequencerHub &&
		reread.HomeHub == "hub.example", true)
	back, _ := s.Streams().Update(ctx, st.ID, func(s *store.Stream) error {
		s.PeerID, s.Sequencer, s.HomeHub = st.PeerID, st.Sequencer, st.HomeHub
		return nil
	})
	eq(t, "and put back", back.PeerID, st.PeerID)
}
