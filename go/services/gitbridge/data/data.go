// Package data ports the Java data/ package:
//   - data/model/Snapshot + snapshot/getforversion + getsavedvers models
//   - data/CannotAcquireLockException
//   - data/ProjectLockImpl (per-project re-entrant lock + global shutdown
//     barrier) exposing the bridge/lock ProjectLock + LockGuard contract
//   - data/LockAllWaiter
//
// Java ReentrantLock re-entrancy is per-THREAD. Go goroutines have no
// identity, so the Go port makes the holder EXPLICIT: the outer
// acquisition returns a *Holder token; nested re-entrant acquisitions and
// releases take that same token and bump/debump its depth. The observable
// contract is unchanged: while Bridge.getUpdatedRepo holds the token,
// SwapJob.Restore/Evict can re-acquire (re-enter) the same project lock.
package data

import (
	"log/slog"
	"sync"
	"time"

	"ollitex/go/services/gitbridge/util"
)

// ---------------------------------------------------------------------------
// Snapshot — ports data/model/Snapshot and the snapshot/getforversion +
// getsavedvers models.
// ---------------------------------------------------------------------------

// User ports snapshot/getsavedvers/WLUser (Anonymous default).
type User struct {
	Name  string
	Email string
}

// UserFrom ports the WLUser(name, email) constructor.
func UserFrom(name, email string) User {
	if name == "" || email == "" {
		return User{Name: "Anonymous", Email: "anonymous@" + util.GetServiceName() + ".com"}
	}
	return User{Name: name, Email: email}
}

// SnapshotFile ports snapshot/getforversion/SnapshotFile (a RawFile in Java).
// Wire shape is a two-item array [contents, path]; this is the domain copy
// that the Bridge commits (the wire package has its own JSON-tagged mirror).
type SnapshotFile struct {
	// Path is the repository-relative path of the file.
	Path     string
	Contents []byte
}

// Size returns the contents length (RawFile.size()).
func (f SnapshotFile) Size() int64 { return int64(len(f.Contents)) }

// SnapshotAttachment ports snapshot/getforversion/SnapshotAttachment.
// Wire shape is a two-item array [url, path]: url is the attachment location
// to FETCH, path is its target location in the repo.
type SnapshotAttachment struct {
	URL  string
	Path string
}

// Snapshot ports data/model/Snapshot. It is the domain model the Bridge
// commits: one per version, combining a getsavedvers SnapshotInfo (user, when)
// with a getforversion SnapshotData (files, attachments). CreatedAt is epoch
// millis so it can be used directly as the git commit date.
type Snapshot struct {
	VersionID int
	Comment   string
	User      User
	CreatedAt int64 // epoch millis
	Srcs      []SnapshotFile
	Atts      []SnapshotAttachment
}

// ParseSnapshotCreated parses the wire "createdAt" ISO-8601 string into epoch
// millis. The v2-import edge case (PR #50) may omit it; an empty or
// unparseable value falls back to the zero value (Java would throw; we are
// lenient so the edge case still produces a commit at epoch 0).
func ParseSnapshotCreated(iso string) int64 {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05.000Z07:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	if iso == "" {
		return 0
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, iso); err == nil {
			return t.UnixMilli()
		}
	}
	// best-effort last: ISO with fractional seconds + no zone
	if t, err := time.Parse("2006-01-02T15:04:05.000Z", iso); err == nil {
		return t.UnixMilli()
	}
	return 0
}

// ---------------------------------------------------------------------------
// Exceptions + waiter
// ---------------------------------------------------------------------------

// CannotAcquireLockException — Java message is fixed.
type CannotAcquireLockException struct{}

func (CannotAcquireLockException) Error() string {
	return "Another operation is in progress. Please try again later."
}

// LockAllWaiter — ports data/LockAllWaiter.
type LockAllWaiter interface {
	ThreadsRemaining(threads int)
}

