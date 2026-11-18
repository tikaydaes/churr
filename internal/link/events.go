package link

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/instant"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// The inner events of section 11 that this file names itself; the rest use
// the constants of model.
const (
	evMessageRemoved  = "message.removed"
	evMessagesExpired = "messages.expired"
	evFormTallied     = "form.tallied"
	evFormRefused     = "form.answer_refused"
	evMemberInvited   = "member.invited"
	evCallStarted     = "call.started"
	evCallJoined      = "call.participant_joined"
	evCallLeft        = "call.participant_left"
	evCallEnded       = "call.ended"
	evServersChanged  = "stream.servers_changed"
	evOwnerChanged    = "stream.owner_changed"
	evLimitsChanged   = "stream.limits_changed"
	evStreamClosed    = "stream.closed"
	// evMoved is declared in move.go: stream.moved.
	// evLocal marks a change that stays on the server it was made on (a
	// pin, a level, an archive). It never travels: toWire answers with it,
	// and Commit leaves such an event out.
	evLocal = "local"
)

// wireEntity is an entity as it travels: a channel link to the stream's own
// channel carries stream_id in place of conversation_id.
type wireEntity struct {
	model.Entity
	StreamID model.StreamID `json:"stream_id,omitempty"`
}

// wireFile is a file entry as it travels: no addresses, and the hash of its
// bytes.
type wireFile struct {
	ID              model.FileID `json:"id"`
	Name            string       `json:"name"`
	Size            int64        `json:"size"`
	MediaType       string       `json:"media_type"`
	Kind            string       `json:"kind"`
	Width           int          `json:"width,omitempty"`
	Height          int          `json:"height,omitempty"`
	DurationSeconds int          `json:"duration_seconds,omitempty"`
	Caption         string       `json:"caption,omitempty"`
	SHA256          wire.Bytes   `json:"sha256"`
}

// wireBlob lists a file an event refers to (section 11).
type wireBlob struct {
	ID        string     `json:"id"`
	SHA256    wire.Bytes `json:"sha256"`
	Size      int64      `json:"size"`
	MediaType string     `json:"media_type"`
	Purpose   string     `json:"purpose"`
	Name      string     `json:"name,omitempty"`
}

// wireMessage is a message as it travels (section 11, "A message on the
// wire"). Action is a /me message (ruling R67). View is the message as a value of the
// catalogue, such as a roll, and object entities carry values over spans;
// each receiver holds them to their kinds' checks (model.CheckValue).
type wireMessage struct {
	ID        model.MessageID      `json:"id"`
	AuthorID  model.AccountID      `json:"author_id"`
	ClientID  string               `json:"client_id,omitempty"`
	ParentID  model.MessageID      `json:"parent_id,omitempty"`
	Quote     *model.Quote         `json:"quote,omitempty"`
	Text      string               `json:"text,omitempty"`
	Entities  []wireEntity         `json:"entities,omitempty"`
	Files     []wireFile           `json:"files,omitempty"`
	Cards     []model.Card         `json:"cards,omitempty"`
	Form      json.RawMessage      `json:"form,omitempty"`
	Fallback  string               `json:"fallback,omitempty"`
	Via       *model.Via           `json:"via,omitempty"`
	System    map[string]any       `json:"system,omitempty"`
	Sealed    *model.SealedPayload `json:"sealed,omitempty"`
	Quiet     bool                 `json:"quiet,omitempty"`
	Action    bool                 `json:"action,omitempty"`
	View      *model.ValueEnvelope `json:"view,omitempty"`
	Fair      *model.FairBlock     `json:"fair,omitempty"`
	CreatedAt model.Time           `json:"created_at"`
	EditedAt  model.Time           `json:"edited_at,omitempty"`
}

// wireForm is as much of a form as this server reads on arrival: enough to
// refuse one that is not a poll (section 19).
type wireForm struct {
	ID     string `json:"id"`
	Action struct {
		Type string `json:"type"`
	} `json:"action"`
	AnswererGroupID string `json:"answerer_group_id,omitempty"`
}

// wireProfile is an account's profile as it travels (section 14).
type wireProfile struct {
	ID           model.AccountID `json:"id"`
	Server       string          `json:"server"`
	Username     string          `json:"username"`
	DisplayName  string          `json:"display_name"`
	Title        string          `json:"title,omitempty"`
	Pronouns     string          `json:"pronouns,omitempty"`
	TimeZone     string          `json:"time_zone,omitempty"`
	Bot          bool            `json:"bot"`
	AvatarFileID model.FileID    `json:"avatar_file_id,omitempty"`
	Version      int64           `json:"version"`
	Removed      bool            `json:"removed"`
}

// homeOf is the server an account belongs to: for a local account, this one.
func (s *Service) homeOf(acc model.Account) model.ServerID {
	if acc.Local() {
		return s.self.ID
	}
	return acc.ServerID
}

// ownerIs reports whether a server owns a stream.
func (s *Service) ownerIs(st store.Stream, server model.ServerID) bool {
	if st.Owned() {
		return s.isSelf(server)
	}
	return st.OwnerServerID == server
}

// apply is the state one event is checked and applied in.
type apply struct {
	s      *Service
	ctx    context.Context
	tx     store.Tx
	rec    *chat.Recorder
	st     store.Stream
	conv   model.Conversation
	origin store.StreamServer
	in     inner
	env    wire.Envelope
	// order is the order string of the event's stamp: where a message it
	// creates is shown, on every server alike.
	order string
}

