package api

import "github.com/tikaydaes/churr/internal/model"

// Channel bans (docs/FEATURES.md section 19.1).

func init() {
	register(func(a *API) {
		a.handle("listChannelBans", a.listChannelBans)
		a.handle("banFromChannel", a.banFromChannel)
		a.handle("liftChannelBan", a.liftChannelBan)
		a.handle("listAllChannelBans", a.listAllChannelBans)
	})
}

// channelBanInput is the contract's ChannelBanInput.
type channelBanInput struct {
	AccountID string `json:"account_id"`
	Reason    string `json:"reason"`
	Seconds   int64  `json:"seconds"`
}

func (a *API) listChannelBans(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	list, err := a.Chat.ChannelBans(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"bans": orEmpty(list)})
}

func (a *API) banFromChannel(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	var body channelBanInput
	if err := r.Decode(&body); err != nil {
		return err
	}
	account, err := BodyID[model.AccountID]("account_id", body.AccountID)
	if err != nil {
		return err
	}
	b, err := a.Chat.BanFromChannel(r.Ctx(), r.Actor(), id, account, body.Reason, body.Seconds)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"ban": b})
}

func (a *API) liftChannelBan(r *Request) error {
	id, account, err := memberPath(r)
	if err != nil {
		return err
	}
	if err := a.Chat.LiftChannelBan(r.Ctx(), r.Actor(), id, account); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listAllChannelBans(r *Request) error {
	list, err := a.Chat.AllChannelBans(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"bans": orEmpty(list)})
}
