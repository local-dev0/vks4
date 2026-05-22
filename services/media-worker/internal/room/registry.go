package room

import (
	"context"
	"sync"

	"github.com/pion/webrtc/v4"
	"go.uber.org/zap"

	"github.com/vks4/vks4/services/media-worker/internal/pipeline"
)

type Registry struct {
	mu         sync.RWMutex
	rooms      map[string]*Room
	pipes      *pipeline.Registry
	log        *zap.Logger
	ice        []webrtc.ICEServer
	videoCodec string
	width      int
	height     int
	fps        int
	vBitrate   int
	aBitrate   int
	sfu        bool
	publicIP   string
	udpMin     uint16
	udpMax     uint16
}

func NewRegistry(p *pipeline.Registry, log *zap.Logger, ice []webrtc.ICEServer, videoCodec string, w, h, fps, vBR, aBR int, sfu bool, publicIP string, udpMin, udpMax uint16) *Registry {
	return &Registry{
		rooms: map[string]*Room{}, pipes: p, log: log, ice: ice,
		videoCodec: videoCodec, width: w, height: h, fps: fps, vBitrate: vBR, aBitrate: aBR,
		sfu: sfu, publicIP: publicIP, udpMin: udpMin, udpMax: udpMax,
	}
}

func (r *Registry) GetOrCreate(ctx context.Context, roomID string) (*Room, error) {
	r.mu.RLock()
	if rm, ok := r.rooms[roomID]; ok {
		r.mu.RUnlock()
		return rm, nil
	}
	r.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if rm, ok := r.rooms[roomID]; ok {
		return rm, nil
	}
	pipe, err := r.pipes.GetOrCreate(ctx, pipeline.Spec{
		RoomID: roomID, Width: r.width, Height: r.height, FPS: r.fps,
		VideoBitrate: r.vBitrate, AudioBitrate: r.aBitrate, VideoCodec: r.videoCodec,
	})
	if err != nil {
		return nil, err
	}
	rm := New(Options{
		ID: roomID, Log: r.log, Pipeline: pipe, ICE: r.ice,
		VideoCodec: r.videoCodec, Width: r.width, Height: r.height,
		SFU: r.sfu, PublicIP: r.publicIP, UDPMin: r.udpMin, UDPMax: r.udpMax,
	})
	r.rooms[roomID] = rm
	return rm, nil
}

func (r *Registry) Get(roomID string) *Room {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.rooms[roomID]
}

func (r *Registry) Destroy(ctx context.Context, roomID string) error {
	r.mu.Lock()
	rm := r.rooms[roomID]
	delete(r.rooms, roomID)
	r.mu.Unlock()
	if rm == nil {
		return nil
	}
	if err := rm.Close(ctx); err != nil {
		r.log.Warn("close room", zap.Error(err))
	}
	return r.pipes.Destroy(ctx, roomID)
}

func (r *Registry) Snapshot() []Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Snapshot, 0, len(r.rooms))
	for _, rm := range r.rooms {
		out = append(out, rm.Snapshot())
	}
	return out
}

func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.rooms)
}