// account loads an account an event names and checks that the origin is its
// home server: the heart of section 12. An account of another server
// inside a validly signed envelope is forged authorship.
func (a *apply) account(id model.AccountID) (model.Account, *rejection, error) {
	acc, err := a.tx.Accounts().Get(a.ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		return acc, reject("unknown_account"), nil
	}
	if err != nil {
		return acc, nil, err
	}
	if home := a.s.homeOf(acc); home != a.origin.ServerID {
		homeName := a.s.id.Name
		if !acc.Local() {
			homeName = acc.ServerName
		}
		return acc, &rejection{reason: "forged_author", forged: true,
			detail: map[string]any{"account_id": id, "home": homeName, "type": a.in.Type}}, nil
	}
	return acc, nil, nil
}

// member is account plus "is a member of the conversation".
func (a *apply) member(id model.AccountID) (model.Account, *rejection, error) {
	acc, rej, err := a.account(id)
	if rej != nil || err != nil {
		return acc, rej, err
	}
	if _, found, err := a.tx.Members().Get(a.ctx, a.conv.ID, id); err != nil {
		return acc, nil, err
	} else if !found {
		return acc, reject("not_a_member"), nil
	}
	return acc, nil, nil
}

// message loads a message an event is about. One this server does not hold
// (it was before its history) is rejected quietly.
func (a *apply) message(id model.MessageID) (model.Message, *rejection, error) {
	m, err := a.tx.Messages().Get(a.ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		// Not here, and it may be on its way: another origin's message can
		// reach this server after the event that refers to it (hold.go).
		return m, &rejection{reason: "unknown_message", quiet: true, missing: id}, nil
	}
	if err == nil && m.ConversationID != a.conv.ID {
		return m, rejectQuiet("unknown_message"), nil
	}
	return m, nil, err
}

// authorHome checks that the origin is the home server of a message's
// author, as stored.
func (a *apply) authorHome(m model.Message) (*rejection, error) {
	_, rej, err := a.account(m.AuthorID)
	return rej, err
}

func (a *apply) decode(v any) *rejection {
	if json.Unmarshal(a.in.raw, v) != nil {
		return reject("malformed")
	}
	return nil
}

