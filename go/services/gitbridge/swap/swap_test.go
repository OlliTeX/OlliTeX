package swap

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ollitex/go/services/gitbridge/config"
	"ollitex/go/services/gitbridge/data"
	"ollitex/go/services/gitbridge/db"
	"ollitex/go/services/gitbridge/repo"
)

// fixtureSrc mirrors FSGitRepoStoreTest.rootdir (the fs fixture set). The Go
// repo tests copy these; here we reuse the same fixture tarball (the plain
// dirs live in test fixtures, but are stored as tar to keep them git-friendly).
const fixtureSrc = "../repo/testdata/fixtures/fs.tar.gz"

// copyTree untars the fs fixture tarball into dest (the store root).
func copyTree(t *testing.T, src, dest string) {
	t.Helper()
	f, err := os.Open(src)
	if err != nil {
		t.Fatalf("open fixture %s: %v", src, err)
	}
	defer f.Close()
	buf, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("gunzip fixture: %v", err)
	}
	defer gz.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatalf("mkdir dest: %v", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		name := hdr.Name
		if len(name) >= 2 && name[:2] == "./" {
			name = name[2:]
		}
		if name == "" {
			continue
		}
		target := filepath.Join(dest, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", target, err)
			}
		case tar.TypeReg, tar.TypeSymlink:
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatalf("mkdir dir: %v", err)
			}
			if err := os.WriteFile(target, data, 0o644); err != nil {
				t.Fatalf("write %s: %v", target, err)
			}
		}
	}
}

func waitWhile(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting (timeout %s)", timeout)
}

// setupJob mirrors SwapJobImplTest.setup: tmp repo dir with proj1/proj2, a DB
// with both projects (proj2 last-accessed 1s older), and a job with
// minProjects=1, low=15000, high=30000, interval=100ms, bzip2.
func setupJob(t *testing.T, method CompressionKind) (*SwapJobImpl, *db.SqliteDBStore) {
	t.Helper()
	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "repostore")
	copyTree(t, fixtureSrc, repoDir)

	// The Java test uses FileUtils::sizeOfDirectory as the fsSizer (a
	// deterministic, recursive file-byte sizer, NOT the statfs default).
	count := func(absPath string) (int64, error) {
		var total int64
		_ = filepath.Walk(absPath, func(_ string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() {
				total += info.Size()
			}
			return nil
		})
		return total, nil
	}

	repoStore := repo.NewFSGitRepoStoreWithSizer(repoDir, 100_000, count)

	dbFile := filepath.Join(tmp, "wlgb.db")
	dbStore, err := db.NewSqliteDBStore(dbFile, 0)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	dbStore.SetLatestVersionForProject("proj1", 0)
	dbStore.SetLatestVersionForProject("proj2", 0)
	now := time.Now().UnixMilli()
	oldest := now - int64((1*time.Second)/time.Millisecond)
	dbStore.SetLastAccessedTime("proj1", &now)
	dbStore.SetLastAccessedTime("proj2", &oldest)

	swapStore := NewInMemorySwapStore()
	lock := data.NewProjectLock(nil)

	job := NewSwapJobImpl(1, 15000, 30000, 100*time.Millisecond, method, lock, repoStore, dbStore, swapStore)
	t.Cleanup(job.Stop)
	return job, dbStore
}

// TestStartingTimerAlwaysCausesASwap ports startingTimerAlwaysCausesASwap.
func TestStartingTimerAlwaysCausesASwap(t *testing.T) {
	job, dbStore := setupJob(t, CompressionBzip2)
	_ = dbStore
	// Mirrors swapJob.lowWatermarkBytes = 16384; swapJob.interval = 1h.
	job.lowWatermarkBytes = 16384
	job.interval = time.Hour

	if got := job.SwapCount(); got != 0 {
		t.Fatalf("swaps before start = %d, want 0", got)
	}
	job.Start()
	waitWhile(t, 10*time.Second, func() bool { return job.SwapCount() > 0 })
	if got := job.SwapCount(); got <= 0 {
		t.Errorf("swaps = %d after start, want > 0", got)
	}
}

