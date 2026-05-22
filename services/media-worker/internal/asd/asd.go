// Active Speaker Detector: per-peer rolling RMS с гистерезисом.
package asd

import (
	"math"
	"sync"
	"time"
)

type Sample struct {
	PeerID string
	DBFS   float64
	When   time.Time
}

type Detector struct {
	mu            sync.Mutex
	levels        map[string]float64
	current       string
	currentSince  time.Time
	thresholdDBFS float64
	hysteresis    time.Duration
}

// New: thresholdDBFS — например -45; hysteresis — например 300ms.
func New(thresholdDBFS float64, hysteresis time.Duration) *Detector {
	return &Detector{
		levels:        map[string]float64{},
		thresholdDBFS: thresholdDBFS,
		hysteresis:    hysteresis,
	}
}

func (d *Detector) Push(s Sample) (newSpeaker string, changed bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.levels[s.PeerID] = s.DBFS
	loudest, level := "", -math.MaxFloat64
	for id, l := range d.levels {
		if l > level {
			loudest, level = id, l
		}
	}
	if level < d.thresholdDBFS {
		return "", false
	}
	if loudest == d.current {
		return "", false
	}
	if !d.currentSince.IsZero() && time.Since(d.currentSince) < d.hysteresis {
		return "", false
	}
	d.current = loudest
	d.currentSince = time.Now()
	return loudest, true
}

func (d *Detector) Forget(peerID string) {
	d.mu.Lock()
	delete(d.levels, peerID)
	if d.current == peerID {
		d.current = ""
		d.currentSince = time.Now()
	}
	d.mu.Unlock()
}

func (d *Detector) Current() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.current
}
