// Aliases — Redis-репозиторий маппинга человекочитаемого имени комнаты в её UUID.
// Нужно для SIP: терминал звонит на room1@host, sip-gateway/media-worker резолвят
// "room1" → UUID реальной комнаты, чтобы SIP-пир попал к WebRTC-участникам.
package redisrepo

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Aliases struct{ rdb *redis.Client }

func NewAliases(rdb *redis.Client) *Aliases { return &Aliases{rdb: rdb} }

func aliasKey(name string) string {
	return "vks4:room:alias:" + strings.ToLower(strings.TrimSpace(name))
}

func (a *Aliases) Set(ctx context.Context, name string, id uuid.UUID) error {
	if a == nil || a.rdb == nil || name == "" {
		return nil
	}
	return a.rdb.Set(ctx, aliasKey(name), id.String(), 0).Err()
}

func (a *Aliases) Delete(ctx context.Context, name string) error {
	if a == nil || a.rdb == nil || name == "" {
		return nil
	}
	return a.rdb.Del(ctx, aliasKey(name)).Err()
}

func (a *Aliases) Resolve(ctx context.Context, name string) (string, error) {
	if a == nil || a.rdb == nil || name == "" {
		return "", redis.Nil
	}
	return a.rdb.Get(ctx, aliasKey(name)).Result()
}
