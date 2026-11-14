package push

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/url"
	"time"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// Devices: one row per installed app or browser profile signed in to one
// account (docs/PUSH.md section 3, docs/SEALED.md section 4.1). A device
// row carries how the device is woken and its two public keys for sealed
// chats.

// DeviceInput is what a client sets on the device of its session. A nil
// field is left as it is.
type DeviceInput struct {
	Name        *string
	Platform    *string
	PushKind    *string
	PushAddress *string
	// PushPublicKey and PushAuth are for a UnifiedPush distributor that
	// wants wake-ups encrypted as Web Push.
	PushPublicKey []byte
	PushAuth      []byte
	// SignKey and BoxKey are the device's Ed25519 and X25519 public
	// keys. Each is set once: a device with different keys is a new
	// device.
	SignKey []byte
	BoxKey  []byte
}

var platforms = map[string]bool{"android": true, "ios": true, "macos": true, "windows": true, "linux": true, "web": true}

const maxPushAddress = 4096

// person refuses sessions that are not a person on a device.
func person(account model.Account, session model.Session) error {
	if !account.Active() {
		return model.ErrUnauthenticated
	}
	if account.Role == model.RoleBot || account.Role == model.RoleRemote ||
		(session.Kind != model.SessionWeb && session.Kind != model.SessionApp) {
		return model.ErrForbidden.With("Devices belong to a person signed in on a web or app session.")
	}
	return nil
}

// show puts a device in the shape its owner sees.
func (s *Service) show(d model.Device, session model.SessionID) model.Device {
	d.WakeRoute = s.Route(d.ID)
	d.Current = d.SessionID != "" && d.SessionID == session
	return d
}

// checkPush decides whether a push registration can be served. A kind the
// server cannot serve is refused with feature_off, so the app falls back
// to the status endpoint (docs/PUSH.md section 3).
func (s *Service) checkPush(ctx context.Context, kind, address string) error {
	switch kind {
	case model.PushNone:
		return nil
	case model.PushAPNs, model.PushFCM:
		if s.relayURL(ctx) == "" {
			return model.ErrFeatureOff.With("This server sends no wake-ups through the relay.")
		}
	case model.PushUnifiedPush:
		if !s.unifiedPushOn(ctx) {
			return model.ErrFeatureOff.With("UnifiedPush is switched off on this server.")
		}
		if u, err := url.Parse(address); err != nil || u.Scheme != "https" || u.Host == "" {
			return model.ErrFeatureOff.With("A UnifiedPush endpoint must be an https address.")
		}
	default:
		return model.Invalid("push_kind", "push_kind is apns, fcm, unifiedpush or none.")
	}
	if address == "" || utf8.RuneCountInString(address) > maxPushAddress {
		return model.Invalid("push_address", "push_address is 1 to 4,096 characters.")
	}
	return nil
}

