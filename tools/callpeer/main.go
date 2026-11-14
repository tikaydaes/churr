// Command callpeer is a second participant for trying the apps' calls by
// hand: a Pion client (internal/calls/calltest) that joins the call in a
// conversation of a devseed community as one of its accounts, publishes
// made-up microphone and camera media, subscribes to everyone's video,
// and after a while reports what arrived from whom.
//
// With -key it also takes part in frame encryption the way a sealed
// chat's call does (docs/CALL-FRAMES.md): it encrypts the microphone
// frames it sends, and tries to decrypt the frames it receives from the
// other participant under each candidate reading of the frame format,
// reporting which one works. That is how the format in CALL-FRAMES.md was
// found.
//
//	go run ./tools/callpeer -seed seed.json -as sam -conv chat_sam -for 20s
package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/tikaydaes/churr/internal/calls/calltest"
)

type seedFile struct {
	Origin   string `json:"origin"`
	Accounts []struct {
		Username    string `json:"username"`
		ID          string `json:"id"`
		AccessToken string `json:"access_token"`
	} `json:"accounts"`
	Conversations map[string]string `json:"conversations"`
}

func main() {
	seedPath := flag.String("seed", "seed.json", "devseed's output")
	as := flag.String("as", "sam", "the account to join as")
	conv := flag.String("conv", "chat_sam", "the conversation, by its key in the seed")
	dur := flag.Duration("for", 20*time.Second, "how long to stay in the call")
	wait := flag.Duration("wait", 120*time.Second, "how long to wait for a call to start")
	keyHex := flag.String("key", "", "frame encryption: this device's 32-byte call sender key, hex")
	otherHex := flag.String("other-key", "", "frame encryption: the other participant's key, hex")
	keyIndex := flag.Int("key-index", 0, "frame encryption: the key index to send under")
	salt := flag.String("salt", "", "frame encryption: the key provider's salt, as text")
	kdf := flag.String("kdf", "", "frame encryption: how to make the AES key: hkdf, pbkdf2 or raw; empty to find out from what arrives")
	dump := flag.String("dump", "", "write the payloads received to this file, one hex line each")
	flag.Parse()

	var seed seedFile
	b, err := os.ReadFile(*seedPath)
	check(err)
	check(json.Unmarshal(b, &seed))
	var token, selfID string
	for _, a := range seed.Accounts {
		if a.Username == *as {
			token, selfID = a.AccessToken, a.ID
		}
	}
	convID := seed.Conversations[*conv]
	if token == "" || convID == "" {
		fail("no token for %s or no conversation %s in the seed", *as, *conv)
	}

	sig := dial(seed.Origin, token)
	t := logT{}

	// Wait for the app to start the call.
	var callID string
	deadline := time.Now().Add(*wait)
	for callID == "" && time.Now().Before(deadline) {
		var got struct {
			Call *struct {
				ID string `json:"id"`
			} `json:"call"`
		}
		if tryAPI(seed.Origin, token, "GET", "/api/v1/conversations/"+convID+"/call", &got) == nil && got.Call != nil {
			callID = got.Call.ID
			break
		}
		time.Sleep(time.Second)
	}
	if callID == "" {
		fail("no call started in %s within %v", *conv, *wait)
	}

	enc := &encrypter{index: byte(*keyIndex)}
	opts := calltest.Options{Simulcast: true, FrameEncryption: *keyHex != ""}
	var own, other []byte
	if *keyHex != "" {
		own, err = hex.DecodeString(*keyHex)
		check(err)
		other, err = hex.DecodeString(*otherHex)
		check(err)
		opts.Encrypt = enc.encrypt
		if *kdf != "" {
			enc.set(deriveKey(*kdf, own, []byte(*salt)))
		}
	}
	c := calltest.New(t, sig, opts)
	defer c.Close()

	var joined struct {
		Call struct {
			ID string `json:"id"`
		} `json:"call"`
		Ticket string `json:"ticket"`
	}
	api(seed.Origin, token, "POST", "/api/v1/conversations/"+convID+"/call/join", &joined)
	ok := c.MustJoin(joined.Call.ID, joined.Ticket)
	fmt.Printf("JOINED call %s as %s (%s)\n", joined.Call.ID, *as, selfID)
	if f := c.Publish("microphone", "camera"); f["type"] != "call.answer" {
		fail("publishing was answered with %v", f)
	}
	fmt.Printf("PUBLISHED %v\n", c.TrackIDs)

	// Everyone's tracks: those in the call already, and those published later.
	type track struct{ id, source, account string }
	var mu sync.Mutex
	tracks := map[string]track{}
	add := func(account string, t map[string]any) {
		id, _ := t["id"].(string)
		src, _ := t["source"].(string)
		if account == selfID || id == "" {
			return
		}
		mu.Lock()
		tracks[id] = track{id, src, account}
		mu.Unlock()
	}
	if call, _ := ok["call"].(map[string]any); call != nil {
		for _, p := range list(call["participants"]) {
			for _, tr := range list(p["tracks"]) {
				add(fmt.Sprint(p["account_id"]), tr)
			}
		}
	}
	subscribe := func() {
		mu.Lock()
		var video [][3]any
		for _, tr := range tracks {
			if tr.source != "microphone" {
				video = append(video, [3]any{tr.id, 1280, 720})
			}
		}
		mu.Unlock()
		if len(video) > 0 {
			fmt.Printf("SUBSCRIBED %v\n", c.Subscribe(video...))
		}
	}
	subscribe()

	// Sample what arrives: calltest keeps the newest payload of each track.
	seen := map[string][][]byte{}
	var found atomic.Value
	end := time.Now().Add(*dur)
	tick := time.NewTicker(15 * time.Millisecond)
	defer tick.Stop()
	for time.Now().Before(end) {
		<-tick.C
		if f := c.Find("call.track_published", nil, 0); f != nil {
			add(fmt.Sprint(f["account_id"]), f["track"].(map[string]any))
			subscribe()
		}
		mu.Lock()
		for id := range tracks {
			p := c.Stream(id).LastPayload()
			if len(p) == 0 {
				continue
			}
			s := seen[id]
			if len(s) == 0 || !equal(s[len(s)-1], p) {
				if len(s) < 400 {
					seen[id] = append(s, p)
				}
			}
		}
		mu.Unlock()
		// Once there are audio frames from the other side, find the
		// derivation that opens them, and send under the same one.
		if *keyHex != "" && found.Load() == nil {
			for id, tr := range tracks {
				if tr.source == "microphone" && len(seen[id]) >= 5 {
					if how := analyse(seen[id], other, []byte(*salt)); how != "" {
						found.Store(how)
						fmt.Printf("DECRYPTED the other side's audio: %s\n", how)
						if *kdf == "" {
							enc.set(deriveKey(strings.Fields(how)[0], own, []byte(*salt)))
						}
					}
				}
			}
		}
	}

	fmt.Printf("SENT tracks %v\n", c.TrackIDs)
	ids := make([]string, 0, len(tracks))
	for id := range tracks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		tr := tracks[id]
		s := c.Stream(id)
		fmt.Printf("RECEIVED %s %s from %s: %d packets, %d bytes, layers %v\n",
			id, tr.source, tr.account, s.Packets.Load(), s.Bytes.Load(), layers(s.Layers()))
	}
	if *keyHex != "" {
		for _, id := range ids {
			tr := tracks[id]
			if tr.source == "microphone" {
				report(seen[id], other, []byte(*salt), "audio")
			} else {
				reportVideo(seen[id], other, []byte(*salt))
			}
		}
	}
	if *dump != "" {
		f, err := os.Create(*dump)
		check(err)
		for _, id := range ids {
			for _, p := range seen[id] {
				fmt.Fprintf(f, "%s %s %s\n", id, tracks[id].source, hex.EncodeToString(p))
			}
		}
		f.Close()
	}
	c.Leave()
}

