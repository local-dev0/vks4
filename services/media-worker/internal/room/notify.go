// Notifier — рассылка обновления roster'а в signaling после изменения участников.
// SIP-peer создаётся через media-worker HTTP API (не через signaling-WS), поэтому
// signaling сам не узнаёт о его появлении и не пушит обновлённый roster клиентам.
// Этот пакет шлёт POST /broadcast в signaling с готовым событием "roster".
package room

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// RosterEntry — формат, который ожидает signaling-клиент (см. session.broadcastRoster).
type RosterEntry struct {
	Slot        int    `json:"slot"`
	PeerID      string `json:"peerId"`
	DisplayName string `json:"displayName"`
}

type Notifier interface {
	OnRosterChange(ctx context.Context, roomID string, entries []RosterEntry)
}

type signalingHTTPNotifier struct {
	url    string // http://signaling:8081
	log    *zap.Logger
	client *http.Client
}

func NewSignalingHTTPNotifier(url string, log *zap.Logger) Notifier {
	return &signalingHTTPNotifier{
		url:    url,
		log:    log,
		client: &http.Client{Timeout: 3 * time.Second},
	}
}

func (n *signalingHTTPNotifier) OnRosterChange(ctx context.Context, roomID string, entries []RosterEntry) {
	if n.url == "" {
		return
	}
	payload, err := json.Marshal(entries)
	if err != nil {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"roomId":  roomID,
		"event":   "roster",
		"payload": json.RawMessage(payload),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url+"/broadcast", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		n.log.Warn("signaling notify failed", zap.String("room", roomID), zap.Error(err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		n.log.Warn("signaling notify bad status", zap.Int("status", resp.StatusCode), zap.String("room", roomID))
		return
	}
	n.log.Info("signaling notify roster", zap.String("room", roomID), zap.Int("entries", len(entries)))
}
