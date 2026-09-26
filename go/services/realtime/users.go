package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Presence — the redis-backed connected-users state, 1:1 with the Node
// real-time ConnectedUsersManager (same keys, same TTLs, same result shape):
//
//	clients_in_project:{pid}                 SET    member = publicId, TTL 4d
//	connected_user:{pid}:{publicId}          HASH   last_updated_at(ms), user_id,
//	                                          first_name, last_name, email,
//	                                          cursorData(JSON), TTL 15min
//	projectNotEmptySince:{pid}               STRING epoch-seconds, SET NX EX 31d
//
// getConnectedUsers filters client_age < 10s (REFRESH_TIMEOUT) — the reason
// the get flow refreshes all live room clients first and delays 1s.

const (
	userTTLSeconds     = 900            // ONE_HOUR/4
	projectSetTTL      = 96 * time.Hour // 4 days
	refreshTimeout     = 10 * time.Second
	projectNotEmptyTTL = 31 * 24 * time.Hour
)

func keyClientsInProject(pid string) string { return "clients_in_project:{" + pid + "}" }
func keyConnectedUser(pid, pub string) string {
	return "connected_user:{" + pid + "}:" + pub
}
func keyProjectNotEmptySince(pid string) string { return "projectNotEmptySince:{" + pid + "}" }

// RedisLike — the narrow ioredis surface Presence drives (go-redis v9's
// *redis.Client satisfies it; tests use an in-memory fake).
type RedisLike interface {
	SAdd(ctx context.Context, key string, member interface{}) (int64, error)
	SRem(ctx context.Context, key string, member interface{}) (int64, error)
	SMembers(ctx context.Context, key string) ([]string, error)
	SCard(ctx context.Context, key string) (int64, error)
	Expire(ctx context.Context, key string, d time.Duration) (bool, error)
	HSet(ctx context.Context, key string, field string, value interface{}) (int64, error)
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	Del(ctx context.Context, key string) (int64, error)
	Get(ctx context.Context, key string) (string, error)
	GetDel(ctx context.Context, key string) (string, error)
	SetNX(ctx context.Context, key string, value interface{}, ttl time.Duration) (bool, error)
}

type presence struct{ r RedisLike }

// presenceUser — the identity fields stored (session user).
type presenceUser struct {
	ID    string
	First string
	Last  string
	Email string
}

// cursorFields — clientTracking.updatePosition payload (Node schema:
// {row?, column?, doc_id?}).
type cursorFields struct {
	DocID  string
	Row    *float64
	Column *float64
}

func (p *presence) markConnected(ctx context.Context, pid, pub string, u presenceUser) error {
	kp := keyClientsInProject(pid)
	if _, err := p.r.SAdd(ctx, kp, pub); err != nil {
		return err
	}
	if _, err := p.r.Expire(ctx, kp, projectSetTTL); err != nil {
		return err
	}
	kc := keyConnectedUser(pid, pub)
	now := time.Now()
	if _, err := p.r.HSet(ctx, kc, "last_updated_at", fmt.Sprint(now.UnixMilli())); err != nil {
		return err
	}
	if _, err := p.r.HSet(ctx, kc, "user_id", u.ID); err != nil {
		return err
	}
	if _, err := p.r.HSet(ctx, kc, "first_name", u.First); err != nil {
		return err
	}
	if _, err := p.r.HSet(ctx, kc, "last_name", u.Last); err != nil {
		return err
	}
	if _, err := p.r.HSet(ctx, kc, "email", u.Email); err != nil {
		return err
	}
	_, err := p.r.Expire(ctx, kc, userTTLSeconds*time.Second)
	return err
}

// updatePosition — markConnected fields + cursorData (Node updateUserPosition
// with a cursor payload).
func (p *presence) updatePosition(ctx context.Context, pid, pub string, u presenceUser, c cursorFields) error {
	if err := p.markConnected(ctx, pid, pub, u); err != nil {
		return err
	}
	cur := map[string]any{"row": c.Row, "column": c.Column}
	if c.DocID != "" {
		cur["doc_id"] = c.DocID
	}
	b, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	kc := keyConnectedUser(pid, pub)
	if _, err := p.r.HSet(ctx, kc, "cursorData", string(b)); err != nil {
		return err
	}
	_, err = p.r.Expire(ctx, kc, userTTLSeconds*time.Second)
	return err
}

func (p *presence) refresh(ctx context.Context, pid, pub string) {
	kc := keyConnectedUser(pid, pub)
	_, _ = p.r.HSet(ctx, kc, "last_updated_at", fmt.Sprint(time.Now().UnixMilli()))
	_, _ = p.r.Expire(ctx, kc, userTTLSeconds*time.Second)
}

