package wire

import (
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
)

// The frame names of section 6.
const (
	FHello        = "hello"
	FAuth         = "auth"
	FReady        = "ready"
	FRoster       = "roster"
	FStreamOpen   = "stream_open"
	FServerJoined = "server_joined"
	FServerLeft   = "server_left"
	FLeave        = "leave"
	FOwnerChanged = "owner_changed"
	FStreamClose  = "stream_close"
	FSubmit       = "submit"
	FAccepted     = "accepted"
	FDeliver      = "deliver"
	FAck          = "ack"
	FCatchUp      = "catch_up"
	FCaughtUp     = "caught_up"
	FEphemeral    = "ephemeral"
	FDirect       = "direct"
	FError        = "error"
	FBye          = "bye"
	// FPing and FPong are the connection's heartbeat, and how a time
	// reference measures a server's clock (clock.go).
	FPing = "ping"
	FPong = "pong"

	// Hub-owned channels (section 23).
	FChannelCreate = "channel_create"
	FChannel       = "channel"
	FChannelAction = "channel_action"
	FJoin          = "join"
	FDirectory     = "directory"
	// FChannelCommit activates a channel a hub has prepared for a move from
	// a direct link (section 21a). It is signed by the former owner server
	// and may be handed to the hub by either of the two servers.
	FChannelCommit = "channel_commit"
	// BMoveAccept is not a frame: it names the signed block in which the
	// other server of a direct link agrees to a move. It travels inside a
	// direct message and then inside channel_create.
	BMoveAccept = "move_accept"
	// BRehomeAnswer names the signed block in which a hub that was offered
	// a channel's home says the offer is dead: it will never take it.
	BRehomeAnswer = "rehome_answer"

	// Trunks between hubs (section 23b). FCluster is also sent by a hub to
	// its servers: the cluster's hubs and their keys.
	FCluster     = "cluster"      // the cluster roster
	FClusterJoin = "cluster_join" // a hub asks the hub it trunked to to admit it
	FHubServers  = "hub_servers"  // one hub's own servers and their keys, signed by it
	FHubClocks   = "hub_clocks"   // a hub's clock against the hubs behind it
	FSync        = "sync"         // what a hub holds of each stream, per origin
	FRelay       = "relay"        // a batch of envelopes from a hub's log
	FRelayAck    = "relay_ack"    // the batch was stored; what was refused
	FAsk         = "ask"          // a server's request, carried to a channel's home hub
	FForward     = "forward"      // a frame for one server, carried to its hub
	FRehome      = "rehome"       // a channel's home offered to another hub, and that hub's answer
	FStreamCtl   = "stream_ctl"   // the owner server's control frame of a stream that crosses a trunk, for the hubs that carry it
	// BClusterMember and BClusterGone are not frames: they name the signed
	// blocks a cluster roster is made of.
	BClusterMember = "cluster_member"
	BClusterGone   = "cluster_gone"
)

// The error codes of section 6, and the three more a direct message's error
// answer may use (section 7).
const (
	EBadFrame           = "bad_frame"
	EVersionUnsupported = "version_unsupported"
	EAuthFailed         = "auth_failed"
	EInviteInvalid      = "invite_invalid"
	EInviteExpired      = "invite_expired"
	EKeyMismatch        = "key_mismatch"
	EBlocked            = "blocked"
	EReplaced           = "replaced"
	EUnknownServer      = "unknown_server"
	EUnknownStream      = "unknown_stream"
	ENotInStream        = "not_in_stream"
	ENotOwner           = "not_owner"
	EConflict           = "conflict"
	EBadSignature       = "bad_signature"
	ETooLarge           = "too_large"
	ERateLimited        = "rate_limited"
	EUnavailable        = "unavailable"
	EInternal           = "internal"
	EForbidden          = "forbidden"
	ENotFound           = "not_found"
	EFeatureOff         = "feature_off"

	// What a time reference refuses a submitted envelope with (section 13).
	// The origin may stamp the event again and send it again.
	EClockAhead   = "clock_ahead"   // `at` is more than 30 seconds ahead of the reference clock
	EClockBehind  = "clock_behind"  // `at` is more than 30 seconds behind, outside any time the origin was disconnected
	EClockOffset  = "clock_offset"  // the offset the origin used is not the one the reference measured
	EClockUnknown = "clock_unknown" // the reference has not measured the origin's clock yet
	ENotMonotonic = "not_monotonic" // `at` or `n` is not above the origin's last accepted one
	EStaleEpoch   = "stale_epoch"   // encrypted under a key epoch that has ended
)