// ---------------------------------------------------------------------------
// ProjectLock — port of data/ProjectLockImpl + bridge/lock/ProjectLock.
//
// Model (mirrors observable Java semantics):
//
//   - one re-entrant lock per project name (tryLock, default 5s timeout).
//   - a global reader barrier: on every TOP-LEVEL acquisition the lock also
//     takes a "reader slot". LockAll (Bridge.doShutdown, the Java
//     ReentrantReadWriteLock write-lock) first waits for all existing slots
//     to drain, then arms a barrier that parks new slot acquisitions
//     forever — Java: the write lock is held until JVM exit, so new readers
//     block on rlock.lock() until shutdown.
//   - a LockAllWaiter is notified of remaining reader counts (Java
//     unlockForProject's `if (waiting) trySignal()` and lockAll's initial
//     trySignal()).
//
// Re-entrancy is token-based (see Holder). LockGuard is the AutoCloseable
// form of Acquire: Close() releases.
// ---------------------------------------------------------------------------

const defaultLockTimeout = 5 * time.Second

// Holder is a re-entrant acquisition chain (project + outstanding depth).
type Holder struct {
	project string
	depth   int
}

// LockGuard ports bridge/lock/ProjectLock.LockGuard (AutoCloseable).
type LockGuard struct {
	p       *ProjectLock
	project string
	h       *Holder
}

// Close ports LockGuard.close: Release (project lock + reader slot).
func (g LockGuard) Close() { g.p.Release(g.project, g.h) }

// plEntry is one per-project re-entrant lock.
type plEntry struct {
	mu    sync.Mutex
	held  bool
	queue []chan struct{} // waiters (FIFO-ish; closed on release)
}

// tryLock is the timed try-lock (Java ReentrantLock.tryLock(5, SECONDS) when
// this goroutine does not already hold it). A single deadline spans the
// whole wait, matching Java's one-shot 5s window.
func (e *plEntry) tryLock(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		e.mu.Lock()
		if !e.held {
			e.held = true
			e.mu.Unlock()
			return true
		}
		w := make(chan struct{})
		e.queue = append(e.queue, w)
		e.mu.Unlock()
		remain := time.Until(deadline)
		if remain <= 0 {
			e.removeWaiter(w)
			return false
		}
		select {
		case <-w:
			// woke on release: single winner re-checks.
			e.mu.Lock()
			if !e.held {
				e.held = true
				e.mu.Unlock()
				return true
			}
			e.mu.Unlock()
			// lost the race: loop, re-register, wait again (like a condvar).
		case <-time.After(remain):
			e.removeWaiter(w)
			return false
		}
	}
}

// removeWaiter unregisters a timed-out waiter (defensive: may be absent if a
// release already closed the queue).
func (e *plEntry) removeWaiter(w chan struct{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, q := range e.queue {
		if q == w {
			e.queue = append(e.queue[:i], e.queue[i+1:]...)
			break
		}
	}
}

// unlock wakes every queued waiter.
func (e *plEntry) unlock() {
	e.mu.Lock()
	if !e.held {
		e.mu.Unlock()
		return
	}
	e.held = false
	q := e.queue
	e.queue = nil
	e.mu.Unlock()
	for _, w := range q {
		close(w)
	}
}

// ProjectLock ports data/ProjectLockImpl.
type ProjectLock struct {
	mu       sync.Mutex
	waiter   LockAllWaiter
	waiting  bool
	readers  int
	parking  chan struct{} // non-nil once LockAll has armed the barrier
	projects map[string]*plEntry
}

// NewProjectLock ports ProjectLockImpl() and ProjectLockImpl(waiter).
func NewProjectLock(waiter LockAllWaiter) *ProjectLock {
	return &ProjectLock{waiter: waiter, projects: map[string]*plEntry{}}
}

// SetWaiter ports ProjectLockImpl.setWaiter.
func (p *ProjectLock) SetWaiter(w LockAllWaiter) {
	p.mu.Lock()
	p.waiter = w
	p.mu.Unlock()
}

func (p *ProjectLock) entryFor(projectName string) *plEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.projects[projectName]
	if !ok {
		e = &plEntry{}
		p.projects[projectName] = e
	}
	return e
}

// Acquire ports lockForProject: timed try-lock of the project lock (5s),
// then take the reader slot. Returns the Holder token; pair with Release.
func (p *ProjectLock) Acquire(projectName string) (*Holder, error) {
	return p.AcquireWith(projectName, defaultLockTimeout)
}

// AcquireWith ports lockForProject with a caller-chosen timeout (test support).
func (p *ProjectLock) AcquireWith(projectName string, timeout time.Duration) (*Holder, error) {
	slog.Debug("taking project lock", "project", projectName)
	e := p.entryFor(projectName)
	if !e.tryLock(timeout) {
		slog.Debug("failed to acquire project lock", "project", projectName)
		return nil, CannotAcquireLockException{}
	}
	if !p.takeReadSlot() {
		e.unlock()
		return nil, CannotAcquireLockException{}
	}
	return &Holder{project: projectName, depth: 1}, nil
}

