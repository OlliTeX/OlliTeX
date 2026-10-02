package compilemanager

import (
	"os"
	"path/filepath"
	"testing"

	clserrors "ollitex/go/services/clsitypst/errors"
)

// sidecarFixture mirrors a live fork output.sourcemap.json (writer geo).
const sidecarFixture = `{
  "typst": "0.15.1+clsi",
  "pages": 2,
  "pageSizes": [[150, 200], [150, 200]],
  "locations": [
    {"span": {"file": "main.typ", "byteOffset": 10}, "page": 0, "x": 10, "y": 60, "w": 40, "h": 12},
    {"span": {"file": "main.typ", "byteOffset": 20}, "page": 0, "x": 10, "y": 90, "w": 30, "h": 12},
    {"span": {"file": "lib.typ", "byteOffset": 5}, "page": 1, "x": 50, "y": 100, "w": 80, "h": 10}
  ]
}`

// seedCompileTree builds the writer-geo compile dir (output.pdf + the
// sidecar next to it, the sources in the compile dir) and, when buildID is
// set, the outputDir/<name>/generated-files/<build>/ copy (the T11
// productionFind land, with output.pdf present).
func seedCompileTree(t *testing.T, m *Manager, projectID, userID, buildID string) string {
	t.Helper()
	compileDir := compileDirOf(m.Paths.CompilesDir, projectID, userID)
	if err := os.MkdirAll(compileDir, 0o755); err != nil {
		t.Fatalf("mkdir compile dir: %v", err)
	}
	// source files in the compile dir (where readFileSync reads them +
	// where Node's synctex -i reads them).
	mustWrite(t, filepath.Join(compileDir, "main.typ"), "alpha\nbeta\n")
	mustWrite(t, filepath.Join(compileDir, "lib.typ"), "in lib\ncontent")
	// output.pdf + the sidecar in the COMPILE dir (writer geo: next to the
	// PDF the runner writes).
	mustWrite(t, filepath.Join(compileDir, "output.pdf"), "%PDF-1.4 fake")
	mustWrite(t, filepath.Join(compileDir, "output.sourcemap.json"), sidecarFixture)
	if buildID != "" {
		// copy the sidecar + output.pdf into the build dir (productionFind
		// semantics); the build dir holds output files only.
		name := compileName(projectID, userID)
		buildDir := filepath.Join(m.Paths.OutputDir, name, "generated-files", buildID)
		if err := os.MkdirAll(buildDir, 0o755); err != nil {
			t.Fatalf("mkdir build dir: %v", err)
		}
		mustWrite(t, filepath.Join(buildDir, "output.pdf"), "%PDF-1.4 fake")
		mustWrite(t, filepath.Join(buildDir, "output.sourcemap.json"), sidecarFixture)
	}
	return compileDir
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// --- validateSyncOpts ---------------------------------------------------------

func TestValidateSyncOpts(t *testing.T) {
	if err := validateSyncOpts(SyncOpts{EditorID: "abc-def012", BuildID: "b1-b2"}); err != nil {
		t.Fatalf("valid opts: %v", err)
	}
	if err := validateSyncOpts(SyncOpts{EditorID: "BAD ID"}); err == nil {
		t.Fatal("editorId with space must be rejected")
	}
	if err := validateSyncOpts(SyncOpts{BuildID: "not-a-buildid"}); err == nil {
		t.Fatal("buildId shape must be rejected")
	}
}

// --- SyncFromCode ----------------------------------------------------------------

func TestSyncFromCodeHappy(t *testing.T) {
	m := &Manager{Paths: Paths{CompilesDir: t.TempDir() + "/compiles", OutputDir: t.TempDir() + "/output"}}
	seedCompileTree(t, m, "p1", "", "")
	// line 1 col 1 (offset 0) -> nearest byteOffset 10 (main.typ)
	res, err := m.SyncFromCode("p1", "", "main.typ", 1, 1, SyncOpts{})
	if err != nil {
		t.Fatalf("SyncFromCode: %v", err)
	}
	if res.DownloadedFromCache {
		t.Fatal("downloadedFromCache must be false")
	}
	if len(res.CodePositions) != 1 {
		t.Fatalf("positions = %d, want 1", len(res.CodePositions))
	}
	r := res.CodePositions[0]
	if r["page"] != 0+1 || r["h"] != 10.0 {
		t.Fatalf("record = %v, want page 1 h 10", r)
	}
	// the record keys must exactly match the clsi view record (flat map).
	for _, k := range []string{"page", "h", "v", "width", "height"} {
		if _, ok := r[k]; !ok {
			t.Fatalf("record missing key %q: %v", k, r)
		}
	}
}

func TestSyncFromCodePerUserBuild(t *testing.T) {
	m := &Manager{Paths: Paths{CompilesDir: t.TempDir() + "/compiles", OutputDir: t.TempDir() + "/output"}}
	seedCompileTree(t, m, "p1", "u1", "cafe-babe")
	res, err := m.SyncFromCode("p1", "u1", "main.typ", 2, 3, SyncOpts{BuildID: "cafe-babe"})
	if err != nil {
		t.Fatalf("SyncFromCode (build): %v", err)
	}
	if len(res.CodePositions) != 1 || res.CodePositions[0]["page"] != 1 {
		t.Fatalf("positions = %v", res.CodePositions)
	}
}

func TestSyncFromCodeUnknownFile(t *testing.T) {
	m := &Manager{Paths: Paths{CompilesDir: t.TempDir() + "/compiles", OutputDir: t.TempDir() + "/output"}}
	seedCompileTree(t, m, "p1", "", "")
	// a file with no sidecar locations -> empty CodePositions (the reader
	// returns nil for that file).
	res, err := m.SyncFromCode("p1", "", "nope.typ", 1, 1, SyncOpts{})
	if err != nil {
		t.Fatalf("SyncFromCode (nope.typ): %v", err)
	}
	if len(res.CodePositions) != 0 {
		t.Fatalf("nope.typ expected empty positions, got %d", len(res.CodePositions))
	}
}

func TestSyncFromCodeNotCompiled(t *testing.T) {
	m := &Manager{Paths: Paths{CompilesDir: t.TempDir() + "/compiles", OutputDir: t.TempDir() + "/output"}}
	seedCompileTree(t, m, "p1", "", "")
	// no output.pdf / sidecar in the default (compile-dir) location for a
	// FRESH project -> NotCompiledError.
	_, err := m.SyncFromCode("fresh", "", "main.typ", 1, 1, SyncOpts{})
	if err == nil || !IsNotCompiled(err) {
		t.Fatalf("want NotCompiled, got %v", err)
	}
}

func TestSyncFromCodeSidecarMissing(t *testing.T) {
	m := &Manager{Paths: Paths{CompilesDir: t.TempDir() + "/compiles", OutputDir: t.TempDir() + "/output"}}
	seedCompileTree(t, m, "p1", "", "")
	// output.pdf present, sidecar removed -> 404 arm (NotFoundError).
	if err := os.Remove(filepath.Join(compileDirOf(m.Paths.CompilesDir, "p1", ""), "output.sourcemap.json")); err != nil {
		t.Fatalf("remove sidecar: %v", err)
	}
	_, err := m.SyncFromCode("p1", "", "main.typ", 1, 1, SyncOpts{})
	if err == nil {
		t.Fatal("want error when sidecar missing")
	}
	if !clserrors.IsNotFoundError(err) {
		t.Fatalf("want clsi NotFound kind, got %v", err)
	}
}

func TestSyncFromCodeMissingFileParam(t *testing.T) {
	m := &Manager{Paths: Paths{CompilesDir: t.TempDir() + "/compiles", OutputDir: t.TempDir() + "/output"}}
	seedCompileTree(t, m, "p1", "", "")
	_, err := m.SyncFromCode("p1", "", "", 1, 1, SyncOpts{})
	if err == nil {
		t.Fatal("want invalid-parameter for empty file")
	}
}

func TestSyncFromCodeBadOptsRejected(t *testing.T) {
	m := &Manager{Paths: Paths{CompilesDir: t.TempDir() + "/compiles", OutputDir: t.TempDir() + "/output"}}
	seedCompileTree(t, m, "p1", "", "")
	_, err := m.SyncFromCode("p1", "", "main.typ", 1, 1, SyncOpts{BuildID: "x y z"})
	if err == nil {
		t.Fatal("want invalid-buildId error")
	}
}

// --- SyncFromPdf -----------------------------------------------------------------

func TestSyncFromPdfHappy(t *testing.T) {
	m := &Manager{Paths: Paths{CompilesDir: t.TempDir() + "/compiles", OutputDir: t.TempDir() + "/output"}}
	seedCompileTree(t, m, "p1", "", "")
	res, err := m.SyncFromPdf("p1", "", 1, 14, 62, SyncOpts{}) // wire page 1 -> sidecar page 0 -> nearest box = loc[0] (main.typ, byteOffset 10)
	if err != nil {
		t.Fatalf("SyncFromPdf: %v", err)
	}
	// main.typ "alpha\nbeta\n": byteOffset 10 -> line 2 col 5
	if len(res.PdfPositions) != 1 {
		t.Fatalf("positions = %d, want 1", len(res.PdfPositions))
	}
	r := res.PdfPositions[0]
	if r["file"] != "main.typ" {
		t.Fatalf("record file = %v, want main.typ", r["file"])
	}
	if r["line"].(int) != 2 || r["column"].(int) != 5 {
		t.Fatalf("record line/col = %v/%v, want 2/5", r["line"], r["column"])
	}
	if res.DownloadedFromCache {
		t.Fatal("downloadedFromCache must be false")
	}
}

func TestSyncFromPdfNoLocationsOnPage(t *testing.T) {
	m := &Manager{Paths: Paths{CompilesDir: t.TempDir() + "/compiles", OutputDir: t.TempDir() + "/output"}}
	seedCompileTree(t, m, "p1", "", "")
	res, err := m.SyncFromPdf("p1", "", 99, 0, 0, SyncOpts{})
	if err != nil {
		t.Fatalf("SyncFromPdf: %v", err)
	}
	if len(res.PdfPositions) != 0 {
		t.Fatalf("want empty positions, got %v", res.PdfPositions)
	}
}

func TestSyncFromPdfLibTyp(t *testing.T) {
	m := &Manager{Paths: Paths{CompilesDir: t.TempDir() + "/compiles", OutputDir: t.TempDir() + "/output"}}
	seedCompileTree(t, m, "p1", "", "")
	// wire page 2 -> sidecar page 1 -> loc[2] (lib.typ, byteOffset 5); lib.typ "in lib\ncontent": offset 5 -> line 1 col 5
	res, err := m.SyncFromPdf("p1", "", 2, 55, 102, SyncOpts{})
	if err != nil {
		t.Fatalf("SyncFromPdf(lib): %v", err)
	}
	if len(res.PdfPositions) != 1 || res.PdfPositions[0]["file"] != "lib.typ" {
		t.Fatalf("lib positions = %v", res.PdfPositions)
	}
	if res.PdfPositions[0]["line"].(int) != 1 || res.PdfPositions[0]["column"].(int) != 6 {
		t.Fatalf("lib line/col = %v/%v", res.PdfPositions[0]["line"], res.PdfPositions[0]["column"])
	}
	if res.DownloadedFromCache {
		t.Fatal("downloadedFromCache must be false")
	}
}

func TestSyncFromPdfNotCompiled(t *testing.T) {
	m := &Manager{Paths: Paths{CompilesDir: t.TempDir() + "/compiles", OutputDir: t.TempDir() + "/output"}}
	seedCompileTree(t, m, "p1", "", "")
	_, err := m.SyncFromPdf("fresh", "", 1, 0, 0, SyncOpts{})
	if err == nil || !IsNotCompiled(err) {
		t.Fatalf("want NotCompiled, got %v", err)
	}
}

func TestSyncFromPdfOptsRejected(t *testing.T) {
	m := &Manager{Paths: Paths{CompilesDir: t.TempDir() + "/compiles", OutputDir: t.TempDir() + "/output"}}
	seedCompileTree(t, m, "p1", "", "")
	_, err := m.SyncFromPdf("p1", "", 1, 0, 0, SyncOpts{EditorID: "UPPER CASE"})
	if err == nil {
		t.Fatal("want invalid-editorId error")
	}
}
