package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Stream is a row of `streams`: one conversation shared over one peer.
type Stream struct {
	ID             model.StreamID
	PeerID         model.PeerID
	ConversationID model.ConversationID // empty until accepted locally
	OwnerServerID  model.ServerID       // empty: this server owns it
	// Sequencer says who is the stream's time reference and keeps its log:
	// self | owner | hub. The name is from when that party numbered events;
	// nobody does now.
	Sequencer string
	Kind      string // channel | adhoc | sealed
	State     string // offered | active | left | removed
	KeyEpoch  int64
	// LastSeq is a delivery cursor: for a stream this server is the
	// reference of, the last position written to its log; otherwise the
	// log position of the last envelope received from whoever delivers.
	LastSeq        int64
	FirstSeq       int64 // not used
	ServersVersion int64
	MaxBlobBytes   int64
	HistoryDays    int
	OfferedName    string
	OfferedAt      model.Time
	AcceptedBy     model.AccountID
	CreatedAt      model.Time
	// HomeHub is, for a stream through a hub, the name of the hub that is
	// its home and its time reference.
	HomeHub string
	// MaxAt is the highest `at` of any event this server has applied or
	// made in the stream: its own next event is stamped above it.
	MaxAt int64
	// TopOrd is the highest order string of any message delivered here. A
	// message that arrives below it is late.
	TopOrd string
}

// The stream states, sequencers and kinds.
const (
	StreamOffered = "offered"
	StreamActive  = "active"
	StreamLeft    = "left"
	StreamRemoved = "removed"

	SequencerSelf  = "self"
	SequencerOwner = "owner"
	SequencerHub   = "hub"

	StreamChannel = "channel"
	StreamAdhoc   = "adhoc"
	StreamSealed  = "sealed"

	StreamServerInvited = "invited"
	StreamServerActive  = "active"
	StreamServerRemoved = "removed"
)

// StreamMove is a row of `stream_moves`: one attempt to move a stream from
// a direct link onto a hub, as this server knows it. Both servers of the
// stream keep one, under the identifier the owner server made.
type StreamMove struct {
	ID       string
	StreamID model.StreamID
	// FromPeerID is the direct link the stream ran over; HubPeerID is this
	// server's own link to the hub named, empty if it has none.
	FromPeerID     model.PeerID
	HubPeerID      model.PeerID
	HubName        string
	HubFingerprint string
	State          string // proposed | accepted | committed | done | declined | abandoned
	Reason         string
	// CommitSeq is, on the owner server, the position of stream.moved in
	// the direct log, and CommitEventID that event's origin event id.
	CommitSeq     int64
	CommitEventID string
	// Accept and AcceptSig are the other server's signed agreement;
	// Commit and CommitSig the owner server's signed commit, which exists
	// from the transaction that commits the move.
	Accept, AcceptSig []byte
	Commit, CommitSig []byte
	ProposedBy        model.AccountID
	AnsweredBy        model.AccountID
	CreatedAt         model.Time
	ExpiresAt         model.Time
	UpdatedAt         model.Time
}

// ChannelRecord is a row of `stream_channels`: the newest record a hub has
// signed for a channel it owns, as this server received it.
type ChannelRecord struct {
	StreamID model.StreamID
	Version  int64
	HomeHub  string
	Record   []byte // the signed bytes
	Sig      []byte
	// Makers is, as JSON, key epoch to the server the hub named to make
	// that epoch's key.
	Makers []byte
	// Moderators are the accounts of this server on the record's moderator
	// list. The link package works them out when it stores a record it has
	// verified; internal/perm reads them.
	Moderators []model.AccountID
}

// The states of a move.
const (
	MoveProposed  = "proposed"
	MoveAccepted  = "accepted"
	MoveCommitted = "committed"
	MoveDone      = "done"
	MoveDeclined  = "declined"
	MoveAbandoned = "abandoned"
)

// Open reports whether the move is still in progress: neither done,
// declined nor abandoned.
func (m StreamMove) Open() bool {
	return m.State != MoveDone && m.State != MoveDeclined && m.State != MoveAbandoned
}

// Owned reports whether this server owns the stream.
func (s Stream) Owned() bool { return s.OwnerServerID == "" }

