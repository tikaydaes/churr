package botkit

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// A WebSocket client in as little as the examples need (RFC 6455): text
// frames in both directions, ping answered with pong, and close. A real
// bot would use a library; this keeps the examples free of dependencies.

type wsConn struct {
	conn net.Conn
	br   *bufio.Reader
	wmu  sync.Mutex
	once sync.Once
}

// dial opens the live connection: an HTTP upgrade that asks for the
// subprotocol churr.v1 and carries the bot's token.
func dial(ctx context.Context, address, token string) (*wsConn, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if u.Port() == "" {
		if u.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	if u.Scheme == "https" {
		conn, err = (&tls.Dialer{NetDialer: &d, Config: &tls.Config{ServerName: u.Hostname()}}).DialContext(ctx, "tcp", host)
	} else {
		conn, err = d.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 16)
	rand.Read(nonce)
	key := base64.StdEncoding.EncodeToString(nonce)
	req, _ := http.NewRequest("GET", address, nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", key)
	req.Header.Set("Sec-WebSocket-Protocol", "churr.v1")
	req.Header.Set("Authorization", "Bearer "+token)
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		conn.Close()
		return nil, fmt.Errorf("the server refused the live connection: %s %s", resp.Status, body)
	}
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		conn.Close()
		return nil, errors.New("the server's answer to the upgrade is not a WebSocket's")
	}
	conn.SetDeadline(time.Time{})
	return &wsConn{conn: conn, br: br}, nil
}

func (w *wsConn) close() { w.once.Do(func() { w.conn.Close() }) }

// write sends one frame. A client masks everything it sends.
func (w *wsConn) write(opcode byte, payload []byte) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	head := []byte{0x80 | opcode}
	switch n := len(payload); {
	case n < 126:
		head = append(head, 0x80|byte(n))
	case n < 1<<16:
		head = append(head, 0x80|126, byte(n>>8), byte(n))
	default:
		head = append(head, 0x80|127)
		head = binary.BigEndian.AppendUint64(head, uint64(n))
	}
	mask := make([]byte, 4)
	rand.Read(mask)
	head = append(head, mask...)
	masked := make([]byte, len(payload))
	for i, c := range payload {
		masked[i] = c ^ mask[i%4]
	}
	w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := w.conn.Write(append(head, masked...))
	return err
}

func (w *wsConn) writeJSON(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.write(0x1, raw)
}

// maxMessage bounds what one message from the server may be.
const maxMessage = 4 << 20

// read returns the next text message, answering pings on the way. A close
// frame ends the connection with its code in the error.
func (w *wsConn) read() ([]byte, error) {
	var message []byte
	for {
		var head [2]byte
		if _, err := io.ReadFull(w.br, head[:]); err != nil {
			return nil, err
		}
		fin, opcode := head[0]&0x80 != 0, head[0]&0x0f
		n := uint64(head[1] & 0x7f)
		switch n {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(w.br, ext[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(w.br, ext[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(ext[:])
		}
		if head[1]&0x80 != 0 || n > maxMessage || uint64(len(message))+n > maxMessage {
			return nil, errors.New("a frame the server should not have sent")
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(w.br, payload); err != nil {
			return nil, err
		}
		switch opcode {
		case 0x8: // close
			code := 1005
			if len(payload) >= 2 {
				code = int(binary.BigEndian.Uint16(payload))
			}
			w.write(0x8, payload[:min(len(payload), 2)])
			return nil, fmt.Errorf("the server closed the connection with code %d", code)
		case 0x9: // ping
			if err := w.write(0xA, payload); err != nil {
				return nil, err
			}
		case 0xA: // pong
		case 0x0, 0x1, 0x2:
			message = append(message, payload...)
			if fin {
				return message, nil
			}
		}
	}
}
