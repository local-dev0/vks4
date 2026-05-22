// Presence — публикация участников в Redis hash `room:{id}:participants`.
// Этот hash потом читает control-plane (GET /api/v1/rooms/{id}/participants).
package presence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

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

type Store struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Store { return &Store{rdb: rdb} }

func roomKey(id string) string { return fmt.Sprintf("room:%s:participants", id) }

func (s *Store) Add(ctx context.Context, roomID string, p Participant) error {
	if s == nil || s.rdb == nil {
		return nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return s.rdb.HSet(ctx, roomKey(roomID), p.PeerID, b).Err()
}

func (s *Store) Remove(ctx context.Context, roomID, peerID string) error {
	if s == nil || s.rdb == nil {
		return nil
	}
	return s.rdb.HDel(ctx, roomKey(roomID), peerID).Err()
}
