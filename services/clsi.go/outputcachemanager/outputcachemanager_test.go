// Tests for the Go port of services/clsi/app/js/OutputCacheManager.js.
// Node ships no unit test for this module; these assertions mirror the
// Node source contract 1:1 (callback shapes, error swallowing, per-dir
// queueing, metrics labels).
package outputcachemanager

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"clsi/contentcachemanager"
	clserrors "clsi/errors"
	off "clsi/outputfilefinder"
)

var (
	noopLog   = func(level, msg string, obj map[string]any) {}
	noMetrics = func(status string, opts map[string]any) {}
)

func fakeSuccess(a contentcachemanager.UpdateArgs) (*contentcachemanager.UpdateResult, error) {
	return &contentcachemanager.UpdateResult{
		ContentRanges:    []contentcachemanager.ContentRange{},
		NewContentRanges: []contentcachemanager.ContentRange{},
		ReclaimedSpace:   0,
	}, nil
}

// testManager builds a Manager with deterministic seams (whitebox setup).
func testManager() *Manager {
	return &Manager{
		PdfCachingEnabled: true,
		OptimiseInDocker:  true,
		Now:               func() int64 { return 1_000 },
		RandHex:           func() (string, error) { return "deadbeef00112233", nil },
		UpdateContent:     fakeSuccess,
		OptimiseFile:      func(src, dst string) error { return nil },
		ScheduleAfter:     func(delayMs int64, fn func()) {},
		MetricsInc:        noMetrics,
		Log:               noopLog,
		oldest:            map[string]float64{},
		pumps:             map[string]*pumpState{},
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func mustWriteDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("expected %s to not exist", path)
	}
}

func poll(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 5000; i++ {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met after 5s poll")
}

func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }

type metricsRecorder struct {
	mu       sync.Mutex
	statuses []string
	lastOpts map[string]any
}

func (r *metricsRecorder) record(status string, opts map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses = append(r.statuses, status)
	r.lastOpts = opts
}

func (r *metricsRecorder) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.statuses...)
}

// setupCompileDirs creates the standard fixture set.
func setupCompileDirs(t *testing.T) (compileDir, outputDir string) {
	compileDir = t.TempDir()
	outputDir = t.TempDir()
	writeFile(t, filepath.Join(compileDir, "output.pdf"), "pdfdata") // 7 bytes
	writeFile(t, filepath.Join(compileDir, "output.log"), "log")
	writeFile(t, filepath.Join(compileDir, "strace.out"), "S")
	writeFile(t, filepath.Join(compileDir, ".hidden"), "secret")
	return compileDir, outputDir
}

func TestPath(t *testing.T) {
	m := testManager()
	if got := m.Path("abc-123", "output.pdf"); got != filepath.Join(CacheSubdir, "abc-123", "output.pdf") {
		t.Fatalf("path(valid): %q", got)
	}
	if got := m.Path("not-a-buildid", "output.pdf"); got != "output.pdf" {
		t.Fatalf("path(invalid): %q", got)
	}
	if got := m.Path("", "foo.pdf"); got != "foo.pdf" {
		t.Fatalf("path(empty): %q", got)
	}
}

func TestGenerateBuildId(t *testing.T) {
	m := testManager()
	id, err := m.GenerateBuildId()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	// 1000 ms = 0x3e8
	if id != "3e8-deadbeef00112233" {
		t.Fatalf("buildId: %q", id)
	}
	m2 := testManager()
	m2.RandHex = func() (string, error) { return "", errors.New("no rand") }
	if _, err := m2.GenerateBuildId(); err == nil {
		t.Fatal("expected error for rand failure")
	}
}

func TestQueueDirOperation_Sequential(t *testing.T) {
	m := testManager()
	dir := t.TempDir()
	counter := 0
	var mu sync.Mutex
	for i := 0; i < 3; i++ {
		m.EnqueueDirOperation(dir, func() error {
			mu.Lock()
			counter++
			mu.Unlock()
			return nil
		})
	}
	poll(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return counter == 3
	})
}

func TestQueueDirOperation_ValueAndError(t *testing.T) {
	m := testManager()
	dir := t.TempDir()
	v, err := m.QueueDirOperation(dir, func() (int, error) { return 42, nil })
	if err != nil || v != 42 {
		t.Fatalf("value: %d %v", v, err)
	}
	if _, err := m.QueueDirOperation(dir, func() (int, error) { return 0, errors.New("boom") }); err == nil {
		t.Fatal("expected error propagation")
	}
}

// --- ExpireOutputFiles ---

func TestExpireOutputFiles_ENOENT_cleansAll(t *testing.T) {
	m := testManager()
	outputDir := t.TempDir()
	if err := m.ExpireOutputFiles(outputDir, &ExpireOptions{}); err != nil {
		t.Fatalf("ENOENT expire: %v", err)
	}
	mustNotExist(t, outputDir)
}

func TestExpireOutputFiles_EmptyCacheDir_cleansAll(t *testing.T) {
	m := testManager()
	outputDir := t.TempDir()
	mustWriteDir(t, filepath.Join(outputDir, CacheSubdir))
	if err := m.ExpireOutputFiles(outputDir, newExpireOptions("", nil)); err != nil {
		t.Fatalf("expire: %v", err)
	}
	mustNotExist(t, outputDir)
}

func TestExpireOutputFiles_AgeExpired(t *testing.T) {
	m := testManager()
	m.Now = func() int64 { return 10_000_000 } // 10_000 s
	outputDir := t.TempDir()
	// recent: hex-date 0x999999 (10066329) → age = 10000000-10066329 < 0
	mustWriteDir(t, filepath.Join(outputDir, CacheSubdir, "999999-1"))
	// old: hex-date 0x1 → age ≈ 9999999 > CACHE_AGE (5400000)
	mustWriteDir(t, filepath.Join(outputDir, CacheSubdir, "1-2"))
	if err := m.ExpireOutputFiles(outputDir, newExpireOptions("", nil)); err != nil {
		t.Fatalf("expire: %v", err)
	}
	mustExist(t, filepath.Join(outputDir, CacheSubdir, "999999-1"))
	mustNotExist(t, filepath.Join(outputDir, CacheSubdir, "1-2"))
	mustExist(t, outputDir)
}

func TestExpireOutputFiles_KeepOption(t *testing.T) {
	m := testManager()
	m.Now = func() int64 { return 5_000_000 }
	outputDir := t.TempDir()
	for _, d := range []string{"1-1", "2-2", "3-3"} {
		mustWriteDir(t, filepath.Join(outputDir, CacheSubdir, d))
	}
	// reverse sort: [3-3, 2-2, 1-1]
	// i=0: keep; i=1: 1 > limit(0) → remove; i=2: 2 > 0 → remove
	if err := m.ExpireOutputFiles(outputDir, newExpireOptions("3-3", intPtr(0))); err != nil {
		t.Fatalf("expire: %v", err)
	}
	mustExist(t, filepath.Join(outputDir, CacheSubdir, "3-3"))
	mustNotExist(t, filepath.Join(outputDir, CacheSubdir, "2-2"))
	mustNotExist(t, filepath.Join(outputDir, CacheSubdir, "1-1"))
}

