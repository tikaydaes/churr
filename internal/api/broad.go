package api

import (
	"github.com/tikaydaes/churr/internal/model"
)

// checkBroadMentions says what the broad mentions of a text would do if it
// were posted (ruling R64).
func (a *API) checkBroadMentions(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	out, err := a.Chat.CheckBroadMentions(r.Ctx(), r.Actor(), id, body.Text)
	if err != nil {
		return err
	}
	return r.JSON(out)
}