// Restamp reports whether a refusal code is one an origin answers by giving
// the event a new stamp and sending it again.
func Restamp(code string) bool {
	switch code {
	case EClockAhead, EClockBehind, EClockOffset, ENotMonotonic:
		return true
	}
	return false
}

// Limits is what the sender of a hello will accept from the other side.
type Limits struct {
	MaxFrameBytes      int   `json:"max_frame_bytes"`
	MaxEnvelopeBytes   int   `json:"max_envelope_bytes"`
	SubmitsPerMinute   int   `json:"submits_per_minute"`
	EphemeralPerMinute int   `json:"ephemeral_per_minute"`
	DirectPerMinute    int   `json:"direct_per_minute"`
	BlobBytesPerHour   int64 `json:"blob_bytes_per_hour"`
}

// DefaultLimits are the defaults of section 23.
func DefaultLimits() Limits {
	return Limits{MaxFrameBytes: MaxFrameBytes, MaxEnvelopeBytes: MaxEnvelopeBytes, SubmitsPerMinute: 3000,
		EphemeralPerMinute: 1200, DirectPerMinute: 600, BlobBytesPerHour: 1 << 30}
}

// Frame is any frame. One struct holds the fields of all of them; each
// frame uses its own few. On the wire a frame is one Frame message of
// proto/churr/link/v1/link.proto whose body says which frame it is
// (EncodeFrame); the json names here are the names of the schema's fields,
// and a frame cannot be sent with a field its body does not have. Receivers
// ignore fields they do not know.
type Frame struct {
	Frame string `json:"frame"`
	ID    int64  `json:"id,omitempty"`
	Re    int64  `json:"re,omitempty"`

	// hello
	Versions  []int   `json:"versions,omitempty"`
	Version   int     `json:"version,omitempty"`
	Kind      string  `json:"kind,omitempty"`
	Name      string  `json:"name,omitempty"`
	SignKey   Bytes   `json:"sign_key,omitempty"`
	BoxKey    Bytes   `json:"box_key,omitempty"`
	BoxKeySig Bytes   `json:"box_key_sig,omitempty"`
	Nonce     Bytes   `json:"nonce,omitempty"`
	Software  string  `json:"software,omitempty"`
	Limits    *Limits `json:"limits,omitempty"`

	// auth, signed blocks and direct messages all carry a signature
	Sig         Bytes `json:"sig,omitempty"`
	InviteProof Bytes `json:"invite_proof,omitempty"`

	// signed blocks: roster and the stream control frames
	Signed Bytes `json:"signed,omitempty"`
	// HomeHub is added by a hub that relays stream_open: the hub that is
	// the stream's home and its time reference.
	HomeHub string `json:"home_hub,omitempty"`

	// submit, deliver, ephemeral
	Envelope *Envelope `json:"envelope,omitempty"`
	// Envelopes is a batch: a submit or deliver frame may carry several
	// envelopes, of one stream or of several, in place of the one. The
	// receiver treats them one by one, in the order given.
	Envelopes []Envelope `json:"envelopes,omitempty"`

	// ack, catch_up, caught_up, error, unsigned stream_close
	StreamID string `json:"stream_id,omitempty"`
	// Cursor, on ack, is the sender's position in the log of whoever
	// delivers to it: a delivery cursor and nothing more.
	Cursor int64 `json:"cursor,omitempty"`
	// Have is, per origin, the highest counter the sender holds everything
	// up to: on catch_up what the asker holds, on caught_up and on a
	// relayed control frame what the log holds. Floor, on caught_up, is per
	// origin the counter below which the log no longer reaches.
	Have  map[string]int64 `json:"have,omitempty"`
	Floor map[string]int64 `json:"floor,omitempty"`
	// IDs, on accepted, are the origin event ids of the envelopes taken.
	IDs []string `json:"ids,omitempty"`
	// OriginEventID, on an error that answers a submit, names the envelope
	// refused.
	OriginEventID string `json:"origin_event_id,omitempty"`

	// direct
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	Body Bytes  `json:"body,omitempty"`

	// ping, pong: t0 by the reference's clock, t1 and t2 by the server's,
	// all in milliseconds; and on a ping, what the reference has worked out
	// about the server's clock so far.
	T0    *int64 `json:"t0,omitempty"`
	T1    *int64 `json:"t1,omitempty"`
	T2    *int64 `json:"t2,omitempty"`
	Clock *Clock `json:"clock,omitempty"`

	// ping from a hub to a server: the server's clock against each other
	// hub of the cluster, composed along the trunks (section 23b).
	Homes map[string]model.HomeClock `json:"homes,omitempty"`

	// Between hubs (section 23b). Cluster is the roster on cluster; Streams
	// what the sender holds on sync, and Done says the sender has said all;
	// Items the batch on relay; Refused what a relay_ack did not take; Hub
	// and Server where an ask or a forward is going and who it is from or
	// for; Inner the frame it carries, as the bytes of that frame
	// (EncodeFrame), passed on as they came; Clocks the figures of
	// hub_clocks; JoinedHave, Also and State what a hub adds, unsigned,
	// beside a channel record it passes to another hub.
	Cluster []ClusterEntry        `json:"cluster,omitempty"`
	Streams map[string]SyncStream `json:"streams,omitempty"`
	Done    bool                  `json:"done,omitempty"`
	Items   []RelayItem           `json:"items,omitempty"`
	Refused []RelayRefusal        `json:"refused,omitempty"`
	Hub     string                `json:"hub,omitempty"`
	Server  string                `json:"server,omitempty"`
	Inner   Bytes                 `json:"inner,omitempty"`
	Clocks  map[string]HubClock   `json:"clocks,omitempty"`
	// Reach, on hub_clocks, names the hubs the sender can reach now that
	// are not on the receiver's side.
	Reach      []string                    `json:"reach,omitempty"`
	JoinedHave map[string]map[string]int64 `json:"joined_have,omitempty"`
	Also       []string                    `json:"also,omitempty"`
	State      *Handoff                    `json:"state,omitempty"`
	// For rehome (section 23b): which step of the exchange this is (offer,
	// cancel, taken, refused, cancelled) and the offer it is about. From and
	// To are then the hubs at its two ends.
	Step  string `json:"step,omitempty"`
	Offer string `json:"offer,omitempty"`

	// error, bye
	Code              string `json:"code,omitempty"`
	Message           string `json:"message,omitempty"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
	Reason            string `json:"reason,omitempty"`
}

// ErrorFrame builds an error frame answering frame number re (0 for none).
func ErrorFrame(re int64, code, message string) Frame {
	return Frame{Frame: FError, Re: re, Code: code, Message: message}
}

// Error is an error frame as a Go error, so a refusal can travel up a call
// chain and be sent back as it is.
type Error struct {
	Code       string
	Message    string
	RetryAfter int
	StreamID   string
	// OriginEventID names the envelope a submit's refusal is about, and
	// Clock, on a refusal for its stamp, is what the reference holds of the
	// origin's clock, so the origin can stamp again at once.
	OriginEventID string
	Clock         *Clock
}

func (e *Error) Error() string { return "link: " + e.Code + ": " + e.Message }

// Errf makes an Error.
func Errf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Frame turns the error into the frame that reports it.
func (e *Error) Frame(re int64) Frame {
	return Frame{Frame: FError, Re: re, Code: e.Code, Message: e.Message, RetryAfterSeconds: e.RetryAfter, StreamID: e.StreamID,
		OriginEventID: e.OriginEventID, Clock: e.Clock}
}

// AsError finds the *Error in err, or wraps anything else as `internal`
// with nothing of the cause in it.
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: EInternal, Message: "The receiver failed."}
}

// ---------------------------------------------------------- signed blocks

// blockMessage is what a signed block's sig covers:
// "churr-signed-v1" 0x00 || lp(<name>) || u32(len(signed)) || signed.
func blockMessage(name string, signed []byte) []byte {
	var l layout
	l.label("churr-signed-v1").lp(name).u32(len(signed)).raw(signed)
	return l.Bytes()
}

// SignBlock makes a signed block frame: the content as the bytes of the
// message the block's name stands for, written once, here, and a signature
// over those bytes. Nobody who carries the block writes them again.
func SignBlock(name string, content any, keys Signer) (Frame, error) {
	signed, err := marshalBlock(name, content)
	if err != nil {
		return Frame{}, err
	}
	return Frame{Frame: name, Signed: signed, Sig: keys.Sign(blockMessage(name, signed))}, nil
}

// VerifyBlock checks a signed block against a key, over the bytes as
// received, and only then parses them into out.
func VerifyBlock(f Frame, signKey []byte, out any) error {
	if !verify(signKey, blockMessage(f.Frame, f.Signed), f.Sig) {
		return Errf(EBadSignature, "The signature on %s does not verify.", f.Frame)
	}
	if err := unmarshalBlock(f.Frame, f.Signed, out); err != nil {
		return Errf(EBadFrame, "The signed part of %s cannot be read.", f.Frame)
	}
	return nil
}

// PeekBlock parses a signed block without checking it, to learn who claims
// to have signed it. Nothing may be done with the result but look up a key.
func PeekBlock(f Frame, out any) error {
	if err := unmarshalBlock(f.Frame, f.Signed, out); err != nil {
		return Errf(EBadFrame, "The signed part of %s cannot be read.", f.Frame)
	}
	return nil
}

// RosterServer is one server on a hub's roster.
type RosterServer struct {
	Name      string     `json:"name"`
	SignKey   Bytes      `json:"sign_key"`
	BoxKey    Bytes      `json:"box_key"`
	BoxKeySig Bytes      `json:"box_key_sig"`
	JoinedAt  model.Time `json:"joined_at"`
}

// Roster is the signed content of a roster frame.
type Roster struct {
	Hub     string         `json:"hub"`
	Version int64          `json:"version"`
	At      model.Time     `json:"at"`
	Servers []RosterServer `json:"servers"`
}

// Control is the signed content of a stream control frame. Each frame uses
// the fields section 6 gives it.
type Control struct {
	StreamID string     `json:"stream_id"`
	Owner    string     `json:"owner,omitempty"`
	Server   string     `json:"server,omitempty"`
	NewOwner string     `json:"new_owner,omitempty"`
	Reason   string     `json:"reason,omitempty"`
	Servers  []string   `json:"servers,omitempty"`
	Version  int64      `json:"version,omitempty"`
	At       model.Time `json:"at"`
}

// ChannelAccount is an account a hub-owned channel's record names as one of
// its managers or moderators, with authority on every instance in it
// (section 23a): the account, and the instance it belongs to.
type ChannelAccount struct {
	AccountID string `json:"account_id"`
	Server    string `json:"server"`
	Username  string `json:"username"`
}

// The listing modes of a hub-owned channel.
const (
	ListingUnlisted = "unlisted"
	ListingRequest  = "request"
	ListingOpen     = "open"
)

// ValidListing reports whether a listing mode is one of the three.
func ValidListing(l string) bool {
	return l == ListingUnlisted || l == ListingRequest || l == ListingOpen
}

// ChannelRecord is the signed content of a channel frame: everything a hub
// records of a channel it owns, whole, at one version. Only the hub signs
// it. Hub and HubKey name the signer; HomeHub names the hub that is the
// stream's home and time reference, which today is the same one.
type ChannelRecord struct {
	StreamID    string   `json:"stream_id"`
	Hub         string   `json:"hub"`
	HubKey      Bytes    `json:"hub_key"`
	HomeHub     string   `json:"home_hub"`
	Version     int64    `json:"version"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Listing     string   `json:"listing"`
	Servers     []string `json:"servers"` // longest-standing first
	// Managers hold the channel as an owner would: instances, name and
	// description, managers and moderators, closing it, and in a private
	// channel who is admitted. Moderators keep the peace: they remove
	// messages and people on every instance.
	Managers   []ChannelAccount `json:"managers"`
	Moderators []ChannelAccount `json:"moderators,omitempty"`
	Invited    []string         `json:"invited"`
	Requests   []string         `json:"requests"`
	// Epoch is the key epoch the home hub has begun for the server list,
	// and Joined and Left what the list change that began it was. The hub
	// makes the epoch's key and sends it to every instance in the list.
	Epoch  int64    `json:"epoch"`
	Joined []string `json:"joined"`
	Left   []string `json:"left"`
	// HistoryDays is how many days of history a joining instance is given.
	HistoryDays int  `json:"history_days,omitempty"`
	Closed      bool `json:"closed,omitempty"`
	// Prepared: the hub holds the channel for a move from a direct link
	// that has not been committed yet (section 21a). It carries nothing
	// and tells nobody but the server that brought it until it is
	// activated or closed.
	Prepared bool       `json:"prepared,omitempty"`
	At       model.Time `json:"at"`
	// Private: the channel was private when it was shared, and is for good.
	// Its listing is unlisted and stays so; the hub refuses to list it, and
	// its people are admitted by its moderators (section 23a).
	Private bool `json:"private,omitempty"`
}

