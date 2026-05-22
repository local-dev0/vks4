// Bridge — SIP call ↔ MCU peer.
// Каркас. Реальная реализация v0.2: на каждое INVITE из FreeSWITCH создаётся
// WebRTC peer в media-worker, RTP-пакеты передаются через FreeSWITCH RTP-proxy
// (verto/mod_audio_fork) или через парный RTP-relay внутри bridge-процесса.
package bridge

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

type Call struct {
	UUID      string    `json:"uuid"`
	Caller    string    `json:"caller"`
	RoomID    string    `json:"roomId"`
	PeerID    string    `json:"peerId,omitempty"`
	StartedAt time.Time `json:"startedAt"`
}

type Bridge struct {
	log   *zap.Logger
	mu    sync.RWMutex
	calls map[string]*Call // UUID → Call
}

func New(log *zap.Logger) *Bridge {
	return &Bridge{log: log, calls: map[string]*Call{}}
}

// Accept регистрирует входящий SIP-звонок. UUID = FreeSWITCH channel id.
// Реальный media-bridge (создание peer в media-worker, RTP forward) — следующая итерация.
func (b *Bridge) Accept(uuid, room, caller string) {
	if uuid == "" || room == "" {
		return
	}
	c := &Call{UUID: uuid, Caller: caller, RoomID: room, StartedAt: time.Now()}
	b.mu.Lock()
	b.calls[uuid] = c
	b.mu.Unlock()
	b.log.Info("sip accept", zap.String("uuid", uuid), zap.String("caller", caller), zap.String("room", room))
	// TODO:
	// 1. через media-worker HTTP API создать peer (POST /rooms/{room}/peers)
	//    с заглушечным SDP. peer.go должен уметь plain-RTP mode (без DTLS-SRTP).
	// 2. RTP forward: FreeSWITCH → sip-gateway → media-worker appsrc.
	// 3. Reverse RTP: media-worker output → sip-gateway → FS → SIP terminal.
}

// Release вызывается при CHANNEL_DESTROY — освобождаем slot и (в будущем) peer в media-worker.
func (b *Bridge) Release(uuid string) {
	b.mu.Lock()
	c := b.calls[uuid]
	delete(b.calls, uuid)
	b.mu.Unlock()
	if c == nil {
		return
	}
	b.log.Info("sip release", zap.String("uuid", uuid), zap.String("room", c.RoomID))
	// TODO: DELETE /rooms/{room}/peers/{peer}
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
