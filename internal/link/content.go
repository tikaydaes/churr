package link

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"sort"
	"time"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is what travels beside the stream's log: history for a server
// that joins or fell behind (section 13), files and custom emoji (section
// 15), the announcements a server makes of its own people (section 14), and
// presence and typing (section 17).

// ---------------------------------------------------------------- history

// historyPart is one history direct message.
type historyPart struct {
	StreamID     model.StreamID `json:"stream_id"`
	Part         int            `json:"part"`
	Last         bool           `json:"last"`
	Conversation *struct {
		Name  string `json:"name"`
		Topic string `json:"topic"`
	} `json:"conversation,omitempty"`
	Accounts  []wireProfile     `json:"accounts,omitempty"`
	MemberIDs []model.AccountID `json:"member_ids,omitempty"`
	Messages  []historyMessage  `json:"messages"`
	Blobs     []wireBlob        `json:"blobs,omitempty"`
}

type historyMessage struct {
	// Order is the message's order string on the server that sends the
	// snapshot: the stamp of the event that made it, or, for what the
	// channel held before it was shared, the owner's own order from then.
	// The receiver keeps it as it is, so every server shows the history in
	// one order.
	Order     string      `json:"order"`
	Message   wireMessage `json:"message"`
	Reactions []struct {
		Emoji      string            `json:"emoji"`
		AccountIDs []model.AccountID `json:"account_ids"`
	} `json:"reactions,omitempty"`
	Tasks []model.Task `json:"tasks,omitempty"`
}

// historyPartBytes is how much one part holds before sealing.
const historyPartBytes = 200000

// profileFor builds the profile of any account this server holds, its own or
// another server's shadow.
func (s *Service) profileFor(ctx context.Context, acc model.Account) (wireProfile, []wireBlob) {
	if acc.Local() {
		return s.profileOf(ctx, s.store, acc)
	}
	return wireProfile{ID: acc.ID, Server: acc.ServerName, Username: acc.Username, DisplayName: acc.DisplayName, Title: acc.Title,
		Pronouns: acc.Pronouns, TimeZone: acc.TimeZone, Version: acc.ProfileVersion, Removed: acc.Removed()}, nil
}

// sendHistory sends a server the snapshot of a conversation: its messages
// as this server holds them now, in their current state. With days set, only
// those from the last so many days; a channel shared with 0 days sends none,
// an ad-hoc channel sends everything.
func (s *Service) sendHistory(ctx context.Context, st store.Stream, to store.Server) {
	s.sendHistoryAs(ctx, st, to, "")
}

// sendHistoryAs is sendHistory as the answer to a request, when re is one.
func (s *Service) sendHistoryAs(ctx context.Context, st store.Stream, to store.Server, re string) {
	since := model.Time(0)
	limit := true
	switch st.Kind {
	case store.StreamChannel:
		since = s.now().Add(-time.Duration(st.HistoryDays) * 24 * time.Hour)
		limit = st.HistoryDays > 0
	case store.StreamSealed:
		limit = false // the devices handle their own history
	}
	s.sendHistoryRange(ctx, st, to, 0, 0, since, limit, true, re)
}

// historyFloor returns the earliest first join, in a private channel, of
// the accounts of one server that are members of it; any is false when it
// has none.
func (s *Service) historyFloor(ctx context.Context, conv model.ConversationID, server model.ServerID) (floor model.Time, any bool, err error) {
	members, err := s.store.Members().All(ctx, conv)
	if err != nil {
		return 0, false, err
	}
	ids := make([]model.AccountID, len(members))
	from := make(map[model.AccountID]model.Time, len(members))
	for i, m := range members {
		ids[i], from[m.AccountID] = m.AccountID, m.VisibleFrom
	}
	accs, err := s.store.Accounts().GetMany(ctx, ids)
	if err != nil {
		return 0, false, err
	}
	for _, a := range accs {
		if a.ServerID != server {
			continue
		}
		if at := from[a.ID]; !any || at < floor {
			floor, any = at, true
		}
	}
	return floor, any, nil
}