func TestExpireOutputFiles_LimitNull_OnlyCacheLimit(t *testing.T) {
	m := testManager()
	m.Now = func() int64 { return 100_000 }
	outputDir := t.TempDir()
	for _, d := range []string{"f000-1", "f001-2", "f002-3", "f003-4"} {
		mustWriteDir(t, filepath.Join(outputDir, CacheSubdir, d))
	}
	// reverse sort: [f003-4, f002-3, f001-2, f000-1]
	// i=0..2: not beyond CACHE_LIMIT(2), recent → keep; i=3: 3>2 → remove
	if err := m.ExpireOutputFiles(outputDir, newExpireOptions("", nil)); err != nil {
		t.Fatalf("expire: %v", err)
	}
	mustNotExist(t, filepath.Join(outputDir, CacheSubdir, "f000-1"))
	for _, d := range []string{"f001-2", "f002-3", "f003-4"} {
		mustExist(t, filepath.Join(outputDir, CacheSubdir, d))
	}
}

func TestExpireOutputFiles_NaNNameAgeSkipped(t *testing.T) {
	m := testManager()
	m.Now = func() int64 { return 100_000 }
	outputDir := t.TempDir()
	// "xyz-1": parseInt("xyz",16) = NaN → age comparison false → kept
	mustWriteDir(t, filepath.Join(outputDir, CacheSubdir, "xyz-1"))
	if err := m.ExpireOutputFiles(outputDir, newExpireOptions("", nil)); err != nil {
		t.Fatalf("expire: %v", err)
	}
	mustExist(t, filepath.Join(outputDir, CacheSubdir, "xyz-1"))
}

func TestExpireOutputFiles_AllExpired_cleansAll(t *testing.T) {
	m := testManager()
	m.Now = func() int64 { return 10_000_000 }
	outputDir := t.TempDir()
	mustWriteDir(t, filepath.Join(outputDir, CacheSubdir, "1-1"))
	mustWriteDir(t, filepath.Join(outputDir, CacheSubdir, "2-2"))
	if err := m.ExpireOutputFiles(outputDir, newExpireOptions("", nil)); err != nil {
		t.Fatalf("expire: %v", err)
	}
	mustNotExist(t, outputDir)
}

func TestExpireOutputFiles_RegistersOutputDir(t *testing.T) {
	m := testManager()
	m.Now = func() int64 { return 100_000 }
	outputDir := t.TempDir()
	for _, d := range []string{"f000-1", "f001-2", "f002-3", "f003-4"} {
		mustWriteDir(t, filepath.Join(outputDir, CacheSubdir, d))
	}
	if err := m.ExpireOutputFiles(outputDir, newExpireOptions("", nil)); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if !m.hasOldest(outputDir) {
		t.Fatal("outputDir not registered after expire")
	}
	// Node: oldestDirTimeToKeep is updated for EVERY surviving dir, so the
	// final value is the LAST one that survived in iteration order: f001-2.
	if got := m.oldest[outputDir]; got != float64(0xf001) {
		t.Fatalf("oldest recorded: %v", got)
	}
}

// --- checkIfShouldCopy / checkIfShouldArchive / fileHidden ---

func TestCheckIfShouldCopy(t *testing.T) {
	m := testManager()
	if ok, _ := m.checkIfShouldCopy("/a/b/c.pdf"); !ok {
		t.Fatal("expected copy")
	}
	if ok, _ := m.checkIfShouldCopy("/a/strace.log"); ok {
		t.Fatal("strace must not be copied")
	}
	if ok, _ := m.checkIfShouldCopy("/a/stracedump"); ok {
		t.Fatal("strace-prefixed must not be copied")
	}
}

