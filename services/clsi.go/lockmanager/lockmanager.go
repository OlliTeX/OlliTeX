// Package lockmanager ports services/clsi/app/js/LockManager.js.
//
// Node parity notes:
//
// LockManager tracks project/compile directories that are being compiled.
// Module-level Map `LOCKS` keyed by the compile directory:
//
//		acquire(key):
//		  no lock present          -> create a Lock, store it, return it
//		  existing lock expired    -> logger.warn({ key }, 'Compile lock expired'),
//	                               drop the map entry, then create fresh
//		  existing lock unexpired  -> throw Errors.AlreadyCompilingError('compile in progress')
//
//		  After creation: checkConcurrencyLimit() — see below.
//
//		getExistingLock(key) -> LOCKS.get(key) (no creation).
//
// A Lock holds: key, expiresAt = now + LOCK_TIMEOUT_MS.
// LOCK_TIMEOUT_MS = config.MaxTimeout*1000 + 120000.
//
// Lock.waitForRelease(): the release promise (Node), a done-channel (Go)
// closed by release(), once.
//
// release():
//   - close releaseCh once
//   - ok = map entry exists (lock not already released)
//     not ok (double release)  -> metric 'compile_lock_released_twice'
//   - if isExpired() now       -> metric 'compile_lock_expired_before_release'
//
// checkConcurrencyLimit():
//
//	gauge 'concurrent_compile_requests' = |LOCKS|
//	size > settings.compileConcurrencyLimit ->
//	  metric 'exceeded-compilier-concurrency-limit'  (upstream typo kept)
//	  throw TooManyCompileRequestsError
package lockmanager

import (
	"sync"
	"time"

	"clsi/config"
	"clsi/errors"
	"clsi/metrics"
)

// LockTimeoutMs is the port of LOCK_TIMEOUT_MS.
const LockTimeoutMs = config.MaxTimeout*1000 + 120000

// LockState mirrors the module-level `LOCKS` Map (projectDir -> *Lock).
var LockState = map[string]*Lock{}

var lockStateMu sync.Mutex

// Lock is one held project/compile-directory lock.
type Lock struct {
	Key       string
	ExpiresAt time.Time

	mu        sync.Mutex
	releaseCh chan struct{}
	done      bool
}

func (l *Lock) isExpired() bool { return !l.ExpiresAt.After(time.Now()) }

// WaitForRelease returns the channel closed when Release is called.
// (Node: this.waitingForRelease promise; resolved on release.)
func (l *Lock) WaitForRelease() <-chan struct{} {
	l.mu.Lock()
	if l.releaseCh == nil {
		l.releaseCh = make(chan struct{})
	}
	defer l.mu.Unlock()
	return l.releaseCh
}

// Release ports lock.release() (see package doc).
func (l *Lock) Release() {
	l.mu.Lock()
	if !l.done {
		l.done = true
		if l.releaseCh != nil {
			close(l.releaseCh)
			l.releaseCh = nil
		}
	}
	l.mu.Unlock()

	lockStateMu.Lock()
	_, ok := LockState[l.Key]
	delete(LockState, l.Key)
	lockStateMu.Unlock()

	if !ok {
		metrics.IncCompileLockReleasedTwice()
	}
	if l.isExpired() {
		metrics.IncCompileLockExpiredBeforeRelease()
	}
}

// Acquire ports LockManager.acquire (see package doc).
func Acquire(key string) (*Lock, error) {
	lockStateMu.Lock()
	defer lockStateMu.Unlock()

	if current, ok := LockState[key]; ok {
		if current.isExpired() {
			metrics.IncCompileLockExpired()
			delete(LockState, key)
		} else {
			return nil, errors.NewAlreadyCompilingError("compile in progress")
		}
	}

	// Node: checkConcurrencyLimit() is called BEFORE `new Lock(key)`.
	// The gauge and the limit decision use |LOCKS| at that instant.
	if err := checkConcurrencyLimit(); err != nil {
		return nil, err
	}

	lock := &Lock{Key: key, ExpiresAt: time.Now().Add(LockTimeoutMs)}
	LockState[key] = lock
	return lock, nil
}

// GetExistingLock ports LockManager.getExistingLock.
func GetExistingLock(key string) *Lock {
	lockStateMu.Lock()
	defer lockStateMu.Unlock()
	return LockState[key]
}

// checkConcurrencyLimit (see package doc).
func checkConcurrencyLimit() error {
	size := len(LockState)
	metrics.GaugeConcurrentCompileRequests(size)
	if size <= config.Get().CompileConcurrencyLimit {
		return nil
	}
	metrics.IncExceededCompilerConcurrencyLimit()
	return errors.NewTooManyCompileRequestsError("too many concurrent compile requests")
}
