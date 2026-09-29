package compilemanager

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ollitex/go/services/clsitex/lockmanager"
)

// --- lifecycle: StopCompile -----------------------------------------------------

func TestStopCompileIdle(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	if err := m.StopCompile("p1", "u1"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestStopCompileNoLockRunning(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.IsRunning = func(name string) bool { return true }
	m.KillLatex = func(name string, cb func(err error)) { cb(nil) }
	if err := m.StopCompile("p1", "u1"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestStopCompileKillError(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.IsRunning = func(name string) bool { return true }
	m.KillLatex = func(name string, cb func(err error)) { cb(errors.New("kill boom")) }
	if err := m.StopCompile("p1", "u1"); err == nil {
		t.Fatal("expected kill error")
	}
}

func TestStopCompileLocked(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	lk := &lockmanager.Lock{Key: "t1"}
	m.GetLock = func(key string) *lockmanager.Lock { return lk }
	m.KillLatex = func(name string, cb func(err error)) { cb(nil) }
	// Release after the flow blocks on the wait channel (Node:
	// lock.waitForRelease after kill, best-effort).
	go func() {
		time.Sleep(20 * time.Millisecond)
		lk.Release()
	}()
	if err := m.StopCompile("p1", "u1"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// --- lifecycle: CheckDirectory / ClearProject* / ClearExpiredProjects ----------

func TestCheckDirectory(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	// nonexistent -> (false, nil)
	ok, err := m.CheckDirectory("/definitely/not/here")
	if ok || err != nil {
		t.Fatalf("absent dir: ok=%v err=%v", ok, err)
	}
	// file (not dir) -> (false, error)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "f.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err = m.CheckDirectory(f)
	if ok || err == nil {
		t.Fatalf("file: ok=%v err=%v", ok, err)
	}
	// dir -> (true, nil)
	ok, err = m.CheckDirectory(tmp)
	if !ok || err != nil {
		t.Fatalf("dir: ok=%v err=%v", ok, err)
	}
}
