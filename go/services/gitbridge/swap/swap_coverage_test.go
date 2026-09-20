// Whitebox coverage tests for package swap, complementing swap_test.go /
// store_test.go / s3_test.go (the mirrored SwapJobImplTest cases).
//
// Drives the error branches of the SwapJobImpl plumbing synchronously
// (same package, so doSwap/Evict/RestoreWith are reachable) against small
// fake repo/db/swap stores, and exercises the real S3SwapStore HTTP path
// against a dead endpoint and a URL-parse failure path.
package swap

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ollitex/go/services/gitbridge/data"
	"ollitex/go/services/gitbridge/db"
)

// ---------------------------------------------------------------------------
// fake implementations of RepoStore / db.DBStore / SwapStore
// ---------------------------------------------------------------------------

type repoFake struct {
	size        int64
	sizeErr     error
	gcErr       error
	blob        []byte
	compressErr error
	ungzipErr   error
	removeErr   error
	panicker    bool
}

func (f *repoFake) TotalSize() (int64, error) {
	if f.panicker {
		panic("boom-total-size")
	}
	return f.size, f.sizeErr
}

func (f *repoFake) Bzip2Project(string) ([]byte, error) {
	return f.blob, f.compressErr
}

func (f *repoFake) GzipProject(string) ([]byte, error) {
	return f.blob, f.compressErr
}

func (f *repoFake) Unbzip2Project(string, []byte) error { return f.ungzipErr }
func (f *repoFake) UngzipProject(string, []byte) error  { return f.ungzipErr }
func (f *repoFake) GcProject(string) error              { return f.gcErr }

func (f *repoFake) Remove(string) error {
	return f.removeErr
}

type dbFake struct {
	projects     int
	unswapped    int
	oldestName   string
	oldestOk     bool
	compressions map[string]string
	touched      []string
}

func (d *dbFake) GetNumProjects() int                          { return d.projects }
func (d *dbFake) GetProjectNames() []string                    { return nil }
func (d *dbFake) SetLatestVersionForProject(string, int)       {}
func (d *dbFake) GetLatestVersionForProject(string) int        { return 0 }
func (d *dbFake) AddURLIndexForProject(string, string, string) {}
func (d *dbFake) DeleteFilesForProject(string, ...string)      {}
func (d *dbFake) GetPathForURLInProject(string, string) (string, bool) {
	return "", false
}
func (d *dbFake) GetOldestUnswappedProject() (string, bool) { return d.oldestName, d.oldestOk }
func (d *dbFake) Swap(string, string)                       {}
func (d *dbFake) Restore(string)                            {}
func (d *dbFake) GetNumUnswappedProjects() int              { return d.unswapped }
func (d *dbFake) GetProjectState(string) db.ProjectState    { return db.ProjectStateNotPresent }
func (d *dbFake) SetLastAccessedTime(project string, t *int64) {
	d.touched = append(d.touched, project)
}
func (d *dbFake) DeleteProject(string) {}
func (d *dbFake) Close() error         { return nil }
func (d *dbFake) GetSwapCompression(project string) (string, bool) {
	c, ok := d.compressions[project]
	return c, ok
}

type swapStoreFake struct {
	stored      map[string][]byte
	uploadErr   error
	downloadErr error
	removeErr   error
	uploaded    int
	removed     int
	downloaded  int
}

func (s *swapStoreFake) Upload(project string, blob []byte) error {
	s.uploaded++
	if s.uploadErr != nil {
		return s.uploadErr
	}
	s.stored[project] = blob
	return nil
}

func (s *swapStoreFake) Download(project string) ([]byte, error) {
	s.downloaded++
	if s.downloadErr != nil {
		return nil, s.downloadErr
	}
	blob, ok := s.stored[project]
	if !ok {
		return nil, errors.New("no such blob " + project)
	}
	out := make([]byte, len(blob))
	copy(out, blob)
	return out, nil
}

func (s *swapStoreFake) Remove(project string) error {
	s.removed++
	if s.removeErr != nil {
		return s.removeErr
	}
	delete(s.stored, project)
	return nil
}

func (s *swapStoreFake) IsSafe() bool { return true }

func newJob(r RepoStore, ddb *dbFake, sSwap SwapStore) *SwapJobImpl {
	return NewSwapJobImpl(0, 0, 0, time.Hour, CompressionBzip2,
		data.NewProjectLock(nil), r, ddb, sSwap)
}

// holdProjectLock acquires `project` and holds it until the test goroutine
// returns from the `release` closure (or the process ends). The caller is
// responsible for not releasing it until the lock-contention paths under
// test have had their chance to observe it.
func holdProjectLock(t *testing.T, lock *data.ProjectLock, project string, holdFor time.Duration) {
	t.Helper()
	h, err := lock.Acquire(project)
	if err != nil {
		t.Fatalf("setup: acquire %s: %v", project, err)
	}
	go func() {
		time.Sleep(holdFor)
		lock.Release(project, h)
	}()
}

