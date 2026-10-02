package dockerlockmanager

import (
	"errors"
	"testing"
	"time"
)

func TestTryLockForceAfterHoldTime(t *testing.T) {
	got, lock := TryLock("container-a")
	if !got || lock == nil {
		t.Fatalf("TryLock first = %v %v, want true lock", got, lock)
	}
	if got, _ = TryLock("container-a"); got {
		t.Fatal("second TryLock within hold time must NOT acquire")
	}

	// Force a stale lock (created more than MaxLockHoldTime ago).
	lockStateMu.Lock()
	lockState["container-a"].Created = time.Now().Add(-MaxLockHoldTime - time.Second)
	lockStateMu.Unlock()

	if got, _ = TryLock("container-a"); !got {
		t.Fatal("stale lock must be taken by force")
	}
}

func TestReleaseLockReferenceIdentity(t *testing.T) {
	_, lock := TryLock("container-r")
	ReleaseLock("container-r", lock) // matching reference
	if got, _ := TryLock("container-r"); !got {
		t.Fatal("key must be free after release — got", got, lock)
	}
	ReleaseLock("container-r", lock) // no longer held -> "that has gone" (no-op)
}

func TestGetLockTimeoutCarriesKey(t *testing.T) {
	origWait, origInterval := MaxLockWaitTime, LockTestInterval
	MaxLockWaitTime, LockTestInterval = 50*time.Millisecond, 5*time.Millisecond
	defer func() { MaxLockWaitTime, LockTestInterval = origWait, origInterval }()

	got, _ := TryLock("container-t")
	if !got {
		t.Fatal("setup TryLock")
	}
	defer func() {
		got, _ = TryLock("container-t") // not held anymore here
		_ = got
	}()

	key := "container-t"
	var err error
	done := make(chan error, 1)
	go GetLock(key, func(err error, l *LockValue) {
		done <- err
	})
	err = <-done
	var to *LockTimeoutError
	if !errors.As(err, &to) {
		t.Fatalf("expected *LockTimeoutError, got %v", err)
	}
	if to.Key != key {
		t.Errorf("key = %q, want %q", to.Key, key)
	}
	if !IsLockTimeoutError(err) {
		t.Error("IsLockTimeoutError must match")
	}
}

func TestRunWithLockRunsAndReleases(t *testing.T) {
	// Runner releases via closure; the release closure frees the key.
	err := RunWithLock("container-w", func(release func()) error {
		release()
		got, again := TryLock("container-w")
		if !got || again == nil {
			return errors.New("runner held the lock: release via closure failed")
		}
		ReleaseLock("container-w", again)
		return nil
	})
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
}

func TestRunWithLockTimeoutPropagates(t *testing.T) {
	origWait, origInterval := MaxLockWaitTime, LockTestInterval
	MaxLockWaitTime, LockTestInterval = 40*time.Millisecond, 5*time.Millisecond
	defer func() { MaxLockWaitTime, LockTestInterval = origWait, origInterval }()

	got, _ := TryLock("container-x")
	if !got {
		t.Fatal("setup TryLock")
	}

	err := RunWithLock("container-x", func(release func()) error {
		return errors.New("unreachable")
	})
	var to *LockTimeoutError
	if !errors.As(err, &to) || to.Key != "container-x" {
		t.Fatalf("RunWithLock must return the timeout error; got %v", err)
	}
}