// applyEvent is the last steps of section 9: is the type known, may this
// origin say it, is it valid; and then the change itself, through chat. A
// rejection is found before anything is written.
func (s *Service) applyEvent(ctx context.Context, tx store.Tx, rec *chat.Recorder, st store.Stream, origin store.StreamServer, in inner, env wire.Envelope) (*rejection, error) {
	a := &apply{s: s, ctx: ctx, tx: tx, rec: rec, st: st, origin: origin, in: in, env: env, order: model.OrderString(env.At, env.Origin, env.N)}
	if st.ConversationID == "" {
		return rejectQuiet("no_conversation"), nil
	}
	conv, err := tx.Conversations().Get(ctx, st.ConversationID)
	if errors.Is(err, model.ErrNotFound) {
		return rejectQuiet("no_conversation"), nil
	}
	if err != nil {
		return nil, err
	}
	a.conv = conv
	switch in.Type {
	case evServersChanged, evOwnerChanged, evLimitsChanged, evStreamClosed, evMoved:
		// These write the stream's own row, and then the conversation's.
		// Every other writer takes the conversation first, so they do too.
		if err := tx.Streams().LockConversation(ctx, conv.ID); err != nil {
			return nil, err
		}
	}
	isOwner := s.ownerIs(st, origin.ServerID)
	if st.Kind == store.StreamSealed {
		switch in.Type {
		case model.EventMessageCreated, model.EventSealedEpochStarted:
			return s.applySealed(a)
		case model.EventMemberJoined, model.EventMemberLeft, model.EventAccountUpdated, evServersChanged, evOwnerChanged, evLimitsChanged, evStreamClosed, evLocal:
		default:
			// No other message event is used in a sealed stream.
			return reject("not_in_a_sealed_stream"), nil
		}
	}
	switch in.Type {
	case model.EventMessageCreated:
		return a.messageCreated()
	case model.EventMessageEdited:
		return a.messageEdited()
	case model.EventMessageDeleted, evMessageRemoved:
		return a.messageDeleted(isOwner)
	case model.EventMessagePurged:
		var b struct {
			MessageID model.MessageID `json:"message_id"`
			Erase     string          `json:"erase"`
		}
		if rej := a.decode(&b); rej != nil {
			return rej, nil
		}
		if b.Erase != "history" && b.Erase != "content" {
			return reject("invalid"), nil
		}
		m, rej, err := a.message(b.MessageID)
		if rej != nil || err != nil {
			return rej, err
		}
		if !isOwner {
			if rej, err := a.authorHome(m); rej != nil || err != nil {
				return rej, err
			}
		}
		return nil, s.chat.ApplyPurge(ctx, tx, rec, conv, m.ID, b.Erase == "content")
	case evMessagesExpired:
		var b struct {
			// ThroughAt is a time of the stream's reference clock: every
			// message stamped at or before it goes.
			ThroughAt int64  `json:"through_at"`
			Scope     string `json:"scope"`
		}
		if rej := a.decode(&b); rej != nil {
			return rej, nil
		}
		switch b.Scope {
		case "all":
			if !isOwner {
				return reject("not_owner"), nil
			}
			return nil, s.chat.ApplyExpire(ctx, tx, rec, conv, b.ThroughAt, func(model.Message) bool { return true })
		case "own":
			homes := map[model.AccountID]bool{}
			return nil, s.chat.ApplyExpire(ctx, tx, rec, conv, b.ThroughAt, func(m model.Message) bool {
				mine, known := homes[m.AuthorID]
				if !known {
					acc, err := tx.Accounts().Get(ctx, m.AuthorID)
					mine = err == nil && s.homeOf(acc) == origin.ServerID
					homes[m.AuthorID] = mine
				}
				return mine
			})
		}
		return reject("invalid"), nil
	case model.EventPreviewAdded, model.EventPreviewRemoved:
		return a.preview()
	case model.EventReactionAdded, model.EventReactionRemoved:
		return a.reaction()
	case model.EventTaskChanged:
		var b struct {
			MessageID model.MessageID `json:"message_id"`
			ItemID    string          `json:"item_id"`
			Checked   bool            `json:"checked"`
			AccountID model.AccountID `json:"account_id"`
		}
		if rej := a.decode(&b); rej != nil {
			return rej, nil
		}
		if _, rej, err := a.member(b.AccountID); rej != nil || err != nil {
			return rej, err
		}
		m, rej, err := a.message(b.MessageID)
		if rej != nil || err != nil {
			return rej, err
		}
		items, err := tx.Checklists().Items(ctx, m.ID, false)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.ItemID == b.ItemID {
				return nil, s.chat.ApplyTick(ctx, tx, rec, m, b.ItemID, b.Checked, b.AccountID, in.At)
			}
		}
		return rejectQuiet("unknown_item"), nil
	case model.EventFormAnswered, model.EventFormAnswerWithdrawn:
		var b struct {
			MessageID model.MessageID `json:"message_id"`
			AccountID model.AccountID `json:"account_id"`
		}
		if rej := a.decode(&b); rej != nil {
			return rej, nil
		}
		if _, rej, err := a.member(b.AccountID); rej != nil || err != nil {
			return rej, err
		}
		return s.applyFormEvent(a, b.MessageID)
	case evFormRefused, model.EventFormOptionAdded, evFormTallied, model.EventFormClosed:
		var b struct {
			MessageID model.MessageID `json:"message_id"`
		}
		if rej := a.decode(&b); rej != nil {
			return rej, nil
		}
		m, rej, err := a.message(b.MessageID)
		if rej != nil || err != nil {
			return rej, err
		}
		if rej, err := a.authorHome(m); rej != nil || err != nil {
			return rej, err
		}
		return s.applyFormEvent(a, b.MessageID)
	case model.EventMemberJoined:
		return a.memberJoined()
	case evMemberRequested:
		return a.memberRequested()
	case evMemberAdmitted:
		return a.memberAdmitted()
	case evMemberDeclined:
		return a.memberDeclined()
	case evMemberRemoved:
		return a.memberRemoved()
	case model.EventMemberLeft:
		var b struct {
			AccountID model.AccountID `json:"account_id"`
			Reason    string          `json:"reason"`
		}
		if rej := a.decode(&b); rej != nil {
			return rej, nil
		}
		if _, rej, err := a.account(b.AccountID); rej != nil || err != nil {
			return rej, err
		}
		if b.Reason != "removed" && b.Reason != "account_removed" {
			b.Reason = "left"
		}
		return nil, s.chat.ApplyLeave(ctx, tx, rec, conv, b.AccountID, b.Reason)
	case evMemberInvited:
		var b struct {
			AccountID   model.AccountID `json:"account_id"`
			ByAccountID model.AccountID `json:"by_account_id"`
			// Server is the invited account's home server, for an owner
			// server that has never met the account.
			Server string `json:"server"`
		}
		if rej := a.decode(&b); rej != nil {
			return rej, nil
		}
		if st.Kind != store.StreamAdhoc {
			return reject("not_an_adhoc_stream"), nil
		}
		if _, rej, err := a.member(b.ByAccountID); rej != nil || err != nil {
			return rej, err
		}
		s.noteInvited(st.ID, b.AccountID)
		s.invitedHere(a, b.AccountID, b.ByAccountID, b.Server)
		return nil, nil
	case model.EventAccountUpdated:
		var b struct {
			Account wireProfile `json:"account"`
			Blobs   []wireBlob  `json:"blobs"`
		}
		if rej := a.decode(&b); rej != nil {
			return rej, nil
		}
		if rej, err := a.profile(b.Account, b.Blobs, true); rej != nil || err != nil {
			return rej, err
		}
		return nil, nil
	case model.EventConversationUpdated:
		var b struct {
			Name  *string `json:"name"`
			Topic *string `json:"topic"`
		}
		if rej := a.decode(&b); rej != nil {
			return rej, nil
		}
		if !isOwner {
			return reject("not_owner"), nil
		}
		if st.Kind != store.StreamChannel {
			return reject("not_a_channel_stream"), nil
		}
		if b.Name != nil {
			clean, ok := model.CleanChannelName(*b.Name)
			if !ok {
				return reject("invalid"), nil
			}
			b.Name = &clean
		}
		if b.Topic != nil && utf8.RuneCountInString(*b.Topic) > 1000 {
			return reject("invalid"), nil
		}
		if s.isSelf(origin.ServerID) && (b.Name == nil || *b.Name == conv.Name) && (b.Topic == nil || *b.Topic == conv.Topic) {
			return nil, nil // already so here
		}
		return nil, s.chat.ApplyConversation(ctx, tx, rec, conv.ID, b.Name, b.Topic)
	case evCallStarted, evCallJoined, evCallLeft, evCallEnded:
		return s.applyCallEvent(a)
	case evServersChanged:
		return a.serversChanged(isOwner)
	case evOwnerChanged:
		return a.ownerChanged(isOwner)
	case evLimitsChanged:
		var b struct {
			MaxBlobBytes int64 `json:"max_blob_bytes"`
		}
		if rej := a.decode(&b); rej != nil {
			return rej, nil
		}
		if !isOwner {
			return reject("not_owner"), nil
		}
		if b.MaxBlobBytes <= 0 {
			return reject("invalid"), nil
		}
		if _, err := tx.Streams().Update(ctx, st.ID, func(x *store.Stream) error {
			x.MaxBlobBytes = b.MaxBlobBytes
			return nil
		}); err != nil {
			return nil, err
		}
		return nil, s.chat.ApplyConversation(ctx, tx, rec, conv.ID, nil, nil)
	case evMoved:
		return a.moved(isOwner)
	case evStreamClosed:
		if !isOwner {
			return reject("not_owner"), nil
		}
		if !s.isSelf(origin.ServerID) {
			rec.After(func() { s.detachStream(s.ctx, st, store.StreamRemoved) })
		}
		return nil, nil
	}
	// A type this server does not act on: nothing is stored, and a newer
	// server's events do not trouble an older one.
	return nil, nil
}

