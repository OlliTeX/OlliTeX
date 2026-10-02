package compilemanager

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"ollitex/go/services/clsitypst/commandrunner"
	off "ollitex/go/services/clsitypst/outputfilefinder"
	"ollitex/go/services/clsitypst/resourcewriter"

	"ollitex/go/services/clsitypst/dockerrunner"
	tr "ollitex/go/services/clsitypst/typstrunner"
)

// --- DoCompileWithLock: the compile happy path -----------------------------------

func TestCompileSuccess(t *testing.T) {
	m := newTestManager(t, nil)
	stats, timings := map[string]any{}, map[string]any{}
	req := makeReq()

	res, err := m.DoCompileWithLock(req, stats, timings)
	if err != nil {
		t.Fatal(err)
	}
	if res.BuildID != "b-test" {
		t.Fatalf("build id: %q", res.BuildID)
	}
	if len(res.OutputFiles) != 1 || res.OutputFiles[0].Path != "output.pdf" {
		t.Fatalf("output files: %+v", res.OutputFiles)
	}
	// isInitialCompile true (the mkdir CREATED the dir).
	if stats["isInitialCompile"] != 1 {
		t.Fatalf("stats: %v", stats)
	}
	if req.MetricsOpts.Compile != "initial" {
		t.Fatalf("compile kind: %q", req.MetricsOpts.Compile)
	}
	for _, k := range []string{"sync", "compile", "output", "compileE2E"} {
		if _, ok := timings[k]; !ok {
			t.Fatalf("missing timing %q: %v", k, timings)
		}
	}
}

func TestCompileBaseHistoryVersionEcho(t *testing.T) {
	m := newTestManager(t, nil)
	// a comparable value verifies the verbatim echo (D9 undefined-safe).
	req := makeReq()
	req.BaseHistoryVersion = "v42"
	res, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := res.BaseHistoryVersion.(string); !ok || got != "v42" {
		t.Fatalf("D9 echo: %v", res.BaseHistoryVersion)
	}
	// nil echoes as nil.
	req2 := makeReq()
	res2, err := m.DoCompileWithLock(req2, map[string]any{}, map[string]any{})
	if err != nil || res2.BaseHistoryVersion != nil {
		t.Fatalf("D9 nil echo: %v", res2.BaseHistoryVersion)
	}
}

// --- DoCompileWithLock: recompile path (existing dir) ----------------------------

func TestCompileRecompile(t *testing.T) {
	m := newTestManager(t, nil)
	// Pre-create the compile dir so the mkdir resolves NOT-created.
	if err := os.MkdirAll(compileDirOf(m.Paths.CompilesDir, "p1", "u1"), 0o700); err != nil {
		t.Fatal(err)
	}
	req := makeReq()
	if _, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if req.MetricsOpts.Compile != "recompile" {
		t.Fatalf("recompile kind: %q", req.MetricsOpts.Compile)
	}
	if req.IsInitialCompile {
		t.Fatal("recompile must not be initial")
	}
}

// --- D6 bootstrap (compileFromClsiCache) ------------------------------------------

func TestCompileFromClsiCacheRestored(t *testing.T) {
	m := newTestManager(t, nil)
	called := false
	m.DownloadLatestCompileCache = func(projectID, userID, compileDir string) (bool, error) {
		called = true
		return true, nil
	}
	req := makeReq()
	req.IsInitialCompile = true
	req.CompileFromClsiCache = true
	req.MetricsOpts.Compile = "initial"
	if _, err := m.doCompile(req, map[string]any{}, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("download not called")
	}
	if req.MetricsOpts.Compile != "from-clsi-cache" {
		t.Fatalf("kind: %q", req.MetricsOpts.Compile)
	}
}

func TestCompileFromClsiCacheWarn(t *testing.T) {
	m := newTestManager(t, nil)
	m.DownloadLatestCompileCache = func(projectID, userID, compileDir string) (bool, error) {
		return false, errors.New("cache boom")
	}
	req := makeReq()
	req.IsInitialCompile = true
	req.CompileFromClsiCache = true
	// error: warn + continue (do NOT abort).
	if _, err := m.doCompile(req, map[string]any{}, map[string]any{}); err != nil {
		t.Fatalf("warn path must continue: %v", err)
	}
}

