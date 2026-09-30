// Package core is the foundation of the Go web service (WEB_GO_PLAN.md P0).
//
// It reproduces the runtime contract of the Node/Express services/web app:
// same session cookies, same Redis session store, same CSRF scheme, same
// response shapes (status, headers, ETags, bodies) — pinned from the Node
// sources (express-session 1.17.2, connect-redis 6.1.3, csurf 1.11.0,
// csrf 3.1.0, cookie-signature 1.0.6, etag 1.8.1, express 4.22) plus
// empirical probes against the live stacks (pins recorded in
// WEB_GO_PLAN.md).
package core

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"context"
	"github.com/redis/go-redis/v9"
)

// ---------- crypto primitives (Node-parity) ----------

// urlB64 is base64 with + → -, / → _, and '=' stripped (base64url, no
// padding), exactly as cookie-signature@1.0.6 sign(), csrf@3.1.0 hash()
// and uid-safe@2.1.5 apply it.
var urlB64 = base64.RawURLEncoding

// signCookie mirrors cookie-signature@1.0.6:
//
//	sign(val, secret) = val + '.' + base64_nopad(HMAC-SHA256(key=secret, msg=val))
//
// Pinned empirically against the live overleaf.sid cookie (2026-09-16):
// the suffix is 43 base64url chars (32 bytes), not the 64-hex of older
// cookie-signature.
// signCookie mirrors the EXACT cookie-signature algorithm this stack
// resolves (pinned from the live e2e cookie, 2026-09):
//
//	sign(val, secret) = val + "." + base64(HMAC-SHA256(key=secret, msg=val))
//	                      with trailing '=' padding stripped
//
// NOTE: STD base64 (+ and / included — the `cookie` serializer then
// URI-encodes them in the Set-Cookie header, which is byte-equivalent as
// far as every conforming client is concerned), NOT base64url.
func signCookie(val, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(val))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return val + "." + strings.TrimRight(sig, "=")
}

// unsignCookie mirrors cookie-signature@1.0.6 unsign(): the candidate
// pre-dot value must sign-check against one of the secrets (timing-safe).
func unsignCookie(val string, secrets []string) string {
	i := strings.LastIndex(val, ".")
	if i < 0 {
		return ""
	}
	candidate := val[:i]
	target := val
	for _, s := range secrets {
		if s == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(signCookie(candidate, s)), []byte(target)) == 1 {
			return candidate
		}
	}
	return ""
}

// newRandomBase64Url is N crypto-random bytes → base64url without padding
// (the uid-safe@2.1.5 charset: A-Za-z0-9, '-', '_'). N=24 → 32 chars
// (the observed session ID length); N=18 → 24 chars (the observed
// csrfSecret length).
func newRandomBase64Url(n int) string {
	buf := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		panic(fmt.Sprintf("crypto random: %v", err))
	}
	return urlB64.EncodeToString(buf)
}

// csrfSaltChars is the rndm@1.2.0 base62 charset used for token salts.
var csrfSaltChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func newCsrfSalt() string {
	s := make([]byte, 8)
	for i := range s {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(csrfSaltChars))))
		if err != nil {
			panic(err)
		}
		s[i] = csrfSaltChars[n.Int64()]
	}
	return string(s)
}

// csrfHash is csrf@3.1.0's hash(): SHA1, base64, then + → -, / → _, '='
// stripped (update(str,'ascii') — for our A-Za-z0-9 inputs plain UTF-8).
func csrfHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return urlB64.EncodeToString(sum[:])
}

// CsrfToken mirrors csrf@3.1.0:
//
//	token = salt(8 base62 chars) + '-' + base64url_nopad(SHA1(salt + '-' + secret))
//
// i.e. 8 + 1 + 27 = 36 chars, exactly the live <meta name="ol-csrfToken">
// shape (pinned 2026-09-16).
func CsrfToken(secret string) string {
	salt := newCsrfSalt()
	return salt + "-" + csrfHash(salt+"-"+secret)
}

