// Tests for the CLSICacheHandler port.
//
// Semantics are pinned to services/clsi/app/js/CLSICacheHandler.js.
package clsicachehandler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	oerrors "clsi/errors"
	"clsi/config"
	"clsi/outputfilefinder"
	"clsi/outputcachemanager"
	"clsi/resourcewriter"
)

func TestMain(m *testing.M) {
	// buildTarball uses IsExtraneousFile, which lazy-builds a precious-file
	// matcher from config; establish it from the default (empty) pattern so
	// the matcher never derives config from env (which panics without env).
	resourcewriter.InitPreciousFileMatcher("")
	// The Go binary bundles DockerRunner, so sandboxed compiles are
	// mandatory and config.New() requires the sandbox compiles host dir.
	// Set it and prime the cached singleton so Standard()/wire() never
	// re-derive the config from env mid-test.
	os.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", os.TempDir()+"-clsi-test-compiles")
	_ = config.ForTest()
	os.Exit(m.Run())
}


func testConfig(t *testing.T, shards ...config.ClsiCacheShard) *config.Config {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.Path.OutputDir = filepath.Join(dir, "output")
	cfg.Path.ClsiCacheDir = filepath.Join(dir, "clsi-cache")
	cfg.APIs.OutputCache.Enabled = len(shards) > 0
	cfg.APIs.OutputCache.Shards = shards
	if len(shards) > 0 {
		cfg.APIs.OutputCache.CurrentShards = len(shards)
		cfg.APIs.OutputCache.DesiredShards = len(shards)
	}
	cfg.APIs.Compile.DownloadHost = "http://downloadhost.example"
	cfg.APIs.Compile.ServerID = "test-server"
	return cfg
}

func newTestHandler(cfg *config.Config) *Handler {
	h := &Handler{
		Config:       func() *config.Config { return cfg },
		nowMs:        func() int64 { return 0 },
		perfMs:       func() float64 { return 0 },
		randF:        func() float64 { return 0.5 },
		lastFailures: map[string]float64{},
	}
	h.wire()
	return h
}

const validObjectID = "abcdefabcdefabcdefabcdef"

func h_outputDir(cfg *config.Config, projectID, userID, buildID string) string {
	project := projectID
	if userID != "" {
		project = projectID + "-" + userID
	}
	return filepath.Join(cfg.Path.OutputDir, project, outputcachemanager.CacheSubdir, buildID)
}

// --- getAvailableShard ---

func TestGetAvailableShardDisabled(t *testing.T) {
	cfg := testConfig(t)
	h := newTestHandler(cfg)
	if got := h.getAvailableShard(validObjectID); got != nil {
		t.Fatalf("expected nil shard, got %+v", got)
	}
}

func TestGetAvailableShardBasic(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "s0", URL: "http://s0"},
		config.ClsiCacheShard{Shard: "s1", URL: "http://s1"},
	)
	h := newTestHandler(cfg)
	got := h.getAvailableShard(validObjectID)
	if got == nil {
		t.Fatal("expected a shard")
	}
	if got.Shard != "s0" && got.Shard != "s1" {
		t.Fatalf("unexpected shard %+v", got)
	}
}

func TestGetAvailableShardCurrentShardsZero(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "s0", URL: "http://s0"},
	)
	cfg.APIs.OutputCache.CurrentShards = 0
	h := newTestHandler(cfg)
	if got := h.getAvailableShard(validObjectID); got != nil {
		t.Fatalf("expected nil shard when currentShards=0, got %+v", got)
	}
}

func TestGetAvailableShardAllTrippedReturnsNil(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "s0", URL: "http://s0"},
		config.ClsiCacheShard{Shard: "s1", URL: "http://s1"},
	)
	h := newTestHandler(cfg)
	h.perfMs = func() float64 { return 10 }
	now := 10.0
	h.lastFailures["http://s0"] = now
	h.lastFailures["http://s1"] = now
	if got := h.getAvailableShard(validObjectID); got != nil {
		t.Fatalf("expected nil when all tripped, got %+v", got)
	}
}