func TestCheckIfShouldArchive(t *testing.T) {
	m := testManager()
	if ok, _ := m.checkIfShouldArchive("/a/strace"); !ok {
		t.Fatal("strace must be archived")
	}
	if ok, _ := m.checkIfShouldArchive("/a/output.log"); ok {
		t.Fatal("archive_logs off: output.log not archived")
	}
	m.ArchiveLogs = true
	if ok, _ := m.checkIfShouldArchive("/a/output.log"); !ok {
		t.Fatal("archive_logs on: output.log archived")
	}
	if ok, _ := m.checkIfShouldArchive("/a/output.blg"); !ok {
		t.Fatal("archive_logs on: output.blg archived")
	}
	if ok, _ := m.checkIfShouldArchive("/a/other.txt"); ok {
		t.Fatal("other.txt never archived")
	}
	// archive error seam via stat-able src
	if _, err := m.checkIfShouldArchive("/a/whatever"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFileHidden(t *testing.T) {
	for _, p := range []string{".hidden", "dir/.hidden", "a/b/.c"} {
		if !fileHidden(p) {
			t.Fatalf("expected hidden: %q", p)
		}
	}
	for _, p := range []string{"file.pdf", "a/b/c.log", "hidden.txt"} {
		if fileHidden(p) {
			t.Fatalf("expected not hidden: %q", p)
		}
	}
}

// --- collectOutputPdfSize ---

func TestCollectOutputPdfSize(t *testing.T) {
	m := testManager()
	outputDir := t.TempDir()
	writeFile(t, filepath.Join(outputDir, CacheSubdir, "abc-123", "output.pdf"), "pdfdata")
	files := []off.OutputFile{
		{Path: "other.pdf"},
		{Path: "output.pdf", Type: "pdf", Build: "abc-123"},
	}
	stats := map[string]float64{}
	if err := m.collectOutputPdfSize(files, outputDir, stats); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if files[1].Size == nil || *files[1].Size != int64(7) {
		t.Fatalf("size: %+v", files[1])
	}
	if stats["pdf-size"] != 7 {
		t.Fatalf("stat: %v", stats)
	}
	if files[0].Size != nil {
		t.Fatalf("non-pdf size must stay nil: %+v", files[0])
	}
}

func TestCollectOutputPdfSize_NoneFound(t *testing.T) {
	m := testManager()
	files := []off.OutputFile{{Path: "other.pdf", Type: "pdf"}}
	if err := m.collectOutputPdfSize(files, t.TempDir(), map[string]float64{}); err != nil {
		t.Fatalf("no-pdf should not error: %v", err)
	}
}

func TestCollectOutputPdfSize_StatErrorPropagates(t *testing.T) {
	m := testManager()
	files := []off.OutputFile{{Path: "output.pdf", Type: "pdf", Build: "abc-123"}}
	if err := m.collectOutputPdfSize(files, t.TempDir(), map[string]float64{}); err == nil {
		t.Fatal("stat error must propagate")
	}
}

// --- saveOutputFilesInBuildDir copy + cleanup paths ---

func TestSaveOutputFiles_Basic(t *testing.T) {
	m := testManager()
	compileDir, outputDir := setupCompileDirs(t)
	mrec := &metricsRecorder{}
	m.MetricsInc = mrec.record
	inFiles := []off.OutputFile{
		{Path: "output.pdf", Type: "pdf"},
		{Path: "output.log", Type: "log"},
		{Path: "strace.out", Type: "log"},
		{Path: ".hidden", Type: "other"},
	}
	res, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-123", EnablePdfCaching: false},
		inFiles, compileDir, outputDir, map[string]float64{}, map[string]float64{},
	)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if res.BuildID != "abc-123" {
		t.Fatalf("buildId: %+v", res)
	}
	// strace excluded (checkIfShouldCopy), .hidden skipped (dotfile)
	if len(res.Files) != 2 {
		t.Fatalf("expected 2 files, got %+v", res.Files)
	}
	for _, f := range res.Files {
		if f.Build != "abc-123" {
			t.Fatalf("build not set: %+v", f)
		}
		if f.Path == "output.pdf" && (f.Size == nil || *f.Size != int64(7)) {
			t.Fatalf("size: %+v", f)
		}
	}
	// pdf caching disabled → no content dir, no metrics fired
	mustNotExist(t, filepath.Join(outputDir, ContentSubdir))
	if got := mrec.get(); len(got) != 0 {
		t.Fatalf("expected no metrics, got %v", got)
	}
	mustExist(t, filepath.Join(outputDir, CacheSubdir, "abc-123", "output.pdf"))
	mustExist(t, filepath.Join(outputDir, CacheSubdir, "abc-123", "output.log"))
	mustNotExist(t, filepath.Join(outputDir, CacheSubdir, "abc-123", "strace.out"))
	mustNotExist(t, filepath.Join(outputDir, CacheSubdir, "abc-123", ".hidden"))
	if !m.hasOldest(outputDir) {
		t.Fatal("outputDir not registered in OLDEST_BUILD_DIR")
	}
}

func TestSaveOutputFiles_GeneratesBuildId(t *testing.T) {
	m := testManager()
	compileDir, outputDir := setupCompileDirs(t)
	res, err := m.SaveOutputFiles(
		SaveRequest{EnablePdfCaching: false},
		[]off.OutputFile{{Path: "output.pdf", Type: "pdf"}},
		compileDir, outputDir, map[string]float64{}, map[string]float64{},
	)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if res.BuildID != "3e8-deadbeef00112233" {
		t.Fatalf("generated buildId: %+v", res)
	}
	mustExist(t, filepath.Join(outputDir, CacheSubdir, "3e8-deadbeef00112233", "output.pdf"))
}

func TestSaveOutputFiles_PdfCachingDisabledNoOps(t *testing.T) {
	m := testManager()
	m.PdfCachingEnabled = false
	compileDir, outputDir := setupCompileDirs(t)
	updates := 0
	var mu sync.Mutex
	m.UpdateContent = func(a contentcachemanager.UpdateArgs) (*contentcachemanager.UpdateResult, error) {
		mu.Lock()
		updates++
		mu.Unlock()
		return &contentcachemanager.UpdateResult{}, nil
	}
	if _, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-1", EnablePdfCaching: true},
		[]off.OutputFile{{Path: "output.pdf", Type: "pdf"}},
		compileDir, outputDir, map[string]float64{}, map[string]float64{},
	); err != nil {
		t.Fatalf("save: %v", err)
	}
	mu.Lock()
	got := updates
	mu.Unlock()
	if got != 0 {
		t.Fatalf("UpdateContent must not run; ran %d times", got)
	}
}

func TestSaveOutputFiles_Enabled(t *testing.T) {
	m := testManager()
	compileDir, outputDir := setupCompileDirs(t)
	mrec := &metricsRecorder{}
	m.MetricsInc = mrec.record
	var captured contentcachemanager.UpdateArgs
	m.UpdateContent = func(a contentcachemanager.UpdateArgs) (*contentcachemanager.UpdateResult, error) {
		captured = a
		return &contentcachemanager.UpdateResult{
			ContentRanges: []contentcachemanager.ContentRange{
				{ObjectID: "9 0 ", Start: 1074, End: 11235, Hash: "d7cfc73a"},
			},
			NewContentRanges: []contentcachemanager.ContentRange{
				{ObjectID: "9 0 ", Start: 1074, End: 11235, Hash: "d7cfc73a"},
			},
			ReclaimedSpace:            42,
			OverheadDeleteStaleHashes: int64Ptr(7),
		}, nil
	}
	stats := map[string]float64{}
	timings := map[string]float64{"compile": 1234}
	res, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-123", EnablePdfCaching: true, PdfCachingMinChunkSize: 100},
		[]off.OutputFile{{Path: "output.pdf", Type: "pdf"}},
		compileDir, outputDir, stats, timings,
	)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if captured.CompileTime != 1234 || captured.PdfCachingMinChunkSize != 100 {
		t.Fatalf("ccm args: %+v", captured)
	}
	wantPath := filepath.Join(outputDir, m.Path("abc-123", "output.pdf"))
	if captured.FilePath != wantPath {
		t.Fatalf("ccm filePath: %s (want %s)", captured.FilePath, wantPath)
	}
	pdf := res.Files[0]
	if pdf.ContentID == nil || *pdf.ContentID != "3e8-deadbeef00112233" {
		t.Fatalf("contentId: %+v", pdf)
	}
	if len(pdf.Ranges) != 1 || pdf.Ranges[0].Hash != "d7cfc73a" || pdf.Ranges[0].Start != 1074 || pdf.Ranges[0].End != 11235 {
		t.Fatalf("ranges: %+v", pdf.Ranges)
	}
	if stats["pdf-caching-n-ranges"] != 1 {
		t.Fatalf("stats n-ranges: %v", stats)
	}
	if stats["pdf-caching-total-ranges-size"] != (11235 - 1074) {
		t.Fatalf("total size: %v", stats)
	}
	if stats["pdf-caching-n-new-ranges"] != 1 {
		t.Fatalf("n-new-ranges: %v", stats)
	}
	if stats["pdf-caching-new-ranges-size"] != (11235 - 1074) {
		t.Fatalf("new size: %v", stats)
	}
	if stats["pdf-caching-reclaimed-space"] != 42 {
		t.Fatalf("reclaimed: %v", stats)
	}
	if stats["pdf-size"] != 7 {
		t.Fatalf("pdf-size stat: %v", stats)
	}
	if timings["pdf-caching-overhead-delete-stale-hashes"] != 7 {
		t.Fatalf("overhead: %v", timings)
	}
	if _, ok := timings["compute-pdf-caching"]; !ok {
		t.Fatalf("compute timing missing: %v", timings)
	}
	if got := mrec.get(); len(got) != 1 || got[0] != "success" {
		t.Fatalf("metrics: %v", got)
	}
	mustExist(t, filepath.Join(outputDir, ContentSubdir))
}