// VerifyCsrfToken mirrors csrf@3.1.0 Tokens.verify (split on the FIRST
// '-', recompute, timing-safe compare).
func VerifyCsrfToken(secret, token string) bool {
	if secret == "" || token == "" {
		return false
	}
	i := strings.IndexByte(token, '-')
	if i < 0 {
		return false
	}
	salt := token[:i]
	if len(salt) != 8 {
		return false
	}
	expected := salt + "-" + csrfHash(salt+"-"+secret)
	if len(expected) != len(token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(token)) == 1
}

// NewCsrfSecret builds a fresh csrfSecret: 18 random bytes → 24 base64url
// chars — the uid-safe(18) shape stored in session.csrfSecret by csurf.
func NewCsrfSecret() string { return newRandomBase64Url(18) }

// EtagWeakBody mirrors etag@1.8.1 entitytag() with weak=true (express
// default):
//
//	W/"<hex(byteLength)>-<base64(SHA1(body))[:27]>"
//
// Pinned empirically: GET /status → ETag W/"12-1XIqlzCgkZ5qAye222Y2URlsyEU"
// where 0x12 == 18 == len("web is alive (web)").
func EtagWeakBody(body string) string {
	sum := sha1.Sum([]byte(body))
	raw := base64.StdEncoding.EncodeToString(sum[:])
	if len(raw) > 27 {
		raw = raw[:27]
	}
	return fmt.Sprintf("W/%q", strconv.FormatInt(int64(len([]byte(body))), 16)+"-"+raw)
}

// ---------- Redis client (go-redis/v9) ----------
//
// Owner directive D (2026-09-30) + audit C3: the old hand-rolled RESP2
// client (single net.Conn behind one sync.Mutex, no I/O deadlines,
// Close-without-lock race, SCAN truncated at 10000 iterations) is
// replaced by github.com/redis/go-redis/v9 — connection pool,
// dial/read/write deadlines, race-free Close. The PUBLIC SURFACE
// (type RedisClient, the struct-literal fields Addr/Password/DB,
// DialRedis, and every method) is unchanged, so the session store,
// rate limiter and all callers are untouched. Lazy construction is
// preserved (cmd/web keeps booting when redis is down and re-dials per
// command — the shadow-service contract).

// RedisError — kept for API compatibility with code compiled against the
// old client; the go-redis-backed implementation returns go-redis error
// types (redis.Nil for missing keys) instead of this one.
type RedisError struct{ Msg string }

func (e *RedisError) Error() string { return "redis: " + e.Msg }

// RedisClient — go-redis/v9-backed wrapper with the historical surface.
type RedisClient struct {
	// Addr is the redis endpoint (host:port). Retained for the public
	// struct-literal contract (&RedisClient{Addr: ...}) and lazy rebuild.
	Addr string
	// Password mirrors the Node REDIS_PASSWORD contract (AUTH).
	Password string
	// DB is the logical database index (0 = default, Node REDIS_DB).
	DB string

	mu sync.Mutex
	rc *redis.Client
}

// newRedisOptions — audit C3: the OLD client had NO I/O deadlines;
// go-redis gets explicit dial/read/write bounds so a hung redis degrades
// requests instead of stalling the process. DisableIndentity keeps the
// handshake at bare AUTH/SELECT (interoperable with the minimal RESP
// test fakes, no CLIENT SETINFO).
func newRedisOptions(addr, password, db string) *redis.Options {
	o := &redis.Options{
		Addr:             addr,
		Password:         password,
		DisableIndentity: true,
		DialTimeout:      5 * time.Second,
		ReadTimeout:      10 * time.Second,
		WriteTimeout:     10 * time.Second,
	}
	if db != "" && db != "0" {
		if n, err := strconv.Atoi(db); err == nil {
			o.DB = n
		}
	}
	return o
}