func TestGetAvailableShardReshardWindow(t *testing.T) {
	// pid bytes[8:12] BE must yield counter%100 = 56 (>50) so the reshard RHS
	// (=(until-now)/(until-from) = 0.5 inside the window) is exceeded.
	pid := "000000000000000000003300"
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "s0", URL: "http://s0"},
		config.ClsiCacheShard{Shard: "s1", URL: "http://s1"},
	)
	cfg.APIs.OutputCache.CurrentShards = 0 // outside window: no shards at all
	cfg.APIs.OutputCache.DesiredShards = 1 // inside window: expand to s0

	outFrom := time.Unix(2, 0) // 2000ms, future relative to now=1500
	outUntil := time.Unix(3, 0)
	cfg.APIs.OutputCache.ReshardFrom = &outFrom
	cfg.APIs.OutputCache.ReshardUntil = &outUntil
	h := newTestHandler(cfg)
	h.nowMs = func() int64 { return 1500 }

	// Outside the window (now < reshardFrom): currentShards=0 -> empty -> nil.
	if got := h.getAvailableShard(pid); got != nil {
		t.Fatalf("outside window: expected nil (no current shards), got %+v", got)
	}

	// Inside the window (from < now < until): reshard expands to desired s0.
	cfg.APIs.OutputCache.ReshardFrom = ptrTime(time.Unix(1, 0))
	cfg.APIs.OutputCache.ReshardUntil = ptrTime(time.Unix(2, 0))
	got := h.getAvailableShard(pid)
	if got == nil || got.Shard != "s0" {
		t.Fatalf("inside window: expected reshard to s0, got %+v", got)
	}
}

func ptrTime(v time.Time) *time.Time { return &v }

// --- notify + enqueue + buildTarball + copyHistorySnapshot ---

