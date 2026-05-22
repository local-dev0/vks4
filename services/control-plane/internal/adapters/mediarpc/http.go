package mediarpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/vks4/vks4/services/control-plane/internal/domain"
)

// HTTPClient — клиент media-worker через REST API (см. services/media-worker/internal/httpapi).
// Реализует mediarpc.Client. CreateRoom — no-op: media-worker создаёт комнаты лениво при первом
// AddPeer (это делает signaling-сервис), поэтому из control-plane достаточно UpdateLayout / Destroy / Kick.
type HTTPClient struct {
	base string
	hc   *http.Client
}

func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		base: baseURL,
		hc:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *HTTPClient) CreateRoom(ctx context.Context, _ domain.Room) error { return nil }

func (c *HTTPClient) DestroyRoom(ctx context.Context, id uuid.UUID) error {
	url := fmt.Sprintf("%s/rooms/%s", c.base, id)
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 && res.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("DestroyRoom %d: %s", res.StatusCode, string(b))
	}
	return nil
}

type layoutCell struct {
	ID     string  `json:"id"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
	ZIndex int     `json:"zIndex,omitempty"`
	VAD    bool    `json:"vad,omitempty"`
}

type layoutReq struct {
	Mode   string       `json:"mode"`
	Width  int          `json:"width,omitempty"`
	Height int          `json:"height,omitempty"`
	Cells  []layoutCell `json:"cells,omitempty"`
}

func (c *HTTPClient) UpdateLayout(ctx context.Context, id uuid.UUID, layout domain.Layout) error {
	cells := make([]layoutCell, 0, len(layout.Cells))
	for _, cc := range layout.Cells {
		cells = append(cells, layoutCell{
			ID: cc.ID, X: cc.X, Y: cc.Y, W: cc.W, H: cc.H, ZIndex: cc.ZIndex, VAD: cc.VAD,
		})
	}
	body, _ := json.Marshal(layoutReq{
		Mode:   string(layout.Mode),
		Width:  layout.Width,
		Height: layout.Height,
		Cells:  cells,
	})
	url := fmt.Sprintf("%s/rooms/%s/layout", c.base, id)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 && res.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("UpdateLayout %d: %s", res.StatusCode, string(b))
	}
	return nil
}

func (c *HTTPClient) AssignSlots(ctx context.Context, id uuid.UUID, slots map[int]string) error {
	body := make(map[string]string, len(slots))
	for k, v := range slots {
		body[fmt.Sprintf("%d", k)] = v
	}
	buf, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/rooms/%s/slots", c.base, id)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 && res.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("AssignSlots %d: %s", res.StatusCode, string(b))
	}
	return nil
}

func (c *HTTPClient) GetRoster(ctx context.Context, id uuid.UUID) (map[string]string, error) {
	url := fmt.Sprintf("%s/rooms/%s/roster", c.base, id)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return map[string]string{}, nil
	}
	var out struct {
		Slots map[string]string `json:"slots"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Slots == nil {
		out.Slots = map[string]string{}
	}
	return out.Slots, nil
}

func (c *HTTPClient) StartRecording(ctx context.Context, id uuid.UUID) (string, error) {
	url := fmt.Sprintf("%s/rooms/%s/recording", c.base, id)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	res, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		b, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("StartRecording %d: %s", res.StatusCode, string(b))
	}
	var out struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Path, nil
}

func (c *HTTPClient) StopRecording(ctx context.Context, id uuid.UUID) (string, int64, error) {
	url := fmt.Sprintf("%s/rooms/%s/recording", c.base, id)
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	res, err := c.hc.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		b, _ := io.ReadAll(res.Body)
		return "", 0, fmt.Errorf("StopRecording %d: %s", res.StatusCode, string(b))
	}
	var out struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", 0, err
	}
	return out.Path, out.Size, nil
}

func (c *HTTPClient) KickPeer(ctx context.Context, room uuid.UUID, peerID string) error {
	url := fmt.Sprintf("%s/rooms/%s/peers/%s", c.base, room, peerID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 && res.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("KickPeer %d: %s", res.StatusCode, string(b))
	}
	return nil
}
