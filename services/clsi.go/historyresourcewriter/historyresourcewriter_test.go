// historyresourcewriter_test.go ports services/clsi/test/unit/js/HistoryResourceWriter.test.js
// (327 L) 1:1 against faked seams (no Docker, no network).
package historyresourcewriter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clsi/config"
	"clsi/urlcache"
)

// fakeSeams mirrors the Node vi.doModel fakes: optCache holds the URLs with a
// .opt entry. A conversion download misses the first time (returns a handle
// and records in optCache) and hits thereafter (returns nil handle). Every
// download writes the destination file so the compile dir reflects what a
// real download leaves behind.
type fakeSeams struct {
	optCache  map[string]struct{}
	downloads []map[string]any
	converted [][]string
	committed int
}

func installFakeSeams(t *testing.T, m *fakeSeams) {
	t.Helper()
	m.optCache = map[string]struct{}{}
	DownloadUrlToFile = func(projectID, urlStr, fallbackURL, destPath string,
		lastModified *time.Time, conversionSuffix string,
	) (*urlcache.ConversionHandle, error) {
		if err := os.WriteFile(destPath, []byte("png-bytes"), 0o644); err != nil {
			return nil, err
		}
		m.downloads = append(m.downloads, map[string]any{
			"projectID": projectID,
			"url":       urlStr,
			"destPath":  destPath,
			"suffix":    conversionSuffix,
		})
		if conversionSuffix != "" {
			if _, ok := m.optCache[urlStr]; ok {
				return nil, nil
			}
			m.optCache[urlStr] = struct{}{}
			return &urlcache.ConversionHandle{
				ConversionPath: destPath + ".conv",
				CachePath:      destPath + ".opt",
				DestPath:       destPath,
			}, nil
		}
		return nil, nil
	}
	IsConversionCached = func(projectID, urlStr string, lastModified *time.Time) (bool, error) {
		_, ok := m.optCache[urlStr]
		return ok, nil
	}
	CommitConversion = func(conversionPath, cachePath, destPath string) error {
		m.committed++
		return nil
	}
	CreateProjectDir = func(projectID string) error { return nil }
	GetProjectCacheDir = func(projectID string) string {
		return filepath.Join(config.Get().Path.ClsiCacheDir, projectID)
	}
	Png2PdfEnabled = func() bool { return true }
	PngConvert = func(projectID, cacheProjectDir string, relativePaths []string,
		stats, timings map[string]any,
	) error {
		m.converted = append(m.converted, append([]string{}, relativePaths...))
		return nil
	}
	DownloadHistorySnapshot = func(projectID, userID, dir string) (bool, error) {
		return false, nil
	}
	// Node: isExtraneousFile: () => false — keep every cached file.
	IsExtraneousFile = func(p string) bool { return false }
	// Node: writeOutputFileIfNeeded: stub resolving().
	WriteOutputFileIfNeeded = func(compileDir string, hasOutputTex bool, content string) error {
		return nil
	}
	t.Cleanup(func() { ResetSeams() })
}

// setup pins CLSI_CACHE_PATH to a fresh temp dir and re-wires the config
// singleton (pattern from urlcache_test.go). Mirrors the Node beforeEach:
// ctx.tmp, projectId=u1, cacheKey=p1-u1, compileDir=tmp/compiles/<cacheKey>.
func setup(t *testing.T) (projectID, userID, cacheKey, compileDir string) {
	t.Helper()
	cacheDir := t.TempDir()
	// Mirror urlcache_test's withCacheDir: the Go binary bundles DockerRunner,
	// so sandboxed compiles are mandatory and config.New() requires the
	// sandbox compiles host dir. Pin all the host-dir envs to temp dirs and
	// CLSI_CACHE_PATH, then re-build the config singleton.
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", os.TempDir()+"/hrw-compiles")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", os.TempDir()+"/hrw-cache-host")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_OUTPUT", os.TempDir()+"/hrw-output")
	t.Setenv("CLSI_CACHE_PATH", cacheDir)
	config.ForTest()
	ResetSeams()
	projectID = "p1"
	userID = "u1"
	cacheKey = projectID + "-" + userID
	compileDir = filepath.Join(t.TempDir(), "compiles", cacheKey)
	if err := os.MkdirAll(compileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return
}

const pngHash = "0123456789012345678901234567890123456789"
const pngHash2 = "1123456789012345678901234567890123456789"

