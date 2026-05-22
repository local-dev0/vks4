// Пакет pipeline — GStreamer compositor + audiomixer per room.
//
// Архитектура:
//
//	per peer:        appsrc-video-peerN  ─┐
//	                                      ├─▶ compositor name=vmix ─▶ x264enc/vp8enc ─▶ rtphXpay ─▶ appsink video_out
//	                 appsrc-audio-peerN  ─┐
//	                                      ├─▶ audiomixer name=amix ─▶ opusenc        ─▶ rtpopuspay ─▶ appsink audio_out
//	                                      └─▶ level (per source) — для VAD/ASD
//	recording branch: tee (после vmix/amix) ─▶ mp4mux ─▶ filesink (RECORDING_DIR/<room>.mp4)
//
// В первой итерации функции go-gst инкапсулированы за интерфейсом Pipeline.
// Конкретная реализация — gstPipeline (см. pipeline_gst.go), требует CGO + GStreamer.
package pipeline

import (
	"context"
	"sync"

	"github.com/vks4/vks4/services/media-worker/internal/layout"
)

// Sample — закодированный медиафрейм от encoder-а на egress.
type Sample struct {
	Data       []byte
	Duration   uint32 // в пакетах RTP передаются raw — duration справочно
	IsKeyFrame bool
}

type Pipeline interface {
	// Start запускает GStreamer pipeline комнаты.
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	// AddPeer создаёт appsrc-видео и appsrc-аудио для нового peer.
	// Возвращает каналы, в которые надо писать RTP пакеты (depayload делает pipeline).
	AddPeer(peerID string) (videoIn chan<- []byte, audioIn chan<- []byte, err error)
	RemovePeer(peerID string) error

	// VideoOut / AudioOut — каналы, откуда worker берёт закодированные миксованные сэмплы.
	VideoOut() <-chan Sample
	AudioOut() <-chan Sample

	// ASDLevel — поток (peerID, dBFS) для активного спикера.
	ASDLevel() <-chan ASDLevel

	// UpdateLayout перестраивает позиции compositor.sink_N.
	UpdateLayout(cells []layout.Cell) error

	// Recording on/off; возвращает путь к файлу при остановке.
	StartRecording(filename string) error
	StopRecording() (path string, size int64, err error)
}

type ASDLevel struct {
	PeerID string
	DBFS   float64
}

// Spec — параметры pipeline комнаты.
type Spec struct {
	RoomID       string
	Width        int
	Height       int
	FPS          int
	VideoBitrate int
	AudioBitrate int
	VideoCodec   string // "vp8" | "h264"
}

// Registry хранит активные pipeline по roomID.
type Registry struct {
	mu        sync.RWMutex
	pipelines map[string]Pipeline
	factory   Factory
}

type Factory func(spec Spec) (Pipeline, error)

func NewRegistry(f Factory) *Registry {
	return &Registry{pipelines: map[string]Pipeline{}, factory: f}
}

func (r *Registry) GetOrCreate(ctx context.Context, spec Spec) (Pipeline, error) {
	r.mu.RLock()
	if p, ok := r.pipelines[spec.RoomID]; ok {
		r.mu.RUnlock()
		return p, nil
	}
	r.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.pipelines[spec.RoomID]; ok {
		return p, nil
	}
	p, err := r.factory(spec)
	if err != nil {
		return nil, err
	}
	if err := p.Start(ctx); err != nil {
		return nil, err
	}
	r.pipelines[spec.RoomID] = p
	return p, nil
}

func (r *Registry) Get(roomID string) Pipeline {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pipelines[roomID]
}

func (r *Registry) Destroy(ctx context.Context, roomID string) error {
	r.mu.Lock()
	p := r.pipelines[roomID]
	delete(r.pipelines, roomID)
	r.mu.Unlock()
	if p == nil {
		return nil
	}
	return p.Stop(ctx)
}

func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.pipelines)
}
