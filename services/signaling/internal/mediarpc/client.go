// Пакет mediarpc — клиент media-worker. В первой итерации — заглушка
// (без сгенерированного protobuf), которая возвращает SDP unchanged
// и логирует вызовы. Реальный клиент подключается через `make gen-proto`
// и слой gRPC поверх api/proto/mcu.proto.
package mediarpc

import (
	"context"
	"sync"

	"go.uber.org/zap"
)

type Client interface {
	AddPeer(ctx context.Context, roomID, peerID, displayName, role, sdpOffer string) (string, error)
	RemovePeer(ctx context.Context, roomID, peerID string) error
	Roster(ctx context.Context, roomID string) (map[string]string, error)
}

type Loopback struct {
	log *zap.Logger
	mu  sync.Mutex
	add func(ctx context.Context, roomID, peerID, name, role, sdpOffer string) (string, error)
}

func NewLoopback(log *zap.Logger) *Loopback { return &Loopback{log: log} }

func (l *Loopback) AddPeer(ctx context.Context, roomID, peerID, name, role, sdp string) (string, error) {
	l.log.Debug("loopback AddPeer", zap.String("room", roomID), zap.String("peer", peerID))
	// echo offer как answer (only для smoke-теста)
	return sdp, nil
}

func (l *Loopback) RemovePeer(ctx context.Context, roomID, peerID string) error {
	l.log.Debug("loopback RemovePeer", zap.String("room", roomID), zap.String("peer", peerID))
	return nil
}

func (l *Loopback) Roster(ctx context.Context, roomID string) (map[string]string, error) {
	return map[string]string{}, nil
}
