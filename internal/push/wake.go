package push

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// The wake-up of docs/PUSH.md section 4: an event identifier only this
// server can read, a collapse key, a priority and perhaps a badge.

// The two kinds of event that can wake a phone.
const (
	KindMessage = "message"
	KindRing    = "ring"
)

// The two priorities.
const (
	PriorityHigh   = "high"
	PriorityNormal = "normal"
)

// tokenLife is how long the server accepts an event identifier back.
const tokenLife = 7 * 24 * time.Hour

// Ref is what an event identifier points at: which device was woken, and
// about what.
type Ref struct {
	DeviceID       model.DeviceID
	Kind           string
	ConversationID model.ConversationID
	// Seq is the message's sequence number, for a message; CallID the
	// call, for a ring.
	Seq    int64
	CallID model.CallID
	At     model.Time
}

var b64 = base64.RawURLEncoding

// idBytes is an identifier as its 16 bytes.
func idBytes(id string) []byte {
	if !model.ValidID(id) {
		return make([]byte, 16)
	}
	b, _ := hex.DecodeString(id[0:8] + id[9:13] + id[14:18] + id[19:23] + id[24:36])
	return b
}

func idString(b []byte) string {
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// Route is a device's wake route: the first eight bytes of a keyed hash of
// the device's identifier under the server's seal key, in base64url, eleven
// characters. It is the same for every wake-up to one device from one
// server and means nothing to anyone else; the app keeps it beside the
// server, to tell which of its servers a wake-up came from.
func (s *Service) Route(device model.DeviceID) string {
	return b64.EncodeToString(s.keys.MAC("wake-route", idBytes(string(device)))[:8])
}

// CollapseKey is the first sixteen bytes of a keyed hash of the device and
// the conversation, in base64url: 22 characters. Two wake-ups for one
// conversation on one device share it, so the newer replaces the older on
// screen; it differs per device, so nobody outside can tell that two
// phones were woken about the same conversation.
func (s *Service) CollapseKey(device model.DeviceID, conv model.ConversationID) string {
	return b64.EncodeToString(s.keys.MAC("wake-collapse", idBytes(string(device)), idBytes(string(conv)))[:16])
}

// The plaintext of a token: the device, one byte of kind, the
// conversation, then the seq (8 bytes) or the call (16 bytes), then the
// time it was made.
const (
	tokenMessage byte = 1
	tokenRing    byte = 2
)

// EventID makes the event identifier of one wake-up: the device's route, a
// dot, and a token. The token is a random nonce followed by an AES-256-GCM
// encryption, under the server's seal key, of what the wake-up is about,
// in base64url. No table of pending wake-ups is needed.
func (s *Service) EventID(ref Ref) (string, error) {
	plain := make([]byte, 0, 16+1+16+16+8)
	plain = append(plain, idBytes(string(ref.DeviceID))...)
	switch ref.Kind {
	case KindRing:
		plain = append(plain, tokenRing)
		plain = append(plain, idBytes(string(ref.ConversationID))...)
		plain = append(plain, idBytes(string(ref.CallID))...)
	default:
		plain = append(plain, tokenMessage)
		plain = append(plain, idBytes(string(ref.ConversationID))...)
		plain = binary.BigEndian.AppendUint64(plain, uint64(ref.Seq))
	}
	plain = binary.BigEndian.AppendUint64(plain, uint64(ref.At))
	sealed, err := s.keys.Seal(plain, "wake", "token", "")
	if err != nil {
		return "", err
	}
	return s.Route(ref.DeviceID) + "." + b64.EncodeToString(sealed), nil
}

// readEventID opens an event identifier this server made. Anything else,
// and anything older than seven days, is not found.
func (s *Service) readEventID(eventID string) (Ref, error) {
	var ref Ref
	_, token, ok := strings.Cut(eventID, ".")
	if !ok || len(eventID) > 256 {
		return ref, model.ErrNotFound
	}
	sealed, err := b64.DecodeString(token)
	if err != nil {
		return ref, model.ErrNotFound
	}
	plain, err := s.keys.Unseal(sealed, "wake", "token", "")
	if err != nil || len(plain) < 16+1+16 {
		return ref, model.ErrNotFound
	}
	ref.DeviceID = model.DeviceID(idString(plain[:16]))
	kind, rest := plain[16], plain[17:]
	ref.ConversationID = model.ConversationID(idString(rest[:16]))
	rest = rest[16:]
	switch {
	case kind == tokenMessage && len(rest) == 16:
		ref.Kind, ref.Seq = KindMessage, int64(binary.BigEndian.Uint64(rest[:8]))
		rest = rest[8:]
	case kind == tokenRing && len(rest) == 24:
		ref.Kind, ref.CallID = KindRing, model.CallID(idString(rest[:16]))
		rest = rest[16:]
	default:
		return ref, model.ErrNotFound
	}
	ref.At = model.Time(binary.BigEndian.Uint64(rest))
	if s.now().Sub(ref.At) > tokenLife {
		return ref, model.ErrNotFound
	}
	return ref, nil
}

// Resolve reads the event identifier a phone was woken with, for the
// wake-up fetch (docs/PUSH.md section 9, steps 1 and 2). An identifier the
// server cannot read, one older than seven days, and one that names a
// device other than the calling session's all answer not_found. Whether
// the account can still read the conversation is the caller's to ask.
func (s *Service) Resolve(ctx context.Context, session model.SessionID, eventID string) (Ref, error) {
	ref, err := s.readEventID(eventID)
	if err != nil {
		return ref, err
	}
	dev, found, err := s.store.Devices().BySession(ctx, session)
	if err != nil {
		return ref, err
	}
	if !found || dev.ID != ref.DeviceID || !dev.RetiredAt.IsZero() {
		return ref, model.ErrNotFound
	}
	return ref, nil
}

// wakeDevice makes one wake-up for one device and hands it to delivery. It
// reports whether there was anything to send it through.
func (s *Service) wakeDevice(ctx context.Context, d model.Device, ref Ref, priority string, badge *int64) bool {
	ref.DeviceID, ref.At = d.ID, s.now()
	eventID, err := s.EventID(ref)
	if err != nil {
		s.log.Warn("a wake-up could not be made", "err", err)
		return false
	}
	w := wakeUp{device: d, eventID: eventID, collapseKey: s.CollapseKey(d.ID, ref.ConversationID), priority: priority, badge: badge}
	switch d.PushKind {
	case model.PushAPNs, model.PushFCM:
		if w.relay = s.relayURL(ctx); w.relay == "" {
			return false
		}
	case model.PushUnifiedPush:
		if !s.unifiedPushOn(ctx) {
			return false
		}
	default:
		return false
	}
	s.deliver(w)
	return true
}