func makeOutputFiles(t *testing.T, cfg *config.Config, projectID, userID, buildID string) []outputfilefinder.OutputFile {
	t.Helper()
	dir := h_outputDir(cfg, projectID, userID, buildID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"output.pdf", "output.log", "output.synctex.gz", "main.blg", "notpdf.blg"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	size := int64(42)
	cid := "cid-1"
	return []outputfilefinder.OutputFile{
		{Path: "output.pdf", Size: &size, ContentID: &cid},
		{Path: "output.log"},
		{Path: "main.blg"},
		{Path: "notpdf.blg"},
	}
}

func TestNotifyDisabledNoEnqueue(t *testing.T) {
	cfg := testConfig(t)
	h := newTestHandler(cfg)
	called := 0
	h.fetchNothing = func(url string, body []byte) error { called++; return nil }
	h.notify(NotifyOpts{ProjectID: "not-an-object-id", MetricsPath: "x"})
	if called != 0 {
		t.Fatalf("expected no enqueue calls, got %d", called)
	}
}

func TestNotifyReturnsShardString(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	got := h.notify(NotifyOpts{
		ProjectID:   validObjectID,
		BuildID:     "b1",
		CompileGroup: "standard",
	})
	if got == nil || got.Shard != "shard-1" {
		t.Fatalf("expected shard-1, got %+v", got)
	}
}

func TestNotifyUserStandardCompileNoPreview(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	var caps []struct {
		url  string
		body []byte
	}
	h.fetchNothing = func(url string, raw []byte) error {
		caps = append(caps, struct {
			url  string
			body []byte
		}{url, raw})
		return nil
	}
	files := makeOutputFiles(t, cfg, validObjectID, "u1", "b1")
	h.notify(NotifyOpts{
		ProjectID:    validObjectID,
		UserID:       "u1",
		BuildID:      "b1",
		CompileGroup: "standard",
		MetricsPath:  "",
		OutputFiles:  files,
	})
	// Standard user compile: preview skipped. History not seeded -> only
	// the tarball enqueue.
	if len(caps) != 1 {
		t.Fatalf("expected 1 enqueue (tarball), got %d", len(caps))
	}
	var body enqueueBody
	if err := json.Unmarshal(caps[0].body, &body); err != nil {
		t.Fatalf("bad enqueue body: %v", err)
	}
	if body.DownloadHost != "http://downloadhost.example" || body.ClsisServerID != "test-server" {
		t.Fatalf("server config not propagated: %+v", body)
	}
	if len(body.Files) != 1 || body.Files[0].Path != "output.tar.gz" {
		t.Fatalf("expected [output.tar.gz], got %+v", body.Files)
	}
	if !fileExists(filepath.Join(h_outputDir(cfg, validObjectID, "u1", "b1"), "output.tar.gz")) {
		t.Fatal("output.tar.gz not written")
	}
}

func TestNotifyNonUserCompilePreviewEnqueue(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	var caps []struct {
		url  string
		body []byte
	}
	h.fetchNothing = func(url string, raw []byte) error {
		caps = append(caps, struct {
			url  string
			body []byte
		}{url, raw})
		return nil
	}
	files := makeOutputFiles(t, cfg, validObjectID, "", "b1")
	h.notify(NotifyOpts{
		ProjectID:    validObjectID,
		BuildID:      "b1",
		CompileGroup: "standard",
		MetricsPath:  "system-compile",
		OutputFiles:  files,
	})
	if len(caps) != 2 {
		t.Fatalf("expected 2 enqueues (preview, tarball), got %d", len(caps))
	}
	var preview enqueueBody
	if err := json.Unmarshal(caps[0].body, &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Files) != 4 {
		t.Fatalf("expected 5 preview files, got %+v", preview.Files)
	}
	if preview.Files[0].Path != "output.pdf" || preview.Files[0].Size == nil {
		t.Fatalf("first preview file should be lean output.pdf: %+v", preview.Files[0])
	}
	if preview.Files[2].Path != "main.blg" || preview.Files[2].Size != nil {
		t.Fatalf("main.blg should be path-only: %+v", preview.Files[3])
	}
	var generic map[string]any
	if err := json.Unmarshal(caps[0].body, &generic); err != nil {
		t.Fatal(err)
	}
	// Node JSON.stringify omits undefined values: empty userId/editorId and
	// nil stats/timings/options are absent from the envelope.
	for _, k := range []string{"projectId", "buildId", "files",
		"downloadHost", "clsiServerId", "compileGroup"} {
		if _, ok := generic[k]; !ok {
			t.Fatalf("missing key %q in envelope", k)
		}
	}
	for _, k := range []string{"userId", "editorId", "stats", "timings", "options"} {
		if _, ok := generic[k]; ok {
			t.Fatalf("unexpected key %q in envelope (Node omits undefined)", k)
		}
	}
	// The second (tarball) enqueue should carry output.tar.gz.
	var tarball enqueueBody
	if err := json.Unmarshal(caps[1].body, &tarball); err != nil {
		t.Fatal(err)
	}
	if len(tarball.Files) != 1 || tarball.Files[0].Path != "output.tar.gz" {
		t.Fatalf("tarball envelope wrong: %+v", tarball.Files)
	}
}

func TestNotifyHistoryEnqueueWhenHistorySnapshotPresent(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	cacheDir := filepath.Join(cfg.Path.ClsiCacheDir, validObjectID+"-u1")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "history.json.gz"), []byte("HIST"), 0o644); err != nil {
		t.Fatal(err)
	}
	var caps []struct{ body []byte }
	h.fetchNothing = func(url string, raw []byte) error {
		caps = append(caps, struct{ body []byte }{raw})
		return nil
	}
	files := makeOutputFiles(t, cfg, validObjectID, "u1", "b1")
	h.notify(NotifyOpts{
		ProjectID:    validObjectID,
		UserID:       "u1",
		BuildID:      "b1",
		CompileGroup: "standard",
		OutputFiles:  files,
	})
	// tarball + history = 2 enqueues (no preview: standard user compile).
	if len(caps) != 2 {
		t.Fatalf("expected 2 enqueues, got %d", len(caps))
	}
	var hist enqueueBody
	if err := json.Unmarshal(caps[1].body, &hist); err != nil {
		t.Fatal(err)
	}
	if len(hist.Files) != 1 || hist.Files[0].Path != "history-resync.json.gz" {
		t.Fatalf("expected history-resync, got %+v", hist.Files)
	}
	got := h_outputDir(cfg, validObjectID, "u1", "b1") + "/history-resync.json.gz"
	if !fileExists(got) {
		t.Fatal("history-resync.json.gz not copied into output dir")
	}
}