// TestSwapsHappenEveryInterval ports swapsHappenEveryInterval.
func TestSwapsHappenEveryInterval(t *testing.T) {
	job, _ := setupJob(t, CompressionBzip2)
	defer job.Stop()
	job.lowWatermarkBytes = 16384

	if got := job.SwapCount(); got != 0 {
		t.Fatalf("swaps before start = %d, want 0", got)
	}
	job.Start()
	waitWhile(t, 10*time.Second, func() bool { return job.SwapCount() > 1 })
	if got := job.SwapCount(); got <= 1 {
		t.Errorf("swaps = %d after interval, want > 1", got)
	}
}

// TestNoProjectsGetSwappedWhenUnderHighWatermark ports
// noProjectsGetSwappedWhenUnderHighWatermark.
func TestNoProjectsGetSwappedWhenUnderHighWatermark(t *testing.T) {
	job, dbStore := setupJob(t, CompressionBzip2)
	defer job.Stop()
	// Mirrors swapJob.highWatermarkBytes = 65536.
	job.highWatermarkBytes = 65536

	if got := dbStore.GetNumUnswappedProjects(); got != 2 {
		t.Fatalf("unswapped projects = %d, want 2", got)
	}
	job.Start()
	waitWhile(t, 10*time.Second, func() bool { return job.SwapCount() >= 1 })
	if got := dbStore.GetNumUnswappedProjects(); got != 2 {
		t.Errorf("unswapped projects = %d after run, want 2 (unchanged)", got)
	}
}