// sendHistoryRange is sendHistory for the messages stamped after afterAt and,
// when throughAt is not 0, at or before it: times of the stream's reference
// clock.
func (s *Service) sendHistoryRange(ctx context.Context, st store.Stream, to store.Server, afterAt, throughAt int64, since model.Time, messages, first bool, re string) {
	conv, err := s.store.Conversations().Get(ctx, st.ConversationID)
	if err != nil {
		return
	}
	part := historyPart{StreamID: st.ID, Part: 1, Messages: []historyMessage{}}
	authors := map[model.AccountID]bool{}
	size := 0
	send := func(last bool) {
		part.Last = last
		raw, _ := json.Marshal(part)
		var fields map[string]any
		json.Unmarshal(raw, &fields)
		if _, err := s.sendDirect(ctx, st.PeerID, to, kHistory, re, fields); err != nil {
			s.log.Debug("link: history could not be sent", "stream", st.ID, "err", err)
		}
		part = historyPart{StreamID: st.ID, Part: part.Part + 1, Messages: []historyMessage{}}
		size = 0
	}
	var held []historyMessage
	if conv.HistoryFromJoin() {
		// A private channel's history goes to a server only from the
		// first join of the earliest of its accounts there: what was said
		// before is not theirs (ruling R87).
		floor, any, err := s.historyFloor(ctx, conv.ID, to.ID)
		if err != nil {
			return
		}
		messages = messages && any
		if floor > since {
			since = floor
		}
	}
	if messages {
		cursor := int64(0)
		for {
			msgs, more, err := s.store.Messages().List(ctx, conv.ID, store.MessageList{SeqPage: store.SeqPage{After: cursor, Limit: 100}})
			if err != nil {
				return
			}
			for _, m := range msgs {
				cursor = m.Seq
				if at := model.OrderAt(m.Order); (afterAt > 0 && at <= afterAt) || (throughAt > 0 && at > throughAt) {
					continue
				}
				// Deleted messages, earlier versions and tick histories
				// are not sent.
				if m.Deleted() || m.CreatedAt.Before(since) {
					continue
				}
				shown, err := s.chat.Neutral(ctx, s.store, conv, m)
				if err != nil {
					return
				}
				w, blobs, err := s.wireMessageOf(ctx, s.store, st, conv, shown)
				if err != nil {
					return
				}
				hm := historyMessage{Order: m.Order, Message: w, Tasks: shown.Tasks}
				reactions, err := s.store.Reactions().List(ctx, m.ID, "")
				if err != nil {
					return
				}
				for _, r := range reactions {
					hm.Reactions = append(hm.Reactions, struct {
						Emoji      string            `json:"emoji"`
						AccountIDs []model.AccountID `json:"account_ids"`
					}{r.Emoji, r.AccountIDs})
				}
				authors[m.AuthorID] = true
				held = append(held, hm)
				part.Blobs = append(part.Blobs, blobs...)
			}
			if !more || len(msgs) == 0 {
				break
			}
		}
	}
	if first {
		// The first part names the conversation, every current member
		// and every author of a message in the snapshot.
		part.Conversation = &struct {
			Name  string `json:"name"`
			Topic string `json:"topic"`
		}{conv.Name, conv.Topic}
		members, err := s.store.Members().IDs(ctx, conv.ID)
		if err != nil {
			return
		}
		part.MemberIDs = members
		for _, id := range members {
			authors[id] = true
		}
	}
	ids := make([]model.AccountID, 0, len(authors))
	for id := range authors {
		ids = append(ids, id)
	}
	accounts, err := s.store.Accounts().GetMany(ctx, ids)
	if err != nil {
		return
	}
	for _, acc := range accounts {
		p, blobs := s.profileFor(ctx, acc)
		part.Accounts = append(part.Accounts, p)
		part.Blobs = append(part.Blobs, blobs...)
	}
	// In the order they are shown, so the receiver's arrival numbers follow
	// it.
	sort.SliceStable(held, func(i, j int) bool { return held[i].Order < held[j].Order })
	for _, hm := range held {
		raw, _ := json.Marshal(hm)
		if size > 0 && size+len(raw) > historyPartBytes {
			send(false)
		}
		part.Messages = append(part.Messages, hm)
		size += len(raw)
	}
	send(true)
}