// makeRequest mirrors ctx.makeRequest.
func makeRequest(overrides map[string]any) *Request {
	req := &Request{
		BaseHistoryVersion: 0,
		RawSnapshot: map[string]any{
			"files": map[string]any{
				"fig.png":  map[string]any{"hash": pngHash, "byteLength": float64(2 * 1024 * 1024)},
				"main.tex": map[string]any{"content": "hello"},
			},
		},
		GlobalBlobs:         []string{},
		RawChangeOperations: [][]map[string]any{},
		Png2pdf:             true,
		HistoryID:           "hist1",
		FilestoreBlobPrefix: "",
		ClSIPerfVariant:     "",
		Draft:               false,
		RootResourcePath:    "main.tex",
		CompileGroup:        "standard",
	}
	for k, v := range overrides {
		switch k {
		case "png2pdf":
			req.Png2pdf = v.(bool)
		case "files":
			files := req.RawSnapshot["files"].(map[string]any)
			for fk, fv := range v.(map[string]any) {
				files[fk] = fv
			}
		case "rawSnapshot":
			req.RawSnapshot = v.(map[string]any)
		case "noRawSnapshot":
			req.RawSnapshot = nil
		case "baseHistoryVersion":
			req.BaseHistoryVersion = v.(int)
		case "changes":
			req.RawChangeOperations = v.([][]map[string]any)
		case "draft":
			req.Draft = v.(bool)
		case "populate":
			req.PopulateClsiCache = v.(bool)
		case "historyID":
			req.HistoryID = v.(string)
		case "blobPrefix":
			req.FilestoreBlobPrefix = v.(string)
		case "variant":
			req.ClSIPerfVariant = v.(string)
		case "metricsPath":
			req.MetricsPath = v.(string)
		}
	}
	return req
}

func syncOnce(t *testing.T, m *fakeSeams, projectID, userID, compileDir string,
	overrides map[string]any,
) (*Result, map[string]any, error) {
	t.Helper()
	req := makeRequest(overrides)
	stats := map[string]any{}
	timings := map[string]any{}
	res, err := SyncResourcesToDisk(context.Background(), projectID, userID, req,
		compileDir, timings, stats)
	return res, stats, err
}

