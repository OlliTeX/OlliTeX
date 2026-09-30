package compilemanager

import (
	"path/filepath"
	"testing"
)

// New leaves no nil function seams (2026-09-30 live audit: MkdirAll and
// DownloadOutputDotSynctex were unwired, so the FIRST compile request
// panicked with a nil pointer dereference and surfaced as a bare 500 on
// /project/<id>/compile).
func TestNewProductionSeamsWired(t *testing.T) {
	m := New(nil, nil)

	// The seams that must exist for any compile to run at all.
	if m.MkdirAll == nil {
		t.Fatal("New: MkdirAll seam is nil — compile panics at DoCompileWithLock")
	}
	if m.DownloadOutputDotSynctex == nil {
		t.Fatal("New: DownloadOutputDotSynctex seam is nil — syncFromCode restore panics")
	}
	if m.ResourceSync == nil || m.Acquire == nil || m.GetLock == nil ||
		m.FindOutputFiles == nil || m.Now == nil || m.SkipMetrics == nil {
		t.Fatal("New: core compile seams must be wired")
	}

	// MkdirAll Node parity: fs.mkdir(dir, {recursive:true}) === dir, i.e.
	// created is true only when THIS call created the directory.
	dir := filepath.Join(t.TempDir(), "a", "b", "compile")
	created, err := m.MkdirAll(dir)
	if err != nil {
		t.Fatalf("MkdirAll fresh: %v", err)
	}
	if !created {
		t.Fatal("MkdirAll fresh dir: want created=true, got false")
	}
	created, err = m.MkdirAll(dir)
	if err != nil {
		t.Fatalf("MkdirAll existing: %v", err)
	}
	if created {
		t.Fatal("MkdirAll existing dir: want created=false, got true")
	}
}
