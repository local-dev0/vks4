package ws

import (
	"net/http"

	"github.com/coder/websocket"
	"go.uber.org/zap"

	"github.com/vks4/vks4/services/signaling/internal/auth"
	"github.com/vks4/vks4/services/signaling/internal/mediarpc"
	"github.com/vks4/vks4/services/signaling/internal/presence"
	"github.com/vks4/vks4/services/signaling/internal/session"
)

type Handler struct {
	hub      *session.Hub
	media    mediarpc.Client
	verifier *auth.Verifier
	presence *presence.Store
	log      *zap.Logger
}

func NewHandler(hub *session.Hub, media mediarpc.Client, verifier *auth.Verifier, pres *presence.Store, log *zap.Logger) *Handler {
	return &Handler{hub: hub, media: media, verifier: verifier, presence: pres, log: log}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // TLS у Caddy; внутри сети ок
		Subprotocols:       []string{"vks4.signaling.v1"},
		// CompressionDisabled убирает несовместимости с разными расширениями.
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		h.log.Warn("ws accept", zap.Error(err))
		return
	}
	// Дефолтный лимит 32 KB слишком мал для больших SDP-офферов
	// (4 sendonly + 4 recvonly transceivers ⇒ десятки m-lines + ICE).
	conn.SetReadLimit(1 << 20) // 1 MB
	h.log.Info("ws accepted", zap.String("remote", r.RemoteAddr), zap.String("proto", conn.Subprotocol()))
	sess := session.New(conn, h.hub, h.media, h.verifier, h.presence, h.log)
	ctx := r.Context()
	// Recover чтобы panic в session.Run не приводил к молчаливому закрытию WS без логов.
	defer func() {
		if rec := recover(); rec != nil {
			h.log.Error("ws session panic", zap.Any("panic", rec))
		}
	}()
	if err := sess.Run(ctx); err != nil {
		h.log.Info("ws closed", zap.Error(err))
	} else {
		h.log.Info("ws ended cleanly")
	}
}