// ---- frame encryption ----

// encrypter encrypts the made-up microphone frames calltest sends, in the
// format docs/CALL-FRAMES.md describes. calltest hands over the frame
// after its first byte, which stays readable, and the frame's length
// cannot change; so the plaintext is what is left after the tag and
// trailer.
type encrypter struct {
	mu    sync.Mutex
	aead  cipher.AEAD
	index byte
	n     uint32
}

func (e *encrypter) set(key []byte) {
	block, err := aes.NewCipher(key)
	check(err)
	aead, err := cipher.NewGCM(block)
	check(err)
	e.mu.Lock()
	e.aead = aead
	e.mu.Unlock()
}

func (e *encrypter) encrypt(rest []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.aead == nil || len(rest) < 16+12+2+1 {
		return // sent in the clear until the key is known
	}
	e.n++
	iv := make([]byte, 12)
	binary.BigEndian.PutUint32(iv[0:], 0x0c4a11ee)
	binary.BigEndian.PutUint32(iv[4:], uint32(time.Now().UnixNano()))
	binary.BigEndian.PutUint32(iv[8:], e.n)
	plain := len(rest) - 16 - 12 - 2
	// The readable first byte (the audio mark 'a') is the associated data.
	ct := e.aead.Seal(nil, iv, rest[:plain], []byte{'a'})
	copy(rest, ct)
	copy(rest[len(ct):], iv)
	rest[len(rest)-2] = 12
	rest[len(rest)-1] = e.index
}