// onHistory stores a history snapshot. History is vouched for by the server
// that sent it and by nobody else: the owner, or for a range this server
// asked for, whichever server answered.
func (s *Service) onHistory(ctx context.Context, m directMessage) {
	var h historyPart
	if json.Unmarshal(m.raw, &h) != nil {
		return
	}
	st, err := s.store.Streams().Get(ctx, h.StreamID)
	if err != nil || st.State != store.StreamActive || st.PeerID != m.peer || st.ConversationID == "" {
		return
	}
	if st.Owned() {
		if m.Re == "" {
			return // an owner takes history only as the answer to its own request
		}
	} else if hubOwned(st) {
		// From a server the hub named as a key-maker of this channel (it
		// owes a joining server its history), or as the answer to this
		// server's own request.
		if m.Re == "" && !s.namedMaker(ctx, st, m.from.Name) {
			s.count(func(stats *Stats) { stats.Dropped["history_not_from_maker"]++ })
			return
		}
	} else if st.OwnerServerID != m.from.ID {
		s.count(func(stats *Stats) { stats.Dropped["history_not_from_owner"]++ })
		return
	}
	err = s.again(ctx, func() error {
		return s.chat.RemoteTx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
			conv, err := tx.Conversations().Get(ctx, st.ConversationID)
			if err != nil {
				return err
			}
			a := &apply{s: s, ctx: ctx, tx: tx, rec: rec, st: st, conv: conv, in: inner{Type: kHistory, At: s.now()}}
			for _, p := range h.Accounts {
				sv, err := tx.Peers().ServerByName(ctx, p.Server)
				if err != nil || s.isSelf(sv.ID) {
					continue
				}
				a.origin = store.StreamServer{ServerID: sv.ID, Name: sv.Name}
				if rej, err := a.profile(p, h.Blobs, false); err != nil {
					return err
				} else if rej != nil {
					continue
				}
			}
			for _, id := range h.MemberIDs {
				acc, err := tx.Accounts().Get(ctx, id)
				if err != nil || acc.Local() {
					continue
				}
				if _, found, err := tx.Members().Get(ctx, conv.ID, id); err != nil {
					return err
				} else if !found {
					if _, err := tx.Members().AddDirect(ctx, conv.ID, id, model.LevelMember, s.now()); err != nil {
						return err
					}
				}
			}
			if h.Conversation != nil && h.Conversation.Topic != conv.Topic && m.Re == "" {
				if _, err := tx.Conversations().Update(ctx, conv.ID, func(c *model.Conversation) error {
					c.Topic = h.Conversation.Topic
					return nil
				}); err != nil {
					return err
				}
			}
			for _, hm := range h.Messages {
				author, err := tx.Accounts().Get(ctx, hm.Message.AuthorID)
				if err != nil || hm.Order == "" {
					continue
				}
				a.origin = store.StreamServer{ServerID: s.homeOf(author), Name: author.ServerName}
				rm, rej, err := a.toMessage(hm.Message, h.Blobs)
				if err != nil {
					return err
				}
				if rej != nil {
					continue
				}
				if rm.Message.ParentID != "" {
					if _, err := tx.Messages().Get(ctx, rm.Message.ParentID); err != nil {
						continue
					}
				}
				rm.Message.Order = hm.Order
				if err := s.chat.ImportMessage(ctx, tx, rec, conv, rm); err != nil {
					if model.CodeOf(err) != model.CodeInternal {
						continue
					}
					return err
				}
				for _, r := range hm.Reactions {
					for _, who := range r.AccountIDs {
						if _, err := tx.Accounts().Get(ctx, who); err == nil {
							if _, err := tx.Reactions().Add(ctx, rm.Message.ID, who, r.Emoji, hm.Message.CreatedAt); err != nil {
								return err
							}
						}
					}
				}
				for _, t := range hm.Tasks {
					if t.Checked && t.CheckedByID != "" {
						if _, err := tx.Accounts().Get(ctx, t.CheckedByID); err == nil {
							tx.Checklists().Tick(ctx, rm.Message.ID, t.ID, t.CheckedByID, true, t.CheckedAt)
						}
					}
				}
				if err := tx.Streams().RaiseTop(ctx, st.ID, hm.Order); err != nil {
					return err
				}
			}
			return err
		})
	})
	if err != nil {
		s.log.Warn("link: a history snapshot could not be stored", "stream", st.ID, "err", err)
	}
	rt := s.rt(st.ID)
	rt.mu.Lock()
	asked := m.Re != "" && rt.firstAsked[m.Re]
	rt.mu.Unlock()
	// A message that came as history may be one that held events wait for.
	select {
	case rt.jobs <- func() { s.retryHeld(s.ctx, rt) }:
	default:
	}
	if h.Last && (m.Re == "" || asked) {
		rt.mu.Lock()
		rt.historyHere = true
		if rt.historyWait != nil {
			close(rt.historyWait)
			rt.historyWait = nil
		}
		rt.mu.Unlock()
	}
	go s.fetchArriving(s.ctx)
}

// onHistoryRequest answers a server that fell behind further than the log
// reaches with the messages created in the range it names.
func (s *Service) onHistoryRequest(ctx context.Context, m directMessage) {
	var b struct {
		StreamID  model.StreamID `json:"stream_id"`
		AfterAt   int64          `json:"after_at"`
		ThroughAt int64          `json:"through_at"`
		// First asks for the snapshot a joining server is sent, members
		// and all: a server that joined a hub-owned channel and was not
		// connected when its key-maker sent it.
		First bool `json:"first"`
	}
	if json.Unmarshal(m.raw, &b) != nil {
		return
	}
	st, err := s.store.Streams().Get(ctx, b.StreamID)
	if err != nil || st.State != store.StreamActive || st.PeerID != m.peer {
		s.replyError(ctx, m, wire.EUnknownStream, "No such stream here.")
		return
	}
	active, _, err := s.activeServers(ctx, s.store, st.ID)
	if err != nil {
		return
	}
	in := false
	for _, ss := range active {
		in = in || ss.ServerID == m.from.ID
	}
	if !in {
		s.replyError(ctx, m, wire.ENotInStream, "That server is not in the stream.")
		return
	}
	if b.First && hubOwned(st) {
		s.sendHistoryAs(ctx, st, m.from, m.MsgID)
		return
	}
	s.sendHistoryRange(ctx, st, m.from, b.AfterAt, b.ThroughAt, 0, true, false, m.MsgID)
}

