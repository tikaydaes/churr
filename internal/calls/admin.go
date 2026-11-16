package calls

import (
	"context"
	"sort"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
)

// LiveCallInfo is a call in progress as the admin console shows it: how
// many people, not who (schema LiveCall).
type LiveCallInfo struct {
	ID               model.CallID `json:"id"`
	StartedAt        model.Time   `json:"started_at"`
	ParticipantCount int          `json:"participant_count"`
	CameraCount      int          `json:"camera_count"`
	// ConversationID is given only for a call in a public channel.
	ConversationID model.ConversationID `json:"conversation_id,omitempty"`
}

// admin answers whether the actor may use the admin console.
func admin(actor model.Account) error {
	return perm.CheckServer(actor, perm.UseAdminConsole, perm.DefaultSettings())
}

// MediaAddress is the address and port call media uses, as clients are
// told it, or "" when the server runs without calls or knows no address.
func (s *Server) MediaAddress(ctx context.Context) string {
	if s.media == nil {
		return ""
	}
	return s.media.address(ctx)
}

// MediaWarning is the admin Overview's warning while clients are told only
// private media addresses, or "".
func (s *Server) MediaWarning(ctx context.Context) string {
	if s.media == nil {
		return ""
	}
	return s.media.privateWarning(ctx)
}

// LiveCalls lists the calls in progress for an admin, oldest first, with
// the media address.
func (s *Server) LiveCalls(ctx context.Context, actor model.Account) ([]LiveCallInfo, string, error) {
	if err := admin(actor); err != nil {
		return nil, "", err
	}
	out := []LiveCallInfo{}
	for _, lc := range s.allCalls() {
		info := LiveCallInfo{ID: lc.id, StartedAt: lc.startedAt}
		if lc.public {
			info.ConversationID = lc.conv
		}
		switch {
		case lc.room != nil:
			info.ParticipantCount, info.CameraCount = lc.room.counts()
		case lc.away != nil:
			info.ParticipantCount = len(lc.away.list())
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt != out[j].StartedAt {
			return out[i].StartedAt < out[j].StartedAt
		}
		return out[i].ID < out[j].ID
	})
	return out, s.MediaAddress(ctx), nil
}

// counts returns how many participants are in the room and how many
// cameras are on.
func (r *room) counts() (people, cameras int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.tracks {
		if t.source == sourceCamera && !t.muted.Load() {
			cameras++
		}
	}
	return len(r.parts), cameras
}

// testFor is how long the connection of a call test is kept.
const testFor = 30 * time.Second

// Test is the admin console's "Test calls": the admin's browser makes an
// offer, the server answers it on the real media port, and the browser
// then sees for itself which of UDP and TCP got through and to which
// address. Nothing is sent or received on the connection, which is closed
// after half a minute.
func (s *Server) Test(ctx context.Context, actor model.Account, offer string) (answer, mediaAddress string, err error) {
	if err := admin(actor); err != nil {
		return "", "", err
	}
	if err := s.enabled(ctx); err != nil {
		return "", "", err
	}
	if offer == "" || len(offer) > maxSDP {
		return "", "", model.Invalid("sdp", "Give the browser's offer as `sdp`.")
	}
	pc, err := s.media.newSend()
	if err != nil {
		return "", "", err
	}
	time.AfterFunc(testFor, func() { pc.Close() })
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: withoutCandidates(offer)}); err != nil {
		pc.Close()
		return "", "", model.Invalid("sdp", "The server cannot use that offer.")
	}
	desc, err := pc.CreateAnswer(nil)
	if err == nil {
		answer, err = s.media.describe(ctx, pc, desc)
	}
	if err != nil {
		pc.Close()
		return "", "", model.Invalid("sdp", "The server cannot answer that offer.")
	}
	return answer, s.media.address(ctx), nil
}