func TestSaveSlowPngList(t *testing.T) {
	_, _, cacheKey, compileDir := setup(t)
	if err := SaveSlowPngList(cacheKey, []string{"a.png", "b.png"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(config.Get().Path.ClsiCacheDir, cacheKey, "png2pdf-slow.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `["a.png","b.png"]` {
		t.Fatalf("slow-png list = %q, want [\"a.png\",\"b.png\"]", data)
	}
	_ = compileDir
}

func TestSlowListGatedConversion(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)

	// Sync 1: no slow-list yet -> the PNG is downloaded normally, not converted.
	if _, _, err := syncResult(t, m, projectID, userID, compileDir, nil); err != nil {
		t.Fatalf("sync1: %v", err)
	}
	if len(m.downloads) != 1 {
		t.Fatalf("sync1 downloads = %d, want 1", len(m.downloads))
	}
	wantDest := filepath.Join(compileDir, "fig.png")
	if m.downloads[0]["destPath"] != wantDest {
		t.Fatalf("sync1 dest = %v, want %q", m.downloads[0]["destPath"], wantDest)
	}
	if m.downloads[0]["suffix"] != "" {
		t.Fatalf("sync1 suffix = %v, want none", m.downloads[0]["suffix"])
	}
	if len(m.converted) != 0 {
		t.Fatalf("sync1 converted: %v", m.converted)
	}

	// A compile then flags fig.png as slow.
	if err := SaveSlowPngList(cacheKey, []string{"fig.png"}); err != nil {
		t.Fatal(err)
	}

	// Sync 2: the newly-slow PNG is pulled in and routed through conversion.
	m.downloads = nil
	m.converted = nil
	m.committed = 0
	if _, _, err := syncResult(t, m, projectID, userID, compileDir, nil); err != nil {
		t.Fatalf("sync2: %v", err)
	}
	if len(m.downloads) != 1 {
		t.Fatalf("sync2 downloads = %d, want 1", len(m.downloads))
	}
	if m.downloads[0]["suffix"] != cacheKey {
		t.Fatalf("sync2 suffix = %v, want %q", m.downloads[0]["suffix"], cacheKey)
	}
	if len(m.converted) != 1 {
		t.Fatalf("sync2 conversions = %d, want 1", len(m.converted))
	}
	if m.committed != 1 {
		t.Fatalf("sync2 commits = %d, want 1", m.committed)
	}

	// Sync 3: already attempted -> not converted again (attempt-once).
	m.downloads = nil
	m.converted = nil
	m.committed = 0
	if _, _, err := syncResult(t, m, projectID, userID, compileDir, nil); err != nil {
		t.Fatalf("sync3: %v", err)
	}
	if len(m.downloads) != 0 {
		t.Fatalf("sync3 downloads = %d, want 0 (attempt-once)", len(m.downloads))
	}
	if len(m.converted) != 0 {
		t.Fatalf("sync3 converted: %v", m.converted)
	}
}

func syncResult(t *testing.T, m *fakeSeams, projectID, userID, compileDir string,
	overrides map[string]any,
) (*Result, map[string]any, error) {
	t.Helper()
	req := makeRequest(overrides)
	stats := map[string]any{}
	timings := map[string]any{}
	res, err := SyncResourcesToDisk(context.Background(), projectID, userID, req,
		compileDir, timings, stats)
	return res, stats, err
}

func TestReServeOptimisedAfterModeSwitch(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)

	// Get fig.png converted so it has an .opt cache entry.
	if err := SaveSlowPngList(cacheKey, []string{"fig.png"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := syncResult(t, m, projectID, userID, compileDir, nil); err != nil {
		t.Fatal(err)
	}
	if len(m.converted) != 1 {
		t.Fatalf("initial conversions = %d, want 1", len(m.converted))
	}

	// The next compile no longer flags it slow (it is now included as a PDF),
	// so it drops off the slow-list.
	if err := SaveSlowPngList(cacheKey, []string{}); err != nil {
		t.Fatal(err)
	}

	// Toggle png2pdf off: the PNG reverts to the original on disk (downloaded
	// without the conversion cacheKey suffix).
	m.downloads = nil
	m.converted = nil
	if _, _, err := syncResult(t, m, projectID, userID, compileDir, map[string]any{"png2pdf": false}); err != nil {
		t.Fatal(err)
	}
	if len(m.downloads) != 1 {
		t.Fatalf("mode-off downloads = %d, want 1", len(m.downloads))
	}
	if m.downloads[0]["suffix"] != "" {
		t.Fatalf("mode-off suffix = %v, want none", m.downloads[0]["suffix"])
	}

	// Toggle png2pdf back on: even though fig.png is no longer on the
	// slow-list, its cached .opt entry is re-served (routed through the
	// conversion path with the cacheKey), so it is restored to the optimised
	// variant rather than reverting to the original - and without a new
	// conversion.
	m.downloads = nil
	m.converted = nil
	m.committed = 0
	if _, _, err := syncResult(t, m, projectID, userID, compileDir, map[string]any{"png2pdf": true}); err != nil {
		t.Fatal(err)
	}
	if len(m.downloads) != 1 {
		t.Fatalf("mode-on downloads = %d, want 1", len(m.downloads))
	}
	if m.downloads[0]["suffix"] != cacheKey {
		t.Fatalf("mode-on suffix = %v, want %q", m.downloads[0]["suffix"], cacheKey)
	}
	if len(m.converted) != 0 {
		t.Fatalf("mode-on conversions = %d, want 0 (opt cache hit)", len(m.converted))
	}
}

func TestAnalyticsNoSlowList(t *testing.T) {
	projectID, userID, _, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)

	// No slow-list saved: fig.png is large enough to convert but was never
	// flagged slow, so it is not a conversion candidate.
	_, stats, err := syncOnce(t, m, projectID, userID, compileDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v := stats["projectHasUnconvertedPngs"]; v != nil {
		t.Fatalf("projectHasUnconvertedPngs = %v, want unset", v)
	}
	if v := stats["optimisable-png-count"]; v != nil {
		t.Fatalf("optimisable-png-count = %v, want unset", v)
	}
	if len(m.converted) != 0 {
		t.Fatalf("converted: %v", m.converted)
	}
}

func TestAnalyticsSlowPngPng2pdfOff(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)

	if err := SaveSlowPngList(cacheKey, []string{"fig.png"}); err != nil {
		t.Fatal(err)
	}
	_, stats, err := syncOnce(t, m, projectID, userID, compileDir, map[string]any{"png2pdf": false})
	if err != nil {
		t.Fatal(err)
	}
	if stats["projectHasUnconvertedPngs"] != 1 {
		t.Fatalf("projectHasUnconvertedPngs = %v, want 1", stats["projectHasUnconvertedPngs"])
	}
	if stats["optimisable-png-count"] != 1 {
		t.Fatalf("optimisable-png-count = %v, want 1", stats["optimisable-png-count"])
	}
	if len(m.converted) != 0 {
		t.Fatalf("conversion must not run: %v", m.converted)
	}
}

func TestAnalyticsSlowPngPng2pdfOn(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)

	if err := SaveSlowPngList(cacheKey, []string{"fig.png"}); err != nil {
		t.Fatal(err)
	}
	_, stats, err := syncOnce(t, m, projectID, userID, compileDir, map[string]any{"png2pdf": true})
	if err != nil {
		t.Fatal(err)
	}
	if stats["projectHasUnconvertedPngs"] != 1 {
		t.Fatalf("projectHasUnconvertedPngs = %v, want 1", stats["projectHasUnconvertedPngs"])
	}
	if stats["optimisable-png-count"] != 1 {
		t.Fatalf("optimisable-png-count = %v, want 1", stats["optimisable-png-count"])
	}
	if len(m.converted) != 1 {
		t.Fatalf("conversions = %d, want 1", len(m.converted))
	}
}

