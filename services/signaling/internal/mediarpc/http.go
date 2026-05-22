package mediarpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// HTTPClient — клиент media-worker через REST API (см. services/media-worker/internal/httpapi).
type HTTPClient struct {
	base string
	hc   *http.Client
	log  *zap.Logger
}

func NewHTTPClient(baseURL string, log *zap.Logger) *HTTPClient {
	return &HTTPClient{
		base: baseURL,
		hc:   &http.Client{Timeout: 15 * time.Second},
		log:  log,
	}
}

type addPeerReq struct {
	PeerID      string `json:"peerId"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	SDPOffer    string `json:"sdpOffer"`
}

type addPeerResp struct {
	SDPAnswer string `json:"sdpAnswer"`
}

func (c *HTTPClient) AddPeer(ctx context.Context, roomID, peerID, displayName, role, sdpOffer string) (string, error) {
	body, err := json.Marshal(addPeerReq{PeerID: peerID, DisplayName: displayName, Role: role, SDPOffer: sdpOffer})
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("%s/rooms/%s/peers", c.base, roomID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		b, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("media-worker AddPeer %d: %s", res.StatusCode, string(b))
	}
	var out addPeerResp
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.SDPAnswer == "" {
		return "", fmt.Errorf("media-worker returned empty SDP answer")
	}
	c.log.Debug("AddPeer ok", zap.String("room", roomID), zap.String("peer", peerID))
	return out.SDPAnswer, nil
}

func (c *HTTPClient) RemovePeer(ctx context.Context, roomID, peerID string) error {
	url := fmt.Sprintf("%s/rooms/%s/peers/%s", c.base, roomID, peerID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 && res.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("media-worker RemovePeer %d: %s", res.StatusCode, string(b))
	}
	return nil
}

type rosterResp struct {
	Slots map[string]string `json:"slots"`
}

// Roster берёт текущий map slot→peerID из media-worker для комнаты.
func (c *HTTPClient) Roster(ctx context.Context, roomID string) (map[string]string, error) {
	url := fmt.Sprintf("%s/rooms/%s/roster", c.base, roomID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("media-worker Roster %d", res.StatusCode)
	}
	var out rosterResp
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Slots, nil
}
