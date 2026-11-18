package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Server is a row of `servers`: another Churr server this one has met, with
// the keys pinned for its name. The link package also keeps one row for
// this server itself, so that a stream's server list can name it.
type Server struct {
	ID          model.ServerID
	Name        string
	SignKey     []byte
	BoxKey      []byte
	FirstSeenAt model.Time
	BlockedAt   model.Time
	// VRFKeys is the server's VRF key history as pinned on a direct link,
	// the JSON the link carries it as; nil until pinned. PinServer does not
	// write it: SetServerVRFKeys does.
	VRFKeys []byte
}

// Blocked reports whether this server accepts nothing from that one.
func (s Server) Blocked() bool { return !s.BlockedAt.IsZero() }

// Peer is a row of `peers`: a direct link to one other server, or a
// connection to a hub.
type Peer struct {
	ID              model.PeerID
	Kind            string // server | hub
	ServerID        model.ServerID
	HubName         string
	HubSignKey      []byte
	URL             string // where this server dials; empty if the peer dials us
	Dials           bool
	State           string // invited | active | paused | removed
	InviteHash      []byte
	InviteExpiresAt model.Time
	CreatedBy       model.AccountID
	CreatedAt       model.Time
	ConnectedAt     model.Time
	LastError       string
	// ClockTold is what the peer, as a time reference, last told this
	// server about this server's clock; ClockMeasured is what this server,
	// as a time reference, last measured of the peer's. Nil until there is
	// one. Update does not write them: SetClockTold and SetClockMeasured do.
	ClockTold     *model.PeerClock
	ClockMeasured *model.PeerClock
	// Away is, for a direct link, the last few stretches the peer was
	// disconnected for, oldest first, each as from and to in milliseconds
	// by this server's clock. Update does not write it: AddAway does.
	Away [][2]int64
}

// The peer states.
const (
	PeerInvited = "invited"
	PeerActive  = "active"
	PeerPaused  = "paused"
	PeerRemoved = "removed"
)

// Peers is the `servers`, `peers` and `peer_servers` tables.
type Peers interface {
	// PinServer stores a server's name and keys the first time the name is
	// seen and returns the row. If the name is already known the stored
	// row comes back untouched and created is false: the caller compares
	// the keys, since a pinned name never takes new ones. An empty ID and
	// zero FirstSeenAt are filled in.
	PinServer(ctx context.Context, s Server) (pinned Server, created bool, err error)
	// Server returns one server, or model.ErrNotFound.
	Server(ctx context.Context, id model.ServerID) (Server, error)
	// ServerByName returns the server pinned under a name, or
	// model.ErrNotFound.
	ServerByName(ctx context.Context, name string) (Server, error)
	// Servers returns every known server, by name.
	Servers(ctx context.Context) ([]Server, error)
	// SetServerBlocked sets blocked_at, or clears it when at is zero.
	// model.ErrNotFound for an unknown server.
	SetServerBlocked(ctx context.Context, id model.ServerID, at model.Time) error
	// SetServerVRFKeys replaces the pinned VRF key history of a server.
	// model.ErrNotFound for an unknown server.
	SetServerVRFKeys(ctx context.Context, id model.ServerID, keys []byte) error

	// Create stores a peer. An empty ID and zero CreatedAt are filled in.
	Create(ctx context.Context, p *Peer) error
	// Get returns one peer, or model.ErrNotFound.
	Get(ctx context.Context, id model.PeerID) (Peer, error)
	// List returns every peer, oldest first.
	List(ctx context.Context) ([]Peer, error)
	// Update changes a peer under a row lock: fn is given the stored row
	// and what it leaves is written. An error from fn changes nothing.
	Update(ctx context.Context, id model.PeerID, fn func(p *Peer) error) (Peer, error)
	// SetClockTold stores what the peer told this server about its clock,
	// and SetClockMeasured what this server measured of the peer's. Both
	// stay through disconnections and restarts. model.ErrNotFound for an
	// unknown peer.
	SetClockTold(ctx context.Context, id model.PeerID, c model.PeerClock) error
	SetClockMeasured(ctx context.Context, id model.PeerID, c model.PeerClock) error
	// AddAway appends a stretch the peer was disconnected for and keeps the
	// newest keep of them.
	AddAway(ctx context.Context, id model.PeerID, from, to int64, keep int) error
	// Delete removes a peer that has no streams: an invitation revoked
	// before anyone used it.
	Delete(ctx context.Context, id model.PeerID) error

	// SetReachable replaces the list of servers reachable through a hub.
	SetReachable(ctx context.Context, peer model.PeerID, servers []model.ServerID) error
	// Reachable returns the servers reachable through a hub, by name.
	Reachable(ctx context.Context, peer model.PeerID) ([]Server, error)
}