// recoverSince asks for the messages stamped after a time of the stream's
// reference clock, which the log that delivers to this server no longer
// holds: of the owner, or if this server is the owner, of the other servers
// in name order.
func (s *Service) recoverSince(ctx context.Context, st store.Stream, afterAt int64) {
	fields := map[string]any{"stream_id": st.ID, "after_at": afterAt, "through_at": 0}
	if !st.Owned() && !hubOwned(st) {
		if owner, err := s.ownerOf(ctx, st); err == nil {
			s.sendDirect(ctx, st.PeerID, owner, kHistoryRequest, "", fields)
		}
		return
	}
	active, _, err := s.activeServers(ctx, s.store, st.ID)
	if err != nil {
		return
	}
	for _, ss := range active {
		if s.isSelf(ss.ServerID) {
			continue
		}
		sv, err := s.store.Peers().Server(ctx, ss.ServerID)
		if err != nil {
			continue
		}
		if _, err := s.ask(ctx, st.PeerID, sv, kHistoryRequest, fields, 30*time.Second); err == nil {
			return
		}
	}
}

// ------------------------------------------------------------------ people

// AccountChanged submits account.updated in every stream one of this
// server's accounts is a member of. The people service calls it when a
// profile changes. Receivers apply the newest version once, however many
// streams carry it.
func (s *Service) AccountChanged(ctx context.Context, account model.AccountID) {
	acc, err := s.store.Accounts().Get(ctx, account)
	if err != nil || !acc.Local() {
		return
	}
	streams, err := s.store.Streams().AccountStreams(ctx, account)
	if err != nil || len(streams) == 0 {
		return
	}
	p, blobs := s.profileOf(ctx, s.store, acc)
	for _, st := range streams {
		if st.Kind == store.StreamSealed {
			continue
		}
		s.enqueue(ctx, st, model.EventAccountUpdated, map[string]any{"account": p, "blobs": blobs})
	}
}

// ------------------------------------------------------------------- files

// transfer is a file on its way in.
type transfer struct {
	file   model.File
	from   model.ServerID
	tmp    *os.File
	next   int
	got    int64
	limit  int64
	sealed bool
}

// fetchState is when a file may be asked for again.
type fetchState struct {
	attempts int
	next     time.Time
	xfer     *transfer
}

func (s *Service) fetchOf(id model.FileID) *fetchState {
	f := s.fetches[id]
	if f == nil {
		f = &fetchState{}
		s.fetches[id] = f
	}
	return f
}

// streamReaching finds an active stream this server shares with another.
func (s *Service) streamReaching(ctx context.Context, server model.ServerID) (store.Stream, bool) {
	streams, err := s.store.Streams().List(ctx, store.StreamActive)
	if err != nil {
		return store.Stream{}, false
	}
	for _, st := range streams {
		active, _, err := s.activeServers(ctx, s.store, st.ID)
		if err != nil {
			continue
		}
		for _, ss := range active {
			if ss.ServerID == server {
				return st, true
			}
		}
	}
	return store.Stream{}, false
}

// fetchArriving asks the origin for every file whose bytes have not come:
// at once when its event is applied, then with a wait that doubles from 10
// seconds to an hour, for 7 days, after which the file is unavailable.
func (s *Service) fetchArriving(ctx context.Context) {
	files, err := s.store.Streams().ArrivingFiles(ctx, 100)
	if err != nil {
		return
	}
	now := time.Now()
	for _, af := range files {
		if af.ServerID == "" {
			continue
		}
		if s.now().Sub(af.File.CreatedAt) > 7*24*time.Hour {
			s.fileFailed(ctx, af.File, "unavailable")
			continue
		}
		st, ok := store.Stream{}, false
		if af.StreamID != "" {
			var err error
			st, err = s.store.Streams().Get(ctx, af.StreamID)
			ok = err == nil
		}
		if !ok {
			if st, ok = s.streamReaching(ctx, af.ServerID); !ok {
				continue
			}
		}
		s.mu.Lock()
		state := s.fetchOf(af.File.ID)
		busy := s.fetching[af.File.ID] && now.Before(state.next)
		due := !now.Before(state.next)
		if !busy && due {
			wait := 10 * time.Second
			for i := 0; i < state.attempts && wait < time.Hour; i++ {
				wait *= 2
			}
			state.attempts++
			state.next = now.Add(min(wait, time.Hour))
			s.fetching[af.File.ID] = true
			from := 0
			if state.xfer != nil && state.xfer.from == af.ServerID {
				from = state.xfer.next // a transfer cut short resumes
			} else {
				state.xfer = nil
			}
			s.mu.Unlock()
			if sv, err := s.store.Peers().Server(ctx, af.ServerID); err == nil {
				s.sendDirect(ctx, st.PeerID, sv, kBlobRequest, "", map[string]any{"stream_id": st.ID, "blob_id": af.File.ID, "from_index": from})
			}
			continue
		}
		s.mu.Unlock()
	}
}

