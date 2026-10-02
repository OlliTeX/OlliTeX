package typstrunner

import (
	"errors"
	"testing"

	"ollitex/go/services/clsitypst/commandrunner"
)

// inlineFakeRunner captures Run invocations and lets tests drive the
// callback (mirroring Node's async docker callback).
type inlineFakeRunner struct {
	captured    map[string]func(err error, out *commandrunner.RunOutput)
	lastTimeout int64
	killErr     error
	killVal     string
	killCalls   int
	syncErr     error // error to fire synchronously (image-not-allowed path)
}

func newInlineFakeRunner() *inlineFakeRunner {
	return &inlineFakeRunner{captured: map[string]func(err error, out *commandrunner.RunOutput){}}
}

func (f *inlineFakeRunner) drive(projectID string, out *commandrunner.RunOutput, err error) {
	cb, ok := f.captured[projectID]
	if !ok {
		return
	}
	cb(err, out)
}

func (f *inlineFakeRunner) Run(projectID string, command []string, directory string, image string,
	timeout int64, environment map[string]string, compileGroup string, cwd string,
	callback func(err error, out *commandrunner.RunOutput)) string {
	if f.syncErr != nil {
		callback(f.syncErr, nil)
		return ""
	}
	f.lastTimeout = timeout
	f.captured[projectID] = callback
	return "container-" + projectID
}

func (f *inlineFakeRunner) Kill(containerID string, callback func(err error)) {
	f.killVal = containerID
	f.killCalls++
	callback(f.killErr)
}

func (f *inlineFakeRunner) CanRunSyncTeXInOutputDir() bool { return true }

func resetCaptureAndTable(f *inlineFakeRunner) {
	processMu.Lock()
	processTable = map[string]string{}
	processMu.Unlock()
	f.captured = map[string]func(err error, out *commandrunner.RunOutput){}
}

// --- buildTypstCompileCommand -----------------------------------------

