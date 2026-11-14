package calls_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/tikaydaes/churr/internal/sealed/ref"
)

// The vectors for call frames in a sealed chat (docs/CALL-FRAMES.md):
// testdata/call-frames/vectors.json. This test is the reference that
// writes the file and checks it; the web chat's worker (e2e/call-frames.mjs)
// and the app (app/test/core/calls/frame_vectors_test.dart) are checked
// against the same file, so the three produce and accept the same bytes.
//
//	go test ./internal/calls -run TestCallFrameVectors                         check
//	UPDATE_CALL_FRAMES=1 go test ./internal/calls -run TestCallFrameVectors    rewrite
const frameVectorsFile = "../../testdata/call-frames/vectors.json"

const frameSalt = "churr call frames v1"

// frameKey derives the AES-128 key from a call sender key as the apps'
// plugin does.
func frameKey(t testing.TB, callSenderKey []byte) []byte {
	k, err := hkdf.Key(sha256.New, callSenderKey, []byte(frameSalt), string(make([]byte, 128)), 16)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// frameHeader says how many leading bytes of a frame stay readable.
func frameHeader(kind string, frame []byte) int {
	n := 1
	if kind == "video" {
		n = 3
		if len(frame) > 0 && frame[0]&1 == 0 {
			n = 10 // a VP8 key frame
		}
	}
	return min(n, len(frame))
}

func frameAEAD(t testing.TB, key []byte) cipher.AEAD {
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	return aead
}

func sealFrame(t testing.TB, key []byte, index byte, iv []byte, kind string, plain []byte) []byte {
	n := frameHeader(kind, plain)
	out := append([]byte{}, plain[:n]...)
	out = frameAEAD(t, key).Seal(out, iv, plain[n:], plain[:n])
	out = append(out, iv...)
	return append(out, byte(len(iv)), index)
}

// openFrame decrypts a frame with the key held at one index, as a receiver
// does: nil when the frame names another index, is malformed, or does not
// open.
func openFrame(t testing.TB, key []byte, held byte, kind string, frame []byte) []byte {
	const tag, trailer = 16, 14
	if len(frame) < tag+trailer || frame[len(frame)-2] != 12 {
		return nil
	}
	n := frameHeader(kind, frame)
	if len(frame) < n+tag+trailer || frame[len(frame)-1] != held || held >= 16 {
		return nil
	}
	ivAt := len(frame) - trailer
	plain, err := frameAEAD(t, key).Open(nil, frame[ivAt:ivAt+12], frame[n:ivAt], frame[:n])
	if err != nil {
		return nil
	}
	return append(append([]byte{}, frame[:n]...), plain...)
}

type frameDevice struct {
	DeviceID      string `json:"device_id"`
	CallSenderKey string `json:"call_sender_key"`
	AESKey        string `json:"aes_key"`
}

type frameEpoch struct {
	KeyEpoch    uint64        `json:"key_epoch"`
	EpochSecret string        `json:"epoch_secret"`
	KeyIndex    byte          `json:"key_index"`
	CallSecret  string        `json:"call_secret"`
	Devices     []frameDevice `json:"devices"`
}

type frameCase struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	DeviceID      string `json:"device_id,omitempty"`
	KeyEpoch      uint64 `json:"key_epoch,omitempty"`
	CallSenderKey string `json:"call_sender_key"`
	AESKey        string `json:"aes_key"`
	KeyIndex      byte   `json:"key_index"`
	HeaderLength  int    `json:"header_length"`
	Plain         string `json:"plain"`
	IV            string `json:"iv"`
	Frame         string `json:"frame"`
}

type frameRefusal struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	CallSenderKey string `json:"call_sender_key"`
	KeyIndexHeld  byte   `json:"key_index_held"`
	Frame         string `json:"frame"`
}

