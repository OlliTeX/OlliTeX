package compilecontroller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	commandrunner "ollitex/go/services/clsitypst/commandrunner"
	dockerrunner "ollitex/go/services/clsitypst/dockerrunner"
	cerrors "ollitex/go/services/clsitypst/errors"
	"ollitex/go/services/clsitypst/lockmanager"
	off "ollitex/go/services/clsitypst/outputfilefinder"
	"ollitex/go/services/clsitypst/resourcewriter"

	compilemanager "ollitex/go/services/clsitypst/compilemanager"
	"ollitex/go/services/clsitypst/typstrunner"
)

// cb builds the requestparser input shape: {compile:{options:{...}}}.
func cb(opts map[string]any) map[string]any {
	return map[string]any{"compile": map[string]any{"options": opts}}
}

// assertWire checks the {compile:{status, ...}} envelope + HTTP code, and
// asserts NO clsiCacheShard key anywhere (D15: Node typst clsiCacheShard is
// always undefined; JSON.stringify omits it).
func assertWire(t *testing.T, res *ctrlTestRecorder, wantCode int, wantStatus string) map[string]any {
	t.Helper()
	if res.status != wantCode {
		t.Fatalf("want HTTP %d got %d (body: %s)", wantCode, res.status, res.body.String())
	}
	var env struct {
		Compile map[string]any `json:"compile"`
	}
	if err := json.Unmarshal(res.body.Bytes(), &env); err != nil {
		t.Fatalf("envelope: %v %s", err, res.body.String())
	}
	if _, has := env.Compile["clsiCacheShard"]; has {
		t.Fatalf("envelope must not carry clsiCacheShard (D15): %s", res.body.String())
	}
	got, _ := env.Compile["status"].(string)
	if got != wantStatus {
		t.Fatalf("want status %q got %q (body: %s)", wantStatus, got, res.body.String())
	}
	return env.Compile
}

// --- success (D16: output.pdf size>0) ------------------------------------------

func TestCompileDispatchSuccess(t *testing.T) {
	c := newTestController(t, t.TempDir())
	res := newCtrlRecorder()
	code, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"},
		cb(map[string]any{"stopOnFirstError": true, "buildId": "abc1-def2"}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("code: %d", code)
	}
	env := assertWire(t, res, 200, "success")
	// D16 success gate: output.pdf size>0 -> 'success' + timestamp stamp.
	if _, has := env["error"]; has {
		t.Fatalf("no error key expected: %v", env)
	}
	if env["buildId"] != "b1" {
		t.Fatalf("buildId: %v", env["buildId"])
	}
	// stats/timings flow through the envelope.
	if env["stats"] == nil || env["timings"] == nil {
		t.Fatalf("stats/timings missing: %v %v", env["stats"], env["timings"])
	}
	// the controller carries its config into the wire (D14 gate surface).
	if env["instanceType"] != "test-instance" || env["zone"] != "zone-a" ||
		env["isSpotInstance"] != true || env["outputUrlPrefix"] != "http://outputs.test" {
		t.Fatalf("config wire: %v", env)
	}
	files, _ := env["outputFiles"].([]any)
	if len(files) != 1 {
		t.Fatalf("outputFiles: %v", env["outputFiles"])
	}
	f0, _ := files[0].(map[string]any)
	wantURL := "http://download.test/project/p1/user/u1/build/b1/output/output.pdf"
	if f0["url"] != wantURL {
		t.Fatalf("url: %v (body %s)", f0["url"], res.body.String())
	}
	// success gate stamped the module timestamp (load agent, T10).
	if got := TimeSinceLastSuccessfulCompile(); got < 0 {
		t.Fatalf("time since last successful: %d", got)
	}
}