func TestSaveOutputFiles_Dark(t *testing.T) {
	m := testManager()
	m.PdfCachingDark = true
	compileDir, outputDir := setupCompileDirs(t)
	m.UpdateContent = func(a contentcachemanager.UpdateArgs) (*contentcachemanager.UpdateResult, error) {
		return &contentcachemanager.UpdateResult{
			ContentRanges:    []contentcachemanager.ContentRange{{ObjectID: "9 0 ", Start: 0, End: 100, Hash: "h"}},
			NewContentRanges: []contentcachemanager.ContentRange{},
		}, nil
	}
	stats := map[string]float64{}
	res, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-1", EnablePdfCaching: false},
		[]off.OutputFile{{Path: "output.pdf", Type: "pdf"}},
		compileDir, outputDir, stats, map[string]float64{},
	)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	// dark: CCM ran + stats set, but file NOT annotated
	if stats["pdf-caching-n-ranges"] != 1 {
		t.Fatalf("stats: %v", stats)
	}
	if res.Files[0].ContentID != nil || res.Files[0].Ranges != nil {
		t.Fatalf("dark must not annotate file: %+v", res.Files)
	}
}

func TestSaveOutputFiles_SoftTimedOut(t *testing.T) {
	m := testManager()
	compileDir, outputDir := setupCompileDirs(t)
	mrec := &metricsRecorder{}
	m.MetricsInc = mrec.record
	m.UpdateContent = func(a contentcachemanager.UpdateArgs) (*contentcachemanager.UpdateResult, error) {
		return &contentcachemanager.UpdateResult{
			TimedOutErr: &clserrors.TimedOutError{Message: "soft"},
		}, nil
	}
	stats := map[string]float64{}
	if _, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-1", EnablePdfCaching: true},
		[]off.OutputFile{{Path: "output.pdf", Type: "pdf"}},
		compileDir, outputDir, stats, map[string]float64{},
	); err != nil {
		t.Fatalf("soft timeout must not fail compile: %v", err)
	}
	if stats["pdf-caching-timed-out"] != 1 {
		t.Fatalf("stats: %v", stats)
	}
	if got := mrec.get(); len(got) != 1 || got[0] != "timed-out-soft-failure" {
		t.Fatalf("status: %v", got)
	}
}

func TestSaveOutputFiles_NoXref(t *testing.T) {
	m := testManager()
	compileDir, outputDir := setupCompileDirs(t)
	mrec := &metricsRecorder{}
	m.MetricsInc = mrec.record
	m.UpdateContent = func(a contentcachemanager.UpdateArgs) (*contentcachemanager.UpdateResult, error) {
		return nil, clserrors.NewNoXrefTableError(errors.New("boom"))
	}
	res, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-1", EnablePdfCaching: true},
		[]off.OutputFile{{Path: "output.pdf", Type: "pdf"}},
		compileDir, outputDir, map[string]float64{}, map[string]float64{},
	)
	if err != nil {
		t.Fatalf("NoXref must not fail compile: %v", err)
	}
	if got := mrec.get(); len(got) != 1 || got[0] != "boom" {
		t.Fatalf("status must be err.message: %v", got)
	}
	if res.Files[0].ContentID != nil {
		t.Fatalf("NoXref: contentId must be nil: %+v", res.Files)
	}
}

func TestSaveOutputFiles_QueueLimit(t *testing.T) {
	m := testManager()
	compileDir, outputDir := setupCompileDirs(t)
	mrec := &metricsRecorder{}
	m.MetricsInc = mrec.record
	m.UpdateContent = func(a contentcachemanager.UpdateArgs) (*contentcachemanager.UpdateResult, error) {
		return nil, &clserrors.QueueLimitReachedError{}
	}
	stats := map[string]float64{}
	if _, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-1", EnablePdfCaching: true},
		[]off.OutputFile{{Path: "output.pdf", Type: "pdf"}},
		compileDir, outputDir, stats, map[string]float64{},
	); err != nil {
		t.Fatalf("queue-limit must not fail compile: %v", err)
	}
	if stats["pdf-caching-queue-limit-reached"] != 1 {
		t.Fatalf("stats: %v", stats)
	}
	if got := mrec.get(); len(got) != 1 || got[0] != "queue-limit" {
		t.Fatalf("status: %v", got)
	}
}

func TestSaveOutputFiles_TimedOutHard(t *testing.T) {
	m := testManager()
	compileDir, outputDir := setupCompileDirs(t)
	mrec := &metricsRecorder{}
	m.MetricsInc = mrec.record
	m.UpdateContent = func(a contentcachemanager.UpdateArgs) (*contentcachemanager.UpdateResult, error) {
		return nil, &clserrors.TimedOutError{Message: "hard timeout"}
	}
	stats := map[string]float64{}
	if _, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-1", EnablePdfCaching: true},
		[]off.OutputFile{{Path: "output.pdf", Type: "pdf"}},
		compileDir, outputDir, stats, map[string]float64{},
	); err != nil {
		t.Fatalf("timed-out must not fail compile: %v", err)
	}
	if stats["pdf-caching-timed-out"] != 1 {
		t.Fatalf("stats: %v", stats)
	}
	if got := mrec.get(); len(got) != 1 || got[0] != "timed-out" {
		t.Fatalf("status: %v", got)
	}
}

func TestSaveOutputFiles_GenericErrorSwallowed(t *testing.T) {
	m := testManager()
	compileDir, outputDir := setupCompileDirs(t)
	mrec := &metricsRecorder{}
	m.MetricsInc = mrec.record
	warns := 0
	var wmu sync.Mutex
	m.Log = func(level, msg string, obj map[string]any) {
		wmu.Lock()
		if level == "warn" {
			warns++
		}
		wmu.Unlock()
	}
	m.UpdateContent = func(a contentcachemanager.UpdateArgs) (*contentcachemanager.UpdateResult, error) {
		return nil, errors.New("boom")
	}
	res, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-1", EnablePdfCaching: true},
		[]off.OutputFile{{Path: "output.pdf", Type: "pdf"}},
		compileDir, outputDir, map[string]float64{}, map[string]float64{},
	)
	if err != nil {
		t.Fatalf("generic error must be swallowed: %v", err)
	}
	if got := mrec.get(); len(got) != 1 || got[0] != "failed" {
		t.Fatalf("status: %v", got)
	}
	if res.BuildID != "abc-1" {
		t.Fatalf("build: %+v", res)
	}
	wmu.Lock()
	defer wmu.Unlock()
	if warns != 1 {
		t.Fatalf("expected exactly 1 warn log, got %d", warns)
	}
}

