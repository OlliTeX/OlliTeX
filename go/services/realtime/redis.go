package realtime

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// goRedis — adapts go-redis/v9's *redis.Client to RedisLike.
// (The Node real-time service uses ioredis via @overleaf/redis-wrapper; the
// narrow surface it relies on is exactly the RedisLike set.)
type goRedis struct {
	c *redis.Client
}

// NewGoRedis connects (mirrors the Node settings precedence:
// REAL_TIME_REDIS_HOST || REDIS_HOST || 127.0.0.1, same for port/password).
func NewGoRedis(host string, port int, password string) (*goRedis, error) {
	c := redis.NewClient(&redis.Options{
		Addr:     host + ":" + strconv.Itoa(port),
		Password: password,
	})
	if err := c.Ping(context.Background()).Err(); err != nil {
		return nil, err
	}
	return &goRedis{c: c}, nil
}

func (g *goRedis) SAdd(ctx context.Context, key string, member interface{}) (int64, error) {
	return g.c.SAdd(ctx, key, member).Result()
}
func (g *goRedis) SRem(ctx context.Context, key string, member interface{}) (int64, error) {
	return g.c.SRem(ctx, key, member).Result()
}
func (g *goRedis) SMembers(ctx context.Context, key string) ([]string, error) {
	return g.c.SMembers(ctx, key).Result()
}
func (g *goRedis) SCard(ctx context.Context, key string) (int64, error) {
	return g.c.SCard(ctx, key).Result()
}
func (g *goRedis) Expire(ctx context.Context, key string, d time.Duration) (bool, error) {
	return g.c.Expire(ctx, key, d).Result()
}
func (g *goRedis) HSet(ctx context.Context, key string, field string, value interface{}) (int64, error) {
	return g.c.HSet(ctx, key, field, value).Result()
}
func (g *goRedis) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return g.c.HGetAll(ctx, key).Result()
}
func (g *goRedis) Del(ctx context.Context, key string) (int64, error) {
	return g.c.Del(ctx, key).Result()
}
func (g *goRedis) Get(ctx context.Context, key string) (string, error) {
	return g.c.Get(ctx, key).Result()
}
func (g *goRedis) GetDel(ctx context.Context, key string) (string, error) {
	return g.c.GetDel(ctx, key).Result()
}
func (g *goRedis) SetNX(ctx context.Context, key string, value interface{}, ttl time.Duration) (bool, error) {
	return g.c.SetNX(ctx, key, value, ttl).Result()
}

// Client — raw go-redis client (for the SessionSource).
func (g *goRedis) Client() *redis.Client { return g.c }

// redisSession — SessionSource over the same redis (the Go web writes
// "sess:<sid>" docs through this store).
type redisSession struct {
	c *redis.Client
}

// NewRedisSession — build a SessionSource on the goRedis client.
func NewRedisSession(g *goRedis) SessionSource { return &redisSession{c: g.c} }

func (r *redisSession) Session(ctx context.Context, sid string) (map[string]any, error) {
	raw, err := r.c.Get(ctx, "sess:"+sid).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	out := map[string]any{}
	if err := jsonUnmarshalInto(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func jsonUnmarshalInto(raw string, v *map[string]any) error {
	return json.Unmarshal([]byte(raw), v)
}