// deriveKey makes the AES key the way the plugin's key provider does,
// under one of the candidate readings.
func deriveKey(how string, raw, salt []byte) []byte {
	switch how {
	case "raw":
		return raw
	case "hkdf":
		// What the plugin does (docs/CALL-FRAMES.md): HKDF-SHA-256 with the
		// key provider's salt, 128 zero bytes of info, 16 bytes out.
		k, err := hkdf.Key(sha256.New, raw, salt, string(make([]byte, 128)), 16)
		check(err)
		return k
	case "hkdf256":
		k, err := hkdf.Key(sha256.New, raw, salt, "", 32)
		check(err)
		return k
	case "pbkdf2":
		k, err := pbkdf2.Key(sha256.New, string(raw), salt, 100000, 16)
		check(err)
		return k
	case "pbkdf2-256":
		k, err := pbkdf2.Key(sha256.New, string(raw), salt, 100000, 32)
		check(err)
		return k
	}
	fail("unknown key derivation %q", how)
	return nil
}

var derivations = []string{"raw", "hkdf", "hkdf256", "pbkdf2", "pbkdf2-256"}

// open tries to decrypt one frame: [readable][ciphertext+tag][iv][iv len][key index].
func open(frame, key []byte, readable int) ([]byte, bool) {
	if len(frame) < readable+16+2 {
		return nil, false
	}
	ivLen := int(frame[len(frame)-2])
	if ivLen != 12 || len(frame) < readable+16+2+ivLen {
		return nil, false
	}
	iv := frame[len(frame)-2-ivLen : len(frame)-2]
	ct := frame[readable : len(frame)-2-ivLen]
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, false
	}
	aead, _ := cipher.NewGCM(block)
	p, err := aead.Open(nil, iv, ct, frame[:readable])
	return p, err == nil
}

// analyse finds the derivation and number of readable bytes that open
// most of these audio frames.
func analyse(frames [][]byte, raw, salt []byte) string {
	for _, how := range derivations {
		key := deriveKey(how, raw, salt)
		for readable := 0; readable <= 2; readable++ {
			n := 0
			for _, f := range frames {
				if _, ok := open(f, key, readable); ok {
					n++
				}
			}
			if n*2 > len(frames) {
				return fmt.Sprintf("%s readable=%d (%d of %d frames)", how, readable, n, len(frames))
			}
		}
	}
	return ""
}

func report(frames [][]byte, raw, salt []byte, what string) {
	fmt.Printf("FRAMES %s: %d sampled\n", what, len(frames))
	if len(frames) == 0 {
		return
	}
	how := analyse(frames, raw, salt)
	fmt.Printf("  derivation: %q\n", how)
	for i, f := range frames {
		if i >= 6 {
			break
		}
		n := len(f)
		if n < 15 {
			fmt.Printf("  frame %d: %x\n", i, f)
			continue
		}
		fmt.Printf("  frame %d: len %d head %x iv %x ivlen %d keyindex %d\n", i, n, f[:min(4, n)], f[n-14:n-2], f[n-2], f[n-1])
	}
}