// StreamServer is a row of `stream_servers` with the server's name.
type StreamServer struct {
	StreamID model.StreamID
	ServerID model.ServerID
	Name     string
	State    string // invited | active | removed
	// LastN is the counter up to which this server holds everything that
	// server made in the stream, as far as whoever delivers has it. On the
	// row for this server itself it is the counter of the last event this
	// server stamped. LastAt is the `at` of that event.
	LastN  int64
	LastAt int64
	// LoggedN is, in a stream this server is the time reference of, the
	// counter of that server's last envelope in the log.
	LoggedN int64
	// JoinedAt is when the server joined; while it is only invited, when
	// the offer was made.
	JoinedAt model.Time
}

// StreamKey is a row of `stream_keys`. Sealed is the key as the link
// package sealed it for the database; the store cannot read it.
type StreamKey struct {
	Epoch     int64
	Sealed    []byte
	CreatedAt model.Time
}

// OutboxRow is one of this server's events waiting for the stream's time
// reference to acknowledge it. ID is the origin event id.
type OutboxRow struct {
	ID        string
	StreamID  model.StreamID
	Envelope  []byte
	CreatedAt model.Time
	Attempts  int
	NextTryAt model.Time
}

// LogRow is one envelope in the log of a stream this server is the time
// reference of. Seq is its position in the log, which AppendLog gives it: a
// delivery cursor, not an order. Origin and N are the server that made the
// event and its counter; Prev is the counter of the same origin's row before
// this one.
type LogRow struct {
	Seq           int64
	Origin        string
	N             int64
	Prev          int64
	OriginEventID string
	Envelope      []byte
	At            model.Time
}

// ArrivingFile is a file row whose bytes have not come yet, with the stream
// it belongs to and the home server of whoever posted it.
type ArrivingFile struct {
	File     model.File
	StreamID model.StreamID
	ServerID model.ServerID
}

