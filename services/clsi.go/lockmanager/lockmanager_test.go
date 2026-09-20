package lockmanager

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"clsi/config"
	cerrors "clsi/errors"
	"clsi/metrics"
)

// lockEnv sets the mandatory env vars required by config.New() (see
// SANDBOXED_COMPILES_HOST_DIR...), then rebuilds the shared config singleton
// under PREEMPTIBLE=TRUE so CompileConcurrencyLimit = 32 (cheap test bound).
func lockEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PREEMPTIBLE", "TRUE")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/tmp/c")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", "/tmp/nc")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_OUTPUT", "/tmp/o")
	c := config.ForTest()
	if c.CompileConcurrencyLimit != 32 {
		t.Fatalf("expected concurrency limit 32 (PREEMPTIBLE), got %d", c.CompileConcurrencyLimit)
	}
}

// resetLocks clears the module-level lock map between tests (whitebox).
func resetLocks() {
	lockStateMu.Lock()
	LockState = map[string]*Lock{}
	lockStateMu.Unlock()
}

func TestAcquireLockAndFetch(t *testing.T) {
	lockEnv(t)
	resetLocks()

	l, err := Acquire("/compiles/p-1")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if l.Key != "/compiles/p-1" {
		t.Errorf("Key = %q", l.Key)
	}
	if !l.ExpiresAt.After(time.Now()) || !l.ExpiresAt.Before(time.Now().Add(10*time.Second)) {
		t.Errorf("ExpiresAt = %v, want ~now + %v", l.ExpiresAt, LockTimeoutMs*time.Millisecond)
	}
	if got := GetExistingLock("/compiles/p-1"); got != l {
		t.Errorf("getExistingLock = %v, want the same lock pointer", got)
	}
	if got := GetExistingLock("/compiles/other"); got != nil {
		t.Errorf("getExistingLock for fresh key = %v, want nil", got)
	}
	l.Release()
	if got := GetExistingLock("/compiles/p-1"); got != nil {
		t.Errorf("after release, getExistingLock = %v, want nil", got)
	}
}

func TestAcquireUnexpiredLockThrows(t *testing.T) {
	lockEnv(t)
	resetLocks()

	if _, err := Acquire("/compiles/p-1"); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	_, err := Acquire("/compiles/p-1")
	var already *cerrors.AlreadyCompilingError
	if !errors.As(err, &already) {
		t.Fatalf("expected AlreadyCompilingError, got %T %v", err, err)
	}
	if already.Message != "compile in progress" {
		t.Errorf("message = %q", already.Message)
	}
}

func TestAcquireExpiredLockReplaces(t *testing.T) {
	lockEnv(t)
	resetLocks()
	before := metrics.CompileLockExpired.Get()

	l, _ := Acquire("/compiles/p-1")
	l.ExpiresAt = time.Now().Add(-time.Second) // force-expire (tests)
	l2, err := Acquire("/compiles/p-1")
	if err != nil {
		t.Fatalf("acquire of expired lock: %v", err)
	}
	if l2 == l {
		t.Error("acquire of expired lock must create a FRESH lock")
	}
	if got := metrics.CompileLockExpired.Get(); got != before+1 {
		t.Errorf("compile_lock_expired = %d, want %d", got, before+1)
	}
}

func TestReleaseTwiceAndExpiredBeforeReleaseMetrics(t *testing.T) {
	lockEnv(t)
	resetLocks()
	beforeTwice := metrics.CompileLockReleasedTwice.Get()
	beforeExpired := metrics.CompileLockExpiredBeforeRelease.Get()

	l, _ := Acquire("/compiles/p-1")
	l.Release()
	// double release
	l.Release()
	if got := metrics.CompileLockReleasedTwice.Get(); got != beforeTwice+1 {
		t.Errorf("compile_lock_released_twice = %d, want %d", got, beforeTwice+1)
	}

	// expired-before-release path
	l2, _ := Acquire("/compiles/p-2")
	l2.ExpiresAt = time.Now().Add(-time.Second)
	l2.Release()
	if got := metrics.CompileLockExpiredBeforeRelease.Get(); got != beforeExpired+1 {
		t.Errorf("compile_lock_expired_before_release = %d, want %d", got, beforeExpired+1)
	}
}

func TestWaitForRelease(t *testing.T) {
	lockEnv(t)
	resetLocks()

	l, _ := Acquire("/compiles/p-1")
	done := l.WaitForRelease()
	l.Release()
	select {
	case <-done:
	default:
		t.Error("release channel not closed after Release()")
	}
}

func TestTooManyCompileRequests(t *testing.T) {
	lockEnv(t)
	resetLocks()
	limit := config.Get().CompileConcurrencyLimit
	if limit != 32 {
		t.Fatalf("expected concurrency limit 32, got %d", limit)
	}
	// checkConcurrencyLimit runs before insertion (Node), so the first
	// (limit + 2) acquires succeed and the (limit + 3)-th throws.
	for i := 0; i < limit+1; i++ {
		if _, err := Acquire(fmt.Sprintf("/compiles/p-%d", i)); err != nil {
			t.Fatalf("acquire #%d: %v", i, err)
		}
	}
	_, err := Acquire("/compiles/p-overflow")
	var tooMany *cerrors.TooManyCompileRequestsError
	if !errors.As(err, &tooMany) {
		t.Fatalf("expected TooManyCompileRequestsError, got %T %v", err, err)
	}
	if tooMany.Message != "too many concurrent compile requests" {
		t.Errorf("message = %q", tooMany.Message)
	}
	resetLocks()
}