// TestCorrectProjGetSwappedWhenOverHighWatermark ports
// correctProjGetSwappedWhenOverHighWatermark (bzip2 default).
func TestCorrectProjGetSwappedWhenOverHighWatermark(t *testing.T) {
	job, dbStore := setupJob(t, CompressionBzip2)
	defer job.Stop()
	// Mirrors swapJob.lowWatermarkBytes = 16384
	job.lowWatermarkBytes = 16384

	if got := dbStore.GetNumUnswappedProjects(); got != 2 {
		t.Fatalf("unswapped projects = %d, want 2", got)
	}
	if got, ok := dbStore.GetOldestUnswappedProject(); !ok || got != "proj2" {
		t.Fatalf("oldest unswapped = %v, want proj2", got)
	}
	job.Start()
	waitWhile(t, 10*time.Second, func() bool { return job.SwapCount() >= 1 })

	if got := dbStore.GetNumUnswappedProjects(); got != 1 {
		t.Fatalf("unswapped projects = %d, want 1", got)
	}
	if got, ok := dbStore.GetOldestUnswappedProject(); !ok || got != "proj1" {
		t.Fatalf("oldest unswapped = %v, want proj1", got)
	}
	if comp, ok := dbStore.GetSwapCompression("proj2"); !ok || comp != "bzip2" {
		t.Fatalf("swap compression for proj2 = %v, want bzip2", comp)
	}

	// Restore proj2.
	if err := job.Restore("proj2"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if comp, ok := dbStore.GetSwapCompression("proj2"); ok && comp != "" {
		t.Fatalf("swap compression after restore = %q, want <set>", comp)
	}
	if got := dbStore.GetNumUnswappedProjects(); got != 2 {
		t.Errorf("unswapped projects after restore = %d, want 2", got)
	}

	// After the next run proj2 (the oldest) is evicted again.
	numSwaps := job.SwapCount()
	waitWhile(t, 10*time.Second, func() bool { return job.SwapCount() > numSwaps })
	if got := dbStore.GetNumUnswappedProjects(); got != 1 {
		t.Fatalf("unswapped projects = %d second round, want 1", got)
	}
	if got, ok := dbStore.GetOldestUnswappedProject(); !ok || got != "proj2" {
		t.Fatalf("oldest unswapped second round = %v, want proj2 (proj1 evicted)", got)
	}
}

// TestSwapCompressionGzip ports swapCompressionGzip.
func TestSwapCompressionGzip(t *testing.T) {
	job, dbStore := setupJob(t, CompressionGzip)
	defer job.Stop()
	job.lowWatermarkBytes = 16384
	if got := dbStore.GetNumUnswappedProjects(); got != 2 {
		t.Fatalf("unswapped projects = %d, want 2", got)
	}
	if got, ok := dbStore.GetOldestUnswappedProject(); !ok || got != "proj2" {
		t.Fatalf("oldest unswapped = %v, want proj2", got)
	}
	job.Start()
	waitWhile(t, 10*time.Second, func() bool { return job.SwapCount() >= 1 })
	if got := dbStore.GetNumUnswappedProjects(); got != 1 {
		t.Fatalf("unswapped projects = %d, want 1", got)
	}
	if got, ok := dbStore.GetOldestUnswappedProject(); !ok || got != "proj1" {
		t.Fatalf("oldest unswapped = %v, want proj1", got)
	}
	if comp, ok := dbStore.GetSwapCompression("proj2"); !ok || comp != "gzip" {
		t.Fatalf("swap compression for proj2 = %v, want gzip", comp)
	}
	if err := job.Restore("proj2"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if comp, ok := dbStore.GetSwapCompression("proj2"); ok && comp != "" {
		t.Fatalf("swap compression after restore = %q, want <set>", comp)
	}
	numSwaps := job.SwapCount()
	waitWhile(t, 10*time.Second, func() bool { return job.SwapCount() > numSwaps })
	if got := dbStore.GetNumUnswappedProjects(); got != 1 {
		t.Fatalf("unswapped projects second round = %d, want 1", got)
	}
	if got, ok := dbStore.GetOldestUnswappedProject(); !ok || got != "proj2" {
		t.Fatalf("oldest unswapped second round = %v, want proj2", got)
	}
}

// TestFromConfigNoopWhenUnSafeAndDisallowed ports the FromConfig no-op path.
func TestFromConfigNoConfig(t *testing.T) {
	job := FromConfig(nil, data.NewProjectLock(nil), nil, nil, nil)
	if _, ok := job.(NoopSwapJob); !ok {
		t.Fatalf("FromConfig(nil) = %T, want NoopSwapJob", job)
	}
}

func TestFromConfigNoopDisallowed(t *testing.T) {
	cfg := config.SwapJob{MinProjects: 1, AllowUnsafeStores: false}
	job := FromConfig(&cfg, data.NewProjectLock(nil), nil, nil, NoopSwapStore{})
	if _, ok := job.(NoopSwapJob); !ok {
		t.Fatalf("FromConfig(unsafe, disallowed) = %T, want NoopSwapJob", job)
	}
}

func TestFromConfigUnsafeButAllowed(t *testing.T) {
	cfg := config.SwapJob{MinProjects: 1, AllowUnsafeStores: true}
	job := FromConfig(&cfg, data.NewProjectLock(nil), nil, nil, NoopSwapStore{})
	if _, ok := job.(*SwapJobImpl); !ok {
		t.Fatalf("FromConfig(unsafe, allowed) = %T, want *SwapJobImpl", job)
	}
}

func TestFromConfigBadCompressionDefaultsToBzip2(t *testing.T) {
	cfg := config.SwapJob{MinProjects: 1, AllowUnsafeStores: true, CompressionMethod: "zip9000"}
	job := FromConfig(&cfg, data.NewProjectLock(nil), nil, nil, NoopSwapStore{})
	impl, ok := job.(*SwapJobImpl)
	if !ok {
		t.Fatalf("want *SwapJobImpl, got %T", job)
	}
	if impl.compressionMethod != CompressionBzip2 {
		t.Errorf("unsupported compression %q must default to bzip2, got %q", cfg.CompressionMethod, impl.compressionMethod)
	}
}
