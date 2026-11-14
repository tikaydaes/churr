package ref

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func unhex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The published vectors, to prove the standard library is being called
// correctly.
func TestPrimitives(t *testing.T) {
	// RFC 7748 section 6.1.
	alice := unhex(t, "77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a")
	bobPub := unhex(t, "de9edb7d7b7dc1b4d35b61c2ece435373f8343c85b78674dadfc7e146f882b4f")
	if got := hex.EncodeToString(X25519Public(alice)); got != "8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a" {
		t.Fatalf("X25519 public: %s", got)
	}
	shared, err := X25519(alice, bobPub)
	if err != nil || hex.EncodeToString(shared) != "4a5d9d5ba4ce2de1728e3bf480350f25e07e21c947d19e3376f09b3c1e161742" {
		t.Fatalf("X25519 shared: %x %v", shared, err)
	}
	if _, err := X25519(alice, make([]byte, 32)); !errors.Is(err, ErrZeroShared) {
		t.Fatalf("an all-zero result must be refused: %v", err)
	}
	// RFC 5869 test case 1.
	okm := HKDF(bytes.Repeat([]byte{0x0b}, 22), unhex(t, "000102030405060708090a0b0c"), unhex(t, "f0f1f2f3f4f5f6f7f8f9"), 42)
	if hex.EncodeToString(okm) != "3cb25f25faacd57a90434f64d0362f2a2d2d0a90cf1a5a4c5db02d56ecc4c5bf34007208d5b887185865" {
		t.Fatalf("HKDF: %x", okm)
	}
	// AES-256-GCM, the all-zero test cases of the GCM specification.
	zero := make([]byte, 32)
	if got := hex.EncodeToString(gcmSeal(zero, zero[:12], nil, nil)); got != "530f8afbc74536b9a963b4f1c4cb738b" {
		t.Fatalf("GCM empty: %s", got)
	}
	if got := hex.EncodeToString(gcmSeal(zero, zero[:12], zero[:16], nil)); got != "cea7403d4d606b6e074ec5d3baf39d18d0d1c8a799996bf0265b98b5d48ab919" {
		t.Fatalf("GCM block: %s", got)
	}
	// RFC 8032 section 7.1, test 1.
	key := SigningKey(unhex(t, "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"))
	if hex.EncodeToString(Public(key)) != "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a" {
		t.Fatalf("Ed25519 public: %x", Public(key))
	}
}

func TestPhrase(t *testing.T) {
	// The first vectors of BIP-39.
	for entropy, want := range map[string]string{
		"00000000000000000000000000000000": "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
		"7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f": "legal winner thank year wave sausage worth useful legal winner thank yellow",
		"80808080808080808080808080808080": "letter advice cage absurd amount doctor acoustic avoid letter advice cage above",
		"ffffffffffffffffffffffffffffffff": "zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo wrong",
	} {
		if got := Phrase(unhex(t, entropy)); got != want {
			t.Fatalf("Phrase(%s) = %q", entropy, got)
		}
		back, err := ReadPhrase("  " + strings.ToUpper(want) + " ")
		if err != nil || hex.EncodeToString(back) != entropy {
			t.Fatalf("ReadPhrase(%q) = %x %v", want, back, err)
		}
	}
	// The first four letters identify a word.
	if b, err := ReadPhrase("lega winn than year wave saus wort usef lega winn than yell"); err != nil || hex.EncodeToString(b) != "7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f" {
		t.Fatalf("four letters: %x %v", b, err)
	}
	for _, bad := range []string{
		"abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon", // checksum
		"abandon abandon abandon",
		"abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon churr",
	} {
		if _, err := ReadPhrase(bad); !errors.Is(err, ErrPhrase) {
			t.Fatalf("ReadPhrase(%q) must be refused: %v", bad, err)
		}
	}
	// No two words share their first four letters, which ReadPhrase
	// relies on.
	seen := map[string]string{}
	for _, w := range Words {
		k := w
		if len(k) > 4 {
			k = k[:4]
		}
		if other, dup := seen[k]; dup {
			t.Fatalf("%q and %q share their first four letters", w, other)
		}
		seen[k] = w
	}
}