// fileFailed marks a file that will not arrive and tells local members.
func (s *Service) fileFailed(ctx context.Context, f model.File, state string) {
	if err := s.store.Streams().SetFileState(ctx, f.ID, state); err != nil {
		return
	}
	s.mu.Lock()
	delete(s.fetching, f.ID)
	if st := s.fetches[f.ID]; st != nil && st.xfer != nil && st.xfer.tmp != nil {
		st.xfer.tmp.Close()
		os.Remove(st.xfer.tmp.Name())
	}
	delete(s.fetches, f.ID)
	s.mu.Unlock()
	f.State = state
	s.chat.TellLocal(ctx, f.ConversationID, "file.changed", map[string]any{"message_id": f.MessageID, "file": f})
}

// onBlobRequest serves a file to a server that shares the stream it belongs
// to, in chunks, each sealed to the asker alone.
func (s *Service) onBlobRequest(ctx context.Context, m directMessage) {
	var b struct {
		StreamID  model.StreamID `json:"stream_id"`
		BlobID    string         `json:"blob_id"`
		FromIndex int            `json:"from_index"`
	}
	if json.Unmarshal(m.raw, &b) != nil || b.FromIndex < 0 {
		return
	}
	st, err := s.store.Streams().Get(ctx, b.StreamID)
	if err != nil || st.State != store.StreamActive || st.PeerID != m.peer {
		s.replyError(ctx, m, wire.EUnknownStream, "No such stream here.")
		return
	}
	active, _, err := s.activeServers(ctx, s.store, st.ID)
	if err != nil {
		return
	}
	in := false
	for _, ss := range active {
		in = in || ss.ServerID == m.from.ID
	}
	if !in {
		s.replyError(ctx, m, wire.ENotInStream, "That server is not in the stream.")
		return
	}
	// A custom emoji is asked for by the emoji's id.
	fileID := model.FileID(b.BlobID)
	if em, err := s.store.Emoji().Get(ctx, model.EmojiID(b.BlobID)); err == nil && em.ServerName == "" {
		fileID = em.FileID
	}
	f, ok, err := s.store.Streams().FileInStream(ctx, fileID, st.ConversationID)
	if err != nil || !ok || s.blobs == nil {
		s.replyError(ctx, m, wire.ENotFound, "No such file in that stream.")
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		r, err := s.blobs.Get(s.ctx, f.BlobKey)
		if err != nil {
			s.replyError(s.ctx, m, wire.ENotFound, "No such file in that stream.")
			return
		}
		defer r.Close()
		buf := make([]byte, wire.ChunkBytes)
		for index := 0; ; index++ {
			n, err := io.ReadFull(r, buf)
			last := err != nil
			if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
				return
			}
			if index >= b.FromIndex && (n > 0 || index == 0) {
				if _, err := s.sendDirect(s.ctx, m.peer, m.from, kBlobChunk, m.MsgID, map[string]any{"stream_id": st.ID, "blob_id": b.BlobID,
					"index": index, "last": last || int64(index+1)*wire.ChunkBytes >= f.Size, "data": wire.Bytes(buf[:n])}); err != nil {
					return
				}
			}
			if last || int64(index+1)*wire.ChunkBytes >= f.Size {
				return
			}
		}
	}()
}

