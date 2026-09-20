// Package swap ports bridge/swap (job + store) for the Go git-bridge.
// The job speaks to a small RepoStore/DB interface so tests and the Bridge
// can inject the real FSGitRepoStore / SqliteDBStore.
package swap

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"ollitex/go/services/gitbridge/config"
	"ollitex/go/services/gitbridge/data"
	"ollitex/go/services/gitbridge/db"
)

const giB int64 = 1 << 30

const maxExceptionProjectNames = 20

// CompressionKind ports SwapJob.CompressionMethod.
type CompressionKind string

const (
	CompressionNone  CompressionKind = ""
	CompressionBzip2 CompressionKind = "bzip2"
	CompressionGzip  CompressionKind = "gzip"
)

func stringToCompressionMethod(s string) CompressionKind {
	switch s {
	case "gzip":
		return CompressionGzip
	case "bzip2":
		return CompressionBzip2
	default:
		return CompressionNone
	}
}

func compressionMethodAsString(m CompressionKind) string {
	switch m {
	case CompressionGzip:
		return "gzip"
	case CompressionBzip2:
		return "bzip2"
	default:
		return ""
	}
}

// RepoStore is the minimal FSGitRepoStore surface the swap job needs.
// *repo.FSGitRepoStore satisfies it.
type RepoStore interface {
	TotalSize() (int64, error)
	Bzip2Project(project string) ([]byte, error)
	GzipProject(project string) ([]byte, error)
	Unbzip2Project(project string, blob []byte) error
	UngzipProject(project string, blob []byte) error
	GcProject(project string) error
	Remove(project string) error
}

// SwapJob ports bridge/swap/job/SwapJob.
type SwapJob interface {
	Start()
	Stop()
	Evict(projectName string) error
	Restore(projectName string) error
	// RestoreWith is the nested (same-holder) form of Restore. Go has no
	// thread identity, so it takes the caller's *data.Holder token. A nil
	// holder is the fresh (standalone) form, identical to Restore; a non-nil
	// holder re-enters that holder's project lock (Java's same-thread
	// ReentrantLock re-entry). The Bridge passes its own token when it
	// restores a project while already holding that project's lock.
	RestoreWith(holder *data.Holder, projectName string) error
	// WaitForRun is a test seam for the next timer tick; closed after the next run.
	WaitForRun() <-chan struct{}
	// Swaps returns the number of completed swap runs.
	SwapCount() int64
}

// NoopSwapJob ports NoopSwapJob.
type NoopSwapJob struct{}

func (NoopSwapJob) Start()                                     {}
func (NoopSwapJob) Stop()                                      {}
func (NoopSwapJob) Evict(string) error                         { return nil }
func (NoopSwapJob) Restore(string) error                       { return nil }
func (NoopSwapJob) RestoreWith(_ *data.Holder, _ string) error { return nil }
func (NoopSwapJob) WaitForRun() <-chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}
func (NoopSwapJob) SwapCount() int64 { return 0 }

// FromConfig ports SwapJob.fromConfig.
func FromConfig(cfg *config.SwapJob, lock *data.ProjectLock, repoStore RepoStore, dbStore db.DBStore, swapStore SwapStore) SwapJob {
	if cfg == nil {
		return NoopSwapJob{}
	}
	if !swapStore.IsSafe() && !cfg.AllowUnsafeStores {
		slog.Warn("swap store not safe; disabling swap job", "store", fmt.Sprintf("%T", swapStore))
		return NoopSwapJob{}
	}
	method := stringToCompressionMethod(cfg.CompressionMethod)
	if method == CompressionNone {
		// Ports Java logging the unsupported method and defaulting to bzip2.
		slog.Warn("unsupported compressionMethod, default to bzip2", "method", cfg.CompressionMethod)
		method = CompressionBzip2
	}
	return NewSwapJobImpl(
		cfg.MinProjects,
		giB*int64(cfg.LowGiB),
		giB*int64(cfg.HighGiB),
		time.Duration(cfg.IntervalMillis)*time.Millisecond,
		method,
		lock,
		repoStore,
		dbStore,
		swapStore,
	)
}

// NewSwapJobImpl ports SwapJobImpl(minProjects, lowWatermarkBytes,
// highWatermarkBytes, interval, method, lock, repoStore, dbStore, swapStore).
func NewSwapJobImpl(
	minProjects int,
	lowWatermarkBytes, highWatermarkBytes int64,
	interval time.Duration,
	method CompressionKind,
	lock *data.ProjectLock,
	repoStore RepoStore,
	dbStore db.DBStore,
	swapStore SwapStore,
) *SwapJobImpl {
	return &SwapJobImpl{
		minProjects:        minProjects,
		lowWatermarkBytes:  lowWatermarkBytes,
		highWatermarkBytes: highWatermarkBytes,
		interval:           interval,
		compressionMethod:  method,
		lock:               lock,
		repoStore:          repoStore,
		dbStore:            dbStore,
		swapStore:          swapStore,
		stopCh:             make(chan struct{}),
	}
}

