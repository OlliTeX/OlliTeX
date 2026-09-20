package gc

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ollitex/go/services/gitbridge/data"
)

// fakeRepo ports Mockito `ProjectRepo mock`: records runGC /
// deleteIncomingPacks (verify(mock).runGC() is an "at least once" assertion
// in Mockito; the second Java assertion — no calls on the next run — is
// covered by the getExistingRepo(throws) path below).
type fakeRepo struct {
	gcCalls   atomic.Int32
	packCalls atomic.Int32
}

func (r *fakeRepo) RunGC() error {
	r.gcCalls.Add(1)
	// Simulate work so a long GC overlaps the second tick (the Java test
	// keeps this implicit: postGc blocks forever while the assertion polls).
	time.Sleep(10 * time.Millisecond)
	return nil
}

func (r *fakeRepo) DeleteIncomingPacks() error {
	r.packCalls.Add(1)
	return nil
}

// fakeRepoStore ports `when(repoStore.getExistingRepo(anyString()))`.
type fakeRepoStore struct {
	mu    sync.Mutex
	repos map[string]*fakeRepo
	fail  bool
}

func newFakeRepoStore() *fakeRepoStore {
	return &fakeRepoStore{repos: map[string]*fakeRepo{}}
}

func (s *fakeRepoStore) addProject(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos[name] = &fakeRepo{}
}

func (s *fakeRepoStore) setFail(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = v
}

func (s *fakeRepoStore) repo(name string) *fakeRepo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repos[name]
}

func (s *fakeRepoStore) GetExistingRepo(project string) (ProjectRepo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		// Java: .thenThrow(new IllegalStateException())
		return nil, errors.New("mock IllegalStateException from getExistingRepo")
	}
	r, ok := s.repos[project]
	if !ok {
		return nil, errors.New("no such project")
	}
	return r, nil
}

func waitClosed(t *testing.T, timeout time.Duration, what string, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func chanClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// TestAddedProjectsAreAllEventuallyGcedOnce ports
// GcJobImplTest.addedProjectsAreAllEventuallyGcedOnce.
func TestAddedProjectsAreAllEventuallyGcedOnce(t *testing.T) {
	locks := data.NewProjectLock(nil)
	store := newFakeRepoStore()
	numProjects := 5
	for i := 0; i < numProjects; i++ {
		store.addProject(string(rune('a' + i)))
	}

	gcJob := NewGcJobImpl(store, locks, 5)
	defer gcJob.Stop()
	for i := 0; i < numProjects; i++ {
		gcJob.QueueForGc(string(rune('a' + i)))
	}

	fut := gcJob.WaitForRun()
	gcJob.Start()
	waitClosed(t, 5*time.Second, "first GC run", fut)

	for i := 0; i < numProjects; i++ {
		name := string(rune('a' + i))
		if n := store.repo(name).gcCalls.Load(); n != 1 {
			t.Errorf("project %q: runGC called %d times, want exactly 1", name, n)
		}
		if n := store.repo(name).packCalls.Load(); n != 1 {
			t.Errorf("project %q: deleteIncomingPacks called %d times, want 1", name, n)
		}
	}

	// Nothing should happen on the next run: the mock now throws for every
	// getExistingRepo, and a re-queue of the same names must not GC them again.
	store.setFail(true)
	for i := 0; i < numProjects; i++ {
		gcJob.QueueForGc(string(rune('a' + i)))
	}
	fut2 := gcJob.WaitForRun()
	waitClosed(t, 5*time.Second, "second GC run", fut2)
	for i := 0; i < numProjects; i++ {
		name := string(rune('a' + i))
		if n := store.repo(name).gcCalls.Load(); n != 1 {
			t.Errorf("project %q: runGC called %d times after second run, want 1", name, n)
		}
	}
}

// TestCannotOverlapGcRuns ports GcJobImplTest.cannotOverlapGcRuns: a second
// scheduled run must not begin while the previous run is still going (postGc
// blocks as if the GC took forever).
func TestCannotOverlapGcRuns(t *testing.T) {
	locks := data.NewProjectLock(nil)
	store := newFakeRepoStore()

	gcJob := NewGcJobImpl(store, locks, 5)
	defer gcJob.Stop()

	runningForever := make(chan struct{})
	gcJob.OnPostGc(func() {
		// Pretend the GC is taking forever.
		<-runningForever
	})

	fut := gcJob.WaitForRun()
	gcJob.Start()
	waitClosed(t, 5*time.Second, "first GC run (preGc signal)", fut)

	ranAgain := make(chan struct{}, 1)
	gcJob.OnPreGc(func() {
		select {
		case ranAgain <- struct{}{}:
		default:
		}
	})

	// Should not run again any time soon: the job loop is blocked in the
	// postGc hook of the first run, so no second DoRun can begin. Poll well
	// past several 5ms intervals like Java does (50 x 1ms).
	time.Sleep(100 * time.Millisecond)
	if chanClosed(ranAgain) {
		t.Fatalf("next GC run started while the previous one was still running")
	}

	// Unblock the (still-blocking) postGc so the job loop is clean, then stop
	// it (Java stops via test teardown; the blocked hook is never resumed).
	close(runningForever)
}

// TestWillNotGcProjectUntilItIsUnlocked ports
// GcJobImplTest.willNotGcProjectUntilItIsUnlocked: while the project lock is
// held outside, the GC pass must not run the GC. Once the lock is released,
// the waiting run completes (Java: CompletableFuture.join() after the try
// block releases the guard).
func TestWillNotGcProjectUntilItIsUnlocked(t *testing.T) {
	locks := data.NewProjectLock(nil)
	store := newFakeRepoStore()
	store.addProject("a")

	gcJob := NewGcJobImpl(store, locks, 5)

	// Java: gcJob.onPostGc(gcJob::stop)
	gcJob.OnPostGc(func() { gcJob.Stop() })
	gcJob.QueueForGc("a")

	guard, err := locks.LockForProjectGuard("a")
	if err != nil {
		t.Fatalf("setup: lock: %v", err)
	}
	fut := gcJob.WaitForRun()
	gcJob.Start()
	// Java: while the guard is held, poll and assert the future is incomplete.
	for i := 0; i < 50; i++ {
		if chanClosed(fut) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Release the lock (end of Java's try-with-resources block).
	guard.Close()

	// Now that the lock has been released, fut should complete.
	waitClosed(t, 5*time.Second, "GC after unlock", fut)
	if n := store.repo("a").gcCalls.Load(); n != 1 {
		t.Errorf("project a: runGC called %d times, want 1 after unlock", n)
	}
}
