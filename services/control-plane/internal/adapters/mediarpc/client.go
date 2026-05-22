// Пакет mediarpc — клиент к media-worker.
// В первой итерации мы не подключаем сгенерированные protobuf-стабы,
// чтобы control-plane мог компилироваться отдельно. Контракт определён
// в api/proto/mcu.proto; здесь — тонкая абстракция, к которой адаптер
// можно подключить, когда запущен `make gen-proto`.
package mediarpc

import (
	"context"

	"github.com/google/uuid"

	"github.com/vks4/vks4/services/control-plane/internal/domain"
)

type Client interface {
	CreateRoom(ctx context.Context, room domain.Room) error
	DestroyRoom(ctx context.Context, id uuid.UUID) error
	UpdateLayout(ctx context.Context, id uuid.UUID, layout domain.Layout) error
	AssignSlots(ctx context.Context, id uuid.UUID, slots map[int]string) error
	GetRoster(ctx context.Context, id uuid.UUID) (map[string]string, error)
	StartRecording(ctx context.Context, id uuid.UUID) (string, error) // url
	StopRecording(ctx context.Context, id uuid.UUID) (string, int64, error)
	KickPeer(ctx context.Context, room uuid.UUID, peerID string) error
}

// Noop — fallback, удобен для локального запуска без media-worker.
type Noop struct{}

func (Noop) CreateRoom(ctx context.Context, _ domain.Room) error               { return nil }
func (Noop) DestroyRoom(ctx context.Context, _ uuid.UUID) error                { return nil }
func (Noop) UpdateLayout(ctx context.Context, _ uuid.UUID, _ domain.Layout) error { return nil }
func (Noop) AssignSlots(ctx context.Context, _ uuid.UUID, _ map[int]string) error { return nil }
func (Noop) GetRoster(ctx context.Context, _ uuid.UUID) (map[string]string, error) { return map[string]string{}, nil }
func (Noop) StartRecording(ctx context.Context, _ uuid.UUID) (string, error)   { return "", nil }
func (Noop) StopRecording(ctx context.Context, _ uuid.UUID) (string, int64, error) {
	return "", 0, nil
}
func (Noop) KickPeer(ctx context.Context, _ uuid.UUID, _ string) error { return nil }