func TestNotifyBuildTarballSilentENOENT(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	var n int
	h.fetchNothing = func(url string, body []byte) error { n++; return nil }
	// Listed but not on disk -> buildTarball raises ENOENT, which notify
	// swallows.
	h.notify(NotifyOpts{
		ProjectID:   validObjectID,
		BuildID:     "b1",
		CompileGroup: "system",
		MetricsPath: "x",
		OutputFiles: []outputfilefinder.OutputFile{{Path: "missing.blg"}},
	})
	// The preview enqueue (missing.blg) still happens.
	if n != 1 {
		t.Fatalf("expected 1 preview enqueue, got %d", n)
	}
}

func TestEnqueueEnvelopeFields(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	var body []byte
	h.fetchNothing = func(url string, raw []byte) error {
		body = raw
		return nil
	}
	h.enqueue(&cfg.APIs.OutputCache.Shards[0], NotifyOpts{
		ProjectID:  validObjectID,
		UserID:     "u1",
		BuildID:    "b1",
		CompileGroup: "standard",
	}, []enqueueFile{{Path: "output.tar.gz"}})
	var env map[string]any
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatal(err)
	}
	if env["projectId"] != validObjectID || env["userId"] != "u1" {
		t.Fatalf("project/user not propagated: %+v", env)
	}
}

func TestEnqueueLargeBody(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	bigFiles := make([]enqueueFile, 0, 50000)
	for i := 0; i < 50000; i++ {
		bigFiles = append(bigFiles, enqueueFile{Path: strings.Repeat("x", 256) + ".blg"})
	}
	// Should complete without error (large body is tolerated).
	h.enqueue(&cfg.APIs.OutputCache.Shards[0], NotifyOpts{
		ProjectID:  validObjectID,
		BuildID:    "b1",
		CompileGroup: "standard",
	}, bigFiles)
	if len(bigFiles) != 50000 {
		t.Fatal("fixture lost files")
	}
}

func TestEnqueueFetchErrorTripsBreaker(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	h.perfMs = func() float64 { return 7 }
	h.fetchNothing = func(url string, raw []byte) error {
		return &RequestFailedError{Response: &http.Response{StatusCode: 500}}
	}
	h.enqueue(&cfg.APIs.OutputCache.Shards[0], NotifyOpts{
		ProjectID:  validObjectID,
		BuildID:    "b1",
		CompileGroup: "standard",
	}, []enqueueFile{{Path: "output.tar.gz"}})
	if !h.isCircuitBreakerTripped("http://shard1") {
		t.Fatal("breaker should be tripped after enqueue error")
	}
}

// --- buildTarball ---

