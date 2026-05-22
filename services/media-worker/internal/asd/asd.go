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

type levelEntry struct {
	dbfs float64
	when time.Time
}

type Detector struct {
	mu            sync.Mutex
	levels        map[string]levelEntry
	current       string
	currentSince  time.Time
	thresholdDBFS float64
	hysteresis    time.Duration
}

// New: thresholdDBFS — например -45; hysteresis — например 800ms.
func New(thresholdDBFS float64, hysteresis time.Duration) *Detector {
	return &Detector{
		levels:        map[string]levelEntry{},
		thresholdDBFS: thresholdDBFS,
		hysteresis:    hysteresis,
	}
}

// effectiveLevel — уровень peer с учётом времени тишины. Каждые 100мс молчания уровень
// затухает на 3 dB. Decay нужен только чтобы peer-ы не "застревали" как loudest навсегда —
// быстрый decay делает switching слишком нервным.
func effectiveLevel(e levelEntry, now time.Time) float64 {
	silentMs := now.Sub(e.when).Milliseconds()
	if silentMs <= 0 {
		return e.dbfs
	}
	decay := float64(silentMs) * 0.03 // 3 dB / 100ms
	return e.dbfs - decay
}

// Push обновляет уровень peer'а. Возвращает (newSpeaker, true) только когда кто-то ДРУГОЙ
// стал loudest и hysteresis истёк. Если все молчат — current speaker сохраняется (не сбрасывается
// в "") чтобы VAD-фрейм не дёргался на каждой паузе между словами.
func (d *Detector) Push(s Sample) (newSpeaker string, changed bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := s.When
	if now.IsZero() {
		now = time.Now()
	}
	d.levels[s.PeerID] = levelEntry{dbfs: s.DBFS, when: now}

	// Находим самого громкого среди тех, кто говорил недавно (less than 2s ago).
	loudest, level := "", -math.MaxFloat64
	for id, e := range d.levels {
		if now.Sub(e.when) > 2*time.Second {
			continue
		}
		eff := effectiveLevel(e, now)
		if eff > level {
			loudest, level = id, eff
		}
	}
	// Если никто не говорит выше порога — оставляем current speaker как есть (не сбрасываем).
	if level < d.thresholdDBFS {
		return "", false
	}
	if loudest == d.current {
		return "", false
	}
	// Hysteresis: новый speaker не сменит current, если переключение было совсем недавно.
	if !d.currentSince.IsZero() && time.Since(d.currentSince) < d.hysteresis {
		return "", false
	}
	d.current = loudest
	d.currentSince = now
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

// Increase default hysteresis: вызывающий код использует 300ms по умолчанию.
// Имеет смысл переключаться плавно — 800ms даёт ощущение фокуса без эпилептических прыжков
// когда несколько участников говорят одновременно.

func (d *Detector) Current() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.current
}
