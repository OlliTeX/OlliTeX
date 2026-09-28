package lockmanager

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"ollitex/go/services/project-history/internal/redisx"
)

const (
	// LockTTL: seconds a lock lives (LOCK_TTL).
	LockTTL = 360

	// MinLockExtensionInterval: minimum interval between extend calls.
	MinLockExtensionInterval = 1000 * time.Millisecond

	// LockTestInterval: polling interval for GetLock (LOCK_TEST_INTERVAL).
	LockTestInterval = 50 * time.Microsecond

	// MaxLockWaitTime: give up after this long (MAX_LOCK_WAIT_TIME).
	MaxLockWaitTime = 10 * time.Second
)

// identity mirrors the Node module constants HOST/PID/RND + COUNT.
type identity struct {
	host   string
	pid    int
	random string
	count  atomic.Int64
}

func newIdentity() *identity {
	host, _ := os.Hostname()
	random, _ := newRandomHexBytes(4)
	return &identity{host: host, pid: os.Getpid(), random: random}
}

func newRandomHexBytes(n int) (string, error) {
	// No external deps: derive pseudo-entropy from pid + time nanos.
	// Uniqueness per process-lifetime is guaranteed by the pid component.
	const hexchars = "0123456789abcdef"
	b := uint64(time.Now().UnixNano())
	b ^= uint64(os.Getpid()) + 1
	out := make([]byte, n*2)
	for i := range out {
		out[i] = hexchars[int(b)&15]
		b = b<<13 ^ (b >> 7) ^ b
	}
	return string(out), nil
}

func (id *identity) LockValue() string {
	return fmt.Sprintf("locked:host=%s:pid=%d:random=%s:time=%d:count=%d",
		id.host, id.pid, id.random, time.Now().UnixMilli(), id.count.Add(1))
}

// Extend is the function passed to the runner.
type Extend func() error

// LockManager talks to the dedicated lock redis (separate client).
type LockManager struct {
	Rd      redisx.Client
	ident   *identity
	maxWait time.Duration
}

func New(rd redisx.Client) *LockManager {
	return &LockManager{Rd: rd, ident: newIdentity(), maxWait: MaxLockWaitTime}
}

// TryLock attempts to acquire the lock. Returns (value, ok, error).
func (lm *LockManager) TryLock(key string) (string, bool, error) {
	value := lm.ident.LockValue()
	got, err := lm.Rd.SetNXWithTTL(key, value, LockTTL)
	if err != nil {
		return "", false, fmt.Errorf("redis error trying to get lock: %w", err)
	}
	return value, got, nil
}

// CheckLock: returns true when the key is free (node: exists===1 → locked).
func (lm *LockManager) CheckLock(key string) (bool, error) {
	n, err := lm.Rd.Exists(key)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

// GetLock polls TryLock for maxWait. Returns (value, error).
func (lm *LockManager) GetLock(key string) (string, error) {
	start := time.Now()
	for {
		value, got, err := lm.TryLock(key)
		if err != nil {
			return "", err
		}
		if got {
			return value, nil
		}
		if time.Since(start) > lm.maxWait {
			return "", &LockTimeoutError{Key: key}
		}
		time.Sleep(LockTestInterval)
	}
}

// LockTimeoutError is returned when maxWait elapses without a lock.
type LockTimeoutError struct{ Key string }

func (e *LockTimeoutError) Error() string { return "Timeout: " + e.Key }

// Release deletes the key iff the stored value still matches this holder.
func (lm *LockManager) Release(key, value string) error {
	stored, ok, err := lm.Rd.Get(key)
	if err != nil {
		return err
	}
	if !ok || stored != value {
		// Expired or taken by someone else — nothing to delete.
		return nil
	}
	_, err = lm.Rd.Del(key)
	return err
}

// Extend re-sets the TTL iff the stored value still matches this holder.
func (lm *LockManager) Extend(key, value string) error {
	stored, ok, err := lm.Rd.Get(key)
	if err != nil {
		return err
	}
	if !ok || stored != value {
		return errors.New("failed to extend lock")
	}
	return lm.Rd.Expire(key, LockTTL)
}

// Lock is a held lock handle.
type Lock struct {
	lm          *LockManager
	key         string
	value       string
	lockTakenAt time.Time
}

func (lm *LockManager) lock(key, value string) *Lock {
	return &Lock{lm: lm, key: key, value: value, lockTakenAt: time.Now()}
}

// Extend extends the redis TTL unless held less than MinLockExtensionInterval.
func (l *Lock) Extend() error {
	if time.Since(l.lockTakenAt) < MinLockExtensionInterval {
		return nil
	}
	if err := l.lm.Extend(l.key, l.value); err != nil {
		return err
	}
	l.lockTakenAt = time.Now()
	return nil
}

func (l *Lock) Release() error {
	return l.lm.Release(l.key, l.value)
}

// RunWithLock: GetLock → run runner(extend) while holding → release.
func (lm *LockManager) RunWithLock(key string, runner func(Extend) error) (err error) {
	value, err := lm.GetLock(key)
	if err != nil {
		return err
	}
	l := lm.lock(key, value)
	defer l.Release()
	return runner(l.Extend)
}

// HealthCheck: acquire + release a per-process health lock to verify redis.
func (lm *LockManager) HealthCheck() error {
	key := fmt.Sprintf("HistoryLock:HealthCheck:host=%s:pid=%d:random=%s",
		lm.ident.host, lm.ident.pid, lm.ident.random)
	value, got, err := lm.TryLock(key)
	if err != nil {
		return err
	}
	if !got {
		return nil
	}
	return lm.Release(key, value)
}