func TestCompileMkdirError(t *testing.T) {
	m := newTestManager(t, nil)
	m.MkdirAll = func(dir string) (bool, error) { return false, errors.New("mkdir boom") }
	req := makeReq()
	if _, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{}); err == nil {
		t.Fatal("expected mkdir error")
	}
}

// --- error paths: RW error, empty resources -------------------------------------

func TestCompileRwError(t *testing.T) {
	m := newTestManager(t, nil)
	m.ResourceSync = func(req *resourcewriter.Request, basePath string) ([]resourcewriter.Resource, error) {
		return nil, errors.New("sync boom")
	}
	req := makeReq()
	_, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{})
	if err == nil || err.Error() != "sync boom" {
		t.Fatalf("rw error: %v", err)
	}
}

func TestCompileNoResources(t *testing.T) {
	m := newTestManager(t, nil)
	req := makeReq()
	req.RWResources = nil
	_, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{})
	if err == nil {
		t.Fatal("expected NotFound for zero resources")
	}
	if want := "no resources provided to compile"; err.Error()[:len(want)] != want {
		t.Fatalf("message: %q", err.Error())
	}
}

// --- error paths: runTypst error + save attach ------------------------------------

func TestCompileRunErrorTerminated(t *testing.T) {
	m := newTestManager(t, &dockerrunner.TerminatedError{})
	req := makeReq()
	_, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{})
	var ce *CompileRunError
	if !errors.As(err, &ce) {
		t.Fatalf("not a CompileRunError: %v", err)
	}
	if !ce.Terminated || ce.TimedOut {
		t.Fatalf("flags: %+v", ce)
	}
	// Node: the error carries outputFiles + buildId (save is attempted even
	// on failure so the user can read output.log).
	if len(ce.OutputFiles) == 0 || ce.BuildID == nil {
		t.Fatalf("failure must attach outputs: %+v %v", ce.OutputFiles, ce.BuildID)
	}
	// errors.As must still reach the dockerrunner typed error through the wrap.
	var te *dockerrunner.TerminatedError
	if !errors.As(err, &te) {
		t.Fatal("typed error must be reachable through CompileRunError")
	}
}

func TestCompileRunErrorTimedOut(t *testing.T) {
	m := newTestManager(t, &dockerrunner.TimedOutError{})
	req := makeReq()
	_, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{})
	var ce *CompileRunError
	if !errors.As(err, &ce) || !ce.TimedOut || ce.Terminated {
		t.Fatalf("timedout: %+v", err)
	}
	var to *dockerrunner.TimedOutError
	if !errors.As(err, &to) {
		t.Fatal("typed error must be reachable")
	}
}

func TestCompileRunErrorSaveFails(t *testing.T) {
	// save fails on the error path: the original error is rethrown, the save
	// error is NOT attached (Node catch: log only).
	m := newTestManager(t, &dockerrunner.TerminatedError{})
	m.FindOutputFiles = func(resources []off.Resource, directory string) (off.FindResult, error) {
		return off.FindResult{}, errors.New("find boom")
	}
	req := makeReq()
	_, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{})
	var ce *CompileRunError
	if !errors.As(err, &ce) {
		t.Fatalf("not a CompileRunError: %v", err)
	}
	if len(ce.OutputFiles) != 0 || ce.BuildID != nil {
		t.Fatalf("save-fail must not attach: %+v %v", ce.OutputFiles, ce.BuildID)
	}
}

func TestCompileSaveSuccessError(t *testing.T) {
	// success path: save error propagates as-is (no CompileRunError wrap).
	m := newTestManager(t, nil)
	m.SaveOutputFiles = func(req SaveOutputReq, rawFiles []off.OutputFile,
		compileDir, outputDir string, stats, timings map[string]float64,
	) (string, []off.OutputFile, error) {
		return "", nil, errors.New("save boom")
	}
	req := makeReq()
	_, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{})
	if err == nil || err.Error() != "save boom" {
		t.Fatalf("save error: %v", err)
	}
}

// --- runTypstOut seam (stats/timings forwarded) ------------------------------------

