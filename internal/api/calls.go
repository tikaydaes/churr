package api

import (
	"github.com/tikaydaes/churr/internal/model"
)

// Calls: the three operations a client uses around the `call.` frames of
// its live connection, and the two of the admin console (docs/CALLS.md).

func init() {
	register(func(a *API) {
		a.handle("getCall", a.getCall)
		a.handle("joinCall", a.joinCall)
		a.handle("leaveCall", a.leaveCall)
		a.handle("listLiveCalls", a.listLiveCalls)
		a.handle("testCalls", a.testCalls)
	})
}

// callsOff is the answer of a server that was built without the calls
// package, which no shipped server is.
var callsOff = model.ErrFeatureOff.With("Calls are switched off on this server.")

func (a *API) getCall(r *Request) error {
	conv, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	if a.Calls == nil {
		return r.JSON(map[string]any{})
	}
	call, err := a.Calls.Get(r.Ctx(), r.Actor(), conv)
	if err != nil {
		return err
	}
	if call == nil {
		// No call in progress: the answer has no `call`.
		return r.JSON(map[string]any{})
	}
	return r.JSON(map[string]any{"call": call})
}

func (a *API) joinCall(r *Request) error {
	conv, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	if a.Calls == nil {
		return callsOff
	}
	call, ticket, err := a.Calls.Join(r.Ctx(), r.Actor(), r.Who.Session, conv)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"call": call, "ticket": ticket})
}

func (a *API) leaveCall(r *Request) error {
	conv, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	if a.Calls == nil {
		return model.ErrNotFound
	}
	if err := a.Calls.Leave(r.Ctx(), r.Actor(), conv); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listLiveCalls(r *Request) error {
	if a.Calls == nil {
		return r.JSON(map[string]any{"calls": []any{}, "media_address": ""})
	}
	list, address, err := a.Calls.LiveCalls(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"calls": list, "media_address": address})
}

func (a *API) testCalls(r *Request) error {
	var body struct {
		SDP string `json:"sdp"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if a.Calls == nil {
		return callsOff
	}
	answer, address, err := a.Calls.Test(r.Ctx(), r.Actor(), body.SDP)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"sdp": answer, "media_address": address})
}
