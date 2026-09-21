package rediswrapper

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RedisLocker mirrors libraries/redis-wrapper/RedisLocker.js 1:1:
//
//	lock acquisition:  SET key <lockValue> EX <LOCK_TTL> NX  (lockValue is a
//	                      signed token — `locked:host..:pid..:random..:time..:count..`)
//	acquisition loop:  poll every 50ms, doubling up to 1s, give up after 10s
//	extend:            EVAL EXTEND_SCRIPT key lockValue LOCK_TTL  (only if we hold it)
//	release:           EVAL UNLOCK_SCRIPT key lockValue            (only if we hold it)
//	overlong SET guard: a successful SET taking > 5s auto-releases (the lock
//	                      may have expired and be held by someone else)
//
// The metrics/logger objects are the same seams as the other library ports
// (the Node peers are @overleaf/metrics + @overleaf/logger, the LIB-14/LIB-13
// Go ports will implement/consume them at that boundary).

// Metrics is the narrow @overleaf/metrics seam this library drives.
type Metrics interface {
	Inc(name string)
	Gauge(name string, value float64)
	// NewTimer mirrors `new metrics.Timer(name)` (used by RedisWebLocker).
	NewTimer(name string) Timer
}

// Timer mirrors `@overleaf/metrics` Timer (start timestamp → Done() measures).
type Timer interface{ Done() }

// NullTimer is the default no-op timer.
type NullTimer struct{}

func (NullTimer) Done() {}

// NullMetrics is the default no-op metrics implementation.
type NullMetrics struct{}

func (NullMetrics) Inc(string)            {}
func (NullMetrics) Gauge(string, float64) {}
func (NullMetrics) NewTimer(string) Timer { return NullTimer{} }

// Logger is the narrow @overleaf/logger seam (Node call sites:
// logger.info/debug/warn({info...}, 'message')).
type Logger interface {
	Debug(info map[string]any, msg string)
	Warn(info map[string]any, msg string)
	Error(info map[string]any, msg string)
}

// NullLogger is the default no-op logger.
type NullLogger struct{}

func (NullLogger) Debug(map[string]any, string) {}
func (NullLogger) Warn(map[string]any, string)  {}
func (NullLogger) Error(map[string]any, string) {}

// RedisLocker knobs (Node RedisLocker constructor assignments; ms units).

// MaxRedisRequestLength — Node MAX_REDIS_REQUEST_LENGTH (ms). A successful
// SET taking longer than this auto-releases: the lock's TTL may have
// expired and the key is no longer ours. A var (not const) so the port's
// own test-suite can shrink it.
var MaxRedisRequestLength int64 = 5000 // ms

