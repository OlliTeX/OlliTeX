package compilecontroller

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	commandrunner "ollitex/go/services/clsitex/commandrunner"
	cerrors "ollitex/go/services/clsitex/errors"
	"ollitex/go/services/clsitex/lastprojectaccess"
	"ollitex/go/services/clsitex/lockmanager"
	"ollitex/go/services/clsitex/resourcewriter"
)

// --- status / stop / clearCache ------------------------------------------------

func TestStatusHandler(t *testing.T) {
	c := newTestController(t, t.TempDir())
	res := newCtrlRecorder()
	code, err := c.Status(res)
	if err != nil || code != http.StatusOK {
		t.Fatalf("status: %d %v", code, err)
	}
	if res.body.String() != "OK" {
		t.Fatalf("body: %q", res.body.String())
	}
	if ct := res.header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content-type: %q", ct)
	}
}

func TestStopCompileHandler(t *testing.T) {
	c := newTestController(t, t.TempDir())
	// No lock + not running -> silent nil -> 204.
	res := newCtrlRecorder()
	code, err := c.StopCompile(res, ProjectUser{ProjectID: "p1", UserID: "u1"})
	if err != nil || code != http.StatusNoContent {
		t.Fatalf("stop: %d %v", code, err)
	}
	// kill error -> (0, err).
	c.Manager.KillLatex = func(name string, cb func(error)) {
		cb(errors.New("kill failed"))
	}
	c.Manager.IsRunning = func(name string) bool { return true }
	if _, err := c.StopCompile(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"}); err == nil {
		t.Fatal("expected kill error")
	}
}

func TestClearCacheHandler(t *testing.T) {
	c := newTestController(t, t.TempDir())
	res := newCtrlRecorder()
	code, err := c.ClearCache(res, ProjectUser{ProjectID: "p1", UserID: "u1"})
	if err != nil || code != http.StatusNoContent {
		t.Fatalf("clear: %d %v", code, err)
	}
	// stop error is tagged "stop compile" (tag = tagErr -> OError.Message).
	c2 := newTestController(t, t.TempDir())
	c2.Manager.IsRunning = func(name string) bool { return true }
	c2.Manager.KillLatex = func(name string, cb func(error)) { cb(errors.New("x")) }
	if _, serr := c2.ClearCache(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"}); serr == nil || serr.Error() != "stop compile" {
		t.Fatalf("want tag 'stop compile', got %v", serr)
	}
	// clear chain error is tagged "clear project".
	c3 := newTestController(t, t.TempDir())
	c3.ClearProject = func(projectId, userId string) error { return errors.New("chain failed") }
	if _, serr := c3.ClearCache(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"}); serr == nil || serr.Error() != "clear project" {
		t.Fatalf("want tag 'clear project', got %v", serr)
	}
	// seam wired via New().
	cfg := ControllerConfig{ClearProject: func(projectId, userId string) error { return nil }}
	c4 := New(c.Manager, cfg)
	if c4.ClearProject == nil {
		t.Fatal("New should wire the ClearProject seam")
	}
	if _, cerr := c4.ClearCache(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"}); cerr != nil {
		t.Fatalf("wired seam: %v", cerr)
	}
}

// --- syncFromCode / syncFromPdf --------------------------------------------------