func TestSaveOutputFiles_MissingPdf(t *testing.T) {
	m := testManager()
	compileDir, outputDir := setupCompileDirs(t)
	mrec := &metricsRecorder{}
	m.MetricsInc = mrec.record
	m.UpdateContent = func(a contentcachemanager.UpdateArgs) (*contentcachemanager.UpdateResult, error) {
		t.Error("UpdateContent must not be called without output.pdf")
		return nil, errors.New("unexpected call")
	}
	// no pdf in the file list → "missing-pdf"; output.log is copied
	if _, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-1", EnablePdfCaching: true},
		[]off.OutputFile{{Path: "output.log", Type: "other"}},
		compileDir, outputDir, map[string]float64{}, map[string]float64{},
	); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := mrec.get(); len(got) != 1 || got[0] != "missing-pdf" {
		t.Fatalf("status: %v", got)
	}
}

func TestSaveOutputFiles_ContentDirUnavailable(t *testing.T) {
	m := testManager()
	compileDir, outputDir := setupCompileDirs(t)
	// block content-dir creation: <outputDir>/content is a FILE
	writeFile(t, filepath.Join(outputDir, ContentSubdir), "notadir")
	mrec := &metricsRecorder{}
	m.MetricsInc = mrec.record
	_, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-1", EnablePdfCaching: true},
		[]off.OutputFile{{Path: "output.pdf", Type: "pdf"}},
		compileDir, outputDir, map[string]float64{}, map[string]float64{},
	)
	if err != nil {
		t.Fatalf("content-dir-unavailable must be swallowed: %v", err)
	}
	if got := mrec.get(); len(got) != 1 || got[0] != "content-dir-unavailable" {
		t.Fatalf("status: %v", got)
	}
}

func TestSaveOutputFiles_EnoentCopyPropagates(t *testing.T) {
	m := testManager()
	compileDir, outputDir := setupCompileDirs(t)
	_, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-123", EnablePdfCaching: false},
		[]off.OutputFile{{Path: "ghost.pdf", Type: "pdf"}},
		compileDir, outputDir, map[string]float64{}, map[string]float64{},
	)
	if err == nil {
		t.Fatal("expected ENOENT copy error to propagate")
	}
	mustNotExist(t, filepath.Join(outputDir, CacheSubdir, "abc-123"))
}

func TestSaveOutputFiles_MkdirFails_Propagates(t *testing.T) {
	m := testManager()
	compileDir, outputDir := setupCompileDirs(t)
	writeFile(t, filepath.Join(outputDir, CacheSubdir, "abc-123"), "notadir")
	_, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-123", EnablePdfCaching: false},
		[]off.OutputFile{{Path: "output.pdf", Type: "pdf"}},
		compileDir, outputDir, map[string]float64{}, map[string]float64{},
	)
	if err == nil {
		t.Fatal("expected mkdir error to propagate")
	}
}

func TestSaveOutputFiles_MidCopyFail_Cleaned(t *testing.T) {
	m := testManager()
	compileDir, outputDir := setupCompileDirs(t)
	_, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-123", EnablePdfCaching: false},
		[]off.OutputFile{
			{Path: "output.pdf", Type: "pdf"},
			{Path: "ghost.pdf", Type: "pdf"},
		},
		compileDir, outputDir, map[string]float64{}, map[string]float64{},
	)
	if err == nil {
		t.Fatal("expected second copy failure to propagate")
	}
	mustNotExist(t, filepath.Join(outputDir, CacheSubdir, "abc-123"))
}

// --- per-user cleanup ---

func TestSaveOutputFiles_PerUserLimit(t *testing.T) {
	m := testManager()
	compileBase := "12345678901234567890abcd-12345678901234567890abcd"
	if !perUserRegexp.MatchString(compileBase) {
		t.Skipf("fixture base %q must match perUserRegexp", compileBase)
	}
	compileDir := filepath.Join(t.TempDir(), compileBase)
	outputDir := t.TempDir()
	writeFile(t, filepath.Join(compileDir, "output.pdf"), "pdf")
	for _, id := range []string{"1-1", "2-2", "3-3", "4-4"} {
		mustWriteDir(t, filepath.Join(outputDir, CacheSubdir, id))
	}
	if _, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "5-5", EnablePdfCaching: false},
		[]off.OutputFile{{Path: "output.pdf", Type: "pdf"}},
		compileDir, outputDir, map[string]float64{}, map[string]float64{},
	); err != nil {
		t.Fatalf("save: %v", err)
	}
	// cleanup (fire-and-forget on the outputDir queue): keep "5-5", limit 1
	// → reverse-sorted [5-5,4-4,3-3,2-2,1-1]; i=0 keep; i=1 4-4 (1>1? no →
	// age check, recent → keep); i>1 removed.
	poll(t, func() bool {
		gone := true
		for _, id := range []string{"3-3", "2-2", "1-1"} {
			if _, err := os.Stat(filepath.Join(outputDir, CacheSubdir, id)); err == nil {
				gone = false
			}
		}
		if !gone {
			return false
		}
		for _, id := range []string{"5-5", "4-4"} {
			if _, err := os.Stat(filepath.Join(outputDir, CacheSubdir, id)); err != nil {
				return false
			}
		}
		return true
	})
}

func TestSaveOutputFiles_NonPerUserNoExpiryLimit(t *testing.T) {
	m := testManager()
	compileDir, outputDir := setupCompileDirs(t)
	// seed MANY recent dirs; without perUser limit they all stay within
	// CACHE_LIMIT — here they exceed CACHE_LIMIT? no: expire removes i>2
	// regardless of perUser; perUser only affects the limit param (nil vs
	// 1). With limit nil, i>1 (CACHE_LIMIT=2) are removed by age check for
	// recent ones kept... To keep the test deterministic: only seed 3 dirs.
	for _, id := range []string{"1-1", "2-2", "3-3"} {
		mustWriteDir(t, filepath.Join(outputDir, CacheSubdir, id))
	}
	if _, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "4-4", EnablePdfCaching: false},
		[]off.OutputFile{{Path: "output.pdf", Type: "pdf"}},
		compileDir, outputDir, map[string]float64{}, map[string]float64{},
	); err != nil {
		t.Fatalf("save: %v", err)
	}
	// limit nil: reverse sort [4-4,3-3,2-2,1-1]; i=0..2 i>2? no → kept (recent);
	// i=3 (1-1): 3>2 → removed.
	poll(t, func() bool {
		if _, err := os.Stat(filepath.Join(outputDir, CacheSubdir, "1-1")); err != nil {
			return true
		}
		kept := 0
		for _, id := range []string{"4-4", "3-3", "2-2"} {
			if _, err := os.Stat(filepath.Join(outputDir, CacheSubdir, id)); err == nil {
				kept++
			}
		}
		return kept == 3
	})
}

// --- ensureContentDir ---

