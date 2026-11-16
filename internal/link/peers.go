package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// KnownServer is another server as the admin console shows it (schema
// KnownServer).
type KnownServer struct {
	ID          model.ServerID `json:"id"`
	Name        string         `json:"name"`
	Fingerprint string         `json:"fingerprint"`
	FirstSeenAt model.Time     `json:"first_seen_at"`
	BlockedAt   model.Time     `json:"blocked_at,omitempty"`
}

// Peer is a peer as the admin console shows it (schema Peer).
type Peer struct {
	model.Peer
	Servers []KnownServer `json:"servers,omitempty"`
}

func knownServer(sv store.Server) KnownServer {
	return KnownServer{ID: sv.ID, Name: sv.Name, Fingerprint: wire.KeyFingerprint(sv.SignKey), FirstSeenAt: sv.FirstSeenAt, BlockedAt: sv.BlockedAt}
}

// peerView fills in what the admin console shows of a peer.
func (s *Service) peerView(ctx context.Context, p store.Peer) (Peer, error) {
	out := Peer{Peer: model.Peer{ID: p.ID, Kind: p.Kind, Dials: p.Dials, State: p.State, InviteExpiresAt: p.InviteExpiresAt,
		CreatedByID: p.CreatedBy, CreatedAt: p.CreatedAt, ConnectedAt: p.ConnectedAt, LastError: p.LastError,
		ToldClock: p.ClockTold, MeasuredClock: p.ClockMeasured}}
	switch {
	case p.Kind == wire.KindHub:
		out.Name, out.Fingerprint = p.HubName, wire.KeyFingerprint(p.HubSignKey)
		servers, err := s.store.Peers().Reachable(ctx, p.ID)
		if err != nil {
			return out, err
		}
		for _, sv := range servers {
			out.Servers = append(out.Servers, knownServer(sv))
		}
	case p.ServerID != "":
		sv, err := s.store.Peers().Server(ctx, p.ServerID)
		if err != nil {
			return out, err
		}
		out.Name, out.Fingerprint = sv.Name, wire.KeyFingerprint(sv.SignKey)
	}
	if s.connOf(p.ID) == nil {
		// connected_at says when the connection that is up began.
		out.ConnectedAt = 0
	}
	return out, nil
}

