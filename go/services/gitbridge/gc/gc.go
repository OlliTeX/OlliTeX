// Package gc ports bridge/gc (GcJob + GcJobImpl).
//
// It is started by the bridge. Every time a project is updated we queue it for
// GC, which executes every hour or so.
//
// We don't queue it into a more immediate executor because there is no way to
// know if a call to Bridge.updateProject (which releases the lock) is going to
// call into Bridge.push. We don't want the GC to run in between an update and
// a push, so GC runs on its own timer instead.
package gc

import (
	"log/slog"
	"sync"
	"time"

	"ollitex/go/services/gitbridge/data"
)

// ProjectRepo is the minimal repo.Project surface the GC job needs
// (*repo.Project satisfies it).
type ProjectRepo interface {
	RunGC() error
	DeleteIncomingPacks() error
}

// RepoStore is the minimal FSGitRepoStore surface the GC job needs.
type RepoStore interface {
	GetExistingRepo(project string) (ProjectRepo, error)
}

// GcJob ports GcJob.
type GcJob interface {
	Start()
	Stop()
	OnPreGc(preGc func())
	OnPostGc(postGc func())
	// QueueForGc queues a project for GC. Callable from any goroutine.
	QueueForGc(projectName string)
	// WaitForRun returns a channel closed when the next DoRun completion
	// signal fires. (Java: CompletableFuture<Void> waitForRun().)
	WaitForRun() <-chan struct{}
	// DoRun runs one GC pass synchronously, then signals waiters. Exposed so
	// tests and the Bridge can drive it deterministically. (Java: doGC.)
	DoRun()
}

// GcJobImpl ports GcJobImpl: its own timer and a synchronized queue.
type GcJobImpl struct {
	repoStore  RepoStore
	locks      *data.ProjectLock
	intervalMs int64

	queueMu sync.Mutex
	gcQueue map[string]struct{}

	preGcMu  sync.Mutex
	preGc    func()
	postGcMu sync.Mutex
	postGc   func()

	waitersMu  sync.Mutex
	jobWaiters []chan struct{}

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
}

// NewGcJobImpl ports GcJobImpl(repoStore, locks, intervalMs).
func NewGcJobImpl(repoStore RepoStore, locks *data.ProjectLock, intervalMs int64) *GcJobImpl {
	g := &GcJobImpl{
		repoStore:  repoStore,
		locks:      locks,
		intervalMs: intervalMs,
		gcQueue:    map[string]struct{}{},
		stopCh:     make(chan struct{}),
	}
	g.preGc = func() {}
	g.postGc = func() {}
	return g
}

// NewGcJobDefault ports GcJobImpl(repoStore, locks) (default 1h interval).
func NewGcJobDefault(repoStore RepoStore, locks *data.ProjectLock) *GcJobImpl {
	return NewGcJobImpl(repoStore, locks, int64(time.Hour/time.Millisecond))
}

func (g *GcJobImpl) Start() {
	g.startOnce.Do(func() {
		slog.Debug("starting GC job to run every", "ms", g.intervalMs)
		go g.run()
	})
}

func (g *GcJobImpl) Stop() {
	g.stopOnce.Do(func() {
		slog.Debug("stopping GC job")
		close(g.stopCh)
	})
}

func (g *GcJobImpl) run() {
	for {
		// Java schedules with a delay equal to the interval (not 0).
		select {
		case <-g.stopCh:
			return
		case <-time.After(time.Duration(g.intervalMs) * time.Millisecond):
		}
		g.DoRun()
	}
}

// OnPreGc / OnPostGc are hooks in case they are needed, e.g. for testing.
func (g *GcJobImpl) OnPreGc(preGc func()) {
	g.preGcMu.Lock()
	g.preGc = preGc
	g.preGcMu.Unlock()
}

func (g *GcJobImpl) OnPostGc(postGc func()) {
	g.postGcMu.Lock()
	g.postGc = postGc
	g.postGcMu.Unlock()
}

// QueueForGc is safe to call from any goroutine.
func (g *GcJobImpl) QueueForGc(projectName string) {
	g.queueMu.Lock()
	defer g.queueMu.Unlock()
	g.gcQueue[projectName] = struct{}{}
}

// WaitForRun ports waitForRun: the returned channel is closed by the next
// DoRun completion (Java: CompletableFuture<Void>).
func (g *GcJobImpl) WaitForRun() <-chan struct{} {
	waiter := make(chan struct{})
	g.waitersMu.Lock()
	g.jobWaiters = append(g.jobWaiters, waiter)
	g.waitersMu.Unlock()
	return waiter
}

// DoRun ports doGC: iterate over and empty the queue, GC each project under
// its lock, signal waiters, run the post-GC hook.
func (g *GcJobImpl) DoRun() {
	slog.Debug("GC job running")
	g.preGcMu.Lock()
	preGc := g.preGc
	g.preGcMu.Unlock()
	preGc()

	g.queueMu.Lock()
	queue := g.gcQueue
	g.gcQueue = map[string]struct{}{}
	g.queueMu.Unlock()

	numGcs := 0
	for proj := range queue {
		slog.Debug("running GC job on project", "project", proj)
		g.lockAndGcProj(proj)
		numGcs++
	}
	slog.Debug("GC job finished", "numGcs", numGcs)

	g.waitersMu.Lock()
	waiters := g.jobWaiters
	g.jobWaiters = nil
	g.waitersMu.Unlock()
	for _, w := range waiters {
		close(w)
	}

	g.postGcMu.Lock()
	postGc := g.postGc
	g.postGcMu.Unlock()
	postGc()
}

// lockAndGcProj ports the per-project body of doGC:
//
//	try (LockGuard __ = locks.lockGuard(proj)) {
//	  try {
//	    repo = repoStore.getExistingRepo(proj);
//	    repo.runGC();
//	    repo.deleteIncomingPacks();
//	  } catch (IOException e) { warn }
//	} catch (CannotAcquireLockException e) { warn("skipping GC") }
func (g *GcJobImpl) lockAndGcProj(proj string) {
	guard, err := g.locks.LockForProjectGuard(proj)
	if err != nil {
		// ports catch (CannotAcquireLockException e)
		slog.Warn("cannot acquire project lock, skipping GC", "project", proj)
		return
	}
	defer guard.Close()

	repo, err := g.repoStore.GetExistingRepo(proj)
	if err != nil {
		// ports catch (IOException e) around the repo lookup
		slog.Warn("failed to GC project", "project", proj, "err", err)
		return
	}
	if err := repo.RunGC(); err != nil {
		slog.Warn("failed to GC project", "project", proj, "err", err)
		return
	}
	if err := repo.DeleteIncomingPacks(); err != nil {
		slog.Warn("failed to delete incoming packs", "project", proj, "err", err)
	}
}
