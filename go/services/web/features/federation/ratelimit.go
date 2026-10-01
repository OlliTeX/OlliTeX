// S2S rate limiting (Node `util/RateLimitStore.mjs`, 03 §5, 04 §6),
// enforced on the RECEIVING instance.
//
// Mirror of NC v36 `lib/private/Security/RateLimiting/Limiter.php`: one
// Redis INCR per receipt + EXPIRE on the window opener (INCR returns 1 →
// set the TTL); on exceed: 429 + `Allow-Retry-After` = remaining window
// TTL. Budgets and keys (04 §6 exact forms):
//
//	federation:ratelimit:authorize:<callerOrigin>:<localNameHash>  30 / 120 s
//	federation:ratelimit:invited:<callerOrigin>:<localNameHash>    30 / 120 s
//	federation:ratelimit:revoke:<callerOrigin>                     5  / 1200 s
//	federation:ratelimit:export:<callerOrigin>:<projectId>          10 / 120 s
//
// `<localNameHash>` = SaltedLocalNameHash over the INVITEE's wire values —
// the raw claim never lands in a Redis key (06 §6).
package federation

import (
	"time"
)

// RedisSeam — the Redis surface rate limiting + replay dedup + invite
// cache need. Production wires a `*core.RedisClient` adapter (S5); tests
// wire a fake. Signatures mirror `core.RedisClient`.
type RedisSeam interface {
	INCR(key string) (int64, error)
	EXPIRE(key string, sec int64) error
	TTL(key string) (int64, error)
	// SET key value NX EX ttl — returns (true, nil) iff the key was set now
	// (first write); (false, nil) when it already existed.
	SETEXReply(key, value string, ttl time.Duration) (bool, error)
	// SET key value EX ttl — plain (overwrite) set with TTL.
	SETEX(key, value string, ttl time.Duration) error
	GET(key string) (string, bool, error)
}

// RateLimits — the 03 §5 budget table. `revoke` is keyed by caller origin
// only (admin action, no invitee).
var RateLimits = map[string]struct {
	Budget        int
	WindowSeconds int
}{
	"authorize-invite": {Budget: 30, WindowSeconds: 120},
	"invited":          {Budget: 30, WindowSeconds: 120},
	"revoke":           {Budget: 5, WindowSeconds: 1200},
	"export-project":   {Budget: 10, WindowSeconds: 120},
}

// CheckRateLimit consumes one budget unit for an inbound S2S action
// (03 §5). Called by the S2S router AFTER assertion verification (step ⑤)
// and BEFORE dispatch (step ⑥). `retryAfterSeconds` = remaining window
// TTL (the 429 header).
func CheckRateLimit(
	r RedisSeam,
	action, callerOrigin, localNameHash, projectId string,
) (allowed bool, retryAfterSeconds int64, err error) {
	limit, ok := RateLimits[action]
	if !ok {
		// Unknown action: the envelope pre-check already refused it; do not
		// rate-limit (no budget row for it).
		return true, 0, nil
	}
	var key string
	switch action {
	case "revoke":
		key = "federation:ratelimit:revoke:" + callerOrigin
	case "export-project":
		key = "federation:ratelimit:export:" + callerOrigin + ":" + projectId
	default:
		key = "federation:ratelimit:" + action + ":" + callerOrigin + ":" + localNameHash
	}
	count, err := r.INCR(key)
	if err != nil {
		return false, 0, err
	}
	if count == 1 {
		// We opened this window — start its TTL.
		if err := r.EXPIRE(key, int64(limit.WindowSeconds)); err != nil {
			return false, 0, err
		}
	}
	if count > int64(limit.Budget) {
		ttl, err := r.TTL(key)
		if err != nil {
			return false, 0, err
		}
		if ttl < 1 {
			ttl = 1
		}
		return false, ttl, nil
	}
	return true, 0, nil
}

// ClaimJti — Redis-backed `jti` dedup (03 §3, 04 §6). Atomic one
// roundtrip: `SET key 1 EX ttl NX` (false on replay); same contract as
// @oidfed/core MemoryReplayStore (true = claimed, false = replayed).
func ClaimJti(r RedisSeam, jti string, expiresAt int64) (claimed bool, err error) {
	ttl := time.Duration(expiresAt+60-time.Now().Unix()) * time.Second
	if ttl < time.Second {
		ttl = time.Second
	}
	return r.SETEXReply("federation:replay:"+jti, "1", ttl)
}

// GetCachedInvite — A-side `invited` preview cache (03 §4.2 "60 s cached
// on caller", 04 §6): 60 s TTL, keyed
// `federation:invite-cache:<peerOrigin>:<localNameHash>`.
func GetCachedInvite(r RedisSeam, peerOrigin, localNameHash string) (string, error) {
	cached, ok, err := r.GET("federation:invite-cache:" + peerOrigin + ":" + localNameHash)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	return cached, nil
}

// SetCachedInvite — stores the preview envelope with a 60 s TTL.
func SetCachedInvite(r RedisSeam, peerOrigin, localNameHash, response string) error {
	return r.SETEX("federation:invite-cache:"+peerOrigin+":"+localNameHash, response, 60*time.Second)
}
