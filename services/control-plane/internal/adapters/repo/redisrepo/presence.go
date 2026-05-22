package redisrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Presence struct{ rdb *redis.Client }

func NewPresence(rdb *redis.Client) *Presence { return &Presence{rdb: rdb} }

type Participant struct {
	PeerID      string    `json:"peerId"`
	UserID      string    `json:"userId,omitempty"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	Transport   string    `json:"transport"`
	Muted       bool      `json:"muted"`
	VideoOff    bool      `json:"videoOff"`
	JoinedAt    time.Time `json:"joinedAt"`
}

func roomKey(id uuid.UUID) string { return fmt.Sprintf("room:%s:participants", id) }

func (p *Presence) Add(ctx context.Context, roomID uuid.UUID, peer Participant) error {
	b, err := json.Marshal(peer)
	if err != nil {
		return err
	}
	return p.rdb.HSet(ctx, roomKey(roomID), peer.PeerID, b).Err()
}

func (p *Presence) Remove(ctx context.Context, roomID uuid.UUID, peerID string) error {
	return p.rdb.HDel(ctx, roomKey(roomID), peerID).Err()
}

func (p *Presence) List(ctx context.Context, roomID uuid.UUID) ([]Participant, error) {
	res, err := p.rdb.HGetAll(ctx, roomKey(roomID)).Result()
	if err != nil {
		return nil, err
	}
	out := make([]Participant, 0, len(res))
	for _, v := range res {
		var pp Participant
		if err := json.Unmarshal([]byte(v), &pp); err == nil {
			out = append(out, pp)
		}
	}
	return out, nil
}

func (p *Presence) Update(ctx context.Context, roomID uuid.UUID, peerID string, patch map[string]any) error {
	cur, err := p.rdb.HGet(ctx, roomKey(roomID), peerID).Result()
	if err != nil {
		return err
	}
	var pp map[string]any
	if err := json.Unmarshal([]byte(cur), &pp); err != nil {
		return err
	}
	for k, v := range patch {
		pp[k] = v
	}
	b, _ := json.Marshal(pp)
	return p.rdb.HSet(ctx, roomKey(roomID), peerID, b).Err()
}

func (p *Presence) Clear(ctx context.Context, roomID uuid.UUID) error {
	return p.rdb.Del(ctx, roomKey(roomID)).Err()
}
