// Registry хранит heartbeats worker-ов в Redis (TTL).
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Worker struct {
	ID       string    `json:"id"`
	GRPCAddr string    `json:"grpcAddr"`
	CPU      float64   `json:"cpu"`
	Rooms    int       `json:"rooms"`
	LastSeen time.Time `json:"lastSeen"`
	Version  string    `json:"version"`
}

type Registry struct {
	rdb *redis.Client
	ttl time.Duration
}

func New(rdb *redis.Client, ttl time.Duration) *Registry {
	if ttl == 0 {
		ttl = 30 * time.Second
	}
	return &Registry{rdb: rdb, ttl: ttl}
}

func key(id string) string { return "mcu:worker:" + id }

func (r *Registry) Heartbeat(ctx context.Context, w Worker) error {
	w.LastSeen = time.Now()
	b, err := json.Marshal(w)
	if err != nil {
		return err
	}
	return r.rdb.Set(ctx, key(w.ID), b, r.ttl).Err()
}

func (r *Registry) List(ctx context.Context) ([]Worker, error) {
	keys, err := r.rdb.Keys(ctx, "mcu:worker:*").Result()
	if err != nil {
		return nil, err
	}
	out := make([]Worker, 0, len(keys))
	for _, k := range keys {
		v, err := r.rdb.Get(ctx, k).Result()
		if err != nil {
			continue
		}
		var w Worker
		if err := json.Unmarshal([]byte(v), &w); err == nil {
			out = append(out, w)
		}
	}
	return out, nil
}

func (r *Registry) Bind(ctx context.Context, roomID, workerID string, ttl time.Duration) error {
	return r.rdb.Set(ctx, "mcu:room:"+roomID, workerID, ttl).Err()
}

func (r *Registry) Resolve(ctx context.Context, roomID string) (string, error) {
	v, err := r.rdb.Get(ctx, "mcu:room:"+roomID).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("redis get: %w", err)
	}
	return v, nil
}
