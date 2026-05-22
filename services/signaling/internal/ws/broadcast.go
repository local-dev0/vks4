package ws

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/vks4/vks4/services/signaling/internal/protocol"
	"github.com/vks4/vks4/services/signaling/internal/session"
)

// BroadcastHandler — внутренний HTTP endpoint для control-plane:
// POST /broadcast {roomId, event, payload}
// Отправляет событие всем WS-сессиям комнаты.
type BroadcastHandler struct {
	hub *session.Hub
	log *zap.Logger
}

func NewBroadcastHandler(hub *session.Hub, log *zap.Logger) *BroadcastHandler {
	return &BroadcastHandler{hub: hub, log: log}
}

type broadcastReq struct {
	RoomID  string          `json:"roomId"`
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
}

func (h *BroadcastHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in broadcastReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if in.RoomID == "" || in.Event == "" {
		http.Error(w, "roomId and event required", http.StatusBadRequest)
		return
	}
	h.hub.BroadcastAll(in.RoomID, protocol.Envelope{
		Type:    in.Event,
		Payload: in.Payload,
	})
	h.log.Debug("broadcast", zap.String("room", in.RoomID), zap.String("event", in.Event))
	w.WriteHeader(http.StatusNoContent)
}