// reportVideo looks at VP8 packets that hold a whole frame: it strips the
// payload descriptor (RFC 7741) and tries each number of readable bytes.
func reportVideo(payloads [][]byte, raw, salt []byte) {
	fmt.Printf("FRAMES video: %d packets sampled\n", len(payloads))
	keys := map[string][]byte{}
	for _, how := range derivations {
		keys[how] = deriveKey(how, raw, salt)
	}
	results := map[string]int{}
	shown := 0
	for _, p := range payloads {
		frame, key, ok := vp8Frame(p)
		if !ok {
			continue
		}
		for how, k := range keys {
			for readable := 0; readable <= 12; readable++ {
				if _, ok := open(frame, k, readable); ok {
					results[fmt.Sprintf("%s readable=%d keyframe=%v", how, readable, key)]++
				}
			}
		}
		if shown < 4 {
			n := len(frame)
			fmt.Printf("  frame: len %d keyframe %v head %x trailer iv %x ivlen %d keyindex %d\n", n, key, frame[:min(12, n)], frame[max(0, n-14):max(0, n-2)], frame[n-2], frame[n-1])
			shown++
		}
	}
	for k, v := range results {
		fmt.Printf("  opened %d with %s\n", v, k)
	}
}

// vp8Frame returns the VP8 frame in a packet that starts a frame, and
// whether it is a key frame.
func vp8Frame(p []byte) ([]byte, bool, bool) {
	if len(p) < 2 || p[0]&0x10 == 0 || p[0]&0x07 != 0 {
		return nil, false, false // not the start of partition 0
	}
	i := 1
	if p[0]&0x80 != 0 { // X
		x := p[1]
		i = 2
		if x&0x80 != 0 { // I
			if p[i]&0x80 != 0 {
				i += 2
			} else {
				i++
			}
		}
		if x&0x40 != 0 { // L
			i++
		}
		if x&0x30 != 0 { // T or K
			i++
		}
	}
	if i >= len(p) {
		return nil, false, false
	}
	f := p[i:]
	return f, f[0]&0x01 == 0, true
}

// ---- the live connection ----

type wsSig struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func dial(origin, token string) *wsSig {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(origin, "http")+"/api/v1/events",
		&websocket.DialOptions{HTTPHeader: h, Subprotocols: []string{"churr.v1"}})
	check(err)
	conn.SetReadLimit(4 << 20)
	s := &wsSig{conn: conn}
	if f, ok := s.Next(5 * time.Second); !ok || f["type"] != "hello" {
		fail("expected hello, got %v", f)
	}
	s.Send(map[string]any{"type": "resume", "positions": map[string]any{}})
	for {
		f, ok := s.Next(10 * time.Second)
		if !ok {
			fail("no ready frame")
		}
		if f["type"] == "ready" {
			break
		}
	}
	go func() {
		for range time.Tick(15 * time.Second) {
			s.Send(map[string]any{"type": "ping"})
		}
	}()
	return s
}

func (s *wsSig) Send(v any) {
	b, _ := json.Marshal(v)
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.conn.Write(ctx, websocket.MessageText, b); err != nil {
		fmt.Fprintln(os.Stderr, "send:", err)
	}
}

func (s *wsSig) Next(wait time.Duration) (map[string]any, bool) {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), wait)
		_, data, err := s.conn.Read(ctx)
		cancel()
		if err != nil {
			return nil, false
		}
		var f map[string]any
		if json.Unmarshal(data, &f) != nil {
			return nil, false
		}
		if f["type"] == "pong" {
			continue
		}
		return f, true
	}
}

// ---- small things ----

func api(origin, token, method, path string, out any) {
	check(tryAPI(origin, token, method, path, out))
}

func tryAPI(origin, token, method, path string, out any) error {
	req, _ := http.NewRequest(method, origin+path, strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var e any
		json.NewDecoder(res.Body).Decode(&e)
		return fmt.Errorf("%s %s: %d %v", method, path, res.StatusCode, e)
	}
	if out != nil {
		json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

func list(v any) []map[string]any {
	var out []map[string]any
	if l, ok := v.([]any); ok {
		for _, e := range l {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func layers(m map[byte]int) string {
	var parts []string
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%c:%d", k, v))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

func equal(a, b []byte) bool { return string(a) == string(b) }

type logT struct{}

func (logT) Helper()                         {}
func (logT) Logf(format string, args ...any) { fmt.Printf(format+"\n", args...) }
func (logT) Fatalf(format string, args ...any) {
	fail(format, args...)
}

func check(err error) {
	if err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "callpeer: "+format+"\n", args...)
	os.Exit(1)
}
