package compilecontroller

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	compilemanager "ollitex/go/services/clsitypst/compilemanager"
)

const syncSidecar = `{
  "typst": "0.15.1+clsi",
  "pages": 2,
  "pageSizes": [[150, 200], [150, 200]],
  "locations": [
    {"span": {"file": "main.typ", "byteOffset": 0}, "page": 0, "x": 10, "y": 60, "w": 40, "h": 12},
    {"span": {"file": "main.typ", "byteOffset": 8}, "page": 1, "x": 20, "y": 90, "w": 30, "h": 12}
  ]
}`

// seedSyncCompileDir builds the compile dir the (unfaked, os-backed) Manager
// sync path reads: output.pdf + the sidecar next to it, the source file.
func seedSyncCompileDir(t *testing.T, tmp string) {
	compileDir := filepath.Join(tmp, "compiles", "p1")
	// p1/u1 tree too (per-user variant uses <p>-<u>).
	compileDirU := filepath.Join(tmp, "compiles", "p1-u1")
	for _, d := range []string{compileDir, compileDirU} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Helper()
			t.Fatalf("mkdir %s: %v", d, err)
		}
		for _, f := range []struct{ name, content string }{
			{"main.typ", "one two three\nfour five six\n"},
			{"output.pdf", "%PDF-1.4 fake"},
			{"output.sourcemap.json", syncSidecar},
		} {
			if err := os.WriteFile(filepath.Join(d, f.name), []byte(f.content), 0o644); err != nil {
				t.Fatalf("write %s: %v", f.name, err)
			}
		}
	}
}

// buildSyncController builds a Controller whose Manager is the raw (unfaked)
// one from newTestManager (the sync path only needs the os-backed manager:
// no docker, no runner — it reads files).
func buildSyncController(t *testing.T, tmp string) *Controller {
	c := newTestController(t, tmp)
	return c
}

// --- SyncFromCode (wire) ------------------------------------------------------

func TestSyncFromCodeWire(t *testing.T) {
	tmp := t.TempDir()
	seedSyncCompileDir(t, tmp)
	c := buildSyncController(t, tmp)
	res := newCtrlRecorder()
	code, err := c.SyncFromCode(res, ProjectUser{ProjectID: "p1"}, SyncQuery{
		File: "main.typ", Line: 1, Column: 2, BuildID: "",
	})
	if code != http.StatusOK || err != nil {
		t.Fatalf("SyncFromCode: %d %v", code, err)
	}
	var body map[string]any
	if err := json.Unmarshal(res.body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v body=%s", err, res.body.String())
	}
	if body["downloadedFromCache"] != false {
		t.Fatalf("downloadedFromCache = %v, want false", body["downloadedFromCache"])
	}
	pdf, ok := body["pdf"].([]any)
	if !ok || len(pdf) != 1 {
		t.Fatalf("pdf = %v, want 1 record", body["pdf"])
	}
	rec := pdf[0].(map[string]any)
	for _, k := range []string{"page", "h", "v", "width", "height"} {
		if _, ok := rec[k]; !ok {
			t.Fatalf("record missing %q: %v", k, rec)
		}
	}
	if rec["page"].(float64) != 1 {
		t.Fatalf("page = %v, want 1", rec["page"])
	}
}

func TestSyncFromCodePerUserBuild(t *testing.T) {
	tmp := t.TempDir()
	seedSyncCompileDir(t, tmp)
	// the per-user (p1-u1) variant is the compile dir path (buildId empty).
	c := buildSyncController(t, tmp)
	res := newCtrlRecorder()
	code, err := c.SyncFromCode(res, ProjectUser{ProjectID: "p1", UserID: "u1"}, SyncQuery{
		File: "main.typ", Line: 2, Column: 4,
	})
	if code != http.StatusOK || err != nil {
		t.Fatalf("SyncFromCode (per-user): %d %v", code, err)
	}
	var body map[string]any
	_ = json.Unmarshal(res.body.Bytes(), &body)
}

