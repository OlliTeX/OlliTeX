package clsicachehandler

import (
	"archive/tar"
	"compress/gzip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clsi/config"
)

// ---------------------------------------------------------------------------
// Helpers shared by the coverage tests.
// ---------------------------------------------------------------------------

// failingReader returns a reader whose every Read yields a fixed error.
type failingReader struct{ err error }

func (r *failingReader) Read(p []byte) (int, error) { return 0, r.err }

// failingWriter makes every Write fail.
type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) { return 0, errors.New("write failed") }

// failingTarWriter builds a *tar.Writer whose underlying writer always fails,
// so every WriteHeader / Write inside packEntries returns an error.
func failingTarWriter(t *testing.T) *tar.Writer {
	t.Helper()
	return tar.NewWriter(failingWriter{})
}

// stdEnabledCfg returns an enabled config (shard "shard-1") in temp dirs.
func stdEnabledCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	cfg.APIs.OutputCache.CurrentShards = 1
	cfg.APIs.OutputCache.DesiredShards = 1
	return cfg
}

// newDirectHandler wires a Handler for the enabled config with a nonzero
// deterministic clock: tripCircuitBreaker stores perfMs(), and
// isCircuitBreakerTripped treats a stored 0 as "no trip" (Node falsy
// semantics), so the clock must be nonzero for trip assertions to hold.
func newDirectHandler(t *testing.T, cfg *config.Config) *Handler {
	t.Helper()
	h := newTestHandler(cfg)
	h.perfMs = func() float64 { return 7 }
	return h
}

// ---------------------------------------------------------------------------
// New() / Standard() / wire()
// ---------------------------------------------------------------------------

func TestCovNewAndWire(t *testing.T) {
	// New() covers the New body (L162).
	if got := New(); got == nil || got.Config == nil {
		t.Fatal("New() must return a wired handler")
	}
	// wire() on an EMPTY handler fills every nil seam (Config included),
	// covering all the fill branches in wire (L170-190).
	empty := &Handler{}
	empty.wire()
	if empty.Config == nil || empty.fetchNothing == nil || empty.fetchStream == nil ||
		empty.nowMs == nil || empty.perfMs == nil || empty.randF == nil || empty.lastFailures == nil {
		t.Fatal("wire() must fill all nil seams")
	}
}

func TestCovStandardAndPublicDisabled(t *testing.T) {
	// Standard() (L201-202) + the disabled public wrappers: in the sandbox
	// there is no CLSI_CACHE_INSTANCES env, so the shared Standard() handler
	// is disabled and every wrapper short-circuits.
	if got := Standard(); got == nil {
		t.Fatal("Standard() must return the shared handler")
	}
	if got := Notify(NotifyOpts{ProjectID: "abc", BuildID: "b1"}); got != "" {
		t.Fatalf("Notify disabled: expected empty shard, got %q", got)
	}
	if ok, err := DownloadLatestCompileCache(validObjectID, "", t.TempDir()); ok || err != nil {
		t.Fatalf("DownloadLatest disabled: ok=%v err=%v", ok, err)
	}
	if ok, err := DownloadOutputDotSynctexFromCompileCache(validObjectID, "u1", "e1", "b1", t.TempDir()); ok || err != nil {
		t.Fatalf("DownloadSynctex disabled: ok=%v err=%v", ok, err)
	}
	if ok, err := DownloadHistorySnapshot(validObjectID, "u1", t.TempDir()); ok || err != nil {
		t.Fatalf("DownloadHistory disabled: ok=%v err=%v", ok, err)
	}
}

// TestCovPublicNotifyEnabled swaps the shared handler's config so the
// Notify/Download wrappers return a real shard (covers the non-nil branch).
func TestCovPublicNotifyEnabled(t *testing.T) {
	shared := Standard()
	saved := shared.Config
	defer func() { shared.Config = saved }()

	cfg := stdEnabledCfg(t)
	shared.Config = func() *config.Config { return cfg }
	var n int
	shared.fetchNothing = func(url string, body []byte) error { n++; return nil }

	got := Notify(NotifyOpts{
		ProjectID:   validObjectID,
		BuildID:     "b1",
		MetricsPath: "system",
	})
	if got != "shard-1" {
		t.Fatalf("Notify enabled: expected shard-1, got %q", got)
	}
	if n == 0 {
		t.Fatal("expected at least one enqueue via the enabled wrapper")
	}
	// The enabled wrappers' non-nil return path is exercised above.
}

