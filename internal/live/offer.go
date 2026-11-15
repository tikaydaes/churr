package live

import (
	"crypto/tls"
	"net"
	"sync"
	"sync/atomic"
)

// Offering a write to a connection whose TLS ends in this process.
//
// The flushers (flush.go) write for every connection whose socket can be
// offered a write that never waits. A TLS connection's socket is under the
// TLS layer, out of reach, so such connections were each written by a
// goroutine of their own, started for every frame: on a server that ends
// TLS itself, which is how Churr runs by default, a message to five
// thousand people was five thousand goroutines made, scheduled and ended,
// one frame and one write each. Measured on one processor at fifty posts a
// second that was most of the processor, and every request waited 25 ms
// behind them each time it came back from the database.
//
// So the listener hands out connections that can be offered a write
// (Offerable), with TLS above them as before. Offered a write, such a
// connection gives the socket what it takes at once and keeps the rest:
// the TLS layer has already made a record of it, and a record must go out
// whole and once. TLS is told all of it went. The connection then has a
// backlog, the flusher sees that and hands it to a goroutine of its own to
// finish, exactly as it does for a plain socket that took part of a write.
// Any write not made as an offer waits for the socket as it always did,
// after sending the backlog, so bytes leave in the order TLS made them.

// Offerable returns a listener whose connections the hub's flushers can
// write to without waiting, when TLS is ended over them in this process.
// Everything else about them is unchanged. Connections the system cannot
// offer a write to are handed out as they are.
func Offerable(l net.Listener) net.Listener { return offerListener{l} }

type offerListener struct{ net.Listener }

func (l offerListener) Accept() (net.Conn, error) {
	nc, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if try := tryWriter(nc); try != nil {
		return &offerConn{Conn: nc, try: try}, nil
	}
	return nc, nil
}

// offerConn is a socket under TLS that can be offered a write.
type offerConn struct {
	net.Conn
	try func([]byte) (int, error)
	// offering is set while the hub offers a write through the TLS layer
	// above (tlsOffer): writes then never wait.
	offering atomic.Bool

	// mu is held for each write, so the backlog and what follows it
	// leave in order whoever writes.
	mu      sync.Mutex
	backlog []byte // what the socket did not take of offered writes
}

func (c *offerConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.offering.Load() {
		if len(c.backlog) > 0 {
			// Already behind: this goes after what waits.
			c.backlog = append(c.backlog, p...)
			return len(p), nil
		}
		n, err := c.try(p)
		if err != nil {
			return n, err
		}
		if n < len(p) {
			c.backlog = append(c.backlog, p[n:]...)
		}
		return len(p), nil
	}
	if err := c.settleLocked(); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}

// settle sends the backlog, waiting for the socket as long as it takes.
func (c *offerConn) settle() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.settleLocked()
}

func (c *offerConn) settleLocked() error {
	if len(c.backlog) == 0 {
		return nil
	}
	_, err := c.Conn.Write(c.backlog)
	c.backlog = nil // not kept: a backlog is rare, and an idle connection holds nothing
	return err
}

func (c *offerConn) behind() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.backlog) > 0
}

// tlsOffer returns, for a TLS connection over a socket from Offerable, the
// write that never waits, whether the socket is behind after one, and the
// write that sends what it is behind by. For any other connection it
// returns nothing.
func tlsOffer(nc net.Conn) (try func([]byte) (int, error), behind func() bool, settle func() error) {
	tc, ok := nc.(*tls.Conn)
	if !ok {
		return nil, nil, nil
	}
	oc, ok := tc.NetConn().(*offerConn)
	if !ok {
		return nil, nil, nil
	}
	try = func(p []byte) (int, error) {
		// One record for up to 16 KB, so a batch of frames is one
		// record and one write to the socket.
		oc.offering.Store(true)
		n, err := tc.Write(p)
		oc.offering.Store(false)
		return n, err
	}
	return try, oc.behind, oc.settle
}
