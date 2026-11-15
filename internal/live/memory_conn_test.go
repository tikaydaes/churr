package live

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// What one connection costs.
//
// On a machine of 1 GB the server's memory goes on its connections, so what
// each holds decides how many people it carries. This test connects a
// crowd to a hub the way the real server is reached (net/http, the upgrade,
// compression asked for, `resume` answered) and holds what each connection
// keeps, once it has settled and with nothing being sent, against a bound,
// so that it cannot creep back up unnoticed.
//
// The clients run in another process (this test binary, started again as
// TestMemoryClients), so that only the server's side is counted. They come
// in two halves, and what is reported is what the second half added: what
// the hub and the test hold whatever the number of connections (the
// flushers, the goroutines working resumes, the accounts) came with the
// first.
//
// CHURR_LIVE_MEMTEST_CONNS sets how many connect (300 unless set), and
// CHURR_LIVE_MEMTEST_PROFILE names a file prefix for heap profiles
// (-plain.heap and -tls.heap are added).

// The bounds, in bytes for each connection, heap and goroutine stacks
// together. What was measured when they were set is in
// docs/OPERATIONS.md section 16; they leave about a third over it.
const (
	maxBytesPerConnection    = 20 << 10
	maxBytesPerConnectionTLS = 28 << 10
)

func TestMemoryPerConnection(t *testing.T) {
	n := 300
	if v, err := strconv.Atoi(os.Getenv("CHURR_LIVE_MEMTEST_CONNS")); err == nil && v > 0 {
		n = v
	}
	if testing.Short() {
		t.Skip("connects a crowd")
	}
	for _, mode := range []struct {
		name  string
		tls   bool
		bound int
	}{{"plain", false, maxBytesPerConnection}, {"tls", true, maxBytesPerConnectionTLS}} {
		t.Run(mode.name, func(t *testing.T) {
			got := measureConnections(t, n, mode.tls, mode.name)
			if raceDetector {
				// Still worth running for what the detector sees; the
				// sizes under it are not the real ones.
				return
			}
			if got > mode.bound {
				t.Errorf("%d bytes for each connection, over the bound of %d: something has started to keep more for every connection", got, mode.bound)
			}
		})
	}
}

// held is the memory in use that connections can account for: live heap
// and goroutine stacks, after a collection.
func held() (heap, stacks uint64, goroutines int) {
	// Twice, so what the first freed through finalizers is gone too;
	// stacks shrink a step at each collection, hence a few more.
	for range 4 {
		runtime.GC()
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc, m.StackInuse, runtime.NumGoroutine()
}

func measureConnections(t *testing.T, n int, withTLS bool, name string) int {
	// Long times, so nothing is closed or sent while it is measured.
	r := newRoom(t, n, Options{Heartbeat: 10 * time.Minute, ResumeWithin: time.Minute, PresenceEvery: time.Hour})
	h := r.hub
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		i, _ := strconv.Atoi(req.URL.Query().Get("a"))
		acc := model.Account{ID: r.ids[i], Username: fmt.Sprintf("p%04d", i), DisplayName: "P", Role: model.RoleMember}
		h.Serve(w, req, model.Session{ID: model.SessionID(fmt.Sprintf("s%d", i)), AccountID: r.ids[i]}, acc)
	})
	srv := httptest.NewUnstartedServer(handler)
	if withTLS {
		srv.StartTLS()
	} else {
		srv.Start()
	}
	t.Cleanup(srv.Close)

	cmd := exec.Command(os.Args[0], "-test.run=^TestMemoryClients$", "-test.count=1")
	cmd.Env = append(os.Environ(), "CHURR_LIVE_MEMTEST_URL="+srv.URL, "CHURR_LIVE_MEMTEST_N="+strconv.Itoa(n), "CHURR_LIVE_MEMTEST_CONV="+string(r.conv))
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		cmd.Wait()
	})
	lines := bufio.NewScanner(stdout)
	// settled waits for the clients to say they have connected, and for
	// that many connections to have caught up and gone quiet.
	settled := func(want int) {
		connected := false
		for lines.Scan() {
			if lines.Text() == "connected" {
				connected = true
				break
			}
		}
		if !connected {
			t.Fatal("the clients did not all connect")
		}
		eventually(t, "every connection has caught up", func() bool {
			if h.Connected() != want {
				return false
			}
			h.mu.RLock()
			defer h.mu.RUnlock()
			for _, set := range h.conns {
				for c := range set {
					c.mu.Lock()
					busy := c.replaying || c.scheduled || len(c.queue) > 0 || len(c.pending) > 0
					c.mu.Unlock()
					if busy {
						return false
					}
				}
			}
			return true
		})
		// Presence of the crowd arriving has gone out by now or goes
		// out in the next moment; then nothing moves.
		time.Sleep(2 * time.Second)
	}
	settled(n / 2)
	heap0, stacks0, g0 := held()
	fmt.Fprintln(stdin, "more")
	settled(n)
	heap1, stacks1, g1 := held()
	n -= n / 2 // the connections the difference is of
	if prefix := os.Getenv("CHURR_LIVE_MEMTEST_PROFILE"); prefix != "" {
		f, err := os.Create(prefix + "-" + name + ".heap")
		if err != nil {
			t.Fatal(err)
		}
		pprof.Lookup("heap").WriteTo(f, 0)
		f.Close()
		g, _ := os.Create(prefix + "-" + name + ".goroutines")
		pprof.Lookup("goroutine").WriteTo(g, 1)
		g.Close()
	}
	heap := (int64(heap1) - int64(heap0)) / int64(n)
	stacks := (int64(stacks1) - int64(stacks0)) / int64(n)
	t.Logf("%s, the second half of %d connections: %d bytes each (heap %d, goroutine stacks %d), %.2f goroutines each",
		name, 2*n, heap+stacks, heap, stacks, float64(g1-g0)/float64(n))
	return int(heap + stacks)
}