// ---------------------------------------------------------------------------
// Noop surfaces (cheap, 1:1 with the Java no-op job).
// ---------------------------------------------------------------------------

func TestNoopSwapJobFullSurface(t *testing.T) {
	var job SwapJob = NoopSwapJob{}
	job.Start()
	job.Stop()
	if err := job.Evict("x"); err != nil {
		t.Fatalf("noop Evict: %v", err)
	}
	if err := job.Restore("x"); err != nil {
		t.Fatalf("noop Restore: %v", err)
	}
	if err := job.RestoreWith(nil, "x"); err != nil {
		t.Fatalf("noop RestoreWith: %v", err)
	}
	// the ported Java CompletableFuture: an already-closed channel.
	select {
	case <-job.WaitForRun():
	default:
		t.Fatalf("NoopSwapJob.WaitForRun must return a closed channel")
	}
	if got := job.SwapCount(); got != 0 {
		t.Fatalf("noop SwapCount = %d, want 0", got)
	}
}

func TestNoopSwapStoreFullSurface(t *testing.T) {
	s := NoopSwapStore{}
	if s.IsSafe() {
		t.Fatalf("NoopSwapStore must be unsafe")
	}
	if err := s.Upload("x", nil); err != nil {
		t.Fatalf("noop Upload: %v", err)
	}
	if _, err := s.Download("x"); err == nil {
		t.Fatalf("noop Download must error")
	}
	if err := s.Remove("x"); err != nil {
		t.Fatalf("noop Remove: %v", err)
	}
}

func TestCompressionMethodAsStringDefault(t *testing.T) {
	if got := compressionMethodAsString(CompressionKind("zip")); got != "" {
		t.Fatalf("default compression string = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// S3SwapStore — exercises the SDK plumbing against a dead endpoint and a
// URL-parse failure path.
// ---------------------------------------------------------------------------

func TestS3SwapStoreRoundTripAgainstLocalHTTP(t *testing.T) {
	// Minimal HTTP stand-in for S3 (no auth, no checksums, no versioning):
	// PUT -> 200; GET -> 200 with a known body; DELETE -> 200.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut, http.MethodPost:
			io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "payload-bytes")
		}
	}))
	defer srv.Close()

	store, err := NewS3SwapStore("ak", "sk", "test-bucket", "", srv.URL)
	if err != nil {
		t.Fatalf("NewS3SwapStore(local endpoint): %v", err)
	}
	if !store.IsSafe() {
		t.Fatalf("S3SwapStore must be safe")
	}
	if err := store.Upload("proj", []byte("data")); err != nil {
		t.Fatalf("s3 upload: %v", err)
	}
	if got, err := store.Download("proj"); err != nil || string(got) != "payload-bytes" {
		t.Fatalf("s3 download: err=%v body=%q", err, got)
	}
	if err := store.Remove("proj"); err != nil {
		t.Fatalf("s3 remove: %v", err)
	}
}

func TestS3SwapStoreOpsFailAgainstDeadEndpoint(t *testing.T) {
	// endpoint at a closed port: every SDK call fails (no HTTP, no
	// credentials) — exercising the real s3.Client error path on each of
	// the Upload/Download/Remove production methods.
	store, err := NewS3SwapStore("ak", "sk", "test-bucket", "us-east-1", "http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("NewS3SwapStore(dead endpoint): %v", err)
	}
	if err := store.Upload("p", []byte("b")); err == nil {
		t.Fatalf("upload to dead endpoint must error")
	}
	if _, err := store.Download("p"); err == nil {
		t.Fatalf("download from dead endpoint must error")
	}
	if err := store.Remove("p"); err == nil {
		t.Fatalf("remove on dead endpoint must error")
	}
}

func TestS3SwapStoreDefaultRegionBuilds(t *testing.T) {
	store, err := NewS3SwapStore("ak", "sk", "b", "", "")
	if err != nil {
		t.Fatalf("NewS3SwapStore(default region): %v", err)
	}
	if !store.IsSafe() {
		t.Fatalf("S3 swap store (region us-east-1) must be safe")
	}
}

func TestS3SwapStoreInvalidEndpointFails(t *testing.T) {
	// "%%" is not a valid URL escape, so url.Parse fails.
	if _, err := NewS3SwapStore("ak", "sk", "b", "us-east-1", "%%"); err == nil {
		t.Fatalf("NewS3SwapStore must fail for an unparseable endpoint")
	}
}

// ---------------------------------------------------------------------------
// doSwap edge branches (direct, same-package).
// ---------------------------------------------------------------------------

