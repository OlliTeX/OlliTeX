// apps_test.go covers the T10 server layer: the 17-route mux, the
// COMPILE_TYPEST_ENABLED gate (501), the readJSON contract (413 / invalid
// JSON), the finish error-middleware arms (LOCKED text), and the route
// dispatch into the compilecontroller (compile / stop / clear / wordcount /
// status / health / smoke / output.zip).
//
// Fixtures mirror compilecontroller/control_test.go: a compilemanager
// Manager built from a full struct literal with every typst seam faked
// (clsi.go test convention; the REAL lockmanager touches clsi/config).
package apps

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	commandrunner "ollitex/go/services/clsitypst/commandrunner"
	clserrors "ollitex/go/services/clsitypst/errors"
	"ollitex/go/services/clsitypst/lockmanager"
	offa "ollitex/go/services/clsitypst/outputfilearchivemanager"
	offf "ollitex/go/services/clsitypst/outputfilefinder"
	"ollitex/go/services/clsitypst/resourcewriter"

	compilecontroller "ollitex/go/services/clsitypst/compilecontroller"
	compilemanager "ollitex/go/services/clsitypst/compilemanager"
	cltypstcfg "ollitex/go/services/clsitypst/config"
	"ollitex/go/services/clsitypst/smoketest"
	"ollitex/go/services/clsitypst/typstrunner"
)

// --- fixtures (clsi.go control_test recipe) --------------------------------------

// fakeRunner implements clsi/commandrunner with a fixed (err, out).
type fakeRunner struct {
	err    error
	out    *commandrunner.RunOutput
	groups []string // the compileGroup of each Run (asserts the wc fallback group)
}

func (r *fakeRunner) Run(projectID string, command []string, directory, image string,
	timeout int64, environment map[string]string, compileGroup string, cwd string,
	callback func(err error, out *commandrunner.RunOutput)) string {
	r.groups = append(r.groups, compileGroup)
	if r.err != nil {
		callback(r.err, nil)
	} else {
		callback(nil, r.out)
	}
	return ""
}
func (r *fakeRunner) Kill(containerID string, callback func(err error)) {}
func (r *fakeRunner) CanRunSyncTeXInOutputDir() bool                    { return false }

// newTestManager copies the compilecontroller newTestManager recipe (faked
// typst seams; Paths point at temp trees).
func newTestManager(t *testing.T, tmp string) *compilemanager.Manager {
	t.Helper()
	m := &compilemanager.Manager{
		Paths: compilemanager.Paths{
			CompilesDir: tmp + "/compiles",
			OutputDir:   tmp + "/output",
		},
		DockerEnv: map[string]string{"HOME": "/tmp"},
		Runner:    &fakeRunner{out: &commandrunner.RunOutput{Stdout: "42 /compile/main.typ", ExitCode: 0}},
		Now:       func() int64 { return 42 },
		MkdirAll:  func(dir string) (bool, error) { return false, nil },
		Acquire: func(key string) (*lockmanager.Lock, error) {
			return &lockmanager.Lock{Key: key}, nil
		},
		GetLock:           func(key string) *lockmanager.Lock { return nil },
		PrepareCompileDir: func(compileDir string) {},
		FindOutputFiles: func(resources []offf.Resource, directory string) (offf.FindResult, error) {
			sz := int64(13)
			return offf.FindResult{
				OutputFiles: []offf.OutputFile{{Path: "output.pdf", Type: "pdf", Size: &sz, Build: "b1"}},
				AllEntries:  []string{"output.pdf"},
			}, nil
		},
		SaveOutputFiles: func(req compilemanager.SaveOutputReq, rawFiles []offf.OutputFile,
			compileDir, outputDir string, stats map[string]float64,
			timings map[string]float64) (string, []offf.OutputFile, error) {
			return "b1", rawFiles, nil
		},
		ResourceSync: func(req *resourcewriter.Request, basePath string) ([]resourcewriter.Resource, error) {
			return []resourcewriter.Resource{{Path: "main.typ", Content: []byte("x")}}, nil
		},
		DownloadLatestCompileCache: func(projectID, userID, compileDir string) (bool, error) {
			return false, nil
		},
		RunTypst: func(compileName string, opts typstrunner.Options, cb func(err error, out *commandrunner.RunOutput)) {
			cb(nil, &commandrunner.RunOutput{ExitCode: 0})
		},
		IsRunning:        func(name string) bool { return false },
		KillTypst:        func(name string, cb func(err error)) { cb(nil) },
		InjectWordometer: func(compileDir, rootResourcePath string) error { return nil },
		RemoveArtifacts:  func(compileDir string) error { return nil },
		ReadPdfMarker: func(pdfPath string) (total, headingWords, numHeadings int, ok bool) {
			return 0, 0, 0, false
		},
	}
	return m
}

