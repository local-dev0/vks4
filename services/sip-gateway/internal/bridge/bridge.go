// Bridge — SIP call ↔ MCU peer.
// Каркас. Реальная реализация v0.2: на каждое INVITE из FreeSWITCH создаётся
// WebRTC peer в media-worker, RTP-пакеты передаются через FreeSWITCH RTP-proxy
// (verto/mod_audio_fork) или через парный RTP-relay внутри bridge-процесса.
package bridge

import (
	"context"
	"errors"
	"sync"

	"go.uber.org/zap"
)

type Call struct {
	UUID    string
	From    string
	To      string
	RoomID  string
	PeerID  string
}

type Bridge struct {
	log   *zap.Logger
	mu    sync.RWMutex
	calls map[string]*Call // UUID → Call
}

func New(log *zap.Logger) *Bridge {
	return &Bridge{log: log, calls: map[string]*Call{}}
}

func (b *Bridge) Accept(ctx context.Context, c *Call) error {
	if c.UUID == "" || c.RoomID == "" {
		return errors.New("uuid and roomId required")
	}
	b.mu.Lock()
	b.calls[c.UUID] = c
	b.mu.Unlock()
	b.log.Info("sip accept", zap.String("uuid", c.UUID), zap.String("from", c.From), zap.String("room", c.RoomID))
	// TODO v0.2:
	// 1. через media-worker HTTP API создать peer (POST /rooms/{room}/peers)
	//    с SDP offer (генерация offer на стороне sip-gateway либо проксирование от FS)
	// 2. собрать SDP answer и отдать обратно в FS через mod_dptools/bridge
	return nil
}

func (b *Bridge) Release(ctx context.Context, uuid string) error {
	b.mu.Lock()
	c := b.calls[uuid]
	delete(b.calls, uuid)
	b.mu.Unlock()
	if c == nil {
		return nil
	}
	b.log.Info("sip release", zap.String("uuid", uuid))
	// TODO v0.2: DELETE /rooms/{room}/peers/{peer}
	return nil
}

func (b *Bridge) Active() []Call {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Call, 0, len(b.calls))
	for _, c := range b.calls {
		out = append(out, *c)
	}
	return out
}