func TestAnalyticsBelowThreshold(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)

	// A slow-listed PNG that is too small to be worth converting is not a
	// conversion candidate, so it must not be counted as convertible either -
	// the analytics flag uses the same size-filtered set as conversion, so the
	// optimised and default groups stay comparable.
	files := map[string]any{
		// "fig.png": map replaced below (below threshold)
	}
	if err := SaveSlowPngList(cacheKey, []string{"fig.png"}); err != nil {
		t.Fatal(err)
	}
	files["fig.png"] = map[string]any{"hash": pngHash, "byteLength": float64(512 * 1024)}
	_, stats, err := syncResult(t, m, projectID, userID, compileDir, map[string]any{"png2pdf": true, "files": files})
	if err != nil {
		t.Fatal(err)
	}
	if v := stats["projectHasUnconvertedPngs"]; v != nil {
		t.Fatalf("projectHasUnconvertedPngs = %v, want unset", v)
	}
	if v := stats["optimisable-png-count"]; v != nil {
		t.Fatalf("optimisable-png-count = %v, want unset", v)
	}
	if len(m.converted) != 0 {
		t.Fatalf("converted: %v", m.converted)
	}
}

func TestAnalyticsMultipleSlowPngs(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)

	// With a single PNG, the count can never be more than 1 and so is
	// indistinguishable from the boolean flag - add a second qualifying PNG
	// to prove this field is an actual count.
	files := map[string]any{
		"fig2.png": map[string]any{"hash": pngHash2, "byteLength": float64(2 * 1024 * 1024)},
	}
	if err := SaveSlowPngList(cacheKey, []string{"fig.png", "fig2.png"}); err != nil {
		t.Fatal(err)
	}
	_, stats, err := syncOnce(t, m, projectID, userID, compileDir, map[string]any{"png2pdf": true, "files": files})
	if err != nil {
		t.Fatal(err)
	}
	if stats["projectHasUnconvertedPngs"] != 1 {
		t.Fatalf("projectHasUnconvertedPngs = %v, want 1", stats["projectHasUnconvertedPngs"])
	}
	if stats["optimisable-png-count"] != 2 {
		t.Fatalf("optimisable-png-count = %v, want 2", stats["optimisable-png-count"])
	}
}

func TestAnalyticsMixedThreshold(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)

	// One slow PNG qualifies by size and one does not - the count must
	// reflect only the size-filtered set, not the raw slow-list size.
	if err := SaveSlowPngList(cacheKey, []string{"fig.png", "fig2.png"}); err != nil {
		t.Fatal(err)
	}
	files := map[string]any{
		"fig.png":  map[string]any{"hash": pngHash, "byteLength": float64(512 * 1024)},       // below threshold
		"fig2.png": map[string]any{"hash": pngHash2, "byteLength": float64(2 * 1024 * 1024)}, // above threshold
	}
	_, stats, err := syncResult(t, m, projectID, userID, compileDir, map[string]any{"png2pdf": true, "files": files})
	if err != nil {
		t.Fatal(err)
	}
	if stats["projectHasUnconvertedPngs"] != 1 {
		t.Fatalf("projectHasUnconvertedPngs = %v, want 1", stats["projectHasUnconvertedPngs"])
	}
	if stats["optimisable-png-count"] != 1 {
		t.Fatalf("optimisable-png-count = %v, want 1", stats["optimisable-png-count"])
	}
}
