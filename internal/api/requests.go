package api

import (
	"github.com/tikaydaes/churr/internal/model"
)

// Chat requests, invitations and closing a chat (rulings R77, R78, R79,
// R103). The chat service keeps them; blocking from a request is the
// people service's block.

func init() {
	register(func(a *API) {
		a.handle("listChatRequests", a.listChatRequests)
		a.handle("acceptChatRequest", a.acceptChatRequest)
		a.handle("declineChatRequest", a.declineChatRequest)
		a.handle("dismissChatRequest", a.dismissChatRequest)
		a.handle("blockChatRequester", a.blockChatRequester)
		a.handle("closeChat", a.closeChat)
		a.handle("withdrawChatRequest", a.withdrawChatRequest)
		a.handle("listChannelOffers", a.listChannelOffers)
		a.handle("acceptChannelOffer", a.acceptChannelOffer)
		a.handle("declineChannelOffer", a.declineChannelOffer)
	})
}

// requestID reads a request's identifier from the path. One that is not an
// identifier is not found, like one that does not exist.
func requestID(r *Request) (string, error) {
	id := r.R.PathValue("id")
	if !model.ValidID(id) {
		return "", model.ErrNotFound
	}
	return id, nil
}

func (a *API) listChatRequests(r *Request) error {
	list, err := a.Chat.Requests(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"requests": list})
}

func (a *API) acceptChatRequest(r *Request) error {
	id, err := requestID(r)
	if err != nil {
		return err
	}
	c, err := a.Chat.AcceptRequest(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"conversation": c})
}

func (a *API) declineChatRequest(r *Request) error {
	id, err := requestID(r)
	if err != nil {
		return err
	}
	if err := a.Chat.DeclineRequest(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) dismissChatRequest(r *Request) error {
	id, err := requestID(r)
	if err != nil {
		return err
	}
	if err := a.Chat.DismissRequest(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) blockChatRequester(r *Request) error {
	id, err := requestID(r)
	if err != nil {
		return err
	}
	who, err := a.Chat.RequestRequester(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	// The request stays as it is: to the person asking it goes on waiting.
	if err := a.People.Block(r.Ctx(), r.Actor(), who); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) closeChat(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	c, err := a.Chat.Close(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"conversation": c})
}

func (a *API) withdrawChatRequest(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Chat.Withdraw(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

// Channel offers (ruling R103): channels a group or the default list
// offers the caller, answered from the sidebar.

func (a *API) listChannelOffers(r *Request) error {
	list, err := a.Chat.Offers(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"offers": list})
}

func (a *API) acceptChannelOffer(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	c, err := a.Chat.AnswerOffer(r.Ctx(), r.Actor(), id, true)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"conversation": c})
}

func (a *API) declineChannelOffer(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	if _, err := a.Chat.AnswerOffer(r.Ctx(), r.Actor(), id, false); err != nil {
		return err
	}
	return r.NoContent()
}