func TestBuildTarballSuccess(t *testing.T) {
	cfg := testConfig(t)
	h := newTestHandler(cfg)
	pdir := h_outputDir(cfg, "p1", "", "build1")
	if err := os.MkdirAll(filepath.Join(pdir, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Non-extraneous inputs only: buildTarball filters OUT isExtraneousFile,
	// so aux paths survive (the persistent-cache tarball). The real
	// outputFiles list is flat (files only), so cache/ is listed via the
	// file, not the directory.
	if err := os.WriteFile(filepath.Join(pdir, "x.aux"), []byte("aux"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdir, "cache", "y.aux"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := []outputfilefinder.OutputFile{{Path: "x.aux"}, {Path: "cache/y.aux"}}
	if err := h.buildTarball("p1", "", "build1", files); err != nil {
		t.Fatalf("buildTarball: %v", err)
	}
	gz, err := os.ReadFile(filepath.Join(pdir, "output.tar.gz"))
	if err != nil {
		t.Fatalf("output.tar.gz not created: %v", err)
	}
	entries, names, err := readGzippedTar(gz)
	if err != nil {
		t.Fatal(err)
	}
	// Flat file entries only, in list order.
	if len(names) != 2 || names[0] != "x.aux" || names[1] != "cache/y.aux" {
		t.Fatalf("unexpected entries: %+v", names)
	}
	if entries[0] != "file" || entries[1] != "file" {
		t.Fatalf("expected file entries, got %v (types=%v)", entries, entries)
	}
}

func TestBuildTarballTooMany(t *testing.T) {
	cfg := testConfig(t)
	h := newTestHandler(cfg)
	// .aux files survive the isExtraneousFile filter (shouldDelete=false),
	// so all 101 remain after filtering and trip the "too many" guard.
	files := make([]outputfilefinder.OutputFile, 0, 101)
	for i := 0; i < 101; i++ {
		files = append(files, outputfilefinder.OutputFile{Path: fmt.Sprintf("f%d.aux", i)})
	}
	err := h.buildTarball("p1", "", "b1", files)
	var oe *oerrors.OError
	if !errors.As(err, &oe) {
		t.Fatalf("expected OError too-many, got %v", err)
	}
	if oe.Message != "too many output files for output.tar.gz" {
		t.Fatalf("wrong OError message: %q", oe.Message)
	}
}

func TestBuildTarballMissingEntryUnlinksPartial(t *testing.T) {
	cfg := testConfig(t)
	h := newTestHandler(cfg)
	pdir := h_outputDir(cfg, "p1", "", "b1")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(pdir, "output.tar.gz")
	if err := os.WriteFile(dst, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A non-extraneous path that is listed but absent on disk -> packEntries
	// stat raises ENOENT; buildTarball unlinks the partial tarball.
	err := h.buildTarball("p1", "", "b1",
		[]outputfilefinder.OutputFile{{Path: "nope-missing.aux"}})
	if err == nil || !isENOENT(err) {
		t.Fatalf("expected ENOENT, got %v", err)
	}
	if fileExists(dst) {
		t.Fatal("dest not unlinked after error")
	}
}

// --- copyHistorySnapshot ---

func TestCopyHistorySnapshotSuccess(t *testing.T) {
	cfg := testConfig(t)
	h := newTestHandler(cfg)
	src := filepath.Join(cfg.Path.ClsiCacheDir, "p1-u1", "history.json.gz")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("HIST"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.copyHistorySnapshot("p1", "u1", "b1"); err != nil {
		t.Fatalf("copy: %v", err)
	}
	dst := filepath.Join(h_outputDir(cfg, "p1", "u1", "b1"), "history-resync.json.gz")
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal("dst not written", err)
	}
	if string(data) != "HIST" {
		t.Fatalf("dst content mismatch: %q", data)
	}
	st, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("src mode not preserved: %o", st.Mode().Perm())
	}
}

func TestCopyHistorySnapshotMissing(t *testing.T) {
	cfg := testConfig(t)
	h := newTestHandler(cfg)
	err := h.copyHistorySnapshot("no-such-p", "u", "b")
	if err == nil || !isENOENT(err) {
		t.Fatalf("expected ENOENT, got %v", err)
	}
}

// --- download paths (via downloadHistorySnapshot) ---

func TestDownloadHistoryDisabled(t *testing.T) {
	cfg := testConfig(t)
	h := newTestHandler(cfg)
	ok, err := h.downloadHistorySnapshot(validObjectID, "u1", t.TempDir())
	if ok || err != nil {
		t.Fatalf("expected disabled short-circuit, got ok=%v err=%v", ok, err)
	}
}

func TestDownloadHistory404(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	h.fetchStream = func(u string) (io.ReadCloser, error) {
		return nil, &RequestFailedError{Response: &http.Response{StatusCode: http.StatusNotFound}}
	}
	out := t.TempDir()
	ok, err := h.downloadHistorySnapshot(validObjectID, "u1", out)
	if ok || err != nil {
		t.Fatalf("expected (false, nil) on 404, got ok=%v err=%v", ok, err)
	}
	// On 404 no file is written and the breaker is closed (a miss is healthy).
	if fileExists(filepath.Join(out, "history-resync.json.gz")) {
		t.Fatal("file should not be written on 404")
	}
	if h.isCircuitBreakerTripped("http://shard1") {
		t.Fatal("breaker should be closed on 404")
	}
}

func TestDownloadHistoryFetchErrorTripsBreaker(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	h.perfMs = func() float64 { return 1000 }
	h.fetchStream = func(u string) (io.ReadCloser, error) {
		return nil, &RequestFailedError{Response: &http.Response{StatusCode: 500}}
	}
	ok, err := h.downloadHistorySnapshot(validObjectID, "u1", t.TempDir())
	if ok || err == nil {
		t.Fatalf("expected error on fetch failure")
	}
	if !h.isCircuitBreakerTripped("http://shard1") {
		t.Fatal("breaker should be tripped after fetch error")
	}
	var oe *oerrors.OError
	if !errors.As(err, &oe) {
		t.Fatalf("expected OError, got %T", err)
	}
}

func TestDownloadHistorySuccess(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	payload := []byte("SYNCTEXPAYLOAD")
	var lastURL string
	h.fetchStream = func(u string) (io.ReadCloser, error) {
		lastURL = u
		return io.NopCloser(bytes.NewReader(payload)), nil
	}
	out := t.TempDir()
	ok, err := h.downloadHistorySnapshot(validObjectID, "u1", out)
	if !ok || err != nil {
		t.Fatalf("expected success, got ok=%v err=%v", ok, err)
	}
	want := "http://shard1/project/" + validObjectID + "/user/u1/latest/output/history-resync.json.gz"
	if lastURL != want {
		t.Fatalf("url = %q want %q", lastURL, want)
	}
	data, err := os.ReadFile(filepath.Join(out, "history-resync.json.gz"))
	if err != nil {
		t.Fatal("output file missing")
	}
	if !bytes.Equal(data, payload) {
		t.Fatal("payload mismatch")
	}
}

func TestDownloadHistoryStreamWriteError(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	h.fetchStream = func(u string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payloadBytes)), nil
	}
	// Force the destination to be a directory so the rename/open fails.
	out := t.TempDir()
	if err := os.MkdirAll(filepath.Join(out, "history-resync.json.gz"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.perfMs = func() float64 { return 5 }
	_, err := h.downloadHistorySnapshot(validObjectID, "u1", out)
	if err == nil {
		t.Fatal("expected error writing into a directory")
	}
	if !h.isCircuitBreakerTripped("http://shard1") {
		t.Fatal("breaker should be tripped on stream write error")
	}
}

func TestDownloadLatestCompileCacheSuccess(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	gz, err := buildGzTarFiles(map[string]string{
		"top.tex":  "TOP",
		"sub/inner.tex": "NESTED",
	})
	if err != nil {
		t.Fatal(err)
	}
	var lastURL string
	h.fetchStream = func(u string) (io.ReadCloser, error) {
		lastURL = u
		return io.NopCloser(bytes.NewReader(gz)), nil
	}
	out := t.TempDir()
	ok, err := h.downloadLatestCompileCache(validObjectID, "u1", out)
	if !ok || err != nil {
		t.Fatalf("expected success, got ok=%v err=%v", ok, err)
	}
	want := "http://shard1/project/" + validObjectID + "/user/u1/latest/output/output.tar.gz"
	if lastURL != want {
		t.Fatalf("url = %q want %q", lastURL, want)
	}
	top, err := os.ReadFile(filepath.Join(out, "top.tex"))
	if err != nil || string(top) != "TOP" {
		t.Fatalf("top.tex mismatch: %v %q", err, top)
	}
	inner, err := os.ReadFile(filepath.Join(out, "sub", "inner.tex"))
	if err != nil || string(inner) != "NESTED" {
		t.Fatalf("inner.tex mismatch: %v %q", err, inner)
	}
}

func TestDownloadLatestCompileCacheMissingSrc(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	// Empty tarball: no entries, no error, no abort.
	gz, _ := buildGzTarFiles(nil)
	h.fetchStream = func(u string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(gz)), nil
	}
	ok, err := h.downloadLatestCompileCache(validObjectID, "u1", t.TempDir())
	if !ok || err != nil {
		t.Fatalf("expected success, got ok=%v err=%v", ok, err)
	}
}

func TestDownloadLatestGarbageStreamTripsBreaker(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	h.perfMs = func() float64 { return 5 }
	h.fetchStream = func(u string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte("not a gzip"))), nil
	}
	ok, err := h.downloadLatestCompileCache(validObjectID, "u1", t.TempDir())
	if ok || err == nil {
		t.Fatalf("expected error on malformed gzip")
	}
	if !h.isCircuitBreakerTripped("http://shard1") {
		t.Fatal("breaker should be tripped after malformed stream")
	}
	var oe *oerrors.OError
	if !errors.As(err, &oe) {
		t.Fatalf("expected OError, got %T", err)
	}
}

func TestDownloadLatestTooManyEntriesAborts(t *testing.T) {
	cfg := testConfig(t,
		config.ClsiCacheShard{Shard: "shard-1", URL: "http://shard1"},
	)
	h := newTestHandler(cfg)
	gz, err := buildGzTarMany(105)
	if err != nil {
		t.Fatal(err)
	}
	h.fetchStream = func(u string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(gz)), nil
	}
	out := t.TempDir()
	ok, err := h.downloadLatestCompileCache(validObjectID, "u1", out)
	if ok || err != nil {
		t.Fatalf("expected abort with no error, got ok=%v err=%v", ok, err)
	}
}

