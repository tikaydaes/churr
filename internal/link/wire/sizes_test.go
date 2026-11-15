package wire

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
)

// TestSizes prints the table of docs/LINK-PROTOCOL.md section 2: what a
// relayed message takes as the JSON frames of link version 2 and as the
// protobuf frames of version 3, as they are and deflated.
//
//	go test -v -run TestSizes ./internal/link/wire
//
// The version 2 form is rebuilt here from the same structs: a frame was
// their JSON, with the event as JSON text inside the ciphertext.
func TestSizes(t *testing.T) {
	k := testKeys(t)
	key := secret.RandomBytes(32)
	stream, author := id(), id()
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 5)[:200]
	event := func() map[string]any {
		return map[string]any{"type": "message.created", "at": model.Time(1_794_557_400_120),
			"message": map[string]any{"id": id(), "author_id": author, "client_id": id(), "text": text,
				"created_at": model.Time(1_794_557_400_120)}, "blobs": []any{}}
	}
	seal := func(n int64, plain []byte) Envelope {
		e, err := SealEnvelope(key, stream, 3, "chat.example.org", id(), Stamp{At: 1_794_557_400_120 + n, N: 4000 + n, Offset: -37}, plain, k)
		if err != nil {
			t.Fatal(err)
		}
		e.Cursor, e.Prev = 90_000+n, 3999+n
		return e
	}
	var oldEnvs, newEnvs []Envelope
	var oldPlain, newPlain int
	for n := int64(0); n < 100; n++ {
		asJSON, _ := json.Marshal(event())
		asProto := EncodeEvent(asJSON)
		oldPlain, newPlain = len(asJSON), len(asProto)
		oldEnvs, newEnvs = append(oldEnvs, seal(n, asJSON)), append(newEnvs, seal(n, asProto))
	}
	row := func(what string, old, new Frame) {
		j, _ := json.Marshal(old)
		p, err := EncodeFrame(new)
		if err != nil {
			t.Fatal(err)
		}
		sent := len(p)
		if d := DeflatedSize(p); d < sent {
			sent = d
		}
		t.Logf("| %-44s | %7d | %7d | %7d | %7d | %7d |", what, len(j), DeflatedSize(j), len(p), DeflatedSize(p), sent)
		if len(p) >= len(j) {
			t.Errorf("%s: protobuf is not smaller than JSON", what)
		}
	}
	t.Logf("the event itself, before it is sealed: %d bytes as JSON, %d as protobuf", oldPlain, newPlain)
	t.Logf("| %-44s | %7s | %7s | %7s | %7s | %7s |", "Frame", "JSON", "deflated", "protobuf", "deflated", "sent")
	row("deliver, one message of 200 bytes of text", Frame{Frame: FDeliver, Envelope: &oldEnvs[0]}, Frame{Frame: FDeliver, Envelope: &newEnvs[0]})
	row("deliver, a batch of 100 such messages", Frame{Frame: FDeliver, Envelopes: oldEnvs}, Frame{Frame: FDeliver, Envelopes: newEnvs})
	ack := Frame{Frame: FAck, StreamID: stream, Cursor: 90_000}
	row("ack", ack, ack)
	accepted := Frame{Frame: FAccepted, Re: 31, IDs: []string{oldEnvs[0].OriginEventID}}
	row("accepted, one envelope", accepted, accepted)
	chunk := Frame{Frame: FDirect, ID: 9, From: "chat.example.org", To: "other.example.net", Body: secret.RandomBytes(ChunkBytes + 200), Sig: secret.RandomBytes(64)}
	row("direct, one 256 KiB chunk of a file", chunk, chunk)
}