func newTestApp(t *testing.T) (*App, *compilemanager.Manager) {
	t.Helper()
	tmp := t.TempDir()
	m := newTestManager(t, tmp)
	cfg := &cltypstcfg.Config{}
	cfg.CompileTypstEnabled = true
	cfg.CompileSizeLimit = "7mb"
	cfg.Path.OutputDir = tmp + "/output"
	ctrl := &compilecontroller.Controller{
		Manager: m,
		Config: compilecontroller.ControllerConfig{
			InstanceType:            "test-instance",
			Zone:                    "zone-a",
			IsSpotInstance:          true,
			OutputURLPrefix:         "http://outputs.test",
			DownloadHost:            "http://download.test",
			AllowedImages:           []string{"pandoc/typst:latest-alpine"},
			AllowedCompileGroups:    []string{"standard"},
			AllowedCompileGroupsSet: true,
			PdfCachingMinChunk:      1024,
		},
	}
	a := &App{
		Config:      cfg,
		CompileCtrl: ctrl,
		// ProcessTooOld defaults to the OK arm (New's production default).
		ProcessTooOld: func() bool { return false },
		diskLow:       func() bool { return false },
		diskCritical:  func() bool { return false },
	}
	return a, m
}

func doGet(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func doPost(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
	return rec
}

// --- New (production wiring) ------------------------------------------------------

func TestNewWiring(t *testing.T) {
	cfg := &cltypstcfg.Config{}
	cfg.CompileTypstEnabled = true
	a := New(cfg, nil, nil)
	if a.Config != cfg {
		t.Fatal("config not wired")
	}
	if a.ProcessTooOld == nil || a.ProcessTooOld() {
		t.Fatal("ProcessTooOld must default to the OK arm")
	}
	if a.diskLow == nil || a.diskCritical == nil {
		t.Fatal("disk seams must be the PPM singletons")
	}
}

// --- Gate (D14: 501 on ALL main-mux routes) -----------------------------------------

func TestGateOffAllRoutes(t *testing.T) {
	cfg := &cltypstcfg.Config{}
	cfg.CompileTypstEnabled = false
	a := &App{Config: cfg}
	h := a.Handler()

	want := `{"error":"typst compilation not enabled"}`
	cases := []struct {
		method, path string
	}{
		{"POST", "/project/p1/compile"},
		{"POST", "/project/p1/compile/stop"},
		{"DELETE", "/project/p1"},
		{"GET", "/project/p1/wordcount"},
		{"POST", "/project/p1/wordcount"},
		{"GET", "/project/p1/status"},
		{"POST", "/project/p1/status"},
		{"POST", "/project/p1/user/u1/compile"},
		{"POST", "/project/p1/user/u1/compile/stop"},
		{"DELETE", "/project/p1/user/u1"},
		{"GET", "/project/p1/user/u1/wordcount"},
		{"POST", "/project/p1/user/u1/wordcount"},
		{"GET", "/project/p1/build/b1/output/output.zip"},
		{"GET", "/project/p1/user/u1/build/b1/output/output.zip"},
		{"GET", "/status"},
		{"GET", "/health_check"},
		{"GET", "/smoke_test_force"},
		// D21 sync routes (T16): the 4 new GET routes are main-mux (the gate
		// wraps them like every other route).
		{"GET", "/project/p1/sync/code?file=main.typ"},
		{"GET", "/project/p1/sync/pdf?page=1"},
		{"GET", "/project/p1/user/u1/sync/code?file=main.typ"},
		{"GET", "/project/p1/user/u1/sync/pdf?page=1"},
	}
	for _, c := range cases {
		var rec *httptest.ResponseRecorder
		switch c.method {
		case "GET":
			rec = doGet(t, h, c.path)
		case "DELETE":
			rec = doGet(t, h, c.path)
		default:
			rec = doPost(t, h, c.path, "{}")
		}
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("%s %s: want 501 got %d (body %s)", c.method, c.path, rec.Code, rec.Body.String())
		}
		if body := rec.Body.String(); body != want {
			t.Fatalf("%s %s: gate body %q", c.method, c.path, body)
		}
	}
}