func TestBuildTypstCompileCommand(t *testing.T) {
	cmd := buildTypstCompileCommand("main.typ")
	if len(cmd) != 7 {
		t.Fatalf("arg count = %d, want 7: %v", len(cmd), cmd)
	}
	if cmd[0] != "sh" || cmd[1] != "-c" || cmd[3] != "--" {
		t.Fatalf("shape wrong: %v", cmd)
	}
	if cmd[4] != "$COMPILE_DIR/main.typ" {
		t.Errorf("mainFile arg = %q", cmd[4])
	}
	if cmd[5] != "output.pdf" || cmd[6] != "output.log" {
		t.Errorf("output args = %v", cmd[5:])
	}
	// the deliberate exit-0 convention (D16).
	if !contains(cmd[2], "exit 0") {
		t.Errorf("sh -c body must end exit 0 = %q", cmd[2])
	}
	if c := buildTypstCompileCommand("sub/dir/main.typ"); c[4] != "$COMPILE_DIR/sub/dir/main.typ" {
		t.Errorf("subpath = %q", c[4])
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- IsRunning / ProcessTable -----------------------------------------

func TestIsRunningInitialState(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	r := New(f, nil, nil)
	if r.IsRunning("never-started") {
		t.Fatalf("should not be running")
	}
}

func TestRunTypstSetsThenClearsProcessTable(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	r := New(f, nil, nil)

	var gotErr error
	var gotOut *commandrunner.RunOutput
	r.RunTypst("pid-1", Options{
		Directory: t.TempDir(),
		MainFile:  "main.typ",
		Stats:     map[string]any{},
	}, func(err error, out *commandrunner.RunOutput) {
		gotErr = err
		gotOut = out
	})
	if !r.IsRunning("pid-1") {
		t.Fatalf("should be running while callback pending")
	}
	out := &commandrunner.RunOutput{Stdout: "typst 0.15.1", ExitCode: 0}
	f.drive("pid-1", out, nil)
	if gotErr != nil {
		t.Fatalf("unexpected err: %v", gotErr)
	}
	if gotOut != out {
		t.Fatalf("out not echoed: %v", gotOut)
	}
	if r.IsRunning("pid-1") {
		t.Fatalf("should be cleared after callback")
	}
}

func TestRunTypstErrorsAndStats(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	r := New(f, nil, nil)
	stats := map[string]any{}
	stdout := "typst 0.15.1\nerror: main.typ:1:1 expected \")\"\nhello\nerror: main.typ:2:1 expected \")\"\nworld\ntypst: This file is at /tmp/x\n"
	r.RunTypst("pid-s", Options{Stats: stats}, func(err error, o *commandrunner.RunOutput) {})
	f.drive("pid-s", &commandrunner.RunOutput{Stdout: stdout, ExitCode: 0}, nil)
	if stats["typst-errors"] != 2 {
		t.Errorf("typst-errors = %v, want 2", stats["typst-errors"])
	}
	if stats["typst-compile-runs"] != 1 {
		t.Errorf("typst-compile-runs = %v, want 1", stats["typst-compile-runs"])
	}
}

func TestRunTypstErrorCallbackShortCircuitsStats(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	r := New(f, nil, nil)
	stats := map[string]any{}
	var gotErr error
	r.RunTypst("pid-e", Options{Stats: stats}, func(err error, out *commandrunner.RunOutput) {
		gotErr = err
	})
	f.drive("pid-e", nil, errors.New("boom"))
	if gotErr == nil {
		t.Fatalf("error not propagated")
	}
	if _, ok := stats["typst-errors"]; ok {
		t.Fatalf("stats written on error path: %v", stats)
	}
	if r.IsRunning("pid-e") {
		t.Fatalf("process table not cleared on error")
	}
}

func TestRunTypstSyncErrorNotInProcessTable(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	r := New(f, nil, nil)
	f.syncErr = errors.New("image not allowed")
	r.RunTypst("pid-sync", Options{}, func(err error, out *commandrunner.RunOutput) {})
	if r.IsRunning("pid-sync") {
		t.Fatalf("sync-error run must not stick in ProcessTable")
	}
	f.syncErr = nil
}

func TestRunTypstDefaultsTimeout(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	r := New(f, nil, nil)
	r.RunTypst("pid-t", Options{Stats: map[string]any{}},
		func(err error, out *commandrunner.RunOutput) {})
	if f.lastTimeout != 60000 {
		t.Fatalf("default timeout = %d, want 60000", f.lastTimeout)
	}
}

func TestRunTypstExplicitTimeout(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	r := New(f, nil, nil)
	r.RunTypst("pid-t2", Options{Timeout: 12345},
		func(err error, out *commandrunner.RunOutput) {})
	if f.lastTimeout != 12345 {
		t.Fatalf("timeout = %d, want 12345", f.lastTimeout)
	}
}

func TestRunTypstNilStatsMap(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	r := New(f, nil, nil)
	var got *commandrunner.RunOutput
	r.RunTypst("pid-nil", Options{Stats: nil}, func(err error, out *commandrunner.RunOutput) {
		got = out
	})
	f.drive("pid-nil", &commandrunner.RunOutput{Stdout: "error: x\n", ExitCode: 0}, nil)
	if got == nil {
		t.Fatal("callback not fired")
	}
}

func TestIsRunningAndKillRunning(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	r := New(f, nil, nil)
	var killed bool
	r.RunTypst("pid-k", Options{}, func(err error, out *commandrunner.RunOutput) {})
	if !r.IsRunning("pid-k") {
		t.Fatalf("pid-k should be running")
	}
	r.KillTypst("pid-k", func(err error) {
		if err == nil {
			killed = true
		}
	})
	if !killed {
		t.Fatal("kill callback not fired")
	}
	if f.killVal != "container-pid-k" {
		t.Fatalf("kill container = %q", f.killVal)
	}
	if f.killCalls != 1 {
		t.Fatalf("kill calls = %d", f.killCalls)
	}
}

// --- RunTypstAsync (promisified wrapper) --------------------------------

func TestRunTypstAsyncSuccess(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	r := New(f, nil, nil)
	done := r.RunTypstAsync("pid-a", Options{Stats: map[string]any{}})
	if !r.IsRunning("pid-a") {
		t.Fatalf("pid-a should be running")
	}
	f.drive("pid-a", &commandrunner.RunOutput{ExitCode: 0}, nil)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("async err = %v", err)
		}
	default:
		t.Fatal("async chan empty after drive")
	}
}

func TestRunTypstAsyncError(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	r := New(f, nil, nil)
	done := r.RunTypstAsync("pid-a2", Options{Stats: map[string]any{}})
	f.drive("pid-a2", nil, errors.New("boom"))
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("async error not delivered")
		}
	default:
		t.Fatal("async chan empty after error drive")
	}
}

func TestKillTypstNothingRunning(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	r := New(f, nil, nil)
	var called bool
	r.KillTypst("no-such", func(err error) {
		called = true
		if err != nil {
			t.Fatalf("kill missing should cb(nil), got %v", err)
		}
	})
	if !called {
		t.Fatal("kill callback not fired")
	}
	if f.killCalls != 0 {
		t.Fatalf("runner.Kill must not be called, got %d", f.killCalls)
	}
}
