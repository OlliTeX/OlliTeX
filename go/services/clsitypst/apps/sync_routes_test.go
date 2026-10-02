// sync_routes_test.go covers the D21 (T16) sync routes: the 4 new GET
// routes (mux registration + gate coverage), the 405 not-compiled arm, the
// 404 sidecar-missing arm, the 400 invalid-buildId arm, and the parseSync
// query coercion.
package apps

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

const appSyncSidecar = `{
  "typst": "0.15.1+clsi",
  "pages": 1,
  "pageSizes": [[150, 200]],
  "locations": [
    {"span": {"file": "main.typ", "byteOffset": 0}, "page": 0, "x": 10, "y": 60, "w": 40, "h": 12}
  ]
}`

// seedAppSyncCompileDir seeds the manager's compile dir (NOT the build
// dir — these routes use the default no-buildId seam: output.pdf + the
// sidecar + the source file next to it, writer geo).
func seedAppSyncCompileDir(t *testing.T, compilesDir, projectID, userID string) {
	t.Helper()
	name := projectID
	if userID != "" {
		name = projectID + "-" + userID
	}
	dir := filepath.Join(compilesDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	for _, f := range []struct{ name, content string }{
		{"main.typ", "hello world\n"},
		{"output.pdf", "%PDF-1.4 fake"},
		{"output.sourcemap.json", appSyncSidecar},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.content), 0o644); err != nil {
			t.Fatalf("write %s: %v", f.name, err)
		}
	}
}

func decodeSyncBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("bad sync body: %v: %s", err, string(body))
	}
	return m
}

func TestSyncCodeRoute(t *testing.T) {
	a, m := newTestApp(t)
	seedAppSyncCompileDir(t, m.Paths.CompilesDir, "p1", "")
	rec := doGet(t, a.Handler(), "/project/p1/sync/code?file=main.typ&line=1&column=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	body := decodeSyncBody(t, rec.Body.Bytes())
	if body["downloadedFromCache"] != false {
		t.Fatalf("downloadedFromCache = %v, want false", body["downloadedFromCache"])
	}
	pdf, ok := body["pdf"].([]any)
	if !ok || len(pdf) != 1 {
		t.Fatalf("pdf = %v, want 1 record", body["pdf"])
	}
	recMap := pdf[0].(map[string]any)
	for _, k := range []string{"page", "h", "v", "width", "height"} {
		if _, ok := recMap[k]; !ok {
			t.Fatalf("record missing %q: %v", k, recMap)
		}
	}
}

func TestSyncPdfRoute(t *testing.T) {
	a, m := newTestApp(t)
	seedAppSyncCompileDir(t, m.Paths.CompilesDir, "p1", "")
	rec := doGet(t, a.Handler(), "/project/p1/sync/pdf?page=1&h=11&v=61")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	body := decodeSyncBody(t, rec.Body.Bytes())
	if body["downloadedFromCache"] != false {
		t.Fatalf("downloadedFromCache = %v", body["downloadedFromCache"])
	}
	code, ok := body["code"].([]any)
	if !ok || len(code) != 1 {
		t.Fatalf("code = %v, want 1", body["code"])
	}
	r := code[0].(map[string]any)
	if r["file"] != "main.typ" {
		t.Fatalf("file = %v, want main.typ", r["file"])
	}
}

func TestSyncRoutesPerUser(t *testing.T) {
	a, m := newTestApp(t)
	seedAppSyncCompileDir(t, m.Paths.CompilesDir, "p1", "u1")
	rec := doGet(t, a.Handler(), "/project/p1/user/u1/sync/code?file=main.typ&line=1&column=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	rec = doGet(t, a.Handler(), "/project/p1/user/u1/sync/pdf?page=1&h=15&v=65")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
}

func TestSyncCodeRouteNotCompiled(t *testing.T) {
	a, m := newTestApp(t)
	_ = m
	// project has no compile dir at all -> NotCompiled -> 405 (empty).
	rec := doGet(t, a.Handler(), "/project/missing/sync/code?file=main.typ&line=1&column=1")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d, want 405 (body %s)", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("405 must render an empty body, got %q", rec.Body.String())
	}
	rec = doGet(t, a.Handler(), "/project/missing/sync/pdf?page=1&h=0&v=0")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("sync/pdf code = %d, want 405", rec.Code)
	}
}

func TestSyncCodeRouteSidecarMissing(t *testing.T) {
	a, m := newTestApp(t)
	// seed output.pdf only (no sidecar) -> 404 "sourcemap not available"
	// rendered as an empty 404 (LOCKED arm).
	dir := m.Paths.CompilesDir + "/p1"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dir+"/output.pdf", []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rec := doGet(t, a.Handler(), "/project/p1/sync/code?file=main.typ&line=1&column=1")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("404 must render an empty body, got %q", rec.Body.String())
	}
}

func TestSyncRouteInvalidBuildId(t *testing.T) {
	a, m := newTestApp(t)
	_ = m
	rec := doGet(t, a.Handler(), "/project/p1/sync/code?file=main.typ&line=1&column=1&buildId=not%20a%20build")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400, body %s", rec.Code, rec.Body.String())
	}
}

func TestSyncRouteBuildIdSeeded(t *testing.T) {
	a, m := newTestApp(t)
	_ = a
	// buildId set -> the build-dir is used: seed it (the productionFind land).
	compileDir := filepath.Join(m.Paths.CompilesDir, "p1")
	if err := os.MkdirAll(compileDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(compileDir, "output.pdf"), []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatalf("write output.pdf: %v", err)
	}
	buildDir := filepath.Join(m.Paths.OutputDir, "p1", "generated-files", "cafe-babe")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatalf("mkdir buildDir: %v", err)
	}
	for _, f := range []struct{ name, content string }{
		{"output.pdf", "%PDF-1.4 fake"},
		{"output.sourcemap.json", appSyncSidecar},
	} {
		if err := os.WriteFile(filepath.Join(buildDir, f.name), []byte(f.content), 0o644); err != nil {
			t.Fatalf("write %s: %v", f.name, err)
		}
	}
	// source file still lives in the compile dir (readFileSync uses it).
	if err := os.WriteFile(filepath.Join(compileDir, "main.typ"), []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("write main.typ: %v", err)
	}
	rec := doGet(t, a.Handler(), "/project/p1/sync/code?file=main.typ&line=1&column=1&buildId=cafe-babe")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
}

func TestCoerceIntAndParseSyncQuery(t *testing.T) {
	if got := coerceInt(""); got != 0 {
		t.Fatalf("coerceInt(\"\") = %d, want 0", got)
	}
	if got := coerceInt("abc"); got != 0 {
		t.Fatalf("coerceInt(abc) = %d, want 0", got)
	}
	if got := coerceInt("42"); got != 42 {
		t.Fatalf("coerceInt(42) = %d, want 42", got)
	}
	v, _ := url.ParseQuery("file=main.typ&line=2&column=3&page=1&h=10&v=20&buildId=b1-b2&editorId=abcdef&compileFromClsiCache=true")
	sq := parseSyncQuery(v)
	if sq.File != "main.typ" || sq.Line != 2 || sq.Column != 3 || sq.Page != 1 || sq.H != 10 || sq.V != 20 {
		t.Fatalf("parse = %+v", sq)
	}
	if sq.BuildID != "b1-b2" || sq.EditorID != "abcdef" || !sq.CompileFromClsiCache {
		t.Fatalf("opts = %+v", sq)
	}
}