// --- Routes ---------------------------------------------------------------------------

func TestCompileRouteSuccess(t *testing.T) {
	a, _ := newTestApp(t)
	h := a.Handler()
	for _, path := range []string{
		"/project/p1/compile",
		"/project/p1/user/u1/compile",
	} {
		rec := doPost(t, h, path, `{"compile":{"options":{"buildId":"abc1-def2"}}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		var env struct {
			Compile struct {
				Status          string `json:"status"`
				BuildID         string `json:"buildId"`
				OutputURL       string `json:"outputUrl"`
				OutputURLPrefix string `json:"outputUrlPrefix"`
			} `json:"compile"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("%s: envelope: %v", path, err)
		}
		if env.Compile.Status != "success" {
			t.Fatalf("%s: status %q (body %s)", path, env.Compile.Status, rec.Body.String())
		}
		if env.Compile.BuildID != "b1" {
			t.Fatalf("%s: buildId %q", path, env.Compile.BuildID)
		}
	}
}

func TestCompileRouteParseError(t *testing.T) {
	// compile present but not an object -> the requestparser error is NOT
	// an InvalidParameter (ParseError) -> finish else-arm: HTTP 500 + the
	// LOCKED body text.
	a, _ := newTestApp(t)
	rec := doPost(t, a.Handler(), "/project/p1/compile", `{"compile":"not-an-object"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "CLSI server error (internal error: top level object should have a compile attribute)") {
		t.Fatalf("else-arm text: %q", rec.Body.String())
	}
}

func TestCompileRouteEmptyBody(t *testing.T) {
	// Empty body forwards an empty map: the parser rejects the missing
	// 'compile' attribute through finish (LOCKED 500 text).
	a, _ := newTestApp(t)
	rec := doPost(t, a.Handler(), "/project/p1/compile", ``)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "top level object should have a compile attribute") {
		t.Fatalf("parse error text: %q", rec.Body.String())
	}
}

func TestCompileStopRoute204(t *testing.T) {
	a, _ := newTestApp(t)
	h := a.Handler()
	for _, path := range []string{
		"/project/p1/compile/stop",
		"/project/p1/user/u1/compile/stop",
	} {
		rec := doPost(t, h, path, "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: want 204 got %d: %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestClearCacheRoute204(t *testing.T) {
	a, m := newTestApp(t)
	// ClearCache routes through Controller.ClearProject (a method value of
	// the raw Manager here: it rm's the compile dir — harmless under tmp).
	h := a.Handler()
	for _, path := range []string{"/project/p1", "/project/p1/user/u1"} {
		req := httptest.NewRequest("DELETE", path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: want 204 got %d: %s", path, rec.Code, rec.Body.String())
		}
	}
	// The manager must NOT have run a second stop-compile via a fake
	// runner kill (KillTypst cb nil => nothing to assert beyond 204s).
	_ = m
}

func TestWordcountRouteGET(t *testing.T) {
	a, m := newTestApp(t)
	// Seed the compile dir so os.Stat (the real fs check in
	// compilemanager.Wordcount) sees main.typ (file defaults to main.typ).
	root := filepath.Join(m.Paths.CompilesDir, "p1")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.typ"), []byte("hello"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rec := doGet(t, a.Handler(), "/project/p1/wordcount")
	if rec.Code != http.StatusOK {
		t.Fatalf("wordcount GET: want 200 got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Texcount struct {
			Encode    string `json:"encode"`
			TextWords int    `json:"textWords"`
		} `json:"texcount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("wordcount GET body: %v", err)
	}
	if body.Texcount.Encode != "utf-8" || body.Texcount.TextWords != 42 {
		t.Fatalf("wordcount fallback: %+v (want wc -w textWords=42)", body.Texcount)
	}
}