// Has reports whether a server is in the record's server list.
func (r ChannelRecord) Has(server string) bool { return contains(r.Servers, server) }

// Manages reports whether an account of an instance is on the manager
// list.
func (r ChannelRecord) Manages(accountID, server string) bool {
	return named(r.Managers, accountID, server)
}

// Moderates reports whether an account of an instance is on the moderator
// list. A manager may do all a moderator may: Keeps answers either.
func (r ChannelRecord) Moderates(accountID, server string) bool {
	return named(r.Moderators, accountID, server)
}

// Keeps reports whether an account of an instance is a manager or a
// moderator of the channel: who may remove messages and people on every
// instance.
func (r ChannelRecord) Keeps(accountID, server string) bool {
	return r.Manages(accountID, server) || r.Moderates(accountID, server)
}

func named(list []ChannelAccount, accountID, server string) bool {
	for _, m := range list {
		if m.AccountID == accountID && m.Server == server {
			return true
		}
	}
	return false
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ChannelCreate is the signed content of a channel_create frame, signed by
// the server that shares a channel to a hub. For a channel moved from a
// direct link (section 21a) MoveID names the move, Servers holds both its
// servers, Managers every manager of it on either, Epoch the key epoch
// that will begin on the hub, and Accept and AcceptSig the other server's
// signed agreement (a move_accept block), which the hub checks.
type ChannelCreate struct {
	StreamID    string           `json:"stream_id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Listing     string           `json:"listing"`
	Creator     ChannelAccount   `json:"creator"`
	MoveID      string           `json:"move_id,omitempty"`
	Servers     []string         `json:"servers,omitempty"`
	Managers    []ChannelAccount `json:"managers,omitempty"`
	Epoch       int64            `json:"epoch,omitempty"`
	Accept      Bytes            `json:"accept,omitempty"`
	AcceptSig   Bytes            `json:"accept_sig,omitempty"`
	At          model.Time       `json:"at"`
	// Private: the channel shared is private, and so it stays on the hub.
	Private bool `json:"private,omitempty"`
	// HistoryDays is how many days of history the channel gives a joining
	// instance, 0 to 90.
	HistoryDays int `json:"history_days,omitempty"`
}

// MoveAccept is the signed content of a move_accept block: the other server
// of a direct link agreeing to move one stream onto one hub (section 21a).
// Managers are the managers of the channel on that server; they do not
// change there until the move has ended.
type MoveAccept struct {
	MoveID    string           `json:"move_id"`
	StreamID  string           `json:"stream_id"`
	Hub       string           `json:"hub"`
	HubKey    Bytes            `json:"hub_key"`
	Owner     string           `json:"owner"`  // the server that proposed: the stream's owner
	Server    string           `json:"server"` // the server that agrees, and signs
	Managers  []ChannelAccount `json:"managers"`
	ExpiresAt model.Time       `json:"expires_at"`
	At        model.Time       `json:"at"`
}

// MoveCommit is the signed content of a channel_commit frame: the former
// owner server saying that it has committed a move. Have and HaveAt are, per
// origin, the counter and the `at` of the last event of the stream's direct
// life: the hub starts each origin from there.
type MoveCommit struct {
	MoveID   string           `json:"move_id"`
	StreamID string           `json:"stream_id"`
	Hub      string           `json:"hub"`
	Owner    string           `json:"owner"`
	Have     map[string]int64 `json:"have"`
	HaveAt   map[string]int64 `json:"have_at"`
	Epoch    int64            `json:"epoch"` // the key epoch the stream ended its direct life under
	// Managers are the managers of the channel on the owner server, read
	// in the transaction that committed. With the other server's, which it
	// signed for itself, they are the channel's managers from here on.
	Managers []ChannelAccount `json:"managers"`
	At       model.Time       `json:"at"`
}

// SignBytes signs content as a named block and returns the bytes and the
// signature, for a block that travels inside something else.
func SignBytes(name string, content any, keys Signer) (signed, sig Bytes, err error) {
	f, err := SignBlock(name, content, keys)
	return f.Signed, f.Sig, err
}

// VerifyBytes is VerifyBlock for a block that travelled inside something
// else.
func VerifyBytes(name string, signed, sig, signKey []byte, out any) error {
	return VerifyBlock(Frame{Frame: name, Signed: signed, Sig: sig}, signKey, out)
}

// The actions of a channel_action frame.
const (
	ActInvite          = "invite"
	ActApprove         = "approve"
	ActDecline         = "decline"
	ActRemoveServer    = "remove_server"
	ActDescribe        = "describe"
	ActSetListing      = "set_listing"
	ActAddManager      = "add_manager"
	ActRemoveManager   = "remove_manager"
	ActClose           = "close"
	ActAddModerator    = "add_moderator"
	ActRemoveModerator = "remove_moderator"
)

// ChannelAction is the signed content of a channel_action frame: what a
// manager asks of the hub, signed by the manager's home instance. A join
// frame uses the same content with Server set to the joining instance,
// signed by it. Account is the account an action on the managers or the
// moderators names.
type ChannelAction struct {
	StreamID    string          `json:"stream_id"`
	ActionID    string          `json:"action_id"`
	Action      string          `json:"action,omitempty"`
	By          ChannelAccount  `json:"by"`
	Server      string          `json:"server,omitempty"`
	Account     *ChannelAccount `json:"account,omitempty"`
	Name        *string         `json:"name,omitempty"`
	Description *string         `json:"description,omitempty"`
	Listing     string          `json:"listing,omitempty"`
	At          model.Time      `json:"at"`
}

// DirectoryEntry is one channel in a hub's directory.
type DirectoryEntry struct {
	StreamID    string           `json:"stream_id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Listing     string           `json:"listing"`
	Managers    []ChannelAccount `json:"managers"`
	Servers     int              `json:"servers"`
	// Yours is the asking server's own place in the channel: "", member,
	// invited or requested. Each server is told only its own.
	Yours string `json:"yours,omitempty"`
}

// Directory is the signed content of a hub's directory frame.
type Directory struct {
	Hub      string           `json:"hub"`
	HubKey   Bytes            `json:"hub_key"`
	At       model.Time       `json:"at"`
	Channels []DirectoryEntry `json:"channels"`
}

// --------------------------------------------------------------- envelope

// Stamp is where an event stands in its conversation, set by the server
// that made it and covered by its signature (section 9): nobody who carries
// the envelope can move it.
type Stamp struct {
	// At is the time of the conversation's reference clock, in
	// milliseconds, as the origin read it when it made the event: its own
	// clock minus Offset.
	At int64
	// N is the origin's counter in the stream. It only grows.
	N int64
	// Offset is the offset the origin used, in milliseconds: its own clock
	// minus the reference's, as the reference told it. The reference checks
	// it against its own figure for the origin.
	Offset int64
}

// Envelope is one signed, encrypted event in a stream (section 9).
type Envelope struct {
	StreamID      string `json:"stream_id"`
	KeyEpoch      int64  `json:"key_epoch"`
	Origin        string `json:"origin"`
	OriginEventID string `json:"origin_event_id"`
	// At, N and Offset are the event's stamp. All three are signed. They
	// are absent, and zero, on an ephemeral envelope.
	At         int64 `json:"at,omitempty"`
	N          int64 `json:"n,omitempty"`
	Offset     int64 `json:"offset,omitempty"`
	Ciphertext Bytes `json:"ciphertext"`
	Sig        Bytes `json:"sig"`
	// Cursor and Prev are added by whoever delivers the envelope from its
	// log and are not signed: Cursor is the envelope's position in that
	// log, and Prev the counter of the same origin's envelope before it
	// there. A receiver that holds less than Prev from that origin has
	// missed something and asks to catch up. Neither says anything about
	// where the event is shown.
	Cursor int64 `json:"cursor,omitempty"`
	Prev   int64 `json:"prev,omitempty"`
}

// Stamp returns the envelope's stamp.
func (e Envelope) Stamp() Stamp { return Stamp{At: e.At, N: e.N, Offset: e.Offset} }

// header is id16(stream_id) || u32(key_epoch) || lp(origin) ||
// id16(origin_event_id) || i64(at) || i64(n) || i64(offset): what ties a
// ciphertext to its place in the stream and in time.
func (e Envelope) header() []byte {
	var l layout
	l.id16(e.StreamID).u32(int(e.KeyEpoch)).lp(e.Origin).id16(e.OriginEventID).i64(e.At).i64(e.N).i64(e.Offset)
	return l.Bytes()
}

func (e Envelope) aad() []byte {
	var l layout
	l.label("churr-envelope-aad-v1").raw(e.header())
	return l.Bytes()
}

// signedBytes is the canonical encoding the origin signs:
// "churr-envelope-v1" 0x00 || header || u32(len(ciphertext)) || ciphertext.
func (e Envelope) signedBytes() []byte {
	var l layout
	l.label("churr-envelope-v1").raw(e.header()).u32(len(e.Ciphertext)).raw(e.Ciphertext)
	return l.Bytes()
}

// WellFormed checks the shape of an envelope: identifiers that are UUIDs, an
// epoch from 1, a stamp that is not negative, a ciphertext long enough to
// hold a nonce and a tag.
func (e Envelope) WellFormed() bool {
	return ValidID(e.StreamID) && ValidID(e.OriginEventID) && e.KeyEpoch >= 1 && e.KeyEpoch < 1<<31 && e.Origin != "" &&
		e.At >= 0 && e.N >= 0 &&
		len(e.Ciphertext) >= chacha20poly1305.NonceSizeX+chacha20poly1305.Overhead && len(e.Sig) == 64
}

// SealEnvelope encrypts an inner event under a stream key and signs the
// result, stamp included, as origin. The nonce is random and travels at the front of the
// ciphertext.
func SealEnvelope(key []byte, streamID string, epoch int64, origin, originEventID string, stamp Stamp, plaintext []byte, keys Signer) (Envelope, error) {
	e := Envelope{StreamID: streamID, KeyEpoch: epoch, Origin: origin, OriginEventID: originEventID, At: stamp.At, N: stamp.N, Offset: stamp.Offset}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return e, err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return e, err
	}
	e.Ciphertext = aead.Seal(nonce, nonce, plaintext, e.aad())
	e.Sig = keys.Sign(e.signedBytes())
	return e, nil
}