// --- extract / writeTarEntry / tarEntryType ---

func TestTarEntryType(t *testing.T) {
	tests := []struct {
		flag   byte
		expect string
	}{
		{'0', "file"},
		{'1', "link"},
		{'2', "symlink"},
		{'3', "character-device"},
		{'4', "block-device"},
		{'5', "directory"},
		{'6', "fifo"},
		{'7', "contiguous-file"},
		{'P', "pax-header"},
		{'g', "pax-global-header"},
		{'\x01', "unknown"},
		{tar.TypeDir, "directory"},
		{tar.TypeRegA, "file"},
		{tar.TypeLink, "link"},
		{tar.TypeSymlink, "symlink"},
	}
	for _, tt := range tests {
		if got := tarEntryType(&tar.Header{Typeflag: tt.flag}); got != tt.expect {
			t.Errorf("typeflag %q: got %q want %q", tt.flag, got, tt.expect)
		}
	}
}

func TestExtractAbortsAfterTooMany(t *testing.T) {
	gz, err := buildGzTarMany(105)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	n, abort, err := extractCompileCache(bytes.NewReader(gz), out, validObjectID, "u1")
	if !abort || err != nil {
		t.Fatalf("expected abort without error, got abort=%v n=%d err=%v", abort, n, err)
	}
	// Entries f0..f99 are written; f100 is the (100+1)th and aborts, skipped.
	if !fileExists(filepath.Join(out, "f99.txt")) {
		t.Fatal("f99.txt should be written before the abort")
	}
	if fileExists(filepath.Join(out, "f100.txt")) {
		t.Fatal("entries written after the abort should be skipped")
	}
	if n != 101 {
		t.Fatalf("expected n=101 written, got %d", n)
	}
}

