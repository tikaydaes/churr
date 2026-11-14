package testkit

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/hub"
	"github.com/tikaydaes/churr/internal/model"
)

// stopWait is how long a hub is given to stop. Stopping closes each
// connection with a last frame, which may take a moment each.
const stopWait = 60 * time.Second

// TestHub is a churr-hub running in the test process.
type TestHub struct {
	t testing.TB
	// Hub is the hub itself, for the operator's commands.
	Hub *hub.Hub
	// Name is the hub's name, hub.localhost; URL is where it listens.
	Name, URL string
	// Dir is its data directory: hub.db and keys/.
	Dir string

	skew atomic.Int64
	port int
	stop func()
}

// Skew moves the hub's clock forward, for what happens after thirty days.
func (h *TestHub) Skew(d time.Duration) { h.skew.Add(int64(d / time.Millisecond)) }

// Invite makes an invitation to the hub, for one server name or, with "",
// for any.
func (h *TestHub) Invite(forName string) string {
	h.t.Helper()
	text, err := h.Hub.Invite(forName, 0)
	if err != nil {
		h.t.Fatal(err)
	}
	return text
}

// NewHub starts a hub on loopback. It stops when the test ends.
func NewHub(t testing.TB) *TestHub { return NewHubNamed(t, "hub.localhost") }

// NewHubNamed is NewHub for a hub with a name of the test's choosing, which
// a cluster of several hubs needs. The name must end in .localhost.
func NewHubNamed(t testing.TB, name string) *TestHub {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	th := &TestHub{t: t, Name: name, Dir: t.TempDir(), port: ln.Addr().(*net.TCPAddr).Port}
	th.URL = fmt.Sprintf("http://127.0.0.1:%d", th.port)
	th.start(ln)
	t.Cleanup(th.Stop)
	return th
}

// hubPorts is where each test hub listens, by its name.
var hubPorts sync.Map

// hubClient is what a test hub dials another with. A hub's own invitation
// names it as wss://NAME/link/v1, which is what the churr-hub program prints
// when it is run on a test hub's data; no test hub serves TLS or owns port
// 443, so a dial of that address is carried, in the clear, to wherever the
// hub of that name listens in this process.
func hubClient() *http.Client {
	c := LoopbackClient()
	c.Transport.(*http.Transport).DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		port, ok := hubPorts.Load(host)
		if !ok {
			return nil, fmt.Errorf("testkit: there is no test hub named %s", host)
		}
		var d net.Dialer
		return d.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(port.(int))))
	}
	return c
}

// start runs the hub on its data directory and its port.
func (h *TestHub) start(ln net.Listener) {
	h.t.Helper()
	cfg := config.Hub{Common: config.Common{Data: h.Dir, Hostname: h.Name, LogLevel: "error"}, KeepDays: 30}
	url := fmt.Sprintf("ws://%s:%d/link/v1", h.Name, h.port)
	hubPorts.Store(h.Name, h.port)
	hb, err := hub.NewWith(cfg, hub.Options{Listener: ln, URL: url, HTTPClient: hubClient(),
		Now: func() model.Time { return model.Now() + model.Time(h.skew.Load()) }})
	if err != nil {
		h.t.Fatal(err)
	}
	h.Hub = hb
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		hb.Run(ctx)
		close(done)
	}()
	h.stop = func() {
		cancel()
		// A hub that does not stop is a hub that is stuck: a lock or its one
		// database connection held by something that will never let go. The
		// test is failed with every goroutine's stack, at once, rather than
		// left to run into the test binary's own time limit with nothing to
		// show for it.
		stuck := func(what string) {
			buf := make([]byte, 4<<20)
			h.t.Errorf("testkit: the hub %s did not %s within %v; every goroutine:\n%s", h.Name, what, stopWait, buf[:runtime.Stack(buf, true)])
		}
		select {
		case <-done:
		case <-time.After(stopWait):
			stuck("stop")
			return
		}
		// Closing the database waits for whatever still holds its one
		// connection. A transaction that waits for a lock it will never be
		// given holds it for good, and this is where a test's cleanup then
		// stood until the test binary's own time limit.
		closed := make(chan struct{})
		go func() {
			hb.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(stopWait):
			stuck("close its database")
		}
	}
}

// Stop stops the hub, as a crash or a shutdown does. Its data stays. Stop on
// a stopped hub does nothing.
func (h *TestHub) Stop() {
	if h.stop != nil {
		h.stop()
		h.stop = nil
	}
}

// Offline runs an operator's command on a stopped hub's data, as churr-hub
// does when the hub is not serving: the data is opened, fn runs, and it is
// closed again.
func (h *TestHub) Offline(fn func(hb *hub.Hub) error) error {
	h.t.Helper()
	if h.stop != nil {
		h.t.Fatal("testkit: Offline is for a hub that has been stopped")
	}
	cfg := config.Hub{Common: config.Common{Data: h.Dir, Hostname: h.Name, LogLevel: "error"}, KeepDays: 30}
	hb, err := hub.NewWith(cfg, hub.Options{URL: fmt.Sprintf("ws://%s:%d/link/v1", h.Name, h.port),
		Now: func() model.Time { return model.Now() + model.Time(h.skew.Load()) }})
	if err != nil {
		h.t.Fatal(err)
	}
	defer hb.Close()
	return fn(hb)
}

// Start starts a stopped hub again on the same data and the same address.
// Hub is a new value afterwards.
func (h *TestHub) Start() {
	h.t.Helper()
	if h.stop != nil {
		return
	}
	var ln net.Listener
	var err error
	for try := 0; try < 100; try++ {
		if ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", h.port)); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		h.t.Fatal(err)
	}
	h.start(ln)
}

// Restart stops the hub and starts it again: what it knew in memory is
// gone, and what it wrote down is all it has.
func (h *TestHub) Restart() {
	h.t.Helper()
	h.Stop()
	h.Start()
}
