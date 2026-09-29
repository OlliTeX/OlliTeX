package compilemanager

import (
	"errors"
	"path/filepath"
	"testing"

	ccmet "ollitex/go/services/clsitex/contentcachemetrics"
	cerrors "ollitex/go/services/clsitex/errors"
	"ollitex/go/services/clsitex/latexmetrics"
	off "ollitex/go/services/clsitex/outputfilefinder"
)

// --- clsiRequestAttrs / f64 / passesCategory / isImageNameAllowed / Error() ----

func TestUnitCovHelpersFull(t *testing.T) {
	req := makeReq()
	req.Timeout = 60
	req.Check = strPtr("error")
	req.Flags = nil
	req.CompileGroup = "users"
	req.SyncType = "full"
	attrs := clsiRequestAttrs(req)
	if attrs["compiler"] != "pdflatex" || attrs["check"] != "error" {
		t.Fatalf("attrs: %v", attrs)
	}
	if attrs["timeout"] != 60 || attrs["compileGroup"] != "users" {
		t.Fatalf("attrs fields: %v", attrs)
	}

	if got := compileName("p9", ""); got != "p9" {
		t.Fatalf("compileName empty-uid: %q", got)
	}
	if got := f64(int(5)); got != 5 {
		t.Fatalf("f64 int: %v", got)
	}
	if got := passesCategory(1); got != "single" {
		t.Fatalf("passes 1: %q", got)
	}
	if got := passesCategory(2); got != "multiple" {
		t.Fatalf("passes 2: %q", got)
	}
	m := newTestManager(t, t.TempDir())
	m.AllowedImages = nil
	if !m.isImageNameAllowed("anything") {
		t.Fatal("empty allowlist should allow any image")
	}
	got := anyToFloatMap(map[string]any{"i": int64(1), "f": float32(2)})
	if got["i"] != 1 || got["f"] != 2 {
		t.Fatalf("anyToFloatMap int64/float32: %v", got)
	}
	if got := (&CompileRunError{Cause: errors.New("inner")}).Error(); got != "inner" {
		t.Fatalf("Error() cause fallback: %q", got)
	}
}

func TestIsFilesOutOfSyncMatrix(t *testing.T) {
	// OError WITHOUT an FOS cause: both predicates must return false
	// without matching (the type-assert branches).
	bogus := cerrors.NewOError("outer")
	if isFilesOutOfSync(bogus) || oIsFilesOutOfSync(bogus) {
		t.Fatal("OError without FOS cause should not match")
	}
	// bare FOS through oIsFilesOutOfSync (the errors.As fallback).
	if !oIsFilesOutOfSync(cerrors.NewFilesOutOfSyncError("x")) {
		t.Fatal("oIs bare FOS")
	}
	if !isFilesOutOfSync(cerrors.NewFilesOutOfSyncError("x")) {
		t.Fatal("isFilesOutOfSync bare FOS")
	}
}

// --- emitMetrics: latexmk rule/img timing blocks + clsi-perf + skip -----------

func TestEmitMetricsLatexmkBlocks(t *testing.T) {
	req := makeReq()
	req.ImageName = "tl2024:base"
	req.CompileGroup = "users"
	latexmk := map[string]any{
		"latexmk-rule-times": []latexmetrics.RuleTime{
			{Rule: "pdflatex", TimeMS: 100},
			{Rule: "someother", TimeMS: 50}, // unknown rule -> "other" label
		},
		"latexmk-time":      map[string]any{"total": float64(160)},
		"latexmk-img-times": []latexmetrics.ImgTime{{Type: "png", Count: 2, TimeMS: 30}},
	}
	stats := map[string]any{"latexmk": latexmk}
	timings := map[string]any{"sync": 1.0, "compile": 2.0, "output": 3.0, "compileE2E": 4.0}
	newTestManager(t, t.TempDir()).emitMetrics(req, "success", stats, timings)

	// clsi-perf gauge + skip-early-return.
	perf := makeReq()
	perf.MetricsOpts = MetricsOpts{Path: "clsi-perf", Method: "GET"}
	mskip := newTestManager(t, t.TempDir())
	mskip.emitMetrics(perf, "success", map[string]any{}, map[string]any{"compileE2E": 4.0})
	// skip-early-return on a non-clsi-perf path.
	mskip2 := newTestManager(t, t.TempDir())
	mskip2.SkipMetrics = func(path string) bool { return true }
	mskip2.emitMetrics(makeReq(), "success", map[string]any{}, map[string]any{})
}