func TestRunTypstOut(t *testing.T) {
	tm := newTestManager(t, nil)
	var gotOpts tr.Options
	tm.RunTypst = func(name string, opts tr.Options, cb func(err error, out *commandrunner.RunOutput)) {
		gotOpts = opts
		cb(errors.New("boom"), nil)
	}
	env := compileEnv("p1")
	stats, timings := map[string]any{}, map[string]any{}
	err := runTypstOut(tm, "p1-u1", "/d", makeReq(), env, stats, timings)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err: %v", err)
	}
	if fmt.Sprintf("%p", gotOpts.Stats) != fmt.Sprintf("%p", stats) ||
		fmt.Sprintf("%p", gotOpts.Timings) != fmt.Sprintf("%p", timings) {
		t.Fatal("stats/timings not forwarded into the runner options")
	}
	if gotOpts.MainFile != "main.typ" || gotOpts.Directory != "/d" {
		t.Fatalf("opts: %+v", gotOpts)
	}
}

func TestTypstOpts(t *testing.T) {
	req := makeReq()
	req.Timeout = 999
	req.CompileGroup = "compile"
	opts := typstOpts(req, "/d", map[string]string{"K": "V"}, nil, nil)
	if opts.Timeout != 999 || opts.Environment["K"] != "V" || opts.CompileGroup != "compile" {
		t.Fatalf("opts: %+v", opts)
	}
}

// --- wordcount helpers + runCmdOut --------------------------------------------------

func TestParseWordcountOutput(t *testing.T) {
	if got := parseWordcountOutput("42 some/file.typ"); got.TextWords != 42 {
		t.Fatalf("parse: %v", got)
	}
	if got := parseWordcountOutput("no digits here"); got.TextWords != 0 {
		t.Fatalf("no digits: %v", got)
	}
	if got := parseWordcountOutput(""); got.TextWords != 0 || got.Encode != "utf-8" {
		t.Fatalf("empty: %v", got)
	}
}

func TestBuildWCCommand(t *testing.T) {
	cmd := buildWCCommand("main.typ")
	if len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-c" {
		t.Fatalf("cmd: %v", cmd)
	}
	ql := byte(0x22) // 0x22
	file := "main.typ"
	want := "wc -w " + string(ql) + "$COMPILE_DIR/" + file + string(ql) +
		" || echo " + string(ql) + "0 /compile/" + file + string(ql)
	if cmd[2] != want {
		t.Fatalf("wc template: %q want %q", cmd[2], want)
	}
}

func TestWordcountTimeout(t *testing.T) {
	if got := wordcountTimeout(nil, false); got != 60000 {
		t.Fatalf("default: %d", got)
	}
	if got := wordcountTimeout(nil, true); got != 300000 {
		t.Fatalf("fallback: %d", got)
	}
	if got := wordcountTimeout(&Request{Timeout: 12345}, false); got != 12345 {
		t.Fatalf("req: %d", got)
	}
}

func TestDwordcountEnv(t *testing.T) {
	m := newTestManager(t, nil)
	env := m.dwordcountEnv()
	if env["HOME"] != "/tmp" || env["CLSI"] != "1" {
		t.Fatalf("docker env not merged: %v", env)
	}
	if env["TYPST_PACKAGE_CACHE_PATH"] != "/tmp/.cache/typst" {
		t.Fatalf("typst cache not merged: %v", env)
	}
}

func TestRwResourcesToOff(t *testing.T) {
	rs := []resourcewriter.Resource{{Path: "a"}, {Path: "b"}}
	offs := rwResourcesToOff(rs)
	if len(offs) != 2 || offs[0].Path != "a" || offs[1].Path != "b" {
		t.Fatalf("off: %+v", offs)
	}
}

func TestRunCmdOut(t *testing.T) {
	r := &fakeRun{out: &commandrunner.RunOutput{Stdout: "42 /compile/main.typ", ExitCode: 0}}
	out, err := runCmdOut(r, "p", []string{"wc"}, "/d", "img", 60000, nil, "wordcount")
	if err != nil || out.Stdout != "42 /compile/main.typ" {
		t.Fatalf("out: %v %v", out, err)
	}
	// error path
	r.err = errors.New("run boom")
	if _, err := runCmdOut(r, "p", []string{"wc"}, "/d", "img", 60000, nil, "wordcount"); err == nil {
		t.Fatal("expected error")
	}
}
