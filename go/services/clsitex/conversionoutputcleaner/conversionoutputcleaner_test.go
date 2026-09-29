package conversionoutputcleaner

import (
	"errors"
	"os"
	"path"
	"testing"
	"time"
)

func TestTTLMSConstant(t *testing.T) {
	if TTL_MS != 60000 {
		t.Errorf("TTL_MS = %d, want 60000", TTL_MS)
	}
}

func TestScheduleCleanupArmsRemovalAfterTTL(t *testing.T) {
	// Fake the timer the same way Node's test uses sinon fake timers:
	// capture the scheduled fire and only "tick" it manually.
	old := ScheduleAfter
	defer func() { ScheduleAfter = old }()
	var fire func()
	ScheduleAfter = func(delayMS int, f func()) { fire = f }

	dir := t.TempDir()
	sub := path.Join(dir, "test-conversion-id")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ScheduleCleanup(dir, "test-conversion-id")
	// Still there before the TTL fired:
	if _, err := os.Stat(sub); err != nil {
		t.Fatalf("dir missing before fire: %v", err)
	}
	fire()
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Errorf("dir still present after fire")
	}
}

func TestScheduleCleanupDefaultTTL(t *testing.T) {
	var delay int
	old := ScheduleAfter
	ScheduleAfter = func(d int, f func()) { delay = d }
	defer func() { ScheduleAfter = old }()
	ScheduleCleanup("/output", "id-only")
	if delay != TTL_MS {
		t.Errorf("default delay = %d, want %d", delay, TTL_MS)
	}
}

func TestScheduleCleanupCustomTTL(t *testing.T) {
	var delay int
	old := ScheduleAfter
	ScheduleAfter = func(d int, f func()) { delay = d }
	defer func() { ScheduleAfter = old }()
	ScheduleCleanup("/output", "id", 5500)
	if delay != 5500 {
		t.Errorf("custom delay = %d, want 5500", delay)
	}
}

func TestRemoveOutputDirRecursive(t *testing.T) {
	dir := t.TempDir()
	sub := path.Join(dir, "x/sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path.Join(sub, "f.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := RemoveOutputDir(path.Join(dir, "x")); err != nil {
		t.Fatalf("RemoveOutputDir = %v", err)
	}
	if _, err := os.Stat(path.Join(dir, "x")); !os.IsNotExist(err) {
		t.Errorf("dir not removed: %v", err)
	}
}

func TestRemoveOutputDirToleratesMissing(t *testing.T) {
	dir := t.TempDir()
	if err := RemoveOutputDir(path.Join(dir, "never-existed")); err != nil {
		t.Errorf("RemoveOutputDir(missing) = %v, want nil (force: true)", err)
	}
}

// TestRemoveOutputDirReturnsNonENOENTError exercises the non-ENOENT error
// return by overriding the injected removal primitive.
func TestRemoveOutputDirReturnsNonENOENTError(t *testing.T) {
	old := removeFunc
	defer func() { removeFunc = old }()
	forceErr := errors.New("permission denied (forced)")
	removeFunc = func(string) error { return forceErr }
	if err := RemoveOutputDir("/whatever"); err == nil || err != forceErr {
		t.Errorf("RemoveOutputDir = %v, want the forced error", err)
	}
}

// TestScheduleCleanupWarnsOnCleanupError covers ScheduleCleanup's error
// branch (Node: logger.warn('failed to clean up conversion output directory')
// and swallow).
func TestScheduleCleanupWarnsOnCleanupError(t *testing.T) {
	oldAfter := ScheduleAfter
	oldWarn := WarnLog
	var fire func()
	ScheduleAfter = func(_ int, f func()) { fire = f }
	defer func() { ScheduleAfter = oldAfter }()

	var warnedErr error
	defer func() { WarnLog = oldWarn }()
	WarnLog = func(err error, dir string) { warnedErr = err }

	oldRemove := removeFunc
	defer func() { removeFunc = oldRemove }()
	removeFunc = func(string) error { return errors.New("rm failed (forced)") }

	ScheduleCleanup("/wherever", "id")
	fire()
	if warnedErr == nil || warnedErr.Error() != "rm failed (forced)" {
		t.Errorf("WarnLog got %v, want the forced error", warnedErr)
	}
}

// TestDefaultScheduleAfterFires exercises the production defaultScheduleAfter
// (named fn, not the package-init closure) and the default WarnLog no-op,
// end-to-end via ScheduleCleanup with a short real timer.
func TestDefaultScheduleAfterFires(t *testing.T) {
	dir := t.TempDir()
	sub := path.Join(dir, "e2e")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Re-establish production defaults (earlier tests capture them).
	oldAfter := ScheduleAfter
	defer func() { ScheduleAfter = oldAfter }()
	ScheduleAfter = defaultScheduleAfter
	oldWarn := WarnLog
	defer func() { WarnLog = oldWarn }()
	WarnLog = defaultWarnLog

	ScheduleCleanup(dir, "e2e", 5)
	for i := 0; i < 400; i++ {
		if _, err := os.Stat(sub); os.IsNotExist(err) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatalf("default timer did not remove %s", sub)
	}
}

// TestDefaultWarnLogNoOp calls the production defaultWarnLog directly so its
// body is covered even though the package-init closure is usually captured.
func TestDefaultWarnLogNoOp(t *testing.T) {
	defaultWarnLog(errors.New("noop"), "/a/dir")
}

// TestRemoveOutputDirIsNotExistForced drives the os.IsNotExist branch of
// RemoveOutputDir (line 72-74) using the OS's genuine ENOENT error.
func TestRemoveOutputDirIsNotExistForced(t *testing.T) {
	old := removeFunc
	defer func() { removeFunc = old }()
	forceErr := os.ErrNotExist
	removeFunc = func(string) error { return forceErr }
	if err := RemoveOutputDir("/never"); err != nil {
		t.Fatalf("RemoveOutputDir on ENOENT must return nil (force: true); got %v", err)
	}
}