// ensureClient lazily builds the go-redis client (shadow-service
// contract: a failed boot dial must not kill the service — each command
// retries). Safe for concurrent use.
func (r *RedisClient) ensureClient() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rc != nil {
		return nil
	}
	if r.Addr == "" {
		return &RedisError{Msg: "redis addr unset"}
	}
	r.rc = redis.NewClient(newRedisOptions(r.Addr, r.Password, r.DB))
	return nil
}

// DialRedis builds the client and PINGs it. A failure here does NOT mean
// the service must die (cmd/web falls back to a lazy RedisClient).
func DialRedis(addr string) (*RedisClient, error) {
	rc := redis.NewClient(newRedisOptions(addr, "", ""))
	err := rc.Ping(context.Background()).Err()
	if err == nil {
		return &RedisClient{Addr: addr, rc: rc}, nil
	}
	_ = rc.Close()
	return nil, err
}

// Close releases the pool (race-free in go-redis, unlike the old
// Close-without-mutex — audit M4).
func (r *RedisClient) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rc == nil {
		return nil
	}
	e := r.rc.Close()
	r.rc = nil
	return e
}

// raw — generic command path (kept for the historical `command()` call
// shape used by core tests; maps to go-redis Do).
func (r *RedisClient) raw(ctx context.Context, args ...any) (any, error) {
	if err := r.ensureClient(); err != nil {
		return nil, err
	}
	return r.rc.Do(ctx, args...).Result()
}

// command — historical unexported surface (core test uses PING through
// it); delegates to the pooled client.
func (r *RedisClient) command(args ...string) (any, error) {
	ctx := context.Background()
	if err := r.ensureClient(); err != nil {
		return nil, err
	}
	return r.rc.Do(ctx, toAny(args)...).Result()
}

// PING implements the node-redis healthCheck().
func (r *RedisClient) PING() error {
	if err := r.ensureClient(); err != nil {
		return err
	}
	pong, err := r.rc.Ping(context.Background()).Result()
	if err != nil {
		return err
	}
	if pong != "PONG" {
		return &RedisError{Msg: "expected PONG, got " + pong}
	}
	return nil
}

func (r *RedisClient) require() (*redis.Client, error) {
	if err := r.ensureClient(); err != nil {
		return nil, err
	}
	return r.rc, nil
}

// SADD adds members to a set (Node rclient.sadd / multi.sadd).
func (r *RedisClient) SADD(key string, members ...string) error {
	c, err := r.require()
	if err != nil {
		return err
	}
	_, err = c.SAdd(context.Background(), key, toAny(members)...).Result()
	return err
}

// SREM removes members from a set (Node rclient.srem — variadic).
func (r *RedisClient) SREM(key string, members ...string) error {
	c, err := r.require()
	if err != nil {
		return err
	}
	_, err = c.SRem(context.Background(), key, toAny(members)...).Result()
	return err
}

// SMEMBERS returns the full set (redis hash order, as the Node client saw).
func (r *RedisClient) SMEMBERS(key string) ([]string, error) {
	c, err := r.require()
	if err != nil {
		return nil, err
	}
	return c.SMembers(context.Background(), key).Result()
}

// PEXPIRE sets a millisecond TTL (Node rclient.pexpire).
func (r *RedisClient) PEXPIRE(key string, ms int64) error {
	c, err := r.require()
	if err != nil {
		return err
	}
	_, err = c.PExpire(context.Background(), key, time.Duration(ms)*time.Millisecond).Result()
	return err
}