// Peers lists every peer.
func (s *Service) Peers(ctx context.Context) ([]Peer, error) {
	list, err := s.store.Peers().List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Peer, 0, len(list))
	for _, p := range list {
		v, err := s.peerView(ctx, p)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// PeerByID returns one peer.
func (s *Service) PeerByID(ctx context.Context, id model.PeerID) (Peer, error) {
	p, err := s.store.Peers().Get(ctx, id)
	if err != nil {
		return Peer{}, err
	}
	return s.peerView(ctx, p)
}

// KnownServers lists the other servers this one knows.
func (s *Service) KnownServers(ctx context.Context) ([]KnownServer, error) {
	list, err := s.store.Peers().Servers(ctx)
	if err != nil {
		return nil, err
	}
	// A hub has a row too, so that a channel it owns has an owner; it is
	// not a server, and is not listed as one.
	hubs := s.hubNames(ctx)
	out := []KnownServer{}
	for _, sv := range list {
		if !s.isSelf(sv.ID) && !hubs[sv.Name] {
			out = append(out, knownServer(sv))
		}
	}
	return out, nil
}

// BlockServer makes this server accept nothing from another, or lifts that.
func (s *Service) BlockServer(ctx context.Context, by model.AccountID, id model.ServerID, block bool) error {
	if s.isSelf(id) {
		return model.ErrNotFound
	}
	at := model.Time(0)
	action := "link.server_unblocked"
	if block {
		at, action = s.now(), "link.server_blocked"
	}
	if err := s.store.Peers().SetServerBlocked(ctx, id, at); err != nil {
		return err
	}
	s.audit(ctx, s.store, by, action, "server", string(id), nil)
	if block {
		// A direct link to it is cut at once; it is refused when it
		// returns.
		peers, err := s.store.Peers().List(ctx)
		if err != nil {
			return err
		}
		for _, p := range peers {
			if p.ServerID == id {
				if c := s.connOf(p.ID); c != nil {
					c.conn.CloseWith(wire.ErrorFrame(0, wire.EBlocked, "This server has been blocked."))
				}
			}
		}
	}
	return nil
}

// CreateInvitation makes an invitation for one other server to link to this
// one directly. This server listens; the one that accepts dials.
func (s *Service) CreateInvitation(ctx context.Context, by model.AccountID, expiresIn time.Duration) (Peer, string, error) {
	if expiresIn <= 0 || expiresIn > inviteLife {
		expiresIn = inviteLife
	}
	raw := secret.RandomBytes(32)
	p := store.Peer{Kind: wire.KindServer, Dials: false, State: store.PeerInvited, InviteHash: wire.InviteHash(raw),
		InviteExpiresAt: s.now().Add(expiresIn), CreatedBy: by, CreatedAt: s.now()}
	if err := s.store.Peers().Create(ctx, &p); err != nil {
		return Peer{}, "", err
	}
	inv := wire.Invitation{V: 1, Kind: wire.KindServer, Name: s.id.Name, URL: s.id.URL, FP: wire.Fingerprint(s.keys.SignPublic()),
		Secret: raw, ExpiresAt: p.InviteExpiresAt}
	s.audit(ctx, s.store, by, "link.invitation_created", "peer", string(p.ID), map[string]any{"expires_at": p.InviteExpiresAt})
	v, err := s.peerView(ctx, p)
	return v, inv.String(), err
}

// InvitationInfo is what an owner is shown before accepting an invitation.
type InvitationInfo struct {
	Kind        string     `json:"kind"`
	Name        string     `json:"name"`
	Fingerprint string     `json:"fingerprint"`
	ExpiresAt   model.Time `json:"expires_at"`
}

// InspectInvitation reads an invitation without dialling anyone.
func (s *Service) InspectInvitation(text string) (InvitationInfo, wire.Invitation, error) {
	inv, err := wire.ParseInvitation(text)
	if err != nil {
		return InvitationInfo{}, inv, model.Invalid("invitation", "That is not an invitation: "+err.Error()+".")
	}
	info := InvitationInfo{Kind: inv.Kind, Name: inv.Name, Fingerprint: wire.FingerprintText(inv.FP), ExpiresAt: inv.ExpiresAt}
	switch {
	case inv.ExpiresAt.Before(s.now()):
		return info, inv, model.ErrConflict.With("That invitation has expired. Ask for a new one.")
	case inv.Name == s.id.Name:
		return info, inv, model.ErrConflict.With("That invitation was made by this server.")
	case inv.For != nil && *inv.For != s.id.Name:
		return info, inv, model.ErrConflict.With("That invitation is for another server.")
	}
	return info, inv, nil
}

// AcceptInvitation dials the issuer of an invitation, proves the invitation,
// pins the issuer's keys and keeps the connection as a new peer.
func (s *Service) AcceptInvitation(ctx context.Context, by model.AccountID, text string) (Peer, error) {
	_, inv, err := s.InspectInvitation(text)
	if err != nil {
		return Peer{}, err
	}
	peers, err := s.store.Peers().List(ctx)
	if err != nil {
		return Peer{}, err
	}
	for _, p := range peers {
		if p.State == store.PeerRemoved || p.State == store.PeerInvited {
			continue
		}
		name := p.HubName
		if p.ServerID != "" {
			if sv, err := s.store.Peers().Server(ctx, p.ServerID); err == nil {
				name = sv.Name
			}
		}
		if name == inv.Name {
			return Peer{}, model.ErrAlreadyExists.With("This server is already linked with " + inv.Name + ".")
		}
	}
	conn, err := wire.Dial(ctx, wire.DialOptions{URL: inv.URL, Identity: s.id, HTTPClient: s.http, InviteSecret: inv.Secret,
		Check: func(h wire.Frame) error {
			// Before anything derived from the secret is sent: the
			// listener is who the invitation says.
			if h.Name != inv.Name || h.Kind != inv.Kind {
				return wire.Errf(wire.EAuthFailed, "The other side is not %s.", inv.Name)
			}
			if !bytes.Equal(wire.Fingerprint(h.SignKey), inv.FP) {
				return wire.Errf(wire.EKeyMismatch, "The other side's key does not match the invitation's fingerprint.")
			}
			return nil
		}})
	if err != nil {
		var refused *wire.Error
		if errors.As(err, &refused) {
			return Peer{}, model.ErrConflict.With("The invitation was refused (" + refused.Code + "): " + refused.Message)
		}
		return Peer{}, model.ErrUnavailable.With("The other side could not be reached.")
	}
	p := store.Peer{Kind: inv.Kind, URL: inv.URL, Dials: true, State: store.PeerActive, CreatedBy: by, CreatedAt: s.now(), ConnectedAt: s.now()}
	if inv.Kind == wire.KindHub {
		p.HubName, p.HubSignKey = inv.Name, conn.Remote.SignKey
	} else {
		sv, created, err := s.store.Peers().PinServer(ctx, store.Server{Name: inv.Name, SignKey: conn.Remote.SignKey, BoxKey: conn.Remote.BoxKey})
		if err != nil {
			conn.Close()
			return Peer{}, err
		}
		if !created && (!bytes.Equal(sv.SignKey, conn.Remote.SignKey) || !bytes.Equal(sv.BoxKey, conn.Remote.BoxKey)) {
			// The name was linked before, with other keys.
			conn.CloseWith(wire.Frame{Frame: wire.FBye, Reason: "removed"})
			s.audit(ctx, s.store, by, "link.key_mismatch", "server", string(sv.ID), map[string]any{"name": inv.Name})
			return Peer{}, model.ErrConflict.With(inv.Name + " was linked before under different keys. Its old keys are still pinned.")
		}
		if sv.Blocked() {
			conn.CloseWith(wire.Frame{Frame: wire.FBye, Reason: "removed"})
			return Peer{}, model.ErrConflict.With(inv.Name + " is blocked on this server.")
		}
		p.ServerID = sv.ID
	}
	if err := s.store.Peers().Create(ctx, &p); err != nil {
		conn.Close()
		return Peer{}, err
	}
	s.audit(ctx, s.store, by, "link.peer_added", "peer", string(p.ID), map[string]any{"name": inv.Name, "kind": inv.Kind,
		"fingerprint": wire.KeyFingerprint(conn.Remote.SignKey)})
	s.confirmName(p, inv.Name, conn.Remote)
	s.startDiallingWith(p, conn)
	return s.peerView(ctx, p)
}

// SetPeerState pauses or resumes a peer.
func (s *Service) SetPeerState(ctx context.Context, by model.AccountID, id model.PeerID, state string) (Peer, error) {
	if state != store.PeerActive && state != store.PeerPaused {
		return Peer{}, model.Invalid("state", "state is active or paused.")
	}
	p, err := s.store.Peers().Update(ctx, id, func(p *store.Peer) error {
		if p.State != store.PeerActive && p.State != store.PeerPaused {
			return model.ErrNotFound
		}
		p.State = state
		return nil
	})
	if err != nil {
		return Peer{}, err
	}
	s.audit(ctx, s.store, by, "link.peer_"+state, "peer", string(id), nil)
	if state == store.PeerPaused {
		s.stopDialling(id)
		if c := s.connOf(id); c != nil {
			c.conn.CloseWith(wire.Frame{Frame: wire.FBye, Reason: "shutdown"})
		}
	} else if p.Dials {
		s.startDialling(p)
	}
	return s.peerView(ctx, p)
}

// RemovePeer removes a peer, or revokes an invitation nobody used. Streams
// this server does not own are left; streams it owns must first be handed
// over or closed.
func (s *Service) RemovePeer(ctx context.Context, by model.AccountID, id model.PeerID) error {
	p, err := s.store.Peers().Get(ctx, id)
	if err != nil {
		return err
	}
	if p.State == store.PeerInvited {
		if err := s.store.Peers().Delete(ctx, id); err != nil {
			return err
		}
		s.audit(ctx, s.store, by, "link.invitation_revoked", "peer", string(id), nil)
		return nil
	}
	if p.State == store.PeerRemoved {
		return model.ErrNotFound
	}
	if s.movingOn(ctx, id) {
		// A move leaves this link or arrives over it: the link stays until
		// the move is over.
		return errMoving
	}
	streams, err := s.store.Streams().List(ctx, "")
	if err != nil {
		return err
	}
	for _, st := range streams {
		if st.PeerID == id && st.State == store.StreamActive && st.Owned() && st.Kind == store.StreamChannel {
			return model.ErrConflict.With("This server owns shared channels on that link. Hand them over or stop sharing them first.")
		}
	}
	for _, st := range streams {
		if st.PeerID != id {
			continue
		}
		switch st.State {
		case store.StreamActive:
			if st.Owned() {
				// A cross-server ad-hoc channel or sealed chat: it
				// simply ends, and stays as a read-only copy.
				s.closeOwned(ctx, st)
			} else {
				s.leave(ctx, st)
			}
		case store.StreamOffered:
			s.store.Streams().Delete(ctx, st.ID)
		}
	}
	s.stopDialling(id)
	if c := s.connOf(id); c != nil {
		c.removed = true
		c.conn.CloseWith(wire.Frame{Frame: wire.FBye, Reason: "removed"})
	}
	if _, err := s.store.Peers().Update(ctx, id, func(p *store.Peer) error {
		p.State = store.PeerRemoved
		return nil
	}); err != nil {
		return err
	}
	s.audit(ctx, s.store, by, "link.peer_removed", "peer", string(id), nil)
	return nil
}

// ------------------------------------------------------------ connections

// peerConn is the one connection of a peer.
type peerConn struct {
	peer    store.Peer
	conn    *wire.Conn
	name    string // the other side's name
	hub     bool
	since   time.Time
	removed bool // a bye with reason removed went one way or the other
	// pinger measures the other side's clock, on a direct link, where this
	// server is the time reference of the streams it owns (clock.go). Nil
	// on a connection to a hub: there the hub does the measuring.
	pinger *wire.Pinger
}

func (s *Service) connOf(id model.PeerID) *peerConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conns[id]
}

