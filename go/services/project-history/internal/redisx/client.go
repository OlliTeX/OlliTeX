// Package redisx is a Redis client for project-history.
//
// Owner directive D (2026-09-30) + audit C3: the hand-rolled RESP-over-TCP
// implementation (one net.Conn, one mutex, no I/O deadlines, Close on a
// live connection) is replaced by github.com/redis/go-redis/v9 — pooled
// connections, dial/read/write deadlines, race-free Close. The Client
// INTERFACE (and therefore redismanager/lockmanager + their tests, which
// use fakes) is unchanged.
package redisx

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client interface — minimal set used by project-history.
type Client interface {
	Get(k string) (string, bool, error)
	Set(k, v string, ttlSeconds ...int) error
	SetNX(k, v string) (bool, error) // SET k v NX (set if absent).
	// SetNXWithTTL: SET k v EX ttl NX → true if the key is new.
	SetNXWithTTL(k, value string, ttlSeconds int) (bool, error)
	Del(keys ...string) (int64, error)  // DEL key [key ...] — multi-arg.
	Exists(keys ...string) (int, error) // EXISTS multi-arg → count.
	Expire(k string, ttlSeconds int) error
	LRange(k string, start, stop int) ([]string, error)
	LRem(k string, n int, value string) (int64, error)
	LLen(k string) (int64, error)
	// Scan iterates matching keys; limit caps results (0 = unlimited).
	Scan(pattern string, limit int) ([]string, error)
	MGet(keys ...string) ([]string, error)
	Ping() error
	Close() error
}

// Dialer — host/port/password contract from redismanager config.
type Dialer struct {
	Host     string
	Port     int
	Password string
}

func (d Dialer) Open() (Client, error) {
	c := redis.NewClient(&redis.Options{
		Addr:             d.Host + ":" + strconv.Itoa(d.Port),
		Password:         d.Password,
		DisableIndentity: true, // no CLIENT SETINFO (minimal servers/fakes)
		DialTimeout:      5 * time.Second,
		ReadTimeout:      10 * time.Second,
		WriteTimeout:     10 * time.Second,
	})
	if err := c.Ping(context.Background()).Err(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return &client{c: c}, nil
}

type client struct {
	c *redis.Client
}

func (c *client) ctx() context.Context { return context.Background() }

func (c *client) Get(k string) (string, bool, error) {
	v, err := c.c.Get(c.ctx(), k).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (c *client) Set(k, v string, ttlSeconds ...int) error {
	args := []any{"SET", k, v}
	if len(ttlSeconds) > 0 {
		args = append(args, "EX", ttlSeconds[0])
	}
	_, err := c.c.Do(c.ctx(), args...).Result()
	return err
}

// SetNX implements SET k v NX → true if the key is new, false if already present.
func (c *client) SetNX(key, val string) (bool, error) {
	v, err := c.c.Do(c.ctx(), "SET", key, val, "NX").Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	s, _ := v.(string)
	return s == "OK" || s == "ok", nil
}

// SetNXWithTTL implements SET k v EX ttl NX → true if the key is new, else
// false. This is the lock-acquire primitive: set a unique value with a
// 360s expiry, atomically, if the key does not already exist.
func (c *client) SetNXWithTTL(key, value string, ttlSeconds int) (bool, error) {
	v, err := c.c.Do(c.ctx(), "SET", key, value, "EX", ttlSeconds, "NX").Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	s, _ := v.(string)
	return s == "OK" || s == "ok", nil
}

func (c *client) Del(keys ...string) (int64, error) {
	args := make([]any, 1+len(keys))
	args[0] = "DEL"
	for i, k := range keys {
		args[i+1] = k
	}
	n, err := c.c.Do(c.ctx(), args...).Result()
	if err != nil {
		return 0, err
	}
	i, _ := n.(int64)
	return i, nil
}

func (c *client) Exists(keys ...string) (int, error) {
	args := make([]any, 1+len(keys))
	args[0] = "EXISTS"
	for i, k := range keys {
		args[i+1] = k
	}
	n, err := c.c.Do(c.ctx(), args...).Result()
	if err != nil {
		return 0, err
	}
	if i, ok := n.(int64); ok {
		return int(i), nil
	}
	return 0, nil
}

func (c *client) Expire(k string, ttl int) error {
	_, err := c.c.Do(c.ctx(), "EXPIRE", k, ttl).Result()
	return err
}

func (c *client) LRange(k string, start, stop int) ([]string, error) {
	v, err := c.c.Do(c.ctx(), "LRANGE", k, start, stop).Result()
	if err != nil {
		return nil, err
	}
	return stringsFrom(v), nil
}

func (c *client) LRem(k string, count int, value string) (int64, error) {
	n, err := c.c.Do(c.ctx(), "LREM", k, count, value).Result()
	if err != nil {
		return 0, err
	}
	i, _ := n.(int64)
	return i, nil
}

func (c *client) LLen(k string) (int64, error) {
	n, err := c.c.Do(c.ctx(), "LLEN", k).Result()
	if err != nil {
		return 0, err
	}
	i, _ := n.(int64)
	return i, nil
}

// MGet — missing keys come back as "" (the OLD client's nil→"" coercion).
func (c *client) MGet(keys ...string) ([]string, error) {
	args := make([]any, len(keys))
	for i, k := range keys {
		args[i] = k
	}
	v, err := c.c.Do(c.ctx(), append([]any{"MGET"}, args...)...).Result()
	if err != nil {
		return nil, err
	}
	return stringsFrom(v), nil
}

// Scan iterates with SCAN MATCH pattern COUNT batch, collecting matches until
// the cursor wraps or limit is reached (limit<=0 unlimited).
func (c *client) Scan(pattern string, limit int) ([]string, error) {
	seen := map[string]bool{}
	it := c.c.Scan(c.ctx(), 0, pattern, 1000).Iterator()
	for it.Next(c.ctx()) {
		seen[it.Val()] = true
		if limit > 0 && len(seen) >= limit {
			break
		}
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (c *client) Ping() error {
	return c.c.Ping(c.ctx()).Err()
}

// Close — race-free pool shutdown (go-redis), fixing the old Close-on-a-live-conn.
func (c *client) Close() error { return c.c.Close() }

// stringsFrom coerces a RESP multi-bulk reply (any slice of any) into []string,
// mapping nil elements to "" (OLD client parity: nil bulk → "").
func stringsFrom(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		if v == nil {
			return nil
		}
		return []string{}
	}
	out := make([]string, len(arr))
	for i, e := range arr {
		if s, ok := e.(string); ok {
			out[i] = s
		}
	}
	return out
}