// Register creates or updates the device of the caller's session. It is
// idempotent: apps call it after sign-in, on every start, and whenever the
// push address changes. Registering again stores the address and clears
// the failure mark, so a phone whose address was forgotten by mistake puts
// itself right the next time it is opened.
func (s *Service) Register(ctx context.Context, account model.Account, session model.Session, in DeviceInput) (model.Device, error) {
	none := model.Device{}
	if err := person(account, session); err != nil {
		return none, err
	}
	if in.Name != nil && (*in.Name == "" || utf8.RuneCountInString(*in.Name) > 60) {
		return none, model.Invalid("name", "A device name is 1 to 60 characters.")
	}
	if in.Platform != nil && !platforms[*in.Platform] {
		return none, model.Invalid("platform", "platform is android, ios, macos, windows, linux or web.")
	}
	if in.SignKey != nil && len(in.SignKey) != ed25519.PublicKeySize {
		return none, model.Invalid("sign_key", "sign_key is 32 bytes.")
	}
	if in.BoxKey != nil && len(in.BoxKey) != 32 {
		return none, model.Invalid("box_key", "box_key is 32 bytes.")
	}
	if (len(in.PushPublicKey) != 0 && len(in.PushPublicKey) != 65) || (len(in.PushAuth) != 0 && len(in.PushAuth) != 16) ||
		(len(in.PushPublicKey) == 0) != (len(in.PushAuth) == 0) {
		return none, model.Invalid("push_public_key", "push_public_key is a 65-byte P-256 public key and push_auth 16 bytes; give both or neither.")
	}
	now := s.now()
	var out model.Device
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		cur, found, err := tx.Devices().BySession(ctx, session.ID)
		if err != nil {
			return err
		}
		if found && !cur.RetiredAt.IsZero() {
			return model.ErrForbidden.With("This device has been removed. Sign in again.")
		}
		d := cur
		if !found {
			d = model.Device{AccountID: account.ID, SessionID: session.ID, Name: session.Label, Platform: "web", CreatedAt: now}
			if session.Kind == model.SessionApp {
				d.Platform = ""
			}
		}
		if in.Name != nil {
			d.Name = *in.Name
		}
		if d.Name == "" {
			d.Name = "Device"
		}
		if in.Platform != nil {
			// The platform is in the signed device list; it stays what
			// it was once the device has keys.
			if len(cur.SignKey) != 0 && cur.Platform != *in.Platform {
				return model.Invalid("platform", "A device that has registered its keys keeps its platform.")
			}
			d.Platform = *in.Platform
		}
		if d.Platform == "" {
			return model.Invalid("platform", "platform is needed.")
		}
		if in.SignKey != nil {
			if len(cur.SignKey) != 0 && string(cur.SignKey) != string(in.SignKey) {
				return model.Invalid("sign_key", "sign_key is set once. A device with a different key is a new device.")
			}
			d.SignKey = in.SignKey
		}
		if in.BoxKey != nil {
			if len(cur.BoxKey) != 0 && len(cur.SignKey) != 0 && string(cur.BoxKey) != string(in.BoxKey) {
				return model.Invalid("box_key", "box_key is set once. A device with a different key is a new device.")
			}
			d.BoxKey = in.BoxKey
		}
		if in.PushKind != nil || in.PushAddress != nil {
			kind, address := d.PushKind, d.PushAddress
			if in.PushKind != nil {
				kind = *in.PushKind
			}
			if in.PushAddress != nil {
				address = *in.PushAddress
			}
			if kind == "" {
				return model.Invalid("push_kind", "push_kind is needed with push_address.")
			}
			if err := s.checkPush(ctx, kind, address); err != nil {
				return err
			}
			if kind == model.PushNone {
				address = ""
			}
			d.PushKind, d.PushAddress, d.PushFailedAt = kind, address, 0
			d.PushPublicKey, d.PushAuth = nil, nil
			if kind == model.PushUnifiedPush {
				d.PushPublicKey, d.PushAuth = in.PushPublicKey, in.PushAuth
			}
		}
		d.LastSeenAt = now
		if !found {
			if err := tx.Devices().Create(ctx, &d); err != nil {
				return err
			}
			out = d
			return nil
		}
		out, err = tx.Devices().Update(ctx, cur.ID, func(row *model.Device) error {
			row.Name, row.Platform, row.SignKey, row.BoxKey = d.Name, d.Platform, d.SignKey, d.BoxKey
			row.PushKind, row.PushAddress, row.PushFailedAt = d.PushKind, d.PushAddress, d.PushFailedAt
			row.PushPublicKey, row.PushAuth, row.LastSeenAt = d.PushPublicKey, d.PushAuth, d.LastSeenAt
			return nil
		})
		return err
	})
	if err != nil {
		return none, err
	}
	return s.show(out, session.ID), nil
}

// Devices lists the caller's devices, retired ones included.
func (s *Service) Devices(ctx context.Context, account model.Account, session model.Session) ([]model.Device, error) {
	list, err := s.store.Devices().ForAccount(ctx, account.ID)
	for i := range list {
		list[i] = s.show(list[i], session.ID)
	}
	return list, err
}

// Device returns one of the caller's own devices. Anybody else's could not
// be known to exist.
func (s *Service) Device(ctx context.Context, account model.Account, id model.DeviceID) (model.Device, error) {
	d, err := s.store.Devices().Get(ctx, id)
	if err != nil {
		return d, err
	}
	if d.AccountID != account.ID {
		return model.Device{}, model.ErrNotFound
	}
	return d, nil
}

// Retire marks one of the caller's devices retired and forgets its push
// address. found is false if it was retired already, as it is when its
// session has just been revoked.
func (s *Service) Retire(ctx context.Context, account model.Account, id model.DeviceID) (model.Device, bool, error) {
	if _, err := s.Device(ctx, account, id); err != nil {
		return model.Device{}, false, err
	}
	return s.store.Devices().Retire(ctx, id, s.now())
}