// Verify checks the origin's signature. The stamp is under it; what a
// carrier adds beside the envelope is not.
func (e Envelope) Verify(signKey []byte) bool { return verify(signKey, e.signedBytes(), e.Sig) }

// Open decrypts the envelope with a stream key. A ciphertext moved into an
// envelope with another stream, epoch, origin or event identifier does not
// open, because all four are in the associated data.
func (e Envelope) Open(key []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(e.Ciphertext) < chacha20poly1305.NonceSizeX+chacha20poly1305.Overhead {
		return nil, errors.New("wire: the ciphertext is too short")
	}
	nonce, sealed := e.Ciphertext[:chacha20poly1305.NonceSizeX], e.Ciphertext[chacha20poly1305.NonceSizeX:]
	return aead.Open(nil, nonce, sealed, e.aad())
}

// ----------------------------------------------------------------- direct

// directMessage is what a direct frame's sig covers: "churr-direct-v1" 0x00
// || lp(from) || lp(to) || u32(len(body)) || body.
func directMessage(from, to string, body []byte) []byte {
	var l layout
	l.label("churr-direct-v1").lp(from).lp(to).u32(len(body)).raw(body)
	return l.Bytes()
}

// SealDirect seals plaintext from this server to another and signs it
// (section 7). The frame it returns has no id yet.
func SealDirect(keys Signer, from, to string, toBoxKey, plaintext []byte) (Frame, error) {
	body, err := secret.SealBoxBetween(toBoxKey, plaintext, from, to)
	if err != nil {
		return Frame{}, err
	}
	return Frame{Frame: FDirect, From: from, To: to, Body: body, Sig: keys.Sign(directMessage(from, to, body))}, nil
}

