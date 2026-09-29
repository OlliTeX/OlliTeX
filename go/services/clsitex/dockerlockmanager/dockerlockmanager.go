// Package dockerlockmanager ports services/clsi/app/js/DockerLockManager.js
// (container-name-locked Docker operations).
//
// Node parity notes:
//
// The lock state is `const LockState = {}` (containerName -> { created:
// Date.now() }), separate from the compile-lock LockManager.
//
//   - tryLock(key, cb): if an unexpired lock (age < MAX_LOCK_HOLD_TIME) exists
//     the key is NOT acquired (cb(null, false)); an old lock (> hold time) is
//     forced ("taking old lock by force"), and the new lock value is returned
//     (cb(null, true, lockValue)).
//   - getLock(key, cb): tries immediately, then retries every
//     LOCK_TEST_INTERVAL (1s) until MAX_LOCK_WAIT_TIME (10s) elapses,
//     returning Error "Lock timeout" (with e.key set) on give-up.
//   - releaseLock(key, lockValue, cb): reference compare; only the matching
//     lock value releases the key. Mismatch logs "tried to release lock taken
//     by force" and absence logs "tried to release lock that has gone".
//   - runWithLock(key, runner, cb): getLock -> runner(releaseLock) ->
//     releaseLock -> cb(error, ...runner-args); the error is whatever the
//     runner reported (Node: error1 || error2).
package dockerlockmanager

import (
	"fmt"
	"sync"
	"time"
)

// Tunables (node: MAX_LOCK_HOLD_TIME / MAX_LOCK_WAIT_TIME / LOCK_TEST_INTERVAL).
var (
	MaxLockHoldTime  = 15000 * time.Millisecond
	MaxLockWaitTime  = 10000 * time.Millisecond
	LockTestInterval = 1000 * time.Millisecond
)

type lockStateKey struct{ containerName string }

var (
	lockStateMu sync.Mutex
	lockState   = map[string]*LockValue{}
)

// LockValue is the per-key lock holder ({ created: Date.now() }).
type LockValue struct {
	Created time.Time
}

// IsLockTimeoutError reports the timeout error from getLock/runWithLock.
func IsLockTimeoutError(err error) bool {
	_, ok := err.(*LockTimeoutError)
	return ok
}

// LockTimeoutError mirrors `new Error('Lock timeout')` with e.key === key.
type LockTimeoutError struct {
	Key string
}

func (e *LockTimeoutError) Error() string { return "Lock timeout" }

// TryLock ports tryLock: forces an old lock (> MaxLockHoldTime) and reports
// (got bool, lock).
func TryLock(key string) (bool, *LockValue) {
	lockStateMu.Lock()
	defer lockStateMu.Unlock()
	if existing := lockState[key]; existing != nil {
		if time.Since(existing.Created) < MaxLockHoldTime {
			return false, nil
		}
		// taking old lock by force (logger.error upstream)
	}
	lock := &LockValue{Created: time.Now()}
	lockState[key] = lock
	return true, lock
}

func takeLock(key string) (bool, *LockValue) { return TryLock(key) }

// GetLock polls: immediately, then every LockTestInterval until MaxLockWaitTime
// elapses (Node's setTimeout retry chain). Blocks the caller; error carries
// the key (Node: e.key = key).
func GetLock(key string, done func(err error, lock *LockValue)) {
	start := time.Now()
	for {
		if got, lock := takeLock(key); got {
			done(nil, lock)
			return
		}
		if time.Since(start) > MaxLockWaitTime {
			done(&LockTimeoutError{Key: key}, nil)
			return
		}
		time.Sleep(LockTestInterval)
	}
}

// ReleaseLock ports releaseLock: only the reference-identical lockValue
// (pointer identity in Go) releases the key.
func ReleaseLock(key string, lockValue *LockValue) {
	lockStateMu.Lock()
	defer lockStateMu.Unlock()
	existing := lockState[key]
	if existing == lockValue {
		delete(lockState, key)
	} else if existing != nil {
		// tried to release lock taken by force (logger.error upstream)
	} else {
		// tried to release lock that has gone (logger.error upstream)
	}
}

// RunWithLock ports runWithLock(): acquire the lock with retry/wait, call
// runner(release) — where release is the release closure that releases the
// key — and return the runner's error (Node: error1 || error2 = first
// non-nil error). The release closure may be invoked multiple times by the
// runner (Node logs "tried to release lock that has gone" on the 2nd, a
// no-op there too). GetLock timeout errors (with key) propagate to the
// return error.
func RunWithLock(key string, runner func(release func()) error) error {
	lock, err := getLockSync(key)
	if err != nil {
		return err
	}
	rel := func() { ReleaseLock(key, lock) }
	return runner(rel)
}

// getLockSync is a sync wrapper around the retry/wait loop (Node's
// getLock(key, callback)). Errors carry the key: *LockTimeoutError{Key}.
func getLockSync(key string) (*LockValue, error) {
	start := time.Now()
	for {
		if got, lock := takeLock(key); got {
			return lock, nil
		}
		if time.Since(start) > MaxLockWaitTime {
			return nil, &LockTimeoutError{Key: key}
		}
		time.Sleep(LockTestInterval)
	}
}

// String is for %v rendering (Node: Error#toString shows the message).
func (e *LockTimeoutError) String() string { return fmt.Sprintf("Lock timeout (key=%s)", e.Key) }
