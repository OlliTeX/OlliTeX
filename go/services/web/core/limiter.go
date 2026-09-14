package core

// RateLimiter — CE parity pin from the live rate-limiter-flexible store
// (P2): redis key `rate-limit:<name>:<clientId>` holds the window count;
// INCR on every consume, EXPIRE(the duration) when the key is created.
// Over-limit calls ALSO increment (the key keeps growing — pinned by the
// 7th-request-then-wait-window battery probe). Client id is the plain IP
// for these routes (Node: `identifier: req.ip` + ipOnly).
type RateLimiter struct {
	Name   string
	Points int
	DurSec int
	Rdb    *RedisClient
}

func NewRateLimiter(rdb *RedisClient, name string, points, seconds int) *RateLimiter {
	return &RateLimiter{Name: name, Points: points, DurSec: seconds, Rdb: rdb}
}

// Consume reports whether the client may act (false = Node's 429).
func (l *RateLimiter) Consume(clientID string) bool {
	if l == nil || l.Rdb == nil {
		return true
	}
	n, err := l.Rdb.INCRBY(l.Key(clientID), 1)
	if err != nil {
		// redis outage: fail OPEN (the gate compares healthy stacks).
		return true
	}
	if n == 1 {
		_ = l.Rdb.EXPIRE(l.Key(clientID), int64(l.DurSec))
	}
	return n <= int64(l.Points)
}

// Key returns the redis key (exposed for test parity probes).
func (l *RateLimiter) Key(clientID string) string {
	return "rate-limit:" + l.Name + ":" + clientID
}

// Send429 writes Node's rate-limit 429 (pinned live 2026-09-14 P3.4: Node does
// `res.status(429); res.write('...'); res.end()` — streamed, so the response is
// chunked and carries NEITHER Content-Type NOR Content-Length).
func Send429(r *Res, msg string) {
	// Node's limiter 429 carries NO content-type header (pinned live).
	// Go's net/http would sniff text/plain for the body — an explicit
	// empty CT suppresses detection AND the header.
	r.W.Header().Set("Content-Type", "")
	// Node streams via res.write/end → chunked. Forcing chunked also removes
	// the Content-Length Go would otherwise auto-set (pinned: absent) so the
	// wire matches Node exactly (Transfer-Encoding: chunked, no CL, no CT).
	r.W.Header().Set("Transfer-Encoding", "chunked")
	r.W.WriteHeader(429)
	_, _ = r.W.Write([]byte(msg))
}