// VerifyDirect checks the sender's signature on a direct frame. A hub does
// this and no more: it cannot open the body.
func VerifyDirect(f Frame, fromSignKey []byte) bool {
	return verify(fromSignKey, directMessage(f.From, f.To, f.Body), f.Sig)
}

// OpenDirect verifies and opens a direct frame addressed to this server.
func OpenDirect(keys Signer, self string, f Frame, fromSignKey []byte) ([]byte, error) {
	if f.To != self {
		return nil, errors.New("wire: the direct message is for another server")
	}
	if !VerifyDirect(f, fromSignKey) {
		return nil, errors.New("wire: the direct message's signature does not verify")
	}
	return keys.OpenBoxBetween(f.Body, f.From, f.To)
}

// SealTo is the sealing of section 7 without the frame: what a form answer
// uses to reach the form's home server inside an envelope (section 19).
func SealTo(from, to string, toBoxKey, plaintext []byte) ([]byte, error) {
	return secret.SealBoxBetween(toBoxKey, plaintext, from, to)
}

// RehomeAnswer is a hub's signed word that an offer of a channel's home will
// never be taken by it (section 23b): it refused the offer, or the hub that
// made it called it off before it arrived.
type RehomeAnswer struct {
	Offer    string     `json:"offer"`
	StreamID string     `json:"stream_id"`
	Hub      string     `json:"hub"`
	Dead     bool       `json:"dead"`
	Why      string     `json:"why,omitempty"`
	At       model.Time `json:"at"`
}