// GET — (value, found, err); redis.Nil maps to found=false.
func (r *RedisClient) GET(key string) (string, bool, error) {
	c, err := r.require()
	if err != nil {
		return "", false, err
	}
	v, err := c.Get(context.Background(), key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// SETNXEX is SET key value NX EX seconds (connect-redis 'NX' path).
// Byte-identical wire format to the OLD client (uppercase NX/EX, seconds
// unit) — pinned by the core fake + tests. Nil-bulk reply (key already
// existed) is the OLD client's SUCCESS no-op (readReply mapped "$-1" →
// (nil, nil)), NOT an error — preserved: the session store's XX-then-NX
// double-save relies on it.
func (r *RedisClient) SETNXEX(key, value string, ttl time.Duration) error {
	ok, err := r.setNXEXReply(key, value, ttl)
	if err == redis.Nil {
		return nil // key already existed — no-op success (Node parity)
	}
	if err != nil {
		return err
	}
	_ = ok
	return nil
}

// SETNXEXReply — true ⇒ set now; false ⇒ already existed (nil reply).
func (r *RedisClient) SETNXEXReply(key, value string, ttl time.Duration) (bool, error) {
	ok, err := r.setNXEXReply(key, value, ttl)
	if err == redis.Nil {
		return false, nil
	}
	return ok, err
}

func (r *RedisClient) setNXEXReply(key, value string, ttl time.Duration) (bool, error) {
	c, err := r.require()
	if err != nil {
		return false, err
	}
	res, err := c.Do(context.Background(), "SET", key, value, "NX", "EX", ttlSeconds(ttl)).Result()
	if err == redis.Nil {
		return false, redis.Nil
	}
	if err != nil {
		return false, err
	}
	return res == "OK", nil
}

// SETXXEX is SET key value XX EX seconds (in-place update of an
// already-existing session). Nil-bulk reply (key absent) is the OLD
// client's SUCCESS no-op — preserved (persist() then falls back to NX).
func (r *RedisClient) SETXXEX(key, value string, ttl time.Duration) error {
	c, err := r.require()
	if err != nil {
		return err
	}
	_, err = c.Do(context.Background(), "SET", key, value, "XX", "EX", ttlSeconds(ttl)).Result()
	if err == redis.Nil {
		return nil // key absent — no-op success (Node parity)
	}
	return err
}

func (r *RedisClient) DEL(key string) error {
	c, err := r.require()
	if err != nil {
		return err
	}
	_, err = c.Del(context.Background(), key).Result()
	return err
}

// Publish — redis PUBLISH (system-message refresh fan-out).
func (r *RedisClient) Publish(channel, message string) error {
	c, err := r.require()
	if err != nil {
		return err
	}
	_, err = c.Publish(context.Background(), channel, message).Result()
	return err
}

// TTL — remaining seconds (redis semantics: -1 no TTL, -2 missing key).
func (r *RedisClient) TTL(key string) (int64, error) {
	c, err := r.require()
	if err != nil {
		return 0, err
	}
	d, err := c.TTL(context.Background(), key).Result()
	if err == redis.Nil {
		return -2, nil
	}
	if err != nil {
		return 0, err
	}
	if d < 0 {
		// -1s / -2s → -1 / -2 (redis integer semantics)
		if d <= -time.Second && d > -2*time.Second {
			return -1, nil
		}
		return -2, nil
	}
	return int64(d.Round(time.Second) / time.Second), nil
}

// INCR / INCRBY — rate-limit family.
func (r *RedisClient) INCR(key string) (int64, error) { return r.INCRBY(key, 1) }

func (r *RedisClient) INCRBY(key string, n int64) (int64, error) {
	c, err := r.require()
	if err != nil {
		return 0, err
	}
	return c.IncrBy(context.Background(), key, n).Result()
}

// EXPIRE — rate-limit window.
func (r *RedisClient) EXPIRE(key string, sec int64) error {
	c, err := r.require()
	if err != nil {
		return err
	}
	_, err = c.Expire(context.Background(), key, time.Duration(sec)*time.Second).Result()
	return err
}

// SCAN — full cursor loop (audit C3: the OLD 10000-iteration cap silently
// truncated the keyspace; go-redis walks to cursor 0).
func (r *RedisClient) SCAN(match string, count int) (keys []string, err error) {
	c, err := r.require()
	if err != nil {
		return nil, err
	}
	it := c.Scan(context.Background(), 0, match, int64(count)).Iterator()
	for it.Next(context.Background()) {
		keys = append(keys, it.Val())
	}
	return keys, it.Err()
}

// ---- small helpers ----

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func ttlSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(d / time.Second)
}