func TestDoSwapRecoversPanic(t *testing.T) {
	r := &repoFake{panicker: true}
	d := &dbFake{}
	s := NewInMemorySwapStore()
	job := newJob(r, d, s)
	// the panic must be swallowed by doSwap's defer (Java's catch-Throwable
	// port); if not, the test fails with the unhandled panic.
	job.doSwap()
}

func TestDoSwapTotalSizeErrorBreaksInnerLoop(t *testing.T) {
	// TotalSize errors: `if err != nil { totalSize = 0; break }` branch on
	// line 239-241, plus the trailing summary (line 289-297).
	r := &repoFake{size: 0, sizeErr: errors.New("size down")}
	d := &dbFake{projects: 10, unswapped: 5, oldestName: "x", oldestOk: true}
	s := &swapStoreFake{stored: map[string][]byte{}}
	job := newJob(r, d, s)
	job.doSwap()
	if s.uploaded != 0 {
		t.Fatalf("no uploads expected, got %d", s.uploaded)
	}
	if got := job.SwapCount(); got < 1 {
		t.Fatalf("SwapCount = %d, want >= 1", got)
	}
}

func TestDoSwapTooManyExceptionsGaveUp(t *testing.T) {
	// inner loop: compress/evict fails on every attempt, so after
	// maxExceptionProjectNames (20) iterations the "giving up" branch runs.
	r := &repoFake{size: 100, compressErr: errors.New("compress failed")}
	d := &dbFake{projects: 10, unswapped: 100, oldestName: "victim", oldestOk: true}
	s := &swapStoreFake{stored: map[string][]byte{}}
	job := newJob(r, d, s)
	job.doSwap()
	if s.uploaded != 0 {
		t.Fatalf("no uploads expected, got %d", s.uploaded)
	}
	if len(d.touched) != maxExceptionProjectNames {
		t.Fatalf("touched projects = %d, want %d", len(d.touched), maxExceptionProjectNames)
	}
}

func TestDoSwapNoOldestUnswappedProject(t *testing.T) {
	// GetOldestUnswappedProject returns !ok: inner loop breaks early.
	r := &repoFake{size: 100}
	d := &dbFake{projects: 10, unswapped: 100, oldestOk: false}
	s := &swapStoreFake{stored: map[string][]byte{}}
	job := newJob(r, d, s)
	job.doSwap()
	if s.uploaded != 0 {
		t.Fatalf("no uploads when no oldest project, got %d", s.uploaded)
	}
	if got := job.SwapCount(); got < 1 {
		t.Fatalf("SwapCount = %d, want >= 1", got)
	}
}

func TestWaitForRunSignalsAfterStart(t *testing.T) {
	r := &repoFake{size: 0}
	d := &dbFake{}
	s := &swapStoreFake{stored: map[string][]byte{}}
	job := newJob(r, d, s)
	defer job.Stop()
	// register a waiter BEFORE starting: the very first doSwap should close
	// it (signalWaiters is called after each run).
	ch := job.WaitForRun()
	job.Start()
	select {
	case <-ch:
		// signalled
	case <-time.After(3 * time.Second):
		t.Fatalf("waiter not signalled after the next run")
	}
}

func TestStartSecondCallDoesNothing(t *testing.T) {
	r := &repoFake{size: 0}
	d := &dbFake{}
	s := NewInMemorySwapStore()
	job := newJob(r, d, s)
	defer job.Stop()
	job.Start()
	job.Start() // startOnce: second call must be a no-op
	// Stop twice: stopOnce must guard.
	job.Stop()
}

// ---------------------------------------------------------------------------
// Evict branches (direct).
// ---------------------------------------------------------------------------

func TestEvictEmptyProjectName(t *testing.T) {
	job := newJob(&repoFake{}, &dbFake{}, &swapStoreFake{stored: map[string][]byte{}})
	if err := job.Evict(""); err == nil {
		t.Fatalf("Evict(\"\") must error")
	}
}

func TestEvictInvalidCompressionMethod(t *testing.T) {
	r := &repoFake{}
	d := &dbFake{}
	s := &swapStoreFake{stored: map[string][]byte{}}
	job := NewSwapJobImpl(0, 0, 0, time.Hour, CompressionKind("zip9000"),
		data.NewProjectLock(nil), r, d, s)
	if err := job.Evict("anything"); err == nil {
		t.Fatalf("Evict with an unsupported compression method must error")
	}
}

func TestEvictCompressErrorPropagates(t *testing.T) {
	r := &repoFake{compressErr: errors.New("compress failed")}
	d := &dbFake{}
	s := &swapStoreFake{stored: map[string][]byte{}}
	job := newJob(r, d, s)
	if err := job.Evict("p"); err == nil {
		t.Fatalf("Evict must propagate compress errors")
	}
}

