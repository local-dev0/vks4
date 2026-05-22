// FreeSWITCH Event Socket Library (ESL) клиент.
// Реализован минимальный набор: connect/auth, события (events_plain),
// api команды (api/bgapi), uuid_bridge.
//
// В первой итерации — каркас, который умеет подключаться и логировать события.
// Реальный bridge SIP↔WebRTC реализуется в v0.2 (см. docs/ROADMAP.md).
package esl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type Client struct {
	addr     string
	password string
	conn     net.Conn
	reader   *textproto.Reader
	mu       sync.Mutex
	log      *zap.Logger
}

func Dial(ctx context.Context, host string, port int, password string, log *zap.Logger) (*Client, error) {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return nil, err
	}
	c := &Client{
		addr:     conn.RemoteAddr().String(),
		password: password,
		conn:     conn,
		reader:   textproto.NewReader(bufio.NewReader(conn)),
		log:      log,
	}
	if err := c.auth(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) auth() error {
	// FreeSWITCH присылает "Content-Type: auth/request" сразу после connect.
	_, err := c.readBlock()
	if err != nil {
		return err
	}
	if err := c.write("auth " + c.password + "\n\n"); err != nil {
		return err
	}
	h, err := c.readBlock()
	if err != nil {
		return err
	}
	if h["Reply-Text"] != "+OK accepted" {
		return errors.New("esl auth failed: " + h["Reply-Text"])
	}
	return nil
}

func (c *Client) write(s string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.conn.Write([]byte(s))
	return err
}

// readBlock читает блок до пустой строки (FreeSWITCH ESL формат).
func (c *Client) readBlock() (map[string]string, error) {
	header, err := c.reader.ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(header))
	for k, v := range header {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out, nil
}

func (c *Client) API(cmd string) (string, error) {
	if err := c.write("api " + cmd + "\n\n"); err != nil {
		return "", err
	}
	h, err := c.readBlock()
	if err != nil {
		return "", err
	}
	return h["Reply-Text"], nil
}

func (c *Client) Subscribe(events ...string) error {
	return c.write("events plain " + strings.Join(events, " ") + "\n\n")
}

func (c *Client) Close() error { return c.conn.Close() }