// attach makes c the peer's connection. A connection already there is
// replaced and closed with an error frame, code replaced.
func (s *Service) attach(c *peerConn) {
	s.mu.Lock()
	old := s.conns[c.peer.ID]
	s.conns[c.peer.ID] = c
	s.mu.Unlock()
	if old != nil {
		old.conn.CloseWith(wire.ErrorFrame(0, wire.EReplaced, "A newer connection for this peer took over."))
	}
}

func (s *Service) detach(c *peerConn) {
	s.mu.Lock()
	if s.conns[c.peer.ID] == c {
		delete(s.conns, c.peer.ID)
	}
	s.mu.Unlock()
}

// nextWait is the dialler's wait before trying again: 1 second, doubled
// after each failure up to 5 minutes, with a random 20 per cent added or
// taken away. jitter is in [0, 1).
func nextWait(failures int, base time.Duration, jitter float64) time.Duration {
	wait := base
	for i := 1; i < failures && wait < 5*time.Minute; i++ {
		wait *= 2
	}
	if wait > 5*time.Minute {
		wait = 5 * time.Minute
	}
	return time.Duration(float64(wait) * (0.8 + 0.4*jitter))
}

// dialBase is the first wait between dials, in nanoseconds. Tests shorten it.
var dialBase atomic.Int64