// fileRows turns the files and blobs of an event into rows in state
// `arriving`, or `too_large` for one over this server's or the stream's
// limit, which is then never fetched.
func (a *apply) fileRows(author model.AccountID, files []wireFile, cards []model.Card, blobs []wireBlob) ([]model.File, *rejection, error) {
	limit, err := store.SettingIntOr(a.ctx, a.tx, store.SettingFilesMaxBytes, model.DefaultFileSize)
	if err != nil {
		return nil, nil, err
	}
	state := func(size int64) string {
		if a.s.holder {
			// The hub keeps every file of the channels it owns, whatever its
			// size: what an instance may send it is bounded by the hub's
			// limit on file bytes an hour.
			return "arriving"
		}
		if size > limit || (a.st.MaxBlobBytes > 0 && size > a.st.MaxBlobBytes) {
			return "too_large"
		}
		return "arriving"
	}
	var out []model.File
	for i, f := range files {
		if !model.ValidID(string(f.ID)) || len(f.SHA256) != 32 || f.Size <= 0 {
			return nil, reject("invalid"), nil
		}
		out = append(out, model.File{ID: f.ID, UploaderID: author, Purpose: "attachment", Position: i, Name: f.Name, Caption: f.Caption,
			Size: f.Size, MediaType: "application/octet-stream", Kind: model.FileOther, SHA256: f.SHA256, State: state(f.Size), CreatedAt: a.in.At})
	}
	byID := map[string]wireBlob{}
	for _, b := range blobs {
		byID[b.ID] = b
	}
	for _, c := range cards {
		if c.ImageFileID == "" {
			continue
		}
		b, ok := byID[string(c.ImageFileID)]
		if !ok || len(b.SHA256) != 32 || b.Size <= 0 {
			return nil, reject("invalid"), nil
		}
		out = append(out, model.File{ID: c.ImageFileID, UploaderID: author, Purpose: "card", Name: "image", Size: b.Size,
			MediaType: "application/octet-stream", Kind: model.FileOther, SHA256: b.SHA256, State: state(b.Size), CreatedAt: a.in.At})
	}
	return out, nil, nil
}

// library puts the image of an emoji or an avatar from another server in
// the library conversation, in state `arriving`, unless it is held already.
func (a *apply) library(b wireBlob, owner model.AccountID) (model.FileID, error) {
	id := model.FileID(b.ID)
	if _, err := a.tx.Files().Get(a.ctx, id); err == nil {
		return id, nil
	} else if !errors.Is(err, model.ErrNotFound) {
		return id, err
	}
	lib, err := a.tx.Conversations().EnsureLibrary(a.ctx)
	if err != nil {
		return id, err
	}
	f := model.File{ID: id, ConversationID: lib.ID, UploaderID: owner, Purpose: b.Purpose, Name: b.Purpose, Size: b.Size,
		MediaType: "application/octet-stream", Kind: model.FileOther, SHA256: b.SHA256, State: "arriving", CreatedAt: a.in.At}
	if b.Name != "" {
		f.Name = b.Name
	}
	return id, a.tx.Files().Insert(a.ctx, &f)
}

// emoji stores the custom emoji an event lists, under the server they came
// from, and returns nothing: the entities and reactions already name them.
func (a *apply) emoji(blobs []wireBlob, owner model.AccountID) error {
	if a.s.isSelf(a.origin.ServerID) {
		return nil
	}
	for _, b := range blobs {
		if b.Purpose != "emoji" || !model.ValidID(b.ID) || !model.ValidEmojiName(b.Name) || len(b.SHA256) != 32 || b.Size <= 0 || b.Size > 1<<20 {
			continue
		}
		if _, err := a.tx.Emoji().Get(a.ctx, model.EmojiID(b.ID)); err == nil {
			continue
		}
		file, err := a.library(b, owner)
		if err != nil {
			return err
		}
		if err := a.tx.Streams().PutRemoteEmoji(a.ctx, model.Emoji{ID: model.EmojiID(b.ID), Name: b.Name, FileID: file, CreatedByID: owner,
			CreatedAt: a.in.At}, a.origin.ServerID); err != nil {
			return err
		}
	}
	return nil
}

// fetchAfter starts fetching an event's files once it has been applied.
func (a *apply) fetchAfter() {
	a.rec.After(func() { go a.s.fetchArriving(a.s.ctx) })
}