// onBlobChunk takes one chunk of a file this server asked for. When the
// last is in, the length and the hash are checked against what the origin's
// signed event said, so a relay cannot have swapped the file; then the file
// is treated as an upload of this server's own.
func (s *Service) onBlobChunk(ctx context.Context, m directMessage) {
	var b struct {
		BlobID string     `json:"blob_id"`
		Index  int        `json:"index"`
		Last   bool       `json:"last"`
		Data   wire.Bytes `json:"data"`
	}
	if json.Unmarshal(m.raw, &b) != nil || len(b.Data) > wire.ChunkBytes {
		return
	}
	id := model.FileID(b.BlobID)
	f, err := s.store.Files().Get(ctx, id)
	if err != nil || f.State != "arriving" {
		return
	}
	uploader, err := s.store.Accounts().Get(ctx, f.UploaderID)
	if err != nil || uploader.ServerID != m.from.ID {
		return // only the file's origin is believed
	}
	s.mu.Lock()
	state := s.fetchOf(id)
	x := state.xfer
	if x == nil || x.from != m.from.ID {
		x = &transfer{file: f, from: m.from.ID, limit: f.Size}
		state.xfer = x
	}
	s.mu.Unlock()
	discard := func() {
		s.mu.Lock()
		if x.tmp != nil {
			x.tmp.Close()
			os.Remove(x.tmp.Name())
		}
		state.xfer = nil
		delete(s.fetching, id)
		s.mu.Unlock()
	}
	if b.Index != x.next {
		if b.Index < x.next {
			return // a repeat
		}
		discard()
		return
	}
	if x.tmp == nil {
		if x.tmp, err = os.CreateTemp(s.tmpDir, "arriving-*"); err != nil {
			discard()
			return
		}
	}
	// The receiver stops and discards if the total passes the declared
	// size.
	if x.got+int64(len(b.Data)) > x.limit {
		discard()
		s.fileFailed(ctx, f, "unavailable")
		return
	}
	if _, err := x.tmp.Write(b.Data); err != nil {
		discard()
		return
	}
	x.got += int64(len(b.Data))
	x.next++
	s.mu.Lock()
	state.next = time.Now().Add(time.Minute) // still coming
	s.mu.Unlock()
	if !b.Last {
		return
	}
	sum := sha256.New()
	if _, err := x.tmp.Seek(0, io.SeekStart); err != nil {
		discard()
		return
	}
	if _, err := io.Copy(sum, x.tmp); err != nil {
		discard()
		return
	}
	if x.got != f.Size || !bytes.Equal(sum.Sum(nil), f.SHA256) {
		// A file that fails is discarded. It is asked for again later.
		s.count(func(stats *Stats) { stats.Dropped["blob_mismatch"]++ })
		discard()
		return
	}
	tmp := x.tmp
	x.tmp = nil
	err = s.chat.ReceiveFile(ctx, id, tmp)
	tmp.Close()
	os.Remove(tmp.Name())
	s.mu.Lock()
	state.xfer = nil
	delete(s.fetching, id)
	if err == nil {
		delete(s.fetches, id)
	}
	s.mu.Unlock()
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		s.log.Warn("link: an arrived file could not be stored", "file", id, "err", err)
	}
}

// ---------------------------------------------------- presence and typing

// RemotePresence is the newest presence a remote account's home server sent.
type RemotePresence struct {
	AccountID    model.AccountID `json:"account_id"`
	Presence     string          `json:"presence"`
	InCall       bool            `json:"in_call"`
	DND          bool            `json:"dnd"`
	Status       map[string]any  `json:"status,omitempty"`
	Availability map[string]any  `json:"availability,omitempty"`
	At           model.Time      `json:"-"`
}

// sendsPresence reports whether this server sends presence and typing to
// other servers: the one off switch of section 17.
func (s *Service) sendsPresence(ctx context.Context) bool {
	on, err := store.SettingBool(ctx, s.store, SettingSendPresence, true)
	return err == nil && on
}

// sendEphemeral sends an event that is never stamped, logged or stored.
func (s *Service) sendEphemeral(ctx context.Context, st store.Stream, typ string, fields map[string]any) {
	pc := s.connOf(st.PeerID)
	if pc == nil || st.State != store.StreamActive {
		return
	}
	env, err := s.seal(ctx, s.store, st, newID(), typ, fields, wire.Stamp{}, 0)
	if err != nil {
		return
	}
	pc.conn.Send(wire.Frame{Frame: wire.FEphemeral, Envelope: &env})
}

// Typing implements chat.Outbound: typing.started goes to the stream of the
// conversation being typed in, unless sending is off.
func (s *Service) Typing(ctx context.Context, conv model.Conversation, account model.AccountID, parent model.MessageID) {
	if !s.sendsPresence(ctx) {
		return
	}
	st, err := s.store.Streams().ByConversation(ctx, conv.ID)
	if err != nil {
		return
	}
	fields := map[string]any{"account_id": account}
	if parent != "" {
		fields["parent_id"] = parent
	}
	s.sendEphemeral(ctx, st, model.EventTypingStarted, fields)
}

// presenceOf is what this server says about one of its accounts now.
func (s *Service) presenceOf(acc model.Account) RemotePresence {
	p := RemotePresence{AccountID: acc.ID, Presence: string(model.PresenceOffline)}
	if s.presence != nil {
		p.Presence = string(s.presence.Presence(acc.ID))
	}
	now := s.now()
	if acc.DNDUntil != nil && (*acc.DNDUntil == 0 || acc.DNDUntil.After(now)) {
		p.DND = true
	}
	if acc.StatusEmoji != "" || acc.StatusText != "" {
		p.Status = map[string]any{"emoji": acc.StatusEmoji, "text": acc.StatusText, "until": acc.StatusUntil}
	}
	if acc.AvailabilityIs != "" {
		p.Availability = map[string]any{"value": acc.AvailabilityIs, "until": acc.AvailabilityEnd}
	}
	return p
}

