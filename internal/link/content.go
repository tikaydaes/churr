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
	"sync"
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
	// Short says the home hub that sent the snapshot does not yet hold all
	// of what was said before it held the channel. The receiver goes on
	// waiting for the rest, which the hub tells it to ask for again
	// (history_more) once it has it.
	Short bool `json:"short,omitempty"`
	// Requests are, in the first part, the requests to be admitted to a
	// private channel a hub owns that wait for a moderator: shared state of
	// the channel, which every instance's moderators see (section 23a). Their
	// accounts are among Accounts.
	Requests []historyJoinRequest `json:"requests,omitempty"`
}

// historyJoinRequest is one waiting request to be admitted.
type historyJoinRequest struct {
	AccountID model.AccountID `json:"account_id"`
	At        model.Time      `json:"at"`
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
	// Form is a poll's counts as the sender holds them, and whether it is
	// closed: what form.tallied and form.closed said to every server.
	Form *historyForm `json:"form,omitempty"`
	// Deleted marks a message deleted for everyone that has replies: sent
	// with nothing of its content, so that its thread has a parent.
	Deleted *historyDeleted `json:"deleted,omitempty"`
}

type historyForm struct {
	AnswerCount int                                     `json:"answer_count"`
	Tally       map[string]map[string]int               `json:"tally"`
	Voters      map[string]map[string][]model.AccountID `json:"voters,omitempty"`
	ClosedAt    model.Time                              `json:"closed_at,omitempty"`
	ClosedByID  model.AccountID                         `json:"closed_by_id,omitempty"`
}

type historyDeleted struct {
	At   model.Time      `json:"at"`
	ByID model.AccountID `json:"by_id,omitempty"`
}

// historyPartBytes is how much one part holds before sealing.
const historyPartBytes = 200000

// profileFor builds the profile of any account this server holds, its own or
// another server's shadow.
func (s *Service) profileFor(ctx context.Context, acc model.Account) (wireProfile, []wireBlob) {
	if acc.Local() {
		return s.profileOf(ctx, s.store, acc)
	}
	p := wireProfile{ID: acc.ID, Server: acc.ServerName, Username: acc.Username, DisplayName: acc.DisplayName, Title: acc.Title,
		Pronouns: acc.Pronouns, TimeZone: acc.TimeZone, Version: acc.ProfileVersion, Removed: acc.Removed()}
	// With the picture, once its bytes are here: whoever is given the
	// profile can then ask this server for it, as a joining instance asks
	// the hub of a channel it owns.
	var blobs []wireBlob
	if acc.AvatarFileID != "" {
		if f, err := s.store.Files().Get(ctx, acc.AvatarFileID); err == nil && f.BlobKey != "" {
			p.AvatarFileID = f.ID
			blobs = append(blobs, blobOf(f, "avatar"))
		}
	}
	return p, blobs
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
	s.sendHistoryShort(ctx, st, to, re, false)
}

// sendHistoryShort is sendHistoryAs that, with behind, marks a holder's
// snapshot short whatever else: the holder does not yet hold all that the
// hub's log held when it let the asker in.
func (s *Service) sendHistoryShort(ctx context.Context, st store.Stream, to store.Server, re string, behind bool) {
	since := model.Time(0)
	limit := true
	switch st.Kind {
	case store.StreamChannel:
		since = s.now().Add(-time.Duration(st.HistoryDays) * 24 * time.Hour)
		limit = st.HistoryDays > 0
	case store.StreamSealed:
		limit = false // the devices handle their own history
	}
	s.sendHistoryRangeShort(ctx, st, to, 0, 0, since, limit, true, re, behind)
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
	s.sendHistoryRangeShort(ctx, st, to, afterAt, throughAt, since, messages, first, re, false)
}