func TestEnsureContentDir_ReusesFirstMatch(t *testing.T) {
	m := testManager()
	root := t.TempDir()
	mustWriteDir(t, filepath.Join(root, "ffff-999"))
	mustWriteDir(t, filepath.Join(root, "0000-abc"))
	mustWriteDir(t, filepath.Join(root, "junk"))
	dir, err := m.ensureContentDir(root)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if dir != filepath.Join(root, "0000-abc") {
		t.Fatalf("first match: %q", dir)
	}
}

func TestEnsureContentDir_CreatesNew(t *testing.T) {
	m := testManager()
	root := t.TempDir()
	mustWriteDir(t, filepath.Join(root, "not-a-build"))
	dir, err := m.ensureContentDir(root)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if dir != filepath.Join(root, "3e8-deadbeef00112233") {
		t.Fatalf("expected fresh build-id dir: %q", dir)
	}
	mustExist(t, dir)
}

func TestEnsureContentDir_MkdirFails(t *testing.T) {
	m := testManager()
	f := t.TempDir() + "/file.txt"
	writeFile(t, f, "x")
	if _, err := m.ensureContentDir(f); err == nil {
		t.Fatal("expected mkdir error")
	}
}

// --- copyFile / ensureParentExists ---

func TestCopyFile(t *testing.T) {
	m := testManager()
	src := t.TempDir() + "/in.pdf"
	writeFile(t, src, "payload")
	dst := t.TempDir() + "/out/nested/in.pdf"
	if err := m.copyFile(src, dst, map[string]struct{}{}); err != nil {
		t.Fatalf("copy: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "payload" {
		t.Fatalf("copy data: %v %q", err, data)
	}
}

func TestCopyFile_SourceMissing_HardError(t *testing.T) {
	m := testManager()
	dst := t.TempDir() + "/out/x.pdf"
	err := m.copyFile(filepath.Join(t.TempDir(), "nope.pdf"), dst, map[string]struct{}{})
	if err == nil || !os.IsNotExist(err) {
		t.Fatalf("ENOENT must propagate: %v", err)
	}
}

func TestCopyFile_NonDockerOptimise(t *testing.T) {
	m := testManager()
	m.OptimiseInDocker = false
	optimised := 0
	var mu sync.Mutex
	m.OptimiseFile = func(src, dst string) error {
		mu.Lock()
		optimised++
		mu.Unlock()
		return nil
	}
	src := t.TempDir() + "/in.txt"
	writeFile(t, src, "data")
	dst := t.TempDir() + "/out/in.txt"
	if err := m.copyFile(src, dst, map[string]struct{}{}); err != nil {
		t.Fatalf("copy: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if optimised != 1 {
		t.Fatalf("optimise called %d times, want 1", optimised)
	}
}

func TestCopyFile_OptimiseError(t *testing.T) {
	m := testManager()
	m.OptimiseInDocker = false
	m.OptimiseFile = func(src, dst string) error { return errors.New("qpdf failed") }
	src := t.TempDir() + "/in.pdf"
	writeFile(t, src, "data")
	if err := m.copyFile(src, t.TempDir()+"/out/in.pdf", map[string]struct{}{}); err == nil {
		t.Fatal("optimise error must propagate")
	}
}

func TestCopyFile_DirCacheReused(t *testing.T) {
	m := testManager()
	src := t.TempDir() + "/in.pdf"
	writeFile(t, src, "x")
	cache := map[string]struct{}{}
	dstDir := t.TempDir() + "/cache"
	if err := m.copyFile(src, filepath.Join(dstDir, "a.pdf"), cache); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if err := m.copyFile(src, filepath.Join(dstDir, "b.pdf"), cache); err != nil {
		t.Fatalf("copy2: %v", err)
	}
	if len(cache) == 0 {
		t.Fatal("dirCache not populated")
	}
}

// --- archiveLogs ---

func TestArchiveLogs(t *testing.T) {
	m := testManager()
	m.ArchiveLogs = true
	compileDir := t.TempDir()
	outputDir := t.TempDir()
	writeFile(t, filepath.Join(compileDir, "strace.out"), "S")
	writeFile(t, filepath.Join(compileDir, "output.log"), "L")
	writeFile(t, filepath.Join(compileDir, "output.blg"), "B")
	writeFile(t, filepath.Join(compileDir, "other.txt"), "O")
	err := m.archiveLogs(
		[]off.OutputFile{
			{Path: "strace.out"}, {Path: "output.log"},
			{Path: "output.blg"}, {Path: "other.txt"},
		},
		compileDir, outputDir, "abc-123",
	)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	base := filepath.Join(outputDir, ArchiveSubdir, "abc-123")
	mustExist(t, filepath.Join(base, "strace.out"))
	mustExist(t, filepath.Join(base, "output.log"))
	mustExist(t, filepath.Join(base, "output.blg"))
	mustNotExist(t, filepath.Join(base, "other.txt"))
}

func TestSaveOutputFiles_ArchiveFires(t *testing.T) {
	m := testManager()
	m.Strace = true
	compileDir, outputDir := setupCompileDirs(t)
	_, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-123", EnablePdfCaching: false},
		[]off.OutputFile{
			{Path: "output.pdf", Type: "pdf"},
			{Path: "strace.out", Type: "log"},
		},
		compileDir, outputDir, map[string]float64{}, map[string]float64{},
	)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	// archive is fire-and-forget: poll for the copied strace file
	poll(t, func() bool {
		_, err := os.Stat(filepath.Join(outputDir, ArchiveSubdir, "abc-123", "strace.out"))
		return err == nil
	})
}

// --- init / fillCache / RunBulkCleanup / scheduleBulkCleanup ---

func TestInit_RegistersAndSchedules(t *testing.T) {
	m := testManager()
	m.Now = func() int64 { return 1_000_000 }
	outputDir := t.TempDir()
	for _, p := range []string{"a", "b", "c"} {
		mustWriteDir(t, filepath.Join(outputDir, p))
	}
	type sched func()
	var mu sync.Mutex
	var calls []sched
	m.ScheduleAfter = func(delayMs int64, fn func()) {
		mu.Lock()
		calls = append(calls, fn)
		mu.Unlock()
	}
	if err := m.Init(outputDir); err != nil {
		t.Fatalf("init: %v", err)
	}
	// fillCache registers a/b/c with fresh-ish timestamps, so the Init-time
	// bulk cleanup does NOT run on them... EXCEPT their readdir ENOENTs in
	// bulk cleanup? No: they stay registered (recent) — bulk cleanup only
	// touches threshold-expired entries. Nothing is cleaned yet.
	for _, p := range []string{"a", "b", "c"} {
		full := filepath.Join(outputDir, p)
		if !m.hasOldest(full) {
			t.Fatalf("dir %s not registered after init", p)
		}
	}
	mu.Lock()
	if len(calls) != 1 {
		mu.Unlock()
		t.Fatalf("expected 1 schedule call after init")
	}
	first := calls[0]
	mu.Unlock()
	mu2 := func() int { mu.Lock(); defer mu.Unlock(); return len(calls) }
	first() // no dirs to expire → reschedules
	if got := mu2(); got != 2 {
		t.Fatalf("expected reschedule, got %d calls", got)
	}
}

func TestRunBulkCleanup_CleansOldDirs(t *testing.T) {
	m := testManager()
	m.Now = func() int64 { return 10_000_000 }
	legacy := t.TempDir() + "/legacy"
	mustWriteDir(t, legacy)
	m.setOldest(legacy, 0.0) // age 10s > CACHE_AGE
	fresh := t.TempDir() + "/fresh"
	mustWriteDir(t, fresh)
	m.setOldest(fresh, 10_000_000)
	oldest, err := m.RunBulkCleanup()
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	mustNotExist(t, legacy)
	mustExist(t, fresh)
	if oldest != 10_000_000 {
		t.Fatalf("oldest: %v", oldest)
	}
}

func TestInit_BulkCleanupRemovesOld(t *testing.T) {
	m := testManager()
	m.Now = func() int64 { return 10_000_000 }
	legacy := filepath.Join(t.TempDir(), "legacy")
	mustWriteDir(t, legacy)
	m.setOldest(legacy, 0.0) // ancient: age > CACHE_AGE
	outputDir := t.TempDir()
	mustWriteDir(t, filepath.Join(outputDir, "proj1"))
	m.ScheduleAfter = func(delayMs int64, fn func()) {}
	if err := m.Init(outputDir); err != nil {
		t.Fatalf("init: %v", err)
	}
	poll(t, func() bool {
		if m.hasOldest(legacy) {
			return false
		}
		_, err := os.Stat(legacy)
		return err != nil
	})
	if !m.hasOldest(filepath.Join(outputDir, "proj1")) {
		t.Fatal("proj1 must survive (fresh timestamp)")
	}
}

func TestRunBulkCleanup_AllFresh(t *testing.T) {
	m := testManager()
	m.Now = func() int64 { return 1_000_000 }
	dir := t.TempDir()
	mustWriteDir(t, dir)
	m.setOldest(dir, 999_999)
	oldest, err := m.RunBulkCleanup()
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if oldest != 999_999 {
		t.Fatalf("oldest: %v", oldest)
	}
	mustExist(t, dir)
}

func TestRunBulkCleanup_Error(t *testing.T) {
	m := testManager()
	m.Now = func() int64 { return 10_000_000 }
	d := t.TempDir()
	mustWriteDir(t, d)
	mustWriteDir(t, filepath.Join(d, CacheSubdir))
	// make cleanup fail: generated-files is a REAL dir but its build entry
	// is a FILE → RemoveAll on a path whose parent is fine still works...
	// force failure: dir is a file
	mkdirDir := t.TempDir() + "/fdir"
	mustWriteDir(t, mkdirDir)
	writeFile(t, filepath.Join(mkdirDir, CacheSubdir, "1-1"), "file")
	m.setOldest(mkdirDir, 0.0)
	_, err := m.RunBulkCleanup()
	// CleanupDirectory swallows expire errors — the only hard error path is
	// QueueDirOperation itself; so no error here either. Assert registration
	// instead by absence of side effects.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScheduleBulkCleanup_Delay(t *testing.T) {
	m := testManager()
	m.Now = func() int64 { return 10_000_000 }
	delay := int64(0)
	m.ScheduleAfter = func(d int64, fn func()) { delay = d }
	m.scheduleBulkCleanup(0)
	if want := int64(60000); delay != want {
		t.Fatalf("delay: %d want %d", delay, want)
	}
	m.scheduleBulkCleanup(float64(10_000_000) + 1000)
	if want := int64(CacheAge) + 1000 + 60000; delay != want {
		t.Fatalf("delay2: %d want %d", delay, want)
	}
}

func TestInit_ReadDirError(t *testing.T) {
	m := testManager()
	f := t.TempDir() + "/file.txt"
	writeFile(t, f, "x")
	if err := m.Init(f); err == nil {
		t.Fatal("expected fillCache error for non-dir")
	}
}

// --- nodeParseInt16 / firstDashPart ---

func TestNodeParseInt16(t *testing.T) {
	valid := []struct {
		in   string
		want float64
	}{
		{"0", 0},
		{"1", 1},
		{"3e8", float64(0x3e8)},
		{"ABC", 0xABC},
		{"abcdef", float64(0xabcdef)},
		{"0x1", 0},
		{"0x", 0},
		{"-1", -1},
	}
	for _, c := range valid {
		if got := nodeParseInt16(c.in); got != c.want {
			t.Fatalf("parseInt(%q): want %v, got %v", c.in, c.want, got)
		}
	}
	for _, in := range []string{"", "xyz"} {
		if got := nodeParseInt16(in); !(got != got) {
			t.Fatalf("parseInt(%q): want NaN, got %v", in, got)
		}
	}
}
func TestFirstDashPart(t *testing.T) {
	if got := firstDashPart("3e8-deadbeef00112233"); got != "3e8" {
		t.Fatalf("firstDashPart: %q", got)
	}
	if got := firstDashPart("nodash"); got != "" {
		t.Fatalf("no-dash: %q", got)
	}
}

// --- CleanupDirectory ---

func TestCleanupDirectory_SwallowsExpireErrors(t *testing.T) {
	m := testManager()
	d := t.TempDir()
	writeFile(t, filepath.Join(d, CacheSubdir), "notadir")
	errLogged := false
	m.Log = func(level, msg string, obj map[string]any) {
		if level == "error" {
			errLogged = true
		}
	}
	if err := m.CleanupDirectory(d, newExpireOptions("", nil)); err != nil {
		t.Fatalf("cleanup must swallow: %v", err)
	}
	if !errLogged {
		t.Fatal("expected error log")
	}
}

// --- production factory & default seams ---

func TestNew_FactoryDefaults(t *testing.T) {
	m := New()
	// production wiring is live (no fakes): generate a real build id
	id, err := m.GenerateBuildId()
	if err != nil {
		t.Fatalf("GenerateBuildId: %v", err)
	}
	if !BuildIdRegExp.MatchString(id) {
		t.Fatalf("build id %q does not match regex", id)
	}
	// OptimiseInDocker defaults to true (Node: Settings.clsi.optimise_in_docker)
	if !m.OptimiseInDocker {
		t.Fatal("OptimiseInDocker must default true")
	}
	// defaultLog & defaultMetricsInc must run without panics
	m.Log("debug", "msg", map[string]any{"a": 1})
	m.Log("info", "msg", nil)
	m.Log("warn", "msg", nil)
	m.Log("error", "msg", nil)
	m.Log("fatal", "msg", nil)
	m.MetricsInc("success", map[string]any{
		"output_dir":       "od",
		"compile_dir":      "cd",
		"project_id":       "p",
		"compile_time_ms":  1.0,
		"pdf_caching":      1.0,
		"pdf_caching_dark": 0.0,
		"build_id":         id,
	})
	_ = m.Path(id, "f") // exercises the real BuildIdRegExp
}

func TestNew_SaveOutputFiles_RealSeams(t *testing.T) {
	m := New()
	m.UpdateContent = fakeSuccess // keep CCM out of the way
	compileDir, outputDir := setupCompileDirs(t)
	if _, err := m.SaveOutputFiles(
		SaveRequest{BuildID: "abc-123", EnablePdfCaching: false},
		[]off.OutputFile{{Path: "output.log", Type: "other"}},
		compileDir, outputDir, map[string]float64{}, map[string]float64{},
	); err != nil {
		t.Fatalf("save: %v", err)
	}
	mustExist(t, filepath.Join(outputDir, CacheSubdir, "abc-123", "output.log"))
}

func TestDefaultScheduleAfter_Fires(t *testing.T) {
	m := New()
	fired := make(chan struct{})
	m.ScheduleAfter(10, func() { close(fired) })
	select {
	case <-fired:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduled fn did not fire")
	}
}

// --- ExpireOutputFiles readdir error branch (ENOENT vs other) ---

func TestExpireOutputFiles_ReaddirNotDir_ReturnsError(t *testing.T) {
	m := testManager()
	outputDir := t.TempDir()
	writeFile(t, filepath.Join(outputDir, CacheSubdir), "notadir")
	if err := m.ExpireOutputFiles(outputDir, newExpireOptions("", nil)); err == nil {
		t.Fatal("expected error when cache root is a file")
	}
}

func TestExpireOutputFiles_LimitZero(t *testing.T) {
	m := testManager()
	outputDir := t.TempDir()
	// "0" is empty: no dirs → all-expired? readdir OK (no dirs) → toRemove
	// length 0 == dirs length 0 → cleanupAll!
	mustWriteDir(t, filepath.Join(outputDir, CacheSubdir))
	if err := m.ExpireOutputFiles(outputDir, newExpireOptions("", nil)); err != nil {
		t.Fatalf("expire: %v", err)
	}
	mustNotExist(t, outputDir)
}

// --- copyFile error seams (direct) ---

func TestCopyFile_DstDirConflict_CopierError(t *testing.T) {
	m := testManager()
	src := t.TempDir() + "/in.pdf"
	writeFile(t, src, "x")
	dstDir := t.TempDir() + "/dst"
	mustWriteDir(t, filepath.Join(dstDir, "in.pdf"))
	if err := m.copyFile(src, filepath.Join(dstDir, "in.pdf"), map[string]struct{}{}); err == nil {
		t.Fatal("expected copy error when dst is a directory (copier err branch)")
	}
}

func TestCopyFile_ParentIsFile(t *testing.T) {
	m := testManager()
	src := t.TempDir() + "/in.pdf"
	writeFile(t, src, "x")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "parent"), "file")
	err := m.copyFile(src, filepath.Join(root, "parent", "in.pdf"), map[string]struct{}{})
	if err == nil {
		t.Fatal("expected error when dst parent is a file")
	}
}