func TestWordcountRouteQueryFile(t *testing.T) {
	a, m := newTestApp(t)
	root := filepath.Join(m.Paths.CompilesDir, "p1")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "other.typ"), []byte("hi"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec := doGet(t, a.Handler(), "/project/p1/wordcount?file=other.typ")
	if rec.Code != http.StatusOK {
		t.Fatalf("wordcount file=other.typ: %d %s", rec.Code, rec.Body.String())
	}
}

func TestWordcountRouteGETNotFound(t *testing.T) {
	a, _ := newTestApp(t)
	rec := doGet(t, a.Handler(), "/project/missing/wordcount")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("404 body must be empty: %q", rec.Body.String())
	}
}

func TestWordcountRoutePOST(t *testing.T) {
	a, m := newTestApp(t)
	root := filepath.Join(m.Paths.CompilesDir, "p1-u1")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.typ"), []byte("hi"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec := doPost(t, a.Handler(), "/project/p1/user/u1/wordcount", `{"compile":{"options":{}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("wordcount POST: want 200 got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Texcount struct {
			TextWords int `json:"textWords"`
		} `json:"texcount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("wordcount POST body: %v", err)
	}
	if body.Texcount.TextWords != 42 {
		t.Fatalf("wordcount POST: %+v", body.Texcount)
	}
}

func TestProjectStatusRoute(t *testing.T) {
	a, _ := newTestApp(t)
	h := a.Handler()
	for _, method := range []string{"GET", "POST"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/project/p1/status", nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "OK" {
			t.Fatalf("status %s: %d %q", method, rec.Code, rec.Body.String())
		}
	}
}

func TestAliveStatus(t *testing.T) {
	a, _ := newTestApp(t)
	rec := doGet(t, a.Handler(), "/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if body := rec.Body.String(); body != "clsi_typst is alive\n" {
		t.Fatalf("status body: %q", body)
	}
	if ct := rec.Result().Header.Get("Content-Type"); ct == "" {
		t.Fatal("status: missing Content-Type")
	}
}

// --- readJSON (LOCKED) --------------------------------------------------------------

func TestReadJSONValid(t *testing.T) {
	a, _ := newTestApp(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/project/p1/compile", strings.NewReader(`{"a":1}`))
	body, ok := a.readJSON(rec, req)
	if !ok || len(body) != 1 {
		t.Fatalf("readJSON: %#v ok=%v", body, ok)
	}
}

func TestReadJSONOverLimit(t *testing.T) {
	a, _ := newTestApp(t)
	a.Config.CompileSizeLimit = "10b"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/project/p1/compile",
		strings.NewReader("0123456789X")) // 11 bytes > 10
	if _, ok := a.readJSON(rec, req); ok {
		t.Fatal("over-limit must reject")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413 got %d", rec.Code)
	}
}

func TestReadJSONAtLimit(t *testing.T) {
	a, _ := newTestApp(t)
	a.Config.CompileSizeLimit = "3b"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/project/p1/compile",
		strings.NewReader(`{}`)) // exactly 3 bytes <= limit
	body, ok := a.readJSON(rec, req)
	if !ok || len(body) != 0 {
		t.Fatalf("at-limit must pass: %#v ok=%v", body, ok)
	}
}

func TestReadJSONNoCapRejectsNonEmpty(t *testing.T) {
	// CompileSizeLimit misconfigured (token -> 0 = no cap, Node parseInt
	// fail): a non-empty body IS the misconfiguration symptom -> 413.
	a, _ := newTestApp(t)
	a.Config.CompileSizeLimit = ""
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/project/p1/compile", strings.NewReader(`{"a":1}`))
	if _, ok := a.readJSON(rec, req); ok {
		t.Fatal("no-cap must reject non-empty body")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413 got %d", rec.Code)
	}
}

func TestReadJSONNoCapEmptyOK(t *testing.T) {
	a, _ := newTestApp(t)
	a.Config.CompileSizeLimit = ""
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/project/p1/compile", nil)
	body, ok := a.readJSON(rec, req)
	if !ok || len(body) != 0 {
		t.Fatalf("no-cap empty body must forward: %#v ok=%v", body, ok)
	}
}

func TestReadJSONInvalid(t *testing.T) {
	a, _ := newTestApp(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/project/p1/compile", strings.NewReader(`{broken`))
	if _, ok := a.readJSON(rec, req); ok {
		t.Fatal("invalid JSON must reject")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Bad request: (invalid compile request body") {
		t.Fatalf("invalid text: %q", rec.Body.String())
	}
}

// readErrorIO implements io.Reader that errors.
type readErrorIO struct{}

func (readErrorIO) Read([]byte) (int, error) { return 0, fmt.Errorf("body read failed") }
func (readErrorIO) Close() error             { return nil }

func TestReadJSONReadError(t *testing.T) {
	a, _ := newTestApp(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/project/p1/compile", nil)
	req.Body = readErrorIO{}
	if _, ok := a.readJSON(rec, req); ok {
		t.Fatal("read error must reject")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Bad request: (read body: body read failed)") {
		t.Fatalf("read error text: %q", rec.Body.String())
	}
}

// --- finish (LOCKED arms) -----------------------------------------------------------

func reqFor(path string) *http.Request {
	return httptest.NewRequest("GET", path, nil)
}

func TestFinishNotFound(t *testing.T) {
	a, _ := newTestApp(t)
	rec := httptest.NewRecorder()
	a.finish(rec, reqFor("/x"), 0, clserrors.NewNotFoundError("gone"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("404 must be empty: %q", rec.Body.String())
	}
}

func TestFinishInvalidParameter(t *testing.T) {
	a, _ := newTestApp(t)
	rec := httptest.NewRecorder()
	a.finish(rec, reqFor("/x"), 0, &clserrors.InvalidParameter{Message: "bad param"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Bad request: (bad param)") {
		t.Fatalf("text: %q", rec.Body.String())
	}
}

func TestFinishEPipe(t *testing.T) {
	a, _ := newTestApp(t)
	rec := httptest.NewRecorder()
	a.finish(rec, reqFor("/x"), 0, fmt.Errorf("EPIPE: broken pipe"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 got %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("epipe must be empty: %q", rec.Body.String())
	}
}

func TestFinishElse(t *testing.T) {
	a, _ := newTestApp(t)
	rec := httptest.NewRecorder()
	a.finish(rec, reqFor("/x"), 0, fmt.Errorf("boom"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "CLSI server error (internal error: boom)") {
		t.Fatalf("text: %q", rec.Body.String())
	}
}

func TestFinishPartialWrite(t *testing.T) {
	a, _ := newTestApp(t)
	rec := httptest.NewRecorder()
	rec.Body.WriteString("partial")
	a.finish(rec, reqFor("/x"), http.StatusInternalServerError, fmt.Errorf("boom"))
	// The partial-write arm must not append to the body.
	if rec.Body.Len() != len("partial") {
		t.Fatalf("partial arm wrote: %q", rec.Body.String())
	}
}

func TestFinishNilError(t *testing.T) {
	a, _ := newTestApp(t)
	rec := httptest.NewRecorder()
	a.finish(rec, reqFor("/x"), 0, nil)
	// finish renders nothing on nil err (the recorder keeps its default
	// Code 200 — assert only that no body was written).
	if rec.Body.Len() != 0 {
		t.Fatalf("nil err must write no body: %q", rec.Body.String())
	}
}

// htRecorder is a throwaway recorder for rendering that is not asserted.
func htRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}

// --- handle contract ------------------------------------------------------------------

func TestHandleAlreadyRendered(t *testing.T) {
	a, _ := newTestApp(t)
	rec := httptest.NewRecorder()
	rec.Body.WriteString("body-already-on-socket")
	a.handle(rec, reqFor("/x"), func(http.ResponseWriter) (int, error) {
		return http.StatusOK, nil
	})
	if rec.Body.Len() != len("body-already-on-socket") {
		t.Fatalf("renderer-only arm must no-op: %q", rec.Body.String())
	}
}

func TestHandlePartialWriteLogOnly(t *testing.T) {
	a, _ := newTestApp(t)
	rec := httptest.NewRecorder()
	rec.Body.WriteString("partial")
	a.handle(rec, reqFor("/x"), func(http.ResponseWriter) (int, error) {
		return http.StatusBadRequest, fmt.Errorf("write failed after body")
	})
	if rec.Body.Len() != len("partial") {
		t.Fatalf("partial arm must log-only: %q", rec.Body.String())
	}
}

// --- health_check (LOCKED order) ----------------------------------------------------

func TestHealthCheckOK(t *testing.T) {
	a, _ := newTestApp(t)
	rec := doGet(t, a.Handler(), "/health_check")
	if rec.Code != http.StatusOK {
		t.Fatalf("health ok: %d", rec.Code)
	}
	var body map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("health body: %v", err)
	}
	if !body["ok"] {
		t.Fatalf("health: %v", body)
	}
}

func TestHealthCheckProcessTooOld(t *testing.T) {
	a, _ := newTestApp(t)
	a.ProcessTooOld = func() bool { return true }
	rec := doGet(t, a.Handler(), "/health_check")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("too-old arm: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"processTooOld":true`) {
		t.Fatalf("too-old body: %s", rec.Body.String())
	}
}

