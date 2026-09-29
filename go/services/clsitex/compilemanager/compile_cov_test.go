package compilemanager

import (
	"errors"
	"testing"

	dockerrunner "ollitex/go/services/clsitex/dockerrunner"
	cerrors "ollitex/go/services/clsitex/errors"
	"ollitex/go/services/clsitex/lockmanager"
)

// --- classify / strPtr / names -------------------------------------------------

func TestClassifyRunError(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	if e := m.classifyRunError(&dockerrunner.TerminatedError{}); !e.Terminated {
		t.Fatal("terminated flag not set")
	}
	if e := m.classifyRunError(&dockerrunner.ExitedError{Code: 1}); e.Code == nil || *e.Code != "1" {
		t.Fatal("exit code not carried")
	}
	if e := m.classifyRunError(&dockerrunner.TimedOutError{}); !e.TimedOut {
		t.Fatal("timedout flag not set")
	}
	plain := cerrors.NewOError("boom")
	if e := m.classifyRunError(plain); e.Message != "boom" || e.Cause != plain {
		t.Fatalf("plain error not wrapped: %v %v", e.Message, e.Cause)
	}
	cr := &CompileRunError{Message: "", Cause: errors.New("x")}
	if cr.Error() != "x" {
		t.Fatal("Error() should fall back to cause")
	}
	if (&CompileRunError{}).Error() != "compile error" {
		t.Fatal("Error() default message")
	}
	if cr.Unwrap() != cr.Cause {
		t.Fatal("Unwrap should return cause")
	}
	if got := strPtr("v"); *got != "v" {
		t.Fatal("strPtr value")
	}
}

func TestIsFilesOutOfSync(t *testing.T) {
	wrapped := cerrors.NewOError("outer").WithCause(cerrors.NewFilesOutOfSyncError("inner"))
	if !isFilesOutOfSync(wrapped) {
		t.Fatal("wrapped FilesOutOfSync not detected")
	}
	if !oIsFilesOutOfSync(wrapped) {
		t.Fatal("wrapped FilesOutOfSync (oIs) not detected")
	}
	if !isFilesOutOfSync(cerrors.NewFilesOutOfSyncError("x")) {
		t.Fatal("bare FilesOutOfSync not detected")
	}
	if isFilesOutOfSync(errors.New("no")) {
		t.Fatal("plain error matched")
	}
}

// --- doCompileWithLock ---------------------------------------------------------

func TestDoCompileWithLockMkdirError(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.MkdirAll = func(dir string) (bool, error) { return false, errors.New("mkdir failed") }
	if _, err := m.DoCompileWithLock(makeReq(), map[string]any{}, map[string]any{}); err == nil {
		t.Fatal("mkdir error should propagate")
	}
}

func TestDoCompileWithLockAcquireError(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.Acquire = func(key string) (*lockmanager.Lock, error) {
		return nil, cerrors.NewAlreadyCompilingError("compile in progress")
	}
	_, err := m.DoCompileWithLock(makeReq(), map[string]any{}, map[string]any{})
	var a *cerrors.AlreadyCompilingError
	if !errors.As(err, &a) {
		t.Fatalf("already-compiling should propagate: %v", err)
	}
}
