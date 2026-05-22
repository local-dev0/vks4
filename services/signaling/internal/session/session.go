package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/vks4/vks4/services/signaling/internal/auth"
	"github.com/vks4/vks4/services/signaling/internal/presence"
	"github.com/vks4/vks4/services/signaling/internal/protocol"
)

type MediaClient interface {
	AddPeer(ctx context.Context, roomID, peerID, displayName, role, sdpOffer string) (sdpAnswer string, err error)
	RemovePeer(ctx context.Context, roomID, peerID string) error
	Roster(ctx context.Context, roomID string) (map[string]string, error)
}

type Session struct {
	ID          string
	conn        *websocket.Conn
	log         *zap.Logger
	hub         *Hub
	media       MediaClient
	verifier    *auth.Verifier
	presence    *presence.Store
	roomID      string
	displayName string
	role        string
	userID      string
	joinedAt    time.Time
	mu          sync.Mutex
	closed      bool
}

func New(conn *websocket.Conn, hub *Hub, media MediaClient, verifier *auth.Verifier, pres *presence.Store, log *zap.Logger) *Session {
	return &Session{
		ID:       uuid.NewString(),
		conn:     conn,
		log:      log,
		hub:      hub,
		media:    media,
		verifier: verifier,
		presence: pres,
	}
}

func (s *Session) Run(ctx context.Context) error {
	defer s.cleanup()
	for {
		var env protocol.Envelope
		if err := wsjson.Read(ctx, s.conn, &env); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		if err := s.handle(ctx, env); err != nil {
			s.sendError("internal", err.Error())
			s.log.Warn("handle", zap.Error(err))
		}
	}
}

func (s *Session) handle(ctx context.Context, env protocol.Envelope) error {
	switch env.Type {
	case protocol.TypeJoin:
		var p protocol.JoinPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return err
		}
		return s.onJoin(ctx, p)
	case protocol.TypeOffer:
		var p protocol.SDPPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return err
		}
		return s.onOffer(ctx, p)
	case protocol.TypeCandidate:
		var p protocol.CandidatePayload
		_ = json.Unmarshal(env.Payload, &p)
		// В trickle ICE кандидаты обычно идут после offer/answer. В нашем MCU
		// Pion на media-worker открыт на UDP — клиент шлёт кандидаты к нашему ICE
		// stack-у через тот же signalling канал (см. протокол docs/SIGNALING.md).
		return nil
	case protocol.TypeChat:
		var p protocol.ChatPayload
		_ = json.Unmarshal(env.Payload, &p)
		if s.roomID != "" {
			s.hub.Broadcast(s.roomID, s.ID, protocol.Envelope{
				Type:    "chat",
				Payload: env.Payload,
			})
		}
		return nil
	case protocol.TypeControl:
		var p protocol.ControlPayload
		_ = json.Unmarshal(env.Payload, &p)
		// Перенаправляем control-события в hub (mute/unmute/raiseHand/setLayout).
		if s.roomID != "" {
			s.hub.Broadcast(s.roomID, s.ID, protocol.Envelope{
				Type:    "control",
				Payload: env.Payload,
			})
		}
		return nil
	case protocol.TypePing:
		return s.send(protocol.Envelope{Type: protocol.TypePong})
	case protocol.TypeLeave:
		return errors.New("leave")
	default:
		s.sendError("invalid_argument", "unknown type")
	}
	return nil
}