// toMessage turns a wire message into the rows chat stores.
func (a *apply) toMessage(w wireMessage, blobs []wireBlob) (chat.RemoteMessage, *rejection, error) {
	var rm chat.RemoteMessage
	if !model.ValidID(string(w.ID)) || utf8.RuneCountInString(w.Text) > model.MaxMessageText || len(w.Files) > model.MaxFilesPerMessage ||
		len(w.Cards) > model.MaxCardsPerMessage || w.CreatedAt.IsZero() {
		return rm, reject("invalid"), nil
	}
	if w.Text == "" && len(w.Files) == 0 && len(w.Cards) == 0 && w.System == nil && w.Form == nil && w.Quote == nil && w.Sealed == nil {
		return rm, reject("invalid"), nil
	}
	if w.Form != nil {
		// Only polls work across servers, and a group is one server's.
		var form wireForm
		if json.Unmarshal(w.Form, &form) != nil || form.Action.Type != string(model.FormTally) || form.AnswererGroupID != "" {
			return rm, reject("form_not_a_poll"), nil
		}
	}
	form, rej := formFromWire(w.Form)
	if rej != nil {
		return rm, rej, nil
	}
	for i := range w.Cards {
		// A shield that names nothing a draw may have is dropped; the card
		// still arrives (docs/DESIGN-FAIR.md section 13).
		if f := w.Cards[i].FairMark; f != nil && !f.Valid() {
			w.Cards[i].FairMark = nil
		}
	}
	m := model.Message{ID: w.ID, AuthorID: w.AuthorID, ParentID: w.ParentID, Quote: w.Quote, Text: w.Text, Cards: w.Cards,
		Fallback: w.Fallback, Via: w.Via, System: w.System, Action: w.Action && w.Text != "", CreatedAt: w.CreatedAt}
	if w.Sealed == nil {
		// A value that fails its kind's check falls back a step, to its alt
		// or to the text; the message still arrives. A sealed message's
		// values travel inside its payload, for the reader's device.
		m.View = model.CheckValue(w.View)
	}
	for _, e := range w.Entities {
		ent := e.Entity
		switch ent.Type {
		case model.EntityGroup:
			continue // groups do not cross servers
		case model.EntityConversation:
			if e.StreamID != a.st.ID {
				continue
			}
			ent.ConversationID = a.conv.ID // each receiver puts its own identifier back
		case model.EntityInstant:
			// Carried as the author's server resolved it (ruling R69);
			// one that does not hold up is drawn as its text.
			if _, ok := instant.ParseTime(ent.Time); !ok || !instant.ValidZone(ent.Zone) ||
				ent.Precision != instant.Minute && ent.Precision != instant.Second {
				continue
			}
		}
		m.Entities = append(m.Entities, ent)
	}
	m.Entities = model.CheckObjects(m.Entities)
	if w.Sealed == nil {
		// The proof of the message's draw, checked; what took chance from
		// a draw that fails is shown as its text (fair.go).
		var ok bool
		if m.Fair, ok = a.fairOf(w); !ok {
			dropDrawn(&m)
		}
		if m.Fair != nil {
			// The client_id the draw was made by, kept with it so that
			// anyone rebuilds its alpha from the message.
			m.ClientID = w.ClientID
		}
	}
	files, rej, err := a.fileRows(w.AuthorID, w.Files, w.Cards, blobs)
	if rej != nil || err != nil {
		return rm, rej, err
	}
	if err := a.emoji(blobs, w.AuthorID); err != nil {
		return rm, nil, err
	}
	if m.Via != nil && m.Via.IconFileID != "" {
		for _, b := range blobs {
			if b.ID == string(m.Via.IconFileID) && len(b.SHA256) == 32 && b.Size > 0 {
				b.Purpose = "avatar"
				if _, err := a.library(b, w.AuthorID); err != nil {
					return rm, nil, err
				}
			}
		}
	}
	return chat.RemoteMessage{Message: m, Files: files, Form: form}, nil, nil
}

func (a *apply) messageCreated() (*rejection, error) {
	var b struct {
		Message wireMessage `json:"message"`
		Blobs   []wireBlob  `json:"blobs"`
	}
	if rej := a.decode(&b); rej != nil {
		return rej, nil
	}
	if _, rej, err := a.member(b.Message.AuthorID); rej != nil || err != nil {
		return rej, err
	}
	if _, err := a.tx.Messages().Get(a.ctx, b.Message.ID); err == nil {
		// Held already: it came with history, or it is this server's own.
		return rejectQuiet("message_exists"), nil
	} else if !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	if b.Message.ParentID != "" {
		p, rej, err := a.message(b.Message.ParentID)
		if rej != nil || err != nil {
			return rej, err
		}
		if p.ParentID != "" {
			return reject("invalid"), nil
		}
	}
	if rej := a.s.personalAllowed(a, b.Message.AuthorID); rej != nil {
		return rej, nil
	}
	rm, rej, err := a.toMessage(b.Message, b.Blobs)
	if rej != nil || err != nil {
		return rej, err
	}
	a.fetchAfter()
	return nil, a.store(rm)
}

// store stores a message from another server where its event's stamp says,
// on this server as on every other. If messages ordered after it have
// already been delivered here it is late: it still takes its place, and
// counts as unread like anything new.
func (a *apply) store(rm chat.RemoteMessage) error {
	rm.Message.Order = a.order
	rm.Message.Late = a.order < a.st.TopOrd
	if err := a.s.chat.ApplyMessage(a.ctx, a.tx, a.rec, a.conv, rm); err != nil {
		return err
	}
	// The stream's row is written after the conversation has given the
	// message its seq, as a local post writes it.
	return a.tx.Streams().RaiseTop(a.ctx, a.st.ID, a.order)
}

func (a *apply) messageEdited() (*rejection, error) {
	var b struct {
		Message wireMessage `json:"message"`
		Blobs   []wireBlob  `json:"blobs"`
	}
	if rej := a.decode(&b); rej != nil {
		return rej, nil
	}
	m, rej, err := a.message(b.Message.ID)
	if rej != nil || err != nil {
		return rej, err
	}
	if rej, err := a.authorHome(m); rej != nil || err != nil {
		return rej, err
	}
	if m.Deleted() {
		return reject("deleted"), nil
	}
	if m.EditOrder != "" && a.order <= m.EditOrder {
		// Of two edits to one message the later stamp stands, whichever
		// arrives first. This one is the earlier.
		return rejectQuiet("superseded"), nil
	}
	b.Message.AuthorID, b.Message.CreatedAt = m.AuthorID, m.CreatedAt
	rm, rej, err := a.toMessage(b.Message, b.Blobs)
	if rej != nil || err != nil {
		return rej, err
	}
	at := b.Message.EditedAt
	keep := !at.IsZero() // no edited_at: a quick correction, which keeps no earlier version
	if !keep {
		at = a.in.At
	}
	a.fetchAfter()
	return nil, a.s.chat.ApplyEdit(a.ctx, a.tx, a.rec, a.conv, rm, keep, at, a.order)
}