// PresenceChanged sends presence.changed for one of this server's accounts,
// inside the streams of kind channel it is a member of and nowhere else, at
// most once per account per stream every 15 seconds; a change inside that
// time is folded into the next one.
func (s *Service) PresenceChanged(account model.AccountID) {
	ctx := s.ctx
	// With no other server connected there is nobody to send it to, and
	// when every client reconnects at once this is asked thousands of
	// times: nothing is read to find that out.
	s.mu.Lock()
	linked := len(s.conns) > 0
	s.mu.Unlock()
	if !linked || !s.sendsPresence(ctx) {
		return
	}
	acc, err := s.store.Accounts().Get(ctx, account)
	if err != nil || !acc.Local() {
		return
	}
	streams, err := s.store.Streams().AccountStreams(ctx, account)
	if err != nil {
		return
	}
	now := s.now()
	for _, st := range streams {
		if st.Kind != store.StreamChannel {
			continue
		}
		key := string(st.ID) + "|" + string(account)
		s.mu.Lock()
		last, sent := s.presenceSent[key]
		if sent && last.After(now) {
			s.mu.Unlock()
			continue // one is already waiting to go
		}
		if sent && now.Sub(last) < 15*time.Second {
			s.presenceSent[key] = last.Add(15 * time.Second)
			wait := last.Add(15 * time.Second).Sub(now)
			s.mu.Unlock()
			time.AfterFunc(wait, func() {
				s.mu.Lock()
				s.presenceSent[key] = s.now().Add(-time.Hour)
				s.mu.Unlock()
				s.PresenceChanged(account)
			})
			continue
		}
		s.presenceSent[key] = now
		s.mu.Unlock()
		p := s.presenceOf(acc)
		s.sendEphemeral(ctx, st, model.EventPresenceChanged, map[string]any{"account_id": p.AccountID, "presence": p.Presence,
			"in_call": p.InCall, "dnd": p.DND, "status": p.Status, "availability": p.Availability})
	}
}

// snapshotSpread is the time over which a server sends its presence
// snapshots after a connection comes back: each stream's goes after a random
// part of it. When a hub restarts, every server on it reconnects in the same
// moment; without the spread they would all send every snapshot at once, and
// each receiver would be told of everyone in one burst (ruling R62).
const snapshotSpread = 5 * time.Second

// TestSnapshotSpread sets the time snapshots are spread over, for tests.
func (s *Service) TestSnapshotSpread(d time.Duration) { s.snapshotSpread.Store(int64(d)) }

// snapshotDelay is how long one stream's snapshot waits after a
// reconnection: a random time below the spread.
func (s *Service) snapshotDelay() time.Duration {
	spread := time.Duration(s.snapshotSpread.Load())
	if spread == 0 {
		spread = snapshotSpread
	}
	if spread < 0 {
		return 0
	}
	return rand.N(spread)
}

// spreadSnapshots sends presence.snapshot on each stream of a peer whose
// connection has just come up, each after its own random delay. A snapshot
// is put together when its time comes, not now, so it says what is true
// then; it is not sent if the connection has gone again by then.
func (s *Service) spreadSnapshots(ctx context.Context, peer model.PeerID) {
	if !s.sendsPresence(ctx) {
		return
	}
	streams, err := s.store.Streams().List(ctx, store.StreamActive)
	if err != nil {
		return
	}
	for _, st := range streams {
		if st.PeerID != peer || st.Kind != store.StreamChannel || st.ConversationID == "" {
			continue
		}
		id := st.ID
		time.AfterFunc(s.snapshotDelay(), func() {
			if s.ctx.Err() != nil {
				return
			}
			s.sendSnapshot(s.ctx, id)
		})
	}
}

// sendSnapshot sends presence.snapshot on one stream, now: when a server
// joins it, and when a spread snapshot's time has come.
func (s *Service) sendSnapshot(ctx context.Context, id model.StreamID) {
	if !s.sendsPresence(ctx) {
		return
	}
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil || st.State != store.StreamActive || st.Kind != store.StreamChannel || st.ConversationID == "" {
		return
	}
	ids, err := s.store.Members().IDs(ctx, st.ConversationID)
	if err != nil {
		return
	}
	accounts, err := s.store.Accounts().GetMany(ctx, ids)
	if err != nil {
		return
	}
	list := []RemotePresence{}
	for _, acc := range accounts {
		if !acc.Local() {
			continue
		}
		if p := s.presenceOf(acc); p.Presence != string(model.PresenceOffline) {
			list = append(list, p)
		}
	}
	if len(list) > 0 {
		s.sendEphemeral(ctx, st, "presence.snapshot", map[string]any{"accounts": list})
		s.count(func(stats *Stats) { stats.Snapshots++ })
	}
}

