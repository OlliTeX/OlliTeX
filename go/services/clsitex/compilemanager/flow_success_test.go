package compilemanager

import (
	"errors"
	"testing"

	off "ollitex/go/services/clsitex/outputfilefinder"
)

// --- doCompile success path (RW + HRW, initial-compile + cache restore) --------

func TestFlowCompileSuccessPlain(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	// deterministic Now for timings
	m.Now = func() int64 { return nowVal }
	req := makeHRWReq()
	res, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.BaseHistoryVersion == nil || *res.BaseHistoryVersion != 9 {
		t.Fatalf("baseHistoryVersion: got %v", res.BaseHistoryVersion)
	}
	if res.BuildID != "b1" {
		t.Fatalf("buildID: %q", res.BuildID)
	}
	if len(res.OutputFiles) == 0 {
		t.Fatalf("outputFiles empty")
	}
	// perf-sample path: SampleRequest nil -> skip (cover recordPerfSample)
}

func TestFlowCompileSuccessNoHRW(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	req := makeReq()
	if _, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestFlowCompileInitialRestoredCache(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.MkdirAll = func(dir string) (bool, error) { return true, nil }
	m.DownloadLatestCompileCache = func(pid, uid, dir string) (bool, error) { return true, nil }
	req := makeReq()
	req.CompileFromClsiCache = true
	if _, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestFlowCompileCheckpointImage(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.AllowedImages = []string{"texlive/texlive:2024.checkpointing"}
	req := makeReq()
	req.ImageName = "texlive/texlive:2024"
	req.EnableCheckpoint = true
	if _, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestFlowCompileCheckpointImageUnavailable(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.AllowedImages = []string{"texlive/texlive:2024"}
	req := makeReq()
	req.ImageName = "texlive/texlive:2024"
	req.EnableCheckpoint = true
	// not allowed -> warn + compile with base image
	if _, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

// --- saveOutputFiles error path ------------------------------------------------

func TestFlowCompileSaveError(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.SaveOutputFiles = func(req SaveOutputReq, rawFiles []off.OutputFile, compileDir, outputDir string,
		stats map[string]float64, timings map[string]float64) (string, []off.OutputFile, error) {
		return "", nil, errors.New("save boom")
	}
	req := makeReq()
	if _, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{}); err == nil {
		t.Fatal("expected save error")
	}
}
