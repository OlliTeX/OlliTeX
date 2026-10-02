package compilemanager

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ollitex/go/services/clsitypst/commandrunner"
	"ollitex/go/services/clsitypst/lockmanager"
	off "ollitex/go/services/clsitypst/outputfilefinder"
	"ollitex/go/services/clsitypst/resourcewriter"

	cltypstcfg "ollitex/go/services/clsitypst/config"
	"ollitex/go/services/clsitypst/dockerrunner"
	tr "ollitex/go/services/clsitypst/typstrunner"
)

// --- fakes (clsi.go seams_test.go conventions) ----------------------------------

// fakeRun is a minimal clsi generic commandrunner.Runner fake
// (wordcount runs; the compile path goes through RunTypst instead).
type fakeRun struct {
	out *commandrunner.RunOutput
	err error
}

func (r *fakeRun) Run(projectID string, command []string, directory string,
	image string, timeout int64, environment map[string]string, group string, cwd string,
	callback func(err error, out *commandrunner.RunOutput)) string {
	if r.err != nil {
		callback(r.err, nil)
	} else {
		callback(nil, r.out)
	}
	return "typst-project-" + projectID
}

func (r *fakeRun) Kill(containerID string, callback func(err error)) { callback(nil) }
func (r *fakeRun) CanRunSyncTeXInOutputDir() bool                    { return false }

// newTestManager builds a Manager with every seam faked. Paths point at a
// temp tree; tests override individual seams for the branch under test.
// (clsi.go newTestManager convention.)
func newTestManager(t *testing.T, typstErr error) *Manager {
	t.Helper()
	base := t.TempDir()
	m := &Manager{
		Paths: Paths{
			CompilesDir: base + "/compiles",
			OutputDir:   base + "/output",
		},
		DockerEnv: map[string]string{"HOME": "/tmp", "CLSI": "1"},
		Now:       func() int64 { return time.Now().UnixMilli() },
	}
	m.MkdirAll = func(dir string) (bool, error) { return mkdirAllWithIdentity(dir) }
	// Stateless fake (clsi.go seams_test convention): the REAL lockmanager
	// touches clsi/config.Get() (sandbox-gated env). The held-lock (423)
	// case is exercised by overriding m.Acquire in the dedicated test.
	m.Acquire = func(key string) (*lockmanager.Lock, error) {
		return &lockmanager.Lock{Key: key}, nil
	}
	m.GetLock = func(key string) *lockmanager.Lock { return nil }
	m.PrepareCompileDir = func(compileDir string) {}
	m.ResourceSync = func(req *resourcewriter.Request, basePath string) ([]resourcewriter.Resource, error) {
		return req.Resources, nil
	}
	m.FindOutputFiles = func(resources []off.Resource, directory string) (off.FindResult, error) {
		sz := int64(13)
		return off.FindResult{
			OutputFiles: []off.OutputFile{{Path: "output.pdf", Type: "pdf", Size: &sz}},
			AllEntries:  []string{"output.pdf", "main.typ"},
		}, nil
	}
	m.SaveOutputFiles = func(req SaveOutputReq, rawFiles []off.OutputFile,
		compileDir, outputDir string, stats, timings map[string]float64,
	) (string, []off.OutputFile, error) {
		return "b-test", rawFiles, nil
	}
	m.Runner = &fakeRun{out: &commandrunner.RunOutput{Stdout: "ok", ExitCode: 0}}
	// typst runner seams (default: success, or the error the test injects).
	m.RunTypst = func(name string, opts tr.Options, cb func(err error, out *commandrunner.RunOutput)) {
		cb(typstErr, &commandrunner.RunOutput{ExitCode: 0})
	}
	m.IsRunning = func(name string) bool { return false }
	m.KillTypst = func(name string, cb func(err error)) { cb(nil) }
	return m
}

// makeReq builds a minimal compile request (root main.typ, full sync).
func makeReq() *Request {
	return &Request{
		ProjectID: "p1", UserID: "u1",
		Compiler:         "typst",
		RootResourcePath: "main.typ",
		SyncType:         "full",
		RWResources:      []resourcewriter.Resource{{Path: "main.typ"}},
		MetricsOpts:      MetricsOpts{Path: "/render", Method: "POST"},
	}
}

