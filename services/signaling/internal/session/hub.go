package session

import (
	"sync"

	"github.com/vks4/vks4/services/signaling/internal/protocol"
)

type Hub struct {
	mu    sync.RWMutex
	rooms map[string]map[string]*Session
}

func NewHub() *Hub {
	return &Hub{rooms: map[string]map[string]*Session{}}
}

func (h *Hub) Add(roomID string, s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[roomID]
	if r == nil {
		r = map[string]*Session{}
		h.rooms[roomID] = r
	}
	r[s.ID] = s
}

func (h *Hub) Remove(roomID, peerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.rooms[roomID]; r != nil {
		delete(r, peerID)
		if len(r) == 0 {
			delete(h.rooms, roomID)
		}
	}
}

func (h *Hub) Participants(roomID string) []protocol.Participant {
	h.mu.RLock()
	defer h.mu.RUnlock()
	r := h.rooms[roomID]
	out := make([]protocol.Participant, 0, len(r))
	for _, s := range r {
		out = append(out, s.ToParticipant())
	}
	return out
}

// Broadcast пересылает событие всем участникам комнаты, кроме отправителя.
func (h *Hub) Broadcast(roomID, except string, env protocol.Envelope) {
	h.mu.RLock()
	r := h.rooms[roomID]
	targets := make([]*Session, 0, len(r))
	for id, s := range r {
		if id == except {
			continue
		}
		targets = append(targets, s)
	}
	h.mu.RUnlock()
	for _, s := range targets {
		s.Write(env)
	}
}

// BroadcastAll — события от сервера (active-speaker, layout) идут всем.
func (h *Hub) BroadcastAll(roomID string, env protocol.Envelope) {
	h.Broadcast(roomID, "", env)
}

// Kick — выкинуть участника (из gRPC API).
func (h *Hub) Kick(roomID, peerID, reason string) {
	h.mu.RLock()
	s := h.rooms[roomID][peerID]
	h.mu.RUnlock()
	if s == nil {
		return
	}
	s.sendError("kicked", reason)
	s.cleanup()
}