type frameVectors struct {
	Description    string         `json:"description"`
	Salt           string         `json:"salt"`
	InfoLength     int            `json:"info_length"`
	ConversationID string         `json:"conversation_id"`
	CallID         string         `json:"call_id"`
	Epochs         []frameEpoch   `json:"epochs"`
	Frames         []frameCase    `json:"frames"`
	Refused        []frameRefusal `json:"refused"`
}

func unhex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("not hex: %q", s)
	}
	return b
}

// buildFrameVectors makes the file's contents from fixed inputs. The
// conversation, call, devices and first epoch secret are those of
// testdata/sealed/call_keys.json, so the two files meet.
func buildFrameVectors(t testing.TB) frameVectors {
	conv, call := ref.MustUUID("0193a0e0-0000-7000-8000-0000000000c0"), ref.MustUUID("0193a0ca-0000-7000-8000-00000000ca11")
	a1, a2, b1 := "0193aa00-0000-7000-8000-0000000000a1", "0193aa00-0000-7000-8000-0000000000a2", "0193aa00-0000-7000-8000-0000000000b1"
	v := frameVectors{
		Description: "Call frames of a sealed chat's call, docs/CALL-FRAMES.md. epochs: from an epoch secret to each device's call sender key " +
			"(SEALED.md 5.2) and on to the AES-128 key frames are encrypted under: HKDF-SHA-256(ikm = call sender key, salt = `salt` as ASCII, " +
			"info = `info_length` zero bytes, 16 bytes). frames: `plain` is one encoded frame; its first `header_length` bytes stay readable " +
			"and are the associated data; `frame` is header | AES-128-GCM ciphertext and 16-byte tag | iv | 0c | key index. A sender given the " +
			"same iv must produce `frame` exactly, and a receiver holding the key at `key_index` must get `plain` back. refused: a receiver " +
			"holding `call_sender_key` at `key_index_held` and no other key must drop `frame`. All bytes are lower-case hex. Written by " +
			"internal/calls/frames_vectors_test.go; do not edit by hand.",
		Salt: frameSalt, InfoLength: 128, ConversationID: conv.String(), CallID: call.String(),
	}
	keys := map[uint64]map[string][]byte{}
	for _, e := range []struct {
		epoch   uint64
		secret  string
		devices []string
	}{
		{1, "fe0f6ba3006399c1ee4854235f9ce60558094eb2670b1c8d74658aca123eff88", []string{a1, a2, b1}},
		// A later epoch that leaves one device out, as after it is removed.
		{18, "2b7e151628aed2a6abf7158809cf4f3c762e7160f38b4da56a784d9045190cfe", []string{a1, b1}},
	} {
		secret := ref.CallSecret(unhex(t, e.secret), conv, e.epoch, call)
		fe := frameEpoch{KeyEpoch: e.epoch, EpochSecret: e.secret, KeyIndex: ref.CallKeyIndex(e.epoch), CallSecret: hex.EncodeToString(secret)}
		keys[e.epoch] = map[string][]byte{}
		for _, d := range e.devices {
			k := ref.CallSenderKey(secret, conv, e.epoch, ref.MustUUID(d))
			keys[e.epoch][d] = k
			fe.Devices = append(fe.Devices, frameDevice{DeviceID: d, CallSenderKey: hex.EncodeToString(k), AESKey: hex.EncodeToString(frameKey(t, k))})
		}
		v.Epochs = append(v.Epochs, fe)
	}

	add := func(name, kind, device string, epoch uint64, sender []byte, index byte, iv, plain string) frameCase {
		p := unhex(t, plain)
		aes := frameKey(t, sender)
		c := frameCase{Name: name, Kind: kind, DeviceID: device, KeyEpoch: epoch, CallSenderKey: hex.EncodeToString(sender), AESKey: hex.EncodeToString(aes),
			KeyIndex: index, HeaderLength: frameHeader(kind, p), Plain: plain, IV: iv,
			Frame: hex.EncodeToString(sealFrame(t, aes, index, unhex(t, iv), kind, p))}
		v.Frames = append(v.Frames, c)
		return c
	}
	opus := "78" + "0b e4 c1 36 ec c5 8d 8c 49 46 0f 4a 02 6e 4b 21 bd 9a 5f 03 77 10 2e 88"
	vp8Key := "10 02 00 9d 01 2a 40 01 b4 00" + "39 03 00 00 1c 22 16 16 22 66 12 20 04 90 40 00 c6 5a 7d 1e 91 3f a0"
	vp8Delta := "31 01 00" + "05 10 f4 3b 8a 91 2c 5e 77 00 d2 19 a4"
	clean := func(s string) string { return string(bytes.ReplaceAll([]byte(s), []byte(" "), nil)) }

	// What the app's plugin was seen to send (CALL-FRAMES.md section 4).
	add("a frame the app's plugin sent (CALL-FRAMES.md section 4)", "audio", "", 0, bytes.Repeat([]byte{0x11}, 32), 5, "93a37d1a33cd967433cd9672", "780102030405")
	audio := add("an Opus frame, epoch 1", "audio", a1, 1, keys[1][a1], 1, "5c0ffee50000000000000001", clean(opus))
	add("a VP8 key frame, epoch 1: ten readable bytes", "video", b1, 1, keys[1][b1], 1, "0badc0de0000000000000002", clean(vp8Key))
	add("a VP8 delta frame, epoch 1: three readable bytes", "video", b1, 1, keys[1][b1], 1, "0badc0de0000000000000003", clean(vp8Delta))
	add("the same Opus frame after the epoch changed: another key and index", "audio", a1, 18, keys[18][a1], 2, "5c0ffee50000000000000004", clean(opus))
	add("an Opus frame of one byte: nothing but the header and the tag", "audio", a2, 1, keys[1][a2], 1, "a2a2a2a20000000000000005", "78")
	add("a VP8 delta frame of three bytes", "video", a2, 1, keys[1][a2], 1, "a2a2a2a20000000000000006", "310100")
	add("a VP8 key frame of ten bytes: nothing but the header and the tag", "video", a2, 1, keys[1][a2], 1, "a2a2a2a20000000000000007", "1002009d012a4001b400")
	add("key index 15, the last of the ring", "audio", "", 0, bytes.Repeat([]byte{0x22}, 32), 15, "000000000000000000000000", "fc0a0b0c")

	good := unhex(t, audio.Frame)
	refuse := func(name string, key []byte, held byte, change func(f []byte) []byte) {
		f := change(append([]byte{}, good...))
		v.Refused = append(v.Refused, frameRefusal{Name: name, Kind: "audio", CallSenderKey: hex.EncodeToString(key), KeyIndexHeld: held, Frame: hex.EncodeToString(f)})
	}
	same := func(f []byte) []byte { return f }
	flip := func(at int) func([]byte) []byte {
		return func(f []byte) []byte {
			if at < 0 {
				at += len(f)
			}
			f[at] ^= 1
			return f
		}
	}
	refuse("another device's key (the account's second device)", keys[1][a2], 1, same)
	refuse("the next epoch's key for the same device, held at the frame's index", keys[18][a1], 1, same)
	refuse("the right key held at another index", keys[1][a1], 2, same)
	refuse("the readable header was changed", keys[1][a1], 1, flip(0))
	refuse("the ciphertext was changed", keys[1][a1], 1, flip(3))
	refuse("the tag was changed", keys[1][a1], 1, flip(-15))
	refuse("the IV was changed", keys[1][a1], 1, flip(-3))
	refuse("the IV length byte is not 12", keys[1][a1], 1, func(f []byte) []byte { f[len(f)-2] = 13; return f })
	refuse("the key index names another key", keys[1][a1], 1, func(f []byte) []byte { f[len(f)-1] = 2; return f })
	refuse("a key index beyond the ring of 16", keys[1][a1], 1, func(f []byte) []byte { f[len(f)-1] = 17; return f })
	refuse("cut short: the last byte is missing", keys[1][a1], 1, func(f []byte) []byte { return f[:len(f)-1] })
	refuse("too short to hold a tag and the trailer", keys[1][a1], 1, func(f []byte) []byte { return append(f[:8], f[len(f)-14:]...) })
	refuse("not encrypted at all", keys[1][a1], 1, func([]byte) []byte { return unhex(t, clean(opus)) })
	refuse("empty", keys[1][a1], 1, func([]byte) []byte { return []byte{} })
	return v
}

