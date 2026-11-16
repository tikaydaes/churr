package api

// The server broadcast (docs/FEATURES.md section 12.6, ruling R74).

func init() {
	register(func(a *API) {
		a.handle("sendBroadcast", a.sendBroadcast)
		a.handle("clearBroadcast", a.clearBroadcast)
	})
}

// broadcastInput is the contract's BroadcastInput.
type broadcastInput struct {
	Text    string `json:"text"`
	Seconds int64  `json:"seconds"`
}

func (a *API) sendBroadcast(r *Request) error {
	var body broadcastInput
	if err := r.Decode(&body); err != nil {
		return err
	}
	b, err := a.Broadcast.Send(r.Ctx(), r.Actor(), body.Text, body.Seconds)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"broadcast": b})
}

func (a *API) clearBroadcast(r *Request) error {
	if err := a.Broadcast.Clear(r.Ctx(), r.Actor()); err != nil {
		return err
	}
	return r.NoContent()
}
