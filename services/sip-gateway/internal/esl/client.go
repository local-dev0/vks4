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
	"io"
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

// Events запускает event reader loop. Возвращает канал событий — каждое событие это
// мап header→value из ESL-сообщения. Поле "Event-Name" указывает тип события (например,
// "CHANNEL_CREATE", "CHANNEL_HANGUP"). Канал закрывается при ошибке чтения или Close().
func (c *Client) Events(ctx context.Context) <-chan map[string]string {
	ch := make(chan map[string]string, 64)
	go func() {
		defer close(ch)
		for {
			if ctx.Err() != nil {
				return
			}
			ev, err := c.readBlock()
			if err != nil {
				c.log.Warn("esl read", zap.Error(err))
				return
			}
			contentType := ev["Content-Type"]
			if contentType == "text/event-plain" {
				// Тело события — отдельный MIME-блок после Content-Length байт.
				body, err := c.readBody(ev["Content-Length"])
				if err != nil {
					c.log.Warn("esl read body", zap.Error(err))
					return
				}
				ev = parseEventBody(body)
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// readBody читает Content-Length байт из соединения как новый блок.
// Использует io.ReadFull — bufio.Read возвращает partial при недостаточно данных,
// и оставшийся SDP body парсился как очередные header'ы → "malformed MIME header".
func (c *Client) readBody(lengthHdr string) (string, error) {
	if lengthHdr == "" {
		return "", nil
	}
	n := 0
	for _, ch := range lengthHdr {
		if ch >= '0' && ch <= '9' {
			n = n*10 + int(ch-'0')
		}
	}
	if n <= 0 {
		return "", nil
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(c.reader.R, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// parseEventBody парсит ESL event body (text/event-plain) в map header→value.
// Формат: "Key: value\n" repeated, с url-decoded значениями.
func parseEventBody(body string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		i := strings.IndexByte(line, ':')
		if i < 1 {
			continue
		}
		k := strings.TrimSpace(line[:i])
		v := strings.TrimSpace(line[i+1:])
		v = urlDecode(v)
		out[k] = v
	}
	return out
}

// urlDecode — минимальный URL-decoder для %XX последовательностей (ESL экранирует значения).
func urlDecode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi := hexDigit(s[i+1])
			lo := hexDigit(s[i+2])
			if hi >= 0 && lo >= 0 {
				b.WriteByte(byte(hi*16 + lo))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func hexDigit(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

func (c *Client) Close() error { return c.conn.Close() }