func init() { dialBase.Store(int64(time.Second)) }

// startDialling begins keeping a connection to a peer this server dials.
func (s *Service) startDialling(p store.Peer) { s.startDiallingWith(p, nil) }

// startDiallingWith is startDialling with a connection that is already up:
// the one an invitation was just accepted on.
func (s *Service) startDiallingWith(p store.Peer, first *wire.Conn) {
	ctx, cancel := context.WithCancel(s.ctx)
	s.mu.Lock()
	if old := s.dialers[p.ID]; old != nil {
		old()
	}
	s.dialers[p.ID] = cancel
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		failures := 0
		conn := first
		for ctx.Err() == nil {
			if conn == nil {
				var err error
				s.count(func(st *Stats) { st.Dials[p.ID]++ })
				if conn, err = s.dial(ctx, p); err != nil {
					failures++
					s.noteError(p.ID, err)
					var refused *wire.Error
					if errors.As(err, &refused) && (refused.Code == wire.EBlocked || refused.Code == wire.EKeyMismatch) {
						// Trying again changes nothing; the owner must act.
						failures += 4
					}
					select {
					case <-ctx.Done():
						return
					case <-time.After(nextWait(failures, time.Duration(dialBase.Load()), rand.Float64())):
					}
					continue
				}
			}
			pc := &peerConn{peer: p, conn: conn, name: conn.Remote.Name, hub: conn.Remote.Kind == wire.KindHub, since: time.Now()}
			s.serve(ctx, pc)
			conn = nil
			if pc.removed {
				return
			}
			if time.Since(pc.since) >= time.Minute {
				failures = 0
			} else {
				failures++
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(nextWait(failures, time.Duration(dialBase.Load()), rand.Float64())):
			}
		}
	}()
}

func (s *Service) stopDialling(id model.PeerID) {
	s.mu.Lock()
	if cancel := s.dialers[id]; cancel != nil {
		cancel()
		delete(s.dialers, id)
	}
	s.mu.Unlock()
}

func (s *Service) noteError(id model.PeerID, err error) {
	msg := "The other side could not be reached."
	var refused *wire.Error
	if errors.As(err, &refused) {
		msg = refused.Code + ": " + refused.Message
	}
	s.store.Peers().Update(s.ctx, id, func(p *store.Peer) error {
		p.LastError = msg
		return nil
	})
}