// --- writeTarGzip + packEntries ---

func TestWriteTarGzipRoundTrip(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sub", "b.txt"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "out.tar.gz")
	if err := writeTarGzip(dst, srcDir, []string{"a.txt", "sub"}); err != nil {
		t.Fatalf("writeTarGzip: %v", err)
	}
	gz, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	_, names, err := readGzippedTar(gz)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 || names[0] != "a.txt" || names[1] != "sub/" || names[2] != "sub/b.txt" {
		t.Fatalf("entries = %q", names)
	}
}

func TestWriteTarGzipMissingEntry(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out.tar.gz")
	err := writeTarGzip(dst, t.TempDir(), []string{"missing.txt"})
	if err == nil || !isENOENT(err) {
		t.Fatalf("expected ENOENT, got %v", err)
	}
	if fileExists(dst) {
		t.Fatal("partial file not removed on error")
	}
}

// --- circuit breaker ---

func TestTripAndCloseCircuitBreaker(t *testing.T) {
	cfg := testConfig(t)
	h := newTestHandler(cfg)
	perf := 3.0
	h.perfMs = func() float64 { return perf }
	h.tripCircuitBreaker("http://x")
	if !h.isCircuitBreakerTripped("http://x") {
		t.Fatal("expected tripped after trip")
	}
	h.closeCircuitBreaker("http://x")
	if h.isCircuitBreakerTripped("http://x") {
		t.Fatal("expected closed after close")
	}
	// Unknown url never tripped / zero failure count.
	if h.isCircuitBreakerTripped("http://never-seen") {
		t.Fatal("unknown shard should not be tripped")
	}
}

// --- default fetch helpers against a real HTTP server ---

