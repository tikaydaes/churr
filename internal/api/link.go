package api

import (
	"net/http"
	"time"

	"github.com/tikaydaes/churr/internal/link"
	"github.com/tikaydaes/churr/internal/model"
)

// The owner's operations on links to other servers ("Admin: linked
// servers"), and the two paths outside /api/v1 that docs/LINK-PROTOCOL.md
// fixes: /.well-known/churr, where this server publishes its name and keys,
// and /link/v1, where another server's link connection arrives.

func init() {
	register(func(a *API) {
		a.handle("listPeers", a.listPeers)
		a.handle("createPeerInvitation", a.createPeerInvitation)
		a.handle("inspectPeerInvitation", a.inspectPeerInvitation)
		a.handle("acceptPeerInvitation", a.acceptPeerInvitation)
		a.handle("getPeer", a.getPeer)
		a.handle("updatePeer", a.updatePeer)
		a.handle("removePeer", a.removePeer)
		a.handle("listKnownServers", a.listKnownServers)
		a.handle("blockServer", func(r *Request) error { return a.blockServer(r, true) })
		a.handle("unblockServer", func(r *Request) error { return a.blockServer(r, false) })
		a.handle("listStreams", a.listStreams)
		a.handle("shareChannel", a.shareChannel)
		a.handle("acceptStream", a.acceptStream)
		a.handle("declineStream", a.declineStream)
		a.handle("leaveStream", a.leaveStream)
		a.handle("addStreamServer", a.addStreamServer)
		a.handle("removeStreamServer", a.removeStreamServer)
		a.handle("transferStream", a.transferStream)
		a.handle("updateStream", a.updateStream)
		a.handle("closeStream", a.closeStream)

		// /link/v1 is not an operation of the contract: other servers use
		// it, not clients, and each proves who it is by its keys.
		// /.well-known/churr is the contract's getWellKnown, which
		// server_ops.go answers with the link's document.
		if a.mux == nil {
			return // a table built only to list handlers has no routes
		}
		a.mux.HandleFunc("GET /link/v1", func(w http.ResponseWriter, r *http.Request) {
			if a.Link == nil {
				writeError(w, a.Log, model.ErrNotFound)
				return
			}
			a.Link.ServeLink(w, r)
		})
	})
}

// links returns the link service once the caller is known to be the owner.
func (a *API) links(r *Request) (*link.Service, error) {
	if a.Link == nil {
		return nil, model.ErrUnavailable
	}
	return a.Link, a.Link.Allowed(r.Ctx(), r.Actor())
}

func (a *API) listPeers(r *Request) error {
	l, err := a.links(r)
	if err != nil {
		return err
	}
	peers, err := l.Peers(r.Ctx())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"peers": orEmpty(peers)})
}

func (a *API) createPeerInvitation(r *Request) error {
	l, err := a.links(r)
	if err != nil {
		return err
	}
	var body struct {
		ExpiresInSeconds *int64 `json:"expires_in_seconds"`
	}
	// The body is optional: with none, the invitation lasts seven days.
	if r.R.ContentLength != 0 {
		if err := r.Decode(&body); err != nil {
			return err
		}
	}
	expires := time.Duration(0)
	if body.ExpiresInSeconds != nil {
		if *body.ExpiresInSeconds <= 0 || *body.ExpiresInSeconds > 7*24*3600 {
			return model.Invalid("expires_in_seconds", "An invitation lasts at most seven days.")
		}
		expires = time.Duration(*body.ExpiresInSeconds) * time.Second
	}
	peer, invitation, err := l.CreateInvitation(r.Ctx(), r.Actor().ID, expires)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"peer": peer, "invitation": invitation, "expires_at": peer.InviteExpiresAt})
}

func (a *API) inspectPeerInvitation(r *Request) error {
	l, err := a.links(r)
	if err != nil {
		return err
	}
	var body struct {
		Invitation string `json:"invitation"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	info, _, err := l.InspectInvitation(body.Invitation)
	if err != nil {
		return err
	}
	return r.JSON(info)
}

func (a *API) acceptPeerInvitation(r *Request) error {
	l, err := a.links(r)
	if err != nil {
		return err
	}
	var body struct {
		Invitation string `json:"invitation"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	peer, err := l.AcceptInvitation(r.Ctx(), r.Actor().ID, body.Invitation)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"peer": peer})
}

func (a *API) getPeer(r *Request) error {
	l, err := a.links(r)
	if err != nil {
		return err
	}
	id, err := PathID[model.PeerID](r, "id")
	if err != nil {
		return err
	}
	peer, err := l.PeerByID(r.Ctx(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"peer": peer})
}