func (a *apply) messageDeleted(isOwner bool) (*rejection, error) {
	var b struct {
		MessageID   model.MessageID `json:"message_id"`
		DeletedByID model.AccountID `json:"deleted_by_id"`
		DeletedAt   model.Time      `json:"deleted_at"`
		RemovedByID model.AccountID `json:"removed_by_id"`
		RemovedAt   model.Time      `json:"removed_at"`
		RemovedBy   *wireProfile    `json:"removed_by"`
		Blobs       []wireBlob      `json:"blobs"`
	}
	if rej := a.decode(&b); rej != nil {
		return rej, nil
	}
	by, at := b.DeletedByID, b.DeletedAt
	if a.in.Type == evMessageRemoved {
		by, at = b.RemovedByID, b.RemovedAt
		switch {
		case hubOwned(a.st):
			// A channel a hub owns: whoever removes a message for everyone
			// is on the manager or the moderator list the hub signed, as
			// an account of the instance that signed this envelope.
			// Nothing else gives that authority: not owning an instance,
			// not a level anywhere.
			if !a.s.keeps(a.ctx, a.tx, a.st, by, a.origin.Name) {
				return &rejection{reason: "not_moderator", detail: map[string]any{"account_id": by, "origin": a.origin.Name}}, nil
			}
			if b.RemovedBy != nil && b.RemovedBy.ID == by {
				// A manager or moderator need not be a member, so this
				// server may never have met the account.
				if rej, err := a.profile(*b.RemovedBy, b.Blobs, false); rej != nil || err != nil {
					return rej, err
				}
			}
		case !isOwner:
			// Only the owner server can remove a message for everyone.
			return reject("not_owner"), nil
		}
	}
	m, rej, err := a.message(b.MessageID)
	if rej != nil || err != nil {
		return rej, err
	}
	if a.in.Type == model.EventMessageDeleted {
		if rej, err := a.authorHome(m); rej != nil || err != nil {
			return rej, err
		}
	}
	// Whoever did it is an account of the server that says so.
	if _, rej, err := a.account(by); rej != nil || err != nil {
		return rej, err
	}
	if m.Deleted() {
		return nil, nil
	}
	if at.IsZero() {
		at = a.in.At
	}
	return nil, a.s.chat.ApplyDelete(a.ctx, a.tx, a.rec, a.conv, m.ID, by, at)
}

func (a *apply) preview() (*rejection, error) {
	var b struct {
		MessageID model.MessageID `json:"message_id"`
		Cards     []model.Card    `json:"cards"`
		Blobs     []wireBlob      `json:"blobs"`
		URL       string          `json:"url"`
	}
	if rej := a.decode(&b); rej != nil {
		return rej, nil
	}
	m, rej, err := a.message(b.MessageID)
	if rej != nil || err != nil {
		return rej, err
	}
	if rej, err := a.authorHome(m); rej != nil || err != nil {
		return rej, err
	}
	if len(b.Cards) > 3 {
		return reject("too_many_previews"), nil
	}
	var cards []model.Card
	for _, c := range m.Cards {
		// preview.added carries the message's link-preview cards and
		// replaces the ones held; preview.removed takes one away.
		if c.PreviewURL == "" || (a.in.Type == model.EventPreviewRemoved && c.PreviewURL != b.URL) {
			cards = append(cards, c)
		}
	}
	var files []model.File
	if a.in.Type == model.EventPreviewAdded {
		for _, c := range b.Cards {
			if c.PreviewURL == "" {
				return reject("invalid"), nil
			}
		}
		cards = append(cards, b.Cards...)
		if len(cards) > model.MaxCardsPerMessage+3 {
			return reject("invalid"), nil
		}
		if files, rej, err = a.fileRows(m.AuthorID, nil, b.Cards, b.Blobs); rej != nil || err != nil {
			return rej, err
		}
		a.fetchAfter()
	}
	return nil, a.s.chat.ApplyCards(a.ctx, a.tx, a.rec, a.conv, m.ID, cards, files, a.in.Type)
}

func (a *apply) reaction() (*rejection, error) {
	var b struct {
		MessageID model.MessageID `json:"message_id"`
		AccountID model.AccountID `json:"account_id"`
		Emoji     json.RawMessage `json:"emoji"`
		Blobs     []wireBlob      `json:"blobs"`
	}
	if rej := a.decode(&b); rej != nil {
		return rej, nil
	}
	if _, rej, err := a.member(b.AccountID); rej != nil || err != nil {
		return rej, err
	}
	m, rej, err := a.message(b.MessageID)
	if rej != nil || err != nil {
		return rej, err
	}
	if m.Deleted() {
		return rejectQuiet("deleted"), nil
	}
	var emoji string
	if json.Unmarshal(b.Emoji, &emoji) != nil {
		// A custom one: {"id", "name", "server"}. Stored as :name: if the
		// emoji is this server's own and as :name@server.name: otherwise.
		var custom struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Server string `json:"server"`
		}
		if json.Unmarshal(b.Emoji, &custom) != nil || !model.ValidEmojiName(custom.Name) || custom.Server == "" {
			return reject("invalid"), nil
		}
		emoji = ":" + custom.Name + ":"
		if custom.Server != a.s.id.Name {
			emoji = ":" + custom.Name + "@" + custom.Server + ":"
			if err := a.emoji(b.Blobs, b.AccountID); err != nil {
				return nil, err
			}
			a.fetchAfter()
		}
	} else if emoji == "" || utf8.RuneCountInString(emoji) > 32 || strings.HasPrefix(emoji, ":") {
		return reject("invalid"), nil
	}
	return nil, a.s.chat.ApplyReaction(a.ctx, a.tx, a.rec, m, b.AccountID, emoji, a.in.Type == model.EventReactionAdded, a.in.At)
}

