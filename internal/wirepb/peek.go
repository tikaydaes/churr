package wirepb

import "google.golang.org/protobuf/encoding/protowire"

// FrameHead is what PeekFrame reads of an Envelope: the fields of the
// envelope itself, and the body as the bytes it is. The slices point into
// the envelope.
type FrameHead struct {
	// Type is the frame's type, or "" for a body this schema does not know.
	Type string
	// ConversationID is the 16 bytes of `conversation_id`, or nil.
	ConversationID []byte
	// Seq is `seq`, or 0; At is `at` in microseconds since 1970, or 0.
	Seq, At int64
	// Body is the frame's message, not decoded.
	Body []byte
	// JSON is the whole frame as JSON text, for a frame that travelled
	// so (docs/WIRE-PROTOBUF.md section 3). Nothing else is set but Type.
	JSON []byte
}

// PeekFrame reads an Envelope's own fields and leaves its body alone: one
// pass over a few tags, nothing copied and nothing allocated. It is for a
// reader that wants to know what a frame is and where it belongs without
// paying to decode it, as a load generator or a router does. ok is false
// for bytes that are not an Envelope.
func PeekFrame(b []byte) (h FrameHead, ok bool) {
	for len(b) > 0 {
		num, t, n := protowire.ConsumeTag(b)
		if n < 0 {
			return h, false
		}
		b = b[n:]
		switch t {
		case protowire.VarintType:
			v, m := protowire.ConsumeVarint(b)
			if m < 0 {
				return h, false
			}
			switch num {
			case envAt:
				h.At = int64(v)
			case envSeq:
				h.Seq = int64(v)
			}
			b = b[m:]
		case protowire.BytesType:
			raw, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return h, false
			}
			switch {
			case num == envConv:
				h.ConversationID = raw
			case num == envJSON:
				h.JSON = raw
			case num == envType:
				if h.Type == "" {
					// Only beside JSON, or from a newer peer. The
					// known types cost nothing to name.
					if f := frameByType[string(raw)]; f != nil {
						h.Type = f.typ
					} else {
						h.Type = string(raw)
					}
				}
			case num == numEmpty || num == numNull:
			default:
				if f := frameByNum[num]; f != nil {
					h.Type, h.Body = f.typ, raw
				}
			}
			b = b[m:]
		default:
			m := protowire.ConsumeFieldValue(num, t, b)
			if m < 0 {
				return h, false
			}
			b = b[m:]
		}
	}
	return h, true
}