func TestSyncFromCodeBuildDir(t *testing.T) {
	tmp := t.TempDir()
	seedSyncCompileDir(t, tmp)
	// buildId set -> the build-dir path: <output>/p1/generated-files/<build>/.
	buildDir := filepath.Join(tmp, "output", "p1", "generated-files", "cafe-babe")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, f := range []struct{ name, content string }{
		{"output.pdf", "%PDF-1.4 fake"},
		{"output.sourcemap.json", syncSidecar},
	} {
		if err := os.WriteFile(filepath.Join(buildDir, f.name), []byte(f.content), 0o644); err != nil {
			t.Fatalf("write %s: %v", f.name, err)
		}
	}
	c := buildSyncController(t, tmp)
	res := newCtrlRecorder()
	code, err := c.SyncFromCode(res, ProjectUser{ProjectID: "p1"}, SyncQuery{
		File: "main.typ", Line: 1, Column: 1, BuildID: "cafe-babe",
	})
	if code != http.StatusOK || err != nil {
		t.Fatalf("SyncFromCode (build): %d %v (body %s)", code, err, res.body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(res.body.Bytes(), &body)
}

func TestSyncFromCodeNotCompiled(t *testing.T) {
	tmp := t.TempDir()
	seedSyncCompileDir(t, tmp)
	c := buildSyncController(t, tmp)
	res := newCtrlRecorder()
	// "fresh" has no compile dir at all -> NotCompiled -> (0, err).
	code, err := c.SyncFromCode(res, ProjectUser{ProjectID: "fresh"}, SyncQuery{
		File: "main.typ", Line: 1, Column: 1,
	})
	if code != 0 || err == nil {
		t.Fatalf("want (0, NotCompiled), got %d %v", code, err)
	}
	if !compilemanager.IsNotCompiled(err) {
		t.Fatalf("want NotCompiled, got %v", err)
	}
}

func TestSyncFromCodeEmptyFile(t *testing.T) {
	tmp := t.TempDir()
	seedSyncCompileDir(t, tmp)
	c := buildSyncController(t, tmp)
	_, err := c.SyncFromCode(newCtrlRecorder(), ProjectUser{ProjectID: "p1"}, SyncQuery{
		File: "", Line: 1, Column: 1,
	})
	if err == nil {
		t.Fatal("want invalid-parameter for empty file")
	}
}

func TestSyncFromCodeBadBuild(t *testing.T) {
	tmp := t.TempDir()
	seedSyncCompileDir(t, tmp)
	c := buildSyncController(t, tmp)
	_, err := c.SyncFromCode(newCtrlRecorder(), ProjectUser{ProjectID: "p1"}, SyncQuery{
		File: "main.typ", Line: 1, Column: 1, BuildID: "not a build",
	})
	if err == nil {
		t.Fatal("want invalid-buildId error")
	}
}

// --- SyncFromPdf (wire) ---------------------------------------------------------

func TestSyncFromPdfWire(t *testing.T) {
	tmp := t.TempDir()
	seedSyncCompileDir(t, tmp)
	c := buildSyncController(t, tmp)
	res := newCtrlRecorder()
	code, err := c.SyncFromPdf(res, ProjectUser{ProjectID: "p1"}, SyncQuery{
		Page: 1, H: 15, V: 65,
	})
	if code != http.StatusOK || err != nil {
		t.Fatalf("SyncFromPdf: %d %v body=%s", code, err, res.body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(res.body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v body=%s", err, res.body.String())
	}
	if body["downloadedFromCache"] != false {
		t.Fatalf("downloadedFromCache = %v", body["downloadedFromCache"])
	}
	codeRecs, ok := body["code"].([]any)
	if !ok || len(codeRecs) != 1 {
		t.Fatalf("code = %v, want 1", body["code"])
	}
	r := codeRecs[0].(map[string]any)
	if r["file"] != "main.typ" {
		t.Fatalf("file = %v, want main.typ", r["file"])
	}
	if r["line"].(float64) < 1 || r["column"].(float64) < 1 {
		t.Fatalf("line/col = %v/%v", r["line"], r["column"])
	}
}

func TestSyncFromPdfNoLocations(t *testing.T) {
	tmp := t.TempDir()
	seedSyncCompileDir(t, tmp)
	c := buildSyncController(t, tmp)
	res := newCtrlRecorder()
	// page 99 has no locations -> empty "code".
	code, err := c.SyncFromPdf(res, ProjectUser{ProjectID: "p1"}, SyncQuery{
		Page: 99, H: 0, V: 0,
	})
	if code != http.StatusOK || err != nil {
		t.Fatalf("SyncFromPdf: %d %v", code, err)
	}
	var body map[string]any
	_ = json.Unmarshal(res.body.Bytes(), &body)
	codeRecs, ok := body["code"].([]any)
	if !ok || len(codeRecs) != 0 {
		t.Fatalf("code = %v, want empty", body["code"])
	}
}

func TestSyncFromPdfNotCompiled(t *testing.T) {
	tmp := t.TempDir()
	seedSyncCompileDir(t, tmp)
	c := buildSyncController(t, tmp)
	code, err := c.SyncFromPdf(newCtrlRecorder(), ProjectUser{ProjectID: "fresh"}, SyncQuery{
		Page: 1, H: 0, V: 0,
	})
	if code != 0 || err == nil {
		t.Fatalf("want (0, NotCompiled), got %d %v", code, err)
	}
	if !compilemanager.IsNotCompiled(err) {
		t.Fatalf("want NotCompiled, got %v", err)
	}
}
