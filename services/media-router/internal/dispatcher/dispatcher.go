// Dispatcher выбирает media-worker для комнаты.
// Стратегия первой итерации: один worker на инсталляцию → возвращает его.
// При расширении: consistent hashing по room_id + heartbeat-фильтр.
package dispatcher

import (
	"context"
	"errors"
	"hash/fnv"
	"sort"
	"time"

	"github.com/vks4/vks4/services/media-router/internal/registry"
)

type Dispatcher struct {
	reg *registry.Registry
	ttl time.Duration
}

func New(r *registry.Registry, ttl time.Duration) *Dispatcher {
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	return &Dispatcher{reg: r, ttl: ttl}
}

// Pick возвращает endpoint media-worker-а для данной комнаты.
// При первом вызове закрепляет комнату за worker-ом (sticky).
func (d *Dispatcher) Pick(ctx context.Context, roomID string) (registry.Worker, error) {
	if id, _ := d.reg.Resolve(ctx, roomID); id != "" {
		workers, err := d.reg.List(ctx)
		if err != nil {
			return registry.Worker{}, err
		}
		for _, w := range workers {
			if w.ID == id {
				return w, nil
			}
		}
		// Закреплённый worker исчез — refresh ниже
	}
	workers, err := d.reg.List(ctx)
	if err != nil {
		return registry.Worker{}, err
	}
	if len(workers) == 0 {
		return registry.Worker{}, errors.New("no workers available")
	}
	// Сортировка по нагрузке (CPU asc, затем rooms asc) — fallback при отсутствии affinity.
	sort.Slice(workers, func(i, j int) bool {
		if workers[i].CPU != workers[j].CPU {
			return workers[i].CPU < workers[j].CPU
		}
		return workers[i].Rooms < workers[j].Rooms
	})
	// Consistent hashing по room_id с топ-K кандидатами.
	pick := hashIndex(roomID, len(workers))
	chosen := workers[pick]
	if err := d.reg.Bind(ctx, roomID, chosen.ID, d.ttl); err != nil {
		return registry.Worker{}, err
	}
	return chosen, nil
}

func hashIndex(s string, n int) int {
	if n <= 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return int(h.Sum32()) % n
}
