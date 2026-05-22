// Тонкий HTTP-клиент к signaling для broadcast событий комнатам.
package signalingrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	base string
	hc   *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		base: baseURL,
		hc:   &http.Client{Timeout: 5 * time.Second},
	}
}

type broadcastReq struct {
	RoomID  string      `json:"roomId"`
	Event   string      `json:"event"`
	Payload interface{} `json:"payload"`
}

// Broadcast — отправить произвольное событие всем WS-сессиям комнаты.
// Используется например при изменении layout.
func (c *Client) Broadcast(ctx context.Context, roomID, event string, payload interface{}) error {
	if c == nil || c.base == "" {
		return nil
	}
	body, err := json.Marshal(broadcastReq{RoomID: roomID, Event: event, Payload: payload})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/broadcast", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("signaling broadcast %d", resp.StatusCode)
	}
	return nil
}