// dial opens a later connection to a peer: no invitation, and the listener
// must show the pinned key.
func (s *Service) dial(ctx context.Context, p store.Peer) (*wire.Conn, error) {
	name, key := p.HubName, p.HubSignKey
	if p.Kind == wire.KindServer {
		sv, err := s.store.Peers().Server(ctx, p.ServerID)
		if err != nil {
			return nil, err
		}
		name, key = sv.Name, sv.SignKey
	}
	return wire.Dial(ctx, wire.DialOptions{URL: p.URL, Identity: s.id, HTTPClient: s.http, Check: func(h wire.Frame) error {
		if h.Name != name || h.Kind != p.Kind {
			return wire.Errf(wire.EAuthFailed, "The other side is not %s.", name)
		}
		if !bytes.Equal(h.SignKey, key) {
			return wire.Errf(wire.EKeyMismatch, "%s shows a key other than the pinned one.", name)
		}
		return nil
	}})
}

// ServeLink is the handler of /link/v1: another server dialling this one.
func (s *Service) ServeLink(w http.ResponseWriter, r *http.Request) {
	var peer store.Peer
	conn, err := wire.Accept(w, r, s.id, func(d wire.Frame, proof, th []byte) error {
		var err error
		peer, err = s.authorize(r.Context(), d, proof, th)
		return err
	})
	if err != nil {
		return
	}
	pc := &peerConn{peer: peer, conn: conn, name: conn.Remote.Name, since: time.Now()}
	s.wg.Add(1)
	defer s.wg.Done()
	s.serve(s.ctx, pc)
}

// authorize is what the listener checks on a dialler whose signatures have
// verified (section 5, "What the listener checks").
func (s *Service) authorize(ctx context.Context, d wire.Frame, proof, th []byte) (store.Peer, error) {
	var none store.Peer
	if d.Name == s.id.Name {
		return none, wire.Errf(wire.EAuthFailed, "That is this server's own name.")
	}
	peers, err := s.store.Peers().List(ctx)
	if err != nil {
		return none, err
	}
	known, err := s.store.Peers().ServerByName(ctx, d.Name)
	pinned := err == nil
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return none, err
	}
	if pinned && known.Blocked() {
		return none, wire.Errf(wire.EBlocked, "This server accepts nothing from %s.", d.Name)
	}
	sameKeys := pinned && bytes.Equal(known.SignKey, d.SignKey) && bytes.Equal(known.BoxKey, d.BoxKey)
	if pinned {
		for _, p := range peers {
			if p.Kind != wire.KindServer || p.ServerID != known.ID {
				continue
			}
			switch p.State {
			case store.PeerActive:
				if p.Dials {
					return none, wire.Errf(wire.EAuthFailed, "This server dials %s, not the other way round.", d.Name)
				}
				if !sameKeys {
					return none, wire.Errf(wire.EKeyMismatch, "%s is pinned with other keys.", d.Name)
				}
				return p, nil
			case store.PeerPaused:
				return none, wire.Errf(wire.EUnavailable, "The link is paused.")
			}
		}
	}
	// Not an active peer: the proof must match an invitation that is
	// unused and not expired.
	now := s.now()
	var match *store.Peer
	expired := false
	for i, p := range peers {
		if p.State != store.PeerInvited || len(p.InviteHash) == 0 || !wire.ProofMatches(p.InviteHash, th, proof) {
			continue
		}
		if p.InviteExpiresAt.Before(now) {
			expired = true
			continue
		}
		match = &peers[i]
	}
	if match == nil {
		if expired {
			return none, wire.Errf(wire.EInviteExpired, "That invitation has expired.")
		}
		return none, wire.Errf(wire.EInviteInvalid, "No invitation matches.")
	}
	if pinned && !sameKeys {
		// The name is pinned from an earlier link, with other keys. The
		// owner is warned; the invitation stays unused.
		s.audit(ctx, s.store, "", "link.key_mismatch", "server", string(known.ID), map[string]any{"name": d.Name})
		return none, wire.Errf(wire.EKeyMismatch, "%s is pinned with other keys.", d.Name)
	}
	sv, _, err := s.store.Peers().PinServer(ctx, store.Server{Name: d.Name, SignKey: d.SignKey, BoxKey: d.BoxKey})
	if err != nil {
		return none, err
	}
	// The invitation is used up, the keys are pinned, the peer is active.
	p, err := s.store.Peers().Update(ctx, match.ID, func(p *store.Peer) error {
		if p.State != store.PeerInvited {
			return wire.Errf(wire.EInviteInvalid, "That invitation has been used.")
		}
		p.State, p.ServerID, p.InviteHash, p.InviteExpiresAt = store.PeerActive, sv.ID, nil, 0
		return nil
	})
	if err != nil {
		return none, err
	}
	s.audit(ctx, s.store, "", "link.peer_added", "peer", string(p.ID), map[string]any{"name": d.Name, "kind": wire.KindServer,
		"fingerprint": wire.KeyFingerprint(d.SignKey)})
	s.confirmName(p, d.Name, d)
	return p, nil
}