func TestSyncFromCodeHandler(t *testing.T) {
	c := newTestController(t, t.TempDir())
	if err := os.MkdirAll(filepath.Join(c.Manager.Paths.CompilesDir, "p1-u1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Manager.Paths.CompilesDir, "p1-u1", "output.synctex.gz"),
		[]byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	c.Manager.Runner = &ctrlFakeRunner{out: &commandrunner.RunOutput{
		Stdout: "SyncTeX result begin\nOutput:/o.pdf\nPage:1\nx:1\ny:2\nh:3\nv:4\nW:5\nH:6\nSyncTeX result end\n",
	}}
	res := newCtrlRecorder()
	code, err := c.SyncFromCode(res, ProjectUser{ProjectID: "p1", UserID: "u1"},
		SyncQuery{File: "main.tex", Line: 10, Column: 5})
	if err != nil || code != http.StatusOK {
		t.Fatalf("syncFromCode: %d %v", code, err)
	}
	var env map[string]any
	if jerr := json.Unmarshal(res.body.Bytes(), &env); jerr != nil {
		t.Fatalf("body: %v %s", jerr, res.body.String())
	}
	if pdf, _ := env["pdf"].([]any); pdf == nil {
		t.Fatalf("missing pdf positions: %s", res.body.String())
	}

	// error path propagates as (0, error).
	c.Manager.Runner = &ctrlFakeRunner{err: errors.New("synctex failed")}
	if _, err := c.SyncFromCode(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"},
		SyncQuery{File: "main.tex"}); err == nil {
		t.Fatal("expected sync error")
	}
}

func TestSyncFromPdfHandler(t *testing.T) {
	c := newTestController(t, t.TempDir())
	if err := os.MkdirAll(filepath.Join(c.Manager.Paths.CompilesDir, "p1-u1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Manager.Paths.CompilesDir, "p1-u1", "output.synctex.gz"),
		[]byte("synctex"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.Manager.Runner = &ctrlFakeRunner{out: &commandrunner.RunOutput{
		Stdout: "Output:/o.pdf\nInput:main.tex\nLine:10\nColumn:5\n",
	}}
	res := newCtrlRecorder()
	code, err := c.SyncFromPdf(res, ProjectUser{ProjectID: "p1", UserID: "u1"},
		SyncQuery{Page: 1, H: 100, V: 200})
	if err != nil || code != http.StatusOK {
		t.Fatalf("syncFromPdf: %d %v", code, err)
	}
	var env map[string]any
	if jerr := json.Unmarshal(res.body.Bytes(), &env); jerr != nil {
		t.Fatalf("body: %v %s", jerr, res.body.String())
	}

	// error path propagates as (0, error).
	c.Manager.Runner = &ctrlFakeRunner{err: errors.New("x")}
	if _, err := c.SyncFromPdf(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"},
		SyncQuery{Page: 1}); err == nil {
		t.Fatal("expected sync error")
	}
}

// --- wordcount -------------------------------------------------------------------

func TestWordcountHandler(t *testing.T) {
	c := newTestController(t, t.TempDir())
	c.Manager.Runner = &ctrlFakeRunner{out: &commandrunner.RunOutput{
		Stdout: "Encoding: utf-8\nWords in text: 3\n"}}
	res := newCtrlRecorder()
	code, err := c.Wordcount(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, "main.tex", "")
	if err != nil || code != http.StatusOK {
		t.Fatalf("wordcount: %d %v", code, err)
	}
	var env map[string]any
	if jerr := json.Unmarshal(res.body.Bytes(), &env); jerr != nil {
		t.Fatalf("body: %v %s", jerr, res.body.String())
	}
	if env["texcount"] == nil {
		t.Fatalf("missing texcount: %s", res.body.String())
	}

	// runner error -> (0, error).
	c.Manager.Runner = &ctrlFakeRunner{err: errors.New("texcount failed")}
	if _, err := c.Wordcount(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"},
		"main.tex", ""); err == nil {
		t.Fatal("expected wordcount error")
	}
}

// --- wordcountWithSync -----------------------------------------------------------

func TestWordcountWithSyncHandler(t *testing.T) {
	// (a) success: RW sync ok, wordcount -> 200 {texcount}.
	c := newTestController(t, t.TempDir())
	c.Manager.Runner = &ctrlFakeRunner{out: &commandrunner.RunOutput{
		Stdout: "Encoding: utf-8\nWords in text: 5\n"}}
	res := newCtrlRecorder()
	code, err := c.WordcountWithSync(res, ProjectUser{ProjectID: "p1", UserID: "u1"},
		"main.tex", "", cb(map[string]any{}))
	if err != nil || code != http.StatusOK {
		t.Fatalf("wordcountWithSync: %d %v", code, err)
	}
	_ = res

	// (b) MissingUpdates -> 409 {baseHistoryVersion}.
	c.Manager.ResourceSync = func(req *resourcewriter.Request, base string) ([]resourcewriter.Resource, error) {
		return nil, cerrors.NewMissingUpdatesError("MU", map[string]any{"baseHistoryVersion": 7})
	}
	resB := newCtrlRecorder()
	code, err = c.WordcountWithSync(resB, ProjectUser{ProjectID: "p1", UserID: "u1"},
		"main.tex", "", cb(map[string]any{}))
	if code != 409 || err != nil {
		t.Fatalf("want 409 got %d: %v", code, err)
	}
	var body map[string]any
	if jerr := json.Unmarshal(resB.body.Bytes(), &body); jerr != nil {
		t.Fatalf("409 body: %v %s", jerr, resB.body.String())
	}
	if v, ok := body["baseHistoryVersion"].(float64); !ok || int(v) != 7 {
		t.Fatalf("baseHistoryVersion: %v", body["baseHistoryVersion"])
	}

	// (c) AlreadyCompiling -> 423 plain.
	c.Manager.ResourceSync = func(req *resourcewriter.Request, base string) ([]resourcewriter.Resource, error) {
		return nil, cerrors.NewAlreadyCompilingError("busy")
	}
	if code, _ := c.WordcountWithSync(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"},
		"main.tex", "", cb(map[string]any{})); code != 423 {
		t.Fatalf("want 423, got %d", code)
	}

	// (d) TooMany -> 503 plain.
	c.Manager.ResourceSync = func(req *resourcewriter.Request, base string) ([]resourcewriter.Resource, error) {
		return nil, cerrors.NewTooManyCompileRequestsError("max")
	}
	if code, _ := c.WordcountWithSync(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"},
		"main.tex", "", cb(map[string]any{})); code != 503 {
		t.Fatalf("want 503, got %d", code)
	}

	// (e) other error -> (0, err).
	c.Manager.ResourceSync = func(req *resourcewriter.Request, base string) ([]resourcewriter.Resource, error) {
		return nil, errors.New("boom")
	}
	if code, err := c.WordcountWithSync(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"},
		"main.tex", "", cb(map[string]any{})); code != 0 || err == nil {
		t.Fatalf("want (0, err), got %d %v", code, err)
	}

	// (f) bad body (no compile key) -> parse error (0, err).
	if code, err := c.WordcountWithSync(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"},
		"main.tex", "", map[string]any{}); code != 0 || err == nil {
		t.Fatalf("want parse error, got %d %v", code, err)
	}
}

// --- New + TimeSinceLastSuccessfulCompile ---------------------------------------

func TestNewAndTimer(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	c := New(m, ControllerConfig{InstanceType: "t3a", Zone: "z1"})
	if c.Manager != m || c.Notify == nil || c.MarkProjectAccessed == nil {
		t.Fatal("New wiring incomplete")
	}
	// MarkProjectAccessed -> lastprojectaccess global map.
	c.MarkProjectAccessed("p-timer", 12345)
	if got := lastprojectaccess.GetLastProjectAccessTime("p-timer"); got != 12345 {
		t.Fatalf("last access: %d != 12345", got)
	}
	// TimeSinceLastSuccessfulCompile returns ms since the baseline.
	setLastSuccessfulCompile(1_000_000)
	if got := TimeSinceLastSuccessfulCompile(); got < 0 {
		t.Fatalf("time since last: %d", got)
	}
}

var _ = lockmanager.Lock{}