func TestSealAndLock(t *testing.T) {
	priv := Fixed("test recipient", 32)
	sealed, err := Seal(X25519Public(priv), "churr seal test v1", []byte("context"), []byte("thirty-two bytes of plaintext!!!"), Fixed("test one-time", 32))
	if err != nil || len(sealed) != 80 {
		t.Fatalf("seal: %d %v", len(sealed), err)
	}
	if got, err := Open(priv, "churr seal test v1", []byte("context"), sealed); err != nil || string(got) != "thirty-two bytes of plaintext!!!" {
		t.Fatalf("open: %q %v", got, err)
	}
	for name, try := range map[string]func() ([]byte, error){
		"another label":   func() ([]byte, error) { return Open(priv, "churr seal other v1", []byte("context"), sealed) },
		"another context": func() ([]byte, error) { return Open(priv, "churr seal test v1", []byte("contexu"), sealed) },
		"another key": func() ([]byte, error) {
			return Open(Fixed("someone else", 32), "churr seal test v1", []byte("context"), sealed)
		},
		"a zero one-time key": func() ([]byte, error) {
			return Open(priv, "churr seal test v1", []byte("context"), append(make([]byte, 32), sealed[32:]...))
		},
	} {
		if _, err := try(); !errors.Is(err, ErrOpen) {
			t.Fatalf("%s must not open: %v", name, err)
		}
	}
	for i := range sealed {
		changed := append([]byte{}, sealed...)
		changed[i] ^= 1
		if _, err := Open(priv, "churr seal test v1", []byte("context"), changed); err == nil {
			t.Fatalf("a seal with byte %d changed opened", i)
		}
	}
	key := Fixed("test lock key", 32)
	locked := Lock(key, "churr lock test v1", []byte("ctx"), []byte("hello"), Fixed("test salt", 16))
	if got, err := Unlock(key, "churr lock test v1", []byte("ctx"), locked); err != nil || string(got) != "hello" {
		t.Fatalf("unlock: %q %v", got, err)
	}
	if _, err := Unlock(key, "churr lock test v2", []byte("ctx"), locked); err == nil {
		t.Fatal("a lock opened under another label")
	}
	for i := range locked {
		changed := append([]byte{}, locked...)
		changed[i] ^= 1
		if _, err := Unlock(key, "churr lock test v1", []byte("ctx"), changed); err == nil {
			t.Fatalf("a lock with byte %d changed opened", i)
		}
	}
}

func TestSignatureLabels(t *testing.T) {
	key := SigningKey(Fixed("label test", 32))
	record := []byte("a record")
	for _, made := range SignatureLabels {
		sig := Sign(key, made, record)
		for _, checked := range SignatureLabels {
			if Verify(Public(key), checked, record, sig) != (made == checked) {
				t.Fatalf("a signature under %q checked under %q", made, checked)
			}
		}
	}
}

func TestFiles(t *testing.T) {
	key := Fixed("file key", 32)
	for _, n := range []int{0, 1, 65535, 65536, 65537, 200000} {
		plain := make([]byte, n)
		for i := range plain {
			plain[i] = byte(i % 251)
		}
		blob := EncryptFile(key, plain)
		chunks := (n + ChunkSize - 1) / ChunkSize
		if chunks == 0 {
			chunks = 1
		}
		if len(blob) != n+16*chunks {
			t.Fatalf("%d bytes: blob of %d", n, len(blob))
		}
		got, err := DecryptFile(key, blob)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("%d bytes: %v", n, err)
		}
		if chunks > 1 {
			// Cut short at a chunk boundary, and chunks swapped.
			if _, err := DecryptFile(key, blob[:sealedChunk]); err == nil {
				t.Fatalf("%d bytes: a file cut short was accepted", n)
			}
			if len(blob) >= 2*sealedChunk {
				swapped := append(append([]byte{}, blob[sealedChunk:2*sealedChunk]...), blob[:sealedChunk]...)
				swapped = append(swapped, blob[2*sealedChunk:]...)
				if _, err := DecryptFile(key, swapped); err == nil {
					t.Fatalf("%d bytes: reordered chunks were accepted", n)
				}
			}
		}
	}
	// A last chunk without its mark.
	unmarked := gcmSeal(key, chunkNonce(0, false), []byte("x"), []byte(LabelFile))
	if _, err := DecryptFile(key, unmarked); err == nil {
		t.Fatal("a file with no last-chunk mark was accepted")
	}
}