func (a *API) updatePeer(r *Request) error {
	l, err := a.links(r)
	if err != nil {
		return err
	}
	id, err := PathID[model.PeerID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		State string `json:"state"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	peer, err := l.SetPeerState(r.Ctx(), r.Actor().ID, id, body.State)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"peer": peer})
}

func (a *API) removePeer(r *Request) error {
	l, err := a.links(r)
	if err != nil {
		return err
	}
	id, err := PathID[model.PeerID](r, "id")
	if err != nil {
		return err
	}
	if err := l.RemovePeer(r.Ctx(), r.Actor().ID, id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listKnownServers(r *Request) error {
	l, err := a.links(r)
	if err != nil {
		return err
	}
	servers, err := l.KnownServers(r.Ctx())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"servers": orEmpty(servers)})
}

func (a *API) blockServer(r *Request, block bool) error {
	l, err := a.links(r)
	if err != nil {
		return err
	}
	id, err := PathID[model.ServerID](r, "id")
	if err != nil {
		return err
	}
	if err := l.BlockServer(r.Ctx(), r.Actor().ID, id, block); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listStreams(r *Request) error {
	l, err := a.links(r)
	if err != nil {
		return err
	}
	state := r.R.URL.Query().Get("state")
	switch state {
	case "", "offered", "active", "left", "removed":
	default:
		return model.Invalid("state", "state is offered, active, left or removed.")
	}
	streams, err := l.Streams(r.Ctx(), state)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"streams": orEmpty(streams)})
}

func (a *API) shareChannel(r *Request) error {
	l, err := a.links(r)
	if err != nil {
		return err
	}
	var body struct {
		ConversationID string   `json:"conversation_id"`
		PeerID         string   `json:"peer_id"`
		ServerIDs      []string `json:"server_ids"`
		HistoryDays    int      `json:"history_days"`
		MaxBlobBytes   int64    `json:"max_blob_bytes"`
		Listing        string   `json:"listing"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	in := link.ShareInput{HistoryDays: body.HistoryDays, MaxBlobBytes: body.MaxBlobBytes, Listing: body.Listing}
	if in.ConversationID, err = BodyID[model.ConversationID]("conversation_id", body.ConversationID); err != nil {
		return err
	}
	if in.PeerID, err = BodyID[model.PeerID]("peer_id", body.PeerID); err != nil {
		return err
	}
	for _, raw := range body.ServerIDs {
		id, err := BodyID[model.ServerID]("server_ids", raw)
		if err != nil {
			return err
		}
		in.ServerIDs = append(in.ServerIDs, id)
	}
	stream, err := l.Share(r.Ctx(), r.Actor(), in)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"stream": stream})
}

// streamID reads the stream named in the path once the caller is known to
// be the owner.
func (a *API) streamID(r *Request) (*link.Service, model.StreamID, error) {
	l, err := a.links(r)
	if err != nil {
		return nil, "", err
	}
	id, err := PathID[model.StreamID](r, "id")
	return l, id, err
}

func (a *API) acceptStream(r *Request) error {
	l, id, err := a.streamID(r)
	if err != nil {
		return err
	}
	var body struct {
		Name      string `json:"name"`
		Private   bool   `json:"private"`
		SectionID string `json:"section_id"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	in := link.AcceptInput{Name: body.Name, Private: body.Private}
	if body.SectionID != "" {
		if in.SectionID, err = BodyID[model.SectionID]("section_id", body.SectionID); err != nil {
			return err
		}
	}
	stream, conv, err := l.Accept(r.Ctx(), r.Actor(), id, in)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"stream": stream, "conversation": conv})
}

func (a *API) declineStream(r *Request) error {
	l, id, err := a.streamID(r)
	if err != nil {
		return err
	}
	if err := l.Decline(r.Ctx(), r.Actor().ID, id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) leaveStream(r *Request) error {
	l, id, err := a.streamID(r)
	if err != nil {
		return err
	}
	if err := l.Leave(r.Ctx(), r.Actor().ID, id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) addStreamServer(r *Request) error {
	l, id, err := a.streamID(r)
	if err != nil {
		return err
	}
	server, err := PathID[model.ServerID](r, "server_id")
	if err != nil {
		return err
	}
	stream, err := l.AddServer(r.Ctx(), r.Actor().ID, id, server)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"stream": stream})
}

func (a *API) removeStreamServer(r *Request) error {
	l, id, err := a.streamID(r)
	if err != nil {
		return err
	}
	server, err := PathID[model.ServerID](r, "server_id")
	if err != nil {
		return err
	}
	stream, err := l.RemoveServer(r.Ctx(), r.Actor().ID, id, server)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"stream": stream})
}

func (a *API) transferStream(r *Request) error {
	l, id, err := a.streamID(r)
	if err != nil {
		return err
	}
	var body struct {
		ServerID string `json:"server_id"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	server, err := BodyID[model.ServerID]("server_id", body.ServerID)
	if err != nil {
		return err
	}
	stream, err := l.Transfer(r.Ctx(), r.Actor().ID, id, server)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"stream": stream})
}

func (a *API) updateStream(r *Request) error {
	l, id, err := a.streamID(r)
	if err != nil {
		return err
	}
	var body struct {
		MaxBlobBytes int64 `json:"max_blob_bytes"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	stream, err := l.Update(r.Ctx(), r.Actor().ID, id, body.MaxBlobBytes)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"stream": stream})
}

func (a *API) closeStream(r *Request) error {
	l, id, err := a.streamID(r)
	if err != nil {
		return err
	}
	if err := l.Close(r.Ctx(), r.Actor().ID, id); err != nil {
		return err
	}
	return r.NoContent()
}