// Reacquire ports the re-entrant property of Java ReentrantLock: the SAME
// holder may re-acquire the same project lock, and Java's timed tryLock
// succeeds immediately (no timeout) when the thread already holds it.
// Must only be called while this holder is outstanding for the project.
func (p *ProjectLock) Reacquire(projectName string, h *Holder) {
	if h == nil || h.project != projectName || h.depth < 1 {
		panic("data: Reacquire called on a holder not outstanding for this project")
	}
	// The per-project mutex is already held by this goroutine (Java:
	// same-thread re-entry), so nothing to wait on; depth only.
	h.depth++
}

// LockForProjectGuard ports ProjectLock.lockGuard (the default AutoCloseable
// method: defer guard.Close()).
func (p *ProjectLock) LockForProjectGuard(projectName string) (LockGuard, error) {
	h, err := p.Acquire(projectName)
	if err != nil {
		return LockGuard{}, err
	}
	return LockGuard{p: p, project: projectName, h: h}, nil
}

// LockForProjectGuardWith is the nested (re-entrant) form of lockGuard: the
// parent holder must be outstanding for the same project.
func (p *ProjectLock) LockForProjectGuardWith(projectName string, parent *Holder) (LockGuard, error) {
	if parent != nil {
		if parent.project != projectName || parent.depth < 1 {
			panic("data: LockForProjectGuardWith parent not outstanding for this project")
		}
		p.Reacquire(projectName, parent)
		return LockGuard{p: p, project: projectName, h: parent}, nil
	}
	return p.LockForProjectGuard(projectName)
}

// Release ports ProjectLockImpl.unlockForProject: the outermost release drops
// the project lock and the reader slot; while LockAll has flagged waiting,
// the waiter is notified of the remaining reader count.
func (p *ProjectLock) Release(projectName string, h *Holder) {
	slog.Debug("releasing project lock", "project", projectName)
	e := p.entryFor(projectName)
	if h != nil {
		h.depth--
		if h.depth >= 1 {
			// nested release: depth only, the lock stays held
			return
		}
	}
	e.unlock()
	p.releaseReadSlot()
	// "if (waiting) trySignal"
	p.mu.Lock()
	waiting := p.waiting
	waiter := p.waiter
	readers := p.readers
	p.mu.Unlock()
	if waiting && waiter != nil && readers > 0 {
		waiter.ThreadsRemaining(readers)
	}
}

// LockAll ports lockAll: flag waiting, notify the waiter of current readers,
// wait until no reader slots remain, then arm the barrier so new top-level
// acquirers park (Java: the write lock is held until JVM exit).
func (p *ProjectLock) LockAll() {
	p.mu.Lock()
	p.waiting = true
	p.mu.Unlock()
	p.notifyWaiter()
	// drain: wait for existing reader slots to leave
	for {
		p.mu.Lock()
		r := p.readers
		p.mu.Unlock()
		if r == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.mu.Lock()
	if p.parking == nil {
		p.parking = make(chan struct{})
	}
	p.mu.Unlock()
}

// waitReaderCount exposes the current reader count (test support).
func (p *ProjectLock) WaitReaderCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.readers
}

func (p *ProjectLock) notifyWaiter() {
	p.mu.Lock()
	waiting := p.waiting
	waiter := p.waiter
	readers := p.readers
	p.mu.Unlock()
	if waiting && waiter != nil && readers > 0 {
		waiter.ThreadsRemaining(readers)
	}
}

// takeReadSlot: while LockAll has not parked the barrier, the slot is taken.
// Once parked, new top-level acquirers park forever (Java semantics: the
// write lock is held until JVM exit, so new readers block indefinitely).
func (p *ProjectLock) takeReadSlot() bool {
	p.mu.Lock()
	parking := p.parking
	if parking == nil {
		p.readers++
		p.mu.Unlock()
		return true
	}
	p.mu.Unlock()
	select {}
}

// releaseReadSlot ports rlock.unlock.
func (p *ProjectLock) releaseReadSlot() {
	p.mu.Lock()
	if p.readers > 0 {
		p.readers--
	}
	p.mu.Unlock()
}
