package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/push"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/store"
)

// Devices, phone wake-ups and the unlock page (docs/PUSH.md sections 3 and
// 9, docs/SEALED.md sections 4.1 and 13.3).

func init() {
	register(func(a *API) {
		a.handle("listDevices", a.listDevices)
		a.handle("registerDevice", a.registerDevice)
		a.handle("removeDevice", a.removeDevice)
		a.handle("beginReattachDevice", a.beginReattachDevice)
		a.handle("finishReattachDevice", a.finishReattachDevice)
		a.handle("setPasskeyUnlockKey", a.setPasskeyUnlockKey)
		a.handle("fetchWakeUp", a.fetchWakeUp)
		a.handle("openUnlockPage", a.openUnlockPage)
	})
}

// wakeLimiter bounds the wake-up fetch per device: a phone fetches once
// per wake-up, and a burst of them when it comes back online.
var wakeLimiter = ratelimit.New(ratelimit.Per(60, time.Minute), 60)

func (a *API) listDevices(r *Request) error {
	devices, err := a.Push.Devices(r.Ctx(), r.Actor(), r.Who.Session)
	if err != nil {
		return err
	}
	// A retired device is listed until an unlocked device has taken it
	// out of the account's device list.
	listed, err := a.Sealed.Listed(r.Ctx(), r.Actor().ID)
	if err != nil {
		return err
	}
	out := make([]model.Device, 0, len(devices))
	for _, d := range devices {
		if d.RetiredAt.IsZero() || listed[d.ID] {
			out = append(out, d)
		}
	}
	return r.JSON(map[string]any{"devices": out})
}

func (a *API) registerDevice(r *Request) error {
	var body struct {
		Name          *string     `json:"name"`
		Platform      *string     `json:"platform"`
		PushKind      *string     `json:"push_kind"`
		PushAddress   *string     `json:"push_address"`
		PushPublicKey model.Bytes `json:"push_public_key"`
		PushAuth      model.Bytes `json:"push_auth"`
		SignKey       model.Bytes `json:"sign_key"`
		BoxKey        model.Bytes `json:"box_key"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	d, err := a.Push.Register(r.Ctx(), r.Actor(), r.Who.Session, push.DeviceInput{Name: body.Name, Platform: body.Platform,
		PushKind: body.PushKind, PushAddress: body.PushAddress, PushPublicKey: body.PushPublicKey, PushAuth: body.PushAuth,
		SignKey: body.SignKey, BoxKey: body.BoxKey})
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"device": d})
}

func (a *API) removeDevice(r *Request) error {
	id, err := PathID[model.DeviceID](r, "id")
	if err != nil {
		return err
	}
	if r.Who.Session.Kind != model.SessionWeb && r.Who.Session.Kind != model.SessionApp {
		return model.ErrForbidden.With("Devices are removed from a web or app session.")
	}
	d, err := a.Push.Device(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	if !d.RetiredAt.IsZero() {
		return model.ErrNotFound
	}
	listed, err := a.Sealed.Listed(r.Ctx(), r.Actor().ID)
	if err != nil {
		return err
	}
	// Its session is revoked, which closes its connections and retires
	// the device; a device whose session had already ended is retired
	// directly.
	if d.SessionID != "" {
		if err := a.Auth.EndSession(r.Ctx(), r.Who, d.SessionID); err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
	}
	if retired, found, err := a.Push.Retire(r.Ctx(), r.Actor(), id); err != nil {
		return err
	} else if found {
		a.Live.Send(r.Actor().ID, model.Event{Type: model.EventDeviceRetired, At: model.Now(), Payload: map[string]any{"device": retired}})
	}
	// A device revoked from another one may be in someone else's hands:
	// it held the backup key, so the key must be replaced. The revoking
	// device does that at once if it is unlocked; the mark makes sure the
	// next unlocked device does otherwise.
	if listed[id] && d.SessionID != r.Who.Session.ID {
		if err := a.Sealed.MarkReplacementDue(r.Ctx(), r.Actor().ID); err != nil {
			return err
		}
	}
	return r.NoContent()
}

func (a *API) beginReattachDevice(r *Request) error {
	var body struct {
		DeviceID string `json:"device_id"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	id, err := BodyID[model.DeviceID]("device_id", body.DeviceID)
	if err != nil {
		return err
	}
	challenge, err := a.Push.BeginReattach(r.Ctx(), r.Actor(), r.Who.Session, id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"challenge": model.Bytes(challenge)})
}

func (a *API) finishReattachDevice(r *Request) error {
	var body struct {
		DeviceID  string       `json:"device_id"`
		Signature *model.Bytes `json:"signature"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	id, err := BodyID[model.DeviceID]("device_id", body.DeviceID)
	if err != nil {
		return err
	}
	if body.Signature == nil {
		return model.BadRequest("signature is needed.")
	}
	d, err := a.Push.FinishReattach(r.Ctx(), r.Actor(), r.Who.Session, id, *body.Signature)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"device": d})
}

func (a *API) setPasskeyUnlockKey(r *Request) error {
	id, err := PathID[model.PasskeyID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		UnlockPublic *model.Bytes `json:"unlock_public"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.UnlockPublic == nil {
		return model.BadRequest("unlock_public is needed.")
	}
	pk, err := a.Sealed.SetPasskeyUnlockKey(r.Ctx(), r.caller(), id, *body.UnlockPublic)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"passkey": pk})
}

