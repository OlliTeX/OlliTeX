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
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
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

// ---------- minimal Redis (RESP2) client ----------
//
// The web app talks to Redis for sessions (connect-redis 6.1.3: key
// "sess:<sid>", JSON value, SET … NX EX for new sessions, SET … XX EX for
// in-place updates, TTL derived from cookie.expires) and rate limiting.
// No Go redis driver is available in the offline module cache, so this is
// the minimal client (RESP2, one connection guarded by a mutex).

type RedisError struct{ Msg string }

func (e *RedisError) Error() string { return "redis: " + e.Msg }

type RedisClient struct {
	mu sync.Mutex
	c  net.Conn
	br *bufio.Reader
	// Addr is the redis endpoint; retained so a dropped connection can be
	// re-established transparently (the web app tolerates a transient
	// redis outage instead of crashing — better than the Node hard exit
	// for shadow operation, and indistinguishable when redis is healthy).
	Addr string
	// Password/DB mirror the Node REDIS_PASSWORD / REDIS_DB contract
	// (AUTH then SELECT on (re)connect).
	Password string
	// DB is the logical database index (0 = default).
	DB string
}

func DialRedis(addr string) (*RedisClient, error) {
	r := &RedisClient{Addr: addr}
	if err := r.connect(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *RedisClient) connect() error {
	c, err := net.DialTimeout("tcp", r.Addr, 5*time.Second)
	if err != nil {
		return err
	}
	r.c = c
	r.br = bufio.NewReaderSize(c, 64*1024)
	if r.Password != "" {
		if _, err := r.commandNoLock("AUTH", r.Password); err != nil {
			return err
		}
	}
	if db := r.DB; db != "" && db != "0" {
		if _, err := r.commandNoLock("SELECT", db); err != nil {
			return err
		}
	}
	return nil
}

// commandNoLock runs one command WITHOUT taking the lock (used while the
// caller already holds it — connect/auth).
func (r *RedisClient) commandNoLock(args ...string) (any, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&sb, "$%d\r\n%s\r\n", len(a), a)
	}
	if _, err := io.WriteString(r.c, sb.String()); err != nil {
		r.c = nil
		return nil, err
	}
	v, err := r.readReply()
	if err != nil {
		r.c = nil
	}
	return v, err
}

func (r *RedisClient) Close() error {
	if r.c != nil {
		return r.c.Close()
	}
	return nil
}

func (r *RedisClient) command(args ...string) (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.c == nil {
		if err := r.connect(); err != nil {
			return nil, err
		}
	}
	// refresh a broken connection transparently
	var sb strings.Builder
	sb.WriteByte('*')
	sb.WriteString(strconv.Itoa(len(args)))
	sb.WriteString("\r\n")
	for _, a := range args {
		sb.WriteByte('$')
		sb.WriteString(strconv.Itoa(len(a)))
		sb.WriteString("\r\n")
		sb.WriteString(a)
		sb.WriteString("\r\n")
	}
	if _, err := io.WriteString(r.c, sb.String()); err != nil {
		r.c = nil // force re-dial on next command
		return nil, err
	}
	v, err := r.readReply()
	if err != nil {
		r.c = nil
	}
	return v, err
}

func (r *RedisClient) readReply() (any, error) {
	line, err := r.br.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if line == "" {
		return nil, &RedisError{Msg: "empty reply"}
	}
	switch line[0] {
	case '+':
		return line[1:], nil
	case '-':
		return nil, &RedisError{Msg: line[1:]}
	case ':':
		n, err := strconv.ParseInt(line[1:], 10, 64)
		return n, err
	case '$':
		n, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil // null bulk
		}
		buf := make([]byte, n+2) // trailing CRLF
		if _, err := io.ReadFull(r.br, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*':
		n, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil
		}
		out := make([]any, 0, n)
		for i := 0; i < n; i++ {
			v, err := r.readReply()
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}
	return nil, &RedisError{Msg: "bad reply: " + line}
}

// PING implements the node-redis healthCheck().
func (r *RedisClient) PING() error {
	v, err := r.command("PING")
	if err != nil {
		return err
	}
	if v != "PONG" {
		return &RedisError{Msg: "expected PONG, got " + fmt.Sprint(v)}
	}
	return nil
}

// ---------- set commands (P3.3 UserSessions parity) ----------

// SADD adds members to a set (Node rclient.sadd / multi.sadd).
func (r *RedisClient) SADD(key string, members ...string) error {
	args := append([]string{"SADD", key}, members...)
	if _, err := r.command(args...); err != nil {
		return err
	}
	return nil
}

