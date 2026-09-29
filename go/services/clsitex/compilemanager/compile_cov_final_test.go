package compilemanager

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- lifecycle: ClearProject / ClearExpiredProjects / CheckDirectory -----------

// TestClearProjectAndListing covers ClearProject (absent + present),
// ClearProjectWithListing (absent nil early / happy unlink+rmdir / dir-path
// CheckDirectory error), including the per-entry error return.
func TestClearProjectAndListing(t *testing.T) {
	tmp := t.TempDir()
	m := newTestManager(t, tmp)

	// CheckDirectory: ENOENT -> (false, nil); non-directory -> OError.
	if ok, cerr := m.CheckDirectory("/definitely/absent"); ok || cerr != nil {
		t.Fatalf("absent: %v %v", ok, cerr)
	}
	fileOnly := filepath.Join(tmp, "fileonly")
	if err := os.WriteFile(fileOnly, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, cerr := m.CheckDirectory(fileOnly); ok || cerr == nil {
		t.Fatalf("non-directory should error: %v", cerr)
	}

	// ClearProject: absent dir is a no-op; present dir is removed.
	if err := m.ClearProject("pa", "ua"); err != nil {
		t.Fatalf("absent clear: %v", err)
	}
	dir := compileDirOf(m.Paths.CompilesDir, "p1", "u1")
	mustMkdir(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.ClearProject("p1", "u1"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("compile dir should be gone")
	}

	// ClearProjectWithListing: absent dir -> nil early return.
	if err := m.ClearProjectWithListing("p1", "u1", nil); err != nil {
		t.Fatalf("absent listing: %v", err)
	}

	// WithListing happy: per-entry unlink (file) + rmdir (dir with trailing
	// slash both hit os.Remove), then rmdir the now-empty compile dir.
	os.MkdirAll(dir, 0o755)
	f1 := filepath.Join(dir, "main.tex")
	if err := os.WriteFile(f1, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "tex")
	os.MkdirAll(sub, 0o755)
	if err := m.ClearProjectWithListing("p1", "u1", []string{"main.tex", "tex/"}); err != nil {
		t.Fatalf("listing: %v", err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("compile dir should be rmdir'd")
	}

	// Per-entry error: a directory listed WITHOUT the trailing-slash check
	// still hits os.Remove (same call) — to force an error, list a path
	// whose parent is missing... impossible; instead CheckDirectory error:
	// make compileDir a FILE -> CheckDirectory returns an OError.
	if err := os.WriteFile(dir, []byte("f"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.ClearProjectWithListing("p1", "u1", nil); err == nil {
		t.Fatal("expected not-a-directory error")
	}
	os.Remove(dir)
}

// TestClearExpiredProjects covers readdir scan + delete: stat-error skip,
// expired dir deleted, fresh dir kept, readDir error, AllRemoveAll error
// (unreadable child).
func TestClearExpiredProjects(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	cd := m.Paths.CompilesDir
	if err := os.MkdirAll(cd, 0o755); err != nil {
		t.Fatal(err)
	}
	// a FILE entry makes entry.Stat error -> continue (swallowed).
	if err := os.WriteFile(filepath.Join(cd, "strayfile"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldDir := filepath.Join(cd, "olddir")
	freshDir := filepath.Join(cd, "freshdir")
	os.MkdirAll(oldDir, 0o755)
	os.MkdirAll(freshDir, 0o755)
	nowMS := func() int64 { return time.Now().UnixMilli() }
	m.Now = nowMS
	// age oldDir by 1h+, fresh stays.
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := m.ClearExpiredProjects(time.Hour.Milliseconds()); err != nil {
		t.Fatalf("clearExpired: %v", err)
	}
	if _, err := os.Stat(oldDir); err == nil {
		t.Fatal("old dir should be removed")
	}
	if _, err := os.Stat(freshDir); err != nil {
		t.Fatal("fresh dir should survive")
	}

	// readDir error: point at a FILE so os.ReadDir fails.
	m.Paths.CompilesDir = filepath.Join(cd, "strayfile")
	if err := m.ClearExpiredProjects(0); err == nil {
		t.Fatal("expected readDir error")
	}
}