func (s *Session) onJoin(ctx context.Context, p protocol.JoinPayload) error {
	if p.Token == "" || p.RoomID == "" {
		return errors.New("token/roomId required")
	}
	c, err := s.verifier.Parse(p.Token)
	if err != nil {
		s.sendError("unauthorized", "invalid token")
		return nil
	}
	s.userID = c.UserID.String()
	s.role = c.Role
	s.displayName = p.DisplayName
	if s.displayName == "" {
		s.displayName = c.Email
	}
	s.roomID = p.RoomID
	s.joinedAt = time.Now()
	s.hub.Add(s.roomID, s)
	// Публикуем участника в Redis — admin UI читает оттуда через control-plane.
	if err := s.presence.Add(ctx, s.roomID, presence.Participant{
		PeerID: s.ID, UserID: s.userID, DisplayName: s.displayName, Role: s.role,
		Transport: "webrtc", JoinedAt: s.joinedAt,
	}); err != nil {
		s.log.Warn("presence add", zap.Error(err))
	}
	// Уведомить участников
	s.hub.Broadcast(s.roomID, s.ID, protocol.Envelope{
		Type: protocol.TypePeerJoined,
		Payload: mustJSON(protocol.Participant{
			PeerID: s.ID, DisplayName: s.displayName, Role: s.role,
		}),
	})
	// Ответ клиенту: список текущих участников.
	parts := s.hub.Participants(s.roomID)
	resp := protocol.JoinedPayload{SelfID: s.ID, Participants: parts}
	return s.send(protocol.Envelope{Type: protocol.TypeJoined, Payload: mustJSON(resp)})
}

func (s *Session) onOffer(ctx context.Context, p protocol.SDPPayload) error {
	if s.roomID == "" {
		return errors.New("not joined")
	}
	answer, err := s.media.AddPeer(ctx, s.roomID, s.ID, s.displayName, s.role, p.SDP)
	if err != nil {
		return err
	}
	if err := s.send(protocol.Envelope{
		Type:    protocol.TypeAnswer,
		Payload: mustJSON(protocol.SDPPayload{SDP: answer}),
	}); err != nil {
		return err
	}
	// После того как peer добавлен в комнату — пушим roster всем (включая нового).
	s.broadcastRoster(ctx)
	return nil
}

// broadcastRoster тянет текущий slot-map из media-worker и рассылает всем участникам.
// Клиенты используют этот map чтобы понять какому peer-у принадлежит какой slot.
func (s *Session) broadcastRoster(ctx context.Context) {
	if s.roomID == "" {
		return
	}
	slots, err := s.media.Roster(ctx, s.roomID)
	if err != nil {
		s.log.Warn("roster fetch", zap.Error(err))
		return
	}
	// Обогащаем displayName-ами из hub'а.
	hubParts := s.hub.Participants(s.roomID)
	displays := make(map[string]string, len(hubParts))
	for _, p := range hubParts {
		displays[p.PeerID] = p.DisplayName
	}
	type entry struct {
		Slot        int    `json:"slot"`
		PeerID      string `json:"peerId"`
		DisplayName string `json:"displayName"`
		IsSelf      bool   `json:"-"`
	}
	roster := make([]entry, 0, len(slots))
	for kStr, peerID := range slots {
		var slot int
		_, _ = fmt.Sscanf(kStr, "%d", &slot)
		roster = append(roster, entry{Slot: slot, PeerID: peerID, DisplayName: displays[peerID]})
	}
	s.hub.BroadcastAll(s.roomID, protocol.Envelope{
		Type:    "roster",
		Payload: mustJSON(roster),
	})
}

func (s *Session) send(env protocol.Envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("closed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return wsjson.Write(ctx, s.conn, env)
}

func (s *Session) sendError(code, msg string) {
	_ = s.send(protocol.Envelope{
		Type:    protocol.TypeError,
		Payload: mustJSON(protocol.ErrorPayload{Code: code, Message: msg}),
	})
}

func (s *Session) cleanup() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	if s.roomID != "" {
		_ = s.media.RemovePeer(context.Background(), s.roomID, s.ID)
		_ = s.presence.Remove(context.Background(), s.roomID, s.ID)
		s.hub.Remove(s.roomID, s.ID)
		s.hub.Broadcast(s.roomID, s.ID, protocol.Envelope{
			Type: protocol.TypePeerLeft,
			Payload: mustJSON(protocol.Participant{
				PeerID: s.ID, DisplayName: s.displayName, Role: s.role,
			}),
		})
		s.broadcastRoster(context.Background())
	}
	_ = s.conn.Close(websocket.StatusNormalClosure, "bye")
}

func (s *Session) ToParticipant() protocol.Participant {
	return protocol.Participant{PeerID: s.ID, DisplayName: s.displayName, Role: s.role}
}

func (s *Session) Write(env protocol.Envelope) { _ = s.send(env) }

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