// SREM removes members from a set (Node rclient.srem — variadic, one call
// for many members per Node's `srem(key, keysToDelete)`).
func (r *RedisClient) SREM(key string, members ...string) error {
	args := append([]string{"SREM", key}, members...)
	if _, err := r.command(args...); err != nil {
		return err
	}
	return nil
}

// SMEMBERS returns the full set (Node rclient.smembers — order: redis
// hash order; Node iterates in that order; gates compare as sets).
func (r *RedisClient) SMEMBERS(key string) ([]string, error) {
	v, err := r.command("SMEMBERS", key)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, &RedisError{Msg: "SMEMBERS: bad reply type"}
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		s, ok := e.(string)
		if !ok {
			return nil, &RedisError{Msg: "SMEMBERS: bad member type"}
		}
		out = append(out, s)
	}
	return out, nil
}

// PEXPIRE sets a millisecond TTL (Node rclient.pexpire with the
// cookieSessionLength ms pin).
func (r *RedisClient) PEXPIRE(key string, ms int64) error {
	_, err := r.command("PEXPIRE", key, strconv.FormatInt(ms, 10))
	return err
}

func (r *RedisClient) GET(key string) (string, bool, error) {
	v, err := r.command("GET", key)
	if err != nil {
		return "", false, err
	}
	if v == nil {
		return "", false, nil
	}
	s, ok := v.(string)
	return s, ok, nil
}

// SETNXEX is SET key value NX EX seconds — the connect-redis
// CustomSetRedisClient 'NX' path (initial set of a brand-new session).
func (r *RedisClient) SETNXEX(key, value string, ttl time.Duration) error {
	_, err := r.command("SET", key, value, "NX", "EX", strconv.Itoa(int(ttl/time.Second)))
	return err
}

// SETXXEX is SET key value XX EX seconds — the 'XX' path (in-place update
// of an already-existing session).
func (r *RedisClient) SETXXEX(key, value string, ttl time.Duration) error {
	_, err := r.command("SET", key, value, "XX", "EX", strconv.Itoa(int(ttl/time.Second)))
	return err
}

func (r *RedisClient) DEL(key string) error { _, err := r.command("DEL", key); return err }

// Publish implements redis PUBLISH. The Node SystemMessageManager listens
// on the 'refresh-system-messages' channel (notifyOtherPods) and refreshes
// its in-memory list cache; the Go mutations must announce the same way or
// the unflipped GET /system/messages (Node-served) goes stale.
func (r *RedisClient) Publish(channel, message string) error {
	_, err := r.command("PUBLISH", channel, message)
	return err
}

func (r *RedisClient) TTL(key string) (int64, error) {
	v, err := r.command("TTL", key)
	if err != nil {
		return 0, err
	}
	n, _ := v.(int64)
	return n, nil
}

// INCR for the rate-limit family (P2): Node's rate-limiter-flexible
// issueKeyCount = plain INCR.
func (r *RedisClient) INCR(key string) (int64, error) { return r.INCRBY(key, 1) }

// EXPIRE for the rate-limit window (P2) + redis ops parity probes.
func (r *RedisClient) EXPIRE(key string, sec int64) error {
	_, err := r.command("EXPIRE", key, strconv.FormatInt(sec, 10))
	return err
}

// SCAN (MATCH, COUNT) — removeSessionsFromRedis parity (P2): the Node
// helper scans the whole keyspace for session docs, not a prefix filter.
func (r *RedisClient) SCAN(match string, count int) (keys []string, err error) {
	cursor := "0"
	for i := 0; i < 10000; i++ {
		v, e := r.command("SCAN", cursor, "MATCH", match, "COUNT", strconv.Itoa(count))
		if e != nil {
			return nil, e
		}
		arr, ok := v.([]any)
		if !ok || len(arr) != 2 {
			return nil, fmt.Errorf("SCAN reply shape: %T", v)
		}
		c, _ := arr[0].(string)
		items, _ := arr[1].([]any)
		for _, it := range items {
			if s, ok := it.(string); ok {
				keys = append(keys, s)
			}
		}
		if c == "0" {
			return keys, nil
		}
		cursor = c
	}
	return keys, nil
}

// INCRBY for the rate-limit family (P1).
func (r *RedisClient) INCRBY(key string, n int64) (int64, error) {
	v, err := r.command("INCRBY", key, strconv.FormatInt(n, 10))
	if err != nil {
		return 0, err
	}
	i, _ := v.(int64)
	return i, nil
}