func TestHealthCheckDiskCritical(t *testing.T) {
	a, _ := newTestApp(t)
	a.diskCritical = func() bool { return true }
	rec := doGet(t, a.Handler(), "/health_check")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("disk arm: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"diskCritical":true`) {
		t.Fatalf("disk body: %s", rec.Body.String())
	}
}

func TestHealthCheckSmokePending(t *testing.T) {
	a, _ := newTestApp(t)
	a.Config.SmokeTest = true
	a.SmokeTest = smoketest.New(&cltypstcfg.Config{})
	rec := doGet(t, a.Handler(), "/health_check")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("smoke pending: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "SmokeTestsPending") {
		t.Fatalf("smoke pending body: %s", rec.Body.String())
	}
}

func TestHealthCheckSmokeOK(t *testing.T) {
	a, _ := newTestApp(t)
	a.Config.SmokeTest = true
	st := newSmokeSuccess(t)
	// Prime the cache: production inits (T11) TriggerRun a live compile at
	// boot; /health_check renders the CACHED result via SendLastResult.
	st.SendNewResult(htRecorder())
	a.SmokeTest = st
	rec := doGet(t, a.Handler(), "/health_check")
	if rec.Code != http.StatusOK || rec.Body.String() != "OK" {
		t.Fatalf("smoke ok: %d %s", rec.Code, rec.Body.String())
	}
}