// SwapJobImpl ports SwapJobImpl: a repeating timer that evicts (uploads to
// the swap store and unlinks) the oldest unswapped projects until size or
// project-count watermarks are met.
type SwapJobImpl struct {
	minProjects        int
	lowWatermarkBytes  int64
	highWatermarkBytes int64
	interval           time.Duration
	compressionMethod  CompressionKind
	lock               *data.ProjectLock
	repoStore          RepoStore
	dbStore            db.DBStore
	swapStore          SwapStore

	swaps atomic.Int64

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}

	waitersMu  sync.Mutex
	jobWaiters []chan struct{}
}

func (j *SwapJobImpl) Start() {
	j.startOnce.Do(func() {
		go j.run()
	})
}

func (j *SwapJobImpl) Stop() {
	j.stopOnce.Do(func() {
		close(j.stopCh)
	})
}

// WaitForRun ports the jobWaiters list of the Java implementation: the
// returned channel is closed by the next timer run (Java: CompletableFuture).
func (j *SwapJobImpl) WaitForRun() <-chan struct{} {
	waiter := make(chan struct{})
	j.waitersMu.Lock()
	j.jobWaiters = append(j.jobWaiters, waiter)
	j.waitersMu.Unlock()
	return waiter
}

func (j *SwapJobImpl) signalWaiters() {
	j.waitersMu.Lock()
	waiters := j.jobWaiters
	j.jobWaiters = nil
	j.waitersMu.Unlock()
	for _, w := range waiters {
		close(w)
	}
}

func (j *SwapJobImpl) SwapCount() int64 { return j.swaps.Load() }

// run ports doSwap (including the periodic reschedule, as a goroutine loop).
func (j *SwapJobImpl) run() {
	// Java schedules at 0, i.e. the first run is immediate.
	for {
		j.doSwap()
		j.signalWaiters()
		select {
		case <-j.stopCh:
			return
		case <-time.After(j.interval):
		}
	}
}

// doSwap ports doSwap_ (with an outer recover() so a failed run is logged,
// never dropped, exactly like Java's `catch (Throwable t)`).
func (j *SwapJobImpl) doSwap() {
	defer func() {
		if t := recover(); t != nil {
			slog.Warn("exception thrown during swap job", "recovered", fmt.Sprintf("%v", t))
		}
	}()

	exceptionProjectNames := []string{}
	slog.Debug("running swap", "number", j.swaps.Load()+1)

	totalSize, err := j.repoStore.TotalSize()
	if err != nil {
		totalSize = 0
	}
	slog.Debug("size is", "total", totalSize, "high", j.highWatermarkBytes)
	if totalSize < j.highWatermarkBytes {
		slog.Debug("no need to swap")
		j.swaps.Add(1)
		return
	}
	numProjects := j.dbStore.GetNumProjects()

	// while we have too many projects on disk
	for {
		totalSize, err = j.repoStore.TotalSize()
		if err != nil {
			break
		}
		numProjects = j.dbStore.GetNumUnswappedProjects()
		if totalSize <= j.lowWatermarkBytes || numProjects <= j.minProjects {
			break
		}
		// check if we've had too many exceptions so far
		if len(exceptionProjectNames) >= maxExceptionProjectNames {
			msg := ""
			for _, s := range exceptionProjectNames {
				msg += s + " "
			}
			slog.Error("too many exceptions while running swap, giving up on this run", "projects", msg)
			break
		}
		// get the oldest project and try to swap it
		projectName, ok := j.dbStore.GetOldestUnswappedProject()
		if !ok {
			break
		}
		err := j.Evict(projectName)
		if err != nil {
			// NOTE: ports Java's hack. If a project fails to swap we keep the
			// same failing project as the oldest unswapped one forever, which
			// fills the disk with errors. By touching the access time we mark
			// it as a non-candidate for this run. (Java swallows a lock failure
			// inside evict() and loops; here the error surfaces and the project
			// is marked, which strictly avoids that busy-loop.)
			slog.Warn("exception while swapping, mark project and move on", "project", projectName, "err", err)
			now := time.Now().UnixMilli()
			j.dbStore.SetLastAccessedTime(projectName, &now)
			exceptionProjectNames = append(exceptionProjectNames, projectName)
		}
	}

	if j.lowWatermarkBytes > 0 && totalSize > j.lowWatermarkBytes {
		// NOTE: if lowWatermarkBytes is 0 we never expect to reach it (the
		// repo database stays on disk), so don't warn.
		slog.Warn("finished swapping, but total size is still too high")
	}
	slog.Debug("sizes", "total", totalSize, "low", j.lowWatermarkBytes, "high",
		j.highWatermarkBytes, "projectsOnDisk", numProjects, "numAll",
		j.dbStore.GetNumProjects(), "min", j.minProjects)
	j.swaps.Add(1)
}

