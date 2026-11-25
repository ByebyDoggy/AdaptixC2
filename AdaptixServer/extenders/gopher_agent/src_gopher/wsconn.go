package main

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsConn adapts a WebSocket (wss://) connection to net.Conn so the gopher
// protocol can ride through Cloudflare without a local bridge on the agent side.
// Frame format: 0x01 + payload = data, 0x02 = keepalive.
type wsConn struct {
	ws        *websocket.Conn
	mu        sync.Mutex
	rbuf      []byte
	closeOnce sync.Once
	done      chan struct{}
}

func dialWebSocket(rawurl string) (net.Conn, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	d := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		// Force IPv4 so the callback source address (and CF-Connecting-IP)
		// stays IPv4 even on dual-stack hosts.
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp4", addr)
		},
	}
	ws, _, err := d.Dial(u.String(), nil)
	if err != nil {
		return nil, err
	}
	c := &wsConn{ws: ws, done: make(chan struct{})}
	go c.keepalive()
	return c, nil
}

func (c *wsConn) keepalive() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			c.mu.Lock()
			err := c.ws.WriteMessage(websocket.BinaryMessage, []byte{2})
			c.mu.Unlock()
			if err != nil {
				_ = c.Close()
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *wsConn) Read(p []byte) (int, error) {
	for len(c.rbuf) == 0 {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return 0, err
		}
		if len(data) == 0 {
			continue
		}
		switch data[0] {
		case 1:
			c.rbuf = data[1:]
		case 2:
			continue
		default:
			continue
		}
	}
	n := copy(p, c.rbuf)
	c.rbuf = c.rbuf[n:]
	return n, nil
}

func (c *wsConn) Write(p []byte) (int, error) {
	msg := make([]byte, 0, len(p)+1)
	msg = append(msg, 1)
	msg = append(msg, p...)
	c.mu.Lock()
	err := c.ws.WriteMessage(websocket.BinaryMessage, msg)
	c.mu.Unlock()
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *wsConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.ws.Close()
	})
	return nil
}

func (c *wsConn) LocalAddr() net.Addr  { return c.ws.LocalAddr() }
func (c *wsConn) RemoteAddr() net.Addr { return c.ws.RemoteAddr() }

func (c *wsConn) SetDeadline(t time.Time) error {
	_ = c.ws.SetReadDeadline(t)
	return c.ws.SetWriteDeadline(t)
}

func (c *wsConn) SetReadDeadline(t time.Time) error  { return c.ws.SetReadDeadline(t) }
func (c *wsConn) SetWriteDeadline(t time.Time) error { return c.ws.SetWriteDeadline(t) }
