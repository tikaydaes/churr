package api

import (
	"time"

	"github.com/tikaydaes/churr/internal/link"
	"github.com/tikaydaes/churr/internal/model"
)

// Channels a hub owns (docs/LINK-PROTOCOL.md section 23a): the hub's
// directory and joining from it, which are the owner's, and the record and
// the managers' actions, which are for whoever the hub's signed manager
// list names. The rule for each is the link package's and internal/perm's;
// these handlers only read the request.

func init() {
	register(func(a *API) {
		a.handle("listHubDirectory", a.listHubDirectory)
		a.handle("joinHubChannel", a.joinHubChannel)
		a.handle("getHubChannel", a.getHubChannel)
		a.handle("moderateHubChannel", a.moderateHubChannel)
		a.handle("askToJoinHubChannel", a.askToJoinHubChannel)
		a.handle("listHubChannelRequests", a.listHubChannelRequests)
		a.handle("listHubChannelCandidates", a.listHubChannelCandidates)
		a.handle("moderateHubChannelMember", a.moderateHubChannelMember)
		a.handle("proposeStreamMove", a.proposeStreamMove)
		a.handle("listStreamMoves", a.listStreamMoves)
		a.handle("acceptStreamMove", func(r *Request) error { return a.answerStreamMove(r, "accept") })
		a.handle("declineStreamMove", func(r *Request) error { return a.answerStreamMove(r, "decline") })
		a.handle("cancelStreamMove", func(r *Request) error { return a.answerStreamMove(r, "cancel") })
	})
}

func (a *API) listHubDirectory(r *Request) error {
	l, err := a.links(r)
	if err != nil {
		return err
	}
	id, err := PathID[model.PeerID](r, "id")
	if err != nil {
		return err
	}
	channels, err := l.Directory(r.Ctx(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"channels": orEmpty(channels)})
}

func (a *API) joinHubChannel(r *Request) error {
	l, err := a.links(r)
	if err != nil {
		return err
	}
	peer, err := PathID[model.PeerID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		StreamID  string `json:"stream_id"`
		Name      string `json:"name"`
		Private   bool   `json:"private"`
		SectionID string `json:"section_id"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	stream, err := BodyID[model.StreamID]("stream_id", body.StreamID)
	if err != nil {
		return err
	}
	in := link.AcceptInput{Name: body.Name, Private: body.Private}
	if body.SectionID != "" {
		if in.SectionID, err = BodyID[model.SectionID]("section_id", body.SectionID); err != nil {
			return err
		}
	}
	st, conv, err := l.JoinListed(r.Ctx(), r.Actor(), peer, stream, in)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"stream": st, "conversation": conv})
}

// hubChannel is the link service and the stream a channel operation names.
// Unlike the admin operations these are not the owner's alone: the link
// service decides who may see the record and who is a manager.
func (a *API) hubChannel(r *Request) (*link.Service, model.StreamID, error) {
	if a.Link == nil {
		return nil, "", model.ErrUnavailable
	}
	id, err := PathID[model.StreamID](r, "id")
	return a.Link, id, err
}

func (a *API) getHubChannel(r *Request) error {
	l, id, err := a.hubChannel(r)
	if err != nil {
		return err
	}
	ch, err := l.Channel(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"channel": ch, "manager": l.IsManager(r.Ctx(), r.Actor(), id), "moderator": l.IsModerator(r.Ctx(), r.Actor(), id)})
}

func (a *API) moderateHubChannel(r *Request) error {
	l, id, err := a.hubChannel(r)
	if err != nil {
		return err
	}
	var body struct {
		Action      string  `json:"action"`
		Server      string  `json:"server"`
		AccountID   string  `json:"account_id"`
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Listing     string  `json:"listing"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	in := link.ModerateInput{Action: body.Action, Server: body.Server, Name: body.Name, Description: body.Description, Listing: body.Listing}
	if body.AccountID != "" {
		if in.AccountID, err = BodyID[model.AccountID]("account_id", body.AccountID); err != nil {
			return err
		}
	}
	ch, err := l.Moderate(r.Ctx(), r.Actor(), id, in)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"channel": ch})
}

// Who is in a private channel on a hub (ruling R94): its moderators, of any
// instance in it, admit and remove people; anyone of an instance in it may
// ask, with the channel's identifier a moderator gave them.

func (a *API) askToJoinHubChannel(r *Request) error {
	l, id, err := a.hubChannel(r)
	if err != nil {
		return err
	}
	p, err := l.AskToJoin(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"request": p})
}

func (a *API) listHubChannelRequests(r *Request) error {
	l, id, err := a.hubChannel(r)
	if err != nil {
		return err
	}
	list, err := l.JoinRequests(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"requests": orEmpty(list)})
}

func (a *API) listHubChannelCandidates(r *Request) error {
	l, id, err := a.hubChannel(r)
	if err != nil {
		return err
	}
	list, err := l.AdmitCandidates(r.Ctx(), r.Actor(), id, r.R.URL.Query().Get("q"))
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"people": orEmpty(list)})
}

func (a *API) moderateHubChannelMember(r *Request) error {
	l, id, err := a.hubChannel(r)
	if err != nil {
		return err
	}
	var body struct {
		Action    string `json:"action"`
		AccountID string `json:"account_id"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	account, err := BodyID[model.AccountID]("account_id", body.AccountID)
	if err != nil {
		return err
	}
	if err := l.MemberAction(r.Ctx(), r.Actor(), id, body.Action, account); err != nil {
		return err
	}
	return r.NoContent()
}

// Moving a channel shared over a direct link onto a hub (section 21a). The
// owner of the server that owns the stream proposes; the other server's
// owner accepts or declines; the rest runs by itself.

func (a *API) proposeStreamMove(r *Request) error {
	l, id, err := a.streamID(r)
	if err != nil {
		return err
	}
	var body struct {
		PeerID           string `json:"peer_id"`
		ExpiresInSeconds *int64 `json:"expires_in_seconds"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	peer, err := BodyID[model.PeerID]("peer_id", body.PeerID)
	if err != nil {
		return err
	}
	expires := time.Duration(0)
	if body.ExpiresInSeconds != nil {
		if *body.ExpiresInSeconds < 60 || *body.ExpiresInSeconds > 7*24*3600 {
			return model.Invalid("expires_in_seconds", "A proposal stands for a minute to seven days.")
		}
		expires = time.Duration(*body.ExpiresInSeconds) * time.Second
	}
	mv, err := l.ProposeMove(r.Ctx(), r.Actor(), id, peer, expires)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"move": mv})
}

func (a *API) listStreamMoves(r *Request) error {
	l, err := a.links(r)
	if err != nil {
		return err
	}
	moves, err := l.Moves(r.Ctx())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"moves": orEmpty(moves)})
}

func (a *API) answerStreamMove(r *Request, what string) error {
	l, err := a.links(r)
	if err != nil {
		return err
	}
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	var mv link.Move
	switch what {
	case "accept":
		mv, err = l.AcceptMove(r.Ctx(), r.Actor(), id)
	case "decline":
		mv, err = l.DeclineMove(r.Ctx(), r.Actor(), id)
	default:
		mv, err = l.CancelMove(r.Ctx(), r.Actor(), id)
	}
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"move": mv})
}