// --- the dockerrunner error values (clsi.go dockerrunner_test.go convention) ------
// The copied dockerrunner's typed errors are the classifyRunError discriminants:
// *TerminatedError (exit 137), *TimedOutError (timeout), *ExitedError{Code}.
func terminatedError() error     { return &dockerrunner.TerminatedError{} }
func timedOutError() error       { return &dockerrunner.TimedOutError{} }
func exitedError(code int) error { return &dockerrunner.ExitedError{Code: code} }

// --- TestNew (clsi.go TestNewConstructorWiring convention) -------------------------

// testdataMarkerPDF resolves the committed wordcount fixture marker PDF
// (wordcount/testdata/markerproj/marker.pdf), relative to this package.
func testdataMarkerPDF() string {
	return filepath.Join("..", "wordcount", "testdata", "markerproj", "marker.pdf")
}

func TestNewWiring(t *testing.T) {
	// env so config.New populates the clsi block (Docker.Env) per D23.
	t.Setenv("SANDBOXED_COMPILES", "true")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/tmp/c")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", "/tmp/nc")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_OUTPUT", "/tmp/o")
	cfg := cltypstcfg.ForTest()

	cr := &fakeRun{out: &commandrunner.RunOutput{}}
	m := New(cfg, nil, cr)
	if m == nil {
		t.Fatal("nil manager")
	}
	if m.Paths.CompilesDir == "" || m.Paths.OutputDir == "" {
		t.Fatalf("paths not set: %+v", m.Paths)
	}
	if m.Acquire == nil || m.GetLock == nil || m.FindOutputFiles == nil ||
		m.ResourceSync == nil || m.SaveOutputFiles == nil ||
		m.DownloadLatestCompileCache == nil {
		t.Fatal("module seams not wired")
	}
	if m.MkdirAll == nil || m.PrepareCompileDir == nil || m.Runner == nil {
		t.Fatal("fs/runner seams not wired")
	}
	// T7: the wordcount seams are ALL wired in production (D22: local
	// packages; D5 fixture-picked 2026-09-29: the gopdf-line engine at
	// github.com/ledongthuc/pdf won; rsc.io/pdf v0.1.1 failed the fixture).
	if m.InjectWordometer == nil || m.RemoveArtifacts == nil || m.ReadPdfMarker == nil {
		t.Fatal("wordcount injector+engine not wired")
	}
	// The engine really parses the committed fixture marker (wordcount
	// package, pdfmarker_test) — verify wiring end-to-end here too.
	if total, heading, heads, ok := m.ReadPdfMarker(testdataMarkerPDF()); !ok {
		t.Fatal("wired ReadPdfMarker failed on the committed fixture marker")
	} else if total != 12 || heading != 0 || heads != 0 {
		t.Fatalf("wired ReadPdfMarker fixture values: %d %d %d", total, heading, heads)
	}
	if m.DockerEnv["HOME"] != "/tmp" || m.DockerEnv["CLSI"] != "1" {
		t.Fatalf("docker env not wired: %v", m.DockerEnv)
	}

	// tr != nil branch: the typst runner's method values are wired.
	trr := tr.New(cr, nil, nil)
	m2 := New(cfg, trr, cr)
	if m2.RunTypst == nil || m2.IsRunning == nil || m2.KillTypst == nil {
		t.Fatal("typst runner != nil branch not wired")
	}
}

// --- unit: helpers ------------------------------------------------------------------

func TestCompileNameAndDir(t *testing.T) {
	if got := compileName("p1", ""); got != "p1" {
		t.Fatalf("user empty: %q", got)
	}
	if got := compileName("p1", "u1"); got != "p1-u1" {
		t.Fatalf("named: %q", got)
	}
	if got := compileDirOf("/x/compiles", "p1", ""); got != "/x/compiles/p1" {
		t.Fatalf("dir: %q", got)
	}
	if got := compileDirOf("/x/compiles", "p1", "u1"); got != "/x/compiles/p1-u1" {
		t.Fatalf("dir: %q", got)
	}
}