// PresenceOf returns the newest presence held for a remote account. There
// is none for an account nothing has been heard about, or whose value is
// older than two minutes of silence from its server: such an account is
// shown with no presence at all rather than as offline.
func (s *Service) PresenceOf(account model.AccountID) (RemotePresence, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.presenceSeen[account]
	return p, ok
}

// sweepPresence forgets presence of accounts whose servers are not
// connected.
func (s *Service) sweepPresence() {
	now := s.now()
	s.mu.Lock()
	for id, p := range s.presenceSeen {
		if now.Sub(p.At) > time.Hour {
			delete(s.presenceSeen, id)
		}
	}
	s.mu.Unlock()
}

// onEphemeral takes a typing or presence frame. It is accepted only from the
// home server of every account it names, for accounts that are members of
// that stream; anything else is dropped and counted.
func (s *Service) onEphemeral(ctx context.Context, pc *peerConn, f wire.Frame) error {
	if f.Envelope == nil || !f.Envelope.WellFormed() || f.Envelope.N != 0 || f.Envelope.At != 0 {
		return wire.Errf(wire.EBadFrame, "An ephemeral frame carries one envelope without a stamp.")
	}
	env := *f.Envelope
	drop := func(why string) error {
		s.count(func(stats *Stats) { stats.Dropped["ephemeral:"+why]++ })
		return nil
	}
	st, err := s.store.Streams().Get(ctx, model.StreamID(env.StreamID))
	if err != nil || st.State != store.StreamActive || st.PeerID != pc.peer.ID || st.ConversationID == "" {
		return drop("not_in_stream")
	}
	if !pc.hub && env.Origin != pc.name {
		return drop("origin")
	}
	if r := s.limits.ephemeral.Take(string(pc.peer.ID) + "|" + env.Origin); !r.OK {
		e := wire.Errf(wire.ERateLimited, "Too many ephemeral frames.")
		e.RetryAfter = r.RetryAfterSeconds()
		s.refusing.Note("ephemeral frames", env.Origin, e.RetryAfter)
		return e
	}
	s.count(func(stats *Stats) { stats.Ephemeral[st.ID]++ })
	o := s.open(ctx, st, env, false)
	if o.rej != nil {
		return drop(o.rej.reason)
	}
	named := func(id model.AccountID) bool {
		acc, err := s.store.Accounts().Get(ctx, id)
		if err != nil || s.homeOf(acc) != o.origin.ServerID || s.isSelf(o.origin.ServerID) {
			return false
		}
		_, found, err := s.store.Members().Get(ctx, st.ConversationID, id)
		return err == nil && found
	}
	now := s.now()
	keep := func(p RemotePresence) bool {
		if !named(p.AccountID) {
			drop("forged")
			return false
		}
		p.At = o.in.At
		s.mu.Lock()
		defer s.mu.Unlock()
		if old, ok := s.presenceSeen[p.AccountID]; ok && !p.At.After(old.At) {
			return false
		}
		s.presenceSeen[p.AccountID] = p
		return true
	}
	switch o.in.Type {
	case model.EventTypingStarted:
		var b struct {
			AccountID model.AccountID `json:"account_id"`
			ParentID  model.MessageID `json:"parent_id"`
		}
		if json.Unmarshal(o.in.raw, &b) != nil || !named(b.AccountID) {
			return drop("forged")
		}
		fields := map[string]any{"account_id": b.AccountID}
		if b.ParentID != "" {
			fields["parent_id"] = b.ParentID
		}
		s.chat.TellLocal(ctx, st.ConversationID, model.EventTypingStarted, fields)
	case model.EventPresenceChanged:
		var p RemotePresence
		if json.Unmarshal(o.in.raw, &p) != nil || now.Sub(o.in.At) > 2*time.Minute {
			return drop("stale")
		}
		if keep(p) {
			s.chat.TellLocal(ctx, st.ConversationID, model.EventPresenceChanged, map[string]any{"account_id": p.AccountID,
				"presence": p.Presence, "in_call": p.InCall})
		}
	case "presence.snapshot":
		var b struct {
			Accounts []RemotePresence `json:"accounts"`
		}
		if json.Unmarshal(o.in.raw, &b) != nil || now.Sub(o.in.At) > 2*time.Minute {
			return drop("stale")
		}
		for _, p := range b.Accounts {
			if keep(p) {
				s.chat.TellLocal(ctx, st.ConversationID, model.EventPresenceChanged, map[string]any{"account_id": p.AccountID,
					"presence": p.Presence, "in_call": p.InCall})
			}
		}
	default:
		return drop("unknown_type")
	}
	return nil
}