// --- pdfStatsSeam: Summary/Timing/Inc closures through ccmet -------------------

func TestPdfStatsSeamClosures(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	stats := map[string]any{
		"pdf-size":                      13,
		"pdf-caching-timed-out":         1,
		"pdf-caching-total-ranges-size": 4,
		"pdf-caching-n-ranges":          2,
		"pdf-caching-new-ranges-size":   1,
	}
	timings := map[string]any{
		"compute-pdf-caching": 5.0,
		"compileE2E":          10.0,
		"pdf-caching-overhead-delete-stale-hashes": 1.0,
	}
	ccmet.EmitPdfStats(stats, timings, m.pdfStatsSeam(), ccmet.MetricsOpts{})
}

// --- runCompile: perf tail (readFdbFile + clsiRequestAttrs via sample log) -----

func TestFlowPerfSampledTail(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fdbMode string // "", "file", "dir"
	}{
		{"noFdb", ""},
		{"fdbFile", "file"},
		{"fdbDirError", "dir"},
	} {
		tmp := t.TempDir()
		m := newTestManager(t, tmp)
		m.SampleRequest = func(uid, path string, pct int) *bool { s := true; return &s }
		m.LoadAvg = func() [3]float64 { return [3]float64{0.1, 0.2, 0.3} }
		cd := compileDirOf(m.Paths.CompilesDir, "p1", "u1")
		mustMkdir(t, cd)
		if tc.fdbMode == "file" {
			mustWriteFile(t, filepath.Join(cd, "output.fdb_latexmk"))
		} else if tc.fdbMode == "dir" {
			// a DIRECTORY at the fdb path makes ReadFile error -> warn.
			mustMkdir(t, filepath.Join(cd, "output.fdb_latexmk"))
		}
		req := makeReq()
		req.Check = strPtr("full")
		res, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if res.BuildID != "b1" {
			t.Fatalf("%s: buildId %q", tc.name, res.BuildID)
		}
	}
}

// --- runCompile: png2pdf slow-png warning + latexmk-errors status ---------------

func TestFlowPngSlowAndErrorStatus(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.Png2pdfOn = func() bool { return true }
	m.SaveSlowPngs = func(cacheKey string, pngs []string) error {
		return errors.New("save slow-pngs failed")
	}
	stats := map[string]any{"latexmk": map[string]any{"latexmk-png-slow": []string{"a.png"}}}
	req := makeHRWReq()
	if _, err := m.DoCompileWithLock(req, stats, map[string]any{}); err != nil {
		t.Fatalf("png2pdf warn path: %v", err)
	}

	// status="error" via stats.latexmk-errors=1.
	m2 := newTestManager(t, t.TempDir())
	if _, err := m2.DoCompileWithLock(makeReq(), map[string]any{"latexmk-errors": 1},
		map[string]any{}); err != nil {
		t.Fatalf("latexmk-errors status: %v", err)
	}
}

// --- doCompile: initial-compile cache download WARN branch ----------------------

func TestFlowInitialDownloadWarn(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.MkdirAll = func(dir string) (bool, error) { return true, nil }
	m.DownloadLatestCompileCache = func(pid, uid, dir string) (bool, error) {
		return false, errors.New("clsi-cache 500")
	}
	req := makeReq()
	req.CompileFromClsiCache = true
	if _, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{}); err != nil {
		t.Fatalf("download warn path: %v", err)
	}
}

// --- saveOutputFiles: BuildID passthrough + FindOutputFiles error ---------------

func TestSaveOutputFilesDirect(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	bid := "b-x"
	req := makeReq()
	req.BuildID = &bid
	var got string
	m.SaveOutputFiles = func(req SaveOutputReq, rawFiles []off.OutputFile, cd, od string,
		stats map[string]float64, timings map[string]float64) (string, []off.OutputFile, error) {
		got = req.BuildID
		return "b-out", rawFiles, nil
	}
	if _, _, _, err := m.saveOutputFiles(req, "/cd", []off.Resource{{Path: "main.tex"}},
		map[string]any{}, map[string]any{}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got != "b-x" {
		t.Fatalf("BuildID passthrough: %q", got)
	}
	m.FindOutputFiles = func(rs []off.Resource, dir string) (off.FindResult, error) {
		return off.FindResult{}, errors.New("find failed")
	}
	if _, _, _, err := m.saveOutputFiles(req, "/cd", nil, map[string]any{}, map[string]any{}); err == nil {
		t.Fatal("expected FindOutputFiles error")
	}
}