// TestMemoryClients is the other process of TestMemoryPerConnection: it
// connects the crowd, says so, and holds the connections until its input
// closes. It keeps as little as it can for each, and reads nothing after
// `hello`: what little an idle connection is sent waits in the kernel.
func TestMemoryClients(t *testing.T) {
	base := os.Getenv("CHURR_LIVE_MEMTEST_URL")
	if base == "" {
		t.Skip("only run by TestMemoryPerConnection")
	}
	n, _ := strconv.Atoi(os.Getenv("CHURR_LIVE_MEMTEST_N"))
	conv := os.Getenv("CHURR_LIVE_MEMTEST_CONV")
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	conns := make([]net.Conn, 0, n)
	in := bufio.NewReader(os.Stdin)
	for i := range n {
		if i == n/2 {
			// The first half is in; the second waits to be asked for.
			fmt.Println("connected")
			if _, err := in.ReadString('\n'); err != nil {
				t.Fatal(err)
			}
		}
		var nc net.Conn
		for attempt := 0; ; attempt++ {
			if u.Scheme == "https" {
				nc, err = tls.Dial("tcp", u.Host, &tls.Config{InsecureSkipVerify: true})
			} else {
				nc, err = net.Dial("tcp", u.Host)
			}
			if err == nil || attempt == 5 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if err != nil {
			t.Fatal(err)
		}
		// What a browser sends, compression included.
		fmt.Fprintf(nc, "GET /?a=%d HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\n"+
			"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Protocol: %s\r\n"+
			"Sec-WebSocket-Extensions: permessage-deflate; client_max_window_bits\r\n\r\n", i, u.Host, Subprotocol)
		br := bufio.NewReaderSize(nc, 1024)
		status, err := br.ReadString('\n')
		if err != nil || !strings.Contains(status, "101") {
			t.Fatalf("upgrade %d: %q %v", i, status, err)
		}
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if line == "\r\n" {
				break
			}
		}
		// hello: one frame from the server, which is not masked.
		var head [2]byte
		if _, err := io.ReadFull(br, head[:]); err != nil {
			t.Fatal(err)
		}
		size := int(head[1] & 0x7f)
		if size == 126 {
			var ext [2]byte
			io.ReadFull(br, ext[:])
			size = int(ext[0])<<8 | int(ext[1])
		}
		if _, err := br.Discard(size); err != nil {
			t.Fatal(err)
		}
		// resume: one text frame, masked with a key of zeros, which
		// leaves the payload as it is.
		payload := fmt.Sprintf(`{"type":"resume","positions":{%q:0},"open":[]}`, conv)
		frame := append([]byte{0x81, 0x80 | byte(len(payload)), 0, 0, 0, 0}, payload...)
		if _, err := nc.Write(frame); err != nil {
			t.Fatal(err)
		}
		conns = append(conns, nc)
	}
	fmt.Println("connected")
	io.Copy(io.Discard, in)
	for _, nc := range conns {
		nc.Close()
	}
}