func TestCallFrameVectors(t *testing.T) {
	built, err := json.MarshalIndent(buildFrameVectors(t), "", " ")
	if err != nil {
		t.Fatal(err)
	}
	built = append(built, '\n')
	if os.Getenv("UPDATE_CALL_FRAMES") != "" {
		if err := os.WriteFile(frameVectorsFile, built, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := os.ReadFile(frameVectorsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, built) {
		t.Fatalf("%s is not what this reference writes; run with UPDATE_CALL_FRAMES=1 if the format changed on purpose", frameVectorsFile)
	}

	// The file is then read as any other implementation reads it.
	var v frameVectors
	if err := json.Unmarshal(stored, &v); err != nil {
		t.Fatal(err)
	}
	conv, call := ref.MustUUID(v.ConversationID), ref.MustUUID(v.CallID)
	for _, e := range v.Epochs {
		secret := ref.CallSecret(unhex(t, e.EpochSecret), conv, e.KeyEpoch, call)
		if hex.EncodeToString(secret) != e.CallSecret || ref.CallKeyIndex(e.KeyEpoch) != e.KeyIndex {
			t.Errorf("epoch %d: the call secret or the key index", e.KeyEpoch)
		}
		for _, d := range e.Devices {
			k := ref.CallSenderKey(secret, conv, e.KeyEpoch, ref.MustUUID(d.DeviceID))
			if hex.EncodeToString(k) != d.CallSenderKey || hex.EncodeToString(frameKey(t, k)) != d.AESKey {
				t.Errorf("epoch %d, device %s: the keys", e.KeyEpoch, d.DeviceID)
			}
		}
	}
	for _, c := range v.Frames {
		key := frameKey(t, unhex(t, c.CallSenderKey))
		plain, frame := unhex(t, c.Plain), unhex(t, c.Frame)
		if hex.EncodeToString(key) != c.AESKey {
			t.Errorf("%s: the AES key", c.Name)
		}
		if frameHeader(c.Kind, plain) != c.HeaderLength || frameHeader(c.Kind, frame) != c.HeaderLength {
			t.Errorf("%s: the header length", c.Name)
		}
		if got := sealFrame(t, key, c.KeyIndex, unhex(t, c.IV), c.Kind, plain); !bytes.Equal(got, frame) {
			t.Errorf("%s: sealed to %x", c.Name, got)
		}
		if got := openFrame(t, key, c.KeyIndex, c.Kind, frame); !bytes.Equal(got, plain) {
			t.Errorf("%s: opened to %x", c.Name, got)
		}
	}
	for _, r := range v.Refused {
		if got := openFrame(t, frameKey(t, unhex(t, r.CallSenderKey)), r.KeyIndexHeld, r.Kind, unhex(t, r.Frame)); got != nil {
			t.Errorf("%s: opened to %x, and should have been dropped", r.Name, got)
		}
	}
	// The first epoch is the one of the sealed chats' own vectors.
	var sealed struct {
		CallSenderKeys map[string]string `json:"call_sender_keys"`
	}
	raw, err := os.ReadFile("../../testdata/sealed/call_keys.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &sealed); err != nil {
		t.Fatal(err)
	}
	for _, d := range v.Epochs[0].Devices {
		if sealed.CallSenderKeys[d.DeviceID] != d.CallSenderKey {
			t.Errorf("device %s: the call sender key differs from testdata/sealed/call_keys.json", d.DeviceID)
		}
	}
}