func TestEvictUploadErrorPropagates(t *testing.T) {
	r := &repoFake{blob: []byte("blob")}
	d := &dbFake{}
	s := &swapStoreFake{stored: map[string][]byte{}, uploadErr: errors.New("upload failed")}
	job := newJob(r, d, s)
	if err := job.Evict("p"); err == nil {
		t.Fatalf("Evict must propagate upload errors")
	}
	if len(s.stored) != 0 {
		t.Fatalf("no blob should be stored after upload failure: %v", s.stored)
	}
}

func TestEvictLockContentionErrors(t *testing.T) {
	// holdProjectLock keeps "held" locked for 5.5s; Evict's default 5s
	// timeout elapses first, surfacing CannotAcquireLockException.
	lock := data.NewProjectLock(nil)
	holdProjectLock(t, lock, "held", 5500*time.Millisecond)
	job := NewSwapJobImpl(0, 0, 0, time.Hour, CompressionBzip2,
		lock, &repoFake{}, &dbFake{}, NewInMemorySwapStore())
	if err := job.Evict("held"); err == nil {
		t.Fatalf("Evict on a held project must error (5s timeout not elapsed? err=nil)")
	}
}

// ---------------------------------------------------------------------------
// Restore branches (direct).
// ---------------------------------------------------------------------------

func TestRestoreEmptyProjectName(t *testing.T) {
	job := newJob(&repoFake{}, &dbFake{}, &swapStoreFake{stored: map[string][]byte{}})
	if err := job.Restore(""); err == nil {
		t.Fatalf("Restore(\"\") must error")
	}
}

func TestRestoreDownloadMissingBlob(t *testing.T) {
	r := &repoFake{}
	d := &dbFake{}
	s := NewInMemorySwapStore()
	job := newJob(r, d, s)
	if err := job.Restore("never-uploaded"); err == nil {
		t.Fatalf("Restore of a missing blob must error")
	}
}

func TestRestoreMissingCompressionErrors(t *testing.T) {
	r := &repoFake{}
	d := &dbFake{compressions: map[string]string{}} // no "p"
	s := &swapStoreFake{stored: map[string][]byte{"p": []byte("b")}}
	job := newJob(r, d, s)
	if err := job.Restore("p"); err == nil {
		t.Fatalf("Restore without a recorded compression must error")
	}
}

func TestRestoreUnknownCompressionErrors(t *testing.T) {
	r := &repoFake{}
	d := &dbFake{compressions: map[string]string{"p": "zip9000"}}
	s := &swapStoreFake{stored: map[string][]byte{"p": []byte("b")}}
	job := newJob(r, d, s)
	if err := job.Restore("p"); err == nil {
		t.Fatalf("Restore with an unknown compression must error")
	}
}

func TestRestoreUngzipErrorPropagatesGzip(t *testing.T) {
	r := &repoFake{ungzipErr: errors.New("ungzip failed")}
	d := &dbFake{compressions: map[string]string{"p": "gzip"}}
	s := &swapStoreFake{stored: map[string][]byte{"p": []byte("b")}}
	job := newJob(r, d, s)
	if err := job.Restore("p"); err == nil {
		t.Fatalf("Restore must propagate ungzip errors (gzip)")
	}
}

func TestRestoreUngzipErrorPropagatesBzip2(t *testing.T) {
	r := &repoFake{ungzipErr: errors.New("unbzip2 failed")}
	d := &dbFake{compressions: map[string]string{"p": "bzip2"}}
	s := &swapStoreFake{stored: map[string][]byte{"p": []byte("b")}}
	job := newJob(r, d, s)
	if err := job.Restore("p"); err == nil {
		t.Fatalf("Restore must propagate unbzip2 errors (bzip2)")
	}
}

func TestRestoreSwapStoreRemoveErrorPropagates(t *testing.T) {
	r := &repoFake{}
	d := &dbFake{compressions: map[string]string{"p": "gzip"}}
	s := &swapStoreFake{stored: map[string][]byte{"p": []byte("b")}, removeErr: errors.New("remove failed")}
	job := newJob(r, d, s)
	if err := job.Restore("p"); err == nil {
		t.Fatalf("Restore must propagate swap-store remove errors")
	}
}

func TestRestoreLockContentionSwallowsError(t *testing.T) {
	// Restore is the one method where a lock-contention error is swallowed
	// (returns nil, not err) — matching the Java behaviour.
	lock := data.NewProjectLock(nil)
	holdProjectLock(t, lock, "held", 5500*time.Millisecond)
	job := NewSwapJobImpl(0, 0, 0, time.Hour, CompressionBzip2,
		lock, &repoFake{}, &dbFake{}, NewInMemorySwapStore())
	if err := job.Restore("held"); err != nil {
		t.Fatalf("Restore on a held project must return nil (Java swallows lock failure), got %v", err)
	}
}