// unlockScript / extendScript mirror the Node lua strings byte-for-byte.
const (
	unlockScript = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
	extendScript = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("expire", KEYS[1], ARGV[2]) else return 0 end`
)

// RedisLocker mirrors class RedisLocker.
type RedisLocker struct {
	Driver           Driver
	GetKey           func(id string) string
	WrapTimeoutError func(err error, id string) error
	MetricsPrefix    string
	LockTTLSeconds   int

	// Node constructor-assigned knobs (modifiable per consumer, kept as
	// fields like the Node instance properties).
	LockTestInterval time.Duration // Node LOCK_TEST_INTERVAL, ms
	MaxTestInterval  time.Duration // Node MAX_TEST_INTERVAL, ms
	MaxLockWaitTime  time.Duration // Node MAX_LOCK_WAIT_TIME, ms

	UnlockScript string
	ExtendScript string

	Metrics Metrics
	Logger  Logger
}

// RedisLockerConfig mirrors the Node `new RedisLocker({rclient, getKey,
// wrapTimeoutError, metricsPrefix, lockTTLSeconds})` argument object.
type RedisLockerConfig struct {
	RClient          Driver
	GetKey           func(id string) string
	WrapTimeoutError func(err error, id string) error
	MetricsPrefix    string
	LockTTLSeconds   int

	// Optional seams (defaults: NullMetrics / NullLogger).
	Metrics Metrics
	Logger  Logger
}

// NewRedisLocker mirrors the RedisLocker constructor, including the TTL
// validation:
//
//	if (typeof lockTTLSeconds !== 'number' || lockTTLSeconds < 30
//	 || lockTTLSeconds >= 1000)
//		throw new Error('redis lock TTL must be at least 30s and below 1000s')
//
// (the Go API is int-typed, so the Node "wrong type" case is
// unrepresentable here — the value bounds are pin-tested 1:1).
func NewRedisLocker(cfg RedisLockerConfig) (*RedisLocker, error) {
	if cfg.LockTTLSeconds < 30 || cfg.LockTTLSeconds >= 1000 {
		return nil, errors.New("redis lock TTL must be at least 30s and below 1000s")
	}
	var metrics Metrics = NullMetrics{}
	var logger Logger = NullLogger{}
	if cfg.Metrics != nil {
		metrics = cfg.Metrics
	}
	if cfg.Logger != nil {
		logger = cfg.Logger
	}
	return &RedisLocker{
		Driver:           cfg.RClient,
		GetKey:           cfg.GetKey,
		WrapTimeoutError: cfg.WrapTimeoutError,
		MetricsPrefix:    cfg.MetricsPrefix,
		LockTTLSeconds:   cfg.LockTTLSeconds,
		LockTestInterval: 50 * time.Millisecond, // Node LOCK_TEST_INTERVAL = 50
		MaxTestInterval:  1 * time.Second,       // Node MAX_TEST_INTERVAL = 1000
		MaxLockWaitTime:  10 * time.Second,      // Node MAX_LOCK_WAIT_TIME = 10000
		UnlockScript:     unlockScript,          // Node this.unlockScript = unlockScript
		ExtendScript:     extendScript,          // Node this.extendScript = extendScript
		Metrics:          metrics,
		Logger:           logger,
	}, nil
}

// randomLock mirrors Node `RedisLocker.randomLock()` (and the web locker's
// equivalent): a signed, process+call-unique token.
func randomLockToken() string {
	return fmt.Sprintf("locked:host=%s:pid=%d:random=%s:time=%d:count=%d",
		host, pid, rnd, time.Now().UnixMilli(), count.Add(1))
}

// TryLock mirrors Node tryLock(id) — one lock attempt. Returns whether the
// lock was acquired and (on success) the lock value to present to
// ExtendLock/ReleaseLock.
//
// Overlong-SET guard (Node): a successful SET taking > 5s → release it and
// report failure — the TTL may have expired and the key may now be
// someone else's.
func (l *RedisLocker) TryLock(ctx context.Context, id string) (bool, string, error) {
	lockValue := randomLockToken()
	key := l.GetKey(id)

	start := time.Now()
	ack, err := l.Driver.SetEx(ctx, key, lockValue, l.LockTTLSeconds, true)
	if err != nil {
		return false, "", err
	}
	if ack == "OK" {
		l.Metrics.Inc(l.MetricsPrefix + "-not-blocking")
		if time.Since(start).Milliseconds() > MaxRedisRequestLength {
			// the key may no longer belong to us: release and fail
			if _, relErr := l.releaseLock(ctx, id, lockValue); relErr != nil {
				return false, "", relErr
			}
			return false, "", nil
		}
		return true, lockValue, nil
	}
	l.Metrics.Inc(l.MetricsPrefix + "-blocking")
	return false, "", nil
}

// GetLock mirrors Node getLock(id) — poll (50ms→1s backoff) until acquired
// or MaxLockWaitTime elapses, then WrapTimeoutError(Error('Timeout'), id).
func (l *RedisLocker) GetLock(ctx context.Context, id string) (string, error) {
	start := time.Now()
	testInterval := l.LockTestInterval

	for {
		if time.Since(start) > l.MaxLockWaitTime {
			return "", l.WrapTimeoutError(errors.New("Timeout"), id)
		}

		got, lockValue, err := l.TryLock(ctx, id)
		if err != nil {
			return "", err
		}
		if got {
			return lockValue, nil
		}

		select {
		case <-time.After(testInterval):
		case <-ctx.Done():
			return "", ctx.Err()
		}
		// Node: setTimeout(attempt, testInterval) then double (cap 1s).
		testInterval *= 2
		if testInterval > l.MaxTestInterval {
			testInterval = l.MaxTestInterval
		}
	}
}

// CheckLock mirrors Node checkLock(id) — is the key currently locked?
// (true = free, like Node's callback(null, !exists)).
func (l *RedisLocker) CheckLock(ctx context.Context, id string) (bool, error) {
	count, err := l.Driver.Exists(ctx, l.GetKey(id))
	if err != nil {
		return false, err
	}
	if count > 0 {
		l.Metrics.Inc(l.MetricsPrefix + "-blocking")
		return false, nil
	}
	l.Metrics.Inc(l.MetricsPrefix + "-not-blocking")
	return true, nil
}

// extendLock mirrors Node extendLock(id, lockValue): EVAL EXTEND_SCRIPT;
// result !== 1 → the lock is gone (held by someone else).
func (l *RedisLocker) extendLock(ctx context.Context, id, lockValue string) error {
	key := l.GetKey(id)
	result, err := l.Driver.Eval(ctx, l.ExtendScript, []string{key}, []any{lockValue, l.LockTTLSeconds})
	if err != nil {
		return err
	}
	if resultAsInt(result) != 1 {
		l.Logger.Error(map[string]any{
			"id":           id,
			"key":          key,
			"lockValue":    lockValue,
			"redis_error":  nil,
			"redis_result": result,
		}, "extending lock error")
		l.Metrics.Inc(l.MetricsPrefix + "-extend-error")
		return errors.New("tried to extend a lock we no longer hold")
	}
	l.Metrics.Inc(l.MetricsPrefix + "-extend-success")
	return nil
}

// ExtendLock is the exported form (Node: public extendLock).
func (l *RedisLocker) ExtendLock(ctx context.Context, id, lockValue string) error {
	return l.extendLock(ctx, id, lockValue)
}

// releaseLock mirrors Node releaseLock(id, lockValue): EVAL UNLOCK_SCRIPT;
// result !== 1 → the lock already expired / was taken.
func (l *RedisLocker) releaseLock(ctx context.Context, id, lockValue string) (int64, error) {
	key := l.GetKey(id)
	result, err := l.Driver.Eval(ctx, l.UnlockScript, []string{key}, []any{lockValue})
	if err != nil {
		return 0, err
	}
	if resultAsInt(result) != 1 {
		return 0, errors.New("tried to release timed out lock")
	}
	return resultAsInt64(result), nil
}

// ReleaseLock is the exported form (Node: public releaseLock; the Node
// callback contract (error, result) maps to (result, error)).
func (l *RedisLocker) ReleaseLock(ctx context.Context, id, lockValue string) (int64, error) {
	return l.releaseLock(ctx, id, lockValue)
}

// resultAsInt / resultAsInt64 coerce ioredis EVAL results (Go drivers
// deliver integers as int64, sometimes as other numeric types) to the
// `!== 1` comparisons the Node lua scripts rely on.
func resultAsInt(result any) int64 {
	switch v := result.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	}
	return -1
}

func resultAsInt64(result any) int64 { return resultAsInt(result) }