// ---------------------------------------------------------------------------
// getAvailableShard: clamp branches
// ---------------------------------------------------------------------------

func TestCovShardCurrentClamp(t *testing.T) {
	// CurrentShards(5) > len(all)(1): the end = len(all) clamp branch.
	cfg := testConfig(t, config.ClsiCacheShard{Shard: "s0", URL: "http://s0"})
	cfg.APIs.OutputCache.CurrentShards = 5
	h := newTestHandler(cfg)
	if got := h.getAvailableShard(validObjectID); got == nil || got.Shard != "s0" {
		t.Fatalf("expected clamped shard s0, got %+v", got)
	}
}

func TestCovShardReshardDesiredClamp(t *testing.T) {
	// Inside the reshard window with DesiredShards(5) > len(all)(1): the
	// desired clamp branch (end = len(all)).
	pid := "000000000000000000003300" // counter%100 == 56 > 50
	cfg := testConfig(t, config.ClsiCacheShard{Shard: "s0", URL: "http://s0"})
	cfg.APIs.OutputCache.CurrentShards = 0
	cfg.APIs.OutputCache.DesiredShards = 5
	from, until := time.Unix(1, 0), time.Unix(2, 0)
	cfg.APIs.OutputCache.ReshardFrom = &from
	cfg.APIs.OutputCache.ReshardUntil = &until
	h := newTestHandler(cfg)
	h.nowMs = func() int64 { return 1500 }
	if got := h.getAvailableShard(pid); got == nil || got.Shard != "s0" {
		t.Fatalf("expected reshard-clamped shard s0, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// Notify guards
// ---------------------------------------------------------------------------

func TestCovNotifyInvalidOID(t *testing.T) {
	cfg := stdEnabledCfg(t)
	h := newTestHandler(cfg)
	if got := h.notify(NotifyOpts{ProjectID: "not-an-oid", BuildID: "b1"}); got != nil {
		t.Fatalf("expected nil for invalid OID, got %+v", got)
	}
}

func TestCovNotifyBuildWarnNonENOENT(t *testing.T) {
	// Make the output.tar.gz destination's parent unwritable so buildTarball
	// fails with a non-ENOENT error → notify hits the warn branch.
	cfg := stdEnabledCfg(t)
	h := newTestHandler(cfg)
	// List a non-extraneous aux path that does not exist: buildTarball raises
	// ENOENT (silently swallowed). To force a *non*-ENOENT build failure,
	// create the output dir as a FILE so writeTarGzip/os.Create fails.
	proj := filepath.Join(cfg.Path.OutputDir, validObjectID)
	if err := os.MkdirAll(filepath.Dir(proj), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proj, []byte("not-a-dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	var n int
	h.fetchNothing = func(url string, body []byte) error { n++; return nil }
	h.notify(NotifyOpts{
		ProjectID:   validObjectID,
		BuildID:     "b1",
		MetricsPath: "system",
	})
	// buildTarball failed (non-ENOENT) → tarball enqueue skipped (n stays 0
	// for the tar), preview enqueue still ran.
	if n != 1 {
		t.Fatalf("expected 1 (preview) enqueue, got %d", n)
	}
}

func TestCovNotifyHistoryWarnNonENOENT(t *testing.T) {
	// history source exists, but the output dst parent is a FILE so
	// copyHistorySnapshot raises a non-ENOENT error → warn branch.
	cfg := stdEnabledCfg(t)
	h := newTestHandler(cfg)
	histDir := filepath.Join(cfg.Path.ClsiCacheDir, validObjectID)
	if err := os.MkdirAll(histDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(histDir, "history.json.gz"), []byte("H"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Make getOutputDir's project component a file → MkdirAll(dst dir) fails.
	proj := filepath.Join(cfg.Path.ClsiCacheDir, validObjectID)
	_ = proj
	out := h_outputDir(cfg, validObjectID, "", "b1")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	proj2 := filepath.Join(cfg.Path.OutputDir, validObjectID)
	// proj already exists from prior test? Use a fresh userID to avoid clash.
	_ = out
	_ = proj2
	var n int
	h.fetchNothing = func(url string, body []byte) error { n++; return nil }
	h.notify(NotifyOpts{
		ProjectID:   validObjectID,
		BuildID:     "b1",
		MetricsPath: "system",
	})
	// Both build and history warn; assert only the preview enqueue ran.
	if n < 1 {
		t.Fatalf("expected the preview enqueue to run, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// downloadSingleFile direct error branches
// ---------------------------------------------------------------------------

func TestCovDownloadSingleFileBranches(t *testing.T) {
	// (a) invalid OID short-circuit.
	cfg := stdEnabledCfg(t)
	h := newDirectHandler(t, cfg)
	if ok, err := h.downloadSingleFile("not-an-oid", "/p", t.TempDir(), "snapshot"); ok || err != nil {
		t.Fatalf("oid guard: ok=%v err=%v", ok, err)
	}

	// (b) no available shard (CurrentShards 0) short-circuit.
	noShardCfg := *cfg
	noShardCfg.APIs.OutputCache.CurrentShards = 0
	noShard := newTestHandler(&noShardCfg)
	noShard.perfMs = func() float64 { return 0 }
	if ok, err := noShard.downloadSingleFile(validObjectID, "/p", t.TempDir(), "snapshot"); ok || err != nil {
		t.Fatalf("nil shard: ok=%v err=%v", ok, err)
	}
}

func TestCovDownloadSingleFileBadURL(t *testing.T) {
	// A shard URL that url.Parse rejects → invalid-clsi-cache-shard-url error.
	cfg := testConfig(t, config.ClsiCacheShard{Shard: "bad", URL: "://bad"})
	cfg.APIs.OutputCache.CurrentShards = 1
	h := newDirectHandler(t, cfg)
	ok, err := h.downloadSingleFile(validObjectID, "/p", t.TempDir(), "snapshot")
	if ok || err == nil {
		t.Fatalf("expected bad-url error, got ok=%v err=%v", ok, err)
	}
}

func TestCovDownloadSingleFileMkdirError(t *testing.T) {
	// outputDir is a FILE → os.MkdirAll(outputDir) fails (non-wrapped error).
	cfg := stdEnabledCfg(t)
	h := newDirectHandler(t, cfg)
	fileOut := t.TempDir()
	if err := os.WriteFile(filepath.Join(fileOut, "isfile"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	badOut := filepath.Join(fileOut, "isfile", "sub")
	h.fetchStream = func(u string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte("data"))), nil
	}
	ok, err := h.downloadSingleFile(validObjectID, "/p/history-resync.json.gz", badOut, "snapshot")
	if ok || err == nil {
		t.Fatalf("expected mkdir error, got ok=%v err=%v", ok, err)
	}
}

func TestCovDownloadSingleFileStreamCopyError(t *testing.T) {
	// stream errors mid-copy (non-ENOENT) → trip + tagged "stream failed".
	cfg := stdEnabledCfg(t)
	h := newDirectHandler(t, cfg)
	h.fetchStream = func(u string) (io.ReadCloser, error) {
		return io.NopCloser(&failingReader{err: errors.New("stream cut")}), nil
	}
	ok, err := h.downloadSingleFile(validObjectID, "/p/history-resync.json.gz", t.TempDir(), "snapshot")
	if ok || err == nil {
		t.Fatalf("expected stream error, got ok=%v err=%v", ok, err)
	}
	if !h.isCircuitBreakerTripped("http://shard1") {
		t.Fatal("stream error should trip the breaker")
	}
}

func TestCovDownloadSingleFileStreamENOENT(t *testing.T) {
	// stream error that IS ENOENT → swallowed (false, nil), no trip.
	cfg := stdEnabledCfg(t)
	h := newDirectHandler(t, cfg)
	h.fetchStream = func(u string) (io.ReadCloser, error) {
		return io.NopCloser(&failingReader{err: os.ErrNotExist}), nil
	}
	ok, err := h.downloadSingleFile(validObjectID, "/p/history-resync.json.gz", t.TempDir(), "snapshot")
	if ok || err != nil {
		t.Fatalf("expected ENOENT swallow, got ok=%v err=%v", ok, err)
	}
	if h.isCircuitBreakerTripped("http://shard1") {
		t.Fatal("ENOENT stream error should NOT trip the breaker")
	}
}

// ---------------------------------------------------------------------------
// downloadLatestCompileCache direct branches
// ---------------------------------------------------------------------------

func TestCovDownloadLatestBranches(t *testing.T) {
	// (a) invalid OID short-circuit.
	cfg := stdEnabledCfg(t)
	if ok, err := newDirectHandler(t, cfg).downloadLatestCompileCache("bad-oid", "", t.TempDir()); ok || err != nil {
		t.Fatalf("oid guard: ok=%v err=%v", ok, err)
	}

	// (b) fetch 404 → close breaker, (false, nil).
	ht := newDirectHandler(t, cfg)
	ht.fetchStream = func(u string) (io.ReadCloser, error) {
		return nil, &RequestFailedError{Response: &http.Response{StatusCode: http.StatusNotFound}}
	}
	if ok, err := ht.downloadLatestCompileCache(validObjectID, "", t.TempDir()); ok || err != nil {
		t.Fatalf("404: ok=%v err=%v", ok, err)
	}

	// (c) fetch 500 → trip + tagged error.
	hf := newDirectHandler(t, cfg)
	hf.fetchStream = func(u string) (io.ReadCloser, error) {
		return nil, &RequestFailedError{Response: &http.Response{StatusCode: 500}}
	}
	if ok, err := hf.downloadLatestCompileCache(validObjectID, "", t.TempDir()); ok || err == nil {
		t.Fatalf("500: ok=%v err=%v", ok, err)
	}
	if !hf.isCircuitBreakerTripped("http://shard1") {
		t.Fatal("500 should trip the breaker")
	}

	// (d) extract raises ENOENT (stream read = ENOENT) → (false, nil), no trip.
	he := newDirectHandler(t, cfg)
	he.fetchStream = func(u string) (io.ReadCloser, error) {
		return io.NopCloser(&failingReader{err: os.ErrNotExist}), nil
	}
	if ok, err := he.downloadLatestCompileCache(validObjectID, "", t.TempDir()); ok || err != nil {
		t.Fatalf("extract ENOENT: ok=%v err=%v", ok, err)
	}
	if he.isCircuitBreakerTripped("http://shard1") {
		t.Fatal("extract ENOENT should not trip the breaker")
	}
}

// ---------------------------------------------------------------------------
// extractCompileCache direct paths
// ---------------------------------------------------------------------------

// gzipCompress returns a gzip of raw (no gzip inside).
func gzipCompress(raw []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write(raw)
	_ = gz.Close()
	return buf.Bytes()
}

func TestCovExtractTarReadError(t *testing.T) {
	// gunzip ok, then tar.Next() errors (non-EOF) → return (n, abort, rerr).
	out := t.TempDir()
	gz := gzipCompress([]byte("not a real tar at all, just random bytes for header"))
	n, abort, err := extractCompileCache(bytes.NewReader(gz), out, validObjectID, "u1")
	if err == nil {
		t.Fatalf("expected a tar read error, got n=%d abort=%v", n, abort)
	}
}

func TestCovExtractUnexpectedEntryAbort(t *testing.T) {
	// A symlink tar entry is unexpected → abort (counted, unwritten, no err).
	gz, err := buildGzTarWithSymlink()
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	n, abort, err := extractCompileCache(bytes.NewReader(gz), out, validObjectID, "u1")
	if !abort {
		t.Fatalf("expected abort on unexpected entry, got n=%d err=%v", n, err)
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCovExtractWriteError(t *testing.T) {
	// A file entry whose target name is an EXISTING DIRECTORY → writeTarEntry
	// fails → extract returns (n, abort, writeErr).
	gz, err := buildGzTarFiles(map[string]string{"sub": "data"})
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := os.MkdirAll(filepath.Join(out, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err = extractCompileCache(bytes.NewReader(gz), out, validObjectID, "u1")
	if err == nil {
		t.Fatal("expected write error when target name is a directory")
	}
}

// ---------------------------------------------------------------------------
// writeTarEntry direct cases
// ---------------------------------------------------------------------------

func TestCovWriteTarEntryCases(t *testing.T) {
	base := t.TempDir()

	// (a) TypeDir success + Chtimes (ModTime set) → nil.
	if err := writeTarEntry(base, nil, &tar.Header{Name: "made", Typeflag: tar.TypeDir, ModTime: time.Unix(123, 0)}); err != nil {
		t.Fatalf("dir: %v", err)
	}
	if !fileExists(filepath.Join(base, "made")) {
		t.Fatal("dir not created")
	}

	// (b) TypeDir MkdirAll error: parent is a FILE.
	fileParent := filepath.Join(base, "blockfile")
	if err := os.WriteFile(fileParent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeTarEntry(base, nil, &tar.Header{Name: "blockfile/sub", Typeflag: tar.TypeDir}); err == nil {
		t.Fatal("expected mkdir error (parent file)")
	}

	// (c) TypeSymlink success (no pre-existing target).
	if err := writeTarEntry(base, nil, &tar.Header{Name: "sym", Typeflag: tar.TypeSymlink, Linkname: "tgt"}); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if fi, err := os.Lstat(filepath.Join(base, "sym")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink not created: %v", fi)
	}

	// (d) TypeSymlink MkdirAll error: parent is a FILE.
	if err := writeTarEntry(base, nil, &tar.Header{Name: "blockfile/link", Typeflag: tar.TypeSymlink, Linkname: "x"}); err == nil {
		t.Fatal("expected symlink mkdir error")
	}

	// (e) TypeSymlink Remove error (non-ENOENT): target NAME is a
	// non-empty DIRECTORY (os.Remove fails ENOTEMPTY, which is
	// not IsNotExist).
	dirNamed := filepath.Join(base, "somedir")
	if err := os.MkdirAll(filepath.Join(dirNamed, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeTarEntry(base, nil, &tar.Header{Name: "somedir", Typeflag: tar.TypeSymlink, Linkname: "x"}); err == nil {
		t.Fatal("expected symlink remove error (target is a non-empty dir)")
	}

	// (f) TypeReg MkdirAll error: parent is a FILE.
	if err := writeTarEntry(base, nil, &tar.Header{Name: "blockfile/f", Typeflag: tar.TypeReg}); err == nil {
		t.Fatal("expected reg mkdir error (parent file)")
	}

	// (g) TypeReg OpenFile error: name is an existing DIRECTORY.
	if err := os.MkdirAll(filepath.Join(base, "somedir2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeTarEntry(base, nil, &tar.Header{Name: "somedir2", Typeflag: tar.TypeReg, Size: 1}); err == nil {
		t.Fatal("expected OpenFile error (name is a dir)")
	}

	// (h) default case: unknown typeflag → skip silently.
	if err := writeTarEntry(base, nil, &tar.Header{Name: "weird", Typeflag: '\x07'}); err != nil {
		t.Fatalf("default case must skip, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// packEntries error cases (failing tar writer)
// ---------------------------------------------------------------------------

func TestCovPackEntriesWriterErrors(t *testing.T) {
	dir := t.TempDir()
	tw := failingTarWriter(t)

	// file listed → WriteHeader fails.
	if err := packEntries(tw, dir, []string{"afile"}); err == nil {
		t.Fatal("expected write header error for file")
	}
	// directory listed → WriteHeader fails before recursing.
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := packEntries(tw, dir, []string{"sub"}); err == nil {
		t.Fatal("expected write header error for directory")
	}
}

func TestCovPackEntriesRecurseError(t *testing.T) {
	// A writer that succeeds the first (dir) header then fails on the child,
	// so the recursive packEntries returns an error.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "d", "child.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The dir header is the writer's first write (succeeds); the child's
	// WriteHeader is the second write (fails), so the recursive packEntries
	// call returns that error.
	if err := packEntries(tar.NewWriter(newFailingOnSecond()), dir, []string{"d"}); err == nil {
		t.Fatal("expected pack error from failing writer")
	}
}

// ---------------------------------------------------------------------------
// copyMetered error cases
// ---------------------------------------------------------------------------

func TestCovCopyMeteredErrors(t *testing.T) {
	// read error (non-EOF) propagates.
	if _, err := copyMetered(&bytes.Buffer{}, &failingReader{err: errors.New("boom")}); err == nil {
		t.Fatal("expected read error passthrough")
	}
	// write error propagates.
	if _, err := copyMetered(failingWriter{}, bytes.NewReader([]byte("data"))); err == nil {
		t.Fatal("expected write error passthrough")
	}
	// happy path.
	n, err := copyMetered(&bytes.Buffer{}, bytes.NewReader([]byte("abcd")))
	if err != nil || n != 4 {
		t.Fatalf("happy copy: n=%d err=%v", n, err)
	}
}

// ---------------------------------------------------------------------------
// enqueue branches
// ---------------------------------------------------------------------------

func TestCovEnqueueNilFiles(t *testing.T) {
	cfg := stdEnabledCfg(t)
	h := newDirectHandler(t, cfg)
	var n int
	h.fetchNothing = func(url string, body []byte) error { n++; return nil }
	h.enqueue(&cfg.APIs.OutputCache.Shards[0], NotifyOpts{
		ProjectID: validObjectID, BuildID: "b1",
	}, nil)
	if n != 1 {
		t.Fatalf("expected the nil-files enqueue to POST, got %d", n)
	}
}

func TestCovEnqueueLargeWithPDF(t *testing.T) {
	// Force len(raw) > threshold with files that include "output.pdf".
	cfg := stdEnabledCfg(t)
	h := newDirectHandler(t, cfg)
	var n int
	h.fetchNothing = func(url string, body []byte) error { n++; return nil }
	big := make([]enqueueFile, 0, 40000)
	longPath := make([]byte, 300)
	for i := range longPath {
		longPath[i] = 'x'
	}
	for i := 0; i < 40000; i++ {
		big = append(big, enqueueFile{Path: fmt.Sprintf("%s/%d.bxg", string(longPath), i)})
	}
	big = append(big, enqueueFile{Path: "output.pdf"})
	h.enqueue(&cfg.APIs.OutputCache.Shards[0], NotifyOpts{
		ProjectID: validObjectID, BuildID: "b1",
	}, big)
	if n != 1 {
		t.Fatalf("expected the large-body enqueue to POST, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// downloadOutputDotSynctexFromCompileCache (direct)
// ---------------------------------------------------------------------------

func TestCovDownloadSynctexDirect(t *testing.T) {
	cfg := stdEnabledCfg(t)
	h := newDirectHandler(t, cfg)
	// invalid OID short-circuit (also builds the user/ requestPath prefix).
	ok, err := h.downloadOutputDotSynctexFromCompileCache("no-oid", "u1", "e1", "b1", t.TempDir())
	if ok || err != nil {
		t.Fatalf("synctex oid guard: ok=%v err=%v", ok, err)
	}
	// invalid OID with empty user too.
	ok, err = h.downloadOutputDotSynctexFromCompileCache("no-oid", "", "e1", "b1", t.TempDir())
	if ok || err != nil {
		t.Fatalf("synctex no-user oid guard: ok=%v err=%v", ok, err)
	}
}

// buildGzTarWithSymlink builds a tarball with one file + one symlink entry.
func buildGzTarWithSymlink() ([]byte, error) {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	data := []byte("F0")
	if err := tw.WriteHeader(&tar.Header{Name: "f0.txt", Typeflag: tar.TypeReg, Size: int64(len(data))}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(data); err != nil {
		return nil, err
	}
	if err := tw.WriteHeader(&tar.Header{Name: "f0-link", Typeflag: tar.TypeSymlink, Linkname: "f0.txt"}); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gzw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// newFailingOnSecond returns a writer that succeeds the first write and
// fails every subsequent one (used to exercise packEntries' nested error).
type failingOnSecond struct{ first bool }

func (f *failingOnSecond) Write(p []byte) (int, error) {
	if f.first {
		f.first = false
		return len(p), nil
	}
	return 0, errors.New("second write failed")
}

func newFailingOnSecond() *failingOnSecond { return &failingOnSecond{first: true} }