// challenge is one reattach in progress.
type challenge struct {
	device  model.DeviceID
	value   []byte
	expires time.Time
}

// challengeLife is how long a reattach challenge may wait for its answer.
const challengeLife = 2 * time.Minute

// attachLabel is the label of the signature a device makes over the
// challenge (docs/SEALED.md section 4.1): "churr device attach v1", one
// zero byte, then the challenge.
const attachLabel = "churr device attach v1\x00"

// reattachable finds the device a new session wants to carry on as, and
// refuses unless the same app or browser may take it up again: it is the
// account's own, it registered a signing key, it is not retired, and the
// session it had has ended by itself.
func (s *Service) reattachable(ctx context.Context, account model.Account, session model.Session, id model.DeviceID) (model.Device, error) {
	none := model.Device{}
	if err := person(account, session); err != nil {
		return none, err
	}
	d, err := s.Device(ctx, account, id)
	if err != nil {
		return none, err
	}
	if !d.RetiredAt.IsZero() || len(d.SignKey) != ed25519.PublicKeySize {
		return none, model.ErrForbidden.With("That device cannot be attached again.")
	}
	if d.SessionID == session.ID {
		return none, model.ErrForbidden.With("That device is already this session's.")
	}
	if d.SessionID != "" {
		old, err := s.store.Sessions().Get(ctx, d.SessionID)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return none, err
		}
		if err == nil && old.RevokedAt.IsZero() && (old.ExpiresAt.IsZero() || old.ExpiresAt.After(s.now())) {
			return none, model.ErrForbidden.With("That device is still signed in elsewhere.")
		}
	}
	// The new session may hold only the keyless row an app is given at
	// sign-in, which is then dropped in the other's favour.
	if mine, found, err := s.store.Devices().BySession(ctx, session.ID); err != nil {
		return none, err
	} else if found && len(mine.SignKey) != 0 {
		return none, model.ErrForbidden.With("This session already has a device with keys.")
	}
	return d, nil
}

// BeginReattach starts attaching an existing device to the caller's new
// session, for the same app or browser signing in to the same account
// again after its session ended by itself. It returns the challenge the
// device must sign.
func (s *Service) BeginReattach(ctx context.Context, account model.Account, session model.Session, id model.DeviceID) ([]byte, error) {
	d, err := s.reattachable(ctx, account, session, id)
	if err != nil {
		return nil, err
	}
	value := secret.RandomBytes(32)
	now := time.Now()
	s.mu.Lock()
	for k, c := range s.challenges {
		if c.expires.Before(now) {
			delete(s.challenges, k)
		}
	}
	s.challenges[session.ID] = challenge{device: d.ID, value: value, expires: now.Add(challengeLife)}
	s.mu.Unlock()
	return value, nil
}

// FinishReattach checks the device's signature over the challenge and
// attaches the device to the caller's session. The device keeps its place
// in every sealed chat.
func (s *Service) FinishReattach(ctx context.Context, account model.Account, session model.Session, id model.DeviceID, signature []byte) (model.Device, error) {
	none := model.Device{}
	d, err := s.reattachable(ctx, account, session, id)
	if err != nil {
		return none, err
	}
	s.mu.Lock()
	c, ok := s.challenges[session.ID]
	delete(s.challenges, session.ID) // a challenge is answered once
	s.mu.Unlock()
	if !ok || c.device != id || c.expires.Before(time.Now()) {
		return none, model.ErrForbidden.With("Begin again: there is no challenge waiting for that device.")
	}
	if len(signature) != ed25519.SignatureSize || !ed25519.Verify(ed25519.PublicKey(d.SignKey), append([]byte(attachLabel), c.value...), signature) {
		return none, model.ErrForbidden.With("The signature is not by that device's signing key.")
	}
	var out model.Device
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if mine, found, err := tx.Devices().BySession(ctx, session.ID); err != nil {
			return err
		} else if found {
			if err := tx.Devices().Delete(ctx, mine.ID); err != nil {
				return err
			}
		}
		out, err = tx.Devices().Attach(ctx, id, session.ID, s.now())
		return err
	})
	if err != nil {
		if model.CodeOf(err) == model.CodeConflict {
			return none, model.ErrForbidden.With("That device cannot be attached again.")
		}
		return none, err
	}
	return s.show(out, session.ID), nil
}