// confirmName fetches the other side's /.well-known/churr once after a
// first handshake and compares name and keys. A mismatch removes the new
// peer and tells the owner; an address that cannot be reached is recorded as
// "name not confirmed" and nothing else happens.
func (s *Service) confirmName(p store.Peer, name string, hello wire.Frame) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()
		doc, err := s.fetchWellKnown(ctx, name, p.URL)
		if err != nil {
			s.audit(ctx, s.store, "", "link.name_not_confirmed", "peer", string(p.ID), map[string]any{"name": name})
			return
		}
		if doc.Check(name) == nil && bytes.Equal(doc.SignKey, hello.SignKey) && bytes.Equal(doc.BoxKey, hello.BoxKey) {
			return
		}
		s.audit(ctx, s.store, "", "link.name_mismatch", "peer", string(p.ID), map[string]any{"name": name})
		s.stopDialling(p.ID)
		if c := s.connOf(p.ID); c != nil {
			c.removed = true
			c.conn.CloseWith(wire.Frame{Frame: wire.FBye, Reason: "removed"})
		}
		s.store.Peers().Update(ctx, p.ID, func(p *store.Peer) error {
			p.State, p.LastError = store.PeerRemoved, "What "+name+" publishes at its own address does not match the keys it showed."
			return nil
		})
	}()
}

// fetchWellKnown reads a server's published identity. dialURL, when known,
// says where it listens, which in a local trial is not port 443.
func (s *Service) fetchWellKnown(ctx context.Context, name, dialURL string) (wire.WellKnown, error) {
	if s.wellKnown != nil {
		return s.wellKnown(ctx, name)
	}
	var doc wire.WellKnown
	address := "https://" + name + "/.well-known/churr"
	if u, err := url.Parse(dialURL); err == nil && dialURL != "" {
		scheme := "https"
		if u.Scheme == "ws" {
			scheme = "http"
		}
		address = scheme + "://" + u.Host + "/.well-known/churr"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return doc, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return doc, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return doc, errors.New("link: no document at " + address)
	}
	err = json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<16)).Decode(&doc)
	return doc, err
}

// ServeWellKnown is the handler of /.well-known/churr.
func (s *Service) ServeWellKnown(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "max-age=3600")
	json.NewEncoder(w).Encode(s.id.WellKnown())
}

// serve runs one authenticated connection until it ends.
func (s *Service) serve(ctx context.Context, pc *peerConn) {
	s.attach(pc)
	defer s.detach(pc)
	defer pc.conn.Close()
	go pc.conn.Watch()
	s.store.Peers().Update(ctx, pc.peer.ID, func(p *store.Peer) error {
		p.ConnectedAt, p.LastError = s.now(), ""
		return nil
	})
	// The stretch the peer was away for is noted before anything of its is
	// read, and before its clock is measured again: the stretch begins at
	// the last measurement of the connection before this one. What the peer
	// made in that time arrives now, with old times.
	s.noteAway(ctx, pc)
	s.startMeasuring(pc)
	// What follows ready (section 5): catch up on every active stream,
	// then submit what waits. A hub's roster arrives by itself.
	go s.afterReady(ctx, pc)
	for {
		f, _, err := pc.conn.Read(ctx)
		if err != nil {
			var bad *wire.Error
			if errors.As(err, &bad) {
				pc.conn.Send(bad.Frame(0))
				continue
			}
			return
		}
		switch f.Frame {
		case wire.FPing:
			// The arrival time is read before anything else is done.
			s.onPing(ctx, pc, f, s.nowMs())
			continue
		case wire.FPong:
			if pc.pinger != nil {
				pc.pinger.Pong(f)
			}
			continue
		}
		if stop := s.dispatch(ctx, pc, f); stop {
			return
		}
	}
}