func TestHealthCheckSmokeOff(t *testing.T) {
	a, _ := newTestApp(t)
	a.Config.SmokeTest = false
	a.SmokeTest = newSmokeSuccess(t) // present but SmokeTest=false
	rec := doGet(t, a.Handler(), "/health_check")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("smoke off: %d %s", rec.Code, rec.Body.String())
	}
}

// --- smoke_test_force ----------------------------------------------------------------

func TestSmokeTestForceNil(t *testing.T) {
	a, _ := newTestApp(t)
	rec := doGet(t, a.Handler(), "/smoke_test_force")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("nil smoke: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "CLSI server error (internal error: smoke test not enabled)") {
		t.Fatalf("nil smoke text: %q", rec.Body.String())
	}
}

func TestSmokeTestForceSuccess(t *testing.T) {
	a, _ := newTestApp(t)
	a.SmokeTest = newSmokeSuccess(t)
	rec := doGet(t, a.Handler(), "/smoke_test_force")
	if rec.Code != http.StatusOK || rec.Body.String() != "OK" {
		t.Fatalf("force: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSmokeTestForceFailure(t *testing.T) {
	a, _ := newTestApp(t)
	a.SmokeTest = newSmokeFail(t)
	rec := doGet(t, a.Handler(), "/smoke_test_force")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("force fail: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "boom-fake-error") {
		t.Fatalf("force fail body: %s", rec.Body.String())
	}
}

// smokeSuccess / smokeFail return fresh *smoketest.SmokeTest values that
// send their fixed last result without a live compile (SetLastError is not
// exposed -> drive through an httptest endpoint the smoke talks to).
func newSmokeSuccess(t *testing.T) *smoketest.SmokeTest {
	t.Helper()
	// Point at an httptest server that returns the success envelope, so
	// Run() exercises the real validation path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := `{"compile":{"status":"success","outputFiles":[{"path":"output.pdf","type":"pdf"}]}}`
		_, _ = w.Write([]byte(data))
	}))
	t.Cleanup(srv.Close)
	st := smoketest.New(&cltypstcfg.Config{})
	st.SetCompileURL(srv.URL + "/project/smoket/compile")
	st.SetTimeout(500 * 1e6) // 0.5s (no cold-pull here)
	return st
}

