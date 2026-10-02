package compilecontroller

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	commandrunner "ollitex/go/services/clsitypst/commandrunner"
	cerrors "ollitex/go/services/clsitypst/errors"
)

// --- status (Node: res.send('OK')) --------------------------------------------

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

// --- stopCompile (Node: stopCompile -> 204) -------------------------------------

func TestStopCompileHandler(t *testing.T) {
	c := newTestController(t, t.TempDir())
	// no lock + not running -> silent nil -> 204.
	res := newCtrlRecorder()
	code, err := c.StopCompile(res, ProjectUser{ProjectID: "p1", UserID: "u1"})
	if err != nil || code != http.StatusNoContent {
		t.Fatalf("stop: %d %v", code, err)
	}
	// kill error -> (0, err).
	c.Manager.KillTypst = func(name string, cb func(error)) { cb(errors.New("kill failed")) }
	c.Manager.IsRunning = func(name string) bool { return true }
	code, err = c.StopCompile(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"})
	if code != 0 || err == nil {
		t.Fatalf("kill err: want (0, err), got %d %v", code, err)
	}
	// lock-wait arm: GetLock returns a released lock -> WaitForRelease is
	// already closed; kill nil -> 204.
	// (No stateless release is possible: build a real lockmanager.Lock and
	// Release it after the call.)
}

// --- clearCache (Node: stopCompile -> clearProject -> 204) ------------------------

func TestClearCacheHandler(t *testing.T) {
	c := newTestController(t, t.TempDir())
	// real compile dir cleared via os.RemoveAll (lifecycle.go real impl).
	compileDir := filepath.Join(c.Manager.Paths.CompilesDir, "p1-u1")
	if err := os.MkdirAll(compileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(compileDir, "main.typ"), []byte("1+1"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := newCtrlRecorder()
	code, err := c.ClearCache(res, ProjectUser{ProjectID: "p1", UserID: "u1"})
	if err != nil || code != http.StatusNoContent {
		t.Fatalf("clear: %d %v", code, err)
	}
	if _, statErr := os.Stat(compileDir); !os.IsNotExist(statErr) {
		t.Fatalf("compile dir not removed: %v", statErr)
	}

	// stop error is tagged 'stop compile' (Go: cerrors.Tag -> OError.Message).
	c2 := newTestController(t, t.TempDir())
	c2.Manager.KillTypst = func(name string, cb func(error)) { cb(errors.New("boom")) }
	c2.Manager.IsRunning = func(name string) bool { return true }
	_, serr := c2.ClearCache(newCtrlRecorder(), ProjectUser{ProjectID: "p", UserID: "u"})
	if serr == nil {
		t.Fatal("want stop error")
	}
	var oerr *cerrors.OError
	if !errors.As(serr, &oerr) || oerr.Message != "stop compile" {
		t.Fatalf("want tag 'stop compile', got %v (%T)", serr, serr)
	}

	// --- tagErr: the 'clear project' tag over a plain error (OError.Message) --
	if te := tagErr(errors.New("x"), "clear project"); te == nil || te.Error() != "clear project" {
		t.Fatalf("tagErr: %v", te)
	}
	var clearOErr *cerrors.OError
	if !errors.As(tagErr(errors.New("y"), "clear project"), &clearOErr) {
		t.Fatalf("tagErr type: %v", tagErr(errors.New("y"), "clear project"))
	}
}

// --- wordcount (Node: GET + POST, plain next(error) passthrough — D2) ------------

func createTypstFile(t *testing.T, c *Controller, sub, name string) {
	t.Helper()
	dir := filepath.Join(c.Manager.Paths.CompilesDir, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("1 + 1"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWordcountHandlerGet(t *testing.T) {
	c := newTestController(t, t.TempDir())
	// GET (body == nil): counts the last synced sources via the Runner
	// (the wordometer seams are faked-nil in newTestManager -> fallback).
	createTypstFile(t, c, "p1-user1", "main.typ")
	c.Manager.InjectWordometer = nil
	c.Manager.RemoveArtifacts = nil
	c.Manager.ReadPdfMarker = nil
	c.Manager.Runner = &ctrlFakeRunner{out: &commandrunner.RunOutput{
		Stdout: "42 /compile/main.typ",
	}}
	res := newCtrlRecorder()
	code, err := c.Wordcount(res, ProjectUser{ProjectID: "p1", UserID: "user1"}, "main.typ", "", nil)
	if err != nil || code != http.StatusOK {
		t.Fatalf("wordcount: %d %v", code, err)
	}
	var env map[string]any
	if jerr := json.Unmarshal(res.body.Bytes(), &env); jerr != nil {
		t.Fatalf("body: %v %s", jerr, res.body.String())
	}
	tex, ok := env["texcount"].(map[string]any)
	if !ok {
		t.Fatalf("missing texcount: %s", res.body.String())
	}
	if words, ok := tex["textWords"].(float64); !ok || int(words) != 42 {
		t.Fatalf("textWords: %v (body %s)", tex["textWords"], res.body.String())
	}
	if enc, _ := tex["encode"].(string); enc != "utf-8" {
		t.Fatalf("encode: %v", tex["encode"])
	}
	if ct := res.header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content-type: %q", ct)
	}
}

func TestWordcountHandlerWordometer(t *testing.T) {
	// wordometer path: inject -> typst compile (Runner OK) -> ReadPdfMarker
	// ok=true -> returns the marker; wcFallback is never reached.
	c := newTestController(t, t.TempDir())
	createTypstFile(t, c, "p1-u1", "main.typ")
	c.Manager.InjectWordometer = func(compileDir, root string) error { return nil }
	c.Manager.RemoveArtifacts = func(compileDir string) error { return nil }
	c.Manager.ReadPdfMarker = func(pdfPath string) (int, int, int, bool) { return 5, 2, 3, true }
	c.Manager.Runner = &ctrlFakeRunner{out: &commandrunner.RunOutput{Stdout: "ok"}}
	res := newCtrlRecorder()
	code, err := c.Wordcount(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, "main.typ", "", nil)
	if err != nil || code != http.StatusOK {
		t.Fatalf("wordometer wordcount: %d %v", code, err)
	}
	var env map[string]any
	if jerr := json.Unmarshal(res.body.Bytes(), &env); jerr != nil {
		t.Fatalf("body: %v", jerr)
	}
	tex, ok := env["texcount"].(map[string]any)
	if !ok {
		t.Fatalf("texcount: %s", res.body.String())
	}
	if tw, ok := tex["textWords"].(float64); !ok || int(tw) != 5 {
		t.Fatalf("textWords (wordometer marker): %v", tex["textWords"])
	}
	if hw, ok := tex["headWords"].(float64); !ok || int(hw) != 2 {
		t.Fatalf("headWords: %v", tex["headWords"])
	}
}

func TestWordcountHandlerPost(t *testing.T) {
	// POST: the body is parsed through the requestparser and carried as the
	// *Request (the compile path with a body, D2 plain passthrough).
	c := newTestController(t, t.TempDir())
	createTypstFile(t, c, "p1-u1", "main.typ")
	c.Manager.InjectWordometer = nil
	c.Manager.RemoveArtifacts = nil
	c.Manager.ReadPdfMarker = nil
	c.Manager.Runner = &ctrlFakeRunner{out: &commandrunner.RunOutput{
		Stdout: "7 /compile/custom.typ",
	}}
	createTypstFile(t, c, "p1-u1", "custom.typ")
	res := newCtrlRecorder()
	body := map[string]any{"compile": map[string]any{"options": map[string]any{}}}
	code, err := c.Wordcount(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, "custom.typ", "", body)
	if err != nil || code != http.StatusOK {
		t.Fatalf("wordcount POST: %d %v", code, err)
	}
	var env map[string]any
	if jerr := json.Unmarshal(res.body.Bytes(), &env); jerr != nil {
		t.Fatalf("body: %v %s", jerr, res.body.String())
	}
	if _, ok := env["texcount"].(map[string]any); !ok {
		t.Fatalf("missing texcount: %s", res.body.String())
	}
}

func TestWordcountHandlerNotFound(t *testing.T) {
	// root resource not synced -> (0, NotFoundError) (server maps 404).
	c := newTestController(t, t.TempDir())
	_, err := c.Wordcount(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"},
		"missing.typ", "", nil)
	if code, cerr := 0, err; code != 0 || cerr == nil {
		t.Fatalf("want (0, err), got %d %v", code, cerr)
	}
	var nf *cerrors.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want NotFoundError, got %v", err)
	}
}

func TestWordcountHandlerRunnerError(t *testing.T) {
	c := newTestController(t, t.TempDir())
	createTypstFile(t, c, "p1-u1", "main.typ")
	c.Manager.InjectWordometer = nil
	c.Manager.RemoveArtifacts = nil
	c.Manager.ReadPdfMarker = nil
	c.Manager.Runner = &ctrlFakeRunner{err: errors.New("wc failed")}
	if code, err := c.Wordcount(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"},
		"main.typ", "", nil); code != 0 || err == nil {
		t.Fatalf("want (0, err), got %d %v", code, err)
	}
}

func TestWordcountHandlerParseError(t *testing.T) {
	// bad POST body -> (0, parse error) (the Node next(error) surface).
	c := newTestController(t, t.TempDir())
	if code, err := c.Wordcount(newCtrlRecorder(), ProjectUser{ProjectID: "p1", UserID: "u1"},
		"main.typ", "", map[string]any{"bogus": 1}); code != 0 || err == nil {
		t.Fatalf("want (0, parse err), got %d %v", code, err)
	}
}