// Streams is the `streams`, `stream_servers`, `stream_keys`, `outbox`,
// `stream_seen` and `stream_log` tables, and the few changes to other
// tables that only a shared conversation needs: where a message is shown,
// shadow accounts, files that are still arriving and emoji from other
// servers.
type Streams interface {
	// Create stores a stream. A zero CreatedAt is filled in.
	// model.ErrAlreadyExists if the identifier is taken.
	Create(ctx context.Context, s *Stream) error
	// Get returns one stream, or model.ErrNotFound.
	Get(ctx context.Context, id model.StreamID) (Stream, error)
	// ByConversation returns the stream a conversation is in while that
	// stream is active, or model.ErrNotFound.
	ByConversation(ctx context.Context, conv model.ConversationID) (Stream, error)
	// List returns streams, oldest first; state "" means every state.
	List(ctx context.Context, state string) ([]Stream, error)
	// Update changes a stream under a row lock, as Peers.Update does.
	Update(ctx context.Context, id model.StreamID, fn func(s *Stream) error) (Stream, error)
	// Delete removes a stream and everything that hangs from it: an offer
	// that was declined or lapsed.
	Delete(ctx context.Context, id model.StreamID) error
	// Detach deletes the stream's outbox, stream_seen and stream_log rows,
	// as leaving or closing does. Keys are kept so the copy can be read.
	Detach(ctx context.Context, id model.StreamID) error

	// PutChannel stores a hub-owned channel's record, replacing the one
	// held.
	PutChannel(ctx context.Context, c ChannelRecord) error
	// Channel returns the record held for a stream, or model.ErrNotFound.
	Channel(ctx context.Context, id model.StreamID) (ChannelRecord, error)
	// DeleteChannel forgets the record held for a stream: a move onto a
	// hub that was abandoned after the hub had prepared the channel.
	DeleteChannel(ctx context.Context, id model.StreamID) error

	// PutMove stores a move, replacing the row with the same ID. A zero
	// CreatedAt is filled in. Changes of state belong in a transaction
	// that has locked the stream's row with Update, which is what keeps
	// two of them apart.
	PutMove(ctx context.Context, m StreamMove) error
	// Move returns one move, or model.ErrNotFound.
	Move(ctx context.Context, id string) (StreamMove, error)
	// Moves returns the moves of one stream, or of every stream when
	// stream is empty, newest first.
	Moves(ctx context.Context, stream model.StreamID) ([]StreamMove, error)

	// PutServer adds a server to a stream's list or changes its state and
	// JoinedAt. LastN is kept if the row exists.
	PutServer(ctx context.Context, ss StreamServer) error
	// Servers returns the stream's server list, by name, whatever state
	// each is in.
	Servers(ctx context.Context, id model.StreamID) ([]StreamServer, error)
	// SetLastN sets the counter held from a server outright: a server that
	// joins starts from where the log stands.
	SetLastN(ctx context.Context, id model.StreamID, server model.ServerID, n int64) error
	// NextN adds one to a server's counter and returns the new value.
	NextN(ctx context.Context, id model.StreamID, server model.ServerID) (int64, error)
	// Stamp gives the next event this server makes in a stream its place
	// (ruling R57): n is one more than the server's counter, and at is now,
	// the reference clock as this server reads it in milliseconds, unless
	// the stream's max_at has reached that, in which case one more than
	// max_at. Both are written back, so an event is always stamped after
	// everything this server has applied in the stream and after its own
	// last. It locks the stream's row until the transaction ends.
	Stamp(ctx context.Context, id model.StreamID, server model.ServerID, now int64) (at, n int64, err error)
	// Applied records that an event of a server with this counter and `at`
	// is held: the server's last_n and last_at move up to them if they are
	// higher, and the stream's max_at to at.
	Applied(ctx context.Context, id model.StreamID, server model.ServerID, n, at int64) error
	// Acked records that the time reference acknowledged one of this
	// server's own events stamped at: acked_at moves up to it.
	Acked(ctx context.Context, id model.StreamID, at int64) error
	// Rewind is what stamping again after a refusal begins with. The
	// stamps this server gave since the reference last acknowledged one
	// are void, so max_at goes back to the highest `at` that stands: the
	// last acknowledged, or the last applied from any other server.
	Rewind(ctx context.Context, id model.StreamID, self model.ServerID) error
	// RaiseTop moves the stream's top_ord up to an order string.
	RaiseTop(ctx context.Context, id model.StreamID, ord string) error
	// SetOrder changes where a message is shown: its order string. For a
	// reply the parent's thread summary follows (last_reply_ord).
	// model.ErrNotFound for no such message.
	SetOrder(ctx context.Context, msg model.MessageID, ord string) error
	// SetEditOrder records the order string of the event that last set a
	// message's content.
	SetEditOrder(ctx context.Context, msg model.MessageID, ord string) error
	// SetOrderFloor sets conversations.order_floor: what a conversation
	// that is no longer shared orders its new messages after.
	SetOrderFloor(ctx context.Context, conv model.ConversationID, floor int64) error
	// LockConversation locks a conversation's row until the transaction
	// ends, as taking a seq does, without changing it. A transaction that
	// writes a stream's row and has not taken a seq in the stream's
	// conversation calls it first: every writer then takes the two rows in
	// the same order, the conversation's and then the stream's. (On
	// PostgreSQL a second write to a stream's row in one transaction
	// checks its reference to the conversation, which waits for whoever
	// holds the conversation's row.) A conversation that is gone is not an
	// error.
	LockConversation(ctx context.Context, conv model.ConversationID) error
	// TakeSeq is Conversations.NextSeq that also says which stream the
	// conversation is in, "" for none, from the same statement.
	TakeSeq(ctx context.Context, conv model.ConversationID) (seq int64, stream model.StreamID, err error)

	// PutKey stores the key of an epoch, replacing what was there.
	PutKey(ctx context.Context, id model.StreamID, k StreamKey) error
	// Key returns the key of an epoch, or model.ErrNotFound.
	Key(ctx context.Context, id model.StreamID, epoch int64) (StreamKey, error)
	// Keys returns every key held for the stream, lowest epoch first.
	Keys(ctx context.Context, id model.StreamID) ([]StreamKey, error)

	// PutOutbox stores an event waiting for the time reference to
	// acknowledge it, replacing a row with the same ID and keeping that
	// row's place in the order.
	PutOutbox(ctx context.Context, row OutboxRow) error
	// GetOutbox returns one waiting envelope, or model.ErrNotFound.
	GetOutbox(ctx context.Context, id string) (OutboxRow, error)
	// Outbox returns a stream's waiting envelopes, oldest first.
	Outbox(ctx context.Context, id model.StreamID) ([]OutboxRow, error)
	// OutboxDue returns waiting envelopes whose next try has come, oldest
	// first, across streams.
	OutboxDue(ctx context.Context, now model.Time, limit int) ([]OutboxRow, error)
	// OutboxTried records a try that got no answer.
	OutboxTried(ctx context.Context, id string, attempts int, next model.Time) error
	// DeleteOutbox removes a waiting envelope and says whether it was there.
	DeleteOutbox(ctx context.Context, id string) (bool, error)
	// OutboxCount is how many envelopes wait, for the admin overview.
	OutboxCount(ctx context.Context) (int64, error)

	// MarkSeen records an applied origin event id. fresh is false, and
	// nothing is written, when it was already there.
	MarkSeen(ctx context.Context, id model.StreamID, originEventID string, seq int64, at model.Time) (fresh bool, err error)
	// Seen returns the seq an origin event id was applied under.
	Seen(ctx context.Context, id model.StreamID, originEventID string) (seq int64, found bool, err error)
	// PruneSeen deletes stream_seen rows older than a moment and returns
	// how many.
	PruneSeen(ctx context.Context, before model.Time) (int64, error)

	// AppendLog writes an envelope to the log of a stream this server is
	// the time reference of, at the next position, and returns the row as
	// stored: Seq is the position and Prev the counter of the same origin's
	// row before it, whether or not that row is still in the log (0 if
	// there never was one). The stream's last_seq moves to the position and
	// the origin's logged_n to the row's counter; the stream's row stays
	// locked until the transaction ends, so positions are given in commit
	// order.
	AppendLog(ctx context.Context, id model.StreamID, row LogRow) (LogRow, error)
	// Log returns log rows with a position above after, in order, at most
	// limit.
	Log(ctx context.Context, id model.StreamID, after int64, limit int) ([]LogRow, error)
	// LogSince returns the log rows of one origin with a counter above n,
	// in log order, at most limit.
	LogSince(ctx context.Context, id model.StreamID, origin string, n int64, limit int) ([]LogRow, error)
	// LogHas reports whether the log holds an envelope with this origin
	// event id.
	LogHas(ctx context.Context, id model.StreamID, originEventID string) (bool, error)
	// LogFloor returns, per origin with rows in the log, the Prev of its
	// oldest row: the counter up to which the log no longer reaches.
	LogFloor(ctx context.Context, id model.StreamID) (map[string]int64, error)
	// SetFormTally replaces the answer count and the counts per option of a
	// poll whose answers another server keeps (LINK-PROTOCOL.md section
	// 19). On such a form the counts are what that server last said, not a
	// count of the rows held here.
	SetFormTally(ctx context.Context, form model.FormID, answerCount int, tally map[string]map[string]int) error

	// PruneLog deletes log rows older than a moment and returns how many.
	PruneLog(ctx context.Context, before model.Time) (int64, error)

	// OutboxIDs returns the origin event ids waiting in a stream's outbox.
	// A message whose id is among them has not been acknowledged.
	OutboxIDs(ctx context.Context, id model.StreamID) ([]string, error)

	// PutShadow creates or updates the local shadow of an account on
	// another server: the same id, role `remote`, the server it lives on.
	// An existing shadow is changed only when acc.ProfileVersion is higher
	// than the one held; changed says whether anything was written.
	// model.ErrConflict if the id belongs to a local account or to an
	// account of a different server.
	PutShadow(ctx context.Context, acc *model.Account) (changed bool, err error)

	// Shadow finds the shadow of another server's account by its username
	// there, or model.ErrNotFound.
	Shadow(ctx context.Context, server model.ServerID, username string) (model.Account, error)

	// FileArrived records that a file's bytes are stored: its keys, what
	// the content turned out to be, and state `ready`.
	FileArrived(ctx context.Context, f model.File) error
	// SetFileState sets files.state: unavailable or too_large.
	SetFileState(ctx context.Context, id model.FileID, state string) error
	// ArrivingFiles returns files in state `arriving`, oldest first.
	ArrivingFiles(ctx context.Context, limit int) ([]ArrivingFile, error)
	// FileInStream reports whether a file belongs to something in the
	// conversation of a stream: a message's attachment or card image, an
	// emoji or an avatar this server would name in that stream's events.
	FileInStream(ctx context.Context, file model.FileID, conv model.ConversationID) (model.File, bool, error)

	// PutRemoteEmoji stores an emoji that came with a shared channel,
	// under the server it came from. A row with that id is left alone.
	PutRemoteEmoji(ctx context.Context, e model.Emoji, server model.ServerID) error
	// RemoteEmoji finds another server's emoji by name.
	RemoteEmoji(ctx context.Context, server model.ServerID, name string) (model.Emoji, error)

	// SharedWith returns the active streams of kind `channel` in which
	// both accounts are members, which is the rule of section 16.
	SharedWith(ctx context.Context, a, b model.AccountID) ([]Stream, error)
	// AccountStreams returns the active streams whose conversations an
	// account is a member of.
	AccountStreams(ctx context.Context, account model.AccountID) ([]Stream, error)
}