// wakeScan is how far past the read position the wake-up fetch looks for
// messages that notify.
const wakeScan = 200

// fetchWakeUp answers with what a phone needs to draw one notification
// after a wake-up (docs/PUSH.md section 9). It does not move the read
// position.
func (a *API) fetchWakeUp(r *Request) error {
	eventID := r.R.PathValue("event_id")
	ref, err := a.Push.Resolve(r.Ctx(), r.Who.Session.ID, eventID)
	if err != nil {
		return err
	}
	if res := wakeLimiter.Take(string(ref.DeviceID)); !res.OK {
		return model.RateLimited(res.RetryAfterSeconds())
	}
	actor := r.Actor()
	// perm decides whether the account can still read the conversation;
	// if not, the wake-up points at nothing.
	conv, err := a.Chat.Conversation(r.Ctx(), actor, ref.ConversationID)
	if err != nil {
		return model.ErrNotFound
	}
	names := map[model.AccountID]string{}
	name := func(id model.AccountID) string {
		if n, ok := names[id]; ok {
			return n
		}
		acc, err := a.Store.Accounts().Get(r.Ctx(), id)
		if err == nil {
			names[id] = acc.DisplayName
		}
		return names[id]
	}
	shown := map[string]any{"id": conv.ID, "kind": conv.Kind}
	if conv.Name != "" {
		shown["name"] = conv.Name
	} else {
		// An ad-hoc channel or a sealed chat is titled by who else is
		// in it.
		var others []string
		for _, id := range conv.MemberIDs {
			if id != actor.ID {
				others = append(others, name(id))
			}
		}
		shown["title"] = strings.Join(others, ", ")
	}
	var readSeq int64
	if conv.Membership != nil {
		readSeq = conv.Membership.ReadSeq
	}
	type wakeMessage struct {
		Message    model.Message `json:"message"`
		AuthorName string        `json:"author_name"`
	}
	notifying := []wakeMessage{}
	msgs, _, _, err := a.Chat.Messages(r.Ctx(), actor, conv.ID, store.MessageList{SeqPage: store.SeqPage{After: readSeq, Limit: wakeScan}})
	if err != nil {
		return err
	}
	for _, m := range msgs {
		if !m.Deleted() && a.Notify.WouldNotify(r.Ctx(), m, actor.ID) {
			notifying = append(notifying, wakeMessage{Message: m, AuthorName: name(m.AuthorID)})
		}
	}
	more := 0
	if len(notifying) > 5 {
		more = len(notifying) - 5
		notifying = notifying[more:]
	}
	totals, err := a.Notify.Totals(r.Ctx(), actor.ID)
	if err != nil {
		return err
	}
	out := map[string]any{"kind": ref.Kind, "conversation": shown, "messages": notifying, "more": more,
		"read": ref.Kind == push.KindMessage && readSeq >= ref.Seq, "badge": totals.Badge}
	if ref.Kind == push.KindRing {
		call := map[string]any{"id": ref.CallID, "ringing": false}
		var from model.AccountID
		if a.Calls != nil {
			// A ring that reaches the phone late does not ring: the
			// phone shows a missed call.
			if ring, ok := a.Calls.RingingFor(actor.ID); ok && ring.CallID == ref.CallID {
				call["ringing"], from = true, ring.FromAccountID
			}
		}
		if from == "" {
			if stored, err := a.Store.Calls().Get(r.Ctx(), ref.CallID); err == nil {
				from = stored.StartedBy
			}
		}
		call["from_account_id"], call["from_name"] = from, name(from)
		out["call"] = call
	}
	return r.JSON(map[string]any{"wake_up": out})
}

// openUnlockPage draws the page that unlocks sealed chats for an app with
// a passkey (docs/SEALED.md section 13.3). A passkey only works at the
// server's own web address, so this way of unlocking always passes through
// a page the server supplies. The page does the work in the browser: the
// server never sees the PRF output or the backup key.
func (a *API) openUnlockPage(r *Request) error {
	deviceID := r.R.URL.Query().Get("device_id")
	if !model.ValidID(deviceID) {
		return model.Invalid("device_id", "device_id names the app's device.")
	}
	if a.UnlockPage == nil {
		return model.ErrUnavailable.With("This server's pages are not built in.")
	}
	a.UnlockPage(r.W, http.StatusOK, map[string]any{"DeviceID": deviceID})
	return nil
}

// pushKinds lists the push kinds the server accepts, for its public
// description, so an app can choose before it registers.
func (a *API) pushKinds(ctx context.Context) []string {
	if a.Push == nil {
		return []string{model.PushNone}
	}
	return a.Push.Kinds(ctx)
}