// sendHistoryRangeShort is sendHistoryRange that, with behind, marks a
// holder's snapshot short (sendHistoryShort).
func (s *Service) sendHistoryRangeShort(ctx context.Context, st store.Stream, to store.Server, afterAt, throughAt int64, since model.Time, messages, first bool, re string, behind bool) {
	conv, err := s.store.Conversations().Get(ctx, st.ConversationID)
	if err != nil {
		return
	}
	// A hub's holder that does not yet hold all that was said before it held
	// the channel says so to an instance it gives the snapshot to, and notes
	// that instance, to tell it to ask again once it has the rest.
	short := false
	if first && s.holder && hubOwned(st) {
		short = behind || s.historyPending(ctx, s.store, st.ID)
		if !s.isHomeHub(st, to) {
			s.noteShort(ctx, st.ID, to.Name, short)
		}
	}
	part := historyPart{StreamID: st.ID, Part: 1, Messages: []historyMessage{}}
	authors := map[model.AccountID]bool{}
	size := 0
	send := func(last bool) {
		part.Last, part.Short = last, short
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
	if conv.HistoryFromJoin() && !s.isHomeHub(st, to) {
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
				// are not sent; a deleted message that has replies goes as
				// a bare marker, so that the thread keeps its parent.
				if m.CreatedAt.Before(since) {
					continue
				}
				if m.Deleted() {
					// A deleted message that used a draw goes too, with its
					// proof and the client_id it was drawn by, so that
					// every draw stays to be seen (docs/FAIR.md).
					if m.ParentID == "" && (m.ReplyCount > 0 || m.Fair != nil) {
						authors[m.AuthorID] = true
						w := wireMessage{ID: m.ID, AuthorID: m.AuthorID, CreatedAt: m.CreatedAt, Fair: m.Fair}
						if m.Fair != nil {
							w.ClientID = m.ClientID
						}
						held = append(held, historyMessage{Order: m.Order, Message: w,
							Deleted: &historyDeleted{At: m.DeletedAt, ByID: m.DeletedByID}})
					}
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
				if shown.Form != nil {
					if hm.Form, err = s.historyFormOf(ctx, m.ID); err != nil {
						return
					}
					if hm.Form != nil {
						for _, byOption := range hm.Form.Voters {
							for _, ids := range byOption {
								for _, id := range ids {
									authors[id] = true // whoever voted is named, so is known
								}
							}
						}
					}
				}
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
		if hubOwned(st) && s.hubPrivate(ctx, s.store, st) {
			knocks, err := s.store.Members().Knocks(ctx, conv.ID)
			if err != nil {
				return
			}
			for _, k := range knocks {
				part.Requests = append(part.Requests, historyJoinRequest{AccountID: k.AccountID, At: k.CreatedAt})
				authors[k.AccountID] = true
			}
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
		// Only as the answer to this server's own request: nobody sends a
		// hub-owned channel's history unasked. And only from the channel's
		// home hub, which keeps it whole; the hub's own holder takes it from
		// the instances it asked when the hub became the home.
		if m.Re == "" {
			s.count(func(stats *Stats) { stats.Dropped["history_unasked"]++ })
			return
		}
		// The answer of the hub that was the home when this server asked is
		// taken too: the home may have changed hands while it was on its way,
		// and the old home vouched for it as the home.
		rtA := s.rt(st.ID)
		rtA.mu.Lock()
		askedOf := rtA.firstAsked[m.Re]
		rtA.mu.Unlock()
		if !s.holder && m.from.Name != st.HomeHub && askedOf != m.from.Name {
			s.count(func(stats *Stats) { stats.Dropped["history_not_from_home"]++ })
			return
		}
	} else if st.OwnerServerID != m.from.ID {
		s.count(func(stats *Stats) { stats.Dropped["history_not_from_owner"]++ })
		return
	}
	// A server that has just joined a hub-owned channel is delivered, from
	// the hub's log, every event above where the log started for it. A
	// message of the snapshot stamped above that start comes by the log
	// too, and is taken from there and not from here: it is not applied
	// twice.
	rtH := s.rt(st.ID)
	rtH.mu.Lock()
	start := rtH.startHave
	rtH.mu.Unlock()
	if hubOwned(st) && start != nil {
		kept := h.Messages[:0]
		for _, hm := range h.Messages {
			if origin, n, ok := model.OrderStamp(hm.Order); ok && origin != "" && n > start[origin] {
				continue
			}
			kept = append(kept, hm)
		}
		h.Messages = kept
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
			if len(h.Requests) > 0 && hubOwned(st) && s.hubPrivate(ctx, tx, st) {
				// Requests waiting for a moderator, made before this server
				// held the channel: kept as a request made here would be.
				for _, r := range h.Requests {
					acc, err := tx.Accounts().Get(ctx, r.AccountID)
					if err != nil || acc.Local() || acc.Role == model.RoleBot {
						continue
					}
					if _, found, err := tx.Members().Get(ctx, conv.ID, r.AccountID); err != nil {
						return err
					} else if found {
						continue
					}
					at := r.At
					if at.IsZero() {
						at = s.now()
					}
					if _, err := tx.Members().Knock(ctx, &model.Knock{ConversationID: conv.ID, AccountID: r.AccountID, CreatedAt: at}); err != nil {
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
				_, err = tx.Messages().Get(ctx, hm.Message.ID)
				existed := err == nil
				if err != nil && !errors.Is(err, model.ErrNotFound) {
					return err
				}
				if hm.Deleted != nil {
					if !existed && model.ValidID(string(hm.Message.ID)) && !hm.Message.CreatedAt.IsZero() {
						// Its proof is kept when it checks out.
						kept, _ := a.fairOf(hm.Message)
						if err := s.importDeleted(ctx, tx, rec, conv, st, hm, kept); err != nil {
							return err
						}
					}
					continue
				}
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
				if hm.Form != nil && !existed {
					if err := s.importCounts(a, rm.Message, *hm.Form); err != nil {
						return err
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
		// Not taken as arrived: a server that joined a hub-owned channel
		// asks for it again (askFirstHistory).
		s.log.Warn("link: a history snapshot could not be stored", "stream", st.ID, "err", err)
		return
	}
	rt := s.rt(st.ID)
	rt.mu.Lock()
	asked := m.Re != "" && rt.firstAsked[m.Re] != ""
	if asked && h.Last {
		// Noted for the diagnosis of a test that waits in vain (Diagnose).
		if rt.firstAnswered == nil {
			rt.firstAnswered = map[string]bool{}
		}
		rt.firstAnswered[m.Re] = true
	}
	rt.mu.Unlock()
	// A message or an account that came as history may be one that held
	// events wait for.
	s.onJobs(rt, func() { s.retryHeld(s.ctx, rt, true) })
	if s.holder && h.Last && asked {
		// Whoever answered has given the hub all it holds of the channel;
		// the hub it was handed over from, if that held it whole, all of it.
		s.settleOwed(ctx, st.ID, m.from.Name, !h.Short)
	}
	if !s.holder && hubOwned(st) && h.Last && asked && !h.Short {
		// The home hub has given this server the channel's history whole:
		// what waited for it is settled now.
		if _, found, err := s.store.Settings().Get(ctx, historyPendingKey(st.ID)); err == nil && found {
			s.store.Settings().Delete(ctx, historyPendingKey(st.ID))
			s.onJobs(rt, func() { s.historyHere(s.ctx, rt) })
		}
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

// historyFormOf is a poll's counts and closing as this server holds them,
// for a snapshot; nil for a message whose form is not a poll.
func (s *Service) historyFormOf(ctx context.Context, msg model.MessageID) (*historyForm, error) {
	f, found, err := s.store.Forms().ByMessage(ctx, msg)
	if err != nil || !found || f.Action.Type != model.FormTally {
		return nil, err
	}
	fields, err := s.counts(ctx, s.store, f)
	if err != nil {
		return nil, err
	}
	out := &historyForm{AnswerCount: f.AnswerCount, ClosedAt: f.ClosedAt, ClosedByID: f.ClosedBy}
	out.Tally, _ = fields["tally"].(map[string]map[string]int)
	out.Voters, _ = fields["voters"].(map[string]map[string][]model.AccountID)
	return out, nil
}

// importCounts takes a poll's counts and closing from a snapshot, for a
// poll this server does not keep the answers of: as form.tallied and
// form.closed would have left them.
func (s *Service) importCounts(a *apply, m model.Message, hf historyForm) error {
	ctx, tx := a.ctx, a.tx
	f, found, err := tx.Forms().ByMessage(ctx, m.ID)
	if err != nil || !found || f.Action.Type != model.FormTally {
		return err
	}
	if _, home, err := s.formHome(ctx, tx, m); err != nil || home {
		return err
	}
	if err := s.adoptCounts(a, f, formBody{AnswerCount: hf.AnswerCount, Tally: hf.Tally, Voters: hf.Voters}); err != nil {
		return err
	}
	if !hf.ClosedAt.IsZero() {
		by := hf.ClosedByID
		if _, err := tx.Accounts().Get(ctx, by); err != nil {
			by = m.AuthorID
		}
		if _, err := tx.Forms().Close(ctx, f.ID, by, hf.ClosedAt); err != nil {
			return err
		}
	}
	return nil
}

// importDeleted stores, from a snapshot, a message deleted for everyone that
// has replies: a bare marker of who deleted it and when, so that its thread
// has a parent.
func (s *Service) importDeleted(ctx context.Context, tx store.Tx, rec *chat.Recorder, conv model.Conversation, st store.Stream, hm historyMessage, kept *model.FairBlock) error {
	m := model.Message{ID: hm.Message.ID, AuthorID: hm.Message.AuthorID, CreatedAt: hm.Message.CreatedAt, Order: hm.Order, Fair: kept}
	if kept != nil {
		m.ClientID = hm.Message.ClientID
	}
	if err := s.chat.ImportMessage(ctx, tx, rec, conv, chat.RemoteMessage{Message: m}); err != nil {
		return err
	}
	by := hm.Deleted.ByID
	if _, err := tx.Accounts().Get(ctx, by); err != nil {
		by = m.AuthorID
	}
	at := hm.Deleted.At
	if at.IsZero() {
		at = m.CreatedAt
	}
	if _, err := tx.Messages().Delete(ctx, m.ID, by, at); err != nil {
		return err
	}
	return tx.Streams().RaiseTop(ctx, st.ID, hm.Order)
}

// isHomeHub reports whether a server is the home hub of a hub-owned channel:
// the owner of all of it, whom its instances give history and files to, and
// whom they take them from.
func (s *Service) isHomeHub(st store.Stream, sv store.Server) bool {
	return hubOwned(st) && st.HomeHub != "" && sv.Name == st.HomeHub && !s.isSelf(sv.ID)
}

// deafToHome, when a test sets it for a server's name, has that server
// leave unanswered every request for the snapshot of a channel.
var deafToHome sync.Map

// onHistoryRequest answers a server that fell behind further than the log
// reaches with the messages created in the range it names.
func (s *Service) onHistoryRequest(ctx context.Context, m directMessage) {
	var b struct {
		StreamID  model.StreamID `json:"stream_id"`
		AfterAt   int64          `json:"after_at"`
		ThroughAt int64          `json:"through_at"`
		// First asks for the snapshot a joining server is sent, members
		// and all: a server that joined a hub-owned channel and was not
		// connected when the history sender sent it.
		First bool `json:"first"`
	}
	if json.Unmarshal(m.raw, &b) != nil {
		return
	}
	if _, deaf := deafToHome.Load(s.id.Name); deaf && b.First {
		return // a test plays a server that does not answer its home hub
	}
	st, err := s.store.Streams().Get(ctx, b.StreamID)
	if err != nil || !s.answersFor(st, m.from) || st.PeerID != m.peer {
		// A hub's holder asked before its copy of the channel is made, or an
		// instance asked by a hub before it holds the record that makes the
		// hub the channel's home (a channel moving onto it): answered when a
		// record of the channel is taken. An instance that holds nothing of
		// the stream (no copy of it at all) says so.
		if ((err == nil && st.ConversationID != "") || (err != nil && s.holder)) && s.keepAsk(ctx, b.StreamID, m) {
			return
		}
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
	// The home hub of a channel it owns asks for all of it, once, when it
	// becomes the home: what was said before the hub held the channel.
	home := s.isHomeHub(st, m.from)
	if !in && !home {
		// The record that lets the asker in, or names the asking hub the
		// home, may not have reached this server yet: the request is
		// answered when it does.
		if s.keepAsk(ctx, st.ID, m) {
			return
		}
		s.replyError(ctx, m, wire.ENotInStream, "That server is not in the stream.")
		return
	}
	if home && st.State != store.StreamActive {
		// A holder whose hub handed the channel over: nothing more is
		// applied to its copy, which goes to the new home as it stands.
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.sendHistoryRange(s.ctx, st, m.from, 0, 0, 0, true, true, m.MsgID)
		}()
		return
	}
	if home {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			rt := s.rt(st.ID)
			select {
			case <-s.ctx.Done():
			case rt.jobs <- func() {
				if cur, err := s.store.Streams().Get(s.ctx, st.ID); err == nil && s.answersFor(cur, m.from) {
					s.sendHistoryRange(s.ctx, cur, m.from, 0, 0, 0, true, true, m.MsgID)
				}
			}:
			}
		}()
		return
	}
	if b.First && hubOwned(st) {
		// Cut once this server holds what the hub's log held when it let
		// the asker in, off the connection's reader.
		rt := s.rt(st.ID)
		rt.mu.Lock()
		have := rt.joinedHave
		rt.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			whole := s.awaitHave(s.ctx, st.ID, have, 10*time.Second)
			if d := historyAnswerDelay.Load(); d > 0 {
				time.Sleep(time.Duration(d)) // a test plays a server slow to answer
			}
			select {
			case <-s.ctx.Done():
			case rt.jobs <- func() {
				if cur, err := s.store.Streams().Get(s.ctx, st.ID); err == nil && cur.State == store.StreamActive {
					s.sendHistoryShort(s.ctx, cur, m.from, m.MsgID, !whole)
				}
			}:
			}
			if !whole && s.holder {
				// Cut before the holder held all the log had when it let the
				// asker in: the snapshot went short, and the asker is told to
				// ask again the moment the holder does hold it.
				if s.awaitHave(s.ctx, st.ID, have, 24*time.Hour) {
					s.onJobs(rt, func() { s.tellShort(s.ctx, st.ID, m.from.Name) })
				}
			}
		}()
		return
	}
	s.sendHistoryRange(ctx, st, m.from, b.AfterAt, b.ThroughAt, 0, true, false, m.MsgID)
}

// keepAsk keeps a request for a stream's history that this server cannot
// answer yet, to answer when a record of the stream is taken
// (answerEarlyAsks), and reports whether it did. It keeps one only where a
// record may make it answerable: on a hub's holder, and from a hub that may
// be the channel's home. A request is kept for a minute from when it came,
// at most sixteen per stream; one asked of an instance by its hub's holder is
// asked again besides, whenever the instance connects.
func (s *Service) keepAsk(ctx context.Context, id model.StreamID, m directMessage) bool {
	if !s.holder && !s.mayBeHome(ctx, m) {
		return false
	}
	now := time.Now()
	if m.kept.IsZero() {
		m.kept = now
	}
	if now.Sub(m.kept) > keyWait {
		return false // waited its minute: answered as it stands
	}
	rt := s.rt(id)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	kept := rt.earlyAsks[:0]
	for _, k := range rt.earlyAsks {
		if k.until.After(now) {
			kept = append(kept, k)
		}
	}
	rt.earlyAsks = append(kept, earlyKey{m: m, until: m.kept.Add(keyWait)})
	if len(rt.earlyAsks) > 16 {
		rt.earlyAsks = rt.earlyAsks[len(rt.earlyAsks)-16:]
	}
	return true
}

// answerEarlyAsks answers again, after a record of a stream was taken, the
// requests for its history kept because they came too soon. One that still
// cannot be answered is kept again, until its minute is up.
func (s *Service) answerEarlyAsks(ctx context.Context, id model.StreamID) {
	rt := s.rt(id)
	rt.mu.Lock()
	asks := rt.earlyAsks
	rt.earlyAsks = nil
	rt.mu.Unlock()
	for _, k := range asks {
		s.onHistoryRequest(ctx, k.m)
	}
}

// recoverSince asks for the messages stamped after a time of the stream's
// reference clock, which the log that delivers to this server no longer
// holds: of the owner, or if this server is the owner, of the other servers
// in name order.
func (s *Service) recoverSince(ctx context.Context, st store.Stream, afterAt int64) {
	fields := map[string]any{"stream_id": st.ID, "after_at": afterAt, "through_at": 0}
	if hubOwned(st) && !s.holder {
		// A hub-owned channel's home hub keeps all of it.
		if hub, ok := s.homeHub(ctx, st); ok {
			s.ask(ctx, st.PeerID, hub, kHistoryRequest, fields, 30*time.Second)
		}
		return
	}
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
	// asked is the server the file was last asked of: its origin, or the
	// home hub of the channel it is in (fetchArriving).
	asked model.ServerID
}

func (s *Service) fetchOf(id model.FileID) *fetchState {
	f := s.fetches[id]
	if f == nil {
		f = &fetchState{}
		s.fetches[id] = f
	}
	return f
}

// hubChannelFor picks an active hub-owned channel to ask its home hub for a
// file whose origin this server shares nothing with any more: one after
// another, by how many times the file has been asked for, so that a file
// one hub does not hold is asked of the next.
func (s *Service) hubChannelFor(ctx context.Context, attempt int) (store.Stream, bool) {
	streams, err := s.store.Streams().List(ctx, store.StreamActive)
	if err != nil {
		return store.Stream{}, false
	}
	var held []store.Stream
	for _, st := range streams {
		if hubOwned(st) {
			held = append(held, st)
		}
	}
	if len(held) == 0 {
		return store.Stream{}, false
	}
	return held[attempt%len(held)], true
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

// fetchArriving asks for every file whose bytes have not come: at once when
// its event is applied, then with a wait that doubles from 10 seconds to an
// hour, for 7 days, after which the file is unavailable. A file of a channel
// a hub owns is asked of the channel's home hub, which keeps the channel's
// files (holder.go); so is a picture or an emoji whose origin this server
// shares only hub-owned channels with, or none any more. Anything else is
// asked of its origin, as the hub's holder asks for every file.
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
			st, ok = s.streamReaching(ctx, af.ServerID)
		}
		s.mu.Lock()
		attempts := s.fetchOf(af.File.ID).attempts
		s.mu.Unlock()
		if !ok && !s.holder {
			// Its origin is in no stream with this server any more: a
			// picture or an emoji it was shown in a hub-owned channel's
			// history. The hubs of those channels keep them.
			st, ok = s.hubChannelFor(ctx, attempts)
		}
		if !ok {
			continue
		}
		target, err := s.store.Peers().Server(ctx, af.ServerID)
		if hubOwned(st) && !s.holder {
			target, ok = s.homeHub(ctx, st)
			err = nil
		} else if from := s.heldFrom(ctx, st.ID); s.holder && from != "" && attempts%2 == 0 {
			// A channel handed to this holder's hub: the hub it came from
			// holds every file of it, at every other try.
			if hub, found := s.clusterHub(ctx, st, from); found {
				target, err = hub, nil
			}
		}
		if err != nil || !ok {
			continue
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
			if state.xfer != nil && state.xfer.from == target.ID {
				from = state.xfer.next // a transfer cut short resumes
			} else {
				state.xfer = nil
			}
			state.asked = target.ID
			s.mu.Unlock()
			s.sendDirect(ctx, st.PeerID, target, kBlobRequest, "", map[string]any{"stream_id": st.ID, "blob_id": af.File.ID, "from_index": from})
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
	if err != nil || !s.answersFor(st, m.from) || st.PeerID != m.peer {
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
	// The home hub of a channel it owns fetches every file of it, to keep.
	if !in && !s.isHomeHub(st, m.from) {
		s.replyError(ctx, m, wire.ENotInStream, "That server is not in the stream.")
		return
	}
	// A custom emoji is asked for by the emoji's id. (Another server's
	// emoji is kept here under a file of the same id.)
	fileID := model.FileID(b.BlobID)
	if em, err := s.store.Emoji().Get(ctx, model.EmojiID(b.BlobID)); err == nil && em.ServerName == "" {
		fileID = em.FileID
	}
	f, ok, err := s.store.Streams().FileInStream(ctx, fileID, st.ConversationID)
	arriving := false
	if err == nil && !ok && s.holder {
		// The hub's holder is fetching it from its origin, asked at the
		// moment its message passed, as the instance asking now was: it
		// answers once the file has come.
		if row, gerr := s.store.Files().Get(ctx, fileID); gerr == nil && row.State == "arriving" {
			arriving = true
		}
	}
	if err != nil || (!ok && !arriving) || s.blobs == nil {
		s.replyError(ctx, m, wire.ENotFound, "No such file in that stream.")
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if arriving {
			if f, ok = s.awaitFile(fileID, st.ConversationID); !ok {
				s.replyError(s.ctx, m, wire.ENotFound, "No such file in that stream.")
				return
			}
		}
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

// awaitFile waits, for at most a minute, for a file that is arriving here
// to have come.
func (s *Service) awaitFile(id model.FileID, conv model.ConversationID) (model.File, bool) {
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		f, ok, err := s.store.Streams().FileInStream(s.ctx, id, conv)
		if err != nil {
			return f, false
		}
		if ok {
			return f, true
		}
		if row, err := s.store.Files().Get(s.ctx, id); err != nil || row.State != "arriving" {
			return f, false
		}
		select {
		case <-s.ctx.Done():
			return f, false
		case <-time.After(100 * time.Millisecond):
		}
	}
	return model.File{}, false
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
	s.mu.Lock()
	asked := s.fetchOf(id).asked
	s.mu.Unlock()
	if err != nil || (uploader.ServerID != m.from.ID && asked != m.from.ID) {
		// Only the file's origin, or the hub it was asked of, is listened
		// to; either way the bytes must match the size and hash the
		// origin's signed event gave.
		return
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