func newSmokeFail(t *testing.T) *smoketest.SmokeTest {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom-fake-error", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	st := smoketest.New(&cltypstcfg.Config{})
	st.SetCompileURL(srv.URL + "/project/smoket/compile")
	st.SetTimeout(500 * 1e6)
	return st
}

func TestSmokeTestForceRunSuccess(t *testing.T) {
	a, _ := newTestApp(t)
	// The force route runs a LIVE compile through Run() (background not
	// used: SendNewResult is synchronous).
	a.SmokeTest = newSmokeSuccess(t)
	rec := doGet(t, a.Handler(), "/smoke_test_force")
	// The live Run posts to the httptest endpoint: 200 "OK".
	if rec.Code != http.StatusOK || rec.Body.String() != "OK" {
		t.Fatalf("force live: %d %s", rec.Code, rec.Body.String())
	}
}

// --- output.zip (frozen otc wiring) ---------------------------------------------------

// assignFind installs the ofa.Find seam for the duration of t (restored to
// nil on cleanup).
func assignFind(t *testing.T, f offa.FindFunc) {
	t.Helper()
	offa.AssignFind(f)
	t.Cleanup(func() { offa.AssignFind(nil) })
}

// seedOutputDir seeds the frozen READER contract (ofa.GetContentDir): the
// build dir + a real file (mirrors clsi.go outputcontroller_test.seedOutputDir).
func seedOutputDir(t *testing.T, outputDir, pid, uid, build string) {
	t.Helper()
	contentDir := offa.GetContentDir(outputDir, pid, uid)
	if err := os.MkdirAll(filepath.Join(contentDir, build), 0755); err != nil {
		t.Fatalf("seed mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, build, "b.pdf"), []byte("PDFDATA"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
}

func TestOutputZipHappy(t *testing.T) {
	a, _ := newTestApp(t)
	outputDir := t.TempDir()
	a.Config.Path.OutputDir = outputDir
	seedOutputDir(t, outputDir, "p1", "", "b1")
	// Find returns the archived file (relative to contentDir).
	assignFind(t, func(args []string, dir string) ([]offa.OutputFile, error) {
		return []offa.OutputFile{{Path: "b1/b.pdf"}}, nil
	})

	rec := doGet(t, a.Handler(), "/project/p1/build/b1/output/output.zip")
	if rec.Code != http.StatusOK {
		t.Fatalf("zip: %d %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Result().Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("zip content-type: %q", ct)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("zip body not a zip: %v", err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != "b1/b.pdf" {
		t.Fatalf("zip entries: want b1/b.pdf got %d entries", len(zr.File))
	}
}

func TestOutputZipPerUserRoute(t *testing.T) {
	a, _ := newTestApp(t)
	outputDir := t.TempDir()
	a.Config.Path.OutputDir = outputDir
	seedOutputDir(t, outputDir, "p1", "u1", "b1")
	assignFind(t, func(args []string, dir string) ([]offa.OutputFile, error) {
		return []offa.OutputFile{{Path: "b1/b.pdf"}}, nil
	})
	rec := doGet(t, a.Handler(), "/project/p1/user/u1/build/b1/output/output.zip")
	if rec.Code != http.StatusOK {
		t.Fatalf("per-user zip: %d %s", rec.Code, rec.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("entries: %d", len(zr.File))
	}
}

func TestOutputZip404NotFound(t *testing.T) {
	a, _ := newTestApp(t)
	a.Config.Path.OutputDir = t.TempDir()
	// Find surfaces a path-not-found -> CreateOutputZip (404, nil) -> render.
	assignFind(t, func(args []string, dir string) ([]offa.OutputFile, error) {
		return nil, &os.PathError{Op: "open", Path: dir, Err: os.ErrNotExist}
	})
	rec := doGet(t, a.Handler(), "/project/p1/build/b1/output/output.zip")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOutputZipDefaultFindNotInitialised(t *testing.T) {
	// ofa.Find nil (default) -> (500, "Find not initialised") -> finish
	// else-arm (LOCKED 500 text), no partial zip.
	a, _ := newTestApp(t)
	a.Config.Path.OutputDir = t.TempDir()
	assignFind(t, nil)

	rec := doGet(t, a.Handler(), "/project/p1/build/b1/output/output.zip")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "CLSI server error (internal error:") {
		t.Fatalf("find-not-initialised text: %q", rec.Body.String())
	}
}

// --- compileSizeBytes (LOCKED parse) --------------------------------------------------

func TestCompileSizeBytes(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"7mb", 7_000_000},
		{"1kb", 1_000},
		{"10b", 10},
		{"1k", 1_024},
		{"2m", 2_000_000},
		{"3GB", 3_000_000_000},
		{"5Gb", 5_000_000_000},
		{"9", 9},
		{"", 0},
		{"junk", 0},
		{"1.5mb", 0},
	}
	for _, c := range cases {
		if got := compileSizeBytes(c.in); got != c.want {
			t.Fatalf("compileSizeBytes(%q) = %d want %d", c.in, got, c.want)
		}
	}
}

// --- EPIPE arm (route level, Node parity) --------------------------------------------
//
// A route that returns (503, EPIPE) is a partial write (code != 0 + err)
// which finish logs only (the LOCKED divergence: Node would refire). The
// EPIPE arm of finish itself is exercised by TestFinishEPipe (the
// (0, EPIPE) case).

func TestEPipeRender(t *testing.T) {
	a, _ := newTestApp(t)
	rec := httptest.NewRecorder()
	a.finish(rec, reqFor("/x"), 0, fmt.Errorf("connect: EPIPE (shutdown)"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("epipe: %d", rec.Code)
	}
}