/*
 * @see SwapJob.Evict for high-level description.
 *
 * 1. Acquires the project lock.
 * 2. Gets the compressed bytes of the project from the repo store.
 * 3. Uploads the bytes to `project` in the swapStore.
 * 4. Sets the last accessed time in the dbStore to NULL (state SWAPPED).
 * 5. Removes the project from the repo store.
 */
func (j *SwapJobImpl) Evict(projName string) error {
	if projName == "" {
		return errors.New("evict: empty project name")
	}
	slog.Debug("evicting project", "project", projName)
	g, err := j.lock.LockForProjectGuard(projName)
	if err != nil {
		slog.Warn("cannot acquire project lock, skipping swap", "project", projName)
		return err
	}
	defer g.Close()
	err = j.evictLocked(projName)
	// A lock failure is reported (unlike Java, which swallows it); other
	// errors propagate as-is.
	return err
}

func (j *SwapJobImpl) evictLocked(projName string) error {
	// 1 + 2: gc and compress (upload, swap, remove below)
	if err := j.repoStore.GcProject(projName); err != nil {
		slog.Error("exception while running gc on project", "project", projName, "err", err)
	}
	var blob []byte
	var err error
	switch j.compressionMethod {
	case CompressionGzip:
		blob, err = j.repoStore.GzipProject(projName)
	case CompressionBzip2:
		blob, err = j.repoStore.Bzip2Project(projName)
	default:
		return fmt.Errorf("evict: invalid compression method %q", j.compressionMethod)
	}
	if err != nil {
		return err
	}
	// 3: upload
	if err := j.swapStore.Upload(projName, blob); err != nil {
		return err
	}
	compression := compressionMethodAsString(j.compressionMethod)
	if compression == "" {
		return errors.New("invalid compression method, should not happen")
	}
	// 4: mark swapped
	j.dbStore.Swap(projName, compression)
	// 5: unlink
	return j.repoStore.Remove(projName)
}

/*
 * @see SwapJob.Restore for high-level description.
 */
// Restore is the standalone (fresh-lock) form of RestoreWith. A nil holder
// re-locates the project lock fresh (Java: swap jobs not nested in the
// Bridge's own lock).
func (j *SwapJobImpl) Restore(projName string) error { return j.RestoreWith(nil, projName) }

/*
 * RestoreWith performs SwapJob.Restore under the given holder if one is
 * supplied (the nested form: the Bridge is already holding that project's
 * lock and re-enters it, Java: same-thread re-entrant tryLock), otherwise it
 * takes a fresh lock.
 */
func (j *SwapJobImpl) RestoreWith(holder *data.Holder, projName string) error {
	if projName == "" {
		return errors.New("restore: empty project name")
	}
	slog.Debug("restoring project", "project", projName)
	g, err := j.lock.LockForProjectGuardWith(projName, holder)
	if err != nil {
		slog.Warn("cannot acquire project lock, skipping restore", "project", projName)
		return nil
	}
	defer g.Close()
	// 2: download from the swap store
	blob, err := j.swapStore.Download(projName)
	if err != nil {
		return err
	}
	// 3: write it back into the repo store, per the recorded compression
	compression, ok := j.dbStore.GetSwapCompression(projName)
	if !ok {
		return fmt.Errorf("missing compression method during restore for %q, should not happen", projName)
	}
	switch stringToCompressionMethod(compression) {
	case CompressionGzip:
		err = j.repoStore.UngzipProject(projName, blob)
	case CompressionBzip2:
		err = j.repoStore.Unbzip2Project(projName, blob)
	default:
		return fmt.Errorf("project %q has unknown compression %q, should not happen", projName, compression)
	}
	if err != nil {
		return err
	}
	// ports Java order: store.remove then dbStore.restore.
	if err := j.swapStore.Remove(projName); err != nil {
		return err
	}
	j.dbStore.Restore(projName)
	return nil
}