func TestApprovalAndCompareCodes(t *testing.T) {
	code := ApprovalCode([]byte("request"))
	if len(code) != 23 || strings.Count(code, "-") != 3 {
		t.Fatalf("code: %q", code)
	}
	if NormalizeApprovalCode(strings.ToLower(strings.ReplaceAll(code, "-", " "))) != code {
		t.Fatalf("normalise: %q", code)
	}
	if NormalizeApprovalCode("OIL00-00000-00000-00000") != "01100-00000-00000-00000" {
		t.Fatal("letters that look like digits")
	}
	a := CompareHalf(Fixed("key a", 32), MustUUID("0193a000-0000-7000-8000-000000000001"), "chat.example.org")
	b := CompareHalf(Fixed("key b", 32), MustUUID("0193a000-0000-7000-8000-000000000002"), "chat.example.org")
	if len(a) != 30 || len(b) != 30 || a == b {
		t.Fatalf("halves: %q %q", a, b)
	}
	if CompareCode(a, b) != CompareCode(b, a) || len(CompareCode(a, b)) != 71 {
		t.Fatalf("code: %q", CompareCode(a, b))
	}
}

func TestTranscriptPlays(t *testing.T) {
	steps, cast, err := Transcript()
	if err != nil {
		t.Fatal(err)
	}
	if len(cast) != 7 {
		t.Fatalf("%d devices take part", len(cast))
	}
	if len(steps) < 30 {
		t.Fatalf("only %d steps", len(steps))
	}
	for _, s := range steps {
		for _, r := range s.Refused {
			t.Logf("step %d refused: %v", s.Step, r)
		}
	}
	last := steps[len(steps)-1]
	for name, v := range last.After {
		t.Logf("%s: unlocked=%v epochs=%d shows=%d unchecked=%d notices=%d", name, v.Unlocked, len(v.Epochs), len(v.Shows), v.Unchecked, len(v.Notices))
	}
}

// TestReplayBeforeOriginal: a replayed copy of a message that arrives
// before the original (a higher sequence number taken in first, as when a
// device reads an old epoch late) is drawn at first, then gives way to the
// original. The message must then be drawn once, at the original's place.
func TestReplayBeforeOriginal(t *testing.T) {
	e := buildEpoch()
	content := ContentKey(e.history, vConv, 1)
	ref := MustUUID("9c051cb7-2ffa-4a41-a05d-ce900221f7b1")
	hd := Header{Format: 1, ConversationID: vConv, KeyEpoch: 1, AccountID: vAnn, DeviceID: vAnnPhone.id, Counter: 1, Reference: ref}
	sealed, err := SealMessage(hd, SenderKey(content, vConv, 1, hd.DeviceID), Fixed("replay test salt", 16), MessageContent("once", nil, nil, nil, nil), vAnnPhone.sign)
	if err != nil {
		t.Fatal(err)
	}
	d := &Device{Account: vBob, Server: vServer, ID: vBobPhone.id, Platform: PlatformIOS, Sign: vBobPhone.sign, Box: vBobPhone.box,
		Rand: NewStream("unused"), Now: func() uint64 { return vTime + 6000 }, Pins: map[UUID]*Pin{}, Chats: map[UUID]*Chat{}}
	if err := d.LearnLists(vAnn, e.lists.ann[1:2]); err != nil {
		t.Fatal(err)
	}
	if err := d.LearnLists(vBob, []SignedList{e.lists.bob}); err != nil {
		t.Fatal(err)
	}
	c := d.Chat(vConv, vAnn)
	if err := c.AcceptEpoch(1, 1, e.stored); err != nil {
		t.Fatal(err)
	}
	// The replay, at seq 5, is taken in first and drawn.
	if got, err := c.Receive(Row{Seq: 5, Author: vAnn, KeyEpoch: 1, Ciphertext: sealed}); err != nil || got.Dropped {
		t.Fatalf("the replay taken in first: %+v %v", got, err)
	}
	// The original, at seq 2, takes its place.
	if got, err := c.Receive(Row{Seq: 2, Author: vAnn, KeyEpoch: 1, Ciphertext: sealed}); err != nil || got.Dropped {
		t.Fatalf("the original: %+v %v", got, err)
	}
	view := c.View()
	if len(view) != 1 || view[0].Reference != ref || view[0].Seq != 2 || len(c.Order) != 1 {
		t.Fatalf("drawn %d times (order %d), want once at seq 2: %+v", len(view), len(c.Order), view)
	}
	// Taking the replay in again changes nothing.
	if got, err := c.Receive(Row{Seq: 5, Author: vAnn, KeyEpoch: 1, Ciphertext: sealed}); err != nil || !got.Dropped {
		t.Fatalf("the replay again: %+v %v", got, err)
	}
	if view := c.View(); len(view) != 1 {
		t.Fatalf("drawn %d times after the replay came again", len(view))
	}
}
