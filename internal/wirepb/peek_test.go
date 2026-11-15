package wirepb

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// TestPeekFrame: for every frame vector, PeekFrame names the type,
// conversation and seq that the JSON has, and hands back the body's bytes.
func TestPeekFrame(t *testing.T) {
	seen := 0
	for _, v := range readVectors(t, "frames.jsonl") {
		var want struct {
			Type string `json:"type"`
			Conv string `json:"conversation_id"`
			Seq  int64  `json:"seq"`
		}
		json.Unmarshal(v.JSON, &want)
		pb := v.pb(t)
		h, ok := PeekFrame(pb)
		if !ok {
			t.Fatalf("%s: not read", v.Name)
		}
		if h.JSON != nil {
			continue
		}
		seen++
		conv := strings.ReplaceAll(want.Conv, "-", "")
		if h.Type != want.Type || h.Seq != want.Seq || hex.EncodeToString(h.ConversationID) != conv {
			t.Fatalf("%s: got %q %x %d, want %q %s %d", v.Name, h.Type, h.ConversationID, h.Seq, want.Type, conv, want.Seq)
		}
		if len(h.Body) > 0 && !bytes.Contains(pb, h.Body) {
			t.Fatalf("%s: the body is not part of the envelope", v.Name)
		}
	}
	if seen < 100 {
		t.Fatalf("only %d typed vectors", seen)
	}
	if _, ok := PeekFrame([]byte{0xff, 0xff, 0xff}); ok {
		t.Fatal("garbage was read")
	}
	if n := testing.AllocsPerRun(100, func() { PeekFrame(readFixturePB) }); n != 0 {
		t.Fatalf("PeekFrame allocates %v times", n)
	}
}

var readFixturePB = EncodeFrame(map[string]any{"type": "typing.started", "at": "2026-11-13T09:30:00.000Z",
	"conversation_id": "0193a6f0-1111-7b3a-9f0e-2a4d5c6b7e8f", "account_id": "0193a001-0000-7000-8000-000000000001"})
