package wire

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
)

func keys(t *testing.T) *secret.Keys {
	t.Helper()
	k, err := secret.Open(filepath.Join(t.TempDir(), "keys"))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func ident(t *testing.T, name, kind string) Identity {
	return Identity{Name: name, Kind: kind, Keys: keys(t), Software: "churr/test"}
}

func TestDecodeIsStrict(t *testing.T) {
	if _, err := Decode("AAAA"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"AA==", "AA+/", "A A", "AAA\n"} {
		if _, err := Decode(bad); err == nil {
			t.Errorf("Decode(%q) was accepted", bad)
		}
	}
	var b Bytes
	if err := json.Unmarshal([]byte(`"AA=="`), &b); err == nil {
		t.Error("padding was accepted inside JSON")
	}
}

// Case 1: the document has exactly the fields of section 3 and box_key_sig
// verifies. Case 2: the fingerprint has the documented form.
func TestWellKnownAndFingerprint(t *testing.T) {
	id := ident(t, "a.example", KindServer)
	id.URL = "wss://a.example/link/v1"
	b, _ := json.Marshal(id.WellKnown())
	var fields map[string]json.RawMessage
	json.Unmarshal(b, &fields)
	for _, f := range []string{"churr", "kind", "name", "sign_key", "box_key", "box_key_sig", "link"} {
		if _, ok := fields[f]; !ok {
			t.Errorf("the document lacks %s", f)
		}
		delete(fields, f)
	}
	if len(fields) != 0 {
		t.Errorf("the document has fields the protocol does not: %v", fields)
	}
	var w WellKnown
	json.Unmarshal(b, &w)
	if err := w.Check("a.example"); err != nil {
		t.Fatal(err)
	}
	if err := w.Check("b.example"); err == nil {
		t.Error("a document for another host was accepted")
	}
	w.BoxKey[0] ^= 1
	if err := w.Check("a.example"); err == nil {
		t.Error("a changed box key was accepted")
	}
	hub := ident(t, "hub.example", KindHub).WellKnown()
	if hub.BoxKey != nil || hub.BoxKeySig != nil {
		t.Error("a hub published a box key")
	}
	fp := KeyFingerprint(id.Keys.SignPublic())
	if !regexp.MustCompile(`^([A-Z2-7]{4}-){7}[A-Z2-7]{4}$`).MatchString(fp) {
		t.Errorf("fingerprint %q is not eight groups of four", fp)
	}
}

func TestInvitationRoundTrip(t *testing.T) {
	id := ident(t, "a.example", KindServer)
	inv := Invitation{V: 1, Kind: KindServer, Name: "a.example", URL: "wss://a.example/link/v1",
		FP: Fingerprint(id.Keys.SignPublic()), Secret: secret.RandomBytes(32), ExpiresAt: model.Now()}
	s := inv.String()
	if !strings.HasPrefix(s, "churr_link_") {
		t.Fatal(s)
	}
	got, err := ParseInvitation(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != inv.Name || string(got.Secret) != string(inv.Secret) || FingerprintText(got.FP) != KeyFingerprint(id.Keys.SignPublic()) {
		t.Error("the invitation did not survive the round trip")
	}
	for _, bad := range []string{"", "churr_link_", "churr_link_!!!", "churr_app_abc", InvitePrefix + Encode([]byte(`{"v":2}`))} {
		if _, err := ParseInvitation(bad); err == nil {
			t.Errorf("ParseInvitation(%q) was accepted", bad)
		}
	}
}

func TestSignedBlock(t *testing.T) {
	a, b := keys(t), keys(t)
	f, err := SignBlock(FServerJoined, Control{StreamID: "s", Owner: "a.example", Version: 2}, a)
	if err != nil {
		t.Fatal(err)
	}
	var c Control
	if err := VerifyBlock(f, a.SignPublic(), &c); err != nil || c.Version != 2 {
		t.Fatal(err, c)
	}
	if err := VerifyBlock(f, b.SignPublic(), &c); AsError(err).Code != EBadSignature {
		t.Error("another server's key verified the block")
	}
	// The frame name is inside what is signed.
	f.Frame = FServerLeft
	if err := VerifyBlock(f, a.SignPublic(), &c); err == nil {
		t.Error("a block verified under another frame name")
	}
}

// Case 27: a ciphertext moved into an envelope with a different stream,
// epoch or origin fails to decrypt.
func TestEnvelope(t *testing.T) {
	a := keys(t)
	key := secret.RandomBytes(32)
	stream, event := model.NewID[string](), model.NewID[string]()
	env, err := SealEnvelope(key, stream, 3, "a.example", event, Stamp{At: 1_700_000_000_000, N: 12, Offset: -250}, []byte(`{"type":"x"}`), a)
	if err != nil {
		t.Fatal(err)
	}
	if !env.WellFormed() || !env.Verify(a.SignPublic()) {
		t.Fatal("a fresh envelope does not verify")
	}
	if got, err := env.Open(key); err != nil || string(got) != `{"type":"x"}` {
		t.Fatal(err, string(got))
	}
	if env.Verify(keys(t).SignPublic()) {
		t.Error("another server's key verified the envelope")
	}
	moved := []func(e *Envelope){
		func(e *Envelope) { e.StreamID = model.NewID[string]() },
		func(e *Envelope) { e.KeyEpoch = 4 },
		func(e *Envelope) { e.Origin = "b.example" },
		func(e *Envelope) { e.OriginEventID = model.NewID[string]() },
		// The stamp is the origin's and nobody else's: a carrier that moves
		// an event in time, renumbers it, or changes the offset it claims to
		// have used breaks both the signature and the seal.
		func(e *Envelope) { e.At += 1 },
		func(e *Envelope) { e.At -= 60_000 },
		func(e *Envelope) { e.N = 13 },
		func(e *Envelope) { e.Offset = 0 },
	}
	if env.Stamp() != (Stamp{At: 1_700_000_000_000, N: 12, Offset: -250}) {
		t.Errorf("the stamp: %+v", env.Stamp())
	}
	for i, change := range moved {
		e := env
		change(&e)
		if e.Verify(a.SignPublic()) {
			t.Errorf("change %d: the signature still verifies", i)
		}
		if _, err := e.Open(key); err == nil {
			t.Errorf("change %d: the ciphertext still opens", i)
		}
	}
	// What a carrier adds beside the envelope is not signed, and says
	// nothing about where the event stands.
	e := env
	e.Cursor, e.Prev = 99, 11
	if !e.Verify(a.SignPublic()) {
		t.Error("the delivery cursor is part of the signature")
	}
	if _, err := env.Open(secret.RandomBytes(32)); err == nil {
		t.Error("another key opened the envelope")
	}
}

// Case 41: a direct message opened by a server other than `to` fails, and
// one whose `from` was changed fails.
func TestDirect(t *testing.T) {
	a, b, c := keys(t), keys(t), keys(t)
	f, err := SealDirect(a, "a.example", "b.example", b.BoxPublic(), []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := OpenDirect(b, "b.example", f, a.SignPublic()); err != nil || string(got) != "hello" {
		t.Fatal(err, string(got))
	}
	if _, err := OpenDirect(c, "c.example", f, a.SignPublic()); err == nil {
		t.Error("a server other than `to` opened it")
	}
	g := f
	g.To = "c.example"
	if _, err := OpenDirect(c, "c.example", g, a.SignPublic()); err == nil {
		t.Error("a redirected message opened")
	}
	g = f
	g.From = "c.example"
	if VerifyDirect(g, c.SignPublic()) || VerifyDirect(g, a.SignPublic()) {
		t.Error("a message with a changed `from` verified")
	}
	// Re-signed by C as its own: the signature verifies, the box does not
	// open, because the sender's name is inside the key derivation.
	g.Sig = c.Sign(directMessage("c.example", "b.example", g.Body))
	if _, err := OpenDirect(b, "b.example", g, c.SignPublic()); err == nil {
		t.Error("a message re-signed by another server opened")
	}
}

// listen starts a listener that accepts with authorize and hands each
// connection to got.
func listen(t *testing.T, id Identity, authorize Authorizer, got chan<- *Conn) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := Accept(w, r, id, authorize)
		if err != nil {
			return
		}
		got <- c
		<-c.Done()
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestHandshake(t *testing.T) {
	a, b := ident(t, "a.example", KindServer), ident(t, "b.example", KindServer)
	got := make(chan *Conn, 1)
	var sawProof []byte
	inviteHash := InviteHash([]byte("0123456789abcdef0123456789abcdef"))
	url := listen(t, a, func(d Frame, proof, th []byte) error {
		sawProof = proof
		if !ProofMatches(inviteHash, th, proof) {
			return Errf(EInviteInvalid, "no")
		}
		return nil
	}, got)

	c, err := Dial(context.Background(), DialOptions{URL: url, Identity: b, InviteSecret: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	server := <-got
	if c.Remote.Name != "a.example" || server.Remote.Name != "b.example" || string(server.Remote.BoxKey) != string(b.Keys.BoxPublic()) {
		t.Fatal("the two sides do not hold each other's hello")
	}
	// The connection outlives the handshake's context, and carries frames.
	if err := c.Send(Frame{Frame: FAck, StreamID: "x", Cursor: 7}); err != nil {
		t.Fatal(err)
	}
	f, _, err := server.Read(context.Background())
	if err != nil || f.Frame != FAck || f.Cursor != 7 {
		t.Fatal(err, f)
	}
	c.Close()

	// A wrong secret is refused with the listener's code.
	_, err = Dial(context.Background(), DialOptions{URL: url, Identity: b, InviteSecret: []byte("ffffffffffffffffffffffffffffffff")})
	if AsError(err).Code != EInviteInvalid {
		t.Fatalf("a wrong secret: %v", err)
	}

	// Case 8: a dialler offering only an unknown version.
	_, err = Dial(context.Background(), DialOptions{URL: url, Identity: b, Versions: []int{7}})
	if AsError(err).Code != EVersionUnsupported {
		t.Fatalf("an unknown version: %v", err)
	}

	// Case 86: a dialler of version 1, the protocol before events were
	// stamped by a shared clock, is refused the same way, and the refusal
	// says which version is spoken here.
	_, err = Dial(context.Background(), DialOptions{URL: url, Identity: b, Versions: []int{1}})
	if e := AsError(err); e.Code != EVersionUnsupported || !strings.Contains(e.Message, "version 2") {
		t.Fatalf("a version 1 dialler: %v", err)
	}
	// One that offers both is taken, at version 2.
	both, err := Dial(context.Background(), DialOptions{URL: url, Identity: b, Versions: []int{1, 2}, InviteSecret: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil || both.Remote.Version != 2 {
		t.Fatalf("a dialler offering versions 1 and 2: %v", err)
	}
	both.Close()
	<-got

	// Case 6: a dialler that does not like the listener's key stops before
	// sending the invitation proof.
	sawProof = nil
	stop := errors.New("not the fingerprint in the invitation")
	_, err = Dial(context.Background(), DialOptions{URL: url, Identity: b, InviteSecret: []byte("0123456789abcdef0123456789abcdef"),
		Check: func(Frame) error { return stop }})
	if !errors.Is(err, stop) || sawProof != nil {
		t.Fatalf("the dialler went on: %v, proof seen: %v", err, sawProof != nil)
	}
}

// Case 9: a handshake signature copied from another connection fails,
// because both nonces are inside what is signed.
func TestHandshakeSignatureIsBoundToItsConnection(t *testing.T) {
	a, b := ident(t, "a.example", KindServer), ident(t, "b.example", KindServer)
	h1, _ := json.Marshal(b.hello())
	h2, _ := json.Marshal(a.hello())
	th1 := transcript(h1, h2)
	sig := b.Keys.Sign(authMessage("dialler", th1))
	// A second connection has fresh nonces, so a fresh transcript.
	h1b, _ := json.Marshal(b.hello())
	h2b, _ := json.Marshal(a.hello())
	th2 := transcript(h1b, h2b)
	if verify(b.Keys.SignPublic(), authMessage("dialler", th2), sig) {
		t.Error("a signature from one connection verified on another")
	}
	if verify(b.Keys.SignPublic(), authMessage("listener", th1), sig) {
		t.Error("a dialler's signature verified as a listener's")
	}
	if ProofMatches(InviteHash([]byte("s")), th2, InviteProof(InviteHash([]byte("s")), th1)) {
		t.Error("an invitation proof from one connection matched on another")
	}
}

// Case 86, the other way round: a listener of another version. The dialler
// stops at the listener's hello with version_unsupported, having sent
// nothing but its own hello: no signature, and nothing of the invitation.
func TestDiallerRefusesAnotherVersion(t *testing.T) {
	a, b := ident(t, "a.example", KindServer), ident(t, "b.example", KindServer)
	sent := make(chan int, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{Subprotocol}, InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer ws.CloseNow()
		ctx := r.Context()
		if _, _, err := ws.Read(ctx); err != nil {
			return
		}
		hello := a.hello()
		hello.Version = 1
		raw, _ := json.Marshal(hello)
		ws.Write(ctx, websocket.MessageText, raw)
		n := 0
		for {
			if _, _, err := ws.Read(ctx); err != nil {
				break
			}
			n++
		}
		sent <- n
	}))
	t.Cleanup(srv.Close)
	_, err := Dial(context.Background(), DialOptions{URL: "ws" + strings.TrimPrefix(srv.URL, "http"), Identity: b,
		InviteSecret: []byte("0123456789abcdef0123456789abcdef")})
	if e := AsError(err); e.Code != EVersionUnsupported {
		t.Fatalf("a listener of version 1: %v", err)
	}
	if n := <-sent; n != 0 {
		t.Fatalf("the dialler sent %d more frames to a listener of another version", n)
	}
}
