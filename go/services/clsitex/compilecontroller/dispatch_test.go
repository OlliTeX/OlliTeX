package compilecontroller

import (
	"encoding/json"
	"errors"
	"testing"

	clsicachehandler "ollitex/go/services/clsitex/clsicachehandler"
	"ollitex/go/services/clsitex/commandrunner"
	dockerrunner "ollitex/go/services/clsitex/dockerrunner"
	cerrors "ollitex/go/services/clsitex/errors"
	latexrunner "ollitex/go/services/clsitex/latexrunner"
	"ollitex/go/services/clsitex/lockmanager"
	off "ollitex/go/services/clsitex/outputfilefinder"
	"ollitex/go/services/clsitex/resourcewriter"
)

// compileBody builds the requestparser input shape: {compile:{options:{...}}}.
func cb(opts map[string]any) map[string]any {
	return map[string]any{"compile": map[string]any{"options": opts}}
}

// assertWire checks the {compile:{status, code, ...}} envelope + HTTP code.
func assertWire(t *testing.T, res *ctrlTestRecorder, wantCode int, wantStatus string) map[string]any {
	if res.status != wantCode {
		t.Fatalf("want HTTP %d got %d (body: %s)", wantCode, res.status, res.body.String())
	}
	var env struct {
		Compile map[string]any `json:"compile"`
	}
	if err := json.Unmarshal(res.body.Bytes(), &env); err != nil {
		t.Fatalf("envelope: %v %s", err, res.body.String())
	}
	got, _ := env.Compile["status"].(string)
	if got != wantStatus {
		t.Fatalf("want status %q got %q (body: %s)", wantStatus, got, res.body.String())
	}
	return env.Compile
}

func TestCompileDispatchSuccess(t *testing.T) {
	c := newTestController(t, t.TempDir())
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"},
		cb(map[string]any{"editorId": "b5e3944b-8b75-4f9a-8d5a-12cd2c6e239e",
			"populateClsiCache": true, "buildId": "abc1-def2",
			"imageName": "texlive/texlive:2024"}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 200, "success")
}

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
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(nil))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 200, "failure")
}

func TestCompileDispatchCoreFile(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.FindOutputFiles = func(rs []off.Resource, dir string) (off.FindResult, error) {
		sz := int64(13)
		return off.FindResult{
			OutputFiles: []off.OutputFile{{Path: "output.pdf", Size: &sz}, {Path: "core"}},
			AllEntries:  []string{"output.pdf", "core"},
		}, nil
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(nil))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// core file present but output.pdf still present -> success (error logged).
	assertWire(t, res, 200, "success")
}

func TestCompileDispatchAlreadyCompiling(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.Acquire = func(key string) (*lockmanager.Lock, error) {
		return nil, cerrors.NewAlreadyCompilingError("in progress")
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(nil))
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
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(nil))
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
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(nil))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	env := assertWire(t, res, 409, "missing-updates")
	bhv, ok := env["baseHistoryVersion"].(float64)
	if !ok || int(bhv) != 99 {
		t.Fatalf("baseHistoryVersion: %v", env["baseHistoryVersion"])
	}
}

func TestCompileDispatchEPIPEShutdown(t *testing.T) {
	c := newTestController(t, t.TempDir())
	// docker stream close on shutdown -> err string contains EPIPE.
	c.Manager.RunLatex = func(name string, opts latexrunner.Options, cb func(error, *commandrunner.RunOutput)) {
		cb(errors.New("stream EPIPE close"), nil)
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(nil))
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
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(nil))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 503, "unavailable")
}

func TestCompileDispatchTerminated(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.RunLatex = func(name string, opts latexrunner.Options, cb func(error, *commandrunner.RunOutput)) {
		cb(&dockerrunner.TerminatedError{}, nil)
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(nil))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 200, "terminated")
}

func TestCompileDispatchTimedOut(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.RunLatex = func(name string, opts latexrunner.Options, cb func(error, *commandrunner.RunOutput)) {
		cb(&dockerrunner.TimedOutError{}, nil)
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(nil))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 200, "timedout")
}

func TestCompileDispatchValidationPass(t *testing.T) {
	c := newTestController(t, t.TempDir())
	// check=validate + successful exit -> validation pass (code 0 -> 200).
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"},
		cb(map[string]any{"check": "validate"}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 200, "validation-pass")
}

func TestCompileDispatchValidationFail(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.RunLatex = func(name string, opts latexrunner.Options, cb func(error, *commandrunner.RunOutput)) {
		cb(&dockerrunner.ExitedError{Code: 1}, nil) // chktex exit 1 = validation fail
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"},
		cb(map[string]any{"check": "error"}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 200, "validation-fail")
}

func TestCompileDispatchGenericError(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.RunLatex = func(name string, opts latexrunner.Options, cb func(error, *commandrunner.RunOutput)) {
		cb(errors.New("boom"), nil)
	}
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, cb(nil))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWire(t, res, 500, "error")
}

func TestCompileNotifyGating(t *testing.T) {
	c := newTestController(t, t.TempDir())
	notifyCalls := 0
	c.Notify = func(opts clsicachehandler.NotifyOpts) string {
		notifyCalls++
		return "shard-test"
	}
	// (a) success + editorId + populateClsiCache => Notify IS called.
	res := newCtrlRecorder()
	_, err := c.Compile(res, ProjectUser{ProjectID: "p1"},
		cb(map[string]any{"editorId": "b5e3944b-8b75-4f9a-8d5a-12cd2c6e23f5",
			"populateClsiCache": true}))
	if err != nil {
		t.Fatal(err)
	}
	assertWire(t, res, 200, "success")
	if notifyCalls != 1 {
		t.Fatalf("Notify calls (a): %d != 1 (body %s)", notifyCalls, res.body.String())
	}

	// (b) no populateClsiCache => Notify NOT called.
	notifyCalls = 0
	res2 := newCtrlRecorder()
	if _, err := c.Compile(res2, ProjectUser{ProjectID: "p1"},
		cb(map[string]any{"editorId": "b5e3944b-8b75-4f9a-8d5a-12cd2c6e23f5"})); err != nil {
		t.Fatal(err)
	}
	if notifyCalls != 0 {
		t.Fatalf("Notify calls (b): %d != 0", notifyCalls)
	}
}