// disconnect — SREM + DEL + projectNotEmptySince bookkeeping (metrics drops
// aside, the key behavior is 1:1).
func (p *presence) disconnect(ctx context.Context, pid, pub string) error {
	kp := keyClientsInProject(pid)
	if _, err := p.r.SRem(ctx, kp, pub); err != nil {
		return err
	}
	if _, err := p.r.Expire(ctx, kp, projectSetTTL); err != nil {
		return err
	}
	if _, err := p.r.Del(ctx, keyConnectedUser(pid, pub)); err != nil {
		return err
	}
	var n int64
	if cn, err := p.r.SCard(ctx, kp); err == nil {
		n = cn
	}
	if n == 0 {
		_, _ = p.r.GetDel(ctx, keyProjectNotEmptySince(pid))
	} else {
		nowSec := time.Now().Unix()
		_, _ = p.r.SetNX(ctx, keyProjectNotEmptySince(pid), fmt.Sprint(nowSec), projectNotEmptyTTL)
	}
	return nil
}

// connectedUser — the getConnectedUsers result element (Node _getConnectedUser
// output shape, pinned by the live capture):
//
//	{last_updated_at, user_id, first_name, last_name, email,
//	 connected: true, client_id, client_age, cursorData?{row,column,doc_id}}
type connectedUser map[string]any

func (p *presence) getConnected(ctx context.Context, pid string) ([]connectedUser, error) {
	members, err := p.r.SMembers(ctx, keyClientsInProject(pid))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := []connectedUser{}
	for _, pubID := range members {
		h, err := p.r.HGetAll(ctx, keyConnectedUser(pid, pubID))
		if err != nil {
			return nil, err
		}
		if h["user_id"] == "" {
			continue // ghost: connected:false path is filtered out below
		}
		var lastMS int64
		fmt.Sscanf(h["last_updated_at"], "%d", &lastMS)
		age := now.Sub(time.UnixMilli(lastMS))
		if age < 0 || age > refreshTimeout {
			continue // stale (REFRESH_TIMEOUT filter)
		}
		u := connectedUser{
			"connected":       true,
			"client_id":       pubID,
			"client_age":      age.Seconds(),
			"user_id":         h["user_id"],
			"first_name":      h["first_name"],
			"last_name":       h["last_name"],
			"email":           h["email"],
			"last_updated_at": h["last_updated_at"],
		}
		if cd, ok := h["cursorData"]; ok && cd != "" {
			var cur map[string]any
			if json.Unmarshal([]byte(cd), &cur) == nil {
				u["cursorData"] = cur
			}
		}
		out = append(out, u)
	}
	return out, nil
}

func (p *presence) count(ctx context.Context, pid string) (int64, error) {
	return p.r.SCard(ctx, keyClientsInProject(pid))
}

// memRedis — in-memory RedisLike for tests (synchronous, deterministic).
type memRedis struct {
	mu      sync.Mutex
	sets    map[string]map[string]bool
	hashes  map[string]map[string]string
	strings map[string]string
}

func newMemRedis() *memRedis {
	return &memRedis{
		sets:    map[string]map[string]bool{},
		hashes:  map[string]map[string]string{},
		strings: map[string]string{},
	}
}

func (m *memRedis) SAdd(_ context.Context, key string, member interface{}) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sets[key]
	if !ok {
		s = map[string]bool{}
		m.sets[key] = s
	}
	if s[fmt.Sprint(member)] {
		return 0, nil
	}
	s[fmt.Sprint(member)] = true
	return 1, nil
}
func (m *memRedis) SRem(_ context.Context, key string, member interface{}) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sets[key]
	if s[fmt.Sprint(member)] {
		delete(s, fmt.Sprint(member))
		return 1, nil
	}
	return 0, nil
}
func (m *memRedis) SMembers(_ context.Context, key string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	v := m.sets[key]
	if v == nil {
		return nil, nil
	}
	for k := range v {
		out = append(out, k)
	}
	return out, nil
}
func (m *memRedis) SCard(_ context.Context, key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.sets[key])), nil
}
func (m *memRedis) Expire(context.Context, string, time.Duration) (bool, error) { return true, nil }
func (m *memRedis) HSet(_ context.Context, key, field string, value interface{}) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hashes[key]
	if !ok {
		h = map[string]string{}
		m.hashes[key] = h
	}
	if _, ok := h[field]; ok {
		h[field] = fmt.Sprint(value)
		return 0, nil
	}
	h[field] = fmt.Sprint(value)
	return 1, nil
}
func (m *memRedis) HGetAll(_ context.Context, key string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for k, v := range m.hashes[key] {
		out[k] = v
	}
	return out, nil
}
func (m *memRedis) Del(_ context.Context, key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := int64(0)
	if delete(m.sets, key); true {
		n++
	}
	if delete(m.hashes, key); true {
		n++
	}
	if delete(m.strings, key); true {
		n++
	}
	return n, nil
}
func (m *memRedis) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.strings[key]
	if !ok {
		return "NOKEY", nil // ioredis redis.Nil sentinel
	}
	return v, nil
}
func (m *memRedis) GetDel(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.strings[key]
	delete(m.strings, key)
	if !ok {
		return "NOKEY", nil
	}
	return v, nil
}
func (m *memRedis) SetNX(_ context.Context, key string, value interface{}, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.strings[key]; ok {
		return false, nil
	}
	m.strings[key] = fmt.Sprint(value)
	return true, nil
}