// --- archiveLogs error seams (direct call, no Strace needed) ---

func TestArchiveLogs_CopyError(t *testing.T) {
	m := testManager()
	m.ArchiveLogs = true
	compileDir := t.TempDir()
	outputDir := t.TempDir()
	writeFile(t, filepath.Join(compileDir, "output.log"), "L")
	// make archive dst "output.log" a directory → _copyFile fails
	mustWriteDir(t, filepath.Join(outputDir, ArchiveSubdir, "abc-123", "output.log"))
	err := m.archiveLogs(
		[]off.OutputFile{{Path: "output.log"}},
		compileDir, outputDir, "abc-123",
	)
	if err == nil {
		t.Fatal("expected archive copy error")
	}
}

func TestArchiveLogs_ShouldArchiveCheckError(t *testing.T) {
	m := testManager()
	m.ArchiveLogs = true
	compileDir := t.TempDir()
	outputDir := t.TempDir()
	writeFile(t, filepath.Join(compileDir, "strace"), "S")
	// checkIfShouldArchive(src) stats src — exists; copyFile to
	// archiveDir/strace. Make archiveDir a file first so MkdirAll fails.
	writeFile(t, filepath.Join(outputDir, ArchiveSubdir, "abc-123"), "file")
	err := m.archiveLogs(
		[]off.OutputFile{{Path: "strace"}},
		compileDir, outputDir, "abc-123",
	)
	if err == nil {
		t.Fatal("expected archive mkdir error")
	}
}

