package main

import (
	"bufio"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// A WebSocket client with only what the load test needs: text frames in
// both directions, pings answered, no compression, no extensions. It is
// written here, in a few lines of the standard library, so that the module's
// one WebSocket dependency stays with the packages that speak the protocol
// for the product, and so that five thousand connections cost this program
// as little as they can.

type wsConn struct {
	conn net.Conn
	r    *bufio.Reader
	wmu  sync.Mutex
}

// dialWS opens a WebSocket to a ws:// or wss:// address with a bearer token
// and a subprotocol.
func dialWS(address, bearer, subprotocol string, timeout time.Duration) (*wsConn, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if u.Port() == "" {
		if u.Scheme == "wss" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	d := net.Dialer{Timeout: timeout}
	var conn net.Conn
	if u.Scheme == "wss" {
		conn, err = tls.DialWithDialer(&d, "tcp", host, &tls.Config{ServerName: u.Hostname(), NextProtos: []string{"http/1.1"}})
	} else {
		conn, err = d.Dial("tcp", host)
	}
	if err != nil {
		return nil, err
	}
	var nonce [16]byte
	rand.Read(nonce[:])
	req := "GET " + u.RequestURI() + " HTTP/1.1\r\nHost: " + u.Host + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + base64.StdEncoding.EncodeToString(nonce[:]) + "\r\n" +
		"Sec-WebSocket-Protocol: " + subprotocol + "\r\nAuthorization: Bearer " + bearer + "\r\n\r\n"
	conn.SetDeadline(time.Now().Add(timeout))
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		return nil, err
	}
	r := bufio.NewReaderSize(conn, 4096)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		conn.Close()
		return nil, fmt.Errorf("the server answered %d: %s", resp.StatusCode, body)
	}
	conn.SetDeadline(time.Time{})
	return &wsConn{conn: conn, r: r}, nil
}

// write sends one text frame. A client masks what it sends.
func (c *wsConn) write(opcode byte, payload []byte) error {
	var head [14]byte
	head[0] = 0x80 | opcode
	n := 2
	switch {
	case len(payload) < 126:
		head[1] = 0x80 | byte(len(payload))
	case len(payload) < 1<<16:
		head[1] = 0x80 | 126
		binary.BigEndian.PutUint16(head[2:], uint16(len(payload)))
		n = 4
	default:
		head[1] = 0x80 | 127
		binary.BigEndian.PutUint64(head[2:], uint64(len(payload)))
		n = 10
	}
	var mask [4]byte
	rand.Read(mask[:])
	copy(head[n:], mask[:])
	buf := make([]byte, 0, n+4+len(payload))
	buf = append(buf, head[:n+4]...)
	for i, b := range payload {
		buf = append(buf, b^mask[i%4])
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_, err := c.conn.Write(buf)
	return err
}

func (c *wsConn) writeText(payload []byte) error { return c.write(1, payload) }

var errClosed = errors.New("the server closed the connection")

// read returns the next text message, answering pings on the way.
func (c *wsConn) read() ([]byte, error) {
	var message []byte
	for {
		var head [2]byte
		if _, err := io.ReadFull(c.r, head[:]); err != nil {
			return nil, err
		}
		fin, opcode := head[0]&0x80 != 0, head[0]&0x0f
		length := uint64(head[1] & 0x7f)
		switch length {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(c.r, b[:]); err != nil {
				return nil, err
			}
			length = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(c.r, b[:]); err != nil {
				return nil, err
			}
			length = binary.BigEndian.Uint64(b[:])
		}
		if length > 8<<20 {
			return nil, errors.New("a frame larger than this client reads")
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(c.r, payload); err != nil {
			return nil, err
		}
		switch opcode {
		case 8:
			// The close frame says why: a code and a few words.
			if len(payload) >= 2 {
				return nil, fmt.Errorf("%w: %d %s", errClosed, binary.BigEndian.Uint16(payload), payload[2:])
			}
			return nil, errClosed
		case 9:
			c.write(10, payload)
		case 10:
		case 0, 1, 2:
			message = append(message, payload...)
			if fin {
				return message, nil
			}
		}
	}
}

func (c *wsConn) close() { c.conn.Close() }
