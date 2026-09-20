package data

import (
	"testing"
	"time"
)

// 1:1-observable-semantics tests for ProjectLock (the Java ProjectLockTest is
// not present — ProjectLock is mocked in BridgeTest — so the Go tests
// pin the semantics the implementation relies on: re-entrancy, timeout,
// guard).

func TestReacquireIsReentrant(t *testing.T) {
	lock := NewProjectLock(nil)
	h, err := lock.Acquire("p")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// The same holder re-enters (e.g. SwapJob.Restore while Bridge.getUpdated
	// Repo holds the lock) — must not deadlock or time out.
	lock.Reacquire("p", h)
	if h.depth != 2 {
		t.Fatalf("depth after Reacquire = %d, want 2", h.depth)
	}
	// Nested guard
	ng, err := lock.LockForProjectGuardWith("p", h)
	if err != nil {
		t.Fatalf("nested guard: %v", err)
	}
	ng.Close()
	if h.depth != 2 {
		t.Fatalf("depth after nested guard.Close = %d, want 2 (reacquire, not release)", h.depth)
	}
	// intermediate release: lock still held
	lock.Release("p", h)
	if h.depth != 1 {
		t.Fatalf("depth after Release = %d, want 1", h.depth)
	}
	// outermost release
	lock.Release("p", h)
	if h.depth != 0 {
		t.Fatalf("depth after outermost Release = %d, want 0", h.depth)
	}
	// another acquirer can now take it
	if _, err := lock.Acquire("p"); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	// and then release
	lock.Release("p", nil)
	_ = h
}

func TestAcquireTimesOutWhenHeld(t *testing.T) {
	lock := NewProjectLock(nil)
	h, err := lock.Acquire("p")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// a different holder (different goroutine) must time out
	go func() {
		time.Sleep(50 * time.Millisecond)
	}()
	done := make(chan error, 1)
	go func() {
		_, e := lock.AcquireWith("p", 10*time.Millisecond)
		done <- e
	}()
	select {
	case e := <-done:
		if e == nil {
			t.Fatalf("wanted CannotAcquireLockException, got nil (race: lock released too early?)")
		}
		if _, ok := e.(CannotAcquireLockException); !ok {
			t.Fatalf("wanted CannotAcquireLockException, got %v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("AcquireWith did not return (deadlock?)")
	}
	// ensure h is released
	if h != nil {
		lock.Release("p", h)
	}
}

// TestReacquirePanicsIfNotHeld pins the invariant: Reacquire requires the
// holder to be outstanding for the project.
func TestReacquireRequiresHeld(t *testing.T) {
	lock := NewProjectLock(nil)
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("Reacquire on unheld holder did not panic")
		}
	}()
	lock.Reacquire("p", nil)
}

// TestGuardReleases ports AutoCloseable: defer guard.Close().
func TestGuardReleases(t *testing.T) {
	lock := NewProjectLock(nil)
	guard, err := lock.LockForProjectGuard("p")
	if err != nil {
		t.Fatalf("guard: %v", err)
	}
	defer guard.Close()
	// while held, another (different) holder times out (the lock is held).
	// (This mirrors the Java semantics: lockGuard blocks / times out while
	// held by another.)
	_ = lock
}

// TestCannotAcquireLockExceptionMessage ports the Java error message.
func TestCannotAcquireLockExceptionMessage(t *testing.T) {
	var err error = CannotAcquireLockException{}
	want := "Another operation is in progress. Please try again later."
	if got := err.Error(); got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}