func TestCompileEnv(t *testing.T) {
	env := compileEnv("p1")
	if env["OVERLEAF_PROJECT_ID"] != "p1" {
		t.Fatalf("env: %v", env)
	}
	if env["HOME"] != "/tmp" || env["TYPST_PACKAGE_CACHE_PATH"] != "/tmp/.cache/typst" {
		t.Fatalf("TYPST_DOCKER_ENV not merged: %v", env)
	}
}

func TestMkdirAllWithIdentity(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "a", "b")
	created, err := mkdirAllWithIdentity(dir)
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	// existing dir -> NOT created (Node: mkdir recursive on an existing dir
	// resolves without the path; the identity is false).
	created, err = mkdirAllWithIdentity(dir)
	if err != nil || created {
		t.Fatalf("existing: %v %v", created, err)
	}
	// error path: target path exists as a FILE.
	file := filepath.Join(tmp, "f")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mkdirAllWithIdentity(file); err == nil {
		t.Fatal("expected error creating dir over a file")
	}
}

func TestClassifyRunError(t *testing.T) {
	ce := classifyRunError(terminatedError())
	if !ce.Terminated || ce.TimedOut {
		t.Fatalf("terminated flags: %+v", ce)
	}
	if ce.Message != "terminated" {
		t.Fatalf("terminated message: %q", ce.Message)
	}
	ce = classifyRunError(timedOutError())
	if !ce.TimedOut || ce.Terminated {
		t.Fatalf("timedout flags: %+v", ce)
	}
	// exited: not raised on the compile path (D16) -> unclassified, msg "exited".
	ce = classifyRunError(exitedError(1))
	if ce.Terminated || ce.TimedOut || ce.Message != "exited" {
		t.Fatalf("exited: %+v", ce)
	}
	// nil cause: Message empty, no flags, default Error() text.
	ce = classifyRunError(nil)
	if ce.Message != "" || ce.Terminated || ce.TimedOut {
		t.Fatalf("nil: %+v", ce)
	}
	if got := ce.Error(); got != "compile error" {
		t.Fatalf("default text: %q", got)
	}
}

func TestCompileErrorMsgAndUnwrap(t *testing.T) {
	cause := terminatedError()
	ce := &CompileRunError{Cause: cause, Message: "custom"}
	if got := ce.Error(); got != "custom" {
		t.Fatalf("Message wins: %q", got)
	}
	if got := ce.Unwrap(); got != cause {
		t.Fatalf("unwrap: %v", got)
	}
}

func TestMetricsOptsMap(t *testing.T) {
	req := makeReq()
	req.MetricsOpts.Compile = "initial"
	m := metricsOptsMap(req)
	if m["path"] != "/render" || m["method"] != "POST" || m["compile"] != "initial" {
		t.Fatalf("map: %v", m)
	}
}

func TestAnyToFloatMap(t *testing.T) {
	in := map[string]any{"a": 1, "b": int64(2), "c": 3.5, "d": "skip"}
	out := anyToFloatMap(in)
	if out["a"] != 1 || out["b"] != 2 || out["c"] != 3.5 {
		t.Fatalf("projections: %v", out)
	}
	if _, ok := out["d"]; ok {
		t.Fatal("non-numeric must not be projected")
	}
	if out := anyToFloatMap(nil); len(out) != 0 {
		t.Fatalf("nil map: %v", out)
	}
}

func TestMergeFloatBack(t *testing.T) {
	into := map[string]any{"kept": 1}
	src := map[string]float64{"new": 2.5, "kept": 99}
	mergeFloatBack(into, src)
	if into["kept"] != 1 {
		t.Fatal("must not overwrite existing keys")
	}
	if into["new"] != 2.5 {
		t.Fatal("must add new keys")
	}
}

func TestStrPtr(t *testing.T) {
	if p := strPtr("x"); *p != "x" {
		t.Fatal("strPtr")
	}
}
