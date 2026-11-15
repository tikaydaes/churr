package wire

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/tikaydaes/churr/internal/secret"
)

// The link's own frames (socket.go) at every length a WebSocket header
// writes differently, deflated and not, from both sides at once, while the
// library answers on the same sockets: each arrives whole.
func TestOwnFramesOfEverySizeBothWays(t *testing.T) {
	a, b := ident(t, "a.example", KindServer), ident(t, "b.example", KindServer)
	got := make(chan *Conn, 1)
	url := listen(t, a, func(Frame, []byte, []byte) error { return nil }, got)
	dialler, err := Dial(context.Background(), DialOptions{URL: url, Identity: b})
	if err != nil {
		t.Fatal(err)
	}
	listener := <-got
	defer dialler.Close()
	defer listener.Close()

	var frames []Frame
	for _, n := range []int{0, 1, 100, 125, 126, 127, 4000, 65535, 65536, 70000, 300000} {
		frames = append(frames,
			Frame{Frame: FDirect, From: "a.example", To: "b.example", Body: secret.RandomBytes(n)},
			Frame{Frame: FDirect, From: "a.example", To: "b.example", Body: []byte(strings.Repeat("a", n))})
	}
	var wg sync.WaitGroup
	for _, side := range [][2]*Conn{{dialler, listener}, {listener, dialler}} {
		from, to := side[0], side[1]
		wg.Add(2)
		go func() {
			defer wg.Done()
			for _, f := range frames {
				if err := from.Send(f); err != nil {
					t.Errorf("sending %d bytes: %v", len(f.Body), err)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for _, f := range frames {
				g, _, err := to.Read(context.Background())
				if err != nil || g.Frame != FDirect || !bytes.Equal(g.Body, f.Body) {
					t.Errorf("a frame with a body of %d bytes did not arrive whole (dialler sent: %v): %v", len(f.Body), from.Dialler, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestOneFrame(t *testing.T) {
	long := append([]byte{0x82, 126, 0x01, 0x00}, make([]byte, 256)...)
	masked := append([]byte{0x8a, 0x80 | 2, 1, 2, 3, 4}, 9, 9)
	for name, c := range map[string]struct {
		p    []byte
		want bool
	}{
		"a pong":             {[]byte{0x8a, 0}, true},
		"a close":            {[]byte{0x88, 2, 3, 232}, true},
		"a masked pong":      {masked, true},
		"a longer frame":     {long, true},
		"half a frame":       {long[:100], false},
		"a frame and a bit":  {append(append([]byte{}, masked...), 0x8a), false},
		"not a final frame":  {[]byte{0x02, 0}, false},
		"a masked half":      {masked[:5], false},
		"nothing":            {nil, false},
		"a header by itself": {[]byte{0x82, 126, 0, 200}, false},
	} {
		if got := oneFrame(c.p); got != c.want {
			t.Errorf("%s: oneFrame = %v, want %v", name, got, c.want)
		}
	}
}