func TestDefaultFetchNothingAndStream(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/post-ok", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	mux.HandleFunc("/get-404", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte("NF"))
	})
	mux.HandleFunc("/get-500", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("boom"))
	})
	mux.HandleFunc("/get-200", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("PAYLOAD"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if err := defaultFetchNothing(srv.URL+"/post-ok", []byte("body")); err != nil {
		t.Fatalf("ok fetch should succeed: %v", err)
	}
	if err := defaultFetchNothing(srv.URL+"/get-500", []byte("body")); err == nil {
		t.Fatal("500 fetch should fail")
	}
	err := defaultFetchNothing(srv.URL+"/get-404", nil)
	var rfe *RequestFailedError
	if !errors.As(err, &rfe) || rfe.Response.StatusCode != 404 {
		t.Fatalf("expected RequestFailedError 404, got %v", err)
	}
	reader, serr := defaultFetchStream(srv.URL+"/get-200")
	if serr != nil {
		t.Fatalf("stream: %v", serr)
	}
	if b, _ := io.ReadAll(reader); !bytes.Equal(b, []byte("PAYLOAD")) {
		t.Fatalf("stream body mismatch: %q", b)
	}
	var sreader io.ReadCloser
	var serr2 error
	sreader, serr2 = defaultFetchStream(srv.URL+"/get-500")
	if sreader != nil {
		t.Fatal("expected nil reader on 500")
	}
	var rfe2 *RequestFailedError
	if !errors.As(serr2, &rfe2) || rfe2.Response.StatusCode != 500 {
		t.Fatalf("expected 500 RequestFailedError, got %v", serr2)
	}
}

func TestDefaultFetchNothingTransportError(t *testing.T) {
	if err := defaultFetchNothing("http://127.0.0.1:1/nowhere", nil); err == nil {
		t.Fatal("expected transport error")
	}
}

// --- RequestFailedError / statusOfRequestFailed ---

func TestRequestFailedErrorSurface(t *testing.T) {
	inner := errors.New("inner")
	e := &RequestFailedError{Response: &http.Response{StatusCode: 500}, Err: inner}
	if !strings.Contains(e.Error(), "500") {
		t.Fatalf("missing status in error: %q", e.Error())
	}
	if e.Unwrap() != inner {
		t.Fatal("Unwrap wrong")
	}
	e2 := &RequestFailedError{Err: inner}
	if !strings.Contains(e2.Error(), "inner") {
		t.Fatalf("missing cause in error: %q", e2.Error())
	}
	e3 := &RequestFailedError{}
	if e3.Error() != "request failed" {
		t.Fatalf("default message wrong: %q", e3.Error())
	}
}

func TestStatusOfRequestFailed(t *testing.T) {
	if statusOfRequestFailed(&RequestFailedError{Response: &http.Response{StatusCode: 404}}) != 404 {
		t.Fatal("status extraction wrong")
	}
	if statusOfRequestFailed(errors.New("x")) != 0 {
		t.Fatal("non-RequestFailedError should be 0")
	}
	if statusOfRequestFailed(&RequestFailedError{Err: errors.New("x")}) != 0 {
		t.Fatal("no-response RequestFailedError should be 0")
	}
}

// --- isENOENT / hasDotBLG / uuidV4 ---

func TestHasDotBLG(t *testing.T) {
	if !hasDotBLG("a.blg") {
		t.Error("a.blg should match")
	}
	if hasDotBLG("a.bxg") {
		t.Error("a.bxg should not match")
	}
	if hasDotBLG("blg") {
		t.Error("bare 'blg' should not match (too short)")
	}
}

func TestUUIDV4Format(t *testing.T) {
	u := uuidV4()
	if len(u) != 36 || u[8] != '-' || u[13] != '-' {
		t.Fatalf("bad uuid format: %q", u)
	}
	if u[14] != '4' {
		t.Fatalf("not version 4: %q", u)
	}
}

// --- helpers ---

var payloadBytes = []byte("SYNCTEXPAYLOAD")

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// readGzippedTar reads a .gz tar and returns (entries, names, err) where
// entries is the tar type per entry and names the names.
func readGzippedTar(gz []byte) ([]string, []string, error) {
	gr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, nil, err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	var types, names []string
	for {
		hdr, rerr := tr.Next()
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, nil, rerr
		}
		types = append(types, tarEntryType(hdr))
		names = append(names, hdr.Name)
	}
	return types, names, nil
}

func buildGzTarFiles(files map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Typeflag: tar.TypeReg,
			Size:     int64(len(content)),
		}); err != nil {
			return nil, err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildGzTarMany(n int) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for i := 0; i < n; i++ {
		data := []byte(fmt.Sprintf("file %d", i))
		if err := tw.WriteHeader(&tar.Header{
			Name:     fmt.Sprintf("f%d.txt", i),
			Typeflag: tar.TypeReg,
			Size:     int64(len(data)),
		}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
