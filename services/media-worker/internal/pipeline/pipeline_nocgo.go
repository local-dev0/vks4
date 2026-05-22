//go:build !cgo

// Заглушка для go build без CGO (тесты, статический анализ).
// Возвращает Pipeline, который успешно стартует и принимает peer-ов,
// но не делает реального микширования. Production-сборка требует CGO + GStreamer.
package pipeline

import (
	"context"

	"github.com/vks4/vks4/services/media-worker/internal/layout"
)

type noopPipeline struct {
	spec  Spec
	video chan Sample
	audio chan Sample
	asd   chan ASDLevel
}

func NewGstFactory() Factory {
	return func(spec Spec) (Pipeline, error) {
		return &noopPipeline{
			spec:  spec,
			video: make(chan Sample, 1),
			audio: make(chan Sample, 1),
			asd:   make(chan ASDLevel, 1),
		}, nil
	}
}

func (p *noopPipeline) Start(ctx context.Context) error { return nil }
func (p *noopPipeline) Stop(ctx context.Context) error  { return nil }
func (p *noopPipeline) AddPeer(peerID string) (chan<- []byte, chan<- []byte, error) {
	v := make(chan []byte, 1)
	a := make(chan []byte, 1)
	return v, a, nil
}
func (p *noopPipeline) RemovePeer(peerID string) error           { return nil }
func (p *noopPipeline) VideoOut() <-chan Sample                  { return p.video }
func (p *noopPipeline) AudioOut() <-chan Sample                  { return p.audio }
func (p *noopPipeline) ASDLevel() <-chan ASDLevel                { return p.asd }
func (p *noopPipeline) UpdateLayout(cells []layout.Cell) error   { return nil }
func (p *noopPipeline) StartRecording(filename string) error     { return nil }
func (p *noopPipeline) StopRecording() (string, int64, error)    { return "", 0, nil }