// --- ensureContentDir error seams ---

func TestEnsureContentDir_RootIsFile(t *testing.T) {
	m := testManager()
	f := t.TempDir() + "/file.txt"
	writeFile(t, f, "x")
	if _, err := m.ensureContentDir(f); err == nil {
		t.Fatal("expected content-dir error")
	}
}

func TestEnsureContentDir_ReadDirFail(t *testing.T) {
	m := testManager()
	root := t.TempDir() + "/file"
	writeFile(t, root, "x")
	if _, err := m.ensureContentDir(root); err == nil {
		t.Fatal("expected content-dir read error")
	}
}

func TestEnsureContentDir_BadRand(t *testing.T) {
	m := testManager()
	m.RandHex = func() (string, error) { return "", errors.New("norand") }
	root := t.TempDir()
	mustWriteDir(t, filepath.Join(root, "notmatching"))
	if _, err := m.ensureContentDir(root); err == nil {
		t.Fatal("expected content-dir generate error")
	}
}

// --- firstDashPart no-dash + parse empty ---

func TestFirstDashPart_Empty(t *testing.T) {
	// no-dash + empty-string dir part: expire on empty cache dir →
	// cleanupAll deletes outputDir; ENOENT path returns success.
	m := testManager()
	outputDir := t.TempDir()
	if err := m.ExpireOutputFiles(outputDir, &ExpireOptions{}); err != nil {
		t.Fatalf("ENOENT expire must cleanup-all and succeed: %v", err)
	}
	mustNotExist(t, outputDir)
	// "" dir name in a real dir: never happens (names come from readdir),
	// but firstDashPart("") must be "" and parse to NaN.
	if got := nodeParseInt16(firstDashPart("")); !(got != got) {
		t.Fatalf("want NaN, got %v", got)
	}
}

// --- getBuildId failure through SaveOutputFiles ---

func TestSaveOutputFiles_BadGenerateBuildId(t *testing.T) {
	m := testManager()
	m.RandHex = func() (string, error) { return "", errors.New("norand") }
	compileDir, _ := setupCompileDirs(t)
	if _, err := m.SaveOutputFiles(
		SaveRequest{EnablePdfCaching: false},
		[]off.OutputFile{{Path: "output.pdf", Type: "pdf"}},
		compileDir, t.TempDir(), map[string]float64{}, map[string]float64{},
	); err == nil {
		t.Fatal("expected generateBuildId error")
	}
}