// dispatch handles one frame. It reports true when the connection is over.
func (s *Service) dispatch(ctx context.Context, pc *peerConn, f wire.Frame) bool {
	var err error
	switch f.Frame {
	case wire.FRoster:
		err = s.onRoster(ctx, pc, f)
	case wire.FCluster:
		err = s.onCluster(ctx, pc, f)
	case wire.FChannel:
		err = s.onChannel(ctx, pc, f)
	case wire.FDirectory:
		if !s.answerFrame(pc.peer.ID, f) {
			err = wire.Errf(wire.EBadFrame, "Nobody asked for a directory.")
		}
	case wire.FStreamOpen, wire.FServerJoined, wire.FServerLeft, wire.FLeave, wire.FOwnerChanged, wire.FStreamClose:
		err = s.onControl(ctx, pc, f)
	case wire.FSubmit:
		err = s.onSubmit(ctx, pc, f)
	case wire.FAccepted:
		s.onAccepted(ctx, pc, f)
	case wire.FDeliver:
		s.onDeliver(ctx, pc, f)
	case wire.FAck:
		s.onAck(ctx, pc, f)
	case wire.FCatchUp:
		err = s.onCatchUp(ctx, pc, f)
	case wire.FCaughtUp:
		s.onCaughtUp(ctx, pc, f)
	case wire.FEphemeral:
		err = s.onEphemeral(ctx, pc, f)
	case wire.FDirect:
		err = s.onDirect(ctx, pc, f)
	case wire.FError:
		s.onError(ctx, pc, f)
		if !s.answerFrame(pc.peer.ID, f) && pc.hub && f.StreamID != "" &&
			(f.Code == wire.EForbidden || f.Code == wire.EUnknownStream || f.Code == wire.ENotFound) {
			// A hub's no to a wish to join one of its channels.
			s.joinRefused(ctx, pc, f)
		}
		return f.Code == wire.EReplaced || f.Code == wire.EBlocked
	case wire.FBye:
		if f.Reason == "removed" {
			pc.removed = true
			s.peerRemovedUs(ctx, pc)
		}
		return true
	case wire.FHello, wire.FAuth, wire.FReady:
		err = wire.Errf(wire.EBadFrame, "%s is out of place after the handshake.", f.Frame)
	default:
		err = wire.Errf(wire.EBadFrame, "Unknown frame %q.", f.Frame)
	}
	if err != nil {
		e := wire.AsError(err)
		if e.Code == wire.EInternal {
			s.log.Warn("link: a frame could not be handled", "frame", f.Frame, "peer", pc.name, "err", err)
		}
		pc.conn.Send(e.Frame(f.ID))
	}
	return false
}

// onError notes an error frame from the other side. A submit is answered by
// an accepted frame, which names any refusal itself (onAccepted); an error
// frame that answers one means the whole frame was not understood, and the
// outbox tries again later.
func (s *Service) onError(ctx context.Context, pc *peerConn, f wire.Frame) {
	s.count(func(st *Stats) { st.Dropped["error:"+f.Code]++ })
	s.mu.Lock()
	s.lastError = f
	s.mu.Unlock()
	s.log.Debug("link: the other side refused something", "peer", pc.name, "code", f.Code, "message", f.Message, "re", f.Re)
	s.catchUpRefused(pc.peer.ID, f.Re)
}

// peerRemovedUs is what happens when the other side says bye with reason
// removed: the peer is marked removed here too and its streams detach.
func (s *Service) peerRemovedUs(_ context.Context, pc *peerConn) {
	// The connection's own context ends with its dialler, which stops
	// here; what follows must still be written down.
	ctx := s.ctx
	s.stopDialling(pc.peer.ID)
	streams, _ := s.store.Streams().List(ctx, store.StreamActive)
	for _, st := range streams {
		if st.PeerID == pc.peer.ID {
			s.detachStream(ctx, st, store.StreamRemoved)
		}
	}
	s.store.Peers().Update(ctx, pc.peer.ID, func(p *store.Peer) error {
		p.State, p.LastError = store.PeerRemoved, "The other side removed the link."
		return nil
	})
	s.audit(ctx, s.store, "", "link.peer_removed_by_other", "peer", string(pc.peer.ID), map[string]any{"name": pc.name})
}