// profile creates or updates the shadow of an account from its profile. The
// origin must be the server named in the profile, and the home of the
// account if it is already known here.
func (a *apply) profile(p wireProfile, blobs []wireBlob, tell bool) (*rejection, error) {
	if !model.ValidID(string(p.ID)) || p.Username == "" || utf8.RuneCountInString(p.DisplayName) > 200 {
		return reject("invalid"), nil
	}
	if p.Server != a.origin.Name {
		return &rejection{reason: "forged_author", forged: true, detail: map[string]any{"account_id": p.ID, "home": p.Server, "type": a.in.Type}}, nil
	}
	if a.s.isSelf(a.origin.ServerID) {
		return nil, nil // this server's own account; nothing to shadow
	}
	if existing, err := a.tx.Accounts().Get(a.ctx, p.ID); err == nil {
		if a.s.homeOf(existing) != a.origin.ServerID {
			return &rejection{reason: "forged_author", forged: true, detail: map[string]any{"account_id": p.ID, "type": a.in.Type}}, nil
		}
	} else if !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	acc := model.Account{ID: p.ID, ServerID: a.origin.ServerID, Username: p.Username, DisplayName: p.DisplayName, Title: p.Title,
		Pronouns: p.Pronouns, TimeZone: p.TimeZone, ProfileVersion: p.Version, CreatedAt: a.in.At}
	if acc.DisplayName == "" {
		acc.DisplayName = p.Username
	}
	if p.Removed {
		acc.RemovedAt = a.in.At
	}
	// The row is made first: the avatar's file row names it as its owner.
	changed, err := a.tx.Streams().PutShadow(a.ctx, &acc)
	if err != nil {
		if errors.Is(err, model.ErrConflict) || errors.Is(err, model.ErrNameTaken) {
			return reject("invalid"), nil
		}
		return nil, err
	}
	if changed && p.AvatarFileID != "" {
		for _, b := range blobs {
			if b.ID == string(p.AvatarFileID) && b.Purpose == "avatar" && len(b.SHA256) == 32 && b.Size > 0 && b.Size <= 8<<20 {
				if _, err := a.library(b, p.ID); err != nil {
					return nil, err
				}
				if _, err := a.tx.Accounts().Update(a.ctx, p.ID, func(x *model.Account) error {
					x.AvatarFileID = p.AvatarFileID
					return nil
				}); err != nil {
					return nil, err
				}
				a.fetchAfter()
			}
		}
	}
	if changed && tell {
		conv := a.conv.ID
		a.rec.After(func() {
			if shown, err := a.s.store.Accounts().Get(a.s.ctx, p.ID); err == nil {
				a.s.chat.TellLocal(a.s.ctx, conv, model.EventAccountUpdated, map[string]any{"account": shown})
			}
		})
	}
	return nil, nil
}

func (a *apply) memberJoined() (*rejection, error) {
	var b struct {
		Account  wireProfile `json:"account"`
		Blobs    []wireBlob  `json:"blobs"`
		JoinedAt model.Time  `json:"joined_at"`
	}
	if rej := a.decode(&b); rej != nil {
		return rej, nil
	}
	switch a.st.Kind {
	case store.StreamAdhoc:
		// Each join follows a member.invited naming that account, unless
		// the account came with the offer that made the channel.
		if ok, err := a.invited(b.Account.ID); err != nil {
			return nil, err
		} else if !ok {
			return reject("not_invited"), nil
		}
	case store.StreamSealed:
		// Never anyone but the chat's two. They are members from the
		// moment each server made its copy, so their own server saying
		// so changes nothing.
		if _, found, err := a.tx.Members().Get(a.ctx, a.conv.ID, b.Account.ID); err != nil {
			return nil, err
		} else if !found {
			n, err := a.tx.Members().Count(a.ctx, a.conv.ID)
			if err != nil {
				return nil, err
			}
			if n >= 2 {
				return reject("sealed_chat_is_full"), nil
			}
		}
	}
	if rej, err := a.profile(b.Account, b.Blobs, false); rej != nil || err != nil {
		return rej, err
	}
	if a.s.isSelf(a.origin.ServerID) {
		// This server's own member. A member here already, and then
		// ApplyJoin changes nothing.
		if _, rej, err := a.account(b.Account.ID); rej != nil || err != nil {
			return rej, err
		}
	}
	at := b.JoinedAt
	if at.IsZero() {
		at = a.in.At
	}
	return nil, a.s.chat.ApplyJoin(a.ctx, a.tx, a.rec, a.conv, b.Account.ID, at)
}