func TestCompileDispatchBuildIDAbsent(t *testing.T) {
	// buildId omitted from the wire when the save seam returns "".
	c := newTestController(t, t.TempDir())
	c.Manager.SaveOutputFiles = func(req compilemanager.SaveOutputReq, raw []off.OutputFile,
		compileDir, outputDir string, stats map[string]float64,
		timings map[string]float64) (string, []off.OutputFile, error) {
		return "", raw, nil
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(map[string]any{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	env := assertWire(t, res, 200, "success")
	if _, has := env["buildId"]; has {
		t.Fatalf("buildId omitted expected: %v", env)
	}
}

func TestCompileDispatchSuccessNoBHV(t *testing.T) {
	// no baseHistoryVersion in the body -> key omitted (D9 echo only when parsed).
	c := newTestController(t, t.TempDir())
	res := newCtrlRecorder()
	if _, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(map[string]any{})); err != nil {
		t.Fatalf("compile: %v", err)
	}
	env := assertWire(t, res, 200, "success")
	if _, has := env["baseHistoryVersion"]; has {
		t.Fatalf("bhv omitted expected: %v", env)
	}
}

func TestCompileDispatchBHVEcho(t *testing.T) {
	// D9 echo: compile.baseHistoryVersion reaches the wire on success.
	c := newTestController(t, t.TempDir())
	body := map[string]any{"compile": map[string]any{
		"options":            map[string]any{},
		"baseHistoryVersion": 7,
	}}
	res := newCtrlRecorder()
	if _, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, body); err != nil {
		t.Fatalf("compile: %v", err)
	}
	env := assertWire(t, res, 200, "success")
	if bhv, ok := env["baseHistoryVersion"].(float64); !ok || int(bhv) != 7 {
		t.Fatalf("bhv echo: %v", env["baseHistoryVersion"])
	}
}

// --- dead tails (D9: unreachable for typst, structure preserved) ----------------

func TestCompileDispatchStoppedOnFirstError(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.FindOutputFiles = func(rs []off.Resource, dir string) (off.FindResult, error) {
		return off.FindResult{}, nil // no output.pdf
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"},
		cb(map[string]any{"stopOnFirstError": true}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 200, "stopped-on-first-error")
}

func TestCompileDispatchFailure(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.FindOutputFiles = func(rs []off.Resource, dir string) (off.FindResult, error) {
		return off.FindResult{}, nil // no output.pdf -> failure
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(map[string]any{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 200, "failure")
}

// output.pdf present but size nil -> not > 0 -> failure gate (D16).
func TestCompileDispatchZeroSizePDF(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.FindOutputFiles = func(rs []off.Resource, dir string) (off.FindResult, error) {
		return off.FindResult{
			OutputFiles: []off.OutputFile{{Path: "output.pdf", Type: "pdf"}},
		}, nil
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(map[string]any{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 200, "failure")
}

// --- error dispatch (Node: the 423/409/503 chain) -------------------------------

func TestCompileDispatchAlreadyCompiling(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.Acquire = func(key string) (*lockmanager.Lock, error) {
		return nil, cerrors.NewAlreadyCompilingError("in progress")
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(map[string]any{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 423, "compile-in-progress")
}

func TestCompileDispatchFilesOutOfSync(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.ResourceSync = func(req *resourcewriter.Request, basePath string) ([]resourcewriter.Resource, error) {
		return nil, cerrors.NewFilesOutOfSyncError("fos")
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(map[string]any{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 409, "conflict")
}

func TestCompileDispatchMissingUpdates(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.ResourceSync = func(req *resourcewriter.Request, basePath string) ([]resourcewriter.Resource, error) {
		return nil, cerrors.NewMissingUpdatesError("MU", map[string]any{"baseHistoryVersion": 99})
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(map[string]any{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	env := assertWire(t, res, 409, "missing-updates")
	if bhv, ok := env["baseHistoryVersion"].(float64); !ok || int(bhv) != 99 {
		t.Fatalf("baseHistoryVersion: %v", env["baseHistoryVersion"])
	}
}

func TestCompileDispatchEPIPEShutdown(t *testing.T) {
	c := newTestController(t, t.TempDir())
	// docker stream close on shutdown: the error message carries the code.
	c.Manager.RunTypst = func(name string, opts typstrunner.Options,
		cb func(err error, out *commandrunner.RunOutput)) {
		cb(errors.New("stream EPIPE close"), nil)
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(map[string]any{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 503, "unavailable")
}

func TestCompileDispatchTooMany(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.Acquire = func(key string) (*lockmanager.Lock, error) {
		return nil, cerrors.NewTooManyCompileRequestsError("too many")
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(map[string]any{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 503, "unavailable")
}

func TestCompileDispatchTerminated(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.RunTypst = func(name string, opts typstrunner.Options,
		cb func(err error, out *commandrunner.RunOutput)) {
		cb(&dockerrunner.TerminatedError{}, nil)
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(map[string]any{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 200, "terminated")
}

func TestCompileDispatchTimedOut(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.RunTypst = func(name string, opts typstrunner.Options,
		cb func(err error, out *commandrunner.RunOutput)) {
		cb(&dockerrunner.TimedOutError{}, nil)
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(map[string]any{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 200, "timedout")
}

func TestCompileDispatchGenericError(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.RunTypst = func(name string, opts typstrunner.Options,
		cb func(err error, out *commandrunner.RunOutput)) {
		cb(errors.New("boom"), nil)
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(map[string]any{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	env := assertWire(t, res, 500, "error")
	// the decorated error payload (error.outputFiles / error.buildId) flows
	// into the wire (Node: the catch attaches both).
	if env["buildId"] == nil {
		t.Fatalf("error buildId: %v", env)
	}
}

// empty Error() -> errorRender "object" (Node: error?.message || error -> {}
// via JSON.stringify).
func TestCompileDispatchEmptyErrorObjectRender(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.ResourceSync = func(req *resourcewriter.Request, basePath string) ([]resourcewriter.Resource, error) {
		return nil, &emptyMsgError{}
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(map[string]any{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 500, "error")
	var env struct {
		Compile map[string]any `json:"compile"`
	}
	if uerr := json.Unmarshal(res.body.Bytes(), &env); uerr != nil {
		t.Fatalf("envelope: %v", uerr)
	}
	if _, ok := env.Compile["error"].(map[string]any); !ok {
		t.Fatalf("error object render: %v / %s", env.Compile["error"], res.body.String())
	}
}

func TestCompileDispatchParseError(t *testing.T) {
	// (0, err) is the Node next(error) surface — the error middleware (T10)
	// maps the final status.
	c := newTestController(t, t.TempDir())
	if code, err := c.Compile(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"},
		map[string]any{"bogus": 1}); code != 0 || err == nil {
		t.Fatalf("want (0, err), got %d %v", code, err)
	}
	// invalid compiler -> parse error.
	if code, err := c.Compile(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"},
		cb(map[string]any{"compiler": "lualatex"})); code != 0 || err == nil {
		t.Fatalf("want parse (0, err), got %d %v", code, err)
	}
}

// --- MarkProjectAccessed (Node: markProjectAsJustAccessed) -----------------------

func TestCompileMarksProjectAccessed(t *testing.T) {
	c := newTestController(t, t.TempDir())
	var captured string
	var ts int64
	c.MarkProjectAccessed = func(projectID string, nowMs int64) {
		captured = projectID
		ts = nowMs
	}
	res := newCtrlRecorder()
	if _, err := c.Compile(res, ProjectUser{ProjectID: "pmark", UserID: "umark"}, cb(map[string]any{})); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if captured != "pmark" || ts <= 0 {
		t.Fatalf("mark: %s %d", captured, ts)
	}
}

type emptyMsgError struct{}

func (*emptyMsgError) Error() string { return "" }

// --- unit: the dispatch discriminants -------------------------------------------

func TestHasOutputPDF(t *testing.T) {
	if hasOutputPDF([]off.OutputFile{}) {
		t.Fatal("empty -> false")
	}
	if hasOutputPDF([]off.OutputFile{{Path: "output.log"}}) {
		t.Fatal("no pdf -> false")
	}
	zero := int64(0)
	if hasOutputPDF([]off.OutputFile{{Path: "output.pdf", Size: &zero}}) {
		t.Fatal("zero size -> false")
	}
	one := int64(1)
	if !hasOutputPDF([]off.OutputFile{{Path: "output.pdf", Size: &one}}) {
		t.Fatal("size>0 -> true")
	}
}

func TestBhvFromInterface(t *testing.T) {
	if bhvFromInterface(nil) != nil {
		t.Fatal("nil -> nil")
	}
	if bhvFromInterface("x") != nil {
		t.Fatal("unknown kind -> nil")
	}
	if got := bhvFromInterface(int(5)); got == nil || *got != 5 {
		t.Fatal("int")
	}
	if got := bhvFromInterface(int64(6)); got == nil || *got != 6 {
		t.Fatal("int64")
	}
	if got := bhvFromInterface(float64(7)); got == nil || *got != 7 {
		t.Fatal("float64")
	}
}

func TestBhvOf(t *testing.T) {
	if got := bhvOf(&cerrors.MissingUpdatesError{Info: nil}); got != nil {
		t.Fatal("nil info -> nil")
	}
	if got := bhvOf(&cerrors.MissingUpdatesError{Info: map[string]any{}}); got != nil {
		t.Fatal("no key -> nil")
	}
	if got := bhvOf(&cerrors.MissingUpdatesError{Info: map[string]any{"baseHistoryVersion": int(1)}}); got == nil || *got != 1 {
		t.Fatal("int")
	}
	if got := bhvOf(&cerrors.MissingUpdatesError{Info: map[string]any{"baseHistoryVersion": int64(2)}}); got == nil || *got != 2 {
		t.Fatal("int64")
	}
	if got := bhvOf(&cerrors.MissingUpdatesError{Info: map[string]any{"baseHistoryVersion": float64(3)}}); got == nil || *got != 3 {
		t.Fatal("float64")
	}
	if got := bhvOf(&cerrors.MissingUpdatesError{Info: map[string]any{"baseHistoryVersion": "no"}}); got != nil {
		t.Fatal("non-numeric -> nil")
	}
}

func TestIsEPipe(t *testing.T) {
	if isEPipe(nil) {
		t.Fatal("nil -> false")
	}
	if isEPipe(errors.New("boom")) {
		t.Fatal("generic -> false")
	}
	if !isEPipe(errors.New("docker: stream EPIPE close")) {
		t.Fatal("EPIPE -> true")
	}
}

func TestAsCompileRunError(t *testing.T) {
	if asCompileRunError(nil) != nil {
		t.Fatal("nil -> nil")
	}
	cre := &compilemanager.CompileRunError{Message: "m"}
	if asCompileRunError(cre) != cre {
		t.Fatal("direct *CompileRunError")
	}
	if asCompileRunError(errors.New("boom")) != nil {
		t.Fatal("generic -> nil")
	}
	// a wrapped error still reaches the classifyRunError extraction (Unwrap).
	wrapped := fmt.Errorf("outer: %w", cre)
	if got := asCompileRunError(wrapped); got != cre {
		t.Fatal("wrapped -> extracted")
	}
}