// afterReady does what follows a new connection's ready frame.
func (s *Service) afterReady(ctx context.Context, pc *peerConn) {
	streams, err := s.store.Streams().List(ctx, store.StreamActive)
	if err != nil {
		return
	}
	for _, st := range streams {
		if st.PeerID != pc.peer.ID || st.Sequencer == store.SequencerSelf {
			continue
		}
		s.catchUp(ctx, st, true)
	}
	s.kickOutbox(ctx, pc.peer.ID)
	s.resendOffersTo(ctx, pc.peer.ID)
	if pc.hub {
		s.resendJoins(ctx, pc.peer.ID)
	}
	s.resendMoves(ctx, pc.peer)
	// Blocks go before any presence, so none is shown that a block hides.
	s.deafEarsToPeer(ctx, pc.peer.ID, true)
	s.spreadSnapshots(ctx, pc.peer.ID)
}

// ----------------------------------------------------------------- roster

// onRoster applies a hub's roster: the servers reachable through it, and
// their keys.
func (s *Service) onRoster(ctx context.Context, pc *peerConn, f wire.Frame) error {
	if !pc.hub {
		return wire.Errf(wire.EBadFrame, "Only a hub sends a roster.")
	}
	var ro wire.Roster
	if err := wire.VerifyBlock(f, pc.peer.HubSignKey, &ro); err != nil {
		return err
	}
	s.mu.Lock()
	have := s.rosters[pc.peer.ID]
	s.mu.Unlock()
	if ro.Hub != pc.peer.HubName || ro.Version <= have {
		return nil // not newer than the one held
	}
	// A later roster that gives a pinned name different keys is refused as
	// a whole and the owner is told.
	for _, rs := range ro.Servers {
		if rs.Name == s.id.Name {
			continue
		}
		if !wire.VerifyBoxKey(rs.Name, rs.SignKey, rs.BoxKey, rs.BoxKeySig) {
			s.audit(ctx, s.store, "", "link.roster_refused", "peer", string(pc.peer.ID), map[string]any{"name": rs.Name, "why": "box key signature"})
			return nil
		}
		known, err := s.store.Peers().ServerByName(ctx, rs.Name)
		if err == nil && (!bytes.Equal(known.SignKey, rs.SignKey) || !bytes.Equal(known.BoxKey, rs.BoxKey)) {
			s.audit(ctx, s.store, "", "link.roster_refused", "peer", string(pc.peer.ID),
				map[string]any{"name": rs.Name, "why": "the roster gives a pinned server other keys"})
			s.count(func(st *Stats) { st.Dropped["roster_refused"]++ })
			return nil
		}
	}
	before, err := s.store.Peers().Reachable(ctx, pc.peer.ID)
	if err != nil {
		return err
	}
	var ids []model.ServerID
	names := map[string]bool{}
	for _, rs := range ro.Servers {
		if rs.Name == s.id.Name {
			continue
		}
		sv, created, err := s.store.Peers().PinServer(ctx, store.Server{Name: rs.Name, SignKey: rs.SignKey, BoxKey: rs.BoxKey})
		if err != nil {
			return err
		}
		ids = append(ids, sv.ID)
		names[rs.Name] = true
		if created {
			s.confirmRosterName(pc.peer, sv)
		}
	}
	if err := s.store.Peers().SetReachable(ctx, pc.peer.ID, ids); err != nil {
		return err
	}
	s.mu.Lock()
	s.rosters[pc.peer.ID] = ro.Version
	s.mu.Unlock()
	// A channel's list may have named a server this one had not met.
	s.reapplyRecords(ctx, pc.peer.ID)
	// A name that disappears has been removed from the hub: the owner of
	// each stream it was in removes it, without being asked.
	for _, sv := range before {
		if !names[sv.Name] {
			go s.serverGone(s.ctx, pc.peer.ID, sv)
		}
	}
	return nil
}

// confirmRosterName compares a server a hub listed with what that server
// publishes itself, where it can be fetched. A mismatch blocks the server
// and tells the owner.
func (s *Service) confirmRosterName(p store.Peer, sv store.Server) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()
		doc, err := s.fetchWellKnown(ctx, sv.Name, "")
		if err != nil {
			return
		}
		if doc.Check(sv.Name) == nil && bytes.Equal(doc.SignKey, sv.SignKey) && bytes.Equal(doc.BoxKey, sv.BoxKey) {
			return
		}
		s.store.Peers().SetServerBlocked(ctx, sv.ID, s.now())
		s.audit(ctx, s.store, "", "link.server_blocked", "server", string(sv.ID),
			map[string]any{"name": sv.Name, "why": "the hub's roster does not match what the server publishes"})
	}()
}

// hostOf returns the host of a link address, for messages.
func hostOf(address string) string {
	u, err := url.Parse(address)
	if err != nil {
		return address
	}
	return strings.ToLower(u.Hostname())
}