func (a *apply) serversChanged(isOwner bool) (*rejection, error) {
	var b struct {
		Version int64    `json:"version"`
		Servers []string `json:"servers"`
		Joined  []string `json:"joined"`
		Left    []string `json:"left"`
		Epoch   int64    `json:"epoch"`
	}
	if rej := a.decode(&b); rej != nil {
		return rej, nil
	}
	s, st := a.s, a.st
	// From the owner; the next version (or the one its control frame
	// already brought); a higher epoch; encrypted under that epoch. In a
	// channel a hub owns no server sends it: the home hub begins every
	// epoch itself, and the server list is its record's.
	switch {
	case hubOwned(st):
		return reject("hub_channel"), nil
	case !isOwner:
		return reject("not_owner"), nil
	case b.Version != st.ServersVersion && b.Version != st.ServersVersion+1:
		return reject("version_out_of_step"), nil
	case b.Epoch <= st.KeyEpoch && !(st.KeyEpoch == 1 && b.Epoch == 1 && st.ServersVersion <= 1):
		return reject("epoch_not_higher"), nil
	case a.env.KeyEpoch != b.Epoch:
		return reject("wrong_epoch"), nil
	}
	if !s.isSelf(a.origin.ServerID) {
		// The list is the owner's to say, by version: the newer one
		// stands, whichever road it arrived by first.
		if err := s.putList(a.ctx, a.tx, st.ID, b.Servers, b.Version); err != nil {
			return nil, err
		}
	}
	k, err := s.keyOf(a.ctx, a.tx, st.ID, b.Epoch)
	if err != nil {
		return nil, err
	}
	if err := s.putKey(a.ctx, a.tx, st.ID, b.Epoch, k.key, a.env.At); err != nil {
		return nil, err
	}
	if _, err := a.tx.Streams().Update(a.ctx, st.ID, func(x *store.Stream) error {
		x.KeyEpoch = b.Epoch
		return nil
	}); err != nil {
		return nil, err
	}
	// Every account of each server that left has left the conversation
	// with it: its home server is no longer there to say so itself.
	for _, name := range b.Left {
		sv, err := a.tx.Peers().ServerByName(a.ctx, name)
		if err != nil || s.isSelf(sv.ID) {
			continue
		}
		ids, err := a.tx.Members().IDs(a.ctx, a.conv.ID)
		if err != nil {
			return nil, err
		}
		accounts, err := a.tx.Accounts().GetMany(a.ctx, ids)
		if err != nil {
			return nil, err
		}
		var gone []model.AccountID
		for _, acc := range accounts {
			if acc.ServerID == sv.ID {
				gone = append(gone, acc.ID)
			}
		}
		if err := s.chat.DropMembers(a.ctx, a.tx, a.conv.ID, gone); err != nil {
			return nil, err
		}
	}
	// What waited here for the epoch to start can go now.
	a.rec.After(func() { go s.kickStream(s.ctx, st.ID, true) })
	if len(b.Joined) > 0 {
		joined := st.ID
		names := b.Joined
		a.rec.After(func() {
			go func() {
				s.deafEarsOnJoin(s.ctx, joined, names)
				s.sendSnapshot(s.ctx, joined)
			}()
		})
		for _, name := range b.Joined {
			if name == s.id.Name {
				// This server has just been let in. What follows waits
				// until the history that says who is here has come.
				rt := s.rt(st.ID)
				rt.mu.Lock()
				if !rt.historyHere && rt.historyWait == nil {
					rt.historyWait = make(chan struct{})
				}
				rt.mu.Unlock()
			}
		}
	}
	if _, err := a.tx.Conversations().Get(a.ctx, a.conv.ID); err != nil {
		return nil, nil // the last member went with the server that left
	}
	return nil, s.chat.ApplyConversation(a.ctx, a.tx, a.rec, a.conv.ID, nil, nil)
}

func (a *apply) ownerChanged(isOwner bool) (*rejection, error) {
	var b struct {
		NewOwner string `json:"new_owner"`
		Version  int64  `json:"version"`
	}
	if rej := a.decode(&b); rej != nil {
		return rej, nil
	}
	s, st := a.s, a.st
	if !isOwner {
		return reject("not_owner"), nil
	}
	active, _, err := s.activeServers(a.ctx, a.tx, st.ID)
	if err != nil {
		return nil, err
	}
	var next *store.StreamServer
	for i, ss := range active {
		if ss.Name == b.NewOwner {
			next = &active[i]
		}
	}
	if next == nil || next.ServerID == a.origin.ServerID {
		return reject("new_owner_not_in_stream"), nil
	}
	mine := s.isSelf(next.ServerID)
	peer, err := a.tx.Peers().Get(a.ctx, st.PeerID)
	if err != nil {
		return nil, err
	}
	if _, err := a.tx.Streams().Update(a.ctx, st.ID, func(x *store.Stream) error {
		x.OwnerServerID = next.ServerID
		if mine {
			x.OwnerServerID = ""
		}
		if peer.Kind == wire.KindServer {
			// On a direct link the new owner also becomes the
			// sequencer, numbering from the seq after this event.
			x.Sequencer = store.SequencerOwner
			if mine {
				x.Sequencer = store.SequencerSelf
			}
		}
		if b.Version > x.ServersVersion {
			x.ServersVersion = b.Version
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if mine {
		// The new owner starts a new key epoch.
		id := st.ID
		a.rec.After(func() {
			go func() {
				cur, err := s.store.Streams().Get(s.ctx, id)
				if err != nil {
					return
				}
				epoch, err := s.newEpoch(s.ctx, cur)
				if err != nil {
					return
				}
				_, names, _ := s.activeServers(s.ctx, s.store, id)
				version := cur.ServersVersion + 1
				s.store.Streams().Update(s.ctx, id, func(x *store.Stream) error {
					x.ServersVersion = version
					return nil
				})
				s.originate(s.ctx, cur, evServersChanged, map[string]any{"version": version, "servers": names, "joined": []string{},
					"left": []string{}, "epoch": epoch}, epoch)
			}()
		})
	}
	return nil, s.chat.ApplyConversation(a.ctx, a.tx, a.rec, a.conv.ID, nil, nil)
}
